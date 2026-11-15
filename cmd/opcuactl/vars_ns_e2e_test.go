// SPDX-License-Identifier: MIT

package opcuactl

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/otfabric/opcuactl/internal/model"
)

// --only-ns and --exclude-ns take a NamespaceIndex (0-65535); a larger value
// is a usage error before anything is sent, a negative one means no filter.
// The flags are an int: on a 32-bit platform a value above 2^31-1 is refused
// by the flag parser itself, also with exit 2.
func TestGetVarsNamespaceFlagRange(t *testing.T) {
	s := startServer(t)
	folder := s.ns.Objects().ID().String()
	ns := int64(s.ns.ID())
	for _, c := range []struct {
		value int64
		// ok: accepted; then only and exclude say whether the variables of
		// the test namespace are listed with --only-ns and --exclude-ns.
		ok            bool
		only, exclude bool
	}{
		{value: -2, ok: true, only: true, exclude: true},
		{value: -1, ok: true, only: true, exclude: true},
		{value: 0, ok: true, only: false, exclude: true},
		{value: ns, ok: true, only: true, exclude: false},
		{value: 65534, ok: true, only: false, exclude: true},
		{value: 65535, ok: true, only: false, exclude: true},
		{value: 65536},
		{value: 65536 + ns},
		{value: 1<<31 - 1},
		{value: 1<<32 + ns},
	} {
		for _, flag := range []string{"--only-ns", "--exclude-ns"} {
			t.Run(fmt.Sprintf("%s %d", flag, c.value), func(t *testing.T) {
				args := []string{"get", "vars", "-e", s.url, "-n", folder, "--no-read", "-f", "json", flag, fmt.Sprint(c.value)}
				if !c.ok {
					// A dead endpoint: the value must be refused before connecting.
					args[3] = deadEndpoint
					_, _, err := runCLI(t, args...)
					if got := ExitCode(err); got != exitUsage {
						t.Fatalf("exit %d, want %d (%v)", got, exitUsage, err)
					}
					if c.value <= math.MaxInt && (!strings.Contains(err.Error(), flag) || !strings.Contains(err.Error(), "0-65535")) {
						t.Errorf("error %q", err)
					}
					return
				}
				want := c.only
				if flag == "--exclude-ns" {
					want = c.exclude
				}
				vars := runJSON[[]model.VarNode](t, args...)
				listed := false
				for _, v := range vars {
					if strings.HasPrefix(v.NodeID, fmt.Sprintf("ns=%d;", ns)) {
						listed = true
					}
				}
				if listed != want {
					t.Errorf("variables of ns=%d listed: %v, want %v (%v)", ns, listed, want, vars)
				}
			})
		}
	}
}
