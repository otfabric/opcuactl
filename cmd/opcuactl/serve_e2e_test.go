// SPDX-License-Identifier: MIT

package opcuactl

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/otfabric/go-opcua"
	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/opcuactl/internal/format"
	"github.com/otfabric/opcuactl/internal/model"
	"github.com/otfabric/opcuactl/internal/serve"
	"gopkg.in/yaml.v3"
)

// nodeSetFixture is a NodeSet2 file with an object Machine and the
// variables Machine.Speed (1500.5) and Machine.Name.
const nodeSetFixture = "../../internal/serve/testdata/machine.NodeSet2.xml"

// servedServer is `opcuactl serve` running in this process; the client
// commands run against it with runCLI.
type servedServer struct {
	info   model.ServeInfo
	stdout *syncBuffer
	stderr *syncBuffer
	cancel context.CancelFunc
	done   chan error
}

// startServe runs serve with args (and a free loopback port, --format json)
// until the test ends.
func startServe(t *testing.T, args ...string) *servedServer {
	t.Helper()
	s := startServeOutput(t, serveOutput{format: format.JSON}, append([]string{"--listen", "127.0.0.1:0"}, args...)...)
	if err := json.Unmarshal([]byte(s.stdout.String()), &s.info); err != nil {
		t.Fatalf("serve output: %v\n%s", err, s.stdout)
	}
	return s
}

