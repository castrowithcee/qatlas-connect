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
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// noEnvironmentRoute fails when any request of the fake server addressed an environment.
func noEnvironmentRoute(t *testing.T, f *fakeGitHub) {
	t.Helper()
	for _, request := range f.recorded() {
		if strings.Contains(request.path, "/environments/") {
			t.Errorf("a request reached an environment route: %s", request.path)
		}
	}
}

// Every Dependabot secret tool satisfies its contract at both levels, deletes with exactly one request to a
// fixed route, and no output carries a value a server adds.
func TestDependabotSecretsToolsSatisfyTheirContractThroughTheApplicationCore(t *testing.T) {
	f, s, base := serveSecrets(t)
	core := secretsCore(t, base, nil)
	for _, tt := range []struct {
		level, connection, target, located, existing, route string
	}{
		{"repository", "repo-listed", `"repository":"octo-org/example"`, `"repository":"octo-org/example"`,
			"REGISTRY_TOKEN", "/api/v3/repos/octo-org/example/dependabot/secrets"},
		{"organization", "org-listed", `"organization":"orgs/octo-org"`, `"organization":"orgs/octo-org"`,
			"SHARED_REGISTRY", "/api/v3/orgs/octo-org/dependabot/secrets"},
	} {
		listed, err := invoke(t, core, dependabotSecretsList.ID, tt.connection, `{`+tt.target+`}`, false)
		if err != nil || !strings.Contains(string(listed), `"name":"`+tt.existing+`"`) ||
			!strings.Contains(string(listed), `"total_count":1`) || !strings.Contains(string(listed), tt.located) ||
			strings.Contains(string(listed), secretValueCanary) || strings.Contains(string(listed), `"environment"`) {
			t.Fatalf("%s list = %s, %v", tt.level, listed, err)
		}
		got, err := invoke(t, core, dependabotSecretsGet.ID, tt.connection, `{`+tt.target+`,"name":"`+tt.existing+`"}`, false)
		if err != nil || !strings.Contains(string(got), `"created_at":"2026-02-02T03:04:05Z"`) ||
			!strings.Contains(string(got), `"updated_at":"2026-02-03T03:04:05Z"`) || !strings.Contains(string(got), tt.located) ||
			strings.Contains(string(got), secretValueCanary) || strings.Contains(string(got), `"value"`) ||
			strings.Contains(string(got), "encrypted_value") {
			t.Fatalf("%s get = %s, %v", tt.level, got, err)
		}
		if tt.level == "organization" && (!strings.Contains(string(got), `"visibility":"selected"`) ||
			!strings.Contains(string(got), `"selected_repositories_url":"https://api.github.com/orgs/`)) {
			t.Errorf("organization get = %s, want visibility and selected_repositories_url", got)
		}
		before := len(f.recorded())
		deleted, err := invoke(t, core, dependabotSecretsDelete.ID, tt.connection, `{`+tt.target+`,"name":"`+tt.existing+`"}`, true)
		if err != nil || !strings.Contains(string(deleted), `"deleted":true`) || !strings.Contains(string(deleted), tt.located) ||
			strings.Contains(string(deleted), secretValueCanary) {
			t.Fatalf("%s delete = %s, %v", tt.level, deleted, err)
		}
		if requests := f.recorded()[before:]; len(requests) != 1 || requests[0].method != http.MethodDelete ||
			requests[0].path != tt.route+"/"+tt.existing {
			t.Fatalf("%s delete requests = %+v, want one DELETE to %s", tt.level, requests, tt.route)
		}
	}
	if len(s.store["dependabot:orgs/octo-org"]) != 0 || len(s.store["dependabot:repos/octo-org/example"]) != 0 {
		t.Errorf("a Dependabot secret was not deleted")
	}
	// The Actions secrets are untouched.
	if len(s.store["orgs/octo-org"]) != 1 || len(s.store["repos/octo-org/example"]) != 1 {
		t.Errorf("an Actions secret changed")
	}
	noEnvironmentRoute(t, f)
}

