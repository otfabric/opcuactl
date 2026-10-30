// SPDX-License-Identifier: MIT

package opcuactl

import (
	"context"
	"crypto/sha1" //nolint:gosec // the OPC UA certificate thumbprint is SHA-1 (Part 6 §6.2.3)
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/otfabric/go-opcua"
	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/opcuactl/internal/format"
	"github.com/otfabric/opcuactl/internal/model"
	"github.com/otfabric/opcuactl/internal/parse"
	"github.com/spf13/cobra"
)

func newDiagnosticsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "diagnostics",
		Aliases: []string{"diag"},
		Short:   "Read the server diagnostics (sessions, subscriptions, counters)",
		Long: `Read the diagnostics a server exposes below Server.ServerDiagnostics
(OPC UA Part 5 §6.3.3): the summary counters, the sessions, the
subscriptions and the sampling intervals in use. diagnostics enable and
disable switch the collection on and off (EnabledFlag).

Who may read what is up to the server. Usually everyone reads the summary,
the session and subscription arrays and EnabledFlag; the details of another
session, the session security diagnostics and changing EnabledFlag need an
administrator (the SecurityAdmin or ConfigureAdmin role), the security
diagnostics also an encrypted channel.

For the DiagnosticInfos of a failed request (as opposed to the server's
diagnostics) see the global --return-diagnostics flag.

Examples:
  opcuactl diagnostics summary -e opc.tcp://localhost:4840
  opcuactl diagnostics sessions -e opc.tcp://localhost:4840
  opcuactl diagnostics sessions -e opc.tcp://localhost:4840 --session self
  opcuactl diagnostics subscriptions -e opc.tcp://localhost:4840 --format json
  opcuactl diagnostics enable -e opc.tcp://localhost:4840 --auth username --username admin --password ...`,
	}
	cmd.AddCommand(
		newDiagnosticsSummaryCmd(),
		newDiagnosticsSessionsCmd(),
		newDiagnosticsSubscriptionsCmd(),
		newDiagnosticsSamplingCmd(),
		newDiagnosticsEnableCmd(true),
		newDiagnosticsEnableCmd(false),
	)
	return cmd
}

// diagnosticsHint explains why a server refuses diagnostics.
func diagnosticsHint(err error, what string) string {
	switch {
	case errors.Is(err, ua.StatusBadUserAccessDenied):
		return what + " needs an administrator (the SecurityAdmin or ConfigureAdmin role on most servers): connect with an administrator identity (--auth username|certificate|issued)"
	case errors.Is(err, ua.StatusBadSecurityModeInsufficient):
		return what + " needs an encrypted channel: --security-mode SignAndEncrypt"
	case errors.Is(err, ua.StatusBadNodeIDUnknown), errors.Is(err, ua.StatusBadNoMatch):
		return "the server does not expose " + what
	case errors.Is(err, ua.StatusBadNotWritable):
		return "the server does not let clients change EnabledFlag"
	}
	return ""
}

func diagnosticsFailed(what string, err error) error {
	msg := fmt.Sprintf("reading %s: %v", what, err)
	if h := diagnosticsHint(err, what); h != "" {
		msg += " (" + h + ")"
	}
	format.Errf("%s", msg)
	return exitCode(exitProtocol, err)
}

// warnIfDisabled tells why a diagnostics array is empty: a server with
// EnabledFlag false collects nothing.
func warnIfDisabled(ctx context.Context, c *opcua.Client, n int) {
	if n > 0 {
		return
	}
	if on, err := c.ServerDiagnosticsEnabled(ctx); err == nil && !on {
		format.Infof("the server diagnostics are disabled (EnabledFlag false): opcuactl diagnostics enable switches them on")
	}
}

// writeKV prints label/value pairs aligned, the table form of a single
// structure.
func writeKV(title string, pairs [][2]string) {
	if title != "" {
		fmt.Println(title)
	}
	width := 0
	for _, p := range pairs {
		width = max(width, len(p[0]))
	}
	for _, p := range pairs {
		fmt.Printf("  %-*s  %s\n", width+1, p[0]+":", p[1])
	}
}

func newDiagnosticsSummaryCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "summary",
		Short: "Show the ServerDiagnosticsSummary",
		Long: `Show the ServerDiagnosticsSummary: current and cumulated sessions and
subscriptions, rejected and timed out sessions, rejected requests, and
whether the server collects diagnostics (EnabledFlag).

Examples:
  opcuactl diagnostics summary -e opc.tcp://localhost:4840
  opcuactl diagnostics summary -e opc.tcp://localhost:4840 --format json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return withClient(func(ctx context.Context, c *opcua.Client) error {
				on, err := c.ServerDiagnosticsEnabled(ctx)
				if err != nil {
					return diagnosticsFailed("EnabledFlag", err)
				}
				s, err := c.ServerDiagnosticsSummary(ctx)
				if err != nil {
					return diagnosticsFailed("the ServerDiagnosticsSummary", err)
				}
				sum := diagnosticsSummary(on, s)
				if format.ParseFormat(outputFormat) != format.Table {
					return newWriter().Write(sum)
				}
				writeKV("Server diagnostics", [][2]string{
					{"Enabled", fmt.Sprintf("%v", sum.Enabled)},
					{"Current sessions", fmt.Sprint(sum.CurrentSessionCount)},
					{"Cumulated sessions", fmt.Sprint(sum.CumulatedSessionCount)},
					{"Rejected sessions", fmt.Sprint(sum.RejectedSessionCount)},
					{"Security rejected sessions", fmt.Sprint(sum.SecurityRejectedSessionCount)},
					{"Session timeouts", fmt.Sprint(sum.SessionTimeoutCount)},
					{"Session aborts", fmt.Sprint(sum.SessionAbortCount)},
					{"Current subscriptions", fmt.Sprint(sum.CurrentSubscriptionCount)},
					{"Cumulated subscriptions", fmt.Sprint(sum.CumulatedSubscriptionCount)},
					{"Publishing intervals", fmt.Sprint(sum.PublishingIntervalCount)},
					{"Rejected requests", fmt.Sprint(sum.RejectedRequestsCount)},
					{"Security rejected requests", fmt.Sprint(sum.SecurityRejectedRequestsCount)},
					{"Server views", fmt.Sprint(sum.ServerViewCount)},
				})
				return nil
			})
		},
	}
}

func diagnosticsSummary(on bool, s *ua.ServerDiagnosticsSummaryDataType) model.DiagnosticsSummary {
	return model.DiagnosticsSummary{
		Enabled:                       on,
		ServerViewCount:               s.ServerViewCount,
		CurrentSessionCount:           s.CurrentSessionCount,
		CumulatedSessionCount:         s.CumulatedSessionCount,
		SecurityRejectedSessionCount:  s.SecurityRejectedSessionCount,
		RejectedSessionCount:          s.RejectedSessionCount,
		SessionTimeoutCount:           s.SessionTimeoutCount,
		SessionAbortCount:             s.SessionAbortCount,
		CurrentSubscriptionCount:      s.CurrentSubscriptionCount,
		CumulatedSubscriptionCount:    s.CumulatedSubscriptionCount,
		PublishingIntervalCount:       s.PublishingIntervalCount,
		SecurityRejectedRequestsCount: s.SecurityRejectedRequestsCount,
		RejectedRequestsCount:         s.RejectedRequestsCount,
	}
}

// sessionArg resolves --session: "self" is the session of opcuactl, ""
// all sessions, anything else a SessionId.
func sessionArg(c *opcua.Client, s string) (*ua.NodeID, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "":
		return nil, nil
	case "self", "own", "me":
		if sess := c.Session(); sess != nil {
			return sess.SessionID(), nil
		}
		return nil, exitCodef(exitProtocol, "no session")
	}
	n, err := parse.NodeID(s)
	if err != nil {
		return nil, exitCodef(exitUsage, "invalid --session: %v", err)
	}
	return n, nil
}

func validateSessionFlag(s string) error {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "self", "own", "me":
		return nil
	}
	if _, err := parse.NodeID(s); err != nil {
		return exitCodef(exitUsage, "invalid --session: %v", err)
	}
	return nil
}

func serviceCounter(sc *ua.ServiceCounterDataType) model.ServiceCounter {
	if sc == nil {
		return model.ServiceCounter{}
	}
	return model.ServiceCounter{Total: sc.TotalCount, Errors: sc.ErrorCount}
}

func sessionDiagnostics(d *ua.SessionDiagnosticsDataType) model.SessionDiagnostics {
	m := model.SessionDiagnostics{
		SessionID:                     d.SessionID.String(),
		SessionName:                   d.SessionName,
		EndpointURL:                   d.EndpointURL,
		LocaleIDs:                     d.LocaleIDs,
		ActualSessionTimeoutMs:        d.ActualSessionTimeout,
		MaxResponseMessageSize:        d.MaxResponseMessageSize,
		ClientConnectionTime:          model.FormatTimestamp(d.ClientConnectionTime),
		ClientLastContactTime:         model.FormatTimestamp(d.ClientLastContactTime),
		CurrentSubscriptionsCount:     d.CurrentSubscriptionsCount,
		CurrentMonitoredItemsCount:    d.CurrentMonitoredItemsCount,
		CurrentPublishRequestsInQueue: d.CurrentPublishRequestsInQueue,
		TotalRequestCount:             serviceCounter(d.TotalRequestCount),
		UnauthorizedRequestCount:      d.UnauthorizedRequestCount,
	}
	if cd := d.ClientDescription; cd != nil {
		m.ClientURI = cd.ApplicationURI
		if cd.ApplicationName != nil {
			m.ClientName = cd.ApplicationName.Text
		}
	}
	for name, sc := range map[string]*ua.ServiceCounterDataType{
		"Read": d.ReadCount, "HistoryRead": d.HistoryReadCount, "Write": d.WriteCount,
		"HistoryUpdate": d.HistoryUpdateCount, "Call": d.CallCount,
		"CreateMonitoredItems": d.CreateMonitoredItemsCount, "ModifyMonitoredItems": d.ModifyMonitoredItemsCount,
		"SetMonitoringMode": d.SetMonitoringModeCount, "SetTriggering": d.SetTriggeringCount,
		"DeleteMonitoredItems": d.DeleteMonitoredItemsCount, "CreateSubscription": d.CreateSubscriptionCount,
		"ModifySubscription": d.ModifySubscriptionCount, "SetPublishingMode": d.SetPublishingModeCount,
		"Publish": d.PublishCount, "Republish": d.RepublishCount, "TransferSubscriptions": d.TransferSubscriptionsCount,
		"DeleteSubscriptions": d.DeleteSubscriptionsCount, "AddNodes": d.AddNodesCount, "AddReferences": d.AddReferencesCount,
		"DeleteNodes": d.DeleteNodesCount, "DeleteReferences": d.DeleteReferencesCount, "Browse": d.BrowseCount,
		"BrowseNext": d.BrowseNextCount, "TranslateBrowsePathsToNodeIds": d.TranslateBrowsePathsToNodeIDsCount,
		"QueryFirst": d.QueryFirstCount, "QueryNext": d.QueryNextCount, "RegisterNodes": d.RegisterNodesCount,
		"UnregisterNodes": d.UnregisterNodesCount,
	} {
		if c := serviceCounter(sc); c.Total != 0 || c.Errors != 0 {
			if m.Services == nil {
				m.Services = map[string]model.ServiceCounter{}
			}
			m.Services[name] = c
		}
	}
	return m
}

func sessionSecurityDiagnostics(d *ua.SessionSecurityDiagnosticsDataType) model.SessionSecurityDiagnostics {
	m := model.SessionSecurityDiagnostics{
		SessionID:               d.SessionID.String(),
		ClientUserID:            d.ClientUserIDOfSession,
		ClientUserIDHistory:     d.ClientUserIDHistory,
		AuthenticationMechanism: d.AuthenticationMechanism,
		Encoding:                d.Encoding,
		TransportProtocol:       d.TransportProtocol,
		SecurityMode:            d.SecurityMode.String(),
		SecurityPolicy:          parse.SecurityPolicyShortName(d.SecurityPolicyURI),
	}
	if len(d.ClientCertificate) > 0 {
		m.ClientCertificateThumbprint = fmt.Sprintf("%x", sha1.Sum(d.ClientCertificate)) //nolint:gosec // see import
	}
	return m
}

func newDiagnosticsSessionsCmd() *cobra.Command {
	var (
		session  string
		security bool
	)
	cmd := &cobra.Command{
		Use:   "sessions",
		Short: "List the sessions (SessionDiagnosticsArray)",
		Long: `List the sessions of the server: name, client, endpoint, connect and last
