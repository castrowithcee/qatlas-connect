package googlecloud

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const (
	zonesPath        = "/compute/v1/projects/my-project-123/zones"
	regionsPath      = "/compute/v1/projects/my-project-123/regions"
	zoneOpsPath      = "/compute/v1/projects/my-project-123/zones/us-central1-a/operations"
	regionOpsPath    = "/compute/v1/projects/my-project-123/regions/us-central1/operations"
	globalOpsPath    = "/compute/v1/projects/my-project-123/global/operations"
	operationUser    = "ada@example.com"
	operationMessage = "operation-message-canary-5e2a"
)

func zoneBody(name string) string {
	return `{"kind":"compute#zone","id":"2000","creationTimestamp":"2026-01-01T00:00:00.000-08:00","name":"` + name +
		`","description":"` + name + `","status":"UP","region":"https://www.googleapis.com/compute/v1/projects/my-project-123/regions/us-central1",` +
		`"availableCpuPlatforms":["Intel Broadwell","AMD Rome"],"supportsPzs":true,"deprecated":{"state":"DEPRECATED"},"unknownMember":"x"}`
}

func regionBody(name string, quotas int) string {
	var list []string
	for i := 0; i < quotas; i++ {
		list = append(list, fmt.Sprintf(`{"metric":"METRIC_%d","limit":%d,"usage":2.5,"owner":"Compute"}`, i, 100+i))
	}
	return `{"kind":"compute#region","id":"1000","name":"` + name + `","status":"UP",` +
		`"zones":["https://www.googleapis.com/compute/v1/projects/my-project-123/zones/us-central1-a"],` +
		`"quotas":[` + strings.Join(list, ",") + `],"supportsPzs":false,"unknownMember":"x"}`
}

func operationBody(name, status, extra string) string {
	return `{"kind":"compute#operation","id":"9000","name":"` + name + `","operationType":"insert",` +
		`"targetLink":"https://www.googleapis.com/compute/v1/projects/my-project-123/zones/us-central1-a/instances/vm-a",` +
		`"targetId":"4100000000000000001","status":"` + status + `","progress":50,"user":"` + operationUser + `",` +
		`"insertTime":"2026-03-04T05:06:07.000-08:00","zone":"https://www.googleapis.com/compute/v1/projects/my-project-123/zones/us-central1-a",` +
		`"creationTimestamp":"deprecated","clientOperationId":"client-1"` + extra + `}`
}

