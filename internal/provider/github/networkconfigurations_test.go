package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"sort"
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
	networkRoute        = "/api/v3/orgs/octo-org/settings/network-configurations"
	networkSettingsRoot = "/api/v3/orgs/octo-org/settings/network-settings"
	networkCanary       = "network-provider-text-canary-4c7a"
	networkUndocumented = "network-undocumented-canary-91d2"
)

// networkConfigurationJSON is a configuration with every documented member plus a key that must not reach a
// result.
func networkConfigurationJSON(id string) string {
	return fmt.Sprintf(`{"id":%q,"name":"cfg-%s","compute_service":"actions","network_settings_ids":["S1","S2"],`+
		`"failover_network_settings_ids":["F1"],"failover_network_enabled":true,"created_on":"2024-04-26T11:31:07Z",`+
		`"subscription":%q}`, id, id, networkUndocumented)
}

type fakeNetwork struct {
	// status makes every network route answer with it and a body that carries a canary.
	status int
	// header is added to a refusal.
	header http.Header
	// total is the number of configurations the list holds.
	total int
	// override replaces the answer of the single routes with this JSON object.
	override string
}

func (n *fakeNetwork) route(w http.ResponseWriter, r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, networkRoute) && !strings.HasPrefix(r.URL.Path, networkSettingsRoot) {
		return false
	}
	w.Header().Set("Content-Type", "application/json")
	if n.status != 0 {
		for key, values := range n.header {
			w.Header()[key] = values
		}
		w.WriteHeader(n.status)
		fmt.Fprintf(w, `{"message":%q}`, networkCanary)
		return true
	}
	answer := func(fallback string) {
		if n.override != "" {
			fmt.Fprint(w, n.override)
			return
		}
		fmt.Fprint(w, fallback)
	}
	switch {
	case strings.HasPrefix(r.URL.Path, networkSettingsRoot):
		answer(`{"id":"220F78DA","network_configuration_id":"934E208B","name":"my_network_settings",` +
			`"subnet_id":"/subscriptions/1/resourceGroups/rg/providers/Microsoft.Network/virtualNetworks/v/subnets/s",` +
			`"region":"eastus","extra":"` + networkUndocumented + `"}`)
	case r.URL.Path == networkRoute && r.Method == http.MethodGet:
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		perPage, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
		var items []string
		for i := (page - 1) * perPage; i < page*perPage && i < n.total; i++ {
			items = append(items, networkConfigurationJSON("C"+strconv.Itoa(i)))
		}
		fmt.Fprintf(w, `{"total_count":%d,"network_configurations":[%s]}`, n.total, strings.Join(items, ","))
	case r.Method == http.MethodPost:
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, networkConfigurationJSON("NEW"))
	case r.Method == http.MethodDelete:
		w.WriteHeader(http.StatusNoContent)
	default:
		answer(networkConfigurationJSON("ONE"))
	}
	return true
}

func networkCore(t *testing.T) (*fakeGitHub, *fakeNetwork, *application.Core, *int) {
	t.Helper()
	n := &fakeNetwork{total: 3}
	f := &fakeGitHub{failure: n.route}
	cfg := coreConfig(serve(t, f))
	read := []config.Effect{config.EffectRead}
	add := func(name string, effects []config.Effect, tools []string, targets, excludes []string) {
		cfg.Connections[name] = config.Connection{Service: "gh", Credential: "gh-reader", Effects: effects,
			Tools: tools, Targets: targets, Exclude: excludes}
	}
	for prefix, effects := range map[string][]config.Effect{"r": read, "w": config.Effects()} {
		var tools []string
		if prefix == "w" {
			tools = networkConfigurationChangeIDs
		}
		add(prefix+"-org", effects, tools, []string{"orgs/octo-org"}, nil)
		add(prefix+"-open", effects, tools, nil, nil)
		add(prefix+"-user", effects, tools, []string{"users/octocat"}, nil)
		add(prefix+"-repo", effects, tools, []string{"repos/octo-org/open"}, nil)
		add(prefix+"-ex-owner", effects, tools, nil, []string{"orgs/octo-org"})
		add(prefix+"-narrow", effects, tools, []string{"orgs/octo-org", "repos/octo-org/*"}, []string{"repos/octo-org/secret"})
	}
	add("unlisted", config.Effects(), nil, []string{"orgs/octo-org"}, nil)
	reads := 0
	red := &redact.Redactor{}
	return f, n, application.New(registry(t), cfg, resolver(red, &reads), red), &reads
}

