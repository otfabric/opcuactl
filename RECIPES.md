# Common Recipes

End-to-end workflows for typical OPC UA operator tasks using `opcuactl`.

Aligned with **v0.3.0** (go-opcua v1.6.0): secure authN/Z over RSA and ECC policies, reverse connect, Local Discovery Server queries, IndexRange reads, OPC UA JSON / XML encodings, event WhereClause filters, monitored-item queues/overflow and data-change filters, and history `raw|modified|processed|attime`.

## Table of Contents

- [Connection presets](#connection-presets)
- [1. First Contact with an Unknown Server](#1-first-contact-with-an-unknown-server)
- [2. Find and Read a Specific Variable](#2-find-and-read-a-specific-variable)
- [3. Navigate by Browse Path](#3-navigate-by-browse-path)
- [4. Monitor Values and Events](#4-monitor-values-and-events)
- [5. Write a Value Safely](#5-write-a-value-safely)
- [6. Historical Data and Export](#6-historical-data-and-export)
- [7. Troubleshoot a Connection](#7-troubleshoot-a-connection)
- [8. Script: Poll and Log a Value](#8-script-poll-and-log-a-value)
- [9. Inspect a Method Before Calling It](#9-inspect-a-method-before-calling-it)
- [10. Use Standard Node Aliases](#10-use-standard-node-aliases)
- [11. Collect All Variables from a Device](#11-collect-all-variables-from-a-device)
- [12. Copy a Value Between Servers in the OPC UA JSON Encoding](#12-copy-a-value-between-servers-in-the-opc-ua-json-encoding)
## Connection presets

Most examples reuse a shell array for connection flags:

```sh
# Lab / anonymous (default)
CONN=(-e opc.tcp://your-server:4840 --auth anonymous --security-mode None --security-policy None)
```

Prefer `auto` once you leave None/None. Client certs are required for Sign / SignAndEncrypt:

```sh
# Anonymous over the strongest advertised secure endpoint
CONN=(-e opc.tcp://your-server:4840 --auth anonymous \
  --security-policy auto --security-mode auto \
  --cert client.pem --key client.key --trusted-cert server-ca.pem)

# Username / password
CONN=(-e opc.tcp://your-server:4840 --auth username \
  --username operator --password "$OPCUACTL_PASSWORD" \
  --security-policy auto --security-mode auto \
  --cert client.pem --key client.key --trusted-cert server-ca.pem)

# X.509 user token (same cert for channel + identity)
CONN=(-e opc.tcp://your-server:4840 --auth certificate \
  --cert client.pem --key client.key \
  --security-policy auto --security-mode auto \
  --trusted-cert server-ca.pem)

# Issued token (JWT / SAML / opaque — whatever the server's UserTokenPolicy expects)
CONN=(-e opc.tcp://your-server:4840 --auth issued --token-file ./token.jwt \
  --security-policy auto --security-mode auto \
  --cert client.pem --key client.key --trusted-cert server-ca.pem)

# ECC: with an ECDSA (or Ed25519) certificate, auto picks the server's best ECC endpoint
CONN=(-e opc.tcp://your-server:4840 \
  --security-policy auto --security-mode auto \
  --cert client-ecc.pem --key client-ecc.key --trusted-cert server-ca.pem)

# Reverse connect: the server dials opcuactl (server behind a firewall / NAT).
# No --endpoint needed; --timeout is how long to wait for the server.
CONN=(--reverse-listen 0.0.0.0:4841 --reverse-server-uri urn:plant1:plc7 --timeout 60s)
```

Inspect advertised policies, modes, and token types first:

```sh
opcuactl discover endpoints "${CONN[@]}" --best -v
```

### Environment variables

Useful when you do not want secrets on the command line:

| Variable | Flag |
|----------|------|
| `OPCUACTL_ENDPOINT` | `-e` / `--endpoint` |
| `OPCUACTL_AUTH` | `--auth` |
| `OPCUACTL_USERNAME` / `OPCUACTL_PASSWORD` | `--username` / `--password` |
| `OPCUACTL_TOKEN` / `OPCUACTL_TOKEN_FILE` | `--token` / `--token-file` |
| `OPCUACTL_CERT` / `OPCUACTL_KEY` | `--cert` / `--key` |
| `OPCUACTL_SERVER_CERT` | `--server-cert` |
| `OPCUACTL_TRUSTED_CERT` | `--trusted-cert` (comma-separated paths) |
| `OPCUACTL_SECURITY_POLICY` / `OPCUACTL_SECURITY_MODE` | `--security-policy` / `--security-mode` |

```sh
export OPCUACTL_ENDPOINT=opc.tcp://your-server:4840
export OPCUACTL_AUTH=username
export OPCUACTL_USERNAME=operator
export OPCUACTL_PASSWORD='...'
export OPCUACTL_CERT=./pki/client.pem
export OPCUACTL_KEY=./pki/client.key
export OPCUACTL_TRUSTED_CERT=./pki/server-ca.pem
export OPCUACTL_SECURITY_POLICY=auto
export OPCUACTL_SECURITY_MODE=auto

opcuactl get value -n i=2258
```

Environment variables apply when the matching flag is not set on the command line.

---

## 1. First Contact with an Unknown Server

Discover what's running, check connectivity, and understand the address space.

```sh
# Step 1: Discover servers on the network
opcuactl discover servers "${CONN[@]}"

# ... or ask the Local Discovery Server of a host (port 4840) what it knows:
# registered servers and, with the multicast extension, servers found with mDNS
opcuactl discover lds --host your-gateway

# ... narrowed to servers with data access and history
opcuactl discover servers -e opc.tcp://your-gateway:4840 --on-network --capability DA --capability HD

# Step 2: List endpoints and pick the most secure one
opcuactl discover endpoints "${CONN[@]}" --best

# Step 3: Test connectivity (TCP → SecureChannel → Session → Browse Root)
opcuactl ping "${CONN[@]}" -v

# Step 4: Check server identity and firmware
opcuactl get server "${CONN[@]}"

# Step 5: List namespaces to identify vendor-specific address spaces
opcuactl find namespace "${CONN[@]}"

# Step 6: Browse the top-level tree
opcuactl browse tree "${CONN[@]}" --depth 2
```

---

## 2. Find and Read a Specific Variable

Locate a variable by name, resolve its NodeID, and read its current value.

```sh
# Search by display name (substring match)
opcuactl find node "${CONN[@]}" --display-name Temperature

# Or search by regex pattern
opcuactl find node "${CONN[@]}" --name "Temp.*" --regex --limit 5

# Once you have the NodeID, read the value
opcuactl get value "${CONN[@]}" -n "ns=2;s=Temperature"

# Array / matrix subset (IndexRange)
opcuactl get value "${CONN[@]}" -n "ns=2;s=ArrayNode" --index-range "0:2" --timestamps

# Ask the server for source timestamps only
opcuactl get value "${CONN[@]}" -n "ns=2;s=Temperature" \
  --timestamps-to-return source --timestamps

# Full node summary (data type, access level, type definition)
opcuactl get node "${CONN[@]}" -n "ns=2;s=Temperature" --full
```

---

## 3. Navigate by Browse Path

Resolve a known path in the address space without knowing the NodeID.

```sh
# Default: resolve in namespace 0 from Objects
opcuactl browse path "${CONN[@]}" --path Server.ServerStatus

# Resolve in a vendor namespace
opcuactl browse path "${CONN[@]}" --path DeviceSet.PLC.Variables --namespace 2

# Mixed namespaces: prefix each segment
opcuactl browse path "${CONN[@]}" --path "0:Server.0:ServerStatus.0:CurrentTime"

# Use --quiet to get just the NodeID (useful in scripts)
NODE_ID=$(opcuactl browse path "${CONN[@]}" --path Server.ServerStatus --quiet)
opcuactl get value "${CONN[@]}" -n "$NODE_ID"
```

---

## 4. Monitor Values and Events

Subscribe to live data changes or filtered events.

```sh
# Monitor until Ctrl+C
opcuactl monitor value "${CONN[@]}" -n "ns=2;s=Temperature"

# Monitor for 60 seconds, sampling every 500ms
opcuactl monitor value "${CONN[@]}" -n "ns=2;s=Temperature" \
  --duration 60s --sampling 500ms

# Collect exactly 10 samples as JSONL (for piping to jq, etc.)
opcuactl monitor value "${CONN[@]}" -n "ns=2;s=Temperature" \
  --count 10 --jsonl

# Fast sampling with batched delivery (queue window + discard-oldest)
# Overflow appears as [OVERFLOW] in table / "overflow": true in JSONL
opcuactl monitor value "${CONN[@]}" -n "ns=2;s=Temperature" \
  --interval 1s --sampling 100ms --queue-size 10 --discard-oldest --jsonl

# Noisy analog signal: only report moves of more than 0.5 (absolute deadband) ...
opcuactl monitor value "${CONN[@]}" -n "ns=2;s=Temperature" \
  --deadband-type absolute --deadband 0.5 --jsonl

# ... or of more than 2 % of the variable's EURange (percent deadband)
opcuactl monitor value "${CONN[@]}" -n "ns=2;s=Level" \
  --deadband-type percent --deadband 2 --jsonl

# Heartbeat-style tags: also report a new source timestamp with an unchanged value
opcuactl monitor value "${CONN[@]}" -n "ns=2;s=Counter" --trigger status-value-timestamp

# First four elements of an array only
opcuactl monitor value "${CONN[@]}" -n "ns=2;s=Recipe" --index-range "0:3"

# Events: OfType + severity window (BaseEventType = i=2041)
opcuactl monitor event "${CONN[@]}" -n i=2253 \
  --of-type i=2041 --severity-min 500 --severity-max 1000 \
  --select EventType --select Message --select Severity \
  --jsonl --duration 30s

# Alarms (alarm-oriented select defaults)
opcuactl monitor alarm "${CONN[@]}" -n i=2253 --duration 30s --jsonl
```

---

## 5. Write a Value Safely

Validate before writing, then confirm the change.

```sh
# Dry-run: check data type, access level, and current value without writing
opcuactl write value "${CONN[@]}" -n "ns=2;s=Setpoint" \
  --value 42.5 --type double --dry-run

# Write with confirmation prompt
opcuactl write value "${CONN[@]}" -n "ns=2;s=Setpoint" \
  --value 42.5 --type double

# Write without prompt (scripting)
opcuactl write value "${CONN[@]}" -n "ns=2;s=Setpoint" \
  --value 42.5 --type double --yes

# Write a single array element
opcuactl write value "${CONN[@]}" -n "ns=2;s=ArrayNode" \
  --value 99 --type int32 --index-range "0" --yes

# Verify the write
opcuactl get value "${CONN[@]}" -n "ns=2;s=Setpoint"
```

---

## 6. Historical Data and Export

History modes: `raw` (default), `modified`, `processed` (needs `--interval` + optional `--aggregate`), `attime` (repeatable `--at`).

```sh
# Raw history over a window
opcuactl history value "${CONN[@]}" -n "ns=2;s=Temperature" \
  --start 2026-03-10T00:00:00Z --end 2026-03-11T00:00:00Z

# Modified values (audit trail of changes)
opcuactl history value "${CONN[@]}" -n "ns=2;s=Temperature" \
  --start 2026-03-10T00:00:00Z --end 2026-03-11T00:00:00Z --mode modified

# Hourly averages
opcuactl history value "${CONN[@]}" -n "ns=2;s=Temperature" \
  --start 2026-03-10T00:00:00Z --end 2026-03-11T00:00:00Z \
  --mode processed --interval 1h --aggregate average

# Exact timestamps (no --start/--end)
opcuactl history value "${CONN[@]}" -n "ns=2;s=Temperature" \
  --mode attime --at 2026-03-10T12:00:00Z --at 2026-03-10T13:00:00Z

# Export node metadata tree as JSON
opcuactl export nodes "${CONN[@]}" --depth 3 -f json --output nodes.json

# Export current values as CSV
opcuactl export values "${CONN[@]}" -n "ns=2;s=Temperature" -n "ns=2;s=Pressure" \
  -f csv --output snapshot.csv

# Export values from a subtree (auto-discover variables)
opcuactl export values "${CONN[@]}" --start-node "ns=2;i=1" --depth 2 \
  -f csv --output subtree-values.csv

# Same history modes on export
opcuactl export values "${CONN[@]}" -n "ns=2;s=Temperature" \
  --history --mode processed --interval 1h --aggregate average \
  --start 2026-03-10T00:00:00Z --end 2026-03-11T00:00:00Z \
  -f csv --output history-hourly.csv

opcuactl export values "${CONN[@]}" -n "ns=2;s=Temperature" \
  --history --mode attime \
  --at 2026-03-10T12:00:00Z --at 2026-03-10T13:00:00Z \
  -f csv --output history-attime.csv
```

`--aggregate` accepts `average` (default), `min`, `max`, `count`.

---

## 7. Troubleshoot a Connection

Diagnose connectivity and security issues step by step.

```sh
# Test basic TCP + OPC UA connectivity
opcuactl ping "${CONN[@]}" -v

# List endpoints with full detail (cert thumbprints, URL mismatches)
opcuactl discover endpoints "${CONN[@]}" -v

# Hostname mismatch is common on PLCs (dial IP vs advertised hostname)
opcuactl discover endpoints "${CONN[@]}" -v 2>&1 | grep -i mismatch

# Verify authentication works
opcuactl get server "${CONN[@]}"

# Trust a private CA / self-signed server cert without disabling validation
opcuactl get server -e opc.tcp://your-server:4840 \
  --security-policy auto --security-mode auto \
  --cert client.pem --key client.key \
  --trusted-cert ./pki/server-ca.pem

# Last resort for lab only — never in production
opcuactl get server -e opc.tcp://your-server:4840 \
  --security-policy auto --security-mode auto \
  --cert client.pem --key client.key \
  --insecure-skip-verify
```

A `BadSecurityPolicyRejected` or "no endpoint" error with an ECC policy usually means the key type of `--cert` does not fit: the `ECC_nistP256*` policies need an ECDSA P-256 certificate, `ECC_nistP384` P-384 (or P-256), `ECC_curve25519` Ed25519, and the RSA policies an RSA certificate. `discover endpoints --best --cert … --key …` shows the best endpoint your certificate can use.

With reverse connect (`--reverse-listen`), a timeout means the server never dialled in: check the client URL configured on the server, the firewall towards the listening port, and raise `--timeout` above the server's retry interval.

If username/certificate auth fails with an opaque BadIdentityToken error, retry with `--security-policy auto --security-mode auto` so the correct user-token PolicyId is taken from the endpoint.

---

## 8. Script: Poll and Log a Value

A minimal shell loop that reads a value every 5 seconds and appends to a log file.

```sh
while true; do
  opcuactl get value "${CONN[@]}" -n "ns=2;s=Temperature" -f json --quiet \
    >> temperature.log
  sleep 5
done
```

For continuous streaming, `monitor value --jsonl` is more efficient than polling:

```sh
opcuactl monitor value "${CONN[@]}" -n "ns=2;s=Temperature" \
  --jsonl --interval 5s >> temperature.jsonl
```

---

## 9. Inspect a Method Before Calling It

Check a method's input/output signature, then call it.

```sh
# Introspect the method signature
opcuactl call method "${CONN[@]}" \
  --object "ns=2;i=1" --method "ns=2;i=2" --introspect

# Call with typed arguments
opcuactl call method "${CONN[@]}" \
  --object "ns=2;i=1" --method "ns=2;i=2" \
  --arg 42 --arg hello --types int32,string

# Call with a JSON argument array
opcuactl call method "${CONN[@]}" \
  --object "ns=2;i=1" --method "ns=2;i=2" \
  --arg-json '[42, "hello"]'
```

---

## 10. Use Standard Node Aliases

Read well-known server nodes without looking up NodeIDs.

```sh
# These use symbolic aliases (no need to remember i=2258, i=2256, etc.)
opcuactl get value "${CONN[@]}" -n CurrentTime
opcuactl get value "${CONN[@]}" -n ServerStatus
opcuactl get node  "${CONN[@]}" -n Server --full
```

---

## 11. Collect All Variables from a Device

Fetch every variable under a device node — with values, types, and status.

```sh
# Direct child variables with values
opcuactl get vars "${CONN[@]}" -n "ns=3;s=PLC"

# Recursive: traverse the full subtree
opcuactl get vars "${CONN[@]}" -n "ns=2;i=5001" --recursive

# Recursive with browse paths (shows parent/child hierarchy)
opcuactl get vars "${CONN[@]}" -n "ns=2;i=5001" --recursive --paths

# Metadata only (no value reads) — useful for inventorying
opcuactl get vars "${CONN[@]}" -n "ns=3;s=PLC" --no-read

# Filter to a specific namespace
opcuactl get vars "${CONN[@]}" -n "ns=2;i=5001" --recursive --only-ns 3

# Filter by name substring
opcuactl get vars "${CONN[@]}" -n "ns=3;s=PLC" --match Revision

# Export as JSON for automation
opcuactl get vars "${CONN[@]}" -n "ns=3;s=PLC" -f json

# Pipe quiet mode to read each variable individually
opcuactl get vars "${CONN[@]}" -n "ns=3;s=PLC" -q | while read nid; do
  opcuactl get node "${CONN[@]}" -n "$nid" --full
done
```

---

## 12. Copy a Value Between Servers in the OPC UA JSON Encoding

`--ua-encoding` prints values in the standard OPC UA DataEncoding (Part 6), which keeps the exact data type — arrays, LocalizedText, structures — and is understood by other OPC UA stacks. `write value --type ua-json` takes it back.

```sh
# Read a DataValue in the OPC UA JSON encoding
opcuactl get value "${CONN[@]}" -n "ns=2;s=Recipe" --ua-encoding json
# {"UaType":6,"Value":[10,20,30],"SourceTimestamp":"2026-10-12T09:15:02.118Z"}

# Copy it to another server: keep the Variant part (UaType + Value, and
# Dimensions for matrices) and write it with --type ua-json
VALUE=$(opcuactl get value "${CONN[@]}" -n "ns=2;s=Recipe" --ua-encoding json \
  | jq -c '{UaType, Value} + (if .Dimensions then {Dimensions} else {} end)')
opcuactl write value -e opc.tcp://other-server:4840 -n "ns=2;s=Recipe" \
  --value "$VALUE" --type ua-json --yes

# XML instead, e.g. to paste into a NodeSet2 file or another tool
opcuactl get value "${CONN[@]}" -n "ns=2;s=Recipe" --ua-encoding xml --pretty
```
