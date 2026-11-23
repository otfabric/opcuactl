// SPDX-License-Identifier: MIT

package opcuactl

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-opcua/pubsub"
	"github.com/otfabric/go-opcua/server"
	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/go-opcua/uafx/fxserver"
	"github.com/otfabric/opcuactl/internal/model"
	"github.com/otfabric/opcuactl/internal/parse"
)

// fxConnections are the AutomationComponents of the connection tests of
// go-opcua (internal/testutil/fxtest/connections.xml).
//
//go:embed testdata/fx_connections.xml
var fxConnections []byte

const (
	fxConnNS     = "urn:opcuactl:test:fx:connections"
	fxCtl        = 1000 // the AutomationComponent
	fxMotion     = 1200 // its FunctionalEntity
	fxSetpoint   = 1221 // InputData
	fxActual     = 1231 // OutputData
	fxGain       = 1241 // ConfigurationData, writable
	fxFixed      = 1242 // ConfigurationData, 7.0
	fxPreset     = 1300 // a preconfigured endpoint of Motion
	fxPeer       = 3000 // a second AutomationComponent
	fxRemoteFE   = 3200 // its FunctionalEntity
	fxUDPProfile = "http://opcfoundation.org/UA-Profile/Transport/pubsub-udp-uadp"
)

// fxConnServer is a server that serves EstablishConnections and
// CloseConnections on the AutomationComponents of fx_connections.xml.
type fxConnServer struct {
	srv *server.Server
	url string
	ns  uint16 // index of fxConnNS
}

func (s *fxConnServer) id(n uint32) *ua.NodeID { return ua.NewNumericNodeID(s.ns, n) }

// nsu is the NodeId n of the test namespace with its URI.
func nsu(n uint32) string { return fmt.Sprintf("nsu=%s;i=%d", fxConnNS, n) }

func startFxConnServer(t *testing.T) *fxConnServer {
	t.Helper()
	srv, err := server.New(
		server.EndPoint("localhost", 0),
		server.InsecureNoSecurity(),
		server.EnableAuthMode(ua.UserTokenTypeAnonymous),
		server.ApplicationURI(fxAppURI),
		server.WithPubSub(nil),
	)
	if err != nil {
		t.Fatal(err)
	}
	ns, err := fxserver.AddModel(srv, fxserver.ModelAC)
	if err != nil {
		t.Fatal(err)
	}
	if err := fxserver.ImportNodeSet(srv, fxConnections); err != nil {
		t.Fatal(err)
	}
	s := &fxConnServer{srv: srv, ns: fxNamespace(t, srv, fxConnNS)}
	// The InputVariables and OutputVariables of the preconfigured endpoint.
	s.set(t, fxPreset+6, []*ua.NodeID{s.id(fxSetpoint)})
	s.set(t, fxPreset+7, []*ua.NodeID{})
	conns, err := fxserver.EnableConnections(srv, ns, fxserver.ConnectionOptions{Interval: 10 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = conns.Close()
		_ = srv.Close()
	})
	s.url = fmt.Sprintf("opc.tcp://localhost:%d", srv.Port())
	return s
}

func (s *fxConnServer) set(t *testing.T, n uint32, v any) {
	t.Helper()
	node := s.srv.Node(s.id(n))
	if node == nil {
		t.Fatalf("no node %d", n)
	}
	if err := node.SetAttribute(ua.AttributeIDValue, server.DataValueFromValue(v)); err != nil {
		t.Fatal(err)
	}
	s.srv.ChangeNotification(node.ID())
}

func (s *fxConnServer) value(n uint32) any {
	node := s.srv.Node(s.id(n))
	if node == nil || node.Value() == nil || node.Value().Value == nil {
		return nil
	}
	return node.Value().Value.Value()
}

