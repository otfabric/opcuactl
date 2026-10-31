// SPDX-License-Identifier: MIT

package opcuactl

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-opcua"
	"github.com/otfabric/go-opcua/id"
	"github.com/otfabric/go-opcua/server"
	"github.com/otfabric/go-opcua/server/attrs"
	"github.com/otfabric/go-opcua/ua"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// These tests run the CLI in-process against a go-opcua server. The commands
// keep their flags in package variables and write to os.Stdout, so the tests
// are serialized and reset the flags before every run.

var cliMu sync.Mutex

// runCLI executes opcuactl with args and returns what it wrote.
func runCLI(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	cliMu.Lock()
	defer cliMu.Unlock()

	resetFlags(rootCmd)

	capture := func(target **os.File) func() string {
		r, w, pipeErr := os.Pipe()
		if pipeErr != nil {
			t.Fatalf("pipe: %v", pipeErr)
		}
		orig := *target
		*target = w
		done := make(chan string, 1)
		go func() {
			var buf bytes.Buffer
			_, _ = io.Copy(&buf, r)
			done <- buf.String()
		}()
		return func() string {
			_ = w.Close()
			*target = orig
			return <-done
		}
	}
	stopOut := capture(&os.Stdout)
	stopErr := capture(&os.Stderr)

	rootCmd.SetArgs(args)
	err = Execute()

	return stopOut(), stopErr(), err
}

func resetFlags(cmd *cobra.Command) {
	reset := func(f *pflag.Flag) {
		if sv, ok := f.Value.(pflag.SliceValue); ok {
			_ = sv.Replace(nil)
		} else {
			_ = f.Value.Set(f.DefValue)
		}
		f.Changed = false
	}
	cmd.Flags().VisitAll(reset)
	cmd.PersistentFlags().VisitAll(reset)
	for _, sub := range cmd.Commands() {
		resetFlags(sub)
	}
}

const testNamespace = "urn:otfabric:opcuactl:test"

// testServer is a running server with a few variables in namespace nsIndex.
type testServer struct {
	srv *server.Server
	ns  *server.NodeNameSpace
	url string
}

func (s *testServer) node(name string) string {
	return fmt.Sprintf("ns=%d;s=%s", s.ns.ID(), name)
}

func startServer(t *testing.T, opts ...server.Option) *testServer {
	t.Helper()
	return startServerWith(t, nil, opts...)
}

