// SPDX-License-Identifier: MIT

package opcuactl

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/otfabric/go-opcua"
	"github.com/otfabric/go-opcua/id"
	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/opcuactl/internal/format"
	"github.com/otfabric/opcuactl/internal/model"
	"github.com/otfabric/opcuactl/internal/parse"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// defaultNotifier is the Server object: it reports the events of every
// condition of a server that has a notifier path to it.
const defaultNotifier = "i=2253"

func newAlarmsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "alarms",
		Aliases: []string{"conditions"},
		Short:   "List, acknowledge, confirm, shelve, suppress and silence alarms and conditions",
		Long: `Work with the alarms and conditions of a server (OPC UA Part 9).

A condition is identified by its ConditionId, the NodeId of the condition
instance, as alarms list shows it. Acknowledge, Confirm and AddComment refer
to one event of the condition by its EventId: by default opcuactl takes the
EventId of the latest event, from a ConditionRefresh.

The events are taken from an event notifier, the Server object (i=2253) by
default; --node names another one (the source of a condition, an area).

A condition branch (Part 9 §5.5.2) is an earlier occurrence of an alarm that
still needs an acknowledgement or a confirmation: it is listed with its
BranchId, and alarms ack/confirm/comment --branch refer to its latest event.

--audit-entry-id sets the AuditEntryId of the requests: a server that audits
the condition methods reports it as the ClientAuditEntryId of its audit
events (alarms audit).

Examples:
  opcuactl alarms list -e opc.tcp://localhost:4840
  opcuactl alarms list -e opc.tcp://localhost:4840 --state unacked --severity-min 500
  opcuactl alarms ack -e opc.tcp://localhost:4840 "ns=2;s=Tank.LevelAlarm" -m "operator on site"
  opcuactl alarms confirm -e opc.tcp://localhost:4840 "ns=2;s=Tank.LevelAlarm"
  opcuactl alarms shelve -e opc.tcp://localhost:4840 "ns=2;s=Tank.LevelAlarm" --for 1h -m "sensor work"
  opcuactl alarms suppress -e opc.tcp://localhost:4840 "ns=2;s=Tank.LevelAlarm" -m "line stopped"
  opcuactl alarms respond -e opc.tcp://localhost:4840 "ns=2;s=Tank.Dialog" Yes
  opcuactl alarms watch -e opc.tcp://localhost:4840 --format json
  opcuactl alarms audit -e opc.tcp://localhost:4840`,
	}
	cmd.PersistentFlags().StringVar(&alarmAuditEntryID, "audit-entry-id", "", "AuditEntryId of the requests (reported as ClientAuditEntryId in audit events)")
	cmd.AddCommand(
		newAlarmsListCmd(),
		newAlarmsEventMethodCmd("ack", "Acknowledge a condition", "acknowledge", false),
		newAlarmsEventMethodCmd("confirm", "Confirm a condition", "confirm", false),
		newAlarmsEventMethodCmd("comment", "Add a comment to a condition", "comment", true),
		newAlarmsEnableCmd(true),
		newAlarmsEnableCmd(false),
		newAlarmsShelveCmd(),
		newAlarmsUnshelveCmd(),
		newAlarmsStateMethodCmd(alarmMethods["suppress"]),
		newAlarmsStateMethodCmd(alarmMethods["unsuppress"]),
		newAlarmsStateMethodCmd(alarmMethods["remove-from-service"]),
		newAlarmsStateMethodCmd(alarmMethods["place-in-service"]),
		newAlarmsStateMethodCmd(alarmMethods["silence"]),
		newAlarmsStateMethodCmd(alarmMethods["reset"]),
		newAlarmsRespondCmd(),
		newAlarmsGroupsCmd(),
		newAlarmsMetricsCmd(),
		newAlarmsWatchCmd(),
		newAlarmsAuditCmd(),
	)
	return cmd
}

// alarmAuditEntryID is --audit-entry-id of the alarms commands.
var alarmAuditEntryID string

// commentFlag registers -m/--message, also accepted as --comment.
func commentFlag(cmd *cobra.Command, comment *string, usage string) {
	cmd.Flags().StringVarP(comment, "message", "m", "", usage)
	cmd.Flags().SetNormalizeFunc(func(_ *pflag.FlagSet, name string) pflag.NormalizedName {
		if name == "comment" {
			name = "message"
		}
		return pflag.NormalizedName(name)
	})
}

// notifierFlags are the flags of the commands that subscribe to condition
// events.
type notifierFlags struct {
	node      string
	interval  time.Duration
	queueSize uint32
}

func (f *notifierFlags) register(cmd *cobra.Command) {
	cmd.Flags().StringVarP(&f.node, "node", "n", defaultNotifier, "Event notifier to take the condition events from")
	cmd.Flags().DurationVar(&f.interval, "interval", 100*time.Millisecond, "Publishing interval of the subscription")
	cmd.Flags().Uint32Var(&f.queueSize, "queue-size", 1000, "Queue size of the event monitored item (the retained conditions of a refresh must fit)")
}

