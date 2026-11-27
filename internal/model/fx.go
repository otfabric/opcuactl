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
	// Capabilities are the ComponentCapabilities (Part 81 §6.2.6 Table
	// 26), absent when the component has none.
	Capabilities *FxCapabilities `json:"capabilities,omitempty"`
}

// FxCapabilities are the ComponentCapabilities of an AutomationComponent
// (Part 81 §6.2.6 Table 26). A member the component does not have, or that
// cannot be read, is absent; SupportsPersistence absent or false means the
// component does not support persistent ConnectionEndpoints.
type FxCapabilities struct {
	SupportsPersistence   *bool   `json:"supportsPersistence,omitempty"`
	MaxConnections        *uint32 `json:"maxConnections,omitempty"`
	MaxConnectionsPerCall *uint32 `json:"maxConnectionsPerCall,omitempty"`
	MaxFunctionalEntities *uint32 `json:"maxFunctionalEntities,omitempty"`
	MinConnections        *uint32 `json:"minConnections,omitempty"`
	CommandBundleRequired *bool   `json:"commandBundleRequired,omitempty"`
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
	Status       string `json:"status,omitempty"`
	IsPersistent bool   `json:"isPersistent"`
	// CleanupTimeoutMs is the CleanupTimeout in whole milliseconds; a
	// negative value (the endpoint is never cleaned up, which a persistent
	// endpoint requires, §6.6.2) stays negative.
	CleanupTimeoutMs                int64  `json:"cleanupTimeoutMs"`
	ConnectionManagerApplicationURI string `json:"connectionManagerApplicationUri,omitempty"`
	// CreationTime and ModificationTime are those of the Diagnostics of
	// the endpoint (§6.6.2 Table 74), RFC 3339 in UTC, absent when it has
	// none.
	CreationTime     string `json:"creationTime,omitempty"`
	ModificationTime string `json:"modificationTime,omitempty"`
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
	// aborted the call (§6.2.4.3.11); the rollback follows Part 81 Table
	// 21, so ConfigurationData written and ids reserved may remain.
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

// FxCMList is the result of fx cm list: the ConnectionConfigurationSets of
// the ConnectionManager of a server (Part 81 §6.7.2, §6.8.2).
type FxCMList struct {
	ConnectionManager string `json:"connectionManager"`
	// AggregatedCurrentState is TRUE while a set is in Error (§6.7.2); nil
	// when the server has no such Variable.
	AggregatedCurrentState *bool     `json:"aggregatedCurrentState,omitempty"`
	Sets                   []FxCMSet `json:"sets"`
	// Truncated is TRUE when a bound of opcuactl cut the listing short.
	Truncated bool `json:"truncated,omitempty"`
}

// FxCMSet is the state of one ConnectionConfigurationSet.
type FxCMSet struct {
	// Path is the Folders and the Name of the set, joined by "/".
	Path   string `json:"path"`
	NodeID string `json:"nodeId"`
	// State is the CurrentState of its state machine (§6.9.2) as the
	// server shows it; StateID names the state, Ready, Processing or
	// Error, when the server gives its NodeId.
	State           string           `json:"state,omitempty"`
	StateID         string           `json:"stateId,omitempty"`
	LastTransition  string           `json:"lastTransition,omitempty"`
	Version         *uint32          `json:"version,omitempty"`
	Edit            *bool            `json:"edit,omitempty"`
	RollbackOnError *bool            `json:"rollbackOnError,omitempty"`
	Connections     []FxCMConnection `json:"connections"`
	// Diagnostics is ConnectionsDiagnostics: the outcome of the last run
	// per Connection.
	Diagnostics []FxCMDiagnostics `json:"diagnostics"`
	// Unread names the values the server did not return (a Bad status, or
	// a value of another type) and the ConnectionsDiagnostics entries that
	// did not decode: the fields above are incomplete for them.
	Unread []string `json:"unread,omitempty"`
}

// FxCMConnection is a ConnectionConfiguration of a set.
type FxCMConnection struct {
	Name   string `json:"name"`
	NodeID string `json:"nodeId"`
	// ProcessingResult as the server gives it; empty without one.
	ProcessingResult     string  `json:"processingResult,omitempty"`
	ProcessingResultCode *uint32 `json:"processingResultCode,omitempty"`
}

// FxCMDiagnostics is one ConnectionDiagnosticsDataType (§6.8.2).
type FxCMDiagnostics struct {
	Name            string `json:"name"`
	LastActivity    string `json:"lastActivity"`
	ConnectionState string `json:"connectionState"`
	ErrorEndpoint1  string `json:"errorEndpoint1"`
	Endpoint1Status string `json:"endpoint1Status"`
	ErrorEndpoint2  string `json:"errorEndpoint2"`
	Endpoint2Status string `json:"endpoint2Status"`
}

// FxCMCallResult is the result of fx cm process: the status of the
// ProcessConnectionConfigurationSets call (§6.7.5) and one result per set.
type FxCMCallResult struct {
	Method     string          `json:"method"`
	Action     string          `json:"action"`
	Status     string          `json:"status"`
	StatusCode uint32          `json:"statusCode"`
	Sets       []FxCMSetResult `json:"sets"`
}

// FxCMSetResult is the result for one set; with fx cm process --wait also
// the state the set was last read in: Ready, Processing or Error by the
// NodeId of the state, "unknown" when the server gave none opcuactl
// recognises, "unread" when it could not be read.
type FxCMSetResult struct {
	Path       string `json:"path"`
	NodeID     string `json:"nodeId"`
	Status     string `json:"status"`
	StatusCode uint32 `json:"statusCode"`
	State      string `json:"state,omitempty"`
}

// FxCMExport is the result of fx cm export: the sets of the
// ConnectionManagerConfiguration file (Annex F.2, F.3).
type FxCMExport struct {
	File  string        `json:"file,omitempty"`
	Bytes int           `json:"bytes"`
	Sets  []FxCMFileSet `json:"sets"`
}

// FxCMFileSet summarises one ConnectionConfigurationSetConf of a file.
type FxCMFileSet struct {
	Path                 string `json:"path"`
	Version              uint32 `json:"version"`
	Connections          int    `json:"connections"`
	CommunicationFlows   int    `json:"communicationFlows"`
	AutomationComponents int    `json:"automationComponents"`
}

// FxCMImport is the result of fx cm import: CloseAndUpdate of the
// ConnectionManagerConfiguration file (F.3.3, Tables F.39 to F.41).
type FxCMImport struct {
	File                  string          `json:"file"`
	RequireCompleteUpdate bool            `json:"requireCompleteUpdate"`
	ChangesApplied        bool            `json:"changesApplied"`
	Operations            []FxCMOperation `json:"operations"`
}

// FxCMOperation is the result of one operation of fx cm import.
type FxCMOperation struct {
	Path       string `json:"path"`
	Operation  string `json:"operation"`
	Status     string `json:"status"`
	StatusCode uint32 `json:"statusCode"`
	// NodeID is the ConnectionConfigurationSet Object the server returned;
	// empty for a removal or a failed operation.
	NodeID string `json:"nodeId,omitempty"`
}

// FxCMEdit is the result of fx cm edit: the writes of an edit of one set
// (Part 81 §6.7.4) and whether it was committed.
type FxCMEdit struct {
	Path   string      `json:"path"`
	NodeID string      `json:"nodeId"`
	Writes []FxCMWrite `json:"writes"`
	// Outcome is "committed" when the Version went up by one and the
	// written Variables read the values written, after a Good
	// CommitUpdates or one that got no answer; "not-committed" when the
	// edit ended before a commit, the server refused it, or answered Good
	// with the Version unchanged (a commit of an edit that had ended is
	// ignored, Part 81 §6.7.4); and "unknown" when the commit cannot be
	// established: the set may or may not have its next Version. A
	// CommitUpdates without an answer is never not-committed.
	Outcome string `json:"outcome"`
	// Committed is TRUE for the outcome committed. Version is the Version
	// of the set read after CommitUpdates.
	Committed bool    `json:"committed"`
	Version   *uint32 `json:"version,omitempty"`
	// Failed names the step that failed; Discarded is TRUE when
	// DiscardUpdates answered Good for an edit known to be open, before
	// CommitUpdates was sent or after the server refused it; never after a
	// CommitUpdates without an answer, since a server ignores
	// DiscardUpdates once a commit ended the edit. CleanupError is why
	// DiscardUpdates failed, when it did.
	Failed       string `json:"failed,omitempty"`
	Discarded    bool   `json:"discarded,omitempty"`
	CleanupError string `json:"cleanupError,omitempty"`
}

// FxCMWrite is one write of an edit.
type FxCMWrite struct {
	Path       string `json:"path"`
	NodeID     string `json:"nodeId,omitempty"`
	Value      string `json:"value"`
	Status     string `json:"status,omitempty"`
	StatusCode uint32 `json:"statusCode,omitempty"`
}
