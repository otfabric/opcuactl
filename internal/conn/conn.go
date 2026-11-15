// SPDX-License-Identifier: MIT

package conn

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/x509"
	"encoding/asn1"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/otfabric/go-opcua"
	uaerrors "github.com/otfabric/go-opcua/errors"
	"github.com/otfabric/go-opcua/gds"
	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/go-opcua/uapolicy"
)

// Config holds the connection parameters extracted from CLI flags.
type Config struct {
	Endpoint         string
	SecurityPolicy   string
	SecurityMode     string
	Auth             string
	Username         string
	Password         string
	AuthPolicyID     string
	Token            string
	TokenFile        string
	CertFile         string
	KeyFile          string
	ServerCertFile   string
	TrustedCertFiles []string
	// InsecureNoSecurity allows a channel without message security
	// (--insecure-no-security). Discovery and TLS transports do not need it.
	InsecureNoSecurity bool
	InsecureSkipVerify bool
	Timeout            time.Duration

	// ReverseListen makes the client wait on this address ("host:port" or
	// opc.tcp URL) for the server to connect (OPC UA reverse connect).
	ReverseListen string
	// ReverseServerURI restricts reverse connections to the server that
	// announces this ApplicationUri.
	ReverseServerURI string

	// TLS of opc.wss and opc.https endpoints. TLSCAFile holds the PEM roots
	// the TLS certificate of the server is verified against (the system
	// roots without it); TLSCertFile and TLSKeyFile the client TLS
	// certificate for mutual TLS. The application instance certificate
	// (CertFile, KeyFile) is separate.
	TLSCAFile             string
	TLSCertFile           string
	TLSKeyFile            string
	TLSInsecureSkipVerify bool

	// TransportProfile is the transport profile URI of --transport, "" for
	// the one of the endpoint URL. ua.TransportProfileWSSJSON and
	// ua.TransportProfileHTTPSJSON select the UA JSON mapping of opc.wss
	// and opc.https (Part 6 §7.5.2, §7.4.5); the URL scheme alone means UA
	// Binary.
	TransportProfile string
	// JSONContentType is the Content-Type of https-uajson requests, "" for
	// the library default (application/json).
	JSONContentType string

	// ReturnDiagnostics is the ReturnDiagnostics mask of every request
	// (Part 4 §7.33); 0 asks for none.
	ReturnDiagnostics uint32

	// remoteCert is the leaf certificate of the server that NewClient
	// expects (from the selected endpoint or --server-cert), for the hint
	// when it is not trusted; nil when not known.
	remoteCert []byte
}

// Reverse reports whether the server connects to the client (reverse connect).
func (cfg *Config) Reverse() bool { return cfg.ReverseListen != "" }

// RequireEndpoint returns an error unless an endpoint URL is configured.
// With reverse connect the endpoint is optional: the client then uses the
// EndpointUrl the server announces.
func (cfg *Config) RequireEndpoint() error {
	if cfg.Endpoint == "" && !cfg.Reverse() {
		return fmt.Errorf("--endpoint is required (or set OPCUACTL_ENDPOINT)")
	}
	return nil
}

// DiscoveryOptions returns the options for the session-less discovery calls
// (GetEndpoints, FindServers, FindServersOnNetwork): how to reach the server
// and how long to wait for it.
func (cfg *Config) DiscoveryOptions() []opcua.Option {
	opts := cfg.transportOptions()
	if cfg.HasTLS() {
		opts = append(opts, cfg.tlsOption())
	}
	if cfg.ReturnDiagnostics != 0 {
		opts = append(opts, opcua.ReturnDiagnostics(cfg.ReturnDiagnostics))
	}
	if cfg.Reverse() {
		var ropts []opcua.ReverseConnectOption
		if cfg.ReverseServerURI != "" {
			ropts = append(ropts, opcua.ReverseServerURI(cfg.ReverseServerURI))
		}
		opts = append(opts, opcua.ReverseConnect(cfg.ReverseListen, ropts...))
	}
	if cfg.Timeout > 0 {
		opts = append(opts, opcua.DialTimeout(cfg.Timeout), opcua.RequestTimeout(cfg.Timeout))
	}
	return opts
}

