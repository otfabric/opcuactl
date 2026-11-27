# JSON output

opcuactl's machine-readable output. `--ua-encoding` is a different thing: it prints a DataValue in the Part 6 encodings ([README](../README.md#opc-ua-json-and-xml-encodings)). Exit codes are in [exitcodes.md](exitcodes.md). A breaking change to a field name or shape is called out in RELEASE.md.

## Conventions

- `--format` (`-f`) takes `table`, `json`, `yaml`, `csv` or `jsonl`. `OPCUACTL_FORMAT` sets the default; `OPCUACTL_OUTPUT_FORMAT` is accepted too.
- `json` prints one compact document and a newline on stdout; `--pretty` indents it. `jsonl` prints one object per line: a list gives one line per element, a single record one line. `monitor` and `pubsub subscribe` stream with `--jsonl`.
- `yaml` prints the same data; `csv` flattens the JSON form, with the object keys as columns.
- Field names are camelCase.
- A result with no record type is its table: under `json` an array of objects keyed by the column headers, under `jsonl` one such object per line.
- Records carry no `schemaVersion`.

## Failures

A failed command has no JSON form: stderr gets `Error: <message>`, stdout stays empty, and the exit code classifies the cause. This differs from the organisation's `{"error":{...}}` convention.

A Bad status on one item is not a failed command. The item is reported in its record and `get value` exits 0:

```json
{"nodeId":"i=999999","value":null,"type":"TypeIDNull","status":"BadNodeIDUnknown","statusCode":2150891520,"serverTimestamp":"2026-10-31T08:39:26.94881Z"}
```

`status` is the status name, `statusCode` its number. Check them per item.

## Examples

`get value --format json`:

```json
{"nodeId":"i=2259","value":0,"type":"TypeIDInt32","status":"Good","statusCode":0,"sourceTimestamp":"2026-10-31T08:39:26.936902Z","serverTimestamp":"2026-10-31T08:39:26.936902Z"}
```

`version --json` (or `--format json`/`jsonl`): one line, the keys always present and in this order; `tag` is `""` on a build without a tag, `version` is `dev` and `built` is `unknown` on a development build; `libraries` are the OT Fabric libraries the binary is built with:

```json
{"name":"opcuactl","version":"0.15.0","tag":"v0.15.0","commit":"1802d0f9afe8642d1e278120b76fcdfac967cb9e","built":"2026-11-07T10:00:00Z","libraries":[{"name":"go-opcua","version":"1.31.0"}],"go":"go1.26.10","platform":"linux/amd64"}
```

`get attribute`, `call` and the `gds` commands add these small records: `nodeId`, `attribute`, `attributeId`, `value`, `status`, `statusCode`; `objectId`, `methodId`, `inputs`, `outputs`; `method`, `status`.

## Records

The field lists of `internal/model`, which a test keeps in step with this page. `?` marks a field omitted when empty.

### Core

**DiscoveredServer** — a discovered OPC UA server.

`applicationUri`, `productUri`, `applicationName`, `applicationType`, `gatewayServerUri`?, `discoveryProfileUri`?, `discoveryUrls`

**ServerOnNetwork** — a discovered server on the network.

`recordId`, `serverName`, `discoveryUrl`, `serverCapabilities`

**DiscoveryResult** — what a Local Discovery Server knows.

`discoveryServerUrl`, `multicastSupported`, `servers`, `serversOnNetwork`

**RegisterResult** — the outcome of `discover register`. `online` is false for `--offline`; `service` is RegisterServer2 or RegisterServer; `configurationResults` are the results of the discovery configurations of RegisterServer2 (BadNotSupported: the discovery server ignored it).

`discoveryServerUrl`, `serverUri`, `online`, `service`, `securityPolicy`, `securityMode`, `status`, `configurationResults`?

**Endpoint** — a server endpoint. `certThumbprint` is the SHA-1 thumbprint of the server certificate (the first certificate of the chain), 40 upper-case hex digits, as `serve` and the GDS commands print it.

`endpointUrl`, `securityPolicy`, `securityMode`, `securityLevel`, `hasCertificate`, `userTokenPolicies`, `transportProfile`?, `applicationUri`?, `certThumbprint`?, `urlMismatch`?

**UserTokenPolicy** — a user token policy.

`policyId`, `tokenType`

**NodeValue** — a read value result.

