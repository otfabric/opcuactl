// SPDX-License-Identifier: MIT

package parse

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/otfabric/go-opcua/id"
	"github.com/otfabric/go-opcua/ua"
)

// ErrNotJSONArray is returned by JSONArguments for JSON that is not an array.
var ErrNotJSONArray = errors.New("not a JSON array")

// JSONArguments parses the method arguments of --arg-json / --arg-file: a
// JSON array with one element per input argument.
//
// When the InputArguments of the method are known (inputs is not empty),
// every element is converted to the DataType and ValueRank of its argument:
// a JSON number for an Int32 argument is sent as an Int32, an array for an
// argument of ValueRank 1 as a typed array. A number out of the range of an
// integer type, a fraction for an integer type or a value of the wrong JSON
// type is an error. Elements without a known argument, and arguments of a
// DataType that is not a built-in type (a structure, an enumeration of a
// companion specification), keep the plain JSON mapping: a number is a
// Double, a string a String, true/false a Boolean, null an empty Variant.
func JSONArguments(data []byte, inputs []*ua.Argument) ([]any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var raw any
	if err := dec.Decode(&raw); err != nil {
		return nil, err
	}
	if dec.More() {
		return nil, errors.New("trailing data after the JSON array")
	}
	list, ok := raw.([]any)
	if !ok {
		return nil, ErrNotJSONArray
	}
	out := make([]any, len(list))
	for i, v := range list {
		if i >= len(inputs) || inputs[i] == nil {
			out[i] = plainJSON(v)
			continue
		}
		c, err := ConvertJSONArgument(v, inputs[i])
		if err != nil {
			name := ""
			if inputs[i].Name != "" {
				name = " (" + inputs[i].Name + ")"
			}
			return nil, fmt.Errorf("argument %d%s: %w", i, name, err)
		}
		out[i] = c
	}
	return out, nil
}

// plainJSON is the value of v without a known DataType: numbers are
// Doubles, as encoding/json decodes them.
func plainJSON(v any) any {
	switch x := v.(type) {
	case json.Number:
		f, err := strconv.ParseFloat(x.String(), 64)
		if err != nil {
			return x.String()
		}
		return f
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = plainJSON(e)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = plainJSON(e)
		}
		return out
	}
	return v
}

// builtinType is the built-in type an argument of a DataType of namespace 0
// is sent as: the built-in types, their common subtypes and the abstract
// integer types.
var builtinType = map[uint32]ua.TypeID{
	id.Boolean:        ua.TypeIDBoolean,
	id.SByte:          ua.TypeIDSByte,
	id.Byte:           ua.TypeIDByte,
	id.Int16:          ua.TypeIDInt16,
	id.UInt16:         ua.TypeIDUint16,
	id.Int32:          ua.TypeIDInt32,
	id.UInt32:         ua.TypeIDUint32,
	id.Int64:          ua.TypeIDInt64,
	id.UInt64:         ua.TypeIDUint64,
	id.Float:          ua.TypeIDFloat,
	id.Double:         ua.TypeIDDouble,
	id.String:         ua.TypeIDString,
	id.DateTime:       ua.TypeIDDateTime,
	id.GUID:           ua.TypeIDGUID,
	id.ByteString:     ua.TypeIDByteString,
	id.XMLElement:     ua.TypeIDXMLElement,
	id.NodeID:         ua.TypeIDNodeID,
	id.ExpandedNodeID: ua.TypeIDExpandedNodeID,
	id.StatusCode:     ua.TypeIDStatusCode,
	id.QualifiedName:  ua.TypeIDQualifiedName,
	id.LocalizedText:  ua.TypeIDLocalizedText,
	// Subtypes of the built-in types.
	id.Duration:  ua.TypeIDDouble,
	id.UtcTime:   ua.TypeIDDateTime,
	id.LocaleID:  ua.TypeIDString,
	id.IntegerID: ua.TypeIDUint32,
	id.Counter:   ua.TypeIDUint32,
	id.Image:     ua.TypeIDByteString,
	id.ImageBMP:  ua.TypeIDByteString,
	id.ImageGIF:  ua.TypeIDByteString,
	id.ImageJPG:  ua.TypeIDByteString,
	id.ImagePNG:  ua.TypeIDByteString,
	// Abstract integer types: the widest of their subtypes. An
	// Enumeration is sent as its Int32 value.
	id.Integer:     ua.TypeIDInt64,
	id.UInteger:    ua.TypeIDUint64,
	id.Enumeration: ua.TypeIDInt32,
}

