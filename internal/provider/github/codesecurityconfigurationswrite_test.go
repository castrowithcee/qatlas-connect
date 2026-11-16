package github

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// codeSecurityDocumentedSettings are the members of the create and update bodies in GitHub's REST description
// 1.1.4, name aside.
var codeSecurityDocumentedSettings = []string{"advanced_security", "code_scanning_default_setup",
	"code_scanning_default_setup_options", "code_scanning_delegated_alert_dismissal", "code_scanning_options",
	"code_security", "dependabot_alerts", "dependabot_delegated_alert_dismissal", "dependabot_reachability",
	"dependabot_security_updates", "dependency_graph", "dependency_graph_autosubmit_action",
	"dependency_graph_autosubmit_action_options", "description", "enforcement", "private_vulnerability_reporting",
	"secret_protection", "secret_scanning", "secret_scanning_delegated_alert_dismissal",
	"secret_scanning_delegated_bypass", "secret_scanning_delegated_bypass_options",
	"secret_scanning_extended_metadata", "secret_scanning_generic_secrets",
	"secret_scanning_generic_secrets_push_protection", "secret_scanning_non_provider_patterns",
	"secret_scanning_push_protection", "secret_scanning_validity_checks"}

// codeSecurityAllSettings sets every documented setting, each state alternating so a swapped field shows.
func codeSecurityAllSettings() map[string]any {
	settings := map[string]any{"name": "Strict", "description": "All settings", "advanced_security": "code_security",
		"enforcement": "unenforced",
		"dependency_graph_autosubmit_action_options": map[string]any{"labeled_runners": true},
		"code_scanning_options":                      map[string]any{"allow_advanced": false},
		"code_scanning_default_setup_options":        map[string]any{"runner_type": "labeled", "runner_label": "ubuntu-scan"},
		"secret_scanning_delegated_bypass_options": map[string]any{"reviewers": []any{
			map[string]any{"reviewer_id": 7, "reviewer_type": "TEAM", "mode": "EXEMPT"},
			map[string]any{"reviewer_id": 9, "reviewer_type": "ROLE"}}},
	}
	states := []string{"enabled", "disabled", "not_set"}
	for i, setting := range codeSecurityStateSettings {
		settings[setting.name] = states[i%len(states)]
	}
	return settings
}

func codeSecurityChangeCore(t *testing.T, failure func(http.ResponseWriter, *http.Request) bool) (*fakeGitHub, *application.Core, *int) {
	t.Helper()
	f := &fakeGitHub{failure: failure}
	cfg := discoveryConfig(serve(t, f))
	add := func(name string, targets, excludes []string, tools []string) {
		cfg.Connections[name] = config.Connection{Service: "gh", Credential: "gh-reader", Effects: config.Effects(),
			Targets: targets, Exclude: excludes, Tools: tools}
	}
	add("w-org", []string{"orgs/octo-org"}, nil, codeSecurityChangeToolIDs)
	add("w-open", nil, nil, codeSecurityChangeToolIDs)
	add("w-user", []string{"users/octocat"}, nil, codeSecurityChangeToolIDs)
	add("w-repo", []string{"repos/octo-org/open"}, nil, codeSecurityChangeToolIDs)
	add("w-ex-owner", nil, []string{"orgs/octo-org"}, codeSecurityChangeToolIDs)
	add("w-narrow", []string{"orgs/octo-org", "repos/octo-org/*"}, []string{"repos/octo-org/secret"}, codeSecurityChangeToolIDs)
	reads := 0
	red := &redact.Redactor{}
	return f, application.New(registry(t), cfg, resolver(red, &reads), red), &reads
}

func codeSecurityAnswer(w http.ResponseWriter, r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, codeSecurityRoute) {
		return false
	}
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.Method == http.MethodPost:
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, codeSecurityConfigurationJSON(21, 1))
	case r.Method == http.MethodPatch && strings.HasSuffix(r.URL.Path, "/13"):
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPatch:
		fmt.Fprint(w, codeSecurityConfigurationJSON(12, 1))
	default:
		w.WriteHeader(http.StatusNoContent)
	}
	return true
}

