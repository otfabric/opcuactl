// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"io"
	"os"
	"testing"
)

// init() runs when the package loads under go test; this test documents build-time defaults.
func TestBuildMetadataDefaults(t *testing.T) {
	t.Parallel()
	if version == "" {
		t.Error("main.version should have a non-empty default")
	}
}

// TestMainVersion runs main with the arguments of the command line: init
// handed the link-time build metadata to the commands.
func TestMainVersion(t *testing.T) {
	oldArgs, oldStdout := os.Args, os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Args = []string{"opcuactl", "version", "--format", "json"}
	os.Stdout = w
	main()
	os.Args, os.Stdout = oldArgs, oldStdout
	_ = w.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}

	var info map[string]string
	if err := json.Unmarshal(out, &info); err != nil {
		t.Fatalf("version output %q: %v", out, err)
	}
	if info["version"] != version || info["commit"] != commit || info["buildDate"] != buildDate {
		t.Errorf("version %v, want main's %s %s %s", info, version, commit, buildDate)
	}
}
