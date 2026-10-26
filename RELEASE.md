# opcuactl Releases

## v0.10.1

**Date:** 2026-10-26  
**Previous release:** v0.10.0

### Summary

A patch release from a test-coverage pass over the commands: end-to-end tests against `opcuactl serve` for every command family, and the bugs they found.

### Fixed

- **`browse refs --direction`.** `forward`, `inverse` and `both` (the documented values, and the default `both`) were passed to a case-sensitive parser that only knew `Forward`, `Inverse` and `Both`, so every lowercase value browsed forward only. They are now case-insensitive, and an unknown direction is a usage error (exit 1).
- **`browse refs --refs`.** A value that is not a numeric reference type NodeId of namespace 0 was ignored silently (all references were returned); it is now a usage error (exit 1).
- **`--node-class` / `--class` of `browse`, `find` and `export nodes`.** `objecttype`, `variabletype`, `referencetype` and `datatype` (offered by the shell completion) were not recognised: `browse` returned every class and `find` nothing. They are recognised in any case now, and an unknown class is a usage error (exit 1) instead of being ignored.
- **`--depth` of `browse tree` (table) and `find node` / `find type`.** `--depth 1` went two levels down; it is now the children of the start node, as in `browse tree --format json`, `export nodes` and `get vars`.
- **`find node --name --regex`** matches the bare BrowseName as well as `ns:Name`, so `^Temp` anchors at the name.

---

## v0.10.0

**Date:** 2026-10-26  
**Previous release:** v0.9.0

### Summary

