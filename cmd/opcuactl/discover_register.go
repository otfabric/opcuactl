// SPDX-License-Identifier: MIT

package opcuactl

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/otfabric/go-opcua"
	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/opcuactl/internal/format"
	"github.com/otfabric/opcuactl/internal/model"
	"github.com/otfabric/opcuactl/internal/parse"
	"github.com/otfabric/opcuactl/internal/serve"
	"github.com/spf13/cobra"
)

// registerServerTypes are the --server-type values.
var registerServerTypes = []string{"server", "clientandserver", "discoveryserver"}

// registerFlags are the flags of discover register.
type registerFlags struct {
	serverURI     string
	productURI    string
	serverNames   []string
	serverType    string
	gatewayURI    string
	discoveryURLs []string
	semaphoreFile string
	offline       bool
	mdnsName      string
	capabilities  []string
	legacy        bool
}

func newDiscoverRegisterCmd() *cobra.Command {
	f := &registerFlags{}
	cmd := &cobra.Command{
		Use:   "register",
		Short: "Register a server with a discovery server",
		Long: `Register a server with a discovery server (an LDS, Part 12) at --endpoint,
or unregister it with --offline.

RegisterServer2 is sent, with the mDNS server name and the capabilities of
--mdns-name and --capability; a discovery server that does not offer it gets
RegisterServer (--legacy sends RegisterServer right away). A registration
lives until it is renewed or ends at the discovery server's registration
timeout: servers renew theirs regularly, opcuactl serve --register-with does.

The request goes over a SecureChannel without a session. Discovery servers
accept a registration only on a secured channel (--security-policy,
--security-mode) opened with the application instance certificate (--cert,
--key) of the registered server, whose ApplicationUri is --server-uri; it is
the default of --server-uri.

Examples:
  opcuactl discover register -e opc.tcp://lds:4840 --cert server.pem --key server.key \
    --discovery-url opc.tcp://myhost:4841 --server-name "My server"
  opcuactl discover register -e opc.tcp://lds:4840 --cert server.pem --key server.key \
    --discovery-url opc.tcp://myhost:4841 --mdns-name myserver --capability DA --capability HD
  opcuactl discover register -e opc.tcp://lds:4840 --cert server.pem --key server.key \
    --discovery-url opc.tcp://myhost:4841 --offline`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rs, cfg, err := f.registeredServer()
			if err != nil {
				return exitCode(exitUsage, err)
			}
			ctx, cancel := connCfg.TimeoutContext(rootContext())
			defer cancel()
			return registerServer(ctx, rs, cfg, f.legacy)
		},
	}
	fs := cmd.Flags()
	fs.StringVar(&f.serverURI, "server-uri", "", "ServerUri to register (default: the ApplicationUri of --cert)")
	fs.StringVar(&f.productURI, "product-uri", "", "ProductUri of the server")
	fs.StringArrayVar(&f.serverNames, "server-name", nil, "Name of the server, [locale:]text (repeatable; default: --server-uri)")
	fs.StringVar(&f.serverType, "server-type", "server", "ApplicationType of the server ("+strings.Join(registerServerTypes, "|")+")")
	fs.StringVar(&f.gatewayURI, "gateway-server-uri", "", "GatewayServerUri, for a server behind a gateway")
	fs.StringArrayVar(&f.discoveryURLs, "discovery-url", nil, "Discovery URL of the server (repeatable, required)")
	fs.StringVar(&f.semaphoreFile, "semaphore-file", "", "File on the discovery server's host whose removal ends the registration")
	fs.BoolVar(&f.offline, "offline", false, "Unregister the server (IsOnline false)")
	fs.StringVar(&f.mdnsName, "mdns-name", "", "mDNS server name, for a discovery server with the multicast extension (default: the first --server-name)")
	fs.StringArrayVar(&f.capabilities, "capability", nil, "Server capability identifier for mDNS (repeatable, e.g. DA, HD, AC)")
	fs.BoolVar(&f.legacy, "legacy", false, "Send RegisterServer, without discovery configuration, instead of RegisterServer2")
	_ = cmd.RegisterFlagCompletionFunc("server-type", fixedCompletion(registerServerTypes))
	return cmd
}

