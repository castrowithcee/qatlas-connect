package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const codeSecurityRoute = "/api/v3/orgs/octo-org/code-security/configurations"

// codeSecurityConfigurationJSON is a configuration with every member GitHub documents set, reviewers of the
// delegated bypass included.
func codeSecurityConfigurationJSON(id int, reviewers int) string {
	entries := make([]string, reviewers)
	for i := range entries {
		entries[i] = fmt.Sprintf(`{"reviewer_id":%d,"reviewer_type":"TEAM","mode":"ALWAYS","security_configuration_id":%d}`, i+1, id)
	}
	return fmt.Sprintf(`{"id":%d,"name":"Baseline","target_type":"organization","description":"Strict set",`+
		`"advanced_security":"code_security","dependency_graph":"enabled","dependency_graph_autosubmit_action":"enabled",`+
		`"dependency_graph_autosubmit_action_options":{"labeled_runners":true},"dependabot_alerts":"enabled",`+
		`"dependabot_reachability":"disabled","dependabot_security_updates":"enabled",`+
		`"dependabot_delegated_alert_dismissal":"disabled","code_scanning_options":{"allow_advanced":false},`+
		`"code_scanning_default_setup":"enabled","code_scanning_default_setup_options":{"runner_type":"labeled",`+
		`"runner_label":"ubuntu-scan"},"code_scanning_delegated_alert_dismissal":"not_set","secret_scanning":"enabled",`+
		`"secret_scanning_push_protection":"enabled","secret_scanning_delegated_bypass":"enabled",`+
		`"secret_scanning_delegated_bypass_options":{"reviewers":[%s]},"secret_scanning_validity_checks":"enabled",`+
		`"secret_scanning_non_provider_patterns":"disabled","secret_scanning_generic_secrets":"enabled",`+
		`"secret_scanning_generic_secrets_push_protection":"enabled","secret_scanning_delegated_alert_dismissal":"disabled",`+
		`"secret_scanning_extended_metadata":"not_set","private_vulnerability_reporting":"enabled","enforcement":"enforced",`+
		`"url":"https://api.github.com/orgs/octo-org/code-security/configurations/%d",`+
		`"html_url":"https://github.com/organizations/octo-org/settings/security_analysis/configurations/%d",`+
		`"created_at":"2026-10-01T00:00:00Z","updated_at":"2026-10-02T00:00:00Z"}`, id, strings.Join(entries, ","), id, id)
}

func codeSecurityRepositoryJSON(fullName string, id int) string {
	name := fullName[strings.LastIndex(fullName, "/")+1:]
	return fmt.Sprintf(`{"status":"attached","repository":{"id":%d,"node_id":"R_%d","name":%q,"full_name":%q,`+
		`"private":true,"html_url":"https://github.com/%s","description":"d","fork":false,`+
		`"url":"https://api.github.com/repos/%s","owner":{"login":"octo-org","id":1,"type":"Organization",`+
		`"node_id":"O_1","avatar_url":"https://x/a","gravatar_id":"","url":"https://x/u","html_url":"https://x/h",`+
		`"followers_url":"","following_url":"","gists_url":"","starred_url":"","subscriptions_url":"",`+
		`"organizations_url":"","repos_url":"","events_url":"","received_events_url":"","site_admin":false}}}`,
		id, id, name, fullName, fullName, fullName)
}

// codeSecurityFake answers the four routes. The ids 403, 404, and 504 answer the matching refusals with
// provider text that must never reach a message.
type codeSecurityFake struct{ repositories string }

