// SPDX-License-Identifier: MIT

package opcuactl

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/otfabric/go-opcua"
	"github.com/otfabric/go-opcua/gds"
	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/go-opcua/uapolicy"
	"github.com/otfabric/opcuactl/internal/format"
	"github.com/otfabric/opcuactl/internal/model"
	"github.com/otfabric/opcuactl/internal/parse"
	"github.com/spf13/cobra"
)

// The pull model of certificate management (Part 12 §6 and §7.9): a client
// of a Global Discovery Server.

func newGDSPullCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pull",
		Short: "Register applications and obtain certificates from a GDS (pull model, Part 12 §7.9)",
		Long: `Talk to a Global Discovery Server (GDS, Part 12 §6 and §7.9) through its
Directory object: find, query, register, update and unregister
applications, request certificates for them (a CSR or a key pair the GDS
generates), collect the issued certificate, read the trust list, check
and revoke certificates. -e is the endpoint of the GDS.

The GDS identifies an application by its ApplicationId, a NodeId it assigns
at registration (gds pull find shows it). Queries are open to everyone;
registration needs the DiscoveryAdmin role, certificate requests,
revocation and trust lists the CertificateAuthorityAdmin role over a
SignAndEncrypt channel, unless the client is the application itself
(connected with a certificate the GDS issued to it).

gds pull enroll does it all at once: it finds or registers the application,
requests a certificate, waits until the GDS issues it and writes the
certificate, the private key and the trust list to files that --cert,
--key and --trusted-cert of opcuactl (or a server) take.

Examples:
  SEC="--security-mode SignAndEncrypt --security-policy Basic256Sha256 --cert app.pem --key app.key --auth username --username gdsadmin --password ..."
  opcuactl gds pull find -e opc.tcp://gds:4840 urn:plant:line4
  opcuactl gds pull register -e opc.tcp://gds:4840 $SEC --uri urn:plant:line4 --name "Line 4" --discovery-url opc.tcp://line4:4840
  opcuactl gds pull enroll -e opc.tcp://gds:4840 $SEC --uri urn:plant:line4 --cert-out line4.pem --key-out line4.key --trust-out ./pki`,
	}
	cmd.AddCommand(
		newGDSPullFindCmd(),
		newGDSPullGetCmd(),
		newGDSPullQueryCmd(),
		newGDSPullQueryServersCmd(),
		newGDSPullRegisterCmd(),
		newGDSPullUpdateCmd(),
		newGDSPullUnregisterCmd(),
		newGDSPullRequestCmd(),
		newGDSPullFinishCmd(false),
		newGDSPullFinishCmd(true),
		newGDSPullTrustListCmd(),
		newGDSPullGroupsCmd(),
		newGDSPullCertsCmd(),
		newGDSPullStatusCmd(),
		newGDSPullRevokeCmd(),
		newGDSPullCheckRevocationCmd(),
		newGDSPullEnrollCmd(),
	)
	return cmd
}

// pullHint explains why a GDS refuses a method.
func pullHint(err error) string {
	switch {
	case errors.Is(err, ua.StatusBadUserAccessDenied):
		return "the GDS needs the DiscoveryAdmin role for registrations and CertificateAuthorityAdmin for certificates, or a session of the application itself"
	case errors.Is(err, ua.StatusBadSecurityModeInsufficient):
		return "certificate requests need an encrypted channel: --security-mode SignAndEncrypt with --security-policy, --cert and --key"
	case errors.Is(err, ua.StatusBadNotFound):
		return "the GDS has no such application (gds pull find)"
	case errors.Is(err, ua.StatusBadNothingToDo):
		return "the request waits for approval by the GDS administrator: gds pull wait collects the certificate once it is issued"
	case errors.Is(err, ua.StatusBadRequestNotAllowed):
		return "the GDS rejected the request"
	case errors.Is(err, ua.StatusBadInvalidArgument):
		return "the GDS does not know this application or request, or the request belongs to another application"
	case errors.Is(err, ua.StatusBadCertificateInvalid):
		return "the GDS cannot use the certificate or request (wrong ApplicationUri, or a key type it does not issue)"
	case errors.Is(err, ua.StatusBadNodeIDUnknown), errors.Is(err, ua.StatusBadMethodInvalid):
		return "the server is not a GDS, or does not implement this method"
	case errors.Is(err, ua.StatusBadTooManyOperations):
		return "the application has too many open requests"
	case err != nil && strings.Contains(err.Error(), "no GDS namespace"):
		return "the server is not a Global Discovery Server"
	}
	return ""
}

func pullFailed(what string, err error) error {
	msg := fmt.Sprintf("%s: %v", what, err)
	if h := pullHint(err); h != "" {
		msg += " (" + h + ")"
	}
	format.Errf("%s", msg)
	return exitCode(3, err)
}

// withPull runs fn with a GDS pull client on a new session.
func withPull(fn func(ctx context.Context, p *gds.PullClient) error) error {
	return withClient(func(ctx context.Context, c *opcua.Client) error {
		return fn(ctx, gds.NewPullClient(c))
	})
}

func nodeIDArg(s, what string) (*ua.NodeID, error) {
	n, err := parse.NodeID(s)
	if err != nil {
		return nil, exitCodef(1, "invalid %s: %v", what, err)
	}
	return n, nil
}

// pullGroup parses --group of the pull commands: empty or default is the
// DefaultApplicationGroup of the GDS (nil), otherwise a NodeId.
func pullGroup(s string) (*ua.NodeID, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "default", "app", "application":
		return nil, nil
	}
	return nodeIDArg(s, "--group")
}

func applicationModel(r *gds.ApplicationRecord) model.GDSApplication {
	m := model.GDSApplication{
		ApplicationURI:     r.ApplicationURI,
		ApplicationType:    parse.ApplicationTypeName(r.ApplicationType),
		ApplicationNames:   []string{},
		ProductURI:         r.ProductURI,
		DiscoveryURLs:      nonNil(r.DiscoveryURLs),
		ServerCapabilities: nonNil(r.ServerCapabilities),
	}
	if r.ApplicationID != nil {
		m.ApplicationID = r.ApplicationID.String()
	}
	for _, n := range r.ApplicationNames {
		if n != nil {
			m.ApplicationNames = append(m.ApplicationNames, n.Text)
		}
	}
	return m
}

