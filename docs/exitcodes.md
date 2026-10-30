# Process exit codes

opcuactl's exit codes are a script-facing contract. Changes are noted in RELEASE.md.

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | Usage or validation error; also any error without a specific code |
| 2 | Connection or transport failure; `pubsub call` with no response within `--timeout`; `serve` when a listener cannot start |
| 3 | OPC UA service or status-code failure, for example a Bad result of `call` or a refused alarm method |
| 4 | Partial success (batch and export operations, `history update-events` and `delete-events` with some failed events); `gds pull check-revocation`: the certificate is not valid |

Defined in `cmd/opcuactl/errors.go`; the commands choose the code.
