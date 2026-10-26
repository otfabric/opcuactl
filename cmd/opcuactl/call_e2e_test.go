// SPDX-License-Identifier: MIT

package opcuactl

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

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

// startTypedMethodServer is startServer with the method Typed of arguments
// of many built-in types, which checks the Go types of its arguments
// (BadTypeMismatch) and returns them.
func startTypedMethodServer(t *testing.T) (*testServer, string, string) {
	t.Helper()
	s := startServer(t)
	method := server.NewFolderNode(ua.NewStringNodeID(s.ns.ID(), "Typed"), "Typed")
	method.SetNodeClass(ua.NodeClassMethod)
	s.ns.AddNode(method)
	s.ns.Objects().AddRef(method, id.HasComponent, true)
	arg := func(name string, dataType uint32, rank int32) *ua.Argument {
		return &ua.Argument{Name: name, DataType: ua.NewNumericNodeID(0, dataType), ValueRank: rank}
	}
	in := []*ua.Argument{
		arg("u16", id.UInt16, 1),
		arg("text", id.LocalizedText, -1),
		arg("time", id.UtcTime, -1),
		arg("i64", id.Int64, -1),
		arg("i8", id.SByte, -3),
		arg("bytes", id.ByteString, -1),
		arg("node", id.NodeID, -1),
		arg("name", id.QualifiedName, -1),
		arg("float", id.Float, -2),
		arg("matrix", id.Boolean, 2),
	}
	if err := method.SetMethodArguments(in, in); err != nil {
		t.Fatalf("method arguments: %v", err)
	}
	s.srv.RegisterMethod(s.ns.Objects().ID(), method.ID(), func(_ context.Context, _, _ *ua.NodeID, args []*ua.Variant) ([]*ua.Variant, ua.StatusCode) {
		if len(args) != len(in) {
			return nil, ua.StatusBadArgumentsMissing
		}
		want := []any{[]uint16{}, (*ua.LocalizedText)(nil), time.Time{}, int64(0), int8(0), []byte{}, (*ua.NodeID)(nil), (*ua.QualifiedName)(nil), float32(0), [][]bool{}}
		for i, a := range args {
			if reflect.TypeOf(a.Value()) != reflect.TypeOf(want[i]) {
				return nil, ua.StatusBadTypeMismatch
			}
		}
		if lt := args[1].Value().(*ua.LocalizedText); lt.Locale != "en" || lt.Text != "hi" {
			return nil, ua.StatusBadInvalidArgument
		}
		if qn := args[7].Value().(*ua.QualifiedName); qn.NamespaceIndex != 2 || qn.Name != "Name" {
			return nil, ua.StatusBadInvalidArgument
		}
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

	t.Run("introspect of a node that is not a method exits 3", func(t *testing.T) {
		for _, tc := range []struct {
			method, want string
		}{
			{s.node("Demo.Nope"), "BadNodeIDUnknown"},
			{s.node(serve.DemoInt32), "BadNodeClassInvalid"},
			{demo, "BadNodeClassInvalid"},
		} {
			for _, f := range []string{"table", "json"} {
				out, errOut := wantExit(t, 3, "call", "method", "-e", ep, "--object", demo, "--method", tc.method, "--introspect", "-f", f)
				if out != "" {
					t.Errorf("introspect %s: printed %q", tc.method, out)
				}
				wantContains(t, errOut, tc.want)
			}
		}
		_, errOut := wantExit(t, 3, "call", "method", "-e", ep, "--object", demo, "--method", s.node(serve.DemoInt32), "--introspect")
		wantContains(t, errOut, "is a Variable, not a Method")
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
			{call("--arg-json", "[2]"), "BadArgumentsMissing"},
			{call("--arg-json", "[1, 2, 3]"), "BadTooManyArguments"},
			{[]string{"call", "method", "-e", ep, "--object", demo, "--method", s.node("Demo.Nope"), "-f", "json"}, "BadMethodInvalid"},
		} {
			out, errOut := wantExit(t, 3, tc.args...)
			wantContains(t, out, tc.want)
			wantContains(t, errOut, tc.want)
		}
	})

	t.Run("json arguments typed by the method signature", func(t *testing.T) {
		// Demo.Add takes two Int32: JSON numbers are sent as Int32.
		for _, args := range []string{`[2, 40]`, `[2.0, 4e1]`, `["2", " 40"]`, ` [ -2147483648 , 2147483647 ] `} {
			res := runJSON[model.CallResult](t, call("--arg-json", args)...)
			want := float64(42)
			if strings.Contains(args, "2147483647") {
				want = -1
			}
			if res.Status != "Good" || len(res.OutputArguments) != 1 || res.OutputArguments[0] != want {
				t.Errorf("--arg-json %s: %+v", args, res)
			}
		}
		file := filepath.Join(t.TempDir(), "args.json")
		if err := os.WriteFile(file, []byte("[2,\n 40]\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		res := runJSON[model.CallResult](t, call("--arg-file", file)...)
		if res.Status != "Good" || len(res.OutputArguments) != 1 || res.OutputArguments[0] != float64(42) {
			t.Errorf("--arg-file: %+v", res)
		}
		out := mustRun(t, call("--arg-file", file)...)
		wantContains(t, out, "Good", "  [0] 42")

		// A value that does not fit the type of its argument is a usage
		// error, reported before the call.
		for _, tc := range []struct{ args, want string }{
			{`[2.5, 1]`, "argument 0 (a): 2.5 is not an integer (Int32)"},
			{`[1, 3000000000]`, "argument 1 (b): 3000000000 is out of range for Int32"},
			{`[1, -2147483649]`, "out of range for Int32"},
			{`["x", 1]`, `argument 0 (a): "x" is not a valid Int32`},
			{`[true, 1]`, "expected an Int32, got a boolean"},
			{`[[1], 1]`, "expected a scalar Int32, got an array"},
			{`[1, 1e400]`, "is out of range for Int32"},
		} {
			_, errOut := wantExit(t, 1, call("--arg-json", tc.args)...)
			wantContains(t, errOut, "invalid --arg-json", tc.want)
			if err := os.WriteFile(file, []byte(tc.args), 0o600); err != nil {
				t.Fatal(err)
			}
			_, errOut = wantExit(t, 1, call("--arg-file", file)...)
			wantContains(t, errOut, "invalid --arg-file", tc.want)
		}
		// null is an empty Variant, which Add rejects.
		wantExit(t, 3, call("--arg-json", `[null, 1]`)...)
	})

	t.Run("json arguments of every built-in type", func(t *testing.T) {
		ts, object, method := startTypedMethodServer(t)
		res := runJSON[model.CallResult](t, "call", "method", "-e", ts.url, "--object", object, "--method", method,
			"--arg-json", `[[1, 2, 65535], {"locale": "en", "text": "hi"}, "2026-10-26T10:00:00Z", "9007199254740993", -128, "AQID", "ns=1;s=X", "2:Name", 1.5, [[true], [false]]]`)
		if res.Status != "Good" || len(res.OutputArguments) != 10 {
			t.Fatalf("typed call: %+v", res)
		}
		if got := res.OutputArguments[3]; got != float64(9007199254740993) {
			t.Errorf("Int64 = %v", got)
		}
		_, errOut := wantExit(t, 1, "call", "method", "-e", ts.url, "--object", object, "--method", method,
			"--arg-json", `[[1, 65536]]`)
		wantContains(t, errOut, "argument 0 (u16): element 1: 65536 is out of range for UInt16")
		_, errOut = wantExit(t, 1, "call", "method", "-e", ts.url, "--object", object, "--method", method,
			"--arg-json", `[5]`)
		wantContains(t, errOut, "expected an array of UInt16, got a scalar")
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
