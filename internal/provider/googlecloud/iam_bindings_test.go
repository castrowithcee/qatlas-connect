package googlecloud

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// The parts of the policy the binding tests read. Spacing and the escape in the condition are deliberate: a
// binding the tool does not change must go back byte for byte.
const (
	viewerBinding = `{"role":"roles/viewer","members":["user:alice@example.com","allUsers"]}`
	condBinding   = `{ "role" : "roles/storage.objectViewer", "members" : [ "group:ops@example.com" ], ` +
		`"condition" : {"title":"temp","expression":"request.time < timestamp(\"2027-01-01T00:00:00Z\") && true"} }`
	editorBinding = `{"role":"roles/editor","members":["user:bob@example.com","deleted:user:carol@example.com?uid=123"]}`
	auditPart     = `[{"service":"allServices","auditLogConfigs":[{"logType":"DATA_READ","exemptedMembers":["user:alice@example.com"]}]}]`
	bindingPolicy = `{"version":3,"etag":"BwOld=","bindings":[` + viewerBinding + `,` + condBinding + `,` +
		editorBinding + `],"auditConfigs":` + auditPart + `}`

	condExpression = "request.time < timestamp(\"2027-01-01T00:00:00Z\") && true"
	bindingProject = `{"name":"projects/415104041262","parent":"folders/2001","projectId":"example-project-123","state":"ACTIVE"}`
	deployAccount  = "deploy@example-project-123.iam.gserviceaccount.com"
	projectPolicy  = "/v3/projects/example-project-123"
)

// bindingEnvironment is a connection that may change, with the policy above behind the project and a
// setIamPolicy that answers with the policy it was sent and a new etag.
func bindingEnvironment(t *testing.T, targets []string, policy string) *environment {
	t.Helper()
	e := newEnvironment(t, targets)
	conn := e.cfg.Connections["work"]
	conn.Effects = []config.Effect{config.EffectRead, config.EffectUpdate}
	e.cfg.Connections["work"] = conn
	if err := e.cfg.Validate(); err != nil {
		t.Fatalf("Validate() = %v", err)
	}
	e.core = application.New(e.reg, e.cfg, e.secrets(), e.red)
	for _, resource := range []string{projectPolicy, "/v3/folders/2001", "/v3/organizations/9001"} {
		e.fake.routes[resource+":getIamPolicy"] = okReply(policy)
	}
	e.fake.routes[projectPolicy] = okReply(bindingProject)
	e.fake.hook = func(n int, req *http.Request) *reply {
		if !strings.HasSuffix(req.URL.Path, ":setIamPolicy") {
			return nil
		}
		// The fake holds its lock while it calls the hook, so it reads the request it just recorded directly.
		var sent struct {
			Policy map[string]any `json:"policy"`
		}
		if err := json.Unmarshal([]byte(e.fake.requests[n-1].body), &sent); err != nil {
			r := reply{status: 400, body: errorObject(400, "INVALID_ARGUMENT", "")}
			return &r
		}
		sent.Policy["etag"] = "BwNew="
		data, _ := json.Marshal(sent.Policy)
		r := okReply(string(data))
		return &r
	}
	return e
}

func (e *environment) confirmBinding(operation, arguments string) (string, error) {
	response, err := e.core.Invoke(context.Background(), application.InvokeRequest{
		Operation: operation, Connection: "work", Arguments: json.RawMessage(arguments), Confirmed: true})
	return string(response.Result), err
}

// writes returns the setIamPolicy requests the fake saw.
func writes(e *environment) []seen {
	var out []seen
	for _, r := range e.fake.api() {
		if strings.HasSuffix(r.url, ":setIamPolicy") {
			out = append(out, r)
		}
	}
	return out
}

