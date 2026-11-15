package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
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
	privateRegistryRoute = "/api/v3/orgs/octo-org/private-registries"
	// registryCanary is a value the simulated server reflects in members GitHub never documents; it must
	// never reach an answer.
	registryCanary = "sealed-canary-value-7f3a"
)

func privateRegistryJSON(name string, ids string) string {
	selected := ""
	visibility := "all"
	if ids != "" {
		visibility = "selected"
		selected = `,"selected_repository_ids":[` + ids + `]`
	}
	return fmt.Sprintf(`{"name":%q,"registry_type":"maven_repository","username":"deploy-bot",`+
		`"replaces_base":true,"visibility":%q%s,"created_at":"2026-10-01T00:00:00Z","updated_at":"2026-10-02T00:00:00Z",`+
		`"auth_type":"oidc_aws","url":"https://registry.invalid/maven","tenant_id":"tenant-1","client_id":"client-1",`+
		`"aws_region":"eu-west-1","account_id":"123456789012","role_name":"deploy","domain":"corp","domain_owner":"999",`+
		`"jfrog_oidc_provider_name":"jf","audience":"aud","identity_mapping_name":"map","namespace":"ns",`+
		`"service_slug":"slug","api_host":"api.cloudsmith.invalid","workload_identity_provider":"projects/1/x",`+
		`"service_account":"sa@example.invalid","encrypted_value":%q,"key_id":%q}`,
		name, visibility, selected, registryCanary, registryCanary)
}

// privateRegistryFake answers the registry routes and the repository lookups by id. Names ending in 403, 404,
// or 504 answer the matching refusals with provider text that must never reach a message.
type privateRegistryFake struct {
	status int
	// big is the selected_repository_ids of the registry BIG_TOKEN.
	big string
}

func (p *privateRegistryFake) route(w http.ResponseWriter, r *http.Request) bool {
	w.Header().Set("Content-Type", "application/json")
	if rest, ok := strings.CutPrefix(r.URL.Path, "/api/v3/repositories/"); ok {
		switch rest {
		case "1":
			fmt.Fprint(w, `{"id":1,"full_name":"octo-org/open"}`)
		case "2":
			fmt.Fprint(w, `{"id":2,"full_name":"Octo-Org/Secret"}`)
		case "4":
			fmt.Fprint(w, `{"id":99,"full_name":"octo-org/open"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found `+providerSecret+`"}`)
		}
		return true
	}
	if !strings.HasPrefix(r.URL.Path, privateRegistryRoute) {
		return false
	}
	if p.status != 0 {
		w.WriteHeader(p.status)
		fmt.Fprint(w, `{"message":"refused `+providerSecret+`"}`)
		return true
	}
	rest := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, privateRegistryRoute), "/")
	switch {
	case r.Method == http.MethodDelete:
		w.WriteHeader(http.StatusNoContent)
	case rest == "":
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		all := []string{privateRegistryJSON("MAVEN_TOKEN", ""), privateRegistryJSON("NPM_TOKEN", "1,2,3,4"),
			privateRegistryJSON("GO_TOKEN", "1")}
		perPage, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
		if perPage < 1 {
			perPage = 30
		}
		from := (page - 1) * perPage
		to := min(from+perPage, len(all))
		var batch []string
		if from < len(all) {
			batch = all[from:to]
		}
		fmt.Fprintf(w, `{"total_count":%d,"configurations":[%s]}`, len(all), strings.Join(batch, ","))
	case rest == "BIG_TOKEN":
		fmt.Fprint(w, privateRegistryJSON("BIG_TOKEN", p.big))
	case rest == "NPM_TOKEN":
		fmt.Fprint(w, privateRegistryJSON("NPM_TOKEN", "1,2,3,4"))
	default:
		fmt.Fprint(w, privateRegistryJSON(rest, ""))
	}
	return true
}

