package manage

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/accessrequest"
	"github.com/castrowithcee/qatlas-connect/internal/approval"
	"github.com/castrowithcee/qatlas-connect/internal/config"
)

// removeRequest stores a request to remove tools and returns its ID.
func (w *decisionWorld) removeRequest(conn string, tools ...string) string {
	w.t.Helper()
	cfg := w.load()
	out, err := w.queue.Add(accessrequest.Env{Config: cfg, Tools: w.tools, Now: decisionEpoch}, accessrequest.Input{
		Kind: accessrequest.KindRemoveTools, Connection: conn, ConnectionID: cfg.Connections[conn].ID,
		Tools: tools, Reason: "reason-text-for-reviewer",
	})
	if err != nil {
		w.t.Fatalf("Add(remove %s, %v) = %v", conn, tools, err)
	}
	return out.Request.ID
}

func (w *decisionWorld) edit(conn string, change func(*config.Connection)) {
	w.t.Helper()
	cfg := w.load()
	c := cfg.Connections[conn]
	change(&c)
	cfg.Connections[conn] = c
	if err := w.store.Save(cfg); err != nil {
		w.t.Fatal(err)
	}
}

func (w *decisionWorld) offered(conn string) []string {
	w.t.Helper()
	cfg := w.load()
	var out []string
	for _, tool := range decisionProvider.Tools {
		if r := cfg.ConnectionRefusal(conn, tool, decisionEpoch); r == "" {
			out = append(out, tool.ID)
		}
	}
	return out
}

func wantDecisionCode(t *testing.T, err error, code string) {
	t.Helper()
	var de *RequestDecisionError
	if !errors.As(err, &de) || de.Code != code {
		t.Errorf("err = %v, want %s", err, code)
	}
}

func TestRemoveOnAToolsListDropsTheExactEntryOnly(t *testing.T) {
	w := newDecisionWorld(t, false)
	w.edit("listed", func(c *config.Connection) { c.Tools = []string{"wiki.pages.get", "wiki.pages.list", "wiki.spaces.get"} })
	id := w.removeRequest("listed", "wiki.pages.get")
	preview, err := w.svc.PreviewRequestDecision(RequestSelection{Request: id})
	if err != nil || preview.Kind != accessrequest.KindRemoveTools || preview.Remaining != 2 || preview.EmptyList || len(preview.SplitPrefixes) != 0 {
		t.Fatalf("preview = %+v, %v", preview, err)
	}
	readerBefore := w.load().Connections["reader"]
	res := w.mustDecide(id, RequestAccept, "")
	if got := w.load().Connections["listed"].Tools; !reflect.DeepEqual(got, []string{"wiki.pages.list", "wiki.spaces.get"}) {
		t.Errorf("tools = %v", got)
	}
	if !reflect.DeepEqual(w.load().Connections["reader"], readerBefore) {
		t.Error("accepting changed another connection")
	}
	if len(w.open()) != 0 || !reflect.DeepEqual(res.Decided, []string{"wiki.pages.get"}) {
		t.Errorf("open = %v, result = %+v", w.open(), res)
	}
	log := w.log()
	for _, want := range []string{`"operation":"config.request.remove.accept"`, `"tool":"wiki.pages.get"`, `"operation":"config.connection.change"`} {
		if !strings.Contains(log, want) {
			t.Errorf("log lacks %s", want)
		}
	}
	if strings.Contains(log, "reason-text-for-reviewer") || strings.Contains(log, `"operation":"config.request.accept"`) {
		t.Error("the reason was logged, or the decision was logged as an offer")
	}
}

func TestRemoveReplacesAPrefixEntryByTheOtherOfferedTools(t *testing.T) {
	w := newDecisionWorld(t, false)
	w.edit("listed", func(c *config.Connection) { c.Tools = []string{"wiki.pages.*", "wiki.spaces.get"} })
	id := w.removeRequest("listed", "wiki.pages.update")
	preview, err := w.svc.PreviewRequestDecision(RequestSelection{Request: id})
	if err != nil || !reflect.DeepEqual(preview.SplitPrefixes, []string{"wiki.pages.*"}) || preview.Remaining != 4 {
		t.Fatalf("preview = %+v, %v", preview, err)
	}
	w.mustDecide(id, RequestAccept, "")
	got := w.load().Connections["listed"].Tools
	if want := []string{"wiki.pages.delete", "wiki.pages.get", "wiki.pages.list", "wiki.spaces.get"}; !reflect.DeepEqual(got, want) {
		t.Errorf("tools = %v, want %v", got, want)
	}
	if want := []string{"wiki.pages.delete", "wiki.pages.get", "wiki.pages.list", "wiki.spaces.get"}; !reflect.DeepEqual(w.offered("listed"), want) {
		t.Errorf("offered = %v", w.offered("listed"))
	}
}

