package tui

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-connect/internal/accessrequest"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/connlog"
	"github.com/castrowithcee/qatlas-connect/internal/manage"
	"github.com/castrowithcee/qatlas-connect/internal/vault"
	"github.com/castrowithcee/qatlas-kit/i18n"
)

var requestProvider = config.ProviderMetadata{
	ID: "wiki", Name: "Wiki", DefaultEffects: []config.Effect{config.EffectRead},
	SupportedEffects: config.Effects(), SecretRoles: []config.SecretRole{{Name: "token"}},
	Groups: []config.ToolGroup{{ID: "pages", Title: "Pages"}, {ID: "spaces", Title: "Spaces"}},
	Tools: []config.ToolMetadata{
		{ID: "wiki.admin.reset", Effect: config.EffectExecute, RequiresToolAllowList: true, Group: "pages"},
		{ID: "wiki.pages.delete", Effect: config.EffectDelete, Group: "pages"},
		{ID: "wiki.pages.get", Effect: config.EffectRead, Group: "pages"},
		{ID: "wiki.pages.update", Effect: config.EffectUpdate, Group: "pages"},
		{ID: "wiki.spaces.create", Effect: config.EffectCreate, Group: "spaces"},
		{ID: "wiki.spaces.get", Effect: config.EffectRead, Group: "spaces"},
	},
}

type requestCatalog struct{}

func (requestCatalog) ProviderMetadata(id string) (config.ProviderMetadata, bool) {
	return requestProvider, id == requestProvider.ID
}
func (requestCatalog) ProviderMetadataAll() []config.ProviderMetadata {
	return []config.ProviderMetadata{requestProvider}
}

var requestEpoch = time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)

const requestPassphrase = "synthetic-passphrase-52d1"

// requestWorld is an editor over a configuration with a connection with a tools list ("listed"), one by effect
// ("reader") and, over an encrypted vault, one by effect that reads a vault credential ("vaulted").
type requestWorld struct {
	t     *testing.T
	dir   string
	store *config.Store
	queue *accessrequest.Queue
	tools []accessrequest.Tool
	m     *Model
}

// newRequestWorld builds the world with a vault of the given state: "none" (no vault at all), "absent",
// "locked" (encrypted, not unlocked in this process) or "unlocked" (encrypted and unlocked).
func newRequestWorld(t *testing.T, vaultState string) *requestWorld {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "qconnect")
	w := &requestWorld{t: t, dir: dir, store: config.NewStore(filepath.Join(dir, "config.yaml"), requestCatalog{})}
	cfg := w.store.New()
	cfg.Services = map[string]config.Service{"svc": {Provider: "wiki", BaseURL: "https://wiki.example.test"}}
	cfg.Credentials = map[string]config.Credential{"kcred": {Provider: "wiki", Type: config.CredentialTypeKeyring}}
	cfg.Connections = map[string]config.Connection{
		"listed": {Service: "svc", Credential: "kcred", Tools: []string{"wiki.pages.get"}},
		"reader": {Service: "svc", Credential: "kcred", Effects: []config.Effect{config.EffectRead}},
	}
	encrypted := vaultState == "locked" || vaultState == "unlocked"
	if encrypted {
		cfg.Credentials["vcred"] = config.Credential{Provider: "wiki", Type: config.CredentialTypeVault}
		cfg.Connections["vaulted"] = config.Connection{Service: "svc", Credential: "vcred", Effects: []config.Effect{config.EffectRead}}
	}
	mustNoError(t, w.store.Save(cfg))
	for _, tool := range requestProvider.Tools {
		w.tools = append(w.tools, accessrequest.Tool{ToolMetadata: tool, Provider: "wiki"})
	}
	var err error
	w.queue, err = accessrequest.NewQueue(filepath.Join(dir, accessrequest.FileName))
	mustNoError(t, err)

	secrets, _ := newResolver(t, dir, nil)
	switch vaultState {
	case "absent":
		secrets = secrets.WithVault(vault.New(dir), nil)
	case "unencrypted":
		setup, _ := newVaultResolver(t, dir)
		mustNoError(t, setup.SetVault("other", "role", "synthetic-seed", nil))
		secrets, _ = newVaultResolver(t, dir)
	case "locked", "unlocked":
		setup, _ := newVaultResolver(t, dir)
		mustNoError(t, setup.SetVault("vcred", "token", "synthetic-token-value", func(string) (string, error) { return requestPassphrase, nil }))
		if vaultState == "unlocked" {
			secrets = setup
		} else {
			secrets, _ = newVaultResolver(t, dir)
		}
	}
	svc := manage.New(w.store, secrets, connlog.SurfaceTUI, nil).WithVaultProcessSupport(false).
		WithRequests(manage.RequestSource{Queue: w.queue, Tools: w.tools, Now: func() time.Time { return requestEpoch }})
	m, err := New(svc, w.store, nil, secrets, nil)
	mustNoError(t, err)
	m.now = func() time.Time { return requestEpoch }
	m.Update(tea.WindowSizeMsg{Width: 130, Height: 40})
	w.m = m
	return w
}

