package github

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const settingsProviderCanary = "settings-provider-text-canary-5c1d"

// fakeSettings answers the finer Actions settings routes of the repository octo-org/example and of the
// organization octo-org, keeps what they hold, and can refuse every request of one method with a status.
type fakeSettings struct {
	mu     sync.Mutex
	f      *fakeGitHub
	state  map[string]map[string]any // route below /api/v3 -> current members
	status map[string]int            // method -> status every request of it answers with
	reads  map[string]int            // route -> GET requests answered
	// readBackFails makes every GET after the first of a route answer 500, like a read that fails after a change.
	readBackFails bool
}

const (
	settingsRepoRoute = "/repos/octo-org/example/actions/"
	settingsOrgRoute  = "/orgs/octo-org/actions/"
)

func serveSettings(t *testing.T) (*fakeGitHub, *fakeSettings, string) {
	t.Helper()
	s := &fakeSettings{status: map[string]int{}, reads: map[string]int{}, state: map[string]map[string]any{}}
	for _, route := range []string{settingsRepoRoute, settingsOrgRoute} {
		s.state[route+"permissions/selected-actions"] = map[string]any{"github_owned_allowed": true,
			"verified_allowed": false, "patterns_allowed": []any{"octo-org/*"}}
		s.state[route+"permissions/fork-pr-contributor-approval"] = map[string]any{
			"approval_policy": "first_time_contributors"}
		s.state[route+"permissions/artifact-and-log-retention"] = map[string]any{"days": 90.0,
			"maximum_allowed_days": 400.0}
	}
	s.state[settingsRepoRoute+"permissions/access"] = map[string]any{"access_level": "none"}
	s.state[settingsRepoRoute+"oidc/customization/sub"] = map[string]any{"use_default": true,
		"use_immutable_subject": false, "sub_claim_prefix": "repo:octo-org/example"}
	s.state[settingsOrgRoute+"oidc/customization/sub"] = map[string]any{
		"include_claim_keys": []any{"repo", "context"}, "use_immutable_subject": false}
	f := &fakeGitHub{}
	s.f = f
	f.failure = s.route
	return f, s, serve(t, f)
}

