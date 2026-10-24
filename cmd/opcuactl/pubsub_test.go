// SPDX-License-Identifier: MIT

package opcuactl

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-opcua/pubsub/message"
	"github.com/otfabric/go-opcua/pubsub/transport"
	"github.com/otfabric/go-opcua/pubsub/transport/eth"
	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/opcuactl/internal/model"
	"github.com/otfabric/opcuactl/internal/parse"
	"github.com/spf13/pflag"
)

func TestDetectEncoding(t *testing.T) {
	for in, want := range map[string]parse.PubSubEncoding{
		`{"a":1}`: parse.EncodingJSON, "  \n[1]": parse.EncodingJSON, "\x91\x01": parse.EncodingUADP, "": parse.EncodingUADP,
	} {
		if got := detectEncoding([]byte(in)); got != want {
			t.Errorf("detectEncoding(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestDecodeInput(t *testing.T) {
	for _, tc := range []struct {
		in, form, want string
		wantErr        bool
	}{
		{"f1 02\n0a", "auto", "\xf1\x02\x0a", false},
		{"0xF102", "hex", "\xf1\x02", false},
		{"f1:02", "hex", "\xf1\x02", false},
		{`{"x":1}`, "auto", `{"x":1}`, false},
		{"\xf1\x02", "auto", "\xf1\x02", false},
		{"8QI=", "base64", "\xf1\x02", false},
		{"zz", "raw", "zz", false},
		{"zz", "hex", "", true},
		{"!!", "base64", "", true},
		{"x", "pcap", "", true},
	} {
		got, err := decodeInput([]byte(tc.in), tc.form)
		if (err != nil) != tc.wantErr || string(got) != tc.want {
			t.Errorf("decodeInput(%q, %s) = %q, %v", tc.in, tc.form, got, err)
		}
	}
}

func TestDecodeMetaDataErrors(t *testing.T) {
	for _, in := range []string{`{`, `{"Name":"x"}`, `{"MessageType":"ua-metadata","MetaData":7}`} {
		if _, err := decodeMetaData([]byte(in)); err == nil {
			t.Errorf("decodeMetaData(%s): expected an error", in)
		}
	}
}

func TestMessageTypeName(t *testing.T) {
	for typ, want := range map[message.DataSetMessageType]string{
		message.DataSetMessageKeyFrame: "keyframe", message.DataSetMessageDeltaFrame: "deltaframe",
		message.DataSetMessageEvent: "event", message.DataSetMessageKeepAlive: "keepalive",
	} {
		if got := messageTypeName(typ); got != want {
			t.Errorf("messageTypeName(%d) = %q", typ, got)
		}
	}
	if got := messageTypeName(9); got == "" {
		t.Error("empty name for an unknown type")
	}
}

func TestJSONSafeValue(t *testing.T) {
	ts := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		in   interface{}
		want interface{}
	}{
		{ua.NewStringNodeID(1, "x"), "ns=1;s=x"},
		{ua.NewExpandedNodeID(ua.NewNumericNodeID(0, 85), "", 0), "i=85"},
		{&ua.QualifiedName{Name: "q"}, "q"},
		{&ua.LocalizedText{Text: "t"}, "t"},
		{ua.StatusBadNodeIDUnknown, ua.StatusBadNodeIDUnknown.Symbol()},
		{ts, "2026-10-24T00:00:00Z"},
		{int32(3), int32(3)},
	} {
		if got := jsonSafeValue(tc.in); got != tc.want {
			t.Errorf("jsonSafeValue(%#v) = %#v, want %#v", tc.in, got, tc.want)
		}
	}
	if got, ok := jsonSafeValue(ua.NewGUID("72962B91-FA75-4AE6-8D28-B404DC7DAF63")).(string); !ok || got == "" {
		t.Errorf("GUID: %v", got)
	}
}

func TestFieldModel(t *testing.T) {
	md := &ua.DataSetMetaDataType{Fields: []*ua.FieldMetaData{{Name: "A"}, {Name: "B"}}}
	f := fieldModel(&message.Field{Index: 1, Value: &ua.DataValue{Value: ua.MustVariant([]int32{1, 2}), Status: ua.StatusBadNodeIDUnknown}}, md, false)
	if f.Name != "B" || f.Type != "Int32[]" || f.Status != ua.StatusBadNodeIDUnknown.Symbol() {
		t.Errorf("unexpected field %+v", f)
	}
	f = fieldModel(&message.Field{Index: 0, Value: &ua.DataValue{Value: ua.MustVariant([]byte{1})}}, nil, true)
	if f.Type != "ByteString" || f.Status == "" {
		t.Errorf("unexpected field %+v", f)
	}
	f = fieldModel(&message.Field{Index: 5}, md, false)
	if f.Name != "" || f.Value != nil {
		t.Errorf("unexpected field %+v", f)
	}
}

func TestPubSubLines(t *testing.T) {
	line := dataSetLine(model.PubSubDataSet{
		Received: "r", PublisherID: "p", WriterGroupName: "G", DataSetWriterID: 2, SequenceNumber: 3,
		MessageType: "deltaframe", Security: "Sign", SecurityTokenID: 4, Status: "Bad", Topic: "t",
		Fields: []model.PubSubField{{Index: 1, Value: 5, Status: "Uncertain"}, {Name: "N", Value: []int{1, 2}}},
	})
	for _, want := range []string{"r  pub=p wg=G writer=2 seq=3 deltaframe", "[Sign token=4]", "[Bad]", "topic=t", "#1=5[Uncertain]", "N=1, 2"} {
		if !strings.Contains(line, want) {
			t.Errorf("%q does not contain %q", line, want)
		}
	}
	md := metaDataLine(model.PubSubMetaData{PublisherID: "p", DataSetWriterID: 1, Name: "n", Version: "1.2",
		Fields: []model.PubSubFieldMeta{{Name: "a", Type: "Int32"}}})
	if md != `metadata pub=p writer=1 "n" version=1.2 fields: a:Int32` {
		t.Errorf("metaDataLine = %q", md)
	}
	if st := statusLine(map[string]interface{}{"b": 2, "a": 1}); st != "status a=1 b=2" {
		t.Errorf("statusLine = %q", st)
	}
}

func TestMetaDataModel(t *testing.T) {
	md := &ua.DataSetMetaDataType{Name: "n", ConfigurationVersion: &ua.ConfigurationVersionDataType{MajorVersion: 1, MinorVersion: 2},
		Fields: []*ua.FieldMetaData{{Name: "a", BuiltInType: uint8(ua.TypeIDDouble), ValueRank: 1}}}
	m := metaDataModel(nil, "uadp", message.StringPublisherID("p"), 3, md)
	if m.Version != "1.2" || m.Fields[0].Type != "Double[]" || m.PublisherID != "p" || m.DataSetWriterID != 3 {
		t.Errorf("unexpected model %+v", m)
	}
}

func TestTransportProfile(t *testing.T) {
	for _, tc := range []struct {
		url  string
		enc  parse.PubSubEncoding
		want string // "" = error
	}{
		{"opc.udp://1.2.3.4:4840", parse.EncodingUADP, transport.ProfileUDPUADP},
		{"opc.udp://1.2.3.4:4840", parse.EncodingAuto, transport.ProfileUDPUADP},
		{"opc.udp://1.2.3.4:4840", parse.EncodingJSON, ""},
		{"mqtt://b:1883", parse.EncodingJSON, transport.ProfileMQTTJSON},
		{"mqtts://b:8883", parse.EncodingUADP, transport.ProfileMQTTUADP},
		{"opc.eth://01-00-5E-00-00-01", parse.EncodingUADP, transport.ProfileEthUADP},
		{"opc.eth://01-00-5E-00-00-01:5.3", parse.EncodingAuto, transport.ProfileEthUADP},
		{"opc.eth://01-00-5E-00-00-01", parse.EncodingJSON, ""},
		{"amqp://b:5672", parse.EncodingUADP, transport.ProfileAMQPUADP},
		{"amqps://b:5671", parse.EncodingJSON, transport.ProfileAMQPJSON},
		{"wss://b", parse.EncodingUADP, ""},
		{"nonsense", parse.EncodingUADP, ""},
	} {
		got, err := transportProfile(tc.url, tc.enc)
		if got != tc.want || (err != nil) != (tc.want == "") {
			t.Errorf("transportProfile(%s, %s) = %q, %v; want %q", tc.url, tc.enc, got, err, tc.want)
		}
	}
	_, err := transportProfile("opc.eth://01-00-5E-00-00-01", parse.EncodingJSON)
	if err == nil || !strings.Contains(err.Error(), "not available over Ethernet") {
		t.Errorf("JSON over Ethernet: %v", err)
	}
}

func TestTransportConfig(t *testing.T) {
	t.Setenv("OPCUACTL_BROKER_PASSWORD", "")
	t.Setenv("OPCUACTL_MQTT_PASSWORD", "")
	f := pubsubTransportFlags{
		url: "mqtt://b:1883", username: "u", password: "p", clientID: "c", mqttVersion: "5", qos: 2,
		properties: []string{"connection-Session Expiry Interval=60"},
	}
	cfg, err := f.config(parse.EncodingAuto)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ProfileURI != transport.ProfileMQTTUADP || cfg.MQTTVersion != "5.0" || cfg.Username != "u" || cfg.Password != "p" ||
		cfg.ClientID != "c" || cfg.Properties["connection-Session Expiry Interval"] != "60" {
		t.Errorf("unexpected config %+v", cfg)
	}

	// The broker password comes from the environment, the new variable first.
	t.Setenv("OPCUACTL_MQTT_PASSWORD", "old")
	f = pubsubTransportFlags{url: "amqp://b:5672"}
	if cfg, _ := f.config(parse.EncodingJSON); cfg.Password != "old" || cfg.ProfileURI != transport.ProfileAMQPJSON {
		t.Errorf("OPCUACTL_MQTT_PASSWORD: %+v", cfg)
	}
	t.Setenv("OPCUACTL_BROKER_PASSWORD", "new")
	if cfg, _ := f.config(parse.EncodingJSON); cfg.Password != "new" {
		t.Errorf("OPCUACTL_BROKER_PASSWORD: %+v", cfg)
	}

	f = pubsubTransportFlags{url: "opc.eth://01-00-5E-00-00-01", iface: "eth0"}
	if cfg, err := f.config(parse.EncodingAuto); err != nil || cfg.ProfileURI != transport.ProfileEthUADP || cfg.Interface != "eth0" {
		t.Errorf("ethernet: %+v, %v", cfg, err)
	}

	for _, tc := range []struct {
		f    pubsubTransportFlags
		enc  parse.PubSubEncoding
		want string
	}{
		{pubsubTransportFlags{}, parse.EncodingAuto, "--url is required"},
		{pubsubTransportFlags{url: "mqtt://b", qos: 3}, parse.EncodingAuto, "--qos must be 0, 1 or 2"},
		{pubsubTransportFlags{url: "opc.eth://01-00-5E-00-00-01"}, parse.EncodingUADP, "--interface is required"},
		{pubsubTransportFlags{url: "opc.eth://01-00-5E-00-00-01", iface: "eth0"}, parse.EncodingJSON, "not available over Ethernet"},
		{pubsubTransportFlags{url: "amqp://b", mqttVersion: "5.0"}, parse.EncodingAuto, "--mqtt-version needs an MQTT broker"},
		{pubsubTransportFlags{url: "mqtt://b", mqttVersion: "4"}, parse.EncodingAuto, "unknown MQTT version"},
		{pubsubTransportFlags{url: "mqtt://b", properties: []string{"x"}}, parse.EncodingAuto, "want key=value"},
		{pubsubTransportFlags{url: "http://b"}, parse.EncodingAuto, "unsupported transport"},
	} {
		_, err := tc.f.config(tc.enc)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("config(%+v): %v, want %q", tc.f, err, tc.want)
		}
	}
}

func TestTransportError(t *testing.T) {
	perm := fmt.Errorf("open: %w", os.ErrPermission)
	if err := transportError("opc.eth://01-00-5E-00-00-01", perm); !errors.Is(err, os.ErrPermission) || !strings.Contains(err.Error(), "cap_net_raw") {
		t.Errorf("permission: %v", err)
	}
	if err := transportError("opc.eth://01-00-5E-00-00-01", eth.ErrNotSupported); !strings.Contains(err.Error(), "Linux, macOS or FreeBSD") {
		t.Errorf("not supported: %v", err)
	}
	if err := transportError("opc.udp://1.2.3.4:4840", perm); err != perm {
		t.Errorf("UDP errors are unchanged: %v", err)
	}
}

func TestPubSubTransportFlagsAliases(t *testing.T) {
	var f pubsubTransportFlags
	fs := pflag.NewFlagSet("t", pflag.ContinueOnError)
	f.register(fs)
	if err := fs.Parse([]string{"--mqtt-username", "a", "--mqtt-password", "b", "--mqtt-client-id", "c", "--transport-property", "k=v"}); err != nil {
		t.Fatal(err)
	}
	if f.username != "a" || f.password != "b" || f.clientID != "c" || len(f.properties) != 1 {
		t.Errorf("the --mqtt-* flags no longer set the broker settings: %+v", f)
	}
	for _, name := range []string{"mqtt-username", "mqtt-password", "mqtt-client-id"} {
		if fl := fs.Lookup(name); fl == nil || fl.Deprecated == "" {
			t.Errorf("--%s is not a deprecated alias", name)
		}
	}
}

func TestPropertyString(t *testing.T) {
	for _, tc := range []struct {
		name string
		v    interface{}
		want string
	}{
		{"TransportProfileUri", "http://opcfoundation.org/UA-Profile/Transport/pubsub-mqtt-json", "pubsub-mqtt-json"},
		{"SecurityMode", int32(3), "SignAndEncrypt"},
		{"PublishingInterval", 250.0, "250ms"},
		{"WriterGroupId", uint16(5), "5"},
		{"DataSetMetaData", ua.NewExtensionObject(&ua.DataSetMetaDataType{Fields: []*ua.FieldMetaData{{Name: "a", BuiltInType: 6}}}), "a:Int32"},
	} {
		if got := propertyString(tc.name, ua.MustVariant(tc.v)); got != tc.want {
			t.Errorf("propertyString(%s, %v) = %q, want %q", tc.name, tc.v, got, tc.want)
		}
	}
	if got := propertiesString(map[string]string{"b": "2", "a": "1"}); got != "a=1 b=2" {
		t.Errorf("propertiesString = %q", got)
	}
}

func TestFieldEncodingOf(t *testing.T) {
	if fieldEncodingOf(0) != message.FieldEncodingVariant ||
		fieldEncodingOf(ua.DataSetFieldContentMaskRawData) != message.FieldEncodingRawData ||
		fieldEncodingOf(ua.DataSetFieldContentMaskStatusCode) != message.FieldEncodingDataValue {
		t.Error("unexpected field encodings")
	}
	if v := versionTime(time.Date(2000, 1, 1, 0, 1, 0, 0, time.UTC)); v != 60 {
		t.Errorf("versionTime = %d", v)
	}
}
