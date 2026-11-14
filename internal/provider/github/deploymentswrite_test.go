package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-kit/redact"
)

var deploymentWriteIDs = []string{deploymentsCreate.ID, deploymentsDelete.ID}

// deploymentWrites answers POST and DELETE on the deployment routes through the failure hook of
// fakeDeployments and records the bodies it received.
type deploymentWrites struct {
	status   int
	response string
	d        *fakeDeployments
	f        *fakeGitHub
}

func serveDeploymentWrites(t *testing.T, status int, response string) (*deploymentWrites, string) {
	t.Helper()
	f, d, base := serveDeployments(t)
	w := &deploymentWrites{status: status, response: response, d: d, f: f}
	d.fail = func(rw http.ResponseWriter, r *http.Request) bool {
		if r.Method != http.MethodPost && r.Method != http.MethodDelete {
			return false
		}
		rw.Header().Set("Content-Type", "application/json")
		rw.WriteHeader(w.status)
		fmt.Fprint(rw, w.response)
		return true
	}
	return w, base
}

// bodies returns the JSON bodies of the POST requests the fake received.
func (w *deploymentWrites) bodies() []map[string]any {
	var bodies []map[string]any
	for _, request := range w.f.recorded() {
		if request.method == http.MethodPost {
			bodies = append(bodies, request.body)
		}
	}
	return bodies
}

func (w *deploymentWrites) posts() []string {
	var posts []string
	for _, request := range w.d.requests() {
		if strings.HasPrefix(request, http.MethodPost+" ") {
			posts = append(posts, request)
		}
	}
	return posts
}

func deploymentWriteConfig(base string) *config.Config {
	cfg := coreConfig(base)
	all := []config.Effect{config.EffectRead, config.EffectCreate, config.EffectDelete}
	cfg.Connections["repo"] = config.Connection{Service: "gh", Credential: "gh-reader", Target: repoTarget, Effects: all}
	cfg.Connections["listed"] = config.Connection{Service: "gh", Credential: "gh-reader", Target: repoTarget,
		Effects: all, Tools: append(append([]string{}, deploymentToolIDs...), deploymentWriteIDs...)}
	cfg.Connections["foreign"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Target: "repos/octo-org/other", Effects: all, Tools: deploymentWriteIDs}
	cfg.Connections["ex"] = config.Connection{Service: "gh", Credential: "gh-reader", Effects: all,
		Tools: deploymentWriteIDs, Exclude: []string{repoTarget}}
	return cfg
}

const createdDeploymentJSON = `{"url":"` + deploymentAPI + `/deployments/77","id":77,"node_id":"MDEwOkRlcGxveW1lbnQ3Nw==",` +
	`"sha":"a84d88e7554fc1fa21bcbc4efae3c782a70d2b9d","ref":"topic-branch","task":"deploy","payload":"{\"a\":1}",` +
	`"original_environment":"staging","environment":"staging","description":"Deploy request","creator":null,` +
	`"created_at":"2026-07-20T01:19:13Z","updated_at":"2026-07-20T01:19:13Z","statuses_url":"` + deploymentAPI +
	`/deployments/77/statuses","repository_url":"` + deploymentAPI + `","transient_environment":false,` +
	`"production_environment":false}`

