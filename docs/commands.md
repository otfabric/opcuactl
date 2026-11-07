# Command reference

<!-- Generated from the command tree (`TestDocsyncManifest`) and each command's `--help`. Regenerate it when a command or flag changes. -->

Every opcuactl command and its flags, as `opcuactl <command> --help` prints them. The [README](../README.md) explains the concepts and walks through the workflows; [RECIPES.md](../RECIPES.md) has longer examples. Exit codes are in [exitcodes.md](exitcodes.md), the JSON output in [json-output.md](json-output.md).

## Global flags

Every command accepts these flags of the root command.

| Flag | Value | Meaning |
|---|---|---|
| `--auth` | `string` | Authentication mode (anonymous\|username\|certificate\|issued) (default "anonymous") |
| `--auth-policy-id` | `string` | User token PolicyId (usually set automatically with security auto) |
| `--cert` | `string` | Client certificate file (PEM/DER; RSA, ECDSA incl. Brainpool, Ed25519 or Ed448) for secure channel and/or certificate auth |
| `--endpoint`, `-e` | `string` | OPC UA server endpoint (opc.tcp://host:port, opc.wss://host:port or opc.https://host:port) |
| `--format`, `-f` | `string` | Output format (table\|json\|yaml\|csv\|jsonl) (default "table") |
| `--insecure-no-security` |  | Allow no message security (policy and mode None): anyone on the network can read and change the traffic, passwords included |
| `--insecure-skip-verify` |  | Skip server certificate verification (INSECURE) |
| `--json-content-type` | `string` | https-uajson: Content-Type of the requests (standard: application/json \| legacy: application/opcua+uajson, for UA-.NETStandard) |
| `--key` | `string` | Client private key file (PEM/DER) |
| `--namespace` | `string` | Default namespace (index or URI) for node ID shorthand |
| `--password` | `string` | Password for --auth username |
| `--pretty` |  | Pretty-print structured output |
| `--quiet`, `-q` |  | Quiet mode: emit only the primary value (for scripting) |
| `--return-diagnostics` | `string` | Ask the server for DiagnosticInfos and print them on failures (none\|service\|operation\|all) |
| `--reverse-listen` | `string` | Reverse connect: listen on this address (host:port) and wait for the server to connect |
| `--reverse-server-uri` | `string` | Reverse connect: only accept the server announcing this ApplicationUri |
| `--security-mode` | `string` | Security mode (None\|Sign\|SignAndEncrypt\|auto) (default "None") |
| `--security-policy` | `string` | Security policy (None\|Basic256Sha256\|Aes128Sha256RsaOaep\|Aes256Sha256RsaPss\|ECC_nistP256\|ECC_nistP384\|ECC_nistP256_AesGcm\|ECC_nistP256_ChaChaPoly\|ECC_curve25519\|ECC_brainpoolP256r1\|ECC_brainpoolP384r1\|ECC_curve448\|Basic128Rsa15\|Basic256\|auto) (default "None") |
| `--server-cert` | `string` | Server certificate file (overrides endpoint cert when set) |
| `--timeout` | `duration` | Connection/operation timeout (default 10s) |
| `--tls-ca` | `string` | opc.wss/opc.https: PEM file with the roots for the server's TLS certificate (default: system roots) |
| `--tls-cert` | `string` | opc.wss/opc.https: client TLS certificate (PEM) for mutual TLS |
| `--tls-insecure-skip-verify` |  | opc.wss/opc.https: do not verify the server's TLS certificate (INSECURE, development only) |
| `--tls-key` | `string` | opc.wss/opc.https: private key (PEM) of --tls-cert |
| `--token` | `string` | Issued token (raw or base64) for --auth issued |
| `--token-file` | `string` | Issued token file for --auth issued |
| `--transport` | `string` | Transport mapping (tcp\|wss\|https\|wss-uajson\|https-uajson; default: UA Binary over the scheme of --endpoint) |
| `--trusted-cert` | `stringArray` | Trusted CA or self-signed server cert (repeatable; PEM/DER) |
| `--username` | `string` | Username for --auth username |
| `--verbose`, `-v` |  | Verbose output |

See [json-output.md](json-output.md) for the structured formats; `OPCUACTL_FORMAT` sets the default of `--format`.

## opcuactl alarms

List, acknowledge, confirm, shelve, suppress and silence alarms and conditions.

Alias: `conditions`.

```text
opcuactl alarms [command]
```

Subcommands: [`ack`](#opcuactl-alarms-ack), [`audit`](#opcuactl-alarms-audit), [`comment`](#opcuactl-alarms-comment), [`confirm`](#opcuactl-alarms-confirm), [`disable`](#opcuactl-alarms-disable), [`enable`](#opcuactl-alarms-enable), [`groups`](#opcuactl-alarms-groups), [`list`](#opcuactl-alarms-list), [`metrics`](#opcuactl-alarms-metrics), [`place-in-service`](#opcuactl-alarms-place-in-service), [`remove-from-service`](#opcuactl-alarms-remove-from-service), [`reset`](#opcuactl-alarms-reset), [`respond`](#opcuactl-alarms-respond), [`shelve`](#opcuactl-alarms-shelve), [`silence`](#opcuactl-alarms-silence), [`suppress`](#opcuactl-alarms-suppress), [`unshelve`](#opcuactl-alarms-unshelve), [`unsuppress`](#opcuactl-alarms-unsuppress), [`watch`](#opcuactl-alarms-watch).

| Flag | Value | Meaning |
|---|---|---|
| `--audit-entry-id` | `string` | AuditEntryId of the requests (reported as ClientAuditEntryId in audit events) |

## opcuactl alarms ack

Acknowledge a condition.

```text
opcuactl alarms ack <conditionId> [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--branch` | `string` | BranchId of a condition branch: take the latest event of the branch |
| `--event-id` | `string` | EventId of the event (base64 or 0x hex), or latest (default "latest") |
| `--interval` | `duration` | Publishing interval of the subscription (default 100ms) |
| `--message`, `-m` | `string` | Comment |
| `--node`, `-n` | `string` | Event notifier to take the condition events from (default "i=2253") |
| `--queue-size` | `uint32` | Queue size of the event monitored item (the retained conditions of a refresh must fit) (default 1000) |

## opcuactl alarms audit

Stream the audit events of the condition methods.

```text
opcuactl alarms audit [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--count` | `int` | Stop after this many audit events (0 = unlimited) |
| `--duration` | `duration` | Stop after this duration (0 = run until Ctrl+C) |
| `--entry-id` | `string` | Only the events of requests with this AuditEntryId (ClientAuditEntryId) |
| `--interval` | `duration` | Publishing interval of the subscription (default 100ms) |
| `--jsonl` |  | Output newline-delimited JSON (JSONL) |
| `--node`, `-n` | `string` | Event notifier to take the condition events from (default "i=2253") |
| `--queue-size` | `uint32` | Queue size of the event monitored item (the retained conditions of a refresh must fit) (default 1000) |

## opcuactl alarms comment

Add a comment to a condition.

```text
opcuactl alarms comment <conditionId> [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--branch` | `string` | BranchId of a condition branch: take the latest event of the branch |
| `--event-id` | `string` | EventId of the event (base64 or 0x hex), or latest (default "latest") |
| `--interval` | `duration` | Publishing interval of the subscription (default 100ms) |
| `--message`, `-m` | `string` | Comment |
| `--node`, `-n` | `string` | Event notifier to take the condition events from (default "i=2253") |
| `--queue-size` | `uint32` | Queue size of the event monitored item (the retained conditions of a refresh must fit) (default 1000) |

## opcuactl alarms confirm

Confirm a condition.

```text
opcuactl alarms confirm <conditionId> [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--branch` | `string` | BranchId of a condition branch: take the latest event of the branch |
| `--event-id` | `string` | EventId of the event (base64 or 0x hex), or latest (default "latest") |
| `--interval` | `duration` | Publishing interval of the subscription (default 100ms) |
| `--message`, `-m` | `string` | Comment |
| `--node`, `-n` | `string` | Event notifier to take the condition events from (default "i=2253") |
| `--queue-size` | `uint32` | Queue size of the event monitored item (the retained conditions of a refresh must fit) (default 1000) |

## opcuactl alarms disable

Disable a condition.

```text
opcuactl alarms disable <conditionId> [flags]
```

No flags of its own; the global flags apply.

## opcuactl alarms enable

Enable a condition.

```text
opcuactl alarms enable <conditionId> [flags]
```

No flags of its own; the global flags apply.

## opcuactl alarms groups

List the alarm groups an alarm is a member of.

Alias: `memberships`.

```text
opcuactl alarms groups <conditionId> [flags]
```

No flags of its own; the global flags apply.

## opcuactl alarms list

List the retained conditions (ConditionRefresh).

```text
opcuactl alarms list [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--interval` | `duration` | Publishing interval of the subscription (default 100ms) |
| `--node`, `-n` | `string` | Event notifier to take the condition events from (default "i=2253") |
| `--queue-size` | `uint32` | Queue size of the event monitored item (the retained conditions of a refresh must fit) (default 1000) |
| `--severity-min` | `uint16` | Only conditions with at least this Severity |
| `--state` | `stringArray` | Only conditions in this state (repeatable: active\|inactive\|unacked\|acked\|unconfirmed\|confirmed\|shelved\|suppressed\|out-of-service\|silenced\|latched\|dialog\|branch) |

## opcuactl alarms metrics

Read or reset alarm metrics (AlarmMetricsType).

```text
opcuactl alarms metrics [command]
```

Subcommands: [`get`](#opcuactl-alarms-metrics-get), [`reset`](#opcuactl-alarms-metrics-reset).

## opcuactl alarms metrics get

Read an AlarmMetrics object.

Alias: `read`.

```text
opcuactl alarms metrics get <metricsNodeId> [flags]
```

No flags of its own; the global flags apply.

## opcuactl alarms metrics reset

Reset an AlarmMetrics object.

```text
opcuactl alarms metrics reset <metricsNodeId> [flags]
```

No flags of its own; the global flags apply.

## opcuactl alarms place-in-service

Put an alarm back in service.

Alias: `in-service`.

```text
opcuactl alarms place-in-service <conditionId> [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--message`, `-m` | `string` | Comment (the *2 variant of the method) |

## opcuactl alarms remove-from-service

Alias: `out-of-service`.

```text
opcuactl alarms remove-from-service <conditionId> [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--message`, `-m` | `string` | Comment (the *2 variant of the method) |

## opcuactl alarms reset

Reset a latched alarm.

```text
opcuactl alarms reset <conditionId> [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--message`, `-m` | `string` | Comment (the *2 variant of the method) |

## opcuactl alarms respond

Respond to a dialog.

```text
opcuactl alarms respond <conditionId> <option> [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--message`, `-m` | `string` | Comment (Respond2) |

## opcuactl alarms shelve

Shelve an alarm.

```text
opcuactl alarms shelve <conditionId> (--for <duration> | --one-shot) [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--for` | `duration` | Shelve for this long (TimedShelve) |
| `--message`, `-m` | `string` | Comment (TimedShelve2 / OneShotShelve2) |
| `--one-shot` |  | Shelve until the alarm goes inactive (OneShotShelve) |

## opcuactl alarms silence

Silence an alarm.

```text
opcuactl alarms silence <conditionId> [flags]
```

No flags of its own; the global flags apply.

## opcuactl alarms suppress

Suppress an alarm.

```text
opcuactl alarms suppress <conditionId> [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--message`, `-m` | `string` | Comment (the *2 variant of the method) |

## opcuactl alarms unshelve

Unshelve an alarm.

```text
opcuactl alarms unshelve <conditionId> [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--message`, `-m` | `string` | Comment (Unshelve2) |

## opcuactl alarms unsuppress

Unsuppress an alarm.

```text
opcuactl alarms unsuppress <conditionId> [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--message`, `-m` | `string` | Comment (the *2 variant of the method) |

## opcuactl alarms watch

Stream condition events (refresh, then live).

```text
opcuactl alarms watch [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--count` | `int` | Stop after this many condition events (0 = unlimited) |
| `--duration` | `duration` | Stop after this duration (0 = run until Ctrl+C) |
| `--interval` | `duration` | Publishing interval of the subscription (default 100ms) |
| `--jsonl` |  | Output newline-delimited JSON (JSONL) |
| `--no-refresh` |  | Do not call ConditionRefresh: only live events |
| `--node`, `-n` | `string` | Event notifier to take the condition events from (default "i=2253") |
| `--queue-size` | `uint32` | Queue size of the event monitored item (the retained conditions of a refresh must fit) (default 1000) |

## opcuactl browse

Browse the OPC UA address space.

```text
opcuactl browse [command]
```

Subcommands: [`children`](#opcuactl-browse-children), [`path`](#opcuactl-browse-path), [`refs`](#opcuactl-browse-refs), [`tree`](#opcuactl-browse-tree).

## opcuactl browse children

List child nodes.

```text
opcuactl browse children [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--node`, `-n` | `string` | Start node ID (default: Objects folder ns=0;i=85) |
| `--node-class` | `string` | Filter by node class (all\|object\|variable\|method\|objecttype\|variabletype\|referencetype\|datatype\|view) (default "all") |

## opcuactl browse path

Resolve a browse path to a node ID.

```text
opcuactl browse path [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--namespace` | `uint16` | Namespace index for path segments |
| `--path` | `string` | Dot-separated browse path (required) |
| `--start-node` | `string` | Custom start node (default: Objects folder) |

## opcuactl browse refs

Inspect node references.

```text
opcuactl browse refs [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--direction` | `string` | Browse direction (forward\|inverse\|both) (default "both") |
| `--include-subtypes` |  | Include reference subtypes (default true) |
| `--node`, `-n` | `string` | Node ID (default: Objects folder ns=0;i=85) |
| `--node-class` | `string` | Filter by node class (default "all") |
| `--refs` | `string` | Reference type ID |

## opcuactl browse tree

Recursively browse the address space.

```text
opcuactl browse tree [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--depth` | `int` | Maximum tree depth (default 5) |
| `--node`, `-n` | `string` | Start node ID (default: Objects folder ns=0;i=85) |
| `--node-class` | `string` | Filter by node class (default "all") |

## opcuactl call

Invoke OPC UA methods.

```text
opcuactl call [command]
```

Subcommands: [`method`](#opcuactl-call-method).

## opcuactl call method

Call an OPC UA method.

```text
opcuactl call method [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--arg` | `stringArray` | Method argument value (repeatable) |
| `--arg-file` | `string` | Read arguments from a JSON file containing an array |
| `--arg-json` | `string` | Arguments as a JSON array (e.g. '[5, "hello"]'), typed by the InputArguments of the method |
| `--introspect` |  | Show method argument signatures without calling |
| `--method` | `string` | Method node ID (required) |
| `--object` | `string` | Object node ID (required) |
| `--types` | `string` | Comma-separated type names for argument coercion |

## opcuactl completion

Generate shell completion scripts.

```text
opcuactl completion [bash|zsh|fish|powershell]
```

No flags of its own; the global flags apply.

## opcuactl diagnostics

Alias: `diag`.

```text
opcuactl diagnostics [command]
```

Subcommands: [`disable`](#opcuactl-diagnostics-disable), [`enable`](#opcuactl-diagnostics-enable), [`sampling`](#opcuactl-diagnostics-sampling), [`sessions`](#opcuactl-diagnostics-sessions), [`subscriptions`](#opcuactl-diagnostics-subscriptions), [`summary`](#opcuactl-diagnostics-summary).

## opcuactl diagnostics disable

Stop the collection of server diagnostics.

```text
opcuactl diagnostics disable [flags]
```

No flags of its own; the global flags apply.

## opcuactl diagnostics enable

Start the collection of server diagnostics (resets the counters).

```text
opcuactl diagnostics enable [flags]
```

No flags of its own; the global flags apply.

## opcuactl diagnostics sampling

List the sampling intervals in use (SamplingIntervalDiagnosticsArray).

```text
opcuactl diagnostics sampling [flags]
```

No flags of its own; the global flags apply.

## opcuactl diagnostics sessions

List the sessions (SessionDiagnosticsArray).

```text
opcuactl diagnostics sessions [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--security` |  | Show the session security diagnostics |
| `--session` | `string` | SessionId of one session (or self) to show in detail |

## opcuactl diagnostics subscriptions

```text
opcuactl diagnostics subscriptions [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--session` | `string` | Only the subscriptions of this SessionId (or self) |

## opcuactl diagnostics summary

Show the ServerDiagnosticsSummary.

```text
opcuactl diagnostics summary [flags]
```

No flags of its own; the global flags apply.

## opcuactl discover

Discover OPC UA servers and endpoints.

```text
opcuactl discover [command]
```

Subcommands: [`endpoints`](#opcuactl-discover-endpoints), [`lds`](#opcuactl-discover-lds), [`servers`](#opcuactl-discover-servers).

## opcuactl discover endpoints

List server endpoints.

```text
opcuactl discover endpoints [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--best` |  | Select the best (most secure) endpoint |
| `--mode` | `string` | Filter by security mode |
| `--policy` | `string` | Filter by security policy |
| `--transport-profile` | `string` | Filter by transport (tcp\|wss\|https\|wss-uajson\|https-uajson or a transport profile URI) |

## opcuactl discover lds

Query a Local Discovery Server.

```text
opcuactl discover lds [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--host` | `string` | Host name, IP address or opc.tcp URL of the discovery server (port 4840 by default) |

## opcuactl discover servers

Discover OPC UA servers.

```text
opcuactl discover servers [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--capability` | `stringArray` | With --on-network: only servers with this capability (repeatable, e.g. DA, HD, LDS) |
| `--locale` | `string` | Preferred locale |
| `--max-records` | `uint32` | With --on-network: maximum number of records (0 = no limit) |
| `--on-network` |  | Use FindServersOnNetwork instead of FindServers |
| `--server-uri` | `string` | Filter by server URI |
| `--start-record` | `uint32` | With --on-network: first record id to return |

## opcuactl export

Export data from the server.

```text
opcuactl export [command]
```

Subcommands: [`nodes`](#opcuactl-export-nodes), [`values`](#opcuactl-export-values).

## opcuactl export nodes

Export node metadata from the address space.

```text
opcuactl export nodes [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--depth` | `int` | Maximum tree depth (default 5) |
| `--include-attrs` |  | Include additional attributes (WriteMask, ValueRank, etc.) |
| `--node`, `-n` | `string` | Start node ID (default: Objects folder ns=0;i=85) |
| `--node-class` | `string` | Filter by node class (default "all") |
| `--output` | `string` | Write output to file instead of stdout |

## opcuactl export values

Export current or historical values.

```text
opcuactl export values [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--aggregate` | `string` | Aggregate for processed history (average\|min\|max\|count) (default "average") |
| `--at` | `stringArray` | Timestamp for attime mode (repeatable, RFC3339) |
| `--depth` | `int` | Maximum depth for subtree discovery (default 5) |
| `--end` | `string` | End time in RFC3339 format (required with --history except attime) |
| `--history` |  | Export historical values instead of current snapshot |
| `--interval` | `duration` | Aggregate interval for processed history |
| `--mode` | `string` | History mode with --history (raw\|modified\|processed\|attime) (default "raw") |
| `--node`, `-n` | `stringArray` | Node ID to export (repeatable) |
| `--nodes-file` | `string` | Read node IDs from a text file (one per line) |
| `--output` | `string` | Write output to file instead of stdout |
| `--start` | `string` | Start time in RFC3339 format (required with --history except attime) |
| `--start-node` | `string` | Start node for subtree discovery (exports all variable children) |

## opcuactl find

Search nodes and namespaces.

```text
opcuactl find [command]
```

Subcommands: [`namespace`](#opcuactl-find-namespace), [`node`](#opcuactl-find-node), [`type`](#opcuactl-find-type).

## opcuactl find namespace

List or search namespaces.

```text
opcuactl find namespace [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--filter` | `string` | Filter namespaces by URI substring |

## opcuactl find node

Search nodes by name or pattern.

```text
opcuactl find node [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--class` | `string` | Filter by node class (default "all") |
| `--depth` | `int` | Maximum search depth (0 = unlimited) |
| `--display-name` | `string` | Match display name (exact, contains, or regex with --regex) |
| `--limit` | `int` | Maximum number of results (0 = unlimited) |
| `--name` | `string` | Match browse name (exact, contains, or regex with --regex) |
| `--regex` |  | Treat --name and --display-name as regex patterns |
| `--start-node` | `string` | Start node ID (default: Objects folder) |

## opcuactl find type

Find nodes by type definition or data type.

```text
opcuactl find type [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--class` | `string` | Filter by node class (default "all") |
| `--data-type` | `string` | Match data type by NodeID or name |
| `--depth` | `int` | Maximum search depth (0 = unlimited) |
| `--limit` | `int` | Maximum number of results (0 = unlimited) |
| `--start-node` | `string` | Start node ID (default: Objects folder) |
| `--type-definition` | `string` | Match type definition by NodeID or name |

## opcuactl gds

Manage the certificates of a server (push model, Part 12 §7).

Alias: `certmgmt`.

```text
opcuactl gds [command]
```

Subcommands: [`apply`](#opcuactl-gds-apply), [`cancel`](#opcuactl-gds-cancel), [`certs`](#opcuactl-gds-certs), [`csr`](#opcuactl-gds-csr), [`delete-cert`](#opcuactl-gds-delete-cert), [`groups`](#opcuactl-gds-groups), [`props`](#opcuactl-gds-props), [`pull`](#opcuactl-gds-pull), [`rejected`](#opcuactl-gds-rejected), [`reset`](#opcuactl-gds-reset), [`self-signed`](#opcuactl-gds-self-signed), [`transaction`](#opcuactl-gds-transaction), [`trustlist`](#opcuactl-gds-trustlist), [`update-cert`](#opcuactl-gds-update-cert).

## opcuactl gds apply

Apply the pending certificate changes (ApplyChanges).

```text
opcuactl gds apply [flags]
```

No flags of its own; the global flags apply.

## opcuactl gds cancel

Drop the pending certificate changes (CancelChanges).

```text
opcuactl gds cancel [flags]
```

No flags of its own; the global flags apply.

## opcuactl gds certs

Show the certificates the server uses.

Alias: `certificates`, `get-certificates`.

```text
opcuactl gds certs [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--group` | `string` | Certificate group whose certificates to show: default\|https\|usertoken or a NodeId (default: the DefaultApplicationGroup) |
| `--out` | `string` | Write the certificates as DER files into this directory |

## opcuactl gds csr

Ask the server for a certificate signing request.

```text
opcuactl gds csr [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--group` | `string` | Certificate group of the certificate: default\|https\|usertoken or a NodeId (default: the DefaultApplicationGroup) |
| `--out` | `string` | Write the request to this file (PEM; DER for .der) |
| `--regenerate` |  | Let the server generate a new private key |
| `--subject` | `string` | Subject of the request (CN=App/O=Company); default the current subject |
| `--type` | `string` | Certificate type (rsa\|rsa-min\|ecc-p256\|ecc-p384\|ecc-brainpool-p256\|ecc-brainpool-p384\|ed25519\|ed448 or a NodeId) |

## opcuactl gds delete-cert

Alias: `delete-certificate`.

```text
opcuactl gds delete-cert [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--group` | `string` | Certificate group of the certificate: default\|https\|usertoken or a NodeId (default: the DefaultApplicationGroup) |
| `--no-apply` |  | Do not call ApplyChanges |
| `--type` | `string` | Certificate type to delete (required) |
| `--yes` |  | Skip the confirmation prompt |

## opcuactl gds groups

List the certificate groups of the server.

Alias: `certificate-groups`.

```text
opcuactl gds groups [flags]
```

No flags of its own; the global flags apply.

## opcuactl gds props

Show the properties of the ServerConfiguration object.

Alias: `properties`, `info`.

```text
opcuactl gds props [flags]
```

No flags of its own; the global flags apply.

## opcuactl gds pull

Register applications and obtain certificates from a GDS (pull model, Part 12 §7.9).

```text
opcuactl gds pull [command]
```

Subcommands: [`certs`](#opcuactl-gds-pull-certs), [`check-revocation`](#opcuactl-gds-pull-check-revocation), [`enroll`](#opcuactl-gds-pull-enroll), [`find`](#opcuactl-gds-pull-find), [`finish`](#opcuactl-gds-pull-finish), [`get`](#opcuactl-gds-pull-get), [`groups`](#opcuactl-gds-pull-groups), [`query`](#opcuactl-gds-pull-query), [`query-servers`](#opcuactl-gds-pull-query-servers), [`register`](#opcuactl-gds-pull-register), [`request`](#opcuactl-gds-pull-request), [`revoke`](#opcuactl-gds-pull-revoke), [`status`](#opcuactl-gds-pull-status), [`trustlist`](#opcuactl-gds-pull-trustlist), [`unregister`](#opcuactl-gds-pull-unregister), [`update`](#opcuactl-gds-pull-update), [`wait`](#opcuactl-gds-pull-wait).

## opcuactl gds pull certs

List the certificates the GDS issued to an application.

Alias: `certificates`.

```text
opcuactl gds pull certs <applicationId> [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--group` | `string` | Certificate group of the GDS (NodeId; default all groups) |
| `--out` | `string` | Write the certificates as DER files into this directory |

## opcuactl gds pull check-revocation

Alias: `check`.

```text
opcuactl gds pull check-revocation <cert> [flags]
```

No flags of its own; the global flags apply.

## opcuactl gds pull enroll

Obtain a certificate and trust list from a GDS in one step.

```text
opcuactl gds pull enroll [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--application-id` | `string` | ApplicationId of a registered application (instead of finding it by --uri) |
| `--capability` | `stringArray` | Server capability, e.g. DA, HD, AC (repeatable) |
| `--cert-out` | `string` | Write the certificate to this file (PEM; DER for .der) |
| `--cert-type` | `string` | Certificate type (rsa\|ecc-p256\|...\|ed448 or a NodeId; default the first type of the group) |
| `--discovery-url` | `stringArray` | Discovery URL of a server (repeatable) |
| `--dns` | `stringArray` | Host name or IP address of the certificate (repeatable; default those of the discovery URLs) |
| `--group` | `string` | Certificate group of the GDS (NodeId; default its DefaultApplicationGroup) |
| `--issuers-out` | `string` | Write the issuer (CA) certificates to this file (PEM) |
| `--key-out` | `string` | Write the private key the GDS generated to this file (PEM, mode 0600) |
| `--name` | `stringArray` | ApplicationName ("text" or "locale:text"; repeatable) |
| `--new-key` | `string` | Generate the private key here: rsa\|ecc-p256\|ecc-p384\|ed25519 |
| `--poll` | `duration` | Interval of the FinishRequest calls while the request waits for approval (default 5s) |
| `--private-key` | `string` | Private key to request the certificate for (default: the GDS generates one) |
| `--product-uri` | `string` | ProductUri (default: the ApplicationUri) |
| `--subject` | `string` | Subject (CN=App/O=Company; default chosen by the GDS) |
| `--trust-out` | `string` | Write the trust list into this directory (trusted/certs, trusted/crl, issuer/certs, issuer/crl) |
| `--type` | `string` | Application type: server\|client\|clientandserver\|discoveryserver (default server) |
| `--uri` | `string` | ApplicationUri |
| `--wait-timeout` | `duration` | How long enrolling may take (0: forever) (default 10m0s) |

## opcuactl gds pull find

Find the registrations of an application by its ApplicationUri.

```text
opcuactl gds pull find <applicationUri> [flags]
```

No flags of its own; the global flags apply.

## opcuactl gds pull finish

Collect the certificate of a request (FinishRequest).

```text
opcuactl gds pull finish <applicationId> <requestId> [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--cert-out` | `string` | Write the certificate to this file (PEM; DER for .der) |
| `--issuers-out` | `string` | Write the issuer (CA) certificates to this file (PEM) |
| `--key-out` | `string` | Write the private key the GDS generated to this file (PEM, mode 0600) |

## opcuactl gds pull get

Show the record of a registered application.

```text
opcuactl gds pull get <applicationId> [flags]
```

No flags of its own; the global flags apply.

## opcuactl gds pull groups

List the certificate groups of an application.

```text
opcuactl gds pull groups <applicationId> [flags]
```

No flags of its own; the global flags apply.

## opcuactl gds pull query

Query the registered applications.

Alias: `query-applications`.

```text
opcuactl gds pull query [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--capability` | `stringArray` | Server capability the application must have (repeatable) |
| `--max` | `uint32` | Maximum number of records (0: no limit) |
| `--name` | `string` | ApplicationName pattern |
| `--product-uri` | `string` | ProductUri pattern |
| `--start` | `uint32` | First record (the NextRecordId of the previous page) |
| `--type` | `string` | Application type: server\|client (clientandserver matches both) |
| `--uri` | `string` | ApplicationUri pattern |

## opcuactl gds pull query-servers

Query the registered servers.

```text
opcuactl gds pull query-servers [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--capability` | `stringArray` | Server capability the server must have (repeatable) |
| `--max` | `uint32` | Maximum number of records (0: no limit) |
| `--name` | `string` | ApplicationName pattern |
| `--product-uri` | `string` | ProductUri pattern |
| `--start` | `uint32` | First record id |
| `--uri` | `string` | ApplicationUri pattern |

## opcuactl gds pull register

Register an application.

```text
opcuactl gds pull register [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--capability` | `stringArray` | Server capability, e.g. DA, HD, AC (repeatable) |
| `--discovery-url` | `stringArray` | Discovery URL of a server (repeatable) |
| `--name` | `stringArray` | ApplicationName ("text" or "locale:text"; repeatable) |
| `--product-uri` | `string` | ProductUri (default: the ApplicationUri) |
| `--type` | `string` | Application type: server\|client\|clientandserver\|discoveryserver (default server) |
| `--uri` | `string` | ApplicationUri |

## opcuactl gds pull request

Request a certificate for an application.

```text
opcuactl gds pull request <applicationId> [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--csr` | `string` | PKCS #10 certificate request of the application (PEM or DER) |
| `--dns` | `stringArray` | Host name or IP address of the certificate (repeatable; default those of the discovery URLs) |
| `--group` | `string` | Certificate group of the GDS (NodeId; default its DefaultApplicationGroup) |
| `--key-password` | `string` | Password that encrypts the private key the GDS generates |
| `--private-key` | `string` | Private key to make the certificate request from |
| `--subject` | `string` | Subject (CN=App/O=Company; default chosen by the GDS) |
| `--type` | `string` | Certificate type (rsa\|ecc-p256\|...\|ed448 or a NodeId; default the first type of the group) |

## opcuactl gds pull revoke

Revoke a certificate the GDS issued.

```text
opcuactl gds pull revoke <applicationId> <cert> [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--yes` |  | Skip the confirmation prompt |

## opcuactl gds pull status

Ask whether an application should renew its certificate.

```text
opcuactl gds pull status <applicationId> [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--group` | `string` | Certificate group of the GDS (NodeId; default its DefaultApplicationGroup) |
| `--type` | `string` | Certificate type (default any type of the group) |

## opcuactl gds pull trustlist

Read the trust list the GDS keeps for an application.

```text
opcuactl gds pull trustlist <applicationId> [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--group` | `string` | Certificate group of the GDS (NodeId; default its DefaultApplicationGroup) |
| `--out` | `string` | Write the entries as DER files into this directory (trusted/certs, trusted/crl, issuer/certs, issuer/crl) |

## opcuactl gds pull unregister

Unregister an application (and revoke its certificates).

```text
opcuactl gds pull unregister <applicationId> [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--yes` |  | Skip the confirmation prompt |

## opcuactl gds pull update

Change the record of a registered application.

```text
opcuactl gds pull update <applicationId> [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--capability` | `stringArray` | Server capability, e.g. DA, HD, AC (repeatable) |
| `--discovery-url` | `stringArray` | Discovery URL of a server (repeatable) |
| `--name` | `stringArray` | ApplicationName ("text" or "locale:text"; repeatable) |
| `--product-uri` | `string` | ProductUri (default: the ApplicationUri) |
| `--type` | `string` | Application type: server\|client\|clientandserver\|discoveryserver (default server) |
| `--uri` | `string` | ApplicationUri |

## opcuactl gds pull wait

Wait for the certificate of a request and collect it.

```text
opcuactl gds pull wait <applicationId> <requestId> [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--cert-out` | `string` | Write the certificate to this file (PEM; DER for .der) |
| `--issuers-out` | `string` | Write the issuer (CA) certificates to this file (PEM) |
| `--key-out` | `string` | Write the private key the GDS generated to this file (PEM, mode 0600) |
| `--poll` | `duration` | Interval of the FinishRequest calls (default 5s) |
| `--wait-timeout` | `duration` | How long to wait for the certificate (0: forever) (default 10m0s) |

## opcuactl gds rejected

List the certificates the server rejected.

```text
opcuactl gds rejected [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--out` | `string` | Write the rejected certificates as DER files into this directory |

## opcuactl gds reset

Reset the trust lists and certificates to the server defaults.

Alias: `reset-to-defaults`.

```text
opcuactl gds reset [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--yes` |  | Skip the confirmation prompt |

## opcuactl gds self-signed

Alias: `create-self-signed`.

```text
opcuactl gds self-signed [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--days` | `uint16` | Validity in days (0: the server's default) |
| `--dns` | `stringArray` | DNS name of the certificate (repeatable) |
| `--group` | `string` | Certificate group of the certificate: default\|https\|usertoken or a NodeId (default: the DefaultApplicationGroup) |
| `--ip` | `stringArray` | IP address of the certificate (repeatable) |
| `--key-size` | `uint16` | Key size in bits (0: the server's default) |
| `--no-apply` |  | Do not call ApplyChanges |
| `--out` | `string` | Write the new certificate to this file (PEM; DER for .der) |
| `--subject` | `string` | Subject (CN=App/O=Company); default the current subject |
| `--type` | `string` | Certificate type (rsa\|rsa-min\|ecc-p256\|...\|https or a NodeId) |
| `--yes` |  | Skip the confirmation prompt |

## opcuactl gds transaction

Alias: `transaction-diagnostics`, `tx`.

```text
opcuactl gds transaction [flags]
```

No flags of its own; the global flags apply.

## opcuactl gds trustlist

Read and change the trust list of a certificate group.

Alias: `trust`.

```text
opcuactl gds trustlist [command]
```

Subcommands: [`add`](#opcuactl-gds-trustlist-add), [`get`](#opcuactl-gds-trustlist-get), [`remove`](#opcuactl-gds-trustlist-remove), [`set`](#opcuactl-gds-trustlist-set).

## opcuactl gds trustlist add

Add certificates to the trusted (or issuer) certificates.

```text
opcuactl gds trustlist add <cert>... [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--group` | `string` | Certificate group whose trust list to change: default\|https\|usertoken or a NodeId (default: the DefaultApplicationGroup) |
| `--issuer` |  | Add to the issuer certificates instead of the trusted certificates |
| `--no-apply` |  | Do not call ApplyChanges |

## opcuactl gds trustlist get

Read the trust list (certificates, issuers, CRLs).

```text
opcuactl gds trustlist get [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--group` | `string` | Certificate group whose trust list to read: default\|https\|usertoken or a NodeId (default: the DefaultApplicationGroup) |
| `--masks` | `strings` | Lists to read: trusted, trusted-crls, issuers, issuer-crls, all (default all) |
| `--out` | `string` | Write the entries as DER files into this directory (trusted/certs, trusted/crl, issuer/certs, issuer/crl) |

## opcuactl gds trustlist remove

Remove certificates from the trusted (or issuer) certificates.

```text
opcuactl gds trustlist remove <thumbprint|cert>... [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--group` | `string` | Certificate group whose trust list to change: default\|https\|usertoken or a NodeId (default: the DefaultApplicationGroup) |
| `--issuer` |  | Remove from the issuer certificates instead of the trusted certificates |
| `--no-apply` |  | Do not call ApplyChanges |

## opcuactl gds trustlist set

Replace the trust list.

```text
opcuactl gds trustlist set <file|dir>... [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--group` | `string` | Certificate group whose trust list to replace: default\|https\|usertoken or a NodeId (default: the DefaultApplicationGroup) |
| `--issuer` | `stringArray` | Issuer certificate or CRL file (repeatable) |
| `--masks` | `strings` | Lists to replace: trusted, trusted-crls, issuers, issuer-crls, all (default all) |
| `--no-apply` |  | Do not call ApplyChanges |
| `--yes` |  | Skip the confirmation prompt |

## opcuactl gds update-cert

```text
opcuactl gds update-cert <cert> [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--group` | `string` | Certificate group of the certificate: default\|https\|usertoken or a NodeId (default: the DefaultApplicationGroup) |
| `--issuer` | `stringArray` | Issuer (CA) certificate file (repeatable) |
| `--key-format` | `string` | Format to send the private key in: pem\|pfx (default: pem, pfx for a PKCS #12 file) |
| `--no-apply` |  | Do not call ApplyChanges |
| `--pfx-password` | `string` | Password of the PFX key (the one the server expects; opens a PKCS #12 --private-key) |
| `--private-key` | `string` | Private key of the certificate, if the server did not make it (PEM, DER or PKCS #12) |
| `--type` | `string` | Certificate type to replace (default: the type of the certificate's key) |
| `--yes` |  | Skip the confirmation prompt |

## opcuactl get

Read values, attributes, and server information.

```text
opcuactl get [command]
```

Subcommands: [`attr`](#opcuactl-get-attr), [`node`](#opcuactl-get-node), [`server`](#opcuactl-get-server), [`value`](#opcuactl-get-value), [`vars`](#opcuactl-get-vars).

## opcuactl get attr

Read node attributes.

```text
opcuactl get attr [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--all` |  | Read all standard OPC UA attributes |
| `--attr` | `stringArray` | Attribute name or numeric ID (repeatable) |
| `--node`, `-n` | `string` | Node ID (required) |

## opcuactl get node

Get node summary.

```text
opcuactl get node [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--full` |  | Fetch additional attributes beyond summary |
| `--node`, `-n` | `stringArray` | Node ID (repeatable) |

## opcuactl get server

Get server status.

```text
opcuactl get server [flags]
```

No flags of its own; the global flags apply.

## opcuactl get value

Read node values.

```text
opcuactl get value [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--index-range` | `string` | IndexRange for array/matrix Value reads |
| `--node`, `-n` | `stringArray` | Node ID to read (repeatable) |
| `--timestamps` |  | Show source and server timestamps in output |
| `--timestamps-to-return` | `string` | Timestamps to request (source\|server\|both\|neither) (default "both") |
| `--ua-encoding` | `string` | Print DataValues in an OPC UA DataEncoding (json\|json-verbose\|json-reversible\|json-nonreversible\|xml) |

## opcuactl get vars

Collect variables under a node.

```text
opcuactl get vars [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--depth` | `int` | Max recursion depth (0 = unlimited when --recursive) |
| `--exclude-ns` | `int` | Exclude nodes in this namespace (-1 = none) (default -1) |
| `--match` | `string` | Filter by browse name / display name substring |
| `--no-read` |  | Do not read Value attribute (metadata only) |
| `--node`, `-n` | `string` | Start node ID (default: Objects folder ns=0;i=85) |
| `--only-ns` | `int` | Only return nodes in this namespace (-1 = all) (default -1) |
| `--paths` |  | Include browse path from start node |
| `--recursive` |  | Traverse subtree recursively |

## opcuactl history

Read and update historical data and events.

```text
opcuactl history [command]
```

Subcommands: [`delete-events`](#opcuactl-history-delete-events), [`event`](#opcuactl-history-event), [`update-events`](#opcuactl-history-update-events), [`value`](#opcuactl-history-value).

## opcuactl history delete-events

```text
opcuactl history delete-events [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--dry-run` |  | Print what would be deleted, do not send it |
| `--event-id` | `stringArray` | EventId to delete, base64 or 0x hex (repeatable, required) |
| `--node`, `-n` | `string` | Event notifier node ID (required) |
| `--yes` |  | Skip the confirmation prompt |

## opcuactl history event

Read historical events of an event notifier.

Alias: `events`.

```text
opcuactl history event [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--continuation` | `string` | Paging control (all = follow continuation points, single = one page only) (default "all") |
| `--end` | `string` | End time in RFC3339 format (before --start: read backward) |
| `--max-events` | `uint32` | Stop after this many events (0 = all) |
| `--node`, `-n` | `string` | Event notifier node ID (required) |
| `--num-values` | `uint32` | Events per page (NumValuesPerNode; 0 = as the server pages) |
| `--of-type` | `string` | Only events of this type or its subtypes (NodeId or name) |
| `--select` | `stringArray` | Event field to return, a BrowsePath like AckedState/Id (repeatable; default: EventId, EventType, SourceName, Time, Message, Severity) |
| `--severity-max` | `uint16` | Maximum Severity (inclusive) |
| `--severity-min` | `uint16` | Minimum Severity (inclusive) |
| `--start` | `string` | Start time in RFC3339 format |

## opcuactl history update-events

```text
opcuactl history update-events [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--dry-run` |  | Print the events, do not send them |
| `--event` | `stringArray` | Event as a JSON object of fields, or an array of them (repeatable) |
| `--events-file` | `string` | JSON file with an event or an array of events (- for stdin) |
| `--mode` | `string` | PerformUpdateType (insert\|replace\|update) (default "insert") |
| `--node`, `-n` | `string` | Event notifier node ID (required) |
| `--yes` |  | Skip the confirmation prompt |

## opcuactl history value

Read historical values for a node.

```text
opcuactl history value [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--aggregate` | `string` | Aggregate for processed mode (average\|min\|max\|count) (default "average") |
| `--at` | `stringArray` | Timestamp for attime mode (repeatable, RFC3339) |
| `--continuation` | `string` | Paging control (all = auto-follow, single = one page only) (default "all") |
| `--end` | `string` | End time in RFC3339 format (required except attime) |
| `--interval` | `duration` | Aggregate interval for processed mode (ProcessingInterval) |
| `--max-values` | `uint32` | Maximum values to return (0 = all, uses pagination) |
| `--mode` | `string` | History read mode (raw\|modified\|processed\|attime) (default "raw") |
| `--node`, `-n` | `string` | Node ID (required) |
| `--start` | `string` | Start time in RFC3339 format (required except attime) |

## opcuactl monitor

Subscribe to live data changes and events.

```text
opcuactl monitor [command]
```

Subcommands: [`alarm`](#opcuactl-monitor-alarm), [`event`](#opcuactl-monitor-event), [`value`](#opcuactl-monitor-value).

## opcuactl monitor alarm

Subscribe to alarm and condition notifications.

```text
opcuactl monitor alarm [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--count` | `int` | Stop after this many events (0 = unlimited) |
| `--duration` | `duration` | Stop after this duration (0 = run until Ctrl+C) |
| `--interval` | `duration` | Publishing interval (default 100ms) |
| `--jsonl` |  | Output newline-delimited JSON (JSONL) |
| `--node`, `-n` | `string` | Event notifier node ID (required) |
| `--select` | `stringArray` | Event field to select (repeatable; default: alarm-related fields) |

## opcuactl monitor event

Subscribe to event notifications.

```text
opcuactl monitor event [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--count` | `int` | Stop after this many events (0 = unlimited) |
| `--duration` | `duration` | Stop after this duration (0 = run until Ctrl+C) |
| `--interval` | `duration` | Publishing interval (default 100ms) |
| `--jsonl` |  | Output newline-delimited JSON (JSONL) |
| `--node`, `-n` | `string` | Event notifier node ID (required) |
| `--of-type` | `string` | OfType WhereClause filter (NodeId) |
| `--select` | `stringArray` | Event field to select (repeatable; default: EventType,SourceName,Message,Severity,Time) |
| `--severity-max` | `uint16` | Maximum Severity (inclusive) |
| `--severity-min` | `uint16` | Minimum Severity (inclusive) |

## opcuactl monitor value

Subscribe to live value changes.

```text
opcuactl monitor value [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--count` | `int` | Stop after this many notifications (0 = unlimited) |
| `--deadband` | `float` | DataChangeFilter deadband value (absolute change, or percent of EURange) |
| `--deadband-type` | `string` | DataChangeFilter deadband type (none\|absolute\|percent) (default "none") |
| `--discard-oldest` |  | Discard oldest samples when the queue is full (default true) |
| `--duration` | `duration` | Stop after this duration (0 = run until Ctrl+C) |
| `--index-range` | `string` | IndexRange to monitor for array values (e.g. "0:3") |
| `--interval` | `duration` | Publishing interval (default 100ms) |
| `--jsonl` |  | Output newline-delimited JSON (JSONL) |
| `--node`, `-n` | `stringArray` | Node ID to monitor (repeatable) |
| `--queue-size` | `uint32` | Monitored item queue size (default 10) |
| `--sampling` | `duration` | Sampling interval per monitored item (0 = server-decided) |
| `--timestamps` | `string` | Timestamps to return (source\|server\|both\|neither) (default "both") |
| `--trigger` | `string` | DataChangeFilter trigger (status\|status-value\|status-value-timestamp) (default "status-value") |

## opcuactl ping

Test connectivity to an OPC UA server.

```text
opcuactl ping [flags]
```

No flags of its own; the global flags apply.

## opcuactl pubsub

OPC UA PubSub: subscribe, publish, Actions, discover, decode, keys, configuration and diagnostics.

```text
opcuactl pubsub [command]
```

Subcommands: [`call`](#opcuactl-pubsub-call), [`config`](#opcuactl-pubsub-config), [`decode`](#opcuactl-pubsub-decode), [`discover`](#opcuactl-pubsub-discover), [`publish`](#opcuactl-pubsub-publish), [`respond`](#opcuactl-pubsub-respond), [`sks`](#opcuactl-pubsub-sks), [`subscribe`](#opcuactl-pubsub-subscribe).

## opcuactl pubsub call

Call a PubSub Action of a Responder and print its result.

```text
opcuactl pubsub call [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--broker-client-id` | `string` | MQTT client id / AMQP container-id (default: random) |
| `--broker-password` | `string` | Broker (MQTT, AMQP) password (or OPCUACTL_BROKER_PASSWORD) |
| `--broker-username` | `string` | Broker (MQTT, AMQP) username |
| `--diagnostics` |  | Print the PubSub diagnostics counters (Part 14 §9.1.11) on stderr when done |
| `--diagnostics-interval` | `duration` | Also print the diagnostics counters at this interval (implies --diagnostics) |
| `--diagnostics-level` | `string` | DiagnosticsLevel of the counters shown (basic\|advanced\|info\|log\|debug) (default "basic") |
| `--encoding` | `string` | Message mapping (uadp\|json; json needs a broker) (default "uadp") |
| `--input` | `stringArray` | Input Name:Type=Value (repeatable, in the order of the request DataSetMetaData) |
| `--interface` | `string` | Network interface: for UDP multicast (default: system choice); required for opc.eth:// |
| `--json-compression` | `string` | Compress JSON action messages over MQTT 5.0 (none\|gzip): Content Type application/json+gzip |
| `--keys-file` | `string` | Keys of the SecurityGroup saved with 'pubsub sks get-keys --show-keys --format json' (instead of an SKS) |
| `--message-security` | `string` | Security of the UADP action messages (none\|sign\|sign-and-encrypt) (default "none") |
| `--mqtt-version` | `string` | MQTT protocol version (3.1.1\|5.0\|BestAvailable; default 3.1.1) |
| `--output` | `stringArray` | Output Name:Type (repeatable, in the order of the response DataSetMetaData) |
| `--publisher-id` | `string` | PublisherId of the Responder (number = UInt16; byte:N, u32:N, u64:N, or a string) (default "1") |
| `--qos` | `uint8` | Broker delivery guarantee: 0 at most once, 1 at least once, 2 exactly once (AMQP: as 1) |
| `--request-address` | `string` | Where the requests go: the request queue on a broker (default: see --topic-prefix); on UDP the Responder's opc.udp:// address (default: --url) |
| `--requestor-id` | `string` | PublisherId of this Requestor (default: a random string) |
| `--resend-interval` | `duration` | UDP: resend the request while no response arrives within this interval (0 = no resends) |
| `--response-address` | `string` | Where the Responder sends the response (broker: default the action-response topic of the request queue; UDP: default --url) |
| `--security-group` | `string` | SecurityGroupId whose keys secure the messages (pulled from the SKS at --endpoint) |
| `--target-id` | `uint16` | ActionTargetId of the Action target (default 1) |
| `--topic-prefix` | `string` | Broker: prefix of the default request queue &lt;prefix&gt;/&lt;encoding&gt;/action-request/&lt;publisher-id&gt;/&lt;writer-group-name&gt; (default "opcua") |
| `--transport-property` | `stringArray` | Connection property key=value (repeatable): AMQP source-capabilities, connection-hostname, ...; MQTT 5.0 connection-* properties |
| `--url` | `string` | Transport address: opc.udp://host:port (unicast or multicast group), opc.eth://MAC[:VID[.PCP]], mqtt(s)://host:port or amqp(s)://host:port |
| `--writer-group-name` | `string` | Name of the Responder's writer group (broker: part of the default request queue) (default "Actions") |
| `--writer-id` | `uint16` | DataSetWriterId of the Action writer of the Responder (default 1) |

## opcuactl pubsub config

Show and change the PubSub configuration of a server.

```text
opcuactl pubsub config [command]
```

Subcommands: [`add-action`](#opcuactl-pubsub-config-add-action), [`add-connection`](#opcuactl-pubsub-config-add-connection), [`add-dataset`](#opcuactl-pubsub-config-add-dataset), [`add-reader`](#opcuactl-pubsub-config-add-reader), [`add-reader-group`](#opcuactl-pubsub-config-add-reader-group), [`add-writer`](#opcuactl-pubsub-config-add-writer), [`add-writer-group`](#opcuactl-pubsub-config-add-writer-group), [`diagnostics`](#opcuactl-pubsub-config-diagnostics), [`disable`](#opcuactl-pubsub-config-disable), [`enable`](#opcuactl-pubsub-config-enable), [`export`](#opcuactl-pubsub-config-export), [`remove`](#opcuactl-pubsub-config-remove), [`reserve-ids`](#opcuactl-pubsub-config-reserve-ids), [`show`](#opcuactl-pubsub-config-show).

## opcuactl pubsub config add-action

Add a PublishedAction data set (PubSubConfiguration CloseAndUpdate).

```text
opcuactl pubsub config add-action [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--method` | `stringArray` | Method of a target ID=OBJECT@METHOD (repeatable; one per target or none) |
| `--name` | `string` | Name of the PublishedAction data set |
| `--request` | `stringArray` | Request field Name:Type (repeatable) |
| `--response` | `stringArray` | Response field Name:Type (repeatable) |
| `--target` | `stringArray` | Action target ID:NAME (repeatable) |

## opcuactl pubsub config add-connection

Add a PubSubConnection (AddConnection).

```text
opcuactl pubsub config add-connection [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--encoding` | `string` | Message mapping over a broker (uadp\|json) (default "uadp") |
| `--interface` | `string` | Network interface (NetworkInterface; required for opc.eth://) |
| `--json-compression` | `string` | Compress JSON messages over MQTT 5.0 (none\|gzip): the JsonCompression connection property |
| `--mqtt-version` | `string` | MQTT protocol version (3.1.1\|5.0\|BestAvailable) |
| `--name` | `string` | Connection name |
| `--publisher-id` | `string` | PublisherId of the connection |
| `--transport-property` | `stringArray` | Connection property key=value (repeatable) |
| `--url` | `string` | Transport address (opc.udp://, opc.eth://, mqtt://, mqtts://, amqp:// or amqps://) |

## opcuactl pubsub config add-dataset

Add a published data set of variables (AddPublishedDataItems).

```text
opcuactl pubsub config add-dataset [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--name` | `string` | Data set name |
| `--var` | `stringArray` | Published variable Field=NodeId (repeatable) |

## opcuactl pubsub config add-reader

Add a DataSetReader to a ReaderGroup (AddDataSetReader).

```text
opcuactl pubsub config add-reader [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--field` | `stringArray` | DataSet field Name:Type (Type[] for an array; repeatable, in DataSet order) |
| `--group` | `string` | NodeId of the ReaderGroup |
| `--message-receive-timeout` | `duration` | MessageReceiveTimeout: the reader goes to Error without messages (0 = none) |
| `--metadata` | `string` | DataSetMetaData file: a ua-metadata message or a DataSetMetaDataType in the OPC UA JSON encoding |
| `--metadata-node` | `string` | Read the DataSetMetaData of this PublishedDataSet or DataSetReader node from the server at --endpoint |
| `--metadata-queue` | `string` | Broker queue of the DataSetMetaData |
| `--name` | `string` | DataSetReader name |
| `--publisher-id` | `string` | PublisherId to read (number = UInt16; byte:N, u32:N, u64:N or a string) |
| `--queue` | `string` | Broker queue to read (MQTT topic, AMQP address) |
| `--target` | `stringArray` | Target variable NodeId for the next field (repeatable) |
| `--writer-group-id` | `uint16` | WriterGroupId to read (0: any) |
| `--writer-id` | `uint16` | DataSetWriterId to read (0: any) |

## opcuactl pubsub config add-reader-group

```text
opcuactl pubsub config add-reader-group [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--connection` | `string` | NodeId of the connection |
| `--message-security` | `string` | Message security (none\|sign\|sign-and-encrypt) (default "none") |
| `--name` | `string` | ReaderGroup name |
| `--security-group` | `string` | SecurityGroupId of the keys |

## opcuactl pubsub config add-writer

Add a DataSetWriter to a WriterGroup (AddDataSetWriter).

```text
opcuactl pubsub config add-writer [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--dataset` | `string` | Name of the published data set |
| `--field-encoding` | `string` | Field encoding (variant\|datavalue\|rawdata) (default "variant") |
| `--group` | `string` | NodeId of the WriterGroup |
| `--key-frame-count` | `uint32` | Key frame every this many messages (1 = key frames only) (default 1) |
| `--metadata-queue` | `string` | Broker queue the DataSetMetaData is announced on |
| `--name` | `string` | DataSetWriter name |
| `--queue` | `string` | Broker queue of the writer (MQTT topic, AMQP address) |
| `--writer-id` | `uint16` | DataSetWriterId (default 1) |

## opcuactl pubsub config add-writer-group

```text
opcuactl pubsub config add-writer-group [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--connection` | `string` | NodeId of the connection |
| `--encoding` | `string` | Message mapping (uadp\|json) (default "uadp") |
| `--interval` | `duration` | Publishing interval (default 1s) |
| `--keep-alive` | `duration` | KeepAliveTime (0 = server default) |
| `--message-security` | `string` | Message security (none\|sign\|sign-and-encrypt) (default "none") |
| `--name` | `string` | WriterGroup name |
| `--queue` | `string` | Broker queue of the group (MQTT topic, AMQP address) |
| `--security-group` | `string` | SecurityGroupId of the keys |
| `--writer-group-id` | `uint16` | WriterGroupId (default 1) |

## opcuactl pubsub config diagnostics

Read the PubSub diagnostics of a component (Part 14 §9.1.11).

```text
opcuactl pubsub config diagnostics [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--node` | `string` | NodeId of the component (default: the PublishSubscribe object) |
| `--reset` |  | Reset the counters first (the Reset method) |

## opcuactl pubsub config disable

Disable a PubSub component (or the whole PubSub).

```text
opcuactl pubsub config disable [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--node` | `string` | NodeId of the component (default: the PublishSubscribe object) |

## opcuactl pubsub config enable

Enable a PubSub component (or the whole PubSub).

```text
opcuactl pubsub config enable [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--node` | `string` | NodeId of the component (default: the PublishSubscribe object) |

## opcuactl pubsub config export

Read the PubSubConfiguration file of a server.

```text
opcuactl pubsub config export [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--out` | `string` | Write the configuration to this file |
| `--ua-encoding` | `string` | OPC UA encoding of the output (json\|json-verbose\|json-reversible\|json-nonreversible\|xml) (default "json-reversible") |

## opcuactl pubsub config remove

Remove a PubSub component.

```text
opcuactl pubsub config remove connection|group|writer|reader|dataset [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--node` | `string` | NodeId of the component to remove |
| `--parent` | `string` | NodeId of its connection (group) or group (writer, reader) |

## opcuactl pubsub config reserve-ids

Reserve WriterGroupIds and DataSetWriterIds (ReserveIds).

```text
opcuactl pubsub config reserve-ids [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--encoding` | `string` | Message mapping (uadp\|json) (default "uadp") |
| `--url` | `string` | Transport URL that selects the transport profile (default "opc.udp://224.0.0.22:4840") |
| `--writer-groups` | `uint16` | Number of WriterGroupIds (default 1) |
| `--writers` | `uint16` | Number of DataSetWriterIds (default 1) |

## opcuactl pubsub config show

Show the PubSub components of a server.

```text
opcuactl pubsub config show [flags]
```

No flags of its own; the global flags apply.

## opcuactl pubsub decode

Decode captured UADP or JSON NetworkMessages.

```text
opcuactl pubsub decode [file...] [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--encoding` | `string` | Message mapping (auto\|uadp\|json) (default "auto") |
| `--field` | `stringArray` | DataSet field Name:Type (Type[] for an array; repeatable, in DataSet order) |
| `--input` | `string` | Input form (auto\|raw\|hex\|base64) (default "auto") |
| `--keys-file` | `string` | Keys of the SecurityGroup saved with 'pubsub sks get-keys --show-keys --format json' (instead of an SKS) |
| `--message-security` | `string` | Minimum security of the message (none\|sign\|sign-and-encrypt) (default "none") |
| `--metadata` | `string` | DataSetMetaData file: a ua-metadata message or a DataSetMetaDataType in the OPC UA JSON encoding |
| `--metadata-node` | `string` | Read the DataSetMetaData of this PublishedDataSet or DataSetReader node from the server at --endpoint |
| `--publisher-id` | `string` | Only messages of this PublisherId |
| `--security-group` | `string` | SecurityGroupId whose keys secure the messages (pulled from the SKS at --endpoint) |
| `--writer-group-id` | `uint16` | Only messages of this WriterGroupId (UADP; 0: all) |
| `--writer-id` | `uint16` | Only DataSetMessages of this DataSetWriterId (0: all) |

## opcuactl pubsub discover

Send UADP discovery probes and print the announcements.

```text
opcuactl pubsub discover [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--count` | `int` | Stop after this many announcements (0 = wait --duration) |
| `--diagnostics` |  | Print the PubSub diagnostics counters (Part 14 §9.1.11) on stderr when done |
| `--diagnostics-interval` | `duration` | Also print the diagnostics counters at this interval (implies --diagnostics) |
| `--diagnostics-level` | `string` | DiagnosticsLevel of the counters shown (basic\|advanced\|info\|log\|debug) (default "basic") |
| `--discovery-layout` | `string` | Byte layout of the probes (spec\|dotnet) (default "spec") |
| `--duration` | `duration` | How long to wait for announcements (default 3s) |
| `--include-groups` |  | connections probe: include the WriterGroups |
| `--include-writers` |  | group and connections probes: include the DataSetWriters |
| `--interface` | `string` | Network interface (UDP multicast; required for opc.eth://) |
| `--jsonl` |  | Output newline-delimited JSON (JSONL) |
| `--keys-file` | `string` | Keys of the SecurityGroup saved with 'pubsub sks get-keys --show-keys --format json' (instead of an SKS) |
| `--listen-url` | `stringArray` | Further address to listen on for announcements (repeatable) |
| `--message-security` | `string` | Minimum security of the announcements (none\|sign\|sign-and-encrypt) (default "none") |
| `--probe` | `strings` | What to probe for (applications\|endpoints\|metadata\|writers\|group\|connections; repeatable) (default [applications]) |
| `--publisher-id` | `string` | Only probe (and show) this PublisherId |
| `--security-group` | `string` | SecurityGroupId whose keys secure the messages (pulled from the SKS at --endpoint) |
| `--transport-profile` | `stringArray` | connections probe: only connections with this TransportProfileUri (repeatable) |
| `--url` | `string` | Where to send the probes: a multicast group or DiscoveryAddress (opc.udp://), or opc.eth:// |
| `--writer-group-id` | `uint16` | WriterGroupId of the group probe |
| `--writer-id` | `uints` | DataSetWriterIds of the metadata and writers probes (default: all) (default []) |

## opcuactl pubsub publish

Publish a DataSet built from the command line (for testing).

```text
opcuactl pubsub publish [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--announce-interval` | `duration` | Repeat the DataSetMetaData announcement (or --metadata-topic message) at this interval |
| `--announce-metadata` |  | Send a UADP DataSetMetaData announcement before the data (UDP, Ethernet) |
| `--answer-probes` |  | Answer UADP discovery probes while running (UDP multicast, Ethernet or --discovery-url) |
| `--broker-client-id` | `string` | MQTT client id / AMQP container-id (default: random) |
| `--broker-password` | `string` | Broker (MQTT, AMQP) password (or OPCUACTL_BROKER_PASSWORD) |
| `--broker-username` | `string` | Broker (MQTT, AMQP) username |
| `--count` | `int` | Number of messages to send (0 = until Ctrl+C or --duration) (default 1) |
| `--dataset-name` | `string` | Name of the DataSet in its DataSetMetaData (default "DataSet") |
| `--diagnostics` |  | Print the PubSub diagnostics counters (Part 14 §9.1.11) on stderr when done |
| `--diagnostics-interval` | `duration` | Also print the diagnostics counters at this interval (implies --diagnostics) |
| `--diagnostics-level` | `string` | DiagnosticsLevel of the counters shown (basic\|advanced\|info\|log\|debug) (default "basic") |
| `--discovery-layout` | `string` | Byte layout of the probes sent (spec\|dotnet) (default "spec") |
| `--discovery-url` | `string` | DiscoveryAddress for UADP discovery probes and announcements (default: --url if it is a multicast group or opc.eth://) |
| `--duration` | `duration` | Stop after this duration (0 = no limit) |
| `--encoding` | `string` | Message mapping (uadp\|json) (default "uadp") |
| `--field` | `stringArray` | Field Name:Type=Value (repeatable) |
| `--field-encoding` | `string` | Field encoding (variant\|datavalue\|rawdata) (default "variant") |
| `--interface` | `string` | Network interface: for UDP multicast (default: system choice); required for opc.eth:// |
| `--interval` | `duration` | Publishing interval (default 1s) |
| `--json-compression` | `string` | Compress JSON messages over MQTT 5.0 (none\|gzip): Content Type application/json+gzip |
| `--keys-file` | `string` | Keys of the SecurityGroup saved with 'pubsub sks get-keys --show-keys --format json' (instead of an SKS) |
| `--max-message-size` | `int` | Largest NetworkMessage in bytes: larger UADP messages are sent as chunks (0 = the transport's limit) |
| `--message-security` | `string` | Security of the messages (none\|sign\|sign-and-encrypt) (default "none") |
| `--metadata-topic` | `string` | Broker queue to send the DataSetMetaData to (retained on MQTT) before the data |
| `--mqtt-version` | `string` | MQTT protocol version (3.1.1\|5.0\|BestAvailable; default 3.1.1) |
| `--numeric-publisher-id` |  | JSON: write numeric PublisherIds as numbers (OPC UA 1.04) instead of strings |
| `--publisher-id` | `string` | PublisherId (number = UInt16; byte:N, u32:N, u64:N, or a string) (default "1") |
| `--qos` | `uint8` | Broker delivery guarantee: 0 at most once, 1 at least once, 2 exactly once (AMQP: as 1) |
| `--retain` |  | Ask the MQTT broker to retain the data messages |
| `--security-group` | `string` | SecurityGroupId whose keys secure the messages (pulled from the SKS at --endpoint) |
| `--topic` | `string` | Broker queue of the data messages: MQTT topic or AMQP address |
| `--transport-property` | `stringArray` | Connection property key=value (repeatable): AMQP source-capabilities, connection-hostname, ...; MQTT 5.0 connection-* properties |
| `--url` | `string` | Transport address: opc.udp://host:port (unicast or multicast group), opc.eth://MAC[:VID[.PCP]], mqtt(s)://host:port or amqp(s)://host:port |
| `--writer-group-id` | `uint16` | WriterGroupId (default 1) |
| `--writer-group-name` | `string` | WriterGroup name (JSON messages) (default "opcuactl") |
| `--writer-id` | `uint16` | DataSetWriterId (default 1) |
| `--writer-name` | `string` | DataSetWriter name (JSON messages) (default "opcuactl") |

## opcuactl pubsub respond

Answer PubSub Action requests (a test Responder).

```text
opcuactl pubsub respond [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--broker-client-id` | `string` | MQTT client id / AMQP container-id (default: random) |
| `--broker-password` | `string` | Broker (MQTT, AMQP) password (or OPCUACTL_BROKER_PASSWORD) |
| `--broker-username` | `string` | Broker (MQTT, AMQP) username |
| `--burst` | `int` | Burst of new requests above --rate (0 = 50) |
| `--count` | `int` | Stop after serving this many requests (0 = until Ctrl+C or --duration) |
| `--dataset-name` | `string` | Name of the PublishedAction (default "Action") |
| `--delay` | `duration` | Wait this long before answering a request |
| `--diagnostics` |  | Print the PubSub diagnostics counters (Part 14 §9.1.11) on stderr when done |
| `--diagnostics-interval` | `duration` | Also print the diagnostics counters at this interval (implies --diagnostics) |
| `--diagnostics-level` | `string` | DiagnosticsLevel of the counters shown (basic\|advanced\|info\|log\|debug) (default "basic") |
| `--duration` | `duration` | Stop after this duration (0 = no limit) |
| `--encoding` | `string` | Message mapping (uadp\|json; json needs a broker) (default "uadp") |
| `--input` | `stringArray` | Request field Name:Type (repeatable; without: any inputs are accepted) |
| `--interface` | `string` | Network interface: for UDP multicast (default: system choice); required for opc.eth:// |
| `--interval` | `duration` | PublishingInterval of the writer group: how often a datagram response is repeated until acknowledged (default 100ms on UDP, 0 on a broker) |
| `--json-compression` | `string` | Compress JSON action messages over MQTT 5.0 (none\|gzip): Content Type application/json+gzip |
| `--jsonl` |  | Output newline-delimited JSON (JSONL) |
| `--keys-file` | `string` | Keys of the SecurityGroup saved with 'pubsub sks get-keys --show-keys --format json' (instead of an SKS) |
| `--max-concurrent` | `int` | Exchanges kept at once; more are answered with Bad_TooManyOperations (0 = 64) |
| `--max-timeout` | `duration` | Cap of the TimeoutHint of a request (0 = 60s) |
| `--message-security` | `string` | Security of the UADP action messages (none\|sign\|sign-and-encrypt) (default "none") |
| `--mqtt-version` | `string` | MQTT protocol version (3.1.1\|5.0\|BestAvailable; default 3.1.1) |
| `--output` | `stringArray` | Response field Name:Type=Value (repeatable; without: echo the inputs) |
| `--publisher-id` | `string` | PublisherId of this Responder (number = UInt16; byte:N, u32:N, u64:N, or a string) (default "1") |
| `--qos` | `uint8` | Broker delivery guarantee: 0 at most once, 1 at least once, 2 exactly once (AMQP: as 1) |
| `--rate` | `float` | New requests accepted per second; more are dropped (0 = 50) |
| `--request-address` | `string` | Where the requests go: the request queue on a broker (default: see --topic-prefix); on UDP the Responder's opc.udp:// address (default: --url) |
| `--security-group` | `string` | SecurityGroupId whose keys secure the messages (pulled from the SKS at --endpoint) |
| `--status` | `string` | StatusCode of the responses (name or number; a Bad status sends no outputs) (default "Good") |
| `--target` | `stringArray` | Action target ID[:Name] (repeatable; default 1) |
| `--topic-prefix` | `string` | Broker: prefix of the default request queue &lt;prefix&gt;/&lt;encoding&gt;/action-request/&lt;publisher-id&gt;/&lt;writer-group-name&gt; (default "opcua") |
| `--transport-property` | `stringArray` | Connection property key=value (repeatable): AMQP source-capabilities, connection-hostname, ...; MQTT 5.0 connection-* properties |
| `--url` | `string` | Transport address: opc.udp://host:port (unicast or multicast group), opc.eth://MAC[:VID[.PCP]], mqtt(s)://host:port or amqp(s)://host:port |
| `--writer-group-id` | `uint16` | WriterGroupId of the writer group (default 1) |
| `--writer-group-name` | `string` | Name of the Responder's writer group (broker: part of the default request queue) (default "Actions") |
| `--writer-id` | `uint16` | DataSetWriterId of the Action writer of the Responder (default 1) |
| `--writer-name` | `string` | Name of the Action DataSetWriter (default "Actions") |

## opcuactl pubsub sks

Security Key Service (SKS) client.

```text
opcuactl pubsub sks [command]
```

Subcommands: [`get-keys`](#opcuactl-pubsub-sks-get-keys).

## opcuactl pubsub sks get-keys

Get the keys of a SecurityGroup (GetSecurityKeys).

```text
opcuactl pubsub sks get-keys [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--future-keys` | `uint32` | Number of future keys wanted after the first (default 1) |
| `--security-group` | `string` | SecurityGroupId |
| `--show-keys` |  | Print the key material (base64); treat the output as a secret |
| `--starting-token` | `uint32` | First token id wanted (0 = the current key) |

## opcuactl pubsub subscribe

Listen for PubSub NetworkMessages and print their DataSets.

```text
opcuactl pubsub subscribe [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--broker-client-id` | `string` | MQTT client id / AMQP container-id (default: random) |
| `--broker-password` | `string` | Broker (MQTT, AMQP) password (or OPCUACTL_BROKER_PASSWORD) |
| `--broker-username` | `string` | Broker (MQTT, AMQP) username |
| `--chunk-max-message-size` | `int` | Largest chunked message reassembled, in bytes (0 = 2 MiB) |
| `--chunk-max-pending` | `int` | Incomplete chunked messages kept (0 = 512) |
| `--chunk-max-pending-bytes` | `int` | Bytes of incomplete chunked messages kept (0 = 32 MiB) |
| `--chunk-timeout` | `duration` | How long an incomplete chunked message waits for its chunks (0 = 5s) |
| `--count` | `int` | Stop after this many DataSetMessages (0 = unlimited) |
| `--diagnostics` |  | Print the PubSub diagnostics counters (Part 14 §9.1.11) on stderr when done |
| `--diagnostics-interval` | `duration` | Also print the diagnostics counters at this interval (implies --diagnostics) |
| `--diagnostics-level` | `string` | DiagnosticsLevel of the counters shown (basic\|advanced\|info\|log\|debug) (default "basic") |
| `--discovery-layout` | `string` | Byte layout of the probes sent (spec\|dotnet) (default "spec") |
| `--discovery-url` | `string` | DiscoveryAddress for UADP discovery probes and announcements (default: --url if it is a multicast group or opc.eth://) |
| `--duration` | `duration` | Stop after this duration (0 = run until Ctrl+C) |
| `--encoding` | `string` | Message mapping (auto\|uadp\|json) (default "auto") |
| `--field` | `stringArray` | DataSet field Name:Type (Type[] for an array; repeatable, in DataSet order) |
| `--interface` | `string` | Network interface: for UDP multicast (default: system choice); required for opc.eth:// |
| `--jsonl` |  | Output newline-delimited JSON (JSONL) |
| `--keys-file` | `string` | Keys of the SecurityGroup saved with 'pubsub sks get-keys --show-keys --format json' (instead of an SKS) |
| `--message-security` | `string` | Minimum security of the messages (none\|sign\|sign-and-encrypt) (default "none") |
| `--metadata` | `string` | DataSetMetaData file: a ua-metadata message or a DataSetMetaDataType in the OPC UA JSON encoding |
| `--metadata-node` | `string` | Read the DataSetMetaData of this PublishedDataSet or DataSetReader node from the server at --endpoint |
| `--mqtt-version` | `string` | MQTT protocol version (3.1.1\|5.0\|BestAvailable; default 3.1.1) |
| `--probe` |  | Probe for the DataSetMetaData of writers seen without one (UADP discovery) |
| `--publisher-id` | `string` | Only messages of this PublisherId |
| `--qos` | `uint8` | Broker delivery guarantee: 0 at most once, 1 at least once, 2 exactly once (AMQP: as 1) |
| `--security-group` | `string` | SecurityGroupId whose keys secure the messages (pulled from the SKS at --endpoint) |
| `--show-errors` |  | Report every message that cannot be decoded (default: the first 10) |
| `--show-metadata` |  | Also print DataSetMetaData and other discovery announcements and status messages |
| `--topic` | `string` | Broker queue to receive from: MQTT topic filter or AMQP address (default "#") |
| `--transport-property` | `stringArray` | Connection property key=value (repeatable): AMQP source-capabilities, connection-hostname, ...; MQTT 5.0 connection-* properties |
| `--url` | `string` | Transport address: opc.udp://host:port (unicast or multicast group), opc.eth://MAC[:VID[.PCP]], mqtt(s)://host:port or amqp(s)://host:port |
| `--writer-group-id` | `uint16` | Only messages of this WriterGroupId (UADP; 0: all) |
| `--writer-id` | `uint16` | Only DataSetMessages of this DataSetWriterId (0: all) |

## opcuactl serve

Run an OPC UA test server.

```text
opcuactl serve [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--allow-anonymous` |  | Allow anonymous sessions (default true) |
| `--application-uri` | `string` | ApplicationUri (default: the one of --cert, or urn:&lt;hostname&gt;:opcuactl:serve) |
| `--cert` | `stringArray` | Application instance certificate file (PEM/DER, repeatable; default: self-signed ones) |
| `--cert-out` | `string` | Write the application certificates (PEM) to this file |
| `--duration` | `duration` | Stop after this time (default: run until Ctrl+C / SIGTERM) |
| `--hostname` | `string` | Host name of the endpoint URLs (default: the host of --listen, or this machine's name for a wildcard address) |
| `--https` | `string` | Also listen for opc.https (UA Binary over HTTPS) on this address (host:port) |
| `--insecure-no-security` |  | Offer endpoints without message security (--security-policy None): anyone on the network can read and change the traffic |
| `--key` | `stringArray` | Private key file of --cert (PEM/DER, repeatable) |
| `--key-out` | `string` | Write the private keys of the application certificates (PEM, mode 0600) to this file |
| `--listen` | `string` | opc.tcp address to listen on (opc.tcp://host:port or host:port; port 0 picks a free port) (default "opc.tcp://0.0.0.0:4840") |
| `--log-clients` |  | Log each client connection, session and refused request on stderr, whatever --log-level |
| `--log-level` | `string` | Server log on stderr (debug\|info\|warn\|error\|off) (default "error") |
| `--max-sessions` | `int` | Sessions the server keeps at most, activated or not (BadTooManySessions beyond) (default 100) |
| `--max-subscriptions` | `int` | Subscriptions of all sessions together at most (BadTooManySubscriptions beyond) (default 1024) |
| `--max-subscriptions-per-session` | `int` | Subscriptions of one session at most (BadTooManySubscriptions beyond) (default 64) |
| `--no-demo` |  | Leave out the demo namespace |
| `--nodeset` | `stringArray` | Import a NodeSet2 XML file (repeatable) |
| `--security-mode` | `strings` | Security modes of the policies other than None (Sign\|SignAndEncrypt; default Sign,SignAndEncrypt) |
| `--security-policy` | `strings` | Security policies to offer (repeatable or comma-separated; default Basic256Sha256) |
| `--sim-interval` | `duration` | Update interval of the simulated demo values (default 1s) |
| `--tls-cert` | `string` | opc.wss/opc.https: TLS certificate (PEM; default: the RSA application certificate) |
| `--tls-key` | `string` | opc.wss/opc.https: private key (PEM) of --tls-cert |
| `--trust-dir` | `string` | Directory of trusted client certificates or CAs (*.der, *.pem, *.crt, *.cer) |
| `--trusted-cert` | `stringArray` | Trusted client certificate or CA (PEM/DER, repeatable); other clients are rejected |
| `--uajson` |  | Serve the UA JSON mappings wss-uajson and https-uajson on --wss and --https |
| `--user` | `stringArray` | Accept this user name:password (repeatable, or OPCUACTL_SERVE_USERS) |
| `--wss` | `string` | Also listen for opc.wss (WebSocket over TLS) on this address (host:port) |

## opcuactl version

Print version information.

```text
opcuactl version [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--json` |  | Print the build information as one JSON object |

## opcuactl write

Write values to OPC UA nodes.

```text
opcuactl write [command]
```

Subcommands: [`value`](#opcuactl-write-value).

## opcuactl write value

Write a value to a node.

```text
opcuactl write value [flags]
```

| Flag | Value | Meaning |
|---|---|---|
| `--dry-run` |  | Validate and parse only, do not send to server |
| `--index-range` | `string` | OPC UA index range for array writes (e.g. "0:2") |
| `--node`, `-n` | `string` | Target node ID (required) |
| `--type` | `string` | Value type (bool\|int16\|uint16\|int32\|uint32\|int64\|uint64\|float\|double\|string\|bytes\|datetime\|guid\|nodeid\|json\|ua-json\|ua-xml) (default "string") |
| `--value` | `string` | Value to write (required) |
| `--yes` |  | Skip write confirmation prompt |
