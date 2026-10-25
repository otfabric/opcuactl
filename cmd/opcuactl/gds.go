// SPDX-License-Identifier: MIT

package opcuactl

import (
	"bufio"
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/otfabric/go-opcua"
	"github.com/otfabric/go-opcua/gds"
	"github.com/otfabric/go-opcua/id"
	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/go-opcua/uapolicy"
	"github.com/otfabric/opcuactl/internal/format"
	"github.com/otfabric/opcuactl/internal/model"
	"github.com/otfabric/opcuactl/internal/parse"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newGDSCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "gds",
		Aliases: []string{"certmgmt"},
		Short:   "Manage the certificates of a server (push model, Part 12 §7)",
		Long: `Manage the trust list and the application instance certificate of a server
by the push model of OPC UA Part 12 §7, through its ServerConfiguration
object: read and change the trust list of the DefaultApplicationGroup, ask
the server for a certificate signing request, install the certificate a CA
issued for it, and read the certificates the server rejected.

Servers allow this only to a session with the SecurityAdmin role over a
SignAndEncrypt channel: connect with --security-mode SignAndEncrypt (and
--security-policy, --cert, --key) and an administrator identity (--auth
username|certificate|issued). The application certificate of opcuactl must
be in the server's trust list.

Changes are made in a transaction of the session (Part 12 §7.10.2) that
ApplyChanges completes. Since every opcuactl command has its own session,
the commands that change something call ApplyChanges themselves; --no-apply
leaves the change pending for servers that keep transactions across
sessions (gds apply or gds cancel then completes it).

Examples:
  SEC="--security-mode SignAndEncrypt --security-policy Basic256Sha256 --cert app.pem --key app.key --auth username --username secadmin --password ..."
  opcuactl gds props -e opc.tcp://plc:4840 $SEC
  opcuactl gds trustlist get -e opc.tcp://plc:4840 $SEC --out ./pki
  opcuactl gds trustlist add -e opc.tcp://plc:4840 $SEC client.der
  opcuactl gds csr -e opc.tcp://plc:4840 $SEC --type rsa --regenerate --out plc.csr
  opcuactl gds update-cert -e opc.tcp://plc:4840 $SEC plc.pem --issuer ca.pem
  opcuactl gds rejected -e opc.tcp://plc:4840 $SEC
  opcuactl gds certs -e opc.tcp://plc:4840 $SEC --group https`,
	}
	trustlist := &cobra.Command{
		Use:     "trustlist",
		Aliases: []string{"trust"},
		Short:   "Read and change the trust list of a certificate group",
		Long: `Read and change the trust list of a certificate group: the
DefaultApplicationGroup by default, --group https (the TLS certificates of
opc.https and opc.wss clients), --group usertoken (X.509 user tokens) or the
NodeId of a group the server defines (gds groups lists them).`,
	}
	trustlist.AddCommand(
		newGDSTrustListGetCmd(),
		newGDSTrustListAddCmd(),
		newGDSTrustListRemoveCmd(),
		newGDSTrustListSetCmd(),
	)
	cmd.AddCommand(
		newGDSPropsCmd(),
		trustlist,
		newGDSCSRCmd(),
		newGDSUpdateCertCmd(),
		newGDSRejectedCmd(),
		newGDSTransactionCmd(true),
		newGDSTransactionCmd(false),
		newGDSCertsCmd(),
		newGDSSelfSignedCmd(),
		newGDSDeleteCertCmd(),
		newGDSResetCmd(),
		newGDSTransactionDiagnosticsCmd(),
		newGDSGroupsCmd(),
	)
	return cmd
}

// gdsHint explains why a server refuses a certificate management method.
func gdsHint(err error) string {
	switch {
	case errors.Is(err, ua.StatusBadUserAccessDenied):
		return "certificate management needs the SecurityAdmin role: connect with an administrator identity (--auth username|certificate|issued)"
	case errors.Is(err, ua.StatusBadSecurityModeInsufficient):
		return "certificate management needs an encrypted channel: --security-mode SignAndEncrypt with --security-policy, --cert and --key"
	case errors.Is(err, ua.StatusBadNodeIDUnknown), errors.Is(err, ua.StatusBadMethodInvalid), errors.Is(err, ua.StatusBadNotImplemented):
		return "the server does not implement push certificate management (no ServerConfiguration object)"
	case errors.Is(err, ua.StatusBadTransactionPending):
		return "another session has certificate changes pending; it must apply or cancel them first"
	case errors.Is(err, ua.StatusBadCertificateURIInvalid):
		return "the certificate must carry the ApplicationUri of the server"
	case errors.Is(err, ua.StatusBadCertificateTimeInvalid):
		return "the certificate is not valid now"
	case errors.Is(err, ua.StatusBadSecurityChecksFailed):
		return "the server did not accept the certificate: the private key does not match it or it is not issued by the given issuers"
	case errors.Is(err, ua.StatusBadCertificateInvalid):
		return "the server cannot use the certificate (not a certificate of the requested type)"
	case errors.Is(err, ua.StatusBadNotSupported):
		return "the server does not support this request (key type, PFX key or certificate type)"
	case errors.Is(err, ua.StatusBadEncodingLimitsExceeded):
		return "the trust list exceeds MaxTrustListSize of the server"
	case errors.Is(err, ua.StatusBadInvalidState):
		return "the server refuses this in its current state (no certificate of the type, or one its endpoints still use)"
	}
	return ""
}

func gdsFailed(what string, err error) error {
	msg := fmt.Sprintf("%s: %v", what, err)
	if h := gdsHint(err); h != "" {
		msg += " (" + h + ")"
	}
	format.Errf("%s", msg)
	return exitCode(3, err)
}

// withGDS runs fn with a gds client on a new session.
func withGDS(fn func(ctx context.Context, g *gds.Client) error) error {
	return withClient(func(ctx context.Context, c *opcua.Client) error {
		return fn(ctx, gds.New(c))
	})
}

