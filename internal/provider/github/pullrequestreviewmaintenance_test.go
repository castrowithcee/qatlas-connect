package github

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const maintenanceCanary = "PROVIDER-TEXT-CANARY"

// fakeMaintenanceReview and fakeMaintenanceComment are the reviews and line comments of the bound
// repository, each tied to the pull request it belongs to.
type fakeMaintenanceReview struct {
	pull        int
	state, body string
}

type fakeMaintenanceComment struct {
	pull int
	body string
}

// fakeReviewMaintenance answers the review and line comment routes of the bound repository. A review or
// comment of another repository is simply absent, the way GitHub answers not-found for it.
type fakeReviewMaintenance struct {
	mu       sync.Mutex
	f        *fakeGitHub
	reviews  map[int64]fakeMaintenanceReview
	comments map[int64]fakeMaintenanceComment
	// status, when not zero, is the answer of every change, with GitHub's text.
	status int
	// ignorePull makes the single review route answer a review whatever pull request the path names.
	ignorePull bool
}

func serveReviewMaintenance(t *testing.T) (*fakeReviewMaintenance, string) {
	t.Helper()
	m := &fakeReviewMaintenance{
		reviews: map[int64]fakeMaintenanceReview{
			80: {pull: 42, state: "CHANGES_REQUESTED", body: "original"},
			81: {pull: 42, state: "PENDING", body: "draft"},
			82: {pull: 43, state: "PENDING", body: "draft of another pull request"},
		},
		comments: map[int64]fakeMaintenanceComment{
			90: {pull: 42, body: "original line comment"},
			91: {pull: 43, body: "line comment of another pull request"},
		},
	}
	m.f = &fakeGitHub{failure: m.route}
	return m, serve(t, m.f)
}

func (m *fakeReviewMaintenance) route(w http.ResponseWriter, r *http.Request) bool {
	rest, ok := strings.CutPrefix(r.URL.Path, actionsPrefix+"pulls/")
	if !ok {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	parts := strings.Split(rest, "/")
	if parts[0] == "comments" && len(parts) == 2 {
		m.comment(w, r, parts[1])
		return true
	}
	if len(parts) >= 3 && parts[1] == "reviews" {
		m.review(w, r, parts[0], parts[2], len(parts) == 4 && parts[3] == "dismissals")
		return true
	}
	return false
}

func (m *fakeReviewMaintenance) refuse(w http.ResponseWriter) bool {
	if m.status == 0 {
		return false
	}
	w.WriteHeader(m.status)
	fmt.Fprintf(w, `{"message":%q}`, maintenanceCanary)
	return true
}

func notFoundJSON(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNotFound)
	fmt.Fprint(w, `{"message":"Not Found"}`)
}

func (m *fakeReviewMaintenance) review(w http.ResponseWriter, r *http.Request, pull, idText string, dismissal bool) {
	number, _ := strconv.Atoi(pull)
	id, _ := strconv.ParseInt(idText, 10, 64)
	entry, exists := m.reviews[id]
	if !exists || (entry.pull != number && !m.ignorePull) {
		notFoundJSON(w)
		return
	}
	if r.Method != http.MethodGet && m.refuse(w) {
		return
	}
	var body map[string]any
	if requests := m.f.recorded(); r.Method != http.MethodGet && len(requests) > 0 {
		body = requests[len(requests)-1].body
	}
	switch {
	case r.Method == http.MethodPut && dismissal:
		entry.state = "DISMISSED"
	case r.Method == http.MethodPut:
		if text, ok := body["body"].(string); ok {
			entry.body = text
		}
	case r.Method == http.MethodDelete:
		delete(m.reviews, id)
	}
	m.reviews[id] = entry
	if r.Method == http.MethodDelete {
		delete(m.reviews, id)
	}
	fmt.Fprintf(w, `{"id":%d,"user":{"login":"octocat"},"body":%q,"state":%q,"commit_id":%q,`+
		`"pull_request_url":"https://api.github.com/repos/octo-org/example/pulls/%d"}`,
		id, entry.body, entry.state, strings.Repeat("a", 40), entry.pull)
}