// goType is the Go type of the elements of an array of a built-in type.
var goType = map[ua.TypeID]reflect.Type{
	ua.TypeIDBoolean:        reflect.TypeOf(false),
	ua.TypeIDSByte:          reflect.TypeOf(int8(0)),
	ua.TypeIDByte:           reflect.TypeOf(uint8(0)),
	ua.TypeIDInt16:          reflect.TypeOf(int16(0)),
	ua.TypeIDUint16:         reflect.TypeOf(uint16(0)),
	ua.TypeIDInt32:          reflect.TypeOf(int32(0)),
	ua.TypeIDUint32:         reflect.TypeOf(uint32(0)),
	ua.TypeIDInt64:          reflect.TypeOf(int64(0)),
	ua.TypeIDUint64:         reflect.TypeOf(uint64(0)),
	ua.TypeIDFloat:          reflect.TypeOf(float32(0)),
	ua.TypeIDDouble:         reflect.TypeOf(float64(0)),
	ua.TypeIDString:         reflect.TypeOf(""),
	ua.TypeIDDateTime:       reflect.TypeOf(time.Time{}),
	ua.TypeIDGUID:           reflect.TypeOf((*ua.GUID)(nil)),
	ua.TypeIDByteString:     reflect.TypeOf([]byte(nil)),
	ua.TypeIDXMLElement:     reflect.TypeOf(ua.XMLElement("")),
	ua.TypeIDNodeID:         reflect.TypeOf((*ua.NodeID)(nil)),
	ua.TypeIDExpandedNodeID: reflect.TypeOf((*ua.ExpandedNodeID)(nil)),
	ua.TypeIDStatusCode:     reflect.TypeOf(ua.StatusCode(0)),
	ua.TypeIDQualifiedName:  reflect.TypeOf((*ua.QualifiedName)(nil)),
	ua.TypeIDLocalizedText:  reflect.TypeOf((*ua.LocalizedText)(nil)),
}

// ArgumentType is the built-in type of the DataType of a method argument,
// and false for a DataType that is not a built-in type of namespace 0 (or
// is BaseDataType or Number, which take any value).
func ArgumentType(dataType *ua.NodeID) (ua.TypeID, bool) {
	if dataType == nil || dataType.Namespace() != 0 || dataType.Type() != ua.NodeIDTypeNumeric {
		return 0, false
	}
	t, ok := builtinType[dataType.IntID()]
	return t, ok
}

// ConvertJSONArgument converts v, a value decoded from JSON with
// json.Decoder.UseNumber, to the DataType and ValueRank of arg (see
// JSONArguments).
func ConvertJSONArgument(v any, arg *ua.Argument) (any, error) {
	t, ok := ArgumentType(arg.DataType)
	if !ok {
		return plainJSON(v), nil
	}
	if v == nil {
		return nil, nil
	}
	depth := arrayDepth(v)
	switch rank := arg.ValueRank; {
	case rank == valueRankScalar:
		if depth > 0 {
			return nil, fmt.Errorf("expected a scalar %s, got an array", typeName(t))
		}
	case rank == valueRankScalarOrOneDimension:
		if depth > 1 {
			return nil, fmt.Errorf("expected a scalar %s or a one-dimensional array, got %d dimensions", typeName(t), depth)
		}
	case rank == valueRankOneOrMoreDimensions:
		if depth == 0 {
			return nil, fmt.Errorf("expected an array of %s, got a scalar", typeName(t))
		}
	case rank > 0:
		if depth == 0 {
			return nil, fmt.Errorf("expected an array of %s, got a scalar", typeName(t))
		}
		if depth != int(rank) {
			return nil, fmt.Errorf("expected an array of %s with %d dimension(s), got %d", typeName(t), rank, depth)
		}
	}
	return convertArray(v, t, depth)
}

// arrayDepth is the number of nested arrays at the start of v: 0 for a
// scalar, 1 for [1, 2], 2 for [[1, 2], [3, 4]].
func arrayDepth(v any) int {
	n := 0
	for {
		a, ok := v.([]any)
		if !ok {
			return n
		}
		n++
		if len(a) == 0 {
			return n
		}
		v = a[0]
	}
}

func convertArray(v any, t ua.TypeID, depth int) (any, error) {
	if depth == 0 {
		return convertScalar(v, t)
	}
	list, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("expected an array of %s, got %s", typeName(t), jsonKind(v))
	}
	st := goType[t]
	for i := 0; i < depth; i++ {
		st = reflect.SliceOf(st)
	}
	out := reflect.MakeSlice(st, len(list), len(list))
	for i, e := range list {
		if e == nil {
			return nil, fmt.Errorf("element %d: null in an array of %s", i, typeName(t))
		}
		c, err := convertArray(e, t, depth-1)
		if err != nil {
			return nil, fmt.Errorf("element %d: %w", i, err)
		}
		out.Index(i).Set(reflect.ValueOf(c))
	}
	return out.Interface(), nil
}

