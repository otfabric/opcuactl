// SPDX-License-Identifier: MIT

package opcuactl

import (
	"context"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/otfabric/go-opcua"
	"github.com/otfabric/go-opcua/gds"
	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/opcuactl/internal/format"
	"github.com/otfabric/opcuactl/internal/model"
	"github.com/otfabric/opcuactl/internal/parse"
	"github.com/spf13/cobra"
)

// The certificate management methods of Part 12 1.05 §7.10 beyond the
// trust list, the signing request and UpdateCertificate.

// writeGroupCertificates prints the certificates of certificate groups.
func writeGroupCertificates(entries []model.GDSCertificate) error {
	if entries == nil {
		entries = []model.GDSCertificate{}
	}
	if format.ParseFormat(outputFormat) != format.Table {
		return newWriter().Write(entries)
	}
	if quiet {
		for _, e := range entries {
			fmt.Println(e.Thumbprint)
		}
		return nil
	}
	headers := []string{"GROUP", "TYPE", "THUMBPRINT", "SUBJECT", "NOT AFTER"}
	if verbose {
		headers = append(headers, "ISSUER", "APPLICATION URI", "FILE")
	}
	rows := make([][]string, len(entries))
	for i, e := range entries {
		subject := e.Subject
		if e.Error != "" {
			subject = "invalid: " + e.Error
		}
		rows[i] = []string{e.Group, e.CertificateType, e.Thumbprint, subject, e.NotAfter}
		if verbose {
			rows[i] = append(rows[i], e.Issuer, e.ApplicationURI, e.File)
		}
	}
	return newWriter().WriteTable(headers, rows)
}

// --- certs ---

func newGDSCertsCmd() *cobra.Command {
	var group, out string
	cmd := &cobra.Command{
		Use:     "certs",
		Aliases: []string{"certificates", "get-certificates"},
		Short:   "Show the certificates the server uses",
		Long: `Show the application instance certificates of a certificate group
(GetCertificates, Part 12 1.05 §7.10.8): one per certificate type, with its
thumbprint, subject and end of validity. --group selects the group
(default, https, usertoken or a NodeId; default the DefaultApplicationGroup),
--out writes the certificates as DER files named by their thumbprint.

Examples:
  opcuactl gds certs -e opc.tcp://localhost:4840 $SEC
  opcuactl gds certs -e opc.tcp://localhost:4840 $SEC --group https --out ./certs`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			grp, _, err := parseGroup(group)
			if err != nil {
				return err
			}
			return withGDS(func(ctx context.Context, g *gds.Client) error {
				types, certs, err := g.GetCertificates(ctx, grp)
				if err != nil {
					return gdsFailed("GetCertificates", err)
				}
				entries := make([]model.GDSCertificate, 0, len(certs))
				for i, der := range certs {
					e := describe("certificate", der)
					e.Group = parse.CertificateGroupName(grp)
					e.CertificateType = parse.CertificateTypeName(types[i])
					if out != "" {
						if err := writeEntry(out, &e, der); err != nil {
							return exitCode(exitUsage, err)
						}
					}
					entries = append(entries, e)
				}
				return writeGroupCertificates(entries)
			})
		},
	}
	groupFlag(cmd, &group, "whose certificates to show")
	cmd.Flags().StringVar(&out, "out", "", "Write the certificates as DER files into this directory")
	return cmd
}

// --- self-signed ---

