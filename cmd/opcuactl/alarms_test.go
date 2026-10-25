// SPDX-License-Identifier: MIT

package opcuactl

import (
	"testing"
	"time"

	"github.com/otfabric/go-opcua"
	"github.com/otfabric/go-opcua/id"
	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/opcuactl/internal/model"
)

func ptr[T any](v T) *T { return &v }

func TestShelvingName(t *testing.T) {
	for _, tt := range []struct {
		in   *ua.NodeID
		want string
	}{
		{nil, ""},
		{ua.NewNumericNodeID(0, id.ShelvedStateMachineTypeUnshelved), "Unshelved"},
		{ua.NewFourByteNodeID(0, id.ShelvedStateMachineTypeTimedShelved), "TimedShelved"},
		{ua.NewNumericNodeID(0, id.ShelvedStateMachineTypeOneShotShelved), "OneShotShelved"},
		{ua.NewNumericNodeID(2, id.ShelvedStateMachineTypeUnshelved), "ns=2;i=2930"},
		{ua.NewStringNodeID(0, "x"), "s=x"},
	} {
		if got := shelvingName(tt.in); got != tt.want {
			t.Errorf("shelvingName(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
	for in, want := range map[string]string{"": "-", "Unshelved": "no", "TimedShelved": "timed", "OneShotShelved": "one-shot", "ns=2;i=1": "ns=2;i=1"} {
		if got := shelvedCell(in); got != want {
			t.Errorf("shelvedCell(%q) = %q, want %q", in, got, want)
		}
	}
	if yesNo(nil) != "-" || yesNo(ptr(true)) != "yes" || yesNo(ptr(false)) != "no" {
		t.Error("yesNo")
	}
	if got := eventTypeName(ua.NewNumericNodeID(0, id.ExclusiveLevelAlarmType)); got != "ExclusiveLevelAlarmType" {
		t.Errorf("eventTypeName = %q", got)
	}
	if got := eventTypeName(ua.NewStringNodeID(3, "MyAlarm")); got != "ns=3;s=MyAlarm" {
		t.Errorf("eventTypeName = %q", got)
	}
}

func TestConditionMatches(t *testing.T) {
	c := &model.Condition{Severity: 500, Active: ptr(true), Acked: ptr(false), Confirmed: ptr(true), Shelving: "TimedShelved"}
	plain := &model.Condition{Severity: 100}
	for _, tt := range []struct {
		c      *model.Condition
		states []string
		min    uint16
		want   bool
	}{
		{c, nil, 0, true},
		{c, nil, 501, false},
		{c, []string{"active", "unacked", "confirmed", "shelved"}, 500, true},
		{c, []string{"inactive"}, 0, false},
		{c, []string{"acked"}, 0, false},
		{c, []string{"unconfirmed"}, 0, false},
		{plain, []string{"unacked"}, 0, false},
		{plain, []string{"active"}, 0, false},
		{plain, []string{"shelved"}, 0, false},
	} {
		got, err := conditionMatches(tt.c, tt.states, tt.min)
		if err != nil || got != tt.want {
			t.Errorf("conditionMatches(%+v, %v, %d) = %v, %v; want %v", tt.c, tt.states, tt.min, got, err, tt.want)
		}
	}
	if _, err := conditionMatches(c, []string{"loud"}, 0); err == nil {
		t.Error("unknown state accepted")
	}
}

func TestConditionNotification(t *testing.T) {
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	ev := &opcua.ConditionEvent{
		ConditionID: ua.NewStringNodeID(2, "Tank.LevelAlarm"),
		EventID:     []byte{1, 2, 3},
		EventType:   ua.NewNumericNodeID(0, id.ExclusiveLevelAlarmType),
		SourceNode:  ua.NewStringNodeID(2, "Tank"),
		Severity:    700,
		Message:     "high",
		Enabled:     true,
		Retain:      true,
		Active:      ptr(true),
		Acked:       ptr(false),
		Quality:     ptr(ua.StatusOK),
		Comment:     ua.NewLocalizedText("seen"),
	}
	n := conditionNotification(ev, now)
	if n == nil || n.Condition == nil || n.EventID != "AQID" || n.SourceNode != "ns=2;s=Tank" || n.Quality != "Good" || n.Comment != "seen" {
		t.Fatalf("notification = %+v", n)
	}
	if n := conditionNotification(&opcua.ConditionEvent{EventType: ua.NewNumericNodeID(0, id.RefreshStartEventType)}, now); n == nil || n.Refresh != "start" {
		t.Errorf("refresh start = %+v", n)
	}
	if n := conditionNotification(&opcua.ConditionEvent{EventType: ua.NewNumericNodeID(0, id.RefreshEndEventType)}, now); n == nil || n.Refresh != "end" {
		t.Errorf("refresh end = %+v", n)
	}
	if n := conditionNotification(&opcua.ConditionEvent{}, now); n != nil {
		t.Errorf("an event without ConditionId = %+v", n)
	}
}

func TestAlarmHint(t *testing.T) {
	for _, st := range []ua.StatusCode{
		ua.StatusBadEventIDUnknown, ua.StatusBadConditionBranchAlreadyAcked, ua.StatusBadConditionBranchAlreadyConfirmed,
		ua.StatusBadConditionAlreadyEnabled, ua.StatusBadConditionAlreadyDisabled, ua.StatusBadConditionDisabled,
		ua.StatusBadConditionAlreadyShelved, ua.StatusBadConditionNotShelved, ua.StatusBadShelvingTimeOutOfRange,
		ua.StatusBadUserAccessDenied, ua.StatusBadNodeIDUnknown, ua.StatusBadMethodInvalid, ua.StatusBadNoMatch,
	} {
		if alarmHint(st) == "" {
			t.Errorf("no hint for %s", st.Symbol())
		}
	}
	if alarmHint(ua.StatusBadInternalError) != "" {
		t.Error("a hint for BadInternalError")
	}
}
