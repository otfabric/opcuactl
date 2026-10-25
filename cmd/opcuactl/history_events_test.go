// SPDX-License-Identifier: MIT

package opcuactl

import (
	"testing"
	"time"

	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/opcuactl/internal/model"
)

func TestHistoryEventValue(t *testing.T) {
	for _, tt := range []struct {
		in   *ua.Variant
		want interface{}
	}{
		{nil, nil},
		{ua.MustVariant(nil), nil},
		{ua.MustVariant([]byte{1, 2, 3}), "AQID"},
		{ua.MustVariant(ua.NewNumericNodeID(0, 2041)), "i=2041"},
		{ua.MustVariant(ua.NewLocalizedText("hi")), "hi"},
		{ua.MustVariant(ua.StatusBadNoData), ua.StatusBadNoData.Symbol()},
		{ua.MustVariant(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)), "2026-01-02T03:04:05Z"},
		{ua.MustVariant(uint16(7)), uint16(7)},
	} {
		if got := historyEventValue(tt.in); got != tt.want {
			t.Errorf("historyEventValue(%v) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestHistoryUpdateResult(t *testing.T) {
	ok := &ua.HistoryUpdateResult{StatusCode: ua.StatusOK, OperationResults: []ua.StatusCode{ua.StatusGoodEntryInserted}}
	bad := &ua.HistoryUpdateResult{StatusCode: ua.StatusBadNotWritable, OperationResults: []ua.StatusCode{ua.StatusOK}}
	for _, tt := range []struct {
		r    *ua.HistoryUpdateResult
		i    int
		want ua.StatusCode
	}{
		{nil, 0, ua.StatusBadUnexpectedError},
		{ok, 0, ua.StatusGoodEntryInserted},
		{ok, 1, ua.StatusOK},
		{bad, 0, ua.StatusBadNotWritable},
	} {
		if got := historyUpdateResult(tt.r, tt.i); got != tt.want {
			t.Errorf("historyUpdateResult(%v, %d) = %v, want %v", tt.r, tt.i, got, tt.want)
		}
	}
}

func TestHistoryUpdateExit(t *testing.T) {
	good := model.HistoryEventUpdate{StatusCode: uint32(ua.StatusGoodEntryReplaced)}
	bad := model.HistoryEventUpdate{StatusCode: uint32(ua.StatusBadNoData)}
	for _, tt := range []struct {
		in   []model.HistoryEventUpdate
		want int
	}{
		{nil, 0},
		{[]model.HistoryEventUpdate{good, good}, 0},
		{[]model.HistoryEventUpdate{good, bad}, 4},
		{[]model.HistoryEventUpdate{bad, bad}, 3},
	} {
		if got := ExitCode(historyUpdateExit(tt.in, "x")); got != tt.want {
			t.Errorf("historyUpdateExit(%v) exit %d, want %d", tt.in, got, tt.want)
		}
	}
}

// Without a terminal on stdin the change goes ahead (as with --yes).
func TestConfirmHistoryChangeNoTerminal(t *testing.T) {
	if !confirmHistoryChange("go?") {
		t.Error("no terminal: not confirmed")
	}
}