func TestCodeSecurityChangesSendEveryDocumentedSettingExactlyOnce(t *testing.T) {
	// The settings the tools take are exactly the documented ones.
	declared := map[string]bool{}
	for _, name := range []string{"name"} {
		declared[name] = true
	}
	for _, descriptor := range []capability.Descriptor{codeSecurityConfigurationsCreate, codeSecurityConfigurationsUpdate} {
		var schema struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if json.Unmarshal(descriptor.InputSchema, &schema) != nil {
			t.Fatal("input schema is not JSON")
		}
		var got []string
		for name := range schema.Properties {
			if name != "name" && name != "configuration_id" {
				got = append(got, name)
			}
		}
		sort.Strings(got)
		if !reflect.DeepEqual(got, codeSecurityDocumentedSettings) {
			t.Errorf("%s offers %v, want the documented %v", descriptor.ID, got, codeSecurityDocumentedSettings)
		}
		var arguments []string
		for _, argument := range descriptor.Arguments {
			if argument.Name != "name" && argument.Name != "configuration_id" {
				arguments = append(arguments, argument.Name)
			}
		}
		sort.Strings(arguments)
		if !reflect.DeepEqual(arguments, codeSecurityDocumentedSettings) {
			t.Errorf("%s describes %v, want the documented settings", descriptor.ID, arguments)
		}
	}

	f, core, _ := codeSecurityChangeCore(t, codeSecurityAnswer)
	settings := codeSecurityAllSettings()
	body := func(extra map[string]any) string {
		all := map[string]any{"owner": "orgs/octo-org"}
		for k, v := range settings {
			all[k] = v
		}
		for k, v := range extra {
			if v == nil {
				delete(all, k)
				continue
			}
			all[k] = v
		}
		encoded, _ := json.Marshal(all)
		return string(encoded)
	}
	check := func(label, method, path string, before int, want map[string]any) {
		t.Helper()
		requests := f.recorded()[before:]
		if len(requests) != 1 || requests[0].method != method || requests[0].path != path {
			t.Fatalf("%s sent %+v, want one %s %s", label, requests, method, path)
		}
		if want != nil && !reflect.DeepEqual(requests[0].body, want) {
			t.Errorf("%s body = %v, want %v", label, requests[0].body, want)
		}
	}
	// JSON round trip gives the numbers the recorder decodes.
	var want map[string]any
	encoded, _ := json.Marshal(settings)
	_ = json.Unmarshal(encoded, &want)

	before := len(f.recorded())
	got, err := invoke(t, core, codeSecurityConfigurationsCreate.ID, "w-org", body(nil), true)
	if err != nil || !strings.Contains(string(got), `"id":21`) || !strings.Contains(string(got), `"owner":"orgs/octo-org"`) {
		t.Fatalf("create = %s, %v", got, err)
	}
	check("create", http.MethodPost, codeSecurityRoute, before, want)

	delete(want, "name")
	before = len(f.recorded())
	got, err = invoke(t, core, codeSecurityConfigurationsUpdate.ID, "w-org",
		body(map[string]any{"configuration_id": 12, "name": nil}), true)
	if err != nil || !strings.Contains(string(got), `"updated":true`) || !strings.Contains(string(got), `"id":12`) {
		t.Fatalf("update = %s, %v", got, err)
	}
	check("update", http.MethodPatch, codeSecurityRoute+"/12", before, want)

	// Only the given settings are sent, an empty description included.
	before = len(f.recorded())
	if _, err := invoke(t, core, codeSecurityConfigurationsUpdate.ID, "w-org",
		`{"owner":"orgs/octo-org","configuration_id":12,"description":""}`, true); err != nil {
		t.Fatal(err)
	}
	check("update one", http.MethodPatch, codeSecurityRoute+"/12", before, map[string]any{"description": ""})

	// GitHub's 204 says nothing new was applied.
	got, err = invoke(t, core, codeSecurityConfigurationsUpdate.ID, "w-org",
		`{"owner":"orgs/octo-org","configuration_id":13,"dependency_graph":"enabled"}`, true)
	if err != nil || !strings.Contains(string(got), `"updated":false`) || strings.Contains(string(got), "configuration\"") {
		t.Errorf("update without news = %s, %v", got, err)
	}

	before = len(f.recorded())
	got, err = invoke(t, core, codeSecurityConfigurationsDelete.ID, "w-org", `{"owner":"orgs/octo-org","configuration_id":12}`, true)
	if err != nil || !strings.Contains(string(got), `"deleted":true`) || !strings.Contains(string(got), `"owner":"orgs/octo-org"`) {
		t.Fatalf("delete = %s, %v", got, err)
	}
	check("delete", http.MethodDelete, codeSecurityRoute+"/12", before, nil)
}