func (s *fakeSettings) route(w http.ResponseWriter, req *http.Request) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := strings.TrimPrefix(req.URL.Path, "/api/v3")
	if !strings.HasPrefix(path, settingsRepoRoute) && !strings.HasPrefix(path, settingsOrgRoute) {
		return false
	}
	w.Header().Set("Content-Type", "application/json")
	if status := s.status[req.Method]; status != 0 {
		w.WriteHeader(status)
		fmt.Fprintf(w, `{"message":%q}`, settingsProviderCanary)
		return true
	}
	current, ok := s.state[path]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return true
	}
	switch req.Method {
	case http.MethodGet:
		s.reads[path]++
		if s.readBackFails && s.reads[path] > 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return true
		}
		data, _ := json.Marshal(current)
		w.Write(data)
	case http.MethodPut:
		requests := s.f.recorded()
		body := requests[len(requests)-1].body
		for key, value := range body {
			current[key] = value
		}
		if strings.HasSuffix(path, "oidc/customization/sub") {
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{}`))
		} else {
			w.WriteHeader(http.StatusNoContent)
		}
	default:
		return false
	}
	return true
}

var settingsReadTools = []string{actionsSelectedGet.ID, actionsForkApprovalGet.ID, actionsRetentionGet.ID,
	actionsAccessGet.ID, actionsOIDCGet.ID}

// settingsConfig binds every shape of connection the settings tools distinguish.
func settingsConfig(base string) *config.Config {
	cfg := coreConfig(base)
	all := []config.Effect{config.EffectRead, config.EffectCreate, config.EffectUpdate, config.EffectDelete}
	tools := append(append([]string{}, actionsSettingsReads...), actionsSettingsChanges...)
	connection := func(c config.Connection, listed bool) config.Connection {
		c.Service, c.Credential, c.Effects = "gh", "gh-reader", all
		if listed {
			c.Tools = tools
		}
		return c
	}
	cfg.Connections["repo"] = connection(config.Connection{Target: repoTarget}, false)
	cfg.Connections["repo-listed"] = connection(config.Connection{Target: repoTarget}, true)
	cfg.Connections["org-listed"] = connection(config.Connection{Targets: []string{"orgs/octo-org"}}, true)
	cfg.Connections["open"] = connection(config.Connection{}, true)
	cfg.Connections["repo-excluded"] = connection(config.Connection{Targets: []string{"repos/octo-org/*"},
		Exclude: []string{"repos/octo-org/example"}}, true)
	cfg.Connections["org-excluded"] = connection(config.Connection{Exclude: []string{"orgs/other-org"}}, true)
	return cfg
}

func settingsCore(t *testing.T, base string, reads *int) *application.Core {
	t.Helper()
	red := &redact.Redactor{}
	return application.New(registry(t), settingsConfig(base), resolver(red, reads), red)
}

// Every tool satisfies its output contract through the application core at both levels, names the level it
// addressed, reports the values the call gave as GitHub holds them before and after, and changes the setting
// with exactly one request.
func TestActionsSettingsToolsWorkAtBothLevels(t *testing.T) {
	f, s, base := serveSettings(t)
	core := settingsCore(t, base, nil)
	type step struct {
		name, get, set, level, target, args string
		wantGet, wantSet                    []string
	}
	repo, org := `"repository":"octo-org/example"`, `"organization":"orgs/octo-org"`
	steps := []step{
		{"selected", actionsSelectedGet.ID, actionsSelectedSet.ID, "repo-listed", repo,
			`,"github_owned_allowed":false,"patterns_allowed":["octo-org/tools@v1","!octo-org/denied"]`,
			[]string{`"github_owned_allowed":true`, `"patterns_allowed":["octo-org/*"]`, `"repository":"octo-org/example"`},
			[]string{`"before":{"github_owned_allowed":true,"patterns_allowed":["octo-org/*"]}`,
				`"after":{"github_owned_allowed":false,"patterns_allowed":["octo-org/tools@v1","!octo-org/denied"]}`}},
		{"selected", actionsSelectedGet.ID, actionsSelectedSet.ID, "org-listed", org, `,"verified_allowed":true`,
			[]string{`"organization":"orgs/octo-org"`, `"verified_allowed":false`},
			[]string{`"before":{"verified_allowed":false}`, `"after":{"verified_allowed":true}`}},
		{"fork approval", actionsForkApprovalGet.ID, actionsForkApprovalSet.ID, "repo-listed", repo,
			`,"approval_policy":"all_external_contributors"`, []string{`"approval_policy":"first_time_contributors"`},
			[]string{`"before":{"approval_policy":"first_time_contributors"}`,
				`"after":{"approval_policy":"all_external_contributors"}`}},
		{"fork approval", actionsForkApprovalGet.ID, actionsForkApprovalSet.ID, "org-listed", org,
			`,"approval_policy":"first_time_contributors_new_to_github"`,
			[]string{`"organization":"orgs/octo-org"`},
			[]string{`"after":{"approval_policy":"first_time_contributors_new_to_github"}`}},
		{"retention", actionsRetentionGet.ID, actionsRetentionSet.ID, "repo-listed", repo, `,"days":30`,
			[]string{`"days":90`, `"maximum_allowed_days":400`}, []string{`"before":{"days":90}`, `"after":{"days":30}`}},
		{"retention", actionsRetentionGet.ID, actionsRetentionSet.ID, "org-listed", org, `,"days":14`,
			[]string{`"organization":"orgs/octo-org"`}, []string{`"after":{"days":14}`}},
		{"access", actionsAccessGet.ID, actionsAccessSet.ID, "repo-listed", repo, `,"access_level":"organization"`,
			[]string{`"access_level":"none"`, `"repository":"octo-org/example"`},
			[]string{`"before":{"access_level":"none"}`, `"after":{"access_level":"organization"}`}},
		{"oidc", actionsOIDCGet.ID, actionsOIDCSet.ID, "repo-listed", repo,
			`,"use_default":false,"include_claim_keys":["repo","context"]`,
			[]string{`"use_default":true`, `"sub_claim_prefix":"repo:octo-org/example"`},
			[]string{`"before":{"use_default":true}`, `"after":{"include_claim_keys":["repo","context"],"use_default":false}`}},
		{"oidc", actionsOIDCGet.ID, actionsOIDCSet.ID, "org-listed", org, `,"include_claim_keys":["repo","job_workflow_ref"]`,
			[]string{`"include_claim_keys":["repo","context"]`, `"organization":"orgs/octo-org"`},
			[]string{`"before":{"include_claim_keys":["repo","context"]}`,
				`"after":{"include_claim_keys":["repo","job_workflow_ref"]}`}},
	}
	for _, tt := range steps {
		got, err := invoke(t, core, tt.get, tt.level, `{`+tt.target+`}`, false)
		if err != nil {
			t.Fatalf("%s get on %s = %v", tt.name, tt.level, err)
		}
		for _, want := range tt.wantGet {
			if !strings.Contains(string(got), want) {
				t.Errorf("%s get on %s = %s, want %s", tt.name, tt.level, got, want)
			}
		}
		before := len(f.recorded())
		set, err := invoke(t, core, tt.set, tt.level, `{`+tt.target+tt.args+`}`, true)
		if err != nil {
			t.Fatalf("%s set on %s = %v", tt.name, tt.level, err)
		}
		for _, want := range tt.wantSet {
			if !strings.Contains(string(set), want) {
				t.Errorf("%s set on %s = %s, want %s", tt.name, tt.level, set, want)
			}
		}
		methods := map[string]int{}
		for _, request := range f.recorded()[before:] {
			methods[request.method]++
		}
		if !reflect.DeepEqual(methods, map[string]int{http.MethodGet: 2, http.MethodPut: 1}) {
			t.Errorf("%s set on %s sent %v, want a read, one change, and a read back", tt.name, tt.level, methods)
		}
	}
	// An unchanged selection keeps what the call did not give, and a retention change never sends the maximum.
	if got := s.state[settingsRepoRoute+"permissions/selected-actions"]; got["verified_allowed"] != false {
		t.Errorf("selected actions = %v, want verified_allowed kept", got)
	}
}

// A change names the target of its own level in the answer, so a repository answer never names an
// organization and the other way round.
func TestActionsSettingsAnswerNameTheirTarget(t *testing.T) {
	_, _, base := serveSettings(t)
	core := settingsCore(t, base, nil)
	got, err := invoke(t, core, actionsRetentionSet.ID, "repo-listed", `{"repository":"octo-org/example","days":7}`, true)
	if err != nil || strings.Contains(string(got), `"organization"`) || !strings.Contains(string(got), `"repository":"octo-org/example"`) {
		t.Errorf("repository answer = %s, %v", got, err)
	}
	got, err = invoke(t, core, actionsRetentionSet.ID, "org-listed", `{"organization":"orgs/octo-org","days":7}`, true)
	if err != nil || strings.Contains(string(got), `"repository"`) || !strings.Contains(string(got), `"organization":"orgs/octo-org"`) {
		t.Errorf("organization answer = %s, %v", got, err)
	}
}

// A partial selection keeps the values it did not give, because GitHub replaces the selection as a whole.
func TestSelectedActionsKeepWhatTheCallDidNotGive(t *testing.T) {
	f, s, base := serveSettings(t)
	core := settingsCore(t, base, nil)
	before := len(f.recorded())
	if _, err := invoke(t, core, actionsSelectedSet.ID, "repo-listed",
		`{"repository":"octo-org/example","verified_allowed":true}`, true); err != nil {
		t.Fatal(err)
	}
	var put recorded
	for _, request := range f.recorded()[before:] {
		if request.method == http.MethodPut {
			put = request
		}
	}
	want := map[string]any{"github_owned_allowed": true, "verified_allowed": true, "patterns_allowed": []any{"octo-org/*"}}
	if !reflect.DeepEqual(put.body, want) {
		t.Errorf("sent selection = %v, want %v", put.body, want)
	}
	if _, err := invoke(t, core, actionsSelectedSet.ID, "repo-listed",
		`{"repository":"octo-org/example","patterns_allowed":[]}`, true); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.state[settingsRepoRoute+"permissions/selected-actions"]["patterns_allowed"].([]any); len(got) != 0 {
		t.Errorf("patterns after clearing = %v, want none", got)
	}
}

// A read that fails after an applied change does not turn it into an error, and no sent value stands in for
// GitHub's state: the answer drops after and says the state could not be read back.
func TestSettingsChangeSurvivesAFailedReadBack(t *testing.T) {
	_, s, base := serveSettings(t)
	s.readBackFails = true
	core := settingsCore(t, base, nil)
	got, err := invoke(t, core, actionsRetentionSet.ID, "repo-listed", `{"repository":"octo-org/example","days":21}`, true)
	if err != nil || strings.Contains(string(got), `"after":`) || !strings.Contains(string(got), `"after_unconfirmed":true`) ||
		!strings.Contains(string(got), `"before":{"days":90}`) {
		t.Errorf("change with a failed read back = %s, %v", got, err)
	}
}

// A read that GitHub refuses with a conflict reports the setting as unavailable, a state with a fixed reason and
// no value or GitHub text; 403 and 404 stay errors, and a change on such a setting stays an error that changes
// nothing.
func TestActionsSettingsReadsReportUnavailableAsAState(t *testing.T) {
	f, s, base := serveSettings(t)
	core := settingsCore(t, base, nil)
	s.status = map[string]int{http.MethodGet: http.StatusConflict}
	for _, tt := range []struct{ tool, connection, arguments string }{
		{actionsSelectedGet.ID, "repo-listed", `{"repository":"octo-org/example"}`},
		{actionsSelectedGet.ID, "org-listed", `{"organization":"orgs/octo-org"}`},
		{actionsForkApprovalGet.ID, "repo-listed", `{"repository":"octo-org/example"}`},
		{actionsRetentionGet.ID, "org-listed", `{"organization":"orgs/octo-org"}`},
		{actionsAccessGet.ID, "repo-listed", `{"repository":"octo-org/example"}`},
		{actionsOIDCGet.ID, "org-listed", `{"organization":"orgs/octo-org"}`},
	} {
		got, err := invoke(t, core, tt.tool, tt.connection, tt.arguments, false)
		if err != nil || !strings.Contains(string(got), `"state":"unavailable"`) ||
			!strings.Contains(string(got), `"reason":"GitHub reports that this setting does not apply`) ||
			strings.Contains(string(got), settingsProviderCanary) || strings.Contains(string(got), `"days"`) {
			t.Errorf("%s answered 409 = %s, %v, want the unavailable state", tt.tool, got, err)
		}
	}
	for _, status := range []int{http.StatusForbidden, http.StatusNotFound} {
		s.status = map[string]int{http.MethodGet: status}
		if _, err := invoke(t, core, actionsAccessGet.ID, "repo-listed", `{"repository":"octo-org/example"}`, false); err == nil {
			t.Errorf("a read answered %d reported no error", status)
		}
	}
	s.status = map[string]int{http.MethodGet: http.StatusConflict}
	before := len(f.recorded())
	if _, err := invoke(t, core, actionsAccessSet.ID, "repo-listed", `{"repository":"octo-org/example","access_level":"user"}`, true); err == nil ||
		!strings.Contains(err.Error(), "nothing was changed") {
		t.Errorf("a change on an unavailable setting = %v, want an error that changed nothing", err)
	}
	for _, request := range f.recorded()[before:] {
		if request.method != http.MethodGet {
			t.Errorf("a change on an unavailable setting sent %s", request.method)
		}
	}
}