// ask files a request as an agent would.
func (w *requestWorld) ask(connection, reason string, tools ...string) {
	w.t.Helper()
	cfg, err := w.store.Load()
	mustNoError(w.t, err)
	env := accessrequest.Env{Config: cfg, Tools: w.tools, Now: requestEpoch}
	_, err = w.queue.Add(env, accessrequest.Input{
		Connection: connection, ConnectionID: cfg.Connections[connection].ID, Tools: tools, Reason: reason, Project: "/work/demo",
	})
	mustNoError(w.t, err)
}

func (w *requestWorld) open() {
	w.t.Helper()
	openSectionByName(w.t, w.m, sectionApprovals)
}

// onRequest moves the cursor onto the first request row and opens it.
func (w *requestWorld) onRequest() {
	w.t.Helper()
	w.open()
	name, ok := w.m.selected()
	if _, isRequest := requestIDOf(name); !ok || !isRequest {
		w.t.Fatalf("selected row = %q, want a request", name)
	}
	pump(w.t, w.m, "enter")
	if w.m.requestDetail == "" {
		w.t.Fatalf("enter did not open the request: screen %v", w.m.screen)
	}
}

func (w *requestWorld) config() *config.Config {
	w.t.Helper()
	cfg, err := w.store.Load()
	mustNoError(w.t, err)
	return cfg
}

func (w *requestWorld) openRequests() []accessrequest.Request {
	w.t.Helper()
	cfg := w.config()
	open, _, err := w.queue.Open(accessrequest.Env{Config: cfg, Tools: w.tools, Now: requestEpoch})
	mustNoError(w.t, err)
	return open
}

// Requests are listed under Approvals whatever the state of the vault, and the note about the vault belongs
// to the connection approvals alone.
func TestRequestsShowForEveryStateOfTheVault(t *testing.T) {
	for _, tt := range []struct {
		state string
		note  string
	}{
		{"none", "Connection approvals:"},
		{"absent", "Connection approvals:"},
		{"unencrypted", "Connection approvals:"},
		{"locked", "Connection approvals:"},
		{"unlocked", ""},
	} {
		t.Run(tt.state, func(t *testing.T) {
			w := newRequestWorld(t, tt.state)
			w.ask("reader", "", "wiki.pages.delete", "wiki.spaces.create")
			w.open()
			view := flatText(w.m.render())
			for _, want := range []string{"reader", "request", "tools: 2", "expires in 14d", "project /work/demo"} {
				if !strings.Contains(view, want) {
					t.Errorf("the list lacks %q:\n%s", want, view)
				}
			}
			if got := strings.Contains(view, "Connection approvals:"); got != (tt.note != "") {
				t.Errorf("note about the connection approvals shown = %v, want %v:\n%s", got, tt.note != "", view)
			}
			// The list's enter opens the request; it is not the unlock key of the empty list.
			if tt.state == "locked" && (!strings.Contains(view, "enter choose") || strings.Contains(view, "enter unlock")) {
				t.Errorf("a locked vault does not take enter away from the request:\n%s", view)
			}
		})
	}
}

// With no request the part of the requests is empty and the placeholder is the whole list, as before.
func TestNoRequestKeepsThePlaceholderOfTheApprovals(t *testing.T) {
	w := newRequestWorld(t, "absent")
	w.open()
	if view := flatText(w.m.render()); !strings.Contains(view, "Approvals are not used") || strings.Contains(view, "Connection approvals:") {
		t.Errorf("empty section:\n%s", view)
	}
}

