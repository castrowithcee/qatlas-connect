package github

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	commitMainSHA = "cccc333300000000000000000000000000000000"
	commitTagSHA  = "dddd444400000000000000000000000000000000"
)

func serveCommitChecks(t *testing.T) (*fakeGitHub, *fakePulls, *application.Core, *int) {
	t.Helper()
	f, p, base := servePulls(t)
	p.refs = map[string]string{"main": commitMainSHA, "v1.2.0": commitTagSHA, commitTagSHA: commitTagSHA}
	reads := 0
	red := &redact.Redactor{}
	return f, p, application.New(registry(t), coreConfig(base), resolver(red, &reads), red), &reads
}

func TestCommitChecksResolveTheRefOnceAndEvaluateTheCommit(t *testing.T) {
	f, p, core, _ := serveCommitChecks(t)
	p.runs = map[string][]fakeCheckRun{
		commitMainSHA: {checkRun("build", "completed", "success"), checkRun("test", "in_progress", "")},
		commitTagSHA:  {checkRun("build", "completed", "failure")},
	}
	p.runsTotal = map[string]int{commitMainSHA: 3}
	p.statuses = map[string][]fakeStatus{commitMainSHA: {{context: "ci/legacy", state: "success"}}}

	for _, tt := range []struct {
		ref, sha, overall string
		checks            int
		truncated         bool
	}{
		{"main", commitMainSHA, "pending", 3, true},
		{"v1.2.0", commitTagSHA, "failure", 1, false},
		{commitTagSHA, commitTagSHA, "failure", 1, false},
	} {
		before := len(f.recorded())
		result, err := invoke(t, core, commitChecksList.ID, "repo", `{"ref":"`+tt.ref+`"}`, false)
		if err != nil {
			t.Fatalf("%s: %v", tt.ref, err)
		}
		var got CommitChecks
		if err := json.Unmarshal(result, &got); err != nil {
			t.Fatal(err)
		}
		if got.Ref != tt.ref || got.SHA != tt.sha || got.Overall != tt.overall || len(got.Checks) != tt.checks ||
			got.Truncated != tt.truncated || len(got.CheckRuns) == 0 {
			t.Errorf("%s = %s, want sha %s, overall %s, %d checks, truncated %v, with the check runs", tt.ref,
				result, tt.sha, tt.overall, tt.checks, tt.truncated)
		}
		var paths []string
		for _, request := range f.recorded()[before:] {
			paths = append(paths, strings.TrimPrefix(request.path, actionsPrefix))
		}
		want := []string{"commits/" + tt.ref, "commits/" + tt.sha + "/check-runs", "commits/" + tt.sha + "/status"}
		if strings.Join(paths, " ") != strings.Join(want, " ") {
			t.Errorf("%s requests = %v, want %v", tt.ref, paths, want)
		}
	}

	// A commit without any check, and the filters of the check run read.
	p.runs[commitMainSHA] = nil
	p.statuses = nil
	result, err := invoke(t, core, commitChecksList.ID, "repo",
		`{"ref":"main","check_name":"build","status":"completed","filter":"all","app_id":15368}`, false)
	if err != nil || !strings.Contains(string(result), `"overall":"none"`) {
		t.Fatalf("filtered = %s, %v, want none", result, err)
	}
	runs := requestsTo(f, "/check-runs")
	if query := runs[len(runs)-1].query; query != "app_id=15368&check_name=build&filter=all&per_page=100&status=completed" {
		t.Errorf("check run query = %q, want every filter", query)
	}
}

func TestCommitChecksAnswerNotFoundAndPermission(t *testing.T) {
	f, _, core, _ := serveCommitChecks(t)
	for _, operation := range []string{commitChecksList.ID, commitChecksWait.ID} {
		if _, err := invoke(t, core, operation, "repo", `{"ref":"no-such-branch"}`, false); classOf(err) != provider.ClassNotFound ||
			!strings.Contains(err.Error(), "no-such-branch") {
			t.Errorf("%s unknown ref = %v, want not-found naming the ref", operation, err)
		}
	}
	route := f.failure
	f.failure = func(w http.ResponseWriter, r *http.Request) bool {
		if strings.HasSuffix(r.URL.Path, "/check-runs") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message":"Resource not accessible by personal access token"}`))
			return true
		}
		return route(w, r)
	}
	for _, operation := range []string{commitChecksList.ID, commitChecksWait.ID} {
		_, err := invoke(t, core, operation, "repo", `{"ref":"main"}`, false)
		if classOf(err) != provider.ClassPermission || !strings.Contains(err.Error(), "Checks: read") {
			t.Errorf("%s refused = %v, want permission naming the rights", operation, err)
		}
	}
}

