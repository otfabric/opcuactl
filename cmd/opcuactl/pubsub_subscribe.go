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

	"github.com/otfabric/go-opcua/pubsub/transport"
	"github.com/otfabric/opcuactl/internal/format"
	"github.com/otfabric/opcuactl/internal/parse"
	"github.com/spf13/cobra"
)

func newPubSubSubscribeCmd() *cobra.Command {
	var (
		tf        pubsubTransportFlags
		sf        pubsubSecurityFlags
		mf        pubsubMetaFlags
		ff        pubsubFilterFlags
		encoding  string
		topic     string
		duration  time.Duration
		count     int
		jsonl     bool
		showMeta  bool
		showError bool
	)

	cmd := &cobra.Command{
		Use:   "subscribe",
		Short: "Listen for PubSub NetworkMessages and print their DataSets",
		Long: `Listen on a PubSub transport and print every DataSetMessage received.

--url selects the transport: opc.udp://host:port listens on a UDP port (a
multicast group address joins the group, --interface picks the network
interface), mqtt://host:port and mqtts://host:port subscribe to --topic on an
MQTT broker (MQTT wildcards allowed, default "#").

UADP and JSON messages are told apart automatically; --encoding forces one.
UADP carries no field names and, with the RawData field encoding, no types:
give the DataSetMetaData with --field, --metadata or --metadata-node. The
metadata publishers announce (ua-metadata messages on a broker, UADP
DataSetMetaData announcements) is learned and used for later messages;
--show-metadata prints it.

--publisher-id, --writer-group-id and --writer-id select the DataSets to show.

Secured UADP messages are verified and decrypted with the keys of
--security-group (pulled from the SKS at --endpoint) or --keys-file;
--message-security rejects messages with weaker security.

Output streams one line per DataSetMessage; --jsonl or --format json prints
newline-delimited JSON. Stop with Ctrl+C, --duration or --count. Messages that
cannot be decoded are reported on stderr.

Examples:
  opcuactl pubsub subscribe --url opc.udp://224.0.0.22:4840
  opcuactl pubsub subscribe --url opc.udp://224.0.0.22:4840 --field Counter:UInt32 --field Temp:Double
  opcuactl pubsub subscribe --url opc.udp://239.0.0.1:4840 --interface eth0 --publisher-id 2234 --writer-id 1
  opcuactl pubsub subscribe --url mqtt://broker:1883 --topic 'plant/line1/#' --jsonl
  opcuactl pubsub subscribe --url opc.udp://224.0.0.22:4840 --metadata-node "ns=1;i=50001" -e opc.tcp://plc:4840
  opcuactl pubsub subscribe --url opc.udp://224.0.0.22:4840 --security-group sg1 --message-security sign-and-encrypt \
      -e opc.tcp://sks:4840 --security-policy Basic256Sha256 --security-mode SignAndEncrypt \
      --auth username --username subscriber --password secret --cert client.pem --key client.key
  opcuactl pubsub subscribe --url opc.udp://224.0.0.22:4840 --count 10 --format json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			enc, err := parse.ParsePubSubEncoding(encoding, true)
			if err != nil {
				return exitCode(1, err)
			}
			minMode, err := parse.MessageSecurity(sf.mode)
			if err != nil {
				return exitCode(1, err)
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
				return exitCode(1, err)
			}

			conn, err := tf.open(ctx, enc)
			if err != nil {
				return err
			}
			defer func() { _ = conn.Close() }()

			msgs := make(chan *transport.Message, 1024)
			var dropped atomic.Int64
			cancelSub, err := conn.Subscribe(ctx, topic, tf.qos, func(m *transport.Message) {
				select {
				case msgs <- m:
				default:
					dropped.Add(1)
				}
			})
			if err != nil {
				return exitCodef(2, "subscribing to %s: %v", tf.url, err)
			}
			defer func() { _ = cancelSub() }()
			if verbose {
				pubsubInfo("listening on %s (topic %q, encoding %s)", tf.url, topic, enc)
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
				case m := <-msgs:
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
							pubsubInfo("%s", metaDataLine(mdm))
						}
					}
					if res.Status != nil && (showMeta || verbose) {
						if showMeta {
							emit(res.Status, statusLine(res.Status))
						} else {
							pubsubInfo("%s", statusLine(res.Status))
						}
					}
					for _, ds := range res.DataSets {
						emit(ds, dataSetLine(ds))
						received++
						if count > 0 && received >= count {
							return nil
						}
					}
					if n := dropped.Swap(0); n > 0 && verbose {
						pubsubInfo("%d message(s) dropped: output too slow", n)
					}
				}
			}
		},
	}

	fs := cmd.Flags()
	tf.register(fs)
	sf.register(cmd, "Minimum security of the messages (none|sign|sign-and-encrypt)")
	mf.register(cmd)
	ff.register(fs)
	fs.StringVar(&encoding, "encoding", "auto", "Message mapping (auto|uadp|json)")
	fs.StringVar(&topic, "topic", "#", "MQTT topic filter to subscribe to")
	fs.DurationVar(&duration, "duration", 0, "Stop after this duration (0 = run until Ctrl+C)")
	fs.IntVar(&count, "count", 0, "Stop after this many DataSetMessages (0 = unlimited)")
	fs.BoolVar(&jsonl, "jsonl", false, "Output newline-delimited JSON (JSONL)")
	fs.BoolVar(&showMeta, "show-metadata", false, "Also print DataSetMetaData announcements and status messages")
	fs.BoolVar(&showError, "show-errors", false, "Report every message that cannot be decoded (default: the first 10)")
	_ = cmd.RegisterFlagCompletionFunc("encoding", fixedCompletion(pubsubEncodingValues))
	return cmd
}