// networkCall is one call of a tool with the connection that offers it.
func networkConnection(id string) string {
	if slicesContainsString(networkConfigurationChangeIDs, id) {
		return "w"
	}
	return "r"
}

func slicesContainsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// networkArgumentsOf is a valid call of each tool.
var networkArgumentsOf = map[string]string{
	networkConfigurationsList.ID:   `{"owner":"orgs/octo-org"}`,
	networkConfigurationsGet.ID:    `{"owner":"orgs/octo-org","network_configuration_id":"C1"}`,
	networkSettingsGet.ID:          `{"owner":"orgs/octo-org","network_settings_id":"S1"}`,
	networkConfigurationsCreate.ID: `{"owner":"orgs/octo-org","name":"x","network_settings_ids":["S1"]}`,
	networkConfigurationsUpdate.ID: `{"owner":"orgs/octo-org","network_configuration_id":"C1","name":"y"}`,
	networkConfigurationsDelete.ID: `{"owner":"orgs/octo-org","network_configuration_id":"C1"}`,
}

func TestNetworkReadsAnswerEveryDocumentedMemberAndNoOther(t *testing.T) {
	_, n, core, _ := networkCore(t)
	out, err := invoke(t, core, networkConfigurationsGet.ID, "r-org", networkArgumentsOf[networkConfigurationsGet.ID], false)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"id":"ONE"`, `"name":"cfg-ONE"`, `"compute_service":"actions"`,
		`"network_settings_ids":["S1","S2"]`, `"failover_network_settings_ids":["F1"]`,
		`"failover_network_enabled":true`, `"created_on":"2024-04-26T11:31:07Z"`, `"owner":"orgs/octo-org"`} {
		if !strings.Contains(string(out), want) {
			t.Errorf("get lacks %s: %s", want, out)
		}
	}
	if strings.Contains(string(out), networkUndocumented) {
		t.Errorf("get shows an undocumented member: %s", out)
	}

	out, err = invoke(t, core, networkConfigurationsList.ID, "r-org", networkArgumentsOf[networkConfigurationsList.ID], false)
	if err != nil || !strings.Contains(string(out), `"total_count":3`) || !strings.Contains(string(out), `"id":"C2"`) ||
		!strings.Contains(string(out), `"has_more":false`) || strings.Contains(string(out), networkUndocumented) {
		t.Errorf("list = %s, %v", out, err)
	}

	out, err = invoke(t, core, networkSettingsGet.ID, "r-org", networkArgumentsOf[networkSettingsGet.ID], false)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"id":"220F78DA"`, `"network_configuration_id":"934E208B"`, `"name":"my_network_settings"`,
		`"subnet_id":"/subscriptions/1/resourceGroups/rg/providers/Microsoft.Network/virtualNetworks/v/subnets/s"`,
		`"region":"eastus"`} {
		if !strings.Contains(string(out), want) {
			t.Errorf("network settings lack %s: %s", want, out)
		}
	}
	if strings.Contains(string(out), networkUndocumented) {
		t.Errorf("network settings show an undocumented member: %s", out)
	}

	// A member GitHub sends as null or leaves out is absent, and a long identifier list is cut.
	ids := make([]string, 101)
	for i := range ids {
		ids[i] = fmt.Sprintf("%q", "I"+strconv.Itoa(i))
	}
	n.override = `{"id":"ONE","name":"n","created_on":null,"network_settings_ids":[` + strings.Join(ids, ",") + `]}`
	out, err = invoke(t, core, networkConfigurationsGet.ID, "r-org", networkArgumentsOf[networkConfigurationsGet.ID], false)
	if err != nil || strings.Contains(string(out), "created_on") || strings.Contains(string(out), `"I100"`) ||
		!strings.Contains(string(out), `"network_settings_ids_truncated":true`) {
		t.Errorf("get with null and long list = %s, %v", out, err)
	}
	// An answer without an identifier is no configuration.
	n.override = `{"name":"n"}`
	if _, err := invoke(t, core, networkConfigurationsGet.ID, "r-org", networkArgumentsOf[networkConfigurationsGet.ID], false); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("get without id = %v, want an invalid response", err)
	}
}

