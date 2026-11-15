// SPDX-License-Identifier: MIT

package opcuactl

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/otfabric/go-opcua/id"
	"github.com/otfabric/go-opcua/server"
	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/go-opcua/uasc"
)

// loopServer is a test server with a loop of references below the folder
// Loop: Loop -> A -> B -> A, and the variable V below B.
func loopServer(t *testing.T) (s *testServer, start string) {
	t.Helper()
	s = startServer(t)
	ns := s.ns.ID()
	folder := func(name string) *server.Node {
		return s.ns.AddNode(server.NewFolderNode(ua.NewStringNodeID(ns, "Loop."+name), name))
	}
	loop, a, b := folder("Root"), folder("A"), folder("B")
	v := s.ns.AddNewVariableStringNode("Loop.V", int32(7))
	loop.AddRef(a, id.Organizes, true)
	a.AddRef(b, id.Organizes, true)
	b.AddRef(a, id.Organizes, true)
	b.AddRef(v, id.HasComponent, true)
	return s, loop.ID().String()
}

// mintingServer is a test server that answers every browse of a node Gen.*
// with three children it never named before: a Variable and two Objects.
func mintingServer(t *testing.T) (s *testServer, start string) {
	t.Helper()
	var next atomic.Int64
	var ns uint16
	s = startServerWith(t, func(srv *server.Server) {
		srv.RegisterHandler(id.BrowseRequestEncodingDefaultBinary, func(_ context.Context, _ *uasc.SecureChannel, r ua.Request, _ uint32) (ua.Response, error) {
			req := r.(*ua.BrowseRequest)
			resp := &ua.BrowseResponse{
				ResponseHeader: &ua.ResponseHeader{
					Timestamp: time.Now(), RequestHandle: req.RequestHeader.RequestHandle,
					ServiceDiagnostics: &ua.DiagnosticInfo{}, StringTable: []string{}, AdditionalHeader: ua.NewExtensionObject(nil),
				},
				Results: make([]*ua.BrowseResult, len(req.NodesToBrowse)),
			}
			for i, br := range req.NodesToBrowse {
				if !strings.HasPrefix(br.NodeID.StringID(), "Gen.") {
					resp.Results[i] = &ua.BrowseResult{StatusCode: ua.StatusBadNodeIDUnknown}
					continue
				}
				result := &ua.BrowseResult{StatusCode: ua.StatusGood}
				for _, class := range []ua.NodeClass{ua.NodeClassVariable, ua.NodeClassObject, ua.NodeClassObject} {
					name := fmt.Sprintf("Gen.%d", next.Add(1))
					result.References = append(result.References, &ua.ReferenceDescription{
						ReferenceTypeID: ua.NewNumericNodeID(0, id.Organizes), IsForward: true,
						NodeID:     ua.NewExpandedNodeID(ua.NewStringNodeID(ns, name), "", 0),
						BrowseName: &ua.QualifiedName{NamespaceIndex: ns, Name: name}, DisplayName: &ua.LocalizedText{Text: name},
						NodeClass: class, TypeDefinition: ua.NewNumericExpandedNodeID(0, id.FolderType),
					})
				}
				resp.Results[i] = result
			}
			return resp, nil
		})
	})
	ns = s.ns.ID()
	return s, ua.NewStringNodeID(ns, "Gen.root").String()
}

// runWithin runs the command, and fails the test when it does not end within
// d: the walk of a loop must end.
func runWithin(t *testing.T, d time.Duration, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	type result struct {
		stdout, stderr string
		err            error
	}
	done := make(chan result, 1)
	go func() {
		o, e, err := runCLI(t, args...)
		done <- result{o, e, err}
	}()
	select {
	case r := <-done:
		return r.stdout, r.stderr, r.err
	case <-time.After(d):
		t.Fatalf("%v did not end within %v", args, d)
		return "", "", nil
	}
}

// nodeIDsIn counts the "nodeId" values in a JSON document.
func nodeIDsIn(t *testing.T, out string) map[string]int {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("%v:\n%s", err, out)
	}
	ids := map[string]int{}
	var walk func(any)
	walk = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			for k, x := range v {
				if s, ok := x.(string); ok && k == "nodeId" {
					ids[s]++
				}
				walk(x)
			}
		case []any:
			for _, x := range v {
				walk(x)
			}
		}
	}
	walk(v)
	return ids
}

