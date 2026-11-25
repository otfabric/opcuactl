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
	"github.com/otfabric/opcuactl/internal/format"
	"github.com/otfabric/opcuactl/internal/model"
)

// Bounds of what fx cm reads from a server, so that a large or hostile
// address space cannot make it read without end. Variables so that tests
// can lower them.
var (
	cmMaxSets      = 10000    // ConnectionConfigurationSets listed
	cmMaxDepth     = 8        // Folder levels below ConnectionConfigurationSets
	cmMaxNodes     = 100000   // Objects visited below ConnectionConfigurationSets
	cmMaxTypeDepth = 16       // supertypes followed to recognise a set type
	cmMaxFileSize  = 64 << 20 // bytes of a ConnectionConfigurationSet file, read or written
	cmChunk        = 64 << 10 // bytes per Read or Write call of the file
	cmMaxPerRead   = 1000     // values per Read request when the server sets no lower MaxNodesPerRead
)

// errNoCM reports that the server has no ConnectionManager.
var errNoCM = errors.New("the server has no UAFX ConnectionManager (FxRoot.ConnectionManager, Part 81 §12.3)")

// errAmbiguous reports that a name matches more than one node.
var errAmbiguous = errors.New("ambiguous")

// cmClient works with the ConnectionManager of a server (Part 81 §6.7).
// The ConnectionManager and its two Methods have the NodeIds Part 81 §12.3
// gives them in the CM namespace; below it opcuactl finds the
// ConnectionConfigurationSets and the file by browsing, so it does not
// depend on how a server names those NodeIds.
type cmClient struct {
	c          *opcua.Client
	namespaces []string
	ns         uafx.NamespaceIndexes
	di         uint16 // index of the DI namespace (the Lock of a set), 0 without
	cm         *ua.NodeID
	perRead    int             // values per Read request
	types      map[string]bool // TypeDefinition -> is a ConnectionConfigurationSetType
}

// newCMClient finds the ConnectionManager of the server: errNoCM when the
// server has none, another error when it could not be checked.
func newCMClient(ctx context.Context, c *opcua.Client) (*cmClient, error) {
	nsa, err := c.NamespaceArray(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading the NamespaceArray: %w", err)
	}
	x := &cmClient{c: c, namespaces: nsa, types: map[string]bool{}, perRead: cmMaxPerRead}
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
		case "http://opcfoundation.org/UA/DI/":
			x.di = uint16(i)
		}
	}
	if x.ns.CM == 0 {
		return nil, errNoCM
	}
	x.cm = ua.NewNumericNodeID(x.ns.CM, fxcmid.FxRootConnectionManager)
	res, err := c.Read(ctx, &ua.ReadRequest{TimestampsToReturn: ua.TimestampsToReturnNeither, NodesToRead: []*ua.ReadValueID{
		{NodeID: x.cm, AttributeID: ua.AttributeIDBrowseName},
		{NodeID: ua.NewNumericNodeID(0, id.ServerServerCapabilitiesOperationLimitsMaxNodesPerRead), AttributeID: ua.AttributeIDValue},
	}})
	if err != nil {
		return nil, fmt.Errorf("reading FxRoot.ConnectionManager: %w", err)
	}
	if len(res.Results) != 2 || res.Results[0] == nil {
		return nil, fmt.Errorf("reading FxRoot.ConnectionManager: %d results for 2 values", len(res.Results))
	}
	switch sc := res.Results[0].Status; {
	case sc == ua.StatusBadNodeIDUnknown:
		return nil, errNoCM
	case sc.Uint32()&0x80000000 != 0:
		return nil, fmt.Errorf("reading FxRoot.ConnectionManager: %w", sc)
	}
	if dv := res.Results[1]; dv != nil && dv.Status == ua.StatusOK && dv.Value != nil {
		if n, ok := dv.Value.Value().(uint32); ok && n > 0 && int64(n) < int64(x.perRead) {
			x.perRead = int(n)
		}
	}
	return x, nil
}

// cmSet is a ConnectionConfigurationSet Object: its path below
// ConnectionConfigurationSets, Folders and Name joined by "/".
type cmSet struct {
	Path   string
	NodeID *ua.NodeID
}

// local returns the NodeId of r when it is a node of this server: a
// reference to another server (ServerIndex) is not followed.
func local(r *ua.ReferenceDescription) *ua.NodeID {
	if r == nil || r.NodeID == nil || r.NodeID.NodeID == nil || r.NodeID.ServerIndex != 0 || r.BrowseName == nil {
		return nil
	}
	return r.NodeID.NodeID
}

// child returns the hierarchical child of n with the BrowseName ns:name,
// nil when it has none and an error when it has several.
func (x *cmClient) child(ctx context.Context, n *ua.NodeID, ns uint16, name string) (*ua.NodeID, error) {
	refs, err := x.c.Node(n).References(ctx, id.HierarchicalReferences, ua.BrowseDirectionForward, ua.NodeClassAll, true)
	if err != nil {
		return nil, fmt.Errorf("browsing %s: %w", n, err)
	}
	var found *ua.NodeID
	for _, r := range refs {
		nid := local(r)
		if nid == nil || r.BrowseName.NamespaceIndex != ns || r.BrowseName.Name != name {
			continue
		}
		if found != nil && !found.Equal(nid) {
			return nil, fmt.Errorf("%s has more than one child %d:%s: %w", n, ns, name, errAmbiguous)
		}
		found = nid
	}
	return found, nil
}