func privateRegistryCore(t *testing.T, fake *privateRegistryFake) (*fakeGitHub, *application.Core, *int) {
	t.Helper()
	f := &fakeGitHub{failure: fake.route}
	cfg := discoveryConfig(serve(t, f))
	add := func(name string, targets, excludes, tools []string) {
		cfg.Connections[name] = config.Connection{Service: "gh", Credential: "gh-reader", Effects: config.Effects(),
			Targets: targets, Exclude: excludes, Tools: tools}
	}
	add("ex-repo", []string{"orgs/octo-org"}, []string{"repos/octo-org/secret"}, nil)
	add("ex-owner", nil, []string{"orgs/octo-org"}, nil)
	add("repo-only", []string{"repos/octo-org/open"}, nil, nil)
	add("w-org", []string{"orgs/octo-org"}, nil, privateRegistryToolIDs)
	add("w-open", nil, nil, privateRegistryToolIDs)
	add("w-user", []string{"users/octocat"}, nil, privateRegistryToolIDs)
	add("w-ex-owner", nil, []string{"orgs/octo-org"}, privateRegistryToolIDs)
	add("w-narrow", []string{"orgs/octo-org"}, []string{"repos/octo-org/secret"}, privateRegistryToolIDs)
	reads := 0
	red := &redact.Redactor{}
	return f, application.New(registry(t), cfg, resolver(red, &reads), red), &reads
}

func TestPrivateRegistryReadsAnswerEveryDocumentedMemberAndNoValue(t *testing.T) {
	f, core, _ := privateRegistryCore(t, &privateRegistryFake{})
	got, err := invoke(t, core, privateRegistriesGet.ID, "org", `{"secret_name":"MAVEN_TOKEN"}`, false)
	var detail map[string]any
	if err != nil || json.Unmarshal(got, &detail) != nil {
		t.Fatalf("get = %s, %v", got, err)
	}
	// Every documented member of the detail read reaches the answer.
	for _, field := range privateRegistryMembers {
		if field.Name == "selected_repository_ids" || field.Name == "withheld" {
			continue
		}
		if _, ok := detail[field.Name]; !ok {
			t.Errorf("get lacks the documented member %s: %s", field.Name, got)
		}
	}
	for _, name := range []string{"owner"} {
		if _, ok := detail[name]; !ok {
			t.Errorf("get lacks %s: %s", name, got)
		}
	}
	if detail["url"] != "https://registry.invalid/maven" || detail["auth_type"] != "oidc_aws" ||
		detail["service_account"] != "sa@example.invalid" {
		t.Errorf("get = %s, want the address and the authentication members", got)
	}
	if detail["owner"] != "orgs/octo-org" || detail["replaces_base"] != true || detail["visibility"] != "all" {
		t.Errorf("get = %s, want the organization and the documented members", got)
	}
	requests := f.recorded()
	if last := requests[len(requests)-1]; last.method != http.MethodGet || last.path != privateRegistryRoute+"/MAVEN_TOKEN" {
		t.Errorf("get sent %+v, want one GET of the registry", last)
	}

	got, err = invoke(t, core, privateRegistriesList.ID, "org", `{}`, false)
	if err != nil || !strings.Contains(string(got), `"total_count":3`) ||
		!strings.Contains(string(got), `"selected_repository_ids":[1,2,3,4]`) ||
		!strings.Contains(string(got), `"owner":"orgs/octo-org"`) || strings.Contains(string(got), `"withheld"`) {
		t.Fatalf("list = %s, %v", got, err)
	}
	// The server reflected a value and a key id; neither is an output member. The address is answered as text
	// and never requested.
	for _, answer := range []string{string(got), string(mustInvoke(t, core, privateRegistriesGet.ID, "org",
		`{"secret_name":"NPM_TOKEN"}`))} {
		for _, leaked := range []string{registryCanary, "encrypted_value", "key_id"} {
			if strings.Contains(answer, leaked) {
				t.Errorf("answer contains %q: %s", leaked, answer)
			}
		}
	}
	for _, request := range f.recorded() {
		if !strings.HasPrefix(request.path, privateRegistryRoute) || strings.Contains(request.path, "public-key") {
			t.Errorf("request %s reached beyond the registries", request.path)
		}
	}
}

func mustInvoke(t *testing.T, core *application.Core, id, connection, arguments string) json.RawMessage {
	t.Helper()
	got, err := invoke(t, core, id, connection, arguments, false)
	if err != nil {
		t.Fatalf("%s = %v", id, err)
	}
	return got
}