// writtenBindings decodes the policy of the one write and returns its bindings by role and condition title.
func writtenPolicy(t *testing.T, e *environment) (map[string]any, map[string][]any) {
	t.Helper()
	sent := writes(e)
	if len(sent) != 1 {
		t.Fatalf("writes = %d, want 1", len(sent))
	}
	var body map[string]map[string]any
	if err := json.Unmarshal([]byte(sent[0].body), &body); err != nil || len(body) != 1 {
		t.Fatalf("body = %s", sent[0].body)
	}
	policy := body["policy"]
	members := map[string][]any{}
	for _, raw := range policy["bindings"].([]any) {
		b := raw.(map[string]any)
		key := b["role"].(string)
		if c, ok := b["condition"].(map[string]any); ok {
			key += "|" + c["title"].(string)
		}
		if _, twice := members[key]; twice {
			t.Fatalf("binding %s written twice", key)
		}
		members[key] = b["members"].([]any)
	}
	return policy, members
}

func project(arguments string) string {
	return `{"resource_type":"project","resource_id":"example-project-123",` + arguments + `}`
}

func TestAddMemberFromAnotherBindingKeepsEverythingElseByteForByte(t *testing.T) {
	e := bindingEnvironment(t, nil, bindingPolicy)
	out, err := e.confirmBinding(iamBindingsAddID, project(`"role":"roles/viewer","member":"user:bob@example.com"`))
	if err != nil {
		t.Fatal(err)
	}
	api := e.fake.api()
	if len(api) != 2 || api[0].body != `{"options":{"requestedPolicyVersion":3}}` || api[1].method != http.MethodPost ||
		api[1].url != "https://cloudresourcemanager.googleapis.com"+projectPolicy+":setIamPolicy" {
		t.Fatalf("requests = %+v", api)
	}
	body := api[1].body
	for _, part := range []string{condBinding, editorBinding, `"auditConfigs":` + auditPart, `"etag":"BwOld="`, `"version":3`} {
		if !strings.Contains(body, part) {
			t.Errorf("written policy lacks %s byte for byte:\n%s", part, body)
		}
	}
	if strings.Contains(body, "updateMask") {
		t.Errorf("body carries an update mask: %s", body)
	}
	_, members := writtenPolicy(t, e)
	if got := members["roles/viewer"]; len(got) != 3 || got[2] != "user:bob@example.com" || got[1] != "allUsers" {
		t.Errorf("viewer = %v", got)
	}
	got := decode(t, out)
	binding := got["binding"].(map[string]any)
	if got["changed"] != true || got["etag"] != "BwNew=" || got["version"] != float64(3) ||
		got["resource"] != "projects/example-project-123" || binding["role"] != "roles/viewer" ||
		len(binding["members"].([]any)) != 3 || got["bindings"] != nil {
		t.Errorf("result = %s", out)
	}
}

func TestAddMemberToANewRoleCreatesTheBinding(t *testing.T) {
	e := bindingEnvironment(t, nil, bindingPolicy)
	if _, err := e.confirmBinding(iamBindingsAddID, project(`"role":"roles/browser","member":"user:alice@example.com"`)); err != nil {
		t.Fatal(err)
	}
	body := writes(e)[0].body
	if !strings.Contains(body, viewerBinding) || !strings.Contains(body, `{"members":["user:alice@example.com"],"role":"roles/browser"}`) {
		t.Errorf("body = %s", body)
	}
}

// Google fixes the domains of the default and agent accounts; they never name an example domain.
const (
	computeDomain = "developer.gserviceaccount.com"
	appspotDomain = "appspot.gserviceaccount.com"
	agentDomain   = "gcp-sa-example.iam.gserviceaccount.com"
)