// The request carries only the arguments given, with the payload as the text it is, and never
// required_contexts or auto_merge; the default answer is narrow and the full one has every member.
func TestDeploymentCreateSendsOneRequestAndAnswersNarrowByDefault(t *testing.T) {
	w, base := serveDeploymentWrites(t, http.StatusCreated, createdDeploymentJSON)
	red := &redact.Redactor{}
	core := application.New(registry(t), deploymentWriteConfig(base), resolver(red, nil), red)

	if _, err := invoke(t, core, deploymentsCreate.ID, "listed", `{"ref":"topic-branch"}`, false); err == nil ||
		len(w.posts()) != 0 {
		t.Fatalf("create without confirmation = %v, want confirmation-required before GitHub", err)
	}
	arguments := `{"ref":"topic-branch","environment":"staging","task":"deploy","description":"Deploy request",` +
		`"payload":"{\"a\":1}","transient_environment":false,"production_environment":false}`
	response, err := core.Invoke(context.Background(), application.InvokeRequest{Operation: deploymentsCreate.ID,
		Connection: "listed", Arguments: json.RawMessage(arguments), Confirmed: true})
	if err != nil {
		t.Fatal(err)
	}
	result := response.Result
	if got := memberNames(t, result); got != "environment,id,ref,repository,sha,state,task" {
		t.Errorf("default answer members = %s", got)
	}
	if len(w.posts()) != 1 || w.posts()[0] != "POST "+deploymentsRoute {
		t.Fatalf("requests = %v, want one POST", w.d.requests())
	}
	sent := w.bodies()[0]
	if len(sent) != 7 || sent["payload"] != `{"a":1}` || sent["ref"] != "topic-branch" ||
		sent["transient_environment"] != false {
		t.Errorf("body = %v, want the seven given members with payload as text", sent)
	}
	for _, forbidden := range []string{"required_contexts", "auto_merge"} {
		if _, ok := sent[forbidden]; ok {
			t.Errorf("body carries %s", forbidden)
		}
	}

	only, err := invoke(t, core, deploymentsCreate.ID, "listed", `{"ref":"topic-branch"}`, true)
	if err != nil {
		t.Fatal(err)
	}
	if minimal := w.bodies()[1]; len(minimal) != 1 {
		t.Errorf("minimal body = %v, want only ref", minimal)
	}
	reg := registry(t)
	descriptor, _, _ := reg.Lookup(deploymentsCreate.ID)
	assertDocumentedMembers(t, only, descriptor.OutputSchema, []string{"state", "id", "node_id", "sha", "ref",
		"task", "payload", "original_environment", "environment", "description", "created_at", "updated_at",
		"transient_environment", "production_environment", "links", "statuses_url", "repository_url", "url"})
	if !strings.Contains(string(only), `"state":"created"`) {
		t.Errorf("full answer = %s", only)
	}
}

// GitHub's 202 and 409 are results, not errors; neither carries a deployment, and nothing of GitHub's text
// reaches the answer.
func TestDeploymentCreateAnswers202And409AsResults(t *testing.T) {
	for name, tt := range map[string]struct {
		status int
		body   string
		state  string
	}{
		"merged branch": {http.StatusAccepted, `{"message":"Auto-merged master into topic-branch ` + providerCanary + `"}`, "accepted"},
		"conflict":      {http.StatusConflict, `{"message":"Conflict: Commit status checks failed ` + providerCanary + `"}`, "conflict"},
	} {
		t.Run(name, func(t *testing.T) {
			w, base := serveDeploymentWrites(t, tt.status, tt.body)
			red := &redact.Redactor{}
			core := application.New(registry(t), deploymentWriteConfig(base), resolver(red, nil), red)
			result, err := invoke(t, core, deploymentsCreate.ID, "listed", `{"ref":"topic-branch"}`, true)
			if err != nil || memberNames(t, result) != "repository,state" || !strings.Contains(string(result), `"state":"`+tt.state+`"`) ||
				strings.Contains(string(result), providerCanary) {
				t.Errorf("answer = %s, %v, want only state %s", result, err, tt.state)
			}
			if len(w.posts()) != 1 {
				t.Errorf("sent %d POST requests, want one", len(w.posts()))
			}
		})
	}
}

// Each refusal names its cause without GitHub's text, and an unclear outcome is never repeated.
func TestDeploymentWritesNameTheCauseWithoutProviderText(t *testing.T) {
	for name, tt := range map[string]struct {
		id        string
		arguments string
		status    int
		class     provider.Class
		fragment  string
	}{
		"create without permission": {deploymentsCreate.ID, `{"ref":"main"}`, http.StatusForbidden, provider.ClassPermission, "Deployments: read and write"},
		"create of a missing repo":  {deploymentsCreate.ID, `{"ref":"main"}`, http.StatusNotFound, provider.ClassNotFound, "deployments"},
		"create of a missing ref":   {deploymentsCreate.ID, `{"ref":"main"}`, http.StatusUnprocessableEntity, provider.ClassProviderError, "the ref may not exist"},
		"create rate limited":       {deploymentsCreate.ID, `{"ref":"main"}`, http.StatusTooManyRequests, provider.ClassRateLimited, "rate-limited"},
		"delete without permission": {deploymentsDelete.ID, `{"deployment_id":42}`, http.StatusForbidden, provider.ClassPermission, "Deployments: read and write"},
		"delete gone meanwhile":     {deploymentsDelete.ID, `{"deployment_id":42}`, http.StatusNotFound, provider.ClassNotFound, "deployment 42"},
		"delete of an active one":   {deploymentsDelete.ID, `{"deployment_id":42}`, http.StatusUnprocessableEntity, provider.ClassProviderError, "inactive deployments"},
	} {
		t.Run(name, func(t *testing.T) {
			_, base := serveDeploymentWrites(t, tt.status, `{"message":"`+providerCanary+`"}`)
			red := &redact.Redactor{}
			core := application.New(registry(t), deploymentWriteConfig(base), resolver(red, nil), red)
			_, err := invoke(t, core, tt.id, "listed", tt.arguments, true)
			if classOf(err) != tt.class || err == nil || !strings.Contains(err.Error(), tt.fragment) ||
				strings.Contains(err.Error(), providerCanary) || strings.Contains(err.Error(), uncertain) {
				t.Errorf("%s = %v, want %s naming %q without provider text", name, err, tt.class, tt.fragment)
			}
		})
	}
}