// SelectEndpoint picks the most secure endpoint matching policy and mode
// (either may be empty / invalid to match anything). When a client
// certificate is configured, only endpoints usable with its key type are
// considered, so an RSA certificate is not paired with an ECC endpoint.
// Only endpoints of the transport of the client are considered (Transport:
// --transport, or else the transport of the endpoint URL): an opc.wss URL
// selects among the opc.wss endpoints, --transport wss-uajson among the
// wss-uajson ones.
func (cfg *Config) SelectEndpoint(endpoints []*ua.EndpointDescription, policy string, mode ua.MessageSecurityMode) (*ua.EndpointDescription, error) {
	if t := cfg.Transport(); t != "" && len(endpoints) > 0 {
		endpoints = EndpointsForTransport(endpoints, t)
		if len(endpoints) == 0 {
			return nil, fmt.Errorf("the server offers no %s endpoint", TransportName(t))
		}
	}
	if cfg.CertFile == "" {
		return ua.SelectEndpoint(endpoints, policy, mode)
	}
	certs, err := loadCertificatesFile(cfg.CertFile)
	if err != nil {
		return nil, fmt.Errorf("loading --cert: %w", err)
	}
	return opcua.SelectEndpoint(endpoints, policy, mode, certs[0].Raw)
}

// NewClient creates a connected OPC UA client from the config; extra options
// apply after those of the config.
func (cfg *Config) NewClient(ctx context.Context, extra ...opcua.Option) (*opcua.Client, error) {
	if err := cfg.RequireEndpoint(); err != nil {
		return nil, err
	}

	opts, err := cfg.buildOptions(ctx)
	if err != nil {
		return nil, err
	}

	c, err := opcua.NewClient(cfg.Endpoint, append(opts, extra...)...)
	if errors.Is(err, uaerrors.ErrInsecureNotAllowed) {
		return nil, fmt.Errorf("%w: set --security-policy (auto, or a policy) with --trusted-cert, or accept this with --insecure-no-security", err)
	}
	if err != nil {
		return nil, fmt.Errorf("creating client: %w", err)
	}

	connectCtx := ctx
	if cfg.Timeout > 0 {
		var cancel context.CancelFunc
		connectCtx, cancel = context.WithTimeout(ctx, cfg.Timeout)
		defer cancel()
	}

	if err := c.Connect(connectCtx); err != nil {
		// Release what a failed attempt holds, the reverse listener in particular.
		_ = c.Close(context.WithoutCancel(ctx))
		if u := cfg.untrusted(err); u != nil {
			err = u
		} else if d := endpointsDiffer(err); d != nil {
			err = d
		}
		if cfg.Reverse() {
			return nil, fmt.Errorf("waiting for reverse connection on %s: %w", cfg.ReverseListen, err)
		}
		return nil, fmt.Errorf("connecting to %s: %w", cfg.Endpoint, err)
	}

	return c, nil
}

func (cfg *Config) buildOptions(ctx context.Context) ([]opcua.Option, error) {
	var opts []opcua.Option

	authType, authOpts, err := cfg.authOptions()
	if err != nil {
		return nil, err
	}
	opts = append(opts, authOpts...)

	if cfg.AuthPolicyID != "" {
		opts = append(opts, opcua.AuthPolicyID(cfg.AuthPolicyID))
	}

	secOpts, err := cfg.securityOptions(ctx, authType)
	if err != nil {
		return nil, err
	}
	opts = append(opts, secOpts...)

	channelOpts, err := cfg.channelCertOptions()
	if err != nil {
		return nil, err
	}
	opts = append(opts, channelOpts...)

	if cfg.ServerCertFile != "" {
		opts = append(opts, opcua.RemoteCertificateFile(cfg.ServerCertFile))
		if certs, err := loadCertificatesFile(cfg.ServerCertFile); err == nil {
			cfg.remoteCert = certs[0].Raw
		}
	}

	if len(cfg.TrustedCertFiles) > 0 {
		trusted, err := loadTrustedCertificates(cfg.TrustedCertFiles)
		if err != nil {
			return nil, err
		}
		opts = append(opts, opcua.TrustedCertificates(trusted...))
	}

	if cfg.InsecureSkipVerify {
		opts = append(opts, opcua.InsecureSkipVerify())
	}

	opts = append(opts, cfg.DiscoveryOptions()...)

	return opts, nil
}

