// SPDX-License-Identifier: MIT

package opcuactl

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/otfabric/go-opcua"
	"github.com/otfabric/go-opcua/id"
	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/opcuactl/internal/format"
	"github.com/otfabric/opcuactl/internal/model"
	"github.com/otfabric/opcuactl/internal/parse"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// historyEventFields are the event fields history event returns by default.
var historyEventFields = []string{"EventId", "EventType", "SourceName", "Time", "Message", "Severity"}

// historyEventModeValues are the --mode values of history update-events
// (PerformUpdateType, Part 11 §6.9.4).
var historyEventModeValues = []string{"insert", "replace", "update"}

func newHistoryEventCmd() *cobra.Command {
	var (
		nodeStr      string
		startStr     string
		endStr       string
		selectStr    []string
		ofTypeStr    string
		severityMin  uint16
		severityMax  uint16
		numValues    uint32
		maxEvents    uint32
		continuation string
	)

	cmd := &cobra.Command{
		Use:     "event",
		Aliases: []string{"events"},
		Short:   "Read historical events of an event notifier",
		Long: `Read the event history of an event notifier (HistoryRead with
ReadEventDetails, Part 4 §5.10.3, Part 11 §6.5.2). The node is an Object or
View whose EventNotifier has the HistoryRead bit (a HistoricalEventNode).

The range: --start and --end in RFC 3339. With --start before --end the
events from --start (inclusive) to --end (exclusive), oldest first; with
--start after --end the events from --start back to --end, newest first. One
of them is enough together with --num-values or --max-events (Part 11 asks
for two of StartTime, EndTime and NumValuesPerNode): only --start reads
forward from it, only --end backward from it.

Fields: --select chooses the event fields (repeatable; default: EventId,
EventType, SourceName, Time, Message, Severity) as BrowsePaths: names joined
with "/" (AckedState/Id), with a namespace index where needed
(2:Temperature); ConditionId selects the NodeId of a condition. EventIds are
printed in base64, as history delete-events and alarms take them. A field
the server does not store reads Bad_NoData.

Filter: --of-type (an event type NodeId or name, its subtypes included),
--severity-min and --severity-max build the WhereClause, the three combined
with And.

Paging: --num-values is the page size (NumValuesPerNode); opcuactl follows
the continuation points until the history is read, --max-events events were
returned or, with --continuation single, after one page, and then releases
the continuation point.

Exit codes: 3 for a failed HistoryRead or a Bad status of the node
(Bad_NodeIdUnknown, Bad_HistoryOperationUnsupported, ...); no events in the
range (Good_NoData) is success.

Examples:
  opcuactl history event -e opc.tcp://localhost:4840 -n "ns=1;s=Demo" --start 2026-03-01T00:00:00Z --end 2026-03-11T00:00:00Z
  opcuactl history event -e opc.tcp://localhost:4840 -n "ns=1;s=Demo" --end 2026-03-11T00:00:00Z --max-events 10
  opcuactl history event -e opc.tcp://localhost:4840 -n "ns=1;s=Demo" --start 2026-03-01T00:00:00Z --end 2026-03-11T00:00:00Z --of-type AlarmConditionType --severity-min 500
  opcuactl history event -e opc.tcp://localhost:4840 -n "ns=1;s=Demo" --start 2026-03-01T00:00:00Z --end 2026-03-11T00:00:00Z --select EventId --select Message --select ActiveState/Id
  opcuactl history event -e opc.tcp://localhost:4840 -n i=2253 --start 2026-03-10T00:00:00Z --end 2026-03-11T00:00:00Z --num-values 100 --format jsonl`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if nodeStr == "" {
				return exitCodef(1, "--node is required")
			}
			nodeID, err := parse.NodeID(nodeStr)
			if err != nil {
				return exitCode(1, err)
			}
			var startTime, endTime time.Time
			if startStr != "" {
				if startTime, err = time.Parse(time.RFC3339, startStr); err != nil {
					return exitCodef(1, "invalid --start time: %v", err)
				}
			}
			if endStr != "" {
				if endTime, err = time.Parse(time.RFC3339, endStr); err != nil {
					return exitCodef(1, "invalid --end time: %v", err)
				}
			}
			continuation = strings.ToLower(continuation)
			if continuation != "all" && continuation != "single" {
				return exitCodef(1, "invalid --continuation %q (all|single)", continuation)
			}
			perNode := numValues
			if maxEvents > 0 && (perNode == 0 || maxEvents < perNode) {
				perNode = maxEvents
			}
			given := 0
			for _, ok := range []bool{!startTime.IsZero(), !endTime.IsZero(), perNode > 0} {
				if ok {
					given++
				}
			}
			if given < 2 {
				return exitCodef(1, "give --start and --end, or one of them with --num-values or --max-events")
			}

			fields := historyEventFields
			if len(selectStr) > 0 {
				fields = selectStr
			}
			selects, err := parse.EventSelectClauses(fields)
			if err != nil {
				return exitCodef(1, "invalid --select: %v", err)
			}
			var ofType *ua.NodeID
			if ofTypeStr != "" {
				if ofType, err = parse.NodeID(ofTypeStr); err != nil {
					return exitCodef(1, "invalid --of-type: %v", err)
				}
			}
			var sevMin, sevMax *uint16
			if cmd.Flags().Changed("severity-min") {
				sevMin = &severityMin
			}
			if cmd.Flags().Changed("severity-max") {
				sevMax = &severityMax
			}
			details := &ua.ReadEventDetails{
				NumValuesPerNode: perNode,
				StartTime:        startTime,
				EndTime:          endTime,
				Filter:           eventFilterWith(selects, ofType, sevMin, sevMax),
			}

			ctx, stop := signalContext()
			defer stop()
			c, err := connCfg.NewClient(ctx)
			if err != nil {
				format.Errf("%v", err)
				return exitCode(2, err)
			}
			defer func() { _ = c.Close(context.WithoutCancel(ctx)) }()

			events, pages, err := readEventHistory(ctx, c, nodeID, details, maxEvents, continuation == "single")
			if err != nil {
				format.Errf("%v", err)
				return err
			}
			results := make([]model.HistoryEventRecord, len(events))
			for i, efl := range events {
				rec := model.HistoryEventRecord{NodeID: nodeID.String(), Fields: make(map[string]interface{}, len(fields))}
				for j, name := range fields {
					var v *ua.Variant
					if efl != nil && j < len(efl.EventFields) {
						v = efl.EventFields[j]
					}
					rec.Fields[name] = historyEventValue(v)
				}
				results[i] = rec
			}
			if verbose {
				format.Errf("returned %d historical event(s) for %s in %d page(s)", len(results), nodeID, pages)
			}

			w := newWriter()
			if format.ParseFormat(outputFormat) == format.Table {
				headers := make([]string, len(fields))
				for i, name := range fields {
					headers[i] = strings.ToUpper(name)
				}
				rows := make([][]string, len(results))
				for i, r := range results {
					row := make([]string, len(fields))
					for j, name := range fields {
						row[j] = format.FormatValue(r.Fields[name])
					}
					rows[i] = row
				}
				return w.WriteTable(headers, rows)
			}
			return w.Write(results)
		},
	}

	cmd.Flags().StringVarP(&nodeStr, "node", "n", "", "Event notifier node ID (required)")
	cmd.Flags().StringVar(&startStr, "start", "", "Start time in RFC3339 format")
	cmd.Flags().StringVar(&endStr, "end", "", "End time in RFC3339 format (before --start: read backward)")
	cmd.Flags().StringArrayVar(&selectStr, "select", nil, "Event field to return, a BrowsePath like AckedState/Id (repeatable; default: EventId, EventType, SourceName, Time, Message, Severity)")
	cmd.Flags().StringVar(&ofTypeStr, "of-type", "", "Only events of this type or its subtypes (NodeId or name)")
	cmd.Flags().Uint16Var(&severityMin, "severity-min", 0, "Minimum Severity (inclusive)")
	cmd.Flags().Uint16Var(&severityMax, "severity-max", 0, "Maximum Severity (inclusive)")
	cmd.Flags().Uint32Var(&numValues, "num-values", 0, "Events per page (NumValuesPerNode; 0 = as the server pages)")
	cmd.Flags().Uint32Var(&maxEvents, "max-events", 0, "Stop after this many events (0 = all)")
	cmd.Flags().StringVar(&continuation, "continuation", "all", "Paging control (all = follow continuation points, single = one page only)")
	_ = cmd.RegisterFlagCompletionFunc("continuation", fixedCompletion(continuationValues))
	_ = cmd.RegisterFlagCompletionFunc("select", fixedCompletion(append(append([]string{}, historyEventFields...), "ReceiveTime", "SourceNode", "LocalTime", parse.ConditionIDField)))
	return cmd
}

