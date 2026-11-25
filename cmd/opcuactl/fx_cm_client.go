// SPDX-License-Identifier: MIT

package opcuactl

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/otfabric/go-opcua"
	"github.com/otfabric/go-opcua/id"
	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/go-opcua/uafx"
	"github.com/otfabric/go-opcua/uafx/fxcmid"
	"github.com/otfabric/opcuactl/internal/model"
)

// Bounds of what fx cm reads from a server, so that a large or hostile
// address space cannot make it read without end. Variables so that tests
// can lower them.
var (
	cmMaxSets      = 10000    // ConnectionConfigurationSets listed
	cmMaxDepth     = 8        // Folder levels below ConnectionConfigurationSets
	cmMaxTypeDepth = 16       // supertypes followed to recognise a set type
	cmMaxFileSize  = 64 << 20 // bytes read from ConnectionManagerConfiguration
	cmChunk        = 64 << 10 // bytes per Read or Write call of the file
)

// errNoCM reports that the server has no ConnectionManager.
var errNoCM = errors.New("the server has no UAFX ConnectionManager (FxRoot.ConnectionManager, Part 81 §12.3)")

// cmClient works with the ConnectionManager of a server (Part 81 §6.7):
// it finds its ConnectionConfigurationSets and its file by browsing, so it
// does not depend on how a server names its NodeIds.
type cmClient struct {
	c          *opcua.Client
	namespaces []string
	ns         uafx.NamespaceIndexes
	cm         *ua.NodeID
	types      map[string]bool // TypeDefinition -> is a ConnectionConfigurationSetType
}

// newCMClient finds the ConnectionManager of the server.
func newCMClient(ctx context.Context, c *opcua.Client) (*cmClient, error) {
	nsa, err := c.NamespaceArray(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading the NamespaceArray: %w", err)
	}
	x := &cmClient{c: c, namespaces: nsa, types: map[string]bool{}}
	for i, u := range nsa {
		if i > 0xffff {
			break
		}
		switch u {
		case "http://opcfoundation.org/UA/FX/Data/":
			x.ns.Data = uint16(i)
		case "http://opcfoundation.org/UA/FX/AC/":
			x.ns.AC = uint16(i)
		case fxcmid.NamespaceURI:
			x.ns.CM = uint16(i)
		}
	}
	if x.ns.CM == 0 {
		return nil, errNoCM
	}
	x.cm = ua.NewNumericNodeID(x.ns.CM, fxcmid.FxRootConnectionManager)
	if _, err := c.Node(x.cm).BrowseName(ctx); err != nil {
		return nil, fmt.Errorf("%w: %w", errNoCM, err)
	}
	return x, nil
}

// cmSet is a ConnectionConfigurationSet Object: its path below
// ConnectionConfigurationSets, Folders and Name joined by "/".
type cmSet struct {
	Path   string
	NodeID *ua.NodeID
}

// child returns the hierarchical child of n with the BrowseName ns:name.
func (x *cmClient) child(ctx context.Context, n *ua.NodeID, ns uint16, name string) (*ua.NodeID, error) {
	refs, err := x.c.Node(n).References(ctx, id.HierarchicalReferences, ua.BrowseDirectionForward, ua.NodeClassAll, true)
	if err != nil {
		return nil, fmt.Errorf("browsing %s: %w", n, err)
	}
	for _, r := range refs {
		if r.BrowseName != nil && r.BrowseName.NamespaceIndex == ns && r.BrowseName.Name == name && r.NodeID != nil && r.NodeID.NodeID != nil {
			return r.NodeID.NodeID, nil
		}
	}
	return nil, nil
}

