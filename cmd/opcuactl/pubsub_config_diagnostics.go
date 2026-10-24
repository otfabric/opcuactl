// SPDX-License-Identifier: MIT

package opcuactl

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/otfabric/go-opcua"
	"github.com/otfabric/go-opcua/id"
	"github.com/otfabric/go-opcua/pubsub/remote"
	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/opcuactl/internal/format"
	"github.com/otfabric/opcuactl/internal/model"
	"github.com/spf13/cobra"
)

func newPubSubConfigDiagnosticsCmd() *cobra.Command {
	var node string
	var reset bool
	cmd := &cobra.Command{
		Use:   "diagnostics",
		Short: "Read the PubSub diagnostics of a component (Part 14 §9.1.11)",
		Long: `Read the Diagnostics object of a PubSub component of the server: of
PublishSubscribe (default), or of the connection, writer group, reader group,
DataSetWriter or DataSetReader --node. It shows the DiagnosticsLevel, the
totals, SubError, every counter (state changes by cause,
SentNetworkMessages, FailedTransmissions, EncryptionErrors,
ReceivedNetworkMessages, ReceivedInvalidNetworkMessages, DecryptionErrors,
FailedDataSetMessages) with its classification, level and first change, and
the LiveValues. Counters above the DiagnosticsLevel of the object are not
active. --reset calls the Reset method first.

Examples:
  opcuactl pubsub config diagnostics -e opc.tcp://localhost:4840
  opcuactl pubsub config diagnostics -e opc.tcp://localhost:4840 --node "ns=1;i=50010" --format json
  opcuactl pubsub config diagnostics -e opc.tcp://localhost:4840 --node "ns=1;i=50010" --reset`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			comp := remote.PublishSubscribe
			if node != "" {
				nid, err := requireNode("node", node)
				if err != nil {
					return err
				}
				comp = nid
			}
			return withRemote(func(ctx context.Context, c *opcua.Client, _ *remote.Client) error {
				m, err := readPubSubDiagnostics(ctx, c, comp, reset)
				if err != nil {
					return err
				}
				return writeConfigDiagnostics(m)
			})
		},
	}
	cmd.Flags().StringVar(&node, "node", "", "NodeId of the component (default: the PublishSubscribe object)")
	cmd.Flags().BoolVar(&reset, "reset", false, "Reset the counters first (the Reset method)")
	return cmd
}

// browseChildren returns the hierarchical children of a node by browse
// name.
func browseChildren(ctx context.Context, c *opcua.Client, nid *ua.NodeID) (map[string]*ua.NodeID, error) {
	refs, err := c.Node(nid).References(ctx, id.HierarchicalReferences, ua.BrowseDirectionForward, ua.NodeClassAll, true)
	if err != nil {
		return nil, err
	}
	out := make(map[string]*ua.NodeID, len(refs))
	for _, r := range refs {
		if r.BrowseName != nil && r.NodeID != nil {
			out[r.BrowseName.Name] = r.NodeID.NodeID
		}
	}
	return out, nil
}

