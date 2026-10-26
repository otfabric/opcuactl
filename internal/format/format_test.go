// SPDX-License-Identifier: MIT

package format

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-opcua/ua"
)

func TestParseFormat(t *testing.T) {
	tests := []struct {
		input string
		want  Format
	}{
		{"", Table},
		{"table", Table},
		{"json", JSON},
		{"JSON", JSON},
		{"yaml", YAML},
		{"unknown", Table},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ParseFormat(tt.input)
			if got != tt.want {
				t.Errorf("ParseFormat(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestWriterJSON(t *testing.T) {
	var buf bytes.Buffer
	w := &Writer{format: JSON, out: &buf}
	data := map[string]string{"key": "value"}
	if err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	var got map[string]string
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["key"] != "value" {
		t.Errorf("got %v, want key=value", got)
	}
}

func TestWriterJSONPretty(t *testing.T) {
	var buf bytes.Buffer
	w := &Writer{format: JSON, pretty: true, out: &buf}
	data := map[string]string{"key": "value"}
	if err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "  ") {
		t.Error("expected indented JSON output")
	}
}

func TestWriteTable(t *testing.T) {
	var buf bytes.Buffer
	w := &Writer{format: Table, out: &buf}
	headers := []string{"NAME", "VALUE"}
	rows := [][]string{{"foo", "bar"}, {"baz", "qux"}}
	if err := w.WriteTable(headers, rows); err != nil {
		t.Fatal(err)
	}
	output := buf.String()
	if !strings.Contains(output, "NAME") {
		t.Error("expected headers in output")
	}
	if !strings.Contains(output, "foo") {
		t.Error("expected data in output")
	}
}

func TestWriteTableAsJSON(t *testing.T) {
	var buf bytes.Buffer
	w := &Writer{format: JSON, out: &buf}
	headers := []string{"NAME", "VALUE"}
	rows := [][]string{{"foo", "bar"}}
	if err := w.WriteTable(headers, rows); err != nil {
		t.Fatal(err)
	}
	var got []map[string]string
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0]["NAME"] != "foo" {
		t.Errorf("unexpected JSON table output: %v", got)
	}
}

func TestParseFormatCSVAndJSONL(t *testing.T) {
	if got := ParseFormat("csv"); got != CSV {
		t.Errorf("ParseFormat(csv) = %q, want %q", got, CSV)
	}
	if got := ParseFormat("jsonl"); got != JSONL {
		t.Errorf("ParseFormat(jsonl) = %q, want %q", got, JSONL)
	}
}

type failingWriter struct{}

func (failingWriter) Write(p []byte) (n int, err error) {
	return 0, errors.New("write failed")
}

func TestWriteCSVTable_writeError(t *testing.T) {
	t.Parallel()
	w := NewWriter(CSV, false)
	w.SetOutput(failingWriter{})
	err := w.WriteTable([]string{"A"}, [][]string{{"b"}})
	if err == nil {
		t.Fatal("expected error from failing writer")
	}
}

func TestWriteCSVTable(t *testing.T) {
	var buf bytes.Buffer
	w := &Writer{format: CSV, out: &buf}
	headers := []string{"NAME", "VALUE"}
	rows := [][]string{{"foo", "bar"}, {"baz", "qux"}}
	if err := w.WriteTable(headers, rows); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 CSV lines (header + 2 rows), got %d: %v", len(lines), lines)
	}
	if lines[0] != "NAME,VALUE" {
		t.Errorf("header = %q, want NAME,VALUE", lines[0])
	}
	if lines[1] != "foo,bar" {
		t.Errorf("row 1 = %q, want foo,bar", lines[1])
	}
}

func TestWriteJSONL(t *testing.T) {
	var buf bytes.Buffer
	w := &Writer{format: JSONL, out: &buf}
	data := []map[string]string{{"a": "1"}, {"b": "2"}}
	if err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 JSONL lines, got %d", len(lines))
	}
	var first map[string]string
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatal(err)
	}
	if first["a"] != "1" {
		t.Errorf("first line = %v, want a=1", first)
	}
}

