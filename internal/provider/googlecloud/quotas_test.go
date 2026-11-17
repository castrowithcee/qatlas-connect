package googlecloud

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const quotaInfoBody = `{"name":"projects/415104041262/locations/global/services/compute.googleapis.com/quotaInfos/CpusPerProjectPerRegion",` +
	`"quotaId":"CpusPerProjectPerRegion","metric":"compute.googleapis.com/cpus","service":"compute.googleapis.com",` +
	`"isPrecise":true,"refreshInterval":"minute","containerType":"PROJECT","dimensions":["region"],` +
	`"metricDisplayName":"CPUs","quotaDisplayName":"CPUs per region","metricUnit":"1","isFixed":false,` +
	`"quotaIncreaseEligibility":{"isEligible":false,"ineligibilityReason":"NO_VALID_BILLING_ACCOUNT"},` +
	`"dimensionsInfos":[{"details":{"value":"24","rolloutInfo":{"ongoingRollout":true}},` +
	`"applicableLocations":["us-east1"],"dimensions":{"region":"us-east1"}}],"isConcurrent":false,` +
	`"serviceRequestQuotaUri":"https://console.cloud.google.com/iam-admin/quotas","unknownField":"x"}`

const preferenceBody = `{"name":"projects/415104041262/locations/global/quotaPreferences/my-config-for-us-east1",` +
	`"dimensions":{"region":"us-east1"},"quotaConfig":{"preferredValue":"100","grantedValue":"50",` +
	`"stateDetail":"partially approved","requestOrigin":"CLOUD_CONSOLE","traceId":"trace-1","annotations":{"k":"v"}},` +
	`"etag":"etag-1","createTime":"2026-01-02T03:04:05Z","updateTime":"2026-01-03T03:04:05Z",` +
	`"service":"compute.googleapis.com","quotaId":"CpusPerProjectPerRegion","reconciling":true,` +
	`"justification":"capacity for a test","contactEmail":"quota-contact@example.com"}`

func quotaInfoRow(id string) string {
	return `{"name":"projects/415104041262/locations/global/services/compute.googleapis.com/quotaInfos/` + id +
		`","quotaId":"` + id + `"}`
}

func preferenceRow(id string) string {
	return `{"name":"projects/415104041262/locations/global/quotaPreferences/` + id + `","quotaId":"q"}`
}

const (
	quotaInfosPath  = "/v1/projects/my-project-123/locations/global/services/compute.googleapis.com/quotaInfos"
	preferencesPath = "/v1/projects/my-project-123/locations/global/quotaPreferences"
)

func TestQuotaInfosListFollowsThreePages(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.hook = func(_ int, req *http.Request) *reply {
		if req.URL.Path != quotaInfosPath {
			return nil
		}
		switch req.URL.Query().Get("pageToken") {
		case "":
			return &reply{status: 200, body: `{"quotaInfos":[` + quotaInfoRow("A") + `],"nextPageToken":"p2"}`}
		case "p2":
			return &reply{status: 200, body: `{"quotaInfos":[` + quotaInfoRow("B") + `],"nextPageToken":"p3"}`}
		}
		return &reply{status: 200, body: `{"quotaInfos":[` + quotaInfoRow("C") + `]}`}
	}
	var ids []string
	cursor := ""
	for page := 1; page <= 3; page++ {
		arguments := `{"project_id":"my-project-123","service":"compute.googleapis.com","page_size":1`
		if cursor != "" {
			arguments += `,"cursor":"` + cursor + `"`
		}
		out, err := e.invoke(quotaInfosListID, arguments+`}`)
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		rows, got := listOf(t, out, "quotaInfos")
		for _, row := range rows {
			ids = append(ids, row["quotaId"].(string))
		}
		cursor, _ = got["next_cursor"].(string)
		if got["has_more"].(bool) != (page < 3) {
			t.Fatalf("page %d: has_more = %v", page, got["has_more"])
		}
	}
	if !slices.Equal(ids, []string{"A", "B", "C"}) {
		t.Errorf("ids = %v", ids)
	}
	if paths := apiPaths(e); paths[0] != "cloudquotas.googleapis.com"+quotaInfosPath {
		t.Errorf("path = %v", paths)
	}
	for i, q := range queriesOf(e) {
		if q.Get("pageSize") != "1" {
			t.Errorf("request %d: %v", i, q)
		}
	}
}

