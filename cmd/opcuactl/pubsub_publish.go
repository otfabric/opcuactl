// SPDX-License-Identifier: MIT

package opcuactl

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"slices"
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

// pubsubPublisher builds the NetworkMessages of `pubsub publish`. It is
// used from one goroutine.
type pubsubPublisher struct {
	enc          parse.PubSubEncoding
	publisher    message.PublisherID
	groupID      uint16
	writerID     uint16
	groupName    string
	writerName   string
	dataSetName  string
	fields       []parse.PubSubFieldValue
	fieldMask    ua.DataSetFieldContentMask
	md           *ua.DataSetMetaDataType
	mode         ua.MessageSecurityMode
	keys         security.KeySource
	nonce        uint32
	numericPubID bool
	interval     time.Duration
	// url and profile describe the connection in PubSubConnection
	// announcements.
	url, profile string
	// maxSize is the largest NetworkMessage: larger UADP messages are sent
	// as chunks (0: no limit).
	maxSize int
	// groupSeq is the GroupHeader SequenceNumber of the last NetworkMessage,
	// annSeq the SequenceNumber of the last discovery announcement.
	groupSeq uint16
	annSeq   uint16
	limiter  discoveryLimiter
	diag     *pubsubDiagnostics
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

// isSecurityError tells a failure of the message security (signing,
// verification, encryption, decryption, keys, security mode) from other
// codec errors.
func isSecurityError(err error) bool {
	return errors.Is(err, uadp.ErrSecurityChecksFailed) || errors.Is(err, uadp.ErrSecurityMode) ||
		errors.Is(err, security.ErrNoKeys) || errors.Is(err, security.ErrSignature)
}

// dataMessage encodes the DataSet as message number seq: one NetworkMessage,
// or its chunks.
func (p *pubsubPublisher) dataMessage(seq uint32, now time.Time) ([][]byte, error) {
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
		GroupHeader:     message.GroupHeader{WriterGroupID: p.groupID, SequenceNumber: p.groupSeq + 1, NetworkMessageNumber: 1},
		Timestamp:       now,
		WriterGroupName: p.groupName,
		Messages:        []*message.DataSetMessage{ds},
	}
	if p.enc == parse.EncodingJSON {
		b, err := jsonmsg.Encode(nm, &jsonmsg.EncodeOptions{MetaData: p.lookup, NumericPublisherID: p.numericPubID})
		if err != nil {
			return nil, err
		}
		return p.single(b)
	}
	parts, err := p.encodeUADP(nm, true)
	if err != nil {
		return nil, err
	}
	p.groupSeq += uint16(len(parts))
	return parts, nil
}

// single checks the size of a JSON message: the JSON mapping has no chunks.
func (p *pubsubPublisher) single(b []byte) ([][]byte, error) {
	if p.maxSize > 0 && len(b) > p.maxSize {
		return nil, fmt.Errorf("the JSON message of %d bytes exceeds the message size limit of %d bytes (JSON has no chunks)", len(b), p.maxSize)
	}
	return [][]byte{b}, nil
}

// encodeUADP encodes nm, secured if secure and the publisher secures its
// messages, as chunks if it is larger than maxSize.
func (p *pubsubPublisher) encodeUADP(nm *message.NetworkMessage, secure bool) ([][]byte, error) {
	opts := &uadp.EncodeOptions{MetaData: p.lookup}
	if secure && p.mode > ua.MessageSecurityModeNone {
		opts.SecurityMode = p.mode
		opts.Keys = p.keys
		opts.NextNonceSequence = func() uint32 {
			p.nonce++
			return p.nonce
		}
	}
	b, err := uadp.Encode(nm, opts)
	if err != nil {
		return nil, err
	}
	if p.maxSize <= 0 || len(b) <= p.maxSize {
		return [][]byte{b}, nil
	}
	return uadp.EncodeChunks(nm, opts, p.maxSize)
}

