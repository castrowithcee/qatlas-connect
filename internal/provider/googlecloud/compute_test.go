package googlecloud

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const (
	instancesPath   = "/compute/v1/projects/my-project-123/zones/us-central1-a/instances"
	aggregatedPath  = "/compute/v1/projects/my-project-123/aggregated/instances"
	machineTypePath = "/compute/v1/projects/my-project-123/zones/us-central1-a/machineTypes"
	scriptCanary    = "startup-script-canary-4b7e"
	keyCanary       = "synthetic-csek-canary-2d19"
)

// instanceBody is an instance with an external address, metadata that holds a start-up script, a
// customer-supplied encryption key, and a service account.
func instanceBody(name string) string {
	return `{"kind":"compute#instance","id":"4100000000000000001","creationTimestamp":"2026-03-04T05:06:07.000-08:00",` +
		`"name":"` + name + `","description":"web","tags":{"items":["http"],"fingerprint":"tagfp"},` +
		`"machineType":"https://www.googleapis.com/compute/v1/projects/my-project-123/zones/us-central1-a/machineTypes/e2-small",` +
		`"status":"RUNNING","zone":"https://www.googleapis.com/compute/v1/projects/my-project-123/zones/us-central1-a",` +
		`"canIpForward":false,"cpuPlatform":"Intel Broadwell",` +
		`"networkInterfaces":[{"network":"n","networkIP":"10.128.0.2","accessConfigs":[{"type":"ONE_TO_ONE_NAT","natIP":"203.0.113.7"}]}],` +
		`"disks":[{"boot":true,"deviceName":"boot","diskEncryptionKey":{"rawKey":"` + keyCanary + `","sha256":"` + keyCanary +
		`","kmsKeyName":"projects/p/locations/l/keyRings/r/cryptoKeys/k"}}],` +
		`"metadata":{"kind":"compute#metadata","fingerprint":"mdfp","items":[{"key":"startup-script","value":"` + scriptCanary +
		`"},{"key":"ssh-keys","value":"` + scriptCanary + `"}]},` +
		`"serviceAccounts":[{"email":"synthetic@example-project.iam.gserviceaccount.com","scopes":["https://www.googleapis.com/auth/cloud-platform"]}],` +
		`"scheduling":{"automaticRestart":true},"labels":{"env":"test"},"deletionProtection":true,"selfLink":"s","unknownMember":"x"}`
}

func machineTypeBody(name string) string {
	return `{"kind":"compute#machineType","id":"1","name":"` + name + `","guestCpus":2,"memoryMb":2048,` +
		`"maximumPersistentDisks":128,"isSharedCpu":true,"imageSpaceGb":0,"deprecated":{"state":"DEPRECATED"},` +
		`"zone":"us-central1-a","architecture":"X86_64"}`
}

func TestInstancesListFollowsThreePagesAndBuildsTheQuery(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.hook = func(_ int, req *http.Request) *reply {
		if req.URL.Path != instancesPath {
			return nil
		}
		switch req.URL.Query().Get("pageToken") {
		case "":
			return &reply{status: 200, body: `{"items":[` + instanceBody("vm-a") + `],"nextPageToken":"p2"}`}
		case "p2":
			return &reply{status: 200, body: `{"items":[` + instanceBody("vm-b") + `],"nextPageToken":"p3"}`}
		}
		return &reply{status: 200, body: `{"items":[` + instanceBody("vm-c") + `]}`}
	}
	var names []string
	cursor := ""
	for page := 1; page <= 3; page++ {
		arguments := `{"project_id":"my-project-123","zone":"us-central1-a","filter":"status = RUNNING","max_results":1,` +
			`"order_by":"creationTimestamp desc"`
		if cursor != "" {
			arguments += `,"cursor":"` + cursor + `"`
		}
		out, err := e.invoke(instancesListID, arguments+`}`)
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		rows, got := listOf(t, out, "instances")
		for _, row := range rows {
			names = append(names, row["name"].(string))
		}
		cursor, _ = got["next_cursor"].(string)
		if got["has_more"].(bool) != (page < 3) {
			t.Fatalf("page %d: has_more = %v", page, got["has_more"])
		}
	}
	if !slices.Equal(names, []string{"vm-a", "vm-b", "vm-c"}) {
		t.Errorf("names = %v", names)
	}
	for i, q := range queriesOf(e) {
		if q.Get("filter") != "status = RUNNING" || q.Get("maxResults") != "1" || q.Get("orderBy") != "creationTimestamp desc" {
			t.Errorf("request %d: %v", i, q)
		}
	}
	for _, r := range e.fake.api() {
		u, _ := url.Parse(r.url)
		if u.Host != "compute.googleapis.com" || u.Path != instancesPath || r.method != http.MethodGet ||
			r.auth == "" || r.body != "" {
			t.Errorf("request = %+v", r)
		}
	}
}

