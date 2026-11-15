package github

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const assignRepositoryPrefix = "/api/v3/repos/octo-org/"

// assignRepositoryIDs are the identifiers the fake serves when a repository is read.
var assignRepositoryIDs = map[string]int64{"example": 1296269, "tools": 1296270, "secret": 1296271}

// assignAnswer serves the repository reads and, below the configurations, the answer of each route.
func assignAnswer(status func() int) func(http.ResponseWriter, *http.Request) bool {
	return func(w http.ResponseWriter, r *http.Request) bool {
		if name, found := strings.CutPrefix(strings.ToLower(r.URL.Path), assignRepositoryPrefix); found && r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			if name == "renamed" {
				fmt.Fprint(w, `{"id":1296272,"full_name":"octo-org/elsewhere"}`)
				return true
			}
			id, known := assignRepositoryIDs[strings.ToLower(name)]
			if !known {
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprint(w, `{"message":"`+providerSecret+`"}`)
				return true
			}
			fmt.Fprintf(w, `{"id":%d,"full_name":"octo-org/%s"}`, id, strings.ToLower(name))
			return true
		}
		if !strings.HasPrefix(r.URL.Path, codeSecurityRoute) {
			return false
		}
		w.Header().Set("Content-Type", "application/json")
		if code := status(); code != 0 {
			w.WriteHeader(code)
			fmt.Fprint(w, `{"message":"`+providerSecret+`"}`)
			return true
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/attach"):
			w.WriteHeader(http.StatusAccepted)
			fmt.Fprint(w, `{}`)
		case strings.HasSuffix(r.URL.Path, "/defaults"):
			fmt.Fprint(w, `{"default_for_new_repos":"all","configuration":`+codeSecurityConfigurationJSON(12, 1)+`}`)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
		return true
	}
}

func codeSecurityAssignCore(t *testing.T, status func() int) (*fakeGitHub, *application.Core, *int) {
	t.Helper()
	f := &fakeGitHub{failure: assignAnswer(status)}
	cfg := discoveryConfig(serve(t, f))
	add := func(name string, targets, excludes []string) {
		cfg.Connections[name] = config.Connection{Service: "gh", Credential: "gh-reader", Effects: config.Effects(),
			Targets: targets, Exclude: excludes, Tools: codeSecurityAssignToolIDs}
	}
	add("w-org", []string{"orgs/octo-org"}, nil)
	add("w-open", nil, nil)
	add("w-all", []string{"orgs/octo-org", "repos/octo-org/*"}, nil)
	add("w-user", []string{"users/octocat"}, nil)
	add("w-repo", []string{"orgs/octo-org", "repos/octo-org/example"}, nil)
	add("w-excluding", []string{"orgs/octo-org", "repos/octo-org/*"}, []string{"repos/octo-org/secret"})
	add("w-ex-owner", nil, []string{"orgs/octo-org"})
	reads := 0
	red := &redact.Redactor{}
	return f, application.New(registry(t), cfg, resolver(red, &reads), red), &reads
}

func noStatus() int { return 0 }

const (
	assignAttachSelected = `{"owner":"orgs/octo-org","configuration_id":12,"scope":"selected",` +
		`"selected_repositories":["octo-org/example","Octo-Org/tools"]}`
	assignDetach     = `{"owner":"orgs/octo-org","selected_repositories":["octo-org/example"]}`
	assignSetDefault = `{"owner":"orgs/octo-org","configuration_id":12,"default_for_new_repos":"all"}`
)

