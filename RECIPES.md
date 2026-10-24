# Common Recipes

End-to-end workflows for typical OPC UA operator tasks using `opcuactl`.

Aligned with **v0.4.1** (go-opcua v1.8.1): OPC UA PubSub (UADP / JSON over UDP and MQTT, message security, Security Key Service, remote configuration), secure authN/Z over RSA and ECC policies, reverse connect, Local Discovery Server queries, IndexRange reads, OPC UA JSON / XML encodings, event WhereClause filters, monitored-item queues/overflow and data-change filters, and history `raw|modified|processed|attime`.

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
- [13. Find Out What a PubSub Publisher Sends](#13-find-out-what-a-pubsub-publisher-sends)
- [14. Read Secured PubSub Messages with Keys from an SKS](#14-read-secured-pubsub-messages-with-keys-from-an-sks)
- [15. Test a PubSub Subscriber](#15-test-a-pubsub-subscriber)
- [16. Reconfigure the PubSub of a Server](#16-reconfigure-the-pubsub-of-a-server)
- [17. PubSub Through an MQTT 5.0 or AMQP 1.0 Broker](#17-pubsub-through-an-mqtt-50-or-amqp-10-broker)
- [18. PubSub over Raw Ethernet](#18-pubsub-over-raw-ethernet)
- [19. Send and Receive Large DataSets in Chunks](#19-send-and-receive-large-datasets-in-chunks)
- [20. Find Publishers with UADP Discovery](#20-find-publishers-with-uadp-discovery)
- [21. Watch PubSub Diagnostics Counters](#21-watch-pubsub-diagnostics-counters)
- [22. Call PubSub Actions (Request / Response)](#22-call-pubsub-actions-request--response)
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
| `OPCUACTL_BROKER_PASSWORD` (or the older `OPCUACTL_MQTT_PASSWORD`) | `pubsub --broker-password` (MQTT and AMQP brokers) |

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

---

## 13. Find Out What a PubSub Publisher Sends

```sh
# Everything on the multicast group, for 10 seconds: publisher ids, writer group
# and writer ids, sequence numbers, security, field values
opcuactl pubsub subscribe --url opc.udp://224.0.0.22:4840 --interface eth0 --duration 10s

# Unnamed fields (#0, #1, ...)? Read the DataSetMetaData from the publishing server:
# "pubsub config show" lists its published data sets and readers with their NodeIds
opcuactl pubsub config show "${CONN[@]}"
opcuactl pubsub subscribe --url opc.udp://224.0.0.22:4840 --metadata-node "ns=2;i=101" "${CONN[@]}"

# Record one publisher's writer as JSON Lines for later analysis
opcuactl pubsub subscribe --url opc.udp://224.0.0.22:4840 --publisher-id 2234 --writer-id 1 \
  --jsonl --duration 1h > capture.jsonl

# MQTT: JSON publishers usually announce their metadata (retained ua-metadata)
opcuactl pubsub subscribe --url mqtt://broker:1883 --topic 'plant/#' --show-metadata

# A message captured elsewhere (Wireshark "Copy as hex stream", MQTT client, ...)
opcuactl pubsub decode message.hex --field Counter:UInt32 --field Temp:Double
```

`--format json` / `--jsonl` give `publisherId`, `writerGroupId`, `dataSetWriterId`, `sequenceNumber`, `security` and one entry per field — easy to check with `jq` (e.g. gaps in sequence numbers).

---

## 14. Read Secured PubSub Messages with Keys from an SKS

Secured UADP messages need the keys of their SecurityGroup. The Security Key Service hands them out only over a SignAndEncrypt session, usually to a named user:

```sh
SKS=(
  -e opc.tcp://sks.example:4840
  --security-policy Basic256Sha256 --security-mode SignAndEncrypt
  --cert ./pki/client.pem --key ./pki/client.key --trusted-cert ./pki/sks.pem
  --auth username --username subscriber --password "$OPCUACTL_PASSWORD"
)

# Which policy, which token, how long until the next key?
opcuactl pubsub sks get-keys "${SKS[@]}" --security-group sg1

# Subscribe with keys pulled from the SKS (refreshed before they rotate);
# reject anything that is not signed and encrypted
opcuactl pubsub subscribe --url opc.udp://224.0.0.22:4840 --security-group sg1 \
  --message-security sign-and-encrypt "${SKS[@]}"

# Offline: save the current and two future keys (a secret!) and use the file
opcuactl pubsub sks get-keys "${SKS[@]}" --security-group sg1 --future-keys 2 \
  --show-keys --format json > sg1-keys.json
chmod 600 sg1-keys.json
opcuactl pubsub decode secured.hex --keys-file sg1-keys.json
```

---

## 15. Test a PubSub Subscriber

```sh
# Feed a subscriber (PublisherId 2234, WriterGroupId 100, DataSetWriterId 1)
# with a DataSet every 500 ms until Ctrl+C
opcuactl pubsub publish --url opc.udp://224.0.0.22:4840 --publisher-id 2234 \
  --writer-group-id 100 --writer-id 1 \
  --field Temperature:Double=21.5 --field Running:Boolean=true --count 0 --interval 500ms

# RawData fields, announcing the DataSetMetaData first
opcuactl pubsub publish --url opc.udp://224.0.0.22:4840 --field Counter:UInt32=1 \
  --field-encoding rawdata --announce-metadata

# Signed and encrypted with the SecurityGroup's keys
opcuactl pubsub publish --url opc.udp://224.0.0.22:4840 --field Counter:UInt32=1 \
  --message-security sign-and-encrypt --security-group sg1 "${SKS[@]}"

# JSON over MQTT, with retained metadata
opcuactl pubsub publish --url mqtt://broker:1883 --encoding json --topic plant/line1 \
  --metadata-topic plant/line1/meta --field Temperature:Double=21.5 --qos 1
```

Every message is a key frame; sequence numbers start at 1 with every run, so a subscriber that remembers the previous run may discard the first messages of the next one (use another `--writer-id`, or wait for its MessageReceiveTimeout).

---

## 16. Reconfigure the PubSub of a Server

```sh
# What is configured, with NodeIds and states
opcuactl pubsub config show "${CONN[@]}"

# Publish two variables to a new multicast group
CONN_ID=$(opcuactl pubsub config add-connection "${CONN[@]}" --name Diag \
  --url opc.udp://239.0.0.10:4840 --publisher-id 2234 -q)
opcuactl pubsub config add-dataset "${CONN[@]}" --name Diag \
  --var Temperature="ns=1;s=Temperature" --var Pressure="ns=1;s=Pressure"
GROUP_ID=$(opcuactl pubsub config add-writer-group "${CONN[@]}" --connection "$CONN_ID" \
  --name DiagGroup --writer-group-id 100 --interval 500ms -q)
opcuactl pubsub config add-writer "${CONN[@]}" --group "$GROUP_ID" --name DiagWriter \
  --writer-id 1 --dataset Diag

# Check it, pause it, remove it
opcuactl pubsub subscribe --url opc.udp://239.0.0.10:4840 --count 3
opcuactl pubsub config disable "${CONN[@]}" --node "$GROUP_ID"
opcuactl pubsub config remove connection "${CONN[@]}" --node "$CONN_ID"
```

The add-connection command also takes Ethernet and broker connections: `--url opc.eth://01-00-5E-00-00-01 --interface eth0`, `--url mqtt://broker:1883 --mqtt-version 5.0`, or `--url amqp://broker:5672 --transport-property target-capabilities=queue` (`--transport-property` is repeatable and becomes the ConnectionProperties of the connection).

---

## 17. PubSub Through an MQTT 5.0 or AMQP 1.0 Broker

`--broker-username` and `--broker-password` (or `OPCUACTL_BROKER_PASSWORD`) log in to the broker. `--username` and `--password` are for the OPC UA session, for example to an SKS. The 0.4 flags `--mqtt-username`, `--mqtt-password` and `--mqtt-client-id` still work.

```sh
export OPCUACTL_BROKER_PASSWORD='...'

# MQTT 5.0, QoS 1: each message shows its topic, content type, UAMessageType and user properties
opcuactl pubsub subscribe --url mqtts://broker:8883 --mqtt-version 5.0 --qos 1 \
  --broker-username plant --topic 'plant/#' --show-metadata

# Publish JSON with MQTT 5.0 connection properties
opcuactl pubsub publish --url mqtts://broker:8883 --mqtt-version 5.0 --qos 1 --broker-username plant \
  --topic plant/line1 --encoding json --field Temp:Double=21.5 --metadata-topic plant/line1/meta \
  --transport-property "connection-Session Expiry Interval=60"

# The same, gzip compressed (Content Type application/json+gzip, Part 14 §7.3.4.9.1).
# Needs MQTT 5.0; subscribe decompresses compressed messages on its own, also on 3.1.1.
opcuactl pubsub publish --url mqtts://broker:8883 --mqtt-version 5.0 --qos 1 --broker-username plant \
  --topic plant/line1 --encoding json --json-compression gzip --field Temp:Double=21.5

# RabbitMQ 4 (AMQP 1.0): the address of a queue is /queues/<name>; the queue must exist.
# A vhost other than / goes in the connection hostname.
opcuactl pubsub subscribe --url amqps://rabbit:5671 --broker-username plant --topic /queues/plant \
  --transport-property connection-hostname=vhost:production

# ActiveMQ Artemis: plain addresses; the capabilities make them anycast queues
opcuactl pubsub subscribe --url amqp://artemis:5672 --broker-username plant --topic plant.line1 \
  --transport-property source-capabilities=queue
opcuactl pubsub publish --url amqp://artemis:5672 --broker-username plant --topic plant.line1 \
  --transport-property target-capabilities=queue --field Temp:Double=21.5 --count 0 --interval 1s
```

The subscriber picks the decoder from the content type (`application/opcua+uadp` or `application/json`) and detects the mapping when there is none. `--qos 2` is exactly-once on MQTT and at-least-once on AMQP.

---

## 18. PubSub over Raw Ethernet

`opc.eth://` sends UADP NetworkMessages straight in Ethernet frames (EtherType 0xB62C), optionally VLAN tagged (`opc.eth://01-00-5E-00-00-01:100.5` is VLAN 100, priority 5). It needs `--interface` and raw-socket rights: run as root, or on Linux grant the capability once.

```sh
sudo setcap cap_net_raw+ep "$(command -v opcuactl)"

opcuactl pubsub subscribe --url opc.eth://01-00-5E-00-00-01 --interface eth0 --show-metadata
opcuactl pubsub publish --url opc.eth://01-00-5E-00-00-01 --interface eth0 --publisher-id 2234 \
  --field Temp:Double=21.5 --announce-metadata --count 0 --interval 100ms
```

Ethernet works on Linux, macOS and FreeBSD (on macOS through BPF devices, usually as root). JSON is not available over Ethernet.

---

## 19. Send and Receive Large DataSets in Chunks

A UADP NetworkMessage larger than the transport carries is split into chunks. `--max-message-size` makes `publish` chunk earlier, for example to test a subscriber:

```sh
opcuactl pubsub publish --url opc.udp://224.0.0.22:4840 --publisher-id 2234 \
  --field "Readings:String=$(seq -s, 1 400)" --max-message-size 512 --diagnostics
```

`subscribe` and `decode` reassemble chunks. On a busy or lossy network, bound what the reassembly keeps:

```sh
opcuactl pubsub subscribe --url opc.udp://224.0.0.22:4840 \
  --chunk-max-message-size 1048576 --chunk-max-pending 64 --chunk-max-pending-bytes 8388608 --chunk-timeout 2s \
  --diagnostics-interval 30s

# Chunks captured as separate files
opcuactl pubsub decode chunk-1.bin chunk-2.bin chunk-3.bin chunk-4.bin
```

ChunksReceived, ChunkedMessagesReceived and ChunksDiscarded in the diagnostics show how many chunks arrived, how many messages they completed and how many were dropped (timeout, limits).

---

## 20. Find Publishers with UADP Discovery

UADP discovery works on UDP and Ethernet. Probes go to the multicast group of `--url`, or to the publishers' DiscoveryAddress (`--discovery-url`).

```sh
# Who publishes on this group?
opcuactl pubsub discover --url opc.udp://224.0.0.22:4840

# The DataSetMetaData and writer configuration of writer 1 of publisher 2234
opcuactl pubsub discover --url opc.udp://224.0.0.22:4840 --probe metadata,writers --publisher-id 2234 --writer-id 1

# The PubSubConnections with their groups, from publishers whose DiscoveryAddress is another group
opcuactl pubsub discover --url opc.udp://224.0.0.23:4840 --listen-url opc.udp://224.0.0.22:4840 \
  --probe connections --include-groups --format json

# Subscribe and probe for the metadata of every writer seen without one
opcuactl pubsub subscribe --url opc.udp://224.0.0.22:4840 --probe --show-metadata

# A test publisher that answers probes and announces its metadata every 10 seconds
opcuactl pubsub publish --url opc.udp://224.0.0.22:4840 --publisher-id 2234 --field Temp:Double=21.5 \
  --count 0 --answer-probes --announce-interval 10s
```

`--discovery-layout dotnet` sends probes in the byte layout of the OPC Foundation .NET stack 2.x, whose probes differ from the specification's layout (and which has no group or connections probes). A publisher binds a unicast DiscoveryAddress, so a tool on the same host can only send probes to it; use a multicast DiscoveryAddress to listen alongside the publisher.

---

## 21. Watch PubSub Diagnostics Counters

`subscribe`, `publish` and `discover` keep the Part 14 diagnostics counters of the component they play (a reader group, a writer group) plus counters for chunks and discovery, and print them on stderr:

```sh
# Every 10 seconds and on exit, with the Advanced counters (DecryptionErrors, ...)
opcuactl pubsub subscribe --url opc.udp://224.0.0.22:4840 --security-group sg1 --keys-file keys.json \
  --diagnostics-interval 10s --diagnostics-level advanced

# As a JSON line on stderr, next to JSON Lines on stdout
opcuactl pubsub subscribe --url opc.udp://224.0.0.22:4840 --format jsonl --diagnostics 2> diagnostics.jsonl
```

The diagnostics of a server's PubSub runtime come from its diagnostics objects:

```sh
opcuactl pubsub config diagnostics "${CONN[@]}"                      # the PublishSubscribe object
opcuactl pubsub config diagnostics "${CONN[@]}" --node "$GROUP_ID"   # a writer group, with SentNetworkMessages
opcuactl pubsub config diagnostics "${CONN[@]}" --node "$GROUP_ID" --reset --format json
```

Counters above the DiagnosticsLevel of the object are shown as inactive (`-`).

---

## 22. Call PubSub Actions (Request / Response)

PubSub Actions (Part 14 §6.2.11.2 in 1.05) let a Requestor call a function of a Responder over the PubSub transports. `pubsub respond` is a test Responder, `pubsub call` a Requestor; together they check a broker or network path, and each tests the other side of a real application.

### Through an MQTT broker

The Responder takes its requests from the request queue of its writer group, by default `<prefix>/<encoding>/action-request/<publisher-id>/<writer-group-name>`, and answers on the matching `action-response` topic. `call` builds the same queue name from the same flags, so they meet without addressing flags:

```sh
export OPCUACTL_BROKER_PASSWORD='...'
B=(--url mqtts://broker:8883 --broker-username plant --mqtt-version 5.0 --encoding json --publisher-id line1-drive)

# Terminal 1: a Responder with target 1 "Start" that takes a speed and answers with a state
opcuactl pubsub respond "${B[@]}" --target 1:Start --input Speed:Double --output State:String=running

# Terminal 2: call it; the result as text, then as JSON
opcuactl pubsub call "${B[@]}" --target-id 1 --input Speed:Double=1450 --output State:String
opcuactl pubsub call "${B[@]}" --target-id 1 --input Speed:Double=1450 --format json

# Terminal 3: watch the exchange: request (Executing), response (Done), acknowledgement (Idle)
opcuactl pubsub subscribe --url mqtts://broker:8883 --broker-username plant --mqtt-version 5.0 --topic 'opcua/#'
```

Another application's Responder usually has its own queue: give it with `--request-address` (and `--response-address` if responses go elsewhere). Start the Responder first: a request sent before it subscribed is lost, and the call times out after `--timeout`.

### Over UDP

Over UDP the Requestor listens on its own `--url` and sends the request to the Responder's address. `--resend-interval` resends the request (and the acknowledgement) while nothing is heard; the Responder repeats its response every `--interval` until it is acknowledged.

```sh
# The Responder on 10.0.0.9, echoing two inputs; requests must be signed and encrypted
opcuactl pubsub respond --url opc.udp://10.0.0.9:4840 --target 1:Echo --input A:Int32 --input B:String \
  --message-security sign-and-encrypt --keys-file keys.json

# The Requestor on 10.0.0.5
opcuactl pubsub call --url opc.udp://10.0.0.5:4841 --request-address opc.udp://10.0.0.9:4840 \
  --input A:Int32=2 --input B:String=pump --resend-interval 200ms \
  --message-security sign-and-encrypt --keys-file keys.json

# Both on one multicast group: no --request-address needed
opcuactl pubsub call --url opc.udp://239.0.0.1:4840 --input A:Int32=2 --input B:String=pump --resend-interval 200ms
```

### Test the error paths of a Requestor

```sh
# Answer with a Bad status (no outputs), or too late for the TimeoutHint
opcuactl pubsub respond --url opc.udp://10.0.0.9:4840 --status BadInternalError
opcuactl pubsub respond --url opc.udp://10.0.0.9:4840 --delay 15s

# Limit the Responder and watch what it rejects
opcuactl pubsub respond --url opc.udp://10.0.0.9:4840 --max-concurrent 4 --rate 10 --burst 5 --max-timeout 5s \
  --diagnostics-interval 10s
```

In scripts, branch on the exit code of `call`: 0 for a Good result, 3 for a Bad result (printed as well), 2 when no response came within `--timeout`. `--quiet` prints only the output values, one per line.
