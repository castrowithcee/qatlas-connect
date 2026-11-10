package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	deploymentsRoute     = "/api/v3/repos/octo-org/example/deployments"
	protectionRulesRoute = "/api/v3/repos/octo-org/example/environments/production/deployment_protection_rules"
	// The fake spells the host of its addresses differently from the test server, as GitHub may.
	deploymentAPI = "https://ghe.example/api/v3/repos/octo-org/example"
)

const deploymentAppJSON = `{"id":37,"slug":"deploy-bot","node_id":"MDExOkludGVncmF0aW9uMQ==",` +
	`"client_id":"Iv1.25b5d1e65ffc4022","owner":{"login":"octo-org","id":9919,"node_id":"MDQ6VXNlcjE=",` +
	`"avatar_url":"https://avatars.example/u/9919","gravatar_id":"","url":"https://api.github.com/users/octo-org",` +
	`"html_url":"https://github.com/octo-org","type":"Organization","site_admin":false},"name":"Deploy bot",` +
	`"description":"Deploys","external_url":"https://example.com","html_url":"https://github.com/apps/deploy-bot",` +
	`"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-02T00:00:00Z","events":["deployment"],` +
	`"installations_count":5}`

// fakeDeployments answers the deployment, deployment status, and deployment protection rule routes through the
// failure hook of fakeGitHub. The foreign members, when set, replace what an answer claims about itself.
type fakeDeployments struct {
	mu    sync.Mutex
	paths []string
	rules map[int64]bool
	// foreignRepository replaces every repository_url, foreignDeployment the deployment_url of a status, and
	// shiftID moves the id of every answered object.
	foreignRepository, foreignDeployment string
	shiftID                              int64
	fail                                 func(http.ResponseWriter, *http.Request) bool
}

func serveDeployments(t *testing.T) (*fakeGitHub, *fakeDeployments, string) {
	t.Helper()
	d := &fakeDeployments{rules: map[int64]bool{3: true, 4: true}}
	f := &fakeGitHub{}
	f.failure = d.route
	return f, d, serve(t, f)
}

func (d *fakeDeployments) repository() string {
	if d.foreignRepository != "" {
		return d.foreignRepository
	}
	return deploymentAPI
}

func (d *fakeDeployments) deploymentJSON(id int64) string {
	id += d.shiftID
	return fmt.Sprintf(`{"url":"%[2]s/deployments/%[1]d","id":%[1]d,"node_id":"MDEwOkRlcGxveW1lbnQx",`+
		`"sha":"a84d88e7554fc1fa21bcbc4efae3c782a70d2b9d","ref":"topic-branch","task":"deploy",`+
		`"payload":{"note":"%[3]s","nested":{"n":1}},"original_environment":"staging","environment":"production",`+
		`"description":"Deploy request from hubot","creator":%[4]s,"created_at":"2026-07-20T01:19:13Z",`+
		`"updated_at":"2026-07-20T01:19:14Z","statuses_url":"%[2]s/deployments/%[1]d/statuses",`+
		`"repository_url":"%[2]s","transient_environment":false,"production_environment":true,`+
		`"performed_via_github_app":%[5]s}`, id, d.repository(), strings.Repeat("p", 1200),
		simpleUserJSON("octocat", 1, "User"), deploymentAppJSON)
}

func (d *fakeDeployments) statusJSON(deploymentID, id int64) string {
	deployment := fmt.Sprintf("%s/deployments/%d", deploymentAPI, deploymentID)
	if d.foreignDeployment != "" {
		deployment = d.foreignDeployment
	}
	id += d.shiftID
	return fmt.Sprintf(`{"url":"%[2]s/statuses/%[1]d","id":%[1]d,"node_id":"MDE2OkRlcGxveW1lbnRTdGF0dXMx",`+
		`"state":"success","creator":%[3]s,"description":"Deployment finished successfully.",`+
		`"environment":"production","target_url":"https://example.com/deployment/42/output",`+
		`"created_at":"2026-07-20T01:19:13Z","updated_at":"2026-07-20T01:19:14Z","deployment_url":"%[2]s",`+
		`"repository_url":"%[4]s","environment_url":"https://test-branch.lab.acme.com",`+
		`"log_url":"https://example.com/deployment/42/output","performed_via_github_app":%[5]s}`,
		id, deployment, simpleUserJSON("octocat", 1, "User"), d.repository(), deploymentAppJSON)
}

