// SPDX-License-Identifier: MIT

package conn

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-opcua/server"
	"github.com/otfabric/go-opcua/ua"
)

func readPEMBlock(t *testing.T, file string) *pem.Block {
	t.Helper()
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(b)
	if block == nil {
		t.Fatalf("%s: no PEM", file)
	}
	return block
}

func writeFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(f, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestLoadCertificatesFile(t *testing.T) {
	t.Parallel()
	certA, keyA := writeTestCertKey(t, t.TempDir())
	certB, _ := writeTestCertKey(t, t.TempDir())
	a, b := readPEMBlock(t, certA), readPEMBlock(t, certB)

	// A key in front of two certificates: the key is skipped.
	bundle := append(pem.EncodeToMemory(readPEMBlock(t, keyA)), append(pem.EncodeToMemory(a), pem.EncodeToMemory(b)...)...)
	certs, err := loadCertificatesFile(writeFile(t, "bundle.pem", bundle))
	if err != nil || len(certs) != 2 || string(certs[0].Raw) != string(a.Bytes) || string(certs[1].Raw) != string(b.Bytes) {
		t.Errorf("bundle: %d certificates, %v", len(certs), err)
	}

	// DER, as OPC UA servers store their certificates.
	certs, err = loadCertificatesFile(writeFile(t, "cert.der", a.Bytes))
	if err != nil || len(certs) != 1 || string(certs[0].Raw) != string(a.Bytes) {
		t.Errorf("DER: %d certificates, %v", len(certs), err)
	}

	for name, data := range map[string][]byte{
		"garbage.pem": []byte("not a certificate"),
		"broken.pem":  pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte{0x30, 0x03, 0x01}}),
		"key.pem":     pem.EncodeToMemory(readPEMBlock(t, keyA)),
	} {
		if _, err := loadCertificatesFile(writeFile(t, name, data)); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	if _, err := loadCertificatesFile(filepath.Join(t.TempDir(), "missing.pem")); err == nil {
		t.Error("missing file: no error")
	}
}

func TestLoadTrustedCertificates(t *testing.T) {
	t.Parallel()
	certA, _ := writeTestCertKey(t, t.TempDir())
	certB, _ := writeTestCertKey(t, t.TempDir())
	certs, err := loadTrustedCertificates([]string{certA, certB})
	if err != nil || len(certs) != 2 {
		t.Errorf("two files: %d certificates, %v", len(certs), err)
	}
	missing := filepath.Join(t.TempDir(), "missing.pem")
	if _, err := loadTrustedCertificates([]string{certA, missing}); err == nil || !strings.Contains(err.Error(), missing) {
		t.Errorf("missing file: %v", err)
	}
	if _, err := loadTrustedCertificates(nil); err == nil || !strings.Contains(err.Error(), "no certificates") {
		t.Errorf("no files: %v", err)
	}
}

// TestSelectEndpoint_certificate pairs the client certificate only with
// the endpoints its key type can use.
func TestSelectEndpoint_certificate(t *testing.T) {
	t.Parallel()
	rsaEP := &ua.EndpointDescription{EndpointURL: "opc.tcp://h:4840", SecurityPolicyURI: ua.SecurityPolicyURIBasic256Sha256, SecurityMode: ua.MessageSecurityModeSignAndEncrypt, SecurityLevel: 10}
	eccEP := &ua.EndpointDescription{EndpointURL: "opc.tcp://h:4840", SecurityPolicyURI: ua.SecurityPolicyURIECCNistP256, SecurityMode: ua.MessageSecurityModeSignAndEncrypt, SecurityLevel: 20}
	eps := []*ua.EndpointDescription{rsaEP, eccEP}

	rsaCert, _ := writeTestCertKey(t, t.TempDir())
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	eccCert, _ := writeCertKey(t, key)

	for cert, want := range map[string]*ua.EndpointDescription{"": eccEP, rsaCert: rsaEP, eccCert: eccEP} {
		ep, err := (&Config{Endpoint: "opc.tcp://h:4840", CertFile: cert}).SelectEndpoint(eps, "", ua.MessageSecurityModeInvalid)
		if err != nil || ep != want {
			t.Errorf("--cert %q: %v, %v, want %s", cert, ep, err, want.SecurityPolicyURI)
		}
	}
	if _, err := (&Config{Endpoint: "opc.tcp://h:4840", CertFile: filepath.Join(t.TempDir(), "missing.pem")}).SelectEndpoint(eps, "", ua.MessageSecurityModeInvalid); err == nil || !strings.Contains(err.Error(), "loading --cert") {
		t.Errorf("missing --cert: %v", err)
	}
}

// TestSecurityOptions resolves auto and explicit security settings against
// a server that offers None only.
func TestSecurityOptions(t *testing.T) {
	t.Parallel()
	srv, err := server.New(
		server.EndPoint("localhost", 0),
		server.InsecureNoSecurity(),
		server.EnableAuthMode(ua.UserTokenTypeAnonymous),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	endpoint := fmt.Sprintf("opc.tcp://localhost:%d", srv.Port())
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	opts, err := (&Config{Endpoint: endpoint, SecurityPolicy: "auto", SecurityMode: "auto"}).securityOptions(ctx, ua.UserTokenTypeAnonymous)
	if err != nil || len(opts) != 1 {
		t.Errorf("auto: %d options, %v", len(opts), err)
	}

	for _, tc := range []struct {
		cfg  Config
		want string
	}{
		{Config{SecurityPolicy: "auto", SecurityMode: "Bogus"}, "invalid --security-mode"},
		{Config{SecurityPolicy: "Basic256Sha256", SecurityMode: "SignAndEncrypt"}, "selecting endpoint"},
		{Config{SecurityPolicy: "auto", SecurityMode: "auto", CertFile: filepath.Join(t.TempDir(), "missing.pem")}, "loading --cert"},
	} {
		tc.cfg.Endpoint = endpoint
		if _, err := tc.cfg.securityOptions(ctx, ua.UserTokenTypeAnonymous); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%+v: %v, want %q", tc.cfg, err, tc.want)
		}
	}

	// Without an endpoint auto cannot look the endpoints up.
	if _, err := (&Config{SecurityPolicy: "auto"}).securityOptions(ctx, ua.UserTokenTypeAnonymous); err == nil {
		t.Error("auto without an endpoint: no error")
	}
	// An unreachable server.
	if _, err := (&Config{Endpoint: "opc.tcp://127.0.0.1:1", SecurityPolicy: "auto", Timeout: time.Second}).securityOptions(ctx, ua.UserTokenTypeAnonymous); err == nil || !strings.Contains(err.Error(), "discovering endpoints") {
		t.Errorf("unreachable: %v", err)
	}

	// An explicit policy with --server-cert needs no lookup.
	serverCert, _ := writeTestCertKey(t, t.TempDir())
	opts, err = (&Config{Endpoint: "opc.tcp://127.0.0.1:1", SecurityPolicy: "Basic256Sha256", SecurityMode: "Sign", ServerCertFile: serverCert}).securityOptions(ctx, ua.UserTokenTypeAnonymous)
	if err != nil || len(opts) != 2 {
		t.Errorf("explicit: %d options, %v", len(opts), err)
	}
	opts, err = (&Config{SecurityPolicy: "None", SecurityMode: "None"}).securityOptions(ctx, ua.UserTokenTypeAnonymous)
	if err != nil || len(opts) != 1 {
		t.Errorf("None: %d options, %v", len(opts), err)
	}
}
