package github

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const limitsProviderCanary = "limits-provider-text-canary-7e2a"

const (
	limitsRepoRoute    = "/repos/octo-org/example/interaction-limits"
	limitsOrgRoute     = "/orgs/octo-org/interaction-limits"
	limitsAccountRoute = "/user/interaction-limits"
)

// fakeLimits answers the three interaction limit routes, keeps what they hold, and can answer every request of
// one method with a status.
type fakeLimits struct {
	mu     sync.Mutex
	f      *fakeGitHub
	state  map[string]map[string]any
	status map[string]int
	// emptyBody answers a read without a limit with no body at all instead of an empty object.
	emptyBody bool
}

func serveLimits(t *testing.T) (*fakeGitHub, *fakeLimits, string) {
	t.Helper()
	l := &fakeLimits{status: map[string]int{}, state: map[string]map[string]any{}}
	f := &fakeGitHub{}
	l.f = f
	f.failure = l.route
	return f, l, serve(t, f)
}

func (l *fakeLimits) route(w http.ResponseWriter, req *http.Request) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	path := strings.TrimPrefix(req.URL.Path, "/api/v3")
	if path != limitsRepoRoute && path != limitsOrgRoute && path != limitsAccountRoute {
		return false
	}
	w.Header().Set("Content-Type", "application/json")
	if status := l.status[req.Method]; status != 0 {
		w.WriteHeader(status)
		fmt.Fprintf(w, `{"message":%q}`, limitsProviderCanary)
		return true
	}
	switch req.Method {
	case http.MethodGet:
		current, ok := l.state[path]
		if !ok && l.emptyBody {
			return true
		}
		if !ok {
			current = map[string]any{}
		}
		data, _ := json.Marshal(current)
		w.Write(data)
	case http.MethodPut:
		requests := l.f.recorded()
		body := requests[len(requests)-1].body
		origin := map[string]string{limitsRepoRoute: "repository", limitsOrgRoute: "organization",
			limitsAccountRoute: "user"}[path]
		l.state[path] = map[string]any{"limit": body["limit"], "origin": origin,
			"expires_at": "2026-11-20T10:00:00Z"}
		data, _ := json.Marshal(l.state[path])
		w.Write(data)
	case http.MethodDelete:
		delete(l.state, path)
		w.WriteHeader(http.StatusNoContent)
	default:
		return false
	}
	return true
}

func containsAll(text string, parts []string) bool {
	for _, part := range parts {
		if !strings.Contains(text, part) {
			return false
		}
	}
	return true
}

var limitTools = []string{interactionLimitsGet.ID, interactionLimitsSet.ID, interactionLimitsRemove.ID}

// limitsConfig binds the shapes of connection the interaction limit tools distinguish. None lists tools: the
// tools are offered by their effect.
func limitsConfig(base string) *config.Config {
	cfg := coreConfig(base)
	all := config.Effects()
	connection := func(c config.Connection) config.Connection {
		c.Service, c.Credential, c.Effects = "gh", "gh-reader", all
		return c
	}
	cfg.Connections["repo"] = connection(config.Connection{Target: repoTarget})
	cfg.Connections["org"] = connection(config.Connection{Targets: []string{"orgs/octo-org"}})
	cfg.Connections["project"] = connection(config.Connection{Target: projectTarget})
	cfg.Connections["user"] = connection(config.Connection{Targets: []string{"users/octocat"}})
	cfg.Connections["open"] = connection(config.Connection{})
	cfg.Connections["other-user"] = connection(config.Connection{Targets: []string{"users/someone-else"}})
	cfg.Connections["reader"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Effects: []config.Effect{config.EffectRead}}
	cfg.Connections["repo-excluded"] = connection(config.Connection{Targets: []string{"repos/octo-org/*"},
		Exclude: []string{"repos/octo-org/example"}})
	cfg.Connections["org-excluded"] = connection(config.Connection{Exclude: []string{"orgs/octo-org"}})
	cfg.Connections["account-excluded"] = connection(config.Connection{Exclude: []string{"users/octocat"}})
	return cfg
}

func limitsCore(t *testing.T, base string, reads *int) *application.Core {
	t.Helper()
	red := &redact.Redactor{}
	return application.New(registry(t), limitsConfig(base), resolver(red, reads), red)
}