func TestInstancesListDefaultsToAPageThatFits(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes[instancesPath] = okReply(`{}`)
	if _, err := e.invoke(instancesListID, `{"project_id":"my-project-123","zone":"us-central1-a"}`); err != nil {
		t.Fatal(err)
	}
	if got := queriesOf(e)[0].Get("maxResults"); got != "100" {
		t.Errorf("maxResults = %q", got)
	}
}

func TestInstancesListEmptyListIsAnEmptyResult(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes[instancesPath] = okReply(`{"id":"x","selfLink":"s"}`)
	out, err := e.invoke(instancesListID, `{"project_id":"my-project-123","zone":"us-central1-a"}`)
	if err != nil {
		t.Fatal(err)
	}
	// The core leaves out empty members, so an empty list is a result without instances.
	if out != `{"has_more":false}` {
		t.Errorf("out = %s", out)
	}
}

func TestInstancesListRefusesAManipulatedCursor(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes[instancesPath] = okReply(`{"items":[],"nextPageToken":"p2"}`)
	out, err := e.invoke(instancesListID, `{"project_id":"my-project-123","zone":"us-central1-a"}`)
	if err != nil {
		t.Fatal(err)
	}
	_, got := listOf(t, out, "instances")
	cursor := got["next_cursor"].(string)
	before := len(e.fake.api())
	for name, arguments := range map[string]string{
		"altered":    `{"project_id":"my-project-123","zone":"us-central1-a","cursor":"` + cursor[:len(cursor)-2] + `xx"}`,
		"bogus":      `{"project_id":"my-project-123","zone":"us-central1-a","cursor":"bogus"}`,
		"other zone": `{"project_id":"my-project-123","zone":"us-central1-b","cursor":"` + cursor + `"}`,
		"other filter": `{"project_id":"my-project-123","zone":"us-central1-a","filter":"name = x","cursor":"` +
			cursor + `"}`,
		"other order": `{"project_id":"my-project-123","zone":"us-central1-a","order_by":"name","cursor":"` + cursor + `"}`,
		"other partial": `{"project_id":"my-project-123","zone":"us-central1-a","return_partial_success":true,"cursor":"` +
			cursor + `"}`,
	} {
		if _, err := e.invoke(instancesListID, arguments); err == nil {
			t.Errorf("%s: want a refusal", name)
		}
	}
	if _, err := e.invoke(instancesListID, `{"project_id":"other-project-1","zone":"us-central1-a","cursor":"`+cursor+`"}`); err == nil {
		t.Error("cursor of another project accepted")
	}
	if _, err := e.invoke(machineTypesListID, `{"project_id":"my-project-123","zone":"us-central1-a","cursor":"`+cursor+`"}`); err == nil {
		t.Error("cursor of another tool accepted")
	}
	if len(e.fake.api()) != before {
		t.Errorf("a refused cursor reached Google")
	}
}

