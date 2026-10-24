// SPDX-License-Identifier: MIT

package parse

import (
	"testing"
	"time"

	"github.com/otfabric/go-opcua/pubsub/message"
	"github.com/otfabric/go-opcua/ua"
)

func TestBuiltinType(t *testing.T) {
	for in, want := range map[string]ua.TypeID{
		"Boolean": ua.TypeIDBoolean, "bool": ua.TypeIDBoolean, "SByte": ua.TypeIDSByte, "Byte": ua.TypeIDByte,
		"Int16": ua.TypeIDInt16, "UInt16": ua.TypeIDUint16, "int32": ua.TypeIDInt32, "UINT32": ua.TypeIDUint32,
		"Int64": ua.TypeIDInt64, "UInt64": ua.TypeIDUint64, "Float": ua.TypeIDFloat, " Double ": ua.TypeIDDouble,
		"String": ua.TypeIDString, "DateTime": ua.TypeIDDateTime, "Guid": ua.TypeIDGUID, "ByteString": ua.TypeIDByteString,
		"NodeId": ua.TypeIDNodeID, "StatusCode": ua.TypeIDStatusCode, "LocalizedText": ua.TypeIDLocalizedText,
		"Variant": ua.TypeIDVariant,
	} {
		got, err := BuiltinType(in)
		if err != nil || got != want {
			t.Errorf("BuiltinType(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	if _, err := BuiltinType("Struct"); err == nil {
		t.Error("BuiltinType(Struct): expected an error")
	}
}

func TestBuiltinTypeName(t *testing.T) {
	for typ, want := range map[ua.TypeID]string{
		ua.TypeIDInt32: "Int32", ua.TypeIDUint16: "UInt16", ua.TypeIDUint32: "UInt32", ua.TypeIDUint64: "UInt64",
		ua.TypeIDGUID: "Guid", ua.TypeIDXMLElement: "XmlElement", ua.TypeIDDouble: "Double", ua.TypeIDString: "String",
	} {
		if got := BuiltinTypeName(typ); got != want {
			t.Errorf("BuiltinTypeName(%d) = %q, want %q", typ, got, want)
		}
	}
}

func TestPubSubField(t *testing.T) {
	f, err := PubSubField("Temp:Double")
	if err != nil || f.Name != "Temp" || f.Type != ua.TypeIDDouble || f.Array {
		t.Errorf("Temp:Double = %+v, %v", f, err)
	}
	f, err = PubSubField("Values:Int32[]")
	if err != nil || f.Name != "Values" || f.Type != ua.TypeIDInt32 || !f.Array {
		t.Errorf("Values:Int32[] = %+v, %v", f, err)
	}
	for _, bad := range []string{"Temp", ":Double", "Temp:Struct", "Temp:"} {
		if _, err := PubSubField(bad); err == nil {
			t.Errorf("PubSubField(%q): expected an error", bad)
		}
	}
}

func TestPubSubFieldWithValue(t *testing.T) {
	ts := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	for in, want := range map[string]interface{}{
		"a:Boolean=true":                  true,
		"a:SByte=-5":                      int8(-5),
		"a:Byte=200":                      uint8(200),
		"a:Int16=-300":                    int16(-300),
		"a:UInt16=300":                    uint16(300),
		"a:Int32=-7":                      int32(-7),
		"a:UInt32=7":                      uint32(7),
		"a:Int64=-8":                      int64(-8),
		"a:UInt64=8":                      uint64(8),
		"a:Float=1.5":                     float32(1.5),
		"a:Double=2.5":                    2.5,
		"a:String=x=y":                    "x=y",
		"a:DateTime=2026-10-24T12:00:00Z": ts,
		"a:StatusCode=0x80340000":         ua.StatusBadNodeIDUnknown,
	} {
		f, err := PubSubFieldWithValue(in)
		if err != nil {
			t.Errorf("%s: %v", in, err)
			continue
		}
		if f.Value != want {
			t.Errorf("%s: value %#v, want %#v", in, f.Value, want)
		}
	}
	for in, check := range map[string]func(interface{}) bool{
		"a:ByteString=AQI=":                           func(v interface{}) bool { b, ok := v.([]byte); return ok && len(b) == 2 },
		"a:Guid=72962B91-FA75-4AE6-8D28-B404DC7DAF63": func(v interface{}) bool { g, ok := v.(*ua.GUID); return ok && g != nil },
		"a:NodeId=ns=1;s=x":                           func(v interface{}) bool { n, ok := v.(*ua.NodeID); return ok && n.String() == "ns=1;s=x" },
		"a:QualifiedName=q":                           func(v interface{}) bool { q, ok := v.(*ua.QualifiedName); return ok && q.Name == "q" },
		"a:LocalizedText=hello":                       func(v interface{}) bool { l, ok := v.(*ua.LocalizedText); return ok && l.Text == "hello" },
	} {
		f, err := PubSubFieldWithValue(in)
		if err != nil || !check(f.Value) {
			t.Errorf("%s: %#v, %v", in, f.Value, err)
		}
	}
	for _, bad := range []string{
		"a:Int32", "a=1", "a:Int32[]=1", "a:Int32=x", "a:SByte=300", "a:Byte=-1",
		"a:StatusCode=zz", "a:Variant=1", "a:DataValue=1", "a:Guid=short", "a:Struct=1",
	} {
		if _, err := PubSubFieldWithValue(bad); err == nil {
			t.Errorf("PubSubFieldWithValue(%q): expected an error", bad)
		}
	}
}

func TestPublisherID(t *testing.T) {
	for in, want := range map[string]message.PublisherID{
		"7":         message.NewPublisherID(message.PublisherIDUInt16, 7),
		"byte:3":    message.NewPublisherID(message.PublisherIDByte, 3),
		"u8:3":      message.NewPublisherID(message.PublisherIDByte, 3),
		"u16:9":     message.NewPublisherID(message.PublisherIDUInt16, 9),
		"u32:70000": message.NewPublisherID(message.PublisherIDUInt32, 70000),
		"uint64:1":  message.NewPublisherID(message.PublisherIDUInt64, 1),
		"str:12":    message.StringPublisherID("12"),
		"line1":     message.StringPublisherID("line1"),
		"urn:a:b":   message.StringPublisherID("urn:a:b"),
	} {
		got, err := PublisherID(in)
		if err != nil || !got.Equal(want) {
			t.Errorf("PublisherID(%q) = %+v, %v; want %+v", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "70000", "byte:256", "u16:x"} {
		if _, err := PublisherID(bad); err == nil {
			t.Errorf("PublisherID(%q): expected an error", bad)
		}
	}
}

func TestPublisherIDValue(t *testing.T) {
	for _, tc := range []struct {
		in   message.PublisherID
		want interface{}
	}{
		{message.NewPublisherID(message.PublisherIDByte, 1), uint8(1)},
		{message.NewPublisherID(message.PublisherIDUInt16, 2), uint16(2)},
		{message.NewPublisherID(message.PublisherIDUInt32, 3), uint32(3)},
		{message.NewPublisherID(message.PublisherIDUInt64, 4), uint64(4)},
		{message.StringPublisherID("s"), "s"},
	} {
		if got := PublisherIDValue(tc.in); got != tc.want {
			t.Errorf("PublisherIDValue(%v) = %#v, want %#v", tc.in, got, tc.want)
		}
	}
}

func TestMessageSecurity(t *testing.T) {
	for in, want := range map[string]ua.MessageSecurityMode{
		"": ua.MessageSecurityModeNone, "none": ua.MessageSecurityModeNone, "Sign": ua.MessageSecurityModeSign,
		"sign-and-encrypt": ua.MessageSecurityModeSignAndEncrypt, "SignAndEncrypt": ua.MessageSecurityModeSignAndEncrypt,
		"sign_and_encrypt": ua.MessageSecurityModeSignAndEncrypt, "encrypt": ua.MessageSecurityModeSignAndEncrypt,
	} {
		if got, err := MessageSecurity(in); err != nil || got != want {
			t.Errorf("MessageSecurity(%q) = %v, %v", in, got, err)
		}
	}
	if _, err := MessageSecurity("tls"); err == nil {
		t.Error("expected an error")
	}
}

func TestParsePubSubEncoding(t *testing.T) {
	for _, tc := range []struct {
		in        string
		allowAuto bool
		want      PubSubEncoding
		wantErr   bool
	}{
		{"uadp", false, EncodingUADP, false},
		{"JSON", false, EncodingJSON, false},
		{"auto", true, EncodingAuto, false},
		{"", true, EncodingAuto, false},
		{"auto", false, "", true},
		{"xml", true, "", true},
	} {
		got, err := ParsePubSubEncoding(tc.in, tc.allowAuto)
		if (err != nil) != tc.wantErr || got != tc.want {
			t.Errorf("ParsePubSubEncoding(%q, %v) = %q, %v", tc.in, tc.allowAuto, got, err)
		}
	}
}

func TestFieldEncoding(t *testing.T) {
	for in, want := range map[string]ua.DataSetFieldContentMask{
		"": 0, "variant": 0, "rawdata": ua.DataSetFieldContentMaskRawData, "raw": ua.DataSetFieldContentMaskRawData,
		"datavalue": ua.DataSetFieldContentMaskStatusCode | ua.DataSetFieldContentMaskSourceTimestamp,
	} {
		if got, err := FieldEncoding(in); err != nil || got != want {
			t.Errorf("FieldEncoding(%q) = %v, %v", in, got, err)
		}
	}
	if _, err := FieldEncoding("xml"); err == nil {
		t.Error("expected an error")
	}
}
