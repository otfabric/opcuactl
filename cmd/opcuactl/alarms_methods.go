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
	"strconv"
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
)

// sameNodeID reports whether n is the NodeId s names.
func sameNodeID(n *ua.NodeID, s string) bool {
	m, err := parse.NodeID(s)
	return err == nil && n != nil && n.String() == m.String()
}

// alarmMethod is a method of the Part 9 1.05 alarm states that takes no
// argument but an optional comment (the *2 variant).
type alarmMethod struct {
	use     string
	aliases []string
	short   string
	action  string
	long    string
	plain   func(c *opcua.Client) func(context.Context, *ua.NodeID) error
	comment func(c *opcua.Client) func(context.Context, *ua.NodeID, string) error
}

var alarmMethods = map[string]alarmMethod{
	"suppress": {
		use: "suppress", short: "Suppress an alarm", action: "suppress",
		long: `Suppress an alarm (Suppress, Part 9 §5.8.5): a suppressed alarm is not
shown to the operator, typically because the plant section is stopped. The
alarm needs a SuppressedState.`,
		plain:   func(c *opcua.Client) func(context.Context, *ua.NodeID) error { return c.Suppress },
		comment: func(c *opcua.Client) func(context.Context, *ua.NodeID, string) error { return c.Suppress2 },
	},
	"unsuppress": {
		use: "unsuppress", short: "Unsuppress an alarm", action: "unsuppress",
		long:    `Unsuppress a suppressed alarm (Unsuppress, Part 9 §5.8.6).`,
		plain:   func(c *opcua.Client) func(context.Context, *ua.NodeID) error { return c.Unsuppress },
		comment: func(c *opcua.Client) func(context.Context, *ua.NodeID, string) error { return c.Unsuppress2 },
	},
	"remove-from-service": {
		use: "remove-from-service", aliases: []string{"out-of-service"}, short: "Take an alarm out of service", action: "remove-from-service",
		long: `Take an alarm out of service (RemoveFromService, Part 9 §5.8.7): the
alarm is not evaluated, for example while its sensor is repaired. The alarm
needs an OutOfServiceState. alarms place-in-service ends it.`,
		plain:   func(c *opcua.Client) func(context.Context, *ua.NodeID) error { return c.RemoveFromService },
		comment: func(c *opcua.Client) func(context.Context, *ua.NodeID, string) error { return c.RemoveFromService2 },
	},
	"place-in-service": {
		use: "place-in-service", aliases: []string{"in-service"}, short: "Put an alarm back in service", action: "place-in-service",
		long:    `Put an alarm that is out of service back in service (PlaceInService, Part 9 §5.8.8).`,
		plain:   func(c *opcua.Client) func(context.Context, *ua.NodeID) error { return c.PlaceInService },
		comment: func(c *opcua.Client) func(context.Context, *ua.NodeID, string) error { return c.PlaceInService2 },
	},
	"silence": {
		use: "silence", short: "Silence an alarm", action: "silence",
		long: `Silence an alarm (Silence, Part 9 §5.8.9): the audible annunciation stops
while the alarm stays unacknowledged. A new activation ends the silence. The
alarm needs a SilenceState; Silence takes no comment.`,
		plain: func(c *opcua.Client) func(context.Context, *ua.NodeID) error { return c.Silence },
	},
	"reset": {
		use: "reset", short: "Reset a latched alarm", action: "reset",
		long: `Reset a latched alarm (Reset, Part 9 §5.8.11): a latching alarm stays
latched after it returns to normal until it is reset. The alarm must be
inactive and needs a LatchedState.`,
		plain:   func(c *opcua.Client) func(context.Context, *ua.NodeID) error { return c.Reset },
		comment: func(c *opcua.Client) func(context.Context, *ua.NodeID, string) error { return c.Reset2 },
	},
}

