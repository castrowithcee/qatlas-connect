package github

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	historyAPI    = "https://api.github.com/repos/octo-org/example"
	historyCanary = "PROVIDERCANARY-history"
)

var historyUser = `{"login":"octocat","id":1,"node_id":"MDQ6VXNlcjE=","avatar_url":"https://github.com/images/error/octocat_happy.gif",` +
	`"gravatar_id":"","url":"https://api.github.com/users/octocat","html_url":"https://github.com/octocat",` +
	`"type":"User","site_admin":false}`

// The events GitHub documents for an issue, with the members every variant may carry.
var historyEvents = `[{"id":1,"node_id":"MDEwOklzc3VlRXZlbnQx","url":"` + historyAPI + `/issues/events/1",` +
	`"actor":` + historyUser + `,"event":"labeled","commit_id":null,"commit_url":null,` +
	`"created_at":"2011-04-14T16:00:49Z","label":{"name":"bug","color":"f29513"},` +
	`"performed_via_github_app":{"id":1,"slug":"octoapp","node_id":"MDExOkludGVncmF0aW9uMQ==","name":"Octocat App"}},` +
	`{"id":2,"node_id":"E2","url":"` + historyAPI + `/issues/events/2","actor":` + historyUser + `,"event":"assigned",` +
	`"created_at":"2011-04-14T16:00:50Z","assignee":` + historyUser + `,"assigner":` + historyUser + `},` +
	`{"id":3,"node_id":"E3","event":"milestoned","created_at":"2011-04-14T16:00:51Z","milestone":{"title":"v1"}},` +
	`{"id":4,"node_id":"E4","event":"renamed","created_at":"2011-04-14T16:00:52Z","rename":{"from":"old","to":"new"}},` +
	`{"id":5,"node_id":"E5","event":"review_requested","created_at":"2011-04-14T16:00:53Z","review_requester":` + historyUser +
	`,"requested_reviewer":` + historyUser + `,"requested_team":{"id":1,"node_id":"T1","name":"Justice League","slug":"justice-league",` +
	`"privacy":"closed","permission":"admin","permissions":{"pull":true,"push":false},"url":"https://api.github.com/teams/1",` +
	`"html_url":"https://github.com/orgs/octo-org/teams/justice-league","members_url":"https://api.github.com/teams/1/members{/member}",` +
	`"parent":{"id":2,"slug":"league"}}},` +
	`{"id":6,"node_id":"E6","event":"review_dismissed","created_at":"2011-04-14T16:00:54Z","dismissed_review":` +
	`{"state":"approved","review_id":42,"dismissal_message":"stale","dismissal_commit_id":"abc123"}},` +
	`{"id":7,"node_id":"E7","event":"locked","created_at":"2011-04-14T16:00:55Z","lock_reason":"too heated"},` +
	`{"id":8,"node_id":"E8","event":"added_to_project","created_at":"2011-04-14T16:00:56Z","project_card":` +
	`{"id":9,"url":"https://api.github.com/projects/columns/cards/9","project_id":3,"project_url":"https://api.github.com/projects/3",` +
	`"column_name":"Todo","previous_column_name":"Backlog"}},` +
	`{"id":9,"node_id":"E9","event":"referenced","created_at":"2011-04-14T16:00:57Z","commit_id":"6dcb09b5b57875f334f61aebed695e2e4193db5e",` +
	`"commit_url":"https://api.github.com/repos/octo-org/secret/commits/6dcb09b5b57875f334f61aebed695e2e4193db5e","author_association":"OWNER"},` +
	`{"id":10,"node_id":"E10","event":"closed","created_at":"2011-04-14T16:00:58Z","commit_id":"6dcb09b5",` +
	`"commit_url":"` + historyAPI + `/commits/6dcb09b5"}]`

func historyCore(t *testing.T, body string, next bool) (*fakeGitHub, *application.Core) {
	t.Helper()
	return cannedCore(t, body, next, relationConfig)
}