func (cfg *Config) authOptions() (ua.UserTokenType, []opcua.Option, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Auth)) {
	case "", "anonymous":
		return ua.UserTokenTypeAnonymous, []opcua.Option{opcua.AuthAnonymous()}, nil

	case "username", "user", "password":
		if cfg.Username == "" {
			return 0, nil, fmt.Errorf("--username is required for --auth username")
		}
		return ua.UserTokenTypeUserName, []opcua.Option{opcua.AuthUsername(cfg.Username, cfg.Password)}, nil

	case "certificate", "x509", "cert":
		if cfg.CertFile == "" || cfg.KeyFile == "" {
			return 0, nil, fmt.Errorf("--cert and --key are required for --auth certificate")
		}
		cert, key, err := loadCertKey(cfg.CertFile, cfg.KeyFile)
		if err != nil {
			return 0, nil, fmt.Errorf("loading auth certificate: %w", err)
		}
		keyOpt := opcua.AuthPrivateKeySigner(key)
		if rsaKey, ok := key.(*rsa.PrivateKey); ok {
			keyOpt = opcua.AuthPrivateKey(rsaKey)
		}
		return ua.UserTokenTypeCertificate, []opcua.Option{opcua.AuthCertificate(cert), keyOpt}, nil

	case "issued", "issuedtoken", "token":
		token, err := cfg.loadIssuedToken()
		if err != nil {
			return 0, nil, err
		}
		return ua.UserTokenTypeIssuedToken, []opcua.Option{opcua.AuthIssuedToken(token)}, nil

	default:
		return 0, nil, fmt.Errorf("unknown --auth %q (anonymous|username|certificate|issued)", cfg.Auth)
	}
}

func (cfg *Config) loadIssuedToken() ([]byte, error) {
	switch {
	case cfg.TokenFile != "":
		b, err := os.ReadFile(cfg.TokenFile)
		if err != nil {
			return nil, fmt.Errorf("reading --token-file: %w", err)
		}
		return bytesTrimSpace(b), nil
	case cfg.Token != "":
		return decodeToken(cfg.Token), nil
	default:
		return nil, fmt.Errorf("--token or --token-file is required for --auth issued")
	}
}

func (cfg *Config) securityOptions(ctx context.Context, authType ua.UserTokenType) ([]opcua.Option, error) {
	policy := strings.TrimSpace(cfg.SecurityPolicy)
	mode := strings.TrimSpace(cfg.SecurityMode)
	autoPolicy := strings.EqualFold(policy, "auto")
	autoMode := strings.EqualFold(mode, "auto")

	// A secured channel needs the server certificate. Unless it was given
	// with --server-cert it is taken from the matching endpoint of the server.
	secured := policy != "" && !strings.EqualFold(policy, "None") && !autoPolicy
	fromEndpoint := autoPolicy || autoMode || (secured && cfg.ServerCertFile == "")

	if fromEndpoint {
		if err := cfg.RequireEndpoint(); err != nil {
			return nil, err
		}
		endpoints, err := opcua.GetEndpoints(ctx, cfg.Endpoint, cfg.DiscoveryOptions()...)
		if err != nil {
			return nil, fmt.Errorf("discovering endpoints: %w", err)
		}

		var secPolicy string
		var secMode ua.MessageSecurityMode
		if !autoPolicy {
			secPolicy = ua.FormatSecurityPolicyURI(policy)
		}
		if !autoMode {
			secMode = ua.MessageSecurityModeFromString(mode)
			if secMode == ua.MessageSecurityModeInvalid {
				return nil, fmt.Errorf("invalid --security-mode %q", mode)
			}
		}

		ep, err := cfg.SelectEndpoint(endpoints, secPolicy, secMode)
		if err != nil {
			return nil, fmt.Errorf("selecting endpoint: %w", err)
		}
		if len(ep.ServerCertificate) > 0 {
			cfg.remoteCert = LeafCertificate(ep.ServerCertificate)
		}
		// The GetEndpoints answer comes without security. CreateSession
		// returns the endpoints again over the secure channel; the client
		// compares the whole answer, not only the selected endpoint, with
		// them and closes the session before ActivateSession when they
		// differ (Part 4 §5.6.2).
		opts := []opcua.Option{opcua.SecurityFromEndpoint(ep, authType), opcua.DiscoveredEndpoints(endpoints)}
		if cfg.InsecureNoSecurity && ep.SecurityPolicyURI == ua.SecurityPolicyURINone {
			opts = append(opts, opcua.InsecureNoSecurity())
		}
		return opts, nil
	}

	if cfg.InsecureNoSecurity && (policy == "" || strings.EqualFold(policy, "None")) {
		return []opcua.Option{opcua.InsecureNoSecurity()}, nil
	}

	var opts []opcua.Option
	if policy != "" && !strings.EqualFold(policy, "None") {
		opts = append(opts, opcua.SecurityPolicy(policy))
	}
	if mode != "" {
		msgMode := ua.MessageSecurityModeFromString(mode)
		if msgMode != ua.MessageSecurityModeInvalid {
			opts = append(opts, opcua.SecurityMode(msgMode))
		}
	}
	return opts, nil
}

