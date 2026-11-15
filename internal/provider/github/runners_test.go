package github

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const runnerProviderCanary = "runner-provider-text-canary-3e8a"

const (
	runnerRepoRoute = "/repos/octo-org/example/actions/runners"
	runnerOrgRoute  = "/orgs/octo-org/actions/runners"
)

// fakeRunners answers the runner routes of the repository octo-org/example and the organization octo-org. A
// runner exists at one level only, so a runner of the other level is a 404 there. It keeps the labels it was
// given and can refuse every request of one method with a status.
type fakeRunners struct {
	mu      sync.Mutex
	f       *fakeGitHub
	runners map[string][]map[string]any // route -> runners in order
	status  map[string]int              // method -> status every request of it answers with
	// ignoreName makes the list ignore the name filter, like a server that does not know it.
	ignoreName bool
	// wrongID makes the runner read answer another runner than the one asked for.
	wrongID bool
	// brokenLabels makes a label change answer without a readable label list.
	brokenLabels bool
	paths        []string
}

func runnerLabel(id int, name, kind string) map[string]any {
	return map[string]any{"id": float64(id), "name": name, "type": kind}
}

func newRunner(id int, name string, custom ...string) map[string]any {
	labels := []any{runnerLabel(1, "self-hosted", "read-only"), runnerLabel(2, "Linux", "read-only"),
		runnerLabel(3, "X64", "read-only")}
	for i, label := range custom {
		labels = append(labels, runnerLabel(100+i, label, "custom"))
	}
	return map[string]any{"id": float64(id), "runner_group_id": 1.0, "name": name, "os": "linux", "status": "online",
		"busy": false, "labels": labels, "ephemeral": false, "version": "2.323.0"}
}

func serveRunners(t *testing.T) (*fakeGitHub, *fakeRunners, string) {
	t.Helper()
	r := &fakeRunners{status: map[string]int{}, runners: map[string][]map[string]any{
		runnerRepoRoute: {newRunner(5, "Build-1", "gpu"), newRunner(6, "build-2"), newRunner(9, "other")},
		runnerOrgRoute:  {newRunner(7, "org-1", "large"), newRunner(8, "org-2")},
	}}
	f := &fakeGitHub{}
	r.f = f
	f.failure = r.route
	return f, r, serve(t, f)
}

func (r *fakeRunners) find(route string, id int) (int, map[string]any) {
	for i, runner := range r.runners[route] {
		if int(runner["id"].(float64)) == id {
			return i, runner
		}
	}
	return -1, nil
}

