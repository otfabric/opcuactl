// SPDX-License-Identifier: MIT

//go:build e2e

package e2e

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

// binary is the binary named by OPCUACTL_E2E_BIN (set by make e2e).
func binary(t *testing.T) string {
	t.Helper()
	bin := os.Getenv("OPCUACTL_E2E_BIN")
	if bin == "" {
		t.Fatal("OPCUACTL_E2E_BIN is not set; run make e2e")
	}
	return bin
}

// run executes the binary.
func run(t *testing.T, args ...string) (output string, exitCode int) {
	t.Helper()
	var out bytes.Buffer
	cmd := exec.Command(binary(t), args...)
	cmd.Stdout = &out
	cmd.Stderr = &out
	code := exitStatus(t, cmd.Run(), &out)
	return out.String(), code
}

// exitStatus is the exit status of the process that ended with err.
func exitStatus(t *testing.T, err error, out *bytes.Buffer) int {
	t.Helper()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &exit):
		return exit.ExitCode()
	default:
		t.Fatalf("run: %v\n%s", err, out)
		return -1
	}
}

// started is a running binary: its stdout line by line, stderr collected.
type started struct {
	cmd    *exec.Cmd
	lines  *bufio.Scanner
	stderr bytes.Buffer
}

// start runs the binary with args and stops it when the test ends.
func start(t *testing.T, args ...string) *started {
	t.Helper()
	s := &started{cmd: exec.Command(binary(t), args...)}
	s.cmd.Stderr = &s.stderr
	stdout, err := s.cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	s.lines = bufio.NewScanner(stdout)
	t.Cleanup(func() {
		if s.cmd.ProcessState == nil {
			_ = s.cmd.Process.Kill()
			_, _ = io.Copy(io.Discard, stdout)
			_ = s.cmd.Wait()
		}
	})
	return s
}

// line returns the next line of stdout; the stream may not end before it.
func (s *started) line(t *testing.T) string {
	t.Helper()
	if !s.lines.Scan() {
		t.Fatalf("%v: stdout ended (%v)\nstderr: %s", s.cmd.Args[1:], s.lines.Err(), &s.stderr)
	}
	return s.lines.Text()
}

// wait drains stdout and returns the exit status.
func (s *started) wait(t *testing.T) int {
	t.Helper()
	for s.lines.Scan() {
	}
	return exitStatus(t, s.cmd.Wait(), &s.stderr)
}

// serve starts opcuactl serve on a free loopback port and returns its
// opc.tcp endpoint URL once it listens.
func serve(t *testing.T) string {
	t.Helper()
	s := start(t, "serve", "--listen", "127.0.0.1:0", "--insecure-no-security", "-q")
	for {
		if url := s.line(t); strings.HasPrefix(url, "opc.tcp://") {
			return url
		}
	}
}

func TestVersionPrintsTheBuiltVersion(t *testing.T) {
	output, code := run(t, "version")
	if code != 0 {
		t.Fatalf("exit code %d, want 0\n%s", code, output)
	}
	// make e2e sets it to the tag of HEAD, empty off a tag: then the binary is a dev build.
	want, ok := os.LookupEnv("OPCUACTL_E2E_VERSION")
	if !ok {
		t.Fatal("OPCUACTL_E2E_VERSION is not set; run make e2e")
	}
	if want == "" {
		want = "dev"
	}
	want = strings.TrimPrefix(want, "v")
	if !strings.HasPrefix(output, "opcuactl "+want+" (") || strings.Count(output, "\n") != 1 {
		t.Fatalf("output is not one line naming opcuactl %s:\n%s", want, output)
	}

	output, code = run(t, "version", "--json")
	if code != 0 {
		t.Fatalf("version --json: exit code %d, want 0\n%s", code, output)
	}
	var info map[string]any
	if err := json.Unmarshal([]byte(output), &info); err != nil || info["name"] != "opcuactl" || info["version"] != want {
		t.Fatalf("version --json is not the build information of opcuactl %s (%v):\n%s", want, err, output)
	}
}

// An unknown command is a usage error: exit code 2 in docs/exitcodes.md.
func TestUnknownCommandIsAUsageError(t *testing.T) {
	output, code := run(t, "nosuchcommand")
	if code != 2 {
		t.Fatalf("exit code %d, want 2\n%s", code, output)
	}
	if !strings.Contains(output, `unknown command "nosuchcommand"`) {
		t.Fatalf("output does not name the unknown command:\n%s", output)
	}
}

// A server that cannot be reached is a transport failure: exit code 4.
func TestUnreachableServerIsATransportError(t *testing.T) {
	output, code := run(t, "get", "value", "-e", "opc.tcp://127.0.0.1:1", "-n", "i=2258", "--timeout", "10s")
	if code != 4 {
		t.Fatalf("exit code %d, want 4\n%s", code, output)
	}
}

// A Bad status of the service is a protocol failure: exit code 6.
func TestBadStatusIsAProtocolError(t *testing.T) {
	url := serve(t)
	output, code := run(t, "write", "value", "-e", url, "--insecure-no-security", "-n", "ns=1;s=NoSuchNode",
		"--value", "1", "--type", "int32", "--yes", "--timeout", "10s")
	if code != 6 {
		t.Fatalf("exit code %d, want 6\n%s", code, output)
	}
	if !strings.Contains(output, "BadNodeIDUnknown") {
		t.Fatalf("output does not name the status:\n%s", output)
	}
}

// SIGINT before a result ends the command with exit code 130.
func TestInterruptBeforeAResult(t *testing.T) {
	// A server that accepts the connection and does not answer.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	s := start(t, "get", "value", "-e", "opc.tcp://"+l.Addr().String(), "--insecure-no-security", "-n", "i=2258", "--timeout", "60s")
	conn, err := l.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	// The client sends its Hello once it connected.
	if _, err := io.ReadFull(conn, make([]byte, 8)); err != nil {
		t.Fatal(err)
	}
	if err := s.cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	if code := s.wait(t); code != 130 {
		t.Fatalf("exit code %d, want 130\nstderr: %s", code, &s.stderr)
	}
}

// SIGINT ends a monitor stream that is running: exit code 0 (docs/exitcodes.md).
func TestInterruptEndsAStream(t *testing.T) {
	url := serve(t)
	s := start(t, "monitor", "value", "-e", url, "--insecure-no-security", "-n", "ns=1;s=Demo.Counter", "--duration", "60s")
	if line := s.line(t); !strings.Contains(line, "Demo.Counter") {
		t.Fatalf("first line %q does not name the node", line)
	}
	started := time.Now()
	if err := s.cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	if code := s.wait(t); code != 0 {
		t.Fatalf("exit code %d, want 0\nstderr: %s", code, &s.stderr)
	}
	if d := time.Since(started); d >= 60*time.Second {
		t.Fatalf("the stream ran for %v after SIGINT", d)
	}
}
