// SPDX-License-Identifier: MIT

package model

// DiagnosticsSummary is Server.ServerDiagnostics.ServerDiagnosticsSummary
// (Part 5 §6.3.3) with the EnabledFlag.
type DiagnosticsSummary struct {
	Enabled                       bool   `json:"enabled"`
	ServerViewCount               uint32 `json:"serverViewCount"`
	CurrentSessionCount           uint32 `json:"currentSessionCount"`
	CumulatedSessionCount         uint32 `json:"cumulatedSessionCount"`
	SecurityRejectedSessionCount  uint32 `json:"securityRejectedSessionCount"`
	RejectedSessionCount          uint32 `json:"rejectedSessionCount"`
	SessionTimeoutCount           uint32 `json:"sessionTimeoutCount"`
	SessionAbortCount             uint32 `json:"sessionAbortCount"`
	CurrentSubscriptionCount      uint32 `json:"currentSubscriptionCount"`
	CumulatedSubscriptionCount    uint32 `json:"cumulatedSubscriptionCount"`
	PublishingIntervalCount       uint32 `json:"publishingIntervalCount"`
	SecurityRejectedRequestsCount uint32 `json:"securityRejectedRequestsCount"`
	RejectedRequestsCount         uint32 `json:"rejectedRequestsCount"`
}

// ServiceCounter counts the requests of a service and how many failed.
type ServiceCounter struct {
	Total  uint32 `json:"total"`
	Errors uint32 `json:"errors"`
}

// SessionDiagnostics is a SessionDiagnosticsDataType. Services holds the
// per-service counters that are not zero.
type SessionDiagnostics struct {
	SessionID                     string                    `json:"sessionId"`
	SessionName                   string                    `json:"sessionName"`
	ClientName                    string                    `json:"clientName,omitempty"`
	ClientURI                     string                    `json:"clientUri,omitempty"`
	EndpointURL                   string                    `json:"endpointUrl,omitempty"`
	LocaleIDs                     []string                  `json:"localeIds,omitempty"`
	ActualSessionTimeoutMs        float64                   `json:"actualSessionTimeoutMs"`
	MaxResponseMessageSize        uint32                    `json:"maxResponseMessageSize"`
	ClientConnectionTime          string                    `json:"clientConnectionTime,omitempty"`
	ClientLastContactTime         string                    `json:"clientLastContactTime,omitempty"`
	CurrentSubscriptionsCount     uint32                    `json:"currentSubscriptionsCount"`
	CurrentMonitoredItemsCount    uint32                    `json:"currentMonitoredItemsCount"`
	CurrentPublishRequestsInQueue uint32                    `json:"currentPublishRequestsInQueue"`
	TotalRequestCount             ServiceCounter            `json:"totalRequestCount"`
	UnauthorizedRequestCount      uint32                    `json:"unauthorizedRequestCount"`
	Services                      map[string]ServiceCounter `json:"services,omitempty"`
}

// SessionSecurityDiagnostics is a SessionSecurityDiagnosticsDataType; the
// client certificate is given by its SHA-1 thumbprint.
type SessionSecurityDiagnostics struct {
	SessionID                   string   `json:"sessionId"`
	ClientUserID                string   `json:"clientUserId"`
	ClientUserIDHistory         []string `json:"clientUserIdHistory,omitempty"`
	AuthenticationMechanism     string   `json:"authenticationMechanism"`
	Encoding                    string   `json:"encoding"`
	TransportProtocol           string   `json:"transportProtocol"`
	SecurityMode                string   `json:"securityMode"`
	SecurityPolicy              string   `json:"securityPolicy"`
	ClientCertificateThumbprint string   `json:"clientCertificateThumbprint,omitempty"`
}

// SubscriptionDiagnostics is a SubscriptionDiagnosticsDataType.
type SubscriptionDiagnostics struct {
	SessionID                    string  `json:"sessionId"`
	SubscriptionID               uint32  `json:"subscriptionId"`
	Priority                     uint8   `json:"priority"`
	PublishingIntervalMs         float64 `json:"publishingIntervalMs"`
	MaxKeepAliveCount            uint32  `json:"maxKeepAliveCount"`
	MaxLifetimeCount             uint32  `json:"maxLifetimeCount"`
	MaxNotificationsPerPublish   uint32  `json:"maxNotificationsPerPublish"`
	PublishingEnabled            bool    `json:"publishingEnabled"`
	ModifyCount                  uint32  `json:"modifyCount"`
	EnableCount                  uint32  `json:"enableCount"`
	DisableCount                 uint32  `json:"disableCount"`
	RepublishRequestCount        uint32  `json:"republishRequestCount"`
	RepublishMessageRequestCount uint32  `json:"republishMessageRequestCount"`
	RepublishMessageCount        uint32  `json:"republishMessageCount"`
	TransferRequestCount         uint32  `json:"transferRequestCount"`
	TransferredToAltClientCount  uint32  `json:"transferredToAltClientCount"`
	TransferredToSameClientCount uint32  `json:"transferredToSameClientCount"`
	PublishRequestCount          uint32  `json:"publishRequestCount"`
	DataChangeNotificationsCount uint32  `json:"dataChangeNotificationsCount"`
	EventNotificationsCount      uint32  `json:"eventNotificationsCount"`
	NotificationsCount           uint32  `json:"notificationsCount"`
	LatePublishRequestCount      uint32  `json:"latePublishRequestCount"`
	CurrentKeepAliveCount        uint32  `json:"currentKeepAliveCount"`
	CurrentLifetimeCount         uint32  `json:"currentLifetimeCount"`
	UnacknowledgedMessageCount   uint32  `json:"unacknowledgedMessageCount"`
	DiscardedMessageCount        uint32  `json:"discardedMessageCount"`
	MonitoredItemCount           uint32  `json:"monitoredItemCount"`
	DisabledMonitoredItemCount   uint32  `json:"disabledMonitoredItemCount"`
	MonitoringQueueOverflowCount uint32  `json:"monitoringQueueOverflowCount"`
	NextSequenceNumber           uint32  `json:"nextSequenceNumber"`
	EventQueueOverflowCount      uint32  `json:"eventQueueOverflowCount"`
}

// SamplingIntervalDiagnostics is a SamplingIntervalDiagnosticsDataType.
type SamplingIntervalDiagnostics struct {
	SamplingIntervalMs         float64 `json:"samplingIntervalMs"`
	MonitoredItemCount         uint32  `json:"monitoredItemCount"`
	MaxMonitoredItemCount      uint32  `json:"maxMonitoredItemCount"`
	DisabledMonitoredItemCount uint32  `json:"disabledMonitoredItemCount"`
}
