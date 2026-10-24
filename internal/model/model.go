// SPDX-License-Identifier: MIT

package model

import "time"

// DiscoveredServer represents a discovered OPC UA server.
type DiscoveredServer struct {
	ApplicationURI      string   `json:"applicationUri"`
	ProductURI          string   `json:"productUri"`
	ApplicationName     string   `json:"applicationName"`
	ApplicationType     string   `json:"applicationType"`
	GatewayServerURI    string   `json:"gatewayServerUri,omitempty"`
	DiscoveryProfileURI string   `json:"discoveryProfileUri,omitempty"`
	DiscoveryURLs       []string `json:"discoveryUrls"`
}

// ServerOnNetwork represents a discovered server on the network.
type ServerOnNetwork struct {
	RecordID           uint32   `json:"recordId"`
	ServerName         string   `json:"serverName"`
	DiscoveryURL       string   `json:"discoveryUrl"`
	ServerCapabilities []string `json:"serverCapabilities"`
}

// DiscoveryResult is what a Local Discovery Server knows.
type DiscoveryResult struct {
	DiscoveryServerURL string             `json:"discoveryServerUrl"`
	MulticastSupported bool               `json:"multicastSupported"`
	Servers            []DiscoveredServer `json:"servers"`
	ServersOnNetwork   []ServerOnNetwork  `json:"serversOnNetwork"`
}

// Endpoint represents a server endpoint.
type Endpoint struct {
	EndpointURL       string            `json:"endpointUrl"`
	SecurityPolicy    string            `json:"securityPolicy"`
	SecurityMode      string            `json:"securityMode"`
	SecurityLevel     uint8             `json:"securityLevel"`
	HasCertificate    bool              `json:"hasCertificate"`
	UserTokenPolicies []UserTokenPolicy `json:"userTokenPolicies"`
	TransportProfile  string            `json:"transportProfile,omitempty"`
	ApplicationURI    string            `json:"applicationUri,omitempty"`
	CertThumbprint    string            `json:"certThumbprint,omitempty"`
	URLMismatch       string            `json:"urlMismatch,omitempty"`
}

// UserTokenPolicy represents a user token policy.
type UserTokenPolicy struct {
	PolicyID  string `json:"policyId"`
	TokenType string `json:"tokenType"`
}

// NodeValue represents a read value result.
type NodeValue struct {
	NodeID          string      `json:"nodeId"`
	Value           interface{} `json:"value"`
	Type            string      `json:"type,omitempty"`
	Status          string      `json:"status"`
	StatusCode      uint32      `json:"statusCode"`
	SourceTimestamp string      `json:"sourceTimestamp,omitempty"`
	ServerTimestamp string      `json:"serverTimestamp,omitempty"`
}

// NodeInfo represents a node summary.
type NodeInfo struct {
	NodeID               string      `json:"nodeId"`
	NodeClass            string      `json:"nodeClass"`
	BrowseName           string      `json:"browseName"`
	DisplayName          string      `json:"displayName"`
	Description          string      `json:"description,omitempty"`
	DataType             string      `json:"dataType,omitempty"`
	AccessLevel          string      `json:"accessLevel,omitempty"`
	AccessInterpretation string      `json:"accessInterpretation,omitempty"`
	UserAccessLevel      string      `json:"userAccessLevel,omitempty"`
	TypeDefinition       string      `json:"typeDefinition,omitempty"`
	Value                interface{} `json:"value,omitempty"`
	ValueStatus          string      `json:"valueStatus,omitempty"`
	WriteMask            string      `json:"writeMask,omitempty"`
	ValueRank            string      `json:"valueRank,omitempty"`
	ArrayDimensions      string      `json:"arrayDimensions,omitempty"`
	MinSamplingInterval  string      `json:"minSamplingInterval,omitempty"`
	Historizing          string      `json:"historizing,omitempty"`
}

