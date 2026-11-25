// SPDX-License-Identifier: MIT

package opcuactl

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/otfabric/go-opcua/id"
	"github.com/otfabric/go-opcua/server"
	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/go-opcua/uafx"
	"github.com/otfabric/go-opcua/uafx/fxcmid"
	"github.com/otfabric/go-opcua/uafx/fxserver"
	"github.com/otfabric/opcuactl/internal/model"
)

// fakeCM is a server with the CM model and a ConnectionManager built by
// hand: its sets, their state and its Methods answer what a test makes
// them answer, which no working ConnectionManager does.
type fakeCM struct {
	t       *testing.T
	srv     *server.Server
	space   *server.NodeNameSpace
	ns, di  uint16
	url     string
	folders *server.Node // ConnectionConfigurationSets

	mu    sync.Mutex
	state map[string]func() *ua.DataValue // CurrentState.Id of a set by its String NodeId

	acl *fakeACL
}

// fakeACL refuses Browse and Read of the nodes with the BrowseNames a test
// names, as a server refuses a user.
type fakeACL struct {
	server.DefaultAccessController
	srv *server.Server

	mu           sync.Mutex
	browse, read map[string]bool
}

func (a *fakeACL) deny(names map[string]bool, n *ua.NodeID) ua.StatusCode {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(names) == 0 {
		return ua.StatusOK
	}
	if node := a.srv.Node(n); node != nil && node.BrowseName() != nil && names[node.BrowseName().Name] {
		return ua.StatusBadUserAccessDenied
	}
	return ua.StatusOK
}

func (a *fakeACL) CheckBrowse(_ context.Context, _ *server.Session, n *ua.NodeID) ua.StatusCode {
	return a.deny(a.browse, n)
}

func (a *fakeACL) CheckRead(_ context.Context, _ *server.Session, n *ua.NodeID) ua.StatusCode {
	return a.deny(a.read, n)
}

// refuse makes the server refuse Browse of the nodes named browse and
// Read of the ones named read, until the next call.
func (a *fakeACL) refuse(browse, read []string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.browse, a.read = map[string]bool{}, map[string]bool{}
	for _, n := range browse {
		a.browse[n] = true
	}
	for _, n := range read {
		a.read[n] = true
	}
}

func startFakeCM(t *testing.T) *fakeCM {
	t.Helper()
	acl := &fakeACL{}
	srv, err := server.New(server.EndPoint("localhost", 0), server.InsecureNoSecurity(),
		server.EnableAuthMode(ua.UserTokenTypeAnonymous), server.ApplicationURI("urn:opcuactl:test:fake-cm"),
		server.WithAccessController(acl))
	if err != nil {
		t.Fatal(err)
	}
	acl.srv = srv
	nss, err := fxserver.AddModel(srv, fxserver.ModelCM)
	if err != nil {
		t.Fatal(err)
	}
	space, err := srv.Namespace(int(nss.CM))
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeCM{t: t, srv: srv, space: space.(*server.NodeNameSpace), ns: nss.CM, di: nss.DI, state: map[string]func() *ua.DataValue{}, acl: acl}
	f.folders = srv.Node(ua.NewNumericNodeID(nss.CM, fxcmid.FxRootConnectionManagerConnectionConfigurationSets))
	if f.folders == nil {
		t.Fatal("no ConnectionConfigurationSets")
	}
	if err := srv.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	f.url = fmt.Sprintf("opc.tcp://localhost:%d", srv.Port())
	return f
}

func (f *fakeCM) add(parent, n *server.Node, ref uint32, typ *ua.NodeID) *server.Node {
	f.t.Helper()
	f.space.AddNode(n)
	parent.AddReference(n, ua.NewNumericNodeID(0, ref), true)
	if typ != nil {
		if tn := f.srv.Node(typ); tn != nil {
			n.AddReference(tn, ua.NewNumericNodeID(0, id.HasTypeDefinition), true)
		}
	}
	return n
}

// object adds the Object ns:name of the type typ below parent.
func (f *fakeCM) object(parent *server.Node, sid string, ns uint16, name string, typ *ua.NodeID) *server.Node {
	f.t.Helper()
	n := server.NewFolderNode(ua.NewStringNodeID(f.ns, sid), name)
	if err := n.SetAttribute(ua.AttributeIDBrowseName, server.DataValueFromValue(&ua.QualifiedName{NamespaceIndex: ns, Name: name})); err != nil {
		f.t.Fatal(err)
	}
	ref := uint32(id.HasComponent)
	if typ != nil && typ.Namespace() == 0 && typ.IntID() == id.FolderType {
		ref = id.Organizes
	}
	return f.add(parent, n, ref, typ)
}

