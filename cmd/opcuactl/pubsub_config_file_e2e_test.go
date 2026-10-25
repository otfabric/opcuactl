// SPDX-License-Identifier: MIT

package opcuactl

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/otfabric/go-opcua/server"
	"github.com/otfabric/opcuactl/internal/model"
)

// TestE2E_PubSubConfigFile adds a PublishedAction to a go-opcua server through
// its PubSubConfiguration file (add-action), makes the server the Responder
// with the method-based commands, calls it, and reads the configuration back
// (export, reserve-ids).
func TestE2E_PubSubConfigFile(t *testing.T) {
	s := startServer(t, server.WithPubSub(nil))
	ep := []string{"-e", s.url}
	run := func(args ...string) string {
		t.Helper()
		return strings.TrimSpace(mustRun(t, append(append([]string{"pubsub", "config"}, args...), ep...)...))
	}

	out := run("add-action", "--name", "Calc", "--request", "A:Int32", "--response", "R:Int32", "--target", "1:Run", "--target", "2:Stop")
	wantContains(t, out, `added dataset "Calc"`)
	out = run("add-action", "--name", "Square", "--request", "n:Int32", "--response", "r:Int32", "--target", "1:Square",
		"--method", "1=ns=1;s=Methods@ns=1;s=Square", "--format", "json")
	var added model.PubSubAdded
	if err := json.Unmarshal([]byte(out), &added); err != nil {
		t.Fatal(err)
	}
	if added.Kind != "dataset" || added.Name != "Square" || added.NodeID == "i=0" {
		t.Errorf("add-action: %+v", added)
	}

	respURL, reqURL := freeUDPURL(t), freeUDPURL(t)
	conn := run("add-connection", "--name", "Responder", "--url", respURL, "--publisher-id", "7", "-q")
	wg := run("add-writer-group", "--connection", conn, "--name", "Actions", "--writer-group-id", "1", "--interval", "50ms", "-q")
	run("add-writer", "--group", wg, "--name", "CalcWriter", "--writer-id", "100", "--dataset", "Calc", "-q")

	// The server answers; it has no handler for Calc.
	args := []string{"pubsub", "call", "--timeout", "1s", "--url", reqURL, "--request-address", respURL, "--publisher-id", "7",
		"--writer-id", "100", "--resend-interval", "100ms", "--input", "A:Int32=2", "--format", "json"}
	var r model.PubSubActionResult
	for range 15 {
		o, _, err := runCLI(t, args...)
		if err != nil && ExitCode(err) == 3 && o != "" {
			r = parseActionResult(t, o)
			break
		}
	}
	if r.Status != "BadNotImplemented" || r.PublisherID != "7" || r.ActionTargetID != 1 {
		t.Errorf("call: %+v", r)
	}

	out = run("export")
	wantContains(t, out, `"Calc"`, `"Square"`, `"Responder"`, `"CalcWriter"`)

	var ids model.PubSubReservedIDs
	if err := json.Unmarshal([]byte(run("reserve-ids", "--writer-groups", "2", "--writers", "1", "--format", "json")), &ids); err != nil {
		t.Fatal(err)
	}
	if ids.DefaultPublisherID != "7" || len(ids.WriterGroupIDs) != 2 || ids.WriterGroupIDs[0] < 0x8000 || len(ids.DataSetWriterIDs) != 1 ||
		ids.TransportProfileURI != "http://opcfoundation.org/UA-Profile/Transport/pubsub-udp-uadp" {
		t.Errorf("reserve-ids: %+v", ids)
	}
	wantContains(t, run("reserve-ids", "--url", "mqtt://127.0.0.1:1883", "--encoding", "json"), "default publisher id: 7", "writer group ids:")

	// Errors.
	for _, a := range [][]string{
		{"add-action", "--name", "Calc", "--target", "1:Run"}, // duplicated name
		{"add-action", "--name", "X"},
		{"add-action", "--target", "1:Run"},
		{"add-action", "--name", "X", "--target", "1:Run", "--target", "1:Again"},
		{"add-action", "--name", "X", "--target", "1:Run", "--request", "bad"},
		{"add-action", "--name", "X", "--target", "1:Run", "--target", "2:B", "--method", "1=ns=1;s=O@ns=1;s=M"},
		{"add-action", "--name", "X", "--target", "1:Run", "--method", "3=ns=1;s=O@ns=1;s=M"},
		{"add-action", "--name", "X", "--target", "1:Run", "--method", "1=ns=1;s=O"},
		{"reserve-ids", "--url", "bogus://x"},
		{"reserve-ids", "--encoding", "x"},
		{"export", "--ua-encoding", "x"},
	} {
		if _, _, err := runCLI(t, append(append([]string{"pubsub", "config"}, a...), ep...)...); err == nil {
			t.Errorf("%v succeeded", a)
		}
	}
}
