package googlecloud

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const conditionalPolicy = `{"version":3,"etag":"BwXyz=","bindings":[` +
	`{"role":"roles/viewer","members":["user:ada@example.com","group:ops@example.com","allUsers"]},` +
	`{"role":"roles/storage.objectViewer","members":["serviceAccount:ci@example-project.iam.gserviceaccount.com"],` +
	`"condition":{"expression":"request.time < timestamp(\"2027-01-01T00:00:00Z\")","title":"temporary","description":"until 2027"}}],` +
	`"auditConfigs":[{"service":"allServices","auditLogConfigs":[{"logType":"DATA_READ","exemptedMembers":["user:bot@example.com"]}]}]}`

func decode(t *testing.T, out string) map[string]any {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func noIO(t *testing.T, e *environment, what string) {
	t.Helper()
	if len(e.fake.api()) != 0 || e.fake.tokenRequests() != 0 || e.lookups != 0 {
		t.Errorf("%s: api=%d tokens=%d lookups=%d, want a refusal before I/O and secret", what,
			len(e.fake.api()), e.fake.tokenRequests(), e.lookups)
	}
}

func TestIAMPolicyIsAReadPostWithVersionThreeAndPassesMembersThrough(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes["/v3/projects/my-project-123:getIamPolicy"] = okReply(conditionalPolicy)
	out, err := e.invoke(iamPoliciesGetID, `{"resource_type":"project","resource_id":"my-project-123"}`)
	if err != nil {
		t.Fatal(err)
	}
	api := e.fake.api()
	if len(api) != 1 || api[0].method != http.MethodPost || api[0].body != `{"options":{"requestedPolicyVersion":3}}` ||
		api[0].header.Get("Content-Type") != "application/json" || !strings.HasPrefix(api[0].auth, "Bearer ") {
		t.Fatalf("requests = %+v", api)
	}
	got := decode(t, out)
	bindings := got["bindings"].([]any)
	if got["resource"] != "projects/my-project-123" || got["version"] != float64(3) || got["etag"] != "BwXyz=" || len(bindings) != 2 {
		t.Fatalf("result = %s", out)
	}
	first := bindings[0].(map[string]any)["members"].([]any)
	if len(first) != 3 || first[0] != "user:ada@example.com" || first[2] != "allUsers" {
		t.Errorf("members = %v, want them unchanged", first)
	}
	condition := bindings[1].(map[string]any)["condition"].(map[string]any)
	if condition["title"] != "temporary" || !strings.Contains(condition["expression"].(string), "timestamp") {
		t.Errorf("condition = %v", condition)
	}
	if audit := got["auditConfigs"].([]any); len(audit) != 1 || !strings.Contains(out, "bot@example.com") {
		t.Errorf("auditConfigs = %v", audit)
	}
	if _, cut := got["truncated"]; cut {
		t.Errorf("truncated set for a small policy")
	}
}

func TestIAMPolicyWithoutBindingsAndOtherVersions(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes["/v3/folders/2001:getIamPolicy"] = okReply(`{"version":1,"etag":"ACAB"}`)
	out, err := e.invoke(iamPoliciesGetID, `{"resource_type":"folder","resource_id":"2001","requested_policy_version":1}`)
	if err != nil {
		t.Fatal(err)
	}
	got := decode(t, out)
	if got["bindings"] != nil || got["etag"] != "ACAB" {
		t.Errorf("result = %s, want a policy without bindings", out)
	}
	if body := e.fake.api()[0].body; body != `{"options":{"requestedPolicyVersion":1}}` {
		t.Errorf("body = %s", body)
	}
	for _, version := range []string{"2", "4", "-1"} {
		e := newEnvironment(t, nil)
		if _, err := e.invoke(iamPoliciesGetID, `{"resource_type":"folder","resource_id":"2001","requested_policy_version":`+version+`}`); err == nil {
			t.Errorf("version %s was accepted", version)
		}
		noIO(t, e, "version "+version)
	}
}

func TestIAMPolicyRenewsTheTokenOnceAndResendsTheBody(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.hook = func(n int, _ *http.Request) *reply {
		if n == 1 {
			return &reply{status: 401, body: errorObject(401, "UNAUTHENTICATED", "ACCESS_TOKEN_EXPIRED")}
		}
		r := okReply(`{"version":1}`)
		return &r
	}
	if _, err := e.invoke(iamPoliciesGetID, `{"resource_type":"organization","resource_id":"9001"}`); err == nil {
		// An unbound connection never reaches an organization.
		t.Fatal("an unbound connection read an organization")
	}
	e = newEnvironment(t, []string{"organizations/9001"})
	e.fake.hook = func(n int, _ *http.Request) *reply {
		if n == 1 {
			return &reply{status: 401, body: errorObject(401, "UNAUTHENTICATED", "ACCESS_TOKEN_EXPIRED")}
		}
		r := okReply(`{"version":1}`)
		return &r
	}
	if _, err := e.invoke(iamPoliciesGetID, `{"resource_type":"organization","resource_id":"9001"}`); err != nil {
		t.Fatal(err)
	}
	api := e.fake.api()
	if len(api) != 2 || e.fake.tokenRequests() != 2 || api[0].body == "" || api[0].body != api[1].body ||
		api[0].auth == api[1].auth || api[1].url != "https://cloudresourcemanager.googleapis.com/v3/organizations/9001:getIamPolicy" {
		t.Errorf("requests = %+v, tokens = %d", api, e.fake.tokenRequests())
	}
}

const saEmail = "deploy@example-project.iam.gserviceaccount.com"

func saBody(project, email, uid string) string {
	return fmt.Sprintf(`{"name":"projects/%s/serviceAccounts/%s","projectId":"%s","email":"%s","uniqueId":"%s"}`,
		project, email, project, email, uid)
}

func TestIAMServiceAccountIsBoundByReadingItAcrossProjects(t *testing.T) {
	e := newEnvironment(t, []string{"projects/my-project-123"})
	e.fake.routes["/v1/projects/-/serviceAccounts/"+saEmail] = okReply(saBody("my-project-123", saEmail, "108123456789012345678"))
	e.fake.routes["/v1/projects/my-project-123/serviceAccounts/"+saEmail+":getIamPolicy"] = okReply(`{"version":1,"etag":"ACAB"}`)
	out, err := e.invoke(iamPoliciesGetID, `{"resource_type":"service-account","resource_id":"`+saEmail+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	api := e.fake.api()
	if len(api) != 2 || api[0].method != http.MethodGet || api[0].url != "https://iam.googleapis.com/v1/projects/-/serviceAccounts/"+saEmail ||
		api[1].method != http.MethodPost || api[1].body != "" ||
		api[1].url != "https://iam.googleapis.com/v1/projects/my-project-123/serviceAccounts/"+saEmail+":getIamPolicy?options.requestedPolicyVersion=3" {
		t.Fatalf("requests = %+v", api)
	}
	if got := decode(t, out); got["resource"] != "projects/my-project-123/serviceAccounts/"+saEmail {
		t.Errorf("result = %s", out)
	}

	// The unique ID names the account as well, and a project known only by number is read once more.
	e = newEnvironment(t, []string{"folders/2001"})
	e.fake.routes["/v1/projects/-/serviceAccounts/108123456789012345678"] = okReply(saBody("my-project-123", saEmail, "108123456789012345678"))
	e.fake.routes["/v3/projects/my-project-123"] = okReply(projectBody)
	e.fake.routes["/v1/projects/my-project-123/serviceAccounts/108123456789012345678:testIamPermissions"] =
		okReply(`{"permissions":["iam.serviceAccounts.get"]}`)
	if _, err := e.invoke(iamPermissionsTestID, `{"resource_type":"service-account",`+
		`"resource_id":"108123456789012345678","permissions":["iam.serviceAccounts.get"]}`); err != nil {
		t.Fatal(err)
	}
}

func TestIAMServiceAccountOfAnotherProjectIsRefusedBeforeThePolicy(t *testing.T) {
	e := newEnvironment(t, []string{"projects/my-project-123"})
	e.fake.routes["/v1/projects/-/serviceAccounts/"+saEmail] = okReply(saBody("other-project-9", saEmail, "1"))
	for tool, args := range map[string]string{
		iamPoliciesGetID:     `{"resource_type":"service-account","resource_id":"` + saEmail + `"}`,
		iamPermissionsTestID: `{"resource_type":"service-account","resource_id":"` + saEmail + `","permissions":["iam.serviceAccounts.get"]}`,
	} {
		if _, err := e.invoke(tool, args); !isInvalidRequest(err) {
			t.Errorf("%s: err = %v, want a refusal", tool, err)
		}
	}
	for _, r := range e.fake.api() {
		if r.method == http.MethodPost {
			t.Errorf("a policy request left for a foreign account: %s", r.url)
		}
	}
}

func TestIAMServiceAccountAnswerForAnotherAccountIsRefused(t *testing.T) {
	for name, body := range map[string]string{
		"other email":     saBody("my-project-123", "other@example-project.iam.gserviceaccount.com", "1"),
		"no project":      `{"name":"projects//serviceAccounts/` + saEmail + `","email":"` + saEmail + `"}`,
		"placeholder":     saBody("-", saEmail, "1"),
		"name of another": `{"name":"projects/other-project-9/serviceAccounts/x","projectId":"my-project-123","email":"` + saEmail + `"}`,
	} {
		e := newEnvironment(t, []string{"projects/my-project-123"})
		e.fake.routes["/v1/projects/-/serviceAccounts/"+saEmail] = okReply(body)
		_, err := e.invoke(iamPoliciesGetID, `{"resource_type":"service-account","resource_id":"`+saEmail+`"}`)
		if classOf(err) != provider.ClassInvalidResponse {
			t.Errorf("%s: err = %v", name, err)
		}
		for _, r := range e.fake.api() {
			if r.method == http.MethodPost {
				t.Errorf("%s: policy requested", name)
			}
		}
	}
}

func TestIAMServiceAccountWithoutTargetsNeedsNoProofReading(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes["/v1/projects/-/serviceAccounts/"+saEmail+":getIamPolicy"] = okReply(`{"version":1}`)
	out, err := e.invoke(iamPoliciesGetID, `{"resource_type":"service-account","resource_id":"`+saEmail+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	if api := e.fake.api(); len(api) != 1 || api[0].method != http.MethodPost {
		t.Errorf("requests = %+v, want the policy alone", api)
	}
	if got := decode(t, out); got["resource"] != "projects/-/serviceAccounts/"+saEmail {
		t.Errorf("result = %s", out)
	}
}

func TestIAMServiceAccountArgumentsAreChecked(t *testing.T) {
	for name, c := range map[string]struct{ targets, args string }{
		"project argument":               {"", `{"resource_type":"service-account","project_id":"my-project-123","resource_id":"` + saEmail + `"}`},
		"path as account":                {"", `{"resource_type":"service-account","resource_id":"a/b"}`},
		"billing only":                   {"billingAccounts/012345-ABCDEF-678901", `{"resource_type":"service-account","resource_id":"` + saEmail + `"}`},
		"project argument for a project": {"", `{"resource_type":"project","project_id":"my-project-123","resource_id":"my-project-123"}`},
	} {
		var targets []string
		if c.targets != "" {
			targets = []string{c.targets}
		}
		e := newEnvironment(t, targets)
		if _, err := e.invoke(iamPoliciesGetID, c.args); err == nil {
			t.Errorf("%s was accepted", name)
		}
		noIO(t, e, name)
	}
}

func TestIAMToolsRefuseResourcesOutsideTheTargetsBeforeIO(t *testing.T) {
	tools := map[string]string{
		iamPoliciesGetID:     `{"resource_type":"%s","resource_id":"%s"}`,
		iamPermissionsTestID: `{"resource_type":"%s","resource_id":"%s","permissions":["resourcemanager.projects.get"]}`,
		denyPoliciesListID:   `{"resource_type":"%s","resource_id":"%s"}`,
		denyPoliciesGetID:    `{"resource_type":"%s","resource_id":"%s","policy_id":"block"}`,
	}
	for tool, shape := range tools {
		for name, c := range map[string]struct {
			targets  []string
			kind, id string
		}{
			"other project":          {[]string{"projects/other-project-9"}, "project", "my-project-123"},
			"project for the folder": {[]string{"projects/other-project-9"}, "folder", "2001"},
			"project for the org":    {[]string{"projects/other-project-9"}, "organization", "9001"},
			"unbound for the org":    {nil, "organization", "9001"},
			"folder for the org":     {[]string{"folders/2001"}, "organization", "9001"},
			"other org for the org":  {[]string{"organizations/9002"}, "organization", "9001"},
			"billing account only":   {[]string{"billingAccounts/012345-ABCDEF-678901"}, "project", "my-project-123"},
			"project ID as folder":   {nil, "folder", "my-project-123"},
			"name instead of an ID":  {nil, "project", "projects/my-project-123"},
			"path instead of an ID":  {nil, "project", "my-project-123/../x"},
		} {
			e := newEnvironment(t, c.targets)
			if _, err := e.invoke(tool, fmt.Sprintf(shape, c.kind, c.id)); err == nil {
				t.Errorf("%s: %s ran", tool, name)
			}
			noIO(t, e, tool+" "+name)
		}
	}
}

func TestIAMFolderIsBoundThroughItsParentChain(t *testing.T) {
	e := newEnvironment(t, []string{"organizations/9001"})
	e.fake.routes["/v3/folders/2002"] = okReply(`{"name":"folders/2002","parent":"folders/2001"}`)
	e.fake.routes["/v3/folders/2001"] = okReply(`{"name":"folders/2001","parent":"organizations/9001"}`)
	e.fake.routes["/v3/folders/2002:getIamPolicy"] = okReply(`{"version":1}`)
	if _, err := e.invoke(iamPoliciesGetID, `{"resource_type":"folder","resource_id":"2002"}`); err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, r := range e.fake.api() {
		paths = append(paths, r.method+" "+strings.TrimPrefix(r.url, "https://cloudresourcemanager.googleapis.com"))
	}
	if got := strings.Join(paths, ","); !strings.HasPrefix(got, "GET /v3/folders/2002,GET /v3/folders/2001,POST /v3/folders/2002:getIamPolicy") {
		t.Errorf("requests = %s, want the chain read before the policy", got)
	}

	e = newEnvironment(t, []string{"organizations/9002"})
	e.fake.routes["/v3/folders/2002"] = okReply(`{"name":"folders/2002","parent":"organizations/9001"}`)
	e.fake.routes["/v3/folders/2002:getIamPolicy"] = okReply(`{"version":1}`)
	if _, err := e.invoke(iamPoliciesGetID, `{"resource_type":"folder","resource_id":"2002"}`); !isInvalidRequest(err) {
		t.Fatalf("err = %v, want a refusal", err)
	}
	for _, r := range e.fake.api() {
		if r.method == http.MethodPost {
			t.Errorf("the policy was read although the folder lies outside: %s", r.url)
		}
	}
}

func TestIAMProjectBelowAFolderTargetIsBoundByReadingIt(t *testing.T) {
	e := newEnvironment(t, []string{"folders/2001"})
	e.fake.routes["/v3/projects/my-project-123"] = okReply(projectBody)
	e.fake.routes["/v3/projects/my-project-123:getIamPolicy"] = okReply(`{"version":1}`)
	if _, err := e.invoke(iamPoliciesGetID, `{"resource_type":"project","resource_id":"my-project-123"}`); err != nil {
		t.Fatal(err)
	}
	if api := e.fake.api(); len(api) != 2 || api[0].method != http.MethodGet || api[1].method != http.MethodPost {
		t.Errorf("requests = %+v, want the project read before the policy", api)
	}
}

func TestIAMOrganizationRunsOnlyWithTheOrganizationTarget(t *testing.T) {
	e := newEnvironment(t, []string{"organizations/9001"})
	e.fake.routes["/v3/organizations/9001:getIamPolicy"] = okReply(`{"version":1}`)
	e.fake.routes["/v3/organizations/9001:testIamPermissions"] = okReply(`{"permissions":["resourcemanager.organizations.get"]}`)
	e.fake.routes["/v2/policies/cloudresourcemanager.googleapis.com/organizations/9001/denypolicies"] = okReply(`{}`)
	for tool, args := range map[string]string{
		iamPoliciesGetID:     `{"resource_type":"organization","resource_id":"9001"}`,
		iamPermissionsTestID: `{"resource_type":"organization","resource_id":"9001","permissions":["resourcemanager.organizations.get"]}`,
		denyPoliciesListID:   `{"resource_type":"organization","resource_id":"9001"}`,
	} {
		if _, err := e.invoke(tool, args); err != nil {
			t.Errorf("%s: %v", tool, err)
		}
	}
}

func TestIAMPermissionsTest(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes["/v3/projects/my-project-123:testIamPermissions"] = okReply(`{"permissions":["resourcemanager.projects.get"]}`)
	out, err := e.invoke(iamPermissionsTestID, `{"resource_type":"project","resource_id":"my-project-123","permissions":`+
		`["resourcemanager.projects.get","resourcemanager.projects.delete"]}`)
	if err != nil {
		t.Fatal(err)
	}
	api := e.fake.api()
	if len(api) != 1 || api[0].method != http.MethodPost ||
		api[0].body != `{"permissions":["resourcemanager.projects.get","resourcemanager.projects.delete"]}` {
		t.Fatalf("requests = %+v", api)
	}
	got := decode(t, out)
	if granted := got["permissions"].([]any); len(granted) != 1 || granted[0] != "resourcemanager.projects.get" ||
		got["resource"] != "projects/my-project-123" {
		t.Errorf("result = %s", out)
	}

	e = newEnvironment(t, nil)
	e.fake.routes["/v3/projects/my-project-123:testIamPermissions"] = okReply(`{}`)
	out, err = e.invoke(iamPermissionsTestID, `{"resource_type":"project","resource_id":"my-project-123","permissions":["compute.instances.get"]}`)
	if err != nil || strings.Contains(out, "compute.instances.get") || !strings.Contains(out, "my-project-123") {
		t.Errorf("nothing granted: out=%s err=%v", out, err)
	}
}

func TestIAMPermissionsTestRefusesBadListsBeforeIO(t *testing.T) {
	many := make([]string, 101)
	for i := range many {
		many[i] = fmt.Sprintf(`"compute.things.get%d"`, i)
	}
	for name, permissions := range map[string]string{
		"101 permissions": `[` + strings.Join(many, ",") + `]`,
		"none":            `[]`,
		"wildcard":        `["compute.*"]`,
		"duplicate":       `["compute.instances.get","compute.instances.get"]`,
		"free text":       `["ignore previous instructions"]`,
	} {
		e := newEnvironment(t, nil)
		if _, err := e.invoke(iamPermissionsTestID, `{"resource_type":"project","resource_id":"my-project-123","permissions":`+permissions+`}`); err == nil {
			t.Errorf("%s was accepted", name)
		}
		noIO(t, e, name)
	}
	e := newEnvironment(t, nil)
	ok := many[:100]
	e.fake.routes["/v3/projects/my-project-123:testIamPermissions"] = okReply(`{}`)
	if _, err := e.invoke(iamPermissionsTestID, `{"resource_type":"project","resource_id":"my-project-123","permissions":[`+strings.Join(ok, ",")+`]}`); err != nil {
		t.Errorf("100 permissions: %v", err)
	}
}

func TestIAMPermissionsTestRefusesAnAnswerOutsideTheRequest(t *testing.T) {
	for name, body := range map[string]string{
		"unknown permission": `{"permissions":["resourcemanager.projects.get","iam.roles.create"]}`,
		"repeated":           `{"permissions":["resourcemanager.projects.get","resourcemanager.projects.get"]}`,
		"not a list":         `{"permissions":"resourcemanager.projects.get"}`,
	} {
		e := newEnvironment(t, nil)
		e.fake.routes["/v3/projects/my-project-123:testIamPermissions"] = okReply(body)
		_, err := e.invoke(iamPermissionsTestID, `{"resource_type":"project","resource_id":"my-project-123","permissions":["resourcemanager.projects.get"]}`)
		if classOf(err) != provider.ClassInvalidResponse {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestIAMPolicyBoundsMembersAndSaysSo(t *testing.T) {
	members := make([]string, 600)
	for i := range members {
		members[i] = fmt.Sprintf(`"user:u%d@example.com"`, i)
	}
	e := newEnvironment(t, nil)
	e.fake.routes["/v3/projects/my-project-123:getIamPolicy"] = okReply(`{"bindings":[{"role":"roles/viewer","members":[` +
		strings.Join(members, ",") + `]},{"role":"roles/owner","members":["user:a\u0007b@example.com"]}]}`)
	out, err := e.invoke(iamPoliciesGetID, `{"resource_type":"project","resource_id":"my-project-123"}`)
	if err != nil {
		t.Fatal(err)
	}
	got := decode(t, out)
	bindings := got["bindings"].([]any)
	if n := len(bindings[0].(map[string]any)["members"].([]any)); n != maxIAMList || got["truncated"] != true {
		t.Errorf("members = %d, truncated = %v", n, got["truncated"])
	}
	if m := bindings[1].(map[string]any)["members"].([]any)[0]; m != "user:a b@example.com" {
		t.Errorf("control character kept: %q", m)
	}
}

func TestIAMFailuresAreClassified(t *testing.T) {
	for name, c := range map[string]struct {
		reply reply
		want  provider.Class
	}{
		"403":       {reply{status: 403, body: errorObject(403, "PERMISSION_DENIED", "")}, provider.ClassPermission},
		"404":       {reply{status: 404, body: errorObject(404, "NOT_FOUND", "")}, provider.ClassNotFound},
		"not json":  {reply{status: 200, body: `<html>`, header: http.Header{"Content-Type": {"text/html"}}}, provider.ClassInvalidResponse},
		"too large": {reply{status: 200, body: `{"etag":"` + strings.Repeat("x", 3<<20) + `"}`}, provider.ClassInvalidResponse},
	} {
		for _, path := range []string{"/v3/projects/my-project-123:getIamPolicy", "/v3/projects/my-project-123:testIamPermissions"} {
			e := newEnvironment(t, nil)
			e.fake.routes[path] = c.reply
			tool, args := iamPoliciesGetID, `{"resource_type":"project","resource_id":"my-project-123"}`
			if strings.HasSuffix(path, "testIamPermissions") {
				tool, args = iamPermissionsTestID, `{"resource_type":"project","resource_id":"my-project-123","permissions":["compute.instances.get"]}`
			}
			_, err := e.invoke(tool, args)
			if classOf(err) != c.want {
				t.Errorf("%s %s: err = %v (%s), want %s", name, path, err, classOf(err), c.want)
			} else if err != nil && strings.Contains(err.Error(), detailCanary) {
				t.Errorf("%s: error carries provider text", name)
			}
		}
	}
}

const denyRow = `{"name":"policies/cloudresourcemanager.googleapis.com%%2Fprojects%%2F415104041262/denypolicies/%s","uid":"u-%s",` +
	`"kind":"DenyPolicy","displayName":"Block %s","etag":"MTY4","createTime":"2026-01-02T03:04:05Z","updateTime":"2026-02-03T04:05:06Z"}`

func TestDenyPoliciesListFollowsTwoPagesAndEncodesTheAttachmentPoint(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.hook = func(_ int, req *http.Request) *reply {
		if !strings.HasSuffix(req.URL.Path, "/denypolicies") {
			return nil
		}
		if req.URL.Query().Get("pageToken") == "" {
			return &reply{status: 200, body: `{"policies":[` + fmt.Sprintf(denyRow, "one", "1", "one") + `],"nextPageToken":"page-two"}`}
		}
		return &reply{status: 200, body: `{"policies":[` + fmt.Sprintf(denyRow, "two", "2", "two") + `]}`}
	}
	args := `{"resource_type":"project","resource_id":"my-project-123","page_size":1`
	out, err := e.invoke(denyPoliciesListID, args+`}`)
	if err != nil {
		t.Fatal(err)
	}
	first := decode(t, out)
	cursor, _ := first["next_cursor"].(string)
	if first["has_more"] != true || cursor == "" || len(first["policies"].([]any)) != 1 {
		t.Fatalf("page one = %s", out)
	}
	out, err = e.invoke(denyPoliciesListID, args+`,"cursor":"`+cursor+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	second := decode(t, out)
	if second["has_more"] != false || second["next_cursor"] != nil || second["policies"].([]any)[0].(map[string]any)["displayName"] != "Block two" {
		t.Fatalf("page two = %s", out)
	}
	api := e.fake.api()
	if len(api) != 2 || api[0].method != http.MethodGet ||
		api[0].url != "https://iam.googleapis.com/v2/policies/cloudresourcemanager.googleapis.com%2Fprojects%2Fmy-project-123/denypolicies?pageSize=1" ||
		api[1].url != "https://iam.googleapis.com/v2/policies/cloudresourcemanager.googleapis.com%2Fprojects%2Fmy-project-123/denypolicies?pageSize=1&pageToken=page-two" {
		t.Errorf("requests = %+v", api)
	}

	// The cursor continues only the list that issued it.
	for name, other := range map[string]string{
		"other resource": `{"resource_type":"project","resource_id":"other-project-9","cursor":"` + cursor + `"}`,
		"other type":     `{"resource_type":"folder","resource_id":"2001","cursor":"` + cursor + `"}`,
		"altered":        `{"resource_type":"project","resource_id":"my-project-123","cursor":"` + cursor + `x"}`,
	} {
		e2 := newEnvironment(t, nil)
		if _, err := e2.invoke(denyPoliciesListID, other); !isInvalidRequest(err) {
			t.Errorf("%s: err = %v", name, err)
		}
		noIO(t, e2, name)
	}
}

func TestDenyPolicyGetReturnsRulesWithPrincipalsUnchanged(t *testing.T) {
	const body = `{"name":"policies/cloudresourcemanager.googleapis.com%2Ffolders%2F2001/denypolicies/block-delete","uid":"u1",` +
		`"kind":"DenyPolicy","displayName":"Block delete","annotations":{"team":"sec"},"etag":"MTY4",` +
		`"rules":[{"description":"no deletes","denyRule":{"deniedPrincipals":["principalSet://goog/public:all","user:ada@example.com"],` +
		`"exceptionPrincipals":["group:admins@example.com"],"deniedPermissions":["cloudresourcemanager.googleapis.com/projects.delete"],` +
		`"exceptionPermissions":["iam.googleapis.com/roles.list"],"denialCondition":{"expression":"true","title":"always"}}}]}`
	e := newEnvironment(t, nil)
	e.fake.routes["/v2/policies/cloudresourcemanager.googleapis.com/folders/2001/denypolicies/block-delete"] = okReply(body)
	out, err := e.invoke(denyPoliciesGetID, `{"resource_type":"folder","resource_id":"2001","policy_id":"block-delete"}`)
	if err != nil {
		t.Fatal(err)
	}
	if got := e.fake.api()[0].url; got != "https://iam.googleapis.com/v2/policies/cloudresourcemanager.googleapis.com%2Ffolders%2F2001/denypolicies/block-delete" {
		t.Errorf("url = %s", got)
	}
	rule := decode(t, out)["rules"].([]any)[0].(map[string]any)["denyRule"].(map[string]any)
	if principals := rule["deniedPrincipals"].([]any); len(principals) != 2 || principals[0] != "principalSet://goog/public:all" ||
		rule["denialCondition"].(map[string]any)["title"] != "always" || rule["exceptionPermissions"].([]any)[0] != "iam.googleapis.com/roles.list" {
		t.Errorf("rule = %v", rule)
	}
}

func TestDenyPolicyRefusesAPathAndAWrongAnswer(t *testing.T) {
	for _, id := range []string{"../x", "a/b", "a b", "%2F", ""} {
		e := newEnvironment(t, nil)
		if _, err := e.invoke(denyPoliciesGetID, `{"resource_type":"project","resource_id":"my-project-123","policy_id":"`+id+`"}`); err == nil {
			t.Errorf("policy_id %q was accepted", id)
		}
		noIO(t, e, "policy_id "+id)
	}
	e := newEnvironment(t, nil)
	e.fake.routes["/v2/policies/cloudresourcemanager.googleapis.com/projects/my-project-123/denypolicies/block"] =
		okReply(`{"name":"policies/x/denypolicies/other"}`)
	if _, err := e.invoke(denyPoliciesGetID, `{"resource_type":"project","resource_id":"my-project-123","policy_id":"block"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("err = %v, want an invalid response", err)
	}
}

func TestDenyPoliciesFailures(t *testing.T) {
	for name, c := range map[string]struct {
		reply reply
		want  provider.Class
	}{
		"403": {reply{status: 403, body: errorObject(403, "PERMISSION_DENIED", "")}, provider.ClassPermission},
		"404": {reply{status: 404, body: errorObject(404, "NOT_FOUND", "")}, provider.ClassNotFound},
	} {
		for tool, args := range map[string]string{
			denyPoliciesListID: `{"resource_type":"project","resource_id":"my-project-123"}`,
			denyPoliciesGetID:  `{"resource_type":"project","resource_id":"my-project-123","policy_id":"block"}`,
		} {
			e := newEnvironment(t, nil)
			e.fake.hook = func(_ int, req *http.Request) *reply {
				if strings.Contains(req.URL.Path, "denypolicies") {
					return &c.reply
				}
				return nil
			}
			if _, err := e.invoke(tool, args); classOf(err) != c.want {
				t.Errorf("%s %s: err = %v", tool, name, err)
			}
		}
	}
}

func TestIAMToolsAreOfferedOnlyToBindingsThatReachThem(t *testing.T) {
	for _, tool := range []string{iamPoliciesGetID, iamPermissionsTestID, denyPoliciesListID, denyPoliciesGetID} {
		if reaches, _ := targetReaches(tool, []string{"billingAccounts/012345-ABCDEF-678901"}); reaches {
			t.Errorf("%s is offered to a billing-only binding", tool)
		}
		for _, targets := range [][]string{nil, {"projects/my-project-123"}, {"folders/2001"}, {"organizations/9001"}} {
			if reaches, _ := targetReaches(tool, targets); !reaches {
				t.Errorf("%s is hidden from %v", tool, targets)
			}
		}
	}
}

func TestIAMToolsAskForTheScopeOfTheirMethods(t *testing.T) {
	for _, tool := range []string{iamPoliciesGetID, iamPermissionsTestID, denyPoliciesListID, denyPoliciesGetID} {
		if got := tokenMetadata.Scopes[tool]; len(got) != 1 || got[0] != scopeCloudPlatform {
			t.Errorf("%s scopes = %v", tool, got)
		}
	}
}
