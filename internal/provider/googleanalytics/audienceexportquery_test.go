package googleanalytics

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const queryName = "properties/200/audienceExports/400"

// queryPage answers a query with rows rows of one synthetic identifier, rowCount rows in all.
func queryPage(rows, rowCount int) string {
	list := make([]string, rows)
	for i := range list {
		list[i] = fmt.Sprintf(`{"dimensionValues":[{"value":"device-sample-%04d"}]}`, i)
	}
	return fmt.Sprintf(`{"audienceRows":[%s],"audienceExport":%s,"rowCount":%d}`, strings.Join(list, ","),
		exportJSON(400, "ACTIVE"), rowCount)
}

type queried struct {
	AudienceRows []struct {
		DimensionValues []struct {
			Value string `json:"value"`
		} `json:"dimensionValues"`
	} `json:"audienceRows"`
	AudienceExport audienceExport `json:"audienceExport"`
	RowCount       int            `json:"rowCount"`
	Offset         int            `json:"offset"`
	Limit          int            `json:"limit"`
	LimitCapped    bool           `json:"limit_capped"`
	NextCursor     string         `json:"next_cursor"`
	HasMore        bool           `json:"has_more"`
	Truncated      []string       `json:"truncated"`
}

func TestAudienceExportsQueryPagesThroughTheBoundCursor(t *testing.T) {
	env := exportEnv(t, &fakeGoogle{api: always(http.StatusOK, queryPage(2, 5))})
	page := func(arguments string) queried {
		t.Helper()
		result, err := env.call(audienceQueryID, "read", arguments)
		var out queried
		if err != nil || json.Unmarshal([]byte(result), &out) != nil {
			t.Fatalf("result = %s, %v", result, err)
		}
		return out
	}
	one := page(`{"name":"` + queryName + `","limit":2}`)
	two := page(`{"name":"` + queryName + `","limit":2,"cursor":"` + one.NextCursor + `"}`)
	three := page(`{"name":"` + queryName + `","limit":2,"cursor":"` + two.NextCursor + `"}`)
	if !one.HasMore || one.Offset != 0 || len(one.AudienceRows) != 2 || !two.HasMore || two.Offset != 2 ||
		three.HasMore || three.NextCursor != "" || three.Offset != 4 || one.RowCount != 5 ||
		one.AudienceExport.State != "ACTIVE" || one.NextCursor == "2" ||
		one.AudienceRows[0].DimensionValues[0].Value != "device-sample-0000" {
		t.Fatalf("pages = %+v %+v %+v", one, two, three)
	}
	requests, _ := env.fake.seen()
	if len(requests) != 3 {
		t.Fatalf("requests = %d", len(requests))
	}
	for i, want := range []string{"", "2", "4"} {
		body := bodyOf(t, requests[i])
		got, _ := body["offset"].(string)
		r := requests[i]
		if got != want || body["limit"] != "2" || r.method != http.MethodPost || r.host != "analyticsdata.googleapis.com" ||
			r.path != "/v1beta/"+queryName+":query" || r.rawQuery != "" || r.auth != "Bearer "+accessPrefix+"1" {
			t.Errorf("request %d = %+v", i, r)
		}
	}
	if got := page(`{"name":"` + queryName + `","offset":3,"limit":2}`); got.Offset != 3 {
		t.Errorf("offset = %d", got.Offset)
	}
}

