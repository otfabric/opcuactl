// SPDX-License-Identifier: MIT

package opcuactl

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// leafCommands returns the runnable commands below cmd.
func leafCommands(cmd *cobra.Command) []*cobra.Command {
	var out []*cobra.Command
	for _, sub := range cmd.Commands() {
		if sub.Runnable() && !sub.HasSubCommands() {
			out = append(out, sub)
		}
		out = append(out, leafCommands(sub)...)
	}
	return out
}

// TestNoSilentErrors: every failing command says why on stderr, once.
func TestNoSilentErrors(t *testing.T) {
	for _, c := range leafCommands(rootCmd) {
		path := strings.Fields(c.CommandPath())[1:]
		t.Run(strings.Join(path, "_"), func(t *testing.T) {
			_, stderr, err := runCLI(t, append(path, "--no-such-flag")...)
			if err == nil {
				t.Fatal("an unknown flag must fail")
			}
			if n := strings.Count(stderr, "Error:"); n != 1 {
				t.Fatalf("want one error line, got %d: %q", n, stderr)
			}
			if !strings.Contains(stderr, "no-such-flag") {
				t.Fatalf("stderr %q does not name the flag", stderr)
			}
		})
	}
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"call", "method"}, "--object is required"},
		{[]string{"monitor", "value"}, "--node"},
		{[]string{"no-such-command"}, "unknown command"},
	} {
		t.Run(strings.Join(tc.args, "_"), func(t *testing.T) {
			_, stderr, err := runCLI(t, tc.args...)
			if err == nil {
				t.Fatal("want an error")
			}
			if n := strings.Count(stderr, "Error:"); n != 1 || !strings.Contains(stderr, tc.want) {
				t.Fatalf("want one error line with %q, got %q", tc.want, stderr)
			}
		})
	}
}
