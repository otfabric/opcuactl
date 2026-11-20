// SPDX-License-Identifier: MIT

package opcuactl

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/opcuactl/internal/format"
	"github.com/otfabric/opcuactl/internal/model"
	"github.com/otfabric/opcuactl/internal/parse"
	"github.com/spf13/cobra"
)

// historySample is a sample of --sample or --samples-file.
type historySample struct {
	time  time.Time
	value any
}

// parseSampleTime parses the time of a sample: RFC 3339 or now.
func parseSampleTime(s string, now time.Time) (time.Time, error) {
	if strings.EqualFold(s, "now") {
		return now, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("time %q: want RFC 3339 (2026-03-01T12:00:00Z) or now", s)
	}
	return t.UTC(), nil
}

// sampleValue converts the text of a value to typ, as write value does.
func sampleValue(s, typ string) (any, error) {
	if parse.IsUAEncodedType(typ) {
		v, err := parse.UAVariant(s, typ)
		if err != nil {
			return nil, err
		}
		return v.Value(), nil
	}
	return parse.CoerceValue(s, typ)
}

// parseSamples reads the samples of --sample TIME=VALUE and of
// --samples-file, a JSON array of {"time": ..., "value": ...}.
func parseSamples(flags []string, file, typ string, now time.Time) ([]historySample, error) {
	var out []historySample
	for _, s := range flags {
		ts, val, ok := strings.Cut(s, "=")
		if !ok {
			return nil, fmt.Errorf("invalid --sample %q: want TIME=VALUE", s)
		}
		t, err := parseSampleTime(ts, now)
		if err != nil {
			return nil, fmt.Errorf("invalid --sample: %w", err)
		}
		v, err := sampleValue(val, typ)
		if err != nil {
			return nil, fmt.Errorf("invalid --sample value %q: %w", val, err)
		}
		out = append(out, historySample{time: t, value: v})
	}
	if file == "" {
		return out, nil
	}
	var data []byte
	var err error
	if file == "-" {
		data, err = io.ReadAll(os.Stdin)
	} else {
		data, err = os.ReadFile(file)
	}
	if err != nil {
		return nil, fmt.Errorf("reading --samples-file: %w", err)
	}
	var entries []struct {
		Time  string          `json:"time"`
		Value json.RawMessage `json:"value"`
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&entries); err != nil {
		return nil, fmt.Errorf("invalid --samples-file: want an array of {\"time\": ..., \"value\": ...}: %w", err)
	}
	for i, e := range entries {
		t, err := parseSampleTime(e.Time, now)
		if err != nil {
			return nil, fmt.Errorf("--samples-file entry %d: %w", i+1, err)
		}
		if len(e.Value) == 0 {
			return nil, fmt.Errorf("--samples-file entry %d: no value", i+1)
		}
		// A JSON string is the text of the value, other JSON (numbers,
		// booleans, the UA JSON of ua-json) is taken as it is.
		text := string(e.Value)
		var str string
		if !parse.IsUAEncodedType(typ) && json.Unmarshal(e.Value, &str) == nil {
			text = str
		}
		v, err := sampleValue(text, typ)
		if err != nil {
			return nil, fmt.Errorf("--samples-file entry %d: value %s: %w", i+1, e.Value, err)
		}
		out = append(out, historySample{time: t, value: v})
	}
	return out, nil
}

// historyValueExit is the exit of a history value change: 0 when every
// operation succeeded, 7 when some failed, 6 when all failed.
func historyValueExit(results []model.HistoryValueUpdate, what string) error {
	events := make([]model.HistoryEventUpdate, len(results))
	for i, r := range results {
		events[i].StatusCode = r.StatusCode
	}
	return historyUpdateExit(events, what)
}