func TestCodeSecurityChangesCheckEverythingBeforeIO(t *testing.T) {
	f, core, reads := codeSecurityChangeCore(t, codeSecurityAnswer)
	with := func(key string, value any) string {
		settings := map[string]any{"owner": "orgs/octo-org", "name": "x", "configuration_id": 12}
		settings[key] = value
		encoded, _ := json.Marshal(settings)
		return string(encoded)
	}
	reviewers := func(n int, entry map[string]any) map[string]any {
		list := make([]any, n)
		for i := range list {
			list[i] = entry
		}
		return map[string]any{"reviewers": list}
	}
	type call struct{ id, connection, arguments string }
	var calls []call
	for _, id := range []string{codeSecurityConfigurationsCreate.ID, codeSecurityConfigurationsUpdate.ID} {
		for _, setting := range codeSecurityStateSettings {
			calls = append(calls, call{id, "w-org", with(setting.name, "on")}, call{id, "w-org", with(setting.name, "Enabled")},
				call{id, "w-org", with(setting.name, true)})
		}
		calls = append(calls,
			call{id, "w-org", with("advanced_security", "not_set")},
			call{id, "w-org", with("advanced_security", "ENABLED")},
			call{id, "w-org", with("enforcement", "enterprise_enforced")},
			call{id, "w-org", with("enforcement", "not_set")},
			call{id, "w-org", with("description", strings.Repeat("d", 256))},
			call{id, "w-org", with("code_scanning_default_setup_options", map[string]any{"runner_type": "custom"})},
			call{id, "w-org", with("code_scanning_default_setup_options", map[string]any{"runner_label": 3})},
			call{id, "w-org", with("code_scanning_default_setup_options", map[string]any{"extra": 1})},
			call{id, "w-org", with("code_scanning_options", map[string]any{"allow_advanced": "yes"})},
			call{id, "w-org", with("dependency_graph_autosubmit_action_options", map[string]any{"labeled_runners": 1})},
			call{id, "w-org", with("secret_scanning_delegated_bypass_options",
				reviewers(1, map[string]any{"reviewer_id": 1, "reviewer_type": "team"}))},
			call{id, "w-org", with("secret_scanning_delegated_bypass_options",
				reviewers(1, map[string]any{"reviewer_id": 1, "reviewer_type": "USER"}))},
			call{id, "w-org", with("secret_scanning_delegated_bypass_options",
				reviewers(1, map[string]any{"reviewer_id": 1, "reviewer_type": "TEAM", "mode": "always"}))},
			call{id, "w-org", with("secret_scanning_delegated_bypass_options",
				reviewers(1, map[string]any{"reviewer_type": "TEAM"}))},
			call{id, "w-org", with("secret_scanning_delegated_bypass_options",
				reviewers(1, map[string]any{"reviewer_id": 0, "reviewer_type": "TEAM"}))},
			call{id, "w-org", with("secret_scanning_delegated_bypass_options",
				reviewers(1, map[string]any{"reviewer_id": 1}))},
			call{id, "w-org", with("secret_scanning_delegated_bypass_options",
				reviewers(101, map[string]any{"reviewer_id": 1, "reviewer_type": "TEAM"}))},
			call{id, "w-org", with("secret_scanning_delegated_bypass_options",
				reviewers(1, map[string]any{"reviewer_id": 1, "reviewer_type": "TEAM", "security_configuration_id": 2}))},
			call{id, "w-org", with("unknown_setting", "enabled")},
			call{id, "w-org", with("name", "")},
			call{id, "w-org", with("name", "   ")},
			call{id, "w-org", with("name", 5)},
		)
	}
	calls = append(calls,
		call{codeSecurityConfigurationsCreate.ID, "w-org", `{"owner":"orgs/octo-org"}`},
		call{codeSecurityConfigurationsCreate.ID, "w-org", `{"owner":"orgs/octo-org","name":"x","configuration_id":1}`},
		call{codeSecurityConfigurationsUpdate.ID, "w-org", `{"owner":"orgs/octo-org"}`},
		call{codeSecurityConfigurationsUpdate.ID, "w-org", `{"owner":"orgs/octo-org","configuration_id":12}`},
		call{codeSecurityConfigurationsUpdate.ID, "w-org", `{"owner":"orgs/octo-org","configuration_id":12,"description":null}`},
		call{codeSecurityConfigurationsUpdate.ID, "w-org", `{"owner":"orgs/octo-org","configuration_id":0,"description":"x"}`},
		call{codeSecurityConfigurationsUpdate.ID, "w-org", `{"owner":"orgs/octo-org","configuration_id":"12","description":"x"}`},
		call{codeSecurityConfigurationsDelete.ID, "w-org", `{"owner":"orgs/octo-org"}`},
		call{codeSecurityConfigurationsDelete.ID, "w-org", `{"owner":"orgs/octo-org","configuration_id":-1}`},
		call{codeSecurityConfigurationsDelete.ID, "w-org", `{"owner":"orgs/octo-org","configuration_id":12,"name":"x"}`},
	)
	// The organization must be one the targets name as an owner, before a credential is read.
	for _, id := range codeSecurityChangeToolIDs {
		for _, owner := range []string{`"owner":"users/octocat"`, `"owner":"orgs/other"`, `"owner":"octo-org"`} {
			calls = append(calls, call{id, "w-org", `{` + owner + `,"name":"x","configuration_id":12,"description":"x"}`})
		}
		calls = append(calls,
			call{id, "w-open", `{"name":"x","configuration_id":12,"description":"x"}`},
			call{id, "w-user", `{"name":"x","configuration_id":12,"description":"x"}`},
			call{id, "w-user", `{"owner":"users/octocat","name":"x","configuration_id":12,"description":"x"}`},
			call{id, "w-repo", `{"name":"x","configuration_id":12,"description":"x"}`},
			call{id, "w-ex-owner", `{"owner":"orgs/octo-org","name":"x","configuration_id":12,"description":"x"}`},
		)
	}
	for _, tt := range calls {
		*reads = 0
		before := len(f.recorded())
		if _, err := invoke(t, core, tt.id, tt.connection, tt.arguments, true); err == nil || !(isInvalidRequest(err) || strings.Contains(err.Error(), "invalid")) {
			t.Errorf("%s(%s) on %s = %v, want an invalid request", tt.id, tt.arguments, tt.connection, err)
		}
		if *reads != 0 || len(f.recorded()) != before {
			t.Errorf("%s(%s) on %s reached the credential or GitHub", tt.id, tt.arguments, tt.connection)
		}
	}
}

