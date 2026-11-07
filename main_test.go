// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"io"
	"os"
	"testing"
)

// TestBuildMetadataDefaults documents the build-time defaults: empty, so
// that internal/buildinfo falls back on the Go build information.
func TestBuildMetadataDefaults(t *testing.T) {
	t.Parallel()
	if version != "" || tag != "" || commit != "" || buildDate != "" {
		t.Error("main's build metadata should be empty unless set with -ldflags -X")
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

	var info map[string]any
	if err := json.Unmarshal(out, &info); err != nil {
		t.Fatalf("version output %q: %v", out, err)
	}
	if info["name"] != "opcuactl" || info["version"] != "dev" || info["commit"] == "" || info["built"] != "unknown" {
		t.Errorf("version %v, want a development build of opcuactl", info)
	}
}
