// SPDX-License-Identifier: MIT

package opcuactl

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/go-opcua/uapolicy"
	"github.com/otfabric/opcuactl/internal/format"
	"github.com/otfabric/opcuactl/internal/model"
	"github.com/otfabric/opcuactl/internal/parse"
	"github.com/otfabric/opcuactl/internal/serve"
	"github.com/spf13/cobra"
)

// Defaults of serve.
var (
	defaultServePolicies = []string{"Basic256Sha256"}
	defaultServeModes    = []string{"Sign", "SignAndEncrypt"}
)

const (
	defaultServeListen = "opc.tcp://0.0.0.0:4840"
	// serveUsersEnv holds the --user values (comma-separated) when no
	// --user is given.
	serveUsersEnv = "OPCUACTL_SERVE_USERS"
)

// serveFlags are the flags of serve.
type serveFlags struct {
	listen      string
	hostname    string
	wss         string
	https       string
	tlsCert     string
	tlsKey      string
	uajson      bool
	policies    []string
	modes       []string
	certs       []string
	keys        []string
	certOut     string
	keyOut      string
	appURI      string
	trusted     []string
	trustDir    string
	insecure    bool
	anonymous   bool
	users       []string
	nodesets    []string
	noDemo      bool
	simInterval time.Duration
	logLevel    string
	duration    time.Duration
}

func newServeCmd() *cobra.Command { return serveCommand(&serveFlags{}) }

