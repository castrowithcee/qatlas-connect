package googlecloud

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

func roleRow(name string) string {
	return fmt.Sprintf(`{"name":"%s","title":"Title of %s","description":"About it","stage":"GA","etag":"BwX="}`, name, name)
}

func roleRowFull(name string, permissions ...string) string {
	quoted := make([]string, len(permissions))
	for i, p := range permissions {
		quoted[i] = `"` + p + `"`
	}
	return fmt.Sprintf(`{"name":"%s","title":"T","stage":"GA","includedPermissions":[%s]}`, name, strings.Join(quoted, ","))
}

func requireNoIO(t *testing.T, e *environment) {
	t.Helper()
	if len(e.fake.api()) != 0 || e.fake.tokenRequests() != 0 || e.lookups != 0 {
		t.Errorf("api=%d tokens=%d lookups=%d, want no I/O and no secret read", len(e.fake.api()), e.fake.tokenRequests(), e.lookups)
	}
}

func TestIAMRolesListPredefinedViewsAndThreePages(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.hook = func(_ int, req *http.Request) *reply {
		if req.URL.Path != "/v1/roles" {
			return nil
		}
		switch req.URL.Query().Get("pageToken") {
		case "":
			return &reply{status: 200, body: `{"roles":[` + roleRow("roles/a.viewer") + `],"nextPageToken":"page-two"}`}
		case "page-two":
			return &reply{status: 200, body: `{"roles":[` + roleRow("roles/b.viewer") + `],"nextPageToken":"page-three"}`}
		}
		return &reply{status: 200, body: `{"roles":[` + roleRow("roles/c.viewer") + `]}`}
	}
	var names []string
	cursor := ""
	for page := 1; page <= 3; page++ {
		arguments := `{"parent_type":"predefined","show_deleted":true,"page_size":1`
		if cursor != "" {
			arguments += `,"cursor":"` + cursor + `"`
		}
		out, err := e.invoke(iamRolesListID, arguments+`}`)
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		rows, got := listOf(t, out, "roles")
		for _, row := range rows {
			names = append(names, row["name"].(string))
			if row["title"] == nil || row["description"] != "About it" || row["stage"] != "GA" || row["etag"] != "BwX=" {
				t.Errorf("row = %v, want every documented field", row)
			}
		}
		next, _ := got["next_cursor"].(string)
		if more := got["has_more"].(bool); more != (page < 3) || strings.Contains(next, "page-") {
			t.Fatalf("page %d: has_more=%v next_cursor=%q", page, more, next)
		}
		cursor = next
	}
	if want := []string{"roles/a.viewer", "roles/b.viewer", "roles/c.viewer"}; !slices.Equal(names, want) {
		t.Errorf("names = %v, want %v", names, want)
	}
	queries := queriesOf(e)
	if len(queries) != 3 || queries[0].Get("showDeleted") != "true" || queries[0].Get("pageSize") != "1" ||
		queries[0].Get("view") != "" || queries[1].Get("pageToken") != "page-two" || queries[2].Get("pageToken") != "page-three" {
		t.Errorf("queries = %v", queries)
	}
	for _, r := range e.fake.api() {
		if r.method != http.MethodGet || !strings.HasPrefix(r.url, "https://iam.googleapis.com/v1/roles?") {
			t.Errorf("request = %s %s", r.method, r.url)
		}
	}
}