// groupFlag is the --group flag of the commands that take a certificate
// group.
func groupFlag(cmd *cobra.Command, group *string, what string) {
	cmd.Flags().StringVar(group, "group", "", "Certificate group "+what+": default|https|usertoken or a NodeId (default: the DefaultApplicationGroup)")
	_ = cmd.RegisterFlagCompletionFunc("group", fixedCompletion(certificateGroupValues()))
}

func certificateGroupValues() []string {
	out := make([]string, len(parse.CertificateGroups))
	for i, g := range parse.CertificateGroups {
		out[i] = g.Name
	}
	return out
}

// parseGroup parses --group (exit code 1 when invalid).
func parseGroup(s string) (group, trustList *ua.NodeID, err error) {
	if group, trustList, err = parse.CertificateGroup(s); err != nil {
		return nil, nil, exitCodef(1, "invalid --group: %v", err)
	}
	return group, trustList, nil
}

// groupTrustList finds the TrustList object of a certificate group.
func groupTrustList(ctx context.Context, c *opcua.Client, group *ua.NodeID) (*ua.NodeID, error) {
	refs, err := c.Node(group).References(ctx, id.HasComponent, ua.BrowseDirectionForward, ua.NodeClassObject, true)
	if err != nil {
		return nil, err
	}
	for _, r := range refs {
		if r.NodeID != nil && r.BrowseName != nil && r.BrowseName.Name == "TrustList" {
			return r.NodeID.NodeID, nil
		}
	}
	return nil, fmt.Errorf("%s is not a certificate group (it has no TrustList)", group)
}

// withGDSGroup runs fn with a gds client on the trust list of the group
// --group names.
func withGDSGroup(groupName string, fn func(ctx context.Context, g *gds.Client, group *ua.NodeID) error) error {
	group, tl, err := parseGroup(groupName)
	if err != nil {
		return err
	}
	return withClient(func(ctx context.Context, c *opcua.Client) error {
		if tl == nil {
			if tl, err = groupTrustList(ctx, c, group); err != nil {
				return gdsFailed("certificate group "+group.String(), err)
			}
		}
		return fn(ctx, gds.New(c).WithTrustList(tl), group)
	})
}

// groupSuffix names a certificate group other than the default one in a
// summary.
func groupSuffix(group *ua.NodeID) string {
	name := parse.CertificateGroupName(group)
	if name == "" || name == "default" {
		return ""
	}
	return " of the " + name + " group"
}

// --- files ---

// readCertsAndCRLs reads the certificates and CRLs of a PEM file (any number
// of CERTIFICATE and X509 CRL blocks) or of a DER file (a certificate, a
// chain of certificates or a CRL).
func readCertsAndCRLs(file string) (certs, crls [][]byte, err error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, nil, err
	}
	rest := b
	found := false
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		found = true
		switch block.Type {
		case "CERTIFICATE":
			if _, err := uapolicy.ParseCertificate(block.Bytes); err != nil {
				return nil, nil, fmt.Errorf("%s: %w", file, err)
			}
			certs = append(certs, block.Bytes)
		case "X509 CRL":
			if _, err := x509.ParseRevocationList(block.Bytes); err != nil {
				return nil, nil, fmt.Errorf("%s: %w", file, err)
			}
			crls = append(crls, block.Bytes)
		}
	}
	if found {
		if len(certs)+len(crls) == 0 {
			return nil, nil, fmt.Errorf("%s: not a certificate or CRL (no CERTIFICATE or X509 CRL PEM block)", file)
		}
		return certs, crls, nil
	}
	if cs, err := uapolicy.ParseCertificates(b); err == nil && len(cs) > 0 {
		for _, c := range cs {
			certs = append(certs, c.Raw)
		}
		return certs, nil, nil
	}
	if _, err := x509.ParseRevocationList(b); err == nil {
		return nil, [][]byte{b}, nil
	}
	return nil, nil, fmt.Errorf("%s: not a certificate or CRL (PEM or DER)", file)
}

// readCerts is readCertsAndCRLs for files that must hold certificates.
func readCerts(file string) ([][]byte, error) {
	certs, crls, err := readCertsAndCRLs(file)
	if err != nil {
		return nil, err
	}
	if len(crls) > 0 || len(certs) == 0 {
		return nil, fmt.Errorf("%s: not a certificate", file)
	}
	return certs, nil
}

// The directories of a trust list in the PKI layout of OPC UA applications
// (UA-.NETStandard, open62541): trusted/certs, trusted/crl, issuer/certs,
// issuer/crl.
var pkiDirs = []struct {
	list string
	dir  string
	mask ua.TrustListMasks
}{
	{"trusted", filepath.Join("trusted", "certs"), ua.TrustListMasksTrustedCertificates},
	{"trusted-crl", filepath.Join("trusted", "crl"), ua.TrustListMasksTrustedCrls},
	{"issuer", filepath.Join("issuer", "certs"), ua.TrustListMasksIssuerCertificates},
	{"issuer-crl", filepath.Join("issuer", "crl"), ua.TrustListMasksIssuerCrls},
}

// trustListLists returns the four lists of a trust list in the order of
// pkiDirs.
func trustListLists(tl *ua.TrustListDataType) [4]*[][]byte {
	return [4]*[][]byte{&tl.TrustedCertificates, &tl.TrustedCrls, &tl.IssuerCertificates, &tl.IssuerCrls}
}

// describe returns the summary of a certificate or CRL of a list.
func describe(list string, der []byte) model.GDSCertificate {
	e := model.GDSCertificate{List: list, Kind: "certificate", Thumbprint: gds.Thumbprint(der)}
	if strings.HasSuffix(list, "-crl") {
		e.Kind = "crl"
		crl, err := x509.ParseRevocationList(der)
		if err != nil {
			e.Error = err.Error()
			return e
		}
		e.Issuer = crl.Issuer.String()
		e.NotBefore = crl.ThisUpdate.UTC().Format(time.RFC3339)
		if !crl.NextUpdate.IsZero() {
			e.NotAfter = crl.NextUpdate.UTC().Format(time.RFC3339)
		}
		e.Revoked = len(crl.RevokedCertificateEntries)
		return e
	}
	c, err := uapolicy.ParseCertificate(der)
	if err != nil {
		e.Error = err.Error()
		return e
	}
	e.Subject = c.Subject.String()
	e.Issuer = c.Issuer.String()
	if len(c.URIs) > 0 {
		e.ApplicationURI = c.URIs[0].String()
	}
	e.NotBefore = c.NotBefore.UTC().Format(time.RFC3339)
	e.NotAfter = c.NotAfter.UTC().Format(time.RFC3339)
	e.CA = c.IsCA
	return e
}