func TestCodeSecurityChangesAreOfferedOnlyListedAndConfirmed(t *testing.T) {
	f, core, reads := codeSecurityChangeCore(t, codeSecurityAnswer)
	arguments := map[string]string{
		codeSecurityConfigurationsCreate.ID: `{"owner":"orgs/octo-org","name":"x"}`,
		codeSecurityConfigurationsUpdate.ID: `{"owner":"orgs/octo-org","configuration_id":12,"description":"x"}`,
		codeSecurityConfigurationsDelete.ID: `{"owner":"orgs/octo-org","configuration_id":12}`,
	}
	for _, id := range codeSecurityChangeToolIDs {
		var unsupported *capability.UnsupportedError
		for _, connection := range []string{"org", "open"} {
			if _, err := core.Describe(application.DescribeRequest{Operation: id, Connection: connection}); !errors.As(err, &unsupported) {
				t.Errorf("describe %s on %s = %v, want unsupported", id, connection, err)
			}
			*reads = 0
			before := len(f.recorded())
			if _, err := invoke(t, core, id, connection, arguments[id], true); !errors.As(err, &unsupported) {
				t.Errorf("invoke %s on %s = %v, want unsupported", id, connection, err)
			}
			if *reads != 0 || len(f.recorded()) != before {
				t.Errorf("%s on %s reached the credential or GitHub", id, connection)
			}
		}
		if _, err := core.Describe(application.DescribeRequest{Operation: id, Connection: "w-org"}); err != nil {
			t.Errorf("describe %s on w-org = %v", id, err)
		}
		before := len(f.recorded())
		unconfirmed := &application.ConfirmationRequiredError{}
		if _, err := invoke(t, core, id, "w-org", arguments[id], false); !errors.As(err, &unconfirmed) {
			t.Errorf("%s without confirmation = %v, want confirmation-required", id, err)
		}
		if len(f.recorded()) != before {
			t.Errorf("%s reached GitHub before confirmation", id)
		}
	}
}

