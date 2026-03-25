package main

import "testing"

// init() runs when the package loads under go test; this test documents build-time defaults.
func TestBuildMetadataDefaults(t *testing.T) {
	t.Parallel()
	if version == "" {
		t.Error("main.version should have a non-empty default")
	}
}