// The repositories of an organization Dependabot secret lie inside the targets or are counted as withheld.
func TestDependabotSecretRepositoriesWithholdForeignRepositories(t *testing.T) {
	f, s, base := serveSecrets(t)
	core := secretsCore(t, base, nil)
	listed, err := invoke(t, core, dependabotSecretRepositoriesList.ID, "org-listed",
		`{"organization":"orgs/octo-org","name":"shared_registry"}`, false)
	if err != nil {
		t.Fatalf("list = %v", err)
	}
	var page ActionSecretRepositoryList
	if json.Unmarshal(listed, &page) != nil || len(page.Repositories) != 1 || page.Withheld != 2 ||
		page.TotalCount != 3 || *page.Repositories[0].FullName != "octo-org/example" {
		t.Fatalf("list = %s, want example listed and two withheld", listed)
	}
	for _, leak := range []string{secretValueCanary, "octo-org/secret", "other-org", "temp_clone_token"} {
		if strings.Contains(string(listed), leak) {
			t.Errorf("list leaks %q: %s", leak, listed)
		}
	}
	if requests := f.recorded(); requests[len(requests)-1].path != "/api/v3/orgs/octo-org/dependabot/secrets/shared_registry/repositories" {
		t.Errorf("path = %s", requests[len(requests)-1].path)
	}
	if _, err := invoke(t, core, dependabotSecretRepositoriesList.ID, "org-listed",
		`{"organization":"orgs/octo-org","name":"MISSING"}`, false); classOf(err) != provider.ClassNotFound {
		t.Errorf("missing secret = %v, want not-found", err)
	}
	if _, err := invoke(t, core, dependabotSecretRepositoriesList.ID, "org-listed",
		`{"repository":"octo-org/example","name":"SHARED_REGISTRY"}`, false); !isInvalidRequest(err) {
		t.Errorf("repository level = %v, want an invalid request", err)
	}
	first, err := invoke(t, core, dependabotSecretRepositoriesList.ID, "org-only",
		`{"organization":"orgs/octo-org","name":"SHARED_REGISTRY","limit":1}`, false)
	var paged struct {
		NextCursor string `json:"next_cursor"`
		HasMore    bool   `json:"has_more"`
	}
	if err != nil || json.Unmarshal(first, &paged) != nil || !paged.HasMore || paged.NextCursor == "" {
		t.Fatalf("first page = %s, %v", first, err)
	}
	// A cursor of the Actions family does not continue this list.
	s.put("dependabot:orgs/octo-org", map[string]any{"name": "SHARED_TOKEN", "visibility": "selected"})
	actions, err := invoke(t, core, actionSecretRepositoriesList.ID, "org-only",
		`{"organization":"orgs/octo-org","name":"SHARED_TOKEN","limit":1}`, false)
	var actionPaged struct {
		NextCursor string `json:"next_cursor"`
	}
	if err != nil || json.Unmarshal(actions, &actionPaged) != nil || actionPaged.NextCursor == "" {
		t.Fatalf("actions page = %s, %v", actions, err)
	}
	if _, err := invoke(t, core, dependabotSecretRepositoriesList.ID, "org-only",
		`{"organization":"orgs/octo-org","name":"SHARED_TOKEN","cursor":"`+actionPaged.NextCursor+`"}`, false); !isInvalidRequest(err) {
		t.Errorf("foreign cursor = %v, want an invalid request", err)
	}
}