func TestAddServiceAccountOfTheSameProject(t *testing.T) {
	cases := []struct{ email, path string }{
		{deployAccount, "/v1/projects/example-project-123/serviceAccounts/" + deployAccount},
		{"415104041262-compute@" + computeDomain,
			"/v1/projects/example-project-123/serviceAccounts/415104041262-compute@" + computeDomain},
		{"example-project-123@" + appspotDomain,
			"/v1/projects/example-project-123/serviceAccounts/example-project-123@" + appspotDomain},
	}
	for _, c := range cases {
		e := bindingEnvironment(t, nil, bindingPolicy)
		e.fake.routes[c.path] = okReply(`{"name":"projects/example-project-123/serviceAccounts/` + c.email +
			`","projectId":"example-project-123","email":"` + c.email + `","uniqueId":"1234"}`)
		if _, err := e.confirmBinding(iamBindingsAddID, project(`"role":"roles/storage.admin","member":"serviceAccount:`+c.email+`"`)); err != nil {
			t.Fatalf("%s: %v", c.email, err)
		}
		api := e.fake.api()
		if len(api) != 4 || !strings.HasSuffix(api[1].url, projectPolicy) || !strings.HasSuffix(api[2].url, c.path) ||
			api[2].method != http.MethodGet || len(writes(e)) != 1 {
			t.Errorf("%s: requests = %+v", c.email, api)
		}
	}
}

func TestMembersOutsideTheRuleAreRefusedBeforeTheWrite(t *testing.T) {
	cases := []struct {
		name, member string
		reads        int
	}{
		{"new user", "user:dave@example.com", 1},
		{"new domain", "domain:example.com", 1},
		{"new group", "group:other@example.com", 1},
		{"new principal set", "principalSet://iam.googleapis.com/locations/global/workforcePools/example-pool/*", 1},
		{"other case of a member", "User:alice@example.com", 1},
		{"member with a uid suffix", "user:carol@example.com?uid=123", 1},
		{"service account of another project", "serviceAccount:deploy@example-other-456.iam.gserviceaccount.com", 2},
		{"compute account of another project", "serviceAccount:999999999999-compute@" + computeDomain, 2},
		{"upper case service account", "serviceAccount:Deploy@example-project-123.iam.gserviceaccount.com", 1},
		{"service agent", "serviceAccount:service-415104041262@" + agentDomain, 2},
		{"service account that does not exist", "serviceAccount:" + deployAccount, 3},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := bindingEnvironment(t, nil, bindingPolicy)
			_, err := e.confirmBinding(iamBindingsAddID, project(`"role":"roles/viewer","member":"`+c.member+`"`))
			var invalid *provider.InvalidRequestError
			if !errors.As(err, &invalid) {
				t.Fatalf("err = %v, want a refusal", err)
			}
			if strings.Contains(err.Error(), c.member) {
				t.Errorf("refusal quotes the member: %v", err)
			}
			if api := e.fake.api(); len(api) != c.reads || len(writes(e)) != 0 {
				t.Errorf("requests = %+v, want %d reads and no write", api, c.reads)
			}
		})
	}
	// A folder admits no service account the policy does not hold, and reads no project for it.
	e := bindingEnvironment(t, nil, bindingPolicy)
	if _, err := e.confirmBinding(iamBindingsAddID, `{"resource_type":"folder","resource_id":"2001",`+
		`"role":"roles/viewer","member":"serviceAccount:`+deployAccount+`"}`); err == nil {
		t.Fatal("a new service account was granted on a folder")
	}
	if api := e.fake.api(); len(api) != 1 {
		t.Errorf("requests = %+v", api)
	}
}

func TestPublicAndDeletedMembersAreNeverGranted(t *testing.T) {
	for _, member := range []string{"allUsers", "allAuthenticatedUsers", "AllUsers", "allauthenticatedusers",
		"deleted:user:carol@example.com?uid=123", "Deleted:user:carol@example.com?uid=123"} {
		e := bindingEnvironment(t, nil, bindingPolicy)
		// allUsers is already in the policy; it is refused all the same.
		if _, err := e.confirmBinding(iamBindingsAddID, project(`"role":"roles/editor","member":"`+member+`"`)); err == nil {
			t.Errorf("%s was granted", member)
		}
		noIO(t, e, member)
		e = bindingEnvironment(t, nil, bindingPolicy)
		if _, err := e.confirmBinding(iamBindingsChangeID, project(`"from_role":"roles/viewer","to_role":"roles/editor","member":"`+member+`"`)); err == nil {
			t.Errorf("%s was moved", member)
		}
		noIO(t, e, member+" moved")
	}
	for _, member := range []string{" user:alice@example.com", "user:alice@example.com\n", "", "alice@example.com"} {
		e := bindingEnvironment(t, nil, bindingPolicy)
		if _, err := e.confirmBinding(iamBindingsRemoveID, project(`"role":"roles/viewer","member":`+quote(member))); err == nil {
			t.Errorf("member %q was accepted", member)
		}
		noIO(t, e, "malformed member")
	}
}