func TestWriteJSONLTable(t *testing.T) {
	var buf bytes.Buffer
	w := &Writer{format: JSONL, out: &buf}
	headers := []string{"KEY", "VAL"}
	rows := [][]string{{"x", "10"}, {"y", "20"}}
	if err := w.WriteTable(headers, rows); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 JSONL lines, got %d", len(lines))
	}
	var row map[string]string
	if err := json.Unmarshal([]byte(lines[0]), &row); err != nil {
		t.Fatal(err)
	}
	if row["KEY"] != "x" || row["VAL"] != "10" {
		t.Errorf("row = %v, want KEY=x VAL=10", row)
	}
}

func TestSetOutput(t *testing.T) {
	var buf1, buf2 bytes.Buffer
	w := &Writer{format: JSON, out: &buf1}
	w.SetOutput(&buf2)
	data := map[string]string{"k": "v"}
	if err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if buf1.Len() != 0 {
		t.Error("expected buf1 to be empty after SetOutput")
	}
	if buf2.Len() == 0 {
		t.Error("expected buf2 to have output after SetOutput")
	}
}

func TestNewWriter(t *testing.T) {
	t.Parallel()
	w := NewWriter(JSON, true)
	if w == nil {
		t.Fatal("NewWriter returned nil")
	}
	var buf bytes.Buffer
	w.SetOutput(&buf)
	if err := w.Write(map[string]int{"n": 1}); err != nil {
		t.Fatal(err)
	}
	if buf.Len() == 0 {
		t.Error("expected JSON output on buffer after SetOutput")
	}
}

func TestWriterYAML(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	w := NewWriter(YAML, false)
	w.SetOutput(&buf)
	if err := w.Write(map[string]string{"k": "v"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "k") || !strings.Contains(buf.String(), "v") {
		t.Errorf("YAML output = %q, want k and v", buf.String())
	}
}

func TestWriterWrite_tableFormatRequiresWriteTable(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	w := NewWriter(Table, false)
	w.SetOutput(&buf)
	err := w.Write(map[string]string{"a": "b"})
	if err == nil {
		t.Fatal("Write with table format should error")
	}
	if !strings.Contains(err.Error(), "WriteTable") {
		t.Errorf("error = %v, want mention of WriteTable", err)
	}
}

func TestWriterJSONL_nonSlice(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	w := NewWriter(JSONL, false)
	w.SetOutput(&buf)
	if err := w.Write(map[string]int{"x": 42}); err != nil {
		t.Fatal(err)
	}
	var got map[string]int
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["x"] != 42 {
		t.Errorf("got %v, want x=42", got)
	}
}

func TestWriterJSONL_interfaceSlice(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	w := NewWriter(JSONL, false)
	w.SetOutput(&buf)
	items := []interface{}{map[string]string{"a": "1"}, map[string]string{"b": "2"}}
	if err := w.Write(items); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %d: %q", len(lines), buf.String())
	}
}

func TestErrf(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	Errf("unit %s", "test")
	_ = w.Close()
	os.Stderr = old
	outB, err := io.ReadAll(r)
	_ = r.Close()
	if err != nil {
		t.Fatalf("read stderr: %v", err)
	}
	out := string(outB)
	if !strings.Contains(out, "Error:") || !strings.Contains(out, "unit test") {
		t.Errorf("Errf output = %q, want Error: and unit test", out)
	}
}

func TestInfof(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	ResetPrinted()
	Infof("found %d node(s)", 3)
	_ = w.Close()
	os.Stderr = old
	outB, err := io.ReadAll(r)
	_ = r.Close()
	if err != nil {
		t.Fatalf("read stderr: %v", err)
	}
	if out := string(outB); out != "found 3 node(s)\n" {
		t.Errorf("Infof output = %q", out)
	}
	if ErrPrinted("found") {
		t.Error("an Infof message counts as a printed error")
	}
}

func TestFormatValue(t *testing.T) {
	tests := []struct {
		name string
		val  interface{}
		want string
	}{
		{"nil", nil, ""},
		{"string", "hello", "hello"},
		{"int", 42, "42"},
		{"float", 3.14, "3.14"},
		{"int slice", []int32{1, 2, 3}, "1, 2, 3"},
		{"float slice", []float64{1.1, 2.2}, "1.1, 2.2"},
		{"string slice", []string{"a", "b", "c"}, "a, b, c"},
		{"empty slice", []int{}, ""},
		{"bool", true, "true"},
	}
	tests = append(tests, uaFormatCases()...)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatValue(tt.val)
			if got != tt.want {
				t.Errorf("FormatValue(%v) = %q, want %q", tt.val, got, tt.want)
			}
		})
	}
}