func (c *codeSecurityFake) route(w http.ResponseWriter, r *http.Request) bool {
	path := r.URL.Path
	if !strings.HasPrefix(path, codeSecurityRoute) {
		return false
	}
	rest := strings.TrimPrefix(strings.TrimPrefix(path, codeSecurityRoute), "/")
	w.Header().Set("Content-Type", "application/json")
	id, tail, _ := strings.Cut(rest, "/")
	switch id {
	case "403":
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"message":"Resource not accessible `+providerSecret+`"}`)
		return true
	case "404":
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"message":"Not Found `+providerSecret+`"}`)
		return true
	case "504":
		w.WriteHeader(http.StatusGatewayTimeout)
		return true
	}
	switch {
	case rest == "":
		if r.URL.Query().Get("after") == "" {
			w.Header().Set("Link", `<https://api.github.com/x?per_page=1&after=Y3Vyc29yOnYy>; rel="next"`)
		}
		fmt.Fprint(w, "["+codeSecurityConfigurationJSON(12, 1)+"]")
	case rest == "defaults":
		fmt.Fprint(w, `[{"default_for_new_repos":"public","configuration":`+codeSecurityConfigurationJSON(12, 1)+`},`+
			`{"default_for_new_repos":"private_and_internal","configuration":`+codeSecurityConfigurationJSON(13, 1)+`}]`)
	case tail == "repositories":
		if r.URL.Query().Get("after") == "" {
			w.Header().Set("Link", `<https://api.github.com/x?per_page=1&after=Y3Vyc29yOnYy>; rel="next"`)
		}
		fmt.Fprint(w, c.repositories)
	case id == "14":
		fmt.Fprint(w, codeSecurityConfigurationJSON(14, 101))
	default:
		fmt.Fprint(w, codeSecurityConfigurationJSON(12, 2))
	}
	return true
}

func serveCodeSecurity(t *testing.T) (*fakeGitHub, *application.Core, *int) {
	t.Helper()
	fake := &codeSecurityFake{repositories: "[" + strings.Join([]string{
		codeSecurityRepositoryJSON("octo-org/open", 1),
		codeSecurityRepositoryJSON("Octo-Org/Secret", 2),
		`{"status":"failed","repository":{"id":3,"full_name":"odd"}}`,
	}, ",") + "]"}
	f := &fakeGitHub{failure: fake.route}
	cfg := discoveryConfig(serve(t, f))
	add := func(name string, targets, excludes []string) {
		cfg.Connections[name] = config.Connection{Service: "gh", Credential: "gh-reader", Effects: config.Effects(),
			Targets: targets, Exclude: excludes}
	}
	add("ex-repo", []string{"orgs/octo-org"}, []string{"repos/octo-org/secret"})
	add("ex-owner", nil, []string{"users/octo-org"})
	add("repo-only", []string{"repos/octo-org/open"}, nil)
	reads := 0
	red := &redact.Redactor{}
	return f, application.New(registry(t), cfg, resolver(red, &reads), red), &reads
}

