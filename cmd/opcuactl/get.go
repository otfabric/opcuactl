// SPDX-License-Identifier: MIT

package opcuactl

import (
	"context"
	"fmt"
	"strings"

	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/opcuactl/internal/format"
	"github.com/otfabric/opcuactl/internal/model"
	"github.com/otfabric/opcuactl/internal/parse"
	"github.com/spf13/cobra"
)

func newGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get",
		Short: "Read values, attributes, and server information",
		Long: `Read node values, attributes, node summaries, and server status.

Examples:
  opcuactl get value --endpoint opc.tcp://localhost:4840 --node i=2258
  opcuactl get node --endpoint opc.tcp://localhost:4840 --node i=2258
  opcuactl get server --endpoint opc.tcp://localhost:4840`,
	}
	cmd.AddCommand(newGetValueCmd(), newGetAttrCmd(), newGetNodeCmd(), newGetServerCmd(), newGetVarsCmd())
	return cmd
}

func newGetValueCmd() *cobra.Command {
	var (
		nodes              []string
		timestamps         bool
		timestampsToReturn string
		indexRange         string
		uaEncoding         string
	)

	cmd := &cobra.Command{
		Use:   "value",
		Short: "Read node values",
		Long: `Read one or more node values from the server.

Use --index-range for array/matrix subsets (e.g. "0:2" or "0:1,0:1").
Use --timestamps-to-return to control which timestamps the server returns
(source|server|both|neither). --timestamps only affects CLI display.

--ua-encoding prints each DataValue in a standard OPC UA DataEncoding of
Part 6 instead of the opcuactl output format: json (1.05 compact),
json-verbose, json-reversible and json-nonreversible (1.04), or xml. One
document per node, in the order of the --node flags; --pretty indents. The
output can be exchanged with other OPC UA stacks and fed to
"write value --type ua-json|ua-xml".

Examples:
  opcuactl get value --endpoint opc.tcp://localhost:4840 --node i=2258
  opcuactl get value -e opc.tcp://localhost:4840 -n i=2258 -n "ns=2;s=Temperature" --format json
  opcuactl get value --endpoint opc.tcp://localhost:4840 --node i=2258 --timestamps
  opcuactl get value -e opc.tcp://localhost:4840 -n "ns=2;s=Array" --index-range "0:2"
  opcuactl get value -e opc.tcp://localhost:4840 -n "ns=2;s=Recipe" --ua-encoding json --pretty
  opcuactl get value -e opc.tcp://localhost:4840 -n "ns=2;s=Recipe" --ua-encoding xml`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(nodes) == 0 {
				return exitCodef(1, "at least one --node is required")
			}

			nodeIDs, err := parse.NodeIDs(nodes)
			if err != nil {
				return exitCode(1, err)
			}

			tsReturn := ua.TimestampsToReturnBoth
			if timestampsToReturn != "" {
				tsReturn = ua.TimestampsToReturnFromString(timestampsToReturn)
			}

			var encoding *parse.UAEncoding
			if uaEncoding != "" {
				enc, err := parse.ParseUAEncoding(uaEncoding)
				if err != nil {
					return exitCodef(1, "invalid --ua-encoding: %v", err)
				}
				encoding = &enc
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

			nodesToRead := make([]*ua.ReadValueID, len(nodeIDs))
			for i, nid := range nodeIDs {
				nodesToRead[i] = &ua.ReadValueID{
					NodeID:      nid,
					AttributeID: ua.AttributeIDValue,
					IndexRange:  indexRange,
				}
			}
			resp, err := c.Read(opCtx, &ua.ReadRequest{
				NodesToRead:        nodesToRead,
				TimestampsToReturn: tsReturn,
			})
			if err != nil {
				format.Errf("reading value: %v", err)
				return exitCode(3, err)
			}

			if encoding != nil {
				for _, dv := range resp.Results {
					data, err := encoding.Encode(dv, prettyOutput)
					if err != nil {
						return exitCodef(3, "encoding value: %v", err)
					}
					fmt.Println(string(data))
				}
				return nil
			}

			showTS := timestamps
			if timestampsToReturn != "neither" && (timestampsToReturn == "source" || timestampsToReturn == "server" || timestampsToReturn == "both") {
				showTS = true
			}
			return outputValues(newWriter(), nodeIDs, resp.Results, showTS)
		},
	}

	cmd.Flags().StringArrayVarP(&nodes, "node", "n", nil, "Node ID to read (repeatable)")
	cmd.Flags().BoolVar(&timestamps, "timestamps", false, "Show source and server timestamps in output")
	cmd.Flags().StringVar(&timestampsToReturn, "timestamps-to-return", "both", "Timestamps to request (source|server|both|neither)")
	cmd.Flags().StringVar(&indexRange, "index-range", "", "IndexRange for array/matrix Value reads")
	cmd.Flags().StringVar(&uaEncoding, "ua-encoding", "", "Print DataValues in an OPC UA DataEncoding (json|json-verbose|json-reversible|json-nonreversible|xml)")
	_ = cmd.RegisterFlagCompletionFunc("timestamps-to-return", fixedCompletion(timestampValues))
	_ = cmd.RegisterFlagCompletionFunc("ua-encoding", fixedCompletion(uaEncodingValues))
	return cmd
}

