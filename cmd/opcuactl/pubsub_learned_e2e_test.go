// SPDX-License-Identifier: MIT

package opcuactl

import (
	"net"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-opcua/pubsub"
	"github.com/otfabric/go-opcua/pubsub/jsonmsg"
	"github.com/otfabric/go-opcua/pubsub/message"
	"github.com/otfabric/go-opcua/ua"
)

func TestPubSubMaxLearnedMetaDataFlag(t *testing.T) {
	for _, v := range []string{"-1", "0"} {
		_, _, err := runCLI(t, "pubsub", "subscribe", "--url", "opc.udp://127.0.0.1:1", "--max-learned-metadata", v)
		if got := ExitCode(err); got != exitUsage || !strings.Contains(err.Error(), "--max-learned-metadata "+v+": at least 1") {
			t.Errorf("%s: exit %d (%v), want %d", v, got, err, exitUsage)
		}
	}
}

// With --max-learned-metadata 1, the DataSetMetaData of two writers takes
// turns, and stderr says at the end how many were dropped to make room.
func TestPubSubMaxLearnedMetaDataEvictions(t *testing.T) {
	url := freeUDPURL(t)
	conn, err := net.Dial("udp4", strings.TrimPrefix(url, "opc.udp://"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	data, err := jsonmsg.Encode(&message.NetworkMessage{
		JSONMask:    pubsub.DefaultJSONNetworkMessageContentMask,
		PublisherID: testPublisher(1),
		Messages: []*message.DataSetMessage{{
			DataSetWriterID: 1, Valid: true, JSONMask: pubsub.DefaultJSONDataSetMessageContentMask,
			Fields: []*message.Field{{Name: "A", Value: &ua.DataValue{EncodingMask: ua.DataValueValue, Value: ua.MustVariant(int32(1))}}},
		}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Two metadata messages before each DataSet: between the two DataSets
	// that end the command, the two writers took turns at least once.
	round := [][]byte{metaDataMessage(t, 1, 1), metaDataMessage(t, 2, 1), data}
	stop := make(chan struct{})
	sent := make(chan struct{})
	go func() {
		defer close(sent)
		tick := time.NewTicker(5 * time.Millisecond)
		defer tick.Stop()
		for {
			for _, b := range round {
				_, _ = conn.Write(b)
			}
			select {
			case <-stop:
				return
			case <-tick.C:
			}
		}
	}()
	_, stderr, err := runCLI(t, "pubsub", "subscribe", "--url", url, "--max-learned-metadata", "1", "--count", "2", "--duration", "20s")
	close(stop)
	<-sent
	if err != nil {
		t.Fatalf("%v\n%s", err, stderr)
	}
	if !strings.Contains(stderr, " learned DataSetMetaData dropped to make room for others (--max-learned-metadata 1)") {
		t.Errorf("stderr %q", stderr)
	}
}
