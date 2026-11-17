package googleanalytics

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const metadataAnswerJSON = `{"name":"properties/200/metadata","dimensions":[` +
	`{"apiName":"country","uiName":"Country","description":"The country of the user.","category":"Geography",` +
	`"deprecatedApiNames":["countryOld"]},` +
	`{"apiName":"customEvent:plan","uiName":"Plan","description":"Custom","category":"Custom","customDefinition":true}],` +
	`"metrics":[{"apiName":"sessions","uiName":"Sessions","description":"Sessions.","category":"Session","type":"TYPE_INTEGER"},` +
	`{"apiName":"customEvent:price","uiName":"Price","type":"TYPE_CURRENCY","customDefinition":true,"expression":"a/b",` +
	`"blockedReasons":["NO_REVENUE_METRICS"],"deprecatedApiNames":["priceOld"]}],` +
	`"comparisons":[{"apiName":"comparisons/1","uiName":"Mobile","description":"Mobile users"}]}`

func TestMetadataGetReturnsEveryMemberIncludingCustomEntries(t *testing.T) {
	freshQuotas(t)
	env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, metadataAnswerJSON)})
	result, err := env.call(metadataGetID, "read", `{"property":"properties/200"}`)
	if err != nil {
		t.Fatal(err)
	}
	requests, _ := env.fake.seen()
	r := requests[0]
	if len(requests) != 1 || r.method != http.MethodGet || r.host != "analyticsdata.googleapis.com" ||
		r.path != "/v1beta/properties/200/metadata" || r.body != "" || r.contentType != "" ||
		r.auth != "Bearer "+accessPrefix+"1" {
		t.Fatalf("requests = %+v", requests)
	}
	var out metadataResult
	if err := json.Unmarshal([]byte(result), &out); err != nil || len(out.Dimensions) != 2 || len(out.Metrics) != 2 ||
		len(out.Comparisons) != 1 || len(out.Truncated) != 0 {
		t.Fatalf("result = %s, %v", result, err)
	}
	d, m := out.Dimensions, out.Metrics
	if d[0].APIName != "country" || d[0].Category != "Geography" || d[0].CustomDefinition ||
		d[0].DeprecatedAPINames[0] != "countryOld" || !d[1].CustomDefinition ||
		m[0].Type != "TYPE_INTEGER" || !m[1].CustomDefinition || m[1].Expression != "a/b" ||
		m[1].BlockedReasons[0] != "NO_REVENUE_METRICS" || out.Comparisons[0].APIName != "comparisons/1" {
		t.Errorf("metadata = %+v", out)
	}
}

func TestMetadataGetUniversalNeedsNoTargetAndNoProof(t *testing.T) {
	freshQuotas(t)
	fake := &fakeGoogle{api: always(http.StatusOK, `{"name":"properties/0/metadata","dimensions":[],"metrics":[]}`)}
	for _, connection := range []string{"read", "property", "account", "mixed"} {
		env := newEnv(t, fake)
		before, _ := fake.seen()
		if _, err := env.call(metadataGetID, connection, `{"property":"properties/0"}`); err != nil {
			t.Errorf("%s: %v", connection, err)
		}
		after, _ := fake.seen()
		last := after[len(after)-1]
		if len(after)-len(before) != 1 || last.host != "analyticsdata.googleapis.com" ||
			last.path != "/v1beta/properties/0/metadata" {
			t.Errorf("%s: requests %+v", connection, after[len(before):])
		}
	}
	// Other tools do not take the universal name.
	env := newEnv(t, fake)
	if _, err := env.call(reportsRunID, "read", `{"property":"properties/0",`+minimalReport[1:]); !refusedInvalid(err) {
		t.Errorf("reports.run with properties/0: %v", err)
	}
}