// signalContext is a context cancelled by Ctrl+C or SIGTERM.
func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

// readEventDetailsObject wraps ReadEventDetails for a HistoryReadRequest.
func readEventDetailsObject(details *ua.ReadEventDetails) *ua.ExtensionObject {
	return &ua.ExtensionObject{
		TypeID:       ua.NewFourByteExpandedNodeID(0, id.ReadEventDetailsEncodingDefaultBinary),
		EncodingMask: ua.ExtensionObjectBinary,
		Value:        details,
	}
}

// readEventHistory reads the event history of nodeID: it follows the
// continuation points until the history is read, limit events (0: no
// limit) were read or, with single, after one page, and releases a
// continuation point it does not follow. The error carries the exit code.
func readEventHistory(ctx context.Context, c *opcua.Client, nodeID *ua.NodeID, details *ua.ReadEventDetails, limit uint32, single bool) ([]*ua.HistoryEventFieldList, int, error) {
	var (
		out   []*ua.HistoryEventFieldList
		cp    []byte
		pages int
	)
	for {
		opCtx, cancel := connCfg.TimeoutContext(ctx)
		resp, err := c.HistoryReadEvent(opCtx, []*ua.HistoryReadValueID{{NodeID: nodeID, ContinuationPoint: cp}}, details)
		cancel()
		if err != nil {
			return out, pages, exitCodef(3, "reading event history of %s: %w", nodeID, err)
		}
		pages++
		printOperationDiagnostics(resp.ResponseHeader, resp.DiagnosticInfos, func(int) string {
			return "history read " + nodeID.String()
		})
		if len(resp.Results) != 1 || resp.Results[0] == nil {
			return out, pages, exitCodef(3, "reading event history of %s: %d results for 1 node", nodeID, len(resp.Results))
		}
		r := resp.Results[0]
		if isBadStatus(r.StatusCode) {
			return out, pages, exitCodef(3, "reading event history of %s: %s", nodeID, r.StatusCode.Symbol())
		}
		if r.HistoryData != nil && r.HistoryData.Value != nil {
			switch he := r.HistoryData.Value.(type) {
			case *ua.HistoryEvent:
				out = append(out, he.Events...)
			case ua.HistoryEvent:
				out = append(out, he.Events...)
			default:
				releaseEventContinuation(ctx, c, nodeID, details, r.ContinuationPoint)
				return out, pages, exitCodef(3, "reading event history of %s: the server returned %T, not HistoryEvent", nodeID, he)
			}
		}
		cp = r.ContinuationPoint
		if limit > 0 && uint32(len(out)) >= limit {
			out = out[:limit]
			break
		}
		if len(cp) == 0 || single {
			break
		}
	}
	releaseEventContinuation(ctx, c, nodeID, details, cp)
	return out, pages, nil
}