`nodeId`, `value`, `type`?, `status`, `statusCode`, `sourceTimestamp`?, `serverTimestamp`?

**NodeInfo** — a node summary.

`nodeId`, `nodeClass`, `browseName`, `displayName`, `description`?, `dataType`?, `accessLevel`?, `accessInterpretation`?, `userAccessLevel`?, `typeDefinition`?, `value`?, `valueStatus`?, `writeMask`?, `valueRank`?, `arrayDimensions`?, `minSamplingInterval`?, `historizing`?

**Reference** — a node reference edge.

`referenceType`, `isForward`, `nodeId`, `browseName`, `displayName`, `nodeClass`, `typeDefinition`?

**ChildNode** — a child node in browse results.

`nodeId`, `browseName`, `displayName`, `nodeClass`, `typeDefinition`?

**ServerStatus** — server metadata.

`startTime`, `currentTime`, `state`, `secondsTillShutdown`?, `shutdownReason`?, `buildInfo`?, `securityPolicy`?, `securityMode`?

**BuildInfo** — server build information.

`productUri`, `manufacturerName`, `productName`, `softwareVersion`, `buildNumber`, `buildDate`

**TreeNode** — a node in a browse tree.

`nodeId`, `browseName`, `displayName`, `nodeClass`, `typeDefinition`?, `depth`, `children`?

**NamespaceEntry** — a namespace in the server's namespace array.

`index`, `uri`

**WriteResult** — the result of a write operation.

`nodeId`, `attribute`? (`write attr`), `status`, `statusCode`

**DryRunResult** — enriched dry-run output for a write operation.

`nodeId`, `attribute`? (`write attr`), `displayName`?, `dataType`?, `accessLevel`?, `currentValue`?, `newValue`, `newType`, `status`

**MethodArgument** — a method input/output argument definition.

`name`, `dataType`, `valueRank`, `description`?

**CallResult** — the result of a method call.

`objectId`, `methodId`, `status`, `statusCode`, `outputArguments`?

**MonitoredValue** — a single data-change notification.

`ts`, `nodeId`, `value`, `type`?, `status`, `statusCode`, `overflow`?, `sourceTimestamp`?, `serverTimestamp`?

**MonitoredEvent** — a single event notification.

`ts`, `nodeId`?, `fields`

**PingStep** — the result of a single ping stage.

`stage`, `status`, `latency`, `error`?

**HistoryValue** — a single historical data point.

`nodeId`, `value`, `type`?, `status`, `statusCode`, `sourceTimestamp`, `serverTimestamp`?

**BrowsePathResult** — a resolved browse path.

`path`, `nodeId`, `message`?

**HistoryEventRecord** — a single historical event entry.

`nodeId`, `fields`

**HistoryEventUpdate** — the result of inserting, replacing, updating or deleting one historical event (HistoryUpdate UpdateEventDetails, DeleteEventDetails).

`nodeId`, `operation`, `index`, `eventId`?, `status`, `statusCode`, `fields`?

**HistoryValueUpdate** — the result of inserting, replacing or updating one historical value, or of deleting values (HistoryUpdate UpdateDataDetails, DeleteRawModifiedDetails, DeleteAtTimeDetails). `operation` is insert, replace, update, delete or delete-modified; `time` is the SourceTimestamp of a sample or a `--at` time, `start` and `end` the range of a delete by range.

`nodeId`, `operation`, `index`, `time`?, `start`?, `end`?, `value`?, `status`, `statusCode`

**ExportedValue** — a snapshot or historical value export entry.

`nodeId`, `value`, `type`?, `status`, `statusCode`, `sourceTimestamp`?, `serverTimestamp`?

**ExportedNode** — a node in an export result.

`nodeId`, `nodeClass`, `browseName`, `displayName`, `description`?, `dataType`?, `accessLevel`?, `userAccessLevel`?, `typeDefinition`?, `writeMask`?, `valueRank`?, `arrayDimensions`?, `depth`

**VarNode** — a variable node collected by get vars.

`path`?, `nodeId`, `browseName`, `displayName`, `typeDefinition`?, `dataType`?, `value`?, `status`?

**PubSubReceived** — tells how a PubSub NetworkMessage arrived: when, from where, and the message headers of the transport (MQTT 5.0, AMQP).

`ts`?, `source`?, `topic`?, `contentType`?, `uaMessageType`?, `userProperties`?