// The Dependabot secret list is paged and its cursor is bound to the family.
func TestDependabotSecretsListPages(t *testing.T) {
	_, s, base := serveSecrets(t)
	for i := 0; i < 4; i++ {
		s.put("dependabot:repos/octo-org/example", map[string]any{"name": fmt.Sprintf("DEP_%d", i)})
		s.put("repos/octo-org/example", map[string]any{"name": fmt.Sprintf("ACT_%d", i)})
	}
	core := secretsCore(t, base, nil)
	first, err := invoke(t, core, dependabotSecretsList.ID, "repo", `{"repository":"octo-org/example","limit":2}`, false)
	var page struct {
		NextCursor string `json:"next_cursor"`
		HasMore    bool   `json:"has_more"`
		TotalCount int    `json:"total_count"`
	}
	if err != nil || json.Unmarshal(first, &page) != nil || !page.HasMore || page.TotalCount != 5 {
		t.Fatalf("first page = %s, %v", first, err)
	}
	second, err := invoke(t, core, dependabotSecretsList.ID, "repo",
		`{"repository":"octo-org/example","cursor":"`+page.NextCursor+`"}`, false)
	if err != nil || !strings.Contains(string(second), `"name":"DEP_1"`) {
		t.Fatalf("second page = %s, %v", second, err)
	}
	if _, err := invoke(t, core, actionSecretsList.ID, "repo",
		`{"repository":"octo-org/example","cursor":"`+page.NextCursor+`"}`, false); !isInvalidRequest(err) {
		t.Errorf("Dependabot cursor on the Actions list = %v, want an invalid request", err)
	}
}

// repository and organization are exclusive and Dependabot has no environment; every refusal, a target outside
// the connection, and an excluded target end before a credential is resolved.
func TestDependabotSecretsTargetIsExclusiveAndCheckedBeforeIO(t *testing.T) {
	f, _, base := serveSecrets(t)
	reads := 0
	core := secretsCore(t, base, &reads)
	for _, tt := range []struct {
		name, id, connection, arguments string
	}{
		{"neither", dependabotSecretsList.ID, "repo", `{}`},
		{"both", dependabotSecretsList.ID, "org-listed", `{"repository":"octo-org/example","organization":"orgs/octo-org"}`},
		{"both for the delete", dependabotSecretsDelete.ID, "org-listed",
			`{"repository":"octo-org/example","organization":"orgs/octo-org","name":"X"}`},
		{"an environment with a repository", dependabotSecretsList.ID, "repo-listed",
			`{"repository":"octo-org/example","environment":"production"}`},
		{"an environment for a get", dependabotSecretsGet.ID, "repo-listed",
			`{"repository":"octo-org/example","environment":"production","name":"REGISTRY_TOKEN"}`},
		{"an environment for the delete", dependabotSecretsDelete.ID, "repo-listed",
			`{"repository":"octo-org/example","environment":"production","name":"REGISTRY_TOKEN"}`},
		{"an empty environment", dependabotSecretsGet.ID, "repo-listed",
			`{"repository":"octo-org/example","environment":"","name":"REGISTRY_TOKEN"}`},
		{"an environment without a repository", dependabotSecretsList.ID, "repo", `{"environment":"production"}`},
		{"an environment with an organization", dependabotSecretsGet.ID, "org-listed",
			`{"organization":"orgs/octo-org","environment":"production","name":"X"}`},
		{"an organization outside the targets", dependabotSecretsList.ID, "repo", `{"organization":"orgs/octo-org"}`},
		{"a user owner", dependabotSecretsList.ID, "open-excluding", `{"organization":"users/octocat"}`},
		{"a repository outside the targets", dependabotSecretsGet.ID, "repo", `{"repository":"other-org/example","name":"X"}`},
		{"an excluded repository", dependabotSecretsList.ID, "org-listed", `{"repository":"octo-org/secret"}`},
		{"an excluded organization", dependabotSecretsDelete.ID, "open-excluding", `{"organization":"orgs/octo-org","name":"X"}`},
		{"a repository of an excluded organization", dependabotSecretsDelete.ID, "open-excluding",
			`{"repository":"octo-org/example","name":"X"}`},
		{"an organization outside the targets for its repositories", dependabotSecretRepositoriesList.ID, "repo",
			`{"organization":"orgs/octo-org","name":"X"}`},
		{"an excluded organization for its repositories", dependabotSecretRepositoriesList.ID, "open-excluding",
			`{"organization":"orgs/octo-org","name":"X"}`},
		{"an environment for its repositories", dependabotSecretRepositoriesList.ID, "org-listed",
			`{"organization":"orgs/octo-org","environment":"production","name":"X"}`},
	} {
		reads = 0
		before := len(f.recorded())
		if _, err := invoke(t, core, tt.id, tt.connection, tt.arguments, true); !isInvalidRequest(err) {
			t.Errorf("%s = %v, want an invalid request", tt.name, err)
		}
		if reads != 0 || len(f.recorded()) != before {
			t.Errorf("%s reached the credential or GitHub", tt.name)
		}
	}
	noEnvironmentRoute(t, f)
}

