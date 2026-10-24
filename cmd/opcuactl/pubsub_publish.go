// SPDX-License-Identifier: MIT

package opcuactl

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/otfabric/go-opcua/pubsub"
	"github.com/otfabric/go-opcua/pubsub/jsonmsg"
	"github.com/otfabric/go-opcua/pubsub/message"
	"github.com/otfabric/go-opcua/pubsub/security"
	"github.com/otfabric/go-opcua/pubsub/transport"
	"github.com/otfabric/go-opcua/pubsub/uadp"
	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/opcuactl/internal/format"
	"github.com/otfabric/opcuactl/internal/parse"
	"github.com/spf13/cobra"
)

// pubsubPublisher builds and sends the NetworkMessages of `pubsub publish`.
type pubsubPublisher struct {
	enc          parse.PubSubEncoding
	publisher    message.PublisherID
	groupID      uint16
	writerID     uint16
	groupName    string
	writerName   string
	fields       []parse.PubSubFieldValue
	fieldMask    ua.DataSetFieldContentMask
	md           *ua.DataSetMetaDataType
	mode         ua.MessageSecurityMode
	keys         security.KeySource
	nonce        uint32
	numericPubID bool
}

func (p *pubsubPublisher) lookup(message.PublisherID, uint16, uint16) *ua.DataSetMetaDataType {
	return p.md
}

func fieldEncodingOf(mask ua.DataSetFieldContentMask) message.FieldEncoding {
	switch {
	case mask&ua.DataSetFieldContentMaskRawData != 0:
		return message.FieldEncodingRawData
	case mask != 0:
		return message.FieldEncodingDataValue
	}
	return message.FieldEncodingVariant
}

// dataMessage encodes the DataSet as message number seq.
func (p *pubsubPublisher) dataMessage(seq uint32, now time.Time) ([]byte, error) {
	fields := make([]*message.Field, len(p.fields))
	for i, f := range p.fields {
		v, err := ua.NewVariant(f.Value)
		if err != nil {
			return nil, fmt.Errorf("field %s: %w", f.Name, err)
		}
		dv := &ua.DataValue{EncodingMask: ua.DataValueValue, Value: v}
		if p.fieldMask&ua.DataSetFieldContentMaskSourceTimestamp != 0 {
			dv.EncodingMask |= ua.DataValueSourceTimestamp
			dv.SourceTimestamp = now
		}
		fields[i] = &message.Field{Index: uint16(i), Name: f.Name, Value: dv}
	}
	pub := p.publisher
	ds := &message.DataSetMessage{
		DataSetWriterID:   p.writerID,
		Type:              message.DataSetMessageKeyFrame,
		Valid:             true,
		UADPMask:          pubsub.DefaultUADPDataSetMessageContentMask,
		JSONMask:          pubsub.DefaultJSONDataSetMessageContentMask,
		FieldContentMask:  p.fieldMask,
		FieldEncoding:     fieldEncodingOf(p.fieldMask),
		SequenceNumber:    seq,
		Timestamp:         now,
		MetaDataVersion:   *p.md.ConfigurationVersion,
		DataSetWriterName: p.writerName,
		PublisherID:       &pub,
		WriterGroupName:   p.groupName,
		Fields:            fields,
	}
	nm := &message.NetworkMessage{
		Type:            message.NetworkMessageDataSet,
		UADPMask:        pubsub.DefaultUADPNetworkMessageContentMask,
		JSONMask:        pubsub.DefaultJSONNetworkMessageContentMask,
		PublisherID:     p.publisher,
		GroupHeader:     message.GroupHeader{WriterGroupID: p.groupID, SequenceNumber: uint16(seq), NetworkMessageNumber: 1},
		Timestamp:       now,
		WriterGroupName: p.groupName,
		Messages:        []*message.DataSetMessage{ds},
	}
	if p.enc == parse.EncodingJSON {
		return jsonmsg.Encode(nm, &jsonmsg.EncodeOptions{MetaData: p.lookup, NumericPublisherID: p.numericPubID})
	}
	return uadp.Encode(nm, p.uadpOptions())
}

