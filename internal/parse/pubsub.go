// SPDX-License-Identifier: MIT

package parse

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/otfabric/go-opcua/pubsub"
	"github.com/otfabric/go-opcua/pubsub/message"
	"github.com/otfabric/go-opcua/ua"
)

// builtinTypes maps lower-case names of the scalar built-in types a PubSub
// field can have to their TypeID.
var builtinTypes = map[string]ua.TypeID{
	"bool":           ua.TypeIDBoolean,
	"boolean":        ua.TypeIDBoolean,
	"sbyte":          ua.TypeIDSByte,
	"byte":           ua.TypeIDByte,
	"int16":          ua.TypeIDInt16,
	"uint16":         ua.TypeIDUint16,
	"int32":          ua.TypeIDInt32,
	"uint32":         ua.TypeIDUint32,
	"int64":          ua.TypeIDInt64,
	"uint64":         ua.TypeIDUint64,
	"float":          ua.TypeIDFloat,
	"float32":        ua.TypeIDFloat,
	"double":         ua.TypeIDDouble,
	"float64":        ua.TypeIDDouble,
	"string":         ua.TypeIDString,
	"datetime":       ua.TypeIDDateTime,
	"guid":           ua.TypeIDGUID,
	"bytes":          ua.TypeIDByteString,
	"bytestring":     ua.TypeIDByteString,
	"nodeid":         ua.TypeIDNodeID,
	"statuscode":     ua.TypeIDStatusCode,
	"qualifiedname":  ua.TypeIDQualifiedName,
	"localizedtext":  ua.TypeIDLocalizedText,
	"variant":        ua.TypeIDVariant,
	"datavalue":      ua.TypeIDDataValue,
	"expandednodeid": ua.TypeIDExpandedNodeID,
}

// BuiltinType parses the name of an OPC UA built-in type (Int32, Double,
// String, ...), case-insensitively.
func BuiltinType(s string) (ua.TypeID, error) {
	if t, ok := builtinTypes[strings.ToLower(strings.TrimSpace(s))]; ok {
		return t, nil
	}
	return 0, fmt.Errorf("unknown built-in type %q", s)
}

// BuiltinTypeName returns the OPC UA name of a built-in type ("Int32").
func BuiltinTypeName(t ua.TypeID) string {
	switch t {
	case ua.TypeIDUint16:
		return "UInt16"
	case ua.TypeIDUint32:
		return "UInt32"
	case ua.TypeIDUint64:
		return "UInt64"
	case ua.TypeIDGUID:
		return "Guid"
	case ua.TypeIDXMLElement:
		return "XmlElement"
	}
	return strings.TrimPrefix(t.String(), "TypeID")
}

// PubSubField parses a DataSet field description "Name:Type" or
// "Name:Type[]" (an array) as used for DataSetMetaData on the command line.
func PubSubField(s string) (pubsub.Field, error) {
	name, typ, ok := strings.Cut(s, ":")
	name = strings.TrimSpace(name)
	if !ok || name == "" {
		return pubsub.Field{}, fmt.Errorf("invalid field %q: want Name:Type", s)
	}
	array := strings.HasSuffix(typ, "[]")
	t, err := BuiltinType(strings.TrimSuffix(typ, "[]"))
	if err != nil {
		return pubsub.Field{}, fmt.Errorf("invalid field %q: %w", s, err)
	}
	return pubsub.Field{Name: name, Type: t, Array: array}, nil
}

// PubSubFieldValue is a field to publish: its description and value.
type PubSubFieldValue struct {
	pubsub.Field
	Value interface{}
}

// PubSubFieldWithValue parses "Name:Type=Value", the field of a DataSet to
// publish. The value is converted as for `write value --type`.
func PubSubFieldWithValue(s string) (PubSubFieldValue, error) {
	spec, value, ok := strings.Cut(s, "=")
	if !ok {
		return PubSubFieldValue{}, fmt.Errorf("invalid field %q: want Name:Type=Value", s)
	}
	f, err := PubSubField(spec)
	if err != nil {
		return PubSubFieldValue{}, err
	}
	if f.Array {
		return PubSubFieldValue{}, fmt.Errorf("invalid field %q: arrays cannot be published from the command line", s)
	}
	_, typ, _ := strings.Cut(spec, ":")
	v, err := CoerceValue(value, typ)
	if err != nil {
		return PubSubFieldValue{}, fmt.Errorf("invalid value for field %q: %w", f.Name, err)
	}
	switch f.Type {
	case ua.TypeIDSByte:
		n, err := strconv.ParseInt(value, 10, 8)
		if err != nil {
			return PubSubFieldValue{}, fmt.Errorf("invalid value for field %q: %w", f.Name, err)
		}
		v = int8(n)
	case ua.TypeIDByte:
		n, err := strconv.ParseUint(value, 10, 8)
		if err != nil {
			return PubSubFieldValue{}, fmt.Errorf("invalid value for field %q: %w", f.Name, err)
		}
		v = uint8(n)
	case ua.TypeIDGUID:
		v = ua.NewGUID(value)
	case ua.TypeIDStatusCode:
		n, err := strconv.ParseUint(strings.TrimPrefix(strings.ToLower(value), "0x"), 16, 32)
		if err != nil {
			return PubSubFieldValue{}, fmt.Errorf("invalid value for field %q: want a hex status code: %w", f.Name, err)
		}
		v = ua.StatusCode(n)
	case ua.TypeIDQualifiedName:
		v = &ua.QualifiedName{Name: value}
	case ua.TypeIDLocalizedText:
		v = &ua.LocalizedText{Text: value, EncodingMask: ua.LocalizedTextText}
	case ua.TypeIDVariant, ua.TypeIDDataValue, ua.TypeIDExpandedNodeID:
		return PubSubFieldValue{}, fmt.Errorf("invalid field %q: type %s cannot be published from the command line", s, BuiltinTypeName(f.Type))
	}
	return PubSubFieldValue{Field: f, Value: v}, nil
}

