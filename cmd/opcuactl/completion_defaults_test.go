// SPDX-License-Identifier: MIT

package opcuactl

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// complete runs the hidden __complete command and returns the candidates
// (without descriptions) and the directive line.
func complete(t *testing.T, args ...string) ([]string, string) {
	t.Helper()
	out := mustRun(t, append([]string{"__complete"}, args...)...)
	var values []string
	var directive string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.HasPrefix(line, ":") {
			directive = line
			continue
		}
		v, _, _ := strings.Cut(line, "\t")
		values = append(values, v)
	}
	return values, directive
}

func wantCandidates(t *testing.T, args []string, want ...string) {
	t.Helper()
	got, _ := complete(t, args...)
	for _, w := range want {
		if !slices.Contains(got, w) {
			t.Errorf("__complete %v = %v, missing %q", args, got, w)
		}
	}
}

func TestCompleteShells(t *testing.T) {
	got, _ := complete(t, "completion", "")
	if want := []string{"bash", "zsh", "fish", "powershell"}; !slices.Equal(got, want) {
		t.Fatalf("completion shells %v, want %v", got, want)
	}
}

// TestCompleteEnumFlagPerGroup checks one enum flag of every command group.
func TestCompleteEnumFlagPerGroup(t *testing.T) {
	wantCandidates(t, []string{"get", "value", "--format", ""}, "table", "json", "yaml", "csv", "jsonl")
	wantCandidates(t, []string{"discover", "endpoints", "--security-mode", ""}, "None", "Sign", "SignAndEncrypt")
	wantCandidates(t, []string{"discover", "servers", "--capability", ""}, "DA", "HD", "LDS")
	wantCandidates(t, []string{"browse", "refs", "--direction", ""}, "forward", "inverse", "both")
	wantCandidates(t, []string{"find", "node", "--endpoint", "opc"}, "opc.tcp://", "opc.wss://", "opc.https://")
	wantCandidates(t, []string{"write", "value", "--type", ""}, "bool", "double", "string")
	wantCandidates(t, []string{"monitor", "value", "--trigger", ""}, "status", "status-value")
	wantCandidates(t, []string{"history", "value", "--mode", ""}, "raw", "processed", "attime")
	wantCandidates(t, []string{"alarms", "list", "--state", ""}, "active", "unacked")
	wantCandidates(t, []string{"diagnostics", "sessions", "--session", ""}, "self")
	wantCandidates(t, []string{"gds", "pull", "query", "--type", ""}, "server", "client", "clientandserver")
	wantCandidates(t, []string{"gds", "pull", "status", "--type", ""}, "rsa", "ecc-p256")
	wantCandidates(t, []string{"pubsub", "publish", "--qos", ""}, "0", "1", "2")
	wantCandidates(t, []string{"pubsub", "subscribe", "--url", ""}, "opc.udp://", "mqtt://", "amqp://")
	wantCandidates(t, []string{"pubsub", "config", "add-writer-group", "--encoding", ""}, "uadp", "json")
	wantCandidates(t, []string{"pubsub", "config", "export", "--ua-encoding", ""}, "json", "xml")
	wantCandidates(t, []string{"pubsub", "config", "remove", ""}, "connection", "writer", "dataset")
	wantCandidates(t, []string{"serve", "--log-level", ""}, "debug", "info")
}

func TestCompleteFilesAndArgs(t *testing.T) {
	ext := fmt.Sprintf(":%d", cobra.ShellCompDirectiveFilterFileExt)
	noFile := fmt.Sprintf(":%d", cobra.ShellCompDirectiveNoFileComp)
	if got, d := complete(t, "get", "value", "--cert", ""); !slices.Contains(got, "pem") || d != ext {
		t.Errorf("--cert: %v %s", got, d)
	}
	if got, d := complete(t, "serve", "--nodeset", ""); !slices.Equal(got, []string{"xml"}) || d != ext {
		t.Errorf("serve --nodeset: %v %s", got, d)
	}
	if got, d := complete(t, "gds", "trustlist", "add", ""); !slices.Contains(got, "crl") || d != ext {
		t.Errorf("gds trustlist add <cert>: %v %s", got, d)
	}
	if _, d := complete(t, "gds", "trustlist", "get", "--out", ""); d != fmt.Sprintf(":%d", cobra.ShellCompDirectiveFilterDirs) {
		t.Errorf("gds trustlist get --out: %s, want directories", d)
	}
	if _, d := complete(t, "get", "value", "--timeout", ""); d != noFile {
		t.Errorf("--timeout: %s, want no file completion", d)
	}
	if _, d := complete(t, "alarms", "ack", ""); d != noFile {
		t.Errorf("alarms ack <conditionId>: %s, want no file completion", d)
	}
}

// valueSetUsage matches a usage string that lists the values of a flag.
var valueSetUsage = regexp.MustCompile(`\w+\|\w+|one of|: \w+, \w+,? (or|and) \w+`)

// TestEveryValueFlagCompletes walks every command: a flag that takes a value
// has a completion function, so a flag whose usage lists a value set cannot
// lack one.
func TestEveryValueFlagCompletes(t *testing.T) {
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		check := func(f *pflag.Flag) {
			if f.Value.Type() == "bool" || f.Name == "help" {
				return
			}
			if _, ok := c.GetFlagCompletionFunc(f.Name); !ok {
				kind := "value"
				if valueSetUsage.MatchString(f.Usage) {
					kind = "value set"
				}
				t.Errorf("%s --%s (%s flag) has no completion: %s", c.CommandPath(), f.Name, kind, f.Usage)
			}
		}
		c.LocalFlags().VisitAll(check)
		c.InheritedFlags().VisitAll(check)
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(rootCmd)
}