func (f *notifierFlags) notifier() (*ua.NodeID, error) {
	n, err := parse.NodeID(f.node)
	if err != nil {
		return nil, exitCodef(1, "invalid --node: %v", err)
	}
	return n, nil
}

// conditionSubscription subscribes to the condition events of a notifier
// with NewConditionEventFilter.
func conditionSubscription(ctx context.Context, c *opcua.Client, f *notifierFlags) (*opcua.Subscription, chan *opcua.PublishNotificationData, error) {
	return eventSubscription(ctx, c, f, opcua.NewConditionEventFilter())
}

// eventSubscription subscribes to the events of a notifier with filter.
func eventSubscription(ctx context.Context, c *opcua.Client, f *notifierFlags, filter *ua.EventFilter) (*opcua.Subscription, chan *opcua.PublishNotificationData, error) {
	notifier, err := f.notifier()
	if err != nil {
		return nil, nil, err
	}
	ch := make(chan *opcua.PublishNotificationData, 256)
	sub, err := c.Subscribe(ctx, &opcua.SubscriptionParameters{Interval: f.interval}, ch)
	if err != nil {
		return nil, nil, exitCodef(3, "starting subscription: %w", err)
	}
	res, err := sub.Monitor(ctx, ua.TimestampsToReturnBoth, &ua.MonitoredItemCreateRequest{
		ItemToMonitor: &ua.ReadValueID{
			NodeID:       notifier,
			AttributeID:  ua.AttributeIDEventNotifier,
			DataEncoding: &ua.QualifiedName{},
		},
		MonitoringMode: ua.MonitoringModeReporting,
		RequestedParameters: &ua.MonitoringParameters{
			ClientHandle:  1,
			DiscardOldest: true,
			Filter:        ua.NewExtensionObject(filter),
			QueueSize:     f.queueSize,
		},
	})
	if err == nil && (len(res.Results) != 1 || res.Results[0] == nil) {
		err = ua.StatusBadUnknownResponse
	} else if err == nil && res.Results[0].StatusCode != ua.StatusOK {
		err = res.Results[0].StatusCode
	}
	if err != nil {
		_ = sub.Cancel(context.WithoutCancel(ctx))
		return nil, nil, exitCodef(3, "monitoring the events of %s: %w", notifier, err)
	}
	return sub, ch, nil
}

// conditionEvents returns the condition events of a notification.
func conditionEvents(notif *opcua.PublishNotificationData) ([]*opcua.ConditionEvent, error) {
	if notif.Error != nil {
		return nil, notif.Error
	}
	enl, ok := notif.Value.(*ua.EventNotificationList)
	if !ok {
		return nil, nil
	}
	out := make([]*opcua.ConditionEvent, 0, len(enl.Events))
	for _, f := range enl.Events {
		ev, err := opcua.ParseConditionEvent(f)
		if err != nil {
			return out, err
		}
		out = append(out, ev)
	}
	return out, nil
}

// conditionKey identifies a condition or one of its branches.
func conditionKey(ev *opcua.ConditionEvent) string {
	k := ev.ConditionID.String()
	if ev.IsBranch() {
		k += "|" + ev.BranchID.String()
	}
	return k
}

// refreshConditions runs a ConditionRefresh and returns the latest event of
// every retained condition and branch, ordered by severity (highest first),
// ConditionId and BranchId (the condition before its branches).
func refreshConditions(ctx context.Context, sub *opcua.Subscription, ch chan *opcua.PublishNotificationData) ([]*opcua.ConditionEvent, error) {
	if err := sub.ConditionRefresh(ctx); err != nil {
		return nil, exitCodef(3, "ConditionRefresh: %w", err)
	}
	started := false
	latest := map[string]*opcua.ConditionEvent{}
	for {
		select {
		case <-ctx.Done():
			return nil, exitCodef(3, "waiting for the end of the ConditionRefresh: %w", ctx.Err())
		case notif := <-ch:
			evs, err := conditionEvents(notif)
			if err != nil {
				format.Errf("condition event: %v", err)
			}
			for _, ev := range evs {
				switch {
				case ev.IsRefreshStart():
					started = true
				case ev.IsRefreshEnd() && started:
					return sortConditions(latest), nil
				case !started || ev.ConditionID == nil:
				case ev.Retain:
					latest[conditionKey(ev)] = ev
				default:
					delete(latest, conditionKey(ev))
				}
			}
		}
	}
}

func sortConditions(m map[string]*opcua.ConditionEvent) []*opcua.ConditionEvent {
	out := make([]*opcua.ConditionEvent, 0, len(m))
	for _, ev := range m {
		out = append(out, ev)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Severity != out[j].Severity {
			return out[i].Severity > out[j].Severity
		}
		return conditionKey(out[i]) < conditionKey(out[j])
	})
	return out
}

