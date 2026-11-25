# Process exit codes

opcuactl's exit codes are a script-facing contract (`cmd/opcuactl/errors.go`). They follow the organisation table (otfabric/engineering `standards/api-design.md#cli-exit-codes`). Commands choose a code; a cancellation, timeout, unsupported service or permission error in the chain overrides it, a lost connection turns a protocol failure into a transport failure, and partial and findings results stay as they are.

| Code | Kind | Meaning |
|------|------|---------|
| 0 | ok | Success |
| 1 | findings | `gds pull check-revocation`: the certificate is not valid; `fx inspect --check`: the server is not a UAFX server or has findings |
| 2 | usage | Invalid flag or value, also a request file of `fx connect` that is not valid. Errors without a code of their own, such as cobra's, are usage errors |
| 3 | invalid_input | An input or output file that cannot be opened |
| 4 | transport | Connection or transport failure, also a connection lost during a service, `monitor` or `alarms watch`, a subscription of `monitor` or `alarms watch` that the server ends or transfers, and a server whose CreateSession endpoints differ from the GetEndpoints answer that `--security-policy auto`, `--security-mode auto` or a policy without `--server-cert` selected from (`BadSecurityChecksFailed`, no session is activated); `serve` when a listener cannot start |
| 5 | timeout | A deadline passed or `Bad_Timeout`; `pubsub call` with no response within `--timeout`; `fx cm process --wait` with a set still Processing when the wait ran out (opcuactl stops watching; the server goes on); `fx cm edit` when CommitUpdates got no answer within `--timeout` (see `outcome`) |
| 6 | protocol | OPC UA service or status-code failure, for example a Bad result of `call`, or a response with another number of results than the request asked for; `fx connect` when EstablishConnections returns Uncertain (the server aborted the call, also when an element failed with `BadUserAccessDenied`) or Bad; `fx close` when no endpoint was closed; `fx cm process` and `fx cm import` when no set or operation succeeded, `fx cm process --wait` when a set ended in Error or its state could not be established (unreadable, or not a state of Part 81 §6.9.2), `fx cm edit` when StartEditing, a write, RenewLock or CommitUpdates failed, `fx cm import` when the answer of CloseAndUpdate does not fit Table F.39 |
| 7 | partial | Batch and export operations, `history update-events` and `delete-events` with some failed events, a recursive walk that reached `--max-nodes`, `fx inspect` cut short by a limit, `fx close` with some endpoints not closed, `fx cm process` and `fx cm import` with some sets or operations failed, `fx cm list` cut short by a bound, `fx cm process` and `edit` naming a set by a path in a listing cut short, `fx cm export` of a file larger than 64 MiB |
| 8 | unsupported | `Bad_ServiceUnsupported`; `fx cm` against a server without a ConnectionManager, `fx cm export` and `import` against one without the ConnectionManagerConfiguration file, `fx cm process` and `edit` when the server answers `Bad_MethodInvalid` or `Bad_NotImplemented` |
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
