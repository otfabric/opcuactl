// SPDX-License-Identifier: MIT

package opcuactl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-opcua"
	"github.com/otfabric/go-opcua/server"
	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/opcuactl/internal/model"
)

// startDiagnosticsServer starts a server where the user admin has the
// SecurityAdmin role, and a client with a subscription on it.
func startDiagnosticsServer(t *testing.T) (*testServer, *opcua.Client, *opcua.Subscription) {
	t.Helper()
	s := startServer(t,
		server.EnableAuthMode(ua.UserTokenTypeUserName),
		server.AllowUsernameOnNone(),
		server.WithUsernameValidator(func(user, pass string) error {
			if (user == "admin" && pass == "secret") || (user == "operator" && pass == "op") {
				return nil
			}
			return errors.New("denied")
		}),
		server.WithRoleMapper(func(tok ua.IdentityToken) []*ua.NodeID {
			if u, ok := tok.(*ua.UserNameIdentityToken); ok && u.UserName == "admin" {
				return []*ua.NodeID{ua.RoleAuthenticatedUser.NodeID(), ua.RoleSecurityAdmin.NodeID()}
			}
			return server.DefaultRoleMapper(tok)
		}),
	)

	ctx := context.Background()
	c, err := opcua.NewClient(s.url, opcua.InsecureNoSecurity(), opcua.SessionName("other-client"))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	sub, _, err := c.NewSubscription().Interval(250 * time.Millisecond).SamplingInterval(250 * time.Millisecond).
		Monitor(ua.NewStringNodeID(s.ns.ID(), "Double")).Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return s, c, sub
}