func quote(value string) string {
	data, _ := json.Marshal(value)
	return string(data)
}

func TestRemoveAllUsersIsAllowed(t *testing.T) {
	e := bindingEnvironment(t, nil, bindingPolicy)
	out, err := e.confirmBinding(iamBindingsRemoveID, project(`"role":"roles/viewer","member":"allUsers"`))
	if err != nil {
		t.Fatal(err)
	}
	_, members := writtenPolicy(t, e)
	if got := members["roles/viewer"]; len(got) != 1 || got[0] != "user:alice@example.com" {
		t.Errorf("viewer = %v", got)
	}
	if got := decode(t, out); got["changed"] != true || strings.Contains(out, "allUsers") {
		t.Errorf("result = %s", out)
	}
}

func TestConditionalBindingIsMatchedExactly(t *testing.T) {
	condition := `"condition":{"title":"temp","expression":` + quote(condExpression) + `}`
	e := bindingEnvironment(t, nil, bindingPolicy)
	out, err := e.confirmBinding(iamBindingsAddID, project(`"role":"roles/storage.objectViewer","member":"user:alice@example.com",`+condition))
	if err != nil {
		t.Fatal(err)
	}
	body := writes(e)[0].body
	// The condition of the changed binding goes back as it was read, escape included.
	if !strings.Contains(body, `"condition":{"title":"temp","expression":"request.time < timestamp(\"2027-01-01T00:00:00Z\") && true"}`) ||
		!strings.Contains(body, viewerBinding) || !strings.Contains(body, editorBinding) {
		t.Errorf("body = %s", body)
	}
	_, members := writtenPolicy(t, e)
	if got := members["roles/storage.objectViewer|temp"]; len(got) != 2 || got[1] != "user:alice@example.com" {
		t.Errorf("conditional binding = %v", members)
	}
	if got := decode(t, out)["binding"].(map[string]any); got["condition"].(map[string]any)["title"] != "temp" {
		t.Errorf("result = %s", out)
	}

	// Another description, or no condition at all, is another binding: the conditional one stays untouched.
	for _, arguments := range []string{
		`"condition":{"title":"temp","expression":` + quote(condExpression) + `,"description":"x"}`,
		`"condition":{"title":"temp","expression":` + quote(condExpression+" ") + `}`,
		`"condition":{"title":"Temp","expression":` + quote(condExpression) + `}`,
	} {
		e := bindingEnvironment(t, nil, bindingPolicy)
		if _, err := e.confirmBinding(iamBindingsAddID, project(`"role":"roles/storage.objectViewer","member":"user:alice@example.com",`+arguments)); err != nil {
			t.Fatal(err)
		}
		if body := writes(e)[0].body; !strings.Contains(body, condBinding) {
			t.Errorf("%s changed the conditional binding: %s", arguments, body)
		}
	}
	e = bindingEnvironment(t, nil, bindingPolicy)
	out, err = e.confirmBinding(iamBindingsRemoveID, project(`"role":"roles/storage.objectViewer","member":"group:ops@example.com"`))
	if err != nil {
		t.Fatal(err)
	}
	if got := decode(t, out); got["changed"] != false || len(writes(e)) != 0 {
		t.Errorf("remove without the condition changed the conditional binding: %s", out)
	}
}