// pubsubFile writes the PubSub configuration of a loopback connection of
// Motion as pubsub config export writes it: the writer publishes Actual,
// the reader writes what it receives to Setpoint.
func (s *fxConnServer) pubsubFile(t *testing.T, dir string) string {
	t.Helper()
	udp := freeUDPURL(t)
	pds := pubsub.PublishedDataItems("PDS", pubsub.Var("x", s.id(fxActual), ua.TypeIDDouble))
	pds.DataSetMetaData.ConfigurationVersion = &ua.ConfigurationVersionDataType{}
	w := pubsub.DataSetWriter("W", 11, "PDS")
	w.Enabled = false
	md := pubsub.MetaData("m", pubsub.Field{Name: "x", Type: ua.TypeIDDouble})
	md.ConfigurationVersion = &ua.ConfigurationVersionDataType{}
	r := pubsub.SetTargetVariables(pubsub.DataSetReader("R", uint16(77), 10, 11, md), pubsub.Target(s.id(fxSetpoint)))
	r.Enabled = false
	cfg := &ua.PubSubConfiguration2DataType{
		PublishedDataSets: []*ua.PublishedDataSetDataType{pds},
		Connections: []*ua.PubSubConnectionDataType{
			pubsub.AttachWriterGroups(pubsub.UDPConnection("Pub", udp, uint16(77)), pubsub.WriterGroup("WG", 10, 10*time.Millisecond, w)),
			pubsub.AttachReaderGroups(pubsub.UDPConnection("Sub", udp, nil), pubsub.ReaderGroup("RG", r)),
		},
		Enabled: true,
	}
	enc, err := parse.ParseUAEncoding("json-reversible")
	if err != nil {
		t.Fatal(err)
	}
	b, err := enc.Encode(cfg, true)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "pubsub.json")
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return "pubsub.json"
}

// connectYAML is a request that creates the endpoint name of Motion
// (verifying Fixed against fixed), writes Gain, reserves ids, applies the
// loopback configuration of pubsubFile and enables it.
func connectYAML(name, pubsubFile string, fixed float64) string {
	return fmt.Sprintf(`commands: [VerifyFunctionalEntity, ReserveCommunicationIds, CreateConnectionEndpoint,
           SetConfigurationData, SetCommunicationConfiguration, EnableCommunication]
endpoints:
  - functionalEntity: "%[1]s"
    name: %[2]s
    mode: PublisherSubscriber
    inputs: ["%[3]s"]
    outputs: ["%[4]s"]
    cleanupTimeoutMs: -1
    relatedEndpoint: {address: "opc.tcp://peer:4840", name: FromMotion, path: ["http://opcfoundation.org/UA/FX/AC/;Remote"]}
    expectedVerificationVariables:
      - {node: "%[5]s", type: Double, value: %[8]v}
    configurationData:
      - {node: "%[6]s", type: Double, value: 2.5}
    communicationLinks:
      dataSetReader: {mask: [ReferenceReader], connection: 1}
      dataSetWriter: {mask: [ReferenceWriter]}
reserveCommunicationIds:
  - {transportProfileUri: "%[9]s", writerGroups: 1, dataSetWriters: 1, transportSpecificInfo: true}
communicationConfigurations:
  - pubsubConfigurationFile: %[7]s
    references:
      - {mask: [ElementAdd, ReferencePubDataset]}
      - {mask: [ElementAdd, ReferenceConnection], connection: 0}
      - {mask: [ElementAdd, ReferenceConnection], connection: 1}
      - {mask: [ElementAdd, ReferenceWriterGroup]}
      - {mask: [ElementAdd, ReferenceReaderGroup], connection: 1}
      - {mask: [ElementAdd, ReferenceWriter]}
      - {mask: [ElementAdd, ReferenceReader], connection: 1}
`, nsu(fxMotion), name, nsu(fxSetpoint), nsu(fxActual), nsu(fxFixed), nsu(fxGain), pubsubFile, fixed, fxUDPProfile)
}

func writeRequest(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func fxConnectJSON(t *testing.T, out string) model.FxConnectResult {
	t.Helper()
	var r model.FxConnectResult
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("%v:\n%s", err, out)
	}
	return r
}

