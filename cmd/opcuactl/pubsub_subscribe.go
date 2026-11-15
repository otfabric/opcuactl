// SPDX-License-Identifier: MIT

package opcuactl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/otfabric/go-opcua/pubsub"
	"github.com/otfabric/go-opcua/pubsub/message"
	"github.com/otfabric/go-opcua/pubsub/transport"
	"github.com/otfabric/go-opcua/pubsub/uadp"
	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/opcuactl/internal/format"
	"github.com/otfabric/opcuactl/internal/parse"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// pubsubChunkFlags bound the reassembly of chunked UADP NetworkMessages.
type pubsubChunkFlags struct {
	maxSize         int
	maxPending      int
	maxPendingBytes int
	timeout         time.Duration
}

func (f *pubsubChunkFlags) register(fs *pflag.FlagSet) {
	fs.IntVar(&f.maxSize, "chunk-max-message-size", 0, "Largest chunked message reassembled, in bytes (0 = 2 MiB)")
	fs.IntVar(&f.maxPending, "chunk-max-pending", 0, "Incomplete chunked messages kept (0 = 512)")
	fs.IntVar(&f.maxPendingBytes, "chunk-max-pending-bytes", 0, "Bytes of incomplete chunked messages kept (0 = 32 MiB)")
	fs.DurationVar(&f.timeout, "chunk-timeout", 0, "How long an incomplete chunked message waits for its chunks (0 = 5s)")
}

// options returns the reassembly limits; zero fields keep go-opcua's
// defaults.
func (f *pubsubChunkFlags) options() (*uadp.ReassemblerOptions, error) {
	if f.maxSize < 0 || f.maxPending < 0 || f.maxPendingBytes < 0 || f.timeout < 0 {
		return nil, exitCodef(exitUsage, "the --chunk-* limits must not be negative")
	}
	return &uadp.ReassemblerOptions{
		MaxMessageSize:     f.maxSize,
		MaxPendingMessages: f.maxPending,
		MaxPendingBytes:    f.maxPendingBytes,
		Timeout:            f.timeout,
	}, nil
}

