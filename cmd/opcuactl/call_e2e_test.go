// SPDX-License-Identifier: MIT

package opcuactl

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/otfabric/go-opcua/id"
	"github.com/otfabric/go-opcua/server"
	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/opcuactl/internal/model"
	"github.com/otfabric/opcuactl/internal/serve"
)

// startMethodServer is startServer with the method Echo(x, y, z) on the
// Objects folder of the test namespace: it returns its input arguments.
func startMethodServer(t *testing.T) (*testServer, string, string) {
	t.Helper()
	s := startServer(t)
	method := server.NewFolderNode(ua.NewStringNodeID(s.ns.ID(), "Echo"), "Echo")
	method.SetNodeClass(ua.NodeClassMethod)
	s.ns.AddNode(method)
	s.ns.Objects().AddRef(method, id.HasComponent, true)
	anyArg := func(name string) *ua.Argument {
		return &ua.Argument{Name: name, DataType: ua.NewNumericNodeID(0, id.BaseDataType), ValueRank: -1}
	}
	in := []*ua.Argument{anyArg("x"), anyArg("y"), anyArg("z")}
	if err := method.SetMethodArguments(in, in); err != nil {
		t.Fatalf("method arguments: %v", err)
	}
	s.srv.RegisterMethod(s.ns.Objects().ID(), method.ID(), func(_ context.Context, _, _ *ua.NodeID, args []*ua.Variant) ([]*ua.Variant, ua.StatusCode) {
		return args, ua.StatusOK
	})
	return s, s.ns.Objects().ID().String(), method.ID().String()
}

// TestE2E_CallMethod calls Demo.Add of `opcuactl serve` and a method that
// echoes its arguments, and checks that a Bad method result exits 3.
func TestE2E_CallMethod(t *testing.T) {
	s, ep := startDemo(t)
	demo, add := s.node(serve.DemoFolder), s.node(serve.DemoAdd)
	call := func(args ...string) []string {
		return append([]string{"call", "method", "-e", ep, "--object", demo, "--method", add}, args...)
	}

	t.Run("introspect", func(t *testing.T) {
		type signature struct {
			ObjectID string                 `json:"objectId"`
			MethodID string                 `json:"methodId"`
			Inputs   []model.MethodArgument `json:"inputs"`
			Outputs  []model.MethodArgument `json:"outputs"`
		}
		sig := runJSON[signature](t, call("--introspect")...)
		if sig.ObjectID != demo || sig.MethodID != add || len(sig.Inputs) != 2 || len(sig.Outputs) != 1 {
			t.Fatalf("signature %+v", sig)
		}
		if a := sig.Inputs[0]; a.Name != "a" || a.DataType != "i=6" || a.ValueRank != -1 || a.Description != "first addend" {
			t.Errorf("input a: %+v", a)
		}
		if o := sig.Outputs[0]; o.Name != "sum" || o.Description != "a + b" {
			t.Errorf("output: %+v", o)
		}

		out := mustRun(t, call("--introspect")...)
		wantContains(t, out, "Object: "+demo+"  Method: "+add, "Input Arguments:", "second addend", "Output Arguments:", "sum")
	})

	t.Run("call", func(t *testing.T) {
		res := runJSON[model.CallResult](t, call("--arg", "2", "--arg", "40", "--types", "int32, int32")...)
		if res.Status != "Good" || res.StatusCode != 0 || len(res.OutputArguments) != 1 || res.OutputArguments[0] != float64(42) {
			t.Errorf("Add(2, 40) = %+v", res)
		}
		out := mustRun(t, call("--arg", "-1", "--arg", "1", "--types", "int32,int32")...)
		wantContains(t, out, "STATUS", "Good", "Output Arguments:", "  [0] 0")
	})

	t.Run("bad result exits 3", func(t *testing.T) {
		for _, tc := range []struct {
			args []string
			want string
		}{
			{call("--arg", "2", "--arg", "x", "--types", "int32,string"), "BadTypeMismatch"},
			{call("--arg", "2", "--types", "int32"), "BadArgumentsMissing"},
			{call("--arg-json", "[1, 2]"), "BadTypeMismatch"}, // JSON numbers are Doubles
			{[]string{"call", "method", "-e", ep, "--object", demo, "--method", s.node("Demo.Nope"), "-f", "json"}, "BadMethodInvalid"},
		} {
			out, errOut := wantExit(t, 3, tc.args...)
			wantContains(t, out, tc.want)
			wantContains(t, errOut, tc.want)
		}
	})

	t.Run("json arguments", func(t *testing.T) {
		es, object, echo := startMethodServer(t)
		res := runJSON[model.CallResult](t, "call", "method", "-e", es.url, "--object", object, "--method", echo,
			"--arg-json", `[5, "hello", true]`)
		if res.Status != "Good" || len(res.OutputArguments) != 3 || res.OutputArguments[0] != float64(5) || res.OutputArguments[1] != "hello" || res.OutputArguments[2] != true {
			t.Errorf("--arg-json: %+v", res)
		}

		file := filepath.Join(t.TempDir(), "args.json")
		if err := os.WriteFile(file, []byte(`["a", 1.5, null]`), 0o600); err != nil {
			t.Fatal(err)
		}
		res = runJSON[model.CallResult](t, "call", "method", "-e", es.url, "--object", object, "--method", echo, "--arg-file", file)
		if len(res.OutputArguments) != 3 || res.OutputArguments[0] != "a" || res.OutputArguments[1] != 1.5 || res.OutputArguments[2] != nil {
			t.Errorf("--arg-file: %+v", res)
		}

		// An argument without a type in --types is a string.
		res = runJSON[model.CallResult](t, "call", "method", "-e", es.url, "--object", object, "--method", echo,
			"--arg", "7", "--arg", "8", "--arg", "9", "--types", "int32")
		if len(res.OutputArguments) != 3 || res.OutputArguments[0] != float64(7) || res.OutputArguments[1] != "8" || res.OutputArguments[2] != "9" {
			t.Errorf("--arg without --types: %+v", res)
		}
	})

	t.Run("usage errors", func(t *testing.T) {
		bad := filepath.Join(t.TempDir(), "bad.json")
		if err := os.WriteFile(bad, []byte(`{"not": "an array"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		for _, args := range [][]string{
			{"call", "method", "-e", ep, "--method", add},
			{"call", "method", "-e", ep, "--object", demo},
			{"call", "method", "-e", ep, "--object", "bogus", "--method", add},
			{"call", "method", "-e", ep, "--object", demo, "--method", "bogus"},
			call("--arg", "x", "--types", "int32"),
			call("--arg-json", "[1,"),
			call("--arg-file", filepath.Join(t.TempDir(), "missing.json")),
			call("--arg-file", bad),
		} {
			_, errOut := wantExit(t, 1, args...)
			if strings.TrimSpace(errOut) == "" {
				t.Errorf("%v: no error message", args)
			}
		}
		wantExit(t, 2, "call", "method", "-e", "opc.tcp://"+freeAddr(t), "--object", demo, "--method", add, "--timeout", "2s")
	})
}
