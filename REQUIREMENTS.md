Below is a ready-to-use AI Agent requirements/specification for implementing opcuactl, derived from the otfabric/opcua v0.1.2 release and the attached README.md / API.md. The library (v0.1.2, 2026-03-11) exposes discovery, browse, read/write, method calls, subscriptions, history, security, reconnect, and monitoring primitives that map well to this CLI. As of v0.1.2, browse path resolution is a first-class feature with client helpers NodeFromPath and NodeFromPathInNamespace for resolving dot-separated paths from the Objects folder (or a given namespace).

⸻

AI Agent Requirements — opcuactl

1. Mission

Build a production-grade Go CLI named opcuactl on top of github.com/otfabric/opcua that provides a clean operator-focused UX for:
	•	discovery of OPC UA servers and endpoints
	•	address-space browsing and search
	•	node value and attribute reads
	•	value writes
	•	method invocation
	•	real-time subscriptions for values and events
	•	historical value/event retrieval
	•	export of node metadata and sampled values
	•	version / capability introspection

The library already supports the key underlying capabilities needed by this CLI: FindServers, FindServersOnNetwork, GetEndpoints, browsing, reads, writes, CallMethod, subscriptions, event filters, and history-read operations.

⸻

2. Product goals

The agent must optimize for these goals:
	1.	Operator-first UX
Every command must be usable interactively from a terminal without requiring Go knowledge.
	2.	Deterministic output for automation
All commands must support machine-readable formats for scripts and pipelines.
	3.	Safe-by-default behavior
Reads are default-safe, writes/calls require explicit user intent, and monitoring/history commands must not accidentally flood output.
	4.	Thin but opinionated wrapper
opcuactl should be a focused CLI layer over the library, not a second SDK.
	5.	Feature parity with the released client surface where practical
Commands should map cleanly to the public API shown in API.md and to the examples listed in the README.

⸻

3. Scope

3.1 In scope

Implement the following command tree:

opcuactl
├── discover
│   ├── servers
│   └── endpoints
├── browse
│   ├── tree
│   ├── children
│   ├── refs
│   └── path
├── find
│   ├── node
│   ├── type
│   └── namespace
├── get
│   ├── value
│   ├── attr
│   ├── node
│   └── server
├── write
│   └── value
├── call
│   └── method
├── monitor
│   ├── value
│   ├── event
│   └── alarm
├── history
│   ├── value
│   └── event
├── export
│   ├── nodes
│   └── values
└── version

These map naturally to the released client APIs for discovery, browse, node helpers, write methods, method calls, subscription builder, event filters, and history reads.

3.2 Out of scope for v1

Do not implement these in the first version unless explicitly required later:
	•	server hosting / embedded server mode
	•	history update / delete operations
	•	AddNodes / DeleteNodes / AddReferences / DeleteReferences
	•	QueryFirst / QueryNext CLI exposure
	•	certificate management PKI toolchain
	•	TUI / ncurses UI
	•	background daemon mode
	•	alarm acknowledge / confirm writeback workflows

The library supports more than the CLI needs; v1 should stay focused.

⸻

4. Technical baseline

The implementation must target:
	•	Language: Go
	•	Minimum Go version: 1.23+ because the upstream library requires it.
	•	Primary dependency: github.com/otfabric/opcua
	•	CLI framework: Cobra preferred
	•	Structured output: JSON as first-class format
	•	Logging: quiet by default; verbose/debug flags optional
	•	Context/timeouts: every network operation must run with context and timeout

⸻

5. Design principles
	1.	One command = one clear user intent
	2.	Human-readable by default, JSON for automation
	3.	No hidden writes
	4.	Connection options centralized and reused
	5.	Consistent node identifier handling
	6.	Stable exit codes
	7.	Graceful reconnect only for long-running monitor commands

The library already exposes reconnect/session/security abstractions and connection-state handling, so the CLI should leverage those rather than reinventing transport/session logic.

⸻

6. Global CLI requirements

6.1 Global flags