func TestE2E_Diagnostics(t *testing.T) {
	s, other, sub := startDiagnosticsServer(t)
	ep := []string{"-e", s.url}
	admin := append([]string{"--auth", "username", "--username", "admin", "--password", "secret"}, ep...)
	operator := append([]string{"--auth", "username", "--username", "operator", "--password", "op"}, ep...)
	run := func(t *testing.T, args ...string) string { t.Helper(); return mustRun(t, args...) }
	fail := func(t *testing.T, wantStderr string, args ...string) {
		t.Helper()
		_, errOut, err := runCLI(t, args...)
		if err == nil {
			t.Fatalf("opcuactl %s succeeded", strings.Join(args, " "))
		}
		if ExitCode(err) != 3 && ExitCode(err) != 1 {
			t.Errorf("opcuactl %s: exit %d", strings.Join(args, " "), ExitCode(err))
		}
		if !strings.Contains(errOut, wantStderr) {
			t.Errorf("opcuactl %s: stderr does not contain %q:\n%s", strings.Join(args, " "), wantStderr, errOut)
		}
	}
	otherID := other.Session().SessionID().String()

	t.Run("summary", func(t *testing.T) {
		out := run(t, append([]string{"diagnostics", "summary"}, ep...)...)
		wantContains(t, out, "Server diagnostics", "Enabled:", "true", "Current sessions:", "Cumulated subscriptions:")
		var sum model.DiagnosticsSummary
		if err := json.Unmarshal([]byte(run(t, append([]string{"diagnostics", "summary", "--format", "json"}, ep...)...)), &sum); err != nil {
			t.Fatal(err)
		}
		if !sum.Enabled || sum.CurrentSessionCount < 2 || sum.CurrentSubscriptionCount < 1 || sum.CumulatedSessionCount < 2 {
			t.Errorf("summary = %+v", sum)
		}
	})

	t.Run("sessions", func(t *testing.T) {
		out := run(t, append([]string{"diagnostics", "sessions"}, ep...)...)
		wantContains(t, out, "SESSION ID", "other-client", otherID)
		var ss []model.SessionDiagnostics
		if err := json.Unmarshal([]byte(run(t, append([]string{"diagnostics", "sessions", "--format", "json"}, ep...)...)), &ss); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, d := range ss {
			if d.SessionID == otherID {
				found = d.SessionName == "other-client" && d.CurrentSubscriptionsCount == 1 && d.CurrentMonitoredItemsCount == 1
			}
		}
		if !found {
			t.Errorf("sessions = %+v", ss)
		}
	})

	t.Run("own session", func(t *testing.T) {
		out := run(t, append([]string{"diagnostics", "sessions", "--session", "self"}, ep...)...)
		wantContains(t, out, "Session ns=", "Requests:", "SERVICE", "TranslateBrowsePathsToNodeIds")
		var d model.SessionDiagnostics
		if err := json.Unmarshal([]byte(run(t, append([]string{"diagnostics", "sessions", "--session", "self", "--format", "json"}, ep...)...)), &d); err != nil {
			t.Fatal(err)
		}
		if d.TotalRequestCount.Total == 0 || d.Services["TranslateBrowsePathsToNodeIds"].Total == 0 {
			t.Errorf("own session = %+v", d)
		}
	})

	t.Run("another session needs an administrator", func(t *testing.T) {
		fail(t, "administrator", append([]string{"diagnostics", "sessions", "--session", otherID}, operator...)...)
		var d model.SessionDiagnostics
		if err := json.Unmarshal([]byte(run(t, append([]string{"diagnostics", "sessions", "--session", otherID, "--format", "json"}, admin...)...)), &d); err != nil {
			t.Fatal(err)
		}
		if d.SessionID != otherID || d.SessionName != "other-client" {
			t.Errorf("other session = %+v", d)
		}
		fail(t, "invalid --session", append([]string{"diagnostics", "sessions", "--session", "not-a-node"}, ep...)...)
	})

	t.Run("security", func(t *testing.T) {
		out := run(t, append([]string{"diagnostics", "sessions", "--security", "--session", "self"}, operator...)...)
		wantContains(t, out, "USER", "operator", "None")
		var d model.SessionSecurityDiagnostics
		if err := json.Unmarshal([]byte(run(t, append([]string{"diagnostics", "sessions", "--security", "--session", "self", "--format", "json"}, operator...)...)), &d); err != nil {
			t.Fatal(err)
		}
		if d.ClientUserID != "operator" || d.SecurityMode != "MessageSecurityModeNone" {
			t.Errorf("security = %+v", d)
		}
		// The array: SecurityAdmin over an encrypted channel only.
		fail(t, "SessionSecurityDiagnosticsArray", append([]string{"diagnostics", "sessions", "--security"}, operator...)...)
	})

	t.Run("subscriptions", func(t *testing.T) {
		out := run(t, append([]string{"diagnostics", "subscriptions"}, ep...)...)
		wantContains(t, out, "SUBSCRIPTION", otherID, fmt.Sprint(sub.SubscriptionID), "250ms")
		var subs []model.SubscriptionDiagnostics
		if err := json.Unmarshal([]byte(run(t, append([]string{"diagnostics", "subscriptions", "--session", otherID, "--format", "json"}, ep...)...)), &subs); err != nil {
			t.Fatal(err)
		}
		if len(subs) != 1 || subs[0].SubscriptionID != sub.SubscriptionID || subs[0].MonitoredItemCount != 1 {
			t.Errorf("subscriptions of the other session = %+v", subs)
		}
		if out := run(t, append([]string{"diagnostics", "subscriptions", "--session", "self", "--format", "json"}, ep...)...); strings.TrimSpace(out) != "[]" {
			t.Errorf("subscriptions of opcuactl's session = %s", out)
		}
	})

	t.Run("sampling", func(t *testing.T) {
		out := run(t, append([]string{"diagnostics", "sampling"}, ep...)...)
		wantContains(t, out, "SAMPLING INTERVAL", "250ms")
		var rates []model.SamplingIntervalDiagnostics
		if err := json.Unmarshal([]byte(run(t, append([]string{"diagnostics", "sampling", "--format", "json"}, ep...)...)), &rates); err != nil {
			t.Fatal(err)
		}
		if len(rates) == 0 {
			t.Error("no sampling intervals")
		}
	})

	t.Run("enable and disable", func(t *testing.T) {
		fail(t, "administrator", append([]string{"diagnostics", "disable"}, ep...)...)
		fail(t, "administrator", append([]string{"diagnostics", "disable"}, operator...)...)
		if !s.srv.ServerDiagnosticsEnabled() {
			t.Fatal("an anonymous client disabled the diagnostics")
		}

		out := run(t, append([]string{"diagnostics", "disable"}, admin...)...)
		wantContains(t, out, "enabled: false")
		if s.srv.ServerDiagnosticsEnabled() {
			t.Fatal("still enabled")
		}
		_, errOut, err := runCLI(t, append([]string{"diagnostics", "sessions"}, ep...)...)
		if err != nil {
			t.Fatalf("sessions while disabled: %v", err)
		}
		wantContains(t, errOut, "disabled (EnabledFlag false)")

		out = run(t, append([]string{"diagnostics", "enable", "--format", "json"}, admin...)...)
		wantContains(t, out, `"enabled":true`)
		if !s.srv.ServerDiagnosticsEnabled() {
			t.Fatal("not enabled")
		}
	})
}