// writeEntry writes a certificate (.der) or CRL (.crl) named by its
// thumbprint into dir.
func writeEntry(dir string, e *model.GDSCertificate, der []byte) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	ext := ".der"
	if e.Kind == "crl" {
		ext = ".crl"
	}
	e.File = filepath.Join(dir, e.Thumbprint+ext)
	return os.WriteFile(e.File, der, 0o644) //nolint:gosec // certificates and CRLs are public
}

// writeCertificates prints certificate summaries as a table or structured.
func writeCertificates(entries []model.GDSCertificate) error {
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
	headers := []string{"LIST", "THUMBPRINT", "SUBJECT / ISSUER", "NOT AFTER", "CA"}
	if verbose {
		headers = append(headers, "APPLICATION URI", "FILE")
	}
	rows := make([][]string, len(entries))
	for i, e := range entries {
		name := e.Subject
		if e.Kind == "crl" {
			name = fmt.Sprintf("%s (%d revoked)", e.Issuer, e.Revoked)
		}
		if e.Error != "" {
			name = "invalid: " + e.Error
		}
		ca := ""
		if e.CA {
			ca = "yes"
		}
		rows[i] = []string{e.List, e.Thumbprint, name, e.NotAfter, ca}
		if verbose {
			rows[i] = append(rows[i], e.ApplicationURI, e.File)
		}
	}
	return newWriter().WriteTable(headers, rows)
}

// stdinIsTerminal reports whether a confirmation can be asked.
var stdinIsTerminal = func() bool { return term.IsTerminal(int(os.Stdin.Fd())) }

// confirmed asks a yes/no question on a terminal; without one, or with
// --yes, the answer is yes.
func confirmed(yes bool, in io.Reader, question string) bool {
	if yes || !stdinIsTerminal() {
		return true
	}
	fmt.Fprintf(os.Stderr, "%s [y/N] ", question)
	line, _ := bufio.NewReader(in).ReadString('\n')
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "y") {
		return true
	}
	fmt.Fprintln(os.Stderr, "Cancelled.")
	return false
}

// finishChange completes a change: ApplyChanges when the server needs it
// (unless noApply), then the result.
func finishChange(ctx context.Context, g *gds.Client, res model.GDSResult, noApply bool, summary string) error {
	if res.ApplyChangesRequired && !noApply {
		err := g.ApplyChanges(ctx)
		switch {
		case err == nil:
			res.Applied = true
		case errors.Is(err, ua.StatusBadNothingToDo):
			// open62541 and node-opcua: the change took effect already.
		default:
			return gdsFailed("ApplyChanges", err)
		}
	}
	pending := res.ApplyChangesRequired && !res.Applied && noApply
	if pending {
		format.Errf("the change is pending in the transaction of this session; servers that keep a transaction per session drop it when opcuactl closes the session")
	}
	if format.ParseFormat(outputFormat) != format.Table {
		return newWriter().Write(res)
	}
	switch {
	case res.Applied:
		summary += "; changes applied"
	case pending:
		summary += "; pending until ApplyChanges (opcuactl gds apply)"
	}
	if !quiet {
		fmt.Println(summary)
	}
	return nil
}

// --- props ---

func newGDSPropsCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "props",
		Aliases: []string{"properties", "info"},
		Short:   "Show the properties of the ServerConfiguration object",
		Long: `Show the properties of the ServerConfiguration object (Part 12 §7.10.3):
ServerCapabilities, SupportedPrivateKeyFormats, MaxTrustListSize,
MulticastDnsEnabled, and the CertificateTypes of the DefaultApplicationGroup.
Most servers let everyone read them.

Examples:
  opcuactl gds props -e opc.tcp://localhost:4840
  opcuactl gds props -e opc.tcp://localhost:4840 --format json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return withClient(func(ctx context.Context, c *opcua.Client) error {
				p, err := gds.New(c).Properties(ctx)
				if err != nil {
					return gdsFailed("reading the ServerConfiguration properties", err)
				}
				out := model.GDSProperties{
					ServerCapabilities:         nonNil(p.ServerCapabilities),
					SupportedPrivateKeyFormats: nonNil(p.SupportedPrivateKeyFormats),
					MaxTrustListSize:           p.MaxTrustListSize,
					MulticastDNSEnabled:        p.MulticastDNSEnabled,
					CertificateTypes:           []string{},
				}
				for _, t := range p.CertificateTypes {
					out.CertificateTypes = append(out.CertificateTypes, parse.CertificateTypeName(t))
				}
				if len(out.SupportedPrivateKeyFormats) == 0 && len(out.CertificateTypes) == 0 {
					format.Errf("the server publishes no ServerConfiguration properties: it probably does not implement push certificate management")
				}
				if format.ParseFormat(outputFormat) != format.Table {
					return newWriter().Write(out)
				}
				size := "unlimited"
				if out.MaxTrustListSize > 0 {
					size = fmt.Sprintf("%d bytes", out.MaxTrustListSize)
				}
				writeKV("Server configuration", [][2]string{
					{"Server capabilities", strings.Join(out.ServerCapabilities, ", ")},
					{"Private key formats", strings.Join(out.SupportedPrivateKeyFormats, ", ")},
					{"Max trust list size", size},
					{"Multicast DNS", fmt.Sprint(out.MulticastDNSEnabled)},
					{"Certificate types", strings.Join(out.CertificateTypes, ", ")},
				})
				return nil
			})
		},
	}
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// --- trustlist ---

func newGDSTrustListGetCmd() *cobra.Command {
	var (
		masks []string
		out   string
		group string
	)
	cmd := &cobra.Command{
		Use:   "get",
		Short: "Read the trust list (certificates, issuers, CRLs)",
		Long: `Read the trust list of the DefaultApplicationGroup, or of --group (OpenWithMasks, Read,
Close) and list its entries: the list (trusted, trusted-crl, issuer,
issuer-crl), the thumbprint, the subject (or for a CRL the issuer and the
number of revoked certificates) and the end of the validity. -v adds the
ApplicationUri and the file.

