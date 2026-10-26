// SPDX-License-Identifier: MIT

package opcuactl

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/otfabric/go-opcua"
	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/opcuactl/internal/format"
	"github.com/otfabric/opcuactl/internal/model"
	"github.com/otfabric/opcuactl/internal/parse"
	"github.com/spf13/cobra"
)

func newCallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "call",
		Short: "Invoke OPC UA methods",
		Long: `Invoke server-side methods and inspect method signatures.

Examples:
  opcuactl call method -e opc.tcp://localhost:4840 --object i=2253 --method "ns=0;i=11492" --arg 5 --types int32
  opcuactl call method -e opc.tcp://localhost:4840 --object i=2253 --method "ns=0;i=11492" --introspect`,
	}
	cmd.AddCommand(newCallMethodCmd())
	return cmd
}

func newCallMethodCmd() *cobra.Command {
	var (
		objectStr  string
		methodStr  string
		argStrs    []string
		typesStr   string
		argJSON    string
		argFile    string
		introspect bool
	)

	cmd := &cobra.Command{
		Use:   "method",
		Short: "Call an OPC UA method",
		Long: `Call a method on an OPC UA object node with optional arguments.

Use --introspect to show the method's input/output argument signatures without calling it.
Use --arg (repeatable) to pass arguments, and --types (comma-separated) for type coercion.
Use --arg-json to pass arguments as a JSON array (e.g. '[5, "hello", true]').
Use --arg-file to read arguments from a JSON file containing an array.

The JSON values are converted to the DataType and ValueRank of the method's
InputArguments: 5 is sent as an Int32 to an Int32 argument, [1, 2] as a
UInt16 array to a UInt16 array argument. Integer types (SByte..UInt64) take
integral numbers in range (or a string holding one, for Int64 beyond 2^53),
Float and Double numbers (or "NaN", "Infinity", "-Infinity"), DateTime an
RFC 3339 string, ByteString base64, NodeId/ExpandedNodeId their text form,
QualifiedName "ns:Name", LocalizedText a string or {"Locale":..,"Text":..}.
A value that does not fit its argument (2.5 or 3000000000 for an Int32) is a
usage error (exit 1). Without InputArguments, or for an argument of a
structure or BaseDataType, JSON numbers are sent as Double, strings as
String and true/false as Boolean.

Examples:
  opcuactl call method -e opc.tcp://localhost:4840 --object i=2253 --method "ns=0;i=11492"
  opcuactl call method -e opc.tcp://localhost:4840 --object i=2253 --method "ns=0;i=11492" --arg 5 --types int32
  opcuactl call method -e opc.tcp://localhost:4840 --object i=2253 --method "ns=0;i=11492" --arg hello --arg 42 --types string,int32
  opcuactl call method -e opc.tcp://localhost:4840 --object i=2253 --method "ns=0;i=11492" --arg-json '[5, "hello"]'
  opcuactl call method -e opc.tcp://localhost:4840 --object i=2253 --method "ns=0;i=11492" --arg-file args.json
  opcuactl call method -e opc.tcp://localhost:4840 --object i=2253 --method "ns=0;i=11492" --introspect`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if objectStr == "" {
				return exitCodef(1, "--object is required")
			}
			if methodStr == "" {
				return exitCodef(1, "--method is required")
			}

			objectID, err := parse.NodeID(objectStr)
			if err != nil {
				return exitCodef(1, "invalid --object: %v", err)
			}

			methodID, err := parse.NodeID(methodStr)
			if err != nil {
				return exitCodef(1, "invalid --method: %v", err)
			}

			// --arg-json / --arg-file: the JSON is checked before
			// connecting, its values are typed once the method signature
			// is known.
			var jsonArgs []byte
			jsonFlag := "--arg-json"
			switch {
			case argJSON != "":
				jsonArgs = []byte(argJSON)
			case argFile != "":
				jsonFlag = "--arg-file"
				if jsonArgs, err = os.ReadFile(argFile); err != nil {
					return exitCodef(1, "reading --arg-file: %v", err)
				}
			}
			if jsonArgs != nil {
				if _, err := parse.JSONArguments(jsonArgs, nil); err != nil {
					return exitCodef(1, "invalid %s: %v", jsonFlag, err)
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

			if introspect {
				inputs, outputs, err := methodSignature(opCtx, c, objectID, methodID)
				if err != nil {
					format.Errf("reading method arguments: %v", err)
					return exitCode(3, err)
				}

				type introspectResult struct {
					ObjectID string                 `json:"objectId"`
					MethodID string                 `json:"methodId"`
					Inputs   []model.MethodArgument `json:"inputs"`
					Outputs  []model.MethodArgument `json:"outputs"`
				}

				result := introspectResult{
					ObjectID: objectID.String(),
					MethodID: methodID.String(),
				}
				for _, a := range inputs {
					ma := model.MethodArgument{
						Name:      a.Name,
						ValueRank: a.ValueRank,
					}
					if a.DataType != nil {
						ma.DataType = a.DataType.String()
					}
					if a.Description != nil {
						ma.Description = a.Description.Text
					}
					result.Inputs = append(result.Inputs, ma)
				}
				for _, a := range outputs {
					ma := model.MethodArgument{
						Name:      a.Name,
						ValueRank: a.ValueRank,
					}
					if a.DataType != nil {
						ma.DataType = a.DataType.String()
					}
					if a.Description != nil {
						ma.Description = a.Description.Text
					}
					result.Outputs = append(result.Outputs, ma)
				}

				w := newWriter()
				f := format.ParseFormat(outputFormat)
				if f == format.Table {
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Object: %s  Method: %s\n\n", objectID, methodID)
					if len(result.Inputs) > 0 {
						_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Input Arguments:")
						headers := []string{"NAME", "DATA TYPE", "VALUE RANK", "DESCRIPTION"}
						rows := make([][]string, len(result.Inputs))
						for i, a := range result.Inputs {
							rows[i] = []string{a.Name, a.DataType, fmt.Sprintf("%d", a.ValueRank), a.Description}
						}
						if err := w.WriteTable(headers, rows); err != nil {
							return err
						}
					}
					if len(result.Outputs) > 0 {
						_, _ = fmt.Fprintln(cmd.OutOrStdout(), "\nOutput Arguments:")
						headers := []string{"NAME", "DATA TYPE", "VALUE RANK", "DESCRIPTION"}
						rows := make([][]string, len(result.Outputs))
						for i, a := range result.Outputs {
							rows[i] = []string{a.Name, a.DataType, fmt.Sprintf("%d", a.ValueRank), a.Description}
						}
						return w.WriteTable(headers, rows)
					}
					return nil
				}
				return w.Write(result)
			}

			// Parse and coerce arguments
			var callArgs []any

			if jsonArgs != nil {
				// The InputArguments of the method give the types of the
				// JSON values; without them (or for a method that does
				// not exist, which the call reports) numbers are Doubles.
				inputs, _, sigErr := c.MethodArguments(opCtx, objectID, methodID)
				if sigErr != nil {
					inputs = nil
				}
				callArgs, err = parse.JSONArguments(jsonArgs, inputs)
				if err != nil {
					return exitCodef(1, "invalid %s: %v", jsonFlag, err)
				}
			} else {
				var types []string
				if typesStr != "" {
					types = strings.Split(typesStr, ",")
				}
				callArgs = make([]any, len(argStrs))
				for i, raw := range argStrs {
					typeName := "string"
					if i < len(types) {
						typeName = strings.TrimSpace(types[i])
					}
					v, err := parse.CoerceValue(raw, typeName)
					if err != nil {
						return exitCodef(1, "argument %d type coercion failed: %v", i, err)
					}
					callArgs[i] = v
				}
			}

			result, err := callMethod(opCtx, c, objectID, methodID, callArgs)
			if err != nil {
				format.Errf("calling method %s on %s: %v", methodID, objectID, err)
				return exitCode(3, err)
			}

			var outArgs []interface{}
			for _, v := range result.OutputArguments {
				if v != nil {
					outArgs = append(outArgs, v.Value())
				}
			}

			callResult := model.CallResult{
				ObjectID:        objectID.String(),
				MethodID:        methodID.String(),
				Status:          result.StatusCode.Symbol(),
				StatusCode:      result.StatusCode.Uint32(),
				OutputArguments: outArgs,
			}

			w := newWriter()
			f := format.ParseFormat(outputFormat)
			if f == format.Table {
				headers := []string{"OBJECT", "METHOD", "STATUS", "STATUS CODE"}
				rows := [][]string{{callResult.ObjectID, callResult.MethodID, callResult.Status, fmt.Sprintf("%d", callResult.StatusCode)}}
				if err := w.WriteTable(headers, rows); err != nil {
					return err
				}
				if len(outArgs) > 0 {
					_, _ = fmt.Fprintln(cmd.OutOrStdout(), "\nOutput Arguments:")
					for i, v := range outArgs {
						_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  [%d] %v\n", i, v)
					}
				}
			} else if err := w.Write(callResult); err != nil {
				return err
			}
			// A Bad result of the method is a status-code failure (exit 3).
			if isBadStatus(result.StatusCode) {
				return exitCodef(3, "calling method %s on %s: %s", methodID, objectID, result.StatusCode.Symbol())
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&objectStr, "object", "", "Object node ID (required)")
	cmd.Flags().StringVar(&methodStr, "method", "", "Method node ID (required)")
	cmd.Flags().StringArrayVar(&argStrs, "arg", nil, "Method argument value (repeatable)")
	cmd.Flags().StringVar(&typesStr, "types", "", "Comma-separated type names for argument coercion")
	cmd.Flags().StringVar(&argJSON, "arg-json", "", "Arguments as a JSON array (e.g. '[5, \"hello\"]'), typed by the InputArguments of the method")
	cmd.Flags().StringVar(&argFile, "arg-file", "", "Read arguments from a JSON file containing an array")
	cmd.Flags().BoolVar(&introspect, "introspect", false, "Show method argument signatures without calling")
	return cmd
}

// callMethod calls one method and prints the DiagnosticInfos the server
// returns for it and its input arguments (--return-diagnostics operation).
func callMethod(ctx context.Context, c *opcua.Client, objectID, methodID *ua.NodeID, args []any) (*ua.CallMethodResult, error) {
	variants := make([]*ua.Variant, len(args))
	for i, a := range args {
		v, err := ua.NewVariant(a)
		if err != nil {
			return nil, fmt.Errorf("argument %d: %w", i, err)
		}
		variants[i] = v
	}
	var resp *ua.CallResponse
	err := c.Send(ctx, &ua.CallRequest{MethodsToCall: []*ua.CallMethodRequest{{
		ObjectID:       objectID,
		MethodID:       methodID,
		InputArguments: variants,
	}}}, func(r ua.Response) error {
		cr, ok := r.(*ua.CallResponse)
		if !ok {
			return fmt.Errorf("unexpected response %T", r)
		}
		resp = cr
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(resp.Results) != 1 || resp.Results[0] == nil {
		return nil, ua.StatusBadUnknownResponse
	}
	res := resp.Results[0]
	printOperationDiagnostics(resp.ResponseHeader, resp.DiagnosticInfos, func(int) string {
		return "call " + methodID.String()
	})
	printOperationDiagnostics(resp.ResponseHeader, res.InputArgumentDiagnosticInfos, func(i int) string {
		return fmt.Sprintf("call %s input argument %d", methodID, i)
	})
	return res, nil
}

// methodSignature reads the input and output arguments of methodID. A node
// that does not exist or is not a Method is an error with its status
// (BadNodeIdUnknown, BadNodeClassInvalid), not an empty signature.
func methodSignature(ctx context.Context, c *opcua.Client, objectID, methodID *ua.NodeID) (inputs, outputs []*ua.Argument, err error) {
	class, err := c.Node(methodID).NodeClass(ctx)
	if err != nil {
		return nil, nil, err
	}
	if class != ua.NodeClassMethod {
		return nil, nil, fmt.Errorf("%s is a %s, not a Method: %w", methodID, nodeClassName(class), ua.StatusBadNodeClassInvalid)
	}
	return c.MethodArguments(ctx, objectID, methodID)
}

// nodeClassName is the NodeClass without the NodeClass prefix (Variable).
func nodeClassName(c ua.NodeClass) string {
	return strings.TrimPrefix(c.String(), "NodeClass")
}