// historyValueResponse fills in the statuses of results from the response
// to the one HistoryUpdateDetails of a value change, prints them and
// returns the exit. what names the change in errors, op in diagnostics;
// whole gives every result the status of the details, for a range delete.
func historyValueResponse(resp *ua.HistoryUpdateResponse, err error, results []model.HistoryValueUpdate, what, op string, whole bool) error {
	if err != nil {
		format.Errf("%s: %v", what, err)
		return exitCode(exitProtocol, err)
	}
	printOperationDiagnostics(resp.ResponseHeader, resp.DiagnosticInfos, func(int) string { return op })
	if len(resp.Results) != 1 {
		return exitCodef(exitProtocol, "%s: %d results for 1 details", what, len(resp.Results))
	}
	for i := range results {
		st := historyUpdateResult(resp.Results[0], i)
		if whole && resp.Results[0] != nil {
			st = resp.Results[0].StatusCode
		}
		results[i].Status, results[i].StatusCode = st.Symbol(), st.Uint32()
	}
	if err := writeHistoryValueUpdates(results); err != nil {
		return err
	}
	return historyValueExit(results, what)
}

// writeHistoryValueUpdates prints the results of a history value change.
func writeHistoryValueUpdates(results []model.HistoryValueUpdate) error {
	w := newWriter()
	if format.ParseFormat(outputFormat) != format.Table {
		return w.Write(results)
	}
	headers := []string{"NODE ID", "INDEX", "OPERATION", "TIME", "VALUE", "STATUS", "STATUS CODE"}
	rows := make([][]string, len(results))
	for i, r := range results {
		when := r.Time
		if r.Start != "" || r.End != "" {
			when = r.Start + " .. " + r.End
		}
		val := ""
		if r.Value != nil {
			val = format.FormatValue(r.Value)
		}
		rows[i] = []string{r.NodeID, fmt.Sprintf("%d", r.Index), r.Operation, when, val, r.Status, fmt.Sprintf("0x%08X", r.StatusCode)}
	}
	return w.WriteTable(headers, rows)
}

