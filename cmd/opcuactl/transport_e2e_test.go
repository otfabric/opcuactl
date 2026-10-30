// SPDX-License-Identifier: MIT

package opcuactl

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"testing"

	"github.com/otfabric/go-opcua/server"
	"github.com/otfabric/go-opcua/ua"
)

// tlsServer is a test server with opc.wss and opc.https endpoints next to
// opc.tcp; its RSA application certificate (CN localhost) serves TLS.
type tlsServer struct {
	*testServer
	cert  *identity
	wss   string
	https string
}

func startTLSServer(t *testing.T, opts ...server.Option) *tlsServer {
	t.Helper()
	cert := newIdentity(t, "rsa", serverURI)
	s := startServer(t, append([]server.Option{
		server.ApplicationIdentity(cert.der, cert.key),
		server.WebSocketEndPoint("localhost", 0),
		server.HTTPSEndPoint("localhost", 0),
		server.EnableSecurity("Basic256Sha256", ua.MessageSecurityModeSignAndEncrypt),
	}, opts...)...)
	port := func(a net.Addr) int {
		if a == nil {
			t.Fatal("the server has no opc.wss / opc.https listener")
		}
		return a.(*net.TCPAddr).Port
	}
	return &tlsServer{
		testServer: s,
		cert:       cert,
		wss:        fmt.Sprintf("opc.wss://localhost:%d", port(s.srv.WebSocketAddr())),
		https:      fmt.Sprintf("opc.https://localhost:%d", port(s.srv.HTTPSAddr())),
	}
}

