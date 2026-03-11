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
