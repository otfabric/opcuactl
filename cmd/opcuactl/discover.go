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
  opcuactl discover endpoints --endpoint opc.tcp://localhost:4840`,
	}
	cmd.AddCommand(newDiscoverServersCmd(), newDiscoverEndpointsCmd())
	return cmd
}

func newDiscoverServersCmd() *cobra.Command {
	var (
		onNetwork bool
		serverURI string
		locale    string
	)

	cmd := &cobra.Command{
		Use:   "servers",
		Short: "Discover OPC UA servers",
		Long: `Discover OPC UA application descriptions from a discovery endpoint.

Examples:
  opcuactl discover servers --endpoint opc.tcp://localhost:4840
  opcuactl discover servers --endpoint opc.tcp://localhost:4840 --on-network
  opcuactl discover servers --endpoint opc.tcp://localhost:4840 --format json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if connCfg.Endpoint == "" {
				return exitCodef(1, "--endpoint is required")
			}

			ctx, cancel := connCfg.TimeoutContext(context.Background())
			defer cancel()

			w := newWriter()

			if onNetwork {
				return discoverServersOnNetwork(ctx, w)
			}
			return discoverServers(ctx, w, serverURI, locale)
		},
	}

	cmd.Flags().BoolVar(&onNetwork, "on-network", false, "Use FindServersOnNetwork instead of FindServers")
	cmd.Flags().StringVar(&serverURI, "server-uri", "", "Filter by server URI")
	cmd.Flags().StringVar(&locale, "locale", "", "Preferred locale")
	return cmd
}

func discoverServers(ctx context.Context, w *format.Writer, serverURI, locale string) error {
	var opts []opcua.Option
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
		servers = append(servers, s)
	}

	f := format.ParseFormat(outputFormat)
	if f == format.Table {
		headers := []string{"APPLICATION NAME", "APPLICATION URI", "TYPE", "DISCOVERY URLS"}
		rows := make([][]string, len(servers))
		for i, s := range servers {
			rows[i] = []string{s.ApplicationName, s.ApplicationURI, s.ApplicationType, strings.Join(s.DiscoveryURLs, ", ")}
		}
		return w.WriteTable(headers, rows)
	}
	return w.Write(servers)
}

func discoverServersOnNetwork(ctx context.Context, w *format.Writer) error {
	servers, err := opcua.FindServersOnNetwork(ctx, connCfg.Endpoint)
	if err != nil {
		format.Errf("discovering servers on network: %v", err)
		return exitCode(2, err)
	}

	var results []model.ServerOnNetwork
	for _, s := range servers {
		results = append(results, model.ServerOnNetwork{
			RecordID:           s.RecordID,
			ServerName:         s.ServerName,
			DiscoveryURL:       s.DiscoveryURL,
			ServerCapabilities: s.ServerCapabilities,
		})
	}

	f := format.ParseFormat(outputFormat)
	if f == format.Table {
		headers := []string{"SERVER NAME", "DISCOVERY URL", "CAPABILITIES"}
		rows := make([][]string, len(results))
		for i, s := range results {
			rows[i] = []string{s.ServerName, s.DiscoveryURL, strings.Join(s.ServerCapabilities, ", ")}
		}
		return w.WriteTable(headers, rows)
	}
	return w.Write(results)
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

Examples:
  opcuactl discover endpoints --endpoint opc.tcp://localhost:4840
  opcuactl discover endpoints --endpoint opc.tcp://localhost:4840 --best
  opcuactl discover endpoints --endpoint opc.tcp://localhost:4840 --policy Basic256Sha256 --format json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if connCfg.Endpoint == "" {
				return exitCodef(1, "--endpoint is required")
			}

			ctx, cancel := connCfg.TimeoutContext(context.Background())
			defer cancel()

			eps, err := opcua.GetEndpoints(ctx, connCfg.Endpoint)
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
		policy := policyFilter
		if policy == "" {
			policy = "Basic256Sha256"
		}
		mode := ua.MessageSecurityModeSignAndEncrypt
		if modeFilter != "" {
			mode = ua.MessageSecurityModeFromString(modeFilter)
		}
		matched, err := ua.SelectEndpoint(eps, parse.SecurityPolicy(policy), mode)
		if err == nil {
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
