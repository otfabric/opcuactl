// SPDX-License-Identifier: MIT

package opcuactl

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/otfabric/go-opcua"
	"github.com/otfabric/go-opcua/id"
	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/opcuactl/internal/format"
	"github.com/otfabric/opcuactl/internal/model"
	"github.com/otfabric/opcuactl/internal/parse"
	"github.com/spf13/cobra"
)

func newExportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export data from the server",
		Long: `Export address-space metadata from the server.

Examples:
  opcuactl export nodes -e opc.tcp://localhost:4840 --depth 3
  opcuactl export nodes -e opc.tcp://localhost:4840 -n i=85 --format json`,
	}
	cmd.AddCommand(newExportNodesCmd(), newExportValuesCmd())
	return cmd
}

func newExportNodesCmd() *cobra.Command {
	var (
		nodeStr      string
		maxDepth     int
		nodeClass    string
		output       string
		includeAttrs bool
	)

	cmd := &cobra.Command{
		Use:   "nodes",
		Short: "Export node metadata from the address space",
		Long: `Recursively walk the address space and export enriched node metadata.

Each node is enriched with its summary attributes (data type, access level, etc.).
Output is a flat list of ExportedNode records.

Use --output to write directly to a file instead of stdout.
Use --include-attrs to include additional attributes (WriteMask, ValueRank, etc.).
Supports --format csv and --format jsonl in addition to table/json/yaml.

Examples:
  opcuactl export nodes -e opc.tcp://localhost:4840
  opcuactl export nodes -e opc.tcp://localhost:4840 -n i=85 --depth 3
  opcuactl export nodes -e opc.tcp://localhost:4840 --node-class variable --format json
  opcuactl export nodes -e opc.tcp://localhost:4840 --format csv --output nodes.csv
  opcuactl export nodes -e opc.tcp://localhost:4840 --format yaml`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			c, err := connCfg.NewClient(ctx)
			if err != nil {
				format.Errf("%v", err)
				return exitCode(2, err)
			}
			defer func() { _ = c.Close(ctx) }()

			nodeID := ua.NewNumericNodeID(0, id.ObjectsFolder)
			if nodeStr != "" {
				nodeID, err = parse.NodeID(nodeStr)
				if err != nil {
					return exitCode(1, err)
				}
			}

			classMask := parse.NodeClassMask(nodeClass)
			n := c.Node(nodeID)

			nodes, err := collectExportNodes(ctx, c, n, maxDepth, classMask, 0)
			if err != nil {
				format.Errf("exporting nodes: %v", err)
				return exitCode(3, err)
			}

			if includeAttrs {
				for i := range nodes {
					enrichExportedNode(ctx, c, &nodes[i])
				}
			}

			if verbose {
				format.Errf("exported %d node(s)", len(nodes))
			}

			w := newWriter()
			if output != "" {
				f, err := os.Create(output)
				if err != nil {
					return exitCodef(1, "creating output file: %v", err)
				}
				defer func() { _ = f.Close() }()
				w.SetOutput(f)
			}
			f := format.ParseFormat(outputFormat)
			if f == format.Table || f == format.CSV || f == format.JSONL {
				headers := []string{"NODE ID", "NODE CLASS", "DISPLAY NAME", "DATA TYPE", "ACCESS", "DEPTH"}
				rows := make([][]string, len(nodes))
				for i, n := range nodes {
					rows[i] = []string{
						n.NodeID,
						n.NodeClass,
						n.DisplayName,
						n.DataType,
						n.AccessLevel,
						fmt.Sprintf("%d", n.Depth),
					}
				}
				return w.WriteTable(headers, rows)
			}
			return w.Write(nodes)
		},
	}

	cmd.Flags().StringVarP(&nodeStr, "node", "n", "", "Start node ID (default: Objects folder ns=0;i=85)")
	cmd.Flags().IntVar(&maxDepth, "depth", 5, "Maximum tree depth")
	cmd.Flags().StringVar(&nodeClass, "node-class", "all", "Filter by node class")
	cmd.Flags().StringVar(&output, "output", "", "Write output to file instead of stdout")
	cmd.Flags().BoolVar(&includeAttrs, "include-attrs", false, "Include additional attributes (WriteMask, ValueRank, etc.)")
	_ = cmd.RegisterFlagCompletionFunc("node-class", fixedCompletion(nodeClassValues))
	return cmd
}

