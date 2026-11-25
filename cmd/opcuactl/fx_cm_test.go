// SPDX-License-Identifier: MIT

package opcuactl

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/go-opcua/uafx/fxcmid"
	"github.com/otfabric/opcuactl/internal/model"
)

func TestCoerceLike(t *testing.T) {
	for _, c := range []struct {
		cur   any
		value string
		want  any
	}{
		{true, "false", false},
		{int16(1), "-2", int16(-2)},
		{uint16(1), "2", uint16(2)},
		{int32(1), "-3", int32(-3)},
		{uint32(1), "3", uint32(3)},
		{int64(1), "-4", int64(-4)},
		{uint64(1), "4", uint64(4)},
		{float32(1), "0.5", float32(0.5)},
		{float64(1), "0.25", 0.25},
		{int8(1), "-5", int8(-5)},
		{uint8(1), "5", uint8(5)},
		{"a", "b", "b"},
		{ua.NewNumericNodeID(0, 1), "i=85", ua.NewNumericNodeID(0, 85)},
	} {
		got, err := coerceLike(c.cur, c.value)
		if err != nil {
			t.Fatalf("%T %q: %v", c.cur, c.value, err)
		}
		if n, ok := c.want.(*ua.NodeID); ok {
			if got.(*ua.NodeID).String() != n.String() {
				t.Fatalf("%v, want %v", got, n)
			}
			continue
		}
		if got != c.want {
			t.Fatalf("%T %q: %v (%T), want %v", c.cur, c.value, got, got, c.want)
		}
	}
	if _, err := coerceLike(ua.NewExtensionObject(nil), "1"); err == nil {
		t.Fatal("an ExtensionObject coerced")
	}
	if _, err := coerceLike(int32(0), "x"); err == nil {
		t.Fatal("x coerced to Int32")
	}
}

func TestCMCallExit(t *testing.T) {
	good, bad, denied := ua.StatusOK, ua.StatusBadInvalidState, ua.StatusBadUserAccessDenied
	for _, c := range []struct {
		status  ua.StatusCode
		results []ua.StatusCode
		want    int
	}{
		{good, []ua.StatusCode{good, good}, exitOK},
		{ua.StatusUncertain, []ua.StatusCode{good, bad}, exitPartial},
		{ua.StatusUncertain, []ua.StatusCode{bad, bad}, exitProtocol},
		{denied, []ua.StatusCode{denied}, exitPermission},
		{ua.StatusUncertain, []ua.StatusCode{denied, denied}, exitPermission},
		{ua.StatusBadInvalidArgument, nil, exitProtocol},
	} {
		if got := ExitCode(cmCallExit("M", c.status, c.results)); got != c.want {
			t.Fatalf("%v %v: exit %d, want %d", c.status, c.results, got, c.want)
		}
	}
	for _, c := range []struct {
		applied bool
		results []ua.StatusCode
		want    int
	}{
		{true, []ua.StatusCode{good}, exitOK},
		{true, []ua.StatusCode{good, bad}, exitPartial},
		{false, []ua.StatusCode{bad}, exitProtocol},
		{false, []ua.StatusCode{denied}, exitPermission},
	} {
		n := 0
		for _, r := range c.results {
			if r == good {
				n++
			}
		}
		if got := ExitCode(cmImportExit(c.applied, n, len(c.results), c.results)); got != c.want {
			t.Fatalf("%v %v: exit %d, want %d", c.applied, c.results, got, c.want)
		}
	}
}

func TestCMStateName(t *testing.T) {
	x := &cmClient{}
	x.ns.CM = 3
	for id, want := range map[uint32]string{
		fxcmid.ConnectionConfigurationSetStateMachineTypeReady:      "Ready",
		fxcmid.ConnectionConfigurationSetStateMachineTypeProcessing: "Processing",
		fxcmid.ConnectionConfigurationSetStateMachineTypeError:      "Error",
		1: "",
	} {
		if got := x.stateName(ua.NewNumericNodeID(3, id)); got != want {
			t.Fatalf("%d: %q, want %q", id, got, want)
		}
	}
	if x.stateName(ua.NewNumericNodeID(2, fxcmid.ConnectionConfigurationSetStateMachineTypeReady)) != "" {
		t.Fatal("a state of another namespace")
	}
	if x.stateName("Ready") != "" || x.stateName(ua.NewStringNodeID(3, "Ready")) != "" {
		t.Fatal("not a numeric NodeId")
	}
	if d, bad := x.diagnostics([]*ua.ExtensionObject{nil, ua.NewExtensionObject(&ua.Range{})}); len(d) != 0 || len(bad) != 2 {
		t.Fatalf("diagnostics of other structures: %+v %v", d, bad)
	}
}

func TestCMNullNodeIDAndAnswered(t *testing.T) {
	for _, n := range []*ua.NodeID{ua.NewTwoByteNodeID(0), ua.NewFourByteNodeID(0, 0), ua.NewNumericNodeID(0, 0)} {
		if !isNullNodeID(n) {
			t.Fatalf("%v is null", n)
		}
	}
	for _, n := range []*ua.NodeID{ua.NewNumericNodeID(1, 0), ua.NewNumericNodeID(0, 1), ua.NewStringNodeID(0, "")} {
		if isNullNodeID(n) {
			t.Fatalf("%v is not null", n)
		}
	}
	for err, want := range map[error]bool{
		fmt.Errorf("x: %w", ua.StatusBadInvalidState):     true,
		fmt.Errorf("x: %w", ua.StatusBadTimeout):          false,
		fmt.Errorf("x: %w", ua.StatusBadConnectionClosed): false,
		fmt.Errorf("x: %w", context.DeadlineExceeded):     false,
		errors.New("no status"):                           false,
	} {
		if got := answered(err); got != want {
			t.Fatalf("%v: answered %v, want %v", err, got, want)
		}
	}
}

