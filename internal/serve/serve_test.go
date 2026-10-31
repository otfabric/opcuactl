// SPDX-License-Identifier: MIT

package serve

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-opcua"
	"github.com/otfabric/go-opcua/id"
	"github.com/otfabric/go-opcua/ua"
)

// TestMain fails when goroutines of the server or the client are still
// running after the tests: a Close that did not stop everything.
func TestMain(m *testing.M) {
	code := m.Run()
	if code == 0 {
		if leaked := waitGoroutines(5 * time.Second); leaked != "" {
			fmt.Fprintf(os.Stderr, "goroutines still running after the tests:\n%s\n", leaked)
			code = 1
		}
	}
	os.Exit(code)
}

// waitGoroutines waits until no goroutine runs code of go-opcua or of this
// package, and returns their stacks if some still do after d.
func waitGoroutines(d time.Duration) string {
	deadline := time.Now().Add(d)
	for {
		buf := make([]byte, 1<<20)
		buf = buf[:runtime.Stack(buf, true)]
		var leaked []string
		for _, g := range strings.Split(string(buf), "\n\n") {
			if strings.Contains(g, "otfabric/go-opcua") || strings.Contains(g, "opcuactl/internal/serve.(") {
				if !strings.Contains(g, "waitGoroutines") {
					leaked = append(leaked, g)
				}
			}
		}
		if len(leaked) == 0 {
			return ""
		}
		if time.Now().After(deadline) {
			return strings.Join(leaked, "\n\n")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

const testURI = "urn:otfabric:opcuactl:test:serve"

func start(t *testing.T, cfg Config) *Server {
	t.Helper()
	if cfg.Listen == "" {
		cfg.Listen = "127.0.0.1:0"
	}
	if cfg.Hostname == "" {
		cfg.Hostname = "127.0.0.1"
	}
	if cfg.Policies == nil {
		cfg.Policies = []string{ua.SecurityPolicyURINone}
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func endpointURL(t *testing.T, s *Server, scheme string) string {
	t.Helper()
	for _, ep := range s.Endpoints() {
		if strings.HasPrefix(ep.EndpointURL, scheme+"://") {
			return ep.EndpointURL
		}
	}
	t.Fatalf("no %s endpoint", scheme)
	return ""
}

func connect(t *testing.T, endpoint string, opts ...opcua.Option) *opcua.Client {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := opcua.NewClient(endpoint, append([]opcua.Option{opcua.InsecureNoSecurity()}, opts...)...)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if err := c.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	return c
}

func TestDemo(t *testing.T) {
	s := start(t, Config{Anonymous: true, Demo: true, Interval: 20 * time.Millisecond, ApplicationURI: testURI, ApplicationName: "test"})
	uri, idx, ok := s.DemoNamespace()
	if !ok || uri != DemoNamespaceURI {
		t.Fatalf("DemoNamespace = %q %d %v", uri, idx, ok)
	}
	if ns := s.Namespaces(); len(ns) <= int(idx) || ns[idx] != DemoNamespaceURI {
		t.Fatalf("Namespaces = %v", ns)
	}
	if got := s.ApplicationURI(); got != testURI {
		t.Errorf("ApplicationURI = %q", got)
	}
	node := func(name string) *ua.NodeID { return ua.NewStringNodeID(idx, name) }

	ctx := context.Background()
	c := connect(t, endpointURL(t, s, "opc.tcp"))

	for name, want := range map[string]any{
		DemoBoolean: true, DemoInt32: int32(42), DemoDouble: 3.14, DemoString: "hello from opcuactl", DemoLevel: demoLevelStart,
	} {
		dv, err := c.ReadValue(ctx, node(name))
		if err != nil || dv.Value == nil || dv.Value.Value() != want {
			t.Errorf("%s = %v, %v; want %v", name, dv, err, want)
		}
	}

	// Counter counts and Sine moves.
	first, err := c.ReadValue(ctx, node(DemoCounter))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		dv, err := c.ReadValue(ctx, node(DemoCounter))
		if err != nil {
			t.Fatal(err)
		}
		if dv.Value.Value().(uint32) > first.Value.Value().(uint32)+2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("Counter does not count: %v", dv.Value.Value())
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Read-only values cannot be written; Level can.
	if sc, err := c.WriteNodeValue(ctx, node(DemoInt32), int32(1)); err != nil || sc == ua.StatusOK {
		t.Errorf("writing Int32: %v %v", sc, err)
	}
	if sc, err := c.WriteNodeValue(ctx, node(DemoLevel), 90.0); err != nil || sc != ua.StatusOK {
		t.Fatalf("writing Level: %v %v", sc, err)
	}
	waitAlarm := func(active bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for s.demo.alarm.State().Active != active {
			if time.Now().After(deadline) {
				t.Fatalf("LevelAlarm active = %v, want %v", !active, active)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	waitAlarm(true)
	if sc, err := c.WriteNodeValue(ctx, node(DemoLevel), 10.0); err != nil || sc != ua.StatusOK {
		t.Fatalf("writing Level: %v %v", sc, err)
	}
	waitAlarm(false)

	// The method.
	for _, tc := range []struct {
		args []any
		want ua.StatusCode
	}{
		{[]any{int32(2), int32(3)}, ua.StatusOK},
		{[]any{int32(2)}, ua.StatusBadArgumentsMissing},
		{[]any{int32(1), int32(2), int32(3)}, ua.StatusBadTooManyArguments},
		{[]any{"x", int32(1)}, ua.StatusBadTypeMismatch},
	} {
		res, err := c.CallMethod(ctx, node(DemoFolder), node(DemoAdd), tc.args...)
		if err != nil {
			t.Fatalf("Add%v: %v", tc.args, err)
		}
		if res.StatusCode != tc.want {
			t.Errorf("Add%v = %v, want %v", tc.args, res.StatusCode, tc.want)
		}
		if tc.want == ua.StatusOK && (len(res.OutputArguments) != 1 || res.OutputArguments[0].Value() != int32(5)) {
			t.Errorf("Add(2, 3) = %v", res.OutputArguments)
		}
	}
	in, out, err := c.MethodArguments(ctx, node(DemoFolder), node(DemoAdd))
	if err != nil || len(in) != 2 || len(out) != 1 || in[0].Name != "a" || out[0].Name != "sum" {
		t.Errorf("MethodArguments = %v %v %v", in, out, err)
	}

	// Sine is historized.
	res, err := s.demo.hist.ReadRaw(node(DemoSine), time.Now().Add(-time.Minute), time.Now().Add(time.Minute), 0, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	hd, ok := res.HistoryData.Value.(*ua.HistoryData)
	if !ok || len(hd.DataValues) < 3 {
		t.Errorf("history of Sine: %+v", res.HistoryData)
	}

	// Demo is a HistoricalEventNode: the start event and the LevelAlarm
	// transitions are in its event history.
	en, err := c.Node(node(DemoFolder)).Attribute(ctx, ua.AttributeIDEventNotifier)
	if want := byte(ua.EventNotifierTypeSubscribeToEvents | ua.EventNotifierTypeHistoryRead | ua.EventNotifierTypeHistoryWrite); err != nil || en.Value() != want {
		t.Errorf("EventNotifier of Demo = %v %v, want %d", en, err, want)
	}
	events, _, st := s.demo.hist.ReadEvents(node(DemoFolder), time.Now().Add(-time.Minute), time.Now().Add(time.Minute), 0, nil, nil)
	if st != ua.StatusOK || len(events) < 3 {
		t.Fatalf("event history of Demo: %v %d events", st, len(events))
	}
	if msg, _ := events[0].Fields["Message"].Value().(*ua.LocalizedText); msg == nil || msg.Text != "opcuactl serve started" || len(events[0].EventID()) != 16 {
		t.Errorf("start event %+v", events[0].Fields)
	}
	alarms := 0
	for _, ev := range events {
		if ev.EventType().IntID() == id.ExclusiveLevelAlarmType {
			alarms++
		}
	}
	if alarms < 2 {
		t.Errorf("%d LevelAlarm events in the history of Demo", alarms)
	}

	if err := s.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

// The level alarm ignores a Level that is no Double.
func TestDemoLevelNotDouble(t *testing.T) {
	s := start(t, Config{Anonymous: true, Demo: true})
	last := 1.0
	_ = s.demo.level.SetAttribute(ua.AttributeIDValue, nil)
	s.demo.evaluateLevel(&last)
	if last != 1.0 {
		t.Errorf("a Level without value was evaluated: %v", last)
	}
}

func TestUsersAndTrust(t *testing.T) {
	policy := ua.SecurityPolicyURIBasic256Sha256
	ids, err := GenerateIdentities(nil, []string{policy}, false, testURI, []string{"localhost", "127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	clientIDs, err := GenerateIdentities(nil, []string{policy}, false, "urn:otfabric:opcuactl:test:client", nil)
	if err != nil {
		t.Fatal(err)
	}
	other, err := GenerateIdentities(nil, []string{policy}, false, "urn:someone:else", nil)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(ids[0].Cert)

	secure := func(s *Server, auth ...opcua.Option) ([]opcua.Option, error) {
		eps, err := opcua.GetEndpoints(context.Background(), endpointURL(t, s, "opc.tcp"))
		if err != nil {
			return nil, err
		}
		ep, err := ua.SelectEndpoint(eps, policy, ua.MessageSecurityModeSignAndEncrypt)
		if err != nil {
			return nil, err
		}
		return append([]opcua.Option{
			opcua.SecurityFromEndpoint(ep, ua.UserTokenTypeUserName),
			opcua.Certificate(clientIDs[0].Cert),
			opcua.PrivateKey(clientIDs[0].Key.(*rsa.PrivateKey)),
			opcua.TrustedCertificates(cert),
		}, auth...), nil
	}
	dial := func(s *Server, auth ...opcua.Option) error {
		opts, err := secure(s, auth...)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		c, err := opcua.NewClient(endpointURL(t, s, "opc.tcp"), opts...)
		if err != nil {
			return err
		}
		defer func() { _ = c.Close(context.Background()) }()
		return c.Connect(ctx)
	}

	t.Run("users", func(t *testing.T) {
		s := start(t, Config{Users: []User{{"op", "pw"}, {"admin", "secret"}}, Identities: ids,
			Policies: []string{ua.SecurityPolicyURINone, policy}, Modes: []ua.MessageSecurityMode{ua.MessageSecurityModeSignAndEncrypt}})
		if err := dial(s, opcua.AuthUsername("admin", "secret")); err != nil {
			t.Errorf("admin: %v", err)
		}
		if err := dial(s, opcua.AuthUsername("admin", "wrong")); err == nil {
			t.Error("a wrong password was accepted")
		}
		if err := dial(s, opcua.AuthUsername("nobody", "pw")); err == nil {
			t.Error("an unknown user was accepted")
		}
	})
	t.Run("trust list", func(t *testing.T) {
		s := start(t, Config{Users: []User{{"op", "pw"}}, Identities: ids, TrustedClients: [][]byte{clientIDs[0].Cert},
			Policies: []string{policy}, Modes: []ua.MessageSecurityMode{ua.MessageSecurityModeSignAndEncrypt}})
		if err := dial(s, opcua.AuthUsername("op", "pw")); err != nil {
			t.Errorf("trusted client: %v", err)
		}
		s2 := start(t, Config{Users: []User{{"op", "pw"}}, Identities: ids, TrustedClients: [][]byte{other[0].Cert},
			Policies: []string{policy}, Modes: []ua.MessageSecurityMode{ua.MessageSecurityModeSignAndEncrypt}})
		if err := dial(s2, opcua.AuthUsername("op", "pw")); err == nil {
			t.Error("an untrusted client was accepted")
		}
	})
}

func TestTransportsAndNodeSet(t *testing.T) {
	ids, err := GenerateIdentities(nil, nil, true, testURI, []string{"127.0.0.1"})
	if err != nil || len(ids) != 1 || ids[0].KeyType() != "rsa" {
		t.Fatalf("GenerateIdentities(needRSA) = %v, %v", ids, err)
	}
	xml, err := os.ReadFile("testdata/machine.NodeSet2.xml")
	if err != nil {
		t.Fatal(err)
	}
	s := start(t, Config{
		Anonymous: true, Identities: ids, Logger: nil,
		WebSocket: "127.0.0.1:0", HTTPS: "127.0.0.1:0", UAJSON: true,
		NodeSets: []NodeSet{{Name: "machine", XML: xml}},
	})
	if _, _, ok := s.DemoNamespace(); ok {
		t.Error("a demo namespace without Demo")
	}
	profiles := map[string]bool{}
	for _, ep := range s.Endpoints() {
		profiles[ep.TransportProfileURI] = true
	}
	for _, p := range []string{ua.TransportProfileUATCP, ua.TransportProfileWSS, ua.TransportProfileHTTPS, ua.TransportProfileWSSJSON, ua.TransportProfileHTTPSJSON} {
		if !profiles[p] {
			t.Errorf("no %s endpoint: %v", p, profiles)
		}
	}
	idx := -1
	for i, ns := range s.Namespaces() {
		if ns == "urn:otfabric:opcuactl:test:machine" {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatalf("the NodeSet namespace is missing: %v", s.Namespaces())
	}
	c := connect(t, endpointURL(t, s, "opc.tcp"))
	dv, err := c.ReadValue(context.Background(), ua.NewStringNodeID(uint16(idx), "Machine.Speed"))
	if err != nil || dv.Value == nil || dv.Value.Value() != 1500.5 {
		t.Errorf("Machine.Speed = %v, %v", dv, err)
	}
	// Without an ApplicationURI the one of the certificate.
	if got := s.ApplicationURI(); got != testURI {
		t.Errorf("ApplicationURI = %q", got)
	}
}

func TestTLSCertificate(t *testing.T) {
	ids, err := GenerateIdentities(nil, nil, true, testURI, []string{"127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	pair := tlsPair(t, ids[0])
	s := start(t, Config{Anonymous: true, HTTPS: "127.0.0.1:0", TLS: &pair, ApplicationURI: testURI})
	if u := endpointURL(t, s, "opc.https"); u == "" {
		t.Fatal("no opc.https endpoint")
	}
	if got := s.ApplicationURI(); got != testURI {
		t.Errorf("ApplicationURI = %q", got)
	}
}

func TestIPv6Hostname(t *testing.T) {
	l, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		if os.Getenv("OPCUACTL_REQUIRE_IPV6") != "" {
			t.Fatalf("no IPv6 loopback: %v", err)
		}
		t.Skip("no IPv6 loopback")
	}
	_ = l.Close()
	s := start(t, Config{Anonymous: true, Listen: "[::1]:0", Hostname: "::1"})
	if u := endpointURL(t, s, "opc.tcp"); !strings.HasPrefix(u, "opc.tcp://[::1]:") {
		t.Errorf("endpoint URL %s", u)
	}
}

func TestConfigErrors(t *testing.T) {
	none := []string{ua.SecurityPolicyURINone}
	for _, tc := range []struct {
		name string
		cfg  Config
		want string
	}{
		{"nobody", Config{Listen: "127.0.0.1:0", Policies: none}, ErrNoUser.Error()},
		{"listen", Config{Listen: "nowhere", Anonymous: true, Policies: none}, "listen address"},
		{"listen port", Config{Listen: "127.0.0.1:http-nope", Anonymous: true, Policies: none}, "listen address"},
		{"wss", Config{Listen: "127.0.0.1:0", WebSocket: "x", Anonymous: true, Policies: none}, "opc.wss address"},
		{"policy", Config{Listen: "127.0.0.1:0", Anonymous: true, Policies: []string{"http://opcfoundation.org/UA/SecurityPolicy#Nope"}}, "unsupported security policy"},
		{"nodeset", Config{Listen: "127.0.0.1:0", Anonymous: true, Policies: none, NodeSets: []NodeSet{{Name: "bad.xml", XML: []byte("<nope")}}}, "importing bad.xml"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(tc.cfg)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("New: %v, want %q", err, tc.want)
			}
		})
	}

	t.Run("port in use", func(t *testing.T) {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = l.Close() }()
		s, err := New(Config{Listen: l.Addr().String(), Hostname: "127.0.0.1", Anonymous: true, Policies: none, Demo: true})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Start(context.Background()); err == nil {
			_ = s.Close()
			t.Fatal("Start on a port in use succeeded")
		}
	})
}

func TestGenerateIdentities(t *testing.T) {
	all := []string{
		ua.SecurityPolicyURINone, ua.SecurityPolicyURIBasic256Sha256, ua.SecurityPolicyURIECCNistP256,
		ua.SecurityPolicyURIECCNistP384, ua.SecurityPolicyURIECCCurve25519, ua.SecurityPolicyURIECCNistP256AesGcm,
	}
	ids, err := GenerateIdentities(nil, all, true, testURI, []string{"localhost", "127.0.0.1", "localhost", ""})
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, id := range ids {
		kinds = append(kinds, id.KeyType())
		if !id.Generated || len(id.Thumbprint()) != 40 {
			t.Errorf("identity %s: generated %v, thumbprint %q", id.KeyType(), id.Generated, id.Thumbprint())
		}
		c, err := x509.ParseCertificate(id.Cert)
		if err != nil {
			t.Fatal(err)
		}
		if len(c.URIs) != 1 || c.URIs[0].String() != testURI || len(c.DNSNames) != 1 || len(c.IPAddresses) != 1 {
			t.Errorf("%s certificate: %v %v %v", id.KeyType(), c.URIs, c.DNSNames, c.IPAddresses)
		}
	}
	if got := strings.Join(kinds, ","); got != "rsa,ecc-P-256,ecc-P-384,ed25519" {
		t.Errorf("generated %s", got)
	}

	// Nothing more is needed.
	more, err := GenerateIdentities(ids, all, true, testURI, nil)
	if err != nil || len(more) != 0 {
		t.Errorf("covered policies generated %d identities, %v", len(more), err)
	}
	// A brainpool policy needs a given certificate.
	if _, err := GenerateIdentities(nil, []string{ua.SecurityPolicyURIECCBrainpoolP256r1}, false, testURI, nil); err == nil || !strings.Contains(err.Error(), "ECC_brainpoolP256r1") {
		t.Errorf("brainpool: %v", err)
	}
	if _, err := GenerateIdentities(nil, []string{ua.SecurityPolicyURIBasic256Sha256}, false, "%zz", nil); err == nil {
		t.Error("an invalid application URI was accepted")
	}
	if _, err := GenerateIdentities(nil, nil, true, "%zz", nil); err == nil {
		t.Error("an invalid application URI was accepted")
	}

	// Round trip through PEM bundles.
	certs := EncodeCertificates(ids)
	keys, err := EncodeKeys(ids)
	if err != nil {
		t.Fatal(err)
	}
	back, err := ParseIdentities(certs, keys)
	if err != nil || len(back) != len(ids) {
		t.Fatalf("ParseIdentities = %d, %v", len(back), err)
	}
	for i := range ids {
		if back[i].Thumbprint() != ids[i].Thumbprint() || back[i].Generated {
			t.Errorf("identity %d differs", i)
		}
	}
	// DER files.
	der, err := x509.MarshalPKCS8PrivateKey(ids[0].Key)
	if err != nil {
		t.Fatal(err)
	}
	if one, err := ParseIdentities(ids[0].Cert, der); err != nil || len(one) != 1 {
		t.Errorf("DER: %v %v", one, err)
	}
	// A chain: the issuer without a key is left out.
	if one, err := ParseIdentities(EncodeCertificates(ids[1:2]), keys[:0]); err == nil {
		t.Errorf("no key: %v", one)
	}
	if _, err := ParseIdentities(EncodeCertificates(ids[:1]), mustKeys(t, ids[1:2])); err == nil || !strings.Contains(err.Error(), "no certificate for the ecc-P-256") {
		t.Errorf("a key without its certificate: %v", err)
	}
	if _, err := ParseIdentities([]byte("nope"), keys); err == nil {
		t.Error("a bad certificate was accepted")
	}
	if _, err := ParseIdentities(certs, []byte("nope")); err == nil {
		t.Error("a bad key was accepted")
	}
	bad := []byte("-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n")
	if _, err := ParseCertificates(bad); err == nil {
		t.Error("a bad PEM certificate was accepted")
	}
	badKey := []byte("-----BEGIN PRIVATE KEY-----\nAAAA\n-----END PRIVATE KEY-----\n")
	if _, err := ParseIdentities(certs, badKey); err == nil {
		t.Error("a bad PEM key was accepted")
	}
	if _, err := EncodeKeys([]Identity{{Key: fakeSigner{}}}); err == nil {
		t.Error("an unknown key was encoded")
	}
	if got := (Identity{Key: fakeSigner{}}).KeyType(); !strings.Contains(got, "fakeKey") {
		t.Errorf("KeyType of an unknown key = %q", got)
	}
}

func mustKeys(t *testing.T, ids []Identity) []byte {
	t.Helper()
	b, err := EncodeKeys(ids)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

type fakeKey struct{}

type fakeSigner struct{}

func (fakeSigner) Public() crypto.PublicKey { return fakeKey{} }
func (fakeSigner) Sign(io.Reader, []byte, crypto.SignerOpts) ([]byte, error) {
	return nil, errors.New("fake")
}

func tlsPair(t *testing.T, id Identity) tls.Certificate {
	t.Helper()
	return tls.Certificate{Certificate: [][]byte{id.Cert}, PrivateKey: id.Key}
}
