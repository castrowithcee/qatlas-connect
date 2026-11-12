package github

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const runnerGroupProviderCanary = "runner-provider-text-canary-7b2e"

// fakeRunnerGroups answers the runner group and hosted runner routes of the organization octo-org, keeps the
// groups, and can refuse every request of one method with a status.
type fakeRunnerGroups struct {
	mu     sync.Mutex
	groups map[int]map[string]any
	status map[string]int
	// answerID makes a read of a group answer another identifier, like a mismatching object.
	answerID int
	// hosted is the list the hosted runner route answers with.
	hosted []any
}

func newRunnerGroup(id int, name, visibility string) map[string]any {
	return map[string]any{"id": float64(id), "name": name, "visibility": visibility, "default": id == 1,
		"selected_repositories_url": "https://api.github.com/orgs/octo-org/actions/runner-groups/" + strconv.Itoa(id) + "/repositories",
		"runners_url":               "https://api.github.com/orgs/octo-org/actions/runner-groups/" + strconv.Itoa(id) + "/runners",
		"hosted_runners_url":        "https://api.github.com/orgs/octo-org/actions/runner-groups/" + strconv.Itoa(id) + "/hosted-runners",
		"network_configuration_id":  "net-1", "inherited": false, "inherited_allows_public_repositories": false,
		"allows_public_repositories": false, "workflow_restrictions_read_only": false,
		"restricted_to_workflows": true, "selected_workflows": []any{"octo-org/example/.github/workflows/deploy.yml@main"}}
}

func serveRunnerGroups(t *testing.T) (*fakeGitHub, *fakeRunnerGroups, string) {
	t.Helper()
	s := &fakeRunnerGroups{status: map[string]int{}, groups: map[int]map[string]any{
		1: newRunnerGroup(1, "Default", "all"), 2: newRunnerGroup(2, "builders", "selected")}}
	s.hosted = []any{map[string]any{"id": 11.0, "name": "large-linux", "runner_group_id": 2.0,
		"image_details":        map[string]any{"id": "ubuntu-latest", "size_gb": 86.0, "display_name": "Ubuntu", "source": "github", "version": "latest"},
		"machine_size_details": map[string]any{"id": "8-core", "cpu_cores": 8.0, "memory_gb": 32.0, "storage_gb": 300.0},
		"status":               "Ready", "platform": "linux-x64", "maximum_runners": 10.0, "public_ip_enabled": true,
		"public_ips":     []any{map[string]any{"enabled": true, "prefix": "203.0.113.0", "length": 28.0}},
		"last_active_on": "2026-11-01T10:00:00Z", "image_gen": false}}
	f := &fakeGitHub{}
	f.failure = func(w http.ResponseWriter, req *http.Request) bool { return s.route(f, w, req) }
	return f, s, serve(t, f)
}

func (s *fakeRunnerGroups) route(f *fakeGitHub, w http.ResponseWriter, req *http.Request) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := strings.TrimPrefix(req.URL.Path, "/api/v3")
	const groups = "/orgs/octo-org/actions/runner-groups"
	hosted := path == "/orgs/octo-org/actions/hosted-runners"
	if !strings.HasPrefix(path, groups) && !hosted {
		return false
	}
	w.Header().Set("Content-Type", "application/json")
	if status := s.status[req.Method]; status != 0 {
		w.WriteHeader(status)
		fmt.Fprintf(w, `{"message":%q}`, runnerGroupProviderCanary)
		return true
	}
	write := func(value any) { data, _ := json.Marshal(value); w.Write(data) }
	if hosted {
		write(map[string]any{"total_count": float64(len(s.hosted)), "runners": s.hosted})
		return true
	}
	if path == groups {
		list := []any{}
		for id := 1; id <= len(s.groups); id++ {
			if req.URL.Query().Get("visible_to_repository") == "" || id == 2 {
				list = append(list, s.groups[id])
			}
		}
		write(map[string]any{"total_count": float64(len(list)), "runner_groups": list})
		return true
	}
	id, err := strconv.Atoi(strings.TrimPrefix(path, groups+"/"))
	group, ok := s.groups[id]
	if err != nil || !ok {
		w.WriteHeader(http.StatusNotFound)
		return true
	}
	switch req.Method {
	case http.MethodGet:
		answer := group
		if s.answerID != 0 {
			answer = newRunnerGroup(s.answerID, "other", "all")
		}
		write(answer)
	case http.MethodPatch:
		requests := f.recorded()
		for key, value := range requests[len(requests)-1].body {
			group[key] = value
		}
		write(group)
	default:
		return false
	}
	return true
}

