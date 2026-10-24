// SPDX-License-Identifier: MIT

package opcuactl

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/otfabric/go-opcua"
	"github.com/otfabric/go-opcua/pubsub"
	"github.com/otfabric/go-opcua/pubsub/jsonmsg"
	"github.com/otfabric/go-opcua/pubsub/message"
	"github.com/otfabric/go-opcua/pubsub/remote"
	"github.com/otfabric/go-opcua/pubsub/security"
	"github.com/otfabric/go-opcua/pubsub/sks"
	"github.com/otfabric/go-opcua/pubsub/transport"
	"github.com/otfabric/go-opcua/pubsub/uadp"
	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/opcuactl/internal/format"
	"github.com/otfabric/opcuactl/internal/model"
	"github.com/otfabric/opcuactl/internal/parse"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var (
	pubsubEncodingValues        = []string{"auto", "uadp", "json"}
	pubsubMessageSecurityValues = []string{"none", "sign", "sign-and-encrypt"}
	pubsubFieldEncodingValues   = []string{"variant", "datavalue", "rawdata"}
)

func newPubSubCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pubsub",
		Short: "OPC UA PubSub: subscribe, publish, decode, keys and configuration",
		Long: `Work with OPC UA PubSub (Part 14).

PubSub sends DataSets from publishers to subscribers without sessions, as
UADP (binary) or JSON NetworkMessages over UDP (opc.udp://, unicast and
multicast) or an MQTT broker (mqtt://, mqtts://).

  subscribe  listen on a transport and print the DataSetMessages received
  publish    send a DataSet built from the command line (for testing)
  decode     decode a captured UADP or JSON NetworkMessage
  sks        get the keys of a SecurityGroup from a Security Key Service
  config     show and change the PublishSubscribe configuration of a server

Secured UADP messages (Sign / SignAndEncrypt, PubSub-Aes128-CTR or
PubSub-Aes256-CTR) need the keys of their SecurityGroup: --security-group
pulls them from the Security Key Service at --endpoint (the session must use
SignAndEncrypt), --keys-file reads keys saved with
"pubsub sks get-keys --show-keys --format json".

Examples:
  opcuactl pubsub subscribe --url opc.udp://224.0.0.22:4840
  opcuactl pubsub subscribe --url mqtt://broker:1883 --topic 'plant/#'
  opcuactl pubsub publish --url opc.udp://224.0.0.22:4840 --field Counter:UInt32=42
  opcuactl pubsub decode capture.bin --field Counter:UInt32
  opcuactl pubsub sks get-keys -e opc.tcp://sks:4840 --security-group sg ...
  opcuactl pubsub config show -e opc.tcp://localhost:4840`,
	}
	cmd.AddCommand(
		newPubSubSubscribeCmd(),
		newPubSubPublishCmd(),
		newPubSubDecodeCmd(),
		newPubSubSKSCmd(),
		newPubSubConfigCmd(),
	)
	return cmd
}

// ---------------------------------------------------------------------------
// Security: keys of a SecurityGroup.

type pubsubSecurityFlags struct {
	group    string
	mode     string
	keysFile string
}

func (f *pubsubSecurityFlags) register(cmd *cobra.Command, modeHelp string) {
	fs := cmd.Flags()
	fs.StringVar(&f.group, "security-group", "", "SecurityGroupId whose keys secure the messages (pulled from the SKS at --endpoint)")
	fs.StringVar(&f.mode, "message-security", "none", modeHelp)
	fs.StringVar(&f.keysFile, "keys-file", "", "Keys of the SecurityGroup saved with 'pubsub sks get-keys --show-keys --format json' (instead of an SKS)")
	_ = cmd.RegisterFlagCompletionFunc("message-security", fixedCompletion(pubsubMessageSecurityValues))
}