func TestCloseAndUpdateResult(t *testing.T) {
	ok, bad := ua.StatusOK, ua.StatusBadInvalidArgument
	v := ua.MustVariant
	for _, c := range []struct {
		name    string
		out     []*ua.Variant
		n       int
		require bool
		fails   bool
	}{
		{"good", []*ua.Variant{v(true), v([]ua.StatusCode{ok}), v([]*ua.NodeID{ua.NewNumericNodeID(1, 1)})}, 1, false, false},
		{"no objects", []*ua.Variant{v(true), v([]ua.StatusCode{ok}), nil}, 1, false, false},
		{"partial", []*ua.Variant{v(true), v([]ua.StatusCode{ok, bad}), v([]*ua.NodeID{})}, 2, false, false},
		{"none applied", []*ua.Variant{v(false), v([]ua.StatusCode{bad}), v([]*ua.NodeID{})}, 1, false, false},
		{"two outputs", []*ua.Variant{v(true), v([]ua.StatusCode{ok})}, 1, false, true},
		{"applied type", []*ua.Variant{v("yes"), v([]ua.StatusCode{ok}), nil}, 1, false, true},
		{"results type", []*ua.Variant{v(true), v([]uint32{0}), nil}, 1, false, true},
		{"results count", []*ua.Variant{v(true), v([]ua.StatusCode{ok, ok}), nil}, 1, false, true},
		{"objects type", []*ua.Variant{v(true), v([]ua.StatusCode{ok}), v([]string{"x"})}, 1, false, true},
		{"objects count", []*ua.Variant{v(true), v([]ua.StatusCode{ok}), v([]*ua.NodeID{ua.NewNumericNodeID(1, 1), ua.NewNumericNodeID(1, 2)})}, 1, false, true},
		{"applied none good", []*ua.Variant{v(true), v([]ua.StatusCode{bad}), nil}, 1, false, true},
		{"all good not applied", []*ua.Variant{v(false), v([]ua.StatusCode{ok}), nil}, 1, false, true},
		{"incomplete under require", []*ua.Variant{v(true), v([]ua.StatusCode{ok, bad}), nil}, 2, true, true},
	} {
		u, err := closeAndUpdateResult(c.out, c.n)
		if err == nil {
			err = u.check(c.require)
		}
		if (err != nil) != c.fails {
			t.Fatalf("%s: %v", c.name, err)
		}
	}
	if _, err := readChunk([]*ua.Variant{ua.MustVariant(int32(1))}); err == nil {
		t.Fatal("an Int32 read as Data")
	}
	if _, err := readChunk(nil); err == nil {
		t.Fatal("no output read as Data")
	}
	if b, err := readChunk([]*ua.Variant{ua.MustVariant(ua.ByteArray{1})}); err != nil || len(b) != 1 {
		t.Fatalf("Byte array: %v %v", b, err)
	}
}

func TestWriteFxCMTables(t *testing.T) {
	v, yes, no := uint32(3), true, false
	out, err := captureRun(t, func() error {
		return writeFxCMList(model.FxCMList{AggregatedCurrentState: &yes, Sets: []model.FxCMSet{
			{Path: "A", StateID: "Error", Version: &v, Edit: &no, Connections: []model.FxCMConnection{{Name: "C1"}}},
		}})
	})
	if err != nil || out == "" {
		t.Fatalf("list: %v %q", err, out)
	}
	out, err = captureRun(t, func() error {
		return writeFxCMEdit(model.FxCMEdit{Path: "A", Committed: true, Writes: []model.FxCMWrite{{Path: "x", Value: "1"}}})
	})
	if err != nil || out == "" {
		t.Fatalf("edit: %v %q", err, out)
	}
	for _, e := range []model.FxCMEdit{{Path: "A", Discarded: true}, {Path: "A", Outcome: "unknown"}, {Path: "A"}, {Path: "A", Committed: true, Version: &v}} {
		if _, err = captureRun(t, func() error { return writeFxCMEdit(e) }); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = captureRun(t, func() error {
		return writeFxCMList(model.FxCMList{Sets: []model.FxCMSet{{Path: "A", Unread: []string{"Version"}}}})
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = captureRun(t, func() error {
		return writeFxCMImport(model.FxCMImport{Operations: []model.FxCMOperation{{Path: "A"}}})
	}); err != nil {
		t.Fatal(err)
	}
}

// captureRun runs fn with the table format and returns what it wrote to
// stdout; what it writes to stderr is dropped.
func captureRun(t *testing.T, fn func() error) (stdout string, err error) {
	t.Helper()
	cliMu.Lock()
	defer cliMu.Unlock()
	prev := outputFormat
	outputFormat = "table"
	defer func() { outputFormat = prev }()
	stopOut, perr := captureFile(&os.Stdout)
	if perr != nil {
		t.Fatal(perr)
	}
	stopErr, perr := captureFile(&os.Stderr)
	if perr != nil {
		stopOut()
		t.Fatal(perr)
	}
	err = fn()
	_ = stopErr()
	return stopOut(), err
}
