// SPDX-License-Identifier: MIT

package opcuactl

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/otfabric/go-opcua/server"
	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/go-opcua/uafx/fxacid"
	"github.com/otfabric/go-opcua/uafx/fxserver"
	"github.com/otfabric/opcuactl/internal/model"
)

// fxController is the AutomationComponent of go-opcua's uafx/server
// example: a controller with a drive as its Asset and a Motion
// FunctionalEntity with a PubSub ConnectionEndpoint.
//
//go:embed testdata/fx_controller.xml
var fxController []byte

// fxBare is an AutomationComponent without its mandatory members.
//
//go:embed testdata/fx_bare.xml
var fxBare []byte

const (
	fxAppURI       = "urn:opcuactl:test:fx"
	fxControllerNS = "urn:example:uafx:controller"
	fxHealthID     = 1004 // Controller/AggregatedHealth
	fxVersionID    = 1203 // Controller/Motion/AuthorAssignedVersion
	fxManufacturer = 1101 // Controller/Drive/Manufacturer
)

// startFxServer starts a server with the UAFX model, the controller and,
// with bare, an AutomationComponent that misses its mandatory members.
// manufacturer, when not "", replaces the Manufacturer of the drive; tweak
// changes the server after that, with the index of the controller's
// namespace and of the AC namespace.
func startFxServer(t *testing.T, bare bool, manufacturer string, tweak ...func(srv *server.Server, ctl, ac uint16)) string {
	t.Helper()
	srv, err := server.New(
		server.EndPoint("localhost", 0),
		server.InsecureNoSecurity(),
		server.EnableAuthMode(ua.UserTokenTypeAnonymous),
		server.ApplicationURI(fxAppURI),
	)
	if err != nil {
		t.Fatal(err)
	}
	ns, err := fxserver.AddModel(srv)
	if err != nil {
		t.Fatal(err)
	}
	if err := fxserver.ImportNodeSet(srv, fxController); err != nil {
		t.Fatal(err)
	}
	if bare {
		if err := fxserver.ImportNodeSet(srv, fxBare); err != nil {
			t.Fatal(err)
		}
	}
	ctl := fxNamespace(t, srv, fxControllerNS)
	// AggregatedHealthDataType (Part 81 §10.1): DeviceMaintenanceRequired,
	// OperationalWarning; FxVersion (§10.28) 1.2.0.0.
	fxSetStructure(t, srv, ua.NewNumericNodeID(ctl, fxHealthID), ns.AC, fxacid.AggregatedHealthDataTypeEncodingDefaultBinary,
		&struct {
			Device      uint16
			Operational uint32
		}{1 << 2, 1 << 16})
	fxSetStructure(t, srv, ua.NewNumericNodeID(ctl, fxVersionID), ns.AC, fxacid.FxVersionEncodingDefaultBinary,
		&struct{ Major, Minor, Build, SubBuild uint16 }{1, 2, 0, 0})
	if manufacturer != "" {
		n := srv.Node(ua.NewNumericNodeID(ctl, fxManufacturer))
		if err := n.SetAttribute(ua.AttributeIDValue, &ua.DataValue{
			EncodingMask: ua.DataValueValue,
			Value:        ua.MustVariant(&ua.LocalizedText{EncodingMask: ua.LocalizedTextText, Text: manufacturer}),
		}); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range tweak {
		f(srv, ctl, ns.AC)
	}
	if err := srv.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	return fmt.Sprintf("opc.tcp://localhost:%d", srv.Port())
}

func fxNamespace(t *testing.T, srv *server.Server, uri string) uint16 {
	t.Helper()
	for i, ns := range srv.Namespaces() {
		if ns.Name() == uri {
			return uint16(i)
		}
	}
	t.Fatalf("no namespace %s", uri)
	return 0
}

// fxSetStructure sets the Value of nid to the UAFX structure v with the
// binary encoding enc of the AC namespace ac.
func fxSetStructure(t *testing.T, srv *server.Server, nid *ua.NodeID, ac uint16, enc uint32, v any) {
	t.Helper()
	body, err := ua.Encode(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Node(nid).SetAttribute(ua.AttributeIDValue, &ua.DataValue{
		EncodingMask: ua.DataValueValue,
		Value: ua.MustVariant(&ua.ExtensionObject{
			TypeID:       ua.NewNumericExpandedNodeID(ac, enc),
			EncodingMask: ua.ExtensionObjectBinary,
			Value:        body,
		}),
	}); err != nil {
		t.Fatal(err)
	}
}

func fxJSON(t *testing.T, out string) model.FxReport {
	t.Helper()
	var r model.FxReport
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	return r
}

func TestE2E_FxInspect(t *testing.T) {
	t.Parallel()
	// A worker: a parallel test cannot run the CLI in this process.
	w := startCLIWorker(t)
	url := startFxServer(t, false, "")

	t.Run("table", func(t *testing.T) {
		out := w.mustRun(t, "fx", "inspect", "-e", url)
		for _, want := range []string{
			"UAFX server: yes",
			"http://opcfoundation.org/UA/FX/AC/",
			"AutomationComponent 6:Controller (ns=6;i=1000)",
			"health: device DeviceMaintenanceRequired, operational OperationalWarning",
			"asset 6:Drive",
			"functional entity 6:Motion",
			"Motion-1 v1.2.0.0",
			"endpoint ",
			"ConnectionManager ",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("output does not contain %q:\n%s", want, out)
			}
		}
		if strings.Contains(out, "Findings") {
			t.Errorf("findings on the example controller:\n%s", out)
		}
	})

	t.Run("json", func(t *testing.T) {
		r := fxJSON(t, w.mustRun(t, "fx", "inspect", "-e", url, "--format", "json"))
		if !r.IsFxServer || !r.HasFxNamespaces || r.Truncated || len(r.Findings) != 0 || r.FxRoot == "" {
			t.Fatalf("report: %+v", r)
		}
		if len(r.Namespaces) != 4 || !r.Namespaces[0].Present || r.Namespaces[0].Index != 2 {
			t.Errorf("namespaces: %+v", r.Namespaces)
		}
		if len(r.AutomationComponents) != 1 || len(r.ConnectionManagers) != 1 {
			t.Fatalf("components %d, managers %d", len(r.AutomationComponents), len(r.ConnectionManagers))
		}
		ac := r.AutomationComponents[0]
		if ac.BrowseName != "6:Controller" || ac.TypeDefinition == "" {
			t.Errorf("component: %+v", ac.FxNode)
		}
		if h := ac.AggregatedHealth; h == nil || h.DeviceHealthBits != 4 || h.OperationalHealthBits != 1<<16 || h.DeviceHealth != "DeviceMaintenanceRequired" {
			t.Errorf("health: %+v", ac.AggregatedHealth)
		}
		if len(ac.Assets) != 1 || ac.Assets[0].Manufacturer == "" || ac.Assets[0].SerialNumber == "" {
			t.Errorf("assets: %+v", ac.Assets)
		}
		if len(ac.FunctionalEntities) != 1 {
			t.Fatalf("entities: %+v", ac.FunctionalEntities)
		}
		fe := ac.FunctionalEntities[0]
		if fe.AuthorAssignedIdentifier != "Motion-1" || fe.AuthorAssignedVersion != "1.2.0.0" || len(fe.SubFunctionalEntities) != 1 {
			t.Errorf("entity: %+v", fe)
		}
		if len(fe.ConnectionEndpoints) != 1 || fe.ConnectionEndpoints[0].Status == "" {
			t.Errorf("endpoints: %+v", fe.ConnectionEndpoints)
		}
	})

	t.Run("formats", func(t *testing.T) {
		// YAML holds the fields of a node where JSON does, not below a key
		// of their own.
		out := w.mustRun(t, "fx", "inspect", "-e", url, "--format", "yaml")
		if !strings.Contains(out, "\n    - nodeid: ns=6;i=1000\n      browsename: 6:Controller\n") || strings.Contains(out, "fxnode") {
			t.Errorf("yaml:\n%s", out)
		}
		out = w.mustRun(t, "fx", "inspect", "-e", url, "--format", "jsonl")
		if lines := strings.Split(strings.TrimSpace(out), "\n"); len(lines) != 1 {
			t.Errorf("jsonl: %d lines", len(lines))
		} else if r := fxJSON(t, lines[0]); !r.IsFxServer {
			t.Errorf("jsonl: %+v", r)
		}
		out = w.mustRun(t, "fx", "inspect", "-e", url, "--format", "csv")
		if !strings.HasPrefix(out, "hasFxNamespaces,isFxServer,namespaces,fxRoot,automationComponents,connectionManagers,findings,truncated\ntrue,true,") {
			t.Errorf("csv:\n%s", out)
		}
	})

	t.Run("check clean", func(t *testing.T) {
		w.mustRun(t, "fx", "inspect", "-e", url, "--check")
	})

	t.Run("limits", func(t *testing.T) {
		out, errOut, err := w.run(t, "fx", "inspect", "-e", url, "--max-nodes", "1", "--format", "json")
		if ExitCode(err) != exitPartial {
			t.Fatalf("exit %d, want 7: %v\n%s", ExitCode(err), err, errOut)
		}
		if r := fxJSON(t, out); !r.Truncated {
			t.Errorf("not truncated: %+v", r)
		}
		if !strings.Contains(errOut, "incomplete") {
			t.Errorf("stderr: %s", errOut)
		}
	})

	t.Run("bad limits", func(t *testing.T) {
		for _, f := range []string{"--max-components", "--max-children", "--max-depth", "--max-nodes"} {
			_, errOut, err := w.run(t, "fx", "inspect", "-e", url, f, "-1")
			if ExitCode(err) != exitUsage || !strings.Contains(errOut, f) {
				t.Errorf("%s -1: exit %d, stderr %s", f, ExitCode(err), errOut)
			}
		}
	})
}

func TestE2E_FxInspectFindings(t *testing.T) {
	t.Parallel()
	w := startCLIWorker(t)
	url := startFxServer(t, true, "")

	out := w.mustRun(t, "fx", "inspect", "-e", url)
	for _, want := range []string{"AutomationComponent 7:Bare", "Findings:", "Part 81 §6.2.2"} {
		if !strings.Contains(out, want) {
			t.Errorf("output does not contain %q:\n%s", want, out)
		}
	}

	out, errOut, err := w.run(t, "fx", "inspect", "-e", url, "--check", "--format", "json")
	if ExitCode(err) != exitFindings {
		t.Fatalf("--check: exit %d, want 1\n%s", ExitCode(err), errOut)
	}
	r := fxJSON(t, out)
	if len(r.Findings) == 0 || r.Findings[0].Clause == "" || r.Findings[0].NodeID == "" {
		t.Errorf("findings: %+v", r.Findings)
	}
	if !strings.Contains(errOut, "finding(s)") {
		t.Errorf("stderr: %s", errOut)
	}

}

// TestE2E_FxInspectMalformedValue checks a value the server holds but that
// is not what Part 81 defines: an AggregatedHealth whose body is too short
// is a finding, and the report has no health for the component.
func TestE2E_FxInspectMalformedValue(t *testing.T) {
	t.Parallel()
	w := startCLIWorker(t)
	url := startFxServer(t, false, "", func(srv *server.Server, ctl, ac uint16) {
		fxSetStructure(t, srv, ua.NewNumericNodeID(ctl, fxHealthID), ac, fxacid.AggregatedHealthDataTypeEncodingDefaultBinary, &struct{ B uint8 }{1})
	})
	out, errOut, err := w.run(t, "fx", "inspect", "-e", url, "--check", "--format", "json")
	if ExitCode(err) != exitFindings {
		t.Fatalf("exit %d, want 1\n%s", ExitCode(err), errOut)
	}
	r := fxJSON(t, out)
	if r.AutomationComponents[0].AggregatedHealth != nil {
		t.Errorf("health %+v", r.AutomationComponents[0].AggregatedHealth)
	}
	found := false
	for _, f := range r.Findings {
		found = found || strings.Contains(f.Text, "AggregatedHealth")
	}
	if !found {
		t.Errorf("no AggregatedHealth finding: %+v", r.Findings)
	}
}

func TestE2E_FxInspectNotFX(t *testing.T) {
	t.Parallel()
	w := startCLIWorker(t)
	s := startServer(t)

	out, errOut, err := w.run(t, "fx", "inspect", "-e", s.url)
	if err != nil {
		t.Fatalf("exit %d: %v\n%s", ExitCode(err), err, errOut)
	}
	if !strings.Contains(out, "UAFX server: no") || !strings.Contains(out, "absent http://opcfoundation.org/UA/FX/Data/") {
		t.Errorf("output:\n%s", out)
	}
	if !strings.Contains(errOut, "not a UAFX server") {
		t.Errorf("stderr: %s", errOut)
	}

	_, errOut, err = w.run(t, "fx", "inspect", "-e", s.url, "--check")
	if ExitCode(err) != exitFindings || !strings.Contains(errOut, "not a UAFX server") {
		t.Errorf("--check: exit %d, stderr %s", ExitCode(err), errOut)
	}
}

func TestE2E_FxInspectDeadEndpoint(t *testing.T) {
	t.Parallel()
	w := startCLIWorker(t)
	_, _, err := w.run(t, "fx", "inspect", "-e", deadEndpoint, "--timeout", "2s")
	if c := ExitCode(err); c != exitTransport && c != exitTimeout {
		t.Errorf("exit %d, want 4 or 5", c)
	}
}

// TestE2E_FxInspectControlChars checks that the text output shows the
// strings of the server with their control characters escaped, and JSON
// keeps them.
func TestE2E_FxInspectControlChars(t *testing.T) {
	t.Parallel()
	w := startCLIWorker(t)
	url := startFxServer(t, false, "Evil\x1b[31mCorp\n")
	out := w.mustRun(t, "fx", "inspect", "-e", url)
	if strings.Contains(out, "\x1b") || !strings.Contains(out, `Evil\x1b[31mCorp\n`) {
		t.Errorf("output:\n%q", out)
	}
	r := fxJSON(t, w.mustRun(t, "fx", "inspect", "-e", url, "--format", "json"))
	if got := r.AutomationComponents[0].Assets[0].Manufacturer; got != "Evil\x1b[31mCorp\n" {
		t.Errorf("JSON manufacturer %q", got)
	}
}
