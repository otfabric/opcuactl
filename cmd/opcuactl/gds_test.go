// SPDX-License-Identifier: MIT

package opcuactl

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-opcua/gds"
	"github.com/otfabric/go-opcua/id"
	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/opcuactl/internal/model"
)

func TestGDSHint(t *testing.T) {
	for _, st := range []ua.StatusCode{
		ua.StatusBadUserAccessDenied, ua.StatusBadSecurityModeInsufficient, ua.StatusBadNodeIDUnknown,
		ua.StatusBadMethodInvalid, ua.StatusBadNotImplemented, ua.StatusBadTransactionPending,
		ua.StatusBadCertificateURIInvalid, ua.StatusBadCertificateTimeInvalid, ua.StatusBadSecurityChecksFailed,
		ua.StatusBadCertificateInvalid, ua.StatusBadNotSupported, ua.StatusBadEncodingLimitsExceeded,
	} {
		if gdsHint(&gds.Error{Method: "M", Status: st}) == "" {
			t.Errorf("no hint for %s", st.Symbol())
		}
	}
	if gdsHint(ua.StatusBadTimeout) != "" {
		t.Error("a hint for BadTimeout")
	}
}

func TestReadCertsAndCRLs(t *testing.T) {
	ca := newTestCA(t)
	dir := t.TempDir()
	write := func(name string, b []byte) string {
		f := filepath.Join(dir, name)
		if err := os.WriteFile(f, b, 0o600); err != nil {
			t.Fatal(err)
		}
		return f
	}
	crl := ca.crl(t)

	// DER certificate, DER chain, DER CRL.
	for _, tc := range []struct {
		name         string
		data         []byte
		certs, crls  int
		wantErrorStr string
	}{
		{"cert.der", ca.der, 1, 0, ""},
		{"chain.der", append(append([]byte{}, ca.der...), ca.der...), 2, 0, ""},
		{"ca.crl", crl, 0, 1, ""},
		{"junk.bin", []byte("junk"), 0, 0, "not a certificate or CRL"},
		{"key.pem", []byte("-----BEGIN PRIVATE KEY-----\nAAAA\n-----END PRIVATE KEY-----\n"), 0, 0, "no CERTIFICATE"},
		{"badcert.pem", []byte("-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n"), 0, 0, "badcert.pem"},
		{"badcrl.pem", []byte("-----BEGIN X509 CRL-----\nAAAA\n-----END X509 CRL-----\n"), 0, 0, "badcrl.pem"},
	} {
		certs, crls, err := readCertsAndCRLs(write(tc.name, tc.data))
		if tc.wantErrorStr != "" {
			if err == nil || !strings.Contains(err.Error(), tc.wantErrorStr) {
				t.Errorf("%s: err = %v, want %q", tc.name, err, tc.wantErrorStr)
			}
			continue
		}
		if err != nil || len(certs) != tc.certs || len(crls) != tc.crls {
			t.Errorf("%s: %d certs, %d crls, %v", tc.name, len(certs), len(crls), err)
		}
	}
	if _, err := readCerts(write("only.crl", crl)); err == nil {
		t.Error("readCerts accepted a CRL")
	}
	if _, _, err := readCertsAndCRLs(filepath.Join(dir, "missing")); err == nil {
		t.Error("a missing file was read")
	}
}

func TestDescribe(t *testing.T) {
	ca := newTestCA(t)
	e := describe("issuer", ca.der)
	if e.Kind != "certificate" || !e.CA || e.Subject == "" || e.Thumbprint != gds.Thumbprint(ca.der) || e.Error != "" {
		t.Errorf("certificate = %+v", e)
	}
	c := describe("issuer-crl", ca.crl(t))
	if c.Kind != "crl" || c.Issuer == "" || c.NotAfter == "" || c.Error != "" {
		t.Errorf("crl = %+v", c)
	}
	if e := describe("trusted", []byte("junk")); e.Error == "" {
		t.Error("no error for a junk certificate")
	}
	if e := describe("trusted-crl", []byte("junk")); e.Error == "" {
		t.Error("no error for a junk CRL")
	}

	// A CRL without NextUpdate.
	der, err := x509.CreateRevocationList(rand.Reader, &x509.RevocationList{Number: big.NewInt(2), ThisUpdate: time.Now()}, ca.cert, ca.key)
	if err == nil {
		if c := describe("trusted-crl", der); c.NotAfter != "" {
			t.Errorf("NotAfter = %q", c.NotAfter)
		}
	}

	// A file where a directory should be.
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeEntry(filepath.Join(file, "sub"), &e, ca.der); err == nil {
		t.Error("writeEntry into a file succeeded")
	}
}

func TestWriteCertificatesTable(t *testing.T) {
	out := captureStdout(t, func() {
		outputFormat = "table"
		_ = writeCertificates([]model.GDSCertificate{
			{List: "trusted", Kind: "certificate", Thumbprint: "AB", Subject: "CN=x", CA: true},
			{List: "trusted-crl", Kind: "crl", Thumbprint: "CD", Issuer: "CN=ca", Revoked: 2},
			{List: "rejected", Kind: "certificate", Thumbprint: "EF", Error: "broken"},
		})
	})
	wantContains(t, out, "CN=x", "yes", "CN=ca (2 revoked)", "invalid: broken")
}

