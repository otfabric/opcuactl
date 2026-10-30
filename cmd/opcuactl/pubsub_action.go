// SPDX-License-Identifier: MIT

package opcuactl

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"time"

	"github.com/otfabric/go-opcua/pubsub"
	"github.com/otfabric/go-opcua/pubsub/message"
	"github.com/otfabric/go-opcua/pubsub/security"
	"github.com/otfabric/go-opcua/pubsub/transport"
	mqtttransport "github.com/otfabric/go-opcua/pubsub/transport/mqtt"
	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/opcuactl/internal/format"
	"github.com/otfabric/opcuactl/internal/model"
	"github.com/otfabric/opcuactl/internal/parse"
	"github.com/spf13/cobra"
)

// PubSub Actions (Part 14 §6.2.11.2 in 1.05): `pubsub call` is a Requestor,
// `pubsub respond` a Responder. Both run go-opcua's PubSub runtime with one
// connection built from the transport flags.

// Counters of the PubSub Actions of go-opcua's runtime.
const (
	ctrActionRequestsSent      = "ActionRequestsSent"
	ctrActionRequestsReceived  = "ActionRequestsReceived"
	ctrActionRequestsRejected  = "ActionRequestsRejected"
	ctrActionResponsesSent     = "ActionResponsesSent"
	ctrActionResponsesReceived = "ActionResponsesReceived"
	ctrActionTimeouts          = "ActionTimeouts"
)

// actionConnection is the name of the connection of the runtime.
const actionConnection = "opcuactl"

// Defaults shared by call and respond, so that the two work together
// without addressing flags.
const (
	defaultActionGroupName   = "Actions"
	defaultActionTopicPrefix = "opcua"
)

// pubsubActionFlags are the flags call and respond share.
type pubsubActionFlags struct {
	tf pubsubTransportFlags
	sf pubsubSecurityFlags
	gf pubsubDiagnosticsFlags

	encoding        string
	publisherID     string
	writerID        uint16
	groupName       string
	topicPrefix     string
	requestAddress  string
	jsonCompression string
}

func (f *pubsubActionFlags) register(cmd *cobra.Command, publisherHelp string) {
	fs := cmd.Flags()
	f.tf.register(fs)
	f.sf.register(cmd, "Security of the UADP action messages (none|sign|sign-and-encrypt)")
	f.gf.register(fs)
	fs.StringVar(&f.encoding, "encoding", "uadp", "Message mapping (uadp|json; json needs a broker)")
	fs.StringVar(&f.publisherID, "publisher-id", "1", publisherHelp)
	fs.Uint16Var(&f.writerID, "writer-id", 1, "DataSetWriterId of the Action writer of the Responder")
	fs.StringVar(&f.groupName, "writer-group-name", defaultActionGroupName, "Name of the Responder's writer group (broker: part of the default request queue)")
	fs.StringVar(&f.topicPrefix, "topic-prefix", defaultActionTopicPrefix, "Broker: prefix of the default request queue <prefix>/<encoding>/action-request/<publisher-id>/<writer-group-name>")
	fs.StringVar(&f.jsonCompression, "json-compression", "", "Compress JSON action messages over MQTT 5.0 (none|gzip): Content Type application/json+gzip")
	fs.StringVar(&f.requestAddress, "request-address", "", "Where the requests go: the request queue on a broker (default: see --topic-prefix); on UDP the Responder's opc.udp:// address (default: --url)")
	_ = cmd.RegisterFlagCompletionFunc("encoding", fixedCompletion([]string{"uadp", "json"}))
	_ = cmd.RegisterFlagCompletionFunc("mqtt-version", fixedCompletion(pubsubMQTTVersionValues))
	_ = cmd.RegisterFlagCompletionFunc("json-compression", fixedCompletion(pubsubJSONCompressionValues))
	_ = cmd.RegisterFlagCompletionFunc("diagnostics-level", fixedCompletion(pubsubDiagnosticsLevelValues))
}