// startServerWith is startServer with a hook that runs before Start.
func startServerWith(t *testing.T, beforeStart func(*server.Server), opts ...server.Option) *testServer {
	t.Helper()
	all := append([]server.Option{
		server.EndPoint("localhost", 0),
		server.InsecureNoSecurity(),
		server.EnableAuthMode(ua.UserTokenTypeAnonymous),
	}, opts...)
	srv, err := server.New(all...)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

	root, err := srv.Namespace(0)
	if err != nil {
		t.Fatalf("namespace 0: %v", err)
	}
	ns := server.NewNodeNameSpace(srv, testNamespace)
	obj := ns.Objects()
	root.Objects().AddRef(obj, id.HasComponent, true)
	add := func(name string, value any) *server.Node {
		n := ns.AddNewVariableStringNode(name, value)
		obj.AddRef(n, id.HasComponent, true)
		return n
	}
	add("Double", float64(100))
	add("Int32", int32(42))
	add("String", "hello")
	add("Int32Array", []int32{1, 2, 3, 4, 5})

	// AnalogItemType variable with EURange [0, 200] for percent deadbands.
	analog := add("Analog", float64(100))
	if typ := srv.Node(ua.NewNumericNodeID(0, id.AnalogItemType)); typ != nil {
		analog.AddRef(typ, id.HasTypeDefinition, true)
	}
	rng := &ua.DataValue{
		EncodingMask: ua.DataValueValue,
		Value:        ua.MustVariant(ua.NewExtensionObject(&ua.Range{Low: 0, High: 200})),
	}
	prop := server.NewNode(
		ua.NewStringNodeID(ns.ID(), "Analog.EURange"),
		map[ua.AttributeID]*ua.DataValue{
			ua.AttributeIDNodeClass:   server.DataValueFromValue(uint32(ua.NodeClassVariable)),
			ua.AttributeIDBrowseName:  server.DataValueFromValue(attrs.BrowseName("EURange")),
			ua.AttributeIDDisplayName: server.DataValueFromValue(attrs.DisplayName("EURange", "EURange")),
			ua.AttributeIDDataType:    server.DataValueFromValue(ua.NewNumericExpandedNodeID(0, id.Range)),
		},
		nil,
		func() *ua.DataValue { return rng },
	)
	ns.AddNode(prop)
	analog.AddRef(prop, id.HasProperty, true)

	if beforeStart != nil {
		beforeStart(srv)
	}
	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("server start: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	return &testServer{srv: srv, ns: ns, url: fmt.Sprintf("opc.tcp://localhost:%d", srv.Port())}
}

// identity is a self-signed application instance certificate on disk.
type identity struct {
	der      []byte
	key      crypto.Signer
	certFile string
	keyFile  string
}

func newIdentity(t *testing.T, kind, appURI string) *identity {
	t.Helper()
	var key crypto.Signer
	var err error
	switch kind {
	case "rsa":
		key, err = rsa.GenerateKey(rand.Reader, 2048)
	case "p256":
		key, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	case "ed25519":
		_, key, err = ed25519.GenerateKey(rand.Reader)
	default:
		t.Fatalf("unknown key kind %q", kind)
	}
	if err != nil {
		t.Fatalf("generating %s key: %v", kind, err)
	}

	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	u, _ := url.Parse(appURI)
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "opcuactl test " + kind, Organization: []string{"otfabric"}},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageContentCommitment |
			x509.KeyUsageKeyEncipherment | x509.KeyUsageDataEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		URIs:                  []*url.URL{u},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, key.Public(), key)
	if err != nil {
		t.Fatalf("creating %s certificate: %v", kind, err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshalling %s key: %v", kind, err)
	}

	dir := t.TempDir()
	id := &identity{
		der:      der,
		key:      key,
		certFile: filepath.Join(dir, kind+"-cert.pem"),
		keyFile:  filepath.Join(dir, kind+"-key.pem"),
	}
	writePEM(t, id.certFile, "CERTIFICATE", der)
	writePEM(t, id.keyFile, "PRIVATE KEY", keyDER)
	return id
}

func writePEM(t *testing.T, file, typ string, der []byte) {
	t.Helper()
	if err := os.WriteFile(file, pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der}), 0o600); err != nil {
		t.Fatalf("writing %s: %v", file, err)
	}
}

func mustRun(t *testing.T, args ...string) string {
	t.Helper()
	out, errOut, err := runCLI(t, args...)
	if err != nil {
		t.Fatalf("opcuactl %s: %v\nstderr: %s", strings.Join(args, " "), err, errOut)
	}
	return out
}

func wantContains(t *testing.T, out string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("output does not contain %q:\n%s", w, out)
		}
	}
}

const (
	serverURI = "urn:otfabric:opcuactl:test:server"
	clientURI = "urn:otfabric:opcuactl:test:client"
)

