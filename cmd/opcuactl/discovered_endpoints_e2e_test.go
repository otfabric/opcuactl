// SPDX-License-Identifier: MIT

package opcuactl

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/otfabric/go-opcua/id"
	"github.com/otfabric/go-opcua/server"
	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/go-opcua/uasc"
)

// sessionRecorder counts the ActivateSession requests a server receives
// (ServerMetrics.OnRequest runs before the handler) and records the
// security mode of every session it activated.
type sessionRecorder struct {
	server.BaseSessionObserver
	activateRequests atomic.Int32
	mu               sync.Mutex
	modes            []ua.MessageSecurityMode
}

func (r *sessionRecorder) OnRequest(service string) {
	if service == "ActivateSession" {
		r.activateRequests.Add(1)
	}
}
func (r *sessionRecorder) OnResponse(string, time.Duration)     {}
func (r *sessionRecorder) OnError(string, time.Duration, error) {}

func (r *sessionRecorder) OnSessionActivated(info server.SessionInfo) {
	r.mu.Lock()
	r.modes = append(r.modes, info.SecurityMode)
	r.mu.Unlock()
}

// reset forgets what was recorded and returns it.
func (r *sessionRecorder) reset() (activateRequests int32, modes []ua.MessageSecurityMode) {
	r.mu.Lock()
	defer r.mu.Unlock()
	modes, r.modes = r.modes, nil
	return r.activateRequests.Swap(0), modes
}

// endpointsServer is a server with Basic256Sha256 Sign and SignAndEncrypt
// endpoints (and None). With partial set, its GetEndpoints answer leaves out
// the SignAndEncrypt endpoints, while CreateSession lists all of them.
func endpointsServer(t *testing.T, partial bool, opts ...server.Option) (*testServer, *sessionRecorder, *identity) {
	t.Helper()
	srvID := newIdentity(t, "rsa", serverURI)
	rec := &sessionRecorder{}
	all := append([]server.Option{
		server.ApplicationIdentity(srvID.der, srvID.key),
		server.EnableSecurity("Basic256Sha256", ua.MessageSecurityModeSign),
		server.EnableSecurity("Basic256Sha256", ua.MessageSecurityModeSignAndEncrypt),
		server.WithMetrics(rec),
		server.WithSessionObserver(rec),
	}, opts...)
	s := startServerWith(t, func(srv *server.Server) {
		if !partial {
			return
		}
		srv.RegisterHandler(id.GetEndpointsRequestEncodingDefaultBinary, func(_ context.Context, _ *uasc.SecureChannel, r ua.Request, _ uint32) (ua.Response, error) {
			req := r.(*ua.GetEndpointsRequest)
			var eps []*ua.EndpointDescription
			for _, ep := range srv.Endpoints() {
				if ep.SecurityMode != ua.MessageSecurityModeSignAndEncrypt {
					eps = append(eps, ep)
				}
			}
			return &ua.GetEndpointsResponse{
				ResponseHeader: &ua.ResponseHeader{
					Timestamp: time.Now(), RequestHandle: req.RequestHeader.RequestHandle,
					ServiceDiagnostics: &ua.DiagnosticInfo{}, StringTable: []string{}, AdditionalHeader: ua.NewExtensionObject(nil),
				},
				Endpoints: eps,
			}, nil
		})
	}, all...)
	return s, rec, srvID
}

