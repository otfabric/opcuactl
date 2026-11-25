// SPDX-License-Identifier: MIT

package opcuactl

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/otfabric/go-opcua"
	"github.com/otfabric/go-opcua/id"
	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/go-opcua/uafx"
	"github.com/otfabric/go-opcua/uafx/fxcm"
	"github.com/otfabric/opcuactl/internal/format"
	"github.com/otfabric/opcuactl/internal/model"
	"github.com/otfabric/opcuactl/internal/parse"
	"github.com/spf13/cobra"
)

func newFxCMCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cm",
		Short: "Work with the ConnectionManager of a server (ConnectionConfigurationSets)",
		Long: `Work with the UAFX ConnectionManager of a server (OPC 10000-81 §6.7):
the ConnectionConfigurationSets it holds, which describe Connections
between AutomationComponents, and the ConnectionManagerConfiguration file
(Annex F.3) through which they are read and written.

  list     the sets with their state, Version and the result per Connection
  process  ProcessConnectionConfigurationSets: establish, enable, disable or
           remove the Connections of sets (§6.7.5)
  edit     EditConnectionConfigurationSets: change configurable Variables
           of a set and commit them as its next Version (§6.7.4)
  export   read the sets as a ConnectionConfigurationSet file (F.2)
  import   write a file and apply its sets (CloseAndUpdate, F.3.3)

A set is named by its NodeId (ns=<index>;... or nsu=<uri>;...) or by the
path fx cm list prints: its Folders and Name joined by "/".

Most servers grant these Methods to the Role ConfigureAdmin only (Part 3
§4.9.2); a refusal is exit 9.

Examples:
  opcuactl fx cm list -e opc.tcp://localhost:4856
  opcuactl fx cm export -e opc.tcp://localhost:4856 --out sets.bin
  opcuactl fx cm import -e opc.tcp://localhost:4856 --file sets.bin --op add
  opcuactl fx cm process -e opc.tcp://localhost:4856 --action establish-enabled Plant/Line1 --wait 30s
  opcuactl fx cm edit -e opc.tcp://localhost:4856 Plant/Line1 --write Flow1/PublishingInterval=50`,
	}
	cmd.AddCommand(newFxCMListCmd(), newFxCMProcessCmd(), newFxCMEditCmd(), newFxCMExportCmd(), newFxCMImportCmd())
	return cmd
}

// withCM runs fn with the ConnectionManager of the server.
func withCM(fn func(ctx context.Context, x *cmClient) error) error {
	return withClient(func(ctx context.Context, c *opcua.Client) error {
		x, err := newCMClient(ctx, c)
		if errors.Is(err, errNoCM) {
			format.Errf("%v", err)
			return exitCode(exitUnsupported, err)
		}
		if err != nil {
			format.Errf("%v", err)
			return exitCode(exitProtocol, err)
		}
		return fn(ctx, x)
	})
}

func newFxCMListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the ConnectionConfigurationSets of the ConnectionManager",
		Long: `List the ConnectionConfigurationSets of the ConnectionManager (Part 81
§6.7.2), in its Folders as well, with for each set its state (§6.9.2:
Ready, Processing or Error), LastTransition, Version, Edit,
RollbackOnError, its Connections with their ProcessingResult and the
ConnectionsDiagnostics of the last run (§6.8.2). The ConnectionManager's
AggregatedCurrentState is TRUE while a set is in Error.

opcuactl reads at most 10000 sets and 8 Folder levels; more is exit 7.

Exit codes: 0 when the sets were read; 7 when a bound cut the listing
short; 8 when the server has no ConnectionManager; 4, 5 or 6 when a
service fails.

Examples:
  opcuactl fx cm list -e opc.tcp://localhost:4856
  opcuactl fx cm list -e opc.tcp://localhost:4856 --format json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return withCM(func(ctx context.Context, x *cmClient) error {
				sets, truncated, err := x.sets(ctx)
				if err != nil {
					format.Errf("listing the sets: %v", err)
					return exitCode(exitProtocol, err)
				}
				out := model.FxCMList{ConnectionManager: x.cm.String(), Sets: []model.FxCMSet{}, Truncated: truncated}
				if out.AggregatedCurrentState, err = x.aggregated(ctx); err != nil {
					format.Errf("%v", err)
					return exitCode(exitProtocol, err)
				}
				for _, s := range sets {
					st, err := x.status(ctx, s)
					if err != nil {
						format.Errf("reading %s: %v", s.Path, err)
						return exitCode(exitProtocol, err)
					}
					out.Sets = append(out.Sets, st)
				}
				if err := writeFxCMList(out); err != nil {
					return err
				}
				if truncated {
					return exitCodef(exitPartial, "the listing was cut short at %d sets or %d Folder levels", cmMaxSets, cmMaxDepth)
				}
				return nil
			})
		},
	}
}

func writeFxCMList(out model.FxCMList) error {
	if format.ParseFormat(outputFormat) != format.Table {
		return newWriter().Write(out)
	}
	rows := make([][]string, 0, len(out.Sets))
	for _, s := range out.Sets {
		state := cmp.Or(s.StateID, s.State)
		version := ""
		if s.Version != nil {
			version = strconv.FormatUint(uint64(*s.Version), 10)
		}
		edit := ""
		if s.Edit != nil {
			edit = strconv.FormatBool(*s.Edit)
		}
		var conns []string
		for _, c := range s.Connections {
			conns = append(conns, c.Name+"="+cmp.Or(c.ProcessingResult, "-"))
		}
		rows = append(rows, []string{s.Path, state, version, edit, strings.Join(conns, " ")})
	}
	if err := newWriter().WriteTable([]string{"SET", "STATE", "VERSION", "EDIT", "CONNECTIONS"}, rows); err != nil {
		return err
	}
	if out.AggregatedCurrentState != nil && *out.AggregatedCurrentState {
		format.Errf("AggregatedCurrentState is TRUE: a set is in Error")
	}
	return nil
}

// cmProcessActions are the values of FxProcessEnum (Part 81 §10.30) by the
// names of --action.
var cmProcessActions = map[string]uafx.FxProcess{
	"establish-enabled":  uafx.ActionEstablishConnectionsEnabled,
	"establish-disabled": uafx.ActionEstablishConnectionsDisabled,
	"establish":          uafx.ActionEstablishConnections,
	"remove":             uafx.ActionRemoveConnections,
	"enable":             uafx.ActionEnableConnections,
	"disable":            uafx.ActionDisableConnections,
}

func actionNames[T any](m map[string]T) string {
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	slices.Sort(names)
	return strings.Join(names, ", ")
}

func newFxCMProcessCmd() *cobra.Command {
	var (
		action string
		wait   time.Duration
	)
	cmd := &cobra.Command{
		Use:   "process <set>...",
		Short: "Establish, enable, disable or remove the Connections of sets",
		Long: `Call ProcessConnectionConfigurationSets of the ConnectionManager (Part 81
§6.7.5, Tables 84 to 86) for the sets given as arguments, with --action:

  establish-enabled   establish the Connections and enable their communication
  establish-disabled  establish them with the communication disabled
  establish           establish them as their configuration says
  enable, disable     enable or disable the communication
  remove              remove the Connections

The Method starts a run per set and returns. With --wait opcuactl then
reads the state of each set (§6.9.2) until it leaves Processing, at most
for the duration given, and prints the state it ended in: Ready, or Error
when the run failed (fx cm list shows the diagnostics).

Exit codes: 0 when every set was started (with --wait: and ended Ready);
7 when some were; 6 when none was, for a Bad status of the call, or with
--wait when a set ended in Error; 5 when --wait ran out; 9 when the
server refuses the call to the user.