// The recursive walks visit each node once where references loop, and end
// with exit 0.
func TestWalkLoop(t *testing.T) {
	s, start := loopServer(t)
	node := func(name string) string { return ua.NewStringNodeID(s.ns.ID(), "Loop."+name).String() }
	for _, c := range []struct {
		args []string
		want []string // each once in the JSON output
	}{
		{[]string{"get", "vars", "-n", start, "--recursive", "--no-read"}, []string{node("V")}},
		{[]string{"get", "vars", "-n", start, "--recursive"}, []string{node("V")}},
		{[]string{"get", "vars", "-n", start, "--recursive", "--depth", "8"}, []string{node("V")}},
		{[]string{"browse", "tree", "-n", start, "--depth", "0"}, []string{start, node("A"), node("B"), node("V")}},
		{[]string{"browse", "tree", "-n", start, "--depth", "8"}, []string{start, node("A"), node("B"), node("V")}},
		{[]string{"export", "nodes", "-n", start, "--depth", "0"}, []string{start, node("A"), node("B"), node("V")}},
		{[]string{"find", "node", "--start-node", start, "--name", "V"}, []string{node("V")}},
	} {
		t.Run(strings.Join(c.args[:2], " ")+" "+strings.Join(c.args[4:], " "), func(t *testing.T) {
			stdout, stderr, err := runWithin(t, 30*time.Second, append(c.args, "-e", s.url, "-f", "json", "--timeout", "5s")...)
			if err != nil {
				t.Fatalf("exit %d: %v\n%s", ExitCode(err), err, stderr)
			}
			ids := nodeIDsIn(t, stdout)
			for _, w := range c.want {
				if ids[w] != 1 {
					t.Errorf("%s appears %d times:\n%s", w, ids[w], stdout)
				}
			}
			if len(ids) != len(c.want) {
				t.Errorf("node ids %v, want %v", ids, c.want)
			}
		})
	}

	// The table of browse tree: each node once, too.
	out := mustRun(t, "browse", "tree", "-e", s.url, "-n", start, "--depth", "0")
	if n := strings.Count(out, "("+node("V")+")"); n != 1 || strings.Count(out, "("+node("A")+")") != 1 {
		t.Errorf("table:\n%s", out)
	}
}

// --max-nodes: a walk of exactly that many nodes is complete; one more ends
// with the output so far and exit 7. 0 is no limit, a negative value a usage
// error.
func TestWalkMaxNodes(t *testing.T) {
	s, start := loopServer(t) // 3 nodes below start: A, B and V
	for _, args := range [][]string{
		{"get", "vars", "-n", start, "--recursive", "--no-read"},
		{"browse", "tree", "-n", start, "--depth", "0"},
		{"browse", "tree", "-n", start, "--depth", "0", "-f", "json"},
		{"export", "nodes", "-n", start, "--depth", "0"},
		{"export", "values", "--start-node", start, "--depth", "0"},
		{"find", "node", "--start-node", start, "--name", "V"},
		{"find", "type", "--start-node", start, "--data-type", "Int32"},
	} {
		for _, c := range []struct {
			max  string
			code int
		}{{"-1", exitUsage}, {"0", exitOK}, {"1", exitPartial}, {"2", exitPartial}, {"3", exitOK}, {"4", exitOK}} {
			t.Run(fmt.Sprintf("%s --max-nodes %s", strings.Join(args[:2], " "), c.max), func(t *testing.T) {
				_, stderr, err := runWithin(t, 30*time.Second, append(args, "-e", s.url, "--max-nodes", c.max)...)
				if got := ExitCode(err); got != c.code {
					t.Fatalf("exit %d, want %d (%v)\n%s", got, c.code, err, stderr)
				}
				if c.code == exitPartial && !strings.Contains(stderr, "stopped after "+c.max+" nodes (--max-nodes): the output is incomplete") {
					t.Errorf("stderr %q", stderr)
				}
			})
		}
	}
}

// A server that names new nodes on every browse: the walk stops at
// --max-nodes with exit 7 and what it found so far, or at --walk-timeout
// with exit 5.
func TestWalkUnboundedServer(t *testing.T) {
	s, start := mintingServer(t)
	for _, c := range []struct {
		args []string
		json bool // the output so far is a JSON document
	}{
		{[]string{"get", "vars", "-n", start, "--recursive", "--no-read", "-f", "json"}, true},
		{[]string{"browse", "tree", "-n", start, "--depth", "0"}, false},
		{[]string{"browse", "tree", "-n", start, "--depth", "0", "-f", "json"}, true},
		{[]string{"export", "nodes", "-n", start, "--depth", "0", "-f", "json"}, true},
		{[]string{"find", "node", "--start-node", start, "--name", "Gen.", "-f", "json"}, true},
	} {
		t.Run(strings.Join(c.args, " "), func(t *testing.T) {
			stdout, stderr, err := runWithin(t, 60*time.Second, append(c.args, "-e", s.url, "--max-nodes", "50")...)
			if got := ExitCode(err); got != exitPartial {
				t.Fatalf("exit %d, want %d (%v)\n%s", got, exitPartial, err, stderr)
			}
			if !strings.Contains(stderr, "stopped after 50 nodes (--max-nodes)") {
				t.Errorf("stderr %q", stderr)
			}
			if c.json {
				if ids := nodeIDsIn(t, stdout); len(ids) == 0 || len(ids) > 51 {
					t.Errorf("%d nodes in the output so far", len(ids))
				}
			} else if n := strings.Count(stdout, "\n"); n != 50 {
				t.Errorf("%d lines, want 50", n)
			}

			_, stderr, err = runWithin(t, 60*time.Second, append(c.args, "-e", s.url, "--max-nodes", "0", "--walk-timeout", "300ms")...)
			if got := ExitCode(err); got != exitTimeout {
				t.Errorf("--walk-timeout: exit %d, want %d (%v)\n%s", got, exitTimeout, err, stderr)
			}
		})
	}
}