// The subject claim takes the documented claim keys, check_run_id, and repository property claims named
// repo_property_ and an alphanumeric name; anything else is refused before a credential is resolved.
func TestOIDCClaimKeysAcceptRepositoryProperties(t *testing.T) {
	f, _, base := serveSettings(t)
	reads := 0
	core := settingsCore(t, base, &reads)
	if _, err := invoke(t, core, actionsOIDCSet.ID, "repo-listed",
		`{"repository":"octo-org/example","use_default":false,"include_claim_keys":["repo","check_run_id","repo_property_Team_1"]}`, true); err != nil {
		t.Fatalf("documented and property claim keys = %v", err)
	}
	for _, key := range []string{"repo_property_", "repo_property_a-b", "repo_property_a b", "repo_properties_x", "Repo_property_x",
		"repo_property_" + strings.Repeat("a", 120)} {
		reads = 0
		before := len(f.recorded())
		arguments := `{"repository":"octo-org/example","use_default":false,"include_claim_keys":["` + key + `"]}`
		if _, err := invoke(t, core, actionsOIDCSet.ID, "repo-listed", arguments, true); !isInvalidRequest(err) {
			t.Errorf("claim key %q = %v, want an invalid request", key, err)
		}
		if reads != 0 || len(f.recorded()) != before {
			t.Errorf("claim key %q reached the credential or GitHub", key)
		}
	}
}

