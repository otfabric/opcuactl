# Examples

Real-world command outputs captured against a **WAGO 750-8102 PFC100 2ETH RS** industrial PLC at `opc.tcp://109.166.142.215:4840`.

Built on [otfabric/go-opcua](https://github.com/otfabric/go-opcua) **v1.5.0** (`opcuactl` **v0.2.1**).

This PLC only offers anonymous / username over `None/None`, and does not support events or history — so the live captures below focus on discovery, browse, read, write dry-run, monitor values, export, and search. For secure auth, IndexRange, event filters, queue overflow, and history modes, use the [syntax examples](#v020-features-not-exercised-on-this-plc) at the end and the workflows in [RECIPES.md](RECIPES.md).

> All captured commands use these connection flags:
> ```bash
> CONN=(
>   -e opc.tcp://109.166.142.215:4840
>   --auth anonymous
>   --security-mode None
>   --security-policy None
> )
> ```
> For brevity the flag block is shown as `"${CONN[@]}"` in the examples below.

---

## Table of Contents

- [Connectivity](#connectivity)
  - [Ping](#ping)
  - [Ping (JSON)](#ping-json)
- [Discovery](#discovery)
  - [Discover Servers](#discover-servers)
  - [Discover Endpoints](#discover-endpoints)
  - [Discover Endpoints — Best](#discover-endpoints-best)
  - [Discover Endpoints — Verbose (Troubleshooting)](#discover-endpoints-verbose-troubleshooting)
- [Browsing](#browsing)
  - [Browse Children (Root)](#browse-children-root)
  - [Browse Children (DeviceSet)](#browse-children-deviceset)
  - [Browse Tree](#browse-tree)
  - [Browse References](#browse-references)
  - [Browse Path](#browse-path)
  - [Browse Path — Qualified (Per-Segment Namespace)](#browse-path-qualified-per-segment-namespace)
  - [Browse Path — Quiet Mode](#browse-path-quiet-mode)
- [Reading Values and Nodes](#reading-values-and-nodes)
  - [Get Value (Single)](#get-value-single)
  - [Get Value (Symbolic Alias)](#get-value-symbolic-alias)
  - [Get Value (Multiple, with Timestamps)](#get-value-multiple-with-timestamps)
  - [Get Value (JSON)](#get-value-json)
  - [Get Value (Quiet Mode)](#get-value-quiet-mode)
  - [Get Node](#get-node)
  - [Get Node (Full, JSON)](#get-node-full-json)
  - [Get Attr](#get-attr)
  - [Get Server](#get-server)
  - [Get Vars (Direct Children)](#get-vars-direct-children)
  - [Get Vars (Metadata Only)](#get-vars-metadata-only)
  - [Get Vars (Quiet Mode)](#get-vars-quiet-mode)
- [Writing](#writing)
  - [Write Value — Dry Run](#write-value-dry-run)
  - [Write Value — Dry Run (JSON)](#write-value-dry-run-json)
- [Monitoring](#monitoring)
  - [Monitor Value (Table)](#monitor-value-table)
  - [Monitor Value (with Sampling Interval)](#monitor-value-with-sampling-interval)
  - [Monitor Value (JSONL, with --count)](#monitor-value-jsonl-with-count)
  - [Monitor Event (Unsupported)](#monitor-event-unsupported)
- [Historical Data](#historical-data)
  - [History Value (Unsupported)](#history-value-unsupported)
- [Export](#export)
  - [Export Nodes (JSON)](#export-nodes-json)
  - [Export Nodes (CSV)](#export-nodes-csv)
  - [Export Values (Table)](#export-values-table)
  - [Export Values (CSV)](#export-values-csv)
- [Searching](#searching)
  - [Find Namespace](#find-namespace)
  - [Find Namespace (Filtered)](#find-namespace-filtered)
  - [Find Namespace (JSON)](#find-namespace-json)
  - [Find Node (by Name)](#find-node-by-name)
  - [Find Node (by Regex)](#find-node-by-regex)
  - [Find Type (by Data Type)](#find-type-by-data-type)
  - [Find Type (by Byte)](#find-type-by-byte)
- [Version](#version)
- [v0.2.0 features (not exercised on this PLC)](#v020-features-not-exercised-on-this-plc)
  - [Secure connect](#secure-connect)
  - [IndexRange and timestamps](#indexrange-and-timestamps)
  - [Monitor queue / overflow / event filters](#monitor-queue-overflow-event-filters)
  - [History modes](#history-modes)
## Connectivity

### Ping

```console
$ opcuactl ping "${CONN[@]}"

  ✓ tcp      181.031875ms
  ✓ secure   1.380848s
  ✓ browse   218.664208ms
```

### Ping (JSON)

```console
$ opcuactl ping "${CONN[@]}" -f json --pretty

[
  {
    "stage": "tcp",
    "status": "ok",
    "latency": "179.893334ms"
  },
  {
    "stage": "secure",
    "status": "ok",
    "latency": "1.31730875s"
  },
  {
    "stage": "browse",
    "status": "ok",
    "latency": "218.091ms"
  }
]
```

> **Ping** tests connectivity in three stages: raw TCP dial, SecureChannel + Session creation, and a test browse of the Objects folder. Each stage reports pass/fail with latency.

---

## Discovery

### Discover Servers

```console
$ opcuactl discover servers "${CONN[@]}"

APPLICATION NAME  APPLICATION URI                                                      TYPE                   DISCOVERY URLS
OPCUAServer@PUT4  urn:PUT4:WAGO:WAGO%20750-8102%20PFC100%202ETH%20RS%20T:OPCUA:Server  ApplicationTypeServer  opc.tcp://109.166.142.215:4840
```

### Discover Endpoints

```console
$ opcuactl discover endpoints "${CONN[@]}"

ENDPOINT URL         SECURITY POLICY  SECURITY MODE            LEVEL  CERT  TOKEN TYPES
opc.tcp://PUT4:4840  None             MessageSecurityModeNone  0      no    UserTokenTypeAnonymous, UserTokenTypeUserName
```

### Discover Endpoints — Best

```console
$ opcuactl discover endpoints "${CONN[@]}" --best

ENDPOINT URL         SECURITY POLICY  SECURITY MODE            LEVEL
opc.tcp://PUT4:4840  None             MessageSecurityModeNone  0
```

> **`--best`** selects the endpoint with the highest security level. On servers that only offer `None/None` this returns the single available endpoint.

### Discover Endpoints — Verbose (Troubleshooting)

```console
$ opcuactl discover endpoints "${CONN[@]}" -v -f json --pretty

[
  {
    "endpointUrl": "opc.tcp://PUT4:4840",
    "securityPolicy": "None",
    "securityMode": "MessageSecurityModeNone",
    "securityLevel": 0,
    "hasCertificate": false,
    "userTokenPolicies": [
      {
        "policyId": "Anonymous",
        "tokenType": "UserTokenTypeAnonymous"
      },
      {
        "policyId": "UserPassword",
        "tokenType": "UserTokenTypeUserName"
      }
    ],
    "transportProfile": "http://opcfoundation.org/UA-Profile/Transport/uatcp-uasc-uabinary",
    "applicationUri": "urn:PUT4:WAGO:WAGO%20750-8102%20PFC100%202ETH%20RS%20T:OPCUA:Server",
    "urlMismatch": "dial=109.166.142.215 ep=PUT4"
  }
]
```

> **Verbose mode** adds `applicationUri`, `certThumbprint` (SHA-256, when a certificate is present), and `urlMismatch` fields. The URL mismatch here shows that we connected to `109.166.142.215` but the server advertises its endpoint as `PUT4` — a common situation with PLCs that use hostnames internally.

---

## Browsing

### Browse Children (Root)

```console
$ opcuactl browse children "${CONN[@]}"

NODE ID      CLASS            BROWSE NAME  DISPLAY NAME  TYPE DEFINITION
ns=2;i=5001  NodeClassObject  2:DeviceSet  DeviceSet     i=58
i=2253       NodeClassObject  0:Server     Server        i=2004
```

### Browse Children (DeviceSet)

```console
$ opcuactl browse children "${CONN[@]}" -n "ns=2;i=5001"

NODE ID                                     CLASS            BROWSE NAME                       DISPLAY NAME                    TYPE DEFINITION
ns=4;s=|plc|WAGO 750-8102 PFC100 2ETH RS T  NodeClassObject  4:WAGO 750-8102 PFC100 2ETH RS T  WAGO 750-8102 PFC100 2ETH RS T  ns=2;i=1001
```

### Browse Tree

```console
$ opcuactl browse tree "${CONN[@]}" --depth 3

DeviceSet (ns=2;i=5001) [NodeClassObject]
  WAGO 750-8102 PFC100 2ETH RS T (ns=4;s=|plc|WAGO 750-8102 PFC100 2ETH RS T) [NodeClassObject]
    Resources (ns=4;i=1001) [NodeClassObject]
      Application (ns=4;s=|var|WAGO 750-8102 PFC100 2ETH RS T.Application) [NodeClassObject]
Server (i=2253) [NodeClassObject]
  ServerRedundancy (i=2296) [NodeClassObject]
    ServerUriArray (i=11314) [NodeClassVariable]
    RedundantServerArray (i=11313) [NodeClassVariable]
    CurrentServerId (i=11312) [NodeClassVariable]
    RedundancySupport (i=3709) [NodeClassVariable]
  VendorServerInfo (i=2295) [NodeClassObject]
  ServerDiagnostics (i=2274) [NodeClassObject]
    EnabledFlag (i=2294) [NodeClassVariable]
    SessionDiagnosticsArray (i=3706) [NodeClassObject]
    SubscriptionDiagnosticsArray (i=2290) [NodeClassVariable]
    ServerDiagnosticsSummary (i=2275) [NodeClassVariable]
      RejectedRequestsCount (i=2288) [NodeClassVariable]
      SecurityRejectedRequestsCount (i=2287) [NodeClassVariable]
      CumulatedSubscriptionCount (i=2286) [NodeClassVariable]
      CurrentSubscriptionCount (i=2285) [NodeClassVariable]
      PublishingIntervalCount (i=2284) [NodeClassVariable]
      SessionAbortCount (i=2282) [NodeClassVariable]
      SessionTimeoutCount (i=2281) [NodeClassVariable]
      RejectedSessionCount (i=3705) [NodeClassVariable]
      SecurityRejectedSessionCount (i=2279) [NodeClassVariable]
      CumulatedSessionCount (i=2278) [NodeClassVariable]
      CurrentSessionCount (i=2277) [NodeClassVariable]
      ServerViewCount (i=2276) [NodeClassVariable]
  ServerCapabilities (i=2268) [NodeClassObject]
    AggregateFunctions (i=2997) [NodeClassObject]
    ModellingRules (i=2996) [NodeClassObject]
    OperationLimits (i=11704) [NodeClassObject]
      MaxNodesPerWrite (i=11707) [NodeClassVariable]
      MaxNodesPerTranslateBrowsePathsToNodeIds (i=11712) [NodeClassVariable]
      MaxNodesPerRegisterNodes (i=11711) [NodeClassVariable]
      MaxNodesPerRead (i=11705) [NodeClassVariable]
      MaxNodesPerBrowse (i=11710) [NodeClassVariable]
      MaxMonitoredItemsPerCall (i=11714) [NodeClassVariable]
    SoftwareCertificates (i=3704) [NodeClassVariable]
    MaxHistoryContinuationPoints (i=2737) [NodeClassVariable]
    MaxQueryContinuationPoints (i=2736) [NodeClassVariable]
    MaxBrowseContinuationPoints (i=2735) [NodeClassVariable]
    MinSupportedSampleRate (i=2272) [NodeClassVariable]
    LocaleIdArray (i=2271) [NodeClassVariable]
    ServerProfileArray (i=2269) [NodeClassVariable]
  Auditing (i=2994) [NodeClassVariable]
  ServiceLevel (i=2267) [NodeClassVariable]
  ServerStatus (i=2256) [NodeClassVariable]
    ShutdownReason (i=2993) [NodeClassVariable]
    SecondsTillShutdown (i=2992) [NodeClassVariable]
    BuildInfo (i=2260) [NodeClassVariable]
      BuildDate (i=2266) [NodeClassVariable]
      BuildNumber (i=2265) [NodeClassVariable]
      SoftwareVersion (i=2264) [NodeClassVariable]
      ProductName (i=2261) [NodeClassVariable]
      ManufacturerName (i=2263) [NodeClassVariable]
      ProductUri (i=2262) [NodeClassVariable]
    State (i=2259) [NodeClassVariable]
    CurrentTime (i=2258) [NodeClassVariable]
    StartTime (i=2257) [NodeClassVariable]
  NamespaceArray (i=2255) [NodeClassVariable]
  ServerArray (i=2254) [NodeClassVariable]
```

### Browse References

```console
$ opcuactl browse refs "${CONN[@]}" -n i=2253 --direction both

DIRECTION  REFERENCE TYPE     NODE ID                                     BROWSE NAME                       DISPLAY NAME                    CLASS
forward    HasComponent       i=2296                                      0:ServerRedundancy                ServerRedundancy                NodeClassObject
forward    HasComponent       i=2295                                      0:VendorServerInfo                VendorServerInfo                NodeClassObject
forward    HasComponent       i=2274                                      0:ServerDiagnostics               ServerDiagnostics               NodeClassObject
forward    HasComponent       i=2268                                      0:ServerCapabilities              ServerCapabilities              NodeClassObject
forward    HasProperty        i=2994                                      0:Auditing                        Auditing                        NodeClassVariable
forward    HasProperty        i=2267                                      0:ServiceLevel                    ServiceLevel                    NodeClassVariable
forward    HasComponent       i=2256                                      0:ServerStatus                    ServerStatus                    NodeClassVariable
forward    HasProperty        i=2255                                      0:NamespaceArray                  NamespaceArray                  NodeClassVariable
forward    HasProperty        i=2254                                      0:ServerArray                     ServerArray                     NodeClassVariable
forward    HasTypeDefinition  i=2004                                      0:ServerType                      ServerType                      NodeClassObjectType
forward    HasNotifier        ns=4;s=|plc|WAGO 750-8102 PFC100 2ETH RS T  4:WAGO 750-8102 PFC100 2ETH RS T  WAGO 750-8102 PFC100 2ETH RS T  NodeClassObject
```

### Browse Path

```console
$ opcuactl browse path "${CONN[@]}" --path Server.ServerStatus

PATH                 NODE ID
Server.ServerStatus  i=2256
```

### Browse Path — Qualified (Per-Segment Namespace)

```console
$ opcuactl browse path "${CONN[@]}" --path "0:Server.0:ServerStatus.0:CurrentTime"

PATH                                   NODE ID
0:Server.0:ServerStatus.0:CurrentTime  i=2258
```

### Browse Path — Quiet Mode

```console
$ opcuactl browse path "${CONN[@]}" --path Server.ServerStatus --quiet

i=2256
```

> **`--quiet`** emits only the resolved NodeID — useful for scripting, e.g. `NODE_ID=$(opcuactl browse path ... --quiet)`.

---

## Reading Values and Nodes

### Get Value (Single)

```console
$ opcuactl get value "${CONN[@]}" -n i=2258

NODE ID  VALUE                              TYPE            STATUS
i=2258   2026-01-10 12:01:47.925 +0000 UTC  TypeIDDateTime  Good
```

### Get Value (Symbolic Alias)

```console
$ opcuactl get value "${CONN[@]}" -n CurrentTime

NODE ID  VALUE                              TYPE            STATUS
i=2258   2026-01-10 12:02:17.406 +0000 UTC  TypeIDDateTime  Good
```

> Standard OPC UA nodes can be referenced by name instead of NodeID: `CurrentTime`, `ServerStatus`, `Server`, etc.

### Get Value (Multiple, with Timestamps)

```console
$ opcuactl get value "${CONN[@]}" -n i=2258 -n i=2255 --timestamps

NODE ID  VALUE                                                                                                                                                                                                TYPE            STATUS  SOURCE TIMESTAMP          SERVER TIMESTAMP
i=2258   2026-01-10 12:01:56.927 +0000 UTC                                                                                                                                                                    TypeIDDateTime  Good    2026-01-10T12:01:56.927Z  2026-01-10T12:01:56.927Z
i=2255   http://opcfoundation.org/UA/, urn:PUT4:WAGO:WAGO%20750-8102%20PFC100%202ETH%20RS%20T:OPCUA:Server, http://opcfoundation.org/UA/DI/, http://PLCopen.org/OpcUa/IEC61131-3/, CODESYSSPV3/3S/IecVarAccess  TypeIDString    Good    2026-01-10T12:01:56.928Z  2026-01-10T12:01:56.928Z
```

> **Array values** (like `NamespaceArray` above) are rendered as comma-separated elements.

### Get Value (JSON)

```console
$ opcuactl get value "${CONN[@]}" -n i=2258 -f json --pretty

{
  "nodeId": "i=2258",
  "value": "2026-01-10T12:02:04.922Z",
  "type": "TypeIDDateTime",
  "status": "Good",
  "statusCode": 0
}
```

### Get Value (Quiet Mode)

```console
$ opcuactl get value "${CONN[@]}" -n i=2258 --quiet

2026-01-10 12:02:11.086 +0000 UTC
```

> **`--quiet`** outputs only the value — no headers, no table chrome. Ideal for piping to other tools.

### Get Node

```console
$ opcuactl get node "${CONN[@]}" -n i=2258

NODE ID  CLASS              BROWSE NAME    DISPLAY NAME  DATA TYPE  VALUE
i=2258   NodeClassVariable  0:CurrentTime  CurrentTime   UtcTime    2026-01-10 12:02:23.625 +0000 UTC
```

> **Data types** are shown as friendly names (`UtcTime`, `Boolean`, `Byte`, `String`) instead of raw NodeIDs like `i=294`.

### Get Node (Full, JSON)

```console
$ opcuactl get node "${CONN[@]}" -n i=2258 --full -f json --pretty

{
  "nodeId": "i=2258",
  "nodeClass": "NodeClassVariable",
  "browseName": "0:CurrentTime",
  "displayName": "CurrentTime",
  "dataType": "UtcTime",
  "accessLevel": "AccessLevelTypeCurrentRead",
  "accessInterpretation": "readable",
  "userAccessLevel": "AccessLevelTypeCurrentRead",
  "typeDefinition": "i=63",
  "value": "2026-01-10T12:02:32.223Z",
  "valueStatus": "Good",
  "valueRank": "-1",
  "historizing": "false"
}
```

> **`--full`** adds extended attributes: `accessInterpretation` (human-readable access summary: "readable", "writable", "read-write"), friendly `dataType` name, and shortened status codes.

### Get Attr

```console
$ opcuactl get attr "${CONN[@]}" -n i=2258 --attr Value --attr DataType --attr BrowseName

NODE ID  ATTRIBUTE   VALUE                              STATUS
i=2258   Value       2026-01-10 12:02:39.828 +0000 UTC  Good
i=2258   DataType    i=294                              Good
i=2258   BrowseName  0:CurrentTime                      Good
```

### Get Server

```console
$ opcuactl get server "${CONN[@]}"

Server Status
  State:          ServerStateRunning
  Start Time:     2025-12-22T09:26:55.418Z
  Current Time:   2026-01-10T12:02:46.303Z
  Security:       None / MessageSecurityModeNone
  Product:        WAGO 750-8102 PFC100 2ETH RS T 3.5.13.20
  Manufacturer:   WAGO
  Build:          3.5.13.20 (2019-05-07T09:38:51Z)
```

### Get Vars (Direct Children)

```console
$ opcuactl get vars "${CONN[@]}" -n "ns=4;s=|var|CODESYS Control for Linux SL.Application"

NODE ID                                                                    BROWSE NAME       DATA TYPE  VALUE  STATUS
ns=4;s=|var|CODESYS Control for Linux SL.Application.PLC_PRG.byCounter     3:byCounter       Byte       0      Good
ns=4;s=|var|CODESYS Control for Linux SL.Application.PLC_PRG.udiCounter    3:udiCounter      UInt32     0      Good
ns=4;s=|var|CODESYS Control for Linux SL.Application.PLC_PRG.xToggleBit    3:xToggleBit      Boolean    false  Good
```

> **`get vars`** collects all Variable children of a node and batch-reads their values, data types, and status codes using `ReadMulti`. Much more efficient than individual reads for large sets.

### Get Vars (Metadata Only)

```console
$ opcuactl get vars "${CONN[@]}" -n "ns=4;s=|var|CODESYS Control for Linux SL.Application" --no-read

NODE ID                                                                    BROWSE NAME       DISPLAY NAME  TYPE DEFINITION
ns=4;s=|var|CODESYS Control for Linux SL.Application.PLC_PRG.byCounter     3:byCounter       byCounter     BaseDataVariableType
ns=4;s=|var|CODESYS Control for Linux SL.Application.PLC_PRG.udiCounter    3:udiCounter      udiCounter    BaseDataVariableType
ns=4;s=|var|CODESYS Control for Linux SL.Application.PLC_PRG.xToggleBit    3:xToggleBit      xToggleBit    BaseDataVariableType
```

> **`--no-read`** skips value reads and shows metadata: NodeID, browse name, display name, and resolved type definition (e.g. "BaseDataVariableType" instead of "i=63").

### Get Vars (Quiet Mode)

```console
$ opcuactl get vars "${CONN[@]}" -n "ns=4;s=|var|CODESYS Control for Linux SL.Application" -q

ns=4;s=|var|CODESYS Control for Linux SL.Application.PLC_PRG.byCounter
ns=4;s=|var|CODESYS Control for Linux SL.Application.PLC_PRG.udiCounter
ns=4;s=|var|CODESYS Control for Linux SL.Application.PLC_PRG.xToggleBit
```

> **`-q`** (quiet) outputs only NodeIDs — useful for piping to other commands.

---

## Writing

### Write Value — Dry Run

```console
$ opcuactl write value "${CONN[@]}" -n i=2258 --value "2026-01-10T12:00:00Z" --type datetime --dry-run

NODE ID  DISPLAY NAME  DATA TYPE  ACCESS    CURRENT VALUE                      NEW VALUE                      NEW TYPE  STATUS
i=2258   CurrentTime   UtcTime    readable  2026-01-10 12:02:53.523 +0000 UTC  2026-01-10 12:00:00 +0000 UTC  datetime  DryRun
```

### Write Value — Dry Run (JSON)

```console
$ opcuactl write value "${CONN[@]}" -n i=2258 --value "2026-01-10T12:00:00Z" --type datetime --dry-run -f json --pretty

{
  "nodeId": "i=2258",
  "displayName": "CurrentTime",
  "dataType": "UtcTime",
  "accessLevel": "readable",
  "currentValue": "2026-01-10T12:03:00.707Z",
  "newValue": "2026-01-10T12:00:00Z",
  "newType": "datetime",
  "status": "DryRun"
}
```

> **`--dry-run`** reads the node's summary (display name, data type, access level) and current value, then shows what *would* be written — without actually writing. Useful for validating before committing changes.

---

## Monitoring

### Monitor Value (Table)

```console
$ opcuactl monitor value "${CONN[@]}" -n i=2258 --duration 3s --interval 1s

2026-01-10T12:03:15.59Z   i=2258  2026-01-10 12:03:15.59 +0000 UTC   [Good]
2026-01-10T12:03:15.699Z  i=2258  2026-01-10 12:03:15.699 +0000 UTC  [Good]
2026-01-10T12:03:15.807Z  i=2258  2026-01-10 12:03:15.807 +0000 UTC  [Good]
2026-01-10T12:03:15.918Z  i=2258  2026-01-10 12:03:15.918 +0000 UTC  [Good]
2026-01-10T12:03:16.026Z  i=2258  2026-01-10 12:03:16.026 +0000 UTC  [Good]
...
```

> Without `--sampling`, the server delivers data changes at its own default sampling rate (~110 ms). Use `--sampling` to control the server-side sampling rate independently of the publishing interval.

### Monitor Value (with Sampling Interval)

```console
$ opcuactl monitor value "${CONN[@]}" -n i=2258 --duration 3s --interval 1s --sampling 1s

2026-01-10T12:03:26.728Z  i=2258  2026-01-10 12:03:26.728 +0000 UTC  [Good]
2026-01-10T12:03:27.739Z  i=2258  2026-01-10 12:03:27.739 +0000 UTC  [Good]
2026-01-10T12:03:28.748Z  i=2258  2026-01-10 12:03:28.748 +0000 UTC  [Good]
```

> With `--sampling 1s` the server samples once per second — 3 notifications in 3 s instead of ~26 without it.

### Monitor Value (JSONL, with --count)

```console
$ opcuactl monitor value "${CONN[@]}" -n i=2258 --count 3 --jsonl

{"ts":"2026-03-11T21:52:09.858247+01:00","nodeId":"i=2258","value":"2026-01-10T12:03:37.31Z","type":"TypeIDDateTime","status":"Good","statusCode":0,"sourceTimestamp":"2026-01-10T12:03:37.31Z"}
{"ts":"2026-03-11T21:52:10.073271+01:00","nodeId":"i=2258","value":"2026-01-10T12:03:37.42Z","type":"TypeIDDateTime","status":"Good","statusCode":0,"sourceTimestamp":"2026-01-10T12:03:37.42Z"}
{"ts":"2026-03-11T21:52:10.073322+01:00","nodeId":"i=2258","value":"2026-01-10T12:03:37.53Z","type":"TypeIDDateTime","status":"Good","statusCode":0,"sourceTimestamp":"2026-01-10T12:03:37.53Z"}
```

### Monitor Event (Unsupported)

```console
$ opcuactl monitor event "${CONN[@]}" -n i=2253 --duration 5s

Error: starting event subscription: connection closed while creating monitored items (server may not support event or alarm subscriptions): EOF
```

> This server does not support event subscriptions. The error message includes the likely cause.

---

## Historical Data

### History Value (Unsupported)

```console
$ opcuactl history value "${CONN[@]}" -n i=2258 --start 2026-01-10T00:00:00Z --end 2026-01-10T11:00:00Z

Error: reading history for i=2258: The server does not support the requested service. StatusBadServiceUnsupported (0x800B0000)
```

> This server does not support historical data access.

---

## Export

### Export Nodes (JSON)

```console
$ opcuactl export nodes "${CONN[@]}" --depth 2 -f json --pretty

[
  {
    "nodeId": "i=85",
    "nodeClass": "NodeClassObject",
    "browseName": "0:Objects",
    "displayName": "Objects",
    "accessLevel": "AccessLevelTypeNone",
    "userAccessLevel": "AccessLevelTypeNone",
    "typeDefinition": "i=61",
    "depth": 0
  },
  {
    "nodeId": "ns=2;i=5001",
    "nodeClass": "NodeClassObject",
    "browseName": "2:DeviceSet",
    "displayName": "DeviceSet",
    "accessLevel": "AccessLevelTypeNone",
    "userAccessLevel": "AccessLevelTypeNone",
    "typeDefinition": "i=58",
    "depth": 1
  },
  ...
]
```

### Export Nodes (CSV)

```console
$ opcuactl export nodes "${CONN[@]}" --depth 2 -f csv

NODE ID,NODE CLASS,DISPLAY NAME,DATA TYPE,ACCESS,DEPTH
i=85,NodeClassObject,Objects,,AccessLevelTypeNone,0
ns=2;i=5001,NodeClassObject,DeviceSet,,AccessLevelTypeNone,1
ns=4;s=|plc|WAGO 750-8102 PFC100 2ETH RS T,NodeClassObject,WAGO 750-8102 PFC100 2ETH RS T,,AccessLevelTypeNone,2
i=2253,NodeClassObject,Server,,AccessLevelTypeNone,1
i=2296,NodeClassObject,ServerRedundancy,,AccessLevelTypeNone,2
i=2295,NodeClassObject,VendorServerInfo,,AccessLevelTypeNone,2
i=2274,NodeClassObject,ServerDiagnostics,,AccessLevelTypeNone,2
i=2268,NodeClassObject,ServerCapabilities,,AccessLevelTypeNone,2
i=2994,NodeClassVariable,Auditing,Boolean,AccessLevelTypeCurrentRead,2
i=2267,NodeClassVariable,ServiceLevel,Byte,AccessLevelTypeCurrentRead,2
i=2256,NodeClassVariable,ServerStatus,ServerStatusDataType,AccessLevelTypeCurrentRead,2
i=2255,NodeClassVariable,NamespaceArray,String,AccessLevelTypeCurrentRead,2
i=2254,NodeClassVariable,ServerArray,String,AccessLevelTypeCurrentRead,2
```

> **Data types** in CSV export show friendly names (`Boolean`, `Byte`, `ServerStatusDataType`, `String`) instead of raw NodeIDs.

### Export Values (Table)

```console
$ opcuactl export values "${CONN[@]}" -n i=2258 -n i=2255

NODE ID  VALUE                                                                                                                                                                                                TYPE            STATUS  SOURCE TIMESTAMP
i=2258   2026-01-10 12:04:32.587 +0000 UTC                                                                                                                                                                    TypeIDDateTime  Good    2026-01-10T12:04:32.587Z
i=2255   http://opcfoundation.org/UA/, urn:PUT4:WAGO:WAGO%20750-8102%20PFC100%202ETH%20RS%20T:OPCUA:Server, http://opcfoundation.org/UA/DI/, http://PLCopen.org/OpcUa/IEC61131-3/, CODESYSSPV3/3S/IecVarAccess  TypeIDString    Good    2026-01-10T12:04:32.587Z
```

### Export Values (CSV)

```console
$ opcuactl export values "${CONN[@]}" -n i=2258 -n i=2267 -f csv

NODE ID,VALUE,TYPE,STATUS,SOURCE TIMESTAMP
i=2258,2026-01-10 12:04:39.303 +0000 UTC,TypeIDDateTime,Good,2026-01-10T12:04:39.303Z
i=2267,200,TypeIDByte,Good,2026-01-10T12:04:39.303Z
```

---

## Searching

### Find Namespace

```console
$ opcuactl find namespace "${CONN[@]}"

INDEX  NAMESPACE URI
0      http://opcfoundation.org/UA/
1      urn:PUT4:WAGO:WAGO%20750-8102%20PFC100%202ETH%20RS%20T:OPCUA:Server
2      http://opcfoundation.org/UA/DI/
3      http://PLCopen.org/OpcUa/IEC61131-3/
4      CODESYSSPV3/3S/IecVarAccess
```

### Find Namespace (Filtered)

```console
$ opcuactl find namespace "${CONN[@]}" --filter "WAGO"

INDEX  NAMESPACE URI
1      urn:PUT4:WAGO:WAGO%20750-8102%20PFC100%202ETH%20RS%20T:OPCUA:Server
```

### Find Namespace (JSON)

```console
$ opcuactl find namespace "${CONN[@]}" -f json --pretty

[
  {
    "index": 0,
    "uri": "http://opcfoundation.org/UA/"
  },
  {
    "index": 1,
    "uri": "urn:PUT4:WAGO:WAGO%20750-8102%20PFC100%202ETH%20RS%20T:OPCUA:Server"
  },
  {
    "index": 2,
    "uri": "http://opcfoundation.org/UA/DI/"
  },
  {
    "index": 3,
    "uri": "http://PLCopen.org/OpcUa/IEC61131-3/"
  },
  {
    "index": 4,
    "uri": "CODESYSSPV3/3S/IecVarAccess"
  }
]
```

### Find Node (by Name)

```console
$ opcuactl find node "${CONN[@]}" --name CurrentTime --depth 5 --limit 3

NODE ID  CLASS              BROWSE NAME    DISPLAY NAME  TYPE DEFINITION
i=2258   NodeClassVariable  0:CurrentTime  CurrentTime   i=63
```

### Find Node (by Regex)

```console
$ opcuactl find node "${CONN[@]}" --name "Server.*" --regex --depth 3 --limit 5

NODE ID  CLASS              BROWSE NAME             DISPLAY NAME          TYPE DEFINITION
i=2253   NodeClassObject    0:Server                Server                i=2004
i=2296   NodeClassObject    0:ServerRedundancy      ServerRedundancy      i=2138
i=11314  NodeClassVariable  0:ServerUriArray        ServerUriArray        i=68
i=11313  NodeClassVariable  0:RedundantServerArray  RedundantServerArray  i=68
i=11312  NodeClassVariable  0:CurrentServerId       CurrentServerId       i=68
```

### Find Type (by Data Type)

```console
$ opcuactl find type "${CONN[@]}" --data-type Boolean --depth 3 --limit 5

NODE ID  CLASS              DISPLAY NAME  DATA TYPE  TYPE DEFINITION
i=2294   NodeClassVariable  EnabledFlag   Boolean    i=68
i=2994   NodeClassVariable  Auditing      Boolean    i=68
```

> **`--data-type`** accepts friendly type names (`Boolean`, `Byte`, `String`, `Double`, etc.) in addition to raw NodeIDs.

### Find Type (by Byte)

```console
$ opcuactl find type "${CONN[@]}" --data-type Byte --depth 3 --limit 5

NODE ID  CLASS              DISPLAY NAME  DATA TYPE  TYPE DEFINITION
i=2267   NodeClassVariable  ServiceLevel  Byte       i=68
```

---

## Version

```console
$ opcuactl version

opcuactl dev
  commit:     none
  built:      unknown
  go version: go1.26.1
```

With a release build, `tag` appears when `main.tag` is set at link time.

---

## v0.2.0 features (not exercised on this PLC)

These commands match the current CLI. They are **not** live captures from the WAGO above — use them as copy-paste templates against a server that supports the feature.

### Secure connect

```sh
# Inspect token types / policies first
opcuactl discover endpoints -e opc.tcp://secure-host:4840 --best -v

# Username + auto endpoint selection
opcuactl get value -e opc.tcp://secure-host:4840 -n i=2258 \
  --auth username --username operator --password "$OPCUACTL_PASSWORD" \
  --security-policy auto --security-mode auto \
  --cert client.pem --key client.key \
  --trusted-cert server-ca.pem

# X.509 user identity
opcuactl get value -e opc.tcp://secure-host:4840 -n i=2258 \
  --auth certificate --cert client.pem --key client.key \
  --security-policy auto --security-mode auto \
  --trusted-cert server-ca.pem

# Issued token
opcuactl get value -e opc.tcp://secure-host:4840 -n i=2258 \
  --auth issued --token-file ./token.jwt \
  --security-policy auto --security-mode auto \
  --cert client.pem --key client.key
```

### IndexRange and timestamps

```sh
opcuactl get value "${CONN[@]}" -n "ns=2;s=ArrayNode" --index-range "0:2"
opcuactl get value "${CONN[@]}" -n i=2258 \
  --timestamps-to-return source --timestamps
```

### Monitor queue / overflow / event filters

```sh
opcuactl monitor value "${CONN[@]}" -n i=2258 \
  --interval 1s --sampling 100ms --queue-size 10 --discard-oldest --jsonl --count 20
# JSONL lines may include "overflow": true when the server sets the Overflow InfoBit

opcuactl monitor event -e opc.tcp://events-host:4840 -n i=2253 \
  --of-type i=2041 --severity-min 500 --severity-max 1000 \
  --jsonl --duration 30s
```

### History modes

```sh
opcuactl history value -e opc.tcp://historian:4840 -n "ns=2;s=Temperature" \
  --start 2026-03-10T00:00:00Z --end 2026-03-11T00:00:00Z \
  --mode processed --interval 1h --aggregate average

opcuactl history value -e opc.tcp://historian:4840 -n "ns=2;s=Temperature" \
  --mode attime --at 2026-03-10T12:00:00Z --at 2026-03-10T13:00:00Z

opcuactl export values -e opc.tcp://historian:4840 -n "ns=2;s=Temperature" \
  --history --mode attime \
  --at 2026-03-10T12:00:00Z --at 2026-03-10T13:00:00Z \
  -f csv --output history-attime.csv
```
