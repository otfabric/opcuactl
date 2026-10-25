// SPDX-License-Identifier: MIT

package parse

import (
	"testing"

	"github.com/otfabric/go-opcua/ua"
)

func TestReturnDiagnostics(t *testing.T) {
	for _, tt := range []struct {
		in      string
		want    uint32
		wantErr bool
	}{
		{"", 0, false},
		{"none", 0, false},
		{"off", 0, false},
		{"service", ua.ServiceLevelAll, false},
		{"Operation", ua.OperationLevelAll, false},
		{"all", ua.ReturnDiagnosticsAll, false},
		{"service,operation", ua.ReturnDiagnosticsAll, false},
		{" service , none ", ua.ServiceLevelAll, false},
		{"0x21", 0x21, false},
		{"1023", 0x3ff, false},
		{"0x400", 0, true},
		{"verbose", 0, true},
		{"-1", 0, true},
	} {
		got, err := ReturnDiagnostics(tt.in)
		if (err != nil) != tt.wantErr {
			t.Errorf("ReturnDiagnostics(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			continue
		}
		if got != tt.want {
			t.Errorf("ReturnDiagnostics(%q) = %#x, want %#x", tt.in, got, tt.want)
		}
	}
}
