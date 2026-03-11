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
	NodeID          string      `json:"nodeId"`
	NodeClass       string      `json:"nodeClass"`
	BrowseName      string      `json:"browseName"`
	DisplayName     string      `json:"displayName"`
	Description     string      `json:"description,omitempty"`
	DataType        string      `json:"dataType,omitempty"`
	AccessLevel     string      `json:"accessLevel,omitempty"`
	UserAccessLevel string      `json:"userAccessLevel,omitempty"`
	TypeDefinition  string      `json:"typeDefinition,omitempty"`
	Value           interface{} `json:"value,omitempty"`
	ValueStatus     string      `json:"valueStatus,omitempty"`
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