// repository and organization are exclusive: neither, both, a foreign or excluded target, or a user instead of
// an organization is refused before a credential is resolved or GitHub is asked, for every shared tool.
func TestActionsSettingsTargetIsExclusiveAndBoundBeforeIO(t *testing.T) {
	f, _, base := serveSettings(t)
	reads := 0
	core := settingsCore(t, base, &reads)
	shared := []string{actionsSelectedGet.ID, actionsForkApprovalGet.ID, actionsRetentionGet.ID, actionsOIDCGet.ID}
	for _, id := range shared {
		for _, tt := range []struct{ name, connection, arguments string }{
			{"neither", "open", `{}`},
			{"both", "open", `{"repository":"octo-org/example","organization":"orgs/octo-org"}`},
			{"an organization outside the targets", "org-listed", `{"organization":"orgs/other-org"}`},
			{"an excluded organization", "org-excluded", `{"organization":"orgs/other-org"}`},
			{"a user instead of an organization", "open", `{"organization":"users/octocat"}`},
			{"a repository outside the targets", "repo-listed", `{"repository":"octo-org/other"}`},
			{"an excluded repository", "repo-excluded", `{"repository":"octo-org/example"}`},
		} {
			reads = 0
			before := len(f.recorded())
			if _, err := invoke(t, core, id, tt.connection, tt.arguments, false); !isInvalidRequest(err) {
				t.Errorf("%s %s = %v, want an invalid request", id, tt.name, err)
			}
			if reads != 0 || len(f.recorded()) != before {
				t.Errorf("%s %s reached the credential or GitHub", id, tt.name)
			}
		}
	}
	// The access tools are repository only: an organization argument is no property of their input.
	for _, id := range []string{actionsAccessGet.ID, actionsAccessSet.ID} {
		for _, descriptor := range registry(t).Provider(Provider) {
			if descriptor.ID == id && strings.Contains(string(descriptor.InputSchema), `"organization":{`) {
				t.Errorf("%s offers an organization", id)
			}
		}
		reads = 0
		before := len(f.recorded())
		if _, err := invoke(t, core, id, "repo-excluded", `{"repository":"octo-org/example","access_level":"none"}`, true); !isInvalidRequest(err) {
			t.Errorf("%s on an excluded repository = %v, want an invalid request", id, err)
		}
		if reads != 0 || len(f.recorded()) != before {
			t.Errorf("%s on an excluded repository reached the credential or GitHub", id)
		}
	}
}

