package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	forkRunConclusion = 7000 // completed, conclusion action_required
	forkRunStatus     = 7001 // status action_required
	waitingRun        = 7002 // waits for deployment reviewers
	finishedRun       = 7003 // completed, success
	foreignGateRun    = 7004 // action_required, but the run of another repository
)

var gateIDs = []string{runsApprove.ID, pendingDeploymentsReview.ID}

// fakeGates answers the approve and pending_deployments routes of octo-org/example and hands every other run
// route to a fakeCleanup, which serves the proof read of a run.
type fakeGates struct {
	cleanup *fakeCleanup
	// pendingRepo is the repository the pending environments name; octo-org/example when empty.
	pendingRepo string
	// changeAnswer, when set, answers every POST in place of the fake.
	changeAnswer func(http.ResponseWriter, *http.Request)
}

func (g *fakeGates) pendingJSON() string {
	repo := g.pendingRepo
	if repo == "" {
		repo = "octo-org/example"
	}
	entry := func(id int, name, escaped string) string {
		return fmt.Sprintf(`{"environment":{"id":%d,"node_id":"EN_%d","name":%q,"url":"%s%s/environments/%s",`+
			`"html_url":"https://ghe.example/%s/deployments"},"wait_timer":0,"wait_timer_started_at":null,`+
			`"current_user_can_approve":true,"reviewers":[]}`, id, id, name, cleanupAPI, repo, escaped, repo)
	}
	return `[` + entry(161, "staging", "staging") + `,` + entry(162, "prod eu", "prod%20eu") + `]`
}

