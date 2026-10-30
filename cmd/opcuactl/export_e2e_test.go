// SPDX-License-Identifier: MIT

package opcuactl

import (
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/opcuactl/internal/model"
	"github.com/otfabric/opcuactl/internal/serve"
)

// TestE2E_ExportNodes exports the demo address space of `opcuactl serve`.
func TestE2E_ExportNodes(t *testing.T) {
	s, ep := startDemo(t)
	demo := s.node(serve.DemoFolder)

	t.Run("json", func(t *testing.T) {
		nodes := runJSON[[]model.ExportedNode](t, "export", "nodes", "-e", ep, "-n", demo, "--depth", "1")
		byID := map[string]model.ExportedNode{}
		for _, n := range nodes {
			byID[n.NodeID] = n
			if n.Depth > 1 {
				t.Errorf("--depth 1 exported %s at depth %d", n.NodeID, n.Depth)
			}
		}
		if root := byID[demo]; root.Depth != 0 || root.NodeClass != "NodeClassObject" || root.DisplayName != "Demo" {
			t.Errorf("root %+v", root)
		}
		level := byID[s.node(serve.DemoLevel)]
		if level.Depth != 1 || level.DataType != "Double" || level.BrowseName != "1:Level" || level.TypeDefinition != "BaseDataVariableType" {
			t.Errorf("Demo.Level %+v", level)
		}
		if _, ok := byID[s.node(serve.DemoAdd)]; !ok {
			t.Errorf("the method Demo.Add was not exported: %v", nodes)
		}
	})

	t.Run("node class filter", func(t *testing.T) {
		vars := runJSON[[]model.ExportedNode](t, "export", "nodes", "-e", ep, "-n", demo, "--depth", "1", "--node-class", "variable")
		var ids []string
		for _, n := range vars {
			ids = append(ids, n.NodeID)
			if n.NodeClass != "NodeClassVariable" {
				t.Errorf("--node-class variable exported %+v", n)
			}
		}
		for _, v := range demoVariables {
			if !slices.Contains(ids, s.node(v)) {
				t.Errorf("--node-class variable misses %s: %v", v, ids)
			}
		}

		objects := runJSON[[]model.ExportedNode](t, "export", "nodes", "-e", ep, "-n", demo, "--depth", "1", "--node-class", "object")
		if len(objects) != 2 || objects[0].NodeID != demo || objects[1].NodeID != s.node(serve.DemoAlarm) {
			t.Errorf("--node-class object: %+v", objects)
		}
	})

	t.Run("include attrs", func(t *testing.T) {
		nodes := runJSON[[]model.ExportedNode](t, "export", "nodes", "-e", ep, "-n", s.node(serve.DemoInt32Array), "--include-attrs")
		if len(nodes) != 1 || nodes[0].ValueRank != "1" || nodes[0].WriteMask == "" {
			t.Errorf("--include-attrs: %+v", nodes)
		}
	})

	t.Run("csv to file", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "nodes.csv")
		out, errOut := wantExit(t, 0, "export", "nodes", "-e", ep, "-n", demo, "--depth", "1", "--format", "csv", "--output", file, "-v")
		if out != "" {
			t.Errorf("--output wrote to stdout too:\n%s", out)
		}
		wantContains(t, errOut, "exported ")
		f, err := os.Open(file)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = f.Close() }()
		rows, err := csv.NewReader(f).ReadAll()
		if err != nil {
			t.Fatalf("not CSV: %v", err)
		}
		if strings.Join(rows[0], ",") != "NODE ID,NODE CLASS,DISPLAY NAME,DATA TYPE,ACCESS,DEPTH" {
			t.Errorf("header %v", rows[0])
		}
		if !slices.ContainsFunc(rows, func(r []string) bool { return r[0] == s.node(serve.DemoCounter) && r[3] == "UInt32" && r[5] == "1" }) {
			t.Errorf("no row of Demo.Counter: %v", rows)
		}
	})

	t.Run("errors", func(t *testing.T) {
		wantExit(t, 2, "export", "nodes", "-e", ep, "-n", "bogus")
		wantExit(t, 2, "export", "nodes", "-e", ep, "--node-class", "bogus")
		wantExit(t, 2, "export", "nodes", "-e", ep, "-n", demo, "--depth", "1", "--output", filepath.Join(t.TempDir(), "missing", "nodes.json"))
		wantExit(t, 4, "export", "nodes", "-e", "opc.tcp://"+freeAddr(t), "--timeout", "2s")
	})
}