contact time, subscriptions, monitored items and request counts.

--session <SessionId> shows the diagnostics of one session in detail, with
the request counts per service ("self" for the session of opcuactl itself).
Another session's details usually need an administrator.

--security shows the session security diagnostics instead: user,
authentication mechanism, transport, security mode and policy, client
certificate thumbprint. The array usually needs a SecurityAdmin over an
encrypted channel; --session self works for everyone.

Examples:
  opcuactl diagnostics sessions -e opc.tcp://localhost:4840
  opcuactl diagnostics sessions -e opc.tcp://localhost:4840 --session self
  opcuactl diagnostics sessions -e opc.tcp://localhost:4840 --session "ns=3;g=..." --format json
  opcuactl diagnostics sessions -e opc.tcp://localhost:4840 --security --session self`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateSessionFlag(session); err != nil {
				return err
			}
			return withClient(func(ctx context.Context, c *opcua.Client) error {
				sid, err := sessionArg(c, session)
				if err != nil {
					return err
				}
				if security {
					return writeSessionSecurity(ctx, c, sid)
				}
				if sid != nil {
					d, err := c.SessionDiagnosticsOf(ctx, sid)
					if err != nil {
						return diagnosticsFailed("the diagnostics of session "+sid.String(), err)
					}
					return writeSessionDetail(sessionDiagnostics(d))
				}
				ds, err := c.SessionDiagnostics(ctx)
				if err != nil {
					return diagnosticsFailed("the SessionDiagnosticsArray", err)
				}
				warnIfDisabled(ctx, c, len(ds))
				out := make([]model.SessionDiagnostics, 0, len(ds))
				for _, d := range ds {
					out = append(out, sessionDiagnostics(d))
				}
				if format.ParseFormat(outputFormat) != format.Table {
					return newWriter().Write(out)
				}
				headers := []string{"SESSION ID", "NAME", "CLIENT", "ENDPOINT", "CONNECTED", "LAST CONTACT", "SUBS", "ITEMS", "REQUESTS", "ERRORS", "UNAUTHORIZED"}
				rows := make([][]string, len(out))
				for i, s := range out {
					client := s.ClientName
					if client == "" {
						client = s.ClientURI
					}
					rows[i] = []string{s.SessionID, s.SessionName, client, s.EndpointURL, s.ClientConnectionTime, s.ClientLastContactTime,
						fmt.Sprint(s.CurrentSubscriptionsCount), fmt.Sprint(s.CurrentMonitoredItemsCount),
						fmt.Sprint(s.TotalRequestCount.Total), fmt.Sprint(s.TotalRequestCount.Errors), fmt.Sprint(s.UnauthorizedRequestCount)}
				}
				return newWriter().WriteTable(headers, rows)
			})
		},
	}
	cmd.Flags().StringVar(&session, "session", "", "SessionId of one session (or self) to show in detail")
	cmd.Flags().BoolVar(&security, "security", false, "Show the session security diagnostics")
	return cmd
}

