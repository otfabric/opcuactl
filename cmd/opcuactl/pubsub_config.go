// SPDX-License-Identifier: MIT

package opcuactl

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/otfabric/go-opcua"
	"github.com/otfabric/go-opcua/pubsub"
	"github.com/otfabric/go-opcua/pubsub/remote"
	"github.com/otfabric/go-opcua/pubsub/transport"
	mqtttransport "github.com/otfabric/go-opcua/pubsub/transport/mqtt"
	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/opcuactl/internal/format"
	"github.com/otfabric/opcuactl/internal/model"
	"github.com/otfabric/opcuactl/internal/parse"
	"github.com/spf13/cobra"
)

var pubsubRemoveKinds = []string{"connection", "group", "writer", "reader", "dataset"}

func newPubSubConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Show and change the PubSub configuration of a server",
		Long: `Show and change the PublishSubscribe configuration of a server (Part 14 §9)
through the methods of its PublishSubscribe object (i=14443): connections,
writer and reader groups, DataSetWriters and DataSetReaders, published data
sets, and Enable / Disable of every component.

Works with every server that implements the PubSub information model
(go-opcua with server.WithPubSub, open62541). Components are addressed by
their NodeId, as printed by "pubsub config show" and by the add commands.

Examples:
  opcuactl pubsub config show -e opc.tcp://localhost:4840
  opcuactl pubsub config add-connection -e opc.tcp://localhost:4840 --name UDP --url opc.udp://224.0.0.22:4840 --publisher-id 2234
  opcuactl pubsub config add-dataset -e opc.tcp://localhost:4840 --name Plant --var Temp="ns=1;s=Temp"
  opcuactl pubsub config add-writer-group -e opc.tcp://localhost:4840 --connection "ns=1;i=50000" --name G --writer-group-id 100 --interval 500ms
  opcuactl pubsub config add-writer -e opc.tcp://localhost:4840 --group "ns=1;i=50010" --name W --writer-id 1 --dataset Plant
  opcuactl pubsub config disable -e opc.tcp://localhost:4840 --node "ns=1;i=50010"
  opcuactl pubsub config remove connection -e opc.tcp://localhost:4840 --node "ns=1;i=50000"
  opcuactl pubsub config add-action -e opc.tcp://localhost:4840 --name Calc --request A:Int32 --response R:Int32 --target 1:Run
  opcuactl pubsub config export -e opc.tcp://localhost:4840

export, reserve-ids and add-action use the PubSubConfiguration file of the
server (Part 14 §9.1.3.7; go-opcua v1.11.0 and later).`,
	}
	cmd.AddCommand(
		newPubSubConfigShowCmd(),
		newPubSubAddConnectionCmd(),
		newPubSubAddDataSetCmd(),
		newPubSubAddWriterGroupCmd(),
		newPubSubAddWriterCmd(),
		newPubSubAddReaderGroupCmd(),
		newPubSubAddReaderCmd(),
		newPubSubRemoveCmd(),
		newPubSubEnableCmd(true),
		newPubSubEnableCmd(false),
		newPubSubConfigDiagnosticsCmd(),
		newPubSubExportCmd(),
		newPubSubReserveIDsCmd(),
		newPubSubAddActionCmd(),
	)
	return cmd
}

// withRemote connects to --endpoint and runs fn with a PubSub configuration
// client. Errors of fn are reported with exit code 3.
func withRemote(fn func(ctx context.Context, c *opcua.Client, r *remote.Client) error) error {
	if err := connCfg.RequireEndpoint(); err != nil {
		return exitCode(1, err)
	}
	ctx := context.Background()
	c, err := connCfg.NewClient(ctx)
	if err != nil {
		return exitCode(2, err)
	}
	defer func() { _ = c.Close(ctx) }()
	opCtx, cancel := connCfg.TimeoutContext(ctx)
	defer cancel()
	if err := fn(opCtx, c, remote.New(c)); err != nil {
		if _, ok := err.(*exitError); ok {
			return err
		}
		return exitCode(3, err)
	}
	return nil
}

func requireNode(flag, value string) (*ua.NodeID, error) {
	if value == "" {
		return nil, exitCodef(1, "--%s is required", flag)
	}
	nid, err := parse.NodeIDWithNamespace(value, namespace)
	if err != nil {
		return nil, exitCodef(1, "--%s: %v", flag, err)
	}
	return nid, nil
}

