// SPDX-License-Identifier: MIT

package conn

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestConfig_NewClient_emptyEndpoint(t *testing.T) {
	t.Parallel()
	_, err := (&Config{}).NewClient(context.Background())
	if err == nil {
		t.Fatal("expected error for empty endpoint")
	}
	if !strings.Contains(err.Error(), "endpoint") {
		t.Errorf("error = %q, want mention of endpoint", err)
	}
}

func TestConfig_NewClient_connectionRefused(t *testing.T) {
	t.Parallel()
	cfg := Config{
		Endpoint: "opc.tcp://127.0.0.1:1",
		Timeout:  300 * time.Millisecond,
		Auth:     "anonymous",
	}
	_, err := cfg.NewClient(context.Background())
	if err == nil {
		t.Fatal("expected connect error")
	}
	if !strings.Contains(err.Error(), "connecting") {
		t.Errorf("error = %q, want connecting failure", err)
	}
}

func TestConfig_TimeoutContext(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cfg := Config{}
	cctx, cancel := cfg.TimeoutContext(ctx)
	defer cancel()
	if cctx != ctx {
		t.Error("with zero timeout, context should be unchanged")
	}
	cancel() // no-op func

	cfg.Timeout = 500 * time.Millisecond
	cctx, cancel = cfg.TimeoutContext(ctx)
	defer cancel()
	if _, ok := cctx.Deadline(); !ok {
		t.Fatal("expected deadline when timeout is set")
	}
}

func TestConfig_buildOptions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tests := []struct {
		name    string
		cfg     Config
		min     int
		wantErr string
	}{
		{"anonymous_default", Config{}, 1, ""},
		{"username", Config{Auth: "username", Username: "u", Password: "p"}, 1, ""},
		{"username_missing", Config{Auth: "username"}, 0, "--username"},
		{"security_policy", Config{SecurityPolicy: "Basic256Sha256"}, 2, ""},
		{"security_policy_none_skipped", Config{SecurityPolicy: "none"}, 1, ""},
		{"security_mode_sign", Config{SecurityMode: "Sign"}, 2, ""},
		{"security_mode_sign_and_encrypt", Config{SecurityMode: "SignAndEncrypt"}, 2, ""},
		{"security_mode_invalid_ignored", Config{SecurityMode: "not-a-mode"}, 1, ""},
		{"unknown_auth", Config{Auth: "kerberos"}, 0, "unknown --auth"},
		{"issued_missing_token", Config{Auth: "issued"}, 0, "--token"},
		{"issued_token", Config{Auth: "issued", Token: "c2VjcmV0"}, 1, ""},
		{"auth_policy_id", Config{AuthPolicyID: "anonymous"}, 2, ""},
		{"remote_server_cert", Config{ServerCertFile: "/tmp/srv.pem"}, 2, ""},
		{"insecure_skip_verify", Config{InsecureSkipVerify: true}, 2, ""},
		{"cert_without_key", Config{CertFile: "/tmp/c.crt"}, 0, "together"},
		{"certificate_missing_files", Config{Auth: "certificate"}, 0, "--cert and --key"},
		{"auto_needs_endpoint", Config{SecurityPolicy: "auto"}, 0, "--endpoint"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts, err := tt.cfg.buildOptions(ctx)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %q, want substring %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("buildOptions() unexpected error: %v", err)
			}
			if len(opts) < tt.min {
				t.Fatalf("buildOptions() returned %d opts, want at least %d", len(opts), tt.min)
			}
		})
	}
}

func TestConfig_buildOptions_certificateAuth(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	certPath, keyPath := writeTestCertKey(t, dir)

	cfg := Config{
		Auth:     "certificate",
		CertFile: certPath,
		KeyFile:  keyPath,
	}
	opts, err := cfg.buildOptions(context.Background())
	if err != nil {
		t.Fatalf("buildOptions: %v", err)
	}
	// AuthCertificate + AuthPrivateKey + CertificateFile + PrivateKeyFile
	if len(opts) < 4 {
		t.Fatalf("got %d opts, want at least 4 for certificate auth + channel certs", len(opts))
	}
}

func TestConfig_buildOptions_trustedCert(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	certPath, _ := writeTestCertKey(t, dir)

	cfg := Config{TrustedCertFiles: []string{certPath}}
	opts, err := cfg.buildOptions(context.Background())
	if err != nil {
		t.Fatalf("buildOptions: %v", err)
	}
	if len(opts) < 2 { // AuthAnonymous + TrustedCertificates
		t.Fatalf("got %d opts, want at least 2", len(opts))
	}
}

func TestConfig_buildOptions_issuedTokenFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "token.txt")
	if err := os.WriteFile(path, []byte("  issued-token-data  \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := Config{Auth: "issuedtoken", TokenFile: path}
	opts, err := cfg.buildOptions(context.Background())
	if err != nil {
		t.Fatalf("buildOptions: %v", err)
	}
	if len(opts) < 1 {
		t.Fatal("expected AuthIssuedToken option")
	}
}

func TestDecodeToken(t *testing.T) {
	t.Parallel()
	if got := string(decodeToken("plain")); got != "plain" {
		t.Fatalf("plain = %q", got)
	}
	if got := string(decodeToken("c2VjcmV0")); got != "secret" {
		t.Fatalf("base64 = %q, want secret", got)
	}
}

func writeTestCertKey(t *testing.T, dir string) (certPath, keyPath string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "opcuactl-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPath = filepath.Join(dir, "client.crt")
	keyPath = filepath.Join(dir, "client.key")
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}), 0o600); err != nil {
		t.Fatal(err)
	}
	return certPath, keyPath
}
