package googlecloud

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

func isInvalidRequest(err error) bool {
	var refused *provider.InvalidRequestError
	return errors.As(err, &refused)
}

func projectRow(number, id, parent string) string {
	return fmt.Sprintf(`{"name":"projects/%s","projectId":"%s","parent":"%s","state":"ACTIVE"}`, number, id, parent)
}

func listOf(t *testing.T, out, member string) (rows []map[string]any, got map[string]any) {
	t.Helper()
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	items, _ := got[member].([]any)
	for _, item := range items {
		rows = append(rows, item.(map[string]any))
	}
	return rows, got
}

func queriesOf(e *environment) []url.Values {
	var out []url.Values
	for _, r := range e.fake.api() {
		u, _ := url.Parse(r.url)
		out = append(out, u.Query())
	}
	return out
}

func TestProjectsSearchFollowsThreePagesWithFilters(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.hook = func(_ int, req *http.Request) *reply {
		if req.URL.Path != "/v3/projects:search" {
			return nil
		}
		switch req.URL.Query().Get("pageToken") {
		case "":
			return &reply{status: 200, body: `{"projects":[` + projectRow("1", "alpha-project-1", "folders/2001") + `],"nextPageToken":"page-two"}`}
		case "page-two":
			return &reply{status: 200, body: `{"projects":[` + projectRow("2", "beta-project-22", "folders/2001") + `],"nextPageToken":"page-three"}`}
		}
		return &reply{status: 200, body: `{"projects":[` + projectRow("3", "gamma-project-3", "folders/2001") + `]}`}
	}
	const filters = `"parent":"folders/2001","state":"ACTIVE","labels":{"env":"prod","team":"*"},"page_size":1`
	var ids []string
	cursor := ""
	for page := 1; page <= 3; page++ {
		arguments := `{` + filters
		if cursor != "" {
			arguments += `,"cursor":"` + cursor + `"`
		}
		out, err := e.invoke(projectsSearchID, arguments+`}`)
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		rows, got := listOf(t, out, "projects")
		for _, row := range rows {
			ids = append(ids, row["projectId"].(string))
		}
		next, _ := got["next_cursor"].(string)
		if more := got["has_more"].(bool); more != (page < 3) || (next != "") != (page < 3) {
			t.Fatalf("page %d: has_more=%v next_cursor=%q", page, more, next)
		}
		if strings.Contains(next, "page-two") || strings.Contains(next, "page-three") {
			t.Errorf("the cursor shows Google's token: %q", next)
		}
		cursor = next
	}
	if want := []string{"alpha-project-1", "beta-project-22", "gamma-project-3"}; !slices.Equal(ids, want) {
		t.Errorf("ids = %v, want %v", ids, want)
	}
	queries := queriesOf(e)
	if len(queries) != 3 {
		t.Fatalf("requests = %d, want 3", len(queries))
	}
	wantQuery := "parent:folders/2001 state:ACTIVE labels.env:prod labels.team:*"
	for i, q := range queries {
		if q.Get("query") != wantQuery || q.Get("pageSize") != "1" {
			t.Errorf("request %d: query=%q pageSize=%q", i, q.Get("query"), q.Get("pageSize"))
		}
	}
	if queries[0].Get("pageToken") != "" || queries[1].Get("pageToken") != "page-two" || queries[2].Get("pageToken") != "page-three" {
		t.Errorf("page tokens = %q %q %q", queries[0].Get("pageToken"), queries[1].Get("pageToken"), queries[2].Get("pageToken"))
	}
}