func stateName(s ua.PubSubState) string { return strings.TrimPrefix(s.String(), "PubSubState") }

// ---------------------------------------------------------------------------
// show

func newPubSubConfigShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show the PubSub components of a server",
		Long: `List the PubSub configuration of a server: connections with their writer
groups, DataSetWriters, reader groups and DataSetReaders, and the published
data sets, with NodeIds, states and the main properties.

Examples:
  opcuactl pubsub config show -e opc.tcp://localhost:4840
  opcuactl pubsub config show -e opc.tcp://localhost:4840 --format json --pretty`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return withRemote(func(ctx context.Context, _ *opcua.Client, r *remote.Client) error {
				root, err := pubsubTree(ctx, r)
				if err != nil {
					return err
				}
				if format.ParseFormat(outputFormat) != format.Table {
					return newWriter().Write(root)
				}
				var rows [][]string
				var walk func(c model.PubSubComponent, depth int)
				walk = func(c model.PubSubComponent, depth int) {
					rows = append(rows, []string{
						strings.Repeat("  ", depth) + c.Kind, c.Name, c.NodeID, c.State, propertiesString(c.Properties),
					})
					for _, ch := range c.Children {
						walk(ch, depth+1)
					}
				}
				walk(root, 0)
				return newWriter().WriteTable([]string{"KIND", "NAME", "NODEID", "STATE", "DETAILS"}, rows)
			})
		},
	}
}

func propertiesString(p map[string]string) string {
	keys := make([]string, 0, len(p))
	for k := range p {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + "=" + p[k]
	}
	return strings.Join(parts, " ")
}

// pubsubTree reads the PubSub components of a server.
func pubsubTree(ctx context.Context, r *remote.Client) (model.PubSubComponent, error) {
	root := model.PubSubComponent{Kind: "PublishSubscribe", Name: "PublishSubscribe", NodeID: remote.PublishSubscribe.String()}
	st, err := r.State(ctx, nil)
	if err != nil {
		return root, fmt.Errorf("reading the PublishSubscribe state: %w", err)
	}
	root.State = stateName(st)

	component := func(kind string, c remote.Component, props ...string) model.PubSubComponent {
		m := model.PubSubComponent{Kind: kind, Name: c.Name, NodeID: c.NodeID.String()}
		if s, err := r.State(ctx, c.NodeID); err == nil {
			m.State = stateName(s)
		}
		for _, p := range props {
			v, err := r.Property(ctx, c.NodeID, p)
			if err != nil || v == nil || v.Value() == nil {
				continue
			}
			s := propertyString(p, v)
			if s == "" {
				continue
			}
			if m.Properties == nil {
				m.Properties = map[string]string{}
			}
			m.Properties[p] = s
		}
		return m
	}

	conns, err := r.Connections(ctx)
	if err != nil {
		return root, fmt.Errorf("listing connections: %w", err)
	}
	for _, conn := range conns {
		cm := component("connection", conn, "PublisherId", "TransportProfileUri")
		wgs, err := r.WriterGroups(ctx, conn.NodeID)
		if err != nil {
			return root, fmt.Errorf("listing the writer groups of %s: %w", conn.Name, err)
		}
		for _, wg := range wgs {
			wm := component("writer-group", wg, "WriterGroupId", "PublishingInterval", "SecurityMode", "SecurityGroupId")
			writers, err := r.DataSetWriters(ctx, wg.NodeID)
			if err != nil {
				return root, fmt.Errorf("listing the writers of %s: %w", wg.Name, err)
			}
			for _, w := range writers {
				wm.Children = append(wm.Children, component("writer", w, "DataSetWriterId", "KeyFrameCount"))
			}
			cm.Children = append(cm.Children, wm)
		}
		rgs, err := r.ReaderGroups(ctx, conn.NodeID)
		if err != nil {
			return root, fmt.Errorf("listing the reader groups of %s: %w", conn.Name, err)
		}
		for _, rg := range rgs {
			rm := component("reader-group", rg, "SecurityMode", "SecurityGroupId")
			readers, err := r.DataSetReaders(ctx, rg.NodeID)
			if err != nil {
				return root, fmt.Errorf("listing the readers of %s: %w", rg.Name, err)
			}
			for _, rd := range readers {
				rm.Children = append(rm.Children, component("reader", rd, "PublisherId", "WriterGroupId", "DataSetWriterId", "DataSetMetaData"))
			}
			cm.Children = append(cm.Children, rm)
		}
		root.Children = append(root.Children, cm)
	}

	pds, err := r.PublishedDataSets(ctx)
	if err != nil {
		return root, fmt.Errorf("listing the published data sets: %w", err)
	}
	for _, p := range pds {
		m := component("dataset", p, "DataSetMetaData")
		m.State = ""
		root.Children = append(root.Children, m)
	}
	return root, nil
}