func outputValues(w *format.Writer, nodeIDs []*ua.NodeID, dvs []*ua.DataValue, timestamps bool) error {
	var values []model.NodeValue
	for i, dv := range dvs {
		nv := model.NodeValue{
			NodeID:     nodeIDs[i].String(),
			Status:     dv.Status.Symbol(),
			StatusCode: dv.Status.Uint32(),
		}
		if dv.Value != nil {
			nv.Value = dv.Value.Value()
			nv.Type = dv.Value.Type().String()
		}
		if timestamps {
			nv.SourceTimestamp = model.FormatTimestamp(dv.SourceTimestamp)
			nv.ServerTimestamp = model.FormatTimestamp(dv.ServerTimestamp)
		}
		values = append(values, nv)
	}

	f := format.ParseFormat(outputFormat)
	if quiet {
		for _, v := range values {
			fmt.Println(format.FormatValue(v.Value))
		}
		return nil
	}
	if f == format.Table {
		headers := []string{"NODE ID", "VALUE", "TYPE", "STATUS"}
		if timestamps {
			headers = append(headers, "SOURCE TIMESTAMP", "SERVER TIMESTAMP")
		}
		rows := make([][]string, len(values))
		for i, v := range values {
			row := []string{v.NodeID, format.FormatValue(v.Value), v.Type, v.Status}
			if timestamps {
				row = append(row, v.SourceTimestamp, v.ServerTimestamp)
			}
			rows[i] = row
		}
		return w.WriteTable(headers, rows)
	}
	if len(values) == 1 {
		return w.Write(values[0])
	}
	return w.Write(values)
}