// variable adds the Variable ns:name with value v below parent, by ref.
func (f *fakeCM) variable(parent *server.Node, sid string, ns uint16, name string, v any, ref uint32) *server.Node {
	f.t.Helper()
	n := server.NewVariableNode(ua.NewStringNodeID(f.ns, sid), name, v)
	access := byte(ua.AccessLevelTypeCurrentRead | ua.AccessLevelTypeCurrentWrite)
	for a, val := range map[ua.AttributeID]any{
		ua.AttributeIDBrowseName:      &ua.QualifiedName{NamespaceIndex: ns, Name: name},
		ua.AttributeIDAccessLevel:     access,
		ua.AttributeIDUserAccessLevel: access,
	} {
		if err := n.SetAttribute(a, server.DataValueFromValue(val)); err != nil {
			f.t.Fatal(err)
		}
	}
	return f.add(parent, n, ref, ua.NewNumericNodeID(0, id.BaseDataVariableType))
}

// method adds the Method ns:name below parent and its handler.
func (f *fakeCM) method(parent *server.Node, sid string, ns uint16, name string, h server.MethodHandler) {
	f.t.Helper()
	n := server.NewNode(ua.NewStringNodeID(f.ns, sid), server.Attributes{
		ua.AttributeIDNodeClass:      server.DataValueFromValue(uint32(ua.NodeClassMethod)),
		ua.AttributeIDBrowseName:     server.DataValueFromValue(&ua.QualifiedName{NamespaceIndex: ns, Name: name}),
		ua.AttributeIDDisplayName:    server.DataValueFromValue(ua.NewLocalizedText(name)),
		ua.AttributeIDExecutable:     server.DataValueFromValue(true),
		ua.AttributeIDUserExecutable: server.DataValueFromValue(true),
	}, []*ua.ReferenceDescription{}, nil)
	f.add(parent, n, id.HasComponent, nil)
	f.srv.RegisterMethod(parent.ID(), n.ID(), h)
}

// folder adds a Folder below parent.
func (f *fakeCM) folder(parent *server.Node, sid, name string) *server.Node {
	return f.object(parent, sid, f.ns, name, ua.NewNumericNodeID(0, id.FolderType))
}

// set adds a ConnectionConfigurationSet below parent with a state machine
// whose CurrentState.Id is what f.setState gives it (Ready at first).
func (f *fakeCM) set(parent *server.Node, sid, name string) *server.Node {
	f.t.Helper()
	s := f.object(parent, sid, f.ns, name, ua.NewNumericNodeID(f.ns, fxcmid.ConnectionConfigurationSetType))
	sm := f.object(s, sid+".SM", f.ns, "ConnectionConfigurationSetStateMachine", ua.NewNumericNodeID(f.ns, fxcmid.ConnectionConfigurationSetStateMachineType))
	cur := f.variable(sm, sid+".CurrentState", 0, "CurrentState", ua.NewLocalizedText("Ready"), id.HasComponent)
	f.setState(sid, f.stateID(fxcmid.ConnectionConfigurationSetStateMachineTypeReady))
	f.variable(cur, sid+".CurrentState.Id", 0, "Id", server.ValueFunc(func() *ua.DataValue {
		f.mu.Lock()
		get := f.state[sid]
		f.mu.Unlock()
		return get()
	}), id.HasProperty)
	return s
}

func (f *fakeCM) stateID(state uint32) func() *ua.DataValue {
	return func() *ua.DataValue { return server.DataValueFromValue(ua.NewNumericNodeID(f.ns, state)) }
}

// setState makes get the CurrentState.Id of the set sid.
func (f *fakeCM) setState(sid string, get func() *ua.DataValue) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.state[sid] = get
}

// onProcess makes h ProcessConnectionConfigurationSets.
func (f *fakeCM) onProcess(h server.MethodHandler) {
	f.srv.RegisterMethod(ua.NewNumericNodeID(f.ns, fxcmid.FxRootConnectionManager), ua.NewNumericNodeID(f.ns, fxcmid.FxRootConnectionManagerProcessConnectionConfigurationSets), h)
}

// onEdit makes h EditConnectionConfigurationSets.
func (f *fakeCM) onEdit(h server.MethodHandler) {
	f.srv.RegisterMethod(ua.NewNumericNodeID(f.ns, fxcmid.FxRootConnectionManager), ua.NewNumericNodeID(f.ns, fxcmid.FxRootConnectionManagerEditConnectionConfigurationSets), h)
}

// started answers ProcessConnectionConfigurationSets with Good for every
// set and starts no run.
func started(_ context.Context, _, _ *ua.NodeID, args []*ua.Variant) ([]*ua.Variant, ua.StatusCode) {
	sets, _ := args[1].Value().([]*ua.NodeID)
	res := make([]ua.StatusCode, len(sets))
	return []*ua.Variant{ua.MustVariant(res)}, ua.StatusOK
}