Every networked subcommand must support a common connection flag set, at minimum:
	•	--endpoint <opc.tcp://host:port>
	•	--security-policy <None|Basic128Rsa15|Basic256|Basic256Sha256|Aes128Sha256RsaOaep|Aes256Sha256RsaPss>
	•	--security-mode <None|Sign|SignAndEncrypt>
	•	--auth <anonymous|username|certificate>
	•	--username <user>
	•	--password <pass>
	•	--cert <file>
	•	--key <file>
	•	--server-cert <file>
	•	--timeout <duration>
	•	--namespace <uri or index> where relevant
	•	--format <table|json|yaml>
	•	--pretty
	•	--verbose
	•	--insecure-skip-verify only if the SDK actually supports the chosen behavior through options

Security and authentication are important because the README explicitly documents support for multiple security policies and auth modes including anonymous, username/password, X.509 certificate, and issued token.

6.2 Environment variables

All important flags must also support env vars, for example:
	•	OPCUACTL_ENDPOINT
	•	OPCUACTL_SECURITY_POLICY
	•	OPCUACTL_SECURITY_MODE
	•	OPCUACTL_AUTH
	•	OPCUACTL_USERNAME
	•	OPCUACTL_PASSWORD
	•	OPCUACTL_CERT
	•	OPCUACTL_KEY
	•	OPCUACTL_TIMEOUT
	•	OPCUACTL_FORMAT

6.3 Output formats

Support at least:
	•	table for humans
	•	json for scripts
	•	yaml optional but useful

Rules:
	•	table output may be concise
	•	JSON must be stable and documented
	•	errors must go to stderr
	•	data must go to stdout

6.4 Exit codes

Minimum contract:
	•	0 success
	•	1 usage / validation error
	•	2 connection / transport failure
	•	3 service or status-code failure
	•	4 partial success where meaningful for batch/export operations

⸻

7. Command requirements

7.1 discover

The docs explicitly expose discovery operations FindServers, FindServersOnNetwork, and GetEndpoints, and the README examples include discovery and endpoints examples.

7.1.1 discover servers

Purpose: discover OPC UA application descriptions from a discovery endpoint.

Behavior:
	•	call FindServers
	•	optional --network mode may call FindServersOnNetwork
	•	print application URI, product URI, application name, discovery URLs, application type

Flags:
	•	--endpoint
	•	--on-network
	•	--server-uri
	•	--locale
	•	--format

Acceptance:
	•	works against LDS/discovery endpoints
	•	returns stable JSON schema
	•	gracefully handles empty results

7.1.2 discover endpoints

Purpose: list server endpoints and supported security/user token combinations.

Behavior:
	•	call GetEndpoints
	•	optionally filter or rank results
	•	optionally expose “best endpoint” selection using SelectEndpoint

Flags:
	•	--endpoint
	•	--policy
	•	--mode
	•	--transport-profile
	•	--best

Output fields:
	•	endpoint URL
	•	security policy URI / friendly name
	•	security mode
	•	security level
	•	server certificate presence
	•	user token policies

Acceptance:
	•	--best must deterministically choose an endpoint consistent with requested policy/mode
	•	must clearly show insecure vs secure endpoints

The existence of GetEndpoints and SelectEndpoint in the API makes this a first-class requirement.

⸻

7.2 browse

The library supports browse requests, node reference helpers, children/reference traversal, and recursive walking.

7.2.1 browse tree

Purpose: recursively print an address-space tree from a start node.

Behavior:
	•	start from --node
	•	recursively walk forward hierarchical references by default
	•	support max depth and node-class filtering
	•	show browse name, node ID, node class, display name
	•	avoid infinite recursion / duplicate loops

Flags:
	•	--node <NodeID> default ns=0;i=85 (Objects)
	•	--depth <n>
	•	--refs <referenceTypeId>
	•	--include-subtypes
	•	--node-class <all|object|variable|method|objecttype|variabletype|referencetype|datatype|view>
	•	--show-attrs
	•	--format

Implementation hint:
	•	prefer Node.Walk() where it fits, otherwise custom recursive browse using References/Browse

7.2.2 browse children

Purpose: list direct child nodes of a node.

Behavior:
	•	use Node.Children(...)
	•	optionally augment with Summary() for rich rows

Output:
	•	child node ID
	•	browse name
	•	display name
	•	node class
	•	type definition if requested

7.2.3 browse refs

Purpose: inspect reference edges of a node.

Behavior:
	•	use Node.References(...)
	•	allow forward/inverse/both
	•	allow specifying reference type and include-subtypes

