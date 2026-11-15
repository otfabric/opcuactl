// SPDX-License-Identifier: MIT

package opcuactl

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// A test that runs in parallel cannot run the CLI in this process, where the
// commands share their flags and os.Stdout with every other test. It runs
// them in a worker instead: the test binary started again, which runs one
// command after the other as runCLI does and returns what each wrote, its
// error and its exit code. A child process per command (startCLI) would do
// as well, but a process built with -race that exits with 0 sleeps a second
// first (GORACE atexit_sleep_ms): a worker does that once, at the end of the
// test.

// cliWorkerEnv makes the test binary a worker. It reads the commands from
// file descriptor 3 and writes their results to file descriptor 4, so that
// os.Stdout and os.Stderr stay what the commands write to.
const cliWorkerEnv = "OPCUACTL_TEST_WORKER"

type cliRequest struct {
	Args []string `json:"args"`
}

type cliResponse struct {
	Stdout string  `json:"stdout"`
	Stderr string  `json:"stderr"`
	Err    *string `json:"err,omitempty"` // the message of the error Execute returned
	Code   int     `json:"code"`          // ExitCode of that error
}

// cliWorkerMain runs the commands of the test, until it closes its end.
func cliWorkerMain() {
	_ = os.Setenv("OPCUACTL_INSECURE_NO_SECURITY", "true")
	requests := json.NewDecoder(os.NewFile(3, "requests"))
	responses := json.NewEncoder(os.NewFile(4, "responses"))
	for {
		var req cliRequest
		if err := requests.Decode(&req); err != nil {
			if errors.Is(err, io.EOF) {
				os.Exit(0)
			}
			fmt.Fprintln(os.Stderr, "opcuactl worker:", err)
			os.Exit(1)
		}
		stdout, stderr, err, pipeErr := executeCLI(req.Args)
		if pipeErr != nil {
			fmt.Fprintln(os.Stderr, "opcuactl worker: pipe:", pipeErr)
			os.Exit(1)
		}
		resp := cliResponse{Stdout: stdout, Stderr: stderr, Code: ExitCode(err)}
		if err != nil {
			msg := err.Error()
			resp.Err = &msg
		}
		if err := responses.Encode(resp); err != nil {
			fmt.Fprintln(os.Stderr, "opcuactl worker:", err)
			os.Exit(1)
		}
	}
}

// cliWorker runs the commands of one test in a worker process, one at a
// time.
type cliWorker struct {
	mu        sync.Mutex
	cmd       *exec.Cmd
	requests  *json.Encoder
	responses *json.Decoder
	// output is what the worker wrote outside the commands: a report of
	// the race detector, a stack trace.
	output *syncBuffer
	done   chan struct{}
	err    error
	killed bool // by the test, after a command did not end in time
}

// startCLIWorker starts a worker for t. It stops when t ends.
func startCLIWorker(t *testing.T) *cliWorker {
	t.Helper()
	reqR, reqW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	respR, respW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	w := &cliWorker{
		requests:  json.NewEncoder(reqW),
		responses: json.NewDecoder(respR),
		output:    &syncBuffer{},
		done:      make(chan struct{}),
	}
	w.cmd = exec.Command(os.Args[0], "-test.run=^$")
	w.cmd.Env = append(os.Environ(), cliWorkerEnv+"=1")
	w.cmd.ExtraFiles = []*os.File{reqR, respW}
	w.cmd.Stdout, w.cmd.Stderr = w.output, w.output
	if err := w.cmd.Start(); err != nil {
		t.Fatalf("starting the opcuactl worker: %v", err)
	}
	_ = reqR.Close()
	_ = respW.Close()
	go func() {
		w.err = w.cmd.Wait()
		close(w.done)
	}()
	t.Cleanup(func() {
		_ = reqW.Close() // the worker exits at the end of the requests
		select {
		case <-w.done:
		case <-time.After(30 * time.Second):
			w.kill()
			<-w.done
			t.Errorf("the opcuactl worker did not exit\n%s", w.output)
		}
		_ = respR.Close()
		switch {
		case strings.Contains(w.output.String(), raceReport):
			t.Errorf("the race detector reported a data race in the opcuactl worker:\n%s", w.output)
		case w.err != nil && !w.killed:
			t.Errorf("the opcuactl worker: %v\n%s", w.err, w.output)
		}
	})
	return w
}