// Names follow GitHub's rules; every refusal ends before a credential is resolved and none quotes a value.
func TestDependabotSecretsArgumentsAreValidatedBeforeIO(t *testing.T) {
	f, _, base := serveSecrets(t)
	reads := 0
	core := secretsCore(t, base, &reads)
	repo := `"repository":"octo-org/example"`
	for _, tt := range []struct {
		name, id, arguments string
	}{
		{"a name starting with a digit", dependabotSecretsGet.ID, `{` + repo + `,"name":"1ABC"}`},
		{"a name with a slash", dependabotSecretsDelete.ID, `{` + repo + `,"name":"A/../B"}`},
		{"a name with a dot segment", dependabotSecretsDelete.ID, `{` + repo + `,"name":".."}`},
		{"the GITHUB_ prefix", dependabotSecretsGet.ID, `{` + repo + `,"name":"github_x"}`},
		{"an empty name", dependabotSecretsGet.ID, `{` + repo + `,"name":""}`},
		{"a value argument", dependabotSecretsGet.ID, `{` + repo + `,"name":"A","value":"` + secretValueCanary + `"}`},
		{"an encrypted value argument", dependabotSecretsDelete.ID,
			`{` + repo + `,"name":"A","encrypted_value":"` + secretValueCanary + `"}`},
		{"a name for the list", dependabotSecretsList.ID, `{` + repo + `,"name":"A"}`},
		{"a bad name for the repositories", dependabotSecretRepositoriesList.ID,
			`{"organization":"orgs/octo-org","name":"A/B"}`},
	} {
		reads = 0
		before := len(f.recorded())
		_, err := invoke(t, core, tt.id, "repo-listed", tt.arguments, true)
		if !isInvalidRequest(err) {
			t.Errorf("%s = %v, want an invalid request", tt.name, err)
		}
		if err != nil && strings.Contains(err.Error(), secretValueCanary) {
			t.Errorf("%s quotes the value: %v", tt.name, err)
		}
		if reads != 0 || len(f.recorded()) != before {
			t.Errorf("%s reached the credential or GitHub", tt.name)
		}
	}
}

// No Dependabot input schema has an environment member, and the descriptors name no environment.
func TestDependabotSecretDescriptorsHaveNoEnvironment(t *testing.T) {
	for _, d := range []capability.Descriptor{dependabotSecretsList, dependabotSecretsGet,
		dependabotSecretRepositoriesList, dependabotSecretsDelete} {
		if strings.Contains(string(d.InputSchema), "environment") || strings.Contains(string(d.OutputSchema), "environment") {
			t.Errorf("%s schema names an environment", d.ID)
		}
		for _, a := range d.Arguments {
			if a.Name == "environment" {
				t.Errorf("%s has an environment argument", d.ID)
			}
		}
		for _, f := range d.Fields {
			if f.Name == "environment" {
				t.Errorf("%s has an environment field", d.ID)
			}
		}
	}
	if got := dependabotSecretFamily.path(&Client{target: target{kind: kindRepository, owner: "o", repo: "r"}},
		"production", "A"); got != "/repos/o/r/dependabot/secrets/A" {
		t.Errorf("path with an environment = %s, want the Dependabot route", got)
	}
}