// keySource returns the keys of the SecurityGroup, nil without one. close
// stops the background refresh of keys pulled from an SKS.
func (f *pubsubSecurityFlags) keySource(ctx context.Context) (ks security.KeySource, closeFn func(), err error) {
	closeFn = func() {}
	switch {
	case f.keysFile != "":
		ring, err := loadKeysFile(f.keysFile, f.group)
		if err != nil {
			return nil, closeFn, exitCode(1, err)
		}
		return ring, closeFn, nil
	case f.group != "":
		if err := connCfg.RequireEndpoint(); err != nil {
			return nil, closeFn, exitCodef(1, "--security-group pulls keys from a Security Key Service: %v", err)
		}
		src, err := sks.NewKeySource(ctx, sks.Config{
			SecurityGroupID: f.group,
			Dial:            func(ctx context.Context) (*opcua.Client, error) { return connCfg.NewClient(ctx) },
			Timeout:         connCfg.Timeout,
			OnError: func(err error) {
				if verbose {
					format.Errf("refreshing keys of SecurityGroup %q: %v", f.group, err)
				}
			},
		})
		if err != nil {
			return nil, closeFn, exitCodef(2, "getting the keys of SecurityGroup %q: %v", f.group, err)
		}
		return src, func() { _ = src.Close() }, nil
	}
	return nil, closeFn, nil
}

// loadKeysFile reads the JSON output of `pubsub sks get-keys --show-keys`.
func loadKeysFile(file, group string) (*security.KeyRing, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	var sk model.SecurityKeys
	if err := json.Unmarshal(b, &sk); err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	policy, err := security.PolicyByURI(sk.SecurityPolicyURI)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	if len(sk.Keys) == 0 {
		return nil, fmt.Errorf("%s: no keys", file)
	}
	keys := make([][]byte, len(sk.Keys))
	for i, k := range sk.Keys {
		if k.Key == "" {
			return nil, fmt.Errorf("%s: key %d has no key material (save it with --show-keys)", file, k.TokenID)
		}
		if keys[i], err = base64.StdEncoding.DecodeString(k.Key); err != nil {
			return nil, fmt.Errorf("%s: key %d: %w", file, k.TokenID, err)
		}
	}
	parseDur := func(s string) time.Duration {
		d, _ := time.ParseDuration(s)
		return d
	}
	if group == "" {
		group = sk.SecurityGroupID
	}
	ring := security.NewKeyRing(group, policy)
	if err := ring.SetKeys(sk.SecurityPolicyURI, sk.FirstTokenID, keys,
		parseDur(sk.TimeToNextKey), parseDur(sk.KeyLifetime)); err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	return ring, nil
}

// ---------------------------------------------------------------------------
// DataSetMetaData: names and types of the fields.

type pubsubMetaFlags struct {
	fields []string
	file   string
	node   string
}

func (f *pubsubMetaFlags) register(cmd *cobra.Command) {
	fs := cmd.Flags()
	fs.StringArrayVar(&f.fields, "field", nil, "DataSet field Name:Type (Type[] for an array; repeatable, in DataSet order)")
	fs.StringVar(&f.file, "metadata", "", "DataSetMetaData file: a ua-metadata message or a DataSetMetaDataType in the OPC UA JSON encoding")
	fs.StringVar(&f.node, "metadata-node", "", "Read the DataSetMetaData of this PublishedDataSet or DataSetReader node from the server at --endpoint")
}

// load returns the DataSetMetaData given on the command line, nil without.
func (f *pubsubMetaFlags) load(ctx context.Context) (*ua.DataSetMetaDataType, error) {
	n := 0
	for _, set := range []bool{len(f.fields) > 0, f.file != "", f.node != ""} {
		if set {
			n++
		}
	}
	if n > 1 {
		return nil, exitCodef(1, "--field, --metadata and --metadata-node are mutually exclusive")
	}
	switch {
	case len(f.fields) > 0:
		fields := make([]pubsub.Field, len(f.fields))
		for i, s := range f.fields {
			fd, err := parse.PubSubField(s)
			if err != nil {
				return nil, exitCode(1, err)
			}
			fields[i] = fd
		}
		return pubsub.MetaData("DataSet", fields...), nil
	case f.file != "":
		b, err := os.ReadFile(f.file)
		if err != nil {
			return nil, exitCode(1, err)
		}
		md, err := decodeMetaData(b)
		if err != nil {
			return nil, exitCodef(1, "%s: %v", f.file, err)
		}
		return md, nil
	case f.node != "":
		nid, err := parse.NodeIDWithNamespace(f.node, namespace)
		if err != nil {
			return nil, exitCode(1, err)
		}
		c, err := connCfg.NewClient(ctx)
		if err != nil {
			return nil, exitCode(2, err)
		}
		defer func() { _ = c.Close(ctx) }()
		opCtx, cancel := connCfg.TimeoutContext(ctx)
		defer cancel()
		v, err := remote.New(c).Property(opCtx, nid, "DataSetMetaData")
		if err != nil {
			return nil, exitCodef(3, "reading the DataSetMetaData of %s: %v", nid, err)
		}
		if eo, ok := v.Value().(*ua.ExtensionObject); ok {
			if md, ok := eo.Value.(*ua.DataSetMetaDataType); ok {
				return md, nil
			}
		}
		return nil, exitCodef(3, "%s has no DataSetMetaData", nid)
	}
	return nil, nil
}