// serveCommand returns the serve command with its flags in f.
func serveCommand(f *serveFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run an OPC UA test server",
		Long: `Run an OPC UA server for tests and simulations until Ctrl+C (SIGINT) or
SIGTERM, or for --duration.

The server listens on --listen (opc.tcp, default ` + defaultServeListen + `) and,
with --wss and --https, on opc.wss (WebSocket over TLS) and opc.https (UA
Binary over HTTPS) as well; --uajson adds the UA JSON mappings wss-uajson and
https-uajson on those listeners. The endpoint URLs carry --hostname (default:
the host of --listen, or the name of this machine when that is a wildcard
address).

Security: --security-policy names the policies (repeatable or comma-separated,
default Basic256Sha256, and None with --insecure-no-security) and --security-mode the modes of the policies
other than None (default Sign,SignAndEncrypt). The application instance
certificates come from --cert / --key (PEM or DER; a file may hold several
certificates or keys); every policy without one gets a self-signed
certificate made at start (RSA, ECDSA P-256 / P-384 or Ed25519; Brainpool
and Curve448 need --cert). --cert-out and --key-out write the certificates
and keys, so clients can trust them and the next run can reuse them. The TLS
certificate of opc.wss and opc.https is --tls-cert / --tls-key, or else the
RSA application certificate. Client application certificates are accepted
unless --trusted-cert or --trust-dir name the trusted ones.

Users: anonymous sessions are allowed unless --allow-anonymous=false;
--user name:password (repeatable, or ` + serveUsersEnv + `, comma-separated)
adds user names. User names are offered on the secure endpoints and on
opc.https, where TLS protects them.

Address space: the demo namespace ` + serve.DemoNamespaceURI + ` (--no-demo
leaves it out) has an object Demo with
  Demo.Boolean, Demo.Int32, Demo.Double, Demo.String, Demo.DateTime,
  Demo.Int32Array   read-only values
  Demo.Counter      UInt32, counts up every --sim-interval
  Demo.Sine         Double, a sine wave, historized (history value)
  Demo.Level        Double, writable; Demo.LevelAlarm, an exclusive level alarm,
                    goes High at 80 and HighHigh at 95
  Demo.Add          method Add(a, b Int32) -> sum
as string NodeIds (ns=<index>;s=Demo.Counter). Demo keeps an event history
(history event, update-events, delete-events): a start event and the
LevelAlarm transitions, the last 1000 events. --nodeset imports NodeSet2 XML
files (repeatable).

On start the endpoints are printed (--format json: the server description as
JSON, -q: the endpoint URLs only).

Examples:
  opcuactl serve
  opcuactl serve --listen opc.tcp://127.0.0.1:4840 --insecure-no-security --security-policy None
  opcuactl serve --cert-out server.pem --key-out server.key --user operator:secret
  opcuactl serve --cert server.pem --key server.key --trust-dir ./pki/trusted
  opcuactl serve --wss 0.0.0.0:4843 --https 0.0.0.0:4443 --uajson --cert-out server.pem
  opcuactl serve --nodeset Opc.Ua.Di.NodeSet2.xml --nodeset machine.xml --format json`,
		Args: cobra.NoArgs,
		// Only the output flags of the root apply; the client connection
		// flags are not checked.
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			return applyEnvOverrides(cmd)
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			f.insecure = f.insecure || connCfg.InsecureNoSecurity
			return f.run(ctx, os.Stdout, os.Stderr, serveOutput{format: format.ParseFormat(outputFormat), pretty: prettyOutput, quiet: quiet, verbose: verbose})
		},
	}

	fs := cmd.Flags()
	fs.StringVar(&f.listen, "listen", defaultServeListen, "opc.tcp address to listen on (opc.tcp://host:port or host:port; port 0 picks a free port)")
	fs.StringVar(&f.hostname, "hostname", "", "Host name of the endpoint URLs (default: the host of --listen, or this machine's name for a wildcard address)")
	fs.StringVar(&f.wss, "wss", "", "Also listen for opc.wss (WebSocket over TLS) on this address (host:port)")
	fs.StringVar(&f.https, "https", "", "Also listen for opc.https (UA Binary over HTTPS) on this address (host:port)")
	fs.StringVar(&f.tlsCert, "tls-cert", "", "opc.wss/opc.https: TLS certificate (PEM; default: the RSA application certificate)")
	fs.StringVar(&f.tlsKey, "tls-key", "", "opc.wss/opc.https: private key (PEM) of --tls-cert")
	fs.BoolVar(&f.uajson, "uajson", false, "Serve the UA JSON mappings wss-uajson and https-uajson on --wss and --https")
	fs.BoolVar(&f.insecure, "insecure-no-security", false, "Offer endpoints without message security (--security-policy None): anyone on the network can read and change the traffic")
	fs.StringSliceVar(&f.policies, "security-policy", nil, "Security policies to offer (repeatable or comma-separated; default Basic256Sha256)")
	fs.StringSliceVar(&f.modes, "security-mode", nil, "Security modes of the policies other than None (Sign|SignAndEncrypt; default Sign,SignAndEncrypt)")
	fs.StringArrayVar(&f.certs, "cert", nil, "Application instance certificate file (PEM/DER, repeatable; default: self-signed ones)")
	fs.StringArrayVar(&f.keys, "key", nil, "Private key file of --cert (PEM/DER, repeatable)")
	fs.StringVar(&f.certOut, "cert-out", "", "Write the application certificates (PEM) to this file")
	fs.StringVar(&f.keyOut, "key-out", "", "Write the private keys of the application certificates (PEM, mode 0600) to this file")
	fs.StringVar(&f.appURI, "application-uri", "", "ApplicationUri (default: the one of --cert, or urn:<hostname>:opcuactl:serve)")
	fs.StringArrayVar(&f.trusted, "trusted-cert", nil, "Trusted client certificate or CA (PEM/DER, repeatable); other clients are rejected")
	fs.StringVar(&f.trustDir, "trust-dir", "", "Directory of trusted client certificates or CAs (*.der, *.pem, *.crt, *.cer)")
	fs.BoolVar(&f.anonymous, "allow-anonymous", true, "Allow anonymous sessions")
	fs.StringArrayVar(&f.users, "user", nil, "Accept this user name:password (repeatable, or "+serveUsersEnv+")")
	fs.StringArrayVar(&f.nodesets, "nodeset", nil, "Import a NodeSet2 XML file (repeatable)")
	fs.BoolVar(&f.noDemo, "no-demo", false, "Leave out the demo namespace")
	fs.DurationVar(&f.simInterval, "sim-interval", time.Second, "Update interval of the simulated demo values")
	fs.StringVar(&f.logLevel, "log-level", "error", "Server log on stderr (debug|info|warn|error|off)")
	fs.DurationVar(&f.duration, "duration", 0, "Stop after this time (default: run until Ctrl+C / SIGTERM)")

	_ = cmd.RegisterFlagCompletionFunc("security-policy", fixedCompletion(servePolicyValues))
	_ = cmd.RegisterFlagCompletionFunc("security-mode", fixedCompletion(serveModeValues))
	_ = cmd.RegisterFlagCompletionFunc("log-level", fixedCompletion(serveLogLevelValues))
	return cmd
}

// serveOutput is how serve prints the server description.
type serveOutput struct {
	format  format.Format
	pretty  bool
	quiet   bool
	verbose bool
}

