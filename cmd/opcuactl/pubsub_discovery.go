// SPDX-License-Identifier: MIT

package opcuactl

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net"
	"net/netip"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/otfabric/go-opcua/pubsub/message"
	"github.com/otfabric/go-opcua/pubsub/transport"
	"github.com/otfabric/go-opcua/pubsub/uadp"
	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/opcuactl/internal/model"
	"github.com/otfabric/opcuactl/internal/parse"
	"github.com/spf13/pflag"
)

// UADP discovery (Part 14 §7.2.4.6) as go-opcua's runtime does it: the same
// announcement at most every 500 ms, probes for missing DataSetMetaData
// after a random 100 to 500 ms, repeated at an interval doubling from 500 ms
// up to 8 s.
const (
	discoveryMinInterval = 500 * time.Millisecond
	maxProbeInterval     = 8 * time.Second
	probeDelayMin        = 100 * time.Millisecond
	probeDelayMax        = 500 * time.Millisecond
)

var pubsubDiscoveryLayoutValues = []string{"spec", "dotnet"}

// sharedAddress tells whether a datagram address reaches every peer: a
// multicast group (opc.udp) or an Ethernet address. Publishers listen for
// probes there; a unicast UDP address belongs to a single host.
func sharedAddress(url string) bool {
	scheme, err := transport.Scheme(url)
	if err != nil {
		return false
	}
	switch scheme {
	case "opc.eth":
		return true
	case "opc.udp":
	default:
		return false
	}
	rest := strings.TrimPrefix(url, "opc.udp://")
	if i := strings.IndexAny(rest, "/?#"); i >= 0 {
		rest = rest[:i]
	}
	host, _, err := net.SplitHostPort(rest)
	if err != nil {
		return false
	}
	if i := strings.IndexByte(host, '%'); i >= 0 {
		host = host[:i]
	}
	a, err := netip.ParseAddr(host)
	return err == nil && a.IsMulticast()
}

// pubsubDiscoveryFlags configure the discovery channel of subscribe and
// publish.
type pubsubDiscoveryFlags struct {
	url    string
	layout string

	parsed message.DiscoveryLayout
}

func (f *pubsubDiscoveryFlags) register(fs *pflag.FlagSet) {
	fs.StringVar(&f.url, "discovery-url", "", "DiscoveryAddress for UADP discovery probes and announcements (default: --url if it is a multicast group or opc.eth://)")
	fs.StringVar(&f.layout, "discovery-layout", "spec", "Byte layout of the probes sent (spec|dotnet)")
}

// validate checks that discovery can work on the transport of url.
func (f *pubsubDiscoveryFlags) validate(url string, enc parse.PubSubEncoding, what string) error {
	l, err := parse.DiscoveryLayout(f.layout)
	if err != nil {
		return exitCode(1, err)
	}
	f.parsed = l
	scheme, _ := transport.Scheme(url)
	if scheme != "opc.udp" && scheme != "opc.eth" {
		return exitCodef(1, "%s needs a datagram transport (opc.udp://, opc.eth://): UADP discovery does not run over brokers", what)
	}
	if enc == parse.EncodingJSON {
		return exitCodef(1, "%s needs the UADP encoding", what)
	}
	if f.url == "" && !sharedAddress(url) {
		return exitCodef(1, "%s needs a multicast --url or a --discovery-url: a unicast address reaches a single host", what)
	}
	return nil
}

// open returns the discovery channel: a connection to --discovery-url, or
// conn. close closes what open opened.
func (f *pubsubDiscoveryFlags) open(ctx context.Context, tf *pubsubTransportFlags, conn transport.Connection) (dc transport.Connection, closeFn func(), err error) {
	if f.url == "" {
		return conn, func() {}, nil
	}
	cfg, err := tf.config(parse.EncodingUADP)
	if err != nil {
		return nil, nil, err
	}
	if s, _ := transport.Scheme(f.url); s != "opc.udp" && s != "opc.eth" {
		return nil, nil, exitCodef(1, "--discovery-url must be opc.udp:// or opc.eth://")
	}
	cfg.URL = f.url
	openCtx, cancel := connCfg.TimeoutContext(ctx)
	defer cancel()
	dc, err = transport.Open(openCtx, cfg)
	if err != nil {
		return nil, nil, exitCodef(2, "opening the discovery address %s: %v", f.url, transportError(f.url, err))
	}
	return dc, func() { _ = dc.Close() }, nil
}

