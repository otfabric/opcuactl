// SPDX-License-Identifier: MIT

package opcuactl

import (
	"io"
	"os"
	"testing"

	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/opcuactl/internal/model"
)

// diagHeader and diagInfos are an operation diagnostic of a response.
var (
	diagHeader = &ua.ResponseHeader{StringTable: []string{"BadEntryExists"}}
	diagInfos  = []*ua.DiagnosticInfo{{EncodingMask: ua.DiagnosticInfoSymbolicID, SymbolicID: 0}}
)

// withOutput sets --format for the test and, with broken, makes os.Stdout
// a closed file that every write fails on; the output is thrown away.
func withOutput(t *testing.T, outFormat string, broken bool) {
	t.Helper()
	cliMu.Lock()
	r, w, err := os.Pipe()
	if err != nil {
		cliMu.Unlock()
		t.Fatal(err)
	}
	stdout, stderr, savedFormat := os.Stdout, os.Stderr, outputFormat
	os.Stdout, os.Stderr, outputFormat = w, w, outFormat
	done := make(chan struct{})
	go func() { _, _ = io.Copy(io.Discard, r); close(done) }()
	if broken {
		_ = r.Close()
		os.Stdout = os.NewFile(^uintptr(0), "closed")
	}
	// Operation diagnostics are printed, to stderr.
	savedDiag := connCfg.ReturnDiagnostics
	connCfg.ReturnDiagnostics = ua.OperationLevelAll
	t.Cleanup(func() {
		connCfg.ReturnDiagnostics = savedDiag
		os.Stdout, os.Stderr, outputFormat = stdout, stderr, savedFormat
		_ = w.Close()
		<-done
		cliMu.Unlock()
	})
}

// TestHistoryValueResponse covers the response handling of update-values
// and delete-values the demo server never produces.
func TestHistoryValueResponse(t *testing.T) {
	results := func() []model.HistoryValueUpdate { return make([]model.HistoryValueUpdate, 2) }
	ok := &ua.HistoryUpdateResponse{ResponseHeader: diagHeader, DiagnosticInfos: diagInfos,
		Results: []*ua.HistoryUpdateResult{{StatusCode: ua.StatusOK, OperationResults: []ua.StatusCode{ua.StatusOK, ua.StatusBadEntryExists}}}}
	for _, c := range []struct {
		name     string
		resp     *ua.HistoryUpdateResponse
		err      error
		whole    bool
		broken   bool
		code     int
		statuses string
	}{
		{name: "service fault", err: ua.StatusBadTooManyOperations, code: exitProtocol},
		{name: "no results", resp: &ua.HistoryUpdateResponse{ResponseHeader: &ua.ResponseHeader{}}, code: exitProtocol},
		{name: "per operation", resp: ok, code: exitPartial, statuses: "Good,BadEntryExists"},
		{name: "whole range", resp: ok, whole: true, code: 0, statuses: "Good,Good"},
		{name: "output fails", resp: ok, broken: true, code: -1},
	} {
		t.Run(c.name, func(t *testing.T) {
			res := results()
			func() {
				withOutput(t, "json", c.broken)
				err := historyValueResponse(c.resp, c.err, res, "changing x", "history change x", c.whole)
				if got := ExitCode(err); got != c.code && (c.code != -1 || err == nil) {
					t.Errorf("exit %d (%v), want %d", got, err, c.code)
				}
			}()
			if c.statuses != "" && statuses(res) != c.statuses {
				t.Errorf("statuses %s, want %s", statuses(res), c.statuses)
			}
		})
	}
}

// TestWriteAttrResponse covers the response handling of write attr the
// demo server never produces.
func TestWriteAttrResponse(t *testing.T) {
	node := ua.NewStringNodeID(1, "x")
	ok := &ua.WriteResponse{ResponseHeader: diagHeader, DiagnosticInfos: diagInfos, Results: []ua.StatusCode{ua.StatusOK}}
	// code -1: any error.
	for _, c := range []struct {
		name   string
		resp   *ua.WriteResponse
		err    error
		format string
		broken bool
		code   int
	}{
		{name: "service fault", err: ua.StatusBadTooManyOperations, format: "json", code: exitProtocol},
		{name: "no results", resp: &ua.WriteResponse{ResponseHeader: &ua.ResponseHeader{}}, format: "json", code: exitProtocol},
		{name: "good json", resp: ok, format: "json", code: 0},
		{name: "output fails", resp: ok, format: "json", broken: true, code: -1},
		{name: "table fails", resp: ok, format: "table", broken: true, code: -1},
	} {
		t.Run(c.name, func(t *testing.T) {
			withOutput(t, c.format, c.broken)
			err := writeAttrResponse(c.resp, c.err, node, ua.AttributeIDDisplayName)
			if got := ExitCode(err); got != c.code && (c.code != -1 || err == nil) {
				t.Errorf("exit %d (%v), want %d", got, err, c.code)
			}
		})
	}
}

// TestRegisterFailed maps a refused registration to exit 6 and a lost
// connection, which carries no status code, to exit 4.
func TestRegisterFailed(t *testing.T) {
	withOutput(t, "json", false)
	if got := ExitCode(registerFailed(ua.StatusBadSecurityChecksFailed)); got != exitProtocol {
		t.Errorf("status: exit %d, want %d", got, exitProtocol)
	}
	if got := ExitCode(registerFailed(io.ErrUnexpectedEOF)); got != exitTransport {
		t.Errorf("connection: exit %d, want %d", got, exitTransport)
	}
}