// metaDataMessage encodes the DataSetMetaData announcement.
func (p *pubsubPublisher) metaDataMessage(now time.Time) ([][]byte, error) {
	if p.enc == parse.EncodingJSON {
		b, err := jsonmsg.EncodeMetaData(&jsonmsg.MetaDataMessage{
			PublisherID:       p.publisher,
			DataSetWriterID:   p.writerID,
			WriterGroupName:   p.groupName,
			DataSetWriterName: p.writerName,
			Timestamp:         now,
			MetaData:          p.md,
		}, &jsonmsg.EncodeOptions{NumericPublisherID: p.numericPubID})
		if err != nil {
			return nil, err
		}
		return p.single(b)
	}
	return p.announcement(p.metaDataAnnouncement(), message.DiscoveryLayoutSpec, true)
}

func (p *pubsubPublisher) metaDataAnnouncement() *message.Discovery {
	return &message.Discovery{
		Type:            message.DiscoveryDataSetMetaData,
		DataSetWriterID: p.writerID,
		MetaData:        p.md,
		StatusCode:      ua.StatusGood,
	}
}

// announcement encodes a discovery announcement in a layout.
func (p *pubsubPublisher) announcement(d *message.Discovery, layout message.DiscoveryLayout, secure bool) ([][]byte, error) {
	p.annSeq++
	d.SequenceNumber = p.annSeq
	d.Layout = layout
	return p.encodeUADP(&message.NetworkMessage{
		Type:        message.NetworkMessageDiscoveryResponse,
		UADPMask:    ua.UADPNetworkMessageContentMaskPublisherID,
		PublisherID: p.publisher,
		Discovery:   d,
	}, secure)
}

// groupConfig is the configuration of the writer group as announced.
func (p *pubsubPublisher) groupConfig(writers bool) *ua.WriterGroupDataType {
	g := &ua.WriterGroupDataType{
		Name:                  p.groupName,
		Enabled:               true,
		SecurityMode:          p.mode,
		WriterGroupID:         p.groupID,
		PublishingInterval:    float64(p.interval) / float64(time.Millisecond),
		MaxNetworkMessageSize: uint32(max(p.maxSize, 0)),
	}
	if writers {
		g.DataSetWriters = []*ua.DataSetWriterDataType{{
			Name:                    p.writerName,
			Enabled:                 true,
			DataSetWriterID:         p.writerID,
			DataSetFieldContentMask: p.fieldMask,
			KeyFrameCount:           1,
			DataSetName:             p.dataSetName,
		}}
	}
	return g
}

