# opcuactl Releases

## v0.2.0

**Date:** 2026-07-25  
**Previous release:** v0.1.1

### Summary

Minor release for operators: bump to **[go-opcua v1.3.1](https://github.com/otfabric/go-opcua/releases/tag/v1.3.1)**, finish incomplete CLI flags against that API, and expose the interop-era client capabilities — IndexRange reads, event WhereClause filters, monitored-item queue windows, history attime/processed aggregates, export history mode parity, and full authN/Z coverage so `opcuactl` can talk to as many servers as possible.

### Dependency

- **`github.com/otfabric/go-opcua`**: v1.0.2 → **v1.3.1**
- No intentional public API signature breaks for the client helpers opcuactl already used (`FindServers` / `GetEndpoints`, `Read`/`Write`, `NewSubscription` / `MonitorEvents`, `HistoryRead*`)
- Behavioural notes from go-opcua v1.3.0 still apply to peers: Write EncodingMask is value-only; subscription queues / monitoring modes follow Part 4 against go-opcua servers

### Authentication and security

- **`--auth anonymous|username|certificate|issued`** — all go-opcua user identity modes (`issued` is new; aliases `issuedtoken` / `token` accepted)
- **`--token` / `--token-file`** — issued-token data (raw or base64)
- **`--auth-policy-id`** — optional override when not using security `auto`
- **`--trusted-cert`** (repeatable) — trust private CAs / self-signed server certs
- **`--security-policy auto` / `--security-mode auto`** — discover endpoints and apply the best matching secure configuration (including user-token PolicyId)
- Certificate auth presents both the secure-channel cert/key and the X.509 user identity token
- `--timeout` also drives dial and request timeouts (`DialTimeout` / `RequestTimeout`)
- Connection-related env vars apply when the matching flag is not set on the CLI, including defaults such as `auth` / `security-*`:
  `OPCUACTL_ENDPOINT`, `OPCUACTL_AUTH`, `OPCUACTL_USERNAME`, `OPCUACTL_PASSWORD`, `OPCUACTL_AUTH_POLICY_ID`, `OPCUACTL_TOKEN`, `OPCUACTL_TOKEN_FILE`, `OPCUACTL_CERT`, `OPCUACTL_KEY`, `OPCUACTL_SERVER_CERT`, `OPCUACTL_TRUSTED_CERT` (CSV), `OPCUACTL_SECURITY_POLICY`, `OPCUACTL_SECURITY_MODE`, `OPCUACTL_TIMEOUT`, `OPCUACTL_FORMAT`, `OPCUACTL_NAMESPACE`

### Correctness fixes

- **`monitor value --queue-size`** — previously logged only; now applied via `MonitorItems` / `RequestedParameters.QueueSize`
- **`history value --mode attime`** — was advertised but rejected; now implemented with `Client.HistoryReadAtTime` and repeatable `--at`
- **`history value --mode processed`** — now sets `ReadProcessedDetails.AggregateType` (default Average) via `--aggregate`

### New and improved commands

#### `get value`
- **`--index-range`** — array/matrix Value subset reads via `ReadRequest.IndexRange`
- **`--timestamps-to-return`** — `source|server|both|neither` (server-side); `--timestamps` still controls display

#### `monitor value`
- **`--discard-oldest`** (new; default `true`) and **`--queue-size`** are applied to monitored-item create parameters (Part 4 window semantics)
- **Overflow** is surfaced in table (`[OVERFLOW]`) and JSON (`overflow: true`) when the DataValue Overflow InfoBit is set

#### `monitor event`
- **`--of-type <NodeId>`**, **`--severity-min`**, **`--severity-max`** build a WhereClause via `ua.NewEventFilter` / `ua.OfType` / Severity comparisons

#### `history value` / `export values --history`
- **`--mode attime`** with repeatable **`--at`** RFC3339 timestamps (`HistoryReadAtTime`; `--start`/`--end` not required)
- **`--mode processed`** requires **`--interval`** and accepts **`--aggregate average|min|max|count`** (`AggregateType`)
- Export history supports the same `raw|modified|processed|attime` modes (including `--at` / `--aggregate` / `--interval`)

### Documentation

- [README.md](README.md) — security/auth table, global flags, IndexRange / history / monitor examples, tables of contents
- [RECIPES.md](RECIPES.md) — connection presets for all auth modes, env-var recipe, history/export and event-filter workflows
- [EXAMPLES.md](EXAMPLES.md) — still captured against a WAGO PLC (`None`/`anonymous`); adds a **v0.2.0 syntax** section for secure auth, IndexRange, event filters, and history modes not available on that device

### Packaging

- MIT license retained; `// SPDX-License-Identifier: MIT` on first-party Go sources
- README badge block standardized (Go version, pkg.go.dev, License, CI, Go Report Card, tokenless Codecov, Release)

### Compatibility

No intentional breaking CLI flag removals. Defaults preserve prior behaviour (`auth=anonymous`, `security-policy/mode=None`, `queue-size=10`, `discard-oldest=true`, history mode `raw`, aggregate `average`, `timestamps-to-return=both`). CLI flag semantics for the three correctness fixes above become effective. Certificate auth now also presents an X.509 identity token (required by servers that advertise `UserTokenTypeCertificate`).

---

## v0.1.1

**Date:** 2026-03-25  
**Previous release:** v0.1.0

### Summary

This release migrates the CLI to the renamed **go-opcua** module (v1.0.2), aligns with upstream v1 API moves, and replaces file-based versioning with **link-time `main.*` build metadata** (including optional **tag** in `opcuactl version`). CI and release automation are updated to shared organization workflows.

### Dependency and imports

- **Primary library:** `github.com/otfabric/opcua` → [`github.com/otfabric/go-opcua`](https://github.com/otfabric/go-opcua) **v1.0.2**.
- Upstream v1.0+ uses idiomatic generated identifiers (CamelCase, Go initialisms). This repo did not rely on legacy `id.Foo_Bar` names; no mechanical renames were required in application code beyond import paths.

### API adjustments for go-opcua v1

The following symbols are used from the **`ua`** package (or moved there) instead of the root client package:

| Area | Change |
|------|--------|
| Symbolic standard node names | `opcua.StandardNodeID` → **`ua.StandardNodeID`** (`internal/parse`) |
| Endpoint selection | `opcua.SelectEndpoint` → **`ua.SelectEndpoint`** (`discover`) |
| Display helpers | `opcua.DataTypeDisplayName`, `TypeDefinitionDisplayName`, `ReferenceTypeDisplayName` → **`ua.*`** (`browse`, `export`, `find`, `get`, `vars`, `write`) |

### Version and build metadata

- **Removed** root **`version.txt`**. Version strings for local/release builds come from Git and the linker.
- **`main` package** defines `version`, `tag`, `commit`, `buildDate`; **`init`** calls `opcuactl.SetBuildInfo` so the `version` subcommand stays in `cmd/opcuactl`.
- **Makefile** `LDFLAGS`: **`-s -w`** plus  
  `-X main.version=… -X main.tag=… -X main.commit=… -X main.buildDate=…`  
  **`VERSION`** defaults to `git describe --tags --always --dirty` (or `dev`); **`TAG`** to a tag pointing at `HEAD` when present.
- **`opcuactl version`**: table and JSON output include **`tag`** when non-empty.

### CI and release

- **CI:** reusable workflow **`otfabric/.github/.github/workflows/go-ci.yml@v2`** (Go **1.25** and **1.26**).
- **Release:** **`release.yml`** dispatches **`go-binary-release.yml@v2`** with the `main.*` ldflags above and **`run-tests: true`**.

### Documentation

- **README** links updated to **otfabric/go-opcua**.
- **EXAMPLES** version sample updated for current `version` output.

### Migration (for forks or custom builds)

1. Run **`go mod tidy`**; require **`github.com/otfabric/go-opcua v1.0.2`** (or newer compatible v1).
2. Replace imports **`github.com/otfabric/opcua/...`** with **`github.com/otfabric/go-opcua/...`**.
3. When bulk-replacing paths, **do not** substitute the substring inside **`github.com/otfabric/opcuactl`** (it contains `opcua` and would corrupt the module path).
4. Point release/build scripts at **`main.version` / `main.tag` / `main.commit` / `main.buildDate`** instead of `cmd/opcuactl.*` or `version.txt`.

---
