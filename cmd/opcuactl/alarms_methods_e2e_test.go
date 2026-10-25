// SPDX-License-Identifier: MIT

package opcuactl

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-opcua"
	"github.com/otfabric/go-opcua/id"
	"github.com/otfabric/go-opcua/server"
	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/opcuactl/internal/model"
)

// alarm105Server is a test server with the alarm states and methods of
// Part 9 1.05: a pump alarm with every optional state, a dialog, an alarm
// group, alarm metrics and auditing.
type alarm105Server struct {
	*testServer
	pump, dialog *server.Condition
	group        string
	metrics      string
}

func startAlarm105Server(t *testing.T) *alarm105Server {
	t.Helper()
	s := startServer(t, server.WithAuditing(true))
	area := server.NewFolderNode(ua.NewStringNodeID(s.ns.ID(), "Area"), "Area")
	s.ns.AddNode(area)
	s.ns.Objects().AddRef(area, server.RefTypeIDOrganizes, true)

	g, err := s.srv.AddAlarmGroup(server.AlarmGroupConfig{NodeID: ua.NewStringNodeID(s.ns.ID(), "Area.Suppression"), Name: "AreaSuppression", Suppression: true})
	if err != nil {
		t.Fatalf("AddAlarmGroup: %v", err)
	}
	add := func(cfg server.ConditionConfig) *server.Condition {
		cfg.Source = area.ID()
		cfg.SourceName = "Area"
		cfg.NodeID = ua.NewStringNodeID(s.ns.ID(), "Area."+cfg.Name)
		c, err := s.srv.AddCondition(cfg)
		if err != nil {
			t.Fatalf("AddCondition %s: %v", cfg.Name, err)
		}
		return c
	}
	a := &alarm105Server{
		testServer: s,
		group:      ua.NewStringNodeID(s.ns.ID(), "Area.Suppression").String(),
		metrics:    ua.NewStringNodeID(s.ns.ID(), "AlarmMetrics").String(),
		pump: add(server.ConditionConfig{
			Type:              ua.NewNumericNodeID(0, id.OffNormalAlarmType),
			Name:              "PumpAlarm",
			Severity:          800,
			Message:           "Pump tripped",
			Confirmable:       true,
			Suppressible:      true,
			Serviceable:       true,
			Silenceable:       true,
			Latching:          true,
			Branching:         true,
			Shelving:          true,
			SuppressionGroups: []*ua.NodeID{ua.NewStringNodeID(s.ns.ID(), "Area.Suppression")},
		}),
		dialog: add(server.ConditionConfig{
			Type:     ua.NewNumericNodeID(0, id.DialogConditionType),
			Name:     "StartDialog",
			Severity: 300,
			Dialog:   &server.DialogConfig{Prompt: "Start the pump?", ResponseOptions: []string{"Yes", "No"}, Active: true},
		}),
	}
	_ = g
	if _, err := s.srv.AddAlarmMetrics(server.AlarmMetricsConfig{NodeID: ua.NewStringNodeID(s.ns.ID(), "AlarmMetrics"), Namespace: s.ns}); err != nil {
		t.Fatalf("AddAlarmMetrics: %v", err)
	}
	if err := a.pump.SetActive(true); err != nil {
		t.Fatalf("SetActive: %v", err)
	}
	return a
}