Output:
	•	reference type
	•	direction
	•	target node ID
	•	browse name
	•	target class
	•	target server index if expanded

7.2.4 browse path

Purpose: resolve browse paths or navigate by path segments.

Library support (v0.1.1+):
	•	Client.NodeFromPath(ctx, path string) (*Node, error) — resolves a dot-separated path from the server’s Objects folder (namespace 0). Example: node, err := c.NodeFromPath(ctx, "Server.ServerStatus").
	•	Client.NodeFromPathInNamespace(ctx, ns uint16, path string) (*Node, error) — same, but all path segments use the given namespace index (e.g. for custom namespaces). Example: node, err := c.NodeFromPathInNamespace(ctx, 1, "MyFolder.Sensor.Temperature").
	•	For paths that do not start from Objects, use Node().TranslateBrowsePathInNamespaceToNodeID or Node().TranslateBrowsePathsToNodeIDs as before.

Behavior:
	•	accept path input such as Server.ServerStatus or MyFolder.Sensor.Temperature (dot-separated; optionally with --namespace for custom NS).
	•	allow --start-node for custom root (then use Node-level translate APIs).
	•	output resolved NodeID(s)
	•	optionally print intermediate segments

Acceptance:
	•	path resolution must handle ambiguous names explicitly
	•	return non-zero exit if no match

⸻

7.3 find

The library exposes namespace helpers, node summaries, browse APIs, and type information, which are enough for CLI-side search.

7.3.1 find node

Purpose: search nodes by browse name, display name, node ID fragment, or pattern.

Behavior:
	•	recursively browse from --start-node
	•	match on browse name and display name
	•	support exact / contains / regex modes
	•	optionally restrict by node class

Flags:
	•	--start-node
	•	--depth
	•	--name
	•	--display-name
	•	--regex
	•	--class
	•	--limit

7.3.2 find type

Purpose: find nodes by type definition or data type.

Behavior:
	•	inspect each candidate node’s TypeDefinition() and/or DataType()
	•	allow match by NodeID or symbolic/friendly name when available

Examples:
	•	all variables of a given variable type
	•	all nodes whose data type is Double

7.3.3 find namespace

Purpose: inspect or search namespaces.

Behavior:
	•	print namespace array
	•	support matching by URI substring
	•	support reverse mapping via FindNamespace / NamespaceURI

The API explicitly provides namespace helpers for this.

⸻

7.4 get

The API offers ReadValue, node attribute helpers, Summary(), and ServerStatus().

7.4.1 get value

Purpose: read one or more node values.

Behavior:
	•	accept repeated --node
	•	use ReadValue / ReadValues or Node.Value()
	•	show status, source timestamp, server timestamp, encoding hints if available

Flags:
	•	--node
	•	--batch
	•	--timestamps
	•	--format

Acceptance:
	•	batch reads should issue efficient grouped requests when possible

7.4.2 get attr

Purpose: read arbitrary attributes.

Behavior:
	•	accept --attr by name or numeric ID
	•	use Node.Attribute() or Attributes()
	•	support reading common attrs like BrowseName, DisplayName, Description, DataType, AccessLevel, UserAccessLevel, NodeClass

7.4.3 get node

Purpose: rich node summary.

Behavior:
	•	call Node.Summary()
	•	also allow --full to fetch extra attributes
	•	output one structured object per node

The docs explicitly describe Node.Summary() as a single-read helper for common attributes.

7.4.4 get server

Purpose: server-level metadata/status.

Behavior:
	•	call ServerStatus(ctx)
	•	include active security policy/mode from client connection
	•	optionally show namespace array and selected endpoint metadata

⸻

7.5 write

The library supports WriteValue, WriteValues, WriteAttribute, and WriteNodeValue.

7.5.1 write value

Purpose: write a value to a node.

Behavior:
	•	require explicit --node and --value
	•	default to writing the Value attribute via WriteNodeValue
	•	support explicit type coercion
	•	show returned StatusCode

Flags:
	•	--node
	•	--value
	•	--type <bool|int16|uint16|int32|uint32|int64|uint64|float|double|string|bytes|datetime|guid|nodeid|expandednodeid|variant|json>
	•	--index-range
	•	--dry-run for parse/validate only
	•	--yes for non-interactive safety override where needed

