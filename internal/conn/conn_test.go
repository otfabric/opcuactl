// SPDX-License-Identifier: MIT

package conn

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-opcua/ua"
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
		{"security_policy_with_server_cert", Config{SecurityPolicy: "Basic256Sha256", ServerCertFile: "/tmp/srv.pem"}, 3, ""},
		{"security_policy_needs_endpoint", Config{SecurityPolicy: "Basic256Sha256"}, 0, "--endpoint"},
		{"reverse_connect", Config{ReverseListen: "127.0.0.1:0", ReverseServerURI: "urn:srv"}, 2, ""},
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

func TestConfig_RequireEndpoint(t *testing.T) {
	t.Parallel()
	if err := (&Config{}).RequireEndpoint(); err == nil {
		t.Error("expected an error without endpoint and reverse listener")
	}
	if err := (&Config{Endpoint: "opc.tcp://h:4840"}).RequireEndpoint(); err != nil {
		t.Errorf("endpoint set: %v", err)
	}
	// With reverse connect the server announces the endpoint.
	if err := (&Config{ReverseListen: "127.0.0.1:4841"}).RequireEndpoint(); err != nil {
		t.Errorf("reverse listener set: %v", err)
	}
}

func TestConfig_DiscoveryOptions(t *testing.T) {
	t.Parallel()
	if n := len((&Config{}).DiscoveryOptions()); n != 0 {
		t.Errorf("empty config: %d options, want 0", n)
	}
	if n := len((&Config{Timeout: time.Second}).DiscoveryOptions()); n != 2 {
		t.Errorf("timeout: %d options, want 2", n)
	}
	if n := len((&Config{ReverseListen: "127.0.0.1:0", Timeout: time.Second}).DiscoveryOptions()); n != 3 {
		t.Errorf("reverse + timeout: %d options, want 3", n)
	}
}

