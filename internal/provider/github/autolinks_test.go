package github

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// fakeAutolinks answers the autolink routes of the bound repository and keeps its autolinks by id.
type fakeAutolinks struct {
	mu     sync.Mutex
	f      *fakeGitHub
	items  map[int]string
	nextID int
	// status, when not zero, answers every request of the routes with it; drop closes the connection instead.
	status int
	drop   bool
}

func serveAutolinks(t *testing.T) (*fakeGitHub, *fakeAutolinks, string) {
	t.Helper()
	a := &fakeAutolinks{nextID: 2, items: map[int]string{1: "TICKET-"}}
	f := &fakeGitHub{}
	a.f = f
	f.failure = a.route
	return f, a, serve(t, f)
}

func autolinkJSON(id int, prefix string) string {
	return fmt.Sprintf(`{"id":%d,"key_prefix":%q,"url_template":"https://example.com/%s<num>","is_alphanumeric":true,`+
		`"updated_at":"2026-01-01T00:00:00Z"}`, id, prefix, prefix)
}

func (a *fakeAutolinks) route(w http.ResponseWriter, req *http.Request) bool {
	rest, ok := strings.CutPrefix(req.URL.Path, actionsPrefix)
	if !ok || rest != "autolinks" && !strings.HasPrefix(rest, "autolinks/") {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.drop && req.Method != http.MethodGet {
		if connection, _, err := w.(http.Hijacker).Hijack(); err == nil {
			connection.Close()
		}
		return true
	}
	if a.status != 0 && req.Method != http.MethodGet {
		w.WriteHeader(a.status)
		fmt.Fprint(w, `{"message":"refused"}`)
		return true
	}
	w.Header().Set("Content-Type", "application/json")
	var id int
	if tail, ok := strings.CutPrefix(rest, "autolinks/"); ok {
		fmt.Sscanf(tail, "%d", &id)
	}
	switch {
	case req.Method == http.MethodGet && rest == "autolinks":
		entries := []string{}
		for i := 1; i < a.nextID; i++ {
			if prefix, ok := a.items[i]; ok {
				entries = append(entries, autolinkJSON(i, prefix))
			}
		}
		fmt.Fprintf(w, `[%s]`, strings.Join(entries, ","))
	case req.Method == http.MethodPost && rest == "autolinks":
		requests := a.f.recorded()
		prefix, _ := requests[len(requests)-1].body["key_prefix"].(string)
		a.items[a.nextID] = prefix
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, autolinkJSON(a.nextID, prefix))
		a.nextID++
	case req.Method == http.MethodGet || req.Method == http.MethodDelete:
		prefix, exists := a.items[id]
		if !exists {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
			return true
		}
		if req.Method == http.MethodDelete {
			delete(a.items, id)
			w.WriteHeader(http.StatusNoContent)
			return true
		}
		fmt.Fprint(w, autolinkJSON(id, prefix))
	default:
		return false
	}
	return true
}

func autolinksConfig(base string) *config.Config {
	cfg := coreConfig(base)
	all := []config.Effect{config.EffectRead, config.EffectCreate, config.EffectUpdate, config.EffectDelete}
	tools := []string{autolinksList.ID, autolinksGet.ID, autolinksCreate.ID, autolinksDelete.ID}
	cfg.Connections["repo"] = config.Connection{Service: "gh", Credential: "gh-reader", Target: repoTarget, Effects: all}
	cfg.Connections["repo-listed"] = config.Connection{Service: "gh", Credential: "gh-reader", Target: repoTarget,
		Effects: all, Tools: tools}
	cfg.Connections["foreign"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Target: "repos/octo-org/other", Effects: all, Tools: tools}
	cfg.Connections["ex"] = config.Connection{Service: "gh", Credential: "gh-reader", Effects: all,
		Tools: tools, Exclude: []string{repoTarget}}
	return cfg
}

