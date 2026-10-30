# Process exit codes

opcuactl's exit codes are a script-facing contract (`cmd/opcuactl/errors.go`). They follow the organisation table (otfabric/engineering `standards/api-design.md#cli-exit-codes`). Commands choose a code; a cancellation, timeout, unsupported service or permission error in the chain overrides it, a lost connection turns a protocol failure into a transport failure, and partial and findings results stay as they are.

| Code | Kind | Meaning |
|------|------|---------|
| 0 | ok | Success |
| 1 | findings | `gds pull check-revocation`: the certificate is not valid |
| 2 | usage | Invalid flag or value. Errors without a code of their own, such as cobra's, are usage errors |
| 3 | invalid_input | An input or output file that cannot be opened |
| 4 | transport | Connection or transport failure, also a connection lost during a service or `alarms watch`; `serve` when a listener cannot start |
| 5 | timeout | A deadline passed or `Bad_Timeout`; `pubsub call` with no response within `--timeout` |
| 6 | protocol | OPC UA service or status-code failure, for example a Bad result of `call` |
| 7 | partial | Batch and export operations, `history update-events` and `delete-events` with some failed events |
| 8 | unsupported | `Bad_ServiceUnsupported` |
| 9 | permission | `Bad_UserAccessDenied`, or the operating system refused an operation |
| 130 | interrupted | SIGINT or SIGTERM before a result. Streams stopped that way end with 0 |

Exit 10 (internal) is not used.

`OPCUACTL_FORMAT` sets the default of `--format`; `OPCUACTL_OUTPUT_FORMAT` is accepted too and `OPCUACTL_FORMAT` wins.

## Mapping from versions before v0.11.0

| Case | Old | New |
|---|---|---|
| Certificate not valid (`check-revocation`) | 4 | 1 |
| Usage or validation | 1 | 2 |
| Unreadable file | 1 | 3 |
| Connection or transport | 2 | 4 |
| Timeout | 2 or 3 | 5 |
| Service or status failure | 3 | 6 |
| Partial success | 4 | 7 |
| Service unsupported | 3 | 8 |
| Access denied | 3 | 9 |
| Interrupted | killed by the signal | 130 |
