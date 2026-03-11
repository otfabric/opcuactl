package opcuactl

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/otfabric/opcua/id"
	"github.com/otfabric/opcua/ua"
	"github.com/otfabric/opcuactl/internal/format"
	"github.com/otfabric/opcuactl/internal/model"
	"github.com/otfabric/opcuactl/internal/parse"
	"github.com/spf13/cobra"
)

func newHistoryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "history",
		Short: "Read historical data",
		Long: `Read historical values and events from the server.

Examples:
  opcuactl history value -e opc.tcp://localhost:4840 -n i=2258 --start 2026-03-01T00:00:00Z --end 2026-03-11T00:00:00Z`,
	}
	cmd.AddCommand(newHistoryValueCmd(), newHistoryEventCmd())
	return cmd
}

func newHistoryValueCmd() *cobra.Command {
	var (
		nodeStr      string
		startStr     string
		endStr       string
		maxValues    uint32
		mode         string
		aggInterval  time.Duration
		continuation string
	)

	cmd := &cobra.Command{
		Use:   "value",
		Short: "Read historical values for a node",
		Long: `Read historical data values within a time range.

The start and end times must be in RFC3339 format (e.g. 2026-03-01T00:00:00Z).
Use --max-values to limit the number of returned values (0 = all).
Use --mode to select the history read mode (raw, modified, processed, attime).
Use --interval with --mode processed to set the aggregate interval.
Use --continuation to control pagination (all = auto-follow, single = one page only).

Examples:
  opcuactl history value -e opc.tcp://localhost:4840 -n i=2258 --start 2026-03-01T00:00:00Z --end 2026-03-11T00:00:00Z
  opcuactl history value -e opc.tcp://localhost:4840 -n i=2258 --start 2026-03-10T00:00:00Z --end 2026-03-11T00:00:00Z --max-values 100
  opcuactl history value -e opc.tcp://localhost:4840 -n i=2258 --start 2026-03-10T00:00:00Z --end 2026-03-11T00:00:00Z --mode modified
  opcuactl history value -e opc.tcp://localhost:4840 -n i=2258 --start 2026-03-10T00:00:00Z --end 2026-03-11T00:00:00Z --continuation single
  opcuactl history value -e opc.tcp://localhost:4840 -n i=2258 --start 2026-03-10T00:00:00Z --end 2026-03-11T00:00:00Z --format json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if nodeStr == "" {
				return exitCodef(1, "--node is required")
			}
			if startStr == "" || endStr == "" {
				return exitCodef(1, "--start and --end are required")
			}

			nodeID, err := parse.NodeID(nodeStr)
			if err != nil {
				return exitCode(1, err)
			}

			startTime, err := time.Parse(time.RFC3339, startStr)
			if err != nil {
				return exitCodef(1, "invalid --start time: %v", err)
			}
			endTime, err := time.Parse(time.RFC3339, endStr)
			if err != nil {
				return exitCodef(1, "invalid --end time: %v", err)
			}

			if !endTime.After(startTime) {
				return exitCodef(1, "--end must be after --start")
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			c, err := connCfg.NewClient(ctx)
			if err != nil {
				format.Errf("%v", err)
				return exitCode(2, err)
			}
			defer func() { _ = c.Close(ctx) }()

			opCtx, opCancel := connCfg.TimeoutContext(ctx)
			defer opCancel()

			var results []model.HistoryValue

			switch strings.ToLower(mode) {
			case "modified":
				nodes := []*ua.HistoryReadValueID{{NodeID: nodeID}}
				details := &ua.ReadRawModifiedDetails{
					IsReadModified:   true,
					StartTime:        startTime,
					EndTime:          endTime,
					NumValuesPerNode: maxValues,
				}
				resp, err := c.HistoryReadRawModified(opCtx, nodes, details)
				if err != nil {
					format.Errf("reading modified history for %s: %v", nodeID, err)
					return exitCode(3, err)
				}
				results = historyResponseToValues(nodeID.String(), resp)

			case "processed":
				if aggInterval == 0 {
					return exitCodef(1, "--interval is required for processed mode")
				}
				nodes := []*ua.HistoryReadValueID{{NodeID: nodeID}}
				details := &ua.ReadProcessedDetails{
					StartTime:          startTime,
					EndTime:            endTime,
					ProcessingInterval: float64(aggInterval / time.Millisecond),
				}
				resp, err := c.HistoryReadProcessed(opCtx, nodes, details)
				if err != nil {
					format.Errf("reading processed history for %s: %v", nodeID, err)
					return exitCode(3, err)
				}
				results = historyResponseToValues(nodeID.String(), resp)

			case "attime":
				return exitCodef(1, "attime mode requires specific timestamps, not yet supported via CLI")

			default: // "raw" or ""
				if continuation == "single" || maxValues > 0 {
					values, err := c.ReadHistory(opCtx, nodeID, startTime, endTime, maxValues)
					if err != nil {
						format.Errf("reading history for %s: %v", nodeID, err)
						return exitCode(3, err)
					}
					results = historyDataValues(nodeID.String(), values)
				} else {
					for dv, err := range c.ReadHistoryAll(ctx, nodeID, startTime, endTime) {
						if err != nil {
							format.Errf("reading history for %s: %v", nodeID, err)
							return exitCode(3, err)
						}
						results = append(results, toHistoryValue(nodeID.String(), dv))
					}
				}
			}

			if verbose {
				format.Errf("returned %d historical value(s) for %s (mode=%s)", len(results), nodeID, mode)
			}

			w := newWriter()
			f := format.ParseFormat(outputFormat)
			if f == format.Table {
				headers := []string{"SOURCE TIMESTAMP", "VALUE", "TYPE", "STATUS"}
				rows := make([][]string, len(results))
				for i, r := range results {
					rows[i] = []string{r.SourceTimestamp, format.FormatValue(r.Value), r.Type, r.Status}
				}
				return w.WriteTable(headers, rows)
			}
			return w.Write(results)
		},
	}

	cmd.Flags().StringVarP(&nodeStr, "node", "n", "", "Node ID (required)")
	cmd.Flags().StringVar(&startStr, "start", "", "Start time in RFC3339 format (required)")
	cmd.Flags().StringVar(&endStr, "end", "", "End time in RFC3339 format (required)")
	cmd.Flags().Uint32Var(&maxValues, "max-values", 0, "Maximum values to return (0 = all, uses pagination)")
	cmd.Flags().StringVar(&mode, "mode", "raw", "History read mode (raw|modified|processed|attime)")
	cmd.Flags().DurationVar(&aggInterval, "interval", 0, "Aggregate interval for processed mode")
	cmd.Flags().StringVar(&continuation, "continuation", "all", "Paging control (all = auto-follow, single = one page only)")
	_ = cmd.RegisterFlagCompletionFunc("mode", fixedCompletion(historyModeValues))
	_ = cmd.RegisterFlagCompletionFunc("continuation", fixedCompletion(continuationValues))
	return cmd
}

func newHistoryEventCmd() *cobra.Command {
	var (
		nodeStr   string
		startStr  string
		endStr    string
		selectStr []string
		maxEvents uint32
	)

	defaultFields := []string{"EventType", "SourceName", "Message", "Severity", "Time"}

	cmd := &cobra.Command{
		Use:   "event",
		Short: "Read historical events for a node",
		Long: `Read historical event data within a time range.

The start and end times must be in RFC3339 format (e.g. 2026-03-01T00:00:00Z).
Use --select to choose event fields (default: EventType, SourceName, Message, Severity, Time).

Examples:
  opcuactl history event -e opc.tcp://localhost:4840 -n i=2253 --start 2026-03-01T00:00:00Z --end 2026-03-11T00:00:00Z
  opcuactl history event -e opc.tcp://localhost:4840 -n i=2253 --start 2026-03-10T00:00:00Z --end 2026-03-11T00:00:00Z --select EventType --select Message
  opcuactl history event -e opc.tcp://localhost:4840 -n i=2253 --start 2026-03-10T00:00:00Z --end 2026-03-11T00:00:00Z --format json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if nodeStr == "" {
				return exitCodef(1, "--node is required")
			}
			if startStr == "" || endStr == "" {
				return exitCodef(1, "--start and --end are required")
			}

			nodeID, err := parse.NodeID(nodeStr)
			if err != nil {
				return exitCode(1, err)
			}

			startTime, err := time.Parse(time.RFC3339, startStr)
			if err != nil {
				return exitCodef(1, "invalid --start time: %v", err)
			}
			endTime, err := time.Parse(time.RFC3339, endStr)
			if err != nil {
				return exitCodef(1, "invalid --end time: %v", err)
			}

			if !endTime.After(startTime) {
				return exitCodef(1, "--end must be after --start")
			}

			fields := defaultFields
			if len(selectStr) > 0 {
				fields = selectStr
			}

			// Build event filter
			selects := make([]*ua.SimpleAttributeOperand, len(fields))
			for i, name := range fields {
				selects[i] = &ua.SimpleAttributeOperand{
					TypeDefinitionID: ua.NewNumericNodeID(0, id.BaseEventType),
					BrowsePath:       []*ua.QualifiedName{{NamespaceIndex: 0, Name: name}},
					AttributeID:      ua.AttributeIDValue,
				}
			}

			filter := &ua.EventFilter{
				SelectClauses: selects,
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			c, err := connCfg.NewClient(ctx)
			if err != nil {
				format.Errf("%v", err)
				return exitCode(2, err)
			}
			defer func() { _ = c.Close(ctx) }()

			opCtx, opCancel := connCfg.TimeoutContext(ctx)
			defer opCancel()

			details := &ua.ReadEventDetails{
				NumValuesPerNode: maxEvents,
				StartTime:        startTime,
				EndTime:          endTime,
				Filter:           filter,
			}

			nodes := []*ua.HistoryReadValueID{
				{NodeID: nodeID},
			}

			resp, err := c.HistoryReadEvent(opCtx, nodes, details)
			if err != nil {
				format.Errf("reading event history for %s: %v", nodeID, err)
				return exitCode(3, err)
			}

			var results []model.HistoryEventRecord
			for _, result := range resp.Results {
				if result.StatusCode != ua.StatusOK {
					format.Errf("history result status: %s", result.StatusCode.Error())
					continue
				}
				if result.HistoryData == nil || result.HistoryData.Value == nil {
					continue
				}

				he, ok := result.HistoryData.Value.(*ua.HistoryEvent)
				if !ok {
					continue
				}

				for _, efl := range he.Events {
					rec := model.HistoryEventRecord{
						NodeID: nodeID.String(),
						Fields: make(map[string]interface{}),
					}
					for i, v := range efl.EventFields {
						name := "unknown"
						if i < len(fields) {
							name = fields[i]
						}
						if v != nil {
							rec.Fields[name] = v.Value()
						}
					}
					results = append(results, rec)
				}
			}

			if verbose {
				format.Errf("returned %d historical event(s) for %s", len(results), nodeID)
			}

			w := newWriter()
			f := format.ParseFormat(outputFormat)
			if f == format.Table {
				headers := make([]string, len(fields))
				for i, name := range fields {
					headers[i] = strings.ToUpper(name)
				}
				rows := make([][]string, len(results))
				for i, r := range results {
					row := make([]string, len(fields))
					for j, name := range fields {
						if v, ok := r.Fields[name]; ok {
							row[j] = fmt.Sprintf("%v", v)
						}
					}
					rows[i] = row
				}
				return w.WriteTable(headers, rows)
			}
			return w.Write(results)
		},
	}

	cmd.Flags().StringVarP(&nodeStr, "node", "n", "", "Node ID (required)")
	cmd.Flags().StringVar(&startStr, "start", "", "Start time in RFC3339 format (required)")
	cmd.Flags().StringVar(&endStr, "end", "", "End time in RFC3339 format (required)")
	cmd.Flags().StringArrayVar(&selectStr, "select", nil, "Event field to return (repeatable; default: EventType, SourceName, Message, Severity, Time)")
	cmd.Flags().Uint32Var(&maxEvents, "max-events", 0, "Maximum events per node (0 = server default)")
	return cmd
}