func TestZonesListFollowsTwoPages(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.hook = func(_ int, req *http.Request) *reply {
		if req.URL.Path != zonesPath {
			return nil
		}
		if req.URL.Query().Get("pageToken") == "" {
			return &reply{status: 200, body: `{"items":[` + zoneBody("us-central1-a") + `],"nextPageToken":"p2"}`}
		}
		return &reply{status: 200, body: `{"items":[` + zoneBody("us-central1-b") + `]}`}
	}
	out, err := e.invoke(zonesListID, `{"project_id":"my-project-123","max_results":1,"filter":"status = UP","order_by":"name",`+
		`"return_partial_success":true}`)
	if err != nil {
		t.Fatal(err)
	}
	rows, got := listOf(t, out, "zones")
	if len(rows) != 1 || rows[0]["name"] != "us-central1-a" || rows[0]["supportsPzs"] != true ||
		len(rows[0]["availableCpuPlatforms"].([]any)) != 2 || rows[0]["deprecated"] == nil ||
		rows[0]["unknownMember"] != nil || got["has_more"] != true {
		t.Fatalf("page 1 = %s", out)
	}
	out, err = e.invoke(zonesListID, `{"project_id":"my-project-123","max_results":1,"filter":"status = UP","order_by":"name",`+
		`"return_partial_success":true,"cursor":"`+got["next_cursor"].(string)+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	if rows, got := listOf(t, out, "zones"); len(rows) != 1 || rows[0]["name"] != "us-central1-b" || got["has_more"] != false {
		t.Errorf("page 2 = %s", out)
	}
	q := queriesOf(e)
	if q[0].Get("filter") != "status = UP" || q[0].Get("maxResults") != "1" || q[0].Get("orderBy") != "name" ||
		q[0].Get("returnPartialSuccess") != "true" || q[1].Get("pageToken") != "p2" {
		t.Errorf("queries = %v", q)
	}
	for _, r := range e.fake.api() {
		if r.method != http.MethodGet || r.body != "" || !strings.HasPrefix(r.url, "https://compute.googleapis.com"+zonesPath) {
			t.Errorf("request = %+v", r)
		}
	}
}

func TestRegionsListKeepsQuotasAndWarning(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes[regionsPath] = okReply(`{"items":[` + regionBody("us-central1", 150) + `],` +
		`"warning":{"code":"UNREACHABLE","message":"m"}}`)
	out, err := e.invoke(regionsListID, `{"project_id":"my-project-123"}`)
	if err != nil {
		t.Fatal(err)
	}
	rows, got := listOf(t, out, "regions")
	if len(rows) != 1 || got["warning"] == nil || got["has_more"] != false {
		t.Fatalf("out = %s", out)
	}
	quotas := rows[0]["quotas"].([]any)
	first := quotas[0].(map[string]any)
	if len(quotas) != 150 || first["metric"] != "METRIC_0" || first["limit"] != float64(100) || first["usage"] != 2.5 ||
		first["owner"] != "Compute" || rows[0]["unknownMember"] != nil || len(rows[0]["zones"].([]any)) != 1 {
		t.Errorf("region = %v", rows[0])
	}
	if q := queriesOf(e)[0]; q.Get("maxResults") != "25" {
		t.Errorf("default page size = %q", q.Get("maxResults"))
	}
}

func TestComputeOperationsGetByScope(t *testing.T) {
	for _, c := range []struct{ scope, arguments, path string }{
		{"zone", `"scope":"zone","zone":"us-central1-a"`, zoneOpsPath},
		{"region", `"scope":"region","region":"us-central1"`, regionOpsPath},
		{"global", `"scope":"global"`, globalOpsPath},
	} {
		e := newEnvironment(t, nil)
		e.fake.routes[c.path+"/operation-1"] = okReply(strings.Replace(operationBody("operation-1", "RUNNING", ""),
			`"zone":"https://www.googleapis.com/compute/v1/projects/my-project-123/zones/us-central1-a",`, "", 1))
		out, err := e.invoke(computeOperationsGetID, `{"project_id":"my-project-123",`+c.arguments+`,"operation":"operation-1"}`)
		if err != nil {
			t.Fatalf("%s: %v", c.scope, err)
		}
		if !strings.Contains(out, `"status":"RUNNING"`) || !strings.Contains(out, `"user":"`+operationUser+`"`) ||
			!strings.Contains(out, `"progress":50`) || strings.Contains(out, "creationTimestamp") {
			t.Errorf("%s: out = %s", c.scope, out)
		}
		if paths := apiPaths(e); len(paths) != 1 || paths[0] != "compute.googleapis.com"+c.path+"/operation-1" {
			t.Errorf("%s: paths = %v", c.scope, paths)
		}
	}
}

func TestComputeOperationsGetDoneWithoutError(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes[zoneOpsPath+"/operation-2"] = okReply(operationBody("operation-2", "DONE",
		`,"progress":100,"endTime":"2026-03-04T05:07:07.000-08:00","warnings":[{"code":"NO_RESULTS_ON_PAGE","message":"m"}]`))
	out, err := e.invoke(computeOperationsGetID, `{"project_id":"my-project-123","scope":"zone","zone":"us-central1-a","operation":"operation-2"}`)
	if err != nil || !strings.Contains(out, `"status":"DONE"`) || !strings.Contains(out, `"warnings"`) ||
		!strings.Contains(out, `"endTime"`) {
		t.Errorf("out = %s, err = %v", out, err)
	}
}

func TestComputeOperationsGetDoneWithErrorIsAProviderError(t *testing.T) {
	for name, c := range map[string]struct {
		status int
		want   provider.Class
	}{
		"quota":        {403, provider.ClassProviderError},
		"not found":    {404, provider.ClassNotFound},
		"rate limited": {429, provider.ClassRateLimited},
		"other":        {400, provider.ClassProviderError},
		"none":         {0, provider.ClassProviderError},
	} {
		e := newEnvironment(t, nil)
		extra := `,"error":{"errors":[{"code":"QUOTA_EXCEEDED","location":"loc","message":"` + operationMessage + `"}]}`
		if c.status != 0 {
			extra += fmt.Sprintf(`,"httpErrorStatusCode":%d`, c.status)
		}
		e.fake.routes[zoneOpsPath+"/operation-3"] = okReply(operationBody("operation-3", "DONE", extra))
		_, err := e.invoke(computeOperationsGetID, `{"project_id":"my-project-123","scope":"zone","zone":"us-central1-a","operation":"operation-3"}`)
		var failed *provider.Error
		if !errors.As(err, &failed) || failed.Class != c.want {
			t.Fatalf("%s: err = %v", name, err)
		}
		if strings.Contains(failed.Message, operationMessage) || strings.Contains(failed.Message, "QUOTA_EXCEEDED") {
			t.Errorf("%s: message holds provider text: %s", name, failed.Message)
		}
		if failed.Original == nil || len(failed.Original.Entries) != 1 || failed.Original.Entries[0].Code != "QUOTA_EXCEEDED" ||
			failed.Original.Entries[0].Path != "loc" || failed.Original.Entries[0].Message != operationMessage {
			t.Errorf("%s: original = %+v", name, failed.Original)
		}
	}
	// A running operation with an error object is not yet a failure.
	e := newEnvironment(t, nil)
	e.fake.routes[zoneOpsPath+"/operation-4"] = okReply(operationBody("operation-4", "RUNNING",
		`,"error":{"errors":[{"code":"X"}]}`))
	if _, err := e.invoke(computeOperationsGetID, `{"project_id":"my-project-123","scope":"zone","zone":"us-central1-a","operation":"operation-4"}`); err != nil {
		t.Errorf("running: %v", err)
	}
}

func TestComputeOperationsGetUnknownOperationIs404(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes[zoneOpsPath+"/operation-9"] = reply{status: 404, body: `{"error":{"code":404,"message":"` + detailCanary +
		`","errors":[{"message":"m","domain":"global","reason":"notFound"}]}}`}
	_, err := e.invoke(computeOperationsGetID, `{"project_id":"my-project-123","scope":"zone","zone":"us-central1-a","operation":"operation-9"}`)
	if classOf(err) != provider.ClassNotFound || strings.Contains(err.Error(), detailCanary) {
		t.Errorf("err = %v (%s)", err, classOf(err))
	}
}

func TestComputeOperationsGetRefusesAnAnswerOfAnotherScope(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes[zoneOpsPath+"/operation-5"] = okReply(strings.Replace(operationBody("operation-5", "RUNNING", ""),
		"us-central1-a\",\"creationTimestamp", "us-central1-b\",\"creationTimestamp", 1))
	_, err := e.invoke(computeOperationsGetID, `{"project_id":"my-project-123","scope":"zone","zone":"us-central1-a","operation":"operation-5"}`)
	if classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("err = %v (%s)", err, classOf(err))
	}
}

