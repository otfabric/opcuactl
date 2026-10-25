// SPDX-License-Identifier: MIT

package opcuactl

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/otfabric/go-opcua/gds"
	"github.com/otfabric/go-opcua/server"
	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/go-opcua/uapolicy"
	"github.com/otfabric/opcuactl/internal/model"
)

// gdsAdminArgs connects as the SecurityAdmin of startGDSServerWith.
func gdsAdminArgs(s *testServer, srvID, cli *identity, trusted ...string) []string {
	args := []string{"-e", s.url, "--security-policy", "Basic256Sha256", "--security-mode", "SignAndEncrypt",
		"--cert", cli.certFile, "--key", cli.keyFile, "--trusted-cert", srvID.certFile,
		"--auth", "username", "--username", gdsAdmin, "--password", gdsAdminPwd}
	for _, f := range trusted {
		args = append(args, "--trusted-cert", f)
	}
	return args
}

// startGDSServerWith is startGDSServer with a PFX password.
func startGDSServerWith(t *testing.T, srvID, cli *identity, pfxPassword string) *testServer {
	t.Helper()
	return startServer(t,
		server.ApplicationIdentity(srvID.der, srvID.key),
		server.EnableSecurity("Basic256Sha256", ua.MessageSecurityModeSignAndEncrypt),
		server.EnableAuthMode(ua.UserTokenTypeUserName),
		server.WithUsernameValidator(func(user, pass string) error {
			if user == gdsAdmin && pass == gdsAdminPwd {
				return nil
			}
			return errors.New("denied")
		}),
		server.WithRoleMapper(func(tok ua.IdentityToken) []*ua.NodeID {
			return []*ua.NodeID{ua.RoleAuthenticatedUser.NodeID(), ua.RoleSecurityAdmin.NodeID()}
		}),
		server.WithPushManagement(server.PushManagement{
			TrustList: &ua.TrustListDataType{TrustedCertificates: [][]byte{cli.der}},
			PFXPassword: func(context.Context, *ua.NodeID, *ua.NodeID) (string, error) {
				return pfxPassword, nil
			},
		}),
	)
}