func newGDSSelfSignedCmd() *cobra.Command {
	var (
		group, certType, subject, out string
		dns, ips                      []string
		days, keySize                 uint16
		noApply, yes                  bool
	)
	cmd := &cobra.Command{
		Use:     "self-signed",
		Aliases: []string{"create-self-signed"},
		Short:   "Have the server create a new key and a self-signed certificate",
		Long: `Have the server create a new private key and a self-signed certificate
for it (CreateSelfSignedCertificate, Part 12 1.05 §7.10.5), which replaces
its certificate of --type in --group when the change is applied
(ApplyChanges, unless --no-apply). The certificate carries the
ApplicationUri of the server.

--subject sets the subject ("CN=App/O=Company"; default the current one),
--dns and --ip the host names (default those of the current certificate),
--days the validity and --key-size the RSA key size (default: the server
chooses). --out writes the new certificate (PEM; DER for .der).

Clients that pinned the old certificate must trust the new one. Asks for
confirmation on a terminal (--yes skips it).

Examples:
  opcuactl gds self-signed -e opc.tcp://plc:4840 $SEC --type rsa --days 365 --out plc.pem
  opcuactl gds self-signed -e opc.tcp://plc:4840 $SEC --group https --type https --dns plc.local --yes`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			req := &gds.SelfSignedCertificate{SubjectName: subject, DNSNames: dns, IPAddresses: ips, LifetimeInDays: days, KeySizeInBits: keySize}
			var err error
			if req.CertificateGroup, _, err = parseGroup(group); err != nil {
				return err
			}
			if req.CertificateType, err = parse.CertificateType(certType); err != nil {
				return exitCodef(exitUsage, "invalid --type: %v", err)
			}
			if !confirmed(yes, os.Stdin, fmt.Sprintf("Replace the %s certificate%s of %s with a new self-signed one?", typeOrDefault(req.CertificateType), groupSuffix(req.CertificateGroup), connCfg.Endpoint)) {
				return nil
			}
			return withGDS(func(ctx context.Context, g *gds.Client) error {
				der, err := g.CreateSelfSignedCertificate(ctx, req)
				if err != nil {
					return gdsFailed("CreateSelfSignedCertificate", err)
				}
				e := describe("certificate", der)
				if out != "" {
					data := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
					if strings.EqualFold(filepath.Ext(out), ".der") {
						data = der
					}
					if err := os.WriteFile(out, data, 0o644); err != nil { //nolint:gosec // a certificate is public
						return exitCode(exitUsage, err)
					}
				}
				res := model.GDSResult{Action: "self-signed", ApplyChangesRequired: true, Thumbprints: []string{e.Thumbprint}}
				return finishChange(ctx, g, res, noApply, fmt.Sprintf("Created the self-signed certificate %s (%s, valid until %s)", e.Thumbprint, e.Subject, e.NotAfter))
			})
		},
	}
	groupFlag(cmd, &group, "of the certificate")
	cmd.Flags().StringVar(&certType, "type", "", "Certificate type (rsa|rsa-min|ecc-p256|...|https or a NodeId)")
	cmd.Flags().StringVar(&subject, "subject", "", "Subject (CN=App/O=Company); default the current subject")
	cmd.Flags().StringArrayVar(&dns, "dns", nil, "DNS name of the certificate (repeatable)")
	cmd.Flags().StringArrayVar(&ips, "ip", nil, "IP address of the certificate (repeatable)")
	cmd.Flags().Uint16Var(&days, "days", 0, "Validity in days (0: the server's default)")
	cmd.Flags().Uint16Var(&keySize, "key-size", 0, "Key size in bits (0: the server's default)")
	cmd.Flags().StringVar(&out, "out", "", "Write the new certificate to this file (PEM; DER for .der)")
	cmd.Flags().BoolVar(&noApply, "no-apply", false, "Do not call ApplyChanges")
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip the confirmation prompt")
	_ = cmd.RegisterFlagCompletionFunc("type", fixedCompletion(certificateTypeValues()))
	return cmd
}

func typeOrDefault(t *ua.NodeID) string {
	if t == nil {
		return "default"
	}
	return parse.CertificateTypeName(t)
}

// --- delete-cert ---