func TestQuotaInfosListEmptyAnswer(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes[quotaInfosPath] = okReply(`{}`)
	out, err := e.invoke(quotaInfosListID, `{"project_id":"my-project-123","service":"compute.googleapis.com"}`)
	if err != nil {
		t.Fatal(err)
	}
	rows, got := listOf(t, out, "quotaInfos")
	if len(rows) != 0 || got["has_more"].(bool) || got["next_cursor"] != nil {
		t.Errorf("out = %s", out)
	}
}

func TestQuotaCursorIsBoundToTheRequest(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.hook = func(_ int, req *http.Request) *reply {
		switch {
		case req.URL.Path == quotaInfosPath:
			return &reply{status: 200, body: `{"quotaInfos":[` + quotaInfoRow("A") + `],"nextPageToken":"p2"}`}
		case req.URL.Path == preferencesPath:
			return &reply{status: 200, body: `{"quotaPreferences":[` + preferenceRow("a") + `],"nextPageToken":"p2"}`}
		}
		return nil
	}
	out, err := e.invoke(quotaInfosListID, `{"project_id":"my-project-123","service":"compute.googleapis.com"}`)
	if err != nil {
		t.Fatal(err)
	}
	_, got := listOf(t, out, "quotaInfos")
	infoCursor := got["next_cursor"].(string)
	out, err = e.invoke(quotaPreferencesListID, `{"project_id":"my-project-123","reconciling":true}`)
	if err != nil {
		t.Fatal(err)
	}
	_, got = listOf(t, out, "quotaPreferences")
	prefCursor := got["next_cursor"].(string)
	before := len(e.fake.api())
	for name, call := range map[string][2]string{
		"altered":       {quotaInfosListID, `{"project_id":"my-project-123","service":"compute.googleapis.com","cursor":"` + infoCursor + `x"}`},
		"other service": {quotaInfosListID, `{"project_id":"my-project-123","service":"storage.googleapis.com","cursor":"` + infoCursor + `"}`},
		"other tool":    {quotaPreferencesListID, `{"project_id":"my-project-123","cursor":"` + infoCursor + `"}`},
		"other filter":  {quotaPreferencesListID, `{"project_id":"my-project-123","reconciling":false,"cursor":"` + prefCursor + `"}`},
		"other order":   {quotaPreferencesListID, `{"project_id":"my-project-123","reconciling":true,"order_by":["service"],"cursor":"` + prefCursor + `"}`},
		"bogus":         {quotaPreferencesListID, `{"project_id":"my-project-123","cursor":"bogus"}`},
	} {
		if _, err := e.invoke(call[0], call[1]); !isInvalidRequest(err) {
			t.Errorf("%s: err = %v, want a refusal", name, err)
		}
	}
	if len(e.fake.api()) != before {
		t.Errorf("refused cursors reached Google")
	}
	if _, err := e.invoke(quotaPreferencesListID, `{"project_id":"my-project-123","reconciling":true,"cursor":"`+prefCursor+`"}`); err != nil {
		t.Errorf("own cursor refused: %v", err)
	}
}