// TestE2E_FxCMFakeWait: --wait counts only a set read in Ready, by the
// NodeId of its state, as done.
func TestE2E_FxCMFakeWait(t *testing.T) {
	f := startFakeCM(t)
	plant := f.folder(f.folders, "Plant", "Plant")
	f.set(plant, "A", "A")
	f.object(plant, "NoSM", f.ns, "NoSM", ua.NewNumericNodeID(f.ns, fxcmid.ConnectionConfigurationSetType))
	f.onProcess(started)
	prevPoll := cmPoll
	cmPoll = 10 * time.Millisecond
	t.Cleanup(func() { cmPoll = prevPoll })

	value := func(v any) func() *ua.DataValue {
		return func() *ua.DataValue { return server.DataValueFromValue(v) }
	}
	var reads atomic.Int32
	for _, c := range []struct {
		name  string
		get   func() *ua.DataValue
		set   string
		wait  string
		want  int
		state string
	}{
		{"ready", f.stateID(fxcmid.ConnectionConfigurationSetStateMachineTypeReady), "Plant/A", "5s", exitOK, "Ready"},
		{"error", f.stateID(fxcmid.ConnectionConfigurationSetStateMachineTypeError), "Plant/A", "5s", exitProtocol, "Error"},
		{"unknown state", f.stateID(4242), "Plant/A", "5s", exitProtocol, "unknown"},
		{"state of another namespace", value(ua.NewNumericNodeID(0, fxcmid.ConnectionConfigurationSetStateMachineTypeReady)), "Plant/A", "5s", exitProtocol, "unknown"},
		{"localized state", value(ua.NewLocalizedText("Ready")), "Plant/A", "5s", exitProtocol, "unread"},
		{"denied state", func() *ua.DataValue {
			return &ua.DataValue{EncodingMask: ua.DataValueStatusCode, Status: ua.StatusBadUserAccessDenied}
		}, "Plant/A", "5s", exitProtocol, "unread"},
		{"no state machine", nil, "Plant/NoSM", "5s", exitProtocol, "unknown"},
		{"processing then ready", func() *ua.DataValue {
			if reads.Add(1) < 3 {
				return f.stateID(fxcmid.ConnectionConfigurationSetStateMachineTypeProcessing)()
			}
			return f.stateID(fxcmid.ConnectionConfigurationSetStateMachineTypeReady)()
		}, "Plant/A", "5s", exitOK, "Ready"},
		{"processing", f.stateID(fxcmid.ConnectionConfigurationSetStateMachineTypeProcessing), "Plant/A", "200ms", exitTimeout, "Processing"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if c.get != nil {
				f.setState("A", c.get)
			}
			out, stderr, err := runCLI(t, "fx", "cm", "process", "-e", f.url, "--action", "establish", c.set, "--wait", c.wait, "--format", "json")
			if code := ExitCode(err); code != c.want {
				t.Fatalf("exit %d, want %d (%v)\n%s%s", code, c.want, err, out, stderr)
			}
			if r := cmJSON[model.FxCMCallResult](t, out); r.Sets[0].State != c.state {
				t.Fatalf("state %q, want %q", r.Sets[0].State, c.state)
			}
		})
	}

	t.Run("slow reads", func(t *testing.T) {
		// A server slower than --wait: the wait ends at its bound, not
		// when the server answers.
		release := make(chan struct{})
		t.Cleanup(func() { close(release) })
		f.setState("A", func() *ua.DataValue {
			select {
			case <-release:
			case <-time.After(time.Second):
			}
			return f.stateID(fxcmid.ConnectionConfigurationSetStateMachineTypeReady)()
		})
		start := time.Now()
		_, _, err := runCLI(t, "fx", "cm", "process", "-e", f.url, "--action", "establish", "Plant/A", "--wait", "300ms")
		if code := ExitCode(err); code != exitTimeout {
			t.Fatalf("exit %d (%v)", code, err)
		}
		if d := time.Since(start); d > 1800*time.Millisecond {
			t.Fatalf("--wait 300ms took %s", d)
		}
	})
}

// TestE2E_FxCMFakeMethods: a refusal or absence of the Method itself, not
// of one set, has its own exit.
func TestE2E_FxCMFakeMethods(t *testing.T) {
	t.Parallel()
	w := startCLIWorker(t)
	f := startFakeCM(t)
	f.set(f.folders, "A", "A")
	for _, c := range []struct {
		name string
		sc   ua.StatusCode
		want int
	}{
		{"denied", ua.StatusBadUserAccessDenied, exitPermission},
		{"no Method", ua.StatusBadMethodInvalid, exitUnsupported},
		{"not implemented", ua.StatusBadNotImplemented, exitUnsupported},
		{"refused", ua.StatusBadInvalidState, exitProtocol},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := func(context.Context, *ua.NodeID, *ua.NodeID, []*ua.Variant) ([]*ua.Variant, ua.StatusCode) {
				return nil, c.sc
			}
			f.onProcess(h)
			f.onEdit(h)
			if _, _, err := w.run(t, "fx", "cm", "process", "-e", f.url, "--action", "establish", "A"); ExitCode(err) != c.want {
				t.Fatalf("process: exit %d, want %d (%v)", ExitCode(err), c.want, err)
			}
			if _, _, err := w.run(t, "fx", "cm", "edit", "-e", f.url, "A", "--write", "X=1"); ExitCode(err) != c.want {
				t.Fatalf("edit: exit %d, want %d (%v)", ExitCode(err), c.want, err)
			}
		})
	}
}

