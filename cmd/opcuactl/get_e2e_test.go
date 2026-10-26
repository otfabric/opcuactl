// SPDX-License-Identifier: MIT

package opcuactl

import (
	"encoding/json"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/otfabric/opcuactl/internal/model"
	"github.com/otfabric/opcuactl/internal/serve"
)

// TestE2E_GetNode reads node summaries from `opcuactl serve`.
func TestE2E_GetNode(t *testing.T) {
	s, ep := startDemo(t)
	level := s.node(serve.DemoLevel)

	info := runJSON[model.NodeInfo](t, "get", "node", "-e", ep, "-n", level, "--full")
	if info.NodeID != level || info.NodeClass != "NodeClassVariable" || info.BrowseName != "1:Level" || info.DisplayName != "Level" ||
		info.DataType != "Double" || info.Value != float64(50) || info.ValueStatus != "Good" ||
		info.AccessInterpretation != "readable, writable" || info.TypeDefinition != "BaseDataVariableType" || info.ValueRank != "-1" {
		t.Errorf("get node --full: %+v", info)
	}
	sine := runJSON[model.NodeInfo](t, "get", "node", "-e", ep, "-n", s.node(serve.DemoSine))
	if sine.AccessInterpretation != "readable, history-read" || sine.ValueRank != "" {
		t.Errorf("get node Demo.Sine (without --full): %+v", sine)
	}

	infos := runJSON[[]model.NodeInfo](t, "get", "node", "-e", ep, "-n", level, "-n", s.node(serve.DemoFolder))
	if len(infos) != 2 || infos[1].NodeClass != "NodeClassObject" || infos[1].Description != "opcuactl serve demo nodes" {
		t.Errorf("two nodes: %+v", infos)
	}

	out := mustRun(t, "get", "node", "-e", ep, "-n", level, "--full")
	wantContains(t, out, "HISTORIZING", "readable, writable", "BaseDataVariableType")
	out = mustRun(t, "get", "node", "-e", ep, "-n", s.node(serve.DemoInt32Array))
	wantContains(t, out, "VALUE", "1, 2, 3, 4, 5")
	if strings.Contains(out, "HISTORIZING") {
		t.Errorf("table without --full has the --full columns:\n%s", out)
	}
	if got := strings.TrimSpace(mustRun(t, "get", "node", "-e", ep, "-n", level, "-n", "i=2253", "-q")); got != level+"\ni=2253" {
		t.Errorf("-q printed %q", got)
	}

	wantExit(t, 1, "get", "node", "-e", ep)
	wantExit(t, 1, "get", "node", "-e", ep, "-n", "bogus")
	_, errOut := wantExit(t, 3, "get", "node", "-e", ep, "-n", s.node("Demo.Nope"))
	wantContains(t, errOut, "BadNodeIDUnknown")
	wantExit(t, 2, "get", "node", "-e", "opc.tcp://"+freeAddr(t), "-n", level, "--timeout", "2s")
}