// sets lists the ConnectionConfigurationSets of the ConnectionManager
// (§6.7.2), in the Folders below ConnectionConfigurationSets as well.
func (x *cmClient) sets(ctx context.Context) ([]cmSet, bool, error) {
	root, err := x.child(ctx, x.cm, x.ns.CM, "ConnectionConfigurationSets")
	if err != nil {
		return nil, false, err
	}
	if root == nil {
		return nil, false, fmt.Errorf("the ConnectionManager has no ConnectionConfigurationSets (Part 81 §6.7.2)")
	}
	var out []cmSet
	truncated := false
	seen := map[string]bool{root.String(): true}
	var walk func(n *ua.NodeID, prefix []string, depth int) error
	walk = func(n *ua.NodeID, prefix []string, depth int) error {
		refs, err := x.c.Node(n).References(ctx, id.HierarchicalReferences, ua.BrowseDirectionForward, ua.NodeClassObject, true)
		if err != nil {
			return fmt.Errorf("browsing %s: %w", n, err)
		}
		for _, r := range refs {
			if r.NodeID == nil || r.NodeID.NodeID == nil || r.BrowseName == nil || seen[r.NodeID.NodeID.String()] {
				continue
			}
			nid := r.NodeID.NodeID
			seen[nid.String()] = true
			path := append(append([]string{}, prefix...), r.BrowseName.Name)
			var typ *ua.NodeID
			if r.TypeDefinition != nil {
				typ = r.TypeDefinition.NodeID
			}
			isSet, err := x.isSetType(ctx, typ)
			if err != nil {
				return err
			}
			switch {
			case isSet:
				if len(out) >= cmMaxSets {
					truncated = true
					return nil
				}
				out = append(out, cmSet{Path: strings.Join(path, "/"), NodeID: nid})
			case typ != nil && typ.Namespace() == 0 && typ.IntID() == id.FolderType:
				if depth >= cmMaxDepth {
					truncated = true
					continue
				}
				if err := walk(nid, path, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(root, nil, 0); err != nil {
		return nil, false, err
	}
	return out, truncated, nil
}

// isSetType reports whether typ is ConnectionConfigurationSetType (§6.8.2)
// or a subtype of it.
func (x *cmClient) isSetType(ctx context.Context, typ *ua.NodeID) (bool, error) {
	if typ == nil {
		return false, nil
	}
	want := ua.NewNumericNodeID(x.ns.CM, fxcmid.ConnectionConfigurationSetType).String()
	var chain []string
	t := typ
	for range cmMaxTypeDepth {
		k := t.String()
		if v, ok := x.types[k]; ok {
			return x.remember(chain, v), nil
		}
		chain = append(chain, k)
		if k == want {
			return x.remember(chain, true), nil
		}
		if t.Namespace() == 0 && t.IntID() == id.BaseObjectType {
			return x.remember(chain, false), nil
		}
		refs, err := x.c.Node(t).References(ctx, id.HasSubtype, ua.BrowseDirectionInverse, ua.NodeClassObjectType, false)
		if err != nil {
			return false, fmt.Errorf("browsing the supertype of %s: %w", t, err)
		}
		if len(refs) == 0 || refs[0].NodeID == nil || refs[0].NodeID.NodeID == nil {
			return x.remember(chain, false), nil
		}
		t = refs[0].NodeID.NodeID
	}
	return x.remember(chain, false), nil
}

func (x *cmClient) remember(chain []string, v bool) bool {
	for _, k := range chain {
		x.types[k] = v
	}
	return v
}

// resolve returns the NodeIds of the sets named by args: a NodeId
// (ns=<index>;..., nsu=<uri>;...) or the path fx cm list prints.
func (x *cmClient) resolve(ctx context.Context, args []string) ([]cmSet, error) {
	var listed []cmSet
	byPath := map[string]cmSet{}
	out := make([]cmSet, 0, len(args))
	for _, a := range args {
		if isNodeIDArg(a) {
			b := &fxRequestBuilder{namespaces: x.namespaces}
			n, err := b.nodeID("set", a)
			if err != nil {
				return nil, exitCode(exitUsage, err)
			}
			out = append(out, cmSet{Path: a, NodeID: n})
			continue
		}
		if listed == nil {
			var err error
			if listed, _, err = x.sets(ctx); err != nil {
				return nil, exitCode(exitProtocol, err)
			}
			for _, s := range listed {
				byPath[s.Path] = s
			}
		}
		s, ok := byPath[a]
		if !ok {
			return nil, exitCodef(exitUsage, "no ConnectionConfigurationSet %q (fx cm list prints the paths)", a)
		}
		out = append(out, s)
	}
	return out, nil
}

// isNodeIDArg reports whether a names a set by NodeId rather than by path.
func isNodeIDArg(a string) bool {
	for _, p := range []string{"ns=", "nsu=", "i=", "s=", "g=", "b="} {
		if strings.HasPrefix(a, p) {
			return true
		}
	}
	return false
}

// status reads the state of set s (§6.8.2, §6.9.2).
func (x *cmClient) status(ctx context.Context, s cmSet) (model.FxCMSet, error) {
	out := model.FxCMSet{Path: s.Path, NodeID: s.NodeID.String(), Connections: []model.FxCMConnection{}, Diagnostics: []model.FxCMDiagnostics{}}
	members := map[string]*ua.NodeID{}
	refs, err := x.c.Node(s.NodeID).References(ctx, id.References, ua.BrowseDirectionForward, ua.NodeClassObject|ua.NodeClassVariable, true)
	if err != nil {
		return out, fmt.Errorf("browsing %s: %w", s.NodeID, err)
	}
	var conns []*ua.ReferenceDescription
	hasConn := ua.NewNumericNodeID(x.ns.CM, fxcmid.HasConnectionConfiguration).String()
	for _, r := range refs {
		if r.NodeID == nil || r.NodeID.NodeID == nil || r.BrowseName == nil {
			continue
		}
		if r.ReferenceTypeID != nil && r.ReferenceTypeID.String() == hasConn {
			conns = append(conns, r)
			continue
		}
		if r.BrowseName.NamespaceIndex == x.ns.CM {
			members[r.BrowseName.Name] = r.NodeID.NodeID
		}
	}
	var sm, cur, last *ua.NodeID
	if sm = members["ConnectionConfigurationSetStateMachine"]; sm != nil {
		if cur, err = x.child(ctx, sm, 0, "CurrentState"); err != nil {
			return out, err
		}
		if last, err = x.child(ctx, sm, 0, "LastTransition"); err != nil {
			return out, err
		}
	}
	type read struct {
		n   *ua.NodeID
		set func(v any)
	}
	var reads []read
	add := func(n *ua.NodeID, set func(v any)) {
		if n != nil {
			reads = append(reads, read{n, set})
		}
	}
	add(members["Version"], func(v any) {
		if u, ok := v.(uint32); ok {
			out.Version = &u
		}
	})
	add(members["Edit"], func(v any) {
		if b, ok := v.(bool); ok {
			out.Edit = &b
		}
	})
	add(members["RollbackOnError"], func(v any) {
		if b, ok := v.(bool); ok {
			out.RollbackOnError = &b
		}
	})
	add(cur, func(v any) { out.State = localizedText(v) })
	add(last, func(v any) { out.LastTransition = localizedText(v) })
	add(members["ConnectionsDiagnostics"], func(v any) { out.Diagnostics = x.diagnostics(v) })
	for _, r := range conns {
		i := len(out.Connections)
		out.Connections = append(out.Connections, model.FxCMConnection{Name: r.BrowseName.Name, NodeID: r.NodeID.NodeID.String()})
		pr, err := x.child(ctx, r.NodeID.NodeID, x.ns.CM, "ProcessingResult")
		if err != nil {
			return out, err
		}
		add(pr, func(v any) {
			if sc, ok := v.(ua.StatusCode); ok {
				out.Connections[i].ProcessingResult = sc.Symbol()
				code := sc.Uint32()
				out.Connections[i].ProcessingResultCode = &code
			}
		})
	}
	if cur != nil {
		if stateID, err := x.child(ctx, cur, 0, "Id"); err == nil && stateID != nil {
			add(stateID, func(v any) { out.StateID = x.stateName(v) })
		}
	}
	if len(reads) == 0 {
		return out, nil
	}
	req := &ua.ReadRequest{TimestampsToReturn: ua.TimestampsToReturnNeither}
	for _, r := range reads {
		req.NodesToRead = append(req.NodesToRead, &ua.ReadValueID{NodeID: r.n, AttributeID: ua.AttributeIDValue})
	}
	res, err := x.c.Read(ctx, req)
	if err != nil {
		return out, fmt.Errorf("reading %s: %w", s.NodeID, err)
	}
	if len(res.Results) != len(reads) {
		return out, fmt.Errorf("reading %s: %d results for %d values", s.NodeID, len(res.Results), len(reads))
	}
	for i, dv := range res.Results {
		if dv == nil || dv.Value == nil || dv.Status.Uint32()&0x80000000 != 0 {
			continue
		}
		reads[i].set(dv.Value.Value())
	}
	return out, nil
}

// stateName maps the NodeId of a state of ConnectionConfigurationSetStateMachineType
// (§6.9.2) to Ready, Processing or Error.
func (x *cmClient) stateName(v any) string {
	n, ok := v.(*ua.NodeID)
	if !ok || n == nil || n.Namespace() != x.ns.CM {
		return ""
	}
	switch n.IntID() {
	case fxcmid.ConnectionConfigurationSetStateMachineTypeReady:
		return "Ready"
	case fxcmid.ConnectionConfigurationSetStateMachineTypeProcessing:
		return "Processing"
	case fxcmid.ConnectionConfigurationSetStateMachineTypeError:
		return "Error"
	}
	return ""
}

// diagnostics decodes ConnectionsDiagnostics, an array of
// ConnectionDiagnosticsDataType (§6.8.2).
func (x *cmClient) diagnostics(v any) []model.FxCMDiagnostics {
	eos, _ := v.([]*ua.ExtensionObject)
	out := []model.FxCMDiagnostics{}
	for _, eo := range eos {
		st, err := uafx.DecodeStructure(eo, x.ns)
		d, ok := st.(*uafx.ConnectionDiagnostics)
		if err != nil || !ok || d == nil {
			continue
		}
		e := model.FxCMDiagnostics{
			LastActivity:    d.LastActivity.String(),
			ConnectionState: d.ConnectionState.String(),
			ErrorEndpoint1:  d.ErrorEndpoint1.String(),
			Endpoint1Status: d.Endpoint1Status.Symbol(),
			ErrorEndpoint2:  d.ErrorEndpoint2.String(),
			Endpoint2Status: d.Endpoint2Status.Symbol(),
		}
		if d.Name != nil {
			e.Name = d.Name.Name
		}
		out = append(out, e)
	}
	return out
}

// localizedText returns the text of a LocalizedText value.
func localizedText(v any) string {
	if lt, ok := v.(*ua.LocalizedText); ok && lt != nil {
		return lt.Text
	}
	return ""
}

// aggregated reads AggregatedCurrentState of the ConnectionManager
// (§6.7.2): TRUE while a set is in Error. nil when it has none.
func (x *cmClient) aggregated(ctx context.Context) (*bool, error) {
	n, err := x.child(ctx, x.cm, x.ns.CM, "AggregatedCurrentState")
	if err != nil || n == nil {
		return nil, err
	}
	v, err := x.c.Node(n).Value(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading AggregatedCurrentState: %w", err)
	}
	if b, ok := v.Value().(bool); ok {
		return &b, nil
	}
	return nil, nil
}
