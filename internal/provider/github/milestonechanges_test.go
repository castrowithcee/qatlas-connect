package github

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-kit/redact"
)

func milestonesConfig(base string) *config.Config {
	cfg := coreConfig(base)
	all := []config.Effect{config.EffectRead, config.EffectCreate, config.EffectUpdate, config.EffectDelete}
	cfg.Connections["repo"] = config.Connection{Service: "gh", Credential: "gh-reader", Target: repoTarget, Effects: all}
	cfg.Connections["repo-listed"] = config.Connection{Service: "gh", Credential: "gh-reader", Target: repoTarget,
		Effects: all, Tools: []string{milestonesDelete.ID}}
	cfg.Connections["owner"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Targets: []string{"orgs/octo-org"}, Exclude: []string{"repos/octo-org/secret"}, Effects: all,
		Tools: []string{milestonesDelete.ID}}
	cfg.Connections["owner-open"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Targets: []string{"orgs/octo-org"}, Exclude: []string{"repos/octo-org/secret"}, Effects: all}
	return cfg
}

// serveMilestoneRoutes answers every milestone route below the bound repository with the given handler.
func serveMilestoneRoutes(t *testing.T, answer func(w http.ResponseWriter, req *http.Request)) (*fakeGitHub, string) {
	t.Helper()
	f := &fakeGitHub{}
	f.failure = func(w http.ResponseWriter, req *http.Request) bool {
		rest, ok := strings.CutPrefix(req.URL.Path, actionsPrefix)
		if !ok || rest != "milestones" && !strings.HasPrefix(rest, "milestones/") {
			return false
		}
		w.Header().Set("Content-Type", "application/json")
		answer(w, req)
		return true
	}
	return f, serve(t, f)
}

func TestMilestoneToolsSatisfyTheirContractThroughTheApplicationCore(t *testing.T) {
	f, base := serveMilestoneRoutes(t, func(w http.ResponseWriter, req *http.Request) {
		switch {
		case req.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		case req.Method == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, documentedMilestone)
		default:
			fmt.Fprint(w, documentedMilestone)
		}
	})
	red := &redact.Redactor{}
	core := application.New(registry(t), milestonesConfig(base), resolver(red, nil), red)

	got, err := invoke(t, core, milestonesGet.ID, "repo", `{"number":1}`, false)
	if err != nil || !strings.Contains(string(got), `"title":"v1.0"`) || !strings.Contains(string(got), `"closed_issues":8`) ||
		!strings.Contains(string(got), `"creator"`) || !strings.Contains(string(got), `"node_id":"MDk6TWlsZXN0b25lMTAwMjYwNA=="`) {
		t.Fatalf("get = %s, %v", got, err)
	}
	created, err := invoke(t, core, milestonesCreate.ID, "repo",
		`{"title":"v1.0","state":"open","description":"d","due_on":"2026-03-01T00:00:00Z"}`, true)
	if err != nil || !strings.Contains(string(created), `"number":1`) {
		t.Fatalf("create = %s, %v", created, err)
	}
	if body := f.recorded()[len(f.recorded())-1]; body.method != http.MethodPost || body.body["title"] != "v1.0" ||
		body.body["due_on"] != "2026-03-01T00:00:00Z" || body.body["state"] != "open" {
		t.Errorf("create request = %+v", body)
	}
	if _, err := invoke(t, core, milestonesCreate.ID, "repo", `{"title":"v1.0"}`, false); err == nil {
		t.Error("create without confirmation succeeded")
	}
	updated, err := invoke(t, core, milestonesUpdate.ID, "repo", `{"number":1,"state":"closed"}`, true)
	if err != nil || !strings.Contains(string(updated), `"number":1`) {
		t.Fatalf("update = %s, %v", updated, err)
	}
	if last := f.recorded()[len(f.recorded())-1]; last.method != http.MethodPatch || len(last.body) != 1 ||
		last.body["state"] != "closed" {
		t.Errorf("update request = %+v, want only the given value", last)
	}
	cleared, err := invoke(t, core, milestonesUpdate.ID, "repo", `{"number":1,"description":""}`, true)
	if err != nil {
		t.Fatalf("clear description = %s, %v", cleared, err)
	}
	if last := f.recorded()[len(f.recorded())-1]; last.body["description"] != "" {
		t.Errorf("clear request = %+v", last)
	}

	var unsupported *capability.UnsupportedError
	if _, err := invoke(t, core, milestonesDelete.ID, "repo", `{"number":1}`, true); !errors.As(err, &unsupported) {
		t.Errorf("delete without a tools list = %v, want unsupported-capability", err)
	}
	before := len(f.recorded())
	unconfirmed := &application.ConfirmationRequiredError{}
	if _, err := invoke(t, core, milestonesDelete.ID, "repo-listed", `{"number":1}`, false); !errors.As(err, &unconfirmed) ||
		len(f.recorded()) != before {
		t.Errorf("delete without confirmation = %v", err)
	}
	deleted, err := invoke(t, core, milestonesDelete.ID, "repo-listed", `{"number":1}`, true)
	if err != nil || !strings.Contains(string(deleted), `"deleted":true`) || !strings.Contains(string(deleted), `"number":1`) {
		t.Fatalf("delete = %s, %v", deleted, err)
	}
	if last := f.recorded()[len(f.recorded())-1]; last.method != http.MethodDelete ||
		!strings.HasSuffix(last.path, "/milestones/1") {
		t.Errorf("delete request = %+v", last)
	}
}

