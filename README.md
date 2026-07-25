# opcuactl

[![Go](https://img.shields.io/badge/Go-1.25%2B-00ADD8?style=flat&logo=go)](https://go.dev/)
[![Go Reference](https://pkg.go.dev/badge/github.com/otfabric/opcuactl.svg)](https://pkg.go.dev/github.com/otfabric/opcuactl)
[![License](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![CI](https://github.com/otfabric/opcuactl/actions/workflows/ci.yml/badge.svg)](https://github.com/otfabric/opcuactl/actions/workflows/ci.yml)
[![Codecov](https://codecov.io/gh/otfabric/opcuactl/graph/badge.svg)](https://codecov.io/gh/otfabric/opcuactl)
[![Release](https://img.shields.io/github/v/release/otfabric/opcuactl?label=release)](https://github.com/otfabric/opcuactl/releases)

A production-grade command-line tool for OPC UA, built in Go on top of [otfabric/go-opcua](https://github.com/otfabric/go-opcua).

`opcuactl` provides operator-focused access to OPC UA servers for discovery, browsing, reading, writing, method calls, real-time monitoring, historical data retrieval, and bulk export — all from the terminal.

## Table of Contents

- [Features](#features)
- [Installation](#installation)
- [Quick Start](#quick-start)
- [Command Reference](#command-reference)
- [Usage Examples](#usage-examples)
- [Global Flags](#global-flags)
- [Output Formats](#output-formats)
- [Exit Codes](#exit-codes)
- [Security](#security)
- [NodeID Formats](#nodeid-formats)
- [Path Resolution](#path-resolution)
- [Monitoring Intervals](#monitoring-intervals)
- [Development](#development)
- [Supported Value Types](#supported-value-types)
- [Built With](#built-with)
- [Documentation](#documentation)
- [License](#license)
## Features

- **Discovery** — find servers on a network, list endpoints, select the most secure configuration
- **Browse** — explore address-space trees, child nodes, references, and resolve browse paths
- **Search** — find nodes by name, display name, type definition, data type, or namespace
- **Read** — read values (including array IndexRange), node summaries, attributes, and server status
- **Write** — write typed values with dry-run validation
- **Call** — invoke OPC UA methods with argument introspection
- **Monitor** — subscribe to live value changes, filtered events, and alarms (queue window + overflow)
- **History** — raw, modified, processed (aggregates), and at-time historical reads
- **Export** — bulk export node metadata and value snapshots or historical time-series
- **AuthN/Z** — anonymous, username, X.509, and issued-token identities; `auto` endpoint security selection

All commands support **table**, **JSON**, **YAML**, **CSV**, and **JSONL** output formats.

See [RELEASE.md](RELEASE.md) for what changed in the latest release.

## Installation

### From source

Requires Go 1.25+.

```sh
go install github.com/otfabric/opcuactl@latest
```

### Build from repository

```sh
git clone https://github.com/otfabric/opcuactl.git
cd opcuactl
make build
# binary: ./bin/opcuactl
```

### Cross-platform builds

```sh
make build-all
```

Produces binaries for linux/amd64, linux/arm64, linux/armv7, darwin/amd64, and darwin/arm64 in the `release/` directory.

## Quick Start

```sh
# Discover servers on a network
opcuactl discover servers -e opc.tcp://localhost:4840

# List endpoints and their security configurations
opcuactl discover endpoints -e opc.tcp://localhost:4840

# Read a value (anonymous / None — fine for lab servers)
opcuactl get value -e opc.tcp://localhost:4840 -n i=2258

# Browse the address space
opcuactl browse children -e opc.tcp://localhost:4840

# Monitor live value changes
opcuactl monitor value -e opc.tcp://localhost:4840 -n i=2258
```

For production servers, prefer secure connect with endpoint auto-selection:

```sh
opcuactl get value -e opc.tcp://plc.example:4840 -n i=2258 \
  --auth username --username operator --password "$OPCUACTL_PASSWORD" \
  --security-policy auto --security-mode auto \
  --cert ./pki/client.pem --key ./pki/client.key \
  --trusted-cert ./pki/server-ca.pem
```

More workflows: [RECIPES.md](RECIPES.md). Captured outputs: [EXAMPLES.md](EXAMPLES.md).
## Command Reference

```
opcuactl
├── discover
│   ├── servers        Find OPC UA servers via FindServers / FindServersOnNetwork
│   └── endpoints      List server endpoints and security configurations
├── browse
│   ├── children       List direct child nodes
│   ├── refs           Inspect reference edges of a node
│   ├── tree           Recursively print an address-space tree
│   └── path           Resolve dot-separated browse paths to NodeIDs
├── find
│   ├── namespace      List and search namespaces
│   ├── node           Search nodes by name, display name, or pattern
│   └── type           Find nodes by type definition or data type
├── get
│   ├── value          Read one or more node values
│   ├── node           Read full node summaries
│   ├── attr           Read arbitrary node attributes by name or ID
│   ├── vars           Collect variables under a node (with batch read)
│   └── server         Read server status and build info
├── write
│   └── value          Write a typed value to a node
├── call
│   └── method         Invoke an OPC UA method
├── monitor
│   ├── value          Subscribe to live data changes
│   ├── event          Subscribe to event notifications
│   └── alarm          Subscribe to alarm/condition events
├── history
│   ├── value          Read historical data values
│   └── event          Read historical events
├── export
│   ├── nodes          Export node metadata from a subtree
│   └── values         Export value snapshots or historical time-series
├── ping               Test connectivity to an OPC UA server
├── version            Print version, build, and runtime info
└── completion         Generate shell completion scripts (bash|zsh|fish|powershell)
```

## Usage Examples

### Discovery

```sh
# Find servers, including network discovery
opcuactl discover servers -e opc.tcp://localhost:4840 --on-network

# Find the most secure endpoint
opcuactl discover endpoints -e opc.tcp://localhost:4840 --best

# Filter endpoints by security policy
opcuactl discover endpoints -e opc.tcp://localhost:4840 --policy Basic256Sha256
```

### Browsing

```sh
# Browse children of the Objects folder (default)
opcuactl browse children -e opc.tcp://localhost:4840

# Browse children of a specific node, filter by class
opcuactl browse children -e opc.tcp://localhost:4840 -n "ns=2;i=1" --node-class variable

# Recursive tree from Objects, max depth 3
opcuactl browse tree -e opc.tcp://localhost:4840 --depth 3

# Resolve a browse path
opcuactl browse path -e opc.tcp://localhost:4840 --path Server.ServerStatus

# Resolve a path in a custom namespace
opcuactl browse path -e opc.tcp://localhost:4840 --path MyFolder.Sensor.Temperature --namespace 1

# List all references for a node
opcuactl browse refs -e opc.tcp://localhost:4840 -n i=2253 --direction both
```

### Reading Values and Nodes

```sh
# Read a single value
opcuactl get value -e opc.tcp://localhost:4840 -n i=2258

# Read multiple values with timestamps
opcuactl get value -e opc.tcp://localhost:4840 -n i=2258 -n "ns=2;s=Temperature" --timestamps

# Read an array subset (IndexRange)
opcuactl get value -e opc.tcp://localhost:4840 -n "ns=2;s=ArrayNode" --index-range "0:2"

# Ask the server for source timestamps only; still print them with --timestamps
opcuactl get value -e opc.tcp://localhost:4840 -n i=2258 \
  --timestamps-to-return source --timestamps

# Get a full node summary (with extended attributes)
opcuactl get node -e opc.tcp://localhost:4840 -n i=2258 --full

# Read a specific attribute by name
opcuactl get attr -e opc.tcp://localhost:4840 -n i=2258 --attr DisplayName

# Read multiple attributes
opcuactl get attr -e opc.tcp://localhost:4840 -n i=2258 --attr Value --attr DataType --attr AccessLevel

# Get server status
opcuactl get server -e opc.tcp://localhost:4840

# Collect all variables under a PLC object
opcuactl get vars -e opc.tcp://localhost:4840 -n "ns=3;s=PLC"

# Recursive variable collection with paths
opcuactl get vars -e opc.tcp://localhost:4840 -n "ns=2;i=5001" --recursive --paths

# Metadata only (no value reads)
opcuactl get vars -e opc.tcp://localhost:4840 -n "ns=3;s=PLC" --no-read

# Filter by namespace
opcuactl get vars -e opc.tcp://localhost:4840 -n "ns=2;i=5001" --recursive --only-ns 3
```

### Writing

```sh
# Write a double value
opcuactl write value -e opc.tcp://localhost:4840 -n "ns=2;s=Temperature" --value 42.5 --type double

# Write a boolean
opcuactl write value -e opc.tcp://localhost:4840 -n "ns=2;s=Active" --value true --type bool

# Write to an array element using index range
opcuactl write value -e opc.tcp://localhost:4840 -n "ns=2;s=ArrayNode" --value 99 --type int32 --index-range "0"

# Skip confirmation prompt
opcuactl write value -e opc.tcp://localhost:4840 -n "ns=2;s=Temperature" --value 42.5 --type double --yes

# Validate without writing
opcuactl write value -e opc.tcp://localhost:4840 -n "ns=2;s=Temperature" --value 42.5 --type double --dry-run
```

### Method Calls

```sh
# Introspect a method's signature
opcuactl call method -e opc.tcp://localhost:4840 --object "ns=2;i=1" --method "ns=2;i=2" --introspect

# Call a method with typed arguments
opcuactl call method -e opc.tcp://localhost:4840 --object "ns=2;i=1" --method "ns=2;i=2" \
  --arg 42 --arg hello --types int32,string

# Call a method with arguments as JSON
opcuactl call method -e opc.tcp://localhost:4840 --object "ns=2;i=1" --method "ns=2;i=2" \
  --arg-json '[42, "hello"]'

# Call a method with arguments from a file
opcuactl call method -e opc.tcp://localhost:4840 --object "ns=2;i=1" --method "ns=2;i=2" \
  --arg-file args.json
```

### Monitoring

```sh
# Monitor a value until Ctrl+C
opcuactl monitor value -e opc.tcp://localhost:4840 -n i=2258

# Monitor for 30 seconds with JSONL output
opcuactl monitor value -e opc.tcp://localhost:4840 -n i=2258 --jsonl --duration 30s

# Monitor with custom sampling interval and queue window
opcuactl monitor value -e opc.tcp://localhost:4840 -n i=2258 \
  --interval 1s --sampling 100ms --queue-size 10 --discard-oldest

# Overflow is marked in table output as [OVERFLOW] and in JSON as "overflow": true
# when the server sets the DataValue Overflow InfoBit

# Monitor events
opcuactl monitor event -e opc.tcp://localhost:4840 -n i=2253 \
  --select EventType --select Message --select Severity

# Monitor events with WhereClause filters (BaseEventType = i=2041)
opcuactl monitor event -e opc.tcp://localhost:4840 -n i=2253 \
  --of-type i=2041 --severity-min 500 --severity-max 1000

# Monitor alarms (event specialization with alarm-relevant defaults)
opcuactl monitor alarm -e opc.tcp://localhost:4840 -n i=2253
```

### Historical Data

```sh
# Read historical values (raw mode, default)
opcuactl history value -e opc.tcp://localhost:4840 -n "ns=2;s=Temperature" \
  --start 2026-03-10T00:00:00Z --end 2026-03-11T00:00:00Z

# Read modified historical values
opcuactl history value -e opc.tcp://localhost:4840 -n "ns=2;s=Temperature" \
  --start 2026-03-10T00:00:00Z --end 2026-03-11T00:00:00Z --mode modified

# Read processed (aggregated) historical values
opcuactl history value -e opc.tcp://localhost:4840 -n "ns=2;s=Temperature" \
  --start 2026-03-10T00:00:00Z --end 2026-03-11T00:00:00Z \
  --mode processed --interval 1h --aggregate average

# Read historical values at specific timestamps
opcuactl history value -e opc.tcp://localhost:4840 -n "ns=2;s=Temperature" \
  --mode attime --at 2026-03-10T12:00:00Z --at 2026-03-10T13:00:00Z

# Read historical events
opcuactl history event -e opc.tcp://localhost:4840 -n i=2253 \
  --start 2026-03-10T00:00:00Z --end 2026-03-11T00:00:00Z
```

### Export

```sh
# Export node metadata as JSON
opcuactl export nodes -e opc.tcp://localhost:4840 --depth 3 -f json

# Export nodes with extended attributes to a file
opcuactl export nodes -e opc.tcp://localhost:4840 --depth 3 -f json --include-attrs --output nodes.json

# Export current value snapshots
opcuactl export values -e opc.tcp://localhost:4840 -n i=2258 -n "ns=2;s=Temperature"

# Export values for nodes listed in a file
opcuactl export values -e opc.tcp://localhost:4840 --nodes-file nodes.txt --output values.csv -f csv

# Export historical values for nodes in a subtree
opcuactl export values -e opc.tcp://localhost:4840 --start-node "ns=2;i=1" --depth 2 \
  --history --start 2026-03-10T00:00:00Z --end 2026-03-11T00:00:00Z

# Export processed history
opcuactl export values -e opc.tcp://localhost:4840 -n "ns=2;s=Temperature" \
  --history --mode processed --interval 1h --aggregate average \
  --start 2026-03-10T00:00:00Z --end 2026-03-11T00:00:00Z

# Export at-time history (same modes as `history value`)
opcuactl export values -e opc.tcp://localhost:4840 -n "ns=2;s=Temperature" \
  --history --mode attime \
  --at 2026-03-10T12:00:00Z --at 2026-03-10T13:00:00Z
```

### Searching

```sh
# Find nodes by display name (substring)
opcuactl find node -e opc.tcp://localhost:4840 --display-name Temperature

# Find nodes by regex pattern
opcuactl find node -e opc.tcp://localhost:4840 --name "Temp.*" --regex --limit 10

# Find nodes by data type
opcuactl find type -e opc.tcp://localhost:4840 --data-type Double

# List all namespaces
opcuactl find namespace -e opc.tcp://localhost:4840

# Search namespaces by URI
opcuactl find namespace -e opc.tcp://localhost:4840 --filter example.org
```

## Global Flags

Every networked command supports these connection and output flags:

| Flag | Env Variable | Description |
|------|-------------|-------------|
| `-e, --endpoint` | `OPCUACTL_ENDPOINT` | OPC UA server endpoint (e.g. `opc.tcp://host:4840`) |
| `--security-policy` | `OPCUACTL_SECURITY_POLICY` | Security policy (`None`, `Basic256Sha256`, …, or `auto`) |
| `--security-mode` | `OPCUACTL_SECURITY_MODE` | Security mode (`None`, `Sign`, `SignAndEncrypt`, or `auto`) |
| `--auth` | `OPCUACTL_AUTH` | Auth mode (`anonymous`, `username`, `certificate`, `issued`) |
| `--username` | `OPCUACTL_USERNAME` | Username for username auth |
| `--password` | `OPCUACTL_PASSWORD` | Password for username auth |
| `--auth-policy-id` | `OPCUACTL_AUTH_POLICY_ID` | User token PolicyId (usually set via security `auto`) |
| `--token` | `OPCUACTL_TOKEN` | Issued token (raw or base64) for `--auth issued` |
| `--token-file` | `OPCUACTL_TOKEN_FILE` | Issued token file for `--auth issued` |
| `--cert` | `OPCUACTL_CERT` | Client certificate file (channel and/or certificate auth) |
| `--key` | `OPCUACTL_KEY` | Client private key file |
| `--server-cert` | `OPCUACTL_SERVER_CERT` | Server certificate file (overrides endpoint cert) |
| `--trusted-cert` | `OPCUACTL_TRUSTED_CERT` | Trusted CA / self-signed cert (repeatable; env is CSV) |
| `--insecure-skip-verify` | — | Skip server certificate verification (INSECURE) |
| `--timeout` | `OPCUACTL_TIMEOUT` | Dial / request / connect timeout (default: `10s`) |
| `--namespace` | `OPCUACTL_NAMESPACE` | Default namespace index for node IDs |
| `-f, --format` | `OPCUACTL_FORMAT` | Output format: `table`, `json`, `yaml`, `csv`, `jsonl` |
| `--pretty` | — | Pretty-print JSON / YAML output |
| `-v, --verbose` | — | Verbose diagnostics on stderr |
| `-q, --quiet` | — | Emit only the primary value (scripting) |

## Output Formats

| Format | Description |
|--------|-------------|
| `table` | Human-readable tabular output (default) |
| `json` | Stable JSON for scripts and automation |
| `yaml` | YAML output |
| `csv` | Comma-separated values |
| `jsonl` | Newline-delimited JSON |

All data goes to stdout, all errors go to stderr.

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Success |
| `1` | Usage or validation error |
| `2` | Connection or transport failure |
| `3` | OPC UA service or status-code failure |
| `4` | Partial success (batch/export operations) |

## Security

`opcuactl` supports the same client authN/Z options as [go-opcua](https://github.com/otfabric/go-opcua) so you can reach as many servers as possible.

| Concern | How to set it |
|---------|----------------|
| User identity | `--auth anonymous\|username\|certificate\|issued` |
| Channel crypto | `--security-policy` / `--security-mode` (`None` … or `auto`) |
| Client application cert | `--cert` + `--key` (required for Sign / SignAndEncrypt) |
| Trust the server | `--trusted-cert` (repeatable), `--server-cert`, or `--insecure-skip-verify` (dev only) |
| User token PolicyId | usually filled by `auto`; override with `--auth-policy-id` if needed |

**Recommended production pattern:** set `--security-policy auto --security-mode auto`, provide a client cert/key, and trust the server CA with `--trusted-cert`. `auto` discovers endpoints, picks the best match, and applies channel settings, remote certificate, and user-token PolicyId for you.

```sh
# 1) Inspect what the server offers
opcuactl discover endpoints -e opc.tcp://host:4840 --best -v

# 2) Username over auto-selected secure endpoint
opcuactl get value -e opc.tcp://host:4840 -n i=2258 \
  --auth username --username operator --password secret \
  --security-policy auto --security-mode auto \
  --cert client.pem --key client.key \
  --trusted-cert server-ca.pem

# Explicit policy/mode (skip auto) — keep username off None/None channels
opcuactl get value -e opc.tcp://host:4840 -n i=2258 \
  --auth username --username operator --password secret \
  --security-policy Basic256Sha256 --security-mode SignAndEncrypt \
  --cert client.pem --key client.key

# X.509 user identity (same cert used for channel + user token)
opcuactl get value -e opc.tcp://host:4840 -n i=2258 \
  --auth certificate --cert client.pem --key client.key \
  --security-policy auto --security-mode auto \
  --trusted-cert server-ca.pem

# Issued token (JWT / SAML / opaque — format depends on the server policy)
opcuactl get value -e opc.tcp://host:4840 -n i=2258 \
  --auth issued --token-file ./token.jwt \
  --security-policy auto --security-mode auto \
  --cert client.pem --key client.key
```

Store secrets in the environment when scripting (`OPCUACTL_PASSWORD`, `OPCUACTL_TOKEN`, …). Env vars apply when the matching flag is not set on the CLI. See [RECIPES.md](RECIPES.md) for copy-paste `CONN=(...)` blocks.

## NodeID Formats

`opcuactl` accepts standard OPC UA NodeID string formats:

| Format | Example |
|--------|---------|
| Numeric | `i=2258` |
| Numeric with namespace | `ns=2;i=1001` |
| String | `ns=2;s=Temperature` |
| GUID | `ns=3;g=12345678-1234-1234-1234-123456789abc` |
| Opaque (base64) | `ns=4;b=dGVzdA==` |

> **Shell quoting:** Node IDs that contain semicolons (`;`) **must** be quoted, otherwise the shell interprets `;` as a command separator.
>
> ```sh
> # Wrong — the shell splits this into two commands
> opcuactl get value -e opc.tcp://host:4840 -n ns=2;i=1001
>
> # Correct
> opcuactl get value -e opc.tcp://host:4840 -n "ns=2;i=1001"
> opcuactl get value -e opc.tcp://host:4840 -n 'ns=2;i=1001'
> ```
>
> This applies to all `--node` / `-n` arguments as well as `--object`, `--method`, and `--start-node` flags.
> Numeric IDs without a namespace prefix (e.g. `i=2258`) do not need quoting.

## Path Resolution

`browse path` resolves dot-separated browse paths to NodeIDs. It supports four resolution modes, chosen automatically based on the flags and path syntax you provide.

### Default (namespace 0, from Objects)

Segments are resolved in namespace 0, starting from the Objects folder (`i=85`).

```sh
opcuactl browse path -e opc.tcp://host:4840 --path Server.ServerStatus
# Resolves: Objects → Server → ServerStatus → i=2256
```

### Single namespace (`--namespace`)

All segments are resolved in the specified namespace.

```sh
opcuactl browse path -e opc.tcp://host:4840 --path MyFolder.Sensor.Temperature --namespace 2
# Resolves: Objects → ns2:MyFolder → ns2:Sensor → ns2:Temperature
```

### Qualified path (per-segment namespace)

Prefix each segment with `ns:` to mix namespaces in one path. The tool detects this format automatically.

```sh
opcuactl browse path -e opc.tcp://host:4840 --path "0:Server.0:ServerStatus.0:CurrentTime"
# Each segment specifies its own namespace index
```

### Custom start node (`--start-node`)

Begin resolution from an arbitrary node instead of Objects.

```sh
opcuactl browse path -e opc.tcp://host:4840 --path Sensor.Temperature \
  --start-node "ns=2;i=100" --namespace 2
# Resolves from ns=2;i=100 instead of Objects
```

### Summary

| Path syntax | Flags | Starts from |
|-------------|-------|-------------|
| `Server.ServerStatus` | *(none)* | Objects (`i=85`), namespace 0 |
| `Folder.Sensor` | `--namespace 2` | Objects, all segments in ns 2 |
| `0:Server.2:Custom` | *(auto-detected)* | Objects, per-segment namespace |
| `Seg1.Seg2` | `--start-node "ns=2;i=100"` | custom node |

## Monitoring Intervals

OPC UA subscriptions separate three concerns — how often the server samples the underlying data source, how many changes it buffers, and how often it delivers notifications to the client.

| Concept | Flag | Default | Description |
|---------|------|---------|-------------|
| **Publishing interval** | `--interval` | `100ms` | How often the server sends a batch of queued notifications to the client. |
| **Sampling interval** | `--sampling` | server-decided | How often the server reads the underlying data source. Must be ≤ the publishing interval for meaningful results. |
| **Queue size** | `--queue-size` | `10` | Maximum number of data-change notifications the server queues between publish cycles. |
| **Discard oldest** | `--discard-oldest` | `true` | When the queue is full, discard oldest samples (`true`) or keep the oldest window and drop newest intermediates (`false`). |

**Example:** With `--interval 1s --sampling 100ms --queue-size 10`, the server samples every 100 ms, queues up to 10 changes, and delivers them every 1 s. If the queue overflows, table output shows `[OVERFLOW]` and JSON includes `"overflow": true`.

```sh
# Fast sampling with batched delivery
opcuactl monitor value -e opc.tcp://host:4840 -n i=2258 \
  --interval 1s --sampling 100ms --queue-size 10 --discard-oldest

# Match sampling to publishing (one notification per cycle)
opcuactl monitor value -e opc.tcp://host:4840 -n i=2258 \
  --interval 1s --sampling 1s

# Default publishing interval (100ms), server-decided sampling
opcuactl monitor value -e opc.tcp://host:4840 -n i=2258
```

> **Tip:** If the server revises your publishing interval (shown on stderr with `-v`), it cannot sustain the requested rate. Increase `--interval` until the revised value matches.
## Development

### Prerequisites

- Go 1.25+
- [staticcheck](https://staticcheck.dev/) (for linting)
- [golangci-lint](https://golangci-lint.run/) (for CI linting)

### Build & Test

```sh
# Run all checks (format, lint, vet, test)
make check

# Run tests with race detector
make test

# Build for current platform (output: ./bin/opcuactl)
make build

# Build for all platforms
make build-all

# Install to /usr/local/bin
make install
```

### Project Structure

```
cmd/opcuactl/          Command implementations (one file per command group)
internal/conn/         Client connection management and auth/security options
internal/format/       Output formatting (table, JSON, YAML)
internal/model/        Output data models
internal/parse/        NodeID parsing, type coercion, security policy handling
```

## Supported Value Types

`write value` supports these `--type` values:

| Type | Example |
|------|---------|
| `bool` / `boolean` | `true`, `false` |
| `int16`, `uint16` | `42`, `65535` |
| `int32`, `uint32` | `100` |
| `int64`, `uint64` | `100` |
| `float` | `3.14` |
| `double` | `3.14159265` |
| `string` | `hello` |
| `bytes` | `aGVsbG8=` (base64) |
| `datetime` | `2026-01-01T00:00:00Z` |
| `guid` | `12345678-1234-1234-1234-123456789012` |
| `nodeid` | `i=2258`, `ns=2;s=Temp` |
| `json` | `{"key":"val"}` |

## Built With

- [otfabric/go-opcua](https://github.com/otfabric/go-opcua) — Pure Go OPC UA client library
- [spf13/cobra](https://github.com/spf13/cobra) — CLI framework

## Documentation

- [RECIPES.md](RECIPES.md) — End-to-end workflows for common operator tasks
- [EXAMPLES.md](EXAMPLES.md) — Real command outputs captured against a WAGO PLC
- [RELEASE.md](RELEASE.md) — Release notes (latest: v0.2.0)
- [CONTRIBUTING.md](CONTRIBUTING.md) — How to contribute
- [SECURITY.md](SECURITY.md) — Security policy and vulnerability reporting

## License

This project is licensed under the MIT License. See [LICENSE](./LICENSE).