// sets lists the ConnectionConfigurationSets of the ConnectionManager
// (§6.7.2), in the Folders below ConnectionConfigurationSets as well. The
// bool is TRUE when a bound cut the listing short.
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
			nid := local(r)
			if nid == nil || seen[nid.String()] {
				continue
			}
			if len(seen) >= cmMaxNodes {
				truncated = true
				return nil
			}
			seen[nid.String()] = true
			path := append(append([]string{}, prefix...), r.BrowseName.Name)
			var typ *ua.NodeID
			if r.TypeDefinition != nil && r.TypeDefinition.ServerIndex == 0 {
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
		if len(refs) == 0 || local(refs[0]) == nil {
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
// (ns=<index>;..., nsu=<uri>;...) or the path fx cm list prints. A path
// that two sets share, or one looked up in a listing a bound cut short, is
// refused: name the set by its NodeId then.
func (x *cmClient) resolve(ctx context.Context, args []string) ([]cmSet, error) {
	var listed []cmSet
	truncated := false
	byPath := map[string][]cmSet{}
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
			if listed, truncated, err = x.sets(ctx); err != nil {
				format.Errf("listing the sets: %v", err)
				return nil, exitCode(exitProtocol, err)
			}
			if listed == nil {
				listed = []cmSet{}
			}
			for _, s := range listed {
				byPath[s.Path] = append(byPath[s.Path], s)
			}
		}
		if truncated {
			return nil, exitCodef(exitPartial, "the listing of the sets was cut short at %d sets, %d Folder levels or %d nodes: name %q by its NodeId", cmMaxSets, cmMaxDepth, cmMaxNodes, a)
		}
		switch m := byPath[a]; len(m) {
		case 0:
			return nil, exitCodef(exitUsage, "no ConnectionConfigurationSet %q (fx cm list prints the paths)", a)
		case 1:
			out = append(out, m[0])
		default:
			return nil, exitCodef(exitUsage, "%d ConnectionConfigurationSets have the path %q: name it by its NodeId (fx cm list prints them)", len(m), a)
		}
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

// read is one value of a Read request and what to do with it.
type cmRead struct {
	name string
	n    *ua.NodeID
	set  func(v any) bool // FALSE: the value has another type
}

// readValues reads the Values of reads in requests of at most x.perRead
// and returns the names of those the server did not return.
func (x *cmClient) readValues(ctx context.Context, reads []cmRead) ([]string, error) {
	var unread []string
	for start := 0; start < len(reads); start += x.perRead {
		batch := reads[start:min(start+x.perRead, len(reads))]
		req := &ua.ReadRequest{TimestampsToReturn: ua.TimestampsToReturnNeither}
		for _, r := range batch {
			req.NodesToRead = append(req.NodesToRead, &ua.ReadValueID{NodeID: r.n, AttributeID: ua.AttributeIDValue})
		}
		res, err := x.c.Read(ctx, req)
		if err != nil {
			return nil, err
		}
		if len(res.Results) != len(batch) {
			return nil, fmt.Errorf("%d results for %d values", len(res.Results), len(batch))
		}
		for i, dv := range res.Results {
			if dv == nil || dv.Status.Uint32()&0x80000000 != 0 || dv.Value == nil || !batch[i].set(dv.Value.Value()) {
				unread = append(unread, batch[i].name)
			}
		}
	}
	return unread, nil
}

// stateNodes returns the CurrentState and LastTransition Variables of the
// state machine of set n (§6.9.2) and the Id Property of CurrentState; nil
// for those the set has not.
func (x *cmClient) stateNodes(ctx context.Context, sm *ua.NodeID) (cur, last, curID *ua.NodeID, err error) {
	if sm == nil {
		return nil, nil, nil, nil
	}
	if cur, err = x.child(ctx, sm, 0, "CurrentState"); err != nil {
		return nil, nil, nil, err
	}
	if last, err = x.child(ctx, sm, 0, "LastTransition"); err != nil {
		return nil, nil, nil, err
	}
	if cur != nil {
		if curID, err = x.child(ctx, cur, 0, "Id"); err != nil {
			return nil, nil, nil, err
		}
	}
	return cur, last, curID, nil
}

// stateMachine returns the ConnectionConfigurationSetStateMachine of set n.
func (x *cmClient) stateMachine(ctx context.Context, n *ua.NodeID) (*ua.NodeID, error) {
	return x.child(ctx, n, x.ns.CM, "ConnectionConfigurationSetStateMachine")
}

// status reads the state of set s (§6.8.2, §6.9.2).
func (x *cmClient) status(ctx context.Context, s cmSet) (model.FxCMSet, error) {
	out := model.FxCMSet{Path: s.Path, NodeID: s.NodeID.String(), Connections: []model.FxCMConnection{}, Diagnostics: []model.FxCMDiagnostics{}}
	members := map[string]*ua.NodeID{}
	refs, err := x.c.Node(s.NodeID).References(ctx, id.HierarchicalReferences, ua.BrowseDirectionForward, ua.NodeClassObject|ua.NodeClassVariable, true)
	if err != nil {
		return out, fmt.Errorf("browsing %s: %w", s.NodeID, err)
	}
	var conns []*ua.ReferenceDescription
	hasConn := ua.NewNumericNodeID(x.ns.CM, fxcmid.HasConnectionConfiguration).String()
	for _, r := range refs {
		nid := local(r)
		if nid == nil {
			continue
		}
		if r.ReferenceTypeID != nil && r.ReferenceTypeID.String() == hasConn {
			conns = append(conns, r)
			continue
		}
		if r.BrowseName.NamespaceIndex == x.ns.CM {
			members[r.BrowseName.Name] = nid
		}
	}
	cur, last, curID, err := x.stateNodes(ctx, members["ConnectionConfigurationSetStateMachine"])
	if err != nil {
		return out, err
	}
	var reads []cmRead
	add := func(name string, n *ua.NodeID, set func(v any) bool) {
		if n != nil {
			reads = append(reads, cmRead{name, n, set})
		}
	}
	add("Version", members["Version"], func(v any) bool {
		u, ok := v.(uint32)
		if ok {
			out.Version = &u
		}
		return ok
	})
	add("Edit", members["Edit"], func(v any) bool {
		b, ok := v.(bool)
		if ok {
			out.Edit = &b
		}
		return ok
	})
	add("RollbackOnError", members["RollbackOnError"], func(v any) bool {
		b, ok := v.(bool)
		if ok {
			out.RollbackOnError = &b
		}
		return ok
	})
	add("CurrentState", cur, func(v any) bool {
		lt, ok := v.(*ua.LocalizedText)
		if ok && lt != nil {
			out.State = lt.Text
		}
		return ok
	})
	add("CurrentState/Id", curID, func(v any) bool {
		out.StateID = x.stateName(v)
		_, ok := v.(*ua.NodeID)
		return ok
	})
	add("LastTransition", last, func(v any) bool {
		lt, ok := v.(*ua.LocalizedText)
		if ok && lt != nil {
			out.LastTransition = lt.Text
		}
		return ok
	})
	add("ConnectionsDiagnostics", members["ConnectionsDiagnostics"], func(v any) bool {
		eos, ok := v.([]*ua.ExtensionObject)
		if !ok && v != nil {
			return false
		}
		var bad []int
		out.Diagnostics, bad = x.diagnostics(eos)
		for _, i := range bad {
			out.Unread = append(out.Unread, fmt.Sprintf("ConnectionsDiagnostics[%d]", i))
		}
		return true
	})
	for _, r := range conns {
		i := len(out.Connections)
		out.Connections = append(out.Connections, model.FxCMConnection{Name: r.BrowseName.Name, NodeID: r.NodeID.NodeID.String()})
		pr, err := x.child(ctx, r.NodeID.NodeID, x.ns.CM, "ProcessingResult")
		if err != nil {
			return out, err
		}
		add(r.BrowseName.Name+"/ProcessingResult", pr, func(v any) bool {
			sc, ok := v.(ua.StatusCode)
			if ok {
				out.Connections[i].ProcessingResult = sc.Symbol()
				code := sc.Uint32()
				out.Connections[i].ProcessingResultCode = &code
			}
			return ok
		})
	}
	unread, err := x.readValues(ctx, reads)
	if err != nil {
		return out, fmt.Errorf("reading %s: %w", s.NodeID, err)
	}
	out.Unread = append(unread, out.Unread...)
	return out, nil
}

// stateName maps the NodeId of a state of ConnectionConfigurationSetStateMachineType
// (§6.9.2) to Ready, Processing or Error, "" for any other value.
func (x *cmClient) stateName(v any) string {
	n, ok := v.(*ua.NodeID)
	if !ok || n == nil || n.Namespace() != x.ns.CM || n.Type() == ua.NodeIDTypeString || n.Type() == ua.NodeIDTypeGUID || n.Type() == ua.NodeIDTypeByteString {
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
// ConnectionDiagnosticsDataType (§6.8.2), and returns the indexes of the
// entries that did not decode.
func (x *cmClient) diagnostics(eos []*ua.ExtensionObject) ([]model.FxCMDiagnostics, []int) {
	out := []model.FxCMDiagnostics{}
	var bad []int
	for i, eo := range eos {
		st, err := uafx.DecodeStructure(eo, x.ns)
		d, ok := st.(*uafx.ConnectionDiagnostics)
		if err != nil || !ok || d == nil {
			bad = append(bad, i)
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
	return out, bad
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
	b, ok := v.Value().(bool)
	if !ok {
		return nil, fmt.Errorf("AggregatedCurrentState is a %T, not a Boolean", v.Value())
	}
	return &b, nil
}