func TestMilestoneArgumentsAndTargetsAreCheckedBeforeIO(t *testing.T) {
	f, base := serveMilestoneRoutes(t, func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, documentedMilestone) })
	reads := 0
	red := &redact.Redactor{}
	core := application.New(registry(t), milestonesConfig(base), resolver(red, &reads), red)
	for _, tt := range []struct{ name, id, connection, arguments string }{
		{"get without number", milestonesGet.ID, "repo", `{}`},
		{"get with number 0", milestonesGet.ID, "repo", `{"number":0}`},
		{"create without title", milestonesCreate.ID, "repo", `{}`},
		{"empty title", milestonesCreate.ID, "repo", `{"title":""}`},
		{"title too long", milestonesCreate.ID, "repo", `{"title":"` + strings.Repeat("x", 256) + `"}`},
		{"control character", milestonesCreate.ID, "repo", "{\"title\":\"a\x01b\"}"},
		{"bad state", milestonesCreate.ID, "repo", `{"title":"x","state":"all"}`},
		{"bad due_on", milestonesCreate.ID, "repo", `{"title":"x","due_on":"tomorrow"}`},
		{"null due_on", milestonesUpdate.ID, "repo", `{"number":1,"due_on":null}`},
		{"description too long", milestonesCreate.ID, "repo",
			`{"title":"x","description":"` + strings.Repeat("x", 65537) + `"}`},
		{"update without a value", milestonesUpdate.ID, "repo", `{"number":1}`},
		{"update without number", milestonesUpdate.ID, "repo", `{"state":"open"}`},
		{"delete without number", milestonesDelete.ID, "repo-listed", `{}`},
		{"foreign repository get", milestonesGet.ID, "repo", `{"number":1,"repository":"other/example"}`},
		{"foreign repository delete", milestonesDelete.ID, "repo-listed", `{"number":1,"repository":"other/example"}`},
		{"excluded repository update", milestonesUpdate.ID, "owner-open",
			`{"number":1,"state":"open","repository":"octo-org/secret"}`},
		{"excluded repository delete", milestonesDelete.ID, "owner", `{"number":1,"repository":"octo-org/secret"}`},
		{"project target", milestonesGet.ID, "planning", `{"number":1}`},
	} {
		reads = 0
		before := len(f.recorded())
		if _, err := invoke(t, core, tt.id, tt.connection, tt.arguments, true); !isInvalidRequest(err) {
			t.Errorf("%s: %v, want an invalid request", tt.name, err)
		}
		if reads != 0 || len(f.recorded()) != before {
			t.Errorf("%s reached the credential or GitHub", tt.name)
		}
	}
}

