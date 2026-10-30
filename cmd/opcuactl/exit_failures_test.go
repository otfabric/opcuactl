// SPDX-License-Identifier: MIT

package opcuactl

import (
	"encoding/binary"
	"io"
	"math"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// deadEndpoint and deadBroker are ports nothing listens on.
const (
	deadEndpoint = "opc.tcp://127.0.0.1:1"
	deadBroker   = "mqtt://127.0.0.1:1"
	datagramURL  = "opc.udp://224.0.0.22:4840"
)

// Every failure before a server answers has the exit code of its kind: the
// command line, a file given on it, or the transport. want is part of the
// message, so that a row cannot pass on an unknown flag.
func TestExitCodesWithoutServer(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing")
	garbage := filepath.Join(dir, "garbage")
	if err := os.WriteFile(garbage, []byte{0xff, 0xff}, 0o600); err != nil {
		t.Fatal(err)
	}
	e, dead := "-e", deadEndpoint
	for _, c := range []struct {
		name string
		args []string
		code int
		want string
	}{
		{"get, no node", []string{"get", "value", e, dead}, exitUsage, "--node"},
		{"get, bad node", []string{"get", "value", e, dead, "-n", "!"}, exitUsage, "invalid node ID"},
		{"get, bad timestamps", []string{"get", "value", e, dead, "-n", "i=1", "--timestamps-to-return", "x"}, exitUsage, "timestamps"},
		{"monitor, no node", []string{"monitor", "value", e, dead}, exitUsage, "--node"},
		{"monitor, bad node", []string{"monitor", "value", e, dead, "-n", "!"}, exitUsage, "invalid node ID"},
		{"monitor, bad timestamps", []string{"monitor", "value", e, dead, "-n", "i=1", "--timestamps", "x"}, exitUsage, "timestamps"},
		{"alarms watch, dead", []string{"alarms", "watch", e, dead}, exitTransport, "refused"},
		{"alarms audit, dead", []string{"alarms", "audit", e, dead}, exitTransport, "refused"},
		{"monitor, dead", []string{"monitor", "value", e, dead, "-n", "i=2258"}, exitTransport, "refused"},
		{"discover servers, no endpoint", []string{"discover", "servers"}, exitUsage, "--endpoint"},
		{"discover servers, dead", []string{"discover", "servers", e, dead}, exitTransport, "refused"},
		{"discover on network, dead", []string{"discover", "servers", e, dead, "--on-network"}, exitTransport, "refused"},
		{"discover lds, dead", []string{"discover", "lds", e, dead}, exitTransport, "refused"},
		{"discover endpoints, no endpoint", []string{"discover", "endpoints"}, exitUsage, "--endpoint"},
		{"discover endpoints, dead", []string{"discover", "endpoints", e, dead}, exitTransport, "refused"},
		{"config show, dead", []string{"pubsub", "config", "show", e, dead}, exitTransport, "refused"},
		{"add-connection, bad encoding", []string{"pubsub", "config", "add-connection", "--name", "c", "--url", datagramURL, "--encoding", "x"}, exitUsage, "encoding"},
		{"add-action, bad target", []string{"pubsub", "config", "add-action", "--name", "n", "--target", "x"}, exitUsage, "--target"},
		{"add-action, method twice", []string{"pubsub", "config", "add-action", "--name", "n", "--target", "1:a", "--method", "1=i=1@i=2", "--method", "1=i=1@i=2"}, exitUsage, "twice"},
		{"decode, no metadata file", []string{"pubsub", "decode", garbage, "--metadata", missing}, exitInvalidInput, "missing"},
		{"decode, bad metadata node", []string{"pubsub", "decode", garbage, "--metadata-node", "!"}, exitUsage, "node"},
		{"decode, metadata node, dead", []string{"pubsub", "decode", garbage, "--metadata-node", "i=1", e, dead}, exitTransport, "refused"},
		{"decode, two garbage files", []string{"pubsub", "decode", garbage, garbage}, exitProtocol, "decoding"},
		{"publish, bad mode", []string{"pubsub", "publish", "--message-security", "x"}, exitUsage, "message security"},
		{"publish, dead broker", []string{"pubsub", "publish", "--url", deadBroker, "--topic", "t", "--field", "A:Int32=1"}, exitTimeout, "connect"},
		{"publish, bad URL", []string{"pubsub", "publish", "--field", "A:Int32=1", "--url", "nonsense"}, exitUsage, "nonsense"},
		{"publish, probes over JSON", []string{"pubsub", "publish", "--url", datagramURL, "--field", "A:Int32=1", "--encoding", "json", "--answer-probes"}, exitUsage, "UADP"},
		{"subscribe, discovery over a broker", []string{"pubsub", "subscribe", "--url", datagramURL, "--probe", "--discovery-url", deadBroker}, exitUsage, "--discovery-url"},
		{"subscribe, bad discovery address", []string{"pubsub", "subscribe", "--url", datagramURL, "--probe", "--discovery-url", "opc.udp://256.0.0.1:1"}, exitTransport, "discovery address"},
		{"subscribe, bad mode", []string{"pubsub", "subscribe", "--message-security", "x"}, exitUsage, "message security"},
		{"discover, bad mode", []string{"pubsub", "discover", "--url", datagramURL, "--probe", "applications", "--message-security", "x"}, exitUsage, "message security"},
		{"call, bad mode", []string{"pubsub", "call", "--url", datagramURL, "--message-security", "x"}, exitUsage, "message security"},
		{"call, dead broker", []string{"pubsub", "call", "--url", deadBroker, "--request-address", "t", "--publisher-id", "7", "--writer-id", "1", "--input", "A:Int32=1"}, exitTransport, ""},
		{"gds request, no key file", []string{"gds", "pull", "request", "ns=1;i=1", e, dead, "--private-key", missing}, exitInvalidInput, "missing"},
		{"gds enroll, no key file", []string{"gds", "pull", "enroll", e, dead, "--uri", "urn:x", "--private-key", missing, "--cert-out", filepath.Join(dir, "cert")}, exitInvalidInput, "missing"},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, _, err := runCLI(t, append(c.args, "--timeout", "500ms")...)
			if got := ExitCode(err); got != c.code {
				t.Errorf("exit %d, want %d (%v)", got, c.code, err)
			}
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("error %v, want %q", err, c.want)
			}
		})
	}
}

