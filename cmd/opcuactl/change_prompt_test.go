// SPDX-License-Identifier: MIT

package opcuactl

import (
	"os"
	"strings"
	"testing"
)

// answerPrompt makes stdin a terminal that answers the confirmation prompt
// with answer, for the length of the test.
func answerPrompt(t *testing.T, answer string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin, isTerm := os.Stdin, stdinIsTerminal
	os.Stdin = r
	stdinIsTerminal = func() bool { return true }
	t.Cleanup(func() {
		os.Stdin, stdinIsTerminal = stdin, isTerm
		_ = r.Close()
	})
	go func() {
		_, _ = w.WriteString(answer + "\n")
		_ = w.Close()
	}()
}

// changeCommands are the commands that change a server, behind the
// confirmation prompt, with arguments that are valid before connecting.
var changeCommands = []struct {
	name, cancelled string
	args            []string
}{
	{"write attr", "Write cancelled.", []string{"write", "attr", "-n", "ns=1;s=x", "--attr", "DisplayName", "--value", "x"}},
	{"update-values", "Update cancelled.", []string{"history", "update-values", "-n", "ns=1;s=x", "--sample", "2020-01-01T00:00:00Z=1"}},
	{"delete-values range", "Delete cancelled.", []string{"history", "delete-values", "-n", "ns=1;s=x", "--start", "2020-01-01T00:00:00Z", "--end", "2020-01-02T00:00:00Z"}},
	{"delete-values at", "Delete cancelled.", []string{"history", "delete-values", "-n", "ns=1;s=x", "--at", "2020-01-01T00:00:00Z"}},
}

// TestChangePromptDeclined answers no: nothing is sent, exit 0.
func TestChangePromptDeclined(t *testing.T) {
	for _, c := range changeCommands {
		t.Run(c.name, func(t *testing.T) {
			answerPrompt(t, "n")
			_, errOut, err := runCLI(t, append(c.args, "-e", deadEndpoint)...)
			if err != nil {
				t.Fatalf("exit %d (%v)\n%s", ExitCode(err), err, errOut)
			}
			wantContains(t, errOut, "[y/N]", c.cancelled)
		})
	}
}

// TestChangeDeadEndpoint answers yes, or passes --yes, to a server that is
// not there: exit 4, transport.
func TestChangeDeadEndpoint(t *testing.T) {
	for _, c := range changeCommands {
		for _, how := range []string{"prompt", "--yes"} {
			t.Run(c.name+" "+how, func(t *testing.T) {
				args := append(c.args, "-e", deadEndpoint, "--timeout", "2s")
				if how == "prompt" {
					answerPrompt(t, "y")
				} else {
					args = append(args, "--yes")
				}
				_, errOut, err := runCLI(t, args...)
				if ExitCode(err) != exitTransport {
					t.Fatalf("exit %d (%v), want %d\n%s", ExitCode(err), err, exitTransport, errOut)
				}
				if strings.Contains(errOut, "cancelled") {
					t.Errorf("cancelled after yes:\n%s", errOut)
				}
			})
		}
	}
}