// TestE2E_ECCSecurityPolicies connects over the ECC policies of OPC UA 1.05
// with ECDSA and Ed25519 certificates given on the command line.
func TestE2E_ECCSecurityPolicies(t *testing.T) {
	srvRSA := newIdentity(t, "rsa", serverURI)
	srvP256 := newIdentity(t, "p256", serverURI)
	srvEd := newIdentity(t, "ed25519", serverURI)
	s := startServer(t,
		server.ApplicationIdentity(srvRSA.der, srvRSA.key),
		server.ApplicationIdentity(srvP256.der, srvP256.key),
		server.ApplicationIdentity(srvEd.der, srvEd.key),
		server.EnableAuthMode(ua.UserTokenTypeCertificate),
		server.WithX509UserValidator(func([]byte) error { return nil }),
		server.EnableSecurity("Basic256Sha256", ua.MessageSecurityModeSignAndEncrypt),
		server.EnableSecurity("ECC_nistP256", ua.MessageSecurityModeSignAndEncrypt),
		server.EnableSecurity("ECC_nistP256_AesGcm", ua.MessageSecurityModeSignAndEncrypt),
		server.EnableSecurity("ECC_nistP256_ChaChaPoly", ua.MessageSecurityModeSign),
		server.EnableSecurity("ECC_curve25519", ua.MessageSecurityModeSignAndEncrypt),
	)

	cliRSA := newIdentity(t, "rsa", clientURI)
	cliP256 := newIdentity(t, "p256", clientURI)
	cliEd := newIdentity(t, "ed25519", clientURI)

	for _, tc := range []struct {
		policy, mode string
		client       *identity
		serverCert   *identity
	}{
		{"ECC_nistP256", "SignAndEncrypt", cliP256, srvP256},
		{"ECC_nistP256_AesGcm", "SignAndEncrypt", cliP256, srvP256},
		{"ECC_nistP256_ChaChaPoly", "Sign", cliP256, srvP256},
		{"ECC_curve25519", "SignAndEncrypt", cliEd, srvEd},
		{"Basic256Sha256", "SignAndEncrypt", cliRSA, srvRSA},
	} {
		t.Run(tc.policy, func(t *testing.T) {
			out := mustRun(t, "get", "server", "-e", s.url, "--format", "json",
				"--security-policy", tc.policy, "--security-mode", tc.mode,
				"--cert", tc.client.certFile, "--key", tc.client.keyFile,
				"--trusted-cert", tc.serverCert.certFile)
			wantContains(t, out, tc.policy, tc.mode)
		})
	}

	t.Run("auto selects by key type", func(t *testing.T) {
		// The ECC endpoints rank highest, but an RSA certificate cannot use them.
		for _, tc := range []struct {
			client     *identity
			serverCert *identity
			want       string
		}{
			{cliRSA, srvRSA, "Basic256Sha256"},
			{cliP256, srvP256, "ECC_nistP256"},
			{cliEd, srvEd, "ECC_curve25519"},
		} {
			out := mustRun(t, "get", "server", "-e", s.url, "--format", "json",
				"--security-policy", "auto", "--security-mode", "auto",
				"--cert", tc.client.certFile, "--key", tc.client.keyFile,
				"--trusted-cert", tc.serverCert.certFile)
			wantContains(t, out, tc.want)

			best := mustRun(t, "discover", "endpoints", "-e", s.url, "--best", "--format", "json",
				"--cert", tc.client.certFile, "--key", tc.client.keyFile)
			wantContains(t, best, tc.want)
		}
	})

	t.Run("discover lists ECC endpoints", func(t *testing.T) {
		out := mustRun(t, "discover", "endpoints", "-e", s.url)
		wantContains(t, out, "ECC_nistP256_AesGcm", "ECC_nistP256_ChaChaPoly", "ECC_curve25519", "Basic256Sha256")
		out = mustRun(t, "discover", "endpoints", "-e", s.url, "--policy", "ECC_curve25519", "--format", "json")
		wantContains(t, out, "ECC_curve25519")
		if strings.Contains(out, "Basic256Sha256") {
			t.Errorf("--policy filter leaked other policies:\n%s", out)
		}
	})

	t.Run("ECDSA user certificate", func(t *testing.T) {
		out := mustRun(t, "get", "value", "-e", s.url, "-n", s.node("Int32"), "-q",
			"--auth", "certificate",
			"--security-policy", "ECC_nistP256", "--security-mode", "SignAndEncrypt",
			"--cert", cliP256.certFile, "--key", cliP256.keyFile,
			"--trusted-cert", srvP256.certFile)
		wantContains(t, out, "42")
	})
}

// freeAddr returns a loopback address nobody listens on.
func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = l.Close() }()
	return l.Addr().String()
}

