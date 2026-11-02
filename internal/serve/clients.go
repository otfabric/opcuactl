// SPDX-License-Identifier: MIT

package serve

import (
	"log/slog"
	"net"
	"time"

	"github.com/otfabric/go-opcua/server"
	"github.com/otfabric/go-opcua/ua"
)

// clientLog logs the connections, sessions and refused requests of the
// clients (server.SessionObserver). The server calls it synchronously; a
// slog handler writing to stderr does not block for long.
type clientLog struct {
	log *slog.Logger
}

func addr(a net.Addr) string {
	if a == nil {
		return ""
	}
	return a.String()
}

func (c clientLog) OnConnectionOpened(remote net.Addr) {
	c.log.Info("client connected", "remote", addr(remote))
}

func (c clientLog) OnConnectionClosed(remote net.Addr, d time.Duration) {
	c.log.Info("client disconnected", "remote", addr(remote), "duration", d.Round(time.Millisecond))
}

func (c clientLog) OnSessionActivated(i server.SessionInfo) {
	msg := "session activated"
	if i.Reactivated {
		msg = "session reactivated"
	}
	c.log.Info(msg, append(sessionAttrs(i), "remote", addr(i.RemoteAddr),
		"policy", policyName(i.SecurityPolicyURI), "mode", i.SecurityMode.String())...)
}

func (c clientLog) OnSessionClosed(i server.SessionInfo, reason server.SessionCloseReason) {
	c.log.Info("session closed", append(sessionAttrs(i), "reason", reason.String())...)
}

func (c clientLog) OnRejected(r server.Rejection) {
	args := []any{"service", r.Service, "status", r.Status.Error(), "remote", addr(r.RemoteAddr),
		"policy", policyName(r.SecurityPolicyURI), "mode", r.SecurityMode.String()}
	if r.Session != nil {
		args = append(args, sessionAttrs(*r.Session)...)
	}
	c.log.Warn("client rejected", args...)
}

// sessionAttrs are the attributes of a session in the log.
func sessionAttrs(i server.SessionInfo) []any {
	args := []any{}
	if i.ID != nil {
		args = append(args, "session", i.ID.String())
	}
	args = append(args, "name", i.Name)
	if i.Client != nil {
		app := ""
		if i.Client.ApplicationName != nil {
			app = i.Client.ApplicationName.Text
		}
		args = append(args, "app", app, "app_uri", i.Client.ApplicationURI)
	}
	args = append(args, "identity", identityName(i), "user", i.User)
	return args
}

func identityName(i server.SessionInfo) string {
	switch i.IdentityType {
	case ua.UserTokenTypeUserName:
		return "UserName"
	case ua.UserTokenTypeCertificate:
		return "Certificate"
	case ua.UserTokenTypeIssuedToken:
		return "IssuedToken"
	case ua.UserTokenTypeAnonymous:
		return "Anonymous"
	}
	return "Anonymous"
}

// policyName is the name of a security policy URI (Basic256Sha256).
func policyName(uri string) string {
	for i := len(uri) - 1; i >= 0; i-- {
		if uri[i] == '#' {
			return uri[i+1:]
		}
	}
	return uri
}