// TestE2E_GetAttr reads attributes by name, by number and all of them.
func TestE2E_GetAttr(t *testing.T) {
	s, ep := startDemo(t)
	level := s.node(serve.DemoLevel)

	attrs := runJSON[[]AttributeResult](t, "get", "attr", "-e", ep, "-n", level,
		"--attr", "Value", "--attr", "3", "--attr", "display-name", "--attr", "DataType", "--attr", "NodeID", "--attr", "Executable")
	want := []struct {
		name  string
		value any
	}{{"Value", float64(50)}, {"BrowseName", "1:Level"}, {"DisplayName", "Level"}, {"DataType", "i=11"}, {"NodeID", level}, {"Executable", nil}}
	if len(attrs) != len(want) {
		t.Fatalf("attributes %+v", attrs)
	}
	for i, w := range want {
		if attrs[i].Attribute != w.name || attrs[i].Value != w.value || attrs[i].NodeID != level {
			t.Errorf("attribute %d = %+v, want %s = %v", i, attrs[i], w.name, w.value)
		}
	}
	if attrs[5].Status != "BadAttributeIDInvalid" || attrs[0].Status != "Good" || attrs[0].AttributeID != 13 {
		t.Errorf("statuses: %+v", attrs)
	}

	one := runJSON[AttributeResult](t, "get", "attr", "-e", ep, "-n", s.node(serve.DemoFolder), "--attr", "Description")
	if one.Value != "opcuactl serve demo nodes" {
		t.Errorf("Description: %+v", one)
	}

	// --all leaves out the attributes a variable does not have, unless -v.
	all := runJSON[[]AttributeResult](t, "get", "attr", "-e", ep, "-n", level, "--all")
	allV := runJSON[[]AttributeResult](t, "get", "attr", "-e", ep, "-n", level, "--all", "-v")
	if len(allV) != len(allAttributeIDs) || len(all) >= len(allV) {
		t.Errorf("--all returned %d attributes, with -v %d, want fewer and %d", len(all), len(allV), len(allAttributeIDs))
	}
	for _, a := range all {
		if a.Status == "BadAttributeIDInvalid" {
			t.Errorf("--all without -v kept %+v", a)
		}
	}
	if !slices.ContainsFunc(allV, func(a AttributeResult) bool { return a.Attribute == "IsAbstract" && a.Status == "BadAttributeIDInvalid" }) {
		t.Errorf("--all -v misses IsAbstract: %+v", allV)
	}

	out := mustRun(t, "get", "attr", "-e", ep, "-n", level, "--attr", "BrowseName", "--attr", "AccessLevel")
	wantContains(t, out, "ATTRIBUTE", "BrowseName", "1:Level", "AccessLevel", "Good")

	wantExit(t, 1, "get", "attr", "-e", ep, "--attr", "Value")
	wantExit(t, 1, "get", "attr", "-e", ep, "-n", level)
	wantExit(t, 1, "get", "attr", "-e", ep, "-n", "bogus", "--attr", "Value")
	_, errOut := wantExit(t, 1, "get", "attr", "-e", ep, "-n", level, "--attr", "Colour")
	wantContains(t, errOut, `invalid --attr "Colour"`)
	wantExit(t, 2, "get", "attr", "-e", "opc.tcp://"+freeAddr(t), "-n", level, "--attr", "Value", "--timeout", "2s")
}

// TestE2E_GetServer reads the server status in a table and as JSON.
func TestE2E_GetServer(t *testing.T) {
	_, ep := startDemo(t)

	status := runJSON[model.ServerStatus](t, "get", "server", "-e", ep)
	if status.State != "ServerStateRunning" || status.StartTime == "" || status.CurrentTime < status.StartTime ||
		status.SecurityPolicy != "None" || status.SecurityMode != "MessageSecurityModeNone" ||
		status.BuildInfo == nil || status.BuildInfo.ProductName != "opcuactl" || status.BuildInfo.ManufacturerName != "otfabric" {
		t.Errorf("server status %+v %+v", status, status.BuildInfo)
	}
	out := mustRun(t, "get", "server", "-e", ep)
	wantContains(t, out, "Server Status\n", "State:          ServerStateRunning", "Security:       None / MessageSecurityModeNone", "Manufacturer:   otfabric")

	wantExit(t, 2, "get", "server", "-e", "opc.tcp://"+freeAddr(t), "--timeout", "2s")
}

