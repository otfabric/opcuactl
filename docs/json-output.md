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

`version --format json` (`tag` only on a tagged build):

```json
{"version":"dev","commit":"none","buildDate":"unknown","goVersion":"go1.27.3"}
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

**Endpoint** — a server endpoint.

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

`nodeId`, `status`, `statusCode`

**DryRunResult** — enriched dry-run output for a write operation.

`nodeId`, `displayName`?, `dataType`?, `accessLevel`?, `currentValue`?, `newValue`, `newType`, `status`

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

`applicationUri`, `demoNamespace`?, `demoNamespaceIndex`?, `namespaces`, `certificates`?, `clientTrust`, `anonymous`, `users`?, `endpoints`

**ServeCertificate** — an application instance certificate of the server.

`thumbprint`, `keyType`, `subject`, `notAfter`, `generated`, `file`?