func TestDeploymentWritesAreNeverRepeatedAfterAnUnclearOutcome(t *testing.T) {
	for name, answer := range map[string]func(http.ResponseWriter, *http.Request){
		"server error":    func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) },
		"gateway timeout": func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusGatewayTimeout) },
		"dropped connection": func(w http.ResponseWriter, _ *http.Request) {
			if connection, _, err := w.(http.Hijacker).Hijack(); err == nil {
				connection.Close()
			}
		},
		"client timeout": func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() },
	} {
		for _, method := range []string{http.MethodPost, http.MethodDelete} {
			t.Run(name+" "+method, func(t *testing.T) {
				_, d, base := serveDeployments(t)
				d.fail = func(w http.ResponseWriter, r *http.Request) bool {
					if r.Method != method {
						return false
					}
					d.mu.Unlock()
					defer d.mu.Lock()
					answer(w, r)
					return true
				}
				timeout := 2 * time.Second
				if name == "client timeout" {
					timeout = 200 * time.Millisecond
				}
				ctx, cancel := context.WithTimeout(context.Background(), timeout)
				defer cancel()
				c := client(t, base, repoTarget)
				var err error
				if method == http.MethodPost {
					_, err = c.createDeployment(ctx, &environmentsArguments{Ref: "main"})
				} else {
					_, err = c.deleteDeployment(ctx, 42)
				}
				if err == nil || !strings.Contains(err.Error(), "may have been applied") {
					t.Errorf("%s = %v, want an unclear outcome", method, err)
				}
				sent := 0
				for _, request := range d.requests() {
					if strings.HasPrefix(request, method+" ") {
						sent++
					}
				}
				if sent != 1 {
					t.Errorf("sent %d %s requests, want exactly one", sent, method)
				}
			})
		}
	}
}

// The delete reads the deployment of the bound repository once before its one DELETE, and a deployment
// another repository owns, or another id, ends at that read.
func TestDeploymentDeleteIsBoundToTheRepositoryByItsProofRead(t *testing.T) {
	w, base := serveDeploymentWrites(t, http.StatusNoContent, "")
	red := &redact.Redactor{}
	core := application.New(registry(t), deploymentWriteConfig(base), resolver(red, nil), red)
	if _, err := invoke(t, core, deploymentsDelete.ID, "listed", `{"deployment_id":42}`, false); err == nil ||
		len(w.d.requests()) != 0 {
		t.Fatalf("delete without confirmation = %v, want confirmation-required before GitHub", err)
	}
	result, err := invoke(t, core, deploymentsDelete.ID, "listed", `{"deployment_id":42}`, true)
	if err != nil || string(result) != `{"deleted":true,"deployment_id":42,"repository":"octo-org/example"}` {
		t.Fatalf("delete = %s, %v", result, err)
	}
	if sent := w.d.requests(); len(sent) != 2 || sent[0] != "GET "+deploymentsRoute+"/42" ||
		sent[1] != "DELETE "+deploymentsRoute+"/42" {
		t.Errorf("requests = %v, want one proof read and one DELETE", sent)
	}

	for name, mutate := range map[string]func(*fakeDeployments){
		"another repository": func(d *fakeDeployments) { d.foreignRepository = "https://ghe.example/api/v3/repos/octo-org/other" },
		"another id":         func(d *fakeDeployments) { d.shiftID = 1 },
	} {
		t.Run(name, func(t *testing.T) {
			w, base := serveDeploymentWrites(t, http.StatusNoContent, "")
			mutate(w.d)
			red := &redact.Redactor{}
			core := application.New(registry(t), deploymentWriteConfig(base), resolver(red, nil), red)
			_, err := invoke(t, core, deploymentsDelete.ID, "listed", `{"deployment_id":42}`, true)
			if classOf(err) != provider.ClassInvalidResponse || w.d.deletes() != 0 {
				t.Errorf("delete = %v after %d DELETE requests, want a refused answer and none", err, w.d.deletes())
			}
		})
	}
}

