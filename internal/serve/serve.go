// SPDX-License-Identifier: MIT

// Package serve runs the OPC UA server of `opcuactl serve`: a go-opcua
// server with the configured endpoints, security and users, a small demo
// address space with simulated values and imported NodeSet2 models.
package serve

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/otfabric/go-opcua/server"
	"github.com/otfabric/go-opcua/ua"
)

// User is a user name with its password.
type User struct {
	Name     string
	Password string
}

// NodeSet is a NodeSet2 XML document to import.
type NodeSet struct {
	Name string // file name, for errors
	XML  []byte
}

// Config is the configuration of a server. Addresses are host:port; port 0
// picks a free port.
type Config struct {
	// Listen is the bind address of the opc.tcp listener.
	Listen string
	// WebSocket and HTTPS are the bind addresses of the opc.wss and the
	// opc.https listeners, "" for none.
	WebSocket string
	HTTPS     string
	// Hostname is the host the endpoint URLs advertise.
	Hostname string
	// TLS is the TLS certificate of opc.wss and opc.https; nil uses the RSA
	// application instance certificate.
	TLS *tls.Certificate
	// UAJSON serves wss-uajson and https-uajson next to UA Binary.
	UAJSON bool

	// Policies are the security policy URIs; Modes the modes of the
	// policies other than None (Sign, SignAndEncrypt; default
	// SignAndEncrypt).
	Policies []string
	Modes    []ua.MessageSecurityMode
	// Identities are the application instance certificates.
	Identities []Identity
	// ApplicationURI is the ApplicationUri; "" takes the one of the first
	// certificate.
	ApplicationURI string
	// ApplicationName is the ApplicationName of the server.
	ApplicationName string
	// TrustedClients, when not nil, are the certificates (CAs or
	// self-signed application certificates) client application
	// certificates are verified against. nil accepts any client
	// certificate.
	TrustedClients [][]byte

	// Anonymous allows anonymous sessions; Users are the accepted user
	// names and passwords.
	Anonymous bool
	Users     []User

	// Demo adds the demo namespace; Interval is the period of its
	// simulated values (default 1s).
	Demo     bool
	Interval time.Duration
	// NodeSets are imported after the demo namespace.
	NodeSets []NodeSet

	// Logger receives the log of the server; nil is silent.
	Logger *slog.Logger
}

// Server is a configured go-opcua server.
type Server struct {
	cfg  Config
	srv  *server.Server
	demo *demo

	stop chan struct{}
	wg   sync.WaitGroup
	once sync.Once
}

// ErrNoUser is returned for a configuration in which nobody can log in.
var ErrNoUser = errors.New("no user can log in: allow anonymous or add a user")

// New creates the server from cfg; Start starts it.
func New(cfg Config) (*Server, error) {
	if cfg.Interval <= 0 {
		cfg.Interval = time.Second
	}
	if !cfg.Anonymous && len(cfg.Users) == 0 {
		return nil, ErrNoUser
	}
	opts, err := cfg.options()
	if err != nil {
		return nil, err
	}
	srv, err := server.New(opts...)
	if err != nil {
		return nil, err
	}
	s := &Server{cfg: cfg, srv: srv, stop: make(chan struct{})}
	if cfg.Demo {
		if s.demo, err = newDemo(srv); err != nil {
			return nil, err
		}
	}
	for _, ns := range cfg.NodeSets {
		if err := srv.ImportNodeSetXML(ns.XML); err != nil {
			return nil, fmt.Errorf("importing %s: %w", ns.Name, err)
		}
	}
	return s, nil
}