// TestE2E_FxCMFakeResolve: a set path two sets share, a listing cut short,
// and --write paths that are ambiguous, not Variables or reachable only by
// a non-hierarchical reference are refused.
func TestE2E_FxCMFakeResolve(t *testing.T) {
	f := startFakeCM(t)
	plant := f.folder(f.folders, "Plant", "Plant")
	f.set(plant, "Dup1", "Dup")
	f.set(plant, "Dup2", "Dup")
	e := f.set(plant, "E", "E")
	flow := f.object(e, "E.Flow", f.ns, "Flow", nil)
	f.variable(flow, "E.Flow.X.cm", f.ns, "X", 1.0, id.HasComponent)
	f.variable(flow, "E.Flow.X.0", 0, "X", 2.0, id.HasComponent)
	f.variable(flow, "E.Flow.Y", f.ns, "Y", 3.0, id.HasComponent)
	f.object(e, "E.Obj", f.ns, "Obj", nil)
	f.variable(e, "E.Hidden", f.ns, "Hidden", 4.0, id.GeneratesEvent)
	f.onProcess(started)
	var commits atomic.Int32
	// Version is 1 + the commits: each commit makes the next Version.
	f.variable(e, "E.Version", f.ns, "Version", server.ValueFunc(func() *ua.DataValue {
		return server.DataValueFromValue(uint32(1 + commits.Load()))
	}), id.HasProperty)
	f.onEdit(func(_ context.Context, _, _ *ua.NodeID, args []*ua.Variant) ([]*ua.Variant, ua.StatusCode) {
		if a, _ := args[0].Value().(int32); uafx.FxEdit(a) == uafx.CommitUpdates {
			commits.Add(1)
		}
		return []*ua.Variant{ua.MustVariant([]ua.StatusCode{ua.StatusOK})}, ua.StatusOK
	})

	for _, c := range []struct {
		name string
		args []string
		want int
	}{
		{"shared path", []string{"process", "--action", "establish", "Plant/Dup"}, exitUsage},
		{"ambiguous name", []string{"edit", "Plant/E", "--write", "Flow/X=5"}, exitUsage},
		{"not a Variable", []string{"edit", "Plant/E", "--write", "Obj=5"}, exitUsage},
		{"non-hierarchical", []string{"edit", "Plant/E", "--write", "Hidden=5"}, exitUsage},
		{"qualified name", []string{"edit", "Plant/E", "--write", fmt.Sprintf("Flow/%d:X=5", f.ns)}, exitOK},
		{"qualified other namespace", []string{"edit", "Plant/E", "--write", "Flow/0:X=6"}, exitOK},
		{"unique name", []string{"edit", "Plant/E", "--write", "Flow/Y=7"}, exitOK},
	} {
		t.Run(c.name, func(t *testing.T) {
			out, stderr, err := runCLI(t, append(append([]string{"fx", "cm"}, c.args...), "-e", f.url)...)
			if code := ExitCode(err); code != c.want {
				t.Fatalf("exit %d, want %d (%v)\n%s%s", code, c.want, err, out, stderr)
			}
		})
	}
	if commits.Load() != 3 {
		t.Fatalf("%d commits, want 3", commits.Load())
	}
	for sid, want := range map[string]float64{"E.Flow.X.cm": 5, "E.Flow.X.0": 6, "E.Flow.Y": 7, "E.Hidden": 4} {
		if got := f.srv.Node(ua.NewStringNodeID(f.ns, sid)).Value().Value.Value(); got != want {
			t.Fatalf("%s is %v, want %v", sid, got, want)
		}
	}

	t.Run("cut short", func(t *testing.T) {
		prev := cmMaxSets
		cmMaxSets = 1
		t.Cleanup(func() { cmMaxSets = prev })
		if _, _, err := runCLI(t, "fx", "cm", "process", "-e", f.url, "--action", "establish", "Plant/E"); ExitCode(err) != exitPartial {
			t.Fatalf("exit %d (%v)", ExitCode(err), err)
		}
		// By NodeId it needs no listing.
		if _, _, err := runCLI(t, "fx", "cm", "process", "-e", f.url, "--action", "establish", fmt.Sprintf("ns=%d;s=E", f.ns)); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("node budget", func(t *testing.T) {
		prev := cmMaxNodes
		cmMaxNodes = 2
		t.Cleanup(func() { cmMaxNodes = prev })
		out, _, err := runCLI(t, "fx", "cm", "list", "-e", f.url, "--format", "json")
		if ExitCode(err) != exitPartial {
			t.Fatalf("exit %d (%v)", ExitCode(err), err)
		}
		if l := cmJSON[model.FxCMList](t, out); !l.Truncated {
			t.Fatalf("%+v", l)
		}
	})
	t.Run("unread values", func(t *testing.T) {
		f.setState("E", func() *ua.DataValue {
			return &ua.DataValue{EncodingMask: ua.DataValueStatusCode, Status: ua.StatusBadUserAccessDenied}
		})
		out, _, err := runCLI(t, "fx", "cm", "list", "-e", f.url, "--format", "json")
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range cmJSON[model.FxCMList](t, out).Sets {
			if s.Path == "Plant/E" && (len(s.Unread) != 1 || s.Unread[0] != "CurrentState/Id" || s.StateID != "") {
				t.Fatalf("%+v", s)
			}
		}
	})
}

// TestE2E_FxCMFakeEdit: the outcome of an edit when the commit is refused
// or gets no answer, when DiscardUpdates fails, and the renewal of the
// Lock.
func TestE2E_FxCMFakeEdit(t *testing.T) {
	f := startFakeCM(t)
	e := f.set(f.folders, "E", "E")
	x := f.variable(e, "E.X", f.ns, "X", 1.0, id.HasComponent)
	// Version: CommitUpdates adds bump to it, 1 unless a case says; with
	// other, the commit leaves X another value than the one written.
	var version, bump atomic.Uint32
	var other atomic.Bool
	version.Store(1)
	f.variable(e, "E.Version", f.ns, "Version", server.ValueFunc(func() *ua.DataValue {
		return server.DataValueFromValue(version.Load())
	}), id.HasProperty)
	lock := f.object(e, "E.Lock", f.di, "Lock", nil)
	var renewals atomic.Int32
	var renewStatus atomic.Int32
	f.method(lock, "E.Lock.RenewLock", f.di, "RenewLock", func(context.Context, *ua.NodeID, *ua.NodeID, []*ua.Variant) ([]*ua.Variant, ua.StatusCode) {
		renewals.Add(1)
		return []*ua.Variant{ua.MustVariant(renewStatus.Load())}, ua.StatusOK
	})
	type step struct {
		sc    ua.StatusCode
		delay time.Duration
	}
	var mu sync.Mutex
	steps := map[uafx.FxEdit]step{}
	// One call at a time: DiscardUpdates sees what the CommitUpdates
	// before it did. Like a conformant server (Part 81 §6.7.4) and
	// go-opcua, it answers Good to a DiscardUpdates of a set no longer in
	// edit, and ignores it.
	var serial sync.Mutex
	f.onEdit(func(_ context.Context, _, _ *ua.NodeID, args []*ua.Variant) ([]*ua.Variant, ua.StatusCode) {
		serial.Lock()
		defer serial.Unlock()
		a, _ := args[0].Value().(int32)
		mu.Lock()
		st := steps[uafx.FxEdit(a)]
		mu.Unlock()
		time.Sleep(st.delay)
		if uafx.FxEdit(a) == uafx.CommitUpdates && st.sc == ua.StatusOK {
			version.Add(bump.Load())
			if other.Load() {
				_ = x.SetAttribute(ua.AttributeIDValue, server.DataValueFromValue(7.0))
			}
		}
		return []*ua.Variant{ua.MustVariant([]ua.StatusCode{st.sc})}, ua.StatusOK
	})
	prevCleanup, prevRenew := cmCleanup, cmRenewAfter
	t.Cleanup(func() { cmCleanup, cmRenewAfter = prevCleanup, prevRenew })
	cmCleanup = 2 * time.Second

	for _, c := range []struct {
		name      string
		steps     map[uafx.FxEdit]step
		bump      uint32
		other     bool
		renew     int32
		timeout   string
		want      int
		outcome   string
		discarded bool
		cleanup   bool
		failed    string
	}{
		{"committed", nil, 1, false, 0, "5s", exitOK, "committed", false, false, ""},
		// The Lock expired after the last write: the server ignores
		// CommitUpdates and answers Good (§6.7.4); the Version stays.
		{"commit ignored, edit expired", nil, 0, false, 0, "5s", exitProtocol, "not-committed", false, false, "CommitUpdates"},
		{"commit not established", nil, 2, false, 0, "5s", exitProtocol, "unknown", false, false, "CommitUpdates"},
		{"commit reads another value", nil, 1, true, 0, "5s", exitProtocol, "unknown", false, false, "CommitUpdates"},
		{"commit refused", map[uafx.FxEdit]step{uafx.CommitUpdates: {sc: ua.StatusBadInvalidState}}, 1, false, 0, "5s", exitProtocol, "not-committed", true, false, "CommitUpdates"},
		{"discard fails", map[uafx.FxEdit]step{uafx.CommitUpdates: {sc: ua.StatusBadInvalidState}, uafx.DiscardUpdates: {sc: ua.StatusBadInternalError}}, 1, false, 0, "5s", exitProtocol, "not-committed", false, true, "CommitUpdates"},
		// CommitUpdates gets no answer within --timeout. A Good
		// DiscardUpdates after it shows nothing (the server ignores it
		// once the commit ended the edit) and is never reported as a
		// rollback; only the Version and the values read back decide.
		{"commit lost, not applied", map[uafx.FxEdit]step{uafx.CommitUpdates: {sc: ua.StatusBadInvalidState, delay: 700 * time.Millisecond}}, 1, false, 0, "400ms", exitTimeout, "unknown", false, false, "CommitUpdates"},
		{"commit lost, applied", map[uafx.FxEdit]step{uafx.CommitUpdates: {delay: 700 * time.Millisecond}}, 1, false, 0, "400ms", exitOK, "committed", false, false, ""},
		{"commit lost, applied, another value", map[uafx.FxEdit]step{uafx.CommitUpdates: {delay: 700 * time.Millisecond}}, 1, true, 0, "400ms", exitTimeout, "unknown", false, false, "CommitUpdates"},
		{"commit lost, discard fails", map[uafx.FxEdit]step{uafx.CommitUpdates: {delay: 700 * time.Millisecond}, uafx.DiscardUpdates: {sc: ua.StatusBadInternalError}}, 2, false, 0, "400ms", exitTimeout, "unknown", false, true, "CommitUpdates"},
		{"lock lost", nil, 1, false, -1, "5s", exitProtocol, "not-committed", true, false, "RenewLock"},
	} {
		t.Run(c.name, func(t *testing.T) {
			mu.Lock()
			steps = c.steps
			if steps == nil {
				steps = map[uafx.FxEdit]step{}
			}
			mu.Unlock()
			bump.Store(c.bump)
			other.Store(c.other)
			v0 := version.Load()
			renewStatus.Store(c.renew)
			renewals.Store(0)
			cmRenewAfter = 0
			out, stderr, err := runCLI(t, "fx", "cm", "edit", "-e", f.url, "E", "--write", "X=2", "--timeout", c.timeout, "--format", "json")
			if code := ExitCode(err); code != c.want {
				t.Fatalf("exit %d, want %d (%v)\n%s%s", code, c.want, err, out, stderr)
			}
			ed := cmJSON[model.FxCMEdit](t, out)
			if ed.Outcome != c.outcome || ed.Discarded != c.discarded || (ed.CleanupError != "") != c.cleanup || ed.Failed != c.failed {
				t.Fatalf("%+v", ed)
			}
			if renewals.Load() == 0 {
				t.Fatal("the Lock was not renewed")
			}
			if c.outcome == "committed" && (ed.Version == nil || *ed.Version != v0+1 || !ed.Committed) {
				t.Fatalf("committed without Version %d: %+v", v0+1, ed)
			}
			if c.outcome != "committed" && ed.Committed {
				t.Fatalf("committed TRUE: %+v", ed)
			}
			if c.name == "commit lost, applied" && !strings.Contains(stderr, "show the commit") {
				t.Fatalf("stderr does not say how the commit was shown: %s", stderr)
			}
			if c.name == "commit ignored, edit expired" && !strings.Contains(stderr, "ignored the commit") {
				t.Fatalf("stderr does not say the commit was ignored: %s", stderr)
			}
			if c.outcome == "unknown" && !strings.Contains(stderr, "unknown") && !strings.Contains(stderr, "cannot be established") {
				t.Fatalf("stderr does not say the outcome is unknown: %s", stderr)
			}
		})
	}
	// The Lock is not renewed while the edit is young.
	cmRenewAfter = time.Hour
	bump.Store(1)
	renewals.Store(0)
	renewStatus.Store(0)
	mu.Lock()
	steps = map[uafx.FxEdit]step{}
	mu.Unlock()
	if _, _, err := runCLI(t, "fx", "cm", "edit", "-e", f.url, "E", "--write", "X=3"); err != nil {
		t.Fatal(err)
	}
	if renewals.Load() != 0 {
		t.Fatalf("%d renewals", renewals.Load())
	}
}

// TestE2E_FxCMFakeEditFaults: fx cm edit against a set whose RenewLock,
// Variables and Version fail in the ways a server may: each ends the edit
// with exit 6 and says which step, or makes the outcome unknown.
func TestE2E_FxCMFakeEditFaults(t *testing.T) {
	f := startFakeCM(t)
	e := f.set(f.folders, "F", "F")
	x := f.variable(e, "F.X", f.ns, "X", 1.0, id.HasComponent)
	// The same node below the set twice: one child, not two.
	e.AddReference(x, ua.NewNumericNodeID(0, id.Organizes), true)
	ro := f.variable(e, "F.RO", f.ns, "RO", 1.0, id.HasComponent)
	readOnly := byte(ua.AccessLevelTypeCurrentRead)
	for _, a := range []ua.AttributeID{ua.AttributeIDAccessLevel, ua.AttributeIDUserAccessLevel} {
		if err := ro.SetAttribute(a, server.DataValueFromValue(readOnly)); err != nil {
			t.Fatal(err)
		}
	}
	f.variable(e, "F.Denied", f.ns, "Denied", server.ValueFunc(func() *ua.DataValue {
		return &ua.DataValue{EncodingMask: ua.DataValueStatusCode, Status: ua.StatusBadUserAccessDenied}
	}), id.HasComponent)
	var version atomic.Uint32
	var badVersion atomic.Bool
	version.Store(1)
	f.variable(e, "F.Version", f.ns, "Version", server.ValueFunc(func() *ua.DataValue {
		if badVersion.Load() {
			return &ua.DataValue{EncodingMask: ua.DataValueStatusCode, Status: ua.StatusBadInternalError}
		}
		return server.DataValueFromValue(version.Load())
	}), id.HasProperty)
	lock := f.object(e, "F.Lock", f.di, "Lock", nil)
	var renew atomic.Int32 // the answer of RenewLock, see below
	f.method(lock, "F.Lock.RenewLock", f.di, "RenewLock", func(context.Context, *ua.NodeID, *ua.NodeID, []*ua.Variant) ([]*ua.Variant, ua.StatusCode) {
		switch renew.Load() {
		case 1:
			return nil, ua.StatusUncertain
		case 2:
			return nil, ua.StatusOK
		case 3:
			return []*ua.Variant{ua.MustVariant("OK")}, ua.StatusOK
		default:
			return []*ua.Variant{ua.MustVariant(int32(0))}, ua.StatusOK
		}
	})
	var badValue atomic.Bool
	f.onEdit(func(_ context.Context, _, _ *ua.NodeID, args []*ua.Variant) ([]*ua.Variant, ua.StatusCode) {
		if a, _ := args[0].Value().(int32); uafx.FxEdit(a) == uafx.CommitUpdates {
			version.Add(1)
			if badValue.Load() {
				_ = x.SetAttribute(ua.AttributeIDValue, &ua.DataValue{EncodingMask: ua.DataValueStatusCode, Status: ua.StatusBadInternalError})
			}
		}
		return []*ua.Variant{ua.MustVariant([]ua.StatusCode{ua.StatusOK})}, ua.StatusOK
	})
	prevCleanup, prevRenew := cmCleanup, cmRenewAfter
	t.Cleanup(func() { cmCleanup, cmRenewAfter = prevCleanup, prevRenew })
	cmCleanup, cmRenewAfter = 2*time.Second, 0

	for _, c := range []struct {
		name       string
		write      string
		renew      int32
		badVersion bool
		badValue   bool
		want       int
		outcome    string
		failed     string
		stderr     string
	}{
		{"committed through a node referenced twice", "X=2", 0, false, false, exitOK, "committed", "", ""},
		{"RenewLock Uncertain", "X=3", 1, false, false, exitProtocol, "not-committed", "RenewLock", "Uncertain"},
		{"RenewLock without output", "X=3", 2, false, false, exitProtocol, "not-committed", "RenewLock", "0 output arguments"},
		{"RenewLock String", "X=3", 3, false, false, exitProtocol, "not-committed", "RenewLock", "not an Int32"},
		{"write refused", "RO=2", 0, false, false, exitProtocol, "not-committed", "write RO", "BadNotWritable"},
		{"value read refused", "Denied=2", 0, false, false, exitPermission, "not-committed", "write Denied", "reading"},
		{"Version unreadable", "X=4", 0, true, false, exitProtocol, "unknown", "CommitUpdates", "Version of the set cannot be read"},
		{"value not read back", "X=5", 0, false, true, exitProtocol, "unknown", "CommitUpdates", "cannot be read back"},
	} {
		t.Run(c.name, func(t *testing.T) {
			renew.Store(c.renew)
			badVersion.Store(c.badVersion)
			badValue.Store(c.badValue)
			t.Cleanup(func() { _ = x.SetAttribute(ua.AttributeIDValue, server.DataValueFromValue(1.0)) })
			out, stderr, err := runCLI(t, "fx", "cm", "edit", "-e", f.url, "F", "--write", c.write, "--format", "json")
			if code := ExitCode(err); code != c.want {
				t.Fatalf("exit %d, want %d (%v)\n%s%s", code, c.want, err, out, stderr)
			}
			ed := cmJSON[model.FxCMEdit](t, out)
			if ed.Outcome != c.outcome || ed.Failed != c.failed {
				t.Fatalf("%+v", ed)
			}
			if !strings.Contains(stderr, c.stderr) {
				t.Fatalf("stderr does not say %q: %s", c.stderr, stderr)
			}
		})
	}
}

// TestE2E_FxCMFakeRefused: every fx cm command against a server that
// refuses the Browse or Read a step needs exits 9 (the server refuses the
// user) and names what it could not read.
func TestE2E_FxCMFakeRefused(t *testing.T) {
	t.Parallel()
	w := startCLIWorker(t)
	f := startFakeCM(t)
	e := f.set(f.folders, "G", "G")
	f.variable(e, "G.X", f.ns, "X", 1.0, id.HasComponent)
	sub := f.object(e, "G.Sub", f.ns, "Sub", nil)
	f.variable(sub, "G.Sub.X", f.ns, "X", 1.0, id.HasComponent)
	// A ConnectionsDiagnostics that is no array of structures: listed as
	// unread, not an error.
	f.variable(e, "G.Diag", f.ns, "ConnectionsDiagnostics", "none", id.HasComponent)
	// A Folder beside the set: its type is browsed to tell it from a set.
	f.folder(f.folders, "Area", "Area")
	// A set with two Locks: which one to renew is ambiguous.
	h := f.set(f.folders, "H", "H")
	f.variable(h, "H.X", f.ns, "X", 1.0, id.HasComponent)
	f.object(h, "H.Lock1", f.di, "Lock", nil)
	f.object(h, "H.Lock2", f.di, "Lock", nil)
	lock := f.object(e, "G.Lock", f.di, "Lock", nil)
	f.method(lock, "G.Lock.RenewLock", f.di, "RenewLock", func(context.Context, *ua.NodeID, *ua.NodeID, []*ua.Variant) ([]*ua.Variant, ua.StatusCode) {
		return []*ua.Variant{ua.MustVariant(int32(0))}, ua.StatusOK
	})
	f.onEdit(func(context.Context, *ua.NodeID, *ua.NodeID, []*ua.Variant) ([]*ua.Variant, ua.StatusCode) {
		return []*ua.Variant{ua.MustVariant([]ua.StatusCode{ua.StatusOK})}, ua.StatusOK
	})
	f.onProcess(started)
	// AggregatedCurrentState is optional (§6.7.2): a Boolean, or a String
	// when notBool.
	var notBool atomic.Bool
	f.variable(f.srv.Node(ua.NewNumericNodeID(f.ns, fxcmid.FxRootConnectionManager)), "CM.Aggregated", f.ns, "AggregatedCurrentState", server.ValueFunc(func() *ua.DataValue {
		if notBool.Load() {
			return server.DataValueFromValue("FALSE")
		}
		return server.DataValueFromValue(false)
	}), id.HasComponent)
	if _, _, err := w.run(t, "fx", "cm", "list", "-e", f.url); err != nil {
		t.Fatalf("list: %v", err)
	}
	notBool.Store(true)
	if _, stderr, err := w.run(t, "fx", "cm", "list", "-e", f.url); ExitCode(err) != exitProtocol || !strings.Contains(stderr, "not a Boolean") {
		t.Fatalf("list with a String AggregatedCurrentState: exit %d: %s", ExitCode(err), stderr)
	}
	notBool.Store(false)
	out, _, err := w.run(t, "fx", "cm", "list", "-e", f.url, "--format", "json")
	if err != nil || !strings.Contains(out, "ConnectionsDiagnostics") {
		t.Fatalf("list: %v: ConnectionsDiagnostics not unread: %s", err, out)
	}
	hid := ua.NewStringNodeID(f.ns, "H").String()
	if _, stderr, err := w.run(t, "fx", "cm", "edit", hid, "--write", "X=2", "-e", f.url); ExitCode(err) != exitProtocol || !strings.Contains(stderr, "more than one child") {
		t.Fatalf("edit with two Locks: exit %d: %s", ExitCode(err), stderr)
	}
	gid := ua.NewStringNodeID(f.ns, "G").String()
	t.Cleanup(func() { f.acl.refuse(nil, nil) })
	for _, c := range []struct {
		name         string
		browse, read []string
		args         []string
	}{
		{"list: the sets", []string{"ConnectionConfigurationSets"}, nil, []string{"list"}},
		{"process: the sets", []string{"ConnectionConfigurationSets"}, nil, []string{"process", "--action", "establish", "G"}},
		{"list: a set", []string{"G"}, nil, []string{"list"}},
		{"list: AggregatedCurrentState", nil, []string{"AggregatedCurrentState"}, []string{"list"}},
		{"list: the ConnectionManager", nil, []string{"ConnectionManager"}, []string{"list"}},
		{"list: the state machine", []string{"ConnectionConfigurationSetStateMachine"}, nil, []string{"list"}},
		{"edit: the Lock", []string{"Lock"}, nil, []string{"edit", gid, "--write", "X=2"}},
		{"edit: the path", []string{"G"}, nil, []string{"edit", gid, "--write", "X=2"}},
		{"edit: a path below the set", []string{"Sub"}, nil, []string{"edit", gid, "--write", "Sub/X=2"}},
		{"list: CurrentState", []string{"CurrentState"}, nil, []string{"list"}},
		{"list: the type of a Folder", []string{"FolderType"}, nil, []string{"list"}},
		{"process --wait: the state machine", []string{"ConnectionConfigurationSetStateMachine"}, nil, []string{"process", "--action", "establish", "--wait", "5s", gid}},
	} {
		t.Run(c.name, func(t *testing.T) {
			f.acl.refuse(c.browse, c.read)
			defer f.acl.refuse(nil, nil)
			out, stderr, err := w.run(t, append(append([]string{"fx", "cm"}, c.args...), "-e", f.url)...)
			if code := ExitCode(err); code != exitPermission {
				t.Fatalf("exit %d, want %d (%v)\n%s%s", code, exitPermission, err, out, stderr)
			}
		})
	}
}