func TestWriteJSONL_typedSlice(t *testing.T) {
	type rec struct{ N int }
	var buf bytes.Buffer
	w := &Writer{format: JSONL, out: &buf}
	if err := w.Write([]rec{{1}, {2}, {3}}); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(buf.String()); got != "{\"N\":1}\n{\"N\":2}\n{\"N\":3}" {
		t.Errorf("typed slice as JSONL:\n%s", got)
	}
	buf.Reset()
	if err := w.Write([]byte("ab")); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(buf.String()); got != `"YWI="` {
		t.Errorf("[]byte is one value: %s", got)
	}
}

// TestErrPrinted checks the record Execute uses to print an error only
// once: a command that printed it with Errf before returning it.
func TestErrPrinted(t *testing.T) {
	old := os.Stderr
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = devNull
	t.Cleanup(func() { os.Stderr = old; _ = devNull.Close() })

	ResetPrinted()
	if ErrPrinted("refused") {
		t.Fatal("ErrPrinted after ResetPrinted")
	}
	Errf("connecting to %s: %s", "opc.tcp://plc:4840", "connection refused")
	if !ErrPrinted("connection refused") || !ErrPrinted("opc.tcp://plc:4840: connection refused") {
		t.Error("ErrPrinted does not find a part of the printed message")
	}
	if ErrPrinted("timeout") {
		t.Error("ErrPrinted found a message that was not printed")
	}
	ResetPrinted()
	if ErrPrinted("connection refused") {
		t.Error("ResetPrinted kept the messages")
	}
}

// TestWriterErrors checks that write errors of the output reach the
// caller in every line-oriented format.
func TestWriterErrors(t *testing.T) {
	t.Parallel()
	jsonl := NewWriter(JSONL, false)
	jsonl.SetOutput(failingWriter{})
	if err := jsonl.Write([]int{1, 2}); err == nil {
		t.Error("JSONL list: no error")
	}
	if err := jsonl.Write(map[string]int{"a": 1}); err == nil {
		t.Error("JSONL item: no error")
	}
	if err := jsonl.WriteTable([]string{"A"}, [][]string{{"b"}}); err == nil {
		t.Error("JSONL table: no error")
	}

	// A row larger than the buffer of the CSV writer fails on its own
	// write, before the flush.
	csvw := NewWriter(CSV, false)
	csvw.SetOutput(failingWriter{})
	if err := csvw.WriteTable([]string{"A"}, [][]string{{strings.Repeat("x", 1<<16)}}); err == nil {
		t.Error("CSV with a large row: no error")
	}
}

type plainStruct struct {
	A int
	B string
}

type badJSON struct{ C chan int }