// TestE2E_ExportValues exports current and historical values of `opcuactl
// serve`.
func TestE2E_ExportValues(t *testing.T) {
	s, ep := startDemo(t, "--sim-interval", "20ms")
	sine, plain := s.node(serve.DemoSine), s.node(serve.DemoInt32)

	t.Run("snapshot", func(t *testing.T) {
		values := runJSON[[]model.ExportedValue](t, "export", "values", "-e", ep, "-n", plain, "-n", s.node(serve.DemoString))
		if len(values) != 2 || values[0].Value != float64(42) || values[0].Type != "TypeIDInt32" || values[1].Value != "hello from opcuactl" || values[1].Status != "Good" {
			t.Errorf("values %+v", values)
		}

		// A subtree: every variable under Demo, nothing else.
		values = runJSON[[]model.ExportedValue](t, "export", "values", "-e", ep, "--start-node", s.node(serve.DemoFolder), "--depth", "1")
		var ids []string
		for _, v := range values {
			ids = append(ids, v.NodeID)
		}
		for _, v := range demoVariables {
			if !slices.Contains(ids, s.node(v)) {
				t.Errorf("--start-node Demo misses %s: %v", v, ids)
			}
		}
		if slices.Contains(ids, s.node(serve.DemoAdd)) || slices.Contains(ids, s.node(serve.DemoFolder)) {
			t.Errorf("--start-node exported more than variables: %v", ids)
		}

		// --nodes-file skips comments and blank lines.
		file := filepath.Join(t.TempDir(), "nodes.txt")
		if err := os.WriteFile(file, []byte("# demo\n\n"+plain+"\n  "+s.node(serve.DemoDouble)+"  \n"), 0o600); err != nil {
			t.Fatal(err)
		}
		out := mustRun(t, "export", "values", "-e", ep, "--nodes-file", file, "--format", "jsonl")
		lines := strings.Split(strings.TrimSpace(out), "\n")
		if len(lines) != 2 || !strings.Contains(lines[0], `"NODE ID":"`+plain+`"`) || !strings.Contains(lines[1], `"VALUE":"3.14"`) {
			t.Errorf("--nodes-file --format jsonl:\n%s", out)
		}

		outFile := filepath.Join(t.TempDir(), "values.json")
		mustRun(t, "export", "values", "-e", ep, "-n", plain, "--format", "json", "--output", outFile)
		data, err := os.ReadFile(outFile)
		if err != nil {
			t.Fatal(err)
		}
		var fromFile []model.ExportedValue
		if err := json.Unmarshal(data, &fromFile); err != nil || len(fromFile) != 1 || fromFile[0].NodeID != plain {
			t.Errorf("--output: %v %s", err, data)
		}

		_, errOut := wantExit(t, 0, "export", "values", "-e", ep, "-n", plain, "-v")
		wantContains(t, errOut, "exported 1 value(s)")
	})

	from, to := waitSineHistory(t, s, ep, 3)
	exportHist := func(args ...string) []string {
		return append([]string{"export", "values", "-e", ep, "--history"}, args...)
	}

	t.Run("history", func(t *testing.T) {
		values := runJSON[[]model.ExportedValue](t, exportHist("-n", sine, "--start", from, "--end", to)...)
		if len(values) < 3 {
			t.Fatalf("%d values", len(values))
		}
		for _, v := range values {
			if v.NodeID != sine || v.Type != "TypeIDDouble" || v.SourceTimestamp == "" {
				t.Errorf("value %+v", v)
			}
		}

		start := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
		end := time.Now().Add(time.Minute).UTC().Format(time.RFC3339)
		processed := runJSON[[]model.ExportedValue](t, exportHist("-n", sine, "--mode", "processed", "--start", start, "--end", end, "--interval", "1m", "--aggregate", "max")...)
		if len(processed) != 2 {
			t.Errorf("--mode processed: %+v", processed)
		}
		at := runJSON[[]model.ExportedValue](t, exportHist("-n", sine, "--mode", "attime", "--at", time.Now().UTC().Format(time.RFC3339))...)
		if len(at) != 1 {
			t.Errorf("--mode attime: %+v", at)
		}
		if modified := runJSON[[]model.ExportedValue](t, exportHist("-n", sine, "--mode", "modified", "--start", from, "--end", to)...); len(modified) != 0 {
			t.Errorf("--mode modified: %+v", modified)
		}

		out, errOut := wantExit(t, 0, exportHist("-n", sine, "--start", from, "--end", to, "--format", "csv", "-v")...)
		wantContains(t, out, "NODE ID,VALUE,TYPE,STATUS,SOURCE TIMESTAMP\n", sine+",")
		wantContains(t, errOut, "historical value(s) across 1 node(s)")
	})

	t.Run("failed nodes", func(t *testing.T) {
		at := time.Now().UTC().Format(time.RFC3339)
		for _, mode := range [][]string{
			{"--start", from, "--end", to},
			{"--mode", "modified", "--start", from, "--end", to},
			{"--mode", "processed", "--start", from, "--end", to, "--interval", "1h"},
			{"--mode", "attime", "--at", at},
		} {
			// Demo.Int32 has no history: with Demo.Sine a partial success
			// (exit 4) that still exports Sine, alone a failure (exit 3).
			out, errOut := wantExit(t, 7, exportHist(append([]string{"-n", sine, "-n", plain, "--format", "json"}, mode...)...)...)
			wantContains(t, errOut, plain, "HistoryOperationUnsupported", "1 of 2 node(s) failed")
			var values []model.ExportedValue
			if err := json.Unmarshal([]byte(out), &values); err != nil {
				t.Fatalf("%v: %v\n%s", mode, err, out)
			}
			for _, v := range values {
				if v.NodeID != sine {
					t.Errorf("%v: exported %+v", mode, v)
				}
			}
			wantExit(t, 6, exportHist(append([]string{"-n", plain}, mode...)...)...)
		}
	})

	t.Run("usage errors", func(t *testing.T) {
		for _, args := range [][]string{
			{"export", "values", "-e", ep},
			{"export", "values", "-e", ep, "--nodes-file", filepath.Join(t.TempDir(), "missing.txt")},
			{"export", "values", "-e", ep, "-n", "bogus"},
			{"export", "values", "-e", ep, "--start-node", "bogus"},
			{"export", "values", "-e", ep, "--start-node", "i=88", "--depth", "1"}, // no variables
			{"export", "values", "-e", ep, "-n", plain, "--output", filepath.Join(t.TempDir(), "missing", "out.json")},
			exportHist("-n", sine),
			exportHist("-n", sine, "--start", "x", "--end", to),
			exportHist("-n", sine, "--start", from, "--end", "x"),
			exportHist("-n", sine, "--start", to, "--end", from),
			exportHist("-n", sine, "--mode", "attime"),
			exportHist("-n", sine, "--mode", "attime", "--at", "x"),
			exportHist("-n", sine, "--mode", "processed", "--start", from, "--end", to),
			exportHist("-n", sine, "--mode", "processed", "--start", from, "--end", to, "--interval", "1m", "--aggregate", "median"),
			exportHist("-n", sine, "--start", from, "--end", to, "--output", filepath.Join(t.TempDir(), "missing", "out.json")),
		} {
			wantExit(t, 2, args...)
		}
		wantExit(t, 4, "export", "values", "-e", "opc.tcp://"+freeAddr(t), "-n", plain, "--timeout", "2s")
	})
}