func (d *fakeDeployments) ruleJSON(id int64) string {
	return fmt.Sprintf(`{"id":%d,"node_id":"IEH37kRlcGxveW1lbnRTdGF0ddiv","enabled":true,"app":{"id":1,`+
		`"node_id":"GHT58kRlcGxveW1lbnRTdTY!bbcy","slug":"a-custom-app",`+
		`"integration_url":"https://api.github.com/apps/a-custom-app"}}`, id+d.shiftID)
}

func (d *fakeDeployments) route(w http.ResponseWriter, r *http.Request) bool {
	path := r.URL.EscapedPath()
	if !strings.HasPrefix(path, deploymentsRoute) && !strings.HasPrefix(path, "/api/v3/repos/octo-org/example/environments/") {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.paths = append(d.paths, r.Method+" "+r.URL.RequestURI())
	if d.fail != nil && d.fail(w, r) {
		return true
	}
	w.Header().Set("Content-Type", "application/json")
	if rest, ok := strings.CutPrefix(path, protectionRulesRoute); ok {
		if rest == "" && r.Method == http.MethodGet {
			entries := []string{}
			for _, id := range []int64{3, 4} {
				if d.rules[id] {
					entries = append(entries, d.ruleJSON(id))
				}
			}
			fmt.Fprintf(w, `{"total_count":%d,"custom_deployment_protection_rules":[%s]}`, len(entries),
				strings.Join(entries, ","))
			return true
		}
		id, _ := strconv.ParseInt(strings.TrimPrefix(rest, "/"), 10, 64)
		if !d.rules[id] {
			return notFoundAnswer(w)
		}
		switch r.Method {
		case http.MethodGet:
			fmt.Fprint(w, d.ruleJSON(id))
		case http.MethodDelete:
			delete(d.rules, id)
			w.WriteHeader(http.StatusNoContent)
		default:
			return false
		}
		return true
	}
	if r.Method != http.MethodGet {
		return false
	}
	segments := strings.Split(strings.TrimPrefix(strings.TrimPrefix(path, deploymentsRoute), "/"), "/")
	switch {
	case segments[0] == "":
		if r.URL.Query().Get("page") != "2" {
			w.Header().Set("Link", `<https://api.github.com/x?page=2>; rel="next"`)
		}
		fmt.Fprintf(w, `[%s,%s]`, d.deploymentJSON(42), d.deploymentJSON(41))
	case len(segments) == 1:
		id, _ := strconv.ParseInt(segments[0], 10, 64)
		fmt.Fprint(w, d.deploymentJSON(id))
	case len(segments) == 2 && segments[1] == "statuses":
		id, _ := strconv.ParseInt(segments[0], 10, 64)
		w.Header().Set("Link", `<https://api.github.com/x?page=2>; rel="next"`)
		fmt.Fprintf(w, `[%s]`, d.statusJSON(id, 1))
	case len(segments) == 3 && segments[1] == "statuses":
		id, _ := strconv.ParseInt(segments[0], 10, 64)
		status, _ := strconv.ParseInt(segments[2], 10, 64)
		fmt.Fprint(w, d.statusJSON(id, status))
	default:
		return notFoundAnswer(w)
	}
	return true
}

func (d *fakeDeployments) requests() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.paths...)
}

func (d *fakeDeployments) deletes() int {
	n := 0
	for _, request := range d.requests() {
		if strings.HasPrefix(request, http.MethodDelete+" ") {
			n++
		}
	}
	return n
}