func convertScalar(v any, t ua.TypeID) (any, error) {
	if _, isArray := v.([]any); isArray {
		return nil, fmt.Errorf("expected %s, got an array", withArticle(typeName(t)))
	}
	switch t {
	case ua.TypeIDBoolean:
		if b, ok := v.(bool); ok {
			return b, nil
		}
	case ua.TypeIDSByte, ua.TypeIDInt16, ua.TypeIDInt32, ua.TypeIDInt64:
		if s, ok := numberText(v); ok {
			return convertInt(s, t)
		}
	case ua.TypeIDByte, ua.TypeIDUint16, ua.TypeIDUint32, ua.TypeIDUint64, ua.TypeIDStatusCode:
		if s, ok := numberText(v); ok {
			return convertUint(s, t)
		}
	case ua.TypeIDFloat, ua.TypeIDDouble:
		if s, ok := numberText(v); ok {
			return convertFloat(s, t)
		}
	case ua.TypeIDString:
		if s, ok := v.(string); ok {
			return s, nil
		}
	case ua.TypeIDXMLElement:
		if s, ok := v.(string); ok {
			return ua.XMLElement(s), nil
		}
	case ua.TypeIDDateTime:
		if s, ok := v.(string); ok {
			ts, err := time.Parse(time.RFC3339Nano, s)
			if err != nil {
				return nil, fmt.Errorf("invalid DateTime %q: expected RFC 3339", s)
			}
			return ts, nil
		}
	case ua.TypeIDByteString:
		if s, ok := v.(string); ok {
			b, err := base64.StdEncoding.DecodeString(s)
			if err != nil {
				return nil, fmt.Errorf("invalid ByteString %q: expected base64", s)
			}
			return b, nil
		}
	case ua.TypeIDGUID:
		if s, ok := v.(string); ok {
			g := ua.NewGUID(s)
			if g == nil {
				return nil, fmt.Errorf("invalid Guid %q", s)
			}
			return g, nil
		}
	case ua.TypeIDNodeID:
		if s, ok := v.(string); ok {
			n, err := ua.ParseNodeID(s)
			if err != nil {
				return nil, fmt.Errorf("invalid NodeId %q: %w", s, err)
			}
			return n, nil
		}
	case ua.TypeIDExpandedNodeID:
		if s, ok := v.(string); ok {
			n, err := ua.ParseExpandedNodeID(s, nil)
			if err != nil {
				return nil, fmt.Errorf("invalid ExpandedNodeId %q: %w", s, err)
			}
			return n, nil
		}
	case ua.TypeIDQualifiedName:
		if s, ok := v.(string); ok {
			return qualifiedName(s), nil
		}
	case ua.TypeIDLocalizedText:
		switch x := v.(type) {
		case string:
			return &ua.LocalizedText{Text: x}, nil
		case map[string]any:
			return localizedText(x)
		}
	}
	return nil, fmt.Errorf("expected %s, got %s", withArticle(typeName(t)), jsonKind(v))
}

// numberText is the text of a JSON number, or of a string holding a number
// (the OPC UA JSON encoding of Int64 and UInt64, and "NaN", "Infinity" and
// "-Infinity" of Float and Double).
func numberText(v any) (string, bool) {
	switch x := v.(type) {
	case json.Number:
		return x.String(), true
	case string:
		return strings.TrimSpace(x), true
	}
	return "", false
}

var intBits = map[ua.TypeID]int{
	ua.TypeIDSByte: 8, ua.TypeIDInt16: 16, ua.TypeIDInt32: 32, ua.TypeIDInt64: 64,
	ua.TypeIDByte: 8, ua.TypeIDUint16: 16, ua.TypeIDUint32: 32, ua.TypeIDUint64: 64, ua.TypeIDStatusCode: 32,
}

// integerText is s as an integer literal: "1e3" and "2.0" are 1000 and 2,
// "2.5" is not an integer.
func integerText(s string, t ua.TypeID) (string, error) {
	if !strings.ContainsAny(s, ".eE") {
		return s, nil
	}
	f, ok := new(big.Float).SetString(s)
	if !ok {
		return "", fmt.Errorf("%q is not a number", s)
	}
	i, acc := f.Int(nil)
	if acc != big.Exact {
		return "", fmt.Errorf("%s is not an integer (%s)", s, typeName(t))
	}
	return i.String(), nil
}

