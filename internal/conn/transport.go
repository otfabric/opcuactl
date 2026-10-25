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
	"github.com/otfabric/go-opcua/uasc"
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

// Transport returns the transport profile URI the client uses: the one of
// --transport (TransportProfile) when set, else the one of the endpoint URL
// (ua.TransportProfileUATCP, ua.TransportProfileWSS or
// ua.TransportProfileHTTPS: the URL scheme alone always means UA Binary),
// "" for an unknown scheme or no endpoint.
func (cfg *Config) Transport() string {
	if cfg.TransportProfile != "" {
		return cfg.TransportProfile
	}
	return ua.TransportProfileForURL(cfg.Endpoint)
}

// ValidateTransport checks that the transport of --transport fits the
// endpoint URL: wss-uajson needs an opc.wss URL, https-uajson an opc.https
// one, and the UA JSON mappings have no reverse connect. A transport profile
// the library does not implement is refused. Without a --transport there is
// nothing to check, without an endpoint only the profile itself.
func (cfg *Config) ValidateTransport() error {
	t := cfg.TransportProfile
	if t == "" {
		return nil
	}
	if ua.IsJSONTransportProfile(t) && cfg.Reverse() {
		return fmt.Errorf("--transport %s: reverse connect is opc.tcp only", TransportName(t))
	}
	want := ua.TransportProfileScheme(t)
	if want == "" {
		return fmt.Errorf("--transport %s: not a transport this client implements (tcp|wss|https|wss-uajson|https-uajson)", t)
	}
	if cfg.Endpoint == "" {
		return nil
	}
	if ua.TransportProfileScheme(ua.TransportProfileForURL(cfg.Endpoint)) != want {
		return fmt.Errorf("--transport %s needs an %s:// endpoint, not %s", TransportName(t), want, cfg.Endpoint)
	}
	return nil
}

// transportOptions returns the options of the UA JSON transports: the
// transport profile when --transport names one (the URL scheme alone
// selects UA Binary) and the Content-Type of https-uajson requests.
func (cfg *Config) transportOptions() []opcua.Option {
	var opts []opcua.Option
	if ua.IsJSONTransportProfile(cfg.TransportProfile) {
		opts = append(opts, opcua.TransportProfile(cfg.TransportProfile))
	}
	if cfg.JSONContentType != "" {
		opts = append(opts, opcua.HTTPSJSONContentType(cfg.JSONContentType))
	}
	return opts
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

// TransportName returns the short name of a transport profile URI: opc.tcp,
// opc.wss, opc.https for the UA Binary mappings, "opc.wss (json)" and
// "opc.https (json)" for the UA JSON ones, or the URI itself when it is not
// a known one.
func TransportName(uri string) string {
	switch uri {
	case ua.TransportProfileUATCP, "":
		return "opc.tcp"
	case ua.TransportProfileWSS:
		return "opc.wss"
	case ua.TransportProfileHTTPS:
		return "opc.https"
	case ua.TransportProfileWSSJSON:
		return "opc.wss (json)"
	case ua.TransportProfileHTTPSJSON:
		return "opc.https (json)"
	}
	return uri
}

// ParseTransport maps a transport name (tcp, opc.tcp, wss, opc.wss, https,
// opc.https, wss-uajson, https-uajson, wss-json, https-json), a transport
// profile URI or a part of one (uatcp, wss-uasc-uabinary) to the transport
// profile URI. A name with "json" in it selects the UA JSON mapping of its
// transport.
func ParseTransport(s string) (string, error) {
	l := strings.ToLower(strings.TrimSpace(s))
	json := strings.Contains(l, "json")
	switch {
	case l == "":
		return "", nil
	case strings.Contains(l, "https"):
		if json {
			return ua.TransportProfileHTTPSJSON, nil
		}
		return ua.TransportProfileHTTPS, nil
	case strings.Contains(l, "wss"), strings.Contains(l, "websocket"):
		if json {
			return ua.TransportProfileWSSJSON, nil
		}
		return ua.TransportProfileWSS, nil
	case strings.Contains(l, "tcp") && !json:
		return ua.TransportProfileUATCP, nil
	case strings.Contains(l, "://"):
		return strings.TrimSpace(s), nil
	}
	return "", fmt.Errorf("unknown transport %q (tcp|wss|https|wss-uajson|https-uajson or a transport profile URI)", s)
}

// JSON Content-Types of https-uajson requests (--json-content-type).
const (
	JSONContentTypeStandard = "standard"
	JSONContentTypeLegacy   = "legacy"
)

// ParseJSONContentType maps --json-content-type to the Content-Type of
// https-uajson requests: standard (application/json, Part 6 §7.4.5; also
// "" for the library default) or legacy (application/opcua+uajson, which
// UA-.NETStandard 2.0.1 needs). The media types themselves are accepted too.
func ParseJSONContentType(s string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "":
		return "", nil
	case JSONContentTypeStandard, uasc.HTTPSJSONContentType:
		return uasc.HTTPSJSONContentType, nil
	case JSONContentTypeLegacy, uasc.HTTPSJSONContentTypeLegacy:
		return uasc.HTTPSJSONContentTypeLegacy, nil
	}
	return "", fmt.Errorf("unknown JSON content type %q (standard|legacy)", s)
}
