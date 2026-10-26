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
