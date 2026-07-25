// SPDX-License-Identifier: MIT

package format

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"text/tabwriter"

	"gopkg.in/yaml.v3"
)

// Format represents an output format.
type Format string

const (
	Table Format = "table"
	JSON  Format = "json"
	YAML  Format = "yaml"
	CSV   Format = "csv"
	JSONL Format = "jsonl"
)

// ParseFormat parses a format string, defaulting to table.
func ParseFormat(s string) Format {
	switch strings.ToLower(s) {
	case "json":
		return JSON
	case "yaml":
		return YAML
	case "csv":
		return CSV
	case "jsonl":
		return JSONL
	default:
		return Table
	}
}

// Writer handles formatted output.
type Writer struct {
	format Format
	pretty bool
	out    io.Writer
}

// NewWriter creates a new format writer.
func NewWriter(format Format, pretty bool) *Writer {
	return &Writer{format: format, pretty: pretty, out: os.Stdout}
}

// Write outputs data in the configured format.
func (w *Writer) Write(v interface{}) error {
	switch w.format {
	case JSON:
		return w.writeJSON(v)
	case YAML:
		return w.writeYAML(v)
	case JSONL:
		return w.writeJSONL(v)
	default:
		return fmt.Errorf("use WriteTable for table format")
	}
}

// WriteTable writes data as a formatted table.
func (w *Writer) WriteTable(headers []string, rows [][]string) error {
	switch w.format {
	case JSON, YAML:
		items := make([]map[string]string, len(rows))
		for i, row := range rows {
			item := make(map[string]string, len(headers))
			for j, h := range headers {
				if j < len(row) {
					item[h] = row[j]
				}
			}
			items[i] = item
		}
		return w.Write(items)
	case CSV:
		return w.writeCSV(headers, rows)
	case JSONL:
		return w.writeJSONLTable(headers, rows)
	default:
		return w.writeTable(headers, rows)
	}
}

func (w *Writer) writeJSON(v interface{}) error {
	enc := json.NewEncoder(w.out)
	if w.pretty {
		enc.SetIndent("", "  ")
	}
	return enc.Encode(v)
}

func (w *Writer) writeYAML(v interface{}) error {
	enc := yaml.NewEncoder(w.out)
	defer func() { _ = enc.Close() }()
	return enc.Encode(v)
}

func (w *Writer) writeTable(headers []string, rows [][]string) error {
	tw := tabwriter.NewWriter(w.out, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, strings.Join(headers, "\t"))
	for _, row := range rows {
		_, _ = fmt.Fprintln(tw, strings.Join(row, "\t"))
	}
	return tw.Flush()
}

func (w *Writer) writeCSV(headers []string, rows [][]string) error {
	cw := csv.NewWriter(w.out)
	if err := cw.Write(headers); err != nil {
		return err
	}
	for _, row := range rows {
		if err := cw.Write(row); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func (w *Writer) writeJSONL(v interface{}) error {
	// For slices, write one JSON line per element
	if items, ok := toSlice(v); ok {
		enc := json.NewEncoder(w.out)
		for _, item := range items {
			if err := enc.Encode(item); err != nil {
				return err
			}
		}
		return nil
	}
	// Single item: one line
	return json.NewEncoder(w.out).Encode(v)
}

func (w *Writer) writeJSONLTable(headers []string, rows [][]string) error {
	enc := json.NewEncoder(w.out)
	for _, row := range rows {
		item := make(map[string]string, len(headers))
		for j, h := range headers {
			if j < len(row) {
				item[h] = row[j]
			}
		}
		if err := enc.Encode(item); err != nil {
			return err
		}
	}
	return nil
}

// SetOutput sets the writer's output destination.
func (w *Writer) SetOutput(out io.Writer) {
	w.out = out
}

// toSlice attempts to convert an interface to a slice of interfaces using reflection.
func toSlice(v interface{}) ([]interface{}, bool) {
	switch s := v.(type) {
	case []interface{}:
		return s, true
	case []map[string]string:
		items := make([]interface{}, len(s))
		for i, m := range s {
			items[i] = m
		}
		return items, true
	default:
		return nil, false
	}
}

// Errf prints an error message to stderr.
func Errf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "Error: "+format+"\n", args...)
}

// FormatValue converts a value to a display string suitable for table/CSV output.
// Slices and arrays are rendered as comma-separated elements instead of Go's
// default "[a b c]" format.
func FormatValue(v interface{}) string {
	if v == nil {
		return ""
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array {
		n := rv.Len()
		parts := make([]string, n)
		for i := 0; i < n; i++ {
			parts[i] = fmt.Sprintf("%v", rv.Index(i).Interface())
		}
		return strings.Join(parts, ", ")
	}
	return fmt.Sprintf("%v", v)
}
