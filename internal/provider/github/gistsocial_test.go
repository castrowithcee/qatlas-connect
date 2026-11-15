package github

import (
	"encoding/json"
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

// socialGists answers the social gist routes on top of fakeGists. Gist aa11 belongs to octocat, bb22 to hubot;
// the token belongs to octocat. Gist aa11 starts unstarred and has comment 7; the star state follows PUT and
// DELETE. override forces a status for one "METHOD path" of the gist routes.
type socialGists struct {
	base     fakeGists
	starred  map[string]bool
	override map[string]int
}

const socialPrefix = notificationPrefix + "/gists/"

func commentBody(id int64, gist string, body string) string {
	return fmt.Sprintf(`{"id":%d,"node_id":"GC_%d","url":"https://api.github.com/gists/%s/comments/%d",`+
		`"body":%q,"user":{"login":"hubot","id":2,"type":"User","site_admin":false},`+
		`"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-02T00:00:00Z","author_association":"NONE"}`,
		id, id, gist, id, body)
}

func (s *socialGists) route(w http.ResponseWriter, r *http.Request) bool {
	path, ok := strings.CutPrefix(r.URL.Path, socialPrefix)
	if !ok {
		return s.base.route(w, r)
	}
	id, rest, _ := strings.Cut(path, "/")
	if rest == "" {
		return s.base.route(w, r)
	}
	w.Header().Set("Content-Type", "application/json")
	if status, forced := s.override[r.Method+" "+r.URL.Path]; forced {
		w.WriteHeader(status)
		return true
	}
	known := id == "aa11" || id == "bb22"
	switch {
	case !known:
		w.WriteHeader(http.StatusNotFound)
	case rest == "comments" && r.Method == http.MethodGet:
		w.Header().Set("Link", `<https://api.github.com/gists/`+id+`/comments?page=2>; rel="next"`)
		fmt.Fprintf(w, "[%s]", commentBody(7, id, "hi "+strings.Repeat("c", 5000)))
	case rest == "comments" && r.Method == http.MethodPost:
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, commentBody(8, id, "created"))
	case rest == "comments/7" && r.Method == http.MethodGet:
		fmt.Fprint(w, commentBody(7, id, "seven"))
	case rest == "comments/9" && r.Method == http.MethodGet:
		fmt.Fprint(w, commentBody(9, "ffff", "stray"))
	case rest == "comments/7" && r.Method == http.MethodPatch:
		fmt.Fprint(w, commentBody(7, id, "edited"))
	case rest == "comments/7" && r.Method == http.MethodDelete:
		w.WriteHeader(http.StatusNoContent)
	case rest == "commits" && r.Method == http.MethodGet:
		fmt.Fprint(w, `[{"url":"https://api.github.com/gists/`+id+`/abc","version":"abc","user":{"login":"octocat"},`+
			`"change_status":{"total":3,"additions":2,"deletions":1},"committed_at":"2026-01-02T00:00:00Z"}]`)
	case rest == "forks" && r.Method == http.MethodGet:
		fmt.Fprintf(w, "[%s,%s]", gistBody("ff01", "octocat"), gistBody("ff02", "hubot"))
	case rest == "forks" && r.Method == http.MethodPost:
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, gistBody("ff03", "octocat"))
	case rest == "star" && r.Method == http.MethodGet:
		if s.starred[id] {
			w.WriteHeader(http.StatusNoContent)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	case rest == "star" && r.Method == http.MethodPut:
		s.starred[id] = true
		w.WriteHeader(http.StatusNoContent)
	case rest == "star" && r.Method == http.MethodDelete:
		delete(s.starred, id)
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
	return true
}

var socialTools = []string{gistCommentsList.ID, gistCommentsGet.ID, gistCommentsCreate.ID, gistCommentsUpdate.ID,
	gistCommentsDelete.ID, gistCommitsList.ID, gistForksList.ID, gistForksCreate.ID, gistStarsSet.ID}

func gistSocialRig(t *testing.T) (*fakeGitHub, *socialGists, *application.Core, *int) {
	t.Helper()
	f := &fakeGitHub{}
	s := &socialGists{base: fakeGists{f: f}, starred: map[string]bool{}, override: map[string]int{}}
	f.failure = s.route
	cfg := coreConfig(serve(t, f))
	all := config.Effects()
	add := func(name string, targets []string, tools ...string) {
		cfg.Connections[name] = connectionOf(config.Connection{Service: "gh", Credential: "gh-reader", Targets: targets},
			all, tools)
	}
	add("open", nil)
	add("open-listed", nil, socialTools...)
	add("me", []string{"users/octocat"}, socialTools...)
	add("other", []string{"users/hubot"}, socialTools...)
	add("org", []string{"orgs/octo-org"}, socialTools...)
	add("repo", []string{repoTarget}, socialTools...)
	add("mixed", []string{"users/octocat", repoTarget}, socialTools...)
	red := &redact.Redactor{}
	reads := 0
	return f, s, application.New(registry(t), cfg, resolver(red, &reads), red), &reads
}

func TestSocialGistToolsSucceedAndSendOneRequestEach(t *testing.T) {
	f, _, core, _ := gistSocialRig(t)

	list, err := invoke(t, core, gistCommentsList.ID, "open", `{"gist_id":"aa11","limit":5}`, false)
	if err != nil || !strings.Contains(string(list), `"id":7`) || !strings.Contains(string(list), `"has_more":true`) ||
		!strings.Contains(string(list), `"body_truncated":true`) || strings.Contains(string(list), strings.Repeat("c", 4001)) ||
		!strings.Contains(string(list), `"author_association":"NONE"`) || !strings.Contains(string(list), `"login":"hubot"`) {
		t.Fatalf("comments list = %s, %v", list, err)
	}
	if last := f.recorded()[len(f.recorded())-1]; last.path != socialPrefix+"aa11/comments" ||
		!strings.Contains(last.query, "per_page=5") {
		t.Errorf("list request = %+v", last)
	}
	var page struct {
		NextCursor string `json:"next_cursor"`
	}
	_ = json.Unmarshal(list, &page)
	if _, err := invoke(t, core, gistCommentsList.ID, "open", `{"gist_id":"aa11","cursor":"`+page.NextCursor+`"}`, false); err != nil {
		t.Errorf("next batch = %v", err)
	}
	if _, err := invoke(t, core, gistCommentsList.ID, "open", `{"gist_id":"bb22","cursor":"`+page.NextCursor+`"}`, false); !isInvalidRequest(err) {
		t.Errorf("cursor of another gist = %v, want an invalid request", err)
	}

	got, err := invoke(t, core, gistCommentsGet.ID, "open", `{"gist_id":"aa11","comment_id":7}`, false)
	if err != nil || !strings.Contains(string(got), `"body":"seven"`) || !strings.Contains(string(got), `"links":{"url"`) {
		t.Errorf("comment get = %s, %v", got, err)
	}
	if _, err := invoke(t, core, gistCommentsGet.ID, "open", `{"gist_id":"aa11","comment_id":9}`, false); classOf(err) != provider.ClassNotFound {
		t.Errorf("comment of another gist = %v, want not-found", err)
	}

	before := len(f.recorded())
	created, err := invoke(t, core, gistCommentsCreate.ID, "open", `{"gist_id":"aa11","body":"hello"}`, true)
	requests := sent(f, before)
	if err != nil || !strings.Contains(string(created), `"id":8`) || len(requests) != 1 ||
		requests[0].method != http.MethodPost || requests[0].path != socialPrefix+"aa11/comments" ||
		requests[0].body["body"] != "hello" {
		t.Errorf("comment create = %s, %v, %+v", created, err, requests)
	}

	before = len(f.recorded())
	updated, err := invoke(t, core, gistCommentsUpdate.ID, "open", `{"gist_id":"aa11","comment_id":7,"body":"edit"}`, true)
	requests = sent(f, before)
	if err != nil || !strings.Contains(string(updated), `"body":"edited"`) || len(requests) != 1 ||
		requests[0].method != http.MethodPatch || requests[0].path != socialPrefix+"aa11/comments/7" {
		t.Errorf("comment update = %s, %v, %+v", updated, err, requests)
	}

	before = len(f.recorded())
	deleted, err := invoke(t, core, gistCommentsDelete.ID, "open-listed", `{"gist_id":"aa11","comment_id":7}`, true)
	requests = sent(f, before)
	if err != nil || !strings.Contains(string(deleted), `"deleted":true`) || len(requests) != 1 ||
		requests[0].method != http.MethodDelete {
		t.Errorf("comment delete = %s, %v, %+v", deleted, err, requests)
	}

	commits, err := invoke(t, core, gistCommitsList.ID, "open", `{"gist_id":"aa11"}`, false)
	if err != nil || !strings.Contains(string(commits), `"version":"abc"`) ||
		!strings.Contains(string(commits), `"additions":2`) || !strings.Contains(string(commits), `"has_more":false`) {
		t.Errorf("commits = %s, %v", commits, err)
	}

	forks, err := invoke(t, core, gistForksList.ID, "open", `{"gist_id":"aa11"}`, false)
	if err != nil || !strings.Contains(string(forks), `"id":"ff01"`) || !strings.Contains(string(forks), `"id":"ff02"`) ||
		strings.Contains(string(forks), `"content"`) {
		t.Errorf("forks = %s, %v", forks, err)
	}

	before = len(f.recorded())
	forked, err := invoke(t, core, gistForksCreate.ID, "open", `{"gist_id":"bb22"}`, true)
	requests = sent(f, before)
	if err != nil || !strings.Contains(string(forked), `"id":"ff03"`) || len(requests) != 1 ||
		requests[0].method != http.MethodPost || requests[0].path != socialPrefix+"bb22/forks" {
		t.Errorf("fork = %s, %v, %+v", forked, err, requests)
	}
}

func TestGistStarsAreSetAndRemovedIdempotently(t *testing.T) {
	f, s, core, _ := gistSocialRig(t)
	for _, step := range []struct {
		arguments string
		changed   bool
		method    string
		starred   bool
	}{
		{`{"gist_id":"aa11","starred":true}`, true, http.MethodPut, true},
		{`{"gist_id":"aa11","starred":true}`, false, "", true},
		{`{"gist_id":"aa11","starred":false}`, true, http.MethodDelete, false},
		{`{"gist_id":"aa11","starred":false}`, false, "", false},
	} {
		before := len(f.recorded())
		out, err := invoke(t, core, gistStarsSet.ID, "open", step.arguments, true)
		requests := sent(f, before)
		want := fmt.Sprintf(`"changed":%t`, step.changed)
		if err != nil || !strings.Contains(string(out), want) || !strings.Contains(string(out), fmt.Sprintf(`"starred":%t`, step.starred)) {
			t.Fatalf("%s = %s, %v", step.arguments, out, err)
		}
		changes := 0
		for _, request := range requests {
			if request.method != http.MethodGet {
				changes++
				if request.method != step.method {
					t.Errorf("%s sent %+v", step.arguments, request)
				}
			}
		}
		if wantChanges := map[bool]int{true: 1, false: 0}[step.changed]; changes != wantChanges {
			t.Errorf("%s sent %d changes, want %d", step.arguments, changes, wantChanges)
		}
	}
	if s.starred["aa11"] {
		t.Error("the star was not removed")
	}
	if _, err := invoke(t, core, gistStarsSet.ID, "open", `{"gist_id":"aa11"}`, true); !isInvalidRequest(err) {
		t.Errorf("starred missing = %v, want an invalid request", err)
	}
}

func TestSocialGistChangesNeedConfirmationAndCommentDeleteIsListedOnly(t *testing.T) {
	f, _, core, _ := gistSocialRig(t)
	unconfirmed := &application.ConfirmationRequiredError{}
	for _, call := range []struct{ tool, connection, arguments string }{
		{gistCommentsCreate.ID, "open", `{"gist_id":"aa11","body":"x"}`},
		{gistCommentsUpdate.ID, "open", `{"gist_id":"aa11","comment_id":7,"body":"x"}`},
		{gistCommentsDelete.ID, "open-listed", `{"gist_id":"aa11","comment_id":7}`},
		{gistForksCreate.ID, "open", `{"gist_id":"aa11"}`},
		{gistStarsSet.ID, "open", `{"gist_id":"aa11","starred":true}`},
	} {
		before := len(f.recorded())
		if _, err := invoke(t, core, call.tool, call.connection, call.arguments, false); !errors.As(err, &unconfirmed) {
			t.Errorf("%s without confirmation = %v, want confirmation-required", call.tool, err)
		}
		if len(f.recorded()) != before {
			t.Errorf("%s without confirmation reached GitHub", call.tool)
		}
	}
	var unsupported *capability.UnsupportedError
	if _, err := invoke(t, core, gistCommentsDelete.ID, "open", `{"gist_id":"aa11","comment_id":7}`, true); !errors.As(err, &unsupported) {
		t.Errorf("comment delete without a tools list = %v, want unsupported-capability", err)
	}
}

func TestSocialGistTargetsBindBeforeAnySecret(t *testing.T) {
	f, _, core, reads := gistSocialRig(t)
	calls := []struct{ tool, arguments string }{
		{gistCommentsList.ID, `{"gist_id":"aa11"}`}, {gistCommentsGet.ID, `{"gist_id":"aa11","comment_id":7}`},
		{gistCommentsCreate.ID, `{"gist_id":"aa11","body":"x"}`},
		{gistCommentsUpdate.ID, `{"gist_id":"aa11","comment_id":7,"body":"x"}`},
		{gistCommentsDelete.ID, `{"gist_id":"aa11","comment_id":7}`},
		{gistCommitsList.ID, `{"gist_id":"aa11"}`}, {gistForksList.ID, `{"gist_id":"aa11"}`},
		{gistForksCreate.ID, `{"gist_id":"aa11"}`}, {gistStarsSet.ID, `{"gist_id":"aa11","starred":true}`},
	}
	for _, connection := range []string{"org", "repo", "mixed"} {
		for _, call := range calls {
			before, count := *reads, len(f.recorded())
			_, err := invoke(t, core, call.tool, connection, call.arguments, true)
			if !isInvalidRequest(err) {
				t.Errorf("%s on %s = %v, want an invalid request", call.tool, connection, err)
			}
			if *reads != before || len(f.recorded()) != count {
				t.Errorf("%s on %s read a secret or reached GitHub", call.tool, connection)
			}
		}
	}
}

func TestForeignGistsAreNotFoundForEverySocialTool(t *testing.T) {
	f, _, core, _ := gistSocialRig(t)
	for _, call := range []struct{ tool, arguments string }{
		{gistCommentsList.ID, `{"gist_id":"bb22"}`}, {gistCommentsGet.ID, `{"gist_id":"bb22","comment_id":7}`},
		{gistCommentsCreate.ID, `{"gist_id":"bb22","body":"x"}`},
		{gistCommentsUpdate.ID, `{"gist_id":"bb22","comment_id":7,"body":"x"}`},
		{gistCommentsDelete.ID, `{"gist_id":"bb22","comment_id":7}`},
		{gistCommitsList.ID, `{"gist_id":"bb22"}`}, {gistForksList.ID, `{"gist_id":"bb22"}`},
		{gistForksCreate.ID, `{"gist_id":"bb22"}`}, {gistStarsSet.ID, `{"gist_id":"bb22","starred":true}`},
	} {
		before := len(f.recorded())
		_, err := invoke(t, core, call.tool, "me", call.arguments, true)
		if classOf(err) != provider.ClassNotFound {
			t.Errorf("%s of a foreign gist = %v, want not-found", call.tool, err)
		}
		for _, request := range sent(f, before) {
			if request.method != http.MethodGet {
				t.Errorf("%s changed a foreign gist: %+v", call.tool, request)
			}
		}
	}
	// The own gist and a fork for the own account pass; the fork list keeps only admitted owners.
	if _, err := invoke(t, core, gistCommentsCreate.ID, "me", `{"gist_id":"aa11","body":"x"}`, true); err != nil {
		t.Errorf("comment on an own gist = %v", err)
	}
	out, err := invoke(t, core, gistForksList.ID, "me", `{"gist_id":"aa11"}`, false)
	if err != nil || !strings.Contains(string(out), `"ff01"`) || strings.Contains(string(out), `"ff02"`) {
		t.Errorf("forks on a bound connection = %s, %v", out, err)
	}
	// A fork needs the account behind the token among the user targets.
	before := len(f.recorded())
	if _, err := invoke(t, core, gistForksCreate.ID, "other", `{"gist_id":"bb22"}`, true); !isInvalidRequest(err) {
		t.Errorf("fork for a foreign account = %v, want an invalid request", err)
	}
	for _, request := range sent(f, before) {
		if request.method != http.MethodGet {
			t.Errorf("a refused fork sent %+v", request)
		}
	}
}

func TestSocialGistFailuresAreClassifiedAndNeverRepeated(t *testing.T) {
	f, s, core, _ := gistSocialRig(t)
	for _, tt := range []struct {
		tool, arguments, route string
		status                 int
		class                  provider.Class
	}{
		{gistCommentsCreate.ID, `{"gist_id":"aa11","body":"x"}`, "POST " + socialPrefix + "aa11/comments", 403, provider.ClassPermission},
		{gistCommentsCreate.ID, `{"gist_id":"aa11","body":"x"}`, "POST " + socialPrefix + "aa11/comments", 404, provider.ClassNotFound},
		{gistCommentsCreate.ID, `{"gist_id":"aa11","body":"x"}`, "POST " + socialPrefix + "aa11/comments", 422, provider.ClassProviderError},
		{gistCommentsUpdate.ID, `{"gist_id":"aa11","comment_id":7,"body":"x"}`, "PATCH " + socialPrefix + "aa11/comments/7", 403, provider.ClassPermission},
		{gistForksCreate.ID, `{"gist_id":"aa11"}`, "POST " + socialPrefix + "aa11/forks", 422, provider.ClassProviderError},
		{gistForksCreate.ID, `{"gist_id":"aa11"}`, "POST " + socialPrefix + "aa11/forks", 403, provider.ClassPermission},
		{gistStarsSet.ID, `{"gist_id":"aa11","starred":true}`, "PUT " + socialPrefix + "aa11/star", 403, provider.ClassPermission},
		{gistStarsSet.ID, `{"gist_id":"aa11","starred":true}`, "PUT " + socialPrefix + "aa11/star", 404, provider.ClassNotFound},
		{gistCommentsList.ID, `{"gist_id":"aa11"}`, "GET " + socialPrefix + "aa11/comments", 403, provider.ClassPermission},
		{gistCommitsList.ID, `{"gist_id":"aa11"}`, "GET " + socialPrefix + "aa11/commits", 404, provider.ClassNotFound},
		{gistForksList.ID, `{"gist_id":"aa11"}`, "GET " + socialPrefix + "aa11/forks", 403, provider.ClassPermission},
	} {
		s.override = map[string]int{tt.route: tt.status}
		before := len(f.recorded())
		_, err := invoke(t, core, tt.tool, "open", tt.arguments, true)
		if classOf(err) != tt.class {
			t.Errorf("%s with %s %d = %v, want class %s", tt.tool, tt.route, tt.status, err, tt.class)
		}
		count := 0
		for _, request := range sent(f, before) {
			if request.method+" "+request.path == tt.route {
				count++
			}
		}
		if count != 1 {
			t.Errorf("%s sent %s %d times, want once", tt.tool, tt.route, count)
		}
	}
	// A comment delete refused by 404 is reported, and a timeout after a change says it may have been applied.
	s.override = map[string]int{"DELETE " + socialPrefix + "aa11/comments/7": 404}
	if _, err := invoke(t, core, gistCommentsDelete.ID, "open-listed", `{"gist_id":"aa11","comment_id":7}`, true); classOf(err) != provider.ClassNotFound {
		t.Errorf("repeated comment delete = %v, want not-found", err)
	}
	for _, call := range []struct{ tool, arguments, route string }{
		{gistCommentsCreate.ID, `{"gist_id":"aa11","body":"x"}`, "POST " + socialPrefix + "aa11/comments"},
		{gistForksCreate.ID, `{"gist_id":"aa11"}`, "POST " + socialPrefix + "aa11/forks"},
		{gistStarsSet.ID, `{"gist_id":"aa11","starred":true}`, "PUT " + socialPrefix + "aa11/star"},
	} {
		s.override = map[string]int{call.route: http.StatusGatewayTimeout}
		before := len(f.recorded())
		_, err := invoke(t, core, call.tool, "open", call.arguments, true)
		if err == nil || !strings.Contains(err.Error(), "may have been applied") {
			t.Errorf("%s after a timeout = %v, want an unclear outcome", call.tool, err)
		}
		count := 0
		for _, request := range sent(f, before) {
			if request.method+" "+request.path == call.route {
				count++
			}
		}
		if count != 1 {
			t.Errorf("%s retried %s: %d requests", call.tool, call.route, count)
		}
		if strings.Contains(fmt.Sprint(err), "Gateway") {
			t.Errorf("%s repeats provider text: %v", call.tool, err)
		}
	}
}

func TestSocialGistInputsAreChecked(t *testing.T) {
	_, _, core, reads := gistSocialRig(t)
	for _, call := range []struct{ tool, arguments string }{
		{gistCommentsList.ID, `{"gist_id":"not-hex"}`}, {gistCommentsGet.ID, `{"gist_id":"aa11","comment_id":0}`},
		{gistCommentsCreate.ID, `{"gist_id":"aa11","body":""}`},
		{gistCommentsCreate.ID, `{"gist_id":"aa11","body":"` + strings.Repeat("x", gistCommentBodyLimit+1) + `"}`},
		{gistCommentsUpdate.ID, `{"gist_id":"aa11","comment_id":7}`},
		{gistCommitsList.ID, `{"gist_id":"aa11","limit":101}`}, {gistForksList.ID, `{"gist_id":"aa11","cursor":"x"}`},
	} {
		before := *reads
		if _, err := invoke(t, core, call.tool, "open", call.arguments, true); !isInvalidRequest(err) {
			t.Errorf("%s %s = %v, want an invalid request", call.tool, call.arguments, err)
		}
		if *reads != before {
			t.Errorf("%s read a secret for an invalid request", call.tool)
		}
	}
}