func (p *pubsubPublisher) uadpOptions() *uadp.EncodeOptions {
	opts := &uadp.EncodeOptions{MetaData: p.lookup}
	if p.mode > ua.MessageSecurityModeNone {
		p.nonce++
		opts.SecurityMode = p.mode
		opts.Keys = p.keys
		opts.NonceSequence = p.nonce
	}
	return opts
}

// metaDataMessage encodes the DataSetMetaData announcement.
func (p *pubsubPublisher) metaDataMessage(now time.Time) ([]byte, error) {
	if p.enc == parse.EncodingJSON {
		return jsonmsg.EncodeMetaData(&jsonmsg.MetaDataMessage{
			PublisherID:       p.publisher,
			DataSetWriterID:   p.writerID,
			WriterGroupName:   p.groupName,
			DataSetWriterName: p.writerName,
			Timestamp:         now,
			MetaData:          p.md,
		}, &jsonmsg.EncodeOptions{NumericPublisherID: p.numericPubID})
	}
	return uadp.Encode(&message.NetworkMessage{
		Type:        message.NetworkMessageDiscoveryResponse,
		UADPMask:    ua.UADPNetworkMessageContentMaskPublisherID,
		PublisherID: p.publisher,
		Discovery: &message.Discovery{
			Type:            message.DiscoveryDataSetMetaData,
			DataSetWriterID: p.writerID,
			MetaData:        p.md,
		},
	}, p.uadpOptions())
}

// versionTime is a ConfigurationVersion: seconds since 2000-01-01 (Part 4
// VersionTime).
func versionTime(t time.Time) uint32 {
	return uint32(t.Sub(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)) / time.Second)
}

