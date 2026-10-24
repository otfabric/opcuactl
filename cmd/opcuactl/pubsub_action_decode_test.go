// SPDX-License-Identifier: MIT

package opcuactl

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/otfabric/go-opcua/pubsub/jsonmsg"
	"github.com/otfabric/go-opcua/pubsub/message"
	"github.com/otfabric/go-opcua/pubsub/uadp"
	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/opcuactl/internal/model"
)

// actionMessages returns an ActionRequest and an ActionResponse
// NetworkMessage of Responder 1, writer 100, target 7 as the go-opcua
// runtime sends them.
func actionMessages() (req, resp *message.NetworkMessage) {
	requestor := message.StringPublisherID("requestor-2")
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	field := func(name string, v any) *message.Field {
		return &message.Field{Name: name, Value: &ua.DataValue{EncodingMask: ua.DataValueValue, Value: ua.MustVariant(v)}}
	}
	req = &message.NetworkMessage{
		Type: message.NetworkMessageDataSet, PublisherID: message.NewPublisherID(message.PublisherIDUInt16, 1), Timestamp: now,
		UADPMask: ua.UADPNetworkMessageContentMaskPublisherID | ua.UADPNetworkMessageContentMaskPayloadHeader,
		JSONMask: ua.JSONNetworkMessageContentMaskNetworkMessageHeader | ua.JSONNetworkMessageContentMaskDataSetMessageHeader |
			ua.JSONNetworkMessageContentMaskPublisherID,
		MessageID: "req-1",
		Action: &message.ActionHeader{Type: message.ActionRequest, ResponseAddress: "opcua/json/action-response/1/Actions",
			CorrelationData: []byte{0xca, 0xfe}, RequestorID: &requestor, TimeoutHint: 5000},
		Messages: []*message.DataSetMessage{{
			DataSetWriterID: 100, Type: message.DataSetMessageActionRequest, Valid: true,
			JSONMask:  ua.JSONDataSetMessageContentMaskTimestamp | ua.JSONDataSetMessageContentMaskMessageType,
			Timestamp: now, ActionTargetID: 7, RequestID: 3, ActionState: ua.ActionStateExecuting,
			Fields: []*message.Field{field("A", int32(2)), field("B", int32(3))},
		}},
	}
	resp = &message.NetworkMessage{
		Type: message.NetworkMessageDataSet, PublisherID: message.NewPublisherID(message.PublisherIDUInt16, 1), Timestamp: now,
		UADPMask: ua.UADPNetworkMessageContentMaskPublisherID | ua.UADPNetworkMessageContentMaskPayloadHeader |
			ua.UADPNetworkMessageContentMaskGroupHeader | ua.UADPNetworkMessageContentMaskWriterGroupID,
		GroupHeader: message.GroupHeader{WriterGroupID: 1},
		MessageID:   "resp-1",
		Action:      &message.ActionHeader{Type: message.ActionResponse, CorrelationData: []byte{0xca, 0xfe}, RequestorID: &requestor},
		Messages: []*message.DataSetMessage{{
			DataSetWriterID: 100, Type: message.DataSetMessageActionResponse, Valid: true,
			UADPMask:  ua.UADPDataSetMessageContentMaskTimestamp | ua.UADPDataSetMessageContentMaskStatus,
			JSONMask:  ua.JSONDataSetMessageContentMaskTimestamp | ua.JSONDataSetMessageContentMaskMessageType,
			Timestamp: now, Status: ua.StatusBadTypeMismatch, ActionTargetID: 7, RequestID: 3, ActionState: ua.ActionStateDone,
			Fields: []*message.Field{field("Sum", int32(5))},
		}},
	}
	return req, resp
}

func TestPubSubDecodeActions(t *testing.T) {
	req, resp := actionMessages()
	dir := t.TempDir()
	write := func(name string, b []byte, err error) string {
		t.Helper()
		if err != nil {
			t.Fatalf("encoding %s: %v", name, err)
		}
		file := filepath.Join(dir, name)
		if err := os.WriteFile(file, b, 0o600); err != nil {
			t.Fatal(err)
		}
		return file
	}
	files := map[string][2]string{}
	b, err := uadp.Encode(req, nil)
	ureq := write("req.bin", b, err)
	b, err = uadp.Encode(resp, nil)
	uresp := write("resp.bin", b, err)
	files["uadp"] = [2]string{ureq, uresp}
	b, err = jsonmsg.Encode(req, nil)
	jreq := write("req.json", b, err)
	b, err = jsonmsg.Encode(resp, nil)
	jresp := write("resp.json", b, err)
	files["json"] = [2]string{jreq, jresp}

	for enc, f := range files {
		t.Run(enc, func(t *testing.T) {
			decoded := func(file string) model.PubSubDataSet {
				t.Helper()
				out := mustRun(t, "pubsub", "decode", file, "--format", "json")
				var ds []model.PubSubDataSet
				if err := json.Unmarshal([]byte(out), &ds); err != nil || len(ds) != 1 {
					t.Fatalf("%s: %v\n%s", file, err, out)
				}
				return ds[0]
			}
			r := decoded(f[0])
			if r.MessageType != "action-request" || r.Encoding != enc || r.PublisherID != "1" || r.DataSetWriterID != 100 || r.Action == nil {
				t.Fatalf("request: %+v", r)
			}
			want := model.PubSubAction{Type: "request", ActionTargetID: 7, RequestID: 3, ActionState: "Executing", RequestorID: "requestor-2",
				ResponseAddress: "opcua/json/action-response/1/Actions", CorrelationData: "cafe", TimeoutHint: 5000}
			if *r.Action != want {
				t.Errorf("request action = %+v, want %+v", *r.Action, want)
			}
			if r.Status != "" || len(r.Fields) != 2 || r.Fields[0].Value != float64(2) || r.Fields[1].Value != float64(3) {
				t.Errorf("request fields: %+v status %q", r.Fields, r.Status)
			}
			s := decoded(f[1])
			if s.MessageType != "action-response" || s.Status != "BadTypeMismatch" || s.Action == nil ||
				s.Action.Type != "response" || s.Action.ActionState != "Done" || s.Action.RequestID != 3 || s.Action.CorrelationData != "cafe" {
				t.Errorf("response: %+v %+v", s, s.Action)
			}
			if len(s.Fields) != 1 || s.Fields[0].Value != float64(5) {
				t.Errorf("response fields: %+v", s.Fields)
			}

			// The table form.
			out := mustRun(t, "pubsub", "decode", f[0], "--field", "A:Int32", "--field", "B:Int32")
			wantContains(t, out, "pub=1 writer=100 seq=0 action-request target=7 request=3 state=Executing requestor=requestor-2 timeout=5000ms",
				"A=2 B=3")
			out = mustRun(t, "pubsub", "decode", f[1])
			wantContains(t, out, "action-response target=7 request=3 state=Done requestor=requestor-2 status=BadTypeMismatch", "=5")
		})
	}
}