func (m *fakeReviewMaintenance) comment(w http.ResponseWriter, r *http.Request, idText string) {
	id, _ := strconv.ParseInt(idText, 10, 64)
	entry, exists := m.comments[id]
	if !exists {
		notFoundJSON(w)
		return
	}
	if r.Method != http.MethodGet && m.refuse(w) {
		return
	}
	if r.Method == http.MethodDelete {
		delete(m.comments, id)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if requests := m.f.recorded(); r.Method == http.MethodPatch && len(requests) > 0 {
		if text, ok := requests[len(requests)-1].body["body"].(string); ok {
			entry.body = text
			m.comments[id] = entry
		}
	}
	fmt.Fprintf(w, `{"id":%d,"path":"a.go","line":3,"side":"RIGHT","commit_id":%q,"user":{"login":"octocat"},`+
		`"body":%q,"created_at":"2026-01-03T00:00:00Z","updated_at":"2026-01-03T00:00:00Z",`+
		`"pull_request_url":"https://api.github.com/repos/octo-org/example/pulls/%d"}`,
		id, strings.Repeat("a", 40), entry.body, entry.pull)
}

func reviewMaintenanceConfig(base string) *config.Config {
	cfg := coreConfig(base)
	all := []config.Effect{config.EffectRead, config.EffectCreate, config.EffectUpdate, config.EffectDelete}
	cfg.Connections["repo"] = config.Connection{Service: "gh", Credential: "gh-reader", Target: repoTarget, Effects: all}
	cfg.Connections["repo-listed"] = config.Connection{Service: "gh", Credential: "gh-reader", Target: repoTarget,
		Effects: all, Tools: []string{pullRequestReviewsDismiss.ID, pullRequestReviewCommentsDelete.ID,
			pullRequestReviewsDeletePending.ID, pullRequestReviewsUpdate.ID, pullRequestReviewCommentsUpdate.ID}}
	cfg.Connections["ex"] = config.Connection{Service: "gh", Credential: "gh-reader", Effects: all,
		Exclude: []string{repoTarget}}
	return cfg
}

func newReviewMaintenanceCore(t *testing.T, base string, reads *int) *application.Core {
	t.Helper()
	red := &redact.Redactor{}
	return application.New(registry(t), reviewMaintenanceConfig(base), resolver(red, reads), red)
}

func changesSent(m *fakeReviewMaintenance, before int) int {
	_, _, rest := split(m.f.recorded()[before:])
	return rest[http.MethodPut] + rest[http.MethodPatch] + rest[http.MethodDelete] + rest[http.MethodPost]
}

// Every tool changes exactly one object with one request after one proof read, and answers in its own shape.
func TestReviewMaintenanceSucceeds(t *testing.T) {
	m, base := serveReviewMaintenance(t)
	core := newReviewMaintenanceCore(t, base, nil)

	before := len(m.f.recorded())
	got, err := invoke(t, core, pullRequestReviewsDismiss.ID, "repo-listed",
		`{"number":42,"review_id":80,"message":"Addressed."}`, true)
	if err != nil || !strings.Contains(string(got), `"state":"DISMISSED"`) {
		t.Fatalf("dismiss = %s, %v", got, err)
	}
	requests := m.f.recorded()[before:]
	if len(requests) != 2 || requests[0].method != http.MethodGet || requests[1].method != http.MethodPut ||
		!strings.HasSuffix(requests[1].path, "/pulls/42/reviews/80/dismissals") ||
		requests[1].body["message"] != "Addressed." {
		t.Errorf("dismiss requests = %+v, want one read and one PUT with the message", requests)
	}

	before = len(m.f.recorded())
	got, err = invoke(t, core, pullRequestReviewsUpdate.ID, "repo", `{"number":42,"review_id":81,"body":"new draft"}`, true)
	if err != nil || !strings.Contains(string(got), `"body":"new draft"`) {
		t.Fatalf("update = %s, %v", got, err)
	}
	requests = m.f.recorded()[before:]
	if len(requests) != 2 || requests[1].method != http.MethodPut || !strings.HasSuffix(requests[1].path, "/pulls/42/reviews/81") ||
		requests[1].body["body"] != "new draft" {
		t.Errorf("update requests = %+v", requests)
	}

	before = len(m.f.recorded())
	got, err = invoke(t, core, pullRequestReviewsDeletePending.ID, "repo", `{"number":42,"review_id":81}`, true)
	if err != nil || !strings.Contains(string(got), `"deleted":true`) || !strings.Contains(string(got), `"review_id":81`) {
		t.Fatalf("delete-pending = %s, %v", got, err)
	}
	requests = m.f.recorded()[before:]
	if len(requests) != 2 || requests[1].method != http.MethodDelete || !strings.HasSuffix(requests[1].path, "/pulls/42/reviews/81") {
		t.Errorf("delete-pending requests = %+v", requests)
	}

	before = len(m.f.recorded())
	got, err = invoke(t, core, pullRequestReviewCommentsUpdate.ID, "repo", `{"comment_id":90,"number":42,"body":"fixed"}`, true)
	if err != nil || !strings.Contains(string(got), `"body":"fixed"`) {
		t.Fatalf("line comment update = %s, %v", got, err)
	}
	requests = m.f.recorded()[before:]
	if len(requests) != 2 || requests[1].method != http.MethodPatch || !strings.HasSuffix(requests[1].path, "/pulls/comments/90") ||
		requests[1].body["body"] != "fixed" {
		t.Errorf("line comment update requests = %+v", requests)
	}

	// Without a number the comment is bound to the repository alone.
	got, err = invoke(t, core, pullRequestReviewCommentsUpdate.ID, "repo", `{"comment_id":91,"body":"also fixed"}`, true)
	if err != nil || !strings.Contains(string(got), `"body":"also fixed"`) {
		t.Fatalf("line comment update without number = %s, %v", got, err)
	}

	before = len(m.f.recorded())
	got, err = invoke(t, core, pullRequestReviewCommentsDelete.ID, "repo-listed", `{"comment_id":90,"number":42}`, true)
	if err != nil || !strings.Contains(string(got), `"deleted":true`) || !strings.Contains(string(got), `"comment_id":90`) {
		t.Fatalf("line comment delete = %s, %v", got, err)
	}
	requests = m.f.recorded()[before:]
	if len(requests) != 2 || requests[1].method != http.MethodDelete || !strings.HasSuffix(requests[1].path, "/pulls/comments/90") {
		t.Errorf("line comment delete requests = %+v", requests)
	}
}

// A review or line comment of another pull request or repository, and a delete of a review that is not
// pending, are refused without any change; an unconfirmed call reaches nothing.
func TestReviewMaintenanceRefusesForeignObjectsWithoutAChange(t *testing.T) {
	m, base := serveReviewMaintenance(t)
	core := newReviewMaintenanceCore(t, base, nil)
	for _, tt := range []struct {
		name, id, connection, arguments string
		class                           provider.Class
	}{
		{"dismiss of a review of another pull request", pullRequestReviewsDismiss.ID, "repo-listed",
			`{"number":42,"review_id":82,"message":"x"}`, provider.ClassNotFound},
		{"dismiss of a review of another repository", pullRequestReviewsDismiss.ID, "repo-listed",
			`{"number":42,"review_id":999,"message":"x"}`, provider.ClassNotFound},
		{"update of a review of another pull request", pullRequestReviewsUpdate.ID, "repo",
			`{"number":42,"review_id":82,"body":"x"}`, provider.ClassNotFound},
		{"delete-pending of a review of another pull request", pullRequestReviewsDeletePending.ID, "repo",
			`{"number":42,"review_id":82}`, provider.ClassNotFound},
		{"line comment update of another pull request", pullRequestReviewCommentsUpdate.ID, "repo",
			`{"comment_id":91,"number":42,"body":"x"}`, provider.ClassNotFound},
		{"line comment update of another repository", pullRequestReviewCommentsUpdate.ID, "repo",
			`{"comment_id":999,"body":"x"}`, provider.ClassNotFound},
		{"line comment delete of another pull request", pullRequestReviewCommentsDelete.ID, "repo-listed",
			`{"comment_id":91,"number":42}`, provider.ClassNotFound},
		{"line comment delete of another repository", pullRequestReviewCommentsDelete.ID, "repo-listed",
			`{"comment_id":999}`, provider.ClassNotFound},
	} {
		before := len(m.f.recorded())
		_, err := invoke(t, core, tt.id, tt.connection, tt.arguments, true)
		if classOf(err) != tt.class {
			t.Errorf("%s = %v, want %s", tt.name, err, tt.class)
		}
		if changesSent(m, before) != 0 {
			t.Errorf("%s sent a change", tt.name)
		}
	}

	// A route that ignores the pull request in its path is still checked against the answer.
	m.ignorePull = true
	before := len(m.f.recorded())
	if _, err := invoke(t, core, pullRequestReviewsUpdate.ID, "repo", `{"number":42,"review_id":82,"body":"x"}`, true); classOf(err) != provider.ClassNotFound {
		t.Errorf("update with an ignoring route = %v, want not-found", err)
	}
	if changesSent(m, before) != 0 {
		t.Error("update with an ignoring route sent a change")
	}
	m.ignorePull = false

	// A review that is not pending is not deleted, whatever its state.
	before = len(m.f.recorded())
	if _, err := invoke(t, core, pullRequestReviewsDeletePending.ID, "repo", `{"number":42,"review_id":80}`, true); !isInvalidRequest(err) ||
		!strings.Contains(err.Error(), "not pending") {
		t.Errorf("delete-pending of a submitted review = %v, want an invalid request naming it", err)
	}
	if changesSent(m, before) != 0 {
		t.Error("delete-pending of a submitted review sent a change")
	}
	if _, kept := m.reviews[80]; !kept {
		t.Error("the submitted review was deleted")
	}

	before = len(m.f.recorded())
	if _, err := invoke(t, core, pullRequestReviewsUpdate.ID, "repo", `{"number":42,"review_id":81,"body":"x"}`, false); err == nil {
		t.Error("an unconfirmed update succeeded")
	}
	if len(m.f.recorded()) != before {
		t.Error("an unconfirmed update reached GitHub")
	}
}

// Refusals and unclear outcomes are named in Qatlas' words, never with GitHub's text, and a change is sent
// exactly once.
func TestReviewMaintenanceFailuresAreNamedAndNeverRepeated(t *testing.T) {
	m, base := serveReviewMaintenance(t)
	core := newReviewMaintenanceCore(t, base, nil)
	calls := []struct{ id, arguments string }{
		{pullRequestReviewsDismiss.ID, `{"number":42,"review_id":80,"message":"x"}`},
		{pullRequestReviewsUpdate.ID, `{"number":42,"review_id":81,"body":"x"}`},
		{pullRequestReviewsDeletePending.ID, `{"number":42,"review_id":81}`},
		{pullRequestReviewCommentsUpdate.ID, `{"comment_id":90,"body":"x"}`},
		{pullRequestReviewCommentsDelete.ID, `{"comment_id":90}`},
	}
	for _, tt := range []struct {
		status    int
		class     provider.Class
		fragment  string
		uncertain bool
	}{
		{http.StatusForbidden, provider.ClassPermission, "Pull requests: read and write", false},
		{http.StatusNotFound, provider.ClassNotFound, "does not hold", false},
		{http.StatusUnprocessableEntity, provider.ClassProviderError, "as invalid", false},
		{http.StatusGatewayTimeout, provider.ClassTimeout, "did not answer in time", true},
		{http.StatusInternalServerError, provider.ClassProviderError, "HTTP 500", true},
	} {
		m.status = tt.status
		for _, call := range calls {
			before := len(m.f.recorded())
			_, err := invoke(t, core, call.id, "repo-listed", call.arguments, true)
			if err == nil || classOf(err) != tt.class || !strings.Contains(err.Error(), tt.fragment) ||
				strings.Contains(err.Error(), uncertain) != tt.uncertain || strings.Contains(err.Error(), maintenanceCanary) {
				t.Errorf("%s answered %d = %v, want %s naming %q, uncertain=%t", call.id, tt.status, err, tt.class,
					tt.fragment, tt.uncertain)
			}
			if got := changesSent(m, before); got != 1 {
				t.Errorf("%s answered %d sent %d changes, want exactly one", call.id, tt.status, got)
			}
		}
	}
}

// Arguments are checked before a credential is resolved, and so are the target and the exclusion.
func TestReviewMaintenanceIsCheckedBeforeSecretAccess(t *testing.T) {
	m, base := serveReviewMaintenance(t)
	reads := 0
	core := newReviewMaintenanceCore(t, base, &reads)
	long := strings.Repeat("x", maxBodyLength+1)
	for _, tt := range []struct{ name, id, connection, arguments string }{
		{"dismiss without message", pullRequestReviewsDismiss.ID, "repo-listed", `{"number":42,"review_id":80}`},
		{"dismiss with a blank message", pullRequestReviewsDismiss.ID, "repo-listed", `{"number":42,"review_id":80,"message":"  "}`},
		{"dismiss with a long message", pullRequestReviewsDismiss.ID, "repo-listed",
			`{"number":42,"review_id":80,"message":"` + long + `"}`},
		{"dismiss without review_id", pullRequestReviewsDismiss.ID, "repo-listed", `{"number":42,"message":"x"}`},
		{"update with a blank body", pullRequestReviewsUpdate.ID, "repo", `{"number":42,"review_id":81,"body":" "}`},
		{"update with a zero review_id", pullRequestReviewsUpdate.ID, "repo", `{"number":42,"review_id":0,"body":"x"}`},
		{"update without number", pullRequestReviewsUpdate.ID, "repo", `{"review_id":81,"body":"x"}`},
		{"delete-pending without number", pullRequestReviewsDeletePending.ID, "repo", `{"review_id":81}`},
		{"line comment update with a blank body", pullRequestReviewCommentsUpdate.ID, "repo", `{"comment_id":90,"body":""}`},
		{"line comment update with a zero number", pullRequestReviewCommentsUpdate.ID, "repo", `{"comment_id":90,"number":0,"body":"x"}`},
		{"line comment delete without comment_id", pullRequestReviewCommentsDelete.ID, "repo-listed", `{}`},
		{"foreign repository", pullRequestReviewsUpdate.ID, "repo",
			`{"repository":"octo-org/other","number":42,"review_id":81,"body":"x"}`},
		{"excluded repository", pullRequestReviewsDeletePending.ID, "ex",
			`{"repository":"octo-org/example","number":42,"review_id":81}`},
		{"excluded repository line comment", pullRequestReviewCommentsUpdate.ID, "ex",
			`{"repository":"octo-org/example","comment_id":90,"body":"x"}`},
	} {
		if _, err := invoke(t, core, tt.id, tt.connection, tt.arguments, true); !isInvalidRequest(err) {
			t.Errorf("%s = %v, want an invalid request", tt.name, err)
		}
	}
	if reads != 0 || len(m.f.recorded()) != 0 {
		t.Errorf("refused requests read %d credentials and sent %d requests", reads, len(m.f.recorded()))
	}
}

// Dismissing a review and deleting a line comment are offered only where a connection's tools list names
// them; the other three tools are offered by effect alone.
func TestReviewMaintenanceListedOnlyToolsNeedAToolsEntry(t *testing.T) {
	m, base := serveReviewMaintenance(t)
	core := newReviewMaintenanceCore(t, base, nil)
	var unsupported *capability.UnsupportedError
	for _, tt := range []struct{ id, arguments string }{
		{pullRequestReviewsDismiss.ID, `{"number":42,"review_id":80,"message":"x"}`},
		{pullRequestReviewCommentsDelete.ID, `{"comment_id":90}`},
	} {
		if _, err := invoke(t, core, tt.id, "repo", tt.arguments, true); !errors.As(err, &unsupported) {
			t.Errorf("%s without a tools entry = %v, want unsupported-capability", tt.id, err)
		}
	}
	if len(m.f.recorded()) != 0 {
		t.Error("an unoffered tool reached GitHub")
	}
	for _, descriptor := range []capability.Descriptor{pullRequestReviewsDismiss, pullRequestReviewCommentsDelete} {
		if !descriptor.RequiresToolAllowList {
			t.Errorf("%s is not listed-only", descriptor.ID)
		}
	}
	for _, descriptor := range []capability.Descriptor{pullRequestReviewsUpdate, pullRequestReviewsDeletePending,
		pullRequestReviewCommentsUpdate} {
		if descriptor.RequiresToolAllowList {
			t.Errorf("%s is listed-only", descriptor.ID)
		}
	}
	if _, err := invoke(t, core, pullRequestReviewsDeletePending.ID, "repo", `{"number":42,"review_id":81}`, true); err != nil {
		t.Errorf("delete-pending on a connection with the effect delete = %v", err)
	}
}

// The profile ticks the two text updates and none of the dismissing or deleting tools.
func TestReviewMaintenanceProfile(t *testing.T) {
	metadata, _ := registry(t).ProviderMetadata(Provider)
	for _, profile := range metadata.Profiles {
		for _, id := range profile.Tools {
			switch id {
			case pullRequestReviewsDismiss.ID, pullRequestReviewsDeletePending.ID, pullRequestReviewCommentsDelete.ID:
				t.Errorf("profile %s selects %s", profile.ID, id)
			}
		}
	}
}

// The not-found a path below pulls names the review or the line comment, not the pull request.
func TestReviewsSubjectNamesReviewsAndLineComments(t *testing.T) {
	for path, want := range map[string]string{
		"pulls/42/reviews/80":            "review 80",
		"pulls/42/reviews/80/dismissals": "review 80",
		"pulls/comments/90":              "review comment 90",
		"pulls/comments/90/reactions":    "",
		"pulls/comments/abc":             "",
		"pulls/42/comments/90/replies":   "review comment 90",
		"pulls/42/reviews/0/dismissals":  "",
		"pulls/42":                       "",
	} {
		if got := reviewsSubject(path); got != want {
			t.Errorf("reviewsSubject(%q) = %q, want %q", path, got, want)
		}
	}
}
