// SPDX-License-Identifier: MIT

package opcuactl

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/otfabric/opcuactl/internal/conn"
	"github.com/otfabric/opcuactl/internal/format"
	"github.com/spf13/cobra"
)

var (
	connCfg      conn.Config
	outputFormat string
	prettyOutput bool
	verbose      bool
	quiet        bool
	namespace    string
)

func newWriter() *format.Writer {
	return format.NewWriter(format.ParseFormat(outputFormat), prettyOutput)
}

var rootCmd = &cobra.Command{
	Use:   "opcuactl",
	Short: "Operator-focused CLI for OPC UA servers",
	Long: `opcuactl is a production-grade CLI for interacting with OPC UA servers.

It provides discovery, browsing, reading, writing, method invocation,
monitoring, and history retrieval capabilities.

Examples:
  opcuactl discover endpoints --endpoint opc.tcp://localhost:4840
  opcuactl get value --endpoint opc.tcp://localhost:4840 --node i=2258
  opcuactl browse children --endpoint opc.tcp://localhost:4840 --node ns=0;i=85`,
	SilenceUsage:  true,
	SilenceErrors: true,
	PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
		return applyEnvOverrides(cmd)
	},
}

func init() {
	pf := rootCmd.PersistentFlags()

	pf.StringVarP(&connCfg.Endpoint, "endpoint", "e", "", "OPC UA server endpoint (opc.tcp://host:port)")
	pf.StringVar(&connCfg.SecurityPolicy, "security-policy", "None", "Security policy (None|Basic128Rsa15|Basic256|Basic256Sha256|Aes128Sha256RsaOaep|Aes256Sha256RsaPss|auto)")
	pf.StringVar(&connCfg.SecurityMode, "security-mode", "None", "Security mode (None|Sign|SignAndEncrypt|auto)")
	pf.StringVar(&connCfg.Auth, "auth", "anonymous", "Authentication mode (anonymous|username|certificate|issued)")
	pf.StringVar(&connCfg.Username, "username", "", "Username for --auth username")
	pf.StringVar(&connCfg.Password, "password", "", "Password for --auth username")
	pf.StringVar(&connCfg.AuthPolicyID, "auth-policy-id", "", "User token PolicyId (usually set automatically with security auto)")
	pf.StringVar(&connCfg.Token, "token", "", "Issued token (raw or base64) for --auth issued")
	pf.StringVar(&connCfg.TokenFile, "token-file", "", "Issued token file for --auth issued")
	pf.StringVar(&connCfg.CertFile, "cert", "", "Client certificate file (PEM/DER) for secure channel and/or certificate auth")
	pf.StringVar(&connCfg.KeyFile, "key", "", "Client private key file (PEM/DER)")
	pf.StringVar(&connCfg.ServerCertFile, "server-cert", "", "Server certificate file (overrides endpoint cert when set)")
	pf.StringArrayVar(&connCfg.TrustedCertFiles, "trusted-cert", nil, "Trusted CA or self-signed server cert (repeatable; PEM/DER)")
	pf.BoolVar(&connCfg.InsecureSkipVerify, "insecure-skip-verify", false, "Skip server certificate verification (INSECURE)")
	pf.DurationVar(&connCfg.Timeout, "timeout", 10*time.Second, "Connection/operation timeout")
	pf.StringVarP(&outputFormat, "format", "f", "table", "Output format (table|json|yaml)")
	pf.BoolVar(&prettyOutput, "pretty", false, "Pretty-print structured output")
	pf.BoolVarP(&verbose, "verbose", "v", false, "Verbose output")
	pf.BoolVarP(&quiet, "quiet", "q", false, "Quiet mode: emit only the primary value (for scripting)")
	pf.StringVar(&namespace, "namespace", "", "Default namespace (index or URI) for node ID shorthand")

	_ = rootCmd.RegisterFlagCompletionFunc("format", fixedCompletion(formatValues))
	_ = rootCmd.RegisterFlagCompletionFunc("security-policy", fixedCompletion(securityPolicyValues))
	_ = rootCmd.RegisterFlagCompletionFunc("security-mode", fixedCompletion(securityModeValues))
	_ = rootCmd.RegisterFlagCompletionFunc("auth", fixedCompletion(authValues))

	rootCmd.AddCommand(
		newDiscoverCmd(),
		newBrowseCmd(),
		newFindCmd(),
		newGetCmd(),
		newWriteCmd(),
		newCallCmd(),
		newMonitorCmd(),
		newHistoryCmd(),
		newExportCmd(),
		newPingCmd(),
		newVersionCmd(),
		newCompletionCmd(),
	)
}

// applyEnvOverrides fills unset flags from the environment. Explicit CLI flags win.
func applyEnvOverrides(cmd *cobra.Command) error {
	flags := cmd.Flags()
	setString := func(name, env string, target *string) {
		if flags.Changed(name) {
			return
		}
		if v := os.Getenv(env); v != "" {
			*target = v
		}
	}

	setString("endpoint", "OPCUACTL_ENDPOINT", &connCfg.Endpoint)
	setString("security-policy", "OPCUACTL_SECURITY_POLICY", &connCfg.SecurityPolicy)
	setString("security-mode", "OPCUACTL_SECURITY_MODE", &connCfg.SecurityMode)
	setString("auth", "OPCUACTL_AUTH", &connCfg.Auth)
	setString("username", "OPCUACTL_USERNAME", &connCfg.Username)
	setString("password", "OPCUACTL_PASSWORD", &connCfg.Password)
	setString("auth-policy-id", "OPCUACTL_AUTH_POLICY_ID", &connCfg.AuthPolicyID)
	setString("token", "OPCUACTL_TOKEN", &connCfg.Token)
	setString("token-file", "OPCUACTL_TOKEN_FILE", &connCfg.TokenFile)
	setString("cert", "OPCUACTL_CERT", &connCfg.CertFile)
	setString("key", "OPCUACTL_KEY", &connCfg.KeyFile)
	setString("server-cert", "OPCUACTL_SERVER_CERT", &connCfg.ServerCertFile)
	setString("format", "OPCUACTL_FORMAT", &outputFormat)
	setString("namespace", "OPCUACTL_NAMESPACE", &namespace)

	if !flags.Changed("trusted-cert") {
		if v := os.Getenv("OPCUACTL_TRUSTED_CERT"); v != "" {
			connCfg.TrustedCertFiles = splitCSV(v)
		}
	}

	if !flags.Changed("timeout") {
		if v := os.Getenv("OPCUACTL_TIMEOUT"); v != "" {
			d, err := time.ParseDuration(v)
			if err != nil {
				return fmt.Errorf("invalid OPCUACTL_TIMEOUT %q: %w", v, err)
			}
			connCfg.Timeout = d
		}
	}
	return nil
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}
