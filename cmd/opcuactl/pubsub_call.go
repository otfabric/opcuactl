// SPDX-License-Identifier: MIT

package opcuactl

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/otfabric/go-opcua/pubsub"
	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/opcuactl/internal/format"
	"github.com/otfabric/opcuactl/internal/model"
	"github.com/otfabric/opcuactl/internal/parse"
	"github.com/spf13/cobra"
)

func newPubSubCallCmd() *cobra.Command {
	var (
		af              pubsubActionFlags
		requestorID     string
		targetID        uint16
		responseAddress string
		inputSpecs      []string
		outputSpecs     []string
		resend          time.Duration
	)

	cmd := &cobra.Command{
		Use:   "call",
		Short: "Call a PubSub Action of a Responder and print its result",
		Long: `Call a PubSub Action (Part 14 §6.2.11.2 in 1.05): send an ActionRequest to a
Responder and print the status and outputs of its ActionResponse.

The Action target is addressed by the Responder's --publisher-id, the
--writer-id of its Action DataSetWriter and --target-id. Every
--input Name:Type=Value adds an input, in the order of the request
DataSetMetaData of the Responder (types as for "pubsub publish"; the JSON
mapping sends the names). --output Name:Type describes the outputs (their
names; RawData needs the types); without it the outputs are shown in the
order received.

--url is the connection of the Requestor:
  - broker (mqtt://, mqtts://, amqp://, amqps://): the request goes to the
    request queue of the Responder's writer group, --request-address, by
    default <prefix>/<encoding>/action-request/<publisher-id>/<writer-group-name>
    (--topic-prefix, default "opcua"; --writer-group-name, default "Actions",
    as "pubsub respond" uses). The response comes on --response-address,
    by default the matching action-response topic.
  - UDP (opc.udp://): the Requestor listens on --url; the request goes to
    --request-address (the Responder's address; default --url, e.g. a shared
    multicast group) and the response comes back to --url.
    --resend-interval resends the request (and the acknowledgement) while
    nothing is heard, for lossy networks.

--timeout is the TimeoutHint of the request and how long the call waits for
the result. UADP messages are signed (--message-security sign) or signed and
encrypted (sign-and-encrypt) with the keys of --security-group (pulled from
the SKS at --endpoint) or --keys-file; the Responder answers with the
security of its writer group. The JSON mapping has no message security.

The result is printed as text, or with --format json|yaml. Exit codes: 0 Good
result, 2 no response within --timeout or a transport failure, 3 a Bad result
of the Action. --diagnostics prints the Action counters on stderr.

Examples:
  opcuactl pubsub call --url mqtt://broker:1883 --publisher-id 1 --writer-id 1 --target-id 1 \
      --input A:Int32=2 --input B:Int32=3 --output Sum:Int32
  opcuactl pubsub call --url mqtt://broker:1883 --encoding json --publisher-id responder-1 \
      --request-address opcua/json/action-request/responder-1/Actions --input Name:String=pump --format json
  opcuactl pubsub call --url opc.udp://127.0.0.1:4841 --request-address opc.udp://127.0.0.1:4840 \
      --input A:Int32=2 --input B:Int32=3 --resend-interval 200ms
  opcuactl pubsub call --url opc.udp://239.0.0.1:4840 --input A:Int32=2 \
      --message-security sign-and-encrypt --keys-file keys.json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			inputs, reqFields, err := parseActionValues(inputSpecs, "--input")
			if err != nil {
				return err
			}
			respFields, err := parseActionFields(outputSpecs, "--output")
			if err != nil {
				return err
			}
			if cmd.Flags().Changed("qos") {
				return exitCodef(1, "--qos: the requests are sent at least once; the Responder's writer group sets the delivery guarantee (pubsub respond --qos)")
			}
			if resend < 0 {
				return exitCodef(1, "--resend-interval must not be negative")
			}
			if connCfg.Timeout <= 0 {
				return exitCodef(1, "--timeout must be positive")
			}
			if requestorID == "" {
				requestorID = randomRequestorID()
			}
			rid, err := parse.PublisherID(requestorID)
			if err != nil {
				return exitCode(1, err)
			}
			s, closeKeys, err := af.setup(ctx)
			if err != nil {
				return err
			}
			defer closeKeys()
			ps, err := startActionRuntime(ctx, pubsub.NewConfiguration(nil, s.connection(rid)), af.tf.url, s.options())
			if err != nil {
				return err
			}
			defer func() { _ = ps.Close() }()
			diag := newActionDiagnostics(ps, "requestor", ctrActionRequestsSent, ctrActionResponsesReceived, ctrActionTimeouts)
			defer af.gf.print(diag)
			defer af.gf.start(diag)()

			target := pubsub.ActionTarget{
				Connection:      actionConnection,
				PublisherID:     s.responder,
				DataSetWriterID: af.writerID,
				ActionTargetID:  targetID,
				RequestAddress:  s.requestAddress,
				ResponseAddress: responseAddress,
				SecurityMode:    s.mode,
				SecurityGroupID: s.groupID,
				Timeout:         connCfg.Timeout,
				ResendInterval:  resend,
			}
			if len(reqFields) > 0 {
				target.RequestMetaData = pubsub.MetaData("Request", reqFields...)
			}
			if len(respFields) > 0 {
				target.ResponseMetaData = pubsub.MetaData("Response", respFields...)
			}
			if verbose {
				pubsubInfo("calling Action target %d of writer %d of publisher %s as %s on %s (request address %q)",
					targetID, af.writerID, s.responder, rid, af.tf.url, s.requestAddress)
			}
			start := time.Now()
			res, err := ps.CallAction(ctx, target, inputs)
			elapsed := time.Since(start)
			if err != nil {
				switch {
				case errors.Is(err, pubsub.ErrActionTimeout):
					return exitCodef(2, "no response from the Action target %d of writer %d of publisher %s within %v", targetID, af.writerID, s.responder, connCfg.Timeout)
				case ctx.Err() != nil:
					return exitCode(2, ctx.Err())
				}
				return exitCodef(2, "calling the Action: %v", err)
			}
			out := model.PubSubActionResult{
				PublisherID:     s.responder.String(),
				DataSetWriterID: af.writerID,
				ActionTargetID:  targetID,
				RequestID:       res.RequestID,
				Status:          res.Status.Symbol(),
				StatusCode:      res.Status.Uint32(),
				Duration:        elapsed.Round(time.Microsecond).String(),
				Outputs:         actionFields(res.Outputs, respFields),
			}
			if err := writeActionResult(out); err != nil {
				return err
			}
			if isBadStatus(res.Status) {
				return exitCode(3, fmt.Errorf("the Action target %d of writer %d of publisher %s answered %s", targetID, af.writerID, s.responder, res.Status.Symbol()))
			}
			return nil
		},
	}

	af.register(cmd, "PublisherId of the Responder (number = UInt16; byte:N, u32:N, u64:N, or a string)")
	fs := cmd.Flags()
	fs.StringVar(&requestorID, "requestor-id", "", "PublisherId of this Requestor (default: a random string)")
	fs.Uint16Var(&targetID, "target-id", 1, "ActionTargetId of the Action target")
	fs.StringVar(&responseAddress, "response-address", "", "Where the Responder sends the response (broker: default the action-response topic of the request queue; UDP: default --url)")
	fs.StringArrayVar(&inputSpecs, "input", nil, "Input Name:Type=Value (repeatable, in the order of the request DataSetMetaData)")
	fs.StringArrayVar(&outputSpecs, "output", nil, "Output Name:Type (repeatable, in the order of the response DataSetMetaData)")
	fs.DurationVar(&resend, "resend-interval", 0, "UDP: resend the request while no response arrives within this interval (0 = no resends)")
	return cmd
}

// isBadStatus reports a Bad StatusCode (severity bits 10).
func isBadStatus(s ua.StatusCode) bool { return uint32(s)&0xC0000000 == 0x80000000 }

// parseActionValues parses the fields with values Name:Type=Value of flag.
func parseActionValues(specs []string, flag string) ([]*ua.Variant, []pubsub.Field, error) {
	values := make([]*ua.Variant, len(specs))
	fields := make([]pubsub.Field, len(specs))
	for i, s := range specs {
		f, err := parse.PubSubFieldWithValue(s)
		if err != nil {
			return nil, nil, exitCodef(1, "%s: %v", flag, err)
		}
		v, err := ua.NewVariant(f.Value)
		if err != nil {
			return nil, nil, exitCodef(1, "%s %s: %v", flag, f.Name, err)
		}
		values[i], fields[i] = v, f.Field
	}
	return values, fields, nil
}

// parseActionFields parses Name:Type field descriptions.
func parseActionFields(specs []string, flag string) ([]pubsub.Field, error) {
	fields := make([]pubsub.Field, len(specs))
	for i, s := range specs {
		f, err := parse.PubSubField(s)
		if err != nil {
			return nil, exitCodef(1, "%s: %v", flag, err)
		}
		fields[i] = f
	}
	return fields, nil
}

// writeActionResult prints the result of a call.
func writeActionResult(r model.PubSubActionResult) error {
	if format.ParseFormat(outputFormat) != format.Table {
		return newWriter().Write(r)
	}
	if quiet {
		for _, f := range r.Outputs {
			fmt.Println(format.FormatValue(f.Value))
		}
		return nil
	}
	fmt.Printf("pub=%s writer=%d target=%d request=%d status=%s duration=%s\n",
		r.PublisherID, r.DataSetWriterID, r.ActionTargetID, r.RequestID, r.Status, r.Duration)
	for _, f := range r.Outputs {
		name := f.Name
		if name == "" {
			name = fmt.Sprintf("#%d", f.Index)
		}
		fmt.Printf("  %s = %s (%s)\n", name, format.FormatValue(f.Value), f.Type)
	}
	return nil
}
