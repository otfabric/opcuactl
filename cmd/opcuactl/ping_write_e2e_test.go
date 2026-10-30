// SPDX-License-Identifier: MIT

package opcuactl

import (
	"encoding/json"
	"net"
	"strings"
	"testing"

	"github.com/otfabric/opcuactl/internal/model"
	"github.com/otfabric/opcuactl/internal/serve"
)

// TestE2E_Ping checks the stages and the exit codes of ping.
func TestE2E_Ping(t *testing.T) {
	_, ep := startDemo(t)

	steps := runJSON[[]model.PingStep](t, "ping", "-e", ep)
	if len(steps) != 3 {
		t.Fatalf("steps %+v", steps)
	}
	for i, stage := range []string{"tcp", "secure", "browse"} {
		if steps[i].Stage != stage || steps[i].Status != "ok" || steps[i].Latency == "" || steps[i].Error != "" {
			t.Errorf("step %d: %+v", i, steps[i])
		}
	}
	out := mustRun(t, "ping", "-e", ep)
	wantContains(t, out, "✓ tcp", "✓ secure", "✓ browse")

	// Nobody listens: the TCP stage fails, exit 2.
	out, _ = wantExit(t, 4, "ping", "-e", "opc.tcp://"+freeAddr(t), "--timeout", "2s")
	wantContains(t, out, "✗ tcp")
	if strings.Contains(out, "secure") {
		t.Errorf("ping went on after the TCP stage failed:\n%s", out)
	}

	// Something that is no OPC UA server: the secure stage fails, exit 2.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	out, _ = wantExit(t, 4, "ping", "-e", "opc.tcp://"+l.Addr().String(), "--timeout", "2s", "--format", "json")
	var failed []model.PingStep
	if err := json.Unmarshal([]byte(out), &failed); err != nil || len(failed) != 2 || failed[0].Status != "ok" || failed[1].Stage != "secure" || failed[1].Status != "fail" || failed[1].Error == "" {
		t.Errorf("ping of a non-OPC UA listener: %v %+v", err, failed)
	}

	wantExit(t, 2, "ping")
}

// TestE2E_WriteValue writes Demo.Level of `opcuactl serve` and checks the
// dry run and the exit codes.
func TestE2E_WriteValue(t *testing.T) {
	s, ep := startDemo(t)
	level := s.node(serve.DemoLevel)

	res := runJSON[model.WriteResult](t, "write", "value", "-e", ep, "-n", level, "--value", "61.5", "--type", "double", "--yes")
	if res.NodeID != level || res.Status != "Good" || res.StatusCode != 0 {
		t.Errorf("write %+v", res)
	}
	if got := strings.TrimSpace(mustRun(t, "get", "value", "-e", ep, "-n", level, "-q")); got != "61.5" {
		t.Errorf("Demo.Level = %s after writing 61.5", got)
	}
	wantContains(t, mustRun(t, "write", "value", "-e", ep, "-n", level, "--value", "62", "--type", "double", "--yes"), "NODE ID", "Good")

	// A dry run reads the node and does not write.
	dry := runJSON[model.DryRunResult](t, "write", "value", "-e", ep, "-n", level, "--value", "99", "--type", "double", "--dry-run")
	if dry.Status != "DryRun" || dry.CurrentValue != float64(62) || dry.NewValue != float64(99) || dry.DisplayName != "Level" ||
		dry.DataType != "Double" || dry.AccessLevel != "readable, writable" || dry.NewType != "double" {
		t.Errorf("dry run %+v", dry)
	}
	out := mustRun(t, "write", "value", "-e", ep, "-n", level, "--value", "99", "--type", "double", "--dry-run")
	wantContains(t, out, "CURRENT VALUE", "62", "99", "DryRun")
	if got := strings.TrimSpace(mustRun(t, "get", "value", "-e", ep, "-n", level, "-q")); got != "62" {
		t.Errorf("--dry-run wrote: Demo.Level = %s", got)
	}
	// Without a server the dry run still validates the value.
	dry = runJSON[model.DryRunResult](t, "write", "value", "-e", "opc.tcp://"+freeAddr(t), "-n", level, "--value", "7", "--type", "int32", "--dry-run", "--timeout", "1s")
	if dry.NewValue != float64(7) || dry.CurrentValue != nil {
		t.Errorf("offline dry run %+v", dry)
	}

	// Demo.Int32 is read-only: the write is printed and exits 3.
	out, errOut := wantExit(t, 6, "write", "value", "-e", ep, "-n", s.node(serve.DemoInt32), "--value", "1", "--type", "int32", "--yes")
	wantContains(t, out, "Bad")
	wantContains(t, errOut, "writing value to "+s.node(serve.DemoInt32))

	for _, args := range [][]string{
		{"--value", "1"},
		{"-n", level},
		{"-n", "bogus", "--value", "1"},
		{"-n", level, "--value", "x", "--type", "double"},
		{"-n", level, "--value", "{", "--type", "ua-json"},
	} {
		wantExit(t, 2, append([]string{"write", "value", "-e", ep, "--yes"}, args...)...)
	}
	wantExit(t, 4, "write", "value", "-e", "opc.tcp://"+freeAddr(t), "-n", level, "--value", "1", "--type", "double", "--yes", "--timeout", "2s")
}