func TestPrivateRegistryListPagesByNumber(t *testing.T) {
	f, core, _ := privateRegistryCore(t, &privateRegistryFake{})
	before := len(f.recorded())
	got, err := invoke(t, core, privateRegistriesList.ID, "org", `{"limit":2}`, false)
	var first struct {
		Configurations []map[string]any `json:"configurations"`
		NextCursor     string           `json:"next_cursor"`
		HasMore        bool             `json:"has_more"`
	}
	if err != nil || json.Unmarshal(got, &first) != nil || len(first.Configurations) != 2 || !first.HasMore ||
		first.NextCursor == "" {
		t.Fatalf("first batch = %s, %v", got, err)
	}
	got, err = invoke(t, core, privateRegistriesList.ID, "org", `{"cursor":"`+first.NextCursor+`"}`, false)
	var second struct {
		Configurations []map[string]any `json:"configurations"`
		HasMore        bool             `json:"has_more"`
	}
	if err != nil || json.Unmarshal(got, &second) != nil || len(second.Configurations) != 1 || second.HasMore ||
		second.Configurations[0]["name"] != "GO_TOKEN" {
		t.Fatalf("second batch = %s, %v", got, err)
	}
	requests := f.recorded()[before:]
	if len(requests) != 2 || requests[0].query != "page=1&per_page=2" || requests[1].query != "page=2&per_page=2" {
		t.Errorf("requests = %+v, want page 1 then page 2 with the batch size of the first", requests)
	}
	// A cursor of another organization's list is refused before any request.
	before = len(f.recorded())
	if _, err := invoke(t, core, privateRegistriesList.ID, "open", `{"owner":"orgs/other","cursor":"`+first.NextCursor+`"}`,
		false); err == nil || len(f.recorded()) != before {
		t.Errorf("foreign cursor = %v, want a refusal without a request", err)
	}
}

func TestPrivateRegistryRepositoriesOutsideTheTargetsAreOnlyCounted(t *testing.T) {
	fake := &privateRegistryFake{}
	f, core, _ := privateRegistryCore(t, fake)
	before := len(f.recorded())
	got, err := invoke(t, core, privateRegistriesGet.ID, "ex-repo", `{"secret_name":"NPM_TOKEN"}`, false)
	if err != nil || !strings.Contains(string(got), `"selected_repository_ids":[1]`) ||
		!strings.Contains(string(got), `"withheld":3`) {
		t.Fatalf("get = %s, %v, want the one repository inside and three withheld", got, err)
	}
	// An excluded repository, an unknown one, and one whose lookup names another id are all withheld.
	for _, request := range f.recorded()[before:] {
		if request.method != http.MethodGet {
			t.Errorf("request %+v is no read", request)
		}
	}
	// Without an exclusion of one of the organization's repositories nothing is looked up.
	before = len(f.recorded())
	if _, err := invoke(t, core, privateRegistriesGet.ID, "org", `{"secret_name":"NPM_TOKEN"}`, false); err != nil {
		t.Fatal(err)
	}
	if n := len(f.recorded()) - before; n != 1 {
		t.Errorf("get without exclusions sent %d requests, want exactly one", n)
	}
	// The lookups are bounded: ids beyond the budget are withheld.
	big := make([]string, privateRegistryLookupBudget+5)
	for i := range big {
		big[i] = strconv.Itoa(1000 + i)
	}
	fake.big = strings.Join(big, ",")
	before = len(f.recorded())
	got, err = invoke(t, core, privateRegistriesGet.ID, "ex-repo", `{"secret_name":"BIG_TOKEN"}`, false)
	if err != nil || strings.Contains(string(got), "selected_repository_ids") ||
		!strings.Contains(string(got), `"withheld":`+strconv.Itoa(privateRegistryLookupBudget+5)) {
		t.Errorf("big get = %s, %v, want every unknown repository withheld", got, err)
	}
	if n := len(f.recorded()) - before; n != 1+privateRegistryLookupBudget {
		t.Errorf("big get sent %d requests, want the read and %d lookups", n, privateRegistryLookupBudget)
	}
}