// Each level reads, sets, and removes its own route with one request per call, names the level it addressed,
// and reports what GitHub answered.
func TestInteractionLimitsWorkAtEveryLevel(t *testing.T) {
	f, _, base := serveLimits(t)
	core := limitsCore(t, base, nil)
	for _, level := range []struct{ name, connection, target, route, origin, named string }{
		{"repository", "repo", `"repository":"octo-org/example"`, limitsRepoRoute, "repository", `"repository":"octo-org/example"`},
		{"organization", "org", `"organization":"orgs/octo-org"`, limitsOrgRoute, "organization", `"organization":"orgs/octo-org"`},
		{"account", "open", ``, limitsAccountRoute, "user", ``},
	} {
		sep := ""
		if level.target != "" {
			sep = ","
		}
		result, err := invoke(t, core, interactionLimitsGet.ID, level.connection, `{`+level.target+`}`, false)
		if err != nil || strings.Contains(string(result), `"limit"`) || strings.Contains(string(result), `"origin"`) ||
			!strings.Contains(string(result), level.named) {
			t.Fatalf("%s read without a limit = %s, %v", level.name, result, err)
		}
		before := len(f.recorded())
		result, err = invoke(t, core, interactionLimitsSet.ID, level.connection,
			`{`+level.target+sep+`"limit":"collaborators_only","expiry":"one_week"}`, true)
		want := []string{`"limit":"collaborators_only"`, `"origin":"` + level.origin + `"`, `"expires_at":"2026-11-20T10:00:00Z"`}
		if err != nil || !containsAll(string(result), want) || !strings.Contains(string(result), level.named) {
			t.Fatalf("%s set = %s, %v", level.name, result, err)
		}
		sent := f.recorded()[before:]
		if len(sent) != 1 || sent[0].method != http.MethodPut || !strings.HasSuffix(sent[0].path, level.route) ||
			!reflect.DeepEqual(sent[0].body, map[string]any{"limit": "collaborators_only", "expiry": "one_week"}) {
			t.Errorf("%s set sent %+v, want one PUT of limit and expiry", level.name, sent)
		}
		result, err = invoke(t, core, interactionLimitsGet.ID, level.connection, `{`+level.target+`}`, false)
		if err != nil || !containsAll(string(result), want) {
			t.Fatalf("%s read = %s, %v", level.name, result, err)
		}
		before = len(f.recorded())
		result, err = invoke(t, core, interactionLimitsRemove.ID, level.connection, `{`+level.target+`}`, true)
		if err != nil || !strings.Contains(string(result), `"removed":true`) || !strings.Contains(string(result), level.named) {
			t.Fatalf("%s remove = %s, %v", level.name, result, err)
		}
		sent = f.recorded()[before:]
		if len(sent) != 1 || sent[0].method != http.MethodDelete || !strings.HasSuffix(sent[0].path, level.route) {
			t.Errorf("%s remove sent %+v, want one DELETE", level.name, sent)
		}
	}
	// The expiry stays out of the request when the call gives none.
	before := len(f.recorded())
	if _, err := invoke(t, core, interactionLimitsSet.ID, "open", `{"limit":"existing_users"}`, true); err != nil {
		t.Fatal(err)
	}
	if sent := f.recorded()[before:]; len(sent) != 1 || !reflect.DeepEqual(sent[0].body, map[string]any{"limit": "existing_users"}) {
		t.Errorf("set without expiry sent %+v", sent)
	}
}

// A level without a limit answers an empty object or no body, and both read as no limit.
func TestInteractionLimitReadWithoutALimitMayBeEmpty(t *testing.T) {
	_, l, base := serveLimits(t)
	l.emptyBody = true
	core := limitsCore(t, base, nil)
	result, err := invoke(t, core, interactionLimitsGet.ID, "open", `{}`, false)
	if err != nil || strings.TrimSpace(string(result)) != `{}` {
		t.Errorf("an empty read = %s, %v, want an empty result", result, err)
	}
}