// The endpoints of GetEndpoints, which auto and an explicit policy without
// --server-cert select from, are compared with those of CreateSession: a
// server whose lists differ is refused with exit 4 before any session is
// activated, and --server-cert with an explicit policy and mode does not
// depend on GetEndpoints.
func TestDiscoveredEndpointsCompared(t *testing.T) {
	cli := newIdentity(t, "rsa", clientURI)
	for _, partial := range []bool{true, false} {
		s, rec, srvID := endpointsServer(t, partial)
		common := []string{"get", "value", "-e", s.url, "-n", s.node("Int32"), "-q",
			"--cert", cli.certFile, "--key", cli.keyFile, "--trusted-cert", srvID.certFile}
		for _, c := range []struct {
			name string
			args []string
			mode ua.MessageSecurityMode // of the session when the lists agree
		}{
			{"auto", []string{"--security-policy", "auto", "--security-mode", "auto"}, ua.MessageSecurityModeSignAndEncrypt},
			{"policy and mode without --server-cert", []string{"--security-policy", "Basic256Sha256", "--security-mode", "Sign"}, ua.MessageSecurityModeSign},
		} {
			name := c.name + ", the same endpoints"
			if partial {
				name = c.name + ", GetEndpoints without SignAndEncrypt"
			}
			t.Run(name, func(t *testing.T) {
				rec.reset()
				stdout, stderr, err := runCLI(t, append(common, c.args...)...)
				requests, modes := rec.reset()
				if !partial {
					if err != nil || strings.TrimSpace(stdout) != "42" {
						t.Fatalf("exit %d (%v)\nstdout: %s\nstderr: %s", ExitCode(err), err, stdout, stderr)
					}
					if len(modes) != 1 || modes[0] != c.mode {
						t.Errorf("activated sessions %v, want one with %s", modes, c.mode)
					}
					return
				}
				if got := ExitCode(err); got != exitTransport {
					t.Fatalf("exit %d, want %d (%v)\nstdout: %s\nstderr: %s\nactivated sessions: %v", got, exitTransport, err, stdout, stderr, modes)
				}
				for _, w := range []string{"BadSecurityChecksFailed", "the endpoints of GetEndpoints and CreateSession differ", "--server-cert"} {
					if !strings.Contains(stderr, w) {
						t.Errorf("stderr does not contain %q:\n%s", w, stderr)
					}
				}
				if requests != 0 || len(modes) != 0 {
					t.Errorf("%d ActivateSession requests, activated sessions %v; want none", requests, modes)
				}
				if stdout != "" {
					t.Errorf("stdout %q", stdout)
				}
			})
		}
		if !partial {
			continue
		}
		t.Run("--server-cert with policy and mode, GetEndpoints without SignAndEncrypt", func(t *testing.T) {
			rec.reset()
			out := mustRun(t, append(common, "--security-policy", "Basic256Sha256", "--security-mode", "SignAndEncrypt",
				"--server-cert", srvID.certFile)...)
			if strings.TrimSpace(out) != "42" {
				t.Errorf("stdout %q", out)
			}
			if _, modes := rec.reset(); len(modes) != 1 || modes[0] != ua.MessageSecurityModeSignAndEncrypt {
				t.Errorf("activated sessions %v, want one with SignAndEncrypt", modes)
			}
		})
	}
}

// With --reverse-listen, auto selects from the GetEndpoints answer of a
// reverse connection, and the list is compared in the same way.
func TestDiscoveredEndpointsComparedReverse(t *testing.T) {
	cli := newIdentity(t, "rsa", clientURI)
	for _, partial := range []bool{true, false} {
		listen := freeAddr(t)
		_, rec, srvID := endpointsServer(t, partial, server.ApplicationURI(serverURI),
			server.WithReverseConnect("opc.tcp://"+listen,
				server.ReverseConnectRetryInterval(50*time.Millisecond, 200*time.Millisecond)))
		stdout, stderr, err := runCLI(t, "get", "value", "-n", "i=2258", "-q", "--reverse-listen", listen, "--timeout", "30s",
			"--security-policy", "auto", "--security-mode", "auto",
			"--cert", cli.certFile, "--key", cli.keyFile, "--trusted-cert", srvID.certFile)
		requests, modes := rec.reset()
		if !partial {
			if err != nil || strings.TrimSpace(stdout) == "" {
				t.Errorf("the same endpoints: exit %d (%v)\nstderr: %s", ExitCode(err), err, stderr)
			}
			if len(modes) != 1 || modes[0] != ua.MessageSecurityModeSignAndEncrypt {
				t.Errorf("the same endpoints: activated sessions %v, want one with SignAndEncrypt", modes)
			}
			continue
		}
		if got := ExitCode(err); got != exitTransport || !strings.Contains(stderr, "BadSecurityChecksFailed") {
			t.Errorf("GetEndpoints without SignAndEncrypt: exit %d, want %d (%v)\nstderr: %s", got, exitTransport, err, stderr)
		}
		if requests != 0 || len(modes) != 0 {
			t.Errorf("GetEndpoints without SignAndEncrypt: %d ActivateSession requests, activated sessions %v; want none", requests, modes)
		}
	}
}

// README's recommended production pattern says that auto selects from
// GetEndpoints and that the list is compared with CreateSession.
func TestReadmeNamesEndpointComparison(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	var para string
	for _, p := range strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n\n") {
		if strings.HasPrefix(p, "**Recommended production pattern:**") {
			para = p
		}
	}
	if para == "" {
		t.Fatal("README has no recommended production pattern")
	}
	for _, w := range []string{"GetEndpoints", "CreateSession", "compares", "BadSecurityChecksFailed", "--server-cert"} {
		if !strings.Contains(para, w) {
			t.Errorf("the recommended production pattern does not name %q:\n%s", w, para)
		}
	}
}