// conditionModel converts a condition event for output.
func conditionModel(ev *opcua.ConditionEvent) *model.Condition {
	m := &model.Condition{
		ConditionID:         ev.ConditionID.String(),
		ConditionName:       ev.ConditionName,
		SourceName:          ev.SourceName,
		Severity:            ev.Severity,
		Message:             ev.Message,
		Time:                model.FormatTimestamp(ev.Time),
		Retain:              ev.Retain,
		Enabled:             ev.Enabled,
		Active:              ev.Active,
		Acked:               ev.Acked,
		Confirmed:           ev.Confirmed,
		Shelving:            shelvingName(ev.ShelvingState),
		SuppressedOrShelved: ev.SuppressedOrShelved,
		ClientUserID:        ev.ClientUserID,
		EventID:             base64.StdEncoding.EncodeToString(ev.EventID),
		Suppressed:          ev.Suppressed,
		OutOfService:        ev.OutOfService,
		Silenced:            ev.Silenced,
		Latched:             ev.Latched,
		DialogActive:        ev.DialogActive,
		LastResponse:        ev.LastResponse,
		AudibleEnabled:      ev.AudibleEnabled,
		ReAlarmRepeatCount:  ev.ReAlarmRepeatCount,
		FirstInGroup:        ev.FirstInGroup,
	}
	if ev.IsBranch() {
		m.BranchID = ev.BranchID.String()
	}
	if ev.Prompt != nil {
		m.Prompt = ev.Prompt.Text
	}
	if ev.SourceNode != nil {
		m.SourceNode = ev.SourceNode.String()
	}
	if ev.EventType != nil {
		m.EventType = eventTypeName(ev.EventType)
	}
	if ev.Quality != nil {
		m.Quality = ev.Quality.Symbol()
	}
	if ev.Comment != nil {
		m.Comment = ev.Comment.Text
	}
	return m
}

// eventTypeName names the standard event types (ExclusiveLevelAlarmType),
// other ones by their NodeId.
func eventTypeName(n *ua.NodeID) string {
	if isNS0Numeric(n) {
		if name := id.ObjectTypeName(n.IntID()); name != "" {
			return name
		}
	}
	return n.String()
}

func isNS0Numeric(n *ua.NodeID) bool {
	if n == nil || n.Namespace() != 0 {
		return false
	}
	switch n.Type() {
	case ua.NodeIDTypeTwoByte, ua.NodeIDTypeFourByte, ua.NodeIDTypeNumeric:
		return true
	}
	return false
}

// shelvingName names the current state of a ShelvingState (its
// CurrentState/Id), "" when the condition has none.
func shelvingName(n *ua.NodeID) string {
	if n == nil {
		return ""
	}
	if isNS0Numeric(n) {
		switch n.IntID() {
		case id.ShelvedStateMachineTypeUnshelved:
			return "Unshelved"
		case id.ShelvedStateMachineTypeTimedShelved:
			return "TimedShelved"
		case id.ShelvedStateMachineTypeOneShotShelved:
			return "OneShotShelved"
		}
	}
	return n.String()
}

func yesNo(b *bool) string {
	switch {
	case b == nil:
		return "-"
	case *b:
		return "yes"
	default:
		return "no"
	}
}

func shelvedCell(s string) string {
	switch s {
	case "":
		return "-"
	case "Unshelved":
		return "no"
	case "TimedShelved":
		return "timed"
	case "OneShotShelved":
		return "one-shot"
	}
	return s
}

// conditionMatches applies the --state and --severity-min filters of
// alarms list.
func conditionMatches(c *model.Condition, states []string, severityMin uint16) (bool, error) {
	if c.Severity < severityMin {
		return false, nil
	}
	is := func(b *bool) bool { return b != nil && *b }
	isNot := func(b *bool) bool { return b != nil && !*b }
	for _, s := range states {
		var ok bool
		switch strings.ToLower(strings.TrimSpace(s)) {
		case "active":
			ok = is(c.Active)
		case "inactive":
			ok = isNot(c.Active)
		case "unacked", "unacknowledged":
			ok = isNot(c.Acked)
		case "acked", "acknowledged":
			ok = is(c.Acked)
		case "unconfirmed":
			ok = isNot(c.Confirmed)
		case "confirmed":
			ok = is(c.Confirmed)
		case "shelved":
			ok = c.Shelving == "TimedShelved" || c.Shelving == "OneShotShelved"
		case "suppressed":
			ok = is(c.Suppressed)
		case "out-of-service", "outofservice":
			ok = is(c.OutOfService)
		case "silenced":
			ok = is(c.Silenced)
		case "latched":
			ok = is(c.Latched)
		case "dialog":
			ok = is(c.DialogActive)
		case "branch":
			ok = c.BranchID != ""
		default:
			return false, fmt.Errorf("unknown --state %q (%s)", s, strings.Join(alarmStateValues, "|"))
		}
		if !ok {
			return false, nil
		}
	}
	return true, nil
}