// A refusal names what the request needs in Qatlas' own words, with the Dependabot permission; GitHub's text
// never reaches the message, and a delete is sent once, also after a timeout.
func TestDependabotSecretsFailuresAreNamed(t *testing.T) {
	f, s, base := serveSecrets(t)
	core := secretsCore(t, base, nil)
	for _, level := range []struct{ name, connection, target, right string }{
		{"repository", "repo-listed", `"repository":"octo-org/example"`, "Dependabot secrets: read"},
		{"organization", "org-listed", `"organization":"orgs/octo-org"`, "admin:org"},
	} {
		for _, tool := range []struct{ id, arguments string }{
			{dependabotSecretsList.ID, `{` + level.target + `}`},
			{dependabotSecretsGet.ID, `{` + level.target + `,"name":"FRESH"}`},
			{dependabotSecretsDelete.ID, `{` + level.target + `,"name":"FRESH"}`},
		} {
			confirmed := tool.id == dependabotSecretsDelete.ID
			for _, tt := range []struct {
				status    int
				class     provider.Class
				fragment  string
				uncertain bool
			}{
				{http.StatusForbidden, provider.ClassPermission, level.right, false},
				{http.StatusNotFound, provider.ClassNotFound, "does not hold", false},
				{http.StatusUnprocessableEntity, "", "", false},
				{http.StatusGatewayTimeout, provider.ClassTimeout, "did not answer in time", confirmed},
			} {
				s.status = tt.status
				before := len(f.recorded())
				_, err := invoke(t, core, tool.id, level.connection, tool.arguments, confirmed)
				if err == nil || (tt.class != "" && classOf(err) != tt.class) || !strings.Contains(err.Error(), tt.fragment) ||
					strings.Contains(err.Error(), uncertain) != tt.uncertain {
					t.Errorf("%s %s answered %d = %v, want %s naming %q", level.name, tool.id, tt.status, err, tt.class, tt.fragment)
				}
				if err != nil && (strings.Contains(err.Error(), secretProviderCanary) || strings.Contains(err.Error(), "Actions")) {
					t.Errorf("%s %s answered %d quotes GitHub's text or names Actions: %v", level.name, tool.id, tt.status, err)
				}
				if requests := f.recorded()[before:]; len(requests) != 1 {
					t.Errorf("%s %s answered %d sent %d requests, want exactly one", level.name, tool.id, tt.status, len(requests))
				}
			}
			s.status = http.StatusForbidden
			_, err := invoke(t, core, tool.id, level.connection, tool.arguments, confirmed)
			if (tool.id == dependabotSecretsDelete.ID) != strings.Contains(err.Error(), "write") {
				t.Errorf("%s %s refused = %v, want write named for the delete only", level.name, tool.id, err)
			}
			s.status = 0
		}
	}
	noEnvironmentRoute(t, f)
}

// A 404 names the Dependabot secret, and never an environment secret as Dependabot.
func TestDependabotSecretsNotFoundNamesTheSecret(t *testing.T) {
	_, _, base := serveSecrets(t)
	core := secretsCore(t, base, nil)
	_, err := invoke(t, core, dependabotSecretsGet.ID, "repo", `{"repository":"octo-org/example","name":"FRESH"}`, false)
	if classOf(err) != provider.ClassNotFound ||
		!strings.Contains(err.Error(), "Dependabot secret FRESH in repository octo-org/example") ||
		strings.Contains(err.Error(), "environment") {
		t.Errorf("get = %v, want not-found naming the Dependabot secret", err)
	}
}

