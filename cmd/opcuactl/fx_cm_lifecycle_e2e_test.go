// SPDX-License-Identifier: MIT

package opcuactl

import (
	"testing"
	"time"

	"github.com/otfabric/go-opcua/pubsub"
	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/go-opcua/uafx"
	"github.com/otfabric/go-opcua/uafx/fxacid"
	"github.com/otfabric/go-opcua/uafx/fxdataid"
	"github.com/otfabric/opcuactl/internal/model"
)

// The namespace indexes of the Namespaces of the ServerAddresses of
// cmLifecycleConf.
const (
	cmlTest = 1
	cmlAC   = 2
	cmlData = 3
)

// cmLifecycleModel is the PubSubConfiguration of an AutomationComponent of
// cmLifecycleConf, as go-opcua's fxcm tests give it: Motion publishes
// Actual on out and writes what it reads on in to Setpoint.
func cmLifecycleModel(out, in string) *uafx.PubSubCommunicationModelConfiguration {
	n := func(id uint32) *ua.NodeID { return ua.NewNumericNodeID(cmlTest, id) }
	pds := pubsub.PublishedDataItems("PDS", pubsub.Var("x", n(fxActual), ua.TypeIDDouble))
	w := pubsub.DataSetWriter("W", 0, "PDS")
	wg := pubsub.WriterGroup("", 0, 10*time.Millisecond, w)
	md := pubsub.MetaData("m", pubsub.Field{Name: "x", Type: ua.TypeIDDouble})
	r := pubsub.SetTargetVariables(pubsub.DataSetReader("R", nil, 0, 0, md), pubsub.Target(n(fxSetpoint)))
	add, match := ua.PubSubConfigurationRefMaskElementAdd, ua.PubSubConfigurationRefMaskElementMatch
	ref := func(m ua.PubSubConfigurationRefMask, c uint16) *ua.PubSubConfigurationRefDataType {
		if m&(ua.PubSubConfigurationRefMaskReferenceConnection|ua.PubSubConfigurationRefMaskReferenceReaderGroup) != 0 {
			m |= match
		}
		return &ua.PubSubConfigurationRefDataType{ConfigurationMask: add | m, ConnectionIndex: c}
	}
	return &uafx.PubSubCommunicationModelConfiguration{
		PubSubConfiguration: &ua.PubSubConfiguration2DataType{
			PublishedDataSets: []*ua.PublishedDataSetDataType{pds},
			Connections: []*ua.PubSubConnectionDataType{
				pubsub.AttachWriterGroups(pubsub.UDPConnection("Pub", out, nil), wg),
				pubsub.AttachReaderGroups(pubsub.UDPConnection("Sub", in, nil), pubsub.ReaderGroup("RG", r)),
			},
			Enabled: true,
		},
		ConfigurationReferences: []*ua.PubSubConfigurationRefDataType{
			ref(ua.PubSubConfigurationRefMaskReferencePubDataset, 0),
			ref(ua.PubSubConfigurationRefMaskReferenceConnection, 0),
			ref(ua.PubSubConfigurationRefMaskReferenceConnection, 1),
			ref(ua.PubSubConfigurationRefMaskReferenceWriterGroup, 0),
			ref(ua.PubSubConfigurationRefMaskReferenceReaderGroup, 1),
			ref(ua.PubSubConfigurationRefMaskReferenceWriter, 0),
			ref(ua.PubSubConfigurationRefMaskReferenceReader, 1),
		},
	}
}

// cmLifecycleConf is a set with the bidirectional Connection Motion
// between Motion of a and Motion of b: a publishes on ab, b on ba.
func cmLifecycleConf(t *testing.T, name string, a, b *fxConnServer) *uafx.ConnectionConfigurationSetConf {
	t.Helper()
	ab, ba := freeUDPURL(t), freeUDPURL(t)
	n := func(id uint32) *uafx.NodeIdentifier {
		return &uafx.NodeIdentifier{Node: ua.NewNumericNodeID(cmlTest, id)}
	}
	links, err := uafx.EncodeStructure(&uafx.PubSubCommunicationLinkConfiguration{
		DataSetWriterRef: &ua.PubSubConfigurationRefDataType{ConfigurationMask: ua.PubSubConfigurationRefMaskReferenceWriter},
		DataSetReaderRef: &ua.PubSubConfigurationRefDataType{ConfigurationMask: ua.PubSubConfigurationRefMaskReferenceReader, ConnectionIndex: 1},
	}, uafx.NamespaceIndexes{Data: cmlData, AC: cmlAC})
	if err != nil {
		t.Fatal(err)
	}
	address := func(browseName string, s *fxConnServer) *uafx.ServerAddressConf {
		return &uafx.ServerAddressConf{
			BrowseName: browseName, Address: s.url, SecurityMode: ua.MessageSecurityModeNone, ServerURI: fxAppURI,
			Namespaces: []string{"http://opcfoundation.org/UA/", fxConnNS, fxacid.NamespaceURI, fxdataid.NamespaceURI},
		}
	}
	endpoint := func(ac, out, in int32, name string) *uafx.ConnectionEndpointConfigurationConf {
		return &uafx.ConnectionEndpointConfigurationConf{
			FunctionalEntityNode:     n(fxMotion),
			Name:                     name,
			ConnectionEndpointTypeID: ua.NewNumericNodeID(cmlAC, fxacid.PubSubConnectionEndpointType),
			InputVariableIDs:         []*uafx.NodeIdentifier{n(fxSetpoint)},
			OutputVariableIDs:        []*uafx.NodeIdentifier{n(fxActual)},
			CleanupTimeout:           -1,
			CommunicationLinks:       links,
			ConfigurationData:        []*uafx.NodeIdentifierValuePair{{Key: n(fxGain), Value: ua.MustVariant(2.5)}},
			AutomationComponentIndex: ac,
			OutboundFlowIndex:        &out,
			InboundFlowIndex:         []int32{in, 0},
		}
	}
	flow := func(name, url string) uafx.CommunicationFlowConfigurationConf {
		return &uafx.PubSubCommunicationFlowConfigurationConf{
			BrowseName: name, Address: &uafx.AddressSelection{Address: ua.NewExtensionObject(&ua.NetworkAddressURLDataType{URL: url})},
			SubscriberConfigurations: []*uafx.SubscriberConfigurationConf{{BrowseName: "S", MessageReceiveTimeout: 1000}},
		}
	}
	return &uafx.ConnectionConfigurationSetConf{
		BrowseName:                       name,
		ConnectionConfigurationSetFolder: []string{"Plant"},
		RollbackOnError:                  true,
		ServerAddresses:                  []*uafx.ServerAddressConf{address("A", a), address("B", b)},
		AutomationComponentConfigurations: []*uafx.AutomationComponentConfigurationConf{
			{BrowseName: "A", AutomationComponentNode: n(fxCtl), ServerAddressIndex: 0, CommunicationModelConfig: cmLifecycleModel(ab, ba)},
			{BrowseName: "B", AutomationComponentNode: n(fxCtl), ServerAddressIndex: 1, CommunicationModelConfig: cmLifecycleModel(ba, ab)},
		},
		CommunicationFlows: []uafx.CommunicationFlowConfigurationConf{flow("AB", ab), flow("BA", ba)},
		Connections: []*uafx.ConnectionConfigurationConf{{
			BrowseName: "Motion", Endpoint1: endpoint(0, 0, 1, "ToB"), Endpoint2: endpoint(1, 1, 0, "ToA"),
		}},
	}
}