func runnerGroupsConfig(base string) *config.Config {
	cfg := coreConfig(base)
	all := []config.Effect{config.EffectRead, config.EffectCreate, config.EffectUpdate, config.EffectDelete}
	tools := append(append([]string{}, runnerGroupReads...), runnerGroupWrites...)
	connection := func(c config.Connection, listed bool) config.Connection {
		c.Service, c.Credential, c.Effects = "gh", "gh-reader", all
		if listed {
			c.Tools = tools
		}
		return c
	}
	cfg.Connections["org"] = connection(config.Connection{Targets: []string{"orgs/octo-org"}}, false)
	cfg.Connections["org-listed"] = connection(config.Connection{Targets: []string{"orgs/octo-org"}}, true)
	cfg.Connections["org-repo"] = connection(config.Connection{Targets: []string{"orgs/octo-org", "repos/octo-org/example"}}, true)
	cfg.Connections["org-excluded"] = connection(config.Connection{Exclude: []string{"orgs/other-org"}}, true)
	cfg.Connections["open"] = connection(config.Connection{}, true)
	cfg.Connections["repo-listed"] = connection(config.Connection{Target: repoTarget}, true)
	cfg.Connections["org-repo-excluded"] = connection(config.Connection{Targets: []string{"orgs/octo-org"},
		Exclude: []string{"repos/octo-org/secret"}}, true)
	return cfg
}

func runnerGroupsCore(t *testing.T, base string, reads *int) *application.Core {
	t.Helper()
	red := &redact.Redactor{}
	return application.New(registry(t), runnerGroupsConfig(base), resolver(red, reads), red)
}

func countMethods(f *fakeGitHub, since int) map[string]int {
	methods := map[string]int{}
	for _, request := range f.recorded()[since:] {
		methods[request.method]++
	}
	return methods
}

