// SPDX-License-Identifier: MIT

package opcuactl

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-opcua/gds"
	"github.com/otfabric/go-opcua/server"
	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/opcuactl/internal/model"
)

// testCA is a certificate authority for the GDS tests.
type testCA struct {
	der      []byte
	cert     *x509.Certificate
	key      crypto.Signer
	certFile string
}

func newTestCA(t *testing.T) *testCA {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "opcuactl test CA", Organization: []string{"otfabric"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, key.Public(), key)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(der)
	ca := &testCA{der: der, cert: cert, key: key, certFile: filepath.Join(t.TempDir(), "ca.pem")}
	writePEM(t, ca.certFile, "CERTIFICATE", der)
	return ca
}

// issue issues an application instance certificate for pub with the
// names of the template.
func (ca *testCA) issue(t *testing.T, pub crypto.PublicKey, names *x509.Certificate) []byte {
	t.Helper()
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 100))
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      names.Subject,
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(12 * time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageContentCommitment |
			x509.KeyUsageKeyEncipherment | x509.KeyUsageDataEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		DNSNames:              names.DNSNames,
		IPAddresses:           names.IPAddresses,
		URIs:                  names.URIs,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, pub, ca.key)
	if err != nil {
		t.Fatal(err)
	}
	return der
}

// crl returns an empty CRL of the CA.
func (ca *testCA) crl(t *testing.T) []byte {
	t.Helper()
	der, err := x509.CreateRevocationList(rand.Reader, &x509.RevocationList{
		Number:     big.NewInt(1),
		ThisUpdate: time.Now().Add(-time.Minute),
		NextUpdate: time.Now().Add(time.Hour),
	}, ca.cert, ca.key)
	if err != nil {
		t.Fatal(err)
	}
	return der
}

const (
	gdsAdmin    = "secadmin"
	gdsAdminPwd = "secret"
)

// startGDSServer starts a server with push certificate management that
// trusts the client certificate cli; the user secadmin has the
// SecurityAdmin role, the user operator not.
func startGDSServer(t *testing.T, srvID, cli *identity) *testServer {
	t.Helper()
	return startServer(t,
		server.ApplicationIdentity(srvID.der, srvID.key),
		server.EnableSecurity("Basic256Sha256", ua.MessageSecurityModeSignAndEncrypt),
		server.EnableAuthMode(ua.UserTokenTypeUserName),
		server.AllowUsernameOnNone(),
		server.WithUsernameValidator(func(user, pass string) error {
			if (user == gdsAdmin && pass == gdsAdminPwd) || (user == "operator" && pass == "op") {
				return nil
			}
			return errors.New("denied")
		}),
		server.WithRoleMapper(func(tok ua.IdentityToken) []*ua.NodeID {
			if u, ok := tok.(*ua.UserNameIdentityToken); ok && u.UserName == gdsAdmin {
				return []*ua.NodeID{ua.RoleAuthenticatedUser.NodeID(), ua.RoleSecurityAdmin.NodeID()}
			}
			return server.DefaultRoleMapper(tok)
		}),
		server.WithPushManagement(server.PushManagement{
			TrustList: &ua.TrustListDataType{TrustedCertificates: [][]byte{cli.der}},
		}),
	)
}