Examples:
  opcuactl fx cm process -e opc.tcp://localhost:4856 --action establish-enabled Plant/Line1
  opcuactl fx cm process -e opc.tcp://localhost:4856 --action remove "nsu=http://opcfoundation.org/UA/FX/CM/;s=cm.set:Plant/Line1" --wait 30s`,
		Args: cobra.MinimumNArgs(1),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if _, ok := cmProcessActions[action]; !ok {
				return exitCodef(exitUsage, "--action must be one of %s", actionNames(cmProcessActions))
			}
			if wait < 0 {
				return exitCodef(exitUsage, "--wait must not be negative")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return withCM(func(ctx context.Context, x *cmClient) error {
				sets, err := x.resolve(ctx, args)
				if err != nil {
					return err
				}
				a := cmProcessActions[action]
				status, results, err := uafx.ProcessConnectionConfigurationSets(ctx, x.c, a, setIDs(sets))
				if err != nil {
					format.Errf("ProcessConnectionConfigurationSets: %v", err)
					return exitCode(exitProtocol, err)
				}
				out := cmCallResult("ProcessConnectionConfigurationSets", action, status, sets, results)
				var waitErr error
				if wait > 0 {
					waitErr = x.waitSets(ctx, &out, wait)
				}
				return cmp.Or(writeFxCMCallResult(out), waitErr, cmCallExit(out.Method, status, results))
			})
		},
	}
	cmd.Flags().StringVar(&action, "action", "", "What to do: "+actionNames(cmProcessActions))
	cmd.Flags().DurationVar(&wait, "wait", 0, "Wait up to this long for each set to leave Processing (0: do not wait)")
	_ = cmd.MarkFlagRequired("action")
	return cmd
}

// cmPoll is how often --wait reads the state of the sets.
var cmPoll = 200 * time.Millisecond

// waitSets reads the state of the sets that were started until each left
// Processing, at most for d. It sets their State and returns the exit for a
// set in Error or one still Processing.
func (x *cmClient) waitSets(ctx context.Context, out *model.FxCMCallResult, d time.Duration) error {
	deadline := time.Now().Add(d)
	failed, pending := 0, 0
	for i := range out.Sets {
		r := &out.Sets[i]
		if r.StatusCode != uint32(ua.StatusOK) {
			continue
		}
		n, err := parseSetID(r.NodeID)
		if err != nil {
			return exitCode(exitProtocol, err)
		}
		for {
			st, err := x.status(ctx, cmSet{Path: r.Path, NodeID: n})
			if err != nil {
				format.Errf("reading %s: %v", r.Path, err)
				return exitCode(exitProtocol, err)
			}
			r.State = cmp.Or(st.StateID, st.State)
			if r.State != "Processing" || !time.Now().Before(deadline) {
				break
			}
			select {
			case <-ctx.Done():
				return exitCode(exitInterrupted, ctx.Err())
			case <-time.After(cmPoll):
			}
		}
		switch r.State {
		case "Processing":
			pending++
		case "Error":
			failed++
		}
	}
	switch {
	case pending > 0:
		return exitCodef(exitTimeout, "%d sets still Processing after %s", pending, d)
	case failed > 0:
		return exitCodef(exitProtocol, "%d sets ended in Error (fx cm list shows the diagnostics)", failed)
	}
	return nil
}

func parseSetID(s string) (*ua.NodeID, error) {
	return ua.ParseNodeID(s)
}