// The target and the values are checked before a credential is resolved or GitHub is asked.
func TestInteractionLimitsAreCheckedBeforeIO(t *testing.T) {
	f, _, base := serveLimits(t)
	reads := 0
	core := limitsCore(t, base, &reads)
	repo, org := `"repository":"octo-org/example"`, `"organization":"orgs/octo-org"`
	for _, tt := range []struct{ name, tool, connection, arguments string }{
		{"both levels", interactionLimitsGet.ID, "open", `{` + repo + `,` + org + `}`},
		{"both levels on a change", interactionLimitsSet.ID, "open", `{` + repo + `,` + org + `,"limit":"existing_users"}`},
		{"a user as organization", interactionLimitsGet.ID, "open", `{"organization":"users/octocat"}`},
		{"a repository pattern", interactionLimitsGet.ID, "open", `{"repository":"octo-org/*"}`},
		{"an unknown limit", interactionLimitsSet.ID, "open", `{` + repo + `,"limit":"nobody"}`},
		{"no limit", interactionLimitsSet.ID, "open", `{` + repo + `}`},
		{"a limit in other case", interactionLimitsSet.ID, "open", `{` + repo + `,"limit":"Existing_Users"}`},
		{"an unknown expiry", interactionLimitsSet.ID, "open", `{` + repo + `,"limit":"existing_users","expiry":"forever"}`},
		{"an expiry in days", interactionLimitsSet.ID, "open", `{` + org + `,"limit":"existing_users","expiry":"two_days"}`},
		{"a repository outside the targets", interactionLimitsGet.ID, "repo", `{"repository":"octo-org/other"}`},
		{"an organization outside the targets", interactionLimitsRemove.ID, "org", `{"organization":"orgs/other-org"}`},
		{"an excluded repository", interactionLimitsSet.ID, "repo-excluded", `{` + repo + `,"limit":"existing_users"}`},
		{"an excluded organization", interactionLimitsRemove.ID, "org-excluded", `{` + org + `}`},
		{"the account of a repository connection", interactionLimitsGet.ID, "repo", `{}`},
		{"the account of a project connection", interactionLimitsSet.ID, "project", `{"limit":"existing_users"}`},
		{"the account of an organization-only connection", interactionLimitsGet.ID, "org", `{}`},
		{"the account change of an organization-only connection", interactionLimitsSet.ID, "org", `{"limit":"existing_users"}`},
		{"the account removal of a repository connection", interactionLimitsRemove.ID, "repo-excluded", `{}`},
	} {
		reads = 0
		before := len(f.recorded())
		if _, err := invoke(t, core, tt.tool, tt.connection, tt.arguments, true); !isInvalidRequest(err) {
			t.Errorf("%s = %v, want an invalid request", tt.name, err)
		}
		if reads != 0 || len(f.recorded()) != before {
			t.Errorf("%s reached the credential or GitHub", tt.name)
		}
	}
	// A connection naming the token's own user may address the account.
	if _, err := invoke(t, core, interactionLimitsGet.ID, "user", `{}`, false); err != nil {
		t.Errorf("account on a connection naming the token's user = %v, want it allowed", err)
	}
}

// A connection with targets addresses the account only when a user target names the token's account; its login
// is known only from GitHub, so the account is read once after the secret and no change is sent before it. A
// connection without targets reads nothing first.
func TestInteractionLimitsCheckTheTokenAccountAgainstTheTargets(t *testing.T) {
	f, _, base := serveLimits(t)
	core := limitsCore(t, base, nil)
	arguments := func(id string) string {
		if id == interactionLimitsSet.ID {
			return `{"limit":"existing_users"}`
		}
		return `{}`
	}
	for _, id := range limitTools {
		for _, connection := range []string{"account-excluded", "other-user"} {
			before := len(f.recorded())
			if _, err := invoke(t, core, id, connection, arguments(id), true); !isInvalidRequest(err) {
				t.Errorf("%s on %s = %v, want an invalid request", id, connection, err)
			}
			sent := f.recorded()[before:]
			if len(sent) != 1 || sent[0].method != http.MethodGet || sent[0].path != "/api/v3/user" {
				t.Errorf("%s on %s sent %+v, want only the read of the account", id, connection, sent)
			}
		}
		before := len(f.recorded())
		if _, err := invoke(t, core, id, "open", arguments(id), true); err != nil {
			t.Errorf("%s on a connection without targets = %v", id, err)
		}
		for _, request := range f.recorded()[before:] {
			if request.path == "/api/v3/user" {
				t.Errorf("%s on a connection without targets read the account", id)
			}
		}
		before = len(f.recorded())
		if _, err := invoke(t, core, id, "user", arguments(id), true); err != nil {
			t.Errorf("%s on a connection naming the token's user = %v", id, err)
		}
		if sent := f.recorded()[before:]; len(sent) != 2 || sent[0].path != "/api/v3/user" {
			t.Errorf("%s on a connection naming the token's user sent %+v, want the account read first", id, sent)
		}
	}
	// Another exclusion lets the account through.
	if _, err := invoke(t, core, interactionLimitsGet.ID, "org-excluded", `{}`, false); err != nil {
		t.Errorf("account with another exclusion = %v", err)
	}
}

