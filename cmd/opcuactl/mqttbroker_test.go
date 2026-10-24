// SPDX-License-Identifier: MIT

package opcuactl

import (
	"bufio"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
)

// testBroker is a minimal MQTT 3.1.1 and 5.0 broker for the PubSub tests:
// CONNECT, PUBLISH (QoS 0, 1 and 2, retain), SUBSCRIBE / UNSUBSCRIBE with
// wildcards, PINGREQ and DISCONNECT. Subscribers receive everything at QoS 0;
// 5.0 subscribers get the properties of the PUBLISH (Content Type, User
// Properties, ...), 3.1.1 subscribers the bare message.
type testBroker struct {
	ln net.Listener
	wg sync.WaitGroup

	mu       sync.Mutex
	clients  map[*brokerClient]struct{}
	retained map[string]brokerMessage
}

// brokerMessage is a payload with the encoded MQTT 5.0 properties of its
// PUBLISH (nil from a 3.1.1 client).
type brokerMessage struct {
	payload, props []byte
}

type brokerClient struct {
	conn    net.Conn
	wmu     sync.Mutex
	version byte // 4: 3.1.1, 5: 5.0
	filters []string
}

const mqtt5 = 5

func startBroker(t *testing.T) *testBroker {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("broker listen: %v", err)
	}
	b := &testBroker{ln: ln, clients: map[*brokerClient]struct{}{}, retained: map[string]brokerMessage{}}
	b.wg.Add(1)
	go b.serve()
	t.Cleanup(b.close)
	return b
}

func (b *testBroker) url() string { return "mqtt://" + b.ln.Addr().String() }

func (b *testBroker) close() {
	_ = b.ln.Close()
	b.mu.Lock()
	for c := range b.clients {
		_ = c.conn.Close()
	}
	b.mu.Unlock()
	b.wg.Wait()
}

func (b *testBroker) serve() {
	defer b.wg.Done()
	for {
		conn, err := b.ln.Accept()
		if err != nil {
			return
		}
		c := &brokerClient{conn: conn}
		b.mu.Lock()
		b.clients[c] = struct{}{}
		b.mu.Unlock()
		b.wg.Add(1)
		go func() {
			defer b.wg.Done()
			b.handle(c)
			b.mu.Lock()
			delete(b.clients, c)
			b.mu.Unlock()
			_ = conn.Close()
		}()
	}
}

func (c *brokerClient) write(typ byte, body []byte) {
	pkt := []byte{typ}
	n := len(body)
	for {
		d := byte(n % 128)
		n /= 128
		if n > 0 {
			d |= 0x80
		}
		pkt = append(pkt, d)
		if n == 0 {
			break
		}
	}
	pkt = append(pkt, body...)
	c.wmu.Lock()
	_, _ = c.conn.Write(pkt)
	c.wmu.Unlock()
}

func readRemaining(r *bufio.Reader) (int, error) {
	n, mult := 0, 1
	for i := 0; i < 4; i++ {
		d, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		n += int(d&0x7f) * mult
		if d&0x80 == 0 {
			return n, nil
		}
		mult *= 128
	}
	return 0, errors.New("bad remaining length")
}

func mqttString(b []byte) (string, []byte, bool) {
	if len(b) < 2 {
		return "", nil, false
	}
	n := int(binary.BigEndian.Uint16(b))
	if len(b) < 2+n {
		return "", nil, false
	}
	return string(b[2 : 2+n]), b[2+n:], true
}

// readVarint reads a variable byte integer (the length of MQTT 5.0
// properties) from b.
func readVarint(b []byte) (n, used int, ok bool) {
	mult := 1
	for i := 0; i < 4 && i < len(b); i++ {
		n += int(b[i]&0x7f) * mult
		if b[i]&0x80 == 0 {
			return n, i + 1, true
		}
		mult *= 128
	}
	return 0, 0, false
}

func appendVarint(b []byte, n int) []byte {
	for {
		d := byte(n % 128)
		n /= 128
		if n > 0 {
			d |= 0x80
		}
		b = append(b, d)
		if n == 0 {
			return b
		}
	}
}

// cutProps splits the MQTT 5.0 properties (with their length) off b.
func cutProps(b []byte) (props, rest []byte, ok bool) {
	n, used, ok := readVarint(b)
	if !ok || len(b) < used+n {
		return nil, nil, false
	}
	return b[used : used+n], b[used+n:], true
}

func appendMQTTString(b []byte, s string) []byte {
	b = binary.BigEndian.AppendUint16(b, uint16(len(s)))
	return append(b, s...)
}