// TestE2E_FxConnect establishes a connection endpoint with fx connect, sees
// data flow over UDP PubSub and closes it with fx close.
func TestE2E_FxConnect(t *testing.T) {
	s := startFxConnServer(t)
	dir := t.TempDir()
	req := writeRequest(t, dir, "connect.yaml", connectYAML("ToLine", s.pubsubFile(t, dir), 7))

	stdout, stderr, err := runCLI(t, "fx", "connect", "-e", s.url, "--ac", nsu(fxCtl), "--request", req, "--format", "json")
	if err != nil {
		t.Fatalf("fx connect: %v\n%s\n%s", err, stdout, stderr)
	}
	r := fxConnectJSON(t, stdout)
	if r.Status != "Good" || r.StatusCode != 0 || len(r.Endpoints) != 1 || r.AutomationComponent != s.id(fxCtl).String() {
		t.Fatalf("result: %+v", r)
	}
	if !strings.Contains(r.Commands, "CreateConnectionEndpoint") {
		t.Errorf("commands: %s", r.Commands)
	}
	e := r.Endpoints[0]
	if e.ConnectionEndpoint == "" || e.VerificationResult != "Match" || e.ConnectionEndpointResult != "Good" ||
		e.CommunicationLinksResult != "Good" || e.EnableCommunicationResult != "Good" || len(e.ConfigurationDataResults) != 1 {
		t.Errorf("endpoint: %+v", e)
	}
	if len(r.ReserveCommunicationIDs) != 1 || r.ReserveCommunicationIDs[0].Result != "Good" ||
		len(r.ReserveCommunicationIDs[0].WriterGroupIDs) != 1 || r.ReserveCommunicationIDs[0].TransportSpecificInfo == nil {
		t.Errorf("reserved ids: %+v", r.ReserveCommunicationIDs)
	}
	if len(r.CommunicationConfigurations) != 1 || r.CommunicationConfigurations[0].Result != "Good" || !r.CommunicationConfigurations[0].ChangesApplied {
		t.Errorf("pubsub configuration: %+v", r.CommunicationConfigurations)
	}
	if got := s.value(fxGain); got != 2.5 {
		t.Errorf("Gain %v, want 2.5", got)
	}

	// The data flows: the writer publishes Actual, the reader writes
	// Setpoint.
	s.set(t, fxActual, 42.5)
	deadline := time.Now().Add(10 * time.Second)
	for s.value(fxSetpoint) != 42.5 {
		if time.Now().After(deadline) {
			t.Fatalf("Setpoint %v, want 42.5", s.value(fxSetpoint))
		}
		time.Sleep(20 * time.Millisecond)
	}

	// The same name again: the server aborts the call.
	stdout, _, err = runCLI(t, "fx", "connect", "-e", s.url, "--ac", nsu(fxCtl), "--request", req, "--format", "json")
	if code := ExitCode(err); code != exitProtocol {
		t.Fatalf("second connect: exit %d (%v)\n%s", code, err, stdout)
	}
	if r := fxConnectJSON(t, stdout); r.Status != "Uncertain" {
		t.Errorf("second connect: %+v", r)
	}

	// fx close with --remove deletes the endpoint.
	stdout, stderr, err = runCLI(t, "fx", "close", "-e", s.url, "--ac", nsu(fxCtl), e.ConnectionEndpoint, "--remove", "--format", "json")
	if err != nil {
		t.Fatalf("fx close: %v\n%s\n%s", err, stdout, stderr)
	}
	var c model.FxCloseResult
	if err := json.Unmarshal([]byte(stdout), &c); err != nil {
		t.Fatalf("%v:\n%s", err, stdout)
	}
	if c.Status != "Good" || !c.Remove || len(c.Endpoints) != 1 || c.Endpoints[0].Status != "Good" || c.Endpoints[0].NodeID != e.ConnectionEndpoint {
		t.Errorf("close: %+v", c)
	}
	id, err := ua.ParseNodeID(e.ConnectionEndpoint)
	if err != nil {
		t.Fatal(err)
	}
	if s.srv.Node(id) != nil {
		t.Errorf("endpoint %s still there after close --remove", id)
	}

	// Closing it again: the endpoint is unknown.
	_, _, err = runCLI(t, "fx", "close", "-e", s.url, "--ac", nsu(fxCtl), e.ConnectionEndpoint)
	if code := ExitCode(err); code != exitProtocol {
		t.Errorf("close again: exit %d (%v)", code, err)
	}
}

// TestE2E_FxConnectAborted checks the result of a call the server aborts:
// a verification mismatch leaves nothing behind and exits 6 with the
// results printed.
func TestE2E_FxConnectAborted(t *testing.T) {
	s := startFxConnServer(t)
	dir := t.TempDir()
	req := writeRequest(t, dir, "connect.yaml", connectYAML("ToLine", s.pubsubFile(t, dir), 8))

	for _, f := range []string{"json", "table"} {
		stdout, _, err := runCLI(t, "fx", "connect", "-e", s.url, "--ac", nsu(fxCtl), "--request", req, "--format", f)
		if code := ExitCode(err); code != exitProtocol {
			t.Fatalf("%s: exit %d (%v)\n%s", f, code, err, stdout)
		}
		// The rollback of Table 21 keeps ConfigurationData and reserved
		// ids: the message must not say that nothing remains.
		const want = "EstablishConnections returned Uncertain: the call was aborted; rollback followed Part 81 Table 21, and some changes may remain"
		if err.Error() != want {
			t.Errorf("%s: error %q, want %q", f, err, want)
		}
		if f == "table" {
			for _, want := range []string{"Status:              Uncertain", "Verification:        Uncertain Mismatch (variables: BadOutOfRange)", "EnableCommunication: BadNothingToDo"} {
				if !strings.Contains(stdout, want) {
					t.Errorf("table does not contain %q:\n%s", want, stdout)
				}
			}
			continue
		}
		r := fxConnectJSON(t, stdout)
		if r.Status != "Uncertain" || len(r.Endpoints) != 1 {
			t.Fatalf("result: %+v", r)
		}
		e := r.Endpoints[0]
		if e.VerificationResult != "Mismatch" || e.ConnectionEndpoint != "" || e.ConnectionEndpointResult != "BadNothingToDo" ||
			e.EnableCommunicationResult != "BadNothingToDo" {
			t.Errorf("endpoint: %+v", e)
		}
	}
	if got := s.value(fxGain); got == 2.5 {
		t.Error("the aborted call wrote Gain")
	}
}

