// SPDX-License-Identifier: MIT

package conn

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/otfabric/go-opcua"
	"github.com/otfabric/go-opcua/ua"
)

// Config holds the connection parameters extracted from CLI flags.
type Config struct {
	Endpoint           string
	SecurityPolicy     string
	SecurityMode       string
	Auth               string
	Username           string
	Password           string
	AuthPolicyID       string
	Token              string
	TokenFile          string
	CertFile           string
	KeyFile            string
	ServerCertFile     string
	TrustedCertFiles   []string
	InsecureSkipVerify bool
	Timeout            time.Duration

	// ReverseListen makes the client wait on this address ("host:port" or
	// opc.tcp URL) for the server to connect (OPC UA reverse connect).
	ReverseListen string
	// ReverseServerURI restricts reverse connections to the server that
	// announces this ApplicationUri.
	ReverseServerURI string
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
	var opts []opcua.Option
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
func (cfg *Config) SelectEndpoint(endpoints []*ua.EndpointDescription, policy string, mode ua.MessageSecurityMode) (*ua.EndpointDescription, error) {
	if cfg.CertFile == "" {
		return ua.SelectEndpoint(endpoints, policy, mode)
	}
	certs, err := loadCertificatesFile(cfg.CertFile)
	if err != nil {
		return nil, fmt.Errorf("loading --cert: %w", err)
	}
	return opcua.SelectEndpoint(endpoints, policy, mode, certs[0].Raw)
}

// NewClient creates a connected OPC UA client from the config.
func (cfg *Config) NewClient(ctx context.Context) (*opcua.Client, error) {
	if err := cfg.RequireEndpoint(); err != nil {
		return nil, err
	}

	opts, err := cfg.buildOptions(ctx)
	if err != nil {
		return nil, err
	}

	c, err := opcua.NewClient(cfg.Endpoint, opts...)
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
		return []opcua.Option{opcua.SecurityFromEndpoint(ep, authType)}, nil
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

// loadCertKey loads a certificate with its RSA, ECDSA or Ed25519 private key.
func loadCertKey(certFile, keyFile string) ([]byte, crypto.Signer, error) {
	pair, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, nil, err
	}
	if len(pair.Certificate) == 0 {
		return nil, nil, fmt.Errorf("certificate file %q contains no certificates", certFile)
	}
	key, ok := pair.PrivateKey.(crypto.Signer)
	if !ok {
		return nil, nil, fmt.Errorf("unsupported private key type %T", pair.PrivateKey)
	}
	return pair.Certificate[0], key, nil
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
		c, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, err
		}
		certs = append(certs, c)
	}
	if len(certs) > 0 {
		return certs, nil
	}

	// DER (single cert)
	c, err := x509.ParseCertificate(b)
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
