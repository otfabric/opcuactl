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
