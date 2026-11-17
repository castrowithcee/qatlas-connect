package googleanalytics

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const accessQuotaJSON = `"quota":{"tokensPerDay":{"consumed":4,"remaining":249996},"tokensPerHour":{"consumed":4,` +
	`"remaining":49996},"concurrentRequests":{"consumed":1,"remaining":49},"serverErrorsPerProjectPerHour":` +
	`{"remaining":50},"tokensPerProjectPerHour":{"consumed":4,"remaining":12496}}`

// accessPage answers a report with rows rows of one email dimension and one metric, rowCount in all.
func accessPage(rows, rowCount int, withQuota bool) string {
	list := make([]string, rows)
	for i := range list {
		list[i] = fmt.Sprintf(`{"dimensionValues":[{"value":"user%d@example.com"}],"metricValues":[{"value":"%d"}]}`, i, i+1)
	}
	quota := ""
	if withQuota {
		quota = "," + accessQuotaJSON
	}
	return fmt.Sprintf(`{"dimensionHeaders":[{"dimensionName":"userEmail"}],"metricHeaders":[{"metricName":"accessCount"}],`+
		`"rows":[%s],"rowCount":%d%s}`, strings.Join(list, ","), rowCount, quota)
}

type accessed struct {
	DimensionHeaders []struct {
		DimensionName string `json:"dimensionName"`
	} `json:"dimensionHeaders"`
	MetricHeaders []struct {
		MetricName string `json:"metricName"`
	} `json:"metricHeaders"`
	Rows []struct {
		DimensionValues []struct {
			Value string `json:"value"`
		} `json:"dimensionValues"`
		MetricValues []struct {
			Value string `json:"value"`
		} `json:"metricValues"`
	} `json:"rows"`
	RowCount    int            `json:"rowCount"`
	Quota       map[string]any `json:"quota"`
	Offset      int            `json:"offset"`
	Limit       int            `json:"limit"`
	LimitCapped bool           `json:"limit_capped"`
	NextCursor  string         `json:"next_cursor"`
	HasMore     bool           `json:"has_more"`
	Truncated   []string       `json:"truncated"`
}

const (
	accessMinimal = `"dimensions":[{"name":"userEmail"}],"metrics":[{"name":"accessCount"}],` +
		`"date_ranges":[{"start_date":"7daysAgo","end_date":"today"}]`
	propertyAccess = `{"entity":"properties/200",` + accessMinimal
	accountAccess  = `{"entity":"accounts/100",` + accessMinimal
)