func writeApplications(apps []model.GDSApplication) error {
	if apps == nil {
		apps = []model.GDSApplication{}
	}
	if format.ParseFormat(outputFormat) != format.Table {
		return newWriter().Write(apps)
	}
	if quiet {
		for _, a := range apps {
			fmt.Println(a.ApplicationID)
		}
		return nil
	}
	headers := []string{"APPLICATION ID", "APPLICATION URI", "NAME", "TYPE"}
	if verbose {
		headers = append(headers, "PRODUCT URI", "DISCOVERY URLS", "CAPABILITIES")
	}
	rows := make([][]string, len(apps))
	for i, a := range apps {
		name := ""
		if len(a.ApplicationNames) > 0 {
			name = a.ApplicationNames[0]
		}
		rows[i] = []string{a.ApplicationID, a.ApplicationURI, name, a.ApplicationType}
		if verbose {
			rows[i] = append(rows[i], a.ProductURI, strings.Join(a.DiscoveryURLs, ", "), strings.Join(a.ServerCapabilities, ","))
		}
	}
	return newWriter().WriteTable(headers, rows)
}

// --- find / get ---

func newGDSPullFindCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "find <applicationUri>",
		Short: "Find the registrations of an application by its ApplicationUri",
		Long: `Find the applications registered with an ApplicationUri
(FindApplications, Part 12 §6.6.4) and show their ApplicationId, the
identifier the other gds pull commands take. -v adds the product URI,
discovery URLs and server capabilities.

Examples:
  opcuactl gds pull find -e opc.tcp://gds:4840 urn:plant:line4
  opcuactl gds pull find -e opc.tcp://gds:4840 urn:plant:line4 -q`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withPull(func(ctx context.Context, p *gds.PullClient) error {
				recs, err := p.FindApplications(ctx, args[0])
				if err != nil {
					return pullFailed("FindApplications", err)
				}
				apps := make([]model.GDSApplication, 0, len(recs))
				for _, r := range recs {
					apps = append(apps, applicationModel(r))
				}
				return writeApplications(apps)
			})
		},
	}
}

func newGDSPullGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <applicationId>",
		Short: "Show the record of a registered application",
		Long: `Show the application record of an ApplicationId (GetApplication, Part 12
§6.6.7): its ApplicationUri, type, names, product URI, discovery URLs and
server capabilities.

Examples:
  opcuactl gds pull get -e opc.tcp://gds:4840 "ns=2;g=5f0c1e2a-..."`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			appID, err := nodeIDArg(args[0], "ApplicationId")
			if err != nil {
				return err
			}
			return withPull(func(ctx context.Context, p *gds.PullClient) error {
				r, err := p.GetApplication(ctx, appID)
				if err != nil {
					return pullFailed("GetApplication", err)
				}
				a := applicationModel(r)
				if format.ParseFormat(outputFormat) != format.Table {
					return newWriter().Write(a)
				}
				if quiet {
					fmt.Println(a.ApplicationURI)
					return nil
				}
				writeKV("Application "+a.ApplicationID, [][2]string{
					{"Application URI", a.ApplicationURI},
					{"Type", a.ApplicationType},
					{"Names", strings.Join(a.ApplicationNames, ", ")},
					{"Product URI", a.ProductURI},
					{"Discovery URLs", strings.Join(a.DiscoveryURLs, ", ")},
					{"Server capabilities", strings.Join(a.ServerCapabilities, ", ")},
				})
				return nil
			})
		},
	}
}

// --- query ---

