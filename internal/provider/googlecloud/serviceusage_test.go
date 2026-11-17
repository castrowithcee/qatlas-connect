package googlecloud

import (
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const serviceBody = `{"name":"projects/415104041262/services/compute.googleapis.com","parent":"projects/415104041262",` +
	`"state":"ENABLED","config":{"name":"compute.googleapis.com","title":"Compute Engine API","unknown":1,` +
	`"documentation":{"summary":"s","rules":[{"selector":"a","description":"d","deprecationDescription":"old"}]},` +
	`"authentication":{"rules":[{"selector":"a","requirements":[{"providerId":"p","audiences":"x"}]}]},` +
	`"endpoints":[{"name":"compute.googleapis.com","allowCors":true}],` +
	`"quota":{"limits":[{"name":"l","unit":"1/min"}]}}}`

func serviceRow(name string) string {
	return `{"name":"projects/415104041262/services/` + name + `","state":"ENABLED"}`
}

func apiPaths(e *environment) []string {
	var out []string
	for _, r := range e.fake.api() {
		u, _ := url.Parse(r.url)
		out = append(out, u.Host+u.Path)
	}
	return out
}

func TestServicesListFollowsThreePagesWithFilter(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.hook = func(_ int, req *http.Request) *reply {
		if req.URL.Path != "/v1/projects/my-project-123/services" {
			return nil
		}
		switch req.URL.Query().Get("pageToken") {
		case "":
			return &reply{status: 200, body: `{"services":[` + serviceRow("a.googleapis.com") + `],"nextPageToken":"p2"}`}
		case "p2":
			return &reply{status: 200, body: `{"services":[` + serviceRow("b.googleapis.com") + `],"nextPageToken":"p3"}`}
		}
		return &reply{status: 200, body: `{"services":[` + serviceRow("c.googleapis.com") + `]}`}
	}
	var names []string
	cursor := ""
	for page := 1; page <= 3; page++ {
		arguments := `{"project_id":"my-project-123","filter":"state:ENABLED","page_size":1`
		if cursor != "" {
			arguments += `,"cursor":"` + cursor + `"`
		}
		out, err := e.invoke(servicesListID, arguments+`}`)
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		rows, got := listOf(t, out, "services")
		for _, row := range rows {
			names = append(names, row["name"].(string))
		}
		cursor, _ = got["next_cursor"].(string)
		if got["has_more"].(bool) != (page < 3) {
			t.Fatalf("page %d: has_more = %v", page, got["has_more"])
		}
	}
	if len(names) != 3 || !strings.HasSuffix(names[2], "/c.googleapis.com") {
		t.Errorf("names = %v", names)
	}
	for i, q := range queriesOf(e) {
		if q.Get("filter") != "state:ENABLED" || q.Get("pageSize") != "1" {
			t.Errorf("request %d: %v", i, q)
		}
	}
	if paths := apiPaths(e); paths[0] != "serviceusage.googleapis.com/v1/projects/my-project-123/services" {
		t.Errorf("path = %v", paths)
	}
}

func TestServicesListRefusesInvalidInputBeforeIO(t *testing.T) {
	for name, arguments := range map[string]string{
		"filter":    `{"project_id":"my-project-123","filter":"state:ENABLED OR name:x"}`,
		"page size": `{"project_id":"my-project-123","page_size":101}`,
		"project":   `{"project_id":"projects/x"}`,
		"cursor":    `{"project_id":"my-project-123","cursor":"bogus"}`,
	} {
		e := newEnvironment(t, nil)
		if _, err := e.invoke(servicesListID, arguments); err == nil {
			t.Errorf("%s: want a refusal", name)
		}
		if len(e.fake.api()) != 0 || e.fake.tokenRequests() != 0 || e.lookups != 0 {
			t.Errorf("%s: api=%d tokens=%d lookups=%d", name, len(e.fake.api()), e.fake.tokenRequests(), e.lookups)
		}
	}
	// The handler itself refuses a filter the schema would not have let through.
	e := newEnvironment(t, nil)
	_, err := handleServicesList(t.Context(), e.resolved(), e.secrets(), e.red,
		[]byte(`{"project_id":"my-project-123","filter":"name:x"}`))
	if !isInvalidRequest(err) || len(e.fake.api()) != 0 {
		t.Errorf("err = %v", err)
	}
}

func TestServicesGetPrunesDeprecatedMembersAndBuildsTheName(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes["/v1/projects/my-project-123/services/compute.googleapis.com"] = okReply(serviceBody)
	out, err := e.invoke(servicesGetID, `{"project_id":"my-project-123","service":"compute.googleapis.com"}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"title":"Compute Engine API"`, `"summary":"s"`, `"allowCors":true`, `"unit":"1/min"`,
		`"state":"ENABLED"`, `"providerId":"p"`} {
		if !strings.Contains(out, want) {
			t.Errorf("result lacks %s: %s", want, out)
		}
	}
	for _, absent := range []string{"deprecationDescription", "audiences", "unknown"} {
		if strings.Contains(out, absent) {
			t.Errorf("result holds %s: %s", absent, out)
		}
	}
}

func TestServicesGetRejectsInvalidServiceNames(t *testing.T) {
	for _, name := range []string{"../x.googleapis.com", "nodot", "a/b.googleapis.com", "X.googleapis.com",
		"a.googleapis.com?x=1", "-a.googleapis.com", "projects/1/services/a.b", ""} {
		e := newEnvironment(t, nil)
		_, err := e.invoke(servicesGetID, `{"project_id":"my-project-123","service":"`+name+`"}`)
		if err == nil {
			t.Errorf("%q: want a refusal", name)
		}
		if len(e.fake.api()) != 0 || e.lookups != 0 {
			t.Errorf("%q: reached I/O", name)
		}
	}
}

func TestServicesBatchGetLimitsAndRequest(t *testing.T) {
	var names []string
	for i := range 21 {
		names = append(names, `"s`+string(rune('a'+i))+`.googleapis.com"`)
	}
	e := newEnvironment(t, nil)
	if _, err := e.invoke(servicesBatchGetID, `{"project_id":"my-project-123","names":[`+strings.Join(names, ",")+`]}`); err == nil {
		t.Error("21 names accepted")
	}
	if _, err := handleServicesBatchGet(t.Context(), e.resolved(), e.secrets(), e.red,
		[]byte(`{"project_id":"my-project-123","names":[`+strings.Join(names, ",")+`]}`)); !isInvalidRequest(err) {
		t.Errorf("handler err = %v", err)
	}
	if _, err := e.invoke(servicesBatchGetID, `{"project_id":"my-project-123","names":["a.googleapis.com","a.googleapis.com"]}`); err == nil {
		t.Error("duplicate names accepted")
	}
	if len(e.fake.api()) != 0 || e.lookups != 0 {
		t.Errorf("refusals reached I/O: api=%d lookups=%d", len(e.fake.api()), e.lookups)
	}
	e.fake.routes["/v1/projects/my-project-123/services:batchGet"] = okReply(`{"services":[` +
		serviceRow("compute.googleapis.com") + `,` + serviceRow("storage.googleapis.com") + `]}`)
	out, err := e.invoke(servicesBatchGetID, `{"project_id":"my-project-123","names":["compute.googleapis.com","storage.googleapis.com"]}`)
	if err != nil {
		t.Fatal(err)
	}
	rows, _ := listOf(t, out, "services")
	if len(rows) != 2 {
		t.Errorf("rows = %v", rows)
	}
	got := queriesOf(e)[0]["names"]
	want := []string{"projects/my-project-123/services/compute.googleapis.com", "projects/my-project-123/services/storage.googleapis.com"}
	if !slices.Equal(got, want) {
		t.Errorf("names = %v", got)
	}
}

func TestServicesRefuseProjectsOutsideTheTargetsBeforeIO(t *testing.T) {
	calls := map[string]string{
		servicesListID:     `{"project_id":"other-project-1"}`,
		servicesGetID:      `{"project_id":"other-project-1","service":"a.googleapis.com"}`,
		servicesBatchGetID: `{"project_id":"other-project-1","names":["a.googleapis.com"]}`,
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
	// A folder target is proven by reading the project and its parent chain before the services are read.
	e := newEnvironment(t, []string{"folders/2001"})
	e.fake.routes["/v3/projects/my-project-123"] = okReply(projectBody)
	e.fake.routes["/v1/projects/my-project-123/services"] = okReply(`{"services":[` + serviceRow("a.googleapis.com") + `]}`)
	if _, err := e.invoke(servicesListID, `{"project_id":"my-project-123"}`); err != nil {
		t.Fatal(err)
	}
	if paths := apiPaths(e); len(paths) != 2 || !strings.HasPrefix(paths[0], "cloudresourcemanager") {
		t.Errorf("paths = %v", paths)
	}
	e = newEnvironment(t, []string{"folders/9999"})
	e.fake.routes["/v3/projects/my-project-123"] = okReply(projectBody)
	e.fake.routes["/v3/folders/2001"] = okReply(`{"name":"folders/2001","parent":"organizations/777"}`)
	if _, err := e.invoke(servicesListID, `{"project_id":"my-project-123"}`); !isInvalidRequest(err) {
		t.Errorf("err = %v, want a refusal", err)
	}
	for _, p := range apiPaths(e) {
		if strings.HasPrefix(p, "serviceusage") {
			t.Errorf("services were read for a refused project")
		}
	}
}

func TestServicesFailures(t *testing.T) {
	cases := []struct {
		name  string
		reply reply
		want  provider.Class
	}{
		{"required API disabled", reply{status: 403, body: errorObject(403, "PERMISSION_DENIED", "SERVICE_DISABLED")}, provider.ClassPermission},
		{"permission denied", reply{status: 403, body: errorObject(403, "PERMISSION_DENIED", "")}, provider.ClassPermission},
		{"not found", reply{status: 404, body: errorObject(404, "NOT_FOUND", "")}, provider.ClassNotFound},
		{"invalid row", okReply(`{"services":[{"name":"folders/1"}]}`), provider.ClassInvalidResponse},
	}
	for _, c := range cases {
		for id, arguments := range map[string]string{
			servicesListID:     `{"project_id":"my-project-123"}`,
			servicesGetID:      `{"project_id":"my-project-123","service":"a.googleapis.com"}`,
			servicesBatchGetID: `{"project_id":"my-project-123","names":["a.googleapis.com"]}`,
		} {
			e := newEnvironment(t, nil)
			e.fake.hook = func(_ int, req *http.Request) *reply { return &c.reply }
			_, err := e.invoke(id, arguments)
			want := c.want
			if c.name == "invalid row" && id == servicesGetID {
				continue
			}
			if classOf(err) != want {
				t.Errorf("%s %s: err = %v (%s), want %s", c.name, id, err, classOf(err), want)
			}
			if err != nil && strings.Contains(err.Error(), detailCanary) {
				t.Errorf("%s %s: error echoes provider text", c.name, id)
			}
		}
	}
	e := newEnvironment(t, nil)
	e.fake.routes["/v1/projects/my-project-123/services"] = reply{status: 403, body: errorObject(403, "PERMISSION_DENIED", "SERVICE_DISABLED")}
	_, err := e.invoke(servicesListID, `{"project_id":"my-project-123"}`)
	if err == nil || !strings.Contains(err.Error(), "Service Usage") {
		t.Errorf("err = %v, want a setup message that names Service Usage", err)
	}
}

func TestServicesToolsNeedOnlyTheReadOnlyScope(t *testing.T) {
	for _, id := range []string{servicesListID, servicesGetID, servicesBatchGetID} {
		if got := tokenMetadata.Scopes[id]; len(got) != 1 || got[0] != scopeReadOnly {
			t.Errorf("%s scopes = %v", id, got)
		}
		if !slices.Contains(readTools, id) {
			t.Errorf("%s is not in the profile read", id)
		}
	}
}