// releaseEventContinuation releases a continuation point of an event read
// (HistoryRead with ReleaseContinuationPoints, Part 4 §5.10.3). A failure
// is only reported: the server frees the point with the session.
func releaseEventContinuation(ctx context.Context, c *opcua.Client, nodeID *ua.NodeID, details *ua.ReadEventDetails, cp []byte) {
	if len(cp) == 0 {
		return
	}
	opCtx, cancel := connCfg.TimeoutContext(context.WithoutCancel(ctx))
	defer cancel()
	req := &ua.HistoryReadRequest{
		HistoryReadDetails:        readEventDetailsObject(details),
		TimestampsToReturn:        ua.TimestampsToReturnBoth,
		ReleaseContinuationPoints: true,
		NodesToRead:               []*ua.HistoryReadValueID{{NodeID: nodeID, ContinuationPoint: cp}},
	}
	err := c.Send(opCtx, req, func(r ua.Response) error {
		resp, ok := r.(*ua.HistoryReadResponse)
		if !ok {
			return fmt.Errorf("unexpected response %T", r)
		}
		if len(resp.Results) == 1 && resp.Results[0] != nil && isBadStatus(resp.Results[0].StatusCode) {
			return resp.Results[0].StatusCode
		}
		return nil
	})
	if err != nil {
		format.Errf("releasing the continuation point of %s: %v", nodeID, err)
	} else if verbose {
		format.Errf("released the continuation point of %s", nodeID)
	}
}