// answers builds the announcements that answer a probe, as far as the
// limiter allows: DataSetMetaData, DataSetWriter and WriterGroup
// configuration, PubSubConnections, PublisherEndpoints (not supported) and
// FindApplications (the state).
func (p *pubsubPublisher) answers(nm *message.NetworkMessage, now time.Time) []discoveryAnswer {
	req := nm.Request
	forUs := req.Layout == message.DiscoveryLayoutDotNet ||
		nm.UADPMask&ua.UADPNetworkMessageContentMaskPublisherID == 0 || nm.PublisherID.Equal(p.publisher)
	if !forUs {
		return nil
	}
	var out []discoveryAnswer
	add := func(key string, d *message.Discovery, data bool) {
		if p.limiter.allow(fmt.Sprintf("%s/%d", key, req.Layout), now) {
			out = append(out, discoveryAnswer{d: d, data: data})
		}
	}
	if req.ProbeType == message.ProbeFindApplications {
		add("applications", &message.Discovery{Type: message.DiscoveryApplicationInformation, Application: &message.ApplicationInformation{
			Type: message.ApplicationInformationStatus, Status: ua.PubSubStateOperational, Timestamp: now,
		}}, false)
		return out
	}
	if req.ProbeType != message.ProbePublisherInformation {
		return nil
	}
	switch req.InformationType {
	case message.InformationPublisherEndpoints:
		add("endpoints", &message.Discovery{Type: message.DiscoveryPublisherEndpoints, StatusCode: ua.StatusBadNotSupported}, false)
	case message.InformationDataSetMetaData:
		if probeWanted(req, p.writerID) {
			add("metadata", p.metaDataAnnouncement(), true)
		}
	case message.InformationDataSetWriterConfig:
		if probeWanted(req, p.writerID) {
			add("writer", &message.Discovery{Type: message.DiscoveryDataSetWriterConfig, DataSetWriterIDs: []uint16{p.writerID},
				WriterGroup: p.groupConfig(true), StatusCodes: []ua.StatusCode{ua.StatusGood}}, true)
		}
	case message.InformationWriterGroupConfig:
		if req.WriterGroupID == p.groupID {
			add("group", &message.Discovery{Type: message.DiscoveryDataSetWriterConfig, DataSetWriterIDs: []uint16{p.writerID},
				WriterGroup: p.groupConfig(req.IncludeDataSetWriters), StatusCodes: []ua.StatusCode{ua.StatusGood}}, true)
		}
	case message.InformationPubSubConnections:
		if len(req.TransportProfileURIs) > 0 && !slices.Contains(req.TransportProfileURIs, p.profile) {
			return nil
		}
		conn := &ua.PubSubConnectionDataType{
			Name:                "opcuactl",
			Enabled:             true,
			PublisherID:         p.publisher.Variant(),
			TransportProfileURI: p.profile,
			Address:             ua.NewExtensionObject(&ua.NetworkAddressURLDataType{URL: p.url}),
		}
		if req.IncludeWriterGroups {
			conn.WriterGroups = []*ua.WriterGroupDataType{p.groupConfig(req.IncludeDataSetWriters)}
		}
		add("connections", &message.Discovery{Type: message.DiscoveryPubSubConnection, Connections: []*ua.PubSubConnectionDataType{conn}}, false)
	}
	return out
}

// versionTime is a ConfigurationVersion: seconds since 2000-01-01 (Part 4
// VersionTime).
func versionTime(t time.Time) uint32 {
	return uint32(t.Sub(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)) / time.Second)
}

func newPubSubPublishCmd() *cobra.Command {
	var (
		tf             pubsubTransportFlags
		sf             pubsubSecurityFlags
		df             pubsubDiscoveryFlags
		gf             pubsubDiagnosticsFlags
		encoding       string
		publisherID    string
		groupID        uint16
		writerID       uint16
		groupName      string
		writerName     string
		dataSetName    string
		fieldSpecs     []string
		fieldEncoding  string
		interval       time.Duration
		count          int
		duration       time.Duration
		topic          string
		metaTopic      string
		retain         bool
		announce       bool
		announceEvery  time.Duration
		answerProbes   bool
		maxMessageSize int
		numericPubID   bool
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

--url selects the transport: opc.udp://host:port (unicast or multicast),
opc.eth://MAC[:VID[.PCP]] (raw Ethernet, with --interface; needs root or
CAP_NET_RAW), or a broker with --topic: mqtt://host:port / mqtts://host:port
(--mqtt-version 5.0 sends the content type and UAMessageType of every
message) or amqp://host:port / amqps://host:port (--topic is the AMQP
address, e.g. /queues/opcua on RabbitMQ 4). --encoding uadp (default) or
json (brokers only). --field-encoding selects the UADP/JSON field encoding:
variant (default), datavalue (with status and source timestamp) or rawdata
(subscribers need the DataSetMetaData).

UADP messages larger than --max-message-size (default: the transport's
limit, 1500 bytes or the MTU over Ethernet) are sent as chunks (Part 14
§7.2.4.4.4); JSON messages cannot be chunked.

The DataSetMetaData can be announced: --announce-metadata sends a UADP
DataSetMetaData announcement over UDP or Ethernet before the data,
--metadata-topic sends it (retained on MQTT) to that broker topic, and
--announce-interval repeats the announcement. --answer-probes answers UADP
discovery probes (DataSetMetaData, DataSetWriter and WriterGroup
configuration, PubSubConnections, FindApplications) received on a multicast
--url or on --discovery-url while the command runs.