// TestE2E_FxConnectVerify verifies a FunctionalEntity without creating
// anything, with a preconfigured endpoint by its NodeId, and in JSON.
func TestE2E_FxConnectVerify(t *testing.T) {
	s := startFxConnServer(t)
	req := writeRequest(t, t.TempDir(), "verify.json", fmt.Sprintf(`{
  "commands": ["VerifyFunctionalEntity"],
  "endpoints": [{
    "functionalEntity": "ns=%[1]d;i=%[2]d",
    "node": "ns=%[1]d;i=%[3]d",
    "expectedVerificationVariables": [
      {"node": "ns=%[1]d;i=%[4]d", "type": "Double", "value": 7},
      {"node": "ns=%[1]d;i=%[5]d"}
    ]
  }]
}`, s.ns, fxMotion, fxPreset, fxFixed, fxGain))
	stdout, stderr, err := runCLI(t, "fx", "connect", "-e", s.url, "--ac", s.id(fxCtl).String(), "--request", req)
	if err != nil {
		t.Fatalf("%v\n%s\n%s", err, stdout, stderr)
	}
	for _, want := range []string{"Status:              Good", "Verification:        Good Match (variables: Good,Good)", "ConnectionEndpoint:  Good"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("output does not contain %q:\n%s", want, stdout)
		}
	}
}

// TestE2E_FxClose checks fx close of endpoints that cannot be closed: of
// another AutomationComponent, a node that is no endpoint, and of a
// preconfigured endpoint with one that is closed.
func TestE2E_FxClose(t *testing.T) {
	s := startFxConnServer(t)
	run := func(endpoints ...string) (model.FxCloseResult, int) {
		t.Helper()
		args := []string{"fx", "close", "-e", s.url, "--ac", nsu(fxCtl), "--format", "json"}
		args = append(args, endpoints...)
		stdout, _, err := runCLI(t, args...)
		var r model.FxCloseResult
		if jerr := json.Unmarshal([]byte(stdout), &r); jerr != nil {
			t.Fatalf("%v (%v):\n%s", jerr, err, stdout)
		}
		return r, ExitCode(err)
	}
	r, code := run(nsu(fxPreset))
	if code != exitOK || r.Status != "Good" || r.Endpoints[0].Status != "Good" {
		t.Errorf("preset: exit %d, %+v", code, r)
	}
	r, code = run(nsu(fxPreset), nsu(fxGain))
	if code != exitPartial || len(r.Endpoints) != 2 || r.Endpoints[0].Status != "Good" || r.Endpoints[1].Status != "BadInvalidArgument" {
		t.Errorf("preset and a variable: exit %d, %+v", code, r)
	}
	r, code = run(nsu(fxRemoteFE))
	if code != exitProtocol || r.Endpoints[0].Status == "Good" {
		t.Errorf("another component: exit %d, %+v", code, r)
	}
	stdout := mustRun(t, "fx", "close", "-e", s.url, "--ac", nsu(fxCtl), nsu(fxPreset))
	if !strings.Contains(stdout, "ENDPOINT") || !strings.Contains(stdout, "Good") {
		t.Errorf("table:\n%s", stdout)
	}
	for _, c := range []struct {
		name string
		args []string
		want int
	}{
		{"bad endpoint", []string{"--ac", nsu(fxCtl), "x=y"}, exitUsage},
		{"not a component", []string{"--ac", nsu(fxMotion), nsu(fxPreset)}, exitProtocol},
		{"no endpoint", []string{"--ac", nsu(fxCtl)}, exitUsage},
	} {
		_, _, err := runCLI(t, append([]string{"fx", "close", "-e", s.url}, c.args...)...)
		if code := ExitCode(err); code != c.want {
			t.Errorf("%s: exit %d, want %d (%v)", c.name, code, c.want, err)
		}
	}
}