// cutProxy forwards a connection to target until the client has sent frames
// UA messages, drops the next one and closes. It stands for a server lost
// after the session is up. It returns the endpoint to use.
func cutProxy(t *testing.T, target string, frames int) string {
	t.Helper()
	return proxy(t, target, frames, 0)
}

// timedCutProxy forwards every message and closes after d: it stands for a
// server lost while a stream sits idle, when no message count would be reached.
func timedCutProxy(t *testing.T, target string, d time.Duration) string {
	t.Helper()
	return proxy(t, target, math.MaxInt, d)
}

func proxy(t *testing.T, target string, frames int, after time.Duration) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = c.Close() }()
				up, err := net.Dial("tcp", strings.TrimPrefix(target, "opc.tcp://"))
				if err != nil {
					return
				}
				defer func() { _ = up.Close() }()
				go func() { _, _ = io.Copy(c, up) }()
				if after > 0 {
					defer time.AfterFunc(after, func() { _ = c.Close() }).Stop()
				}
				for i := 0; i < frames; i++ {
					hdr := make([]byte, 8) // type, final, size
					if _, err := io.ReadFull(c, hdr); err != nil {
						return
					}
					body := make([]byte, binary.LittleEndian.Uint32(hdr[4:])-8)
					if _, err := io.ReadFull(c, body); err != nil {
						return
					}
					msg := append(hdr, body...)
					if i == 0 {
						msg = helloTo(msg, target)
					}
					if _, err := up.Write(msg); err != nil {
						return
					}
				}
			}()
		}
	}()
	return "opc.tcp://" + ln.Addr().String()
}

// helloTo returns the Hello message with its EndpointUrl replaced by url: the
// server refuses another address than its own.
func helloTo(msg []byte, url string) []byte {
	const fields = 8 + 20 // the header, and five UInt32
	out := append([]byte{}, msg[:fields]...)
	out = binary.LittleEndian.AppendUint32(out, uint32(len(url)))
	out = append(out, url...)
	binary.LittleEndian.PutUint32(out[4:], uint32(len(out)))
	return out
}