func newHistoryUpdateValuesCmd() *cobra.Command {
	var (
		nodeStr     string
		mode        string
		samples     []string
		samplesFile string
		typeName    string
		dryRun      bool
		yes         bool
	)
	cmd := &cobra.Command{
		Use:   "update-values",
		Short: "Insert, replace or update historical values",
		Long: `Insert, replace or update samples in the value history of a variable
(HistoryUpdate with UpdateDataDetails, Part 11 §6.9.2), for example to
backfill or correct a historian. The variable needs the HistoryWrite bit of
its AccessLevel.

--mode insert adds samples (Bad_EntryExists where the history has one at the
time), replace replaces samples (Bad_NoEntryExists where it has none), update
does either.

A sample is --sample TIME=VALUE (repeatable): TIME in RFC 3339 or now, VALUE
converted with --type as write value does (default double; ua-json and
ua-xml take a Variant in the OPC UA JSON or XML encoding). --samples-file
reads a JSON array of {"time": "2026-03-01T12:00:00Z", "value": 12.5}
(- reads stdin). The time is the SourceTimestamp of the sample.

Use --dry-run to print the samples without sending them, --yes to skip the
confirmation on a terminal.

Exit codes: 6 when the HistoryUpdate fails or every sample failed, 7 when
some samples failed; the table lists the result of each sample.

Examples:
  opcuactl history update-values -e opc.tcp://localhost:4840 -n "ns=1;s=Demo.Sine" --sample 2026-03-01T12:00:00Z=0.5 --yes
  opcuactl history update-values -e opc.tcp://localhost:4840 -n "ns=1;s=Demo.Sine" --mode replace --sample 2026-03-01T12:00:00Z=0.7 --yes
  opcuactl history update-values -e opc.tcp://localhost:4840 -n "ns=2;s=Count" --type int32 --samples-file backfill.json --dry-run`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if nodeStr == "" {
				return exitCodef(exitUsage, "--node is required")
			}
			nodeID, err := parse.NodeID(nodeStr)
			if err != nil {
				return exitCode(exitUsage, err)
			}
			mode = strings.ToLower(mode)
			var perform ua.PerformUpdateType
			switch mode {
			case "insert":
				perform = ua.PerformUpdateTypeInsert
			case "replace":
				perform = ua.PerformUpdateTypeReplace
			case "update":
				perform = ua.PerformUpdateTypeUpdate
			default:
				return exitCodef(exitUsage, "invalid --mode %q (insert|replace|update)", mode)
			}
			list, err := parseSamples(samples, samplesFile, typeName, time.Now().UTC())
			if err != nil {
				return exitCode(exitUsage, err)
			}
			if len(list) == 0 {
				return exitCodef(exitUsage, "--sample or --samples-file is required")
			}
			results := make([]model.HistoryValueUpdate, len(list))
			values := make([]*ua.DataValue, len(list))
			for i, s := range list {
				v, err := ua.NewVariant(s.value)
				if err != nil {
					return exitCodef(exitUsage, "sample %d: %v", i+1, err)
				}
				values[i] = &ua.DataValue{EncodingMask: ua.DataValueValue | ua.DataValueSourceTimestamp, Value: v, SourceTimestamp: s.time}
				results[i] = model.HistoryValueUpdate{NodeID: nodeID.String(), Operation: mode, Index: i + 1,
					Time: s.time.Format(time.RFC3339Nano), Value: jsonSafeValue(s.value)}
			}
			if dryRun {
				for i := range results {
					results[i].Status = "DryRun"
				}
				return writeHistoryValueUpdates(results)
			}
			if !yes && !confirmHistoryChange(fmt.Sprintf("%s %d sample(s) in the history of %s on %s?", strings.ToUpper(mode[:1])+mode[1:], len(list), nodeID, connCfg.Endpoint)) {
				fmt.Fprintln(os.Stderr, "Update cancelled.")
				return nil
			}
			ctx, stop := signalContext()
			defer stop()
			c, err := connCfg.NewClient(ctx)
			if err != nil {
				format.Errf("%v", err)
				return exitCode(exitTransport, err)
			}
			defer func() { _ = c.Close(context.WithoutCancel(ctx)) }()
			opCtx, cancel := connCfg.TimeoutContext(ctx)
			defer cancel()
			resp, err := c.HistoryUpdateData(opCtx, &ua.UpdateDataDetails{NodeID: nodeID, PerformInsertReplace: perform, UpdateValues: values})
			return historyValueResponse(resp, err, results, "updating the history of "+nodeID.String(), "history update "+nodeID.String(), false)
		},
	}
	cmd.Flags().StringVarP(&nodeStr, "node", "n", "", "Variable node ID (required)")
	cmd.Flags().StringVar(&mode, "mode", "insert", "PerformUpdateType (insert|replace|update)")
	cmd.Flags().StringArrayVar(&samples, "sample", nil, "Sample TIME=VALUE, TIME in RFC 3339 or now (repeatable)")
	cmd.Flags().StringVar(&samplesFile, "samples-file", "", `JSON file with an array of {"time": ..., "value": ...} (- for stdin)`)
	cmd.Flags().StringVar(&typeName, "type", "double", "Value type, as write value --type")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print the samples, do not send them")
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip the confirmation prompt")
	_ = cmd.RegisterFlagCompletionFunc("mode", fixedCompletion(historyEventModeValues))
	_ = cmd.RegisterFlagCompletionFunc("type", fixedCompletion(writeTypeValues))
	return cmd
}

