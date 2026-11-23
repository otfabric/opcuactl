// SPDX-License-Identifier: MIT

package model

// FxReport is what fx inspect found of the OPC UA FX information model
// (OPC 10000-81) on a server.
type FxReport struct {
	// HasFxNamespaces: the server has the namespaces Part 81 §14.2 makes
	// mandatory (DI, UAFX Data, and UAFX AC or CM).
	HasFxNamespaces bool `json:"hasFxNamespaces"`
	// IsFxServer: those namespaces and FxRoot (Part 81 §12.2). Not a
	// conformance statement.
	IsFxServer           bool                    `json:"isFxServer"`
	Namespaces           []FxNamespace           `json:"namespaces"`
	ServerCapabilities   []string                `json:"serverCapabilities,omitempty"`
	FxRoot               string                  `json:"fxRoot,omitempty"`
	AutomationComponents []FxAutomationComponent `json:"automationComponents"`
	ConnectionManagers   []FxConnectionManager   `json:"connectionManagers"`
	Findings             []FxFinding             `json:"findings"`
	// Truncated: the server holds more than the limits let fx inspect read.
	Truncated bool `json:"truncated"`
}

// FxNamespace is one namespace of the UAFX model on a server.
type FxNamespace struct {
	URI             string `json:"uri"`
	Present         bool   `json:"present"`
	Index           uint16 `json:"index,omitempty"`
	Version         string `json:"version,omitempty"`
	PublicationDate string `json:"publicationDate,omitempty"`
}

// FxNode is what the report holds of every node.
type FxNode struct {
	NodeID         string `json:"nodeId"`
	BrowseName     string `json:"browseName"`
	DisplayName    string `json:"displayName,omitempty"`
	TypeDefinition string `json:"typeDefinition,omitempty"`
}

// FxAutomationComponent is an AutomationComponent (Part 81 §6.2.2).
type FxAutomationComponent struct {
	FxNode             `yaml:",inline"`
	ConformanceName    string               `json:"conformanceName,omitempty"`
	AggregatedHealth   *FxHealth            `json:"aggregatedHealth,omitempty"`
	Assets             []FxAsset            `json:"assets"`
	FunctionalEntities []FxFunctionalEntity `json:"functionalEntities"`
	Descriptors        []FxNode             `json:"descriptors,omitempty"`
}

// FxHealth is an AggregatedHealthDataType (Part 81 §10.1): the option sets
// as names ("OK" when no bit is set) and as numbers.
type FxHealth struct {
	DeviceHealth          string `json:"deviceHealth"`
	DeviceHealthBits      uint16 `json:"deviceHealthBits"`
	OperationalHealth     string `json:"operationalHealth"`
	OperationalHealthBits uint32 `json:"operationalHealthBits"`
}

// FxAsset is an Asset (Part 81 §6.3.2) with its nameplate.
type FxAsset struct {
	FxNode             `yaml:",inline"`
	Manufacturer       string `json:"manufacturer,omitempty"`
	ManufacturerURI    string `json:"manufacturerUri,omitempty"`
	Model              string `json:"model,omitempty"`
	ProductCode        string `json:"productCode,omitempty"`
	HardwareRevision   string `json:"hardwareRevision,omitempty"`
	SoftwareRevision   string `json:"softwareRevision,omitempty"`
	DeviceRevision     string `json:"deviceRevision,omitempty"`
	SerialNumber       string `json:"serialNumber,omitempty"`
	ProductInstanceURI string `json:"productInstanceUri,omitempty"`
	AssetID            string `json:"assetId,omitempty"`
	ComponentName      string `json:"componentName,omitempty"`
	DeviceClass        string `json:"deviceClass,omitempty"`
}

// FxFunctionalEntity is a FunctionalEntity (Part 81 §6.4.2).
type FxFunctionalEntity struct {
	FxNode                   `yaml:",inline"`
	AuthorURI                string                 `json:"authorUri,omitempty"`
	AuthorAssignedIdentifier string                 `json:"authorAssignedIdentifier,omitempty"`
	AuthorAssignedVersion    string                 `json:"authorAssignedVersion,omitempty"`
	OperationalHealth        string                 `json:"operationalHealth,omitempty"`
	OperationalHealthBits    *uint32                `json:"operationalHealthBits,omitempty"`
	ConnectionEndpoints      []FxConnectionEndpoint `json:"connectionEndpoints"`
	SubFunctionalEntities    []FxFunctionalEntity   `json:"subFunctionalEntities,omitempty"`
}

// FxConnectionEndpoint is a ConnectionEndpoint (Part 81 §6.6.2).
type FxConnectionEndpoint struct {
	FxNode `yaml:",inline"`
	// Status is the ConnectionEndpointStatusEnum (Part 81 §10.17), ""
	// when it cannot be read.
	Status                          string `json:"status,omitempty"`
	IsPersistent                    bool   `json:"isPersistent"`
	CleanupTimeoutMs                int64  `json:"cleanupTimeoutMs"`
	ConnectionManagerApplicationURI string `json:"connectionManagerApplicationUri,omitempty"`
}