// decodeMetaData reads a "ua-metadata" JSON message or a
// DataSetMetaDataType in the OPC UA JSON encoding.
func decodeMetaData(b []byte) (*ua.DataSetMetaDataType, error) {
	if t, err := jsonmsg.MessageType(b); err == nil && t == jsonmsg.MessageTypeMetaData {
		m, err := jsonmsg.DecodeMetaData(b, nil)
		if err != nil {
			return nil, err
		}
		return m.MetaData, nil
	}
	md := new(ua.DataSetMetaDataType)
	if err := ua.DecodeJSON(b, md); err != nil {
		return nil, err
	}
	if len(md.Fields) == 0 {
		return nil, errors.New("no fields in the DataSetMetaData")
	}
	return md, nil
}

// ---------------------------------------------------------------------------
// Filters on PublisherId, WriterGroupId and DataSetWriterId.

type pubsubFilterFlags struct {
	publisherID   string
	writerGroupID uint16
	writerID      uint16
}

func (f *pubsubFilterFlags) register(fs *pflag.FlagSet) {
	fs.StringVar(&f.publisherID, "publisher-id", "", "Only messages of this PublisherId")
	fs.Uint16Var(&f.writerGroupID, "writer-group-id", 0, "Only messages of this WriterGroupId (UADP; 0: all)")
	fs.Uint16Var(&f.writerID, "writer-id", 0, "Only DataSetMessages of this DataSetWriterId (0: all)")
}

// ---------------------------------------------------------------------------
// Decoding.

// pubsubDecoder decodes UADP and JSON NetworkMessages. It learns the
// DataSetMetaData announced on the wire (ua-metadata messages, UADP
// DataSetMetaData announcements) and uses it for later messages.
type pubsubDecoder struct {
	encoding  parse.PubSubEncoding
	filter    pubsubFilterFlags
	publisher *message.PublisherID
	static    *ua.DataSetMetaDataType
	keys      security.KeySource
	minMode   ua.MessageSecurityMode

	mu      sync.Mutex
	learned map[string]*ua.DataSetMetaDataType
}

func newPubSubDecoder(enc parse.PubSubEncoding, filter pubsubFilterFlags, static *ua.DataSetMetaDataType,
	keys security.KeySource, minMode ua.MessageSecurityMode) (*pubsubDecoder, error) {
	d := &pubsubDecoder{
		encoding: enc,
		filter:   filter,
		static:   static,
		keys:     keys,
		minMode:  minMode,
		learned:  make(map[string]*ua.DataSetMetaDataType),
	}
	if filter.publisherID != "" {
		p, err := parse.PublisherID(filter.publisherID)
		if err != nil {
			return nil, err
		}
		d.publisher = &p
	}
	if minMode > ua.MessageSecurityModeNone && keys == nil {
		return nil, errors.New("--message-security needs the keys of a SecurityGroup (--security-group or --keys-file)")
	}
	return d, nil
}

// pubsubResult is what one NetworkMessage held.
type pubsubResult struct {
	DataSets []model.PubSubDataSet
	MetaData []model.PubSubMetaData
	Status   map[string]interface{}
}