func propertyString(name string, v *ua.Variant) string {
	switch x := v.Value().(type) {
	case *ua.ExtensionObject:
		if md, ok := x.Value.(*ua.DataSetMetaDataType); ok {
			fields := make([]string, len(md.Fields))
			for i, f := range md.Fields {
				fields[i] = f.Name + ":" + parse.BuiltinTypeName(ua.TypeID(f.BuiltInType))
			}
			return strings.Join(fields, ",")
		}
	case string:
		if name == "TransportProfileUri" {
			return x[strings.LastIndex(x, "/")+1:]
		}
	case int32:
		if name == "SecurityMode" {
			return strings.TrimPrefix(ua.MessageSecurityMode(x).String(), "MessageSecurityMode")
		}
	case float64:
		if name == "PublishingInterval" {
			return (time.Duration(x * float64(time.Millisecond))).String()
		}
	}
	return fmt.Sprint(v.Value())
}

// ---------------------------------------------------------------------------
// add

func writeAdded(kind, name string, nid *ua.NodeID, results []model.PubSubResult) error {
	out := model.PubSubAdded{Kind: kind, Name: name, NodeID: nid.String(), Results: results}
	if quiet {
		fmt.Println(out.NodeID)
		return nil
	}
	if format.ParseFormat(outputFormat) != format.Table {
		return newWriter().Write(out)
	}
	fmt.Printf("added %s %q: %s\n", kind, name, out.NodeID)
	if len(results) > 0 {
		rows := make([][]string, len(results))
		for i, r := range results {
			rows[i] = []string{r.Field, r.NodeID, r.Status}
		}
		return newWriter().WriteTable([]string{"FIELD", "NODEID", "STATUS"}, rows)
	}
	return nil
}