func TestCodeSecurityToolsAnswerEveryDocumentedMember(t *testing.T) {
	f, core, _ := serveCodeSecurity(t)
	// Every documented member of a configuration reaches the answer of the detail read.
	got, err := invoke(t, core, codeSecurityConfigurationsGet.ID, "org", `{"configuration_id":12}`, false)
	var detail map[string]any
	if err != nil || json.Unmarshal(got, &detail) != nil {
		t.Fatalf("get = %s, %v", got, err)
	}
	for _, field := range codeSecurityConfigurationMembers {
		if _, ok := detail[field.Name]; !ok {
			t.Errorf("get lacks the member %s: %s", field.Name, got)
		}
	}
	if detail["owner"] != "orgs/octo-org" {
		t.Errorf("owner = %v, want the organization the read acted on", detail["owner"])
	}
	for _, want := range []string{`"labeled_runners":true`, `"allow_advanced":false`, `"runner_label":"ubuntu-scan"`,
		`"reviewer_type":"TEAM"`, `"mode":"ALWAYS"`, `"security_configuration_id":12`} {
		if !strings.Contains(string(got), want) {
			t.Errorf("get lacks %s: %s", want, got)
		}
	}

	got, err = invoke(t, core, codeSecurityConfigurationsList.ID, "org", `{"target_type":"global","limit":1}`, false)
	var list struct {
		Configurations []map[string]any `json:"configurations"`
		HasMore        bool             `json:"has_more"`
		NextCursor     string           `json:"next_cursor"`
	}
	if err != nil || json.Unmarshal(got, &list) != nil || len(list.Configurations) != 1 || !list.HasMore {
		t.Fatalf("list = %s, %v", got, err)
	}
	for _, field := range codeSecurityConfigurationMembers {
		if _, ok := list.Configurations[0][field.Name]; !ok {
			t.Errorf("list entry lacks the member %s", field.Name)
		}
	}

	got, err = invoke(t, core, codeSecurityDefaultsGet.ID, "org", `{}`, false)
	if err != nil || strings.Count(string(got), `"default_for_new_repos"`) != 2 ||
		!strings.Contains(string(got), `"default_for_new_repos":"private_and_internal"`) ||
		strings.Count(string(got), `"advanced_security":"code_security"`) != 2 {
		t.Errorf("defaults = %s, %v", got, err)
	}

	got, err = invoke(t, core, codeSecurityConfigurationRepositoriesList.ID, "org",
		`{"configuration_id":12,"status":["attached","failed","enforced"],"limit":50}`, false)
	if err != nil || !strings.Contains(string(got), `"full_name":"octo-org/open"`) ||
		!strings.Contains(string(got), `"status":"attached"`) {
		t.Errorf("repositories = %s, %v", got, err)
	}

	var joined []string
	for _, r := range f.recorded() {
		joined = append(joined, r.path+"?"+r.query)
	}
	all := strings.Join(joined, "\n")
	for _, want := range []string{codeSecurityRoute + "?", "target_type=global", "per_page=1", codeSecurityRoute + "/12?",
		codeSecurityRoute + "/defaults?", codeSecurityRoute + "/12/repositories?", "status=attached%2Cfailed%2Cenforced",
		"per_page=50"} {
		if !strings.Contains(all, want) {
			t.Errorf("requests lack %q:\n%s", want, all)
		}
	}
	for _, r := range f.recorded() {
		if r.method != http.MethodGet {
			t.Errorf("request %s %s is not a read", r.method, r.path)
		}
	}
	// Reviewers beyond the cap are cut and say so.
	got, _ = invoke(t, core, codeSecurityConfigurationsGet.ID, "org", `{"configuration_id":14}`, false)
	var cut struct {
		Options struct {
			Reviewers []any `json:"reviewers"`
			Truncated bool  `json:"reviewers_truncated"`
		} `json:"secret_scanning_delegated_bypass_options"`
	}
	if json.Unmarshal(got, &cut) != nil || len(cut.Options.Reviewers) != projectionListLimit || !cut.Options.Truncated {
		t.Errorf("reviewers = %d, truncated %v; want %d and a visible cut", len(cut.Options.Reviewers),
			cut.Options.Truncated, projectionListLimit)
	}
}

func TestCodeSecurityListsPageByCursor(t *testing.T) {
	f, core, _ := serveCodeSecurity(t)
	for _, tt := range []struct{ id, arguments, filters string }{
		{codeSecurityConfigurationsList.ID, `{"limit":1}`, ``},
		{codeSecurityConfigurationRepositoriesList.ID, `{"configuration_id":12,"limit":1,"status":["failed"]}`,
			`"configuration_id":12,"status":["failed"],`},
	} {
		first, err := invoke(t, core, tt.id, "org", tt.arguments, false)
		var page struct {
			HasMore    bool   `json:"has_more"`
			NextCursor string `json:"next_cursor"`
		}
		if err != nil || json.Unmarshal(first, &page) != nil || !page.HasMore || page.NextCursor == "" {
			t.Fatalf("%s first = %s, %v", tt.id, first, err)
		}
		for name, cursor := range alteredCursors(t, page.NextCursor) {
			if _, err := invoke(t, core, tt.id, "org", `{"configuration_id":12,"cursor":"`+cursor+`"}`, false); !isInvalidRequest(err) {
				t.Errorf("%s with %s = %v, want invalid request", tt.id, name, err)
			}
		}
		if _, err := invoke(t, core, tt.id, "org", `{"configuration_id":12,"status":["attached"],"target_type":"global","cursor":"`+
			page.NextCursor+`"}`, false); !isInvalidRequest(err) {
			t.Errorf("%s cursor with other filters = %v, want invalid request", tt.id, err)
		}
		if _, err := invoke(t, core, tt.id, "open", `{"owner":"orgs/other","configuration_id":12,"cursor":"`+
			page.NextCursor+`"}`, false); !isInvalidRequest(err) {
			t.Errorf("%s cursor of another organization = %v, want invalid request", tt.id, err)
		}
		before := len(f.recorded())
		args := strings.TrimSuffix(strings.TrimPrefix(tt.arguments, "{"), "}")
		args = strings.Replace(args, `"limit":1`, `"cursor":"`+page.NextCursor+`"`, 1)
		next, err := invoke(t, core, tt.id, "org", "{"+args+"}", false)
		if err != nil || strings.Contains(string(next), `"has_more":true`) {
			t.Errorf("%s continuation = %s, %v; want the last batch", tt.id, next, err)
		}
		recorded := f.recorded()
		last := recorded[len(recorded)-1].query
		if len(recorded) != before+1 || !strings.Contains(last, "after=Y3Vyc29yOnYy") || !strings.Contains(last, "per_page=1") {
			t.Errorf("%s continuation query = %q, want the after cursor and the first batch size", tt.id, last)
		}
	}
}