func writeSessionDetail(s model.SessionDiagnostics) error {
	if format.ParseFormat(outputFormat) != format.Table {
		return newWriter().Write(s)
	}
	writeKV("Session "+s.SessionID, [][2]string{
		{"Name", s.SessionName},
		{"Client", strings.TrimSpace(s.ClientName + " " + s.ClientURI)},
		{"Endpoint", s.EndpointURL},
		{"Locales", strings.Join(s.LocaleIDs, ", ")},
		{"Session timeout", fmt.Sprintf("%g ms", s.ActualSessionTimeoutMs)},
		{"Max response size", fmt.Sprint(s.MaxResponseMessageSize)},
		{"Connected", s.ClientConnectionTime},
		{"Last contact", s.ClientLastContactTime},
		{"Subscriptions", fmt.Sprint(s.CurrentSubscriptionsCount)},
		{"Monitored items", fmt.Sprint(s.CurrentMonitoredItemsCount)},
		{"Queued publish requests", fmt.Sprint(s.CurrentPublishRequestsInQueue)},
		{"Requests", fmt.Sprintf("%d (%d errors)", s.TotalRequestCount.Total, s.TotalRequestCount.Errors)},
		{"Unauthorized requests", fmt.Sprint(s.UnauthorizedRequestCount)},
	})
	if len(s.Services) == 0 {
		return nil
	}
	names := make([]string, 0, len(s.Services))
	for n := range s.Services {
		names = append(names, n)
	}
	sort.Strings(names)
	fmt.Println()
	rows := make([][]string, len(names))
	for i, n := range names {
		rows[i] = []string{n, fmt.Sprint(s.Services[n].Total), fmt.Sprint(s.Services[n].Errors)}
	}
	return newWriter().WriteTable([]string{"SERVICE", "REQUESTS", "ERRORS"}, rows)
}

