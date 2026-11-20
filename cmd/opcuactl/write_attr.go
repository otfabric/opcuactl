// SPDX-License-Identifier: MIT

package opcuactl

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/opcuactl/internal/format"
	"github.com/otfabric/opcuactl/internal/model"
	"github.com/otfabric/opcuactl/internal/parse"
	"github.com/spf13/cobra"
)

// writeAttrTypeValues are the --type values of write attr.
var writeAttrTypeValues = []string{"auto", "bool", "byte", "uint16", "int32", "uint32", "double", "string",
	"localizedtext", "qualifiedname", "nodeid", "uint32-array", "ua-json", "ua-xml"}

// attributeType is the --type of an attribute's data type (Part 3 §5, the
// attributes of the NodeClasses); "" for a structure, which needs ua-json
// or ua-xml.
func attributeType(attr ua.AttributeID) string {
	switch attr {
	case ua.AttributeIDDisplayName, ua.AttributeIDDescription, ua.AttributeIDInverseName:
		return "localizedtext"
	case ua.AttributeIDBrowseName:
		return "qualifiedname"
	case ua.AttributeIDNodeID, ua.AttributeIDDataType:
		return "nodeid"
	case ua.AttributeIDNodeClass, ua.AttributeIDValueRank:
		return "int32"
	case ua.AttributeIDWriteMask, ua.AttributeIDUserWriteMask, ua.AttributeIDAccessLevelEx:
		return "uint32"
	case ua.AttributeIDIsAbstract, ua.AttributeIDSymmetric, ua.AttributeIDContainsNoLoops,
		ua.AttributeIDHistorizing, ua.AttributeIDExecutable, ua.AttributeIDUserExecutable:
		return "bool"
	case ua.AttributeIDEventNotifier, ua.AttributeIDAccessLevel, ua.AttributeIDUserAccessLevel:
		return "byte"
	case ua.AttributeIDAccessRestrictions:
		return "uint16"
	case ua.AttributeIDMinimumSamplingInterval:
		return "double"
	case ua.AttributeIDArrayDimensions:
		return "uint32-array"
	}
	return ""
}

// attributeValue converts --value to the Go value of typ.
func attributeValue(value, typ string) (any, error) {
	switch strings.ToLower(typ) {
	case "ua-json", "ua-xml":
		v, err := parse.UAVariant(value, typ)
		if err != nil {
			return nil, err
		}
		return v.Value(), nil
	case "bool", "boolean":
		return strconv.ParseBool(value)
	case "byte":
		v, err := strconv.ParseUint(value, 0, 8)
		return byte(v), err
	case "uint16":
		v, err := strconv.ParseUint(value, 0, 16)
		return uint16(v), err
	case "int32":
		v, err := strconv.ParseInt(value, 0, 32)
		return int32(v), err
	case "uint32":
		v, err := strconv.ParseUint(value, 0, 32)
		return uint32(v), err
	case "double":
		return strconv.ParseFloat(value, 64)
	case "string":
		return value, nil
	case "localizedtext":
		locale, text, ok := strings.Cut(value, ":")
		if !ok || !isLocale(locale) {
			return ua.NewLocalizedText(value), nil
		}
		return ua.NewLocalizedTextWithLocale(text, locale), nil
	case "qualifiedname":
		ns, name, ok := strings.Cut(value, ":")
		if !ok {
			return &ua.QualifiedName{Name: value}, nil
		}
		n, err := strconv.ParseUint(ns, 10, 16)
		if err != nil {
			return nil, fmt.Errorf("namespace index %q of %q: want ns:name", ns, value)
		}
		return &ua.QualifiedName{NamespaceIndex: uint16(n), Name: name}, nil
	case "nodeid":
		return parse.NodeID(value)
	case "uint32-array":
		out := []uint32{}
		for _, f := range splitCSV(value) {
			v, err := strconv.ParseUint(f, 10, 32)
			if err != nil {
				return nil, fmt.Errorf("array dimension %q: %w", f, err)
			}
			out = append(out, uint32(v))
		}
		return out, nil
	}
	return nil, fmt.Errorf("unknown --type %q (%s)", typ, strings.Join(writeAttrTypeValues, "|"))
}

