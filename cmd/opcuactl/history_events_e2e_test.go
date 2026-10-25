// SPDX-License-Identifier: MIT

package opcuactl

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/opcuactl/internal/model"
	"github.com/otfabric/opcuactl/internal/serve"
	"gopkg.in/yaml.v3"
)

// historyEvents runs history event with --format json and decodes it.
func historyEvents(t *testing.T, args ...string) []model.HistoryEventRecord {
	t.Helper()
	out := mustRun(t, append([]string{"history", "event", "-f", "json"}, args...)...)
	var recs []model.HistoryEventRecord
	if err := json.Unmarshal([]byte(out), &recs); err != nil {
		t.Fatalf("history event output: %v\n%s", err, out)
	}
	return recs
}

// historyUpdates decodes the JSON output of update-events / delete-events.
func historyUpdates(t *testing.T, out string) []model.HistoryEventUpdate {
	t.Helper()
	var res []model.HistoryEventUpdate
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("history update output: %v\n%s", err, out)
	}
	return res
}

func eventIDs(recs []model.HistoryEventRecord) []string {
	out := make([]string, len(recs))
	for i, r := range recs {
		out[i], _ = r.Fields["EventId"].(string)
	}
	return out
}

// wantExit checks the exit code of a CLI run.
func wantExit(t *testing.T, code int, args ...string) (stdout, stderr string) {
	t.Helper()
	out, errOut, err := runCLI(t, args...)
	if got := ExitCode(err); got != code {
		t.Fatalf("opcuactl %s: exit %d (%v), want %d\nstdout: %s\nstderr: %s", strings.Join(args, " "), got, err, code, out, errOut)
	}
	return out, errOut
}

