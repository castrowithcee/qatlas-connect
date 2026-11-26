package tui

import (
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/accessrequest"
	"github.com/castrowithcee/qatlas-kit/i18n"
)

// askRemove files a request to remove tools as an agent would.
func (w *requestWorld) askRemove(connection, reason string, tools ...string) {
	w.t.Helper()
	cfg := w.config()
	env := accessrequest.Env{Config: cfg, Tools: w.tools, Now: requestEpoch}
	_, err := w.queue.Add(env, accessrequest.Input{
		Kind: accessrequest.KindRemoveTools, Connection: connection, ConnectionID: cfg.Connections[connection].ID,
		Tools: tools, Reason: reason, Project: "/work/demo",
	})
	mustNoError(w.t, err)
}

// setTools gives a connection a tools list.
func (w *requestWorld) setTools(connection string, tools ...string) {
	w.t.Helper()
	cfg := w.config()
	c := cfg.Connections[connection]
	c.Tools = tools
	cfg.Connections[connection] = c
	mustNoError(w.t, w.store.Save(cfg))
}

func TestRequestRemoveDetailShowsKindGroupsAndNoBlock(t *testing.T) {
	w := newRequestWorld(t, "none")
	w.setTools("listed", "wiki.pages.get", "wiki.pages.update", "wiki.spaces.get")
	w.askRemove("listed", "too much", "wiki.pages.update", "wiki.spaces.get")
	w.onRequest()
	view := flatText(w.m.render())
	for _, want := range []string{"Request to remove tools from listed", "Pages (1)", "Spaces (1)", "[x] wiki.pages.update",
		"Tools: 2 of 2 ticked", "accept", "decline"} {
		if !strings.Contains(view, want) {
			t.Errorf("the detail lacks %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "block") {
		t.Errorf("the key line of a removal offers blocking:\n%s", view)
	}
	before := w.config().Connections["listed"]
	press(t, w.m, "b")
	if w.m.requestChoice != nil || w.m.requestDetail == "" || len(w.openRequests()) != 1 ||
		!slices.Equal(w.config().Connections["listed"].BlockedTools, before.BlockedTools) {
		t.Errorf("b acted on a removal: choice %v status %q", w.m.requestChoice, w.m.status)
	}
	// No click target for blocking, one for accepting and declining.
	w.m.render()
	var actions []action
	for _, target := range w.m.frameKeys {
		actions = append(actions, target.Action)
	}
	if slices.Contains(actions, actBlock) || !slices.Contains(actions, actYes) || !slices.Contains(actions, actDecline) {
		t.Errorf("click targets = %v", actions)
	}
	// The list row names the kind.
	w.m.closeRequest()
	w.open()
	if got := flatText(w.m.render()); !strings.Contains(got, "remove tools") {
		t.Errorf("the list does not name the kind:\n%s", got)
	}
}

func TestRequestRemoveAcceptByKeyAndByClick(t *testing.T) {
	for _, byClick := range []bool{false, true} {
		w := newRequestWorld(t, "none")
		w.setTools("listed", "wiki.pages.get", "wiki.pages.update", "wiki.spaces.get")
		w.askRemove("listed", "", "wiki.pages.update", "wiki.spaces.get")
		w.onRequest()
		// Untick wiki.spaces.get: heading, update, heading, get.
		press(t, w.m, "down", "down", "down", "space")
		if byClick {
			w.m.render()
			clicked := false
			for _, target := range w.m.frameKeys {
				if target.Action == actYes {
					clickAt(t, w.m, target.Rect.X+(target.Rect.Width-1)/2, target.Rect.Y)
					clicked = true
					break
				}
			}
			if !clicked {
				t.Fatal("no click target for accept")
			}
		} else {
			press(t, w.m, "y")
		}
		if w.m.requestChoice == nil {
			t.Fatalf("no confirmation, status %q fail %q", w.m.status, w.m.fail)
		}
		view := flatText(w.m.render())
		for _, want := range []string{"Remove the tools from listed", "Remove the ticked tools", "Offers 2 tools afterwards."} {
			if !strings.Contains(view, want) {
				t.Errorf("the confirmation lacks %q:\n%s", want, view)
			}
		}
		if len(w.config().Connections["listed"].Tools) != 3 {
			t.Fatal("the confirmation changed the connection")
		}
		pump(t, w.m, "enter")
		if tools := w.config().Connections["listed"].Tools; !slices.Equal(tools, []string{"wiki.pages.get", "wiki.spaces.get"}) {
			t.Errorf("tools = %v", tools)
		}
		if w.m.screen != screenList || !strings.Contains(w.m.status, "Removed 1 tool(s) from listed") {
			t.Errorf("screen %v, status %q", w.m.screen, w.m.status)
		}
	}
}

func TestRequestRemoveDecline(t *testing.T) {
	w := newRequestWorld(t, "none")
	w.setTools("listed", "wiki.pages.get", "wiki.pages.update")
	w.askRemove("listed", "", "wiki.pages.update")
	w.onRequest()
	pump(t, w.m, "x")
	if got := w.config().Connections["listed"]; !slices.Equal(got.Tools, []string{"wiki.pages.get", "wiki.pages.update"}) || len(got.BlockedTools) != 0 {
		t.Errorf("declining changed the connection: %+v", got)
	}
	if len(w.openRequests()) != 0 || !strings.Contains(w.m.status, "Declined 1 tool(s) for listed") {
		t.Errorf("open %d, status %q", len(w.openRequests()), w.m.status)
	}
}

// A prefix entry that covers a removed tool is split, and a list left empty is named, before anything is saved.
func TestRequestRemoveNamesPrefixSplitAndEmptyList(t *testing.T) {
	w := newRequestWorld(t, "none")
	w.setTools("listed", "wiki.pages.*")
	w.askRemove("listed", "", "wiki.pages.update")
	w.onRequest()
	press(t, w.m, "y")
	view := flatText(w.m.render())
	for _, want := range []string{"The prefix wiki.pages.* is replaced by the exact IDs", "no longer join by themselves", "Offers 2 tools afterwards."} {
		if !strings.Contains(view, want) {
			t.Errorf("the confirmation lacks %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "no tool at all") {
		t.Errorf("a list that keeps tools is named empty:\n%s", view)
	}
	pump(t, w.m, "enter")
	if tools := w.config().Connections["listed"].Tools; !slices.Equal(tools, []string{"wiki.pages.delete", "wiki.pages.get"}) {
		t.Errorf("tools = %v", tools)
	}

	w2 := newRequestWorld(t, "none")
	w2.askRemove("listed", "", "wiki.pages.get")
	w2.onRequest()
	press(t, w2.m, "y")
	if view := flatText(w2.m.render()); !strings.Contains(view, "Offers 0 tools afterwards.") || !strings.Contains(view, "offers no tool at all") {
		t.Errorf("the empty list is not named:\n%s", view)
	}
	pump(t, w2.m, "enter")
	if tools := w2.config().Connections["listed"].Tools; tools == nil || len(tools) != 0 {
		t.Errorf("tools = %#v, want an empty list", tools)
	}
}

// A connection by effect has the one way, the switch to a tools list, with the tools that remain.
func TestRequestRemoveOnAConnectionByEffectSwitchesToAList(t *testing.T) {
	w := newRequestWorld(t, "none")
	w.askRemove("reader", "", "wiki.pages.get")
	w.onRequest()
	press(t, w.m, "y")
	if w.m.requestChoice == nil || len(w.m.requestChoice.modes) != 1 || w.m.requestChoice.modes[0] != "switch-to-list" {
		t.Fatalf("choice = %+v", w.m.requestChoice)
	}
	view := flatText(w.m.render())
	for _, want := range []string{"Switch to a tools list without them", "Offers exactly 1 tools afterwards", "switches it to a tools list"} {
		if !strings.Contains(view, want) {
			t.Errorf("the choice lacks %q:\n%s", want, view)
		}
	}
	pump(t, w.m, "enter")
	got := w.config().Connections["reader"]
	if len(got.Effects) != 0 || !slices.Equal(got.Tools, []string{"wiki.spaces.get"}) {
		t.Errorf("after switching: %+v", got)
	}
}

func TestRequestRemoveInGerman(t *testing.T) {
	w := newRequestWorld(t, "none")
	w.askRemove("reader", "bitte", "wiki.pages.get")
	useLanguage(t, i18n.German)
	w.onRequest()
	view := flatText(w.m.render())
	for _, want := range []string{"Anfrage, Tools aus reader zu entfernen", "ablehnen", "annehmen"} {
		if !strings.Contains(view, want) {
			t.Errorf("the German detail lacks %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "blockieren") {
		t.Errorf("the German key line offers blocking:\n%s", view)
	}
	press(t, w.m, "y")
	view = flatText(w.m.render())
	for _, want := range []string{"Tools aus reader entfernen", "Auf Toolliste ohne diese Tools umstellen", "Bietet danach genau 1 Tools an"} {
		if !strings.Contains(view, want) {
			t.Errorf("the German confirmation lacks %q:\n%s", want, view)
		}
	}
	assertViewFits(t, w.m.render(), 130, 40)
}
