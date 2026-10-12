// SPDX-License-Identifier: MIT

package opcuactl

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/opcuactl/internal/format"
	"github.com/otfabric/opcuactl/internal/model"
	"github.com/otfabric/opcuactl/internal/parse"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newWriteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "write",
		Short: "Write values to OPC UA nodes",
		Long: `Write values and attributes to OPC UA server nodes.

Examples:
  opcuactl write value --endpoint opc.tcp://localhost:4840 --node "ns=2;s=Temperature" --value 42.5 --type double`,
	}
	cmd.AddCommand(newWriteValueCmd())
	return cmd
}

func newWriteValueCmd() *cobra.Command {
	var (
		nodeStr    string
		value      string
		typeName   string
		indexRange string
		dryRun     bool
		yes        bool
	)

	cmd := &cobra.Command{
		Use:   "value",
		Short: "Write a value to a node",
		Long: `Write a value to a node's Value attribute using WriteNodeValue.

The --type flag controls type coercion (bool, int16, uint16, int32, uint32, int64, uint64,
float, double, string, bytes, datetime, guid, nodeid, json). If omitted, the value is sent as a string.

--type ua-json and --type ua-xml take a Variant in the OPC UA JSON or XML
DataEncoding (Part 6), which reaches every data type: arrays, LocalizedText,
structures. It is the format "get value --ua-encoding json|xml" prints inside
"Value"; for JSON: {"UaType":<built-in type id>,"Value":<value>}.

Use --dry-run to validate parsing without sending the write to the server.
Use --index-range for writing to specific array elements (e.g. "0:2").
Use --yes to skip the safety confirmation prompt.

Examples:
  opcuactl write value -e opc.tcp://localhost:4840 -n "ns=2;s=Temp" --value 42.5 --type double
  opcuactl write value -e opc.tcp://localhost:4840 -n "ns=2;s=Active" --value true --type bool
  opcuactl write value -e opc.tcp://localhost:4840 -n "ns=2;s=Temp" --value 42.5 --type double --dry-run
  opcuactl write value -e opc.tcp://localhost:4840 -n "ns=2;s=Arr" --value 42 --type int32 --index-range 0
  opcuactl write value -e opc.tcp://localhost:4840 -n "ns=2;s=Arr" --value '{"UaType":6,"Value":[1,2,3]}' --type ua-json
  opcuactl write value -e opc.tcp://localhost:4840 -n "ns=2;s=Label" --value '{"UaType":21,"Value":{"Locale":"en","Text":"Tank 1"}}' --type ua-json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if nodeStr == "" {
				return exitCodef(1, "--node is required")
			}
			if value == "" {
				return exitCodef(1, "--value is required")
			}

			nodeID, err := parse.NodeID(nodeStr)
			if err != nil {
				return exitCode(1, err)
			}

			var goVal interface{}
			if parse.IsUAEncodedType(typeName) {
				variant, vErr := parse.UAVariant(value, typeName)
				if vErr != nil {
					return exitCodef(1, "decoding %s value: %v", typeName, vErr)
				}
				goVal = variant.Value()
			} else {
				goVal, err = parse.CoerceValue(value, typeName)
				if err != nil {
					return exitCodef(1, "type coercion failed: %v", err)
				}
			}

			// TTY safety confirmation
			if !dryRun && !yes && term.IsTerminal(int(os.Stdin.Fd())) {
				fmt.Fprintf(os.Stderr, "Write %v (%s) to %s on %s? [y/N] ", goVal, typeName, nodeID, connCfg.Endpoint)
				reader := bufio.NewReader(os.Stdin)
				line, _ := reader.ReadString('\n')
				if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "y") {
					fmt.Fprintln(os.Stderr, "Write cancelled.")
					return nil
				}
			}

			if dryRun {
				result := model.DryRunResult{
					NodeID:   nodeID.String(),
					NewValue: goVal,
					NewType:  typeName,
					Status:   "DryRun",
				}

				// Connect to read current state if possible.
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				if c, cErr := connCfg.NewClient(ctx); cErr == nil {
					defer func() { _ = c.Close(ctx) }()
					opCtx, opCancel := connCfg.TimeoutContext(ctx)
					defer opCancel()
					n := c.Node(nodeID)
					if summary, sErr := n.Summary(opCtx); sErr == nil {
						if summary.DisplayName != nil {
							result.DisplayName = summary.DisplayName.Text
						}
						if summary.DataType != nil {
							result.DataType = ua.DataTypeDisplayName(summary.DataType)
						}
						if summary.AccessLevel != 0 {
							result.AccessLevel = friendlyAccessLevel(summary.AccessLevel)
						}
					}
					if dv, rErr := c.ReadValue(opCtx, nodeID); rErr == nil && dv.Value != nil {
						result.CurrentValue = dv.Value.Value()
					}
				}

				w := newWriter()
				f := format.ParseFormat(outputFormat)
				if f == format.Table {
					curVal := ""
					if result.CurrentValue != nil {
						curVal = fmt.Sprintf("%v", result.CurrentValue)
					}
					headers := []string{"NODE ID", "DISPLAY NAME", "DATA TYPE", "ACCESS", "CURRENT VALUE", "NEW VALUE", "NEW TYPE", "STATUS"}
					rows := [][]string{{result.NodeID, result.DisplayName, result.DataType, result.AccessLevel, curVal, fmt.Sprintf("%v", result.NewValue), result.NewType, result.Status}}
					return w.WriteTable(headers, rows)
				}
				return w.Write(result)
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

			var sc ua.StatusCode
			if indexRange != "" {
				v, vErr := ua.NewVariant(goVal)
				if vErr != nil {
					return exitCodef(1, "creating variant: %v", vErr)
				}
				scs, wErr := c.WriteValues(opCtx, &ua.WriteValue{
					NodeID:      nodeID,
					AttributeID: ua.AttributeIDValue,
					IndexRange:  indexRange,
					Value:       &ua.DataValue{Value: v},
				})
				if wErr != nil {
					format.Errf("writing value to %s: %v", nodeID, wErr)
					return exitCode(3, wErr)
				}
				if len(scs) > 0 {
					sc = scs[0]
				}
			} else {
				var wErr error
				sc, wErr = c.WriteNodeValue(opCtx, nodeID, goVal)
				if wErr != nil {
					format.Errf("writing value to %s: %v", nodeID, wErr)
					return exitCode(3, wErr)
				}
			}

			result := model.WriteResult{
				NodeID:     nodeID.String(),
				Status:     sc.Symbol(),
				StatusCode: sc.Uint32(),
			}

			w := newWriter()
			f := format.ParseFormat(outputFormat)
			if f == format.Table {
				headers := []string{"NODE ID", "STATUS", "STATUS CODE"}
				rows := [][]string{{result.NodeID, result.Status, fmt.Sprintf("%d", result.StatusCode)}}
				return w.WriteTable(headers, rows)
			}
			return w.Write(result)
		},
	}

	cmd.Flags().StringVarP(&nodeStr, "node", "n", "", "Target node ID (required)")
	cmd.Flags().StringVar(&value, "value", "", "Value to write (required)")
	cmd.Flags().StringVar(&typeName, "type", "string", "Value type (bool|int16|uint16|int32|uint32|int64|uint64|float|double|string|bytes|datetime|guid|nodeid|json|ua-json|ua-xml)")
	cmd.Flags().StringVar(&indexRange, "index-range", "", "OPC UA index range for array writes (e.g. \"0:2\")")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Validate and parse only, do not send to server")
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip write confirmation prompt")
	_ = cmd.RegisterFlagCompletionFunc("type", fixedCompletion(writeTypeValues))
	return cmd
}