// readPubSubDiagnostics reads the Diagnostics object of a component.
func readPubSubDiagnostics(ctx context.Context, c *opcua.Client, comp *ua.NodeID, reset bool) (model.PubSubDiagnostics, error) {
	out := model.PubSubDiagnostics{NodeID: comp.String(), Counters: []model.PubSubCounter{}}
	if bn, err := c.Node(comp).BrowseName(ctx); err == nil && bn != nil {
		out.Name = bn.Name
	}
	kids, err := browseChildren(ctx, c, comp)
	if err != nil {
		return out, fmt.Errorf("browsing %s: %w", comp, err)
	}
	diag := kids["Diagnostics"]
	if diag == nil {
		return out, fmt.Errorf("%s has no Diagnostics object", comp)
	}
	if typ, err := c.Node(diag).TypeDefinition(ctx); err == nil && typ != nil {
		out.Component = diagnosticsComponent(typ)
	}
	parts, err := browseChildren(ctx, c, diag)
	if err != nil {
		return out, fmt.Errorf("browsing %s: %w", diag, err)
	}
	if reset {
		m := parts["Reset"]
		if m == nil {
			return out, fmt.Errorf("%s has no Reset method", diag)
		}
		res, err := c.Call(ctx, &ua.CallMethodRequest{ObjectID: diag, MethodID: m})
		if err == nil && res.StatusCode != ua.StatusOK {
			err = res.StatusCode
		}
		if err != nil {
			return out, fmt.Errorf("calling Reset: %w", err)
		}
	}

	// Everything is read in one request: the values of the object, of the
	// counters and their properties, and of the LiveValues.
	var reads []*ua.ReadValueID
	read := func(nid *ua.NodeID) int {
		if nid == nil {
			return -1
		}
		reads = append(reads, &ua.ReadValueID{NodeID: nid, AttributeID: ua.AttributeIDValue})
		return len(reads) - 1
	}
	level, totalInfo, totalErr, subErr := read(parts["DiagnosticsLevel"]), read(parts["TotalInformation"]), read(parts["TotalError"]), read(parts["SubError"])
	type counterRead struct {
		name                         string
		value, active, class, lv, ts int
	}
	var counters []counterRead
	if cn := parts["Counters"]; cn != nil {
		vars, err := browseChildren(ctx, c, cn)
		if err != nil {
			return out, fmt.Errorf("browsing the counters: %w", err)
		}
		for _, name := range sortedKeys(vars) {
			props, err := browseChildren(ctx, c, vars[name])
			if err != nil {
				return out, fmt.Errorf("browsing counter %s: %w", name, err)
			}
			counters = append(counters, counterRead{name: name, value: read(vars[name]), active: read(props["Active"]),
				class: read(props["Classification"]), lv: read(props["DiagnosticsLevel"]), ts: read(props["TimeFirstChange"])})
		}
	}
	live := map[string]int{}
	if ln := parts["LiveValues"]; ln != nil {
		vars, err := browseChildren(ctx, c, ln)
		if err != nil {
			return out, fmt.Errorf("browsing the LiveValues: %w", err)
		}
		for name, nid := range vars {
			live[name] = read(nid)
		}
	}
	if len(reads) == 0 {
		return out, fmt.Errorf("%s is empty", diag)
	}
	resp, err := c.Read(ctx, &ua.ReadRequest{NodesToRead: reads, TimestampsToReturn: ua.TimestampsToReturnNeither})
	if err != nil {
		return out, err
	}
	value := func(i int) (interface{}, bool) {
		if i < 0 || i >= len(resp.Results) || resp.Results[i] == nil || resp.Results[i].Status != ua.StatusOK || resp.Results[i].Value == nil {
			return nil, false
		}
		return resp.Results[i].Value.Value(), true
	}
	if v, ok := value(level); ok {
		out.Level = diagnosticsLevelName(ua.DiagnosticsLevel(toInt(v)))
	}
	if v, ok := value(totalInfo); ok {
		out.TotalInformation = uint64(toInt(v))
	}
	if v, ok := value(totalErr); ok {
		out.TotalError = uint64(toInt(v))
	}
	if v, ok := value(subErr); ok {
		out.SubError, _ = v.(bool)
	}
	for _, cr := range counters {
		m := model.PubSubCounter{Name: cr.name, Classification: "Information"}
		if v, ok := value(cr.value); ok {
			m.Value = uint64(toInt(v))
		}
		if v, ok := value(cr.active); ok {
			m.Active, _ = v.(bool)
		}
		if v, ok := value(cr.class); ok && ua.PubSubDiagnosticsCounterClassification(toInt(v)) == ua.PubSubDiagnosticsCounterClassificationError {
			m.Classification = "Error"
		}
		if v, ok := value(cr.lv); ok {
			m.Level = diagnosticsLevelName(ua.DiagnosticsLevel(toInt(v)))
		}
		if v, ok := value(cr.ts); ok {
			if t, ok := v.(time.Time); ok && !t.IsZero() {
				m.TimeFirstChange = model.FormatTimestamp(t)
			}
		}
		out.Counters = append(out.Counters, m)
	}
	for name, i := range live {
		if v, ok := value(i); ok {
			if out.LiveValues == nil {
				out.LiveValues = map[string]interface{}{}
			}
			out.LiveValues[name] = jsonSafeValue(v)
		}
	}
	return out, nil
}

// diagnosticsComponent names the component of a diagnostics object type.
func diagnosticsComponent(typ *ua.NodeID) string {
	if typ.Namespace() == 0 {
		switch typ.IntID() {
		case id.PubSubDiagnosticsRootType:
			return "PublishSubscribe"
		case id.PubSubDiagnosticsConnectionType:
			return "connection"
		case id.PubSubDiagnosticsWriterGroupType:
			return "writer-group"
		case id.PubSubDiagnosticsReaderGroupType:
			return "reader-group"
		case id.PubSubDiagnosticsDataSetWriterType:
			return "writer"
		case id.PubSubDiagnosticsDataSetReaderType:
			return "reader"
		}
	}
	return "component"
}

func toInt(v interface{}) int64 {
	switch x := v.(type) {
	case int32:
		return int64(x)
	case uint32:
		return int64(x)
	case uint16:
		return int64(x)
	case int64:
		return x
	case uint64:
		return int64(x)
	}
	return 0
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func writeConfigDiagnostics(m model.PubSubDiagnostics) error {
	if format.ParseFormat(outputFormat) != format.Table {
		return newWriter().Write(m)
	}
	sub := ""
	if m.SubError {
		sub = " [SubError]"
	}
	fmt.Printf("diagnostics %s %q %s (level %s): information=%d error=%d%s\n",
		m.Component, m.Name, m.NodeID, m.Level, m.TotalInformation, m.TotalError, sub)
	rows := make([][]string, len(m.Counters))
	for i, c := range m.Counters {
		value := strconv.FormatUint(c.Value, 10)
		if !c.Active {
			value = "-"
		}
		rows[i] = []string{c.Name, value, c.Classification, c.Level, strconv.FormatBool(c.Active), c.TimeFirstChange}
	}
	if err := newWriter().WriteTable([]string{"COUNTER", "VALUE", "CLASSIFICATION", "LEVEL", "ACTIVE", "FIRST CHANGE"}, rows); err != nil {
		return err
	}
	if len(m.LiveValues) == 0 {
		return nil
	}
	fmt.Println()
	var live [][]string
	for _, k := range sortedKeys(m.LiveValues) {
		live = append(live, []string{k, format.FormatValue(m.LiveValues[k])})
	}
	return newWriter().WriteTable([]string{"LIVE VALUE", "VALUE"}, live)
}