func (b *testBroker) handle(c *brokerClient) {
	r := bufio.NewReader(c.conn)
	for {
		h, err := r.ReadByte()
		if err != nil {
			return
		}
		n, err := readRemaining(r)
		if err != nil {
			return
		}
		body := make([]byte, n)
		if _, err := io.ReadFull(r, body); err != nil {
			return
		}
		switch h >> 4 {
		case 1: // CONNECT
			_, rest, ok := mqttString(body)
			if !ok || len(rest) < 1 {
				return
			}
			c.version = rest[0]
			if c.version == mqtt5 {
				c.write(0x20, []byte{0, 0, 0})
			} else {
				c.write(0x20, []byte{0, 0})
			}
		case 3: // PUBLISH
			qos := (h >> 1) & 3
			topic, rest, ok := mqttString(body)
			if !ok {
				return
			}
			var id []byte
			if qos > 0 {
				if len(rest) < 2 {
					return
				}
				id, rest = rest[:2], rest[2:]
			}
			var props []byte
			if c.version == mqtt5 {
				if props, rest, ok = cutProps(rest); !ok {
					return
				}
			}
			switch qos {
			case 1:
				c.write(0x40, id) // PUBACK
			case 2:
				c.write(0x50, id) // PUBREC; PUBCOMP follows the PUBREL
			}
			b.publish(topic, brokerMessage{payload: rest, props: props}, h&1 == 1)
		case 6: // PUBREL
			if len(body) < 2 {
				return
			}
			c.write(0x70, body[:2]) // PUBCOMP
		case 8: // SUBSCRIBE
			if len(body) < 2 {
				return
			}
			id, rest := body[:2], body[2:]
			ack := append([]byte{}, id...)
			if c.version == mqtt5 {
				var ok bool
				if _, rest, ok = cutProps(rest); !ok {
					return
				}
				ack = append(ack, 0)
			}
			var filters []string
			for len(rest) > 0 {
				f, r2, ok := mqttString(rest)
				if !ok || len(r2) < 1 {
					return
				}
				filters = append(filters, f)
				rest = r2[1:]
				ack = append(ack, 0)
			}
			b.mu.Lock()
			c.filters = append(c.filters, filters...)
			retained := map[string]brokerMessage{}
			for topic, m := range b.retained {
				for _, f := range filters {
					if topicMatches(f, topic) {
						retained[topic] = m
						break
					}
				}
			}
			b.mu.Unlock()
			c.write(0x90, ack)
			for topic, m := range retained {
				c.write(0x31, c.publishBody(topic, m))
			}
		case 10: // UNSUBSCRIBE
			if len(body) < 2 {
				return
			}
			id, rest := body[:2], body[2:]
			ack := append([]byte{}, id...)
			if c.version == mqtt5 {
				var ok bool
				if _, rest, ok = cutProps(rest); !ok {
					return
				}
				ack = append(ack, 0)
			}
			b.mu.Lock()
			for len(rest) > 0 {
				f, r2, ok := mqttString(rest)
				if !ok {
					break
				}
				rest = r2
				if c.version == mqtt5 {
					ack = append(ack, 0)
				}
				for i, have := range c.filters {
					if have == f {
						c.filters = append(c.filters[:i], c.filters[i+1:]...)
						break
					}
				}
			}
			b.mu.Unlock()
			c.write(0xb0, ack)
		case 12: // PINGREQ
			c.write(0xd0, nil)
		case 14: // DISCONNECT
			return
		}
	}
}

// publishBody is the body of a QoS 0 PUBLISH of m to the client.
func (c *brokerClient) publishBody(topic string, m brokerMessage) []byte {
	body := appendMQTTString(nil, topic)
	if c.version == mqtt5 {
		body = append(appendVarint(body, len(m.props)), m.props...)
	}
	return append(body, m.payload...)
}

func (b *testBroker) publish(topic string, m brokerMessage, retain bool) {
	b.mu.Lock()
	if retain {
		if len(m.payload) == 0 {
			delete(b.retained, topic)
		} else {
			b.retained[topic] = brokerMessage{payload: append([]byte(nil), m.payload...), props: append([]byte(nil), m.props...)}
		}
	}
	var targets []*brokerClient
	for c := range b.clients {
		for _, f := range c.filters {
			if topicMatches(f, topic) {
				targets = append(targets, c)
				break
			}
		}
	}
	b.mu.Unlock()
	for _, c := range targets {
		c.write(0x30, c.publishBody(topic, m))
	}
}

// topicMatches matches an MQTT topic against a filter with + and #.
func topicMatches(filter, topic string) bool {
	fs, ts := strings.Split(filter, "/"), strings.Split(topic, "/")
	for i, f := range fs {
		if f == "#" {
			return true
		}
		if i >= len(ts) {
			return false
		}
		if f != "+" && f != ts[i] {
			return false
		}
	}
	return len(fs) == len(ts)
}

func TestTopicMatches(t *testing.T) {
	for _, tc := range []struct {
		filter, topic string
		want          bool
	}{
		{"#", "a/b", true},
		{"a/#", "a/b/c", true},
		{"a/+", "a/b", true},
		{"a/+", "a/b/c", false},
		{"a/b", "a/b", true},
		{"a/b", "a/c", false},
		{"a/b/c", "a/b", false},
	} {
		if got := topicMatches(tc.filter, tc.topic); got != tc.want {
			t.Errorf("topicMatches(%q, %q) = %v, want %v", tc.filter, tc.topic, got, tc.want)
		}
	}
}
