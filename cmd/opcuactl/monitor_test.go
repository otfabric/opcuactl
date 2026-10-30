// SPDX-License-Identifier: MIT

package opcuactl

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-opcua"
	"github.com/otfabric/go-opcua/ua"
)

// captureStderr returns what fn writes to stderr.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	cliMu.Lock()
	defer cliMu.Unlock()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = orig }()
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	fn()
	_ = w.Close()
	return <-done
}

func dataChange(handle uint32, v any) *opcua.PublishNotificationData {
	return &opcua.PublishNotificationData{Value: &ua.DataChangeNotification{
		MonitoredItems: []*ua.MonitoredItemNotification{{ClientHandle: handle, Value: &ua.DataValue{Value: ua.MustVariant(v)}}},
	}}
}

func eventList(values ...string) *opcua.PublishNotificationData {
	var events []*ua.EventFieldList
	for _, v := range values {
		events = append(events, &ua.EventFieldList{EventFields: []*ua.Variant{ua.MustVariant(v)}})
	}
	return &opcua.PublishNotificationData{Value: &ua.EventNotificationList{Events: events}}
}

func TestStreamNotifications(t *testing.T) {
	closed := make(chan struct{})
	close(closed)
	ended := make(chan time.Time)
	close(ended)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	done := func(*opcua.PublishNotificationData) bool { return true }
	never := func(*opcua.PublishNotificationData) bool { return false }

	// Queued so that the random select of the loop meets lost with some left.
	queue := func(n int) chan *opcua.PublishNotificationData {
		ch := make(chan *opcua.PublishNotificationData, n)
		for i := 0; i < n; i++ {
			ch <- &opcua.PublishNotificationData{}
		}
		return ch
	}

	t.Run("cancelled", func(t *testing.T) {
		if err := streamNotifications(cancelled, nil, nil, nil, "x", never); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("deadline", func(t *testing.T) {
		if err := streamNotifications(context.Background(), ended, nil, nil, "x", never); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("handler done", func(t *testing.T) {
		if err := streamNotifications(context.Background(), nil, nil, queue(1), "x", done); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("lost after what arrived", func(t *testing.T) {
		seen := 0
		var err error
		captureStderr(t, func() {
			err = streamNotifications(context.Background(), nil, closed, queue(32), "x", func(*opcua.PublishNotificationData) bool {
				seen++
				return false
			})
		})
		if got := ExitCode(err); got != exitTransport {
			t.Fatalf("exit %d (%v)", got, err)
		}
		if seen != 32 {
			t.Errorf("handled %d, want 32", seen)
		}
	})
	t.Run("done in what arrived before lost", func(t *testing.T) {
		if err := streamNotifications(context.Background(), nil, closed, queue(32), "x", done); err != nil {
			t.Fatal(err)
		}
	})
}

func TestValuePrinter(t *testing.T) {
	handles := map[uint32]string{1: "i=2258"}
	t.Run("table, unknown handle, overflow", func(t *testing.T) {
		out := captureStdout(t, func() {
			print := valuePrinter(handles, 0, false)
			if print(dataChange(1, int32(7))) || print(dataChange(9, int32(8))) {
				t.Error("done without a count")
			}
			over := dataChange(1, int32(9))
			over.Value.(*ua.DataChangeNotification).MonitoredItems[0].Value.Status = ua.StatusCodeOverflowBit | ua.StatusCodeInfoTypeDataValue
			print(over)
		})
		if !strings.Contains(out, "i=2258  7") || !strings.Contains(out, "handle:9  8") || !strings.Contains(out, "[OVERFLOW]") {
			t.Errorf("stdout %q", out)
		}
	})
	t.Run("json", func(t *testing.T) {
		out := captureStdout(t, func() { valuePrinter(handles, 0, true)(dataChange(1, int32(7))) })
		if !strings.Contains(out, `"i=2258"`) {
			t.Errorf("stdout %q", out)
		}
	})
	t.Run("count", func(t *testing.T) {
		captureStdout(t, func() {
			print := valuePrinter(handles, 2, false)
			if print(dataChange(1, int32(1))) {
				t.Error("done after one")
			}
			if !print(dataChange(1, int32(2))) {
				t.Error("not done after two")
			}
		})
	})
	t.Run("error and other notifications are skipped", func(t *testing.T) {
		errOut := captureStderr(t, func() {
			print := valuePrinter(handles, 1, false)
			if print(&opcua.PublishNotificationData{Error: errors.New("publish failed")}) || print(eventList("x")) {
				t.Error("done")
			}
		})
		if !strings.Contains(errOut, "publish failed") {
			t.Errorf("stderr %q", errOut)
		}
	})
}

func TestEventPrinter(t *testing.T) {
	t.Run("table", func(t *testing.T) {
		out := captureStdout(t, func() {
			extra := eventList("a")
			extra.Value.(*ua.EventNotificationList).Events[0].EventFields = append(extra.Value.(*ua.EventNotificationList).Events[0].EventFields, nil, ua.MustVariant("b"))
			eventPrinter("i=2253", []string{"Message"}, 0, false)(extra)
		})
		if !strings.Contains(out, "i=2253  Message=a") {
			t.Errorf("stdout %q", out)
		}
	})
	t.Run("json keeps a field without a name", func(t *testing.T) {
		out := captureStdout(t, func() {
			extra := eventList("a", "b")
			extra.Value.(*ua.EventNotificationList).Events[1].EventFields = append(extra.Value.(*ua.EventNotificationList).Events[1].EventFields, ua.MustVariant("c"))
			eventPrinter("i=2253", []string{"Message"}, 0, true)(extra)
		})
		if !strings.Contains(out, `"unknown":"c"`) {
			t.Errorf("stdout %q", out)
		}
	})
	t.Run("count inside a notification", func(t *testing.T) {
		out := captureStdout(t, func() {
			if !eventPrinter("i=2253", []string{"Message"}, 2, true)(eventList("a", "b", "c")) {
				t.Error("not done")
			}
		})
		if strings.Contains(out, `"c"`) {
			t.Errorf("stdout %q", out)
		}
	})
	t.Run("error and other notifications are skipped", func(t *testing.T) {
		errOut := captureStderr(t, func() {
			print := eventPrinter("i=2253", nil, 1, false)
			if print(&opcua.PublishNotificationData{Error: errors.New("publish failed")}) || print(dataChange(1, int32(1))) {
				t.Error("done")
			}
		})
		if !strings.Contains(errOut, "publish failed") {
			t.Errorf("stderr %q", errOut)
		}
	})
}