// Reference represents a node reference edge.
type Reference struct {
	ReferenceType  string `json:"referenceType"`
	IsForward      bool   `json:"isForward"`
	NodeID         string `json:"nodeId"`
	BrowseName     string `json:"browseName"`
	DisplayName    string `json:"displayName"`
	NodeClass      string `json:"nodeClass"`
	TypeDefinition string `json:"typeDefinition,omitempty"`
}

// ChildNode represents a child node in browse results.
type ChildNode struct {
	NodeID         string `json:"nodeId"`
	BrowseName     string `json:"browseName"`
	DisplayName    string `json:"displayName"`
	NodeClass      string `json:"nodeClass"`
	TypeDefinition string `json:"typeDefinition,omitempty"`
}

// ServerStatus represents server metadata.
type ServerStatus struct {
	StartTime           string     `json:"startTime"`
	CurrentTime         string     `json:"currentTime"`
	State               string     `json:"state"`
	SecondsTillShutdown uint32     `json:"secondsTillShutdown,omitempty"`
	ShutdownReason      string     `json:"shutdownReason,omitempty"`
	BuildInfo           *BuildInfo `json:"buildInfo,omitempty"`
	SecurityPolicy      string     `json:"securityPolicy,omitempty"`
	SecurityMode        string     `json:"securityMode,omitempty"`
}

// BuildInfo represents server build information.
type BuildInfo struct {
	ProductURI       string `json:"productUri"`
	ManufacturerName string `json:"manufacturerName"`
	ProductName      string `json:"productName"`
	SoftwareVersion  string `json:"softwareVersion"`
	BuildNumber      string `json:"buildNumber"`
	BuildDate        string `json:"buildDate"`
}

// FormatTimestamp returns an RFC3339Nano timestamp, or empty if zero.
func FormatTimestamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339Nano)
}

// TreeNode represents a node in a browse tree.
type TreeNode struct {
	NodeID         string     `json:"nodeId"`
	BrowseName     string     `json:"browseName"`
	DisplayName    string     `json:"displayName"`
	NodeClass      string     `json:"nodeClass"`
	TypeDefinition string     `json:"typeDefinition,omitempty"`
	Depth          int        `json:"depth"`
	Children       []TreeNode `json:"children,omitempty"`
}

// NamespaceEntry represents a namespace in the server's namespace array.
type NamespaceEntry struct {
	Index int    `json:"index"`
	URI   string `json:"uri"`
}

// WriteResult represents the result of a write operation.
type WriteResult struct {
	NodeID     string `json:"nodeId"`
	Status     string `json:"status"`
	StatusCode uint32 `json:"statusCode"`
}

// DryRunResult represents enriched dry-run output for a write operation.
type DryRunResult struct {
	NodeID       string      `json:"nodeId"`
	DisplayName  string      `json:"displayName,omitempty"`
	DataType     string      `json:"dataType,omitempty"`
	AccessLevel  string      `json:"accessLevel,omitempty"`
	CurrentValue interface{} `json:"currentValue,omitempty"`
	NewValue     interface{} `json:"newValue"`
	NewType      string      `json:"newType"`
	Status       string      `json:"status"`
}

// MethodArgument represents a method input/output argument definition.
type MethodArgument struct {
	Name        string `json:"name"`
	DataType    string `json:"dataType"`
	ValueRank   int32  `json:"valueRank"`
	Description string `json:"description,omitempty"`
}

// CallResult represents the result of a method call.
type CallResult struct {
	ObjectID        string        `json:"objectId"`
	MethodID        string        `json:"methodId"`
	Status          string        `json:"status"`
	StatusCode      uint32        `json:"statusCode"`
	OutputArguments []interface{} `json:"outputArguments,omitempty"`
}