func TestCodeSecurityAssignmentsSendOneRequestWithResolvedIdentifiers(t *testing.T) {
	f, core, _ := codeSecurityAssignCore(t, noStatus)
	last := func(before int) (recorded, []recorded) {
		requests := f.recorded()[before:]
		return requests[len(requests)-1], requests[:len(requests)-1]
	}

	before := len(f.recorded())
	got, err := invoke(t, core, codeSecurityConfigurationsAttach.ID, "w-all", assignAttachSelected, true)
	if err != nil || !strings.Contains(string(got), `"accepted":true`) || !strings.Contains(string(got), `"owner":"orgs/octo-org"`) ||
		!strings.Contains(string(got), `"selected_repositories":["octo-org/example","Octo-Org/tools"]`) {
		t.Fatalf("attach = %s, %v", got, err)
	}
	write, reads := last(before)
	if write.method != http.MethodPost || write.path != codeSecurityRoute+"/12/attach" ||
		!reflect.DeepEqual(write.body, map[string]any{"scope": "selected",
			"selected_repository_ids": []any{float64(1296269), float64(1296270)}}) {
		t.Errorf("attach wrote %+v", write)
	}
	if len(reads) != 2 || reads[0].method != http.MethodGet || reads[0].path != assignRepositoryPrefix+"example" ||
		reads[1].path != "/api/v3/repos/Octo-Org/tools" {
		t.Errorf("attach read %+v, want the two repositories first", reads)
	}

	// Another scope names no repository and sends none, on a connection that names the whole organization.
	for _, connection := range []string{"w-org", "w-open", "w-all"} {
		before = len(f.recorded())
		if _, err := invoke(t, core, codeSecurityConfigurationsAttach.ID, connection,
			`{"owner":"orgs/octo-org","configuration_id":12,"scope":"all_without_configurations"}`, true); err != nil {
			t.Fatalf("attach scope on %s = %v", connection, err)
		}
		write, reads = last(before)
		if len(reads) != 0 || write.method != http.MethodPost || write.path != codeSecurityRoute+"/12/attach" ||
			!reflect.DeepEqual(write.body, map[string]any{"scope": "all_without_configurations"}) {
			t.Errorf("attach scope on %s sent %+v after %+v", connection, write, reads)
		}
	}

	before = len(f.recorded())
	got, err = invoke(t, core, codeSecurityConfigurationsDetach.ID, "w-all", assignDetach, true)
	if err != nil || !strings.Contains(string(got), `"accepted":true`) {
		t.Fatalf("detach = %s, %v", got, err)
	}
	write, reads = last(before)
	if write.method != http.MethodDelete || write.path != codeSecurityRoute+"/detach" ||
		!reflect.DeepEqual(write.body, map[string]any{"selected_repository_ids": []any{float64(1296269)}}) || len(reads) != 1 {
		t.Errorf("detach wrote %+v after %+v", write, reads)
	}

	before = len(f.recorded())
	got, err = invoke(t, core, codeSecurityConfigurationsSetDefault.ID, "w-org", assignSetDefault, true)
	if err != nil || !strings.Contains(string(got), `"default_for_new_repos":"all"`) || !strings.Contains(string(got), `"id":12`) {
		t.Fatalf("set-default = %s, %v", got, err)
	}
	write, reads = last(before)
	if write.method != http.MethodPut || write.path != codeSecurityRoute+"/12/defaults" || len(reads) != 0 ||
		!reflect.DeepEqual(write.body, map[string]any{"default_for_new_repos": "all"}) {
		t.Errorf("set-default wrote %+v after %+v", write, reads)
	}
	// A default reaches only repositories that do not exist yet, so narrowed targets do not stop it.
	if _, err := invoke(t, core, codeSecurityConfigurationsSetDefault.ID, "w-excluding", assignSetDefault, true); err != nil {
		t.Errorf("set-default on narrowed targets = %v", err)
	}
}