func writeConditions(conds []*model.Condition) error {
	w := newWriter()
	if format.ParseFormat(outputFormat) != format.Table {
		if conds == nil {
			conds = []*model.Condition{}
		}
		return w.Write(conds)
	}
	if quiet {
		for _, c := range conds {
			fmt.Println(c.ConditionID)
		}
		return nil
	}
	branches := false
	for _, c := range conds {
		branches = branches || c.BranchID != ""
	}
	headers := []string{"CONDITION ID", "NAME", "SOURCE", "SEVERITY", "ACTIVE", "ACKED", "CONFIRMED", "ENABLED", "SHELVED", "EVENT ID"}
	if branches {
		headers = append(headers[:1], append([]string{"BRANCH"}, headers[1:]...)...)
	}
	if verbose {
		headers = append(headers, "STATES", "TIME", "MESSAGE")
	}
	rows := make([][]string, len(conds))
	for i, c := range conds {
		enabled := "no"
		if c.Enabled {
			enabled = "yes"
		}
		source := c.SourceName
		if source == "" {
			source = c.SourceNode
		}
		row := []string{c.ConditionID, c.ConditionName, source, fmt.Sprintf("%d", c.Severity),
			yesNo(c.Active), yesNo(c.Acked), yesNo(c.Confirmed), enabled, shelvedCell(c.Shelving), c.EventID}
		if branches {
			branch := c.BranchID
			if branch == "" {
				branch = "-"
			}
			row = append(row[:1], append([]string{branch}, row[1:]...)...)
		}
		if verbose {
			row = append(row, conditionStates(c), c.Time, c.Message)
		}
		rows[i] = row
	}
	return w.WriteTable(headers, rows)
}

// conditionStates lists the optional states of Part 9 1.05 a condition is
// in (suppressed, out-of-service, silenced, latched, dialog, first-in-group,
// re-alarmed), "-" for none.
func conditionStates(c *model.Condition) string {
	var out []string
	is := func(b *bool) bool { return b != nil && *b }
	for _, s := range []struct {
		on   bool
		name string
	}{
		{is(c.Suppressed), "suppressed"},
		{is(c.OutOfService), "out-of-service"},
		{is(c.Silenced), "silenced"},
		{is(c.Latched), "latched"},
		{is(c.DialogActive), "dialog"},
		{is(c.FirstInGroup), "first-in-group"},
		{c.ReAlarmRepeatCount != nil && *c.ReAlarmRepeatCount > 0, fmt.Sprintf("re-alarmed(%d)", derefInt16(c.ReAlarmRepeatCount))},
	} {
		if s.on {
			out = append(out, s.name)
		}
	}
	if len(out) == 0 {
		return "-"
	}
	return strings.Join(out, ",")
}

func derefInt16(p *int16) int16 {
	if p == nil {
		return 0
	}
	return *p
}

// withClient connects and runs fn with a context bounded by --timeout; the
// requests carry the AuditEntryId of --audit-entry-id.
func withClient(fn func(ctx context.Context, c *opcua.Client) error) error {
	return withClientTimeout(connCfg.Timeout, fn)
}

// withClientTimeout is withClient with the operations bounded by timeout
// instead of --timeout (0: unbounded).
func withClientTimeout(timeout time.Duration, fn func(ctx context.Context, c *opcua.Client) error) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	c, err := connCfg.NewClient(ctx)
	if err != nil {
		format.Errf("%v", err)
		return exitCode(2, err)
	}
	defer func() { _ = c.Close(context.WithoutCancel(ctx)) }()
	opCtx, cancel := ctx, context.CancelFunc(func() {})
	if timeout > 0 {
		opCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()
	if alarmAuditEntryID != "" {
		opCtx = opcua.WithAuditEntryID(opCtx, alarmAuditEntryID)
	}
	return fn(opCtx, c)
}

func newAlarmsListCmd() *cobra.Command {
	var (
		nf          notifierFlags
		states      []string
		severityMin uint16
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the retained conditions (ConditionRefresh)",
		Long: `List the conditions the server retains: the active, unacknowledged and
unconfirmed ones. opcuactl subscribes to the condition events of --node,
calls ConditionRefresh and shows the latest event of every condition and
condition branch the refresh reports.

The table shows the ConditionId, the name, the source, the severity, the
states (yes, no, or - when the condition has no such state), the shelving
state (no, timed, one-shot) and the EventId of the latest event (base64), the
one alarms ack/confirm/comment refer to. When there are condition branches a
BRANCH column shows their BranchId (- for the condition itself). -v adds the
optional states (suppressed, out-of-service, silenced, latched, dialog,
first-in-group, re-alarmed), the time and message; --format json has every
state, the dialog prompt and the last response.

Filters:
  --state S          only conditions in state S (repeatable, all must hold):
                     active, inactive, unacked, acked, unconfirmed,
                     confirmed, shelved, suppressed, out-of-service,
                     silenced, latched, dialog, branch
  --severity-min N   only conditions with Severity >= N

Examples:
  opcuactl alarms list -e opc.tcp://localhost:4840
  opcuactl alarms list -e opc.tcp://localhost:4840 --node "ns=2;s=Tank" -v
  opcuactl alarms list -e opc.tcp://localhost:4840 --state active --state unacked
  opcuactl alarms list -e opc.tcp://localhost:4840 --format json --pretty`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := conditionMatches(&model.Condition{}, states, 0); err != nil {
				return exitCode(1, err)
			}
			if _, err := nf.notifier(); err != nil {
				return err
			}
			return withClient(func(ctx context.Context, c *opcua.Client) error {
				sub, ch, err := conditionSubscription(ctx, c, &nf)
				if err != nil {
					format.Errf("%v", err)
					return err
				}
				defer func() { _ = sub.Cancel(context.WithoutCancel(ctx)) }()
				evs, err := refreshConditions(ctx, sub, ch)
				if err != nil {
					format.Errf("%v", err)
					return err
				}
				var conds []*model.Condition
				for _, ev := range evs {
					m := conditionModel(ev)
					if ok, _ := conditionMatches(m, states, severityMin); ok {
						conds = append(conds, m)
					}
				}
				return writeConditions(conds)
			})
		},
	}
	nf.register(cmd)
	cmd.Flags().StringArrayVar(&states, "state", nil, "Only conditions in this state (repeatable: "+strings.Join(alarmStateValues, "|")+")")
	cmd.Flags().Uint16Var(&severityMin, "severity-min", 0, "Only conditions with at least this Severity")
	_ = cmd.RegisterFlagCompletionFunc("state", fixedCompletion(alarmStateValues))
	return cmd
}

