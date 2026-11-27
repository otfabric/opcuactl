// SPDX-License-Identifier: MIT

package opcuactl

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/otfabric/go-opcua"
	"github.com/otfabric/go-opcua/server"
	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/go-opcua/uafx"
	"github.com/otfabric/go-opcua/uafx/fxacid"
	"github.com/otfabric/go-opcua/uafx/fxcm"
	"github.com/otfabric/go-opcua/uafx/fxserver"
	"github.com/otfabric/opcuactl/internal/model"
)

const cmAppURI = "urn:opcuactl:test:cm"

// fxCMServer is a server with a UAFX ConnectionManager, as
// examples/uafx/cm of go-opcua runs it.
type fxCMServer struct {
	srv  *server.Server
	m    *fxcm.Manager
	cm   *fxserver.ConnectionManager
	url  string
	deny atomic.Bool // refuse every CM operation
}

func startFxCMServer(t *testing.T) *fxCMServer {
	t.Helper()
	return startFxCMServerWith(t, true, true)
}

// startFxCMServerWith starts the server with or without Edit on its sets and
// the ConnectionManagerConfiguration file.
func startFxCMServerWith(t *testing.T, editable, file bool) *fxCMServer {
	t.Helper()
	srv, err := server.New(
		server.EndPoint("localhost", 0),
		server.InsecureNoSecurity(),
		server.EnableAuthMode(ua.UserTokenTypeAnonymous),
		server.ApplicationURI(cmAppURI),
	)
	if err != nil {
		t.Fatal(err)
	}
	ns, err := fxserver.AddModel(srv, fxserver.ModelCM)
	if err != nil {
		t.Fatal(err)
	}
	m, err := fxcm.New(fxcm.Options{
		Dialer: &fxcm.ClientDialer{Options: []opcua.Option{
			opcua.ApplicationURI(cmAppURI), opcua.InsecureNoSecurity(), opcua.DialTimeout(time.Second),
		}},
		ApplicationURI: cmAppURI,
	})
	if err != nil {
		t.Fatal(err)
	}
	s := &fxCMServer{srv: srv, m: m}
	cm, err := fxserver.EnableConnectionManager(srv, ns, fxserver.ConnectionManagerOptions{
		Manager: m, Editable: editable, File: file,
		Authorizer: func(ctx context.Context, r fxserver.CMRequest) ua.StatusCode {
			if s.deny.Load() {
				return ua.StatusBadUserAccessDenied
			}
			return ua.StatusOK
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	s.cm = cm
	if err := srv.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cm.Close()
		_ = m.Close()
		_ = srv.Close()
	})
	s.url = fmt.Sprintf("opc.tcp://localhost:%d", srv.Port())
	return s
}

// cmSetConf is a set with one Connection between two AutomationComponents
// that are not running (the ports are closed), in the Folder Plant.
func cmSetConf(name string) *uafx.ConnectionConfigurationSetConf {
	return cmSetConfAt(name, 1, 2)
}

// cmSetConfAt is cmSetConf with its AutomationComponents on the ports p1
// and p2 of 127.0.0.1.
func cmSetConfAt(name string, p1, p2 int) *uafx.ConnectionConfigurationSetConf {
	addr := func(n string, port int) *uafx.ServerAddressConf {
		return &uafx.ServerAddressConf{BrowseName: n, Address: fmt.Sprintf("opc.tcp://127.0.0.1:%d", port),
			SecurityMode: ua.MessageSecurityModeNone, ServerURI: "urn:example:" + n,
			Namespaces: []string{"http://opcfoundation.org/UA/", "urn:example:uafx:controller", fxacid.NamespaceURI}}
	}
	node := func(id uint32) *uafx.NodeIdentifier { return &uafx.NodeIdentifier{Node: ua.NewNumericNodeID(1, id)} }
	endpoint := func(ac int32, n string) *uafx.ConnectionEndpointConfigurationConf {
		e := &uafx.ConnectionEndpointConfigurationConf{
			FunctionalEntityNode: node(1200), Name: n, AutomationComponentIndex: ac,
			ConnectionEndpointTypeID: ua.NewNumericNodeID(2, fxacid.PubSubConnectionEndpointType),
		}
		if ac == 0 {
			out := int32(0)
			e.OutputVariableIDs, e.OutboundFlowIndex = []*uafx.NodeIdentifier{node(1201)}, &out
		} else {
			e.InputVariableIDs, e.InboundFlowIndex = []*uafx.NodeIdentifier{node(1202)}, []int32{0, 0}
		}
		return e
	}
	return &uafx.ConnectionConfigurationSetConf{
		BrowseName:                       name,
		ConnectionConfigurationSetFolder: []string{"Plant"},
		ServerAddresses:                  []*uafx.ServerAddressConf{addr("PLC1", p1), addr("PLC2", p2)},
		AutomationComponentConfigurations: []*uafx.AutomationComponentConfigurationConf{
			{BrowseName: "AC1", AutomationComponentNode: node(1000), ServerAddressIndex: 0},
			{BrowseName: "AC2", AutomationComponentNode: node(1000), ServerAddressIndex: 1},
		},
		CommunicationFlows: []uafx.CommunicationFlowConfigurationConf{&uafx.PubSubCommunicationFlowConfigurationConf{
			BrowseName:               "Flow",
			PublishingInterval:       ptr(100.0),
			PublishingIntervalModify: ptr(true),
			Address:                  &uafx.AddressSelection{Address: ua.NewExtensionObject(&ua.NetworkAddressURLDataType{URL: "opc.udp://239.0.0.1:4840"})},
			SubscriberConfigurations: []*uafx.SubscriberConfigurationConf{{BrowseName: "Sub", MessageReceiveTimeout: 100}},
		}},
		Connections: []*uafx.ConnectionConfigurationConf{{BrowseName: "C1", Endpoint1: endpoint(0, "Out"), Endpoint2: endpoint(1, "In")}},
	}
}

func writeSetFile(t *testing.T, sets ...*uafx.ConnectionConfigurationSetConf) string {
	t.Helper()
	b, err := fxcm.EncodeFile(sets)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "sets.bin")
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func cmJSON[T any](t *testing.T, out string) T {
	t.Helper()
	var v T
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("decoding %q: %v", out, err)
	}
	return v
}