**PubSubProperty** — a name and value pair of a message header.

`name`, `value`

**PubSubDataSet** — one decoded DataSetMessage of a PubSub NetworkMessage.

`encoding`, `publisherId`, `writerGroupId`?, `writerGroupName`?, `dataSetWriterId`, `dataSetWriterName`?, `messageType`, `sequenceNumber`, `timestamp`?, `status`?, `metaDataVersion`?, `security`, `securityTokenId`?, `action`?, `fields`

**PubSubAction** — the action header of a PubSub Action request or response message (Part 14 §7.2.4.4.2 and §7.2.4.5.9 in 1.05).

`type`, `actionTargetId`, `requestId`, `actionState`, `requestorId`?, `responseAddress`?, `correlationData`?, `timeoutHint`?

**PubSubActionResult** — the result of a PubSub Action call: the Done response of the Responder.

`publisherId`, `dataSetWriterId`, `actionTargetId`, `requestId`, `status`, `statusCode`, `duration`, `outputs`

**PubSubActionRequest** — a PubSub Action request a Responder served, and its result.

`ts`, `requestorId`?, `dataSetWriterId`, `actionTargetId`, `requestId`, `inputs`, `status`, `statusCode`, `outputs`

**PubSubField** — one field of a DataSetMessage.

`index`, `name`?, `value`, `type`?, `status`?, `sourceTimestamp`?

**PubSubMetaData** — a DataSetMetaData announcement seen on the wire.

`encoding`, `publisherId`, `dataSetWriterId`, `name`, `version`?, `fields`

**PubSubFieldMeta** — one field of a DataSet.

`name`, `type`, `valueRank`?

**SecurityKeys** — the result of GetSecurityKeys. Key material is only included when asked for.

`securityGroupId`, `securityPolicyUri`, `firstTokenId`, `timeToNextKey`, `keyLifetime`, `keys`

**SecurityKey** — one key of a SecurityGroup.

`tokenId`, `length`, `key`?

**PubSubComponent** — a component of the PublishSubscribe configuration of a server.

`kind`, `name`, `nodeId`, `state`?, `properties`?, `children`?

**PubSubAdded** — the result of a PubSub configuration method that adds a component.

`kind`, `name`, `nodeId`, `results`?

**PubSubReservedIDs** — the result of ReserveIds (Part 14 §9.1.3.7.5).

`transportProfileUri`, `defaultPublisherId`, `writerGroupIds`, `dataSetWriterIds`

**PubSubDiagnostics** — are the diagnostics counters of a PubSub component (Part 14 §9.1.11): of a server's component, or of what a pubsub command sent and received.

`component`, `name`?, `nodeId`?, `diagnosticsLevel`, `totalInformation`, `totalError`, `subError`?, `counters`, `liveValues`?

**PubSubCounter** — one diagnostics counter (PubSubDiagnosticsCounterType).

`name`, `value`, `classification`, `diagnosticsLevel`, `active`, `timeFirstChange`?

**PubSubAnnouncement** — a UADP discovery announcement (Part 14 §7.2.4.6) other than DataSetMetaData, which is a PubSubMetaData.

`publisherId`, `announcement`, `sequenceNumber`, `status`?, `details`?

**PubSubResult** — the per-variable result of AddPublishedDataItems.

`field`, `nodeId`, `status`, `statusCode`

### Alarms and conditions

**Condition** — the state of an OPC UA condition (Part 9) as its latest condition event reports it. The states an instance does not have (a ConditionType has no AckedState, an alarm without ConfirmedState) are omitted.

`conditionId`, `branchId`?, `conditionName`?, `sourceNode`?, `sourceName`?, `eventType`?, `severity`, `message`?, `time`?, `retain`, `enabled`, `active`?, `acked`?, `confirmed`?, `shelving`?, `suppressedOrShelved`?, `suppressed`?, `outOfService`?, `silenced`?, `latched`?, `dialogActive`?, `prompt`?, `lastResponse`?, `audibleEnabled`?, `reAlarmRepeatCount`?, `firstInGroup`?, `quality`?, `comment`?, `clientUserId`?, `eventId`

**ConditionNotification** — one line of alarms watch: a condition event, or the start or end of a ConditionRefresh (Refresh "start" / "end").

`ts`, `refresh`?

**AlarmAction** — the result of a condition method (Acknowledge, Confirm, AddComment, Enable, Disable, the shelving, suppression, service, silence and reset methods, Respond).

