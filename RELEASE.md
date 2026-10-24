# opcuactl Releases

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

### Tests

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
