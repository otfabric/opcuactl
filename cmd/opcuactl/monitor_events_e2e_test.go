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
	"github.com/otfabric/opcuactl/internal/serve"
)

type cliResult struct {
	stdout, stderr string
	err            error
}

// runWhile runs opcuactl in the background and calls tick every 50 ms
// until it exits: the command subscribes at some point, and tick makes the
// events it waits for.
func runWhile(t *testing.T, tick func(), args ...string) cliResult {
	t.Helper()
	done := make(chan cliResult, 1)
	go func() {
		out, errOut, err := runCLI(t, args...)
		done <- cliResult{out, errOut, err}
	}()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.After(30 * time.Second)
	for {
		select {
		case r := <-done:
			return r
		case <-ticker.C:
			tick()
		case <-timeout:
			t.Fatalf("opcuactl %s did not exit", strings.Join(args, " "))
		}
	}
}

func monitoredEvents(t *testing.T, out string) []model.MonitoredEvent {
	t.Helper()
	var evs []model.MonitoredEvent
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		var ev model.MonitoredEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("not JSONL: %q: %v", line, err)
		}
		evs = append(evs, ev)
	}
	return evs
}

// TestE2E_MonitorEvent subscribes to the events of the Server object while
// the server reports events of two severities.
func TestE2E_MonitorEvent(t *testing.T) {
	s := startServer(t)
	serverObj := ua.NewNumericNodeID(0, id.Server)
	emit := func() {
		for _, ev := range []struct {
			msg string
			sev uint16
		}{{"low", 100}, {"high", 500}} {
			_ = s.srv.EmitBaseEvent(serverObj, &server.BaseEvent{
				EventID:    []byte(ev.msg + time.Now().String()),
				EventType:  ua.NewNumericNodeID(0, id.BaseEventType),
				SourceNode: serverObj,
				SourceName: "Server",
				Time:       time.Now(),
				Message:    ua.NewLocalizedText(ev.msg),
				Severity:   ev.sev,
			})
		}
	}
	monitor := func(args ...string) []string {
		return append([]string{"monitor", "event", "-e", s.url, "-n", "i=2253", "--duration", "20s"}, args...)
	}

	t.Run("filtered", func(t *testing.T) {
		r := runWhile(t, emit, monitor("--count", "3", "--jsonl", "--severity-min", "400", "--severity-max", "600",
			"--of-type", "i=2041", "--select", "Message", "--select", "Severity", "-v")...)
		if r.err != nil {
			t.Fatalf("monitor event: %v\n%s", r.err, r.stderr)
		}
		wantContains(t, r.stderr, "monitoring events on i=2253 (fields: Message, Severity)")
		evs := monitoredEvents(t, r.stdout)
		if len(evs) != 3 {
			t.Fatalf("--count 3 printed %d events", len(evs))
		}
		for _, ev := range evs {
			msg, _ := ev.Fields["Message"].(map[string]any)
			if ev.NodeID != "i=2253" || ev.Fields["Severity"] != float64(500) || msg["text"] != "high" || len(ev.Fields) != 2 {
				t.Errorf("event %+v passed --severity-min 400", ev)
			}
		}
	})

	t.Run("default fields in a table", func(t *testing.T) {
		r := runWhile(t, emit, monitor("--count", "2", "--severity-max", "200")...)
		if r.err != nil {
			t.Fatalf("monitor event: %v\n%s", r.err, r.stderr)
		}
		lines := strings.Split(strings.TrimSpace(r.stdout), "\n")
		if len(lines) != 2 {
			t.Fatalf("--count 2 printed:\n%s", r.stdout)
		}
		for _, l := range lines {
			wantContains(t, l, "  i=2253  ", "EventType=i=2041", "SourceName=Server", "Severity=100", "Time=")
		}
	})

	t.Run("errors", func(t *testing.T) {
		wantExit(t, 2, "monitor", "event", "-e", s.url)
		wantExit(t, 2, "monitor", "event", "-e", s.url, "-n", "bogus")
		wantExit(t, 2, "monitor", "event", "-e", s.url, "-n", "i=2253", "--of-type", "bogus")
		_, errOut := wantExit(t, 6, "monitor", "event", "-e", s.url, "-n", s.node("Nope"), "--duration", "5s")
		wantContains(t, errOut, "BadNodeIDUnknown")
		wantExit(t, 4, "monitor", "event", "-e", "opc.tcp://"+freeAddr(t), "-n", "i=2253", "--timeout", "2s")
	})
}

// TestE2E_MonitorAlarm subscribes to the LevelAlarm of `opcuactl serve`
// while Demo.Level moves in and out of its limits.
func TestE2E_MonitorAlarm(t *testing.T) {
	s, ep := startDemo(t, "--sim-interval", "20ms")
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	c, err := opcua.NewClient(ep, opcua.InsecureNoSecurity())
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close(context.Background()) }()
	level := ua.NewStringNodeID(s.info.DemoNamespaceIndex, serve.DemoLevel)
	high := false
	toggle := func() {
		high = !high
		v := 50.0
		if high {
			v = 90
		}
		_, _ = c.WriteNodeValue(ctx, level, v)
	}
	demo := s.node(serve.DemoFolder)

	r := runWhile(t, toggle, "monitor", "alarm", "-e", ep, "-n", demo, "--count", "2", "--jsonl", "--duration", "20s")
	if r.err != nil {
		t.Fatalf("monitor alarm: %v\n%s", r.err, r.stderr)
	}
	evs := monitoredEvents(t, r.stdout)
	if len(evs) != 2 {
		t.Fatalf("--count 2 printed %d events", len(evs))
	}
	for _, ev := range evs {
		active, _ := ev.Fields["ActiveState"].(map[string]any)
		if ev.NodeID != demo || ev.Fields["EventType"] != "i=9482" || ev.Fields["SourceName"] != "Demo" ||
			ev.Fields["Severity"] != float64(700) || ev.Fields["Retain"] != true ||
			(active["text"] != "Active" && active["text"] != "Inactive") {
			t.Errorf("alarm event %+v", ev)
		}
		if _, ok := ev.Fields["ConfirmedState"]; !ok {
			t.Errorf("the default fields miss ConfirmedState: %+v", ev.Fields)
		}
	}

	r = runWhile(t, toggle, "monitor", "alarm", "-e", ep, "-n", demo, "--count", "1", "--select", "SourceName", "--select", "Severity", "--duration", "20s", "-v")
	if r.err != nil {
		t.Fatalf("monitor alarm: %v\n%s", r.err, r.stderr)
	}
	wantContains(t, r.stdout, demo+"  SourceName=Demo  Severity=700")
	wantContains(t, r.stderr, "monitoring alarms on "+demo)

	wantExit(t, 2, "monitor", "alarm", "-e", ep)
	wantExit(t, 2, "monitor", "alarm", "-e", ep, "-n", "bogus")
	_, errOut := wantExit(t, 6, "monitor", "alarm", "-e", ep, "-n", s.node("Demo.Nope"), "--duration", "5s")
	wantContains(t, errOut, "BadNodeIDUnknown")
	wantExit(t, 4, "monitor", "alarm", "-e", "opc.tcp://"+freeAddr(t), "-n", "i=2253", "--timeout", "2s")
}
