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

// fakeIssueLock answers the issue reads below the bound repository and the lock and pin changes through the
// failure hook of fakeGitHub. Issue 42 is the base fake's issue, 55 another issue of the repository, 7 a pull
// request, and every other number is unknown.
type fakeIssueLock struct {
	mu sync.Mutex
	f  *fakeGitHub
	// lock answers the lock route; nil answers 204. mutation answers the GraphQL document; nil answers the
	// state the mutation asks for.
	lock     func(w http.ResponseWriter, req *http.Request)
	mutation func(w http.ResponseWriter, pin bool)
}

func serveIssueLock(t *testing.T) (*fakeIssueLock, string) {
	t.Helper()
	l := &fakeIssueLock{f: &fakeGitHub{}}
	l.f.failure = l.route
	return l, serve(t, l.f)
}

func (l *fakeIssueLock) route(w http.ResponseWriter, req *http.Request) bool {
	w.Header().Set("Content-Type", "application/json")
	if req.URL.Path == "/api/graphql" {
		requests := l.f.recorded()
		document := requests[len(requests)-1].document
		if !strings.HasPrefix(document, "mutation") {
			return false
		}
		pin := strings.Contains(document, "result:pinIssue")
		if l.mutation != nil {
			l.mutation(w, pin)
			return true
		}
		id, _ := requests[len(requests)-1].variables["id"].(string)
		number := map[string]int{"I_42": 42, "I_55": 55}[id]
		fmt.Fprintf(w, `{"data":{"result":{"issue":{"number":%d,"isPinned":%t}}}}`, number, pin)
		return true
	}
	rest, ok := strings.CutPrefix(req.URL.Path, actionsPrefix+"issues/")
	if !ok {
		return false
	}
	switch {
	case req.Method == http.MethodGet && rest == "55":
		fmt.Fprint(w, `{"id":55000,"node_id":"I_55","number":55,"title":"canary","state":"open","body":"canary",`+
			`"user":{"login":"octocat"},"assignees":[],"labels":[],"html_url":"https://github.com/octo-org/example/issues/55"}`)
	case req.Method == http.MethodGet && (rest == "42" || rest == "7"):
		return false
	case req.Method == http.MethodGet:
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"message":"Not Found"}`)
	case strings.HasSuffix(rest, "/lock") && (req.Method == http.MethodPut || req.Method == http.MethodDelete):
		if l.lock != nil {
			l.lock(w, req)
			return true
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		return false
	}
	return true
}

func issueLockConfig(base string) *config.Config {
	cfg := coreConfig(base)
	all := []config.Effect{config.EffectRead, config.EffectCreate, config.EffectUpdate, config.EffectDelete}
	cfg.Connections["repo"] = config.Connection{Service: "gh", Credential: "gh-reader", Target: repoTarget, Effects: all}
	cfg.Connections["repo-listed"] = config.Connection{Service: "gh", Credential: "gh-reader", Target: repoTarget,
		Effects: all, Tools: []string{issuesLock.ID, issuesUnlock.ID, issuesPin.ID, issuesUnpin.ID}}
	cfg.Connections["ex"] = config.Connection{Service: "gh", Credential: "gh-reader", Effects: all,
		Tools: []string{issuesLock.ID}, Exclude: []string{"repos/octo-org/example"}}
	return cfg
}

// changes counts the requests that are neither a read nor the issue read.
func (l *fakeIssueLock) changes() []string {
	var out []string
	for _, r := range l.f.recorded() {
		if r.method != http.MethodGet {
			out = append(out, r.method+" "+strings.TrimPrefix(r.path, actionsPrefix))
		}
	}
	return out
}

// Every tool reads its issue below the bound repository first, sends exactly one change, answers with number
// and state only, and pins with the node of its own read.
func TestIssueLockAndPinSendOneChangeEach(t *testing.T) {
	l, base := serveIssueLock(t)
	red := &redact.Redactor{}
	core := application.New(registry(t), issueLockConfig(base), resolver(red, nil), red)
	for _, tt := range []struct {
		id, args, want, request string
	}{
		{issuesLock.ID, `{"number":42,"lock_reason":"too heated"}`, `"locked":true`, "PUT issues/42/lock"},
		{issuesUnlock.ID, `{"number":42}`, `"locked":false`, "DELETE issues/42/lock"},
		{issuesPin.ID, `{"number":55}`, `"pinned":true`, "POST /api/graphql"},
		{issuesUnpin.ID, `{"number":55}`, `"pinned":false`, "POST /api/graphql"},
	} {
		before := len(l.f.recorded())
		got, err := invoke(t, core, tt.id, "repo-listed", tt.args, true)
		if err != nil || !strings.Contains(string(got), tt.want) || !strings.Contains(string(got), `"number":`) ||
			strings.Contains(string(got), "canary") {
			t.Fatalf("%s = %s, %v", tt.id, got, err)
		}
		sent := l.f.recorded()[before:]
		if sent[0].method != http.MethodGet || !strings.HasSuffix(sent[0].path, "/issues/"+tt.args[10:12]) {
			t.Errorf("%s did not read its issue first: %+v", tt.id, sent[0])
		}
		changes := 0
		for _, r := range sent[1:] {
			changes++
			if r.method+" "+strings.TrimPrefix(r.path, actionsPrefix) != tt.request &&
				r.method+" "+r.path != tt.request {
				t.Errorf("%s sent %s %s, want %s", tt.id, r.method, r.path, tt.request)
			}
		}
		if len(sent) != 2 || changes != 1 {
			t.Errorf("%s sent %d requests, want a read and one change", tt.id, len(sent))
		}
	}
	// The node identifier of the pin is the one of the read of the chosen issue.
	for _, r := range l.f.recorded() {
		if strings.HasPrefix(r.document, "mutation") && r.variables["id"] != "I_55" {
			t.Errorf("mutation variables = %v, want the node of issue 55", r.variables)
		}
	}
	// A lock without a reason sends an empty object, and the reason it was given is reported.
	if got, err := invoke(t, core, issuesLock.ID, "repo-listed", `{"number":42}`, true); err != nil ||
		strings.Contains(string(got), "lock_reason") {
		t.Errorf("lock without reason = %s, %v", got, err)
	}
	last := l.f.recorded()[len(l.f.recorded())-1]
	if last.method != http.MethodPut || len(last.body) != 0 {
		t.Errorf("lock without reason sent %+v, want an empty object", last)
	}
	got, _ := invoke(t, core, issuesLock.ID, "repo-listed", `{"number":42,"lock_reason":"spam"}`, true)
	last = l.f.recorded()[len(l.f.recorded())-1]
	if last.body["lock_reason"] != "spam" || !strings.Contains(string(got), `"lock_reason":"spam"`) {
		t.Errorf("lock with reason sent %+v and answered %s", last.body, got)
	}
}

// Locking and pinning ask for confirmation, and github.issues.lock is offered only where a connection lists it.
func TestIssueLockIsListedOnlyAndEveryChangeNeedsConfirmation(t *testing.T) {
	l, base := serveIssueLock(t)
	red := &redact.Redactor{}
	core := application.New(registry(t), issueLockConfig(base), resolver(red, nil), red)
	var unsupported *capability.UnsupportedError
	if _, err := invoke(t, core, issuesLock.ID, "repo", `{"number":42}`, true); !errors.As(err, &unsupported) {
		t.Errorf("lock without a tools list = %v, want unsupported-capability", err)
	}
	for _, id := range []string{issuesUnlock.ID, issuesPin.ID, issuesUnpin.ID} {
		if _, err := invoke(t, core, id, "repo", `{"number":42}`, true); err != nil {
			t.Errorf("%s without a tools list = %v, want it offered", id, err)
		}
	}
	before := len(l.f.recorded())
	unconfirmed := &application.ConfirmationRequiredError{}
	for _, id := range []string{issuesLock.ID, issuesUnlock.ID, issuesPin.ID, issuesUnpin.ID} {
		if _, err := invoke(t, core, id, "repo-listed", `{"number":42}`, false); !errors.As(err, &unconfirmed) {
			t.Errorf("%s without confirmation = %v", id, err)
		}
	}
	if len(l.f.recorded()) != before {
		t.Error("a change without confirmation reached GitHub")
	}
	for _, tool := range []capability.Descriptor{issuesLock, issuesUnlock, issuesPin, issuesUnpin} {
		if tool.Risk.Confirmation != capability.ConfirmationRequired || tool.RequiresToolAllowList != (tool.ID == issuesLock.ID) {
			t.Errorf("%s risk = %+v, listed-only = %t", tool.ID, tool.Risk, tool.RequiresToolAllowList)
		}
	}
}

// Arguments, the repository, and the exclusion are checked before a credential is resolved or a request sent.
func TestIssueLockIsCheckedBeforeSecretAccess(t *testing.T) {
	l, base := serveIssueLock(t)
	reads := 0
	red := &redact.Redactor{}
	core := application.New(registry(t), issueLockConfig(base), resolver(red, &reads), red)
	for _, tt := range []struct{ name, id, connection, args string }{
		{"unknown reason", issuesLock.ID, "repo-listed", `{"number":42,"lock_reason":"boring"}`},
		{"capitalized reason", issuesLock.ID, "repo-listed", `{"number":42,"lock_reason":"Spam"}`},
		{"reason on unlock", issuesUnlock.ID, "repo-listed", `{"number":42,"lock_reason":"spam"}`},
		{"reason on pin", issuesPin.ID, "repo-listed", `{"number":42,"lock_reason":"spam"}`},
		{"zero number", issuesLock.ID, "repo-listed", `{"number":0}`},
		{"zero number pin", issuesPin.ID, "repo-listed", `{"number":0}`},
		{"node id from the caller", issuesPin.ID, "repo-listed", `{"number":42,"issue_id":"I_other"}`},
		{"foreign repository lock", issuesLock.ID, "repo-listed", `{"number":42,"repository":"octo-org/other"}`},
		{"foreign repository unlock", issuesUnlock.ID, "repo-listed", `{"number":42,"repository":"octo-org/other"}`},
		{"foreign repository pin", issuesPin.ID, "repo-listed", `{"number":42,"repository":"octo-org/other"}`},
		{"foreign repository unpin", issuesUnpin.ID, "repo-listed", `{"number":42,"repository":"octo-org/other"}`},
		{"excluded repository", issuesLock.ID, "ex", `{"number":42,"repository":"octo-org/example"}`},
	} {
		if _, err := invoke(t, core, tt.id, tt.connection, tt.args, true); !isInvalidRequest(err) {
			t.Errorf("%s = %v, want an invalid request", tt.name, err)
		}
	}
	if reads != 0 || len(l.f.recorded()) != 0 {
		t.Errorf("refused requests read %d credentials and sent %d requests", reads, len(l.f.recorded()))
	}
	for _, reason := range lockReasons {
		if _, err := invoke(t, core, issuesLock.ID, "repo-listed", `{"number":42,"lock_reason":"`+reason+`"}`, true); err != nil {
			t.Errorf("lock reason %q = %v", reason, err)
		}
	}
}

// Pin and unpin refuse a pull request, and no tool changes a number GitHub does not hold in the repository.
func TestIssueLockRefusesWhatTheReadDoesNotBindToTheRepository(t *testing.T) {
	l, base := serveIssueLock(t)
	red := &redact.Redactor{}
	core := application.New(registry(t), issueLockConfig(base), resolver(red, nil), red)
	for _, id := range []string{issuesPin.ID, issuesUnpin.ID} {
		if _, err := invoke(t, core, id, "repo-listed", `{"number":7}`, true); !isInvalidRequest(err) ||
			!strings.Contains(err.Error(), "is a pull request") {
			t.Errorf("%s on a pull request = %v", id, err)
		}
	}
	for _, id := range []string{issuesLock.ID, issuesUnlock.ID, issuesPin.ID, issuesUnpin.ID} {
		if _, err := invoke(t, core, id, "repo-listed", `{"number":99}`, true); classOf(err) != provider.ClassNotFound {
			t.Errorf("%s on an unknown number = %v, want not-found", id, err)
		}
	}
	if changes := l.changes(); len(changes) != 0 {
		t.Errorf("refused issues were changed: %v", changes)
	}
}

// Lock and unlock take a pull request number of the repository and send exactly one change for it.
func TestIssueLockAcceptsAPullRequestNumber(t *testing.T) {
	l, base := serveIssueLock(t)
	red := &redact.Redactor{}
	core := application.New(registry(t), issueLockConfig(base), resolver(red, nil), red)
	for _, tt := range []struct{ id, request, want string }{
		{issuesLock.ID, "PUT issues/7/lock", `"locked":true`},
		{issuesUnlock.ID, "DELETE issues/7/lock", `"locked":false`},
	} {
		before := len(l.changes())
		got, err := invoke(t, core, tt.id, "repo-listed", `{"number":7}`, true)
		if err != nil || !strings.Contains(string(got), tt.want) || !strings.Contains(string(got), `"is_pull_request":true`) {
			t.Fatalf("%s on a pull request = %s, %v", tt.id, got, err)
		}
		if changes := l.changes()[before:]; len(changes) != 1 || changes[0] != tt.request {
			t.Errorf("%s sent %v, want %s", tt.id, changes, tt.request)
		}
	}
	if got, _ := invoke(t, core, issuesUnlock.ID, "repo-listed", `{"number":42}`, true); strings.Contains(string(got), "is_pull_request") {
		t.Errorf("an issue answer = %s, want no is_pull_request", got)
	}
}

// Refusals of the change name no provider text; an unclear outcome is sent once and reported as uncertain.
func TestIssueLockChangeFailures(t *testing.T) {
	status := func(code int) func(http.ResponseWriter, *http.Request) {
		return func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(code)
			fmt.Fprint(w, `{"message":"canary"}`)
		}
	}
	for name, tt := range map[string]struct {
		answer    func(http.ResponseWriter, *http.Request)
		class     provider.Class
		uncertain bool
	}{
		"forbidden":       {status(http.StatusForbidden), provider.ClassPermission, false},
		"not found":       {status(http.StatusNotFound), provider.ClassNotFound, false},
		"gone":            {status(http.StatusGone), provider.ClassProviderError, false},
		"unprocessable":   {status(http.StatusUnprocessableEntity), provider.ClassProviderError, false},
		"gateway timeout": {status(http.StatusGatewayTimeout), provider.ClassTimeout, true},
		"server error":    {status(http.StatusBadGateway), provider.ClassProviderError, true},
		"dropped connection": {func(w http.ResponseWriter, _ *http.Request) {
			if connection, _, err := w.(http.Hijacker).Hijack(); err == nil {
				connection.Close()
			}
		}, "", true},
	} {
		t.Run(name, func(t *testing.T) {
			l, base := serveIssueLock(t)
			l.lock = tt.answer
			red := &redact.Redactor{}
			core := application.New(registry(t), issueLockConfig(base), resolver(red, nil), red)
			for _, call := range []struct{ id, args string }{
				{issuesLock.ID, `{"number":42,"lock_reason":"spam"}`},
				{issuesUnlock.ID, `{"number":42}`},
			} {
				before := len(l.changes())
				_, err := invoke(t, core, call.id, "repo-listed", call.args, true)
				if err == nil || strings.Contains(err.Error(), "canary") ||
					strings.Contains(err.Error(), uncertain) != tt.uncertain ||
					(tt.class != "" && classOf(err) != tt.class) {
					t.Errorf("%s = %v, want class %q, uncertain %v", call.id, err, tt.class, tt.uncertain)
				}
				if n := len(l.changes()) - before; n != 1 {
					t.Errorf("%s sent %d changes, want exactly one", call.id, n)
				}
			}
		})
	}
}

func TestIssuePinChangeFailures(t *testing.T) {
	graphQL := func(body string) func(http.ResponseWriter, bool) {
		return func(w http.ResponseWriter, _ bool) { fmt.Fprint(w, body) }
	}
	for name, tt := range map[string]struct {
		answer    func(http.ResponseWriter, bool)
		class     provider.Class
		uncertain bool
		contains  string
	}{
		"forbidden": {graphQL(`{"errors":[{"type":"FORBIDDEN","message":"canary","path":["result"]}],"data":null}`),
			provider.ClassPermission, false, "pinning needs write access"},
		"not found": {graphQL(`{"errors":[{"type":"NOT_FOUND","message":"canary","path":["result"]}],"data":null}`),
			provider.ClassNotFound, false, ""},
		"limit reached": {graphQL(`{"errors":[{"type":"UNPROCESSABLE","message":"canary","path":["result"]}],"data":null}`),
			provider.ClassProviderError, false, "three pinned issues"},
		"http forbidden": {func(w http.ResponseWriter, _ bool) {
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"message":"canary"}`)
		}, provider.ClassPermission, false, "pinning needs write access"},
		"gateway timeout": {func(w http.ResponseWriter, _ bool) { w.WriteHeader(http.StatusGatewayTimeout) },
			provider.ClassTimeout, true, ""},
		"server error": {func(w http.ResponseWriter, _ bool) { w.WriteHeader(http.StatusBadGateway) },
			provider.ClassProviderError, true, ""},
		"unreadable answer":      {graphQL(`{"data":`), provider.ClassInvalidResponse, true, ""},
		"answer without a state": {graphQL(`{"data":{"result":null}}`), provider.ClassInvalidResponse, true, ""},
		"answer of another issue": {graphQL(`{"data":{"result":{"issue":{"number":99,"isPinned":true}}}}`),
			provider.ClassInvalidResponse, true, ""},
		"dropped connection": {func(w http.ResponseWriter, _ bool) {
			if connection, _, err := w.(http.Hijacker).Hijack(); err == nil {
				connection.Close()
			}
		}, "", true, ""},
	} {
		t.Run(name, func(t *testing.T) {
			l, base := serveIssueLock(t)
			l.mutation = tt.answer
			red := &redact.Redactor{}
			core := application.New(registry(t), issueLockConfig(base), resolver(red, nil), red)
			for _, id := range []string{issuesPin.ID, issuesUnpin.ID} {
				before := len(l.changes())
				_, err := invoke(t, core, id, "repo-listed", `{"number":42}`, true)
				if err == nil || strings.Contains(err.Error(), "canary") ||
					strings.Contains(err.Error(), uncertain) != tt.uncertain ||
					(tt.class != "" && classOf(err) != tt.class) {
					t.Errorf("%s = %v, want class %q, uncertain %v", id, err, tt.class, tt.uncertain)
				}
				// Only the pin names the limit; the permission message names the pin in both directions.
				if tt.contains != "" && id == issuesPin.ID && !strings.Contains(err.Error(), tt.contains) {
					t.Errorf("%s = %v, want a message naming %q", id, err, tt.contains)
				}
				if n := len(l.changes()) - before; n != 1 {
					t.Errorf("%s sent %d changes, want exactly one", id, n)
				}
			}
		})
	}
}
