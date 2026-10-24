// SPDX-License-Identifier: MIT

package opcuactl

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-opcua/pubsub"
	"github.com/otfabric/go-opcua/pubsub/jsonmsg"
	"github.com/otfabric/go-opcua/pubsub/message"
	"github.com/otfabric/go-opcua/pubsub/security"
	"github.com/otfabric/go-opcua/pubsub/transport"
	"github.com/otfabric/go-opcua/pubsub/uadp"
	"github.com/otfabric/go-opcua/server"
	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/opcuactl/internal/model"
)

// freeUDPURL returns an opc.udp:// loopback address nobody listens on.
func freeUDPURL(t *testing.T) string {
	t.Helper()
	c, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen udp: %v", err)
	}
	defer func() { _ = c.Close() }()
	return fmt.Sprintf("opc.udp://%s", c.LocalAddr().String())
}

// startRuntime runs a go-opcua PubSub runtime for the test.
func startRuntime(t *testing.T, cfg *ua.PubSubConfigurationDataType, opts ...pubsub.Option) {
	t.Helper()
	ps, err := pubsub.New(cfg, opts...)
	if err != nil {
		t.Fatalf("pubsub.New: %v", err)
	}
	if err := ps.Start(context.Background()); err != nil {
		t.Fatalf("pubsub start: %v", err)
	}
	t.Cleanup(func() { _ = ps.Close() })
}

// startPublisher publishes Counter (UInt32 42) and Name (String "pump")
// every 50 ms as PublisherId 2234, WriterGroupId 100, DataSetWriterId 7.
func startPublisher(t *testing.T, conn *ua.PubSubConnectionDataType, group *ua.WriterGroupDataType, opts ...pubsub.Option) {
	t.Helper()
	values := pubsub.NewMemoryDataSource()
	counter, name := ua.NewStringNodeID(1, "counter"), ua.NewStringNodeID(1, "name")
	pds := pubsub.PublishedDataItems("Measurements",
		pubsub.Var("Counter", counter, ua.TypeIDUint32),
		pubsub.Var("Name", name, ua.TypeIDString))
	_ = values.Set(counter, uint32(42))
	_ = values.Set(name, "pump")
	cfg := pubsub.NewConfiguration([]*ua.PublishedDataSetDataType{pds}, pubsub.AttachWriterGroups(conn, group))
	startRuntime(t, cfg, append([]pubsub.Option{pubsub.WithDataSource(values)}, opts...)...)
}

// receiver collects the DataSets a runtime reader receives.
type receiver struct {
	ch chan *pubsub.ReceivedDataSet
}

func newReceiver() *receiver { return &receiver{ch: make(chan *pubsub.ReceivedDataSet, 64)} }

func (r *receiver) handle(ds *pubsub.ReceivedDataSet) {
	select {
	case r.ch <- ds:
	default:
	}
}

func (r *receiver) wait(t *testing.T) *pubsub.ReceivedDataSet {
	t.Helper()
	select {
	case ds := <-r.ch:
		return ds
	case <-time.After(10 * time.Second):
		t.Fatal("no DataSet received")
		return nil
	}
}

// drain discards what arrives until the receiver has been quiet for a while.
func (r *receiver) drain() {
	for {
		select {
		case <-r.ch:
		case <-time.After(300 * time.Millisecond):
			return
		}
	}
}

func parseDataSets(t *testing.T, out string) []model.PubSubDataSet {
	t.Helper()
	var all []model.PubSubDataSet
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" || !strings.Contains(line, `"messageType"`) {
			continue
		}
		var ds model.PubSubDataSet
		if err := json.Unmarshal([]byte(line), &ds); err != nil {
			t.Fatalf("parsing %q: %v", line, err)
		}
		all = append(all, ds)
	}
	return all
}

func fieldValue(ds model.PubSubDataSet, name string) interface{} {
	for _, f := range ds.Fields {
		if f.Name == name {
			return f.Value
		}
	}
	return nil
}

