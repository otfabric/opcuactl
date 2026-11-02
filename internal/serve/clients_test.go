// SPDX-License-Identifier: MIT

package serve

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-opcua"
	"github.com/otfabric/go-opcua/server"
	"github.com/otfabric/go-opcua/ua"
)

// syncBuffer is a bytes.Buffer for concurrent writers.
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

func waitLog(t *testing.T, b *syncBuffer, want string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(b.String(), want) {
		if time.Now().After(deadline) {
			t.Fatalf("no %q in the client log:\n%s", want, b.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Config.Clients logs the connection, the session and its end of a client,
// and a request the server refused.
func TestClientLog(t *testing.T) {
	var out syncBuffer
	s := start(t, Config{Anonymous: true, Clients: slog.New(slog.NewTextHandler(&out, nil))})
	url := endpointURL(t, s, "opc.tcp")

	c := connect(t, url, opcua.SessionName("log test"), opcua.ApplicationName("log client"))
	waitLog(t, &out, "msg=\"session activated\"")
	if err := c.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitLog(t, &out, "msg=\"session closed\"")
	waitLog(t, &out, "msg=\"client disconnected\"")
	log := out.String()
	for _, want := range []string{"msg=\"client connected\" remote=127.0.0.1:", "name=\"log test\"", "app=\"log client\"",
		"identity=Anonymous", "policy=None", "reason=\"closed by client\""} {
		if !strings.Contains(log, want) {
			t.Errorf("no %q in the client log:\n%s", want, log)
		}
	}

	// A user name, which this server does not offer.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	bad, err := opcua.NewClient(url, opcua.InsecureNoSecurity(), opcua.AuthUsername("mallory", "x"), opcua.AuthPolicyID("anonymous_none"))
	if err != nil {
		t.Fatal(err)
	}
	if err := bad.Connect(ctx); err == nil {
		t.Fatal("a user name was accepted")
	}
	_ = bad.Close(ctx)
	waitLog(t, &out, "msg=\"client rejected\" service=ActivateSession")
	waitLog(t, &out, "user=mallory")
}

func TestClientLogHelpers(t *testing.T) {
	if addr(nil) != "" {
		t.Error("addr(nil)")
	}
	if got := policyName(ua.SecurityPolicyURIBasic256Sha256); got != "Basic256Sha256" {
		t.Errorf("policyName = %q", got)
	}
	if got := policyName("no-hash"); got != "no-hash" {
		t.Errorf("policyName = %q", got)
	}
	for tt, want := range map[ua.UserTokenType]string{
		ua.UserTokenTypeAnonymous:   "Anonymous",
		ua.UserTokenTypeUserName:    "UserName",
		ua.UserTokenTypeCertificate: "Certificate",
		ua.UserTokenTypeIssuedToken: "IssuedToken",
		ua.UserTokenType(99):        "Anonymous",
	} {
		if got := identityName(server.SessionInfo{IdentityType: tt}); got != want {
			t.Errorf("identityName(%v) = %q, want %q", tt, got, want)
		}
	}
	// A reactivation and a rejection without a session.
	var out syncBuffer
	l := clientLog{log: slog.New(slog.NewTextHandler(&out, nil))}
	l.OnSessionActivated(server.SessionInfo{Reactivated: true, Client: &ua.ApplicationDescription{}})
	l.OnRejected(server.Rejection{Service: server.RejectedOpenSecureChannel, Status: ua.StatusBadCertificateUntrusted})
	for _, want := range []string{"session reactivated", "service=OpenSecureChannel", "BadCertificateUntrusted"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("no %q in %s", want, out.String())
		}
	}
}