func TestCodeSecurityAssignmentsCheckEverythingBeforeIO(t *testing.T) {
	f, core, reads := codeSecurityAssignCore(t, noStatus)
	attach, detach, def := codeSecurityConfigurationsAttach.ID, codeSecurityConfigurationsDetach.ID,
		codeSecurityConfigurationsSetDefault.ID
	org := `"owner":"orgs/octo-org"`
	many := make([]string, 51)
	for i := range many {
		many[i] = fmt.Sprintf("octo-org/r%d", i)
	}
	manyJSON, _ := json.Marshal(many)
	type call struct{ id, connection, arguments string }
	calls := []call{
		// scope
		{attach, "w-all", `{` + org + `,"configuration_id":12}`},
		{attach, "w-all", `{` + org + `,"configuration_id":12,"scope":"everything"}`},
		{attach, "w-all", `{` + org + `,"configuration_id":12,"scope":"ALL"}`},
		{attach, "w-all", `{` + org + `,"configuration_id":12,"scope":true}`},
		{attach, "w-all", `{` + org + `,"scope":"all"}`},
		{attach, "w-all", `{` + org + `,"configuration_id":0,"scope":"all"}`},
		{attach, "w-all", `{` + org + `,"configuration_id":"12","scope":"all"}`},
		// every scope but selected is refused where the targets or exclusions narrow the organization
		{attach, "w-repo", `{` + org + `,"configuration_id":12,"scope":"all"}`},
		{attach, "w-repo", `{` + org + `,"configuration_id":12,"scope":"public"}`},
		{attach, "w-excluding", `{` + org + `,"configuration_id":12,"scope":"private_or_internal"}`},
		{attach, "w-excluding", `{` + org + `,"configuration_id":12,"scope":"all_without_configurations"}`},
		// an organization target alone names no repository, so nothing can be selected
		{attach, "w-org", assignAttachSelected},
		{detach, "w-org", assignDetach},
		// a list belongs to selected only
		{attach, "w-all", `{` + org + `,"configuration_id":12,"scope":"all","selected_repositories":["octo-org/example"]}`},
		{attach, "w-all", `{` + org + `,"configuration_id":12,"scope":"selected"}`},
		{attach, "w-all", `{` + org + `,"configuration_id":12,"scope":"selected","selected_repositories":[]}`},
		{attach, "w-all", `{` + org + `,"configuration_id":12,"scope":"all","selected_repositories":[]}`},
		// identifiers are never an argument
		{attach, "w-all", `{` + org + `,"configuration_id":12,"scope":"selected","selected_repository_ids":[1296269]}`},
		{attach, "w-all", `{` + org + `,"configuration_id":12,"scope":"selected","selected_repositories":[1296269]}`},
		{detach, "w-all", `{` + org + `,"selected_repository_ids":[1296269]}`},
		{def, "w-all", `{` + org + `,"configuration_id":12,"default_for_new_repos":"all","selected_repositories":["octo-org/example"]}`},
		// repositories outside the organization, the targets, or the connection
		{attach, "w-all", `{` + org + `,"configuration_id":12,"scope":"selected","selected_repositories":["other-org/example"]}`},
		{detach, "w-all", `{` + org + `,"selected_repositories":["other-org/example"]}`},
		{attach, "w-repo", `{` + org + `,"configuration_id":12,"scope":"selected","selected_repositories":["octo-org/tools"]}`},
		{detach, "w-repo", `{` + org + `,"selected_repositories":["octo-org/example","octo-org/tools"]}`},
		{attach, "w-excluding", `{` + org + `,"configuration_id":12,"scope":"selected","selected_repositories":["octo-org/secret"]}`},
		{detach, "w-excluding", `{` + org + `,"selected_repositories":["octo-org/secret"]}`},
		{attach, "w-all", `{` + org + `,"configuration_id":12,"scope":"selected","selected_repositories":["octo-org/example","OCTO-ORG/example"]}`},
		{detach, "w-all", `{` + org + `,"selected_repositories":["example"]}`},
		{detach, "w-all", `{` + org + `,"selected_repositories":["octo-org/example/extra"]}`},
		{detach, "w-all", `{` + org + `,"selected_repositories":[]}`},
		{detach, "w-all", `{` + org + `}`},
		{detach, "w-all", `{` + org + `,"selected_repositories":` + string(manyJSON) + `}`},
		{attach, "w-all", `{` + org + `,"configuration_id":12,"scope":"selected","selected_repositories":` + string(manyJSON) + `}`},
		{detach, "w-all", `{` + org + `,"configuration_id":12,"selected_repositories":["octo-org/example"]}`},
		// default
		{def, "w-all", `{` + org + `,"configuration_id":12}`},
		{def, "w-all", `{` + org + `,"default_for_new_repos":"all"}`},
		{def, "w-all", `{` + org + `,"configuration_id":12,"default_for_new_repos":"ALL"}`},
		{def, "w-all", `{` + org + `,"configuration_id":12,"default_for_new_repos":"private"}`},
		{def, "w-all", `{` + org + `,"configuration_id":12,"default_for_new_repos":null}`},
		{def, "w-all", `{` + org + `,"configuration_id":-3,"default_for_new_repos":"none"}`},
	}
	// The organization must be one the targets name as an owner, before a credential is read.
	body := map[string]string{attach: `"configuration_id":12,"scope":"selected","selected_repositories":["octo-org/example"]`,
		detach: `"selected_repositories":["octo-org/example"]`, def: `"configuration_id":12,"default_for_new_repos":"all"`}
	for id, rest := range body {
		for _, owner := range []string{`"owner":"users/octocat",`, `"owner":"orgs/other",`, `"owner":"octo-org",`} {
			calls = append(calls, call{id, "w-all", `{` + owner + rest + `}`})
		}
		calls = append(calls,
			call{id, "w-open", `{` + rest + `}`},
			call{id, "w-user", `{` + rest + `}`},
			call{id, "w-user", `{"owner":"users/octocat",` + rest + `}`},
			call{id, "w-ex-owner", `{` + org + `,` + rest + `}`})
	}
	for _, tt := range calls {
		*reads = 0
		before := len(f.recorded())
		if _, err := invoke(t, core, tt.id, tt.connection, tt.arguments, true); err == nil ||
			!(isInvalidRequest(err) || strings.Contains(err.Error(), "invalid")) {
			t.Errorf("%s(%s) on %s = %v, want an invalid request", tt.id, tt.arguments, tt.connection, err)
		}
		if *reads != 0 || len(f.recorded()) != before {
			t.Errorf("%s(%s) on %s reached the credential or GitHub", tt.id, tt.arguments, tt.connection)
		}
	}
}

