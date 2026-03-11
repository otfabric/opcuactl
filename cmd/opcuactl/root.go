package opcuactl

import (
	"os"
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
}

func init() {
	pf := rootCmd.PersistentFlags()

	pf.StringVarP(&connCfg.Endpoint, "endpoint", "e", "", "OPC UA server endpoint (opc.tcp://host:port)")
	pf.StringVar(&connCfg.SecurityPolicy, "security-policy", "None", "Security policy (None|Basic128Rsa15|Basic256|Basic256Sha256|Aes128Sha256RsaOaep|Aes256Sha256RsaPss)")
	pf.StringVar(&connCfg.SecurityMode, "security-mode", "None", "Security mode (None|Sign|SignAndEncrypt)")
	pf.StringVar(&connCfg.Auth, "auth", "anonymous", "Authentication mode (anonymous|username|certificate)")
	pf.StringVar(&connCfg.Username, "username", "", "Username for authentication")
	pf.StringVar(&connCfg.Password, "password", "", "Password for authentication")
	pf.StringVar(&connCfg.CertFile, "cert", "", "Client certificate file (PEM/DER)")
	pf.StringVar(&connCfg.KeyFile, "key", "", "Client private key file (PEM/DER)")
	pf.StringVar(&connCfg.ServerCertFile, "server-cert", "", "Server certificate file for validation")
	pf.DurationVar(&connCfg.Timeout, "timeout", 10*time.Second, "Connection/operation timeout")
	pf.StringVarP(&outputFormat, "format", "f", "table", "Output format (table|json|yaml)")
	pf.BoolVar(&prettyOutput, "pretty", false, "Pretty-print structured output")
	pf.BoolVarP(&verbose, "verbose", "v", false, "Verbose output")

	bindEnv("OPCUACTL_ENDPOINT", &connCfg.Endpoint)
	bindEnv("OPCUACTL_SECURITY_POLICY", &connCfg.SecurityPolicy)
	bindEnv("OPCUACTL_SECURITY_MODE", &connCfg.SecurityMode)
	bindEnv("OPCUACTL_AUTH", &connCfg.Auth)
	bindEnv("OPCUACTL_USERNAME", &connCfg.Username)
	bindEnv("OPCUACTL_PASSWORD", &connCfg.Password)
	bindEnv("OPCUACTL_CERT", &connCfg.CertFile)
	bindEnv("OPCUACTL_KEY", &connCfg.KeyFile)
	bindEnv("OPCUACTL_FORMAT", &outputFormat)

	rootCmd.AddCommand(
		newDiscoverCmd(),
		newBrowseCmd(),
		newGetCmd(),
		newVersionCmd(),
	)
}

func bindEnv(envVar string, target *string) {
	if target == nil {
		return
	}
	if v := os.Getenv(envVar); v != "" && *target == "" {
		*target = v
	}
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}