func TestQuotaInfosGetOutputsEveryDocumentedField(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes[quotaInfosPath+"/CPUS-per-project-region"] = okReply(quotaInfoBody)
	out, err := e.invoke(quotaInfosGetID, `{"project_id":"my-project-123","service":"compute.googleapis.com","quota_id":"CPUS-per-project-region"}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"quotaId":"CpusPerProjectPerRegion"`, `"metric":"compute.googleapis.com/cpus"`,
		`"service":"compute.googleapis.com"`, `"isPrecise":true`, `"refreshInterval":"minute"`,
		`"containerType":"PROJECT"`, `"dimensions":["region"]`, `"metricDisplayName":"CPUs"`,
		`"quotaDisplayName":"CPUs per region"`, `"metricUnit":"1"`, `"ineligibilityReason":"NO_VALID_BILLING_ACCOUNT"`,
		`"value":"24"`, `"ongoingRollout":true`, `"applicableLocations":["us-east1"]`, `"region":"us-east1"`,
		`"serviceRequestQuotaUri":"https://console.cloud.google.com/iam-admin/quotas"`} {
		if !strings.Contains(out, want) {
			t.Errorf("result lacks %s: %s", want, out)
		}
	}
	if strings.Contains(out, "unknownField") {
		t.Errorf("result holds an undocumented member: %s", out)
	}
}

func TestQuotaPreferencesGetOutputsEveryDocumentedField(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes[preferencesPath+"/my-config-for-us-east1"] = okReply(preferenceBody)
	out, err := e.invoke(quotaPreferencesGetID, `{"project_id":"my-project-123","preference_id":"my-config-for-us-east1"}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"preferredValue":"100"`, `"grantedValue":"50"`, `"stateDetail":"partially approved"`,
		`"requestOrigin":"CLOUD_CONSOLE"`, `"traceId":"trace-1"`, `"annotations":{"k":"v"}`, `"etag":"etag-1"`,
		`"createTime":"2026-01-02T03:04:05Z"`, `"updateTime":"2026-01-03T03:04:05Z"`, `"service":"compute.googleapis.com"`,
		`"quotaId":"CpusPerProjectPerRegion"`, `"reconciling":true`, `"justification":"capacity for a test"`,
		`"contactEmail":"quota-contact@example.com"`, `"dimensions":{"region":"us-east1"}`} {
		if !strings.Contains(out, want) {
			t.Errorf("result lacks %s: %s", want, out)
		}
	}
}

func TestQuotaPreferencesListBuildsFilterAndOrder(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes[preferencesPath] = okReply(`{"quotaPreferences":[` + preferenceRow("a") + `]}`)
	_, err := e.invoke(quotaPreferencesListID, `{"project_id":"my-project-123","reconciling":true,`+
		`"request_type":"CLOUD_CONSOLE","created_after":"2026-01-02T03:04:05Z","order_by":["service","create_time"]}`)
	if err != nil {
		t.Fatal(err)
	}
	q := queriesOf(e)[0]
	if q.Get("filter") != "reconciling=true AND request_type=CLOUD_CONSOLE AND creation_time>2026-01-02T03:04:05Z" ||
		q.Get("orderBy") != "service, create_time" {
		t.Errorf("query = %v", q)
	}
	e = newEnvironment(t, nil)
	e.fake.routes[preferencesPath] = okReply(`{}`)
	if _, err := e.invoke(quotaPreferencesListID, `{"project_id":"my-project-123","reconciling":false,"combine":"OR","created_before":"2026-01-02T03:04:05"}`); err != nil {
		t.Fatal(err)
	}
	if got := queriesOf(e)[0].Get("filter"); got != "reconciling=false OR creation_time<2026-01-02T03:04:05" {
		t.Errorf("filter = %q", got)
	}
}

