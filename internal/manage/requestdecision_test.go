package manage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/accessrequest"
	"github.com/castrowithcee/qatlas-connect/internal/approval"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/connlog"
	"github.com/castrowithcee/qatlas-connect/internal/vault"
	"github.com/castrowithcee/qatlas-kit/redact"
)

var decisionProvider = config.ProviderMetadata{
	ID: "wiki", Name: "Wiki", DefaultEffects: []config.Effect{config.EffectRead},
	SupportedEffects: config.Effects(), SecretRoles: []config.SecretRole{{Name: "token"}},
	Tools: []config.ToolMetadata{
		{ID: "wiki.admin.reset", Effect: config.EffectExecute, RequiresToolAllowList: true},
		{ID: "wiki.pages.delete", Effect: config.EffectDelete},
		{ID: "wiki.pages.get", Effect: config.EffectRead},
		{ID: "wiki.pages.list", Effect: config.EffectRead},
		{ID: "wiki.pages.update", Effect: config.EffectUpdate},
		{ID: "wiki.spaces.create", Effect: config.EffectCreate},
		{ID: "wiki.spaces.get", Effect: config.EffectRead},
	},
}

type decisionCatalog struct{}

func (decisionCatalog) ProviderMetadata(id string) (config.ProviderMetadata, bool) {
	return decisionProvider, id == decisionProvider.ID
}
func (decisionCatalog) ProviderMetadataAll() []config.ProviderMetadata {
	return []config.ProviderMetadata{decisionProvider}
}

var decisionEpoch = time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)

const decisionPassphrase = "synthetic-passphrase-52d1"

type decisionWorld struct {
	t     *testing.T
	dir   string
	store *config.Store
	queue *accessrequest.Queue
	svc   *Service
	proof AdminProof
	tools []accessrequest.Tool
	v     *vault.Vault
}

// newDecisionWorld saves four connections: "listed" with a tools list, "reader" by effect, "vaulted" by effect
// over a vault credential, and "keyed" by effect over a keyring credential. encrypted gives it an encrypted
// vault that already approved "vaulted".
func newDecisionWorld(t *testing.T, encrypted bool) *decisionWorld {
	t.Helper()
	dir := t.TempDir()
	w := &decisionWorld{t: t, dir: dir}
	w.store = config.NewStore(filepath.Join(dir, "config.yaml"), decisionCatalog{})
	cfg := w.store.New()
	cfg.Services = map[string]config.Service{"svc": {Provider: "wiki", BaseURL: "https://wiki.example.test"}}
	cfg.Credentials = map[string]config.Credential{
		"vcred": {Provider: "wiki", Type: config.CredentialTypeVault},
		"kcred": {Provider: "wiki", Type: config.CredentialTypeKeyring},
	}
	cfg.Connections = map[string]config.Connection{
		"listed":  {Service: "svc", Credential: "kcred", Tools: []string{"wiki.pages.get"}},
		"reader":  {Service: "svc", Credential: "kcred", Effects: []config.Effect{config.EffectRead}},
		"vaulted": {Service: "svc", Credential: "vcred", Effects: []config.Effect{config.EffectRead}},
		"keyed":   {Service: "svc", Credential: "kcred", Effects: []config.Effect{config.EffectRead}},
	}
	if err := w.store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	for _, tool := range decisionProvider.Tools {
		w.tools = append(w.tools, accessrequest.Tool{ToolMetadata: tool, Provider: "wiki"})
	}
	var err error
	if w.queue, err = accessrequest.NewQueue(filepath.Join(dir, accessrequest.FileName)); err != nil {
		t.Fatal(err)
	}
	secrets := newFakeSecrets()
	if encrypted {
		w.v = vault.New(dir)
		offer := func(string) (string, error) { return decisionPassphrase, nil }
		if err := w.v.Set("vcred", "token", "synthetic-token-value", offer); err != nil {
			t.Fatal(err)
		}
		loaded, err := w.store.Load()
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := approval.Approve(context.Background(), loaded, w.v, nil); err != nil {
			t.Fatal(err)
		}
		secrets.vault = w.v
	}
	w.svc = New(w.store, secrets, connlog.SurfaceCLI, &redact.Redactor{}).WithVaultProcessSupport(false).
		WithRequests(RequestSource{Queue: w.queue, Tools: w.tools, Now: func() time.Time { return decisionEpoch }})
	session := NewAdminSession(true, nil)
	session.Grant(0)
	w.proof, _ = session.Authorize(0)
	return w
}