// Every tool is offered only where a connection's tools list names it, and a change without confirmation sends
// nothing.
func TestActionsSettingsNeedTheirNameInTheToolsList(t *testing.T) {
	f, _, base := serveSettings(t)
	reads := 0
	core := settingsCore(t, base, &reads)
	arguments := map[string]string{
		actionsSelectedGet.ID: `{"repository":"octo-org/example"}`, actionsSelectedSet.ID: `{"repository":"octo-org/example","verified_allowed":true}`,
		actionsForkApprovalGet.ID: `{"repository":"octo-org/example"}`, actionsForkApprovalSet.ID: `{"repository":"octo-org/example","approval_policy":"first_time_contributors"}`,
		actionsRetentionGet.ID: `{"repository":"octo-org/example"}`, actionsRetentionSet.ID: `{"repository":"octo-org/example","days":5}`,
		actionsAccessGet.ID: `{"repository":"octo-org/example"}`, actionsAccessSet.ID: `{"repository":"octo-org/example","access_level":"user"}`,
		actionsOIDCGet.ID: `{"repository":"octo-org/example"}`, actionsOIDCSet.ID: `{"repository":"octo-org/example","use_default":true}`,
	}
	for id, args := range arguments {
		var unsupported *capability.UnsupportedError
		if _, err := core.Describe(application.DescribeRequest{Operation: id, Connection: "repo"}); !errors.As(err, &unsupported) {
			t.Errorf("describe %s on a connection without the tool = %v, want unsupported", id, err)
		}
		reads = 0
		before := len(f.recorded())
		if _, err := invoke(t, core, id, "repo", args, true); !errors.As(err, &unsupported) {
			t.Errorf("invoke %s on a connection without the tool = %v, want unsupported", id, err)
		}
		if reads != 0 || len(f.recorded()) != before {
			t.Errorf("%s on a connection without the tool reached the credential or GitHub", id)
		}
		if !strings.HasSuffix(id, ".set") {
			continue
		}
		var unconfirmed *application.ConfirmationRequiredError
		if _, err := invoke(t, core, id, "repo-listed", args, false); !errors.As(err, &unconfirmed) {
			t.Errorf("%s without confirmation = %v, want confirmation-required", id, err)
		}
		if len(f.recorded()) != before {
			t.Errorf("%s reached GitHub before confirmation", id)
		}
	}
	// None of the ten tools is part of a profile that a new connection starts with, and the changes are in no
	// profile at all.
	metadata, _ := registry(t).ProviderMetadata(Provider)
	for _, profile := range metadata.Profiles {
		for _, id := range profile.Tools {
			if contains(actionsSettingsChanges, id) {
				t.Errorf("profile %s selects %s", profile.ID, id)
			}
			if contains(actionsSettingsReads, id) && (profile.Recommended || profile.ID != "actions-admin") {
				t.Errorf("profile %s selects %s", profile.ID, id)
			}
		}
	}
}