func TestAccessReportSendsTheFilteredRequestToTheAdminOrigin(t *testing.T) {
	env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, accessPage(2, 2, true))})
	arguments := propertyAccess + `,"dimension_filter":{"and_group":{"expressions":[{"filter":{"field_name":"userEmail",` +
		`"string_filter":{"value":"@example.com","match_type":"ENDS_WITH","case_sensitive":false}}},` +
		`{"not_expression":{"filter":{"field_name":"epochTimeMicros","numeric_filter":{"operation":"GREATER_THAN",` +
		`"value":{"int64_value":5}}}}}]}},"metric_filter":{"filter":{"field_name":"accessCount","between_filter":` +
		`{"from_value":{"int64_value":1},"to_value":{"double_value":9.5}}}},` +
		`"order_bys":[{"desc":true,"metric":{"metric_name":"accessCount"}},{"dimension":{"dimension_name":"userEmail",` +
		`"order_type":"ALPHANUMERIC"}}],"time_zone":"Europe/Zurich","include_all_users":true,"expand_groups":true}`
	result, err := env.call(accessPropertyID, "read", arguments)
	var out accessed
	if err != nil || json.Unmarshal([]byte(result), &out) != nil || len(out.Rows) != 2 || out.RowCount != 2 ||
		out.HasMore || out.NextCursor != "" || out.DimensionHeaders[0].DimensionName != "userEmail" ||
		out.MetricHeaders[0].MetricName != "accessCount" || out.Rows[1].DimensionValues[0].Value != "user1@example.com" ||
		out.Rows[1].MetricValues[0].Value != "2" || out.Quota["tokensPerHour"] == nil {
		t.Fatalf("result = %s, %v", result, err)
	}
	requests, _ := env.fake.seen()
	if len(requests) != 1 {
		t.Fatalf("requests = %d", len(requests))
	}
	r := requests[0]
	if r.method != http.MethodPost || r.host != "analyticsadmin.googleapis.com" ||
		r.path != "/v1beta/properties/200:runAccessReport" || r.rawQuery != "" || r.auth != "Bearer "+accessPrefix+"1" {
		t.Errorf("request = %+v", r)
	}
	want := `{"dateRanges":[{"endDate":"today","startDate":"7daysAgo"}],"dimensionFilter":{"andGroup":{"expressions":` +
		`[{"accessFilter":{"fieldName":"userEmail","stringFilter":{"caseSensitive":false,"matchType":"ENDS_WITH",` +
		`"value":"@example.com"}}},{"notExpression":{"accessFilter":{"fieldName":"epochTimeMicros","numericFilter":` +
		`{"operation":"GREATER_THAN","value":{"int64Value":"5"}}}}}]}},"dimensions":[{"name":"userEmail"}],` +
		`"expandGroups":true,"includeAllUsers":true,"limit":"10000","metricFilter":{"accessFilter":{` +
		`"betweenFilter":{"fromValue":{"int64Value":"1"},"toValue":{"doubleValue":9.5}},"fieldName":"accessCount"}},` +
		`"metrics":[{"name":"accessCount"}],"orderBys":[{"desc":true,"metric":{"metricName":"accessCount"}},` +
		`{"dimension":{"dimensionName":"userEmail","orderType":"ALPHANUMERIC"}}],"returnEntityQuota":true,` +
		`"timeZone":"Europe/Zurich"}`
	if r.body != want {
		t.Errorf("body = %s\nwant   %s", r.body, want)
	}
}

func TestAccessReportQuotaIsAskedForByDefaultAndCanBeTurnedOff(t *testing.T) {
	env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, accessPage(1, 1, false))})
	for _, c := range []struct {
		extra string
		want  bool
	}{{``, true}, {`,"return_entity_quota":true`, true}, {`,"return_entity_quota":false`, false}} {
		if _, err := env.call(accessPropertyID, "read", propertyAccess+c.extra+`}`); err != nil {
			t.Fatal(err)
		}
	}
	requests, _ := env.fake.seen()
	for i, want := range []bool{true, true, false} {
		if got := bodyOf(t, requests[i])["returnEntityQuota"]; got != want {
			t.Errorf("request %d returnEntityQuota = %v, want %v", i, got, want)
		}
	}
}

func TestAccessReportOfAnAccountNeverAsksForAQuota(t *testing.T) {
	env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, accessPage(1, 1, false))})
	result, err := env.call(accessAccountID, "read", accountAccess+`,"return_entity_quota":false}`)
	var out accessed
	if err != nil || json.Unmarshal([]byte(result), &out) != nil || out.Quota != nil {
		t.Fatalf("result = %s, %v", result, err)
	}
	if _, err := env.call(accessAccountID, "read", accountAccess+`}`); err != nil {
		t.Fatal(err)
	}
	requests, _ := env.fake.seen()
	if len(requests) != 2 || requests[0].path != "/v1beta/accounts/100:runAccessReport" ||
		requests[0].method != http.MethodPost || requests[0].host != "analyticsadmin.googleapis.com" {
		t.Fatalf("requests = %+v", requests)
	}
	for i := range requests {
		if bodyOf(t, requests[i])["returnEntityQuota"] != false {
			t.Errorf("request %d body = %s", i, requests[i].body)
		}
	}
	if _, err := env.call(accessAccountID, "read", accountAccess+`,"return_entity_quota":true}`); !refusedInvalid(err) {
		t.Errorf("quota for an account: %v", err)
	}
	if after, _ := env.fake.seen(); len(after) != 2 {
		t.Errorf("requests = %d", len(after))
	}
}