func newPubSubPublishCmd() *cobra.Command {
	var (
		tf            pubsubTransportFlags
		sf            pubsubSecurityFlags
		encoding      string
		publisherID   string
		groupID       uint16
		writerID      uint16
		groupName     string
		writerName    string
		dataSetName   string
		fieldSpecs    []string
		fieldEncoding string
		interval      time.Duration
		count         int
		duration      time.Duration
		topic         string
		metaTopic     string
		retain        bool
		announce      bool
		numericPubID  bool
	)

	cmd := &cobra.Command{
		Use:   "publish",
		Short: "Publish a DataSet built from the command line (for testing)",
		Long: `Publish a DataSet as PubSub NetworkMessages, for testing subscribers.

Every --field Name:Type=Value adds a field (Boolean, SByte, Byte, Int16,
UInt16, Int32, UInt32, Int64, UInt64, Float, Double, String, DateTime
(RFC 3339), Guid, ByteString (base64), NodeId, StatusCode (hex),
QualifiedName, LocalizedText). Each message is a key frame carrying all
fields, with a sequence number counting up from 1.

--url selects the transport (opc.udp://host:port, unicast or multicast, or
mqtt://host:port / mqtts://host:port with --topic). --encoding uadp (default)
or json (MQTT only). --field-encoding selects the UADP/JSON field encoding:
variant (default), datavalue (with status and source timestamp) or rawdata
(subscribers need the DataSetMetaData).

The DataSetMetaData can be announced first: --announce-metadata sends a UADP
DataSetMetaData announcement over UDP, --metadata-topic sends a retained
ua-metadata (JSON) or UADP announcement to that MQTT topic.

UADP messages are signed (--message-security sign) or signed and encrypted
(sign-and-encrypt) with the keys of --security-group, pulled from the SKS at
--endpoint, or of --keys-file.

The command sends --count messages (default 1; 0 = until Ctrl+C or
--duration) every --interval.

Examples:
  opcuactl pubsub publish --url opc.udp://224.0.0.22:4840 --field Counter:UInt32=42
  opcuactl pubsub publish --url opc.udp://127.0.0.1:4840 --publisher-id 2234 --writer-group-id 100 \
      --writer-id 1 --field Temp:Double=21.5 --field Name:String=pump --count 0 --interval 500ms
  opcuactl pubsub publish --url mqtt://broker:1883 --topic plant/line1 --encoding json \
      --field Temp:Double=21.5 --metadata-topic plant/line1/meta
  opcuactl pubsub publish --url opc.udp://224.0.0.22:4840 --field Counter:UInt32=1 \
      --message-security sign-and-encrypt --keys-file keys.json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			enc, err := parse.ParsePubSubEncoding(encoding, false)
			if err != nil {
				return exitCode(1, err)
			}
			mode, err := parse.MessageSecurity(sf.mode)
			if err != nil {
				return exitCode(1, err)
			}
			if len(fieldSpecs) == 0 {
				return exitCodef(1, "at least one --field Name:Type=Value is required")
			}
			fields := make([]parse.PubSubFieldValue, len(fieldSpecs))
			mdFields := make([]pubsub.Field, len(fieldSpecs))
			for i, s := range fieldSpecs {
				f, err := parse.PubSubFieldWithValue(s)
				if err != nil {
					return exitCode(1, err)
				}
				fields[i], mdFields[i] = f, f.Field
			}
			fieldMask, err := parse.FieldEncoding(fieldEncoding)
			if err != nil {
				return exitCode(1, err)
			}
			pid, err := parse.PublisherID(publisherID)
			if err != nil {
				return exitCode(1, err)
			}
			scheme, err := transport.Scheme(tf.url)
			if err != nil && tf.url != "" {
				return exitCode(1, err)
			}
			broker := isBrokerScheme(scheme)
			switch {
			case broker && topic == "":
				return exitCodef(1, "--topic is required with a broker (MQTT topic, AMQP address)")
			case !broker && metaTopic != "":
				return exitCodef(1, "--metadata-topic needs a broker; use --announce-metadata over UDP or Ethernet")
			case mode > ua.MessageSecurityModeNone && enc == parse.EncodingJSON:
				return exitCodef(1, "--message-security is only available with the UADP encoding")
			case mode == ua.MessageSecurityModeNone && (sf.group != "" || sf.keysFile != ""):
				return exitCodef(1, "--security-group/--keys-file need --message-security sign or sign-and-encrypt")
			}

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			var keys security.KeySource
			if mode > ua.MessageSecurityModeNone {
				if sf.group == "" && sf.keysFile == "" {
					return exitCodef(1, "--message-security %s needs --security-group or --keys-file", sf.mode)
				}
				var closeKeys func()
				keys, closeKeys, err = sf.keySource(ctx)
				if err != nil {
					return err
				}
				defer closeKeys()
			}

			md := pubsub.MetaData(dataSetName, mdFields...)
			md.ConfigurationVersion = &ua.ConfigurationVersionDataType{
				MajorVersion: versionTime(time.Now()),
				MinorVersion: versionTime(time.Now()),
			}
			p := &pubsubPublisher{
				enc: enc, publisher: pid, groupID: groupID, writerID: writerID,
				groupName: groupName, writerName: writerName, fields: fields, fieldMask: fieldMask,
				md: md, mode: mode, keys: keys, numericPubID: numericPubID,
			}

			conn, err := tf.open(ctx, enc)
			if err != nil {
				return err
			}
			defer func() { _ = conn.Close() }()

			send := func(b []byte, opts transport.PublishOptions) error {
				sendCtx, cancel := connCfg.TimeoutContext(ctx)
				defer cancel()
				return conn.Send(sendCtx, b, opts)
			}

			sent, bytesSent := 0, 0
			if announce || metaTopic != "" {
				b, err := p.metaDataMessage(time.Now())
				if err != nil {
					return exitCodef(3, "encoding the DataSetMetaData: %v", err)
				}
				if err := send(b, transport.PublishOptions{Topic: metaTopic, QoS: tf.qos, Retain: true, MessageType: transport.MessageTypeMetaData}); err != nil {
					return exitCodef(2, "sending the DataSetMetaData: %v", err)
				}
				if verbose {
					pubsubInfo("sent DataSetMetaData (%d bytes)", len(b))
				}
			}

			var deadline <-chan time.Time
			if duration > 0 {
				timer := time.NewTimer(duration)
				defer timer.Stop()
				deadline = timer.C
			}
			ticker := time.NewTicker(interval)
			defer ticker.Stop()

			for seq := uint32(1); ; seq++ {
				b, err := p.dataMessage(seq, time.Now())
				if err != nil {
					return exitCodef(3, "encoding message %d: %v", seq, err)
				}
				if err := send(b, transport.PublishOptions{Topic: topic, QoS: tf.qos, Retain: retain, MessageType: transport.MessageTypeData}); err != nil {
					return exitCodef(2, "sending message %d: %v", seq, err)
				}
				sent++
				bytesSent += len(b)
				if verbose {
					pubsubInfo("sent message %d (%d bytes)", seq, len(b))
				}
				if count > 0 && sent >= count {
					break
				}
				select {
				case <-ctx.Done():
					return publishSummary(tf.url, topic, sent, bytesSent)
				case <-deadline:
					return publishSummary(tf.url, topic, sent, bytesSent)
				case <-ticker.C:
				}
			}
			return publishSummary(tf.url, topic, sent, bytesSent)
		},
	}

	fs := cmd.Flags()
	tf.register(fs)
	sf.register(cmd, "Security of the messages (none|sign|sign-and-encrypt)")
	fs.StringVar(&encoding, "encoding", "uadp", "Message mapping (uadp|json)")
	fs.StringVar(&publisherID, "publisher-id", "1", "PublisherId (number = UInt16; byte:N, u32:N, u64:N, or a string)")
	fs.Uint16Var(&groupID, "writer-group-id", 1, "WriterGroupId")
	fs.Uint16Var(&writerID, "writer-id", 1, "DataSetWriterId")
	fs.StringVar(&groupName, "writer-group-name", "opcuactl", "WriterGroup name (JSON messages)")
	fs.StringVar(&writerName, "writer-name", "opcuactl", "DataSetWriter name (JSON messages)")
	fs.StringVar(&dataSetName, "dataset-name", "DataSet", "Name of the DataSet in its DataSetMetaData")
	fs.StringArrayVar(&fieldSpecs, "field", nil, "Field Name:Type=Value (repeatable)")
	fs.StringVar(&fieldEncoding, "field-encoding", "variant", "Field encoding (variant|datavalue|rawdata)")
	fs.DurationVar(&interval, "interval", time.Second, "Publishing interval")
	fs.IntVar(&count, "count", 1, "Number of messages to send (0 = until Ctrl+C or --duration)")
	fs.DurationVar(&duration, "duration", 0, "Stop after this duration (0 = no limit)")
	fs.StringVar(&topic, "topic", "", "MQTT topic (queue name) of the data messages")
	fs.StringVar(&metaTopic, "metadata-topic", "", "MQTT topic to send the DataSetMetaData to (retained) before the data")
	fs.BoolVar(&retain, "retain", false, "Ask the MQTT broker to retain the data messages")
	fs.BoolVar(&announce, "announce-metadata", false, "Send a UADP DataSetMetaData announcement before the data (UDP)")
	fs.BoolVar(&numericPubID, "numeric-publisher-id", false, "JSON: write numeric PublisherIds as numbers (OPC UA 1.04) instead of strings")
	_ = cmd.RegisterFlagCompletionFunc("encoding", fixedCompletion([]string{"uadp", "json"}))
	_ = cmd.RegisterFlagCompletionFunc("field-encoding", fixedCompletion(pubsubFieldEncodingValues))
	return cmd
}

func publishSummary(url, topic string, sent, bytesSent int) error {
	if quiet {
		return nil
	}
	if format.ParseFormat(outputFormat) == format.Table {
		dest := url
		if topic != "" {
			dest += " topic " + topic
		}
		fmt.Printf("published %d message(s), %d bytes, to %s\n", sent, bytesSent, dest)
		return nil
	}
	return newWriter().Write(map[string]interface{}{
		"url":      url,
		"topic":    topic,
		"messages": sent,
		"bytes":    bytesSent,
	})
}