var deploymentToolIDs = []string{deploymentsList.ID, deploymentsGet.ID, deploymentStatusesList.ID,
	deploymentStatusesGet.ID, protectionRulesList.ID, protectionRulesGet.ID, protectionRulesDelete.ID}

var deploymentArguments = map[string]string{
	deploymentsList.ID:        `{}`,
	deploymentsGet.ID:         `{"deployment_id":42}`,
	deploymentStatusesList.ID: `{"deployment_id":42}`,
	deploymentStatusesGet.ID:  `{"deployment_id":42,"status_id":7}`,
	protectionRulesList.ID:    `{"environment":"production"}`,
	protectionRulesGet.ID:     `{"environment":"production","protection_rule_id":3}`,
	protectionRulesDelete.ID:  `{"environment":"production","protection_rule_id":3}`,
}

// deploymentsConfig binds a connection with every effect and no tools list, one that lists every deployment
// tool, one whose only target is another repository, and one that excludes the repository.
func deploymentsConfig(base string) *config.Config {
	cfg := coreConfig(base)
	all := []config.Effect{config.EffectRead, config.EffectDelete}
	cfg.Connections["repo"] = config.Connection{Service: "gh", Credential: "gh-reader", Target: repoTarget, Effects: all}
	cfg.Connections["listed"] = config.Connection{Service: "gh", Credential: "gh-reader", Target: repoTarget,
		Effects: all, Tools: deploymentToolIDs}
	cfg.Connections["foreign"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Target: "repos/octo-org/other", Effects: all, Tools: deploymentToolIDs}
	cfg.Connections["ex"] = config.Connection{Service: "gh", Credential: "gh-reader", Effects: all,
		Tools: deploymentToolIDs, Exclude: []string{repoTarget}}
	return cfg
}

