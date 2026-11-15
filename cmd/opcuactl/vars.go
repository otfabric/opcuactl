// SPDX-License-Identifier: MIT

package opcuactl

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/otfabric/go-opcua"
	"github.com/otfabric/go-opcua/id"
	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/opcuactl/internal/format"
	"github.com/otfabric/opcuactl/internal/model"
	"github.com/otfabric/opcuactl/internal/parse"
	"github.com/spf13/cobra"
)

func newGetVarsCmd() *cobra.Command {
	var (
		nodeStr   string
		recursive bool
		depth     int
		noRead    bool
		paths     bool
		match     string
		// int64: pflag's int flag keeps the low 32 bits of a value on 32-bit
		// builds, so 4294967297 would pass the range check as 1.
		onlyNS    int64
		excludeNS int64
		wf        walkFlags
	)

	cmd := &cobra.Command{
		Use:   "vars",
		Short: "Collect variables under a node",
		Long: `Collect all Variable nodes under a start node, optionally recursively,
and read their values, data types, and status codes.

By default, returns direct child variables with values. Use --recursive to
traverse the subtree. Use --no-read for metadata only.

Each node is visited once, also where references loop. The walk stops after
--max-nodes nodes, with the results so far and exit 7, and after
--walk-timeout with exit 5.

Examples:
  opcuactl get vars --endpoint opc.tcp://localhost:4840 -n "ns=3;s=PLC"
  opcuactl get vars -e opc.tcp://localhost:4840 -n "ns=2;i=5001" --recursive --paths
  opcuactl get vars -e opc.tcp://localhost:4840 -n "ns=3;s=PLC" --no-read
  opcuactl get vars -e opc.tcp://localhost:4840 -n "ns=3;s=PLC" --match Revision
  opcuactl get vars -e opc.tcp://localhost:4840 -n "ns=2;i=5001" --recursive --only-ns 3 -f json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := checkNamespaceFlag("--only-ns", onlyNS); err != nil {
				return err
			}
			if err := checkNamespaceFlag("--exclude-ns", excludeNS); err != nil {
				return err
			}
			if err := wf.validate(); err != nil {
				return err
			}

			ctx, cancel := context.WithCancel(rootContext())
			defer cancel()

			c, err := connCfg.NewClient(ctx)
			if err != nil {
				format.Errf("%v", err)
				return exitCode(exitTransport, err)
			}
			defer func() { _ = c.Close(ctx) }()

			opCtx, opCancel := connCfg.TimeoutContext(ctx)
			defer opCancel()

			startNode := ua.NewNumericNodeID(0, id.ObjectsFolder)
			if nodeStr != "" {
				startNode, err = parse.NodeID(nodeStr)
				if err != nil {
					return exitCode(exitUsage, err)
				}
			}

			// Determine browse depth.
			maxDepth := 0 // direct children only (depth 0 in BrowseWithDepth = immediate children)
			if recursive {
				if depth > 0 {
					maxDepth = depth - 1 // user depth 1 = direct children = BrowseWithDepth maxDepth 0
				} else {
					maxDepth = -1 // unlimited
				}
			}

			// Browse for all nodes, each once, then filter to variables.
			var refs []opcua.BrowseWithDepthResult
			var partial error
			walkCtx, walkCancel := wf.context(opCtx)
			defer walkCancel()
			for wr, err := range wf.walk(walkCtx, c.Node(startNode), maxDepth) {
				if partial = partialWalk("browsing variables from "+startNode.String(), err); partial != nil {
					break
				}
				if err != nil {
					format.Errf("browsing variables from %s: %v", startNode, err)
					return exitCode(exitProtocol, err)
				}
				refs = append(refs, opcua.BrowseWithDepthResult{Ref: wr.Ref, Depth: wr.Depth})
			}

			// Filter to Variable nodes and apply namespace / match filters.
			type varRef struct {
				ref   *ua.ReferenceDescription
				depth int
			}
			var variables []varRef
			for _, r := range refs {
				if r.Ref.NodeClass != ua.NodeClassVariable {
					continue
				}
				nid := r.Ref.NodeID.NodeID
				if onlyNS >= 0 && nid.Namespace() != uint16(onlyNS) {
					continue
				}
				if excludeNS >= 0 && nid.Namespace() == uint16(excludeNS) {
					continue
				}
				if match != "" {
					name := ""
					if r.Ref.DisplayName != nil {
						name = r.Ref.DisplayName.Text
					}
					browse := ""
					if r.Ref.BrowseName != nil {
						browse = r.Ref.BrowseName.Name
					}
					matchLower := strings.ToLower(match)
					if !strings.Contains(strings.ToLower(name), matchLower) &&
						!strings.Contains(strings.ToLower(browse), matchLower) {
						continue
					}
				}
				variables = append(variables, varRef{ref: r.Ref, depth: r.Depth})
			}

			// Build path map if requested.
			var pathMap map[string]string
			if paths {
				pathMap = buildPathMap(refs)
			}

			// Build results: metadata first.
			results := make([]model.VarNode, len(variables))
			for i, v := range variables {
				nid := v.ref.NodeID.NodeID
				vn := model.VarNode{
					NodeID: nid.String(),
				}
				if v.ref.BrowseName != nil {
					vn.BrowseName = formatBrowseName(v.ref.BrowseName)
				}
				if v.ref.DisplayName != nil {
					vn.DisplayName = v.ref.DisplayName.Text
				}
				if v.ref.TypeDefinition != nil && v.ref.TypeDefinition.NodeID != nil {
					vn.TypeDefinition = ua.TypeDefinitionDisplayName(v.ref.TypeDefinition.NodeID)
				}
				if paths && pathMap != nil {
					vn.Path = pathMap[nid.String()]
				}
				results[i] = vn
			}

			// Batch-read values and data types unless --no-read.
			if !noRead && len(variables) > 0 {
				items := make([]opcua.ReadItem, 0, len(variables)*2)
				for _, v := range variables {
					nid := v.ref.NodeID.NodeID
					items = append(items,
						opcua.ReadItem{NodeID: nid, AttributeID: ua.AttributeIDValue},
						opcua.ReadItem{NodeID: nid, AttributeID: ua.AttributeIDDataType},
					)
				}

				readResults, err := c.ReadMulti(opCtx, items)
				if err != nil {
					format.Errf("batch reading variables: %v", err)
					return exitCode(exitProtocol, err)
				}

				for i := range results {
					valRes := readResults[i*2]
					dtRes := readResults[i*2+1]

					if valRes.DataValue != nil && valRes.DataValue.Value != nil {
						results[i].Value = valRes.DataValue.Value.Value()
					}
					results[i].Status = valRes.StatusCode.Symbol()

					if dtRes.DataValue != nil && dtRes.DataValue.Value != nil {
						if dtID, ok := dtRes.DataValue.Value.Value().(*ua.NodeID); ok {
							results[i].DataType = ua.DataTypeDisplayName(dtID)
						}
					}
				}
			}

			// Output.
			if err := writeVars(results, noRead, paths); err != nil {
				return err
			}
			return partial
		},
	}

	cmd.Flags().StringVarP(&nodeStr, "node", "n", "", "Start node ID (default: Objects folder ns=0;i=85)")
	cmd.Flags().BoolVar(&recursive, "recursive", false, "Traverse subtree recursively")
	cmd.Flags().IntVar(&depth, "depth", 0, "Max recursion depth (0 = unlimited when --recursive)")
	cmd.Flags().BoolVar(&noRead, "no-read", false, "Do not read Value attribute (metadata only)")
	cmd.Flags().BoolVar(&paths, "paths", false, "Include browse path from start node")
	cmd.Flags().StringVar(&match, "match", "", "Filter by browse name / display name substring")
	cmd.Flags().Int64Var(&onlyNS, "only-ns", -1, "Only return nodes in this namespace index (0-65535; -1 = all)")
	cmd.Flags().Int64Var(&excludeNS, "exclude-ns", -1, "Exclude nodes in this namespace index (0-65535; -1 = none)")
	wf.register(cmd.Flags())
	return cmd
}

func writeVars(results []model.VarNode, noRead, paths bool) error {
	w := newWriter()
	f := format.ParseFormat(outputFormat)
	if quiet {
		for _, v := range results {
			fmt.Println(v.NodeID)
		}
		return nil
	}
	if f == format.Table {
		return writeVarsTable(w, results, noRead, paths)
	}
	if len(results) == 1 {
		return w.Write(results[0])
	}
	return w.Write(results)
}

// checkNamespaceFlag refuses a namespace index flag above 65535, the largest
// NamespaceIndex (a UInt16, Part 3 §8.2.2). A negative value means no filter.
func checkNamespaceFlag(name string, v int64) error {
	if v > math.MaxUint16 {
		return exitCodef(exitUsage, "%s %d is not a namespace index (0-65535, or -1 for no filter)", name, v)
	}
	return nil
}

func writeVarsTable(w *format.Writer, results []model.VarNode, noRead, showPaths bool) error {
	var headers []string
	if showPaths {
		headers = append(headers, "PATH")
	}
	if noRead {
		headers = append(headers, "NODE ID", "BROWSE NAME", "DISPLAY NAME", "TYPE DEFINITION")
	} else {
		headers = append(headers, "NODE ID", "BROWSE NAME", "DATA TYPE", "VALUE", "STATUS")
	}

	rows := make([][]string, len(results))
	for i, v := range results {
		var row []string
		if showPaths {
			row = append(row, v.Path)
		}
		if noRead {
			row = append(row, v.NodeID, v.BrowseName, v.DisplayName, v.TypeDefinition)
		} else {
			row = append(row, v.NodeID, v.BrowseName, v.DataType, format.FormatValue(v.Value), v.Status)
		}
		rows[i] = row
	}
	return w.WriteTable(headers, rows)
}

// buildPathMap builds a map from NodeID string to browse path for all refs.
func buildPathMap(refs []opcua.BrowseWithDepthResult) map[string]string {
	type entry struct {
		nid    string
		parent string
		name   string
		depth  int
	}

	// First pass: build entries with parent tracking.
	// BrowseWithDepth returns refs in DFS order. Track the parent at each depth level.
	parentAtDepth := make(map[int]string)
	entries := make([]entry, len(refs))
	for i, r := range refs {
		nid := ""
		if r.Ref.NodeID != nil && r.Ref.NodeID.NodeID != nil {
			nid = r.Ref.NodeID.NodeID.String()
		}
		name := ""
		if r.Ref.DisplayName != nil {
			name = r.Ref.DisplayName.Text
		} else if r.Ref.BrowseName != nil {
			name = r.Ref.BrowseName.Name
		}
		parent := ""
		if r.Depth > 0 {
			parent = parentAtDepth[r.Depth-1]
		}
		entries[i] = entry{nid: nid, parent: parent, name: name, depth: r.Depth}
		parentAtDepth[r.Depth] = nid
	}

	// Build path map by resolving parent chains.
	pathOf := make(map[string]string)
	for _, e := range entries {
		if e.depth == 0 {
			pathOf[e.nid] = "/" + e.name
		} else if parentPath, ok := pathOf[e.parent]; ok {
			pathOf[e.nid] = parentPath + "/" + e.name
		} else {
			pathOf[e.nid] = "/" + e.name
		}
	}
	return pathOf
}