func TestSearchCursorIsBoundToTheRequest(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes["/v3/projects:search"] = okReply(`{"projects":[` + projectRow("1", "alpha-project-1", "folders/2001") + `],"nextPageToken":"page-two"}`)
	e.fake.routes["/v3/folders:search"] = okReply(`{"folders":[],"nextPageToken":"folder-page-two"}`)
	out, err := e.invoke(projectsSearchID, `{"state":"ACTIVE"}`)
	if err != nil {
		t.Fatal(err)
	}
	_, got := listOf(t, out, "projects")
	cursor := got["next_cursor"].(string)
	folderOut, err := e.invoke(foldersSearchID, `{"state":"ACTIVE"}`)
	if err != nil {
		t.Fatal(err)
	}
	_, folderPage := listOf(t, folderOut, "folders")
	folderCursor := folderPage["next_cursor"].(string)
	before := len(e.fake.api())

	flipped := []byte(cursor)
	flipped[len(flipped)-1] ^= 1
	for name, arguments := range map[string]string{
		"altered":           `{"state":"ACTIVE","cursor":"` + string(flipped) + `"}`,
		"other filter":      `{"state":"DELETE_REQUESTED","cursor":"` + cursor + `"}`,
		"no filter":         `{"cursor":"` + cursor + `"}`,
		"cut short":         `{"state":"ACTIVE","cursor":"` + cursor[:len(cursor)-4] + `"}`,
		"foreign":           `{"state":"ACTIVE","cursor":"c29tZXRoaW5nLWVsc2U"}`,
		"cursor of a tool":  `{"state":"ACTIVE","cursor":"` + folderCursor + `"}`,
		"google token only": `{"state":"ACTIVE","cursor":"page-two"}`,
	} {
		if _, err := e.invoke(projectsSearchID, arguments); !isInvalidRequest(err) {
			t.Errorf("%s: err = %v, want a refusal of the cursor", name, err)
		}
	}
	if got := len(e.fake.api()); got != before {
		t.Errorf("a refused cursor reached Google: %d requests, want %d", got, before)
	}
	// A different page size keeps the sequence, so the cursor still continues it.
	if _, err := e.invoke(projectsSearchID, `{"state":"ACTIVE","page_size":5,"cursor":"`+cursor+`"}`); err != nil {
		t.Errorf("page_size broke the cursor: %v", err)
	}
	// Another connection scope binds another cursor: the same filter under other targets is a new search.
	other := newEnvironment(t, []string{"organizations/9001"})
	if _, err := other.invoke(projectsSearchID, `{"state":"ACTIVE","cursor":"`+cursor+`"}`); err == nil {
		t.Error("a cursor of an unbound connection continued a bound one")
	}
	if len(other.fake.api()) != 0 {
		t.Error("a refused cursor reached Google")
	}
}

