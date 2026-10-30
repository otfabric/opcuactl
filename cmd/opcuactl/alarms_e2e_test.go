// SPDX-License-Identifier: MIT

package opcuactl

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-opcua/id"
	"github.com/otfabric/go-opcua/server"
	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/opcuactl/internal/model"
)

// alarmServer is a test server with a tank and three conditions: an active
// confirmable, shelvable level alarm, a retained ConditionType and an
// inactive alarm the server does not retain.
type alarmServer struct {
	*testServer
	level, info, quiet *server.Condition
	tank               string
}

func startAlarmServer(t *testing.T) *alarmServer {
	t.Helper()
	s := startServer(t)
	tank := server.NewFolderNode(ua.NewStringNodeID(s.ns.ID(), "Tank"), "Tank")
	s.ns.AddNode(tank)
	s.ns.Objects().AddRef(tank, server.RefTypeIDOrganizes, true)

	add := func(cfg server.ConditionConfig) *server.Condition {
		cfg.Source = tank.ID()
		cfg.SourceName = "Tank"
		cfg.NodeID = ua.NewStringNodeID(s.ns.ID(), "Tank."+cfg.Name)
		c, err := s.srv.AddCondition(cfg)
		if err != nil {
			t.Fatalf("AddCondition %s: %v", cfg.Name, err)
		}
		return c
	}
	a := &alarmServer{
		testServer: s,
		tank:       tank.ID().String(),
		level: add(server.ConditionConfig{
			Type:           ua.NewNumericNodeID(0, id.ExclusiveLevelAlarmType),
			Name:           "LevelAlarm",
			Severity:       700,
			Message:        "Tank level high",
			Confirmable:    true,
			Shelving:       true,
			MaxTimeShelved: 2 * time.Hour,
		}),
		info: add(server.ConditionConfig{
			Type:     ua.NewNumericNodeID(0, id.ConditionType),
			Name:     "Info",
			Severity: 100,
			Message:  "maintenance due",
			Retain:   true,
		}),
		quiet: add(server.ConditionConfig{Name: "Quiet", Severity: 300}),
	}
	if err := a.level.SetActive(true); err != nil {
		t.Fatalf("SetActive: %v", err)
	}
	return a
}

func (a *alarmServer) id(c *server.Condition) string { return c.NodeID().String() }

// listConditions runs alarms list --format json.
func listConditions(t *testing.T, args ...string) []model.Condition {
	t.Helper()
	out := mustRun(t, append([]string{"alarms", "list", "--format", "json"}, args...)...)
	var conds []model.Condition
	if err := json.Unmarshal([]byte(out), &conds); err != nil {
		t.Fatalf("alarms list json: %v\n%s", err, out)
	}
	return conds
}

func findCondition(conds []model.Condition, conditionID string) *model.Condition {
	for i := range conds {
		if conds[i].ConditionID == conditionID {
			return &conds[i]
		}
	}
	return nil
}