// startServeOutput runs serve with args and out until it printed the
// server.
func startServeOutput(t *testing.T, out serveOutput, args ...string) *servedServer {
	t.Helper()
	f := &serveFlags{}
	if err := serveCommand(f).ParseFlags(append([]string{"--insecure-no-security"}, args...)); err != nil {
		t.Fatalf("serve %v: %v", args, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &servedServer{stdout: &syncBuffer{}, stderr: &syncBuffer{}, cancel: cancel, done: make(chan error, 1)}
	go func() { s.done <- f.run(ctx, s.stdout, s.stderr, out) }()
	t.Cleanup(func() { s.stop(t) })
	deadline := time.Now().Add(20 * time.Second)
	for !strings.Contains(s.stderr.String(), "Serving") {
		select {
		case err := <-s.done:
			s.done <- err
			t.Fatalf("serve %v exited: %v\nstderr: %s", args, err, s.stderr)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("serve %v did not start\nstderr: %s", args, s.stderr)
		}
		time.Sleep(10 * time.Millisecond)
	}
	return s
}

// stop shuts the server down and checks that it stopped cleanly.
func (s *servedServer) stop(t *testing.T) {
	t.Helper()
	s.cancel()
	select {
	case err := <-s.done:
		s.done <- err
		if err != nil {
			t.Errorf("serve: %v", err)
		}
		if !strings.Contains(s.stderr.String(), "Server stopped") {
			t.Errorf("serve did not report the shutdown:\n%s", s.stderr)
		}
	case <-time.After(20 * time.Second):
		t.Errorf("serve did not stop\nstderr: %s", s.stderr)
	}
}

// url returns the URL of the first endpoint with the transport profile.
func (s *servedServer) url(t *testing.T, profile string) string {
	t.Helper()
	for _, ep := range s.info.Endpoints {
		if ep.TransportProfile == profile {
			return ep.EndpointURL
		}
	}
	t.Fatalf("no %s endpoint in %+v", profile, s.info.Endpoints)
	return ""
}

// node returns the NodeId of a demo node.
func (s *servedServer) node(name string) string {
	return fmt.Sprintf("ns=%d;s=%s", s.info.DemoNamespaceIndex, name)
}

// TestE2E_Serve runs the client commands against `opcuactl serve` over
// opc.tcp (None and Basic256Sha256 with a user), opc.wss and opc.https in
// UA Binary and UA JSON.
func TestE2E_Serve(t *testing.T) {
	dir := t.TempDir()
	certOut, keyOut := filepath.Join(dir, "server.pem"), filepath.Join(dir, "server.key")
	s := startServe(t, "--wss", "127.0.0.1:0", "--https", "127.0.0.1:0", "--uajson",
		"--user", "operator:secret", "--user", "admin:admin", "--cert-out", certOut, "--key-out", keyOut,
		"--sim-interval", "50ms", "--hostname", "127.0.0.1", "--log-level", "off")
	tcp := s.url(t, ua.TransportProfileUATCP)

	t.Run("description", func(t *testing.T) {
		info := s.info
		if info.DemoNamespace != serve.DemoNamespaceURI || info.Namespaces[info.DemoNamespaceIndex] != serve.DemoNamespaceURI {
			t.Errorf("demo namespace %q ns=%d in %v", info.DemoNamespace, info.DemoNamespaceIndex, info.Namespaces)
		}
		if info.ApplicationURI != "urn:127.0.0.1:opcuactl:serve" || !info.Anonymous || strings.Join(info.Users, ",") != "operator,admin" || info.ClientTrust != "any" {
			t.Errorf("description %+v", info)
		}
		if len(info.Certificates) != 1 || !info.Certificates[0].Generated || info.Certificates[0].KeyType != "rsa" || info.Certificates[0].File != certOut {
			t.Errorf("certificates %+v", info.Certificates)
		}
		profiles := map[string]bool{}
		for _, ep := range info.Endpoints {
			profiles[ep.TransportProfile] = true
		}
		if len(profiles) != 5 {
			t.Errorf("transports %v", profiles)
		}
		st, err := os.Stat(keyOut)
		if err != nil || st.Mode().Perm() != 0o600 {
			t.Errorf("--key-out: %v %v", st, err)
		}
	})

	t.Run("opc.tcp None", func(t *testing.T) {
		wantContains(t, mustRun(t, "get", "value", "-e", tcp, "-n", s.node(serve.DemoInt32), "-q"), "42")
		wantContains(t, mustRun(t, "browse", "children", "-e", tcp, "-n", s.node(serve.DemoFolder)),
			"Demo.Counter", "Demo.Sine", "Demo.Level", "Demo.LevelAlarm", "Demo.Add")
		wantContains(t, mustRun(t, "call", "method", "-e", tcp, "--object", s.node(serve.DemoFolder), "--method", s.node(serve.DemoAdd),
			"--arg", "2", "--arg", "40", "--types", "int32,int32"), "42")

		// Read-only and writable.
		// A Bad write status is printed and exits 3.
		roOut, _, err := runCLI(t, "write", "value", "-e", tcp, "-n", s.node(serve.DemoInt32), "--value", "1", "--type", "int32", "--yes")
		if ExitCode(err) != 3 {
			t.Errorf("write to a read-only node: %v (exit %d)", err, ExitCode(err))
		}
		wantContains(t, roOut, "BadUserAccessDenied")
		wantContains(t, mustRun(t, "write", "value", "-e", tcp, "-n", s.node(serve.DemoLevel), "--value", "90", "--type", "double", "--yes"), "Good")
		wantContains(t, mustRun(t, "get", "value", "-e", tcp, "-n", s.node(serve.DemoLevel), "-q"), "90")

		// The level alarm follows Level.
		deadline := time.Now().Add(10 * time.Second)
		for {
			conds := listConditions(t, "-e", tcp)
			if len(conds) == 1 && conds[0].ConditionName == "LevelAlarm" && conds[0].Active != nil && *conds[0].Active {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("LevelAlarm is not active: %+v", conds)
			}
			time.Sleep(50 * time.Millisecond)
		}

		// Counter changes: a subscription sees several values.
		out := mustRun(t, "monitor", "value", "-e", tcp, "-n", s.node(serve.DemoCounter), "--count", "3", "--duration", "20s", "--format", "jsonl")
		if n := strings.Count(strings.TrimSpace(out), "\n") + 1; n != 3 {
			t.Errorf("monitor printed %d values:\n%s", n, out)
		}

		// Sine is historized.
		now := time.Now().UTC()
		out = mustRun(t, "history", "value", "-e", tcp, "-n", s.node(serve.DemoSine), "--format", "json",
			"--start", now.Add(-time.Minute).Format(time.RFC3339), "--end", now.Add(time.Minute).Format(time.RFC3339))
		if strings.Count(out, `"value"`) < 2 {
			t.Errorf("history of Sine:\n%s", out)
		}
	})

	cli := newIdentity(t, "rsa", clientURI)
	secure := []string{"--security-policy", "Basic256Sha256", "--security-mode", "SignAndEncrypt",
		"--cert", cli.certFile, "--key", cli.keyFile, "--trusted-cert", certOut}

	t.Run("opc.tcp Basic256Sha256 with a user", func(t *testing.T) {
		out := mustRun(t, append([]string{"get", "value", "-e", tcp, "-n", s.node(serve.DemoString), "-q",
			"--auth", "username", "--username", "operator", "--password", "secret"}, secure...)...)
		wantContains(t, out, "hello from opcuactl")
		out = mustRun(t, append([]string{"write", "value", "-e", tcp, "-n", s.node(serve.DemoLevel), "--value", "33", "--type", "double", "--yes",
			"--auth", "username", "--username", "operator", "--password", "secret"}, secure...)...)
		wantContains(t, out, "Good")
		out = mustRun(t, append([]string{"get", "value", "-e", tcp, "-n", s.node(serve.DemoInt32), "-q"}, secure...)...)
		wantContains(t, out, "42")

		_, errOut, err := runCLI(t, append([]string{"get", "value", "-e", tcp, "-n", s.node(serve.DemoInt32),
			"--auth", "username", "--username", "operator", "--password", "wrong"}, secure...)...)
		if err == nil || ExitCode(err) != 2 {
			t.Errorf("a wrong password: %v %s", err, errOut)
		}
		// Basic256Sha256 Sign and auto.
		out = mustRun(t, "get", "server", "-e", tcp, "--format", "json", "--security-policy", "auto", "--security-mode", "auto",
			"--cert", cli.certFile, "--key", cli.keyFile, "--trusted-cert", certOut)
		wantContains(t, out, "SignAndEncrypt")
	})

	ca := []string{"--tls-ca", certOut}
	for _, tc := range []struct{ name, profile, transport string }{
		{"opc.wss", ua.TransportProfileWSS, ""},
		{"opc.https", ua.TransportProfileHTTPS, ""},
		{"wss-uajson", ua.TransportProfileWSSJSON, "wss-uajson"},
		{"https-uajson", ua.TransportProfileHTTPSJSON, "https-uajson"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := func(a ...string) []string {
				a = append(append(a, "-e", s.url(t, tc.profile)), ca...)
				if tc.transport != "" {
					a = append(a, "--transport", tc.transport)
				}
				return a
			}
			wantContains(t, mustRun(t, args("get", "value", "-n", s.node(serve.DemoInt32), "-q")...), "42")
			wantContains(t, mustRun(t, args("write", "value", "-n", s.node(serve.DemoLevel), "--value", "12.5", "--type", "double", "--yes")...), "Good")
			wantContains(t, mustRun(t, args("monitor", "value", "-n", s.node(serve.DemoCounter), "--count", "2", "--duration", "20s")...), "Demo.Counter")
			wantContains(t, mustRun(t, args("call", "method", "--object", s.node(serve.DemoFolder), "--method", s.node(serve.DemoAdd),
				"--arg", "1", "--arg", "2", "--types", "int32,int32")...), "3")
		})
	}

	t.Run("user over https-uajson None", func(t *testing.T) {
		out := mustRun(t, append([]string{"get", "value", "-e", s.url(t, ua.TransportProfileHTTPSJSON), "--transport", "https-uajson",
			"-n", s.node(serve.DemoInt32), "-q", "--auth", "username", "--username", "admin", "--password", "admin"}, ca...)...)
		wantContains(t, out, "42")
	})

	t.Run("discover endpoints", func(t *testing.T) {
		out := mustRun(t, "discover", "endpoints", "-e", tcp)
		wantContains(t, out, "opc.tcp", "Basic256Sha256")
		out = mustRun(t, append([]string{"discover", "endpoints", "-e", s.url(t, ua.TransportProfileHTTPS)}, ca...)...)
		wantContains(t, out, "opc.https (json)")
		out = mustRun(t, append([]string{"discover", "endpoints", "-e", s.url(t, ua.TransportProfileWSS)}, ca...)...)
		wantContains(t, out, "opc.wss (json)")
	})
}

// TestE2E_ServeCertificatesAndTrust reuses the written certificate and
// restricts the clients to a trust list.
func TestE2E_ServeCertificatesAndTrust(t *testing.T) {
	dir := t.TempDir()
	certOut, keyOut := filepath.Join(dir, "server.pem"), filepath.Join(dir, "server.key")
	first := startServe(t, "--security-policy", "Basic256Sha256,ECC_nistP256", "--security-mode", "SignAndEncrypt",
		"--cert-out", certOut, "--key-out", keyOut)
	if len(first.info.Certificates) != 2 || first.info.Certificates[1].KeyType != "ecc-P-256" {
		t.Fatalf("certificates %+v", first.info.Certificates)
	}
	first.stop(t)

	trusted := newIdentity(t, "rsa", clientURI)
	untrusted := newIdentity(t, "p256", clientURI)
	trustDir := filepath.Join(dir, "trusted")
	if err := os.Mkdir(trustDir, 0o700); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(trusted.certFile)
	if err := os.WriteFile(filepath.Join(trustDir, "client.pem"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(trustDir, "README.txt"), []byte("not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(trustDir, "sub.pem"), 0o700); err != nil {
		t.Fatal(err)
	}

	s := startServe(t, "--security-policy", "Basic256Sha256,ECC_nistP256", "--security-mode", "SignAndEncrypt",
		"--cert", certOut, "--key", keyOut, "--trust-dir", trustDir, "--trusted-cert", trusted.certFile, "--allow-anonymous")
	for i, c := range s.info.Certificates {
		if c.Generated || c.Thumbprint != first.info.Certificates[i].Thumbprint || c.File != certOut {
			t.Errorf("certificate %d: %+v, first run %+v", i, c, first.info.Certificates[i])
		}
	}
	if s.info.ClientTrust != "2 certificate(s)" {
		t.Errorf("client trust %q", s.info.ClientTrust)
	}
	tcp := s.url(t, ua.TransportProfileUATCP)
	out := mustRun(t, "get", "value", "-e", tcp, "-n", s.node(serve.DemoInt32), "-q", "--security-policy", "Basic256Sha256",
		"--security-mode", "SignAndEncrypt", "--cert", trusted.certFile, "--key", trusted.keyFile, "--trusted-cert", certOut)
	wantContains(t, out, "42")
	_, errOut, err := runCLI(t, "get", "value", "-e", tcp, "-n", s.node(serve.DemoInt32), "--security-policy", "ECC_nistP256",
		"--security-mode", "SignAndEncrypt", "--cert", untrusted.certFile, "--key", untrusted.keyFile, "--trusted-cert", certOut)
	if err == nil {
		t.Errorf("an untrusted client connected: %s", errOut)
	}
}

// TestE2E_ServeNodeSet imports a NodeSet2 file without the demo namespace.
func TestE2E_ServeNodeSet(t *testing.T) {
	s := startServe(t, "--no-demo", "--nodeset", nodeSetFixture, "--security-policy", "None")
	if s.info.DemoNamespace != "" || len(s.info.Certificates) != 0 {
		t.Errorf("description %+v", s.info)
	}
	idx := -1
	for i, ns := range s.info.Namespaces {
		if ns == "urn:otfabric:opcuactl:test:machine" {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatalf("namespaces %v", s.info.Namespaces)
	}
	tcp := s.url(t, ua.TransportProfileUATCP)
	wantContains(t, mustRun(t, "get", "value", "-e", tcp, "-n", fmt.Sprintf("ns=%d;s=Machine.Speed", idx), "-q"), "1500.5")
	wantContains(t, mustRun(t, "browse", "children", "-e", tcp), "Machine")
}

// TestE2E_ServeOutput prints the server in the other formats.
func TestE2E_ServeOutput(t *testing.T) {
	none := []string{"--listen", "127.0.0.1:0", "--security-policy", "None", "--hostname", "localhost"}
	t.Run("table", func(t *testing.T) {
		dir := t.TempDir()
		s := startServeOutput(t, serveOutput{format: format.Table, verbose: true}, append(none,
			"--security-policy", "Basic256Sha256", "--cert-out", filepath.Join(dir, "c.pem"), "--trusted-cert", newIdentity(t, "rsa", clientURI).certFile,
			"--user", "op:pw", "--allow-anonymous=false")...)
		wantContains(t, s.stdout.String(), "Application URI:  urn:localhost:opcuactl:serve", "Demo namespace:   ns=1",
			"Certificate:      rsa ", "self-signed, generated, "+filepath.Join(dir, "c.pem"), "Client trust:     1 certificate(s)",
			"Users:            op\n", "ENDPOINT URL", "APP URI", "opc.tcp://localhost:")
	})
	t.Run("table without demo", func(t *testing.T) {
		s := startServeOutput(t, serveOutput{format: format.Table}, append(none, "--no-demo")...)
		wantContains(t, s.stdout.String(), "Client trust:     any client certificate", "Users:            anonymous\n")
		if strings.Contains(s.stdout.String(), "Demo namespace") || strings.Contains(s.stdout.String(), "Certificate:") {
			t.Errorf("table:\n%s", s.stdout)
		}
	})
	t.Run("quiet", func(t *testing.T) {
		s := startServeOutput(t, serveOutput{format: format.Table, quiet: true}, none...)
		lines := strings.Split(strings.TrimSpace(s.stdout.String()), "\n")
		if len(lines) != 1 || !strings.HasPrefix(lines[0], "opc.tcp://localhost:") {
			t.Errorf("quiet: %q", lines)
		}
	})
	t.Run("csv", func(t *testing.T) {
		s := startServeOutput(t, serveOutput{format: format.CSV}, none...)
		wantContains(t, s.stdout.String(), "ENDPOINT URL,TRANSPORT")
	})
	t.Run("yaml", func(t *testing.T) {
		s := startServeOutput(t, serveOutput{format: format.YAML}, none...)
		var info model.ServeInfo
		if err := yaml.Unmarshal([]byte(s.stdout.String()), &info); err != nil {
			t.Fatalf("yaml: %v\n%s", err, s.stdout)
		}
		wantContains(t, s.stdout.String(), "endpoints:", "applicationuri: urn:localhost:opcuactl:serve")
	})
	t.Run("log", func(t *testing.T) {
		s := startServeOutput(t, serveOutput{format: format.JSON}, append(none, "--log-level", "debug")...)
		s.stop(t)
		wantContains(t, s.stderr.String(), "level=")
	})
	t.Run("users from the environment", func(t *testing.T) {
		t.Setenv(serveUsersEnv, "a:1,b:2")
		s := startServe(t, "--security-policy", "Basic256Sha256")
		if strings.Join(s.info.Users, ",") != "a,b" {
			t.Errorf("users %v", s.info.Users)
		}
	})
}

// TestE2E_ServeCLI runs serve through the command line for --duration.
func TestE2E_ServeCLI(t *testing.T) {
	out, errOut, err := runCLI(t, "serve", "--listen", "127.0.0.1:0", "--security-policy", "None", "--duration", "300ms", "--format", "json")
	if err != nil {
		t.Fatalf("serve: %v\n%s", err, errOut)
	}
	var info model.ServeInfo
	if err := json.Unmarshal([]byte(out), &info); err != nil || len(info.Endpoints) != 1 {
		t.Fatalf("serve output %v:\n%s", err, out)
	}
	wantContains(t, errOut, "Serving", "Server stopped")

	// The client flags do not apply to serve.
	t.Setenv("OPCUACTL_TLS_CERT", "/nonexistent")
	if _, errOut, err := runCLI(t, "serve", "--listen", "127.0.0.1:0", "--security-policy", "None", "--duration", "10ms", "-q"); err != nil {
		t.Errorf("serve with a client environment: %v\n%s", err, errOut)
	}
}

// TestE2E_ServeFlagValidation checks the flags before anything starts.
func TestE2E_ServeFlagValidation(t *testing.T) {
	dir := t.TempDir()
	srvCert := newIdentity(t, "rsa", serverURI)
	other := newIdentity(t, "p256", serverURI)
	empty := filepath.Join(dir, "empty")
	_ = os.Mkdir(empty, 0o700)
	badDir := filepath.Join(dir, "bad")
	_ = os.Mkdir(badDir, 0o700)
	_ = os.WriteFile(filepath.Join(badDir, "x.pem"), []byte("nope"), 0o600)
	badXML := filepath.Join(dir, "bad.xml")
	_ = os.WriteFile(badXML, []byte("<nope"), 0o600)
	missing := filepath.Join(dir, "missing")

	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--listen", "opc.wss://127.0.0.1:0"}, "invalid --listen"},
		{[]string{"--listen", "127.0.0.1"}, "invalid --listen"},
		{[]string{"--listen", "opc.tcp://127.0.0.1:0/path"}, "no path"},
		{[]string{"--listen", "opc.tcp://%zz"}, "invalid --listen"},
		{[]string{"--listen", "127.0.0.1:port"}, "invalid port"},
		{[]string{"--wss", "nope"}, "invalid --wss"},
		{[]string{"--https", "nope"}, "invalid --https"},
		{[]string{"--uajson"}, "--uajson needs --wss or --https"},
		{[]string{"--tls-cert", srvCert.certFile}, "must be given together"},
		{[]string{"--tls-cert", srvCert.certFile, "--tls-key", srvCert.keyFile}, "give one of them"},
		{[]string{"--wss", "127.0.0.1:0", "--tls-cert", missing, "--tls-key", missing}, "loading --tls-cert"},
		{[]string{"--security-policy", "Nope"}, "invalid --security-policy"},
		{[]string{"--security-mode", "None"}, "the mode None"},
		{[]string{"--security-mode", "Loud"}, "invalid --security-mode"},
		{[]string{"--log-level", "loud"}, "invalid --log-level"},
		{[]string{"--user", "nocolon"}, "invalid --user"},
		{[]string{"--allow-anonymous=false"}, "nobody could log in"},
		{[]string{"--user", "a:b", "--security-policy", "None"}, "--user needs a secure"},
		{[]string{"--cert", srvCert.certFile}, "in pairs"},
		{[]string{"--cert", missing, "--key", srvCert.keyFile}, "--cert:"},
		{[]string{"--cert", srvCert.certFile, "--key", missing}, "--key:"},
		{[]string{"--cert", srvCert.certFile, "--key", other.keyFile}, "no certificate for the ecc-P-256"},
		{[]string{"--security-policy", "ECC_brainpoolP256r1"}, "needs a certificate of its key type"},
		{[]string{"--sim-interval", "0s"}, "--sim-interval must be positive"},
		{[]string{"--duration", "-1s"}, "--duration must not be negative"},
		{[]string{"--trusted-cert", missing}, "--trusted-cert:"},
		{[]string{"--trusted-cert", badXML}, "--trusted-cert " + badXML},
		{[]string{"--trust-dir", missing}, "--trust-dir:"},
		{[]string{"--trust-dir", empty}, "has no certificates"},
		{[]string{"--trust-dir", badDir}, "--trust-dir " + filepath.Join(badDir, "x.pem")},
		{[]string{"--nodeset", missing}, "--nodeset:"},
		{[]string{"--nodeset", badXML}, "importing " + badXML},
		{[]string{"--cert-out", filepath.Join(missing, "c.pem")}, "--cert-out:"},
		{[]string{"--key-out", filepath.Join(missing, "c.key")}, "--key-out:"},
		{[]string{"--key-out", filepath.Join(dir, "k.pem"), "--cert", other.certFile, "--key", other.keyFile, "--security-policy", "ECC_nistP256"}, ""},
		{[]string{"--cert-out", filepath.Join(dir, "none.pem"), "--security-policy", "None"}, ""},
	} {
		args := append([]string{"serve", "--listen", "127.0.0.1:0", "--duration", "1ms"}, tc.args...)
		_, errOut, err := runCLI(t, args...)
		if tc.want == "" {
			if err != nil {
				t.Errorf("%v: %v %s", tc.args, err, errOut)
			}
			continue
		}
		if err == nil || ExitCode(err) != 1 || !strings.Contains(errOut, tc.want) {
			t.Errorf("%v: exit %d, stderr %q; want %q", tc.args, ExitCode(err), errOut, tc.want)
		}
	}

	// --cert-out makes a certificate for a server without security.
	if b, err := os.ReadFile(filepath.Join(dir, "none.pem")); err != nil || !strings.Contains(string(b), "CERTIFICATE") {
		t.Errorf("--cert-out with --security-policy None: %v %q", err, b)
	}

	t.Run("port in use", func(t *testing.T) {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = l.Close() }()
		_, errOut, err := runCLI(t, "serve", "--listen", l.Addr().String(), "--security-policy", "None")
		if ExitCode(err) != 2 || !strings.Contains(errOut, "starting the server") {
			t.Errorf("exit %d: %s", ExitCode(err), errOut)
		}
	})
}

// TestE2E_ServeSignals stops `opcuactl serve` in a child process with
// SIGTERM and SIGINT while a client is connected.
func TestE2E_ServeSignals(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no SIGTERM / SIGINT on Windows")
	}
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGINT} {
		t.Run(sig.String(), func(t *testing.T) {
			p := startCLI(t, "serve", "--listen", "127.0.0.1:0", "--security-policy", "None", "-q")
			p.waitStderr(t, "Serving")
			p.waitStdout(t, "\n")
			endpoint := strings.TrimSpace(p.stdout.String())

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			c, err := opcua.NewClient(endpoint, opcua.InsecureNoSecurity())
			if err != nil {
				t.Fatal(err)
			}
			if err := c.Connect(ctx); err != nil {
				t.Fatalf("connecting to %s: %v", endpoint, err)
			}
			defer func() { _ = c.Close(context.Background()) }()

			if err := p.cmd.Process.Signal(sig); err != nil {
				t.Fatal(err)
			}
			p.wait(t)
			if p.err != nil {
				t.Errorf("serve exited with %v\nstderr: %s", p.err, p.stderr)
			}
			wantContains(t, p.stderr.String(), "Server stopped")
		})
	}
}