// run is runCLI in the worker: the same output, the same error message and
// the same exit code.
func (w *cliWorker) run(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	return w.runWithin(t, 0, args...)
}

// runWithin is run, and fails the test when the command does not end within
// d; 0 is no limit.
func (w *cliWorker) runWithin(t *testing.T, d time.Duration, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.requests.Encode(cliRequest{Args: args}); err != nil {
		t.Fatalf("opcuactl %s: the worker takes no command: %v\n%s", strings.Join(args, " "), err, w.output)
	}
	var resp cliResponse
	answered := make(chan error, 1)
	go func() { answered <- w.responses.Decode(&resp) }()
	var limit <-chan time.Time
	if d > 0 {
		timer := time.NewTimer(d)
		defer timer.Stop()
		limit = timer.C
	}
	select {
	case err := <-answered:
		if err != nil {
			t.Fatalf("opcuactl %s: no result from the worker: %v\n%s", strings.Join(args, " "), err, w.output)
		}
	case <-limit:
		w.kill()
		t.Fatalf("%v did not end within %v", args, d)
	}
	if resp.Err != nil {
		err = &exitError{code: resp.Code, err: errors.New(*resp.Err)}
	}
	return resp.Stdout, resp.Stderr, err
}

func (w *cliWorker) kill() {
	w.killed = true
	_ = w.cmd.Process.Kill()
}

// mustRun is mustRun in the worker.
func (w *cliWorker) mustRun(t *testing.T, args ...string) string {
	t.Helper()
	out, errOut, err := w.run(t, args...)
	if err != nil {
		t.Fatalf("opcuactl %s: %v\nstderr: %s", strings.Join(args, " "), err, errOut)
	}
	return out
}

// cliRunner runs opcuactl, in this process (inProcess) or in a worker, for
// the helpers that both kinds of test use.
type cliRunner interface {
	run(t *testing.T, args ...string) (stdout, stderr string, err error)
	mustRun(t *testing.T, args ...string) string
}

// inProcess is runCLI and mustRun.
type inProcess struct{}

func (inProcess) run(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	return runCLI(t, args...)
}

func (inProcess) mustRun(t *testing.T, args ...string) string {
	t.Helper()
	return mustRun(t, args...)
}

// raceReport starts what the race detector writes about a data race.
const raceReport = "WARNING: DATA RACE"

// childTestEnv names the test that the test binary, started again, runs as
// the only one.
const childTestEnv = "OPCUACTL_TEST_CHILD"

// runInChild reports whether this process is the child that runs t. When it
// is not, it runs t in such a child, a process of the test binary in which no
// other test runs, in parallel with the tests in this process, and fails t
// with the output of the child when t failed there or did not run. A test
// whose checks measure the heap runs so: the other tests do not allocate in
// that heap.
func runInChild(t *testing.T) (child bool) {
	t.Helper()
	if os.Getenv(childTestEnv) == t.Name() {
		return true
	}
	t.Parallel()
	args := []string{"-test.run=^" + regexp.QuoteMeta(t.Name()) + "$", "-test.count=1", "-test.v"}
	if d, ok := t.Deadline(); ok {
		args = append(args, "-test.timeout="+time.Until(d).String())
	}
	cmd := exec.Command(os.Args[0], args...)
	cmd.Env = append(os.Environ(), childTestEnv+"="+t.Name())
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "--- PASS: "+t.Name()+" ") || strings.Contains(string(out), raceReport) {
		t.Errorf("%s in a child process: %v\n%s", t.Name(), err, out)
	}
	return false
}