// The delete is offered only where a connection's tools list names it, and needs confirmation; both refusals
// end before a secret is read.
func TestDependabotSecretsDeleteIsListedOnlyAndNeedsConfirmation(t *testing.T) {
	f, _, base := serveSecrets(t)
	reads := 0
	core := secretsCore(t, base, &reads)
	var unsupported *capability.UnsupportedError
	if _, err := core.Describe(application.DescribeRequest{Operation: dependabotSecretsDelete.ID, Connection: "repo"}); !errors.As(err, &unsupported) {
		t.Errorf("describe delete on repo = %v, want unsupported", err)
	}
	if _, err := invoke(t, core, dependabotSecretsDelete.ID, "repo", `{"repository":"octo-org/example","name":"REGISTRY_TOKEN"}`, true); !errors.As(err, &unsupported) {
		t.Errorf("invoke delete on repo = %v, want unsupported", err)
	}
	unconfirmed := &application.ConfirmationRequiredError{}
	if _, err := invoke(t, core, dependabotSecretsDelete.ID, "repo-listed", `{"repository":"octo-org/example","name":"A"}`, false); !errors.As(err, &unconfirmed) {
		t.Errorf("delete without confirmation = %v, want confirmation-required", err)
	}
	if reads != 0 || len(f.recorded()) != 0 {
		t.Errorf("refused requests read %d credentials and sent %d requests", reads, len(f.recorded()))
	}
}

// A secret the server answers with a value or any other member the schema lacks never shows it in an output.
func TestDependabotSecretsDropAnyValueAMemberAdds(t *testing.T) {
	_, s, base := serveSecrets(t)
	s.put("dependabot:repos/octo-org/example", map[string]any{"name": "EXTRA", "key_id": "012345", "plaintext": secretValueCanary})
	core := secretsCore(t, base, nil)
	listed, err := invoke(t, core, dependabotSecretsList.ID, "repo", `{"repository":"octo-org/example"}`, false)
	got, err2 := invoke(t, core, dependabotSecretsGet.ID, "repo", `{"repository":"octo-org/example","name":"EXTRA"}`, false)
	if err != nil || err2 != nil {
		t.Fatalf("list = %v, get = %v", err, err2)
	}
	for _, output := range []string{string(listed), string(got)} {
		for _, leak := range []string{secretValueCanary, "encrypted_value", "key_id", "plaintext", `"value"`} {
			if strings.Contains(output, leak) {
				t.Errorf("output leaks %q: %s", leak, output)
			}
		}
	}
}

// A refusal of a Dependabot secret route names the secret by its checked name, and no environment path is
// named as Dependabot.
func TestDependabotSecretSubjectNamesOnlyCheckedNames(t *testing.T) {
	for path, want := range map[string]string{
		"dependabot/secrets":                     "the Dependabot secrets",
		"dependabot/secrets/DEPLOY":              "Dependabot secret DEPLOY",
		"dependabot/secrets/bad%2Fname":          "a secret",
		"dependabot/secrets/DEPLOY/repositories": "the repositories of Dependabot secret DEPLOY",
		"dependabot/secretsX":                    "",
		"actions/secrets/DEPLOY":                 "",
		"environments/prod/secrets":              "",
		"environments/prod/secrets/DEPLOY":       "",
	} {
		if got := dependabotSecretFamily.subject(path); got != want {
			t.Errorf("subject(%q) = %q, want %q", path, got, want)
		}
	}
	c := &Client{endpoints: endpoints{rest: "https://api.github.com"}}
	for url, want := range map[string]string{
		"https://api.github.com/orgs/octo-org/dependabot/secrets/SHARED/repositories": "the repositories of Dependabot secret SHARED of orgs/octo-org",
		"https://api.github.com/repos/octo-org/example/environments/prod/secrets/X":   "secret X of environment prod",
	} {
		request, _ := http.NewRequest(http.MethodGet, url, nil)
		if got := c.restSubject(request).String(); !strings.Contains(got, want) || strings.Contains(got, "Dependabot") != strings.Contains(want, "Dependabot") {
			t.Errorf("subject of %s = %q, want %q", url, got, want)
		}
	}
}
