# Common Recipes

End-to-end workflows for typical OPC UA operator tasks using `opcuactl`.

Aligned with **v0.10.0**: a test server (`serve`), UA JSON over WebSockets and HTTPS (wss-uajson, https-uajson), Alarms & Conditions (list, acknowledge, confirm, shelve, watch), server diagnostics and DiagnosticInfos, certificate management by the GDS push model (trust list, CSR, certificate renewal, rejected certificates), `opc.wss` / `opc.https` endpoints with TLS, OPC UA PubSub (UADP / JSON over UDP and MQTT, message security, Security Key Service, remote configuration), secure authN/Z over RSA and ECC policies, reverse connect, Local Discovery Server queries, IndexRange reads, OPC UA JSON / XML encodings, event WhereClause filters, monitored-item queues/overflow and data-change filters, history `raw|modified|processed|attime`, and historical events (read with filters and paging, insert, replace, update, delete).

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
- [23. Handle Alarms: List, Acknowledge, Confirm, Shelve](#23-handle-alarms-list-acknowledge-confirm-shelve)
- [24. Who Is Connected? Server Diagnostics](#24-who-is-connected-server-diagnostics)
- [25. Connect over HTTPS or a WebSocket](#25-connect-over-https-or-a-websocket)
- [26. Manage a Server's Certificates (GDS Push)](#26-manage-a-servers-certificates-gds-push)
- [27. Alarm Maintenance: Out of Service, Suppression, Latched Alarms, Dialogs, Audit](#27-alarm-maintenance-out-of-service-suppression-latched-alarms-dialogs-audit)
- [28. Get a Certificate from a GDS (Pull)](#28-get-a-certificate-from-a-gds-pull)
- [29. Run a Test Server and Talk to It (incl. UA JSON)](#29-run-a-test-server-and-talk-to-it-incl-ua-json)
## Connection presets

Most examples reuse a shell array for connection flags:

```sh
# Lab / anonymous (default)
CONN=(-e opc.tcp://your-server:4840 --auth anonymous --insecure-no-security)
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

# ECC: with an ECDSA (NIST or Brainpool), Ed25519 or Ed448 certificate, auto picks the server's best ECC endpoint
CONN=(-e opc.tcp://your-server:4840 \
  --security-policy auto --security-mode auto \
  --cert client-ecc.pem --key client-ecc.key --trusted-cert server-ca.pem)

# WebSocket (opc.wss) or HTTPS (opc.https): TLS verified against the CA of the server's TLS certificate
CONN=(-e opc.wss://your-server:4843 --tls-ca ./pki/tls-ca.pem)
CONN=(-e opc.https://your-server:4443 --tls-ca ./pki/tls-ca.pem \
  --auth username --username operator --password "$OPCUACTL_PASSWORD")

# The same in UA JSON (wss-uajson / https-uajson) instead of UA Binary
CONN=(-e opc.wss://your-server:4843 --transport wss-uajson --tls-ca ./pki/tls-ca.pem)

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
| `OPCUACTL_INSECURE_NO_SECURITY` | `--insecure-no-security` (`true` / `false`) |
| `OPCUACTL_TLS_CA` | `--tls-ca` (`opc.wss` / `opc.https`) |
| `OPCUACTL_TLS_CERT` / `OPCUACTL_TLS_KEY` | `--tls-cert` / `--tls-key` (mutual TLS) |
| `OPCUACTL_TLS_INSECURE_SKIP_VERIFY` | `--tls-insecure-skip-verify` (`true` / `false`; development only) |
| `OPCUACTL_TRANSPORT` | `--transport` (`tcp`, `wss`, `https`, `wss-uajson`, `https-uajson`) |
| `OPCUACTL_JSON_CONTENT_TYPE` | `--json-content-type` (`standard`, `legacy`; https-uajson) |
| `OPCUACTL_RETURN_DIAGNOSTICS` | `--return-diagnostics` (`none`, `service`, `operation`, `all`) |
| `OPCUACTL_BROKER_PASSWORD` (or the older `OPCUACTL_MQTT_PASSWORD`) | `pubsub --broker-password` (MQTT and AMQP brokers) |
| `OPCUACTL_SERVE_USERS` | `serve --user` (comma-separated `name:password`) |

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

# Condition state with ConditionRefresh and EventIds (see recipe 23)
opcuactl alarms watch "${CONN[@]}" --duration 30s --jsonl
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

Historical events of an event notifier (an Object with the HistoryRead bit in its EventNotifier, often the Server object `i=2253` or an area): `--select` picks fields by BrowsePath, `--of-type` / `--severity-min` / `--severity-max` filter on the server, `--num-values` pages and `--max-events` stops early.

```sh
# A day of events, oldest first, as JSON Lines
opcuactl history event "${CONN[@]}" -n i=2253 \
  --start 2026-03-10T00:00:00Z --end 2026-03-11T00:00:00Z --num-values 500 -f jsonl > events.jsonl

# The 20 latest alarm events of Severity >= 700 with their state
opcuactl history event "${CONN[@]}" -n i=2253 --end "$(date -u +%Y-%m-%dT%H:%M:%SZ)" --max-events 20 \
  --of-type AlarmConditionType --severity-min 700 \
  --select EventId --select Time --select SourceName --select ConditionId --select ActiveState/Id --select AckedState/Id

# Annotate the history, fix a typo, remove a wrong entry (HistoryWrite needed)
opcuactl history update-events "${CONN[@]}" -n i=2253 --yes \
  --event '{"Time":"2026-03-10T06:00:00Z","SourceName":"Line 2","Message":"planned stop","Severity":100}'
opcuactl history update-events "${CONN[@]}" -n i=2253 --mode replace --yes \
  --event '{"EventId":"<base64 from history event>","Message":"planned stop, line 2","Severity":100}'
opcuactl history delete-events "${CONN[@]}" -n i=2253 --event-id "<base64 from history event>" --yes

# Load events from a file, check them first
opcuactl history update-events "${CONN[@]}" -n i=2253 --events-file shift-log.json --dry-run
opcuactl history update-events "${CONN[@]}" -n i=2253 --events-file shift-log.json --yes || echo "exit $? (7: some events failed)"
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

A `BadSecurityPolicyRejected` or "no endpoint" error with an ECC policy usually means the key type of `--cert` does not fit: the `ECC_nistP256*` policies need an ECDSA P-256 certificate, `ECC_nistP384` P-384 (or P-256), `ECC_curve25519` Ed25519, `ECC_brainpoolP256r1` / `ECC_brainpoolP384r1` an ECDSA key on that Brainpool curve, `ECC_curve448` Ed448, and the RSA policies an RSA certificate. `discover endpoints --best --cert … --key …` shows the best endpoint your certificate can use.

With reverse connect (`--reverse-listen`), a timeout means the server never dialled in: check the client URL configured on the server, the firewall towards the listening port, and raise `--timeout` above the server's retry interval.

When the server answers with a Bad status and nothing more, ask it why with `--return-diagnostics all`: the DiagnosticInfos it returns (symbolic id, text, and as additional info the operation, e.g. the node and attribute) are printed on stderr.

```sh
opcuactl get value "${CONN[@]}" -n "ns=2;s=Tank.Level" --return-diagnostics all
opcuactl write value "${CONN[@]}" -n "ns=2;s=Setpoint" --value 42 --type double --yes --return-diagnostics operation
```

For `opc.wss` / `opc.https` endpoints, `x509: certificate signed by unknown authority` concerns the TLS certificate: pass its CA with `--tls-ca` (not `--trusted-cert`, which is for the application instance certificate); a handshake failure against a server that wants mutual TLS needs `--tls-cert` / `--tls-key`.

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

The JSON values take the types of the method's InputArguments: for a method `Add(a Int32, b Int32)`, `--arg-json '[2, 40]'` sends two Int32. A value that does not fit (`2.5`, `3000000000`) is rejected with exit 2 before the call.

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

In scripts, branch on the exit code of `call`: 0 for a Good result, 6 for a Bad result (printed as well), 5 when no response came within `--timeout`. `--quiet` prints only the output values, one per line.

---

## 23. Handle Alarms: List, Acknowledge, Confirm, Shelve

The Part 9 workflow from a terminal: see what is pending, acknowledge it with a comment, confirm it once the cause is fixed, shelve a nuisance alarm for a while.

```sh
# What is pending: the retained conditions, highest severity first
opcuactl alarms list "${CONN[@]}"
opcuactl alarms list "${CONN[@]}" --state unacked --severity-min 500 -v

# Of one area or source only (the notifier the events come from)
opcuactl alarms list "${CONN[@]}" --node "ns=2;s=Area1"

# Acknowledge with a comment; the EventId of the latest event is looked up with a ConditionRefresh
opcuactl alarms ack "${CONN[@]}" "ns=2;s=Tank.LevelAlarm" -m "operator on site"

# Confirm after the fix (alarms with ConfirmedState only), and leave a note
opcuactl alarms confirm "${CONN[@]}" "ns=2;s=Tank.LevelAlarm" -m "level sensor replaced"
opcuactl alarms comment "${CONN[@]}" "ns=2;s=Tank.LevelAlarm" -m "spare part ordered"

# Shelve a chattering alarm during maintenance, then unshelve it
opcuactl alarms shelve "${CONN[@]}" "ns=2;s=Tank.LevelAlarm" --for 2h
opcuactl alarms unshelve "${CONN[@]}" "ns=2;s=Tank.LevelAlarm"

# Take a condition out of service and back
opcuactl alarms disable "${CONN[@]}" "ns=2;s=Tank.LevelAlarm"
opcuactl alarms enable "${CONN[@]}" "ns=2;s=Tank.LevelAlarm"
```

Script it: acknowledge every unacknowledged alarm of an area with one comment.

```sh
opcuactl alarms list "${CONN[@]}" --node "ns=2;s=Area1" --state unacked -q |
while read -r cond; do
  opcuactl alarms ack "${CONN[@]}" "$cond" -m "acknowledged by shift handover" || echo "failed: $cond" >&2
done
```

Log the condition events as JSON Lines (the refresh first, marked `"refresh": "start"` / `"end"`, then every change):

```sh
opcuactl alarms watch "${CONN[@]}" --jsonl >> alarms.jsonl
jq -r 'select(.conditionId) | [.ts, .conditionName, .severity, .active, .acked] | @tsv' alarms.jsonl
```

To acknowledge exactly the event an operator saw (not a newer one), pass its EventId from `alarms list` or `alarms watch`: `--event-id +LbyRwxcuik8Tz1iutx3ug==`. A server that refused the method exits with code 6 (9 for `the user may not call this method`) and the reason: `already acknowledged`, `the EventId is not one of the recent events of the condition`, `the condition is disabled`, `the user may not call this method` (connect with an identity that may).

---

## 24. Who Is Connected? Server Diagnostics

When a server runs out of sessions or a client misbehaves, its diagnostics tell who is connected, from where and how busy they are.

```sh
# Counters: current and cumulated sessions and subscriptions, rejected sessions and requests, timeouts
opcuactl diagnostics summary "${CONN[@]}"

# The sessions: client application, endpoint, connect and last contact time, subscriptions, requests
opcuactl diagnostics sessions "${CONN[@]}"

# One session with its request counts per service (an administrator for other sessions)
opcuactl diagnostics sessions "${CONN[@]}" --session "ns=3;g=5F1C..."
opcuactl diagnostics sessions "${CONN[@]}" --session self

# The user, authentication and security of a session
opcuactl diagnostics sessions "${CONN[@]}" --security --session self

# Subscriptions: publishing interval, items, notifications, republish requests (a lagging client)
opcuactl diagnostics subscriptions "${CONN[@]}"
opcuactl diagnostics subscriptions "${CONN[@]}" --format json | jq 'sort_by(-.republishRequestCount) | .[0:5]'

# Sampling intervals in use: what the server samples how often
opcuactl diagnostics sampling "${CONN[@]}"
```

Find sessions that have been quiet for long (a client that died without closing its session):

```sh
opcuactl diagnostics sessions "${CONN[@]}" --format json |
  jq -r '.[] | [.clientLastContactTime, .sessionName, .clientName, .endpointUrl] | @tsv' | sort
```

Diagnostics collection can be switched off on servers that care about the overhead; switching it on resets the counters. Both need an administrator (SecurityAdmin or ConfigureAdmin); a refusal says so:

```sh
ADMIN=(--auth username --username admin --password "$ADMIN_PASSWORD")
opcuactl diagnostics enable "${CONN[@]}" "${ADMIN[@]}"
opcuactl diagnostics disable "${CONN[@]}" "${ADMIN[@]}"
```

---

## 25. Connect over HTTPS or a WebSocket

Some servers (and gateways in front of them) offer `opc.wss://` (OPC UA over a TLS WebSocket) or `opc.https://` (UA Binary over HTTPS) next to, or instead of, `opc.tcp`, often because only port 443 gets through a firewall.

```sh
# Which endpoints does it offer? Each one shows its transport (some servers list
# only those of the transport you ask on, others all; --transport-profile filters)
opcuactl discover endpoints -e opc.wss://gateway.example:443 --tls-ca ./pki/tls-ca.pem
opcuactl discover endpoints -e opc.tcp://gateway.example:4840 --transport-profile wss

# Is the TLS listener reachable? (TCP, then TLS + session, then browse)
opcuactl ping -e opc.https://gateway.example:443 --tls-ca ./pki/tls-ca.pem

# Read, monitor, acknowledge: every command works over either transport
opcuactl get value -e opc.wss://gateway.example:443 --tls-ca ./pki/tls-ca.pem -n i=2258
opcuactl monitor value -e opc.https://gateway.example:443 --tls-ca ./pki/tls-ca.pem -n "ns=2;s=Temp" --duration 1m

# A secure channel inside the WebSocket: auto picks among the opc.wss endpoints
opcuactl get server -e opc.wss://gateway.example:443 --tls-ca ./pki/tls-ca.pem \
  --security-policy auto --security-mode auto --cert client.pem --key client.key --trusted-cert server-ca.pem

# Mutual TLS: the gateway wants a client TLS certificate
opcuactl browse children -e opc.wss://gateway.example:443 --tls-ca ./pki/tls-ca.pem \
  --tls-cert ./pki/tls-client.pem --tls-key ./pki/tls-client.key

# Lab only: skip the TLS verification (prints a warning)
opcuactl get value -e opc.wss://localhost:4843 --tls-insecure-skip-verify -n i=2258

# The same URLs in UA JSON instead of UA Binary (wss-uajson is None only)
opcuactl discover endpoints -e opc.wss://gateway.example:443 --tls-ca ./pki/tls-ca.pem --transport-profile wss-uajson
opcuactl get value -e opc.wss://gateway.example:443 --transport wss-uajson --tls-ca ./pki/tls-ca.pem -n i=2258
# A UA-.NETStandard server wants the Content-Type application/opcua+uajson
opcuactl get value -e opc.https://gateway.example:443 --transport https-uajson --json-content-type legacy --tls-ca ./pki/tls-ca.pem -n i=2258
```

Two certificates are in play: the TLS certificate of the listener (`--tls-ca`, `--tls-cert` / `--tls-key`) and the application instance certificates of OPC UA (`--cert`, `--key`, `--trusted-cert`). Over HTTPS there is no secure channel; servers usually offer the None endpoint there and accept user names because TLS protects them.

---

## 26. Manage a Server's Certificates (GDS Push)

Servers with push certificate management (OPC UA Part 12 §7: go-opcua with `WithPushManagement`, open62541 built with GDS push, UA-.NETStandard, node-opcua) let an administrator change their trust list and application instance certificate remotely. It needs a session with the **SecurityAdmin** role over a **SignAndEncrypt** channel, and opcuactl's own certificate in the server's trust list.

```sh
SEC=(-e opc.tcp://plc:4840 --security-policy Basic256Sha256 --security-mode SignAndEncrypt
     --cert ./pki/opcuactl.pem --key ./pki/opcuactl.key --trusted-cert ./pki/ca.pem
     --auth username --username secadmin --password "$SECADMIN_PASSWORD")

# Does the server do it, and with which keys and certificate types?
opcuactl gds props "${SEC[@]}"
```

**Onboard a new client** that the server refused on its first connection:

```sh
opcuactl gds rejected "${SEC[@]}"                      # subject, thumbprint, validity
opcuactl gds rejected "${SEC[@]}" --out ./rejected      # <thumbprint>.der files
opcuactl gds trustlist add "${SEC[@]}" ./rejected/<thumbprint>.der
```

**Trust a CA instead of single certificates**: CA certificates in the *trusted* list make everything they issued trusted; in the *issuer* list (`--issuer`) they only complete chains of certificates trusted otherwise. CRLs come with `trustlist set`:

```sh
opcuactl gds trustlist add "${SEC[@]}" ca.pem                       # trust everything the CA issued
opcuactl gds trustlist add "${SEC[@]}" --issuer intermediate.pem    # chain validation only
opcuactl gds trustlist remove "${SEC[@]}" 3F2A9C...C01B             # or: remove old-client.der
```

**Back up, edit and restore the trust list**: `--out` writes the layout OPC UA applications use for their PKI directory, and `set` reads it back (it replaces all four lists unless `--masks` names fewer):

```sh
opcuactl gds trustlist get "${SEC[@]}" --out ./plc-pki
ls ./plc-pki/trusted/certs ./plc-pki/trusted/crl ./plc-pki/issuer/certs ./plc-pki/issuer/crl
cp new-ca.der ./plc-pki/trusted/certs/ && cp new-ca.crl ./plc-pki/trusted/crl/
opcuactl gds trustlist set "${SEC[@]}" ./plc-pki --yes

# Or from single files: trusted certificates and CRLs as arguments, issuers with --issuer
opcuactl gds trustlist set "${SEC[@]}" opcuactl.der scada.der ca.pem ca.crl --issuer intermediate.pem --yes

# Only the issuer lists, the trusted ones stay
opcuactl gds trustlist set "${SEC[@]}" --masks issuers,issuer-crls --issuer intermediate.pem --issuer intermediate.crl
```

**Renew the server's certificate through your CA**: the server makes the key, the CA never sees it:

```sh
# 1. A request for a new key (the server keeps the key until the certificate comes)
opcuactl gds csr "${SEC[@]}" --type rsa --regenerate --out plc.csr

# 2. Your CA signs it (keep the ApplicationUri and host names of the request)
openssl x509 -req -in plc.csr -CA ca.pem -CAkey ca.key -CAcreateserial -days 365 \
  -copy_extensions copyall -out plc.pem

# 3. Install it; new connections use it right away, open ones keep the old one
opcuactl gds update-cert "${SEC[@]}" plc.pem --issuer ca.pem --yes

# 4. Clients now validate the new certificate against the CA
opcuactl get server -e opc.tcp://plc:4840 --security-policy Basic256Sha256 --security-mode SignAndEncrypt \
  --cert ./pki/opcuactl.pem --key ./pki/opcuactl.key --trusted-cert ca.pem
```

A certificate made elsewhere comes with its key: `gds update-cert "${SEC[@]}" plc.der --private-key plc.key` (sent as PEM over the encrypted channel). `--type` (rsa, ecc-p256, ecc-p384, ed25519, …) picks which certificate of a server with several to replace; `update-cert` takes the type of the certificate's key.

The changes are a transaction of the session that ApplyChanges completes; since every opcuactl command opens its own session, the changing commands apply themselves. `--no-apply` leaves the change pending, and `gds apply` / `gds cancel` complete it on servers that keep a transaction across sessions. A refusal says what is missing: `Bad_UserAccessDenied` is the SecurityAdmin role, `Bad_SecurityModeInsufficient` the encrypted channel, `Bad_CertificateUriInvalid` the server's ApplicationUri in the certificate, `Bad_TransactionPending` another administrator's open changes.

**Certificate groups, HTTPS and user tokens.** Besides the application certificate a server may keep an HTTPS certificate (for `opc.https` / `opc.wss`) and a trust list for X.509 user tokens. `--group https|usertoken` (or the NodeId of a group the application defines) selects them on `trustlist`, `csr`, `update-cert`, `certs`, `self-signed` and `delete-cert`:

```sh
opcuactl gds groups "${SEC[@]}"                                    # groups, trust lists, certificate types
opcuactl gds certs "${SEC[@]}" --group https --out ./plc-https     # what the server uses now
opcuactl gds csr "${SEC[@]}" --group https --type https --regenerate --out plc-https.csr
opcuactl gds update-cert "${SEC[@]}" --group https plc-https.pem --issuer ca.pem --yes
opcuactl gds trustlist add "${SEC[@]}" --group usertoken idp-ca.pem   # trust X.509 users of a CA
```

**A key as PKCS #12, a self-signed certificate, starting over:**

```sh
opcuactl gds update-cert "${SEC[@]}" plc.pem --private-key plc.pfx --pfx-password "$PFX" --yes   # sent as PFX
opcuactl gds update-cert "${SEC[@]}" plc.pem --private-key plc.key --key-format pfx --yes        # PEM key, sent as PFX
opcuactl gds self-signed "${SEC[@]}" --type ecc-p256 --subject "CN=plc/O=Plant" --dns plc --days 365 --out plc.pem --yes
opcuactl gds delete-cert "${SEC[@]}" --type ecc-p256 --yes     # refused while an endpoint uses it
opcuactl gds reset "${SEC[@]}" --yes                           # trust lists and certificates back to the defaults
opcuactl gds transaction "${SEC[@]}"                           # what the last ApplyChanges did, and what failed
```

---

## 27. Alarm Maintenance: Out of Service, Suppression, Latched Alarms, Dialogs, Audit

Part 9 1.05 gives operators more than acknowledge and shelve: alarms can be taken out of service for a repair, suppressed, silenced, reset once the cause of a latched alarm is gone, and dialogs ask the operator a question. A comment (`-m`, also `--comment`) goes with all of them (the `…2` method variants), and an audit trail records who did what.

```sh
# Pump 3 is repaired: its alarms are out of service until it is back
opcuactl alarms remove-from-service "${CONN[@]}" "ns=2;s=Pump3.Overload" -m "WO-1182: motor replacement"
opcuactl alarms list "${CONN[@]}" --state out-of-service -v
opcuactl alarms place-in-service "${CONN[@]}" "ns=2;s=Pump3.Overload" -m "WO-1182 done"

# Suppress / unsuppress, silence a horn, reset a latched alarm after the fix
opcuactl alarms suppress "${CONN[@]}" "ns=2;s=Tank.LevelAlarm" -m "tank drained"
opcuactl alarms unsuppress "${CONN[@]}" "ns=2;s=Tank.LevelAlarm"
opcuactl alarms silence "${CONN[@]}" "ns=2;s=Hall2.Horn"
opcuactl alarms reset "${CONN[@]}" "ns=2;s=Press.GuardOpen" -m "guard closed and checked"

# Shelve with a reason
opcuactl alarms shelve "${CONN[@]}" "ns=2;s=Tank.LevelAlarm" --for 8h -m "sensor on order"
opcuactl alarms shelve "${CONN[@]}" "ns=2;s=Tank.LevelAlarm" --one-shot -m "known during cleaning"

# A dialog: the prompt and the options are in alarms list --format json / alarms watch
opcuactl alarms list "${CONN[@]}" --state dialog -v
opcuactl alarms respond "${CONN[@]}" "ns=2;s=Line1.StartDialog" Yes -m "area clear"

# Branches: previous states that still need an acknowledgement
opcuactl alarms list "${CONN[@]}" --state branch
opcuactl alarms ack "${CONN[@]}" "ns=2;s=Tank.LevelAlarm" --branch "ns=1;g=6a0f..." -m "seen"

# Alarm groups an alarm belongs to; alarm metrics (AlarmMetricsType) and their reset
opcuactl alarms groups "${CONN[@]}" "ns=2;s=Tank.LevelAlarm"
opcuactl alarms metrics get "${CONN[@]}" "ns=2;s=Plant.AlarmMetrics" --format json
opcuactl alarms metrics reset "${CONN[@]}" "ns=2;s=Plant.AlarmMetrics"
```

**Audit trail.** `--audit-entry-id` puts an id (a ticket, a shift) on every request of an `alarms` command; the server reports it as ClientAuditEntryId in its audit events. `alarms audit` streams the audit events of the condition methods: who, what, with which comment, accepted or refused.

```sh
opcuactl alarms audit "${CONN[@]}" --jsonl >> alarm-audit.jsonl &
opcuactl alarms ack "${CONN[@]}" "ns=2;s=Tank.LevelAlarm" -m "checked" --audit-entry-id ticket-4711
opcuactl alarms audit "${CONN[@]}" --entry-id ticket-4711 --count 1
```

The server must audit (`Server/Auditing` true) and let the user receive audit events. A method the condition does not support exits with 6 (`Bad_MethodInvalid`, the alarm has no such optional state); `Bad_InvalidState` says the alarm is not in a state that allows it (not latched, not active, not out of service), `Bad_DialogNotActive` / `Bad_DialogResponseInvalid` that the dialog is over or the option unknown.

---

## 28. Get a Certificate from a GDS (Pull)

A Global Discovery Server (Part 12 §7.9; go-opcua `gdsserver`, UA-.NETStandard GDS) keeps a directory of applications and issues their certificates. Registering needs the **DiscoveryAdmin** role, issuing **CertificateAuthorityAdmin** (or a session of the application itself, with its current certificate), always over **SignAndEncrypt**.

```sh
GDS=(-e opc.tcp://gds:58810 --security-policy Basic256Sha256 --security-mode SignAndEncrypt
     --cert ./pki/opcuactl.pem --key ./pki/opcuactl.key --trusted-cert ./pki/gds.pem
     --auth username --username gdsadmin --password "$GDS_PASSWORD")
```

**One step for a new device**: register it (or find it by its ApplicationUri), make a key here, request the certificate, wait for an administrator's approval, and write certificate, key and the trust list in the PKI layout:

```sh
opcuactl gds pull enroll "${GDS[@]}" --uri urn:plant:hmi7 --name "HMI 7" --type client \
  --new-key ecc-p256 --cert-type ecc-p256 --cert-out ./hmi7/own/hmi7.pem --trust-out ./hmi7 --wait-timeout 1h
# hmi7.key next to the certificate (mode 0600); ./hmi7/trusted/certs, ./hmi7/trusted/crl, ...
```

Without `--new-key` or `--private-key` the GDS generates the key pair and returns it once: `--key-out` names the file (default next to `--cert-out` with `.key`).

**Step by step** (an existing CSR, a script that collects later):

```sh
APP=$(opcuactl gds pull register "${GDS[@]}" --uri urn:plant:line4 --name "Line 4" \
        --discovery-url opc.tcp://line4:4840 --capability DA -q)
REQ=$(opcuactl gds pull request "${GDS[@]}" "$APP" --csr line4.csr -q)
opcuactl gds pull finish "${GDS[@]}" "$APP" "$REQ" --cert-out line4.pem --issuers-out ca.pem \
  || echo "still waiting for approval"            # exit 6 (Bad_NothingToDo) while pending
opcuactl gds pull wait "${GDS[@]}" "$APP" "$REQ" --cert-out line4.pem --poll 30s
```

**Directory and lifecycle:**

```sh
opcuactl gds pull find "${GDS[@]}" urn:plant:line4              # ApplicationId(s) of a URI
opcuactl gds pull query "${GDS[@]}" --type server --uri "urn:plant:%"
opcuactl gds pull query-servers "${GDS[@]}" --capability DA
opcuactl gds pull update "${GDS[@]}" "$APP" --discovery-url opc.tcp://line4.plant:4840
opcuactl gds pull status "${GDS[@]}" "$APP" --type rsa          # time to renew? (exit 0 either way)
opcuactl gds pull trustlist "${GDS[@]}" "$APP" --out ./line4-pki
opcuactl gds pull revoke "${GDS[@]}" "$APP" line4.pem --yes
opcuactl gds pull check-revocation "${GDS[@]}" line4.pem || echo "revoked or unknown"   # exit 1
opcuactl gds pull unregister "${GDS[@]}" "$APP" --yes           # also revokes its certificates
```

---

## 29. Run a Test Server and Talk to It (incl. UA JSON)

`opcuactl serve` gives you a server to point the other commands at: on a laptop without a PLC, in a CI job, or to check another client. Its demo namespace (ns=1) has values that change (`Demo.Counter`, `Demo.Sine`), a writable one with an alarm (`Demo.Level`, `Demo.LevelAlarm`), a historized one (`Demo.Sine`) and a method (`Demo.Add`).

**Start it** with opc.tcp, opc.wss and opc.https, the UA JSON mappings, a user, and the certificate saved for the clients:

```sh
opcuactl serve --wss 0.0.0.0:4843 --https 0.0.0.0:4443 --uajson \
  --user operator:secret --cert-out ./pki/server.pem --key-out ./pki/server.key
# Application URI:  urn:myhost:opcuactl:serve
# Demo namespace:   ns=1 urn:otfabric:opcuactl:demo (ns=1;s=Demo.Counter, ...)
# Certificate:      rsa 041463BD0F7B1FF91FF86CBD92CDF69B4587CA1F (self-signed, generated, ./pki/server.pem)
# ...
# ENDPOINT URL             TRANSPORT         SECURITY POLICY  SECURITY MODE ...
```

**Read, write, call, subscribe** over opc.tcp:

```sh
EP=opc.tcp://localhost:4840
opcuactl browse children -e $EP -n "ns=1;s=Demo"
opcuactl monitor value -e $EP -n "ns=1;s=Demo.Counter" -n "ns=1;s=Demo.Sine" --count 10
opcuactl call method -e $EP --object "ns=1;s=Demo" --method "ns=1;s=Demo.Add" --arg 2 --arg 40 --types int32,int32

# Raise the alarm (High at 80, HighHigh at 95), acknowledge it, lower the level again
opcuactl write value -e $EP -n "ns=1;s=Demo.Level" --value 90 --type double --yes
opcuactl alarms list -e $EP
opcuactl alarms ack -e $EP "ns=1;s=Demo.LevelAlarm" -m "seen"
opcuactl write value -e $EP -n "ns=1;s=Demo.Level" --value 20 --type double --yes

# The last minute of the sine wave
opcuactl history value -e $EP -n "ns=1;s=Demo.Sine" \
  --start "$(date -u -d '-1 min' +%Y-%m-%dT%H:%M:%SZ)" --end "$(date -u +%Y-%m-%dT%H:%M:%SZ)"

# The event history of Demo: the start event and the alarm transitions, newest first
opcuactl history event -e $EP -n "ns=1;s=Demo" --end "$(date -u -d '+1 min' +%Y-%m-%dT%H:%M:%SZ)" --max-events 10
opcuactl history event -e $EP -n "ns=1;s=Demo" --start 2026-01-01T00:00:00Z --end 2100-01-01T00:00:00Z \
  --of-type ExclusiveLevelAlarmType --select Time --select ActiveState/Id --select Severity
opcuactl history update-events -e $EP -n "ns=1;s=Demo" --event '{"Message":"calibrated","Severity":100}' --yes
```

**Securely, with the user** (the client needs a certificate of its own; the server accepts any unless `--trusted-cert` / `--trust-dir` say otherwise):

```sh
opcuactl get value -e $EP -n "ns=1;s=Demo.String" \
  --security-policy Basic256Sha256 --security-mode SignAndEncrypt \
  --cert ./pki/client.pem --key ./pki/client.key --trusted-cert ./pki/server.pem \
  --auth username --username operator --password secret
```

**UA JSON** over the same listeners: wss-uajson is None only (TLS protects it), https-uajson also signs the session with Basic256Sha256. The server certificate is the TLS certificate, so it is the `--tls-ca` too:

```sh
opcuactl discover endpoints -e opc.wss://localhost:4843 --tls-ca ./pki/server.pem --transport-profile wss-uajson
opcuactl get value -e opc.wss://localhost:4843 --transport wss-uajson --tls-ca ./pki/server.pem -n "ns=1;s=Demo.Sine"
opcuactl monitor value -e opc.https://localhost:4443 --transport https-uajson --tls-ca ./pki/server.pem \
  -n "ns=1;s=Demo.Counter" --count 5
opcuactl write value -e opc.https://localhost:4443 --transport https-uajson --tls-ca ./pki/server.pem \
  -n "ns=1;s=Demo.Level" --value 85 --type double --yes --auth username --username operator --password secret
```

**In CI**: a free port, the endpoint from the JSON output, and a clean stop:

```sh
opcuactl serve --listen 127.0.0.1:0 --insecure-no-security --security-policy None --format json > serve.json &
SERVE=$!
until EP=$(jq -er '.endpoints[0].endpointUrl' serve.json 2>/dev/null); do sleep 0.1; done
opcuactl ping -e "$EP" && opcuactl get value -e "$EP" -n "ns=1;s=Demo.Int32" -q   # 42
kill -TERM $SERVE; wait $SERVE        # exit 0 after "Server stopped"
```

**Your own information model**: `--nodeset` imports NodeSet2 XML files (companion specifications, a model exported from your modelling tool); `--no-demo` leaves the demo out. Reuse the saved certificate so clients keep trusting it:

```sh
opcuactl serve --cert ./pki/server.pem --key ./pki/server.key --no-demo \
  --nodeset Opc.Ua.Di.NodeSet2.xml --nodeset machine.NodeSet2.xml
opcuactl find namespace -e opc.tcp://localhost:4840
```