func TestAccessReportPagesThroughTheBoundCursor(t *testing.T) {
	env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, accessPage(2, 5, true))})
	page := func(arguments string) accessed {
		t.Helper()
		result, err := env.call(accessPropertyID, "read", arguments)
		var out accessed
		if err != nil || json.Unmarshal([]byte(result), &out) != nil {
			t.Fatalf("result = %s, %v", result, err)
		}
		return out
	}
	one := page(propertyAccess + `,"limit":2}`)
	two := page(propertyAccess + `,"limit":2,"cursor":"` + one.NextCursor + `"}`)
	three := page(propertyAccess + `,"limit":2,"cursor":"` + two.NextCursor + `"}`)
	if !one.HasMore || one.Offset != 0 || !two.HasMore || two.Offset != 2 || three.HasMore || three.NextCursor != "" ||
		three.Offset != 4 || one.NextCursor == "2" {
		t.Fatalf("pages = %+v %+v %+v", one, two, three)
	}
	requests, _ := env.fake.seen()
	for i, want := range []string{"", "2", "4"} {
		body := bodyOf(t, requests[i])
		if got, _ := body["offset"].(string); got != want || body["limit"] != "2" {
			t.Errorf("request %d body = %s", i, requests[i].body)
		}
	}
	if got := page(propertyAccess + `,"offset":3,"limit":2}`); got.Offset != 3 {
		t.Errorf("offset = %d", got.Offset)
	}
	if got := page(propertyAccess + `,"limit":50000}`); got.Limit != maxReportRows || !got.LimitCapped {
		t.Errorf("capped = %+v", got)
	}
}

func TestAccessReportRefusesAManipulatedOrForeignCursor(t *testing.T) {
	env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, accessPage(2, 5, true))})
	result, err := env.call(accessPropertyID, "read", propertyAccess+`,"limit":2}`)
	var out accessed
	if err != nil || json.Unmarshal([]byte(result), &out) != nil || out.NextCursor == "" {
		t.Fatalf("result = %s, %v", result, err)
	}
	for name, c := range map[string]struct{ tool, connection, arguments, cursor string }{
		"altered":          {accessPropertyID, "read", propertyAccess, flipLast(out.NextCursor)},
		"garbage":          {accessPropertyID, "read", propertyAccess, "not-a-cursor"},
		"other entity":     {accessPropertyID, "read", `{"entity":"properties/201",` + accessMinimal, out.NextCursor},
		"other report":     {accessPropertyID, "read", `{"entity":"properties/200","dimensions":[{"name":"epochTimeMicros"}],"metrics":[{"name":"accessCount"}],"date_ranges":[{"start_date":"7daysAgo","end_date":"today"}]`, out.NextCursor},
		"other tool":       {accessAccountID, "read", accountAccess, out.NextCursor},
		"other connection": {accessPropertyID, "property", propertyAccess, out.NextCursor},
	} {
		if _, err := env.call(c.tool, c.connection, c.arguments+`,"cursor":"`+c.cursor+`"}`); !refusedInvalid(err) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := env.call(accessPropertyID, "read", propertyAccess+`,"offset":1,"cursor":"`+out.NextCursor+`"}`); !refusedInvalid(err) {
		t.Errorf("offset with cursor: %v", err)
	}
	if after, _ := env.fake.seen(); len(after) != 1 {
		t.Errorf("requests = %d, want only the first page", len(after))
	}
}