--masks reads only some lists (trusted, trusted-crls, issuers, issuer-crls,
all; repeatable or comma-separated). --out writes the entries as DER files
named by their thumbprint in the PKI layout of OPC UA applications
(trusted/certs, trusted/crl, issuer/certs, issuer/crl), which
gds trustlist set reads back.

Examples:
  opcuactl gds trustlist get -e opc.tcp://localhost:4840 $SEC
  opcuactl gds trustlist get -e opc.tcp://localhost:4840 $SEC --masks issuers,issuer-crls
  opcuactl gds trustlist get -e opc.tcp://localhost:4840 $SEC --group https
  opcuactl gds trustlist get -e opc.tcp://localhost:4840 $SEC --out ./pki --format json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := parse.TrustListMasks(masks)
			if err != nil {
				return exitCodef(1, "invalid --masks: %v", err)
			}
			return withGDSGroup(group, func(ctx context.Context, g *gds.Client, _ *ua.NodeID) error {
				tl, err := g.ReadTrustList(ctx, m)
				if err != nil {
					return gdsFailed("reading the trust list", err)
				}
				var entries []model.GDSCertificate
				for i, l := range trustListLists(tl) {
					for _, der := range *l {
						e := describe(pkiDirs[i].list, der)
						if out != "" {
							if err := writeEntry(filepath.Join(out, pkiDirs[i].dir), &e, der); err != nil {
								return exitCode(1, err)
							}
						}
						entries = append(entries, e)
					}
				}
				return writeCertificates(entries)
			})
		},
	}
	cmd.Flags().StringSliceVar(&masks, "masks", nil, "Lists to read: trusted, trusted-crls, issuers, issuer-crls, all (default all)")
	cmd.Flags().StringVar(&out, "out", "", "Write the entries as DER files into this directory (trusted/certs, trusted/crl, issuer/certs, issuer/crl)")
	_ = cmd.RegisterFlagCompletionFunc("masks", fixedCompletion(trustListMaskValues))
	groupFlag(cmd, &group, "whose trust list to read")
	return cmd
}

func newGDSTrustListAddCmd() *cobra.Command {
	var (
		issuer, noApply bool
		group           string
	)
	cmd := &cobra.Command{
		Use:   "add <cert>...",
		Short: "Add certificates to the trusted (or issuer) certificates",
		Long: `Add certificates (PEM or DER files; a PEM file may hold several) to the
trusted certificates of the trust list, or with --issuer to the issuer
certificates (CA certificates that validate chains without being trusted
themselves). opcuactl then calls ApplyChanges unless --no-apply.

AddCertificate cannot add CRLs: use gds trustlist set for them.

Examples:
  opcuactl gds trustlist add -e opc.tcp://localhost:4840 $SEC client.der
  opcuactl gds trustlist add -e opc.tcp://localhost:4840 $SEC --issuer ca.pem
  opcuactl gds trustlist add -e opc.tcp://localhost:4840 $SEC --group usertoken user-ca.pem --issuer`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var certs [][]byte
			for _, f := range args {
				cs, err := readCerts(f)
				if err != nil {
					return exitCode(1, err)
				}
				certs = append(certs, cs...)
			}
			list := listName(issuer)
			return withGDSGroup(group, func(ctx context.Context, g *gds.Client, grp *ua.NodeID) error {
				list += groupSuffix(grp)
				res := model.GDSResult{Action: "add", ApplyChangesRequired: true}
				for _, der := range certs {
					if err := g.AddCertificate(ctx, der, !issuer); err != nil {
						return gdsFailed("adding "+gds.Thumbprint(der), err)
					}
					res.Thumbprints = append(res.Thumbprints, gds.Thumbprint(der))
				}
				return finishChange(ctx, g, res, noApply, fmt.Sprintf("Added %d certificate(s) to the %s: %s", len(certs), list, strings.Join(res.Thumbprints, ", ")))
			})
		},
	}
	cmd.Flags().BoolVar(&issuer, "issuer", false, "Add to the issuer certificates instead of the trusted certificates")
	cmd.Flags().BoolVar(&noApply, "no-apply", false, "Do not call ApplyChanges")
	groupFlag(cmd, &group, "whose trust list to change")
	return cmd
}

func listName(issuer bool) string {
	if issuer {
		return "issuer certificates"
	}
	return "trusted certificates"
}

// thumbprintArg returns the thumbprint an argument names: a SHA-1
// thumbprint in hexadecimal (colons and spaces allowed), or a certificate
// file.
func thumbprintArg(s string) (string, error) {
	h := strings.ToUpper(strings.NewReplacer(":", "", " ", "").Replace(strings.TrimSpace(s)))
	if len(h) == 40 && strings.Trim(h, "0123456789ABCDEF") == "" {
		return h, nil
	}
	certs, err := readCerts(s)
	if err != nil {
		return "", fmt.Errorf("%q is neither a SHA-1 thumbprint nor a certificate file: %w", s, err)
	}
	return gds.Thumbprint(certs[0]), nil
}

