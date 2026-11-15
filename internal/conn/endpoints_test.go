// SPDX-License-Identifier: MIT

package conn

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/otfabric/go-opcua/ua"
)

// Only go-opcua's comparison of the CreateSession endpoints with the
// discovered ones gets the hint; BadSecurityChecksFailed for another reason,
// such as a server that does not accept the client certificate, keeps its
// message.
func TestEndpointsDiffer(t *testing.T) {
	for _, c := range []struct {
		name string
		err  error
		hint bool
	}{
		{"discovered endpoint missing", fmt.Errorf("opcua: the server endpoints of CreateSession do not contain the discovered endpoint x: %w", ua.StatusBadSecurityChecksFailed), true},
		{"endpoint not discovered", fmt.Errorf("opcua: the server endpoints of CreateSession contain the endpoint x that discovery did not return: %w", ua.StatusBadSecurityChecksFailed), true},
		{"selected endpoint missing", fmt.Errorf("opcua: the server endpoints of CreateSession do not contain the selected endpoint (p, m): %w", ua.StatusBadSecurityChecksFailed), true},
		{"wrapped", fmt.Errorf("session: %w", fmt.Errorf("opcua: the server endpoints of CreateSession do not contain the discovered endpoint x: %w", ua.StatusBadSecurityChecksFailed)), true},
		{"status alone", ua.StatusBadSecurityChecksFailed, false},
		{"signature", fmt.Errorf("opcua: verify session signature: %w", ua.StatusBadSecurityChecksFailed), false},
		{"other status", fmt.Errorf("opcua: the server endpoints of CreateSession: %w", ua.StatusBadCertificateInvalid), false},
		{"nil", nil, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := endpointsDiffer(c.err)
			if !c.hint {
				if got != nil {
					t.Fatalf("hint for %v: %v", c.err, got)
				}
				return
			}
			var d *EndpointsDifferError
			if !errors.As(got, &d) {
				t.Fatalf("no hint for %v", c.err)
			}
			if !errors.Is(got, ua.StatusBadSecurityChecksFailed) || !errors.Is(got, c.err) {
				t.Errorf("%v does not wrap %v", got, c.err)
			}
			msg := got.Error()
			for _, w := range []string{"the endpoints of GetEndpoints and CreateSession differ", "no session was activated",
				"--security-policy, --security-mode and --server-cert", c.err.Error()} {
				if !strings.Contains(msg, w) {
					t.Errorf("message does not contain %q: %s", w, msg)
				}
			}
		})
	}
}
