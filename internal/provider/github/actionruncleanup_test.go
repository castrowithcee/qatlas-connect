package github

import (
	"encoding/json"
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

// cleanupAPI is the REST root of the fake instance as GitHub names it in the addresses of its answers.
const cleanupAPI = "https://ghe.example/api/v3/repos/"

type fakeCleanupRun struct {
	status, conclusion string
	attempts           int
	// repo is the repository the run's url names; octo-org/example when empty.
	repo string
}

type fakeCleanupArtifact struct {
	name string
	repo string
}

// fakeCleanup answers the run, attempt, approval, and artifact routes of octo-org/example through the failure
// hook of fakeGitHub. A run or artifact whose repo names another repository stands for an answer GitHub must
// never give for this repository.
type fakeCleanup struct {
	mu        sync.Mutex
	runs      map[int64]*fakeCleanupRun
	artifacts map[int64]*fakeCleanupArtifact
	// approvalsRepo is the repository the environments of the approvals name; octo-org/example when empty.
	approvalsRepo string
	// readStatus, when set, answers every read with this status and GitHub's own text.
	readStatus int
	// changeAnswer, when set, answers every delete and force-cancel in place of the fake.
	changeAnswer func(http.ResponseWriter)
}

func cleanupRunJSON(id int64, run *fakeCleanupRun, attempt int) string {
	repo := run.repo
	if repo == "" {
		repo = "octo-org/example"
	}
	conclusion := "null"
	if run.conclusion != "" {
		conclusion = strconv.Quote(run.conclusion)
	}
	previous := "null"
	if attempt > 1 {
		previous = strconv.Quote(fmt.Sprintf("%s%s/actions/runs/%d/attempts/%d", cleanupAPI, repo, id, attempt-1))
	}
	return fmt.Sprintf(`{"id":%[1]d,"name":"CI","node_id":"WFR_%[1]d","check_suite_id":42,"check_suite_node_id":"CS_42",`+
		`"head_branch":"main","head_sha":"009b8a3a9ccbb128af87f9b1c0f4c62e8a304f6d",`+
		`"path":".github/workflows/ci.yml@main","run_number":106,"run_attempt":%[2]d,`+
		`"referenced_workflows":[{"path":"octo-org/shared/.github/workflows/lint.yml@v1","sha":"abc","ref":"refs/tags/v1"}],`+
		`"event":"push","status":%[3]q,"conclusion":%[4]s,"workflow_id":5,`+
		`"url":"%[5]s%[6]s/actions/runs/%[1]d","html_url":"https://ghe.example/%[6]s/actions/runs/%[1]d",`+
		`"pull_requests":[{"id":9,"number":3,"url":"%[5]s%[6]s/pulls/3","head":{"ref":"feature","sha":"def",`+
		`"repo":{"id":1,"name":"example","url":"%[5]s%[6]s"}},"base":{"ref":"main","sha":"abc",`+
		`"repo":{"id":1,"name":"example","url":"%[5]s%[6]s"}}}],`+
		`"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:10:00Z",`+
		`"actor":{"login":"octocat","id":1,"type":"User"},"triggering_actor":{"login":"hubot","id":2,"type":"User"},`+
		`"run_started_at":"2026-01-01T00:01:00Z","jobs_url":"%[5]s%[6]s/actions/runs/%[1]d/jobs",`+
		`"logs_url":"%[5]s%[6]s/actions/runs/%[1]d/logs","check_suite_url":"%[5]s%[6]s/check-suites/42",`+
		`"artifacts_url":"%[5]s%[6]s/actions/runs/%[1]d/artifacts","cancel_url":"%[5]s%[6]s/actions/runs/%[1]d/cancel",`+
		`"rerun_url":"%[5]s%[6]s/actions/runs/%[1]d/rerun","previous_attempt_url":%[7]s,`+
		`"workflow_url":"%[5]s%[6]s/actions/workflows/5",`+
		`"head_commit":{"id":"009b8a3a9ccbb128af87f9b1c0f4c62e8a304f6d","tree_id":"t1","message":"Run %[8]s\u001b",`+
		`"timestamp":"2026-01-01T00:00:00Z","author":{"name":"Mona","email":"mona@example.com"},`+
		`"committer":{"name":"Mona","email":"mona@example.com"}},`+
		`"repository":{"id":1,"full_name":"%[6]s"},"head_repository":{"id":1,"full_name":"%[6]s"},`+
		`"head_repository_id":1,"display_title":"Fix %[8]s"}`,
		id, attempt, run.status, conclusion, cleanupAPI, repo, previous, bodyCanary)
}

func (c *fakeCleanup) approvalsJSON() string {
	repo := c.approvalsRepo
	if repo == "" {
		repo = "octo-org/example"
	}
	environment := func(id int, name, escaped string) string {
		return fmt.Sprintf(`{"id":%d,"node_id":"EN_%d","name":%q,"url":"%s%s/environments/%s",`+
			`"html_url":"https://ghe.example/%s/deployments/activity_log?environments_filter=%s",`+
			`"created_at":"2020-11-23T22:00:40Z","updated_at":"2020-11-23T22:00:41Z"}`,
			id, id, name, cleanupAPI, repo, escaped, repo, escaped)
	}
	user := `{"login":"octocat","id":1,"node_id":"U_1","type":"User","site_admin":false,` +
		`"html_url":"https://ghe.example/octocat","url":"https://ghe.example/api/v3/users/octocat"}`
	long := strings.Repeat("x", projectionTextLimit+5)
	return `[{"state":"approved","comment":"Ship it!\u0007","environments":[` + environment(161, "staging", "staging") + `,` +
		environment(162, "prod eu", "prod%20eu") + `],"user":` + user + `},` +
		`{"state":"rejected","comment":"` + long + `","environments":[` + environment(161, "staging", "staging") +
		`],"user":` + user + `}]`
}

func (c *fakeCleanup) route(w http.ResponseWriter, r *http.Request) bool {
	rest, ok := strings.CutPrefix(r.URL.Path, actionsPrefix)
	if !ok || !(strings.HasPrefix(rest, "actions/runs/") || strings.HasPrefix(rest, "actions/artifacts/")) {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	parts := strings.Split(strings.TrimPrefix(rest, "actions/"), "/")
	id, _ := strconv.ParseInt(parts[1], 10, 64)
	switch {
	case r.Method == http.MethodGet && c.readStatus != 0:
		refuse(w, c.readStatus)
	case r.Method != http.MethodGet && c.changeAnswer != nil:
		c.changeAnswer(w)
	case parts[0] == "artifacts" && len(parts) == 2:
		artifact, ok := c.artifacts[id]
		switch {
		case !ok:
			refuse(w, http.StatusNotFound)
		case r.Method == http.MethodDelete:
			delete(c.artifacts, id)
			w.WriteHeader(http.StatusNoContent)
		default:
			repo := artifact.repo
			if repo == "" {
				repo = "octo-org/example"
			}
			name, _ := json.Marshal(artifact.name)
			fmt.Fprintf(w, `{"id":%[1]d,"node_id":"MDg6QXJ0aWZhY3QxMQ==","name":%[2]s,"size_in_bytes":556,`+
				`"url":"%[3]s%[4]s/actions/artifacts/%[1]d","archive_download_url":"%[3]s%[4]s/actions/artifacts/%[1]d/zip",`+
				`"expired":false,"created_at":"2020-01-10T14:59:22Z","expires_at":"2020-03-21T14:59:22Z",`+
				`"updated_at":"2020-02-21T14:59:22Z","digest":"sha256:cfc3236bdad15b5898bca8408945c9e19e1917da8704adc20eaa618444290a8c",`+
				`"workflow_run":{"id":2332938,"repository_id":1296269,"head_repository_id":1296269,"head_branch":"main",`+
				`"head_sha":"328faa0536e6fef19753d9d91dc96a9931694ce3"}}`, id, name, cleanupAPI, repo)
		}
	case parts[0] == "runs":
		run, ok := c.runs[id]
		switch {
		case !ok:
			refuse(w, http.StatusNotFound)
		case r.Method == http.MethodDelete && len(parts) == 2:
			delete(c.runs, id)
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && len(parts) == 3 && parts[2] == "force-cancel":
			run.status, run.conclusion = "completed", "cancelled"
			w.WriteHeader(http.StatusAccepted)
			fmt.Fprint(w, `{}`)
		case r.Method == http.MethodGet && len(parts) == 2:
			fmt.Fprint(w, cleanupRunJSON(id, run, run.attempts))
		case r.Method == http.MethodGet && len(parts) == 4 && parts[2] == "attempts":
			attempt, _ := strconv.Atoi(parts[3])
			if attempt < 1 || attempt > run.attempts {
				refuse(w, http.StatusNotFound)
				return true
			}
			fmt.Fprint(w, cleanupRunJSON(id, run, attempt))
		case r.Method == http.MethodGet && len(parts) == 3 && parts[2] == "approvals":
			fmt.Fprint(w, c.approvalsJSON())
		default:
			refuse(w, http.StatusNotFound)
		}
	default:
		refuse(w, http.StatusNotFound)
	}
	return true
}

const (
	doneRun    = 6000
	activeRun  = 6001
	foreignRun = 6002
	ownedFile  = 11
	foreignArt = 12
)

func cleanupFixture() *fakeCleanup {
	return &fakeCleanup{
		runs: map[int64]*fakeCleanupRun{
			doneRun:    {status: "completed", conclusion: "failure", attempts: 2},
			activeRun:  {status: "in_progress", attempts: 1},
			foreignRun: {status: "completed", conclusion: "success", attempts: 1, repo: "octo-org/other"},
		},
		artifacts: map[int64]*fakeCleanupArtifact{
			ownedFile:  {name: "build\u0007-output"},
			foreignArt: {name: "secret", repo: "octo-org/other"},
		},
	}
}

var cleanupTools = []string{runsDelete.ID, runsForceCancel.ID, artifactsDelete.ID, runAttemptsGet.ID, runApprovalsList.ID}

// cleanupConfig binds a repository connection that lists the cleanup tools, one that selects by effects only,
// and one whose targets cover the organization's repositories but exclude octo-org/example.
func cleanupConfig(base string) *config.Config {
	cfg := coreConfig(base)
	effects := []config.Effect{config.EffectRead, config.EffectUpdate, config.EffectDelete}
	cfg.Connections["cleanup"] = config.Connection{Service: "gh", Credential: "gh-reader", Target: repoTarget,
		Effects: effects, Tools: cleanupTools}
	cfg.Connections["cleanup-unlisted"] = config.Connection{Service: "gh", Credential: "gh-reader", Target: repoTarget,
		Effects: effects}
	cfg.Connections["cleanup-excluding"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Targets: []string{"repos/octo-org/*"}, Exclude: []string{"repos/octo-org/example"}, Effects: effects,
		Tools: cleanupTools}
	return cfg
}

func serveCleanup(t *testing.T, reads *int) (*fakeGitHub, *fakeCleanup, *application.Core) {
	t.Helper()
	c := cleanupFixture()
	f := &fakeGitHub{failure: c.route}
	base := serve(t, f)
	red := &redact.Redactor{}
	return f, c, application.New(registry(t), cleanupConfig(base), resolver(red, reads), red)
}

// changesOf returns the deletes and posts sent since before.
func changesOf(f *fakeGitHub, before int) []recorded {
	return append(requestsOf(f, before, http.MethodDelete), requestsOf(f, before, http.MethodPost)...)
}

// checkSchemaMembers reports every member of an answer that the output schema of the tool lacks, at the top
// level and in the entries of a top-level list, and every listed member the answer lacks.
func checkSchemaMembers(t *testing.T, descriptor capability.Descriptor, result json.RawMessage, want ...string) {
	t.Helper()
	var schema struct {
		Properties map[string]struct {
			Items struct {
				Properties map[string]any `json:"properties"`
			} `json:"items"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(descriptor.OutputSchema, &schema); err != nil {
		t.Fatal(err)
	}
	var answer map[string]json.RawMessage
	if err := json.Unmarshal(result, &answer); err != nil {
		t.Fatalf("answer %s: %v", result, err)
	}
	for name, value := range answer {
		property, ok := schema.Properties[name]
		if !ok && name != "repository" {
			t.Errorf("%s answers %s outside its output schema", descriptor.ID, name)
		}
		var entries []map[string]any
		if json.Unmarshal(value, &entries) == nil && property.Items.Properties != nil {
			for _, entry := range entries {
				for member := range entry {
					if _, ok := property.Items.Properties[member]; !ok {
						t.Errorf("%s answers %s[].%s outside its output schema", descriptor.ID, name, member)
					}
				}
			}
		}
	}
	for _, name := range want {
		if _, ok := answer[name]; !ok {
			t.Errorf("%s lacks %s: %s", descriptor.ID, name, result)
		}
	}
}

// A run delete reads the run, checks that GitHub's own address names this repository, and sends exactly one
// delete; a run gone when the delete arrives counts as done; an unknown run, a run GitHub answers for another
// repository, and a run that has not completed are refused without a delete.
func TestRunDeleteIsBoundToTheRepository(t *testing.T) {
	f, c, core := serveCleanup(t, nil)
	before := len(f.recorded())
	result, err := invoke(t, core, runsDelete.ID, "cleanup", `{"run_id":6000}`, true)
	var deleted RunDeleted
	if err != nil || json.Unmarshal(result, &deleted) != nil || deleted != (RunDeleted{RunID: doneRun, Deleted: true}) {
		t.Fatalf("delete = %s, %v", result, err)
	}
	checkSchemaMembers(t, runsDelete, result, "run_id", "deleted", "already_deleted")
	changes := changesOf(f, before)
	if len(changes) != 1 || changes[0].method != http.MethodDelete || changes[0].path != actionsPrefix+"actions/runs/6000" {
		t.Errorf("changes = %+v, want exactly one delete of the run", changes)
	}
	if reads := requestsOf(f, before, http.MethodGet); len(reads) != 1 || reads[0].path != actionsPrefix+"actions/runs/6000" {
		t.Errorf("proof reads = %+v, want one read of the run", reads)
	}

	for _, tt := range []struct {
		name, arguments string
		class           provider.Class
		fragment        string
	}{
		{"an unknown run", `{"run_id":999}`, provider.ClassNotFound, "workflow run 999"},
		{"a run of another repository", `{"run_id":6002}`, provider.ClassInvalidResponse, "does not prove it belongs"},
		{"a running run", `{"run_id":6001}`, provider.ClassProviderError, "has not completed"},
	} {
		before = len(f.recorded())
		_, err := invoke(t, core, runsDelete.ID, "cleanup", tt.arguments, true)
		if classOf(err) != tt.class || !strings.Contains(err.Error(), tt.fragment) || strings.Contains(err.Error(), "octo-org/other") {
			t.Errorf("delete of %s = %v, want %s naming %q", tt.name, err, tt.class, tt.fragment)
		}
		if changes := changesOf(f, before); len(changes) != 0 {
			t.Errorf("delete of %s sent %+v", tt.name, changes)
		}
	}

	c.runs[doneRun] = &fakeCleanupRun{status: "completed", conclusion: "success", attempts: 1}
	c.changeAnswer = func(w http.ResponseWriter) { refuse(w, http.StatusNotFound) }
	result, err = invoke(t, core, runsDelete.ID, "cleanup", `{"run_id":6000}`, true)
	deleted = RunDeleted{}
	if err != nil || json.Unmarshal(result, &deleted) != nil || !deleted.AlreadyDeleted || !deleted.Deleted {
		t.Errorf("delete of a vanished run = %s, %v; want already_deleted", result, err)
	}
}

// Force-cancel reads the run first: a completed run is reported with its state without a request, a running
// run gets exactly one force-cancel, and a run of another repository is refused without one.
func TestRunForceCancelIsBoundToTheRepository(t *testing.T) {
	f, _, core := serveCleanup(t, nil)
	before := len(f.recorded())
	result, err := invoke(t, core, runsForceCancel.ID, "cleanup", `{"run_id":6001}`, true)
	var cancel RunCancel
	if err != nil || json.Unmarshal(result, &cancel) != nil || cancel != (RunCancel{RunID: activeRun, Accepted: true}) {
		t.Fatalf("force-cancel = %s, %v", result, err)
	}
	checkSchemaMembers(t, runsForceCancel, result, "run_id", "accepted")
	changes := changesOf(f, before)
	if len(changes) != 1 || changes[0].method != http.MethodPost ||
		changes[0].path != actionsPrefix+"actions/runs/6001/force-cancel" {
		t.Errorf("changes = %+v, want exactly one force-cancel", changes)
	}

	before = len(f.recorded())
	result, err = invoke(t, core, runsForceCancel.ID, "cleanup", `{"run_id":6000}`, true)
	cancel = RunCancel{}
	if err != nil || json.Unmarshal(result, &cancel) != nil ||
		cancel != (RunCancel{RunID: doneRun, Status: "completed", Conclusion: "failure"}) {
		t.Errorf("force-cancel of a completed run = %s, %v", result, err)
	}
	checkSchemaMembers(t, runsForceCancel, result, "run_id", "accepted", "status", "conclusion")
	if changes := changesOf(f, before); len(changes) != 0 {
		t.Errorf("force-cancel of a completed run sent %+v", changes)
	}

	for _, arguments := range []string{`{"run_id":6002}`, `{"run_id":999}`} {
		before = len(f.recorded())
		if _, err := invoke(t, core, runsForceCancel.ID, "cleanup", arguments, true); err == nil {
			t.Errorf("force-cancel %s succeeded", arguments)
		}
		if changes := changesOf(f, before); len(changes) != 0 {
			t.Errorf("force-cancel %s sent %+v", arguments, changes)
		}
	}
}

// An artifact delete reads the artifact, checks its identifier and that its address names this repository, and
// sends exactly one delete; an artifact gone when the delete arrives counts as done.
func TestArtifactDeleteIsBoundToTheRepository(t *testing.T) {
	f, c, core := serveCleanup(t, nil)
	before := len(f.recorded())
	result, err := invoke(t, core, artifactsDelete.ID, "cleanup", `{"artifact_id":11}`, true)
	var deleted ArtifactDeleted
	if err != nil || json.Unmarshal(result, &deleted) != nil {
		t.Fatalf("delete = %s, %v", result, err)
	}
	if !deleted.Deleted || deleted.AlreadyDeleted || deleted.ArtifactID != ownedFile || deleted.Artifact == nil ||
		deleted.Artifact.Name != "build -output" || deleted.Artifact.SizeBytes != 556 ||
		deleted.Artifact.WorkflowRun == nil || deleted.Artifact.WorkflowRun.ID != 2332938 {
		t.Errorf("delete = %s", result)
	}
	checkSchemaMembers(t, artifactsDelete, result, "artifact_id", "deleted", "already_deleted", "artifact")
	var artifact map[string]any
	var answer struct {
		Artifact json.RawMessage `json:"artifact"`
	}
	_ = json.Unmarshal(result, &answer)
	_ = json.Unmarshal(answer.Artifact, &artifact)
	for _, member := range []string{"id", "name", "size_bytes", "expired", "created_at", "expires_at", "digest",
		"node_id", "updated_at", "workflow_run", "links"} {
		if _, ok := artifact[member]; !ok {
			t.Errorf("artifact lacks %s: %s", member, result)
		}
	}
	if strings.Contains(string(result), "/zip") {
		t.Errorf("delete answers the download address: %s", result)
	}
	changes := changesOf(f, before)
	if len(changes) != 1 || changes[0].method != http.MethodDelete || changes[0].path != actionsPrefix+"actions/artifacts/11" {
		t.Errorf("changes = %+v, want exactly one delete of the artifact", changes)
	}

	for _, tt := range []struct {
		name, arguments string
		class           provider.Class
	}{
		{"an unknown artifact", `{"artifact_id":999}`, provider.ClassNotFound},
		{"an artifact of another repository", `{"artifact_id":12}`, provider.ClassInvalidResponse},
	} {
		before = len(f.recorded())
		_, err := invoke(t, core, artifactsDelete.ID, "cleanup", tt.arguments, true)
		if classOf(err) != tt.class || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "octo-org/other") {
			t.Errorf("delete of %s = %v, want %s", tt.name, err, tt.class)
		}
		if changes := changesOf(f, before); len(changes) != 0 {
			t.Errorf("delete of %s sent %+v", tt.name, changes)
		}
	}

	c.artifacts[ownedFile] = &fakeCleanupArtifact{name: "again"}
	c.changeAnswer = func(w http.ResponseWriter) { refuse(w, http.StatusNotFound) }
	result, err = invoke(t, core, artifactsDelete.ID, "cleanup", `{"artifact_id":11}`, true)
	deleted = ArtifactDeleted{}
	if err != nil || json.Unmarshal(result, &deleted) != nil || !deleted.AlreadyDeleted || !deleted.Deleted {
		t.Errorf("delete of a vanished artifact = %s, %v; want already_deleted", result, err)
	}
}

// An attempt read answers every member of the run projection for that attempt, and refuses an answer for
// another run, attempt, or repository.
func TestRunAttemptAnswersEveryMemberOfThisRepository(t *testing.T) {
	f, c, core := serveCleanup(t, nil)
	result, err := invoke(t, core, runAttemptsGet.ID, "cleanup", `{"run_id":6000,"attempt_number":2}`, false)
	if err != nil {
		t.Fatal(err)
	}
	checkSchemaMembers(t, runAttemptsGet, result, "id", "name", "title", "workflow_id", "run_number", "attempt",
		"event", "status", "conclusion", "branch", "head_sha", "actor", "created_at", "updated_at", "started_at", "url",
		"node_id", "check_suite_id", "check_suite_node_id", "path", "referenced_workflows", "actor_detail",
		"triggering_actor", "head_commit", "head_repository", "head_repository_id", "pull_requests", "links")
	var run WorkflowRun
	if err := json.Unmarshal(result, &run); err != nil || run.Attempt != 2 || run.ID != doneRun ||
		run.Links["previous_attempt_url"] == "" || strings.ContainsRune(run.HeadCommit.Message, '\x1b') {
		t.Errorf("attempt = %s, %v", result, err)
	}
	if strings.Contains(string(result), "/logs") {
		t.Errorf("attempt answers the log address: %s", result)
	}
	requests := f.recorded()
	if last := requests[len(requests)-1]; last.path != actionsPrefix+"actions/runs/6000/attempts/2" || len(requests) != 1 {
		t.Errorf("requests = %+v, want one read of the attempt", requests)
	}

	for _, tt := range []struct {
		name, arguments string
		class           provider.Class
	}{
		{"an unknown attempt", `{"run_id":6000,"attempt_number":3}`, provider.ClassNotFound},
		{"a run of another repository", `{"run_id":6002,"attempt_number":1}`, provider.ClassInvalidResponse},
	} {
		if _, err := invoke(t, core, runAttemptsGet.ID, "cleanup", tt.arguments, false); classOf(err) != tt.class {
			t.Errorf("%s = %v, want %s", tt.name, err, tt.class)
		}
	}
	// An answer for another attempt than the requested one is refused as well.
	c.runs[7000] = &fakeCleanupRun{status: "completed", attempts: 1}
	f.failure = func(w http.ResponseWriter, r *http.Request) bool {
		if strings.HasSuffix(r.URL.Path, "/actions/runs/7000/attempts/2") {
			fmt.Fprint(w, cleanupRunJSON(7000, c.runs[7000], 1))
			return true
		}
		return c.route(w, r)
	}
	if _, err := invoke(t, core, runAttemptsGet.ID, "cleanup", `{"run_id":7000,"attempt_number":2}`, false); classOf(err) !=
		provider.ClassInvalidResponse {
		t.Errorf("answer for another attempt = %v, want it refused", err)
	}
}

// The approvals read proves the run first, answers every member of each review with cleaned texts, and shows
// nothing when an environment names another repository or the run belongs to one.
func TestRunApprovalsAnswerEveryMemberOfThisRepository(t *testing.T) {
	f, c, core := serveCleanup(t, nil)
	result, err := invoke(t, core, runApprovalsList.ID, "cleanup", `{"run_id":6000}`, false)
	if err != nil {
		t.Fatal(err)
	}
	checkSchemaMembers(t, runApprovalsList, result, "run_id", "approvals")
	var list RunApprovals
	if err := json.Unmarshal(result, &list); err != nil || len(list.Approvals) != 2 || list.Truncated {
		t.Fatalf("approvals = %s, %v", result, err)
	}
	first, second := list.Approvals[0], list.Approvals[1]
	if first.State != "approved" || first.Comment != "Ship it! " || first.User == nil || valueOr(first.User.Login) != "octocat" ||
		len(first.Environments) != 2 || first.Environments[1].Name != "prod eu" || valueOr(first.Environments[1].ID) != 162 {
		t.Errorf("first review = %+v", first)
	}
	if second.State != "rejected" || !second.CommentTruncated || len([]rune(second.Comment)) != projectionTextLimit {
		t.Errorf("second review = %+v, want the comment cut with a marker", second)
	}
	var raw struct {
		Approvals []struct {
			Environments []map[string]any `json:"environments"`
			User         map[string]any   `json:"user"`
		} `json:"approvals"`
	}
	_ = json.Unmarshal(result, &raw)
	for _, member := range []string{"id", "node_id", "name", "url", "created_at", "updated_at", "links"} {
		if _, ok := raw.Approvals[0].Environments[0][member]; !ok {
			t.Errorf("environment lacks %s: %s", member, result)
		}
	}
	for _, member := range []string{"login", "id", "node_id", "type", "site_admin", "url", "links"} {
		if _, ok := raw.Approvals[0].User[member]; !ok {
			t.Errorf("user lacks %s: %s", member, result)
		}
	}
	requests := f.recorded()
	if len(requests) != 2 || requests[0].path != actionsPrefix+"actions/runs/6000" ||
		requests[1].path != actionsPrefix+"actions/runs/6000/approvals" {
		t.Errorf("requests = %+v, want the run, then its approvals", requests)
	}

	// A run of another repository ends before its approvals are read.
	before := len(f.recorded())
	if _, err := invoke(t, core, runApprovalsList.ID, "cleanup", `{"run_id":6002}`, false); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("approvals of a foreign run = %v, want it refused", err)
	}
	if requests := f.recorded()[before:]; len(requests) != 1 {
		t.Errorf("approvals of a foreign run sent %+v, want the proof read only", requests)
	}
	// An environment of another repository withholds the whole answer.
	c.approvalsRepo = "octo-org/other"
	_, err = invoke(t, core, runApprovalsList.ID, "cleanup", `{"run_id":6000}`, false)
	if classOf(err) != provider.ClassInvalidResponse || strings.Contains(err.Error(), "staging") {
		t.Errorf("approvals naming another repository = %v, want them refused", err)
	}
}

// Every refusal of the arguments, a repository outside the targets, and an excluded repository end before a
// credential is resolved and before GitHub is contacted.
func TestRunCleanupArgumentsAndTargetAreCheckedBeforeIO(t *testing.T) {
	reads := 0
	f, _, core := serveCleanup(t, &reads)
	cases := []struct{ name, id, connection, arguments string }{
		{"a zero artifact_id", artifactsDelete.ID, "cleanup", `{"artifact_id":0}`},
		{"a zero attempt_number", runAttemptsGet.ID, "cleanup", `{"run_id":1,"attempt_number":0}`},
		{"an attempt_number over the bound", runAttemptsGet.ID, "cleanup", `{"run_id":1,"attempt_number":1001}`},
		{"a negative run_id", runsDelete.ID, "cleanup", `{"run_id":-1}`},
	}
	for _, id := range cleanupTools {
		arguments := `"run_id":6000`
		switch id {
		case artifactsDelete.ID:
			arguments = `"artifact_id":11`
		case runAttemptsGet.ID:
			arguments += `,"attempt_number":1`
		}
		cases = append(cases,
			struct{ name, id, connection, arguments string }{"a repository outside the targets of " + id, id, "cleanup",
				`{"repository":"octo-org/other",` + arguments + `}`},
			struct{ name, id, connection, arguments string }{"another owner of " + id, id, "cleanup",
				`{"repository":"other-org/example",` + arguments + `}`},
			struct{ name, id, connection, arguments string }{"an excluded repository of " + id, id, "cleanup-excluding",
				`{"repository":"octo-org/example",` + arguments + `}`})
	}
	for _, tt := range cases {
		reads = 0
		before := len(f.recorded())
		_, err := invoke(t, core, tt.id, tt.connection, tt.arguments, true)
		if !isInvalidRequest(err) {
			t.Errorf("%s = %v, want an invalid request", tt.name, err)
		}
		if reads != 0 || len(f.recorded()) != before {
			t.Errorf("%s reached the credential or GitHub", tt.name)
		}
	}
}

// The deletes and force-cancel are offered only where a connection's tools list names them and need
// confirmation; the reads are offered by effects. Every refusal ends before a secret is read.
func TestRunCleanupChangesAreListedOnlyAndConfirmed(t *testing.T) {
	reads := 0
	f, _, core := serveCleanup(t, &reads)
	for _, id := range []string{runAttemptsGet.ID, runApprovalsList.ID} {
		if _, err := core.Describe(application.DescribeRequest{Operation: id, Connection: "cleanup-unlisted"}); err != nil {
			t.Errorf("describe %s without a tools list = %v, want it offered by effects", id, err)
		}
	}
	changes := map[string]string{runsDelete.ID: `{"run_id":6000}`, runsForceCancel.ID: `{"run_id":6001}`,
		artifactsDelete.ID: `{"artifact_id":11}`}
	var unsupported *capability.UnsupportedError
	var confirm *application.ConfirmationRequiredError
	for id, arguments := range changes {
		if _, err := core.Describe(application.DescribeRequest{Operation: id, Connection: "cleanup-unlisted"}); !errors.As(err, &unsupported) {
			t.Errorf("describe %s without a tools list = %v, want unsupported", id, err)
		}
		if _, err := invoke(t, core, id, "cleanup-unlisted", arguments, true); !errors.As(err, &unsupported) {
			t.Errorf("%s without a tools list = %v, want unsupported", id, err)
		}
		if _, err := invoke(t, core, id, "cleanup", arguments, false); !errors.As(err, &confirm) {
			t.Errorf("unconfirmed %s = %v, want confirmation-required", id, err)
		}
	}
	if reads != 0 || len(f.recorded()) != 0 {
		t.Errorf("refused requests read %d credentials and sent %d requests", reads, len(f.recorded()))
	}
}

// A refusal names what the request needs in Qatlas' own words, never GitHub's text; a conflict says that the
// object's state refused it; an unclear outcome says that the change may have been applied, after exactly one
// change request.
func TestRunCleanupFailuresAreNamedAndChangesSentOnce(t *testing.T) {
	f, c, core := serveCleanup(t, nil)
	changes := map[string]string{runsDelete.ID: `{"run_id":6000}`, runsForceCancel.ID: `{"run_id":6001}`,
		artifactsDelete.ID: `{"artifact_id":11}`}
	readsOnly := map[string]string{runAttemptsGet.ID: `{"run_id":6000,"attempt_number":1}`,
		runApprovalsList.ID: `{"run_id":6000}`}
	noText := func(name string, err error) {
		if err != nil && strings.Contains(err.Error(), cacheProviderCanary) {
			t.Errorf("%s quotes GitHub's text: %v", name, err)
		}
	}

	for _, tt := range []struct {
		status   int
		class    provider.Class
		fragment string
	}{
		{http.StatusForbidden, provider.ClassPermission, "Actions: read"},
		{http.StatusNotFound, provider.ClassNotFound, "does not hold"},
		{http.StatusBadGateway, provider.ClassProviderError, "HTTP 502"},
	} {
		c.readStatus = tt.status
		for id, arguments := range readsOnly {
			before := len(f.recorded())
			_, err := invoke(t, core, id, "cleanup", arguments, false)
			if classOf(err) != tt.class || !strings.Contains(err.Error(), tt.fragment) {
				t.Errorf("%s answered %d = %v, want %s naming %q", id, tt.status, err, tt.class, tt.fragment)
			}
			noText(id, err)
			if sent := len(f.recorded()) - before; sent != 1 {
				t.Errorf("%s answered %d sent %d requests, want one", id, tt.status, sent)
			}
		}
		// A refused proof read ends a change before it is sent.
		for id, arguments := range changes {
			before := len(f.recorded())
			_, err := invoke(t, core, id, "cleanup", arguments, true)
			if classOf(err) != tt.class || strings.Contains(err.Error(), uncertain) {
				t.Errorf("%s with a read answered %d = %v, want %s", id, tt.status, err, tt.class)
			}
			noText(id, err)
			if sent := changesOf(f, before); len(sent) != 0 {
				t.Errorf("%s with a refused read sent %+v", id, sent)
			}
		}
	}
	c.readStatus = 0

	for _, tt := range []struct {
		name      string
		answer    func(http.ResponseWriter)
		class     provider.Class
		fragment  string
		uncertain bool
	}{
		{"forbidden", func(w http.ResponseWriter) { refuse(w, http.StatusForbidden) }, provider.ClassPermission,
			"Actions: read and write", false},
		{"conflict", func(w http.ResponseWriter) { refuse(w, http.StatusConflict) }, provider.ClassProviderError,
			"in its current state", false},
		{"gateway timeout", func(w http.ResponseWriter) { refuse(w, http.StatusGatewayTimeout) }, provider.ClassTimeout,
			"did not answer in time", true},
		{"server error", func(w http.ResponseWriter) { refuse(w, http.StatusInternalServerError) }, provider.ClassProviderError,
			"", true},
		{"dropped connection", func(w http.ResponseWriter) {
			if connection, _, err := w.(http.Hijacker).Hijack(); err == nil {
				connection.Close()
			}
		}, "", "", true},
	} {
		c.changeAnswer = tt.answer
		for id, arguments := range changes {
			before := len(f.recorded())
			_, err := invoke(t, core, id, "cleanup", arguments, true)
			if err == nil || (tt.class != "" && classOf(err) != tt.class) || !strings.Contains(err.Error(), tt.fragment) ||
				strings.Contains(err.Error(), uncertain) != tt.uncertain {
				t.Errorf("%s %s = %v, want %s naming %q, uncertain %t", tt.name, id, err, tt.class, tt.fragment, tt.uncertain)
			}
			noText(tt.name, err)
			if tt.name == "conflict" && strings.Contains(err.Error(), conflictMessage) {
				t.Errorf("conflict of %s keeps the generic message: %v", id, err)
			}
			if sent := changesOf(f, before); len(sent) != 1 {
				t.Errorf("%s %s sent %d changes, want exactly one", tt.name, id, len(sent))
			}
		}
	}
}
