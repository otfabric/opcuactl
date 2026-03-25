package conn

import (
	"context"
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
	tests := []struct {
		name string
		cfg  Config
		min  int // minimum option count
	}{
		{"anonymous_default", Config{}, 1},
		{"username", Config{Auth: "username", Username: "u", Password: "p"}, 1},
		{"security_policy", Config{SecurityPolicy: "Basic256Sha256"}, 2},
		{"security_policy_none_skipped", Config{SecurityPolicy: "none"}, 1},
		{"security_mode_sign", Config{SecurityMode: "Sign"}, 2},
		{"security_mode_sign_and_encrypt", Config{SecurityMode: "SignAndEncrypt"}, 2},
		{"security_mode_invalid_ignored", Config{SecurityMode: "not-a-mode"}, 1},
		{"certificate_auth", Config{Auth: "certificate", CertFile: "/tmp/x.crt", KeyFile: "/tmp/x.key"}, 2},
		{"anonymous_plus_client_cert", Config{CertFile: "/tmp/c.crt", KeyFile: "/tmp/c.key"}, 3},
		{"remote_server_cert", Config{ServerCertFile: "/tmp/srv.pem"}, 2},
		{"insecure_skip_verify", Config{InsecureSkipVerify: true}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := tt.cfg.buildOptions()
			if len(opts) < tt.min {
				t.Fatalf("buildOptions() returned %d opts, want at least %d", len(opts), tt.min)
			}
		})
	}
}
