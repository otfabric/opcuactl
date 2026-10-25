// SPDX-License-Identifier: MIT

package opcuactl

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/otfabric/go-opcua/gds/gdsserver"
	"github.com/otfabric/go-opcua/server"
	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/opcuactl/internal/model"
)

const (
	gdsCAAdmin    = "gdsadmin"
	gdsCAAdminPwd = "gds-secret"
)

// gdsPullServer is a server with a Global Discovery Server whose requests
// an approver goroutine approves while approve is set.
type gdsPullServer struct {
	*testServer
	gds     *gdsserver.GDS
	approve atomic.Bool
}

func startGDSPullServer(t *testing.T, srvID *identity) *gdsPullServer {
	t.Helper()
	p := &gdsPullServer{}
	var g atomic.Pointer[gdsserver.GDS]
	s := startServerWith(t, func(srv *server.Server) {
		gd, err := gdsserver.Attach(srv, gdsserver.Config{})
		if err != nil {
			t.Fatalf("gdsserver.Attach: %v", err)
		}
		g.Store(gd)
		p.gds = gd
	},
		server.ApplicationIdentity(srvID.der, srvID.key),
		server.EnableSecurity("Basic256Sha256", ua.MessageSecurityModeSignAndEncrypt),
		server.EnableAuthMode(ua.UserTokenTypeUserName),
		server.WithUsernameValidator(func(user, pass string) error {
			if user == gdsCAAdmin && pass == gdsCAAdminPwd {
				return nil
			}
			return errors.New("denied")
		}),
		server.WithRoleMapper(func(tok ua.IdentityToken) []*ua.NodeID {
			if u, ok := tok.(*ua.UserNameIdentityToken); ok && u.UserName == gdsCAAdmin {
				return []*ua.NodeID{ua.RoleAuthenticatedUser.NodeID(), g.Load().DiscoveryAdminRole(), g.Load().CertificateAuthorityAdminRole()}
			}
			return server.DefaultRoleMapper(tok)
		}),
	)
	p.testServer = s
	t.Cleanup(p.gds.Close)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	t.Cleanup(func() { cancel(); <-done })
	go func() {
		defer close(done)
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(20 * time.Millisecond):
			}
			if !p.approve.Load() {
				continue
			}
			for _, r := range p.gds.Requests() {
				if r.State == gdsserver.RequestPending {
					_ = p.gds.ApproveRequest(ctx, r.ID, true)
				}
			}
		}
	}()
	return p
}