// TestE2E_FxCM: import, list, export, edit, process with --wait and remove
// against a ConnectionManager whose AutomationComponents are not running.
func TestE2E_FxCM(t *testing.T) {
	t.Parallel()
	// A worker: a parallel test cannot run the CLI in this process.
	w := startCLIWorker(t)
	s := startFxCMServer(t)
	file := writeSetFile(t, cmSetConf("Line1"), cmSetConf("Line2"))

	// import: add both sets.
	out, stderr, err := w.run(t, "fx", "cm", "import", "-e", s.url, "--file", file, "--op", "add", "--require-complete", "--format", "json")
	if err != nil {
		t.Fatalf("import: %v\n%s", err, stderr)
	}
	imp := cmJSON[model.FxCMImport](t, out)
	if !imp.ChangesApplied || len(imp.Operations) != 2 || imp.Operations[0].Path != "Plant/Line1" || imp.Operations[0].Status != "Good" || imp.Operations[0].NodeID == "" {
		t.Fatalf("import: %+v", imp)
	}
	// The same sets again: their Names are taken.
	_, _, err = w.run(t, "fx", "cm", "import", "-e", s.url, "--file", file, "--op", "add", "--format", "json")
	if code := ExitCode(err); code != exitProtocol {
		t.Fatalf("re-import: exit %d (%v)", code, err)
	}

	// list: both sets, Ready, Version 1, not in edit.
	out, stderr, err = w.run(t, "fx", "cm", "list", "-e", s.url, "--format", "json")
	if err != nil {
		t.Fatalf("list: %v\n%s", err, stderr)
	}
	l := cmJSON[model.FxCMList](t, out)
	if len(l.Sets) != 2 || l.AggregatedCurrentState == nil || *l.AggregatedCurrentState {
		t.Fatalf("list: %+v", l)
	}
	one := l.Sets[0]
	if one.Path != "Plant/Line1" || one.StateID != "Ready" || one.Version == nil || *one.Version != 1 || one.Edit == nil || *one.Edit {
		t.Fatalf("list: %+v", one)
	}
	if len(one.Connections) != 1 || one.Connections[0].Name != "C1" {
		t.Fatalf("connections: %+v", one.Connections)
	}
	out, _, err = w.run(t, "fx", "cm", "list", "-e", s.url)
	if err != nil || !strings.Contains(out, "Plant/Line2") || !strings.Contains(out, "Ready") {
		t.Fatalf("list table: %v\n%s", err, out)
	}

	// export: the file holds both sets.
	exported := filepath.Join(t.TempDir(), "out.bin")
	out, stderr, err = w.run(t, "fx", "cm", "export", "-e", s.url, "--out", exported, "--format", "json")
	if err != nil {
		t.Fatalf("export: %v\n%s", err, stderr)
	}
	ex := cmJSON[model.FxCMExport](t, out)
	if len(ex.Sets) != 2 || ex.Sets[1].Path != "Plant/Line2" || ex.Sets[1].Connections != 1 {
		t.Fatalf("export: %+v", ex)
	}
	b, err := os.ReadFile(exported)
	if err != nil {
		t.Fatal(err)
	}
	if sets, err := fxcm.DecodeFile(b); err != nil || len(sets) != 2 {
		t.Fatalf("exported file: %d sets, %v", len(sets), err)
	}
	out, _, err = w.run(t, "fx", "cm", "export", "-e", s.url)
	if err != nil || !strings.Contains(out, "Plant/Line1") {
		t.Fatalf("export table: %v\n%s", err, out)
	}

	// edit: a new PublishingInterval becomes Version 2.
	out, stderr, err = w.run(t, "fx", "cm", "edit", "-e", s.url, "Plant/Line1", "--write", "Flow/PublishingInterval=250", "--format", "json")
	if err != nil {
		t.Fatalf("edit: %v\n%s", err, stderr)
	}
	ed := cmJSON[model.FxCMEdit](t, out)
	if !ed.Committed || ed.Version == nil || *ed.Version != 2 || len(ed.Writes) != 1 || ed.Writes[0].Status != "Good" {
		t.Fatalf("edit: %+v", ed)
	}
	cfg, ok := s.m.Config(fxcm.SetKey{Folder: []string{"Plant"}, Name: "Line1"})
	if !ok {
		t.Fatal("Line1 is gone")
	}
	if pi := cfg.Conf().CommunicationFlows[0].(*uafx.PubSubCommunicationFlowConfigurationConf).PublishingInterval; pi == nil || *pi != 250 {
		t.Fatalf("PublishingInterval %v", pi)
	}
	// An unknown Variable: usage, and the edit is discarded.
	out, _, err = w.run(t, "fx", "cm", "edit", "-e", s.url, "Plant/Line1", "--write", "Flow/Nope=1", "--format", "json")
	if code := ExitCode(err); code != exitUsage {
		t.Fatalf("edit unknown: exit %d (%v)", code, err)
	}
	if ed := cmJSON[model.FxCMEdit](t, out); ed.Committed || !ed.Discarded {
		t.Fatalf("edit unknown: %+v", ed)
	}
	// A value the Variable does not take: the write fails, the edit is
	// discarded, the Version stays.
	out, _, err = w.run(t, "fx", "cm", "edit", "-e", s.url, "Plant/Line1", "--write", "Flow/PublishingInterval=fast", "--format", "json")
	if code := ExitCode(err); code != exitUsage {
		t.Fatalf("edit bad value: exit %d (%v)", code, err)
	}
	if ed := cmJSON[model.FxCMEdit](t, out); !ed.Discarded {
		t.Fatalf("edit bad value: %+v", ed)
	}
	out, _, err = w.run(t, "fx", "cm", "edit", "-e", s.url, "Plant/Line1", "--write", "Flow/PublishingInterval=300")
	if err != nil || !strings.Contains(out, "PublishingInterval") {
		t.Fatalf("edit table: %v\n%s", err, out)
	}

	// process: the AutomationComponents are not running, so the run ends in
	// Error: exit 6 with --wait.
	setID := imp.Operations[0].NodeID
	out, stderr, err = w.run(t, "fx", "cm", "process", "-e", s.url, "--action", "establish-enabled", setID, "--wait", "30s", "--format", "json")
	if code := ExitCode(err); code != exitProtocol {
		t.Fatalf("process: exit %d (%v)\n%s", code, err, stderr)
	}
	pr := cmJSON[model.FxCMCallResult](t, out)
	if pr.Status != "Good" || len(pr.Sets) != 1 || pr.Sets[0].State != "Error" {
		t.Fatalf("process: %+v", pr)
	}
	out, _, err = w.run(t, "fx", "cm", "list", "-e", s.url, "--format", "json")
	if err != nil {
		t.Fatal(err)
	}
	l = cmJSON[model.FxCMList](t, out)
	if l.AggregatedCurrentState == nil || !*l.AggregatedCurrentState || l.Sets[0].StateID != "Error" || len(l.Sets[0].Diagnostics) != 1 {
		t.Fatalf("list after the failed run: %+v", l)
	}
	if l.Sets[0].Connections[0].ProcessingResult == "" || l.Sets[0].Connections[0].ProcessingResult == "Good" {
		t.Fatalf("ProcessingResult: %+v", l.Sets[0].Connections[0])
	}

	// remove: nothing was established, the set goes back to Ready.
	out, stderr, err = w.run(t, "fx", "cm", "process", "-e", s.url, "--action", "remove", "Plant/Line1", "--wait", "30s")
	if err != nil || !strings.Contains(out, "Ready") {
		t.Fatalf("remove: %v\n%s%s", err, out, stderr)
	}
	// Without --wait the call result alone.
	out, stderr, err = w.run(t, "fx", "cm", "process", "-e", s.url, "--action", "remove", "Plant/Line2", "--format", "json")
	if err != nil {
		t.Fatalf("process without --wait: %v\n%s", err, stderr)
	}
	if pr := cmJSON[model.FxCMCallResult](t, out); pr.Sets[0].Status != "Good" || pr.Sets[0].State != "" {
		t.Fatalf("process without --wait: %+v", pr)
	}

	// import remove: both sets go.
	out, stderr, err = w.run(t, "fx", "cm", "import", "-e", s.url, "--file", file, "--op", "remove,remove", "--format", "json")
	if err != nil {
		t.Fatalf("import remove: %v\n%s", err, stderr)
	}
	if imp := cmJSON[model.FxCMImport](t, out); !imp.ChangesApplied || imp.Operations[0].NodeID != "" || imp.Operations[1].Operation != "ElementRemove" {
		t.Fatalf("import remove: %+v", imp)
	}
	if keys := s.m.Keys(); len(keys) != 0 {
		t.Fatalf("sets left: %v", keys)
	}
}