// The reads give every documented member, mark addresses as data, and name the organization; the list
// forwards the repository filter by name and pages from the reported total.
func TestRunnerGroupReadsGiveEveryDocumentedMember(t *testing.T) {
	f, _, base := serveRunnerGroups(t)
	core := runnerGroupsCore(t, base, nil)
	got, err := invoke(t, core, runnerGroupsList.ID, "org-listed", `{"organization":"orgs/octo-org"}`, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"total_count":2`, `"organization":"orgs/octo-org"`, `"name":"builders"`, `"visibility":"selected"`,
		`"default":true`, `"selected_repositories_url":"https://`, `"runners_url":"https://`, `"hosted_runners_url":"https://`,
		`"network_configuration_id":"net-1"`, `"inherited":false`, `"inherited_allows_public_repositories":false`,
		`"allows_public_repositories":false`, `"workflow_restrictions_read_only":false`, `"restricted_to_workflows":true`,
		`"selected_workflows":["octo-org/example/.github/workflows/deploy.yml@main"]`, `"has_more":false`} {
		if !strings.Contains(string(got), want) {
			t.Errorf("list = %s, want %s", got, want)
		}
	}
	before := len(f.recorded())
	got, err = invoke(t, core, runnerGroupsList.ID, "org-repo",
		`{"organization":"orgs/octo-org","visible_to_repository":"octo-org/example","limit":1}`, false)
	if err != nil || !strings.Contains(string(got), `"total_count":1`) || strings.Contains(string(got), `"Default"`) {
		t.Errorf("filtered list = %s, %v", got, err)
	}
	if query := f.recorded()[before].query; !strings.Contains(query, "visible_to_repository=example") ||
		!strings.Contains(query, "per_page=1") {
		t.Errorf("filtered list query = %q, want the repository name and the batch size", query)
	}
	one, err := invoke(t, core, runnerGroupsGet.ID, "org-listed", `{"organization":"orgs/octo-org","id":2}`, false)
	if err != nil || !strings.Contains(string(one), `"id":2`) || !strings.Contains(string(one), `"organization":"orgs/octo-org"`) ||
		!strings.Contains(string(one), `"restricted_to_workflows":true`) {
		t.Errorf("get = %s, %v", one, err)
	}
	hosted, err := invoke(t, core, hostedRunnersList.ID, "org-listed", `{"organization":"orgs/octo-org"}`, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"total_count":1`, `"name":"large-linux"`, `"runner_group_id":2`, `"size_gb":86`, `"display_name":"Ubuntu"`,
		`"source":"github"`, `"version":"latest"`, `"cpu_cores":8`, `"memory_gb":32`, `"storage_gb":300`, `"status":"Ready"`,
		`"platform":"linux-x64"`, `"maximum_runners":10`, `"public_ip_enabled":true`, `"prefix":"203.0.113.0"`, `"length":28`,
		`"last_active_on":"2026-11-01T10:00:00Z"`, `"image_gen":false`} {
		if !strings.Contains(string(hosted), want) {
			t.Errorf("hosted list = %s, want %s", hosted, want)
		}
	}
}

// Pages continue from the reported total and the cursor binds to the organization and the filter.
func TestRunnerGroupListPages(t *testing.T) {
	f, s, base := serveRunnerGroups(t)
	for id := 3; id <= 5; id++ {
		s.groups[id] = newRunnerGroup(id, "g"+strconv.Itoa(id), "all")
	}
	core := runnerGroupsCore(t, base, nil)
	got, err := invoke(t, core, runnerGroupsList.ID, "org-listed", `{"organization":"orgs/octo-org","limit":2}`, false)
	var first struct {
		NextCursor string `json:"next_cursor"`
		HasMore    bool   `json:"has_more"`
	}
	if err != nil || json.Unmarshal(got, &first) != nil || !first.HasMore || first.NextCursor == "" {
		t.Fatalf("first page = %s, %v", got, err)
	}
	before := len(f.recorded())
	if _, err := invoke(t, core, runnerGroupsList.ID, "org-listed",
		`{"organization":"orgs/octo-org","limit":2,"cursor":"`+first.NextCursor+`"}`, false); err != nil {
		t.Fatal(err)
	}
	if query := f.recorded()[before].query; !strings.Contains(query, "page=2") {
		t.Errorf("second page query = %q, want page 2", query)
	}
	before = len(f.recorded())
	if _, err := invoke(t, core, hostedRunnersList.ID, "org-listed",
		`{"organization":"orgs/octo-org","cursor":"`+first.NextCursor+`"}`, false); !isInvalidRequest(err) || len(f.recorded()) != before {
		t.Errorf("a foreign cursor = %v, want an invalid request without a request", err)
	}
}

// Provider texts are cut, controls replaced, and the network details of a hosted runner are cut hard.
func TestRunnerViewsCutUntrustedText(t *testing.T) {
	_, s, base := serveRunnerGroups(t)
	long := strings.Repeat("x", 500)
	s.groups[2]["name"] = "bad\u0007" + long
	workflows := make([]any, 150)
	for i := range workflows {
		workflows[i] = "a/b/.github/workflows/w.yml@main"
	}
	s.groups[2]["selected_workflows"] = workflows
	ips := make([]any, 25)
	for i := range ips {
		ips[i] = map[string]any{"enabled": true, "prefix": strings.Repeat("9", 300), "length": 28.0}
	}
	s.hosted[0].(map[string]any)["public_ips"] = ips
	core := runnerGroupsCore(t, base, nil)
	got, err := invoke(t, core, runnerGroupsGet.ID, "org-listed", `{"organization":"orgs/octo-org","id":2}`, false)
	var group RunnerGroup
	if err != nil || json.Unmarshal(got, &group) != nil || len([]rune(group.Name)) != projectionNameLimit || !group.NameTruncated ||
		strings.Contains(group.Name, "\a") || len(group.SelectedWorkflows) != 100 || !group.SelectedWorkflowsTruncated {
		t.Errorf("group = %s, %v", got, err)
	}
	got, err = invoke(t, core, hostedRunnersList.ID, "org-listed", `{"organization":"orgs/octo-org"}`, false)
	var list HostedRunnerList
	if err != nil || json.Unmarshal(got, &list) != nil || len(list.Runners) != 1 ||
		len(list.Runners[0].PublicIPs) != 10 || !list.Runners[0].PublicIPsTruncated ||
		len([]rune(*list.Runners[0].PublicIPs[0].Prefix)) != 64 {
		t.Errorf("hosted list = %s, %v", got, err)
	}
}

// withKept adds the settings of group 2 that an update sends back as read.
func withKept(body map[string]any) map[string]any {
	body["allows_public_repositories"] = false
	body["restricted_to_workflows"] = true
	body["selected_workflows"] = []any{"octo-org/example/.github/workflows/deploy.yml@main"}
	return body
}

// An update reads the group below the organization, sends exactly one PATCH with the name and the visibility
// and nothing else, and answers with the group as GitHub answered the change.
func TestRunnerGroupUpdateSendsOnePatch(t *testing.T) {
	f, s, base := serveRunnerGroups(t)
	core := runnerGroupsCore(t, base, nil)
	for _, tt := range []struct {
		name, args string
		want       map[string]any
		wantAnswer []string
	}{
		{"both", `"name":"renamed","visibility":"private"`, withKept(map[string]any{"name": "renamed", "visibility": "private"}),
			[]string{`"before":{"name":"builders","visibility":"selected"}`, `"name":"renamed"`, `"visibility":"private"`}},
		{"visibility only", `"visibility":"all"`, withKept(map[string]any{"name": "renamed", "visibility": "all"}),
			[]string{`"before":{"name":"renamed","visibility":"private"}`, `"visibility":"all"`}},
		{"name only", `"name":"again"`, withKept(map[string]any{"name": "again"}),
			[]string{`"before":{"name":"renamed","visibility":"all"}`, `"name":"again"`}},
	} {
		before := len(f.recorded())
		got, err := invoke(t, core, runnerGroupsUpdate.ID, "org-listed", `{"organization":"orgs/octo-org","id":2,`+tt.args+`}`, true)
		if err != nil {
			t.Fatalf("%s = %v", tt.name, err)
		}
		for _, want := range append(tt.wantAnswer, `"id":2`, `"organization":"orgs/octo-org"`) {
			if !strings.Contains(string(got), want) {
				t.Errorf("%s = %s, want %s", tt.name, got, want)
			}
		}
		if methods := countMethods(f, before); !reflect.DeepEqual(methods, map[string]int{http.MethodGet: 1, http.MethodPatch: 1}) {
			t.Errorf("%s sent %v, want a read and one change", tt.name, methods)
		}
		var patch recorded
		for _, request := range f.recorded()[before:] {
			if request.method == http.MethodPatch {
				patch = request
			}
		}
		if patch.path != "/api/v3/orgs/octo-org/actions/runner-groups/2" || !reflect.DeepEqual(patch.body, tt.want) {
			t.Errorf("%s sent %s %v, want %v", tt.name, patch.path, patch.body, tt.want)
		}
	}
	if s.groups[2]["name"] != "again" {
		t.Errorf("group name = %v", s.groups[2]["name"])
	}
}

// The name of the current group goes back to GitHub as GitHub reported it, not as the cut view of it.
func TestRunnerGroupUpdateKeepsTheCurrentNameUncut(t *testing.T) {
	f, s, base := serveRunnerGroups(t)
	long := strings.Repeat("n", 300)
	s.groups[2]["name"] = long
	core := runnerGroupsCore(t, base, nil)
	before := len(f.recorded())
	if _, err := invoke(t, core, runnerGroupsUpdate.ID, "org-listed", `{"organization":"orgs/octo-org","id":2,"visibility":"all"}`, true); err != nil {
		t.Fatal(err)
	}
	for _, request := range f.recorded()[before:] {
		if request.method == http.MethodPatch && request.body["name"] != long {
			t.Errorf("sent name has %d characters, want the 300 GitHub reported", len(fmt.Sprint(request.body["name"])))
		}
	}
}

// The default and an inherited group are handled by what the API allows: nothing is refused beforehand.
func TestRunnerGroupUpdateLeavesDefaultAndInheritedToTheAPI(t *testing.T) {
	f, s, base := serveRunnerGroups(t)
	s.groups[3] = newRunnerGroup(3, "from-enterprise", "all")
	s.groups[3]["inherited"] = true
	core := runnerGroupsCore(t, base, nil)
	for _, id := range []int{1, 3} {
		before := len(f.recorded())
		if _, err := invoke(t, core, runnerGroupsUpdate.ID, "org-listed",
			fmt.Sprintf(`{"organization":"orgs/octo-org","id":%d,"visibility":"private"}`, id), true); err != nil {
			t.Errorf("group %d = %v", id, err)
		}
		if methods := countMethods(f, before); methods[http.MethodPatch] != 1 {
			t.Errorf("group %d sent %v, want one change", id, methods)
		}
	}
}

// A group that is not below the organization, or one GitHub answers with another identifier, changes nothing.
func TestRunnerGroupIdIsBoundByTheReadBack(t *testing.T) {
	f, s, base := serveRunnerGroups(t)
	core := runnerGroupsCore(t, base, nil)
	before := len(f.recorded())
	if _, err := invoke(t, core, runnerGroupsUpdate.ID, "org-listed", `{"organization":"orgs/octo-org","id":99,"visibility":"all"}`, true); classOf(err) != provider.ClassNotFound {
		t.Errorf("a foreign group = %v, want not found", err)
	}
	s.answerID = 7
	if _, err := invoke(t, core, runnerGroupsUpdate.ID, "org-listed", `{"organization":"orgs/octo-org","id":2,"visibility":"all"}`, true); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("a mismatching group = %v, want an invalid response", err)
	}
	if _, err := invoke(t, core, runnerGroupsGet.ID, "org-listed", `{"organization":"orgs/octo-org","id":2}`, false); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("a mismatching read = %v, want an invalid response", err)
	}
	if methods := countMethods(f, before); methods[http.MethodPatch] != 0 {
		t.Errorf("a refused binding sent %v", methods)
	}
}

// Organization and repository are bound before a credential is resolved or GitHub is asked.
func TestRunnerGroupTargetsAreBoundBeforeIO(t *testing.T) {
	f, _, base := serveRunnerGroups(t)
	reads := 0
	core := runnerGroupsCore(t, base, &reads)
	for _, id := range append(append([]string{}, runnerGroupReads...), runnerGroupWrites...) {
		for _, tt := range []struct{ name, connection, arguments string }{
			{"no organization", "open", `{"id":2,"visibility":"all"}`},
			{"a repository instead", "open", `{"repository":"octo-org/example","id":2,"visibility":"all"}`},
			{"an organization outside the targets", "org-listed", `{"organization":"orgs/other-org","id":2,"visibility":"all"}`},
			{"an excluded organization", "org-excluded", `{"organization":"orgs/other-org","id":2,"visibility":"all"}`},
			{"a user", "open", `{"organization":"users/octocat","id":2,"visibility":"all"}`},
			{"a repository target only", "repo-listed", `{"organization":"orgs/octo-org","id":2,"visibility":"all"}`},
		} {
			reads = 0
			before := len(f.recorded())
			if _, err := invoke(t, core, id, tt.connection, tt.arguments, true); !isInvalidRequest(err) {
				t.Errorf("%s %s = %v, want an invalid request", id, tt.name, err)
			}
			if reads != 0 || len(f.recorded()) != before {
				t.Errorf("%s %s reached the credential or GitHub", id, tt.name)
			}
		}
	}
	for _, tt := range []struct{ name, connection, repository string }{
		{"another organization's repository", "org-repo", "other-org/example"},
		{"an excluded repository", "org-repo-excluded", "octo-org/secret"},
		{"a repository outside the targets", "org-repo", "octo-org/other"},
		{"a malformed repository", "org-repo", "example"},
	} {
		reads = 0
		before := len(f.recorded())
		arguments := `{"organization":"orgs/octo-org","visible_to_repository":"` + tt.repository + `"}`
		if _, err := invoke(t, core, runnerGroupsList.ID, tt.connection, arguments, false); !isInvalidRequest(err) {
			t.Errorf("%s = %v, want an invalid request", tt.name, err)
		}
		if reads != 0 || len(f.recorded()) != before {
			t.Errorf("%s reached the credential or GitHub", tt.name)
		}
	}
}

// Every tool is offered only where a connection's tools list names it; the change needs confirmation and none
// of the four sits in a standard profile, and only the reads in the administrator profile.
func TestRunnerGroupToolsNeedTheirNameInTheToolsList(t *testing.T) {
	f, _, base := serveRunnerGroups(t)
	core := runnerGroupsCore(t, base, nil)
	arguments := map[string]string{
		runnerGroupsList.ID: `{"organization":"orgs/octo-org"}`, runnerGroupsGet.ID: `{"organization":"orgs/octo-org","id":2}`,
		runnerGroupsUpdate.ID: `{"organization":"orgs/octo-org","id":2,"visibility":"all"}`,
		hostedRunnersList.ID:  `{"organization":"orgs/octo-org"}`,
	}
	for id, args := range arguments {
		before := len(f.recorded())
		if _, err := core.Describe(application.DescribeRequest{Operation: id, Connection: "org"}); err == nil {
			t.Errorf("describe %s on a connection without the tool succeeded", id)
		}
		if _, err := invoke(t, core, id, "org", args, true); err == nil {
			t.Errorf("invoke %s on a connection without the tool succeeded", id)
		}
		if id == runnerGroupsUpdate.ID {
			if _, err := invoke(t, core, id, "org-listed", args, false); err == nil {
				t.Errorf("%s without confirmation succeeded", id)
			}
		}
		if len(f.recorded()) != before {
			t.Errorf("%s reached GitHub without its name or its confirmation", id)
		}
	}
	metadata, _ := registry(t).ProviderMetadata(Provider)
	for _, profile := range metadata.Profiles {
		for _, id := range profile.Tools {
			if contains(runnerGroupWrites, id) {
				t.Errorf("profile %s selects %s", profile.ID, id)
			}
			if contains(runnerGroupReads, id) && (profile.Recommended || profile.ID != "actions-admin") {
				t.Errorf("profile %s selects %s", profile.ID, id)
			}
		}
	}
}

// Every check runs before a credential is resolved.
func TestRunnerGroupArgumentsAreValidatedBeforeIO(t *testing.T) {
	f, _, base := serveRunnerGroups(t)
	reads := 0
	core := runnerGroupsCore(t, base, &reads)
	org := `"organization":"orgs/octo-org"`
	for _, tt := range []struct{ name, tool, arguments string }{
		{"no id", runnerGroupsGet.ID, `{` + org + `}`},
		{"id zero", runnerGroupsGet.ID, `{` + org + `,"id":0}`},
		{"update without a value", runnerGroupsUpdate.ID, `{` + org + `,"id":2}`},
		{"an empty name", runnerGroupsUpdate.ID, `{` + org + `,"id":2,"name":""}`},
		{"a blank name", runnerGroupsUpdate.ID, `{` + org + `,"id":2,"name":"   "}`},
		{"a long name", runnerGroupsUpdate.ID, `{` + org + `,"id":2,"name":"` + strings.Repeat("a", 256) + `"}`},
		{"a name with a line break", runnerGroupsUpdate.ID, `{` + org + `,"id":2,"name":"a\nb"}`},
		{"a name with a control character", runnerGroupsUpdate.ID, `{` + org + `,"id":2,"name":"a\u0001b"}`},
		{"an unknown visibility", runnerGroupsUpdate.ID, `{` + org + `,"id":2,"visibility":"internal"}`},
		{"selected repositories", runnerGroupsUpdate.ID, `{` + org + `,"id":2,"visibility":"selected","selected_repository_ids":[1]}`},
		{"a limit too high", runnerGroupsList.ID, `{` + org + `,"limit":101}`},
		{"a malformed cursor", hostedRunnersList.ID, `{` + org + `,"cursor":"abc"}`},
	} {
		reads = 0
		before := len(f.recorded())
		if _, err := invoke(t, core, tt.tool, "org-listed", tt.arguments, true); err == nil {
			t.Errorf("%s was accepted", tt.name)
		}
		if reads != 0 || len(f.recorded()) != before {
			t.Errorf("%s reached the credential or GitHub", tt.name)
		}
	}
	// The update offers neither selected repositories nor any other member of GitHub's body.
	if strings.Contains(string(runnerGroupsUpdate.InputSchema), "selected_repository_ids") ||
		strings.Contains(string(runnerGroupsUpdate.InputSchema), "allows_public_repositories") {
		t.Error("the update offers a member beyond name and visibility")
	}
}

// GitHub's refusals become Qatlas' own messages without GitHub's text; a refusal or an unclear outcome of the
// change is sent once and never repeated, and an unclear outcome says so.
func TestRunnerGroupFailuresNameTheirMeaningWithoutProviderText(t *testing.T) {
	f, s, base := serveRunnerGroups(t)
	core := runnerGroupsCore(t, base, nil)
	for _, tt := range []struct {
		method    string
		status    int
		class     provider.Class
		fragment  string
		uncertain bool
		patches   int
	}{
		{http.MethodGet, http.StatusForbidden, provider.ClassPermission, "admin:org", false, 0},
		{http.MethodGet, http.StatusNotFound, provider.ClassNotFound, "does not hold", false, 0},
		{http.MethodPatch, http.StatusForbidden, provider.ClassPermission, "changing it needs admin:org", false, 1},
		{http.MethodPatch, http.StatusNotFound, provider.ClassNotFound, "does not hold", false, 1},
		{http.MethodPatch, http.StatusConflict, provider.ClassProviderError, "nothing was changed", false, 1},
		{http.MethodPatch, http.StatusUnprocessableEntity, provider.ClassProviderError, "name or the visibility", false, 1},
		{http.MethodPatch, http.StatusGatewayTimeout, provider.ClassTimeout, "did not answer in time", true, 1},
		{http.MethodPatch, http.StatusBadGateway, provider.ClassProviderError, "HTTP 502", true, 1},
	} {
		s.status = map[string]int{tt.method: tt.status}
		before := len(f.recorded())
		_, err := invoke(t, core, runnerGroupsUpdate.ID, "org-listed", `{"organization":"orgs/octo-org","id":2,"name":"x"}`, true)
		if classOf(err) != tt.class || err == nil || !strings.Contains(err.Error(), tt.fragment) ||
			strings.Contains(err.Error(), uncertain) != tt.uncertain {
			t.Errorf("%s answered %d = %v, want %s naming %q", tt.method, tt.status, err, tt.class, tt.fragment)
		}
		if err != nil && strings.Contains(err.Error(), runnerGroupProviderCanary) {
			t.Errorf("%s answered %d quotes GitHub's text: %v", tt.method, tt.status, err)
		}
		if got := countMethods(f, before)[http.MethodPatch]; got != tt.patches {
			t.Errorf("%s answered %d sent %d changes, want %d", tt.method, tt.status, got, tt.patches)
		}
	}
	// The reads name their own rights and never quote GitHub.
	s.status = map[string]int{http.MethodGet: http.StatusForbidden}
	for _, tt := range []struct{ tool, arguments, fragment string }{
		{runnerGroupsList.ID, `{"organization":"orgs/octo-org"}`, "admin:org"},
		{runnerGroupsGet.ID, `{"organization":"orgs/octo-org","id":2}`, "admin:org"},
		{hostedRunnersList.ID, `{"organization":"orgs/octo-org"}`, "manage_runner:org"},
	} {
		_, err := invoke(t, core, tt.tool, "org-listed", tt.arguments, false)
		if classOf(err) != provider.ClassPermission || err == nil || !strings.Contains(err.Error(), tt.fragment) ||
			strings.Contains(err.Error(), runnerGroupProviderCanary) {
			t.Errorf("%s refused = %v, want the right it needs", tt.tool, err)
		}
	}
}

// A change whose answer GitHub sends unreadable is an open outcome, not a success and not a clean refusal.
func TestRunnerGroupUpdateWithAnUnreadableAnswerIsUncertain(t *testing.T) {
	f, s, base := serveRunnerGroups(t)
	core := runnerGroupsCore(t, base, nil)
	s.status = map[string]int{}
	patched := false
	previous := f.failure
	f.failure = func(w http.ResponseWriter, req *http.Request) bool {
		if req.Method == http.MethodPatch {
			patched = true
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"id":"two"}`))
			return true
		}
		return previous(w, req)
	}
	before := len(f.recorded())
	_, err := invoke(t, core, runnerGroupsUpdate.ID, "org-listed", `{"organization":"orgs/octo-org","id":2,"visibility":"all"}`, true)
	if !patched || err == nil || !strings.Contains(err.Error(), uncertain) {
		t.Errorf("an unreadable answer = %v, want the open outcome", err)
	}
	if got := countMethods(f, before)[http.MethodPatch]; got != 1 {
		t.Errorf("sent %d changes, want one", got)
	}
}