func newPubSubAddConnectionCmd() *cobra.Command {
	var name, url, publisherID, encoding, iface, mqttVersion, jsonCompression string
	var props []string
	cmd := &cobra.Command{
		Use:   "add-connection",
		Short: "Add a PubSubConnection (AddConnection)",
		Long: `Add a PubSubConnection to the server. --url selects the transport:
opc.udp://host:port (UADP over UDP), opc.eth://MAC[:VID[.PCP]] (UADP over
Ethernet, with --interface), mqtt://host:port / mqtts://host:port or
amqp://host:port / amqps://host:port (UADP or, with --encoding json, JSON
over an MQTT or AMQP 1.0 broker). --publisher-id is the PublisherId the
server sends with (number = UInt16; byte:N, u32:N, u64:N or a string); leave
it out for a connection that only subscribes.

--mqtt-version sets the MqttVersion connection property (3.1.1, 5.0 or
BestAvailable); --transport-property key=value adds further connection
properties (KeyValuePairs), such as the MQTT 5.0 "connection-Session Expiry
Interval" or the AMQP "connection-hostname" and "target-capabilities".
--json-compression gzip makes a JSON connection over MQTT 5.0 send its
messages gzip compressed (application/json+gzip, Part 14 §7.3.4.9.1; the
go-opcua connection property JsonCompression).

Examples:
  opcuactl pubsub config add-connection -e opc.tcp://localhost:4840 --name UDP --url opc.udp://224.0.0.22:4840 --publisher-id 2234
  opcuactl pubsub config add-connection -e opc.tcp://localhost:4840 --name Broker --url mqtt://broker:1883 --encoding json --publisher-id str:line1
  opcuactl pubsub config add-connection -e opc.tcp://localhost:4840 --name MQTT5 --url mqtt://broker:1883 --mqtt-version 5.0 --publisher-id 7
  opcuactl pubsub config add-connection -e opc.tcp://localhost:4840 --name AMQP --url amqp://rabbitmq:5672 --publisher-id 7 \
      --transport-property connection-hostname=vhost:plant
  opcuactl pubsub config add-connection -e opc.tcp://localhost:4840 --name ETH --url opc.eth://01-00-5E-00-00-01 --interface eth0 --publisher-id 7`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if name == "" || url == "" {
				return exitCodef(1, "--name and --url are required")
			}
			enc, err := parse.ParsePubSubEncoding(encoding, false)
			if err != nil {
				return exitCode(1, err)
			}
			if _, err := transportProfile(url, enc); err != nil {
				return exitCode(1, err)
			}
			scheme, _ := transport.Scheme(url)
			if scheme == "opc.eth" && iface == "" {
				return exitCodef(1, "--interface is required with opc.eth:// (the network interface of the server)")
			}
			if mqttVersion != "" && !isMQTTScheme(scheme) {
				return exitCodef(1, "--mqtt-version needs an MQTT broker (mqtt://, mqtts://)")
			}
			version, err := parse.MQTTVersion(mqttVersion)
			if err != nil {
				return exitCode(1, err)
			}
			compress, err := checkJSONCompression(jsonCompression, enc, url)
			if err != nil {
				return err
			}
			properties, err := parse.TransportProperties(props)
			if err != nil {
				return exitCode(1, err)
			}
			var pid interface{}
			if publisherID != "" {
				p, err := parse.PublisherID(publisherID)
				if err != nil {
					return exitCode(1, err)
				}
				pid = parse.PublisherIDValue(p)
			}
			mapping := pubsub.MappingUADP
			if enc == parse.EncodingJSON {
				mapping = pubsub.MappingJSON
			}
			var cfg *ua.PubSubConnectionDataType
			switch scheme {
			case "mqtt", "mqtts":
				cfg = pubsub.MQTTConnection(name, url, pid, mapping)
				if mqttVersion != "" {
					pubsub.SetMQTTVersion(cfg, version)
				}
			case "amqp", "amqps":
				cfg = pubsub.AMQPConnection(name, url, pid, mapping)
			case "opc.eth":
				cfg = pubsub.EthernetConnection(name, url, iface, pid)
			default:
				cfg = pubsub.UDPConnection(name, url, pid)
				if iface != "" {
					cfg.Address = ua.NewExtensionObject(&ua.NetworkAddressURLDataType{URL: url, NetworkInterface: iface})
				}
			}
			keys := make([]string, 0, len(properties))
			for k := range properties {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				pubsub.SetConnectionProperty(cfg, k, properties[k])
			}
			if compress {
				pubsub.SetJSONCompression(cfg, mqtttransport.CompressionGzip)
			}
			return withRemote(func(ctx context.Context, _ *opcua.Client, r *remote.Client) error {
				nid, err := r.AddConnection(ctx, cfg)
				if err != nil {
					return err
				}
				return writeAdded("connection", name, nid, nil)
			})
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Connection name")
	cmd.Flags().StringVar(&url, "url", "", "Transport address ("+pubsubSchemes+")")
	cmd.Flags().StringVar(&publisherID, "publisher-id", "", "PublisherId of the connection")
	cmd.Flags().StringVar(&encoding, "encoding", "uadp", "Message mapping over a broker (uadp|json)")
	cmd.Flags().StringVar(&iface, "interface", "", "Network interface (NetworkInterface; required for opc.eth://)")
	cmd.Flags().StringVar(&mqttVersion, "mqtt-version", "", "MQTT protocol version (3.1.1|5.0|BestAvailable)")
	cmd.Flags().StringArrayVar(&props, "transport-property", nil, "Connection property key=value (repeatable)")
	cmd.Flags().StringVar(&jsonCompression, "json-compression", "", "Compress JSON messages over MQTT 5.0 (none|gzip): the JsonCompression connection property")
	_ = cmd.RegisterFlagCompletionFunc("mqtt-version", fixedCompletion(pubsubMQTTVersionValues))
	_ = cmd.RegisterFlagCompletionFunc("json-compression", fixedCompletion(pubsubJSONCompressionValues))
	return cmd
}

func newPubSubAddDataSetCmd() *cobra.Command {
	var name string
	var vars []string
	cmd := &cobra.Command{
		Use:   "add-dataset",
		Short: "Add a published data set of variables (AddPublishedDataItems)",
		Long: `Add a PublishedDataItems data set: every --var Field=NodeId publishes the
Value of a variable as the field Field. The server reports a status per
variable; variables with a Bad status are not part of the data set.

Example:
  opcuactl pubsub config add-dataset -e opc.tcp://localhost:4840 --name Plant \
      --var Temp="ns=1;s=Temp" --var Level="ns=1;s=Level"`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if name == "" || len(vars) == 0 {
				return exitCodef(1, "--name and at least one --var Field=NodeId are required")
			}
			aliases := make([]string, len(vars))
			pvs := make([]*ua.PublishedVariableDataType, len(vars))
			for i, v := range vars {
				alias, node, ok := strings.Cut(v, "=")
				if !ok || alias == "" {
					return exitCodef(1, "invalid --var %q: want Field=NodeId", v)
				}
				nid, err := parse.NodeIDWithNamespace(node, namespace)
				if err != nil {
					return exitCodef(1, "invalid --var %q: %v", v, err)
				}
				aliases[i] = alias
				pvs[i] = &ua.PublishedVariableDataType{PublishedVariable: nid, AttributeID: ua.AttributeIDValue}
			}
			return withRemote(func(ctx context.Context, _ *opcua.Client, r *remote.Client) error {
				res, err := r.AddPublishedDataItems(ctx, name, aliases, nil, pvs)
				if err != nil {
					return err
				}
				results := make([]model.PubSubResult, len(res.AddResults))
				for i, st := range res.AddResults {
					results[i] = model.PubSubResult{Status: st.Symbol(), StatusCode: st.Uint32()}
					if i < len(aliases) {
						results[i].Field = aliases[i]
						results[i].NodeID = pvs[i].PublishedVariable.String()
					}
				}
				return writeAdded("dataset", name, res.DataSetNodeID, results)
			})
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Data set name")
	cmd.Flags().StringArrayVar(&vars, "var", nil, "Published variable Field=NodeId (repeatable)")
	return cmd
}

// groupSecurity applies --message-security / --security-group to a group.
func groupSecurity(mode, group string) (ua.MessageSecurityMode, error) {
	m, err := parse.MessageSecurity(mode)
	if err != nil {
		return 0, exitCode(1, err)
	}
	if m > ua.MessageSecurityModeNone && group == "" {
		return 0, exitCodef(1, "--message-security %s needs --security-group", mode)
	}
	return m, nil
}

func newPubSubAddWriterGroupCmd() *cobra.Command {
	var (
		conn, name, encoding, queue, secMode, secGroup string
		groupID                                        uint16
		interval                                       time.Duration
		keepAlive                                      time.Duration
	)
	cmd := &cobra.Command{
		Use:   "add-writer-group",
		Short: "Add a WriterGroup to a connection (AddWriterGroup)",
		Long: `Add a WriterGroup to the connection --connection. It publishes every
--interval with the UADP (default) or JSON mapping. --queue is the broker queue
of the group. --message-security and --security-group sign / encrypt its UADP
messages with the keys of a SecurityGroup.

Example:
  opcuactl pubsub config add-writer-group -e opc.tcp://localhost:4840 --connection "ns=1;i=50000" \
      --name G --writer-group-id 100 --interval 500ms`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			connID, err := requireNode("connection", conn)
			if err != nil {
				return err
			}
			if name == "" {
				return exitCodef(1, "--name is required")
			}
			enc, err := parse.ParsePubSubEncoding(encoding, false)
			if err != nil {
				return exitCode(1, err)
			}
			mode, err := groupSecurity(secMode, secGroup)
			if err != nil {
				return err
			}
			var cfg *ua.WriterGroupDataType
			if enc == parse.EncodingJSON {
				cfg = pubsub.JSONWriterGroup(name, groupID, interval, queue)
			} else {
				cfg = pubsub.WriterGroup(name, groupID, interval)
				if queue != "" {
					cfg.TransportSettings = ua.NewExtensionObject(&ua.BrokerWriterGroupTransportDataType{
						QueueName:                  queue,
						RequestedDeliveryGuarantee: ua.BrokerTransportQoSAtLeastOnce,
					})
				}
			}
			if keepAlive > 0 {
				cfg.KeepAliveTime = float64(keepAlive) / float64(time.Millisecond)
			}
			if mode > ua.MessageSecurityModeNone {
				pubsub.Secure(cfg, mode, secGroup)
			}
			return withRemote(func(ctx context.Context, _ *opcua.Client, r *remote.Client) error {
				nid, err := r.AddWriterGroup(ctx, connID, cfg)
				if err != nil {
					return err
				}
				return writeAdded("writer-group", name, nid, nil)
			})
		},
	}
	f := cmd.Flags()
	f.StringVar(&conn, "connection", "", "NodeId of the connection")
	f.StringVar(&name, "name", "", "WriterGroup name")
	f.Uint16Var(&groupID, "writer-group-id", 1, "WriterGroupId")
	f.DurationVar(&interval, "interval", time.Second, "Publishing interval")
	f.DurationVar(&keepAlive, "keep-alive", 0, "KeepAliveTime (0 = server default)")
	f.StringVar(&encoding, "encoding", "uadp", "Message mapping (uadp|json)")
	f.StringVar(&queue, "queue", "", "Broker queue of the group (MQTT topic, AMQP address)")
	f.StringVar(&secMode, "message-security", "none", "Message security (none|sign|sign-and-encrypt)")
	f.StringVar(&secGroup, "security-group", "", "SecurityGroupId of the keys")
	_ = cmd.RegisterFlagCompletionFunc("message-security", fixedCompletion(pubsubMessageSecurityValues))
	return cmd
}

func newPubSubAddWriterCmd() *cobra.Command {
	var (
		group, name, dataSet, queue, metaQueue, fieldEncoding string
		writerID                                              uint16
		keyFrames                                             uint32
	)
	cmd := &cobra.Command{
		Use:   "add-writer",
		Short: "Add a DataSetWriter to a WriterGroup (AddDataSetWriter)",
		Long: `Add a DataSetWriter that publishes the published data set --dataset in the
WriterGroup --group. --key-frame-count > 1 sends delta frames between key
frames; --queue and --metadata-queue are its broker queues (MQTT topics, AMQP addresses).

Example:
  opcuactl pubsub config add-writer -e opc.tcp://localhost:4840 --group "ns=1;i=50010" \
      --name W --writer-id 1 --dataset Plant`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			groupID, err := requireNode("group", group)
			if err != nil {
				return err
			}
			if name == "" || dataSet == "" {
				return exitCodef(1, "--name and --dataset are required")
			}
			mask, err := parse.FieldEncoding(fieldEncoding)
			if err != nil {
				return exitCode(1, err)
			}
			cfg := pubsub.DataSetWriter(name, writerID, dataSet)
			cfg.KeyFrameCount = keyFrames
			cfg.DataSetFieldContentMask = mask
			if queue != "" || metaQueue != "" {
				pubsub.SetWriterQueue(cfg, queue, metaQueue)
			}
			return withRemote(func(ctx context.Context, _ *opcua.Client, r *remote.Client) error {
				nid, err := r.AddDataSetWriter(ctx, groupID, cfg)
				if err != nil {
					return err
				}
				return writeAdded("writer", name, nid, nil)
			})
		},
	}
	f := cmd.Flags()
	f.StringVar(&group, "group", "", "NodeId of the WriterGroup")
	f.StringVar(&name, "name", "", "DataSetWriter name")
	f.Uint16Var(&writerID, "writer-id", 1, "DataSetWriterId")
	f.StringVar(&dataSet, "dataset", "", "Name of the published data set")
	f.Uint32Var(&keyFrames, "key-frame-count", 1, "Key frame every this many messages (1 = key frames only)")
	f.StringVar(&fieldEncoding, "field-encoding", "variant", "Field encoding (variant|datavalue|rawdata)")
	f.StringVar(&queue, "queue", "", "Broker queue of the writer (MQTT topic, AMQP address)")
	f.StringVar(&metaQueue, "metadata-queue", "", "Broker queue the DataSetMetaData is announced on")
	_ = cmd.RegisterFlagCompletionFunc("field-encoding", fixedCompletion(pubsubFieldEncodingValues))
	return cmd
}

