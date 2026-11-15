// SPDX-License-Identifier: MIT

package opcuactl

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// exampleCommand is one command line of an example: lines that end in a
// backslash and an array assignment over several lines are joined.
type exampleCommand struct {
	where string // file:line or command path
	text  string
}

// arrayStart matches the first line of a shell array assignment.
var arrayStart = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*=\(`)

// joinCommands joins the lines of one example block into command lines.
func joinCommands(where func(line int) string, lines []string, first int) []exampleCommand {
	var out []exampleCommand
	var cur strings.Builder
	start, open := 0, false
	for i, line := range lines {
		if cur.Len() == 0 {
			start = i
		}
		trimmed := strings.TrimSpace(line)
		cur.WriteString(strings.TrimSuffix(trimmed, `\`))
		cur.WriteByte(' ')
		if !open && arrayStart.MatchString(trimmed) {
			open = !strings.Contains(trimmed, ")")
		} else if open && strings.Contains(trimmed, ")") {
			open = false
		}
		if strings.HasSuffix(trimmed, `\`) || open {
			continue
		}
		out = append(out, exampleCommand{where(first + start), strings.TrimSpace(cur.String())})
		cur.Reset()
	}
	if cur.Len() > 0 {
		out = append(out, exampleCommand{where(first + start), strings.TrimSpace(cur.String())})
	}
	return out
}

// documentExamples returns the command lines of the code blocks of the
// documents and of the examples in the help of every command.
func documentExamples(t *testing.T) []exampleCommand {
	t.Helper()
	docs := []string{"README.md", "RECIPES.md", "EXAMPLES.md", "SECURITY.md"}
	more, err := filepath.Glob(filepath.Join("..", "..", "docs", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range more {
		docs = append(docs, filepath.Join("docs", filepath.Base(m)))
	}
	var out []exampleCommand
	for _, doc := range docs {
		b, err := os.ReadFile(filepath.Join("..", "..", doc))
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
		at := func(line int) string { return doc + ":" + strconv.Itoa(line+1) }
		var block []string
		first, in := 0, false
		for i, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), "```") {
				if in {
					out = append(out, joinCommands(at, block, first)...)
					block = nil
				}
				in, first = !in, i+1
				continue
			}
			if in {
				block = append(block, line)
			}
		}
	}
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		help := strings.ReplaceAll(c.Long, "\r\n", "\n")
		if i := strings.Index(help, "Examples:\n"); i >= 0 {
			help = help[i+len("Examples:\n"):]
		} else {
			help = ""
		}
		help += "\n" + strings.ReplaceAll(c.Example, "\r\n", "\n")
		path := c.CommandPath()
		out = append(out, joinCommands(func(int) string { return path }, strings.Split(help, "\n"), 0)...)
		for _, s := range c.Commands() {
			walk(s)
		}
	}
	walk(rootCmd)
	if len(out) < 100 {
		t.Fatalf("%d example command lines found", len(out))
	}
	return out
}

// No example puts a secret on the command line: they read it from a file
// or the environment.
func TestExamplesKeepSecretsOffTheCommandLine(t *testing.T) {
	secret := regexp.MustCompile(`--(password|token|pfx-password|key-password|broker-password|mqtt-password)(=|\s+)([^\s-]\S*)`)
	for _, c := range documentExamples(t) {
		if m := secret.FindStringSubmatch(c.text); m != nil {
			t.Errorf("%s: --%s %s on the command line: %s", c.where, m[1], m[3], c.text)
		}
	}
}

// Every example with an explicit secure --security-mode trusts the server
// certificate, directly or through the array it uses.
func TestSecureExamplesTrustTheServer(t *testing.T) {
	secure := regexp.MustCompile(`--security-mode[= ]"?(Sign|SignAndEncrypt)\b`)
	trusts := func(s string) bool {
		return strings.Contains(s, "--trusted-cert") || strings.Contains(s, "--insecure-skip-verify")
	}
	checked := 0
	for _, c := range documentExamples(t) {
		if !secure.MatchString(c.text) || !strings.Contains(c.text, "-e ") && !strings.Contains(c.text, "--endpoint") {
			continue
		}
		checked++
		if !trusts(c.text) {
			t.Errorf("%s: no --trusted-cert: %s", c.where, c.text)
		}
	}
	if checked == 0 {
		t.Fatal("no secure example found")
	}
}