func newGDSPullQueryCmd() *cobra.Command {
	var (
		name, uri, typ, product string
		caps                    []string
		start, maxRecords       uint32
	)
	cmd := &cobra.Command{
		Use:     "query",
		Aliases: []string{"query-applications"},
		Short:   "Query the registered applications",
		Long: `Query the applications registered with the GDS (QueryApplications, Part 12
§6.6.9). The filters --name, --uri and --product-uri take the patterns of
the LIKE operator (% for any text, _ for one character); --type
(server|client|clientandserver|discoveryserver) and --capability
(repeatable) narrow the result. --start and --max page through it: the
next page starts at the NextRecordId that --format json shows.

Examples:
  opcuactl gds pull query -e opc.tcp://gds:4840
  opcuactl gds pull query -e opc.tcp://gds:4840 --uri "urn:plant:%" --type server
  opcuactl gds pull query -e opc.tcp://gds:4840 --max 50 --format json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			q := &gds.ApplicationQuery{StartingRecordID: start, MaxRecordsToReturn: maxRecords,
				ApplicationName: name, ApplicationURI: uri, ProductURI: product, ServerCapabilities: caps}
			if typ != "" {
				t, err := parse.ApplicationType(typ)
				if err != nil {
					return exitCodef(1, "invalid --type: %v", err)
				}
				// QueryApplications: 0 all, 1 servers, 2 clients.
				switch t {
				case ua.ApplicationTypeClient:
					q.ApplicationType = 2
				default:
					q.ApplicationType = 1
				}
			}
			return withPull(func(ctx context.Context, p *gds.PullClient) error {
				res, err := p.QueryApplications(ctx, q)
				if err != nil {
					return pullFailed("QueryApplications", err)
				}
				out := model.GDSApplicationQuery{NextRecordID: res.NextRecordID, Applications: []model.GDSApplication{}}
				if !res.LastCounterResetTime.IsZero() {
					out.LastCounterResetTime = model.FormatTimestamp(res.LastCounterResetTime)
				}
				for _, a := range res.Applications {
					m := model.GDSApplication{
						ApplicationURI:     a.ApplicationURI,
						ApplicationType:    parse.ApplicationTypeName(a.ApplicationType),
						ApplicationNames:   []string{},
						ProductURI:         a.ProductURI,
						DiscoveryURLs:      nonNil(a.DiscoveryURLs),
						ServerCapabilities: []string{},
					}
					if a.ApplicationName != nil {
						m.ApplicationNames = append(m.ApplicationNames, a.ApplicationName.Text)
					}
					out.Applications = append(out.Applications, m)
				}
				if format.ParseFormat(outputFormat) != format.Table {
					return newWriter().Write(out)
				}
				if quiet {
					for _, a := range out.Applications {
						fmt.Println(a.ApplicationURI)
					}
					return nil
				}
				rows := make([][]string, len(out.Applications))
				for i, a := range out.Applications {
					rows[i] = []string{a.ApplicationURI, strings.Join(a.ApplicationNames, ", "), a.ApplicationType, a.ProductURI, strings.Join(a.DiscoveryURLs, ", ")}
				}
				if err := newWriter().WriteTable([]string{"APPLICATION URI", "NAME", "TYPE", "PRODUCT URI", "DISCOVERY URLS"}, rows); err != nil {
					return err
				}
				if res.NextRecordID != 0 {
					format.Infof("more applications: --start %d", res.NextRecordID)
				}
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "ApplicationName pattern")
	cmd.Flags().StringVar(&uri, "uri", "", "ApplicationUri pattern")
	cmd.Flags().StringVar(&typ, "type", "", "Application type: server|client (clientandserver matches both)")
	cmd.Flags().StringVar(&product, "product-uri", "", "ProductUri pattern")
	cmd.Flags().StringArrayVar(&caps, "capability", nil, "Server capability the application must have (repeatable)")
	cmd.Flags().Uint32Var(&start, "start", 0, "First record (the NextRecordId of the previous page)")
	cmd.Flags().Uint32Var(&maxRecords, "max", 0, "Maximum number of records (0: no limit)")
	return cmd
}

func newGDSPullQueryServersCmd() *cobra.Command {
	var (
		name, uri, product string
		caps               []string
		start, maxRecords  uint32
	)
	cmd := &cobra.Command{
		Use:   "query-servers",
		Short: "Query the registered servers",
		Long: `Query the servers registered with the GDS (QueryServers, Part 12 §6.6.10):
one line per discovery URL, with the record id and the server
capabilities. The filters are those of gds pull query.

Examples:
  opcuactl gds pull query-servers -e opc.tcp://gds:4840
  opcuactl gds pull query-servers -e opc.tcp://gds:4840 --capability DA --format json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			q := &gds.ServerQuery{StartingRecordID: start, MaxRecordsToReturn: maxRecords,
				ApplicationName: name, ApplicationURI: uri, ProductURI: product, ServerCapabilities: caps}
			return withPull(func(ctx context.Context, p *gds.PullClient) error {
				res, err := p.QueryServers(ctx, q)
				if err != nil {
					return pullFailed("QueryServers", err)
				}
				out := model.GDSServerQuery{Servers: []model.GDSServer{}}
				if !res.LastCounterResetTime.IsZero() {
					out.LastCounterResetTime = model.FormatTimestamp(res.LastCounterResetTime)
				}
				for _, s := range res.Servers {
					out.Servers = append(out.Servers, model.GDSServer{RecordID: s.RecordID, ServerName: s.ServerName,
						DiscoveryURL: s.DiscoveryURL, ServerCapabilities: nonNil(s.ServerCapabilities)})
				}
				if format.ParseFormat(outputFormat) != format.Table {
					return newWriter().Write(out)
				}
				if quiet {
					for _, s := range out.Servers {
						fmt.Println(s.DiscoveryURL)
					}
					return nil
				}
				rows := make([][]string, len(out.Servers))
				for i, s := range out.Servers {
					rows[i] = []string{fmt.Sprint(s.RecordID), s.ServerName, s.DiscoveryURL, strings.Join(s.ServerCapabilities, ",")}
				}
				return newWriter().WriteTable([]string{"RECORD ID", "SERVER NAME", "DISCOVERY URL", "CAPABILITIES"}, rows)
			})
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "ApplicationName pattern")
	cmd.Flags().StringVar(&uri, "uri", "", "ApplicationUri pattern")
	cmd.Flags().StringVar(&product, "product-uri", "", "ProductUri pattern")
	cmd.Flags().StringArrayVar(&caps, "capability", nil, "Server capability the server must have (repeatable)")
	cmd.Flags().Uint32Var(&start, "start", 0, "First record id")
	cmd.Flags().Uint32Var(&maxRecords, "max", 0, "Maximum number of records (0: no limit)")
	return cmd
}

// --- register / update / unregister ---

// recordFlags are the fields of an application record.
type recordFlags struct {
	uri, typ, product string
	names, urls, caps []string
}

func (f *recordFlags) register(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.uri, "uri", "", "ApplicationUri")
	cmd.Flags().StringArrayVar(&f.names, "name", nil, `ApplicationName ("text" or "locale:text"; repeatable)`)
	cmd.Flags().StringVar(&f.typ, "type", "", "Application type: server|client|clientandserver|discoveryserver (default server)")
	cmd.Flags().StringVar(&f.product, "product-uri", "", "ProductUri (default: the ApplicationUri)")
	cmd.Flags().StringArrayVar(&f.urls, "discovery-url", nil, "Discovery URL of a server (repeatable)")
	cmd.Flags().StringArrayVar(&f.caps, "capability", nil, "Server capability, e.g. DA, HD, AC (repeatable)")
	_ = cmd.RegisterFlagCompletionFunc("type", fixedCompletion([]string{"server", "client", "clientandserver", "discoveryserver"}))
}

// apply sets the fields given on the command line on r.
func (f *recordFlags) apply(cmd *cobra.Command, r *gds.ApplicationRecord) error {
	changed := cmd.Flags().Changed
	if changed("uri") {
		r.ApplicationURI = f.uri
	}
	if changed("type") || r.ApplicationType == 0 && f.typ == "" {
		t, err := parse.ApplicationType(f.typ)
		if err != nil {
			return exitCodef(1, "invalid --type: %v", err)
		}
		r.ApplicationType = t
	}
	if changed("name") {
		r.ApplicationNames = nil
		for _, n := range f.names {
			locale, text, ok := strings.Cut(n, ":")
			if !ok || !isLocale(locale) {
				locale, text = "", n
			}
			if locale != "" {
				r.ApplicationNames = append(r.ApplicationNames, ua.NewLocalizedTextWithLocale(text, locale))
			} else {
				r.ApplicationNames = append(r.ApplicationNames, ua.NewLocalizedText(text))
			}
		}
	}
	if changed("product-uri") {
		r.ProductURI = f.product
	}
	if changed("discovery-url") {
		r.DiscoveryURLs = f.urls
	}
	if changed("capability") {
		r.ServerCapabilities = f.caps
	}
	if r.ApplicationURI == "" {
		return exitCodef(1, "--uri is required")
	}
	if len(r.ApplicationNames) == 0 {
		r.ApplicationNames = []*ua.LocalizedText{ua.NewLocalizedText(r.ApplicationURI)}
	}
	if r.ProductURI == "" {
		r.ProductURI = r.ApplicationURI
	}
	return nil
}

// isLocale reports whether s looks like a locale id (en, en-US, de-DE).
func isLocale(s string) bool {
	if len(s) < 2 || len(s) > 8 {
		return false
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && r != '-' {
			return false
		}
	}
	return true
}

