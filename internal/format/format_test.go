package format

import (
	"bytes"
	"encoding/json"
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