func TestE2E_GDSConfiguration(t *testing.T) {
	srvID := newIdentity(t, "rsa", serverURI)
	cli := newIdentity(t, "rsa", clientURI)
	ca := newTestCA(t)
	s := startGDSServerWith(t, srvID, cli, "pfx-secret")
	admin := gdsAdminArgs(s, srvID, cli)
	keep := t.TempDir() // files the later subtests use
	run := func(t *testing.T, args ...string) string {
		t.Helper()
		return mustRun(t, append(args, admin...)...)
	}
	fail := func(t *testing.T, code int, wantStderr string, args ...string) {
		t.Helper()
		_, errOut, err := runCLI(t, append(args, admin...)...)
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

	t.Run("groups", func(t *testing.T) {
		out := run(t, "gds", "groups")
		wantContains(t, out, "NAME", "DefaultApplicationGroup", "DefaultHttpsGroup", "DefaultUserTokenGroup", "i=14156", "rsa")
		var groups []model.GDSCertificateGroup
		if err := json.Unmarshal([]byte(run(t, "gds", "groups", "--format", "json")), &groups); err != nil {
			t.Fatal(err)
		}
		if len(groups) != 3 || groups[0].TrustList == "" {
			t.Errorf("groups = %+v", groups)
		}
		wantContains(t, run(t, "gds", "groups", "-q"), "i=14088")
	})

	t.Run("certs", func(t *testing.T) {
		dir := t.TempDir()
		out := run(t, "gds", "certs", "--out", dir, "-v")
		wantContains(t, out, "GROUP", "default", "rsa", gds.Thumbprint(srvID.der), "CN=opcuactl test rsa", serverURI)
		if b, err := os.ReadFile(filepath.Join(dir, gds.Thumbprint(srvID.der)+".der")); err != nil || len(b) == 0 {
			t.Errorf("--out: %v", err)
		}
		var entries []model.GDSCertificate
		if err := json.Unmarshal([]byte(run(t, "gds", "certs", "--format", "json")), &entries); err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 || entries[0].CertificateType != "rsa" || entries[0].Group != "default" {
			t.Errorf("certs = %+v", entries)
		}
		wantContains(t, run(t, "gds", "certs", "-q"), gds.Thumbprint(srvID.der))
		fail(t, 1, "invalid --group", "gds", "certs", "--group", "bad group")
	})

	t.Run("trust lists of the https and usertoken groups", func(t *testing.T) {
		out := run(t, "gds", "trustlist", "add", "--group", "usertoken", "--issuer", ca.certFile)
		wantContains(t, out, "issuer certificates of the usertoken group", "changes applied")
		if tl := s.srv.GroupTrustList(gds.DefaultUserTokenGroup); tl == nil || len(tl.IssuerCertificates) != 1 {
			t.Fatalf("usertoken trust list = %+v", tl)
		}
		wantContains(t, run(t, "gds", "trustlist", "get", "--group", "usertoken"), "issuer", gds.Thumbprint(ca.der))
		run(t, "gds", "trustlist", "remove", "--group", "usertoken", "--issuer", ca.certFile)
		run(t, "gds", "trustlist", "set", "--group", "https", ca.certFile, "--yes")
		if tl := s.srv.GroupTrustList(gds.DefaultHTTPSGroup); tl == nil || len(tl.TrustedCertificates) != 1 {
			t.Fatalf("https trust list = %+v", tl)
		}
		// A group by its NodeId.
		wantContains(t, run(t, "gds", "trustlist", "get", "--group", gds.DefaultHTTPSGroup.String()), gds.Thumbprint(ca.der))
		fail(t, 3, "certificate group", "gds", "trustlist", "get", "--group", "ns=1;s=Int32")
		fail(t, 1, "invalid --group", "gds", "trustlist", "set", "--group", "bad group", ca.certFile)
	})

	t.Run("csr of the https group", func(t *testing.T) {
		out := run(t, "gds", "csr", "--group", "https", "--type", "https", "--regenerate")
		wantContains(t, out, "CERTIFICATE REQUEST")
		fail(t, 1, "invalid --group", "gds", "csr", "--group", "bad group")
	})

	t.Run("update-cert with a PFX key", func(t *testing.T) {
		dir := t.TempDir()
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatal(err)
		}
		issued := ca.issue(t, key.Public(), mustParse(t, srvID.der))
		certFile := filepath.Join(dir, "server.der")
		if err := os.WriteFile(certFile, issued, 0o600); err != nil {
			t.Fatal(err)
		}
		keyDER, _ := x509.MarshalPKCS8PrivateKey(key)
		keyFile := filepath.Join(dir, "server.key")
		writePEM(t, keyFile, "PRIVATE KEY", keyDER)

		// The wrong password: the server cannot open the PFX.
		fail(t, 3, "UpdateCertificate", "gds", "update-cert", certFile, "--private-key", keyFile, "--key-format", "pfx", "--pfx-password", "wrong", "--issuer", ca.certFile, "--yes")
		out := run(t, "gds", "update-cert", certFile, "--private-key", keyFile, "--key-format", "pfx", "--pfx-password", "pfx-secret", "--issuer", ca.certFile, "--yes")
		wantContains(t, out, "Installed the certificate "+gds.Thumbprint(issued))
		if !serverOffers(s.srv, issued) {
			t.Fatal("the endpoints do not carry the new certificate")
		}
		admin = gdsAdminArgs(s, srvID, cli, ca.certFile)

		// A PKCS #12 file is sent as it is.
		key2, _ := rsa.GenerateKey(rand.Reader, 2048)
		issued2 := ca.issue(t, key2.Public(), mustParse(t, srvID.der))
		pfx, err := uapolicy.EncodePFX(key2, [][]byte{issued2}, "pfx-secret")
		if err != nil {
			t.Fatal(err)
		}
		pfxFile := filepath.Join(dir, "server.p12")
		if err := os.WriteFile(pfxFile, pfx, 0o600); err != nil {
			t.Fatal(err)
		}
		cert2 := filepath.Join(dir, "server2.der")
		if err := os.WriteFile(cert2, issued2, 0o600); err != nil {
			t.Fatal(err)
		}
		run(t, "gds", "update-cert", cert2, "--private-key", pfxFile, "--pfx-password", "pfx-secret", "--issuer", ca.certFile, "--yes")
		if !serverOffers(s.srv, issued2) {
			t.Fatal("the endpoints do not carry the PFX certificate")
		}
		fail(t, 1, "--pfx-password", "gds", "update-cert", cert2, "--private-key", pfxFile, "--pfx-password", "nope", "--yes")
		fail(t, 1, "need --private-key", "gds", "update-cert", cert2, "--key-format", "pfx", "--yes")
		fail(t, 1, "unknown --key-format", "gds", "update-cert", cert2, "--private-key", pfxFile, "--pfx-password", "pfx-secret", "--key-format", "jks", "--yes")
	})

	t.Run("transaction", func(t *testing.T) {
		out := run(t, "gds", "transaction")
		wantContains(t, out, "Last certificate transaction", "Result:", "Good", "Certificate groups:", "default")
		var tx model.GDSTransaction
		if err := json.Unmarshal([]byte(run(t, "gds", "transaction", "--format", "json")), &tx); err != nil {
			t.Fatal(err)
		}
		if tx.Result != "Good" || tx.StartTime == "" || len(tx.AffectedCertificateGroups) == 0 {
			t.Errorf("transaction = %+v", tx)
		}
		wantContains(t, run(t, "gds", "transaction", "-q"), "Good")
	})

	t.Run("self-signed", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "self.pem")
		out := run(t, "gds", "self-signed", "--type", "rsa", "--subject", "CN=Self/O=Plant", "--dns", "localhost", "--days", "30", "--out", file, "--yes")
		wantContains(t, out, "Created the self-signed certificate", "CN=Self", "changes applied")
		b, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		block, _ := pem.Decode(b)
		if block == nil || !serverOffers(s.srv, block.Bytes) {
			t.Fatal("the endpoints do not carry the self-signed certificate")
		}
		c := mustParse(t, block.Bytes)
		if c.Subject.CommonName != "Self" || len(c.URIs) != 1 || c.URIs[0].String() != serverURI {
			t.Errorf("self-signed = %v %v", c.Subject, c.URIs)
		}
		self := filepath.Join(keep, "self-trusted.pem")
		writePEM(t, self, "CERTIFICATE", block.Bytes)
		admin = append(admin, "--trusted-cert", self)
		var res model.GDSResult
		der := filepath.Join(t.TempDir(), "self.der")
		if err := json.Unmarshal([]byte(run(t, "gds", "self-signed", "--type", "rsa", "--no-apply", "--out", der, "--yes", "--format", "json")), &res); err != nil {
			t.Fatal(err)
		}
		if res.Action != "self-signed" || res.Applied || !res.ApplyChangesRequired {
			t.Errorf("--no-apply = %+v", res)
		}
		if b, _ := os.ReadFile(der); len(b) == 0 || b[0] != 0x30 {
			t.Error("--out .der is not DER")
		}
		fail(t, 1, "invalid --type", "gds", "self-signed", "--type", "dsa", "--yes")
		fail(t, 1, "invalid --group", "gds", "self-signed", "--group", "bad group", "--yes")
	})

	t.Run("delete-cert", func(t *testing.T) {
		fail(t, 1, "--type is required", "gds", "delete-cert", "--yes")
		fail(t, 1, "invalid --type", "gds", "delete-cert", "--type", "dsa", "--yes")
		fail(t, 1, "invalid --group", "gds", "delete-cert", "--group", "bad group", "--type", "rsa", "--yes")
		// The server refuses to delete the certificate its endpoints use.
		fail(t, 3, "endpoints still use", "gds", "delete-cert", "--type", "rsa", "--yes")
		// The HTTPS certificate of a server without opc.https endpoints goes.
		run(t, "gds", "self-signed", "--group", "https", "--type", "https", "--yes")
		wantContains(t, run(t, "gds", "certs", "--group", "https"), "https")
		wantContains(t, run(t, "gds", "delete-cert", "--group", "https", "--type", "https", "--yes"), "Deleted the https certificate", "changes applied")
		if out := run(t, "gds", "certs", "--group", "https", "-q"); strings.TrimSpace(out) != "" {
			t.Errorf("certs of the https group after the delete = %q", out)
		}
		fail(t, 3, "DeleteCertificate", "gds", "delete-cert", "--group", "https", "--type", "https", "--yes")
	})

	t.Run("reset", func(t *testing.T) {
		wantContains(t, run(t, "gds", "reset", "--yes"), "ResetToServerDefaults: done")
		if !serverOffers(s.srv, srvID.der) {
			t.Error("the reset did not restore the starting certificate")
		}
		admin = gdsAdminArgs(s, srvID, cli)
		wantContains(t, run(t, "gds", "reset", "--yes", "--format", "json"), `"method":"ResetToServerDefaults"`)
	})
}