func newPubSubAddReaderGroupCmd() *cobra.Command {
	var conn, name, secMode, secGroup string
	cmd := &cobra.Command{
		Use:   "add-reader-group",
		Short: "Add a ReaderGroup to a connection (AddReaderGroup)",
		Long: `Add a ReaderGroup to the connection --connection. --message-security and
--security-group make its readers require secured messages and verify them
with the keys of a SecurityGroup.

Example:
  opcuactl pubsub config add-reader-group -e opc.tcp://localhost:4840 --connection "ns=1;i=50000" --name R`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			connID, err := requireNode("connection", conn)
			if err != nil {
				return err
			}
			if name == "" {
				return exitCodef(1, "--name is required")
			}
			mode, err := groupSecurity(secMode, secGroup)
			if err != nil {
				return err
			}
			cfg := pubsub.ReaderGroup(name)
			if mode > ua.MessageSecurityModeNone {
				pubsub.Secure(cfg, mode, secGroup)
			}
			return withRemote(func(ctx context.Context, _ *opcua.Client, r *remote.Client) error {
				nid, err := r.AddReaderGroup(ctx, connID, cfg)
				if err != nil {
					return err
				}
				return writeAdded("reader-group", name, nid, nil)
			})
		},
	}
	f := cmd.Flags()
	f.StringVar(&conn, "connection", "", "NodeId of the connection")
	f.StringVar(&name, "name", "", "ReaderGroup name")
	f.StringVar(&secMode, "message-security", "none", "Message security (none|sign|sign-and-encrypt)")
	f.StringVar(&secGroup, "security-group", "", "SecurityGroupId of the keys")
	_ = cmd.RegisterFlagCompletionFunc("message-security", fixedCompletion(pubsubMessageSecurityValues))
	return cmd
}