// latestEventID returns the EventId of the latest event of a retained
// condition, from a ConditionRefresh.
func latestEventID(ctx context.Context, c *opcua.Client, nf *notifierFlags, conditionID *ua.NodeID, branch string) ([]byte, error) {
	sub, ch, err := conditionSubscription(ctx, c, nf)
	if err != nil {
		return nil, err
	}
	defer func() { _ = sub.Cancel(context.WithoutCancel(ctx)) }()
	evs, err := refreshConditions(ctx, sub, ch)
	if err != nil {
		return nil, err
	}
	want := conditionID.String()
	var branches []string
	for _, ev := range evs {
		if ev.ConditionID.String() != want {
			continue
		}
		switch {
		case !ev.IsBranch() && branch == "":
			return ev.EventID, nil
		case ev.IsBranch() && branch != "" && sameNodeID(ev.BranchID, branch):
			return ev.EventID, nil
		case ev.IsBranch():
			branches = append(branches, ev.BranchID.String())
		}
	}
	if branch != "" {
		if len(branches) == 0 {
			branches = []string{"none"}
		}
		return nil, exitCodef(3, "condition %s has no retained branch %s (retained branches: %s)", want, branch, strings.Join(branches, ", "))
	}
	return nil, exitCodef(3, "condition %s is not retained by the server (or not visible from %s): no event to refer to; pass --event-id", want, nf.node)
}

// alarmHint explains the status codes the condition methods refuse with.
func alarmHint(err error) string {
	for _, h := range []struct {
		st   ua.StatusCode
		hint string
	}{
		{ua.StatusBadEventIDUnknown, "the EventId is not one of the recent events of the condition: use --event-id latest"},
		{ua.StatusBadConditionBranchAlreadyAcked, "the condition is already acknowledged"},
		{ua.StatusBadConditionBranchAlreadyConfirmed, "the condition is already confirmed"},
		{ua.StatusBadConditionAlreadyEnabled, "the condition is already enabled"},
		{ua.StatusBadConditionAlreadyDisabled, "the condition is already disabled"},
		{ua.StatusBadConditionDisabled, "the condition is disabled: alarms enable it first"},
		{ua.StatusBadConditionAlreadyShelved, "the alarm is already shelved: alarms unshelve it first"},
		{ua.StatusBadConditionNotShelved, "the alarm is not shelved"},
		{ua.StatusBadShelvingTimeOutOfRange, "the shelving time is longer than the MaxTimeShelved of the alarm"},
		{ua.StatusBadUserAccessDenied, "the user may not call this method: connect with an identity that may (--auth)"},
		{ua.StatusBadNodeIDUnknown, "no such condition (check the ConditionId with alarms list)"},
		{ua.StatusBadMethodInvalid, "the condition does not have this method (a ConditionType has no Acknowledge; an alarm has Confirm, Suppress, RemoveFromService, Silence and Reset only with ConfirmedState, SuppressedState, OutOfServiceState, SilenceState and LatchedState)"},
		{ua.StatusBadNoMatch, "the condition has no ShelvingState: it cannot be shelved"},
		{ua.StatusBadDialogNotActive, "the dialog is not active: there is nothing to respond to"},
		{ua.StatusBadDialogResponseInvalid, "not one of the response options of the dialog"},
		{ua.StatusBadInvalidState, "the alarm is not in a state that allows this (an active alarm cannot be reset)"},
	} {
		if errors.Is(err, h.st) {
			return h.hint
		}
	}
	return ""
}