// FxConnectionManager is a ConnectionManager (Part 81 §6.7.2).
type FxConnectionManager struct {
	FxNode                      `yaml:",inline"`
	ConnectionConfigurationSets []FxNode `json:"connectionConfigurationSets"`
}

// FxFinding is a deviation from Part 81 that fx inspect found.
type FxFinding struct {
	NodeID string `json:"nodeId,omitempty"`
	Path   string `json:"path,omitempty"`
	Clause string `json:"clause"`
	Text   string `json:"text"`
}

// FxConnectResult is the result of fx connect: the status of the
// EstablishConnections call (Part 81 §6.2.4) and its output arguments.
// StatusCodes are their symbols ("Good", "BadNothingToDo").
type FxConnectResult struct {
	AutomationComponent string `json:"automationComponent"`
	Commands            string `json:"commands"`
	// Status is Good, or Uncertain when a command failed and the server
	// aborted the call and undid what it had done (§6.2.4.3.11).
	Status                      string                        `json:"status"`
	StatusCode                  uint32                        `json:"statusCode"`
	AssetVerifications          []FxAssetVerificationResult   `json:"assetVerifications"`
	Endpoints                   []FxEndpointResult            `json:"endpoints"`
	ReserveCommunicationIDs     []FxReservedIDs               `json:"reserveCommunicationIds"`
	CommunicationConfigurations []FxCommunicationConfigResult `json:"communicationConfigurations"`
}

// FxAssetVerificationResult is an AssetVerificationResultDataType (Part 81
// §10.6).
type FxAssetVerificationResult struct {
	Status                   string   `json:"status"`
	Result                   string   `json:"result"`
	VariableErrors           []string `json:"variableErrors,omitempty"`
	AdditionalVariableErrors []string `json:"additionalVariableErrors,omitempty"`
}

// FxEndpointResult is a ConnectionEndpointConfigurationResultDataType
// (Part 81 §10.15).
type FxEndpointResult struct {
	// ConnectionEndpoint is the NodeId of the endpoint created or used,
	// "" when there is none.
	ConnectionEndpoint        string   `json:"connectionEndpoint,omitempty"`
	FunctionalEntityResult    string   `json:"functionalEntityResult"`
	ConnectionEndpointResult  string   `json:"connectionEndpointResult"`
	VerificationResult        string   `json:"verificationResult"`
	VerificationStatus        string   `json:"verificationStatus"`
	VerificationErrors        []string `json:"verificationErrors,omitempty"`
	EstablishControlResults   []string `json:"establishControlResults,omitempty"`
	ConfigurationDataResults  []string `json:"configurationDataResults,omitempty"`
	ReassignControlResults    []string `json:"reassignControlResults,omitempty"`
	CommunicationLinksResult  string   `json:"communicationLinksResult"`
	EnableCommunicationResult string   `json:"enableCommunicationResult"`
}

// FxReservedIDs is a PubSubReserveCommunicationIdsResult2DataType (Part 81
// §10.43.4).
type FxReservedIDs struct {
	Result             string   `json:"result"`
	DefaultPublisherID any      `json:"defaultPublisherId,omitempty"`
	WriterGroupIDs     []uint16 `json:"writerGroupIds"`
	DataSetWriterIDs   []uint16 `json:"dataSetWriterIds"`
	// TransportSpecificInfo is, for UDP-UADP, the UDP port of unicast
	// reception.
	TransportSpecificInfo any `json:"transportSpecificInfo,omitempty"`
}

// FxCommunicationConfigResult is a
// PubSubCommunicationConfigurationResultDataType (Part 81 §10.11.3).
type FxCommunicationConfigResult struct {
	Result           string   `json:"result"`
	ChangesApplied   bool     `json:"changesApplied"`
	ReferenceResults []string `json:"referenceResults,omitempty"`
	// ConfigurationValues are the identifiers the server gave the elements
	// of the configuration.
	ConfigurationValues  []FxConfigurationValue `json:"configurationValues,omitempty"`
	ConfigurationObjects []string               `json:"configurationObjects,omitempty"`
}

// FxConfigurationValue is a PubSubConfigurationValueDataType (Part 14
// §6.2.12.4): the element (as its ConfigurationMask names and indexes),
// its name and the identifier the server gave it.
type FxConfigurationValue struct {
	Element    string `json:"element"`
	Name       string `json:"name"`
	Identifier any    `json:"identifier,omitempty"`
}

// FxCloseResult is the result of fx close: the status of the
// CloseConnections call (Part 81 §6.2.5) and one result per endpoint.
type FxCloseResult struct {
	AutomationComponent string `json:"automationComponent"`
	Remove              bool   `json:"remove"`
	// Status is Good, or Uncertain when an endpoint could not be closed
	// (Table 23).
	Status     string             `json:"status"`
	StatusCode uint32             `json:"statusCode"`
	Endpoints  []FxClosedEndpoint `json:"endpoints"`
}

// FxClosedEndpoint is the result of closing one ConnectionEndpoint.
type FxClosedEndpoint struct {
	NodeID     string `json:"nodeId"`
	Status     string `json:"status"`
	StatusCode uint32 `json:"statusCode"`
}
