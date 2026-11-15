// SPDX-License-Identifier: MIT

package opcuactl

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-opcua/pubsub"
	"github.com/otfabric/go-opcua/pubsub/jsonmsg"
	"github.com/otfabric/go-opcua/pubsub/message"
	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/opcuactl/internal/parse"
)

func testPublisher(i int) message.PublisherID {
	return message.NewPublisherID(message.PublisherIDUInt32, uint64(i))
}

// metaDataMessage is a JSON ua-metadata message of writer 1 of publisher i
// with n fields.
func metaDataMessage(t testing.TB, i, n int) []byte {
	t.Helper()
	fields := make([]pubsub.Field, n)
	for j := range fields {
		fields[j] = pubsub.Field{Name: fmt.Sprintf("field-%04d-of-a-dataset-with-a-long-name", j), Type: ua.TypeIDDouble}
	}
	b, err := jsonmsg.EncodeMetaData(&jsonmsg.MetaDataMessage{PublisherID: testPublisher(i), DataSetWriterID: 1,
		MetaData: pubsub.MetaData(fmt.Sprintf("DataSet %d", i), fields...)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func testDecoder(t *testing.T, filter pubsubFilterFlags) *pubsubDecoder {
	t.Helper()
	d, err := newPubSubDecoder(parse.EncodingAuto, filter, nil, nil, ua.MessageSecurityModeNone)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func heapInUse() uint64 {
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

// The DataSetMetaData that pubsub subscribe learns from the network is kept
// for at most 1024 writers (the default of --max-learned-metadata): the heap
// does not grow with the number of writers that announce one.
func TestPubSubLearnedMetaDataBounded(t *testing.T) {
	const limit, more = 1024, 8000
	d := testDecoder(t, pubsubFilterFlags{})
	learn := func(from, to int) {
		for i := from; i < to; i++ {
			if _, err := d.decode(metaDataMessage(t, i, 8), nil); err != nil {
				t.Fatalf("writer %d: %v", i, err)
			}
		}
	}
	learn(0, limit)
	before := heapInUse()
	learn(limit, limit+more)
	after := heapInUse()
	// Each DataSetMetaData of 8 fields holds about 2 KB: 8000 more that
	// were kept would add more than 16 MB.
	if after > before && after-before > 4<<20 {
		t.Errorf("the heap grew by %d bytes for %d more writers", after-before, more)
	}
	if d.lookup(testPublisher(0), 0, 1) != nil {
		t.Error("the DataSetMetaData of the first writer is still kept")
	}
	for _, i := range []int{more, limit + more - 1} {
		if d.lookup(testPublisher(i), 0, 1) == nil {
			t.Errorf("the DataSetMetaData of writer %d, one of the last %d, is not kept", i, limit)
		}
	}
}

// The writer used least recently makes room; a writer learned again keeps
// one entry.
func TestLearnedMetaDataEviction(t *testing.T) {
	md := pubsub.MetaData("DS")
	for _, c := range []struct {
		max, want int
	}{{-1, 1}, {0, 1}, {1, 1}, {2, 2}, {defaultMaxLearnedMetaData, defaultMaxLearnedMetaData}} {
		l := newLearnedMetaData(c.max)
		for i := 0; i < c.want+3; i++ {
			l.put(fmt.Sprint(i), md)
		}
		if l.size() != c.want || l.evictions() != 3 {
			t.Errorf("max %d: %d kept, %d evicted, want %d and 3", c.max, l.size(), l.evictions(), c.want)
		}
	}

	l := newLearnedMetaData(2)
	l.put("a", md)
	l.put("b", md)
	l.put("a", pubsub.MetaData("newer"))
	if l.get("a").Name != "newer" || l.size() != 2 {
		t.Fatalf("a learned again: %q, %d kept", l.get("a").Name, l.size())
	}
	l.get("b")
	l.put("c", md) // a is the least recently used
	if l.get("a") != nil || l.get("b") == nil || l.get("c") == nil || l.evictions() != 1 {
		t.Errorf("a %v, b %v, c %v, %d evicted", l.get("a"), l.get("b"), l.get("c"), l.evictions())
	}
}

// A DataSetMetaData with more fields than are kept is an error, and not
// learned.
func TestPubSubLearnedMetaDataFields(t *testing.T) {
	d := testDecoder(t, pubsubFilterFlags{})
	for _, c := range []struct {
		fields int
		kept   bool
	}{{0, true}, {1, true}, {maxLearnedFields - 1, true}, {maxLearnedFields, true}, {maxLearnedFields + 1, false}} {
		_, err := d.decode(metaDataMessage(t, c.fields, c.fields), nil)
		kept := d.lookup(testPublisher(c.fields), 0, 1) != nil
		if kept != c.kept || (err == nil) != c.kept {
			t.Errorf("%d fields: kept %v, error %v", c.fields, kept, err)
		}
		if err != nil && !strings.Contains(err.Error(), fmt.Sprintf("has %d fields, more than the %d that are kept", c.fields, maxLearnedFields)) {
			t.Errorf("%d fields: %v", c.fields, err)
		}
	}
}

// --publisher-id and --writer-id apply before DataSetMetaData is learned.
func TestPubSubLearnedMetaDataFiltered(t *testing.T) {
	for _, c := range []struct {
		filter pubsubFilterFlags
		pub    int
		kept   bool
	}{
		{pubsubFilterFlags{publisherID: "u32:7"}, 7, true},
		{pubsubFilterFlags{publisherID: "u32:7"}, 8, false},
		{pubsubFilterFlags{writerID: 1}, 8, true},
		{pubsubFilterFlags{writerID: 2}, 8, false},
	} {
		d := testDecoder(t, c.filter)
		_, err := d.decode(metaDataMessage(t, c.pub, 1), nil)
		if kept := d.lookup(testPublisher(c.pub), 0, 1) != nil; kept != c.kept || (!c.kept && !errors.Is(err, errFiltered)) {
			t.Errorf("%+v, publisher %d: kept %v, error %v", c.filter, c.pub, kept, err)
		}
	}
}

// The prober of --probe probes for at most maxProbePending (1024) writers
// at a time.
func TestPubSubProberBounded(t *testing.T) {
	p := newPubSubProber(nil, message.DiscoveryLayoutSpec, nil, func(message.PublisherID, uint16) bool { return false })
	for i := 0; i < 1024+100; i++ {
		p.want(testPublisher(i), 1)
	}
	if n := len(p.pending); n != 1024 {
		t.Errorf("%d writers pending, want 1024", n)
	}
}

// A writer is probed for while its messages arrive: one not seen for
// probeExpiry is forgotten, as is one whose DataSetMetaData arrived.
func TestPubSubProberExpiry(t *testing.T) {
	answered := testPublisher(2)
	p := newPubSubProber(nil, message.DiscoveryLayoutSpec, nil, func(pub message.PublisherID, _ uint16) bool {
		return pub == answered
	})
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	now := t0
	p.now = func() time.Time { return now }
	for i := 0; i < 3; i++ {
		p.want(testPublisher(i), 1)
	}
	p.want(testPublisher(9), 0) // no DataSetWriterId: nothing to probe for
	if len(p.pending) != 3 {
		t.Fatalf("%d pending, want 3", len(p.pending))
	}
	due, _ := p.due(t0.Add(probeDelayMax))
	if len(due) != 2 || len(p.pending) != 2 {
		t.Fatalf("%d due, %d pending, want the 2 not answered", len(due), len(p.pending))
	}

	// Writer 0 is seen again just before it would expire, writer 1 is not.
	now = t0.Add(probeExpiry)
	p.want(testPublisher(0), 1)
	for _, c := range []struct {
		at      time.Duration
		pending int
	}{
		{probeExpiry, 2},
		{probeExpiry + time.Nanosecond, 1},
		{2 * probeExpiry, 1},
		{2*probeExpiry + time.Nanosecond, 0},
	} {
		p.due(t0.Add(c.at))
		if len(p.pending) != c.pending {
			t.Errorf("at %v: %d pending, want %d", c.at, len(p.pending), c.pending)
		}
	}

	// Room for a new writer once others expired.
	for i := 0; i < maxProbePending; i++ {
		p.want(testPublisher(100+i), 1)
	}
	now = now.Add(2 * probeExpiry)
	p.want(testPublisher(1), 1)
	if _, ok := p.pending[metaKey(testPublisher(1), 1)]; ok {
		t.Error("a writer was added beyond maxProbePending")
	}
	p.due(now.Add(time.Nanosecond))
	p.want(testPublisher(1), 1)
	if len(p.pending) != 1 {
		t.Errorf("%d pending after the others expired, want 1", len(p.pending))
	}
}
