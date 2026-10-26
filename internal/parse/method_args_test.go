// SPDX-License-Identifier: MIT

package parse

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-opcua/id"
	"github.com/otfabric/go-opcua/ua"
)

func argOf(dataType uint32, rank int32) *ua.Argument {
	return &ua.Argument{Name: "x", DataType: ua.NewNumericNodeID(0, dataType), ValueRank: rank}
}

func TestJSONArgumentsPlain(t *testing.T) {
	got, err := JSONArguments([]byte(`[5, "hello", true, null, [1, 2], {"a": 1}]`), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []any{float64(5), "hello", true, nil, []any{float64(1), float64(2)}, map[string]any{"a": float64(1)}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}

	for _, bad := range []string{`[1,`, `{"a": 1}`, `5`, `[1] [2]`, ``} {
		if _, err := JSONArguments([]byte(bad), nil); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
	if _, err := JSONArguments([]byte(`"x"`), nil); !errors.Is(err, ErrNotJSONArray) {
		t.Errorf("not an array: %v", err)
	}
}

func TestJSONArgumentsTyped(t *testing.T) {
	inputs := []*ua.Argument{
		{Name: "a", DataType: ua.NewNumericNodeID(0, id.Int32), ValueRank: -1},
		{Name: "b", DataType: ua.NewNumericNodeID(0, id.BaseDataType), ValueRank: -1},
	}
	got, err := JSONArguments([]byte(`[2, 3, 4]`), inputs)
	if err != nil {
		t.Fatal(err)
	}
	// Int32, any (Double) and an argument the method does not have (Double).
	if want := []any{int32(2), float64(3), float64(4)}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
	_, err = JSONArguments([]byte(`[2.5]`), inputs)
	if err == nil || err.Error() != "argument 0 (a): 2.5 is not an integer (Int32)" {
		t.Errorf("fraction: %v", err)
	}
	inputs[0].Name = ""
	_, err = JSONArguments([]byte(`[2.5]`), inputs)
	if err == nil || err.Error() != "argument 0: 2.5 is not an integer (Int32)" {
		t.Errorf("unnamed: %v", err)
	}
}

func TestConvertJSONArgumentScalars(t *testing.T) {
	ts := time.Date(2026, 10, 26, 10, 0, 0, 500, time.UTC)
	for _, tc := range []struct {
		dataType uint32
		json     string
		want     any
	}{
		{id.Boolean, `true`, true},
		{id.SByte, `-128`, int8(-128)},
		{id.Byte, `255`, uint8(255)},
		{id.Int16, `-32768`, int16(-32768)},
		{id.UInt16, `65535`, uint16(65535)},
		{id.Int32, `2147483647`, int32(2147483647)},
		{id.Int32, `4e1`, int32(40)},
		{id.Int32, `2.000`, int32(2)},
		{id.Int32, `"-7"`, int32(-7)},
		{id.UInt32, `4294967295`, uint32(4294967295)},
		{id.Int64, `"9223372036854775807"`, int64(math.MaxInt64)},
		{id.Int64, `-9223372036854775808`, int64(math.MinInt64)},
		{id.UInt64, `18446744073709551615`, uint64(math.MaxUint64)},
		{id.Integer, `5`, int64(5)},
		{id.UInteger, `5`, uint64(5)},
		{id.Enumeration, `3`, int32(3)},
		{id.IntegerID, `3`, uint32(3)},
		{id.StatusCode, `2147483648`, ua.StatusCode(0x80000000)},
		{id.Float, `1.5`, float32(1.5)},
		{id.Float, `"-Infinity"`, float32(math.Inf(-1))},
		{id.Double, `1e-3`, 0.001},
		{id.Double, `"Infinity"`, math.Inf(1)},
		{id.Duration, `250`, float64(250)},
		{id.String, `"hello"`, "hello"},
		{id.LocaleID, `"en"`, "en"},
		{id.XMLElement, `"<a/>"`, ua.XMLElement("<a/>")},
		{id.DateTime, `"2026-10-26T10:00:00.0000005Z"`, ts},
		{id.UtcTime, `"2026-10-26T10:00:00.0000005Z"`, ts},
		{id.ByteString, `"AQID"`, []byte{1, 2, 3}},
		{id.Image, `""`, []byte{}},
		{id.GUID, `"72962B91-FA75-4AE6-8D28-B404DC7DAF63"`, ua.NewGUID("72962B91-FA75-4AE6-8D28-B404DC7DAF63")},
		{id.NodeID, `"ns=1;s=X"`, ua.NewStringNodeID(1, "X")},
		{id.ExpandedNodeID, `"i=85"`, ua.NewNumericExpandedNodeID(0, 85)},
		{id.QualifiedName, `"2:Name"`, &ua.QualifiedName{NamespaceIndex: 2, Name: "Name"}},
		{id.QualifiedName, `"Name"`, &ua.QualifiedName{Name: "Name"}},
		{id.QualifiedName, `"x:y"`, &ua.QualifiedName{Name: "x:y"}},
		{id.LocalizedText, `"hi"`, &ua.LocalizedText{Text: "hi"}},
		{id.LocalizedText, `{"Locale": "en", "TEXT": "hi"}`, ua.NewLocalizedTextWithLocale("hi", "en")},
		{id.Int32, `null`, nil},
	} {
		got, err := JSONArguments([]byte("["+tc.json+"]"), []*ua.Argument{argOf(tc.dataType, -1)})
		if err != nil {
			t.Errorf("%d %s: %v", tc.dataType, tc.json, err)
			continue
		}
		if !sameValue(got[0], tc.want) {
			t.Errorf("%d %s = %#v, want %#v", tc.dataType, tc.json, got[0], tc.want)
		}
	}
	got, _ := JSONArguments([]byte(`["NaN"]`), []*ua.Argument{argOf(id.Double, -1)})
	if f, ok := got[0].(float64); !ok || !math.IsNaN(f) {
		t.Errorf("NaN = %#v", got[0])
	}
}

func TestConvertJSONArgumentErrors(t *testing.T) {
	for _, tc := range []struct {
		dataType uint32
		rank     int32
		json     string
		want     string
	}{
		{id.Boolean, -1, `1`, "expected a Boolean, got the number 1"},
		{id.Boolean, -1, `"true"`, "expected a Boolean, got a string"},
		{id.SByte, -1, `128`, "128 is out of range for SByte"},
		{id.Byte, -1, `-1`, "-1 is out of range for Byte"},
		{id.Byte, -1, `256`, "256 is out of range for Byte"},
		{id.UInt64, -1, `-99999999999999999999`, "out of range for UInt64"},
		{id.UInt64, -1, `18446744073709551616`, "out of range for UInt64"},
		{id.Int16, -1, `1.5`, "1.5 is not an integer (Int16)"},
		{id.Int32, -1, `"1.x"`, `"1.x" is not a number`},
		{id.Int32, -1, `"abc"`, `"abc" is not a valid Int32`},
		{id.UInt32, -1, `"abc"`, `"abc" is not a valid UInt32`},
		{id.Int32, -1, `{}`, "expected an Int32, got an object"},
		{id.Int32, -1, `[1]`, "expected a scalar Int32, got an array"},
		{id.Float, -1, `1e39`, "1e39 is out of range for Float"},
		{id.Double, -1, `"x"`, `"x" is not a valid Double`},
		{id.String, -1, `1`, "expected a String, got the number 1"},
		{id.XMLElement, -1, `true`, "expected a XmlElement, got a boolean"},
		{id.DateTime, -1, `"yesterday"`, "invalid DateTime"},
		{id.ByteString, -1, `"!!"`, "invalid ByteString"},
		{id.GUID, -1, `"nope"`, "invalid Guid"},
		{id.NodeID, -1, `"ns=x;i=1"`, "invalid NodeId"},
		{id.ExpandedNodeID, -1, `"ns=x;i=1"`, "invalid ExpandedNodeId"},
		{id.QualifiedName, -1, `null`, ""},
		{id.QualifiedName, -1, `5`, "expected a QualifiedName, got the number 5"},
		{id.LocalizedText, -1, `{"text": 5}`, `LocalizedText field "text": expected a string, got the number 5`},
		{id.LocalizedText, -1, `{"lang": "en"}`, `LocalizedText has no field "lang"`},
		{id.LocalizedText, -1, `5`, "expected a LocalizedText, got the number 5"},
		{id.Int32, 1, `5`, "expected an array of Int32, got a scalar"},
		{id.Int32, 0, `5`, "expected an array of Int32, got a scalar"},
		{id.Int32, 1, `[[5]]`, "expected an array of Int32 with 1 dimension(s), got 2"},
		{id.Int32, -3, `[[5]]`, "expected a scalar Int32 or a one-dimensional array, got 2 dimensions"},
		{id.Int32, 1, `[1, null]`, "element 1: null in an array of Int32"},
		{id.Int32, 1, `[1, "x"]`, `element 1: "x" is not a valid Int32`},
		{id.Int32, 2, `[[1], 2]`, "element 1: expected an array of Int32, got the number 2"},
		{id.Int32, -2, `[[1], [[2]]]`, "element 1: element 0: expected an Int32, got an array"},
	} {
		_, err := JSONArguments([]byte("["+tc.json+"]"), []*ua.Argument{argOf(tc.dataType, tc.rank)})
		if tc.want == "" {
			if err != nil {
				t.Errorf("%d %s: %v", tc.dataType, tc.json, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%d rank %d %s: error %v, want %q", tc.dataType, tc.rank, tc.json, err, tc.want)
		}
	}
}

func TestConvertJSONArgumentArrays(t *testing.T) {
	for _, tc := range []struct {
		dataType uint32
		rank     int32
		json     string
		want     any
	}{
		{id.Int32, 1, `[1, 2]`, []int32{1, 2}},
		{id.Int32, 1, `[]`, []int32{}},
		{id.Int32, 0, `[[1], [2]]`, [][]int32{{1}, {2}}},
		{id.Int32, -3, `7`, int32(7)},
		{id.Int32, -3, `[7]`, []int32{7}},
		{id.Int32, -2, `[[7]]`, [][]int32{{7}}},
		{id.String, 1, `["a", "b"]`, []string{"a", "b"}},
		{id.NodeID, 1, `["i=85"]`, []*ua.NodeID{ua.NewNumericNodeID(0, 85)}},
		{id.Boolean, 2, `[[true, false], [false, true]]`, [][]bool{{true, false}, {false, true}}},
		{id.ByteString, 1, `["AQ=="]`, [][]byte{{1}}},
	} {
		got, err := JSONArguments([]byte("["+tc.json+"]"), []*ua.Argument{argOf(tc.dataType, tc.rank)})
		if err != nil {
			t.Errorf("%d %s: %v", tc.dataType, tc.json, err)
			continue
		}
		if !sameValue(got[0], tc.want) {
			t.Errorf("%d %s = %#v, want %#v", tc.dataType, tc.json, got[0], tc.want)
		}
		if _, err := ua.NewVariant(got[0]); err != nil {
			t.Errorf("%d %s: variant: %v", tc.dataType, tc.json, err)
		}
	}
}

func TestArgumentType(t *testing.T) {
	for _, tc := range []struct {
		dataType *ua.NodeID
		ok       bool
	}{
		{nil, false},
		{ua.NewNumericNodeID(0, id.BaseDataType), false},
		{ua.NewNumericNodeID(0, id.Number), false},
		{ua.NewNumericNodeID(0, id.Range), false},
		{ua.NewNumericNodeID(1, id.Int32), false},
		{ua.NewStringNodeID(0, "Int32"), false},
		{ua.NewNumericNodeID(0, id.Int32), true},
	} {
		if _, ok := ArgumentType(tc.dataType); ok != tc.ok {
			t.Errorf("ArgumentType(%v) = %v", tc.dataType, ok)
		}
	}
	// A structure argument keeps the plain JSON mapping.
	got, err := JSONArguments([]byte(`[{"Low": 1}]`), []*ua.Argument{argOf(id.Range, -1)})
	if err != nil || !reflect.DeepEqual(got[0], map[string]any{"Low": float64(1)}) {
		t.Errorf("structure: %#v, %v", got, err)
	}
}

func TestTypeName(t *testing.T) {
	for typ, want := range map[ua.TypeID]string{
		ua.TypeIDInt32: "Int32", ua.TypeIDUint16: "UInt16", ua.TypeIDUint32: "UInt32", ua.TypeIDUint64: "UInt64",
		ua.TypeIDGUID: "Guid", ua.TypeIDNodeID: "NodeId", ua.TypeIDExpandedNodeID: "ExpandedNodeId",
		ua.TypeIDXMLElement: "XmlElement", ua.TypeIDLocalizedText: "LocalizedText",
	} {
		if got := typeName(typ); got != want {
			t.Errorf("typeName(%d) = %q, want %q", typ, got, want)
		}
	}
	if got := jsonKind(3.5); got != "float64" {
		t.Errorf("jsonKind = %q", got)
	}
	if got := plainJSON(json.Number("1x")); got != "1x" {
		t.Errorf("plainJSON = %#v", got)
	}
}

// sameValue compares NodeIds by their text: the parser and the
// constructors pick different encodings for the same NodeId.
func sameValue(got, want any) bool {
	switch w := want.(type) {
	case *ua.NodeID, *ua.ExpandedNodeID, []*ua.NodeID:
		return fmt.Sprint(got) == fmt.Sprint(w) && reflect.TypeOf(got) == reflect.TypeOf(w)
	}
	return reflect.DeepEqual(got, want)
}