func TestMilestoneFailuresNameNoProviderTextAndChangesAreSentOnce(t *testing.T) {
	for name, tt := range map[string]struct {
		answer    func(http.ResponseWriter)
		class     provider.Class
		uncertain bool
	}{
		"not found": {func(w http.ResponseWriter) { w.WriteHeader(http.StatusNotFound); fmt.Fprint(w, `{"message":"canary"}`) },
			provider.ClassNotFound, false},
		"forbidden": {func(w http.ResponseWriter) {
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"message":"canary"}`)
		},
			provider.ClassPermission, false},
		"invalid": {func(w http.ResponseWriter) {
			w.WriteHeader(http.StatusUnprocessableEntity)
			fmt.Fprint(w, `{"message":"canary","errors":[{"code":"already_exists","field":"title"}]}`)
		}, "", false},
		"gateway timeout": {func(w http.ResponseWriter) { w.WriteHeader(http.StatusGatewayTimeout) },
			provider.ClassTimeout, true},
		"server error": {func(w http.ResponseWriter) { w.WriteHeader(http.StatusBadGateway) },
			provider.ClassProviderError, true},
		"unreadable answer": {func(w http.ResponseWriter) { fmt.Fprint(w, `{"number":`) },
			provider.ClassInvalidResponse, true},
		"dropped connection": {func(w http.ResponseWriter) {
			if connection, _, err := w.(http.Hijacker).Hijack(); err == nil {
				connection.Close()
			}
		}, "", true},
	} {
		t.Run(name, func(t *testing.T) {
			f, base := serveMilestoneRoutes(t, func(w http.ResponseWriter, _ *http.Request) { tt.answer(w) })
			red := &redact.Redactor{}
			core := application.New(registry(t), milestonesConfig(base), resolver(red, nil), red)
			for _, call := range []struct{ id, args string }{
				{milestonesCreate.ID, `{"title":"x"}`},
				{milestonesUpdate.ID, `{"number":1,"state":"closed"}`},
				{milestonesDelete.ID, `{"number":1}`},
			} {
				if call.id == milestonesDelete.ID && name == "unreadable answer" {
					continue // a delete answers without a body, so there is nothing to read
				}
				before := len(f.recorded())
				connection := "repo"
				if call.id == milestonesDelete.ID {
					connection = "repo-listed"
				}
				_, err := invoke(t, core, call.id, connection, call.args, true)
				if err == nil || strings.Contains(err.Error(), "canary") {
					t.Errorf("%s = %v, want an error without provider text", call.id, err)
					continue
				}
				isUncertain := strings.Contains(err.Error(), uncertain)
				if isUncertain != tt.uncertain {
					t.Errorf("%s = %v, uncertain = %t, want %t", call.id, err, isUncertain, tt.uncertain)
				}
				if tt.class != "" && classOf(err) != tt.class {
					t.Errorf("%s class = %q, want %q", call.id, classOf(err), tt.class)
				}
				if n := len(f.recorded()) - before; n != 1 {
					t.Errorf("%s sent %d requests, want exactly one", call.id, n)
				}
			}
			before := len(f.recorded())
			_, err := invoke(t, core, milestonesGet.ID, "repo", `{"number":1}`, false)
			if err == nil || strings.Contains(err.Error(), "canary") {
				t.Errorf("get = %v", err)
			}
			if tt.class != "" && name != "unreadable answer" && classOf(err) != tt.class {
				t.Errorf("get class = %q, want %q", classOf(err), tt.class)
			}
			if len(f.recorded())-before < 1 {
				t.Error("get sent no request")
			}
		})
	}
}
