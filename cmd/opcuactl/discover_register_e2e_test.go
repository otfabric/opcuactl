// SPDX-License-Identifier: MIT

package opcuactl

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/opcuactl/internal/format"
	"github.com/otfabric/opcuactl/internal/model"
)

// registeredURIs returns the ApplicationUris FindServers returns at url.
func registeredURIs(t *testing.T, url string) []string {
	t.Helper()
	var servers []model.DiscoveredServer
	out := mustRun(t, "discover", "servers", "-e", url, "--format", "json")
	if err := json.Unmarshal([]byte(out), &servers); err != nil {
		t.Fatalf("discover servers: %v\n%s", err, out)
	}
	var uris []string
	for _, s := range servers {
		uris = append(uris, s.ApplicationURI)
	}
	return uris
}

func containsURI(uris []string, uri string) bool {
	for _, u := range uris {
		if u == uri {
			return true
		}
	}
	return false
}

// TestE2E_DiscoverRegister registers a server with `opcuactl serve --lds`
// over a secured channel, and unregisters it.
func TestE2E_DiscoverRegister(t *testing.T) {
	dir := t.TempDir()
	certOut := filepath.Join(dir, "lds.pem")
	lds := startServe(t, "--lds", "--no-demo", "--cert-out", certOut, "--application-uri", "urn:otfabric:opcuactl:test:lds")
	if !lds.info.LDS {
		t.Fatalf("serve --lds: lds not in %+v", lds.info)
	}
	url := lds.url(t, "http://opcfoundation.org/UA-Profile/Transport/uatcp-uasc-uabinary")

	const registrant = "urn:otfabric:opcuactl:test:registrant"
	id := newIdentity(t, "rsa", registrant)
	secure := []string{"--security-policy", "Basic256Sha256", "--security-mode", "SignAndEncrypt",
		"--cert", id.certFile, "--key", id.keyFile, "--trusted-cert", certOut}
	register := func(extra ...string) []string {
		return append(append([]string{"discover", "register", "-e", url, "--discovery-url", "opc.tcp://registrant.example:4841"}, secure...), extra...)
	}

	t.Run("register", func(t *testing.T) {
		out := mustRun(t, register("--server-name", "en:Registrant", "--capability", "DA")...)
		wantContains(t, out, "Registered "+registrant, "RegisterServer2", "Basic256Sha256", "SignAndEncrypt")
		if uris := registeredURIs(t, url); !containsURI(uris, registrant) {
			t.Errorf("FindServers after register: %v", uris)
		}
		if !strings.Contains(lds.stderr.String(), registrant) {
			t.Errorf("serve --lds did not log the registration:\n%s", lds.stderr)
		}
	})

	t.Run("legacy json", func(t *testing.T) {
		out := mustRun(t, register("--legacy", "--format", "json")...)
		var res model.RegisterResult
		if err := json.Unmarshal([]byte(out), &res); err != nil {
			t.Fatalf("json: %v\n%s", err, out)
		}
		if res.Service != "RegisterServer" || res.ServerURI != registrant || !res.Online || res.Status != "Good" {
			t.Errorf("result %+v", res)
		}
	})

	t.Run("offline", func(t *testing.T) {
		out := mustRun(t, register("--offline")...)
		wantContains(t, out, "Unregistered "+registrant)
		if uris := registeredURIs(t, url); containsURI(uris, registrant) {
			t.Errorf("FindServers after --offline: %v", uris)
		}
	})

	t.Run("another server uri is refused", func(t *testing.T) {
		_, errOut, err := runCLI(t, register("--server-uri", "urn:someone:else")...)
		if err == nil || ExitCode(err) != exitProtocol {
			t.Fatalf("exit %d (%v), want %d", ExitCode(err), err, exitProtocol)
		}
		wantContains(t, errOut, "hint:")
	})

	t.Run("insecure is refused", func(t *testing.T) {
		_, _, err := runCLI(t, "discover", "register", "-e", url, "--discovery-url", "opc.tcp://x:1",
			"--server-uri", registrant, "--security-policy", "None", "--insecure-no-security")
		if err == nil || ExitCode(err) != exitProtocol {
			t.Fatalf("exit %d (%v), want %d", ExitCode(err), err, exitProtocol)
		}
	})
}

