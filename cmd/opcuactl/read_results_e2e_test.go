// SPDX-License-Identifier: MIT

package opcuactl

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-opcua/id"
	"github.com/otfabric/go-opcua/server"
	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/go-opcua/uasc"
)

// readResultsServer is a server whose Read handler answers a Read of two
// or more nodes with results(n) results for its n nodes: the results of the
// nodes it was asked for, repeated or cut to that number. A Read of one
// node, such as the NamespaceArray at connect, is answered in full.
func readResultsServer(t *testing.T, results func(n int) int, opts ...server.Option) *testServer {
	t.Helper()
	return startServerWith(t, func(srv *server.Server) {
		srv.RegisterHandler(id.ReadRequestEncodingDefaultBinary, func(_ context.Context, _ *uasc.SecureChannel, r ua.Request, _ uint32) (ua.Response, error) {
			req := r.(*ua.ReadRequest)
			dvs := make([]*ua.DataValue, len(req.NodesToRead))
			for i, rv := range req.NodesToRead {
				dvs[i] = &ua.DataValue{EncodingMask: ua.DataValueStatusCode, Status: ua.StatusBadNodeIDUnknown}
				if ns, err := srv.Namespace(int(rv.NodeID.Namespace())); err == nil {
					if dv := ns.Attribute(rv.NodeID, rv.AttributeID); dv != nil {
						cp := *dv
						dvs[i] = &cp
					}
				}
			}
			if n := len(dvs); n >= 2 {
				want := results(n)
				for len(dvs) < want {
					dvs = append(dvs, dvs[len(dvs)%n])
				}
				dvs = dvs[:want]
			}
			return &ua.ReadResponse{
				ResponseHeader: &ua.ResponseHeader{
					Timestamp: time.Now(), RequestHandle: req.RequestHeader.RequestHandle,
					ServiceDiagnostics: &ua.DiagnosticInfo{}, StringTable: []string{}, AdditionalHeader: ua.NewExtensionObject(nil),
				},
				Results: dvs,
			}, nil
		})
	}, opts...)
}

// A batch read whose Read the server answers with another number of results
// than nodes ends with exit 6 and BadUnknownResponse on stderr, and prints
// no result; with one result per node the command prints them and exits 0.
func TestReadResultCount(t *testing.T) {
	counts := []struct {
		name    string
		results func(n int) int
	}{
		{"n results", func(n int) int { return n }},
		{"n+1 results", func(n int) int { return n + 1 }},
		{"n-1 results", func(n int) int { return n - 1 }},
		{"no results", func(int) int { return 0 }},
		{"2n results", func(n int) int { return 2 * n }},
	}
	commands := []struct {
		name string
		opts []server.Option
		args func(s *testServer) []string
		// want is on stdout with one result per node; result is a part of
		// a printed result, never on stdout with another count.
		want   func(s *testServer) []string
		result func(s *testServer) string
	}{
		{
			name: "get vars",
			args: func(s *testServer) []string { return []string{"get", "vars", "-n", s.ns.Objects().ID().String()} },
			want: func(s *testServer) []string { return []string{s.node("Int32"), "42", "Good"} },
			result: func(s *testServer) string {
				return s.node("Int32")
			},
		},
		{
			name: "get vars json",
			args: func(s *testServer) []string {
				return []string{"get", "vars", "-n", s.ns.Objects().ID().String(), "--format", "json"}
			},
			want:   func(s *testServer) []string { return []string{`"nodeId":"` + s.node("Int32") + `"`, `"value":42`} },
			result: func(s *testServer) string { return s.node("Int32") },
		},
		{
			name:   "get node",
			args:   func(s *testServer) []string { return []string{"get", "node", "-n", s.node("Int32")} },
			want:   func(s *testServer) []string { return []string{s.node("Int32"), "42"} },
			result: func(s *testServer) string { return s.node("Int32") },
		},
		{
			name:   "get attr",
			args:   func(s *testServer) []string { return []string{"get", "attr", "--all", "-n", s.node("Int32")} },
			want:   func(s *testServer) []string { return []string{"DataType", "42"} },
			result: func(s *testServer) string { return s.node("Int32") },
		},
		{
			name: "export values",
			args: func(s *testServer) []string {
				return []string{"export", "values", "-n", s.node("Int32"), "-n", s.node("String")}
			},
			want:   func(s *testServer) []string { return []string{s.node("Int32"), "42", "hello"} },
			result: func(s *testServer) string { return s.node("Int32") },
		},
		{
			name:   "pubsub config diagnostics",
			opts:   []server.Option{server.WithPubSub(nil)},
			args:   func(*testServer) []string { return []string{"pubsub", "config", "diagnostics"} },
			want:   func(*testServer) []string { return []string{"diagnostics PublishSubscribe", "COUNTER", "StateError"} },
			result: func(*testServer) string { return "diagnostics" },
		},
	}
	for _, cmd := range commands {
		for _, c := range counts {
			t.Run(cmd.name+", "+c.name, func(t *testing.T) {
				s := readResultsServer(t, c.results, cmd.opts...)
				stdout, stderr, err := runCLI(t, append(cmd.args(s), "-e", s.url)...)
				if c.name == "n results" {
					if err != nil {
						t.Fatalf("exit %d (%v)\nstderr: %s", ExitCode(err), err, stderr)
					}
					wantContains(t, stdout, cmd.want(s)...)
					return
				}
				if got := ExitCode(err); got != exitProtocol {
					t.Fatalf("exit %d, want %d (%v)\nstdout: %s\nstderr: %s", got, exitProtocol, err, stdout, stderr)
				}
				if !strings.Contains(stderr, "BadUnknownResponse") {
					t.Errorf("stderr does not name BadUnknownResponse:\n%s", stderr)
				}
				if strings.Contains(stdout, cmd.result(s)) {
					t.Errorf("a result is printed:\n%s", stdout)
				}
			})
		}
	}
}