func alarmFailed(action string, conditionID *ua.NodeID, err error) error {
	msg := fmt.Sprintf("%s %s: %v", action, conditionID, err)
	if h := alarmHint(err); h != "" {
		msg += " (" + h + ")"
	}
	format.Errf("%s", msg)
	return exitCode(3, err)
}

func writeAlarmAction(a model.AlarmAction) error {
	a.AuditEntryID = alarmAuditEntryID
	w := newWriter()
	if format.ParseFormat(outputFormat) != format.Table {
		return w.Write(a)
	}
	if quiet {
		fmt.Println(a.Status)
		return nil
	}
	headers := []string{"CONDITION ID", "ACTION", "STATUS"}
	row := []string{a.ConditionID, a.Action, a.Status}
	if a.EventID != "" {
		headers = append(headers, "EVENT ID")
		row = append(row, a.EventID)
	}
	if a.SelectedResponse != nil {
		headers = append(headers, "RESPONSE")
		row = append(row, strings.TrimSpace(fmt.Sprintf("%d %s", *a.SelectedResponse, a.Response)))
	}
	return w.WriteTable(headers, [][]string{row})
}

func conditionArg(args []string) (*ua.NodeID, error) {
	n, err := parse.NodeID(args[0])
	if err != nil {
		return nil, exitCodef(1, "invalid ConditionId: %v", err)
	}
	return n, nil
}

// newAlarmsEventMethodCmd builds alarms ack, confirm and comment: the
// methods that take the EventId of an event of the condition and a comment.
func newAlarmsEventMethodCmd(use, short, action string, commentRequired bool) *cobra.Command {
	var (
		nf      notifierFlags
		eventID string
		comment string
		branch  string
	)
	cmd := &cobra.Command{
		Use:   use + " <conditionId>",
		Short: short,
		Long: short + ` (Part 9 §5.5.6, §5.7.3, §5.7.4).

The method refers to an event of the condition by its EventId. By default
(--event-id latest) opcuactl runs a ConditionRefresh on --node and takes the
EventId of the latest event of the condition; a condition the server does not
retain any more needs an explicit --event-id (base64 as alarms list and
alarms watch print it, or 0x hex). --branch takes the latest event of a
condition branch instead (its BranchId, as alarms list shows it).

-m (or --comment) sets the comment (` + map[bool]string{true: "required", false: "optional"}[commentRequired] + `); the server stores it in the Comment of
the condition with the user that called the method (ClientUserId).

Examples:
  opcuactl alarms ` + use + ` -e opc.tcp://localhost:4840 "ns=2;s=Tank.LevelAlarm" -m "checked the level sensor"
  opcuactl alarms ` + use + ` -e opc.tcp://localhost:4840 "ns=2;s=Tank.LevelAlarm" --event-id +LbyRwxcuik8Tz1iutx3ug== -m "seen"
  opcuactl alarms ` + use + ` -e opc.tcp://localhost:4840 "ns=2;s=Tank.LevelAlarm" --branch "ns=2;g=5f0c..." -m "earlier occurrence"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			conditionID, err := conditionArg(args)
			if err != nil {
				return err
			}
			if commentRequired && comment == "" {
				return exitCodef(1, "-m/--message is required")
			}
			eid, latest, err := parse.EventID(eventID)
			if err != nil {
				return exitCodef(1, "invalid --event-id: %v", err)
			}
			if branch != "" {
				if !latest {
					return exitCodef(1, "--branch and --event-id exclude each other")
				}
				if _, err := parse.NodeID(branch); err != nil {
					return exitCodef(1, "invalid --branch: %v", err)
				}
			}
			if _, err := nf.notifier(); err != nil {
				return err
			}
			return withClient(func(ctx context.Context, c *opcua.Client) error {
				if latest {
					if eid, err = latestEventID(ctx, c, &nf, conditionID, branch); err != nil {
						format.Errf("%v", err)
						return err
					}
				}
				var call func(context.Context, *ua.NodeID, []byte, string) error
				switch action {
				case "acknowledge":
					call = c.Acknowledge
				case "confirm":
					call = c.Confirm
				default:
					call = c.AddComment
				}
				if err := call(ctx, conditionID, eid, comment); err != nil {
					return alarmFailed(action, conditionID, err)
				}
				return writeAlarmAction(model.AlarmAction{
					ConditionID: conditionID.String(),
					Action:      action,
					EventID:     base64.StdEncoding.EncodeToString(eid),
					Comment:     comment,
					Status:      ua.StatusOK.Symbol(),
				})
			})
		},
	}
	nf.register(cmd)
	cmd.Flags().StringVar(&eventID, "event-id", "latest", "EventId of the event (base64 or 0x hex), or latest")
	cmd.Flags().StringVar(&branch, "branch", "", "BranchId of a condition branch: take the latest event of the branch")
	commentFlag(cmd, &comment, "Comment")
	return cmd
}

func newAlarmsEnableCmd(enable bool) *cobra.Command {
	use, action, short := "disable", "disable", "Disable a condition"
	if enable {
		use, action, short = "enable", "enable", "Enable a condition"
	}
	return &cobra.Command{
		Use:   use + " <conditionId>",
		Short: short,
		Long: short + ` (Part 9 §5.5.4, §5.5.5).

A disabled condition reports no events, is not retained and does not appear in
alarms list; its non-identifying variables read Bad_ConditionDisabled.

Examples:
  opcuactl alarms ` + use + ` -e opc.tcp://localhost:4840 "ns=2;s=Tank.LevelAlarm"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			conditionID, err := conditionArg(args)
			if err != nil {
				return err
			}
			return withClient(func(ctx context.Context, c *opcua.Client) error {
				call := c.DisableCondition
				if enable {
					call = c.EnableCondition
				}
				if err := call(ctx, conditionID); err != nil {
					return alarmFailed(action, conditionID, err)
				}
				return writeAlarmAction(model.AlarmAction{ConditionID: conditionID.String(), Action: action, Status: ua.StatusOK.Symbol()})
			})
		},
	}
}