func newAlarmsStateMethodCmd(m alarmMethod) *cobra.Command {
	var comment string
	long := m.long + "\n"
	example := "\nExamples:\n  opcuactl alarms " + m.use + ` -e opc.tcp://localhost:4840 "ns=2;s=Tank.LevelAlarm"`
	if m.comment != nil {
		long += `
With -m (--comment) opcuactl calls the *2 variant of the method, which
stores the comment in the Comment of the condition (Part 9 1.05).
`
		example += "\n  opcuactl alarms " + m.use + ` -e opc.tcp://localhost:4840 "ns=2;s=Tank.LevelAlarm" -m "maintenance"`
	}
	cmd := &cobra.Command{
		Use:     m.use + " <conditionId>",
		Aliases: m.aliases,
		Short:   m.short,
		Long:    long + example,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			conditionID, err := conditionArg(args)
			if err != nil {
				return err
			}
			return withClient(func(ctx context.Context, c *opcua.Client) error {
				if comment != "" {
					err = m.comment(c)(ctx, conditionID, comment)
				} else {
					err = m.plain(c)(ctx, conditionID)
				}
				if err != nil {
					return alarmFailed(m.action, conditionID, err)
				}
				return writeAlarmAction(model.AlarmAction{ConditionID: conditionID.String(), Action: m.action, Comment: comment, Status: ua.StatusOK.Symbol()})
			})
		},
	}
	if m.comment != nil {
		commentFlag(cmd, &comment, "Comment (the *2 variant of the method)")
	}
	return cmd
}

// --- respond ---

// responseOptions reads the ResponseOptionSet of a dialog.
func responseOptions(ctx context.Context, c *opcua.Client, conditionID *ua.NodeID) ([]string, error) {
	n, err := c.Node(conditionID).TranslateBrowsePathInNamespaceToNodeID(ctx, 0, "ResponseOptionSet")
	if err != nil {
		return nil, err
	}
	v, err := c.Node(n).Value(ctx)
	if err != nil {
		return nil, err
	}
	var out []string
	switch x := v.Value().(type) {
	case []*ua.LocalizedText:
		for _, lt := range x {
			if lt == nil {
				out = append(out, "")
				continue
			}
			out = append(out, lt.Text)
		}
	case []string:
		out = x
	default:
		return nil, fmt.Errorf("ResponseOptionSet is %T, want LocalizedText[]", x)
	}
	return out, nil
}

// selectResponse finds the response option a text names (its index, or
// its text in any case).
func selectResponse(arg string, options []string) (int32, error) {
	if i, err := strconv.ParseInt(arg, 10, 32); err == nil {
		return int32(i), nil
	}
	for i, o := range options {
		if strings.EqualFold(strings.TrimSpace(arg), strings.TrimSpace(o)) {
			return int32(i), nil
		}
	}
	return 0, fmt.Errorf("%q is not a response option of the dialog (%s)", arg, describeOptions(options))
}

func describeOptions(options []string) string {
	if len(options) == 0 {
		return "it has none"
	}
	parts := make([]string, len(options))
	for i, o := range options {
		parts[i] = fmt.Sprintf("%d=%s", i, o)
	}
	return strings.Join(parts, ", ")
}

func newAlarmsRespondCmd() *cobra.Command {
	var comment string
	cmd := &cobra.Command{
		Use:   "respond <conditionId> <option>",
		Short: "Respond to a dialog",
		Long: `Respond to an active dialog (DialogConditionType, Part 9 §5.6): the server
asks the operator a question (the Prompt, as alarms list --format json and
alarms watch show it) and offers a set of response options.

<option> is the index of the response in the ResponseOptionSet of the
dialog, or its text (case does not matter): opcuactl reads the
ResponseOptionSet to find it. With -m (--comment) opcuactl calls Respond2,
which stores the comment.

Examples:
  opcuactl alarms respond -e opc.tcp://localhost:4840 "ns=2;s=Line1.StartDialog" Yes
  opcuactl alarms respond -e opc.tcp://localhost:4840 "ns=2;s=Line1.StartDialog" 0 -m "checked the guard"`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			conditionID, err := conditionArg(args)
			if err != nil {
				return err
			}
			return withClient(func(ctx context.Context, c *opcua.Client) error {
				options, oerr := responseOptions(ctx, c, conditionID)
				sel, err := selectResponse(args[1], options)
				if err != nil {
					if oerr != nil {
						err = fmt.Errorf("%w; reading the ResponseOptionSet: %v", err, oerr)
					}
					format.Errf("respond %s: %v", conditionID, err)
					return exitCode(1, err)
				}
				if comment != "" {
					err = c.Respond2(ctx, conditionID, sel, comment)
				} else {
					err = c.Respond(ctx, conditionID, sel)
				}
				if err != nil {
					if errors.Is(err, ua.StatusBadDialogResponseInvalid) && len(options) > 0 {
						err = fmt.Errorf("%w (options: %s)", err, describeOptions(options))
					}
					return alarmFailed("respond", conditionID, err)
				}
				a := model.AlarmAction{ConditionID: conditionID.String(), Action: "respond", Comment: comment, SelectedResponse: &sel, Status: ua.StatusOK.Symbol()}
				if sel >= 0 && int(sel) < len(options) {
					a.Response = options[sel]
				}
				return writeAlarmAction(a)
			})
		},
	}
	commentFlag(cmd, &comment, "Comment (Respond2)")
	return cmd
}

