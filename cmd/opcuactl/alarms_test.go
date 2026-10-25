// SPDX-License-Identifier: MIT

package opcuactl

import (
	"strings"
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

func TestConditionStates(t *testing.T) {
	if got := conditionStates(&model.Condition{}); got != "-" {
		t.Errorf("no states = %q", got)
	}
	c := &model.Condition{Suppressed: ptr(true), OutOfService: ptr(true), Silenced: ptr(false), Latched: ptr(true),
		DialogActive: ptr(true), FirstInGroup: ptr(true), ReAlarmRepeatCount: ptr(int16(2))}
	if got := conditionStates(c); got != "suppressed,out-of-service,latched,dialog,first-in-group,re-alarmed(2)" {
		t.Errorf("states = %q", got)
	}
	for _, s := range []string{"suppressed", "out-of-service", "latched", "dialog"} {
		if ok, err := conditionMatches(c, []string{s}, 0); !ok || err != nil {
			t.Errorf("--state %s: %v %v", s, ok, err)
		}
	}
	for _, s := range []string{"silenced", "branch"} {
		if ok, err := conditionMatches(c, []string{s}, 0); ok || err != nil {
			t.Errorf("--state %s: %v %v", s, ok, err)
		}
	}
	if derefInt16(nil) != 0 {
		t.Error("derefInt16(nil)")
	}
}

func TestSelectResponse(t *testing.T) {
	opts := []string{"Yes", "No"}
	for in, want := range map[string]int32{"0": 0, "1": 1, "yes": 0, " NO ": 1, "5": 5} {
		got, err := selectResponse(in, opts)
		if err != nil || got != want {
			t.Errorf("selectResponse(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	if _, err := selectResponse("maybe", opts); err == nil || !strings.Contains(err.Error(), "0=Yes, 1=No") {
		t.Errorf("maybe: %v", err)
	}
	if _, err := selectResponse("maybe", nil); err == nil || !strings.Contains(err.Error(), "it has none") {
		t.Errorf("no options: %v", err)
	}
}

func TestMethodName(t *testing.T) {
	for _, tt := range []struct {
		in   *ua.NodeID
		want string
	}{
		{nil, ""},
		{ua.NewNumericNodeID(0, id.AcknowledgeableConditionTypeAcknowledge), "Acknowledge"},
		{ua.NewNumericNodeID(0, id.Server), "Server"},
		{ua.NewNumericNodeID(0, 999999), "i=999999"},
		{ua.NewStringNodeID(2, "M"), "ns=2;s=M"},
	} {
		if got := methodName(tt.in); got != tt.want {
			t.Errorf("methodName(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestAuditModel(t *testing.T) {
	d := 90 * time.Second
	ev := &opcua.AuditConditionEvent{
		EventID: []byte{1}, EventType: ua.NewNumericNodeID(0, id.AuditConditionShelvingEventType),
		SourceNode: ua.NewStringNodeID(2, "A"), Time: time.Unix(1, 0), ActionTimeStamp: time.Unix(2, 0),
		MethodID: ua.NewNumericNodeID(0, id.ShelvedStateMachineTypeTimedShelve2), ConditionEventID: []byte{2},
		Comment: ua.NewLocalizedText("c"), SelectedResponse: ptr(int32(1)), ShelvingTime: &d, ClientAuditEntryID: "e",
	}
	m := auditModel(ev, time.Unix(3, 0))
	if m.EventType != "AuditConditionShelvingEventType" || m.ConditionID != "ns=2;s=A" || m.Method != "TimedShelve2" ||
		m.Comment != "c" || *m.ShelvingTimeMs != 90000 || m.ConditionEventID != "Ag==" || m.ActionTime == "" {
		t.Errorf("model = %+v", m)
	}
	out := captureStdout(t, func() { printAuditEvent(m, false) })
	wantContains(t, out, "TimedShelve2 refused", `comment="c"`, "response=1", "shelvingTime=1m30s", "auditEntryId=e")
	out = captureStdout(t, func() { printAuditEvent(m, true) })
	wantContains(t, out, `"method":"TimedShelve2"`)
	if evs, err := auditEvents(&opcua.PublishNotificationData{Value: &ua.DataChangeNotification{}}); evs != nil || err != nil {
		t.Errorf("data change: %v %v", evs, err)
	}
	if _, err := auditEvents(&opcua.PublishNotificationData{Value: &ua.EventNotificationList{Events: []*ua.EventFieldList{{}}}}); err == nil {
		t.Error("a short event list parsed")
	}
}