// pubsubActionSetup is what the shared flags resolve to.
type pubsubActionSetup struct {
	enc       parse.PubSubEncoding
	mapping   pubsub.MessageMapping
	transport transport.Config
	broker    bool
	responder message.PublisherID
	mode      ua.MessageSecurityMode
	// groupID is the SecurityGroupId of the keys (with mode > None).
	groupID string
	keys    security.KeySource
	// requestAddress is the request queue on a broker, the Responder's
	// address (or empty for --url) on a datagram transport.
	requestAddress string
	// compress sends JSON action messages gzip compressed.
	compress bool
}

// setup validates the shared flags. It does not open anything but the
// key source; close stops it.
func (f *pubsubActionFlags) setup(ctx context.Context) (*pubsubActionSetup, func(), error) {
	noop := func() {}
	enc, err := parse.ParsePubSubEncoding(f.encoding, false)
	if err != nil {
		return nil, noop, exitCode(exitUsage, err)
	}
	mode, err := parse.MessageSecurity(f.sf.mode)
	if err != nil {
		return nil, noop, exitCode(exitUsage, err)
	}
	if err := f.gf.validate(); err != nil {
		return nil, noop, err
	}
	tcfg, err := f.tf.config(enc)
	if err != nil {
		return nil, noop, err
	}
	pid, err := parse.PublisherID(f.publisherID)
	if err != nil {
		return nil, noop, exitCode(exitUsage, err)
	}
	if f.writerID == 0 {
		return nil, noop, exitCodef(exitUsage, "--writer-id must not be 0")
	}
	scheme, _ := transport.Scheme(tcfg.URL)
	s := &pubsubActionSetup{enc: enc, mapping: pubsub.MappingUADP, transport: tcfg, broker: isBrokerScheme(scheme),
		responder: pid, mode: mode, requestAddress: f.requestAddress}
	if enc == parse.EncodingJSON {
		s.mapping = pubsub.MappingJSON
	}
	if s.compress, err = checkJSONCompression(f.jsonCompression, enc, tcfg.URL); err != nil {
		return nil, noop, err
	}
	switch {
	case mode > ua.MessageSecurityModeNone && enc == parse.EncodingJSON:
		return nil, noop, exitCodef(exitUsage, "--message-security is only available with the UADP encoding")
	case mode == ua.MessageSecurityModeNone && (f.sf.group != "" || f.sf.keysFile != ""):
		return nil, noop, exitCodef(exitUsage, "--security-group/--keys-file need --message-security sign or sign-and-encrypt")
	case mode > ua.MessageSecurityModeNone && f.sf.group == "" && f.sf.keysFile == "":
		return nil, noop, exitCodef(exitUsage, "--message-security %s needs --security-group or --keys-file", f.sf.mode)
	}
	if s.broker {
		if s.requestAddress == "" {
			s.requestAddress = pubsub.ActionRequestTopic(f.topicPrefix, s.mapping, pid.String(), f.groupName)
		}
	} else if s.requestAddress != "" {
		if rs, err := transport.Scheme(s.requestAddress); err != nil || rs != scheme {
			return nil, noop, exitCodef(exitUsage, "--request-address %q must be a %s:// address like --url", s.requestAddress, scheme)
		}
	}
	closeKeys := noop
	if mode > ua.MessageSecurityModeNone {
		if s.keys, closeKeys, err = f.sf.keySource(ctx); err != nil {
			return nil, noop, err
		}
		s.groupID = f.sf.group
		if ring, ok := s.keys.(*security.KeyRing); ok && s.groupID == "" {
			s.groupID = ring.SecurityGroupID()
		}
	}
	return s, closeKeys, nil
}

// connection returns the PubSubConnection of the runtime, with PublisherId
// pid.
func (s *pubsubActionSetup) connection(pid message.PublisherID) *ua.PubSubConnectionDataType {
	c := &ua.PubSubConnectionDataType{
		Name:                actionConnection,
		Enabled:             true,
		PublisherID:         pid.Variant(),
		TransportProfileURI: s.transport.ProfileURI,
		Address:             ua.NewExtensionObject(&ua.NetworkAddressURLDataType{URL: s.transport.URL, NetworkInterface: s.transport.Interface}),
	}
	if s.compress {
		pubsub.SetJSONCompression(c, mqtttransport.CompressionGzip)
	}
	return c
}