func newGDSTrustListRemoveCmd() *cobra.Command {
	var (
		issuer, noApply bool
		group           string
	)
	cmd := &cobra.Command{
		Use:   "remove <thumbprint|cert>...",
		Short: "Remove certificates from the trusted (or issuer) certificates",
		Long: `Remove certificates from the trusted certificates of the trust list, or
with --issuer from the issuer certificates. A certificate is named by its
SHA-1 thumbprint (as gds trustlist get shows it; case, colons and spaces do
not matter) or by a certificate file. The server also removes the CRLs of a
CA certificate. opcuactl then calls ApplyChanges unless --no-apply.

Examples:
  opcuactl gds trustlist remove -e opc.tcp://localhost:4840 $SEC 3F2A...C01B
  opcuactl gds trustlist remove -e opc.tcp://localhost:4840 $SEC --issuer old-ca.pem`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var thumbs []string
			for _, a := range args {
				t, err := thumbprintArg(a)
				if err != nil {
					return exitCode(1, err)
				}
				thumbs = append(thumbs, t)
			}
			return withGDSGroup(group, func(ctx context.Context, g *gds.Client, grp *ua.NodeID) error {
				list := listName(issuer) + groupSuffix(grp)
				res := model.GDSResult{Action: "remove", ApplyChangesRequired: true, Thumbprints: thumbs}
				for _, t := range thumbs {
					if err := g.RemoveCertificate(ctx, t, !issuer); err != nil {
						msg := "removing " + t
						if errors.Is(err, ua.StatusBadInvalidArgument) {
							msg += " (not in the " + list + ")"
						}
						return gdsFailed(msg, err)
					}
				}
				return finishChange(ctx, g, res, noApply, fmt.Sprintf("Removed %d certificate(s) from the %s: %s", len(thumbs), list, strings.Join(thumbs, ", ")))
			})
		},
	}
	cmd.Flags().BoolVar(&issuer, "issuer", false, "Remove from the issuer certificates instead of the trusted certificates")
	cmd.Flags().BoolVar(&noApply, "no-apply", false, "Do not call ApplyChanges")
	groupFlag(cmd, &group, "whose trust list to change")
	return cmd
}

// readPKIDir reads a directory in the layout of pkiDirs into tl and returns
// the lists it found.
func readPKIDir(dir string, tl *ua.TrustListDataType) (ua.TrustListMasks, error) {
	var found ua.TrustListMasks
	lists := trustListLists(tl)
	for i, d := range pkiDirs {
		files, err := os.ReadDir(filepath.Join(dir, d.dir))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return 0, err
		}
		found |= d.mask
		for _, f := range files {
			if f.IsDir() || strings.HasPrefix(f.Name(), ".") {
				continue
			}
			certs, crls, err := readCertsAndCRLs(filepath.Join(dir, d.dir, f.Name()))
			if err != nil {
				return 0, err
			}
			switch crl := strings.HasSuffix(d.list, "-crl"); {
			case crl && len(certs) > 0:
				return 0, fmt.Errorf("%s: a certificate in %s", filepath.Join(dir, d.dir, f.Name()), d.dir)
			case !crl && len(crls) > 0:
				return 0, fmt.Errorf("%s: a CRL in %s", filepath.Join(dir, d.dir, f.Name()), d.dir)
			}
			*lists[i] = append(*lists[i], certs...)
			*lists[i] = append(*lists[i], crls...)
		}
	}
	if found == 0 {
		return 0, fmt.Errorf("%s: no trusted/certs, trusted/crl, issuer/certs or issuer/crl directory", dir)
	}
	return found, nil
}

func newGDSTrustListSetCmd() *cobra.Command {
	var (
		issuers []string
		masks   []string
		noApply bool
		yes     bool
		group   string
	)
	cmd := &cobra.Command{
		Use:   "set <file|dir>...",
		Short: "Replace the trust list",
		Long: `Write a complete trust list (Open, Write, CloseAndUpdate): the server
replaces the lists --masks names (default all four) with what is given.

Each argument is a certificate or CRL file (PEM or DER): its certificates
become trusted certificates, its CRLs trusted CRLs. --issuer files
(repeatable) give the issuer certificates and their CRLs. A directory in
the layout gds trustlist get --out writes (trusted/certs, trusted/crl,
issuer/certs, issuer/crl) gives all four lists, so a trust list can be
saved, edited and written back.

Lists that --masks names but nothing fills are emptied. An empty trusted
list locks every client out, opcuactl included, unless its certificate is
validated through the issuers. Asks for confirmation on a terminal (--yes
skips it). opcuactl then calls ApplyChanges unless --no-apply.

Examples:
  opcuactl gds trustlist set -e opc.tcp://localhost:4840 $SEC app.der ca.pem --issuer ca-crl.pem
  opcuactl gds trustlist get -e opc.tcp://localhost:4840 $SEC --out ./pki
  opcuactl gds trustlist set -e opc.tcp://localhost:4840 $SEC ./pki --yes
  opcuactl gds trustlist set -e opc.tcp://localhost:4840 $SEC --masks issuers --issuer ca.pem`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && len(issuers) == 0 {
				return exitCodef(1, "nothing to write: give certificate or CRL files, a PKI directory, or --issuer files")
			}
			lists, err := parse.TrustListMasks(masks)
			if err != nil {
				return exitCodef(1, "invalid --masks: %v", err)
			}
			tl := &ua.TrustListDataType{SpecifiedLists: uint32(lists)}
			var given ua.TrustListMasks
			for _, a := range args {
				if st, err := os.Stat(a); err == nil && st.IsDir() {
					found, err := readPKIDir(a, tl)
					if err != nil {
						return exitCode(1, err)
					}
					given |= found
					continue
				}
				certs, crls, err := readCertsAndCRLs(a)
				if err != nil {
					return exitCode(1, err)
				}
				tl.TrustedCertificates = append(tl.TrustedCertificates, certs...)
				tl.TrustedCrls = append(tl.TrustedCrls, crls...)
			}
			for _, f := range issuers {
				certs, crls, err := readCertsAndCRLs(f)
				if err != nil {
					return exitCode(1, err)
				}
				tl.IssuerCertificates = append(tl.IssuerCertificates, certs...)
				tl.IssuerCrls = append(tl.IssuerCrls, crls...)
			}
			for i, l := range trustListLists(tl) {
				if len(*l) > 0 {
					given |= pkiDirs[i].mask
				}
				if lists&pkiDirs[i].mask == 0 {
					if len(*l) > 0 {
						return exitCodef(1, "%s entries given, but --masks does not replace that list", pkiDirs[i].list)
					}
					*l = nil
				}
			}
			counts := fmt.Sprintf("%d trusted, %d trusted CRLs, %d issuers, %d issuer CRLs",
				len(tl.TrustedCertificates), len(tl.TrustedCrls), len(tl.IssuerCertificates), len(tl.IssuerCrls))
			if lists&ua.TrustListMasksTrustedCertificates != 0 && len(tl.TrustedCertificates) == 0 {
				format.Errf("warning: the trusted certificates will be empty")
			}
			if _, _, err := parseGroup(group); err != nil {
				return err
			}
			if !confirmed(yes, os.Stdin, fmt.Sprintf("Replace the trust list of %s (%s)?", connCfg.Endpoint, counts)) {
				return nil
			}
			return withGDSGroup(group, func(ctx context.Context, g *gds.Client, _ *ua.NodeID) error {
				required, err := g.WriteTrustList(ctx, tl)
				if err != nil {
					return gdsFailed("writing the trust list", err)
				}
				res := model.GDSResult{Action: "set", ApplyChangesRequired: required}
				return finishChange(ctx, g, res, noApply, "Wrote the trust list ("+counts+")")
			})
		},
	}
	cmd.Flags().StringArrayVar(&issuers, "issuer", nil, "Issuer certificate or CRL file (repeatable)")
	cmd.Flags().StringSliceVar(&masks, "masks", nil, "Lists to replace: trusted, trusted-crls, issuers, issuer-crls, all (default all)")
	cmd.Flags().BoolVar(&noApply, "no-apply", false, "Do not call ApplyChanges")
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip the confirmation prompt")
	_ = cmd.RegisterFlagCompletionFunc("masks", fixedCompletion(trustListMaskValues))
	groupFlag(cmd, &group, "whose trust list to replace")
	return cmd
}