// A repository outside the targets, an excluded one, and arguments out of bounds, among them the two GitHub
// members the tool never offers, are refused before a secret is read or GitHub is contacted; both tools need
// their name in a connection's tools list.
func TestDeploymentWritesRefuseForeignTargetsAndBadArgumentsBeforeIO(t *testing.T) {
	w, base := serveDeploymentWrites(t, http.StatusCreated, createdDeploymentJSON)
	reads := 0
	red := &redact.Redactor{}
	reg := registry(t)
	core := application.New(reg, deploymentWriteConfig(base), resolver(red, &reads), red)
	noIO := func(what string, call func() error, refused func(error) bool) {
		t.Helper()
		reads = 0
		before := len(w.d.requests())
		if err := call(); !refused(err) {
			t.Errorf("%s = %v, want a refusal", what, err)
		}
		if reads != 0 || len(w.d.requests()) != before {
			t.Errorf("%s reached the credential or GitHub", what)
		}
	}
	valid := map[string]string{deploymentsCreate.ID: `"ref":"main"`, deploymentsDelete.ID: `"deployment_id":42`}
	for _, id := range deploymentWriteIDs {
		for _, connection := range []string{"foreign", "ex"} {
			noIO(id+" on "+connection, func() error {
				_, err := invoke(t, core, id, connection, `{"repository":"octo-org/example",`+valid[id]+`}`, true)
				return err
			}, func(err error) bool { return err != nil })
		}
		noIO(id+" without a tools list entry", func() error {
			_, err := invoke(t, core, id, "repo", `{`+valid[id]+`}`, true)
			var unsupported *capability.UnsupportedError
			if !errors.As(err, &unsupported) {
				return nil
			}
			return err
		}, func(err error) bool { return err != nil })
		descriptor, _, _ := reg.Lookup(id)
		if !descriptor.RequiresToolAllowList || descriptor.Risk.Confirmation != capability.ConfirmationRequired {
			t.Errorf("%s is not listed-only with confirmation", id)
		}
	}
	for id, arguments := range map[string][]string{
		deploymentsCreate.ID: {`{}`, `{"ref":"../x"}`, `{"ref":"main","required_contexts":[]}`,
			`{"ref":"main","auto_merge":false}`, `{"ref":"main","environment":"` + strings.Repeat("e", 256) + `"}`,
			`{"ref":"main","task":"deploy\u0001"}`, `{"ref":"main","description":"` + strings.Repeat("d", 141) + `"}`,
			`{"ref":"main","payload":"` + strings.Repeat("p", 4097) + `"}`, `{"ref":"main","payload":{"a":1}}`,
			`{"ref":"main","transient_environment":"yes"}`},
		deploymentsDelete.ID: {`{}`, `{"deployment_id":0}`, `{"deployment_id":-1}`},
	} {
		for _, argument := range arguments {
			noIO(id+" "+argument, func() error {
				_, err := invoke(t, core, id, "listed", argument, true)
				return err
			}, func(err error) bool { return err != nil })
		}
	}
	for _, id := range deploymentWriteIDs {
		descriptor, _, _ := reg.Lookup(id)
		for _, argument := range descriptor.Arguments {
			if argument.Name == "required_contexts" || argument.Name == "auto_merge" {
				t.Errorf("%s offers %s", id, argument.Name)
			}
		}
	}
}

// Commit statuses and deployment statuses are never written: a status of another system could vouch for a
// check that never ran.
func TestNoToolWritesCommitStatusesOrDeploymentStatuses(t *testing.T) {
	reg := registry(t)
	for _, id := range []string{"github.commit-statuses.create", "github.deployment-statuses.create"} {
		if _, _, ok := reg.Lookup(id); ok {
			t.Errorf("%s exists", id)
		}
	}
	for _, descriptor := range reg.All() {
		if !strings.HasPrefix(descriptor.ID, Provider+".") {
			continue
		}
		name := strings.TrimPrefix(descriptor.ID, Provider+".")
		if (strings.HasPrefix(name, "commit-statuses.") || strings.HasPrefix(name, "deployment-statuses.")) &&
			descriptor.Risk.Effect != capability.EffectRead {
			t.Errorf("%s writes a status", descriptor.ID)
		}
	}
}