// TestE2E_HTTPSAndWebSocket runs commands over opc.wss and opc.https.
func TestE2E_HTTPSAndWebSocket(t *testing.T) {
	s := startTLSServer(t)
	ca := []string{"--tls-ca", s.cert.certFile}

	for _, ep := range []string{s.wss, s.https} {
		t.Run(strings.SplitN(ep, ":", 2)[0], func(t *testing.T) {
			out := mustRun(t, append([]string{"get", "value", "-e", ep, "-n", s.node("Int32"), "-q"}, ca...)...)
			wantContains(t, out, "42")

			out = mustRun(t, append([]string{"write", "value", "-e", ep, "-n", s.node("String"), "--value", "over " + ep, "--yes"}, ca...)...)
			wantContains(t, out, "Good")
			out = mustRun(t, append([]string{"get", "value", "-e", ep, "-n", s.node("String"), "-q"}, ca...)...)
			wantContains(t, out, "over "+ep)

			out = mustRun(t, append([]string{"browse", "children", "-e", ep, "-n", "i=85"}, ca...)...)
			wantContains(t, out, "Server")

			// A subscription: Publish is a long poll over HTTPS.
			out = mustRun(t, append([]string{"monitor", "value", "-e", ep, "-n", s.node("Int32"), "--count", "1", "--duration", "20s", "--format", "json"}, ca...)...)
			wantContains(t, out, `"value":42`)

			out = mustRun(t, append([]string{"ping", "-e", ep, "--format", "json"}, ca...)...)
			var steps []struct{ Stage, Status, Error string }
			if err := json.Unmarshal([]byte(out), &steps); err != nil {
				t.Fatalf("ping json: %v\n%s", err, out)
			}
			if len(steps) != 3 {
				t.Fatalf("ping stages = %+v", steps)
			}
			for _, st := range steps {
				if st.Status != "ok" {
					t.Errorf("ping stage %s: %s %s", st.Stage, st.Status, st.Error)
				}
			}
		})
	}

	t.Run("unknown authority", func(t *testing.T) {
		for _, ep := range []string{s.wss, s.https} {
			_, errOut, err := runCLI(t, "get", "value", "-e", ep, "-n", s.node("Int32"), "--timeout", "5s")
			if err == nil {
				t.Fatalf("%s: connected without trusting the TLS certificate", ep)
			}
			if ExitCode(err) != 4 || !strings.Contains(errOut, "certificate") {
				t.Errorf("%s: exit %d, stderr %s", ep, ExitCode(err), errOut)
			}
		}
	})

	t.Run("insecure skip verify warns", func(t *testing.T) {
		out, errOut, err := runCLI(t, "get", "value", "-e", s.wss, "-n", s.node("Int32"), "-q", "--tls-insecure-skip-verify")
		if err != nil {
			t.Fatalf("get value: %v\n%s", err, errOut)
		}
		wantContains(t, out, "42")
		wantContains(t, errOut, "Warning: --tls-insecure-skip-verify")
	})

	t.Run("environment", func(t *testing.T) {
		t.Setenv("OPCUACTL_TLS_CA", s.cert.certFile)
		t.Setenv("OPCUACTL_ENDPOINT", s.https)
		wantContains(t, mustRun(t, "get", "value", "-n", s.node("Int32"), "-q"), "42")

		t.Setenv("OPCUACTL_TLS_CA", "")
		t.Setenv("OPCUACTL_TLS_INSECURE_SKIP_VERIFY", "true")
		wantContains(t, mustRun(t, "get", "value", "-n", s.node("Int32"), "-q"), "42")

		t.Setenv("OPCUACTL_TLS_INSECURE_SKIP_VERIFY", "maybe")
		if _, _, err := runCLI(t, "get", "value", "-n", s.node("Int32")); err == nil {
			t.Error("an invalid OPCUACTL_TLS_INSECURE_SKIP_VERIFY was accepted")
		}
	})

	t.Run("discover endpoints shows the transport", func(t *testing.T) {
		// The server answers with the endpoints of the transport asked on.
		for ep, want := range map[string]string{s.url: "opc.tcp ", s.wss: "opc.wss ", s.https: "opc.https "} {
			out := mustRun(t, append([]string{"discover", "endpoints", "-e", ep}, ca...)...)
			wantContains(t, out, "TRANSPORT", want, ep, "Basic256Sha256")
		}

		out := mustRun(t, append([]string{"discover", "endpoints", "-e", s.https, "--transport-profile", "https", "--format", "json"}, ca...)...)
		var eps []struct {
			EndpointURL      string `json:"endpointUrl"`
			TransportProfile string `json:"transportProfile"`
		}
		if err := json.Unmarshal([]byte(out), &eps); err != nil {
			t.Fatalf("json: %v\n%s", err, out)
		}
		if len(eps) == 0 {
			t.Fatal("no https endpoints")
		}
		for _, ep := range eps {
			if ep.TransportProfile != ua.TransportProfileHTTPS || !strings.HasPrefix(ep.EndpointURL, "opc.https://") {
				t.Errorf("--transport-profile https listed %+v", ep)
			}
		}
		out = mustRun(t, append([]string{"discover", "endpoints", "-e", s.https, "--transport-profile", "tcp", "--format", "json"}, ca...)...)
		if strings.Contains(out, "opc.https") {
			t.Errorf("--transport-profile tcp listed https endpoints:\n%s", out)
		}

		if _, _, err := runCLI(t, "discover", "endpoints", "-e", s.url, "--transport-profile", "carrier-pigeon"); err == nil {
			t.Error("an unknown transport was accepted")
		}
	})

	t.Run("best endpoint of the dialled transport", func(t *testing.T) {
		out := mustRun(t, append([]string{"discover", "endpoints", "-e", s.wss, "--best", "--format", "json"}, ca...)...)
		wantContains(t, out, `"transportProfile":"`+ua.TransportProfileWSS+`"`, "Basic256Sha256")
		out = mustRun(t, append([]string{"discover", "endpoints", "-e", s.https, "--best"}, ca...)...)
		wantContains(t, out, "opc.https", s.https)
		_, errOut, err := runCLI(t, append([]string{"discover", "endpoints", "-e", s.https, "--best", "--transport-profile", "wss"}, ca...)...)
		if err == nil || !strings.Contains(errOut, "no opc.wss endpoint") {
			t.Errorf("--best --transport-profile wss on an https URL: %v %s", err, errOut)
		}
	})

	t.Run("security auto over wss", func(t *testing.T) {
		cli := newIdentity(t, "rsa", clientURI)
		out := mustRun(t, append([]string{"get", "server", "-e", s.wss, "--format", "json",
			"--security-policy", "auto", "--security-mode", "auto",
			"--cert", cli.certFile, "--key", cli.keyFile, "--trusted-cert", s.cert.certFile}, ca...)...)
		wantContains(t, out, "Basic256Sha256", "SignAndEncrypt")
	})

	t.Run("tls flag validation", func(t *testing.T) {
		for _, args := range [][]string{
			{"--tls-ca", "/nonexistent/ca.pem"},
			{"--tls-cert", s.cert.certFile},
			{"--tls-cert", s.cert.certFile, "--tls-key", s.cert.certFile},
			{"--tls-ca", s.cert.keyFile},
		} {
			_, _, err := runCLI(t, append([]string{"get", "value", "-e", s.wss, "-n", "i=2258"}, args...)...)
			if err == nil || ExitCode(err) != 2 && ExitCode(err) != 3 {
				t.Errorf("%v: err = %v (exit %d), want a usage error", args, err, ExitCode(err))
			}
		}
	})
}

// TestE2E_MutualTLS connects to a server that requires a client TLS
// certificate.
func TestE2E_MutualTLS(t *testing.T) {
	srvCert := newIdentity(t, "rsa", serverURI)
	cli := newIdentity(t, "p256", clientURI)
	clientCA, err := x509.ParseCertificate(cli.der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(clientCA)
	s := startTLSServer(t, server.TLSConfig(&tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{{Certificate: [][]byte{srvCert.der}, PrivateKey: srvCert.key}},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    pool,
	}))
	ca := []string{"--tls-ca", srvCert.certFile}

	for _, ep := range []string{s.wss, s.https} {
		if _, _, err := runCLI(t, append([]string{"get", "value", "-e", ep, "-n", s.node("Int32"), "--timeout", "5s"}, ca...)...); err == nil {
			t.Errorf("%s: connected without a client TLS certificate", ep)
		}
		out := mustRun(t, append([]string{"get", "value", "-e", ep, "-n", s.node("Int32"), "-q",
			"--tls-cert", cli.certFile, "--tls-key", cli.keyFile}, ca...)...)
		wantContains(t, out, "42")
	}
}