func TestCodeSecurityAssignmentsAreOfferedOnlyListedAndConfirmed(t *testing.T) {
	f, core, reads := codeSecurityAssignCore(t, noStatus)
	arguments := map[string]string{codeSecurityConfigurationsAttach.ID: assignAttachSelected,
		codeSecurityConfigurationsDetach.ID: assignDetach, codeSecurityConfigurationsSetDefault.ID: assignSetDefault}
	for _, id := range codeSecurityAssignToolIDs {
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
		if _, err := core.Describe(application.DescribeRequest{Operation: id, Connection: "w-all"}); err != nil {
			t.Errorf("describe %s on w-all = %v", id, err)
		}
		before := len(f.recorded())
		unconfirmed := &application.ConfirmationRequiredError{}
		if _, err := invoke(t, core, id, "w-all", arguments[id], false); !errors.As(err, &unconfirmed) {
			t.Errorf("%s without confirmation = %v, want confirmation-required", id, err)
		}
		if len(f.recorded()) != before {
			t.Errorf("%s reached GitHub before confirmation", id)
		}
	}
}

func TestCodeSecurityAssignmentsAreResultsWithoutProviderTextAndNeverRepeated(t *testing.T) {
	status := 0
	f, core, _ := codeSecurityAssignCore(t, func() int { return status })
	arguments := map[string]string{codeSecurityConfigurationsAttach.ID: assignAttachSelected,
		codeSecurityConfigurationsDetach.ID: assignDetach, codeSecurityConfigurationsSetDefault.ID: assignSetDefault}
	reads := map[string]int{codeSecurityConfigurationsAttach.ID: 2, codeSecurityConfigurationsDetach.ID: 1}
	classes := map[int]provider.Class{400: provider.ClassProviderError, 403: provider.ClassPermission,
		404: provider.ClassNotFound, 409: provider.ClassProviderError, 422: provider.ClassProviderError,
		502: provider.ClassProviderError, 504: provider.ClassTimeout}
	for _, id := range codeSecurityAssignToolIDs {
		for code, class := range classes {
			status = code
			before := len(f.recorded())
			_, err := invoke(t, core, id, "w-all", arguments[id], true)
			if err == nil || strings.Contains(err.Error(), providerSecret) || len(f.recorded()) != before+reads[id]+1 {
				t.Errorf("%s with %d = %v after %d requests, want one write without provider text", id, code, err,
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
	// A client-side deadline is a timeout after exactly one write.
	stall := newChangeStall(t)
	f.failure = func(w http.ResponseWriter, r *http.Request) bool {
		if r.Method == http.MethodGet && strings.HasPrefix(strings.ToLower(r.URL.Path), assignRepositoryPrefix) {
			return assignAnswer(noStatus)(w, r)
		}
		return stall.route(w, r)
	}
	for _, id := range codeSecurityAssignToolIDs {
		ctx, cancel := stall.context()
		before := len(f.recorded())
		_, err := core.Invoke(ctx, application.InvokeRequest{Operation: id, Connection: "w-all", Confirmed: true,
			Arguments: json.RawMessage(arguments[id])})
		cancel()
		if classOf(err) != provider.ClassTimeout || len(f.recorded()) != before+reads[id]+1 {
			t.Errorf("%s deadline = %v after %d requests, want one timeout", id, err, len(f.recorded())-before)
		}
	}
}

// The identifiers come only from reading each repository: a repository GitHub does not know, or reports under
// another name, ends the call before any write.
func TestCodeSecurityAssignmentsTakeIdentifiersOnlyFromTheReads(t *testing.T) {
	f, core, _ := codeSecurityAssignCore(t, noStatus)
	for _, id := range []string{codeSecurityConfigurationsAttach.ID, codeSecurityConfigurationsDetach.ID} {
		for _, name := range []string{"octo-org/renamed", "octo-org/unknown"} {
			arguments := `{"owner":"orgs/octo-org","configuration_id":12,"scope":"selected","selected_repositories":["` + name + `"]}`
			if id == codeSecurityConfigurationsDetach.ID {
				arguments = `{"owner":"orgs/octo-org","selected_repositories":["` + name + `"]}`
			}
			before := len(f.recorded())
			_, err := invoke(t, core, id, "w-all", arguments, true)
			if err == nil || strings.Contains(err.Error(), providerSecret) {
				t.Errorf("%s with %s = %v, want a refusal without provider text", id, name, err)
			}
			for _, request := range f.recorded()[before:] {
				if request.method != http.MethodGet {
					t.Errorf("%s with %s sent %s %s after a failed read", id, name, request.method, request.path)
				}
			}
		}
	}
}