func (cfg *Config) options() ([]server.Option, error) {
	host, port, err := splitHostPort(cfg.Listen)
	if err != nil {
		return nil, fmt.Errorf("listen address %q: %w", cfg.Listen, err)
	}
	advertised := cfg.Hostname
	if advertised == "" {
		advertised = host
	}
	tcpHost := advertised
	if strings.Contains(tcpHost, ":") {
		tcpHost = "[" + tcpHost + "]" // server.EndPoint does not bracket IPv6 hosts
	}
	opts := []server.Option{
		server.EndPoint(tcpHost, port),
		server.ListenOn(cfg.Listen),
		server.ProductName("opcuactl"),
		server.ManufacturerName("otfabric"),
	}
	if cfg.ApplicationName != "" {
		opts = append(opts, server.ServerName(cfg.ApplicationName))
	}
	if cfg.Logger != nil {
		opts = append(opts, server.SetLogger(cfg.Logger))
	}
	for _, id := range cfg.Identities {
		opts = append(opts, server.ApplicationIdentity(id.Cert, id.Key))
	}
	// After the identities: ApplicationIdentity takes the ApplicationUri of
	// the first certificate.
	if cfg.ApplicationURI != "" {
		opts = append(opts, server.ApplicationURI(cfg.ApplicationURI))
	}
	for _, t := range []struct {
		addr    string
		name    string
		add     func(string, int) server.Option
		listen  func(string) server.Option
		enabled bool
	}{
		{cfg.WebSocket, "opc.wss", server.WebSocketEndPoint, server.ListenWebSocketOn, cfg.WebSocket != ""},
		{cfg.HTTPS, "opc.https", server.HTTPSEndPoint, server.ListenHTTPSOn, cfg.HTTPS != ""},
	} {
		if !t.enabled {
			continue
		}
		_, p, err := splitHostPort(t.addr)
		if err != nil {
			return nil, fmt.Errorf("%s address %q: %w", t.name, t.addr, err)
		}
		opts = append(opts, t.add(advertised, p), t.listen(t.addr))
	}
	if cfg.TLS != nil {
		opts = append(opts, server.TLSConfig(&tls.Config{Certificates: []tls.Certificate{*cfg.TLS}, MinVersion: tls.VersionTLS12}))
	}
	if cfg.UAJSON {
		opts = append(opts, server.EnableUAJSON())
	}
	modes := cfg.Modes
	if len(modes) == 0 {
		modes = []ua.MessageSecurityMode{ua.MessageSecurityModeSignAndEncrypt}
	}
	for _, p := range cfg.Policies {
		if p == ua.SecurityPolicyURINone {
			opts = append(opts, server.EnableSecurity(p, ua.MessageSecurityModeNone))
			continue
		}
		for _, m := range modes {
			opts = append(opts, server.EnableSecurity(p, m))
		}
	}
	if cfg.TrustedClients != nil {
		opts = append(opts, server.WithClientCertificateTrustList(cfg.TrustedClients...))
	}
	if cfg.Anonymous {
		opts = append(opts, server.EnableAuthMode(ua.UserTokenTypeAnonymous))
	}
	if len(cfg.Users) > 0 {
		users := cfg.Users
		opts = append(opts,
			server.EnableAuthMode(ua.UserTokenTypeUserName),
			server.WithUsernameValidator(func(name, password string) error {
				for _, u := range users {
					if subtle.ConstantTimeCompare([]byte(u.Name), []byte(name)) == 1 &&
						subtle.ConstantTimeCompare([]byte(u.Password), []byte(password)) == 1 {
						return nil
					}
				}
				return ua.StatusBadUserAccessDenied
			}))
	}
	return opts, nil
}

// splitHostPort splits host:port with a numeric port.
func splitHostPort(addr string) (string, int, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", 0, err
	}
	p, err := net.LookupPort("tcp", port)
	if err != nil {
		return "", 0, err
	}
	return host, p, nil
}

// Start starts the listeners and the simulation of the demo namespace.
func (s *Server) Start(ctx context.Context) error {
	if err := s.srv.Start(ctx); err != nil {
		return err
	}
	if s.demo != nil {
		s.demo.emitStarted()
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.demo.run(s.cfg.Interval, s.stop)
		}()
	}
	return nil
}

// Close stops the simulation and the server: the listeners, the secure
// channels and the sessions. It may be called more than once.
func (s *Server) Close() error {
	var err error
	s.once.Do(func() {
		close(s.stop)
		s.wg.Wait()
		err = s.srv.Close()
	})
	return err
}

// Endpoints returns the endpoint descriptions the server offers.
func (s *Server) Endpoints() []*ua.EndpointDescription { return s.srv.Endpoints() }

// ApplicationURI returns the ApplicationUri the endpoints carry.
func (s *Server) ApplicationURI() string {
	for _, ep := range s.srv.Endpoints() {
		if ep.Server != nil && ep.Server.ApplicationURI != "" {
			return ep.Server.ApplicationURI
		}
	}
	return s.cfg.ApplicationURI
}

// DemoNamespace returns the URI and index of the demo namespace, ok false
// without it.
func (s *Server) DemoNamespace() (uri string, index uint16, ok bool) {
	if s.demo == nil {
		return "", 0, false
	}
	return DemoNamespaceURI, s.demo.ns.ID(), true
}

// Namespaces returns the URIs of the namespaces of the server, by index.
func (s *Server) Namespaces() []string {
	var out []string
	for _, ns := range s.srv.Namespaces() {
		out = append(out, ns.Name())
	}
	return out
}

// shortPolicy returns the name of a security policy URI.
func shortPolicy(uri string) string {
	if i := strings.LastIndex(uri, "#"); i >= 0 {
		return uri[i+1:]
	}
	return uri
}