// TestE2E_ReverseConnect lets the server connect to the CLI.
func TestE2E_ReverseConnect(t *testing.T) {
	listen := freeAddr(t)
	s := startServer(t,
		server.ApplicationURI(serverURI),
		server.WithReverseConnect("opc.tcp://"+listen,
			server.ReverseConnectRetryInterval(50*time.Millisecond, 200*time.Millisecond)),
	)
	reverse := []string{"--reverse-listen", listen, "--timeout", "30s"}

	t.Run("get value with endpoint", func(t *testing.T) {
		out := mustRun(t, append([]string{"get", "value", "-e", s.url, "-n", s.node("Int32"), "-q"}, reverse...)...)
		wantContains(t, out, "42")
	})
	t.Run("get value without endpoint", func(t *testing.T) {
		out := mustRun(t, append([]string{"get", "value", "-n", s.node("String"), "-q"}, reverse...)...)
		wantContains(t, out, "hello")
	})
	t.Run("server uri", func(t *testing.T) {
		out := mustRun(t, append([]string{"get", "value", "-n", s.node("Int32"), "-q", "--reverse-server-uri", serverURI}, reverse...)...)
		wantContains(t, out, "42")

		_, _, err := runCLI(t, "get", "value", "-n", s.node("Int32"),
			"--reverse-listen", listen, "--reverse-server-uri", "urn:someone:else", "--timeout", "1s")
		if err == nil {
			t.Fatal("a server announcing another ServerUri was accepted")
		}
	})
	t.Run("discover endpoints", func(t *testing.T) {
		out := mustRun(t, append([]string{"discover", "endpoints"}, reverse...)...)
		wantContains(t, out, "None")
	})
	t.Run("discover servers", func(t *testing.T) {
		out := mustRun(t, append([]string{"discover", "servers"}, reverse...)...)
		wantContains(t, out, serverURI)
	})
	t.Run("ping skips tcp", func(t *testing.T) {
		out := mustRun(t, append([]string{"ping", "--format", "json"}, reverse...)...)
		wantContains(t, out, `"secure"`, `"browse"`)
		if strings.Contains(out, `"tcp"`) {
			t.Errorf("ping dialled the server although it uses reverse connect:\n%s", out)
		}
	})
	t.Run("endpoint still required without reverse", func(t *testing.T) {
		if _, _, err := runCLI(t, "get", "value", "-n", "i=2258"); err == nil {
			t.Fatal("expected an error without --endpoint and --reverse-listen")
		}
	})
}

// TestE2E_Discovery queries a Local Discovery Server another server
// registered with.
func TestE2E_Discovery(t *testing.T) {
	const ldsURI, registrantURI = "urn:otfabric:opcuactl:test:lds", "urn:otfabric:opcuactl:test:registrant"

	lds := startServer(t,
		server.ApplicationURI(ldsURI),
		server.ServerName("LDS"),
		server.WithLocalDiscoveryServer(server.LDSAllowInsecureRegistration()),
	)
	registered := make(chan struct{}, 16)
	startServer(t,
		server.ApplicationURI(registrantURI),
		server.ServerName("Registrant"),
		server.WithDiscoveryRegistration(lds.url,
			server.RegisterInsecure(),
			server.RegisterServerCapabilities("DA", "HD"),
			server.RegisterEventHandler(func(server.RegistrationEvent) {
				select {
				case registered <- struct{}{}:
				default:
				}
			}),
		),
	)
	deadline := time.Now().Add(30 * time.Second)
	for len(lds.srv.RegisteredServers()) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the server did not register with the LDS")
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Run("servers", func(t *testing.T) {
		out := mustRun(t, "discover", "servers", "-e", lds.url)
		wantContains(t, out, ldsURI, registrantURI)

		out = mustRun(t, "discover", "servers", "-e", lds.url, "--server-uri", registrantURI, "--format", "json")
		wantContains(t, out, registrantURI)
		if strings.Contains(out, ldsURI) {
			t.Errorf("--server-uri did not filter:\n%s", out)
		}
	})

	t.Run("lds", func(t *testing.T) {
		out := mustRun(t, "discover", "lds", "--host", lds.url)
		wantContains(t, out, "Discovery server: "+lds.url, registrantURI, "not supported")

		out = mustRun(t, "discover", "lds", "-e", lds.url, "--format", "json")
		var res struct {
			DiscoveryServerURL string `json:"discoveryServerUrl"`
			MulticastSupported bool   `json:"multicastSupported"`
			Servers            []struct {
				ApplicationURI string `json:"applicationUri"`
			} `json:"servers"`
		}
		if err := json.Unmarshal([]byte(out), &res); err != nil {
			t.Fatalf("json: %v\n%s", err, out)
		}
		if res.DiscoveryServerURL != lds.url || res.MulticastSupported || len(res.Servers) != 2 {
			t.Errorf("unexpected result: %+v", res)
		}
	})

	t.Run("lds needs a target", func(t *testing.T) {
		if _, _, err := runCLI(t, "discover", "lds"); err == nil {
			t.Fatal("expected an error without --host and --endpoint")
		}
	})

	t.Run("on-network without multicast", func(t *testing.T) {
		if _, _, err := runCLI(t, "discover", "servers", "-e", lds.url, "--on-network"); err == nil {
			t.Fatal("FindServersOnNetwork succeeded on a server without the multicast extension")
		}
	})
}