`conditionId`, `action`, `eventId`?, `comment`?, `shelvingTimeMs`?, `selectedResponse`?, `response`?, `auditEntryId`?, `status`

**AlarmGroup** — an alarm group (AlarmGroupType) a condition is a member of.

`nodeId`, `browseName`?, `displayName`?

**AlarmMetrics** — are the values of an AlarmMetricsType object (Part 9 1.05).

`nodeId`, `alarmCount`, `startTime`?, `maximumActiveStateSeconds`, `maximumUnAckSeconds`, `maximumReAlarmCount`, `currentAlarmRate`, `maximumAlarmRate`, `averageAlarmRate`, `rateMinutes`

**AuditEvent** — an audit event of a condition method (AuditConditionEventType and its subtypes, Part 9 §5.10): who called which method of which condition, and whether the server accepted the call.

`ts`, `eventType`, `conditionId`?, `sourceName`?, `time`?, `actionTime`?, `status`, `method`?, `clientUserId`?, `clientAuditEntryId`?, `serverId`?, `conditionEventId`?, `comment`?, `selectedResponse`?, `shelvingTimeMs`?, `message`?, `eventId`

### Diagnostics

**DiagnosticsSummary** — Server.ServerDiagnostics.ServerDiagnosticsSummary (Part 5 §6.3.3) with the EnabledFlag.

`enabled`, `serverViewCount`, `currentSessionCount`, `cumulatedSessionCount`, `securityRejectedSessionCount`, `rejectedSessionCount`, `sessionTimeoutCount`, `sessionAbortCount`, `currentSubscriptionCount`, `cumulatedSubscriptionCount`, `publishingIntervalCount`, `securityRejectedRequestsCount`, `rejectedRequestsCount`

**ServiceCounter** — counts the requests of a service and how many failed.

`total`, `errors`

**SessionDiagnostics** — a SessionDiagnosticsDataType. Services holds the per-service counters that are not zero.

`sessionId`, `sessionName`, `clientName`?, `clientUri`?, `endpointUrl`?, `localeIds`?, `actualSessionTimeoutMs`, `maxResponseMessageSize`, `clientConnectionTime`?, `clientLastContactTime`?, `currentSubscriptionsCount`, `currentMonitoredItemsCount`, `currentPublishRequestsInQueue`, `totalRequestCount`, `unauthorizedRequestCount`, `services`?

**SessionSecurityDiagnostics** — a SessionSecurityDiagnosticsDataType; the client certificate is given by its SHA-1 thumbprint.

`sessionId`, `clientUserId`, `clientUserIdHistory`?, `authenticationMechanism`, `encoding`, `transportProtocol`, `securityMode`, `securityPolicy`, `clientCertificateThumbprint`?

**SubscriptionDiagnostics** — a SubscriptionDiagnosticsDataType.

`sessionId`, `subscriptionId`, `priority`, `publishingIntervalMs`, `maxKeepAliveCount`, `maxLifetimeCount`, `maxNotificationsPerPublish`, `publishingEnabled`, `modifyCount`, `enableCount`, `disableCount`, `republishRequestCount`, `republishMessageRequestCount`, `republishMessageCount`, `transferRequestCount`, `transferredToAltClientCount`, `transferredToSameClientCount`, `publishRequestCount`, `dataChangeNotificationsCount`, `eventNotificationsCount`, `notificationsCount`, `latePublishRequestCount`, `currentKeepAliveCount`, `currentLifetimeCount`, `unacknowledgedMessageCount`, `discardedMessageCount`, `monitoredItemCount`, `disabledMonitoredItemCount`, `monitoringQueueOverflowCount`, `nextSequenceNumber`, `eventQueueOverflowCount`

**SamplingIntervalDiagnostics** — a SamplingIntervalDiagnosticsDataType.

`samplingIntervalMs`, `monitoredItemCount`, `maxMonitoredItemCount`, `disabledMonitoredItemCount`

### OPC UA FX

**FxReport** — what `fx inspect` read of the OPC UA FX model of a server (OPC 10000-81). `hasFxNamespaces`: the server has the namespaces of Part 81 §14.2 (DI, Data, and AC or CM); `isFxServer`: those and FxRoot (§12.2), not a conformance statement. `truncated`: a limit cut the inspection short (exit 7).