func metaKey(p message.PublisherID, writerID uint16) string {
	return fmt.Sprintf("%s/%d", p.String(), writerID)
}

func (d *pubsubDecoder) lookup(p message.PublisherID, _ uint16, writerID uint16) *ua.DataSetMetaDataType {
	d.mu.Lock()
	md := d.learned[metaKey(p, writerID)]
	d.mu.Unlock()
	if md != nil {
		return md
	}
	if d.static != nil && (d.filter.writerID == 0 || d.filter.writerID == writerID || writerID == 0) {
		return d.static
	}
	return nil
}

func (d *pubsubDecoder) learn(p message.PublisherID, writerID uint16, md *ua.DataSetMetaDataType) {
	if md == nil {
		return
	}
	d.mu.Lock()
	d.learned[metaKey(p, writerID)] = md
	d.mu.Unlock()
}

func (d *pubsubDecoder) publisherMatches(p message.PublisherID) bool {
	return d.publisher == nil || d.publisher.String() == p.String()
}

func (d *pubsubDecoder) writerMatches(id uint16) bool {
	return d.filter.writerID == 0 || d.filter.writerID == id
}

// detectEncoding tells JSON from UADP: JSON messages are objects or arrays.
func detectEncoding(b []byte) parse.PubSubEncoding {
	for _, c := range b {
		switch c {
		case ' ', '\t', '\r', '\n':
			continue
		case '{', '[':
			return parse.EncodingJSON
		default:
			return parse.EncodingUADP
		}
	}
	return parse.EncodingUADP
}

// errFiltered marks a message the filters dropped.
var errFiltered = errors.New("filtered")

// decode decodes one NetworkMessage. It returns errFiltered for a message
// the filters drop.
func (d *pubsubDecoder) decode(b []byte, msg *transport.Message) (*pubsubResult, error) {
	enc := d.encoding
	if enc == parse.EncodingAuto || enc == "" {
		enc = detectEncoding(b)
	}
	if enc == parse.EncodingJSON {
		return d.decodeJSON(b, msg)
	}
	return d.decodeUADP(b, msg)
}

func (d *pubsubDecoder) decodeUADP(b []byte, msg *transport.Message) (*pubsubResult, error) {
	opts := &uadp.DecodeOptions{
		Accept: func(h *message.NetworkMessage) bool {
			if !d.publisherMatches(h.PublisherID) {
				return false
			}
			if d.filter.writerGroupID != 0 && h.Type == message.NetworkMessageDataSet &&
				h.GroupHeader.WriterGroupID != d.filter.writerGroupID {
				return false
			}
			if d.filter.writerID != 0 && h.Type == message.NetworkMessageDataSet && len(h.Messages) > 0 {
				for _, m := range h.Messages {
					if d.writerMatches(m.DataSetWriterID) {
						return true
					}
				}
				return false
			}
			return true
		},
		Security: func(*message.NetworkMessage) (ua.MessageSecurityMode, security.KeySource, error) {
			return d.minMode, d.keys, nil
		},
		MetaData: d.lookup,
	}
	nm, err := uadp.Decode(b, opts)
	if errors.Is(err, uadp.ErrFiltered) {
		return nil, errFiltered
	}
	if err != nil {
		return nil, err
	}
	res := &pubsubResult{}
	if nm.Type == message.NetworkMessageDiscoveryResponse && nm.Discovery != nil &&
		nm.Discovery.Type == message.DiscoveryDataSetMetaData && nm.Discovery.MetaData != nil {
		d.learn(nm.PublisherID, nm.Discovery.DataSetWriterID, nm.Discovery.MetaData)
		res.MetaData = append(res.MetaData, metaDataModel(msg, "uadp", nm.PublisherID, nm.Discovery.DataSetWriterID, nm.Discovery.MetaData))
		return res, nil
	}
	res.DataSets = d.dataSets(nm, msg, "uadp")
	if len(res.DataSets) == 0 {
		return nil, errFiltered
	}
	return res, nil
}