func TestAccessReportChecksTheEntityAgainstTheTargetsBeforeAnyIO(t *testing.T) {
	env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, accessPage(1, 1, true))})
	for name, c := range map[string]struct{ tool, connection, arguments string }{
		"foreign property":         {accessPropertyID, "property", `{"entity":"properties/999",` + accessMinimal + `}`},
		"property of an account":   {accessPropertyID, "property", `{"entity":"accounts/100",` + accessMinimal + `}`},
		"account as property":      {accessAccountID, "read", `{"entity":"properties/200",` + accessMinimal + `}`},
		"foreign account":          {accessAccountID, "account", `{"entity":"accounts/999",` + accessMinimal + `}`},
		"account on property bind": {accessAccountID, "property", accountAccess + `}`},
		"property of other tool":   {accessPropertyID, "read", `{"entity":"properties/200/x",` + accessMinimal + `}`},
		"traversal":                {accessPropertyID, "read", `{"entity":"properties/200/../1",` + accessMinimal + `}`},
		"bound account only":       {accessPropertyID, "property", `{"entity":"properties/0",` + accessMinimal + `}`},
		"limit zero":               {accessPropertyID, "read", propertyAccess + `,"limit":0}`},
		"limit too large":          {accessPropertyID, "read", propertyAccess + `,"limit":100001}`},
		"three date ranges": {accessPropertyID, "read", `{"entity":"properties/200","date_ranges":[` +
			strings.TrimSuffix(strings.Repeat(`{"start_date":"today","end_date":"today"},`, 3), ",") + `]}`},
		"empty filter":   {accessPropertyID, "read", propertyAccess + `,"dimension_filter":{"filter":{"field_name":"a","empty_filter":{}}}}`},
		"two filters":    {accessPropertyID, "read", propertyAccess + `,"dimension_filter":{"filter":{"field_name":"a"},"not_expression":{"filter":{"field_name":"a"}}}}`},
		"unknown member": {accessPropertyID, "read", propertyAccess + `,"return_property_quota":true}`},
		"order both":     {accessPropertyID, "read", propertyAccess + `,"order_bys":[{"metric":{"metric_name":"a"},"dimension":{"dimension_name":"b"}}]}`},
	} {
		if _, err := env.call(c.tool, c.connection, c.arguments); !refusedInvalid(err) ||
			strings.Contains(err.Error(), "999") {
			t.Errorf("%s: %v", name, err)
		}
	}
	env.noIO(t)
}

func TestAccessReportProvesTheAccountOfAPropertyUnderABoundAccount(t *testing.T) {
	env := newEnv(t, &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodGet {
			return respond(r, http.StatusOK, "application/json", `{"name":"properties/200","parent":"accounts/100"}`), nil
		}
		return respond(r, http.StatusOK, "application/json", accessPage(1, 1, true)), nil
	}})
	if _, err := env.call(accessPropertyID, "account", propertyAccess+`}`); err != nil {
		t.Fatal(err)
	}
	requests, _ := env.fake.seen()
	if len(requests) != 2 || requests[0].method != http.MethodGet || requests[0].path != "/v1beta/properties/200" ||
		requests[1].method != http.MethodPost || requests[1].path != "/v1beta/properties/200:runAccessReport" {
		t.Errorf("requests = %+v", requests)
	}
	env = newEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"name":"properties/200","parent":"accounts/900"}`)})
	if _, err := env.call(accessPropertyID, "account", propertyAccess+`}`); !refusedInvalid(err) {
		t.Errorf("foreign account: %v", err)
	}
	if requests, _ := env.fake.seen(); len(requests) != 1 || requests[0].method != http.MethodGet {
		t.Errorf("requests = %+v", requests)
	}
}

func TestAccessReportBoundsUntrustedRowsAndMarksTheCut(t *testing.T) {
	long := strings.Repeat("z", 500)
	cells := strings.TrimSuffix(strings.Repeat(`{"value":"c"},`, 120), ",")
	answer := `{"dimensionHeaders":[{"dimensionName":"userEmail"}],"metricHeaders":[{"metricName":"accessCount"}],` +
		`"rows":[{"dimensionValues":[{"value":"` + long + `"}],"metricValues":[{"value":"1"}]},` +
		`{"dimensionValues":[` + cells + `]},{"dimensionValues":[{"value":"third"}]}],"rowCount":3}`
	env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, answer)})
	result, err := env.call(accessPropertyID, "read", propertyAccess+`,"limit":2}`)
	var out accessed
	if err != nil || json.Unmarshal([]byte(result), &out) != nil || len(out.Rows) != 2 ||
		len([]rune(out.Rows[0].DimensionValues[0].Value)) != maxTextRunes+1 ||
		len(out.Rows[1].DimensionValues) != maxCells || !out.HasMore ||
		strings.Join(out.Truncated, ",") != "rows,cells" {
		t.Errorf("result = %.400s, %v", result, err)
	}
	// A member the answer type does not list, such as a quota of another kind, is never returned.
	env = newEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"rows":[],"rowCount":0,"unknown":"x","quota":{"extra":{"remaining":1}}}`)})
	result, err = env.call(accessPropertyID, "read", propertyAccess+`}`)
	if err != nil || strings.Contains(result, "unknown") || strings.Contains(result, "extra") {
		t.Errorf("result = %s, %v", result, err)
	}
}

