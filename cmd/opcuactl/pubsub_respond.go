// SPDX-License-Identifier: MIT

package opcuactl

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/otfabric/go-opcua/pubsub"
	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/opcuactl/internal/format"
	"github.com/otfabric/opcuactl/internal/model"
	"github.com/otfabric/opcuactl/internal/parse"
	"github.com/spf13/cobra"
)

// defaultDatagramActionInterval is the PublishingInterval of the writer
// group of a Responder on UDP or Ethernet: how often it repeats a response
// until the Requestor acknowledges it.
const defaultDatagramActionInterval = 100 * time.Millisecond

func newPubSubRespondCmd() *cobra.Command {
	var (
		af            pubsubActionFlags
		groupID       uint16
		writerName    string
		dataSetName   string
		targetSpecs   []string
		inputSpecs    []string
		outputSpecs   []string
		statusStr     string
		delay         time.Duration
		interval      time.Duration
		count         int
		duration      time.Duration
		jsonl         bool
		maxConcurrent int
		rate          float64
		burst         int
		maxTimeout    time.Duration
	)

	cmd := &cobra.Command{
		Use:   "respond",
		Short: "Answer PubSub Action requests (a test Responder)",
		Long: `Run a PubSub Action Responder (Part 14 §6.2.11.2 in 1.05) for testing
Requestors: answer every ActionRequest and print it with its result.

The Responder has PublisherId --publisher-id, a writer group
--writer-group-name / --writer-group-id and one Action DataSetWriter
--writer-id of a PublishedAction --dataset-name whose targets are --target
ID[:Name] (repeatable, default 1). --input Name:Type describes the request
DataSetMetaData: requests with missing or surplus inputs, or inputs of
another type, are answered with Bad_ArgumentsMissing, Bad_TooManyArguments
or Bad_TypeMismatch; without --input any inputs are accepted.

Every target answers with the fixed outputs of --output Name:Type=Value
(repeatable), or, without --output, echoes its inputs. --status answers with
another StatusCode (e.g. BadInternalError: no outputs are sent) and --delay
waits before answering (a delay beyond the TimeoutHint of the request makes
the Requestor time out).

--url is the Responder's connection:
  - broker (mqtt://, mqtts://, amqp://, amqps://): requests arrive on the
    queue of the writer group, --request-address, by default
    <prefix>/<encoding>/action-request/<publisher-id>/<writer-group-name>
    (--topic-prefix, default "opcua"); responses go to the ResponseAddress
    of each request (the matching action-response topic by default).
  - UDP (opc.udp://): requests arrive on --url, responses go to the
    ResponseAddress of the request (the Requestor's host or a multicast
    group) or to --url. The response is repeated every --interval until the
    Requestor acknowledges it.

The limits of the Responder: --max-concurrent exchanges (default 64; more are
answered with Bad_TooManyOperations), --rate new requests per second with
bursts of --burst (default 50, 50; more are dropped) and --max-timeout, the
cap of the TimeoutHint of a request (default 60s).

UADP requests must carry the security of the writer group: --message-security
sign or sign-and-encrypt with the keys of --security-group (SKS at
--endpoint) or --keys-file. Requests with weaker security are rejected
without a response.

Every request served is printed as a line, or as JSON Lines with --jsonl or
--format json. Stop with Ctrl+C, --duration or --count. --diagnostics prints
the Action counters on stderr when done (--diagnostics-interval also while
running).

Examples:
  opcuactl pubsub respond --url mqtt://broker:1883 --input A:Int32 --input B:Int32
  opcuactl pubsub respond --url mqtt://broker:1883 --encoding json --publisher-id responder-1 \
      --target 1:Start --target 2:Stop --output Result:String=ok
  opcuactl pubsub respond --url opc.udp://127.0.0.1:4840 --input A:Int32 --output Sum:Int32=5
  opcuactl pubsub respond --url opc.udp://127.0.0.1:4840 --status BadInternalError --count 1
  opcuactl pubsub respond --url opc.udp://239.0.0.1:4840 --message-security sign-and-encrypt \
      --keys-file keys.json --rate 10 --burst 5 --max-concurrent 4 --diagnostics`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			reqFields, err := parseActionFields(inputSpecs, "--input")
			if err != nil {
				return err
			}
			var fixed []*ua.Variant
			var respFields []pubsub.Field
			if len(outputSpecs) > 0 {
				if fixed, respFields, err = parseActionValues(outputSpecs, "--output"); err != nil {
					return err
				}
			} else {
				// Echo: the outputs are the inputs.
				respFields = reqFields
			}
			status, err := parse.StatusCode(statusStr)
			if err != nil {
				return exitCodef(exitUsage, "--status: %v", err)
			}
			switch {
			case delay < 0, interval < 0, duration < 0, maxTimeout < 0:
				return exitCodef(exitUsage, "--delay, --interval, --duration and --max-timeout must not be negative")
			case count < 0, maxConcurrent < 0, burst < 0, rate < 0:
				return exitCodef(exitUsage, "--count, --max-concurrent, --rate and --burst must not be negative")
			case groupID == 0:
				return exitCodef(exitUsage, "--writer-group-id must not be 0")
			}
			if len(targetSpecs) == 0 {
				targetSpecs = []string{"1"}
			}
			targets := make([]*ua.ActionTargetDataType, len(targetSpecs))
			seen := map[uint16]bool{}
			for i, spec := range targetSpecs {
				id, name, err := parse.ActionTarget(spec)
				if err != nil {
					return exitCodef(exitUsage, "--target: %v", err)
				}
				if seen[id] {
					return exitCodef(exitUsage, "--target: ActionTargetId %d given twice", id)
				}
				seen[id] = true
				targets[i] = pubsub.ActionTargetOf(id, name)
			}

			s, closeKeys, err := af.setup(ctx)
			if err != nil {
				return err
			}
			defer closeKeys()
			if !s.broker && af.requestAddress != "" {
				return exitCodef(exitUsage, "--request-address is the request queue on a broker; over UDP the requests arrive on --url")
			}
			if !cmd.Flags().Changed("interval") && !s.broker {
				interval = defaultDatagramActionInterval
			}

			// The configuration: the PublishedAction and its writer.
			pds := pubsub.PublishedActions(dataSetName, reqFields, respFields, targets...)
			writer := pubsub.DataSetWriter(writerName, af.writerID, dataSetName)
			var group *ua.WriterGroupDataType
			if s.enc == parse.EncodingJSON {
				group = pubsub.JSONWriterGroup(af.groupName, groupID, interval, s.requestAddress, writer)
			} else {
				group = pubsub.WriterGroup(af.groupName, groupID, interval, writer)
				if s.broker {
					group.TransportSettings = ua.NewExtensionObject(&ua.BrokerWriterGroupTransportDataType{
						QueueName: s.requestAddress, RequestedDeliveryGuarantee: ua.BrokerTransportQoSAtLeastOnce})
				}
			}
			if s.broker && cmd.Flags().Changed("qos") {
				group.TransportSettings.Value.(*ua.BrokerWriterGroupTransportDataType).RequestedDeliveryGuarantee = brokerQoS(af.tf.qos)
			}
			if s.mode > ua.MessageSecurityModeNone {
				pubsub.Secure(group, s.mode, s.groupID)
			}
			cfg := pubsub.NewConfiguration([]*ua.PublishedDataSetDataType{pds},
				pubsub.AttachWriterGroups(s.connection(s.responder), group))

			// The handler answers every target and prints the request.
			var (
				mu     sync.Mutex
				served int
				done   = make(chan struct{})
				// sentBefore is the number of responses sent when the
				// last request (--count) was served.
				sentBefore uint64
				running    atomic.Pointer[pubsub.PubSub]
			)
			useJSON := jsonl || format.ParseFormat(outputFormat) != format.Table
			handler := func(ctx context.Context, in []*ua.Variant) ([]*ua.Variant, ua.StatusCode) {
				inv, _ := pubsub.ActionInvocationFrom(ctx)
				st := status
				if delay > 0 {
					t := time.NewTimer(delay)
					select {
					case <-t.C:
					case <-ctx.Done():
						t.Stop()
						st = ua.StatusBadTimeout
					}
				}
				out := fixed
				if out == nil {
					out = in
				}
				if isBadStatus(st) {
					out = nil
				}
				r := model.PubSubActionRequest{
					Timestamp:       model.FormatTimestamp(time.Now().UTC()),
					DataSetWriterID: inv.DataSetWriterID,
					ActionTargetID:  inv.ActionTargetID,
					RequestID:       inv.RequestID,
					Inputs:          actionFields(in, reqFields),
					Status:          st.Symbol(),
					StatusCode:      st.Uint32(),
					Outputs:         actionFields(out, respFields),
				}
				if inv.RequestorID != nil {
					r.RequestorID = inv.RequestorID.String()
				}
				mu.Lock()
				defer mu.Unlock()
				if count > 0 && served >= count {
					// Beyond --count while stopping: not shown.
					return out, st
				}
				served++
				if useJSON {
					b, _ := json.Marshal(r)
					fmt.Println(string(b))
				} else if !quiet {
					fmt.Println(actionRequestLine(r))
				}
				if count > 0 && served == count {
					if ps := running.Load(); ps != nil {
						sentBefore = responsesSent(ps)
					}
					close(done)
				}
				return out, st
			}
			opts := append(s.options(), pubsub.WithActionOptions(pubsub.ActionOptions{
				MaxConcurrent: maxConcurrent, Rate: rate, Burst: burst, MaxTimeout: maxTimeout,
			}))
			for _, t := range targets {
				opts = append(opts, pubsub.WithActionHandler(dataSetName, t.ActionTargetID, handler))
			}

			ps, err := startActionRuntime(ctx, cfg, af.tf.url, opts)
			if err != nil {
				return err
			}
			defer func() { _ = ps.Close() }()
			running.Store(ps)
			diag := newActionDiagnostics(ps, "responder", ctrActionRequestsReceived, ctrActionRequestsRejected, ctrActionResponsesSent)
			defer af.gf.print(diag)
			defer af.gf.start(diag)()
			if verbose {
				where := af.tf.url
				if s.broker {
					where += " queue " + s.requestAddress
				}
				format.Infof("responding as publisher %s, writer %d, targets %s on %s", s.responder, af.writerID, strings.Join(targetSpecs, ","), where)
			}

			var deadline <-chan time.Time
			if duration > 0 {
				timer := time.NewTimer(duration)
				defer timer.Stop()
				deadline = timer.C
			}
			select {
			case <-ctx.Done():
			case <-deadline:
			case <-done:
				// Let the runtime send the last response.
				mu.Lock()
				before := sentBefore
				mu.Unlock()
				waitResponseSent(ps, before)
			}
			return nil
		},
	}

	af.register(cmd, "PublisherId of this Responder (number = UInt16; byte:N, u32:N, u64:N, or a string)")
	fs := cmd.Flags()
	fs.Uint16Var(&groupID, "writer-group-id", 1, "WriterGroupId of the writer group")
	fs.StringVar(&writerName, "writer-name", "Actions", "Name of the Action DataSetWriter")
	fs.StringVar(&dataSetName, "dataset-name", "Action", "Name of the PublishedAction")
	fs.StringArrayVar(&targetSpecs, "target", nil, "Action target ID[:Name] (repeatable; default 1)")
	fs.StringArrayVar(&inputSpecs, "input", nil, "Request field Name:Type (repeatable; without: any inputs are accepted)")
	fs.StringArrayVar(&outputSpecs, "output", nil, "Response field Name:Type=Value (repeatable; without: echo the inputs)")
	fs.StringVar(&statusStr, "status", "Good", "StatusCode of the responses (name or number; a Bad status sends no outputs)")
	fs.DurationVar(&delay, "delay", 0, "Wait this long before answering a request")
	fs.DurationVar(&interval, "interval", 0, "PublishingInterval of the writer group: how often a datagram response is repeated until acknowledged (default 100ms on UDP, 0 on a broker)")
	fs.IntVar(&count, "count", 0, "Stop after serving this many requests (0 = until Ctrl+C or --duration)")
	fs.DurationVar(&duration, "duration", 0, "Stop after this duration (0 = no limit)")
	fs.BoolVar(&jsonl, "jsonl", false, "Output newline-delimited JSON (JSONL)")
	fs.IntVar(&maxConcurrent, "max-concurrent", 0, "Exchanges kept at once; more are answered with Bad_TooManyOperations (0 = 64)")
	fs.Float64Var(&rate, "rate", 0, "New requests accepted per second; more are dropped (0 = 50)")
	fs.IntVar(&burst, "burst", 0, "Burst of new requests above --rate (0 = 50)")
	fs.DurationVar(&maxTimeout, "max-timeout", 0, "Cap of the TimeoutHint of a request (0 = 60s)")
	return cmd
}

