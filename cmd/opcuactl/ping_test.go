// SPDX-License-Identifier: MIT

package opcuactl

import (
	"context"
	"net"
	"strings"
	"testing"
)

func TestDialTCP(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	addr := l.Addr().String()
	for _, ep := range []string{"opc.tcp://" + addr, "opc.wss://" + addr, "opc.https://" + addr + "/path"} {
		c, err := dialTCP(context.Background(), ep)
		if err != nil {
			t.Errorf("dialTCP(%s): %v", ep, err)
			continue
		}
		_ = c.Close()
	}
	for _, ep := range []string{"opc.wss:///nohost", "opc.https://%zz", "pigeon://x"} {
		if _, err := dialTCP(context.Background(), ep); err == nil {
			t.Errorf("dialTCP(%s) succeeded", ep)
		}
	}
	if _, err := dialTCP(context.Background(), "opc.wss:///x"); err == nil || !strings.Contains(err.Error(), "missing host") {
		t.Errorf("missing host: %v", err)
	}
}