func TestE2E_GDS(t *testing.T) {
	srvID := newIdentity(t, "rsa", serverURI)
	cli := newIdentity(t, "rsa", clientURI)
	ca := newTestCA(t)
	s := startGDSServer(t, srvID, cli)

	secure := func(id *identity) []string {
		return []string{"-e", s.url, "--security-policy", "Basic256Sha256", "--security-mode", "SignAndEncrypt",
			"--cert", id.certFile, "--key", id.keyFile, "--trusted-cert", srvID.certFile, "--trusted-cert", ca.certFile}
	}
	admin := append(secure(cli), "--auth", "username", "--username", gdsAdmin, "--password", gdsAdminPwd)
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
			t.Errorf("opcuactl %s: exit %d, want %d", strings.Join(args, " "), ExitCode(err), code)
		}
		if !strings.Contains(errOut, wantStderr) {
			t.Errorf("opcuactl %s: stderr does not contain %q:\n%s", strings.Join(args, " "), wantStderr, errOut)
		}
	}
	trusted := func() [][]byte { return s.srv.TrustList().TrustedCertificates }
	cliThumb := gds.Thumbprint(cli.der)

	t.Run("props", func(t *testing.T) {
		out := run(t, "gds", "props")
		wantContains(t, out, "Server configuration", "Private key formats:", "PEM", "Certificate types:", "rsa", "Max trust list size:", "1048576 bytes")
		var p model.GDSProperties
		if err := json.Unmarshal([]byte(run(t, "gds", "props", "--format", "json")), &p); err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(p.SupportedPrivateKeyFormats, "PEM") || !slices.Contains(p.CertificateTypes, "rsa") || p.MaxTrustListSize != 1<<20 {
			t.Errorf("props = %+v", p)
		}
	})

	t.Run("access", func(t *testing.T) {
		operator := append(secure(cli), "--auth", "username", "--username", "operator", "--password", "op")
		fail(t, 3, "SecurityAdmin role", append([]string{"gds", "trustlist", "get"}, operator...)...)
		fail(t, 3, "--security-mode SignAndEncrypt", "gds", "rejected", "-e", s.url, "--auth", "username", "--username", gdsAdmin, "--password", gdsAdminPwd)
		fail(t, 3, "SecurityAdmin role", append([]string{"gds", "apply"}, operator...)...)
	})

	t.Run("trustlist get", func(t *testing.T) {
		out := run(t, "gds", "trustlist", "get")
		wantContains(t, out, "LIST", "THUMBPRINT", "trusted", cliThumb, "CN=opcuactl test rsa")

		dir := t.TempDir()
		var entries []model.GDSCertificate
		if err := json.Unmarshal([]byte(run(t, "gds", "trustlist", "get", "--out", dir, "--format", "json")), &entries); err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 || entries[0].List != "trusted" || entries[0].Thumbprint != cliThumb || entries[0].ApplicationURI != clientURI {
			t.Fatalf("entries = %+v", entries)
		}
		b, err := os.ReadFile(filepath.Join(dir, "trusted", "certs", cliThumb+".der"))
		if err != nil || !bytes.Equal(b, cli.der) {
			t.Errorf("written certificate: %v", err)
		}

		out = run(t, "gds", "trustlist", "get", "--masks", "issuers", "--format", "json")
		if strings.TrimSpace(out) != "[]" {
			t.Errorf("issuers = %s", out)
		}
		out = run(t, "gds", "trustlist", "get", "-q")
		if strings.TrimSpace(out) != cliThumb {
			t.Errorf("quiet = %q", out)
		}
		out = run(t, "gds", "trustlist", "get", "-v")
		wantContains(t, out, "APPLICATION URI", clientURI)

		fail(t, 1, "invalid --masks", append([]string{"gds", "trustlist", "get", "--masks", "everything"}, admin...)...)
	})

	stranger := newIdentity(t, "rsa", "urn:otfabric:opcuactl:test:stranger")
	strangerThumb := gds.Thumbprint(stranger.der)
	strangerRead := append([]string{"get", "value", "-n", s.node("Int32"), "-q"}, secure(stranger)...)

	t.Run("rejected and add", func(t *testing.T) {
		if _, _, err := runCLI(t, strangerRead...); err == nil {
			t.Fatal("an untrusted client connected")
		}
		dir := t.TempDir()
		out := run(t, "gds", "rejected", "--out", dir)
		wantContains(t, out, "rejected", strangerThumb)
		file := filepath.Join(dir, strangerThumb+".der")
		if b, err := os.ReadFile(file); err != nil || !bytes.Equal(b, stranger.der) {
			t.Fatalf("rejected file: %v", err)
		}

		// --no-apply leaves the change in the transaction of the session,
		// which the server drops with it.
		_, errOut, err := runCLI(t, append([]string{"gds", "trustlist", "add", file, "--no-apply"}, admin...)...)
		if err != nil {
			t.Fatal(err)
		}
		wantContains(t, errOut, "pending")
		out, errOut, err = runCLI(t, append([]string{"gds", "trustlist", "add", file, "--no-apply", "--format", "json"}, admin...)...)
		if err != nil {
			t.Fatal(err)
		}
		wantContains(t, errOut, "pending")
		wantContains(t, out, `"applyChangesRequired":true`, `"applied":false`)
		if len(trusted()) != 1 {
			t.Fatalf("--no-apply applied the change: %d trusted", len(trusted()))
		}

		out = run(t, "gds", "trustlist", "add", file)
		wantContains(t, out, "Added 1 certificate(s) to the trusted certificates", strangerThumb, "changes applied")
		if len(trusted()) != 2 {
			t.Fatalf("trusted = %d certificates", len(trusted()))
		}
		wantContains(t, mustRun(t, strangerRead...), "42")
	})

	t.Run("remove", func(t *testing.T) {
		// Lower case with colons, as other tools print thumbprints.
		var colons []string
		for i := 0; i < len(strangerThumb); i += 2 {
			colons = append(colons, strings.ToLower(strangerThumb[i:i+2]))
		}
		var res model.GDSResult
		if err := json.Unmarshal([]byte(run(t, "gds", "trustlist", "remove", strings.Join(colons, ":"), "--format", "json")), &res); err != nil {
			t.Fatal(err)
		}
		if !res.Applied || res.Action != "remove" || len(res.Thumbprints) != 1 || res.Thumbprints[0] != strangerThumb {
			t.Errorf("result = %+v", res)
		}
		if len(trusted()) != 1 {
			t.Fatalf("trusted = %d certificates", len(trusted()))
		}
		if _, _, err := runCLI(t, strangerRead...); err == nil {
			t.Fatal("a removed client connected")
		}
		fail(t, 3, "not in the trusted certificates", append([]string{"gds", "trustlist", "remove", stranger.certFile}, admin...)...)
		fail(t, 1, "neither a SHA-1 thumbprint nor a certificate file", append([]string{"gds", "trustlist", "remove", "nope"}, admin...)...)
	})

	t.Run("issuer add and remove", func(t *testing.T) {
		out := run(t, "gds", "trustlist", "add", "--issuer", ca.certFile)
		wantContains(t, out, "issuer certificates")
		if tl := s.srv.TrustList(); len(tl.IssuerCertificates) != 1 || !bytes.Equal(tl.IssuerCertificates[0], ca.der) {
			t.Fatalf("issuers = %d", len(tl.IssuerCertificates))
		}
		run(t, "gds", "trustlist", "remove", "--issuer", ca.certFile)
		if tl := s.srv.TrustList(); len(tl.IssuerCertificates) != 0 {
			t.Fatalf("issuers = %d", len(tl.IssuerCertificates))
		}
		fail(t, 1, "no such file", append([]string{"gds", "trustlist", "add", filepath.Join(t.TempDir(), "missing.der")}, admin...)...)
	})

	t.Run("set", func(t *testing.T) {
		dir := t.TempDir()
		run(t, "gds", "trustlist", "get", "--out", dir)
		crlFile := filepath.Join(t.TempDir(), "ca.crl")
		writePEM(t, crlFile, "X509 CRL", ca.crl(t))

		out := run(t, "gds", "trustlist", "set", dir, stranger.certFile, "--issuer", ca.certFile, "--issuer", crlFile)
		wantContains(t, out, "Wrote the trust list (2 trusted, 0 trusted CRLs, 1 issuers, 1 issuer CRLs)", "changes applied")
		tl := s.srv.TrustList()
		if len(tl.TrustedCertificates) != 2 || len(tl.IssuerCertificates) != 1 || len(tl.IssuerCrls) != 1 {
			t.Fatalf("trust list = %d trusted, %d issuers, %d issuer CRLs", len(tl.TrustedCertificates), len(tl.IssuerCertificates), len(tl.IssuerCrls))
		}
		wantContains(t, mustRun(t, strangerRead...), "42")
		out = run(t, "gds", "trustlist", "get", "--masks", "issuer-crls")
		wantContains(t, out, "issuer-crl", "CN=opcuactl test CA", "(0 revoked)")

		// Round trip through a PKI directory, and only one list.
		full := t.TempDir()
		run(t, "gds", "trustlist", "get", "--out", full)
		if _, err := os.Stat(filepath.Join(full, "issuer", "crl")); err != nil {
			t.Fatal(err)
		}
		run(t, "gds", "trustlist", "set", "--masks", "trusted", cli.certFile, "--yes")
		tl = s.srv.TrustList()
		if len(tl.TrustedCertificates) != 1 || len(tl.IssuerCertificates) != 1 {
			t.Fatalf("--masks trusted: %d trusted, %d issuers", len(tl.TrustedCertificates), len(tl.IssuerCertificates))
		}
		var res model.GDSResult
		if err := json.Unmarshal([]byte(run(t, "gds", "trustlist", "set", full, "--format", "json")), &res); err != nil {
			t.Fatal(err)
		}
		if !res.ApplyChangesRequired || !res.Applied {
			t.Errorf("result = %+v", res)
		}
		if tl = s.srv.TrustList(); len(tl.TrustedCertificates) != 2 || len(tl.IssuerCrls) != 1 {
			t.Fatalf("round trip: %d trusted, %d issuer CRLs", len(tl.TrustedCertificates), len(tl.IssuerCrls))
		}

		fail(t, 1, "nothing to write", append([]string{"gds", "trustlist", "set"}, admin...)...)
		fail(t, 1, "--masks does not replace", append([]string{"gds", "trustlist", "set", "--masks", "trusted", cli.certFile, "--issuer", ca.certFile}, admin...)...)
		fail(t, 1, "no trusted/certs", append([]string{"gds", "trustlist", "set", t.TempDir()}, admin...)...)
		fail(t, 1, "invalid --masks", append([]string{"gds", "trustlist", "set", "--masks", "x", cli.certFile}, admin...)...)
		fail(t, 1, "not a certificate or CRL", append([]string{"gds", "trustlist", "set", cli.keyFile}, admin...)...)
		fail(t, 1, "not a certificate or CRL", append([]string{"gds", "trustlist", "set", cli.certFile, "--issuer", cli.keyFile}, admin...)...)
		bad := t.TempDir()
		if err := os.MkdirAll(filepath.Join(bad, "trusted", "crl"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(bad, "trusted", "crl", "x.der"), cli.der, 0o600); err != nil {
			t.Fatal(err)
		}
		fail(t, 1, "a certificate in", append([]string{"gds", "trustlist", "set", bad}, admin...)...)
		bad = t.TempDir()
		if err := os.MkdirAll(filepath.Join(bad, "trusted", "certs"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(bad, "trusted", "certs", "x.crl"), ca.crl(t), 0o600); err != nil {
			t.Fatal(err)
		}
		fail(t, 1, "a CRL in", append([]string{"gds", "trustlist", "set", bad}, admin...)...)
		_, errOut, _ := runCLI(t, append([]string{"gds", "trustlist", "set", "--masks", "trusted", "--issuer", ca.certFile}, admin...)...)
		wantContains(t, errOut, "--masks does not replace")
	})

	t.Run("csr and update-cert", func(t *testing.T) {
		dir := t.TempDir()
		csrFile := filepath.Join(dir, "server.csr")
		out := run(t, "gds", "csr", "--type", "rsa", "--regenerate", "--out", csrFile)
		wantContains(t, out, "written to "+csrFile, "Subject:", "CN=opcuactl test rsa", serverURI, "RSA")
		b, err := os.ReadFile(csrFile)
		if err != nil {
			t.Fatal(err)
		}
		block, _ := pem.Decode(b)
		if block == nil || block.Type != "CERTIFICATE REQUEST" {
			t.Fatalf("not a PEM request:\n%s", b)
		}
		csr, err := x509.ParseCertificateRequest(block.Bytes)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Equal(csr.RawSubjectPublicKeyInfo, mustSPKI(t, srvID.der)) {
			t.Fatal("--regenerate kept the key")
		}

		// The CA issues the certificate; its PEM carries the chain.
		issued := ca.issue(t, csr.PublicKey, &x509.Certificate{Subject: csr.Subject, DNSNames: csr.DNSNames, IPAddresses: csr.IPAddresses, URIs: csr.URIs})
		chain := filepath.Join(dir, "server.pem")
		if err := os.WriteFile(chain, append(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: issued}),
			pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.der})...), 0o600); err != nil {
			t.Fatal(err)
		}
		out = run(t, "gds", "update-cert", chain, "--yes")
		wantContains(t, out, "Installed the certificate "+gds.Thumbprint(issued), "changes applied")
		if !serverOffers(s.srv, issued) {
			t.Fatal("the endpoints do not carry the new certificate")
		}
		// A new connection validates it against the CA.
		wantContains(t, mustRun(t, "get", "value", "-n", s.node("Int32"), "-q", "-e", s.url, "--security-policy", "Basic256Sha256",
			"--security-mode", "SignAndEncrypt", "--cert", cli.certFile, "--key", cli.keyFile, "--trusted-cert", ca.certFile), "42")
	})

	t.Run("csr output", func(t *testing.T) {
		out := run(t, "gds", "csr")
		wantContains(t, out, "-----BEGIN CERTIFICATE REQUEST-----")
		var res model.GDSSigningRequest
		derFile := filepath.Join(t.TempDir(), "server.der")
		if err := json.Unmarshal([]byte(run(t, "gds", "csr", "--subject", "CN=Renamed/O=Plant", "--out", derFile, "--format", "json")), &res); err != nil {
			t.Fatal(err)
		}
		if res.Subject != "CN=Renamed,O=Plant" && res.Subject != "O=Plant,CN=Renamed" {
			t.Errorf("subject = %q", res.Subject)
		}
		if !slices.Contains(res.URIs, serverURI) || res.File != derFile || !strings.Contains(res.PEM, "CERTIFICATE REQUEST") {
			t.Errorf("csr = %+v", res)
		}
		b, _ := os.ReadFile(derFile)
		if _, err := x509.ParseCertificateRequest(b); err != nil {
			t.Errorf("--out .der: %v", err)
		}
		out = run(t, "gds", "csr", "--out", filepath.Join(t.TempDir(), "q.csr"), "-q")
		wantContains(t, out, "q.csr")
		fail(t, 1, "invalid --type", append([]string{"gds", "csr", "--type", "dsa"}, admin...)...)
		fail(t, 3, "CreateSigningRequest", append([]string{"gds", "csr", "--type", "ecc-p256"}, admin...)...)
		fail(t, 1, "no such file", append([]string{"gds", "csr", "--out", filepath.Join(t.TempDir(), "missing", "x.csr")}, admin...)...)
	})

	t.Run("update-cert with a key", func(t *testing.T) {
		dir := t.TempDir()
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatal(err)
		}
		names, _ := x509.ParseCertificate(srvID.der)
		issued := ca.issue(t, key.Public(), names)
		certFile := filepath.Join(dir, "server.der")
		if err := os.WriteFile(certFile, issued, 0o600); err != nil {
			t.Fatal(err)
		}
		keyDER, _ := x509.MarshalPKCS8PrivateKey(key)
		keyFile := filepath.Join(dir, "server.key.der")
		if err := os.WriteFile(keyFile, keyDER, 0o600); err != nil {
			t.Fatal(err)
		}
		var res model.GDSResult
		if err := json.Unmarshal([]byte(run(t, "gds", "update-cert", certFile, "--private-key", keyFile, "--issuer", ca.certFile, "--format", "json")), &res); err != nil {
			t.Fatal(err)
		}
		if !res.Applied || res.Thumbprints[0] != gds.Thumbprint(issued) {
			t.Errorf("result = %+v", res)
		}
		if !serverOffers(s.srv, issued) {
			t.Fatal("the endpoints do not carry the new certificate")
		}

		fail(t, 1, "does not match", append([]string{"gds", "update-cert", certFile, "--private-key", cli.keyFile}, admin...)...)
		fail(t, 1, "no such file", append([]string{"gds", "update-cert", certFile, "--private-key", filepath.Join(dir, "missing")}, admin...)...)
		fail(t, 1, "not a certificate", append([]string{"gds", "update-cert", cli.keyFile}, admin...)...)
		fail(t, 1, "not a certificate", append([]string{"gds", "update-cert", certFile, "--issuer", cli.keyFile}, admin...)...)
		fail(t, 1, "invalid --type", append([]string{"gds", "update-cert", certFile, "--type", "dsa"}, admin...)...)
		// The ApplicationUri of the client is not the server's.
		other := ca.issue(t, cli.key.Public(), mustParse(t, cli.der))
		otherFile := filepath.Join(dir, "other.der")
		if err := os.WriteFile(otherFile, other, 0o600); err != nil {
			t.Fatal(err)
		}
		fail(t, 3, "ApplicationUri of the server", append([]string{"gds", "update-cert", otherFile, "--private-key", cli.keyFile, "--yes"}, admin...)...)
	})

	t.Run("apply and cancel", func(t *testing.T) {
		wantContains(t, run(t, "gds", "apply"), "ApplyChanges: done")
		wantContains(t, run(t, "gds", "cancel", "--format", "json"), `"method":"CancelChanges"`, `"status":"done"`)
		if out := run(t, "gds", "cancel", "-q"); out != "" {
			t.Errorf("quiet cancel printed %q", out)
		}
	})
}