func TestPrivateRegistryToolsCheckEverythingBeforeIO(t *testing.T) {
	f, core, reads := privateRegistryCore(t, &privateRegistryFake{})
	type call struct{ id, connection, arguments string }
	var calls []call
	for _, id := range privateRegistryToolIDs {
		connection := "org"
		if id == privateRegistriesDelete.ID {
			connection = "w-org"
		}
		named := id != privateRegistriesList.ID
		with := func(owner string) string {
			body := `{` + owner
			if named {
				body += `,"secret_name":"MAVEN_TOKEN"`
			}
			return strings.TrimPrefix(body, `{,`) + `}`
		}
		// Only an organization an owner target names is accepted, and not an excluded one.
		calls = append(calls,
			call{id, connection, with(`"owner":"orgs/other"`)},
			call{id, connection, with(`"owner":"users/octocat"`)},
			call{id, connection, with(`"owner":"octo-org"`)},
			call{id, connection, with(`"owner":"orgs/octo-org/projects/1"`)},
		)
		if id == privateRegistriesDelete.ID {
			calls = append(calls,
				call{id, "w-open", with(``)},
				call{id, "w-user", with(``)},
				call{id, "w-user", with(`"owner":"users/octocat"`)},
				call{id, "w-ex-owner", with(`"owner":"orgs/octo-org"`)})
		} else {
			calls = append(calls,
				call{id, "open", with(``)},
				call{id, "user", with(`"owner":"users/octocat"`)},
				call{id, "repo-only", with(`"owner":"orgs/octo-org"`)},
				call{id, "ex-owner", with(`"owner":"orgs/octo-org"`)})
		}
		calls = append(calls, call{id, connection, `{"owner":"orgs/octo-org","unknown":1` + map[bool]string{true: `,"secret_name":"MAVEN_TOKEN"`}[named] + `}`})
	}
	// The secret name is one plain segment; no name reaches another route or the public key route.
	for _, id := range []string{privateRegistriesGet.ID, privateRegistriesDelete.ID} {
		connection := "org"
		if id == privateRegistriesDelete.ID {
			connection = "w-org"
		}
		for _, name := range []string{"", "..", ".", "a/b", "a%2Fb", "public-key", "a b", "a?x=1", "a#b", "ä",
			strings.Repeat("A", 256)} {
			encoded, _ := json.Marshal(map[string]any{"owner": "orgs/octo-org", "secret_name": name})
			calls = append(calls, call{id, connection, string(encoded)})
		}
		calls = append(calls,
			call{id, connection, `{"owner":"orgs/octo-org"}`},
			call{id, connection, `{"owner":"orgs/octo-org","secret_name":5}`},
			call{id, connection, `{"owner":"orgs/octo-org","secret_name":"A","limit":1}`})
	}
	calls = append(calls,
		call{privateRegistriesList.ID, "org", `{"limit":0}`},
		call{privateRegistriesList.ID, "org", `{"limit":101}`},
		call{privateRegistriesList.ID, "org", `{"cursor":"x"}`},
		call{privateRegistriesList.ID, "org", `{"secret_name":"A"}`})
	for _, tt := range calls {
		*reads = 0
		before := len(f.recorded())
		_, err := invoke(t, core, tt.id, tt.connection, tt.arguments, true)
		if tt.arguments == `{"limit":0}` {
			// A zero limit is the default, not a refusal.
			continue
		}
		if err == nil || !(isInvalidRequest(err) || strings.Contains(err.Error(), "invalid")) {
			t.Errorf("%s(%s) on %s = %v, want an invalid request", tt.id, tt.arguments, tt.connection, err)
		}
		if *reads != 0 || len(f.recorded()) != before {
			t.Errorf("%s(%s) on %s reached the credential or GitHub", tt.id, tt.arguments, tt.connection)
		}
	}
}

func TestPrivateRegistryDeleteIsOfferedOnlyListedAndConfirmed(t *testing.T) {
	f, core, reads := privateRegistryCore(t, &privateRegistryFake{})
	arguments := `{"owner":"orgs/octo-org","secret_name":"MAVEN_TOKEN"}`
	var unsupported *capability.UnsupportedError
	for _, connection := range []string{"org", "open"} {
		if _, err := core.Describe(application.DescribeRequest{Operation: privateRegistriesDelete.ID, Connection: connection}); !errors.As(err, &unsupported) {
			t.Errorf("describe on %s = %v, want unsupported", connection, err)
		}
		*reads = 0
		before := len(f.recorded())
		if _, err := invoke(t, core, privateRegistriesDelete.ID, connection, arguments, true); !errors.As(err, &unsupported) {
			t.Errorf("invoke on %s = %v, want unsupported", connection, err)
		}
		if *reads != 0 || len(f.recorded()) != before {
			t.Errorf("delete on %s reached the credential or GitHub", connection)
		}
	}
	// The two reads are offered without a tools entry.
	for _, id := range privateRegistryReadToolIDs {
		if _, err := core.Describe(application.DescribeRequest{Operation: id, Connection: "org"}); err != nil {
			t.Errorf("describe %s on org = %v", id, err)
		}
	}
	before := len(f.recorded())
	unconfirmed := &application.ConfirmationRequiredError{}
	if _, err := invoke(t, core, privateRegistriesDelete.ID, "w-org", arguments, false); !errors.As(err, &unconfirmed) ||
		len(f.recorded()) != before {
		t.Errorf("delete without confirmation = %v after %d requests, want confirmation-required and none",
			err, len(f.recorded())-before)
	}
	got, err := invoke(t, core, privateRegistriesDelete.ID, "w-org", arguments, true)
	if err != nil || !strings.Contains(string(got), `"deleted":true`) || !strings.Contains(string(got), `"name":"MAVEN_TOKEN"`) ||
		!strings.Contains(string(got), `"owner":"orgs/octo-org"`) {
		t.Fatalf("delete = %s, %v", got, err)
	}
	requests := f.recorded()[before:]
	if len(requests) != 1 || requests[0].method != http.MethodDelete || requests[0].path != privateRegistryRoute+"/MAVEN_TOKEN" {
		t.Errorf("delete sent %+v, want exactly one DELETE of the registry", requests)
	}
	// A registry selected for repositories outside the targets is deleted without reading them.
	before = len(f.recorded())
	if _, err := invoke(t, core, privateRegistriesDelete.ID, "w-narrow", `{"owner":"orgs/octo-org","secret_name":"NPM_TOKEN"}`,
		true); err != nil {
		t.Errorf("delete on w-narrow = %v", err)
	}
	if n := len(f.recorded()) - before; n != 1 {
		t.Errorf("delete on w-narrow sent %d requests, want one", n)
	}
}

