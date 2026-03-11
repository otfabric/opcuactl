package opcuactl

import (
	"context"
	"fmt"
	"strings"

	"github.com/otfabric/opcua"
	"github.com/otfabric/opcua/ua"
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
			rows[i] = []string{e.EndpointURL, e.SecurityPolicy, e.SecurityMode, fmt.Sprintf("%d", e.SecurityLevel), cert, strings.Join(tokenTypes, ", ")}
		}
		return w.WriteTable(headers, rows)
	}
	return w.Write(endpoints)
}

func selectBestEndpoint(eps []*ua.EndpointDescription, policyFilter, modeFilter string) error {
	w := newWriter()
	policy := policyFilter
	if policy == "" {
		policy = "Basic256Sha256"
	}
	mode := ua.MessageSecurityModeSignAndEncrypt
	if modeFilter != "" {
		mode = ua.MessageSecurityModeFromString(modeFilter)
	}

	ep, err := opcua.SelectEndpoint(eps, parse.SecurityPolicy(policy), mode)
	if err != nil {
		format.Errf("selecting best endpoint: %v", err)
		return exitCode(3, err)
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