func TestComputeRefusesInvalidInputBeforeIO(t *testing.T) {
	long := strings.Repeat("a", 64)
	for name, c := range map[string]struct{ tool, arguments string }{
		"max results 501":    {instancesListID, `{"project_id":"my-project-123","zone":"us-central1-a","max_results":501}`},
		"max results 0":      {instancesListID, `{"project_id":"my-project-123","zone":"us-central1-a","max_results":0}`},
		"aggregated 501":     {instancesAggregatedListID, `{"project_id":"my-project-123","max_results":501}`},
		"machine types 501":  {machineTypesListID, `{"project_id":"my-project-123","zone":"us-central1-a","max_results":501}`},
		"project path":       {instancesListID, `{"project_id":"projects/x","zone":"us-central1-a"}`},
		"project traversal":  {instancesListID, `{"project_id":"../x","zone":"us-central1-a"}`},
		"project domain":     {instancesListID, `{"project_id":"example.com:proj","zone":"us-central1-a"}`},
		"zone path":          {instancesListID, `{"project_id":"my-project-123","zone":"zones/us-central1-a"}`},
		"zone traversal":     {instancesListID, `{"project_id":"my-project-123","zone":"../x"}`},
		"zone query":         {instancesListID, `{"project_id":"my-project-123","zone":"a?b=1"}`},
		"zone upper":         {instancesListID, `{"project_id":"my-project-123","zone":"US-central1-a"}`},
		"zone long":          {instancesListID, `{"project_id":"my-project-123","zone":"` + long + `"}`},
		"zone missing":       {instancesListID, `{"project_id":"my-project-123"}`},
		"zone on aggregated": {instancesAggregatedListID, `{"project_id":"my-project-123","zone":"us-central1-a"}`},
		"instance path":      {instancesGetID, `{"project_id":"my-project-123","zone":"us-central1-a","instance":"a/b"}`},
		"instance traversal": {instancesGetID, `{"project_id":"my-project-123","zone":"us-central1-a","instance":".."}`},
		"instance zero id":   {instancesGetID, `{"project_id":"my-project-123","zone":"us-central1-a","instance":"0"}`},
		"instance zone path": {instancesGetID, `{"project_id":"my-project-123","zone":"a/b","instance":"vm"}`},
		"order by":           {instancesListID, `{"project_id":"my-project-123","zone":"us-central1-a","order_by":"name; drop"}`},
		"filter control":     {instancesListID, `{"project_id":"my-project-123","zone":"us-central1-a","filter":"a\nb"}`},
		"filter long":        {machineTypesListID, `{"project_id":"my-project-123","zone":"us-central1-a","filter":"` + strings.Repeat("a", 1025) + `"}`},
		"cursor":             {machineTypesListID, `{"project_id":"my-project-123","zone":"us-central1-a","cursor":"bogus"}`},
		"unknown":            {instancesGetID, `{"project_id":"my-project-123","zone":"us-central1-a","instance":"vm","path":"/x"}`},
		"scopes on list":     {instancesListID, `{"project_id":"my-project-123","zone":"us-central1-a","include_all_scopes":true}`},
	} {
		e := newEnvironment(t, nil)
		if _, err := e.invoke(c.tool, c.arguments); err == nil {
			t.Errorf("%s: want a refusal", name)
		}
		if len(e.fake.api()) != 0 || e.fake.tokenRequests() != 0 || e.lookups != 0 {
			t.Errorf("%s: api=%d tokens=%d lookups=%d", name, len(e.fake.api()), e.fake.tokenRequests(), e.lookups)
		}
	}
	// The handlers refuse what the schema would not have let through.
	e := newEnvironment(t, nil)
	for name, call := range map[string]func() error{
		"list": func() error {
			_, err := handleInstancesList(t.Context(), e.resolved(), e.secrets(), e.red,
				[]byte(`{"project_id":"my-project-123","zone":"us-central1-a","max_results":501}`))
			return err
		},
		"aggregated zone": func() error {
			_, err := handleInstancesAggregatedList(t.Context(), e.resolved(), e.secrets(), e.red,
				[]byte(`{"project_id":"my-project-123","zone":"us-central1-a"}`))
			return err
		},
		"get": func() error {
			_, err := handleInstancesGet(t.Context(), e.resolved(), e.secrets(), e.red,
				[]byte(`{"project_id":"my-project-123","zone":"us-central1-a","instance":"a/b"}`))
			return err
		},
		"machine types": func() error {
			_, err := handleMachineTypesList(t.Context(), e.resolved(), e.secrets(), e.red,
				[]byte(`{"project_id":"my-project-123","zone":"a/b"}`))
			return err
		},
	} {
		if !isInvalidRequest(call()) {
			t.Errorf("%s: want a refusal of the handler", name)
		}
	}
	if len(e.fake.api()) != 0 {
		t.Error("a handler refusal reached Google")
	}
}