func TestSearchDropsRowsOutsideTheTargets(t *testing.T) {
	// folders/2001 -> folders/2002 -> organizations/9001, and folders/7001 -> organizations/9999.
	routes := func(e *environment) {
		e.fake.routes["/v3/folders/2001"] = okReply(`{"name":"folders/2001","parent":"folders/2002"}`)
		e.fake.routes["/v3/folders/2002"] = okReply(`{"name":"folders/2002","parent":"organizations/9001"}`)
		e.fake.routes["/v3/folders/7001"] = okReply(`{"name":"folders/7001","parent":"organizations/9999"}`)
	}
	t.Run("projects", func(t *testing.T) {
		e := newEnvironment(t, []string{"folders/2002", "projects/direct-project-1"})
		routes(e)
		e.fake.routes["/v3/projects:search"] = okReply(`{"projects":[` + strings.Join([]string{
			projectRow("1", "inside-project-1", "folders/2001"),
			projectRow("2", "inside-project-2", "folders/2001"),
			projectRow("3", "outside-project-3", "folders/7001"),
			projectRow("4", "outside-project-4", "organizations/9999"),
			projectRow("5", "direct-project-1", "organizations/9999"),
			`{"name":"projects/6","projectId":"orphan-project-6"}`,
		}, ",") + `]}`)
		out, err := e.invoke(projectsSearchID, `{}`)
		if err != nil {
			t.Fatal(err)
		}
		rows, _ := listOf(t, out, "projects")
		var ids []string
		for _, row := range rows {
			ids = append(ids, row["projectId"].(string))
		}
		if want := []string{"inside-project-1", "inside-project-2", "direct-project-1"}; !slices.Equal(ids, want) {
			t.Errorf("ids = %v, want %v", ids, want)
		}
		for _, leaked := range []string{"outside-project", "orphan", "7001"} {
			if strings.Contains(out, leaked) {
				t.Errorf("the result names %q", leaked)
			}
		}
		// folders/2001 is read once for both rows, folders/7001 once; folders/2002 is a target and needs no read.
		var reads []string
		for _, r := range e.fake.api() {
			if u, _ := url.Parse(r.url); strings.HasPrefix(u.Path, "/v3/folders/") {
				reads = append(reads, u.Path)
			}
		}
		if want := []string{"/v3/folders/2001", "/v3/folders/7001"}; !slices.Equal(reads, want) {
			t.Errorf("folder reads = %v, want %v", reads, want)
		}
	})
	t.Run("decided by the parent without a read", func(t *testing.T) {
		e := newEnvironment(t, []string{"organizations/9001"})
		e.fake.routes["/v3/projects:search"] = okReply(`{"projects":[` + projectRow("1", "inside-project-1", "organizations/9001") +
			`,` + projectRow("2", "outside-project-2", "organizations/9999") + `]}`)
		out, err := e.invoke(projectsSearchID, `{}`)
		if err != nil {
			t.Fatal(err)
		}
		if rows, _ := listOf(t, out, "projects"); len(rows) != 1 {
			t.Errorf("rows = %d, want 1", len(rows))
		}
		if got := len(e.fake.api()); got != 1 {
			t.Errorf("requests = %d, want the search only", got)
		}
	})
	t.Run("folders", func(t *testing.T) {
		e := newEnvironment(t, []string{"folders/2002"})
		routes(e)
		e.fake.routes["/v3/folders:search"] = okReply(`{"folders":[` + strings.Join([]string{
			`{"name":"folders/2002","parent":"organizations/9001","displayName":"The target"}`,
			`{"name":"folders/2001","parent":"folders/2002","displayName":"Below"}`,
			`{"name":"folders/2003","parent":"folders/2001","displayName":"Deeper"}`,
			`{"name":"folders/7001","parent":"organizations/9999","displayName":"Elsewhere"}`,
			`{"name":"folders/7002","parent":"folders/7001","displayName":"Elsewhere below"}`,
		}, ",") + `]}`)
		out, err := e.invoke(foldersSearchID, `{}`)
		if err != nil {
			t.Fatal(err)
		}
		rows, _ := listOf(t, out, "folders")
		var names []string
		for _, row := range rows {
			names = append(names, row["name"].(string))
		}
		if want := []string{"folders/2002", "folders/2001", "folders/2003"}; !slices.Equal(names, want) {
			t.Errorf("names = %v, want %v", names, want)
		}
		if strings.Contains(out, "Elsewhere") {
			t.Error("a folder outside the targets is in the result")
		}
	})
	t.Run("organizations", func(t *testing.T) {
		e := newEnvironment(t, []string{"organizations/9001", "folders/2001"})
		e.fake.routes["/v3/organizations:search"] = okReply(`{"organizations":[` +
			`{"name":"organizations/9001","displayName":"mine.example","directoryCustomerId":"C01"},` +
			`{"name":"organizations/9999","displayName":"other.example"}]}`)
		out, err := e.invoke(organizationsSearchID, `{}`)
		if err != nil {
			t.Fatal(err)
		}
		rows, _ := listOf(t, out, "organizations")
		if len(rows) != 1 || rows[0]["name"] != "organizations/9001" || rows[0]["directoryCustomerId"] != "C01" ||
			strings.Contains(out, "other.example") {
			t.Errorf("out = %s", out)
		}
	})
}

