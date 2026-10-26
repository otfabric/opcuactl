// SPDX-License-Identifier: MIT

package parse

import (
	"reflect"
	"strings"
	"testing"

	"github.com/otfabric/go-opcua/ua"
)

func TestNodeID(t *testing.T) {
	tests := []struct {
		input   string
		wantStr string
		wantErr bool
	}{
		{"i=2258", "i=2258", false},
		{"ns=2;i=1001", "ns=2;i=1001", false},
		{"ns=2;s=Temperature", "ns=2;s=Temperature", false},
		{"", "", true},
		{"invalid", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			id, err := NodeID(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("NodeID(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if err == nil && id.String() != tt.wantStr {
				t.Errorf("NodeID(%q) = %q, want %q", tt.input, id.String(), tt.wantStr)
			}
		})
	}
}

func TestNodeIDs(t *testing.T) {
	ids, err := NodeIDs([]string{"i=2258", "ns=2;i=1001"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 {
		t.Fatalf("got %d IDs, want 2", len(ids))
	}

	_, err = NodeIDs([]string{"i=2258", "invalid"})
	if err == nil {
		t.Fatal("expected error for invalid ID")
	}
}

func TestSecurityPolicy(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", "http://opcfoundation.org/UA/SecurityPolicy#None"},
		{"none", "http://opcfoundation.org/UA/SecurityPolicy#None"},
		{"None", "http://opcfoundation.org/UA/SecurityPolicy#None"},
		{"Basic256Sha256", "http://opcfoundation.org/UA/SecurityPolicy#Basic256Sha256"},
		{"http://opcfoundation.org/UA/SecurityPolicy#Basic256", "http://opcfoundation.org/UA/SecurityPolicy#Basic256"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := SecurityPolicy(tt.input)
			if got != tt.want {
				t.Errorf("SecurityPolicy(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSecurityPolicyShortName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", "None"},
		{"http://opcfoundation.org/UA/SecurityPolicy#Basic256Sha256", "Basic256Sha256"},
		{"http://opcfoundation.org/UA/SecurityPolicy#None", "None"},
		{"https://example.com/policy/only/path", "https://example.com/policy/only/path"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := SecurityPolicyShortName(tt.input)
			if got != tt.want {
				t.Errorf("SecurityPolicyShortName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestCoerceValue(t *testing.T) {
	tests := []struct {
		value    string
		typeName string
		wantErr  bool
		check    func(interface{}) bool
	}{
		{"true", "bool", false, func(v interface{}) bool { return v == true }},
		{"false", "boolean", false, func(v interface{}) bool { return v == false }},
		{"42", "int16", false, func(v interface{}) bool { return v == int16(42) }},
		{"65535", "uint16", false, func(v interface{}) bool { return v == uint16(65535) }},
		{"100", "int32", false, func(v interface{}) bool { return v == int32(100) }},
		{"100", "uint32", false, func(v interface{}) bool { return v == uint32(100) }},
		{"100", "int64", false, func(v interface{}) bool { return v == int64(100) }},
		{"100", "uint64", false, func(v interface{}) bool { return v == uint64(100) }},
		{"3.14", "float", false, func(v interface{}) bool { return v == float32(3.14) }},
		{"3.14", "double", false, func(v interface{}) bool { return v == 3.14 }},
		{"hello", "string", false, func(v interface{}) bool { return v == "hello" }},
		{"aGVsbG8=", "bytes", false, func(v interface{}) bool {
			b, ok := v.([]byte)
			return ok && string(b) == "hello"
		}},
		{"2026-01-01T00:00:00Z", "datetime", false, func(v interface{}) bool { return v != nil }},
		{"12345678-1234-1234-1234-123456789012", "guid", false, func(v interface{}) bool {
			return v == "12345678-1234-1234-1234-123456789012"
		}},
		{"short", "guid", true, nil},
		{"i=2258", "nodeid", false, func(v interface{}) bool {
			nid, ok := v.(*ua.NodeID)
			return ok && nid.String() == "i=2258"
		}},
		{`{"key":"val"}`, "json", false, func(v interface{}) bool { return v == `{"key":"val"}` }},
		{"fallback", "unknown", false, func(v interface{}) bool { return v == "fallback" }},
		{"notabool", "bool", true, nil},
		{"notanumber", "int32", true, nil},
	}
	for _, tt := range tests {
		t.Run(tt.typeName+"/"+tt.value, func(t *testing.T) {
			got, err := CoerceValue(tt.value, tt.typeName)
			if (err != nil) != tt.wantErr {
				t.Errorf("CoerceValue(%q, %q) error = %v, wantErr %v", tt.value, tt.typeName, err, tt.wantErr)
				return
			}
			if err == nil && tt.check != nil && !tt.check(got) {
				t.Errorf("CoerceValue(%q, %q) = %v (%T), check failed", tt.value, tt.typeName, got, got)
			}
		})
	}
}

func TestNodeClassMask(t *testing.T) {
	tests := []struct {
		input string
		want  ua.NodeClass
	}{
		{"", ua.NodeClassAll},
		{"all", ua.NodeClassAll},
		{"ALL", ua.NodeClassAll},
		{"variable", ua.NodeClassVariable},
		{"object", ua.NodeClassObject},
		{"method", ua.NodeClassMethod},
		// The multi-word classes the completion offers: title-casing the
		// first letter alone made them unknown (0, every class).
		{"objecttype", ua.NodeClassObjectType},
		{"ObjectType", ua.NodeClassObjectType},
		{"variabletype", ua.NodeClassVariableType},
		{"referencetype", ua.NodeClassReferenceType},
		{"datatype", ua.NodeClassDataType},
		{"view", ua.NodeClassView},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := NodeClassMask(tt.input)
			if err != nil || got != tt.want {
				t.Errorf("NodeClassMask(%q) = %v, %v, want %v", tt.input, got, err, tt.want)
			}
		})
	}
	if _, err := NodeClassMask("bogus"); err == nil {
		t.Error("NodeClassMask(bogus): expected an error")
	}
}

func TestBrowseDirection(t *testing.T) {
	for in, want := range map[string]ua.BrowseDirection{
		"forward": ua.BrowseDirectionForward,
		"Inverse": ua.BrowseDirectionInverse,
		"inverse": ua.BrowseDirectionInverse,
		"both":    ua.BrowseDirectionBoth,
		"":        ua.BrowseDirectionBoth,
	} {
		if got, err := BrowseDirection(in); err != nil || got != want {
			t.Errorf("BrowseDirection(%q) = %v, %v, want %v", in, got, err, want)
		}
	}
	if _, err := BrowseDirection("sideways"); err == nil {
		t.Error("BrowseDirection(sideways): expected an error")
	}
}

func TestAttributeIDFromName(t *testing.T) {
	tests := []struct {
		input   string
		want    ua.AttributeID
		wantErr bool
	}{
		{"Value", ua.AttributeIDValue, false},
		{"value", ua.AttributeIDValue, false},
		{"13", ua.AttributeIDValue, false},
		{"BrowseName", ua.AttributeIDBrowseName, false},
		{"browse-name", ua.AttributeIDBrowseName, false},
		{"DisplayName", ua.AttributeIDDisplayName, false},
		{"display-name", ua.AttributeIDDisplayName, false},
		{"DataType", ua.AttributeIDDataType, false},
		{"data-type", ua.AttributeIDDataType, false},
		{"AccessLevel", ua.AttributeIDAccessLevel, false},
		{"WriteMask", ua.AttributeIDWriteMask, false},
		{"Historizing", ua.AttributeIDHistorizing, false},
		{"NodeClass", ua.AttributeIDNodeClass, false},
		{"node-id", ua.AttributeIDNodeID, false},
		{"Executable", ua.AttributeIDExecutable, false},
		{"user-executable", ua.AttributeIDUserExecutable, false},
		{"Symmetric", ua.AttributeIDSymmetric, false},
		{"27", ua.AttributeID(27), false},
		{"28", ua.AttributeIDInvalid, true},
		{"inverse-name", ua.AttributeIDInverseName, false},
		{"contains-no-loops", ua.AttributeIDContainsNoLoops, false},
		{"event-notifier", ua.AttributeIDEventNotifier, false},
		{"value-rank", ua.AttributeIDValueRank, false},
		{"array-dimensions", ua.AttributeIDArrayDimensions, false},
		{"minimum-sampling-interval", ua.AttributeIDMinimumSamplingInterval, false},
		{"unknown-attr", ua.AttributeIDInvalid, true},
		{"999", ua.AttributeIDInvalid, true},
		{"0", ua.AttributeIDInvalid, true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := AttributeIDFromName(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("AttributeIDFromName(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("AttributeIDFromName(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestNodeIDWithNamespace(t *testing.T) {
	tests := []struct {
		input     string
		namespace string
		wantStr   string
		wantErr   bool
	}{
		{"i=2258", "", "i=2258", false},
		{"i=2258", "2", "ns=2;i=2258", false},
		{"s=Temperature", "2", "ns=2;s=Temperature", false},
		{"ns=3;s=Foo", "2", "ns=3;s=Foo", false}, // explicit ns= not overridden
		{"", "2", "", true},
		{"CurrentTime", "", "i=2258", false},
		{"not-a-valid-node-id-or-name", "2", "", true},
		// Parses but canonical form differs from input; not a symbolic standard name.
		{"ns=2;i=001", "", "", true},
		// ParseNodeID fails outright.
		{"###not-a-node-id###", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.input+"/ns="+tt.namespace, func(t *testing.T) {
			id, err := NodeIDWithNamespace(tt.input, tt.namespace)
			if (err != nil) != tt.wantErr {
				t.Errorf("NodeIDWithNamespace(%q, %q) error = %v, wantErr %v", tt.input, tt.namespace, err, tt.wantErr)
				return
			}
			if err == nil && id.String() != tt.wantStr {
				t.Errorf("NodeIDWithNamespace(%q, %q) = %q, want %q", tt.input, tt.namespace, id.String(), tt.wantStr)
			}
		})
	}
}

func TestDataChangeTrigger(t *testing.T) {
	for in, want := range map[string]ua.DataChangeTrigger{
		"":                       ua.DataChangeTriggerStatusValue,
		"status":                 ua.DataChangeTriggerStatus,
		"status-value":           ua.DataChangeTriggerStatusValue,
		"StatusValue":            ua.DataChangeTriggerStatusValue,
		"status-value-timestamp": ua.DataChangeTriggerStatusValueTimestamp,
		"status_value_timestamp": ua.DataChangeTriggerStatusValueTimestamp,
	} {
		got, err := DataChangeTrigger(in)
		if err != nil || got != want {
			t.Errorf("DataChangeTrigger(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	if _, err := DataChangeTrigger("sometimes"); err == nil {
		t.Error("expected an error for an unknown trigger")
	}
}

func TestDeadbandType(t *testing.T) {
	for in, want := range map[string]ua.DeadbandType{
		"": ua.DeadbandTypeNone, "none": ua.DeadbandTypeNone,
		"absolute": ua.DeadbandTypeAbsolute, "ABS": ua.DeadbandTypeAbsolute,
		"percent": ua.DeadbandTypePercent, "pct": ua.DeadbandTypePercent,
	} {
		got, err := DeadbandType(in)
		if err != nil || got != want {
			t.Errorf("DeadbandType(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	if _, err := DeadbandType("relative"); err == nil {
		t.Error("expected an error for an unknown deadband type")
	}
}

func TestUAEncoding(t *testing.T) {
	v := ua.MustVariant([]int32{1, 2})
	for name, want := range map[string]string{
		"json":               `{"UaType":6,"Value":[1,2]}`,
		"json-compact":       `{"UaType":6,"Value":[1,2]}`,
		"json-verbose":       `{"UaType":6,"Value":[1,2]}`,
		"json-reversible":    `{"Type":6,"Body":[1,2]}`,
		"json-nonreversible": `[1,2]`,
		"xml":                `<Variant xmlns="http://opcfoundation.org/UA/2008/02/Types.xsd"><Value><ListOfInt32><Int32>1</Int32><Int32>2</Int32></ListOfInt32></Value></Variant>`,
	} {
		enc, err := ParseUAEncoding(name)
		if err != nil {
			t.Fatalf("ParseUAEncoding(%q): %v", name, err)
		}
		got, err := enc.Encode(v, false)
		if err != nil || string(got) != want {
			t.Errorf("%s: got %s, %v; want %s", name, got, err, want)
		}
	}
	if _, err := ParseUAEncoding("bson"); err == nil {
		t.Error("expected an error for an unknown encoding")
	}

	enc, _ := ParseUAEncoding("json")
	pretty, err := enc.Encode(v, true)
	if err != nil || !strings.Contains(string(pretty), "\n") {
		t.Errorf("pretty output is not indented: %s, %v", pretty, err)
	}
}

func TestUAVariant(t *testing.T) {
	v, err := UAVariant(`{"UaType":6,"Value":[1,2]}`, "ua-json")
	if err != nil || !reflect.DeepEqual(v.Value(), []int32{1, 2}) {
		t.Errorf("ua-json: %v, %v", v, err)
	}
	v, err = UAVariant(`<Variant xmlns="http://opcfoundation.org/UA/2008/02/Types.xsd"><Value><Double>1.5</Double></Value></Variant>`, "UA-XML")
	if err != nil || v.Value() != 1.5 {
		t.Errorf("ua-xml: %v, %v", v, err)
	}
	if _, err := UAVariant(`{"UaType":`, "ua-json"); err == nil {
		t.Error("expected an error for malformed JSON")
	}
	if _, err := UAVariant("1", "int32"); err == nil {
		t.Error("expected an error for a type that is not an OPC UA encoding")
	}
	if !IsUAEncodedType("ua-json") || !IsUAEncodedType("UA-XML") || IsUAEncodedType("json") {
		t.Error("IsUAEncodedType misclassifies")
	}
}

func TestSecurityPolicy_ECC(t *testing.T) {
	for in, want := range map[string]string{
		"ECC_nistP256":        ua.SecurityPolicyURIECCNistP256,
		"ECC_curve25519":      ua.SecurityPolicyURIECCCurve25519,
		"Aes128Sha256RsaOaep": ua.SecurityPolicyURIAes128Sha256RsaOaep,
		"none":                ua.SecurityPolicyURINone,
	} {
		if got := SecurityPolicy(in); got != want {
			t.Errorf("SecurityPolicy(%q) = %q, want %q", in, got, want)
		}
	}
	if got := SecurityPolicyShortName(ua.SecurityPolicyURIECCNistP256AesGcm); got != "ECC_nistP256_AesGcm" {
		t.Errorf("short name = %q", got)
	}
}