func (w *decisionWorld) load() *config.Config {
	w.t.Helper()
	cfg, err := w.store.Load()
	if err != nil {
		w.t.Fatal(err)
	}
	return cfg
}

// request stores a request and returns its ID.
func (w *decisionWorld) request(conn string, tools ...string) string {
	w.t.Helper()
	cfg := w.load()
	out, err := w.queue.Add(accessrequest.Env{Config: cfg, Tools: w.tools, Now: decisionEpoch}, accessrequest.Input{
		Connection: conn, ConnectionID: cfg.Connections[conn].ID, Tools: tools, Reason: "reason-text-for-reviewer",
	})
	if err != nil {
		w.t.Fatalf("Add(%s, %v) = %v", conn, tools, err)
	}
	return out.Request.ID
}

func (w *decisionWorld) open() []accessrequest.Request {
	w.t.Helper()
	got, _, err := w.queue.Open(accessrequest.Env{Config: w.load(), Tools: w.tools, Now: decisionEpoch})
	if err != nil {
		w.t.Fatal(err)
	}
	return got
}

func (w *decisionWorld) decide(id string, action RequestAction, mode AcceptMode, tools ...string) (RequestDecisionResult, error) {
	return w.svc.DecideRequest(context.Background(), w.proof, RequestDecision{
		RequestSelection: RequestSelection{Request: id, Tools: tools}, Action: action, Mode: mode})
}

func (w *decisionWorld) log() string {
	w.t.Helper()
	files, _ := filepath.Glob(filepath.Join(w.dir, "*", "logs", "*.jsonl"))
	var all strings.Builder
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			w.t.Fatal(err)
		}
		all.Write(data)
	}
	return all.String()
}

func (w *decisionWorld) mustDecide(id string, action RequestAction, mode AcceptMode, tools ...string) RequestDecisionResult {
	w.t.Helper()
	res, err := w.decide(id, action, mode, tools...)
	if err != nil {
		w.t.Fatalf("DecideRequest(%s) = %v", action, err)
	}
	return res
}

func TestAcceptOnAToolsList(t *testing.T) {
	w := newDecisionWorld(t, false)
	id := w.request("listed", "wiki.pages.update", "wiki.spaces.*")
	cfg := w.load()
	conn := cfg.Connections["listed"]
	// The block of an entry the new list covers is released by the save; the other one stays.
	conn.BlockedTools = []string{"wiki.spaces.get", "wiki.pages.delete"}
	cfg.Connections["listed"] = conn
	if err := w.store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	res := w.mustDecide(id, RequestAccept, "")
	got := w.load().Connections["listed"]
	if want := []string{"wiki.pages.get", "wiki.pages.update", "wiki.spaces.*"}; !reflect.DeepEqual(got.Tools, want) {
		t.Errorf("Tools = %v, want %v", got.Tools, want)
	}
	if want := []string{"wiki.pages.delete"}; !reflect.DeepEqual(got.BlockedTools, want) {
		t.Errorf("BlockedTools = %v, want only the uncovered block", got.BlockedTools)
	}
	if len(res.Remaining) != 0 || res.Approval.State != ApprovalNotNeeded || len(w.open()) != 0 {
		t.Errorf("result %+v, open %v", res, w.open())
	}
}

func TestAcceptOnEffectsAddsOnlyTheNeededEffects(t *testing.T) {
	w := newDecisionWorld(t, false)
	id := w.request("reader", "wiki.pages.update", "wiki.pages.delete", "wiki.spaces.create")
	res := w.mustDecide(id, RequestAccept, AcceptAddEffects, "wiki.pages.update")
	got := w.load().Connections["reader"]
	if want := []config.Effect{config.EffectRead, config.EffectUpdate}; !reflect.DeepEqual(got.Effects, want) || got.Tools != nil {
		t.Errorf("connection = %+v, want read and update by effect", got)
	}
	if want := []string{"wiki.pages.delete", "wiki.spaces.create"}; !reflect.DeepEqual(res.Remaining, want) {
		t.Errorf("Remaining = %v, want the unselected tools left open", res.Remaining)
	}
	if open := w.open(); len(open) != 1 || !reflect.DeepEqual(open[0].Tools, res.Remaining) {
		t.Errorf("open = %+v", open)
	}
}

