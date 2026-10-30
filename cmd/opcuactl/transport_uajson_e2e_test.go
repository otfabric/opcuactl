// SPDX-License-Identifier: MIT

package opcuactl

import (
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/otfabric/go-opcua/server"
	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/go-opcua/uasc"
)

// TestE2E_UAJSON runs commands over the UA JSON mappings of opc.wss and
// opc.https (--transport wss-uajson / https-uajson) against a server with
// server.EnableUAJSON.
func TestE2E_UAJSON(t *testing.T) {
	s := startTLSServer(t, server.EnableUAJSON())
	ca := []string{"--tls-ca", s.cert.certFile}

	for _, tc := range []struct{ name, ep, profile string }{
		{"wss-uajson", s.wss, ua.TransportProfileWSSJSON},
		{"https-uajson", s.https, ua.TransportProfileHTTPSJSON},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := func(a ...string) []string { return append(append(a, "-e", tc.ep, "--transport", tc.name), ca...) }

			out := mustRun(t, args("get", "value", "-n", s.node("Int32"), "-q")...)
			wantContains(t, out, "42")

			out = mustRun(t, args("write", "value", "-n", s.node("String"), "--value", "over "+tc.name, "--yes")...)
			wantContains(t, out, "Good")
			out = mustRun(t, args("get", "value", "-n", s.node("String"), "-q")...)
			wantContains(t, out, "over "+tc.name)

			out = mustRun(t, args("browse", "children", "-n", "i=85")...)
			wantContains(t, out, "Server")

			out = mustRun(t, args("monitor", "value", "-n", s.node("Int32"), "--count", "1", "--duration", "20s", "--format", "json")...)
			wantContains(t, out, `"value":42`)

			out = mustRun(t, args("ping", "--format", "json")...)
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

			// --best picks among the endpoints of --transport.
			out = mustRun(t, args("discover", "endpoints", "--best", "--format", "json")...)
			wantContains(t, out, `"transportProfile":"`+tc.profile+`"`)

			// The listing names the JSON endpoints; the filter keeps them.
			out = mustRun(t, append([]string{"discover", "endpoints", "-e", tc.ep}, ca...)...)
			wantContains(t, out, strings.SplitN(tc.ep, ":", 2)[0]+" (json)")
			out = mustRun(t, append([]string{"discover", "endpoints", "-e", tc.ep, "--transport-profile", tc.name, "--format", "json"}, ca...)...)
			var eps []struct {
				TransportProfile string `json:"transportProfile"`
				SecurityPolicy   string `json:"securityPolicy"`
			}
			if err := json.Unmarshal([]byte(out), &eps); err != nil {
				t.Fatalf("json: %v\n%s", err, out)
			}
			if len(eps) == 0 {
				t.Fatalf("--transport-profile %s listed nothing", tc.name)
			}
			for _, ep := range eps {
				if ep.TransportProfile != tc.profile {
					t.Errorf("--transport-profile %s listed %+v", tc.name, ep)
				}
			}
		})
	}

	t.Run("security auto selects within the transport", func(t *testing.T) {
		cli := newIdentity(t, "rsa", clientURI)
		sec := []string{"--security-policy", "auto", "--security-mode", "auto",
			"--cert", cli.certFile, "--key", cli.keyFile, "--trusted-cert", s.cert.certFile}
		// https-uajson: the https policies sign the session.
		out := mustRun(t, append(append([]string{"get", "server", "-e", s.https, "--transport", "https-uajson", "--format", "json"}, sec...), ca...)...)
		wantContains(t, out, "Basic256Sha256")
		// wss-uajson is None only.
		out = mustRun(t, append(append([]string{"get", "value", "-e", s.wss, "--transport", "wss-uajson", "-n", s.node("Int32"), "-q"}, sec...), ca...)...)
		wantContains(t, out, "42")
		_, errOut, err := runCLI(t, append([]string{"get", "value", "-e", s.wss, "--transport", "wss-uajson", "-n", s.node("Int32"),
			"--security-policy", "Basic256Sha256", "--security-mode", "SignAndEncrypt", "--cert", cli.certFile, "--key", cli.keyFile}, ca...)...)
		if err == nil {
			t.Errorf("wss-uajson with Basic256Sha256 connected: %s", errOut)
		}
	})

	t.Run("environment", func(t *testing.T) {
		t.Setenv("OPCUACTL_TLS_CA", s.cert.certFile)
		t.Setenv("OPCUACTL_ENDPOINT", s.https)
		t.Setenv("OPCUACTL_TRANSPORT", "https-uajson")
		t.Setenv("OPCUACTL_JSON_CONTENT_TYPE", "legacy")
		wantContains(t, mustRun(t, "get", "value", "-n", s.node("Int32"), "-q"), "42")

		t.Setenv("OPCUACTL_JSON_CONTENT_TYPE", "text/plain")
		if _, _, err := runCLI(t, "get", "value", "-n", s.node("Int32")); err == nil || ExitCode(err) != 2 {
			t.Errorf("an invalid OPCUACTL_JSON_CONTENT_TYPE: %v", err)
		}
		t.Setenv("OPCUACTL_JSON_CONTENT_TYPE", "")
		t.Setenv("OPCUACTL_TRANSPORT", "wss-uajson")
		if _, errOut, err := runCLI(t, "get", "value", "-n", s.node("Int32")); err == nil || !strings.Contains(errOut, "needs an opc.wss:// endpoint") {
			t.Errorf("OPCUACTL_TRANSPORT wss-uajson with an opc.https endpoint: %v %s", err, errOut)
		}
	})

	t.Run("flag validation", func(t *testing.T) {
		for _, tc := range []struct {
			args []string
			want string
		}{
			{[]string{"-e", s.url, "--transport", "wss-uajson"}, "needs an opc.wss:// endpoint"},
			{[]string{"-e", s.wss, "--transport", "https-uajson"}, "needs an opc.https:// endpoint"},
			{[]string{"-e", s.https, "--transport", "tcp"}, "needs an opc.tcp:// endpoint"},
			{[]string{"-e", s.wss, "--transport", "carrier-pigeon"}, "invalid --transport"},
			{[]string{"-e", s.wss, "--transport", "http://example.com/UA-Profile/Transport/pigeon"}, "not a transport this client implements"},
			{[]string{"--reverse-listen", "localhost:0", "--transport", "wss-uajson"}, "reverse connect"},
			{[]string{"-e", s.https, "--transport", "https-uajson", "--json-content-type", "xml"}, "invalid --json-content-type"},
		} {
			_, errOut, err := runCLI(t, append([]string{"get", "value", "-n", "i=2258"}, tc.args...)...)
			if err == nil || ExitCode(err) != 2 || !strings.Contains(errOut, tc.want) {
				t.Errorf("%v: exit %d, stderr %s; want %q", tc.args, ExitCode(err), errOut, tc.want)
			}
		}
	})
}