func newWriteAttrCmd() *cobra.Command {
	var (
		nodeStr  string
		attrName string
		value    string
		typeName string
		dryRun   bool
		yes      bool
	)
	cmd := &cobra.Command{
		Use:   "attr",
		Short: "Write an attribute of a node",
		Long: `Write an attribute other than Value of a node, such as DisplayName,
Description, BrowseName, WriteMask, AccessLevel, Historizing,
MinimumSamplingInterval, ValueRank, ArrayDimensions or EventNotifier (write value
writes the Value). --attr is the name or numeric id of the attribute, as in
get attr.

--value is converted to the data type of the attribute (--type auto):
  DisplayName, Description, InverseName   [locale:]text
  BrowseName                              [ns:]name
  DataType                                a NodeId
  ArrayDimensions                         comma-separated UInt32 (2,3)
  AccessLevel, UserAccessLevel, EventNotifier, WriteMask, AccessLevelEx
                                          a number (decimal, or 0x.. hex)
  IsAbstract, Historizing, Executable, ...  true or false
--type ua-json and ua-xml take a Variant in the OPC UA JSON or XML encoding,
for the structured attributes (RolePermissions, DataTypeDefinition).

Servers let a client write an attribute only when the WriteMask of the node
allows it (Part 3, WriteMask); other writes are refused, typically with
BadNotWritable.

Use --dry-run to read the current attribute and show the conversion without
writing. Use --yes to skip the confirmation prompt.

Examples:
  opcuactl write attr -e opc.tcp://localhost:4840 -n "ns=2;s=Temp" --attr DisplayName --value "en:Temperature"
  opcuactl write attr -e opc.tcp://localhost:4840 -n "ns=2;s=Temp" --attr Description --value "Tank 1 temperature" --yes
  opcuactl write attr -e opc.tcp://localhost:4840 -n "ns=2;s=Temp" --attr Historizing --value true
  opcuactl write attr -e opc.tcp://localhost:4840 -n "ns=2;s=Temp" --attr AccessLevel --value 0x03 --dry-run`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if nodeStr == "" {
				return exitCodef(exitUsage, "--node is required")
			}
			if attrName == "" {
				return exitCodef(exitUsage, "--attr is required")
			}
			if !cmd.Flags().Changed("value") {
				return exitCodef(exitUsage, "--value is required")
			}
			nodeID, err := parse.NodeID(nodeStr)
			if err != nil {
				return exitCode(exitUsage, err)
			}
			attr, err := parse.AttributeIDFromName(attrName)
			if err != nil {
				return exitCode(exitUsage, err)
			}
			if attr == ua.AttributeIDValue {
				return exitCodef(exitUsage, "--attr Value: use write value, which converts to the data type of the variable and takes --index-range")
			}
			typ := strings.ToLower(typeName)
			if typ == "" || typ == "auto" {
				if typ = attributeType(attr); typ == "" {
					return exitCodef(exitUsage, "--attr %s is a structure: give the value with --type ua-json or ua-xml", friendlyAttrName(attr))
				}
			}
			goVal, err := attributeValue(value, typ)
			if err != nil {
				return exitCodef(exitUsage, "--value for %s: %v", friendlyAttrName(attr), err)
			}
			variant, err := ua.NewVariant(goVal)
			if err != nil {
				return exitCodef(exitUsage, "creating variant: %v", err)
			}

			if !dryRun && !yes && !confirmHistoryChange(fmt.Sprintf("Write %s = %s to %s on %s?", friendlyAttrName(attr), format.FormatValue(goVal), nodeID, connCfg.Endpoint)) {
				fmt.Fprintln(os.Stderr, "Write cancelled.")
				return nil
			}

			ctx, cancel := context.WithCancel(rootContext())
			defer cancel()
			if dryRun {
				return writeAttrDryRun(ctx, nodeID, attr, goVal, typ)
			}
			c, err := connCfg.NewClient(ctx)
			if err != nil {
				format.Errf("%v", err)
				return exitCode(exitTransport, err)
			}
			defer func() { _ = c.Close(ctx) }()
			opCtx, opCancel := connCfg.TimeoutContext(ctx)
			defer opCancel()

			resp, err := c.Write(opCtx, &ua.WriteRequest{NodesToWrite: []*ua.WriteValue{{
				NodeID:      nodeID,
				AttributeID: attr,
				Value:       &ua.DataValue{EncodingMask: ua.DataValueValue, Value: variant},
			}}})
			return writeAttrResponse(resp, err, nodeID, attr)
		},
	}
	cmd.Flags().StringVarP(&nodeStr, "node", "n", "", "Target node ID (required)")
	cmd.Flags().StringVar(&attrName, "attr", "", "Attribute name or numeric id (required; e.g. DisplayName, Description, AccessLevel)")
	cmd.Flags().StringVar(&value, "value", "", "Value to write (required)")
	cmd.Flags().StringVar(&typeName, "type", "auto", "Value type ("+strings.Join(writeAttrTypeValues, "|")+"); auto: the data type of the attribute")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Read the current attribute and show the conversion, do not write")
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip write confirmation prompt")
	_ = cmd.RegisterFlagCompletionFunc("type", fixedCompletion(writeAttrTypeValues))
	_ = cmd.RegisterFlagCompletionFunc("attr", fixedCompletion(writableAttributeNames()))
	return cmd
}