func collectExportNodes(ctx context.Context, c *opcua.Client, n *opcua.Node, maxDepth int, classMask ua.NodeClass, _ int) ([]model.ExportedNode, error) {
	var results []model.ExportedNode

	// Include the root node at depth 0
	opCtx, cancel := connCfg.TimeoutContext(ctx)
	rootEN := nodeToExportedNode(opCtx, n, 0)
	cancel()
	if classMask == ua.NodeClassAll || ua.NodeClassFromString(rootEN.NodeClass)&classMask != 0 {
		results = append(results, rootEN)
	}

	// Walk children with dedup; adjust limit since root is already depth 0
	walkDepth := maxDepth - 1
	if maxDepth <= 0 {
		walkDepth = -1 // unlimited
	}

	for wr, err := range n.WalkLimitDedup(ctx, walkDepth) {
		if err != nil {
			return results, err
		}

		ref := wr.Ref
		nid := ""
		if ref.NodeID != nil && ref.NodeID.NodeID != nil {
			nid = ref.NodeID.NodeID.String()
		}
		if nid == "" {
			continue
		}

		opCtx, cancel := connCfg.TimeoutContext(ctx)
		en := nodeToExportedNode(opCtx, c.Node(ref.NodeID.NodeID), wr.Depth+1)
		cancel()

		if classMask != ua.NodeClassAll && ua.NodeClassFromString(en.NodeClass)&classMask == 0 {
			continue
		}
		results = append(results, en)
	}
	return results, nil
}

func nodeToExportedNode(ctx context.Context, n *opcua.Node, depth int) model.ExportedNode {
	en := model.ExportedNode{
		NodeID: n.ID.String(),
		Depth:  depth,
	}

	summary, err := n.Summary(ctx)
	if err != nil {
		return en
	}

	en.NodeClass = summary.NodeClass.String()
	if summary.BrowseName != nil {
		en.BrowseName = formatBrowseName(summary.BrowseName)
	}
	if summary.DisplayName != nil {
		en.DisplayName = summary.DisplayName.Text
	}
	if summary.Description != nil {
		en.Description = summary.Description.Text
	}
	if summary.DataType != nil {
		en.DataType = ua.DataTypeDisplayName(summary.DataType)
	}
	en.AccessLevel = summary.AccessLevel.String()
	en.UserAccessLevel = summary.UserAccessLevel.String()
	if summary.TypeDefinition != nil {
		en.TypeDefinition = ua.TypeDefinitionDisplayName(summary.TypeDefinition)
	}
	return en
}

