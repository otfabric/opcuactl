// SPDX-License-Identifier: MIT

package parse

import (
	"testing"

	"github.com/otfabric/go-opcua/ua"
)

// The parsers take what a user types or a file holds. They reject bad input
// with an error; none may panic.

func FuzzNodeID(f *testing.F) {
	for _, s := range []string{"i=2258", "ns=2;s=Tank.Level", "ns=1;g=72962B91-FA75-4AE6-8D28-B404DC7DAF63", "ns=1;b=AQID", "", "ns=", "ns=99999;i=1"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		n, err := NodeID(s)
		if err != nil {
			return
		}
		if n == nil {
			t.Fatalf("NodeID(%q): nil without an error", s)
		}
		again, err := NodeID(n.String())
		if err != nil {
			t.Fatalf("NodeID(%q) printed as %q, which does not parse: %v", s, n.String(), err)
		}
		if again.String() != n.String() {
			t.Fatalf("NodeID(%q): %q became %q", s, n.String(), again.String())
		}
	})
}

func FuzzNodeIDWithNamespace(f *testing.F) {
	f.Add("s=Tank", "2")
	f.Add("ns=2;s=Tank", "http://example.org/")
	f.Add("", "")
	f.Fuzz(func(t *testing.T, s, namespace string) {
		_, _ = NodeIDWithNamespace(s, namespace)
	})
}

func FuzzCoerceValue(f *testing.F) {
	for _, tn := range []string{"bool", "int16", "uint16", "int32", "uint32", "int64", "uint64", "float", "double", "string", "datetime", "bytestring", "guid", ""} {
		f.Add("1", tn)
		f.Add("-1e400", tn)
	}
	f.Add("2026-10-31T12:00:00Z", "datetime")
	f.Fuzz(func(t *testing.T, value, typeName string) {
		_, _ = CoerceValue(value, typeName)
	})
}

func FuzzUAVariant(f *testing.F) {
	f.Add(`{"UaType":6,"Value":[1,2,3]}`, "ua-json")
	f.Add(`{"UaType":21,"Value":{"Locale":"en","Text":"Tank 1"}}`, "ua-json")
	f.Add(`<Int32>5</Int32>`, "ua-xml")
	f.Add(``, "ua-json")
	f.Fuzz(func(t *testing.T, value, typeName string) {
		_, _ = UAVariant(value, typeName)
	})
}

func FuzzJSONArguments(f *testing.F) {
	f.Add([]byte(`[42, "hello"]`))
	f.Add([]byte(`[]`))
	f.Add([]byte(`[null, {"a": 1}]`))
	f.Add([]byte(`[1e400, [1, [2]]]`))
	f.Add([]byte(`{`))
	inputs := []*ua.Argument{
		{Name: "a", DataType: ua.NewNumericNodeID(0, uint32(ua.TypeIDInt32)), ValueRank: -1},
		{Name: "b", DataType: ua.NewNumericNodeID(0, uint32(ua.TypeIDString)), ValueRank: 1},
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = JSONArguments(data, inputs)
	})
}

func FuzzEvents(f *testing.F) {
	f.Add([]byte(`[{"time":"2026-10-31T12:00:00Z","fields":{"Message":"x"}}]`))
	f.Add([]byte(`[]`))
	f.Add([]byte(`[{}]`))
	f.Add([]byte(`{`))
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = Events(data)
	})
}

func FuzzEventFieldPath(f *testing.F) {
	f.Add("Severity")
	f.Add("1:Tank/2:Level")
	f.Add("/")
	f.Fuzz(func(t *testing.T, s string) {
		_, _ = EventFieldPath(s)
		_, _ = EventSelectClauses([]string{s})
	})
}

func FuzzEventID(f *testing.F) {
	f.Add("latest")
	f.Add("00ff")
	f.Add("0x00ff")
	f.Add("zz")
	f.Fuzz(func(t *testing.T, s string) {
		_, _, _ = EventID(s)
	})
}

func FuzzPubSubField(f *testing.F) {
	f.Add("Level:float")
	f.Add("Level:float=1.5")
	f.Add("=")
	f.Fuzz(func(t *testing.T, s string) {
		_, _ = PubSubField(s)
		_, _ = PubSubFieldWithValue(s)
	})
}

func FuzzPublisherID(f *testing.F) {
	f.Add("1")
	f.Add("uint64:18446744073709551615")
	f.Add("string:plant")
	f.Add("byte:256")
	f.Fuzz(func(t *testing.T, s string) {
		id, err := PublisherID(s)
		if err == nil {
			_ = PublisherIDValue(id)
		}
	})
}

func FuzzStatusCodeAndTarget(f *testing.F) {
	f.Add("Good")
	f.Add("0x80340000")
	f.Add("1:ping")
	f.Fuzz(func(t *testing.T, s string) {
		_, _ = StatusCode(s)
		_, _, _ = ActionTarget(s)
		_, _ = TransportProperties([]string{s})
		_, _ = TrustListMasks([]string{s})
		_, _ = CertificateType(s)
		_, _, _ = CertificateGroup(s)
	})
}
