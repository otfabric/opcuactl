// SPDX-License-Identifier: MIT

package model

// Condition is the state of an OPC UA condition (Part 9) as its latest
// condition event reports it. The states an instance does not have (a
// ConditionType has no AckedState, an alarm without ConfirmedState) are
// omitted.
type Condition struct {
	ConditionID         string `json:"conditionId"`
	BranchID            string `json:"branchId,omitempty"` // a branch of the condition (Part 9 §5.5.2)
	ConditionName       string `json:"conditionName,omitempty"`
	SourceNode          string `json:"sourceNode,omitempty"`
	SourceName          string `json:"sourceName,omitempty"`
	EventType           string `json:"eventType,omitempty"`
	Severity            uint16 `json:"severity"`
	Message             string `json:"message,omitempty"`
	Time                string `json:"time,omitempty"`
	Retain              bool   `json:"retain"`
	Enabled             bool   `json:"enabled"`
	Active              *bool  `json:"active,omitempty"`
	Acked               *bool  `json:"acked,omitempty"`
	Confirmed           *bool  `json:"confirmed,omitempty"`
	Shelving            string `json:"shelving,omitempty"` // Unshelved, TimedShelved or OneShotShelved
	SuppressedOrShelved *bool  `json:"suppressedOrShelved,omitempty"`
	Suppressed          *bool  `json:"suppressed,omitempty"`
	OutOfService        *bool  `json:"outOfService,omitempty"`
	Silenced            *bool  `json:"silenced,omitempty"`
	Latched             *bool  `json:"latched,omitempty"`
	DialogActive        *bool  `json:"dialogActive,omitempty"`
	Prompt              string `json:"prompt,omitempty"`
	LastResponse        *int32 `json:"lastResponse,omitempty"`
	AudibleEnabled      *bool  `json:"audibleEnabled,omitempty"`
	ReAlarmRepeatCount  *int16 `json:"reAlarmRepeatCount,omitempty"`
	FirstInGroup        *bool  `json:"firstInGroup,omitempty"`
	Quality             string `json:"quality,omitempty"`
	Comment             string `json:"comment,omitempty"`
	ClientUserID        string `json:"clientUserId,omitempty"`
	EventID             string `json:"eventId"` // base64
}

// ConditionNotification is one line of alarms watch: a condition event, or
// the start or end of a ConditionRefresh (Refresh "start" / "end").
type ConditionNotification struct {
	Timestamp string `json:"ts"`
	Refresh   string `json:"refresh,omitempty"`
	*Condition
}

// AlarmAction is the result of a condition method (Acknowledge, Confirm,
// AddComment, Enable, Disable, the shelving, suppression, service, silence
// and reset methods, Respond).
type AlarmAction struct {
	ConditionID      string `json:"conditionId"`
	Action           string `json:"action"`
	EventID          string `json:"eventId,omitempty"`
	Comment          string `json:"comment,omitempty"`
	ShelvingMs       int64  `json:"shelvingTimeMs,omitempty"`
	SelectedResponse *int32 `json:"selectedResponse,omitempty"`
	Response         string `json:"response,omitempty"` // the text of the selected response option
	AuditEntryID     string `json:"auditEntryId,omitempty"`
	Status           string `json:"status"`
}

// AlarmGroup is an alarm group (AlarmGroupType) a condition is a member of.
type AlarmGroup struct {
	NodeID      string `json:"nodeId"`
	BrowseName  string `json:"browseName,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
}

// AlarmMetrics are the values of an AlarmMetricsType object (Part 9 1.05).
type AlarmMetrics struct {
	NodeID              string  `json:"nodeId"`
	AlarmCount          uint32  `json:"alarmCount"`
	StartTime           string  `json:"startTime,omitempty"`
	MaximumActiveStateS float64 `json:"maximumActiveStateSeconds"`
	MaximumUnAckS       float64 `json:"maximumUnAckSeconds"`
	MaximumReAlarmCount uint32  `json:"maximumReAlarmCount"`
	CurrentAlarmRate    float64 `json:"currentAlarmRate"`
	MaximumAlarmRate    float64 `json:"maximumAlarmRate"`
	AverageAlarmRate    float64 `json:"averageAlarmRate"`
	RateMinutes         uint16  `json:"rateMinutes"`
}

// AuditEvent is an audit event of a condition method (AuditConditionEventType
// and its subtypes, Part 9 §5.10): who called which method of which
// condition, and whether the server accepted the call.
type AuditEvent struct {
	Timestamp          string `json:"ts"`
	EventType          string `json:"eventType"`
	ConditionID        string `json:"conditionId,omitempty"`
	SourceName         string `json:"sourceName,omitempty"`
	Time               string `json:"time,omitempty"`
	ActionTime         string `json:"actionTime,omitempty"`
	Status             bool   `json:"status"`
	Method             string `json:"method,omitempty"`
	ClientUserID       string `json:"clientUserId,omitempty"`
	ClientAuditEntryID string `json:"clientAuditEntryId,omitempty"`
	ServerID           string `json:"serverId,omitempty"`
	ConditionEventID   string `json:"conditionEventId,omitempty"` // base64
	Comment            string `json:"comment,omitempty"`
	SelectedResponse   *int32 `json:"selectedResponse,omitempty"`
	ShelvingTimeMs     *int64 `json:"shelvingTimeMs,omitempty"`
	Message            string `json:"message,omitempty"`
	EventID            string `json:"eventId"` // base64
}
