// SPDX-License-Identifier: MIT

package opcuactl

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/opcuactl/internal/serve"
)

// opcuactl serve with opc.tcp, opc.wss and opc.https endpoints answers
// GetEndpoints and CreateSession with the same endpoints for the URL a
// client names, also with the host in another letter case or with a
// trailing slash, so auto and an explicit policy without --server-cert
// connect over every transport.
func TestServeEndpointURLVariants(t *testing.T) {
	dir := t.TempDir()
	certOut := filepath.Join(dir, "server.pem")
	s := startServe(t, "--listen", "localhost:0", "--wss", "localhost:0", "--https", "localhost:0", "--uajson",
		"--cert-out", certOut, "--log-level", "off")
	cli := newIdentity(t, "rsa", clientURI)
	trust := []string{"--cert", cli.certFile, "--key", cli.keyFile, "--trusted-cert", certOut, "--tls-ca", certOut}

	variants := []struct {
		name string
		url  func(string) string
	}{
		{"as served", func(u string) string { return u }},
		{"host in upper case", func(u string) string { return strings.Replace(u, "localhost", "LOCALHOST", 1) }},
		{"trailing slash", func(u string) string { return u + "/" }},
		{"host in upper case and trailing slash", func(u string) string { return strings.Replace(u, "localhost", "LocalHost", 1) + "/" }},
	}
	for _, tr := range []struct {
		name, profile, transport string
		security                 [][]string
	}{
		{"opc.tcp", ua.TransportProfileUATCP, "", [][]string{
			{"--security-policy", "auto", "--security-mode", "auto"},
			{"--security-policy", "Basic256Sha256", "--security-mode", "SignAndEncrypt"},
		}},
		{"opc.wss", ua.TransportProfileWSS, "", [][]string{
			{"--security-policy", "auto", "--security-mode", "auto"},
			{"--security-policy", "Basic256Sha256", "--security-mode", "SignAndEncrypt"},
		}},
		{"opc.https", ua.TransportProfileHTTPS, "", [][]string{
			{"--security-policy", "auto", "--security-mode", "auto"},
			{"--security-policy", "Basic256Sha256", "--security-mode", "SignAndEncrypt"},
		}},
		{"wss-uajson", ua.TransportProfileWSSJSON, "wss-uajson", [][]string{
			{"--security-policy", "auto", "--security-mode", "auto"},
		}},
		{"https-uajson", ua.TransportProfileHTTPSJSON, "https-uajson", [][]string{
			{"--security-policy", "auto", "--security-mode", "auto"},
		}},
	} {
		served := s.url(t, tr.profile)
		if !strings.Contains(served, "localhost") {
			t.Fatalf("%s endpoint %q does not name localhost", tr.name, served)
		}
		for _, v := range variants {
			for _, sec := range tr.security {
				t.Run(tr.name+", "+v.name+", "+strings.Join(sec, " "), func(t *testing.T) {
					args := append([]string{"get", "value", "-e", v.url(served), "-n", s.node(serve.DemoInt32), "-q"}, sec...)
					if tr.transport != "" {
						args = append(args, "--transport", tr.transport)
					}
					stdout, stderr, err := runCLI(t, append(args, trust...)...)
					if err != nil || strings.TrimSpace(stdout) != "42" {
						t.Errorf("exit %d (%v)\nstdout: %s\nstderr: %s", ExitCode(err), err, stdout, stderr)
					}
				})
			}
		}
	}
}