// TestE2E_DiscoveryOnNetwork asks a discovery server with the multicast
// extension for the servers it knows from mDNS.
func TestE2E_DiscoveryOnNetwork(t *testing.T) {
	opts := []server.Option{
		server.EndPoint("localhost", 0),
		server.InsecureNoSecurity(),
		server.EnableAuthMode(ua.UserTokenTypeAnonymous),
		server.ApplicationURI("urn:otfabric:opcuactl:test:ldsme"),
		server.ServerName("LDSME"),
		server.WithLocalDiscoveryServer(),
		server.WithMulticastDiscovery(
			server.MulticastHostname("opcuactl-test-host"),
			server.MulticastCapabilities("LDS", "DA"),
			server.MulticastIPv4Only(),
		),
	}
	srv, err := server.New(opts...)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	if err := srv.Start(context.Background()); err != nil {
		if os.Getenv("OPCUACTL_REQUIRE_MULTICAST") != "" {
			t.Fatalf("multicast discovery: %v", err)
		}
		t.Skipf("multicast discovery is not available here: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	if len(srv.ServersOnNetwork()) == 0 {
		if os.Getenv("OPCUACTL_REQUIRE_MULTICAST") != "" {
			t.Fatal("the server announced nothing")
		}
		t.Skip("the server announced nothing (no multicast interface)")
	}
	endpoint := fmt.Sprintf("opc.tcp://localhost:%d", srv.Port())

	out := mustRun(t, "discover", "servers", "-e", endpoint, "--on-network")
	wantContains(t, out, "RECORD", "LDSME", "DA")

	out = mustRun(t, "discover", "servers", "-e", endpoint, "--on-network", "--capability", "DA", "--max-records", "1", "--format", "json")
	wantContains(t, out, "LDSME")

	out = mustRun(t, "discover", "servers", "-e", endpoint, "--on-network", "--capability", "HD", "--format", "json")
	if strings.Contains(out, "LDSME") {
		t.Errorf("--capability HD returned a server without it:\n%s", out)
	}

	out = mustRun(t, "discover", "lds", "--host", endpoint)
	wantContains(t, out, "Servers on network (FindServersOnNetwork, mDNS):", "LDSME")
}

// writeLoop writes the given values to a node, one every interval, after
// giving the command under test time to create its subscription.
func writeLoop(t *testing.T, s *testServer, name string, interval time.Duration, values ...any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	c, err := opcua.NewClient(s.url, opcua.InsecureNoSecurity())
	if err != nil {
		t.Fatalf("writer client: %v", err)
	}
	if err := c.Connect(ctx); err != nil {
		t.Fatalf("writer connect: %v", err)
	}
	nodeID := ua.NewStringNodeID(s.ns.ID(), name)
	go func() {
		defer cancel()
		defer func() { _ = c.Close(context.Background()) }()
		time.Sleep(time.Second)
		for _, v := range values {
			time.Sleep(interval)
			_, _ = c.WriteNodeValue(ctx, nodeID, v)
		}
	}()
}

type monitoredLine struct {
	NodeID string  `json:"nodeId"`
	Value  float64 `json:"value"`
	Status string  `json:"status"`
}

func monitoredValues(t *testing.T, out string) []float64 {
	t.Helper()
	var values []float64
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		var m monitoredLine
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("not JSONL: %q: %v", line, err)
		}
		values = append(values, m.Value)
	}
	return values
}

