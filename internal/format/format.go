// SPDX-License-Identifier: MIT

package format

import (
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/otfabric/go-opcua/ua"
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
	case CSV:
		headers, rows, err := csvRecords(v)
		if err != nil {
			return err
		}
		return w.writeCSV(headers, rows)
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

// csvRecords flattens v for CSV output by way of its JSON form: an object is
// one row, an array one row per element. The columns are the object keys in
// the order they first appear; a scalar element goes in a "value" column. A
// string cell holds the string, null is empty, and any other value (a number,
// a boolean, a nested object or array) holds its compact JSON.
func csvRecords(v interface{}) ([]string, [][]string, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, nil, err
	}
	var elems []json.RawMessage
	if err := json.Unmarshal(data, &elems); err != nil {
		elems = []json.RawMessage{data}
	}
	var headers []string
	col := map[string]int{}
	cells := make([]map[int]string, len(elems))
	for i, raw := range elems {
		cells[i] = map[int]string{}
		keys, vals, ok := objectFields(raw)
		if !ok {
			keys, vals = []string{"value"}, []json.RawMessage{raw}
		}
		for j, k := range keys {
			c, seen := col[k]
			if !seen {
				c = len(headers)
				col[k] = c
				headers = append(headers, k)
			}
			cells[i][c] = csvCell(vals[j])
		}
	}
	rows := make([][]string, len(elems))
	for i := range rows {
		rows[i] = make([]string, len(headers))
		for c, s := range cells[i] {
			rows[i][c] = s
		}
	}
	return headers, rows, nil
}

// objectFields returns the keys and values of a JSON object in document
// order, or false if raw is not an object.
func objectFields(raw json.RawMessage) ([]string, []json.RawMessage, bool) {
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, nil, false
	}
	var keys []string
	var vals []json.RawMessage
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, nil, false
		}
		var val json.RawMessage
		if err := dec.Decode(&val); err != nil {
			return nil, nil, false
		}
		keys = append(keys, tok.(string))
		vals = append(vals, val)
	}
	return keys, vals, true
}

func csvCell(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	if string(raw) == "null" {
		return ""
	}
	return string(raw)
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
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice || rv.Type().Elem().Kind() == reflect.Uint8 {
		return nil, false
	}
	items := make([]interface{}, rv.Len())
	for i := range items {
		items[i] = rv.Index(i).Interface()
	}
	return items, true
}

var (
	printedMu sync.Mutex
	printed   []string
)

// Errf prints an error message to stderr.
func Errf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	printedMu.Lock()
	printed = append(printed, msg)
	printedMu.Unlock()
	fmt.Fprintf(os.Stderr, "Error: %s\n", msg)
}

// Infof prints an informational message to stderr: the progress and summary
// lines of --verbose, warnings and hints that accompany a successful
// command. Unlike Errf it has no "Error:" prefix and does not count as a
// printed error.
func Infof(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}

// ErrPrinted reports whether an Errf message since the last ResetPrinted
// contains msg: a command that printed its error before returning it.
func ErrPrinted(msg string) bool {
	printedMu.Lock()
	defer printedMu.Unlock()
	for _, p := range printed {
		if strings.Contains(p, msg) {
			return true
		}
	}
	return false
}

// ResetPrinted forgets the messages printed by Errf.
func ResetPrinted() {
	printedMu.Lock()
	printed = nil
	printedMu.Unlock()
}

// FormatValue converts a value to a display string suitable for table/CSV output.
//
// Slices and arrays are rendered as comma-separated elements instead of Go's
// default "[a b c]" format. OPC UA values are rendered as JSON output renders
// them, never as Go pointers: a ByteString in base64, a DateTime in RFC 3339,
// a StatusCode by its name, a NodeId, QualifiedName or LocalizedText by its
// text, a Variant or DataValue by its value. An ExtensionObject with a
// decoded body is the name of its type and the JSON of the body
// (ServerStatusDataType {"StartTime":...}); one with a body that could not be
// decoded is its TypeId, encoding and length. Other structures are their
// type name and JSON.
func FormatValue(v interface{}) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case []byte:
		if x == nil {
			return ""
		}
		return base64.StdEncoding.EncodeToString(x)
	case time.Time:
		return x.Format(time.RFC3339Nano)
	case ua.StatusCode:
		return x.Symbol()
	case *ua.ExtensionObject:
		return formatExtensionObject(x)
	case *ua.Variant:
		if x == nil {
			return ""
		}
		return FormatValue(x.Value())
	case *ua.DataValue:
		return formatDataValue(x)
	case ua.XMLElement:
		return string(x)
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		n := rv.Len()
		parts := make([]string, n)
		for i := 0; i < n; i++ {
			parts[i] = FormatValue(rv.Index(i).Interface())
		}
		return strings.Join(parts, ", ")
	case reflect.Pointer, reflect.Interface, reflect.Map:
		if rv.IsNil() {
			return ""
		}
	}
	switch x := v.(type) {
	case error:
		return x.Error()
	case fmt.Stringer:
		return x.String()
	}
	switch {
	case rv.Kind() == reflect.Pointer && rv.Elem().Kind() != reflect.Struct:
		return FormatValue(rv.Elem().Interface())
	case rv.Kind() == reflect.Struct, rv.Kind() == reflect.Pointer, rv.Kind() == reflect.Map:
		return formatStruct(v)
	}
	return fmt.Sprintf("%v", v)
}

// formatStruct is the type name and the compact JSON of v:
// BuildInfo {"ProductURI":"...",...}.
func formatStruct(v interface{}) string {
	t := reflect.TypeOf(v)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	b, err := json.Marshal(v)
	if err != nil {
		return t.Name()
	}
	if t.Name() == "" {
		return string(b)
	}
	return t.Name() + " " + string(b)
}

// formatExtensionObject renders the decoded body of an ExtensionObject, or
// its TypeId, encoding and length when the body could not be decoded.
func formatExtensionObject(eo *ua.ExtensionObject) string {
	if eo == nil {
		return ""
	}
	if eo.Value != nil {
		return FormatValue(eo.Value)
	}
	typeID := "i=0"
	if eo.TypeID != nil && eo.TypeID.NodeID != nil {
		typeID = eo.TypeID.String()
	}
	switch {
	case eo.EncodingMask == ua.ExtensionObjectEmpty && len(eo.RawBody) == 0:
		return fmt.Sprintf("ExtensionObject{TypeId: %s, empty}", typeID)
	case eo.EncodingMask == ua.ExtensionObjectXML:
		return fmt.Sprintf("ExtensionObject{TypeId: %s, xml, %d bytes}", typeID, len(eo.RawBody))
	}
	return fmt.Sprintf("ExtensionObject{TypeId: %s, binary, %d bytes}", typeID, len(eo.RawBody))
}

// formatDataValue is the value of a DataValue, with its status when that is
// not Good.
func formatDataValue(dv *ua.DataValue) string {
	if dv == nil {
		return ""
	}
	s := FormatValue(dv.Value)
	if dv.Status != ua.StatusOK {
		if s == "" {
			return dv.Status.Symbol()
		}
		s += " (" + dv.Status.Symbol() + ")"
	}
	return s
}