// A request that an agent files while the editor runs shows after the next switch to Approvals.
func TestRequestFiledOutsideShowsAfterSwitchingToApprovals(t *testing.T) {
	w := newRequestWorld(t, "unlocked")
	w.open()
	if len(w.m.requests) != 0 {
		t.Fatalf("requests = %d before anyone asked", len(w.m.requests))
	}
	w.m.screen = screenNav
	press(t, w.m, "1")
	w.ask("reader", "", "wiki.pages.delete")
	w.open()
	if len(w.m.requests) != 1 || !strings.Contains(flatText(w.m.render()), "tools: 1") {
		t.Errorf("the new request is not shown:\n%s", w.m.render())
	}
	// The periodic refresh of the header reads the queue as well.
	w.ask("listed", "", "wiki.spaces.get")
	_, cmd := w.m.Update(vaultTickMsg{})
	_ = cmd
	if len(w.m.requests) != 2 || len(w.m.names) < 2 {
		t.Errorf("the tick did not refresh the requests: %d, list %v", len(w.m.requests), w.m.names)
	}
}

// The detail shows the tools in the groups of the tool picker, all ticked, and the untrusted reason as such.
func TestRequestDetailShowsGroupsTicksAndReason(t *testing.T) {
	w := newRequestWorld(t, "none")
	w.ask("reader", "needed\nto \x1b[31mclean up", "wiki.pages.delete", "wiki.pages.update", "wiki.spaces.create")
	w.onRequest()
	view := flatText(w.m.render())
	for _, want := range []string{"Pages (2)", "Spaces (1)", "[x] wiki.pages.delete", "[x] wiki.spaces.create",
		"Tools: 3 of 3 ticked", "not verified", `"needed\nto \u001b[31mclean up"`, "/work/demo"} {
		if !strings.Contains(view, want) {
			t.Errorf("the detail lacks %q:\n%s", want, view)
		}
	}
	if strings.Contains(w.m.render(), "\x1b[31mclean") {
		t.Error("the reason reached the screen with its control character")
	}
	// A group heading ticks and unticks its tools.
	press(t, w.m, "space")
	if got := flatText(w.m.render()); !strings.Contains(got, "Tools: 1 of 3 ticked") || !strings.Contains(got, "[ ] wiki.pages.delete") {
		t.Errorf("space on the heading of Pages:\n%s", got)
	}
	press(t, w.m, "a")
	if got := flatText(w.m.render()); !strings.Contains(got, "Tools: 3 of 3 ticked") {
		t.Errorf("a does not tick all:\n%s", got)
	}
	press(t, w.m, "a")
	press(t, w.m, "y")
	if len(w.openRequests()) != 1 || !strings.Contains(w.m.status, "Tick at least one tool") {
		t.Errorf("deciding with nothing ticked: status %q", w.m.status)
	}
}

// Accepting the ticked tools of a connection with a tools list adds them, and the unticked stay open.
func TestRequestAcceptTickedToolsOfAListedConnection(t *testing.T) {
	w := newRequestWorld(t, "none")
	w.ask("listed", "", "wiki.pages.delete", "wiki.pages.update", "wiki.spaces.create")
	w.onRequest()
	// Untick wiki.pages.update: heading, delete, update.
	press(t, w.m, "down", "down", "space")
	pump(t, w.m, "y")
	tools := w.config().Connections["listed"].Tools
	if !slices.Equal(tools, []string{"wiki.pages.get", "wiki.pages.delete", "wiki.spaces.create"}) {
		t.Errorf("tools = %v", tools)
	}
	open := w.openRequests()
	if len(open) != 1 || !slices.Equal(open[0].Tools, []string{"wiki.pages.update"}) {
		t.Errorf("open requests = %+v, want only wiki.pages.update", open)
	}
	if w.m.screen != screenList || !strings.Contains(w.m.status, "Accepted 2 tool(s) for listed") ||
		!strings.Contains(w.m.status, "1 tool(s) stay open") {
		t.Errorf("screen %v, status %q", w.m.screen, w.m.status)
	}
}