func TestComputeRefusesProjectsOutsideTheTargetsBeforeIO(t *testing.T) {
	calls := map[string]string{
		instancesListID:           `{"project_id":"other-project-1","zone":"us-central1-a"}`,
		instancesAggregatedListID: `{"project_id":"other-project-1"}`,
		instancesGetID:            `{"project_id":"other-project-1","zone":"us-central1-a","instance":"vm"}`,
		machineTypesListID:        `{"project_id":"other-project-1","zone":"us-central1-a"}`,
	}
	for id, arguments := range calls {
		e := newEnvironment(t, []string{"projects/my-project-123"})
		_, err := e.invoke(id, arguments)
		if !isInvalidRequest(err) || strings.Contains(err.Error(), "my-project-123") {
			t.Errorf("%s: err = %v", id, err)
		}
		if len(e.fake.api()) != 0 || e.fake.tokenRequests() != 0 || e.lookups != 0 {
			t.Errorf("%s: api=%d tokens=%d lookups=%d", id, len(e.fake.api()), e.fake.tokenRequests(), e.lookups)
		}
	}
	// A folder target is proven by reading the project and its parent chain before Compute is read.
	e := newEnvironment(t, []string{"folders/2001"})
	e.fake.routes["/v3/projects/my-project-123"] = okReply(projectBody)
	e.fake.routes[instancesPath] = okReply(`{"items":[` + instanceBody("vm-a") + `]}`)
	if _, err := e.invoke(instancesListID, `{"project_id":"my-project-123","zone":"us-central1-a"}`); err != nil {
		t.Fatal(err)
	}
	if paths := apiPaths(e); len(paths) != 2 || !strings.HasPrefix(paths[0], "cloudresourcemanager") ||
		!strings.HasPrefix(paths[1], "compute.googleapis.com/compute/v1/") {
		t.Errorf("paths = %v", paths)
	}
	e = newEnvironment(t, []string{"folders/9999"})
	e.fake.routes["/v3/projects/my-project-123"] = okReply(projectBody)
	e.fake.routes["/v3/folders/2001"] = okReply(`{"name":"folders/2001","parent":"organizations/777"}`)
	if _, err := e.invoke(instancesListID, `{"project_id":"my-project-123","zone":"us-central1-a"}`); !isInvalidRequest(err) {
		t.Errorf("err = %v, want a refusal", err)
	}
	for _, p := range apiPaths(e) {
		if strings.HasPrefix(p, "compute") {
			t.Errorf("Compute was read for a refused project")
		}
	}
}

