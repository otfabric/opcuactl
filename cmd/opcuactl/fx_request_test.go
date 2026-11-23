// SPDX-License-Identifier: MIT

package opcuactl

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/go-opcua/uafx"
)

var testNamespaces = []string{"http://opcfoundation.org/UA/", "urn:server", "urn:ctl", "http://opcfoundation.org/UA/FX/AC/"}

func buildRequest(t *testing.T, doc string) (*uafx.EstablishConnectionsRequest, error) {
	t.Helper()
	f, err := decodeFxRequest([]byte(doc))
	if err != nil {
		return nil, err
	}
	b := &fxRequestBuilder{namespaces: testNamespaces, ac: 3}
	return b.build(f)
}

// TestFxRequestBuild checks the mapping of every field of the request file
// to the request of uafx.
func TestFxRequestBuild(t *testing.T) {
	req, err := buildRequest(t, `
commands: [VerifyAssetCmd, verifyfunctionalentity, CreateConnectionEndpoint, EstablishControl,
           SetConfigurationData, ReassignControl, ReserveCommunicationIds,
           SetCommunicationConfiguration, EnableCommunication]
assetVerifications:
  - asset: nsu=urn:ctl;i=1100
    mode: IdentityAndCompatibility
    expectedResult: compatible
    variables:
      - {key: "nsu=urn:ctl;Manufacturer", type: String, value: ACME}
      - {key: "3:Model", type: ua-json, value: {"Type": 12, "Body": "M1"}}
      - {key: SerialNumber}
    additionalVariables:
      - {node: "ns=2;i=7", index: [1, 2], type: Int32, value: [1, 2]}
endpoints:
  - functionalEntity: "ns=2;i=1200"
    name: ToLine
    type: "ns=3;i=99"
    mode: subscriber
    inputs: ["ns=2;i=1"]
    outputs: []
    persistent: true
    cleanupTimeoutMs: 2500
    preconfigured: true
    relatedEndpoint: {address: "opc.tcp://peer", name: Other, path: ["urn:x;A", "B"]}
    controlGroups: ["ns=2;i=1251"]
    communicationLinks:
      dataSetReader: {mask: [ReferenceReader, PubSubConfigurationRefMaskElementMatch], connection: 1, group: 2, element: 3}
      expectedSubscribedDataSetVersion: {major: 4, minor: 5}
      expectedPublishedDataSetVersion: {major: 6, minor: 7}
  - functionalEntity: "ns=2;i=1200"
    node: "ns=2;i=1300"
reserveCommunicationIds:
  - {transportProfileUri: urn:udp, writerGroups: 2, dataSetWriters: 3, transportSpecificInfo: true}
communicationConfigurations:
  - pubsubConfiguration: {"Enabled": true}
    requireCompleteUpdate: true
    references: [{mask: [ElementRemove, ReferenceSecurityGroup, ReferencePushTarget, ReferenceSubDataset,
                         ElementModify, ReferenceWriterGroup, ReferenceReaderGroup]}]
`)
	if err != nil {
		t.Fatal(err)
	}
	if req.CommandMask != uafx.AllFxCommands {
		t.Errorf("commands %s", req.CommandMask)
	}
	a := req.AssetVerifications[0]
	if a.AssetToVerify.String() != "ns=2;i=1100" || a.VerificationMode != uafx.AssetIdentityAndCompatibility ||
		a.ExpectedVerificationResult != uafx.AssetVerificationCompatible {
		t.Errorf("asset: %+v", a)
	}
	if kv := a.ExpectedVerificationVariables; len(kv) != 3 || kv[0].Key.NamespaceIndex != 2 || kv[0].Key.Name != "Manufacturer" ||
		kv[0].Value.Value() != "ACME" || kv[1].Key.NamespaceIndex != 3 || kv[1].Value.Value() != "M1" || kv[2].Value != nil {
		t.Errorf("variables: %+v", kv)
	}
	if p := a.ExpectedAdditionalVerificationVariables[0]; p.Key.Node.String() != "ns=2;i=7" || len(p.Key.ArrayIndex) != 2 {
		t.Errorf("additional: %+v", p)
	} else if v, ok := p.Value.Value().([]int32); !ok || len(v) != 2 {
		t.Errorf("additional value %T", p.Value.Value())
	}
	e := req.ConnectionEndpointConfigurations[0]
	p := e.ConnectionEndpoint.Parameter
	if p == nil || p.Name != "ToLine" || p.ConnectionEndpointTypeID.String() != "ns=3;i=99" || p.Mode != uafx.PubSubSubscriber ||
		!p.IsPersistent || p.CleanupTimeout != 2500 || !p.IsPreconfigured || len(p.InputVariableIDs) != 1 || p.OutputVariableIDs == nil {
		t.Fatalf("parameter: %+v", p)
	}
	if r := p.RelatedEndpoint; r.Address != "opc.tcp://peer" || r.ConnectionEndpointName != "Other" || len(r.ConnectionEndpointPath) != 2 ||
		r.ConnectionEndpointPath[0].NamespaceURI != "urn:x" || r.ConnectionEndpointPath[0].Name != "A" || r.ConnectionEndpointPath[1].Name != "B" {
		t.Errorf("related: %+v", r)
	}
	if len(e.ControlGroups) != 1 {
		t.Errorf("control groups: %v", e.ControlGroups)
	}
	l := e.CommunicationLinks
	if l.DataSetReaderRef.ConfigurationMask != ua.PubSubConfigurationRefMaskReferenceReader|ua.PubSubConfigurationRefMaskElementMatch ||
		l.DataSetReaderRef.ConnectionIndex != 1 || l.DataSetReaderRef.GroupIndex != 2 || l.DataSetReaderRef.ElementIndex != 3 ||
		l.DataSetWriterRef != nil || l.ExpectedSubscribedDataSetVersion.MinorVersion != 5 || l.ExpectedPublishedDataSetVersion.MajorVersion != 6 {
		t.Errorf("links: %+v", l)
	}
	if d := req.ConnectionEndpointConfigurations[1].ConnectionEndpoint; d.Parameter != nil || d.Node.String() != "ns=2;i=1300" {
		t.Errorf("definition by node: %+v", d)
	}
	if r := req.ReserveCommunicationIDs[0]; r.TransportProfileURI != "urn:udp" || r.NumReqWriterGroupIDs != 2 || r.NumReqDataSetWriterIDs != 3 || !r.RequestTransportSpecificInfo {
		t.Errorf("reserve: %+v", r)
	}
	c := req.CommunicationConfigurations[0]
	if !c.PubSubConfiguration.Enabled || !c.RequireCompleteUpdate || len(c.ConfigurationReferences) != 1 || c.ConfigurationReferences[0].ConfigurationMask != 0x1ccc {
		t.Errorf("configuration: %+v %+v", c, c.ConfigurationReferences[0])
	}
}