// Every read answers every member GitHub documents within its output schema, the filters and the paging reach
// GitHub as its query, the payload stays cut text, and the delete reads the rule once before one DELETE.
func TestDeploymentToolsSatisfyTheirContractThroughTheApplicationCore(t *testing.T) {
	_, d, base := serveDeployments(t)
	red := &redact.Redactor{}
	reg := registry(t)
	core := application.New(reg, deploymentsConfig(base), resolver(red, nil), red)
	schema := func(id string) json.RawMessage {
		descriptor, _, _ := reg.Lookup(id)
		return descriptor.OutputSchema
	}
	account := []string{"login", "avatar_url", "gravatar_id", "site_admin", "name", "email", "starred_at",
		"user_view_type", "followers_url", "received_events_url"}
	app := []string{"slug", "client_id", "owner", "external_url", "events", "installations_count"}

	listed, err := invoke(t, core, deploymentsList.ID, "listed", `{}`, false)
	var list DeploymentList
	if err != nil || json.Unmarshal(listed, &list) != nil || len(list.Deployments) != 2 || !list.HasMore ||
		list.NextCursor == "" || !strings.Contains(string(listed), `"repository":"octo-org/example"`) {
		t.Fatalf("list = %s, %v", listed, err)
	}
	assertDocumentedMembers(t, listed, schema(deploymentsList.ID), append(append([]string{"deployments", "id",
		"node_id", "sha", "ref", "task", "payload", "payload_truncated", "original_environment", "environment",
		"description", "creator", "created_at", "updated_at", "transient_environment", "production_environment",
		"performed_via_github_app", "links", "statuses_url", "repository_url", "has_more", "next_cursor"},
		account...), app...))
	payload := *list.Deployments[0].Payload
	if !list.Deployments[0].PayloadTruncated || len([]rune(payload)) != projectionTextLimit ||
		!strings.HasPrefix(payload, `{"nested":{"n":1},"note":"ppp`) && !strings.HasPrefix(payload, `{"note":"ppp`) {
		t.Errorf("payload = %q (cut %t), want compact JSON text cut at the text limit", payload,
			list.Deployments[0].PayloadTruncated)
	}

	next, err := invoke(t, core, deploymentsList.ID, "listed", `{"cursor":"`+list.NextCursor+`"}`, false)
	if err != nil || strings.Contains(string(next), `"has_more":true`) {
		t.Errorf("second batch = %s, %v", next, err)
	}
	if last := d.requests()[len(d.requests())-1]; !strings.Contains(last, "page=2") || !strings.Contains(last, "per_page=30") {
		t.Errorf("second batch request = %q, want page 2 of 30", last)
	}
	if _, err := invoke(t, core, deploymentsList.ID, "listed", `{"ref":"main","cursor":"`+list.NextCursor+`"}`,
		false); !isInvalidRequest(err) {
		t.Errorf("a cursor of other filters = %v, want an invalid request", err)
	}

	filtered := `{"sha":"a84d88e7554fc1fa21bcbc4efae3c782a70d2b9d","ref":"release/v1","task":"deploy:migrations",` +
		`"environment":"prod/eu west","limit":5}`
	if _, err := invoke(t, core, deploymentsList.ID, "listed", filtered, false); err != nil {
		t.Fatalf("filtered list = %v", err)
	}
	request := d.requests()[len(d.requests())-1]
	query, _ := url.ParseQuery(request[strings.Index(request, "?")+1:])
	for key, want := range map[string]string{"sha": "a84d88e7554fc1fa21bcbc4efae3c782a70d2b9d", "ref": "release/v1",
		"task": "deploy:migrations", "environment": "prod/eu west", "per_page": "5", "page": "1"} {
		if query.Get(key) != want {
			t.Errorf("filtered query %s = %q, want %q (%s)", key, query.Get(key), want, request)
		}
	}

	got, err := invoke(t, core, deploymentsGet.ID, "listed", `{"deployment_id":42}`, false)
	if err != nil || !strings.Contains(string(got), `"id":42`) {
		t.Fatalf("get = %s, %v", got, err)
	}
	assertDocumentedMembers(t, got, schema(deploymentsGet.ID), []string{"id", "sha", "ref", "payload", "creator",
		"performed_via_github_app", "links", "url", "statuses_url", "repository_url"})

	statuses, err := invoke(t, core, deploymentStatusesList.ID, "listed", `{"deployment_id":42,"limit":1}`, false)
	if err != nil || !strings.Contains(string(statuses), `"deployment_id":42`) ||
		!strings.Contains(string(statuses), `"has_more":true`) {
		t.Fatalf("status list = %s, %v", statuses, err)
	}
	assertDocumentedMembers(t, statuses, schema(deploymentStatusesList.ID), append(append([]string{"statuses",
		"id", "node_id", "state", "creator", "description", "environment", "created_at", "updated_at",
		"performed_via_github_app", "links", "url", "deployment_url", "repository_url", "target_url",
		"environment_url", "log_url", "deployment_id", "has_more", "next_cursor"}, account...), app...))

	status, err := invoke(t, core, deploymentStatusesGet.ID, "listed", `{"deployment_id":42,"status_id":7}`, false)
	if err != nil || !strings.Contains(string(status), `"id":7`) {
		t.Fatalf("status get = %s, %v", status, err)
	}
	assertDocumentedMembers(t, status, schema(deploymentStatusesGet.ID), []string{"id", "state", "log_url",
		"target_url", "environment_url", "deployment_url"})

	rules, err := invoke(t, core, protectionRulesList.ID, "listed", `{"environment":"production"}`, false)
	if err != nil {
		t.Fatalf("rule list = %v", err)
	}
	assertDocumentedMembers(t, rules, schema(protectionRulesList.ID), []string{"custom_deployment_protection_rules",
		"id", "node_id", "enabled", "app", "slug", "links", "integration_url", "environment", "total_count"})
	rule, err := invoke(t, core, protectionRulesGet.ID, "listed", `{"environment":"production","protection_rule_id":3}`, false)
	if err != nil {
		t.Fatalf("rule get = %v", err)
	}
	assertDocumentedMembers(t, rule, schema(protectionRulesGet.ID), []string{"id", "node_id", "enabled", "app",
		"slug", "integration_url", "environment"})

	unconfirmed := &application.ConfirmationRequiredError{}
	before := len(d.requests())
	if _, err := invoke(t, core, protectionRulesDelete.ID, "listed", deploymentArguments[protectionRulesDelete.ID],
		false); !errors.As(err, &unconfirmed) || len(d.requests()) != before {
		t.Errorf("delete without confirmation = %v, want confirmation-required before GitHub", err)
	}
	deleted, err := invoke(t, core, protectionRulesDelete.ID, "listed", deploymentArguments[protectionRulesDelete.ID], true)
	if err != nil || string(deleted) !=
		`{"deleted":true,"environment":"production","protection_rule_id":3,"repository":"octo-org/example"}` {
		t.Fatalf("delete = %s, %v", deleted, err)
	}
	if sent := d.requests()[before:]; len(sent) != 2 || sent[0] != "GET "+protectionRulesRoute+"/3" ||
		sent[1] != "DELETE "+protectionRulesRoute+"/3" {
		t.Errorf("delete requests = %v, want one proof read and one DELETE", sent)
	}

	// A rule deleted already ends at the proof read, as not-found, without a DELETE.
	before = d.deletes()
	if _, err := invoke(t, core, protectionRulesDelete.ID, "listed", deploymentArguments[protectionRulesDelete.ID],
		true); classOf(err) != provider.ClassNotFound || !strings.Contains(err.Error(), "deployment protection rule 3 of environment production") {
		t.Errorf("repeated delete = %v, want not-found naming the rule", err)
	}
	if d.deletes() != before {
		t.Errorf("repeated delete sent a DELETE")
	}
}

