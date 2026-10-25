// SPDX-License-Identifier: MIT

package parse

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/otfabric/go-opcua/id"
	"github.com/otfabric/go-opcua/ua"
)

// ConditionIDField is the --select name of the ConditionId of a condition
// event: the SelectClause with an empty BrowsePath and AttributeId NodeId on
// ConditionType (Part 9 §5.5.2).
const ConditionIDField = "ConditionId"

// EventFieldPath parses an event field as a BrowsePath: BrowseNames joined
// with "/" ("AckedState/Id"), each in namespace 0 or written with its
// namespace index ("2:Temperature").
func EventFieldPath(s string) ([]*ua.QualifiedName, error) {
	if strings.TrimSpace(s) == "" {
		return nil, fmt.Errorf("empty event field")
	}
	parts := strings.Split(s, "/")
	out := make([]*ua.QualifiedName, len(parts))
	for i, p := range parts {
		qn := &ua.QualifiedName{Name: p}
		if ns, name, ok := strings.Cut(p, ":"); ok {
			n, err := strconv.ParseUint(ns, 10, 16)
			if err == nil {
				qn = &ua.QualifiedName{NamespaceIndex: uint16(n), Name: name}
			}
		}
		if qn.Name == "" {
			return nil, fmt.Errorf("event field %q has an empty BrowseName", s)
		}
		out[i] = qn
	}
	return out, nil
}

// EventSelectClauses returns the SelectClauses (Part 4 §7.7.4) of event
// fields: BrowsePaths on BaseEventType, ConditionId the NodeId of the
// condition.
func EventSelectClauses(fields []string) ([]*ua.SimpleAttributeOperand, error) {
	out := make([]*ua.SimpleAttributeOperand, len(fields))
	for i, f := range fields {
		if f == ConditionIDField {
			out[i] = &ua.SimpleAttributeOperand{
				TypeDefinitionID: ua.NewNumericNodeID(0, id.ConditionType),
				BrowsePath:       []*ua.QualifiedName{},
				AttributeID:      ua.AttributeIDNodeID,
			}
			continue
		}
		path, err := EventFieldPath(f)
		if err != nil {
			return nil, err
		}
		out[i] = &ua.SimpleAttributeOperand{
			TypeDefinitionID: ua.NewNumericNodeID(0, id.BaseEventType),
			BrowsePath:       path,
			AttributeID:      ua.AttributeIDValue,
		}
	}
	return out, nil
}

// EventField is a field of a historical event given on the command line.
type EventField struct {
	Name  string
	Value *ua.Variant
}

// Event is a historical event given on the command line: its fields
// ordered by name.
type Event []EventField

// Field returns the value of the named field, nil if the event has none.
func (e Event) Field(name string) *ua.Variant {
	for _, f := range e {
		if f.Name == name {
			return f.Value
		}
	}
	return nil
}

// Set sets the named field, keeping the fields ordered by name.
func (e Event) Set(name string, v *ua.Variant) Event {
	for i, f := range e {
		if f.Name == name {
			e[i].Value = v
			return e
		}
	}
	e = append(e, EventField{Name: name, Value: v})
	sort.Slice(e, func(i, j int) bool { return e[i].Name < e[j].Name })
	return e
}

// Names returns the field names of the event.
func (e Event) Names() []string {
	out := make([]string, len(e))
	for i, f := range e {
		out[i] = f.Name
	}
	return out
}