// checkDeadband verifies what a deadband lets through: every reported value
// is further than deadband away from the one reported before it, and the
// last one is the value that left the deadband. The first value depends on
// when the subscription started and is not checked.
func checkDeadband(t *testing.T, got []float64, deadband, last float64) {
	t.Helper()
	if len(got) < 2 || got[len(got)-1] != last {
		t.Errorf("values = %v, want at least two ending in %v", got, last)
		return
	}
	for i := 1; i < len(got); i++ {
		if diff := got[i] - got[i-1]; diff <= deadband && diff >= -deadband {
			t.Errorf("values = %v: %v was reported although it is within %v of %v", got, got[i], deadband, got[i-1])
		}
	}
}

// TestE2E_MonitorDataChangeFilter checks that the deadband flags reach the
// server and that rejected items are reported.
func TestE2E_MonitorDataChangeFilter(t *testing.T) {
	s := startServer(t)

	t.Run("absolute deadband", func(t *testing.T) {
		// 100 → 101 (inside the deadband of 5) → 110 (outside) → 112 (inside).
		writeLoop(t, s, "Double", 400*time.Millisecond, float64(101), float64(110), float64(112))
		out := mustRun(t, "monitor", "value", "-e", s.url, "-n", s.node("Double"), "--jsonl",
			"--interval", "50ms", "--sampling", "0s",
			"--deadband-type", "absolute", "--deadband", "5", "--duration", "4s")
		checkDeadband(t, monitoredValues(t, out), 5, 110)
	})

	t.Run("percent deadband", func(t *testing.T) {
		// EURange is [0, 200]: 10 % is 20. 100 → 110 (inside) → 150 (outside).
		writeLoop(t, s, "Analog", 400*time.Millisecond, float64(110), float64(150))
		out := mustRun(t, "monitor", "value", "-e", s.url, "-n", s.node("Analog"), "--jsonl",
			"--interval", "50ms", "--sampling", "0s",
			"--deadband-type", "percent", "--deadband", "10", "--duration", "3500ms")
		checkDeadband(t, monitoredValues(t, out), 20, 150)
	})

	t.Run("no filter reports every change", func(t *testing.T) {
		writeLoop(t, s, "Int32", 300*time.Millisecond, int32(43), int32(44))
		out := mustRun(t, "monitor", "value", "-e", s.url, "-n", s.node("Int32"), "--jsonl",
			"--interval", "50ms", "--sampling", "0s", "--duration", "3s")
		// Without a filter every write arrives, also the ones a deadband
		// would have swallowed.
		got := monitoredValues(t, out)
		if len(got) < 2 || got[len(got)-1] != 44 || got[len(got)-2] != 43 {
			t.Errorf("values = %v, want ... 43 44", got)
		}
	})

	t.Run("index range", func(t *testing.T) {
		out := mustRun(t, "monitor", "value", "-e", s.url, "-n", s.node("Int32Array"),
			"--format", "json", "--index-range", "1:2", "--count", "1", "--duration", "20s")
		wantContains(t, out, `"value":[2,3]`)
	})

	t.Run("rejected items", func(t *testing.T) {
		// A deadband on a string, and a percent deadband without EURange.
		for _, args := range [][]string{
			{"-n", s.node("String"), "--deadband-type", "absolute", "--deadband", "1"},
			{"-n", s.node("Double"), "--deadband-type", "percent", "--deadband", "10"},
		} {
			_, errOut, err := runCLI(t, append([]string{"monitor", "value", "-e", s.url, "--duration", "1s"}, args...)...)
			if err == nil {
				t.Fatalf("monitor %v: the server accepted the filter", args)
			}
			if !strings.Contains(errOut, "Bad") {
				t.Errorf("monitor %v: stderr does not name the status: %s", args, errOut)
			}
		}

		// One good item next to a rejected one keeps running.
		out, errOut, err := runCLI(t, "monitor", "value", "-e", s.url, "--jsonl",
			"-n", s.node("Analog"), "-n", s.node("String"),
			"--deadband-type", "absolute", "--deadband", "1", "--count", "1", "--duration", "20s")
		if err != nil {
			t.Fatalf("monitor with one rejected item: %v\n%s", err, errOut)
		}
		wantContains(t, errOut, s.node("String"))
		wantContains(t, out, s.node("Analog"))
	})

	t.Run("flag validation", func(t *testing.T) {
		for _, args := range [][]string{
			{"--trigger", "sometimes"},
			{"--deadband-type", "relative"},
			{"--deadband", "5"},
			{"--deadband-type", "absolute", "--deadband", "-1"},
			{"--deadband-type", "percent", "--deadband", "101"},
		} {
			if _, _, err := runCLI(t, append([]string{"monitor", "value", "-e", s.url, "-n", s.node("Double")}, args...)...); err == nil {
				t.Errorf("monitor %v: expected a usage error", args)
			}
		}
	})

	t.Run("trigger", func(t *testing.T) {
		out := mustRun(t, "monitor", "value", "-e", s.url, "-n", s.node("String"), "--format", "json",
			"--trigger", "status-value-timestamp", "--count", "1", "--duration", "20s")
		wantContains(t, out, "hello")
	})
}

