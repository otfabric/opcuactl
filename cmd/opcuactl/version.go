// SPDX-License-Identifier: MIT

package opcuactl

import (
	"fmt"

	"github.com/otfabric/opcuactl/internal/buildinfo"
	"github.com/otfabric/opcuactl/internal/format"
	"github.com/spf13/cobra"
)

// SetBuildInfo is called from main with values from main's link-time -X flags.
func SetBuildInfo(ver, gitTag, sha, built string) {
	buildinfo.Set(ver, gitTag, sha, built)
}

func newVersionCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Long: `Print the opcuactl version, tag, commit, build date, Go version and
platform on one line, or as one JSON object with --json (or --format json or
jsonl).

Examples:
  opcuactl version
  opcuactl version --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			info := buildinfo.Get("opcuactl")
			line := info.String()
			if f := format.ParseFormat(outputFormat); asJSON || f == format.JSON || f == format.JSONL {
				line = info.JSON()
			}
			_, err := fmt.Fprintln(cmd.OutOrStdout(), line)
			return err
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print the build information as one JSON object")
	return cmd
}