// brokerQoS maps --qos to the delivery guarantee of a writer group.
func brokerQoS(qos uint8) ua.BrokerTransportQoS {
	switch qos {
	case 0:
		return ua.BrokerTransportQoSAtMostOnce
	case 2:
		return ua.BrokerTransportQoSExactlyOnce
	}
	return ua.BrokerTransportQoSAtLeastOnce
}

// waitResponseSent waits briefly until the runtime sent the response to the
// last request (the handler returns before the response goes out).
func waitResponseSent(ps *pubsub.PubSub, before uint64) {
	deadline := time.Now().Add(min(connCfg.Timeout, 2*time.Second))
	for responsesSent(ps) <= before && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
}

// responsesSent is the ActionResponsesSent counter of the runtime.
func responsesSent(ps *pubsub.PubSub) uint64 {
	st, _ := ps.Status(pubsub.Root)
	return st.Counters.ActionResponsesSent
}

// actionRequestLine is the table form of a request served.
func actionRequestLine(r model.PubSubActionRequest) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s  ", r.Timestamp)
	if r.RequestorID != "" {
		fmt.Fprintf(&b, "requestor=%s ", r.RequestorID)
	}
	fmt.Fprintf(&b, "writer=%d target=%d request=%d ", r.DataSetWriterID, r.ActionTargetID, r.RequestID)
	b.WriteString("inputs:")
	b.WriteString(fieldsString(r.Inputs))
	fmt.Fprintf(&b, " -> %s", r.Status)
	if len(r.Outputs) > 0 {
		b.WriteString(" outputs:")
		b.WriteString(fieldsString(r.Outputs))
	}
	return b.String()
}
