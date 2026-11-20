// SPDX-License-Identifier: MIT

package opcuactl

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/opcuactl/internal/model"
	"github.com/otfabric/opcuactl/internal/serve"
)

// TestE2E_WriteAttr writes the DisplayName and Description of Demo.Level,
// and is refused for attributes its WriteMask does not allow.
func TestE2E_WriteAttr(t *testing.T) {
	s := startServe(t)
	ep := s.url(t, "http://opcfoundation.org/UA-Profile/Transport/uatcp-uasc-uabinary")
	level := s.node(serve.DemoLevel)

	out := mustRun(t, "write", "attr", "-e", ep, "-n", level, "--attr", "DisplayName", "--value", "en:Tank level", "--yes")
	wantContains(t, out, "DisplayName", "Good")
	out = mustRun(t, "get", "attr", "-e", ep, "-n", level, "--attr", "DisplayName")
	wantContains(t, out, "Tank level")

	out = mustRun(t, "write", "attr", "-e", ep, "-n", level, "--attr", "5", "--value", "Level of tank 1", "--format", "json")
	var res model.WriteResult
	if err := json.Unmarshal([]byte(out), &res); err != nil || res.Attribute != "Description" || res.Status != "Good" {
		t.Errorf("write Description: %v %+v", err, res)
	}
	wantContains(t, mustRun(t, "get", "attr", "-e", ep, "-n", level, "--attr", "Description"), "Level of tank 1")

	out = mustRun(t, "write", "attr", "-e", ep, "-n", level, "--attr", "description", "--value", `{"UaType":21,"Value":{"Text":"via ua-json"}}`, "--type", "ua-json")
	wantContains(t, out, "Good")

	// Dry run: the current value and the conversion.
	out = mustRun(t, "write", "attr", "-e", ep, "-n", level, "--attr", "AccessLevel", "--value", "0x01", "--dry-run", "--format", "json")
	var dry model.DryRunResult
	if err := json.Unmarshal([]byte(out), &dry); err != nil || dry.Attribute != "AccessLevel" || dry.NewType != "byte" ||
		dry.Status != "DryRun" || dry.CurrentValue == nil {
		t.Errorf("dry run: %v %+v", err, dry)
	}
	wantContains(t, mustRun(t, "write", "attr", "-e", ep, "-n", level, "--attr", "BrowseName", "--value", "1:X", "--dry-run"), "DryRun", "qualifiedname")

	// The WriteMask of Level allows neither.
	for _, a := range [][]string{{"BrowseName", "1:Other"}, {"Historizing", "true"}} {
		_, _, err := runCLI(t, "write", "attr", "-e", ep, "-n", level, "--attr", a[0], "--value", a[1])
		if err == nil || ExitCode(err) != exitProtocol || !strings.Contains(err.Error(), "BadNotWritable") {
			t.Errorf("write %s: exit %d %v, want %d BadNotWritable", a[0], ExitCode(err), err, exitProtocol)
		}
	}
	if _, _, err := runCLI(t, "write", "attr", "-e", deadEndpoint, "-n", level, "--attr", "DisplayName", "--value", "x"); ExitCode(err) != exitTransport {
		t.Errorf("dead endpoint: exit %d %v", ExitCode(err), err)
	}
}