// An answer that does not prove it belongs to the bound repository and the requested object is refused
// without showing any of it, and the delete sends nothing after such a proof read.
func TestDeploymentToolsRefuseAnswersOfAnotherRepositoryOrObject(t *testing.T) {
	for name, tt := range map[string]struct {
		set   func(*fakeDeployments)
		tools []string
	}{
		"another repository": {func(d *fakeDeployments) { d.foreignRepository = "https://ghe.example/api/v3/repos/octo-org/other" },
			[]string{deploymentsList.ID, deploymentsGet.ID, deploymentStatusesList.ID, deploymentStatusesGet.ID}},
		"another owner in the host spelling": {func(d *fakeDeployments) {
			d.foreignRepository = "https://ghe.example/api/v3/repos/evil/example"
		}, []string{deploymentsGet.ID, deploymentStatusesGet.ID}},
		"another deployment": {func(d *fakeDeployments) {
			d.foreignDeployment = deploymentAPI + "/deployments/43"
		}, []string{deploymentStatusesList.ID, deploymentStatusesGet.ID}},
		"another id": {func(d *fakeDeployments) { d.shiftID = 1 },
			[]string{deploymentsGet.ID, deploymentStatusesGet.ID, protectionRulesGet.ID, protectionRulesDelete.ID}},
	} {
		t.Run(name, func(t *testing.T) {
			_, d, base := serveDeployments(t)
			tt.set(d)
			red := &redact.Redactor{}
			core := application.New(registry(t), deploymentsConfig(base), resolver(red, nil), red)
			for _, id := range tt.tools {
				out, err := invoke(t, core, id, "listed", deploymentArguments[id], true)
				if classOf(err) != provider.ClassInvalidResponse || strings.Contains(err.Error(), "evil") ||
					strings.Contains(err.Error(), "octo-org/other") || out != nil {
					t.Errorf("%s = %s, %v, want an invalid response that names nothing foreign", id, out, err)
				}
			}
			if d.deletes() != 0 {
				t.Errorf("a refused proof read was followed by a DELETE")
			}
		})
	}
}