// MonitoredValue represents a single data-change notification.
type MonitoredValue struct {
	Timestamp       string      `json:"ts"`
	NodeID          string      `json:"nodeId"`
	Value           interface{} `json:"value"`
	Type            string      `json:"type,omitempty"`
	Status          string      `json:"status"`
	StatusCode      uint32      `json:"statusCode"`
	Overflow        bool        `json:"overflow,omitempty"`
	SourceTimestamp string      `json:"sourceTimestamp,omitempty"`
	ServerTimestamp string      `json:"serverTimestamp,omitempty"`
}

// MonitoredEvent represents a single event notification.
type MonitoredEvent struct {
	Timestamp string                 `json:"ts"`
	NodeID    string                 `json:"nodeId,omitempty"`
	Fields    map[string]interface{} `json:"fields"`
}

// PingStep represents the result of a single ping stage.
type PingStep struct {
	Stage   string `json:"stage"`
	Status  string `json:"status"`
	Latency string `json:"latency"`
	Error   string `json:"error,omitempty"`
}

// HistoryValue represents a single historical data point.
type HistoryValue struct {
	NodeID          string      `json:"nodeId"`
	Value           interface{} `json:"value"`
	Type            string      `json:"type,omitempty"`
	Status          string      `json:"status"`
	StatusCode      uint32      `json:"statusCode"`
	SourceTimestamp string      `json:"sourceTimestamp"`
	ServerTimestamp string      `json:"serverTimestamp,omitempty"`
}

// BrowsePathResult represents a resolved browse path.
type BrowsePathResult struct {
	Path    string `json:"path"`
	NodeID  string `json:"nodeId"`
	Message string `json:"message,omitempty"`
}

// HistoryEventRecord represents a single historical event entry.
type HistoryEventRecord struct {
	NodeID string                 `json:"nodeId"`
	Fields map[string]interface{} `json:"fields"`
}

// ExportedValue represents a snapshot or historical value export entry.
type ExportedValue struct {
	NodeID          string      `json:"nodeId"`
	Value           interface{} `json:"value"`
	Type            string      `json:"type,omitempty"`
	Status          string      `json:"status"`
	StatusCode      uint32      `json:"statusCode"`
	SourceTimestamp string      `json:"sourceTimestamp,omitempty"`
	ServerTimestamp string      `json:"serverTimestamp,omitempty"`
}

// ExportedNode represents a node in an export result.
type ExportedNode struct {
	NodeID          string `json:"nodeId"`
	NodeClass       string `json:"nodeClass"`
	BrowseName      string `json:"browseName"`
	DisplayName     string `json:"displayName"`
	Description     string `json:"description,omitempty"`
	DataType        string `json:"dataType,omitempty"`
	AccessLevel     string `json:"accessLevel,omitempty"`
	UserAccessLevel string `json:"userAccessLevel,omitempty"`
	TypeDefinition  string `json:"typeDefinition,omitempty"`
	WriteMask       string `json:"writeMask,omitempty"`
	ValueRank       string `json:"valueRank,omitempty"`
	ArrayDimensions string `json:"arrayDimensions,omitempty"`
	Depth           int    `json:"depth"`
}

// VarNode represents a variable node collected by get vars.
type VarNode struct {
	Path           string      `json:"path,omitempty"`
	NodeID         string      `json:"nodeId"`
	BrowseName     string      `json:"browseName"`
	DisplayName    string      `json:"displayName"`
	TypeDefinition string      `json:"typeDefinition,omitempty"`
	DataType       string      `json:"dataType,omitempty"`
	Value          interface{} `json:"value,omitempty"`
	Status         string      `json:"status,omitempty"`
}

// PubSubReceived tells how a PubSub NetworkMessage arrived: when, from
// where, and the message headers of the transport (MQTT 5.0, AMQP).
type PubSubReceived struct {
	Received string `json:"ts,omitempty"`
	Source   string `json:"source,omitempty"`
	Topic    string `json:"topic,omitempty"`
	// ContentType is the MIME type the transport carried
	// ("application/opcua+uadp", "application/json").
	ContentType string `json:"contentType,omitempty"`
	// UAMessageType is "ua-data", "ua-metadata", ... (MQTT 5.0 user
	// property, AMQP subject).
	UAMessageType string `json:"uaMessageType,omitempty"`
	// UserProperties are the other message properties, in order.
	UserProperties []PubSubProperty `json:"userProperties,omitempty"`
}