func (d *pubsubDecoder) decodeJSON(b []byte, msg *transport.Message) (*pubsubResult, error) {
	v, err := jsonmsg.DecodeAny(b, &jsonmsg.DecodeOptions{MetaData: d.lookup})
	if err != nil {
		return nil, err
	}
	res := &pubsubResult{}
	switch m := v.(type) {
	case *jsonmsg.MetaDataMessage:
		if !d.publisherMatches(m.PublisherID) || !d.writerMatches(m.DataSetWriterID) {
			return nil, errFiltered
		}
		d.learn(m.PublisherID, m.DataSetWriterID, m.MetaData)
		res.MetaData = append(res.MetaData, metaDataModel(msg, "json", m.PublisherID, m.DataSetWriterID, m.MetaData))
	case *jsonmsg.StatusMessage:
		if !d.publisherMatches(m.PublisherID) {
			return nil, errFiltered
		}
		res.Status = map[string]interface{}{
			"messageType": jsonmsg.MessageTypeStatus,
			"publisherId": m.PublisherID.String(),
			"state":       strings.TrimPrefix(m.Status.String(), "PubSubState"),
			"isCyclic":    m.IsCyclic,
		}
		if !m.Timestamp.IsZero() {
			res.Status["timestamp"] = model.FormatTimestamp(m.Timestamp)
		}
	case *message.NetworkMessage:
		res.DataSets = d.dataSets(m, msg, "json")
		if len(res.DataSets) == 0 {
			return nil, errFiltered
		}
	default:
		return nil, fmt.Errorf("unexpected message %T", v)
	}
	return res, nil
}

// dataSets converts the DataSetMessages of nm that pass the filters.
func (d *pubsubDecoder) dataSets(nm *message.NetworkMessage, msg *transport.Message, enc string) []model.PubSubDataSet {
	var out []model.PubSubDataSet
	sec := "None"
	var token uint32
	if s := nm.Security; s != nil {
		token = s.SecurityTokenID
		switch {
		case s.Encrypted:
			sec = "SignAndEncrypt"
		case s.Signed:
			sec = "Sign"
		}
	}
	for _, ds := range nm.Messages {
		pub := nm.PublisherID
		if ds.PublisherID != nil {
			pub = *ds.PublisherID
		}
		if !d.publisherMatches(pub) || !d.writerMatches(ds.DataSetWriterID) {
			continue
		}
		groupName := nm.WriterGroupName
		if ds.WriterGroupName != "" {
			groupName = ds.WriterGroupName
		}
		if enc == "json" && d.filter.writerGroupID != 0 {
			// JSON messages carry the WriterGroupName, not the id.
			continue
		}
		ts := ds.Timestamp
		if ts.IsZero() {
			ts = nm.Timestamp
		}
		m := model.PubSubDataSet{
			Encoding:          enc,
			PublisherID:       pub.String(),
			WriterGroupID:     nm.GroupHeader.WriterGroupID,
			WriterGroupName:   groupName,
			DataSetWriterID:   ds.DataSetWriterID,
			DataSetWriterName: ds.DataSetWriterName,
			MessageType:       messageTypeName(ds.Type),
			SequenceNumber:    ds.SequenceNumber,
			Timestamp:         model.FormatTimestamp(ts),
			Security:          sec,
			SecurityTokenID:   token,
			Fields:            []model.PubSubField{},
		}
		if msg != nil {
			m.Received = model.FormatTimestamp(msg.Received.UTC())
			m.Topic = msg.Topic
			if msg.Source != nil {
				m.Source = msg.Source.String()
			}
		}
		if ds.Status != 0 {
			m.Status = ds.Status.Symbol()
		}
		if v := ds.MetaDataVersion; v.MajorVersion != 0 || v.MinorVersion != 0 {
			m.MetaDataVersion = fmt.Sprintf("%d.%d", v.MajorVersion, v.MinorVersion)
		}
		if !ds.Valid && enc == "uadp" {
			m.Status = "Invalid"
		}
		md := d.lookup(pub, nm.GroupHeader.WriterGroupID, ds.DataSetWriterID)
		for _, f := range ds.Fields {
			m.Fields = append(m.Fields, fieldModel(f, md, ds.FieldEncoding == message.FieldEncodingDataValue))
		}
		out = append(out, m)
	}
	return out
}

