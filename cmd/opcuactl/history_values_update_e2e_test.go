// SPDX-License-Identifier: MIT

package opcuactl

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/opcuactl/internal/model"
	"github.com/otfabric/opcuactl/internal/serve"
)

// runValueUpdates runs a history value change and returns its results and
// exit code.
func runValueUpdates(t *testing.T, args ...string) ([]model.HistoryValueUpdate, int) {
	t.Helper()
	out, errOut, err := runCLI(t, append(args, "--format", "json")...)
	var res []model.HistoryValueUpdate
	if jerr := json.Unmarshal([]byte(out), &res); jerr != nil {
		t.Fatalf("opcuactl %s: %v %v\n%s\n%s", strings.Join(args, " "), err, jerr, out, errOut)
	}
	return res, ExitCode(err)
}

func statuses(res []model.HistoryValueUpdate) string {
	s := make([]string, len(res))
	for i, r := range res {
		s[i] = r.Status
	}
	return strings.Join(s, ",")
}

// TestE2E_HistoryUpdateValues changes the history of Demo.Sine of
// `opcuactl serve` in a range before the simulation started.
func TestE2E_HistoryUpdateValues(t *testing.T) {
	s, ep := startDemo(t)
	sine := s.node(serve.DemoSine)
	const t1, t2, t3 = "2020-01-01T00:00:00Z", "2020-01-01T00:01:00Z", "2020-01-01T00:02:00Z"
	update := func(args ...string) []string {
		return append([]string{"history", "update-values", "-e", ep, "-n", sine, "--yes"}, args...)
	}
	del := func(args ...string) []string {
		return append([]string{"history", "delete-values", "-e", ep, "-n", sine, "--yes"}, args...)
	}
	read := func() []model.HistoryValue {
		return runJSON[[]model.HistoryValue](t, "history", "value", "-e", ep, "-n", sine,
			"--start", "2019-12-31T00:00:00Z", "--end", "2020-01-02T00:00:00Z")
	}

	res, code := runValueUpdates(t, update("--sample", t1+"=1.5", "--sample", t2+"=2.5")...)
	if code != 0 || statuses(res) != "Good,Good" || res[0].Time != t1 || res[1].Value != 2.5 {
		t.Fatalf("insert: exit %d %+v", code, res)
	}
	if got := read(); len(got) != 2 || got[0].Value != 1.5 {
		t.Fatalf("history after insert: %+v", got)
	}

	if res, code = runValueUpdates(t, update("--sample", t1+"=1")...); code != exitProtocol || statuses(res) != "BadEntryExists" {
		t.Errorf("insert again: exit %d %+v", code, res)
	}
	if res, code = runValueUpdates(t, update("--mode", "replace", "--sample", t1+"=9", "--sample", t3+"=3")...); code != exitPartial ||
		statuses(res) != "Good,BadNoEntryExists" {
		t.Errorf("replace: exit %d %+v", code, res)
	}

	file := filepath.Join(t.TempDir(), "samples.json")
	if err := os.WriteFile(file, []byte(`[{"time":"`+t3+`","value":"3.5"},{"time":"`+t2+`","value":4}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	if res, code = runValueUpdates(t, update("--mode", "update", "--samples-file", file)...); code != 0 || statuses(res) != "Good,Good" {
		t.Errorf("update from file: exit %d %+v", code, res)
	}
	got := read()
	if len(got) != 3 || got[0].Value != 9.0 || got[1].Value != 4.0 || got[2].Value != 3.5 {
		t.Errorf("history after replace and update: %+v", got)
	}

	if res, code = runValueUpdates(t, update("--sample", "now=1", "--dry-run")...); code != 0 || statuses(res) != "DryRun" {
		t.Errorf("dry run: exit %d %+v", code, res)
	}
	out := mustRun(t, update("--sample", t1+"=1", "--dry-run")...)
	wantContains(t, out, "DryRun", "insert", t1)

	// A variable without HistoryWrite access.
	if res, code = runValueUpdates(t, "history", "update-values", "-e", ep, "-n", s.node(serve.DemoInt32), "--yes", "--sample", t1+"=1", "--type", "int32"); code != exitProtocol ||
		statuses(res) != "BadNotWritable" {
		t.Errorf("no HistoryWrite: exit %d %+v", code, res)
	}

	if res, code = runValueUpdates(t, del("--at", t1, "--at", "2020-01-01T00:00:30Z")...); code != exitPartial || statuses(res) != "Good,BadNoEntryExists" {
		t.Errorf("delete at: exit %d %+v", code, res)
	}
	if res, code = runValueUpdates(t, del("--start", "2019-12-31T00:00:00Z", "--end", "2020-01-02T00:00:00Z", "--modified")...); code != exitProtocol ||
		res[0].Operation != "delete-modified" {
		t.Errorf("delete modified: exit %d %+v", code, res)
	}
	out = mustRun(t, del("--start", "2019-12-31T00:00:00Z", "--end", "2020-01-02T00:00:00Z")...)
	wantContains(t, out, "delete", "2019-12-31T00:00:00Z .. 2020-01-02T00:00:00Z", "Good")
	if got := read(); len(got) != 0 {
		t.Errorf("history after delete: %+v", got)
	}
	wantContains(t, mustRun(t, del("--at", t1, "--dry-run")...), "DryRun")
	if res, code = runValueUpdates(t, del("--start", t1, "--end", t2, "--dry-run")...); code != 0 || res[0].Start != t1 || res[0].End != t2 {
		t.Errorf("delete range dry run: exit %d %+v", code, res)
	}

	if _, _, err := runCLI(t, "history", "update-values", "-e", deadEndpoint, "-n", sine, "--yes", "--sample", t1+"=1"); ExitCode(err) != exitTransport {
		t.Errorf("update, dead endpoint: %v", err)
	}
	if _, _, err := runCLI(t, "history", "delete-values", "-e", deadEndpoint, "-n", sine, "--yes", "--at", t1); ExitCode(err) != exitTransport {
		t.Errorf("delete, dead endpoint: %v", err)
	}
}

// TestHistoryValueUpdateFlags checks the flags before a connection.
func TestHistoryValueUpdateFlags(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		f := filepath.Join(dir, name)
		if err := os.WriteFile(f, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return f
	}
	const ts = "2020-01-01T00:00:00Z"
	for _, c := range []struct {
		name string
		args []string
		want string
	}{
		{"update, no node", []string{"update-values", "--sample", ts + "=1"}, "--node"},
		{"update, bad node", []string{"update-values", "-n", "!", "--sample", ts + "=1"}, "node"},
		{"update, bad mode", []string{"update-values", "-n", "i=1", "--mode", "upsert", "--sample", ts + "=1"}, "--mode"},
		{"update, no samples", []string{"update-values", "-n", "i=1"}, "--sample"},
		{"update, no =", []string{"update-values", "-n", "i=1", "--sample", ts}, "TIME=VALUE"},
		{"update, bad time", []string{"update-values", "-n", "i=1", "--sample", "yesterday=1"}, "RFC 3339"},
		{"update, bad value", []string{"update-values", "-n", "i=1", "--sample", ts + "=x"}, "value"},
		{"update, bad ua-json", []string{"update-values", "-n", "i=1", "--type", "ua-json", "--sample", ts + "={"}, "value"},
		{"update, missing file", []string{"update-values", "-n", "i=1", "--samples-file", filepath.Join(dir, "none")}, "--samples-file"},
		{"update, bad file", []string{"update-values", "-n", "i=1", "--samples-file", write("bad.json", `{"time":1}`)}, "array"},
		{"update, file bad time", []string{"update-values", "-n", "i=1", "--samples-file", write("t.json", `[{"time":"x","value":1}]`)}, "entry 1"},
		{"update, file no value", []string{"update-values", "-n", "i=1", "--samples-file", write("v.json", `[{"time":"now"}]`)}, "no value"},
		{"update, file bad value", []string{"update-values", "-n", "i=1", "--samples-file", write("b.json", `[{"time":"now","value":"x"}]`)}, "entry 1"},
		{"delete, no node", []string{"delete-values", "--at", ts}, "--node"},
		{"delete, bad node", []string{"delete-values", "-n", "!", "--at", ts}, "node"},
		{"delete, nothing", []string{"delete-values", "-n", "i=1"}, "--at"},
		{"delete, both", []string{"delete-values", "-n", "i=1", "--at", ts, "--start", ts}, "one of them"},
		{"delete, modified at", []string{"delete-values", "-n", "i=1", "--at", ts, "--modified"}, "--modified"},
		{"delete, start only", []string{"delete-values", "-n", "i=1", "--start", ts}, "together"},
		{"delete, bad start", []string{"delete-values", "-n", "i=1", "--start", "x", "--end", ts}, "--start"},
		{"delete, bad end", []string{"delete-values", "-n", "i=1", "--start", ts, "--end", "x"}, "--end"},
		{"delete, end before start", []string{"delete-values", "-n", "i=1", "--start", ts, "--end", "2019-01-01T00:00:00Z"}, "after"},
		{"delete, bad at", []string{"delete-values", "-n", "i=1", "--at", "x"}, "--at"},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, _, err := runCLI(t, append([]string{"history", c.args[0], "-e", deadEndpoint, "--yes"}, c.args[1:]...)...)
			code := exitUsage
			if c.name == "update, missing file" {
				code = exitInvalidInput
			}
			if ExitCode(err) != code || err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("exit %d %v, want %d with %q", ExitCode(err), err, code, c.want)
			}
		})
	}
	// now is the time of the run.
	before := time.Now().UTC()
	list, err := parseSamples([]string{"now=1"}, "", "double", before)
	if err != nil || len(list) != 1 || !list[0].time.Equal(before) {
		t.Errorf("now: %v %v", list, err)
	}
}

// TestParseSamplesUAJSONAndStdin reads a ua-json sample and samples from
// stdin.
func TestParseSamplesUAJSONAndStdin(t *testing.T) {
	now := time.Now().UTC()
	list, err := parseSamples([]string{`now={"UaType":6,"Value":7}`}, "", "ua-json", now)
	if err != nil || len(list) != 1 || list[0].value != int32(7) {
		t.Fatalf("ua-json: %+v %v", list, err)
	}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = stdin })
	go func() {
		_, _ = w.WriteString(`[{"time":"2020-01-01T00:00:00Z","value":{"UaType":11,"Value":1.25}}]`)
		_ = w.Close()
	}()
	list, err = parseSamples(nil, "-", "ua-json", now)
	if err != nil || len(list) != 1 || list[0].value != 1.25 || list[0].time.Year() != 2020 {
		t.Errorf("stdin: %+v %v", list, err)
	}
}
