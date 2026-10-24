// SPDX-License-Identifier: MIT

package opcuactl

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-opcua/pubsub"
	"github.com/otfabric/go-opcua/pubsub/message"
	"github.com/otfabric/go-opcua/pubsub/uadp"
	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/opcuactl/internal/model"
	"github.com/otfabric/opcuactl/internal/parse"
)

func parsedField(t *testing.T, spec string) parse.PubSubFieldValue {
	t.Helper()
	f, err := parse.PubSubFieldWithValue(spec)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// withDiscoveryAddress gives a UDP connection a DiscoveryAddress.
func withDiscoveryAddress(c *ua.PubSubConnectionDataType, url string) *ua.PubSubConnectionDataType {
	c.TransportSettings = ua.NewExtensionObject(&ua.DatagramConnectionTransport2DataType{
		DiscoveryAddress: ua.NewExtensionObject(&ua.NetworkAddressURLDataType{URL: url}),
	})
	return c
}

// diagnosticsOf returns the counters of the last JSON diagnostics line on
// stderr.
func diagnosticsOf(t *testing.T, stderr string) map[string]uint64 {
	t.Helper()
	var last string
	for _, line := range strings.Split(stderr, "\n") {
		if strings.HasPrefix(line, `{"diagnostics":`) {
			last = line
		}
	}
	if last == "" {
		t.Fatalf("no diagnostics on stderr:\n%s", stderr)
	}
	var v struct {
		Diagnostics model.PubSubDiagnostics `json:"diagnostics"`
	}
	if err := json.Unmarshal([]byte(last), &v); err != nil {
		t.Fatal(err)
	}
	out := map[string]uint64{}
	for _, c := range v.Diagnostics.Counters {
		out[c.Name] = c.Value
	}
	return out
}

// TestE2E_PubSubChunks sends and receives chunked UADP NetworkMessages.
func TestE2E_PubSubChunks(t *testing.T) {
	blob := strings.Repeat("opcua-", 500) // 3000 bytes

	t.Run("publish chunked", func(t *testing.T) {
		url := freeUDPURL(t)
		rcv := newReceiver()
		md := pubsub.MetaData("DS", pubsub.Field{Name: "Blob", Type: ua.TypeIDString})
		startRuntime(t, pubsub.NewConfiguration(nil, pubsub.AttachReaderGroups(
			pubsub.UDPConnection("sub", url, nil),
			pubsub.ReaderGroup("R", pubsub.DataSetReader("r", uint16(5), 3, 4, md)))),
			pubsub.WithDataSetHandler(rcv.handle))

		stdout, stderr, err := runCLI(t, "pubsub", "publish", "--url", url, "--publisher-id", "5", "--writer-group-id", "3",
			"--writer-id", "4", "--field", "Blob:String="+blob, "--max-message-size", "500", "--format", "json", "--diagnostics")
		if err != nil {
			t.Fatalf("publish: %v\n%s", err, stderr)
		}
		wantContains(t, stdout, `"messages":1`, `"chunks":`)
		if ds := rcv.wait(t); ds.Field("Blob") == nil || ds.Field("Blob").Value.Value() != blob {
			t.Errorf("the reassembled DataSet lost its field: %+v", ds)
		}
		if d := diagnosticsOf(t, stderr); d["ChunksSent"] < 7 || d["SentNetworkMessages"] != d["ChunksSent"] || d["FailedTransmissions"] != 0 {
			t.Errorf("unexpected diagnostics %v", d)
		}
		// JSON has no chunks.
		if _, _, err := runCLI(t, "pubsub", "publish", "--url", "mqtt://127.0.0.1:1", "--topic", "t", "--encoding", "json",
			"--field", "Blob:String="+blob, "--max-message-size", "-1"); err == nil {
			t.Error("a negative --max-message-size was accepted")
		}
	})

	t.Run("subscribe reassembles", func(t *testing.T) {
		url := freeUDPURL(t)
		values := pubsub.NewMemoryDataSource()
		node := ua.NewStringNodeID(1, "blob")
		_ = values.Set(node, blob)
		pds := pubsub.PublishedDataItems("Big", pubsub.Var("Blob", node, ua.TypeIDString))
		g := pubsub.WriterGroup("G", 10, 50*time.Millisecond, pubsub.DataSetWriter("W", 2, "Big"))
		g.MaxNetworkMessageSize = 400
		startRuntime(t, pubsub.NewConfiguration([]*ua.PublishedDataSetDataType{pds},
			pubsub.AttachWriterGroups(pubsub.UDPConnection("pub", url, uint16(77)), g)), pubsub.WithDataSource(values))

		stdout, stderr, err := runCLI(t, "pubsub", "subscribe", "--url", url, "--count", "1", "--duration", "15s",
			"--field", "Blob:String", "--chunk-timeout", "2s", "--chunk-max-pending", "16", "--diagnostics", "--format", "json")
		if err != nil {
			t.Fatalf("subscribe: %v\n%s", err, stderr)
		}
		got := parseDataSets(t, stdout)
		if len(got) != 1 || fieldValue(got[0], "Blob") != blob || got[0].PublisherID != "77" {
			t.Fatalf("unexpected DataSets:\n%s", stdout)
		}
		if d := diagnosticsOf(t, stderr); d["ChunkedMessagesReceived"] < 1 || d["ChunksReceived"] < 8 || d["ReceivedNetworkMessages"] < 1 {
			t.Errorf("unexpected diagnostics %v", d)
		}
		if _, _, err := runCLI(t, "pubsub", "subscribe", "--url", url, "--chunk-timeout", "-1s"); err == nil {
			t.Error("a negative --chunk-timeout was accepted")
		}
	})

	t.Run("decode chunks", func(t *testing.T) {
		nm := &message.NetworkMessage{
			Type:        message.NetworkMessageDataSet,
			UADPMask:    pubsub.DefaultUADPNetworkMessageContentMask,
			PublisherID: message.NewPublisherID(message.PublisherIDUInt16, 9),
			GroupHeader: message.GroupHeader{WriterGroupID: 1, SequenceNumber: 1, NetworkMessageNumber: 1},
			Messages: []*message.DataSetMessage{{
				DataSetWriterID: 1, Type: message.DataSetMessageKeyFrame, Valid: true, SequenceNumber: 1,
				UADPMask: pubsub.DefaultUADPDataSetMessageContentMask,
				Fields:   []*message.Field{{Value: &ua.DataValue{EncodingMask: ua.DataValueValue, Value: ua.MustVariant(blob)}}},
			}},
		}
		parts, err := uadp.EncodeChunks(nm, nil, 600)
		if err != nil || len(parts) < 2 {
			t.Fatalf("EncodeChunks: %d parts, %v", len(parts), err)
		}
		dir := t.TempDir()
		var files []string
		for i, p := range parts {
			f := filepath.Join(dir, "chunk"+string(rune('a'+i)))
			if err := os.WriteFile(f, []byte(hex.EncodeToString(p)), 0o600); err != nil {
				t.Fatal(err)
			}
			files = append(files, f)
		}
		// The chunks in any order.
		files[0], files[len(files)-1] = files[len(files)-1], files[0]
		out := mustRun(t, append(append([]string{"pubsub", "decode"}, files...), "--field", "Text:String")...)
		wantContains(t, out, "pub=9 wg=1 writer=1", "Text=opcua-opcua-")
		_, stderr, err := runCLI(t, "pubsub", "decode", files[0])
		if err != nil || !strings.Contains(stderr, "incomplete chunked message") {
			t.Errorf("one chunk: %v, %s", err, stderr)
		}
	})
}

// TestE2E_PubSubDiscovery exchanges UADP discovery probes and announcements
// with the go-opcua runtime.
func TestE2E_PubSubDiscovery(t *testing.T) {
	t.Run("discover", func(t *testing.T) {
		data, disc := freeUDPURL(t), freeUDPURL(t)
		startPublisher(t, withDiscoveryAddress(pubsub.UDPConnection("pub", data, uint16(2234)), disc),
			pubsub.WriterGroup("G", 100, 50*time.Millisecond, pubsub.DataSetWriter("W", 7, "Measurements")))

		stdout, stderr, err := runCLI(t, "pubsub", "discover", "--url", disc, "--listen-url", data, "--probe", "metadata,writers",
			"--publisher-id", "2234", "--writer-id", "7", "--count", "2", "--duration", "10s", "--diagnostics")
		if err != nil {
			t.Fatalf("discover: %v\n%s", err, stderr)
		}
		wantContains(t, stdout, `metadata pub=2234 writer=7 "Measurements"`, "Counter:UInt32",
			"announcement DataSetWriterConfiguration pub=2234", "writerGroup=G", "7:W(Measurements)")
		wantContains(t, stderr, "diagnostics discovery", "DiscoveryRequestsSent")

		// A probe for another publisher gets no answer.
		_, stderr, err = runCLI(t, "pubsub", "discover", "--url", disc, "--listen-url", data, "--probe", "metadata",
			"--publisher-id", "1", "--duration", "700ms")
		if err != nil || !strings.Contains(stderr, "no announcements") {
			t.Errorf("probe for another publisher: %v, %s", err, stderr)
		}
	})

	t.Run("subscribe probes for metadata", func(t *testing.T) {
		data, disc := freeUDPURL(t), freeUDPURL(t)
		w := pubsub.DataSetWriter("W", 7, "Measurements")
		w.DataSetFieldContentMask = ua.DataSetFieldContentMaskRawData
		startPublisher(t, withDiscoveryAddress(pubsub.UDPConnection("pub", data, uint16(2234)), disc),
			pubsub.WriterGroup("G", 100, 50*time.Millisecond, w))

		stdout, stderr, err := runCLI(t, "pubsub", "subscribe", "--url", data, "--discovery-url", disc, "--probe",
			"--show-metadata", "--count", "1", "--duration", "15s", "--diagnostics", "--format", "json")
		if err != nil {
			t.Fatalf("subscribe: %v\n%s", err, stderr)
		}
		got := parseDataSets(t, stdout)
		if len(got) != 1 || fieldValue(got[0], "Counter") != float64(42) || fieldValue(got[0], "Name") != "pump" {
			t.Fatalf("unexpected DataSets:\n%s", stdout)
		}
		wantContains(t, stdout, `"name":"Measurements"`)
		if d := diagnosticsOf(t, stderr); d["DiscoveryRequestsSent"] < 1 || d["DiscoveryResponsesReceived"] < 1 {
			t.Errorf("unexpected diagnostics %v", d)
		}
	})

	t.Run("publish answers probes", func(t *testing.T) {
		data, disc := freeUDPURL(t), freeUDPURL(t)
		rcv := newReceiver()
		startRuntime(t, pubsub.NewConfiguration(nil, pubsub.AttachReaderGroups(
			withDiscoveryAddress(pubsub.UDPConnection("sub", data, nil), disc),
			pubsub.ReaderGroup("R", pubsub.DataSetReader("r", uint16(9), 1, 3, nil)))),
			pubsub.WithDataSetHandler(rcv.handle))

		_, stderr, err := runCLI(t, "pubsub", "publish", "--url", data, "--discovery-url", disc, "--answer-probes",
			"--publisher-id", "9", "--writer-id", "3", "--field", "Counter:UInt32=11", "--field-encoding", "rawdata",
			"--count", "0", "--interval", "100ms", "--duration", "2s", "--diagnostics", "--diagnostics-level", "advanced")
		if err != nil {
			t.Fatalf("publish: %v\n%s", err, stderr)
		}
		ds := rcv.wait(t)
		if v := ds.Field("Counter"); v == nil || v.Value.Value() != uint32(11) {
			t.Errorf("the reader did not get the DataSetMetaData: %+v", ds)
		}
		wantContains(t, stderr, "diagnostics writer-group (level Advanced)", "EncryptionErrors", "DiscoveryRequestsReceived")
		if strings.Contains(stderr, "DiscoveryResponsesSent        0") {
			t.Errorf("no probe answered:\n%s", stderr)
		}
	})

	t.Run("announce interval", func(t *testing.T) {
		url := freeUDPURL(t)
		done := make(chan string, 1)
		go func() { done <- collectUDP(t, url, 6) }()
		time.Sleep(200 * time.Millisecond)
		mustRun(t, "pubsub", "publish", "--url", url, "--field", "A:Int16=1", "--announce-interval", "60ms",
			"--count", "4", "--interval", "100ms")
		announcements := 0
		for _, line := range strings.Split(<-done, "\n") {
			b, _ := hex.DecodeString(line)
			if typ, _, err := uadp.PeekType(b); err == nil && typ == message.NetworkMessageDiscoveryResponse {
				announcements++
			}
		}
		if announcements < 2 {
			t.Errorf("%d DataSetMetaData announcements, want at least 2", announcements)
		}
	})

	t.Run("errors", func(t *testing.T) {
		b := startBroker(t)
		for _, args := range [][]string{
			{"subscribe", "--url", b.url(), "--probe"},
			{"subscribe", "--url", "opc.udp://127.0.0.1:1", "--probe"},
			{"subscribe", "--url", "opc.udp://127.0.0.1:1", "--probe", "--encoding", "json"},
			{"subscribe", "--url", "opc.udp://224.0.0.22:1", "--probe", "--discovery-layout", "x"},
			{"subscribe", "--url", "opc.udp://127.0.0.1:1", "--diagnostics-level", "x"},
			{"subscribe", "--url", "opc.udp://127.0.0.1:1", "--diagnostics-interval", "-1s"},
			{"publish", "--url", "opc.udp://127.0.0.1:1", "--answer-probes", "--field", "A:Int32=1"},
			{"publish", "--url", b.url(), "--topic", "t", "--answer-probes", "--field", "A:Int32=1"},
			{"publish", "--url", b.url(), "--topic", "t", "--announce-interval", "1s", "--field", "A:Int32=1"},
			{"publish", "--url", "opc.udp://127.0.0.1:1", "--announce-interval", "-1s", "--field", "A:Int32=1"},
			{"discover"},
			{"discover", "--url", b.url()},
			{"discover", "--url", "opc.udp://127.0.0.1:1"},
			{"discover", "--url", "opc.udp://127.0.0.1:1", "--listen-url", b.url()},
			{"discover", "--url", "opc.udp://224.0.0.22:1", "--probe", "group"},
			{"discover", "--url", "opc.udp://224.0.0.22:1", "--probe", "bogus"},
			{"discover", "--url", "opc.udp://224.0.0.22:1", "--writer-id", "0"},
			{"discover", "--url", "opc.udp://224.0.0.22:1", "--publisher-id", "u8:300"},
			{"discover", "--url", "opc.udp://224.0.0.22:1", "--discovery-layout", "x"},
			{"decode", filepath.Join(t.TempDir(), "missing")},
		} {
			if _, _, err := runCLI(t, append([]string{"pubsub"}, args...)...); err == nil {
				t.Errorf("pubsub %v: expected an error", args)
			}
		}
	})
}

func TestDiscoveryProbes(t *testing.T) {
	pub := message.NewPublisherID(message.PublisherIDUInt16, 7)
	probes, err := discoveryProbes([]string{"applications", "endpoints", "metadata", "writers", "group", "connections"},
		&pub, []uint16{1, 2}, 5, true, true, []string{"p"}, message.DiscoveryLayoutSpec)
	if err != nil || len(probes) != 6 {
		t.Fatalf("discoveryProbes: %d, %v", len(probes), err)
	}
	if probes[0].Request.ProbeType != message.ProbeFindApplications || probes[2].Request.InformationType != message.InformationDataSetMetaData ||
		len(probes[2].Request.DataSetWriterIDs) != 2 || probes[4].Request.WriterGroupID != 5 || !probes[4].Request.IncludeDataSetWriters ||
		!probes[5].Request.IncludeWriterGroups || probes[5].Request.TransportProfileURIs[0] != "p" ||
		!probes[1].PublisherID.Equal(pub) {
		t.Errorf("unexpected probes %+v", probes)
	}
	for _, nm := range probes {
		if _, err := uadp.Encode(nm, nil); err != nil {
			t.Errorf("%s probe: %v", nm.Request.InformationType, err)
		}
	}
	p, _ := discoveryProbes([]string{"metadata"}, nil, nil, 0, false, false, nil, message.DiscoveryLayoutDotNet)
	if p[0].UADPMask != 0 || p[0].Request.Layout != message.DiscoveryLayoutDotNet {
		t.Error("a probe without --publisher-id names a publisher")
	}
	if _, err := uadp.Encode(p[0], nil); err != nil {
		t.Errorf(".NET probe: %v", err)
	}
}

func TestSharedAddress(t *testing.T) {
	for url, want := range map[string]bool{
		"opc.udp://224.0.0.22:4840": true, "opc.udp://[ff02::1]:4840": true, "opc.udp://127.0.0.1:4840": false,
		"opc.eth://01-00-5E-00-00-01": true, "mqtt://b:1883": false, "opc.udp://nohost": false, "x": false,
	} {
		if got := sharedAddress(url); got != want {
			t.Errorf("sharedAddress(%s) = %v", url, got)
		}
	}
}

func TestPublisherAnswers(t *testing.T) {
	pub := message.NewPublisherID(message.PublisherIDUInt16, 7)
	p := &pubsubPublisher{publisher: pub, groupID: 3, writerID: 4, groupName: "G", writerName: "W", dataSetName: "DS",
		md: pubsub.MetaData("DS", pubsub.Field{Name: "A", Type: ua.TypeIDInt32}), mode: ua.MessageSecurityModeNone,
		profile: "prof", url: "opc.udp://224.0.0.22:4840", limiter: discoveryLimiter{}}
	now := time.Now()
	ask := func(hasPub bool, who message.PublisherID, req message.DiscoveryRequest) []discoveryAnswer {
		return p.answers(probeMessage(who, hasPub, req), now)
	}
	md := message.DiscoveryRequest{ProbeType: message.ProbePublisherInformation, InformationType: message.InformationDataSetMetaData}
	if a := ask(true, pub, md); len(a) != 1 || !a[0].data || a[0].d.Type != message.DiscoveryDataSetMetaData {
		t.Errorf("metadata: %+v", a)
	}
	if a := ask(true, pub, md); len(a) != 0 {
		t.Error("the same announcement was sent twice within the minimum interval")
	}
	if a := ask(true, message.NewPublisherID(message.PublisherIDUInt16, 8), message.DiscoveryRequest{ProbeType: message.ProbeFindApplications}); len(a) != 0 {
		t.Error("answered a probe for another publisher")
	}
	other := md
	other.DataSetWriterIDs = []uint16{5}
	other.Layout = message.DiscoveryLayoutDotNet
	if a := ask(false, pub, other); len(a) != 0 {
		t.Error("answered a probe for another writer")
	}
	for _, req := range []message.DiscoveryRequest{
		{ProbeType: message.ProbeFindApplications},
		{ProbeType: message.ProbePublisherInformation, InformationType: message.InformationPublisherEndpoints},
		{ProbeType: message.ProbePublisherInformation, InformationType: message.InformationDataSetWriterConfig, DataSetWriterIDs: []uint16{4}},
		{ProbeType: message.ProbePublisherInformation, InformationType: message.InformationWriterGroupConfig, WriterGroupID: 3, IncludeDataSetWriters: true},
		{ProbeType: message.ProbePublisherInformation, InformationType: message.InformationPubSubConnections, IncludeWriterGroups: true, TransportProfileURIs: []string{"prof"}},
	} {
		a := ask(false, pub, req)
		if len(a) != 1 {
			t.Errorf("%+v: %d answers", req, len(a))
			continue
		}
		parts, err := p.announcement(a[0].d, req.Layout, a[0].data)
		if err != nil || len(parts) != 1 {
			t.Errorf("%+v: %v", req, err)
			continue
		}
		nm, err := uadp.Decode(parts[0], nil)
		if err != nil || nm.Discovery == nil {
			t.Errorf("%+v: decode: %v", req, err)
			continue
		}
		m := announcementModel(nil, nm)
		if line := announcementLine(m); !strings.HasPrefix(line, "announcement "+nm.Discovery.Type.String()+" pub=7") {
			t.Errorf("announcementLine = %q", line)
		}
	}
	if a := ask(false, pub, message.DiscoveryRequest{ProbeType: message.ProbePublisherInformation, InformationType: message.InformationPubSubConnections,
		TransportProfileURIs: []string{"other"}}); len(a) != 0 {
		t.Error("answered a connections probe for another profile")
	}
	if a := ask(false, pub, message.DiscoveryRequest{ProbeType: 9}); len(a) != 0 {
		t.Error("answered an unknown probe")
	}
}

func TestPublisherChunks(t *testing.T) {
	p := &pubsubPublisher{enc: "uadp", publisher: message.NewPublisherID(message.PublisherIDUInt16, 1), groupID: 1, writerID: 1,
		md: pubsub.MetaData("DS", pubsub.Field{Name: "B", Type: ua.TypeIDByteString}), mode: ua.MessageSecurityModeNone, maxSize: 300}
	p.md.ConfigurationVersion = &ua.ConfigurationVersionDataType{MajorVersion: 1}
	p.fields = append(p.fields, parsedField(t, "B:ByteString="+base64.StdEncoding.EncodeToString(make([]byte, 1000))))
	parts, err := p.dataMessage(1, time.Now())
	if err != nil || len(parts) < 4 {
		t.Fatalf("dataMessage: %d parts, %v", len(parts), err)
	}
	if p.groupSeq != uint16(len(parts)) {
		t.Errorf("the GroupHeader sequence number did not advance by the chunks: %d", p.groupSeq)
	}
	for _, b := range parts {
		if len(b) > 300 {
			t.Errorf("a chunk of %d bytes", len(b))
		}
	}
	p.enc, p.maxSize = "json", 100
	if _, err := p.dataMessage(2, time.Now()); err == nil || !strings.Contains(err.Error(), "JSON has no chunks") {
		t.Errorf("an oversized JSON message: %v", err)
	}
}