func TestE2E_GDSPull(t *testing.T) {
	srvID := newIdentity(t, "rsa", serverURI)
	cli := newIdentity(t, "rsa", clientURI)
	s := startGDSPullServer(t, srvID)
	admin := []string{"-e", s.url, "--security-policy", "Basic256Sha256", "--security-mode", "SignAndEncrypt",
		"--cert", cli.certFile, "--key", cli.keyFile, "--trusted-cert", srvID.certFile,
		"--auth", "username", "--username", gdsCAAdmin, "--password", gdsCAAdminPwd}
	open := []string{"-e", s.url}
	anon := admin[:len(admin)-6]
	run := func(t *testing.T, args ...string) string {
		t.Helper()
		return mustRun(t, append(args, admin...)...)
	}
	fail := func(t *testing.T, code int, wantStderr string, args ...string) {
		t.Helper()
		_, errOut, err := runCLI(t, args...)
		if err == nil {
			t.Fatalf("opcuactl %s succeeded", strings.Join(args, " "))
		}
		if ExitCode(err) != code {
			t.Errorf("opcuactl %s: exit %d, want %d (%s)", strings.Join(args, " "), ExitCode(err), code, errOut)
		}
		if !strings.Contains(errOut, wantStderr) {
			t.Errorf("opcuactl %s: stderr does not contain %q:\n%s", strings.Join(args, " "), wantStderr, errOut)
		}
	}
	const appURI = "urn:otfabric:opcuactl:test:line4"
	var appID string
	dir := t.TempDir()

	t.Run("register", func(t *testing.T) {
		fail(t, 3, "SignAndEncrypt", append([]string{"gds", "pull", "register", "--uri", appURI}, open...)...)
		fail(t, 3, "DiscoveryAdmin", append([]string{"gds", "pull", "register", "--uri", appURI}, anon...)...)
		fail(t, 1, "--uri is required", append([]string{"gds", "pull", "register"}, admin...)...)
		fail(t, 1, "invalid --type", append([]string{"gds", "pull", "register", "--uri", appURI, "--type", "plc"}, admin...)...)
		var a model.GDSApplication
		if err := json.Unmarshal([]byte(run(t, "gds", "pull", "register", "--uri", appURI, "--name", "en:Line 4", "--name", "Linie 4",
			"--discovery-url", "opc.tcp://line4:4840", "--capability", "DA", "--product-uri", "urn:vendor:plc", "--format", "json")), &a); err != nil {
			t.Fatal(err)
		}
		if a.ApplicationID == "" || a.ApplicationURI != appURI || a.ApplicationType != "server" || len(a.ApplicationNames) != 2 {
			t.Fatalf("registered = %+v", a)
		}
		appID = a.ApplicationID
		wantContains(t, run(t, "gds", "pull", "register", "--uri", "urn:otfabric:opcuactl:test:hmi", "--type", "client"), "Registered urn:otfabric:opcuactl:test:hmi: ApplicationId")
		if out := strings.TrimSpace(run(t, "gds", "pull", "register", "--uri", "urn:otfabric:opcuactl:test:q", "--type", "client", "-q")); !strings.Contains(out, "ns=") {
			t.Errorf("quiet register = %q", out)
		}
	})

	t.Run("find, get, query", func(t *testing.T) {
		out := mustRun(t, append([]string{"gds", "pull", "find", appURI, "-v"}, open...)...)
		wantContains(t, out, "APPLICATION ID", appID, appURI, "Line 4", "server", "opc.tcp://line4:4840", "DA")
		if out := mustRun(t, append([]string{"gds", "pull", "find", appURI, "-q"}, open...)...); strings.TrimSpace(out) != appID {
			t.Errorf("find -q = %q", out)
		}
		var none []model.GDSApplication
		if err := json.Unmarshal([]byte(mustRun(t, append([]string{"gds", "pull", "find", "urn:nobody", "--format", "json"}, open...)...)), &none); err != nil || len(none) != 0 {
			t.Errorf("find urn:nobody = %v %v", none, err)
		}

		out = run(t, "gds", "pull", "get", appID)
		wantContains(t, out, "Application URI:", appURI, "urn:vendor:plc")
		wantContains(t, run(t, "gds", "pull", "get", appID, "-q"), appURI)
		fail(t, 1, "invalid ApplicationId", append([]string{"gds", "pull", "get", "nonsense"}, admin...)...)
		fail(t, 3, "GetApplication", append([]string{"gds", "pull", "get", "ns=1;g=00000000-0000-0000-0000-000000000001"}, admin...)...)

		out = mustRun(t, append([]string{"gds", "pull", "query"}, open...)...)
		wantContains(t, out, "APPLICATION URI", appURI, "urn:otfabric:opcuactl:test:hmi")
		var q model.GDSApplicationQuery
		if err := json.Unmarshal([]byte(mustRun(t, append([]string{"gds", "pull", "query", "--type", "client", "--format", "json"}, open...)...)), &q); err != nil {
			t.Fatal(err)
		}
		for _, a := range q.Applications {
			if a.ApplicationURI == appURI {
				t.Errorf("--type client returned the server: %+v", q)
			}
		}
		wantContains(t, mustRun(t, append([]string{"gds", "pull", "query", "--uri", "%line4", "-q"}, open...)...), appURI)
		_, errOut, err := runCLI(t, append([]string{"gds", "pull", "query", "--max", "1"}, open...)...)
		if err != nil || !strings.Contains(errOut, "more applications") {
			t.Errorf("query --max 1: %v %s", err, errOut)
		}
		fail(t, 1, "invalid --type", append([]string{"gds", "pull", "query", "--type", "x"}, open...)...)

		out = mustRun(t, append([]string{"gds", "pull", "query-servers"}, open...)...)
		wantContains(t, out, "DISCOVERY URL", "opc.tcp://line4:4840", "Line 4")
		var sq model.GDSServerQuery
		if err := json.Unmarshal([]byte(mustRun(t, append([]string{"gds", "pull", "query-servers", "--capability", "DA", "--format", "json"}, open...)...)), &sq); err != nil {
			t.Fatal(err)
		}
		if len(sq.Servers) != 1 || sq.Servers[0].DiscoveryURL != "opc.tcp://line4:4840" {
			t.Errorf("query-servers = %+v", sq)
		}
		wantContains(t, mustRun(t, append([]string{"gds", "pull", "query-servers", "-q"}, open...)...), "opc.tcp://line4:4840")
	})

	t.Run("update", func(t *testing.T) {
		wantContains(t, run(t, "gds", "pull", "update", appID, "--discovery-url", "opc.tcp://line4.plant:4840"), "Updated "+appID)
		var a model.GDSApplication
		if err := json.Unmarshal([]byte(run(t, "gds", "pull", "get", appID, "--format", "json")), &a); err != nil {
			t.Fatal(err)
		}
		if len(a.DiscoveryURLs) != 1 || a.DiscoveryURLs[0] != "opc.tcp://line4.plant:4840" || a.ProductURI != "urn:vendor:plc" || len(a.ApplicationNames) != 2 {
			t.Errorf("updated = %+v", a)
		}
		var b model.GDSApplication
		if err := json.Unmarshal([]byte(run(t, "gds", "pull", "update", appID, "--product-uri", "urn:vendor:plc2", "--format", "json")), &b); err != nil || b.ProductURI != "urn:vendor:plc2" {
			t.Errorf("update json = %+v %v", b, err)
		}
		fail(t, 3, "DiscoveryAdmin", append([]string{"gds", "pull", "update", appID, "--uri", "urn:other"}, anon...)...)
	})

	var issuedFile string
	t.Run("request a CSR, finish pending, approve", func(t *testing.T) {
		key, _ := rsa.GenerateKey(rand.Reader, 2048)
		u, _ := url.Parse(appURI)
		csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: "Line 4"}, URIs: []*url.URL{u}, DNSNames: []string{"line4"}}, key)
		if err != nil {
			t.Fatal(err)
		}
		csrFile := filepath.Join(dir, "line4.csr")
		writePEM(t, csrFile, "CERTIFICATE REQUEST", csrDER)
		var req model.GDSRequest
		if err := json.Unmarshal([]byte(run(t, "gds", "pull", "request", appID, "--csr", csrFile, "--type", "rsa", "--format", "json")), &req); err != nil {
			t.Fatal(err)
		}
		if req.RequestID == "" || req.Kind != "signing-request" || req.ApplicationID != appID {
			t.Fatalf("request = %+v", req)
		}
		fail(t, 3, "waits for approval", append([]string{"gds", "pull", "finish", appID, req.RequestID}, admin...)...)
		if err := s.gds.ApproveRequest(context.Background(), s.gds.RequestID(mustNodeID(t, req.RequestID)), true); err != nil {
			t.Fatal(err)
		}
		issuedFile = filepath.Join(dir, "line4.pem")
		out := run(t, "gds", "pull", "finish", appID, req.RequestID, "--cert-out", issuedFile, "--issuers-out", filepath.Join(dir, "ca.pem"))
		wantContains(t, out, "Certificate issued", "CN=Line 4", "Certificate file:", "Issuers file:")
		certs, err := readCerts(issuedFile)
		if err != nil || !uapolicyKeyMatches(key, certs[0]) {
			t.Fatalf("issued certificate: %v", err)
		}
		if _, err := os.Stat(filepath.Join(dir, "line4.key")); err == nil {
			t.Error("a key file was written for a signing request")
		}
		fail(t, 3, "FinishRequest", append([]string{"gds", "pull", "finish", appID, req.RequestID}, admin...)...)
		fail(t, 1, "invalid RequestId", append([]string{"gds", "pull", "finish", appID, "x y"}, admin...)...)
	})

	s.approve.Store(true)

	t.Run("new key pair, wait", func(t *testing.T) {
		reqID := strings.TrimSpace(run(t, "gds", "pull", "request", appID, "--subject", "CN=Line 4 new", "--dns", "line4", "-q"))
		certFile := filepath.Join(dir, "new.der")
		var res model.GDSIssued
		if err := json.Unmarshal([]byte(run(t, "gds", "pull", "wait", appID, reqID, "--cert-out", certFile, "--poll", "50ms", "--format", "json")), &res); err != nil {
			t.Fatal(err)
		}
		if res.KeyFile != filepath.Join(dir, "new.key") || res.CertFile != certFile || res.Certificate.Subject != "CN=Line 4 new" || len(res.Issuers) != 1 {
			t.Fatalf("wait = %+v", res)
		}
		st, err := os.Stat(res.KeyFile)
		if err != nil || st.Mode().Perm() != 0o600 {
			t.Fatalf("key file: %v %v", err, st)
		}
		der, _ := os.ReadFile(certFile)
		key, err := readSigner(res.KeyFile)
		if err != nil || !uapolicyKeyMatches(key, der) {
			t.Fatalf("the key does not match: %v", err)
		}

		// Without --cert-out the certificate and key are printed.
		reqID = strings.TrimSpace(run(t, "gds", "pull", "request", appID, "-q"))
		out, errOut, err := runCLI(t, append([]string{"gds", "pull", "wait", appID, reqID, "--poll", "50ms"}, admin...)...)
		if err != nil {
			t.Fatal(err)
		}
		wantContains(t, out, "BEGIN CERTIFICATE", "PRIVATE KEY")
		wantContains(t, errOut, "--key-out")

		fail(t, 1, "--key-password is for a key pair", append([]string{"gds", "pull", "request", appID, "--csr", filepath.Join(dir, "line4.csr"), "--key-password", "x"}, admin...)...)
		fail(t, 1, "exclude each other", append([]string{"gds", "pull", "request", appID, "--csr", "a", "--private-key", "b"}, admin...)...)
		fail(t, 1, "no such file", append([]string{"gds", "pull", "request", appID, "--csr", filepath.Join(dir, "missing.csr")}, admin...)...)
		fail(t, 1, "invalid --group", append([]string{"gds", "pull", "request", appID, "--group", "x y"}, admin...)...)
		fail(t, 1, "invalid --type", append([]string{"gds", "pull", "request", appID, "--type", "dsa"}, admin...)...)
		fail(t, 3, "DiscoveryAdmin", append([]string{"gds", "pull", "request", appID}, anon...)...)
	})

	t.Run("request from a key", func(t *testing.T) {
		keyFile := cli.keyFile
		var req model.GDSRequest
		if err := json.Unmarshal([]byte(run(t, "gds", "pull", "request", appID, "--private-key", keyFile, "--subject", "CN=From key", "--format", "json")), &req); err != nil {
			t.Fatal(err)
		}
		out := run(t, "gds", "pull", "wait", appID, req.RequestID, "--poll", "50ms", "--cert-out", filepath.Join(dir, "fromkey.pem"), "-q")
		if len(strings.TrimSpace(out)) != 40 {
			t.Errorf("wait -q = %q", out)
		}
		wantContains(t, run(t, "gds", "pull", "request", appID, "--private-key", keyFile), "gds pull wait")
	})

	t.Run("trust list, groups, certs, status", func(t *testing.T) {
		pki := filepath.Join(dir, "pki")
		out := run(t, "gds", "pull", "trustlist", appID, "--out", pki)
		wantContains(t, out, "trusted", "go-opcua GDS CA")
		files, err := os.ReadDir(filepath.Join(pki, "trusted", "certs"))
		if err != nil || len(files) == 0 {
			t.Fatalf("trust list files: %v", err)
		}
		out = run(t, "gds", "pull", "groups", appID)
		group := strings.TrimSpace(strings.Split(out, "\n")[0])
		if !strings.Contains(group, "ns=") {
			t.Fatalf("groups = %q", out)
		}
		wantContains(t, run(t, "gds", "pull", "groups", appID, "--format", "json"), group)
		run(t, "gds", "pull", "trustlist", appID, "--group", group)

		var entries []model.GDSCertificate
		if err := json.Unmarshal([]byte(run(t, "gds", "pull", "certs", appID, "--format", "json")), &entries); err != nil {
			t.Fatal(err)
		}
		if len(entries) < 3 || entries[0].CertificateType != "rsa" {
			t.Errorf("certs = %+v", entries)
		}
		certsDir := filepath.Join(dir, "certs")
		wantContains(t, run(t, "gds", "pull", "certs", appID, "--group", group, "--out", certsDir), "rsa", group)

		// Without --type every certificate type counts, and the
		// application has only an RSA certificate.
		wantContains(t, run(t, "gds", "pull", "status", appID), "Update required")
		var st model.GDSCertificateStatus
		if err := json.Unmarshal([]byte(run(t, "gds", "pull", "status", appID, "--type", "rsa", "--format", "json")), &st); err != nil || st.UpdateRequired || st.CertificateType != "rsa" {
			t.Errorf("status = %+v %v", st, err)
		}
		wantContains(t, run(t, "gds", "pull", "status", appID, "--group", group, "--type", "rsa", "-q"), "false")
		fail(t, 1, "invalid --type", append([]string{"gds", "pull", "status", appID, "--type", "dsa"}, admin...)...)
	})

	t.Run("revoke and check", func(t *testing.T) {
		out := run(t, "gds", "pull", "check-revocation", issuedFile)
		wantContains(t, out, "Status:", "Good")
		var rs model.GDSRevocationStatus
		if err := json.Unmarshal([]byte(run(t, "gds", "pull", "check-revocation", issuedFile, "--format", "json")), &rs); err != nil || rs.Status != "Good" {
			t.Errorf("check = %+v %v", rs, err)
		}
		wantContains(t, run(t, "gds", "pull", "revoke", appID, issuedFile, "--yes"), "Revoked")
		_, _, err := runCLI(t, append([]string{"gds", "pull", "check-revocation", issuedFile, "-q"}, admin...)...)
		if ExitCode(err) != 4 {
			t.Errorf("check of a revoked certificate: exit %d (%v)", ExitCode(err), err)
		}
		wantContains(t, run(t, "gds", "pull", "revoke", appID, filepath.Join(dir, "fromkey.pem"), "--yes", "--format", "json"), `"action":"revoke"`)
		fail(t, 1, "not a certificate", append([]string{"gds", "pull", "revoke", appID, cli.keyFile, "--yes"}, admin...)...)
		fail(t, 1, "not a certificate", append([]string{"gds", "pull", "check-revocation", cli.keyFile}, admin...)...)
	})

	t.Run("enroll", func(t *testing.T) {
		certFile := filepath.Join(dir, "hmi.pem")
		trust := filepath.Join(dir, "hmi-pki")
		out := run(t, "gds", "pull", "enroll", "--uri", "urn:otfabric:opcuactl:test:panel", "--type", "client", "--name", "Panel",
			"--new-key", "ecc-p256", "--cert-type", "ecc-p256", "--cert-out", certFile, "--trust-out", trust, "--poll", "50ms")
		wantContains(t, out, "Enrolled", "Key file:", "Trust list entries:")
		key, err := readSigner(filepath.Join(dir, "hmi.key"))
		if err != nil {
			t.Fatal(err)
		}
		certs, err := readCerts(certFile)
		if err != nil || !uapolicyKeyMatches(key, certs[0]) {
			t.Fatalf("enrolled certificate: %v", err)
		}
		if c := mustParse(t, certs[0]); len(c.URIs) != 1 || c.URIs[0].String() != "urn:otfabric:opcuactl:test:panel" {
			t.Errorf("enrolled URIs = %v", c.URIs)
		}
		if files, err := os.ReadDir(filepath.Join(trust, "trusted", "certs")); err != nil || len(files) == 0 {
			t.Errorf("trust list: %v", err)
		}

		// An application the GDS knows, with a key of its own; the GDS
		// generates the key for an enrollment without one.
		var res model.GDSIssued
		if err := json.Unmarshal([]byte(run(t, "gds", "pull", "enroll", "--application-id", appID, "--private-key", cli.keyFile,
			"--cert-out", filepath.Join(dir, "again.der"), "--poll", "50ms", "--format", "json")), &res); err != nil {
			t.Fatal(err)
		}
		if res.ApplicationID != appID || res.KeyFile != "" || len(res.TrustList) == 0 {
			t.Errorf("enroll --application-id = %+v", res)
		}
		if err := json.Unmarshal([]byte(run(t, "gds", "pull", "enroll", "--uri", appURI, "--cert-out", filepath.Join(dir, "gen.pem"),
			"--key-out", filepath.Join(dir, "gen-key.pem"), "--poll", "50ms", "--format", "json")), &res); err != nil {
			t.Fatal(err)
		}
		if res.ApplicationID != appID || res.KeyFile != filepath.Join(dir, "gen-key.pem") {
			t.Errorf("enroll --uri of a registered application = %+v", res)
		}

		fail(t, 1, "--cert-out is required", append([]string{"gds", "pull", "enroll", "--uri", appURI}, admin...)...)
		fail(t, 1, "exclude each other", append([]string{"gds", "pull", "enroll", "--uri", appURI, "--cert-out", "x", "--private-key", "a", "--new-key", "rsa"}, admin...)...)
		fail(t, 1, "invalid --new-key", append([]string{"gds", "pull", "enroll", "--uri", appURI, "--cert-out", "x", "--new-key", "dsa"}, admin...)...)
		fail(t, 1, "--uri is required", append([]string{"gds", "pull", "enroll", "--cert-out", "x"}, admin...)...)
		fail(t, 1, "invalid --application-id", append([]string{"gds", "pull", "enroll", "--application-id", "x y", "--cert-out", "x"}, admin...)...)
		fail(t, 3, "DiscoveryAdmin", append([]string{"gds", "pull", "enroll", "--uri", "urn:new", "--cert-out", filepath.Join(dir, "x.pem")}, anon...)...)
	})

	t.Run("unregister", func(t *testing.T) {
		wantContains(t, run(t, "gds", "pull", "unregister", appID, "--yes"), "UnregisterApplication: done")
		var none []model.GDSApplication
		if err := json.Unmarshal([]byte(mustRun(t, append([]string{"gds", "pull", "find", appURI, "--format", "json"}, open...)...)), &none); err != nil || len(none) != 0 {
			t.Errorf("after unregister: %v %v", none, err)
		}
		fail(t, 3, "UnregisterApplication", append([]string{"gds", "pull", "unregister", appID, "--yes"}, admin...)...)
	})
}

// TestE2E_GDSPullNotAGDS names a server without a GDS.
func TestE2E_GDSPullNotAGDS(t *testing.T) {
	s := startServer(t)
	_, errOut, err := runCLI(t, "gds", "pull", "find", "urn:x", "-e", s.url)
	if ExitCode(err) != 3 {
		t.Fatalf("exit %d (%v)", ExitCode(err), err)
	}
	wantContains(t, errOut, "not a Global Discovery Server")
}

func mustNodeID(t *testing.T, s string) *ua.NodeID {
	t.Helper()
	n, err := ua.ParseNodeID(s)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func uapolicyKeyMatches(key any, der []byte) bool {
	c, err := x509.ParseCertificate(der)
	if err != nil {
		return false
	}
	k, ok := key.(interface{ Public() crypto.PublicKey })
	if !ok {
		return false
	}
	pub, ok := k.Public().(interface{ Equal(crypto.PublicKey) bool })
	return ok && pub.Equal(c.PublicKey)
}