// historyEventValue makes an event field printable: EventIds and other
// ByteStrings in base64, NodeIds, LocalizedTexts and StatusCodes as text.
func historyEventValue(v *ua.Variant) interface{} {
	if v == nil {
		return nil
	}
	switch x := v.Value().(type) {
	case nil:
		return nil
	case []byte:
		return base64.StdEncoding.EncodeToString(x)
	default:
		return jsonSafeValue(x)
	}
}

// confirmHistoryChange asks on a terminal whether to go on; without a
// terminal it goes on.
func confirmHistoryChange(prompt string) bool {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return true
	}
	fmt.Fprintf(os.Stderr, "%s [y/N] ", prompt)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "y")
}

// historyUpdateExit is the exit of a history update: 0 when every
// operation succeeded, 4 when some failed, 3 when all failed.
func historyUpdateExit(results []model.HistoryEventUpdate, what string) error {
	bad := 0
	for _, r := range results {
		if isBadStatus(ua.StatusCode(r.StatusCode)) {
			bad++
		}
	}
	switch {
	case bad == 0:
		return nil
	case bad == len(results):
		return exitCodef(3, "%s: all %d operation(s) failed", what, bad)
	default:
		return exitCodef(4, "%s: %d of %d operation(s) failed", what, bad, len(results))
	}
}

// writeHistoryEventUpdates prints the results of a history update.
func writeHistoryEventUpdates(results []model.HistoryEventUpdate, withFields bool) error {
	w := newWriter()
	if format.ParseFormat(outputFormat) != format.Table {
		return w.Write(results)
	}
	headers := []string{"NODE ID", "INDEX", "OPERATION", "EVENT ID", "STATUS", "STATUS CODE"}
	if withFields {
		headers = append(headers, "FIELDS")
	}
	rows := make([][]string, len(results))
	for i, r := range results {
		rows[i] = []string{r.NodeID, fmt.Sprintf("%d", r.Index), r.Operation, r.EventID, r.Status, fmt.Sprintf("0x%08X", r.StatusCode)}
		if withFields {
			names := make([]string, 0, len(r.Fields))
			for k := range r.Fields {
				names = append(names, k)
			}
			sort.Strings(names)
			parts := make([]string, len(names))
			for j, k := range names {
				parts[j] = fmt.Sprintf("%s=%s", k, format.FormatValue(r.Fields[k]))
			}
			rows[i] = append(rows[i], strings.Join(parts, " "))
		}
	}
	return w.WriteTable(headers, rows)
}

// historyUpdateResult is the result of one operation of a HistoryUpdate
// details: its OperationResult, or the StatusCode of the details when they
// failed as a whole.
func historyUpdateResult(r *ua.HistoryUpdateResult, i int) ua.StatusCode {
	if r == nil {
		return ua.StatusBadUnexpectedError
	}
	if isBadStatus(r.StatusCode) || i >= len(r.OperationResults) {
		return r.StatusCode
	}
	return r.OperationResults[i]
}