func newPubSubAddReaderCmd() *cobra.Command {
	var (
		mf                                         pubsubMetaFlags
		group, name, publisherID, queue, metaQueue string
		groupID, writerID                          uint16
		targets                                    []string
		timeout                                    time.Duration
	)
	cmd := &cobra.Command{
		Use:   "add-reader",
		Short: "Add a DataSetReader to a ReaderGroup (AddDataSetReader)",
		Long: `Add a DataSetReader to the ReaderGroup --group. It reads the DataSets of
--publisher-id / --writer-group-id / --writer-id (leave out or 0: any),
described by the DataSetMetaData of --field, --metadata or --metadata-node.
Every --target NodeId receives the next field of the DataSet (the server
writes the received values to these variables).

Example:
  opcuactl pubsub config add-reader -e opc.tcp://localhost:4840 --group "ns=1;i=50020" --name R \
      --publisher-id 2234 --writer-group-id 100 --writer-id 1 \
      --field Temp:Double --target "ns=1;s=TempCopy"`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			gid, err := requireNode("group", group)
			if err != nil {
				return err
			}
			if name == "" {
				return exitCodef(1, "--name is required")
			}
			var pid interface{}
			if publisherID != "" {
				p, err := parse.PublisherID(publisherID)
				if err != nil {
					return exitCode(1, err)
				}
				pid = parse.PublisherIDValue(p)
			}
			md, err := mf.load(context.Background())
			if err != nil {
				return err
			}
			cfg := pubsub.DataSetReader(name, pid, groupID, writerID, md)
			if timeout > 0 {
				cfg.MessageReceiveTimeout = float64(timeout) / float64(time.Millisecond)
			}
			if len(targets) > 0 {
				tv := make([]*ua.FieldTargetDataType, len(targets))
				for i, t := range targets {
					nid, err := parse.NodeIDWithNamespace(t, namespace)
					if err != nil {
						return exitCodef(1, "--target %q: %v", t, err)
					}
					tv[i] = pubsub.Target(nid)
				}
				pubsub.SetTargetVariables(cfg, tv...)
			}
			if queue != "" || metaQueue != "" {
				pubsub.SetReaderQueue(cfg, queue, metaQueue)
			}
			return withRemote(func(ctx context.Context, _ *opcua.Client, r *remote.Client) error {
				nid, err := r.AddDataSetReader(ctx, gid, cfg)
				if err != nil {
					return err
				}
				return writeAdded("reader", name, nid, nil)
			})
		},
	}
	f := cmd.Flags()
	mf.register(cmd)
	f.StringVar(&group, "group", "", "NodeId of the ReaderGroup")
	f.StringVar(&name, "name", "", "DataSetReader name")
	f.StringVar(&publisherID, "publisher-id", "", "PublisherId to read (number = UInt16; byte:N, u32:N, u64:N or a string)")
	f.Uint16Var(&groupID, "writer-group-id", 0, "WriterGroupId to read (0: any)")
	f.Uint16Var(&writerID, "writer-id", 0, "DataSetWriterId to read (0: any)")
	f.StringArrayVar(&targets, "target", nil, "Target variable NodeId for the next field (repeatable)")
	f.DurationVar(&timeout, "message-receive-timeout", 0, "MessageReceiveTimeout: the reader goes to Error without messages (0 = none)")
	f.StringVar(&queue, "queue", "", "Broker queue to read (MQTT topic, AMQP address)")
	f.StringVar(&metaQueue, "metadata-queue", "", "Broker queue of the DataSetMetaData")
	return cmd
}