// TestE2E_HistoryEvents reads the event history of the Demo object of
// `opcuactl serve`: the start event and the LevelAlarm transitions, with
// filters, paging, both directions and the output formats.
func TestE2E_HistoryEvents(t *testing.T) {
	s := startServe(t, "--sim-interval", "20ms", "--log-level", "off")
	ep := s.url(t, ua.TransportProfileUATCP)
	demo := s.node(serve.DemoFolder)
	from, to := "2000-01-01T00:00:00Z", "2100-01-01T00:00:00Z"
	all := func(extra ...string) []model.HistoryEventRecord {
		return historyEvents(t, append([]string{"-e", ep, "-n", demo, "--start", from, "--end", to}, extra...)...)
	}

	// High, HighHigh and back: alarm events in the history of Demo.
	alarms := func() int {
		return len(all("--of-type", "ExclusiveLevelAlarmType", "--select", "EventId", "--select", "ActiveState/Id"))
	}
	before := alarms()
	for _, v := range []string{"90", "97", "50"} {
		n := alarms()
		mustRun(t, "write", "value", "-e", ep, "-n", s.node(serve.DemoLevel), "--value", v, "--type", "double", "--yes")
		deadline := time.Now().Add(10 * time.Second)
		for alarms() == n {
			if time.Now().After(deadline) {
				t.Fatalf("no alarm event after Level = %s", v)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	if got := alarms(); got < before+3 {
		t.Fatalf("%d alarm events, want at least %d", got, before+3)
	}

	t.Run("default fields", func(t *testing.T) {
		recs := all()
		if len(recs) < 4 {
			t.Fatalf("%d events: %+v", len(recs), recs)
		}
		first := recs[0].Fields
		for _, f := range historyEventFields {
			if _, ok := first[f]; !ok {
				t.Errorf("field %s missing in %v", f, first)
			}
		}
		if first["Message"] != "opcuactl serve started" || first["Severity"] != float64(serve.DemoStartedSeverity) || first["EventType"] != "i=2041" || first["SourceName"] != "Demo" {
			t.Errorf("start event %v", first)
		}
		var last time.Time
		for _, r := range recs {
			if r.NodeID != demo {
				t.Errorf("nodeId %s", r.NodeID)
			}
			tm, err := time.Parse(time.RFC3339Nano, r.Fields["Time"].(string))
			if err != nil || tm.Before(last) {
				t.Errorf("Time %v after %v: %v", r.Fields["Time"], last, err)
			}
			last = tm
		}
	})

	t.Run("filters", func(t *testing.T) {
		recs := all("--of-type", "ExclusiveLevelAlarmType", "--select", "EventType", "--select", "ConditionId", "--select", "ActiveState/Id", "--select", "0:Severity")
		active := 0
		for _, r := range recs {
			if r.Fields["EventType"] != "i=9482" || r.Fields["ConditionId"] != s.node(serve.DemoAlarm) {
				t.Errorf("of-type event %v", r.Fields)
			}
			if r.Fields["ActiveState/Id"] == true {
				active++
			}
		}
		if active < 2 {
			t.Errorf("%d active alarm events in %v", active, recs)
		}
		for _, r := range all("--severity-min", "500") {
			if r.Fields["Severity"].(float64) < 500 {
				t.Errorf("severity-min 500: %v", r.Fields)
			}
		}
		low := all("--severity-max", "100", "--of-type", "i=2041")
		if len(low) != 1 || low[0].Fields["Message"] != "opcuactl serve started" {
			t.Errorf("severity-max 100: %v", low)
		}
		if got := all("--of-type", "i=2041", "--severity-min", "500", "--severity-max", "600"); len(got) != 0 {
			t.Errorf("severity 500..600: %v", got)
		}
	})

	t.Run("paging", func(t *testing.T) {
		want := eventIDs(all())
		out, errOut := wantExit(t, 0, "history", "event", "-e", ep, "-n", demo, "--start", from, "--end", to, "--num-values", "1", "-f", "json", "-v")
		var recs []model.HistoryEventRecord
		if err := json.Unmarshal([]byte(out), &recs); err != nil {
			t.Fatal(err)
		}
		if got := eventIDs(recs); !slices.Equal(got, want[:len(got)]) || len(got) < len(want) {
			t.Errorf("paged %v, want %v", got, want)
		}
		wantContains(t, errOut, "page(s)")
		if strings.Contains(errOut, "released") {
			t.Errorf("released a continuation point of a complete read:\n%s", errOut)
		}

		_, errOut = wantExit(t, 0, "history", "event", "-e", ep, "-n", demo, "--start", from, "--end", to, "--num-values", "1", "--max-events", "2", "-v")
		wantContains(t, errOut, "returned 2 historical event(s)", "in 2 page(s)", "released the continuation point")
		got := eventIDs(historyEvents(t, "-e", ep, "-n", demo, "--start", from, "--end", to, "--num-values", "2", "--max-events", "3"))
		if !slices.Equal(got, want[:3]) {
			t.Errorf("max-events 3 in pages of 2: %v, want %v", got, want[:3])
		}
		got = eventIDs(historyEvents(t, "-e", ep, "-n", demo, "--start", from, "--end", to, "--num-values", "2", "--continuation", "single"))
		if !slices.Equal(got, want[:2]) {
			t.Errorf("single page: %v, want %v", got, want[:2])
		}
		// Only --start with a limit reads forward from it.
		got = eventIDs(historyEvents(t, "-e", ep, "-n", demo, "--start", from, "--max-events", "2"))
		if !slices.Equal(got, want[:2]) {
			t.Errorf("start + max-events: %v, want %v", got, want[:2])
		}
	})

	t.Run("backward", func(t *testing.T) {
		want := eventIDs(all())
		slices.Reverse(want)
		got := eventIDs(historyEvents(t, "-e", ep, "-n", demo, "--start", to, "--end", from, "--num-values", "1"))
		if !slices.Equal(got, want[:len(got)]) || len(got) < len(want) {
			t.Errorf("backward %v, want %v", got, want)
		}
		got = eventIDs(historyEvents(t, "-e", ep, "-n", demo, "--end", to, "--max-events", "1"))
		if len(got) != 1 || got[0] != want[0] {
			t.Errorf("end + max-events 1: %v, want %v", got, want[0])
		}
	})

	t.Run("formats", func(t *testing.T) {
		args := []string{"history", "event", "-e", ep, "-n", demo, "--start", from, "--end", to}
		table := mustRun(t, args...)
		wantContains(t, table, "EVENTID", "EVENTTYPE", "SOURCENAME", "TIME", "MESSAGE", "SEVERITY", "opcuactl serve started")
		var y []model.HistoryEventRecord
		if err := yaml.Unmarshal([]byte(mustRun(t, append(args, "-f", "yaml")...)), &y); err != nil || len(y) < 4 {
			t.Errorf("yaml: %v %v", err, y)
		}
		lines := strings.Split(strings.TrimSpace(mustRun(t, append(args, "-f", "jsonl")...)), "\n")
		var rec model.HistoryEventRecord
		if len(lines) < 4 || json.Unmarshal([]byte(lines[0]), &rec) != nil || rec.Fields["Message"] != "opcuactl serve started" {
			t.Errorf("jsonl: %v", lines)
		}
		// An empty range is Good_NoData: no events, success.
		if got := historyEvents(t, "-e", ep, "-n", demo, "--start", "2000-01-01T00:00:00Z", "--end", "2000-01-02T00:00:00Z"); len(got) != 0 {
			t.Errorf("empty range: %v", got)
		}
	})

	t.Run("exit codes", func(t *testing.T) {
		base := []string{"history", "event", "-e", ep, "--start", from, "--end", to}
		_, errOut := wantExit(t, 3, append(base, "-n", s.node("Nope"))...)
		wantContains(t, errOut, ua.StatusBadNodeIDUnknown.Symbol())
		_, errOut = wantExit(t, 3, append(base, "-n", s.node(serve.DemoLevel))...)
		wantContains(t, errOut, "BadHistoryOperationUnsupported")
		wantExit(t, 1, "history", "event", "-e", ep, "--start", from, "--end", to)
		wantExit(t, 1, append(base, "-n", "ns=1;x=")...)
		wantExit(t, 1, "history", "event", "-e", ep, "-n", demo, "--start", "yesterday", "--end", to)
		wantExit(t, 1, "history", "event", "-e", ep, "-n", demo, "--start", from, "--end", "tomorrow")
		wantExit(t, 1, "history", "event", "-e", ep, "-n", demo, "--start", from)
		wantExit(t, 1, append(base, "-n", demo, "--continuation", "some")...)
		wantExit(t, 1, append(base, "-n", demo, "--select", "AckedState//Id")...)
		wantExit(t, 1, append(base, "-n", demo, "--of-type", "NoSuchType")...)
		wantExit(t, 2, "history", "event", "-e", "opc.tcp://"+freeAddr(t), "-n", demo, "--start", from, "--end", to, "--timeout", "2s")
	})
}

// TestE2E_HistoryEventUpdates inserts, replaces, updates and deletes events
// in the event history of the Demo object of `opcuactl serve`.
func TestE2E_HistoryEventUpdates(t *testing.T) {
	s := startServe(t, "--log-level", "off")
	ep := s.url(t, ua.TransportProfileUATCP)
	demo := s.node(serve.DemoFolder)
	// The events of the test are in 2001, apart from the live ones.
	read := func() []model.HistoryEventRecord {
		return historyEvents(t, "-e", ep, "-n", demo, "--start", "2001-01-01T00:00:00Z", "--end", "2002-01-01T00:00:00Z",
			"--select", "EventId", "--select", "EventType", "--select", "Time", "--select", "Message", "--select", "Severity", "--select", "SourceName")
	}
	update := func(code int, args ...string) []model.HistoryEventUpdate {
		t.Helper()
		out, _ := wantExit(t, code, append([]string{"history", "update-events", "-e", ep, "-n", demo, "-f", "json", "--yes"}, args...)...)
		return historyUpdates(t, out)
	}
	del := func(code int, args ...string) []model.HistoryEventUpdate {
		t.Helper()
		out, _ := wantExit(t, code, append([]string{"history", "delete-events", "-e", ep, "-n", demo, "-f", "json", "--yes"}, args...)...)
		return historyUpdates(t, out)
	}

	res := update(0,
		"--event", `{"EventId":"0x0a01","Time":"2001-03-01T00:00:00Z","Message":"first","Severity":200,"SourceName":"Demo"}`,
		"--event", `[{"Time":"2001-04-01T00:00:00Z","Message":{"Locale":"en","Text":"second"},"Severity":{"UaType":5,"Value":300}},{"EventId":"0x0a03","EventType":"i=2041","Time":"2001-05-01T00:00:00Z","Message":"third","Severity":400}]`)
	if len(res) != 3 || res[1].EventID == "" || res[0].EventID != "CgE=" {
		t.Fatalf("insert %+v", res)
	}
	for i, r := range res {
		if r.Status != ua.StatusGoodEntryInserted.Symbol() || r.Operation != "insert" || r.Index != i+1 || r.NodeID != demo {
			t.Errorf("insert result %+v", r)
		}
	}
	recs := read()
	if len(recs) != 3 || recs[0].Fields["Message"] != "first" || recs[1].Fields["Message"] != "second" || recs[1].Fields["EventId"] != res[1].EventID ||
		recs[1].Fields["Severity"] != float64(300) || recs[1].Fields["EventType"] != "i=2041" {
		t.Fatalf("after insert %+v", recs)
	}

	t.Run("dry run", func(t *testing.T) {
		res := update(0, "--mode", "replace", "--dry-run", "--event", `{"EventId":"0x0a01","Message":"changed"}`)
		if len(res) != 1 || res[0].Status != "DryRun" || res[0].Fields["Message"] != "changed" {
			t.Errorf("dry run %+v", res)
		}
		out, _ := wantExit(t, 0, "history", "delete-events", "-e", ep, "-n", demo, "--event-id", "0x0a01", "--dry-run")
		wantContains(t, out, "DryRun", "CgE=")
		if got := read(); got[0].Fields["Message"] != "first" || len(got) != 3 {
			t.Errorf("dry run changed the history: %+v", got)
		}
		out, _ = wantExit(t, 0, "history", "update-events", "-e", ep, "-n", demo, "--dry-run", "--event", `{"EventId":"0x0a01","Message":"x"}`)
		wantContains(t, out, "FIELDS", "Message=x")
	})

	t.Run("insert existing", func(t *testing.T) {
		res := update(4, "--event", `[{"EventId":"0x0a01","Time":"2001-03-01T00:00:00Z","Message":"again"},{"EventId":"0x0a04","Time":"2001-06-01T00:00:00Z","Message":"fourth"}]`)
		if res[0].Status != ua.StatusBadEntryExists.Symbol() || res[1].Status != ua.StatusGoodEntryInserted.Symbol() {
			t.Errorf("partial insert %+v", res)
		}
		res = update(3, "--event", `{"EventId":"0x0a01","Time":"2001-03-01T00:00:00Z","Message":"again"}`)
		if res[0].StatusCode != uint32(ua.StatusBadEntryExists) {
			t.Errorf("failed insert %+v", res)
		}
	})

	t.Run("replace and update", func(t *testing.T) {
		res := update(0, "--mode", "replace", "--event", `{"EventId":"0x0a01","Message":"first, corrected","Severity":250}`)
		if res[0].Status != ua.StatusGoodEntryReplaced.Symbol() {
			t.Errorf("replace %+v", res)
		}
		res = update(3, "--mode", "replace", "--event", `{"EventId":"0x0bad","Message":"none"}`)
		if res[0].Status != ua.StatusBadNoEntryExists.Symbol() {
			t.Errorf("replace unknown %+v", res)
		}
		res = update(0, "--mode", "update", "--event", `[{"EventId":"0x0a03","Time":"2001-05-01T00:00:00Z","Message":"third, updated"},{"EventId":"0x0a05","Time":"2001-07-01T00:00:00Z","Message":"fifth"}]`)
		if res[0].Status != ua.StatusGoodEntryReplaced.Symbol() || res[1].Status != ua.StatusGoodEntryInserted.Symbol() {
			t.Errorf("update %+v", res)
		}
		recs := read()
		msgs := make([]string, len(recs))
		for i, r := range recs {
			msgs[i], _ = r.Fields["Message"].(string)
		}
		if !slices.Equal(msgs, []string{"first, corrected", "second", "third, updated", "fourth", "fifth"}) {
			t.Errorf("messages %q", msgs)
		}
		// The replaced event keeps its Time, takes the fields given.
		if recs[0].Fields["Time"] != "2001-03-01T00:00:00Z" || recs[0].Fields["Severity"] != float64(250) || recs[0].Fields["SourceName"] != nil {
			t.Errorf("replaced event %v", recs[0].Fields)
		}
	})

	t.Run("events file", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "events.json")
		if err := os.WriteFile(file, []byte(`[{"EventId":"0x0a06","Time":"2001-08-01T00:00:00Z","Message":"from a file","Severity":100}]`), 0o600); err != nil {
			t.Fatal(err)
		}
		res := update(0, "--events-file", file)
		if res[0].EventID != "CgY=" || res[0].Status != ua.StatusGoodEntryInserted.Symbol() {
			t.Errorf("events file %+v", res)
		}
	})

	t.Run("delete", func(t *testing.T) {
		res := del(4, "--event-id", "CgE=", "--event-id", "0x0bad")
		if res[0].Status != ua.StatusOK.Symbol() || res[1].Status != ua.StatusBadNoData.Symbol() || res[1].EventID != "C60=" {
			t.Errorf("partial delete %+v", res)
		}
		res = del(0, "--event-id", "0x0a03", "--event-id", "0x0a04")
		if res[0].Status != ua.StatusOK.Symbol() || res[1].Status != ua.StatusOK.Symbol() {
			t.Errorf("delete %+v", res)
		}
		res = del(3, "--event-id", "0x0a03")
		if res[0].Status != ua.StatusBadNoData.Symbol() {
			t.Errorf("delete again %+v", res)
		}
		if got := eventIDs(read()); len(got) != 3 || got[1] != "CgU=" || got[2] != "CgY=" {
			t.Errorf("after delete %v", got)
		}
		out, _ := wantExit(t, 0, "history", "delete-events", "-e", ep, "-n", demo, "--event-id", "0x0a05", "--yes")
		wantContains(t, out, "NODE ID", "OPERATION", "delete", "CgU=", "Good")
	})

	t.Run("not writable", func(t *testing.T) {
		res := del(3, "--event-id", "0x01", "-n", "i=2253")
		if res[0].Status != ua.StatusBadHistoryOperationUnsupported.Symbol() {
			t.Errorf("delete on the Server object %+v", res)
		}
		res = update(3, "-n", s.node("Nope"), "--event", `{"Message":"x"}`)
		if res[0].Status != ua.StatusBadNodeIDUnknown.Symbol() {
			t.Errorf("update of an unknown node %+v", res)
		}
	})

	t.Run("usage", func(t *testing.T) {
		u := []string{"history", "update-events", "-e", ep, "-n", demo, "--yes"}
		wantExit(t, 1, "history", "update-events", "-e", ep, "--event", `{"Message":"x"}`)
		wantExit(t, 1, "history", "update-events", "-e", ep, "-n", "ns=1;x=", "--event", `{"Message":"x"}`)
		wantExit(t, 1, u...)
		wantExit(t, 1, append(u, "--mode", "upsert", "--event", `{"Message":"x"}`)...)
		wantExit(t, 1, append(u, "--event", `{"Message":`)...)
		wantExit(t, 1, append(u, "--event", `{"Severity":-1}`)...)
		wantExit(t, 1, append(u, "--mode", "replace", "--event", `{"Message":"x"}`)...)
		wantExit(t, 1, append(u, "--events-file", filepath.Join(t.TempDir(), "missing.json"))...)
		bad := filepath.Join(t.TempDir(), "bad.json")
		if err := os.WriteFile(bad, []byte(`[1]`), 0o600); err != nil {
			t.Fatal(err)
		}
		wantExit(t, 1, append(u, "--events-file", bad)...)
		d := []string{"history", "delete-events", "-e", ep, "--yes"}
		wantExit(t, 1, d...)
		wantExit(t, 1, append(d, "-n", "ns=1;x=", "--event-id", "0x01")...)
		wantExit(t, 1, append(d, "-n", demo)...)
		wantExit(t, 1, append(d, "-n", demo, "--event-id", "latest")...)
		wantExit(t, 1, append(d, "-n", demo, "--event-id", "0xzz")...)
		wantExit(t, 2, "history", "delete-events", "-e", "opc.tcp://"+freeAddr(t), "-n", demo, "--event-id", "0x01", "--yes", "--timeout", "2s")
		wantExit(t, 2, "history", "update-events", "-e", "opc.tcp://"+freeAddr(t), "-n", demo, "--event", `{"Message":"x"}`, "--yes", "--timeout", "2s")
	})
}