func newFxCMEditCmd() *cobra.Command {
	var writes []string
	cmd := &cobra.Command{
		Use:   "edit <set> --write <path>=<value>...",
		Short: "Change the configurable Variables of a set and commit them as its next Version",
		Long: `Edit a ConnectionConfigurationSet in one session (Part 81 §6.7.4, Tables
80 to 82): EditConnectionConfigurationSets StartEditing takes the Lock of
the set, opcuactl writes each --write, and CommitUpdates makes the edit the
next Version of the set. When a write or the commit fails, opcuactl calls
DiscardUpdates, so the set keeps its Version. The Lock belongs to the
session, which is why the steps run in one command.

--write <path>=<value> names a Variable below the set by the BrowseNames
on the way to it, joined by "/" (for example
"Flow1/PublishingInterval=50"); the value is converted to the type of the
Variable's current value (Boolean, the integer and float types, String,
NodeId). A value that starts with "{" is a Variant in the OPC UA JSON
encoding (Part 6), as write value --type ua-json takes it, for the other
types. The server checks the value against the Selections of the
SelectionList Variable (Bad_OutOfRange when RestrictToList forbids it).

A commit does not change the Connections: fx cm process applies the new
Version.

Exit codes: 0 when the edit was committed; 6 when StartEditing, a write
or CommitUpdates failed (the edit was discarded); 2 when --write is not
<path>=<value> or a path is not found; 9 when the server refuses the user.

Examples:
  opcuactl fx cm edit -e opc.tcp://localhost:4856 Plant/Line1 --write Flow1/PublishingInterval=50`,
		Args: cobra.ExactArgs(1),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if len(writes) == 0 {
				return exitCodef(exitUsage, "give at least one --write <path>=<value>")
			}
			for _, w := range writes {
				if p, _, ok := strings.Cut(w, "="); !ok || p == "" {
					return exitCodef(exitUsage, "--write %q is not <path>=<value>", w)
				}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return withCM(func(ctx context.Context, x *cmClient) error {
				sets, err := x.resolve(ctx, args)
				if err != nil {
					return err
				}
				out, err := x.edit(ctx, sets[0], writes)
				return cmp.Or(writeFxCMEdit(out), err)
			})
		},
	}
	cmd.Flags().StringArrayVar(&writes, "write", nil, "<path>=<value>: a Variable below the set and its new value (repeatable)")
	return cmd
}

// edit runs StartEditing, the writes and CommitUpdates on s in one session,
// and DiscardUpdates when a step after StartEditing fails.
func (x *cmClient) edit(ctx context.Context, s cmSet, writes []string) (model.FxCMEdit, error) {
	out := model.FxCMEdit{Path: s.Path, NodeID: s.NodeID.String(), Writes: []model.FxCMWrite{}}
	call := func(a uafx.FxEdit) (ua.StatusCode, error) {
		status, results, err := uafx.EditConnectionConfigurationSets(ctx, x.c, a, []*ua.NodeID{s.NodeID})
		if err != nil {
			return 0, err
		}
		if status == ua.StatusOK && len(results) == 1 {
			return results[0], nil
		}
		if len(results) == 1 && results[0] != ua.StatusOK {
			return results[0], nil
		}
		return status, nil
	}
	fail := func(step string, sc ua.StatusCode, err error) (model.FxCMEdit, error) {
		if err == nil {
			err = sc
		}
		out.Failed = step
		format.Errf("%s: %v", step, err)
		code := exitProtocol
		if errors.Is(err, ua.StatusBadUserAccessDenied) {
			code = exitPermission
		}
		return out, exitCode(code, fmt.Errorf("%s: %w", step, err))
	}
	sc, err := call(uafx.StartEditing)
	if err != nil || sc != ua.StatusOK {
		return fail("StartEditing", sc, err)
	}
	discard := func() {
		if sc, err := call(uafx.DiscardUpdates); err == nil && sc == ua.StatusOK {
			out.Discarded = true
		}
	}
	for _, w := range writes {
		path, value, _ := strings.Cut(w, "=")
		r := model.FxCMWrite{Path: path, Value: value}
		n, err := x.path(ctx, s.NodeID, path)
		if err != nil {
			discard()
			out.Writes = append(out.Writes, r)
			out.Failed = "write " + path
			format.Errf("%v", err)
			return out, exitCode(exitUsage, err)
		}
		v, err := x.coerce(ctx, n, value)
		if err != nil {
			discard()
			out.Writes = append(out.Writes, r)
			out.Failed = "write " + path
			format.Errf("%s: %v", path, err)
			return out, exitCode(exitUsage, err)
		}
		r.NodeID = n.String()
		wsc, err := x.c.WriteNodeValue(ctx, n, v)
		if err == nil {
			r.Status, r.StatusCode = wsc.Symbol(), wsc.Uint32()
		}
		out.Writes = append(out.Writes, r)
		if err != nil || wsc != ua.StatusOK {
			discard()
			return fail("write "+path, wsc, err)
		}
	}
	sc, err = call(uafx.CommitUpdates)
	if err != nil || sc != ua.StatusOK {
		discard()
		return fail("CommitUpdates", sc, err)
	}
	out.Committed = true
	if st, err := x.status(ctx, s); err == nil {
		out.Version = st.Version
	}
	return out, nil
}