// Every check runs before a credential is resolved: values GitHub documents only, and the level-specific rules
// of the OIDC subject claim.
func TestActionsSettingsArgumentsAreValidatedBeforeIO(t *testing.T) {
	f, _, base := serveSettings(t)
	reads := 0
	core := settingsCore(t, base, &reads)
	repo, org := `"repository":"octo-org/example"`, `"organization":"orgs/octo-org"`
	many := make([]string, 1001)
	for i := range many {
		many[i] = fmt.Sprintf(`"octo-org/a%d"`, i)
	}
	for _, tt := range []struct{ name, tool, connection, arguments string }{
		{"selected without a value", actionsSelectedSet.ID, "repo-listed", `{` + repo + `}`},
		{"a pattern with a space", actionsSelectedSet.ID, "repo-listed", `{` + repo + `,"patterns_allowed":["octo-org/a b"]}`},
		{"a pattern with a comma", actionsSelectedSet.ID, "repo-listed", `{` + repo + `,"patterns_allowed":["a/b,c/d"]}`},
		{"an empty pattern", actionsSelectedSet.ID, "repo-listed", `{` + repo + `,"patterns_allowed":[""]}`},
		{"a repeated pattern", actionsSelectedSet.ID, "repo-listed", `{` + repo + `,"patterns_allowed":["a/b","a/b"]}`},
		{"too many patterns", actionsSelectedSet.ID, "repo-listed", `{` + repo + `,"patterns_allowed":[` + strings.Join(many, ",") + `]}`},
		{"an unknown approval policy", actionsForkApprovalSet.ID, "repo-listed", `{` + repo + `,"approval_policy":"nobody"}`},
		{"no approval policy", actionsForkApprovalSet.ID, "org-listed", `{` + org + `}`},
		{"zero days", actionsRetentionSet.ID, "repo-listed", `{` + repo + `,"days":0}`},
		{"too many days", actionsRetentionSet.ID, "repo-listed", `{` + repo + `,"days":401}`},
		{"no days", actionsRetentionSet.ID, "org-listed", `{` + org + `}`},
		{"an unknown access level", actionsAccessSet.ID, "repo-listed", `{` + repo + `,"access_level":"everyone"}`},
		{"an unknown claim key", actionsOIDCSet.ID, "repo-listed", `{` + repo + `,"use_default":false,"include_claim_keys":["email"]}`},
		{"a repeated claim key", actionsOIDCSet.ID, "repo-listed", `{` + repo + `,"use_default":false,"include_claim_keys":["repo","repo"]}`},
		{"no claim key", actionsOIDCSet.ID, "repo-listed", `{` + repo + `,"use_default":false,"include_claim_keys":[]}`},
		{"a repository without use_default", actionsOIDCSet.ID, "repo-listed", `{` + repo + `,"include_claim_keys":["repo"]}`},
		{"use_default false without keys", actionsOIDCSet.ID, "repo-listed", `{` + repo + `,"use_default":false}`},
		{"use_default true with keys", actionsOIDCSet.ID, "repo-listed", `{` + repo + `,"use_default":true,"include_claim_keys":["repo"]}`},
		{"use_default for an organization", actionsOIDCSet.ID, "org-listed", `{` + org + `,"use_default":true}`},
		{"an organization without a value", actionsOIDCSet.ID, "org-listed", `{` + org + `}`},
	} {
		reads = 0
		before := len(f.recorded())
		if _, err := invoke(t, core, tt.tool, tt.connection, tt.arguments, true); !isInvalidRequest(err) {
			t.Errorf("%s = %v, want an invalid request", tt.name, err)
		}
		if reads != 0 || len(f.recorded()) != before {
			t.Errorf("%s reached the credential or GitHub", tt.name)
		}
	}
}