func writeSessionSecurity(ctx context.Context, c *opcua.Client, sid *ua.NodeID) error {
	var out []model.SessionSecurityDiagnostics
	if sid != nil {
		d, err := c.SessionSecurityDiagnosticsOf(ctx, sid)
		if err != nil {
			return diagnosticsFailed("the security diagnostics of session "+sid.String(), err)
		}
		out = append(out, sessionSecurityDiagnostics(d))
	} else {
		ds, err := c.SessionSecurityDiagnostics(ctx)
		if err != nil {
			return diagnosticsFailed("the SessionSecurityDiagnosticsArray", err)
		}
		warnIfDisabled(ctx, c, len(ds))
		for _, d := range ds {
			out = append(out, sessionSecurityDiagnostics(d))
		}
	}
	if format.ParseFormat(outputFormat) != format.Table {
		if sid != nil {
			return newWriter().Write(out[0])
		}
		if out == nil {
			out = []model.SessionSecurityDiagnostics{}
		}
		return newWriter().Write(out)
	}
	headers := []string{"SESSION ID", "USER", "AUTHENTICATION", "TRANSPORT", "SECURITY MODE", "SECURITY POLICY", "CLIENT CERT"}
	rows := make([][]string, len(out))
	for i, s := range out {
		rows[i] = []string{s.SessionID, s.ClientUserID, s.AuthenticationMechanism, s.TransportProtocol, s.SecurityMode, s.SecurityPolicy, s.ClientCertificateThumbprint}
	}
	return newWriter().WriteTable(headers, rows)
}