func TestIssueEventsReportEveryDocumentedMemberAndPage(t *testing.T) {
	f, core := historyCore(t, historyEvents, true)
	got, err := invoke(t, core, issueEventsList.ID, "repo", `{"number":42,"limit":5}`, false)
	if err != nil {
		t.Fatal(err)
	}
	coversSchema(t, issueEventsList.ID, got)
	request := lastRequest(t, f)
	query, _ := url.ParseQuery(request.query)
	if request.method != http.MethodGet || request.path != "/api/v3/repos/octo-org/example/issues/42/events" ||
		query.Get("per_page") != "5" || query.Get("page") != "1" {
		t.Errorf("request = %+v, want the fixed events route with paging", request)
	}
	first := members(t, got, "events", "#0")
	wantMembers(t, "labeled", first, map[string]string{"event": `"labeled"`, "label": `{"name":"bug","color":"f29513"}`})
	wantMembers(t, "app", members(t, got, "events", "#0", "performed_via_github_app"), map[string]string{"slug": `"octoapp"`})
	wantMembers(t, "milestone", members(t, got, "events", "#2"), map[string]string{"milestone": `{"title":"v1"}`})
	wantMembers(t, "rename", members(t, got, "events", "#3"), map[string]string{"rename": `{"from":"old","to":"new"}`})
	team := members(t, got, "events", "#4", "requested_team")
	wantMembers(t, "team", team, map[string]string{"slug": `"justice-league"`, "permissions": `{"pull":true,"push":false}`})
	wantMembers(t, "parent", members(t, got, "events", "#4", "requested_team", "parent"), map[string]string{"slug": `"league"`})
	wantMembers(t, "dismissed", members(t, got, "events", "#5"), map[string]string{
		"dismissed_review": `{"state":"approved","review_id":42,"dismissal_message":"stale","dismissal_commit_id":"abc123"}`})
	wantMembers(t, "locked", members(t, got, "events", "#6"), map[string]string{"lock_reason": `"too heated"`})
	wantMembers(t, "card", members(t, got, "events", "#7", "project_card"), map[string]string{"column_name": `"Todo"`})
	// A commit of a repository outside the targets keeps its SHA and repository, never its address.
	foreign := members(t, got, "events", "#8")
	wantMembers(t, "foreign commit", foreign, map[string]string{
		"commit_id": `"6dcb09b5b57875f334f61aebed695e2e4193db5e"`, "commit_repository": `"octo-org/secret"`})
	if _, ok := foreign["links"]; ok || strings.Contains(string(got), "octo-org/secret/commits") {
		t.Errorf("event = %s, leaked the address of a commit outside the targets", got)
	}
	own := members(t, got, "events", "#9")
	wantMembers(t, "own commit", own, map[string]string{"links": `{"commit_url":"` + historyAPI + `/commits/6dcb09b5"}`})
	if _, ok := own["commit_repository"]; ok {
		t.Error("a commit of the bound repository names its repository")
	}
	if !strings.Contains(string(got), `"has_more":true`) || !strings.Contains(string(got), `"next_cursor"`) {
		t.Errorf("answer = %s, want a continuation", got)
	}
	var cursor string
	for _, part := range strings.Split(string(got), `"next_cursor":"`)[1:] {
		cursor, _, _ = strings.Cut(part, `"`)
	}
	if _, err := invoke(t, core, issueEventsList.ID, "repo", fmt.Sprintf(`{"number":43,"cursor":%q}`, cursor), false); !isInvalidRequest(err) {
		t.Errorf("a cursor of another issue = %v, want an invalid request", err)
	}
	if _, err := invoke(t, core, issueEventsList.ID, "repo", fmt.Sprintf(`{"number":42,"cursor":%q}`, cursor), false); err != nil {
		t.Fatal(err)
	}
	query, _ = url.ParseQuery(lastRequest(t, f).query)
	if query.Get("page") != "2" || query.Get("per_page") != "5" {
		t.Errorf("continuation query = %v, want page 2 of the first batch size", query)
	}
}