// A refusal names what the request needs, never GitHub's own text.
func TestDeploymentRefusalsNameTheCauseWithoutProviderText(t *testing.T) {
	_, d, base := serveDeployments(t)
	red := &redact.Redactor{}
	core := application.New(registry(t), deploymentsConfig(base), resolver(red, nil), red)
	answer := func(status int, method string) func(http.ResponseWriter, *http.Request) bool {
		return func(w http.ResponseWriter, r *http.Request) bool {
			if method != "" && r.Method != method {
				return false
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			fmt.Fprint(w, `{"message":"`+providerCanary+`","documentation_url":"https://docs.github.com/rest"}`)
			return true
		}
	}
	for _, tt := range []struct {
		name, id, method string
		status           int
		class            provider.Class
		want             string
	}{
		{"list without a permission", deploymentsList.ID, "", http.StatusForbidden, provider.ClassPermission, "Deployments: read"},
		{"get of a missing deployment", deploymentsGet.ID, "", http.StatusNotFound, provider.ClassNotFound, "deployment 42 in repository octo-org/example"},
		{"status list without a permission", deploymentStatusesList.ID, "", http.StatusForbidden, provider.ClassPermission, "Deployments: read"},
		{"status list of a missing deployment", deploymentStatusesList.ID, "", http.StatusNotFound, provider.ClassNotFound, "the statuses of deployment 42"},
		{"status get of a missing status", deploymentStatusesGet.ID, "", http.StatusNotFound, provider.ClassNotFound, "status 7 of deployment 42"},
		{"rule list without a permission", protectionRulesList.ID, "", http.StatusForbidden, provider.ClassPermission, "Actions: read"},
		{"rule list of a missing environment", protectionRulesList.ID, "", http.StatusNotFound, provider.ClassNotFound, "the deployment protection rules of environment production"},
		{"rule get of a missing rule", protectionRulesGet.ID, "", http.StatusNotFound, provider.ClassNotFound, "deployment protection rule 3 of environment production"},
		{"delete without a read permission", protectionRulesDelete.ID, "", http.StatusForbidden, provider.ClassPermission, "Actions: read"},
		{"delete without a change permission", protectionRulesDelete.ID, http.MethodDelete, http.StatusForbidden, provider.ClassPermission, "Administration: read and write"},
		{"delete of a rule gone meanwhile", protectionRulesDelete.ID, http.MethodDelete, http.StatusNotFound, provider.ClassNotFound, "deployment protection rule 3"},
	} {
		d.fail = answer(tt.status, tt.method)
		_, err := invoke(t, core, tt.id, "listed", deploymentArguments[tt.id], true)
		if classOf(err) != tt.class || strings.Contains(err.Error(), providerCanary) ||
			strings.Contains(err.Error(), "may have been applied") || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s = %v, want class %s naming %q without provider text", tt.name, err, tt.class, tt.want)
		}
	}
}