// ---------------------------------------------------------------------------
// Subscriber: probes for missing DataSetMetaData.

// pubsubProber probes for the DataSetMetaData of the writers a subscriber
// receives messages of but has no DataSetMetaData for.
type pubsubProber struct {
	conn   transport.Connection
	layout message.DiscoveryLayout
	diag   *pubsubDiagnostics
	// known tells whether the DataSetMetaData of a writer arrived.
	known func(p message.PublisherID, writerID uint16) bool

	mu      sync.Mutex
	pending map[string]*probeState
	wake    chan struct{}
}

type probeState struct {
	pub    message.PublisherID
	writer uint16
	next   time.Time
	wait   time.Duration
}

func newPubSubProber(conn transport.Connection, layout message.DiscoveryLayout, diag *pubsubDiagnostics,
	known func(message.PublisherID, uint16) bool) *pubsubProber {
	return &pubsubProber{conn: conn, layout: layout, diag: diag, known: known,
		pending: map[string]*probeState{}, wake: make(chan struct{}, 1)}
}

// want schedules probes for the DataSetMetaData of a writer.
func (p *pubsubProber) want(pub message.PublisherID, writerID uint16) {
	if writerID == 0 {
		return
	}
	k := metaKey(pub, writerID)
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.pending[k]; ok {
		return
	}
	delay := probeDelayMin + rand.N(probeDelayMax-probeDelayMin)
	p.pending[k] = &probeState{pub: pub, writer: writerID, next: time.Now().Add(delay), wait: discoveryMinInterval}
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

// run sends the probes that are due until ctx ends.
func (p *pubsubProber) run(ctx context.Context) {
	t := time.NewTimer(time.Hour)
	defer t.Stop()
	for {
		now := time.Now()
		var due []*probeState
		next := now.Add(time.Hour)
		p.mu.Lock()
		for k, s := range p.pending {
			if p.known(s.pub, s.writer) {
				delete(p.pending, k)
				continue
			}
			if !s.next.After(now) {
				due = append(due, &probeState{pub: s.pub, writer: s.writer})
				s.next = now.Add(s.wait)
				s.wait = min(2*s.wait, maxProbeInterval)
			}
			if s.next.Before(next) {
				next = s.next
			}
		}
		p.mu.Unlock()
		for _, s := range due {
			p.probe(ctx, s.pub, s.writer)
		}
		t.Reset(time.Until(next))
		select {
		case <-ctx.Done():
			return
		case <-p.wake:
			if !t.Stop() {
				<-t.C
			}
		case <-t.C:
		}
	}
}

func (p *pubsubProber) probe(ctx context.Context, pub message.PublisherID, writerID uint16) {
	b, err := uadp.Encode(probeMessage(pub, true, message.DiscoveryRequest{
		ProbeType:        message.ProbePublisherInformation,
		InformationType:  message.InformationDataSetMetaData,
		DataSetWriterIDs: []uint16{writerID},
		Layout:           p.layout,
	}), nil)
	if err == nil {
		sendCtx, cancel := connCfg.TimeoutContext(ctx)
		err = p.conn.Send(sendCtx, b, transport.PublishOptions{})
		cancel()
	}
	if err != nil {
		if verbose && ctx.Err() == nil {
			pubsubInfo("probing for the DataSetMetaData of %s writer %d: %v", pub, writerID, err)
		}
		return
	}
	p.diag.add(ctrDiscoveryRequestsSent, 1)
	if verbose {
		pubsubInfo("probed for the DataSetMetaData of pub=%s writer=%d", pub, writerID)
	}
}

// probeMessage is a discovery probe, for the publisher pub if hasPub.
func probeMessage(pub message.PublisherID, hasPub bool, req message.DiscoveryRequest) *message.NetworkMessage {
	nm := &message.NetworkMessage{Type: message.NetworkMessageDiscoveryRequest, Request: &req}
	if hasPub {
		nm.UADPMask, nm.PublisherID = ua.UADPNetworkMessageContentMaskPublisherID, pub
	}
	return nm
}

// ---------------------------------------------------------------------------
// Publisher: answers to probes.

// discoveryAnswer is an announcement and whether it goes to the data
// address (DataSetMetaData and writer configuration, where the subscribers
// listen) or the discovery channel.
type discoveryAnswer struct {
	d    *message.Discovery
	data bool
}

// discoveryLimiter sends the same announcement at most every
// discoveryMinInterval (Part 14 §7.2.4.6.12.2).
type discoveryLimiter map[string]time.Time

func (l discoveryLimiter) allow(key string, now time.Time) bool {
	if last, ok := l[key]; ok && now.Sub(last) < discoveryMinInterval {
		return false
	}
	l[key] = now
	return true
}

// ---------------------------------------------------------------------------
// Announcements.

// announcementModel describes an announcement other than DataSetMetaData.
func announcementModel(msg *transport.Message, nm *message.NetworkMessage) model.PubSubAnnouncement {
	d := nm.Discovery
	out := model.PubSubAnnouncement{
		PubSubReceived: receivedModel(msg),
		PublisherID:    nm.PublisherID.String(),
		Type:           d.Type.String(),
		SequenceNumber: d.SequenceNumber,
		Details:        map[string]interface{}{},
	}
	if d.StatusCode != ua.StatusOK {
		out.Status = d.StatusCode.Symbol()
	}
	switch d.Type {
	case message.DiscoveryPublisherEndpoints:
		var eps []string
		for _, e := range d.Endpoints {
			if e != nil {
				eps = append(eps, fmt.Sprintf("%s %s %s", e.EndpointURL, policyName(e.SecurityPolicyURI),
					strings.TrimPrefix(e.SecurityMode.String(), "MessageSecurityMode")))
			}
		}
		out.Details["endpoints"] = eps
	case message.DiscoveryDataSetWriterConfig:
		out.Details["dataSetWriterIds"] = d.DataSetWriterIDs
		if g := d.WriterGroup; g != nil {
			out.Details["writerGroup"] = g.Name
			out.Details["writerGroupId"] = g.WriterGroupID
			out.Details["dataSetWriters"] = writerNames(g.DataSetWriters)
		}
	case message.DiscoveryPubSubConnection:
		var conns []string
		for _, c := range d.Connections {
			if c == nil {
				continue
			}
			s := fmt.Sprintf("%s %s", c.Name, c.TransportProfileURI[strings.LastIndex(c.TransportProfileURI, "/")+1:])
			if c.Address != nil {
				if a, ok := c.Address.Value.(*ua.NetworkAddressURLDataType); ok {
					s += " " + a.URL
				}
			}
			for _, g := range c.WriterGroups {
				if g != nil {
					s += fmt.Sprintf(" group %d:%s", g.WriterGroupID, g.Name)
				}
			}
			conns = append(conns, s)
		}
		out.Details["connections"] = conns
	case message.DiscoveryApplicationInformation:
		if a := d.Application; a != nil {
			if a.Description != nil {
				out.Details["applicationUri"] = a.Description.ApplicationURI
				if a.Description.ApplicationName != nil {
					out.Details["applicationName"] = a.Description.ApplicationName.Text
				}
				if len(a.Capabilities) > 0 {
					out.Details["capabilities"] = a.Capabilities
				}
			} else {
				out.Details["state"] = stateName(a.Status)
				out.Details["isCyclic"] = a.IsCyclic
			}
		}
	}
	if len(out.Details) == 0 {
		out.Details = nil
	}
	return out
}

func writerNames(ws []*ua.DataSetWriterDataType) []string {
	var out []string
	for _, w := range ws {
		if w != nil {
			out = append(out, fmt.Sprintf("%d:%s(%s)", w.DataSetWriterID, w.Name, w.DataSetName))
		}
	}
	return out
}

func policyName(uri string) string { return uri[strings.LastIndex(uri, "#")+1:] }

// announcementLine is the table form of an announcement.
func announcementLine(a model.PubSubAnnouncement) string {
	var b strings.Builder
	fmt.Fprintf(&b, "announcement %s pub=%s seq=%d", a.Type, a.PublisherID, a.SequenceNumber)
	if a.Status != "" {
		fmt.Fprintf(&b, " [%s]", a.Status)
	}
	b.WriteString(receivedSuffix(a.PubSubReceived))
	keys := make([]string, 0, len(a.Details))
	for k := range a.Details {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := a.Details[k]
		if l, ok := v.([]string); ok {
			v = strings.Join(l, ", ")
		}
		fmt.Fprintf(&b, " %s=%v", k, v)
	}
	return b.String()
}

// probeWanted tells whether a probe asks for a writer.
func probeWanted(req *message.DiscoveryRequest, writerID uint16) bool {
	return len(req.DataSetWriterIDs) == 0 || slices.Contains(req.DataSetWriterIDs, writerID)
}