func TestIssueEventsOfTheRepositoryCarryTheirIssue(t *testing.T) {
	body := `[{"id":1,"event":"closed","created_at":"2011-04-14T16:00:49Z","issue":{"number":7,"title":"Crash","state":"closed",` +
		`"html_url":"https://github.com/octo-org/example/issues/7","body":"details","user":` + historyUser +
		`,"pull_request":{"url":"x"},"repository_url":"` + historyAPI + `"}},` +
		`{"id":2,"event":"closed","issue":{"number":8,"title":"` + historyCanary + `","repository":{"full_name":"octo-org/secret"}}}]`
	f, core := historyCore(t, body, false)
	got, err := invoke(t, core, issueEventsList.ID, "repo", `{}`, false)
	if err != nil {
		t.Fatal(err)
	}
	coversSchema(t, issueEventsList.ID, got)
	if request := lastRequest(t, f); request.path != "/api/v3/repos/octo-org/example/issues/events" {
		t.Errorf("path = %s, want the repository events route", request.path)
	}
	wantMembers(t, "issue", members(t, got, "events", "#0", "issue"), map[string]string{
		"number": `7`, "title": `"Crash"`, "is_pull_request": `true`, "body": `"details"`})
	other := members(t, got, "events", "#1", "issue")
	wantMembers(t, "withheld issue", other, map[string]string{"number": `8`, "repository": `"octo-org/secret"`})
	if _, ok := other["title"]; ok || strings.Contains(string(got), historyCanary) {
		t.Errorf("answer = %s, leaked an issue outside the targets", got)
	}
}

// A timeline of every variant GitHub documents, one event of an unknown type, and references to objects
// outside the targets.
var historyTimeline = `[` +
	`{"event":"commented","id":10,"node_id":"IC1","url":"` + historyAPI + `/issues/comments/10","html_url":"https://github.com/octo-org/example/issues/42#issuecomment-10",` +
	`"issue_url":"` + historyAPI + `/issues/42","user":` + historyUser + `,"actor":` + historyUser + `,"body":"hello","body_html":"<p>x</p>",` +
	`"created_at":"2011-04-14T16:00:49Z","updated_at":"2011-04-14T16:00:50Z","author_association":"OWNER",` +
	`"reactions":{"url":"` + historyAPI + `/issues/comments/10/reactions","total_count":1,"+1":1}},` +
	`{"event":"cross-referenced","created_at":"2011-04-14T16:00:49Z","updated_at":"2011-04-14T16:00:49Z","actor":` + historyUser +
	`,"source":{"type":"issue","issue":{"number":5,"title":"Own title","state":"open","html_url":"https://github.com/octo-org/example/issues/5",` +
	`"repository":{"full_name":"octo-org/example"}}}},` +
	`{"event":"cross-referenced","created_at":"2011-04-14T16:00:49Z","actor":` + historyUser +
	`,"source":{"type":"issue","issue":{"number":9,"title":"` + historyCanary + `","body":"` + historyCanary + `","state":"open",` +
	`"repository":{"full_name":"octo-org/secret"},"pull_request":{"url":"x"}}}},` +
	`{"event":"cross-referenced","source":{"type":"issue","issue":{"number":3,"title":"` + historyCanary + `"}}},` +
	`{"event":"committed","sha":"6dcb09b5b57875f334f61aebed695e2e4193db5e","node_id":"C1","url":"` + historyAPI + `/git/commits/6dcb09b5",` +
	`"html_url":"https://github.com/octo-org/example/commit/6dcb09b5","author":{"date":"2011-04-14T16:00:49Z","email":"a@example.invalid","name":"A"},` +
	`"committer":{"date":"2011-04-14T16:00:49Z","email":"c@example.invalid","name":"C"},"message":"fix\n\n` + strings.Repeat("m", 5000) + `",` +
	`"tree":{"sha":"t1","url":"` + historyAPI + `/git/trees/t1"},"parents":[{"sha":"p1","url":"` + historyAPI + `/git/commits/p1",` +
	`"html_url":"https://github.com/octo-org/example/commit/p1"}],"verification":{"verified":false,"reason":"unsigned","payload":null,"signature":null}},` +
	`{"event":"reviewed","id":77,"node_id":"R1","user":` + historyUser + `,"body":"` + strings.Repeat("r", 5000) + `","state":"approved",` +
	`"html_url":"https://github.com/octo-org/example/pull/42#pullrequestreview-77","pull_request_url":"` + historyAPI + `/pulls/42",` +
	`"submitted_at":"2011-04-14T16:00:49Z","commit_id":"6dcb09b5","author_association":"MEMBER","_links":{"html":{"href":"x"}}},` +
	`{"event":"line-commented","node_id":"L1","comments":[{"id":5,"node_id":"RC5","path":"a.go","line":3,"side":"RIGHT","commit_id":"c1",` +
	`"body":"` + strings.Repeat("l", 5000) + `","diff_hunk":"@@ -1 +1 @@","user":` + historyUser + `,"pull_request_review_id":77,` +
	`"created_at":"2011-04-14T16:00:49Z","updated_at":"2011-04-14T16:00:49Z","html_url":"https://github.com/octo-org/example/pull/42#discussion_r5"}]},` +
	`{"event":"commit-commented","node_id":"M1","comments":[{"id":6,"node_id":"CC6","body":"nice","commit_id":"c1","user":` + historyUser +
	`,"created_at":"2011-04-14T16:00:49Z","html_url":"https://github.com/octo-org/example/commit/c1#commitcomment-6"}]},` +
	`{"event":"referenced","commit_id":"abcdef12","commit_url":"https://api.github.com/repos/octo-org/secret/commits/abcdef12"},` +
	`{"event":"closed","state_reason":"completed","id":99,"actor":` + historyUser + `},` +
	`{"event":"brand_new_event","id":100,"node_id":"N1","created_at":"2011-04-14T16:00:49Z","label":"not-an-object","body":"` + historyCanary + `"},` +
	`{"event":"mystery","id":101,"actor":"not-an-account","created_at":"2011-04-14T16:00:49Z"}]`