func TestPrivateRegistryFailuresAreResultsWithoutProviderTextAndChangesAreNeverRepeated(t *testing.T) {
	fake := &privateRegistryFake{}
	f, core, _ := privateRegistryCore(t, fake)
	arguments := map[string]string{privateRegistriesList.ID: `{"owner":"orgs/octo-org"}`,
		privateRegistriesGet.ID:    `{"owner":"orgs/octo-org","secret_name":"MAVEN_TOKEN"}`,
		privateRegistriesDelete.ID: `{"owner":"orgs/octo-org","secret_name":"MAVEN_TOKEN"}`}
	classes := map[int]provider.Class{403: provider.ClassPermission, 404: provider.ClassNotFound,
		502: provider.ClassProviderError, 504: provider.ClassTimeout}
	for _, id := range privateRegistryToolIDs {
		connection := "org"
		if id == privateRegistriesDelete.ID {
			connection = "w-org"
		}
		for code, class := range classes {
			fake.status = code
			before := len(f.recorded())
			_, err := invoke(t, core, id, connection, arguments[id], true)
			if err == nil || strings.Contains(err.Error(), providerSecret) || len(f.recorded()) != before+1 {
				t.Errorf("%s with %d = %v after %d requests, want one result without provider text", id, code, err,
					len(f.recorded())-before)
				continue
			}
			if got := classOf(err); got != class {
				t.Errorf("%s with %d has class %q, want %q", id, code, got, class)
			}
			if code == 403 && !strings.Contains(err.Error(), "admin:org") {
				t.Errorf("%s with 403 = %v, want the permission named", id, err)
			}
			if id == privateRegistriesDelete.ID && code == 502 && !strings.Contains(err.Error(), strings.TrimSpace(uncertain)) {
				t.Errorf("delete with 502 = %v, want the uncertain outcome said", err)
			}
		}
	}
	fake.status = 0
	// A client-side deadline is a timeout after exactly one request.
	f.failure = func(w http.ResponseWriter, r *http.Request) bool {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
		return true
	}
	for _, id := range privateRegistryToolIDs {
		connection := "org"
		if id == privateRegistriesDelete.ID {
			connection = "w-org"
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2500*time.Millisecond)
		before := len(f.recorded())
		_, err := core.Invoke(ctx, application.InvokeRequest{Operation: id, Connection: connection, Confirmed: true,
			Arguments: json.RawMessage(arguments[id])})
		cancel()
		if classOf(err) != provider.ClassTimeout || len(f.recorded()) != before+1 {
			t.Errorf("%s deadline = %v after %d requests, want one timeout", id, err, len(f.recorded())-before)
		}
	}
}

func TestPrivateRegistrySubjectNamesOnlyAllowedNames(t *testing.T) {
	for _, tt := range []struct {
		rest []string
		want string
	}{
		{nil, "the private registries"},
		{[]string{""}, "the private registries"},
		{[]string{"MAVEN_TOKEN"}, "private registry MAVEN_TOKEN"},
		{[]string{"public-key"}, "the private registries"},
		{[]string{"a%2Fb"}, "the private registries"},
	} {
		if got := privateRegistrySubject(tt.rest); got != tt.want {
			t.Errorf("privateRegistrySubject(%v) = %q, want %q", tt.rest, got, tt.want)
		}
	}
}