func newGDSPullRegisterCmd() *cobra.Command {
	var rf recordFlags
	cmd := &cobra.Command{
		Use:   "register",
		Short: "Register an application",
		Long: `Register an application with the GDS (RegisterApplication, Part 12
§6.6.5; the DiscoveryAdmin role) and print the ApplicationId the GDS
assigned. --uri is required; --name defaults to the URI. A server needs at
least one --discovery-url.

Examples:
  opcuactl gds pull register -e opc.tcp://gds:4840 $SEC --uri urn:plant:line4 --name "Line 4 PLC" \
    --discovery-url opc.tcp://line4:4840 --capability DA --product-uri urn:vendor:plc`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r := &gds.ApplicationRecord{}
			if err := rf.apply(cmd, r); err != nil {
				return err
			}
			return withPull(func(ctx context.Context, p *gds.PullClient) error {
				appID, err := p.RegisterApplication(ctx, r)
				if err != nil {
					return pullFailed("RegisterApplication", err)
				}
				r.ApplicationID = appID
				a := applicationModel(r)
				if format.ParseFormat(outputFormat) != format.Table {
					return newWriter().Write(a)
				}
				if quiet {
					fmt.Println(a.ApplicationID)
					return nil
				}
				fmt.Printf("Registered %s: ApplicationId %s\n", a.ApplicationURI, a.ApplicationID)
				return nil
			})
		},
	}
	rf.register(cmd)
	return cmd
}

func newGDSPullUpdateCmd() *cobra.Command {
	var rf recordFlags
	cmd := &cobra.Command{
		Use:   "update <applicationId>",
		Short: "Change the record of a registered application",
		Long: `Change the record of a registered application (UpdateApplication, Part 12
§6.6.6): opcuactl reads the record and replaces the fields given on the
command line; repeatable flags replace the whole list.

Examples:
  opcuactl gds pull update -e opc.tcp://gds:4840 $SEC "ns=2;g=5f0c..." --discovery-url opc.tcp://line4.plant:4840`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			appID, err := nodeIDArg(args[0], "ApplicationId")
			if err != nil {
				return err
			}
			return withPull(func(ctx context.Context, p *gds.PullClient) error {
				r, err := p.GetApplication(ctx, appID)
				if err != nil {
					return pullFailed("GetApplication", err)
				}
				if err := rf.apply(cmd, r); err != nil {
					return err
				}
				r.ApplicationID = appID
				if err := p.UpdateApplication(ctx, r); err != nil {
					return pullFailed("UpdateApplication", err)
				}
				a := applicationModel(r)
				if format.ParseFormat(outputFormat) != format.Table {
					return newWriter().Write(a)
				}
				if !quiet {
					fmt.Printf("Updated %s (%s)\n", a.ApplicationID, a.ApplicationURI)
				}
				return nil
			})
		},
	}
	rf.register(cmd)
	return cmd
}

func newGDSPullUnregisterCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "unregister <applicationId>",
		Short: "Unregister an application (and revoke its certificates)",
		Long: `Unregister an application (UnregisterApplication, Part 12 §6.6.8). The GDS
revokes the certificates it issued to the application. Asks for
confirmation on a terminal (--yes skips it).

Examples:
  opcuactl gds pull unregister -e opc.tcp://gds:4840 $SEC "ns=2;g=5f0c..." --yes`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			appID, err := nodeIDArg(args[0], "ApplicationId")
			if err != nil {
				return err
			}
			if !confirmed(yes, os.Stdin, fmt.Sprintf("Unregister %s from %s and revoke its certificates?", appID, connCfg.Endpoint)) {
				return nil
			}
			return withPull(func(ctx context.Context, p *gds.PullClient) error {
				if err := p.UnregisterApplication(ctx, appID); err != nil {
					return pullFailed("UnregisterApplication", err)
				}
				return writeMethodStatus("UnregisterApplication", "done")
			})
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip the confirmation prompt")
	return cmd
}

// --- request / finish / wait ---

// requestFlags select the certificate of a request.
type requestFlags struct {
	group, certType string
	subject         string
	dns             []string
}

// register registers the flags; typeFlag names the certificate type flag.
func (f *requestFlags) register(cmd *cobra.Command, typeFlag string) {
	cmd.Flags().StringVar(&f.group, "group", "", "Certificate group of the GDS (NodeId; default its DefaultApplicationGroup)")
	cmd.Flags().StringVar(&f.certType, typeFlag, "", "Certificate type (rsa|ecc-p256|...|ed448 or a NodeId; default the first type of the group)")
	cmd.Flags().StringVar(&f.subject, "subject", "", "Subject (CN=App/O=Company; default chosen by the GDS)")
	cmd.Flags().StringArrayVar(&f.dns, "dns", nil, "Host name or IP address of the certificate (repeatable; default those of the discovery URLs)")
	_ = cmd.RegisterFlagCompletionFunc(typeFlag, fixedCompletion(certificateTypeValues()))
}

func (f *requestFlags) parse() (group, certType *ua.NodeID, err error) {
	if group, err = pullGroup(f.group); err != nil {
		return nil, nil, err
	}
	if certType, err = parse.CertificateType(f.certType); err != nil {
		return nil, nil, exitCodef(1, "invalid --type: %v", err)
	}
	return group, certType, nil
}

// readCSR reads a PKCS #10 request (PEM or DER).
func readCSR(file string) ([]byte, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	if block, _ := pem.Decode(b); block != nil {
		if !strings.Contains(block.Type, "CERTIFICATE REQUEST") {
			return nil, fmt.Errorf("%s: a %s, not a CERTIFICATE REQUEST", file, block.Type)
		}
		return block.Bytes, nil
	}
	return b, nil
}

// readSigner reads a private key file.
func readSigner(file string) (crypto.Signer, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	k, err := uapolicy.ParsePrivateKey(b)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	return k, nil
}