func TestRemoveAPrefixOfTheRequestTakesEveryToolItCovers(t *testing.T) {
	w := newDecisionWorld(t, false)
	w.edit("listed", func(c *config.Connection) { c.Tools = []string{"wiki.pages.get", "wiki.pages.list", "wiki.spaces.get"} })
	w.mustDecide(w.removeRequest("listed", "wiki.pages.*"), RequestAccept, "")
	if got := w.load().Connections["listed"].Tools; !reflect.DeepEqual(got, []string{"wiki.spaces.get"}) {
		t.Errorf("tools = %v", got)
	}
}

func TestRemoveMayLeaveAnEmptyListAndSaysSo(t *testing.T) {
	w := newDecisionWorld(t, false)
	id := w.removeRequest("listed", "wiki.pages.get")
	preview, err := w.svc.PreviewRequestDecision(RequestSelection{Request: id})
	if err != nil || !preview.EmptyList || preview.Remaining != 0 {
		t.Fatalf("preview = %+v, %v", preview, err)
	}
	w.mustDecide(id, RequestAccept, "")
	if got := w.load().Connections["listed"].Tools; got == nil || len(got) != 0 {
		t.Errorf("tools = %#v, want an empty list", got)
	}
	if len(w.offered("listed")) != 0 {
		t.Errorf("offered = %v", w.offered("listed"))
	}
}

func TestRemoveOnEffectsNeedsSwitchToList(t *testing.T) {
	w := newDecisionWorld(t, false)
	id := w.removeRequest("reader", "wiki.pages.get")
	before := mustRead(t, w.store.Path())
	for _, mode := range []AcceptMode{"", AcceptAddEffects} {
		_, err := w.decide(id, RequestAccept, mode)
		wantDecisionCode(t, err, RequestCodeInvalid)
	}
	if string(mustRead(t, w.store.Path())) != string(before) || len(w.open()) != 1 {
		t.Error("a refused decision changed something")
	}
	preview, err := w.svc.PreviewRequestDecision(RequestSelection{Request: id})
	if err != nil || !preview.ByEffects || preview.Remaining != 2 {
		t.Fatalf("preview = %+v, %v", preview, err)
	}
	res := w.mustDecide(id, RequestAccept, AcceptSwitchToList)
	got := w.load().Connections["reader"]
	if !reflect.DeepEqual(got.Tools, []string{"wiki.pages.list", "wiki.spaces.get"}) || got.Effects != nil || res.Mode != AcceptSwitchToList {
		t.Errorf("connection = %+v, result = %+v", got, res)
	}
}

func TestRemoveOnAToolsListRefusesAMode(t *testing.T) {
	w := newDecisionWorld(t, false)
	_, err := w.decide(w.removeRequest("listed", "wiki.pages.get"), RequestAccept, AcceptSwitchToList)
	wantDecisionCode(t, err, RequestCodeInvalid)
}

func TestRemovalMustChangeNothingElse(t *testing.T) {
	w := newDecisionWorld(t, false)
	d, err := w.svc.openDecision(RequestSelection{Request: w.removeRequest("reader", "wiki.pages.get")})
	if err != nil {
		t.Fatal(err)
	}
	plan := d.planRemove()
	if !d.removalHolds(plan) {
		t.Fatal("the planned removal does not hold")
	}
	untouched := plan
	untouched.cfg = d.cfg
	tooMuch := plan
	tooMuch.removed = append([]string{"wiki.pages.list"}, plan.removed...)
	for name, p := range map[string]removePlan{"keeps the tool": untouched, "removes another": tooMuch} {
		if d.removalHolds(p) {
			t.Errorf("%s: the postcondition accepted it", name)
		}
	}
}

func TestDeclineRemoveChangesOnlyTheQueue(t *testing.T) {
	w := newDecisionWorld(t, false)
	id := w.removeRequest("listed", "wiki.pages.get")
	before := mustRead(t, w.store.Path())
	w.mustDecide(id, RequestDecline, "")
	if string(mustRead(t, w.store.Path())) != string(before) || len(w.open()) != 0 {
		t.Error("declining changed the configuration or left the request open")
	}
	if log := w.log(); !strings.Contains(log, `"operation":"config.request.remove.decline"`) || strings.Contains(log, `"config.request.decline"`) {
		t.Errorf("log = %s", log)
	}
}

