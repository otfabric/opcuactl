# AGENTS.md — opcuactl

## What this is

Command-line client and test server for OPC UA built on [go-opcua](https://github.com/otfabric/go-opcua): discovery, browsing, reads and writes, methods, subscriptions, alarms, history, GDS, PubSub, `serve`. Pure Go, one static binary. Profile: **cli**.

## Organisation standards

This repository follows **otfabric/engineering policy 1.0**, profile **cli** (`.otfabric/policy.yaml`, mode `baseline`). Where this file and the organisation standards disagree, this file wins here — say so in your report.

## Where the truth lives

| Question | Source |
|---|---|
| Exit codes (script-facing API) | docs/exitcodes.md |
| Commands and flags | README.md |
| Worked examples | EXAMPLES.md, RECIPES.md |
| Security guidance | SECURITY.md |
| OPC UA behaviour | go-opcua |
| Release history | RELEASE.md (`## vX.Y.Z` … `---`) |

## Hard rules

1. Exit codes and output formats are a public contract: changing a code or a field is a breaking change with a RELEASE.md note.
2. Never lower the CI `coverage-min`, add lint suppressions, or skip or delete tests to make CI pass.
3. go-opcua is pinned to a released tag; no `replace` directives.
4. Tests live next to the commands in `cmd/opcuactl`; the end-to-end tests there run against the in-process server, so keep `cmd/` in CI.

## Commands

| When you change… | Run |
|---|---|
| anything | `make check` |
| a single test run | `make test` |

## Definition of Done

`make check` green; tests for new commands; README.md, EXAMPLES.md or RECIPES.md updated for new flags; RELEASE.md section for user-visible changes.

## Git and releases

Branches and PRs. Do not push to `main`, tag or run the Release workflow unless the maintainer asks in the session.