func historyDataValues(nodeID string, dvs []*ua.DataValue) []model.HistoryValue {
	results := make([]model.HistoryValue, 0, len(dvs))
	for _, dv := range dvs {
		results = append(results, toHistoryValue(nodeID, dv))
	}
	return results
}

func historyResponseToValues(nodeID string, resp *ua.HistoryReadResponse) []model.HistoryValue {
	var results []model.HistoryValue
	for _, r := range resp.Results {
		if r.StatusCode != ua.StatusOK {
			continue
		}
		if r.HistoryData == nil || r.HistoryData.Value == nil {
			continue
		}
		hd, ok := r.HistoryData.Value.(*ua.HistoryData)
		if !ok {
			continue
		}
		for _, dv := range hd.DataValues {
			results = append(results, toHistoryValue(nodeID, dv))
		}
	}
	return results
}

func toHistoryValue(nodeID string, dv *ua.DataValue) model.HistoryValue {
	hv := model.HistoryValue{
		NodeID:          nodeID,
		Status:          dv.Status.Symbol(),
		StatusCode:      dv.Status.Uint32(),
		SourceTimestamp: model.FormatTimestamp(dv.SourceTimestamp),
		ServerTimestamp: model.FormatTimestamp(dv.ServerTimestamp),
	}
	if dv.Value != nil {
		hv.Value = dv.Value.Value()
		hv.Type = dv.Value.Type().String()
	}
	return hv
}
