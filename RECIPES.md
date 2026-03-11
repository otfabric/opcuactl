# Common Recipes

End-to-end workflows for typical OPC UA operator tasks using `opcuactl`.

All examples use a shell array for connection flags:

```sh
CONN=(-e opc.tcp://your-server:4840 --auth anonymous --security-mode None --security-policy None)
```

Adjust `--auth`, `--security-mode`, and `--security-policy` to match your server's configuration. Use `discover endpoints --best` to find the most secure option.

---

## 1. First Contact with an Unknown Server

Discover what's running, check connectivity, and understand the address space.

```sh
# Step 1: Discover servers on the network
opcuactl discover servers "${CONN[@]}"

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

# Get a full node summary (data type, access level, type definition)
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

## 4. Monitor a Value in Real Time

Subscribe to live data changes and stream output.

```sh
# Monitor until Ctrl+C
opcuactl monitor value "${CONN[@]}" -n "ns=2;s=Temperature"

# Monitor for 60 seconds, sampling every 500ms
opcuactl monitor value "${CONN[@]}" -n "ns=2;s=Temperature" \
  --duration 60s --sampling 500ms

# Collect exactly 10 samples as JSONL (for piping to jq, etc.)
opcuactl monitor value "${CONN[@]}" -n "ns=2;s=Temperature" \
  --count 10 --jsonl

# Fast sampling with batched delivery
opcuactl monitor value "${CONN[@]}" -n "ns=2;s=Temperature" \
  --interval 1s --sampling 100ms --queue-size 10
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

# Verify the write
opcuactl get value "${CONN[@]}" -n "ns=2;s=Setpoint"
```

---

## 6. Export for Reporting

Bulk-export node metadata or value snapshots for offline analysis.

```sh
# Export node metadata tree as JSON
opcuactl export nodes "${CONN[@]}" --depth 3 -f json --output nodes.json

# Export current values as CSV
opcuactl export values "${CONN[@]}" -n "ns=2;s=Temperature" -n "ns=2;s=Pressure" \
  -f csv --output snapshot.csv

# Export values from a subtree (auto-discover variables)
opcuactl export values "${CONN[@]}" --start-node "ns=2;i=1" --depth 2 \
  -f csv --output subtree-values.csv

# Export historical data (if supported by server)
opcuactl export values "${CONN[@]}" --start-node "ns=2;i=1" --depth 2 \
  --history --start 2026-03-10T00:00:00Z --end 2026-03-11T00:00:00Z \
  -f csv --output history.csv
```

---

## 7. Troubleshoot a Connection

Diagnose connectivity and security issues step by step.

```sh
# Test basic TCP + OPC UA connectivity
opcuactl ping "${CONN[@]}" -v

# List endpoints with full detail (cert thumbprints, URL mismatches)
opcuactl discover endpoints "${CONN[@]}" -v

# Check if the server advertises a different hostname than the one you connected to
# (look for "URL mismatch" warnings in verbose output)
opcuactl discover endpoints "${CONN[@]}" -v 2>&1 | grep -i mismatch

# Verify authentication works
opcuactl get server "${CONN[@]}"
```

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