func newGDSPullRequestCmd() *cobra.Command {
	var (
		rf          requestFlags
		csrFile     string
		keyFile     string
		keyPassword string
	)
	cmd := &cobra.Command{
		Use:   "request <applicationId>",
		Short: "Request a certificate for an application",
		Long: `Request a certificate for a registered application and print the
RequestId, which gds pull finish and gds pull wait take.

  --csr FILE          send a PKCS #10 request (PEM or DER) of the
                      application (StartSigningRequest, Part 12 §7.9.4)
  --private-key FILE  make the request from this private key here, with
                      the ApplicationUri of the record, --subject and --dns
                      (StartSigningRequest; RSA, NIST ECDSA and Ed25519 keys)
  neither             the GDS generates the key pair (StartNewKeyPairRequest,
                      §7.9.5); --key-password encrypts the key it returns

--group selects a certificate group of the GDS, --type the certificate
type. A GDS may hold the request until an administrator approves it.

Examples:
  opcuactl gds pull request -e opc.tcp://gds:4840 $SEC "ns=2;g=5f0c..." --csr line4.csr
  opcuactl gds pull request -e opc.tcp://gds:4840 $SEC "ns=2;g=5f0c..." --type ecc-p256 --dns line4.plant
  opcuactl gds pull request -e opc.tcp://gds:4840 $SEC "ns=2;g=5f0c..." --private-key line4.key -q`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			appID, err := nodeIDArg(args[0], "ApplicationId")
			if err != nil {
				return err
			}
			group, certType, err := rf.parse()
			if err != nil {
				return err
			}
			var (
				csr []byte
				key crypto.Signer
			)
			switch {
			case csrFile != "" && keyFile != "":
				return exitCodef(1, "--csr and --private-key exclude each other")
			case csrFile != "":
				if csr, err = readCSR(csrFile); err != nil {
					return exitCode(1, err)
				}
			case keyFile != "":
				if key, err = readSigner(keyFile); err != nil {
					return exitCode(1, err)
				}
			}
			if keyPassword != "" && (csr != nil || key != nil) {
				return exitCodef(1, "--key-password is for a key pair the GDS generates (without --csr or --private-key)")
			}
			return withPull(func(ctx context.Context, p *gds.PullClient) error {
				res := model.GDSRequest{ApplicationID: appID.String(), Kind: "signing-request"}
				if key != nil {
					rec, err := p.GetApplication(ctx, appID)
					if err != nil {
						return pullFailed("GetApplication", err)
					}
					if csr, err = gds.NewCertificateRequest(key, rec.ApplicationURI, rf.subject, rf.dns); err != nil {
						format.Errf("making the certificate request: %v", err)
						return exitCode(1, err)
					}
				}
				var reqID *ua.NodeID
				if csr != nil {
					reqID, err = p.StartSigningRequest(ctx, appID, group, certType, csr)
					if err != nil {
						return pullFailed("StartSigningRequest", err)
					}
				} else {
					res.Kind = "new-key-pair"
					reqID, err = p.StartNewKeyPairRequest(ctx, appID, &gds.NewKeyPairRequest{CertificateGroup: group, CertificateType: certType,
						SubjectName: rf.subject, DomainNames: rf.dns, PrivateKeyPassword: keyPassword})
					if err != nil {
						return pullFailed("StartNewKeyPairRequest", err)
					}
				}
				res.RequestID = reqID.String()
				if format.ParseFormat(outputFormat) != format.Table {
					return newWriter().Write(res)
				}
				if quiet {
					fmt.Println(res.RequestID)
					return nil
				}
				fmt.Printf("Request %s (%s) for %s; collect the certificate with gds pull wait %q %q\n", res.RequestID, res.Kind, res.ApplicationID, res.ApplicationID, res.RequestID)
				return nil
			})
		},
	}
	rf.register(cmd, "type")
	cmd.Flags().StringVar(&csrFile, "csr", "", "PKCS #10 certificate request of the application (PEM or DER)")
	cmd.Flags().StringVar(&keyFile, "private-key", "", "Private key to make the certificate request from")
	cmd.Flags().StringVar(&keyPassword, "key-password", "", "Password that encrypts the private key the GDS generates")
	return cmd
}

// issuedFiles are the files gds pull finish, wait and enroll write.
type issuedFiles struct {
	certOut, keyOut, issuersOut string
}

func (f *issuedFiles) register(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.certOut, "cert-out", "", "Write the certificate to this file (PEM; DER for .der)")
	cmd.Flags().StringVar(&f.keyOut, "key-out", "", "Write the private key the GDS generated to this file (PEM, mode 0600)")
	cmd.Flags().StringVar(&f.issuersOut, "issuers-out", "", "Write the issuer (CA) certificates to this file (PEM)")
}

// keyFile is where the private key goes: --key-out, else next to
// --cert-out.
func (f *issuedFiles) keyFile() string {
	if f.keyOut != "" || f.certOut == "" {
		return f.keyOut
	}
	return strings.TrimSuffix(f.certOut, filepath.Ext(f.certOut)) + ".key"
}

func writePEMFile(file, typ string, ders ...[]byte) error {
	var b []byte
	for _, der := range ders {
		b = append(b, pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der})...)
	}
	return os.WriteFile(file, b, 0o644) //nolint:gosec // certificates are public
}

// writeIssued writes the certificate, the key and the issuers and returns
// their summary.
func writeIssued(f *issuedFiles, appID, reqID string, cert []byte, key []byte, issuers [][]byte) (*model.GDSIssued, error) {
	out := &model.GDSIssued{ApplicationID: appID, RequestID: reqID, Certificate: describe("certificate", cert), Issuers: []model.GDSCertificate{}}
	for _, der := range issuers {
		out.Issuers = append(out.Issuers, describe("issuer", der))
	}
	if f.certOut != "" {
		var err error
		if strings.EqualFold(filepath.Ext(f.certOut), ".der") {
			err = os.WriteFile(f.certOut, cert, 0o644) //nolint:gosec // a certificate is public
		} else {
			err = writePEMFile(f.certOut, "CERTIFICATE", cert)
		}
		if err != nil {
			return nil, err
		}
		out.CertFile = f.certOut
	}
	if len(key) > 0 {
		if kf := f.keyFile(); kf != "" {
			if err := os.WriteFile(kf, key, 0o600); err != nil {
				return nil, err
			}
			out.KeyFile = kf
		} else {
			out.PrivateKeyPEM = string(key)
			format.Infof("warning: the GDS generated the private key and returns it only once; it is printed because --key-out was not given")
		}
	}
	if f.issuersOut != "" && len(issuers) > 0 {
		if err := writePEMFile(f.issuersOut, "CERTIFICATE", issuers...); err != nil {
			return nil, err
		}
		out.IssuersFile = f.issuersOut
	}
	return out, nil
}

