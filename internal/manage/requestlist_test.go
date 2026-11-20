package manage

import (
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/accessrequest"
	"github.com/castrowithcee/qatlas-connect/internal/connlog"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// OpenRequests returns the requests newest first with their entries in the order of the provider's groups,
// and the reason of an agent cleaned: a known secret redacted, control characters escaped, cut at the bound.
func TestOpenRequestsCleansTheReasonAndOrdersEntries(t *testing.T) {
	w := newDecisionWorld(t, false)
	const known = "synthetic-known-secret-7d3a"
	w.svc = New(w.store, newFakeSecrets(), connlog.SurfaceTUI, redactorOf(known)).WithVaultProcessSupport(false).
		WithRequests(RequestSource{Queue: w.queue, Tools: w.tools, Now: w.svc.requests.Now})
	cfg := w.load()
	_, err := w.queue.Add(accessrequest.Env{Config: cfg, Tools: w.tools, Now: decisionEpoch}, accessrequest.Input{
		Connection: "reader", ConnectionID: cfg.Connections["reader"].ID, Reason: "use " + known + "\x1b[2J\nnow",
		Tools: []string{"wiki.spaces.create", "wiki.pages.update", "wiki.pages.delete"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := w.svc.OpenRequests()
	if err != nil || len(got) != 1 {
		t.Fatalf("OpenRequests() = %v, %v", got, err)
	}
	r := got[0]
	if strings.Contains(r.Reason, known) || strings.ContainsAny(r.Reason, "\x1b\n") || !strings.Contains(r.Reason, `\u001b[2J\nnow`) {
		t.Errorf("reason = %q", r.Reason)
	}
	var order []string
	for _, e := range r.Entries {
		order = append(order, e.Tool)
	}
	if strings.Join(order, " ") != "wiki.pages.delete wiki.pages.update wiki.spaces.create" {
		t.Errorf("entries = %v", order)
	}
	if r.Entries[0].Group != "pages" || r.Entries[2].Group != "spaces" {
		t.Errorf("groups = %+v", r.Entries)
	}
}

func TestOpenRequestsWithoutASourceIsEmpty(t *testing.T) {
	w := newDecisionWorld(t, false)
	svc := New(w.store, newFakeSecrets(), connlog.SurfaceTUI, nil)
	if got, err := svc.OpenRequests(); err != nil || len(got) != 0 {
		t.Errorf("OpenRequests() = %v, %v", got, err)
	}
}

func redactorOf(values ...string) *redact.Redactor {
	r := &redact.Redactor{}
	r.Add(values...)
	return r
}