func newHistoryUpdateEventsCmd() *cobra.Command {
	var (
		nodeStr    string
		mode       string
		eventJSON  []string
		eventsFile string
		dryRun     bool
		yes        bool
	)

	cmd := &cobra.Command{
		Use:   "update-events",
		Short: "Insert, replace or update historical events",
		Long: `Insert, replace or update events in the event history of a notifier
(HistoryUpdate with UpdateEventDetails, Part 11 §6.9.4). The notifier needs
the HistoryWrite bit of its EventNotifier.

--mode insert adds new events (Bad_EntryExists for a known EventId), replace
replaces events matched by EventId (Bad_NoEntryExists for an unknown one; the
fields not given are dropped), update does either.

Each event is a JSON object of fields (--event, repeatable, or --events-file
with an object or an array of them; - reads stdin), named as history event
--select takes them:
  EventId     base64 or 0x hex; insert and update make one when missing
  EventType   NodeId or name (default BaseEventType for insert and update)
  Time        RFC 3339 (default now for insert and update)
  SourceNode  NodeId          SourceName  string
  Message     string or {"Locale":"en","Text":"..."}
  Severity    number 1..1000
  other fields: a string, boolean or number (Double), or any Variant in the
  OPC UA JSON encoding: {"UaType":<built-in type id>,"Value":...}
Events with the same fields go into one UpdateEventDetails.

Use --dry-run to print the events without sending them, --yes to skip the
confirmation on a terminal.

Exit codes: 3 when the HistoryUpdate fails or every event failed, 4 when
some events failed; the table lists the result of each event.

Examples:
  opcuactl history update-events -e opc.tcp://localhost:4840 -n "ns=1;s=Demo" --event '{"Message":"maintenance start","Severity":200,"SourceName":"Demo"}' --yes
  opcuactl history update-events -e opc.tcp://localhost:4840 -n "ns=1;s=Demo" --mode replace --event '{"EventId":"+LbyRwxcuik8Tz1iutx3ug==","Message":"corrected","Severity":300}' --yes
  opcuactl history update-events -e opc.tcp://localhost:4840 -n "ns=1;s=Demo" --events-file events.json --dry-run`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if nodeStr == "" {
				return exitCodef(1, "--node is required")
			}
			nodeID, err := parse.NodeID(nodeStr)
			if err != nil {
				return exitCode(1, err)
			}
			var perform ua.PerformUpdateType
			switch strings.ToLower(mode) {
			case "insert":
				perform = ua.PerformUpdateTypeInsert
			case "replace":
				perform = ua.PerformUpdateTypeReplace
			case "update":
				perform = ua.PerformUpdateTypeUpdate
			default:
				return exitCodef(1, "invalid --mode %q (insert|replace|update)", mode)
			}
			mode = strings.ToLower(mode)
			var events []parse.Event
			for _, s := range eventJSON {
				evs, err := parse.Events([]byte(s))
				if err != nil {
					return exitCodef(1, "invalid --event: %v", err)
				}
				events = append(events, evs...)
			}
			if eventsFile != "" {
				var data []byte
				if eventsFile == "-" {
					data, err = io.ReadAll(os.Stdin)
				} else {
					data, err = os.ReadFile(eventsFile)
				}
				if err != nil {
					return exitCodef(1, "reading --events-file: %v", err)
				}
				evs, err := parse.Events(data)
				if err != nil {
					return exitCodef(1, "invalid --events-file: %v", err)
				}
				events = append(events, evs...)
			}
			if len(events) == 0 {
				return exitCodef(1, "--event or --events-file is required")
			}
			now := time.Now().UTC()
			for i, ev := range events {
				if perform == ua.PerformUpdateTypeReplace {
					if ev.Field("EventId") == nil || ev.Field("EventId").Value() == nil {
						return exitCodef(1, "event %d: --mode replace needs the EventId", i+1)
					}
					continue
				}
				if v := ev.Field("EventId"); v == nil || v.Value() == nil {
					eid := make([]byte, 16)
					_, _ = rand.Read(eid)
					ev = ev.Set("EventId", ua.MustVariant(eid))
				}
				if v := ev.Field("EventType"); v == nil || v.Value() == nil {
					ev = ev.Set("EventType", ua.MustVariant(ua.NewNumericNodeID(0, id.BaseEventType)))
				}
				if v := ev.Field("Time"); v == nil || v.Value() == nil {
					ev = ev.Set("Time", ua.MustVariant(now))
				}
				events[i] = ev
			}

			results := make([]model.HistoryEventUpdate, len(events))
			for i, ev := range events {
				results[i] = model.HistoryEventUpdate{NodeID: nodeID.String(), Operation: mode, Index: i + 1}
				if b, ok := ev.Field("EventId").Value().([]byte); ok {
					results[i].EventID = base64.StdEncoding.EncodeToString(b)
				}
			}
			if dryRun {
				for i, ev := range events {
					results[i].Status = "DryRun"
					results[i].Fields = make(map[string]interface{}, len(ev))
					for _, f := range ev {
						results[i].Fields[f.Name] = historyEventValue(f.Value)
					}
				}
				return writeHistoryEventUpdates(results, true)
			}
			if !yes && !confirmHistoryChange(fmt.Sprintf("%s %d event(s) in the history of %s on %s?", strings.ToUpper(mode[:1])+mode[1:], len(events), nodeID, connCfg.Endpoint)) {
				fmt.Fprintln(os.Stderr, "Update cancelled.")
				return nil
			}

			// One UpdateEventDetails per set of fields, in the order of
			// the events.
			var details []*ua.UpdateEventDetails
			var members [][]int
			group := map[string]int{}
			for i, ev := range events {
				key := strings.Join(ev.Names(), "\x00")
				g, ok := group[key]
				if !ok {
					selects, err := parse.EventSelectClauses(ev.Names())
					if err != nil {
						return exitCodef(1, "event %d: %v", i+1, err)
					}
					g = len(details)
					group[key] = g
					details = append(details, &ua.UpdateEventDetails{
						NodeID:               nodeID,
						PerformInsertReplace: perform,
						Filter:               &ua.EventFilter{SelectClauses: selects, WhereClause: &ua.ContentFilter{}},
					})
					members = append(members, nil)
				}
				values := make([]*ua.Variant, len(ev))
				for j, f := range ev {
					values[j] = f.Value
				}
				details[g].EventData = append(details[g].EventData, &ua.HistoryEventFieldList{EventFields: values})
				members[g] = append(members[g], i)
			}

			ctx, stop := signalContext()
			defer stop()
			c, err := connCfg.NewClient(ctx)
			if err != nil {
				format.Errf("%v", err)
				return exitCode(2, err)
			}
			defer func() { _ = c.Close(context.WithoutCancel(ctx)) }()
			opCtx, cancel := connCfg.TimeoutContext(ctx)
			defer cancel()
			resp, err := c.HistoryUpdateEvents(opCtx, details...)
			if err != nil {
				format.Errf("updating the event history of %s: %v", nodeID, err)
				return exitCode(3, err)
			}
			printOperationDiagnostics(resp.ResponseHeader, resp.DiagnosticInfos, func(int) string {
				return "history update " + nodeID.String()
			})
			if len(resp.Results) != len(details) {
				return exitCodef(3, "updating the event history of %s: %d results for %d details", nodeID, len(resp.Results), len(details))
			}
			for g, idx := range members {
				for j, i := range idx {
					st := historyUpdateResult(resp.Results[g], j)
					results[i].Status, results[i].StatusCode = st.Symbol(), st.Uint32()
				}
			}
			if err := writeHistoryEventUpdates(results, false); err != nil {
				return err
			}
			return historyUpdateExit(results, "updating the event history of "+nodeID.String())
		},
	}

	cmd.Flags().StringVarP(&nodeStr, "node", "n", "", "Event notifier node ID (required)")
	cmd.Flags().StringVar(&mode, "mode", "insert", "PerformUpdateType (insert|replace|update)")
	cmd.Flags().StringArrayVar(&eventJSON, "event", nil, "Event as a JSON object of fields, or an array of them (repeatable)")
	cmd.Flags().StringVar(&eventsFile, "events-file", "", "JSON file with an event or an array of events (- for stdin)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print the events, do not send them")
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip the confirmation prompt")
	_ = cmd.RegisterFlagCompletionFunc("mode", fixedCompletion(historyEventModeValues))
	return cmd
}