// TestE2E_UAEncodings reads values in the OPC UA JSON and XML encodings and
// writes them back.
func TestE2E_UAEncodings(t *testing.T) {
	s := startServer(t)
	arr := s.node("Int32Array")

	for enc, want := range map[string]string{
		"json":               `{"UaType":6,"Value":[1,2,3,4,5]`,
		"json-verbose":       `{"UaType":6,"Value":[1,2,3,4,5]`,
		"json-reversible":    `{"Value":{"Type":6,"Body":[1,2,3,4,5]}`,
		"json-nonreversible": `{"Value":[1,2,3,4,5]`,
		"xml":                `<ListOfInt32><Int32>1</Int32>`,
	} {
		out := mustRun(t, "get", "value", "-e", s.url, "-n", arr, "--ua-encoding", enc)
		wantContains(t, out, want)
	}

	out := mustRun(t, "get", "value", "-e", s.url, "-n", arr, "-n", s.node("String"), "--ua-encoding", "json", "--pretty")
	wantContains(t, out, "\n  \"UaType\": 6", `"UaType": 12`)

	if _, _, err := runCLI(t, "get", "value", "-e", s.url, "-n", arr, "--ua-encoding", "bson"); err == nil {
		t.Error("expected an error for an unknown --ua-encoding")
	}

	mustRun(t, "write", "value", "-e", s.url, "-n", arr, "--yes", "--type", "ua-json", "--value", `{"UaType":6,"Value":[9,8,7]}`)
	wantContains(t, mustRun(t, "get", "value", "-e", s.url, "-n", arr, "--format", "json"), "[9,8,7]")

	mustRun(t, "write", "value", "-e", s.url, "-n", s.node("Double"), "--yes", "--type", "ua-xml",
		"--value", `<Variant xmlns="http://opcfoundation.org/UA/2008/02/Types.xsd"><Value><Double>12.5</Double></Value></Variant>`)
	wantContains(t, mustRun(t, "get", "value", "-e", s.url, "-n", s.node("Double"), "-q"), "12.5")

	if _, _, err := runCLI(t, "write", "value", "-e", s.url, "-n", arr, "--yes", "--type", "ua-json", "--value", `{"UaType":`); err == nil {
		t.Error("expected an error for malformed ua-json")
	}

	out = mustRun(t, "write", "value", "-e", s.url, "-n", arr, "--dry-run", "--format", "json",
		"--type", "ua-json", "--value", `{"UaType":6,"Value":[1]}`)
	wantContains(t, out, "DryRun")
}