// TestE2E_ReturnDiagnostics prints the DiagnosticInfos of failed operations.
func TestE2E_ReturnDiagnostics(t *testing.T) {
	s := startServer(t)
	missing := fmt.Sprintf("ns=%d;s=Missing", s.ns.ID())

	t.Run("read", func(t *testing.T) {
		_, errOut, err := runCLI(t, "get", "value", "-e", s.url, "-n", missing, "-n", s.node("Int32"), "--return-diagnostics", "operation")
		if err != nil {
			t.Fatalf("get value: %v", err)
		}
		wantContains(t, errOut, "Diagnostic (read "+missing+"):", "SymbolicId:", "BadNodeIdUnknown", "AdditionalInfo:", "Text:")
		if strings.Contains(errOut, "Diagnostic (read "+s.node("Int32")) {
			t.Errorf("a diagnostic for the good read:\n%s", errOut)
		}

		// Without the flag, or with service only, nothing is printed.
		for _, args := range [][]string{{}, {"--return-diagnostics", "service"}} {
			_, errOut, _ := runCLI(t, append([]string{"get", "value", "-e", s.url, "-n", missing}, args...)...)
			if strings.Contains(errOut, "Diagnostic") {
				t.Errorf("%v: diagnostics printed:\n%s", args, errOut)
			}
		}
	})

	t.Run("environment", func(t *testing.T) {
		t.Setenv("OPCUACTL_RETURN_DIAGNOSTICS", "all")
		_, errOut, _ := runCLI(t, "get", "value", "-e", s.url, "-n", missing)
		wantContains(t, errOut, "Diagnostic (read "+missing+"):")
	})

	t.Run("write", func(t *testing.T) {
		_, errOut, _ := runCLI(t, "write", "value", "-e", s.url, "-n", missing, "--value", "1", "--yes", "--return-diagnostics", "all")
		wantContains(t, errOut, "Diagnostic (write "+missing+"):", "BadNodeIdUnknown")
	})

	t.Run("call", func(t *testing.T) {
		_, errOut, _ := runCLI(t, "call", "method", "-e", s.url, "--object", "i=2253", "--method", missing, "--return-diagnostics", "all")
		wantContains(t, errOut, "Diagnostic (call "+missing+"):")
	})

	t.Run("service", func(t *testing.T) {
		// A server without the multicast extension rejects FindServersOnNetwork.
		_, errOut, err := runCLI(t, "discover", "servers", "-e", s.url, "--on-network", "--return-diagnostics", "service")
		if err == nil {
			t.Fatal("FindServersOnNetwork succeeded")
		}
		wantContains(t, errOut, "Diagnostic (service BadServiceUnsupported):", "AdditionalInfo:", "FindServersOnNetwork")
	})

	t.Run("invalid mask", func(t *testing.T) {
		_, _, err := runCLI(t, "get", "value", "-e", s.url, "-n", missing, "--return-diagnostics", "loud")
		if err == nil || ExitCode(err) != 1 {
			t.Errorf("err = %v (exit %d)", err, ExitCode(err))
		}
	})
}