// The tools are offered by their effect, not by a tools list; a change needs its own confirmation and sends
// nothing without it.
func TestInteractionLimitsAreOfferedByEffectAndConfirmed(t *testing.T) {
	f, _, base := serveLimits(t)
	core := limitsCore(t, base, nil)
	var unsupported *capability.UnsupportedError
	unconfirmedArguments := map[string]string{interactionLimitsSet.ID: `{"repository":"octo-org/example","limit":"existing_users"}`,
		interactionLimitsRemove.ID: `{"repository":"octo-org/example"}`}
	for _, id := range limitTools[1:] {
		if _, err := core.Describe(application.DescribeRequest{Operation: id, Connection: "reader"}); !errors.As(err, &unsupported) {
			t.Errorf("describe %s on a read-only connection = %v, want unsupported", id, err)
		}
		if _, err := core.Describe(application.DescribeRequest{Operation: id, Connection: "open"}); err != nil {
			t.Errorf("describe %s on a connection with every effect = %v", id, err)
		}
		before := len(f.recorded())
		var unconfirmed *application.ConfirmationRequiredError
		if _, err := invoke(t, core, id, "open", unconfirmedArguments[id], false); !errors.As(err, &unconfirmed) {
			t.Errorf("%s without confirmation = %v, want confirmation-required", id, err)
		}
		if len(f.recorded()) != before {
			t.Errorf("%s reached GitHub before confirmation", id)
		}
	}
	if _, err := core.Describe(application.DescribeRequest{Operation: interactionLimitsGet.ID, Connection: "reader"}); err != nil {
		t.Errorf("describe the read on a read-only connection = %v", err)
	}
}

// GitHub's refusals become Qatlas' own messages without GitHub's text and name the right each level needs; a
// change is sent once and an unclear outcome is reported as one.
func TestInteractionLimitFailuresNameTheirMeaningWithoutProviderText(t *testing.T) {
	f, l, base := serveLimits(t)
	core := limitsCore(t, base, nil)
	for _, level := range []struct{ name, connection, target, right string }{
		{"repository", "open", `"repository":"octo-org/example",`, "repo on a classic token"},
		{"organization", "open", `"organization":"orgs/octo-org",`, "admin:org"},
		{"account", "open", ``, "repo or user on a classic token"},
	} {
		for _, tt := range []struct {
			method    string
			status    int
			class     provider.Class
			fragment  string
			uncertain bool
		}{
			{http.MethodGet, http.StatusForbidden, provider.ClassPermission, level.right, false},
			{http.MethodGet, http.StatusNotFound, provider.ClassNotFound, "does not hold", false},
			{http.MethodPut, http.StatusForbidden, provider.ClassPermission, level.right, false},
			{http.MethodPut, http.StatusNotFound, provider.ClassNotFound, "does not hold", false},
			{http.MethodPut, http.StatusConflict, provider.ClassProviderError, "wider level", false},
			{http.MethodPut, http.StatusUnprocessableEntity, provider.ClassProviderError, "nothing was changed", false},
			{http.MethodPut, http.StatusGatewayTimeout, provider.ClassTimeout, "did not answer in time", true},
			{http.MethodPut, http.StatusBadGateway, provider.ClassProviderError, "HTTP 502", true},
			{http.MethodDelete, http.StatusForbidden, provider.ClassPermission, level.right, false},
			{http.MethodDelete, http.StatusConflict, provider.ClassProviderError, "wider level", false},
			{http.MethodDelete, http.StatusGatewayTimeout, provider.ClassTimeout, "did not answer in time", true},
		} {
			l.status = map[string]int{tt.method: tt.status}
			before := len(f.recorded())
			var err error
			switch tt.method {
			case http.MethodGet:
				_, err = invoke(t, core, interactionLimitsGet.ID, level.connection, `{`+strings.TrimSuffix(level.target, ",")+`}`, false)
			case http.MethodPut:
				_, err = invoke(t, core, interactionLimitsSet.ID, level.connection, `{`+level.target+`"limit":"existing_users"}`, true)
			default:
				_, err = invoke(t, core, interactionLimitsRemove.ID, level.connection, `{`+strings.TrimSuffix(level.target, ",")+`}`, true)
			}
			if classOf(err) != tt.class || err == nil || !strings.Contains(err.Error(), tt.fragment) ||
				strings.Contains(err.Error(), uncertain) != tt.uncertain {
				t.Errorf("%s %s answered %d = %v, want %s naming %q, uncertain=%t", level.name, tt.method, tt.status, err,
					tt.class, tt.fragment, tt.uncertain)
			}
			if err != nil && strings.Contains(err.Error(), limitsProviderCanary) {
				t.Errorf("%s %s answered %d quotes GitHub's text: %v", level.name, tt.method, tt.status, err)
			}
			sent := 0
			for _, request := range f.recorded()[before:] {
				if request.method == tt.method {
					sent++
				}
			}
			if sent != 1 {
				t.Errorf("%s %s answered %d sent %d requests, want exactly one", level.name, tt.method, tt.status, sent)
			}
		}
	}
}