// path resolves the BrowseNames of p, joined by "/", below n.
func (x *cmClient) path(ctx context.Context, n *ua.NodeID, p string) (*ua.NodeID, error) {
	cur := n
	for _, name := range strings.Split(p, "/") {
		next, err := x.childNamed(ctx, cur, name)
		if err != nil {
			return nil, err
		}
		if next == nil {
			return nil, fmt.Errorf("--write %s: %s has no child %q", p, cur, name)
		}
		cur = next
	}
	return cur, nil
}

// childNamed returns the child of n with the BrowseName name in any
// namespace, preferring the CM namespace.
func (x *cmClient) childNamed(ctx context.Context, n *ua.NodeID, name string) (*ua.NodeID, error) {
	refs, err := x.c.Node(n).References(ctx, id.References, ua.BrowseDirectionForward, ua.NodeClassObject|ua.NodeClassVariable, true)
	if err != nil {
		return nil, fmt.Errorf("browsing %s: %w", n, err)
	}
	var found *ua.NodeID
	for _, r := range refs {
		if r.BrowseName == nil || r.BrowseName.Name != name || r.NodeID == nil || r.NodeID.NodeID == nil {
			continue
		}
		if r.BrowseName.NamespaceIndex == x.ns.CM {
			return r.NodeID.NodeID, nil
		}
		if found == nil {
			found = r.NodeID.NodeID
		}
	}
	return found, nil
}

// coerce converts value to the type of the current value of n, or decodes
// it as an OPC UA JSON Variant.
func (x *cmClient) coerce(ctx context.Context, n *ua.NodeID, value string) (any, error) {
	if strings.HasPrefix(strings.TrimSpace(value), "{") {
		v, err := parse.UAVariant(value, "ua-json")
		if err != nil {
			return nil, err
		}
		return v.Value(), nil
	}
	cur, err := x.c.Node(n).Value(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", n, err)
	}
	return coerceLike(cur.Value(), value)
}

// coerceLike converts value to the type of cur.
func coerceLike(cur any, value string) (any, error) {
	var typ string
	switch cur.(type) {
	case bool:
		typ = "bool"
	case int16:
		typ = "int16"
	case uint16:
		typ = "uint16"
	case int32:
		typ = "int32"
	case uint32:
		typ = "uint32"
	case int64:
		typ = "int64"
	case uint64:
		typ = "uint64"
	case float32:
		typ = "float"
	case float64:
		typ = "double"
	case string:
		typ = "string"
	case *ua.NodeID:
		typ = "nodeid"
	default:
		return nil, fmt.Errorf("the Variable holds a %T: give the value as an OPC UA JSON Variant ({\"UaType\":...})", cur)
	}
	return parse.CoerceValue(value, typ)
}

func writeFxCMEdit(out model.FxCMEdit) error {
	if format.ParseFormat(outputFormat) != format.Table {
		return newWriter().Write(out)
	}
	rows := make([][]string, 0, len(out.Writes))
	for _, w := range out.Writes {
		rows = append(rows, []string{w.Path, w.Value, w.Status})
	}
	if err := newWriter().WriteTable([]string{"VARIABLE", "VALUE", "STATUS"}, rows); err != nil {
		return err
	}
	switch {
	case out.Committed && out.Version != nil:
		format.Infof("committed %s: Version %d", out.Path, *out.Version)
	case out.Committed:
		format.Infof("committed %s", out.Path)
	case out.Discarded:
		format.Errf("the edit of %s was discarded", out.Path)
	}
	return nil
}