UADP messages are signed (--message-security sign) or signed and encrypted
(sign-and-encrypt) with the keys of --security-group, pulled from the SKS at
--endpoint, or of --keys-file.

The command sends --count messages (default 1; 0 = until Ctrl+C or
--duration) every --interval. --diagnostics prints the counters of what was
sent on stderr when done (--diagnostics-interval also while running).

Examples:
  opcuactl pubsub publish --url opc.udp://224.0.0.22:4840 --field Counter:UInt32=42
  opcuactl pubsub publish --url opc.udp://127.0.0.1:4840 --publisher-id 2234 --writer-group-id 100 \
      --writer-id 1 --field Temp:Double=21.5 --field Name:String=pump --count 0 --interval 500ms
  opcuactl pubsub publish --url mqtt://broker:1883 --topic plant/line1 --encoding json \
      --field Temp:Double=21.5 --metadata-topic plant/line1/meta
  opcuactl pubsub publish --url mqtt://broker:1883 --mqtt-version 5.0 --qos 2 --topic plant/line1 --field Temp:Double=21.5
  opcuactl pubsub publish --url amqp://rabbitmq:5672 --topic /queues/opcua --broker-username opcua \
      --broker-password secret --transport-property connection-hostname=vhost:plant --field Temp:Double=21.5
  opcuactl pubsub publish --url amqp://artemis:5672 --topic opcua.data --transport-property target-capabilities=queue \
      --encoding json --field Temp:Double=21.5
  sudo opcuactl pubsub publish --url opc.eth://01-00-5E-00-00-01 --interface eth0 --field Counter:UInt32=1
  opcuactl pubsub publish --url opc.udp://224.0.0.22:4840 --field Blob:ByteString=... --max-message-size 1400
  opcuactl pubsub publish --url opc.udp://224.0.0.22:4840 --field Counter:UInt32=1 --count 0 --answer-probes \
      --announce-interval 10s --diagnostics
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
			if err := gf.validate(); err != nil {
				return err
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
			case maxMessageSize < 0:
				return exitCodef(1, "--max-message-size must not be negative")
			case announceEvery < 0:
				return exitCodef(1, "--announce-interval must not be negative")
			case announceEvery > 0 && broker && metaTopic == "":
				return exitCodef(1, "--announce-interval needs --metadata-topic with a broker")
			}
			if answerProbes {
				if err := df.validate(tf.url, enc, "--answer-probes"); err != nil {
					return err
				}
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
			diag := newPubSubDiagnostics("writer-group",
				[]pubsub.DiagnosticsCounterID{pubsub.CounterSentNetworkMessages, pubsub.CounterFailedTransmissions, pubsub.CounterEncryptionErrors},
				ctrChunksSent, ctrDiscoveryRequestsReceived, ctrDiscoveryRequestsIgnored, ctrDiscoveryResponsesSent)
			profile, _ := transportProfile(tf.url, enc)
			p := &pubsubPublisher{
				enc: enc, publisher: pid, groupID: groupID, writerID: writerID,
				groupName: groupName, writerName: writerName, dataSetName: dataSetName, fields: fields, fieldMask: fieldMask,
				md: md, mode: mode, keys: keys, numericPubID: numericPubID, interval: interval,
				url: tf.url, profile: profile, maxSize: maxMessageSize, limiter: discoveryLimiter{}, diag: diag,
			}

			conn, err := tf.open(ctx, enc)
			if err != nil {
				return err
			}
			defer func() { _ = conn.Close() }()
			if p.maxSize == 0 && enc == parse.EncodingUADP {
				p.maxSize = conn.MaxMessageSize()
			}
			defer gf.print(diag)
			defer gf.start(diag)()

			// sendParts sends a message or its chunks.
			sendParts := func(c transport.Connection, parts [][]byte, opts transport.PublishOptions) (int, error) {
				n := 0
				for _, b := range parts {
					sendCtx, cancel := connCfg.TimeoutContext(ctx)
					err := c.Send(sendCtx, b, opts)
					cancel()
					if err != nil {
						diag.add(pubsub.CounterFailedTransmissions.String(), 1)
						return n, transportError(tf.url, err)
					}
					diag.add(pubsub.CounterSentNetworkMessages.String(), 1)
					n += len(b)
				}
				if len(parts) > 1 {
					diag.add(ctrChunksSent, len(parts))
				}
				return n, nil
			}
			encodeFailed := func(err error) {
				diag.add(pubsub.CounterFailedTransmissions.String(), 1)
				if isSecurityError(err) {
					diag.add(pubsub.CounterEncryptionErrors.String(), 1)
				}
			}

			sent, bytesSent, chunks := 0, 0, 0
			sendMetaData := func() error {
				parts, err := p.metaDataMessage(time.Now())
				if err != nil {
					encodeFailed(err)
					return exitCodef(3, "encoding the DataSetMetaData: %v", err)
				}
				n, err := sendParts(conn, parts, transport.PublishOptions{Topic: metaTopic, QoS: tf.qos, Retain: true, MessageType: transport.MessageTypeMetaData})
				if err != nil {
					return exitCodef(2, "sending the DataSetMetaData: %v", err)
				}
				if verbose {
					pubsubInfo("sent DataSetMetaData (%d bytes)", n)
				}
				return nil
			}
			if announce || metaTopic != "" {
				if err := sendMetaData(); err != nil {
					return err
				}
			}

			// Probes arrive on the receive goroutine of the transport and
			// are answered from the loop below.
			probes := make(chan []byte, 64)
			var disc transport.Connection
			if answerProbes {
				var closeDisc func()
				disc, closeDisc, err = df.open(ctx, &tf, conn)
				if err != nil {
					return err
				}
				defer closeDisc()
				cancelSub, err := tf.subscribe(ctx, disc, "", func(m *transport.Message) {
					if t, _, err := uadp.PeekType(m.Payload); err != nil || t != message.NetworkMessageDiscoveryRequest {
						return
					}
					select {
					case probes <- m.Payload:
					default:
						diag.add(ctrDiscoveryRequestsIgnored, 1)
					}
				})
				if err != nil {
					return err
				}
				defer func() { _ = cancelSub() }()
			}
			answer := func(b []byte) {
				nm, err := uadp.Decode(b, &uadp.DecodeOptions{
					Security: func(*message.NetworkMessage) (ua.MessageSecurityMode, security.KeySource, error) {
						return ua.MessageSecurityModeNone, keys, nil
					},
				})
				if err != nil || nm.Type != message.NetworkMessageDiscoveryRequest || nm.Request == nil {
					return
				}
				diag.add(ctrDiscoveryRequestsReceived, 1)
				answers := p.answers(nm, time.Now())
				if len(answers) == 0 {
					diag.add(ctrDiscoveryRequestsIgnored, 1)
					return
				}
				for _, a := range answers {
					parts, err := p.announcement(a.d, nm.Request.Layout, a.data)
					if err != nil {
						encodeFailed(err)
						continue
					}
					to := disc
					if a.data {
						to = conn
					}
					if _, err := sendParts(to, parts, transport.PublishOptions{}); err == nil {
						diag.add(ctrDiscoveryResponsesSent, 1)
						if verbose {
							pubsubInfo("answered a %s probe with a %s announcement", nm.Request.InformationType, a.d.Type)
						}
					}
				}
			}

			sendData := func(seq uint32) error {
				parts, err := p.dataMessage(seq, time.Now())
				if err != nil {
					encodeFailed(err)
					return exitCodef(3, "encoding message %d: %v", seq, err)
				}
				n, err := sendParts(conn, parts, transport.PublishOptions{Topic: topic, QoS: tf.qos, Retain: retain, MessageType: transport.MessageTypeData})
				if err != nil {
					return exitCodef(2, "sending message %d: %v", seq, err)
				}
				sent++
				bytesSent += n
				if len(parts) > 1 {
					chunks += len(parts)
				}
				if verbose {
					pubsubInfo("sent message %d (%d bytes, %d NetworkMessage(s))", seq, n, len(parts))
				}
				return nil
			}

			var deadline <-chan time.Time
			if duration > 0 {
				timer := time.NewTimer(duration)
				defer timer.Stop()
				deadline = timer.C
			}
			var announceTick <-chan time.Time
			if announceEvery > 0 {
				t := time.NewTicker(announceEvery)
				defer t.Stop()
				announceTick = t.C
			}
			ticker := time.NewTicker(interval)
			defer ticker.Stop()

			summary := func() error { return publishSummary(tf.url, topic, sent, bytesSent, chunks) }
			if err := sendData(1); err != nil {
				return err
			}
			for count <= 0 || sent < count {
				select {
				case <-ctx.Done():
					return summary()
				case <-deadline:
					return summary()
				case b := <-probes:
					answer(b)
				case <-announceTick:
					if err := sendMetaData(); err != nil {
						return err
					}
				case <-ticker.C:
					if err := sendData(uint32(sent) + 1); err != nil {
						return err
					}
				}
			}
			return summary()
		},
	}

	fs := cmd.Flags()
	tf.register(fs)
	sf.register(cmd, "Security of the messages (none|sign|sign-and-encrypt)")
	df.register(fs)
	gf.register(fs)
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
	fs.StringVar(&topic, "topic", "", "Broker queue of the data messages: MQTT topic or AMQP address")
	fs.StringVar(&metaTopic, "metadata-topic", "", "Broker queue to send the DataSetMetaData to (retained on MQTT) before the data")
	fs.BoolVar(&retain, "retain", false, "Ask the MQTT broker to retain the data messages")
	fs.BoolVar(&announce, "announce-metadata", false, "Send a UADP DataSetMetaData announcement before the data (UDP, Ethernet)")
	fs.DurationVar(&announceEvery, "announce-interval", 0, "Repeat the DataSetMetaData announcement (or --metadata-topic message) at this interval")
	fs.BoolVar(&answerProbes, "answer-probes", false, "Answer UADP discovery probes while running (UDP multicast, Ethernet or --discovery-url)")
	fs.IntVar(&maxMessageSize, "max-message-size", 0, "Largest NetworkMessage in bytes: larger UADP messages are sent as chunks (0 = the transport's limit)")
	fs.BoolVar(&numericPubID, "numeric-publisher-id", false, "JSON: write numeric PublisherIds as numbers (OPC UA 1.04) instead of strings")
	_ = cmd.RegisterFlagCompletionFunc("encoding", fixedCompletion([]string{"uadp", "json"}))
	_ = cmd.RegisterFlagCompletionFunc("field-encoding", fixedCompletion(pubsubFieldEncodingValues))
	_ = cmd.RegisterFlagCompletionFunc("mqtt-version", fixedCompletion(pubsubMQTTVersionValues))
	_ = cmd.RegisterFlagCompletionFunc("discovery-layout", fixedCompletion(pubsubDiscoveryLayoutValues))
	_ = cmd.RegisterFlagCompletionFunc("diagnostics-level", fixedCompletion(pubsubDiagnosticsLevelValues))
	return cmd
}

func publishSummary(url, topic string, sent, bytesSent, chunks int) error {
	if quiet {
		return nil
	}
	if format.ParseFormat(outputFormat) == format.Table {
		dest := url
		if topic != "" {
			dest += " topic " + topic
		}
		in := ""
		if chunks > 0 {
			in = fmt.Sprintf(" in %d chunks", chunks)
		}
		fmt.Printf("published %d message(s), %d bytes%s, to %s\n", sent, bytesSent, in, dest)
		return nil
	}
	out := map[string]interface{}{
		"url":      url,
		"topic":    topic,
		"messages": sent,
		"bytes":    bytesSent,
	}
	if chunks > 0 {
		out["chunks"] = chunks
	}
	return newWriter().Write(out)
}
