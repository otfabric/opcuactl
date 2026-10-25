// SPDX-License-Identifier: MIT

package model

// Condition is the state of an OPC UA condition (Part 9) as its latest
// condition event reports it. The states an instance does not have (a
// ConditionType has no AckedState, an alarm without ConfirmedState) are
// omitted.
type Condition struct {
	ConditionID         string `json:"conditionId"`
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
// AddComment, Enable, Disable, TimedShelve, OneShotShelve, Unshelve).
type AlarmAction struct {
	ConditionID string `json:"conditionId"`
	Action      string `json:"action"`
	EventID     string `json:"eventId,omitempty"`
	Comment     string `json:"comment,omitempty"`
	ShelvingMs  int64  `json:"shelvingTimeMs,omitempty"`
	Status      string `json:"status"`
}