func TestCommitChecksRefuseArgumentsAndTargetsBeforeACredentialIsRead(t *testing.T) {
	f, _, core, reads := serveCommitChecks(t)
	for _, operation := range []string{commitChecksList.ID, commitChecksWait.ID} {
		for _, arguments := range []string{`{}`, `{"ref":"a..b"}`, `{"ref":"main","status":"waiting"}`,
			`{"ref":"main","filter":"newest"}`, `{"ref":"main","app_id":0}`, `{"ref":"main","number":7}`,
			`{"ref":"main","repository":"octo-org/other"}`} {
			if _, err := invoke(t, core, operation, "repo", arguments, false); !isInvalidRequest(err) {
				t.Errorf("%s %s = %v, want an invalid request", operation, arguments, err)
			}
		}
		if _, err := invoke(t, core, operation, "planning", `{"ref":"main"}`, false); err == nil {
			t.Errorf("%s ran on a project connection", operation)
		}
	}
	cfg := coreConfig("http://127.0.0.1:1")
	cfg.Connections["ex"] = config.Connection{Service: "gh", Credential: "gh-reader", Exclude: []string{repoTarget}}
	red := &redact.Redactor{}
	excluded := application.New(registry(t), cfg, resolver(red, reads), red)
	for _, operation := range []string{commitChecksList.ID, commitChecksWait.ID} {
		if _, err := invoke(t, excluded, operation, "ex", `{"repository":"octo-org/example","ref":"main"}`, false); !isInvalidRequest(err) {
			t.Errorf("%s excluded = %v, want an invalid request", operation, err)
		}
	}
	if *reads != 0 || len(f.recorded()) != 0 {
		t.Errorf("a refused request read %d credentials and sent %d requests", *reads, len(f.recorded()))
	}
}

func TestCommitChecksToolsAreGroupedAndOfferedLikeThePullRequestChecks(t *testing.T) {
	reg := registry(t)
	if err := reg.ValidateProfiles(); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{commitChecksList.ID, commitChecksWait.ID} {
		if group := groupOf(id); group != "pull-requests" {
			t.Errorf("%s group = %q, want pull-requests", id, group)
		}
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	for _, profile := range metadata.Profiles {
		lists := strings.Contains(strings.Join(profile.Tools, " "), pullChecksList.ID)
		for _, id := range []string{commitChecksList.ID, commitChecksWait.ID} {
			if strings.Contains(strings.Join(profile.Tools, " "), id) != lists {
				t.Errorf("profile %s offers %s = %v, pull request checks = %v", profile.ID, id, !lists, lists)
			}
		}
	}
	if !strings.Contains(commitChecksWait.Description, "qconnect wait github.commit-checks.wait") ||
		!commitChecksWait.Waits || commitChecksWait.Risk != commitChecksList.Risk {
		t.Errorf("descriptor = %+v, want the command named, Waits set, and the risk of the list", commitChecksWait)
	}
}

// newCommitWaitFixture is newWaitFixture for the commit wait: a poll begins with the check run read.
func newCommitWaitFixture(t *testing.T, onPoll func(poll int, p *fakePulls) int) *waitFixture {
	t.Helper()
	w := newWaitFixture(t, func(int, *fakePulls) int { return 0 })
	w.p.refs = map[string]string{"main": waitOldHead}
	inner := w.f.failure
	w.f.failure = func(rw http.ResponseWriter, r *http.Request) bool {
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/check-runs") {
			w.mu.Lock()
			w.poll++
			status := onPoll(w.poll, w.p)
			w.mu.Unlock()
			if status != 0 {
				rw.WriteHeader(status)
				return true
			}
		}
		return inner(rw, r)
	}
	return w
}