func TestIssueTimelineReportsEveryVariantAndReducesForeignObjects(t *testing.T) {
	f, core := historyCore(t, historyTimeline, false)
	got, err := invoke(t, core, issueTimelineList.ID, "repo", `{"number":42}`, false)
	if err != nil {
		t.Fatal(err)
	}
	coversSchema(t, issueTimelineList.ID, got)
	if request := lastRequest(t, f); request.path != "/api/v3/repos/octo-org/example/issues/42/timeline" {
		t.Errorf("path = %s, want the fixed timeline route", request.path)
	}
	text := string(got)
	// Nothing of an object outside the targets but identifiers.
	if strings.Contains(text, "octo-org/secret/commits") {
		t.Errorf("timeline = %s, leaked the address of a commit outside the targets", text)
	}
	for _, leaked := range []string{`"title":"` + historyCanary} {
		if strings.Contains(text, leaked) {
			t.Errorf("timeline leaked %s", leaked)
		}
	}
	wantMembers(t, "comment", members(t, got, "events", "#0"), map[string]string{"body": `"hello"`, "event": `"commented"`,
		"url": `"https://github.com/octo-org/example/issues/42#issuecomment-10"`})
	if strings.Contains(text, "body_html") || strings.Contains(text, "<p>x</p>") {
		t.Error("the rendered body is not left out")
	}
	own := members(t, got, "events", "#1", "source")
	wantMembers(t, "own source", own, map[string]string{"type": `"issue"`})
	wantMembers(t, "own source issue", members(t, got, "events", "#1", "source", "issue"), map[string]string{"title": `"Own title"`})
	foreign := members(t, got, "events", "#2", "source")
	wantMembers(t, "foreign source", foreign, map[string]string{"withheld": `true`})
	wantMembers(t, "foreign source issue", members(t, got, "events", "#2", "source", "issue"), map[string]string{
		"number": `9`, "repository": `"octo-org/secret"`, "is_pull_request": `true`})
	unknown := members(t, got, "events", "#3", "source")
	wantMembers(t, "unknown source", unknown, map[string]string{"withheld": `true`})
	if _, ok := unknown["issue"]; ok {
		t.Error("an issue of unknown repository is shown")
	}
	committed := members(t, got, "events", "#4")
	wantMembers(t, "committed", committed, map[string]string{"sha": `"6dcb09b5b57875f334f61aebed695e2e4193db5e"`,
		"message_truncated": `true`, "tree": `{"sha":"t1","links":{"url":"` + historyAPI + `/git/trees/t1"}}`})
	wantMembers(t, "committed author", members(t, got, "events", "#4", "author"), map[string]string{"email": `"a@example.invalid"`})
	wantMembers(t, "parent", members(t, got, "events", "#4", "parents", "#0"), map[string]string{"sha": `"p1"`})
	wantMembers(t, "reviewed", members(t, got, "events", "#5"), map[string]string{"state": `"approved"`, "body_truncated": `true`})
	if body := string(members(t, got, "events", "#5")["body"]); len(body) > 4100 {
		t.Errorf("review body holds %d bytes, want it cut", len(body))
	}
	line := members(t, got, "events", "#6", "line_comments", "#0")
	wantMembers(t, "line comment", line, map[string]string{"path": `"a.go"`, "body_truncated": `true`, "diff_hunk": `"@@ -1 +1 @@"`})
	wantMembers(t, "commit comment", members(t, got, "events", "#7", "commit_comments", "#0"), map[string]string{"body": `"nice"`})
	ref := members(t, got, "events", "#8")
	wantMembers(t, "foreign reference", ref, map[string]string{"commit_id": `"abcdef12"`, "commit_repository": `"octo-org/secret"`})
	if _, ok := ref["links"]; ok {
		t.Error("a commit outside the targets keeps an address")
	}
	wantMembers(t, "closed", members(t, got, "events", "#9"), map[string]string{"state_reason": `"completed"`})
	// An event of an unknown type, or of an unexpected shape, keeps its type and its basic members.
	wantMembers(t, "new type", members(t, got, "events", "#10"), map[string]string{"event": `"brand_new_event"`, "id": `100`})
	wantMembers(t, "odd shape", members(t, got, "events", "#11"), map[string]string{"event": `"mystery"`, "id": `101`})
}

