// SPDX-License-Identifier: MIT

package opcuactl

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/otfabric/go-opcua/pubsub"
	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/opcuactl/internal/format"
	"github.com/otfabric/opcuactl/internal/model"
	"github.com/otfabric/opcuactl/internal/parse"
	"github.com/spf13/pflag"
)

var pubsubDiagnosticsLevelValues = []string{"basic", "advanced", "info", "log", "debug"}

// Counters of go-opcua's PubSub runtime that Part 14 does not define: the
// chunks of chunked NetworkMessages and the UADP discovery messages.
const (
	ctrChunksSent                 = "ChunksSent"
	ctrChunksReceived             = "ChunksReceived"
	ctrChunkedMessagesReceived    = "ChunkedMessagesReceived"
	ctrChunksDiscarded            = "ChunksDiscarded"
	ctrDiscoveryRequestsSent      = "DiscoveryRequestsSent"
	ctrDiscoveryRequestsReceived  = "DiscoveryRequestsReceived"
	ctrDiscoveryRequestsIgnored   = "DiscoveryRequestsIgnored"
	ctrDiscoveryResponsesSent     = "DiscoveryResponsesSent"
	ctrDiscoveryResponsesReceived = "DiscoveryResponsesReceived"
)

// pubsubCounter is one diagnostics counter.
type pubsubCounter struct {
	name  string
	level ua.DiagnosticsLevel
	error bool
	value atomic.Uint64
	first atomic.Int64 // TimeFirstChange, Unix nanoseconds
}

// pubsubDiagnostics counts what a command sends and receives, with the
// counters of the Part 14 diagnostics object (§9.1.11) of the component the
// command plays (a writer group for publish, a reader group for subscribe)
// and the chunk and discovery counters of go-opcua. All methods are safe
// for concurrent use and do nothing on a nil receiver.
type pubsubDiagnostics struct {
	component string
	counters  []*pubsubCounter
	byName    map[string]*pubsubCounter
	// refresh, if set, updates the counters before a snapshot (counters
	// kept by go-opcua's runtime).
	refresh func(d *pubsubDiagnostics)
}

func newPubSubDiagnostics(component string, std []pubsub.DiagnosticsCounterID, extra ...string) *pubsubDiagnostics {
	d := &pubsubDiagnostics{component: component, byName: map[string]*pubsubCounter{}}
	for _, id := range std {
		d.addCounter(&pubsubCounter{name: id.String(), level: id.Level(),
			error: id.Classification() == ua.PubSubDiagnosticsCounterClassificationError})
	}
	for _, name := range extra {
		d.addCounter(&pubsubCounter{name: name, level: ua.DiagnosticsLevelBasic,
			error: name == ctrChunksDiscarded || name == ctrActionRequestsRejected || name == ctrActionTimeouts})
	}
	return d
}

func (d *pubsubDiagnostics) addCounter(c *pubsubCounter) {
	d.counters = append(d.counters, c)
	d.byName[c.name] = c
}

// add adds n to a counter.
func (d *pubsubDiagnostics) add(name string, n int) {
	if d == nil || n <= 0 {
		return
	}
	c := d.byName[name]
	if c == nil {
		return
	}
	c.first.CompareAndSwap(0, time.Now().UnixNano())
	c.value.Add(uint64(n))
}

// set sets a counter to v.
func (d *pubsubDiagnostics) set(name string, v uint64) {
	if d == nil {
		return
	}
	c := d.byName[name]
	if c == nil {
		return
	}
	if v > 0 {
		c.first.CompareAndSwap(0, time.Now().UnixNano())
	}
	c.value.Store(v)
}

// value returns the value of a counter.
func (d *pubsubDiagnostics) value(name string) uint64 {
	if d == nil || d.byName[name] == nil {
		return 0
	}
	return d.byName[name].value.Load()
}

// snapshot returns the counters up to level; the others are not active.
func (d *pubsubDiagnostics) snapshot(level ua.DiagnosticsLevel) model.PubSubDiagnostics {
	if d.refresh != nil {
		d.refresh(d)
	}
	out := model.PubSubDiagnostics{Component: d.component, Level: diagnosticsLevelName(level), Counters: []model.PubSubCounter{}}
	for _, c := range d.counters {
		if c.level > level {
			continue
		}
		v := c.value.Load()
		m := model.PubSubCounter{
			Name: c.name, Value: v, Classification: "Information", Level: diagnosticsLevelName(c.level), Active: true,
		}
		if c.error {
			m.Classification = "Error"
			out.TotalError += v
		} else {
			out.TotalInformation += v
		}
		if ns := c.first.Load(); ns != 0 {
			m.TimeFirstChange = model.FormatTimestamp(time.Unix(0, ns).UTC())
		}
		out.Counters = append(out.Counters, m)
	}
	return out
}

func diagnosticsLevelName(l ua.DiagnosticsLevel) string {
	return strings.TrimPrefix(l.String(), "DiagnosticsLevel")
}

// pubsubDiagnosticsFlags select the diagnostics a command prints.
type pubsubDiagnosticsFlags struct {
	enabled  bool
	interval time.Duration
	level    string

	parsed ua.DiagnosticsLevel
}

func (f *pubsubDiagnosticsFlags) register(fs *pflag.FlagSet) {
	fs.BoolVar(&f.enabled, "diagnostics", false, "Print the PubSub diagnostics counters (Part 14 §9.1.11) on stderr when done")
	fs.DurationVar(&f.interval, "diagnostics-interval", 0, "Also print the diagnostics counters at this interval (implies --diagnostics)")
	fs.StringVar(&f.level, "diagnostics-level", "basic", "DiagnosticsLevel of the counters shown (basic|advanced|info|log|debug)")
}

// validate parses the level.
func (f *pubsubDiagnosticsFlags) validate() error {
	l, err := parse.DiagnosticsLevel(f.level)
	if err != nil {
		return exitCode(exitUsage, err)
	}
	if f.interval < 0 {
		return exitCodef(exitUsage, "--diagnostics-interval must not be negative")
	}
	f.parsed = l
	if f.interval > 0 {
		f.enabled = true
	}
	return nil
}

// start prints d every interval until the returned function is called.
func (f *pubsubDiagnosticsFlags) start(d *pubsubDiagnostics) (stop func()) {
	if f.interval <= 0 {
		return func() {}
	}
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		t := time.NewTicker(f.interval)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				f.print(d)
			}
		}
	}()
	return func() {
		close(done)
		<-finished
	}
}

// print writes the diagnostics on stderr: a JSON line with --format json or
// yaml, a block of text otherwise.
func (f *pubsubDiagnosticsFlags) print(d *pubsubDiagnostics) {
	if !f.enabled || d == nil {
		return
	}
	writeDiagnostics(os.Stderr, d.snapshot(f.parsed), format.ParseFormat(outputFormat) != format.Table)
}

func writeDiagnostics(w io.Writer, m model.PubSubDiagnostics, asJSON bool) {
	if asJSON {
		b, _ := json.Marshal(map[string]interface{}{"diagnostics": m})
		_, _ = fmt.Fprintln(w, string(b))
		return
	}
	_, _ = fmt.Fprintf(w, "diagnostics %s (level %s): information=%d error=%d\n", m.Component, m.Level, m.TotalInformation, m.TotalError)
	width := 0
	for _, c := range m.Counters {
		width = max(width, len(c.Name))
	}
	for _, c := range m.Counters {
		mark := ""
		if c.Classification == "Error" {
			mark = " (error)"
		}
		_, _ = fmt.Fprintf(w, "  %-*s %d%s\n", width, c.Name, c.Value, mark)
	}
}