// After a server error, a gateway timeout, a dropped connection, or a client timeout, the delete reports that
// it may have been applied, and the DELETE is sent exactly once.
func TestDeploymentProtectionRuleDeleteIsNeverRepeatedAfterAnUnclearOutcome(t *testing.T) {
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
		t.Run(name, func(t *testing.T) {
			_, d, base := serveDeployments(t)
			d.fail = func(w http.ResponseWriter, r *http.Request) bool {
				if r.Method != http.MethodDelete {
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
			_, err := client(t, base, repoTarget).deleteProtectionRule(ctx,
				&environmentsArguments{Environment: "production", ProtectionRuleID: 3})
			if err == nil || !strings.Contains(err.Error(), "may have been applied") {
				t.Errorf("delete = %v, want an unclear outcome", err)
			}
			if d.deletes() != 1 {
				t.Errorf("delete sent %d DELETE requests, want exactly one", d.deletes())
			}
		})
	}
}

// A repository outside the targets and an excluded repository are refused before a secret is read or GitHub
// is contacted, by every tool, as are arguments out of bounds; without a tools list entry, the delete is not
// offered at all.
func TestDeploymentToolsRefuseForeignTargetsAndBadArgumentsBeforeIO(t *testing.T) {
	f, _, base := serveDeployments(t)
	reads := 0
	red := &redact.Redactor{}
	reg := registry(t)
	core := application.New(reg, deploymentsConfig(base), resolver(red, &reads), red)
	noIO := func(what string, call func() error, refused func(error) bool) {
		t.Helper()
		reads = 0
		before := len(f.recorded())
		if err := call(); !refused(err) {
			t.Errorf("%s = %v, want a refusal", what, err)
		}
		if reads != 0 || len(f.recorded()) != before {
			t.Errorf("%s reached the credential or GitHub", what)
		}
	}
	for _, id := range deploymentToolIDs {
		withRepository := `{"repository":"octo-org/example",` + strings.TrimPrefix(deploymentArguments[id], "{")
		if deploymentArguments[id] == `{}` {
			withRepository = `{"repository":"octo-org/example"}`
		}
		for _, connection := range []string{"foreign", "ex"} {
			noIO(id+" on "+connection, func() error {
				_, err := invoke(t, core, id, connection, withRepository, true)
				return err
			}, func(err error) bool { return err != nil })
		}
	}
	for id, arguments := range map[string][]string{
		deploymentsList.ID: {`{"sha":"a84d88e"}`, `{"ref":"../x"}`, `{"task":"deploy\u0001"}`,
			`{"environment":"` + strings.Repeat("e", 256) + `"}`, `{"cursor":"bogus"}`},
		deploymentsGet.ID:         {`{"deployment_id":0}`},
		deploymentStatusesGet.ID:  {`{"deployment_id":42,"status_id":0}`},
		protectionRulesGet.ID:     {`{"environment":"..","protection_rule_id":3}`, `{"environment":"production","protection_rule_id":0}`},
		protectionRulesDelete.ID:  {`{"environment":".","protection_rule_id":3}`},
		deploymentStatusesList.ID: {`{"deployment_id":-1}`},
	} {
		for _, argument := range arguments {
			noIO(id+" "+argument, func() error {
				_, err := invoke(t, core, id, "listed", argument, true)
				return err
			}, func(err error) bool { return err != nil })
		}
	}

	for _, id := range deploymentToolIDs {
		descriptor, _, _ := reg.Lookup(id)
		if descriptor.RequiresToolAllowList != (id == protectionRulesDelete.ID) {
			t.Errorf("%s requires an allow-list = %t", id, descriptor.RequiresToolAllowList)
		}
	}
	var unsupported *capability.UnsupportedError
	noIO("delete without a tools list entry", func() error {
		_, err := invoke(t, core, protectionRulesDelete.ID, "repo", deploymentArguments[protectionRulesDelete.ID], true)
		return err
	}, func(err error) bool { return errors.As(err, &unsupported) })
}

func TestDeploymentSubjectsNameOnlyCheckedNames(t *testing.T) {
	for path, want := range map[string]string{
		"deployments":               "the deployments",
		"deployments/42":            "deployment 42",
		"deployments/42/statuses":   "the statuses of deployment 42",
		"deployments/42/statuses/7": "status 7 of deployment 42",
		"deployments/42/statuses/x": "a status of deployment 42",
		"deployments/x":             "a deployment",
		"deployments/42/other":      "a resource of deployment 42",
		"deploymentsx":              "",
		"labels":                    "",
	} {
		if got := deploymentsSubject(path); got != want {
			t.Errorf("deploymentsSubject(%q) = %q, want %q", path, got, want)
		}
	}
	for path, want := range map[string]string{
		"environments/prod/deployment_protection_rules":   "the deployment protection rules of environment prod",
		"environments/prod/deployment_protection_rules/3": "deployment protection rule 3 of environment prod",
		"environments/prod/deployment_protection_rules/x": "a deployment protection rule of environment prod",
	} {
		if got := environmentsSubject(path); got != want {
			t.Errorf("environmentsSubject(%q) = %q, want %q", path, got, want)
		}
	}
}