Safety requirements:
	•	no implicit write from stdin without --value or --file
	•	if output is tty and not --yes, optionally warn before write to production-looking endpoints

Acceptance:
	•	wrong type coercions must fail before network send where possible

⸻

7.6 call

The docs expose Call, CallMethod, and MethodArguments.

7.6.1 call method

Purpose: invoke an OPC UA method.

Behavior:
	•	require --object and --method
	•	optionally introspect arguments first using MethodArguments
	•	accept arguments as repeated --arg, JSON array, or file input
	•	print output arguments and status

Flags:
	•	--object <NodeID>
	•	--method <NodeID>
	•	--arg <value>
	•	--arg-json <json>
	•	--arg-file <file>
	•	--types <comma-separated>
	•	--introspect

Acceptance:
	•	--introspect must show expected input/output argument signatures before invocation
	•	method output must be machine-readable in JSON mode

⸻

7.7 monitor

The library supports subscriptions, monitoring of values and events, and a higher-level monitor.NodeMonitor. Notification payloads include data-change, event, and status-change notifications.

7.7.1 monitor value

Purpose: subscribe to live value changes.

Behavior:
	•	support one or many nodes
	•	use either client.NewSubscription().Monitor(...) or monitor.NodeMonitor
	•	stream line-by-line updates
	•	optionally emit newline-delimited JSON

Flags:
	•	--node repeatable
	•	--interval <duration>
	•	--queue-size <n>
	•	--sampling <duration>
	•	--timestamps <source|server|both|neither>
	•	--duration <duration>
	•	--count <n>
	•	--jsonl

Acceptance:
	•	command must cancel cleanly on Ctrl+C
	•	must propagate subscription errors to stderr
	•	reconnect behavior should be explicit and observable

7.7.2 monitor event

Purpose: subscribe to event notifications.

Behavior:
	•	allow monitoring event notifier nodes
	•	construct event filter with selectable fields
	•	use SubscriptionBuilder.MonitorEvents(filter, ...)

Flags:
	•	--node
	•	--select <field> repeatable, defaults to common fields like EventType,SourceName,Message,Severity,Time
	•	--where <expression> optional future extension
	•	--duration
	•	--jsonl

The API and docs show event monitoring support and an event filter builder.

7.7.3 monitor alarm

Important note:
	•	there is no dedicated alarm convenience API shown in the released surface; alarms appear to be a specialized form of OPC UA events rather than a separate client primitive. Therefore monitor alarm must be implemented as a constrained specialization of monitor event, using alarm/condition-oriented event fields and filtering conventions, not as a new transport feature.

Behavior:
	•	alias / specialization over event monitoring
	•	default selected fields should include alarm-relevant fields such as event type, message, severity, active state, acked state, confirmed state, retain, source name, time
	•	if event field paths are not resolvable on all servers, degrade gracefully and show missing-field diagnostics

⸻

7.8 history

The client supports history value reads, event history reads, raw/modified, processed, and at-time operations, plus an iterator for paging.

7.8.1 history value

Purpose: read historical data values.

Behavior:
	•	default to raw history read over a time range
	•	allow processed and at-time variants later if useful
	•	support paging and full export
	•	output timestamps, status, value

Flags:
	•	--node
	•	--start <RFC3339>
	•	--end <RFC3339>
	•	--max-values <n>
	•	--mode <raw|modified|processed|attime>
	•	--interval <duration> for processed mode
	•	--continuation all|single-page

Acceptance:
	•	command must validate start/end
	•	JSON must preserve timestamps accurately

7.8.2 history event

Purpose: read event history from historical event sources.

Behavior:
	•	call HistoryReadEvent
	•	support selected event fields
	•	optionally filter by event type

Flags:
	•	--node
	•	--start
	•	--end
	•	--select
	•	--limit

⸻

7.9 export

This command family is CLI composition over browse/get/history, not a unique library primitive.

7.9.1 export nodes

Purpose: export node metadata from a subtree.

Behavior:
	•	walk/browse from --start-node
	•	export browse name, display name, node class, node ID, type definition, data type, access level, description, parent path if derivable

Formats:
	•	json
	•	jsonl
	•	csv

Flags:
	•	--start-node
	•	--depth
	•	--class
	•	--include-attrs
	•	--output <file>