func TestAudienceExportsQueryRefusesAManipulatedOrForeignCursor(t *testing.T) {
	env := exportEnv(t, &fakeGoogle{api: always(http.StatusOK, queryPage(2, 5))})
	result, err := env.call(audienceQueryID, "read", `{"name":"`+queryName+`","limit":2}`)
	var out queried
	if err != nil || json.Unmarshal([]byte(result), &out) != nil || out.NextCursor == "" {
		t.Fatalf("result = %s, %v", result, err)
	}
	for name, c := range map[string]struct{ connection, name, cursor string }{
		"altered":          {"read", queryName, flipLast(out.NextCursor)},
		"garbage":          {"read", queryName, "not-a-cursor"},
		"other export":     {"read", "properties/200/audienceExports/401", out.NextCursor},
		"other property":   {"read", "properties/201/audienceExports/400", out.NextCursor},
		"other tool":       {"read", queryName, mustCursorOf(t, env, audienceListID)},
		"other connection": {"property", queryName, out.NextCursor},
	} {
		arguments := `{"name":"` + c.name + `","cursor":"` + c.cursor + `"}`
		if _, err := env.call(audienceQueryID, c.connection, arguments); !refusedInvalid(err) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := env.call(audienceQueryID, "read", `{"name":"`+queryName+`","offset":1,"cursor":"`+out.NextCursor+`"}`); !refusedInvalid(err) {
		t.Errorf("offset with cursor: %v", err)
	}
	if after, _ := env.fake.seen(); len(after) != 1 {
		t.Errorf("requests = %d, want only the first page", len(after))
	}
}

// mustCursorOf returns a cursor of another tool of the same connection and property.
func mustCursorOf(t *testing.T, env *testEnv, tool string) string {
	t.Helper()
	pages := &fakeGoogle{api: always(http.StatusOK, listPage("tok-2", 1))}
	other := exportEnv(t, pages)
	result, err := other.call(tool, "read", `{"property":"properties/200"}`)
	var out struct {
		NextCursor string `json:"next_cursor"`
	}
	if err != nil || json.Unmarshal([]byte(result), &out) != nil || out.NextCursor == "" {
		t.Fatalf("result = %s, %v", result, err)
	}
	return out.NextCursor
}

func TestAudienceExportsQueryOfAnExportThatIsNotActiveIsRefused(t *testing.T) {
	for name, c := range map[string]struct {
		status int
		body   string
		class  provider.Class
	}{
		"creating":  {http.StatusBadRequest, errorObject(400, "FAILED_PRECONDITION", ""), provider.ClassProviderError},
		"not found": {http.StatusNotFound, errorObject(404, "NOT_FOUND", ""), provider.ClassNotFound},
	} {
		env := exportEnv(t, &fakeGoogle{api: always(c.status, c.body)})
		_, err := env.call(audienceQueryID, "read", `{"name":"`+queryName+`"}`)
		if classOf(err) != c.class || leaks(err.Error()) != "" {
			t.Errorf("%s: %v", name, err)
		}
		if requests, _ := env.fake.seen(); len(requests) != 1 {
			t.Errorf("%s: requests = %d, want exactly one", name, len(requests))
		}
	}
}

func TestAudienceExportsQueryChecksTheNameAgainstTheTargetsBeforeAnyIO(t *testing.T) {
	env := exportEnv(t, &fakeGoogle{api: always(http.StatusOK, queryPage(1, 1))})
	for name, c := range map[string]struct{ connection, arguments string }{
		"foreign property": {"property", `{"name":"properties/999/audienceExports/400"}`},
		"bad form":         {"read", `{"name":"properties/200/audiences/400"}`},
		"traversal":        {"read", `{"name":"properties/200/audienceExports/../../accounts/1"}`},
		"query string":     {"read", `{"name":"properties/200/audienceExports/4?alt=media"}`},
		"limit zero":       {"read", `{"name":"` + queryName + `","limit":0}`},
		"unknown member":   {"read", `{"name":"` + queryName + `","dimension_filter":{}}`},
	} {
		if _, err := env.call(audienceQueryID, c.connection, c.arguments); !refusedInvalid(err) ||
			strings.Contains(err.Error(), "999") {
			t.Errorf("%s: %v", name, err)
		}
	}
	env.noIO(t)
}

func TestAudienceExportsQueryProvesTheAccountOfAPropertyUnderABoundAccount(t *testing.T) {
	env := exportEnv(t, &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
		if r.URL.Host == "analyticsadmin.googleapis.com" {
			return respond(r, http.StatusOK, "application/json", `{"name":"properties/200","parent":"accounts/100"}`), nil
		}
		return respond(r, http.StatusOK, "application/json", queryPage(1, 1)), nil
	}})
	if _, err := env.call(audienceQueryID, "account", `{"name":"`+queryName+`"}`); err != nil {
		t.Fatal(err)
	}
	requests, _ := env.fake.seen()
	if len(requests) != 2 || requests[0].host != "analyticsadmin.googleapis.com" || requests[1].host != "analyticsdata.googleapis.com" {
		t.Errorf("requests = %+v", requests)
	}
	// A property of another account is refused after the proof read, without a data request.
	env = exportEnv(t, &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
		return respond(r, http.StatusOK, "application/json", `{"name":"properties/200","parent":"accounts/900"}`), nil
	}})
	if _, err := env.call(audienceQueryID, "account", `{"name":"`+queryName+`"}`); !refusedInvalid(err) {
		t.Errorf("foreign account: %v", err)
	}
	if requests, _ := env.fake.seen(); len(requests) != 1 || requests[0].host != "analyticsadmin.googleapis.com" {
		t.Errorf("requests = %+v", requests)
	}
}