func convertInt(s string, t ua.TypeID) (any, error) {
	s, err := integerText(s, t)
	if err != nil {
		return nil, err
	}
	n, err := strconv.ParseInt(s, 10, intBits[t])
	if err != nil {
		return nil, numberError(s, t, err)
	}
	switch t {
	case ua.TypeIDSByte:
		return int8(n), nil
	case ua.TypeIDInt16:
		return int16(n), nil
	case ua.TypeIDInt32:
		return int32(n), nil
	}
	return n, nil
}

func convertUint(s string, t ua.TypeID) (any, error) {
	s, err := integerText(s, t)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(s, "-") {
		if _, err := strconv.ParseInt(s, 10, 64); err == nil || errors.Is(err, strconv.ErrRange) {
			return nil, fmt.Errorf("%s is out of range for %s", s, typeName(t))
		}
	}
	n, err := strconv.ParseUint(s, 10, intBits[t])
	if err != nil {
		return nil, numberError(s, t, err)
	}
	switch t {
	case ua.TypeIDByte:
		return uint8(n), nil
	case ua.TypeIDUint16:
		return uint16(n), nil
	case ua.TypeIDUint32:
		return uint32(n), nil
	case ua.TypeIDStatusCode:
		return ua.StatusCode(n), nil
	}
	return n, nil
}

func convertFloat(s string, t ua.TypeID) (any, error) {
	bits := 64
	if t == ua.TypeIDFloat {
		bits = 32
	}
	var f float64
	switch s {
	case "NaN":
		f = math.NaN()
	case "Infinity":
		f = math.Inf(1)
	case "-Infinity":
		f = math.Inf(-1)
	default:
		var err error
		if f, err = strconv.ParseFloat(s, bits); err != nil {
			return nil, numberError(s, t, err)
		}
	}
	if t == ua.TypeIDFloat {
		return float32(f), nil
	}
	return f, nil
}

func numberError(s string, t ua.TypeID, err error) error {
	if errors.Is(err, strconv.ErrRange) {
		return fmt.Errorf("%s is out of range for %s", s, typeName(t))
	}
	return fmt.Errorf("%q is not a valid %s", s, typeName(t))
}

// qualifiedName parses "ns:Name" or "Name" (namespace 0).
func qualifiedName(s string) *ua.QualifiedName {
	if i := strings.IndexByte(s, ':'); i > 0 {
		if ns, err := strconv.ParseUint(s[:i], 10, 16); err == nil {
			return &ua.QualifiedName{NamespaceIndex: uint16(ns), Name: s[i+1:]}
		}
	}
	return &ua.QualifiedName{Name: s}
}

// localizedText is {"Locale": "en", "Text": "hello"} (keys in any case).
func localizedText(m map[string]any) (*ua.LocalizedText, error) {
	lt := &ua.LocalizedText{}
	for k, v := range m {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("LocalizedText field %q: expected a string, got %s", k, jsonKind(v))
		}
		switch strings.ToLower(k) {
		case "locale":
			lt.Locale = s
		case "text":
			lt.Text = s
		default:
			return nil, fmt.Errorf("LocalizedText has no field %q (Locale, Text)", k)
		}
	}
	lt.UpdateMask()
	return lt, nil
}

// typeName is the OPC UA name of a built-in type (Int32, not TypeIDInt32).
func typeName(t ua.TypeID) string {
	switch t {
	case ua.TypeIDUint16:
		return "UInt16"
	case ua.TypeIDUint32:
		return "UInt32"
	case ua.TypeIDUint64:
		return "UInt64"
	case ua.TypeIDGUID:
		return "Guid"
	case ua.TypeIDNodeID:
		return "NodeId"
	case ua.TypeIDExpandedNodeID:
		return "ExpandedNodeId"
	case ua.TypeIDXMLElement:
		return "XmlElement"
	}
	return strings.TrimPrefix(t.String(), "TypeID")
}

func jsonKind(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case bool:
		return "a boolean"
	case json.Number:
		return "the number " + x.String()
	case string:
		return "a string"
	case []any:
		return "an array"
	case map[string]any:
		return "an object"
	}
	return fmt.Sprintf("%T", v)
}

// ValueRanks of Part 3 §5.6.2.
const (
	valueRankScalarOrOneDimension = -3
	valueRankScalar               = -1
	valueRankOneOrMoreDimensions  = 0
)

// withArticle is "an Int32", "a Double".
func withArticle(name string) string {
	if name != "" && strings.ContainsRune("AEIOU", rune(name[0])) {
		return "an " + name
	}
	return "a " + name
}