// --- groups ---

func newAlarmsGroupsCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "groups <conditionId>",
		Aliases: []string{"memberships"},
		Short:   "List the alarm groups an alarm is a member of",
		Long: `List the alarm groups (AlarmGroupType, AlarmSuppressionGroupType) an alarm
is a member of: GetGroupMemberships (Part 9 1.05 §5.8.18). A suppression
group suppresses its alarms while one of its digital variables is true.

Examples:
  opcuactl alarms groups -e opc.tcp://localhost:4840 "ns=2;s=Tank.LevelAlarm"
  opcuactl alarms groups -e opc.tcp://localhost:4840 "ns=2;s=Tank.LevelAlarm" --format json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			conditionID, err := conditionArg(args)
			if err != nil {
				return err
			}
			return withClient(func(ctx context.Context, c *opcua.Client) error {
				ids, err := c.GetGroupMemberships(ctx, conditionID)
				if err != nil {
					return alarmFailed("GetGroupMemberships", conditionID, err)
				}
				groups := make([]model.AlarmGroup, 0, len(ids))
				for _, g := range ids {
					m := model.AlarmGroup{NodeID: g.String()}
					if bn, err := c.Node(g).BrowseName(ctx); err == nil && bn != nil {
						m.BrowseName = bn.Name
					}
					if dn, err := c.Node(g).DisplayName(ctx); err == nil && dn != nil {
						m.DisplayName = dn.Text
					}
					groups = append(groups, m)
				}
				if format.ParseFormat(outputFormat) != format.Table {
					return newWriter().Write(groups)
				}
				if quiet {
					for _, g := range groups {
						fmt.Println(g.NodeID)
					}
					return nil
				}
				rows := make([][]string, len(groups))
				for i, g := range groups {
					rows[i] = []string{g.NodeID, g.BrowseName, g.DisplayName}
				}
				return newWriter().WriteTable([]string{"GROUP ID", "BROWSE NAME", "DISPLAY NAME"}, rows)
			})
		},
	}
}

// --- metrics ---

func newAlarmsMetricsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "metrics",
		Short: "Read or reset alarm metrics (AlarmMetricsType)",
		Long: `Read or reset an AlarmMetrics object (AlarmMetricsType, Part 9 1.05 §8):
the number of alarms since the start time, the longest active and
unacknowledged times, the most re-alarms, and the alarm rates (alarms per
Rate minutes). A server has one for all of its alarms and may have one per
alarm group; browse for objects of type AlarmMetricsType to find them.