func newGetNodeCmd() *cobra.Command {
	var (
		nodes []string
		full  bool
	)

	cmd := &cobra.Command{
		Use:   "node",
		Short: "Get node summary",
		Long: `Read a rich summary of one or more nodes including common attributes.

Use --full to include additional attributes such as WriteMask, ValueRank,
ArrayDimensions, MinimumSamplingInterval, and Historizing.

Examples:
  opcuactl get node --endpoint opc.tcp://localhost:4840 --node i=2258
  opcuactl get node --endpoint opc.tcp://localhost:4840 --node "ns=0;i=85" --format json
  opcuactl get node --endpoint opc.tcp://localhost:4840 --node i=2258 --full`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(nodes) == 0 {
				return exitCodef(1, "at least one --node is required")
			}

			nodeIDs, err := parse.NodeIDs(nodes)
			if err != nil {
				return exitCode(1, err)
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

			w := newWriter()
			var infos []model.NodeInfo
			for _, nid := range nodeIDs {
				n := c.Node(nid)
				summary, err := n.Summary(opCtx)
				if err != nil {
					format.Errf("getting summary for %s: %v", nid, err)
					return exitCode(3, err)
				}

				info := model.NodeInfo{
					NodeID:    summary.NodeID.String(),
					NodeClass: summary.NodeClass.String(),
				}
				if summary.BrowseName != nil {
					info.BrowseName = formatBrowseName(summary.BrowseName)
				}
				if summary.DisplayName != nil {
					info.DisplayName = summary.DisplayName.Text
				}
				if summary.Description != nil {
					info.Description = summary.Description.Text
				}
				if summary.DataType != nil {
					info.DataType = ua.DataTypeDisplayName(summary.DataType)
				}
				if summary.AccessLevel != 0 {
					info.AccessLevel = summary.AccessLevel.String()
					info.AccessInterpretation = friendlyAccessLevel(summary.AccessLevel)
				}
				if summary.UserAccessLevel != 0 {
					info.UserAccessLevel = summary.UserAccessLevel.String()
				}
				if summary.TypeDefinition != nil {
					info.TypeDefinition = ua.TypeDefinitionDisplayName(summary.TypeDefinition)
				}
				if summary.Value != nil && summary.Value.Value != nil {
					info.Value = summary.Value.Value.Value()
					info.ValueStatus = summary.Value.Status.Symbol()
				}

				if full {
					// Fetch extra attributes
					dvs, err := n.Attributes(opCtx,
						ua.AttributeIDWriteMask,
						ua.AttributeIDValueRank,
						ua.AttributeIDArrayDimensions,
						ua.AttributeIDMinimumSamplingInterval,
						ua.AttributeIDHistorizing,
					)
					if err == nil && len(dvs) >= 5 {
						if dvs[0] != nil && dvs[0].Value != nil && dvs[0].Value.Value() != nil {
							info.WriteMask = fmt.Sprintf("%v", dvs[0].Value.Value())
						}
						if dvs[1] != nil && dvs[1].Value != nil && dvs[1].Value.Value() != nil {
							info.ValueRank = fmt.Sprintf("%v", dvs[1].Value.Value())
						}
						if dvs[2] != nil && dvs[2].Value != nil && dvs[2].Value.Value() != nil {
							info.ArrayDimensions = fmt.Sprintf("%v", dvs[2].Value.Value())
						}
						if dvs[3] != nil && dvs[3].Value != nil && dvs[3].Value.Value() != nil {
							info.MinSamplingInterval = fmt.Sprintf("%v", dvs[3].Value.Value())
						}
						if dvs[4] != nil && dvs[4].Value != nil && dvs[4].Value.Value() != nil {
							info.Historizing = fmt.Sprintf("%v", dvs[4].Value.Value())
						}
					}
				}

				infos = append(infos, info)
			}

			f := format.ParseFormat(outputFormat)
			if quiet {
				for _, info := range infos {
					fmt.Println(info.NodeID)
				}
				return nil
			}
			if f == format.Table {
				headers := []string{"NODE ID", "CLASS", "BROWSE NAME", "DISPLAY NAME", "DATA TYPE", "VALUE"}
				if full {
					headers = append(headers, "ACCESS", "TYPE DEFINITION", "WRITE MASK", "VALUE RANK", "ARRAY DIM", "MIN SAMPLING", "HISTORIZING")
				}
				rows := make([][]string, len(infos))
				for i, info := range infos {
					val := ""
					if info.Value != nil {
						val = format.FormatValue(info.Value)
					}
					row := []string{info.NodeID, info.NodeClass, info.BrowseName, info.DisplayName, info.DataType, val}
					if full {
						row = append(row, info.AccessInterpretation, info.TypeDefinition, info.WriteMask, info.ValueRank, info.ArrayDimensions, info.MinSamplingInterval, info.Historizing)
					}
					rows[i] = row
				}
				return w.WriteTable(headers, rows)
			}
			if len(infos) == 1 {
				return w.Write(infos[0])
			}
			return w.Write(infos)
		},
	}

	cmd.Flags().StringArrayVarP(&nodes, "node", "n", nil, "Node ID (repeatable)")
	cmd.Flags().BoolVar(&full, "full", false, "Fetch additional attributes beyond summary")
	return cmd
}

func newGetServerCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "server",
		Short: "Get server status",
		Long: `Read server status, build info, and connection security details.

Examples:
  opcuactl get server --endpoint opc.tcp://localhost:4840
  opcuactl get server --endpoint opc.tcp://localhost:4840 --format json`,
		RunE: func(cmd *cobra.Command, args []string) error {
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

			ss, err := c.ServerStatus(opCtx)
			if err != nil {
				format.Errf("getting server status: %v", err)
				return exitCode(3, err)
			}

			w := newWriter()
			status := model.ServerStatus{
				StartTime:           model.FormatTimestamp(ss.StartTime),
				CurrentTime:         model.FormatTimestamp(ss.CurrentTime),
				State:               ss.State.String(),
				SecondsTillShutdown: ss.SecondsTillShutdown,
				SecurityPolicy:      parse.SecurityPolicyShortName(c.SecurityPolicy()),
				SecurityMode:        c.SecurityMode().String(),
			}
			if ss.ShutdownReason != nil {
				status.ShutdownReason = ss.ShutdownReason.Text
			}
			if ss.BuildInfo != nil {
				status.BuildInfo = &model.BuildInfo{
					ProductURI:       ss.BuildInfo.ProductURI,
					ManufacturerName: ss.BuildInfo.ManufacturerName,
					ProductName:      ss.BuildInfo.ProductName,
					SoftwareVersion:  ss.BuildInfo.SoftwareVersion,
					BuildNumber:      ss.BuildInfo.BuildNumber,
					BuildDate:        model.FormatTimestamp(ss.BuildInfo.BuildDate),
				}
			}

			f := format.ParseFormat(outputFormat)
			if f == format.Table {
				fmt.Printf("Server Status\n")
				fmt.Printf("  State:          %s\n", status.State)
				fmt.Printf("  Start Time:     %s\n", status.StartTime)
				fmt.Printf("  Current Time:   %s\n", status.CurrentTime)
				fmt.Printf("  Security:       %s / %s\n", status.SecurityPolicy, status.SecurityMode)
				if status.BuildInfo != nil {
					fmt.Printf("  Product:        %s %s\n", status.BuildInfo.ProductName, status.BuildInfo.SoftwareVersion)
					fmt.Printf("  Manufacturer:   %s\n", status.BuildInfo.ManufacturerName)
					fmt.Printf("  Build:          %s (%s)\n", status.BuildInfo.BuildNumber, status.BuildInfo.BuildDate)
				}
				return nil
			}
			return w.Write(status)
		},
	}
}

// AttributeResult represents a single attribute read result.
type AttributeResult struct {
	NodeID      string      `json:"nodeId"`
	Attribute   string      `json:"attribute"`
	AttributeID uint32      `json:"attributeId"`
	Value       interface{} `json:"value"`
	Status      string      `json:"status"`
	StatusCode  uint32      `json:"statusCode"`
}

// friendlyAccessLevel interprets the AccessLevel bitmask into a short
// human-readable summary like "readable, writable".
func friendlyAccessLevel(al ua.AccessLevelType) string {
	if al == 0 {
		return "none"
	}
	var parts []string
	if al&ua.AccessLevelTypeCurrentRead != 0 {
		parts = append(parts, "readable")
	}
	if al&ua.AccessLevelTypeCurrentWrite != 0 {
		parts = append(parts, "writable")
	}
	if al&ua.AccessLevelTypeHistoryRead != 0 {
		parts = append(parts, "history-read")
	}
	if al&ua.AccessLevelTypeHistoryWrite != 0 {
		parts = append(parts, "history-write")
	}
	return strings.Join(parts, ", ")
}

// formatBrowseName formats a QualifiedName as "ns:name".
func formatBrowseName(qn *ua.QualifiedName) string {
	if qn == nil {
		return ""
	}
	return fmt.Sprintf("%d:%s", qn.NamespaceIndex, qn.Name)
}

// formatAttrValue converts raw OPC UA types into human-readable strings.
func formatAttrValue(v interface{}) interface{} {
	switch t := v.(type) {
	case *ua.QualifiedName:
		if t == nil {
			return nil
		}
		return fmt.Sprintf("%d:%s", t.NamespaceIndex, t.Name)
	case *ua.LocalizedText:
		if t == nil {
			return nil
		}
		return t.Text
	case *ua.NodeID:
		if t == nil {
			return nil
		}
		return t.String()
	case *ua.ExpandedNodeID:
		if t == nil || t.NodeID == nil {
			return nil
		}
		return t.NodeID.String()
	default:
		return v
	}
}

// friendlyAttrName strips the "AttributeID" prefix from the attribute string representation.
func friendlyAttrName(id ua.AttributeID) string {
	return strings.TrimPrefix(id.String(), "AttributeID")
}