func newAlarmsShelveCmd() *cobra.Command {
	var (
		forDur  time.Duration
		oneShot bool
		comment string
	)
	cmd := &cobra.Command{
		Use:   "shelve <conditionId> (--for <duration> | --one-shot)",
		Short: "Shelve an alarm",
		Long: `Shelve an alarm (Part 9 §5.8.10): a shelved alarm is suppressed from
display. --for shelves it for a time (TimedShelve; the server refuses more
than the MaxTimeShelved of the alarm), --one-shot until it goes inactive
(OneShotShelve). alarms unshelve ends either. With -m (--comment) opcuactl
calls TimedShelve2 / OneShotShelve2, which store the comment with the
shelving (Part 9 1.05).

Examples:
  opcuactl alarms shelve -e opc.tcp://localhost:4840 "ns=2;s=Tank.LevelAlarm" --for 30m
  opcuactl alarms shelve -e opc.tcp://localhost:4840 "ns=2;s=Tank.LevelAlarm" --one-shot -m "sensor replaced today"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			conditionID, err := conditionArg(args)
			if err != nil {
				return err
			}
			switch {
			case oneShot && cmd.Flags().Changed("for"):
				return exitCodef(1, "--for and --one-shot exclude each other")
			case !oneShot && forDur <= 0:
				return exitCodef(1, "--for <duration> (> 0) or --one-shot is required")
			}
			return withClient(func(ctx context.Context, c *opcua.Client) error {
				a := model.AlarmAction{ConditionID: conditionID.String(), Comment: comment, Status: ua.StatusOK.Symbol()}
				switch {
				case oneShot && comment != "":
					a.Action = "one-shot-shelve"
					err = c.OneShotShelve2(ctx, conditionID, comment)
				case oneShot:
					a.Action = "one-shot-shelve"
					err = c.OneShotShelve(ctx, conditionID)
				default:
					a.Action = "timed-shelve"
					a.ShelvingMs = forDur.Milliseconds()
					if comment != "" {
						err = c.TimedShelve2(ctx, conditionID, forDur, comment)
					} else {
						err = c.TimedShelve(ctx, conditionID, forDur)
					}
				}
				if err != nil {
					return alarmFailed(a.Action, conditionID, err)
				}
				return writeAlarmAction(a)
			})
		},
	}
	cmd.Flags().DurationVar(&forDur, "for", 0, "Shelve for this long (TimedShelve)")
	cmd.Flags().BoolVar(&oneShot, "one-shot", false, "Shelve until the alarm goes inactive (OneShotShelve)")
	commentFlag(cmd, &comment, "Comment (TimedShelve2 / OneShotShelve2)")
	return cmd
}

func newAlarmsUnshelveCmd() *cobra.Command {
	var comment string
	cmd := &cobra.Command{
		Use:   "unshelve <conditionId>",
		Short: "Unshelve an alarm",
		Long: `Unshelve a shelved alarm (Part 9 §5.8.10). With -m (--comment) opcuactl
calls Unshelve2, which stores the comment.

Examples:
  opcuactl alarms unshelve -e opc.tcp://localhost:4840 "ns=2;s=Tank.LevelAlarm"
  opcuactl alarms unshelve -e opc.tcp://localhost:4840 "ns=2;s=Tank.LevelAlarm" -m "back in operation"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			conditionID, err := conditionArg(args)
			if err != nil {
				return err
			}
			return withClient(func(ctx context.Context, c *opcua.Client) error {
				if comment != "" {
					err = c.Unshelve2(ctx, conditionID, comment)
				} else {
					err = c.Unshelve(ctx, conditionID)
				}
				if err != nil {
					return alarmFailed("unshelve", conditionID, err)
				}
				return writeAlarmAction(model.AlarmAction{ConditionID: conditionID.String(), Action: "unshelve", Comment: comment, Status: ua.StatusOK.Symbol()})
			})
		},
	}
	commentFlag(cmd, &comment, "Comment (Unshelve2)")
	return cmd
}

