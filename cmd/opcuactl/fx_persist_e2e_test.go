// SPDX-License-Identifier: MIT

package opcuactl

import (
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-opcua/uafx/fxserver"
	"github.com/otfabric/go-opcua/uafx/fxstore"
	"github.com/otfabric/opcuactl/internal/model"
)

// fxInspectEndpoint returns the AutomationComponent ac of fx inspect
// against url and its ConnectionEndpoint ep, nil when it has none.
func fxInspectEndpoint(t *testing.T, w *cliWorker, url, ac, ep string) (model.FxAutomationComponent, *model.FxConnectionEndpoint) {
	t.Helper()
	r := fxJSON(t, w.mustRun(t, "fx", "inspect", "-e", url, "--format", "json"))
	for _, c := range r.AutomationComponents {
		if c.NodeID != ac {
			continue
		}
		var walk func([]model.FxFunctionalEntity) *model.FxConnectionEndpoint
		walk = func(fes []model.FxFunctionalEntity) *model.FxConnectionEndpoint {
			for _, fe := range fes {
				for i := range fe.ConnectionEndpoints {
					if fe.ConnectionEndpoints[i].NodeID == ep {
						return &fe.ConnectionEndpoints[i]
					}
				}
				if e := walk(fe.SubFunctionalEntities); e != nil {
					return e
				}
			}
			return nil
		}
		return c, walk(c.FunctionalEntities)
	}
	t.Fatalf("no AutomationComponent %s in the report", ac)
	return model.FxAutomationComponent{}, nil
}

// TestE2E_FxPersistentRestart drives a go-opcua AutomationComponent with a
// store through the CLI only: fx inspect shows SupportsPersistence TRUE
// (Part 81 §6.2.6 Table 26); fx connect creates a persistent endpoint
// (§5.5.5, §6.6.2), which fx inspect shows with its negative
// CleanupTimeout and the CreationTime of its Diagnostics (Table 74); after a
// restart of the server on the same store fx inspect shows the endpoint
// restored at its NodeId, Operational, with the same CreationTime. Without
// a store SupportsPersistence is absent and fx connect gets
// Bad_NotSupported for IsPersistent TRUE (Table 12).
func TestE2E_FxPersistentRestart(t *testing.T) {
	t.Parallel()
	w := startCLIWorker(t)
	store := &fxstore.MemoryStore{}
	opts := fxserver.ConnectionOptions{Store: store}
	s := startFxConnServerWith(t, opts)
	ctl := s.id(fxCtl).String()

	ac, _ := fxInspectEndpoint(t, w, s.url, ctl, "")
	if c := ac.Capabilities; c == nil || c.SupportsPersistence == nil || !*c.SupportsPersistence {
		t.Fatalf("capabilities: %+v", c)
	}
	dir := t.TempDir()
	yaml := strings.Replace(connectYAML("Kept", s.pubsubFile(t, dir), 7), "    cleanupTimeoutMs: -1\n", "    persistent: true\n    cleanupTimeoutMs: -1\n", 1)
	req := writeRequest(t, dir, "connect.yaml", yaml)
	stdout, stderr, err := w.run(t, "fx", "connect", "-e", s.url, "--ac", nsu(fxCtl), "--request", req, "--format", "json")
	if err != nil {
		t.Fatalf("fx connect: %v\n%s\n%s", err, stdout, stderr)
	}
	r := fxConnectJSON(t, stdout)
	if r.Status != "Good" || len(r.Endpoints) != 1 || r.Endpoints[0].ConnectionEndpoint == "" {
		t.Fatalf("result: %+v", r)
	}
	id := r.Endpoints[0].ConnectionEndpoint

	_, ep := fxInspectEndpoint(t, w, s.url, ctl, id)
	if ep == nil || !ep.IsPersistent || ep.CleanupTimeoutMs != -1 || ep.CreationTime == "" || ep.ModificationTime < ep.CreationTime {
		t.Fatalf("endpoint before the restart: %+v", ep)
	}
	created := ep.CreationTime
	// No persistence finding (the fixture has others: no Descriptors).
	for _, f := range fxJSON(t, w.mustRun(t, "fx", "inspect", "-e", s.url, "--format", "json")).Findings {
		if f.Clause == "6.2.6" || strings.HasPrefix(f.Text, "persistent ConnectionEndpoint") {
			t.Errorf("finding: %+v", f)
		}
	}

	s.stop()
	s = startFxConnServerWith(t, opts)
	deadline := time.Now().Add(10 * time.Second)
	for {
		_, ep = fxInspectEndpoint(t, w, s.url, ctl, id)
		if ep != nil && ep.Status == "Operational" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("endpoint after the restart: %+v", ep)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !ep.IsPersistent || ep.CleanupTimeoutMs != -1 || ep.CreationTime != created {
		t.Errorf("endpoint after the restart: %+v, created before %s", ep, created)
	}

	// Without a store.
	n := startFxConnServer(t)
	ac, _ = fxInspectEndpoint(t, w, n.url, n.id(fxCtl).String(), "")
	if c := ac.Capabilities; c != nil && c.SupportsPersistence != nil {
		t.Errorf("capabilities without a store: %+v", c)
	}
	req = writeRequest(t, dir, "connect2.yaml", strings.Replace(yaml, "pubsub.json", n.pubsubFile(t, t.TempDir()), 1))
	stdout, _, err = w.run(t, "fx", "connect", "-e", n.url, "--ac", nsu(fxCtl), "--request", req, "--format", "json")
	if code := ExitCode(err); code != exitProtocol {
		t.Fatalf("fx connect without a store: exit %d (%v)\n%s", code, err, stdout)
	}
	if r := fxConnectJSON(t, stdout); len(r.Endpoints) != 1 || r.Endpoints[0].ConnectionEndpointResult != "BadNotSupported" {
		t.Errorf("result without a store: %+v", r)
	}
}