func TestE2E_AlarmStateMethods(t *testing.T) {
	a := startAlarm105Server(t)
	ep := []string{"-e", a.url}
	run := func(t *testing.T, args ...string) string { t.Helper(); return mustRun(t, append(args, ep...)...) }
	fail := func(t *testing.T, wantStderr string, args ...string) {
		t.Helper()
		_, errOut, err := runCLI(t, append(args, ep...)...)
		if err == nil {
			t.Fatalf("opcuactl %s succeeded", strings.Join(args, " "))
		}
		if wantStderr != "" && !strings.Contains(errOut, wantStderr) {
			t.Errorf("opcuactl %s: stderr does not contain %q:\n%s", strings.Join(args, " "), wantStderr, errOut)
		}
	}
	pump := a.pump.NodeID().String()
	dialog := a.dialog.NodeID().String()

	t.Run("list shows the 1.05 states", func(t *testing.T) {
		c := findCondition(listConditions(t, ep...), pump)
		if c == nil || c.Suppressed == nil || *c.Suppressed || c.OutOfService == nil || c.Silenced == nil || c.Latched == nil || !*c.Latched {
			t.Fatalf("PumpAlarm = %+v", c)
		}
		d := findCondition(listConditions(t, ep...), dialog)
		if d == nil || d.DialogActive == nil || !*d.DialogActive || d.Prompt != "Start the pump?" {
			t.Fatalf("StartDialog = %+v", d)
		}
		out := run(t, "alarms", "list", "-v")
		wantContains(t, out, "STATES", "latched", "dialog")
		if conds := listConditions(t, append(ep, "--state", "dialog")...); len(conds) != 1 || conds[0].ConditionID != dialog {
			t.Errorf("--state dialog = %+v", conds)
		}
		if conds := listConditions(t, append(ep, "--state", "latched")...); len(conds) != 1 {
			t.Errorf("--state latched = %+v", conds)
		}
	})

	t.Run("suppress and unsuppress", func(t *testing.T) {
		out := run(t, "alarms", "suppress", pump, "-m", "line stopped")
		wantContains(t, out, "suppress", "Good")
		if st := a.pump.State(); !st.Suppressed || st.Comment == nil || st.Comment.Text != "line stopped" {
			t.Fatalf("after suppress: %+v", st)
		}
		if conds := listConditions(t, append(ep, "--state", "suppressed")...); len(conds) != 1 {
			t.Errorf("--state suppressed = %+v", conds)
		}
		run(t, "alarms", "unsuppress", pump)
		if a.pump.State().Suppressed {
			t.Fatal("still suppressed")
		}
		fail(t, "does not have this method", "alarms", "suppress", dialog)
	})

	t.Run("out of service", func(t *testing.T) {
		var res model.AlarmAction
		out := run(t, "alarms", "remove-from-service", pump, "--comment", "sensor repair", "--format", "json")
		if err := json.Unmarshal([]byte(out), &res); err != nil {
			t.Fatal(err)
		}
		if res.Action != "remove-from-service" || res.Comment != "sensor repair" {
			t.Errorf("result = %+v", res)
		}
		if !a.pump.State().OutOfService {
			t.Fatal("not out of service")
		}
		if conds := listConditions(t, append(ep, "--state", "out-of-service")...); len(conds) != 1 {
			t.Errorf("--state out-of-service = %+v", conds)
		}
		run(t, "alarms", "in-service", pump, "-m", "repaired")
		if a.pump.State().OutOfService {
			t.Fatal("still out of service")
		}
	})

	t.Run("silence", func(t *testing.T) {
		run(t, "alarms", "silence", pump)
		if !a.pump.State().Silenced {
			t.Fatal("not silenced")
		}
		if conds := listConditions(t, append(ep, "--state", "silenced")...); len(conds) != 1 {
			t.Errorf("--state silenced = %+v", conds)
		}
		if _, _, err := runCLI(t, append([]string{"alarms", "silence", pump, "-m", "x"}, ep...)...); err == nil {
			t.Error("silence accepted a comment")
		}
	})

	t.Run("reset", func(t *testing.T) {
		fail(t, "cannot be reset", "alarms", "reset", pump)
		if err := a.pump.SetActive(false); err != nil {
			t.Fatal(err)
		}
		run(t, "alarms", "reset", pump, "-m", "cleared")
		if a.pump.State().Latched {
			t.Fatal("still latched")
		}
		if err := a.pump.SetActive(true); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("shelve with a comment", func(t *testing.T) {
		out := run(t, "alarms", "shelve", pump, "--for", "10m", "-m", "known issue")
		wantContains(t, out, "timed-shelve")
		if st := a.pump.State(); st.Shelving != server.TimedShelved || st.Comment == nil || st.Comment.Text != "known issue" {
			t.Fatalf("after timed shelve: %+v", st)
		}
		run(t, "alarms", "unshelve", pump, "-m", "done")
		if st := a.pump.State(); st.Shelving != server.Unshelved || st.Comment.Text != "done" {
			t.Fatalf("after unshelve: %+v", st)
		}
		run(t, "alarms", "shelve", pump, "--one-shot", "--comment", "once")
		if st := a.pump.State(); st.Shelving != server.OneShotShelved || st.Comment.Text != "once" {
			t.Fatalf("after one-shot: %+v", st)
		}
		run(t, "alarms", "unshelve", pump)
	})

	t.Run("respond", func(t *testing.T) {
		fail(t, "not a response option", "alarms", "respond", dialog, "Maybe")
		var res model.AlarmAction
		out := run(t, "alarms", "respond", dialog, "yes", "-m", "go", "--format", "json")
		if err := json.Unmarshal([]byte(out), &res); err != nil {
			t.Fatal(err)
		}
		if res.SelectedResponse == nil || *res.SelectedResponse != 0 || res.Response != "Yes" {
			t.Errorf("respond = %+v", res)
		}
		if st := a.dialog.State(); st.DialogActive || st.LastResponse != 0 {
			t.Fatalf("after respond: %+v", st)
		}
		fail(t, "dialog is not active", "alarms", "respond", dialog, "1")
		if err := a.dialog.ShowDialog("Stop the pump?"); err != nil {
			t.Fatal(err)
		}
		fail(t, "options: 0=Yes, 1=No", "alarms", "respond", dialog, "7")
		out = run(t, "alarms", "respond", dialog, "1")
		wantContains(t, out, "RESPONSE", "1 No")
	})

	t.Run("groups", func(t *testing.T) {
		out := run(t, "alarms", "groups", pump)
		wantContains(t, out, "GROUP ID", a.group, "AreaSuppression")
		var groups []model.AlarmGroup
		if err := json.Unmarshal([]byte(run(t, "alarms", "groups", pump, "--format", "json")), &groups); err != nil {
			t.Fatal(err)
		}
		if len(groups) != 1 || groups[0].NodeID != a.group || groups[0].BrowseName != "AreaSuppression" {
			t.Errorf("groups = %+v", groups)
		}
		if out := run(t, "alarms", "groups", pump, "-q"); strings.TrimSpace(out) != a.group {
			t.Errorf("quiet = %q", out)
		}
		fail(t, "GetGroupMemberships", "alarms", "groups", dialog)
	})

	t.Run("metrics", func(t *testing.T) {
		out := run(t, "alarms", "metrics", "get", a.metrics)
		wantContains(t, out, "Alarm metrics", "Alarm count:", "Average alarm rate:")
		var m model.AlarmMetrics
		if err := json.Unmarshal([]byte(run(t, "alarms", "metrics", "get", a.metrics, "--format", "json")), &m); err != nil {
			t.Fatal(err)
		}
		if m.NodeID != a.metrics || m.AlarmCount == 0 || m.RateMinutes != 1 || m.StartTime == "" {
			t.Errorf("metrics = %+v", m)
		}
		run(t, "alarms", "metrics", "reset", a.metrics)
		if err := json.Unmarshal([]byte(run(t, "alarms", "metrics", "get", a.metrics, "--format", "json")), &m); err != nil {
			t.Fatal(err)
		}
		if m.AlarmCount != 0 {
			t.Errorf("after reset: %+v", m)
		}
		wantContains(t, run(t, "alarms", "metrics", "get", a.metrics, "-q"), "0")
		fail(t, "invalid NodeId", "alarms", "metrics", "get", "nonsense")
		fail(t, "invalid NodeId", "alarms", "metrics", "reset", "nonsense")
		fail(t, "reset the metrics of", "alarms", "metrics", "reset", "ns=1;s=None")
	})

	t.Run("branches", func(t *testing.T) {
		branch, err := a.pump.CreateBranch()
		if err != nil {
			t.Fatal(err)
		}
		out := run(t, "alarms", "list")
		wantContains(t, out, "BRANCH", branch.String())
		var found bool
		for _, c := range listConditions(t, append(ep, "--state", "branch")...) {
			found = found || (c.BranchID == branch.String() && c.ConditionID == pump)
		}
		if !found {
			t.Fatalf("--state branch does not list %s", branch)
		}
		run(t, "alarms", "ack", pump, "--branch", branch.String(), "-m", "old one")
		if st, ok := a.pump.Branch(branch); ok && !st.Acked {
			t.Fatalf("branch not acknowledged: %+v", st)
		}
		if a.pump.State().Acked {
			t.Error("acknowledging the branch acknowledged the condition")
		}
		fail(t, "no retained branch", "alarms", "ack", pump, "--branch", "ns=1;s=NoBranch")
		fail(t, "exclude each other", "alarms", "ack", pump, "--branch", branch.String(), "--event-id", "AQID")
		fail(t, "invalid --branch", "alarms", "ack", pump, "--branch", "garbage")
	})
}

// TestE2E_AlarmsAudit streams the audit events of condition methods, and
// checks that --audit-entry-id reaches the server.
func TestE2E_AlarmsAudit(t *testing.T) {
	a := startAlarm105Server(t)
	pump := a.pump.NodeID().String()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c, err := opcua.NewClient(a.url, opcua.SecurityMode(ua.MessageSecurityModeNone))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close(context.Background()) }()

	// alarms audit, while another client comments with an AuditEntryId
	// until the subscription has seen one.
	call := func(fn func(context.Context) error) (stop func()) {
		done := make(chan struct{})
		stopped := make(chan struct{})
		go func() {
			defer close(stopped)
			for {
				select {
				case <-done:
					return
				case <-time.After(100 * time.Millisecond):
				}
				_ = fn(ctx)
			}
		}()
		return func() { close(done); <-stopped }
	}
	stop := call(func(ctx context.Context) error {
		return c.AddComment(opcua.WithAuditEntryID(ctx, "ticket-1"), a.pump.NodeID(), a.pump.State().EventID, "audited")
	})
	out, errOut, err := runCLI(t, "alarms", "audit", "-e", a.url, "--count", "1", "--duration", "20s", "--jsonl", "--entry-id", "ticket-1")
	stop()
	if err != nil {
		t.Fatalf("audit: %v\n%s", err, errOut)
	}
	var ev model.AuditEvent
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &ev); err != nil {
		t.Fatalf("not JSON: %q: %v", out, err)
	}
	if ev.ConditionID != pump || ev.ClientAuditEntryID != "ticket-1" || ev.Comment != "audited" || !ev.Status || ev.Method != "AddComment" || ev.ConditionEventID == "" {
		t.Errorf("audit event = %+v", ev)
	}

	// The table form, for a refused call.
	stop = call(func(ctx context.Context) error { return c.Unshelve(ctx, a.pump.NodeID()) })
	out, errOut, err = runCLI(t, "alarms", "audit", "-e", a.url, "--count", "1", "--duration", "20s", "--node", "i=2253")
	stop()
	if err != nil {
		t.Fatalf("audit: %v\n%s", err, errOut)
	}
	wantContains(t, out, pump, "Unshelve", "refused")
	if _, _, err := runCLI(t, "alarms", "audit", "-e", a.url, "--node", "bad"); err == nil {
		t.Error("an invalid --node was accepted")
	}

	// --audit-entry-id: the server reports it in the audit event of the call.
	ch := make(chan *opcua.PublishNotificationData, 16)
	sub, err := c.Subscribe(ctx, &opcua.SubscriptionParameters{Interval: 50 * time.Millisecond}, ch)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sub.Monitor(ctx, ua.TimestampsToReturnBoth, &ua.MonitoredItemCreateRequest{
		ItemToMonitor:  &ua.ReadValueID{NodeID: ua.NewNumericNodeID(0, id.Server), AttributeID: ua.AttributeIDEventNotifier},
		MonitoringMode: ua.MonitoringModeReporting,
		RequestedParameters: &ua.MonitoringParameters{ClientHandle: 1, QueueSize: 100,
			Filter: ua.NewExtensionObject(opcua.NewAuditConditionEventFilter())},
	}); err != nil {
		t.Fatal(err)
	}
	var res model.AlarmAction
	if err := json.Unmarshal([]byte(mustRun(t, "alarms", "silence", pump, "-e", a.url, "--audit-entry-id", "ticket-2", "--format", "json")), &res); err != nil {
		t.Fatal(err)
	}
	if res.AuditEntryID != "ticket-2" {
		t.Errorf("result = %+v", res)
	}
	for {
		select {
		case <-ctx.Done():
			t.Fatal("no audit event with the AuditEntryId ticket-2")
		case notif := <-ch:
			evs, _ := auditEvents(notif)
			for _, ev := range evs {
				if ev.ClientAuditEntryID == "ticket-2" && methodName(ev.MethodID) == "Silence" {
					return
				}
			}
		}
	}
}