func newAlarmsWatchCmd() *cobra.Command {
	var (
		nf        notifierFlags
		noRefresh bool
		duration  time.Duration
		count     int
		jsonl     bool
	)
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Stream condition events (refresh, then live)",
		Long: `Stream the condition events of --node: first the retained conditions of a
ConditionRefresh (between a refresh start and end marker), then every state
change as it happens. Each event carries the EventId that alarms ack,
confirm and comment take. Events of condition branches carry
branch=<BranchId>; the optional states an alarm is in follow states=.

With --format json (or --jsonl) every line is a JSON object; the refresh
markers have "refresh": "start" / "end". --no-refresh skips the refresh.
Stop with Ctrl+C, --duration or --count (condition events, not markers).

opcuactl monitor alarm subscribes to plain event fields of your choice; alarms
watch uses the condition fields of Part 9 and ConditionRefresh.

Examples:
  opcuactl alarms watch -e opc.tcp://localhost:4840
  opcuactl alarms watch -e opc.tcp://localhost:4840 --node "ns=2;s=Tank" --jsonl
  opcuactl alarms watch -e opc.tcp://localhost:4840 --no-refresh --duration 10m`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := nf.notifier(); err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			c, err := connCfg.NewClient(ctx)
			if err != nil {
				format.Errf("%v", err)
				return exitCode(2, err)
			}
			defer func() { _ = c.Close(context.WithoutCancel(ctx)) }()

			opCtx, cancel := connCfg.TimeoutContext(ctx)
			sub, ch, err := conditionSubscription(opCtx, c, &nf)
			if err == nil && !noRefresh {
				if rerr := sub.ConditionRefresh(opCtx); rerr != nil {
					_ = sub.Cancel(context.WithoutCancel(ctx))
					err = exitCodef(3, "ConditionRefresh: %w", rerr)
				}
			}
			cancel()
			if err != nil {
				format.Errf("%v", err)
				return err
			}
			defer func() { _ = sub.Cancel(context.WithoutCancel(ctx)) }()
			if verbose {
				format.Infof("subscription %d started, watching conditions on %s", sub.SubscriptionID, nf.node)
			}

			var deadline <-chan time.Time
			if duration > 0 {
				timer := time.NewTimer(duration)
				defer timer.Stop()
				deadline = timer.C
			}
			useJSON := jsonl || format.ParseFormat(outputFormat) != format.Table
			received := 0
			for {
				select {
				case <-ctx.Done():
					return nil
				case <-deadline:
					return nil
				case notif := <-ch:
					evs, err := conditionEvents(notif)
					if err != nil {
						format.Errf("condition event: %v", err)
					}
					for _, ev := range evs {
						line := conditionNotification(ev, time.Now())
						if line == nil {
							continue
						}
						printConditionNotification(line, useJSON)
						if line.Condition == nil {
							continue
						}
						received++
						if count > 0 && received >= count {
							return nil
						}
					}
				}
			}
		},
	}
	nf.register(cmd)
	cmd.Flags().BoolVar(&noRefresh, "no-refresh", false, "Do not call ConditionRefresh: only live events")
	cmd.Flags().DurationVar(&duration, "duration", 0, "Stop after this duration (0 = run until Ctrl+C)")
	cmd.Flags().IntVar(&count, "count", 0, "Stop after this many condition events (0 = unlimited)")
	cmd.Flags().BoolVar(&jsonl, "jsonl", false, "Output newline-delimited JSON (JSONL)")
	return cmd
}

// conditionNotification converts an event for alarms watch: a refresh
// marker, a condition event, or nil for an event of no condition.
func conditionNotification(ev *opcua.ConditionEvent, now time.Time) *model.ConditionNotification {
	n := &model.ConditionNotification{Timestamp: model.FormatTimestamp(now)}
	switch {
	case ev.IsRefreshStart():
		n.Refresh = "start"
	case ev.IsRefreshEnd():
		n.Refresh = "end"
	case ev.ConditionID == nil:
		return nil
	default:
		n.Condition = conditionModel(ev)
	}
	return n
}

func printConditionNotification(n *model.ConditionNotification, useJSON bool) {
	if useJSON {
		data, _ := json.Marshal(n)
		fmt.Println(string(data))
		return
	}
	if n.Condition == nil {
		fmt.Printf("%s  -- refresh %s --\n", n.Timestamp, n.Refresh)
		return
	}
	c := n.Condition
	enabled := "no"
	if c.Enabled {
		enabled = "yes"
	}
	branch, states := "", ""
	if c.BranchID != "" {
		branch = " branch=" + c.BranchID
	}
	if st := conditionStates(c); st != "-" {
		states = " states=" + st
	}
	fmt.Printf("%s  %s  %s  severity=%d active=%s acked=%s confirmed=%s enabled=%s shelved=%s%s retain=%v%s eventId=%s  %q\n",
		n.Timestamp, c.ConditionID, c.ConditionName, c.Severity, yesNo(c.Active), yesNo(c.Acked), yesNo(c.Confirmed),
		enabled, shelvedCell(c.Shelving), states, c.Retain, branch, c.EventID, c.Message)
}
