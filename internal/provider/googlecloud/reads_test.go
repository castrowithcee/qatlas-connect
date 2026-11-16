package googlecloud

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

func folderRow(id, parent string) string {
	return `{"name":"folders/` + id + `","parent":"` + parent + `","displayName":"f` + id + `","state":"ACTIVE"}`
}

const organizationBody = `{"name":"organizations/123","displayName":"example.com","directoryCustomerId":"C0abc",` +
	`"state":"ACTIVE","createTime":"2026-01-02T03:04:05Z","updateTime":"2026-02-03T04:05:06Z","etag":"e1"}`

func TestOrganizationsGetNeedsTheOrganizationAmongTheTargets(t *testing.T) {
	for _, targets := range [][]string{nil, {"folders/1"}, {"organizations/999"}, {"projects/my-project-123"}} {
		e := newEnvironment(t, targets)
		_, err := e.invoke(organizationsGetID, `{"organization_id":"123"}`)
		// The core withholds the tool from a binding that never reaches it; the handler refuses the rest.
		if err == nil {
			t.Errorf("targets %v: err = %v, want a refusal", targets, err)
		}
		if err != nil && strings.Contains(err.Error(), "999") {
			t.Errorf("the refusal names a foreign target: %v", err)
		}
		if len(e.fake.api()) != 0 || e.fake.tokenRequests() != 0 || e.lookups != 0 {
			t.Errorf("targets %v: api=%d tokens=%d lookups=%d, want no I/O and no secret", targets,
				len(e.fake.api()), e.fake.tokenRequests(), e.lookups)
		}
	}
	e := newEnvironment(t, []string{"organizations/123"})
	e.fake.routes["/v3/organizations/123"] = okReply(organizationBody)
	out, err := e.invoke(organizationsGetID, `{"organization_id":"123"}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"name":"organizations/123"`, `"directoryCustomerId":"C0abc"`, `"etag":"e1"`,
		`"createTime"`, `"updateTime"`, `"displayName":"example.com"`, `"state":"ACTIVE"`} {
		if !strings.Contains(out, want) {
			t.Errorf("result lacks %s: %s", want, out)
		}
	}
}

func TestOrganizationsGetFailuresAndBadAnswers(t *testing.T) {
	cases := []struct {
		name  string
		reply reply
		want  provider.Class
	}{
		{"permission denied", reply{status: 403, body: errorObject(403, "PERMISSION_DENIED", "")}, provider.ClassPermission},
		{"not found", reply{status: 404, body: errorObject(404, "NOT_FOUND", "")}, provider.ClassNotFound},
		{"other organization", okReply(`{"name":"organizations/999"}`), provider.ClassInvalidResponse},
	}
	for _, c := range cases {
		e := newEnvironment(t, []string{"organizations/123"})
		e.fake.routes["/v3/organizations/123"] = c.reply
		_, err := e.invoke(organizationsGetID, `{"organization_id":"123"}`)
		if classOf(err) != c.want {
			t.Errorf("%s: err = %v (%s), want %s", c.name, err, classOf(err), c.want)
		}
	}
}

func TestFoldersGetProvesTheFolderAgainstTheTargets(t *testing.T) {
	e := newEnvironment(t, []string{"organizations/123"})
	e.fake.routes["/v3/folders/3"] = okReply(`{"name":"folders/3","parent":"folders/2","displayName":"x","state":"ACTIVE","etag":"e"}`)
	e.fake.routes["/v3/folders/2"] = okReply(`{"name":"folders/2","parent":"organizations/123"}`)
	out, err := e.invoke(foldersGetID, `{"folder_id":"3"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"parent":"folders/2"`) || !strings.Contains(out, `"etag":"e"`) {
		t.Errorf("result = %s", out)
	}
	if n := len(e.fake.api()); n != 2 {
		t.Errorf("requests = %d, want the folder and its parent", n)
	}
	// A folder below another organization is refused after the proof, without naming it.
	e = newEnvironment(t, []string{"organizations/123"})
	e.fake.routes["/v3/folders/3"] = okReply(`{"name":"folders/3","parent":"organizations/777"}`)
	_, err = e.invoke(foldersGetID, `{"folder_id":"3"}`)
	if !isInvalidRequest(err) || strings.Contains(err.Error(), "777") {
		t.Errorf("err = %v, want a refusal that names nothing", err)
	}
	// A folder target itself needs no walk.
	e = newEnvironment(t, []string{"folders/3"})
	e.fake.routes["/v3/folders/3"] = okReply(`{"name":"folders/3","parent":"organizations/777"}`)
	if _, err = e.invoke(foldersGetID, `{"folder_id":"3"}`); err != nil || len(e.fake.api()) != 1 {
		t.Errorf("err = %v, requests = %d", err, len(e.fake.api()))
	}
}