func TestSearchRefusesAnInvalidRow(t *testing.T) {
	for tool, body := range map[string]string{
		projectsSearchID:      `{"projects":[{"name":"folders/1"}]}`,
		foldersSearchID:       `{"folders":[{"name":"projects/1"}]}`,
		organizationsSearchID: `{"organizations":[{"name":"organizations/x"}]}`,
	} {
		e := newEnvironment(t, []string{"organizations/9001"})
		path := "/v3/" + tool[len(Provider)+1:len(tool)-len(".search")] + ":search"
		e.fake.routes[path] = okReply(body)
		if _, err := e.invoke(tool, `{}`); classOf(err) != provider.ClassInvalidResponse {
			t.Errorf("%s: err = %v, want invalid-provider-response", tool, err)
		}
	}
	e := newEnvironment(t, nil)
	e.fake.routes["/v3/projects:search"] = okReply(`{"projects":"no list"}`)
	if _, err := e.invoke(projectsSearchID, `{}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("err = %v, want invalid-provider-response", err)
	}
	e.fake.routes["/v3/projects:search"] = okReply(`{"projects":[],"nextPageToken":"` + strings.Repeat("a", maxPageTokenBytes+1) + `"}`)
	if _, err := e.invoke(projectsSearchID, `{}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("a page token too long to continue: err = %v", err)
	}
}

func TestSearchEmptyPageIsAnEmptyList(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes["/v3/projects:search"] = okReply(`{}`)
	out, err := e.invoke(projectsSearchID, `{}`)
	rows, got := listOf(t, out, "projects")
	if err != nil || len(rows) != 0 || got["has_more"] == true || got["next_cursor"] != nil {
		t.Errorf("out=%s err=%v", out, err)
	}
}

func TestSearchBoundsTheReadsThatProveTheTargets(t *testing.T) {
	e := newEnvironment(t, []string{"organizations/9001"})
	e.fake.hook = func(_ int, req *http.Request) *reply {
		var id int
		if _, err := fmt.Sscanf(req.URL.Path, "/v3/folders/%d", &id); err == nil {
			return &reply{status: 200, body: fmt.Sprintf(`{"name":"folders/%d","parent":"folders/%d"}`, id, id+1)}
		}
		return nil
	}
	e.fake.routes["/v3/projects:search"] = okReply(`{"projects":[` + strings.Join([]string{
		projectRow("1", "chain-project-1", "folders/100"),
		projectRow("2", "chain-project-2", "folders/200"),
		projectRow("3", "chain-project-3", "folders/300"),
	}, ",") + `]}`)
	out, err := e.invoke(projectsSearchID, `{}`)
	if !isInvalidRequest(err) || out != "" {
		t.Fatalf("out=%q err=%v, want a refusal that asks for a narrower search", out, err)
	}
	if got := len(e.fake.api()); got != 1+maxProofReads {
		t.Errorf("requests = %d, want the search and %d reads", got, maxProofReads)
	}
}

func TestOrganizationsSearchNeedsAnOrganizationTarget(t *testing.T) {
	for name, targets := range map[string][]string{
		"unbound":            nil,
		"folder":             {"folders/2001"},
		"project":            {"projects/my-project-123"},
		"billing account":    {"billingAccounts/012345-ABCDEF-678901"},
		"folder and project": {"folders/2001", "projects/my-project-123"},
	} {
		e := newEnvironment(t, targets)
		e.fake.routes["/v3/organizations:search"] = okReply(`{"organizations":[{"name":"organizations/9001"}]}`)
		// The core does not even offer the tool to such a connection, and the handler refuses on its own.
		if out, err := e.invoke(organizationsSearchID, `{}`); err == nil || out != "" {
			t.Errorf("%s: out=%q err=%v, want a refusal", name, out, err)
		}
		out, err := handleOrganizationsSearch(context.Background(), e.resolved(), e.secrets(), e.red, json.RawMessage(`{}`))
		if out != nil || !isInvalidRequest(err) {
			t.Errorf("%s: handler out=%v err=%v, want a refusal of the request", name, out, err)
		}
		if len(e.fake.api()) != 0 || e.fake.tokenRequests() != 0 || e.lookups != 0 {
			t.Errorf("%s: api=%d tokens=%d lookups=%d, want a refusal before I/O and secret", name,
				len(e.fake.api()), e.fake.tokenRequests(), e.lookups)
		}
	}
}