func TestInstancesNeverReturnMetadataValuesOrKeyMaterial(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes[instancesPath] = okReply(`{"items":[` + instanceBody("vm-a") + `]}`)
	e.fake.routes[instancesPath+"/vm-a"] = okReply(instanceBody("vm-a"))
	e.fake.routes[aggregatedPath] = okReply(`{"items":{"zones/us-central1-a":{"instances":[` + instanceBody("vm-a") + `]}}}`)
	for id, arguments := range map[string]string{
		instancesListID:           `{"project_id":"my-project-123","zone":"us-central1-a"}`,
		instancesGetID:            `{"project_id":"my-project-123","zone":"us-central1-a","instance":"vm-a"}`,
		instancesAggregatedListID: `{"project_id":"my-project-123"}`,
	} {
		out, err := e.invoke(id, arguments)
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		for _, absent := range []string{scriptCanary, keyCanary, "rawKey", "sha256", "unknownMember", `"items":[{"key"`} {
			if strings.Contains(out, absent) {
				t.Errorf("%s: result holds %s: %s", id, absent, out)
			}
		}
		for _, want := range []string{`"keys":["startup-script","ssh-keys"]`, `"fingerprint":"mdfp"`, `"tagfp"`,
			`synthetic@example-project.iam.gserviceaccount.com`, `203.0.113.7`, `"deletionProtection":true`,
			`"status":"RUNNING"`, `"kmsKeyName"`, `"cpuPlatform":"Intel Broadwell"`, `"labels":{"env":"test"}`} {
			if !strings.Contains(out, want) {
				t.Errorf("%s: result lacks %s: %s", id, want, out)
			}
		}
	}
}

func TestInstancesGetWithoutExternalAddress(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes[instancesPath+"/vm-b"] = okReply(`{"name":"vm-b","id":"42","status":"TERMINATED","zone":"us-central1-a",` +
		`"networkInterfaces":[{"networkIP":"10.128.0.3"}],"metadata":{"fingerprint":"fp"}}`)
	out, err := e.invoke(instancesGetID, `{"project_id":"my-project-123","zone":"us-central1-a","instance":"vm-b"}`)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "accessConfigs") || strings.Contains(out, "natIP") || got["status"] != "TERMINATED" {
		t.Errorf("out = %s", out)
	}
	if md := got["metadata"].(map[string]any); md["fingerprint"] != "fp" || md["keys"] != nil {
		t.Errorf("metadata = %v", md)
	}
}

func TestInstancesGetTakesTheNumericIDAndChecksTheAnswer(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes[instancesPath+"/4100000000000000001"] = okReply(instanceBody("vm-a"))
	if _, err := e.invoke(instancesGetID, `{"project_id":"my-project-123","zone":"us-central1-a","instance":"4100000000000000001"}`); err != nil {
		t.Errorf("numeric ID: %v", err)
	}
	e.fake.routes[instancesPath+"/vm-z"] = okReply(instanceBody("vm-a"))
	_, err := e.invoke(instancesGetID, `{"project_id":"my-project-123","zone":"us-central1-a","instance":"vm-z"}`)
	if classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("another instance: err = %v", err)
	}
	e.fake.routes[instancesPath+"/vm-a"] = okReply(strings.Replace(instanceBody("vm-a"), `"zone":"https://www.googleapis.com/compute/v1/projects/my-project-123/zones/us-central1-a"`, `"zone":"https://www.googleapis.com/compute/v1/projects/my-project-123/zones/us-central1-b"`, 1))
	_, err = e.invoke(instancesGetID, `{"project_id":"my-project-123","zone":"us-central1-a","instance":"vm-a"}`)
	if classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("another zone: err = %v", err)
	}
}