func TestAcceptOnEffectsRefusesAnAllowListToolAndNeedsAMode(t *testing.T) {
	w := newDecisionWorld(t, false)
	id := w.request("reader", "wiki.admin.reset", "wiki.pages.update")
	before := w.load()
	_, err := w.decide(id, RequestAccept, AcceptAddEffects, "wiki.admin.reset")
	var de *RequestDecisionError
	if !errors.As(err, &de) || de.Code != RequestCodeNeedsList || !strings.Contains(de.Message, "tools list") {
		t.Fatalf("add effects for an allow-list tool = %v, want needs-tool-list", err)
	}
	if _, err := w.decide(id, RequestAccept, ""); !errors.As(err, &de) || de.Code != RequestCodeInvalid {
		t.Fatalf("no mode = %v, want invalid-request", err)
	}
	if !reflect.DeepEqual(w.load(), before) || len(w.open()) != 1 {
		t.Error("a refused decision changed something")
	}
	// Switching is the way to accept it.
	w.mustDecide(id, RequestAccept, AcceptSwitchToList, "wiki.admin.reset")
	if got := w.load().Connections["reader"]; !reflect.DeepEqual(got.Tools, []string{"wiki.admin.reset", "wiki.pages.get", "wiki.pages.list", "wiki.spaces.get"}) {
		t.Errorf("Tools = %v", got.Tools)
	}
}

func TestSwitchToListKeepsTodaysToolsPlusTheSelectedOnesAndDropsEffects(t *testing.T) {
	w := newDecisionWorld(t, false)
	id := w.request("reader", "wiki.pages.update", "wiki.spaces.*")
	res := w.mustDecide(id, RequestAccept, AcceptSwitchToList)
	got := w.load().Connections["reader"]
	want := []string{"wiki.pages.get", "wiki.pages.list", "wiki.pages.update", "wiki.spaces.*", "wiki.spaces.get"}
	// wiki.spaces.get is offered today and stays an exact ID next to the prefix.
	if !reflect.DeepEqual(got.Tools, want) || got.Effects != nil {
		t.Errorf("connection = %+v, want tools %v and no effects", got, want)
	}
	if res.Mode != AcceptSwitchToList {
		t.Errorf("Mode = %q", res.Mode)
	}
}

func TestPreviewNamesTheConsequenceOfBothWays(t *testing.T) {
	w := newDecisionWorld(t, false)
	id := w.request("reader", "wiki.pages.update", "wiki.pages.delete", "wiki.spaces.create")
	p, err := w.svc.PreviewRequestDecision(RequestSelection{Request: id, Tools: []string{"wiki.pages.update", "wiki.spaces.create"}})
	if err != nil {
		t.Fatal(err)
	}
	wantGains := []EffectGain{{config.EffectCreate, 1}, {config.EffectUpdate, 1}}
	if !p.ByEffects || !reflect.DeepEqual(p.AddEffects, wantGains) || p.SwitchTools != 5 || len(p.AddEffectsBlockedBy) != 0 {
		t.Errorf("preview = %+v, want gains %v and 5 tools after switching (3 read + 2 selected)", p, wantGains)
	}
	if len(p.Entries) != 2 || p.Entries[0].Tool != "wiki.pages.update" || p.Entries[0].Group != "pages" || p.Entries[1].Group != "spaces" {
		t.Errorf("entries = %+v", p.Entries)
	}

	id2 := w.request("listed", "wiki.pages.*", "wiki.spaces.get")
	p, err = w.svc.PreviewRequestDecision(RequestSelection{Request: id2})
	if err != nil || p.ByEffects || p.NewTools != 4 {
		t.Errorf("tools list preview = %+v, %v, want 4 new tools", p, err)
	}

	id3 := w.request("keyed", "wiki.admin.reset")
	p, err = w.svc.PreviewRequestDecision(RequestSelection{Request: id3})
	if err != nil || !reflect.DeepEqual(p.AddEffectsBlockedBy, []string{"wiki.admin.reset"}) || len(p.AddEffects) != 0 || p.SwitchTools != 4 {
		t.Errorf("allow-list preview = %+v, %v", p, err)
	}
	// A preview writes nothing.
	if len(w.open()) != 3 {
		t.Error("the preview changed the queue")
	}
}