// --- csr ---

func newGDSCSRCmd() *cobra.Command {
	var (
		certType   string
		regenerate bool
		subject    string
		out        string
		group      string
	)
	cmd := &cobra.Command{
		Use:   "csr",
		Short: "Ask the server for a certificate signing request",
		Long: `Ask the server for a PKCS #10 certificate signing request (CreateSigningRequest)
for its application instance certificate of --type (rsa, rsa-min,
ecc-p256, ecc-p384, ecc-brainpool-p256, ecc-brainpool-p384, ed25519, ed448
or a NodeId; default: the server picks). The request carries the
ApplicationUri and host names of the current certificate.

--regenerate makes the server create a new private key (opcuactl sends 32
random bytes of entropy); without it the request is for the current key.
--subject replaces the subject ("CN=App/O=Company/C=BE"). --group asks
for the certificate of another certificate group (https: the TLS
certificate of opc.https and opc.wss).

The request is printed as PEM, or written to --out (DER when the name ends
in .der). Have a CA issue the certificate and install it with
gds update-cert. The server keeps a regenerated key until then.

Examples:
  opcuactl gds csr -e opc.tcp://localhost:4840 $SEC --type rsa --regenerate --out server.csr
  opcuactl gds csr -e opc.tcp://localhost:4840 $SEC --subject "CN=Line 4 PLC/O=Plant" > server.csr`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			typ, err := parse.CertificateType(certType)
			if err != nil {
				return exitCodef(1, "invalid --type: %v", err)
			}
			grp, _, err := parseGroup(group)
			if err != nil {
				return err
			}
			return withGDS(func(ctx context.Context, g *gds.Client) error {
				der, err := g.CreateSigningRequest(ctx, &gds.SigningRequest{
					CertificateGroup:     grp,
					CertificateType:      typ,
					SubjectName:          subject,
					RegeneratePrivateKey: regenerate,
				})
				if err != nil {
					return gdsFailed("CreateSigningRequest", err)
				}
				res := model.GDSSigningRequest{
					CertificateType: parse.CertificateTypeName(typ),
					PEM:             string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der})),
				}
				if csr, err := x509.ParseCertificateRequest(der); err == nil {
					res.Subject = csr.Subject.String()
					res.KeyAlgorithm = csr.PublicKeyAlgorithm.String()
					res.DNSNames = csr.DNSNames
					for _, u := range csr.URIs {
						res.URIs = append(res.URIs, u.String())
					}
					for _, ip := range csr.IPAddresses {
						res.IPAddresses = append(res.IPAddresses, ip.String())
					}
				}
				if out != "" {
					data := []byte(res.PEM)
					if strings.EqualFold(filepath.Ext(out), ".der") {
						data = der
					}
					if err := os.WriteFile(out, data, 0o644); err != nil { //nolint:gosec // a CSR is public
						return exitCode(1, err)
					}
					res.File = out
				}
				if format.ParseFormat(outputFormat) != format.Table {
					return newWriter().Write(res)
				}
				if out == "" {
					fmt.Print(res.PEM)
					return nil
				}
				if quiet {
					fmt.Println(out)
					return nil
				}
				writeKV("Certificate signing request written to "+out, [][2]string{
					{"Subject", res.Subject},
					{"Key", res.KeyAlgorithm},
					{"URIs", strings.Join(res.URIs, ", ")},
					{"DNS names", strings.Join(res.DNSNames, ", ")},
					{"IP addresses", strings.Join(res.IPAddresses, ", ")},
				})
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&certType, "type", "", "Certificate type (rsa|rsa-min|ecc-p256|ecc-p384|ecc-brainpool-p256|ecc-brainpool-p384|ed25519|ed448 or a NodeId)")
	cmd.Flags().BoolVar(&regenerate, "regenerate", false, "Let the server generate a new private key")
	cmd.Flags().StringVar(&subject, "subject", "", "Subject of the request (CN=App/O=Company); default the current subject")
	cmd.Flags().StringVar(&out, "out", "", "Write the request to this file (PEM; DER for .der)")
	_ = cmd.RegisterFlagCompletionFunc("type", fixedCompletion(certificateTypeValues()))
	groupFlag(cmd, &group, "of the certificate")
	return cmd
}