// UntrustedCertificateError is a server certificate that the trust list
// (--trusted-cert) does not cover. Its message says how to trust it; Unwrap
// returns go-opcua's error.
type UntrustedCertificateError struct {
	// Thumbprint is the SHA-1 thumbprint of the server certificate (upper
	// case hex, as discover, serve and the GDS commands print it), "" when
	// it is not known.
	Thumbprint string
	// NoTrustList is true when no --trusted-cert was given.
	NoTrustList bool
	err         error
}

func (e *UntrustedCertificateError) Error() string {
	var b strings.Builder
	b.WriteString("server certificate not trusted: ")
	if e.NoTrustList {
		b.WriteString("no --trusted-cert is given")
	} else {
		b.WriteString("it is not one of the --trusted-cert certificates, and none of them issued it")
	}
	b.WriteString("; pass the server certificate")
	if e.Thumbprint != "" {
		b.WriteString(" (SHA-1 thumbprint " + e.Thumbprint + ")")
	}
	b.WriteString(", or the CA certificate that issued it, with --trusted-cert (opcuactl discover endpoints -v shows the thumbprint of each endpoint)")
	return b.String()
}

func (e *UntrustedCertificateError) Unwrap() error { return e.err }

// untrusted returns err as an UntrustedCertificateError when it says that
// the trust list does not cover the server certificate, nil otherwise.
func (cfg *Config) untrusted(err error) error {
	var unknown x509.UnknownAuthorityError
	switch {
	case errors.Is(err, uaerrors.ErrInvalidCertificate) && len(cfg.TrustedCertFiles) == 0:
		return &UntrustedCertificateError{Thumbprint: Thumbprint(cfg.remoteCert), NoTrustList: true, err: err}
	case errors.As(err, &unknown):
		return &UntrustedCertificateError{Thumbprint: Thumbprint(cfg.remoteCert), err: err}
	}
	return nil
}

// EndpointsDifferError is a server whose CreateSession endpoints differ from
// the GetEndpoints answer the client selected its endpoint from (Part 4
// §5.6.2): the client closed the session before ActivateSession. Its message
// says what to check; Unwrap returns go-opcua's error, which matches
// ua.StatusBadSecurityChecksFailed.
type EndpointsDifferError struct{ err error }

func (e *EndpointsDifferError) Error() string {
	return "the endpoints of GetEndpoints and CreateSession differ (BadSecurityChecksFailed), no session was activated; " +
		"check the URL of --endpoint (letter case, trailing slash) or give --security-policy, --security-mode and --server-cert: " +
		e.err.Error()
}

func (e *EndpointsDifferError) Unwrap() error { return e.err }