func TestPartialSelectionLeavesTheRestOpen(t *testing.T) {
	w := newDecisionWorld(t, false)
	id := w.request("listed", "wiki.pages.update", "wiki.pages.delete", "wiki.spaces.create")
	w.mustDecide(id, RequestDecline, "", "wiki.pages.delete")
	open := w.open()
	if len(open) != 1 || !reflect.DeepEqual(open[0].Tools, []string{"wiki.pages.update", "wiki.spaces.create"}) || open[0].ID != id {
		t.Fatalf("open = %+v", open)
	}
	if _, err := w.decide(id, RequestDecline, "", "wiki.pages.get"); err == nil {
		t.Error("a tool outside the request was accepted as a selection")
	}
}

func TestDeclineChangesOnlyTheQueue(t *testing.T) {
	w := newDecisionWorld(t, false)
	id := w.request("listed", "wiki.pages.update")
	before := w.load()
	res := w.mustDecide(id, RequestDecline, "")
	if !reflect.DeepEqual(w.load(), before) || len(w.open()) != 0 || !reflect.DeepEqual(res.Decided, []string{"wiki.pages.update"}) {
		t.Errorf("decline: result %+v", res)
	}
}

func TestBlockWritesBlockedToolsAndClosesTheRequest(t *testing.T) {
	w := newDecisionWorld(t, true)
	id := w.request("vaulted", "wiki.pages.update", "wiki.pages.delete")
	res := w.mustDecide(id, RequestBlock, "", "wiki.pages.update")
	got := w.load().Connections["vaulted"]
	if !reflect.DeepEqual(got.BlockedTools, []string{"wiki.pages.update"}) || !reflect.DeepEqual(got.Effects, []config.Effect{config.EffectRead}) {
		t.Errorf("connection = %+v", got)
	}
	if res.Approval.State != ApprovalNotNeeded {
		t.Errorf("Approval = %+v, a block needs none", res.Approval)
	}
	if open := w.open(); len(open) != 1 || !reflect.DeepEqual(open[0].Tools, []string{"wiki.pages.delete"}) {
		t.Errorf("open = %+v", open)
	}
	// Blocking changed no fingerprint: nothing is open under Approvals.
	report, err := approval.Pending(w.load(), w.v)
	if err != nil || len(report.Open) != 0 {
		t.Errorf("Pending = %+v, %v", report, err)
	}
}

func TestToolOfferedMeanwhileFallsAwayAndADeletedConnectionEndsTheRequest(t *testing.T) {
	w := newDecisionWorld(t, false)
	id := w.request("listed", "wiki.pages.update", "wiki.pages.delete")
	cfg := w.load()
	conn := cfg.Connections["listed"]
	conn.Tools = append(conn.Tools, "wiki.pages.update")
	cfg.Connections["listed"] = conn
	if err := w.store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	res := w.mustDecide(id, RequestAccept, "", "wiki.pages.update", "wiki.pages.delete")
	if !reflect.DeepEqual(res.Decided, []string{"wiki.pages.delete"}) || !reflect.DeepEqual(res.Dropped, []string{"wiki.pages.update"}) {
		t.Errorf("result = %+v", res)
	}
	if got := w.load().Connections["listed"].Tools; !reflect.DeepEqual(got, []string{"wiki.pages.get", "wiki.pages.update", "wiki.pages.delete"}) {
		t.Errorf("Tools = %v", got)
	}
	if !strings.Contains(w.log(), `"operation":"config.request.drop"`) {
		t.Error("the dropped tool was not logged")
	}

	id = w.request("reader", "wiki.pages.update")
	cfg = w.load()
	delete(cfg.Connections, "reader")
	if err := w.store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	var de *RequestDecisionError
	if _, err := w.decide(id, RequestAccept, AcceptAddEffects); !errors.As(err, &de) || de.Code != RequestCodeNotFound {
		t.Errorf("deleted connection = %v, want not-found", err)
	}
	if len(w.open()) != 0 {
		t.Error("the request of a deleted connection stayed open")
	}
}