// TestWriteAttrFlags checks the flags before a connection.
func TestWriteAttrFlags(t *testing.T) {
	base := []string{"write", "attr", "-e", deadEndpoint}
	for _, c := range []struct {
		name string
		args []string
		want string
	}{
		{"no node", []string{"--attr", "DisplayName", "--value", "x"}, "--node"},
		{"no attr", []string{"-n", "i=1", "--value", "x"}, "--attr"},
		{"no value", []string{"-n", "i=1", "--attr", "DisplayName"}, "--value"},
		{"bad node", []string{"-n", "!", "--attr", "DisplayName", "--value", "x"}, "node"},
		{"bad attr", []string{"-n", "i=1", "--attr", "Colour", "--value", "x"}, "unknown attribute"},
		{"value attr", []string{"-n", "i=1", "--attr", "Value", "--value", "x"}, "write value"},
		{"structure", []string{"-n", "i=1", "--attr", "RolePermissions", "--value", "x"}, "ua-json"},
		{"bad bool", []string{"-n", "i=1", "--attr", "Historizing", "--value", "maybe"}, "Historizing"},
		{"bad type", []string{"-n", "i=1", "--attr", "DisplayName", "--value", "x", "--type", "colour"}, "unknown --type"},
		{"bad ua-json", []string{"-n", "i=1", "--attr", "DisplayName", "--value", "{", "--type", "ua-json"}, "DisplayName"},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, _, err := runCLI(t, append(base, c.args...)...)
			if ExitCode(err) != exitUsage || err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("exit %d %v, want %d with %q", ExitCode(err), err, exitUsage, c.want)
			}
		})
	}
}

// TestAttributeValue converts each default type of the attributes.
func TestAttributeValue(t *testing.T) {
	for a := ua.AttributeIDNodeID; a <= ua.AttributeIDAccessLevelEx; a++ {
		typ := attributeType(a)
		switch a {
		case ua.AttributeIDValue, ua.AttributeIDDataTypeDefinition, ua.AttributeIDRolePermissions, ua.AttributeIDUserRolePermissions:
			if typ != "" {
				t.Errorf("%v: type %q, want none", a, typ)
			}
			continue
		}
		if typ == "" {
			t.Errorf("%v: no type", a)
		}
	}
	for _, c := range []struct {
		value, typ string
		want       any
	}{
		{"true", "bool", true},
		{"0x0f", "byte", byte(15)},
		{"7", "uint16", uint16(7)},
		{"-1", "int32", int32(-1)},
		{"0x10", "uint32", uint32(16)},
		{"1.5", "double", 1.5},
		{"s", "string", "s"},
		{"2,3", "uint32-array", []uint32{2, 3}},
		{"", "uint32-array", []uint32{}},
	} {
		got, err := attributeValue(c.value, c.typ)
		if err != nil || format2(got) != format2(c.want) {
			t.Errorf("%s %s: %v %v, want %v", c.typ, c.value, got, err, c.want)
		}
	}
	if v, _ := attributeValue("de:Behälter", "localizedtext"); v.(*ua.LocalizedText).Locale != "de" {
		t.Errorf("localized text: %+v", v)
	}
	if v, _ := attributeValue("a:b:c", "localizedtext"); v.(*ua.LocalizedText).Text != "a:b:c" {
		t.Errorf("text with colons: %+v", v)
	}
	if v, _ := attributeValue("Name", "qualifiedname"); v.(*ua.QualifiedName).Name != "Name" {
		t.Errorf("qualified name: %+v", v)
	}
	if v, _ := attributeValue("3:Name", "qualifiedname"); v.(*ua.QualifiedName).NamespaceIndex != 3 {
		t.Errorf("qualified name with ns: %+v", v)
	}
	if v, err := attributeValue("i=11", "nodeid"); err != nil || v.(*ua.NodeID).IntID() != 11 {
		t.Errorf("nodeid: %v %v", v, err)
	}
	if v, err := attributeValue(`<Variant xmlns="http://opcfoundation.org/UA/2008/02/Types.xsd"><Value><Boolean>true</Boolean></Value></Variant>`, "ua-xml"); err != nil || v != true {
		t.Errorf("ua-xml: %v %v", v, err)
	}
	for _, bad := range [][2]string{{"x:Name", "qualifiedname"}, {"1,x", "uint32-array"}, {"256", "byte"}} {
		if _, err := attributeValue(bad[0], bad[1]); err == nil {
			t.Errorf("%s %s: no error", bad[1], bad[0])
		}
	}
}

func format2(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
