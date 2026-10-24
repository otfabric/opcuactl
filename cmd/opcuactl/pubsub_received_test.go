// SPDX-License-Identifier: MIT

package opcuactl

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-opcua/pubsub"
	"github.com/otfabric/go-opcua/pubsub/transport"
	"github.com/otfabric/opcuactl/internal/model"
	"github.com/otfabric/opcuactl/internal/parse"
)

func TestEncodingOf(t *testing.T) {
	for _, tc := range []struct {
		payload, ct string
		want        parse.PubSubEncoding
	}{
		{`{"a":1}`, "", parse.EncodingJSON},
		{"\x91", "", parse.EncodingUADP},
		// The content type wins over the payload.
		{`{"a":1}`, "application/opcua+uadp", parse.EncodingUADP},
		{"\x91", "application/json; charset=utf-8", parse.EncodingJSON},
		{"\x91", "Application/JSON", parse.EncodingJSON},
		// Unknown content types fall back to the payload.
		{`{"a":1}`, "text/plain", parse.EncodingJSON},
	} {
		if got := encodingOf([]byte(tc.payload), &transport.Message{ContentType: tc.ct}); got != tc.want {
			t.Errorf("encodingOf(%q, %q) = %s; want %s", tc.payload, tc.ct, got, tc.want)
		}
	}
	if got := encodingOf([]byte("{"), nil); got != parse.EncodingJSON {
		t.Errorf("encodingOf without a message = %s", got)
	}
}

func TestGunzipJSON(t *testing.T) {
	msg := []byte(`{"MessageId":"1"}`)
	gz := gzipJSON(msg)
	for _, tc := range []struct {
		payload []byte
		ct      string
		gz      bool
		err     bool
	}{
		{gz, transport.ContentTypeJSONGzip, true, false},
		{gz, "", true, false}, // MQTT 3.1.1: no content type
		{gz, transport.ContentTypeJSON, false, false},
		{msg, "", false, false},
		{msg, transport.ContentTypeJSONGzip, true, true},
		{gz[:len(gz)-4], transport.ContentTypeJSONGzip, true, true},
		{gzipJSON(make([]byte, pubsub.DefaultJSONDecompressionLimit+1)), "", true, true},
	} {
		plain, gz, err := gunzipJSON(tc.payload, &transport.Message{ContentType: tc.ct})
		if gz != tc.gz || (err != nil) != tc.err || (gz && !tc.err && string(plain) != string(msg)) {
			t.Errorf("gunzipJSON(%x, %q) = %q, %v, %v", tc.payload[:4], tc.ct, plain, gz, err)
		}
	}
	d := &pubsubDecoder{}
	if _, err := d.decode([]byte("x"), &transport.Message{ContentType: transport.ContentTypeJSONGzip}); err == nil {
		t.Error("corrupt gzip compressed JSON: expected an error")
	}
}

func TestReceivedModel(t *testing.T) {
	if r := receivedModel(nil); r.Received != "" || r.Topic != "" {
		t.Errorf("receivedModel(nil) = %+v", r)
	}
	r := receivedModel(&transport.Message{
		Topic: "plant/a", ContentType: transport.ContentTypeJSON, Received: time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC),
		UserProperties: []transport.Property{{Name: "Plant", Value: "1"}, {Name: transport.UAMessageTypeProperty, Value: "ua-data"}, {Name: "Plant", Value: "2"}},
	})
	if r.ContentType != "application/json" || r.UAMessageType != "ua-data" || len(r.UserProperties) != 2 ||
		r.UserProperties[1] != (model.PubSubProperty{Name: "Plant", Value: "2"}) || r.Received != "2026-10-24T00:00:00Z" {
		t.Errorf("unexpected model %+v", r)
	}
	suffix := receivedSuffix(r)
	if suffix != " topic=plant/a content-type=application/json ua-message-type=ua-data [Plant=1] [Plant=2]" {
		t.Errorf("receivedSuffix = %q", suffix)
	}
	b, _ := json.Marshal(model.PubSubDataSet{PubSubReceived: r})
	for _, want := range []string{`"contentType":"application/json"`, `"uaMessageType":"ua-data"`, `"userProperties":[{"name":"Plant","value":"1"}`, `"topic":"plant/a"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("%s does not contain %s", b, want)
		}
	}
	if b, _ := json.Marshal(model.PubSubDataSet{}); strings.Contains(string(b), "contentType") || strings.Contains(string(b), "userProperties") {
		t.Errorf("empty transport headers are not omitted: %s", b)
	}
}