// TestE2E_GDSWithoutPushManagement names the missing ServerConfiguration.
func TestE2E_GDSWithoutPushManagement(t *testing.T) {
	s := startServer(t)
	_, errOut, err := runCLI(t, "gds", "rejected", "-e", s.url)
	if err == nil || ExitCode(err) != 3 {
		t.Fatalf("err = %v (exit %d)", err, ExitCode(err))
	}
	wantContains(t, errOut, "GetRejectedList")
	_, errOut, err = runCLI(t, "gds", "props", "-e", s.url)
	if err != nil {
		t.Fatal(err)
	}
	wantContains(t, errOut, "publishes no ServerConfiguration properties")
	_, _, err = runCLI(t, "gds", "props", "-e", "opc.tcp://127.0.0.1:1", "--timeout", "2s")
	if ExitCode(err) != 2 {
		t.Errorf("unreachable server: exit %d", ExitCode(err))
	}
}

func mustParse(t *testing.T, der []byte) *x509.Certificate {
	t.Helper()
	c, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func mustSPKI(t *testing.T, der []byte) []byte {
	t.Helper()
	return mustParse(t, der).RawSubjectPublicKeyInfo
}

// serverOffers reports whether the endpoints of srv carry the certificate.
func serverOffers(srv *server.Server, cert []byte) bool {
	for _, ep := range srv.Endpoints() {
		if bytes.HasPrefix(ep.ServerCertificate, cert) {
			return true
		}
	}
	return false
}