func messageTypeName(t message.DataSetMessageType) string {
	switch t {
	case message.DataSetMessageKeyFrame:
		return "keyframe"
	case message.DataSetMessageDeltaFrame:
		return "deltaframe"
	case message.DataSetMessageEvent:
		return "event"
	case message.DataSetMessageKeepAlive:
		return "keepalive"
	}
	return t.String()
}

func fieldModel(f *message.Field, md *ua.DataSetMetaDataType, dataValue bool) model.PubSubField {
	out := model.PubSubField{Index: f.Index, Name: f.Name}
	if out.Name == "" && md != nil && int(f.Index) < len(md.Fields) {
		out.Name = md.Fields[f.Index].Name
	}
	dv := f.Value
	if dv == nil {
		return out
	}
	if dv.Value != nil {
		out.Value = jsonSafeValue(dv.Value.Value())
		out.Type = parse.BuiltinTypeName(dv.Value.Type())
		if rv := reflect.ValueOf(dv.Value.Value()); rv.Kind() == reflect.Slice && dv.Value.Type() != ua.TypeIDByteString {
			out.Type += "[]"
		}
	}
	if dataValue || dv.Status != ua.StatusOK {
		out.Status = dv.Status.Symbol()
	}
	out.SourceTimestamp = model.FormatTimestamp(dv.SourceTimestamp)
	return out
}

// jsonSafeValue makes values without a useful JSON form readable.
func jsonSafeValue(v interface{}) interface{} {
	switch x := v.(type) {
	case *ua.NodeID:
		return x.String()
	case *ua.ExpandedNodeID:
		return x.String()
	case *ua.GUID:
		return x.String()
	case *ua.QualifiedName:
		return x.Name
	case *ua.LocalizedText:
		return x.Text
	case ua.StatusCode:
		return x.Symbol()
	case time.Time:
		return model.FormatTimestamp(x)
	}
	return v
}

func metaDataModel(msg *transport.Message, enc string, p message.PublisherID, writerID uint16, md *ua.DataSetMetaDataType) model.PubSubMetaData {
	out := model.PubSubMetaData{
		Encoding:        enc,
		PublisherID:     p.String(),
		DataSetWriterID: writerID,
		Name:            md.Name,
		Fields:          []model.PubSubFieldMeta{},
	}
	if msg != nil {
		out.Received = model.FormatTimestamp(msg.Received.UTC())
		out.Topic = msg.Topic
		if msg.Source != nil {
			out.Source = msg.Source.String()
		}
	}
	if v := md.ConfigurationVersion; v != nil {
		out.Version = fmt.Sprintf("%d.%d", v.MajorVersion, v.MinorVersion)
	}
	for _, f := range md.Fields {
		typ := parse.BuiltinTypeName(ua.TypeID(f.BuiltInType))
		if f.ValueRank >= 1 {
			typ += "[]"
		}
		out.Fields = append(out.Fields, model.PubSubFieldMeta{Name: f.Name, Type: typ, ValueRank: f.ValueRank})
	}
	return out
}

// ---------------------------------------------------------------------------
// Output.

// dataSetLine is the table form of a DataSetMessage.
func dataSetLine(ds model.PubSubDataSet) string {
	var b strings.Builder
	ts := ds.Timestamp
	if ts == "" {
		ts = ds.Received
	}
	if ts != "" {
		b.WriteString(ts)
		b.WriteString("  ")
	}
	fmt.Fprintf(&b, "pub=%s", ds.PublisherID)
	if ds.WriterGroupID != 0 {
		fmt.Fprintf(&b, " wg=%d", ds.WriterGroupID)
	} else if ds.WriterGroupName != "" {
		fmt.Fprintf(&b, " wg=%s", ds.WriterGroupName)
	}
	fmt.Fprintf(&b, " writer=%d seq=%d %s", ds.DataSetWriterID, ds.SequenceNumber, ds.MessageType)
	if ds.Security != "None" {
		fmt.Fprintf(&b, " [%s token=%d]", ds.Security, ds.SecurityTokenID)
	}
	if ds.Status != "" && ds.Status != "OK" {
		fmt.Fprintf(&b, " [%s]", ds.Status)
	}
	if ds.Topic != "" {
		fmt.Fprintf(&b, " topic=%s", ds.Topic)
	}
	b.WriteString(" ")
	for _, f := range ds.Fields {
		name := f.Name
		if name == "" {
			name = fmt.Sprintf("#%d", f.Index)
		}
		fmt.Fprintf(&b, " %s=%s", name, format.FormatValue(f.Value))
		if f.Status != "" && f.Status != "OK" {
			fmt.Fprintf(&b, "[%s]", f.Status)
		}
	}
	return b.String()
}