// serveSetup is a configuration with what serve prints about it.
type serveSetup struct {
	cfg         serve.Config
	certFiles   map[int]string // identity index -> file it came from or went to
	trustedFrom int            // number of trusted certificates, -1: any
}

// run starts the server, prints it and serves until ctx ends (or
// --duration), then shuts it down.
func (f *serveFlags) run(ctx context.Context, stdout, stderr io.Writer, out serveOutput) error {
	setup, err := f.setup(stderr)
	if err != nil {
		return exitCode(exitUsage, err)
	}
	srv, err := serve.New(setup.cfg)
	if err != nil {
		return exitCode(exitUsage, err)
	}
	if f.duration > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, f.duration)
		defer cancel()
	}
	if err := srv.Start(ctx); err != nil {
		_ = srv.Close()
		return exitCodef(exitTransport, "starting the server: %v", err)
	}
	if err := writeServeInfo(stdout, out, srv, setup); err != nil {
		_ = srv.Close()
		return err
	}
	_, _ = fmt.Fprintln(stderr, "Serving; stop with Ctrl+C")
	<-ctx.Done()
	if err := srv.Close(); err != nil {
		return exitCodef(exitTransport, "stopping the server: %v", err)
	}
	_, _ = fmt.Fprintln(stderr, "Server stopped")
	return nil
}

