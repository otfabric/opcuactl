// SPDX-License-Identifier: MIT

package opcuactl

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/otfabric/go-opcua"
	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/opcuactl/internal/format"
	"github.com/otfabric/opcuactl/internal/model"
	"github.com/otfabric/opcuactl/internal/parse"
	"github.com/spf13/cobra"
)

func newDiscoverCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "discover",
		Short: "Discover OPC UA servers and endpoints",
		Long: `Discover OPC UA servers and their endpoints on the network.

Examples:
  opcuactl discover servers --endpoint opc.tcp://localhost:4840
  opcuactl discover lds --host plc-gateway.local
  opcuactl discover endpoints --endpoint opc.tcp://localhost:4840`,
	}
	cmd.AddCommand(newDiscoverServersCmd(), newDiscoverLDSCmd(), newDiscoverEndpointsCmd())
	return cmd
}

func newDiscoverServersCmd() *cobra.Command {
	var (
		onNetwork    bool
		serverURI    string
		locale       string
		capabilities []string
		startRecord  uint32
		maxRecords   uint32
	)

	cmd := &cobra.Command{
		Use:   "servers",
		Short: "Discover OPC UA servers",
		Long: `Discover OPC UA application descriptions from a discovery endpoint.

By default FindServers is called: the server itself and, on a Local Discovery
Server (LDS), the servers registered with it.

--on-network calls FindServersOnNetwork instead: the servers a discovery
server with the multicast extension (LDS-ME) found on the subnet with mDNS.
--capability keeps only servers that announce all given capabilities (for
example DA, HD, AC, LDS), --start-record and --max-records page through a
large table.

Examples:
  opcuactl discover servers --endpoint opc.tcp://localhost:4840
  opcuactl discover servers --endpoint opc.tcp://localhost:4840 --on-network
  opcuactl discover servers --endpoint opc.tcp://localhost:4840 --on-network --capability DA --capability HD
  opcuactl discover servers --endpoint opc.tcp://localhost:4840 --format json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := connCfg.RequireEndpoint(); err != nil {
				return exitCode(1, err)
			}

			ctx, cancel := connCfg.TimeoutContext(context.Background())
			defer cancel()

			w := newWriter()

			if onNetwork {
				return discoverServersOnNetwork(ctx, w, startRecord, maxRecords, capabilities)
			}
			return discoverServers(ctx, w, serverURI, locale)
		},
	}

	cmd.Flags().BoolVar(&onNetwork, "on-network", false, "Use FindServersOnNetwork instead of FindServers")
	cmd.Flags().StringVar(&serverURI, "server-uri", "", "Filter by server URI")
	cmd.Flags().StringVar(&locale, "locale", "", "Preferred locale")
	cmd.Flags().StringArrayVar(&capabilities, "capability", nil, "With --on-network: only servers with this capability (repeatable, e.g. DA, HD, LDS)")
	cmd.Flags().Uint32Var(&startRecord, "start-record", 0, "With --on-network: first record id to return")
	cmd.Flags().Uint32Var(&maxRecords, "max-records", 0, "With --on-network: maximum number of records (0 = no limit)")
	return cmd
}

func discoverServers(ctx context.Context, w *format.Writer, serverURI, locale string) error {
	opts := connCfg.DiscoveryOptions()
	if locale != "" {
		opts = append(opts, opcua.Locales(locale))
	}

	apps, err := opcua.FindServers(ctx, connCfg.Endpoint, opts...)
	if err != nil {
		format.Errf("discovering servers: %v", err)
		return exitCode(2, err)
	}

	var servers []model.DiscoveredServer
	for _, app := range apps {
		if serverURI != "" && app.ApplicationURI != serverURI {
			continue
		}
		servers = append(servers, discoveredServer(app))
	}

	if format.ParseFormat(outputFormat) == format.Table {
		return writeServersTable(w, servers)
	}
	return w.Write(servers)
}

func discoveredServer(app *ua.ApplicationDescription) model.DiscoveredServer {
	s := model.DiscoveredServer{
		ApplicationURI:      app.ApplicationURI,
		ProductURI:          app.ProductURI,
		ApplicationType:     app.ApplicationType.String(),
		GatewayServerURI:    app.GatewayServerURI,
		DiscoveryProfileURI: app.DiscoveryProfileURI,
		DiscoveryURLs:       app.DiscoveryURLs,
	}
	if app.ApplicationName != nil {
		s.ApplicationName = app.ApplicationName.Text
	}
	return s
}

func serverOnNetwork(s *ua.ServerOnNetwork) model.ServerOnNetwork {
	return model.ServerOnNetwork{
		RecordID:           s.RecordID,
		ServerName:         s.ServerName,
		DiscoveryURL:       s.DiscoveryURL,
		ServerCapabilities: s.ServerCapabilities,
	}
}

func writeServersTable(w *format.Writer, servers []model.DiscoveredServer) error {
	headers := []string{"APPLICATION NAME", "APPLICATION URI", "TYPE", "DISCOVERY URLS"}
	rows := make([][]string, len(servers))
	for i, s := range servers {
		rows[i] = []string{s.ApplicationName, s.ApplicationURI, s.ApplicationType, strings.Join(s.DiscoveryURLs, ", ")}
	}
	return w.WriteTable(headers, rows)
}

func writeServersOnNetworkTable(w *format.Writer, servers []model.ServerOnNetwork) error {
	headers := []string{"RECORD", "SERVER NAME", "DISCOVERY URL", "CAPABILITIES"}
	rows := make([][]string, len(servers))
	for i, s := range servers {
		rows[i] = []string{fmt.Sprintf("%d", s.RecordID), s.ServerName, s.DiscoveryURL, strings.Join(s.ServerCapabilities, ", ")}
	}
	return w.WriteTable(headers, rows)
}

func discoverServersOnNetwork(ctx context.Context, w *format.Writer, startRecord, maxRecords uint32, capabilities []string) error {
	// FindServersOnNetwork needs a connection but no session.
	c, err := opcua.NewClient(connCfg.Endpoint, append(connCfg.DiscoveryOptions(), opcua.AutoReconnect(false))...)
	if err != nil {
		return exitCode(1, err)
	}
	if err := c.Dial(ctx); err != nil {
		format.Errf("discovering servers on network: %v", err)
		return exitCode(2, err)
	}
	defer func() { _ = c.Close(ctx) }()

	resp, err := c.FindServersOnNetworkWithFilter(ctx, startRecord, maxRecords, capabilities)
	if err != nil {
		format.Errf("discovering servers on network: %v", err)
		return exitCode(2, err)
	}

	results := make([]model.ServerOnNetwork, 0, len(resp.Servers))
	for _, s := range resp.Servers {
		results = append(results, serverOnNetwork(s))
	}

	if format.ParseFormat(outputFormat) == format.Table {
		return writeServersOnNetworkTable(w, results)
	}
	return w.Write(results)
}

func newDiscoverLDSCmd() *cobra.Command {
	var host string

	cmd := &cobra.Command{
		Use:   "lds",
		Short: "Query a Local Discovery Server",
		Long: `Ask the Local Discovery Server (LDS) of a host which servers it knows.

--host is a host name or IP address; the well-known discovery port 4840 is
added. A complete opc.tcp URL, or --endpoint, works as well.

Two lists are returned: the applications registered with the discovery server
(FindServers) and the servers it found on the network with mDNS
(FindServersOnNetwork). The second needs the multicast extension (LDS-ME);
a discovery server without it is not an error.

Examples:
  opcuactl discover lds --host 192.168.1.10
  opcuactl discover lds --host opc.tcp://gateway.local:4840 --format json --pretty
  opcuactl discover lds --endpoint opc.tcp://localhost:4840`,
		RunE: func(cmd *cobra.Command, args []string) error {
			target := host
			if target == "" {
				target = connCfg.Endpoint
			}
			if target == "" {
				return exitCodef(1, "--host or --endpoint is required")
			}

			ctx, cancel := connCfg.TimeoutContext(context.Background())
			defer cancel()

			res, err := opcua.DiscoverServers(ctx, target, connCfg.DiscoveryOptions()...)
			if err != nil {
				format.Errf("querying discovery server: %v", err)
				return exitCode(2, err)
			}

			result := model.DiscoveryResult{
				DiscoveryServerURL: res.DiscoveryServerURL,
				MulticastSupported: res.MulticastSupported,
				Servers:            make([]model.DiscoveredServer, 0, len(res.Servers)),
				ServersOnNetwork:   make([]model.ServerOnNetwork, 0, len(res.ServersOnNetwork)),
			}
			for _, app := range res.Servers {
				result.Servers = append(result.Servers, discoveredServer(app))
			}
			for _, s := range res.ServersOnNetwork {
				result.ServersOnNetwork = append(result.ServersOnNetwork, serverOnNetwork(s))
			}

			w := newWriter()
			if f := format.ParseFormat(outputFormat); f != format.Table && f != format.CSV {
				return w.Write(result)
			}
			if format.ParseFormat(outputFormat) == format.Table {
				fmt.Printf("Discovery server: %s\n\nRegistered servers (FindServers):\n", result.DiscoveryServerURL)
			}
			if err := writeServersTable(w, result.Servers); err != nil {
				return err
			}
			if !result.MulticastSupported {
				if format.ParseFormat(outputFormat) == format.Table {
					fmt.Println("\nServers on network: not supported by this discovery server (no multicast extension)")
				}
				return nil
			}
			if format.ParseFormat(outputFormat) == format.Table {
				fmt.Println("\nServers on network (FindServersOnNetwork, mDNS):")
			}
			return writeServersOnNetworkTable(w, result.ServersOnNetwork)
		},
	}

	cmd.Flags().StringVar(&host, "host", "", "Host name, IP address or opc.tcp URL of the discovery server (port 4840 by default)")
	return cmd
}