`hasFxNamespaces`, `isFxServer`, `namespaces`, `serverCapabilities`?, `fxRoot`?, `automationComponents`, `connectionManagers`, `findings`, `truncated`

**FxNamespace** — one namespace of the UAFX model (DI, Data, AC, CM, in this order), with its NamespaceMetadata.

`uri`, `present`, `index`?, `version`?, `publicationDate`?

**FxNode** — what every node of the report has; the records below hold its fields too.

`nodeId`, `browseName`, `displayName`?, `typeDefinition`?

**FxAutomationComponent** — an AutomationComponent (§6.2.2).

`conformanceName`?, `aggregatedHealth`?, `assets`, `functionalEntities`, `descriptors`?, `capabilities`?

**FxCapabilities** — the ComponentCapabilities of an AutomationComponent (§6.2.6 Table 26); a member the component does not have is absent. `supportsPersistence` absent or false: the component does not support persistent ConnectionEndpoints.

`supportsPersistence`?, `maxConnections`?, `maxConnectionsPerCall`?, `maxFunctionalEntities`?, `minConnections`?, `commandBundleRequired`?

**FxHealth** — an AggregatedHealthDataType (§10.1): the option sets as names (`OK` when no bit is set, else the bits joined by `|`) and as numbers.

`deviceHealth`, `deviceHealthBits`, `operationalHealth`, `operationalHealthBits`

**FxAsset** — an Asset (§6.3.2) with its nameplate.

`manufacturer`?, `manufacturerUri`?, `model`?, `productCode`?, `hardwareRevision`?, `softwareRevision`?, `deviceRevision`?, `serialNumber`?, `productInstanceUri`?, `assetId`?, `componentName`?, `deviceClass`?

**FxFunctionalEntity** — a FunctionalEntity (§6.4.2); `authorAssignedVersion` is Major.Minor.Build.SubBuild.

`authorUri`?, `authorAssignedIdentifier`?, `authorAssignedVersion`?, `operationalHealth`?, `operationalHealthBits`?, `connectionEndpoints`, `subFunctionalEntities`?

**FxConnectionEndpoint** — a ConnectionEndpoint (§6.6.2); `status` is the name of the ConnectionEndpointStatusEnum value (§10.17). `cleanupTimeoutMs` is negative for an endpoint that is never cleaned up, which a persistent endpoint requires; `creationTime` and `modificationTime` (RFC 3339, UTC) are those of its Diagnostics (Table 74).

`status`?, `isPersistent`, `cleanupTimeoutMs`, `connectionManagerApplicationUri`?, `creationTime`?, `modificationTime`?

**FxConnectionManager** — a ConnectionManager (§6.7.2).

`connectionConfigurationSets`

**FxFinding** — a deviation from Part 81; `clause` is the clause number, `nodeId` and `path` are absent for the server as a whole.

`nodeId`?, `path`?, `clause`, `text`

**FxConnectResult** — what `fx connect` got back from EstablishConnections (Part 81 §6.2.4). `commands` are the commands of the call; `status` is `Good`, or `Uncertain` when a command failed and the server aborted the call (§6.2.4.3.11, exit 6); the rollback follows Part 81 Table 21, which leaves the ConfigurationData the call wrote and the ids it reserved, so some changes may remain. The lists hold one result per element of the request; StatusCodes are their symbols, and an element of a command the call did not run is `BadNothingToDo`.

`automationComponent`, `commands`, `status`, `statusCode`, `assetVerifications`, `endpoints`, `reserveCommunicationIds`, `communicationConfigurations`

**FxAssetVerificationResult** — the verification of an Asset (§10.6): `result` is NotSet, Match, Compatible or Mismatch.

`status`, `result`, `variableErrors`?, `additionalVariableErrors`?

**FxEndpointResult** — the result of a ConnectionEndpointConfiguration (§10.15). `connectionEndpoint` is the NodeId of the endpoint the call created or used, absent when there is none; `verificationResult` is NotSet, Match or Mismatch.

`connectionEndpoint`?, `functionalEntityResult`, `connectionEndpointResult`, `verificationResult`, `verificationStatus`, `verificationErrors`?, `establishControlResults`?, `configurationDataResults`?, `reassignControlResults`?, `communicationLinksResult`, `enableCommunicationResult`

**FxReservedIDs** — the PubSub ids the server reserved (§10.43.4); `transportSpecificInfo` is, for UDP-UADP, the UDP port of unicast reception.