// TestE2E_FxConnectErrors checks the exit codes of requests that cannot be
// sent and of servers that cannot take them.
func TestE2E_FxConnectErrors(t *testing.T) {
	s := startFxConnServer(t)
	notFX := startFxServerWithoutModel(t)
	dir := t.TempDir()
	good := writeRequest(t, dir, "good.yaml", "commands: [VerifyFunctionalEntity]\nendpoints:\n  - {functionalEntity: \""+nsu(fxMotion)+"\", node: \""+nsu(fxPreset)+"\"}\n")
	for _, c := range []struct {
		name    string
		args    []string
		request string
		want    int
		stderr  string
	}{
		{"missing file", []string{"--ac", nsu(fxCtl), "--request", filepath.Join(dir, "none.yaml")}, "", exitInvalidInput, "reading --request"},
		{"unknown command", []string{"--ac", nsu(fxCtl)}, "commands: [Connect]\n", exitUsage, `unknown command "Connect"`},
		{"no commands", []string{"--ac", nsu(fxCtl)}, "endpoints: []\n", exitUsage, "no commands"},
		{"unknown field", []string{"--ac", nsu(fxCtl)}, "commands: [VerifyAsset]\nassets: []\n", exitUsage, `unknown field "assets"`},
		{"not an object", []string{"--ac", nsu(fxCtl)}, "- VerifyAsset\n", exitUsage, "want an object"},
		{"bad json", []string{"--ac", nsu(fxCtl)}, "{\"commands\": [", exitUsage, "request file"},
		{"unknown namespace", []string{"--ac", "nsu=urn:none;i=1", "--request", good}, "", exitUsage, "--ac"},
		{"bad node", []string{"--ac", nsu(fxCtl)}, "commands: [VerifyFunctionalEntity]\nendpoints: [{functionalEntity: \"x=y\"}]\n", exitUsage, "endpoints[0].functionalEntity"},
		{"value without type", []string{"--ac", nsu(fxCtl)}, "commands: [VerifyFunctionalEntity]\nendpoints: [{functionalEntity: \"" + nsu(fxMotion) + "\", node: \"" + nsu(fxPreset) + "\", expectedVerificationVariables: [{node: \"" + nsu(fxFixed) + "\", value: 7}]}]\n", exitUsage, "a value needs a type"},
		{"node and name", []string{"--ac", nsu(fxCtl)}, "commands: [VerifyFunctionalEntity]\nendpoints: [{functionalEntity: \"" + nsu(fxMotion) + "\", node: \"" + nsu(fxPreset) + "\", name: X}]\n", exitUsage, "exclusive"},
		{"bad call", []string{"--ac", nsu(fxCtl)}, "commands: [VerifyAsset]\nendpoints:\n  - {functionalEntity: \"" + nsu(fxMotion) + "\", node: \"" + nsu(fxPreset) + "\"}\n", exitProtocol, "EstablishConnections"},
		{"not a component", []string{"--ac", nsu(fxMotion), "--request", good}, "", exitProtocol, "EstablishConnections"},
		{"not a UAFX server", []string{"-e", notFX, "--ac", "ns=1;i=1", "--request", good}, "", exitProtocol, "no UAFX AC namespace"},
	} {
		t.Run(c.name, func(t *testing.T) {
			args := append([]string{"fx", "connect", "-e", s.url}, c.args...)
			if c.request != "" {
				args = append(args, "--request", writeRequest(t, t.TempDir(), "r.yaml", c.request))
			}
			_, stderr, err := runCLI(t, args...)
			if code := ExitCode(err); code != c.want {
				t.Errorf("exit %d, want %d (%v)", code, c.want, err)
			}
			if msg := stderr + fmt.Sprint(err); !strings.Contains(msg, c.stderr) {
				t.Errorf("error does not contain %q: %s", c.stderr, msg)
			}
		})
	}
}

// startFxServerWithoutModel starts a server without the UAFX model.
func startFxServerWithoutModel(t *testing.T) string {
	t.Helper()
	srv, err := server.New(server.EndPoint("localhost", 0), server.InsecureNoSecurity(), server.EnableAuthMode(ua.UserTokenTypeAnonymous))
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	return fmt.Sprintf("opc.tcp://localhost:%d", srv.Port())
}