func TestAConditionSetsVersionThreeAndRemovingTheLastMemberDropsTheBinding(t *testing.T) {
	plain := `{"version":1,"etag":"BwOld=","bindings":[` + viewerBinding + `]}`
	e := bindingEnvironment(t, nil, plain)
	if _, err := e.confirmBinding(iamBindingsAddID, project(`"role":"roles/viewer","member":"user:alice@example.com",`+
		`"condition":{"title":"until","expression":"request.time < timestamp(\"2027-01-01T00:00:00Z\")"}`)); err != nil {
		t.Fatal(err)
	}
	policy, members := writtenPolicy(t, e)
	if policy["version"] != float64(3) || len(members["roles/viewer|until"]) != 1 || len(members["roles/viewer"]) != 2 {
		t.Errorf("policy = %v", policy)
	}

	e = bindingEnvironment(t, nil, bindingPolicy)
	out, err := e.confirmBinding(iamBindingsRemoveID, project(`"role":"roles/storage.objectViewer","member":"group:ops@example.com",`+
		`"condition":{"title":"temp","expression":`+quote(condExpression)+`}`))
	if err != nil {
		t.Fatal(err)
	}
	if body := writes(e)[0].body; strings.Contains(body, "storage.objectViewer") || !strings.Contains(body, `"version":3`) {
		t.Errorf("body = %s, want the empty binding dropped and the version kept", body)
	}
	// An empty list is left out of the answer.
	if got := decode(t, out)["binding"].(map[string]any); got["members"] != nil || got["role"] != "roles/storage.objectViewer" {
		t.Errorf("result = %s", out)
	}
}

func TestNoChangeWritesNothing(t *testing.T) {
	for _, c := range []struct{ tool, arguments string }{
		{iamBindingsAddID, `"role":"roles/editor","member":"user:bob@example.com"`},
		{iamBindingsRemoveID, `"role":"roles/viewer","member":"user:dave@example.com"`},
		{iamBindingsRemoveID, `"role":"roles/owner","member":"user:alice@example.com"`},
		{iamBindingsChangeID, `"from_role":"roles/owner","to_role":"roles/viewer","member":"user:alice@example.com"`},
	} {
		e := bindingEnvironment(t, nil, bindingPolicy)
		out, err := e.confirmBinding(c.tool, project(c.arguments))
		if err != nil {
			t.Fatalf("%s: %v", c.arguments, err)
		}
		if got := decode(t, out); got["changed"] != false || got["etag"] != "BwOld=" || len(e.fake.api()) != 1 {
			t.Errorf("%s: result = %s, requests = %d", c.arguments, out, len(e.fake.api()))
		}
	}
}

func TestChangeRoleMovesTheMemberInOneWrite(t *testing.T) {
	e := bindingEnvironment(t, nil, bindingPolicy)
	out, err := e.confirmBinding(iamBindingsChangeID, project(`"from_role":"roles/editor","to_role":"roles/viewer","member":"user:bob@example.com"`))
	if err != nil {
		t.Fatal(err)
	}
	_, members := writtenPolicy(t, e)
	if got := members["roles/editor"]; len(got) != 1 || got[0] != "deleted:user:carol@example.com?uid=123" {
		t.Errorf("editor = %v", got)
	}
	if got := members["roles/viewer"]; len(got) != 3 || got[2] != "user:bob@example.com" {
		t.Errorf("viewer = %v", got)
	}
	got := decode(t, out)
	if got["binding"].(map[string]any)["role"] != "roles/viewer" || got["from_binding"].(map[string]any)["role"] != "roles/editor" {
		t.Errorf("result = %s", out)
	}
	e = bindingEnvironment(t, nil, bindingPolicy)
	if _, err := e.confirmBinding(iamBindingsChangeID, project(`"from_role":"roles/editor","to_role":"roles/viewer","member":"user:dave@example.com"`)); err == nil {
		t.Error("a member outside from_role was moved")
	}
	if len(writes(e)) != 0 {
		t.Error("a refused move wrote")
	}
	e = bindingEnvironment(t, nil, bindingPolicy)
	if _, err := e.confirmBinding(iamBindingsChangeID, project(`"from_role":"roles/viewer","to_role":"roles/viewer","member":"user:alice@example.com"`)); err == nil {
		t.Error("the same role was accepted")
	}
	noIO(t, e, "same role")
}

