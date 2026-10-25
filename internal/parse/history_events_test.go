// SPDX-License-Identifier: MIT

package parse

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-opcua/id"
	"github.com/otfabric/go-opcua/ua"
)

func TestEventFieldPath(t *testing.T) {
	for _, tt := range []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"Severity", "0:Severity", false},
		{"AckedState/Id", "0:AckedState/0:Id", false},
		{"2:Temperature", "2:Temperature", false},
		{"Tank/3:Level/Value", "0:Tank/3:Level/0:Value", false},
		{"x:Name", "0:x:Name", false},
		{"", "", true},
		{" ", "", true},
		{"AckedState//Id", "", true},
		{"2:", "", true},
	} {
		got, err := EventFieldPath(tt.in)
		if (err != nil) != tt.wantErr {
			t.Errorf("EventFieldPath(%q) error %v, want error %v", tt.in, err, tt.wantErr)
			continue
		}
		if err != nil {
			continue
		}
		parts := make([]string, len(got))
		for i, qn := range got {
			parts[i] = fmt.Sprintf("%d:%s", qn.NamespaceIndex, qn.Name)
		}
		if s := strings.Join(parts, "/"); s != tt.want {
			t.Errorf("EventFieldPath(%q) = %s, want %s", tt.in, s, tt.want)
		}
	}
}

func TestEventSelectClauses(t *testing.T) {
	ops, err := EventSelectClauses([]string{"EventId", ConditionIDField, "ActiveState/Id"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 3 || ops[0].AttributeID != ua.AttributeIDValue || ops[0].TypeDefinitionID.IntID() != id.BaseEventType ||
		ops[0].BrowsePath[0].Name != "EventId" {
		t.Errorf("EventId %+v", ops[0])
	}
	if ops[1].AttributeID != ua.AttributeIDNodeID || len(ops[1].BrowsePath) != 0 || ops[1].TypeDefinitionID.IntID() != id.ConditionType {
		t.Errorf("ConditionId %+v", ops[1])
	}
	if len(ops[2].BrowsePath) != 2 || ops[2].BrowsePath[1].Name != "Id" {
		t.Errorf("ActiveState/Id %+v", ops[2])
	}
	if _, err := EventSelectClauses([]string{"a//b"}); err == nil {
		t.Error("empty BrowseName accepted")
	}
}

func TestEvents(t *testing.T) {
	evs, err := Events([]byte(`[
		{"EventId":"AQID","EventType":"BaseEventType","SourceNode":"ns=1;s=Demo","SourceName":"Demo",
		 "Time":"2026-01-02T03:04:05.5Z","ReceiveTime":"2026-01-02T03:04:06Z","Severity":500,
		 "Message":{"Locale":"en","Text":"hello"},"Note":"text","Flag":true,"Value":1.5,"Missing":null,
		 "2:Count":{"UaType":7,"Value":42}},
		{"Message":"plain","EventId":"0x0a0b"}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 2 {
		t.Fatalf("%d events", len(evs))
	}
	e := evs[0]
	if names := strings.Join(e.Names(), ","); names != "2:Count,EventId,EventType,Flag,Message,Missing,Note,ReceiveTime,Severity,SourceName,SourceNode,Time,Value" {
		t.Errorf("names %s", names)
	}
	if b, _ := e.Field("EventId").Value().([]byte); !bytes.Equal(b, []byte{1, 2, 3}) {
		t.Errorf("EventId %v", e.Field("EventId"))
	}
	if n, _ := e.Field("EventType").Value().(*ua.NodeID); n == nil || n.IntID() != id.BaseEventType {
		t.Errorf("EventType %v", e.Field("EventType"))
	}
	if n, _ := e.Field("SourceNode").Value().(*ua.NodeID); n == nil || n.String() != "ns=1;s=Demo" {
		t.Errorf("SourceNode %v", e.Field("SourceNode"))
	}
	if tm, _ := e.Field("Time").Value().(time.Time); !tm.Equal(time.Date(2026, 1, 2, 3, 4, 5, 5e8, time.UTC)) {
		t.Errorf("Time %v", tm)
	}
	if v, _ := e.Field("Severity").Value().(uint16); v != 500 {
		t.Errorf("Severity %v", e.Field("Severity"))
	}
	if lt, _ := e.Field("Message").Value().(*ua.LocalizedText); lt == nil || lt.Text != "hello" || lt.Locale != "en" {
		t.Errorf("Message %v", e.Field("Message"))
	}
	if e.Field("Note").Value() != "text" || e.Field("Flag").Value() != true || e.Field("Value").Value() != 1.5 ||
		e.Field("Missing").Value() != nil || e.Field("2:Count").Value() != uint32(42) || e.Field("SourceName").Value() != "Demo" {
		t.Errorf("fields %+v", e)
	}
	if e.Field("Nope") != nil {
		t.Error("missing field found")
	}
	if lt, _ := evs[1].Field("Message").Value().(*ua.LocalizedText); lt == nil || lt.Text != "plain" {
		t.Errorf("plain Message %v", evs[1].Field("Message"))
	}

	one, err := Events([]byte(` {"Severity":1} `))
	if err != nil || len(one) != 1 {
		t.Fatalf("single object: %v %v", one, err)
	}
	one[0] = one[0].Set("Time", ua.MustVariant(time.Unix(0, 0)))
	one[0] = one[0].Set("EventId", ua.MustVariant([]byte{1}))
	one[0] = one[0].Set("Severity", ua.MustVariant(uint16(2)))
	if names := strings.Join(one[0].Names(), ","); names != "EventId,Severity,Time" || one[0].Field("Severity").Value() != uint16(2) {
		t.Errorf("Set: %s %v", names, one[0])
	}

	for _, bad := range []string{
		``, `[`, `{`, `[1]`, `{}`, `[{}]`, `{"a//b":1}`,
		`{"EventId":1}`, `{"EventId":"latest"}`, `{"EventId":"!!"}`,
		`{"EventType":5}`, `{"EventType":"ns=1;x="}`,
		`{"Time":1}`, `{"Time":"yesterday"}`,
		`{"Severity":-1}`, `{"Severity":1.5}`, `{"Severity":70000}`, `{"Severity":"high"}`,
		`{"Message":5}`, `{"SourceName":5}`,
		`{"Other":[1,2]}`, `{"Other":{"a":1}}`, `{"Other":{"UaType":"x"}}`, `{"Other":{"a":}}`,
	} {
		if _, err := Events([]byte(bad)); err == nil {
			t.Errorf("Events(%s) accepted", bad)
		}
	}
}