func TestAssigneesListAndCheck(t *testing.T) {
	f, core := historyCore(t, `[`+historyUser+`]`, true)
	got, err := invoke(t, core, assigneesList.ID, "repo", `{"limit":10}`, false)
	if err != nil {
		t.Fatal(err)
	}
	coversSchema(t, assigneesList.ID, got)
	request := lastRequest(t, f)
	if request.path != "/api/v3/repos/octo-org/example/assignees" || !strings.Contains(request.query, "per_page=10") {
		t.Errorf("request = %+v", request)
	}
	wantMembers(t, "assignee", members(t, got, "assignees", "#0"), map[string]string{"login": `"octocat"`, "type": `"User"`})
	if !strings.Contains(string(got), `"has_more":true`) {
		t.Errorf("answer = %s, want a continuation", got)
	}

	for _, tt := range []struct {
		status int
		want   string
	}{{http.StatusNoContent, `true`}, {http.StatusNotFound, `false`}} {
		f := &fakeGitHub{}
		f.failure = func(w http.ResponseWriter, _ *http.Request) bool {
			w.WriteHeader(tt.status)
			if tt.status == http.StatusNotFound {
				fmt.Fprintf(w, `{"message":%q}`, historyCanary)
			}
			return true
		}
		red := &redact.Redactor{}
		core := application.New(registry(t), relationConfig(serve(t, f)), resolver(red, nil), red)
		got, err := invoke(t, core, assigneesCheck.ID, "repo", `{"assignee":"octo-cat_1"}`, false)
		if err != nil {
			t.Fatalf("check %d = %v", tt.status, err)
		}
		coversSchema(t, assigneesCheck.ID, got)
		wantMembers(t, "check", members(t, got), map[string]string{"assignee": `"octo-cat_1"`, "assignable": tt.want})
		if request := lastRequest(t, f); request.path != "/api/v3/repos/octo-org/example/assignees/octo-cat_1" {
			t.Errorf("path = %s, want the fixed assignee route", request.path)
		}
	}
}