func TestRequestDeclineAndBlock(t *testing.T) {
	w := newRequestWorld(t, "none")
	w.ask("listed", "", "wiki.pages.delete")
	w.onRequest()
	before := w.config().Connections["listed"]
	pump(t, w.m, "x")
	if got := w.config().Connections["listed"]; !slices.Equal(got.Tools, before.Tools) || len(got.BlockedTools) != 0 {
		t.Errorf("declining changed the connection: %+v", got)
	}
	if len(w.openRequests()) != 0 || !strings.Contains(w.m.status, "Declined 1 tool(s) for listed") {
		t.Errorf("open %d, status %q", len(w.openRequests()), w.m.status)
	}

	w.ask("listed", "", "wiki.pages.delete")
	w.onRequest()
	pump(t, w.m, "b")
	got := w.config().Connections["listed"]
	if !slices.Equal(got.BlockedTools, []string{"wiki.pages.delete"}) || !slices.Equal(got.Tools, before.Tools) {
		t.Errorf("blocking: %+v", got)
	}
	if len(w.openRequests()) != 0 || !strings.Contains(w.m.status, "Blocked 1 tool(s) for listed") {
		t.Errorf("open %d, status %q", len(w.openRequests()), w.m.status)
	}
}

// On a connection by effect, accepting asks how, with the consequence of each way from the core's preview.
func TestRequestAcceptOnAConnectionByEffectAsksHow(t *testing.T) {
	w := newRequestWorld(t, "none")
	w.ask("reader", "", "wiki.pages.delete", "wiki.spaces.create")
	w.onRequest()
	press(t, w.m, "y")
	if w.m.requestChoice == nil {
		t.Fatalf("no choice opened, status %q fail %q", w.m.status, w.m.fail)
	}
	view := flatText(w.m.render())
	for _, want := range []string{"Add the effects", "Switch to a tools list", "create: 1 tools now", "delete: 1 tools now",
		"every tool of this effect registered later", "Offers exactly 4 tools", "no longer join by themselves"} {
		if !strings.Contains(view, want) {
			t.Errorf("the choice lacks %q:\n%s", want, view)
		}
	}
	if len(w.config().Connections["reader"].Effects) != 1 {
		t.Error("the choice changed the connection before it was made")
	}
	press(t, w.m, "esc")
	if w.m.requestChoice != nil || w.m.requestDetail == "" {
		t.Error("esc does not return from the choice to the detail")
	}

	// Add the effects.
	press(t, w.m, "y")
	pump(t, w.m, "enter")
	got := w.config().Connections["reader"]
	if got.Tools != nil || !slices.Equal(got.Effects, []config.Effect{config.EffectRead, config.EffectCreate, config.EffectDelete}) {
		t.Errorf("after adding the effects: %+v", got)
	}
}

func TestRequestSwitchToAToolsList(t *testing.T) {
	w := newRequestWorld(t, "none")
	w.ask("reader", "", "wiki.pages.delete")
	w.onRequest()
	press(t, w.m, "y", "down")
	pump(t, w.m, "enter")
	got := w.config().Connections["reader"]
	if len(got.Effects) != 0 || !slices.Equal(got.Tools, []string{"wiki.pages.delete", "wiki.pages.get", "wiki.spaces.get"}) {
		t.Errorf("after switching: %+v", got)
	}
}

// A tool that needs its exact ID leaves only the switch to a tools list.
func TestRequestOfAToolWithAnExplicitNameOffersOnlySwitching(t *testing.T) {
	w := newRequestWorld(t, "none")
	w.ask("reader", "", "wiki.admin.reset")
	w.onRequest()
	press(t, w.m, "y")
	if w.m.requestChoice == nil || len(w.m.requestChoice.modes) != 1 || w.m.requestChoice.modes[0] != manage.AcceptSwitchToList {
		t.Fatalf("choice = %+v", w.m.requestChoice)
	}
	view := flatText(w.m.render())
	if strings.Contains(view, "Add the effects") || !strings.Contains(view, "Not possible: wiki.admin.reset can only be released by its exact ID") {
		t.Errorf("the choice:\n%s", view)
	}
	pump(t, w.m, "enter")
	if !slices.Contains(w.config().Connections["reader"].Tools, "wiki.admin.reset") {
		t.Errorf("tools = %v", w.config().Connections["reader"].Tools)
	}
}

