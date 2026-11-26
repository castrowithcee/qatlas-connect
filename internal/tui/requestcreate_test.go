package tui

import (
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/accessrequest"
	"github.com/castrowithcee/qatlas-connect/internal/manage"
	"github.com/castrowithcee/qatlas-kit/i18n"
)

// askCreate files a request to create a connection as an agent would.
func (w *requestWorld) askCreate(name, reason string, tools ...string) {
	w.t.Helper()
	env := accessrequest.Env{Config: w.config(), Tools: w.tools, Now: requestEpoch}
	_, err := w.queue.Add(env, accessrequest.Input{
		Kind: accessrequest.KindCreateConnection, Connection: name, Provider: "wiki", Tools: tools, Reason: reason, Project: "/work/demo",
	})
	mustNoError(w.t, err)
}

// walkRequestSetup accepts the open request and walks the setup to its summary, reusing the service and
// credential the configuration has. rename, when set, replaces the prefilled name.
func (w *requestWorld) walkRequestSetup(rename string) {
	w.t.Helper()
	pump(w.t, w.m, "y")
	if w.m.wizard == nil || w.m.wizard.step != stepService || w.m.wizard.create == nil {
		w.t.Fatalf("accepting did not open the setup past its provider: wizard %+v, screen %v, fail %q", w.m.wizard, w.m.screen, w.m.fail)
	}
	press(w.t, w.m, "f2") // service: the configured one
	press(w.t, w.m, "f2") // credential: the configured one
	if got := w.m.fieldValue("name"); got != w.m.wizard.create.Name {
		w.t.Fatalf("the name row = %q, want the requested %q", got, w.m.wizard.create.Name)
	}
	if rename != "" {
		clearField(w.t, w.m)
		typeText(w.t, w.m, rename)
	}
	press(w.t, w.m, "f2") // scope
	if w.m.fail != "" || w.m.wizard.step != stepEffects {
		w.t.Fatalf("scope did not advance: step %d, fail %q", w.m.wizard.step, w.m.fail)
	}
}