func TestAudienceExportsQueryIsNoCoreRequestAndLeavesTheGateAlone(t *testing.T) {
	freshQuotas(t)
	env := exportEnv(t, &fakeGoogle{api: always(http.StatusOK, queryPage(1, 1))})
	for i := 0; i < maxConcurrentReports; i++ {
		if _, err := quotas.enter("test", "properties/200", env.open(t, "read").identity); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := env.call(audienceQueryID, "read", `{"name":"`+queryName+`"}`); err != nil {
		t.Errorf("query with all Core slots taken: %v", err)
	}
}

func TestAudienceExportsQueryBoundsUntrustedRowsAndMarksTheCut(t *testing.T) {
	long := strings.Repeat("z", 500)
	cells := strings.TrimSuffix(strings.Repeat(`{"value":"c"},`, 120), ",")
	answer := `{"audienceRows":[{"dimensionValues":[{"value":"` + long + `"}]},{"dimensionValues":[` + cells + `]},` +
		`{"dimensionValues":[{"value":"third"}]}],"audienceExport":` + exportJSON(400, "ACTIVE") + `,"rowCount":3}`
	env := exportEnv(t, &fakeGoogle{api: always(http.StatusOK, answer)})
	result, err := env.call(audienceQueryID, "read", `{"name":"`+queryName+`","limit":2}`)
	var out queried
	if err != nil || json.Unmarshal([]byte(result), &out) != nil || len(out.AudienceRows) != 2 ||
		len([]rune(out.AudienceRows[0].DimensionValues[0].Value)) != maxTextRunes+1 ||
		len(out.AudienceRows[1].DimensionValues) != maxCells || !out.HasMore ||
		strings.Join(out.Truncated, ",") != "audienceRows,cells" {
		t.Errorf("result = %.400s, %v", result, err)
	}
	// A query for another export than the one asked for is not trusted.
	env = exportEnv(t, &fakeGoogle{api: always(http.StatusOK, strings.Replace(queryPage(1, 1), "audienceExports/400", "audienceExports/401", 1))})
	if _, err := env.call(audienceQueryID, "read", `{"name":"`+queryName+`"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("other export: %v", err)
	}
}

func TestAudienceExportsQueryMapsFailuresWithoutProviderText(t *testing.T) {
	for _, c := range []struct {
		name  string
		api   func(int, *http.Request) (*http.Response, error)
		class provider.Class
	}{
		{"403", always(http.StatusForbidden, errorObject(403, "PERMISSION_DENIED", "X")), provider.ClassPermission},
		{"429 without Retry-After", always(http.StatusTooManyRequests, errorObject(429, "RESOURCE_EXHAUSTED", "")), provider.ClassRateLimited},
		{"500", always(http.StatusInternalServerError, errorObject(500, "INTERNAL", "")), provider.ClassProviderError},
		{"not json", always(http.StatusOK, `{"audienceRows":`), provider.ClassInvalidResponse},
		{"timeout", func(int, *http.Request) (*http.Response, error) { return nil, timeoutError{} }, provider.ClassTimeout},
	} {
		env := exportEnv(t, &fakeGoogle{api: c.api})
		_, err := env.call(audienceQueryID, "read", `{"name":"`+queryName+`"}`)
		if classOf(err) != c.class || leaks(err.Error()) != "" {
			t.Errorf("%s: %v", c.name, err)
		}
		if requests, _ := env.fake.seen(); len(requests) != 1 {
			t.Errorf("%s: requests = %d, want exactly one", c.name, len(requests))
		}
	}
}

func TestAudienceExportsQueryIsPersonalDataInTheReadProfile(t *testing.T) {
	reg := registry(t)
	metadata, _ := reg.ProviderMetadata(Provider)
	for _, d := range reg.Provider(Provider) {
		if d.ID == audienceQueryID && (d.Risk != audienceUsersRisk || d.Risk.DataSensitivity == dataSensitivityReports ||
			!strings.Contains(d.Description, "PERSONAL DATA")) {
			t.Errorf("%s = %+v", d.ID, d.Risk)
		}
	}
	if got := metadata.Token.Scopes[audienceQueryID]; len(got) != 1 || got[0] != scopeReadOnly {
		t.Errorf("scopes = %v", got)
	}
	for _, profile := range metadata.Profiles {
		holds := false
		for _, id := range profile.Tools {
			holds = holds || id == audienceQueryID
		}
		if !holds {
			t.Errorf("profile %s holds the query tool: %t", profile.ID, holds)
		}
	}
}