func uaFormatCases() []struct {
	name string
	val  interface{}
	want string
} {
	ts := time.Date(2026, 10, 26, 10, 0, 0, 5, time.UTC)
	status := &ua.ServerStatusDataType{
		StartTime: ts,
		State:     ua.ServerStateRunning,
		BuildInfo: &ua.BuildInfo{ProductName: "opcuactl"},
	}
	statusJSON, _ := json.Marshal(status)
	diag := &ua.DiagnosticInfo{SymbolicID: 1}
	diagJSON, _ := json.Marshal(diag)
	return []struct {
		name string
		val  interface{}
		want string
	}{
		{"bytestring", []byte{1, 2, 3}, "AQID"},
		{"nil bytestring", []byte(nil), ""},
		{"bytestring array", [][]byte{{1}, {2}}, "AQ==, Ag=="},
		{"datetime", ts, "2026-10-26T10:00:00.000000005Z"},
		{"status code", ua.StatusBadNodeIDUnknown, "BadNodeIDUnknown"},
		{"node id", ua.NewStringNodeID(1, "X"), "ns=1;s=X"},
		{"nil node id", (*ua.NodeID)(nil), ""},
		{"expanded node id", ua.NewNumericExpandedNodeID(0, 85), "i=85"},
		{"qualified name", &ua.QualifiedName{NamespaceIndex: 2, Name: "Name"}, "2:Name"},
		{"localized text", ua.NewLocalizedTextWithLocale("hi", "en"), "en: hi"},
		{"localized text array", []*ua.LocalizedText{ua.NewLocalizedText("a"), ua.NewLocalizedText("b")}, "a, b"},
		{"node class", ua.NodeClassVariable, "NodeClassVariable"},
		{"xml", ua.XMLElement("<a/>"), "<a/>"},
		{"variant", ua.MustVariant(int32(5)), "5"},
		{"nil variant", (*ua.Variant)(nil), ""},
		{"variant array", []*ua.Variant{ua.MustVariant("a"), ua.MustVariant(true)}, "a, true"},
		{"data value", &ua.DataValue{EncodingMask: ua.DataValueValue, Value: ua.MustVariant(1.5)}, "1.5"},
		{"bad data value", &ua.DataValue{Status: ua.StatusBadNoData}, "BadNoData"},
		{"uncertain data value", &ua.DataValue{Value: ua.MustVariant(1.5), Status: ua.StatusUncertain}, "1.5 (Uncertain)"},
		{"nil data value", (*ua.DataValue)(nil), ""},
		{"diagnostic info", diag, "DiagnosticInfo " + string(diagJSON)},
		{"nil diagnostic info", (*ua.DiagnosticInfo)(nil), ""},
		{"decoded extension object", ua.NewExtensionObject(status), "ServerStatusDataType " + string(statusJSON)},
		{"extension object array", []*ua.ExtensionObject{ua.NewExtensionObject(&ua.Range{Low: 0, High: 1}), ua.NewExtensionObject(&ua.Range{High: 2})}, `Range {"Low":0,"High":1}, Range {"Low":0,"High":2}`},
		{"undecoded extension object", &ua.ExtensionObject{EncodingMask: ua.ExtensionObjectBinary, TypeID: ua.NewNumericExpandedNodeID(2, 5001), RawBody: []byte{1, 2, 3, 4}}, "ExtensionObject{TypeId: ns=2;i=5001, binary, 4 bytes}"},
		{"xml extension object", &ua.ExtensionObject{EncodingMask: ua.ExtensionObjectXML, TypeID: ua.NewNumericExpandedNodeID(2, 5002)}, "ExtensionObject{TypeId: ns=2;i=5002, xml, 0 bytes}"},
		{"empty extension object", &ua.ExtensionObject{TypeID: ua.NewNumericExpandedNodeID(0, 0)}, "ExtensionObject{TypeId: i=0, empty}"},
		{"extension object without type", &ua.ExtensionObject{EncodingMask: ua.ExtensionObjectBinary}, "ExtensionObject{TypeId: i=0, binary, 0 bytes}"},
		{"nil extension object", (*ua.ExtensionObject)(nil), ""},
		{"struct", plainStruct{A: 1, B: "x"}, `plainStruct {"A":1,"B":"x"}`},
		{"struct pointer", &plainStruct{A: 1}, `plainStruct {"A":1,"B":""}`},
		{"anonymous struct", struct{ A int }{2}, `{"A":2}`},
		{"map", map[string]int{"a": 1}, `{"a":1}`},
		{"nil map", map[string]int(nil), ""},
		{"unmarshallable", badJSON{}, "badJSON"},
		{"error", errors.New("boom"), "boom"},
		{"int pointer", new(int), "0"},
	}
}

func TestWriterCSV(t *testing.T) {
	t.Parallel()
	type row struct {
		Name   string      `json:"name"`
		Value  interface{} `json:"value"`
		Status string      `json:"status,omitempty"`
	}
	cases := []struct {
		name string
		v    interface{}
		want string
	}{
		{"struct", row{Name: "a", Value: 1.5}, "name,value\na,1.5\n"},
		{"slice with union of columns", []row{
			{Name: "a", Value: "x"},
			{Name: "b", Value: []int{1, 2}, Status: "Bad"},
			{Name: "c"},
		}, "name,value,status\na,x,\nb,\"[1,2]\",Bad\nc,,\n"},
		{"nested object", map[string]interface{}{"k": map[string]int{"x": 1}}, "k\n\"{\"\"x\"\":1}\"\n"},
		{"scalars", []int{1, 2}, "value\n1\n2\n"},
		{"single scalar", true, "value\ntrue\n"},
		{"empty slice", []row{}, "\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			w := NewWriter(CSV, false)
			w.SetOutput(&buf)
			if err := w.Write(tc.v); err != nil {
				t.Fatal(err)
			}
			if got := buf.String(); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}

	w := NewWriter(CSV, false)
	w.SetOutput(&bytes.Buffer{})
	if err := w.Write(badJSON{C: make(chan int)}); err == nil {
		t.Error("CSV of an unmarshalable value: no error")
	}
}