func TestIAMRolesListViewFullAndBasicAndInvalidView(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes["/v1/roles"] = okReply(`{"roles":[` + roleRowFull("roles/x.viewer", "x.things.get", "x.things.list") + `]}`)
	out, err := e.invoke(iamRolesListID, `{"parent_type":"predefined","view":"FULL"}`)
	if err != nil {
		t.Fatal(err)
	}
	rows, _ := listOf(t, out, "roles")
	if perms, _ := rows[0]["includedPermissions"].([]any); len(perms) != 2 {
		t.Errorf("row = %v, want the permissions of view FULL", rows[0])
	}
	if q := queriesOf(e)[0]; q.Get("view") != "FULL" || q.Get("pageSize") != "10" {
		t.Errorf("query = %v, want view FULL and the page of 10 Qatlas picks for FULL", q)
	}
	if _, err := e.invoke(iamRolesListID, `{"parent_type":"predefined","view":"FULL","page_size":11}`); !isInvalidRequest(err) {
		t.Errorf("page_size 11 with FULL: err = %v, want a refusal", err)
	}
	before := len(e.fake.api())
	for _, bad := range []string{`"view":"MEDIUM"`, `"view":"full"`} {
		if _, err := e.invoke(iamRolesListID, `{"parent_type":"predefined",`+bad+`}`); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
	if len(e.fake.api()) != before {
		t.Errorf("an invalid view reached the API")
	}
	e.fake.routes["/v1/roles"] = okReply(`{"roles":[` + roleRow("roles/x.viewer") + `]}`)
	if _, err := e.invoke(iamRolesListID, `{"parent_type":"predefined","view":"BASIC"}`); err != nil {
		t.Fatal(err)
	}
	api := queriesOf(e)
	if got := api[len(api)-1].Get("view"); got != "BASIC" {
		t.Errorf("view = %q, want BASIC", got)
	}
}

func TestIAMRolesListCutsLongPermissionLists(t *testing.T) {
	e := newEnvironment(t, nil)
	perms := make([]string, 1200)
	for i := range perms {
		perms[i] = fmt.Sprintf("svc.thing%d.get", i)
	}
	e.fake.routes["/v1/roles"] = okReply(`{"roles":[` + roleRowFull("roles/big.admin", perms...) + `]}`)
	out, err := e.invoke(iamRolesListID, `{"parent_type":"predefined","view":"FULL"}`)
	if err != nil {
		t.Fatal(err)
	}
	rows, _ := listOf(t, out, "roles")
	if got := len(rows[0]["includedPermissions"].([]any)); got != maxRolePermissions || rows[0]["includedPermissionsTotal"] != float64(1200) {
		t.Errorf("permissions = %d, total = %v, want the cut and the real count", got, rows[0]["includedPermissionsTotal"])
	}
}

func TestIAMRolesListTamperedCursorIsRefusedBeforeIO(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes["/v1/roles"] = okReply(`{"roles":[],"nextPageToken":"page-two"}`)
	e.fake.routes["/v1/projects/my-project-123/roles"] = okReply(`{"roles":[],"nextPageToken":"x"}`)
	e.fake.routes["/v3/projects/my-project-123"] = okReply(projectBody)
	out, err := e.invoke(iamRolesListID, `{"parent_type":"predefined"}`)
	if err != nil {
		t.Fatal(err)
	}
	_, got := listOf(t, out, "roles")
	cursor := got["next_cursor"].(string)
	n := len(e.fake.api())
	for name, arguments := range map[string]string{
		"altered":    `{"parent_type":"predefined","cursor":"` + cursor[:len(cursor)-2] + `ab"}`,
		"cut":        `{"parent_type":"predefined","cursor":"` + cursor[:len(cursor)/2] + `"}`,
		"other view": `{"parent_type":"predefined","view":"FULL","cursor":"` + cursor + `"}`,
		"other type": `{"parent_type":"project","parent_id":"my-project-123","cursor":"` + cursor + `"}`,
		"deleted":    `{"parent_type":"predefined","show_deleted":true,"cursor":"` + cursor + `"}`,
	} {
		if _, err := e.invoke(iamRolesListID, arguments); !isInvalidRequest(err) {
			t.Errorf("%s: err = %v, want a refused cursor", name, err)
		}
	}
	if len(e.fake.api()) != n {
		t.Errorf("a refused cursor reached the API")
	}
	// A cursor of another tool or another connection binding is not accepted either.
	if _, err := e.invoke(iamRolesQueryID, `{"resource_type":"organization","resource_id":"9001","cursor":"`+cursor+`"}`); err == nil {
		t.Error("a cursor of another tool was accepted")
	}
}

func TestIAMRolesListProjectRolesAreBoundToTargets(t *testing.T) {
	t.Run("outside the targets refused before I/O", func(t *testing.T) {
		e := newEnvironment(t, []string{"projects/allowed-project"})
		_, err := e.invoke(iamRolesListID, `{"parent_type":"project","parent_id":"other-project"}`)
		if err == nil || strings.Contains(err.Error(), "other-project") || strings.Contains(err.Error(), "allowed-project") {
			t.Fatalf("err = %v, want a refusal without names", err)
		}
		requireNoIO(t, e)
	})
	t.Run("proved along the parent chain", func(t *testing.T) {
		e := newEnvironment(t, []string{"organizations/9001"})
		e.fake.routes["/v3/projects/415104041262"] = okReply(projectBody)
		e.fake.routes["/v3/folders/2001"] = okReply(`{"name":"folders/2001","parent":"organizations/9001"}`)
		e.fake.routes["/v1/projects/my-project-123/roles"] = okReply(`{"roles":[` + roleRow("projects/my-project-123/roles/custom") + `]}`)
		out, err := e.invoke(iamRolesListID, `{"parent_type":"project","parent_id":"415104041262"}`)
		if err != nil {
			t.Fatal(err)
		}
		rows, _ := listOf(t, out, "roles")
		if len(rows) != 1 || rows[0]["name"] != "projects/my-project-123/roles/custom" {
			t.Errorf("rows = %v", rows)
		}
		var paths []string
		for _, r := range e.fake.api() {
			paths = append(paths, strings.SplitN(strings.TrimPrefix(r.url, "https://"), "?", 2)[0])
		}
		want := []string{"cloudresourcemanager.googleapis.com/v3/projects/415104041262",
			"cloudresourcemanager.googleapis.com/v3/folders/2001", "iam.googleapis.com/v1/projects/my-project-123/roles"}
		if !slices.Equal(paths, want) {
			t.Errorf("requests = %v, want the proof reads before the roles", paths)
		}
	})
	t.Run("a project outside by the proof is refused before the roles", func(t *testing.T) {
		e := newEnvironment(t, []string{"organizations/9999"})
		e.fake.routes["/v3/projects/my-project-123"] = okReply(projectBody)
		e.fake.routes["/v3/folders/2001"] = okReply(`{"name":"folders/2001","parent":"organizations/9001"}`)
		if _, err := e.invoke(iamRolesListID, `{"parent_type":"project","parent_id":"my-project-123"}`); !isInvalidRequest(err) {
			t.Fatalf("err = %v, want a refusal", err)
		}
		for _, r := range e.fake.api() {
			if strings.Contains(r.url, "iam.googleapis.com") {
				t.Errorf("the roles were read: %s", r.url)
			}
		}
	})
	t.Run("a row of another project is an invalid response", func(t *testing.T) {
		e := newEnvironment(t, nil)
		e.fake.routes["/v3/projects/my-project-123"] = okReply(projectBody)
		e.fake.routes["/v1/projects/my-project-123/roles"] = okReply(`{"roles":[` + roleRow("projects/other-project-9/roles/custom") + `]}`)
		_, err := e.invoke(iamRolesListID, `{"parent_type":"project","parent_id":"my-project-123"}`)
		if classOf(err) != provider.ClassInvalidResponse {
			t.Errorf("err = %v, want an invalid response", err)
		}
	})
}

func TestIAMOrganizationRolesNeedTheExactOrganizationTarget(t *testing.T) {
	calls := []struct{ tool, arguments string }{
		{iamRolesListID, `{"parent_type":"organization","parent_id":"9001"}`},
		{iamRolesGetID, `{"role":"organizations/9001/roles/custom"}`},
		{iamRolesQueryID, `{"resource_type":"organization","resource_id":"9001"}`},
		{iamPermissionsQueryID, `{"resource_type":"organization","resource_id":"9001"}`},
	}
	for _, targets := range [][]string{nil, {"organizations/9999"}, {"folders/2001"}, {"projects/my-project-123"}} {
		for _, call := range calls {
			t.Run(fmt.Sprint(call.tool, targets), func(t *testing.T) {
				e := newEnvironment(t, targets)
				_, err := e.invoke(call.tool, call.arguments)
				if !isInvalidRequest(err) || strings.Contains(err.Error(), "9999") || strings.Contains(err.Error(), "2001") {
					t.Fatalf("err = %v, want a refusal that names no target", err)
				}
				requireNoIO(t, e)
			})
		}
	}
	e := newEnvironment(t, []string{"organizations/9001"})
	e.fake.routes["/v1/organizations/9001/roles"] = okReply(`{"roles":[` + roleRow("organizations/9001/roles/custom") + `]}`)
	e.fake.routes["/v1/organizations/9001/roles/custom"] = okReply(roleRowFull("organizations/9001/roles/custom", "a.b.c"))
	if _, err := e.invoke(iamRolesListID, calls[0].arguments); err != nil {
		t.Fatal(err)
	}
	out, err := e.invoke(iamRolesGetID, calls[1].arguments)
	if err != nil || !strings.Contains(out, "a.b.c") {
		t.Fatalf("out=%s err=%v", out, err)
	}
	for _, r := range e.fake.api() {
		if strings.Contains(r.url, "cloudresourcemanager") {
			t.Errorf("an organization needs no proof read, got %s", r.url)
		}
	}
}

func TestIAMRolesGet(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes["/v1/roles/compute.viewer"] = okReply(roleRowFull("roles/compute.viewer", "compute.disks.get") +
		"")
	out, err := e.invoke(iamRolesGetID, `{"role":"roles/compute.viewer"}`)
	if err != nil || !strings.Contains(out, "compute.disks.get") {
		t.Fatalf("out=%s err=%v", out, err)
	}
	// The answer must name the role that was asked.
	e.fake.routes["/v1/roles/compute.admin"] = okReply(roleRow("roles/other.admin"))
	if _, err := e.invoke(iamRolesGetID, `{"role":"roles/compute.admin"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("err = %v, want an invalid response", err)
	}
	for _, bad := range []string{`"roles/*"`, `"roles/../x"`, `"projects/my-project-123/roles/"`, `"folders/1/roles/x"`,
		`"roles/a b"`, `"https://iam.googleapis.com/v1/roles/x"`} {
		if _, err := e.invoke(iamRolesGetID, `{"role":`+bad+`}`); err == nil {
			t.Errorf("role %s accepted", bad)
		}
	}
	t.Run("project role uses the project ID", func(t *testing.T) {
		e := newEnvironment(t, []string{"projects/my-project-123"})
		e.fake.routes["/v3/projects/415104041262"] = okReply(projectBody)
		e.fake.routes["/v1/projects/my-project-123/roles/custom"] = okReply(roleRowFull("projects/my-project-123/roles/custom", "a.b.c"))
		if _, err := e.invoke(iamRolesGetID, `{"role":"projects/415104041262/roles/custom"}`); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("project role outside the targets", func(t *testing.T) {
		e := newEnvironment(t, []string{"projects/allowed-project"})
		if _, err := e.invoke(iamRolesGetID, `{"role":"projects/other-project/roles/custom"}`); !isInvalidRequest(err) {
			t.Fatalf("err = %v", err)
		}
		requireNoIO(t, e)
	})
}

func TestIAMPredefinedRolesAreNotBoundToTargets(t *testing.T) {
	e := newEnvironment(t, []string{"projects/my-project-123"})
	e.fake.routes["/v1/roles"] = okReply(`{"roles":[` + roleRow("roles/viewer") + `]}`)
	if _, err := e.invoke(iamRolesListID, `{"parent_type":"predefined"}`); err != nil {
		t.Fatal(err)
	}
	if got := len(e.fake.api()); got != 1 {
		t.Errorf("requests = %d, want the one list and no proof read", got)
	}
	if _, err := e.invoke(iamRolesListID, `{"parent_type":"predefined","parent_id":"my-project-123"}`); !isInvalidRequest(err) {
		t.Errorf("a predefined list with a parent_id: err = %v", err)
	}
}

func TestIAMToolsReachNothingOnBillingOnlyConnections(t *testing.T) {
	e := newEnvironment(t, []string{"billingAccounts/012345-ABCDEF-678901"})
	for _, call := range []struct{ tool, arguments string }{
		{iamRolesListID, `{"parent_type":"predefined"}`},
		{iamRolesGetID, `{"role":"roles/viewer"}`},
		{iamRolesQueryID, `{"resource_type":"project","resource_id":"my-project-123"}`},
		{iamPermissionsQueryID, `{"resource_type":"project","resource_id":"my-project-123"}`},
	} {
		if _, err := e.invoke(call.tool, call.arguments); err == nil {
			t.Errorf("%s ran on a billing-only connection", call.tool)
		}
		for _, targets := range [][]string{{"billingAccounts/012345-ABCDEF-678901"}} {
			if reaches, _ := targetReaches(call.tool, targets); reaches {
				t.Errorf("%s is offered to a billing-only binding", call.tool)
			}
		}
		for _, targets := range [][]string{nil, {"projects/my-project-123"}, {"folders/1"}, {"organizations/1"}} {
			if reaches, _ := targetReaches(call.tool, targets); !reaches {
				t.Errorf("%s is hidden from %v", call.tool, targets)
			}
		}
	}
	requireNoIO(t, e)
}

func TestIAMQueryGrantableSendsAFixedPostBodyAndFollowsTwoPages(t *testing.T) {
	e := newEnvironment(t, []string{"organizations/9001"})
	e.fake.routes["/v3/projects/my-project-123"] = okReply(`{"name":"projects/1","projectId":"my-project-123","parent":"organizations/9001"}`)
	e.fake.hook = func(_ int, req *http.Request) *reply {
		if req.URL.Path != "/v1/roles:queryGrantableRoles" {
			return nil
		}
		return &reply{status: 200, body: `{"roles":[` + roleRow("roles/viewer") + `,` +
			roleRow("organizations/9001/roles/named") + `,` + roleRow("organizations/7777/roles/foreign") + `,` +
			roleRow("projects/my-project-123/roles/mine") + `],"nextPageToken":"more"}`}
	}
	out, err := e.invoke(iamRolesQueryID, `{"resource_type":"project","resource_id":"my-project-123","page_size":4}`)
	if err != nil {
		t.Fatal(err)
	}
	rows, got := listOf(t, out, "roles")
	if len(rows) != 4 || rows[1]["title"] == nil {
		t.Fatalf("rows = %v", rows)
	}
	if foreign := rows[2]; len(foreign) != 1 || foreign["name"] != "organizations/7777/roles/foreign" {
		t.Errorf("foreign organization role = %v, want its name only", foreign)
	}
	e.fake.hook = func(_ int, req *http.Request) *reply {
		if req.URL.Path != "/v1/roles:queryGrantableRoles" {
			return nil
		}
		return &reply{status: 200, body: `{"roles":[` + roleRow("roles/editor") + `]}`}
	}
	cursor := got["next_cursor"].(string)
	out, err = e.invoke(iamRolesQueryID, `{"resource_type":"project","resource_id":"my-project-123","cursor":"`+cursor+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, got := listOf(t, out, "roles"); got["has_more"] != false || got["next_cursor"] != nil {
		t.Errorf("second page = %v", got)
	}
	var posts []seen
	for _, r := range e.fake.api() {
		if strings.HasPrefix(r.url, "https://iam.googleapis.com/") {
			posts = append(posts, r)
		}
	}
	if len(posts) != 2 {
		t.Fatalf("IAM requests = %d", len(posts))
	}
	for i, r := range posts {
		var body map[string]any
		if err := json.Unmarshal([]byte(r.body), &body); err != nil {
			t.Fatal(err)
		}
		if r.method != http.MethodPost || r.url != "https://iam.googleapis.com/v1/roles:queryGrantableRoles" ||
			r.header.Get("Content-Type") != "application/json" || body["fullResourceName"] !=
			"//cloudresourcemanager.googleapis.com/projects/my-project-123" {
			t.Errorf("request %d = %s %s %v", i, r.method, r.url, body)
		}
		if _, hasView := body["view"]; hasView || len(body) != 2+i {
			t.Errorf("body %d = %v, want only the checked members", i, body)
		}
	}
	var second map[string]any
	_ = json.Unmarshal([]byte(posts[1].body), &second)
	if second["pageToken"] != "more" {
		t.Errorf("second body = %v, want Google's token, not the cursor", second)
	}
}

func TestIAMQueryRejectsRowsOfOtherScopes(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes["/v3/folders/2001"] = okReply(`{"name":"folders/2001","parent":"organizations/9001"}`)
	e.fake.routes["/v1/roles:queryGrantableRoles"] = okReply(`{"roles":[` + roleRow("projects/my-project-123/roles/x") + `]}`)
	_, err := e.invoke(iamRolesQueryID, `{"resource_type":"folder","resource_id":"2001"}`)
	if classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("err = %v, want an invalid response for a project role on a folder", err)
	}
	e.fake.routes["/v1/roles:queryGrantableRoles"] = okReply(`{"roles":[{"title":"no name"}]}`)
	if _, err := e.invoke(iamRolesQueryID, `{"resource_type":"folder","resource_id":"2001"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("err = %v, want an invalid response for a row without a name", err)
	}
}

func TestIAMQueryResourceIsStructured(t *testing.T) {
	e := newEnvironment(t, nil)
	for _, arguments := range []string{
		`{"resource_type":"bucket","resource_id":"x"}`,
		`{"resource_type":"project","resource_id":"//cloudresourcemanager.googleapis.com/projects/p"}`,
		`{"resource_type":"folder","resource_id":"my-folder"}`,
		`{"resource_type":"organization","resource_id":"9001/../x"}`,
		`{"resource_type":"project"}`,
		`{"resource_id":"my-project-123"}`,
		`{"full_resource_name":"//cloudresourcemanager.googleapis.com/projects/p","resource_type":"project","resource_id":"my-project-123"}`,
	} {
		for _, tool := range []string{iamRolesQueryID, iamPermissionsQueryID} {
			if _, err := e.invoke(tool, arguments); err == nil {
				t.Errorf("%s accepted %s", tool, arguments)
			}
		}
	}
	requireNoIO(t, e)
}

func TestIAMQueryTestablePermissions(t *testing.T) {
	e := newEnvironment(t, []string{"folders/2001"})
	e.fake.routes["/v3/folders/2001"] = okReply(`{"name":"folders/2001","parent":"organizations/9001"}`)
	e.fake.routes["/v1/permissions:queryTestablePermissions"] = okReply(`{"permissions":[` +
		`{"name":"a.b.get","title":"T","description":"D","stage":"GA","customRolesSupportLevel":"SUPPORTED","apiDisabled":true,"primaryPermission":"a.b.read"},` +
		`{"name":"a.b.list"}],"nextPageToken":"more"}`)
	out, err := e.invoke(iamPermissionsQueryID, `{"resource_type":"folder","resource_id":"2001","page_size":2}`)
	if err != nil {
		t.Fatal(err)
	}
	rows, got := listOf(t, out, "permissions")
	if len(rows) != 2 || rows[0]["customRolesSupportLevel"] != "SUPPORTED" || rows[0]["apiDisabled"] != true ||
		rows[0]["primaryPermission"] != "a.b.read" || rows[0]["stage"] != "GA" || rows[0]["title"] != "T" ||
		rows[0]["description"] != "D" || got["has_more"] != true {
		t.Errorf("rows = %v", rows)
	}
	api := e.fake.api()
	var body map[string]any
	_ = json.Unmarshal([]byte(api[len(api)-1].body), &body)
	if body["fullResourceName"] != "//cloudresourcemanager.googleapis.com/folders/2001" || body["pageSize"] != float64(2) {
		t.Errorf("body = %v", body)
	}
	if _, err := e.invoke(iamPermissionsQueryID, `{"resource_type":"folder","resource_id":"2001","page_size":501}`); err == nil {
		t.Error("page_size 501 accepted")
	}
	// A project target cannot hold a folder.
	e2 := newEnvironment(t, []string{"projects/my-project-123"})
	if _, err := e2.invoke(iamPermissionsQueryID, `{"resource_type":"folder","resource_id":"2001"}`); !isInvalidRequest(err) {
		t.Errorf("err = %v, want a refusal", err)
	}
	requireNoIO(t, e2)
}

func TestIAMQueryUnauthorizedRenewsOnceWithTheBodyIntact(t *testing.T) {
	e := newEnvironment(t, []string{"organizations/9001"})
	e.fake.hook = func(n int, req *http.Request) *reply {
		if n == 1 {
			return &reply{status: 401, body: errorObject(401, "UNAUTHENTICATED", "ACCESS_TOKEN_EXPIRED")}
		}
		return &reply{status: 200, body: `{"permissions":[{"name":"a.b.get"}]}`}
	}
	if _, err := e.invoke(iamPermissionsQueryID, `{"resource_type":"organization","resource_id":"9001"}`); err != nil {
		t.Fatal(err)
	}
	api := e.fake.api()
	if len(api) != 2 || api[0].body == "" || api[0].body != api[1].body || e.fake.tokenRequests() != 2 {
		t.Errorf("requests = %+v, tokens = %d, want one repeat with the same body and a new token", api, e.fake.tokenRequests())
	}
}

func TestIAMFailures(t *testing.T) {
	cases := []struct {
		name  string
		reply reply
		want  provider.Class
	}{
		{"forbidden", reply{status: 403, body: errorObject(403, "PERMISSION_DENIED", "")}, provider.ClassPermission},
		{"api disabled", reply{status: 403, body: errorObject(403, "PERMISSION_DENIED", "SERVICE_DISABLED")}, provider.ClassPermission},
		{"not found", reply{status: 404, body: errorObject(404, "NOT_FOUND", "")}, provider.ClassNotFound},
	}
	for _, c := range cases {
		for _, call := range []struct{ tool, arguments string }{
			{iamRolesListID, `{"parent_type":"predefined"}`},
			{iamRolesGetID, `{"role":"roles/viewer"}`},
			{iamRolesQueryID, `{"resource_type":"organization","resource_id":"9001"}`},
			{iamPermissionsQueryID, `{"resource_type":"organization","resource_id":"9001"}`},
		} {
			t.Run(c.name+" "+call.tool, func(t *testing.T) {
				e := newEnvironment(t, []string{"organizations/9001"})
				answer := c.reply
				e.fake.hook = func(int, *http.Request) *reply { return &answer }
				_, err := e.invoke(call.tool, call.arguments)
				if classOf(err) != c.want {
					t.Fatalf("err = %v, want %s", err, c.want)
				}
				if strings.Contains(err.Error(), detailCanary) {
					t.Errorf("error %q carries provider text", err)
				}
			})
		}
	}
}

func TestIAMRolesBoundProviderText(t *testing.T) {
	e := newEnvironment(t, nil)
	long := strings.Repeat("x", 5000)
	e.fake.routes["/v1/roles"] = okReply(`{"roles":[{"name":"roles/viewer","title":"` + long + `","description":"` + long + `\u0007"}]}`)
	out, err := e.invoke(iamRolesListID, `{"parent_type":"predefined"}`)
	if err != nil {
		t.Fatal(err)
	}
	rows, _ := listOf(t, out, "roles")
	if len(rows[0]["title"].(string)) > maxNameText || len(rows[0]["description"].(string)) > maxText ||
		strings.ContainsRune(rows[0]["description"].(string), 7) {
		t.Errorf("row = %v, want bounded text", rows[0])
	}
}

func TestIAMDescriptorsCoverTheDiscoveryFields(t *testing.T) {
	// Members of Role and Permission in the discovery document of iam v1 (revision 20261106); the
	// onlyInPredefinedRoles member of Permission is marked deprecated and left out.
	check := func(d capability.Descriptor, list string, item []string) {
		var schema struct {
			Properties map[string]struct {
				Items struct {
					Properties map[string]json.RawMessage `json:"properties"`
				} `json:"items"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(d.OutputSchema, &schema); err != nil {
			t.Fatal(err)
		}
		for _, field := range item {
			if _, found := schema.Properties[list].Items.Properties[field]; !found {
				t.Errorf("%s output schema lacks %s", d.ID, field)
			}
		}
		for _, f := range []string{list, "next_cursor", "has_more"} {
			if !slices.ContainsFunc(d.Fields, func(x capability.Field) bool { return x.Name == f }) {
				t.Errorf("%s fields lack %s", d.ID, f)
			}
		}
	}
	role := []string{"name", "title", "description", "includedPermissions", "stage", "etag", "deleted"}
	check(iamRolesList, "roles", role)
	check(iamRolesQuery, "roles", role)
	check(iamPermissionsQuery, "permissions", []string{"name", "title", "description", "stage",
		"customRolesSupportLevel", "apiDisabled", "primaryPermission"})
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(iamRolesGet.OutputSchema, &schema); err != nil {
		t.Fatal(err)
	}
	for _, field := range role {
		if _, found := schema.Properties[field]; !found {
			t.Errorf("iam-roles.get output schema lacks %s", field)
		}
	}
	// Every documented parameter is an argument: showDeleted, view, pageSize, pageToken (as cursor), parent.
	for _, want := range []string{"parent_type", "parent_id", "view", "show_deleted", "page_size", "cursor"} {
		if !slices.ContainsFunc(iamRolesList.Arguments, func(a capability.Argument) bool { return a.Name == want }) {
			t.Errorf("iam-roles.list lacks the argument %s", want)
		}
	}
	for _, d := range []capability.Descriptor{iamRolesList, iamRolesGet, iamRolesQuery, iamPermissionsQuery} {
		if d.Risk != readRisk {
			t.Errorf("%s risk = %+v", d.ID, d.Risk)
		}
	}
}