// A server lost after the session is up fails the operation, which reports
// what it was doing and exits with the code of a transport failure.
func TestExitCodesServerLost(t *testing.T) {
	s := startServer(t)
	payload := filepath.Join(t.TempDir(), "payload")
	if err := os.WriteFile(payload, []byte{0xff}, 0o600); err != nil {
		t.Fatal(err)
	}
	// Connecting takes six messages; later is after the first operation.
	ep, later := cutProxy(t, s.url, 6), cutProxy(t, s.url, 7)
	node := "i=2258"
	from, to := "--start=2026-01-01T00:00:00Z", "--end=2026-01-02T00:00:00Z"
	for _, c := range []struct {
		args []string
		want string
		ep   string
	}{
		{[]string{"get", "value", "-n", node}, "reading value", ep},
		{[]string{"get", "server"}, "getting server status", ep},
		{[]string{"get", "attr", "-n", node, "--all"}, "reading attributes", ep},
		{[]string{"get", "vars", "-n", "i=2253"}, "browsing variables", ep},
		{[]string{"find", "namespace"}, "reading namespace array", ep},
		{[]string{"export", "nodes", "-n", "i=2253"}, "exporting nodes", ep},
		{[]string{"export", "values", "-n", "i=2253"}, "reading values", ep},
		{[]string{"history", "value", "-n", node, from, to, "--mode", "modified"}, "reading modified history", ep},
		{[]string{"history", "value", "-n", node, from, to, "--mode", "processed", "--interval", "1s"}, "reading processed history", ep},
		{[]string{"history", "value", "-n", node, "--mode", "attime", "--at", "2026-01-01T00:00:00Z"}, "reading at-time history", ep},
		{[]string{"history", "event", "-n", node, from, to}, "reading event history", ep},
		{[]string{"monitor", "value", "-n", node}, "starting subscription", ep},
		{[]string{"write", "value", "-n", node, "--value", "1", "--yes"}, "writing value", ep},
		{[]string{"call", "method", "--object", "i=2253", "--method", "i=11492"}, "calling method", ep},
		{[]string{"alarms", "watch"}, "starting subscription", ep},
		{[]string{"ping"}, "", ep},
		{[]string{"history", "update-events", "-n", node, "--event", `{"Message":"x"}`, "--yes"}, "updating the event history", ep},
		{[]string{"history", "delete-events", "-n", node, "--event-id", "0x0a01", "--yes"}, "deleting from the event history", ep},
		{[]string{"alarms", "metrics", "get", "i=2253"}, "reading the alarm metrics", ep},
		{[]string{"export", "values", "--start-node", "i=2253"}, "discovering variables", ep},
		{[]string{"pubsub", "decode", payload, "--metadata-node", "i=2253"}, "DataSetMetaData", ep},
		{[]string{"monitor", "value", "-n", node}, "creating monitored items", later},
		{[]string{"get", "vars", "-n", "i=2253"}, "batch reading", later},
		{[]string{"find", "node", "--name", "x"}, "walk", later},
		{[]string{"find", "type", "--data-type", "double"}, "walk", later},
		{[]string{"alarms", "list"}, "ConditionRefresh", cutProxy(t, s.url, 9)},
	} {
		t.Run(strings.Join(c.args, " "), func(t *testing.T) {
			_, stderr, err := runCLI(t, append(c.args, "-e", c.ep, "--timeout", "1s")...)
			if err == nil {
				t.Fatal("no error")
			}
			msg := stderr + err.Error()
			if !strings.Contains(msg, c.want) {
				t.Errorf("%q, want %q", msg, c.want)
			}
			if got := ExitCode(err); got != exitTransport {
				t.Errorf("exit %d, want %d (%v)", got, exitTransport, err)
			}
		})
	}
}

// A stream ends when its connection does, with what arrived before it, and
// without reconnecting: the proxy would take a new connection.
func TestAlarmsWatchConnectionLost(t *testing.T) {
	s := startServer(t)
	stdout, stderr, err := runCLI(t, "alarms", "watch", "-e", cutProxy(t, s.url, 10), "--timeout", "1s")
	if got := ExitCode(err); got != exitTransport {
		t.Errorf("exit %d, want %d (%v)", got, exitTransport, err)
	}
	if !strings.Contains(stderr, "connection lost") {
		t.Errorf("stderr %q", stderr)
	}
	if !strings.Contains(stdout, "-- refresh end --") {
		t.Errorf("stdout %q", stdout)
	}
}

// The monitor commands end like alarms watch: with what arrived before the
// loss, exit 4, no reconnect and no hang.
func TestMonitorConnectionLost(t *testing.T) {
	s := startServer(t)
	const node = "i=2258"
	for _, c := range []struct {
		args   []string
		want   string
		stdout string
	}{
		{[]string{"monitor", "value", "-n", node}, "monitoring i=2258: connection lost", node},
		{[]string{"monitor", "value", "-n", node, "-n", "i=2259"}, "monitoring i=2258, i=2259: connection lost", node},
		{[]string{"monitor", "event", "-n", "i=2253"}, "monitoring events on i=2253: connection lost", ""},
		{[]string{"monitor", "alarm", "-n", "i=2253"}, "monitoring events on i=2253: connection lost", ""},
	} {
		t.Run(strings.Join(c.args, " "), func(t *testing.T) {
			stdout, stderr, err := runCLI(t, append(c.args, "-e", timedCutProxy(t, s.url, time.Second), "--timeout", "1s")...)
			if got := ExitCode(err); got != exitTransport {
				t.Errorf("exit %d, want %d (%v)", got, exitTransport, err)
			}
			if !strings.Contains(stderr, c.want) {
				t.Errorf("stderr %q, want %q", stderr, c.want)
			}
			if !strings.Contains(stdout, c.stdout) {
				t.Errorf("stdout %q, want %q", stdout, c.stdout)
			}
		})
	}
}

// Some command lines are only judged once connected.
func TestExitCodesWithServer(t *testing.T) {
	s := startServer(t)
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"browse", "refs", "--node-class", "x"}, "node class"},
		{[]string{"diagnostics", "subscriptions", "--session", "!"}, "--session"},
	} {
		t.Run(strings.Join(c.args, " "), func(t *testing.T) {
			_, _, err := runCLI(t, append(c.args, "-e", s.url)...)
			if got := ExitCode(err); got != exitUsage {
				t.Errorf("exit %d, want %d (%v)", got, exitUsage, err)
			}
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("error %v, want %q", err, c.want)
			}
		})
	}
}