// options returns the runtime options: the broker settings of the flags,
// the keys and a logger for --verbose.
func (s *pubsubActionSetup) options() []pubsub.Option {
	tcfg := s.transport
	opts := []pubsub.Option{
		pubsub.WithRetryInterval(time.Second),
		// The runtime's transports (the connection and the senders to other
		// datagram addresses) get the broker settings of the flags.
		pubsub.WithTransportConfig(func(_ *ua.PubSubConnectionDataType, cfg *transport.Config) {
			cfg.MQTTVersion = tcfg.MQTTVersion
			cfg.ClientID = tcfg.ClientID
			cfg.Username = tcfg.Username
			cfg.Password = tcfg.Password
			if len(tcfg.Properties) > 0 {
				if cfg.Properties == nil {
					cfg.Properties = map[string]string{}
				}
				maps.Copy(cfg.Properties, tcfg.Properties)
			}
		}),
	}
	if s.keys != nil {
		opts = append(opts, pubsub.WithKeySource(s.groupID, s.keys))
	}
	if verbose {
		opts = append(opts, pubsub.WithLogger(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))))
	}
	return opts
}

// startActionRuntime starts the runtime and waits up to --timeout until its
// connection is open and subscribed: a request or response sent to it from
// then on is received.
func startActionRuntime(ctx context.Context, cfg *ua.PubSubConfigurationDataType, url string, opts []pubsub.Option) (*pubsub.PubSub, error) {
	ps, err := pubsub.New(cfg, opts...)
	if err != nil {
		return nil, exitCode(exitUsage, err)
	}
	if err := ps.Start(ctx); err != nil {
		return nil, exitCode(exitTransport, err)
	}
	wctx, cancel := connCfg.TimeoutContext(ctx)
	defer cancel()
	if err := ps.Ready(wctx); err != nil {
		reason := "it did not open in time"
		id, _ := ps.Lookup(pubsub.KindConnection, pubsub.Root, actionConnection)
		if st, err := ps.Status(id); err == nil && st.LastError != nil {
			reason = transportError(url, st.LastError).Error()
		}
		_ = ps.Close()
		if ctx.Err() != nil {
			return nil, exitCode(exitTransport, ctx.Err())
		}
		return nil, exitCodef(exitTransport, "connecting to %s: %s", url, reason)
	}
	return ps, nil
}

// newActionDiagnostics returns the diagnostics of a command that show the
// counters of the runtime.
func newActionDiagnostics(ps *pubsub.PubSub, component string, counters ...string) *pubsubDiagnostics {
	d := newPubSubDiagnostics(component, nil, counters...)
	d.refresh = func(d *pubsubDiagnostics) {
		st, err := ps.Status(pubsub.Root)
		if err != nil {
			return
		}
		c := st.Counters
		for name, v := range map[string]uint64{
			ctrActionRequestsSent:      c.ActionRequestsSent,
			ctrActionRequestsReceived:  c.ActionRequestsReceived,
			ctrActionRequestsRejected:  c.ActionRequestsRejected,
			ctrActionResponsesSent:     c.ActionResponsesSent,
			ctrActionResponsesReceived: c.ActionResponsesReceived,
			ctrActionTimeouts:          c.ActionTimeouts,
		} {
			d.set(name, v)
		}
	}
	return d
}

// randomRequestorID is a PublisherId for the Requestor.
func randomRequestorID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return "opcuactl-" + hex.EncodeToString(b)
}

// actionFields converts values to fields, named after md.
func actionFields(values []*ua.Variant, md []pubsub.Field) []model.PubSubField {
	out := make([]model.PubSubField, len(values))
	for i, v := range values {
		f := &message.Field{Index: uint16(i)}
		if i < len(md) {
			f.Name = md[i].Name
		}
		if v != nil {
			f.Value = &ua.DataValue{EncodingMask: ua.DataValueValue, Value: v}
		}
		out[i] = fieldModel(f, nil, false)
	}
	return out
}

// fieldsString is the table form of fields.
func fieldsString(fields []model.PubSubField) string {
	s := ""
	for _, f := range fields {
		name := f.Name
		if name == "" {
			name = fmt.Sprintf("#%d", f.Index)
		}
		s += fmt.Sprintf(" %s=%s", name, format.FormatValue(f.Value))
	}
	return s
}