// An entry GitHub sends without an identifier or a name is an invalid response, never passed on.
func TestRunnerReadsRefuseUnusableEntries(t *testing.T) {
	_, s, base := serveRunnerGroups(t)
	core := runnerGroupsCore(t, base, nil)
	s.groups[2]["name"] = ""
	if _, err := invoke(t, core, runnerGroupsList.ID, "org-listed", `{"organization":"orgs/octo-org"}`, false); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("a group without a name = %v, want an invalid response", err)
	}
	s.hosted = []any{map[string]any{"name": "no-id"}}
	if _, err := invoke(t, core, hostedRunnersList.ID, "org-listed", `{"organization":"orgs/octo-org"}`, false); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("a hosted runner without an identifier = %v, want an invalid response", err)
	}
}

func lastPatch(f *fakeGitHub, since int) (recorded, bool) {
	for _, request := range f.recorded()[since:] {
		if request.method == http.MethodPatch {
			return request, true
		}
	}
	return recorded{}, false
}

// The settings an update does not change go back as read, so nothing is loosened: the public repository
// setting in both states, and the restriction with its whole list only while the group is restricted.
func TestRunnerGroupUpdateSendsBackWhatItRead(t *testing.T) {
	f, s, base := serveRunnerGroups(t)
	core := runnerGroupsCore(t, base, nil)
	for _, allows := range []bool{true, false} {
		s.groups[2]["allows_public_repositories"] = allows
		before := len(f.recorded())
		if _, err := invoke(t, core, runnerGroupsUpdate.ID, "org-listed", `{"organization":"orgs/octo-org","id":2,"visibility":"all"}`, true); err != nil {
			t.Fatal(err)
		}
		patch, _ := lastPatch(f, before)
		if patch.body["allows_public_repositories"] != allows || patch.body["restricted_to_workflows"] != true ||
			!reflect.DeepEqual(patch.body["selected_workflows"], []any{"octo-org/example/.github/workflows/deploy.yml@main"}) {
			t.Errorf("allows %t: sent %v", allows, patch.body)
		}
	}
	s.groups[2]["restricted_to_workflows"] = false
	before := len(f.recorded())
	if _, err := invoke(t, core, runnerGroupsUpdate.ID, "org-listed", `{"organization":"orgs/octo-org","id":2,"visibility":"all"}`, true); err != nil {
		t.Fatal(err)
	}
	patch, _ := lastPatch(f, before)
	if _, ok := patch.body["restricted_to_workflows"]; ok || patch.body["selected_workflows"] != nil {
		t.Errorf("an unrestricted group sent %v, want no restriction and no list", patch.body)
	}
}

// A list this tool cannot read whole ends the call before the request, without provider text.
func TestRunnerGroupUpdateRefusesAShortenedWorkflowList(t *testing.T) {
	f, s, base := serveRunnerGroups(t)
	core := runnerGroupsCore(t, base, nil)
	many := make([]any, 101)
	for i := range many {
		many[i] = "a/b/.github/workflows/w.yml@main"
	}
	for name, list := range map[string][]any{"too many entries": many,
		"a long entry": {strings.Repeat("w", 1001)}} {
		s.groups[2]["selected_workflows"] = list
		before := len(f.recorded())
		_, err := invoke(t, core, runnerGroupsUpdate.ID, "org-listed", `{"organization":"orgs/octo-org","id":2,"visibility":"all"}`, true)
		if !isInvalidRequest(err) || !strings.Contains(err.Error(), "nothing was changed") || strings.Contains(err.Error(), runnerGroupProviderCanary) {
			t.Errorf("%s = %v, want a refusal that changed nothing", name, err)
		}
		if _, patched := lastPatch(f, before); patched {
			t.Errorf("%s sent a change", name)
		}
	}
}