func TestComputeOperationsListByScopeWithTwoPages(t *testing.T) {
	for _, c := range []struct{ scope, arguments, path string }{
		{"zone", `"scope":"zone","zone":"us-central1-a"`, zoneOpsPath},
		{"region", `"scope":"region","region":"us-central1"`, regionOpsPath},
		{"global", `"scope":"global"`, globalOpsPath},
	} {
		e := newEnvironment(t, nil)
		e.fake.hook = func(_ int, req *http.Request) *reply {
			if req.URL.Path != c.path {
				return nil
			}
			if req.URL.Query().Get("pageToken") == "" {
				return &reply{status: 200, body: `{"items":[` + strings.Replace(operationBody("operation-a", "DONE",
					`,"error":{"errors":[{"code":"C","message":"m"}]}`),
					`"zone":"https://www.googleapis.com/compute/v1/projects/my-project-123/zones/us-central1-a",`, "", 1) +
					`],"nextPageToken":"p2"}`}
			}
			return &reply{status: 200, body: `{"items":[` + strings.Replace(operationBody("operation-b", "RUNNING", ""),
				`"zone":"https://www.googleapis.com/compute/v1/projects/my-project-123/zones/us-central1-a",`, "", 1) + `]}`}
		}
		base := `{"project_id":"my-project-123",` + c.arguments + `,"filter":"status != DONE","max_results":1`
		out, err := e.invoke(computeOperationsListID, base+`}`)
		if err != nil {
			t.Fatalf("%s: %v", c.scope, err)
		}
		rows, got := listOf(t, out, "operations")
		if len(rows) != 1 || rows[0]["name"] != "operation-a" || rows[0]["error"] == nil || got["has_more"] != true {
			t.Fatalf("%s page 1 = %s", c.scope, out)
		}
		out, err = e.invoke(computeOperationsListID, base+`,"cursor":"`+got["next_cursor"].(string)+`"}`)
		if err != nil {
			t.Fatalf("%s: %v", c.scope, err)
		}
		if rows, got := listOf(t, out, "operations"); len(rows) != 1 || rows[0]["name"] != "operation-b" || got["has_more"] != false {
			t.Errorf("%s page 2 = %s", c.scope, out)
		}
		if q := queriesOf(e); q[0].Get("filter") != "status != DONE" || q[1].Get("pageToken") != "p2" {
			t.Errorf("%s queries = %v", c.scope, q)
		}
	}
}