// TestE2E_FxCMErrors: exit codes of fx cm.
func TestE2E_FxCMErrors(t *testing.T) {
	t.Parallel()
	// A worker: a parallel test cannot run the CLI in this process.
	w := startCLIWorker(t)
	s := startFxCMServer(t)
	file := writeSetFile(t, cmSetConf("Line1"))
	if _, _, err := w.run(t, "fx", "cm", "import", "-e", s.url, "--file", file, "--op", "add"); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(t.TempDir(), "bad.bin")
	if err := os.WriteFile(bad, []byte("not a file"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A file that decodes but holds no set.
	if _, stderr, err := w.run(t, "fx", "cm", "import", "-e", s.url, "--file", writeSetFile(t), "--op", "add"); ExitCode(err) != exitInvalidInput || !strings.Contains(stderr+fmt.Sprint(err), "no ConnectionConfigurationSet") {
		t.Fatalf("import of a file with no set: exit %d (%v): %s", ExitCode(err), err, stderr)
	}
	other := startFxConnServer(t) // a UAFX server without a ConnectionManager

	for _, c := range []struct {
		name string
		args []string
		want int
	}{
		{"unknown action", []string{"process", "--action", "nope", "Plant/Line1"}, exitUsage},
		{"negative wait", []string{"process", "--action", "remove", "--wait", "-1s", "Plant/Line1"}, exitUsage},
		{"unknown set", []string{"process", "--action", "remove", "Plant/None"}, exitUsage},
		{"bad NodeId", []string{"process", "--action", "remove", "ns=x;i=1"}, exitUsage},
		{"unknown NodeId", []string{"process", "--action", "remove", "ns=1;i=424242"}, exitProtocol},
		{"edit without write", []string{"edit", "Plant/Line1"}, exitUsage},
		{"edit bad write", []string{"edit", "Plant/Line1", "--write", "novalue"}, exitUsage},
		{"edit unknown NodeId", []string{"edit", "ns=1;i=424242", "--write", "Flow/PublishingInterval=1"}, exitProtocol},
		{"import missing file", []string{"import", "--file", filepath.Join(t.TempDir(), "none"), "--op", "add"}, exitInvalidInput},
		{"import bad file", []string{"import", "--file", bad, "--op", "add"}, exitInvalidInput},
		{"import op count", []string{"import", "--file", file, "--op", "add,add"}, exitUsage},
		{"import bad op", []string{"import", "--file", file, "--op", "merge"}, exitUsage},
		{"export unwritable", []string{"export", "--out", filepath.Join(t.TempDir(), "no", "dir", "x.bin")}, exitInvalidInput},
	} {
		t.Run(c.name, func(t *testing.T) {
			args := append([]string{"fx", "cm"}, c.args...)
			args = append(args, "-e", s.url)
			_, _, err := w.run(t, args...)
			if code := ExitCode(err); code != c.want {
				t.Fatalf("exit %d, want %d (%v)", code, c.want, err)
			}
		})
	}

	t.Run("no ConnectionManager", func(t *testing.T) {
		_, _, err := w.run(t, "fx", "cm", "list", "-e", other.url)
		if code := ExitCode(err); code != exitUnsupported {
			t.Fatalf("exit %d (%v)", code, err)
		}
	})

	t.Run("denied", func(t *testing.T) {
		s.deny.Store(true)
		defer s.deny.Store(false)
		for _, args := range [][]string{
			{"process", "--action", "remove", "Plant/Line1"},
			{"edit", "Plant/Line1", "--write", "Flow/PublishingInterval=1"},
			{"import", "--file", file, "--op", "replace"},
		} {
			_, _, err := w.run(t, append(append([]string{"fx", "cm"}, args...), "-e", s.url)...)
			if code := ExitCode(err); code != exitPermission {
				t.Fatalf("%v: exit %d (%v)", args, code, err)
			}
		}
	})
}

// TestE2E_FxCMWaitTimeout: --wait ends with exit 5 while a set is still
// Processing: its AutomationComponents accept TCP and never answer.
func TestE2E_FxCMWaitTimeout(t *testing.T) {
	t.Parallel()
	w := startCLIWorker(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var held []net.Conn
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			held = append(held, c)
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		<-done
		for _, c := range held {
			_ = c.Close()
		}
	})
	port := ln.Addr().(*net.TCPAddr).Port
	s := startFxCMServer(t)
	file := writeSetFile(t, cmSetConfAt("Slow", port, port))
	if _, _, err := w.run(t, "fx", "cm", "import", "-e", s.url, "--file", file, "--op", "add"); err != nil {
		t.Fatal(err)
	}
	out, _, err := w.run(t, "fx", "cm", "process", "-e", s.url, "--action", "establish", "Plant/Slow", "--wait", "100ms", "--format", "json")
	if code := ExitCode(err); code != exitTimeout {
		t.Fatalf("exit %d (%v)\n%s", code, err, out)
	}
	if pr := cmJSON[model.FxCMCallResult](t, out); pr.Sets[0].State != "Processing" {
		t.Fatalf("%+v", pr)
	}
	// A second run while the first is Processing is refused (Bad_InvalidState).
	_, _, err = w.run(t, "fx", "cm", "process", "-e", s.url, "--action", "establish", "Plant/Slow")
	if code := ExitCode(err); code != exitProtocol {
		t.Fatalf("second run: exit %d (%v)", code, err)
	}
	// Let the run end before the server closes.
	deadline := time.Now().Add(30 * time.Second)
	for {
		st, _ := s.m.Status(fxcm.SetKey{Folder: []string{"Plant"}, Name: "Slow"})
		if st.State != fxcm.StateProcessing || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestE2E_FxCMWithoutFileOrEdit: a ConnectionManager without the file
// (F.3 is optional) and with sets that cannot be edited.
func TestE2E_FxCMWithoutFileOrEdit(t *testing.T) {
	t.Parallel()
	w := startCLIWorker(t)
	s := startFxCMServerWith(t, false, false)
	if _, err := s.m.Add(cmSetConf("Line1")); err != nil {
		t.Fatal(err)
	}
	s.cm.Flush()
	file := writeSetFile(t, cmSetConf("Line2"))
	for _, args := range [][]string{
		{"export"},
		{"import", "--file", file, "--op", "add"},
	} {
		_, _, err := w.run(t, append(append([]string{"fx", "cm"}, args...), "-e", s.url)...)
		if code := ExitCode(err); code != exitUnsupported {
			t.Fatalf("%v: exit %d (%v)", args, code, err)
		}
	}
	out, _, err := w.run(t, "fx", "cm", "edit", "-e", s.url, "Plant/Line1", "--write", "Flow/PublishingInterval=1", "--format", "json")
	if code := ExitCode(err); code != exitProtocol {
		t.Fatalf("edit: exit %d (%v)", code, err)
	}
	if ed := cmJSON[model.FxCMEdit](t, out); ed.Failed != "StartEditing" || ed.Committed {
		t.Fatalf("edit: %+v", ed)
	}
	out, _, err = w.run(t, "fx", "cm", "list", "-e", s.url, "--format", "json")
	if err != nil {
		t.Fatal(err)
	}
	if l := cmJSON[model.FxCMList](t, out); len(l.Sets) != 1 || l.Sets[0].Edit != nil {
		t.Fatalf("list: %+v", l)
	}
}

// TestE2E_FxCMBounds: the bounds of what fx cm reads (exit 7), and files
// moved in several chunks.
func TestE2E_FxCMBounds(t *testing.T) {
	s := startFxCMServer(t)
	file := writeSetFile(t, cmSetConf("Line1"), cmSetConf("Line2"))
	prevChunk, prevSets, prevDepth, prevSize := cmChunk, cmMaxSets, cmMaxDepth, cmMaxFileSize
	t.Cleanup(func() { cmChunk, cmMaxSets, cmMaxDepth, cmMaxFileSize = prevChunk, prevSets, prevDepth, prevSize })

	cmChunk = 100 // the file is a few KiB: many Write and Read calls
	if _, stderr, err := runCLI(t, "fx", "cm", "import", "-e", s.url, "--file", file, "--op", "add"); err != nil {
		t.Fatalf("import: %v\n%s", err, stderr)
	}
	out, _, err := runCLI(t, "fx", "cm", "export", "-e", s.url, "--format", "json")
	if err != nil {
		t.Fatal(err)
	}
	if ex := cmJSON[model.FxCMExport](t, out); len(ex.Sets) != 2 {
		t.Fatalf("export: %+v", ex)
	}

	cmMaxFileSize = 150
	if _, _, err := runCLI(t, "fx", "cm", "export", "-e", s.url); ExitCode(err) != exitPartial {
		t.Fatalf("file size: exit %d (%v)", ExitCode(err), err)
	}
	cmMaxFileSize = prevSize

	cmMaxSets = 1
	out, _, err = runCLI(t, "fx", "cm", "list", "-e", s.url, "--format", "json")
	if ExitCode(err) != exitPartial {
		t.Fatalf("sets: exit %d (%v)", ExitCode(err), err)
	}
	if l := cmJSON[model.FxCMList](t, out); !l.Truncated || len(l.Sets) != 1 {
		t.Fatalf("sets: %+v", l)
	}
	cmMaxSets = prevSets

	cmMaxDepth = 0 // the sets are in the Folder Plant
	out, _, err = runCLI(t, "fx", "cm", "list", "-e", s.url, "--format", "json")
	if ExitCode(err) != exitPartial {
		t.Fatalf("depth: exit %d (%v)", ExitCode(err), err)
	}
	if l := cmJSON[model.FxCMList](t, out); !l.Truncated || len(l.Sets) != 0 {
		t.Fatalf("depth: %+v", l)
	}
	cmMaxDepth = prevDepth

	// A value as an OPC UA JSON Variant (Double, UaType 11), and a bad one.
	out, stderr, err := runCLI(t, "fx", "cm", "edit", "-e", s.url, "Plant/Line1", "--write", `Flow/PublishingInterval={"UaType":11,"Value":75}`, "--format", "json")
	if err != nil {
		t.Fatalf("edit json: %v\n%s", err, stderr)
	}
	if ed := cmJSON[model.FxCMEdit](t, out); !ed.Committed {
		t.Fatalf("edit json: %+v", ed)
	}
	if _, _, err := runCLI(t, "fx", "cm", "edit", "-e", s.url, "Plant/Line1", "--write", `Flow/PublishingInterval={bad`); ExitCode(err) != exitUsage {
		t.Fatalf("edit bad json: exit %d (%v)", ExitCode(err), err)
	}
	// A Variable whose type opcuactl does not convert: the ServerAddress
	// is a structure.
	if _, _, err := runCLI(t, "fx", "cm", "edit", "-e", s.url, "Plant/Line1", "--write", "PLC1=x"); ExitCode(err) != exitUsage {
		t.Fatalf("edit structure: exit %d (%v)", ExitCode(err), err)
	}
}

// TestE2E_FxCMModelOnly: a server with the CM model and no ConnectionManager
// behind it lists no sets.
func TestE2E_FxCMModelOnly(t *testing.T) {
	t.Parallel()
	w := startCLIWorker(t)
	srv, err := server.New(server.EndPoint("localhost", 0), server.InsecureNoSecurity(), server.EnableAuthMode(ua.UserTokenTypeAnonymous), server.ApplicationURI("urn:opcuactl:test:cm-model"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fxserver.AddModel(srv, fxserver.ModelCM); err != nil {
		t.Fatal(err)
	}
	if err := srv.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	url := fmt.Sprintf("opc.tcp://localhost:%d", srv.Port())
	out, stderr, err := w.run(t, "fx", "cm", "list", "-e", url, "--format", "json")
	if err != nil {
		t.Fatalf("list: %v\n%s", err, stderr)
	}
	if l := cmJSON[model.FxCMList](t, out); len(l.Sets) != 0 || l.ConnectionManager == "" {
		t.Fatalf("list: %+v", l)
	}
	if _, _, err = w.run(t, "fx", "cm", "export", "-e", url); ExitCode(err) != exitUnsupported {
		t.Fatalf("export: exit %d (%v)", ExitCode(err), err)
	}
	if _, _, err = w.run(t, "fx", "cm", "process", "-e", url, "--action", "remove", "Plant/X"); ExitCode(err) != exitUsage {
		t.Fatalf("process: exit %d (%v)", ExitCode(err), err)
	}
}

// TestE2E_FxCMFileFaults: fx cm export and import against file Methods
// that fail or answer what OPC 10000-20 §4.2 and Table F.39 do not allow.
// CloseAndUpdate is called once whatever it answers. The cases that need
// smaller package bounds are in TestE2E_FxCMFileBounds.
func TestE2E_FxCMFileFaults(t *testing.T) {
	t.Parallel()
	file := writeSetFile(t, cmSetConf("Line1"))
	type handler = func(ctx context.Context, obj, method *ua.NodeID, args []*ua.Variant) ([]*ua.Variant, ua.StatusCode)
	fail := func(sc ua.StatusCode) handler {
		return func(context.Context, *ua.NodeID, *ua.NodeID, []*ua.Variant) ([]*ua.Variant, ua.StatusCode) {
			return nil, sc
		}
	}
	answer := func(out ...any) handler {
		return func(context.Context, *ua.NodeID, *ua.NodeID, []*ua.Variant) ([]*ua.Variant, ua.StatusCode) {
			vs := make([]*ua.Variant, len(out))
			for i, v := range out {
				vs[i] = ua.MustVariant(v)
			}
			return vs, ua.StatusOK
		}
	}
	var calls atomic.Int32
	// CloseAndUpdate refused: counted, then answered as fail answers.
	refused := fail(ua.StatusBadTypeMismatch)
	typeMismatch := func(ctx context.Context, obj, method *ua.NodeID, args []*ua.Variant) ([]*ua.Variant, ua.StatusCode) {
		calls.Add(1)
		return refused(ctx, obj, method, args)
	}
	byteString := func(_ context.Context, _, _ *ua.NodeID, args []*ua.Variant) ([]*ua.Variant, ua.StatusCode) {
		if len(args) != 3 {
			return nil, ua.StatusBadArgumentsMissing
		}
		if b, ok := args[2].Value().([]byte); !ok || len(b) != 1 || b[0] != byte(uafx.ElementAdd) {
			return nil, ua.StatusBadTypeMismatch
		}
		return []*ua.Variant{ua.MustVariant(true), ua.MustVariant([]ua.StatusCode{ua.StatusOK}), ua.MustVariant([]*ua.NodeID{ua.NewNumericNodeID(1, 7)})}, ua.StatusOK
	}
	// readOnce answers the first Read with body and the next with no data
	// (the end of the file).
	readOnce := func(body any) handler {
		var n atomic.Int32
		return func(context.Context, *ua.NodeID, *ua.NodeID, []*ua.Variant) ([]*ua.Variant, ua.StatusCode) {
			if n.Add(1) == 1 {
				return []*ua.Variant{ua.MustVariant(body)}, ua.StatusOK
			}
			return []*ua.Variant{ua.MustVariant([]byte{})}, ua.StatusOK
		}
	}
	readArray := func() handler {
		body, err := fxcm.EncodeFile([]*uafx.ConnectionConfigurationSetConf{cmSetConf("A")})
		if err != nil {
			t.Fatal(err)
		}
		return readOnce(ua.ByteArray(body))
	}
	type fault struct {
		name   string
		method string
		h      handler
		args   []string
		want   int
	}
	cases := []fault{
		{"open refused", "Open", fail(ua.StatusBadInvalidState), []string{"export"}, exitProtocol},
		{"open denied", "Open", fail(ua.StatusBadUserAccessDenied), []string{"import", "--file", file, "--op", "add"}, exitPermission},
		{"open no handle", "Open", answer(), []string{"export"}, exitProtocol},
		{"open string handle", "Open", answer("h"), []string{"export"}, exitProtocol},
		{"read fails", "Read", fail(ua.StatusBadInvalidArgument), []string{"export"}, exitProtocol},
		{"read byte array", "Read", readArray(), []string{"export"}, exitOK},
		{"read not a file", "Read", readOnce([]byte("not a file")), []string{"export"}, exitInvalidInput},
		{"read no data", "Read", answer(), []string{"export"}, exitProtocol},
		{"read wrong type", "Read", answer(int32(7)), []string{"export"}, exitProtocol},
		{"close fails", "Close", fail(ua.StatusBadInvalidArgument), []string{"export"}, exitProtocol},
		{"write fails", "Write", fail(ua.StatusBadResourceUnavailable), []string{"import", "--file", file, "--op", "add"}, exitProtocol},
		{"update outputs", "CloseAndUpdate", answer(true), []string{"import", "--file", file, "--op", "add"}, exitProtocol},
		{"update results", "CloseAndUpdate", answer(true, []ua.StatusCode{}, []*ua.NodeID{}), []string{"import", "--file", file, "--op", "add"}, exitProtocol},
		{"update not decoded", "CloseAndUpdate", typeMismatch, []string{"import", "--file", file, "--op", "add"}, exitProtocol},
		{"update byte string", "CloseAndUpdate", byteString, []string{"import", "--file", file, "--op", "add"}, exitOK},
		{"update applied type", "CloseAndUpdate", answer("yes", []ua.StatusCode{ua.StatusOK}, []*ua.NodeID{}), []string{"import", "--file", file, "--op", "add"}, exitProtocol},
		{"update contradiction", "CloseAndUpdate", answer(true, []ua.StatusCode{ua.StatusBadInvalidArgument}, []*ua.NodeID{}), []string{"import", "--file", file, "--op", "add"}, exitProtocol},
		{"update objects", "CloseAndUpdate", answer(true, []ua.StatusCode{ua.StatusOK}, []*ua.NodeID{ua.NewNumericNodeID(1, 1), ua.NewNumericNodeID(1, 2)}), []string{"import", "--file", file, "--op", "add"}, exitProtocol},
	}
	// The cases of one Method share a server and a CLI worker and run one
	// after another, the Method answered by the handler of the running
	// case; the Methods run in parallel. A server and a worker cost about
	// 2 s each under the race detector, which a server per case spent 18
	// times on a 2-CPU runner.
	var methods []string
	byMethod := map[string][]fault{}
	for _, c := range cases {
		if byMethod[c.method] == nil {
			methods = append(methods, c.method)
		}
		byMethod[c.method] = append(byMethod[c.method], c)
	}
	t.Run("cases", func(group *testing.T) {
		for _, m := range methods {
			group.Run(m, func(t *testing.T) {
				t.Parallel()
				w := startCLIWorker(t)
				s := startFxCMServer(t)
				ns := s.cmNS(t)
				var current atomic.Pointer[handler]
				s.srv.RegisterMethod(ua.NewStringNodeID(ns, "cm.File"), ua.NewStringNodeID(ns, "cm.File."+m),
					func(ctx context.Context, obj, method *ua.NodeID, args []*ua.Variant) ([]*ua.Variant, ua.StatusCode) {
						return (*current.Load())(ctx, obj, method, args)
					})
				for _, c := range byMethod[m] {
					t.Run(c.name, func(t *testing.T) {
						current.Store(&c.h)
						out, stderr, err := w.run(t, append(append([]string{"fx", "cm"}, c.args...), "-e", s.url)...)
						if code := ExitCode(err); code != c.want {
							t.Fatalf("exit %d, want %d (%v)\n%s%s", code, c.want, err, out, stderr)
						}
					})
				}
			})
		}
	})
	if calls.Load() != 1 {
		t.Fatalf("CloseAndUpdate called %d times, want 1: it is not retried", calls.Load())
	}
}

// TestE2E_FxCMFileBounds: the file Method faults that need smaller package
// bounds, so they run in this process, one at a time.
func TestE2E_FxCMFileBounds(t *testing.T) {
	file := writeSetFile(t, cmSetConf("Line1"))
	read := func(body []byte) func(context.Context, *ua.NodeID, *ua.NodeID, []*ua.Variant) ([]*ua.Variant, ua.StatusCode) {
		return func(context.Context, *ua.NodeID, *ua.NodeID, []*ua.Variant) ([]*ua.Variant, ua.StatusCode) {
			return []*ua.Variant{ua.MustVariant(body)}, ua.StatusOK
		}
	}
	for _, c := range []struct {
		name  string
		bound *int
		value int
		body  []byte
		want  int
	}{
		// A server that never ends the file stops at the bound.
		{"read without end", &cmMaxFileSize, 10, []byte("x"), exitPartial},
		// More bytes than the Length asked for.
		{"read too much", &cmChunk, 1, make([]byte, 2), exitProtocol},
	} {
		t.Run(c.name, func(t *testing.T) {
			prev := *c.bound
			*c.bound = c.value
			t.Cleanup(func() { *c.bound = prev })
			s := startFxCMServer(t)
			ns := s.cmNS(t)
			s.srv.RegisterMethod(ua.NewStringNodeID(ns, "cm.File"), ua.NewStringNodeID(ns, "cm.File.Read"), read(c.body))
			out, stderr, err := runCLI(t, "fx", "cm", "export", "-e", s.url)
			if code := ExitCode(err); code != c.want {
				t.Fatalf("exit %d, want %d (%v)\n%s%s", code, c.want, err, out, stderr)
			}
		})
	}

	// An import larger than the bound is refused before it is sent.
	prev := cmMaxFileSize
	cmMaxFileSize = 100
	t.Cleanup(func() { cmMaxFileSize = prev })
	s := startFxCMServer(t)
	if _, _, err := runCLI(t, "fx", "cm", "import", "-e", s.url, "--file", file, "--op", "add"); ExitCode(err) != exitInvalidInput {
		t.Fatalf("large import: exit %d (%v)", ExitCode(err), err)
	}
}

// cmNS is the index of the CM namespace on s.
func (s *fxCMServer) cmNS(t *testing.T) uint16 {
	t.Helper()
	for i, n := range s.srv.Namespaces() {
		if n.Name() == "http://opcfoundation.org/UA/FX/CM/" {
			return uint16(i)
		}
	}
	t.Fatal("no CM namespace")
	return 0
}
