// SPDX-License-Identifier: MIT

package main

import (
	"os"

	"github.com/otfabric/opcuactl/cmd/opcuactl"
)

// Injected at link time, e.g. -X main.version=… -X main.tag=…; empty means a
// development build (see internal/buildinfo).
var (
	version   = ""
	tag       = ""
	commit    = ""
	buildDate = ""
)

func init() {
	opcuactl.SetBuildInfo(version, tag, commit, buildDate)
}

func main() {
	if err := opcuactl.Execute(); err != nil {
		os.Exit(opcuactl.ExitCode(err))
	}
}