func printIssued(out *model.GDSIssued, title string, cert []byte) error {
	if format.ParseFormat(outputFormat) != format.Table {
		return newWriter().Write(out)
	}
	if quiet {
		fmt.Println(out.Certificate.Thumbprint)
		return nil
	}
	issuers := make([]string, len(out.Issuers))
	for i, e := range out.Issuers {
		issuers[i] = e.Subject
	}
	pairs := [][2]string{
		{"Application", out.ApplicationID},
		{"Subject", out.Certificate.Subject},
		{"Issuer", out.Certificate.Issuer},
		{"Thumbprint", out.Certificate.Thumbprint},
		{"Valid until", out.Certificate.NotAfter},
		{"Issuers", strings.Join(issuers, "; ")},
	}
	for _, p := range [][2]string{{"Certificate file", out.CertFile}, {"Key file", out.KeyFile}, {"Issuers file", out.IssuersFile}, {"Trust list", out.TrustDir}} {
		if p[1] != "" {
			pairs = append(pairs, p)
		}
	}
	if out.TrustList != nil {
		pairs = append(pairs, [2]string{"Trust list entries", fmt.Sprint(len(out.TrustList))})
	}
	writeKV(title, pairs)
	if out.CertFile == "" {
		fmt.Print(string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert})))
	}
	if out.PrivateKeyPEM != "" {
		fmt.Print(out.PrivateKeyPEM)
	}
	return nil
}

func newGDSPullFinishCmd(wait bool) *cobra.Command {
	var (
		files    issuedFiles
		poll     time.Duration
		waitTime time.Duration
	)
	use, short, long := "finish", "Collect the certificate of a request (FinishRequest)", `Collect the certificate of a request (FinishRequest, Part 12 §7.9.6). A
request that waits for approval fails with Bad_NothingToDo; gds pull wait
polls until the GDS issues the certificate.`
	if wait {
		use, short, long = "wait", "Wait for the certificate of a request and collect it", `Wait until the GDS issues the certificate of a request and collect it:
FinishRequest every --poll interval while the request waits for approval,
for at most --wait-timeout (Part 12 §7.9.6).`
	}
	cmd := &cobra.Command{
		Use:   use + " <applicationId> <requestId>",
		Short: short,
		Long: long + `

--cert-out writes the certificate (PEM; DER for .der), --issuers-out the CA
certificates. For a key pair the GDS generated, the private key goes to
--key-out, or next to --cert-out with the extension .key; the GDS returns it
only once. Without --cert-out the certificate is printed as PEM.

Examples:
  opcuactl gds pull ` + use + ` -e opc.tcp://gds:4840 $SEC "ns=2;g=5f0c..." "ns=2;g=77a1..." --cert-out line4.pem --key-out line4.key
  opcuactl gds pull ` + use + ` -e opc.tcp://gds:4840 $SEC "ns=2;g=5f0c..." "ns=2;g=77a1..." --cert-out line4.der --issuers-out ca.pem`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			appID, err := nodeIDArg(args[0], "ApplicationId")
			if err != nil {
				return err
			}
			reqID, err := nodeIDArg(args[1], "RequestId")
			if err != nil {
				return err
			}
			timeout := connCfg.Timeout
			if wait {
				timeout = waitTime
			}
			return withClientTimeout(timeout, func(ctx context.Context, c *opcua.Client) error {
				p := gds.NewPullClient(c)
				var res *gds.IssuedCertificate
				if wait {
					res, err = p.WaitForCertificate(ctx, appID, reqID, poll)
				} else {
					res, err = p.FinishRequest(ctx, appID, reqID)
				}
				if err != nil {
					return pullFailed("FinishRequest", err)
				}
				out, err := writeIssued(&files, appID.String(), reqID.String(), res.Certificate, res.PrivateKey, res.IssuerCertificates)
				if err != nil {
					return exitCode(1, err)
				}
				return printIssued(out, "Certificate issued", res.Certificate)
			})
		},
	}
	files.register(cmd)
	if wait {
		cmd.Flags().DurationVar(&poll, "poll", 5*time.Second, "Interval of the FinishRequest calls")
		cmd.Flags().DurationVar(&waitTime, "wait-timeout", 10*time.Minute, "How long to wait for the certificate (0: forever)")
	}
	return cmd
}

// --- trust list, groups, certificates, status ---

func newGDSPullTrustListCmd() *cobra.Command {
	var group, out string
	cmd := &cobra.Command{
		Use:   "trustlist <applicationId>",
		Short: "Read the trust list the GDS keeps for an application",
		Long: `Read the trust list of a certificate group of the application
(GetTrustList, then the TrustList file, Part 12 §7.9.12): the CA
certificates and CRLs the application is to trust. --out writes the entries
as DER files in the PKI layout (trusted/certs, trusted/crl, issuer/certs,
issuer/crl) that --trusted-cert and gds trustlist set take.

Examples:
  opcuactl gds pull trustlist -e opc.tcp://gds:4840 $SEC "ns=2;g=5f0c..."
  opcuactl gds pull trustlist -e opc.tcp://gds:4840 $SEC "ns=2;g=5f0c..." --out ./pki`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			appID, err := nodeIDArg(args[0], "ApplicationId")
			if err != nil {
				return err
			}
			grp, err := pullGroup(group)
			if err != nil {
				return err
			}
			return withPull(func(ctx context.Context, p *gds.PullClient) error {
				tl, err := p.ReadTrustList(ctx, appID, grp)
				if err != nil {
					return pullFailed("reading the trust list", err)
				}
				entries, err := trustListEntries(tl, out)
				if err != nil {
					return exitCode(1, err)
				}
				return writeCertificates(entries)
			})
		},
	}
	cmd.Flags().StringVar(&group, "group", "", "Certificate group of the GDS (NodeId; default its DefaultApplicationGroup)")
	cmd.Flags().StringVar(&out, "out", "", "Write the entries as DER files into this directory (trusted/certs, trusted/crl, issuer/certs, issuer/crl)")
	return cmd
}

// trustListEntries describes the entries of a trust list and writes them
// into dir in the PKI layout when dir is set.
func trustListEntries(tl *ua.TrustListDataType, dir string) ([]model.GDSCertificate, error) {
	entries := []model.GDSCertificate{}
	for i, l := range trustListLists(tl) {
		for _, der := range *l {
			e := describe(pkiDirs[i].list, der)
			if dir != "" {
				if err := writeEntry(filepath.Join(dir, pkiDirs[i].dir), &e, der); err != nil {
					return nil, err
				}
			}
			entries = append(entries, e)
		}
	}
	return entries, nil
}

func newGDSPullGroupsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "groups <applicationId>",
		Short: "List the certificate groups of an application",
		Long: `List the certificate groups the GDS keeps for an application
(GetCertificateGroups, Part 12 §7.9.10), as NodeIds for --group.

Examples:
  opcuactl gds pull groups -e opc.tcp://gds:4840 $SEC "ns=2;g=5f0c..."`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			appID, err := nodeIDArg(args[0], "ApplicationId")
			if err != nil {
				return err
			}
			return withPull(func(ctx context.Context, p *gds.PullClient) error {
				groups, err := p.GetCertificateGroups(ctx, appID)
				if err != nil {
					return pullFailed("GetCertificateGroups", err)
				}
				ids := make([]string, len(groups))
				for i, g := range groups {
					ids[i] = g.String()
				}
				if format.ParseFormat(outputFormat) != format.Table {
					return newWriter().Write(ids)
				}
				for _, s := range ids {
					fmt.Println(s)
				}
				return nil
			})
		},
	}
}

