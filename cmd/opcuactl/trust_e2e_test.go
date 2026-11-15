// SPDX-License-Identifier: MIT

package opcuactl

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/opcuactl/internal/model"
	"github.com/otfabric/opcuactl/internal/serve"
)

// --insecure-skip-verify is named on stderr, like --tls-insecure-skip-verify;
// stdout keeps only the value.
func TestInsecureSkipVerifyWarning(t *testing.T) {
	s := startServer(t)
	stdout, stderr, err := runCLI(t, "get", "value", "-e", s.url, "-n", s.node("Int32"), "-q", "--insecure-skip-verify")
	if err != nil {
		t.Fatalf("%v\n%s", err, stderr)
	}
	if strings.TrimSpace(stdout) != "42" {
		t.Errorf("stdout %q", stdout)
	}
	if !strings.Contains(stderr, "Warning: --insecure-skip-verify: the application certificate of the server is not verified") {
		t.Errorf("stderr %q", stderr)
	}

	_, stderr, err = runCLI(t, "get", "value", "-e", s.url, "-n", s.node("Int32"), "-q")
	if err != nil || strings.Contains(stderr, "Warning") {
		t.Errorf("without the flag: %v, stderr %q", err, stderr)
	}
}

// A server certificate that the trust list does not cover fails with exit 4
// and a hint that names --trusted-cert and the certificate's SHA-1
// thumbprint, the one serve and discover endpoints -v print.
func TestUntrustedServerCertificateHint(t *testing.T) {
	dir := t.TempDir()
	certOut := filepath.Join(dir, "server.pem")
	s := startServe(t, "--security-policy", "Basic256Sha256", "--cert-out", certOut, "--hostname", "127.0.0.1", "--log-level", "off")
	if len(s.info.Certificates) != 1 {
		t.Fatalf("certificates %+v", s.info.Certificates)
	}
	thumbprint := s.info.Certificates[0].Thumbprint
	tcp := s.url(t, ua.TransportProfileUATCP)
	cli := newIdentity(t, "rsa", clientURI)
	other := newIdentity(t, "rsa", "urn:otfabric:opcuactl:test:other")
	secure := []string{"get", "value", "-e", tcp, "-n", s.node(serve.DemoInt32), "-q",
		"--security-policy", "Basic256Sha256", "--security-mode", "SignAndEncrypt", "--cert", cli.certFile, "--key", cli.keyFile}

	for _, c := range []struct {
		name  string
		extra []string
		want  string
	}{
		{"no trust list", nil, "no --trusted-cert is given"},
		{"another certificate", []string{"--trusted-cert", other.certFile}, "not one of the --trusted-cert certificates"},
		{"auto", []string{"--security-policy", "auto", "--security-mode", "auto"}, "no --trusted-cert is given"},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, stderr, err := runCLI(t, append(secure, c.extra...)...)
			if got := ExitCode(err); got != exitTransport {
				t.Fatalf("exit %d, want %d (%v)", got, exitTransport, err)
			}
			for _, w := range []string{"server certificate not trusted", c.want, "SHA-1 thumbprint " + thumbprint, "with --trusted-cert"} {
				if !strings.Contains(stderr, w) {
					t.Errorf("stderr does not contain %q:\n%s", w, stderr)
				}
			}
			if strings.Contains(stderr, "TrustedCertificates") || strings.Contains(stderr, "--insecure-skip-verify") {
				t.Errorf("stderr names a Go option or the insecure flag:\n%s", stderr)
			}
		})
	}

	// The thumbprint discover endpoints prints is the one serve prints.
	eps := runJSON[[]model.Endpoint](t, "discover", "endpoints", "-e", tcp, "-v")
	secured := 0
	for _, ep := range eps {
		if !ep.HasCertificate {
			continue
		}
		secured++
		if ep.CertThumbprint != thumbprint {
			t.Errorf("%s %s: certThumbprint %q, serve prints %q", ep.SecurityPolicy, ep.SecurityMode, ep.CertThumbprint, thumbprint)
		}
	}
	if secured == 0 {
		t.Errorf("no endpoint with a certificate: %+v", eps)
	}
	out := mustRun(t, "discover", "endpoints", "-e", tcp, "-v")
	wantContains(t, out, "CERT THUMBPRINT", thumbprint)

	// With the certificate trusted the same command works.
	out = mustRun(t, append(secure, "--trusted-cert", certOut)...)
	wantContains(t, out, "42")
}