func TestAccessReportMapsFailuresWithoutProviderText(t *testing.T) {
	for _, c := range []struct {
		name  string
		api   func(int, *http.Request) (*http.Response, error)
		class provider.Class
	}{
		{"403 missing scope", always(http.StatusForbidden, errorObject(403, "PERMISSION_DENIED", "X")), provider.ClassPermission},
		{"429 without Retry-After", always(http.StatusTooManyRequests, errorObject(429, "RESOURCE_EXHAUSTED", "")), provider.ClassRateLimited},
		{"404", always(http.StatusNotFound, errorObject(404, "NOT_FOUND", "")), provider.ClassNotFound},
		{"500", always(http.StatusInternalServerError, errorObject(500, "INTERNAL", "")), provider.ClassProviderError},
		{"not json", always(http.StatusOK, `{"rows":`), provider.ClassInvalidResponse},
		{"timeout", func(int, *http.Request) (*http.Response, error) { return nil, timeoutError{} }, provider.ClassTimeout},
	} {
		for _, tool := range []struct{ id, arguments string }{{accessPropertyID, propertyAccess + `}`},
			{accessAccountID, accountAccess + `}`}} {
			env := newEnv(t, &fakeGoogle{api: c.api})
			_, err := env.call(tool.id, "read", tool.arguments)
			if classOf(err) != c.class || leaks(err.Error()) != "" {
				t.Errorf("%s %s: %v", tool.id, c.name, err)
			}
			if requests, _ := env.fake.seen(); len(requests) != 1 {
				t.Errorf("%s %s: requests = %d, want exactly one", tool.id, c.name, len(requests))
			}
		}
	}
}

func TestAccessReportIsPersonalDataInTheReadProfile(t *testing.T) {
	reg := registry(t)
	metadata, _ := reg.ProviderMetadata(Provider)
	found := 0
	for _, d := range reg.Provider(Provider) {
		if d.ID != accessAccountID && d.ID != accessPropertyID {
			continue
		}
		found++
		if d.Risk != accessRisk || d.Risk.DataSensitivity == dataSensitivity || d.Risk.DataSensitivity == dataSensitivityReports ||
			!strings.Contains(d.Description, "PERSONAL DATA") || !strings.Contains(d.Description, "untrusted") {
			t.Errorf("%s = %+v", d.ID, d.Risk)
		}
		if got := metadata.Token.Scopes[d.ID]; len(got) != 1 || got[0] != scopeReadOnly {
			t.Errorf("%s scopes = %v", d.ID, got)
		}
		for _, profile := range metadata.Profiles {
			holds := false
			for _, id := range profile.Tools {
				holds = holds || id == d.ID
			}
			if !holds {
				t.Errorf("profile %s lacks %s", profile.ID, d.ID)
			}
		}
	}
	if found != 2 || !strings.Contains(metadata.Profiles[0].Description, "personal data") {
		t.Errorf("found %d tools, read profile %q", found, metadata.Profiles[0].Description)
	}
}