func TestCodeSecurityArgumentsAndTargetsAreCheckedBeforeIO(t *testing.T) {
	f, core, reads := serveCodeSecurity(t)
	for _, tt := range []struct{ id, connection, arguments string }{
		// The owner must be an organization the targets name as an owner.
		{codeSecurityConfigurationsList.ID, "open", `{}`},
		{codeSecurityConfigurationsList.ID, "user", `{}`},
		{codeSecurityConfigurationsList.ID, "open", `{"owner":"users/octocat"}`},
		{codeSecurityConfigurationsList.ID, "org", `{"owner":"users/octo-org"}`},
		{codeSecurityConfigurationsList.ID, "org", `{"owner":"orgs/other"}`},
		{codeSecurityConfigurationsList.ID, "org", `{"owner":"octo-org"}`},
		{codeSecurityConfigurationsList.ID, "starrepo", `{}`},
		{codeSecurityConfigurationsList.ID, "repo-only", `{}`},
		{codeSecurityConfigurationsList.ID, "planning", `{}`},
		{codeSecurityConfigurationsList.ID, "ex-owner", `{"owner":"orgs/octo-org"}`},
		{codeSecurityConfigurationsGet.ID, "user", `{"configuration_id":12}`},
		{codeSecurityConfigurationsGet.ID, "org", `{"owner":"orgs/other","configuration_id":12}`},
		{codeSecurityConfigurationsGet.ID, "ex-owner", `{"owner":"orgs/octo-org","configuration_id":12}`},
		{codeSecurityDefaultsGet.ID, "open", `{"owner":"users/octocat"}`},
		{codeSecurityDefaultsGet.ID, "org", `{"owner":"orgs/other"}`},
		{codeSecurityDefaultsGet.ID, "repo-only", `{}`},
		{codeSecurityConfigurationRepositoriesList.ID, "user", `{"configuration_id":12}`},
		{codeSecurityConfigurationRepositoriesList.ID, "org", `{"owner":"orgs/other","configuration_id":12}`},
		{codeSecurityConfigurationRepositoriesList.ID, "ex-owner", `{"owner":"orgs/octo-org","configuration_id":12}`},
		// Malformed arguments.
		{codeSecurityConfigurationsList.ID, "org", `{"target_type":"organization"}`},
		{codeSecurityConfigurationsList.ID, "org", `{"limit":0}`},
		{codeSecurityConfigurationsGet.ID, "org", `{}`},
		{codeSecurityConfigurationsGet.ID, "org", `{"configuration_id":0}`},
		{codeSecurityConfigurationsGet.ID, "org", `{"configuration_id":-3}`},
		{codeSecurityConfigurationsGet.ID, "org", `{"configuration_id":"12"}`},
		{codeSecurityConfigurationRepositoriesList.ID, "org", `{}`},
		{codeSecurityConfigurationRepositoriesList.ID, "org", `{"configuration_id":12,"status":["all"]}`},
		{codeSecurityConfigurationRepositoriesList.ID, "org", `{"configuration_id":12,"status":["weird"]}`},
		{codeSecurityConfigurationRepositoriesList.ID, "org", `{"configuration_id":12,"status":["failed","failed"]}`},
		{codeSecurityConfigurationRepositoriesList.ID, "org", `{"configuration_id":12,"status":[]}`},
		{codeSecurityConfigurationRepositoriesList.ID, "org", `{"configuration_id":12,"status":"failed"}`},
		{codeSecurityConfigurationRepositoriesList.ID, "org", `{"configuration_id":12,"status":["removed","detached"]}`},
		{codeSecurityConfigurationRepositoriesList.ID, "org", `{"configuration_id":12,"status":["detached","removed"]}`},
		{codeSecurityConfigurationRepositoriesList.ID, "org", `{"configuration_id":12,"limit":101}`},
	} {
		*reads = 0
		before := len(f.recorded())
		if _, err := invoke(t, core, tt.id, tt.connection, tt.arguments, true); !isInvalidRequest(err) {
			t.Errorf("%s(%s) on %s = %v, want an invalid request", tt.id, tt.arguments, tt.connection, err)
		}
		if *reads != 0 || len(f.recorded()) != before {
			t.Errorf("%s(%s) on %s reached the credential or GitHub", tt.id, tt.arguments, tt.connection)
		}
	}
	// A connection that names the organization, or none at all, reaches the tools; the configured spelling of
	// the owner keeps its case.
	for _, connection := range []string{"open", "org", "ex-repo"} {
		if _, err := invoke(t, core, codeSecurityConfigurationsList.ID, connection, `{"owner":"orgs/octo-org"}`, false); err != nil {
			t.Errorf("list on %s = %v", connection, err)
		}
	}
}

