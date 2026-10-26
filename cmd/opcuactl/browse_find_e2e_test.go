// SPDX-License-Identifier: MIT

package opcuactl

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/opcuactl/internal/model"
	"github.com/otfabric/opcuactl/internal/serve"
)

// runJSON runs opcuactl with --format json and decodes what it printed.
func runJSON[T any](t *testing.T, args ...string) T {
	t.Helper()
	out := mustRun(t, append(args, "--format", "json")...)
	var v T
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("opcuactl %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return v
}

// startDemo runs `opcuactl serve` with the demo address space and returns
// it with its opc.tcp endpoint.
func startDemo(t *testing.T, args ...string) (*servedServer, string) {
	t.Helper()
	s := startServe(t, append([]string{"--security-policy", "None", "--log-level", "off"}, args...)...)
	return s, s.url(t, ua.TransportProfileUATCP)
}

// demoVariables are the variables of the Demo object of serve.
var demoVariables = []string{
	serve.DemoBoolean, serve.DemoInt32, serve.DemoDouble, serve.DemoString, serve.DemoDateTime,
	serve.DemoInt32Array, serve.DemoCounter, serve.DemoSine, serve.DemoLevel,
}

// TestE2E_Browse walks the demo address space with browse children, refs,
// tree and path.
func TestE2E_Browse(t *testing.T) {
	s, ep := startDemo(t)
	demo := s.node(serve.DemoFolder)

	t.Run("children", func(t *testing.T) {
		children := runJSON[[]model.ChildNode](t, "browse", "children", "-e", ep, "-n", demo, "--node-class", "variable")
		var ids []string
		for _, c := range children {
			ids = append(ids, c.NodeID)
			if c.NodeClass != "NodeClassVariable" {
				t.Errorf("--node-class variable returned %+v", c)
			}
		}
		for _, v := range demoVariables {
			if !slices.Contains(ids, s.node(v)) {
				t.Errorf("children of Demo miss %s: %v", v, ids)
			}
		}

		out := mustRun(t, "browse", "children", "-e", ep, "-n", demo, "--node-class", "Method")
		wantContains(t, out, "NODE ID", s.node(serve.DemoAdd), "NodeClassMethod")
		if strings.Contains(out, s.node(serve.DemoSine)) {
			t.Errorf("--node-class Method listed a variable:\n%s", out)
		}

		// The multi-word node classes of the completion.
		out = mustRun(t, "browse", "children", "-e", ep, "-n", "i=88", "--node-class", "objecttype")
		wantContains(t, out, "BaseObjectType")

		// The default start node is the Objects folder.
		wantContains(t, mustRun(t, "browse", "children", "-e", ep), "Server", demo)
	})

	t.Run("refs", func(t *testing.T) {
		refs := runJSON[[]model.Reference](t, "browse", "refs", "-e", ep, "-n", demo, "--direction", "forward")
		found := map[string]bool{}
		for _, r := range refs {
			if !r.IsForward {
				t.Errorf("--direction forward returned an inverse reference: %+v", r)
			}
			found[r.ReferenceType+" "+r.NodeID] = true
		}
		for _, want := range []string{"HasComponent " + s.node(serve.DemoSine), "HasCondition " + s.node(serve.DemoAlarm), "HasComponent " + s.node(serve.DemoAdd)} {
			if !found[want] {
				t.Errorf("refs of Demo miss %s: %v", want, found)
			}
		}

		// --direction inverse returns the inverse references only: the
		// Server notifies of the events of Demo.
		inverse := runJSON[[]model.Reference](t, "browse", "refs", "-e", ep, "-n", demo, "--direction", "inverse")
		if len(inverse) == 0 {
			t.Fatal("no inverse references of Demo")
		}
		for _, r := range inverse {
			if r.IsForward {
				t.Errorf("--direction inverse returned a forward reference: %+v", r)
			}
		}
		if !slices.ContainsFunc(inverse, func(r model.Reference) bool { return r.ReferenceType == "HasNotifier" && r.NodeID == "i=2253" }) {
			t.Errorf("inverse references of Demo miss HasNotifier from the Server: %+v", inverse)
		}

		// The default (both) has the references of both directions.
		both := runJSON[[]model.Reference](t, "browse", "refs", "-e", ep, "-n", demo)
		if len(both) != len(refs)+len(inverse) {
			t.Errorf("--direction both returned %d references, want %d forward + %d inverse", len(both), len(refs), len(inverse))
		}

		out := mustRun(t, "browse", "refs", "-e", ep, "-n", demo, "--refs", "i=47", "--direction", "forward", "--node-class", "variable")
		wantContains(t, out, "DIRECTION", "forward", "HasComponent", s.node(serve.DemoLevel))
		if strings.Contains(out, "HasCondition") || strings.Contains(out, s.node(serve.DemoAdd)) {
			t.Errorf("--refs i=47 --node-class variable returned other references:\n%s", out)
		}
		inv := mustRun(t, "browse", "refs", "-e", ep, "-n", demo, "--direction", "inverse")
		// The Objects folder organizes Demo (go-opcua v1.26.1 records the
		// inverse of a reference on its target).
		wantContains(t, inv, "inverse", "HasNotifier", "Organizes", "i=85")
	})

	t.Run("unknown node", func(t *testing.T) {
		// go-opcua v1.26.1 returns the Bad BrowseResult status: exit 3.
		for _, args := range [][]string{
			{"browse", "children", "-e", ep, "-n", "ns=1;s=NoSuchNode"},
			{"browse", "refs", "-e", ep, "-n", "ns=1;s=NoSuchNode"},
		} {
			_, stderr, err := runCLI(t, args...)
			if ExitCode(err) != 3 {
				t.Errorf("opcuactl %s: %v (exit %d)", strings.Join(args, " "), err, ExitCode(err))
			}
			wantContains(t, stderr, "BadNodeIDUnknown")
		}
	})

	t.Run("tree", func(t *testing.T) {
		tree := runJSON[model.TreeNode](t, "browse", "tree", "-e", ep, "-n", demo, "--depth", "1")
		if tree.NodeID != demo || tree.BrowseName != "1:Demo" || tree.NodeClass != "NodeClassObject" || tree.Depth != 0 {
			t.Errorf("root %+v", tree)
		}
		var names []string
		for _, c := range tree.Children {
			names = append(names, c.DisplayName)
			if c.Depth != 1 || len(c.Children) != 0 {
				t.Errorf("--depth 1 child %s at depth %d with %d children", c.NodeID, c.Depth, len(c.Children))
			}
		}
		for _, want := range []string{"Counter", "Sine", "LevelAlarm", "Add"} {
			if !slices.Contains(names, want) {
				t.Errorf("tree misses %s: %v", want, names)
			}
		}

		deep := runJSON[model.TreeNode](t, "browse", "tree", "-e", ep, "-n", demo, "--depth", "2", "--node-class", "variable")
		for _, c := range deep.Children {
			if c.NodeID == s.node(serve.DemoAdd) {
				t.Errorf("--node-class variable kept the method %+v", c)
			}
		}

		out := mustRun(t, "browse", "tree", "-e", ep, "-n", demo, "--depth", "2")
		wantContains(t, out,
			"Counter ("+s.node(serve.DemoCounter)+") [NodeClassVariable]\n",
			"Add ("+s.node(serve.DemoAdd)+") [NodeClassMethod]\n",
			"  InputArguments (", // children are indented
		)
		// --depth 1 is the children in the table as in JSON.
		shallow := mustRun(t, "browse", "tree", "-e", ep, "-n", demo, "--depth", "1")
		wantContains(t, shallow, "Add ("+s.node(serve.DemoAdd)+")")
		if strings.Contains(shallow, "InputArguments") {
			t.Errorf("--depth 1 printed grandchildren:\n%s", shallow)
		}
		methods := mustRun(t, "browse", "tree", "-e", ep, "-n", demo, "--node-class", "method", "--depth", "1")
		wantContains(t, methods, "Add ("+s.node(serve.DemoAdd)+")")
		if strings.Contains(methods, "NodeClassVariable") {
			t.Errorf("--node-class method printed variables:\n%s", methods)
		}
	})

	t.Run("path", func(t *testing.T) {
		for _, tc := range []struct {
			args []string
			want string
		}{
			{[]string{"--path", "Server.ServerStatus"}, "i=2256"},
			{[]string{"--path", "Demo.Counter", "--namespace", "1"}, s.node(serve.DemoCounter)},
			{[]string{"--path", "1:Demo.1:Sine"}, s.node(serve.DemoSine)},
			{[]string{"--path", "Level", "--namespace", "1", "--start-node", demo}, s.node(serve.DemoLevel)},
		} {
			got := strings.TrimSpace(mustRun(t, append([]string{"browse", "path", "-e", ep, "-q"}, tc.args...)...))
			if got != tc.want {
				t.Errorf("browse path %v = %q, want %q", tc.args, got, tc.want)
			}
		}

		res := runJSON[model.BrowsePathResult](t, "browse", "path", "-e", ep, "--path", "Demo.Add", "--namespace", "1")
		if res.Path != "Demo.Add" || res.NodeID != s.node(serve.DemoAdd) {
			t.Errorf("browse path json %+v", res)
		}
		_, errOut := wantExit(t, 0, "browse", "path", "-e", ep, "--path", "Server", "-v")
		wantContains(t, errOut, `resolved "Server" → i=2253`)
		wantContains(t, mustRun(t, "browse", "path", "-e", ep, "--path", "Server"), "PATH", "NODE ID", "i=2253")
	})

	t.Run("errors", func(t *testing.T) {
		for _, args := range [][]string{
			{"browse", "path", "-e", ep},
			{"browse", "path", "-e", ep, "--path", "X", "--start-node", "bogus"},
			{"browse", "children", "-e", ep, "-n", "bogus"},
			{"browse", "children", "-e", ep, "--node-class", "bogus"},
			{"browse", "refs", "-e", ep, "-n", "bogus"},
			{"browse", "refs", "-e", ep, "--direction", "sideways"},
			{"browse", "refs", "-e", ep, "--refs", "ns=1;s=NoReferenceType"},
			{"browse", "tree", "-e", ep, "-n", "bogus"},
			{"browse", "tree", "-e", ep, "--node-class", "bogus"},
		} {
			wantExit(t, 1, args...)
		}
		for _, args := range [][]string{
			{"browse", "path", "-e", ep, "--path", "Nope"},
			{"browse", "path", "-e", ep, "--path", "Demo.Nope", "--namespace", "1"},
			{"browse", "path", "-e", ep, "--path", "1:Demo.1:Nope"},
			{"browse", "path", "-e", ep, "--path", "Nope", "--start-node", demo},
		} {
			wantExit(t, 3, args...)
		}
		closed := "opc.tcp://" + freeAddr(t)
		for _, cmd := range [][]string{{"browse", "children"}, {"browse", "refs"}, {"browse", "tree"}, {"browse", "path", "--path", "Server"}} {
			wantExit(t, 2, append(cmd, "-e", closed, "--timeout", "2s")...)
		}
	})
}

// TestE2E_Find searches the demo address space with find namespace, node
// and type.
func TestE2E_Find(t *testing.T) {
	s, ep := startDemo(t)
	demo := s.node(serve.DemoFolder)

	t.Run("namespace", func(t *testing.T) {
		all := runJSON[[]model.NamespaceEntry](t, "find", "namespace", "-e", ep)
		if len(all) < 2 || all[0].URI != "http://opcfoundation.org/UA/" {
			t.Errorf("namespaces %+v", all)
		}
		demoNS := runJSON[[]model.NamespaceEntry](t, "find", "namespace", "-e", ep, "--filter", "OPCUACTL:DEMO")
		if len(demoNS) != 1 || demoNS[0].URI != serve.DemoNamespaceURI || demoNS[0].Index != int(s.info.DemoNamespaceIndex) {
			t.Errorf("--filter: %+v", demoNS)
		}
		wantContains(t, mustRun(t, "find", "namespace", "-e", ep), "INDEX", "NAMESPACE URI", serve.DemoNamespaceURI)
	})

	t.Run("node", func(t *testing.T) {
		nodes := runJSON[[]model.NodeInfo](t, "find", "node", "-e", ep, "--name", "sine")
		if len(nodes) != 1 || nodes[0].NodeID != s.node(serve.DemoSine) || nodes[0].DataType != "Double" || nodes[0].BrowseName != "1:Sine" {
			t.Errorf("--name sine: %+v", nodes)
		}

		// A regex anchors at the name, not at the namespace index.
		nodes = runJSON[[]model.NodeInfo](t, "find", "node", "-e", ep, "--name", "^Coun.*", "--regex", "--start-node", demo)
		if len(nodes) != 1 || nodes[0].NodeID != s.node(serve.DemoCounter) {
			t.Errorf("--name ^Coun.* --regex: %+v", nodes)
		}
		nodes = runJSON[[]model.NodeInfo](t, "find", "node", "-e", ep, "--display-name", "^(Int32|Double)$", "--regex", "--start-node", demo)
		if len(nodes) != 2 {
			t.Errorf("--display-name regex: %+v", nodes)
		}

		nodes = runJSON[[]model.NodeInfo](t, "find", "node", "-e", ep, "--display-name", "level", "--class", "variable", "--start-node", demo, "--depth", "1")
		if len(nodes) != 1 || nodes[0].NodeID != s.node(serve.DemoLevel) {
			t.Errorf("--display-name level --class variable --depth 1: %+v", nodes)
		}

		nodes = runJSON[[]model.NodeInfo](t, "find", "node", "-e", ep, "--name", "a", "--start-node", demo, "--limit", "2")
		if len(nodes) != 2 {
			t.Errorf("--limit 2 returned %d nodes", len(nodes))
		}

		nodes = runJSON[[]model.NodeInfo](t, "find", "node", "-e", ep, "--name", "BaseObjectType", "--class", "objecttype", "--start-node", "i=88", "--depth", "1")
		if len(nodes) != 1 || nodes[0].NodeID != "i=58" {
			t.Errorf("--class objecttype: %+v", nodes)
		}

		out := mustRun(t, "find", "node", "-e", ep, "--name", "Add", "--start-node", demo)
		wantContains(t, out, "NODE ID", s.node(serve.DemoAdd), "NodeClassMethod")
	})

	t.Run("type", func(t *testing.T) {
		ids := func(nodes []model.NodeInfo) []string {
			var out []string
			for _, n := range nodes {
				out = append(out, n.NodeID)
			}
			return out
		}
		doubles := runJSON[[]model.NodeInfo](t, "find", "type", "-e", ep, "--data-type", "double", "--start-node", demo, "--depth", "1")
		if got := ids(doubles); !slices.Equal(got, []string{s.node(serve.DemoDouble), s.node(serve.DemoSine), s.node(serve.DemoLevel)}) {
			t.Errorf("--data-type double: %v", got)
		}
		int32s := runJSON[[]model.NodeInfo](t, "find", "type", "-e", ep, "--data-type", "i=6", "--start-node", demo)
		if got := ids(int32s); !slices.Equal(got, []string{s.node(serve.DemoInt32), s.node(serve.DemoInt32Array)}) {
			t.Errorf("--data-type i=6: %v", got)
		}

		alarms := runJSON[[]model.NodeInfo](t, "find", "type", "-e", ep, "--type-definition", "ExclusiveLevel", "--start-node", demo)
		if len(alarms) != 1 || alarms[0].NodeID != s.node(serve.DemoAlarm) || alarms[0].TypeDefinition != "ExclusiveLevelAlarmType" {
			t.Errorf("--type-definition ExclusiveLevel: %+v", alarms)
		}

		out, errOut := wantExit(t, 0, "find", "type", "-e", ep, "--type-definition", "i=63", "--start-node", demo, "--limit", "2", "--class", "variable", "-v")
		wantContains(t, errOut, "found 2 node(s)")
		wantContains(t, out, "DATA TYPE", "BaseDataVariableType", s.node(serve.DemoBoolean))

		none := runJSON[[]model.NodeInfo](t, "find", "type", "-e", ep, "--type-definition", "i=63", "--data-type", "Boolean", "--start-node", demo, "--depth", "1")
		if got := ids(none); !slices.Equal(got, []string{s.node(serve.DemoBoolean)}) {
			t.Errorf("--type-definition and --data-type: %v", got)
		}
	})

	t.Run("errors", func(t *testing.T) {
		for _, args := range [][]string{
			{"find", "node", "-e", ep},
			{"find", "node", "-e", ep, "--name", "(", "--regex"},
			{"find", "node", "-e", ep, "--display-name", "(", "--regex"},
			{"find", "node", "-e", ep, "--name", "x", "--start-node", "bogus"},
			{"find", "node", "-e", ep, "--name", "x", "--class", "bogus"},
			{"find", "type", "-e", ep},
			{"find", "type", "-e", ep, "--data-type", "x", "--start-node", "bogus"},
			{"find", "type", "-e", ep, "--data-type", "x", "--class", "bogus"},
		} {
			wantExit(t, 1, args...)
		}
		closed := "opc.tcp://" + freeAddr(t)
		for _, cmd := range [][]string{{"find", "namespace"}, {"find", "node", "--name", "x"}, {"find", "type", "--data-type", "x"}} {
			wantExit(t, 2, append(cmd, "-e", closed, "--timeout", "2s")...)
		}
	})
}