// Events parses historical events written in JSON: an object, or an array
// of objects, of field names (as --select takes them) and their values.
//
// The fields of BaseEventType take plain JSON: EventId base64 (or 0x hex),
// EventType and SourceNode a NodeId, Time and ReceiveTime RFC 3339,
// Severity a number (UInt16), Message a string or {"Locale","Text"},
// SourceName a string. Other fields take a string (String), a boolean
// (Boolean), a number (Double), null, or any Variant in the OPC UA JSON
// encoding ({"UaType":<built-in type id>,"Value":...}), which every field
// takes too.
func Events(data []byte) ([]Event, error) {
	data = bytes.TrimSpace(data)
	var objects []map[string]json.RawMessage
	if len(data) > 0 && data[0] == '[' {
		if err := json.Unmarshal(data, &objects); err != nil {
			return nil, fmt.Errorf("events: %w", err)
		}
	} else {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(data, &obj); err != nil {
			return nil, fmt.Errorf("event: %w", err)
		}
		objects = append(objects, obj)
	}
	out := make([]Event, 0, len(objects))
	for i, obj := range objects {
		if len(obj) == 0 {
			return nil, fmt.Errorf("event %d has no fields", i+1)
		}
		names := make([]string, 0, len(obj))
		for name := range obj {
			names = append(names, name)
		}
		sort.Strings(names)
		ev := make(Event, 0, len(names))
		for _, name := range names {
			if name != ConditionIDField {
				if _, err := EventFieldPath(name); err != nil {
					return nil, fmt.Errorf("event %d: %w", i+1, err)
				}
			}
			v, err := eventFieldValue(name, obj[name])
			if err != nil {
				return nil, fmt.Errorf("event %d: field %s: %w", i+1, name, err)
			}
			ev = append(ev, EventField{Name: name, Value: v})
		}
		out = append(out, ev)
	}
	return out, nil
}

// eventFieldValue decodes the JSON value of an event field.
func eventFieldValue(name string, raw json.RawMessage) (*ua.Variant, error) {
	raw = bytes.TrimSpace(raw)
	if bytes.Equal(raw, []byte("null")) {
		return ua.MustVariant(nil), nil
	}
	if len(raw) > 0 && raw[0] == '{' {
		var probe map[string]json.RawMessage
		if err := json.Unmarshal(raw, &probe); err != nil {
			return nil, err
		}
		if _, ok := probe["UaType"]; ok {
			return UAVariant(string(raw), "ua-json")
		}
	}
	var s string
	str := func() error {
		if err := json.Unmarshal(raw, &s); err != nil {
			return fmt.Errorf("want a string: %s", raw)
		}
		return nil
	}
	switch name {
	case "EventId":
		if err := str(); err != nil {
			return nil, err
		}
		b, latest, err := EventID(s)
		if err != nil || latest {
			return nil, fmt.Errorf("invalid EventId %q (base64 or 0x hex)", s)
		}
		return ua.MustVariant(b), nil
	case "EventType", "SourceNode":
		if err := str(); err != nil {
			return nil, err
		}
		n, err := NodeID(s)
		if err != nil {
			return nil, err
		}
		return ua.MustVariant(n), nil
	case "Time", "ReceiveTime":
		if err := str(); err != nil {
			return nil, err
		}
		t, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			return nil, fmt.Errorf("want an RFC 3339 time: %w", err)
		}
		return ua.MustVariant(t), nil
	case "Severity":
		var f float64
		if err := json.Unmarshal(raw, &f); err != nil || f != math.Trunc(f) || f < 0 || f > math.MaxUint16 {
			return nil, fmt.Errorf("want a number 0..65535: %s", raw)
		}
		return ua.MustVariant(uint16(f)), nil
	case "Message":
		var lt struct {
			Locale string
			Text   string
		}
		if json.Unmarshal(raw, &s) == nil {
			return ua.MustVariant(ua.NewLocalizedText(s)), nil
		}
		if err := json.Unmarshal(raw, &lt); err != nil {
			return nil, fmt.Errorf("want a string or {\"Locale\",\"Text\"}: %s", raw)
		}
		return ua.MustVariant(ua.NewLocalizedTextWithLocale(lt.Text, lt.Locale)), nil
	case "SourceName":
		if err := str(); err != nil {
			return nil, err
		}
		return ua.MustVariant(s), nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	switch x := v.(type) {
	case string, bool, float64:
		return ua.NewVariant(x)
	}
	return nil, fmt.Errorf(`arrays and objects take the OPC UA JSON encoding ({"UaType":<type id>,"Value":...}): %s`, raw)
}