func TestInstancesAggregatedListGroupsByZoneAndKeepsUnreachables(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes[aggregatedPath] = okReply(`{"items":{` +
		`"zones/us-central1-b":{"warning":{"code":"NO_RESULTS_ON_PAGE","message":"empty"}},` +
		`"zones/us-central1-a":{"instances":[` + instanceBody("vm-a") + `]},` +
		`"zones/europe-west1-b":{"warning":{"code":"UNREACHABLE","message":"zone down","data":[{"key":"scope","value":"zones/europe-west1-b"}]}}},` +
		`"unreachables":["zones/europe-west1-b"],"nextPageToken":"p2",` +
		`"warning":{"code":"UNREACHABLE","message":"partial"}}`)
	out, err := e.invoke(instancesAggregatedListID,
		`{"project_id":"my-project-123","return_partial_success":true,"include_all_scopes":true,"max_results":50}`)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Scopes       []map[string]any `json:"scopes"`
		Unreachables []string         `json:"unreachables"`
		Warning      map[string]any   `json:"warning"`
		HasMore      bool             `json:"has_more"`
		NextCursor   string           `json:"next_cursor"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Scopes) != 2 || got.Scopes[0]["scope"] != "zones/europe-west1-b" || got.Scopes[1]["scope"] != "zones/us-central1-a" {
		t.Errorf("scopes = %v", got.Scopes)
	}
	if _, has := got.Scopes[0]["instances"]; has || got.Scopes[0]["warning"] == nil {
		t.Errorf("unreachable scope = %v", got.Scopes[0])
	}
	if !slices.Equal(got.Unreachables, []string{"zones/europe-west1-b"}) || got.Warning["code"] != "UNREACHABLE" ||
		!got.HasMore || got.NextCursor == "" {
		t.Errorf("out = %s", out)
	}
	q := queriesOf(e)[0]
	if q.Get("returnPartialSuccess") != "true" || q.Get("includeAllScopes") != "true" || q.Get("maxResults") != "50" {
		t.Errorf("query = %v", q)
	}
}

func TestInstancesAggregatedListRefusesInvalidAnswers(t *testing.T) {
	for name, body := range map[string]string{
		"scope with a path": `{"items":{"zones/a/../b":{"instances":[]}}}`,
		"unknown scope":     `{"items":{"projects/x":{"instances":[]}}}`,
		"instance zone":     `{"items":{"zones/us-central1-b":{"instances":[` + instanceBody("vm-a") + `]}}}`,
		"instance name":     `{"items":{"zones/us-central1-a":{"instances":[{"name":"../x"}]}}}`,
		"long token":        `{"nextPageToken":"` + strings.Repeat("a", 3001) + `"}`,
	} {
		e := newEnvironment(t, nil)
		e.fake.routes[aggregatedPath] = okReply(body)
		if _, err := e.invoke(instancesAggregatedListID, `{"project_id":"my-project-123"}`); classOf(err) != provider.ClassInvalidResponse {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	// The global scope is a valid scope.
	e := newEnvironment(t, nil)
	e.fake.routes[aggregatedPath] = okReply(`{"items":{"global":{"warning":{"code":"X","message":"m"}}}}`)
	if _, err := e.invoke(instancesAggregatedListID, `{"project_id":"my-project-123"}`); err != nil {
		t.Errorf("global scope: %v", err)
	}
}

func TestInstancesListBoundsProviderText(t *testing.T) {
	e := newEnvironment(t, nil)
	long := strings.Repeat("x", 5000)
	e.fake.routes[instancesPath] = okReply(`{"items":[{"name":"vm-a","statusMessage":"` + long + `\u0007","labels":{"k":"` + long +
		`"}}],"warning":{"code":"X","message":"` + long + `"}}`)
	out, err := e.invoke(instancesListID, `{"project_id":"my-project-123","zone":"us-central1-a"}`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, long) || len(out) > 4000 || strings.Contains(out, `\u0007`) {
		t.Errorf("text was not bounded: %d bytes", len(out))
	}
}

func TestInstancesListFailures(t *testing.T) {
	cases := []struct {
		name  string
		reply reply
		want  provider.Class
	}{
		{"403 permission denied", reply{status: 403, body: errorObject(403, "PERMISSION_DENIED", "")}, provider.ClassPermission},
		{"403 API disabled", reply{status: 403, body: errorObject(403, "PERMISSION_DENIED", "SERVICE_DISABLED")}, provider.ClassPermission},
		{"403 legacy form", reply{status: 403, body: `{"error":{"code":403,"message":"` + detailCanary +
			`","errors":[{"message":"m","domain":"global","reason":"forbidden"}]}}`}, provider.ClassPermission},
		{"404 not found", reply{status: 404, body: errorObject(404, "NOT_FOUND", "")}, provider.ClassNotFound},
		{"404 legacy form", reply{status: 404, body: `{"error":{"code":404,"message":"` + detailCanary +
			`","errors":[{"message":"m","domain":"global","reason":"notFound"}]}}`}, provider.ClassNotFound},
		{"404 unknown form", reply{status: 404, body: `{"error":{"code":404}}`}, provider.ClassInvalidResponse},
		{"429", reply{status: 429, body: errorObject(429, "RESOURCE_EXHAUSTED", "RATE_LIMIT_EXCEEDED")}, provider.ClassRateLimited},
		{"not an object", okReply(`{"items":[1]}`), provider.ClassInvalidResponse},
	}
	for _, c := range cases {
		for id, arguments := range map[string]string{
			instancesListID:           `{"project_id":"my-project-123","zone":"us-central1-a"}`,
			instancesAggregatedListID: `{"project_id":"my-project-123"}`,
			instancesGetID:            `{"project_id":"my-project-123","zone":"us-central1-a","instance":"vm-a"}`,
			machineTypesListID:        `{"project_id":"my-project-123","zone":"us-central1-a"}`,
		} {
			if c.name == "not an object" && id == instancesGetID {
				continue
			}
			e := newEnvironment(t, nil)
			e.fake.hook = func(_ int, req *http.Request) *reply { return &c.reply }
			_, err := e.invoke(id, arguments)
			if classOf(err) != c.want {
				t.Errorf("%s %s: err = %v (%s), want %s", c.name, id, err, classOf(err), c.want)
			}
			if err != nil && strings.Contains(err.Error(), detailCanary) {
				t.Errorf("%s %s: error echoes provider text", c.name, id)
			}
		}
	}
	e := newEnvironment(t, nil)
	e.fake.routes[instancesPath] = reply{status: 403, body: errorObject(403, "PERMISSION_DENIED", "SERVICE_DISABLED")}
	_, err := e.invoke(instancesListID, `{"project_id":"my-project-123","zone":"us-central1-a"}`)
	if err == nil || !strings.Contains(err.Error(), "Compute Engine") {
		t.Errorf("err = %v, want a setup message that names Compute Engine", err)
	}
}

func TestMachineTypesListKeepsTheDocumentedFields(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.hook = func(_ int, req *http.Request) *reply {
		if req.URL.Path != machineTypePath {
			return nil
		}
		if req.URL.Query().Get("pageToken") == "" {
			return &reply{status: 200, body: `{"items":[` + machineTypeBody("e2-micro") + `],"nextPageToken":"p2",` +
				`"warning":{"code":"DEPRECATED_RESOURCE_USED","message":"m"}}`}
		}
		return &reply{status: 200, body: `{"items":[` + machineTypeBody("e2-small") + `]}`}
	}
	out, err := e.invoke(machineTypesListID, `{"project_id":"my-project-123","zone":"us-central1-a","max_results":1,"filter":"name eq e2-.*"}`)
	if err != nil {
		t.Fatal(err)
	}
	rows, got := listOf(t, out, "machine_types")
	if len(rows) != 1 || rows[0]["guestCpus"] != float64(2) || rows[0]["memoryMb"] != float64(2048) ||
		rows[0]["maximumPersistentDisks"] != float64(128) || rows[0]["isSharedCpu"] != true ||
		rows[0]["deprecated"].(map[string]any)["state"] != "DEPRECATED" || got["warning"] == nil || got["has_more"] != true {
		t.Errorf("out = %s", out)
	}
	if strings.Contains(out, "imageSpaceGb") {
		t.Errorf("deprecated member returned: %s", out)
	}
	out, err = e.invoke(machineTypesListID, `{"project_id":"my-project-123","zone":"us-central1-a","max_results":1,"filter":"name eq e2-.*","cursor":"`+
		got["next_cursor"].(string)+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	if rows, got := listOf(t, out, "machine_types"); len(rows) != 1 || rows[0]["name"] != "e2-small" || got["has_more"] != false {
		t.Errorf("page 2 = %s", out)
	}
	if q := queriesOf(e); q[0].Get("filter") != "name eq e2-.*" || q[0].Get("maxResults") != "1" || q[1].Get("pageToken") != "p2" {
		t.Errorf("queries = %v", q)
	}
	e.fake.routes[machineTypePath] = okReply(`{}`)
	e.fake.hook = nil
	if out, err := e.invoke(machineTypesListID, `{"project_id":"my-project-123","zone":"us-central1-a"}`); err != nil ||
		out != `{"has_more":false}` {
		t.Errorf("empty list: %s, %v", out, err)
	}
	if q := queriesOf(e); q[len(q)-1].Has("maxResults") {
		t.Errorf("machine types ask for a size that Google did not need: %v", q[len(q)-1])
	}
}

func TestFilterReachesOnlyTheFilterParameter(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes[instancesPath] = okReply(`{}`)
	filter := `name = "a&pageToken=evil&maxResults=1#x"`
	arguments, _ := json.Marshal(map[string]any{"project_id": "my-project-123", "zone": "us-central1-a", "filter": filter})
	if _, err := e.invoke(instancesListID, string(arguments)); err != nil {
		t.Fatal(err)
	}
	q := queriesOf(e)[0]
	if q.Get("filter") != filter || q.Has("pageToken") || q.Get("maxResults") != "100" {
		t.Errorf("query = %v", q)
	}
	if got := len(q); got != 2 {
		t.Errorf("query has %d parameters: %v", got, q)
	}
}

func TestComputeToolsNeedOnlyTheComputeReadScopeAndBelongToRead(t *testing.T) {
	ids := []string{instancesListID, instancesAggregatedListID, instancesGetID, machineTypesListID}
	for _, id := range ids {
		if got := tokenMetadata.Scopes[id]; len(got) != 1 || got[0] != scopeComputeReadOnly {
			t.Errorf("%s scopes = %v", id, got)
		}
		if !slices.Contains(readTools, id) {
			t.Errorf("%s is not in the profile read", id)
		}
	}
	if scopeComputeReadOnly != "https://www.googleapis.com/auth/compute.readonly" {
		t.Errorf("scope = %s", scopeComputeReadOnly)
	}
}

func TestComputeToolsAreCompleteReadTools(t *testing.T) {
	e := newEnvironment(t, nil)
	for _, id := range []string{instancesListID, instancesAggregatedListID, instancesGetID, machineTypesListID} {
		d, _, found := e.reg.Lookup(id)
		if !found {
			t.Fatalf("%s is not registered", id)
		}
		if d.Risk != readRisk {
			t.Errorf("%s risk = %+v", id, d.Risk)
		}
		if !strings.Contains(d.Description, "untrusted") {
			t.Errorf("%s description says nothing of untrusted data", id)
		}
	}
	for _, id := range []string{instancesListID, instancesAggregatedListID, instancesGetID} {
		d, _, _ := e.reg.Lookup(id)
		if !strings.Contains(d.Description, "metadata.keys") || !strings.Contains(d.Description, "never a value") {
			t.Errorf("%s description does not name the metadata rule", id)
		}
	}
}

func TestComputeSecretCanariesStayOutOfTheInvokeResultOfErrors(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes[instancesPath] = reply{status: 500, body: fmt.Sprintf(`{"error":{"message":%q}}`, detailCanary)}
	_, err := e.invoke(instancesListID, `{"project_id":"my-project-123","zone":"us-central1-a"}`)
	if err == nil || strings.Contains(err.Error(), detailCanary) {
		t.Errorf("err = %v", err)
	}
}