func setIDs(sets []cmSet) []*ua.NodeID {
	out := make([]*ua.NodeID, len(sets))
	for i, s := range sets {
		out[i] = s.NodeID
	}
	return out
}

func cmCallResult(method, action string, status ua.StatusCode, sets []cmSet, results []ua.StatusCode) model.FxCMCallResult {
	out := model.FxCMCallResult{Method: method, Action: action, Status: status.Symbol(), StatusCode: status.Uint32(), Sets: []model.FxCMSetResult{}}
	for i, r := range results {
		out.Sets = append(out.Sets, model.FxCMSetResult{Path: sets[i].Path, NodeID: sets[i].NodeID.String(), Status: r.Symbol(), StatusCode: r.Uint32()})
	}
	return out
}

func writeFxCMCallResult(out model.FxCMCallResult) error {
	if format.ParseFormat(outputFormat) != format.Table {
		return newWriter().Write(out)
	}
	rows := make([][]string, 0, len(out.Sets))
	for _, s := range out.Sets {
		rows = append(rows, []string{s.Path, s.Status, s.State})
	}
	return newWriter().WriteTable([]string{"SET", "STATUS", "STATE"}, rows)
}

// cmCallExit is the exit of fx cm process for the status of the call and
// the results per set (Tables 85, 86).
func cmCallExit(method string, status ua.StatusCode, results []ua.StatusCode) error {
	good := 0
	for _, r := range results {
		if r == ua.StatusOK {
			good++
		}
	}
	switch {
	case status == ua.StatusOK && good == len(results):
		return nil
	case status == ua.StatusBadUserAccessDenied:
		return exitCode(exitPermission, fmt.Errorf("%s: %w", method, status))
	case good > 0:
		return exitCodef(exitPartial, "%s returned %s: %d of %d sets succeeded", method, status.Symbol(), good, len(results))
	case allDenied(results):
		return exitCode(exitPermission, fmt.Errorf("%s: %w", method, ua.StatusBadUserAccessDenied))
	default:
		return exitCodef(exitProtocol, "%s returned %s: no set succeeded", method, status.Symbol())
	}
}

func allDenied(results []ua.StatusCode) bool {
	if len(results) == 0 {
		return false
	}
	for _, r := range results {
		if r != ua.StatusBadUserAccessDenied {
			return false
		}
	}
	return true
}

func newFxCMExportCmd() *cobra.Command {
	var file string
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Read the sets as a ConnectionConfigurationSet file",
		Long: `Read the ConnectionManagerConfiguration file of the ConnectionManager
(Part 81 F.3, opened for Read): the ConnectionConfigurationSets it holds
as the file of Annex F.2, a UABinaryFileDataType. With --out opcuactl
writes the file there, which fx cm import and other tools read; it prints
a summary of the sets either way.

Exit codes: 0 when the file was read; 3 when it cannot be decoded or
--out cannot be written; 7 when it is larger than 64 MiB; 8 when the
server has no ConnectionManager or file; 4, 5, 6 or 9 when a service
fails.

Examples:
  opcuactl fx cm export -e opc.tcp://localhost:4856
  opcuactl fx cm export -e opc.tcp://localhost:4856 --out sets.bin`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return withCM(func(ctx context.Context, x *cmClient) error {
				f, err := x.file(ctx)
				if err != nil {
					return cmFileErr(err)
				}
				data, err := f.readAll(ctx)
				if err != nil {
					return cmFileErr(err)
				}
				sets, err := fxcm.DecodeFile(data)
				if err != nil {
					format.Errf("decoding the file: %v", err)
					return exitCode(exitInvalidInput, err)
				}
				if file != "" {
					if err := os.WriteFile(file, data, 0o600); err != nil {
						format.Errf("%v", err)
						return exitCode(exitInvalidInput, err)
					}
				}
				out := model.FxCMExport{File: file, Bytes: len(data), Sets: cmFileSets(sets)}
				return writeFxCMExport(out)
			})
		},
	}
	cmd.Flags().StringVar(&file, "out", "", "Write the file (F.2) here")
	return cmd
}