func certificateTypeValues() []string {
	out := make([]string, len(parse.CertificateTypes))
	for i, t := range parse.CertificateTypes {
		out[i] = t.Name
	}
	return out
}

// --- update-cert ---

// certTypePolicies maps ECC certificate types to the policy whose keys they
// hold, the most specific first.
var certTypePolicies = []struct {
	certType uint32
	policy   string
}{
	{id.EccNistP384ApplicationCertificateType, ua.SecurityPolicyURIECCNistP384},
	{id.EccNistP256ApplicationCertificateType, ua.SecurityPolicyURIECCNistP256},
	{id.EccBrainpoolP384r1ApplicationCertificateType, ua.SecurityPolicyURIECCBrainpoolP384r1},
	{id.EccBrainpoolP256r1ApplicationCertificateType, ua.SecurityPolicyURIECCBrainpoolP256r1},
	{id.EccCurve25519ApplicationCertificateType, ua.SecurityPolicyURIECCCurve25519},
	{id.EccCurve448ApplicationCertificateType, ua.SecurityPolicyURIECCCurve448},
}

// certificateTypeOf returns the application certificate type of a
// certificate's key, nil when it is none of them.
func certificateTypeOf(der []byte) *ua.NodeID {
	pub, err := uapolicy.CertificatePublicKey(der)
	if err != nil {
		return nil
	}
	if k, ok := pub.(*rsa.PublicKey); ok {
		if k.N.BitLen() < 2048 {
			return ua.NewNumericNodeID(0, id.RsaMinApplicationCertificateType)
		}
		return ua.NewNumericNodeID(0, id.RsaSha256ApplicationCertificateType)
	}
	for _, t := range certTypePolicies {
		if uapolicy.CheckPublicKey(t.policy, pub) == nil {
			return ua.NewNumericNodeID(0, t.certType)
		}
	}
	return nil
}

// readPrivateKey reads a private key file (PEM or DER, or a PKCS #12 file
// opened with password) and returns it in keyFormat for UpdateCertificate
// (PEM, PFX, or "" for the format of the file), after checking that it
// belongs to the certificate. A PFX made here is protected with password.
func readPrivateKey(file string, cert []byte, keyFormat, password string) ([]byte, string, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, "", err
	}
	var (
		key   crypto.Signer
		isPFX bool
	)
	if block, _ := pem.Decode(b); block == nil {
		k, _, perr := uapolicy.ParsePFX(b, password)
		switch {
		case perr == nil:
			key, isPFX = k, true
		case errors.Is(perr, uapolicy.ErrPFXPassword):
			return nil, "", fmt.Errorf("%s: %w (check --pfx-password)", file, perr)
		}
	}
	if key == nil {
		if key, err = uapolicy.ParsePrivateKey(b); err != nil {
			return nil, "", fmt.Errorf("%s: %w", file, err)
		}
	}
	if !uapolicy.KeyMatchesCertificate(key, cert) {
		return nil, "", fmt.Errorf("private key %s does not match the certificate", file)
	}
	switch strings.ToUpper(keyFormat) {
	case "":
		if isPFX {
			return b, gds.PrivateKeyFormatPFX, nil
		}
	case gds.PrivateKeyFormatPFX:
		if isPFX {
			return b, gds.PrivateKeyFormatPFX, nil
		}
		pfx, err := uapolicy.EncodePFX(key, [][]byte{cert}, password)
		if err != nil {
			return nil, "", fmt.Errorf("%s: %w", file, err)
		}
		return pfx, gds.PrivateKeyFormatPFX, nil
	case gds.PrivateKeyFormatPEM:
	default:
		return nil, "", fmt.Errorf("unknown --key-format %q (pem|pfx)", keyFormat)
	}
	if block, _ := pem.Decode(b); block != nil {
		return b, gds.PrivateKeyFormatPEM, nil
	}
	pkcs8, err := uapolicy.MarshalPrivateKey(key)
	if err != nil {
		return nil, "", fmt.Errorf("%s: %w", file, err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8}), gds.PrivateKeyFormatPEM, nil
}

