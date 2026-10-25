// SPDX-License-Identifier: MIT

package opcuactl

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/otfabric/go-opcua"
	"github.com/otfabric/go-opcua/pubsub"
	"github.com/otfabric/go-opcua/pubsub/remote"
	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/opcuactl/internal/format"
	"github.com/otfabric/opcuactl/internal/model"
	"github.com/otfabric/opcuactl/internal/parse"
	"github.com/spf13/cobra"
)

// The commands on the PubSubConfiguration file of a server (Part 14
// §9.1.3.7).

func newPubSubExportCmd() *cobra.Command {
	var out, encoding string
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Read the PubSubConfiguration file of a server",
		Long: `Read the PubSubConfiguration file of a server (PublishSubscribe.PubSubConfiguration,
Part 14 §9.1.3.7): its whole configuration as a PubSubConfiguration2DataType,
printed in the OPC UA JSON encoding (--ua-encoding json-reversible by
default) or written to --out.

Examples:
  opcuactl pubsub config export -e opc.tcp://localhost:4840
  opcuactl pubsub config export -e opc.tcp://localhost:4840 --out pubsub.json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			enc, err := parse.ParseUAEncoding(encoding)
			if err != nil {
				return exitCode(1, err)
			}
			return withRemote(func(ctx context.Context, _ *opcua.Client, r *remote.Client) error {
				cfg, err := r.ReadConfiguration(ctx)
				if err != nil {
					return err
				}
				b, err := enc.Encode(cfg, true)
				if err != nil {
					return err
				}
				b = append(b, '\n')
				if out != "" {
					return os.WriteFile(out, b, 0o644)
				}
				_, err = os.Stdout.Write(b)
				return err
			})
		},
	}
	cmd.Flags().StringVar(&out, "out", "", "Write the configuration to this file")
	cmd.Flags().StringVar(&encoding, "ua-encoding", "json-reversible", "OPC UA encoding of the output (json|json-verbose|json-reversible|json-nonreversible|xml)")
	return cmd
}

func newPubSubReserveIDsCmd() *cobra.Command {
	var url, encoding string
	var groups, writers uint16
	cmd := &cobra.Command{
		Use:   "reserve-ids",
		Short: "Reserve WriterGroupIds and DataSetWriterIds (ReserveIds)",
		Long: `Reserve WriterGroupIds and DataSetWriterIds with the ReserveIds method of the
PubSubConfiguration file (Part 14 §9.1.3.7.5) and print the default
PublisherId of the server for the transport of --url and --encoding. The
reservation lasts as long as the session, so this shows which ids a server
gives out.

Example:
  opcuactl pubsub config reserve-ids -e opc.tcp://localhost:4840 --url mqtt://broker:1883 --writer-groups 1 --writers 2`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			enc, err := parse.ParsePubSubEncoding(encoding, false)
			if err != nil {
				return exitCode(1, err)
			}
			profile, err := transportProfile(url, enc)
			if err != nil {
				return exitCodef(1, "--url: %v", err)
			}
			return withRemote(func(ctx context.Context, _ *opcua.Client, r *remote.Client) error {
				ids, err := r.ReserveIDs(ctx, profile, groups, writers)
				if err != nil {
					return err
				}
				res := model.PubSubReservedIDs{TransportProfileURI: profile, WriterGroupIDs: ids.WriterGroupIDs, DataSetWriterIDs: ids.DataSetWriterIDs}
				if ids.DefaultPublisherID != nil {
					res.DefaultPublisherID = format.FormatValue(ids.DefaultPublisherID.Value())
				}
				if format.ParseFormat(outputFormat) != format.Table {
					return newWriter().Write(res)
				}
				fmt.Printf("default publisher id: %s\n", res.DefaultPublisherID)
				fmt.Printf("writer group ids:     %s\n", joinIDs(res.WriterGroupIDs))
				fmt.Printf("dataset writer ids:   %s\n", joinIDs(res.DataSetWriterIDs))
				return nil
			})
		},
	}
	f := cmd.Flags()
	f.StringVar(&url, "url", "opc.udp://224.0.0.22:4840", "Transport URL that selects the transport profile")
	f.StringVar(&encoding, "encoding", "uadp", "Message mapping (uadp|json)")
	f.Uint16Var(&groups, "writer-groups", 1, "Number of WriterGroupIds")
	f.Uint16Var(&writers, "writers", 1, "Number of DataSetWriterIds")
	_ = cmd.RegisterFlagCompletionFunc("encoding", fixedCompletion([]string{"uadp", "json"}))
	return cmd
}

func joinIDs(ids []uint16) string {
	s := make([]string, len(ids))
	for i, x := range ids {
		s[i] = strconv.Itoa(int(x))
	}
	return strings.Join(s, ", ")
}

// parseActionMethod parses ID=OBJECT@METHOD.
func parseActionMethod(s string) (uint16, *ua.ActionMethodDataType, error) {
	id, rest, ok := strings.Cut(s, "=")
	obj, method, ok2 := cutLast(rest, "@")
	if !ok || !ok2 {
		return 0, nil, fmt.Errorf("invalid method %q: want ID=OBJECT@METHOD", s)
	}
	n, err := strconv.ParseUint(id, 10, 16)
	if err != nil {
		return 0, nil, fmt.Errorf("invalid method %q: ActionTargetId: %v", s, err)
	}
	o, err := parse.NodeIDWithNamespace(obj, namespace)
	if err != nil {
		return 0, nil, fmt.Errorf("invalid method %q: object: %v", s, err)
	}
	m, err := parse.NodeIDWithNamespace(method, namespace)
	if err != nil {
		return 0, nil, fmt.Errorf("invalid method %q: method: %v", s, err)
	}
	return uint16(n), pubsub.ActionMethodOf(o, m), nil
}