func TestCodeSecurityRepositoriesOutsideTheTargetsAreOnlyCounted(t *testing.T) {
	_, core, _ := serveCodeSecurity(t)
	read := func(connection string) (listed []string, withheld int, raw string) {
		t.Helper()
		out, err := invoke(t, core, codeSecurityConfigurationRepositoriesList.ID, connection,
			`{"owner":"orgs/octo-org","configuration_id":12}`, false)
		if err != nil {
			t.Fatalf("list on %s = %v", connection, err)
		}
		var page struct {
			Repositories []struct {
				Repository struct {
					FullName string `json:"full_name"`
				} `json:"repository"`
			} `json:"repositories"`
			Withheld int `json:"withheld"`
		}
		if json.Unmarshal(out, &page) != nil {
			t.Fatalf("answer = %s", out)
		}
		for _, entry := range page.Repositories {
			listed = append(listed, entry.Repository.FullName)
		}
		return listed, page.Withheld, string(out)
	}
	// A repository whose name cannot be checked is outside the targets on every connection.
	listed, withheld, raw := read("org")
	if len(listed) != 2 || withheld != 1 || strings.Contains(raw, `"odd"`) {
		t.Errorf("org: listed %v, withheld %d: %s", listed, withheld, raw)
	}
	listed, withheld, _ = read("open")
	if len(listed) != 2 || withheld != 1 {
		t.Errorf("open: listed %v, withheld %d", listed, withheld)
	}
	// An excluded repository is counted and its identifiers never shown.
	listed, withheld, raw = read("ex-repo")
	if len(listed) != 1 || listed[0] != "octo-org/open" || withheld != 2 ||
		strings.Contains(raw, "Secret") || strings.Contains(raw, `"id":2`) || strings.Contains(raw, "R_2") {
		t.Errorf("ex-repo: listed %v, withheld %d: %s", listed, withheld, raw)
	}
	// The answer of a batch without withheld repositories leaves the count out.
	out, err := invoke(t, core, codeSecurityConfigurationsList.ID, "ex-repo", `{"owner":"orgs/octo-org"}`, false)
	if err != nil || strings.Contains(string(out), "withheld") {
		t.Errorf("list = %s, %v; want no withheld member", out, err)
	}
}