func TestIssueHistoryRefusalsAreNamedWithoutProviderText(t *testing.T) {
	tools := []struct{ id, arguments string }{
		{issueEventsList.ID, `{"number":42}`}, {issueEventsList.ID, `{}`}, {issueTimelineList.ID, `{"number":42}`},
		{assigneesList.ID, `{}`}, {assigneesCheck.ID, `{"assignee":"octocat"}`},
	}
	for _, tt := range []struct {
		status int
		class  provider.Class
		check  bool
	}{
		{http.StatusForbidden, provider.ClassPermission, true},
		{http.StatusNotFound, provider.ClassNotFound, false},
		{http.StatusUnprocessableEntity, provider.ClassProviderError, true},
		{http.StatusGatewayTimeout, provider.ClassTimeout, true},
	} {
		f := &fakeGitHub{}
		f.failure = func(w http.ResponseWriter, _ *http.Request) bool {
			w.WriteHeader(tt.status)
			fmt.Fprintf(w, `{"message":%q}`, historyCanary)
			return true
		}
		red := &redact.Redactor{}
		core := application.New(registry(t), relationConfig(serve(t, f)), resolver(red, nil), red)
		for _, tool := range tools {
			_, err := invoke(t, core, tool.id, "repo", tool.arguments, false)
			if tool.id == assigneesCheck.ID && !tt.check {
				if err != nil {
					t.Errorf("%s answered %d = %v, want a result", tool.id, tt.status, err)
				}
				continue
			}
			if classOf(err) != tt.class || strings.Contains(err.Error(), historyCanary) {
				t.Errorf("%s answered %d = %v, want %s without GitHub's text", tool.id, tt.status, err, tt.class)
			}
		}
	}
}

func TestIssueHistoryChecksTargetsAndArgumentsBeforeIO(t *testing.T) {
	f, core, reads := guardedCore(t)
	before := len(f.recorded())
	for _, tt := range []struct{ name, id, connection, arguments string }{
		{"foreign repository", issueEventsList.ID, "repo", `{"repository":"octo-org/other","number":42}`},
		{"foreign repository timeline", issueTimelineList.ID, "repo", `{"repository":"octo-org/other","number":42}`},
		{"foreign repository assignees", assigneesList.ID, "repo", `{"repository":"octo-org/other"}`},
		{"foreign repository check", assigneesCheck.ID, "repo", `{"repository":"octo-org/other","assignee":"octocat"}`},
		{"excluded repository", issueTimelineList.ID, "repoexcluded", `{"repository":"octo-org/example","number":42}`},
		{"excluded repository check", assigneesCheck.ID, "repoexcluded", `{"repository":"octo-org/example","assignee":"octocat"}`},
		{"project connection", issueEventsList.ID, "planning", `{"number":42}`},
		{"timeline without number", issueTimelineList.ID, "repo", `{}`},
		{"number zero", issueEventsList.ID, "repo", `{"number":0}`},
		{"bad limit", assigneesList.ID, "repo", `{"limit":101}`},
		{"bad cursor", assigneesList.ID, "repo", `{"cursor":"x"}`},
		{"assignee with a slash", assigneesCheck.ID, "repo", `{"assignee":"a/../b"}`},
		{"assignee with a bot suffix", assigneesCheck.ID, "repo", `{"assignee":"app[bot]"}`},
		{"missing assignee", assigneesCheck.ID, "repo", `{}`},
	} {
		if _, err := invoke(t, core, tt.id, tt.connection, tt.arguments, false); err == nil {
			t.Errorf("%s was accepted", tt.name)
		}
	}
	if *reads != 0 || len(f.recorded()) != before {
		t.Errorf("refused calls read %d credentials and sent %d requests", *reads, len(f.recorded())-before)
	}
}

// The assignee login travels as one escaped path segment of the bound repository.
func TestAssigneeCheckSendsOneEscapedSegment(t *testing.T) {
	f, core := historyCore(t, ``, false)
	f.failure = func(w http.ResponseWriter, _ *http.Request) bool { w.WriteHeader(http.StatusNoContent); return true }
	if _, err := invoke(t, core, assigneesCheck.ID, "repo", `{"assignee":"Octo-Cat"}`, false); err != nil {
		t.Fatal(err)
	}
	if request := lastRequest(t, f); request.method != http.MethodGet ||
		request.path != "/api/v3/repos/octo-org/example/assignees/Octo-Cat" {
		t.Errorf("request = %+v", request)
	}
}