// endpointsDiffer returns err as an EndpointsDifferError when it is
// go-opcua's comparison of the endpoints of CreateSession with the
// discovered ones, nil otherwise. BadSecurityChecksFailed alone does not
// tell: servers also refuse a client certificate with it. go-opcua names the
// comparison in its message ("the server endpoints of CreateSession ..."),
// in every one of its three cases.
func endpointsDiffer(err error) error {
	if errors.Is(err, ua.StatusBadSecurityChecksFailed) && strings.Contains(err.Error(), "server endpoints of CreateSession") {
		return &EndpointsDifferError{err: err}
	}
	return nil
}

// LeafCertificate returns the first DER certificate of a ServerCertificate,
// which may hold the chain of the server after its own certificate (Part 4
// §7.2). It returns der as it is when that is not DER.
func LeafCertificate(der []byte) []byte {
	var raw asn1.RawValue
	rest, err := asn1.Unmarshal(der, &raw)
	if err != nil {
		return der
	}
	return der[:len(der)-len(rest)]
}

// Thumbprint returns the SHA-1 thumbprint of a DER certificate in upper-case
// hex (Part 6 §6.2.3), as gds.Thumbprint; "" for none.
func Thumbprint(der []byte) string {
	if len(der) == 0 {
		return ""
	}
	return gds.Thumbprint(der)
}

func (cfg *Config) channelCertOptions() ([]opcua.Option, error) {
	if cfg.CertFile == "" && cfg.KeyFile == "" {
		return nil, nil
	}
	if cfg.CertFile == "" || cfg.KeyFile == "" {
		return nil, fmt.Errorf("--cert and --key must be provided together")
	}
	// CertificateFile/PrivateKeyFile load on ApplyConfig; keep file options so
	// ApplicationURI is derived from the cert the same way as go-opcua examples.
	return []opcua.Option{
		opcua.CertificateFile(cfg.CertFile),
		opcua.PrivateKeyFile(cfg.KeyFile),
	}, nil
}

// loadCertKey loads a certificate with its RSA, ECDSA (NIST or Brainpool),
// Ed25519 or Ed448 private key. crypto/tls cannot load Brainpool and Ed448
// keys, so both files go through uapolicy.
func loadCertKey(certFile, keyFile string) ([]byte, crypto.Signer, error) {
	certs, err := loadCertificatesFile(certFile)
	if err != nil {
		return nil, nil, err
	}
	b, err := os.ReadFile(keyFile)
	if err != nil {
		return nil, nil, err
	}
	key, err := uapolicy.ParsePrivateKey(b)
	if err != nil {
		return nil, nil, err
	}
	leaf := certs[0].Raw
	if !uapolicy.KeyMatchesCertificate(key, leaf) {
		return nil, nil, fmt.Errorf("private key %q does not match certificate %q", keyFile, certFile)
	}
	return leaf, key, nil
}

func loadTrustedCertificates(files []string) ([]*x509.Certificate, error) {
	var out []*x509.Certificate
	for _, f := range files {
		certs, err := loadCertificatesFile(f)
		if err != nil {
			return nil, fmt.Errorf("trusted cert %q: %w", f, err)
		}
		out = append(out, certs...)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no certificates found in --trusted-cert files")
	}
	return out, nil
}

func loadCertificatesFile(filename string) ([]*x509.Certificate, error) {
	b, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	var certs []*x509.Certificate
	rest := b
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		c, err := uapolicy.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, err
		}
		certs = append(certs, c)
	}
	if len(certs) > 0 {
		return certs, nil
	}

	// DER (single cert)
	c, err := uapolicy.ParseCertificate(b)
	if err != nil {
		return nil, err
	}
	return []*x509.Certificate{c}, nil
}

func decodeToken(s string) []byte {
	s = strings.TrimSpace(s)
	if decoded, err := base64.StdEncoding.DecodeString(s); err == nil && len(decoded) > 0 {
		return decoded
	}
	if decoded, err := base64.RawStdEncoding.DecodeString(s); err == nil && len(decoded) > 0 {
		return decoded
	}
	return []byte(s)
}

func bytesTrimSpace(b []byte) []byte {
	return []byte(strings.TrimSpace(string(b)))
}

// TimeoutContext returns a context with the configured timeout, or the parent context.
func (cfg *Config) TimeoutContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if cfg.Timeout > 0 {
		return context.WithTimeout(ctx, cfg.Timeout)
	}
	return ctx, func() {}
}