// PublisherID parses a PublisherId. A plain number is a UInt16 (the usual
// choice of open62541 and go-opcua), a type prefix selects another width:
// "byte:1", "u16:7", "u32:7", "u64:7", "str:name". Anything that is not a
// number is a String PublisherId.
func PublisherID(s string) (message.PublisherID, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return message.PublisherID{}, fmt.Errorf("empty PublisherId")
	}
	prefix, rest, hasPrefix := strings.Cut(s, ":")
	if hasPrefix {
		var t message.PublisherIDType
		var bits int
		switch strings.ToLower(prefix) {
		case "byte", "u8":
			t, bits = message.PublisherIDByte, 8
		case "u16", "uint16":
			t, bits = message.PublisherIDUInt16, 16
		case "u32", "uint32":
			t, bits = message.PublisherIDUInt32, 32
		case "u64", "uint64":
			t, bits = message.PublisherIDUInt64, 64
		case "str", "string":
			return message.StringPublisherID(rest), nil
		default:
			// "urn:..." and other strings with a colon.
			return message.StringPublisherID(s), nil
		}
		n, err := strconv.ParseUint(rest, 10, bits)
		if err != nil {
			return message.PublisherID{}, fmt.Errorf("invalid PublisherId %q: %w", s, err)
		}
		return message.NewPublisherID(t, n), nil
	}
	if n, err := strconv.ParseUint(s, 10, 64); err == nil {
		if n > math.MaxUint16 {
			return message.PublisherID{}, fmt.Errorf("PublisherId %d does not fit a UInt16: use u32:%d or u64:%d", n, n, n)
		}
		return message.NewPublisherID(message.PublisherIDUInt16, n), nil
	}
	return message.StringPublisherID(s), nil
}

// PublisherIDValue returns the PublisherId as the Go value the pubsub
// builders take (uint8, uint16, uint32, uint64 or string).
func PublisherIDValue(p message.PublisherID) interface{} {
	switch p.Type {
	case message.PublisherIDByte:
		return uint8(p.Num)
	case message.PublisherIDUInt16:
		return uint16(p.Num)
	case message.PublisherIDUInt32:
		return uint32(p.Num)
	case message.PublisherIDUInt64:
		return p.Num
	default:
		return p.Str
	}
}

// MessageSecurity parses a PubSub message security mode
// (none|sign|sign-and-encrypt).
func MessageSecurity(s string) (ua.MessageSecurityMode, error) {
	switch strings.ToLower(strings.ReplaceAll(s, "_", "-")) {
	case "", "none":
		return ua.MessageSecurityModeNone, nil
	case "sign":
		return ua.MessageSecurityModeSign, nil
	case "sign-and-encrypt", "signandencrypt", "encrypt":
		return ua.MessageSecurityModeSignAndEncrypt, nil
	}
	return ua.MessageSecurityModeInvalid, fmt.Errorf("unknown message security %q (none|sign|sign-and-encrypt)", s)
}

// PubSubEncoding is a PubSub message mapping.
type PubSubEncoding string

// PubSub message mappings.
const (
	EncodingAuto PubSubEncoding = "auto"
	EncodingUADP PubSubEncoding = "uadp"
	EncodingJSON PubSubEncoding = "json"
)

// ParsePubSubEncoding parses a message mapping name (auto|uadp|json).
func ParsePubSubEncoding(s string, allowAuto bool) (PubSubEncoding, error) {
	switch e := PubSubEncoding(strings.ToLower(s)); e {
	case EncodingUADP, EncodingJSON:
		return e, nil
	case EncodingAuto, "":
		if allowAuto {
			return EncodingAuto, nil
		}
	}
	if allowAuto {
		return "", fmt.Errorf("unknown encoding %q (auto|uadp|json)", s)
	}
	return "", fmt.Errorf("unknown encoding %q (uadp|json)", s)
}

// FieldEncoding parses a DataSet field encoding (variant|datavalue|rawdata)
// into the DataSetFieldContentMask that selects it.
func FieldEncoding(s string) (ua.DataSetFieldContentMask, error) {
	switch strings.ToLower(s) {
	case "", "variant":
		return 0, nil
	case "datavalue", "data-value":
		return ua.DataSetFieldContentMaskStatusCode | ua.DataSetFieldContentMaskSourceTimestamp, nil
	case "rawdata", "raw-data", "raw":
		return ua.DataSetFieldContentMaskRawData, nil
	}
	return 0, fmt.Errorf("unknown field encoding %q (variant|datavalue|rawdata)", s)
}