func historyReference(repo string, number int, title string) string {
	return fmt.Sprintf(`{"number":%d,"title":%q,"state":"open","state_reason":null,"repository":{"id":9,"node_id":"R9",`+
		`"name":%q,"full_name":%q,"private":false,"html_url":"https://github.com/%s","description":%q},`+
		`"issue_type":{"id":4,"node_id":"IT4","name":"Bug","color":"red"}}`, number, title,
		repo[strings.Index(repo, "/")+1:], repo, repo, title)
}

// The events GitHub added for agents, issue types, sub-issues, and dependencies carry their own members.
var historyNewEvents = `[` +
	`{"id":1,"event":"labeled","created_at":"2026-01-01T00:00:00Z","label":{"name":"bug","color":"f29513"},` +
	`"intent":{"rationale":"` + strings.Repeat("i", 1500) + `","confidence":"HIGH"}},` +
	`{"id":2,"event":"issue_type_changed","created_at":"2026-01-01T00:00:00Z","issue_type":{"id":4,"name":"Bug","color":"red"},` +
	`"prev_issue_type":{"id":3,"name":"Task","color":null}},` +
	`{"id":3,"event":"sub_issue_added","created_at":"2026-01-01T00:00:00Z","sub_issue":` + historyReference("octo-org/example", 7, "Inside") + `},` +
	`{"id":4,"event":"parent_issue_added","created_at":"2026-01-01T00:00:00Z","parent_issue":` + historyReference("octo-org/secret", 8, historyCanary) + `},` +
	`{"id":5,"event":"blocked_by_added","created_at":"2026-01-01T00:00:00Z","blocked_by":{"number":9,"title":"` + historyCanary + `","state":"open"}},` +
	`{"id":6,"event":"blocking_added","created_at":"2026-01-01T00:00:00Z","blocking":` + historyReference("octo-org/example", 10, "Blocks") + `}]`

func TestIssueEventsReportIntentTypesAndIssueReferences(t *testing.T) {
	for _, tool := range []struct{ id, arguments string }{
		{issueEventsList.ID, `{"number":42}`}, {issueEventsList.ID, `{}`}, {issueTimelineList.ID, `{"number":42}`}} {
		_, core := historyCore(t, historyNewEvents, false)
		got, err := invoke(t, core, tool.id, "repo", tool.arguments, false)
		if err != nil {
			t.Fatal(err)
		}
		coversSchema(t, tool.id, got)
		intent := members(t, got, "events", "#0", "intent")
		wantMembers(t, "intent", intent, map[string]string{"confidence": `"HIGH"`, "rationale_truncated": `true`})
		if len(string(intent["rationale"])) > 1100 {
			t.Errorf("rationale holds %d bytes, want it cut", len(intent["rationale"]))
		}
		wantMembers(t, "type", members(t, got, "events", "#1"), map[string]string{
			"issue_type": `{"id":4,"name":"Bug","color":"red"}`, "prev_issue_type": `{"id":3,"name":"Task"}`})
		inside := members(t, got, "events", "#2", "sub_issue")
		wantMembers(t, "sub_issue", inside, map[string]string{"number": `7`, "title": `"Inside"`, "state": `"open"`,
			"issue_type": `{"id":4,"node_id":"IT4","name":"Bug","color":"red"}`})
		wantMembers(t, "repository", members(t, got, "events", "#2", "sub_issue", "repository"),
			map[string]string{"full_name": `"octo-org/example"`, "description": `"Inside"`})
		// Outside the targets or without a repository, an issue shows identifiers only.
		foreign := members(t, got, "events", "#3", "parent_issue")
		wantMembers(t, "foreign reference", foreign, map[string]string{"number": `8`, "withheld": `true`,
			"repository": `{"id":9,"node_id":"R9","full_name":"octo-org/secret"}`})
		unknown := members(t, got, "events", "#4", "blocked_by")
		wantMembers(t, "unknown reference", unknown, map[string]string{"withheld": `true`})
		if _, ok := unknown["number"]; ok {
			t.Error("a reference of unknown repository shows its number")
		}
		wantMembers(t, "blocking", members(t, got, "events", "#5", "blocking"), map[string]string{"number": `10`})
		if strings.Contains(string(got), historyCanary) {
			t.Errorf("%s leaked an issue outside the targets: %s", tool.id, got)
		}
	}
}