// captureStdout runs fn with stdout captured, under the lock of the CLI
// tests.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	cliMu.Lock()
	defer cliMu.Unlock()
	saved := outputFormat
	defer func() { outputFormat = saved }()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	fn()
	_ = w.Close()
	os.Stdout = orig
	return <-done
}

func TestConfirmed(t *testing.T) {
	saved := stdinIsTerminal
	defer func() { stdinIsTerminal = saved }()

	stdinIsTerminal = func() bool { return false }
	if !confirmed(false, strings.NewReader(""), "q?") {
		t.Error("no terminal: not confirmed")
	}
	stdinIsTerminal = func() bool { return true }
	if !confirmed(true, strings.NewReader(""), "q?") {
		t.Error("--yes: not confirmed")
	}
	if !confirmed(false, strings.NewReader("y\n"), "q?") {
		t.Error("y: not confirmed")
	}
	if confirmed(false, strings.NewReader("no\n"), "q?") {
		t.Error("no: confirmed")
	}
}

func TestThumbprintArg(t *testing.T) {
	const h = "0123456789abcdef0123456789ABCDEF01234567"
	got, err := thumbprintArg(" 01:23:45:67:89:ab:cd:ef:01:23:45:67:89:AB:CD:EF:01:23:45:67 ")
	if err != nil || got != strings.ToUpper(h) {
		t.Errorf("thumbprintArg = %q, %v", got, err)
	}
	if _, err := thumbprintArg("0123"); err == nil {
		t.Error("a short thumbprint was accepted")
	}
}

func selfSigned(t *testing.T, key crypto.Signer) []byte {
	t.Helper()
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "k"}, NotBefore: time.Now(), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, key.Public(), key)
	if err != nil {
		t.Fatal(err)
	}
	return der
}

func TestCertificateTypeOf(t *testing.T) {
	p256, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	p384, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	_, ed, _ := ed25519.GenerateKey(rand.Reader)
	small, _ := rsa.GenerateKey(rand.Reader, 1024) //nolint:gosec // RsaMin
	p224, _ := ecdsa.GenerateKey(elliptic.P224(), rand.Reader)
	for _, tc := range []struct {
		name string
		der  []byte
		want uint32
	}{
		{"p256", selfSigned(t, p256), id.EccNistP256ApplicationCertificateType},
		{"p384", selfSigned(t, p384), id.EccNistP384ApplicationCertificateType},
		{"ed25519", selfSigned(t, ed), id.EccCurve25519ApplicationCertificateType},
		{"rsa1024", selfSigned(t, small), id.RsaMinApplicationCertificateType},
		{"p224", selfSigned(t, p224), 0},
		{"junk", []byte("junk"), 0},
	} {
		got := certificateTypeOf(tc.der)
		switch {
		case tc.want == 0 && got != nil:
			t.Errorf("%s: %v, want nil", tc.name, got)
		case tc.want != 0 && (got == nil || got.IntID() != tc.want):
			t.Errorf("%s: %v, want %d", tc.name, got, tc.want)
		}
	}
}

func TestReadPrivateKeyPEM(t *testing.T) {
	cli := newIdentity(t, "p256", clientURI)
	b, err := readPrivateKeyPEM(cli.keyFile, cli.der)
	if err != nil || !strings.Contains(string(b), "PRIVATE KEY") {
		t.Fatalf("PEM key: %v", err)
	}
	junk := filepath.Join(t.TempDir(), "junk.key")
	if err := os.WriteFile(junk, []byte("junk"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readPrivateKeyPEM(junk, cli.der); err == nil {
		t.Error("a junk key was accepted")
	}
}

func TestReadPKIDir(t *testing.T) {
	dir := t.TempDir()
	// A file where trusted/certs should be a directory.
	if err := os.MkdirAll(filepath.Join(dir, "trusted"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "trusted", "certs"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readPKIDir(dir, &ua.TrustListDataType{}); err == nil {
		t.Error("trusted/certs as a file was accepted")
	}

	// Junk, hidden files and subdirectories.
	ok := t.TempDir()
	certs := filepath.Join(ok, "issuer", "certs")
	if err := os.MkdirAll(filepath.Join(certs, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(certs, ".hidden"), []byte("junk"), 0o600); err != nil {
		t.Fatal(err)
	}
	tl := &ua.TrustListDataType{}
	if m, err := readPKIDir(ok, tl); err != nil || m != ua.TrustListMasksIssuerCertificates {
		t.Errorf("readPKIDir = %v, %v", m, err)
	}
	if err := os.WriteFile(filepath.Join(certs, "junk.der"), []byte("junk"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readPKIDir(ok, tl); err == nil {
		t.Error("a junk file was accepted")
	}
}