func TestTransport(t *testing.T) {
	t.Parallel()
	for url, want := range map[string]string{
		"opc.tcp://h:4840":   ua.TransportProfileUATCP,
		"opc.wss://h:4843":   ua.TransportProfileWSS,
		"opc.https://h:4443": ua.TransportProfileHTTPS,
		"https://h":          ua.TransportProfileHTTPS,
		"":                   "",
		"carrier-pigeon://x": "",
	} {
		if got := (&Config{Endpoint: url}).Transport(); got != want {
			t.Errorf("Transport(%q) = %q, want %q", url, got, want)
		}
	}

	tcp := &ua.EndpointDescription{EndpointURL: "opc.tcp://h:4840"}
	tcpProfile := &ua.EndpointDescription{EndpointURL: "opc.tcp://h:4840", TransportProfileURI: ua.TransportProfileUATCP}
	wss := &ua.EndpointDescription{EndpointURL: "opc.wss://h:4843", TransportProfileURI: ua.TransportProfileWSS}
	https := &ua.EndpointDescription{EndpointURL: "opc.https://h:4443"}
	odd := &ua.EndpointDescription{EndpointURL: "x://h"}
	all := []*ua.EndpointDescription{tcp, tcpProfile, wss, nil, https, odd}
	if got := EndpointsForTransport(all, ua.TransportProfileUATCP); len(got) != 3 || got[0] != tcp || got[1] != tcpProfile || got[2] != odd {
		t.Errorf("tcp endpoints = %v", got)
	}
	if got := EndpointsForTransport(all, ua.TransportProfileHTTPS); len(got) != 1 || got[0] != https {
		t.Errorf("https endpoints = %v", got)
	}
	if got := EndpointsForTransport(all, ""); len(got) != len(all) {
		t.Errorf("all endpoints = %v", got)
	}

	for uri, want := range map[string]string{
		ua.TransportProfileUATCP: "opc.tcp", "": "opc.tcp", ua.TransportProfileWSS: "opc.wss",
		ua.TransportProfileHTTPS: "opc.https", "urn:x": "urn:x",
	} {
		if got := TransportName(uri); got != want {
			t.Errorf("TransportName(%q) = %q, want %q", uri, got, want)
		}
	}

	for in, want := range map[string]string{
		"": "", "tcp": ua.TransportProfileUATCP, "opc.tcp": ua.TransportProfileUATCP, "uatcp": ua.TransportProfileUATCP,
		"WSS": ua.TransportProfileWSS, "websocket": ua.TransportProfileWSS, "opc.wss": ua.TransportProfileWSS,
		"https": ua.TransportProfileHTTPS, ua.TransportProfileHTTPS: ua.TransportProfileHTTPS,
		"urn:other://profile": "urn:other://profile",
	} {
		got, err := ParseTransport(in)
		if err != nil || got != want {
			t.Errorf("ParseTransport(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := ParseTransport("pigeon"); err == nil {
		t.Error("ParseTransport(pigeon) succeeded")
	}
}

func TestSelectEndpoint_transport(t *testing.T) {
	t.Parallel()
	tcp := &ua.EndpointDescription{EndpointURL: "opc.tcp://h:4840", SecurityPolicyURI: ua.SecurityPolicyURINone, SecurityMode: ua.MessageSecurityModeNone, SecurityLevel: 1}
	wss := &ua.EndpointDescription{EndpointURL: "opc.wss://h:4843", TransportProfileURI: ua.TransportProfileWSS, SecurityPolicyURI: ua.SecurityPolicyURINone, SecurityMode: ua.MessageSecurityModeNone, SecurityLevel: 2}
	eps := []*ua.EndpointDescription{tcp, wss}

	ep, err := (&Config{Endpoint: "opc.tcp://h:4840"}).SelectEndpoint(eps, "", ua.MessageSecurityModeInvalid)
	if err != nil || ep != tcp {
		t.Errorf("tcp: %v, %v", ep, err)
	}
	ep, err = (&Config{Endpoint: "opc.wss://h:4843"}).SelectEndpoint(eps, "", ua.MessageSecurityModeInvalid)
	if err != nil || ep != wss {
		t.Errorf("wss: %v, %v", ep, err)
	}
	if _, err := (&Config{Endpoint: "opc.https://h:4443"}).SelectEndpoint(eps, "", ua.MessageSecurityModeInvalid); err == nil || !strings.Contains(err.Error(), "no opc.https endpoint") {
		t.Errorf("https: %v", err)
	}
}

func TestTLSConfig(t *testing.T) {
	t.Parallel()
	if tc, err := (&Config{}).TLSConfig(); tc != nil || err != nil {
		t.Errorf("no TLS settings: %v, %v", tc, err)
	}
	if opts := (&Config{}).DiscoveryOptions(); len(opts) != 0 {
		t.Errorf("DiscoveryOptions without settings = %d options", len(opts))
	}

	dir := t.TempDir()
	certFile, keyFile := writeTestCertKey(t, dir)

	tc, err := (&Config{TLSCAFile: certFile, TLSCertFile: certFile, TLSKeyFile: keyFile}).TLSConfig()
	if err != nil {
		t.Fatalf("TLSConfig: %v", err)
	}
	if tc.RootCAs == nil || len(tc.Certificates) != 1 || tc.InsecureSkipVerify || tc.MinVersion != tls.VersionTLS12 {
		t.Errorf("tls config = %+v", tc)
	}

	// A DER CA file.
	b, _ := os.ReadFile(certFile)
	block, _ := pem.Decode(b)
	der := filepath.Join(dir, "ca.der")
	if err := os.WriteFile(der, block.Bytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if tc, err := (&Config{TLSCAFile: der}).TLSConfig(); err != nil || tc.RootCAs == nil {
		t.Errorf("DER CA: %v", err)
	}

	if tc, err := (&Config{TLSInsecureSkipVerify: true}).TLSConfig(); err != nil || !tc.InsecureSkipVerify {
		t.Errorf("insecure: %+v, %v", tc, err)
	}

	for _, cfg := range []Config{
		{TLSCAFile: filepath.Join(dir, "missing.pem")},
		{TLSCAFile: keyFile},
		{TLSCertFile: certFile},
		{TLSKeyFile: keyFile},
		{TLSCertFile: keyFile, TLSKeyFile: keyFile},
	} {
		if _, err := cfg.TLSConfig(); err == nil {
			t.Errorf("%+v: no error", cfg)
		}
	}

	// A TLS setting that does not load fails the client.
	bad := Config{Endpoint: "opc.wss://127.0.0.1:1", TLSCAFile: filepath.Join(dir, "missing.pem"), Timeout: time.Second, ReturnDiagnostics: ua.ReturnDiagnosticsAll}
	if opts := bad.DiscoveryOptions(); len(opts) != 4 {
		t.Errorf("DiscoveryOptions = %d options, want TLS, diagnostics and the two timeouts", len(opts))
	}
	if _, err := bad.NewClient(context.Background()); err == nil || !strings.Contains(err.Error(), "--tls-ca") {
		t.Errorf("NewClient with a missing --tls-ca: %v", err)
	}
}