func TestComputeOperationsListCursorIsBoundToTheScope(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes[zoneOpsPath] = okReply(`{"items":[],"nextPageToken":"p2"}`)
	out, err := e.invoke(computeOperationsListID, `{"project_id":"my-project-123","scope":"zone","zone":"us-central1-a"}`)
	if err != nil {
		t.Fatal(err)
	}
	_, got := listOf(t, out, "operations")
	cursor := got["next_cursor"].(string)
	before := len(e.fake.api())
	for name, arguments := range map[string]string{
		"other zone":   `{"project_id":"my-project-123","scope":"zone","zone":"us-central1-b","cursor":"` + cursor + `"}`,
		"other region": `{"project_id":"my-project-123","scope":"region","region":"us-central1","cursor":"` + cursor + `"}`,
		"global":       `{"project_id":"my-project-123","scope":"global","cursor":"` + cursor + `"}`,
	} {
		if _, err := e.invoke(computeOperationsListID, arguments); !isInvalidRequest(err) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if len(e.fake.api()) != before {
		t.Error("a refused cursor reached Google")
	}
}

func TestComputeOperationsRefuseBadScopeCombinationsBeforeIO(t *testing.T) {
	for name, c := range map[string]struct{ tool, arguments string }{
		"get zone without zone":      {computeOperationsGetID, `{"project_id":"my-project-123","scope":"zone","operation":"op-1"}`},
		"get region without region":  {computeOperationsGetID, `{"project_id":"my-project-123","scope":"region","operation":"op-1"}`},
		"get zone with region":       {computeOperationsGetID, `{"project_id":"my-project-123","scope":"zone","zone":"us-central1-a","region":"us-central1","operation":"op-1"}`},
		"get region with zone":       {computeOperationsGetID, `{"project_id":"my-project-123","scope":"region","zone":"us-central1-a","region":"us-central1","operation":"op-1"}`},
		"get global with zone":       {computeOperationsGetID, `{"project_id":"my-project-123","scope":"global","zone":"us-central1-a","operation":"op-1"}`},
		"get global with region":     {computeOperationsGetID, `{"project_id":"my-project-123","scope":"global","region":"us-central1","operation":"op-1"}`},
		"get unknown scope":          {computeOperationsGetID, `{"project_id":"my-project-123","scope":"organization","operation":"op-1"}`},
		"get missing scope":          {computeOperationsGetID, `{"project_id":"my-project-123","operation":"op-1"}`},
		"get operation path":         {computeOperationsGetID, `{"project_id":"my-project-123","scope":"global","operation":"a/b"}`},
		"get operation traversal":    {computeOperationsGetID, `{"project_id":"my-project-123","scope":"global","operation":".."}`},
		"get operation query":        {computeOperationsGetID, `{"project_id":"my-project-123","scope":"global","operation":"a?b=1"}`},
		"get operation upper":        {computeOperationsGetID, `{"project_id":"my-project-123","scope":"global","operation":"Op-1"}`},
		"get operation zero id":      {computeOperationsGetID, `{"project_id":"my-project-123","scope":"global","operation":"0"}`},
		"get zone path":              {computeOperationsGetID, `{"project_id":"my-project-123","scope":"zone","zone":"a/b","operation":"op-1"}`},
		"get region path":            {computeOperationsGetID, `{"project_id":"my-project-123","scope":"region","region":"../x","operation":"op-1"}`},
		"get project path":           {computeOperationsGetID, `{"project_id":"projects/x","scope":"global","operation":"op-1"}`},
		"get unknown argument":       {computeOperationsGetID, `{"project_id":"my-project-123","scope":"global","operation":"op-1","path":"/x"}`},
		"list zone without zone":     {computeOperationsListID, `{"project_id":"my-project-123","scope":"zone"}`},
		"list region without region": {computeOperationsListID, `{"project_id":"my-project-123","scope":"region"}`},
		"list global with zone":      {computeOperationsListID, `{"project_id":"my-project-123","scope":"global","zone":"us-central1-a"}`},
		"list region with zone":      {computeOperationsListID, `{"project_id":"my-project-123","scope":"region","region":"us-central1","zone":"us-central1-a"}`},
		"list zone with region":      {computeOperationsListID, `{"project_id":"my-project-123","scope":"zone","zone":"us-central1-a","region":"us-central1"}`},
		"list unknown scope":         {computeOperationsListID, `{"project_id":"my-project-123","scope":"all"}`},
		"list max results":           {computeOperationsListID, `{"project_id":"my-project-123","scope":"global","max_results":501}`},
		"list order by":              {computeOperationsListID, `{"project_id":"my-project-123","scope":"global","order_by":"x; y"}`},
		"list filter control":        {computeOperationsListID, `{"project_id":"my-project-123","scope":"global","filter":"a\nb"}`},
		"list cursor":                {computeOperationsListID, `{"project_id":"my-project-123","scope":"global","cursor":"bogus"}`},
		"list all scopes":            {computeOperationsListID, `{"project_id":"my-project-123","scope":"global","include_all_scopes":true}`},
		"list region path":           {computeOperationsListID, `{"project_id":"my-project-123","scope":"region","region":"regions/us-central1"}`},
		"zones region":               {zonesListID, `{"project_id":"my-project-123","region":"us-central1"}`},
		"zones zone":                 {zonesListID, `{"project_id":"my-project-123","zone":"us-central1-a"}`},
		"zones max results":          {zonesListID, `{"project_id":"my-project-123","max_results":0}`},
		"zones project path":         {zonesListID, `{"project_id":"../x"}`},
		"zones cursor":               {zonesListID, `{"project_id":"my-project-123","cursor":"bogus"}`},
		"regions max results":        {regionsListID, `{"project_id":"my-project-123","max_results":501}`},
		"regions all scopes":         {regionsListID, `{"project_id":"my-project-123","include_all_scopes":true}`},
		"regions filter long":        {regionsListID, `{"project_id":"my-project-123","filter":"` + strings.Repeat("a", 1025) + `"}`},
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
		"get": func() error {
			_, err := handleComputeOperationsGet(t.Context(), e.resolved(), e.secrets(), e.red,
				[]byte(`{"project_id":"my-project-123","scope":"zone","operation":"op-1"}`))
			return err
		},
		"list": func() error {
			_, err := handleComputeOperationsList(t.Context(), e.resolved(), e.secrets(), e.red,
				[]byte(`{"project_id":"my-project-123","scope":"global","region":"us-central1"}`))
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

func TestZonesRegionsAndOperationsRefuseProjectsOutsideTheTargetsBeforeIO(t *testing.T) {
	for id, arguments := range map[string]string{
		zonesListID:             `{"project_id":"other-project-1"}`,
		regionsListID:           `{"project_id":"other-project-1"}`,
		computeOperationsGetID:  `{"project_id":"other-project-1","scope":"global","operation":"op-1"}`,
		computeOperationsListID: `{"project_id":"other-project-1","scope":"region","region":"us-central1"}`,
	} {
		e := newEnvironment(t, []string{"projects/my-project-123"})
		_, err := e.invoke(id, arguments)
		if !isInvalidRequest(err) || strings.Contains(err.Error(), "my-project-123") {
			t.Errorf("%s: err = %v", id, err)
		}
		if len(e.fake.api()) != 0 || e.fake.tokenRequests() != 0 || e.lookups != 0 {
			t.Errorf("%s: api=%d tokens=%d lookups=%d", id, len(e.fake.api()), e.fake.tokenRequests(), e.lookups)
		}
	}
}

func TestZonesRegionsAndOperationsFailures(t *testing.T) {
	cases := []struct {
		name  string
		reply reply
		want  provider.Class
	}{
		{"403 permission denied", reply{status: 403, body: errorObject(403, "PERMISSION_DENIED", "")}, provider.ClassPermission},
		{"404", reply{status: 404, body: errorObject(404, "NOT_FOUND", "")}, provider.ClassNotFound},
		{"429", reply{status: 429, body: errorObject(429, "RESOURCE_EXHAUSTED", "RATE_LIMIT_EXCEEDED")}, provider.ClassRateLimited},
		{"item is no object", okReply(`{"items":[1]}`), provider.ClassInvalidResponse},
		{"item without name", okReply(`{"items":[{"kind":"x"}]}`), provider.ClassInvalidResponse},
	}
	for _, c := range cases {
		for id, arguments := range map[string]string{
			zonesListID:             `{"project_id":"my-project-123"}`,
			regionsListID:           `{"project_id":"my-project-123"}`,
			computeOperationsGetID:  `{"project_id":"my-project-123","scope":"global","operation":"op-1"}`,
			computeOperationsListID: `{"project_id":"my-project-123","scope":"global"}`,
		} {
			e := newEnvironment(t, nil)
			e.fake.hook = func(_ int, req *http.Request) *reply { return &c.reply }
			_, err := e.invoke(id, arguments)
			if classOf(err) != c.want {
				t.Errorf("%s %s: err = %v (%s), want %s", c.name, id, err, classOf(err), c.want)
			}
		}
	}
}

func TestComputeOperationToolsAreReadToolsWithPersonalDataSensitivity(t *testing.T) {
	e := newEnvironment(t, nil)
	for _, id := range []string{zonesListID, regionsListID, computeOperationsGetID, computeOperationsListID} {
		if got := tokenMetadata.Scopes[id]; !slices.Equal(got, []string{scopeComputeReadOnly}) {
			t.Errorf("%s scopes = %v", id, got)
		}
		if !slices.Contains(readTools, id) {
			t.Errorf("%s is not in the profile read", id)
		}
		d, _, found := e.reg.Lookup(id)
		if !found {
			t.Fatalf("%s is not registered", id)
		}
		person := strings.Contains(id, "compute-operations")
		want := readRisk
		if person {
			want.DataSensitivity = computeOperationSensitivity
		}
		if d.Risk != want || !strings.Contains(d.Description, "untrusted") {
			t.Errorf("%s risk = %+v", id, d.Risk)
		}
		if person && (!strings.Contains(d.Description, "personal data") || !strings.Contains(d.Description, "email address")) {
			t.Errorf("%s description carries no warning about personal data", id)
		}
		if !person && d.Risk.DataSensitivity != dataSensitivity {
			t.Errorf("%s sensitivity = %s", id, d.Risk.DataSensitivity)
		}
	}
}
