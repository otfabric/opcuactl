// SPDX-License-Identifier: MIT

package opcuactl

import (
	"context"
	"strings"
	"time"

	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/opcuactl/internal/format"
	"github.com/otfabric/opcuactl/internal/model"
	"github.com/otfabric/opcuactl/internal/parse"
	"github.com/spf13/cobra"
)

func newHistoryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "history",
		Short: "Read and update historical data and events",
		Long: `Read historical values and events from the server, and insert, replace,
update or delete historical events (Part 11).

Examples:
  opcuactl history value -e opc.tcp://localhost:4840 -n i=2258 --start 2026-03-01T00:00:00Z --end 2026-03-11T00:00:00Z
  opcuactl history event -e opc.tcp://localhost:4840 -n "ns=1;s=Demo" --start 2026-03-01T00:00:00Z --end 2026-03-11T00:00:00Z
  opcuactl history update-events -e opc.tcp://localhost:4840 -n "ns=1;s=Demo" --event '{"Message":"note","Severity":200}' --yes
  opcuactl history delete-events -e opc.tcp://localhost:4840 -n "ns=1;s=Demo" --event-id +LbyRwxcuik8Tz1iutx3ug== --yes`,
	}
	cmd.AddCommand(newHistoryValueCmd(), newHistoryEventCmd(), newHistoryUpdateEventsCmd(), newHistoryDeleteEventsCmd())
	return cmd
}

