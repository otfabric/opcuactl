// SPDX-License-Identifier: MIT

package opcuactl

import (
	"testing"
	"time"

	"github.com/otfabric/go-opcua/pubsub/transport"
)

// The queue of pubsub subscribe drops what does not fit while the output is
// blocked, and says so on stderr without --verbose: at once, then at most
// once per dropReportInterval, and the total at the end.
func TestPubSubQueueReportsDrops(t *testing.T) {
	q := newPubSubQueue(4)
	for i := 0; i < 10; i++ {
		q.deliver(&transport.Message{Payload: []byte{byte(i)}})
	}
	if len(q.ch) != 4 {
		t.Fatalf("queued %d, want 4", len(q.ch))
	}
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		deliver int
		at      time.Duration
		want    string
	}{
		{0, 0, "6 message(s) dropped: output too slow\n"},
		{3, time.Second, ""},
		{0, dropReportInterval - time.Nanosecond, ""},
		{0, dropReportInterval, "3 message(s) dropped: output too slow\n"},
		{0, 2 * dropReportInterval, ""},
		{2, 2*dropReportInterval + time.Second, "2 message(s) dropped: output too slow\n"},
		{1, 2*dropReportInterval + 2*time.Second, ""},
	} {
		for i := 0; i < c.deliver; i++ {
			q.deliver(&transport.Message{})
		}
		if got := captureStderr(t, func() { q.report(t0.Add(c.at)) }); got != c.want {
			t.Errorf("at %v: stderr %q, want %q", c.at, got, c.want)
		}
	}
	if got, want := captureStderr(t, q.final), "12 message(s) dropped in all: output too slow\n"; got != want {
		t.Errorf("final: %q, want %q", got, want)
	}
	for i := 0; i < 4; i++ {
		if m := <-q.ch; len(m.Payload) != 1 || m.Payload[0] != byte(i) {
			t.Fatalf("message %d: %v, want the first ones in order", i, m.Payload)
		}
	}

	none := newPubSubQueue(4)
	none.deliver(&transport.Message{})
	if got := captureStderr(t, func() { none.report(t0); none.final() }); got != "" {
		t.Errorf("without drops: %q", got)
	}
}