7.9.2 export values

Purpose: snapshot or historical export of values.

Modes:
	•	snapshot: read current values for a list or subtree
	•	history: export time-series values for nodes over a range

Flags:
	•	--node repeatable
	•	--nodes-file
	•	--start-node
	•	--depth
	•	--history
	•	--start
	•	--end
	•	--output
	•	--format <json|jsonl|csv>

Acceptance:
	•	CSV export must flatten values predictably
	•	complex variants must serialize consistently

⸻

7.10 version

Purpose:
	•	print CLI version
	•	optionally print linked library version / build info
	•	optionally print capability matrix

Behavior:
	•	default: semantic version, git commit, build date
	•	--verbose: include supported commands, formats, default security values, Go version

The upstream release is currently v0.1.1 (browse path helpers added). See https://github.com/otfabric/opcua/releases/tag/v0.1.1.

⸻

8. NodeID and path handling requirements

The API exposes ua.ParseNodeID and ua.MustParseNodeID, with accepted forms like ns=<ns>;i=<id> and shorthand i=<n>. The CLI must fully adopt these canonical forms.

Requirements:
	•	accept standard string NodeIDs:
	•	i=2258
	•	ns=2;i=1001
	•	ns=2;s=Temperature
	•	ns=3;g=<guid>
	•	ns=4;b=<base64>
	•	preserve canonical NodeID string form in JSON output
	•	never invent alternate syntax
	•	namespace URI resolution should be available where user provides URI instead of numeric index
	•	path syntax must be clearly documented as CLI syntax, separate from OPC UA native NodeID syntax

⸻

9. Output schema requirements

For JSON output:
	•	all timestamps RFC3339Nano
	•	all NodeIDs as strings
	•	all status codes rendered as both numeric and symbolic when possible
	•	all enum-like values rendered as friendly string plus numeric value where useful
	•	values should preserve type information

Recommended generic result envelope for streaming commands:

{
  "ts": "2026-03-11T06:00:00Z",
  "command": "monitor value",
  "endpoint": "opc.tcp://host:4840",
  "data": { ... }
}

For non-streaming commands, plain JSON objects/arrays are acceptable.

⸻

10. Error handling requirements

The library documents sentinel error categories and returns OPC UA status codes for many operations. The CLI must distinguish transport/configuration failures from server-returned service/status failures.

Requirements:
	•	print friendly one-line error summary to stderr
	•	in --format json, emit structured error object
	•	include OPC UA StatusCode when available
	•	for partial operations, report per-item results
	•	never silently discard monitored notification errors
	•	Ctrl+C must cancel context cleanly

⸻

11. Security requirements

The README explicitly lists six supported security policies and multiple auth modes. The CLI must expose these clearly and safely.

Requirements:
	•	anonymous mode supported out of the box
	•	username/password supported
	•	certificate-based auth supported where library options allow
	•	endpoint security settings must be visible in discovery and connection logs
	•	do not echo passwords
	•	prefer secure endpoint selection when user requests --best
	•	surface server certificate thumbprint/fingerprint where possible

⸻

12. Monitoring UX requirements

Because subscriptions are a major upstream feature, CLI streaming UX must be polished. The README and API describe subscription builder APIs, event monitoring, and a higher-level monitor package.

Requirements:
	•	monitor value and monitor event must support:
	•	human stream mode
	•	JSONL stream mode
	•	show subscription setup summary in verbose mode
	•	allow duration-based or count-based automatic stop
	•	show dropped/delivered counts if using high-level monitor wrapper where available
	•	heartbeat/keepalive events may be shown in verbose mode only

⸻

13. Testing requirements

The README states the project already emphasizes unit tests, integration tests, self-integration, fuzzing, and linting. opcuactl should match that quality bar.

Required tests:
	•	unit tests for:
	•	flag parsing
	•	NodeID parsing
	•	output rendering
	•	value coercion for writes
	•	method arg parsing
	•	history argument validation
	•	integration tests against:
	•	public example/simple test server
	•	local in-process server where feasible
	•	golden tests for:
	•	table output
	•	JSON output
	•	smoke tests for:
	•	discover endpoints
	•	get value
	•	browse tree depth 1
	•	monitor value cancel behavior

⸻