func TestMetadataGetRefusesForeignPropertyBeforeIO(t *testing.T) {
	freshQuotas(t)
	env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, metadataAnswerJSON)})
	for _, c := range []struct{ connection, arguments string }{
		{"property", `{"property":"properties/201"}`}, {"account", `{}`}, {"read", `{"property":"accounts/1"}`},
		{"read", `{"property":"properties/00"}`},
	} {
		if _, err := env.call(metadataGetID, c.connection, c.arguments); !refusedInvalid(err) {
			t.Errorf("%s %s: %v", c.connection, c.arguments, err)
		}
	}
	if requests, grants := env.fake.seen(); len(requests) != 0 || grants != 0 {
		t.Errorf("requests %d, grants %d", len(requests), grants)
	}
	// A property of a bound account is proven by a read of the Admin API first.
	fake := &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
		if r.URL.Host == "analyticsadmin.googleapis.com" {
			return respond(r, http.StatusOK, "application/json",
				`{"name":"properties/901","parent":"accounts/900"}`), nil
		}
		return respond(r, http.StatusOK, "application/json", metadataAnswerJSON), nil
	}}
	env = newEnv(t, fake)
	_, err := env.call(metadataGetID, "account", `{"property":"properties/901"}`)
	requests, _ := fake.seen()
	if !refusedInvalid(err) || len(requests) != 1 || requests[0].host != "analyticsadmin.googleapis.com" ||
		strings.Contains(err.Error(), "901") {
		t.Errorf("err = %v, requests %+v", err, requests)
	}
}

func TestMetadataGetBoundsTheListsAndTheText(t *testing.T) {
	freshQuotas(t)
	var b strings.Builder
	b.WriteString(`{"name":"properties/200/metadata","dimensions":[`)
	for i := 0; i < maxMetadataEntries+3; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`{"apiName":"d` + strings.Repeat("x", 1) + `","description":"` + strings.Repeat("y", 1500) + `"}`)
	}
	b.WriteString(`],"metrics":[]}`)
	env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, b.String())})
	result, err := env.call(metadataGetID, "read", `{"property":"properties/200"}`)
	var out metadataResult
	if err != nil || json.Unmarshal([]byte(result), &out) != nil || len(out.Dimensions) != maxMetadataEntries ||
		len(out.Truncated) != 1 || out.Truncated[0] != "dimensions" ||
		len(out.Dimensions[0].Description) > 1100 || strings.Contains(result, strings.Repeat("y", 1100)) {
		t.Fatalf("result length %d, truncated %v, %v", len(result), out.Truncated, err)
	}
}

const compatibilityAnswerJSON = `{"dimensionCompatibilities":[` +
	`{"dimensionMetadata":{"apiName":"country","uiName":"Country","category":"Geography"},"compatibility":"COMPATIBLE"},` +
	`{"dimensionMetadata":{"apiName":"itemName","uiName":"Item name"},"compatibility":"INCOMPATIBLE"}],` +
	`"metricCompatibilities":[{"metricMetadata":{"apiName":"sessions","type":"TYPE_INTEGER"},"compatibility":"COMPATIBLE"}]}`

func TestCompatibilityCheckReturnsMixedResultAndSendsTheRequest(t *testing.T) {
	freshQuotas(t)
	env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, compatibilityAnswerJSON)})
	arguments := `{"property":"properties/200","dimensions":[{"name":"country"}],"metrics":[{"name":"sessions"}],` +
		`"dimension_filter":{"filter":{"field_name":"country","string_filter":{"value":"CH"}}},` +
		`"metric_filter":{"filter":{"field_name":"sessions","numeric_filter":{"operation":"GREATER_THAN",` +
		`"value":{"int64_value":1}}}},"compatibility_filter":"COMPATIBLE"}`
	result, err := env.call(compatibilityID, "read", arguments)
	if err != nil {
		t.Fatal(err)
	}
	requests, _ := env.fake.seen()
	r := requests[0]
	if len(requests) != 1 || r.method != http.MethodPost || r.host != "analyticsdata.googleapis.com" ||
		r.path != "/v1beta/properties/200:checkCompatibility" || r.contentType != "application/json" {
		t.Fatalf("requests = %+v", requests)
	}
	body := bodyOf(t, r)
	got, _ := json.Marshal(body)
	want := `{"compatibilityFilter":"COMPATIBLE","dimensionFilter":{"filter":{"fieldName":"country","stringFilter":{"value":"CH"}}},` +
		`"dimensions":[{"name":"country"}],"metricFilter":{"filter":{"fieldName":"sessions","numericFilter":` +
		`{"operation":"GREATER_THAN","value":{"int64Value":"1"}}}},"metrics":[{"name":"sessions"}]}`
	if string(got) != want {
		t.Errorf("body = %s", got)
	}
	var out compatibilityResult
	if err := json.Unmarshal([]byte(result), &out); err != nil || len(out.DimensionCompatibilities) != 2 ||
		out.DimensionCompatibilities[0].Compatibility != "COMPATIBLE" ||
		out.DimensionCompatibilities[1].Compatibility != "INCOMPATIBLE" ||
		out.DimensionCompatibilities[1].DimensionMetadata.APIName != "itemName" ||
		len(out.MetricCompatibilities) != 1 || out.MetricCompatibilities[0].MetricMetadata.Type != "TYPE_INTEGER" {
		t.Fatalf("result = %s, %v", result, err)
	}
}