// PubSubProperty is a name and value pair of a message header.
type PubSubProperty struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// PubSubDataSet is one decoded DataSetMessage of a PubSub NetworkMessage.
type PubSubDataSet struct {
	PubSubReceived
	Encoding          string `json:"encoding"`
	PublisherID       string `json:"publisherId"`
	WriterGroupID     uint16 `json:"writerGroupId,omitempty"`
	WriterGroupName   string `json:"writerGroupName,omitempty"`
	DataSetWriterID   uint16 `json:"dataSetWriterId"`
	DataSetWriterName string `json:"dataSetWriterName,omitempty"`
	MessageType       string `json:"messageType"`
	SequenceNumber    uint32 `json:"sequenceNumber"`
	Timestamp         string `json:"timestamp,omitempty"`
	Status            string `json:"status,omitempty"`
	MetaDataVersion   string `json:"metaDataVersion,omitempty"`
	Security          string `json:"security"`
	SecurityTokenID   uint32 `json:"securityTokenId,omitempty"`
	// Action is set for the DataSetMessage of a PubSub Action request or
	// response; the Status of a response is the result of the Action.
	Action *PubSubAction `json:"action,omitempty"`
	Fields []PubSubField `json:"fields"`
}

// PubSubAction is the action header of a PubSub Action request or response
// message (Part 14 §7.2.4.4.2 and §7.2.4.5.9 in 1.05).
type PubSubAction struct {
	Type            string `json:"type"` // request or response
	ActionTargetID  uint16 `json:"actionTargetId"`
	RequestID       uint16 `json:"requestId"`
	ActionState     string `json:"actionState"`
	RequestorID     string `json:"requestorId,omitempty"`
	ResponseAddress string `json:"responseAddress,omitempty"`
	CorrelationData string `json:"correlationData,omitempty"` // hex
	// TimeoutHint is in milliseconds (requests).
	TimeoutHint float64 `json:"timeoutHint,omitempty"`
}

// PubSubActionResult is the result of a PubSub Action call: the Done
// response of the Responder.
type PubSubActionResult struct {
	PublisherID     string        `json:"publisherId"`
	DataSetWriterID uint16        `json:"dataSetWriterId"`
	ActionTargetID  uint16        `json:"actionTargetId"`
	RequestID       uint16        `json:"requestId"`
	Status          string        `json:"status"`
	StatusCode      uint32        `json:"statusCode"`
	Duration        string        `json:"duration"`
	Outputs         []PubSubField `json:"outputs"`
}

// PubSubActionRequest is a PubSub Action request a Responder served, and
// its result.
type PubSubActionRequest struct {
	Timestamp       string        `json:"ts"`
	RequestorID     string        `json:"requestorId,omitempty"`
	DataSetWriterID uint16        `json:"dataSetWriterId"`
	ActionTargetID  uint16        `json:"actionTargetId"`
	RequestID       uint16        `json:"requestId"`
	Inputs          []PubSubField `json:"inputs"`
	Status          string        `json:"status"`
	StatusCode      uint32        `json:"statusCode"`
	Outputs         []PubSubField `json:"outputs"`
}

// PubSubField is one field of a DataSetMessage.
type PubSubField struct {
	Index           uint16      `json:"index"`
	Name            string      `json:"name,omitempty"`
	Value           interface{} `json:"value"`
	Type            string      `json:"type,omitempty"`
	Status          string      `json:"status,omitempty"`
	SourceTimestamp string      `json:"sourceTimestamp,omitempty"`
}