func TestServeParseLogLevel(t *testing.T) {
	for in, want := range map[string]string{"debug": "DEBUG", "info": "INFO", "WARN": "WARN", "warning": "WARN", "error": "ERROR"} {
		l, err := parseLogLevel(in)
		if err != nil || l == nil || l.String() != want {
			t.Errorf("parseLogLevel(%q) = %v, %v", in, l, err)
		}
	}
	for _, in := range []string{"off", "none"} {
		if l, err := parseLogLevel(in); l != nil || err != nil {
			t.Errorf("parseLogLevel(%q) = %v, %v", in, l, err)
		}
	}
	if servePolicyValues[len(servePolicyValues)-1] == "auto" {
		t.Error("serve offers the security policy auto")
	}
	if got := certificateURI([]byte("nope")); got != "" {
		t.Errorf("certificateURI of no certificate = %q", got)
	}
}

// Without --insecure-no-security serve offers no None endpoint, a client
// connects to a None endpoint only with it, and discovery needs no opt-in.
func TestE2E_NoSecurityIsAChoice(t *testing.T) {
	t.Setenv("OPCUACTL_INSECURE_NO_SECURITY", "")
	if got, err := servePolicies(nil, false); err != nil || len(got) != 1 || got[0] == ua.SecurityPolicyURINone {
		t.Errorf("default policies: %v, %v", got, err)
	}
	if got, err := servePolicies(nil, true); err != nil || len(got) != 2 || got[0] != ua.SecurityPolicyURINone {
		t.Errorf("default policies with --insecure-no-security: %v, %v", got, err)
	}
	if _, err := servePolicies([]string{"None"}, false); err == nil || !strings.Contains(err.Error(), "--insecure-no-security") {
		t.Errorf("--security-policy None: %v", err)
	}

	s := startServe(t, "--security-policy", "None", "--log-level", "off")
	ep := s.url(t, ua.TransportProfileUATCP)
	if _, errOut, err := runCLI(t, "get", "value", "-e", ep, "-n", "i=2258"); err == nil || !strings.Contains(errOut, "--insecure-no-security") {
		t.Errorf("connect without the opt-in: %v, %s", err, errOut)
	}
	if _, errOut, err := runCLI(t, "discover", "endpoints", "-e", ep); err != nil {
		t.Errorf("discovery without the opt-in: %v, %s", err, errOut)
	}
	if _, errOut, err := runCLI(t, "get", "value", "-e", ep, "-n", "i=2258", "--insecure-no-security"); err != nil {
		t.Errorf("connect with the opt-in: %v, %s", err, errOut)
	}
}