func TestRevisionConflictChangesNeitherConfigNorQueue(t *testing.T) {
	w := newDecisionWorld(t, false)
	id := w.request("listed", "wiki.pages.update")
	queueBefore := mustRead(t, w.queue.Path())
	// The clock is read right after the decision loaded the configuration: another writer saves in between.
	armed, round := true, 0
	w.svc = w.svc.WithRequests(RequestSource{Queue: w.queue, Tools: w.tools, Now: func() time.Time {
		if armed {
			armed = false
			round++
			cfg := w.load()
			conn := cfg.Connections["listed"]
			conn.Description = "changed by another writer " + strings.Repeat("x", round)
			cfg.Connections["listed"] = conn
			if err := w.store.Save(cfg); err != nil {
				t.Fatal(err)
			}
		}
		return decisionEpoch
	}})
	for _, action := range []RequestAction{RequestAccept, RequestBlock} {
		armed = true
		_, err := w.decide(id, action, "")
		if !errors.Is(err, config.ErrConflict) {
			t.Fatalf("%s on a stale revision = %v, want a conflict", action, err)
		}
	}
	got := w.load().Connections["listed"]
	if !reflect.DeepEqual(got.Tools, []string{"wiki.pages.get"}) || got.BlockedTools != nil {
		t.Errorf("connection = %+v, want only the other writer's change", got)
	}
	if string(mustRead(t, w.queue.Path())) != string(queueBefore) {
		t.Error("a conflict changed the queue")
	}
}