func TestCodeSecurityRefusalsAndTimeoutsAreResultsWithoutProviderText(t *testing.T) {
	f, core, _ := serveCodeSecurity(t)
	for _, tt := range []struct{ id, arguments string }{
		{codeSecurityConfigurationsGet.ID, `%d`},
		{codeSecurityConfigurationRepositoriesList.ID, `%d`},
	} {
		for status, class := range map[int]provider.Class{403: provider.ClassPermission, 404: provider.ClassNotFound,
			504: provider.ClassTimeout} {
			before := len(f.recorded())
			_, err := invoke(t, core, tt.id, "org", fmt.Sprintf(`{"configuration_id":`+tt.arguments+`}`, status), false)
			if classOf(err) != class || strings.Contains(err.Error(), providerSecret) || len(f.recorded()) != before+1 {
				t.Errorf("%s with %d = %v after %d requests, want one %s without provider text", tt.id, status, err,
					len(f.recorded())-before, class)
			}
			if class == provider.ClassPermission && !strings.Contains(err.Error(), "read:org") {
				t.Errorf("%s with %d = %v, want the permission named", tt.id, status, err)
			}
			if class == provider.ClassNotFound && !strings.Contains(err.Error(), "code security configuration") {
				t.Errorf("%s with %d = %v, want the missing object named", tt.id, status, err)
			}
		}
	}
	// The list and the defaults answer the same refusals through the organization route.
	for _, id := range []string{codeSecurityConfigurationsList.ID, codeSecurityDefaultsGet.ID} {
		f.failure = func(w http.ResponseWriter, r *http.Request) bool {
			if !strings.HasPrefix(r.URL.Path, codeSecurityRoute) {
				return false
			}
			w.WriteHeader(http.StatusGatewayTimeout)
			return true
		}
		before := len(f.recorded())
		_, err := invoke(t, core, id, "org", `{}`, false)
		if classOf(err) != provider.ClassTimeout || len(f.recorded()) != before+1 {
			t.Errorf("%s on a gateway timeout = %v after %d requests, want one timeout", id, err, len(f.recorded())-before)
		}
		f.failure = func(w http.ResponseWriter, r *http.Request) bool {
			if !strings.HasPrefix(r.URL.Path, codeSecurityRoute) {
				return false
			}
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"message":"`+providerSecret+`"}`)
			return true
		}
		_, err = invoke(t, core, id, "org", `{}`, false)
		if classOf(err) != provider.ClassPermission || strings.Contains(err.Error(), providerSecret) {
			t.Errorf("%s on a refusal = %v, want a permission without provider text", id, err)
		}
	}
	// A client-side deadline is a timeout after exactly one request.
	f.failure = func(w http.ResponseWriter, r *http.Request) bool {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	before := len(f.recorded())
	_, err := core.Invoke(ctx, application.InvokeRequest{Operation: codeSecurityConfigurationsGet.ID, Connection: "org",
		Arguments: json.RawMessage(`{"configuration_id":12}`)})
	if classOf(err) != provider.ClassTimeout || len(f.recorded()) != before+1 {
		t.Errorf("deadline = %v after %d requests, want one timeout", err, len(f.recorded())-before)
	}
}

func TestCodeSecuritySubjectNamesOnlyCheckedParts(t *testing.T) {
	for _, tt := range []struct {
		rest []string
		want string
	}{
		{nil, "the code security configurations"},
		{[]string{"defaults"}, "the default code security configurations"},
		{[]string{"12"}, "code security configuration 12"},
		{[]string{"12/repositories"}, "the repositories of code security configuration 12"},
		{[]string{"x<script>"}, "the code security configurations"},
		{[]string{"0"}, "the code security configurations"},
	} {
		if got := codeSecuritySubject(tt.rest); got != tt.want {
			t.Errorf("codeSecuritySubject(%v) = %q, want %q", tt.rest, got, tt.want)
		}
	}
}
