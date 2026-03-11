package main

import (
	"os"

	"github.com/otfabric/opcuactl/cmd/opcuactl"
)

func main() {
	if err := opcuactl.Execute(); err != nil {
		os.Exit(opcuactl.ExitCode(err))
	}
}