// allAttributeIDs contains all 22 standard OPC UA attribute IDs (1-22).
var allAttributeIDs = []ua.AttributeID{
	ua.AttributeIDNodeID,
	ua.AttributeIDNodeClass,
	ua.AttributeIDBrowseName,
	ua.AttributeIDDisplayName,
	ua.AttributeIDDescription,
	ua.AttributeIDWriteMask,
	ua.AttributeIDUserWriteMask,
	ua.AttributeIDIsAbstract,
	ua.AttributeIDSymmetric,
	ua.AttributeIDInverseName,
	ua.AttributeIDContainsNoLoops,
	ua.AttributeIDEventNotifier,
	ua.AttributeIDValue,
	ua.AttributeIDDataType,
	ua.AttributeIDValueRank,
	ua.AttributeIDArrayDimensions,
	ua.AttributeIDAccessLevel,
	ua.AttributeIDUserAccessLevel,
	ua.AttributeIDMinimumSamplingInterval,
	ua.AttributeIDHistorizing,
	ua.AttributeIDExecutable,
	ua.AttributeIDUserExecutable,
}

func newGetAttrCmd() *cobra.Command {
	var (
		nodeStr string
		attrs   []string
		all     bool
	)

	cmd := &cobra.Command{
		Use:   "attr",
		Short: "Read node attributes",
		Long: `Read one or more attributes of a node by name or numeric ID.
Use --all to read all 22 standard OPC UA attributes at once.

Common attribute names: NodeID, NodeClass, BrowseName, DisplayName, Description,
DataType, Value, AccessLevel, UserAccessLevel, WriteMask, ValueRank, Historizing.

Examples:
  opcuactl get attr -e opc.tcp://localhost:4840 -n i=2258 --attr Value
  opcuactl get attr -e opc.tcp://localhost:4840 -n i=2258 --attr BrowseName --attr DisplayName --attr DataType
  opcuactl get attr -e opc.tcp://localhost:4840 -n i=2258 --attr 13 --format json
  opcuactl get attr -e opc.tcp://localhost:4840 -n i=2258 --all
  opcuactl get attr -e opc.tcp://localhost:4840 -n i=2258 --all --format json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if nodeStr == "" {
				return exitCodef(1, "--node is required")
			}
			if len(attrs) == 0 && !all {
				return exitCodef(1, "at least one --attr is required (or use --all)")
			}

			nodeID, err := parse.NodeID(nodeStr)
			if err != nil {
				return exitCode(1, err)
			}

			var attrIDs []ua.AttributeID
			if all {
				attrIDs = allAttributeIDs
			} else {
				attrIDs = make([]ua.AttributeID, len(attrs))
				for i, a := range attrs {
					aid, err := parse.AttributeIDFromName(a)
					if err != nil {
						return exitCodef(1, "invalid --attr %q: %v", a, err)
					}
					attrIDs[i] = aid
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

			n := c.Node(nodeID)
			dvs, err := n.Attributes(opCtx, attrIDs...)
			if err != nil {
				format.Errf("reading attributes for %s: %v", nodeID, err)
				return exitCode(3, err)
			}

			var results []AttributeResult
			for i, dv := range dvs {
				// When --all is used without --verbose, skip unsupported attributes.
				if all && !verbose && dv != nil && dv.Status == ua.StatusBadAttributeIDInvalid {
					continue
				}
				r := AttributeResult{
					NodeID:      nodeID.String(),
					Attribute:   friendlyAttrName(attrIDs[i]),
					AttributeID: uint32(attrIDs[i]),
				}
				if dv != nil {
					r.Status = dv.Status.Symbol()
					r.StatusCode = dv.Status.Uint32()
					if dv.Value != nil {
						r.Value = formatAttrValue(dv.Value.Value())
					}
				}
				results = append(results, r)
			}

			w := newWriter()
			f := format.ParseFormat(outputFormat)
			if f == format.Table {
				headers := []string{"NODE ID", "ATTRIBUTE", "VALUE", "STATUS"}
				rows := make([][]string, len(results))
				for i, r := range results {
					rows[i] = []string{r.NodeID, r.Attribute, format.FormatValue(r.Value), r.Status}
				}
				return w.WriteTable(headers, rows)
			}
			if len(results) == 1 {
				return w.Write(results[0])
			}
			return w.Write(results)
		},
	}

	cmd.Flags().StringVarP(&nodeStr, "node", "n", "", "Node ID (required)")
	cmd.Flags().StringArrayVar(&attrs, "attr", nil, "Attribute name or numeric ID (repeatable)")
	cmd.Flags().BoolVar(&all, "all", false, "Read all standard OPC UA attributes")
	_ = cmd.RegisterFlagCompletionFunc("attr", fixedCompletion(attrValues))
	return cmd
}