func TestAutolinksToolsSatisfyTheirContractThroughTheApplicationCore(t *testing.T) {
	f, _, base := serveAutolinks(t)
	red := &redact.Redactor{}
	core := application.New(registry(t), autolinksConfig(base), resolver(red, nil), red)

	listed, err := invoke(t, core, autolinksList.ID, "repo", `{}`, false)
	if err != nil || !strings.Contains(string(listed), `"key_prefix":"TICKET-"`) ||
		!strings.Contains(string(listed), `"updated_at":"2026-01-01T00:00:00Z"`) ||
		!strings.Contains(string(listed), `"repository":"octo-org/example"`) {
		t.Fatalf("list = %s, %v", listed, err)
	}
	got, err := invoke(t, core, autolinksGet.ID, "repo", `{"id":1}`, false)
	if err != nil || !strings.Contains(string(got), `"is_alphanumeric":true`) ||
		!strings.Contains(string(got), `"url_template":"https://example.com/TICKET-\u003cnum\u003e"`) {
		t.Fatalf("get = %s, %v", got, err)
	}
	created, err := invoke(t, core, autolinksCreate.ID, "repo",
		`{"key_prefix":"BUG-","url_template":"https://bugs.example.com/<num>","is_alphanumeric":false}`, true)
	if err != nil || !strings.Contains(string(created), `"id":2`) {
		t.Fatalf("create = %s, %v", created, err)
	}
	sent := f.recorded()[len(f.recorded())-1]
	if sent.method != http.MethodPost || sent.body["is_alphanumeric"] != false ||
		sent.body["url_template"] != "https://bugs.example.com/<num>" {
		t.Errorf("create sent %+v", sent)
	}

	var unsupported *capability.UnsupportedError
	if _, err := invoke(t, core, autolinksDelete.ID, "repo", `{"id":2}`, true); !errors.As(err, &unsupported) {
		t.Errorf("delete without a tools list = %v, want unsupported-capability", err)
	}
	before := len(f.recorded())
	unconfirmed := &application.ConfirmationRequiredError{}
	if _, err := invoke(t, core, autolinksDelete.ID, "repo-listed", `{"id":2}`, false); !errors.As(err, &unconfirmed) ||
		len(f.recorded()) != before {
		t.Errorf("delete without confirmation = %v, requests %d", err, len(f.recorded())-before)
	}
	deleted, err := invoke(t, core, autolinksDelete.ID, "repo-listed", `{"id":2}`, true)
	if err != nil || !strings.Contains(string(deleted), `"deleted":true`) || !strings.Contains(string(deleted), `"id":2`) {
		t.Fatalf("delete = %s, %v", deleted, err)
	}
	// The id is read under the repository first, then exactly one DELETE follows.
	sentAll := f.recorded()[before:]
	if len(sentAll) != 2 || sentAll[0].method != http.MethodGet || sentAll[1].method != http.MethodDelete {
		t.Errorf("delete requests = %+v, want one read then one DELETE", sentAll)
	}
	before = len(f.recorded())
	if _, err := invoke(t, core, autolinksDelete.ID, "repo-listed", `{"id":99}`, true); classOf(err) != provider.ClassNotFound {
		t.Errorf("delete of an unknown id = %v, want not-found", err)
	}
	for _, r := range f.recorded()[before:] {
		if r.method == http.MethodDelete {
			t.Error("delete of an unknown id sent a DELETE")
		}
	}
}

func TestAutolinksValidateBeforeIO(t *testing.T) {
	f, _, base := serveAutolinks(t)
	reads := 0
	red := &redact.Redactor{}
	core := application.New(registry(t), autolinksConfig(base), resolver(red, &reads), red)

	create := func(template string) string {
		return `{"key_prefix":"X-","url_template":"` + template + `"}`
	}
	for _, tt := range []struct{ name, id, connection, arguments string }{
		{"missing id", autolinksGet.ID, "repo", `{}`},
		{"zero id", autolinksGet.ID, "repo", `{"id":0}`},
		{"missing id on delete", autolinksDelete.ID, "repo-listed", `{}`},
		{"missing prefix", autolinksCreate.ID, "repo", `{"url_template":"https://e.com/<num>"}`},
		{"http scheme", autolinksCreate.ID, "repo", create("http://e.com/<num>")},
		{"no placeholder", autolinksCreate.ID, "repo", create("https://e.com/x")},
		{"two placeholders", autolinksCreate.ID, "repo", create("https://e.com/<num>/<num>")},
		{"user info", autolinksCreate.ID, "repo", create("https://user:pw" + "@e.com/<num>")},
		{"no host", autolinksCreate.ID, "repo", create("https:///<num>")},
		{"other scheme", autolinksCreate.ID, "repo", create("javascript:alert(1)//<num>")},
		{"too long", autolinksCreate.ID, "repo", create("https://e.com/" + strings.Repeat("a", 2048) + "<num>")},
		{"foreign target", autolinksGet.ID, "foreign", `{"id":1,"repository":"octo-org/example"}`},
		{"excluded target", autolinksList.ID, "ex", `{"repository":"octo-org/example"}`},
		{"foreign delete", autolinksDelete.ID, "foreign", `{"id":1,"repository":"octo-org/example"}`},
		{"excluded create", autolinksCreate.ID, "ex",
			`{"repository":"octo-org/example","key_prefix":"X-","url_template":"https://e.com/<num>"}`},
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

func TestAutolinksClassifyRefusalsAndNeverRetryAChange(t *testing.T) {
	f, a, base := serveAutolinks(t)
	red := &redact.Redactor{}
	core := application.New(registry(t), autolinksConfig(base), resolver(red, nil), red)
	createArgs := `{"key_prefix":"Z-","url_template":"https://e.com/<num>"}`

	for _, tt := range []struct {
		name   string
		status int
		drop   bool
		class  provider.Class
	}{
		{"forbidden", http.StatusForbidden, false, provider.ClassPermission},
		{"not found", http.StatusNotFound, false, provider.ClassNotFound},
		{"unprocessable", http.StatusUnprocessableEntity, false, ""},
		{"gateway timeout", http.StatusGatewayTimeout, false, provider.ClassTimeout},
		{"dropped connection", 0, true, ""},
	} {
		a.status, a.drop = tt.status, tt.drop
		for id, arguments := range map[string]string{autolinksCreate.ID: createArgs, autolinksDelete.ID: `{"id":1}`} {
			before := len(f.recorded())
			_, err := invoke(t, core, id, "repo-listed", arguments, true)
			if err == nil || (tt.class != "" && classOf(err) != tt.class) {
				t.Errorf("%s %s = %v, want %s", tt.name, id, err, tt.class)
			}
			changes := 0
			for _, r := range f.recorded()[before:] {
				if r.method == http.MethodPost || r.method == http.MethodDelete {
					changes++
				}
			}
			if changes != 1 {
				t.Errorf("%s %s sent %d changes, want exactly one", tt.name, id, changes)
			}
			if (tt.status == http.StatusGatewayTimeout || tt.drop) && !strings.Contains(err.Error(), uncertain) {
				t.Errorf("%s %s = %v, want an uncertain outcome", tt.name, id, err)
			}
		}
	}
}