// endpoints are the names of the ConnectionEndpoints the ConnectionManager
// created on s.
func (s *fxConnServer) endpoints() []string {
	var out []string
	for _, e := range s.conns.Endpoints() {
		if nd := s.srv.Node(e); nd != nil && !e.Equal(s.id(fxPreset)) {
			out = append(out, nd.BrowseName().Name)
		}
	}
	return out
}

// TestE2E_FxCMLifecycle drives a working ConnectionManager through the CLI
// only: two AutomationComponents, a set imported from a file, establish
// with --wait (Ready, endpoints on both, data both ways), disable, enable
// and remove (no endpoints left).
func TestE2E_FxCMLifecycle(t *testing.T) {
	a, b := startFxConnServer(t), startFxConnServer(t)
	b.set(t, fxActual, 17.0)
	s := startFxCMServer(t)
	file := writeSetFile(t, cmLifecycleConf(t, "Line", a, b))
	if out, stderr, err := runCLI(t, "fx", "cm", "import", "-e", s.url, "--file", file, "--op", "add"); err != nil {
		t.Fatalf("import: %v\n%s%s", err, out, stderr)
	}
	process := func(action string) {
		t.Helper()
		out, stderr, err := runCLI(t, "fx", "cm", "process", "-e", s.url, "--action", action, "Plant/Line", "--wait", "30s", "--format", "json")
		if err != nil {
			t.Fatalf("%s: %v\n%s%s", action, err, out, stderr)
		}
		r := cmJSON[model.FxCMCallResult](t, out)
		if len(r.Sets) != 1 || r.Sets[0].State != "Ready" {
			t.Fatalf("%s: %+v", action, r)
		}
	}
	eventually := func(what string, ok func() bool) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for !ok() {
			if time.Now().After(deadline) {
				t.Fatalf("%s: not within 10s", what)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	equal := func(x, y []string) bool { return len(x) == len(y) && (len(x) == 0 || x[0] == y[0]) }

	process("establish-enabled")
	if !equal(a.endpoints(), []string{"ToB"}) || !equal(b.endpoints(), []string{"ToA"}) {
		t.Fatalf("endpoints %v %v", a.endpoints(), b.endpoints())
	}
	if g := a.value(fxGain); g != 2.5 {
		t.Fatalf("ConfigurationData Gain %v, want 2.5", g)
	}
	eventually("data from a to b", func() bool { return b.value(fxSetpoint) == 42.5 })
	eventually("data from b to a", func() bool { return a.value(fxSetpoint) == 17.0 })
	out, _, err := runCLI(t, "fx", "cm", "list", "-e", s.url, "--format", "json")
	if err != nil {
		t.Fatal(err)
	}
	l := cmJSON[model.FxCMList](t, out)
	if len(l.Sets) != 1 || l.Sets[0].StateID != "Ready" || len(l.Sets[0].Connections) != 1 || l.Sets[0].Connections[0].ProcessingResult != "Good" || len(l.Sets[0].Unread) != 0 {
		t.Fatalf("list: %+v", l)
	}

	// Disabled, the data stops: a new value of Actual no longer arrives.
	process("disable")
	a.set(t, fxActual, 99.0)
	time.Sleep(300 * time.Millisecond)
	if v := b.value(fxSetpoint); v == 99.0 {
		t.Fatal("data flows while the Connection is disabled")
	}
	process("enable")
	eventually("data after enable", func() bool { return b.value(fxSetpoint) == 99.0 })

	process("remove")
	if len(a.endpoints()) != 0 || len(b.endpoints()) != 0 {
		t.Fatalf("endpoints after remove: %v %v", a.endpoints(), b.endpoints())
	}
}
