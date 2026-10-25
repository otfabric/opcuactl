// SPDX-License-Identifier: MIT

package opcuactl

import (
	"bytes"
	"strings"
	"testing"

	"github.com/otfabric/go-opcua"
	"github.com/otfabric/go-opcua/ua"
)

func TestDiagnosticsHint(t *testing.T) {
	for _, st := range []ua.StatusCode{ua.StatusBadUserAccessDenied, ua.StatusBadSecurityModeInsufficient, ua.StatusBadNodeIDUnknown, ua.StatusBadNotWritable} {
		if diagnosticsHint(st, "x") == "" {
			t.Errorf("no hint for %s", st.Symbol())
		}
	}
	if diagnosticsHint(ua.StatusBadTimeout, "x") != "" {
		t.Error("a hint for BadTimeout")
	}
}

func TestWriteDiagnostics(t *testing.T) {
	var buf bytes.Buffer
	printServiceDiagnostic(&buf, nil)
	printServiceDiagnostic(&buf, ua.StatusBadTimeout)
	if buf.Len() != 0 {
		t.Fatalf("printed %q for errors without diagnostics", buf.String())
	}

	err := exitCode(3, &opcua.ServiceError{StatusCode: ua.StatusBadTooManyOperations, Diagnostic: &ua.Diagnostic{
		SymbolicID:      "BadTooManyOperations",
		NamespaceURI:    "urn:vendor",
		LocalizedText:   "too many",
		Locale:          "en",
		AdditionalInfo:  "2 > 1",
		InnerStatusCode: ua.StatusBadInternalError,
		Inner:           &ua.Diagnostic{SymbolicID: "BadInternalError"},
	}})
	printServiceDiagnostic(&buf, err)
	out := buf.String()
	for _, want := range []string{
		"Diagnostic (service BadTooManyOperations):",
		"  SymbolicId:      BadTooManyOperations (urn:vendor)",
		"  Text:            too many [en]",
		"  AdditionalInfo:  2 > 1",
		"  InnerStatusCode: BadInternalError",
		"  Inner:\n    SymbolicId:      BadInternalError",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output does not contain %q:\n%s", want, out)
		}
	}

	buf.Reset()
	hdr := &ua.ResponseHeader{StringTable: []string{"BadNodeIdUnknown", "http://opcfoundation.org/UA/"}}
	infos := []*ua.DiagnosticInfo{
		nil,
		{},
		{EncodingMask: ua.DiagnosticInfoSymbolicID | ua.DiagnosticInfoNamespaceURI, SymbolicID: 0, NamespaceURI: 1},
	}
	writeOperationDiagnostics(&buf, hdr, infos, func(i int) string { return "op " + string(rune('0'+i)) })
	out = buf.String()
	if strings.Contains(out, "op 0") || strings.Contains(out, "op 1") || !strings.Contains(out, "Diagnostic (op 2):\n  SymbolicId:      BadNodeIdUnknown\n") {
		t.Errorf("operation diagnostics:\n%s", out)
	}
}