func TestBlockingARequestToRemoveIsRefused(t *testing.T) {
	w := newDecisionWorld(t, false)
	id := w.removeRequest("listed", "wiki.pages.get")
	before := mustRead(t, w.store.Path())
	_, err := w.decide(id, RequestBlock, "")
	wantDecisionCode(t, err, RequestCodeInvalid)
	if string(mustRead(t, w.store.Path())) != string(before) || len(w.open()) != 1 {
		t.Error("a refused block changed something")
	}
}

func TestRemoveRevisionConflictChangesNothing(t *testing.T) {
	w := newDecisionWorld(t, false)
	id := w.removeRequest("listed", "wiki.pages.get")
	queueBefore := mustRead(t, w.queue.Path())
	armed := true
	w.svc = w.svc.WithRequests(RequestSource{Queue: w.queue, Tools: w.tools, Now: func() time.Time {
		if armed {
			armed = false
			w.edit("listed", func(c *config.Connection) { c.Description = "changed by another writer" })
		}
		return decisionEpoch
	}})
	_, err := w.decide(id, RequestAccept, "")
	if !errors.Is(err, config.ErrConflict) {
		t.Fatalf("err = %v, want a conflict", err)
	}
	if got := w.load().Connections["listed"].Tools; !reflect.DeepEqual(got, []string{"wiki.pages.get"}) || string(mustRead(t, w.queue.Path())) != string(queueBefore) {
		t.Errorf("tools = %v, or the queue changed", got)
	}
}

func TestRemoveNeedsAdminProofForAnEncryptedVault(t *testing.T) {
	w := newDecisionWorld(t, true)
	id := w.removeRequest("vaulted", "wiki.pages.get")
	before, queueBefore := w.load(), mustRead(t, w.queue.Path())
	for _, action := range []RequestAction{RequestAccept, RequestDecline} {
		_, err := w.svc.DecideRequest(context.Background(), AdminProof{}, RequestDecision{
			RequestSelection: RequestSelection{Request: id}, Action: action, Mode: AcceptSwitchToList})
		wantDecisionCode(t, err, RequestCodeAdminRequired)
	}
	if !reflect.DeepEqual(w.load(), before) || string(mustRead(t, w.queue.Path())) != string(queueBefore) {
		t.Error("a refused decision changed something")
	}
}

func TestRemoveOnAVaultConnectionApprovesInTheSameStep(t *testing.T) {
	w := newDecisionWorld(t, true)
	id := w.removeRequest("vaulted", "wiki.pages.get")
	if p, err := w.svc.PreviewRequestDecision(RequestSelection{Request: id}); err != nil || !p.ApprovalExpected {
		t.Fatalf("preview = %+v, %v, want ApprovalExpected", p, err)
	}
	res := w.mustDecide(id, RequestAccept, AcceptSwitchToList)
	if res.Approval.State != ApprovalGranted {
		t.Fatalf("Approval = %+v, want approved", res.Approval)
	}
	report, err := approval.Pending(w.load(), w.v)
	if err != nil || len(report.Open) != 0 {
		t.Errorf("Pending = %+v, %v, want nothing open", report, err)
	}
}

func TestRemoveDropsAToolTheConnectionNoLongerOffers(t *testing.T) {
	w := newDecisionWorld(t, false)
	w.edit("listed", func(c *config.Connection) { c.Tools = []string{"wiki.pages.get", "wiki.pages.list"} })
	id := w.removeRequest("listed", "wiki.pages.get", "wiki.pages.list")
	w.edit("listed", func(c *config.Connection) { c.Tools = []string{"wiki.pages.list"} })
	res := w.mustDecide(id, RequestAccept, "")
	// The queue drops the entry the connection stopped offering before the decision sees it.
	if !reflect.DeepEqual(res.Decided, []string{"wiki.pages.list"}) || len(w.open()) != 0 {
		t.Errorf("result = %+v", res)
	}
	if !strings.Contains(w.log(), `"operation":"config.request.remove.drop"`) {
		t.Error("the dropped tool was not logged")
	}
	if got := w.load().Connections["listed"].Tools; got == nil || len(got) != 0 {
		t.Errorf("tools = %#v", got)
	}
}