func newGDSUpdateCertCmd() *cobra.Command {
	var (
		issuers     []string
		keyFile     string
		keyFormat   string
		pfxPassword string
		certType    string
		group       string
		noApply     bool
		yes         bool
	)
	cmd := &cobra.Command{
		Use:   "update-cert <cert>",
		Short: "Install a new application instance certificate on the server",
		Long: `Install a new application instance certificate (UpdateCertificate) and
apply it (ApplyChanges, unless --no-apply). New SecureChannels then use the
new certificate; channels already open keep the old one.

<cert> is a PEM or DER file. A PEM file may carry the issuer chain after
the certificate; --issuer adds CA certificates (repeatable). The
certificate must carry the ApplicationUri of the server and, with issuers,
be issued by one of them.

For a certificate issued for a request of gds csr the server has the key.
For one made elsewhere, --private-key gives its private key (PEM, DER, or a
PKCS #12 file; --key remains the key of opcuactl's own --cert). It is sent
over the encrypted channel in --key-format: pem, or pfx (PKCS #12). A key
file is converted to PFX protected with --pfx-password, the password the
server expects for PFX keys (Part 12 sends none; servers keep it in their
configuration); a PKCS #12 file is sent as it is and --pfx-password opens it
to check the key. By default the key goes as PEM, a PKCS #12 file as PFX.

--type selects the certificate to replace, by default the type of the
certificate's key; --group the certificate group (https: the TLS
certificate of opc.https and opc.wss).

Clients that pinned the old server certificate must trust the new one or
its CA (--trusted-cert). Asks for confirmation on a terminal (--yes skips
it).

Examples:
  opcuactl gds csr -e opc.tcp://plc:4840 $SEC --regenerate --out plc.csr
  # ... the CA issues plc.pem for plc.csr ...
  opcuactl gds update-cert -e opc.tcp://plc:4840 $SEC plc.pem --issuer ca.pem
  opcuactl gds update-cert -e opc.tcp://plc:4840 $SEC plc.der --private-key plc.key --yes
  opcuactl gds update-cert -e opc.tcp://plc:4840 $SEC plc.der --private-key plc.p12 --pfx-password secret
  opcuactl gds update-cert -e opc.tcp://plc:4840 $SEC https.pem --private-key https.key --group https --type https`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			certs, err := readCerts(args[0])
			if err != nil {
				return exitCode(1, err)
			}
			u := &gds.CertificateUpdate{Certificate: certs[0], IssuerCertificates: certs[1:]}
			for _, f := range issuers {
				cs, err := readCerts(f)
				if err != nil {
					return exitCode(1, err)
				}
				u.IssuerCertificates = append(u.IssuerCertificates, cs...)
			}
			if u.CertificateType, err = parse.CertificateType(certType); err != nil {
				return exitCodef(1, "invalid --type: %v", err)
			}
			if u.CertificateGroup, _, err = parseGroup(group); err != nil {
				return err
			}
			if u.CertificateType == nil {
				u.CertificateType = certificateTypeOf(u.Certificate)
				if parse.CertificateGroupName(u.CertificateGroup) == "https" {
					u.CertificateType = gds.HTTPSCertificateType
				}
			}
			switch {
			case keyFile != "":
				if u.PrivateKey, u.PrivateKeyFormat, err = readPrivateKey(keyFile, u.Certificate, keyFormat, pfxPassword); err != nil {
					return exitCode(1, err)
				}
			case keyFormat != "" || pfxPassword != "":
				return exitCodef(1, "--key-format and --pfx-password need --private-key")
			}
			e := describe("certificate", u.Certificate)
			if !confirmed(yes, os.Stdin, fmt.Sprintf("Replace the %s certificate%s of %s with %s (%s)?", parse.CertificateTypeName(u.CertificateType), groupSuffix(u.CertificateGroup), connCfg.Endpoint, e.Subject, e.Thumbprint)) {
				return nil
			}
			return withGDS(func(ctx context.Context, g *gds.Client) error {
				required, err := g.UpdateCertificate(ctx, u)
				if err != nil {
					return gdsFailed("UpdateCertificate", err)
				}
				res := model.GDSResult{Action: "update-cert", ApplyChangesRequired: required, Thumbprints: []string{e.Thumbprint}}
				return finishChange(ctx, g, res, noApply, fmt.Sprintf("Installed the certificate %s (%s, valid until %s)", e.Thumbprint, e.Subject, e.NotAfter))
			})
		},
	}
	cmd.Flags().StringArrayVar(&issuers, "issuer", nil, "Issuer (CA) certificate file (repeatable)")
	cmd.Flags().StringVar(&keyFile, "private-key", "", "Private key of the certificate, if the server did not make it (PEM, DER or PKCS #12)")
	cmd.Flags().StringVar(&keyFormat, "key-format", "", "Format to send the private key in: pem|pfx (default: pem, pfx for a PKCS #12 file)")
	cmd.Flags().StringVar(&pfxPassword, "pfx-password", "", "Password of the PFX key (the one the server expects; opens a PKCS #12 --private-key)")
	cmd.Flags().StringVar(&certType, "type", "", "Certificate type to replace (default: the type of the certificate's key)")
	cmd.Flags().BoolVar(&noApply, "no-apply", false, "Do not call ApplyChanges")
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip the confirmation prompt")
	_ = cmd.RegisterFlagCompletionFunc("type", fixedCompletion(certificateTypeValues()))
	_ = cmd.RegisterFlagCompletionFunc("key-format", fixedCompletion([]string{"pem", "pfx"}))
	groupFlag(cmd, &group, "of the certificate")
	return cmd
}

// --- rejected ---

func newGDSRejectedCmd() *cobra.Command {
	var out string
	cmd := &cobra.Command{
		Use:   "rejected",
		Short: "List the certificates the server rejected",
		Long: `List the application certificates the server rejected (GetRejectedList):
clients it did not trust. --out writes them as DER files named by their
thumbprint; gds trustlist add then trusts one.

Examples:
  opcuactl gds rejected -e opc.tcp://localhost:4840 $SEC
  opcuactl gds rejected -e opc.tcp://localhost:4840 $SEC --out ./rejected
  opcuactl gds trustlist add -e opc.tcp://localhost:4840 $SEC ./rejected/<thumbprint>.der`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return withGDS(func(ctx context.Context, g *gds.Client) error {
				certs, err := g.GetRejectedList(ctx)
				if err != nil {
					return gdsFailed("GetRejectedList", err)
				}
				var entries []model.GDSCertificate
				for _, der := range certs {
					e := describe("rejected", der)
					if out != "" {
						if err := writeEntry(out, &e, der); err != nil {
							return exitCode(1, err)
						}
					}
					entries = append(entries, e)
				}
				return writeCertificates(entries)
			})
		},
	}
	cmd.Flags().StringVar(&out, "out", "", "Write the rejected certificates as DER files into this directory")
	return cmd
}

// --- apply / cancel ---

func newGDSTransactionCmd(apply bool) *cobra.Command {
	use, method, short := "cancel", "CancelChanges", "Drop the pending certificate changes (CancelChanges)"
	if apply {
		use, method, short = "apply", "ApplyChanges", "Apply the pending certificate changes (ApplyChanges)"
	}
	return &cobra.Command{
		Use:   use,
		Short: short,
		Long: short + `.

The commands that change the trust list or the certificate apply their
change themselves; this is for changes made with --no-apply on servers that
keep a transaction across sessions, or by another tool. Servers that keep
the transaction per session (Part 12 §7.10.2), go-opcua among them, have
dropped such changes when the session that made them closed; there is then
nothing to ` + use + `.

Examples:
  opcuactl gds ` + use + ` -e opc.tcp://localhost:4840 $SEC`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return withGDS(func(ctx context.Context, g *gds.Client) error {
				f := g.CancelChanges
				if apply {
					f = g.ApplyChanges
				}
				status := "done"
				if err := f(ctx); errors.Is(err, ua.StatusBadNothingToDo) {
					status = "nothing to do"
				} else if err != nil {
					return gdsFailed(method, err)
				}
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
			})
		},
	}
}