func newGDSPullCertsCmd() *cobra.Command {
	var group, out string
	cmd := &cobra.Command{
		Use:     "certs <applicationId>",
		Aliases: []string{"certificates"},
		Short:   "List the certificates the GDS issued to an application",
		Long: `List the certificates the GDS issued to an application (GetCertificates,
Part 12 §7.9.11), in all groups or in --group. --out writes them as DER
files named by their thumbprint.

Examples:
  opcuactl gds pull certs -e opc.tcp://gds:4840 $SEC "ns=2;g=5f0c..."`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			appID, err := nodeIDArg(args[0], "ApplicationId")
			if err != nil {
				return err
			}
			grp, err := pullGroup(group)
			if err != nil {
				return err
			}
			return withPull(func(ctx context.Context, p *gds.PullClient) error {
				types, certs, err := p.GetCertificates(ctx, appID, grp)
				if err != nil {
					return pullFailed("GetCertificates", err)
				}
				entries := make([]model.GDSCertificate, 0, len(certs))
				for i, der := range certs {
					e := describe("certificate", der)
					if grp != nil {
						e.Group = grp.String()
					}
					if i < len(types) {
						e.CertificateType = parse.CertificateTypeName(types[i])
					}
					if out != "" {
						if err := writeEntry(out, &e, der); err != nil {
							return exitCode(1, err)
						}
					}
					entries = append(entries, e)
				}
				return writeGroupCertificates(entries)
			})
		},
	}
	cmd.Flags().StringVar(&group, "group", "", "Certificate group of the GDS (NodeId; default all groups)")
	cmd.Flags().StringVar(&out, "out", "", "Write the certificates as DER files into this directory")
	return cmd
}

func newGDSPullStatusCmd() *cobra.Command {
	var group, certType string
	cmd := &cobra.Command{
		Use:   "status <applicationId>",
		Short: "Ask whether an application should renew its certificate",
		Long: `Ask the GDS whether the application should request a new certificate of
--type in --group (GetCertificateStatus, Part 12 §7.9.13): because it
expires soon, the CA changed or it was revoked. The exit code is 0 either
way; -q prints true or false.

Examples:
  opcuactl gds pull status -e opc.tcp://gds:4840 $SEC "ns=2;g=5f0c..."
  opcuactl gds pull status -e opc.tcp://gds:4840 $SEC "ns=2;g=5f0c..." --type rsa -q`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			appID, err := nodeIDArg(args[0], "ApplicationId")
			if err != nil {
				return err
			}
			grp, err := pullGroup(group)
			if err != nil {
				return err
			}
			typ, err := parse.CertificateType(certType)
			if err != nil {
				return exitCodef(1, "invalid --type: %v", err)
			}
			return withPull(func(ctx context.Context, p *gds.PullClient) error {
				update, err := p.GetCertificateStatus(ctx, appID, grp, typ)
				if err != nil {
					return pullFailed("GetCertificateStatus", err)
				}
				res := model.GDSCertificateStatus{ApplicationID: appID.String(), CertificateType: parse.CertificateTypeName(typ), UpdateRequired: update}
				if grp != nil {
					res.Group = grp.String()
				}
				if format.ParseFormat(outputFormat) != format.Table {
					return newWriter().Write(res)
				}
				switch {
				case quiet:
					fmt.Println(update)
				case update:
					fmt.Println("Update required: request a new certificate (gds pull request or gds pull enroll)")
				default:
					fmt.Println("No update required")
				}
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&group, "group", "", "Certificate group of the GDS (NodeId; default its DefaultApplicationGroup)")
	cmd.Flags().StringVar(&certType, "type", "", "Certificate type (default any type of the group)")
	return cmd
}

// --- revocation ---

func newGDSPullRevokeCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "revoke <applicationId> <cert>",
		Short: "Revoke a certificate the GDS issued",
		Long: `Revoke a certificate the GDS issued to the application
(RevokeCertificate, Part 12 §7.9.9; the CertificateAuthorityAdmin role).
The GDS adds it to the CRL of its CA, which the trust lists then carry.
<cert> is a PEM or DER file. Asks for confirmation on a terminal (--yes
skips it).

Examples:
  opcuactl gds pull revoke -e opc.tcp://gds:4840 $SEC "ns=2;g=5f0c..." line4.pem --yes`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			appID, err := nodeIDArg(args[0], "ApplicationId")
			if err != nil {
				return err
			}
			certs, err := readCerts(args[1])
			if err != nil {
				return exitCode(1, err)
			}
			e := describe("certificate", certs[0])
			if !confirmed(yes, os.Stdin, fmt.Sprintf("Revoke %s (%s)?", e.Thumbprint, e.Subject)) {
				return nil
			}
			return withPull(func(ctx context.Context, p *gds.PullClient) error {
				if err := p.RevokeCertificate(ctx, appID, certs[0]); err != nil {
					return pullFailed("RevokeCertificate", err)
				}
				res := model.GDSResult{Action: "revoke", Thumbprints: []string{e.Thumbprint}}
				if format.ParseFormat(outputFormat) != format.Table {
					return newWriter().Write(res)
				}
				if !quiet {
					fmt.Printf("Revoked %s (%s)\n", e.Thumbprint, e.Subject)
				}
				return nil
			})
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip the confirmation prompt")
	return cmd
}

func newGDSPullCheckRevocationCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "check-revocation <cert>",
		Aliases: []string{"check"},
		Short:   "Ask the GDS whether a certificate is valid",
		Long: `Ask the GDS whether a certificate is valid (CheckRevocationStatus, Part
12 §7.9.14): Good, or why not (Bad_CertificateRevoked,
Bad_CertificateUntrusted, ...), and until when the answer holds. The exit
code is 0 for Good and 4 otherwise. <cert> is a PEM or DER file.

Examples:
  opcuactl gds pull check-revocation -e opc.tcp://gds:4840 client.pem`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			certs, err := readCerts(args[0])
			if err != nil {
				return exitCode(1, err)
			}
			e := describe("certificate", certs[0])
			return withPull(func(ctx context.Context, p *gds.PullClient) error {
				st, until, err := p.CheckRevocationStatus(ctx, certs[0])
				if err != nil {
					return pullFailed("CheckRevocationStatus", err)
				}
				res := model.GDSRevocationStatus{Thumbprint: e.Thumbprint, Subject: e.Subject, Status: st.Symbol()}
				if !until.IsZero() {
					res.ValidUntil = model.FormatTimestamp(until)
				}
				var werr error
				switch {
				case format.ParseFormat(outputFormat) != format.Table:
					werr = newWriter().Write(res)
				case quiet:
					fmt.Println(res.Status)
				default:
					writeKV("Revocation status of "+e.Thumbprint, [][2]string{
						{"Subject", res.Subject},
						{"Status", res.Status},
						{"Valid until", res.ValidUntil},
					})
				}
				if werr != nil {
					return werr
				}
				if st != ua.StatusOK {
					return exitCode(4, st)
				}
				return nil
			})
		},
	}
}