func TestCompatibilityCheckRefusesBadArgumentsAndForeignPropertiesBeforeIO(t *testing.T) {
	freshQuotas(t)
	env := newEnv(t, &fakeGoogle{})
	for _, c := range []struct{ connection, arguments string }{
		{"read", `{"property":"properties/200","compatibility_filter":"ALL"}`},
		{"read", `{"property":"properties/200","dimension_filter":{"filter":{"field_name":"a"}}}`},
		{"read", `{"property":"properties/200","dimensions":[{"name":"a","dimension_expression":{}}]}`},
		{"read", `{"property":"properties/200","date_ranges":[{"start_date":"today","end_date":"today"}]}`},
		{"read", `{"property":"properties/0"}`},
		{"property", `{"property":"properties/201"}`},
		{"account", `{}`},
	} {
		if _, err := env.call(compatibilityID, c.connection, c.arguments); !refusedInvalid(err) {
			t.Errorf("%s %s: %v", c.connection, c.arguments, err)
		}
	}
	if requests, grants := env.fake.seen(); len(requests) != 0 || grants != 0 || env.reads.Load() != 0 {
		t.Errorf("requests %d, grants %d, secret reads %d", len(requests), grants, env.reads.Load())
	}
}

func TestMetadataAndCompatibilityMapFailuresWithoutProviderText(t *testing.T) {
	for _, tool := range []struct{ id, arguments string }{
		{metadataGetID, `{"property":"properties/200"}`}, {compatibilityID, `{"property":"properties/200"}`},
	} {
		freshQuotas(t)
		env := newEnv(t, &fakeGoogle{api: always(http.StatusForbidden, errorObject(403, "PERMISSION_DENIED", "X"))})
		if _, err := env.call(tool.id, "read", tool.arguments); classOf(err) != provider.ClassPermission ||
			leaks(err.Error()) != "" {
			t.Errorf("%s 403: %v", tool.id, err)
		}
		env = newEnv(t, &fakeGoogle{api: always(http.StatusTooManyRequests, errorObject(429, "RESOURCE_EXHAUSTED", "X"))})
		_, err := env.call(tool.id, "read", tool.arguments)
		requests, _ := env.fake.seen()
		if classOf(err) != provider.ClassRateLimited || leaks(err.Error()) != "" || len(requests) != 1 {
			t.Errorf("%s 429: %v, requests %d", tool.id, err, len(requests))
		}
	}
}

func TestMetadataAndCompatibilityShareTheLocalQuotaRules(t *testing.T) {
	freshQuotas(t)
	// A report leaves a low remainder, so both Core requests are refused before any I/O.
	env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, reportPage(1, 1, minTokensLeft-1))})
	if _, err := env.call(reportsRunID, "read", `{"property":"properties/200",`+minimalReport[1:]); err != nil {
		t.Fatal(err)
	}
	before, _ := env.fake.seen()
	for _, id := range []string{metadataGetID, compatibilityID} {
		if _, err := env.call(id, "read", `{"property":"properties/200"}`); classOf(err) != provider.ClassRateLimited {
			t.Errorf("%s: %v", id, err)
		}
	}
	if after, _ := env.fake.seen(); len(after) != len(before) {
		t.Errorf("requests %d -> %d", len(before), len(after))
	}
}