func metaDataLine(md model.PubSubMetaData) string {
	fields := make([]string, len(md.Fields))
	for i, f := range md.Fields {
		fields[i] = f.Name + ":" + f.Type
	}
	return fmt.Sprintf("metadata pub=%s writer=%d %q version=%s fields: %s",
		md.PublisherID, md.DataSetWriterID, md.Name, md.Version, strings.Join(fields, ", "))
}

func statusLine(st map[string]interface{}) string {
	keys := make([]string, 0, len(st))
	for k := range st {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%s=%v", k, st[k])
	}
	return "status " + strings.Join(parts, " ")
}

// transportProfile returns the TransportProfileUri for a URL and mapping.
func transportProfile(url string, enc parse.PubSubEncoding) (string, error) {
	scheme, err := transport.Scheme(url)
	if err != nil {
		return "", err
	}
	switch scheme {
	case "opc.udp":
		if enc == parse.EncodingJSON {
			return "", errors.New("the JSON mapping is not available over UDP: use uadp, or an MQTT broker")
		}
		return transport.ProfileUDPUADP, nil
	case "mqtt", "mqtts":
		if enc == parse.EncodingJSON {
			return transport.ProfileMQTTJSON, nil
		}
		return transport.ProfileMQTTUADP, nil
	}
	return "", fmt.Errorf("unsupported transport %q (opc.udp, mqtt, mqtts)", scheme)
}

// pubsubTransportFlags configure the transport connection.
type pubsubTransportFlags struct {
	url          string
	iface        string
	mqttUser     string
	mqttPassword string
	mqttClientID string
	qos          uint8
}

func (f *pubsubTransportFlags) register(fs *pflag.FlagSet) {
	fs.StringVar(&f.url, "url", "", "Transport address: opc.udp://host:port (unicast or multicast group), mqtt://host:port or mqtts://host:port")
	fs.StringVar(&f.iface, "interface", "", "Network interface for UDP multicast (default: system choice)")
	fs.StringVar(&f.mqttUser, "mqtt-username", "", "MQTT broker username")
	fs.StringVar(&f.mqttPassword, "mqtt-password", "", "MQTT broker password")
	fs.StringVar(&f.mqttClientID, "mqtt-client-id", "", "MQTT client id (default: random)")
	fs.Uint8Var(&f.qos, "qos", 0, "MQTT quality of service (0 or 1)")
}

func (f *pubsubTransportFlags) open(ctx context.Context, enc parse.PubSubEncoding) (transport.Connection, error) {
	if f.url == "" {
		return nil, exitCodef(1, "--url is required (opc.udp://..., mqtt://... or mqtts://...)")
	}
	if f.qos > 1 {
		return nil, exitCodef(1, "--qos must be 0 or 1")
	}
	profile, err := transportProfile(f.url, enc)
	if err != nil {
		return nil, exitCode(1, err)
	}
	if f.mqttPassword == "" {
		f.mqttPassword = os.Getenv("OPCUACTL_MQTT_PASSWORD")
	}
	openCtx, cancel := connCfg.TimeoutContext(ctx)
	defer cancel()
	c, err := transport.Open(openCtx, transport.Config{
		ProfileURI: profile,
		URL:        f.url,
		Interface:  f.iface,
		ClientID:   f.mqttClientID,
		Username:   f.mqttUser,
		Password:   f.mqttPassword,
	})
	if err != nil {
		return nil, exitCode(2, err)
	}
	return c, nil
}