func newHistoryDeleteEventsCmd() *cobra.Command {
	var (
		nodeStr  string
		eventIDs []string
		dryRun   bool
		yes      bool
	)

	cmd := &cobra.Command{
		Use:   "delete-events",
		Short: "Delete historical events by EventId",
		Long: `Delete events from the event history of a notifier by their EventId
(HistoryUpdate with DeleteEventDetails, Part 11 §6.9.7). The notifier needs
the HistoryWrite bit of its EventNotifier.

--event-id takes the EventId in base64, as history event prints it, or hex
with 0x (repeatable). An EventId the history does not have is Bad_NoData.

Use --dry-run to print what would be deleted, --yes to skip the confirmation
on a terminal.

Exit codes: 3 when the HistoryUpdate fails or no event was deleted, 4 when
some of the EventIds failed.

Examples:
  opcuactl history delete-events -e opc.tcp://localhost:4840 -n "ns=1;s=Demo" --event-id +LbyRwxcuik8Tz1iutx3ug== --yes
  opcuactl history delete-events -e opc.tcp://localhost:4840 -n "ns=1;s=Demo" --event-id 0x0102 --event-id 0x0304 --dry-run`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if nodeStr == "" {
				return exitCodef(1, "--node is required")
			}
			nodeID, err := parse.NodeID(nodeStr)
			if err != nil {
				return exitCode(1, err)
			}
			if len(eventIDs) == 0 {
				return exitCodef(1, "--event-id is required")
			}
			ids := make([][]byte, len(eventIDs))
			results := make([]model.HistoryEventUpdate, len(eventIDs))
			for i, s := range eventIDs {
				b, latest, err := parse.EventID(s)
				if err != nil || latest {
					return exitCodef(1, "invalid --event-id %q (base64 or 0x hex)", s)
				}
				ids[i] = b
				results[i] = model.HistoryEventUpdate{
					NodeID: nodeID.String(), Operation: "delete", Index: i + 1,
					EventID: base64.StdEncoding.EncodeToString(b),
				}
			}
			if dryRun {
				for i := range results {
					results[i].Status = "DryRun"
				}
				return writeHistoryEventUpdates(results, false)
			}
			if !yes && !confirmHistoryChange(fmt.Sprintf("Delete %d event(s) from the history of %s on %s?", len(ids), nodeID, connCfg.Endpoint)) {
				fmt.Fprintln(os.Stderr, "Delete cancelled.")
				return nil
			}

			ctx, stop := signalContext()
			defer stop()
			c, err := connCfg.NewClient(ctx)
			if err != nil {
				format.Errf("%v", err)
				return exitCode(2, err)
			}
			defer func() { _ = c.Close(context.WithoutCancel(ctx)) }()
			opCtx, cancel := connCfg.TimeoutContext(ctx)
			defer cancel()
			resp, err := c.HistoryDeleteEvents(opCtx, &ua.DeleteEventDetails{NodeID: nodeID, EventIDs: ids})
			if err != nil {
				format.Errf("deleting from the event history of %s: %v", nodeID, err)
				return exitCode(3, err)
			}
			printOperationDiagnostics(resp.ResponseHeader, resp.DiagnosticInfos, func(int) string {
				return "history delete " + nodeID.String()
			})
			if len(resp.Results) != 1 {
				return exitCodef(3, "deleting from the event history of %s: %d results for 1 details", nodeID, len(resp.Results))
			}
			for i := range results {
				st := historyUpdateResult(resp.Results[0], i)
				results[i].Status, results[i].StatusCode = st.Symbol(), st.Uint32()
			}
			if err := writeHistoryEventUpdates(results, false); err != nil {
				return err
			}
			return historyUpdateExit(results, "deleting from the event history of "+nodeID.String())
		},
	}

	cmd.Flags().StringVarP(&nodeStr, "node", "n", "", "Event notifier node ID (required)")
	cmd.Flags().StringArrayVar(&eventIDs, "event-id", nil, "EventId to delete, base64 or 0x hex (repeatable, required)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print what would be deleted, do not send it")
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip the confirmation prompt")
	return cmd
}
