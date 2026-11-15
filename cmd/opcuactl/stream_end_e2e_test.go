// SPDX-License-Identifier: MIT

package opcuactl

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-opcua"
	uaerrors "github.com/otfabric/go-opcua/errors"
	"github.com/otfabric/go-opcua/id"
	"github.com/otfabric/go-opcua/server"
	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/go-opcua/uasc"
)

func statusChange(s ua.StatusCode) *opcua.PublishNotificationData {
	return &opcua.PublishNotificationData{Value: &ua.StatusChangeNotification{Status: s}}
}

// A StatusChangeNotification that ends the subscription ends the stream
// with exit 4, after what arrived before it; one with another status, and a
// NotificationData the client could not use, do not.
func TestStreamNotificationsStatusChange(t *testing.T) {
	for _, c := range []struct {
		name    string
		notif   *opcua.PublishNotificationData
		ends    bool
		message string
	}{
		{"transferred", statusChange(ua.StatusGoodSubscriptionTransferred), true, "x: the server transferred the subscription to another session (GoodSubscriptionTransferred)"},
		{"lifetime expired", statusChange(ua.StatusBadTimeout), true, "x: the server ended the subscription (BadTimeout)"},
		{"closed", statusChange(ua.StatusBadSubscriptionIDInvalid), true, "x: the server ended the subscription (BadSubscriptionIDInvalid)"},
		{"publishing failed", &opcua.PublishNotificationData{Error: ua.StatusBadSessionClosed}, true, "x: subscription lost:"},
		{"good status", statusChange(ua.StatusOK), false, ""},
		{"uncertain status", statusChange(ua.StatusUncertain), false, ""},
		{"empty notification data", &opcua.PublishNotificationData{Error: uaerrors.ErrEmptyResponse}, false, ""},
		{"unknown notification data", &opcua.PublishNotificationData{Error: uaerrors.ErrInvalidResponseType}, false, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			ch := make(chan *opcua.PublishNotificationData, 3)
			ch <- dataChange(1, int32(1))
			ch <- c.notif
			ch <- dataChange(1, int32(2))
			seen := 0
			var err error
			// After the three, a closed lost channel ends a stream that
			// went on.
			lost := make(chan struct{})
			handle := func(*opcua.PublishNotificationData) bool {
				seen++
				if len(ch) == 0 {
					close(lost)
				}
				return false
			}
			stderr := captureStderr(t, func() {
				err = streamNotifications(context.Background(), nil, lost, ch, "x", handle)
			})
			if got := ExitCode(err); got != exitTransport {
				t.Fatalf("exit %d, want %d (%v)", got, exitTransport, err)
			}
			if c.ends {
				if seen != 1 {
					t.Errorf("handled %d, want the 1 before the end", seen)
				}
				if !strings.Contains(stderr, c.message) || !strings.Contains(err.Error(), c.message) {
					t.Errorf("stderr %q, error %q, want %q", stderr, err, c.message)
				}
				return
			}
			if seen != 3 || !strings.Contains(err.Error(), "connection lost") {
				t.Errorf("handled %d, error %v: the stream did not go on", seen, err)
			}
		})
	}
}

// transferServer is a test server that reports the subscription id of every
// CreateMonitoredItems it answered.
func transferServer(t *testing.T) (*testServer, <-chan uint32) {
	t.Helper()
	monitored := make(chan uint32, 16)
	s := startServerWith(t, func(srv *server.Server) {
		srv.RegisterHandler(id.CreateMonitoredItemsRequestEncodingDefaultBinary, func(ctx context.Context, sc *uasc.SecureChannel, r ua.Request, reqID uint32) (ua.Response, error) {
			resp, err := srv.MonitoredItemService.CreateMonitoredItems(ctx, sc, r, reqID)
			if err == nil {
				monitored <- r.(*ua.CreateMonitoredItemsRequest).SubscriptionID
			}
			return resp, err
		})
	})
	return s, monitored
}

// When another session takes over the subscription of a stream
// (TransferSubscriptions), the server sends the stream's session
// Good_SubscriptionTransferred and nothing more: the stream ends with exit 4,
// as for a lost connection, instead of waiting in silence.
func TestStreamSubscriptionTransferred(t *testing.T) {
	s, monitored := transferServer(t)
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"monitor", "value", "-n", s.node("Int32")}, "monitoring " + s.node("Int32") + ": the server transferred the subscription"},
		{[]string{"monitor", "event", "-n", "i=2253"}, "monitoring events on i=2253: the server transferred the subscription"},
		{[]string{"monitor", "alarm", "-n", "i=2253"}, "monitoring events on i=2253: the server transferred the subscription"},
		{[]string{"alarms", "watch", "--no-refresh"}, "watching conditions on i=2253: the server transferred the subscription"},
	} {
		t.Run(strings.Join(c.args, " "), func(t *testing.T) {
			type result struct {
				stderr string
				err    error
			}
			done := make(chan result, 1)
			go func() {
				_, stderr, err := runCLI(t, append(c.args, "-e", s.url, "--duration", "60s")...)
				done <- result{stderr, err}
			}()
			var subID uint32
			select {
			case subID = <-monitored:
			case r := <-done:
				t.Fatalf("ended before its subscription: %v\n%s", r.err, r.stderr)
			case <-time.After(15 * time.Second):
				t.Fatal("no monitored item was created")
			}
			transferSubscription(t, s.url, subID)
			select {
			case r := <-done:
				if got := ExitCode(r.err); got != exitTransport {
					t.Errorf("exit %d, want %d (%v)", got, exitTransport, r.err)
				}
				if !strings.Contains(r.stderr, c.want) || !strings.Contains(r.stderr, "GoodSubscriptionTransferred") {
					t.Errorf("stderr %q, want %q", r.stderr, c.want)
				}
			case <-time.After(15 * time.Second):
				t.Fatal("the stream did not end after its subscription was transferred")
			}
		})
	}
}

// transferSubscription moves subscription subID to a new session.
func transferSubscription(t *testing.T, url string, subID uint32) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := opcua.NewClient(url, opcua.InsecureNoSecurity(), opcua.AutoReconnect(false))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	res, err := c.TransferSubscriptions(ctx, []uint32{subID}, false)
	if err == nil && (len(res.Results) != 1 || res.Results[0].StatusCode != ua.StatusOK) {
		err = errors.New("not transferred")
	}
	if err != nil {
		t.Fatalf("TransferSubscriptions(%d): %v %+v", subID, err, res)
	}
}