func newHistoryValueCmd() *cobra.Command {
	var (
		nodeStr      string
		startStr     string
		endStr       string
		atTimes      []string
		maxValues    uint32
		mode         string
		aggregate    string
		aggInterval  time.Duration
		continuation string
	)

	cmd := &cobra.Command{
		Use:   "value",
		Short: "Read historical values for a node",
		Long: `Read historical data values within a time range.

The start and end times must be in RFC3339 format (e.g. 2026-03-01T00:00:00Z),
except for --mode attime which uses repeatable --at timestamps instead.

Use --mode to select the history read mode (raw, modified, processed, attime).
Use --interval with --mode processed to set the aggregate interval (ProcessingInterval in ms).
Use --aggregate with --mode processed (average|min|max|count; default average).
Use --continuation to control pagination for raw mode (all = auto-follow, single = one page only).

Examples:
  opcuactl history value -e opc.tcp://localhost:4840 -n i=2258 --start 2026-03-01T00:00:00Z --end 2026-03-11T00:00:00Z
  opcuactl history value -e opc.tcp://localhost:4840 -n i=2258 --start 2026-03-10T00:00:00Z --end 2026-03-11T00:00:00Z --mode modified
  opcuactl history value -e opc.tcp://localhost:4840 -n i=2258 --start 2026-03-10T00:00:00Z --end 2026-03-11T00:00:00Z --mode processed --interval 1h --aggregate average
  opcuactl history value -e opc.tcp://localhost:4840 -n i=2258 --mode attime --at 2026-03-10T12:00:00Z --at 2026-03-10T13:00:00Z
  opcuactl history value -e opc.tcp://localhost:4840 -n i=2258 --start 2026-03-10T00:00:00Z --end 2026-03-11T00:00:00Z --format json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if nodeStr == "" {
				return exitCodef(1, "--node is required")
			}

			nodeID, err := parse.NodeID(nodeStr)
			if err != nil {
				return exitCode(1, err)
			}

			mode = strings.ToLower(mode)
			var startTime, endTime time.Time
			if mode != "attime" {
				if startStr == "" || endStr == "" {
					return exitCodef(1, "--start and --end are required")
				}
				startTime, err = time.Parse(time.RFC3339, startStr)
				if err != nil {
					return exitCodef(1, "invalid --start time: %v", err)
				}
				endTime, err = time.Parse(time.RFC3339, endStr)
				if err != nil {
					return exitCodef(1, "invalid --end time: %v", err)
				}
				if !endTime.After(startTime) {
					return exitCodef(1, "--end must be after --start")
				}
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

			switch mode {
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
				results, err = historyResponseToValues(nodeID.String(), resp)
				if err != nil {
					format.Errf("reading history for %s: %v", nodeID, err)
					return exitCode(3, err)
				}

			case "processed":
				if aggInterval == 0 {
					return exitCodef(1, "--interval is required for processed mode")
				}
				aggID, err := aggregateNodeID(aggregate)
				if err != nil {
					return exitCode(1, err)
				}
				nodes := []*ua.HistoryReadValueID{{NodeID: nodeID}}
				details := &ua.ReadProcessedDetails{
					StartTime:          startTime,
					EndTime:            endTime,
					ProcessingInterval: float64(aggInterval / time.Millisecond),
					AggregateType:      []*ua.NodeID{aggID},
				}
				resp, err := c.HistoryReadProcessed(opCtx, nodes, details)
				if err != nil {
					format.Errf("reading processed history for %s: %v", nodeID, err)
					return exitCode(3, err)
				}
				results, err = historyResponseToValues(nodeID.String(), resp)
				if err != nil {
					format.Errf("reading history for %s: %v", nodeID, err)
					return exitCode(3, err)
				}

			case "attime":
				if len(atTimes) == 0 {
					return exitCodef(1, "--at is required for attime mode (repeatable RFC3339 timestamps)")
				}
				reqTimes := make([]time.Time, 0, len(atTimes))
				for _, s := range atTimes {
					t, err := time.Parse(time.RFC3339, s)
					if err != nil {
						return exitCodef(1, "invalid --at time %q: %v", s, err)
					}
					reqTimes = append(reqTimes, t)
				}
				nodes := []*ua.HistoryReadValueID{{NodeID: nodeID}}
				details := &ua.ReadAtTimeDetails{ReqTimes: reqTimes}
				resp, err := c.HistoryReadAtTime(opCtx, nodes, details)
				if err != nil {
					format.Errf("reading at-time history for %s: %v", nodeID, err)
					return exitCode(3, err)
				}
				results, err = historyResponseToValues(nodeID.String(), resp)
				if err != nil {
					format.Errf("reading history for %s: %v", nodeID, err)
					return exitCode(3, err)
				}

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
	cmd.Flags().StringVar(&startStr, "start", "", "Start time in RFC3339 format (required except attime)")
	cmd.Flags().StringVar(&endStr, "end", "", "End time in RFC3339 format (required except attime)")
	cmd.Flags().StringArrayVar(&atTimes, "at", nil, "Timestamp for attime mode (repeatable, RFC3339)")
	cmd.Flags().Uint32Var(&maxValues, "max-values", 0, "Maximum values to return (0 = all, uses pagination)")
	cmd.Flags().StringVar(&mode, "mode", "raw", "History read mode (raw|modified|processed|attime)")
	cmd.Flags().StringVar(&aggregate, "aggregate", "average", "Aggregate for processed mode (average|min|max|count)")
	cmd.Flags().DurationVar(&aggInterval, "interval", 0, "Aggregate interval for processed mode (ProcessingInterval)")
	cmd.Flags().StringVar(&continuation, "continuation", "all", "Paging control (all = auto-follow, single = one page only)")
	_ = cmd.RegisterFlagCompletionFunc("mode", fixedCompletion(historyModeValues))
	_ = cmd.RegisterFlagCompletionFunc("aggregate", fixedCompletion(aggregateValues))
	_ = cmd.RegisterFlagCompletionFunc("continuation", fixedCompletion(continuationValues))
	return cmd
}

func historyDataValues(nodeID string, dvs []*ua.DataValue) []model.HistoryValue {
	results := make([]model.HistoryValue, 0, len(dvs))
	for _, dv := range dvs {
		results = append(results, toHistoryValue(nodeID, dv))
	}
	return results
}

func historyResponseToValues(nodeID string, resp *ua.HistoryReadResponse) ([]model.HistoryValue, error) {
	dvs, err := historyDataValuesFromResp(resp)
	if err != nil {
		return nil, err
	}
	return historyDataValues(nodeID, dvs), nil
}

// historyDataValuesFromResp returns the DataValues of a HistoryRead
// response, or the status of a result that is Bad (an unknown node, a node
// without history, a mode the server does not support). A Good or
// Uncertain result without data (GoodNoData) is no error.
func historyDataValuesFromResp(resp *ua.HistoryReadResponse) ([]*ua.DataValue, error) {
	var out []*ua.DataValue
	if resp == nil {
		return out, nil
	}
	for _, r := range resp.Results {
		if r == nil {
			continue
		}
		if isBadStatus(r.StatusCode) {
			return nil, r.StatusCode
		}
		if r.HistoryData == nil || r.HistoryData.Value == nil {
			continue
		}
		switch hd := r.HistoryData.Value.(type) {
		case *ua.HistoryData:
			out = append(out, hd.DataValues...)
		case ua.HistoryData:
			out = append(out, hd.DataValues...)
		case *ua.HistoryModifiedData:
			out = append(out, hd.DataValues...)
		case ua.HistoryModifiedData:
			out = append(out, hd.DataValues...)
		}
	}
	return out, nil
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
