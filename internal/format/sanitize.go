// SPDX-License-Identifier: MIT

package format

import (
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"
)

// Sanitize returns s as text output prints it: the C0 control characters
// (TAB, CR and LF included), DEL, the C1 control characters, the
// bidirectional embeddings, overrides and isolates (U+202A-U+202E,
// U+2066-U+2069) and bytes that are not UTF-8 are replaced by a visible
// escape: \t, \r, \n, \x1b, \u009b, ‮. A terminal therefore shows a
// string from the server or the network as the characters it holds. JSON,
// JSONL, YAML and CSV output keep the value as it is.
func Sanitize(s string) string {
	i := 0
	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		if needsEscape(r, size) {
			break
		}
		i += size
	}
	if i == len(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 8)
	b.WriteString(s[:i])
	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case !needsEscape(r, size):
			b.WriteString(s[i : i+size])
		case r == utf8.RuneError:
			fmt.Fprintf(&b, `\x%02x`, s[i])
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\n':
			b.WriteString(`\n`)
		case r < 0x80:
			fmt.Fprintf(&b, `\x%02x`, r)
		default:
			fmt.Fprintf(&b, `\u%04x`, r)
		}
		i += size
	}
	return b.String()
}

// needsEscape reports whether the rune r, size bytes long, is one Sanitize
// replaces.
func needsEscape(r rune, size int) bool {
	switch {
	case r == utf8.RuneError && size == 1:
		return true
	case r < 0x20, r == 0x7f:
		return true
	case r >= 0x80 && r <= 0x9f:
		return true
	case r >= 0x202a && r <= 0x202e, r >= 0x2066 && r <= 0x2069:
		return true
	}
	return false
}

// sanitizeArgs returns args for a message on stderr: each one is formatted
// as its verb says, then sanitized. The format string itself is not.
func sanitizeArgs(args []interface{}) []interface{} {
	out := make([]interface{}, len(args))
	for i, a := range args {
		out[i] = sanitized{a}
	}
	return out
}

// sanitized formats v with the verb, flags, width and precision it is
// printed with, and sanitizes the result.
type sanitized struct{ v interface{} }

func (s sanitized) Format(f fmt.State, verb rune) {
	_, _ = io.WriteString(f, Sanitize(fmt.Sprintf(fmt.FormatString(f, verb), s.v)))
}

// Printf prints to stdout as fmt.Printf does, its arguments sanitized: the
// text output of strings from the server or the network.
func Printf(format string, args ...interface{}) {
	_, _ = fmt.Fprintf(os.Stdout, format, sanitizeArgs(args)...)
}