func TestAdminProofIsRequiredForAnEncryptedVault(t *testing.T) {
	w := newDecisionWorld(t, true)
	id := w.request("vaulted", "wiki.pages.update")
	before, queueBefore := w.load(), mustRead(t, w.queue.Path())
	for _, action := range []RequestAction{RequestAccept, RequestDecline, RequestBlock} {
		_, err := w.svc.DecideRequest(context.Background(), AdminProof{}, RequestDecision{
			RequestSelection: RequestSelection{Request: id}, Action: action, Mode: AcceptAddEffects})
		var de *RequestDecisionError
		if !errors.As(err, &de) || de.Code != RequestCodeAdminRequired {
			t.Errorf("%s without proof = %v, want admin-required", action, err)
		}
	}
	if !reflect.DeepEqual(w.load(), before) || string(mustRead(t, w.queue.Path())) != string(queueBefore) {
		t.Error("a refused decision changed something")
	}
	// Without an encrypted vault no proof is asked for.
	plain := newDecisionWorld(t, false)
	pid := plain.request("listed", "wiki.pages.update")
	if _, err := plain.svc.DecideRequest(context.Background(), AdminProof{}, RequestDecision{
		RequestSelection: RequestSelection{Request: pid}, Action: RequestDecline}); err != nil {
		t.Errorf("decline without a vault = %v", err)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestAcceptApprovesAVaultConnectionInTheSameStep(t *testing.T) {
	w := newDecisionWorld(t, true)
	id := w.request("vaulted", "wiki.pages.update")
	res := w.mustDecide(id, RequestAccept, AcceptAddEffects)
	if res.Approval.State != ApprovalGranted {
		t.Fatalf("Approval = %+v, want approved", res.Approval)
	}
	report, err := approval.Pending(w.load(), w.v)
	if err != nil || len(report.Open) != 0 {
		t.Errorf("Pending = %+v, %v, want nothing open", report, err)
	}
	p, err := w.svc.PreviewRequestDecision(RequestSelection{Request: w.request("vaulted", "wiki.pages.delete")})
	if err != nil || !p.ApprovalExpected {
		t.Errorf("preview = %+v, %v, want ApprovalExpected", p, err)
	}
}

func TestFailedApprovalKeepsTheConfigAndSaysItIsOpen(t *testing.T) {
	w := newDecisionWorld(t, true)
	id := w.request("vaulted", "wiki.pages.update")
	// A locked vault handle: the proof stands, but this process cannot approve.
	w.svc = w.svc.WithSecrets(&fakeSecrets{entries: map[string]string{}, vault: vault.New(w.dir)})
	res := w.mustDecide(id, RequestAccept, AcceptAddEffects)
	if res.Approval.State != ApprovalStaysOpen || res.Approval.Detail == "" {
		t.Fatalf("Approval = %+v, want open with a reason", res.Approval)
	}
	if got := w.load().Connections["vaulted"].Effects; !reflect.DeepEqual(got, []config.Effect{config.EffectRead, config.EffectUpdate}) {
		t.Errorf("Effects = %v, the config must stay saved", got)
	}
	if len(w.open()) != 0 {
		t.Error("the accepted request stayed open")
	}
}

func TestAcceptOnAKeyringConnectionAttemptsNoApproval(t *testing.T) {
	w := newDecisionWorld(t, true)
	id := w.request("keyed", "wiki.pages.update")
	res := w.mustDecide(id, RequestAccept, AcceptAddEffects)
	if res.Approval.State != ApprovalNotNeeded {
		t.Errorf("Approval = %+v, want not needed for a keyring connection", res.Approval)
	}
}

func TestDecisionsAreLoggedPerToolWithoutTheReason(t *testing.T) {
	w := newDecisionWorld(t, false)
	id := w.request("listed", "wiki.pages.update", "wiki.pages.delete", "wiki.spaces.create")
	w.mustDecide(id, RequestAccept, "", "wiki.pages.update")
	w.mustDecide(id, RequestDecline, "", "wiki.pages.delete")
	w.mustDecide(id, RequestBlock, "", "wiki.spaces.create")
	log := w.log()
	for _, want := range []string{
		`"operation":"config.request.accept"`, `"tool":"wiki.pages.update"`,
		`"operation":"config.request.decline"`, `"tool":"wiki.pages.delete"`,
		`"operation":"config.request.block"`, `"tool":"wiki.spaces.create"`,
		`"operation":"config.connection.change"`, `"path":"cli"`,
	} {
		if !strings.Contains(log, want) {
			t.Errorf("log lacks %s", want)
		}
	}
	if strings.Contains(log, "reason-text-for-reviewer") {
		t.Error("the request's reason reached the log")
	}
	if n := strings.Count(log, `"operation":"config.request.`); n != 3 {
		t.Errorf("%d request entries, want 3", n)
	}
}

func TestOnlyTheTerminalAndTheTUIDecide(t *testing.T) {
	w := newDecisionWorld(t, false)
	id := w.request("listed", "wiki.pages.update")
	web := New(w.store, newFakeSecrets(), connlog.SurfaceWeb, nil).WithRequests(RequestSource{Queue: w.queue, Tools: w.tools})
	_, err := web.DecideRequest(context.Background(), w.proof, RequestDecision{RequestSelection: RequestSelection{Request: id}, Action: RequestDecline})
	var de *RequestDecisionError
	if !errors.As(err, &de) || de.Code != RequestCodeUnavailable {
		t.Errorf("web decision = %v", err)
	}
}

func TestQueueFailureAfterTheSaveIsAWarningAndTheRequestHealsOnOpen(t *testing.T) {
	w := newDecisionWorld(t, false)
	id := w.request("listed", "wiki.pages.update")
	original := mustRead(t, w.queue.Path())
	// Taking the configuration's lock happens after the request was read and before the queue is settled.
	broken := false
	store := w.store.Observed(config.Observer{LockWaited: func(time.Duration) {
		if !broken {
			broken = true
			if err := os.WriteFile(w.queue.Path(), []byte("version: [not valid"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}})
	svc := New(store, newFakeSecrets(), connlog.SurfaceCLI, &redact.Redactor{}).WithVaultProcessSupport(false).
		WithRequests(RequestSource{Queue: w.queue, Tools: w.tools, Now: func() time.Time { return decisionEpoch }})
	res, err := svc.DecideRequest(context.Background(), w.proof, RequestDecision{
		RequestSelection: RequestSelection{Request: id}, Action: RequestAccept})
	if err != nil {
		t.Fatalf("DecideRequest() = %v, want the decision to count", err)
	}
	if !slices.Contains(w.load().Connections["listed"].Tools, "wiki.pages.update") {
		t.Error("the configuration was not saved")
	}
	if len(res.Warnings) == 0 || !strings.Contains(res.Warnings[0], "queue") {
		t.Errorf("Warnings = %v, want the queue failure", res.Warnings)
	}
	if err := os.WriteFile(w.queue.Path(), original, 0o600); err != nil {
		t.Fatal(err)
	}
	if open := w.open(); len(open) != 0 {
		t.Errorf("open = %+v, want the accepted request pruned", open)
	}
}

func TestAcceptOnAToolsListRefusesAMode(t *testing.T) {
	w := newDecisionWorld(t, false)
	id := w.request("listed", "wiki.pages.update")
	before := w.load()
	var de *RequestDecisionError
	if _, err := w.decide(id, RequestAccept, AcceptAddEffects); !errors.As(err, &de) || de.Code != RequestCodeInvalid {
		t.Fatalf("a mode on a tools list = %v, want invalid-request", err)
	}
	if !reflect.DeepEqual(w.load(), before) || len(w.open()) != 1 {
		t.Error("a refused decision changed something")
	}
}
