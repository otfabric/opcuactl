package parse

import (
	"testing"

	"github.com/otfabric/opcua/ua"
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
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := NodeClassMask(tt.input)
			if got != tt.want {
				t.Errorf("NodeClassMask(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
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
