package opcuactl

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/otfabric/go-opcua/id"
	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/opcuactl/internal/format"
	"github.com/otfabric/opcuactl/internal/model"
	"github.com/otfabric/opcuactl/internal/parse"
	"github.com/spf13/cobra"
)

func newMonitorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "monitor",
		Short: "Subscribe to live data changes and events",
		Long: `Monitor real-time value changes and event notifications from a server.

Examples:
  opcuactl monitor value --endpoint opc.tcp://localhost:4840 --node i=2258
  opcuactl monitor event --endpoint opc.tcp://localhost:4840 --node i=2253`,
	}
	cmd.AddCommand(newMonitorValueCmd(), newMonitorEventCmd(), newMonitorAlarmCmd())
	return cmd
}

func newMonitorValueCmd() *cobra.Command {
	var (
		nodes      []string
		interval   time.Duration
		sampling   time.Duration
		queueSize  uint32
		timestamps string
		duration   time.Duration
		count      int
		jsonl      bool
	)

	cmd := &cobra.Command{
		Use:   "value",
		Short: "Subscribe to live value changes",
		Long: `Subscribe to data-change notifications on one or more nodes.

Output streams line-by-line to stdout. Use --jsonl for newline-delimited JSON.
Cancel with Ctrl+C. Optionally limit by --duration or --count.

Examples:
  opcuactl monitor value -e opc.tcp://localhost:4840 -n i=2258
  opcuactl monitor value -e opc.tcp://localhost:4840 -n i=2258 -n "ns=2;s=Temp" --interval 500ms
  opcuactl monitor value -e opc.tcp://localhost:4840 -n i=2258 --jsonl --duration 30s
  opcuactl monitor value -e opc.tcp://localhost:4840 -n i=2258 --count 10 --format json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(nodes) == 0 {
				return exitCodef(1, "at least one --node is required")
			}

			nodeIDs, err := parse.NodeIDs(nodes)
			if err != nil {
				return exitCode(1, err)
			}

			ts := ua.TimestampsToReturnBoth
			if timestamps != "" {
				ts = ua.TimestampsToReturnFromString(timestamps)
			}

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			c, err := connCfg.NewClient(ctx)
			if err != nil {
				format.Errf("%v", err)
				return exitCode(2, err)
			}
			defer func() { _ = c.Close(ctx) }()

			// Build subscription
			builder := c.NewSubscription().
				Interval(interval).
				Timestamps(ts).
				SamplingInterval(sampling).
				Monitor(nodeIDs...)

			sub, notifyCh, err := builder.Start(ctx)
			if err != nil {
				format.Errf("starting subscription: %v", err)
				return exitCode(3, err)
			}
			defer func() { _ = sub.Cancel(ctx) }()

			if sub.RevisedPublishingInterval != interval {
				format.Errf("server revised publishing interval: %s → %s", interval, sub.RevisedPublishingInterval)
			}
			if verbose {
				format.Errf("subscription %d started, monitoring %d node(s), interval=%s", sub.SubscriptionID, len(nodeIDs), sub.RevisedPublishingInterval)
				if sampling > 0 {
					format.Errf("  sampling interval: %s, queue size: %d", sampling, queueSize)
				}
			}

			// Build handle → nodeID map
			handleMap := make(map[uint32]string)
			for i, nid := range nodeIDs {
				handleMap[uint32(i)] = nid.String()
			}

			// Set up optional deadline
			var deadline <-chan time.Time
			if duration > 0 {
				timer := time.NewTimer(duration)
				defer timer.Stop()
				deadline = timer.C
			}

			received := 0
			useJSON := jsonl || format.ParseFormat(outputFormat) != format.Table

			for {
				select {
				case <-ctx.Done():
					return nil
				case <-deadline:
					return nil
				case notif, ok := <-notifyCh:
					if !ok {
						return nil
					}
					if notif.Error != nil {
						format.Errf("notification error: %v", notif.Error)
						continue
					}

					dcn, ok := notif.Value.(*ua.DataChangeNotification)
					if !ok {
						continue
					}

					for _, item := range dcn.MonitoredItems {
						nodeStr := handleMap[item.ClientHandle]
						if nodeStr == "" {
							nodeStr = fmt.Sprintf("handle:%d", item.ClientHandle)
						}

						mv := model.MonitoredValue{
							Timestamp:       model.FormatTimestamp(time.Now()),
							NodeID:          nodeStr,
							Status:          item.Value.Status.Symbol(),
							StatusCode:      item.Value.Status.Uint32(),
							SourceTimestamp: model.FormatTimestamp(item.Value.SourceTimestamp),
							ServerTimestamp: model.FormatTimestamp(item.Value.ServerTimestamp),
						}

						if item.Value.Value != nil {
							mv.Value = item.Value.Value.Value()
							mv.Type = item.Value.Value.Type().String()
						}

						if useJSON {
							data, _ := json.Marshal(mv)
							fmt.Println(string(data))
						} else {
							ts := mv.SourceTimestamp
							if ts == "" {
								ts = mv.Timestamp
							}
							fmt.Printf("%s  %s  %v  [%s]\n", ts, mv.NodeID, mv.Value, mv.Status)
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

	cmd.Flags().StringArrayVarP(&nodes, "node", "n", nil, "Node ID to monitor (repeatable)")
	cmd.Flags().DurationVar(&interval, "interval", 100*time.Millisecond, "Publishing interval")
	cmd.Flags().DurationVar(&sampling, "sampling", 0, "Sampling interval per monitored item (0 = use publishing interval)")
	cmd.Flags().Uint32Var(&queueSize, "queue-size", 10, "Monitored item queue size")
	cmd.Flags().StringVar(&timestamps, "timestamps", "both", "Timestamps to return (source|server|both|neither)")
	cmd.Flags().DurationVar(&duration, "duration", 0, "Stop after this duration (0 = run until Ctrl+C)")
	cmd.Flags().IntVar(&count, "count", 0, "Stop after this many notifications (0 = unlimited)")
	cmd.Flags().BoolVar(&jsonl, "jsonl", false, "Output newline-delimited JSON (JSONL)")
	_ = cmd.RegisterFlagCompletionFunc("timestamps", fixedCompletion(timestampValues))
	return cmd
}

func newMonitorEventCmd() *cobra.Command {
	var (
		nodeStr   string
		selectStr []string
		duration  time.Duration
		count     int
		jsonl     bool
		interval  time.Duration
	)

	defaultFields := []string{"EventType", "SourceName", "Message", "Severity", "Time"}

	cmd := &cobra.Command{
		Use:   "event",
		Short: "Subscribe to event notifications",
		Long: `Subscribe to event notifications on a node (typically the Server object or an event notifier).

Select fields are the OPC UA event field names to return. Defaults to:
EventType, SourceName, Message, Severity, Time.

Examples:
  opcuactl monitor event -e opc.tcp://localhost:4840 --node i=2253
  opcuactl monitor event -e opc.tcp://localhost:4840 --node i=2253 --select EventType --select Message --select Severity
  opcuactl monitor event -e opc.tcp://localhost:4840 --node i=2253 --jsonl --duration 60s`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if nodeStr == "" {
				return exitCodef(1, "--node is required")
			}

			nodeID, err := parse.NodeID(nodeStr)
			if err != nil {
				return exitCode(1, err)
			}

			fields := defaultFields
			if len(selectStr) > 0 {
				fields = selectStr
			}

			// Build event filter
			selects := make([]*ua.SimpleAttributeOperand, len(fields))
			for i, name := range fields {
				selects[i] = &ua.SimpleAttributeOperand{
					TypeDefinitionID: ua.NewNumericNodeID(0, id.BaseEventType),
					BrowsePath:       []*ua.QualifiedName{{NamespaceIndex: 0, Name: name}},
					AttributeID:      ua.AttributeIDValue,
				}
			}

			filter := &ua.EventFilter{
				SelectClauses: selects,
			}

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			c, err := connCfg.NewClient(ctx)
			if err != nil {
				format.Errf("%v", err)
				return exitCode(2, err)
			}
			defer func() { _ = c.Close(ctx) }()

			sub, notifyCh, err := c.NewSubscription().
				Interval(interval).
				MonitorEvents(filter, nodeID).
				Start(ctx)
			if err != nil {
				format.Errf("starting event subscription: %v", err)
				return exitCode(3, err)
			}
			defer func() { _ = sub.Cancel(ctx) }()

			if sub.RevisedPublishingInterval != interval {
				format.Errf("server revised publishing interval: %s → %s", interval, sub.RevisedPublishingInterval)
			}
			if verbose {
				format.Errf("subscription %d started, monitoring events on %s (fields: %s)",
					sub.SubscriptionID, nodeID, strings.Join(fields, ", "))
			}

			var deadline <-chan time.Time
			if duration > 0 {
				timer := time.NewTimer(duration)
				defer timer.Stop()
				deadline = timer.C
			}

			received := 0
			useJSON := jsonl || format.ParseFormat(outputFormat) != format.Table

			for {
				select {
				case <-ctx.Done():
					return nil
				case <-deadline:
					return nil
				case notif, ok := <-notifyCh:
					if !ok {
						return nil
					}
					if notif.Error != nil {
						format.Errf("notification error: %v", notif.Error)
						continue
					}

					evtList, ok := notif.Value.(*ua.EventNotificationList)
					if !ok {
						continue
					}

					for _, evt := range evtList.Events {
						me := model.MonitoredEvent{
							Timestamp: model.FormatTimestamp(time.Now()),
							NodeID:    nodeID.String(),
							Fields:    make(map[string]interface{}),
						}

						for i, field := range evt.EventFields {
							name := "unknown"
							if i < len(fields) {
								name = fields[i]
							}
							if field != nil {
								me.Fields[name] = field.Value()
							}
						}

						if useJSON {
							data, _ := json.Marshal(me)
							fmt.Println(string(data))
						} else {
							var parts []string
							for _, name := range fields {
								if v, ok := me.Fields[name]; ok {
									parts = append(parts, fmt.Sprintf("%s=%v", name, v))
								}
							}
							fmt.Printf("%s  %s  %s\n", me.Timestamp, me.NodeID, strings.Join(parts, "  "))
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

	cmd.Flags().StringVarP(&nodeStr, "node", "n", "", "Event notifier node ID (required)")
	cmd.Flags().StringArrayVar(&selectStr, "select", nil, "Event field to select (repeatable; default: EventType,SourceName,Message,Severity,Time)")
	cmd.Flags().DurationVar(&interval, "interval", 100*time.Millisecond, "Publishing interval")
	cmd.Flags().DurationVar(&duration, "duration", 0, "Stop after this duration (0 = run until Ctrl+C)")
	cmd.Flags().IntVar(&count, "count", 0, "Stop after this many events (0 = unlimited)")
	cmd.Flags().BoolVar(&jsonl, "jsonl", false, "Output newline-delimited JSON (JSONL)")
	return cmd
}

func newMonitorAlarmCmd() *cobra.Command {
	var (
		nodeStr   string
		selectStr []string
		duration  time.Duration
		count     int
		jsonl     bool
		interval  time.Duration
	)

	defaultFields := []string{
		"EventType", "SourceName", "Message", "Severity", "Time",
		"ActiveState", "AckedState", "ConfirmedState", "Retain",
	}

	cmd := &cobra.Command{
		Use:   "alarm",
		Short: "Subscribe to alarm and condition notifications",
		Long: `Subscribe to alarm/condition event notifications on a node.

This is a specialization of event monitoring with alarm-relevant default
fields: EventType, SourceName, Message, Severity, Time, ActiveState,
AckedState, ConfirmedState, Retain.

Fields that are not resolvable on a given server will appear as null values.

Examples:
  opcuactl monitor alarm -e opc.tcp://localhost:4840 --node i=2253
  opcuactl monitor alarm -e opc.tcp://localhost:4840 --node i=2253 --jsonl
  opcuactl monitor alarm -e opc.tcp://localhost:4840 --node i=2253 --select EventType --select Message --select ActiveState`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if nodeStr == "" {
				return exitCodef(1, "--node is required")
			}

			nodeID, err := parse.NodeID(nodeStr)
			if err != nil {
				return exitCode(1, err)
			}

			fields := defaultFields
			if len(selectStr) > 0 {
				fields = selectStr
			}

			// Build event filter with alarm fields
			selects := make([]*ua.SimpleAttributeOperand, len(fields))
			for i, name := range fields {
				selects[i] = &ua.SimpleAttributeOperand{
					TypeDefinitionID: ua.NewNumericNodeID(0, id.BaseEventType),
					BrowsePath:       []*ua.QualifiedName{{NamespaceIndex: 0, Name: name}},
					AttributeID:      ua.AttributeIDValue,
				}
			}

			filter := &ua.EventFilter{
				SelectClauses: selects,
			}

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			c, err := connCfg.NewClient(ctx)
			if err != nil {
				format.Errf("%v", err)
				return exitCode(2, err)
			}
			defer func() { _ = c.Close(ctx) }()

			sub, notifyCh, err := c.NewSubscription().
				Interval(interval).
				MonitorEvents(filter, nodeID).
				Start(ctx)
			if err != nil {
				format.Errf("starting alarm subscription: %v", err)
				return exitCode(3, err)
			}
			defer func() { _ = sub.Cancel(ctx) }()

			if sub.RevisedPublishingInterval != interval {
				format.Errf("server revised publishing interval: %s → %s", interval, sub.RevisedPublishingInterval)
			}
			if verbose {
				format.Errf("subscription %d started, monitoring alarms on %s (fields: %s)",
					sub.SubscriptionID, nodeID, strings.Join(fields, ", "))
			}

			var deadline <-chan time.Time
			if duration > 0 {
				timer := time.NewTimer(duration)
				defer timer.Stop()
				deadline = timer.C
			}

			received := 0
			useJSON := jsonl || format.ParseFormat(outputFormat) != format.Table

			for {
				select {
				case <-ctx.Done():
					return nil
				case <-deadline:
					return nil
				case notif, ok := <-notifyCh:
					if !ok {
						return nil
					}
					if notif.Error != nil {
						format.Errf("notification error: %v", notif.Error)
						continue
					}

					evtList, ok := notif.Value.(*ua.EventNotificationList)
					if !ok {
						continue
					}

					for _, evt := range evtList.Events {
						me := model.MonitoredEvent{
							Timestamp: model.FormatTimestamp(time.Now()),
							NodeID:    nodeID.String(),
							Fields:    make(map[string]interface{}),
						}

						for i, field := range evt.EventFields {
							name := "unknown"
							if i < len(fields) {
								name = fields[i]
							}
							if field != nil {
								me.Fields[name] = field.Value()
							}
						}

						if useJSON {
							data, _ := json.Marshal(me)
							fmt.Println(string(data))
						} else {
							var parts []string
							for _, name := range fields {
								if v, ok := me.Fields[name]; ok {
									parts = append(parts, fmt.Sprintf("%s=%v", name, v))
								}
							}
							fmt.Printf("%s  %s  %s\n", me.Timestamp, me.NodeID, strings.Join(parts, "  "))
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

	cmd.Flags().StringVarP(&nodeStr, "node", "n", "", "Event notifier node ID (required)")
	cmd.Flags().StringArrayVar(&selectStr, "select", nil, "Event field to select (repeatable; default: alarm-related fields)")
	cmd.Flags().DurationVar(&interval, "interval", 100*time.Millisecond, "Publishing interval")
	cmd.Flags().DurationVar(&duration, "duration", 0, "Stop after this duration (0 = run until Ctrl+C)")
	cmd.Flags().IntVar(&count, "count", 0, "Stop after this many events (0 = unlimited)")
	cmd.Flags().BoolVar(&jsonl, "jsonl", false, "Output newline-delimited JSON (JSONL)")
	return cmd
}