func TestFoldersGetRefusedBeforeIOWithoutAFolderRoot(t *testing.T) {
	e := newEnvironment(t, []string{"projects/my-project-123"})
	if _, err := e.invoke(foldersGetID, `{"folder_id":"3"}`); err == nil {
		t.Fatalf("err = %v", err)
	}
	if len(e.fake.api()) != 0 || e.lookups != 0 {
		t.Errorf("api=%d lookups=%d", len(e.fake.api()), e.lookups)
	}
}

func TestFoldersGetFailures(t *testing.T) {
	for status, want := range map[int]provider.Class{403: provider.ClassPermission, 404: provider.ClassNotFound} {
		e := newEnvironment(t, nil)
		e.fake.routes["/v3/folders/3"] = reply{status: status, body: errorObject(status, map[int]string{403: "PERMISSION_DENIED", 404: "NOT_FOUND"}[status], "")}
		if _, err := e.invoke(foldersGetID, `{"folder_id":"3"}`); classOf(err) != want {
			t.Errorf("status %d: err = %v", status, err)
		}
	}
}

func TestFoldersListFollowsThreePagesAndShowDeleted(t *testing.T) {
	e := newEnvironment(t, []string{"organizations/123"})
	e.fake.hook = func(_ int, req *http.Request) *reply {
		if req.URL.Path != "/v3/folders" {
			return nil
		}
		switch req.URL.Query().Get("pageToken") {
		case "":
			return &reply{status: 200, body: `{"folders":[` + folderRow("1", "organizations/123") + `],"nextPageToken":"p2"}`}
		case "p2":
			return &reply{status: 200, body: `{"folders":[` + folderRow("2", "organizations/123") + `],"nextPageToken":"p3"}`}
		}
		return &reply{status: 200, body: `{"folders":[` + folderRow("3", "organizations/123") + `]}`}
	}
	var names []string
	cursor := ""
	for page := 1; page <= 3; page++ {
		arguments := `{"parent":"organizations/123","show_deleted":true,"page_size":1`
		if cursor != "" {
			arguments += `,"cursor":"` + cursor + `"`
		}
		out, err := e.invoke(foldersListID, arguments+`}`)
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		rows, got := listOf(t, out, "folders")
		for _, row := range rows {
			names = append(names, row["name"].(string))
		}
		cursor, _ = got["next_cursor"].(string)
		if got["has_more"].(bool) != (page < 3) {
			t.Fatalf("page %d: has_more = %v", page, got["has_more"])
		}
	}
	if want := []string{"folders/1", "folders/2", "folders/3"}; !slices.Equal(names, want) {
		t.Errorf("names = %v", names)
	}
	for i, q := range queriesOf(e) {
		if q.Get("parent") != "organizations/123" || q.Get("showDeleted") != "true" || q.Get("pageSize") != "1" {
			t.Errorf("request %d: %v", i, q)
		}
	}
	// showDeleted stays off the query unless it was asked for.
	e = newEnvironment(t, nil)
	e.fake.routes["/v3/folders"] = okReply(`{}`)
	if _, err := e.invoke(foldersListID, `{"parent":"organizations/123"}`); err != nil {
		t.Fatal(err)
	}
	if q := queriesOf(e)[0]; q.Has("showDeleted") {
		t.Errorf("query = %v", q)
	}
}

func TestListCursorIsBoundToTheListAndTamperingIsRefused(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes["/v3/folders"] = okReply(`{"folders":[` + folderRow("1", "organizations/123") + `],"nextPageToken":"p2"}`)
	e.fake.routes["/v3/projects"] = okReply(`{"projects":[],"nextPageToken":"q2"}`)
	out, err := e.invoke(foldersListID, `{"parent":"organizations/123"}`)
	if err != nil {
		t.Fatal(err)
	}
	_, got := listOf(t, out, "folders")
	cursor := got["next_cursor"].(string)
	before := len(e.fake.api())
	for name, call := range map[string][2]string{
		"altered":      {foldersListID, `{"parent":"organizations/123","cursor":"` + cursor + `x"}`},
		"other parent": {foldersListID, `{"parent":"organizations/124","cursor":"` + cursor + `"}`},
		"other flag":   {foldersListID, `{"parent":"organizations/123","show_deleted":true,"cursor":"` + cursor + `"}`},
		"other tool":   {projectsListID, `{"parent":"organizations/123","cursor":"` + cursor + `"}`},
	} {
		if _, err := e.invoke(call[0], call[1]); !isInvalidRequest(err) {
			t.Errorf("%s: err = %v, want a refused cursor", name, err)
		}
	}
	if len(e.fake.api()) != before {
		t.Errorf("a refused cursor reached Google")
	}
}