// setup validates the flags and loads the files into a configuration.
func (f *serveFlags) setup(stderr io.Writer) (*serveSetup, error) {
	listenHost, listen, err := serveAddr(f.listen, "opc.tcp")
	if err != nil {
		return nil, fmt.Errorf("invalid --listen: %w", err)
	}
	cfg := serve.Config{
		Listen:          listen,
		Hostname:        f.hostname,
		UAJSON:          f.uajson,
		Anonymous:       f.anonymous,
		Demo:            !f.noDemo,
		Interval:        f.simInterval,
		ApplicationName: "opcuactl serve",
	}
	if f.simInterval <= 0 {
		return nil, fmt.Errorf("--sim-interval must be positive")
	}
	if f.duration < 0 {
		return nil, fmt.Errorf("--duration must not be negative")
	}
	if f.wss != "" {
		if _, cfg.WebSocket, err = serveAddr(f.wss, "opc.wss"); err != nil {
			return nil, fmt.Errorf("invalid --wss: %w", err)
		}
	}
	if f.https != "" {
		if _, cfg.HTTPS, err = serveAddr(f.https, "opc.https"); err != nil {
			return nil, fmt.Errorf("invalid --https: %w", err)
		}
	}
	tlsListeners := cfg.WebSocket != "" || cfg.HTTPS != ""
	if f.uajson && !tlsListeners {
		return nil, fmt.Errorf("--uajson needs --wss or --https: UA JSON is served on those listeners")
	}
	switch {
	case (f.tlsCert == "") != (f.tlsKey == ""):
		return nil, fmt.Errorf("--tls-cert and --tls-key must be given together")
	case f.tlsCert != "" && !tlsListeners:
		return nil, fmt.Errorf("--tls-cert is the certificate of --wss and --https; give one of them")
	case f.tlsCert != "":
		pair, err := tls.LoadX509KeyPair(f.tlsCert, f.tlsKey)
		if err != nil {
			return nil, fmt.Errorf("loading --tls-cert/--tls-key: %w", err)
		}
		cfg.TLS = &pair
	}
	if cfg.Hostname == "" {
		cfg.Hostname = listenHost
		if isWildcard(listenHost) {
			if cfg.Hostname, err = os.Hostname(); err != nil || cfg.Hostname == "" {
				cfg.Hostname = "localhost"
			}
			cfg.Hostname = strings.ToLower(cfg.Hostname)
		}
	}
	if cfg.Policies, err = servePolicies(f.policies, f.insecure); err != nil {
		return nil, err
	}
	if cfg.Modes, err = serveModes(f.modes); err != nil {
		return nil, err
	}
	level, err := parseLogLevel(f.logLevel)
	if err != nil {
		return nil, err
	}
	if level != nil {
		cfg.Logger = slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: *level}))
	}
	if cfg.Users, err = f.parseUsers(); err != nil {
		return nil, err
	}
	if !cfg.Anonymous && len(cfg.Users) == 0 {
		return nil, fmt.Errorf("--allow-anonymous=false needs a --user: nobody could log in")
	}
	secure := slices.ContainsFunc(cfg.Policies, func(p string) bool { return p != ua.SecurityPolicyURINone })
	if len(cfg.Users) > 0 && !secure && cfg.HTTPS == "" {
		return nil, fmt.Errorf("--user needs a secure --security-policy (or --https): user names are not offered on unsecured endpoints")
	}

	setup := &serveSetup{certFiles: map[int]string{}, trustedFrom: -1}
	if len(f.certs) != len(f.keys) {
		return nil, fmt.Errorf("--cert and --key must be given in pairs (%d --cert, %d --key)", len(f.certs), len(f.keys))
	}
	for i := range f.certs {
		ids, err := loadIdentities(f.certs[i], f.keys[i])
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			setup.certFiles[len(cfg.Identities)] = f.certs[i]
			cfg.Identities = append(cfg.Identities, id)
		}
	}
	cfg.ApplicationURI = f.appURI
	if cfg.ApplicationURI == "" && len(cfg.Identities) == 0 {
		cfg.ApplicationURI = "urn:" + cfg.Hostname + ":opcuactl:serve"
	}
	appURI := cfg.ApplicationURI
	if appURI == "" {
		appURI = certificateURI(cfg.Identities[0].Cert)
	}
	// TLS needs an RSA certificate, and so does --cert-out on a server
	// without any.
	needRSA := (tlsListeners && cfg.TLS == nil) ||
		((f.certOut != "" || f.keyOut != "") && len(cfg.Identities) == 0 && !secure)
	hosts := []string{cfg.Hostname, "localhost", "127.0.0.1", "::1"}
	if !isWildcard(listenHost) {
		hosts = append(hosts, listenHost)
	}
	generated, err := serve.GenerateIdentities(cfg.Identities, cfg.Policies, needRSA, appURI, hosts)
	if err != nil {
		return nil, err
	}
	cfg.Identities = append(cfg.Identities, generated...)
	if setup.trustedFrom, cfg.TrustedClients, err = f.trustList(); err != nil {
		return nil, err
	}
	for _, file := range f.nodesets {
		b, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("--nodeset: %w", err)
		}
		cfg.NodeSets = append(cfg.NodeSets, serve.NodeSet{Name: file, XML: b})
	}

	if f.certOut != "" {
		if err := os.WriteFile(f.certOut, serve.EncodeCertificates(cfg.Identities), 0o644); err != nil { //nolint:gosec // certificates are public
			return nil, fmt.Errorf("--cert-out: %w", err)
		}
		for i := range cfg.Identities {
			setup.certFiles[i] = f.certOut
		}
	}
	if f.keyOut != "" {
		b, err := serve.EncodeKeys(cfg.Identities)
		if err != nil {
			return nil, fmt.Errorf("--key-out: %w", err)
		}
		if err := os.WriteFile(f.keyOut, b, 0o600); err != nil {
			return nil, fmt.Errorf("--key-out: %w", err)
		}
	}
	setup.cfg = cfg
	return setup, nil
}

// serveAddr parses a listen address: scheme://host:port or host:port. A
// missing host is the wildcard address 0.0.0.0. It returns the host and
// host:port.
func serveAddr(s, scheme string) (string, string, error) {
	addr := s
	if strings.Contains(s, "://") {
		u, err := url.Parse(s)
		if err != nil {
			return "", "", err
		}
		if u.Scheme != scheme {
			return "", "", fmt.Errorf("%q is not an %s:// address", s, scheme)
		}
		if u.Path != "" && u.Path != "/" {
			return "", "", fmt.Errorf("%q: a listen address has no path", s)
		}
		addr = u.Host
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", "", fmt.Errorf("%q: %w", s, err)
	}
	if _, err := net.LookupPort("tcp", port); err != nil || port == "" {
		return "", "", fmt.Errorf("%q: invalid port %q", s, port)
	}
	if host == "" {
		host = "0.0.0.0"
	}
	return host, net.JoinHostPort(host, port), nil
}

// isWildcard reports whether host is an address of all interfaces.
func isWildcard(host string) bool {
	ip := net.ParseIP(host)
	return host == "" || (ip != nil && ip.IsUnspecified())
}

