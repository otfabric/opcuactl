// SPDX-License-Identifier: MIT

package opcuactl

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/otfabric/go-opcua"
	"github.com/otfabric/go-opcua/ua"
)

// DiagnosticInfo output (--return-diagnostics). The server returns
// diagnostics only for what failed: the service diagnostics of a request it
// rejected as a whole (an *opcua.ServiceError), and one DiagnosticInfo per
// failed operation in the DiagnosticInfos of a response. Both are printed
// on stderr, so that the regular output (table or JSON) stays unchanged.

// printServiceDiagnostic prints the service diagnostics an error carries.
func printServiceDiagnostic(w io.Writer, err error) {
	var se *opcua.ServiceError
	if !errors.As(err, &se) || se.Diagnostic == nil {
		return
	}
	_, _ = fmt.Fprintf(w, "Diagnostic (service %s):\n", se.StatusCode.Symbol())
	writeDiagnostic(w, "  ", se.Diagnostic)
}

// printOperationDiagnostics prints the DiagnosticInfos of a response, label
// names operation i (e.g. the node it read).
func printOperationDiagnostics(hdr *ua.ResponseHeader, infos []*ua.DiagnosticInfo, label func(i int) string) {
	if connCfg.ReturnDiagnostics&ua.OperationLevelAll == 0 {
		return
	}
	writeOperationDiagnostics(os.Stderr, hdr, infos, label)
}

func writeOperationDiagnostics(w io.Writer, hdr *ua.ResponseHeader, infos []*ua.DiagnosticInfo, label func(i int) string) {
	for i, info := range infos {
		if info == nil {
			continue
		}
		d := hdr.Diagnostic(info)
		if d == nil {
			continue
		}
		_, _ = fmt.Fprintf(w, "Diagnostic (%s):\n", label(i))
		writeDiagnostic(w, "  ", d)
	}
}

// writeDiagnostic prints the members of a resolved DiagnosticInfo that are
// set, one per line, and the inner diagnostics indented below.
func writeDiagnostic(w io.Writer, indent string, d *ua.Diagnostic) {
	for d != nil {
		line := func(name, value string) {
			if value != "" {
				_, _ = fmt.Fprintf(w, "%s%-16s %s\n", indent, name+":", value)
			}
		}
		sym := d.SymbolicID
		if sym != "" && d.NamespaceURI != "" && d.NamespaceURI != "http://opcfoundation.org/UA/" {
			sym += " (" + d.NamespaceURI + ")"
		}
		line("SymbolicId", sym)
		text := d.LocalizedText
		if text != "" && d.Locale != "" {
			text += " [" + d.Locale + "]"
		}
		line("Text", text)
		line("AdditionalInfo", d.AdditionalInfo)
		if d.InnerStatusCode != 0 {
			line("InnerStatusCode", d.InnerStatusCode.Symbol())
		}
		d = d.Inner
		if d != nil {
			_, _ = fmt.Fprintf(w, "%sInner:\n", indent)
			indent += "  "
		}
	}
}