func newExportValuesCmd() *cobra.Command {
	var (
		nodes       []string
		nodesFile   string
		startNode   string
		maxDepth    int
		history     bool
		startStr    string
		endStr      string
		atTimes     []string
		mode        string
		aggregate   string
		aggInterval time.Duration
		output      string
	)

	cmd := &cobra.Command{
		Use:   "values",
		Short: "Export current or historical values",
		Long: `Export values for specified nodes or a discovered subtree.

In snapshot mode (default), reads current values for specified nodes or all
variable nodes in a subtree. In history mode (--history), exports historical
values within a time range. Use --mode raw|modified|processed|attime (default raw).

Use --nodes-file to read node IDs from a text file (one per line).
Use --output to write directly to a file instead of stdout.
Supports --format csv and --format jsonl in addition to table/json/yaml.

Examples:
  opcuactl export values -e opc.tcp://localhost:4840 -n i=2258 -n i=2259
  opcuactl export values -e opc.tcp://localhost:4840 --start-node i=85 --depth 3
  opcuactl export values -e opc.tcp://localhost:4840 --nodes-file nodes.txt --format csv --output values.csv
  opcuactl export values -e opc.tcp://localhost:4840 -n i=2258 --history --start 2026-03-01T00:00:00Z --end 2026-03-11T00:00:00Z
  opcuactl export values -e opc.tcp://localhost:4840 -n i=2258 --history --mode processed --interval 1h --aggregate average --start 2026-03-01T00:00:00Z --end 2026-03-02T00:00:00Z
  opcuactl export values -e opc.tcp://localhost:4840 -n i=2258 --format json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Load nodes from file if specified
			if nodesFile != "" {
				fileNodes, err := readNodesFile(nodesFile)
				if err != nil {
					return exitCodef(1, "reading --nodes-file: %v", err)
				}
				nodes = append(nodes, fileNodes...)
			}

			if len(nodes) == 0 && startNode == "" {
				return exitCodef(1, "at least one --node, --nodes-file, or --start-node is required")
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			c, err := connCfg.NewClient(ctx)
			if err != nil {
				format.Errf("%v", err)
				return exitCode(2, err)
			}
			defer func() { _ = c.Close(ctx) }()

			// Collect node IDs
			var nodeIDs []*ua.NodeID
			if len(nodes) > 0 {
				nodeIDs, err = parse.NodeIDs(nodes)
				if err != nil {
					return exitCode(1, err)
				}
			} else {
				// Discover variable nodes from subtree
				startID, err := parse.NodeID(startNode)
				if err != nil {
					return exitCode(1, err)
				}
				n := c.Node(startID)
				exported, err := collectExportNodes(ctx, c, n, maxDepth, ua.NodeClassVariable, 0)
				if err != nil {
					format.Errf("discovering variables: %v", err)
					return exitCode(3, err)
				}
				for _, en := range exported {
					nid, err := parse.NodeID(en.NodeID)
					if err != nil {
						continue
					}
					nodeIDs = append(nodeIDs, nid)
				}
			}

			if len(nodeIDs) == 0 {
				format.Errf("no variable nodes found")
				return exitCodef(1, "no nodes to export")
			}

			if history {
				return exportHistoryValues(ctx, c, nodeIDs, exportHistoryOpts{
					startStr: startStr, endStr: endStr, atTimes: atTimes,
					mode: mode, aggregate: aggregate, aggInterval: aggInterval,
					output: output,
				})
			}
			return exportSnapshotValues(ctx, c, nodeIDs, output)
		},
	}

	cmd.Flags().StringArrayVarP(&nodes, "node", "n", nil, "Node ID to export (repeatable)")
	cmd.Flags().StringVar(&nodesFile, "nodes-file", "", "Read node IDs from a text file (one per line)")
	cmd.Flags().StringVar(&startNode, "start-node", "", "Start node for subtree discovery (exports all variable children)")
	cmd.Flags().IntVar(&maxDepth, "depth", 5, "Maximum depth for subtree discovery")
	cmd.Flags().BoolVar(&history, "history", false, "Export historical values instead of current snapshot")
	cmd.Flags().StringVar(&startStr, "start", "", "Start time in RFC3339 format (required with --history except attime)")
	cmd.Flags().StringVar(&endStr, "end", "", "End time in RFC3339 format (required with --history except attime)")
	cmd.Flags().StringArrayVar(&atTimes, "at", nil, "Timestamp for attime mode (repeatable, RFC3339)")
	cmd.Flags().StringVar(&mode, "mode", "raw", "History mode with --history (raw|modified|processed|attime)")
	cmd.Flags().StringVar(&aggregate, "aggregate", "average", "Aggregate for processed history (average|min|max|count)")
	cmd.Flags().DurationVar(&aggInterval, "interval", 0, "Aggregate interval for processed history")
	cmd.Flags().StringVar(&output, "output", "", "Write output to file instead of stdout")
	_ = cmd.RegisterFlagCompletionFunc("mode", fixedCompletion(historyModeValues))
	_ = cmd.RegisterFlagCompletionFunc("aggregate", fixedCompletion(aggregateValues))
	return cmd
}

type exportHistoryOpts struct {
	startStr, endStr string
	atTimes          []string
	mode, aggregate  string
	aggInterval      time.Duration
	output           string
}

func exportSnapshotValues(ctx context.Context, c *opcua.Client, nodeIDs []*ua.NodeID, output string) error {
	opCtx, opCancel := connCfg.TimeoutContext(ctx)
	defer opCancel()

	dvs, err := c.ReadValues(opCtx, nodeIDs...)
	if err != nil {
		format.Errf("reading values: %v", err)
		return exitCode(3, err)
	}

	var results []model.ExportedValue
	for i, dv := range dvs {
		ev := model.ExportedValue{
			NodeID:          nodeIDs[i].String(),
			Status:          dv.Status.Symbol(),
			StatusCode:      dv.Status.Uint32(),
			SourceTimestamp: model.FormatTimestamp(dv.SourceTimestamp),
			ServerTimestamp: model.FormatTimestamp(dv.ServerTimestamp),
		}
		if dv.Value != nil {
			ev.Value = dv.Value.Value()
			ev.Type = dv.Value.Type().String()
		}
		results = append(results, ev)
	}

	if verbose {
		format.Errf("exported %d value(s)", len(results))
	}

	w := newWriter()
	if output != "" {
		f, err := os.Create(output)
		if err != nil {
			return exitCodef(1, "creating output file: %v", err)
		}
		defer func() { _ = f.Close() }()
		w.SetOutput(f)
	}
	f := format.ParseFormat(outputFormat)
	if f == format.Table || f == format.CSV || f == format.JSONL {
		headers := []string{"NODE ID", "VALUE", "TYPE", "STATUS", "SOURCE TIMESTAMP"}
		rows := make([][]string, len(results))
		for i, r := range results {
			rows[i] = []string{r.NodeID, format.FormatValue(r.Value), r.Type, r.Status, r.SourceTimestamp}
		}
		return w.WriteTable(headers, rows)
	}
	return w.Write(results)
}

func exportHistoryValues(ctx context.Context, c *opcua.Client, nodeIDs []*ua.NodeID, opts exportHistoryOpts) error {
	mode := strings.ToLower(opts.mode)
	if mode == "" {
		mode = "raw"
	}

	var startTime, endTime time.Time
	var reqTimes []time.Time
	var err error
	if mode != "attime" {
		if opts.startStr == "" || opts.endStr == "" {
			return exitCodef(1, "--start and --end are required with --history")
		}
		startTime, err = time.Parse(time.RFC3339, opts.startStr)
		if err != nil {
			return exitCodef(1, "invalid --start time: %v", err)
		}
		endTime, err = time.Parse(time.RFC3339, opts.endStr)
		if err != nil {
			return exitCodef(1, "invalid --end time: %v", err)
		}
		if !endTime.After(startTime) {
			return exitCodef(1, "--end must be after --start")
		}
	} else {
		if len(opts.atTimes) == 0 {
			return exitCodef(1, "--at is required for attime mode")
		}
		for _, s := range opts.atTimes {
			t, err := time.Parse(time.RFC3339, s)
			if err != nil {
				return exitCodef(1, "invalid --at time %q: %v", s, err)
			}
			reqTimes = append(reqTimes, t)
		}
	}

	opCtx, opCancel := connCfg.TimeoutContext(ctx)
	defer opCancel()

	var results []model.ExportedValue
	for _, nid := range nodeIDs {
		var dvs []*ua.DataValue
		switch mode {
		case "modified":
			resp, err := c.HistoryReadRawModified(opCtx, []*ua.HistoryReadValueID{{NodeID: nid}}, &ua.ReadRawModifiedDetails{
				IsReadModified: true, StartTime: startTime, EndTime: endTime,
			})
			if err != nil {
				format.Errf("reading modified history for %s: %v", nid, err)
				continue
			}
			dvs = historyDataValuesFromResp(resp)
		case "processed":
			if opts.aggInterval == 0 {
				return exitCodef(1, "--interval is required for processed mode")
			}
			aggID, err := aggregateNodeID(opts.aggregate)
			if err != nil {
				return exitCode(1, err)
			}
			resp, err := c.HistoryReadProcessed(opCtx, []*ua.HistoryReadValueID{{NodeID: nid}}, &ua.ReadProcessedDetails{
				StartTime: startTime, EndTime: endTime,
				ProcessingInterval: float64(opts.aggInterval / time.Millisecond),
				AggregateType:      []*ua.NodeID{aggID},
			})
			if err != nil {
				format.Errf("reading processed history for %s: %v", nid, err)
				continue
			}
			dvs = historyDataValuesFromResp(resp)
		case "attime":
			resp, err := c.HistoryReadAtTime(opCtx, []*ua.HistoryReadValueID{{NodeID: nid}}, &ua.ReadAtTimeDetails{
				ReqTimes: reqTimes,
			})
			if err != nil {
				format.Errf("reading at-time history for %s: %v", nid, err)
				continue
			}
			dvs = historyDataValuesFromResp(resp)
		default:
			for dv, err := range c.ReadHistoryAll(ctx, nid, startTime, endTime) {
				if err != nil {
					format.Errf("reading history for %s: %v", nid, err)
					break
				}
				dvs = append(dvs, dv)
			}
		}
		for _, dv := range dvs {
			if dv == nil {
				continue
			}
			ev := model.ExportedValue{
				NodeID:          nid.String(),
				Status:          dv.Status.Symbol(),
				StatusCode:      dv.Status.Uint32(),
				SourceTimestamp: model.FormatTimestamp(dv.SourceTimestamp),
				ServerTimestamp: model.FormatTimestamp(dv.ServerTimestamp),
			}
			if dv.Value != nil {
				ev.Value = dv.Value.Value()
				ev.Type = dv.Value.Type().String()
			}
			results = append(results, ev)
		}
	}

	output := opts.output

	if verbose {
		format.Errf("exported %d historical value(s) across %d node(s)", len(results), len(nodeIDs))
	}

	w := newWriter()
	if output != "" {
		f, err := os.Create(output)
		if err != nil {
			return exitCodef(1, "creating output file: %v", err)
		}
		defer func() { _ = f.Close() }()
		w.SetOutput(f)
	}
	f := format.ParseFormat(outputFormat)
	if f == format.Table || f == format.CSV || f == format.JSONL {
		headers := []string{"NODE ID", "VALUE", "TYPE", "STATUS", "SOURCE TIMESTAMP"}
		rows := make([][]string, len(results))
		for i, r := range results {
			rows[i] = []string{r.NodeID, format.FormatValue(r.Value), r.Type, r.Status, r.SourceTimestamp}
		}
		return w.WriteTable(headers, rows)
	}
	return w.Write(results)
}

func readNodesFile(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var nodes []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		nodes = append(nodes, line)
	}
	return nodes, scanner.Err()
}

func enrichExportedNode(ctx context.Context, c *opcua.Client, en *model.ExportedNode) {
	nodeID, err := parse.NodeID(en.NodeID)
	if err != nil {
		return
	}
	opCtx, cancel := connCfg.TimeoutContext(ctx)
	defer cancel()

	n := c.Node(nodeID)
	dvs, err := n.Attributes(opCtx,
		ua.AttributeIDWriteMask,
		ua.AttributeIDValueRank,
		ua.AttributeIDArrayDimensions,
	)
	if err != nil || len(dvs) < 3 {
		return
	}
	if dvs[0] != nil && dvs[0].Value != nil {
		en.WriteMask = fmt.Sprintf("%v", dvs[0].Value.Value())
	}
	if dvs[1] != nil && dvs[1].Value != nil {
		en.ValueRank = fmt.Sprintf("%v", dvs[1].Value.Value())
	}
	if dvs[2] != nil && dvs[2].Value != nil {
		en.ArrayDimensions = fmt.Sprintf("%v", dvs[2].Value.Value())
	}
}