`opcuactl serve` runs an OPC UA test server built on **[go-opcua v1.26.0](https://github.com/otfabric/go-opcua/releases/tag/v1.26.0)**: opc.tcp, opc.wss and opc.https with the UA JSON mappings of `server.EnableUAJSON`, generated or given certificates, a client trust list and users, and a demo address space with changing, writable and historized values, an alarm with an event history and a method, or your own NodeSet2 models. The client commands can now be tried, and other clients tested, without a PLC. Historical events (Part 11) get a full client: `history event` reads them with a WhereClause, BrowsePath fields and paging, `history update-events` and `history delete-events` change them.

### Added

- **`serve`.** Runs until Ctrl+C or SIGTERM (or `--duration`) and shuts down gracefully, closing the sessions; exit 1 for an invalid flag or file, 2 when a listener cannot start.
  - **Endpoints.** `--listen` (opc.tcp, default `opc.tcp://0.0.0.0:4840`, port 0 picks a free one), `--wss` and `--https` for opc.wss and opc.https, `--uajson` for wss-uajson and https-uajson on them, `--tls-cert` / `--tls-key` for their TLS certificate (default: the RSA application certificate) and `--hostname` for the endpoint URLs.
  - **Security.** `--security-policy` (repeatable or comma-separated, default `None,Basic256Sha256`, all policies of the client) and `--security-mode` (default `Sign,SignAndEncrypt`). `--cert` / `--key` give the application certificates; every other policy gets a self-signed one at start (RSA, ECDSA P-256 / P-384, Ed25519), printed with its thumbprint. `--cert-out` / `--key-out` save them for the clients and the next run. `--trusted-cert` and `--trust-dir` restrict the clients to a trust list.
  - **Users.** `--allow-anonymous` (default true) and `--user name:password` (repeatable, or `OPCUACTL_SERVE_USERS`).
  - **Address space.** The demo namespace `urn:otfabric:opcuactl:demo` (ns=1): read-only values of the common types, `Demo.Counter` and `Demo.Sine` changing every `--sim-interval` (Sine historized for `history value`), the writable `Demo.Level` with the exclusive level alarm `Demo.LevelAlarm`, and the method `Demo.Add`. `--nodeset` imports NodeSet2 files; `--no-demo` leaves the demo out.
  - **Output.** The server and its endpoints (the table of `discover endpoints`) on start; `--format json|yaml` as one document, `-q` the endpoint URLs. `--log-level` shows the server log on stderr.
  - **Event history.** `Demo` is a HistoricalEventNode (EventNotifier SubscribeToEvents | HistoryRead | HistoryWrite, `Server.EnableEventHistory`): it keeps its last 1000 events, an "opcuactl serve started" event (Severity 100) and the LevelAlarm transitions, for `history event`, `update-events` and `delete-events`.
- **`history event`: event history reads as Part 11 §6.5.2 has them.** HistoryRead with ReadEventDetails on an event notifier.
  - `--start` after `--end` reads backward (newest first); one of them is enough with `--num-values` or `--max-events` (two of StartTime, EndTime and NumValuesPerNode).
  - `--select` takes BrowsePaths (`AckedState/Id`, `2:Temperature`) and `ConditionId`; the default fields are EventId, EventType, SourceName, Time, Message and Severity; EventIds print in base64.
  - `--of-type`, `--severity-min` and `--severity-max` build the WhereClause, combined with And (`[And(1,2), OfType, GreaterThanOrEqual]`, only the elements needed).
  - `--num-values` is the page size (NumValuesPerNode): the continuation points are followed until the history is read, `--max-events` events were returned or, with `--continuation single`, after one page; a continuation point left over is released.
  - A Bad status of the node (`BadNodeIdUnknown`, `BadHistoryOperationUnsupported`, ...) exits 3; an empty range (`GoodNoData`) is success.
- **`history update-events`.** Insert, replace or update events (HistoryUpdate with UpdateEventDetails, Part 11 §6.9.4, `--mode insert|replace|update`) given as JSON objects (`--event`, repeatable, or `--events-file`, `-` for stdin): EventId in base64 or 0x hex (made for insert and update when missing), EventType / SourceNode as NodeIds, Time in RFC 3339 (default now), Message, SourceName, Severity, other fields as JSON or OPC UA JSON Variants. Events with the same fields share one UpdateEventDetails.
- **`history delete-events`.** Delete events by `--event-id` (HistoryUpdate with DeleteEventDetails, Part 11 §6.9.7); an unknown EventId is `BadNoData`.
  - Both print the result of each event, confirm on a terminal like `write value` (`--yes`, `--dry-run`) and exit 4 when some events failed, 3 when all failed or the HistoryUpdate did.
- **Shell completion** of the `serve` values of `--security-policy`, `--security-mode` and `--log-level`, of `history update-events --mode` and of the common `history event --select` fields.

### Changed

- **`history event`.** `--max-events` is the number of events to return across pages (it was NumValuesPerNode of one request, without paging; that is now `--num-values`), the default fields start with EventId, the range may run backward, and a Bad status of the node exits 3 (it was printed and skipped with exit 0).

### Fixed

- **`--format jsonl` writes one line per item for every list.** Lists of typed results (`history value`, `history event`, ...) were written as one JSON array on one line.
- **`write value` exits 3 on a Bad write status.** The result is still printed, but a Bad StatusCode from the server (for example `BadUserAccessDenied` or `BadTypeMismatch`) now exits 3, the README's status-code failure, instead of 0.

### Dependency

- **`github.com/otfabric/go-opcua`**: v1.25.0 → [v1.26.0](https://github.com/otfabric/go-opcua/releases/tag/v1.26.0)
  - Event history in the server: `Server.EnableEventHistory`, `Historian.EnableEvents`, HistoryRead ReadEventDetails and HistoryUpdate UpdateEventDetails / DeleteEventDetails, which `serve` offers on `Demo` and the `history` commands use with `HistoryReadEvent`, `HistoryUpdateEvents` and `HistoryDeleteEvents`.
  - `serve` uses the server package: `server.EnableUAJSON`, `WebSocketEndPoint` / `HTTPSEndPoint`, `ApplicationIdentity`, `WithClientCertificateTrustList`, `WithUsernameValidator`, `AddCondition`, `NewHistorian`, `EnableEventHistory`, `RegisterMethod` and `ImportNodeSetXML`.

---

## v0.9.0

**Date:** 2026-10-25  
**Previous release:** v0.8.1

### Summary

The UA JSON mappings of WebSockets and HTTPS, wss-uajson (Part 6 §7.5.2) and https-uajson (Part 6 §7.4.5), on **[go-opcua v1.25.0](https://github.com/otfabric/go-opcua/releases/tag/v1.25.0)**: `--transport wss-uajson|https-uajson` runs every command over them, and `discover endpoints` lists and selects the JSON endpoints.

### Added

- **`--transport`** (`OPCUACTL_TRANSPORT`): `tcp`, `wss`, `https`, `wss-uajson`, `https-uajson` or a transport profile URI. The URL scheme alone still means UA Binary; `wss-uajson` on an `opc.wss://` endpoint and `https-uajson` on an `opc.https://` endpoint use UA JSON instead, for every command (subscriptions included). A transport that does not fit the scheme of `--endpoint`, an unknown one, or UA JSON with `--reverse-listen` is a usage error (exit 1).
- **`--json-content-type standard|legacy`** (`OPCUACTL_JSON_CONTENT_TYPE`): the Content-Type of https-uajson requests, `application/json` (Part 6 §7.4.5, the default) or `application/opcua+uajson`, the only one UA-.NETStandard 2.0.1 takes.
- **`discover endpoints`** shows the JSON endpoints as `opc.wss (json)` and `opc.https (json)` in the TRANSPORT column, and `--transport-profile` filters by `wss-uajson` and `https-uajson`.
- **Shell completion** of `--transport`, `--json-content-type` and the new `--transport-profile` values.

### Changed

- **Endpoint selection follows `--transport`.** `--security-policy auto`, `--security-mode auto` and `discover endpoints --best` choose among the endpoints of the transport the client uses: `--transport`, or else the one of the `--endpoint` URL as before. wss-uajson endpoints are None / None (TLS protects them); over https-uajson the https policies sign the session.
- **`--transport-profile https-uajson` / `wss-uajson`** select the JSON profiles. They matched "https" and "wss" and selected the UA Binary ones.

### Dependency

- **`github.com/otfabric/go-opcua`**: v1.24.0 → **[v1.25.0](https://github.com/otfabric/go-opcua/releases/tag/v1.25.0)**
  - `opcua.TransportProfile`, `opcua.HTTPSJSONContentType` and the JSON endpoints of `SecurityFromEndpoint`.

---

## v0.8.1

**Date:** 2026-10-25  
**Previous release:** v0.8.0

### Summary

Built on **[go-opcua v1.24.0](https://github.com/otfabric/go-opcua/releases/tag/v1.24.0)**: secure connections to servers that send their certificate with its issuer chain (UA-.NETStandard by default) sign over the leaf certificate, as Part 4 §6.1.8 specifies.

### Changed

- **`github.com/otfabric/go-opcua`**: v1.23.1 → **[v1.24.0](https://github.com/otfabric/go-opcua/releases/tag/v1.24.0)**
  - The session signatures (ClientSignature, the X.509 user token signature) cover the leaf of a server certificate chain.

---

## v0.8.0

**Date:** 2026-10-25  
**Previous release:** v0.7.0

### Summary

Alarms and conditions, server diagnostics, certificate management by the push and the pull model of Part 12, OPC UA over HTTPS and WebSockets, and Brainpool and Ed448 certificates, on **[go-opcua v1.23.1](https://github.com/otfabric/go-opcua/releases/tag/v1.23.1)**. It also fixes `--timestamps-to-return`, which ignored most of its documented values.

### Added

- **`opc.wss://` and `opc.https://` endpoints.** Every command takes them in `-e`. `--tls-ca` gives the roots for the server's TLS certificate, `--tls-cert` / `--tls-key` set up mutual TLS, and `--tls-insecure-skip-verify` skips the check with a warning; all of them can also come from `OPCUACTL_TLS_*`. `discover endpoints` shows a TRANSPORT column and filters with `--transport-profile tcp|wss|https`. `--best` and `--security-policy auto` choose among the endpoints of the transport you dialed, and `ping` dials the TCP port of these URLs.
- **Brainpool and Ed448 certificates.** `--cert`, `--key` and `--trusted-cert` take ECDSA keys on brainpoolP256r1 / brainpoolP384r1 and Ed448 keys, which crypto/tls cannot load. That opens the ECC_brainpoolP256r1, ECC_brainpoolP384r1 and ECC_curve448 security policies.
- **`alarms` (Part 9).**
  - `alarms list` calls ConditionRefresh and shows the retained conditions with their states, branches (BranchId) and the EventId of their latest event (`--state`, `--severity-min`, `--node`). `-v` adds the optional states: suppressed, out of service, silenced, latched, dialog.
  - `alarms ack`, `confirm` and `comment` take the latest EventId by default; `--event-id` names another event and `--branch` names a branch.
  - `enable`, `disable`, `shelve --for|--one-shot` and `unshelve` call the condition methods.
  - The Part 9 1.05 methods are commands too: `suppress`, `unsuppress`, `remove-from-service`, `place-in-service`, `silence`, `reset` (latched alarms) and `respond` (dialogs, by option index or text). With `-m` / `--comment`, the commands that have a `…2` variant (Suppress2, TimedShelve2, Respond2, …) call it.
  - `alarms groups` lists the alarm groups of an alarm, and `alarms metrics get|reset` reads and resets AlarmMetricsType objects.
  - `alarms watch` streams the condition events. `alarms audit` streams the audit events of the condition methods; `--entry-id` filters it, and `--audit-entry-id` sets the AuditEntryId of the requests of every `alarms` command.
  - A refused method exits with 3 and gives the reason.
- **`diagnostics`.** Reads Server.ServerDiagnostics:
  - `summary` (with EnabledFlag);
  - `sessions`: `--session <id>|self` shows the details with the counters per service, and `--security` the security diagnostics;
  - `subscriptions` and `sampling`.
  
  `enable` and `disable` switch the collection on and off. The global `--return-diagnostics none|service|operation|all` asks the server for DiagnosticInfos and prints them on stderr for a failed request or operation.
- **`gds`: push certificate management (Part 12 §7).**
  - `props` shows the ServerConfiguration properties.
  - `trustlist get|add|remove|set` reads and changes the trust list. `--out` and the PKI directory layout come with it, and `--group https|usertoken|<NodeId>` selects the trust list of another certificate group.
  - `csr` and `update-cert` renew a certificate of a group. update-cert sends the private key as PEM or PKCS #12 (`--key-format pfx`, `--pfx-password`).
  - `groups` lists the certificate groups, and `certs` shows the certificates in use (GetCertificates).
  - `self-signed` has the server create a certificate (CreateSelfSignedCertificate), and `delete-cert` deletes one (DeleteCertificate).
  - `reset` calls ResetToServerDefaults, and `transaction` shows the TransactionDiagnostics.
  - `rejected` lists the rejected certificates, and `apply` and `cancel` call ApplyChanges and CancelChanges.
  - The changing commands apply in their own session unless you pass `--no-apply`.
- **`gds pull`: certificates from a Global Discovery Server (Part 12 §7.9).**
  - `find`, `get`, `query`, `query-servers`, `register`, `update` and `unregister` manage the application directory.
  - `request` starts a certificate request from a CSR, from a private key (`--private-key`), or for a key pair the GDS generates. `finish` collects the certificate; while the request waits for approval it exits with 3. `wait` polls until the certificate is issued.
  - `trustlist`, `groups`, `certs`, `status` (renewal due?), `revoke` and `check-revocation` (exit 4 when the certificate is not Good) cover the rest.
  - `enroll` registers or finds the application, requests the certificate with a key made here (`--new-key rsa|ecc-p256|ecc-p384|ed25519`), from `--private-key`, or by the GDS, waits, and writes the certificate, the private key (mode 0600) and the trust list.

### Changed

- **Exit code 4** also means that `gds pull check-revocation` found the certificate revoked or not valid.

### Fixed

- **`--timestamps-to-return` / `--timestamps`** (`get value`, `monitor value`). Only the capitalized names were recognized, so the documented lowercase values became Source without any warning, including the default `both`. Any case works now, and an unknown value is an error.

### Dependency

- **`github.com/otfabric/go-opcua`**: v1.11.0 → **[v1.23.1](https://github.com/otfabric/go-opcua/releases/tag/v1.23.1)**

---

## v0.7.0

**Date:** 2026-10-25  
**Previous release:** v0.6.0

### Summary

PubSub Actions on a server, on **[go-opcua v1.11.0](https://github.com/otfabric/go-opcua/releases/tag/v1.11.0)**. `pubsub config add-action` adds a PublishedAction through the server's PubSubConfiguration file. `pubsub config export` and `pubsub config reserve-ids` read that file, and Action requests and responses go in chunks when they are large.

### Added

- **`pubsub config add-action`.** Adds a PublishedAction to a server with CloseAndUpdate on its PubSubConfiguration file (Part 14 §9.1.3.7). You give the request fields (`--request Name:Type`), the response fields (`--response`) and the targets (`--target ID:NAME`). With `--method ID=OBJECT@METHOD` for every target, it is a PublishedActionMethod whose targets call those Methods of the server. A DataSetWriter of the data set (`config add-writer`) makes the server the Responder.
- **`pubsub config export`.** Reads the server's whole PubSub configuration from the file (a PubSubConfiguration2DataType) and prints it in the OPC UA JSON or XML encoding (`--ua-encoding`), or writes it to `--out`.
- **`pubsub config reserve-ids`.** Reserves WriterGroupIds and DataSetWriterIds (`--writer-groups`, `--writers`) and shows the server's default PublisherId for the transport of `--url` / `--encoding`.

### Changed

- **`pubsub call` and `pubsub respond`: chunked Actions.** A UADP request or response larger than the NetworkMessage limit of the transport goes in chunks (Part 14 §7.2.4.4.4) and is reassembled on the other side. Before, such a call failed.

### Dependency

- **`github.com/otfabric/go-opcua`**: v1.10.0 → **[v1.11.0](https://github.com/otfabric/go-opcua/releases/tag/v1.11.0)**

---

## v0.6.0

**Date:** 2026-10-25  
**Previous release:** v0.5.1

### Summary

gzip compressed JSON PubSub over MQTT 5.0 (`application/json+gzip`, Part 14 §7.3.4.9.1), on **[go-opcua v1.10.0](https://github.com/otfabric/go-opcua/releases/tag/v1.10.0)**.

### Added

- **`--json-compression none|gzip`** on `pubsub publish`, `pubsub call`, `pubsub respond` and `pubsub config add-connection`: JSON messages over MQTT 5.0 go out gzip compressed with the Content Type `application/json+gzip` (data, metadata and action messages; on a server connection the go-opcua property `JsonCompression`). It needs `--encoding json` and an MQTT broker; a connection that ends up on MQTT 3.1.1 (`--mqtt-version BestAvailable`) sends uncompressed, with a warning.
- **`pubsub subscribe` and `pubsub decode` decompress gzip compressed JSON**: by the content type on MQTT 5.0, by the gzip header without one (MQTT 3.1.1), up to 16 MiB per message. The content type shown stays `application/json+gzip`. Before, such messages were reported as unsupported.

### Dependency

- **`github.com/otfabric/go-opcua`**: v1.9.1 → **[v1.10.0](https://github.com/otfabric/go-opcua/releases/tag/v1.10.0)**

---

## v0.5.1

**Date:** 2026-10-24  
**Previous release:** v0.5.0

### Summary

Patch release: bump to **[go-opcua v1.9.1](https://github.com/otfabric/go-opcua/releases/tag/v1.9.1)** for its PubSub, MQTT, NodeId and secure channel fixes. No CLI flags, commands or output formats changed.

### Dependency

- **`github.com/otfabric/go-opcua`**: v1.9.0 → **[v1.9.1](https://github.com/otfabric/go-opcua/releases/tag/v1.9.1)**
- No source changes were needed.

### What the library update brings to the CLI

- **`pubsub subscribe` with discovery:** a reader that learns its DataSetMetaData from an announcement keeps the sequence numbers of its writers, so a late or duplicated datagram is no longer printed again out of order.
- **`pubsub publish`:** the first message of a writer group is sent once the connection's own subscriptions are in place, so a one-shot publish to a group the same process listens on is not lost.
- **SKS key timing:** a key from a Security Key Service changes when the SKS changes it, so `pubsub subscribe --security-group` verifies messages right after a key change.
- **MQTT:** two connections with one client identifier no longer take the session from each other in a tight loop; the client waits its back-off after the broker disconnects it.
- **NodeIds with `nsu=`:** a namespace URI with a `;` or `%` is escaped in the JSON and XML encodings (`--encoding json|xml`) and read back correctly.
- **Servers closing channels:** a go-opcua server now ends a secure channel with an Error message (`Bad_SecureChannelClosed`), so `monitor` and other long-running commands report why the server closed the connection.

---

## v0.5.0

**Date:** 2026-10-24  
**Previous release:** v0.4.1  

### Summary

Feature release on **[go-opcua v1.9.0](https://github.com/otfabric/go-opcua/releases/tag/v1.9.0)**, which brings the PubSub transports and features of OPC UA Part 14 that v0.4 lacked. The `pubsub` commands now speak raw Ethernet, MQTT 5.0 and AMQP 1.0. They send and reassemble chunked NetworkMessages, probe for publishers and answer probes with UADP discovery (and a new `pubsub discover` command), call PubSub Actions and answer them (`pubsub call`, `pubsub respond`), and count what they do with the Part 14 diagnostics counters. `pubsub config diagnostics` reads the diagnostics objects of a server.

Existing commands and output formats are unchanged. The `--mqtt-*` broker flags are deprecated in favour of `--broker-*` but keep working.

### Dependency

- **`github.com/otfabric/go-opcua`**: v1.8.1 → **[v1.9.0](https://github.com/otfabric/go-opcua/releases/tag/v1.9.0)**: PubSub Ethernet, MQTT 5.0 and AMQP 1.0 transports, chunked NetworkMessages, UADP discovery, Part 14 diagnostics counters, and the PubSub diagnostics objects in the server. PubSub Actions (`PubSub.CallAction`, `PublishedActions`, `WithActionHandler`, `ActionOptions`).

### Added: transports

- **Ethernet (`opc.eth://MAC[:VID[.PCP]]`)**: UADP in Ethernet frames, optionally VLAN tagged, on Linux, macOS and FreeBSD. `--interface` is required. A missing raw-socket permission gets a hint (run as root, or `sudo setcap cap_net_raw+ep "$(command -v opcuactl)"`); JSON over Ethernet (or UDP) is rejected with a clear error.
- **AMQP 1.0 (`amqp://`, `amqps://`)**: UADP and JSON through brokers such as RabbitMQ 4 (`--topic /queues/<name>`, vhost via `--transport-property connection-hostname=vhost:<name>`) and ActiveMQ Artemis (plain addresses, anycast with `source-capabilities=queue` / `target-capabilities=queue`).
- **MQTT 5.0**: `--mqtt-version 3.1.1|5.0|BestAvailable` (default 3.1.1), `--qos 0|1|2` (QoS 2 is exactly-once on MQTT, at-least-once on AMQP).
- **Broker credentials**: `--broker-username`, `--broker-password` (or `OPCUACTL_BROKER_PASSWORD`; `OPCUACTL_MQTT_PASSWORD` still works) and `--broker-client-id` (MQTT client id, AMQP container-id) for MQTT and AMQP. `--mqtt-username`, `--mqtt-password` and `--mqtt-client-id` remain as deprecated aliases. `--username` and `--password` stay with the OPC UA session.
- **`--transport-property key=value`** (repeatable): connection properties for the transport (MQTT 5.0 `connection-*`, AMQP capabilities and hostname, ...).

### Added: messages

- **Broker message headers in `subscribe`**: each line (and the JSON output) shows the topic, content type, UAMessageType and user properties (MQTT 5.0 user properties, AMQP application properties). The decoder is picked by content type (`application/opcua+uadp`, `application/json`), with detection as the fallback.
- **`publish` sets the UAMessageType** of its data and metadata messages (`ua-data`, `ua-metadata`); the transport fills the content type.
- **Chunked NetworkMessages (Part 14 §7.2.4.4.4)**: `publish --max-message-size N` (default: the transport's limit) splits larger UADP messages into chunks, each secured on its own. `subscribe` reassembles them, bounded by `--chunk-max-message-size`, `--chunk-max-pending`, `--chunk-max-pending-bytes` and `--chunk-timeout`. `decode` accepts several files, so chunks captured separately reassemble too.

### Added: discovery

- **`pubsub discover`**: sends UADP discovery probes (`--probe applications|endpoints|metadata|writers|group|connections`, filtered by `--publisher-id`, `--writer-id`, `--writer-group-id`, `--transport-profile`, with `--include-writers` / `--include-groups`) to a multicast group, an Ethernet address or a DiscoveryAddress, and prints the announcements for `--duration` (`--listen-url` adds addresses to listen on).
- **`subscribe --probe`**: probes for the DataSetMetaData of writers seen without one, with a growing back-off. `--publisher-id` and `--writer-id` without metadata probe right away.
- **`publish --answer-probes`**: answers probes for its application state, DataSetMetaData, writer and group configuration and connection (endpoint probes get Bad_NotSupported: it has no server), rate-limited per probe. `--announce-interval` repeats the DataSetMetaData announcement (or the `--metadata-topic` message on a broker).
- **`--discovery-url`** sets the DiscoveryAddress (default: `--url` if it is a multicast group or Ethernet address); **`--discovery-layout spec|dotnet`** speaks the probe layout of the OPC Foundation .NET stack 2.x.

### Added: PubSub Actions

- **`pubsub call`**: a Requestor for PubSub Actions (Part 14 §6.2.11.2 in 1.05). It calls the Action target addressed by `--publisher-id`, `--writer-id` and `--target-id` with typed `--input Name:Type=Value` fields and prints the status and outputs of the response as text, JSON or YAML (`--quiet`: the output values). `--output Name:Type` names the outputs. Exit code 0 for a Good result, 3 for a Bad result, 2 when no response arrives within `--timeout` (the TimeoutHint of the request).
- **`pubsub respond`**: a test Responder with a PublishedAction of `--target ID[:Name]` targets and the request DataSetMetaData of `--input Name:Type`. It answers with the fixed `--output Name:Type=Value` values or echoes the inputs, optionally with another `--status` or after a `--delay`, and prints every request it serves (JSON Lines with `--jsonl`) until Ctrl+C, `--count` or `--duration`. `--max-concurrent`, `--rate`, `--burst` and `--max-timeout` set its ActionOptions.
- **Transports**: UDP (UADP, optionally signed and encrypted with `--message-security` and the keys of `--security-group` or `--keys-file`), MQTT 3.1.1 / 5.0 and AMQP 1.0 (UADP and JSON), with the transport and broker flags of the other `pubsub` commands. On a broker both commands default to the request queue `<prefix>/<encoding>/action-request/<publisher-id>/<writer-group-name>` (`--topic-prefix`, `--writer-group-name`; `--request-address` and `--response-address` override it); over UDP `call` sends to `--request-address` and listens on `--url`, with `--resend-interval` for lossy networks.
- **Action messages in `subscribe` and `decode`**: ActionRequest and ActionResponse NetworkMessages (UADP and JSON) are shown as `action-request` and `action-response` DataSetMessages with their ActionTargetId, RequestId, ActionState, RequestorId, TimeoutHint, ResponseAddress and CorrelationData, and the status of the response.
- **Diagnostics**: `--diagnostics` on `call` and `respond` prints the Action counters of the runtime (ActionRequestsSent, ActionResponsesReceived, ActionTimeouts; ActionRequestsReceived, ActionRequestsRejected, ActionResponsesSent).

### Added: diagnostics

- **`--diagnostics`, `--diagnostics-interval`, `--diagnostics-level`** on `subscribe`, `publish` and `discover`: the Part 14 diagnostics counters (§9.1.11) of the component the command plays (SentNetworkMessages, FailedTransmissions, ReceivedNetworkMessages, ReceivedInvalidNetworkMessages, DecryptionErrors, ...) plus chunk and discovery counters, with totals by classification. They are printed on stderr on exit and every interval, as text or a JSON line with `--format json`.
- **`pubsub config diagnostics [--node X] [--reset]`**: reads the diagnostics object of a server's PublishSubscribe object, connection, group, writer or reader: DiagnosticsLevel, totals, SubError, every counter with classification, level, Active and TimeFirstChange, and the LiveValues. `--reset` calls the Reset method first.

### Changed

- **`pubsub config add-connection`** accepts Ethernet, MQTT and AMQP connections, with `--interface`, `--mqtt-version` and `--transport-property` (written as ConnectionProperties).
- **`pubsub decode`** takes several files.

### Not supported yet

- Adding a PublishedAction to a server with `pubsub config`: Part 14 defines no method for it (`config show` lists a server's PublishedActions as data sets). PublishedActionMethod targets, Actions discovery announcements and ActionMetaData messages, and chunked action messages are not supported by go-opcua either.
- The gzip-compressed JSON content type.

### Documentation

- **README**: transports table, broker credentials, chunks, discovery and diagnostics; Features list and Command Reference updated.
- **README**: PubSub Actions section with the transports, defaults and flags of `pubsub call` and `pubsub respond`.
- **RECIPES**: new recipes 17 to 21 for MQTT 5.0 and AMQP brokers, Ethernet, chunks, discovery and diagnostics; recipe 22 for PubSub Actions through an MQTT broker and over UDP.
- **EXAMPLES**: live captures against Mosquitto, RabbitMQ, Artemis, raw Ethernet between Linux containers, and the go-opcua v1.9.0 PubSub example server; `pubsub call` against `pubsub respond` over UDP and through Mosquitto.

---

## v0.4.1

**Date:** 2026-10-24  
**Previous release:** v0.4.0

### Summary

Patch release: bump to **[go-opcua v1.8.1](https://github.com/otfabric/go-opcua/releases/tag/v1.8.1)** for its secure channel, client and interoperability fixes. No CLI flags, commands or output formats changed.

### Dependency

- **`github.com/otfabric/go-opcua`**: v1.7.0 → **[v1.8.1](https://github.com/otfabric/go-opcua/releases/tag/v1.8.1)**
- No source changes were needed.

### What the library update brings to the CLI

- **Secured channels stay up (v1.8.0, v1.8.1):** a request that fails to encode or is cancelled no longer leaves a gap in the sequence numbers (the server then refused every later chunk), and a security token replaced early is not renewed a second time (servers such as opcua-asyncio closed the channel). This matters for long-running `monitor` and `export` sessions.
- **Aes256_Sha256_RsaPss with UA-.NETStandard servers (v1.8.1):** the asymmetric block size is correct, so connecting with `--security-policy Aes256_Sha256_RsaPss` to an OPC Foundation .NET server works.
- **Fewer spurious reconnects (v1.8.0):** a fault for a session the client already left, or a stale `Bad_NoSubscription`, no longer reconnects or recreates subscriptions of `monitor` commands.
- **Republish recovery (v1.8.1):** a server that answers Republish without the requested message no longer makes `monitor` loop on recovery.
- **Errors instead of panics (v1.8.0):** malformed browse and attribute responses are reported as errors.
- **PubSub keys (v1.7.1):** keys from a Security Key Service are refetched in time, so `pubsub subscribe --security-group` keeps verifying messages across key changes.

### Documentation

- **README**: go-opcua's interop suite now covers six OPC UA stacks.
- **RECIPES**: aligned with v0.4.1.

---

## v0.4.0

**Date:** 2026-10-24  
**Previous release:** v0.3.0

### Summary

Feature release on **[go-opcua v1.7.0](https://github.com/otfabric/go-opcua/releases/tag/v1.7.0)**: a new `pubsub` command group brings OPC UA PubSub (Part 14) to the CLI — listen to and publish UADP and JSON NetworkMessages over UDP and MQTT, decode captured messages, verify and decrypt secured messages with keys from a Security Key Service, and show and change the PubSub configuration of a server through its PublishSubscribe methods. All of it is covered by end-to-end tests against the go-opcua PubSub runtime and server.

Existing commands, flags and output formats are unchanged.

### Added — `pubsub` commands

- **`pubsub subscribe`**: listens on `opc.udp://host:port` (unicast, or a multicast group with `--interface`) or an MQTT broker (`mqtt://`, `mqtts://`, `--topic` with wildcards, `--qos`, `--mqtt-username` / `--mqtt-password` / `OPCUACTL_MQTT_PASSWORD`, `--mqtt-client-id`) and prints one line per DataSetMessage (publisher, writer group, writer, sequence number, message type, security, fields), or JSON Lines with `--jsonl` / `--format json`. UADP and JSON are detected per message (`--encoding` forces one); `--publisher-id`, `--writer-group-id` and `--writer-id` filter; `--count`, `--duration` or Ctrl+C stop it. Undecodable messages are reported on stderr (`--show-errors` for all of them).
- **DataSetMetaData for field names and RawData types:** `--field Name:Type` (repeatable), `--metadata` (a `ua-metadata` message or a `DataSetMetaDataType` in the OPC UA JSON encoding), `--metadata-node` (the `DataSetMetaData` of a PublishedDataSet or DataSetReader, read from the server at `--endpoint`), and the metadata publishers announce on the wire (`ua-metadata` messages, UADP DataSetMetaData announcements), which is learned and used for later messages; `--show-metadata` prints the announcements and `ua-status` messages.
- **`pubsub publish`**: sends a key-frame DataSet built from `--field Name:Type=Value` (all scalar built-in types) as UADP (UDP or MQTT) or JSON (MQTT), with the Variant, DataValue or RawData field encoding (`--field-encoding`), `--publisher-id` (`N`, `byte:N`, `u32:N`, `u64:N` or a string), `--writer-group-id`, `--writer-id`, `--interval`, `--count` (default 1, 0 = until Ctrl+C) and `--duration`. `--announce-metadata` (UDP) and `--metadata-topic` (MQTT, retained) announce the DataSetMetaData first.
- **`pubsub decode [file|-]`**: decodes one captured message — raw bytes, hex or base64 (`--input`, detected by default) — as ua-data, ua-metadata, ua-status or a UADP metadata announcement, with the same metadata, filter and key flags as `subscribe`.
- **Message security:** `--security-group` pulls the keys of a SecurityGroup from the Security Key Service at `--endpoint` (GetSecurityKeys over the session flags, refreshed before they rotate); `--keys-file` reads saved keys instead. Subscribers verify and decrypt Sign / SignAndEncrypt UADP messages (PubSub-Aes128-CTR, PubSub-Aes256-CTR) and reject messages weaker than `--message-security`; publishers sign or sign and encrypt.
- **`pubsub sks get-keys`**: GetSecurityKeys for `--security-group` (`--starting-token`, `--future-keys`): security policy, token ids, key lengths, TimeToNextKey and KeyLifetime. Key material only with `--show-keys`; its JSON output is the `--keys-file` format.
- **`pubsub config`** (any Part 14 server: go-opcua with `server.WithPubSub`, open62541):
  - `show`: the PublishSubscribe tree — connections, writer groups, writers, reader groups, readers and published data sets with NodeIds, states and main properties (PublisherId, transport, WriterGroupId, PublishingInterval, security, DataSetWriterId, field lists); `--format json` nests them.
  - `add-connection` (UDP or MQTT, UADP or JSON), `add-dataset` (AddPublishedDataItems, with the per-variable status), `add-writer-group` (UADP or JSON, `--queue`, `--keep-alive`, `--message-security` / `--security-group`), `add-writer` (`--key-frame-count`, `--field-encoding`, `--queue`, `--metadata-queue`), `add-reader-group`, `add-reader` (filters, metadata flags, `--target` variables, `--message-receive-timeout`); `-q` prints only the new NodeId.
  - `remove connection|group|writer|reader|dataset`, `enable` and `disable` (a component, or the whole PubSub without `--node`).

### Dependency

- **`github.com/otfabric/go-opcua`**: v1.6.0 → **[v1.7.0](https://github.com/otfabric/go-opcua/releases/tag/v1.7.0)**. No new module dependencies: the MQTT client is part of go-opcua. opcuactl remains a pure-Go, statically linked binary.

### What the library update brings to existing commands

- **Reverse connect:** a server that was refused while opcuactl waited is reported, instead of only "context canceled" at the timeout.
- **`call method` against go-opcua v1.7.0 servers:** they accept subtypes of the declared argument types (IntegerId, Duration, …) and empty arrays.
- **Structures:** several generated structures whose Go name differs from the OPC UA name (`ReadValueID`, `NetworkAddressURLDataType`, …) are now registered, so they decode instead of arriving empty.

### Fixed

- **Silent failures:** a command that failed on a missing or invalid flag, an unknown flag or an unknown command exited with status 1 without a message. Every error is now printed on stderr, once.

### Tests

- Every command is checked to print exactly one error line when it fails.
- End-to-end tests run the CLI in-process against the go-opcua PubSub runtime and server: UADP over UDP in both directions (Variant, DataValue and RawData fields, metadata announcements, filters), JSON and UADP through an in-test MQTT 3.1.1 broker (retained metadata, QoS 1), a Security Key Service with a Basic256Sha256 SignAndEncrypt session and a username identity (GetSecurityKeys, refused callers, Aes256-CTR secured messages both ways, keys files), and the configuration methods of a server with `WithPubSub` (a reader writing what a writer publishes to a target variable, enable / disable, removal, error paths).
- Unit tests for the new parsers (built-in types, field specs, PublisherIds, message security, encodings) and output helpers.

### Known limitations

- What go-opcua v1.7.0 does not implement is not available: Ethernet (`opc.eth://`) and AMQP transports, chunked NetworkMessages, UADP discovery requests.
- `pubsub publish` sends key frames only; its sequence numbers start at 1 with every run.
- UDP multicast needs a route for the group; on macOS loopback-only setups pass `--interface lo0`.

### Documentation

- **README**: PubSub feature overview, command tree, usage examples and a PubSub section (transports, PublisherIds, metadata, message security, configuration).
- **EXAMPLES**: live captures of `pubsub config`, `subscribe`, `publish` and `decode` against the go-opcua v1.7.0 PubSub example server.
- **RECIPES**: finding out what a publisher sends, secured messages with keys from an SKS, testing a subscriber, reconfiguring a server's PubSub.

---

## v0.3.0

**Date:** 2026-10-12  
**Previous release:** v0.2.1

### Summary

Feature release on **[go-opcua v1.6.0](https://github.com/otfabric/go-opcua/releases/tag/v1.6.0)**: the CLI gains the ECC security policies, reverse connect, Local Discovery Server and mDNS queries, data-change filters (deadband, trigger) and the OPC UA JSON / XML data encodings. All new functionality is covered by end-to-end tests that run the CLI against a go-opcua server.

Existing commands, flags and output formats keep working; see **Behaviour changes** for the three things that differ.

### Behaviour changes

- **Minimum Go version is 1.26** for `go install` and source builds (required by go-opcua v1.6.0). Pre-built binaries are unaffected.
- **An explicit secure `--security-policy` now looks up the server certificate.** `--security-policy Basic256Sha256 --security-mode SignAndEncrypt` used to fail unless `--server-cert` was given; opcuactl now takes the certificate (and the user-token PolicyId) from the matching endpoint, as `auto` does. This costs one GetEndpoints round trip; pass `--server-cert` to skip it.
- **`auto` and `discover endpoints --best` respect the key type of `--cert`.** With a client certificate only endpoints that key can use are considered: an RSA certificate no longer selects an ECC endpoint it cannot open. `--best` with `--mode` alone no longer assumes `Basic256Sha256`.
- `discover servers --on-network` has a new first table column `RECORD` (JSON output is unchanged).

### Added — security

- **ECC security policies:** `ECC_nistP256`, `ECC_nistP384`, `ECC_nistP256_AesGcm`, `ECC_nistP256_ChaChaPoly` and `ECC_curve25519` for `--security-policy`, `discover endpoints --policy` and shell completion.
- **ECDSA and Ed25519 certificates** for `--cert` / `--key`, for the secure channel and for `--auth certificate` (previously RSA only).

### Added — reverse connect

- **`--reverse-listen host:port`** (`OPCUACTL_REVERSE_LISTEN`): opcuactl listens and waits for the server to connect, for servers behind a firewall or NAT. Works with every command, including discovery and secured sessions. `--endpoint` becomes optional: the endpoint URL announced by the server is used.
- **`--reverse-server-uri`** (`OPCUACTL_REVERSE_SERVER_URI`): only accept the server announcing this ApplicationUri.
- `--timeout` bounds the wait for the server. `ping` skips its TCP stage with reverse connect.

### Added — discovery

- **`discover lds --host <host>`**: asks the Local Discovery Server of a host (port 4840 by default) for the servers registered with it and, with the multicast extension, the servers it found with mDNS.
- **`discover servers --on-network`**: `--capability` (repeatable server capability filter, e.g. `DA`, `HD`), `--start-record` and `--max-records` for paging.

### Added — monitoring

- **DataChangeFilter on `monitor value`:** `--trigger status|status-value|status-value-timestamp`, `--deadband-type none|absolute|percent`, `--deadband <value>`.
- **`monitor value --index-range`** to monitor part of an array.
- Monitored items the server rejects are reported on stderr with their status code while the others keep running; the command exits with code 3 when none could be created. Before, a rejected item was silently never reported.

### Added — data encoding

- **`get value --ua-encoding json|json-verbose|json-reversible|json-nonreversible|xml`** prints DataValues in the OPC UA JSON (Part 6 §5.4) or XML (§5.3) DataEncoding; `--pretty` indents.
- **`write value --type ua-json|ua-xml`** takes a Variant in those encodings, which reaches every data type (arrays, LocalizedText, structures).

### Fixed

- A failed connection attempt now releases its resources before the command returns.

### Dependency

- **`github.com/otfabric/go-opcua`**: v1.5.0 → **[v1.6.0](https://github.com/otfabric/go-opcua/releases/tag/v1.6.0)**
- **Go**: 1.25 → 1.26; CI runs on Go 1.26 and 1.27.
- `golang.org/x/crypto` v0.58.0 (new, indirect; ChaCha20-Poly1305 for the ECC policies), `golang.org/x/sys` v0.49.0, `golang.org/x/term` v0.47.0. opcuactl remains a pure-Go, statically linked binary.

### What the library update brings to existing commands

- **Stricter channel security:** sequence numbers of secured channels are verified, so replayed or reordered messages are rejected.
- **Subscriptions survive more failures:** `monitor` sessions recreate subscriptions that went silent or lost their session, and no longer deliver a republished notification twice.
- **Clearer errors:** a server's error at OpenSecureChannel surfaces with its status code instead of `EOF`; malformed responses no longer crash the client.

### Documentation

- **README**: security policy table, reverse connect, data-change filters, OPC UA JSON / XML encodings, new flags.
- **EXAMPLES**: live captures of the new features against a go-opcua v1.6.0 server.
- **RECIPES**: ECC and reverse-connect connection presets, LDS discovery, deadband monitoring, copying a value between servers in the OPC UA JSON encoding.

---

## v0.2.1

**Date:** 2026-10-11  
**Previous release:** v0.2.0

### Summary

Patch release: bump to **[go-opcua v1.5.0](https://github.com/otfabric/go-opcua/releases/tag/v1.5.0)** for its client hardening (reconnect, subscriptions, handshake timeouts, SecureChannel tokens), and rework the README with installation instructions for Linux, macOS and Windows. No CLI flags, commands or output formats changed.

### Dependency

- **`github.com/otfabric/go-opcua`**: v1.3.1 → **[v1.5.0](https://github.com/otfabric/go-opcua/releases/tag/v1.5.0)**
- **`golang.org/x/sys`** (indirect): v0.42.0 → v0.44.0 (fixes [GO-2026-5024](https://pkg.go.dev/vuln/GO-2026-5024); Windows-only code path)
- No source changes were needed.

### What the library update brings to the CLI

- **Reconnect and subscriptions (v1.5.0):** `monitor value|event|alarm` sessions that use auto-reconnect no longer come back "connected" with dead subscriptions after a connection drop; subscriptions that expire on the server are recreated.
- **No more hangs on a silent peer (v1.5.0):** the HEL/ACK handshake honours the timeout, so connect and close return when a host accepts TCP but never answers.
- **SecureChannel token handling (v1.5.0):** renewal is race-free and superseded tokens are retired, which matters for long-running `monitor` and `export` sessions on secured endpoints.
- **Quiet library logging (v1.4.0):** go-opcua no longer writes to `slog.Default()`, so library log lines do not leak onto stderr.
- **Schema UA-1.05.07 (v1.5.0):** new status codes (`UncertainOverRange`, `UncertainUnderRange`) and node ids are known by name.

### Documentation

- **README**: installation instructions for Linux, macOS and Windows (pre-built binaries with checksum verification), "Why opcuactl?", use cases and an FAQ.

---

## v0.2.0

**Date:** 2026-07-25  
**Previous release:** v0.1.1

### Summary

Minor release for operators: bump to **[go-opcua v1.3.1](https://github.com/otfabric/go-opcua/releases/tag/v1.3.1)**, finish incomplete CLI flags against that API, and expose the interop-era client capabilities — IndexRange reads, event WhereClause filters, monitored-item queue windows, history attime/processed aggregates, export history mode parity, and full authN/Z coverage so `opcuactl` can talk to as many servers as possible.

### Dependency

- **`github.com/otfabric/go-opcua`**: v1.0.2 → **v1.3.1**
- No intentional public API signature breaks for the client helpers opcuactl already used (`FindServers` / `GetEndpoints`, `Read`/`Write`, `NewSubscription` / `MonitorEvents`, `HistoryRead*`)
- Behavioural notes from go-opcua v1.3.0 still apply to peers: Write EncodingMask is value-only; subscription queues / monitoring modes follow Part 4 against go-opcua servers

### Authentication and security

- **`--auth anonymous|username|certificate|issued`** — all go-opcua user identity modes (`issued` is new; aliases `issuedtoken` / `token` accepted)
- **`--token` / `--token-file`** — issued-token data (raw or base64)
- **`--auth-policy-id`** — optional override when not using security `auto`
- **`--trusted-cert`** (repeatable) — trust private CAs / self-signed server certs
- **`--security-policy auto` / `--security-mode auto`** — discover endpoints and apply the best matching secure configuration (including user-token PolicyId)
- Certificate auth presents both the secure-channel cert/key and the X.509 user identity token
- `--timeout` also drives dial and request timeouts (`DialTimeout` / `RequestTimeout`)
- Connection-related env vars apply when the matching flag is not set on the CLI, including defaults such as `auth` / `security-*`:
  `OPCUACTL_ENDPOINT`, `OPCUACTL_AUTH`, `OPCUACTL_USERNAME`, `OPCUACTL_PASSWORD`, `OPCUACTL_AUTH_POLICY_ID`, `OPCUACTL_TOKEN`, `OPCUACTL_TOKEN_FILE`, `OPCUACTL_CERT`, `OPCUACTL_KEY`, `OPCUACTL_SERVER_CERT`, `OPCUACTL_TRUSTED_CERT` (CSV), `OPCUACTL_SECURITY_POLICY`, `OPCUACTL_SECURITY_MODE`, `OPCUACTL_TIMEOUT`, `OPCUACTL_FORMAT`, `OPCUACTL_NAMESPACE`

### Correctness fixes

- **`monitor value --queue-size`** — previously logged only; now applied via `MonitorItems` / `RequestedParameters.QueueSize`
- **`history value --mode attime`** — was advertised but rejected; now implemented with `Client.HistoryReadAtTime` and repeatable `--at`
- **`history value --mode processed`** — now sets `ReadProcessedDetails.AggregateType` (default Average) via `--aggregate`

### New and improved commands

#### `get value`
- **`--index-range`** — array/matrix Value subset reads via `ReadRequest.IndexRange`
- **`--timestamps-to-return`** — `source|server|both|neither` (server-side); `--timestamps` still controls display

#### `monitor value`
- **`--discard-oldest`** (new; default `true`) and **`--queue-size`** are applied to monitored-item create parameters (Part 4 window semantics)
- **Overflow** is surfaced in table (`[OVERFLOW]`) and JSON (`overflow: true`) when the DataValue Overflow InfoBit is set

#### `monitor event`
- **`--of-type <NodeId>`**, **`--severity-min`**, **`--severity-max`** build a WhereClause via `ua.NewEventFilter` / `ua.OfType` / Severity comparisons

#### `history value` / `export values --history`
- **`--mode attime`** with repeatable **`--at`** RFC3339 timestamps (`HistoryReadAtTime`; `--start`/`--end` not required)
- **`--mode processed`** requires **`--interval`** and accepts **`--aggregate average|min|max|count`** (`AggregateType`)
- Export history supports the same `raw|modified|processed|attime` modes (including `--at` / `--aggregate` / `--interval`)

### Documentation

- [README.md](README.md) — security/auth table, global flags, IndexRange / history / monitor examples, tables of contents
- [RECIPES.md](RECIPES.md) — connection presets for all auth modes, env-var recipe, history/export and event-filter workflows
- [EXAMPLES.md](EXAMPLES.md) — still captured against a WAGO PLC (`None`/`anonymous`); adds a **v0.2.0 syntax** section for secure auth, IndexRange, event filters, and history modes not available on that device

### Packaging

- MIT license retained; `// SPDX-License-Identifier: MIT` on first-party Go sources
- README badge block standardized (Go version, pkg.go.dev, License, CI, Go Report Card, tokenless Codecov, Release)

### Compatibility

No intentional breaking CLI flag removals. Defaults preserve prior behaviour (`auth=anonymous`, `security-policy/mode=None`, `queue-size=10`, `discard-oldest=true`, history mode `raw`, aggregate `average`, `timestamps-to-return=both`). CLI flag semantics for the three correctness fixes above become effective. Certificate auth now also presents an X.509 identity token (required by servers that advertise `UserTokenTypeCertificate`).

---

## v0.1.1

**Date:** 2026-03-25  
**Previous release:** v0.1.0

### Summary

This release migrates the CLI to the renamed **go-opcua** module (v1.0.2), aligns with upstream v1 API moves, and replaces file-based versioning with **link-time `main.*` build metadata** (including optional **tag** in `opcuactl version`). CI and release automation are updated to shared organization workflows.

### Dependency and imports

- **Primary library:** `github.com/otfabric/opcua` → [`github.com/otfabric/go-opcua`](https://github.com/otfabric/go-opcua) **v1.0.2**.
- Upstream v1.0+ uses idiomatic generated identifiers (CamelCase, Go initialisms). This repo did not rely on legacy `id.Foo_Bar` names; no mechanical renames were required in application code beyond import paths.

### API adjustments for go-opcua v1

The following symbols are used from the **`ua`** package (or moved there) instead of the root client package:

| Area | Change |
|------|--------|
| Symbolic standard node names | `opcua.StandardNodeID` → **`ua.StandardNodeID`** (`internal/parse`) |
| Endpoint selection | `opcua.SelectEndpoint` → **`ua.SelectEndpoint`** (`discover`) |
| Display helpers | `opcua.DataTypeDisplayName`, `TypeDefinitionDisplayName`, `ReferenceTypeDisplayName` → **`ua.*`** (`browse`, `export`, `find`, `get`, `vars`, `write`) |

### Version and build metadata

- **Removed** root **`version.txt`**. Version strings for local/release builds come from Git and the linker.
- **`main` package** defines `version`, `tag`, `commit`, `buildDate`; **`init`** calls `opcuactl.SetBuildInfo` so the `version` subcommand stays in `cmd/opcuactl`.
- **Makefile** `LDFLAGS`: **`-s -w`** plus  
  `-X main.version=… -X main.tag=… -X main.commit=… -X main.buildDate=…`  
  **`VERSION`** defaults to `git describe --tags --always --dirty` (or `dev`); **`TAG`** to a tag pointing at `HEAD` when present.
- **`opcuactl version`**: table and JSON output include **`tag`** when non-empty.

### CI and release

- **CI:** reusable workflow **`otfabric/.github/.github/workflows/go-ci.yml@v2`** (Go **1.25** and **1.26**).
- **Release:** **`release.yml`** dispatches **`go-binary-release.yml@v2`** with the `main.*` ldflags above and **`run-tests: true`**.

### Documentation

- **README** links updated to **otfabric/go-opcua**.
- **EXAMPLES** version sample updated for current `version` output.

### Migration (for forks or custom builds)

1. Run **`go mod tidy`**; require **`github.com/otfabric/go-opcua v1.0.2`** (or newer compatible v1).
2. Replace imports **`github.com/otfabric/opcua/...`** with **`github.com/otfabric/go-opcua/...`**.
3. When bulk-replacing paths, **do not** substitute the substring inside **`github.com/otfabric/opcuactl`** (it contains `opcua` and would corrupt the module path).
4. Point release/build scripts at **`main.version` / `main.tag` / `main.commit` / `main.buildDate`** instead of `cmd/opcuactl.*` or `version.txt`.

---