func TestCodeSecurityChangesAreResultsWithoutProviderTextAndNeverRepeated(t *testing.T) {
	status := 0
	f, core, _ := codeSecurityChangeCore(t, func(w http.ResponseWriter, r *http.Request) bool {
		if !strings.HasPrefix(r.URL.Path, codeSecurityRoute) {
			return false
		}
		w.WriteHeader(status)
		fmt.Fprint(w, `{"message":"`+providerSecret+`"}`)
		return true
	})
	arguments := map[string]string{
		codeSecurityConfigurationsCreate.ID: `{"owner":"orgs/octo-org","name":"x"}`,
		codeSecurityConfigurationsUpdate.ID: `{"owner":"orgs/octo-org","configuration_id":12,"description":"x"}`,
		codeSecurityConfigurationsDelete.ID: `{"owner":"orgs/octo-org","configuration_id":12}`,
	}
	classes := map[int]provider.Class{400: provider.ClassProviderError, 403: provider.ClassPermission,
		404: provider.ClassNotFound, 409: provider.ClassProviderError, 422: provider.ClassProviderError,
		502: provider.ClassProviderError, 504: provider.ClassTimeout}
	for _, id := range codeSecurityChangeToolIDs {
		for code, class := range classes {
			status = code
			before := len(f.recorded())
			_, err := invoke(t, core, id, "w-org", arguments[id], true)
			if err == nil || strings.Contains(err.Error(), providerSecret) || len(f.recorded()) != before+1 {
				t.Errorf("%s with %d = %v after %d requests, want one result without provider text", id, code, err,
					len(f.recorded())-before)
				continue
			}
			if got := classOf(err); got != class {
				t.Errorf("%s with %d has class %q, want %q", id, code, got, class)
			}
			if code == 403 && !strings.Contains(err.Error(), "write:org") {
				t.Errorf("%s with 403 = %v, want the permission named", id, err)
			}
			if code == 502 && !strings.Contains(err.Error(), strings.TrimSpace(uncertain)) {
				t.Errorf("%s with %d = %v, want the uncertain outcome said", id, code, err)
			}
		}
	}
	// A client-side deadline is a timeout after exactly one request.
	stall := newChangeStall(t)
	f.failure = stall.route
	for _, id := range codeSecurityChangeToolIDs {
		ctx, cancel := stall.context()
		before := len(f.recorded())
		_, err := core.Invoke(ctx, application.InvokeRequest{Operation: id, Connection: "w-org", Confirmed: true,
			Arguments: json.RawMessage(arguments[id])})
		cancel()
		if classOf(err) != provider.ClassTimeout || len(f.recorded()) != before+1 {
			t.Errorf("%s deadline = %v after %d requests, want one timeout", id, err, len(f.recorded())-before)
		}
	}
}

func TestCodeSecurityUpdateAndDeleteAreAllowedForRepositoriesOutsideTheTargets(t *testing.T) {
	f, core, _ := codeSecurityChangeCore(t, codeSecurityAnswer)
	// The connection excludes a repository the configuration may be attached to; the change still acts on the
	// organization's object and asks GitHub for nothing else.
	before := len(f.recorded())
	if _, err := invoke(t, core, codeSecurityConfigurationsUpdate.ID, "w-narrow",
		`{"owner":"orgs/octo-org","configuration_id":12,"secret_scanning":"enabled"}`, true); err != nil {
		t.Errorf("update = %v", err)
	}
	if _, err := invoke(t, core, codeSecurityConfigurationsDelete.ID, "w-narrow",
		`{"owner":"orgs/octo-org","configuration_id":12}`, true); err != nil {
		t.Errorf("delete = %v", err)
	}
	requests := f.recorded()[before:]
	if len(requests) != 2 || requests[0].method != http.MethodPatch || requests[1].method != http.MethodDelete {
		t.Errorf("requests = %+v, want one PATCH and one DELETE", requests)
	}
	// The configuration id is addressed below the organization of the path only.
	for _, request := range requests {
		if request.path != codeSecurityRoute+"/12" {
			t.Errorf("path = %s, want the configuration below the organization", request.path)
		}
	}
}