func newDiscoverEndpointsCmd() *cobra.Command {
	var (
		policyFilter    string
		modeFilter      string
		transportFilter string
		best            bool
	)

	cmd := &cobra.Command{
		Use:   "endpoints",
		Short: "List server endpoints",
		Long: `List server endpoints with security and user token information.

--best selects the most secure endpoint. With --cert it only considers
endpoints the key of that certificate can use (RSA or ECC).

Examples:
  opcuactl discover endpoints --endpoint opc.tcp://localhost:4840
  opcuactl discover endpoints --endpoint opc.tcp://localhost:4840 --best
  opcuactl discover endpoints --endpoint opc.tcp://localhost:4840 --best --cert ecc-cert.pem --key ecc-key.pem
  opcuactl discover endpoints --endpoint opc.tcp://localhost:4840 --policy Basic256Sha256 --format json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := connCfg.RequireEndpoint(); err != nil {
				return exitCode(1, err)
			}

			ctx, cancel := connCfg.TimeoutContext(context.Background())
			defer cancel()

			eps, err := opcua.GetEndpoints(ctx, connCfg.Endpoint, connCfg.DiscoveryOptions()...)
			if err != nil {
				format.Errf("getting endpoints: %v", err)
				return exitCode(2, err)
			}

			if best {
				return selectBestEndpoint(eps, policyFilter, modeFilter)
			}
			return listEndpoints(eps, policyFilter, modeFilter, transportFilter)
		},
	}

	cmd.Flags().StringVar(&policyFilter, "policy", "", "Filter by security policy")
	cmd.Flags().StringVar(&modeFilter, "mode", "", "Filter by security mode")
	cmd.Flags().StringVar(&transportFilter, "transport-profile", "", "Filter by transport profile")
	cmd.Flags().BoolVar(&best, "best", false, "Select the best (most secure) endpoint")
	_ = cmd.RegisterFlagCompletionFunc("policy", fixedCompletion(securityPolicyValues))
	_ = cmd.RegisterFlagCompletionFunc("mode", fixedCompletion(securityModeValues))
	return cmd
}

func listEndpoints(eps []*ua.EndpointDescription, policyFilter, modeFilter, transportFilter string) error {
	w := newWriter()
	var endpoints []model.Endpoint
	for _, ep := range eps {
		policyShort := parse.SecurityPolicyShortName(ep.SecurityPolicyURI)
		if policyFilter != "" && !strings.EqualFold(policyShort, policyFilter) {
			continue
		}
		if modeFilter != "" && !strings.EqualFold(ep.SecurityMode.String(), modeFilter) {
			continue
		}
		if transportFilter != "" && !strings.Contains(ep.TransportProfileURI, transportFilter) {
			continue
		}

		e := model.Endpoint{
			EndpointURL:      ep.EndpointURL,
			SecurityPolicy:   policyShort,
			SecurityMode:     ep.SecurityMode.String(),
			SecurityLevel:    ep.SecurityLevel,
			HasCertificate:   len(ep.ServerCertificate) > 0,
			TransportProfile: ep.TransportProfileURI,
		}
		if ep.Server != nil {
			e.ApplicationURI = ep.Server.ApplicationURI
		}
		if len(ep.ServerCertificate) > 0 {
			h := sha256.Sum256(ep.ServerCertificate)
			e.CertThumbprint = fmt.Sprintf("%x", h[:8]) // first 8 bytes = 16 hex chars
		}
		if mismatch := endpointURLMismatch(connCfg.Endpoint, ep.EndpointURL); mismatch != "" {
			e.URLMismatch = mismatch
		}
		for _, tp := range ep.UserIdentityTokens {
			e.UserTokenPolicies = append(e.UserTokenPolicies, model.UserTokenPolicy{
				PolicyID:  tp.PolicyID,
				TokenType: tp.TokenType.String(),
			})
		}
		endpoints = append(endpoints, e)
	}

	f := format.ParseFormat(outputFormat)
	if f == format.Table {
		headers := []string{"ENDPOINT URL", "SECURITY POLICY", "SECURITY MODE", "LEVEL", "CERT", "TOKEN TYPES"}
		if verbose {
			headers = append(headers, "APP URI", "CERT THUMBPRINT", "URL MISMATCH")
		}
		rows := make([][]string, len(endpoints))
		for i, e := range endpoints {
			tokenTypes := make([]string, len(e.UserTokenPolicies))
			for j, tp := range e.UserTokenPolicies {
				tokenTypes[j] = tp.TokenType
			}
			cert := "no"
			if e.HasCertificate {
				cert = "yes"
			}
			row := []string{e.EndpointURL, e.SecurityPolicy, e.SecurityMode, fmt.Sprintf("%d", e.SecurityLevel), cert, strings.Join(tokenTypes, ", ")}
			if verbose {
				row = append(row, e.ApplicationURI, e.CertThumbprint, e.URLMismatch)
			}
			rows[i] = row
		}
		return w.WriteTable(headers, rows)
	}
	return w.Write(endpoints)
}

func selectBestEndpoint(eps []*ua.EndpointDescription, policyFilter, modeFilter string) error {
	w := newWriter()

	if len(eps) == 0 {
		return exitCodef(3, "no endpoints returned by server")
	}

	var ep *ua.EndpointDescription

	// If the user explicitly set --policy or --mode, try an exact match first.
	if policyFilter != "" || modeFilter != "" {
		policy := ""
		if policyFilter != "" {
			policy = parse.SecurityPolicy(policyFilter)
		}
		mode := ua.MessageSecurityModeInvalid
		if modeFilter != "" {
			mode = ua.MessageSecurityModeFromString(modeFilter)
		}
		matched, err := connCfg.SelectEndpoint(eps, policy, mode)
		if err == nil {
			ep = matched
		}
	}

	// With a client certificate (--cert): the best endpoint its key can use,
	// so an RSA certificate is not paired with an ECC endpoint.
	if ep == nil && connCfg.CertFile != "" {
		if matched, err := connCfg.SelectEndpoint(eps, "", ua.MessageSecurityModeInvalid); err == nil {
			ep = matched
		}
	}

	// Fall back: pick the endpoint with the highest security level.
	if ep == nil {
		sorted := make([]*ua.EndpointDescription, len(eps))
		copy(sorted, eps)
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].SecurityLevel > sorted[j].SecurityLevel
		})
		ep = sorted[0]
	}

	e := model.Endpoint{
		EndpointURL:    ep.EndpointURL,
		SecurityPolicy: parse.SecurityPolicyShortName(ep.SecurityPolicyURI),
		SecurityMode:   ep.SecurityMode.String(),
		SecurityLevel:  ep.SecurityLevel,
		HasCertificate: len(ep.ServerCertificate) > 0,
	}
	for _, tp := range ep.UserIdentityTokens {
		e.UserTokenPolicies = append(e.UserTokenPolicies, model.UserTokenPolicy{
			PolicyID:  tp.PolicyID,
			TokenType: tp.TokenType.String(),
		})
	}

	f := format.ParseFormat(outputFormat)
	if f == format.Table {
		headers := []string{"ENDPOINT URL", "SECURITY POLICY", "SECURITY MODE", "LEVEL"}
		rows := [][]string{{e.EndpointURL, e.SecurityPolicy, e.SecurityMode, fmt.Sprintf("%d", e.SecurityLevel)}}
		return w.WriteTable(headers, rows)
	}
	return w.Write(e)
}

// endpointURLMismatch returns a warning string if the host in the discovered
// endpoint URL differs from the host the user connected to.
func endpointURLMismatch(dialURL, endpointURL string) string {
	dialHost := hostFromOPCURL(dialURL)
	epHost := hostFromOPCURL(endpointURL)
	if dialHost == "" || epHost == "" || strings.EqualFold(dialHost, epHost) {
		return ""
	}
	return fmt.Sprintf("dial=%s ep=%s", dialHost, epHost)
}

// hostFromOPCURL extracts the hostname from an opc.tcp:// or http(s):// URL.
func hostFromOPCURL(rawURL string) string {
	// opc.tcp:// is not a standard scheme; normalize for url.Parse.
	normalized := strings.Replace(rawURL, "opc.tcp://", "http://", 1)
	u, err := url.Parse(normalized)
	if err != nil {
		return ""
	}
	return u.Hostname()
}
