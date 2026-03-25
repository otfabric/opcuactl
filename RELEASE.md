# opcuactl Releases

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