`result`, `defaultPublisherId`?, `writerGroupIds`, `dataSetWriterIds`, `transportSpecificInfo`?

**FxCommunicationConfigResult** — the result of applying the PubSub configuration (§10.11.3).

`result`, `changesApplied`, `referenceResults`?, `configurationValues`?, `configurationObjects`?

**FxConfigurationValue** — an identifier the server gave an element of the PubSub configuration (Part 14 §6.2.12.4); `element` names the element by its ConfigurationMask and indexes ("ElementAdd|ReferenceWriter connection=0 group=0 element=0").

`element`, `name`, `identifier`?

**FxCloseResult** — what `fx close` got back from CloseConnections (Part 81 §6.2.5): `status` is `Good`, or `Uncertain` when an endpoint could not be closed (Table 23), and one result per endpoint.

`automationComponent`, `remove`, `status`, `statusCode`, `endpoints`

**FxClosedEndpoint** — the result of closing one ConnectionEndpoint (Table 24).

`nodeId`, `status`, `statusCode`

**FxCMList** — what `fx cm list` read: the ConnectionConfigurationSets of the ConnectionManager (Part 81 §6.7.2, §6.8.2). `aggregatedCurrentState` is TRUE while a set is in Error; `truncated` is TRUE when a bound cut the listing short.

`connectionManager`, `aggregatedCurrentState`?, `sets`, `truncated`?

**FxCMSet** — the state of one ConnectionConfigurationSet: `path` is its Folders and Name joined by `/`; `state` is the CurrentState of its state machine (§6.9.2) as the server shows it, `stateId` Ready, Processing or Error from its Id; `diagnostics` is ConnectionsDiagnostics; `unread` names the values the server did not return (a Bad status or another type) and the ConnectionsDiagnostics entries that did not decode, so the fields for them are missing.

`path`, `nodeId`, `state`?, `stateId`?, `lastTransition`?, `version`?, `edit`?, `rollbackOnError`?, `connections`, `diagnostics`, `unread`?

**FxCMConnection** — a ConnectionConfiguration of a set with its ProcessingResult.

`name`, `nodeId`, `processingResult`?, `processingResultCode`?

**FxCMDiagnostics** — one ConnectionDiagnosticsDataType (§6.8.2): the outcome of the last run of a Connection.

`name`, `lastActivity`, `connectionState`, `errorEndpoint1`, `endpoint1Status`, `errorEndpoint2`, `endpoint2Status`

**FxCMCallResult** — what `fx cm process` got back from ProcessConnectionConfigurationSets (§6.7.5, Tables 85 and 86): the status of the call and one result per set.

`method`, `action`, `status`, `statusCode`, `sets`

**FxCMSetResult** — the result for one set; with `--wait`, `state` is the state the set was last read in: `Ready`, `Processing` or `Error` by the NodeId in CurrentState.Id, `unknown` when that is no state of §6.9.2 or the set has none, `unread` when it could not be read.

`path`, `nodeId`, `status`, `statusCode`, `state`?

**FxCMExport** — what `fx cm export` read from ConnectionManagerConfiguration (F.3): the size of the file and a summary of its sets.

`file`?, `bytes`, `sets`

**FxCMFileSet** — one ConnectionConfigurationSetConf of a file (F.2).

`path`, `version`, `connections`, `communicationFlows`, `automationComponents`

**FxCMImport** — what `fx cm import` got back from CloseAndUpdate (F.3.3, Tables F.39 to F.41): ChangesApplied and one result per operation.

`file`, `requireCompleteUpdate`, `changesApplied`, `operations`

**FxCMOperation** — the result of one operation; `nodeId` is the set Object the server returned, absent for a removal or a failed operation.

`path`, `operation`, `status`, `statusCode`, `nodeId`?

**FxCMEdit** — what `fx cm edit` did (§6.7.4): the writes, whether CommitUpdates succeeded and the Version after it. `outcome` is `committed` (the Version read after CommitUpdates is the one before StartEditing plus one, and the written Variables read the values written; also when CommitUpdates got no answer), `not-committed` (the edit ended before the commit, the server refused it, or it answered Good with the Version unchanged: a server ignores the commit of an edit that ended, §6.7.4) or `unknown` (the commit cannot be established: the Version cannot be read, is unchanged after a CommitUpdates that got no answer, or went up by more than one, or a Variable reads another value); a CommitUpdates without an answer is never `not-committed`, whatever DiscardUpdates answers, since a server ignores DiscardUpdates once a commit ended the edit; `version` is the Version read after CommitUpdates; `failed` names the step that failed, `discarded` is TRUE when DiscardUpdates answered Good for an edit known to be open (before CommitUpdates was sent, or after the server refused it; never after a CommitUpdates without an answer) and `cleanupError` says why DiscardUpdates failed.

