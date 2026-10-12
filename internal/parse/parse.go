// SPDX-License-Identifier: MIT

package parse

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/otfabric/go-opcua/ua"
)

// NodeID parses a string to a ua.NodeID using the standard OPC UA format.
// If namespace is non-empty and the string has no explicit ns= prefix, it prepends "ns=<namespace>;".
func NodeID(s string) (*ua.NodeID, error) {
	return NodeIDWithNamespace(s, "")
}

// NodeIDWithNamespace parses a string to a ua.NodeID, using the given default namespace
// when the string has no explicit "ns=" prefix.
// If the input doesn't match standard OPC UA syntax, it tries to resolve the string
// as a well-known standard node name (e.g. "CurrentTime" → i=2258).
func NodeIDWithNamespace(s, namespace string) (*ua.NodeID, error) {
	if s == "" {
		return nil, fmt.Errorf("invalid node ID: empty string")
	}
	raw := s
	// If a default namespace is set and the string doesn't already specify one
	if namespace != "" && !strings.HasPrefix(strings.ToLower(s), "ns=") {
		raw = "ns=" + namespace + ";" + s
	}
	id, err := ua.ParseNodeID(raw)
	if err == nil {
		// ParseNodeID may succeed for arbitrary strings; verify the round-trip.
		if rt := id.String(); rt == raw || strings.EqualFold(rt, raw) {
			return id, nil
		}
	}
	// Fallback: try as a well-known standard node name (namespace 0 only)
	if nid, ok := ua.StandardNodeID(s); ok {
		return nid, nil
	}
	if err != nil {
		return nil, fmt.Errorf("invalid node ID %q: %w", s, err)
	}
	return nil, fmt.Errorf("invalid node ID %q", s)
}

// NodeIDs parses multiple node ID strings.
func NodeIDs(ss []string) ([]*ua.NodeID, error) {
	return NodeIDsWithNamespace(ss, "")
}

// NodeIDsWithNamespace parses multiple node ID strings with a default namespace.
func NodeIDsWithNamespace(ss []string, namespace string) ([]*ua.NodeID, error) {
	ids := make([]*ua.NodeID, len(ss))
	for i, s := range ss {
		id, err := NodeIDWithNamespace(s, namespace)
		if err != nil {
			return nil, err
		}
		ids[i] = id
	}
	return ids, nil
}

// SecurityPolicy normalizes a security policy name to a full URI.
func SecurityPolicy(s string) string {
	if s == "" || strings.EqualFold(s, "none") {
		return ua.SecurityPolicyURINone
	}
	if strings.HasPrefix(s, "http") {
		return s
	}
	return ua.FormatSecurityPolicyURI(s)
}

// DataChangeTrigger parses the trigger of a data-change filter.
func DataChangeTrigger(s string) (ua.DataChangeTrigger, error) {
	switch strings.ToLower(strings.NewReplacer("-", "", "_", "").Replace(s)) {
	case "status":
		return ua.DataChangeTriggerStatus, nil
	case "", "statusvalue":
		return ua.DataChangeTriggerStatusValue, nil
	case "statusvaluetimestamp":
		return ua.DataChangeTriggerStatusValueTimestamp, nil
	}
	return 0, fmt.Errorf("unknown trigger %q (status|status-value|status-value-timestamp)", s)
}

// DeadbandType parses the deadband type of a data-change filter.
func DeadbandType(s string) (ua.DeadbandType, error) {
	switch strings.ToLower(s) {
	case "", "none":
		return ua.DeadbandTypeNone, nil
	case "absolute", "abs":
		return ua.DeadbandTypeAbsolute, nil
	case "percent", "pct":
		return ua.DeadbandTypePercent, nil
	}
	return 0, fmt.Errorf("unknown deadband type %q (none|absolute|percent)", s)
}

// UAEncoding describes one of the standard OPC UA data encodings of Part 6.
type UAEncoding struct {
	XML  bool
	Form ua.JSONForm
}

// ParseUAEncoding parses the name of an OPC UA data encoding.
func ParseUAEncoding(s string) (UAEncoding, error) {
	switch strings.ToLower(s) {
	case "json", "json-compact":
		return UAEncoding{Form: ua.JSONCompact}, nil
	case "json-verbose":
		return UAEncoding{Form: ua.JSONVerbose}, nil
	case "json-reversible":
		return UAEncoding{Form: ua.JSONLegacyReversible}, nil
	case "json-nonreversible":
		return UAEncoding{Form: ua.JSONLegacyNonReversible}, nil
	case "xml":
		return UAEncoding{XML: true}, nil
	}
	return UAEncoding{}, fmt.Errorf("unknown OPC UA encoding %q (json|json-verbose|json-reversible|json-nonreversible|xml)", s)
}

// Encode encodes a value (built-in type, structure or DataValue) in the
// OPC UA JSON or XML DataEncoding.
func (e UAEncoding) Encode(v interface{}, pretty bool) ([]byte, error) {
	var opts []ua.CodecOption
	if pretty {
		opts = append(opts, ua.WithIndent("  "))
	}
	if e.XML {
		return ua.EncodeXML(v, opts...)
	}
	return ua.EncodeJSON(v, append(opts, ua.WithJSONForm(e.Form))...)
}

// UAVariant decodes a Variant written in the OPC UA JSON ("ua-json") or XML
// ("ua-xml") DataEncoding. It gives access to every data type, including
// arrays, LocalizedText and structures.
func UAVariant(value, typeName string) (*ua.Variant, error) {
	v := new(ua.Variant)
	var err error
	switch strings.ToLower(typeName) {
	case "ua-json":
		err = ua.DecodeJSON([]byte(value), v)
	case "ua-xml":
		err = ua.DecodeXML([]byte(value), v)
	default:
		return nil, fmt.Errorf("%q is not an OPC UA encoding", typeName)
	}
	if err != nil {
		return nil, err
	}
	return v, nil
}