// TestE2E_PubSubUDP round-trips UADP over UDP between the CLI and the
// go-opcua PubSub runtime in both directions.
func TestE2E_PubSubUDP(t *testing.T) {
	t.Run("subscribe", func(t *testing.T) {
		url := freeUDPURL(t)
		startPublisher(t, pubsub.UDPConnection("pub", url, uint16(2234)),
			pubsub.WriterGroup("G", 100, 50*time.Millisecond, pubsub.DataSetWriter("W", 7, "Measurements")))

		out := mustRun(t, "pubsub", "subscribe", "--url", url, "--count", "2", "--duration", "15s", "--format", "json")
		got := parseDataSets(t, out)
		if len(got) != 2 {
			t.Fatalf("got %d DataSets, want 2:\n%s", len(got), out)
		}
		ds := got[0]
		if ds.PublisherID != "2234" || ds.WriterGroupID != 100 || ds.DataSetWriterID != 7 ||
			ds.Encoding != "uadp" || ds.MessageType != "keyframe" || ds.Security != "None" {
			t.Errorf("unexpected header: %+v", ds)
		}
		if len(ds.Fields) != 2 || ds.Fields[0].Value != float64(42) || ds.Fields[1].Value != "pump" || ds.Fields[0].Type != "UInt32" {
			t.Errorf("unexpected fields: %+v", ds.Fields)
		}

		// Field names from the command line, filters that match.
		out = mustRun(t, "pubsub", "subscribe", "--url", url, "--count", "1", "--duration", "15s",
			"--field", "Counter:UInt32", "--field", "Name:String",
			"--publisher-id", "2234", "--writer-group-id", "100", "--writer-id", "7")
		wantContains(t, out, "pub=2234 wg=100 writer=7", "keyframe", "Counter=42", "Name=pump")

		// Filters that do not match: nothing is printed.
		for _, filter := range [][]string{{"--writer-id", "8"}, {"--publisher-id", "1"}, {"--writer-group-id", "1"}} {
			args := append([]string{"pubsub", "subscribe", "--url", url, "--duration", "400ms"}, filter...)
			if out := mustRun(t, args...); strings.TrimSpace(out) != "" {
				t.Errorf("%v: unexpected output:\n%s", filter, out)
			}
		}
	})

	t.Run("publish", func(t *testing.T) {
		url := freeUDPURL(t)
		rcv := newReceiver()
		md := pubsub.MetaData("DS", pubsub.Field{Name: "Counter", Type: ua.TypeIDUint32}, pubsub.Field{Name: "T", Type: ua.TypeIDDouble})
		startRuntime(t, pubsub.NewConfiguration(nil, pubsub.AttachReaderGroups(
			pubsub.UDPConnection("sub", url, nil),
			// One reader per run: every run starts its sequence numbers at 1.
			pubsub.ReaderGroup("R",
				pubsub.DataSetReader("r4", uint16(5), 3, 4, md),
				pubsub.DataSetReader("r5", uint16(5), 3, 5, md),
				pubsub.DataSetReader("r6", uint16(5), 3, 6, md)))),
			pubsub.WithDataSetHandler(rcv.handle))

		for i, enc := range []string{"variant", "datavalue", "rawdata"} {
			out := mustRun(t, "pubsub", "publish", "--url", url, "--publisher-id", "5", "--writer-group-id", "3",
				"--writer-id", fmt.Sprint(4+i), "--field", "Counter:UInt32=7", "--field", "T:Double=2.5",
				"--field-encoding", enc, "--count", "2", "--interval", "20ms")
			wantContains(t, out, "published 2 message(s)", url)
			ds := rcv.wait(t)
			if v := ds.Field("Counter"); v == nil || v.Value.Value() != uint32(7) {
				t.Errorf("%s: Counter = %v", enc, v)
			}
			if v := ds.Field("T"); v == nil || v.Value.Value() != 2.5 {
				t.Errorf("%s: T = %v", enc, v)
			}
			rcv.drain()
		}
	})

	t.Run("cli to cli with metadata announcement", func(t *testing.T) {
		url := freeUDPURL(t)
		done := make(chan string, 1)
		go func() {
			// A UADP listener that collects what the CLI sends.
			done <- collectUDP(t, url, 2)
		}()
		time.Sleep(200 * time.Millisecond)
		out := mustRun(t, "pubsub", "publish", "--url", url, "--field", "A:Int16=-3", "--field", "B:Boolean=true",
			"--announce-metadata", "--field-encoding", "rawdata", "--format", "json")
		wantContains(t, out, `"messages":1`)
		hexMsgs := <-done
		file := filepath.Join(t.TempDir(), "capture.txt")
		parts := strings.Split(hexMsgs, "\n")
		if len(parts) != 2 {
			t.Fatalf("captured %d messages, want 2", len(parts))
		}
		// The announcement decodes on its own; the RawData message needs it.
		if err := os.WriteFile(file, []byte(parts[0]), 0o600); err != nil {
			t.Fatal(err)
		}
		out = mustRun(t, "pubsub", "decode", file)
		wantContains(t, out, "metadata pub=1 writer=1", "A:Int16", "B:Boolean")
		if err := os.WriteFile(file, []byte(parts[1]), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := runCLI(t, "pubsub", "decode", file); err == nil {
			t.Error("a RawData message decoded without its DataSetMetaData")
		}
		out = mustRun(t, "pubsub", "decode", file, "--field", "A:Int16", "--field", "B:Boolean")
		wantContains(t, out, "A=-3", "B=true")
	})
}

// collectUDP receives n datagrams on url and returns them as hex lines.
func collectUDP(t *testing.T, url string, n int) string {
	addr := strings.TrimPrefix(url, "opc.udp://")
	c, err := net.ListenPacket("udp4", addr)
	if err != nil {
		t.Errorf("listen %s: %v", addr, err)
		return ""
	}
	defer func() { _ = c.Close() }()
	_ = c.SetDeadline(time.Now().Add(10 * time.Second))
	var lines []string
	buf := make([]byte, 65536)
	for len(lines) < n {
		k, _, err := c.ReadFrom(buf)
		if err != nil {
			t.Errorf("read: %v", err)
			break
		}
		lines = append(lines, hex.EncodeToString(buf[:k]))
	}
	return strings.Join(lines, "\n")
}

// TestE2E_PubSubDecode decodes messages encoded by the go-opcua codecs.
func TestE2E_PubSubDecode(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, b []byte) string {
		f := filepath.Join(dir, name)
		if err := os.WriteFile(f, b, 0o600); err != nil {
			t.Fatal(err)
		}
		return f
	}
	ts := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	pub := message.NewPublisherID(message.PublisherIDUInt16, 77)
	nm := &message.NetworkMessage{
		UADPMask:        pubsub.DefaultUADPNetworkMessageContentMask,
		JSONMask:        pubsub.DefaultJSONNetworkMessageContentMask,
		PublisherID:     pub,
		GroupHeader:     message.GroupHeader{WriterGroupID: 5, SequenceNumber: 1},
		WriterGroupName: "G",
		Messages: []*message.DataSetMessage{{
			DataSetWriterID: 9, Valid: true, SequenceNumber: 11, Timestamp: ts,
			UADPMask: pubsub.DefaultUADPDataSetMessageContentMask,
			JSONMask: pubsub.DefaultJSONDataSetMessageContentMask,
			Fields: []*message.Field{
				{Index: 0, Name: "Level", Value: &ua.DataValue{EncodingMask: ua.DataValueValue, Value: ua.MustVariant(float64(3.5))}},
				{Index: 1, Name: "Tag", Value: &ua.DataValue{EncodingMask: ua.DataValueValue, Value: ua.MustVariant("x")}},
			},
		}},
	}
	md := pubsub.MetaData("Tank", pubsub.Field{Name: "Level", Type: ua.TypeIDDouble}, pubsub.Field{Name: "Tag", Type: ua.TypeIDString})

	uadpBytes, err := uadp.Encode(nm, nil)
	if err != nil {
		t.Fatal(err)
	}
	jsonBytes, err := jsonmsg.Encode(nm, nil)
	if err != nil {
		t.Fatal(err)
	}
	metaBytes, err := jsonmsg.EncodeMetaData(&jsonmsg.MetaDataMessage{PublisherID: pub, DataSetWriterID: 9, MetaData: md}, nil)
	if err != nil {
		t.Fatal(err)
	}
	statusBytes, err := jsonmsg.EncodeStatus(&jsonmsg.StatusMessage{PublisherID: pub, Status: ua.PubSubStateOperational, IsCyclic: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	mdJSON, err := ua.EncodeJSON(md)
	if err != nil {
		t.Fatal(err)
	}

	rawFile := write("msg.bin", uadpBytes)
	hexFile := write("msg.hex", []byte(hex.EncodeToString(uadpBytes)+"\n"))
	b64File := write("msg.b64", []byte(base64.StdEncoding.EncodeToString(uadpBytes)))
	jsonFile := write("msg.json", jsonBytes)
	metaFile := write("meta.json", metaBytes)
	mdFile := write("md.json", mdJSON)
	statusFile := write("status.json", statusBytes)

	t.Run("uadp raw, hex and base64", func(t *testing.T) {
		for _, args := range [][]string{{rawFile}, {hexFile}, {hexFile, "--input", "hex"}, {b64File, "--input", "base64"}, {rawFile, "--input", "raw", "--encoding", "uadp"}} {
			out := mustRun(t, append([]string{"pubsub", "decode"}, args...)...)
			wantContains(t, out, "pub=77 wg=5 writer=9 seq=11 keyframe", "#0=3.5", "#1=x")
		}
	})
	t.Run("uadp with metadata", func(t *testing.T) {
		for _, args := range [][]string{{"--metadata", metaFile}, {"--metadata", mdFile}, {"--field", "Level:Double", "--field", "Tag:String"}} {
			out := mustRun(t, append([]string{"pubsub", "decode", rawFile}, args...)...)
			wantContains(t, out, "Level=3.5", "Tag=x")
		}
	})
	t.Run("json", func(t *testing.T) {
		out := mustRun(t, "pubsub", "decode", jsonFile, "--format", "json")
		var got []model.PubSubDataSet
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatalf("%v:\n%s", err, out)
		}
		if len(got) != 1 || got[0].Encoding != "json" || got[0].PublisherID != "77" || got[0].DataSetWriterID != 9 ||
			fieldValue(got[0], "Level") != 3.5 || fieldValue(got[0], "Tag") != "x" || got[0].WriterGroupName != "G" {
			t.Errorf("unexpected result: %+v", got)
		}
	})
	t.Run("metadata and status messages", func(t *testing.T) {
		out := mustRun(t, "pubsub", "decode", metaFile)
		wantContains(t, out, "metadata pub=77 writer=9", `"Tank"`, "Level:Double", "Tag:String")
		out = mustRun(t, "pubsub", "decode", metaFile, "--format", "json")
		wantContains(t, out, `"name":"Tank"`)
		out = mustRun(t, "pubsub", "decode", statusFile)
		wantContains(t, out, "status", "state=Operational", "publisherId=77")
		out = mustRun(t, "pubsub", "decode", statusFile, "--format", "yaml")
		wantContains(t, out, "state: Operational")
	})
	t.Run("filters", func(t *testing.T) {
		out, errOut, err := runCLI(t, "pubsub", "decode", rawFile, "--writer-id", "1")
		if err != nil || strings.TrimSpace(out) != "" || !strings.Contains(errOut, "does not match") {
			t.Errorf("err=%v out=%q stderr=%q", err, out, errOut)
		}
		out = mustRun(t, "pubsub", "decode", jsonFile, "--publisher-id", "77", "--writer-id", "9")
		wantContains(t, out, "Level=3.5")
	})
	t.Run("errors", func(t *testing.T) {
		garbage := write("garbage.bin", []byte{0x01, 0xff})
		empty := write("empty.bin", nil)
		for _, args := range [][]string{
			{garbage},
			{empty},
			{rawFile, "--input", "bogus"},
			{rawFile, "--encoding", "xml"},
			{hexFile, "--input", "base64"},
			{rawFile, "--message-security", "sign"},
			{rawFile, "--message-security", "bogus"},
			{rawFile, "--field", "Level"},
			{rawFile, "--field", "Level:Double", "--metadata", metaFile},
			{rawFile, "--metadata", jsonFile},
			{rawFile, "--keys-file", filepath.Join(dir, "missing.json")},
			{filepath.Join(dir, "missing.bin")},
		} {
			if _, _, err := runCLI(t, append([]string{"pubsub", "decode"}, args...)...); err == nil {
				t.Errorf("decode %v: expected an error", args)
			}
		}
	})
}

// TestE2E_PubSubMQTT round-trips UADP and JSON through an MQTT broker.
func TestE2E_PubSubMQTT(t *testing.T) {
	b := startBroker(t)

	t.Run("subscribe json with announced metadata", func(t *testing.T) {
		w := pubsub.SetWriterQueue(pubsub.DataSetWriter("W", 3, "Measurements"), "plant/line1", "plant/line1/meta")
		startPublisher(t, pubsub.MQTTConnection("broker", b.url(), "line1", pubsub.MappingJSON),
			pubsub.JSONWriterGroup("G", 1, 50*time.Millisecond, "plant/line1", w))

		out := mustRun(t, "pubsub", "subscribe", "--url", b.url(), "--topic", "plant/#", "--show-metadata",
			"--count", "2", "--duration", "15s", "--jsonl")
		wantContains(t, out, `"name":"Measurements"`)
		got := parseDataSets(t, out)
		if len(got) != 2 {
			t.Fatalf("got %d DataSets:\n%s", len(got), out)
		}
		if got[0].Encoding != "json" || got[0].PublisherID != "line1" || got[0].Topic != "plant/line1" ||
			fieldValue(got[0], "Counter") != float64(42) || fieldValue(got[0], "Name") != "pump" {
			t.Errorf("unexpected DataSet: %+v", got[0])
		}
	})

	collect := func(t *testing.T, topic string, n int) <-chan *transport.Message {
		t.Helper()
		ctx := context.Background()
		c, err := transport.Open(ctx, transport.Config{URL: b.url()})
		if err != nil {
			t.Fatalf("mqtt open: %v", err)
		}
		t.Cleanup(func() { _ = c.Close() })
		ch := make(chan *transport.Message, n+8)
		if _, err := c.Subscribe(ctx, topic, 0, func(m *transport.Message) { ch <- m }); err != nil {
			t.Fatalf("mqtt subscribe: %v", err)
		}
		return ch
	}
	next := func(t *testing.T, ch <-chan *transport.Message) *transport.Message {
		t.Helper()
		select {
		case m := <-ch:
			return m
		case <-time.After(10 * time.Second):
			t.Fatal("nothing received from the broker")
			return nil
		}
	}

	t.Run("publish json with metadata topic", func(t *testing.T) {
		ch := collect(t, "cli/json/#", 4)
		out := mustRun(t, "pubsub", "publish", "--url", b.url(), "--encoding", "json", "--topic", "cli/json/data",
			"--metadata-topic", "cli/json/meta", "--publisher-id", "str:cli", "--field", "Temp:Double=21.5",
			"--qos", "1", "--count", "1")
		wantContains(t, out, "published 1 message(s)", "topic cli/json/data")

		meta := next(t, ch)
		mm, err := jsonmsg.DecodeMetaData(meta.Payload, nil)
		if err != nil || meta.Topic != "cli/json/meta" || mm.MetaData.Fields[0].Name != "Temp" || mm.PublisherID.String() != "cli" {
			t.Fatalf("metadata: topic %s, %+v, %v", meta.Topic, mm, err)
		}
		data := next(t, ch)
		nm, err := jsonmsg.Decode(data.Payload, nil)
		if err != nil {
			t.Fatal(err)
		}
		if f := nm.Messages[0].Fields[0]; f.Name != "Temp" || f.Value.Value.Value() != 21.5 {
			t.Errorf("unexpected field %+v", f)
		}
	})

	t.Run("publish uadp over mqtt", func(t *testing.T) {
		ch := collect(t, "cli/uadp", 2)
		mustRun(t, "pubsub", "publish", "--url", b.url(), "--topic", "cli/uadp", "--publisher-id", "u32:70000",
			"--field", "N:Int64=-9", "--count", "1")
		m := next(t, ch)
		nm, err := uadp.Decode(m.Payload, nil)
		if err != nil {
			t.Fatal(err)
		}
		if nm.PublisherID.Type != message.PublisherIDUInt32 || nm.PublisherID.Num != 70000 ||
			nm.Messages[0].Fields[0].Value.Value.Value() != int64(-9) {
			t.Errorf("unexpected message %+v", nm)
		}
	})

	t.Run("errors", func(t *testing.T) {
		for _, args := range [][]string{
			{"publish", "--url", b.url(), "--field", "A:Int32=1"},                                       // no topic
			{"publish", "--url", "opc.udp://127.0.0.1:1", "--field", "A:Int32=1", "--encoding", "json"}, // JSON over UDP
			{"publish", "--url", "opc.udp://127.0.0.1:1", "--field", "A:Int32=1", "--metadata-topic", "x"},
			{"publish", "--url", b.url(), "--topic", "t", "--field", "A:Int32=1", "--encoding", "json", "--message-security", "sign", "--keys-file", "k"},
			{"publish", "--url", b.url(), "--topic", "t", "--field", "A:Int32=1", "--qos", "2"},
			{"publish", "--url", "http://x", "--field", "A:Int32=1"},
			{"publish", "--field", "A:Int32=1"},
			{"publish", "--url", b.url(), "--topic", "t"},
			{"publish", "--url", b.url(), "--topic", "t", "--field", "A:Int32=x"},
			{"publish", "--url", b.url(), "--topic", "t", "--field", "A:Int32=1", "--field-encoding", "bogus"},
			{"publish", "--url", b.url(), "--topic", "t", "--field", "A:Int32=1", "--publisher-id", "u16:99999"},
			{"publish", "--url", b.url(), "--topic", "t", "--field", "A:Int32=1", "--encoding", "auto"},
			{"publish", "--url", b.url(), "--topic", "t", "--field", "A:Int32=1", "--message-security", "sign"},
			{"publish", "--url", b.url(), "--topic", "t", "--field", "A:Int32=1", "--security-group", "sg"},
			{"subscribe"},
			{"subscribe", "--url", "opc.udp://127.0.0.1:1", "--encoding", "bogus"},
			{"subscribe", "--url", "opc.udp://127.0.0.1:1", "--message-security", "sign"},
			{"subscribe", "--url", "opc.udp://127.0.0.1:1", "--security-group", "sg"},
			{"subscribe", "--url", "opc.udp://127.0.0.1:1", "--publisher-id", "u8:300"},
		} {
			if _, _, err := runCLI(t, append([]string{"pubsub"}, args...)...); err == nil {
				t.Errorf("pubsub %v: expected an error", args)
			}
		}
	})
}

// TestE2E_PubSubSKS gets keys from a go-opcua Security Key Service and uses
// them for secured UADP messages in both directions.
func TestE2E_PubSubSKS(t *testing.T) {
	srvID := newIdentity(t, "rsa", serverURI)
	cliID := newIdentity(t, "rsa", clientURI)

	ks := security.NewKeyService()
	if err := ks.AddSecurityGroup(security.SecurityGroupConfig{
		SecurityGroupID:   "sg",
		SecurityPolicyURI: security.PolicyURIAes256CTR,
		KeyLifetime:       time.Hour,
		MaxFutureKeyCount: 3,
		MaxPastKeyCount:   1,
	}); err != nil {
		t.Fatal(err)
	}
	s := startServerWith(t, func(srv *server.Server) {
		if _, err := srv.EnableSecurityKeyService(ks); err != nil {
			t.Fatalf("EnableSecurityKeyService: %v", err)
		}
	},
		server.ApplicationIdentity(srvID.der, srvID.key),
		server.EnableSecurity("Basic256Sha256", ua.MessageSecurityModeSignAndEncrypt),
		server.EnableAuthMode(ua.UserTokenTypeUserName),
		server.WithUsernameValidator(func(user, pass string) error {
			if user == "pub" && pass == "pw" {
				return nil
			}
			return errors.New("denied")
		}),
	)
	session := []string{"-e", s.url, "--security-policy", "Basic256Sha256", "--security-mode", "SignAndEncrypt",
		"--cert", cliID.certFile, "--key", cliID.keyFile, "--trusted-cert", srvID.certFile}
	user := append(append([]string{}, session...), "--auth", "username", "--username", "pub", "--password", "pw")
	keySrc, err := ks.KeySource("sg")
	if err != nil {
		t.Fatal(err)
	}

	keysFile := filepath.Join(t.TempDir(), "keys.json")
	t.Run("get-keys", func(t *testing.T) {
		out := mustRun(t, append([]string{"pubsub", "sks", "get-keys", "--security-group", "sg"}, user...)...)
		wantContains(t, out, "SecurityGroup:  sg", security.PolicyURIAes256CTR, "KeyLifetime:    1h0m0s", "TOKEN", "LENGTH")
		if strings.Contains(out, "KEY ") {
			t.Errorf("key material printed without --show-keys:\n%s", out)
		}

		out = mustRun(t, append([]string{"pubsub", "sks", "get-keys", "--security-group", "sg", "--future-keys", "2",
			"--show-keys", "--format", "json"}, user...)...)
		var sk model.SecurityKeys
		if err := json.Unmarshal([]byte(out), &sk); err != nil {
			t.Fatalf("%v:\n%s", err, out)
		}
		cur, _ := ks.CurrentTokenID("sg")
		if sk.FirstTokenID != cur || len(sk.Keys) != 3 || sk.Keys[0].Length != security.Aes256CTR().KeyLength() || sk.Keys[0].Key == "" {
			t.Errorf("unexpected keys: %+v", sk)
		}
		if err := os.WriteFile(keysFile, []byte(out), 0o600); err != nil {
			t.Fatal(err)
		}

		out = mustRun(t, append([]string{"pubsub", "sks", "get-keys", "--security-group", "sg", "--show-keys"}, user...)...)
		wantContains(t, out, "KEY")
	})

	t.Run("get-keys refused", func(t *testing.T) {
		for _, args := range [][]string{
			append([]string{"--security-group", "sg"}, session...),                       // anonymous
			append([]string{"--security-group", "nosuch"}, user...),                      // unknown group
			{"--security-group", "sg"},                                                   // no endpoint
			append([]string{}, user...),                                                  // no group
			{"--security-group", "sg", "-e", "opc.tcp://127.0.0.1:1", "--timeout", "1s"}, // unreachable
		} {
			if _, _, err := runCLI(t, append([]string{"pubsub", "sks", "get-keys"}, args...)...); err == nil {
				t.Errorf("get-keys %v: expected an error", args)
			}
		}
	})

	t.Run("subscribe with keys from the SKS", func(t *testing.T) {
		url := freeUDPURL(t)
		startPublisher(t, pubsub.UDPConnection("pub", url, uint16(2234)),
			pubsub.Secure(pubsub.WriterGroup("G", 100, 50*time.Millisecond, pubsub.DataSetWriter("W", 7, "Measurements")),
				ua.MessageSecurityModeSignAndEncrypt, "sg"),
			pubsub.WithKeySource("sg", keySrc))

		args := append([]string{"pubsub", "subscribe", "--url", url, "--security-group", "sg",
			"--message-security", "sign-and-encrypt", "--count", "2", "--duration", "20s", "--format", "json",
			"--field", "Counter:UInt32", "--field", "Name:String"}, user...)
		got := parseDataSets(t, mustRun(t, args...))
		if len(got) != 2 || got[0].Security != "SignAndEncrypt" || got[0].SecurityTokenID == 0 ||
			fieldValue(got[0], "Counter") != float64(42) || fieldValue(got[0], "Name") != "pump" {
			t.Errorf("unexpected DataSets: %+v", got)
		}

		// The same with the saved keys, and without keys at all.
		out := mustRun(t, "pubsub", "subscribe", "--url", url, "--keys-file", keysFile, "--count", "1", "--duration", "20s")
		wantContains(t, out, "[SignAndEncrypt token=", "#0=42")
		out, errOut, err := runCLI(t, "pubsub", "subscribe", "--url", url, "--duration", "500ms")
		if err != nil || strings.TrimSpace(out) != "" || !strings.Contains(errOut, "security") {
			t.Errorf("without keys: err=%v out=%q stderr=%q", err, out, errOut)
		}
	})

	t.Run("publish with saved keys", func(t *testing.T) {
		url := freeUDPURL(t)
		rcv := newReceiver()
		md := pubsub.MetaData("DS", pubsub.Field{Name: "Counter", Type: ua.TypeIDUint32})
		startRuntime(t, pubsub.NewConfiguration(nil, pubsub.AttachReaderGroups(
			pubsub.UDPConnection("sub", url, nil),
			pubsub.Secure(pubsub.ReaderGroup("R",
				pubsub.DataSetReader("r1", uint16(1), 1, 1, md),
				pubsub.DataSetReader("r2", uint16(1), 1, 2, md),
				pubsub.DataSetReader("r3", uint16(1), 1, 3, md)),
				ua.MessageSecurityModeSignAndEncrypt, "sg"))),
			pubsub.WithDataSetHandler(rcv.handle), pubsub.WithKeySource("sg", keySrc))

		for i, mode := range []string{"sign-and-encrypt", "sign"} {
			mustRun(t, "pubsub", "publish", "--url", url, "--field", "Counter:UInt32=5", "--count", "3", "--interval", "20ms",
				"--writer-id", fmt.Sprint(1+2*i), "--message-security", mode, "--keys-file", keysFile, "--security-group", "sg")
			if mode == "sign" {
				// The reader requires SignAndEncrypt: signed messages are rejected.
				select {
				case ds := <-rcv.ch:
					t.Errorf("a signed message passed a SignAndEncrypt reader: %+v", ds)
				case <-time.After(300 * time.Millisecond):
				}
				continue
			}
			if v := rcv.wait(t).Field("Counter"); v == nil || v.Value.Value() != uint32(5) {
				t.Errorf("Counter = %v", v)
			}
			rcv.drain()
		}

		// Keys pulled from the SKS for publishing.
		mustRun(t, append([]string{"pubsub", "publish", "--url", url, "--field", "Counter:UInt32=6", "--count", "2", "--writer-id", "2",
			"--interval", "20ms", "--message-security", "sign-and-encrypt", "--security-group", "sg"}, user...)...)
		if v := rcv.wait(t).Field("Counter"); v == nil || v.Value.Value() != uint32(6) {
			t.Errorf("Counter = %v", v)
		}
	})

	t.Run("bad keys files", func(t *testing.T) {
		dir := t.TempDir()
		for name, content := range map[string]string{
			"notjson":   "{",
			"policy":    `{"securityPolicyUri":"urn:nope","keys":[{"tokenId":1,"key":"AA=="}]}`,
			"nokeys":    `{"securityPolicyUri":"` + security.PolicyURIAes128CTR + `","keys":[]}`,
			"nomat":     `{"securityPolicyUri":"` + security.PolicyURIAes128CTR + `","keys":[{"tokenId":1,"length":52}]}`,
			"badbase64": `{"securityPolicyUri":"` + security.PolicyURIAes128CTR + `","keys":[{"tokenId":1,"key":"***"}]}`,
			"badlength": `{"securityPolicyUri":"` + security.PolicyURIAes128CTR + `","keys":[{"tokenId":1,"key":"AA=="}]}`,
		} {
			f := filepath.Join(dir, name)
			if err := os.WriteFile(f, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := loadKeysFile(f, ""); err == nil {
				t.Errorf("%s: expected an error", name)
			}
		}
	})
}

// TestE2E_PubSubConfig configures the PubSub of a go-opcua server through
// its PublishSubscribe methods.
func TestE2E_PubSubConfig(t *testing.T) {
	s := startServer(t, server.WithPubSub(nil))
	src := s.ns.AddNewVariableStringNode("Src", int32(7))
	dst := s.ns.AddNewVariableStringNode("Dst", int32(0))
	ep := []string{"-e", s.url}
	run := func(args ...string) string {
		t.Helper()
		return strings.TrimSpace(mustRun(t, append(append([]string{"pubsub", "config"}, args...), ep...)...))
	}

	out := run("show")
	wantContains(t, out, "PublishSubscribe", "i=14443", "Operational")

	url := freeUDPURL(t)
	conn := run("add-connection", "--name", "pub", "--url", url, "--publisher-id", "9", "-q")
	var ds model.PubSubAdded
	if err := json.Unmarshal([]byte(run("add-dataset", "--name", "pds", "--var", "Value="+src.ID().String(),
		"--var", fmt.Sprintf("Missing=ns=%d;s=nosuch", s.ns.ID()), "--format", "json")), &ds); err != nil {
		t.Fatal(err)
	}
	if ds.Kind != "dataset" || len(ds.Results) != 2 || ds.Results[0].StatusCode != 0 || ds.Results[1].StatusCode == 0 {
		t.Errorf("unexpected add-dataset result: %+v", ds)
	}
	wg := run("add-writer-group", "--connection", conn, "--name", "wg", "--writer-group-id", "1", "--interval", "50ms", "-q")
	w := run("add-writer", "--group", wg, "--name", "w", "--writer-id", "1", "--dataset", "pds", "-q")
	b := startBroker(t)
	mq := run("add-connection", "--name", "mq", "--url", b.url(), "--encoding", "json", "--publisher-id", "str:srv", "-q")
	out = run("add-writer-group", "--connection", mq, "--name", "wg2", "--writer-group-id", "2", "--encoding", "json",
		"--queue", "q", "--keep-alive", "1s")
	wantContains(t, out, `added writer-group "wg2"`)
	wg3 := run("add-writer-group", "--connection", mq, "--name", "wg3", "--writer-group-id", "3", "--queue", "q3", "-q")
	run("add-writer", "--group", wg3, "--name", "w3", "--writer-id", "3", "--dataset", "pds", "--queue", "q",
		"--metadata-queue", "m", "--key-frame-count", "5", "--field-encoding", "rawdata")
	run("remove", "connection", "--node", mq)

	out = run("show")
	wantContains(t, out, "connection", "pub", conn, "PublisherId=9", "pubsub-udp-uadp",
		"writer-group", "WriterGroupId=1", "PublishingInterval=50ms", "writer", w, "DataSetWriterId=1",
		"dataset", "Value:Int32")
	var tree model.PubSubComponent
	if err := json.Unmarshal([]byte(run("show", "--format", "json")), &tree); err != nil {
		t.Fatal(err)
	}
	if len(tree.Children) != 2 || tree.Children[0].Name != "pub" || len(tree.Children[0].Children) != 1 ||
		tree.Children[0].Children[0].Children[0].NodeID != w || tree.Children[1].Kind != "dataset" {
		t.Errorf("unexpected tree: %+v", tree)
	}

	// The server publishes the variable.
	sub := mustRun(t, "pubsub", "subscribe", "--url", url, "--count", "1", "--duration", "15s", "--metadata-node", ds.NodeID, "-e", s.url)
	wantContains(t, sub, "pub=9 wg=1 writer=1", "Value=7")

	// A reader that writes what the writer publishes to Dst.
	subConn := run("add-connection", "--name", "sub", "--url", url, "-q")
	rg := run("add-reader-group", "--connection", subConn, "--name", "rg", "-q")
	rd := run("add-reader", "--group", rg, "--name", "r", "--publisher-id", "9", "--writer-group-id", "1",
		"--writer-id", "1", "--field", "Value:Int32", "--target", dst.ID().String(), "--message-receive-timeout", "2s", "-q")
	deadline := time.Now().Add(10 * time.Second)
	for {
		v := strings.TrimSpace(mustRun(t, "get", "value", "-e", s.url, "-n", dst.ID().String(), "-q"))
		if v == "7" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the reader did not write the target: %s", v)
		}
		time.Sleep(50 * time.Millisecond)
	}
	out = run("show")
	wantContains(t, out, "reader-group", "rg", "reader", rd, "WriterGroupId=1", "Value:Int32")

	// Enable and disable.
	wantContains(t, run("disable", "--node", wg), "Disabled")
	wantContains(t, run("enable", "--node", wg, "--format", "json"), `"state":"Operational"`)
	wantContains(t, run("disable"), "i=14443", "Disabled")
	wantContains(t, run("enable", "-q"), "Operational")
	if _, _, err := runCLI(t, "pubsub", "config", "enable", "--node", wg, "-e", s.url); err == nil {
		t.Error("enabling an enabled group succeeded")
	}

	// Errors.
	for _, args := range [][]string{
		{"remove", "group", "--node", wg},
		{"remove", "thing", "--node", wg},
		{"remove", "connection"},
		{"add-writer", "--group", wg, "--name", "w2", "--writer-id", "2", "--dataset", "nosuch"},
		{"add-writer", "--group", wg, "--name", "w2"},
		{"add-writer", "--name", "w2", "--dataset", "pds"},
		{"add-writer", "--group", "bogus", "--name", "w2", "--dataset", "pds"},
		{"add-writer", "--group", wg, "--name", "w2", "--dataset", "pds", "--field-encoding", "x"},
		{"add-connection", "--name", "x"},
		{"add-connection", "--name", "x", "--url", "opc.udp://127.0.0.1:1", "--encoding", "json"},
		{"add-connection", "--name", "x", "--url", "opc.udp://127.0.0.1:1", "--publisher-id", "u8:999"},
		{"add-connection", "--name", "pub", "--url", url},
		{"add-dataset", "--name", "x"},
		{"add-dataset", "--name", "x", "--var", "nonodeid"},
		{"add-dataset", "--name", "x", "--var", "a=bogus"},
		{"add-writer-group", "--connection", conn},
		{"add-writer-group", "--connection", conn, "--name", "x", "--message-security", "sign"},
		{"add-writer-group", "--connection", conn, "--name", "x", "--message-security", "x"},
		{"add-writer-group", "--connection", conn, "--name", "x", "--encoding", "x"},
		{"add-reader-group", "--connection", conn},
		{"add-reader-group", "--name", "x"},
		{"add-reader-group", "--connection", conn, "--name", "x", "--message-security", "sign"},
		{"add-reader", "--group", rg},
		{"add-reader", "--name", "x"},
		{"add-reader", "--group", rg, "--name", "x", "--publisher-id", "u8:999"},
		{"add-reader", "--group", rg, "--name", "x", "--field", "bad"},
		{"add-reader", "--group", rg, "--name", "x", "--target", "bogus"},
		{"enable", "--node", "bogus"},
	} {
		if _, _, err := runCLI(t, append(append([]string{"pubsub", "config"}, args...), ep...)...); err == nil {
			t.Errorf("config %v: expected an error", args)
		}
	}
	if _, _, err := runCLI(t, "pubsub", "config", "show"); err == nil {
		t.Error("config show without endpoint succeeded")
	}

	// Secured groups are accepted by the server.
	run("add-reader-group", "--connection", subConn, "--name", "srg", "--message-security", "sign", "--security-group", "sg")
	run("add-writer-group", "--connection", conn, "--name", "swg", "--writer-group-id", "3", "--message-security", "sign-and-encrypt", "--security-group", "sg")

	// Removal.
	wantContains(t, run("remove", "reader", "--node", rd, "--parent", rg), "removed reader")
	run("remove", "writer", "--node", w, "--parent", wg)
	run("remove", "group", "--node", rg, "--parent", subConn)
	run("remove", "connection", "--node", subConn)
	run("remove", "connection", "--node", conn)
	run("remove", "dataset", "--node", ds.NodeID, "-q")
	out = run("show", "--format", "json")
	var empty model.PubSubComponent
	if err := json.Unmarshal([]byte(out), &empty); err != nil || len(empty.Children) != 0 {
		t.Errorf("components left after removal: %s", out)
	}
}
