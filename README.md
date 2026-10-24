# opcuactl — OPC UA Command-Line Client: Discover, Browse, Read, Write, Call Methods, Monitor, History, Export & PubSub

[![Go](https://img.shields.io/badge/Go-1.26%2B-00ADD8?style=flat&logo=go)](https://go.dev/)
[![Go Reference](https://pkg.go.dev/badge/github.com/otfabric/opcuactl.svg)](https://pkg.go.dev/github.com/otfabric/opcuactl)
[![License](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![CI](https://github.com/otfabric/opcuactl/actions/workflows/ci.yml/badge.svg)](https://github.com/otfabric/opcuactl/actions/workflows/ci.yml)
[![Codecov](https://codecov.io/gh/otfabric/opcuactl/graph/badge.svg)](https://codecov.io/gh/otfabric/opcuactl)
[![Release](https://img.shields.io/github/v/release/otfabric/opcuactl?label=release)](https://github.com/otfabric/opcuactl/releases)

**opcuactl is a command-line client for OPC UA, the interoperability standard of industrial automation.** It discovers servers and their endpoints, browses and searches the address space, reads and writes values, calls methods, subscribes to data changes, events and alarms, reads historical data, exports whole subtrees, and listens to, publishes and configures OPC UA PubSub — from a terminal, over anything from an unsecured lab connection to a signed and encrypted channel with user authentication.

It is built for commissioning, troubleshooting and integration work on PLCs, gateways and SCADA servers, and for scripts and CI: every command can print a table, JSON, YAML, CSV or JSON Lines, and reports failures through stable exit codes. It is written in Go on top of [go-opcua](https://github.com/otfabric/go-opcua), whose client is interop-tested against open62541, Eclipse Milo, async-opcua, the OPC Foundation UA-.NETStandard stack, node-opcua and opcua-asyncio.

**Quick links:** [Quick start](#quick-start) · [Features](#features) · [Installation](#installation) · [Commands](#command-reference) · [Examples](#usage-examples) · [Security](#security) · [FAQ](#faq)

> **Responsible use:** opcuactl talks to real control systems. Only use it on servers and networks you own or are authorized to test. `write value` changes process values and `call method` runs server-side logic: know what a node controls before you write to it (`--dry-run` validates a write without sending it). Subscriptions and large browse or export runs add load to a server that may have limited sessions and resources.

---

## Why opcuactl?

- **Find what is there** — `discover servers`, `discover lds` and `discover endpoints` list the servers on a host, the ones registered with a Local Discovery Server or announced with mDNS, and every endpoint with its security policy, mode and user-token types; `--best` picks the most secure one.
- **Explore the address space** — `browse children`, `browse tree`, `browse refs` and `browse path` walk the node hierarchy; `find node`, `find type` and `find namespace` search it.
- **Read anything** — values (with timestamps and array index ranges), full node summaries, any attribute, server status, or every variable under an object in one batch.
- **Write and call, carefully** — typed writes with a confirmation prompt and `--dry-run`; method calls with argument introspection.
- **Watch a server live** — `monitor value`, `monitor event` and `monitor alarm` stay subscribed and print every change as it arrives, with queue size, sampling interval, deadband and trigger filters and event filters under your control.
- **History** — raw, modified, processed (aggregate) and at-time reads, plus historical events.
- **Export** — node metadata, value snapshots or historical time series for a whole subtree.
- **Security that connects** — anonymous, username, X.509 and issued-token identities over the RSA and the ECC security policies (NIST P-256/P-384, Curve25519, AES-GCM, ChaCha20-Poly1305); `--security-policy auto --security-mode auto` discovers the endpoints and configures the best match for your certificate.
- **PubSub** — `pubsub subscribe` prints the DataSets publishers send as UADP or JSON over UDP (unicast and multicast), Ethernet, MQTT 3.1.1 / 5.0 or AMQP 1.0, `pubsub publish` sends test DataSets (chunked if they are large, answering discovery probes), `pubsub discover` probes for publishers, their metadata and configuration, `pubsub call` calls a PubSub Action of a Responder and `pubsub respond` answers Action requests for testing, `pubsub decode` decodes a capture, `pubsub sks get-keys` fetches the keys of a SecurityGroup, and `pubsub config` shows and changes the PubSub configuration of a server and reads its diagnostics counters.
- **Reach servers behind a firewall** — with `--reverse-listen` opcuactl waits for the server to connect (OPC UA reverse connect); every command works over it.
- **Standard encodings** — read and write values in the OPC UA JSON and XML DataEncodings to exchange them with other stacks.
- **Script-friendly** — `--format table|json|yaml|csv|jsonl`, `--quiet` for a bare value, stable exit codes and `OPCUACTL_*` environment variables.
- **Pre-built binaries** — Linux, macOS and Windows (x86-64 and ARM) from [GitHub Releases](https://github.com/otfabric/opcuactl/releases), with SHA-256 checksums; no runtime to install.

---

## Quick start

Download a binary from [GitHub Releases](https://github.com/otfabric/opcuactl/releases) (see [Installation](#installation) for copy-paste commands for Linux, macOS and Windows, or build from source), then point it at a server — replace `opc.tcp://localhost:4840` with your endpoint:

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

# Listen to OPC UA PubSub (UADP) on a UDP multicast group
opcuactl pubsub subscribe --url opc.udp://224.0.0.22:4840
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
---

## Use cases

- **Commissioning and acceptance tests** — check that every tag of a new PLC or gateway is reachable, has the expected type and access level, and carries a plausible value.
- **Troubleshooting** — is the server up, which endpoints and security policies does it offer, does this user get in, does this node update? `ping`, `discover endpoints` and `monitor value` answer in seconds.
- **Integration work** — find NodeIds by browse path or name, read a method's signature before wiring it into an application, capture values as JSON for a test fixture.
- **Data collection** — log live values as JSON Lines, pull historical data as CSV, snapshot a subtree before and after a change.
- **Automation and CI** — smoke-test an OPC UA server in a pipeline using exit codes and JSON output.
- **PubSub commissioning** — see what is on the wire (publisher ids, writer ids, sequence numbers, security), check that a secured group decrypts with the SKS keys, feed a subscriber with test data, or reconfigure a publisher without its engineering tool.

---

## Table of Contents

- [Why opcuactl?](#why-opcuactl)
- [Quick start](#quick-start)
- [Use cases](#use-cases)
- [Features](#features)
- [Installation](#installation)
  - [Linux](#linux)
  - [macOS](#macos)
  - [Windows (PowerShell)](#windows-powershell)
  - [Manual download](#manual-download)
  - [Update](#update)
  - [Go install](#go-install)
  - [Build from source](#build-from-source)
  - [Shell completion](#shell-completion)
- [Command Reference](#command-reference)
- [Usage Examples](#usage-examples)
- [Global Flags](#global-flags)
- [Output Formats](#output-formats)
- [Exit Codes](#exit-codes)
- [Security](#security)
  - [Security policies](#security-policies)
  - [Reverse connect](#reverse-connect)
- [PubSub](#pubsub)
  - [PubSub Actions](#pubsub-actions)
- [NodeID Formats](#nodeid-formats)
- [Path Resolution](#path-resolution)
- [Monitoring Intervals](#monitoring-intervals)
  - [Data-change filters](#data-change-filters)
- [FAQ](#faq)
- [Development](#development)
- [Supported Value Types](#supported-value-types)
- [Built With](#built-with)
- [Documentation](#documentation)
- [License](#license)

## Features

- **Discovery** — find servers on a host, query a Local Discovery Server (registered servers and mDNS / FindServersOnNetwork with capability filter), list endpoints, select the most secure configuration
- **Browse** — explore address-space trees, child nodes, references, and resolve browse paths
- **Search** — find nodes by name, display name, type definition, data type, or namespace
- **Read** — read values (including array IndexRange), node summaries, attributes, and server status; OPC UA JSON / XML output with `--ua-encoding`
- **Write** — write typed values with dry-run validation; any data type as OPC UA JSON / XML
- **Call** — invoke OPC UA methods with argument introspection
- **Monitor** — subscribe to live value changes, filtered events, and alarms (queue window + overflow, DataChangeFilter with absolute / percent deadband and trigger, IndexRange)
- **History** — raw, modified, processed (aggregates), and at-time historical reads
- **Export** — bulk export node metadata and value snapshots or historical time-series
- **AuthN/Z** — anonymous, username, X.509, and issued-token identities; RSA and ECC security policies; `auto` endpoint security selection
- **Reverse connect** — let the server open the connection (`--reverse-listen`)
- **PubSub** — subscribe to and publish UADP / JSON NetworkMessages over UDP (unicast, multicast), Ethernet (`opc.eth://`, UADP), MQTT 3.1.1 / 5.0 (QoS 0–2, user properties) and AMQP 1.0 (RabbitMQ, Artemis), chunked NetworkMessages, UADP discovery (probes and announcements), PubSub Actions (request / response: `pubsub call` and a test Responder, `pubsub respond`), decode captures, message security (PubSub-Aes128-CTR / Aes256-CTR) with keys from a Security Key Service, Part 14 diagnostics counters, and remote configuration and diagnostics of a server's PublishSubscribe object

All commands support **table**, **JSON**, **YAML**, **CSV**, and **JSONL** output formats.

See [RELEASE.md](RELEASE.md) for what changed in the latest release.

---

## Installation

The easiest way to install opcuactl is to download a pre-built binary from the [GitHub Releases page](https://github.com/otfabric/opcuactl/releases). No Go toolchain is required.

| Platform | CPU | Download |
|---|---|---|
| Linux | x86-64 | `opcuactl-<version>-linux-amd64.tar.gz` |
| Linux | 64-bit ARM (Raspberry Pi 4/5 with a 64-bit OS, AWS Graviton) | `opcuactl-<version>-linux-arm64.tar.gz` |
| Linux | 32-bit ARM (Raspberry Pi with a 32-bit OS) | `opcuactl-<version>-linux-armv7.tar.gz` |
| macOS | Apple Silicon (M1 and later) | `opcuactl-<version>-darwin-arm64.tar.gz` |
| macOS | Intel | `opcuactl-<version>-darwin-amd64.tar.gz` |
| Windows | x86-64 | `opcuactl-<version>-windows-amd64.zip` |
| Windows | ARM64 | `opcuactl-<version>-windows-arm64.zip` |

Each release also contains `opcuactl-<version>-checksums.txt` (SHA-256) so you can verify your download. The commands below fetch the **latest** release, verify its checksum and install it. Afterwards, check that it works:

```console
opcuactl version
```

### Linux

```bash
# 1. Find the latest version and your CPU architecture
VERSION=$(curl -fsSLI -o /dev/null -w '%{url_effective}' https://github.com/otfabric/opcuactl/releases/latest | sed 's|.*/v||')
case "$(uname -m)" in
  x86_64)         ARCH=amd64 ;;
  aarch64|arm64)  ARCH=arm64 ;;
  armv7l)         ARCH=armv7 ;;
  *) echo "Unsupported architecture: $(uname -m)" ;;
esac

# 2. Download the archive and the checksum file
FILE="opcuactl-${VERSION}-linux-${ARCH}.tar.gz"
BASE="https://github.com/otfabric/opcuactl/releases/download/v${VERSION}"
curl -fsSLO "${BASE}/${FILE}"
curl -fsSLO "${BASE}/opcuactl-${VERSION}-checksums.txt"

# 3. Verify the download (should print "OK")
grep "${FILE}" "opcuactl-${VERSION}-checksums.txt" | sha256sum -c -

# 4. Extract and install (the binary in the archive carries the version and platform in its name)
mkdir -p opcuactl-dl && tar -xzf "${FILE}" -C opcuactl-dl
sudo install -m 0755 "$(find opcuactl-dl -type f -name 'opcuactl*' | head -n1)" /usr/local/bin/opcuactl
```

No `sudo`? Install into your home directory instead (and make sure `~/.local/bin` is in your `PATH`):

```bash
mkdir -p ~/.local/bin
install -m 0755 "$(find opcuactl-dl -type f -name 'opcuactl*' | head -n1)" ~/.local/bin/opcuactl
```

### macOS

```bash
# 1. Find the latest version and your CPU architecture (arm64 = Apple Silicon, x86_64 = Intel)
VERSION=$(curl -fsSLI -o /dev/null -w '%{url_effective}' https://github.com/otfabric/opcuactl/releases/latest | sed 's|.*/v||')
case "$(uname -m)" in
  arm64)   ARCH=arm64 ;;
  x86_64)  ARCH=amd64 ;;
esac

# 2. Download the archive and the checksum file
FILE="opcuactl-${VERSION}-darwin-${ARCH}.tar.gz"
BASE="https://github.com/otfabric/opcuactl/releases/download/v${VERSION}"
curl -fsSLO "${BASE}/${FILE}"
curl -fsSLO "${BASE}/opcuactl-${VERSION}-checksums.txt"

# 3. Verify the download (should print "OK")
grep "${FILE}" "opcuactl-${VERSION}-checksums.txt" | shasum -a 256 -c -

# 4. Extract and install (the binary in the archive carries the version and platform in its name)
mkdir -p opcuactl-dl && tar -xzf "${FILE}" -C opcuactl-dl
sudo install -m 0755 "$(find opcuactl-dl -type f -name 'opcuactl*' | head -n1)" /usr/local/bin/opcuactl
```

If you downloaded the archive with a web browser instead of `curl` and macOS refuses to run the binary ("cannot be verified"), remove the quarantine flag: `xattr -d com.apple.quarantine /usr/local/bin/opcuactl`.

### Windows (PowerShell)

```powershell
# 1. Find the latest version and your CPU architecture
$version = (Invoke-RestMethod https://api.github.com/repos/otfabric/opcuactl/releases/latest).tag_name.TrimStart('v')
$arch    = if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64') { 'arm64' } else { 'amd64' }

# 2. Download the archive and the checksum file
$file = "opcuactl-$version-windows-$arch.zip"
$base = "https://github.com/otfabric/opcuactl/releases/download/v$version"
Invoke-WebRequest "$base/$file" -OutFile "$env:TEMP\$file"
Invoke-WebRequest "$base/opcuactl-$version-checksums.txt" -OutFile "$env:TEMP\opcuactl-checksums.txt"

# 3. Verify the download (throws an error on mismatch)
$expected = ((Select-String -Path "$env:TEMP\opcuactl-checksums.txt" -Pattern $file).Line -split '\s+')[0]
$actual   = (Get-FileHash "$env:TEMP\$file" -Algorithm SHA256).Hash
if ($expected -ne $actual) { throw "Checksum mismatch for $file" }

# 4. Extract to a per-user folder and name the program opcuactl.exe
#    (the .exe in the archive carries the version and platform in its name)
$dir = "$env:LOCALAPPDATA\Programs\opcuactl"
New-Item -ItemType Directory -Force -Path $dir | Out-Null
Expand-Archive "$env:TEMP\$file" -DestinationPath "$env:TEMP\opcuactl-dl" -Force
$exe = Get-ChildItem "$env:TEMP\opcuactl-dl" -Recurse -Filter 'opcuactl*.exe' | Select-Object -First 1
Copy-Item $exe.FullName "$dir\opcuactl.exe" -Force

# 5. Add opcuactl to your user PATH (then open a new terminal)
$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if ($userPath -notlike "*$dir*") {
  [Environment]::SetEnvironmentVariable('Path', "$userPath;$dir", 'User')
}
```

If Windows SmartScreen warns about an unrecognised app, choose **More info → Run anyway**.

### Manual download

1. Open the [latest release](https://github.com/otfabric/opcuactl/releases/latest).
2. Download the archive for your platform from the table above.
3. Extract it, rename the binary to `opcuactl` (`opcuactl.exe` on Windows) and move it to a folder on your `PATH`.

### Update

Run the same commands again. They always fetch the latest release and overwrite the previous binary.

### Go install

With Go 1.26 or later you can install straight from source. The binary lands in `$(go env GOPATH)/bin`; `opcuactl version` reports `dev` for builds made this way.

```console
go install github.com/otfabric/opcuactl@latest
```

### Build from source

Requires Go 1.26 or later:

```console
git clone https://github.com/otfabric/opcuactl.git
cd opcuactl
make build
```

`make build` runs the checks and writes the binary to **`bin/opcuactl`**. `make build-all` cross-compiles the Linux and macOS binaries into `release/`; the Windows binaries are produced by the release workflow.

### Shell completion

`opcuactl completion bash|zsh|fish|powershell` prints a completion script; `opcuactl completion <shell> --help` shows how to load it.

---

## Command Reference

```
opcuactl
├── discover
│   ├── servers        Find OPC UA servers via FindServers / FindServersOnNetwork
│   ├── lds            Query a Local Discovery Server (registered servers + mDNS)
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
├── pubsub
│   ├── subscribe      Listen on UDP / Ethernet / MQTT / AMQP and print received DataSetMessages
│   ├── publish        Publish a DataSet built from the command line (testing)
│   ├── call           Call a PubSub Action of a Responder and print its result
│   ├── respond        Answer PubSub Action requests (a test Responder)
│   ├── discover       Send UADP discovery probes and print the announcements
│   ├── decode         Decode captured UADP or JSON NetworkMessages (chunks too)
│   ├── sks
│   │   └── get-keys   Get the keys of a SecurityGroup (GetSecurityKeys)
│   └── config
│       ├── show       Show the PubSub components of a server
│       ├── add-connection / add-dataset / add-writer-group / add-writer
│       ├── add-reader-group / add-reader
│       ├── remove     Remove a connection, group, writer, reader or data set
│       ├── enable / disable
│       └── diagnostics Read (and reset) the diagnostics counters of a component
├── ping               Test connectivity to an OPC UA server
├── version            Print version, build, and runtime info
└── completion         Generate shell completion scripts (bash|zsh|fish|powershell)
```

## Usage Examples

### Discovery

```sh
# Find servers, including network discovery
opcuactl discover servers -e opc.tcp://localhost:4840 --on-network

# Servers on the network that offer data access and history (capability filter)
opcuactl discover servers -e opc.tcp://localhost:4840 --on-network --capability DA --capability HD

# Everything the Local Discovery Server of a host knows (port 4840):
# registered servers and, with the multicast extension, servers found with mDNS
opcuactl discover lds --host 192.168.1.10

# Find the most secure endpoint
opcuactl discover endpoints -e opc.tcp://localhost:4840 --best

# Filter endpoints by security policy
opcuactl discover endpoints -e opc.tcp://localhost:4840 --policy Basic256Sha256

# The best endpoint your certificate can use (an ECC key gets an ECC endpoint)
opcuactl discover endpoints -e opc.tcp://localhost:4840 --best --cert client-ecc.pem --key client-ecc.key
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

# Any data type as a Variant in the OPC UA JSON encoding: a whole array ...
opcuactl write value -e opc.tcp://localhost:4840 -n "ns=2;s=ArrayNode" \
  --value '{"UaType":6,"Value":[1,2,3]}' --type ua-json

# ... or a LocalizedText
opcuactl write value -e opc.tcp://localhost:4840 -n "ns=2;s=Label" \
  --value '{"UaType":21,"Value":{"Locale":"en","Text":"Tank 1"}}' --type ua-json
```

### OPC UA JSON and XML Encodings

`--format json` prints opcuactl's own, compact output. `--ua-encoding` prints the DataValue exactly as Part 6 of the OPC UA specification defines it, so it can be exchanged with other OPC UA stacks and tools:

```sh
# OPC UA JSON, 1.05 compact form (what open62541 reads and writes)
opcuactl get value -e opc.tcp://localhost:4840 -n "ns=2;s=ArrayNode" --ua-encoding json
# {"UaType":6,"Value":[1,2,3],"SourceTimestamp":"2026-10-12T09:15:02.118Z",...}

# Other forms: json-verbose, json-reversible and json-nonreversible (OPC UA 1.04), xml
opcuactl get value -e opc.tcp://localhost:4840 -n "ns=2;s=ArrayNode" --ua-encoding xml --pretty
```

One document is printed per `--node`. The `UaType` / `Value` pair of the JSON output is what `write value --type ua-json` accepts. This is a data encoding only: the connection to the server remains UA TCP with the binary encoding.

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

# Absolute deadband: only report when the value moved more than 0.5
opcuactl monitor value -e opc.tcp://localhost:4840 -n "ns=2;s=Temperature" \
  --deadband-type absolute --deadband 0.5

# Percent deadband: 2 % of the variable's EURange (AnalogItemType variables)
opcuactl monitor value -e opc.tcp://localhost:4840 -n "ns=2;s=Level" \
  --deadband-type percent --deadband 2

# Also report when only the source timestamp changed (value unchanged)
opcuactl monitor value -e opc.tcp://localhost:4840 -n "ns=2;s=Temperature" \
  --trigger status-value-timestamp

# Monitor a slice of an array; changes outside it do not notify
opcuactl monitor value -e opc.tcp://localhost:4840 -n "ns=2;s=ArrayNode" --index-range "0:3"

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

### PubSub

```sh
# Everything that arrives on a UDP multicast group (UADP), one line per DataSetMessage
opcuactl pubsub subscribe --url opc.udp://224.0.0.22:4840 --interface eth0

# Name the fields: UADP carries no names (and with RawData no types)
opcuactl pubsub subscribe --url opc.udp://224.0.0.22:4840 --field Temperature:Double --field Time:DateTime

# ... or take the DataSetMetaData from the publishing server's PublishedDataSet
opcuactl pubsub subscribe --url opc.udp://224.0.0.22:4840 -e opc.tcp://plc:4840 --metadata-node "ns=2;i=175"

# One publisher, one writer, as JSON Lines
opcuactl pubsub subscribe --url opc.udp://224.0.0.22:4840 --publisher-id 2234 --writer-id 1 --jsonl

# JSON (or UADP) through an MQTT broker; announced ua-metadata is used automatically
opcuactl pubsub subscribe --url mqtt://broker:1883 --topic 'plant/#' --show-metadata

# Secured UADP: keys of SecurityGroup sg1 from the Security Key Service (SignAndEncrypt session)
opcuactl pubsub subscribe --url opc.udp://224.0.0.22:4840 --security-group sg1 --message-security sign-and-encrypt \
  -e opc.tcp://sks:4840 --security-policy Basic256Sha256 --security-mode SignAndEncrypt \
  --cert client.pem --key client.key --auth username --username subscriber --password "$OPCUACTL_PASSWORD"

# Publish a test DataSet: 10 messages, one every 500 ms
opcuactl pubsub publish --url opc.udp://224.0.0.22:4840 --publisher-id 2234 --writer-group-id 100 --writer-id 1 \
  --field Counter:UInt32=42 --field Temp:Double=21.5 --count 10 --interval 500ms

# JSON over MQTT, with a retained ua-metadata message first
opcuactl pubsub publish --url mqtt://broker:1883 --topic plant/line1 --encoding json \
  --field Temp:Double=21.5 --metadata-topic plant/line1/meta

# MQTT 5.0 with QoS 1 and broker credentials: content type, UAMessageType and user properties are shown
OPCUACTL_BROKER_PASSWORD=secret opcuactl pubsub subscribe --url mqtts://broker:8883 --mqtt-version 5.0 --qos 1 \
  --broker-username plant --topic 'plant/#'

# AMQP 1.0: a RabbitMQ queue (vhost via connection-hostname) and an Artemis anycast queue
opcuactl pubsub subscribe --url amqp://rabbit:5672 --topic /queues/plant --transport-property connection-hostname=vhost:plant
opcuactl pubsub publish --url amqp://artemis:5672 --topic plant.line1 --encoding json --field Temp:Double=21.5 \
  --transport-property target-capabilities=queue

# Raw Ethernet (UADP; needs root or CAP_NET_RAW)
sudo opcuactl pubsub subscribe --url opc.eth://01-00-5E-00-00-01 --interface eth0

# NetworkMessages above 512 bytes are sent as chunks; subscribe reassembles them
opcuactl pubsub publish --url opc.udp://224.0.0.22:4840 --field "Readings:String=$(seq -s, 1 500)" --max-message-size 512

# UADP discovery: who publishes here, and the DataSetMetaData of writer 1 of publisher 2234
opcuactl pubsub discover --url opc.udp://224.0.0.22:4840
opcuactl pubsub discover --url opc.udp://224.0.0.22:4840 --probe metadata,writers --publisher-id 2234 --writer-id 1
# A publisher that answers probes and announces its metadata every 10 s; a subscriber that probes for it
opcuactl pubsub publish --url opc.udp://224.0.0.22:4840 --field Temp:Double=21.5 --count 0 --answer-probes --announce-interval 10s
opcuactl pubsub subscribe --url opc.udp://224.0.0.22:4840 --probe

# PubSub Actions: a test Responder that echoes two inputs, and a call through the same MQTT broker
opcuactl pubsub respond --url mqtt://broker:1883 --input A:Int32 --input B:Int32
opcuactl pubsub call --url mqtt://broker:1883 --input A:Int32=2 --input B:Int32=3 --output A:Int32 --output B:Int32
# ... over UDP: the Requestor listens on --url, the request goes to the Responder's address
opcuactl pubsub call --url opc.udp://10.0.0.5:4841 --request-address opc.udp://10.0.0.9:4840 --input Speed:Double=1450 \
  --resend-interval 200ms --format json

# Diagnostics counters (Part 14 §9.1.11) every 10 s and on exit
opcuactl pubsub subscribe --url opc.udp://224.0.0.22:4840 --diagnostics-interval 10s --diagnostics-level advanced

# Decode a captured message (raw bytes, hex or base64; UADP or JSON); chunks as separate files
opcuactl pubsub decode capture.hex --field Counter:UInt32 --field Temp:Double
opcuactl pubsub decode chunk1.bin chunk2.bin chunk3.bin

# Keys of a SecurityGroup; --show-keys --format json writes a --keys-file
opcuactl pubsub sks get-keys -e opc.tcp://sks:4840 --security-group sg1 --future-keys 2 \
  --security-policy Basic256Sha256 --security-mode SignAndEncrypt --cert client.pem --key client.key \
  --auth username --username subscriber --password "$OPCUACTL_PASSWORD" --show-keys --format json > keys.json

# The PubSub configuration of a server, and changing it
opcuactl pubsub config show -e opc.tcp://localhost:4840
opcuactl pubsub config add-connection -e opc.tcp://localhost:4840 --name Diag --url opc.udp://224.0.0.22:4840 --publisher-id 2234
opcuactl pubsub config add-dataset -e opc.tcp://localhost:4840 --name Diag --var Temperature="ns=1;s=Temperature"
opcuactl pubsub config add-writer-group -e opc.tcp://localhost:4840 --connection "ns=2;i=166" --name DiagGroup --writer-group-id 100 --interval 500ms
opcuactl pubsub config add-writer -e opc.tcp://localhost:4840 --group "ns=2;i=179" --name DiagWriter --writer-id 1 --dataset Diag
opcuactl pubsub config disable -e opc.tcp://localhost:4840 --node "ns=2;i=179"
opcuactl pubsub config remove connection -e opc.tcp://localhost:4840 --node "ns=2;i=166"
opcuactl pubsub config add-connection -e opc.tcp://localhost:4840 --name Broker --url mqtt://broker:1883 --mqtt-version 5.0 --encoding json
opcuactl pubsub config diagnostics -e opc.tcp://localhost:4840 --node "ns=2;i=179"
```

See [PubSub](#pubsub) for the flags and how security works.

## Global Flags

Every networked command supports these connection and output flags:

| Flag | Env Variable | Description |
|------|-------------|-------------|
| `-e, --endpoint` | `OPCUACTL_ENDPOINT` | OPC UA server endpoint (e.g. `opc.tcp://host:4840`) |
| `--security-policy` | `OPCUACTL_SECURITY_POLICY` | Security policy (`None`, `Basic256Sha256`, `ECC_nistP256`, …, or `auto`; see [Security](#security)) |
| `--security-mode` | `OPCUACTL_SECURITY_MODE` | Security mode (`None`, `Sign`, `SignAndEncrypt`, or `auto`) |
| `--auth` | `OPCUACTL_AUTH` | Auth mode (`anonymous`, `username`, `certificate`, `issued`) |
| `--username` | `OPCUACTL_USERNAME` | Username for username auth |
| `--password` | `OPCUACTL_PASSWORD` | Password for username auth |
| `--auth-policy-id` | `OPCUACTL_AUTH_POLICY_ID` | User token PolicyId (usually set via security `auto`) |
| `--token` | `OPCUACTL_TOKEN` | Issued token (raw or base64) for `--auth issued` |
| `--token-file` | `OPCUACTL_TOKEN_FILE` | Issued token file for `--auth issued` |
| `--cert` | `OPCUACTL_CERT` | Client certificate file (RSA, ECDSA or Ed25519; channel and/or certificate auth) |
| `--key` | `OPCUACTL_KEY` | Client private key file |
| `--server-cert` | `OPCUACTL_SERVER_CERT` | Server certificate file (overrides endpoint cert; skips the endpoint lookup for an explicit policy) |
| `--trusted-cert` | `OPCUACTL_TRUSTED_CERT` | Trusted CA / self-signed cert (repeatable; env is CSV) |
| `--insecure-skip-verify` | — | Skip server certificate verification (INSECURE) |
| `--reverse-listen` | `OPCUACTL_REVERSE_LISTEN` | Reverse connect: listen on `host:port` and wait for the server to connect |
| `--reverse-server-uri` | `OPCUACTL_REVERSE_SERVER_URI` | Reverse connect: only accept the server announcing this ApplicationUri |
| `--timeout` | `OPCUACTL_TIMEOUT` | Dial / request / connect timeout, and how long to wait for a reverse connection (default: `10s`) |
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
| Who opens the connection | the client by default; the server with `--reverse-listen` ([reverse connect](#reverse-connect)) |
| Client application cert | `--cert` + `--key` (required for Sign / SignAndEncrypt) |
| Trust the server | `--trusted-cert` (repeatable), `--server-cert`, or `--insecure-skip-verify` (dev only) |
| User token PolicyId | usually filled by `auto`; override with `--auth-policy-id` if needed |

### Security policies

| Policy | Certificate key | Notes |
|--------|-----------------|-------|
| `Basic256Sha256`, `Aes128Sha256RsaOaep`, `Aes256Sha256RsaPss` | RSA | The widely deployed RSA policies |
| `ECC_nistP256`, `ECC_nistP384` | ECDSA P-256 / P-384 | ECC with ephemeral key agreement (OPC UA 1.05) |
| `ECC_nistP256_AesGcm`, `ECC_nistP256_ChaChaPoly` | ECDSA P-256 | Authenticated encryption (OPC UA 1.05.07) |
| `ECC_curve25519` | Ed25519 | Curve25519 key agreement, ChaCha20-Poly1305 |
| `Basic128Rsa15`, `Basic256` | RSA | Deprecated; only for legacy servers |

The key type of `--cert` decides which policies you can use: an RSA certificate cannot open an ECC channel and the other way round. `auto`, and `discover endpoints --best --cert …`, therefore only consider endpoints that fit your certificate — a server that offers both gets an ECC channel from a client with an ECC certificate and an RSA channel otherwise. With an explicit policy the server certificate is taken from the matching endpoint (or from `--server-cert`).

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

# ECC: an ECDSA P-256 certificate over an authenticated-encryption policy
opcuactl get value -e opc.tcp://host:4840 -n i=2258 \
  --security-policy ECC_nistP256_AesGcm --security-mode SignAndEncrypt \
  --cert client-ecc.pem --key client-ecc.key \
  --trusted-cert server-ca.pem

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

### Reverse connect

Some servers cannot be reached from where you sit — they are behind a firewall or NAT — but can open connections themselves. With OPC UA reverse connect the server dials the client. Configure the server with the address opcuactl listens on, then add `--reverse-listen` to any command:

```sh
# Wait (up to --timeout) for the server to connect to port 4841, then read
opcuactl get value --reverse-listen 0.0.0.0:4841 --timeout 60s -n i=2258

# Several servers may dial in: name the one you want by its ApplicationUri
opcuactl get value --reverse-listen 0.0.0.0:4841 --timeout 60s -n i=2258 \
  --reverse-server-uri urn:plant1:plc7

# Everything else works unchanged, including discovery and secure channels
opcuactl discover endpoints --reverse-listen 0.0.0.0:4841 --timeout 60s
opcuactl monitor value --reverse-listen 0.0.0.0:4841 --timeout 60s -n "ns=2;s=Temperature" \
  --security-policy auto --security-mode auto --cert client.pem --key client.key --trusted-cert server-ca.pem
```

`--endpoint` is optional with reverse connect: without it opcuactl uses the endpoint URL the server announces; with it the announcement has to match. The announcement itself is not authenticated — anyone who can reach the listener can send one — so use a security mode other than `None` and validate the server certificate when the network is not trusted. `ping` skips its TCP stage, as there is nothing to dial.

## PubSub

OPC UA PubSub (Part 14) sends DataSets from publishers to subscribers without sessions. The `pubsub` commands work with the UADP and JSON message mappings over these transports (`--url`):

| Transport | URL | Mappings | Notes |
|-----------|-----|----------|-------|
| UDP | `opc.udp://host:port` | UADP | unicast or multicast; `--interface` picks the interface for multicast |
| Ethernet | `opc.eth://MAC[:VID[.PCP]]` | UADP | `--interface` required; Linux, macOS, FreeBSD; needs root or `sudo setcap cap_net_raw+ep "$(command -v opcuactl)"` |
| MQTT | `mqtt://`, `mqtts://` | UADP, JSON, gzip compressed JSON | `--mqtt-version 3.1.1\|5.0\|BestAvailable`, `--qos 0..2`, `--topic`; `--retain` on publish; `--json-compression gzip` on publish, call, respond and `config add-connection` (MQTT 5.0; subscribers decompress on their own) |
| AMQP 1.0 | `amqp://`, `amqps://` | UADP, JSON | `--topic` is the AMQP address: RabbitMQ `/queues/<q>` (vhost with `--transport-property connection-hostname=vhost:<name>`), Artemis a plain address (anycast with `source-capabilities=queue` / `target-capabilities=queue`); `--qos 2` acts as 1 |

Broker credentials are `--broker-username`, `--broker-password` (or `OPCUACTL_BROKER_PASSWORD`; `OPCUACTL_MQTT_PASSWORD` still works) and `--broker-client-id` (MQTT client id, AMQP container-id). `--username` and `--password` are the OPC UA session's. The 0.4 flags `--mqtt-username`, `--mqtt-password` and `--mqtt-client-id` still work but are deprecated. `--transport-property key=value` (repeatable) passes connection properties to the transport: MQTT 5.0 `connection-*` properties, AMQP `source-capabilities`, `target-capabilities`, `connection-hostname`, and so on.

| Concern | Flags |
|---------|-------|
| Transport | `--url`, `--interface`, `--topic`, `--qos`, `--mqtt-version`, `--json-compression`, `--broker-*`, `--transport-property` |
| Chunks | publish `--max-message-size`; subscribe `--chunk-max-message-size`, `--chunk-max-pending`, `--chunk-max-pending-bytes`, `--chunk-timeout` |
| Discovery | `pubsub discover`; subscribe `--probe`; publish `--answer-probes`, `--announce-metadata`, `--announce-interval`; `--discovery-url`, `--discovery-layout spec\|dotnet` |
| Diagnostics | `--diagnostics`, `--diagnostics-interval`, `--diagnostics-level basic\|advanced\|info\|log\|debug`; `pubsub config diagnostics` |
| Message mapping | `--encoding auto\|uadp\|json` (subscribe and decode detect it) |
| Which DataSets | `--publisher-id`, `--writer-group-id` (UADP), `--writer-id` |
| Field names and types | `--field Name:Type` (repeatable, `Type[]` for arrays), `--metadata file`, `--metadata-node NodeId`, or announced on the wire |
| Message security | `--security-group`, `--message-security none\|sign\|sign-and-encrypt`, `--keys-file` |
| Actions | `pubsub call` and `pubsub respond`, see [PubSub Actions](#pubsub-actions) |
| Session (SKS, metadata, config) | the [global flags](#global-flags): `-e`, `--security-*`, `--auth`, `--cert`, … |

**PublisherIds** are written as a number (a UInt16, the usual choice), `byte:N`, `u32:N`, `u64:N` or a string (`str:name`, or any non-number). UADP subscribers match PublisherIds by value, publishers send the type you give.

**DataSetMetaData.** UADP messages carry no field names and, with the RawData field encoding, no types. `subscribe` and `decode` print unnamed fields as `#0`, `#1`, … unless they know the metadata: from `--field`, from a file (`--metadata`: a `ua-metadata` JSON message or a `DataSetMetaDataType` in the OPC UA JSON encoding), from a server (`--metadata-node`: the `DataSetMetaData` property of a PublishedDataSet or DataSetReader), or announced by the publisher (`ua-metadata` messages on a broker, UADP DataSetMetaData announcements). `publish` announces its own with `--announce-metadata` (UDP) or `--metadata-topic` (MQTT).

**Message security.** Secured UADP messages (Sign or SignAndEncrypt with PubSub-Aes128-CTR or PubSub-Aes256-CTR) need the keys of their SecurityGroup. `--security-group` pulls them with GetSecurityKeys from the Security Key Service at `--endpoint` and keeps them fresh; the SKS only hands out keys over a SignAndEncrypt session, usually not to anonymous users. `--keys-file` reads keys saved with `pubsub sks get-keys --show-keys --format json` instead — treat that file like a password. A subscriber's `--message-security` is the minimum it accepts; weaker messages are reported on stderr. JSON messages have no message security.

**Configuration.** `pubsub config` calls the methods of the server's PublishSubscribe object (AddConnection, AddPublishedDataItems, AddWriterGroup, AddDataSetWriter, AddReaderGroup, AddDataSetReader, the Remove methods, Enable / Disable); components are addressed by the NodeIds `config show` and the add commands print (`-q` prints only the NodeId). It works with go-opcua (`server.WithPubSub`) and open62541 servers.

**Broker messages.** `subscribe` shows the topic, content type, UAMessageType (`ua-data`, `ua-metadata`, …) and the user properties (MQTT 5.0, AMQP application properties) of each broker message, and picks the decoder by content type (`application/opcua+uadp`, `application/json`), falling back to detecting it. `publish` sends the UAMessageType of its data and metadata messages; the content type comes from the transport.

**Chunked NetworkMessages.** UADP NetworkMessages larger than `--max-message-size` (default: the transport's limit, such as the UDP datagram size) are split into chunks (Part 14 §7.2.4.4.4), each secured on its own. `subscribe` and `decode` reassemble them; `--chunk-max-message-size` (default 2 MiB), `--chunk-max-pending` (512), `--chunk-max-pending-bytes` (32 MiB) and `--chunk-timeout` (5 s) bound the memory the reassembly uses. JSON messages have no chunks.

**Discovery.** UADP discovery (Part 14 §7.2.4.6) works on UDP and Ethernet. `pubsub discover` sends probes (`--probe applications|endpoints|metadata|writers|group|connections`) and prints the announcements; `subscribe --probe` asks for the DataSetMetaData of writers it sees without one; `publish --answer-probes` answers probes for its application state, DataSetMetaData, writer and group configuration and connection, and `--announce-metadata` / `--announce-interval` announce the DataSetMetaData. Probes go to the multicast group or Ethernet address of `--url`, or to `--discovery-url` (the DiscoveryAddress). A unicast DiscoveryAddress is bound by the publisher, so other tools on the same host only send probes to it. `--discovery-layout dotnet` speaks the layout of the OPC Foundation .NET stack 2.x (which has no group and connections probes).

**Diagnostics.** `subscribe`, `publish` and `discover` count what they send and receive with the counters of the Part 14 diagnostics objects (§9.1.11: SentNetworkMessages, FailedTransmissions, ReceivedNetworkMessages, ReceivedInvalidNetworkMessages, DecryptionErrors, …) plus counters for chunks and discovery messages, and print them on stderr when done (`--diagnostics`), every `--diagnostics-interval`, as a JSON line with `--format json`. `--diagnostics-level` selects the counters by DiagnosticsLevel. `pubsub config diagnostics [--node X] [--reset]` reads the diagnostics object of a server's PublishSubscribe object, connection, group, writer or reader: the totals, every counter with its classification, level and first change, and the LiveValues.

**Output.** `subscribe` streams one line per DataSetMessage (`--jsonl` or `--format json` for JSON Lines) until Ctrl+C, `--count` or `--duration`; messages that cannot be decoded are reported on stderr (the first 10, or all with `--show-errors`). Not supported yet: the gzip-compressed JSON content type.

### PubSub Actions

PubSub Actions (Part 14 §6.2.11.2 in 1.05) call a function of another PubSub application: a Requestor sends an ActionRequest, a Responder runs the Action target and answers with an ActionResponse. `pubsub call` is a Requestor: it prints the status and outputs of the response. `pubsub respond` is a Responder for testing Requestors: it answers every request with fixed outputs or by echoing the inputs and prints each request it serves. Both run go-opcua's PubSub runtime.

| Transport | Mappings | Request goes to | Response comes back on |
|-----------|----------|-----------------|------------------------|
| UDP (`opc.udp://`) | UADP, optionally signed and encrypted | `--request-address` (the Responder's `--url`); default the caller's `--url`, e.g. a shared multicast group | the caller's `--url` |
| MQTT 3.1.1 / 5.0 | UADP, JSON | the request queue `--request-address`, default `<prefix>/<encoding>/action-request/<publisher-id>/<writer-group-name>` | `--response-address`, default the matching `action-response` topic |
| AMQP 1.0 | UADP, JSON | as MQTT (an AMQP address) | as MQTT |

The defaults of `--topic-prefix` (`opcua`), `--publisher-id` (`1`), `--writer-group-name` (`Actions`), `--writer-id` (`1`) and `--target-id` / `--target` (`1`) are the same in both commands, so `call` reaches a `respond` on the same broker without addressing flags.

| Flag | call | respond |
|------|------|---------|
| `--url`, `--interface`, `--broker-*`, `--mqtt-version`, `--transport-property` | the Requestor's connection | the Responder's connection |
| `--encoding uadp\|json` | message mapping (JSON needs a broker) | same |
| `--publisher-id` | PublisherId of the Responder | its own PublisherId |
| `--writer-id`, `--writer-group-name`, `--topic-prefix` | address the Responder's Action writer and request queue | its Action writer and request queue |
| `--request-address` | where the request goes (queue or `opc.udp://` address) | the request queue (brokers) |
| `--target-id N` / `--target ID[:Name]` | the Action target to call | the Action targets it serves (repeatable) |
| `--input` | `Name:Type=Value` inputs, in order | `Name:Type` request DataSetMetaData; wrong inputs get Bad_ArgumentsMissing / Bad_TooManyArguments / Bad_TypeMismatch |
| `--output` | `Name:Type` names (and RawData types) of the outputs | `Name:Type=Value` fixed outputs; without them the inputs are echoed |
| `--requestor-id`, `--response-address` | its own PublisherId (default random), where the response goes | |
| `--timeout` | TimeoutHint of the request and how long to wait | |
| `--resend-interval` | UDP: resend the request and acknowledgement while nothing is heard | |
| `--status`, `--delay` | | answer with another StatusCode (no outputs if Bad), after a delay |
| `--max-concurrent`, `--rate`, `--burst`, `--max-timeout` | | ActionOptions: concurrent exchanges (64), new requests per second and burst (50, 50; more are dropped), cap of the TimeoutHint (60 s) |
| `--interval` | | how often a datagram response is repeated until acknowledged (100 ms on UDP) |
| `--count`, `--duration`, `--jsonl` | | stop after N requests served or a time; JSON Lines output |
| `--message-security`, `--security-group`, `--keys-file` | security of the UADP messages | security its writer group requires |
| `--diagnostics*` | ActionRequestsSent, ActionResponsesReceived, ActionTimeouts | ActionRequestsReceived, ActionRequestsRejected, ActionResponsesSent |

`call` exits with 0 for a Good result, 3 for a Bad result of the Action (the result is printed too) and 2 when no response arrives within `--timeout`. A Responder answers nothing to a request it cannot address, that is for another writer or target, or that lacks the security of its writer group (§6.2.11.2.2), so such calls time out. On a broker a request sent before the Responder subscribed is lost. `subscribe` and `decode` show action messages as `action-request` and `action-response` DataSetMessages with their ActionTargetId, RequestId, ActionState, RequestorId, TimeoutHint and the status of the response.

Not supported: PublishedActionMethod targets bound to server Methods, Actions discovery announcements and ActionMetaData messages, chunked action messages, and adding a PublishedAction to a server with `pubsub config` (Part 14 has no method for it; `config show` lists a server's PublishedActions as data sets).

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

### Data-change filters

By default the server reports every change of value or status. A DataChangeFilter narrows that down on the server, which saves bandwidth on noisy analog signals:

| Flag | Values | Description |
|------|--------|-------------|
| `--trigger` | `status`, `status-value` (default), `status-value-timestamp` | What counts as a change: the status only, status or value, or also a new source timestamp with an unchanged value. |
| `--deadband-type` | `none` (default), `absolute`, `percent` | `absolute`: report when the value moved more than `--deadband` away from the last reported value. `percent`: `--deadband` is a percentage (0–100) of the variable's `EURange`. |
| `--deadband` | number | The deadband value. |
| `--index-range` | e.g. `0:3`, `1,0:1` | Monitor part of an array or matrix; changes outside the range do not notify. |

Deadbands need a numeric variable, and a percent deadband needs an `EURange` property (AnalogItemType). The server answers per item: rejected items are reported on stderr with their status (`BadFilterNotAllowed`, `BadMonitoredItemFilterUnsupported`, `BadDeadbandFilterInvalid`, `BadIndexRangeInvalid`) while the others keep running; the command exits with code 3 when no item could be created. The filter is only sent when one of the flags is given, so servers without filter support are unaffected.

> **Tip:** If the server revises your publishing interval (shown on stderr with `-v`), it cannot sustain the requested rate. Increase `--interval` until the revised value matches.
## FAQ

### What is opcuactl?

A single-binary OPC UA client for the terminal. It covers discovery, browsing, reading, writing, method calls, subscriptions, history and export, with output for people (tables) and for programs (JSON, YAML, CSV, JSON Lines).

### How do I find the NodeId of a tag?

Browse to it (`opcuactl browse tree -e <endpoint> --depth 3`), resolve its browse path (`opcuactl browse path`), or search by name (`opcuactl find node`). See [NodeID Formats](#nodeid-formats) and [Path Resolution](#path-resolution).

### Which security settings should I use?

Start with `opcuactl discover endpoints -e <endpoint> --best -v` to see what the server offers. For anything but a lab server use `--security-policy auto --security-mode auto` with a client certificate and `--trusted-cert` for the server's CA or certificate; see [Security](#security).

### The server rejects my certificate. What should I check?

Most servers only accept application certificates an administrator has trusted: after the first attempt, move the opcuactl certificate from the server's *rejected* to its *trusted* store. Also check that the certificate's application URI and validity period are what the server expects.

### Connection refused, timeouts, or a wrong host in the endpoint. What should I check?

Run `opcuactl ping -e <endpoint>` first. A server often advertises endpoints with an internal hostname that your machine cannot resolve; connect with the address you can reach and let `--security-policy auto --security-mode auto` take the security settings from the advertised endpoint. Raise `--timeout` on slow links.

### Will monitoring a live server disturb it?

A subscription is a normal OPC UA client session and costs the server one session, one subscription and the monitored items you create. Keep sampling intervals realistic and close sessions you no longer need; servers with a low session limit can run out.

### How do I use opcuactl in scripts and CI?

Use `--format json` (or `jsonl` for streams), `--quiet` when you only need the value, put connection settings in `OPCUACTL_*` environment variables, and branch on the [exit code](#exit-codes).

### Which servers has it been tested with?

The client library underneath, [go-opcua](https://github.com/otfabric/go-opcua), runs an interoperability suite against six OPC UA stacks — open62541, Eclipse Milo, async-opcua (Rust), UA-.NETStandard (C#), node-opcua and opcua-asyncio (Python), see its [interop comparison](https://github.com/otfabric/go-opcua#go-opcua-compared-with-other-opc-ua-stacks) — including the ECC security policies, reverse connect, discovery, data-change filters, the JSON / XML encodings and PubSub (UADP and JSON, UDP and MQTT, message security, SKS and the configuration methods, against open62541); [EXAMPLES.md](EXAMPLES.md) shows outputs captured against a WAGO PLC.

## Development

### Prerequisites

- Go 1.26+
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
| `ua-json` | `{"UaType":6,"Value":[1,2,3]}` — a Variant in the OPC UA JSON encoding |
| `ua-xml` | `<Variant xmlns="http://opcfoundation.org/UA/2008/02/Types.xsd"><Value><Double>1.5</Double></Value></Variant>` |

## Built With

- [otfabric/go-opcua](https://github.com/otfabric/go-opcua) — Pure Go OPC UA client library
- [spf13/cobra](https://github.com/spf13/cobra) — CLI framework

## Documentation

- [RECIPES.md](RECIPES.md) — End-to-end workflows for common operator tasks
- [EXAMPLES.md](EXAMPLES.md) — Real command outputs captured against a WAGO PLC
- [RELEASE.md](RELEASE.md) — Release notes
- [CONTRIBUTING.md](CONTRIBUTING.md) — How to contribute
- [SECURITY.md](SECURITY.md) — Security policy and vulnerability reporting

## License

This project is licensed under the MIT License. See [LICENSE](./LICENSE).
