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

// testBroker is a minimal MQTT 3.1.1 broker for the PubSub tests: CONNECT,
// PUBLISH (QoS 0 and 1, retain), SUBSCRIBE / UNSUBSCRIBE with wildcards,
// PINGREQ and DISCONNECT. Subscribers receive everything at QoS 0.
type testBroker struct {
	ln net.Listener
	wg sync.WaitGroup

	mu       sync.Mutex
	clients  map[*brokerClient]struct{}
	retained map[string][]byte
}

type brokerClient struct {
	conn    net.Conn
	wmu     sync.Mutex
	filters []string
}

func startBroker(t *testing.T) *testBroker {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("broker listen: %v", err)
	}
	b := &testBroker{ln: ln, clients: map[*brokerClient]struct{}{}, retained: map[string][]byte{}}
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
			c.write(0x20, []byte{0, 0})
		case 3: // PUBLISH
			qos := (h >> 1) & 3
			topic, rest, ok := mqttString(body)
			if !ok {
				return
			}
			if qos > 0 {
				if len(rest) < 2 {
					return
				}
				id := rest[:2]
				rest = rest[2:]
				c.write(0x40, id) // PUBACK (QoS 2 is not used by the tests)
			}
			b.publish(topic, rest, h&1 == 1)
		case 8: // SUBSCRIBE
			if len(body) < 2 {
				return
			}
			id, rest := body[:2], body[2:]
			ack := append([]byte{}, id...)
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
			var retained [][2]string
			for topic, payload := range b.retained {
				for _, f := range filters {
					if topicMatches(f, topic) {
						retained = append(retained, [2]string{topic, string(payload)})
						break
					}
				}
			}
			b.mu.Unlock()
			c.write(0x90, ack)
			for _, m := range retained {
				c.write(0x31, append(appendMQTTString(nil, m[0]), m[1]...))
			}
		case 10: // UNSUBSCRIBE
			if len(body) < 2 {
				return
			}
			id, rest := body[:2], body[2:]
			b.mu.Lock()
			for len(rest) > 0 {
				f, r2, ok := mqttString(rest)
				if !ok {
					break
				}
				rest = r2
				for i, have := range c.filters {
					if have == f {
						c.filters = append(c.filters[:i], c.filters[i+1:]...)
						break
					}
				}
			}
			b.mu.Unlock()
			c.write(0xb0, id)
		case 12: // PINGREQ
			c.write(0xd0, nil)
		case 14: // DISCONNECT
			return
		}
	}
}

func (b *testBroker) publish(topic string, payload []byte, retain bool) {
	b.mu.Lock()
	if retain {
		if len(payload) == 0 {
			delete(b.retained, topic)
		} else {
			b.retained[topic] = append([]byte(nil), payload...)
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
	pkt := append(appendMQTTString(nil, topic), payload...)
	for _, c := range targets {
		c.write(0x30, pkt)
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