func TestNetworkListPagesByCursorBoundToTheOrganization(t *testing.T) {
	f, n, core, _ := networkCore(t)
	n.total = 3
	out, err := invoke(t, core, networkConfigurationsList.ID, "r-org", `{"owner":"orgs/octo-org","limit":2}`, false)
	if err != nil {
		t.Fatal(err)
	}
	var first struct {
		Configurations []struct{ ID string } `json:"network_configurations"`
		NextCursor     string                `json:"next_cursor"`
		HasMore        bool                  `json:"has_more"`
	}
	if json.Unmarshal(out, &first) != nil || len(first.Configurations) != 2 || !first.HasMore || first.NextCursor == "" {
		t.Fatalf("first batch = %s", out)
	}
	out, err = invoke(t, core, networkConfigurationsList.ID, "r-org",
		`{"owner":"orgs/octo-org","cursor":"`+first.NextCursor+`"}`, false)
	var second struct {
		Configurations []struct{ ID string } `json:"network_configurations"`
		HasMore        bool                  `json:"has_more"`
	}
	if err != nil || json.Unmarshal(out, &second) != nil || len(second.Configurations) != 1 || second.HasMore ||
		second.Configurations[0].ID != "C2" {
		t.Fatalf("second batch = %s, %v", out, err)
	}
	requests := f.recorded()
	last := requests[len(requests)-1]
	query, _ := url.ParseQuery(last.query)
	if last.method != http.MethodGet || last.path != networkRoute || query.Get("page") != "2" || query.Get("per_page") != "2" {
		t.Errorf("continuation sent %+v, want page 2 of the first batch size", last)
	}
	// A cursor of another list or a forged one is refused.
	for _, cursor := range []string{"AAAA", first.NextCursor + "x"} {
		if _, err := invoke(t, core, networkConfigurationsList.ID, "r-org",
			`{"owner":"orgs/octo-org","cursor":"`+cursor+`"}`, false); err == nil {
			t.Errorf("cursor %q accepted", cursor)
		}
	}
}

