// SPDX-License-Identifier: MIT

package opcuactl

import (
	"strings"
	"testing"

	"github.com/otfabric/opcuactl/internal/serve"
)

// TestE2E_VerboseInfo checks that the --verbose lines of the commands are
// printed on stderr without the "Error:" prefix of errors.
func TestE2E_VerboseInfo(t *testing.T) {
	s, ep := startDemo(t)
	demo := s.node(serve.DemoFolder)
	from, to := "2000-01-01T00:00:00Z", "2100-01-01T00:00:00Z"
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"browse", "path", "-e", ep, "--path", "Server", "-v"}, `resolved "Server" → i=2253`},
		{[]string{"find", "type", "-e", ep, "--data-type", "double", "--start-node", demo, "-v"}, "found "},
		{[]string{"export", "nodes", "-e", ep, "-n", demo, "--depth", "1", "-v"}, "exported "},
		{[]string{"export", "values", "-e", ep, "-n", s.node(serve.DemoInt32), "-v"}, "exported 1 value(s)"},
		{[]string{"history", "value", "-e", ep, "-n", s.node(serve.DemoSine), "--start", from, "--end", to, "-v"}, "historical value(s) for"},
		{[]string{"history", "event", "-e", ep, "-n", demo, "--start", from, "--end", to, "-v"}, "historical event(s) for"},
		{[]string{"monitor", "value", "-e", ep, "-n", s.node(serve.DemoInt32), "--duration", "200ms", "-v"}, "subscription "},
		{[]string{"monitor", "event", "-e", ep, "-n", "i=2253", "--duration", "200ms", "-v"}, "monitoring events on"},
	} {
		_, errOut := wantExit(t, 0, tc.args...)
		wantContains(t, errOut, tc.want)
		if strings.Contains(errOut, "Error:") {
			t.Errorf("opcuactl %s: info line printed as an error:\n%s", strings.Join(tc.args, " "), errOut)
		}
	}

	// Errors keep their prefix.
	_, errOut := wantExit(t, 6, "browse", "path", "-e", ep, "--path", "Nope", "-v")
	wantContains(t, errOut, "Error: ")
}