func TestEtagConflictIsAConflictWithoutRetry(t *testing.T) {
	e := bindingEnvironment(t, nil, bindingPolicy)
	e.fake.routes[projectPolicy+":setIamPolicy"] = reply{status: 409, body: errorObject(409, "ABORTED", "")}
	e.fake.hook = nil
	_, err := e.confirmBinding(iamBindingsAddID, project(`"role":"roles/viewer","member":"user:bob@example.com"`))
	if classOf(err) != provider.ClassProviderError || !strings.Contains(err.Error(), "conflict") ||
		strings.Contains(err.Error(), detailCanary) {
		t.Fatalf("err = %v", err)
	}
	if len(writes(e)) != 1 {
		t.Errorf("writes = %d, want exactly one", len(writes(e)))
	}
}

func TestAFailedWriteIsNeverRepeatedAndSaysWhenItsOutcomeIsUnknown(t *testing.T) {
	for _, c := range []struct {
		answer  reply
		unknown bool
	}{
		{reply{status: 503, body: errorObject(503, "UNAVAILABLE", "")}, true},
		{reply{status: 500, body: ``}, true},
		{reply{status: 200, body: `not json`, header: http.Header{"Content-Type": {"text/plain"}}}, true},
		{reply{status: 401, body: errorObject(401, "UNAUTHENTICATED", "ACCESS_TOKEN_EXPIRED")}, false},
		{reply{status: 403, body: errorObject(403, "PERMISSION_DENIED", "")}, false},
	} {
		e := bindingEnvironment(t, nil, bindingPolicy)
		e.fake.routes[projectPolicy+":setIamPolicy"] = c.answer
		e.fake.hook = nil
		_, err := e.confirmBinding(iamBindingsAddID, project(`"role":"roles/viewer","member":"user:bob@example.com"`))
		if err == nil {
			t.Fatalf("status %d: no error", c.answer.status)
		}
		if strings.Contains(err.Error(), "unknown") != c.unknown {
			t.Errorf("status %d: err = %v, unknown outcome %t", c.answer.status, err, c.unknown)
		}
		// The process caches the access token, so a later case may need no token request at all.
		if len(writes(e)) != 1 || e.fake.tokenRequests() > 1 {
			t.Errorf("status %d: writes = %d, tokens = %d, want one write and at most one token", c.answer.status,
				len(writes(e)), e.fake.tokenRequests())
		}
	}
}

func TestAPolicyWithoutEtagIsNotWritten(t *testing.T) {
	e := bindingEnvironment(t, nil, `{"version":1,"bindings":[`+viewerBinding+`]}`)
	_, err := e.confirmBinding(iamBindingsAddID, project(`"role":"roles/editor","member":"user:alice@example.com"`))
	if classOf(err) != provider.ClassInvalidResponse || len(writes(e)) != 0 {
		t.Errorf("err = %v, writes = %d", err, len(writes(e)))
	}
}