// cmFileErr is the exit for a failure of the file Methods.
func cmFileErr(err error) error {
	var ee *exitError
	if errors.As(err, &ee) {
		format.Errf("%v", err)
		return err
	}
	format.Errf("%v", err)
	return exitCode(exitProtocol, err)
}

func cmFileSets(sets []*uafx.ConnectionConfigurationSetConf) []model.FxCMFileSet {
	out := []model.FxCMFileSet{}
	for _, s := range sets {
		if s == nil {
			continue
		}
		out = append(out, model.FxCMFileSet{
			Path:                 strings.Join(append(append([]string{}, s.ConnectionConfigurationSetFolder...), s.BrowseName), "/"),
			Version:              s.Version,
			Connections:          len(s.Connections),
			CommunicationFlows:   len(s.CommunicationFlows),
			AutomationComponents: len(s.AutomationComponentConfigurations),
		})
	}
	return out
}

func writeFxCMExport(out model.FxCMExport) error {
	if format.ParseFormat(outputFormat) != format.Table {
		return newWriter().Write(out)
	}
	rows := make([][]string, 0, len(out.Sets))
	for _, s := range out.Sets {
		rows = append(rows, []string{s.Path, strconv.FormatUint(uint64(s.Version), 10), strconv.Itoa(s.Connections), strconv.Itoa(s.CommunicationFlows), strconv.Itoa(s.AutomationComponents)})
	}
	return newWriter().WriteTable([]string{"SET", "VERSION", "CONNECTIONS", "FLOWS", "ACS"}, rows)
}

var cmOperations = map[string]uafx.ConnectionConfigurationSetOperation{
	"add":     uafx.ElementAdd,
	"remove":  uafx.ElementRemove,
	"replace": uafx.ElementReplace,
}

