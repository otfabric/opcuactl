package opcuactl

import (
	"fmt"
	"runtime"

	"github.com/otfabric/opcuactl/internal/format"
	"github.com/spf13/cobra"
)

var (
	version   = "dev"
	commit    = "none"
	buildDate = "unknown"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Long: `Print the opcuactl version, commit hash, build date, and Go version.

Examples:
  opcuactl version
  opcuactl version --format json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			w := newWriter()

			info := struct {
				Version   string `json:"version"`
				Commit    string `json:"commit"`
				BuildDate string `json:"buildDate"`
				GoVersion string `json:"goVersion"`
			}{
				Version:   version,
				Commit:    commit,
				BuildDate: buildDate,
				GoVersion: runtime.Version(),
			}

			f := format.ParseFormat(outputFormat)
			if f == format.Table {
				fmt.Printf("opcuactl %s\n", info.Version)
				fmt.Printf("  commit:     %s\n", info.Commit)
				fmt.Printf("  built:      %s\n", info.BuildDate)
				fmt.Printf("  go version: %s\n", info.GoVersion)
				return nil
			}
			return w.Write(info)
		},
	}
}