Examples:
  opcuactl alarms metrics get -e opc.tcp://localhost:4840 "ns=2;s=AlarmMetrics"
  opcuactl alarms metrics reset -e opc.tcp://localhost:4840 "ns=2;s=AlarmMetrics"`,
	}
	get := &cobra.Command{
		Use:     "get <metricsNodeId>",
		Aliases: []string{"read"},
		Short:   "Read an AlarmMetrics object",
		Long: `Read the components of an AlarmMetrics object (AlarmMetricsType). The
durations are in seconds in --format json. Components the object does not
have read as 0.

Examples:
  opcuactl alarms metrics get -e opc.tcp://localhost:4840 "ns=2;s=AlarmMetrics"
  opcuactl alarms metrics get -e opc.tcp://localhost:4840 "ns=2;s=AlarmMetrics" --format json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := parse.NodeID(args[0])
			if err != nil {
				return exitCodef(1, "invalid NodeId: %v", err)
			}
			return withClient(func(ctx context.Context, c *opcua.Client) error {
				m, err := c.ReadAlarmMetrics(ctx, n)
				if err != nil {
					format.Errf("reading the alarm metrics of %s: %v", n, err)
					return exitCode(3, err)
				}
				out := alarmMetricsModel(n, m)
				if format.ParseFormat(outputFormat) != format.Table {
					return newWriter().Write(out)
				}
				if quiet {
					fmt.Println(out.AlarmCount)
					return nil
				}
				rate := fmt.Sprintf("alarms per %d min", out.RateMinutes)
				writeKV("Alarm metrics "+out.NodeID, [][2]string{
					{"Alarm count", fmt.Sprint(out.AlarmCount)},
					{"Start time", out.StartTime},
					{"Max active time", m.MaximumActiveState.String()},
					{"Max unacknowledged time", m.MaximumUnAck.String()},
					{"Max re-alarm count", fmt.Sprint(out.MaximumReAlarmCount)},
					{"Current alarm rate", fmt.Sprintf("%g %s", out.CurrentAlarmRate, rate)},
					{"Maximum alarm rate", fmt.Sprintf("%g %s", out.MaximumAlarmRate, rate)},
					{"Average alarm rate", fmt.Sprintf("%g %s", out.AverageAlarmRate, rate)},
				})
				return nil
			})
		},
	}
	reset := &cobra.Command{
		Use:   "reset <metricsNodeId>",
		Short: "Reset an AlarmMetrics object",
		Long: `Reset the counters of an AlarmMetrics object (its Reset method): the
alarm count, the maxima and the rates start again from now.

Examples:
  opcuactl alarms metrics reset -e opc.tcp://localhost:4840 "ns=2;s=AlarmMetrics"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := parse.NodeID(args[0])
			if err != nil {
				return exitCodef(1, "invalid NodeId: %v", err)
			}
			return withClient(func(ctx context.Context, c *opcua.Client) error {
				if err := c.ResetAlarmMetrics(ctx, n); err != nil {
					return alarmFailed("reset the metrics of", n, err)
				}
				return writeAlarmAction(model.AlarmAction{ConditionID: n.String(), Action: "reset-metrics", Status: ua.StatusOK.Symbol()})
			})
		},
	}
	cmd.AddCommand(get, reset)
	return cmd
}

func alarmMetricsModel(n *ua.NodeID, m *opcua.AlarmMetrics) model.AlarmMetrics {
	out := model.AlarmMetrics{
		NodeID:              n.String(),
		AlarmCount:          m.AlarmCount,
		MaximumActiveStateS: m.MaximumActiveState.Seconds(),
		MaximumUnAckS:       m.MaximumUnAck.Seconds(),
		MaximumReAlarmCount: m.MaximumReAlarmCount,
		CurrentAlarmRate:    m.CurrentAlarmRate,
		MaximumAlarmRate:    m.MaximumAlarmRate,
		AverageAlarmRate:    m.AverageAlarmRate,
		RateMinutes:         m.RateMinutes,
	}
	if !m.StartTime.IsZero() {
		out.StartTime = model.FormatTimestamp(m.StartTime)
	}
	return out
}

// --- audit ---

// methodName names the condition methods of namespace 0 (Acknowledge,
// TimedShelve2), other methods by their NodeId.
func methodName(n *ua.NodeID) string {
	if n == nil {
		return ""
	}
	if isNS0Numeric(n) {
		name := id.Name(n.IntID())
		if name != strconv.FormatUint(uint64(n.IntID()), 10) {
			if i := strings.LastIndex(name, "_"); i >= 0 {
				return name[i+1:]
			}
			return name
		}
	}
	return n.String()
}

// auditModel converts an audit event for output.
func auditModel(ev *opcua.AuditConditionEvent, now time.Time) *model.AuditEvent {
	m := &model.AuditEvent{
		Timestamp:          model.FormatTimestamp(now),
		SourceName:         ev.SourceName,
		Status:             ev.Status,
		Method:             methodName(ev.MethodID),
		ClientUserID:       ev.ClientUserID,
		ClientAuditEntryID: ev.ClientAuditEntryID,
		ServerID:           ev.ServerID,
		SelectedResponse:   ev.SelectedResponse,
		Message:            ev.Message,
		EventID:            base64.StdEncoding.EncodeToString(ev.EventID),
	}
	if ev.EventType != nil {
		m.EventType = eventTypeName(ev.EventType)
	}
	if ev.SourceNode != nil {
		m.ConditionID = ev.SourceNode.String()
	}
	if !ev.Time.IsZero() {
		m.Time = model.FormatTimestamp(ev.Time)
	}
	if !ev.ActionTimeStamp.IsZero() {
		m.ActionTime = model.FormatTimestamp(ev.ActionTimeStamp)
	}
	if len(ev.ConditionEventID) > 0 {
		m.ConditionEventID = base64.StdEncoding.EncodeToString(ev.ConditionEventID)
	}
	if ev.Comment != nil {
		m.Comment = ev.Comment.Text
	}
	if ev.ShelvingTime != nil {
		ms := ev.ShelvingTime.Milliseconds()
		m.ShelvingTimeMs = &ms
	}
	return m
}

