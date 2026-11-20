// SPDX-License-Identifier: MIT

package conn

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-opcua/server"
)

// TestConfig_NewChannel opens a SecureChannel without a session, with the
// ECC Sign legacy option of the config, and fails on a dead endpoint.
func TestConfig_NewChannel(t *testing.T) {
	t.Parallel()
	srv, err := server.New(server.InsecureNoSecurity(), server.EndPoint("localhost", 0))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := srv.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	cfg := Config{
		Endpoint: fmt.Sprintf("opc.tcp://localhost:%d", srv.Port()),
		Timeout:  5 * time.Second,
		Auth:     "anonymous",

		InsecureNoSecurity:     true,
		ECCSignLegacyKeyLength: true,
	}
	c, err := cfg.NewChannel(ctx)
	if err != nil {
		t.Fatalf("NewChannel: %v", err)
	}
	defer func() { _ = c.Close(ctx) }()
	if c.SecureChannel() == nil {
		t.Error("no SecureChannel")
	}
	if c.Session() != nil {
		t.Error("NewChannel created a session")
	}

	cfg.Endpoint, cfg.Timeout = "opc.tcp://127.0.0.1:1", 300*time.Millisecond
	if _, err := cfg.NewChannel(ctx); err == nil || !strings.Contains(err.Error(), "connecting") {
		t.Errorf("dead endpoint: %v", err)
	}
}
