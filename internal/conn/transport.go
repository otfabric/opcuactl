// SPDX-License-Identifier: MIT

package conn

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strings"

	"github.com/otfabric/go-opcua"
	"github.com/otfabric/go-opcua/ua"
)

// HasTLS reports whether any TLS setting (--tls-*) is configured.
func (cfg *Config) HasTLS() bool {
	return cfg.TLSCAFile != "" || cfg.TLSCertFile != "" || cfg.TLSKeyFile != "" || cfg.TLSInsecureSkipVerify
}

// TLSConfig returns the TLS configuration of opc.wss and opc.https
// endpoints, nil when no TLS setting is configured (the library defaults:
// the system roots, no client certificate).
func (cfg *Config) TLSConfig() (*tls.Config, error) {
	if !cfg.HasTLS() {
		return nil, nil
	}
	tc := &tls.Config{MinVersion: tls.VersionTLS12}
	if cfg.TLSCAFile != "" {
		pool, err := loadCAPool(cfg.TLSCAFile)
		if err != nil {
			return nil, fmt.Errorf("loading --tls-ca: %w", err)
		}
		tc.RootCAs = pool
	}
	switch {
	case cfg.TLSCertFile != "" && cfg.TLSKeyFile != "":
		pair, err := tls.LoadX509KeyPair(cfg.TLSCertFile, cfg.TLSKeyFile)
		if err != nil {
			return nil, fmt.Errorf("loading --tls-cert/--tls-key: %w", err)
		}
		tc.Certificates = []tls.Certificate{pair}
	case cfg.TLSCertFile != "" || cfg.TLSKeyFile != "":
		return nil, fmt.Errorf("--tls-cert and --tls-key must be provided together")
	}
	// Development only; the CLI warns when it is set.
	tc.InsecureSkipVerify = cfg.TLSInsecureSkipVerify
	return tc, nil
}

// loadCAPool reads PEM (or a single DER) certificates into a pool.
func loadCAPool(file string) (*x509.CertPool, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if pool.AppendCertsFromPEM(b) {
		return pool, nil
	}
	c, err := x509.ParseCertificate(b)
	if err != nil {
		return nil, fmt.Errorf("no certificates in %s", file)
	}
	pool.AddCert(c)
	return pool, nil
}

// tlsOption applies TLSConfig; a configuration that does not load fails the
// creation of the client.
func (cfg *Config) tlsOption() opcua.Option {
	return func(c *opcua.Config) error {
		tc, err := cfg.TLSConfig()
		if err != nil {
			return err
		}
		return opcua.TLSConfig(tc)(c)
	}
}

// Transport returns the transport profile URI of the endpoint URL
// (ua.TransportProfileUATCP, ua.TransportProfileWSS or
// ua.TransportProfileHTTPS), "" for an unknown scheme or no endpoint.
func (cfg *Config) Transport() string {
	return ua.TransportProfileForURL(cfg.Endpoint)
}

// EndpointsForTransport returns the endpoints with the transport profile
// uri. An endpoint without a TransportProfileUri counts as the transport of
// its URL scheme (opc.tcp if unknown); an empty uri matches every endpoint.
func EndpointsForTransport(endpoints []*ua.EndpointDescription, uri string) []*ua.EndpointDescription {
	if uri == "" {
		return endpoints
	}
	var out []*ua.EndpointDescription
	for _, ep := range endpoints {
		if ep != nil && EndpointTransport(ep) == uri {
			out = append(out, ep)
		}
	}
	return out
}

// EndpointTransport returns the transport profile URI of an endpoint: its
// TransportProfileUri, or the one of its URL scheme when it has none.
func EndpointTransport(ep *ua.EndpointDescription) string {
	if ep.TransportProfileURI != "" {
		return ep.TransportProfileURI
	}
	if t := ua.TransportProfileForURL(ep.EndpointURL); t != "" {
		return t
	}
	return ua.TransportProfileUATCP
}

// TransportName returns the short name of a transport profile URI (opc.tcp,
// opc.wss, opc.https), or the URI itself when it is not a known one.
func TransportName(uri string) string {
	switch uri {
	case ua.TransportProfileUATCP, "":
		return "opc.tcp"
	case ua.TransportProfileWSS:
		return "opc.wss"
	case ua.TransportProfileHTTPS:
		return "opc.https"
	}
	return uri
}

// ParseTransport maps a transport name (tcp, opc.tcp, wss, opc.wss, https,
// opc.https), a transport profile URI or a part of one (uatcp,
// wss-uasc-uabinary) to the transport profile URI.
func ParseTransport(s string) (string, error) {
	l := strings.ToLower(strings.TrimSpace(s))
	switch {
	case l == "":
		return "", nil
	case strings.Contains(l, "https"):
		return ua.TransportProfileHTTPS, nil
	case strings.Contains(l, "wss"), l == "websocket":
		return ua.TransportProfileWSS, nil
	case strings.Contains(l, "tcp"):
		return ua.TransportProfileUATCP, nil
	case strings.Contains(l, "://"):
		return strings.TrimSpace(s), nil
	}
	return "", fmt.Errorf("unknown transport %q (tcp|wss|https or a transport profile URI)", s)
}