// IsUAEncodedType reports whether a --type value names an OPC UA encoding.
func IsUAEncodedType(typeName string) bool {
	t := strings.ToLower(typeName)
	return t == "ua-json" || t == "ua-xml"
}

// SecurityPolicyShortName extracts the short name from a policy URI.
func SecurityPolicyShortName(uri string) string {
	if uri == "" {
		return "None"
	}
	if idx := strings.LastIndex(uri, "#"); idx >= 0 {
		return uri[idx+1:]
	}
	return uri
}

// NodeClassMask converts a node class filter string to ua.NodeClass.
func NodeClassMask(s string) ua.NodeClass {
	if s == "" || strings.EqualFold(s, "all") {
		return ua.NodeClassAll
	}
	// SDK expects title-case (e.g. "Variable", "Object")
	titled := strings.ToUpper(s[:1]) + strings.ToLower(s[1:])
	return ua.NodeClassFromString(titled)
}

// AttributeIDFromName converts a friendly attribute name to a ua.AttributeID.
func AttributeIDFromName(s string) (ua.AttributeID, error) {
	switch strings.ToLower(s) {
	case "nodeid", "node-id", "1":
		return ua.AttributeIDNodeID, nil
	case "nodeclass", "node-class", "2":
		return ua.AttributeIDNodeClass, nil
	case "browsename", "browse-name", "3":
		return ua.AttributeIDBrowseName, nil
	case "displayname", "display-name", "4":
		return ua.AttributeIDDisplayName, nil
	case "description", "5":
		return ua.AttributeIDDescription, nil
	case "writemask", "write-mask", "6":
		return ua.AttributeIDWriteMask, nil
	case "userwritemask", "user-write-mask", "7":
		return ua.AttributeIDUserWriteMask, nil
	case "isabstract", "is-abstract", "8":
		return ua.AttributeIDIsAbstract, nil
	case "symmetric", "9":
		return ua.AttributeIDSymmetric, nil
	case "inversename", "inverse-name", "10":
		return ua.AttributeIDInverseName, nil
	case "containsnoloops", "contains-no-loops", "11":
		return ua.AttributeIDContainsNoLoops, nil
	case "eventnotifier", "event-notifier", "12":
		return ua.AttributeIDEventNotifier, nil
	case "value", "13":
		return ua.AttributeIDValue, nil
	case "datatype", "data-type", "14":
		return ua.AttributeIDDataType, nil
	case "valuerank", "value-rank", "15":
		return ua.AttributeIDValueRank, nil
	case "arraydimensions", "array-dimensions", "16":
		return ua.AttributeIDArrayDimensions, nil
	case "accesslevel", "access-level", "17":
		return ua.AttributeIDAccessLevel, nil
	case "useraccesslevel", "user-access-level", "18":
		return ua.AttributeIDUserAccessLevel, nil
	case "minimumsamplinginterval", "minimum-sampling-interval", "19":
		return ua.AttributeIDMinimumSamplingInterval, nil
	case "historizing", "20":
		return ua.AttributeIDHistorizing, nil
	case "executable", "21":
		return ua.AttributeIDExecutable, nil
	case "userexecutable", "user-executable", "22":
		return ua.AttributeIDUserExecutable, nil
	default:
		// Try parsing as a numeric ID
		v, err := strconv.ParseUint(s, 10, 32)
		if err != nil {
			return ua.AttributeIDInvalid, fmt.Errorf("unknown attribute %q", s)
		}
		id := ua.AttributeID(v)
		if id == ua.AttributeIDInvalid || id > 27 {
			return ua.AttributeIDInvalid, fmt.Errorf("invalid attribute ID %d", v)
		}
		return id, nil
	}
}

// CoerceValue converts a string value to a Go type based on the given OPC UA type name.
func CoerceValue(value, typeName string) (interface{}, error) {
	switch strings.ToLower(typeName) {
	case "bool", "boolean":
		return strconv.ParseBool(value)
	case "int16":
		v, err := strconv.ParseInt(value, 10, 16)
		return int16(v), err
	case "uint16":
		v, err := strconv.ParseUint(value, 10, 16)
		return uint16(v), err
	case "int32":
		v, err := strconv.ParseInt(value, 10, 32)
		return int32(v), err
	case "uint32":
		v, err := strconv.ParseUint(value, 10, 32)
		return uint32(v), err
	case "int64":
		v, err := strconv.ParseInt(value, 10, 64)
		return v, err
	case "uint64":
		v, err := strconv.ParseUint(value, 10, 64)
		return v, err
	case "float", "float32":
		v, err := strconv.ParseFloat(value, 32)
		return float32(v), err
	case "double", "float64":
		return strconv.ParseFloat(value, 64)
	case "string":
		return value, nil
	case "bytes", "bytestring":
		return base64.StdEncoding.DecodeString(value)
	case "datetime":
		return time.Parse(time.RFC3339, value)
	case "guid":
		if len(value) != 36 {
			return nil, fmt.Errorf("invalid GUID format %q: expected 36 characters", value)
		}
		return value, nil
	case "nodeid":
		nid, err := ua.ParseNodeID(value)
		if err != nil {
			return nil, fmt.Errorf("invalid NodeID %q: %w", value, err)
		}
		return nid, nil
	case "json":
		return value, nil
	default:
		return value, nil
	}
}