14. Suggested internal architecture

The README’s package structure shows clear boundaries: opcua, ua, monitor, errors, logger, stats, etc. The CLI should mirror that separation.

Recommended layout:

cmd/opcuactl/
  root.go
  discover.go
  browse.go
  find.go
  get.go
  write.go
  call.go
  monitor.go
  history.go
  export.go
  version.go

internal/
  app/            # command orchestration
  conn/           # client creation, shared options, auth/security
  format/         # table/json/yaml rendering
  parse/          # NodeID, values, args, time ranges
  browseutil/     # recursive walkers, path resolution
  exportutil/     # CSV/JSONL writers
  monitorutil/    # subscription/event stream helpers
  model/          # CLI output structs


⸻

15. Implementation mapping to upstream API

Use this mapping as the implementation contract:
	•	discover servers → Client.FindServers, Client.FindServersOnNetwork
	•	discover endpoints → Client.GetEndpoints, SelectEndpoint
	•	browse children → Node.Children
	•	browse refs → Node.References / Node.Browse
	•	browse tree → Node.Walk or recursive References
	•	browse path → Client.NodeFromPath, Client.NodeFromPathInNamespace (from Objects/custom NS); Node().TranslateBrowsePathInNamespaceToNodeID / TranslateBrowsePathsToNodeIDs for custom start node
	•	find namespace → NamespaceArray, FindNamespace, NamespaceURI
	•	get value → ReadValue, ReadValues, Node.Value
	•	get attr → Node.Attribute, Node.Attributes
	•	get node → Node.Summary
	•	get server → ServerStatus
	•	write value → WriteNodeValue, WriteValue, WriteAttribute
	•	call method → CallMethod, MethodArguments
	•	monitor value → SubscriptionBuilder.Monitor, monitor.NodeMonitor
	•	monitor event → SubscriptionBuilder.MonitorEvents, event filter builder
	•	monitor alarm → event specialization, not separate protocol primitive
	•	history value → ReadHistory, HistoryReadRawModified, HistoryReadProcessed, HistoryReadAtTime
	•	history event → HistoryReadEvent

⸻

16. Non-functional requirements
	•	startup latency for simple reads should be low
	•	no goroutine leaks on canceled monitor commands
	•	deterministic JSON output
	•	race-free tests
	•	readable help text with examples
	•	no CGo, matching upstream pure-Go philosophy from the README.

⸻

17. Explicit agent instructions

Use these as direct instructions for the coding agent:
	1.	Implement opcuactl in Go using Cobra.
	2.	Reuse a single shared connection/config layer across all subcommands.
	3.	Prefer upstream high-level helpers before falling back to raw UA requests.
	4.	Keep command names and hierarchy exactly as specified.
	5.	Provide table and json outputs for every command.
	6.	Ensure every command has at least two realistic examples in help text.
	7.	Add tests for parsing, rendering, and at least one integration path per command family.
	8.	Do not implement server-mode features in this CLI.
	9.	Treat monitor alarm as filtered event monitoring unless a dedicated client primitive appears later.
	10.	Do not expose undocumented upstream behavior as if it were guaranteed.

⸻

18. Suggested phased delivery

Phase 1:
	•	version
	•	discover servers
	•	discover endpoints
	•	get value
	•	get node
	•	browse children
	•	browse refs

Phase 2:
	•	browse tree
	•	find namespace
	•	find node
	•	write value
	•	call method

Phase 3:
	•	monitor value
	•	monitor event
	•	history value
	•	export nodes

Phase 4:
	•	browse path
	•	find type
	•	history event
	•	export values
	•	monitor alarm

⸻

19. Final note on assumptions

Two areas need careful implementation rather than assumption:
	•	browse path: as of v0.1.1 the library provides dedicated convenience wrappers (NodeFromPath, NodeFromPathInNamespace) for path resolution from Objects or a given namespace; use these for browse path. For custom start nodes, use Node().TranslateBrowsePathInNamespaceToNodeID or TranslateBrowsePathsToNodeIDs.
	•	monitor alarm: do not pretend there is a separate alarm API; the safer interpretation is “alarm-oriented event monitoring.”

If you want, I can turn this into a Cursor/Copilot-ready AGENTS.md file with repo structure, coding standards, acceptance tests, and command help examples.