func newGDSDeleteCertCmd() *cobra.Command {
	var (
		group, certType string
		noApply, yes    bool
	)
	cmd := &cobra.Command{
		Use:     "delete-cert",
		Aliases: []string{"delete-certificate"},
		Short:   "Delete a certificate of the server",
		Long: `Delete the certificate of --type in --group (DeleteCertificate, Part 12
1.05 §7.10.9) when the change is applied (ApplyChanges, unless --no-apply).
A server refuses to delete the certificate its endpoints use, and the HTTPS
certificate while it has opc.https or opc.wss endpoints. Asks for
confirmation on a terminal (--yes skips it).

Examples:
  opcuactl gds delete-cert -e opc.tcp://plc:4840 $SEC --type ecc-p256
  opcuactl gds delete-cert -e opc.tcp://plc:4840 $SEC --group "ns=2;s=MqttGroup" --type rsa --yes`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			grp, _, err := parseGroup(group)
			if err != nil {
				return err
			}
			typ, err := parse.CertificateType(certType)
			if err != nil {
				return exitCodef(exitUsage, "invalid --type: %v", err)
			}
			if typ == nil {
				return exitCodef(exitUsage, "--type is required")
			}
			if !confirmed(yes, os.Stdin, fmt.Sprintf("Delete the %s certificate%s of %s?", parse.CertificateTypeName(typ), groupSuffix(grp), connCfg.Endpoint)) {
				return nil
			}
			return withGDS(func(ctx context.Context, g *gds.Client) error {
				if err := g.DeleteCertificate(ctx, grp, typ); err != nil {
					return gdsFailed("DeleteCertificate", err)
				}
				res := model.GDSResult{Action: "delete-cert", ApplyChangesRequired: true}
				return finishChange(ctx, g, res, noApply, fmt.Sprintf("Deleted the %s certificate%s", parse.CertificateTypeName(typ), groupSuffix(grp)))
			})
		},
	}
	groupFlag(cmd, &group, "of the certificate")
	cmd.Flags().StringVar(&certType, "type", "", "Certificate type to delete (required)")
	cmd.Flags().BoolVar(&noApply, "no-apply", false, "Do not call ApplyChanges")
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip the confirmation prompt")
	_ = cmd.RegisterFlagCompletionFunc("type", fixedCompletion(certificateTypeValues()))
	return cmd
}

// --- reset ---

func writeMethodStatus(method, status string) error {
	res := struct {
		Method string `json:"method"`
		Status string `json:"status"`
	}{method, status}
	if format.ParseFormat(outputFormat) != format.Table {
		return newWriter().Write(res)
	}
	if !quiet {
		fmt.Printf("%s: %s\n", method, status)
	}
	return nil
}

func newGDSResetCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:     "reset",
		Aliases: []string{"reset-to-defaults"},
		Short:   "Reset the trust lists and certificates to the server defaults",
		Long: `Reset the trust lists and certificates of every certificate group to the
default configuration of the server (ResetToServerDefaults, Part 12 1.05
§7.10.13). Certificates and trust list entries added since are dropped, so
opcuactl itself may no longer be trusted afterwards. A server refuses the
reset while another session has changes pending. Asks for confirmation on a
terminal (--yes skips it).

Examples:
  opcuactl gds reset -e opc.tcp://plc:4840 $SEC --yes`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !confirmed(yes, os.Stdin, fmt.Sprintf("Reset the trust lists and certificates of %s to the server defaults?", connCfg.Endpoint)) {
				return nil
			}
			return withGDS(func(ctx context.Context, g *gds.Client) error {
				if err := g.ResetToServerDefaults(ctx); err != nil {
					return gdsFailed("ResetToServerDefaults", err)
				}
				return writeMethodStatus("ResetToServerDefaults", "done")
			})
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip the confirmation prompt")
	return cmd
}

// --- transaction ---

func trustListName(n *ua.NodeID) string {
	if n != nil && n.Namespace() == 0 && n.Type() <= ua.NodeIDTypeNumeric {
		for _, g := range parse.CertificateGroups {
			if g.TrustList == n.IntID() {
				return g.Name + " trust list"
			}
		}
	}
	return n.String()
}

func transactionModel(d *gds.TransactionDiagnostics) model.GDSTransaction {
	out := model.GDSTransaction{
		Result:                    d.Result.Symbol(),
		AffectedTrustLists:        []string{},
		AffectedCertificateGroups: []string{},
		Errors:                    []model.GDSTransactionError{},
	}
	if !d.StartTime.IsZero() {
		out.StartTime = model.FormatTimestamp(d.StartTime)
	}
	if !d.EndTime.IsZero() {
		out.EndTime = model.FormatTimestamp(d.EndTime)
	}
	for _, n := range d.AffectedTrustLists {
		out.AffectedTrustLists = append(out.AffectedTrustLists, trustListName(n))
	}
	for _, n := range d.AffectedCertificateGroups {
		out.AffectedCertificateGroups = append(out.AffectedCertificateGroups, parse.CertificateGroupName(n))
	}
	for _, e := range d.Errors {
		te := model.GDSTransactionError{Target: e.TargetID.String(), Error: e.Error.Symbol()}
		if e.Message != nil {
			te.Message = e.Message.Text
		}
		out.Errors = append(out.Errors, te)
	}
	return out
}

func newGDSTransactionDiagnosticsCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "transaction",
		Aliases: []string{"transaction-diagnostics", "tx"},
		Short:   "Show the diagnostics of the last certificate transaction",
		Long: `Show the TransactionDiagnostics of the server (Part 12 1.05 §7.10.11): the
start and end of the last certificate management transaction (or of the one
in progress), its result, the trust lists and certificate groups it
changed, and the errors ApplyChanges recorded. Servers show them only to
SecurityAdmin.

Examples:
  opcuactl gds transaction -e opc.tcp://plc:4840 $SEC
  opcuactl gds transaction -e opc.tcp://plc:4840 $SEC --format json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return withGDS(func(ctx context.Context, g *gds.Client) error {
				d, err := g.TransactionDiagnostics(ctx)
				if err != nil {
					return gdsFailed("reading the TransactionDiagnostics", err)
				}
				out := transactionModel(d)
				if format.ParseFormat(outputFormat) != format.Table {
					return newWriter().Write(out)
				}
				if quiet {
					fmt.Println(out.Result)
					return nil
				}
				writeKV("Last certificate transaction", [][2]string{
					{"Start time", out.StartTime},
					{"End time", out.EndTime},
					{"Result", out.Result},
					{"Trust lists", strings.Join(out.AffectedTrustLists, ", ")},
					{"Certificate groups", strings.Join(out.AffectedCertificateGroups, ", ")},
				})
				if len(out.Errors) == 0 {
					return nil
				}
				fmt.Println()
				rows := make([][]string, len(out.Errors))
				for i, e := range out.Errors {
					rows[i] = []string{e.Target, e.Error, e.Message}
				}
				return newWriter().WriteTable([]string{"TARGET", "ERROR", "MESSAGE"}, rows)
			})
		},
	}
}

// --- groups ---

// certificateGroups browses the CertificateGroups folder of
// ServerConfiguration: the default groups and those of the application,
// with their trust lists and certificate types.
func certificateGroups(ctx context.Context, c *opcua.Client) ([]model.GDSCertificateGroup, error) {
	groups, err := gds.New(c).CertificateGroups(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]model.GDSCertificateGroup, 0, len(groups))
	for _, cg := range groups {
		g := model.GDSCertificateGroup{Name: cg.Name, NodeID: cg.Group.String(), CertificateTypes: []string{}}
		if cg.TrustList != nil {
			g.TrustList = cg.TrustList.String()
		}
		if n, err := c.Node(cg.Group).TranslateBrowsePathInNamespaceToNodeID(ctx, 0, "CertificateTypes"); err == nil {
			if v, err := c.Node(n).Value(ctx); err == nil && v != nil {
				if types, ok := v.Value().([]*ua.NodeID); ok {
					for _, t := range types {
						g.CertificateTypes = append(g.CertificateTypes, parse.CertificateTypeName(t))
					}
				}
			}
		}
		out = append(out, g)
	}
	return out, nil
}

func newGDSGroupsCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "groups",
		Aliases: []string{"certificate-groups"},
		Short:   "List the certificate groups of the server",
		Long: `List the certificate groups of ServerConfiguration (Part 12 §7.8.3.3): the
DefaultApplicationGroup, DefaultHttpsGroup and DefaultUserTokenGroup, and the
groups the application defines, with the NodeId of each group (for
--group), its trust list and its certificate types.

Examples:
  opcuactl gds groups -e opc.tcp://localhost:4840 $SEC
  opcuactl gds groups -e opc.tcp://localhost:4840 $SEC --format json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return withClient(func(ctx context.Context, c *opcua.Client) error {
				groups, err := certificateGroups(ctx, c)
				if err != nil {
					return gdsFailed("browsing the certificate groups", err)
				}
				if format.ParseFormat(outputFormat) != format.Table {
					return newWriter().Write(groups)
				}
				if quiet {
					for _, g := range groups {
						fmt.Println(g.NodeID)
					}
					return nil
				}
				rows := make([][]string, len(groups))
				for i, g := range groups {
					rows[i] = []string{g.Name, g.NodeID, g.TrustList, strings.Join(g.CertificateTypes, ", ")}
				}
				return newWriter().WriteTable([]string{"NAME", "GROUP ID", "TRUST LIST", "CERTIFICATE TYPES"}, rows)
			})
		},
	}
}