// --- enroll ---

func newGDSPullEnrollCmd() *cobra.Command {
	var (
		rf       recordFlags
		req      requestFlags
		files    issuedFiles
		appIDArg string
		keyFile  string
		newKey   string
		trustOut string
		poll     time.Duration
		waitTime time.Duration
	)
	cmd := &cobra.Command{
		Use:   "enroll",
		Short: "Obtain a certificate and trust list from a GDS in one step",
		Long: `Enroll an application with the GDS (Part 12 §7.9): find the application
by --uri (or take --application-id), register it if the GDS does not know
it (--name, --type, --discovery-url, --capability, --product-uri), request
a certificate, wait until the GDS issues it (--poll, --wait-timeout) and
read the trust list.

The key: --private-key FILE makes a certificate request from an existing
private key; --new-key rsa|ecc-p256|ecc-p384|ed25519 generates one here; without
either the GDS generates the key pair.

The result goes to files: --cert-out the certificate (PEM; DER for .der),
--key-out the private key (default next to --cert-out with .key; mode
0600), --issuers-out the CA certificates, --trust-out the trust list in the
PKI layout (trusted/certs, trusted/crl, issuer/certs, issuer/crl).
--cert-out is required.

Examples:
  opcuactl gds pull enroll -e opc.tcp://gds:4840 $SEC --uri urn:plant:line4 --name "Line 4" \
    --discovery-url opc.tcp://line4:4840 --cert-out line4.pem --trust-out ./pki
  opcuactl gds pull enroll -e opc.tcp://gds:4840 $SEC --uri urn:plant:hmi --type client \
    --new-key ecc-p256 --cert-out hmi.pem --key-out hmi.key
  opcuactl --cert hmi.pem --key hmi.key --trusted-cert ./pki/trusted/certs/... get value ...`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if files.certOut == "" {
				return exitCodef(1, "--cert-out is required")
			}
			group, certType, err := req.parse()
			if err != nil {
				return err
			}
			r := &gds.EnrollRequest{CertificateGroup: group, CertificateType: certType, SubjectName: req.subject, DomainNames: req.dns, PollInterval: poll}
			if appIDArg != "" {
				if r.ApplicationID, err = nodeIDArg(appIDArg, "--application-id"); err != nil {
					return err
				}
			}
			if appIDArg == "" || rf.uri != "" {
				r.Record = &gds.ApplicationRecord{}
				if err := rf.apply(cmd, r.Record); err != nil {
					return err
				}
			}
			switch {
			case keyFile != "" && newKey != "":
				return exitCodef(1, "--private-key and --new-key exclude each other")
			case keyFile != "":
				if r.Key, err = readSigner(keyFile); err != nil {
					return exitCode(1, err)
				}
			case newKey != "":
				if r.Key, err = generateKey(newKey); err != nil {
					return exitCodef(1, "invalid --new-key: %v", err)
				}
			}
			return withClientTimeout(waitTime, func(ctx context.Context, c *opcua.Client) error {
				e, err := gds.NewPullClient(c).Enroll(ctx, r)
				if err != nil {
					return pullFailed("enrolling", err)
				}
				var keyPEM []byte
				if keyFile == "" {
					der, err := uapolicy.MarshalPrivateKey(e.PrivateKey)
					if err != nil {
						return exitCode(1, err)
					}
					keyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
				}
				out, err := writeIssued(&files, e.ApplicationID.String(), "", e.Certificate, keyPEM, e.IssuerCertificates)
				if err != nil {
					return exitCode(1, err)
				}
				if e.TrustList != nil {
					if out.TrustList, err = trustListEntries(e.TrustList, trustOut); err != nil {
						return exitCode(1, err)
					}
					out.TrustDir = trustOut
				}
				return printIssued(out, "Enrolled "+e.ApplicationID.String(), e.Certificate)
			})
		},
	}
	rf.register(cmd)
	// --type is the application type of the record here.
	req.register(cmd, "cert-type")
	files.register(cmd)
	cmd.Flags().StringVar(&appIDArg, "application-id", "", "ApplicationId of a registered application (instead of finding it by --uri)")
	cmd.Flags().StringVar(&keyFile, "private-key", "", "Private key to request the certificate for (default: the GDS generates one)")
	cmd.Flags().StringVar(&newKey, "new-key", "", "Generate the private key here: rsa|ecc-p256|ecc-p384|ed25519")
	cmd.Flags().StringVar(&trustOut, "trust-out", "", "Write the trust list into this directory (trusted/certs, trusted/crl, issuer/certs, issuer/crl)")
	cmd.Flags().DurationVar(&poll, "poll", 5*time.Second, "Interval of the FinishRequest calls while the request waits for approval")
	cmd.Flags().DurationVar(&waitTime, "wait-timeout", 10*time.Minute, "How long enrolling may take (0: forever)")
	_ = cmd.RegisterFlagCompletionFunc("new-key", fixedCompletion([]string{"rsa", "ecc-p256", "ecc-p384", "ed25519"}))
	return cmd
}

// generateKey generates a private key of a kind --new-key names.
func generateKey(kind string) (crypto.Signer, error) {
	switch strings.ToLower(kind) {
	case "rsa":
		return rsa.GenerateKey(rand.Reader, 2048)
	case "ecc-p256", "p256":
		return ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	case "ecc-p384", "p384":
		return ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	case "ed25519", "curve25519":
		_, k, err := ed25519.GenerateKey(rand.Reader)
		return k, err
	}
	return nil, fmt.Errorf("unknown key kind %q (rsa|ecc-p256|ecc-p384|ed25519)", kind)
}