func newHistoryDeleteValuesCmd() *cobra.Command {
	var (
		nodeStr  string
		startStr string
		endStr   string
		modified bool
		at       []string
		dryRun   bool
		yes      bool
	)
	cmd := &cobra.Command{
		Use:   "delete-values",
		Short: "Delete historical values",
		Long: `Delete samples from the value history of a variable. The variable needs
the HistoryWrite bit of its AccessLevel.

--start and --end delete the samples of a time range (HistoryUpdate with
DeleteRawModifiedDetails, Part 11 §6.9.5); --modified deletes the modified
values the history keeps of the range instead of the samples. --at deletes
the samples at these times (DeleteAtTimeDetails, Part 11 §6.9.6;
repeatable), Bad_NoEntryExists where the history has none. Times are RFC 3339.

Use --dry-run to print what would be deleted, --yes to skip the confirmation
on a terminal.

Exit codes: 6 when the HistoryUpdate fails or nothing was deleted, 7 when
some of the --at times failed.

Examples:
  opcuactl history delete-values -e opc.tcp://localhost:4840 -n "ns=1;s=Demo.Sine" --start 2026-03-01T00:00:00Z --end 2026-03-02T00:00:00Z --yes
  opcuactl history delete-values -e opc.tcp://localhost:4840 -n "ns=1;s=Demo.Sine" --at 2026-03-01T12:00:00Z --at 2026-03-01T13:00:00Z --dry-run`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if nodeStr == "" {
				return exitCodef(exitUsage, "--node is required")
			}
			nodeID, err := parse.NodeID(nodeStr)
			if err != nil {
				return exitCode(exitUsage, err)
			}
			byRange := startStr != "" || endStr != ""
			switch {
			case byRange && len(at) > 0:
				return exitCodef(exitUsage, "--at deletes single samples, --start/--end a range: give one of them")
			case !byRange && len(at) == 0:
				return exitCodef(exitUsage, "--start and --end, or --at, are required")
			case modified && !byRange:
				return exitCodef(exitUsage, "--modified applies to --start and --end")
			}
			var results []model.HistoryValueUpdate
			var start, end time.Time
			var times []time.Time
			if byRange {
				if startStr == "" || endStr == "" {
					return exitCodef(exitUsage, "--start and --end are required together")
				}
				if start, err = time.Parse(time.RFC3339, startStr); err != nil {
					return exitCodef(exitUsage, "invalid --start time: %v", err)
				}
				if end, err = time.Parse(time.RFC3339, endStr); err != nil {
					return exitCodef(exitUsage, "invalid --end time: %v", err)
				}
				if !end.After(start) {
					return exitCodef(exitUsage, "--end must be after --start")
				}
				op := "delete"
				if modified {
					op = "delete-modified"
				}
				results = []model.HistoryValueUpdate{{NodeID: nodeID.String(), Operation: op, Index: 1,
					Start: start.UTC().Format(time.RFC3339Nano), End: end.UTC().Format(time.RFC3339Nano)}}
			} else {
				for i, s := range at {
					t, err := time.Parse(time.RFC3339, s)
					if err != nil {
						return exitCodef(exitUsage, "invalid --at time %q: %v", s, err)
					}
					times = append(times, t.UTC())
					results = append(results, model.HistoryValueUpdate{NodeID: nodeID.String(), Operation: "delete", Index: i + 1, Time: t.UTC().Format(time.RFC3339Nano)})
				}
			}
			if dryRun {
				for i := range results {
					results[i].Status = "DryRun"
				}
				return writeHistoryValueUpdates(results)
			}
			what := fmt.Sprintf("the samples from %s to %s", startStr, endStr)
			if !byRange {
				what = fmt.Sprintf("%d sample(s)", len(times))
			}
			if !yes && !confirmHistoryChange(fmt.Sprintf("Delete %s from the history of %s on %s?", what, nodeID, connCfg.Endpoint)) {
				fmt.Fprintln(os.Stderr, "Delete cancelled.")
				return nil
			}
			ctx, stop := signalContext()
			defer stop()
			c, err := connCfg.NewClient(ctx)
			if err != nil {
				format.Errf("%v", err)
				return exitCode(exitTransport, err)
			}
			defer func() { _ = c.Close(context.WithoutCancel(ctx)) }()
			opCtx, cancel := connCfg.TimeoutContext(ctx)
			defer cancel()
			var resp *ua.HistoryUpdateResponse
			if byRange {
				resp, err = c.HistoryDeleteRawModified(opCtx, &ua.DeleteRawModifiedDetails{NodeID: nodeID, IsDeleteModified: modified, StartTime: start, EndTime: end})
			} else {
				resp, err = c.HistoryDeleteAtTime(opCtx, &ua.DeleteAtTimeDetails{NodeID: nodeID, ReqTimes: times})
			}
			return historyValueResponse(resp, err, results, "deleting from the history of "+nodeID.String(), "history delete "+nodeID.String(), byRange)
		},
	}
	cmd.Flags().StringVarP(&nodeStr, "node", "n", "", "Variable node ID (required)")
	cmd.Flags().StringVar(&startStr, "start", "", "Start of the range to delete, RFC 3339")
	cmd.Flags().StringVar(&endStr, "end", "", "End of the range to delete, RFC 3339")
	cmd.Flags().BoolVar(&modified, "modified", false, "With --start/--end: delete the modified values of the range, not the samples")
	cmd.Flags().StringArrayVar(&at, "at", nil, "Delete the sample at this time, RFC 3339 (repeatable)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print what would be deleted, do not send it")
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip the confirmation prompt")
	return cmd
}