// TestE2E_UAJSON_ServerWithout shows that --transport selects the JSON
// mapping: a server without EnableUAJSON does not take it, while the same
// URL works with UA Binary.
func TestE2E_UAJSON_ServerWithout(t *testing.T) {
	s := startTLSServer(t)
	ca := []string{"--tls-ca", s.cert.certFile}
	for _, tc := range []struct{ ep, transport string }{{s.wss, "wss-uajson"}, {s.https, "https-uajson"}} {
		if _, _, err := runCLI(t, append([]string{"get", "value", "-e", tc.ep, "--transport", tc.transport, "-n", s.node("Int32"), "--timeout", "5s"}, ca...)...); err == nil {
			t.Errorf("%s over %s: a server without UA JSON answered", tc.transport, tc.ep)
		}
		// Asking for a JSON endpoint the server does not offer.
		_, errOut, err := runCLI(t, append([]string{"discover", "endpoints", "-e", tc.ep, "--best", "--transport-profile", tc.transport}, ca...)...)
		if err == nil || !strings.Contains(errOut, "(json) endpoint") {
			t.Errorf("--best --transport-profile %s: %v %s", tc.transport, err, errOut)
		}
	}
	wantContains(t, mustRun(t, append([]string{"get", "value", "-e", s.wss, "--transport", "wss", "-n", s.node("Int32"), "-q"}, ca...)...), "42")
}

// TestE2E_UAJSON_ContentType checks the Content-Type of https-uajson
// requests: application/json by default, application/opcua+uajson with
// --json-content-type legacy (UA-.NETStandard).
func TestE2E_UAJSON_ContentType(t *testing.T) {
	s := startTLSServer(t, server.EnableUAJSON())
	target, err := url.Parse("https://" + strings.TrimPrefix(s.https, "opc.https://"))
	if err != nil {
		t.Fatal(err)
	}
	var (
		mu    sync.Mutex
		types []string
	)
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}} //nolint:gosec // test proxy to the test server
	front := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		types = append(types, r.Header.Get("Content-Type"))
		mu.Unlock()
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(front.Close)
	ep := "opc.https://" + strings.TrimPrefix(front.URL, "https://")

	seen := func() []string {
		mu.Lock()
		defer mu.Unlock()
		out := types
		types = nil
		return out
	}
	for _, tc := range []struct{ flag, want string }{
		{"", uasc.HTTPSJSONContentType},
		{"standard", uasc.HTTPSJSONContentType},
		{"legacy", uasc.HTTPSJSONContentTypeLegacy},
		{uasc.HTTPSJSONContentTypeLegacy, uasc.HTTPSJSONContentTypeLegacy},
	} {
		a := []string{"get", "value", "-e", ep, "--transport", "https-uajson", "-n", s.node("Int32"), "-q", "--tls-insecure-skip-verify"}
		if tc.flag != "" {
			a = append(a, "--json-content-type", tc.flag)
		}
		wantContains(t, mustRun(t, a...), "42")
		got := seen()
		if len(got) == 0 {
			t.Fatalf("--json-content-type %q: no request through the proxy", tc.flag)
		}
		for _, ct := range got {
			if ct != tc.want {
				t.Errorf("--json-content-type %q: Content-Type %q, want %q", tc.flag, ct, tc.want)
			}
		}
	}
}