func subscriptionDiagnostics(d *ua.SubscriptionDiagnosticsDataType) model.SubscriptionDiagnostics {
	return model.SubscriptionDiagnostics{
		SessionID:                    d.SessionID.String(),
		SubscriptionID:               d.SubscriptionID,
		Priority:                     d.Priority,
		PublishingIntervalMs:         d.PublishingInterval,
		MaxKeepAliveCount:            d.MaxKeepAliveCount,
		MaxLifetimeCount:             d.MaxLifetimeCount,
		MaxNotificationsPerPublish:   d.MaxNotificationsPerPublish,
		PublishingEnabled:            d.PublishingEnabled,
		ModifyCount:                  d.ModifyCount,
		EnableCount:                  d.EnableCount,
		DisableCount:                 d.DisableCount,
		RepublishRequestCount:        d.RepublishRequestCount,
		RepublishMessageRequestCount: d.RepublishMessageRequestCount,
		RepublishMessageCount:        d.RepublishMessageCount,
		TransferRequestCount:         d.TransferRequestCount,
		TransferredToAltClientCount:  d.TransferredToAltClientCount,
		TransferredToSameClientCount: d.TransferredToSameClientCount,
		PublishRequestCount:          d.PublishRequestCount,
		DataChangeNotificationsCount: d.DataChangeNotificationsCount,
		EventNotificationsCount:      d.EventNotificationsCount,
		NotificationsCount:           d.NotificationsCount,
		LatePublishRequestCount:      d.LatePublishRequestCount,
		CurrentKeepAliveCount:        d.CurrentKeepAliveCount,
		CurrentLifetimeCount:         d.CurrentLifetimeCount,
		UnacknowledgedMessageCount:   d.UnacknowledgedMessageCount,
		DiscardedMessageCount:        d.DiscardedMessageCount,
		MonitoredItemCount:           d.MonitoredItemCount,
		DisabledMonitoredItemCount:   d.DisabledMonitoredItemCount,
		MonitoringQueueOverflowCount: d.MonitoringQueueOverflowCount,
		NextSequenceNumber:           d.NextSequenceNumber,
		EventQueueOverflowCount:      d.EventQueueOverflowCount,
	}
}

