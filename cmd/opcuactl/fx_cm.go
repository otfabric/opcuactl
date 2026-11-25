// SPDX-License-Identifier: MIT

package opcuactl

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
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

// withCM runs fn with the ConnectionManager of the server, bounded by
// --timeout.
func withCM(fn func(ctx context.Context, x *cmClient) error) error {
	return withCMTimeout(connCfg.Timeout, fn)
}

// withCMTimeout is withCM bounded by timeout (0: unbounded).
func withCMTimeout(timeout time.Duration, fn func(ctx context.Context, x *cmClient) error) error {
	return withClientTimeout(timeout, func(ctx context.Context, c *opcua.Client) error {
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

opcuactl reads at most 10000 sets, 8 Folder levels and 100000 Objects
below ConnectionConfigurationSets; more is exit 7. A value the server
does not return is left out and named in "unread" (JSON).

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
					return exitCodef(exitPartial, "the listing was cut short at %d sets, %d Folder levels or %d nodes", cmMaxSets, cmMaxDepth, cmMaxNodes)
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
	for _, s := range out.Sets {
		if len(s.Unread) > 0 {
			format.Errf("%s: the server did not return %s", s.Path, strings.Join(s.Unread, ", "))
		}
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
reads the state of the sets that were started (§6.9.2), by the NodeId in
CurrentState.Id, until each left Processing, and prints the state it
ended in: Ready, or Error when the run failed (fx cm list shows the
diagnostics). --wait bounds the whole wait, the reads included, and
comes on top of --timeout; when it runs out opcuactl stops watching, and
the server goes on processing.
Only a set read in Ready counts as done: one whose state cannot be read
or is not a state of §6.9.2 is a failure.

Exit codes: 0 when every set was started (with --wait: and read in
Ready); 7 when some were; 6 when none was, for a Bad status of the call,
or with --wait when a set ended in Error or its state could not be
established; 5 when --wait ran out; 8 when the server has no such
Method; 9 when the server refuses the call to the user.

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
			// --timeout bounds the call; --wait comes on top of it.
			timeout := connCfg.Timeout
			if timeout > 0 && wait > 0 {
				timeout += wait
			}
			return withCMTimeout(timeout, func(ctx context.Context, x *cmClient) error {
				sets, err := x.resolve(ctx, args)
				if err != nil {
					return err
				}
				a := cmProcessActions[action]
				status, results, err := uafx.ProcessConnectionConfigurationSets(ctx, x.c, a, setIDs(sets))
				if err != nil {
					return cmMethodExit("ProcessConnectionConfigurationSets", err)
				}
				out := cmCallResult("ProcessConnectionConfigurationSets", action, status, sets, results)
				var waitErr error
				if wait > 0 {
					waitErr = x.waitSets(ctx, &out, sets, wait)
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
// Processing, within d for all of it. It sets their State and returns the
// exit for a set in Error, one whose state was not established, or one
// still Processing.
func (x *cmClient) waitSets(ctx context.Context, out *model.FxCMCallResult, sets []cmSet, d time.Duration) error {
	wctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	waitErr := func(err error) error {
		if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
			return exitCodef(exitTimeout, "--wait %s ran out: the server goes on processing (fx cm list shows the state)", d)
		}
		format.Errf("reading the state of the sets: %v", err)
		return exitCode(exitProtocol, err)
	}
	type pending struct {
		r  *model.FxCMSetResult
		id *ua.NodeID
	}
	var ps []*pending
	failed, unknown := 0, 0
	for i := range out.Sets {
		r := &out.Sets[i]
		if r.StatusCode != uint32(ua.StatusOK) {
			continue
		}
		r.State = "unread"
		sm, err := x.stateMachine(wctx, sets[i].NodeID)
		var curID *ua.NodeID
		if err == nil {
			_, _, curID, err = x.stateNodes(wctx, sm)
		}
		if err != nil {
			return waitErr(err)
		}
		if curID == nil {
			r.State = "unknown"
			unknown++
			continue
		}
		ps = append(ps, &pending{r: r, id: curID})
	}
	for len(ps) > 0 {
		reads := make([]cmRead, len(ps))
		for i, p := range ps {
			reads[i] = cmRead{name: strconv.Itoa(i), n: p.id, set: func(v any) bool {
				_, ok := v.(*ua.NodeID)
				p.r.State = cmp.Or(x.stateName(v), "unknown")
				return ok
			}}
		}
		unread, err := x.readValues(wctx, reads)
		if err != nil {
			return waitErr(err)
		}
		for _, name := range unread {
			i, _ := strconv.Atoi(name)
			ps[i].r.State = "unread"
		}
		next := ps[:0]
		for _, p := range ps {
			switch p.r.State {
			case "Processing":
				next = append(next, p)
			case "Ready":
			case "Error":
				failed++
			default:
				unknown++
			}
		}
		ps = next
		if len(ps) == 0 {
			break
		}
		select {
		case <-wctx.Done():
			return waitErr(wctx.Err())
		case <-time.After(cmPoll):
		}
	}
	switch {
	case failed > 0:
		return exitCodef(exitProtocol, "%d sets ended in Error (fx cm list shows the diagnostics)", failed)
	case unknown > 0:
		return exitCodef(exitProtocol, "the state of %d sets could not be established", unknown)
	}
	return nil
}

// cmMethodExit is the exit for a failed call of a ConnectionManager
// Method: 8 when the server has not the Method, 9 when it refuses the
// user (refine), 4 or 5 when the call got no answer, 6 otherwise.
func cmMethodExit(method string, err error) error {
	format.Errf("%s: %v", method, err)
	if errors.Is(err, ua.StatusBadMethodInvalid) || errors.Is(err, ua.StatusBadNotImplemented) {
		return exitCode(exitUnsupported, err)
	}
	return exitCode(exitProtocol, err)
}

// answered reports whether err is a Bad status the server returned, so
// that the call is known to have had no effect, rather than a call that
// got no answer.
func answered(err error) bool {
	var sc ua.StatusCode
	if !errors.As(err, &sc) || connectionLost(err) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return false
	}
	return sc != ua.StatusBadTimeout && sc != ua.StatusBadRequestTimeout
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
session, which is why the steps run in one command; opcuactl renews it
(RenewLock, OPC 10000-100 §7.6) when the edit takes longer than a few
seconds, and a Lock that is lost ends the edit.

The result says what became of the edit ("outcome" in JSON). A Good
CommitUpdates is not enough: a server ignores the commit of a set no
longer in edit, for one when the Lock expired (§6.7.4). So opcuactl reads
the Version before StartEditing and after the commit, and the written
Variables: committed when the Version went up by one and they read the
values written; not-committed when the edit ended before the commit, the
server refused it, or answered Good with the Version unchanged; unknown
when the commit cannot be established (the Version cannot be read, went up
by more than one, or a Variable reads another value): fx cm list shows the
set. When CommitUpdates gets no answer, the same reads decide between
committed and unknown, never not-committed: the answer may be lost after
the commit, and a server ignores DiscardUpdates once the commit ended the
edit, so a Good DiscardUpdates proves nothing.

--write <path>=<value> names a Variable below the set by the BrowseNames
of the hierarchical references on the way to it, joined by "/" (for
example "Flow1/PublishingInterval=50"). A name that more than one child
has is refused; "<namespace index>:<name>" picks one (fx browse shows the
BrowseNames). The value is converted to the type of the Variable's
current value (Boolean, the integer and float types, String, NodeId). A value that starts with "{" is a Variant in the OPC UA JSON
encoding (Part 6), as write value --type ua-json takes it, for the other
types. The server checks the value against the Selections of the
SelectionList Variable (Bad_OutOfRange when RestrictToList forbids it).

A commit does not change the Connections: fx cm process applies the new
Version.

Exit codes: 0 when the edit was committed (also when CommitUpdates got no
answer and the Version and the values read back show the commit); 6 when
StartEditing, a write, RenewLock or CommitUpdates failed, or the commit
was ignored or cannot be established; 4 or 5 when the server stopped
answering and the commit cannot be established (outcome unknown); 2 when --write is not <path>=<value> or a
path is not found, is ambiguous or is not a Variable; 9 when the server
refuses the user.

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

// cmRenewAfter is how long an edit goes without a call to the server
// before opcuactl renews its Lock.
var cmRenewAfter = 5 * time.Second

// edit runs StartEditing, the writes and CommitUpdates on s in one session,
// and DiscardUpdates when a step after StartEditing fails.
func (x *cmClient) edit(ctx context.Context, s cmSet, writes []string) (model.FxCMEdit, error) {
	out := model.FxCMEdit{Path: s.Path, NodeID: s.NodeID.String(), Writes: []model.FxCMWrite{}, Outcome: "not-committed"}
	call := func(ctx context.Context, a uafx.FxEdit) (ua.StatusCode, error) {
		status, results, err := uafx.EditConnectionConfigurationSets(ctx, x.c, a, []*ua.NodeID{s.NodeID})
		if err != nil {
			return 0, err
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
		if errors.Is(err, ua.StatusBadMethodInvalid) || errors.Is(err, ua.StatusBadNotImplemented) {
			code = exitUnsupported
		}
		return out, exitCode(code, fmt.Errorf("%s: %w", step, err))
	}
	usage := func(path string, err error) (model.FxCMEdit, error) {
		out.Failed = "write " + path
		format.Errf("%v", err)
		return out, exitCode(exitUsage, err)
	}
	// endEdit calls DiscardUpdates and reports whether it answered Good.
	// A server ignores DiscardUpdates of a set that is not in edit
	// (§6.7.4), so Good drops the edit only while the edit is known to be
	// open, before CommitUpdates was sent.
	endEdit := func() bool {
		cctx, cancel := cleanupContext(ctx)
		defer cancel()
		sc, err := call(cctx, uafx.DiscardUpdates)
		switch {
		case err != nil:
			out.CleanupError = err.Error()
		case sc != ua.StatusOK:
			out.CleanupError = sc.Symbol()
		default:
			return true
		}
		format.Errf("DiscardUpdates: %s: the server discards the edit when the session ends (Part 81 §6.7.4)", out.CleanupError)
		return false
	}
	discard := func() { out.Discarded = endEdit() }
	// The Version before the edit: a Good CommitUpdates alone does not
	// show a commit, since a server ignores CommitUpdates of a set that is
	// no longer in edit (§6.7.4), for one when the Lock expired after the
	// last write.
	var before *uint32
	if st, err := x.status(ctx, s); err == nil {
		before = st.Version
	}
	sc, err := call(ctx, uafx.StartEditing)
	if err != nil || sc != ua.StatusOK {
		return fail("StartEditing", sc, err)
	}
	renew, err := x.lockRenewal(ctx, s.NodeID)
	if err != nil {
		discard()
		return fail("Lock", 0, err)
	}
	var written []cmWritten
	for _, w := range writes {
		path, value, _ := strings.Cut(w, "=")
		r := model.FxCMWrite{Path: path, Value: value}
		n, err := x.path(ctx, s.NodeID, path)
		if err != nil {
			discard()
			out.Writes = append(out.Writes, r)
			return usage(path, err)
		}
		v, err := x.coerce(ctx, n, value)
		if err != nil {
			discard()
			out.Writes = append(out.Writes, r)
			return usage(path, fmt.Errorf("%s: %w", path, err))
		}
		if err := renew(); err != nil {
			discard()
			out.Writes = append(out.Writes, r)
			return fail("RenewLock", 0, err)
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
		written = append(written, cmWritten{path, n, v})
	}
	if err := renew(); err != nil {
		discard()
		return fail("RenewLock", 0, err)
	}
	sc, err = call(ctx, uafx.CommitUpdates)
	switch {
	case err == nil && sc == ua.StatusOK:
		return x.verifyCommit(ctx, s, before, written, out)
	case err == nil || answered(err):
		discard()
		return fail("CommitUpdates", sc, err)
	default:
		// CommitUpdates got no answer: it may have been applied, and the
		// answer lost. DiscardUpdates ends an edit that is still open, but
		// its Good answer shows nothing, since a server ignores it once the
		// commit ended the edit (§6.7.4). Only the Version and the values
		// read back, within the cleanup bound, can show the commit.
		endEdit()
		out.Outcome = "unknown"
		rctx, cancel := cleanupContext(ctx)
		defer cancel()
		committed, why := x.commitEvidence(rctx, s, before, written, &out)
		if committed {
			out.Committed, out.Outcome = true, "committed"
			format.Errf("CommitUpdates got no answer (%v), but the Version and the values read back show the commit", err)
			return out, nil
		}
		format.Errf("CommitUpdates got no answer: whether %s has its next Version is unknown: %s (fx cm list shows it)", s.Path, why)
		return fail("CommitUpdates", sc, err)
	}
}

// cmWritten is a write of an edit: the Variable and the value written.
type cmWritten struct {
	path string
	n    *ua.NodeID
	v    any
}

// verifyCommit establishes what a Good CommitUpdates did (§6.7.4):
// committed when the Version went from before to before+1 and the written
// Variables read the values written; not-committed when the Version is
// unchanged (the server ignored the commit of an edit that had ended);
// unknown otherwise.
func (x *cmClient) verifyCommit(ctx context.Context, s cmSet, before *uint32, written []cmWritten, out model.FxCMEdit) (model.FxCMEdit, error) {
	committed, why := x.commitEvidence(ctx, s, before, written, &out)
	switch {
	case committed:
		out.Committed, out.Outcome = true, "committed"
		return out, nil
	case before != nil && out.Version != nil && *out.Version == *before:
		out.Failed = "CommitUpdates"
		err := fmt.Errorf("CommitUpdates answered Good, but the Version of %s is still %d: the edit had ended (its Lock went) and the server ignored the commit (Part 81 §6.7.4)", s.Path, *before)
		format.Errf("%v", err)
		return out, exitCode(exitProtocol, err)
	}
	out.Outcome, out.Failed = "unknown", "CommitUpdates"
	err := fmt.Errorf("CommitUpdates answered Good, but the commit cannot be established: %s (fx cm list shows the Version of %s)", why, s.Path)
	format.Errf("%v", err)
	return out, exitCode(exitProtocol, err)
}

// commitEvidence reports whether the set shows the commit of an edit
// begun at Version before: the Version is before+1 and the written
// Variables read the values written. It sets out.Version to the Version
// read; why says what is missing when it reports FALSE.
func (x *cmClient) commitEvidence(ctx context.Context, s cmSet, before *uint32, written []cmWritten, out *model.FxCMEdit) (bool, string) {
	st, err := x.status(ctx, s)
	switch {
	case err != nil:
		return false, fmt.Sprintf("reading the set: %v", err)
	case before == nil || st.Version == nil:
		return false, "the Version of the set cannot be read"
	}
	out.Version = st.Version
	if after := *st.Version; after != *before+1 {
		return false, fmt.Sprintf("the Version went from %d to %d, not %d", *before, after, *before+1)
	}
	reads := make([]cmRead, len(written))
	got := make([]any, len(written))
	for i, w := range written {
		reads[i] = cmRead{name: w.path, n: w.n, set: func(v any) bool {
			got[i] = v
			return reflect.DeepEqual(v, w.v)
		}}
	}
	unread, err := x.readValues(ctx, reads)
	if err != nil {
		return false, fmt.Sprintf("reading the written Variables: %v", err)
	}
	if len(unread) > 0 {
		for i, w := range written {
			if w.path != unread[0] {
				continue
			}
			if got[i] == nil {
				return false, fmt.Sprintf("%s cannot be read back", w.path)
			}
			return false, fmt.Sprintf("%s reads %v, not the %v written", w.path, got[i], w.v)
		}
	}
	return true, ""
}

// lockRenewal returns a func that renews the Lock of set n (RenewLock,
// OPC 10000-100 §7.6) when cmRenewAfter passed since it last ran; it does
// nothing for a set whose Lock has no RenewLock.
func (x *cmClient) lockRenewal(ctx context.Context, n *ua.NodeID) (func() error, error) {
	var lock, renew *ua.NodeID
	if x.di != 0 {
		var err error
		if lock, err = x.child(ctx, n, x.di, "Lock"); err != nil {
			return nil, err
		}
		if lock != nil {
			if renew, err = x.child(ctx, lock, x.di, "RenewLock"); err != nil {
				return nil, err
			}
		}
	}
	last := time.Now()
	return func() error {
		if renew == nil || time.Since(last) < cmRenewAfter {
			return nil
		}
		res, err := x.c.Call(ctx, &ua.CallMethodRequest{ObjectID: lock, MethodID: renew})
		if err != nil {
			return err
		}
		if res.StatusCode != ua.StatusOK {
			return res.StatusCode
		}
		if len(res.OutputArguments) != 1 || res.OutputArguments[0] == nil {
			return fmt.Errorf("RenewLock returned %d output arguments, want 1", len(res.OutputArguments))
		}
		st, ok := res.OutputArguments[0].Value().(int32)
		if !ok {
			return fmt.Errorf("the RenewLockStatus is a %T, not an Int32", res.OutputArguments[0].Value())
		}
		if st != 0 {
			return fmt.Errorf("RenewLockStatus %d: the session no longer holds the Lock", st)
		}
		last = time.Now()
		return nil
	}, nil
}

// path resolves the BrowseNames of p, joined by "/", below n along
// hierarchical references, to a Variable.
func (x *cmClient) path(ctx context.Context, n *ua.NodeID, p string) (*ua.NodeID, error) {
	cur := n
	segs := strings.Split(p, "/")
	for i, seg := range segs {
		next, class, err := x.childNamed(ctx, cur, seg)
		if err != nil {
			return nil, fmt.Errorf("--write %s: %w", p, err)
		}
		if next == nil {
			return nil, fmt.Errorf("--write %s: %s has no child %q", p, cur, seg)
		}
		if i == len(segs)-1 && class != ua.NodeClassVariable {
			return nil, fmt.Errorf("--write %s: %s is not a Variable", p, next)
		}
		cur = next
	}
	return cur, nil
}

// childNamed returns the hierarchical child of n with the BrowseName seg:
// a name in any namespace, or "<namespace index>:<name>". A name more than
// one child has is an error.
func (x *cmClient) childNamed(ctx context.Context, n *ua.NodeID, seg string) (*ua.NodeID, ua.NodeClass, error) {
	name, ns, qualified := seg, uint16(0), false
	if i := strings.IndexByte(seg, ':'); i > 0 {
		if v, err := strconv.ParseUint(seg[:i], 10, 16); err == nil {
			name, ns, qualified = seg[i+1:], uint16(v), true
		}
	}
	refs, err := x.c.Node(n).References(ctx, id.HierarchicalReferences, ua.BrowseDirectionForward, ua.NodeClassObject|ua.NodeClassVariable, true)
	if err != nil {
		return nil, 0, fmt.Errorf("browsing %s: %w", n, err)
	}
	var found *ua.NodeID
	var class ua.NodeClass
	var names []string
	for _, r := range refs {
		nid := local(r)
		if nid == nil || r.BrowseName.Name != name || qualified && r.BrowseName.NamespaceIndex != ns {
			continue
		}
		if found != nil && found.Equal(nid) {
			continue
		}
		names = append(names, fmt.Sprintf("%d:%s", r.BrowseName.NamespaceIndex, r.BrowseName.Name))
		found, class = nid, r.NodeClass
	}
	if len(names) > 1 {
		return nil, 0, fmt.Errorf("%s has %d children named %q (%s): give \"<namespace index>:<name>\": %w", n, len(names), name, strings.Join(names, ", "), errAmbiguous)
	}
	return found, class, nil
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
	case int8:
		v, err := strconv.ParseInt(value, 10, 8)
		return int8(v), err
	case uint8:
		v, err := strconv.ParseUint(value, 10, 8)
		return uint8(v), err
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
	case out.Outcome == "unknown":
		format.Errf("the outcome of the edit of %s is unknown", out.Path)
	case out.Discarded:
		format.Errf("the edit of %s was discarded", out.Path)
	default:
		format.Errf("the edit of %s was not committed", out.Path)
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

opcuactl checks the file decodes and is at most 64 MiB before it writes
it, and prints ChangesApplied (Table F.39), the result of each operation
and the set Objects the server returned. CloseAndUpdate is called once and
never retried; an answer whose ChangesApplied contradicts its Results is
exit 6.

Exit codes: 0 when every operation was applied; 7 when some were; 6 when
none was, for a Bad status of the call or an answer that does not fit
Table F.39; 3 when the file cannot be read or decoded or is too large; 2 when --op does not fit the file; 8 when the server has no
ConnectionManager or file; 9 when the server refuses the user.

Examples:
  opcuactl fx cm import -e opc.tcp://localhost:4856 --file sets.bin --op add
  opcuactl fx cm import -e opc.tcp://localhost:4856 --file sets.bin --op replace,add --require-complete`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := readBounded(file, cmMaxFileSize)
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
				u, err := f.update(ctx, data, complete, operations)
				if err != nil {
					return cmFileErr(err)
				}
				applied, results, objs := u.applied, u.results, u.objs
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

// isNullNodeID reports whether n is the null NodeId in any of its numeric
// encodings (Part 3 §8.2.4, Part 6 §5.2.2.9).
func isNullNodeID(n *ua.NodeID) bool {
	switch n.Type() {
	case ua.NodeIDTypeTwoByte, ua.NodeIDTypeFourByte, ua.NodeIDTypeNumeric:
		return n.Namespace() == 0 && n.IntID() == 0
	}
	return false
}

// readBounded reads the file name, at most max bytes.
func readBounded(name string, max int) ([]byte, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, int64(max)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > max {
		return nil, fmt.Errorf("%s is larger than %d bytes", name, max)
	}
	return data, nil
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