func TestBindingTargetsAndConfirmation(t *testing.T) {
	add := func(resourceType, id string) string {
		return `{"resource_type":"` + resourceType + `","resource_id":"` + id + `","role":"roles/viewer","member":"user:alice@example.com"}`
	}
	for _, c := range []struct {
		name    string
		targets []string
		args    string
	}{
		{"organization without targets", nil, add("organization", "9001")},
		{"organization not named", []string{"projects/example-project-123", "organizations/9002"}, add("organization", "9001")},
		{"project outside", []string{"projects/example-other-456"}, add("project", "example-project-123")},
		{"service account resource", nil, add("service-account", deployAccount)},
		{"billing only", []string{"billingAccounts/ABCDEF-123456-ABCDEF"}, add("project", "example-project-123")},
	} {
		e := bindingEnvironment(t, c.targets, bindingPolicy)
		if _, err := e.confirmBinding(iamBindingsAddID, c.args); err == nil {
			t.Errorf("%s: accepted", c.name)
		}
		noIO(t, e, c.name)
	}

	e := bindingEnvironment(t, []string{"organizations/9001"}, bindingPolicy)
	if _, err := e.confirmBinding(iamBindingsAddID, strings.Replace(add("organization", "9001"), "roles/viewer", "roles/browser", 1)); err != nil {
		t.Fatal(err)
	}
	if sent := writes(e); len(sent) != 1 || sent[0].url != "https://cloudresourcemanager.googleapis.com/v3/organizations/9001:setIamPolicy" {
		t.Errorf("writes = %+v", sent)
	}

	for _, tool := range []string{iamBindingsAddID, iamBindingsRemoveID, iamBindingsChangeID} {
		e := bindingEnvironment(t, nil, bindingPolicy)
		args := project(`"role":"roles/viewer","member":"user:bob@example.com"`)
		if tool == iamBindingsChangeID {
			args = project(`"from_role":"roles/editor","to_role":"roles/viewer","member":"user:bob@example.com"`)
		}
		if _, err := e.invoke(tool, args); err == nil {
			t.Errorf("%s ran without confirmation", tool)
		}
		noIO(t, e, tool+" unconfirmed")
	}
}

func TestBindingToolsAreConfirmedUpdatesWithASummary(t *testing.T) {
	e := bindingEnvironment(t, nil, bindingPolicy)
	for _, id := range []string{iamBindingsAddID, iamBindingsRemoveID, iamBindingsChangeID} {
		descriptor, _, ok := e.reg.Lookup(id)
		if !ok || descriptor.Risk.Effect != "update" || descriptor.Risk.Confirmation != "required" ||
			descriptor.Risk.Idempotency != "idempotent" || !descriptor.Risk.OpenWorld || descriptor.RequiresToolAllowList {
			t.Errorf("%s risk = %+v", id, descriptor.Risk)
		}
		if strings.Contains(string(descriptor.InputSchema), "policy") || strings.Contains(string(descriptor.InputSchema), "etag") {
			t.Errorf("%s takes a policy or an etag", id)
		}
	}
	summary := e.reg.ConfirmationSummary(iamBindingsChangeID)
	if summary == nil {
		t.Fatal("change-role has no confirmation summary")
	}
	text := summary(json.RawMessage(project(`"from_role":"roles/viewer","to_role":"roles/owner","member":"user:alice@example.com"`)))
	for _, part := range []string{"projects/example-project-123", "roles/viewer", "roles/owner", "user:alice@example.com", "Move", "full control"} {
		if !strings.Contains(text, part) {
			t.Errorf("summary %q lacks %s", text, part)
		}
	}
	add := e.reg.ConfirmationSummary(iamBindingsAddID)(json.RawMessage(`{"resource_type":"folder","resource_id":"2001",` +
		`"role":"roles/viewer","member":"group:ops@example.com","condition":{"title":"temp","expression":"true"}}`))
	if !strings.Contains(add, "folders/2001") || !strings.Contains(add, `"temp"`) || !strings.Contains(add, "Grant") {
		t.Errorf("summary = %q", add)
	}
	if remove := e.reg.ConfirmationSummary(iamBindingsRemoveID)(json.RawMessage(project(`"role":"roles/owner","member":"allUsers"`))); !strings.Contains(remove, "Remove allUsers from roles/owner") ||
		strings.Contains(remove, "full control") {
		t.Errorf("summary = %q", remove)
	}
	if !slices.Contains(tokenMetadata.Scopes[iamBindingsAddID], scopeCloudPlatform) {
		t.Error("add-member does not ask for cloud-platform")
	}
}