func newFxCMImportCmd() *cobra.Command {
	var (
		file     string
		ops      []string
		complete bool
	)
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Write a ConnectionConfigurationSet file and apply its sets",
		Long: `Write a ConnectionConfigurationSet file (Part 81 Annex F.2) to the
ConnectionManagerConfiguration file of the ConnectionManager (opened for
Write with EraseExisting, F.3.2) and apply its sets with CloseAndUpdate
(F.3.3, Tables F.39 to F.41), one operation per set:

  add      ElementAdd: add the set
  remove   ElementRemove: remove the set of the same Folders and Name,
           after removing its Connections
  replace  ElementReplace: replace it

--op gives one operation for every set of the file, or one per set in the
order of the file (--op add,replace). With --require-complete the server
applies all operations or none.

opcuactl checks the file decodes before it writes it, and prints
ChangesApplied (Table F.39), the result of each operation and the set
Objects the server returned.

Exit codes: 0 when every operation was applied; 7 when some were; 6 when
none was or for a Bad status of the call; 3 when the file cannot be read
or decoded; 2 when --op does not fit the file; 8 when the server has no
ConnectionManager or file; 9 when the server refuses the user.

Examples:
  opcuactl fx cm import -e opc.tcp://localhost:4856 --file sets.bin --op add
  opcuactl fx cm import -e opc.tcp://localhost:4856 --file sets.bin --op replace,add --require-complete`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := os.ReadFile(file)
			if err != nil {
				format.Errf("%v", err)
				return exitCode(exitInvalidInput, err)
			}
			sets, err := fxcm.DecodeFile(data)
			if err != nil {
				format.Errf("%s: %v", file, err)
				return exitCode(exitInvalidInput, err)
			}
			if len(sets) == 0 {
				return exitCodef(exitInvalidInput, "%s holds no ConnectionConfigurationSet", file)
			}
			operations, err := cmImportOps(ops, len(sets))
			if err != nil {
				return exitCode(exitUsage, err)
			}
			return withCM(func(ctx context.Context, x *cmClient) error {
				f, err := x.file(ctx)
				if err != nil {
					return cmFileErr(err)
				}
				applied, results, objs, err := f.update(ctx, data, complete, operations)
				if err != nil {
					return cmFileErr(err)
				}
				out := model.FxCMImport{File: file, RequireCompleteUpdate: complete, ChangesApplied: applied, Operations: []model.FxCMOperation{}}
				paths := cmFileSets(sets)
				good := 0
				for i, r := range results {
					o := model.FxCMOperation{Operation: operations[i].String(), Status: r.Symbol(), StatusCode: r.Uint32()}
					if i < len(paths) {
						o.Path = paths[i].Path
					}
					if i < len(objs) && objs[i] != nil && !isNullNodeID(objs[i]) {
						o.NodeID = objs[i].String()
					}
					if r == ua.StatusOK {
						good++
					}
					out.Operations = append(out.Operations, o)
				}
				return cmp.Or(writeFxCMImport(out), cmImportExit(applied, good, len(results), results))
			})
		},
	}
	cmd.Flags().StringVar(&file, "file", "", "ConnectionConfigurationSet file (F.2) to apply")
	cmd.Flags().StringSliceVar(&ops, "op", nil, "Operation per set: add, remove or replace (one for all, or one per set)")
	cmd.Flags().BoolVar(&complete, "require-complete", false, "Apply all operations or none (RequireCompleteUpdate)")
	_ = cmd.MarkFlagRequired("file")
	_ = cmd.MarkFlagRequired("op")
	return cmd
}

func isNullNodeID(n *ua.NodeID) bool {
	return n.Namespace() == 0 && n.Type() == ua.NodeIDTypeTwoByte && n.IntID() == 0
}

// cmImportOps returns the operation of each of n sets from --op.
func cmImportOps(ops []string, n int) ([]uafx.ConnectionConfigurationSetOperation, error) {
	if len(ops) != 1 && len(ops) != n {
		return nil, fmt.Errorf("--op gives %d operations for %d sets: give one for all or one per set", len(ops), n)
	}
	out := make([]uafx.ConnectionConfigurationSetOperation, n)
	for i := range out {
		s := ops[0]
		if len(ops) == n {
			s = ops[i]
		}
		o, ok := cmOperations[strings.TrimSpace(s)]
		if !ok {
			return nil, fmt.Errorf("--op %q: must be one of %s", s, actionNames(cmOperations))
		}
		out[i] = o
	}
	return out, nil
}

func writeFxCMImport(out model.FxCMImport) error {
	if format.ParseFormat(outputFormat) != format.Table {
		return newWriter().Write(out)
	}
	rows := make([][]string, 0, len(out.Operations))
	for _, o := range out.Operations {
		rows = append(rows, []string{o.Path, o.Operation, o.Status, o.NodeID})
	}
	if err := newWriter().WriteTable([]string{"SET", "OPERATION", "STATUS", "NODEID"}, rows); err != nil {
		return err
	}
	if !out.ChangesApplied {
		format.Errf("ChangesApplied is FALSE: no set was changed")
	}
	return nil
}

func cmImportExit(applied bool, good, n int, results []ua.StatusCode) error {
	switch {
	case applied && good == n:
		return nil
	case applied && good > 0:
		return exitCodef(exitPartial, "CloseAndUpdate applied %d of %d operations", good, n)
	case allDenied(results):
		return exitCode(exitPermission, fmt.Errorf("CloseAndUpdate: %w", ua.StatusBadUserAccessDenied))
	default:
		return exitCodef(exitProtocol, "CloseAndUpdate applied no operation")
	}
}