func TestQuotaRefuseInvalidInputBeforeIO(t *testing.T) {
	calls := map[string][2]string{
		"service name":      {quotaInfosListID, `{"project_id":"my-project-123","service":"../x"}`},
		"service uppercase": {quotaInfosGetID, `{"project_id":"my-project-123","service":"A.googleapis.com","quota_id":"q"}`},
		"quota id path":     {quotaInfosGetID, `{"project_id":"my-project-123","service":"a.googleapis.com","quota_id":"a/b"}`},
		"quota id query":    {quotaInfosGetID, `{"project_id":"my-project-123","service":"a.googleapis.com","quota_id":"a?x=1"}`},
		"preference name":   {quotaPreferencesGetID, `{"project_id":"my-project-123","preference_id":"projects/1/locations/global/quotaPreferences/x"}`},
		"preference dots":   {quotaPreferencesGetID, `{"project_id":"my-project-123","preference_id":"../x"}`},
		"filter injection":  {quotaPreferencesListID, `{"project_id":"my-project-123","created_after":"2026-01-02T03:04:05 OR reconciling=true"}`},
		"request type":      {quotaPreferencesListID, `{"project_id":"my-project-123","request_type":"X OR a=b"}`},
		"combine":           {quotaPreferencesListID, `{"project_id":"my-project-123","combine":"XOR"}`},
		"order field":       {quotaPreferencesListID, `{"project_id":"my-project-123","order_by":["etag"]}`},
		"order duplicate":   {quotaPreferencesListID, `{"project_id":"my-project-123","order_by":["service","service"]}`},
		"page size":         {quotaInfosListID, `{"project_id":"my-project-123","service":"a.googleapis.com","page_size":101}`},
		"project name":      {quotaPreferencesListID, `{"project_id":"projects/x"}`},
	}
	for name, call := range calls {
		e := newEnvironment(t, nil)
		if _, err := e.invoke(call[0], call[1]); err == nil {
			t.Errorf("%s: want a refusal", name)
		}
		if len(e.fake.api()) != 0 || e.fake.tokenRequests() != 0 || e.lookups != 0 {
			t.Errorf("%s: api=%d tokens=%d lookups=%d", name, len(e.fake.api()), e.fake.tokenRequests(), e.lookups)
		}
	}
	// The handlers refuse what the schema would not have let through.
	e := newEnvironment(t, nil)
	for name, call := range map[string]func() error{
		"filter": func() error {
			_, err := handleQuotaPreferencesList(t.Context(), e.resolved(), e.secrets(), e.red,
				[]byte(`{"project_id":"my-project-123","created_before":"x OR y"}`))
			return err
		},
		"order": func() error {
			_, err := handleQuotaPreferencesList(t.Context(), e.resolved(), e.secrets(), e.red,
				[]byte(`{"project_id":"my-project-123","order_by":["quota_id","service","create_time","quota_id"]}`))
			return err
		},
		"quota id": func() error {
			_, err := handleQuotaInfosGet(t.Context(), e.resolved(), e.secrets(), e.red,
				[]byte(`{"project_id":"my-project-123","service":"a.googleapis.com","quota_id":"a/b"}`))
			return err
		},
	} {
		if err := call(); !isInvalidRequest(err) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if len(e.fake.api()) != 0 {
		t.Errorf("handler refusals reached Google")
	}
}

func TestQuotaToolsRefuseProjectsOutsideTheTargetsBeforeIO(t *testing.T) {
	calls := map[string]string{
		quotaInfosListID:       `{"project_id":"other-project-1","service":"a.googleapis.com"}`,
		quotaInfosGetID:        `{"project_id":"other-project-1","service":"a.googleapis.com","quota_id":"q"}`,
		quotaPreferencesListID: `{"project_id":"other-project-1"}`,
		quotaPreferencesGetID:  `{"project_id":"other-project-1","preference_id":"p"}`,
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
	// A folder target is proven by reading the project before the quotas are read.
	e := newEnvironment(t, []string{"folders/2001"})
	e.fake.routes["/v3/projects/my-project-123"] = okReply(projectBody)
	e.fake.routes[preferencesPath] = okReply(`{"quotaPreferences":[` + preferenceRow("a") + `]}`)
	if _, err := e.invoke(quotaPreferencesListID, `{"project_id":"my-project-123"}`); err != nil {
		t.Fatal(err)
	}
	if paths := apiPaths(e); len(paths) != 2 || !strings.HasPrefix(paths[0], "cloudresourcemanager") ||
		!strings.HasPrefix(paths[1], "cloudquotas") {
		t.Errorf("paths = %v", paths)
	}
}

func TestQuotaFailures(t *testing.T) {
	cases := []struct {
		name  string
		reply reply
		want  provider.Class
	}{
		{"required API disabled", reply{status: 403, body: errorObject(403, "PERMISSION_DENIED", "SERVICE_DISABLED")}, provider.ClassPermission},
		{"permission denied", reply{status: 403, body: errorObject(403, "PERMISSION_DENIED", "")}, provider.ClassPermission},
		{"not found", reply{status: 404, body: errorObject(404, "NOT_FOUND", "")}, provider.ClassNotFound},
		{"quota exhausted", reply{status: 429, body: errorObject(429, "RESOURCE_EXHAUSTED", "RATE_LIMIT_EXCEEDED")}, provider.ClassRateLimited},
		{"foreign row", okReply(`{"quotaInfos":[{"name":"folders/1"}],"quotaPreferences":[{"name":"folders/1"}]}`), provider.ClassInvalidResponse},
		{"row of another service", okReply(`{"quotaInfos":[{"name":"projects/1/locations/global/services/other.googleapis.com/quotaInfos/q"}]}`), provider.ClassInvalidResponse},
		{"row of another location", okReply(`{"quotaPreferences":[{"name":"projects/1/locations/us/quotaPreferences/q"}]}`), provider.ClassInvalidResponse},
	}
	for _, c := range cases {
		for id, arguments := range map[string]string{
			quotaInfosListID:       `{"project_id":"my-project-123","service":"compute.googleapis.com"}`,
			quotaInfosGetID:        `{"project_id":"my-project-123","service":"compute.googleapis.com","quota_id":"q"}`,
			quotaPreferencesListID: `{"project_id":"my-project-123"}`,
			quotaPreferencesGetID:  `{"project_id":"my-project-123","preference_id":"p"}`,
		} {
			if strings.HasPrefix(c.name, "row") || c.name == "foreign row" {
				if strings.HasSuffix(id, ".get") ||
					(c.name == "row of another service" && id != quotaInfosListID) ||
					(c.name == "row of another location" && id != quotaPreferencesListID) {
					continue
				}
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
}

func TestQuotaResultsAreBounded(t *testing.T) {
	long := strings.Repeat("x", 5000)
	e := newEnvironment(t, nil)
	e.fake.routes[preferencesPath+"/p"] = okReply(`{"name":"projects/1/locations/global/quotaPreferences/p",` +
		`"justification":"` + long + `\u0007line","contactEmail":"` + long + `","quotaConfig":{"stateDetail":"` + long + `"}}`)
	out, err := e.invoke(quotaPreferencesGetID, `{"project_id":"my-project-123","preference_id":"p"}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) > 4*maxText || strings.Contains(out, long[:2000]) {
		t.Errorf("result is not bounded: %d bytes", len(out))
	}
}

func TestQuotaToolsAreOutsideTheReadProfile(t *testing.T) {
	for _, id := range []string{quotaInfosListID, quotaInfosGetID, quotaPreferencesListID, quotaPreferencesGetID} {
		if got := tokenMetadata.Scopes[id]; len(got) != 1 || got[0] != scopeCloudPlatform {
			t.Errorf("%s scopes = %v", id, got)
		}
		if slices.Contains(readTools, id) {
			t.Errorf("%s is in the profile read, which asks for the read-only scope", id)
		}
	}
	if quotaPreferencesList.Risk.DataSensitivity == readRisk.DataSensitivity ||
		quotaPreferencesGet.Risk.DataSensitivity != quotaPreferencesList.Risk.DataSensitivity ||
		quotaInfosList.Risk.DataSensitivity != readRisk.DataSensitivity {
		t.Errorf("sensitivity: preferences %q, infos %q", quotaPreferencesList.Risk.DataSensitivity,
			quotaInfosList.Risk.DataSensitivity)
	}
	for _, d := range []string{quotaPreferencesList.Description, quotaPreferencesGet.Description} {
		if !strings.Contains(d, "personal data") {
			t.Errorf("description lacks the personal data warning: %s", d)
		}
	}
}