func TestListParentOutsideTheTargetsIsRefusedBeforeIO(t *testing.T) {
	cases := []struct {
		targets []string
		parent  string
	}{
		{[]string{"organizations/123"}, "organizations/124"},
		{[]string{"folders/5"}, "organizations/123"}, // partial binding: the organization is not part of it
		{[]string{"folders/5"}, "organizations/124"},
		{[]string{"projects/my-project-123"}, "folders/5"},
		{[]string{"billingAccounts/012345-ABCDEF-678901"}, "organizations/123"},
	}
	for _, tool := range []string{foldersListID, projectsListID} {
		for _, c := range cases {
			e := newEnvironment(t, c.targets)
			_, err := e.invoke(tool, `{"parent":"`+c.parent+`"}`)
			if err == nil {
				t.Errorf("%s %v %s: err = %v", tool, c.targets, c.parent, err)
			}
			if len(e.fake.api()) != 0 || e.fake.tokenRequests() != 0 || e.lookups != 0 {
				t.Errorf("%s %v %s: api=%d lookups=%d, want a refusal before I/O", tool, c.targets, c.parent,
					len(e.fake.api()), e.lookups)
			}
		}
	}
}

func TestListParentFolderIsProvenByReadingItsChain(t *testing.T) {
	e := newEnvironment(t, []string{"organizations/123"})
	e.fake.routes["/v3/folders/7"] = okReply(`{"name":"folders/7","parent":"organizations/123"}`)
	e.fake.routes["/v3/projects"] = okReply(`{"projects":[` + projectRow("1", "alpha-project-1", "folders/7") + `]}`)
	out, err := e.invoke(projectsListID, `{"parent":"folders/7"}`)
	if err != nil {
		t.Fatal(err)
	}
	if rows, _ := listOf(t, out, "projects"); len(rows) != 1 {
		t.Errorf("rows = %v", rows)
	}
	// One read proves the parent and the rows below it need none.
	if n := len(e.fake.api()); n != 2 {
		t.Errorf("requests = %d, want the parent and the list", n)
	}
	// A folder outside the organization is refused, and the list is never read.
	e = newEnvironment(t, []string{"organizations/123"})
	e.fake.routes["/v3/folders/7"] = okReply(`{"name":"folders/7","parent":"organizations/777"}`)
	_, err = e.invoke(foldersListID, `{"parent":"folders/7"}`)
	if !isInvalidRequest(err) || strings.Contains(err.Error(), "777") {
		t.Errorf("err = %v", err)
	}
	for _, r := range e.fake.api() {
		if strings.HasSuffix(r.url, "/v3/folders") || strings.Contains(r.url, "/v3/folders?") {
			t.Errorf("the list was read: %s", r.url)
		}
	}
	// A folder target as parent needs no proof read.
	e = newEnvironment(t, []string{"folders/7"})
	e.fake.routes["/v3/folders"] = okReply(`{"folders":[]}`)
	if _, err = e.invoke(foldersListID, `{"parent":"folders/7"}`); err != nil || len(e.fake.api()) != 1 {
		t.Errorf("err = %v, requests = %d", err, len(e.fake.api()))
	}
}

func TestListDropsRowsOutsideTheTargetsAndKeepsEmptyLists(t *testing.T) {
	e := newEnvironment(t, []string{"folders/7"})
	e.fake.routes["/v3/projects"] = okReply(`{"projects":[` + projectRow("1", "alpha-project-1", "folders/7") + `,` +
		projectRow("2", "beta-project-22", "folders/8") + `,` + projectRow("3", "gamma-project-3", "organizations/999") + `]}`)
	e.fake.routes["/v3/folders/8"] = okReply(`{"name":"folders/8","parent":"organizations/999"}`)
	out, err := e.invoke(projectsListID, `{"parent":"folders/7"}`)
	if err != nil {
		t.Fatal(err)
	}
	rows, _ := listOf(t, out, "projects")
	if len(rows) != 1 || rows[0]["projectId"] != "alpha-project-1" || strings.Contains(out, "999") {
		t.Errorf("rows = %v", rows)
	}
	e = newEnvironment(t, nil)
	e.fake.routes["/v3/folders"] = okReply(`{}`)
	out, err = e.invoke(foldersListID, `{"parent":"organizations/123"}`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, `"name"`) || !strings.Contains(out, `"has_more":false`) {
		t.Errorf("out = %s", out)
	}
}

func TestListRowsWithoutAUsableNameAreInvalid(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes["/v3/folders"] = okReply(`{"folders":[{"name":"x/1"}]}`)
	if _, err := e.invoke(foldersListID, `{"parent":"organizations/123"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("err = %v", err)
	}
}

func TestListFailuresMapToTheirClass(t *testing.T) {
	for status, want := range map[int]provider.Class{403: provider.ClassPermission, 404: provider.ClassNotFound} {
		e := newEnvironment(t, nil)
		e.fake.routes["/v3/projects"] = reply{status: status, body: errorObject(status, map[int]string{403: "PERMISSION_DENIED", 404: "NOT_FOUND"}[status], "")}
		if _, err := e.invoke(projectsListID, `{"parent":"folders/7"}`); classOf(err) != want {
			t.Errorf("status %d: err = %v", status, err)
		}
	}
}