// servePolicies returns the URIs of --security-policy.
func servePolicies(names []string, insecure bool) ([]string, error) {
	if len(names) == 0 {
		names = defaultServePolicies
		if insecure {
			names = append([]string{"None"}, names...)
		}
	}
	var out []string
	for _, n := range names {
		n = strings.TrimSpace(n)
		uri := parse.SecurityPolicy(n)
		if n == "" || !uapolicy.IsSupported(uri) {
			return nil, fmt.Errorf("invalid --security-policy %q (%s)", n, strings.Join(servePolicyValues, "|"))
		}
		if uri == ua.SecurityPolicyURINone && !insecure {
			return nil, fmt.Errorf("--security-policy None offers no message security: accept this with --insecure-no-security")
		}
		if !slices.Contains(out, uri) {
			out = append(out, uri)
		}
	}
	return out, nil
}

// serveModes returns the modes of --security-mode.
func serveModes(names []string) ([]ua.MessageSecurityMode, error) {
	if len(names) == 0 {
		names = defaultServeModes
	}
	var out []ua.MessageSecurityMode
	for _, n := range names {
		var m ua.MessageSecurityMode
		switch strings.ToLower(strings.TrimSpace(n)) {
		case "sign":
			m = ua.MessageSecurityModeSign
		case "signandencrypt":
			m = ua.MessageSecurityModeSignAndEncrypt
		case "none":
			return nil, fmt.Errorf("--security-mode None: the mode None is that of --security-policy None")
		default:
			return nil, fmt.Errorf("invalid --security-mode %q (Sign|SignAndEncrypt)", n)
		}
		if !slices.Contains(out, m) {
			out = append(out, m)
		}
	}
	return out, nil
}

// parseLogLevel parses --log-level; nil is off.
func parseLogLevel(s string) (*slog.Level, error) {
	var l slog.Level
	switch strings.ToLower(s) {
	case "off", "none":
		return nil, nil
	case "debug":
		l = slog.LevelDebug
	case "info":
		l = slog.LevelInfo
	case "warn", "warning":
		l = slog.LevelWarn
	case "error":
		l = slog.LevelError
	default:
		return nil, fmt.Errorf("invalid --log-level %q (%s)", s, strings.Join(serveLogLevelValues, "|"))
	}
	return &l, nil
}

// parseUsers parses --user (or the environment) as name:password.
func (f *serveFlags) parseUsers() ([]serve.User, error) {
	specs := f.users
	if len(specs) == 0 {
		specs = splitCSV(os.Getenv(serveUsersEnv))
	}
	var out []serve.User
	for _, spec := range specs {
		name, password, ok := strings.Cut(spec, ":")
		if !ok || name == "" {
			return nil, fmt.Errorf("invalid --user %q: want name:password", spec)
		}
		out = append(out, serve.User{Name: name, Password: password})
	}
	return out, nil
}

// loadIdentities reads the certificates of certFile with the keys of keyFile.
func loadIdentities(certFile, keyFile string) ([]serve.Identity, error) {
	c, err := os.ReadFile(certFile)
	if err != nil {
		return nil, fmt.Errorf("--cert: %w", err)
	}
	k, err := os.ReadFile(keyFile)
	if err != nil {
		return nil, fmt.Errorf("--key: %w", err)
	}
	ids, err := serve.ParseIdentities(c, k)
	if err != nil {
		return nil, fmt.Errorf("--cert %s / --key %s: %w", certFile, keyFile, err)
	}
	return ids, nil
}

// certificateURI returns the ApplicationUri of a certificate.
func certificateURI(der []byte) string {
	c, err := uapolicy.ParseCertificate(der)
	if err != nil || len(c.URIs) == 0 {
		return ""
	}
	return c.URIs[0].String()
}

// trustList loads --trusted-cert and --trust-dir: the number of
// certificates and the certificates, -1 and nil to accept any client.
func (f *serveFlags) trustList() (int, [][]byte, error) {
	if len(f.trusted) == 0 && f.trustDir == "" {
		return -1, nil, nil
	}
	var out [][]byte
	add := func(file, flag string) error {
		b, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("%s: %w", flag, err)
		}
		certs, err := serve.ParseCertificates(b)
		if err != nil {
			return fmt.Errorf("%s %s: %w", flag, file, err)
		}
		out = append(out, certs...)
		return nil
	}
	for _, file := range f.trusted {
		if err := add(file, "--trusted-cert"); err != nil {
			return 0, nil, err
		}
	}
	if f.trustDir != "" {
		entries, err := os.ReadDir(f.trustDir)
		if err != nil {
			return 0, nil, fmt.Errorf("--trust-dir: %w", err)
		}
		for _, e := range entries {
			switch strings.ToLower(filepath.Ext(e.Name())) {
			case ".der", ".pem", ".crt", ".cer":
			default:
				continue
			}
			if e.IsDir() {
				continue
			}
			if err := add(filepath.Join(f.trustDir, e.Name()), "--trust-dir"); err != nil {
				return 0, nil, err
			}
		}
		if len(out) == 0 {
			return 0, nil, fmt.Errorf("--trust-dir %s has no certificates (*.der, *.pem, *.crt, *.cer)", f.trustDir)
		}
	}
	return len(out), out, nil
}

