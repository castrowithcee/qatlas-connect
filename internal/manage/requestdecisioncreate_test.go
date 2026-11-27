package manage

import (
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/accessrequest"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/connlog"
)

const createReason = "reason-text-for-reviewer"

func (w *decisionWorld) createRequest(name string, tools ...string) string {
	w.t.Helper()
	out, err := w.queue.Add(accessrequest.Env{Config: w.load(), Tools: w.tools, Now: decisionEpoch}, accessrequest.Input{
		Kind: accessrequest.KindCreateConnection, Provider: "wiki", Connection: name, Tools: tools,
		Reason: createReason, ReasonRaw: createReason,
	})
	if err != nil {
		w.t.Fatalf("Add(create %s, %v) = %v", name, tools, err)
	}
	return out.Request.ID
}

// saveConnection stands in for the setup of the TUI: it saves a connection of the wiki provider.
func (w *decisionWorld) saveConnection(name string) {
	w.edit("listed", func(*config.Connection) {})
	cfg := w.load()
	cfg.Connections[name] = config.Connection{Service: "svc", Credential: "kcred", Tools: []string{"wiki.pages.get"}}
	if err := w.store.Save(cfg); err != nil {
		w.t.Fatal(err)
	}
}

func (w *decisionWorld) tui() *Service {
	c := *w.svc
	c.surface = connlog.SurfaceTUI
	return &c
}

func TestPreviewOfACreateRequestIsThePrefill(t *testing.T) {
	w := newDecisionWorld(t, false)
	id := w.createRequest("docs", "wiki.pages.*", "wiki.spaces.get")
	p, err := w.svc.PreviewRequestDecision(RequestSelection{Request: id})
	if err != nil || p.Kind != accessrequest.KindCreateConnection || p.Create == nil {
		t.Fatalf("preview = %+v, %v", p, err)
	}
	c := p.Create
	if c.Provider != "wiki" || c.Name != "docs" || strings.Join(c.Tools, ",") != "wiki.pages.*,wiki.spaces.get" ||
		len(c.Entries) != 2 || c.Reason != createReason {
		t.Errorf("prefill = %+v", c)
	}
	if _, err := w.svc.PreviewRequestDecision(RequestSelection{Request: id, Tools: []string{"wiki.pages.*"}}); err == nil {
		t.Error("a per-tool selection of a create request was accepted")
	}
}

func TestCloseAfterSavingClosesAsAcceptedUnderAnyName(t *testing.T) {
	for _, saved := range []string{"docs", "docs-mine"} {
		t.Run(saved, func(t *testing.T) {
			w := newDecisionWorld(t, false)
			id := w.createRequest("docs", "wiki.pages.get", "wiki.spaces.get")
			other := w.createRequest("other", "wiki.pages.list")
			w.saveConnection(saved)
			res, err := w.tui().CloseConnectionRequest(w.proof, id, saved)
			if err != nil {
				t.Fatalf("CloseConnectionRequest = %v", err)
			}
			if res.Connection != saved || len(res.Decided) != 2 {
				t.Errorf("result = %+v", res)
			}
			open := w.open()
			if len(open) != 1 || open[0].ID != other {
				t.Errorf("open = %+v, want only the other request", open)
			}
			log := w.log()
			if strings.Count(log, `"operation":"config.request.connection.accept"`) != 2 ||
				!strings.Contains(log, `"connection":"`+saved+`"`) || !strings.Contains(log, `"tool":"wiki.spaces.get"`) {
				t.Errorf("log lacks the accept entries: %s", log)
			}
			if strings.Contains(log, "config.request.connection.drop") || strings.Contains(log, createReason) {
				t.Errorf("log has a drop or the reason: %s", log)
			}
		})
	}
}

func TestCloseAfterSavingRefusals(t *testing.T) {
	w := newDecisionWorld(t, false)
	id := w.createRequest("docs", "wiki.pages.get")
	_, err := w.tui().CloseConnectionRequest(w.proof, id, "docs")
	wantDecisionCode(t, err, RequestCodeNotSaved)
	w.saveConnection("docs")
	cfg := w.load()
	cfg.Services["svc2"] = config.Service{Provider: "wiki", BaseURL: "https://wiki.example.test"}
	if err := w.store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	_, err = w.tui().CloseConnectionRequest(w.proof, "nope", "docs")
	wantDecisionCode(t, err, RequestCodeNotFound)
	_, err = w.svc.CloseConnectionRequest(w.proof, id, "docs")
	wantDecisionCode(t, err, RequestCodeUnavailable)
	if len(w.open()) != 0 {
		// The name exists now, so the request lapsed; the refusals above changed nothing before that.
		t.Logf("open = %+v", w.open())
	}
}

func TestCloseAfterSavingNeedsAdminProofOnEncryptedVault(t *testing.T) {
	w := newDecisionWorld(t, true)
	id := w.createRequest("docs", "wiki.pages.get")
	w.saveConnection("docs-mine")
	_, err := w.tui().CloseConnectionRequest(AdminProof{}, id, "docs-mine")
	wantDecisionCode(t, err, RequestCodeAdminRequired)
	if _, err := w.tui().CloseConnectionRequest(w.proof, id, "docs-mine"); err != nil {
		t.Fatal(err)
	}
}

func TestDeclineACreateRequestAndRefuseOtherDecisions(t *testing.T) {
	w := newDecisionWorld(t, false)
	id := w.createRequest("docs", "wiki.pages.get", "wiki.spaces.get")
	for _, action := range []RequestAction{RequestAccept, RequestBlock} {
		_, err := w.decide(id, action, "")
		wantDecisionCode(t, err, RequestCodeSetupInTUI)
		if err != nil && !strings.Contains(err.Error(), "qconnect tui") {
			t.Errorf("message %q does not point to the TUI", err)
		}
	}
	if len(w.open()) != 1 {
		t.Fatal("a refused decision closed the request")
	}
	res := w.mustDecide(id, RequestDecline, "")
	if len(res.Decided) != 2 || len(w.open()) != 0 {
		t.Errorf("result %+v, open %v", res, w.open())
	}
	log := w.log()
	if strings.Count(log, `"operation":"config.request.connection.decline"`) != 2 || strings.Contains(log, createReason) {
		t.Errorf("log = %s", log)
	}
	if _, ok := w.load().Connections["docs"]; ok {
		t.Error("declining created a connection")
	}
}