func TestE2E_Alarms(t *testing.T) {
	a := startAlarmServer(t)
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
	level := a.id(a.level)

	t.Run("list", func(t *testing.T) {
		out := run(t, "alarms", "list")
		wantContains(t, out, "CONDITION ID", "EVENT ID", level, "LevelAlarm", "Info", "Tank", "700")
		if strings.Contains(out, "Quiet") {
			t.Errorf("a condition the server does not retain is listed:\n%s", out)
		}
		lines := strings.Split(strings.TrimSpace(out), "\n")
		if len(lines) != 3 || !strings.Contains(lines[1], "LevelAlarm") {
			t.Errorf("want the header, then by severity LevelAlarm and Info:\n%s", out)
		}

		conds := listConditions(t, ep...)
		c := findCondition(conds, level)
		if c == nil || len(conds) != 2 {
			t.Fatalf("conditions = %+v", conds)
		}
		st := a.level.State()
		if c.EventID != base64.StdEncoding.EncodeToString(st.EventID) {
			t.Errorf("EventId %s, the server's latest is %x", c.EventID, st.EventID)
		}
		if c.ConditionName != "LevelAlarm" || c.Severity != 700 || c.Message != "Tank level high" || c.SourceName != "Tank" ||
			c.EventType != "ExclusiveLevelAlarmType" || !c.Enabled || !c.Retain || c.Shelving != "Unshelved" ||
			c.Active == nil || !*c.Active || c.Acked == nil || *c.Acked || c.Confirmed == nil || *c.Confirmed {
			t.Errorf("LevelAlarm = %+v", c)
		}
		if info := findCondition(conds, a.id(a.info)); info == nil || info.Acked != nil || info.Active != nil || info.Shelving != "" {
			t.Errorf("Info (ConditionType: no Acked, Active or Shelving) = %+v", info)
		}

		out = run(t, "alarms", "list", "-v", "--node", a.tank)
		wantContains(t, out, "MESSAGE", "Tank level high")
		wantContains(t, run(t, "alarms", "list", "-q"), level+"\n")
	})

	t.Run("list filters", func(t *testing.T) {
		if conds := listConditions(t, append(ep, "--state", "unacked")...); len(conds) != 1 || conds[0].ConditionID != level {
			t.Errorf("--state unacked = %+v", conds)
		}
		if conds := listConditions(t, append(ep, "--severity-min", "500")...); len(conds) != 1 {
			t.Errorf("--severity-min 500 = %+v", conds)
		}
		if conds := listConditions(t, append(ep, "--state", "shelved")...); len(conds) != 0 {
			t.Errorf("--state shelved = %+v", conds)
		}
		fail(t, "unknown --state", "alarms", "list", "--state", "angry")
		fail(t, "invalid --node", "alarms", "list", "--node", "nonsense")
		fail(t, "Bad", "alarms", "list", "--node", "ns=99;s=Nowhere")
	})

	t.Run("acknowledge and confirm", func(t *testing.T) {
		out := run(t, "alarms", "ack", level, "-m", "on my way")
		wantContains(t, out, "acknowledge", "Good", "EVENT ID")
		st := a.level.State()
		if !st.Acked || st.Confirmed || st.Comment == nil || st.Comment.Text != "on my way" {
			t.Fatalf("after ack: %+v", st)
		}
		fail(t, "already acknowledged", "alarms", "ack", level)

		if conds := listConditions(t, append(ep, "--state", "unconfirmed")...); len(conds) != 1 {
			t.Errorf("--state unconfirmed after ack = %+v", conds)
		}

		// An explicit EventId: the latest one, as alarms list printed it.
		c := findCondition(listConditions(t, ep...), level)
		out = run(t, "alarms", "confirm", level, "--event-id", c.EventID, "--format", "json")
		var res model.AlarmAction
		if err := json.Unmarshal([]byte(out), &res); err != nil {
			t.Fatalf("json: %v\n%s", err, out)
		}
		if res.Action != "confirm" || res.Status != "Good" || res.EventID != c.EventID {
			t.Errorf("confirm = %+v", res)
		}
		if !a.level.State().Confirmed {
			t.Error("not confirmed")
		}
		fail(t, "already confirmed", "alarms", "confirm", level)
	})

	t.Run("comment", func(t *testing.T) {
		run(t, "alarms", "comment", level, "-m", "sensor replaced")
		if st := a.level.State(); st.Comment == nil || st.Comment.Text != "sensor replaced" {
			t.Errorf("comment = %+v", st.Comment)
		}
		fail(t, "-m/--message is required", "alarms", "comment", level)
		fail(t, "not one of the recent events", "alarms", "comment", level, "-m", "x", "--event-id", "0x0102")
		fail(t, "invalid --event-id", "alarms", "comment", level, "-m", "x", "--event-id", "!!")
	})

	t.Run("not retained", func(t *testing.T) {
		fail(t, "not retained", "alarms", "ack", a.id(a.quiet))
		fail(t, "invalid ConditionId", "alarms", "ack", "garbage")
		if _, _, err := runCLI(t, append([]string{"alarms", "ack"}, ep...)...); err == nil {
			t.Error("alarms ack without a ConditionId succeeded")
		}
	})

	t.Run("method a ConditionType does not have", func(t *testing.T) {
		_, _, err := runCLI(t, append([]string{"alarms", "ack", a.id(a.info)}, ep...)...)
		if err == nil || ExitCode(err) != 6 {
			t.Errorf("ack of a ConditionType: %v (exit %d)", err, ExitCode(err))
		}
		fail(t, "cannot be shelved", "alarms", "shelve", a.id(a.info), "--one-shot")
	})

	t.Run("shelve", func(t *testing.T) {
		out := run(t, "alarms", "shelve", level, "--for", "1h")
		wantContains(t, out, "timed-shelve", "Good")
		if st := a.level.State(); st.Shelving != server.TimedShelved {
			t.Fatalf("shelving = %v", st.Shelving)
		}
		if conds := listConditions(t, append(ep, "--state", "shelved")...); len(conds) != 1 || conds[0].Shelving != "TimedShelved" {
			t.Errorf("--state shelved = %+v", conds)
		}
		wantContains(t, run(t, "alarms", "list"), "timed")
		fail(t, "already shelved", "alarms", "shelve", level, "--for", "30m")

		run(t, "alarms", "unshelve", level)
		if st := a.level.State(); st.Shelving != server.Unshelved {
			t.Fatalf("after unshelve: %v", st.Shelving)
		}
		fail(t, "not shelved", "alarms", "unshelve", level)

		run(t, "alarms", "shelve", level, "--one-shot")
		if st := a.level.State(); st.Shelving != server.OneShotShelved {
			t.Fatalf("one-shot: %v", st.Shelving)
		}
		wantContains(t, run(t, "alarms", "list"), "one-shot")
		run(t, "alarms", "unshelve", level)

		fail(t, "MaxTimeShelved", "alarms", "shelve", level, "--for", "3h")
		fail(t, "exclude each other", "alarms", "shelve", level, "--for", "1h", "--one-shot")
		fail(t, "--for <duration>", "alarms", "shelve", level)
	})

	t.Run("disable and enable", func(t *testing.T) {
		run(t, "alarms", "disable", level)
		if a.level.State().Enabled {
			t.Fatal("still enabled")
		}
		if findCondition(listConditions(t, ep...), level) != nil {
			t.Error("a disabled condition is listed")
		}
		fail(t, "already disabled", "alarms", "disable", level)
		fail(t, "disabled", "alarms", "comment", level, "-m", "x", "--event-id", "AQID")

		run(t, "alarms", "enable", level)
		if !a.level.State().Enabled {
			t.Fatal("not enabled")
		}
		fail(t, "already enabled", "alarms", "enable", level)
	})

	t.Run("access denied", func(t *testing.T) {
		if h := alarmHint(ua.StatusBadUserAccessDenied); !strings.Contains(h, "--auth") {
			t.Errorf("hint = %q", h)
		}
	})
}