// PubSubMetaData is a DataSetMetaData announcement seen on the wire.
type PubSubMetaData struct {
	PubSubReceived
	Encoding        string            `json:"encoding"`
	PublisherID     string            `json:"publisherId"`
	DataSetWriterID uint16            `json:"dataSetWriterId"`
	Name            string            `json:"name"`
	Version         string            `json:"version,omitempty"`
	Fields          []PubSubFieldMeta `json:"fields"`
}

// PubSubFieldMeta describes one field of a DataSet.
type PubSubFieldMeta struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	ValueRank int32  `json:"valueRank,omitempty"`
}

// SecurityKeys is the result of GetSecurityKeys. Key material is only
// included when asked for.
type SecurityKeys struct {
	SecurityGroupID   string        `json:"securityGroupId"`
	SecurityPolicyURI string        `json:"securityPolicyUri"`
	FirstTokenID      uint32        `json:"firstTokenId"`
	TimeToNextKey     string        `json:"timeToNextKey"`
	KeyLifetime       string        `json:"keyLifetime"`
	Keys              []SecurityKey `json:"keys"`
}

// SecurityKey is one key of a SecurityGroup.
type SecurityKey struct {
	TokenID uint32 `json:"tokenId"`
	Length  int    `json:"length"`
	Key     string `json:"key,omitempty"` // base64
}

// PubSubComponent is a component of the PublishSubscribe configuration of
// a server.
type PubSubComponent struct {
	Kind       string            `json:"kind"`
	Name       string            `json:"name"`
	NodeID     string            `json:"nodeId"`
	State      string            `json:"state,omitempty"`
	Properties map[string]string `json:"properties,omitempty"`
	Children   []PubSubComponent `json:"children,omitempty"`
}

// PubSubAdded is the result of a PubSub configuration method that adds a
// component.
type PubSubAdded struct {
	Kind    string         `json:"kind"`
	Name    string         `json:"name"`
	NodeID  string         `json:"nodeId"`
	Results []PubSubResult `json:"results,omitempty"`
}

// PubSubDiagnostics are the diagnostics counters of a PubSub component
// (Part 14 §9.1.11): of a server's component, or of what a pubsub command
// sent and received.
type PubSubDiagnostics struct {
	Component        string                 `json:"component"`
	Name             string                 `json:"name,omitempty"`
	NodeID           string                 `json:"nodeId,omitempty"`
	Level            string                 `json:"diagnosticsLevel"`
	TotalInformation uint64                 `json:"totalInformation"`
	TotalError       uint64                 `json:"totalError"`
	SubError         bool                   `json:"subError,omitempty"`
	Counters         []PubSubCounter        `json:"counters"`
	LiveValues       map[string]interface{} `json:"liveValues,omitempty"`
}

// PubSubCounter is one diagnostics counter (PubSubDiagnosticsCounterType).
type PubSubCounter struct {
	Name            string `json:"name"`
	Value           uint64 `json:"value"`
	Classification  string `json:"classification"` // Information or Error
	Level           string `json:"diagnosticsLevel"`
	Active          bool   `json:"active"`
	TimeFirstChange string `json:"timeFirstChange,omitempty"`
}

// PubSubAnnouncement is a UADP discovery announcement (Part 14 §7.2.4.6)
// other than DataSetMetaData, which is a PubSubMetaData.
type PubSubAnnouncement struct {
	PubSubReceived
	PublisherID    string                 `json:"publisherId"`
	Type           string                 `json:"announcement"`
	SequenceNumber uint16                 `json:"sequenceNumber"`
	Status         string                 `json:"status,omitempty"`
	Details        map[string]interface{} `json:"details,omitempty"`
}

// PubSubResult is the per-variable result of AddPublishedDataItems.
type PubSubResult struct {
	Field      string `json:"field"`
	NodeID     string `json:"nodeId"`
	Status     string `json:"status"`
	StatusCode uint32 `json:"statusCode"`
}