`path`, `nodeId`, `writes`, `outcome`, `committed`, `version`?, `failed`?, `discarded`?, `cleanupError`?

**FxCMWrite** — one write of an edit.

`path`, `nodeId`?, `value`, `status`?, `statusCode`?

### GDS and certificate management

**GDSProperties** — are the properties of the ServerConfiguration object (OPC UA Part 12 §7.10.3) and the certificate types of the DefaultApplicationGroup.

`serverCapabilities`, `supportedPrivateKeyFormats`, `maxTrustListSize`, `multicastDnsEnabled`, `certificateTypes`

**GDSCertificate** — a certificate or CRL of a trust list or the rejected list. List is trusted, trusted-crl, issuer, issuer-crl or rejected.

`list`, `group`?, `certificateType`?, `kind`, `subject`?, `issuer`?, `applicationUri`?, `thumbprint`, `notBefore`?, `notAfter`?, `ca`?, `revoked`?, `file`?, `error`?

**GDSResult** — the outcome of a change of the server's certificates.

`action`, `thumbprints`?, `applyChangesRequired`, `applied`

**GDSSigningRequest** — the certificate signing request CreateSigningRequest returned.

`certificateType`?, `subject`, `uris`?, `dnsNames`?, `ipAddresses`?, `keyAlgorithm`, `pem`, `file`?

**GDSTransaction** — are the TransactionDiagnostics of ServerConfiguration (Part 12 §7.10.11): the last certificate management transaction.

`startTime`?, `endTime`?, `result`, `affectedTrustLists`, `affectedCertificateGroups`, `errors`

**GDSTransactionError** — an error of a transaction: the trust list, certificate group or certificate it concerns.

`target`, `error`, `message`?

**GDSCertificateGroup** — a certificate group of ServerConfiguration.

`name`, `nodeId`, `trustList`, `certificateTypes`

**GDSApplication** — an application record of a Global Discovery Server (ApplicationRecordDataType, Part 12 §6.6.3).

`applicationId`?, `applicationUri`, `applicationType`, `applicationNames`, `productUri`?, `discoveryUrls`, `serverCapabilities`

**GDSApplicationQuery** — the result of QueryApplications.

`lastCounterResetTime`?, `nextRecordId`, `applications`

**GDSServer** — a server of QueryServers.

`recordId`, `serverName`, `discoveryUrl`, `serverCapabilities`

**GDSServerQuery** — the result of QueryServers.

`lastCounterResetTime`?, `servers`

**GDSRequest** — a certificate request of a GDS.

`applicationId`, `requestId`, `kind`

**GDSIssued** — a certificate a GDS issued (FinishRequest, Enroll) and where opcuactl wrote it.

`applicationId`, `requestId`?, `certificate`, `issuers`, `certFile`?, `keyFile`?, `issuersFile`?, `privateKeyPem`?, `trustList`?, `trustDir`?

**GDSCertificateStatus** — the result of GetCertificateStatus.

`applicationId`, `group`?, `certificateType`?, `updateRequired`

**GDSRevocationStatus** — the result of CheckRevocationStatus.

`thumbprint`, `subject`?, `status`, `validUntil`?

### Test server

**ServeInfo** — a server started by `opcuactl serve`.

`applicationUri`, `demoNamespace`?, `demoNamespaceIndex`?, `namespaces`, `certificates`?, `clientTrust`, `anonymous`, `users`?, `limits` (`maxSessions`, `maxSubscriptions`, `maxSubscriptionsPerSession`, `maxOperations`: 0 is no limit), `lds`? (a Local Discovery Server, `--lds`), `registeredWith`? (the `--register-with` URLs), `nodeManagement`? (`--node-management`), `endpoints`

**ServeCertificate** — an application instance certificate of the server.

`thumbprint`, `keyType`, `subject`, `notAfter`, `generated`, `file`?