func newDiagnosticsSubscriptionsCmd() *cobra.Command {
	var session string
	cmd := &cobra.Command{
		Use:   "subscriptions",
		Short: "List the subscriptions (SubscriptionDiagnosticsArray)",
		Long: `List the subscriptions of the server: session, id, publishing interval,
monitored items, publish requests and notifications, republish and
transfer counts. --format json shows every counter of the
SubscriptionDiagnosticsDataType.

--session <SessionId> (or self) keeps the subscriptions of one session.

Examples:
  opcuactl diagnostics subscriptions -e opc.tcp://localhost:4840
  opcuactl diagnostics subscriptions -e opc.tcp://localhost:4840 --session self --format json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateSessionFlag(session); err != nil {
				return err
			}
			return withClient(func(ctx context.Context, c *opcua.Client) error {
				sid, err := sessionArg(c, session)
				if err != nil {
					return err
				}
				ds, err := c.SubscriptionDiagnostics(ctx)
				if err != nil {
					return diagnosticsFailed("the SubscriptionDiagnosticsArray", err)
				}
				warnIfDisabled(ctx, c, len(ds))
				out := []model.SubscriptionDiagnostics{}
				for _, d := range ds {
					m := subscriptionDiagnostics(d)
					if sid != nil && m.SessionID != sid.String() {
						continue
					}
					out = append(out, m)
				}
				if format.ParseFormat(outputFormat) != format.Table {
					return newWriter().Write(out)
				}
				headers := []string{"SESSION ID", "SUBSCRIPTION", "INTERVAL", "ENABLED", "ITEMS", "PUBLISH REQUESTS", "NOTIFICATIONS", "DATA CHANGES", "EVENTS", "REPUBLISH", "TRANSFERS"}
				rows := make([][]string, len(out))
				for i, s := range out {
					rows[i] = []string{s.SessionID, fmt.Sprint(s.SubscriptionID), fmt.Sprintf("%gms", s.PublishingIntervalMs),
						fmt.Sprint(s.PublishingEnabled), fmt.Sprint(s.MonitoredItemCount), fmt.Sprint(s.PublishRequestCount),
						fmt.Sprint(s.NotificationsCount), fmt.Sprint(s.DataChangeNotificationsCount), fmt.Sprint(s.EventNotificationsCount),
						fmt.Sprint(s.RepublishRequestCount), fmt.Sprint(s.TransferRequestCount)}
				}
				return newWriter().WriteTable(headers, rows)
			})
		},
	}
	cmd.Flags().StringVar(&session, "session", "", "Only the subscriptions of this SessionId (or self)")
	return cmd
}

func newDiagnosticsSamplingCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sampling",
		Short: "List the sampling intervals in use (SamplingIntervalDiagnosticsArray)",
		Long: `List the sampling intervals the server samples monitored items at, with
the number of monitored items (current, maximum, disabled) of each.

Examples:
  opcuactl diagnostics sampling -e opc.tcp://localhost:4840`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return withClient(func(ctx context.Context, c *opcua.Client) error {
				ds, err := c.SamplingIntervalDiagnostics(ctx)
				if err != nil {
					return diagnosticsFailed("the SamplingIntervalDiagnosticsArray", err)
				}
				warnIfDisabled(ctx, c, len(ds))
				out := make([]model.SamplingIntervalDiagnostics, 0, len(ds))
				for _, d := range ds {
					out = append(out, model.SamplingIntervalDiagnostics{
						SamplingIntervalMs:         d.SamplingInterval,
						MonitoredItemCount:         d.MonitoredItemCount,
						MaxMonitoredItemCount:      d.MaxMonitoredItemCount,
						DisabledMonitoredItemCount: d.DisabledMonitoredItemCount,
					})
				}
				if format.ParseFormat(outputFormat) != format.Table {
					return newWriter().Write(out)
				}
				rows := make([][]string, len(out))
				for i, s := range out {
					rows[i] = []string{fmt.Sprintf("%gms", s.SamplingIntervalMs), fmt.Sprint(s.MonitoredItemCount), fmt.Sprint(s.MaxMonitoredItemCount), fmt.Sprint(s.DisabledMonitoredItemCount)}
				}
				return newWriter().WriteTable([]string{"SAMPLING INTERVAL", "ITEMS", "MAX ITEMS", "DISABLED ITEMS"}, rows)
			})
		},
	}
}

func newDiagnosticsEnableCmd(enable bool) *cobra.Command {
	use, short := "disable", "Stop the collection of server diagnostics"
	if enable {
		use, short = "enable", "Start the collection of server diagnostics (resets the counters)"
	}
	return &cobra.Command{
		Use:   use,
		Short: short,
		Long: short + `.

Writes Server.ServerDiagnostics.EnabledFlag. Servers let administrators only
do so (the SecurityAdmin or ConfigureAdmin role): connect with such an
identity, e.g. --auth username --username admin. While diagnostics are off
the counters stand still and the arrays are empty; switching them on resets
the counters.

Examples:
  opcuactl diagnostics ` + use + ` -e opc.tcp://localhost:4840 --auth username --username admin --password secret`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return withClient(func(ctx context.Context, c *opcua.Client) error {
				if err := c.SetServerDiagnosticsEnabled(ctx, enable); err != nil {
					msg := fmt.Sprintf("writing EnabledFlag: %v", err)
					if h := diagnosticsHint(err, "changing EnabledFlag"); h != "" {
						msg += " (" + h + ")"
					}
					format.Errf("%s", msg)
					return exitCode(exitProtocol, err)
				}
				on, err := c.ServerDiagnosticsEnabled(ctx)
				if err != nil {
					return diagnosticsFailed("EnabledFlag", err)
				}
				res := struct {
					Enabled bool `json:"enabled"`
				}{on}
				if format.ParseFormat(outputFormat) != format.Table {
					return newWriter().Write(res)
				}
				if !quiet {
					fmt.Printf("Server diagnostics enabled: %v\n", on)
				} else {
					fmt.Println(on)
				}
				return nil
			})
		},
	}
}