// ---------------------------------------------------------------------------
// remove, enable, disable

func newPubSubRemoveCmd() *cobra.Command {
	var node, parent string
	cmd := &cobra.Command{
		Use:       "remove connection|group|writer|reader|dataset",
		Short:     "Remove a PubSub component",
		ValidArgs: pubsubRemoveKinds,
		Long: `Remove a PubSub component, with what it contains:

  connection  RemoveConnection (--node: the connection)
  group       RemoveGroup (--node: a writer or reader group, --parent: its connection)
  writer      RemoveDataSetWriter (--node: the writer, --parent: its WriterGroup)
  reader      RemoveDataSetReader (--node: the reader, --parent: its ReaderGroup)
  dataset     RemovePublishedDataSet (--node: the published data set and its writers)

Examples:
  opcuactl pubsub config remove connection -e opc.tcp://localhost:4840 --node "ns=1;i=50000"
  opcuactl pubsub config remove writer -e opc.tcp://localhost:4840 --node "ns=1;i=50011" --parent "ns=1;i=50010"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			kind := strings.ToLower(args[0])
			nid, err := requireNode("node", node)
			if err != nil {
				return err
			}
			var parentID *ua.NodeID
			switch kind {
			case "group", "writer", "reader":
				if parentID, err = requireNode("parent", parent); err != nil {
					return err
				}
			case "connection", "dataset":
			default:
				return exitCodef(1, "unknown component %q (%s)", args[0], strings.Join(pubsubRemoveKinds, "|"))
			}
			return withRemote(func(ctx context.Context, _ *opcua.Client, r *remote.Client) error {
				var err error
				switch kind {
				case "connection":
					err = r.RemoveConnection(ctx, nid)
				case "group":
					err = r.RemoveGroup(ctx, parentID, nid)
				case "writer":
					err = r.RemoveDataSetWriter(ctx, parentID, nid)
				case "reader":
					err = r.RemoveDataSetReader(ctx, parentID, nid)
				case "dataset":
					err = r.RemovePublishedDataSet(ctx, nid)
				}
				if err != nil {
					return err
				}
				if !quiet {
					fmt.Printf("removed %s %s\n", kind, nid)
				}
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&node, "node", "", "NodeId of the component to remove")
	cmd.Flags().StringVar(&parent, "parent", "", "NodeId of its connection (group) or group (writer, reader)")
	return cmd
}

func newPubSubEnableCmd(enable bool) *cobra.Command {
	var node string
	use, verb := "enable", "Enable"
	if !enable {
		use, verb = "disable", "Disable"
	}
	cmd := &cobra.Command{
		Use:   use,
		Short: verb + " a PubSub component (or the whole PubSub)",
		Long: verb + ` a PubSub component (connection, group, writer or reader) with the ` + verb + `
method of its Status object, or the whole PubSub of the server without --node.
The new state is printed.

Examples:
  opcuactl pubsub config ` + use + ` -e opc.tcp://localhost:4840 --node "ns=1;i=50010"
  opcuactl pubsub config ` + use + ` -e opc.tcp://localhost:4840`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var nid *ua.NodeID
			if node != "" {
				var err error
				if nid, err = requireNode("node", node); err != nil {
					return err
				}
			}
			return withRemote(func(ctx context.Context, _ *opcua.Client, r *remote.Client) error {
				var err error
				if enable {
					err = r.Enable(ctx, nid)
				} else {
					err = r.Disable(ctx, nid)
				}
				if err != nil {
					return err
				}
				st, err := r.State(ctx, nid)
				if err != nil {
					return err
				}
				target := remote.PublishSubscribe.String()
				if nid != nil {
					target = nid.String()
				}
				if quiet {
					fmt.Println(stateName(st))
					return nil
				}
				if format.ParseFormat(outputFormat) != format.Table {
					return newWriter().Write(map[string]string{"nodeId": target, "state": stateName(st)})
				}
				fmt.Printf("%s: %s\n", target, stateName(st))
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&node, "node", "", "NodeId of the component (default: the PublishSubscribe object)")
	return cmd
}
