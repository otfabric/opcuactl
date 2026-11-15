// SPDX-License-Identifier: MIT

package opcuactl

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-opcua/id"
	"github.com/otfabric/go-opcua/server"
	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/go-opcua/uasc"
)

// writeResultsServer is a server whose Write handler answers with n results,
// each Good, whatever was asked.
func writeResultsServer(t *testing.T, n int) *testServer {
	t.Helper()
	return startServerWith(t, func(srv *server.Server) {
		srv.RegisterHandler(id.WriteRequestEncodingDefaultBinary, func(_ context.Context, _ *uasc.SecureChannel, r ua.Request, _ uint32) (ua.Response, error) {
			req := r.(*ua.WriteRequest)
			return &ua.WriteResponse{
				ResponseHeader: &ua.ResponseHeader{
					Timestamp: time.Now(), RequestHandle: req.RequestHeader.RequestHandle,
					ServiceDiagnostics: &ua.DiagnosticInfo{}, StringTable: []string{}, AdditionalHeader: ua.NewExtensionObject(nil),
				},
				Results: make([]ua.StatusCode, n),
			}, nil
		})
	})
}

// write value reports the status of its one write only when the server
// answered with exactly one result.
func TestWriteValueResultCount(t *testing.T) {
	for _, c := range []struct {
		results int
		code    int
		stderr  string
	}{
		{0, exitProtocol, "0 results for 1 write"},
		{1, exitOK, ""},
		{2, exitProtocol, "2 results for 1 write"},
	} {
		t.Run(fmt.Sprintf("%d results", c.results), func(t *testing.T) {
			s := writeResultsServer(t, c.results)
			stdout, stderr, err := runCLI(t, "write", "value", "-e", s.url, "-n", s.node("Int32"),
				"--value", "5", "--type", "int32", "--yes")
			if got := ExitCode(err); got != c.code {
				t.Fatalf("exit %d, want %d (%v)\nstdout: %s\nstderr: %s", got, c.code, err, stdout, stderr)
			}
			if c.code == exitOK {
				wantContains(t, stdout, "Good")
				return
			}
			if !strings.Contains(stderr, c.stderr) {
				t.Errorf("stderr %q, want %q", stderr, c.stderr)
			}
			if strings.Contains(stdout, "Good") {
				t.Errorf("a write without its one result is reported:\n%s", stdout)
			}
		})
	}
}