// serveInfo describes the running server.
func serveInfo(srv *serve.Server, setup *serveSetup) model.ServeInfo {
	info := model.ServeInfo{
		ApplicationURI: srv.ApplicationURI(),
		Namespaces:     srv.Namespaces(),
		ClientTrust:    "any",
		Anonymous:      setup.cfg.Anonymous,
	}
	if uri, idx, ok := srv.DemoNamespace(); ok {
		info.DemoNamespace, info.DemoNamespaceIndex = uri, idx
	}
	if setup.trustedFrom >= 0 {
		info.ClientTrust = fmt.Sprintf("%d certificate(s)", setup.trustedFrom)
	}
	for _, u := range setup.cfg.Users {
		info.Users = append(info.Users, u.Name)
	}
	for i, id := range setup.cfg.Identities {
		c := model.ServeCertificate{Thumbprint: id.Thumbprint(), KeyType: id.KeyType(), Generated: id.Generated, File: setup.certFiles[i]}
		if x, err := uapolicy.ParseCertificate(id.Cert); err == nil {
			c.Subject = x.Subject.String()
			c.NotAfter = x.NotAfter.UTC().Format(time.RFC3339)
		}
		info.Certificates = append(info.Certificates, c)
	}
	for _, ep := range srv.Endpoints() {
		info.Endpoints = append(info.Endpoints, endpointModel(ep))
	}
	return info
}

// writeServeInfo prints the server: the endpoint URLs with -q, the
// description for json, yaml and jsonl, the endpoint table for csv, and a
// summary with the endpoint table for table.
func writeServeInfo(w io.Writer, out serveOutput, srv *serve.Server, setup *serveSetup) error {
	info := serveInfo(srv, setup)
	if out.quiet {
		var seen []string
		for _, ep := range info.Endpoints {
			if !slices.Contains(seen, ep.EndpointURL) {
				seen = append(seen, ep.EndpointURL)
				_, _ = fmt.Fprintln(w, ep.EndpointURL)
			}
		}
		return nil
	}
	fw := format.NewWriter(out.format, out.pretty)
	fw.SetOutput(w)
	switch out.format {
	case format.JSON, format.YAML, format.JSONL:
		return fw.Write(info)
	case format.CSV:
		return fw.WriteTable(endpointTable(info.Endpoints, out.verbose))
	}
	_, _ = fmt.Fprintf(w, "Application URI:  %s\n", info.ApplicationURI)
	if info.DemoNamespace != "" {
		_, _ = fmt.Fprintf(w, "Demo namespace:   ns=%d %s (ns=%d;s=Demo.Counter, ...)\n", info.DemoNamespaceIndex, info.DemoNamespace, info.DemoNamespaceIndex)
	}
	for _, c := range info.Certificates {
		var notes []string
		if c.Generated {
			notes = append(notes, "self-signed, generated")
		}
		if c.File != "" {
			notes = append(notes, c.File)
		}
		note := ""
		if len(notes) > 0 {
			note = " (" + strings.Join(notes, ", ") + ")"
		}
		_, _ = fmt.Fprintf(w, "Certificate:      %s %s%s\n", c.KeyType, c.Thumbprint, note)
	}
	if info.ClientTrust == "any" {
		_, _ = fmt.Fprintln(w, "Client trust:     any client certificate")
	} else {
		_, _ = fmt.Fprintf(w, "Client trust:     %s\n", info.ClientTrust)
	}
	users := slices.Clone(info.Users)
	if info.Anonymous {
		users = append([]string{"anonymous"}, users...)
	}
	_, _ = fmt.Fprintf(w, "Users:            %s\n\n", strings.Join(users, ", "))
	return fw.WriteTable(endpointTable(info.Endpoints, out.verbose))
}
