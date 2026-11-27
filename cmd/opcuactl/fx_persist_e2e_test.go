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

// fxInspectEndpoint returns the report of fx inspect against url, its
// AutomationComponent ac and the ConnectionEndpoint ep of ac, nil when it
// has none.
func fxInspectEndpoint(t *testing.T, w *cliWorker, url, ac, ep string) (model.FxReport, model.FxAutomationComponent, *model.FxConnectionEndpoint) {
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
		return r, c, walk(c.FunctionalEntities)
	}
	t.Fatalf("no AutomationComponent %s in the report", ac)
	return r, model.FxAutomationComponent{}, nil
}

// persistenceFindings are the findings of r about persistence (Part 81
// §6.2.6, the CleanupTimeout of §6.6.2); the fixture has others (it has no
// Descriptors).
func persistenceFindings(r model.FxReport) []model.FxFinding {
	var out []model.FxFinding
	for _, f := range r.Findings {
		if f.Clause == "6.2.6" || strings.HasPrefix(f.Text, "persistent ConnectionEndpoint") {
			out = append(out, f)
		}
	}
	return out
}

// TestE2E_FxPersistentRestart drives a go-opcua AutomationComponent with a
// store through the CLI only: fx connect creates a persistent endpoint
// (Part 81 §5.5.5, §6.6.2); fx inspect shows SupportsPersistence TRUE
// (§6.2.6 Table 26) and the endpoint with its negative CleanupTimeout and
// the CreationTime of its Diagnostics (Table 74), with no persistence
// finding; after a restart of the server on the same store fx inspect shows
// the endpoint restored at its NodeId, Operational, with the same
// CreationTime. The test keeps to two servers and few CLI runs: under the
// race detector on a 2-CPU runner it holds one of two parallel slots.
func TestE2E_FxPersistentRestart(t *testing.T) {
	t.Parallel()
	w := startCLIWorker(t)
	opts := fxserver.ConnectionOptions{Store: &fxstore.MemoryStore{}}
	s := startFxConnServerWith(t, opts)
	ctl := s.id(fxCtl).String()

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

	rep, ac, ep := fxInspectEndpoint(t, w, s.url, ctl, id)
	if c := ac.Capabilities; c == nil || c.SupportsPersistence == nil || !*c.SupportsPersistence {
		t.Errorf("capabilities: %+v", c)
	}
	if ep == nil || !ep.IsPersistent || ep.CleanupTimeoutMs != -1 || ep.CreationTime == "" || ep.ModificationTime < ep.CreationTime {
		t.Fatalf("endpoint before the restart: %+v", ep)
	}
	if f := persistenceFindings(rep); len(f) != 0 {
		t.Errorf("findings: %+v", f)
	}
	created := ep.CreationTime

	s.stop()
	s = startFxConnServerWith(t, opts)
	deadline := time.Now().Add(10 * time.Second)
	for {
		rep, ac, ep = fxInspectEndpoint(t, w, s.url, ctl, id)
		if ep != nil && ep.Status == "Operational" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("endpoint after the restart: %+v", ep)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !ep.IsPersistent || ep.CleanupTimeoutMs != -1 || ep.CreationTime != created {
		t.Errorf("endpoint after the restart: %+v, created before %s", ep, created)
	}
	if c := ac.Capabilities; c == nil || c.SupportsPersistence == nil || !*c.SupportsPersistence {
		t.Errorf("capabilities after the restart: %+v", c)
	}
	if f := persistenceFindings(rep); len(f) != 0 {
		t.Errorf("findings after the restart: %+v", f)
	}
}