// With an encrypted vault every decision goes through the admin session, and accepting approves the
// connection in the same step.
func TestRequestDecisionNeedsTheAdminSessionAndApproves(t *testing.T) {
	w := newRequestWorld(t, "locked")
	w.ask("vaulted", "", "wiki.pages.delete")
	w.onRequest()
	pump(t, w.m, "y")
	if w.m.requestChoice == nil {
		t.Fatalf("the choice did not open: screen %v", w.m.screen)
	}
	pump(t, w.m, "enter")
	if w.m.screen != screenAdminAuth {
		t.Fatalf("screen = %v, want the admin passphrase dialog", w.m.screen)
	}
	if len(w.config().Connections["vaulted"].Effects) != 1 {
		t.Fatal("the connection changed before the passphrase")
	}
	// A cancelled dialog returns to the choice with its state.
	press(t, w.m, "esc")
	if w.m.requestDetail == "" || w.m.requestChoice == nil || w.m.screen != screenConfirm {
		t.Fatalf("after esc: screen %v detail %q", w.m.screen, w.m.requestDetail)
	}
	pump(t, w.m, "enter")
	typeText(t, w.m, requestPassphrase)
	pump(t, w.m, "enter")
	if w.m.fail != "" {
		t.Fatalf("fail = %q", w.m.fail)
	}
	if !strings.Contains(w.m.status, "Accepted 1 tool(s) for vaulted") || !strings.Contains(w.m.status, "connection approved") {
		t.Errorf("status = %q", w.m.status)
	}
	if !isApproved(t, w.dir, requestPassphrase, w.config(), "vaulted") {
		t.Error("accepting did not approve the connection")
	}
}

// The overview counts the open requests by themselves and names them as the first next step.
func TestOverviewCountsRequestsAndNamesThemAsNextStep(t *testing.T) {
	w := newRequestWorld(t, "none")
	w.ask("reader", "", "wiki.pages.delete")
	w.ask("listed", "", "wiki.spaces.get")
	w.m.screen = screenNav
	openSectionByName(t, w.m, sectionOverview)
	var found bool
	for _, tile := range w.m.overviewTiles() {
		if tile.label == "Requests" {
			found = true
			if tile.value != "2" || tile.target != sectionApprovals {
				t.Errorf("requests tile = %+v", tile)
			}
		}
	}
	if !found {
		t.Fatal("no requests tile")
	}
	steps := w.m.nextSteps()
	if len(steps) == 0 || !strings.Contains(steps[0].text, "open tool requests (2): decide them in Access > Approvals") || steps[0].target != sectionApprovals {
		t.Errorf("next steps = %+v", steps)
	}
	if view := flatText(w.m.render()); !strings.Contains(view, "Requests") || !strings.Contains(view, "open tool requests (2)") {
		t.Errorf("overview:\n%s", view)
	}
}

// The German texts and the key line of the detail read in both languages.
func TestRequestDetailInGerman(t *testing.T) {
	w := newRequestWorld(t, "none")
	w.ask("reader", "bitte", "wiki.pages.delete")
	useLanguage(t, i18n.German)
	w.onRequest()
	view := flatText(w.m.render())
	for _, want := range []string{"Tool-Anfrage für reader", "nicht geprüft", "alle/keine", "ablehnen", "blockieren", "annehmen"} {
		if !strings.Contains(view, want) {
			t.Errorf("the German detail lacks %q:\n%s", want, view)
		}
	}
}

// Both screens of a decision fit a small terminal.
func TestRequestScreensFitASmallTerminal(t *testing.T) {
	w := newRequestWorld(t, "none")
	var tools []string
	for _, tool := range requestProvider.Tools {
		if tool.Effect != config.EffectRead {
			tools = append(tools, tool.ID)
		}
	}
	w.ask("reader", strings.Repeat("a long reason ", 30), tools...)
	w.m.Update(tea.WindowSizeMsg{Width: 60, Height: 24})
	w.onRequest()
	assertViewFits(t, w.m.render(), 60, 24)
	press(t, w.m, "end")
	view := w.m.render()
	assertViewFits(t, view, 60, 24)
	if !strings.Contains(flatText(view), "> [x]") {
		t.Errorf("the cursor row is not in view after end:\n%s", view)
	}
	press(t, w.m, "y")
	assertViewFits(t, w.m.render(), 60, 24)
}
