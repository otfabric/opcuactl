# opcuactl — Implementation Progress

## Phase 1 ✅

All Phase 1 commands implemented, building, and tested.

### Commands

| Command               | Status | Notes                                                    |
|-----------------------|--------|----------------------------------------------------------|
| `version`             | ✅     | Table + JSON/YAML output; ldflags for version/commit     |
| `discover servers`    | ✅     | FindServers + FindServersOnNetwork (--on-network)        |
| `discover endpoints`  | ✅     | GetEndpoints + SelectEndpoint (--best)                   |
| `get value`           | ✅     | ReadValue / ReadValues; --timestamps; multi-node         |
| `get node`            | ✅     | Node.Summary(); multi-node                               |
| `get server`          | ✅     | ServerStatus + BuildInfo + security info                 |
| `browse children`     | ✅     | HierarchicalReferences; --node-class filter              |
| `browse refs`         | ✅     | Full reference browsing; direction/subtypes/class filter |

### Infrastructure

| Component          | Status | File(s)                           |
|--------------------|--------|-----------------------------------|
| Go module          | ✅     | go.mod, go.sum                    |
| Entry point        | ✅     | main.go                           |
| Root command       | ✅     | cmd/opcuactl/root.go              |
| Exit codes         | ✅     | cmd/opcuactl/errors.go            |
| Connection layer   | ✅     | internal/conn/conn.go             |
| Output formatting  | ✅     | internal/format/format.go         |
| Node ID parsing    | ✅     | internal/parse/parse.go           |
| Output models      | ✅     | internal/model/model.go           |

### Global Flags

All per-spec: `--endpoint`, `--security-policy`, `--security-mode`, `--auth`,
`--username`, `--password`, `--cert`, `--key`, `--server-cert`, `--timeout`,
`--format`, `--pretty`, `--verbose`. Environment variable fallbacks via
`OPCUACTL_*` prefix.

### Output Formats

Table (default), JSON, YAML. `--pretty` for indented JSON.

### Exit Codes

| Code | Meaning    |
|------|------------|
| 0    | Success    |
| 1    | Usage      |
| 2    | Connection |
| 3    | Service    |
| 4    | Partial    |

### Tests

- `internal/parse`: NodeID, NodeIDs, SecurityPolicy, SecurityPolicyShortName — **PASS**
- `internal/format`: ParseFormat, WriterJSON, WriterJSONPretty, WriteTable — **PASS**

### Dependencies

- `github.com/otfabric/opcua` v0.1.2
- `github.com/spf13/cobra` v1.10.2
- `gopkg.in/yaml.v3`

---

## Phase 2 — Not started

- `browse tree`
- `find namespace`
- `find node`
- `write value`
- `call method`

## Phase 3 — Not started

- `monitor value`
- `monitor event`
- `history value`
- `export nodes`

## Phase 4 — Not started

- `browse path`
- `find type`
- `history event`
- `export values`
- `monitor alarm`