// TestE2E_DiscoverRegisterInsecure registers without security with an LDS
// that allows it.
func TestE2E_DiscoverRegisterInsecure(t *testing.T) {
	lds := startServe(t, "--lds", "--lds-allow-insecure-registration", "--no-demo", "--application-uri", "urn:otfabric:opcuactl:test:lds2")
	url := lds.url(t, "http://opcfoundation.org/UA-Profile/Transport/uatcp-uasc-uabinary")
	const uri = "urn:otfabric:opcuactl:test:insecure"
	out := mustRun(t, "discover", "register", "-e", url, "--discovery-url", "opc.tcp://x:4841", "--server-uri", uri,
		"--security-policy", "None", "--insecure-no-security", "--server-type", "clientandserver", "--mdns-name", "insecure")
	wantContains(t, out, "Registered "+uri, "None")
	if uris := registeredURIs(t, url); !containsURI(uris, uri) {
		t.Errorf("FindServers: %v", uris)
	}
}

// TestE2E_ServeRegisterWith runs one serve that registers with another.
func TestE2E_ServeRegisterWith(t *testing.T) {
	lds := startServe(t, "--lds", "--no-demo", "--application-uri", "urn:otfabric:opcuactl:test:lds3")
	url := lds.url(t, "http://opcfoundation.org/UA-Profile/Transport/uatcp-uasc-uabinary")
	const uri = "urn:otfabric:opcuactl:test:registered-serve"
	srv := startServe(t, "--no-demo", "--application-uri", uri, "--register-with", url, "--register-interval", "1m")
	if len(srv.info.RegisteredWith) != 1 || srv.info.RegisteredWith[0] != url {
		t.Fatalf("registeredWith %v", srv.info.RegisteredWith)
	}
	deadline := time.Now().Add(30 * time.Second)
	for !containsURI(registeredURIs(t, url), uri) {
		if time.Now().After(deadline) {
			t.Fatalf("serve --register-with did not register\nstderr: %s", srv.stderr)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !strings.Contains(srv.stderr.String(), "registered") {
		t.Errorf("no registration in the log:\n%s", srv.stderr)
	}
}

// TestE2E_ServeDiscoveryTable prints the discovery lines of serve.
func TestE2E_ServeDiscoveryTable(t *testing.T) {
	t.Parallel()
	s := startServeOutput(t, serveOutput{format: format.Table}, "--listen", "127.0.0.1:0", "--no-demo", "--lds",
		"--node-management", "--register-with", "opc.tcp://127.0.0.1:1", "--register-insecure")
	wantContains(t, s.stdout.String(), "Discovery:        Local Discovery Server", "Registers with:   opc.tcp://127.0.0.1:1", "Node management:")
}

// TestDiscoverRegisterFlags checks the flags before a connection.
func TestDiscoverRegisterFlags(t *testing.T) {
	dir := t.TempDir()
	noURI := newIdentity(t, "rsa", "urn:x")
	for _, c := range []struct {
		name string
		args []string
		want string
	}{
		{"no endpoint", []string{"--discovery-url", "opc.tcp://x:1", "--server-uri", "urn:x"}, "--endpoint"},
		{"no discovery url", []string{"-e", deadEndpoint, "--server-uri", "urn:x"}, "--discovery-url"},
		{"no server uri", []string{"-e", deadEndpoint, "--discovery-url", "opc.tcp://x:1"}, "--server-uri"},
		{"legacy with mdns", []string{"-e", deadEndpoint, "--discovery-url", "opc.tcp://x:1", "--server-uri", "urn:x", "--legacy", "--capability", "DA"}, "--legacy"},
		{"client type", []string{"-e", deadEndpoint, "--discovery-url", "opc.tcp://x:1", "--server-uri", "urn:x", "--server-type", "client"}, "--server-type"},
		{"missing cert", []string{"-e", deadEndpoint, "--discovery-url", "opc.tcp://x:1", "--cert", filepath.Join(dir, "none"), "--key", noURI.keyFile}, "--cert"},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, _, err := runCLI(t, append([]string{"discover", "register"}, c.args...)...)
			code := exitUsage
			if c.name == "missing cert" {
				code = exitInvalidInput
			}
			if err == nil || ExitCode(err) != code || !strings.Contains(err.Error(), c.want) {
				t.Errorf("exit %d, %v; want %d with %q", ExitCode(err), err, code, c.want)
			}
		})
	}
	// The ServerUri of --cert is the default.
	_, _, err := runCLI(t, "discover", "register", "-e", deadEndpoint, "--discovery-url", "opc.tcp://x:1", "--cert", noURI.certFile, "--key", noURI.keyFile)
	if err == nil || ExitCode(err) != exitTransport {
		t.Errorf("with the URI of --cert: exit %d (%v), want %d", ExitCode(err), err, exitTransport)
	}
}