func printAuditEvent(m *model.AuditEvent, useJSON bool) {
	if useJSON {
		data, _ := json.Marshal(m)
		fmt.Println(string(data))
		return
	}
	status := "ok"
	if !m.Status {
		status = "refused"
	}
	extra := ""
	if m.Comment != "" {
		extra += fmt.Sprintf(" comment=%q", m.Comment)
	}
	if m.SelectedResponse != nil {
		extra += fmt.Sprintf(" response=%d", *m.SelectedResponse)
	}
	if m.ShelvingTimeMs != nil {
		extra += fmt.Sprintf(" shelvingTime=%s", time.Duration(*m.ShelvingTimeMs)*time.Millisecond)
	}
	if m.ClientAuditEntryID != "" {
		extra += " auditEntryId=" + m.ClientAuditEntryID
	}
	fmt.Printf("%s  %s  %s  %s %s user=%q%s  %q\n", m.Timestamp, m.ConditionID, m.EventType, m.Method, status, m.ClientUserID, extra, m.Message)
}

// auditEvents returns the audit events of a notification.
func auditEvents(notif *opcua.PublishNotificationData) ([]*opcua.AuditConditionEvent, error) {
	if notif.Error != nil {
		return nil, notif.Error
	}
	enl, ok := notif.Value.(*ua.EventNotificationList)
	if !ok {
		return nil, nil
	}
	out := make([]*opcua.AuditConditionEvent, 0, len(enl.Events))
	for _, f := range enl.Events {
		ev, err := opcua.ParseAuditConditionEvent(f)
		if err != nil {
			return out, err
		}
		out = append(out, ev)
	}
	return out, nil
}

func newAlarmsAuditCmd() *cobra.Command {
	var (
		nf       notifierFlags
		duration time.Duration
		count    int
		jsonl    bool
		entryID  string
	)
	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Stream the audit events of the condition methods",
		Long: `Stream the audit events of the condition methods (AuditConditionEventType
and its subtypes, Part 9 §5.10): who acknowledged, confirmed, commented,
shelved, suppressed, silenced, reset, enabled or answered which condition,
when, with which comment, and whether the server accepted the call.

The server must audit (Server/Auditing true) and let the user receive audit
events. Each line shows the ConditionId, the event type, the method, ok or
refused, the user, and the comment, response, shelving time and
ClientAuditEntryId when there are any. --entry-id shows only the events of
requests with that AuditEntryId (as alarms --audit-entry-id sets it).

With --format json (or --jsonl) every line is a JSON object. Stop with
Ctrl+C, --duration or --count.

Examples:
  opcuactl alarms audit -e opc.tcp://localhost:4840 --auth username --username auditor --password ...
  opcuactl alarms audit -e opc.tcp://localhost:4840 --jsonl --duration 1h
  opcuactl alarms audit -e opc.tcp://localhost:4840 --entry-id ticket-4711`,
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
			sub, ch, err := eventSubscription(opCtx, c, &nf, opcua.NewAuditConditionEventFilter())
			cancel()
			if err != nil {
				format.Errf("%v", err)
				return err
			}
			defer func() { _ = sub.Cancel(context.WithoutCancel(ctx)) }()
			if verbose {
				format.Errf("subscription %d started, watching audit events on %s", sub.SubscriptionID, nf.node)
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
					evs, err := auditEvents(notif)
					if err != nil {
						format.Errf("audit event: %v", err)
					}
					for _, ev := range evs {
						if entryID != "" && ev.ClientAuditEntryID != entryID {
							continue
						}
						printAuditEvent(auditModel(ev, time.Now()), useJSON)
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
	cmd.Flags().DurationVar(&duration, "duration", 0, "Stop after this duration (0 = run until Ctrl+C)")
	cmd.Flags().IntVar(&count, "count", 0, "Stop after this many audit events (0 = unlimited)")
	cmd.Flags().BoolVar(&jsonl, "jsonl", false, "Output newline-delimited JSON (JSONL)")
	cmd.Flags().StringVar(&entryID, "entry-id", "", "Only the events of requests with this AuditEntryId (ClientAuditEntryId)")
	return cmd
}