// writeAttrResponse prints the result of the one write of write attr and
// returns the exit.
func writeAttrResponse(resp *ua.WriteResponse, err error, nodeID *ua.NodeID, attr ua.AttributeID) error {
	if err != nil {
		format.Errf("writing %s of %s: %v", friendlyAttrName(attr), nodeID, err)
		return exitCode(exitProtocol, err)
	}
	printOperationDiagnostics(resp.ResponseHeader, resp.DiagnosticInfos, func(int) string {
		return "write " + friendlyAttrName(attr) + " of " + nodeID.String()
	})
	if len(resp.Results) != 1 {
		return exitCodef(exitProtocol, "writing %s of %s: %d results for 1 write", friendlyAttrName(attr), nodeID, len(resp.Results))
	}
	sc := resp.Results[0]
	result := model.WriteResult{NodeID: nodeID.String(), Attribute: friendlyAttrName(attr), Status: sc.Symbol(), StatusCode: sc.Uint32()}
	w := newWriter()
	if format.ParseFormat(outputFormat) == format.Table {
		err = w.WriteTable([]string{"NODE ID", "ATTRIBUTE", "STATUS", "STATUS CODE"},
			[][]string{{result.NodeID, result.Attribute, result.Status, strconv.FormatUint(uint64(result.StatusCode), 10)}})
	} else {
		err = w.Write(result)
	}
	if err != nil {
		return err
	}
	if isBadStatus(sc) {
		return exitCodef(exitProtocol, "writing %s of %s: %s", friendlyAttrName(attr), nodeID, sc.Symbol())
	}
	return nil
}

// writableAttributeNames are the --attr completions: the attributes other
// than Value.
func writableAttributeNames() []string {
	var out []string
	for a := ua.AttributeIDNodeID; a <= ua.AttributeIDAccessLevelEx; a++ {
		if a != ua.AttributeIDValue {
			out = append(out, friendlyAttrName(a))
		}
	}
	return out
}

// writeAttrDryRun reads the attribute, when the server can be reached, and
// prints what would be written.
func writeAttrDryRun(ctx context.Context, nodeID *ua.NodeID, attr ua.AttributeID, goVal any, typ string) error {
	result := model.DryRunResult{NodeID: nodeID.String(), Attribute: friendlyAttrName(attr), NewValue: goVal, NewType: typ, Status: "DryRun"}
	if c, err := connCfg.NewClient(ctx); err == nil {
		defer func() { _ = c.Close(ctx) }()
		opCtx, opCancel := connCfg.TimeoutContext(ctx)
		defer opCancel()
		resp, err := c.Read(opCtx, &ua.ReadRequest{NodesToRead: []*ua.ReadValueID{{NodeID: nodeID, AttributeID: attr}}})
		if err == nil && len(resp.Results) == 1 && resp.Results[0].Status == ua.StatusOK && resp.Results[0].Value != nil {
			result.CurrentValue = resp.Results[0].Value.Value()
		}
	}
	w := newWriter()
	if format.ParseFormat(outputFormat) == format.Table {
		cur := ""
		if result.CurrentValue != nil {
			cur = format.FormatValue(result.CurrentValue)
		}
		return w.WriteTable([]string{"NODE ID", "ATTRIBUTE", "CURRENT VALUE", "NEW VALUE", "NEW TYPE", "STATUS"},
			[][]string{{result.NodeID, result.Attribute, cur, format.FormatValue(result.NewValue), result.NewType, result.Status}})
	}
	return w.Write(result)
}
