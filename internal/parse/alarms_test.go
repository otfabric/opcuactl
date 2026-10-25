// SPDX-License-Identifier: MIT

package parse

import (
	"bytes"
	"testing"
)

func TestEventID(t *testing.T) {
	for _, tt := range []struct {
		in         string
		want       []byte
		wantLatest bool
		wantErr    bool
	}{
		{"", nil, true, false},
		{"latest", nil, true, false},
		{"LATEST", nil, true, false},
		{"AQID", []byte{1, 2, 3}, false, false},
		{"AQIDBA==", []byte{1, 2, 3, 4}, false, false},
		{"AQIDBA", []byte{1, 2, 3, 4}, false, false},
		{"_-8", []byte{0xff, 0xef}, false, false},
		{"0x0a0B", []byte{0x0a, 0x0b}, false, false},
		{"0xzz", nil, false, true},
		{"0x", nil, false, true},
		{"!!", nil, false, true},
	} {
		got, latest, err := EventID(tt.in)
		if (err != nil) != tt.wantErr {
			t.Errorf("EventID(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			continue
		}
		if latest != tt.wantLatest || !bytes.Equal(got, tt.want) {
			t.Errorf("EventID(%q) = %x, %v; want %x, %v", tt.in, got, latest, tt.want, tt.wantLatest)
		}
	}
}
