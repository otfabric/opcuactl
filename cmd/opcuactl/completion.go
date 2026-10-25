// SPDX-License-Identifier: MIT

package opcuactl

import (
	"os"

	"github.com/spf13/cobra"
)

// Shared completion values for reuse across commands.
var (
	formatValues            = []string{"table", "json", "yaml", "csv", "jsonl"}
	securityPolicyValues    = []string{"None", "Basic256Sha256", "Aes128Sha256RsaOaep", "Aes256Sha256RsaPss", "ECC_nistP256", "ECC_nistP384", "ECC_nistP256_AesGcm", "ECC_nistP256_ChaChaPoly", "ECC_curve25519", "ECC_brainpoolP256r1", "ECC_brainpoolP384r1", "ECC_curve448", "Basic128Rsa15", "Basic256", "auto"}
	securityModeValues      = []string{"None", "Sign", "SignAndEncrypt", "auto"}
	authValues              = []string{"anonymous", "username", "certificate", "issued"}
	nodeClassValues         = []string{"all", "object", "variable", "method", "objecttype", "variabletype", "referencetype", "datatype", "view"}
	directionValues         = []string{"forward", "inverse", "both"}
	writeTypeValues         = []string{"bool", "int16", "uint16", "int32", "uint32", "int64", "uint64", "float", "double", "string", "bytes", "datetime", "guid", "nodeid", "json", "ua-json", "ua-xml"}
	historyModeValues       = []string{"raw", "modified", "processed", "attime"}
	continuationValues      = []string{"all", "single"}
	timestampValues         = []string{"source", "server", "both", "neither"}
	aggregateValues         = []string{"average", "min", "max", "count"}
	triggerValues           = []string{"status", "status-value", "status-value-timestamp"}
	deadbandTypeValues      = []string{"none", "absolute", "percent"}
	uaEncodingValues        = []string{"json", "json-verbose", "json-reversible", "json-nonreversible", "xml"}
	returnDiagnosticsValues = []string{"none", "service", "operation", "all"}
	transportValues         = []string{"tcp", "wss", "https", "wss-uajson", "https-uajson"}
	jsonContentTypeValues   = []string{"standard", "legacy"}
	alarmStateValues        = []string{"active", "inactive", "unacked", "acked", "unconfirmed", "confirmed", "shelved", "suppressed", "out-of-service", "silenced", "latched", "dialog", "branch"}
	trustListMaskValues     = []string{"trusted", "trusted-crls", "issuers", "issuer-crls", "all"}
	attrValues              = []string{"NodeID", "NodeClass", "BrowseName", "DisplayName", "Description", "DataType", "Value", "AccessLevel", "UserAccessLevel", "WriteMask", "ValueRank", "Historizing", "MinimumSamplingInterval", "ArrayDimensions"}
)

// fixedCompletion returns a completion function that suggests the given values.
func fixedCompletion(values []string) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return values, cobra.ShellCompDirectiveNoFileComp
	}
}

func newCompletionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completion [bash|zsh|fish|powershell]",
		Short: "Generate shell completion scripts",
		Long: `Generate shell completion scripts for opcuactl.

To load completions:

Bash:
  $ source <(opcuactl completion bash)
  # To install permanently:
  $ opcuactl completion bash > /etc/bash_completion.d/opcuactl

Zsh:
  $ source <(opcuactl completion zsh)
  # To install permanently:
  $ opcuactl completion zsh > "${fpath[1]}/_opcuactl"

Fish:
  $ opcuactl completion fish | source
  # To install permanently:
  $ opcuactl completion fish > ~/.config/fish/completions/opcuactl.fish

PowerShell:
  PS> opcuactl completion powershell | Out-String | Invoke-Expression
  # To install permanently, add the output to your PowerShell profile.`,
		DisableFlagsInUseLine: true,
		ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
		Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return cmd.Root().GenBashCompletion(os.Stdout)
			case "zsh":
				return cmd.Root().GenZshCompletion(os.Stdout)
			case "fish":
				return cmd.Root().GenFishCompletion(os.Stdout, true)
			case "powershell":
				return cmd.Root().GenPowerShellCompletionWithDesc(os.Stdout)
			}
			return nil
		},
	}
	return cmd
}
