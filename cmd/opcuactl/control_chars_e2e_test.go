// SPDX-License-Identifier: MIT

package opcuactl

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/otfabric/go-opcua/id"
)

// ctrlValue holds one character of each class text output escapes.
const ctrlValue = "a\x1b[2Jb\rc\nd\te\x7ff\u009bg\u202eh\u2066i"

// terminalControl returns the first rune of out, other than a line end,
// that a terminal acts on.
func terminalControl(out string) (rune, bool) {
	for _, r := range out {
		if (r < 0x20 && r != '\n') || r == 0x7f || (r >= 0x80 && r <= 0x9f) ||
			(r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069) || r == utf8.RuneError {
			return r, true
		}
	}
	return 0, false
}

// Text output shows a string value from the server with its control
// characters escaped, on one line; JSON output keeps the value as it is.
func TestTextOutputEscapesControlCharacters(t *testing.T) {
	s := startServer(t)
	s.ns.Objects().AddRef(s.ns.AddNewVariableStringNode("Ctrl", ctrlValue), id.HasComponent, true)
	node := s.node("Ctrl")
	escaped := `a\x1b[2Jb\rc\nd\te\x7ff\u009bg\u202eh\u2066i`

	for _, args := range [][]string{
		{"get", "value", "-n", node},
		{"get", "value", "-n", node, "-q"},
		{"get", "vars", "-n", s.ns.Objects().ID().String()},
		{"monitor", "value", "-n", node, "--count", "1", "--duration", "20s"},
	} {
		t.Run(strings.Join(append(args[:2:2], args[4:]...), " "), func(t *testing.T) {
			out := mustRun(t, append(args, "-e", s.url)...)
			if r, found := terminalControl(out); found {
				t.Errorf("stdout holds %U:\n%q", r, out)
			}
			if !strings.Contains(out, escaped) {
				t.Errorf("stdout does not show %s:\n%s", escaped, out)
			}
			for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
				if strings.Contains(line, "i") && strings.HasPrefix(strings.TrimSpace(line), "d") {
					t.Errorf("the value starts a line of its own: %q", line)
				}
			}
		})
	}

	out := mustRun(t, "get", "value", "-e", s.url, "-n", node, "-f", "json")
	var v struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("%v:\n%s", err, out)
	}
	if v.Value != ctrlValue {
		t.Errorf("JSON value %q, want %q", v.Value, ctrlValue)
	}
}