// TestE2E_AlarmsWatch streams the refresh, then live condition events.
func TestE2E_AlarmsWatch(t *testing.T) {
	a := startAlarmServer(t)

	t.Run("refresh", func(t *testing.T) {
		// Two retained conditions; the refresh start marker comes first.
		out := mustRun(t, "alarms", "watch", "-e", a.url, "--count", "2", "--duration", "20s", "--format", "json")
		lines := strings.Split(strings.TrimSpace(out), "\n")
		var got []model.ConditionNotification
		for _, l := range lines {
			var n model.ConditionNotification
			if err := json.Unmarshal([]byte(l), &n); err != nil {
				t.Fatalf("not JSONL: %q: %v", l, err)
			}
			got = append(got, n)
		}
		if len(got) != 3 || got[0].Refresh != "start" || got[1].Condition == nil || got[2].Condition == nil {
			t.Fatalf("watch = %s", out)
		}
		ids := got[1].ConditionID + " " + got[2].ConditionID
		if !strings.Contains(ids, a.id(a.level)) || !strings.Contains(ids, a.id(a.info)) {
			t.Errorf("refreshed %s", ids)
		}
	})

	t.Run("table", func(t *testing.T) {
		out := mustRun(t, "alarms", "watch", "-e", a.url, "--count", "2", "--duration", "20s", "--node", a.tank)
		wantContains(t, out, "-- refresh start --", "LevelAlarm", "severity=700", "active=yes", "acked=no", `"Tank level high"`)
	})

	t.Run("live", func(t *testing.T) {
		// Without a refresh only live events arrive: change the severity until
		// the command has seen one (events before its monitored item exists
		// are not delivered to it).
		done := make(chan struct{})
		stopped := make(chan struct{})
		go func() {
			defer close(stopped)
			sev := uint16(800)
			for {
				select {
				case <-done:
					return
				case <-time.After(50 * time.Millisecond):
				}
				_ = a.level.SetSeverity(sev)
				sev = 1700 - sev // 800, 900, 800, ...
			}
		}()
		out, errOut, err := runCLI(t, "alarms", "watch", "-e", a.url, "--no-refresh", "--count", "1", "--duration", "20s", "--jsonl")
		close(done)
		<-stopped
		if err != nil {
			t.Fatalf("watch: %v\n%s", err, errOut)
		}
		var n model.ConditionNotification
		if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &n); err != nil {
			t.Fatalf("not JSON: %q: %v", out, err)
		}
		if n.Refresh != "" || n.Condition == nil || n.ConditionID != a.id(a.level) || n.Severity < 800 {
			t.Errorf("live event = %+v", n)
		}
	})
}
