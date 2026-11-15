// SPDX-License-Identifier: MIT

package format

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"unicode/utf8"
)

// controlFree reports the first rune of out that a terminal would act on,
// other than the line ends the output has itself.
func controlFree(out string) (rune, bool) {
	for i, r := range out {
		if r == utf8.RuneError {
			if _, size := utf8.DecodeRuneInString(out[i:]); size == 1 {
				return r, false
			}
		}
		if (r < 0x20 && r != '\n') || r == 0x7f || (r >= 0x80 && r <= 0x9f) ||
			(r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069) {
			return r, false
		}
	}
	return 0, true
}

func TestSanitize(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"", ""},
		{"plain text, äöü, 温度, 🙂", "plain text, äöü, 温度, 🙂"},
		{"\x1b[2J\x1b]0;title\x07", `\x1b[2J\x1b]0;title\x07`},
		{"a\rb", `a\rb`},
		{"a\nb", `a\nb`},
		{"a\tb", `a\tb`},
		{"\x00\x01\x1f", `\x00\x01\x1f`},
		{"del\x7f", `del\x7f`},
		{"csi\u009b2J", `csi\u009b2J`},
		{"\u0080\u009f", `\u0080\u009f`},
		{"\u202aa\u202bb\u202cc\u202dd\u202ee", `\u202aa\u202bb\u202cc\u202dd\u202ee`},
		{"\u2066a\u2067b\u2068c\u2069", `\u2066a\u2067b\u2068c\u2069`},
		{"\u2029\u200f\u00a0", "\u2029\u200f\u00a0"}, // printable or not acted on: kept
		{"bad \xff\x9b utf-8", `bad \xff\x9b utf-8`},
		{`a\x1b`, `a\x1b`}, // an escape stays as it is
	} {
		got := Sanitize(c.in)
		if got != c.want {
			t.Errorf("Sanitize(%q) = %q, want %q", c.in, got, c.want)
		}
		if r, ok := controlFree(strings.ReplaceAll(got, "\n", "")); !ok {
			t.Errorf("Sanitize(%q) keeps %U", c.in, r)
		}
		if Sanitize(got) != got {
			t.Errorf("Sanitize(%q) is not stable", got)
		}
	}
}

// controlCell is a cell with one of each class Sanitize replaces.
const controlCell = "v\x1b[31mred\x1b[0m\rover\nline\tcol\x7f\u009b1A\u202eright\u2066\xff"

func TestWriteTableSanitizes(t *testing.T) {
	var b bytes.Buffer
	w := NewWriter(Table, false)
	w.SetOutput(&b)
	if err := w.WriteTable([]string{"NAME", "VALUE"}, [][]string{{"a", controlCell}, {"b\nfake", "row"}}); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	if r, ok := controlFree(out); !ok {
		t.Errorf("table output keeps %U:\n%q", r, out)
	}
	if lines := strings.Count(out, "\n"); lines != 3 {
		t.Errorf("%d lines, want a header and 2 rows:\n%s", lines, out)
	}
	if !strings.Contains(out, Sanitize(controlCell)) || !strings.Contains(out, `b\nfake`) {
		t.Errorf("table:\n%s", out)
	}
}

// JSON, YAML and CSV output keep the value as it is (JSON carries UTF-8
// only: the test value is).
func TestStructuredOutputKeepsRawValue(t *testing.T) {
	controlCell := strings.TrimSuffix(controlCell, "\xff")
	for _, f := range []Format{JSON, JSONL, YAML, CSV} {
		var b bytes.Buffer
		w := NewWriter(f, false)
		w.SetOutput(&b)
		if err := w.Write([]map[string]string{{"value": controlCell}}); err != nil {
			t.Fatal(err)
		}
		switch f {
		case JSON, JSONL:
			var v []map[string]string
			line := strings.TrimSpace(b.String())
			if f == JSONL {
				var one map[string]string
				if err := json.Unmarshal([]byte(line), &one); err != nil {
					t.Fatalf("%s: %v", f, err)
				}
				v = append(v, one)
			} else if err := json.Unmarshal([]byte(line), &v); err != nil {
				t.Fatalf("%s: %v", f, err)
			}
			if len(v) != 1 || v[0]["value"] != controlCell {
				t.Errorf("%s: %q", f, v)
			}
		default:
			if strings.Contains(b.String(), Sanitize(controlCell)) {
				t.Errorf("%s output is sanitized: %q", f, b.String())
			}
		}
	}
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stderr
	os.Stderr = w
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	fn()
	_ = w.Close()
	os.Stderr = orig
	return <-done
}

type stringer struct{ s string }

func (p *stringer) String() string { return p.s }

// Errf and Infof sanitize their arguments, not their format string; the
// verbs keep their meaning. ErrPrinted knows the message as it was given.
func TestStderrMessagesSanitize(t *testing.T) {
	ResetPrinted()
	defer ResetPrinted()
	err := errors.New("bad\x1b[2Jvalue")
	got := captureStderr(t, func() {
		Errf("reading %s: %v", "ns=1;s=a\nb", err)
		Infof("line one\n  %q %5d|%-4s|%x", "q\x1b", 42, "ab", 255)
		Infof("%v %s", nil, (*stringer)(nil))
	})
	want := "Error: reading ns=1;s=a\\nb: bad\\x1b[2Jvalue\n" +
		"line one\n  \"q\\x1b\" " + fmt.Sprintf("%5d|%-4s|%x", 42, "ab", 255) + "\n" +
		fmt.Sprintf("%v %s", nil, (*stringer)(nil)) + "\n"
	if got != want {
		t.Errorf("stderr\n%q\nwant\n%q", got, want)
	}
	if !ErrPrinted("bad\x1b[2Jvalue") {
		t.Error("ErrPrinted does not know the message as given")
	}
}

func TestPrintfSanitizes(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	Printf("%s  %s\n", "ns=1;s=X", controlCell)
	_ = w.Close()
	os.Stdout = orig
	b, _ := io.ReadAll(r)
	if string(b) != "ns=1;s=X  "+Sanitize(controlCell)+"\n" {
		t.Errorf("%q", b)
	}
}
