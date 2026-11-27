// SPDX-License-Identifier: MIT

package opcuactl

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/go-opcua/uafx"
	"github.com/otfabric/opcuactl/internal/model"
)

// TestFxResult checks the exit of fx inspect: --check reports a server
// that is not a UAFX server and findings with 1, which wins over 7 for a
// report a limit cut short.
func TestFxResult(t *testing.T) {
	fx := model.FxReport{IsFxServer: true}
	finding := []model.FxFinding{{Clause: "6.2.2", Text: "x"}}
	for _, c := range []struct {
		name  string
		rep   model.FxReport
		check bool
		want  int
	}{
		{"clean", fx, false, exitOK},
		{"clean check", fx, true, exitOK},
		{"not fx", model.FxReport{}, false, exitOK},
		{"not fx check", model.FxReport{}, true, exitFindings},
		{"findings", model.FxReport{IsFxServer: true, Findings: finding}, false, exitOK},
		{"findings check", model.FxReport{IsFxServer: true, Findings: finding}, true, exitFindings},
		{"truncated", model.FxReport{IsFxServer: true, Truncated: true}, false, exitPartial},
		{"truncated check", model.FxReport{IsFxServer: true, Truncated: true}, true, exitPartial},
		{"truncated findings check", model.FxReport{IsFxServer: true, Truncated: true, Findings: finding}, true, exitFindings},
		{"truncated findings", model.FxReport{IsFxServer: true, Truncated: true, Findings: finding}, false, exitPartial},
	} {
		if got := ExitCode(fxResult(c.rep, c.check)); got != c.want {
			t.Errorf("%s: exit %d, want %d", c.name, got, c.want)
		}
	}
}

// TestFxReport checks the mapping of the parts of a report the test
// servers do not have: a publication date, connection configuration sets,
// a finding for the server as a whole, missing optional values.
func TestFxReport(t *testing.T) {
	date := time.Date(2025, 4, 1, 12, 0, 0, 0, time.FixedZone("x", 3600))
	set := uafx.Node{NodeID: ua.NewNumericNodeID(5, 7), BrowseName: &ua.QualifiedName{NamespaceIndex: 5, Name: "Set"}}
	r := &uafx.Report{
		Namespaces: []uafx.Namespace{{URI: "urn:di", Index: 2, Present: true, Version: "1.05", PublicationDate: date}, {URI: "urn:cm"}},
		ConnectionManagers: []uafx.ConnectionManager{{
			Node:                        uafx.Node{NodeID: ua.NewNumericNodeID(5, 1)},
			ConnectionConfigurationSets: []uafx.Node{set},
		}},
		AutomationComponents: []uafx.AutomationComponent{{
			Node:   uafx.Node{},
			Assets: []uafx.Asset{{}},
			FunctionalEntities: []uafx.FunctionalEntity{{ConnectionEndpoints: []uafx.ConnectionEndpoint{
				{CleanupTimeout: 2 * time.Second},
				{CleanupTimeoutMs: ptr(-0.5)},
				{CleanupTimeoutMs: ptr(math.NaN())},
				{CleanupTimeoutMs: ptr(-1e300)},
				{CleanupTimeoutMs: ptr(2500.0), CleanupTimeout: 2500 * time.Millisecond, CreationTime: date},
			}}},
			Capabilities: &uafx.ComponentCapabilities{MinConnections: ptr(uint32(1)), MaxConnectionsPerCall: ptr(uint32(3)), MaxFunctionalEntities: ptr(uint32(2))},
		}},
		Findings: []uafx.Finding{{Clause: "14.2", Text: "no DI"}},
	}
	rep := fxReport(r)
	if got := rep.Namespaces[0].PublicationDate; got != "2025-04-01T11:00:00Z" {
		t.Errorf("publication date %q", got)
	}
	if rep.Namespaces[1].PublicationDate != "" || rep.Namespaces[1].Present {
		t.Errorf("absent namespace: %+v", rep.Namespaces[1])
	}
	if s := rep.ConnectionManagers[0].ConnectionConfigurationSets; len(s) != 1 || s[0].BrowseName != "5:Set" || s[0].NodeID != "ns=5;i=7" {
		t.Errorf("sets: %+v", s)
	}
	if f := rep.Findings[0]; f.NodeID != "" || f.Clause != "14.2" {
		t.Errorf("finding: %+v", f)
	}
	ac := rep.AutomationComponents[0]
	if ac.NodeID != "" || ac.AggregatedHealth != nil || nameplate(ac.Assets[0]) != "no nameplate" {
		t.Errorf("component: %+v", ac)
	}
	fe := ac.FunctionalEntities[0]
	if fe.OperationalHealth != "" || fe.OperationalHealthBits != nil || fe.AuthorAssignedVersion != "" {
		t.Errorf("entity: %+v", fe)
	}
	if ep := fe.ConnectionEndpoints[0]; ep.Status != "" || ep.CleanupTimeoutMs != 2000 || ep.CreationTime != "" {
		t.Errorf("endpoint: %+v", ep)
	}
	// A negative CleanupTimeout stays negative (never cleaned up, Part 81
	// §6.6.2), NaN is 0, the times are UTC.
	for i, want := range []int64{-1, 0, math.MinInt64, 2500} {
		if got := fe.ConnectionEndpoints[i+1].CleanupTimeoutMs; got != want {
			t.Errorf("endpoint %d: cleanup %d, want %d", i+1, got, want)
		}
	}
	if got := fe.ConnectionEndpoints[4].CreationTime; got != "2025-04-01T11:00:00Z" || fe.ConnectionEndpoints[4].ModificationTime != "" {
		t.Errorf("creation time %q", got)
	}
	// The text output of such a report: no panics, the unknowns named.
	out := captureStdout(t, func() { printFxReport(rep) })
	for _, want := range []string{"UAFX server: no", "absent urn:cm", "status unknown", "server: no DI (Part 81 §14.2)", "  5:Set (ns=5;i=7)",
		"cleanup timeout never", "cleanup timeout 2.5s, created 2025-04-01T11:00:00Z\n",
		"capabilities: supports persistence no, max connections per call 3, max functional entities 2, min connections 1\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("output does not contain %q:\n%s", want, out)
		}
	}
}
