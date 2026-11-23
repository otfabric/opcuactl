// SPDX-License-Identifier: MIT

//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fxServer builds go-opcua's examples/uafx/server from the go-opcua module
// opcuactl pins, runs it as a process of its own on a free port and returns
// its endpoint: the published API across the module boundary, not an
// in-process fixture.
func fxServer(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "uafx-server")
	build := exec.Command("go", "build", "-o", bin, "github.com/otfabric/go-opcua/examples/uafx/server")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build examples/uafx/server: %v\n%s", err, out)
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()

	srv := exec.Command(bin, "-host", "127.0.0.1", "-port", fmt.Sprint(port), "-interval", "1h")
	var log strings.Builder
	srv.Stderr = &log
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = srv.Process.Kill()
		_ = srv.Wait()
	})
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	for deadline := time.Now().Add(30 * time.Second); ; {
		if c, err := net.DialTimeout("tcp", addr, time.Second); err == nil {
			_ = c.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the UAFX server does not listen on %s:\n%s", addr, log.String())
		}
		time.Sleep(100 * time.Millisecond)
	}
	return "opc.tcp://" + addr
}

func TestFxInspectSeparateServer(t *testing.T) {
	url := fxServer(t)
	conn := []string{"-e", url, "--insecure-no-security", "--timeout", "30s"}

	output, code := run(t, append([]string{"fx", "inspect"}, conn...)...)
	if code != 0 {
		t.Fatalf("exit code %d, want 0\n%s", code, output)
	}
	for _, want := range []string{"UAFX server: yes", "AutomationComponent", "asset ", "functional entity ", "endpoint ", "ConnectionManager "} {
		if !strings.Contains(output, want) {
			t.Errorf("output does not contain %q:\n%s", want, output)
		}
	}

	output, code = run(t, append([]string{"fx", "inspect", "--check", "--format", "json"}, conn...)...)
	if code != 0 {
		t.Fatalf("--check: exit code %d, want 0\n%s", code, output)
	}
	var r struct {
		IsFxServer           bool              `json:"isFxServer"`
		AutomationComponents []json.RawMessage `json:"automationComponents"`
		Findings             []json.RawMessage `json:"findings"`
		Truncated            bool              `json:"truncated"`
	}
	if err := json.Unmarshal([]byte(output), &r); err != nil {
		t.Fatalf("%v\n%s", err, output)
	}
	if !r.IsFxServer || len(r.AutomationComponents) != 1 || len(r.Findings) != 0 || r.Truncated {
		t.Errorf("report: %s", output)
	}

	// A limit that cuts the report short: exit 7, never a complete report.
	output, code = run(t, append([]string{"fx", "inspect", "--check", "--max-nodes", "1", "--format", "json"}, conn...)...)
	if code != 7 || !strings.Contains(output, `"truncated":true`) {
		t.Errorf("--max-nodes 1: exit code %d, want 7\n%s", code, output)
	}

	// opcuactl's own test server has no UAFX model: --check exits 1.
	output, code = run(t, "fx", "inspect", "--check", "-e", serve(t), "--insecure-no-security")
	if code != 1 || !strings.Contains(output, "not a UAFX server") {
		t.Errorf("opcuactl serve: exit code %d, want 1\n%s", code, output)
	}
}
