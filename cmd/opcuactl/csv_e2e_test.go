// SPDX-License-Identifier: MIT

package opcuactl

import (
	"encoding/csv"
	"strings"
	"testing"
)

// TestE2E_CSVOutput runs the read-only commands with -f csv: each writes
// parseable CSV with a header row, also when it writes structured data.
func TestE2E_CSVOutput(t *testing.T) {
	_, ep := startDemo(t)
	for _, args := range [][]string{
		{"get", "value", "-n", "i=2256"},
		{"get", "value", "-n", "i=2258", "-n", "i=2259"},
		{"get", "attr", "-n", "i=2256", "--all"},
		{"get", "node", "-n", "i=2256"},
		{"get", "server"},
		{"get", "vars", "-n", "i=2256"},
		{"browse", "children", "-n", "i=85"},
		{"browse", "refs", "-n", "i=85"},
		{"browse", "tree", "-n", "i=2253", "--depth", "1"},
		{"browse", "path", "--path", "Demo.Add", "--namespace", "1"},
		{"find", "namespace"},
		{"find", "node", "--name", "ServerStatus"},
		{"diagnostics", "summary"},
		{"discover", "endpoints"},
		{"ping"},
	} {
		args := append(args, "-e", ep, "-f", "csv")
		out, errOut, err := runCLI(t, args...)
		name := strings.Join(args, " ")
		if err != nil {
			t.Errorf("opcuactl %s: %v\nstderr: %s", name, err, errOut)
			continue
		}
		recs, err := csv.NewReader(strings.NewReader(out)).ReadAll()
		if err != nil || len(recs) < 2 {
			t.Errorf("opcuactl %s: not CSV with a header and a row (%v):\n%s", name, err, out)
		}
	}
}
