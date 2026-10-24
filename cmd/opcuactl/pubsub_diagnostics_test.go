// SPDX-License-Identifier: MIT

package opcuactl

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-opcua/pubsub"
	"github.com/otfabric/go-opcua/ua"
)

func TestPubSubDiagnostics(t *testing.T) {
	d := newPubSubDiagnostics("reader-group",
		[]pubsub.DiagnosticsCounterID{pubsub.CounterReceivedNetworkMessages, pubsub.CounterDecryptionErrors},
		ctrChunksReceived, ctrChunksDiscarded)
	d.add(pubsub.CounterReceivedNetworkMessages.String(), 3)
	d.add(pubsub.CounterDecryptionErrors.String(), 2)
	d.add(ctrChunksDiscarded, 1)
	d.add(ctrChunksReceived, 0)    // nothing to count
	d.add("NoSuchCounter", 5)      // ignored
	var nilDiag *pubsubDiagnostics // nil-safe
	nilDiag.add(ctrChunksReceived, 1)
	if nilDiag.value(ctrChunksReceived) != 0 || d.value(pubsub.CounterReceivedNetworkMessages.String()) != 3 {
		t.Error("unexpected values")
	}

	basic := d.snapshot(ua.DiagnosticsLevelBasic)
	if basic.Level != "Basic" || len(basic.Counters) != 3 || basic.TotalInformation != 3 || basic.TotalError != 1 {
		t.Errorf("Basic: %+v", basic)
	}
	adv := d.snapshot(ua.DiagnosticsLevelAdvanced)
	if len(adv.Counters) != 4 || adv.TotalError != 3 {
		t.Errorf("Advanced: %+v", adv)
	}
	for _, c := range adv.Counters {
		switch c.Name {
		case "DecryptionErrors":
			if c.Classification != "Error" || c.Level != "Advanced" || c.TimeFirstChange == "" || !c.Active {
				t.Errorf("DecryptionErrors: %+v", c)
			}
		case ctrChunksReceived:
			if c.TimeFirstChange != "" || c.Classification != "Information" {
				t.Errorf("ChunksReceived: %+v", c)
			}
		}
	}

	var text, js bytes.Buffer
	writeDiagnostics(&text, adv, false)
	writeDiagnostics(&js, adv, true)
	for _, want := range []string{"diagnostics reader-group (level Advanced): information=3 error=3", "DecryptionErrors        2 (error)", "ChunksReceived          0"} {
		if !strings.Contains(text.String(), want) {
			t.Errorf("%q does not contain %q", text.String(), want)
		}
	}
	if !strings.HasPrefix(js.String(), `{"diagnostics":{"component":"reader-group","diagnosticsLevel":"Advanced"`) {
		t.Errorf("JSON: %s", js.String())
	}
}

func TestPubSubDiagnosticsFlags(t *testing.T) {
	f := pubsubDiagnosticsFlags{level: "info", interval: time.Millisecond}
	if err := f.validate(); err != nil || !f.enabled || f.parsed != ua.DiagnosticsLevelInfo {
		t.Errorf("validate: %+v, %v", f, err)
	}
	for _, bad := range []pubsubDiagnosticsFlags{{level: "x"}, {level: "basic", interval: -1}} {
		if err := bad.validate(); err == nil {
			t.Errorf("%+v: expected an error", bad)
		}
	}
	// The periodic printer stops cleanly.
	d := newPubSubDiagnostics("writer-group", nil, ctrChunksSent)
	stop := (&pubsubDiagnosticsFlags{interval: time.Hour}).start(d)
	stop()
	(&pubsubDiagnosticsFlags{}).start(d)()
}