// registeredServer builds the RegisteredServer and the mDNS configuration
// of the flags.
func (f *registerFlags) registeredServer() (*ua.RegisteredServer, *ua.MdnsDiscoveryConfiguration, error) {
	if err := connCfg.RequireEndpoint(); err != nil {
		return nil, nil, err
	}
	if len(f.discoveryURLs) == 0 {
		return nil, nil, errors.New("--discovery-url is required: the URL clients find the server at")
	}
	if f.legacy && (f.mdnsName != "" || len(f.capabilities) > 0) {
		return nil, nil, errors.New("--mdns-name and --capability are sent with RegisterServer2: leave out --legacy")
	}
	rs := &ua.RegisteredServer{
		ServerURI:         f.serverURI,
		ProductURI:        f.productURI,
		GatewayServerURI:  f.gatewayURI,
		DiscoveryURLs:     f.discoveryURLs,
		SemaphoreFilePath: f.semaphoreFile,
		IsOnline:          !f.offline,
	}
	t, err := parse.ApplicationType(f.serverType)
	if err != nil || t == ua.ApplicationTypeClient {
		return nil, nil, fmt.Errorf("invalid --server-type %q (%s)", f.serverType, strings.Join(registerServerTypes, "|"))
	}
	rs.ServerType = t
	if rs.ServerURI == "" && connCfg.CertFile != "" {
		b, err := os.ReadFile(connCfg.CertFile)
		if err != nil {
			return nil, nil, fmt.Errorf("--cert: %w", err)
		}
		if certs, err := serve.ParseCertificates(b); err == nil && len(certs) > 0 {
			rs.ServerURI = certificateURI(certs[0])
		}
	}
	if rs.ServerURI == "" {
		return nil, nil, errors.New("--server-uri is required without a --cert that carries an ApplicationUri")
	}
	for _, n := range f.serverNames {
		locale, text := "", n
		if l, t, ok := strings.Cut(n, ":"); ok && isLocale(l) {
			locale, text = l, t
		}
		rs.ServerNames = append(rs.ServerNames, ua.NewLocalizedTextWithLocale(text, locale))
	}
	if len(rs.ServerNames) == 0 {
		rs.ServerNames = []*ua.LocalizedText{ua.NewLocalizedText(rs.ServerURI)}
	}
	if f.legacy {
		return rs, nil, nil
	}
	mdns := &ua.MdnsDiscoveryConfiguration{MdnsServerName: f.mdnsName, ServerCapabilities: f.capabilities}
	if mdns.MdnsServerName == "" {
		mdns.MdnsServerName = rs.ServerNames[0].Text
	}
	return rs, mdns, nil
}

// registerServer sends RegisterServer2 (or, with legacy or when the
// discovery server does not offer it, RegisterServer) and prints the result.
func registerServer(ctx context.Context, rs *ua.RegisteredServer, mdns *ua.MdnsDiscoveryConfiguration, legacy bool) error {
	c, err := connCfg.NewChannel(ctx, opcua.AutoReconnect(false))
	if err != nil {
		format.Errf("%v", err)
		return exitCode(exitTransport, err)
	}
	defer func() { _ = c.Close(context.WithoutCancel(ctx)) }()

	res := model.RegisterResult{
		DiscoveryServerURL: connCfg.Endpoint,
		ServerURI:          rs.ServerURI,
		Online:             rs.IsOnline,
		Service:            "RegisterServer2",
	}
	res.SecurityPolicy = strings.TrimPrefix(c.SecurityPolicy(), "http://opcfoundation.org/UA/SecurityPolicy#")
	res.SecurityMode = strings.TrimPrefix(c.SecurityMode().String(), "MessageSecurityMode")
	var status ua.StatusCode
	if !legacy {
		resp, err := c.RegisterServer2(ctx, rs, mdns)
		switch {
		case err == nil:
			status = resp.ResponseHeader.ServiceResult
			for _, r := range resp.ConfigurationResults {
				res.ConfigurationResults = append(res.ConfigurationResults, r.Symbol())
			}
		case errors.Is(err, ua.StatusBadServiceUnsupported), errors.Is(err, ua.StatusBadNotImplemented), errors.Is(err, ua.StatusBadNotSupported):
			legacy = true
		default:
			return registerFailed(err)
		}
	}
	if legacy {
		res.Service = "RegisterServer"
		resp, err := c.RegisterServer(ctx, rs)
		if err != nil {
			return registerFailed(err)
		}
		status = resp.ResponseHeader.ServiceResult
	}
	if status != ua.StatusOK {
		return registerFailed(status)
	}
	res.Status = ua.StatusOK.Symbol()

	w := newWriter()
	if format.ParseFormat(outputFormat) != format.Table {
		return w.Write(res)
	}
	what := "Registered"
	if !rs.IsOnline {
		what = "Unregistered"
	}
	_, _ = fmt.Fprintf(os.Stdout, "%s %s with %s (%s, %s %s)\n", what, res.ServerURI, res.DiscoveryServerURL, res.Service, res.SecurityPolicy, res.SecurityMode)
	for i, r := range res.ConfigurationResults {
		if r != ua.StatusOK.Symbol() {
			_, _ = fmt.Fprintf(os.Stdout, "Discovery configuration %d: %s (ignored by the discovery server)\n", i, r)
		}
	}
	return nil
}

// registerFailed reports a refused registration: BadSecurityChecksFailed
// and BadUserAccessDenied usually mean an unsecured channel or a ServerUri
// that is not the one of the certificate.
func registerFailed(err error) error {
	format.Errf("registering: %v", err)
	var sc ua.StatusCode
	if errors.As(err, &sc) {
		switch sc {
		case ua.StatusBadSecurityChecksFailed, ua.StatusBadUserAccessDenied, ua.StatusBadServerURIInvalid,
			ua.StatusBadSecurityModeInsufficient:
			format.Errf("hint: discovery servers accept a registration on a secured channel (--security-policy, --security-mode) opened with the certificate (--cert, --key) whose ApplicationUri is --server-uri")
		}
		return exitCode(exitProtocol, err)
	}
	return exitCode(exitTransport, err)
}
