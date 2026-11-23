// SPDX-License-Identifier: MIT

package opcuactl

import (
	"context"
	"fmt"
	"strings"

	"github.com/otfabric/go-opcua"
	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/go-opcua/uafx"
	"github.com/otfabric/go-opcua/uafx/fxacid"
	"github.com/otfabric/opcuactl/internal/format"
	"github.com/otfabric/opcuactl/internal/model"
	"github.com/spf13/cobra"
)

// The commands on the connections of an AutomationComponent: fx connect
// calls EstablishConnections (Part 81 §6.2.4), fx close CloseConnections
// (§6.2.5).

func newFxConnectCmd() *cobra.Command {
	var ac, request string
	cmd := &cobra.Command{
		Use:   "connect",
		Short: "Establish a connection endpoint of an AutomationComponent (EstablishConnections)",
		Long: `Call EstablishConnections on an AutomationComponent (OPC 10000-81
§6.2.4) with the request in --request, a JSON or YAML file, and print the
results of the call.

A UAFX connection has an endpoint on each AutomationComponent; fx connect
sets up one side, so a connection takes a call on each of the two. The
commands of the request run in the order of §6.2.4.3.1: verify the Assets
and FunctionalEntities, reserve PubSub ids, create the endpoints, write
ConfigurationData, apply the PubSub configuration and enable it. A
request may also verify only, without creating anything.

The request file:

  commands: [VerifyFunctionalEntity, CreateConnectionEndpoint,
             SetCommunicationConfiguration, EnableCommunication]
  endpoints:
    - functionalEntity: nsu=urn:example:line;i=1200
      name: ToLine                    # or node: an existing endpoint
      mode: PublisherSubscriber       # Publisher, Subscriber
      inputs:  [nsu=urn:example:line;i=1211]
      outputs: [nsu=urn:example:line;i=1212]
      cleanupTimeoutMs: 5000          # absent: -1, no cleanup
      preconfigured: true             # take the preconfigured endpoint
      relatedEndpoint: {address: opc.tcp://peer:4840, name: FromController,
                        path: ["urn:example:peer;Peer", "urn:example:peer;Motion"]}
      expectedVerificationVariables:
        - {node: nsu=...;i=1203, type: ua-json, value: {...}}
      configurationData:
        - {node: nsu=...;i=1220, type: Double, value: 2.5}
      communicationLinks:
        dataSetReader: {mask: [ReferenceReader], connection: 1}
        dataSetWriter: {mask: [ReferenceWriter]}
  reserveCommunicationIds:
    - {transportProfileUri: http://opcfoundation.org/UA-Profile/Transport/pubsub-udp-uadp,
       writerGroups: 1, dataSetWriters: 1, transportSpecificInfo: true}
  communicationConfigurations:
    - pubsubConfigurationFile: pubsub.json   # or pubsubConfiguration: {...}
      references:
        - {mask: [ElementAdd, ReferenceConnection], connection: 0}
  assetVerifications:
    - {asset: nsu=...;i=1100, mode: Identity, expectedResult: Match,
       variables: [{key: "nsu=http://opcfoundation.org/UA/DI/;Manufacturer",
                    type: LocalizedText, value: {Text: ACME}}]}

NodeIds are in the string form of Part 6 ("ns=2;i=5"), or with the
namespace URI ("nsu=<uri>;i=5"), which is resolved with the NamespaceArray
of the server. A value has a built-in type (Int32, Double, String, ...)
or "ua-json" for a Variant in the OPC UA JSON encoding; a value of null
without a type only checks that the node exists. The PubSub configuration
is a PubSubConfiguration2DataType in the OPC UA JSON encoding, as
pubsub config export writes it. Its NodeIds are not resolved: they keep
their namespace indexes, which must be those of the target server. The
commands and the fields of the file are those of Part 81
§10: a field a command does not use must be empty (Table 8).

The server aborts the call when a command fails and returns Uncertain;
the results tell which element failed and which commands did not run
(BadNothingToDo). The rollback follows Part 81 Table 21, which does not
undo everything: ConfigurationData the call wrote and ids it reserved
stay, so an aborted call may leave changes on the server.

Exit codes: 0 when the call is Good; 6 when it is Uncertain (aborted) or
Bad, with the results printed for Uncertain, also when an element of an
aborted call failed with BadUserAccessDenied; 9 when the server refuses
the call itself to the user; 2 for a request file that is not valid and 3 for
one that cannot be read.

Examples:
  opcuactl fx connect -e opc.tcp://localhost:4855 \
    --ac "nsu=urn:example:uafx:controller;i=1000" --request connect.yaml
  opcuactl fx connect -e opc.tcp://localhost:4855 \
    --ac "ns=2;i=1000" --request verify.json --format json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			f, err := readFxRequest(request)
			if err != nil {
				return err
			}
			// The commands are checked before connecting.
			if _, err := fxCommandMask(f.Commands); err != nil {
				return exitCode(exitUsage, err)
			}
			return withClient(func(ctx context.Context, c *opcua.Client) error {
				b, acID, err := fxBuilder(ctx, c, ac)
				if err != nil {
					return err
				}
				req, err := b.build(f)
				if err != nil {
					return exitCode(exitUsage, err)
				}
				res, err := uafx.EstablishConnections(ctx, c, acID, req)
				if err != nil {
					format.Errf("EstablishConnections: %v", err)
					return exitCode(exitProtocol, err)
				}
				out := fxConnectResult(acID, req.CommandMask, res)
				if format.ParseFormat(outputFormat) == format.Table {
					printFxConnectResult(out)
				} else if err := newWriter().Write(out); err != nil {
					return err
				}
				if res.Status != ua.StatusOK {
					return exitCodef(exitProtocol, "EstablishConnections returned %s: the call was aborted; rollback followed Part 81 Table 21, and some changes may remain", res.Status.Symbol())
				}
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&ac, "ac", "", "NodeId of the AutomationComponent (ns=<index>;... or nsu=<uri>;...)")
	cmd.Flags().StringVar(&request, "request", "", "The request: a JSON or YAML file")
	_ = cmd.MarkFlagRequired("ac")
	_ = cmd.MarkFlagRequired("request")
	return cmd
}

func newFxCloseCmd() *cobra.Command {
	var (
		ac     string
		remove bool
	)
	cmd := &cobra.Command{
		Use:   "close <connection endpoint>...",
		Short: "Close connection endpoints of an AutomationComponent (CloseConnections)",
		Long: `Call CloseConnections on an AutomationComponent (OPC 10000-81 §6.2.5)
for the ConnectionEndpoints given as arguments (NodeIds) and print one
result per endpoint.

The server disables the DataSetReader and DataSetWriter of each endpoint.
With --remove it also removes what EstablishConnections created for the
endpoint, and the endpoint itself unless it is preconfigured.

Exit codes: 0 when every endpoint was closed; 7 when the call is
Uncertain and some endpoints were closed; 6 when none was, or for a Bad
status of the call; 9 when the server refuses the call to the user.

Examples:
  opcuactl fx close -e opc.tcp://localhost:4855 \
    --ac "nsu=urn:example:uafx:controller;i=1000" \
    "nsu=urn:example:uafx:controller;i=1300"
  opcuactl fx close -e opc.tcp://localhost:4855 --ac "ns=2;i=1000" \
    "ns=2;i=5001" "ns=2;i=5002" --remove`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, endpoints []string) error {
			return withClient(func(ctx context.Context, c *opcua.Client) error {
				b, acID, err := fxBuilder(ctx, c, ac)
				if err != nil {
					return err
				}
				ids, err := b.nodeIDs("endpoint", endpoints)
				if err != nil {
					return exitCode(exitUsage, err)
				}
				status, results, err := uafx.CloseConnections(ctx, c, acID, ids, remove)
				if err != nil {
					format.Errf("CloseConnections: %v", err)
					return exitCode(exitProtocol, err)
				}
				out := model.FxCloseResult{
					AutomationComponent: acID.String(), Remove: remove,
					Status: status.Symbol(), StatusCode: status.Uint32(),
					Endpoints: []model.FxClosedEndpoint{},
				}
				closed := 0
				for i, r := range results {
					if r == ua.StatusOK {
						closed++
					}
					out.Endpoints = append(out.Endpoints, model.FxClosedEndpoint{NodeID: ids[i].String(), Status: r.Symbol(), StatusCode: r.Uint32()})
				}
				if format.ParseFormat(outputFormat) == format.Table {
					rows := make([][]string, 0, len(out.Endpoints))
					for _, e := range out.Endpoints {
						rows = append(rows, []string{e.NodeID, e.Status})
					}
					if err := newWriter().WriteTable([]string{"ENDPOINT", "STATUS"}, rows); err != nil {
						return err
					}
				} else if err := newWriter().Write(out); err != nil {
					return err
				}
				return fxCloseExit(status, closed, len(results))
			})
		},
	}
	cmd.Flags().StringVar(&ac, "ac", "", "NodeId of the AutomationComponent (ns=<index>;... or nsu=<uri>;...)")
	cmd.Flags().BoolVar(&remove, "remove", false, "Also remove the endpoint and what EstablishConnections created for it")
	_ = cmd.MarkFlagRequired("ac")
	return cmd
}

// fxCloseExit is the exit of fx close for the status of the call and the
// number of endpoints closed of n.
func fxCloseExit(status ua.StatusCode, closed, n int) error {
	switch {
	case status == ua.StatusOK && closed == n:
		return nil
	case closed > 0:
		return exitCodef(exitPartial, "CloseConnections returned %s: %d of %d endpoints closed", status.Symbol(), closed, n)
	default:
		return exitCodef(exitProtocol, "CloseConnections returned %s: no endpoint closed", status.Symbol())
	}
}

// fxBuilder reads the NamespaceArray of the server and parses the NodeId of
// the AutomationComponent with it.
func fxBuilder(ctx context.Context, c *opcua.Client, ac string) (*fxRequestBuilder, *ua.NodeID, error) {
	nsa, err := c.NamespaceArray(ctx)
	if err != nil {
		return nil, nil, exitCode(exitProtocol, fmt.Errorf("reading the NamespaceArray: %w", err))
	}
	b := &fxRequestBuilder{namespaces: nsa}
	acNS := -1
	for i, u := range nsa {
		if u == fxacid.NamespaceURI {
			acNS = i
		}
	}
	if acNS < 0 || acNS > 0xffff {
		return nil, nil, exitCodef(exitProtocol, "the server has no UAFX AC namespace (%s): it is not a UAFX server", fxacid.NamespaceURI)
	}
	b.ac = uint16(acNS)
	id, err := b.nodeID("--ac", ac)
	if err != nil {
		return nil, nil, exitCodef(exitUsage, "%s", strings.TrimPrefix(err.Error(), errFxRequest.Error()+": "))
	}
	return b, id, nil
}

func symbols(codes []ua.StatusCode) []string {
	if len(codes) == 0 {
		return nil
	}
	out := make([]string, len(codes))
	for i, c := range codes {
		out[i] = c.Symbol()
	}
	return out
}

var (
	fxAssetResultNames = map[uafx.AssetVerificationResult]string{
		uafx.AssetVerificationNotSet: "NotSet", uafx.AssetVerificationMatch: "Match",
		uafx.AssetVerificationCompatible: "Compatible", uafx.AssetVerificationMismatch: "Mismatch",
	}
	fxEntityResultNames = map[uafx.FunctionalEntityVerificationResult]string{
		uafx.FunctionalEntityVerificationNotSet: "NotSet", uafx.FunctionalEntityVerificationMatch: "Match",
		uafx.FunctionalEntityVerificationMismatch: "Mismatch",
	}
)

func enumName[T ~int32](m map[T]string, v T) string {
	if s, ok := m[v]; ok {
		return s
	}
	return fmt.Sprintf("%d", int32(v))
}

// fxRefString names a PubSubConfigurationRefDataType: its mask bits and
// indexes.
func fxRefString(r *ua.PubSubConfigurationRefDataType) string {
	if r == nil {
		return ""
	}
	var bits []string
	for i := 0; i < 16; i++ {
		b := ua.PubSubConfigurationRefMask(1 << i)
		if r.ConfigurationMask&b != 0 {
			bits = append(bits, strings.TrimPrefix(b.String(), "PubSubConfigurationRefMask"))
		}
	}
	return fmt.Sprintf("%s connection=%d group=%d element=%d", strings.Join(bits, "|"), r.ConnectionIndex, r.GroupIndex, r.ElementIndex)
}

func fxConnectResult(ac *ua.NodeID, mask uafx.FxCommandMask, r *uafx.EstablishConnectionsResult) model.FxConnectResult {
	out := model.FxConnectResult{
		AutomationComponent:         ac.String(),
		Commands:                    mask.String(),
		Status:                      r.Status.Symbol(),
		StatusCode:                  r.Status.Uint32(),
		AssetVerifications:          []model.FxAssetVerificationResult{},
		Endpoints:                   []model.FxEndpointResult{},
		ReserveCommunicationIDs:     []model.FxReservedIDs{},
		CommunicationConfigurations: []model.FxCommunicationConfigResult{},
	}
	for _, a := range r.AssetVerificationResults {
		if a == nil {
			continue
		}
		out.AssetVerifications = append(out.AssetVerifications, model.FxAssetVerificationResult{
			Status:                   a.VerificationStatus.Symbol(),
			Result:                   enumName(fxAssetResultNames, a.VerificationResult),
			VariableErrors:           symbols(a.VerificationVariablesErrors),
			AdditionalVariableErrors: symbols(a.VerificationAdditionalVariablesErrors),
		})
	}
	for _, e := range r.ConnectionEndpointConfigurationResults {
		if e == nil {
			continue
		}
		m := model.FxEndpointResult{
			FunctionalEntityResult:    e.FunctionalEntityNodeResult.Symbol(),
			ConnectionEndpointResult:  e.ConnectionEndpointResult.Symbol(),
			VerificationResult:        enumName(fxEntityResultNames, e.VerificationResult),
			VerificationStatus:        e.VerificationStatus.Symbol(),
			VerificationErrors:        symbols(e.VerificationVariablesErrors),
			EstablishControlResults:   symbols(e.EstablishControlResult),
			ConfigurationDataResults:  symbols(e.ConfigurationDataResult),
			ReassignControlResults:    symbols(e.ReassignControlResult),
			CommunicationLinksResult:  e.CommunicationLinksResult.Symbol(),
			EnableCommunicationResult: e.EnableCommunicationResult.Symbol(),
		}
		if id := e.ConnectionEndpointID; id != nil && (id.Namespace() != 0 || id.IntID() != 0 || id.StringID() != "") {
			m.ConnectionEndpoint = id.String()
		}
		out.Endpoints = append(out.Endpoints, m)
	}
	for _, r := range r.ReserveCommunicationIDsResults {
		if r == nil {
			continue
		}
		out.ReserveCommunicationIDs = append(out.ReserveCommunicationIDs, model.FxReservedIDs{
			Result:                r.Result.Symbol(),
			DefaultPublisherID:    historyEventValue(r.DefaultPublisherID),
			WriterGroupIDs:        nonNilIDs(r.WriterGroupIDs),
			DataSetWriterIDs:      nonNilIDs(r.DataSetWriterIDs),
			TransportSpecificInfo: historyEventValue(r.TransportSpecificInfo),
		})
	}
	for _, c := range r.CommunicationConfigurationResults {
		if c == nil {
			continue
		}
		m := model.FxCommunicationConfigResult{
			Result: c.Result.Symbol(), ChangesApplied: c.ChangesApplied,
			ReferenceResults: symbols(c.ReferenceResults),
		}
		for _, v := range c.ConfigurationValues {
			if v == nil {
				continue
			}
			m.ConfigurationValues = append(m.ConfigurationValues, model.FxConfigurationValue{
				Element: fxRefString(v.ConfigurationElement), Name: v.Name, Identifier: historyEventValue(v.Identifier),
			})
		}
		for _, o := range c.ConfigurationObjects {
			if o != nil {
				m.ConfigurationObjects = append(m.ConfigurationObjects, o.String())
			}
		}
		out.CommunicationConfigurations = append(out.CommunicationConfigurations, m)
	}
	return out
}

func nonNilIDs(v []uint16) []uint16 {
	if v == nil {
		return []uint16{}
	}
	return v
}

// printFxConnectResult prints the result for people: the status, then each
// element with what is not Good.
func printFxConnectResult(r model.FxConnectResult) {
	p := func(f string, args ...any) { format.Printf(f+"\n", args...) }
	p("AutomationComponent: %s", r.AutomationComponent)
	p("Commands:            %s", r.Commands)
	p("Status:              %s", r.Status)
	for i, a := range r.AssetVerifications {
		p("Asset %d: %s %s%s", i, a.Status, a.Result, listSuffix("variables", a.VariableErrors)+listSuffix("additional", a.AdditionalVariableErrors))
	}
	for i, e := range r.Endpoints {
		id := e.ConnectionEndpoint
		if id == "" {
			id = "-"
		}
		p("Endpoint %d: %s", i, id)
		p("  FunctionalEntity:    %s", e.FunctionalEntityResult)
		p("  ConnectionEndpoint:  %s", e.ConnectionEndpointResult)
		p("  Verification:        %s %s%s", e.VerificationStatus, e.VerificationResult, listSuffix("variables", e.VerificationErrors))
		if len(e.EstablishControlResults)+len(e.ReassignControlResults) > 0 {
			p("  ControlGroups:       establish %s, reassign %s", strings.Join(e.EstablishControlResults, ","), strings.Join(e.ReassignControlResults, ","))
		}
		if len(e.ConfigurationDataResults) > 0 {
			p("  ConfigurationData:   %s", strings.Join(e.ConfigurationDataResults, ","))
		}
		p("  CommunicationLinks:  %s", e.CommunicationLinksResult)
		p("  EnableCommunication: %s", e.EnableCommunicationResult)
	}
	for i, ids := range r.ReserveCommunicationIDs {
		p("Reserved ids %d: %s publisher=%v writerGroups=%v dataSetWriters=%v transportSpecificInfo=%v",
			i, ids.Result, ids.DefaultPublisherID, ids.WriterGroupIDs, ids.DataSetWriterIDs, ids.TransportSpecificInfo)
	}
	for i, c := range r.CommunicationConfigurations {
		p("PubSub configuration %d: %s changesApplied=%t%s", i, c.Result, c.ChangesApplied, listSuffix("references", c.ReferenceResults))
		for _, v := range c.ConfigurationValues {
			p("  %s %q: %v", v.Element, v.Name, v.Identifier)
		}
	}
}

func listSuffix(name string, v []string) string {
	if len(v) == 0 {
		return ""
	}
	return fmt.Sprintf(" (%s: %s)", name, strings.Join(v, ","))
}