func cutLast(s, sep string) (string, string, bool) {
	i := strings.LastIndex(s, sep)
	if i < 0 {
		return s, "", false
	}
	return s[:i], s[i+len(sep):], true
}

func newPubSubAddActionCmd() *cobra.Command {
	var (
		name                                    string
		targetSpecs, methodSpecs, request, resp []string
	)
	cmd := &cobra.Command{
		Use:   "add-action",
		Short: "Add a PublishedAction data set (PubSubConfiguration CloseAndUpdate)",
		Long: `Add a PublishedAction (Part 14 §6.2.3.10.3) to a server through its
PubSubConfiguration file (CloseAndUpdate, §9.1.3.7.6): the request fields
--request, the response fields --response (Name:Type) and the Action targets
--target ID:NAME. With --method ID=OBJECT@METHOD for every target it is a
PublishedActionMethod whose targets call these Methods of the server.

A DataSetWriter of the data set in a WriterGroup (add-writer) makes the
server the Responder; "pubsub call" calls it.

Examples:
  opcuactl pubsub config add-action -e opc.tcp://localhost:4840 --name Calc \
      --request A:Int32 --request B:Int32 --response Sum:Int32 --target 1:Add
  opcuactl pubsub config add-action -e opc.tcp://localhost:4840 --name Square \
      --request n:Int32 --response result:Int32 --target 1:Square \
      --method '1=ns=1;s=Methods@ns=1;s=Square'`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if name == "" || len(targetSpecs) == 0 {
				return exitCodef(1, "--name and --target are required")
			}
			reqFields, err := parseActionFields(request, "--request")
			if err != nil {
				return err
			}
			respFields, err := parseActionFields(resp, "--response")
			if err != nil {
				return err
			}
			targets := make([]*ua.ActionTargetDataType, len(targetSpecs))
			index := map[uint16]int{}
			for i, spec := range targetSpecs {
				id, tname, err := parse.ActionTarget(spec)
				if err != nil {
					return exitCodef(1, "--target: %v", err)
				}
				if _, dup := index[id]; dup {
					return exitCodef(1, "--target: ActionTargetId %d given twice", id)
				}
				index[id] = i
				targets[i] = pubsub.ActionTargetOf(id, tname)
			}
			pds := pubsub.PublishedActions(name, reqFields, respFields, targets...)
			if len(methodSpecs) > 0 {
				methods := make([]*ua.ActionMethodDataType, len(targets))
				for _, spec := range methodSpecs {
					id, m, err := parseActionMethod(spec)
					if err != nil {
						return exitCodef(1, "--method: %v", err)
					}
					i, ok := index[id]
					if !ok {
						return exitCodef(1, "--method: no --target %d", id)
					}
					if methods[i] != nil {
						return exitCodef(1, "--method: ActionTargetId %d given twice", id)
					}
					methods[i] = m
				}
				for i, m := range methods {
					if m == nil {
						return exitCodef(1, "--method: none for target %d (give one for every target or none)", targets[i].ActionTargetID)
					}
				}
				pds = pubsub.PublishedActionMethods(name, reqFields, respFields, targets, methods)
			}
			return withRemote(func(ctx context.Context, _ *opcua.Client, r *remote.Client) error {
				res, err := r.UpdateConfiguration(ctx, &ua.PubSubConfiguration2DataType{PublishedDataSets: []*ua.PublishedDataSetDataType{pds}},
					[]*ua.PubSubConfigurationRefDataType{remote.Ref(ua.PubSubConfigurationRefMaskElementAdd, ua.PubSubConfigurationRefMaskReferencePubDataset, 0, 0, 0)}, true)
				if err != nil {
					return err
				}
				if len(res.Results) != 1 {
					return fmt.Errorf("CloseAndUpdate returned %d results for 1 reference", len(res.Results))
				}
				if !res.ChangesApplied || res.Results[0] != ua.StatusOK {
					return fmt.Errorf("CloseAndUpdate: %w", res.Results[0])
				}
				nid := ua.NewTwoByteNodeID(0)
				if len(res.Objects) == 1 && res.Objects[0] != nil {
					nid = res.Objects[0]
				}
				return writeAdded("dataset", name, nid, nil)
			})
		},
	}
	f := cmd.Flags()
	f.StringVar(&name, "name", "", "Name of the PublishedAction data set")
	f.StringArrayVar(&request, "request", nil, "Request field Name:Type (repeatable)")
	f.StringArrayVar(&resp, "response", nil, "Response field Name:Type (repeatable)")
	f.StringArrayVar(&targetSpecs, "target", nil, "Action target ID:NAME (repeatable)")
	f.StringArrayVar(&methodSpecs, "method", nil, "Method of a target ID=OBJECT@METHOD (repeatable; one per target or none)")
	return cmd
}