func TestNetworkToolsCheckEverythingBeforeIO(t *testing.T) {
	f, _, core, reads := networkCore(t)
	type call struct{ id, connection, arguments string }
	var calls []call
	with := func(id string, extra map[string]any) string {
		all := map[string]any{}
		_ = json.Unmarshal([]byte(networkArgumentsOf[id]), &all)
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
	for _, id := range networkConfigurationToolIDs {
		prefix := networkConnection(id)
		// The organization must be an owner target, before a credential is read.
		for _, owner := range []string{"users/octocat", "orgs/other", "octo-org", "repos/octo-org/open"} {
			calls = append(calls, call{id, prefix + "-org", with(id, map[string]any{"owner": owner})})
		}
		calls = append(calls,
			call{id, prefix + "-open", with(id, map[string]any{"owner": nil})},
			call{id, prefix + "-user", with(id, map[string]any{"owner": nil})},
			call{id, prefix + "-user", with(id, map[string]any{"owner": "users/octocat"})},
			call{id, prefix + "-repo", with(id, map[string]any{"owner": nil})},
			call{id, prefix + "-ex-owner", with(id, map[string]any{"owner": "orgs/octo-org"})},
			call{id, prefix + "-org", with(id, map[string]any{"path": "/x"})},
			call{id, prefix + "-org", with(id, map[string]any{"enterprise": "acme"})},
		)
	}
	badIDs := []any{"", "a/b", "../x", "a b", "a?b", "a%2Fb", strings.Repeat("a", 129), 5, true, nil}
	for _, id := range []string{networkConfigurationsGet.ID, networkConfigurationsUpdate.ID, networkConfigurationsDelete.ID} {
		for _, bad := range badIDs {
			calls = append(calls, call{id, networkConnection(id) + "-org", with(id, map[string]any{"network_configuration_id": bad})})
		}
	}
	for _, bad := range badIDs {
		calls = append(calls, call{networkSettingsGet.ID, "r-org", with(networkSettingsGet.ID, map[string]any{"network_settings_id": bad})})
	}
	for _, bad := range []any{0, 101, -1, "5"} {
		calls = append(calls, call{networkConfigurationsList.ID, "r-org", with(networkConfigurationsList.ID, map[string]any{"limit": bad})})
	}
	for _, id := range []string{networkConfigurationsCreate.ID, networkConfigurationsUpdate.ID} {
		c := networkConnection(id) + "-org"
		for _, bad := range []any{"", "a b", "a/b", "ä", strings.Repeat("n", 101), 5} {
			calls = append(calls, call{id, c, with(id, map[string]any{"name": bad})})
		}
		for _, bad := range []any{"codespaces", "Actions", "", 1} {
			calls = append(calls, call{id, c, with(id, map[string]any{"compute_service": bad})})
		}
		for _, bad := range []any{[]any{}, []any{"a", "b"}, []any{"a/b"}, []any{5}, "S1"} {
			calls = append(calls, call{id, c, with(id, map[string]any{"network_settings_ids": bad})})
		}
		for _, bad := range []any{[]any{"a", "b"}, []any{"../x"}, "F1"} {
			calls = append(calls, call{id, c, with(id, map[string]any{"failover_network_settings_ids": bad})})
		}
		calls = append(calls, call{id, c, with(id, map[string]any{"failover_network_enabled": "yes"})})
	}
	calls = append(calls,
		call{networkConfigurationsCreate.ID, "w-org", with(networkConfigurationsCreate.ID, map[string]any{"name": nil})},
		call{networkConfigurationsCreate.ID, "w-org", with(networkConfigurationsCreate.ID, map[string]any{"network_settings_ids": nil})},
		call{networkConfigurationsCreate.ID, "w-org", with(networkConfigurationsCreate.ID, map[string]any{"network_configuration_id": "C1"})},
		call{networkConfigurationsUpdate.ID, "w-org", with(networkConfigurationsUpdate.ID, map[string]any{"name": nil})},
		call{networkConfigurationsDelete.ID, "w-org", with(networkConfigurationsDelete.ID, map[string]any{"name": "x"})},
	)
	for _, tt := range calls {
		*reads = 0
		before := len(f.recorded())
		_, err := invoke(t, core, tt.id, tt.connection, tt.arguments, true)
		if err == nil || !(isInvalidRequest(err) || strings.Contains(err.Error(), "invalid")) {
			t.Errorf("%s(%s) on %s = %v, want an invalid request", tt.id, tt.arguments, tt.connection, err)
		}
		if *reads != 0 || len(f.recorded()) != before {
			t.Errorf("%s(%s) on %s reached the credential or GitHub", tt.id, tt.arguments, tt.connection)
		}
	}
}

func TestNetworkChangesSendExactlyTheCheckedBodyOnce(t *testing.T) {
	f, _, core, _ := networkCore(t)
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
	before := len(f.recorded())
	out, err := invoke(t, core, networkConfigurationsCreate.ID, "w-org", `{"owner":"orgs/octo-org","name":"my-cfg.1_x",`+
		`"compute_service":"actions","network_settings_ids":["23456789ABDCEF1"],"failover_network_settings_ids":["F1"],`+
		`"failover_network_enabled":false}`, true)
	if err != nil || !strings.Contains(string(out), `"id":"NEW"`) || !strings.Contains(string(out), `"owner":"orgs/octo-org"`) ||
		strings.Contains(string(out), networkUndocumented) {
		t.Fatalf("create = %s, %v", out, err)
	}
	check("create", http.MethodPost, networkRoute, before, map[string]any{"name": "my-cfg.1_x", "compute_service": "actions",
		"network_settings_ids": []any{"23456789ABDCEF1"}, "failover_network_settings_ids": []any{"F1"},
		"failover_network_enabled": false})

	// Create without the optional members sends only the required ones.
	before = len(f.recorded())
	if _, err := invoke(t, core, networkConfigurationsCreate.ID, "w-org", networkArgumentsOf[networkConfigurationsCreate.ID], true); err != nil {
		t.Fatal(err)
	}
	check("create minimal", http.MethodPost, networkRoute, before,
		map[string]any{"name": "x", "network_settings_ids": []any{"S1"}})

	// Only the given settings of an update are sent, an empty failover list included, and the answer says that
	// runner groups may be affected.
	before = len(f.recorded())
	out, err = invoke(t, core, networkConfigurationsUpdate.ID, "w-org",
		`{"owner":"orgs/octo-org","network_configuration_id":"C1","failover_network_settings_ids":[],"compute_service":"none"}`, true)
	if err != nil || !strings.Contains(string(out), `"note":"Runner groups`) || !strings.Contains(string(out), `"id":"ONE"`) {
		t.Fatalf("update = %s, %v", out, err)
	}
	check("update", http.MethodPatch, networkRoute+"/C1", before,
		map[string]any{"failover_network_settings_ids": []any{}, "compute_service": "none"})

	before = len(f.recorded())
	out, err = invoke(t, core, networkConfigurationsDelete.ID, "w-org", networkArgumentsOf[networkConfigurationsDelete.ID], true)
	if err != nil || !strings.Contains(string(out), `"deleted":true`) || !strings.Contains(string(out), `"note":"Runner groups`) ||
		!strings.Contains(string(out), `"owner":"orgs/octo-org"`) {
		t.Fatalf("delete = %s, %v", out, err)
	}
	check("delete", http.MethodDelete, networkRoute+"/C1", before, nil)

	// The routes are fixed: an identifier cannot leave its path segment.
	for _, requests := range f.recorded() {
		if strings.Contains(requests.path, "..") || strings.Contains(requests.query, "x") && requests.method != http.MethodGet {
			t.Errorf("request %+v leaves the fixed route", requests)
		}
	}
}

func TestNetworkChangesAreOfferedOnlyListedAndConfirmed(t *testing.T) {
	f, _, core, reads := networkCore(t)
	for _, id := range networkConfigurationChangeIDs {
		var unsupported *capability.UnsupportedError
		for _, connection := range []string{"unlisted", "r-org"} {
			if _, err := core.Describe(application.DescribeRequest{Operation: id, Connection: connection}); !errors.As(err, &unsupported) {
				t.Errorf("describe %s on %s = %v, want unsupported", id, connection, err)
			}
			*reads = 0
			before := len(f.recorded())
			if _, err := invoke(t, core, id, connection, networkArgumentsOf[id], true); !errors.As(err, &unsupported) {
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
		if _, err := invoke(t, core, id, "w-org", networkArgumentsOf[id], false); !errors.As(err, &unconfirmed) {
			t.Errorf("%s without confirmation = %v, want confirmation-required", id, err)
		}
		if len(f.recorded()) != before {
			t.Errorf("%s reached GitHub before confirmation", id)
		}
	}
	// The reads need no listing.
	for _, id := range []string{networkConfigurationsList.ID, networkConfigurationsGet.ID, networkSettingsGet.ID} {
		if _, err := invoke(t, core, id, "r-org", networkArgumentsOf[id], false); err != nil {
			t.Errorf("%s on r-org = %v", id, err)
		}
	}
}

func TestNetworkRefusalsAreResultsWithoutProviderTextAndNeverRepeated(t *testing.T) {
	f, n, core, _ := networkCore(t)
	for _, id := range networkConfigurationToolIDs {
		change := slicesContainsString(networkConfigurationChangeIDs, id)
		connection := networkConnection(id) + "-org"
		// The plan limit and a missing right or target end as the state, never as an error.
		for _, code := range []int{http.StatusForbidden, http.StatusNotFound} {
			n.status, n.header = code, nil
			before := len(f.recorded())
			out, err := invoke(t, core, id, connection, networkArgumentsOf[id], true)
			if err != nil || !strings.Contains(string(out), `"state":"unavailable"`) || !strings.Contains(string(out), `"reason":`) ||
				strings.Contains(string(out), networkCanary) || len(f.recorded()) != before+1 {
				t.Errorf("%s with %d = %s, %v after %d requests, want the state and one request", id, code, out, err,
					len(f.recorded())-before)
			}
			if change != strings.Contains(string(out), "nothing was changed") {
				t.Errorf("%s with %d = %s, want nothing-was-changed said for changes only", id, code, out)
			}
			if strings.Contains(string(out), `"deleted":true`) || strings.Contains(string(out), `"note"`) {
				t.Errorf("%s with %d = %s, want no success member", id, code, out)
			}
		}
		// Every other refusal is an error with Qatlas' own message.
		classes := map[int]provider.Class{400: provider.ClassProviderError, 409: provider.ClassProviderError,
			422: provider.ClassProviderError, 502: provider.ClassProviderError, 504: provider.ClassTimeout}
		for code, class := range classes {
			n.status = code
			before := len(f.recorded())
			_, err := invoke(t, core, id, connection, networkArgumentsOf[id], true)
			if err == nil || strings.Contains(err.Error(), networkCanary) || len(f.recorded()) != before+1 {
				t.Errorf("%s with %d = %v after %d requests, want one error without provider text", id, code, err,
					len(f.recorded())-before)
				continue
			}
			if got := classOf(err); got != class {
				t.Errorf("%s with %d has class %q, want %q", id, code, got, class)
			}
			if code == 422 && change && !strings.Contains(err.Error(), "nothing was changed") {
				t.Errorf("%s with 422 = %v, want the rejection explained", id, err)
			}
			if code == 502 && change && !strings.Contains(err.Error(), strings.TrimSpace(uncertain)) {
				t.Errorf("%s with 502 = %v, want the uncertain outcome said", id, err)
			}
		}
		// A rate limit is no plan limit.
		n.status, n.header = http.StatusForbidden, http.Header{"X-Ratelimit-Remaining": {"0"}, "Retry-After": {"1"}}
		if _, err := invoke(t, core, id, connection, networkArgumentsOf[id], true); classOf(err) != provider.ClassRateLimited {
			t.Errorf("%s rate limited = %v, want a rate limit error", id, err)
		}
		n.header = nil
	}
}

func TestNetworkDeadlineIsATimeoutAfterExactlyOneRequest(t *testing.T) {
	f, _, core, _ := networkCore(t)
	f.failure = func(w http.ResponseWriter, r *http.Request) bool {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
		return true
	}
	for _, id := range []string{networkConfigurationsGet.ID, networkConfigurationsCreate.ID} {
		ctx, cancel := context.WithTimeout(context.Background(), 2500*time.Millisecond)
		before := len(f.recorded())
		_, err := core.Invoke(ctx, application.InvokeRequest{Operation: id, Connection: networkConnection(id) + "-org",
			Confirmed: true, Arguments: json.RawMessage(networkArgumentsOf[id])})
		cancel()
		if classOf(err) != provider.ClassTimeout || len(f.recorded()) != before+1 {
			t.Errorf("%s deadline = %v after %d requests, want one timeout", id, err, len(f.recorded())-before)
		}
	}
}

func TestNetworkToolsAreInTheOrganizationAdminGroupAndOfferNoFreeInput(t *testing.T) {
	reg := registry(t)
	var found []string
	for _, descriptor := range reg.Provider(Provider) {
		segment := strings.Split(descriptor.ID, ".")[1]
		if segment != "network-configurations" && segment != "network-settings" {
			continue
		}
		found = append(found, descriptor.ID)
		change := slicesContainsString(networkConfigurationChangeIDs, descriptor.ID)
		if descriptor.Group != "organization-admin" || descriptor.RequiresToolAllowList != change ||
			(!change && descriptor.Risk.Effect != capability.EffectRead) {
			t.Errorf("%s = group %q, listed-only %t, risk %+v", descriptor.ID, descriptor.Group,
				descriptor.RequiresToolAllowList, descriptor.Risk)
		}
		if !strings.Contains(string(descriptor.InputSchema), `"owner"`) {
			t.Errorf("%s offers no owner target", descriptor.ID)
		}
		for _, word := range []string{"enterprise", "base_url", "path", "url", "subnet", "subscription", "resource_group"} {
			if strings.Contains(string(descriptor.InputSchema), `"`+word) {
				t.Errorf("%s input offers %q", descriptor.ID, word)
			}
		}
	}
	sort.Strings(found)
	want := append([]string(nil), networkConfigurationToolIDs...)
	sort.Strings(want)
	if !reflect.DeepEqual(found, want) {
		t.Errorf("network tools = %v, want %v", found, want)
	}
}

// The create and update bodies of GitHub's REST description 1.1.4 hold exactly these members.
func TestNetworkChangesTakeExactlyTheDocumentedMembers(t *testing.T) {
	documented := []string{"compute_service", "failover_network_enabled", "failover_network_settings_ids", "name",
		"network_settings_ids"}
	for _, descriptor := range []capability.Descriptor{networkConfigurationsCreate, networkConfigurationsUpdate} {
		var schema struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if json.Unmarshal(descriptor.InputSchema, &schema) != nil {
			t.Fatalf("%s input schema is not JSON", descriptor.ID)
		}
		var got, arguments []string
		for name := range schema.Properties {
			if name != "owner" && name != "network_configuration_id" {
				got = append(got, name)
			}
		}
		for _, argument := range descriptor.Arguments {
			if argument.Name != "owner" && argument.Name != "network_configuration_id" {
				arguments = append(arguments, argument.Name)
			}
		}
		sort.Strings(got)
		sort.Strings(arguments)
		if !reflect.DeepEqual(got, documented) || !reflect.DeepEqual(arguments, documented) {
			t.Errorf("%s offers %v and describes %v, want %v", descriptor.ID, got, arguments, documented)
		}
	}
}

func TestNetworkSubjectNamesOnlyCheckedParts(t *testing.T) {
	for _, tt := range []struct {
		rest []string
		want string
	}{
		{[]string{"network-configurations"}, "the network configurations"},
		{[]string{"network-configurations", "C1"}, "network configuration C1"},
		{[]string{"network-configurations", "a b"}, "the network configurations"},
		{[]string{"network-settings", "S-1_x"}, "network settings S-1_x"},
		{[]string{"network-settings", "../x"}, "the network settings"},
		{[]string{"other"}, ""},
		{nil, ""},
	} {
		if got := networkSubject(tt.rest); got != tt.want {
			t.Errorf("networkSubject(%v) = %q, want %q", tt.rest, got, tt.want)
		}
	}
}
