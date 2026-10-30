// SPDX-License-Identifier: MIT

package opcuactl

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-opcua/pubsub"
	"github.com/otfabric/go-opcua/pubsub/security"
	"github.com/otfabric/go-opcua/server"
	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/opcuactl/internal/model"
)

// cliExecEnv makes the test binary run opcuactl with the JSON-encoded
// arguments in it instead of the tests: the in-process CLI runs one command
// at a time, and call and respond have to run together.
const cliExecEnv = "OPCUACTL_TEST_EXEC"

func TestMain(m *testing.M) {
	if args := os.Getenv(cliExecEnv); args != "" {
		var argv []string
		if err := json.Unmarshal([]byte(args), &argv); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		_ = os.Setenv("OPCUACTL_INSECURE_NO_SECURITY", "true")
		rootCmd.SetArgs(argv)
		os.Exit(ExitCode(Execute()))
	}
	// The test servers run without message security; the tests that need the
	// refusal clear this.
	_ = os.Setenv("OPCUACTL_INSECURE_NO_SECURITY", "true")
	os.Exit(m.Run())
}

// syncBuffer is a bytes.Buffer safe for a writing process and a reading
// test.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// cliProcess is opcuactl running in a child process.
type cliProcess struct {
	cmd            *exec.Cmd
	stdout, stderr *syncBuffer
	done           chan struct{}
	err            error
}

// startCLI runs opcuactl with args in a child process until it exits or the
// test ends.
func startCLI(t *testing.T, args ...string) *cliProcess {
	t.Helper()
	argv, _ := json.Marshal(args)
	p := &cliProcess{stdout: &syncBuffer{}, stderr: &syncBuffer{}, done: make(chan struct{})}
	p.cmd = exec.Command(os.Args[0], "-test.run=^$")
	p.cmd.Env = append(os.Environ(), cliExecEnv+"="+string(argv))
	p.cmd.Stdout, p.cmd.Stderr = p.stdout, p.stderr
	if err := p.cmd.Start(); err != nil {
		t.Fatalf("starting opcuactl %v: %v", args, err)
	}
	go func() {
		p.err = p.cmd.Wait()
		close(p.done)
	}()
	t.Cleanup(func() {
		_ = p.cmd.Process.Kill()
		<-p.done
	})
	return p
}

// wait waits for the process to exit.
func (p *cliProcess) wait(t *testing.T) {
	t.Helper()
	select {
	case <-p.done:
	case <-time.After(20 * time.Second):
		t.Fatalf("opcuactl did not exit\nstdout: %s\nstderr: %s", p.stdout, p.stderr)
	}
}

// waitStderr waits until the process wrote want on stderr.
func (p *cliProcess) waitStderr(t *testing.T, want string) {
	t.Helper()
	p.waitOutput(t, p.stderr, want)
}

// waitStdout waits until the process wrote want on stdout. The two are
// copied by separate goroutines: what the process wrote to stdout before
// stderr may arrive after it.
func (p *cliProcess) waitStdout(t *testing.T, want string) {
	t.Helper()
	p.waitOutput(t, p.stdout, want)
}

func (p *cliProcess) waitOutput(t *testing.T, out *syncBuffer, want string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for !strings.Contains(out.String(), want) {
		select {
		case <-p.done:
			t.Fatalf("opcuactl exited (%v) before writing %q\nstdout: %s\nstderr: %s", p.err, want, p.stdout, p.stderr)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("opcuactl did not write %q\nstdout: %s\nstderr: %s", want, p.stdout, p.stderr)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// callUntilAnswered runs `pubsub call` until a Responder that is still
// subscribing answers (a request sent before is lost on a broker).
func callUntilAnswered(t *testing.T, args ...string) string {
	t.Helper()
	args = append([]string{"pubsub", "call", "--timeout", "1s"}, args...)
	var errOut string
	for range 15 {
		out, e, err := runCLI(t, args...)
		if err == nil {
			return out
		}
		if ExitCode(err) != 2 {
			t.Fatalf("opcuactl %s: %v\nstderr: %s", strings.Join(args, " "), err, e)
		}
		errOut = e
	}
	t.Fatalf("opcuactl %s: no answer\nstderr: %s", strings.Join(args, " "), errOut)
	return ""
}

func parseActionResult(t *testing.T, out string) model.PubSubActionResult {
	t.Helper()
	var r model.PubSubActionResult
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("parsing %q: %v", out, err)
	}
	return r
}

func parseActionRequests(t *testing.T, out string) []model.PubSubActionRequest {
	t.Helper()
	var all []model.PubSubActionRequest
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		var r model.PubSubActionRequest
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("parsing %q: %v", line, err)
		}
		all = append(all, r)
	}
	return all
}