func newPubSubSubscribeCmd() *cobra.Command {
	var (
		tf        pubsubTransportFlags
		sf        pubsubSecurityFlags
		mf        pubsubMetaFlags
		ff        pubsubFilterFlags
		cf        pubsubChunkFlags
		df        pubsubDiscoveryFlags
		gf        pubsubDiagnosticsFlags
		encoding  string
		topic     string
		duration  time.Duration
		count     int
		jsonl     bool
		showMeta  bool
		showError bool
		probe     bool
		maxMeta   int
	)

	cmd := &cobra.Command{
		Use:   "subscribe",
		Short: "Listen for PubSub NetworkMessages and print their DataSets",
		Long: `Listen on a PubSub transport and print every DataSetMessage received.

--url selects the transport: opc.udp://host:port listens on a UDP port (a
multicast group address joins the group, --interface picks the network
interface), opc.eth://MAC[:VID[.PCP]] receives raw Ethernet frames on
--interface (root or CAP_NET_RAW on Linux, BPF access on macOS / FreeBSD),
mqtt://host:port and mqtts://host:port subscribe to --topic on an MQTT broker
(MQTT wildcards allowed, default "#"; --mqtt-version 5.0 for MQTT 5.0),
amqp://host:port and amqps://host:port receive from the AMQP 1.0 address
--topic (RabbitMQ 4: /queues/<queue>; ActiveMQ Artemis: the address, with
--transport-property source-capabilities=queue for anycast).

UADP and JSON messages are told apart by the content type the transport
carries (MQTT 5.0, AMQP) or else by looking at them; --encoding forces one.
The content type, UAMessageType and user properties of MQTT 5.0 and AMQP
messages are shown with each DataSet.

UADP carries no field names and, with the RawData field encoding, no types:
give the DataSetMetaData with --field, --metadata or --metadata-node. The
metadata publishers announce (ua-metadata messages on a broker, UADP
DataSetMetaData announcements) is learned and used for later messages;
--show-metadata prints it, and the other discovery announcements. The
DataSetMetaData of at most --max-learned-metadata writers (default 1024) is
kept, the one used least recently makes room, and one with more than 4096
fields is reported on stderr and not kept. --probe asks publishers for the
DataSetMetaData of the writers it sees without one (UADP discovery probes on
a multicast --url or on --discovery-url), of at most 1024 writers at a time,
each while its messages arrive.

Chunked UADP NetworkMessages are reassembled within the --chunk-* limits.

--publisher-id, --writer-group-id and --writer-id select the DataSets to show.

Secured UADP messages are verified and decrypted with the keys of
--security-group (pulled from the SKS at --endpoint) or --keys-file;
--message-security rejects messages with weaker security. With keys it is
sign unless given: an unsigned UADP message is reported on stderr and not
printed; --message-security none accepts it. Without keys, and for JSON
messages, which have no message security, anyone who can reach --url can send
what is printed.

Output streams one line per DataSetMessage; --jsonl or --format json prints
newline-delimited JSON. Stop with Ctrl+C, --duration or --count. Messages that
cannot be decoded are reported on stderr. --diagnostics prints the counters
of what was received on stderr when done (--diagnostics-interval also while
running).

Examples:
  opcuactl pubsub subscribe --url opc.udp://224.0.0.22:4840
  opcuactl pubsub subscribe --url opc.udp://224.0.0.22:4840 --field Counter:UInt32 --field Temp:Double
  opcuactl pubsub subscribe --url opc.udp://239.0.0.1:4840 --interface eth0 --publisher-id 2234 --writer-id 1
  opcuactl pubsub subscribe --url opc.udp://224.0.0.22:4840 --probe --show-metadata
  sudo opcuactl pubsub subscribe --url opc.eth://01-00-5E-00-00-01 --interface eth0
  opcuactl pubsub subscribe --url mqtt://broker:1883 --topic 'plant/line1/#' --jsonl
  opcuactl pubsub subscribe --url mqtt://broker:1883 --mqtt-version 5.0 --qos 2 --topic 'plant/#'
  OPCUACTL_BROKER_PASSWORD="$(cat broker.txt)" opcuactl pubsub subscribe --url amqp://rabbitmq:5672 --topic /queues/opcua --broker-username opcua
  opcuactl pubsub subscribe --url amqp://artemis:5672 --topic opcua.data --transport-property source-capabilities=queue
  opcuactl pubsub subscribe --url opc.udp://224.0.0.22:4840 --metadata-node "ns=1;i=50001" -e opc.tcp://plc:4840
  opcuactl pubsub subscribe --url opc.udp://224.0.0.22:4840 --security-group sg1 --message-security sign-and-encrypt \
      -e opc.tcp://sks:4840 --security-policy Basic256Sha256 --security-mode SignAndEncrypt --trusted-cert sks.der \
      --auth username --username subscriber --password-file subscriber.txt --cert client.pem --key client.key
  opcuactl pubsub subscribe --url opc.udp://224.0.0.22:4840 --duration 1m --diagnostics --diagnostics-level advanced
  opcuactl pubsub subscribe --url opc.udp://224.0.0.22:4840 --count 10 --format json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			enc, err := parse.ParsePubSubEncoding(encoding, true)
			if err != nil {
				return exitCode(exitUsage, err)
			}
			minMode, err := parse.MessageSecurity(sf.mode)
			if err != nil {
				return exitCode(exitUsage, err)
			}
			// With the keys of a SecurityGroup the minimum is Sign, unless
			// --message-security says otherwise.
			if !cmd.Flags().Changed("message-security") && (sf.group != "" || sf.keysFile != "") {
				minMode = ua.MessageSecurityModeSign
			}
			reasm, err := cf.options()
			if err != nil {
				return err
			}
			if maxMeta < 1 {
				return exitCodef(exitUsage, "--max-learned-metadata %d: at least 1", maxMeta)
			}
			if err := gf.validate(); err != nil {
				return err
			}
			if _, err := tf.config(enc); err != nil {
				return err
			}
			if probe {
				if err := df.validate(tf.url, enc, "--probe"); err != nil {
					return err
				}
			}

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			md, err := mf.load(ctx)
			if err != nil {
				return err
			}
			keys, closeKeys, err := sf.keySource(ctx)
			if err != nil {
				return err
			}
			defer closeKeys()

			dec, err := newPubSubDecoder(enc, ff, md, keys, minMode)
			if err != nil {
				return exitCode(exitUsage, err)
			}
			dec.reasm = uadp.NewReassembler(reasm)
			dec.learned = newLearnedMetaData(maxMeta)
			defer func() {
				if n := dec.learned.evictions(); n > 0 {
					format.Infof("%d learned DataSetMetaData dropped to make room for others (--max-learned-metadata %d)", n, maxMeta)
				}
			}()
			dec.diag = newPubSubDiagnostics("reader-group",
				[]pubsub.DiagnosticsCounterID{pubsub.CounterReceivedNetworkMessages, pubsub.CounterReceivedInvalidNetworkMessages, pubsub.CounterDecryptionErrors},
				ctrChunksReceived, ctrChunkedMessagesReceived, ctrChunksDiscarded, ctrDiscoveryRequestsSent, ctrDiscoveryResponsesReceived)

			conn, err := tf.open(ctx, enc)
			if err != nil {
				return err
			}
			defer func() { _ = conn.Close() }()
			defer gf.print(dec.diag)
			defer gf.start(dec.diag)()

			queue := newPubSubQueue(1024)
			defer queue.final()
			deliver := queue.deliver
			cancelSub, err := tf.subscribe(ctx, conn, topic, deliver)
			if err != nil {
				return err
			}
			defer func() { _ = cancelSub() }()

			if probe {
				dc, closeDC, err := df.open(ctx, &tf, conn)
				if err != nil {
					return err
				}
				defer closeDC()
				if dc != conn && sharedAddress(df.url) {
					// Announcements other than DataSetMetaData arrive on a
					// shared discovery address; a unicast one belongs to
					// the publisher.
					cancelDisc, err := tf.subscribe(ctx, dc, "", deliver)
					if err != nil {
						return err
					}
					defer func() { _ = cancelDisc() }()
				}
				prober := newPubSubProber(dc, df.parsed, dec.diag, func(p message.PublisherID, writerID uint16) bool {
					return dec.lookup(p, 0, writerID) != nil
				})
				dec.missing = prober.want
				if dec.publisher != nil && ff.writerID != 0 && md == nil {
					prober.want(*dec.publisher, ff.writerID)
				}
				probeCtx, cancelProbe := context.WithCancel(ctx)
				done := make(chan struct{})
				go func() {
					defer close(done)
					prober.run(probeCtx)
				}()
				defer func() {
					cancelProbe()
					<-done
				}()
			}
			if verbose {
				format.Infof("listening on %s (topic %q, encoding %s)", tf.url, topic, enc)
			}

			var deadline <-chan time.Time
			if duration > 0 {
				timer := time.NewTimer(duration)
				defer timer.Stop()
				deadline = timer.C
			}

			useJSON := jsonl || format.ParseFormat(outputFormat) != format.Table
			emit := func(v interface{}, line string) {
				if useJSON {
					b, _ := json.Marshal(v)
					fmt.Println(string(b))
				} else {
					fmt.Println(line)
				}
			}

			received, failures := 0, 0
			for {
				select {
				case <-ctx.Done():
					return nil
				case <-deadline:
					return nil
				case m := <-queue.ch:
					res, err := dec.decode(m.Payload, m)
					if errors.Is(err, errFiltered) {
						continue
					}
					if err != nil {
						failures++
						if showError || verbose || failures <= 10 {
							src := m.Topic
							if m.Source != nil {
								src = m.Source.String()
							}
							format.Errf("decoding a %d-byte message from %s: %v", len(m.Payload), src, err)
						}
						continue
					}
					for _, mdm := range res.MetaData {
						if showMeta {
							emit(mdm, metaDataLine(mdm))
						} else if verbose {
							format.Infof("%s", metaDataLine(mdm))
						}
					}
					for _, a := range res.Announcements {
						if showMeta {
							emit(a, announcementLine(a))
						} else if verbose {
							format.Infof("%s", announcementLine(a))
						}
					}
					if res.Status != nil && (showMeta || verbose) {
						if showMeta {
							emit(res.Status, statusLine(res.Status))
						} else {
							format.Infof("%s", statusLine(res.Status))
						}
					}
					for _, ds := range res.DataSets {
						emit(ds, dataSetLine(ds))
						received++
						if count > 0 && received >= count {
							return nil
						}
					}
					queue.report(time.Now())
				}
			}
		},
	}

	fs := cmd.Flags()
	tf.register(fs)
	sf.register(cmd, "Minimum security of the UADP messages (none|sign|sign-and-encrypt); sign when --security-group or --keys-file is given")
	mf.register(cmd)
	ff.register(fs)
	cf.register(fs)
	df.register(fs)
	gf.register(fs)
	fs.StringVar(&encoding, "encoding", "auto", "Message mapping (auto|uadp|json)")
	fs.StringVar(&topic, "topic", "#", "Broker queue to receive from: MQTT topic filter or AMQP address")
	fs.DurationVar(&duration, "duration", 0, "Stop after this duration (0 = run until Ctrl+C)")
	fs.IntVar(&count, "count", 0, "Stop after this many DataSetMessages (0 = unlimited)")
	fs.BoolVar(&jsonl, "jsonl", false, "Output newline-delimited JSON (JSONL)")
	fs.BoolVar(&showMeta, "show-metadata", false, "Also print DataSetMetaData and other discovery announcements and status messages")
	fs.BoolVar(&showError, "show-errors", false, "Report every message that cannot be decoded (default: the first 10)")
	fs.BoolVar(&probe, "probe", false, "Probe for the DataSetMetaData of writers seen without one (UADP discovery)")
	fs.IntVar(&maxMeta, "max-learned-metadata", defaultMaxLearnedMetaData, "Writers whose DataSetMetaData learned from the network is kept; the one used least recently makes room")
	_ = cmd.RegisterFlagCompletionFunc("encoding", fixedCompletion(pubsubEncodingValues))
	_ = cmd.RegisterFlagCompletionFunc("mqtt-version", fixedCompletion(pubsubMQTTVersionValues))
	_ = cmd.RegisterFlagCompletionFunc("discovery-layout", fixedCompletion(pubsubDiscoveryLayoutValues))
	_ = cmd.RegisterFlagCompletionFunc("diagnostics-level", fixedCompletion(pubsubDiagnosticsLevelValues))
	return cmd
}

// dropReportInterval is the least time between two lines that report
// dropped messages; the first drop is reported at once.
const dropReportInterval = 10 * time.Second

// pubsubQueue is the bounded queue between the transport and the output of
// pubsub subscribe. A message that finds it full is dropped and counted; the
// count goes to stderr, with or without --verbose.
type pubsubQueue struct {
	ch      chan *transport.Message
	dropped atomic.Int64 // since the last report
	total   int64        // reported so far
	last    time.Time    // of the last report
}

func newPubSubQueue(size int) *pubsubQueue {
	return &pubsubQueue{ch: make(chan *transport.Message, size)}
}

// deliver queues m, or drops it when the queue is full. It does not block.
func (q *pubsubQueue) deliver(m *transport.Message) {
	select {
	case q.ch <- m:
	default:
		q.dropped.Add(1)
	}
}

// report prints the number of messages dropped since the last report, at
// most once per dropReportInterval.
func (q *pubsubQueue) report(now time.Time) {
	if q.dropped.Load() == 0 || (!q.last.IsZero() && now.Sub(q.last) < dropReportInterval) {
		return
	}
	n := q.dropped.Swap(0)
	q.total += n
	q.last = now
	format.Infof("%d message(s) dropped: output too slow", n)
}

// final prints the number of messages dropped in all, if any.
func (q *pubsubQueue) final() {
	if total := q.total + q.dropped.Swap(0); total > 0 {
		format.Infof("%d message(s) dropped in all: output too slow", total)
	}
}