func TestIssueTimelineReportsCommentPinsAndCopilotLineComments(t *testing.T) {
	body := `[{"event":"commented","id":1,"url":"` + historyAPI + `/issues/comments/1","issue_url":"` + historyAPI + `/issues/42",` +
		`"body":"x","minimized":{"reason":"spam"},"pin":{"pinned_at":"2026-01-01T00:00:00Z","pinned_by":` + historyUser + `}},` +
		`{"event":"reviewed","id":2,"pull_request_url":"` + historyAPI + `/pulls/42","state":"approved"},` +
		`{"event":"line-commented","comments":[{"id":5,"path":"a.go","is_copilot_code_review":true,` +
		`"copilot_code_review_sources":[{"kind":"file","name":"n","path":"p.go"}],"pull_request_url":"` + historyAPI + `/pulls/42",` +
		`"url":"` + historyAPI + `/pulls/comments/5","reactions":{"total_count":2}}]}]`
	_, core := historyCore(t, body, false)
	got, err := invoke(t, core, issueTimelineList.ID, "repo", `{"number":42}`, false)
	if err != nil {
		t.Fatal(err)
	}
	coversSchema(t, issueTimelineList.ID, got)
	wantMembers(t, "comment", members(t, got, "events", "#0"), map[string]string{"minimized": `{"reason":"spam"}`,
		"links": `{"url":"` + historyAPI + `/issues/comments/1","issue_url":"` + historyAPI + `/issues/42"}`})
	wantMembers(t, "pin", members(t, got, "events", "#0", "pin"), map[string]string{"pinned_at": `"2026-01-01T00:00:00Z"`})
	wantMembers(t, "pin account", members(t, got, "events", "#0", "pin", "pinned_by"), map[string]string{"login": `"octocat"`})
	wantMembers(t, "review", members(t, got, "events", "#1"), map[string]string{
		"links": `{"pull_request_url":"` + historyAPI + `/pulls/42"}`})
	line := members(t, got, "events", "#2", "line_comments", "#0")
	wantMembers(t, "line comment", line, map[string]string{"is_copilot_code_review": `true`,
		"copilot_code_review_sources": `[{"kind":"file","name":"n","path":"p.go"}]`})
}

func TestIssueTimelineExcludeIsSentAndBoundToTheCursor(t *testing.T) {
	f, core := historyCore(t, `[{"event":"commented","id":1}]`, true)
	got, err := invoke(t, core, issueTimelineList.ID, "repo", `{"number":42,"limit":1,"exclude":["commented","cross-referenced"]}`, false)
	if err != nil {
		t.Fatal(err)
	}
	query, _ := url.ParseQuery(lastRequest(t, f).query)
	if query.Get("exclude") != "commented,cross-referenced" || query.Get("per_page") != "1" {
		t.Errorf("query = %v, want the excluded names as one comma-separated list", query)
	}
	_, after, _ := strings.Cut(string(got), `"next_cursor":"`)
	cursor, _, _ := strings.Cut(after, `"`)
	for _, tt := range []struct {
		name, arguments string
		ok              bool
	}{
		{"same filter", `{"number":42,"exclude":["commented","cross-referenced"],"cursor":%q}`, true},
		{"no filter", `{"number":42,"cursor":%q}`, false},
		{"other filter", `{"number":42,"exclude":["commented"],"cursor":%q}`, false},
	} {
		_, err := invoke(t, core, issueTimelineList.ID, "repo", fmt.Sprintf(tt.arguments, cursor), false)
		if (err == nil) != tt.ok {
			t.Errorf("%s: err = %v, want ok %t", tt.name, err, tt.ok)
		}
	}
	for _, arguments := range []string{`{"number":42,"exclude":["Commented"]}`, `{"number":42,"exclude":["a,b"]}`,
		`{"number":42,"exclude":[""]}`, `{"number":42,"exclude":["x"],"unknown":1}`} {
		if _, err := invoke(t, core, issueTimelineList.ID, "repo", arguments, false); err == nil {
			t.Errorf("%s was accepted", arguments)
		}
	}
}