// writeKeysFile writes a keys file of SecurityGroup sg with one random
// PubSub-Aes256-CTR key.
func writeKeysFile(t *testing.T) string {
	t.Helper()
	key := make([]byte, security.Aes256CTR().KeyLength())
	_, _ = rand.Read(key)
	b, _ := json.Marshal(model.SecurityKeys{SecurityGroupID: "sg", SecurityPolicyURI: security.PolicyURIAes256CTR, FirstTokenID: 1,
		TimeToNextKey: "1h", KeyLifetime: "1h", Keys: []model.SecurityKey{{TokenID: 1, Length: len(key), Key: base64.StdEncoding.EncodeToString(key)}}})
	file := filepath.Join(t.TempDir(), "keys.json")
	if err := os.WriteFile(file, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return file
}

// TestE2E_PubSubActionsUDP calls a `pubsub respond` Responder with
// `pubsub call` over UDP, plain and secured.
func TestE2E_PubSubActionsUDP(t *testing.T) {
	keys := writeKeysFile(t)
	for _, secured := range []bool{false, true} {
		t.Run(fmt.Sprint("secured=", secured), func(t *testing.T) {
			respURL, reqURL := freeUDPURL(t), freeUDPURL(t)
			var sec []string
			if secured {
				sec = []string{"--message-security", "sign-and-encrypt", "--keys-file", keys}
			}
			resp := startCLI(t, append([]string{"pubsub", "respond", "--url", respURL, "--publisher-id", "7", "--writer-id", "100",
				"--target", "1:Add", "--target", "2:Echo", "--input", "A:Int32", "--input", "B:Int32", "--jsonl", "--count", "3", "-v"}, sec...)...)
			resp.waitStderr(t, "responding as publisher 7")

			call := append([]string{"--url", reqURL, "--request-address", respURL, "--publisher-id", "7", "--writer-id", "100",
				"--resend-interval", "100ms", "--input", "A:Int32=2", "--input", "B:Int32=3"}, sec...)
			// The Responder listens once it reports itself, and the
			// resends cover a lost datagram: one call, no retries, which
			// would be new requests that count towards --count 3.
			out := mustRun(t, append([]string{"pubsub", "call"}, append(call, "--output", "A:Int32", "--output", "B:Int32", "--format", "json")...)...)
			r := parseActionResult(t, out)
			if r.Status != "Good" || r.PublisherID != "7" || r.DataSetWriterID != 100 || r.ActionTargetID != 1 || r.RequestID == 0 ||
				len(r.Outputs) != 2 || r.Outputs[0].Name != "A" || r.Outputs[0].Value != float64(2) || r.Outputs[1].Value != float64(3) ||
				r.Outputs[0].Type != "Int32" {
				t.Errorf("unexpected result: %+v", r)
			}

			// The text form, target 2, without output names.
			out = mustRun(t, append([]string{"pubsub", "call", "--target-id", "2"}, call...)...)
			wantContains(t, out, "pub=7 writer=100 target=2 request=", "status=Good", "#0 = 2 (Int32)", "#1 = 3 (Int32)")
			// --quiet prints the output values.
			out = mustRun(t, append([]string{"pubsub", "call", "-q"}, call...)...)
			if out != "2\n3\n" {
				t.Errorf("--quiet: %q", out)
			}

			resp.wait(t)
			if resp.err != nil {
				t.Fatalf("respond: %v\nstderr: %s", resp.err, resp.stderr)
			}
			reqs := parseActionRequests(t, resp.stdout.String())
			if len(reqs) != 3 {
				t.Fatalf("respond printed %d requests:\n%s", len(reqs), resp.stdout)
			}
			if q := reqs[0]; q.RequestorID == "" || q.DataSetWriterID != 100 || q.ActionTargetID != 1 || q.Status != "Good" ||
				len(q.Inputs) != 2 || q.Inputs[0].Name != "A" || q.Inputs[1].Value != float64(3) || len(q.Outputs) != 2 {
				t.Errorf("unexpected request: %+v", q)
			}
			if reqs[1].ActionTargetID != 2 {
				t.Errorf("second request: %+v", reqs[1])
			}
		})
	}
}

// TestE2E_PubSubActionsResults covers the results of a call: fixed outputs,
// Bad statuses of the Action and of the runtime, and timeouts.
func TestE2E_PubSubActionsResults(t *testing.T) {
	respURL, reqURL := freeUDPURL(t), freeUDPURL(t)
	resp := startCLI(t, "pubsub", "respond", "--url", respURL, "--input", "A:Int32", "--output", "Sum:Int32=5",
		"--output", "Text:String=ok", "-v", "--diagnostics", "--format", "json", "--duration", "60s")
	resp.waitStderr(t, "responding as publisher 1")
	call := []string{"--url", reqURL, "--request-address", respURL, "--resend-interval", "100ms"}

	out := callUntilAnswered(t, append(call, "--input", "A:Int32=1", "--output", "Sum:Int32", "--output", "Text:String", "--format", "json")...)
	if r := parseActionResult(t, out); r.Status != "Good" || len(r.Outputs) != 2 || r.Outputs[0].Value != float64(5) || r.Outputs[1].Value != "ok" {
		t.Errorf("fixed outputs: %+v", r)
	}

	// Results the runtime gives: wrong inputs.
	for _, tc := range []struct {
		inputs []string
		status string
	}{
		{[]string{"--input", "A:String=x"}, "BadTypeMismatch"},
		{nil, "BadArgumentsMissing"},
		{[]string{"--input", "A:Int32=1", "--input", "B:Int32=2"}, "BadTooManyArguments"},
	} {
		out, _, err := runCLI(t, append(append([]string{"pubsub", "call", "--format", "json"}, call...), tc.inputs...)...)
		if ExitCode(err) != 3 {
			t.Errorf("%v: exit code %d (%v), want 3", tc.inputs, ExitCode(err), err)
			continue
		}
		if r := parseActionResult(t, out); r.Status != tc.status || len(r.Outputs) != 0 {
			t.Errorf("%v: %+v, want %s", tc.inputs, r, tc.status)
		}
	}

	// No response: an unknown writer (dropped), an unknown target
	// (rejected) and nobody listening.
	for _, args := range [][]string{
		append([]string{"--writer-id", "9", "--input", "A:Int32=1"}, call...),
		append([]string{"--target-id", "9", "--input", "A:Int32=1"}, call...),
		{"--url", reqURL, "--request-address", freeUDPURL(t), "--input", "A:Int32=1"},
	} {
		start := time.Now()
		_, errOut, err := runCLI(t, append([]string{"pubsub", "call", "--timeout", "300ms"}, args...)...)
		if ExitCode(err) != 2 || !strings.Contains(errOut, "no response") {
			t.Errorf("%v: exit code %d, stderr %q", args, ExitCode(err), errOut)
		}
		if d := time.Since(start); d > 5*time.Second {
			t.Errorf("%v: took %v", args, d)
		}
	}

	_ = resp.cmd.Process.Signal(os.Interrupt)
	resp.wait(t)
	diag := diagnosticsOf(t, resp.stderr.String())
	if diag[ctrActionRequestsReceived] < 4 || diag[ctrActionResponsesSent] < 4 || diag[ctrActionRequestsRejected] == 0 {
		t.Errorf("responder diagnostics: %v\nstderr: %s", diag, resp.stderr)
	}
}

// TestE2E_PubSubRespondStatus answers with a Bad status and after a delay.
func TestE2E_PubSubRespondStatus(t *testing.T) {
	t.Run("status", func(t *testing.T) {
		respURL, reqURL := freeUDPURL(t), freeUDPURL(t)
		resp := startCLI(t, "pubsub", "respond", "--url", respURL, "--status", "BadInternalError", "--output", "X:Int32=1", "--count", "1", "-v")
		resp.waitStderr(t, "responding")
		call := []string{"pubsub", "call", "--url", reqURL, "--request-address", respURL, "--resend-interval", "100ms", "--input", "A:Int32=1"}
		out, errOut, err := runCLI(t, call...)
		if ExitCode(err) != 3 || !strings.Contains(out, "status=BadInternalError") || !strings.Contains(errOut, "BadInternalError") {
			t.Errorf("exit code %d, out %q, stderr %q", ExitCode(err), out, errOut)
		}
		resp.wait(t)
		wantContains(t, resp.stdout.String(), "target=1", "inputs: #0=1 -> BadInternalError")
		if strings.Contains(resp.stdout.String(), "outputs:") {
			t.Errorf("a Bad response has no outputs: %s", resp.stdout)
		}
	})
	t.Run("delay", func(t *testing.T) {
		respURL, reqURL := freeUDPURL(t), freeUDPURL(t)
		resp := startCLI(t, "pubsub", "respond", "--url", respURL, "--delay", "3s", "--duration", "30s", "-v")
		resp.waitStderr(t, "responding")
		_, errOut, err := runCLI(t, "pubsub", "call", "--url", reqURL, "--request-address", respURL, "--timeout", "500ms",
			"--resend-interval", "100ms", "--input", "A:Int32=1", "--diagnostics", "--format", "json")
		// The TimeoutHint of 500ms ends on both sides: the Responder
		// cancels the handler and answers Done with Bad_Timeout (Part 14
		// §6.2.11), the Requestor stops waiting. Which comes first is a
		// race, and both are timeouts.
		d := diagnosticsOf(t, errOut)
		switch ExitCode(err) {
		case 2:
			if !strings.Contains(errOut, "no response") || d[ctrActionTimeouts] != 1 {
				t.Errorf("timed out: stderr %q, requestor diagnostics %v", errOut, d)
			}
		case 3:
			if !strings.Contains(errOut, "answered BadTimeout") || d[ctrActionResponsesReceived] == 0 {
				t.Errorf("answered: stderr %q, requestor diagnostics %v", errOut, d)
			}
		default:
			t.Errorf("exit code %d, stderr %q", ExitCode(err), errOut)
		}
		if d[ctrActionRequestsSent] == 0 {
			t.Errorf("requestor diagnostics: %v", d)
		}
		resp.waitStdout(t, "-> BadTimeout")
	})
}

// TestE2E_PubSubActionsMQTT calls a `pubsub respond` Responder through the
// test broker, UADP and JSON, MQTT 3.1.1 and 5.0, and watches the exchange
// with `pubsub subscribe`.
func TestE2E_PubSubActionsMQTT(t *testing.T) {
	b := startBroker(t)
	for _, tc := range []struct{ enc, version string }{{"uadp", "3.1.1"}, {"json", "3.1.1"}, {"json", "5.0"}} {
		t.Run(tc.enc+"/"+tc.version, func(t *testing.T) {
			prefix := "act" + strings.ReplaceAll(tc.version, ".", "")
			common := []string{"--url", b.url(), "--encoding", tc.enc, "--mqtt-version", tc.version, "--topic-prefix", prefix,
				"--publisher-id", "responder-1"}
			sub := startCLI(t, "pubsub", "subscribe", "--url", b.url(), "--topic", prefix+"/"+tc.enc+"/#", "--count", "3",
				"--format", "json", "--field", "A:Int32", "--field", "B:Int32", "-v")
			sub.waitStderr(t, "listening on")
			resp := startCLI(t, append([]string{"pubsub", "respond", "--input", "A:Int32", "--input", "B:Int32",
				"--count", "1", "--jsonl", "-v"}, common...)...)
			resp.waitStderr(t, "responding as publisher responder-1")

			out := callUntilAnswered(t, append(common, "--input", "A:Int32=2", "--input", "B:Int32=3",
				"--output", "A:Int32", "--output", "B:Int32", "--format", "json")...)
			if r := parseActionResult(t, out); r.Status != "Good" || len(r.Outputs) != 2 || r.Outputs[1].Value != float64(3) {
				t.Errorf("result: %+v", r)
			}
			resp.wait(t)
			if reqs := parseActionRequests(t, resp.stdout.String()); len(reqs) != 1 || reqs[0].Inputs[0].Value != float64(2) {
				t.Errorf("respond: %s", resp.stdout)
			}

			sub.wait(t)
			// The request, the response and the acknowledgement (Idle), after
			// the requests sent before the Responder subscribed.
			got := parseDataSets(t, sub.stdout.String())
			var req, res model.PubSubDataSet
			for _, ds := range got {
				if ds.MessageType == "action-response" && res.Action == nil {
					res = ds
				}
			}
			for _, ds := range got {
				if ds.MessageType == "action-request" && res.Action != nil && ds.Action.RequestID == res.Action.RequestID && ds.Action.ActionState == "Executing" {
					req = ds
				}
			}
			if req.Action == nil || res.Action == nil {
				t.Fatalf("no request and response:\n%s\nstderr: %s", sub.stdout, sub.stderr)
			}
			if req.MessageType != "action-request" || req.Action == nil || req.Action.ActionState != "Executing" ||
				req.Action.RequestorID == "" || req.PublisherID != "responder-1" || fieldValue(req, "A") != float64(2) ||
				req.Topic != prefix+"/"+tc.enc+"/action-request/responder-1/Actions" {
				t.Errorf("request: %+v %+v", req, req.Action)
			}
			if res.MessageType != "action-response" || res.Status != "Good" || res.Action == nil || res.Action.ActionState != "Done" ||
				res.Action.RequestID != req.Action.RequestID || fieldValue(res, "B") != float64(3) ||
				res.Topic != prefix+"/"+tc.enc+"/action-response/responder-1/Actions" {
				t.Errorf("response: %+v %+v", res, res.Action)
			}
		})
	}
}

// TestE2E_PubSubRespondInProcess serves a go-opcua runtime Requestor with
// the in-process CLI.
func TestE2E_PubSubRespondInProcess(t *testing.T) {
	b := startBroker(t)
	requestor, err := pubsub.New(pubsub.NewConfiguration(nil, pubsub.MQTTConnection("R", b.url(), "requestor-9", pubsub.MappingUADP)))
	if err != nil {
		t.Fatal(err)
	}
	if err := requestor.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = requestor.Close() })

	results := make(chan *pubsub.ActionResult, 1)
	go func() {
		for range 50 {
			res, err := requestor.CallAction(context.Background(), pubsub.ActionTarget{
				Connection: "R", PublisherID: uint16(1), DataSetWriterID: 1, ActionTargetID: 3,
				RequestAddress: pubsub.ActionRequestTopic("opcua", pubsub.MappingUADP, "1", "Actions"),
				Timeout:        300 * time.Millisecond,
			}, []*ua.Variant{ua.MustVariant("hello")})
			if err == nil {
				results <- res
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		close(results)
	}()
	out := mustRun(t, "pubsub", "respond", "--url", b.url(), "--target", "3:Greet", "--input", "Name:String",
		"--output", "Reply:String=hi", "--count", "1", "--diagnostics", "--qos", "1")
	wantContains(t, out, "requestor=requestor-9 writer=1 target=3 request=", "inputs: Name=hello -> Good outputs: Reply=hi")
	res := <-results
	if res == nil || res.Status != ua.StatusGood || len(res.Outputs) != 1 || res.Outputs[0].Value() != "hi" {
		t.Errorf("result: %+v", res)
	}
}

func TestPubSubActionFlagErrors(t *testing.T) {
	udp := "opc.udp://127.0.0.1:1"
	for _, args := range [][]string{
		{"call"},
		{"call", "--url", udp, "--writer-id", "0"},
		{"call", "--url", udp, "--encoding", "json"},
		{"call", "--url", udp, "--encoding", "bogus"},
		{"call", "--url", udp, "--input", "A:Int32"},
		{"call", "--url", udp, "--input", "A:Int32=x"},
		{"call", "--url", udp, "--output", "A"},
		{"call", "--url", udp, "--message-security", "sign"},
		{"call", "--url", udp, "--keys-file", "k"},
		{"call", "--url", udp, "--request-address", "mqtt://x:1883"},
		{"call", "--url", udp, "--resend-interval", "-1s"},
		{"call", "--url", udp, "--timeout", "0s"},
		{"call", "--url", udp, "--publisher-id", "u8:300"},
		{"call", "--url", udp, "--requestor-id", "u16:99999"},
		{"call", "--url", "mqtt://127.0.0.1:1", "--encoding", "json", "--message-security", "sign", "--keys-file", "k"},
		{"call", "--url", udp, "--message-security", "sign", "--keys-file", filepath.Join(t.TempDir(), "missing.json")},
		{"call", "--url", udp, "--diagnostics-level", "bogus"},
		{"respond"},
		{"respond", "--url", udp, "--target", "x"},
		{"respond", "--url", udp, "--target", "1", "--target", "1:Again"},
		{"respond", "--url", udp, "--input", "A"},
		{"respond", "--url", udp, "--output", "A:Int32"},
		{"respond", "--url", udp, "--status", "Bogus"},
		{"respond", "--url", udp, "--delay", "-1s"},
		{"respond", "--url", udp, "--count", "-1"},
		{"respond", "--url", udp, "--rate", "-1"},
		{"respond", "--url", udp, "--writer-group-id", "0"},
		{"respond", "--url", udp, "--writer-id", "0"},
		{"respond", "--url", udp, "--encoding", "json"},
		{"respond", "--url", "opc.eth://01-00-5E-00-00-01"},
		{"respond", "--url", udp, "--request-address", "opc.udp://127.0.0.1:2"},
		{"call", "--url", "mqtt://127.0.0.1:1", "--qos", "1"},
	} {
		_, _, err := runCLI(t, append([]string{"pubsub"}, args...)...)
		if err == nil || ExitCode(err) != 1 {
			t.Errorf("pubsub %v: exit code %d (%v), want 1", args, ExitCode(err), err)
		}
	}
	// A broker nobody listens on: a transport failure.
	_, _, err := runCLI(t, "pubsub", "call", "--url", "mqtt://127.0.0.1:1", "--timeout", "500ms")
	if ExitCode(err) != 2 {
		t.Errorf("unreachable broker: exit code %d (%v), want 2", ExitCode(err), err)
	}
}

func TestActionHelpers(t *testing.T) {
	if !isBadStatus(ua.StatusBadInternalError) || isBadStatus(ua.StatusGood) || isBadStatus(ua.StatusUncertain) {
		t.Error("isBadStatus")
	}
	for qos, want := range map[uint8]ua.BrokerTransportQoS{0: ua.BrokerTransportQoSAtMostOnce, 1: ua.BrokerTransportQoSAtLeastOnce, 2: ua.BrokerTransportQoSExactlyOnce} {
		if got := brokerQoS(qos); got != want {
			t.Errorf("brokerQoS(%d) = %v", qos, got)
		}
	}
	line := actionRequestLine(model.PubSubActionRequest{Timestamp: "T", RequestorID: "r", DataSetWriterID: 1, ActionTargetID: 2, RequestID: 3,
		Inputs: []model.PubSubField{{Index: 0, Name: "A", Value: 1}}, Status: "Good", Outputs: []model.PubSubField{{Index: 0, Value: "x"}}})
	if want := "T  requestor=r writer=1 target=2 request=3 inputs: A=1 -> Good outputs: #0=x"; line != want {
		t.Errorf("line %q, want %q", line, want)
	}
	if id := randomRequestorID(); !strings.HasPrefix(id, "opcuactl-") || len(id) != len("opcuactl-")+8 {
		t.Errorf("requestor id %q", id)
	}
}

// TestE2E_PubSubConfigShowActions lists a server's PublishedAction: the
// server shows it as a PublishedDataSetType object with its writer.
func TestE2E_PubSubConfigShowActions(t *testing.T) {
	pds := pubsub.PublishedActions("Calc", []pubsub.Field{{Name: "A", Type: ua.TypeIDInt32}}, nil, pubsub.ActionTargetOf(1, "Run"))
	cfg := pubsub.NewConfiguration([]*ua.PublishedDataSetDataType{pds},
		pubsub.AttachWriterGroups(pubsub.UDPConnection("conn", freeUDPURL(t), uint16(4)),
			pubsub.WriterGroup("Actions", 2, time.Second, pubsub.DataSetWriter("CalcWriter", 3, "Calc"))))
	s := startServer(t, server.WithPubSub(cfg))
	out := mustRun(t, "pubsub", "config", "show", "-e", s.url)
	wantContains(t, out, "dataset", "Calc", "CalcWriter")
}