// TestFxRequestDefaults checks the defaults of a new endpoint: the
// PubSubConnectionEndpointType of the AC namespace and no cleanup.
func TestFxRequestDefaults(t *testing.T) {
	req, err := buildRequest(t, `{"commands": ["CreateConnectionEndpoint"], "endpoints": [{"functionalEntity": "ns=2;i=1", "name": "E", "mode": "Publisher"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	p := req.ConnectionEndpointConfigurations[0].ConnectionEndpoint.Parameter
	if p.ConnectionEndpointTypeID.Namespace() != 3 || p.CleanupTimeout != -1 || p.Mode != uafx.PubSubPublisher || p.RelatedEndpoint != nil {
		t.Errorf("parameter: %+v", p)
	}
}

// TestFxRequestErrors checks that a request file that is not valid is
// refused before a call, with the field named.
func TestFxRequestErrors(t *testing.T) {
	ep := "endpoints: [{functionalEntity: \"ns=2;i=1\", node: \"ns=2;i=2\", "
	for _, c := range []struct{ doc, want string }{
		{"", "want an object"},
		{"commands: [", "request file"},
		{`{"commands": []} {}`, "trailing data"},
		{"commands: []\n", "no commands"},
		{"commands: [VerifyAsset]\nassetVerifications: [{mode: Identity, expectedResult: Match}]\n", "assetVerifications[0].asset: no NodeId"},
		{"commands: [VerifyAsset]\nassetVerifications: [{asset: \"ns=2;i=1\", mode: Exact, expectedResult: Match}]\n", `unknown mode "Exact"`},
		{"commands: [VerifyAsset]\nassetVerifications: [{asset: \"ns=2;i=1\", mode: Identity, expectedResult: Equal}]\n", `unknown result "Equal"`},
		{"commands: [VerifyAsset]\nassetVerifications: [{asset: \"ns=2;i=1\", mode: Identity, expectedResult: Match, variables: [{key: \"\"}]}]\n", "variables[0].key: no name"},
		{"commands: [VerifyAsset]\nassetVerifications: [{asset: \"ns=2;i=1\", mode: Identity, expectedResult: Match, variables: [{key: \"nsu=urn:none;X\"}]}]\n", "no namespace urn:none"},
		{"commands: [VerifyAsset]\nassetVerifications: [{asset: \"ns=2;i=1\", mode: Identity, expectedResult: Match, variables: [{key: \"nsu=urn:ctl\"}]}]\n", "is not nsu=<uri>;<name>"},
		{"commands: [VerifyAsset]\nassetVerifications: [{asset: \"ns=2;i=1\", mode: Identity, expectedResult: Match, variables: [{key: K, type: Int32, value: x}]}]\n", "variables[0].value"},
		{"commands: [VerifyAsset]\nassetVerifications: [{asset: \"ns=2;i=1\", mode: Identity, expectedResult: Match, additionalVariables: [{node: \"\"}]}]\n", "additionalVariables[0].node"},
		{"commands: [VerifyAsset]\nassetVerifications: [{asset: \"nsu=urn:none;i=1\", mode: Identity, expectedResult: Match}]\n", "urn:none"},
		{"commands: [X]\n" + ep + "expectedVerificationVariables: [{node: \"ns=2;i=3\", type: Int32}]}]\n", "unknown command"},
		{"commands: [VerifyFunctionalEntity]\n" + ep + "expectedVerificationVariables: [{node: \"ns=2;i=3\", type: Int32}]}]\n", "type Int32 without a value"},
		{"commands: [VerifyFunctionalEntity]\n" + ep + "expectedVerificationVariables: [{node: \"ns=2;i=3\", type: Quaternion, value: 1}]}]\n", "unknown built-in type"},
		{"commands: [VerifyFunctionalEntity]\n" + ep + "expectedVerificationVariables: [{node: \"ns=2;i=3\", type: ua-xml, value: \"<x/>\"}]}]\n", "only ua-json"},
		{"commands: [VerifyFunctionalEntity]\n" + ep + "expectedVerificationVariables: [{node: \"ns=2;i=3\", type: ua-json, value: [1}]}]\n", "request file"},
		{"commands: [VerifyFunctionalEntity]\n" + ep + "expectedVerificationVariables: [{node: \"ns=2;i=3\", type: ua-json, value: {\"Type\": \"x\"}}]}]\n", "expectedVerificationVariables[0].value"},
		{"commands: [VerifyFunctionalEntity]\n" + ep + "expectedVerificationVariables: [{node: \"ns=2;i=3\", type: Byte, value: 300}]}]\n", "expectedVerificationVariables[0].value"},
		{"commands: [VerifyFunctionalEntity]\n" + ep + "controlGroups: [\"\"]}]\n", "controlGroups[0]: no NodeId"},
		{"commands: [VerifyFunctionalEntity]\n" + ep + "configurationData: [{node: \"ns=2;i=3\", value: 1}]}]\n", "configurationData[0].value: a value needs a type"},
		{"commands: [VerifyFunctionalEntity]\n" + ep + "communicationLinks: {dataSetReader: {mask: [Reader]}}}]\n", `dataSetReader: unknown mask bit "Reader"`},
		{"commands: [VerifyFunctionalEntity]\n" + ep + "communicationLinks: {dataSetWriter: {mask: [Writer]}}}]\n", `dataSetWriter: unknown mask bit "Writer"`},
		{"commands: [VerifyFunctionalEntity]\nendpoints: [{functionalEntity: \"ns=2;i=1\", node: \"bad node\"}]\n", "endpoints[0].node"},
		{"commands: [CreateConnectionEndpoint]\nendpoints: [{functionalEntity: \"ns=2;i=1\"}]\n", "endpoints[0]: no name"},
		{"commands: [CreateConnectionEndpoint]\nendpoints: [{functionalEntity: \"ns=2;i=1\", name: E, mode: Both}]\n", `unknown mode "Both"`},
		{"commands: [CreateConnectionEndpoint]\nendpoints: [{functionalEntity: \"ns=2;i=1\", name: E, mode: Publisher, type: \"bad node\"}]\n", "endpoints[0].type"},
		{"commands: [CreateConnectionEndpoint]\nendpoints: [{functionalEntity: \"ns=2;i=1\", name: E, mode: Publisher, inputs: [\"\"]}]\n", "endpoints[0].inputs[0]"},
		{"commands: [CreateConnectionEndpoint]\nendpoints: [{functionalEntity: \"ns=2;i=1\", name: E, mode: Publisher, outputs: [\"\"]}]\n", "endpoints[0].outputs[0]"},
		{"commands: [CreateConnectionEndpoint]\nendpoints: [{functionalEntity: \"ns=2;i=1\", name: E, mode: Publisher, expectedVerificationVariables: [{node: \"\"}]}]\n", "expectedVerificationVariables[0].node"},
		{"commands: [SetCommunicationConfiguration]\ncommunicationConfigurations: [{}]\n", "no pubsubConfiguration"},
		{"commands: [SetCommunicationConfiguration]\ncommunicationConfigurations: [{pubsubConfiguration: {Enabled: 3}}]\n", "communicationConfigurations[0].pubsubConfiguration"},
		{"commands: [SetCommunicationConfiguration]\ncommunicationConfigurations: [{pubsubConfiguration: {}, references: [{mask: [Add]}]}]\n", "references[0]: unknown mask bit"},
	} {
		_, err := buildRequest(t, c.doc)
		if err == nil || !strings.Contains(err.Error(), c.want) || !errors.Is(err, errFxRequest) {
			t.Errorf("%q: error %v, want %q", c.doc, err, c.want)
		}
	}
}

// TestReadFxRequest checks the files of a request: a PubSub configuration
// relative to the request file, and the errors of files that cannot be
// read.
func TestReadFxRequest(t *testing.T) {
	dir := t.TempDir()
	write := func(name, s string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	write("ps.json", `{"Enabled": true}`)
	f, err := readFxRequest(write("r.yaml", "commands: [SetCommunicationConfiguration]\ncommunicationConfigurations: [{pubsubConfigurationFile: ps.json}, {pubsubConfiguration: {}}]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if string(f.CommunicationConfigurations[0].PubSubConfiguration) != `{"Enabled": true}` {
		t.Errorf("configuration: %s", f.CommunicationConfigurations[0].PubSubConfiguration)
	}
	abs := filepath.Join(dir, "ps.json")
	if _, err := readFxRequest(write("abs.yaml", "commands: [X]\ncommunicationConfigurations: [{pubsubConfigurationFile: \""+abs+"\"}]\n")); err != nil {
		t.Errorf("absolute path: %v", err)
	}
	for _, c := range []struct {
		path string
		code int
		want string
	}{
		{filepath.Join(dir, "none"), exitInvalidInput, "reading --request"},
		{write("bad.yaml", "commands: ["), exitUsage, "request file"},
		{write("both.yaml", "commands: [X]\ncommunicationConfigurations: [{pubsubConfigurationFile: ps.json, pubsubConfiguration: {}}]\n"), exitUsage, "exclusive"},
		{write("gone.yaml", "commands: [X]\ncommunicationConfigurations: [{pubsubConfigurationFile: gone.json}]\n"), exitInvalidInput, "pubsubConfigurationFile"},
	} {
		_, err := readFxRequest(c.path)
		if ExitCode(err) != c.code || err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: exit %d, %v; want %d, %q", c.path, ExitCode(err), err, c.code, c.want)
		}
	}
}

// TestFxCloseExit checks the exit of fx close: 0 when every endpoint was
// closed, 7 when some were, 6 when none was.
func TestFxCloseExit(t *testing.T) {
	for _, c := range []struct {
		status    ua.StatusCode
		closed, n int
		want      int
	}{
		{ua.StatusOK, 2, 2, exitOK},
		{ua.StatusUncertain, 1, 2, exitPartial},
		{ua.StatusUncertain, 0, 2, exitProtocol},
		{ua.StatusOK, 0, 1, exitProtocol},
	} {
		if got := ExitCode(fxCloseExit(c.status, c.closed, c.n)); got != c.want {
			t.Errorf("%v %d/%d: exit %d, want %d", c.status, c.closed, c.n, got, c.want)
		}
	}
}

// TestFxConnectResult checks the mapping of the parts of a result the test
// server does not return: nil elements, asset verification, configuration
// values and objects, a null endpoint id and unknown enumeration values.
func TestFxConnectResult(t *testing.T) {
	r := fxConnectResult(ua.NewNumericNodeID(2, 1), uafx.VerifyAssetCmd, &uafx.EstablishConnectionsResult{
		Status: ua.StatusUncertain,
		AssetVerificationResults: []*uafx.AssetVerificationResultData{nil, {
			VerificationStatus: ua.StatusBadInvalidState, VerificationResult: uafx.AssetVerificationMismatch,
			VerificationVariablesErrors: []ua.StatusCode{ua.StatusOK}, VerificationAdditionalVariablesErrors: []ua.StatusCode{ua.StatusBadOutOfRange},
		}},
		ConnectionEndpointConfigurationResults: []*uafx.ConnectionEndpointConfigurationResult{nil, {
			ConnectionEndpointID: ua.NewNumericNodeID(0, 0), VerificationResult: 9,
			EstablishControlResult: []ua.StatusCode{ua.StatusBadNotSupported}, ReassignControlResult: []ua.StatusCode{ua.StatusBadNothingToDo},
		}},
		ReserveCommunicationIDsResults: []*uafx.PubSubReserveCommunicationIDsResult{nil, {DefaultPublisherID: ua.MustVariant(uint16(7))}},
		CommunicationConfigurationResults: []*uafx.PubSubCommunicationConfigurationResult{nil, {
			ConfigurationValues: []*ua.PubSubConfigurationValueDataType{nil, {
				ConfigurationElement: &ua.PubSubConfigurationRefDataType{ConfigurationMask: ua.PubSubConfigurationRefMaskElementAdd | ua.PubSubConfigurationRefMaskReferenceWriter, ElementIndex: 2},
				Name:                 "W", Identifier: ua.MustVariant(uint16(11)),
			}, {Name: "none"}},
			ConfigurationObjects: []*ua.NodeID{nil, ua.NewNumericNodeID(1, 5)},
		}},
	})
	if r.Status != "Uncertain" || r.Commands != "VerifyAsset" || len(r.AssetVerifications) != 1 || len(r.Endpoints) != 1 ||
		len(r.ReserveCommunicationIDs) != 1 || len(r.CommunicationConfigurations) != 1 {
		t.Fatalf("result: %+v", r)
	}
	if a := r.AssetVerifications[0]; a.Result != "Mismatch" || a.Status != "BadInvalidState" || a.AdditionalVariableErrors[0] != "BadOutOfRange" {
		t.Errorf("asset: %+v", a)
	}
	if e := r.Endpoints[0]; e.ConnectionEndpoint != "" || e.VerificationResult != "9" || e.EstablishControlResults[0] != "BadNotSupported" {
		t.Errorf("endpoint: %+v", e)
	}
	if ids := r.ReserveCommunicationIDs[0]; ids.DefaultPublisherID != uint16(7) || ids.WriterGroupIDs == nil || ids.TransportSpecificInfo != nil {
		t.Errorf("ids: %+v", ids)
	}
	c := r.CommunicationConfigurations[0]
	if len(c.ConfigurationValues) != 2 || c.ConfigurationValues[0].Element != "ElementAdd|ReferenceWriter connection=0 group=0 element=2" ||
		c.ConfigurationValues[0].Identifier != uint16(11) || c.ConfigurationValues[1].Element != "" || len(c.ConfigurationObjects) != 1 {
		t.Errorf("configuration: %+v", c)
	}

	// The table shows every part.
	stdout := captureStdout(t, func() { printFxConnectResult(r) })
	for _, want := range []string{"Asset 0: BadInvalidState Mismatch (variables: Good) (additional: BadOutOfRange)",
		"ControlGroups:       establish BadNotSupported, reassign BadNothingToDo", "Endpoint 0: -", `"W": 11`} {
		if !strings.Contains(stdout, want) {
			t.Errorf("table does not contain %q:\n%s", want, stdout)
		}
	}
}
