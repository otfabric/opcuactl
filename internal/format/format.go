package format

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
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
)

// ParseFormat parses a format string, defaulting to table.
func ParseFormat(s string) Format {
	switch strings.ToLower(s) {
	case "json":
		return JSON
	case "yaml":
		return YAML
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

// Errf prints an error message to stderr.
func Errf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "Error: "+format+"\n", args...)
}
