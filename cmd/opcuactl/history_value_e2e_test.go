// SPDX-License-Identifier: MIT

package opcuactl

import (
	"testing"
	"time"

	"github.com/otfabric/opcuactl/internal/model"
	"github.com/otfabric/opcuactl/internal/serve"
)

// waitSineHistory waits until the historian of serve kept n samples of
// Demo.Sine and returns a range around them.
func waitSineHistory(t *testing.T, s *servedServer, ep string, n int) (from, to string) {
	t.Helper()
	from = time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	to = time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	deadline := time.Now().Add(20 * time.Second)
	for {
		values := runJSON[[]model.HistoryValue](t, "history", "value", "-e", ep, "-n", s.node(serve.DemoSine), "--start", from, "--end", to)
		if len(values) >= n {
			return from, to
		}
		if time.Now().After(deadline) {
			t.Fatalf("Demo.Sine has %d samples in its history, want %d", len(values), n)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestE2E_HistoryValue reads the history of Demo.Sine of `opcuactl serve`
// in every mode, and the errors of a node without history.
func TestE2E_HistoryValue(t *testing.T) {
	s, ep := startDemo(t, "--sim-interval", "20ms")
	sine, plain := s.node(serve.DemoSine), s.node(serve.DemoInt32)
	from, to := waitSineHistory(t, s, ep, 5)
	hist := func(args ...string) []string { return append([]string{"history", "value", "-e", ep}, args...) }

	t.Run("raw", func(t *testing.T) {
		values := runJSON[[]model.HistoryValue](t, hist("-n", sine, "--start", from, "--end", to)...)
		if len(values) < 5 {
			t.Fatalf("%d values", len(values))
		}
		prev := ""
		for _, v := range values {
			if v.NodeID != sine || v.Status != "Good" || v.Type != "TypeIDDouble" {
				t.Errorf("value %+v", v)
			}
			if f, ok := v.Value.(float64); !ok || f < -1 || f > 1 {
				t.Errorf("Demo.Sine value %v out of [-1, 1]", v.Value)
			}
			if v.SourceTimestamp < prev {
				t.Errorf("not in time order: %s after %s", v.SourceTimestamp, prev)
			}
			prev = v.SourceTimestamp
		}

		for _, args := range [][]string{{"--max-values", "3"}, {"--max-values", "3", "--continuation", "single"}} {
			page := runJSON[[]model.HistoryValue](t, hist(append([]string{"-n", sine, "--start", from, "--end", to}, args...)...)...)
			if len(page) != 3 || page[0].SourceTimestamp != values[0].SourceTimestamp {
				t.Errorf("%v: %d values from %v, want 3 from %s", args, len(page), page, values[0].SourceTimestamp)
			}
		}

		out, errOut := wantExit(t, 0, hist("-n", sine, "--start", from, "--end", to, "--max-values", "2", "-v")...)
		wantContains(t, out, "SOURCE TIMESTAMP", "TypeIDDouble", "Good")
		wantContains(t, errOut, "returned 2 historical value(s) for "+sine+" (mode=raw)")
	})

	t.Run("processed", func(t *testing.T) {
		start := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
		end := time.Now().Add(time.Minute).UTC().Format(time.RFC3339)
		for _, agg := range []string{"average", "min", "max", "count"} {
			values := runJSON[[]model.HistoryValue](t, hist("-n", sine, "--mode", "PROCESSED", "--start", start, "--end", end, "--interval", "30s", "--aggregate", agg)...)
			if len(values) != 4 {
				t.Errorf("--aggregate %s: %d intervals of 30s in 2m, want 4: %+v", agg, len(values), values)
			}
		}
	})

	t.Run("attime and modified", func(t *testing.T) {
		values := runJSON[[]model.HistoryValue](t, hist("-n", sine, "--start", from, "--end", to)...)
		last, err := time.Parse(time.RFC3339Nano, values[len(values)-1].SourceTimestamp)
		if err != nil {
			t.Fatal(err)
		}
		at := last.Add(time.Second).UTC().Format(time.RFC3339)
		got := runJSON[[]model.HistoryValue](t, hist("-n", sine, "--mode", "attime", "--at", at, "--at", at)...)
		if len(got) != 2 || got[0].Value == nil {
			t.Errorf("--mode attime: %+v", got)
		}

		// Nothing was ever modified: an empty, Good result.
		if got := runJSON[[]model.HistoryValue](t, hist("-n", sine, "--mode", "modified", "--start", from, "--end", to)...); len(got) != 0 {
			t.Errorf("--mode modified: %+v", got)
		}
	})

	t.Run("node without history exits 3", func(t *testing.T) {
		at := time.Now().UTC().Format(time.RFC3339)
		for _, mode := range [][]string{
			{"--start", from, "--end", to},
			{"--start", from, "--end", to, "--max-values", "1"},
			{"--mode", "modified", "--start", from, "--end", to},
			{"--mode", "processed", "--start", from, "--end", to, "--interval", "1h"},
			{"--mode", "attime", "--at", at},
		} {
			_, errOut := wantExit(t, 3, hist(append([]string{"-n", plain}, mode...)...)...)
			wantContains(t, errOut, "HistoryOperationUnsupported")
		}
	})

	t.Run("usage errors", func(t *testing.T) {
		for _, args := range [][]string{
			{},
			{"-n", "bogus", "--start", from, "--end", to},
			{"-n", sine},
			{"-n", sine, "--start", "yesterday", "--end", to},
			{"-n", sine, "--start", from, "--end", "tomorrow"},
			{"-n", sine, "--start", to, "--end", from},
			{"-n", sine, "--mode", "attime"},
			{"-n", sine, "--mode", "attime", "--at", "noon"},
			{"-n", sine, "--mode", "processed", "--start", from, "--end", to},
			{"-n", sine, "--mode", "processed", "--start", from, "--end", to, "--interval", "1m", "--aggregate", "median"},
		} {
			wantExit(t, 1, hist(args...)...)
		}
		wantExit(t, 2, "history", "value", "-e", "opc.tcp://"+freeAddr(t), "-n", sine, "--start", from, "--end", to, "--timeout", "2s")
	})
}