func TestSearchToolsRefuseTargetsThatCannotHoldTheirRows(t *testing.T) {
	for _, c := range []struct {
		tool    string
		targets []string
	}{
		{foldersSearchID, []string{"projects/my-project-123"}},
		{foldersSearchID, []string{"billingAccounts/012345-ABCDEF-678901"}},
		{projectsSearchID, []string{"billingAccounts/012345-ABCDEF-678901"}},
	} {
		e := newEnvironment(t, c.targets)
		if _, err := e.invoke(c.tool, `{}`); err == nil {
			t.Errorf("%s with %v ran", c.tool, c.targets)
		}
		if len(e.fake.api()) != 0 || e.lookups != 0 {
			t.Errorf("%s with %v: api=%d lookups=%d", c.tool, c.targets, len(e.fake.api()), e.lookups)
		}
	}
}

func TestSearchQuotesFilterValues(t *testing.T) {
	cases := []struct {
		value    string
		wildcard bool
		want     string
		refused  bool
	}{
		{"Test", true, "Test", false},
		{"Test*", true, "Test*", false},
		{"my-folder_1.x", true, "my-folder_1.x", false},
		{"Test String", true, `"Test String"`, false},
		{"Büro Süd", true, `"Büro Süd"`, false},
		{"a:b", true, `"a:b"`, false},
		{"x OR parent:folders/1", true, `"x OR parent:folders/1"`, false},
		{"or", true, `"or"`, false},
		{"NOT", false, `"NOT"`, false},
		{"Test String*", true, "", true},
		{"Te*st", true, "", true},
		{"*", true, "", true},
		{"Test*", false, "", true},
		{`say "hi"`, true, "", true},
		{`back\slash`, true, "", true},
		{"line\nbreak", true, "", true},
		{"", true, "", true},
		{strings.Repeat("a", maxFilterValue+1), true, "", true},
	}
	for _, c := range cases {
		got, err := filterValue("display_name", c.value, c.wildcard)
		if c.refused {
			if err == nil || strings.Contains(err.Error(), c.value) && c.value != "" && len(c.value) < 20 {
				t.Errorf("filterValue(%q) = %q, %v; want a refusal that does not echo the value", c.value, got, err)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("filterValue(%q) = %q, %v; want %q", c.value, got, err, c.want)
		}
	}
}

func TestSearchBuildsTheQueryOfEveryDocumentedField(t *testing.T) {
	cases := []struct {
		tool, arguments, want string
	}{
		{foldersSearchID, `{"parent":"organizations/9001","display_name":"Test String","state":"ACTIVE"}`,
			`parent=organizations/9001 AND displayName="Test String" AND state=ACTIVE`},
		{foldersSearchID, `{"display_name":"Test*"}`, `displayName=Test*`},
		{projectsSearchID, `{"parent":"folders/1","parent_type":"folder","parent_id":"1","project_id":"my-proj*",` +
			`"display_name":"My Project","state":"DELETE_REQUESTED","label_text":"red"}`,
			`parent:folders/1 parent.type:folder parent.id:1 projectId:my-proj* displayName:"My Project" ` +
				`state:DELETE_REQUESTED labels:red`},
		{projectsSearchID, `{"labels":{"b":"2","a":"*"}}`, `labels.a:* labels.b:2`},
		{organizationsSearchID, `{"directory_customer_id":"123456789"}`, `directoryCustomerId:123456789`},
		{organizationsSearchID, `{"domain":"example.com"}`, `domain:example.com`},
	}
	for _, c := range cases {
		e := newEnvironment(t, []string{"organizations/9001"})
		member := strings.Split(c.tool, ".")[2]
		e.fake.routes["/v3/"+member+":search"] = okReply(`{}`)
		if _, err := e.invoke(c.tool, c.arguments); err != nil {
			t.Errorf("%s %s: %v", c.tool, c.arguments, err)
			continue
		}
		queries := queriesOf(e)
		if len(queries) != 1 || queries[0].Get("query") != c.want {
			t.Errorf("%s %s: query = %v, want %q", c.tool, c.arguments, queries, c.want)
		}
	}
}

func TestSearchRefusesBadFilterArgumentsBeforeAnyRequest(t *testing.T) {
	for tool, list := range map[string][]string{
		projectsSearchID: {
			`{"query":"parent:folders/1"}`, `{"parent":"projects/1"}`, `{"parent":"folders/1 OR parent:folders/2"}`,
			`{"project_id":"My-Project"}`, `{"project_id":"a b"}`, `{"state":"STATE_UNSPECIFIED"}`,
			`{"display_name":"a\"b"}`, `{"labels":{"Bad Key":"x"}}`, `{"labels":{"k":"a b"}}`,
			`{"labels":{"k":""}}`, `{"label_text":"a*"}`, `{"parent_type":"project"}`, `{"parent_id":"x"}`,
			`{"page_size":0}`, `{"page_size":101}`, `{"cursor":""}`,
		},
		foldersSearchID: {`{"project_id":"my-project-123"}`, `{"labels":{"a":"b"}}`, `{"parent":"folders/x"}`},
		organizationsSearchID: {
			`{"domain":"a b"}`, `{"directory_customer_id":"abc"}`,
			`{"domain":"example.com","directory_customer_id":"123"}`, `{"state":"ACTIVE"}`,
		},
	} {
		e := newEnvironment(t, []string{"organizations/9001"})
		for _, arguments := range list {
			if _, err := e.invoke(tool, arguments); err == nil {
				t.Errorf("%s %s accepted", tool, arguments)
			}
		}
		if len(e.fake.api()) != 0 || e.lookups != 0 {
			t.Errorf("%s: api=%d lookups=%d, want refusals before I/O", tool, len(e.fake.api()), e.lookups)
		}
	}
}

func TestSearchDescriptorsCoverTheDiscoveryDocument(t *testing.T) {
	// The members and the parameters of cloudresourcemanager v3, revision 20261107, minus the deprecated ones.
	cases := []struct {
		d       capability.Descriptor
		list    string
		members []string
		args    []string
	}{
		{organizationsSearch, "organizations", []string{"name", "displayName", "directoryCustomerId", "state",
			"createTime", "updateTime", "deleteTime", "etag"}, []string{"directory_customer_id", "domain", "page_size", "cursor"}},
		{foldersSearch, "folders", []string{"name", "parent", "displayName", "state", "createTime", "updateTime",
			"deleteTime", "etag", "tags", "configuredCapabilities", "managementProject"},
			[]string{"parent", "display_name", "state", "page_size", "cursor"}},
		{projectsSearch, "projects", []string{"name", "parent", "projectId", "state", "displayName", "createTime",
			"updateTime", "deleteTime", "etag", "labels", "tags", "managementType", "isManagementProject",
			"configuredCapabilities"}, []string{"parent", "parent_type", "parent_id", "project_id", "display_name",
			"state", "label_text", "labels", "page_size", "cursor"}},
	}
	for _, c := range cases {
		var output struct {
			Properties map[string]struct {
				Items struct {
					Properties map[string]json.RawMessage `json:"properties"`
				} `json:"items"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(c.d.OutputSchema, &output); err != nil {
			t.Fatal(err)
		}
		for _, member := range c.members {
			if _, found := output.Properties[c.list].Items.Properties[member]; !found {
				t.Errorf("%s: output schema lacks %s", c.d.ID, member)
			}
		}
		for _, name := range []string{"next_cursor", "has_more"} {
			if _, found := output.Properties[name]; !found {
				t.Errorf("%s: output schema lacks %s", c.d.ID, name)
			}
		}
		var input struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if err := json.Unmarshal(c.d.InputSchema, &input); err != nil {
			t.Fatal(err)
		}
		if len(input.Properties) != len(c.args) {
			t.Errorf("%s: input schema has %d properties, want %d", c.d.ID, len(input.Properties), len(c.args))
		}
		for _, arg := range c.args {
			if _, found := input.Properties[arg]; !found {
				t.Errorf("%s: input schema lacks %s", c.d.ID, arg)
			}
			if !slices.ContainsFunc(c.d.Arguments, func(a capability.Argument) bool { return a.Name == arg }) {
				t.Errorf("%s: arguments lack %s", c.d.ID, arg)
			}
		}
	}
}