func (r *fakeRunners) route(w http.ResponseWriter, req *http.Request) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	escaped := strings.TrimPrefix(req.URL.EscapedPath(), "/api/v3")
	var route string
	switch {
	case strings.HasPrefix(escaped, runnerRepoRoute):
		route = runnerRepoRoute
	case strings.HasPrefix(escaped, runnerOrgRoute):
		route = runnerOrgRoute
	default:
		return false
	}
	r.paths = append(r.paths, req.Method+" "+escaped)
	w.Header().Set("Content-Type", "application/json")
	if status := r.status[req.Method]; status != 0 {
		w.WriteHeader(status)
		fmt.Fprintf(w, `{"message":%q}`, runnerProviderCanary)
		return true
	}
	rest := strings.TrimPrefix(escaped, route)
	notFound := func() bool {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"message":"Not Found"}`)
		return true
	}
	write := func(value any) bool {
		data, _ := json.Marshal(value)
		w.Write(data)
		return true
	}
	if rest == "" {
		if req.Method != http.MethodGet {
			return false
		}
		perPage, _ := strconv.Atoi(req.URL.Query().Get("per_page"))
		page, _ := strconv.Atoi(req.URL.Query().Get("page"))
		all := r.runners[route]
		if name := req.URL.Query().Get("name"); name != "" && !r.ignoreName {
			var kept []map[string]any
			for _, runner := range all {
				if runner["name"] == name {
					kept = append(kept, runner)
				}
			}
			all = kept
		}
		start := min((page-1)*perPage, len(all))
		end := min(start+perPage, len(all))
		return write(map[string]any{"total_count": len(all), "runners": all[start:end]})
	}
	parts := strings.SplitN(strings.TrimPrefix(rest, "/"), "/", 3)
	id, _ := strconv.Atoi(parts[0])
	index, runner := r.find(route, id)
	if runner == nil {
		return notFound()
	}
	var body map[string]any
	if requests := r.f.recorded(); req.Method != http.MethodGet && len(requests) > 0 {
		body = requests[len(requests)-1].body
	}
	labels, _ := runner["labels"].([]any)
	answer := func() bool {
		if r.brokenLabels {
			return write(map[string]any{"labels": []any{map[string]any{"id": 1.0}}})
		}
		return write(map[string]any{"total_count": len(labels), "labels": labels})
	}
	custom := func(names []any) []any {
		out := []any{}
		for _, label := range labels {
			if label.(map[string]any)["type"] == "read-only" {
				out = append(out, label)
			}
		}
		for i, name := range names {
			out = append(out, runnerLabel(200+i, name.(string), "custom"))
		}
		return out
	}
	switch {
	case len(parts) == 1 && req.Method == http.MethodGet:
		if r.wrongID {
			return write(newRunner(id+1000, "other"))
		}
		return write(runner)
	case len(parts) == 1 && req.Method == http.MethodDelete:
		r.runners[route] = slices.Delete(r.runners[route], index, index+1)
		w.WriteHeader(http.StatusNoContent)
		return true
	case len(parts) == 2 && parts[1] == "labels" && req.Method == http.MethodGet:
		return write(map[string]any{"total_count": len(labels), "labels": labels})
	case len(parts) == 2 && parts[1] == "labels" && req.Method == http.MethodPost:
		names, _ := body["labels"].([]any)
		have := map[string]bool{}
		for _, label := range labels {
			have[label.(map[string]any)["name"].(string)] = true
		}
		for _, name := range names {
			if !have[name.(string)] {
				labels = append(labels, runnerLabel(300+len(labels), name.(string), "custom"))
			}
		}
		runner["labels"] = labels
		return answer()
	case len(parts) == 2 && parts[1] == "labels" && req.Method == http.MethodPut:
		names, _ := body["labels"].([]any)
		labels = custom(names)
		runner["labels"] = labels
		return answer()
	case len(parts) == 3 && parts[1] == "labels" && req.Method == http.MethodDelete:
		name, _ := url.PathUnescape(parts[2])
		kept := []any{}
		for _, label := range labels {
			if label.(map[string]any)["name"] != name {
				kept = append(kept, label)
			}
		}
		labels = kept
		runner["labels"] = labels
		return answer()
	}
	return false
}

// runnersConfig binds every shape of connection the runner tools distinguish.
func runnersConfig(base string) *config.Config {
	cfg := coreConfig(base)
	all := []config.Effect{config.EffectRead, config.EffectCreate, config.EffectUpdate, config.EffectDelete}
	tools := slices.Concat(runnersReads, runnersChanges)
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

func runnersCore(t *testing.T, base string, reads *int) *application.Core {
	t.Helper()
	red := &redact.Redactor{}
	return application.New(registry(t), runnersConfig(base), resolver(red, reads), red)
}

func methodCounts(requests []recorded) map[string]int {
	methods := map[string]int{}
	for _, request := range requests {
		methods[request.method]++
	}
	return methods
}

// A repository and an organization list, read, and label-list their runners with every documented member, and
// an answer names the level it addressed.
func TestRunnersReadAtBothLevels(t *testing.T) {
	_, _, base := serveRunners(t)
	core := runnersCore(t, base, nil)
	members := []string{`"id":5`, `"runner_group_id":1`, `"name":"Build-1"`, `"os":"linux"`, `"status":"online"`,
		`"busy":false`, `"ephemeral":false`, `"version":"2.323.0"`, `"labels":[{"id":1,"name":"self-hosted","type":"read-only"}`,
		`{"id":100,"name":"gpu","type":"custom"}`}
	for _, tt := range []struct {
		tool, connection, arguments string
		want                        []string
	}{
		{runnersList.ID, "repo-listed", `{"repository":"octo-org/example"}`,
			append([]string{`"total_count":3`, `"has_more":false`, `"repository":"octo-org/example"`}, members...)},
		{runnersList.ID, "org-listed", `{"organization":"orgs/octo-org"}`,
			[]string{`"total_count":2`, `"name":"org-1"`, `"organization":"orgs/octo-org"`}},
		{runnersGet.ID, "repo-listed", `{"repository":"octo-org/example","runner_id":5}`,
			append([]string{`"repository":"octo-org/example"`}, members...)},
		{runnersGet.ID, "org-listed", `{"organization":"orgs/octo-org","runner_id":7}`,
			[]string{`"name":"org-1"`, `"organization":"orgs/octo-org"`}},
		{runnerLabelsList.ID, "repo-listed", `{"repository":"octo-org/example","runner_id":5}`,
			[]string{`"runner_id":5`, `"total_count":4`, `{"id":100,"name":"gpu","type":"custom"}`, `"repository":"octo-org/example"`}},
		{runnerLabelsList.ID, "org-listed", `{"organization":"orgs/octo-org","runner_id":8}`,
			[]string{`"runner_id":8`, `"total_count":3`, `"organization":"orgs/octo-org"`}},
	} {
		got, err := invoke(t, core, tt.tool, tt.connection, tt.arguments, false)
		if err != nil {
			t.Fatalf("%s %s = %v", tt.tool, tt.arguments, err)
		}
		for _, want := range tt.want {
			if !strings.Contains(string(got), want) {
				t.Errorf("%s %s = %s, want %s", tt.tool, tt.arguments, got, want)
			}
		}
		other := `"organization"`
		if strings.Contains(tt.arguments, `"organization"`) {
			other = `"repository"`
		}
		if strings.Contains(string(got), other) {
			t.Errorf("%s answered %s naming the other level: %s", tt.tool, tt.arguments, got)
		}
	}
}

// The list pages by cursor and a name filter that GitHub ignored never lets another runner through, while a
// name that differs only in case still matches.
func TestRunnersListPagesAndFiltersByName(t *testing.T) {
	_, r, base := serveRunners(t)
	core := runnersCore(t, base, nil)
	first, err := invoke(t, core, runnersList.ID, "repo-listed", `{"repository":"octo-org/example","limit":2}`, false)
	if err != nil || !strings.Contains(string(first), `"has_more":true`) || strings.Contains(string(first), `"id":9`) {
		t.Fatalf("first batch = %s, %v", first, err)
	}
	var page struct {
		NextCursor string `json:"next_cursor"`
	}
	if err := json.Unmarshal(first, &page); err != nil || page.NextCursor == "" {
		t.Fatalf("first batch names no cursor: %s", first)
	}
	second, err := invoke(t, core, runnersList.ID, "repo-listed",
		`{"repository":"octo-org/example","limit":2,"cursor":"`+page.NextCursor+`"}`, false)
	if err != nil || !strings.Contains(string(second), `"id":9`) || !strings.Contains(string(second), `"has_more":false`) {
		t.Errorf("second batch = %s, %v", second, err)
	}
	if _, err := invoke(t, core, runnersList.ID, "repo-listed",
		`{"organization":"orgs/octo-org","limit":2,"cursor":"`+page.NextCursor+`"}`, false); err == nil {
		t.Error("a cursor of another list was accepted")
	}
	r.ignoreName = true
	got, err := invoke(t, core, runnersList.ID, "repo-listed", `{"repository":"octo-org/example","name":"build-1"}`, false)
	if err != nil || !strings.Contains(string(got), `"name":"Build-1"`) || strings.Contains(string(got), `"id":6`) ||
		strings.Contains(string(got), `"id":9`) {
		t.Errorf("filtered list = %s, %v, want only Build-1", got, err)
	}
}

// A removal reads the runner below the chosen target first and sends one request; a runner of the other
// level, or an answer that names another runner, ends the call before anything is removed.
func TestRunnerRemovalIsBoundByAnEvidenceRead(t *testing.T) {
	f, r, base := serveRunners(t)
	core := runnersCore(t, base, nil)
	before := len(f.recorded())
	got, err := invoke(t, core, runnersDelete.ID, "repo-listed", `{"repository":"octo-org/example","runner_id":5}`, true)
	if err != nil || !strings.Contains(string(got), `"deleted":true`) || !strings.Contains(string(got), `"runner_id":5`) ||
		!strings.Contains(string(got), `"repository":"octo-org/example"`) {
		t.Fatalf("removal = %s, %v", got, err)
	}
	if got := methodCounts(f.recorded()[before:]); !reflect.DeepEqual(got, map[string]int{http.MethodGet: 1, http.MethodDelete: 1}) {
		t.Errorf("removal sent %v, want one read and one removal", got)
	}
	if _, runner := r.find(runnerRepoRoute, 5); runner != nil {
		t.Error("the runner is still there")
	}
	got, err = invoke(t, core, runnersDelete.ID, "org-listed", `{"organization":"orgs/octo-org","runner_id":7}`, true)
	if err != nil || !strings.Contains(string(got), `"organization":"orgs/octo-org"`) {
		t.Errorf("organization removal = %s, %v", got, err)
	}
	before = len(f.recorded())
	// A runner of the organization is not a runner of the repository, and the other way round.
	for _, tt := range []struct{ connection, arguments string }{
		{"repo-listed", `{"repository":"octo-org/example","runner_id":8}`},
		{"org-listed", `{"organization":"orgs/octo-org","runner_id":6}`},
	} {
		_, err := invoke(t, core, runnersDelete.ID, tt.connection, tt.arguments, true)
		if classOf(err) != provider.ClassNotFound || err == nil || strings.Contains(err.Error(), runnerProviderCanary) {
			t.Errorf("%s = %v, want not-found", tt.arguments, err)
		}
	}
	r.wrongID = true
	_, err = invoke(t, core, runnersDelete.ID, "repo-listed", `{"repository":"octo-org/example","runner_id":6}`, true)
	if classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("an evidence read naming another runner = %v, want invalid-provider-response", err)
	}
	if got := methodCounts(f.recorded()[before:]); got[http.MethodDelete] != 0 {
		t.Errorf("a refused removal sent %v", got)
	}
}

// Every label change reads the runner first and sends one request; read-only labels are refused before anything
// is sent, and the answer holds the labels GitHub reports.
func TestRunnerLabelsChangeWithOneRequest(t *testing.T) {
	f, r, base := serveRunners(t)
	core := runnersCore(t, base, nil)
	for _, tt := range []struct {
		name, tool, connection, arguments, method string
		want                                      []string
		absent                                    string
	}{
		{"add", runnerLabelsAdd.ID, "repo-listed", `{"repository":"octo-org/example","runner_id":6,"labels":["gpu","fast"]}`,
			http.MethodPost, []string{`"total_count":5`, `"name":"fast"`, `"runner_id":6`, `"repository":"octo-org/example"`}, ""},
		{"set", runnerLabelsSet.ID, "org-listed", `{"organization":"orgs/octo-org","runner_id":7,"labels":["medium"]}`,
			http.MethodPut, []string{`"total_count":4`, `"name":"medium"`, `"organization":"orgs/octo-org"`}, `"name":"large"`},
		{"set none", runnerLabelsSet.ID, "repo-listed", `{"repository":"octo-org/example","runner_id":5,"labels":[]}`,
			http.MethodPut, []string{`"total_count":3`, `"name":"self-hosted"`}, `"name":"gpu"`},
		{"remove", runnerLabelsRemove.ID, "repo-listed", `{"repository":"octo-org/example","runner_id":6,"label":"fast"}`,
			http.MethodDelete, []string{`"total_count":4`}, `"name":"fast"`},
	} {
		before := len(f.recorded())
		got, err := invoke(t, core, tt.tool, tt.connection, tt.arguments, true)
		if err != nil {
			t.Fatalf("%s = %v", tt.name, err)
		}
		for _, want := range tt.want {
			if !strings.Contains(string(got), want) {
				t.Errorf("%s = %s, want %s", tt.name, got, want)
			}
		}
		if tt.absent != "" && strings.Contains(string(got), tt.absent) {
			t.Errorf("%s = %s, want %s gone", tt.name, got, tt.absent)
		}
		if got := methodCounts(f.recorded()[before:]); !reflect.DeepEqual(got, map[string]int{http.MethodGet: 1, tt.method: 1}) {
			t.Errorf("%s sent %v, want one read and one %s", tt.name, got, tt.method)
		}
	}
	// A label with a slash travels as one escaped path segment.
	if _, err := invoke(t, core, runnerLabelsAdd.ID, "repo-listed", `{"repository":"octo-org/example","runner_id":6,"labels":["a/b"]}`, true); err != nil {
		t.Fatal(err)
	}
	if _, err := invoke(t, core, runnerLabelsRemove.ID, "repo-listed", `{"repository":"octo-org/example","runner_id":6,"label":"a/b"}`, true); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(r.paths, "DELETE "+runnerRepoRoute+"/6/labels/a%2Fb") {
		t.Errorf("paths = %v, want the label as one escaped segment", r.paths)
	}
	// Read-only labels, named in any case, are refused before a change is sent.
	for _, tt := range []struct{ tool, arguments string }{
		{runnerLabelsRemove.ID, `{"repository":"octo-org/example","runner_id":6,"label":"linux"}`},
		{runnerLabelsRemove.ID, `{"repository":"octo-org/example","runner_id":6,"label":"self-hosted"}`},
		{runnerLabelsAdd.ID, `{"repository":"octo-org/example","runner_id":6,"labels":["x64"]}`},
		{runnerLabelsSet.ID, `{"repository":"octo-org/example","runner_id":6,"labels":["gpu","Linux"]}`},
	} {
		before := len(f.recorded())
		if _, err := invoke(t, core, tt.tool, "repo-listed", tt.arguments, true); !isInvalidRequest(err) ||
			!strings.Contains(err.Error(), "read-only label") {
			t.Errorf("%s %s = %v, want a read-only refusal", tt.tool, tt.arguments, err)
		}
		if got := methodCounts(f.recorded()[before:]); got[http.MethodGet] != 1 || len(got) != 1 {
			t.Errorf("%s sent %v, want the evidence read only", tt.tool, got)
		}
	}
	// A runner of another level is refused by the evidence read.
	before := len(f.recorded())
	_, err := invoke(t, core, runnerLabelsAdd.ID, "repo-listed", `{"repository":"octo-org/example","runner_id":8,"labels":["gpu"]}`, true)
	if classOf(err) != provider.ClassNotFound || methodCounts(f.recorded()[before:])[http.MethodPost] != 0 {
		t.Errorf("a foreign runner = %v, want not-found without a change", err)
	}
}

// repository and organization are exclusive: neither, both, a foreign or excluded target, or a user is refused
// before a credential is resolved or GitHub is asked, for every tool.
func TestRunnerTargetIsExclusiveAndBoundBeforeIO(t *testing.T) {
	f, _, base := serveRunners(t)
	reads := 0
	core := runnersCore(t, base, &reads)
	extra := map[string]string{runnersList.ID: ``, runnersGet.ID: `,"runner_id":5`, runnersDelete.ID: `,"runner_id":5`,
		runnerLabelsList.ID: `,"runner_id":5`, runnerLabelsAdd.ID: `,"runner_id":5,"labels":["a"]`,
		runnerLabelsSet.ID: `,"runner_id":5,"labels":["a"]`, runnerLabelsRemove.ID: `,"runner_id":5,"label":"a"`}
	for id, more := range extra {
		for _, tt := range []struct{ name, connection, target string }{
			{"neither", "open", ``},
			{"both", "open", `"repository":"octo-org/example","organization":"orgs/octo-org",`},
			{"an organization outside the targets", "org-listed", `"organization":"orgs/other-org",`},
			{"an excluded organization", "org-excluded", `"organization":"orgs/other-org",`},
			{"a user instead of an organization", "open", `"organization":"users/octocat",`},
			{"a repository outside the targets", "repo-listed", `"repository":"octo-org/other",`},
			{"an excluded repository", "repo-excluded", `"repository":"octo-org/example",`},
		} {
			reads = 0
			before := len(f.recorded())
			arguments := `{` + tt.target + strings.TrimPrefix(more, `,`) + `}`
			if tt.target != "" && more == "" {
				arguments = `{` + strings.TrimSuffix(tt.target, `,`) + `}`
			}
			if _, err := invoke(t, core, id, tt.connection, arguments, true); !isInvalidRequest(err) {
				t.Errorf("%s %s = %v, want an invalid request", id, tt.name, err)
			}
			if reads != 0 || len(f.recorded()) != before {
				t.Errorf("%s %s reached the credential or GitHub", id, tt.name)
			}
		}
	}
}

// Every runner tool is offered only where a connection's tools list names it, a change without confirmation
// sends nothing, and no runner tool is part of a profile a connection starts with except the reads of the
// Actions administrator.
func TestRunnerToolsNeedTheirNameInTheToolsList(t *testing.T) {
	f, _, base := serveRunners(t)
	reads := 0
	core := runnersCore(t, base, &reads)
	repo := `"repository":"octo-org/example","runner_id":5`
	arguments := map[string]string{
		runnersList.ID: `{"repository":"octo-org/example"}`, runnersGet.ID: `{` + repo + `}`, runnersDelete.ID: `{` + repo + `}`,
		runnerLabelsList.ID: `{` + repo + `}`, runnerLabelsAdd.ID: `{` + repo + `,"labels":["a"]}`,
		runnerLabelsSet.ID: `{` + repo + `,"labels":[]}`, runnerLabelsRemove.ID: `{` + repo + `,"label":"gpu"}`,
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
		if !slices.Contains(runnersChanges, id) {
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
	// The same holds for a connection that selects by effects: it never offers a runner tool.
	cfg := runnersConfig(base)
	cfg.Connections["effects"] = config.Connection{Service: "gh", Credential: "gh-reader", Target: repoTarget,
		Effects: []config.Effect{config.EffectRead, config.EffectUpdate, config.EffectDelete}}
	effects := application.New(registry(t), cfg, resolver(&redact.Redactor{}, nil), &redact.Redactor{})
	for id := range arguments {
		var unsupported *capability.UnsupportedError
		if _, err := effects.Describe(application.DescribeRequest{Operation: id, Connection: "effects"}); !errors.As(err, &unsupported) {
			t.Errorf("describe %s on an effects connection = %v, want unsupported", id, err)
		}
	}
	metadata, _ := registry(t).ProviderMetadata(Provider)
	for _, profile := range metadata.Profiles {
		for _, id := range profile.Tools {
			if slices.Contains(runnersChanges, id) {
				t.Errorf("profile %s selects %s", profile.ID, id)
			}
			if slices.Contains(runnersReads, id) && (profile.Recommended || profile.ID != "actions-admin") {
				t.Errorf("profile %s selects %s", profile.ID, id)
			}
		}
	}
}

// Every check runs before a credential is resolved, and no label name or provider text is quoted.
func TestRunnerArgumentsAreValidatedBeforeIO(t *testing.T) {
	f, _, base := serveRunners(t)
	reads := 0
	core := runnersCore(t, base, &reads)
	repo := `"repository":"octo-org/example"`
	many := make([]string, 101)
	for i := range many {
		many[i] = fmt.Sprintf(`"l%d"`, i)
	}
	for _, tt := range []struct{ name, tool, arguments string }{
		{"no runner id", runnersGet.ID, `{` + repo + `}`},
		{"runner id zero", runnersGet.ID, `{` + repo + `,"runner_id":0}`},
		{"a negative runner id", runnersDelete.ID, `{` + repo + `,"runner_id":-4}`},
		{"a fractional runner id", runnerLabelsList.ID, `{` + repo + `,"runner_id":1.5}`},
		{"limit zero", runnersList.ID, `{` + repo + `,"limit":0,"cursor":"x"}`},
		{"limit too large", runnersList.ID, `{` + repo + `,"limit":101}`},
		{"a name with a line break", runnersList.ID, `{` + repo + `,"name":"a\nb"}`},
		{"no labels", runnerLabelsAdd.ID, `{` + repo + `,"runner_id":5}`},
		{"an empty list to add", runnerLabelsAdd.ID, `{` + repo + `,"runner_id":5,"labels":[]}`},
		{"too many labels", runnerLabelsSet.ID, `{` + repo + `,"runner_id":5,"labels":[` + strings.Join(many, ",") + `]}`},
		{"a repeated label", runnerLabelsSet.ID, `{` + repo + `,"runner_id":5,"labels":["a","A"]}`},
		{"an empty label", runnerLabelsAdd.ID, `{` + repo + `,"runner_id":5,"labels":[""]}`},
		{"a padded label", runnerLabelsAdd.ID, `{` + repo + `,"runner_id":5,"labels":[" a"]}`},
		{"a control character", runnerLabelsAdd.ID, `{` + repo + `,"runner_id":5,"labels":["a\u0007b"]}`},
		{"a long label", runnerLabelsAdd.ID, `{` + repo + `,"runner_id":5,"labels":["` + strings.Repeat("a", 101) + `"]}`},
		{"a dot label", runnerLabelsRemove.ID, `{` + repo + `,"runner_id":5,"label":".."}`},
		{"no label to remove", runnerLabelsRemove.ID, `{` + repo + `,"runner_id":5}`},
		{"an unknown member", runnersGet.ID, `{` + repo + `,"runner_id":5,"token":true}`},
	} {
		reads = 0
		before := len(f.recorded())
		if _, err := invoke(t, core, tt.tool, "repo-listed", tt.arguments, true); !isInvalidRequest(err) {
			t.Errorf("%s = %v, want an invalid request", tt.name, err)
		}
		if reads != 0 || len(f.recorded()) != before {
			t.Errorf("%s reached the credential or GitHub", tt.name)
		}
	}
}

// GitHub's refusals become Qatlas' own messages without GitHub's text and name the right each level needs; a
// refusal or an unclear outcome of a change is sent once and never repeated.
func TestRunnerFailuresNameTheirMeaningWithoutProviderText(t *testing.T) {
	f, r, base := serveRunners(t)
	core := runnersCore(t, base, nil)
	levels := map[string]struct {
		connection, target, right string
		id                        int
	}{
		"repository":   {"repo-listed", `"repository":"octo-org/example"`, "repo on a classic token", 6},
		"organization": {"org-listed", `"organization":"orgs/octo-org"`, "admin:org", 8},
	}
	for name, level := range levels {
		for _, tt := range []struct {
			method    string
			status    int
			class     provider.Class
			fragment  string
			uncertain bool
			deletes   int
		}{
			{http.MethodGet, http.StatusForbidden, provider.ClassPermission, level.right, false, 0},
			{http.MethodGet, http.StatusNotFound, provider.ClassNotFound, "does not hold", false, 0},
			{http.MethodDelete, http.StatusForbidden, provider.ClassPermission, level.right, false, 1},
			{http.MethodDelete, http.StatusUnprocessableEntity, provider.ClassProviderError, "nothing was removed", false, 1},
			{http.MethodDelete, http.StatusGatewayTimeout, provider.ClassTimeout, "did not answer in time", true, 1},
			{http.MethodDelete, http.StatusBadGateway, provider.ClassProviderError, "HTTP 502", true, 1},
		} {
			r.status = map[string]int{tt.method: tt.status}
			before := len(f.recorded())
			// A refused evidence read has to come from a state where the GET works, so a method other than
			// GET leaves the read free.
			_, err := invoke(t, core, runnersDelete.ID, level.connection,
				`{`+level.target+`,"runner_id":`+strconv.Itoa(level.id)+`}`, true)
			if classOf(err) != tt.class || err == nil || !strings.Contains(err.Error(), tt.fragment) ||
				strings.Contains(err.Error(), uncertain) != tt.uncertain || strings.Contains(err.Error(), runnerProviderCanary) {
				t.Errorf("%s %s answered %d = %v, want %s naming %q", name, tt.method, tt.status, err, tt.class, tt.fragment)
			}
			if got := methodCounts(f.recorded()[before:])[http.MethodDelete]; got != tt.deletes {
				t.Errorf("%s %s answered %d sent %d removals, want %d", name, tt.method, tt.status, got, tt.deletes)
			}
		}
		r.status = map[string]int{}
		// Label changes: a rejected list says nothing was changed; a timeout says the change may have been applied.
		for _, tt := range []struct {
			status    int
			class     provider.Class
			fragment  string
			uncertain bool
		}{
			{http.StatusUnprocessableEntity, provider.ClassProviderError, "nothing was changed", false},
			{http.StatusNotFound, provider.ClassNotFound, "does not hold", false},
			{http.StatusGatewayTimeout, provider.ClassTimeout, "did not answer in time", true},
		} {
			r.status = map[string]int{http.MethodPut: tt.status}
			before := len(f.recorded())
			_, err := invoke(t, core, runnerLabelsSet.ID, level.connection,
				`{`+level.target+`,"runner_id":`+strconv.Itoa(level.id)+`,"labels":["gpu"]}`, true)
			if classOf(err) != tt.class || err == nil || !strings.Contains(err.Error(), tt.fragment) ||
				strings.Contains(err.Error(), uncertain) != tt.uncertain || strings.Contains(err.Error(), runnerProviderCanary) {
				t.Errorf("%s labels answered %d = %v, want %s naming %q", name, tt.status, err, tt.class, tt.fragment)
			}
			if got := methodCounts(f.recorded()[before:])[http.MethodPut]; got != 1 {
				t.Errorf("%s labels answered %d sent %d changes, want 1", name, tt.status, got)
			}
		}
		r.status = map[string]int{http.MethodGet: http.StatusForbidden}
		_, err := invoke(t, core, runnersList.ID, level.connection, `{`+level.target+`}`, false)
		if classOf(err) != provider.ClassPermission || err == nil || !strings.Contains(err.Error(), "reading them needs") ||
			strings.Contains(err.Error(), "changing") {
			t.Errorf("%s read refused = %v, want the read right", name, err)
		}
		r.status = map[string]int{}
	}
}

// GitHub's answers are untrusted data: an entry without identifier or name is an invalid response, an
// unreadable label answer after a change says the change may have been applied, text is cut hard, and nothing
// outside the documented members is passed on.
func TestRunnerAnswersAreBoundedAndChecked(t *testing.T) {
	_, r, base := serveRunners(t)
	core := runnersCore(t, base, nil)
	long := strings.Repeat("n", 500)
	r.runners[runnerRepoRoute][0]["name"] = long + "\x07"
	r.runners[runnerRepoRoute][0]["token"] = "must-not-appear"
	got, err := invoke(t, core, runnersGet.ID, "repo-listed", `{"repository":"octo-org/example","runner_id":5}`, false)
	if err != nil || strings.Contains(string(got), "must-not-appear") || strings.Contains(string(got), "\\u0007") ||
		!strings.Contains(string(got), `"name":"`+strings.Repeat("n", projectionNameLimit)+`"`) {
		t.Errorf("long name = %s, %v, want a clean, cut name", got, err)
	}
	r.runners[runnerRepoRoute][1]["name"] = nil
	if _, err := invoke(t, core, runnersList.ID, "repo-listed", `{"repository":"octo-org/example"}`, false); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("a runner without a name = %v, want invalid-provider-response", err)
	}
	r.brokenLabels = true
	_, err = invoke(t, core, runnerLabelsAdd.ID, "repo-listed", `{"repository":"octo-org/example","runner_id":5,"labels":["gpu2"]}`, true)
	if classOf(err) != provider.ClassInvalidResponse || err == nil || !strings.Contains(err.Error(), uncertain) {
		t.Errorf("an unreadable label answer = %v, want an invalid response that names the open outcome", err)
	}
}

// A 404 names the runner or the labels of a runner and the level, never a label name.
func TestRunnerSubjectsNameNoUntrustedText(t *testing.T) {
	for path, want := range map[string]string{
		"actions/runners":                  "the self-hosted runners",
		"actions/runners/5":                "self-hosted runner 5",
		"actions/runners/5/labels":         "the labels of self-hosted runner 5",
		"actions/runners/5/labels/a%2Fb":   "a label of self-hosted runner 5",
		"actions/runners/not-a-number":     "a self-hosted runner",
		"actions/runnersx":                 "",
		"actions/variables":                "",
		"environments/x/variables/runners": "",
	} {
		if got := runnerSubject(path); got != want {
			t.Errorf("runnerSubject(%q) = %q, want %q", path, got, want)
		}
	}
	_, _, base := serveRunners(t)
	core := runnersCore(t, base, nil)
	_, err := invoke(t, core, runnersGet.ID, "org-listed", `{"organization":"orgs/octo-org","runner_id":99}`, false)
	if err == nil || !strings.Contains(err.Error(), "self-hosted runner 99 of orgs/octo-org") {
		t.Errorf("missing organization runner = %v, want it named with its level", err)
	}
	_, err = invoke(t, core, runnersGet.ID, "repo-listed", `{"repository":"octo-org/example","runner_id":99}`, false)
	if err == nil || !strings.Contains(err.Error(), "self-hosted runner 99") {
		t.Errorf("missing repository runner = %v, want it named", err)
	}
}