// TestE2E_GetVars collects the variables under Demo.
func TestE2E_GetVars(t *testing.T) {
	s, ep := startDemo(t)
	demo := s.node(serve.DemoFolder)
	ids := func(vars []model.VarNode) []string {
		var out []string
		for _, v := range vars {
			out = append(out, v.NodeID)
		}
		return out
	}

	vars := runJSON[[]model.VarNode](t, "get", "vars", "-e", ep, "-n", demo)
	got := ids(vars)
	for _, v := range demoVariables {
		if !slices.Contains(got, s.node(v)) {
			t.Errorf("get vars misses %s: %v", v, got)
		}
	}
	for _, v := range vars {
		if v.NodeID == s.node(serve.DemoInt32) && (v.Value != float64(42) || v.DataType != "Int32" || v.Status != "Good" || v.BrowseName != "1:Int32") {
			t.Errorf("Demo.Int32: %+v", v)
		}
		if strings.Contains(v.NodeID, "LevelAlarm") || v.Path != "" {
			t.Errorf("without --recursive or --paths: %+v", v)
		}
	}

	// --recursive goes into LevelAlarm; --depth 2 stops at its properties.
	deep := runJSON[[]model.VarNode](t, "get", "vars", "-e", ep, "-n", demo, "--recursive", "--paths", "--no-read")
	two := runJSON[[]model.VarNode](t, "get", "vars", "-e", ep, "-n", demo, "--recursive", "--depth", "2", "--paths", "--no-read")
	if len(two) <= len(vars) || len(deep) <= len(two) {
		t.Errorf("%d direct, %d to depth 2, %d recursive variables", len(vars), len(two), len(deep))
	}
	paths := map[string]string{}
	for _, v := range deep {
		paths[v.NodeID] = v.Path
		if v.Value != nil || v.Status != "" {
			t.Errorf("--no-read read %+v", v)
		}
	}
	if paths[s.node(serve.DemoLevel)] != "/Level" || paths[s.node(serve.DemoAdd)+".InputArguments"] != "/Add/InputArguments" {
		t.Errorf("paths %v", paths)
	}
	if !slices.ContainsFunc(deep, func(v model.VarNode) bool { return v.Path == "/LevelAlarm/ActiveState/Id" }) {
		t.Errorf("--recursive misses /LevelAlarm/ActiveState/Id")
	}

	match := runJSON[model.VarNode](t, "get", "vars", "-e", ep, "-n", demo, "--match", "SINE")
	if match.NodeID != s.node(serve.DemoSine) {
		t.Errorf("--match SINE (one result, an object): %+v", match)
	}

	ns0 := runJSON[[]model.VarNode](t, "get", "vars", "-e", ep, "-n", demo, "--recursive", "--only-ns", "0", "--no-read")
	for _, v := range ns0 {
		if !strings.HasPrefix(v.NodeID, "i=") {
			t.Errorf("--only-ns 0 returned %s", v.NodeID)
		}
	}
	notNS0 := runJSON[[]model.VarNode](t, "get", "vars", "-e", ep, "-n", demo, "--recursive", "--exclude-ns", "0", "--no-read")
	if len(ns0) == 0 || len(ns0)+len(notNS0) != len(deep) {
		t.Errorf("--only-ns 0: %d, --exclude-ns 0: %d, all: %d", len(ns0), len(notNS0), len(deep))
	}

	if q := strings.Fields(mustRun(t, "get", "vars", "-e", ep, "-n", demo, "-q")); !slices.Equal(q, ids(vars)) {
		t.Errorf("-q printed %v, want %v", q, ids(vars))
	}
	out := mustRun(t, "get", "vars", "-e", ep, "-n", demo)
	wantContains(t, out, "NODE ID", "DATA TYPE", "hello from opcuactl")
	out = mustRun(t, "get", "vars", "-e", ep, "-n", demo, "--no-read", "--paths")
	wantContains(t, out, "PATH", "TYPE DEFINITION", "/Counter", "BaseDataVariableType")
	if strings.Contains(out, "hello from opcuactl") {
		t.Errorf("--no-read table has values:\n%s", out)
	}

	wantExit(t, 1, "get", "vars", "-e", ep, "-n", "bogus")
	wantExit(t, 2, "get", "vars", "-e", "opc.tcp://"+freeAddr(t), "--timeout", "2s")
}

// TestVersionCommand prints the build information.
func TestVersionCommand(t *testing.T) {
	saved := []string{version, tag, commit, buildDate}
	t.Cleanup(func() { SetBuildInfo(saved[0], saved[1], saved[2], saved[3]) })

	SetBuildInfo("1.2.3", "v1.2.3", "abc1234", "2026-10-26")
	out := mustRun(t, "version")
	wantContains(t, out, "opcuactl 1.2.3\n", "tag:        v1.2.3", "commit:     abc1234", "built:      2026-10-26", "go version: "+runtime.Version())

	var info map[string]string
	if err := json.Unmarshal([]byte(mustRun(t, "version", "--format", "json")), &info); err != nil {
		t.Fatal(err)
	}
	if info["version"] != "1.2.3" || info["tag"] != "v1.2.3" || info["commit"] != "abc1234" || info["buildDate"] != "2026-10-26" || info["goVersion"] != runtime.Version() {
		t.Errorf("version json %v", info)
	}

	SetBuildInfo("dev", "", "none", "unknown")
	if out := mustRun(t, "version"); strings.Contains(out, "tag:") {
		t.Errorf("a build without tag printed one:\n%s", out)
	}
	wantExit(t, 1, "version", "extra")
}

// TestCompletionCommand generates the completion scripts.
func TestCompletionCommand(t *testing.T) {
	for shell, want := range map[string]string{
		"bash":       "__start_opcuactl",
		"zsh":        "#compdef opcuactl",
		"fish":       "complete -c opcuactl",
		"powershell": "Register-ArgumentCompleter",
	} {
		wantContains(t, mustRun(t, "completion", shell), want)
	}
	wantExit(t, 1, "completion", "tcsh")
	wantExit(t, 1, "completion")

	// The values of a flag come from fixedCompletion.
	out := mustRun(t, "__complete", "browse", "refs", "--direction", "")
	wantContains(t, out, "forward\n", "inverse\n", "both\n", ":4\n")
}