// An organization that only switches the immutable subject keeps its current keys; without any, they must be given.
func TestOrganizationOIDCImmutableSubjectKeepsCurrentKeys(t *testing.T) {
	f, s, base := serveSettings(t)
	core := settingsCore(t, base, nil)
	before := len(f.recorded())
	if _, err := invoke(t, core, actionsOIDCSet.ID, "org-listed", `{"organization":"orgs/octo-org","use_immutable_subject":true}`, true); err != nil {
		t.Fatal(err)
	}
	var put recorded
	for _, request := range f.recorded()[before:] {
		if request.method == http.MethodPut {
			put = request
		}
	}
	want := map[string]any{"include_claim_keys": []any{"repo", "context"}, "use_immutable_subject": true}
	if !reflect.DeepEqual(put.body, want) {
		t.Errorf("sent subject claim = %v, want %v", put.body, want)
	}
	s.state[settingsOrgRoute+"oidc/customization/sub"] = map[string]any{"include_claim_keys": []any{}}
	before = len(f.recorded())
	if _, err := invoke(t, core, actionsOIDCSet.ID, "org-listed", `{"organization":"orgs/octo-org","use_immutable_subject":true}`, true); !isInvalidRequest(err) {
		t.Errorf("immutable subject without keys = %v, want an invalid request", err)
	}
	for _, request := range f.recorded()[before:] {
		if request.method != http.MethodGet {
			t.Errorf("a refused change sent %s %s", request.method, request.path)
		}
	}
}