func TestRequestCreateDetailShowsTheRequestAndOffersAcceptAndDeclineOnly(t *testing.T) {
	w := newRequestWorld(t, "none")
	w.askCreate("wiki-admin", "needs the admin tools", "wiki.admin.reset", "wiki.pages.get")
	w.onRequest()
	view := flatText(w.m.render())
	for _, want := range []string{"Request to create the connection wiki-admin", "wiki-admin", "wiki", "wiki.admin.reset",
		"Requested tools: 2", "needs the admin tools", "accept", "decline"} {
		if !strings.Contains(view, want) {
			t.Errorf("the detail lacks %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "block") || strings.Contains(view, "[x]") {
		t.Errorf("the detail offers blocking or ticking:\n%s", view)
	}
	var actions []action
	for _, target := range w.m.frameKeys {
		actions = append(actions, target.Action)
	}
	if slices.Contains(actions, actBlock) || !slices.Contains(actions, actYes) || !slices.Contains(actions, actDecline) {
		t.Errorf("click targets = %v", actions)
	}
	press(t, w.m, "b", "space")
	if len(w.openRequests()) != 1 || w.m.requestDetail == "" {
		t.Error("b or space acted on a request to create a connection")
	}
	w.m.closeRequest()
	w.open()
	if got := flatText(w.m.render()); !strings.Contains(got, "create connection") {
		t.Errorf("the list does not name the kind:\n%s", got)
	}
}

func TestRequestCreateDetailInGerman(t *testing.T) {
	w := newRequestWorld(t, "none")
	w.askCreate("wiki-admin", "bitte", "wiki.pages.get")
	useLanguage(t, i18n.German)
	w.onRequest()
	view := flatText(w.m.render())
	for _, want := range []string{"Anfrage, die Connection wiki-admin anzulegen", "gewünschter Name", "Gewünschte Tools: 1", "annehmen", "ablehnen"} {
		if !strings.Contains(view, want) {
			t.Errorf("the German detail lacks %q:\n%s", want, view)
		}
	}
}

// A request that no longer passes the core's check offers only declining.
func TestRequestCreateWithAProblemCannotBeAccepted(t *testing.T) {
	w := newRequestWorld(t, "none")
	w.askCreate("wiki-admin", "", "wiki.pages.get")
	w.onRequest()
	w.m.requestPrefill = &manage.ConnectionPrefill{Problem: "a tool is gone"}
	if keys := w.m.createKeys(); strings.Contains(keys, "{yes}") || !strings.Contains(keys, "{decline}") {
		t.Errorf("keys = %q", keys)
	}
}

func TestRequestCreateAcceptOpensTheSetupPrefilledAndSavingClosesIt(t *testing.T) {
	for _, rename := range []string{"", "renamed-wiki"} {
		w := newRequestWorld(t, "none")
		w.askCreate("wiki-admin", "", "wiki.admin.reset", "wiki.pages.get")
		w.onRequest()
		w.walkRequestSetup(rename)

		// The rights step shows the tools list with the requested tools ticked, not the recommended effects.
		if got := w.m.fieldValue(accessLabel); got != accessTools {
			t.Errorf("access mode = %q, want %q", got, accessTools)
		}
		list := w.m.field(toolListLabel)
		if list == nil || list.hidden {
			t.Fatal("the tools list is not shown")
		}
		if got := list.marked(); !slices.Equal(got, []string{"wiki.admin.reset", "wiki.pages.get"}) {
			t.Errorf("ticked tools = %v", got)
		}
		// Nothing is written before the person saves, and no secret was asked for.
		if len(w.openRequests()) != 1 {
			t.Fatal("the request closed before the connection was saved")
		}
		for _, name := range []string{"wiki-admin", rename} {
			if _, ok := w.config().Connections[name]; ok && name != "" {
				t.Fatalf("connection %q exists before saving", name)
			}
		}
		press(t, w.m, "f2") // rights -> summary
		pump(t, w.m, "f2")  // save
		saved := "wiki-admin"
		if rename != "" {
			saved = rename
		}
		conn, ok := w.config().Connections[saved]
		if !ok {
			t.Fatalf("connection %q was not saved: fail %q status %q", saved, w.m.fail, w.m.status)
		}
		if !slices.Equal(conn.Tools, []string{"wiki.admin.reset", "wiki.pages.get"}) {
			t.Errorf("saved tools = %v", conn.Tools)
		}
		if len(w.openRequests()) != 0 {
			t.Errorf("saving did not close the request: status %q", w.m.status)
		}
		if !strings.Contains(w.m.status, "request accepted and closed") {
			t.Errorf("status = %q", w.m.status)
		}
	}
}

func TestRequestCreateCancelledSetupLeavesTheRequestOpen(t *testing.T) {
	w := newRequestWorld(t, "none")
	w.askCreate("wiki-admin", "", "wiki.pages.get")
	w.onRequest()
	w.walkRequestSetup("")
	// Back to the first step the setup has, which asks before it is dropped.
	press(t, w.m, "esc", "esc", "esc")
	if w.m.wizard == nil || w.m.wizard.step != stepService {
		t.Fatalf("esc went back past the service step: %+v", w.m.wizard)
	}
	press(t, w.m, "esc")
	if w.m.screen != screenLeave {
		t.Fatalf("esc did not ask before dropping the setup: screen %v", w.m.screen)
	}
	press(t, w.m, "d")
	if w.m.wizard != nil || len(w.openRequests()) != 1 {
		t.Errorf("cancelling: wizard %v, open %d", w.m.wizard, len(w.openRequests()))
	}
	if _, ok := w.config().Connections["wiki-admin"]; ok {
		t.Error("a connection was saved")
	}
}

func TestRequestCreateDeclineClosesTheRequest(t *testing.T) {
	w := newRequestWorld(t, "none")
	w.askCreate("wiki-admin", "", "wiki.pages.get", "wiki.spaces.get")
	w.onRequest()
	pump(t, w.m, "x")
	if len(w.openRequests()) != 0 || !strings.Contains(w.m.status, "Declined 2 tool(s) for wiki-admin") {
		t.Errorf("open %d, status %q", len(w.openRequests()), w.m.status)
	}
	if _, ok := w.config().Connections["wiki-admin"]; ok {
		t.Error("declining created a connection")
	}
}

// The prefill puts the requested targets on the scope step and stays free to change.
func TestSetupFromARequestFillsTheTargets(t *testing.T) {
	w := newRequestWorld(t, "none")
	w.m.startSetupFromRequest("req", manage.ConnectionPrefill{
		Provider: "wiki", Name: "wiki-admin", Targets: []string{"space-a", "space-b"}, Tools: []string{"wiki.pages.get"},
	})
	press(t, w.m, "f2", "f2")
	row := targetRowOf(w.m.fields)
	if row == nil || !slices.Equal(row.entries, []string{"space-a", "space-b"}) {
		t.Errorf("target row = %+v", row)
	}
}
