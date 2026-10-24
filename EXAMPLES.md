# Examples

Real-world command outputs captured against a **WAGO 750-8102 PFC100 2ETH RS** industrial PLC at `opc.tcp://109.166.142.215:4840`.

Built on [otfabric/go-opcua](https://github.com/otfabric/go-opcua) **v1.6.0** (`opcuactl` **v0.3.0**).

This PLC only offers anonymous / username over `None/None`, and does not support events or history — so the live captures below focus on discovery, browse, read, write dry-run, monitor values, export, and search. For secure auth, IndexRange, event filters, queue overflow, and history modes, use the [syntax examples](#v020-features-not-exercised-on-this-plc) at the end and the workflows in [RECIPES.md](RECIPES.md). The [v0.3.0 features](#v030-features-captured-against-a-go-opcua-server) — ECC security policies, reverse connect, Local Discovery Server, data-change filters and the OPC UA JSON / XML encodings — are live captures against a go-opcua v1.6.0 demo server. The [v0.4.0 PubSub features](#v040-features-captured-against-a-go-opcua-server) are live captures against the go-opcua v1.7.0 PubSub example server. The [v0.5.0 PubSub features](#v050-features-captured-against-brokers-and-a-go-opcua-server) (MQTT 5.0, AMQP 1.0, Ethernet, chunks, discovery and diagnostics) are live captures against real brokers, Linux containers and the go-opcua v1.9.0 PubSub example server.

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
- [v0.5.0 features (captured against brokers and a go-opcua server)](#v050-features-captured-against-brokers-and-a-go-opcua-server)
  - [MQTT 5.0](#mqtt-50)
  - [AMQP 1.0: RabbitMQ and Artemis](#amqp-10-rabbitmq-and-artemis)
  - [Ethernet](#ethernet)
  - [Chunked NetworkMessages](#chunked-networkmessages)
  - [UADP discovery](#uadp-discovery)
  - [Diagnostics of a server](#diagnostics-of-a-server)
- [v0.4.0 features (captured against a go-opcua server)](#v040-features-captured-against-a-go-opcua-server)
  - [PubSub configuration of a server](#pubsub-configuration-of-a-server)
  - [Subscribe](#subscribe)
  - [Disable, enable and remove](#disable-enable-and-remove)
  - [Publish and decode](#publish-and-decode)
- [v0.3.0 features (captured against a go-opcua server)](#v030-features-captured-against-a-go-opcua-server)
  - [ECC security policies](#ecc-security-policies)
  - [Local Discovery Server and mDNS](#local-discovery-server-and-mdns)
  - [Reverse connect](#reverse-connect)
  - [OPC UA JSON and XML encodings](#opc-ua-json-and-xml-encodings)
  - [Data-change filters (deadband)](#data-change-filters-deadband)
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

## v0.5.0 features (captured against brokers and a go-opcua server)

Captured with Eclipse Mosquitto 2 (MQTT 5.0, port 18831), RabbitMQ 4.1.4 (AMQP 1.0, port 15681) and ActiveMQ Artemis 2.42.0 (AMQP 1.0, port 15682) in Docker on `localhost`, raw Ethernet between two Linux containers (Docker Desktop, `CAP_NET_RAW`), UDP on the loopback interface of macOS, and the PubSub example server of go-opcua v1.9.0 (`examples/pubsub/server -port 48400 -pubsub opc.udp://127.0.0.1:47200`).

### MQTT 5.0

The subscriber shows the topic, content type, UAMessageType and (if any) the user properties of each message, and its diagnostics counters on exit. The publisher sends the retained `ua-metadata` message first:

```console
$ opcuactl pubsub subscribe --url mqtt://127.0.0.1:18831 --mqtt-version 5.0 --qos 1 --topic 'plant/#' --show-metadata --count 2 --diagnostics

$ opcuactl pubsub publish --url mqtt://127.0.0.1:18831 --mqtt-version 5.0 --qos 1 --topic plant/line1 --encoding json \
    --publisher-id str:line1 --field Temp:Double=21.5 --field Running:Boolean=true --metadata-topic plant/line1/meta \
    --count 2 --interval 500ms --transport-property "connection-Session Expiry Interval=60"
published 2 message(s), 846 bytes, to mqtt://127.0.0.1:18831 topic plant/line1
```

Output of the subscriber:

```console
metadata pub=line1 writer=1 "DataSet" version=846161288.846161288 topic=plant/line1/meta content-type=application/json ua-message-type=ua-metadata fields: Temp:Double, Running:Boolean
2026-10-24T12:48:08.583143Z  pub=line1 wg=opcuactl writer=1 seq=1 keyframe topic=plant/line1 content-type=application/json ua-message-type=ua-data  Temp=21.5 Running=true
2026-10-24T12:48:09.083364Z  pub=line1 wg=opcuactl writer=1 seq=2 keyframe topic=plant/line1 content-type=application/json ua-message-type=ua-data  Temp=21.5 Running=true
diagnostics reader-group (level Basic): information=3 error=0
  ReceivedNetworkMessages    3
  ChunksReceived             0
  ChunkedMessagesReceived    0
  ChunksDiscarded            0 (error)
  DiscoveryRequestsSent      0
  DiscoveryResponsesReceived 0
```

### AMQP 1.0: RabbitMQ and Artemis

RabbitMQ 4 addresses a queue as `/queues/<name>` (here `plant`, declared beforehand; another vhost would be `--transport-property connection-hostname=vhost:<name>`). The broker password comes from the environment:

```console
$ export OPCUACTL_BROKER_PASSWORD=...
$ opcuactl pubsub subscribe --url amqp://127.0.0.1:15681 --broker-username plant --topic /queues/plant --count 2 --jsonl

$ opcuactl pubsub publish --url amqp://127.0.0.1:15681 --broker-username plant --topic /queues/plant --encoding json \
    --publisher-id 2234 --field Temp:Double=21.5 --count 2 --interval 500ms
published 2 message(s), 772 bytes, to amqp://127.0.0.1:15681 topic /queues/plant
```

```console
{"ts":"2026-10-24T12:48:39.052451Z","topic":"/queues/plant","contentType":"application/json","uaMessageType":"ua-data","encoding":"json","publisherId":"2234","writerGroupName":"opcuactl","dataSetWriterId":1,"messageType":"keyframe","sequenceNumber":1,"timestamp":"2026-10-24T12:48:39.049118Z","metaDataVersion":"846161319.846161319","security":"None","fields":[{"index":0,"name":"Temp","value":21.5,"type":"Double"}]}
{"ts":"2026-10-24T12:48:39.553727Z","topic":"/queues/plant","contentType":"application/json","uaMessageType":"ua-data","encoding":"json","publisherId":"2234","writerGroupName":"opcuactl","dataSetWriterId":1,"messageType":"keyframe","sequenceNumber":2,"timestamp":"2026-10-24T12:48:39.550221Z","metaDataVersion":"846161319.846161319","security":"None","fields":[{"index":0,"name":"Temp","value":21.5,"type":"Double"}]}
```

Artemis takes plain addresses; the capabilities make the address an anycast queue instead of a multicast topic. UADP this time, decoded by its content type:

```console
$ opcuactl pubsub subscribe --url amqp://127.0.0.1:15682 --broker-username plant --topic plant.line1 \
    --transport-property source-capabilities=queue --transport-property target-capabilities=queue --count 1

$ opcuactl pubsub publish --url amqp://127.0.0.1:15682 --broker-username plant --topic plant.line1 \
    --transport-property source-capabilities=queue --transport-property target-capabilities=queue \
    --publisher-id 2234 --writer-group-id 100 --field Temp:Double=21.5
published 1 message(s), 51 bytes, to amqp://127.0.0.1:15682 topic plant.line1
```

```console
2026-10-24T12:48:58.672606Z  pub=2234 wg=100 writer=1 seq=1 keyframe topic=plant.line1 content-type=application/opcua+uadp ua-message-type=ua-data  #0=21.5
```

### Ethernet

UADP frames to the multicast MAC address `01-00-5E-00-00-01`, between two containers on one Docker network:

```console
sub$ opcuactl pubsub subscribe --url opc.eth://01-00-5E-00-00-01 --interface eth0 --count 3 --show-metadata

pub$ opcuactl pubsub publish --url opc.eth://01-00-5E-00-00-01 --interface eth0 --publisher-id 2234 --writer-group-id 100 \
       --field Temp:Double=21.5 --announce-metadata --count 2 --interval 500ms
published 2 message(s), 102 bytes, to opc.eth://01-00-5E-00-00-01
```

```console
metadata pub=2234 writer=1 "DataSet" version=846161446.846161446 fields: Temp:Double
2026-10-24T12:50:46.0635119Z  pub=2234 wg=100 writer=1 seq=1 keyframe  Temp=21.5
2026-10-24T12:50:46.564521Z  pub=2234 wg=100 writer=1 seq=2 keyframe  Temp=21.5
```

Without the raw-socket capability, and without `--interface`:

```console
$ opcuactl pubsub subscribe --url opc.eth://01-00-5E-00-00-01 --interface eth0
Error: pubsub/transport/eth: open AF_PACKET socket: operation not permitted (raw Ethernet access needs CAP_NET_RAW or root); run as root, or on Linux grant the capability: sudo setcap cap_net_raw+ep "$(command -v opcuactl)"

$ opcuactl pubsub subscribe --url opc.eth://01-00-5E-00-00-01
Error: --interface is required with opc.eth:// (the network interface the frames are sent and received on, e.g. eth0)
```

### Chunked NetworkMessages

A 1.6 KB DataSet with `--max-message-size 512` goes out as four chunks, and the subscriber reassembles it. Both print their diagnostics:

```console
$ opcuactl pubsub subscribe --url opc.udp://127.0.0.1:47300 --count 1 --field Readings:String --diagnostics

$ opcuactl pubsub publish --url opc.udp://127.0.0.1:47300 --publisher-id 2234 --writer-group-id 100 \
    --field "Readings:String=$(seq -s, 1 400)" --max-message-size 512 --diagnostics
published 1 message(s), 1649 bytes in 4 chunks, to opc.udp://127.0.0.1:47300
diagnostics writer-group (level Basic): information=8 error=0
  SentNetworkMessages       4
  FailedTransmissions       0 (error)
  ChunksSent                4
  DiscoveryRequestsReceived 0
  DiscoveryRequestsIgnored  0
  DiscoveryResponsesSent    0
```

Output of the subscriber (the field is cut here):

```console
2026-10-24T12:49:12.795243Z  pub=2234 wg=100 writer=1 seq=1 keyframe  Readings=1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,...
diagnostics reader-group (level Basic): information=6 error=0
  ReceivedNetworkMessages    1
  ChunksReceived             4
  ChunkedMessagesReceived    1
  ChunksDiscarded            0 (error)
  DiscoveryRequestsSent      0
  DiscoveryResponsesReceived 0
```

### UADP discovery

A publisher on a multicast group that answers discovery probes:

```console
$ opcuactl pubsub publish --url opc.udp://224.0.0.22:47301 --interface lo0 --publisher-id 2234 --writer-group-id 100 \
    --dataset-name Line1 --field Temp:Double=21.5 --field Running:Boolean=true --count 0 --duration 8s --answer-probes --diagnostics
```

Who publishes on the group, and the DataSetMetaData and writer configuration of writer 1:

```console
$ opcuactl pubsub discover --url opc.udp://224.0.0.22:47301 --interface lo0 --duration 2s
announcement ApplicationInformation pub=2234 seq=1 isCyclic=false state=Operational

$ opcuactl pubsub discover --url opc.udp://224.0.0.22:47301 --interface lo0 --probe metadata,writers --publisher-id 2234 --writer-id 1 --duration 2s
metadata pub=2234 writer=1 "Line1" version=846161352.846161352 fields: Temp:Double, Running:Boolean
announcement DataSetWriterConfiguration pub=2234 seq=3 dataSetWriterIds=[1] dataSetWriters=1:opcuactl(Line1) writerGroup=opcuactl writerGroupId=100
```

A subscriber that probes for the metadata of the writers it sees without one. The first message has unnamed fields; the following ones are named:

```console
$ opcuactl pubsub subscribe --url opc.udp://224.0.0.22:47301 --interface lo0 --probe --count 2 --show-metadata --diagnostics
2026-10-24T12:49:18.817656Z  pub=2234 wg=100 writer=1 seq=7 keyframe  #0=21.5 #1=true
metadata pub=2234 writer=1 "Line1" version=846161352.846161352 fields: Temp:Double, Running:Boolean
2026-10-24T12:49:19.817624Z  pub=2234 wg=100 writer=1 seq=8 keyframe  Temp=21.5 Running=true
diagnostics reader-group (level Basic): information=5 error=0
  ReceivedNetworkMessages    3
  ChunksReceived             0
  ChunkedMessagesReceived    0
  ChunksDiscarded            0 (error)
  DiscoveryRequestsSent      1
  DiscoveryResponsesReceived 1
```

The publisher at the end, after answering the four probes:

```console
published 8 message(s), 424 bytes, to opc.udp://224.0.0.22:47301
diagnostics writer-group (level Basic): information=20 error=0
  SentNetworkMessages       12
  FailedTransmissions       0 (error)
  ChunksSent                0
  DiscoveryRequestsReceived 4
  DiscoveryRequestsIgnored  0
  DiscoveryResponsesSent    4
```

### Diagnostics of a server

The diagnostics objects of the go-opcua PubSub example server, starting with the PublishSubscribe object and its LiveValues:

```console
$ opcuactl pubsub config diagnostics -e opc.tcp://localhost:48400

diagnostics PublishSubscribe "PublishSubscribe" i=14443 (level Basic): information=2 error=0
COUNTER                    VALUE  CLASSIFICATION  LEVEL  ACTIVE  FIRST CHANGE
StateDisabledByMethod      0      Information     Basic  true
StateError                 0      Error           Basic  true
StateOperationalByMethod   0      Information     Basic  true
StateOperationalByParent   1      Information     Basic  true    2026-10-24T12:49:35.02623Z
StateOperationalFromError  0      Information     Basic  true
StatePausedByParent        1      Information     Basic  true    2026-10-24T12:49:35.023142Z

LIVE VALUE                 VALUE
ConfiguredDataSetReaders   1
ConfiguredDataSetWriters   1
OperationalDataSetReaders  1
OperationalDataSetWriters  1
```

A writer group (NodeIds from `pubsub config show`). EncryptionErrors is an Advanced counter, so it is inactive at the Basic level:

```console
$ opcuactl pubsub config diagnostics -e opc.tcp://localhost:48400 --node "ns=2;i=161"

diagnostics writer-group "Publisher" ns=2;i=161 (level Basic): information=32 error=0
COUNTER                    VALUE  CLASSIFICATION  LEVEL     ACTIVE  FIRST CHANGE
EncryptionErrors           -      Error           Advanced  false
FailedTransmissions        0      Error           Basic     true
SentNetworkMessages        30     Information     Basic     true    2026-10-24T12:49:35.026689Z
StateDisabledByMethod      0      Information     Basic     true
StateError                 0      Error           Basic     true
StateOperationalByMethod   0      Information     Basic     true
StateOperationalByParent   1      Information     Basic     true    2026-10-24T12:49:35.026463Z
StateOperationalFromError  0      Information     Basic     true
StatePausedByParent        1      Information     Basic     true    2026-10-24T12:49:35.023166Z

LIVE VALUE                 VALUE
ConfiguredDataSetWriters   1
OperationalDataSetWriters  1
```

`--reset` calls the Reset method first, then reads the counters (abridged):

```console
$ opcuactl pubsub config diagnostics -e opc.tcp://localhost:48400 --node "ns=2;i=161" --reset --format json --pretty
{
  "component": "writer-group",
  "name": "Publisher",
  "nodeId": "ns=2;i=161",
  "diagnosticsLevel": "Basic",
  "totalInformation": 0,
  "totalError": 0,
  "counters": [
    { "name": "EncryptionErrors", "value": 0, "classification": "Error", "diagnosticsLevel": "Advanced", "active": false },
    { "name": "FailedTransmissions", "value": 0, "classification": "Error", "diagnosticsLevel": "Basic", "active": true },
    { "name": "SentNetworkMessages", "value": 0, "classification": "Information", "diagnosticsLevel": "Basic", "active": true },
    ...
  ],
  "liveValues": {
    "ConfiguredDataSetWriters": 1,
    "OperationalDataSetWriters": 1
  }
}
```

### PubSub Actions over UDP

`pubsub respond` and `pubsub call` (go-opcua v1.9.0) on the loopback interface of macOS. The Responder echoes two inputs, serves three requests and prints its diagnostics; the Requestor listens on port 4841 and sends to the Responder on port 4840:

```console
$ opcuactl pubsub respond --url opc.udp://127.0.0.1:4840 --target 1:Echo --input A:Int32 --input B:String --count 3 --diagnostics
```

The calls: text and JSON results, a request with an input of the wrong type (answered by the runtime, exit code 3), a target the Responder does not have (rejected without a response, exit code 2 after `--timeout`), and `--quiet`:

```console
$ opcuactl pubsub call --url opc.udp://127.0.0.1:4841 --request-address opc.udp://127.0.0.1:4840 --resend-interval 200ms --input A:Int32=2 --input B:String=pump --output A:Int32 --output B:String
pub=1 writer=1 target=1 request=1 status=Good duration=1.69ms
  A = 2 (Int32)
  B = pump (String)
$ echo $?
0
$ opcuactl pubsub call --url opc.udp://127.0.0.1:4841 --request-address opc.udp://127.0.0.1:4840 --resend-interval 200ms --input A:Int32=7 --input B:String=valve --format json --pretty
{
  "publisherId": "1",
  "dataSetWriterId": 1,
  "actionTargetId": 1,
  "requestId": 1,
  "status": "Good",
  "statusCode": 0,
  "duration": "637µs",
  "outputs": [
    {
      "index": 0,
      "value": 7,
      "type": "Int32"
    },
    {
      "index": 1,
      "value": "valve",
      "type": "String"
    }
  ]
}
$ echo $?
0
$ opcuactl pubsub call --url opc.udp://127.0.0.1:4841 --request-address opc.udp://127.0.0.1:4840 --resend-interval 200ms --input A:String=two --input B:String=pump
pub=1 writer=1 target=1 request=1 status=BadTypeMismatch duration=630µs
Error: the Action target 1 of writer 1 of publisher 1 answered BadTypeMismatch
$ echo $?
3
$ opcuactl pubsub call --url opc.udp://127.0.0.1:4841 --request-address opc.udp://127.0.0.1:4840 --resend-interval 200ms --target-id 9 --input A:Int32=1 --input B:String=x --timeout 2s --diagnostics
diagnostics requestor (level Basic): information=11 error=1
  ActionRequestsSent      11
  ActionResponsesReceived 0
  ActionTimeouts          1 (error)
Error: no response from the Action target 9 of writer 1 of publisher 1 within 2s
$ echo $?
2
$ opcuactl pubsub call --url opc.udp://127.0.0.1:4841 --request-address opc.udp://127.0.0.1:4840 --resend-interval 200ms --input A:Int32=3 --input B:String=fan -q
3
fan
$ echo $?
0
```

Output of the Responder: the requests its handler served, then the counters (the Executing requests and the Idle acknowledgements count as received; the eleven rejected are the request for target 9 and its resends):

```console
2026-10-24T13:41:14.540642Z  requestor=opcuactl-9cf4c278 writer=1 target=1 request=1 inputs: A=2 B=pump -> Good outputs: A=2 B=pump
2026-10-24T13:41:14.570091Z  requestor=opcuactl-1c0ab936 writer=1 target=1 request=1 inputs: A=7 B=valve -> Good outputs: A=7 B=valve
2026-10-24T13:41:16.657162Z  requestor=opcuactl-8686d2f3 writer=1 target=1 request=1 inputs: A=3 B=fan -> Good outputs: A=3 B=fan
diagnostics responder (level Basic): information=16 error=11
  ActionRequestsReceived 8
  ActionRequestsRejected 11 (error)
  ActionResponsesSent    8
```

### PubSub Actions through MQTT

Through Eclipse Mosquitto 2.0.22 in Docker on port 18883. JSON over MQTT 5.0: a Responder `responder-1` with target 1 "Start" and a fixed output, a subscriber that watches the exchange, and two calls:

```console
$ opcuactl pubsub subscribe --url mqtt://127.0.0.1:18883 --mqtt-version 5.0 --topic 'opcua/#' --count 3

$ opcuactl pubsub respond --url mqtt://127.0.0.1:18883 --mqtt-version 5.0 --encoding json --publisher-id responder-1 --target 1:Start \
    --input Speed:Double --output State:String=running --count 2

$ opcuactl pubsub call --url mqtt://127.0.0.1:18883 --mqtt-version 5.0 --encoding json --publisher-id responder-1 --input Speed:Double=1450 --output State:String
pub=responder-1 writer=1 target=1 request=1 status=Good duration=53.108ms
  State = running (String)
$ echo $?
0
$ opcuactl pubsub call --url mqtt://127.0.0.1:18883 --mqtt-version 5.0 --encoding json --publisher-id responder-1 --input Speed:Double=900 --format json
{"publisherId":"responder-1","dataSetWriterId":1,"actionTargetId":1,"requestId":1,"status":"Good","statusCode":0,"duration":"44.48ms","outputs":[{"index":0,"value":"running","type":"String"}]}
$ echo $?
0
```

Output of the Responder:

```console
2026-10-24T13:41:17.744607Z  requestor=opcuactl-1b32681c writer=1 target=1 request=1 inputs: Speed=1450 -> Good outputs: State=running
2026-10-24T13:41:17.838002Z  requestor=opcuactl-c7d00ee6 writer=1 target=1 request=1 inputs: Speed=900 -> Good outputs: State=running
```

Output of the subscriber: the request (Executing), the response (Done) and the acknowledgement (Idle) of the first call, with their topics, content type and UAMessageType:

```console
2026-10-24T13:41:17.743489Z  pub=responder-1 writer=1 seq=0 action-request target=1 request=1 state=Executing requestor=opcuactl-1b32681c timeout=10000ms response-address=opcua/json/action-response/responder-1/Actions topic=opcua/json/action-request/responder-1/Actions content-type=application/json ua-message-type=ua-action-request  Speed=1450
2026-10-24T13:41:17.744662Z  pub=responder-1 writer=1 seq=0 action-response target=1 request=1 state=Done requestor=opcuactl-1b32681c status=Good topic=opcua/json/action-response/responder-1/Actions content-type=application/json ua-message-type=ua-action-response  State=running
2026-10-24T13:41:17.794934Z  pub=responder-1 writer=1 seq=0 action-request target=1 request=1 state=Idle requestor=opcuactl-1b32681c timeout=10000ms response-address=opcua/json/action-response/responder-1/Actions topic=opcua/json/action-request/responder-1/Actions content-type=application/json ua-message-type=ua-action-request
```

UADP over MQTT 3.1.1 with the default addresses of both commands (`--verbose` shows the request queue):

```console
$ opcuactl pubsub respond --url mqtt://127.0.0.1:18883 --input A:Int32 --input B:Int32 --count 1 -v
responding as publisher 1, writer 1, targets 1 on mqtt://127.0.0.1:18883 queue opcua/uadp/action-request/1/Actions
2026-10-24T13:41:18.901801Z  requestor=opcuactl-02591172 writer=1 target=1 request=1 inputs: A=2 B=3 -> Good outputs: A=2 B=3

$ opcuactl pubsub call --url mqtt://127.0.0.1:18883 --input A:Int32=2 --input B:Int32=3 -v
calling Action target 1 of writer 1 of publisher 1 as opcuactl-02591172 on mqtt://127.0.0.1:18883 (request address "opcua/uadp/action-request/1/Actions")
pub=1 writer=1 target=1 request=1 status=Good duration=53.423ms
  #0 = 2 (Int32)
  #1 = 3 (Int32)
$ echo $?
0
```

---

## v0.4.0 features (captured against a go-opcua server)

PubSub captures against the PubSub example server of [go-opcua](https://github.com/otfabric/go-opcua) v1.7.0 (`examples/pubsub/server`), started with `-port 48400 -pubsub opc.udp://127.0.0.1:47200`: it publishes its `Temperature` variable as UADP over UDP and subscribes to it again. The WAGO PLC above has no PubSub.

### PubSub configuration of a server

```console
$ opcuactl pubsub config show -e opc.tcp://localhost:48400

KIND              NAME              NODEID      STATE        DETAILS
PublishSubscribe  PublishSubscribe  i=14443     Operational
  connection      UDP               ns=2;i=105  Operational  PublisherId=1 TransportProfileUri=pubsub-udp-uadp
    writer-group  Publisher         ns=2;i=114  Operational  PublishingInterval=500ms SecurityMode=None WriterGroupId=1
      writer      Plant             ns=2;i=134  Operational  DataSetWriterId=1 KeyFrameCount=1
    reader-group  Subscriber        ns=2;i=141  Operational  SecurityMode=None
      reader      Plant             ns=2;i=149  Operational  DataSetMetaData=Temperature:Double DataSetWriterId=1 PublisherId=1 WriterGroupId=1
  dataset         Plant             ns=2;i=101               DataSetMetaData=Temperature:Double
```

Add a second publisher through the PublishSubscribe methods: a connection to another UDP address, a published data set with the temperature and the server time, a writer group and a writer (`-q` prints only the NodeId):

```console
$ opcuactl pubsub config add-connection -e opc.tcp://localhost:48400 --name Diag --url opc.udp://127.0.0.1:47201 --publisher-id 2234 -q
ns=2;i=166

$ opcuactl pubsub config add-dataset -e opc.tcp://localhost:48400 --name Diag --var Temperature="ns=1;s=Temperature" --var Time=i=2258
added dataset "Diag": ns=2;i=175
FIELD        NODEID              STATUS
Temperature  ns=1;s=Temperature  Good
Time         i=2258              Good

$ opcuactl pubsub config add-writer-group -e opc.tcp://localhost:48400 --connection "ns=2;i=166" --name DiagGroup --writer-group-id 100 --interval 500ms -q
ns=2;i=179

$ opcuactl pubsub config add-writer -e opc.tcp://localhost:48400 --group "ns=2;i=179" --name DiagWriter --writer-id 1 --dataset Diag
added writer "DiagWriter": ns=2;i=199
```

### Subscribe

Without metadata the UADP fields have no names:

```console
$ opcuactl pubsub subscribe --url opc.udp://127.0.0.1:47201 --count 3

2026-10-24T00:11:21.287133Z  pub=2234 wg=100 writer=1 seq=1 keyframe  #0=24.5 #1=2026-10-24T00:11:21.28713Z
2026-10-24T00:11:21.787345Z  pub=2234 wg=100 writer=1 seq=2 keyframe  #0=24.5 #1=2026-10-24T00:11:21.787336Z
2026-10-24T00:11:22.287452Z  pub=2234 wg=100 writer=1 seq=3 keyframe  #0=25 #1=2026-10-24T00:11:22.287442Z
```

The DataSetMetaData of the published data set, read from the server, names them:

```console
$ opcuactl pubsub subscribe --url opc.udp://127.0.0.1:47201 --count 2 --metadata-node "ns=2;i=175" -e opc.tcp://localhost:48400

2026-10-24T00:11:33.287876Z  pub=2234 wg=100 writer=1 seq=25 keyframe  Temperature=30.5 Time=2026-10-24T00:11:33.287868Z
2026-10-24T00:11:33.787643Z  pub=2234 wg=100 writer=1 seq=26 keyframe  Temperature=30.5 Time=2026-10-24T00:11:33.787607Z
```

```console
$ opcuactl pubsub subscribe --url opc.udp://127.0.0.1:47201 --count 1 --jsonl --field Temperature:Double --field Time:DateTime

{"ts":"2026-10-24T00:11:34.288322Z","source":"127.0.0.1:63089","encoding":"uadp","publisherId":"2234","writerGroupId":100,"dataSetWriterId":1,"messageType":"keyframe","sequenceNumber":27,"timestamp":"2026-10-24T00:11:34.288062Z","metaDataVersion":"846115880.846115880","security":"None","fields":[{"index":0,"name":"Temperature","value":31,"type":"Double"},{"index":1,"name":"Time","value":"2026-10-24T00:11:34.288059Z","type":"DateTime"}]}
```

### Disable, enable and remove

```console
$ opcuactl pubsub config disable -e opc.tcp://localhost:48400 --node "ns=2;i=179"
ns=2;i=179: Disabled

$ opcuactl pubsub subscribe --url opc.udp://127.0.0.1:47201 --duration 2s
(no output: the writer group is disabled)

$ opcuactl pubsub config enable -e opc.tcp://localhost:48400 --node "ns=2;i=179"
ns=2;i=179: Operational

$ opcuactl pubsub config remove connection -e opc.tcp://localhost:48400 --node "ns=2;i=166"
removed connection ns=2;i=166

$ opcuactl pubsub config remove dataset -e opc.tcp://localhost:48400 --node "ns=2;i=175"
removed dataset ns=2;i=175
```

### Publish and decode

A RawData DataSet with a DataSetMetaData announcement first, captured with a UDP listener on port 47210 (`cap-meta.hex`, `cap-data.hex`):

```console
$ opcuactl pubsub publish --url opc.udp://127.0.0.1:47210 --publisher-id 2234 --writer-group-id 100 \
    --field Counter:UInt32=42 --field Temp:Double=21.5 --field-encoding rawdata --announce-metadata
published 1 message(s), 52 bytes, to opc.udp://127.0.0.1:47210

$ cat cap-data.hex
f101ba080f64000000000001000100010100fb100100141157404c63dd0100003fb46e323fb46e322a0000000000000000803540

$ opcuactl pubsub decode cap-meta.hex
metadata pub=2234 writer=1 "DataSet" version=846115903.846115903 fields: Counter:UInt32, Temp:Double

$ opcuactl pubsub decode cap-data.hex
Error: decoding the message: DataSetMessage 0: pubsub/uadp: RawData needs the DataSetMetaData

$ opcuactl pubsub decode cap-data.hex --field Counter:UInt32 --field Temp:Double
2026-10-24T00:11:43.909506Z  pub=2234 wg=100 writer=1 seq=1 keyframe  Counter=42 Temp=21.5
```

```console
$ opcuactl pubsub decode cap-data.hex --field Counter:UInt32 --field Temp:Double --format json --pretty
[
  {
    "encoding": "uadp",
    "publisherId": "2234",
    "writerGroupId": 100,
    "dataSetWriterId": 1,
    "messageType": "keyframe",
    "sequenceNumber": 1,
    "timestamp": "2026-10-24T00:11:43.909506Z",
    "metaDataVersion": "846115903.846115903",
    "security": "None",
    "fields": [
      { "index": 0, "name": "Counter", "value": 42, "type": "UInt32" },
      { "index": 1, "name": "Temp", "value": 21.5, "type": "Double" }
    ]
  }
]
```

> Secured messages, the Security Key Service and MQTT are covered by the end-to-end tests (`cmd/opcuactl/pubsub_e2e_test.go`) and by the syntax examples in [README.md](README.md#pubsub) and [RECIPES.md](RECIPES.md).

---

## v0.3.0 features (captured against a go-opcua server)

The WAGO PLC above offers none of these, so the outputs below were captured against a [go-opcua](https://github.com/otfabric/go-opcua) v1.6.0 demo server on `localhost` that offers RSA and ECC endpoints, acts as a Local Discovery Server with multicast discovery, and is configured for reverse connect. Its test variables live in namespace 1.

### ECC security policies

```console
$ opcuactl discover endpoints -e opc.tcp://localhost:52812

ENDPOINT URL               SECURITY POLICY      SECURITY MODE                      LEVEL  CERT  TOKEN TYPES
opc.tcp://localhost:52812  None                 MessageSecurityModeNone            1      yes   UserTokenTypeAnonymous
opc.tcp://localhost:52812  Basic256Sha256       MessageSecurityModeSignAndEncrypt  33     yes   UserTokenTypeAnonymous
opc.tcp://localhost:52812  ECC_nistP256         MessageSecurityModeSignAndEncrypt  63     yes   UserTokenTypeAnonymous
opc.tcp://localhost:52812  ECC_nistP256_AesGcm  MessageSecurityModeSignAndEncrypt  83     yes   UserTokenTypeAnonymous
```

With an ECDSA P-256 client certificate, `--best` and `auto` pick the strongest endpoint that key can use:

```console
$ opcuactl discover endpoints -e opc.tcp://localhost:52812 --best --cert client-ecc.pem --key client-ecc.key

ENDPOINT URL               SECURITY POLICY      SECURITY MODE                      LEVEL
opc.tcp://localhost:52812  ECC_nistP256_AesGcm  MessageSecurityModeSignAndEncrypt  83
```

```console
$ opcuactl get server -e opc.tcp://localhost:52812 --security-policy auto --security-mode auto \
    --cert client-ecc.pem --key client-ecc.key --trusted-cert server.pem

Server Status
  State:          ServerStateRunning
  Start Time:     2026-10-12T09:30:38.798959Z
  Current Time:   2026-10-12T09:30:38.959869Z
  Security:       ECC_nistP256_AesGcm / MessageSecurityModeSignAndEncrypt
  Product:        otfabric OPC/UA Server 0.0.0-dev
  Manufacturer:   otfabric
  Build:           ()
```

> With an RSA client certificate the same two commands select `Basic256Sha256`: an RSA key cannot open an ECC channel.

### Local Discovery Server and mDNS

```console
$ opcuactl discover lds --host opc.tcp://localhost:52812

Discovery server: opc.tcp://localhost:52812

Registered servers (FindServers):
APPLICATION NAME  APPLICATION URI                    TYPE                   DISCOVERY URLS
Demo              urn:otfabric:opcuactl:test:server  ApplicationTypeServer  opc.tcp://localhost:52812

Servers on network (FindServersOnNetwork, mDNS):
RECORD  SERVER NAME     DISCOVERY URL              CAPABILITIES
0       Demo-demo-host  opc.tcp://demo-host:52812  LDS, DA
```

> `--host` takes a host name or IP address (port 4840 is added) or a complete URL. On a discovery server without the multicast extension the second table is replaced by a note; that is not an error.

```console
$ opcuactl discover servers -e opc.tcp://localhost:52812 --on-network --capability DA

RECORD  SERVER NAME     DISCOVERY URL              CAPABILITIES
0       Demo-demo-host  opc.tcp://demo-host:52812  LDS, DA
```

### Reverse connect

The server is configured to dial `opc.tcp://127.0.0.1:52811`; opcuactl listens there and needs no `--endpoint`:

```console
$ opcuactl get value --reverse-listen 127.0.0.1:52811 --timeout 30s -n "ns=1;s=Int32"

NODE ID       VALUE  TYPE         STATUS  SOURCE TIMESTAMP             SERVER TIMESTAMP
ns=1;s=Int32  42     TypeIDInt32  Good    2026-10-12T09:30:38.994404Z
```

```console
$ opcuactl ping --reverse-listen 127.0.0.1:52811 --timeout 30s

  ✓ secure   53.713542ms
  ✓ browse   402.166µs
```

> `ping` has no `tcp` stage here: with reverse connect the server opens the connection.

### OPC UA JSON and XML encodings

```console
$ opcuactl get value -e opc.tcp://localhost:52812 -n "ns=1;s=Int32Array" --ua-encoding json

{"UaType":6,"Value":[1,2,3,4,5],"SourceTimestamp":"2026-10-12T09:30:39.050837Z"}
```

```console
$ opcuactl get value -e opc.tcp://localhost:52812 -n "ns=1;s=Int32Array" --ua-encoding json-reversible

{"Value":{"Type":6,"Body":[1,2,3,4,5]},"SourceTimestamp":"2026-10-12T09:30:39.053299Z"}
```

```console
$ opcuactl get value -e opc.tcp://localhost:52812 -n "ns=1;s=Double" --ua-encoding xml --pretty

<DataValue xmlns="http://opcfoundation.org/UA/2008/02/Types.xsd">
  <Value>
    <Value>
      <Double>100</Double>
    </Value>
  </Value>
  <SourceTimestamp>2026-10-12T09:30:39.05567Z</SourceTimestamp>
</DataValue>
```

Writing a whole array as a Variant in the OPC UA JSON encoding, and reading it back:

```console
$ opcuactl write value -e opc.tcp://localhost:52812 -n "ns=1;s=Int32Array" \
    --value '{"UaType":6,"Value":[9,8,7]}' --type ua-json --yes

NODE ID            STATUS  STATUS CODE
ns=1;s=Int32Array  Good    0

$ opcuactl get value -e opc.tcp://localhost:52812 -n "ns=1;s=Int32Array"

NODE ID            VALUE    TYPE         STATUS  SOURCE TIMESTAMP  SERVER TIMESTAMP
ns=1;s=Int32Array  9, 8, 7  TypeIDInt32  Good
```

### Data-change filters (deadband)

While this ran, another client wrote 101, 110, 112 and 118 to the variable (starting from 100). With an absolute deadband of 5 only the moves of more than 5 from the last *reported* value arrive:

```console
$ opcuactl monitor value -e opc.tcp://localhost:52812 -n "ns=1;s=Double" --interval 50ms \
    --deadband-type absolute --deadband 5 --duration 2500ms

2026-10-12T09:30:39.063367Z  ns=1;s=Double  100  [Good]
2026-10-12T11:30:39.915791+02:00  ns=1;s=Double  110  [Good]
2026-10-12T11:30:40.7157+02:00  ns=1;s=Double  118  [Good]
```

The server decides per item whether it can apply a filter. A deadband on a string is rejected and reported on stderr, the analog item next to it keeps running:

```console
$ opcuactl monitor value -e opc.tcp://localhost:52812 -n "ns=1;s=String" -n "ns=1;s=Analog" \
    --deadband-type percent --deadband 10 --count 1 --duration 5s --jsonl

{"ts":"2026-10-12T11:30:41.672287+02:00","nodeId":"ns=1;s=Analog","value":100,"type":"TypeIDDouble","status":"Good","statusCode":0,"sourceTimestamp":"2026-10-12T09:30:41.570019Z"}
Error: monitoring ns=1;s=String: BadFilterNotAllowed
```

A percent deadband needs an `EURange` property; without one nothing can be monitored and the command exits with code 3:

```console
$ opcuactl monitor value -e opc.tcp://localhost:52812 -n "ns=1;s=Double" \
    --deadband-type percent --deadband 10 --duration 1s

Error: monitoring ns=1;s=Double: BadMonitoredItemFilterUnsupported
```

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