func (g *fakeGates) route(w http.ResponseWriter, r *http.Request) bool {
	rest, ok := strings.CutPrefix(r.URL.Path, actionsPrefix+"actions/runs/")
	if !ok {
		return false
	}
	parts := strings.Split(rest, "/")
	if r.Method == http.MethodPost && g.changeAnswer != nil {
		g.changeAnswer(w, r)
		return true
	}
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.Method == http.MethodPost && len(parts) == 2 && parts[1] == "approve":
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{}`)
	case r.Method == http.MethodGet && len(parts) == 2 && parts[1] == "pending_deployments":
		fmt.Fprint(w, g.pendingJSON())
	case r.Method == http.MethodPost && len(parts) == 2 && parts[1] == "pending_deployments":
		fmt.Fprintf(w, `[{"id":901,"environment":"staging","sha":%q},{"id":902,"environment":"prod eu"}]`, providerCanary)
	default:
		return g.cleanup.route(w, r)
	}
	return true
}

func gateConfig(base string) *config.Config {
	cfg := coreConfig(base)
	effects := []config.Effect{config.EffectRead, config.EffectUpdate}
	cfg.Connections["gates"] = config.Connection{Service: "gh", Credential: "gh-reader", Target: repoTarget,
		Effects: effects, Tools: gateIDs}
	cfg.Connections["gates-unlisted"] = config.Connection{Service: "gh", Credential: "gh-reader", Target: repoTarget,
		Effects: effects}
	cfg.Connections["gates-excluding"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Targets: []string{"repos/octo-org/*"}, Exclude: []string{"repos/octo-org/example"}, Effects: effects,
		Tools: gateIDs}
	return cfg
}

func serveGates(t *testing.T, reads *int) (*fakeGitHub, *fakeGates, *application.Core, string) {
	t.Helper()
	cleanup := cleanupFixture()
	cleanup.runs[forkRunConclusion] = &fakeCleanupRun{status: "completed", conclusion: "action_required", attempts: 1}
	cleanup.runs[forkRunStatus] = &fakeCleanupRun{status: "action_required", attempts: 1}
	cleanup.runs[waitingRun] = &fakeCleanupRun{status: "waiting", attempts: 1}
	cleanup.runs[finishedRun] = &fakeCleanupRun{status: "completed", conclusion: "success", attempts: 1}
	cleanup.runs[foreignGateRun] = &fakeCleanupRun{status: "completed", conclusion: "action_required", attempts: 1,
		repo: "octo-org/other"}
	g := &fakeGates{cleanup: cleanup}
	f := &fakeGitHub{failure: g.route}
	base := serve(t, f)
	red := &redact.Redactor{}
	return f, g, application.New(registry(t), gateConfig(base), resolver(red, reads), red), base
}

func postsOf(f *fakeGitHub, before int) []recorded { return requestsOf(f, before, http.MethodPost) }

const gateReviewArguments = `{"run_id":7002,"state":"approved","environment_ids":[161,162],"comment":"Ship it"}`

// An approval reads the run once, sends exactly one POST for a run that waits for approval, in either form
// GitHub reports it, and refuses every other run, among them the run of another repository, without a POST.
func TestRunApproveSendsOnePostForARunWaitingForApproval(t *testing.T) {
	f, _, core, _ := serveGates(t, nil)
	for _, id := range []int{forkRunConclusion, forkRunStatus} {
		before := len(f.recorded())
		result, err := invoke(t, core, runsApprove.ID, "gates", fmt.Sprintf(`{"run_id":%d}`, id), true)
		var approved RunApproved
		if err != nil || json.Unmarshal(result, &approved) != nil || approved != (RunApproved{RunID: int64(id), Approved: true}) {
			t.Fatalf("approve %d = %s, %v", id, result, err)
		}
		checkSchemaMembers(t, runsApprove, result, "run_id", "approved")
		sent := f.recorded()[before:]
		if len(sent) != 2 || sent[0].method != http.MethodGet || sent[0].path != fmt.Sprintf("%sactions/runs/%d", actionsPrefix, id) ||
			sent[1].method != http.MethodPost || sent[1].path != fmt.Sprintf("%sactions/runs/%d/approve", actionsPrefix, id) {
			t.Errorf("requests = %+v, want one proof read and one approve", sent)
		}
		if len(sent[1].body) != 0 {
			t.Errorf("approve body = %v, want none", sent[1].body)
		}
	}
	for _, tt := range []struct {
		name, arguments string
		class           provider.Class
		fragment        string
	}{
		{"a run of another repository", `{"run_id":7004}`, provider.ClassInvalidResponse, "does not prove it belongs"},
		{"a run that waits for deployment reviewers", `{"run_id":7002}`, provider.ClassProviderError, "does not wait for approval"},
		{"a finished run", `{"run_id":7003}`, provider.ClassProviderError, "does not wait for approval"},
		{"a running run", `{"run_id":6001}`, provider.ClassProviderError, "does not wait for approval"},
		{"an unknown run", `{"run_id":999}`, provider.ClassNotFound, "workflow run 999"},
	} {
		before := len(f.recorded())
		_, err := invoke(t, core, runsApprove.ID, "gates", tt.arguments, true)
		if err == nil || classOf(err) != tt.class || !strings.Contains(err.Error(), tt.fragment) ||
			strings.Contains(err.Error(), "octo-org/other") {
			t.Errorf("approve of %s = %v, want %s naming %q", tt.name, err, tt.class, tt.fragment)
		}
		if posts := postsOf(f, before); len(posts) != 0 {
			t.Errorf("approve of %s sent %+v", tt.name, posts)
		}
	}
}

// A review reads the run and its pending deployments, then sends exactly one POST with the three members
// given; every request outside the contract ends before a POST.
func TestPendingDeploymentsReviewChecksTheRunAndItsEnvironmentsFirst(t *testing.T) {
	f, g, core, _ := serveGates(t, nil)
	before := len(f.recorded())
	result, err := invoke(t, core, pendingDeploymentsReview.ID, "gates", gateReviewArguments, true)
	if err != nil {
		t.Fatal(err)
	}
	var reviewed DeploymentReviewed
	if json.Unmarshal(result, &reviewed) != nil || !reflect.DeepEqual(reviewed, DeploymentReviewed{RunID: waitingRun,
		State: "approved", EnvironmentIDs: []int64{161, 162}, DeploymentIDs: []int64{901, 902}}) {
		t.Errorf("review = %s", result)
	}
	if strings.Contains(string(result), providerCanary) {
		t.Errorf("answer carries provider text: %s", result)
	}
	checkSchemaMembers(t, pendingDeploymentsReview, result, "run_id", "state", "environment_ids", "deployment_ids")
	sent := f.recorded()[before:]
	if len(sent) != 3 || sent[0].method != http.MethodGet || sent[0].path != actionsPrefix+"actions/runs/7002" ||
		sent[1].method != http.MethodGet || sent[1].path != actionsPrefix+"actions/runs/7002/pending_deployments" ||
		sent[2].method != http.MethodPost || sent[2].path != actionsPrefix+"actions/runs/7002/pending_deployments" {
		t.Fatalf("requests = %+v, want two proof reads and one POST", sent)
	}
	body := sent[2].body
	if len(body) != 3 || body["state"] != "approved" || body["comment"] != "Ship it" ||
		!reflect.DeepEqual(body["environment_ids"], []any{161.0, 162.0}) {
		t.Errorf("body = %v", body)
	}

	rejected, err := invoke(t, core, pendingDeploymentsReview.ID, "gates",
		`{"run_id":7002,"state":"rejected","environment_ids":[162],"comment":"Not now"}`, true)
	if err != nil || !strings.Contains(string(rejected), `"state":"rejected"`) {
		t.Errorf("rejection = %s, %v", rejected, err)
	}

	for _, tt := range []struct {
		name, arguments string
		class           provider.Class
		fragment        string
	}{
		{"a run of another repository", `{"run_id":7004,"state":"approved","environment_ids":[161],"comment":"x"}`,
			provider.ClassInvalidResponse, "does not prove it belongs"},
		{"an unknown run", `{"run_id":999,"state":"approved","environment_ids":[161],"comment":"x"}`,
			provider.ClassNotFound, "workflow run 999"},
		{"an environment that is not pending", `{"run_id":7002,"state":"approved","environment_ids":[999],"comment":"x"}`,
			provider.ClassProviderError, "no pending deployment"},
		{"one foreign environment among pending ones", `{"run_id":7002,"state":"approved","environment_ids":[161,999],"comment":"x"}`,
			provider.ClassProviderError, "no pending deployment"},
	} {
		before := len(f.recorded())
		_, err := invoke(t, core, pendingDeploymentsReview.ID, "gates", tt.arguments, true)
		if err == nil || classOf(err) != tt.class || !strings.Contains(err.Error(), tt.fragment) {
			t.Errorf("review of %s = %v, want %s naming %q", tt.name, err, tt.class, tt.fragment)
		}
		if posts := postsOf(f, before); len(posts) != 0 {
			t.Errorf("review of %s sent %+v", tt.name, posts)
		}
	}

	g.pendingRepo = "octo-org/other"
	before = len(f.recorded())
	_, err = invoke(t, core, pendingDeploymentsReview.ID, "gates", gateReviewArguments, true)
	if classOf(err) != provider.ClassInvalidResponse || len(postsOf(f, before)) != 0 {
		t.Errorf("review with environments of another repository = %v, want a refused answer and no POST", err)
	}
	g.pendingRepo = ""

	for _, arguments := range []string{
		`{"run_id":7002,"state":"dismissed","environment_ids":[161],"comment":"x"}`,
		`{"run_id":7002,"state":"APPROVED","environment_ids":[161],"comment":"x"}`,
		`{"run_id":7002,"environment_ids":[161],"comment":"x"}`,
		`{"run_id":7002,"state":"approved","environment_ids":[],"comment":"x"}`,
		`{"run_id":7002,"state":"approved","comment":"x"}`,
		`{"run_id":7002,"state":"approved","environment_ids":[161,161],"comment":"x"}`,
		`{"run_id":7002,"state":"approved","environment_ids":[0],"comment":"x"}`,
		`{"run_id":7002,"state":"approved","environment_ids":["161"],"comment":"x"}`,
		`{"run_id":7002,"state":"approved","environment_ids":[` + strings.TrimSuffix(strings.Repeat("1,", 21), ",") + `],"comment":"x"}`,
		`{"run_id":7002,"state":"approved","environment_ids":[161]}`,
		`{"run_id":7002,"state":"approved","environment_ids":[161],"comment":""}`,
		`{"run_id":7002,"state":"approved","environment_ids":[161],"comment":"` + strings.Repeat("c", 1001) + `"}`,
		`{"run_id":7002,"state":"approved","environment_ids":[161],"comment":"a\u0007b"}`,
		`{"run_id":7002,"state":"approved","environment_ids":[161],"comment":{"a":1}}`,
		`{"run_id":0,"state":"approved","environment_ids":[161],"comment":"x"}`,
		`{"state":"approved","environment_ids":[161],"comment":"x"}`,
		`{"run_id":7002,"state":"approved","environment_ids":[161],"comment":"x","prevent_self_review":false}`,
	} {
		before := len(f.recorded())
		if _, err := invoke(t, core, pendingDeploymentsReview.ID, "gates", arguments, true); !isInvalidRequest(err) {
			t.Errorf("review %s = %v, want an invalid request", arguments, err)
		}
		if len(f.recorded()) != before {
			t.Errorf("review %s reached GitHub", arguments)
		}
	}
	long := strings.Repeat("c", 1000)
	if _, err := invoke(t, core, pendingDeploymentsReview.ID, "gates",
		`{"run_id":7002,"state":"approved","environment_ids":[161],"comment":"`+long+`"}`, true); err != nil {
		t.Errorf("review with a comment of 1000 characters = %v", err)
	}
}

// Every refusal names its cause in Qatlas' own words, never GitHub's text; an unclear outcome says that the
// change may have been applied; each case sends exactly one POST.
func TestGateFailuresAreNamedAndChangesSentOnce(t *testing.T) {
	f, g, core, _ := serveGates(t, nil)
	calls := map[string]string{runsApprove.ID: `{"run_id":7000}`, pendingDeploymentsReview.ID: gateReviewArguments}
	for _, tt := range []struct {
		name      string
		answer    func(http.ResponseWriter, *http.Request)
		class     provider.Class
		fragment  string
		uncertain bool
	}{
		{"forbidden", func(w http.ResponseWriter, _ *http.Request) { refuse(w, http.StatusForbidden) },
			provider.ClassPermission, "read and write", false},
		{"not found", func(w http.ResponseWriter, _ *http.Request) { refuse(w, http.StatusNotFound) },
			provider.ClassNotFound, "", false},
		{"conflict", func(w http.ResponseWriter, _ *http.Request) { refuse(w, http.StatusConflict) },
			provider.ClassProviderError, "nothing was changed", false},
		{"unprocessable", func(w http.ResponseWriter, _ *http.Request) { refuse(w, http.StatusUnprocessableEntity) },
			provider.ClassProviderError, "nothing was changed", false},
		{"gateway timeout", func(w http.ResponseWriter, _ *http.Request) { refuse(w, http.StatusGatewayTimeout) },
			provider.ClassTimeout, "did not answer in time", true},
		{"server error", func(w http.ResponseWriter, _ *http.Request) { refuse(w, http.StatusInternalServerError) },
			provider.ClassProviderError, "", true},
		{"dropped connection", func(w http.ResponseWriter, _ *http.Request) {
			if connection, _, err := w.(http.Hijacker).Hijack(); err == nil {
				connection.Close()
			}
		}, "", "", true},
	} {
		g.changeAnswer = tt.answer
		for id, arguments := range calls {
			before := len(f.recorded())
			_, err := invoke(t, core, id, "gates", arguments, true)
			if err == nil || (tt.class != "" && classOf(err) != tt.class) || !strings.Contains(err.Error(), tt.fragment) ||
				strings.Contains(err.Error(), uncertain) != tt.uncertain {
				t.Errorf("%s %s = %v, want %s naming %q, uncertain %t", tt.name, id, err, tt.class, tt.fragment, tt.uncertain)
			}
			if err != nil && (strings.Contains(err.Error(), cacheProviderCanary) || strings.Contains(err.Error(), conflictMessage) ||
				strings.Contains(err.Error(), rejectedMessage)) {
				t.Errorf("%s %s quotes provider text or a generic message: %v", tt.name, id, err)
			}
			if sent := postsOf(f, before); len(sent) != 1 {
				t.Errorf("%s %s sent %d POST requests, want exactly one", tt.name, id, len(sent))
			}
		}
	}
	g.changeAnswer = nil

	// A refused proof read ends the call before a POST.
	g.cleanup.readStatus = http.StatusForbidden
	for id, arguments := range calls {
		before := len(f.recorded())
		_, err := invoke(t, core, id, "gates", arguments, true)
		if classOf(err) != provider.ClassPermission || strings.Contains(err.Error(), uncertain) || len(postsOf(f, before)) != 0 {
			t.Errorf("%s with a refused read = %v, want a permission refusal and no POST", id, err)
		}
	}
}

// A client timeout and a stalled answer leave the outcome open and are never repeated.
func TestGateChangesAreNeverRepeatedAfterATimeout(t *testing.T) {
	for _, call := range []string{"approve", "review"} {
		t.Run(call, func(t *testing.T) {
			f, g, _, base := serveGates(t, nil)
			g.changeAnswer = func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() }
			ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
			defer cancel()
			c := client(t, base, repoTarget)
			var err error
			if call == "approve" {
				_, err = c.approveRun(ctx, forkRunConclusion)
			} else {
				_, err = c.reviewPendingDeployments(ctx, &actionsArguments{RunID: waitingRun, ReviewState: "approved",
					EnvironmentIDs: []int64{161}, Comment: "x"})
			}
			if err == nil || !strings.Contains(err.Error(), "may have been applied") {
				t.Errorf("%s = %v, want an unclear outcome", call, err)
			}
			if posts := postsOf(f, 0); len(posts) != 1 {
				t.Errorf("sent %d POST requests, want exactly one", len(posts))
			}
		})
	}
}

// A repository outside the targets and an excluded one end before a secret is read or GitHub is contacted;
// both tools need their name in a connection's tools list and a confirmation.
func TestGatesAreListedOnlyConfirmedAndBoundBeforeIO(t *testing.T) {
	reads := 0
	f, _, core, _ := serveGates(t, &reads)
	valid := map[string]string{runsApprove.ID: `"run_id":7000`,
		pendingDeploymentsReview.ID: `"run_id":7002,"state":"approved","environment_ids":[161],"comment":"x"`}
	var unsupported *capability.UnsupportedError
	var confirm *application.ConfirmationRequiredError
	reg := registry(t)
	for _, id := range gateIDs {
		descriptor, _, _ := reg.Lookup(id)
		if !descriptor.RequiresToolAllowList || descriptor.Risk.Confirmation != capability.ConfirmationRequired ||
			descriptor.Risk.Effect != capability.EffectUpdate || descriptor.Group != "actions" {
			t.Errorf("%s is not a listed-only, confirmed update of the actions group", id)
		}
		if _, err := core.Describe(application.DescribeRequest{Operation: id, Connection: "gates-unlisted"}); !errors.As(err, &unsupported) {
			t.Errorf("describe %s without a tools list = %v, want unsupported", id, err)
		}
		if _, err := invoke(t, core, id, "gates-unlisted", `{`+valid[id]+`}`, true); !errors.As(err, &unsupported) {
			t.Errorf("%s without a tools list = %v, want unsupported", id, err)
		}
		if _, err := invoke(t, core, id, "gates", `{`+valid[id]+`}`, false); !errors.As(err, &confirm) {
			t.Errorf("unconfirmed %s = %v, want confirmation-required", id, err)
		}
		for name, tt := range map[string]struct{ connection, repository string }{
			"outside the targets": {"gates", "octo-org/other"},
			"another owner":       {"gates", "other-org/example"},
			"excluded":            {"gates-excluding", "octo-org/example"},
		} {
			if _, err := invoke(t, core, id, tt.connection, `{"repository":"`+tt.repository+`",`+valid[id]+`}`, true); !isInvalidRequest(err) {
				t.Errorf("%s on a repository %s = %v, want an invalid request", id, name, err)
			}
		}
	}
	if reads != 0 || len(f.recorded()) != 0 {
		t.Errorf("refused requests read %d credentials and sent %d requests", reads, len(f.recorded()))
	}
}