func commitWait(w *waitFixture, ctx context.Context, arguments string) (CommitChecksWait, error) {
	w.t.Helper()
	response, err := w.core.Invoke(ctx, application.InvokeRequest{Operation: commitChecksWait.ID,
		Connection: "repo", Arguments: json.RawMessage(arguments)})
	var got CommitChecksWait
	if err == nil {
		if err := json.Unmarshal(response.Result, &got); err != nil {
			w.t.Fatalf("result %s: %v", response.Result, err)
		}
	}
	return got, err
}

func TestCommitWaitEndsWithTheResultOfTheChecks(t *testing.T) {
	finishing := func(conclusion string) func(int, *fakePulls) int {
		return func(poll int, p *fakePulls) int {
			runs := []fakeCheckRun{checkRun("build", "completed", "success"), checkRun("test", "in_progress", "")}
			if poll >= 3 {
				runs[1] = checkRun("test", "completed", conclusion)
			}
			p.runs = map[string][]fakeCheckRun{waitOldHead: runs}
			return 0
		}
	}
	for conclusion, result := range map[string]string{"success": "pass", "failure": "fail"} {
		for name, ctx := range map[string]context.Context{"broker": context.Background(), "command": cliWait(5 * time.Second)} {
			w := newCommitWaitFixture(t, finishing(conclusion))
			got, err := commitWait(w, ctx, `{"ref":"main"}`)
			if err != nil || got.Result != result || got.Polls != 3 || got.Ref != "main" || got.SHA != waitOldHead ||
				len(got.Checks) != 2 {
				t.Errorf("%s %s: got %+v, %v, want %s after 3 polls", conclusion, name, got, err, result)
			}
		}
	}

	w := newCommitWaitFixture(t, func(int, *fakePulls) int { return 0 })
	if got, err := commitWait(w, context.Background(), `{"ref":"main"}`); err != nil || got.Result != "none" ||
		got.Polls != checksWaitNonePolls || len(got.Checks) != 0 {
		t.Errorf("no check: got %+v, %v, want none after %d polls", got, err, checksWaitNonePolls)
	}

	running := func(_ int, p *fakePulls) int {
		p.runs = map[string][]fakeCheckRun{waitOldHead: {checkRun("build", "in_progress", "")}}
		return 0
	}
	w = newCommitWaitFixture(t, running)
	if got, err := commitWait(w, cliWait(60*time.Millisecond), `{"ref":"main"}`); err != nil || got.Result != "timeout" ||
		got.SHA != waitOldHead || len(got.Checks) != 1 || got.Polls < 2 {
		t.Errorf("command: got %+v, %v, want timeout with the checks of the last poll", got, err)
	}
	checksWaitSlice = 60 * time.Millisecond
	if got, err := commitWait(w, context.Background(), `{"ref":"main"}`); err != nil || got.Result != "pending" ||
		got.SHA != waitOldHead || len(got.Checks) != 1 {
		t.Errorf("broker: got %+v, %v, want pending with the awaited commit", got, err)
	}
}

// A branch that moves on while the wait runs does not change the awaited commit.
func TestCommitWaitStaysAtTheCommitTheRefNamedAtTheStart(t *testing.T) {
	w := newCommitWaitFixture(t, func(poll int, p *fakePulls) int {
		p.refs["main"] = waitNewHead
		p.runs = map[string][]fakeCheckRun{
			waitOldHead: {checkRun("build", "in_progress", "")},
			waitNewHead: {checkRun("build", "completed", "failure")},
		}
		if poll >= 2 {
			p.runs[waitOldHead] = []fakeCheckRun{checkRun("build", "completed", "success")}
		}
		return 0
	})
	got, err := commitWait(w, cliWait(5*time.Second), `{"ref":"main"}`)
	if err != nil || got.Result != "pass" || got.SHA != waitOldHead || got.Polls != 2 {
		t.Errorf("got %+v, %v, want pass at the first commit after 2 polls", got, err)
	}
	if resolved := requestsTo(w.f, "/commits/main"); len(resolved) != 1 {
		t.Errorf("ref resolved %d times, want once", len(resolved))
	}
	for _, request := range requestsTo(w.f, "/commits/"+waitNewHead) {
		t.Errorf("request %s reads the commit the branch moved to", request.path)
	}
}