// GitHub's refusals become Qatlas' own messages without GitHub's text, name the right each level needs, and a
// refusal or an unclear outcome of a change is sent once and never repeated.
func TestActionsSettingsFailuresNameTheirMeaningWithoutProviderText(t *testing.T) {
	f, s, base := serveSettings(t)
	core := settingsCore(t, base, nil)
	levels := map[string]struct{ connection, target, right string }{
		"repository":   {"repo-listed", `"repository":"octo-org/example"`, "repo on a classic token"},
		"organization": {"org-listed", `"organization":"orgs/octo-org"`, "admin:org"},
	}
	for name, level := range levels {
		for _, tt := range []struct {
			method    string
			status    int
			class     provider.Class
			fragment  string
			uncertain bool
			puts      int
		}{
			{http.MethodGet, http.StatusForbidden, provider.ClassPermission, level.right, false, 0},
			{http.MethodGet, http.StatusNotFound, provider.ClassNotFound, "does not hold", false, 0},
			{http.MethodGet, http.StatusConflict, provider.ClassProviderError, "so nothing was changed", false, 0},
			{http.MethodPut, http.StatusForbidden, provider.ClassPermission, level.right, false, 1},
			{http.MethodPut, http.StatusNotFound, provider.ClassNotFound, "does not hold", false, 1},
			{http.MethodPut, http.StatusConflict, provider.ClassProviderError, "nothing was changed", false, 1},
			{http.MethodPut, http.StatusUnprocessableEntity, provider.ClassProviderError, "number of days", false, 1},
			{http.MethodPut, http.StatusGatewayTimeout, provider.ClassTimeout, "did not answer in time", true, 1},
			{http.MethodPut, http.StatusBadGateway, provider.ClassProviderError, "HTTP 502", true, 1},
		} {
			s.status = map[string]int{tt.method: tt.status}
			before := len(f.recorded())
			_, err := invoke(t, core, actionsRetentionSet.ID, level.connection, `{`+level.target+`,"days":10}`, true)
			if classOf(err) != tt.class || err == nil || !strings.Contains(err.Error(), tt.fragment) ||
				strings.Contains(err.Error(), uncertain) != tt.uncertain {
				t.Errorf("%s %s answered %d = %v, want %s naming %q", name, tt.method, tt.status, err, tt.class, tt.fragment)
			}
			if err != nil && strings.Contains(err.Error(), settingsProviderCanary) {
				t.Errorf("%s %s answered %d quotes GitHub's text: %v", name, tt.method, tt.status, err)
			}
			puts := 0
			for _, request := range f.recorded()[before:] {
				if request.method == http.MethodPut {
					puts++
				}
			}
			if puts != tt.puts {
				t.Errorf("%s %s answered %d sent %d changes, want %d", name, tt.method, tt.status, puts, tt.puts)
			}
		}
		// A refused read names the read right only.
		s.status = map[string]int{http.MethodGet: http.StatusForbidden}
		_, err := invoke(t, core, actionsRetentionGet.ID, level.connection, `{`+level.target+`}`, false)
		if classOf(err) != provider.ClassPermission || err == nil || !strings.Contains(err.Error(), "reading it needs") ||
			strings.Contains(err.Error(), "changing") {
			t.Errorf("%s read refused = %v, want the read right", name, err)
		}
		s.status = map[string]int{}
	}
	// A change of the selection while allowed_actions is not selected is refused and says so.
	s.status = map[string]int{http.MethodGet: http.StatusConflict}
	_, err := invoke(t, core, actionsSelectedSet.ID, "repo-listed", `{"repository":"octo-org/example","verified_allowed":true}`, true)
	if err == nil || !strings.Contains(err.Error(), "allowed_actions is selected") {
		t.Errorf("selected actions without selection = %v, want the allowed_actions hint", err)
	}
}

// GitHub's answers are untrusted data: a missing or unknown value is an invalid response, never passed on.
func TestActionsSettingsRefuseUnreadableAnswers(t *testing.T) {
	_, s, base := serveSettings(t)
	core := settingsCore(t, base, nil)
	s.state[settingsRepoRoute+"permissions/fork-pr-contributor-approval"] = map[string]any{"approval_policy": "anything"}
	s.state[settingsRepoRoute+"permissions/artifact-and-log-retention"] = map[string]any{"days": 3.0}
	s.state[settingsRepoRoute+"permissions/access"] = map[string]any{"access_level": "world"}
	s.state[settingsRepoRoute+"oidc/customization/sub"] = map[string]any{"include_claim_keys": []any{"repo"}}
	for _, id := range []string{actionsForkApprovalGet.ID, actionsRetentionGet.ID, actionsAccessGet.ID, actionsOIDCGet.ID} {
		if _, err := invoke(t, core, id, "repo-listed", `{"repository":"octo-org/example"}`, false); classOf(err) != provider.ClassInvalidResponse {
			t.Errorf("%s with an unreadable answer = %v, want invalid-provider-response", id, err)
		}
	}
}
