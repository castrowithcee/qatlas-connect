package googleads

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// reportArgs builds the JSON of reports.search arguments on the client account.
func reportArgs(extra string) string {
	return `{"customer_id":"` + clientID + `","resource":"campaign","fields":["campaign.id","metrics.clicks"]` + extra + `}`
}

// campaignRow is a row of a campaign report.
func campaignRow(id, clicks string) string {
	return `{"campaign":{"resourceName":"customers/` + clientID + `/campaigns/` + id + `","id":"` + id + `","name":"C ` + id +
		`"},"metrics":{"clicks":"` + clicks + `"}}`
}

func reportReply(extra string, items ...string) reply {
	return okReply(`{"results":[` + strings.Join(items, ",") + `],"fieldMask":"campaign.id,metrics.clicks"` + extra + `}`)
}

func lastBody(t *testing.T, e *environment) map[string]any {
	t.Helper()
	requests := e.fake.api()
	if len(requests) == 0 {
		t.Fatal("no request was sent")
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(requests[len(requests)-1].body), &body); err != nil {
		t.Fatal(err)
	}
	return body
}

func TestReportsSearchBuildsTheQueryFromStructure(t *testing.T) {
	e := newEnvironment(t, nil, nil)
	e.fake.routes["POST "+searchPath(clientID)] = reportReply("")
	_, err := e.invoke(reportsSearchID, `{"customer_id":"`+clientID+`","resource":"campaign","fields":["campaign.id",`+
		`"campaign.name","segments.date","metrics.clicks"],"conditions":[`+
		`{"field":"campaign.status","operator":"IN","values":["ENABLED","PAUSED"]},`+
		`{"field":"campaign.name","operator":"LIKE","value":"Brand%"},`+
		`{"field":"campaign.name","operator":"NOT REGEXP_MATCH","value":"^x.*$"},`+
		`{"field":"metrics.clicks","operator":">=","value":10},`+
		`{"field":"metrics.ctr","operator":"BETWEEN","values":[0.5,"1.5"]},`+
		`{"field":"campaign.network_settings.target_google_search","operator":"=","value":true},`+
		`{"field":"campaign.labels","operator":"CONTAINS ANY","values":["customers/`+clientID+`/labels/7"]},`+
		`{"field":"campaign.start_date_time","operator":"IS NOT NULL"},`+
		`{"field":"segments.date","operator":"BETWEEN","values":["2026-01-01","2026-01-31"]},`+
		`{"field":"segments.date","operator":"DURING","value":"LAST_30_DAYS"}],`+
		`"orderings":[{"field":"metrics.clicks","direction":"DESC"},{"field":"campaign.id"}],"limit":50,`+
		`"summary_row":true,"omit_unselected_resource_names":true}`)
	if err != nil {
		t.Fatal(err)
	}
	want := "SELECT campaign.id, campaign.name, segments.date, metrics.clicks FROM campaign WHERE " +
		"campaign.status IN ('ENABLED', 'PAUSED') AND campaign.name LIKE 'Brand%' AND campaign.name NOT " +
		"REGEXP_MATCH '^x.*$' AND metrics.clicks >= 10 AND metrics.ctr BETWEEN 0.5 AND 1.5 AND " +
		"campaign.network_settings.target_google_search = TRUE AND campaign.labels CONTAINS ANY ('customers/" +
		clientID + "/labels/7') AND campaign.start_date_time IS NOT NULL AND segments.date BETWEEN '2026-01-01' " +
		"AND '2026-01-31' AND segments.date DURING LAST_30_DAYS ORDER BY metrics.clicks DESC, campaign.id ASC " +
		"LIMIT 51 PARAMETERS omit_unselected_resource_names=true"
	requests := e.fake.api()
	if len(requests) != 1 || requests[0].query != want {
		t.Fatalf("query = %q\nwant    %q", requests[0].query, want)
	}
	body := lastBody(t, e)
	settings, _ := body["searchSettings"].(map[string]any)
	if settings["returnTotalResultsCount"] != true || settings["returnSummaryRow"] != true || settings["omitResults"] != nil {
		t.Errorf("searchSettings = %v", settings)
	}
	if requests[0].method != "POST" || strings.Contains(requests[0].url, "SELECT") || requests[0].developerToken != "" {
		t.Errorf("request = %+v", requests[0])
	}
}

func TestReportsSearchRefusesBeforeSecretAndIO(t *testing.T) {
	var notSelectable, notFilterableOnly string
	for _, f := range catalogFields {
		if f.flags&flagSelectable == 0 && strings.HasPrefix(f.name, "ad_group_ad.") && notSelectable == "" {
			notSelectable = f.name
		}
	}
	if notSelectable == "" {
		t.Fatal("the catalog holds no field of ad_group_ad that cannot be selected")
	}
	notFilterableOnly = "campaign.url_custom_parameters"
	cases := map[string]string{
		"query string":                `,"query":"SELECT campaign.id FROM campaign"`,
		"unknown resource":            `,"resource":"campaignx"`,
		"resource of another form":    `,"resource":"campaign; DROP"`,
		"no fields":                   `,"fields":[]`,
		"too many fields":             `,"fields":["` + strings.Repeat(`campaign.id","`, 101) + `campaign.id"]`,
		"unknown field":               `,"fields":["campaign.nope"]`,
		"wildcard":                    `,"fields":["campaign.*"]`,
		"field of another resource":   `,"resource":"customer","fields":["ad_group.id"]`,
		"repeated field":              `,"fields":["campaign.id","campaign.id"]`,
		"field not selectable":        `,"resource":"ad_group_ad","fields":["` + notSelectable + `"]`,
		"withheld passkey":            `,"resource":"customer_user_access","fields":["customer_user_access.passkey_enabled"]`,
		"withheld link id":            `,"resource":"third_party_app_analytics_link","fields":["third_party_app_analytics_link.shareable_link_id"]`,
		"withheld webhook":            `,"resource":"asset","fields":["asset.lead_form_asset.delivery_methods"]`,
		"withheld shared key":         `,"resource":"android_privacy_shared_key_google_campaign","fields":["android_privacy_shared_key_google_campaign.shared_campaign_key"]`,
		"withheld in condition":       `,"resource":"customer_user_access","fields":["customer_user_access.user_id"],"conditions":[{"field":"customer_user_access.passkey_enabled","operator":"=","value":true}]`,
		"withheld in ordering":        `,"resource":"customer_user_access","fields":["customer_user_access.user_id"],"orderings":[{"field":"customer_user_access.passkey_enabled"}]`,
		"condition not filterable":    `,"conditions":[{"field":"` + notFilterableOnly + `","operator":"IS NULL"}]`,
		"ordering not sortable":       `,"orderings":[{"field":"campaign.resource_name"}]`,
		"unknown operator":            `,"conditions":[{"field":"campaign.id","operator":"OR","value":1}]`,
		"operator of wrong type":      `,"conditions":[{"field":"campaign.status","operator":"LIKE","value":"EN%"}]`,
		"contains on scalar":          `,"conditions":[{"field":"campaign.id","operator":"CONTAINS ANY","values":[1]}]`,
		"comparison on text":          `,"conditions":[{"field":"campaign.name","operator":">","value":"a"}]`,
		"enum UNSPECIFIED":            `,"conditions":[{"field":"campaign.status","operator":"=","value":"UNSPECIFIED"}]`,
		"enum UNKNOWN":                `,"conditions":[{"field":"campaign.status","operator":"!=","value":"UNKNOWN"}]`,
		"enum UNKNOWN in list":        `,"conditions":[{"field":"campaign.status","operator":"IN","values":["ENABLED","UNKNOWN"]}]`,
		"enum not listed":             `,"conditions":[{"field":"campaign.status","operator":"=","value":"enabled"}]`,
		"single quote":                `,"conditions":[{"field":"campaign.name","operator":"=","value":"O'Brien"}]`,
		"double quote":                `,"conditions":[{"field":"campaign.name","operator":"=","value":"say \"hi\""}]`,
		"backslash":                   `,"conditions":[{"field":"campaign.name","operator":"=","value":"a\\b"}]`,
		"control character":           `,"conditions":[{"field":"campaign.name","operator":"=","value":"a\nb"}]`,
		"quote in list":               `,"conditions":[{"field":"campaign.name","operator":"IN","values":["a","b'"]}]`,
		"quote in LIKE":               `,"conditions":[{"field":"campaign.name","operator":"LIKE","value":"%'%"}]`,
		"quote in regexp":             `,"conditions":[{"field":"campaign.name","operator":"REGEXP_MATCH","value":"a\"b"}]`,
		"bad regexp":                  `,"conditions":[{"field":"campaign.name","operator":"REGEXP_MATCH","value":"(a"}]`,
		"number with exponent":        `,"conditions":[{"field":"metrics.clicks","operator":"=","value":1e3}]`,
		"number injected as text":     `,"conditions":[{"field":"metrics.clicks","operator":"=","value":"1 OR 1=1"}]`,
		"boolean for number":          `,"conditions":[{"field":"metrics.clicks","operator":"=","value":true}]`,
		"bad date":                    `,"conditions":[{"field":"segments.date","operator":"=","value":"2026-02-30"}]`,
		"date of another form":        `,"conditions":[{"field":"segments.date","operator":"=","value":"01.01.2026"}]`,
		"DURING unknown function":     `,"conditions":[{"field":"segments.date","operator":"DURING","value":"LAST_YEAR"}]`,
		"DURING on number":            `,"conditions":[{"field":"metrics.clicks","operator":"DURING","value":"TODAY"}]`,
		"BETWEEN with one value":      `,"conditions":[{"field":"metrics.clicks","operator":"BETWEEN","values":[1]}]`,
		"IS NULL with value":          `,"conditions":[{"field":"campaign.id","operator":"IS NULL","value":1}]`,
		"= without value":             `,"conditions":[{"field":"campaign.id","operator":"="}]`,
		"= with values":               `,"conditions":[{"field":"campaign.id","operator":"=","values":[1]}]`,
		"IN with empty list":          `,"conditions":[{"field":"campaign.id","operator":"IN","values":[]}]`,
		"object as value":             `,"conditions":[{"field":"campaign.id","operator":"=","value":{"a":1}}]`,
		"null as value":               `,"conditions":[{"field":"campaign.id","operator":"=","value":null}]`,
		"extra member of a condition": `,"conditions":[{"field":"campaign.id","operator":"=","value":1,"or":"x"}]`,
		"too many conditions":         `,"conditions":[` + strings.TrimSuffix(strings.Repeat(`{"field":"campaign.id","operator":"IS NULL"},`, 51), ",") + `]`,
		"bad direction":               `,"orderings":[{"field":"campaign.id","direction":"UP"}]`,
		"limit zero":                  `,"limit":0`,
		"limit over cap":              `,"limit":1001`,
		"page token":                  `,"page_token":"abc"`,
		"login header":                `,"login_customer_id":"1000000000"`,
	}
	for name, extra := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnvironment(t, nil, nil)
			if _, err := e.invoke(reportsSearchID, reportArgs(extra)); err == nil {
				t.Errorf("%s was accepted", extra)
			}
			if e.lookups != 0 || len(e.fake.api()) != 0 || e.fake.tokenRequests() != 0 {
				t.Errorf("lookups=%d requests=%d tokens=%d, want none", e.lookups, len(e.fake.api()), e.fake.tokenRequests())
			}
		})
	}
	t.Run("the package refuses without the input schema in front", func(t *testing.T) {
		e := newEnvironment(t, nil, nil)
		for _, value := range []string{`O'Brien`, `say "hi"`, `back\slash`, "line\nbreak"} {
			raw, _ := json.Marshal(map[string]any{"customer_id": clientID, "resource": "campaign",
				"fields": []string{"campaign.id"}, "conditions": []map[string]any{{"field": "campaign.name",
					"operator": "=", "value": value}}})
			_, err := handleReportsSearch(context.Background(), e.resolved(), e.secrets(), e.red, raw)
			if !isInvalidRequest(err) || strings.Contains(err.Error(), value) {
				t.Errorf("%q: err = %v, want invalid-request without the value", value, err)
			}
		}
		if e.lookups != 0 || len(e.fake.api()) != 0 {
			t.Errorf("lookups=%d requests=%d", e.lookups, len(e.fake.api()))
		}
	})
	t.Run("messages never quote a value", func(t *testing.T) {
		e := newEnvironment(t, nil, nil)
		_, err := e.invoke(reportsSearchID, reportArgs(`,"fields":["campaign.zzz-canary"]`))
		_, err2 := e.invoke(reportsSearchID, reportArgs(`,"conditions":[{"field":"campaign.status","operator":"=","value":"zzz-canary"}]`))
		for _, err := range []error{err, err2} {
			if err == nil || strings.Contains(err.Error(), "zzz-canary") {
				t.Errorf("err = %v", err)
			}
		}
	})
}

func TestReportsSearchAcceptsWhatTheCatalogAllows(t *testing.T) {
	for name, extra := range map[string]string{
		"enum equality":        `,"conditions":[{"field":"campaign.status","operator":"!=","value":"REMOVED"}]`,
		"integer as string":    `,"conditions":[{"field":"campaign.id","operator":"=","value":"123456789012345678"}]`,
		"negative double":      `,"conditions":[{"field":"metrics.ctr","operator":"<","value":-0.25}]`,
		"datetime":             `,"conditions":[{"field":"campaign.start_date_time","operator":">","value":"2026-01-01 10:00:00"}]`,
		"LIKE with brackets":   `,"conditions":[{"field":"campaign.name","operator":"NOT LIKE","value":"[[]x[]]%"}]`,
		"ordering default":     `,"orderings":[{"field":"campaign.id"}]`,
		"filter on unselected": `,"conditions":[{"field":"campaign.status","operator":"=","value":"ENABLED"}]`,
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnvironment(t, nil, nil)
			e.fake.routes["POST "+searchPath(clientID)] = reportReply("")
			if _, err := e.invoke(reportsSearchID, reportArgs(extra)); err != nil {
				t.Errorf("%s: %v", extra, err)
			}
		})
	}
}

func TestReportsSearchResult(t *testing.T) {
	t.Run("rows by field name, absent fields are null", func(t *testing.T) {
		e := newEnvironment(t, nil, nil)
		e.fake.routes["POST "+searchPath(clientID)] = reportReply(`,"totalResultsCount":"2","queryResourceConsumption":"42"`,
			campaignRow("1", "5"), `{"campaign":{"id":"2"}}`)
		out, err := e.invoke(reportsSearchID, reportArgs(""))
		if err != nil {
			t.Fatal(err)
		}
		got := decode(t, out)
		rows, _ := got["rows"].([]any)
		if len(rows) != 2 || got["has_more"] != false || got["total_results_count"] != float64(2) ||
			got["query_resource_consumption"] != float64(42) || got["field_mask"] != "campaign.id,metrics.clicks" ||
			!strings.HasPrefix(got["query"].(string), "SELECT campaign.id, metrics.clicks FROM campaign") {
			t.Fatalf("result = %s", out)
		}
		first, second := rows[0].(map[string]any), rows[1].(map[string]any)
		if first["campaign.id"] != "1" || first["metrics.clicks"] != "5" || len(first) != 2 {
			t.Errorf("first = %v", first)
		}
		// The core leaves empty members out of a result, so a null may arrive as a missing key.
		if v := second["metrics.clicks"]; v != nil || second["campaign.id"] != "2" {
			t.Errorf("second = %v", second)
		}
	})
	t.Run("only the selected fields leave the provider", func(t *testing.T) {
		e := newEnvironment(t, nil, nil)
		e.fake.routes["POST "+searchPath(clientID)] = reportReply("",
			`{"campaign":{"id":"1","name":"hidden-name-canary","resourceName":"x"},"metrics":{"clicks":"5","impressions":"9"}}`)
		out, err := e.invoke(reportsSearchID, reportArgs(""))
		if err != nil || strings.Contains(out, "hidden-name-canary") || strings.Contains(out, "impressions") {
			t.Errorf("out = %s, err = %v", out, err)
		}
	})
	t.Run("camel-case names of nested and digit fields", func(t *testing.T) {
		e := newEnvironment(t, nil, nil)
		e.fake.routes["POST "+searchPath(clientID)] = reportReply("",
			`{"adGroupAd":{"ad":{"responsiveSearchAd":{"path1":"p"},"id":"7"}}}`)
		out, err := e.invoke(reportsSearchID, `{"customer_id":"`+clientID+`","resource":"ad_group_ad","fields":["ad_group_ad.ad.id",`+
			`"ad_group_ad.ad.responsive_search_ad.path1"]}`)
		if err != nil {
			t.Fatal(err)
		}
		row := decode(t, out)["rows"].([]any)[0].(map[string]any)
		if row["ad_group_ad.ad.id"] != "7" || row["ad_group_ad.ad.responsive_search_ad.path1"] != "p" {
			t.Errorf("row = %v", row)
		}
	})
	t.Run("has_more from one row more than the limit", func(t *testing.T) {
		e := newEnvironment(t, nil, nil)
		e.fake.routes["POST "+searchPath(clientID)] = reportReply("", campaignRow("1", "1"), campaignRow("2", "2"),
			campaignRow("3", "3"))
		out, err := e.invoke(reportsSearchID, reportArgs(`,"limit":2`))
		if err != nil {
			t.Fatal(err)
		}
		got := decode(t, out)
		if rows, _ := got["rows"].([]any); len(rows) != 2 || got["has_more"] != true {
			t.Errorf("result = %s", out)
		}
		if !strings.HasSuffix(e.fake.api()[0].query, "LIMIT 3") {
			t.Errorf("query = %s", e.fake.api()[0].query)
		}
	})
	t.Run("has_more from the total count", func(t *testing.T) {
		e := newEnvironment(t, nil, nil)
		e.fake.routes["POST "+searchPath(clientID)] = reportReply(`,"totalResultsCount":"250"`, campaignRow("1", "1"))
		out, err := e.invoke(reportsSearchID, reportArgs(`,"limit":100`))
		if err != nil || decode(t, out)["has_more"] != true {
			t.Errorf("out = %s, err = %v", out, err)
		}
	})
	t.Run("default and maximum limit", func(t *testing.T) {
		e := newEnvironment(t, nil, nil)
		e.fake.routes["POST "+searchPath(clientID)] = reportReply("")
		for _, c := range []struct{ extra, want string }{{"", "LIMIT 101"}, {`,"limit":1000`, "LIMIT 1001"}, {`,"limit":1`, "LIMIT 2"}} {
			if _, err := e.invoke(reportsSearchID, reportArgs(c.extra)); err != nil {
				t.Fatal(err)
			}
			requests := e.fake.api()
			if q := requests[len(requests)-1].query; !strings.HasSuffix(q, c.want) {
				t.Errorf("%s: query = %s", c.extra, q)
			}
		}
	})
	t.Run("the output is cut at its byte limit", func(t *testing.T) {
		e := newEnvironment(t, nil, nil)
		big := strings.Repeat("x", 4000)
		var items []string
		for i := 0; i < 400; i++ {
			items = append(items, fmt.Sprintf(`{"campaign":{"id":"%d","name":"%s"}}`, i, big))
		}
		e.fake.routes["POST "+searchPath(clientID)] = reportReply("", items...)
		out, err := e.invoke(reportsSearchID, `{"customer_id":"`+clientID+`","resource":"campaign","fields":["campaign.id","campaign.name"],"limit":400}`)
		if err != nil {
			t.Fatal(err)
		}
		got := decode(t, out)
		rows, _ := got["rows"].([]any)
		if got["truncated"] != true || got["has_more"] != true || len(rows) == 0 || len(rows) >= 400 || len(out) > maxOutputBytes+2048 {
			t.Errorf("rows=%d truncated=%v has_more=%v size=%d", len(rows), got["truncated"], got["has_more"], len(out))
		}
	})
	t.Run("text and structure are bounded", func(t *testing.T) {
		e := newEnvironment(t, nil, nil)
		deep := strings.Repeat(`{"a":`, 20) + `1` + strings.Repeat(`}`, 20)
		e.fake.routes["POST "+searchPath(clientID)] = reportReply("",
			`{"campaign":{"name":"a\u0007b`+strings.Repeat("y", 6000)+`","id":"1"},"metrics":{"clicks":`+deep+`}}`)
		out, err := e.invoke(reportsSearchID, reportArgs(`,"fields":["campaign.name","metrics.clicks"]`))
		if err != nil {
			t.Fatal(err)
		}
		row := decode(t, out)["rows"].([]any)[0].(map[string]any)
		name := row["campaign.name"].(string)
		if len(name) > maxLongText || !strings.HasPrefix(name, "a b") || !strings.HasSuffix(name, "…") {
			t.Errorf("name = %.20q (%d bytes)", name, len(name))
		}
		if strings.Count(out, `{"a":`) >= 20 {
			t.Errorf("depth was not cut: %.200s", out)
		}
	})
	t.Run("messages drop withheld keys wherever they occur", func(t *testing.T) {
		e := newEnvironment(t, nil, nil)
		e.fake.routes["POST "+searchPath(clientID)] = reportReply("",
			`{"multiPartyAuthReview":{"customerUserAccessReview":{"newCustomerUserAccess":{"emailAddress":"a@example.invalid",`+
				`"passkeyEnabled":true,"userId":"5"}}}}`)
		out, err := e.invoke(reportsSearchID, `{"customer_id":"`+clientID+`","resource":"multi_party_auth_review",`+
			`"fields":["multi_party_auth_review.customer_user_access_review.new_customer_user_access"]}`)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out, "passkey") || !strings.Contains(out, "a@example.invalid") {
			t.Errorf("out = %s", out)
		}
	})
	t.Run("summary row holds the selected metrics only", func(t *testing.T) {
		e := newEnvironment(t, nil, nil)
		e.fake.routes["POST "+searchPath(clientID)] = reportReply(`,"summaryRow":{"metrics":{"clicks":"99"}}`, campaignRow("1", "5"))
		out, err := e.invoke(reportsSearchID, reportArgs(`,"summary_row":true`))
		if err != nil {
			t.Fatal(err)
		}
		summary, _ := decode(t, out)["summary_row"].(map[string]any)
		if len(summary) != 1 || summary["metrics.clicks"] != "99" {
			t.Errorf("summary = %v", summary)
		}
	})
	t.Run("omit_results asks for the count only", func(t *testing.T) {
		e := newEnvironment(t, nil, nil)
		e.fake.routes["POST "+searchPath(clientID)] = okReply(`{"totalResultsCount":"4000"}`)
		out, err := e.invoke(reportsSearchID, reportArgs(`,"omit_results":true`))
		if err != nil {
			t.Fatal(err)
		}
		got := decode(t, out)
		settings, _ := lastBody(t, e)["searchSettings"].(map[string]any)
		if rows, _ := got["rows"].([]any); len(rows) != 0 || got["total_results_count"] != float64(4000) ||
			got["has_more"] != false || settings["omitResults"] != true {
			t.Errorf("result = %s, settings = %v", out, settings)
		}
	})
	t.Run("validate_only sends the flag and returns no rows", func(t *testing.T) {
		e := newEnvironment(t, nil, nil)
		e.fake.routes["POST "+searchPath(clientID)] = okReply(`{}`)
		out, err := e.invoke(reportsSearchID, reportArgs(`,"validate_only":true`))
		if err != nil {
			t.Fatal(err)
		}
		got := decode(t, out)
		body := lastBody(t, e)
		if got["validate_only"] != true || got["rows"] != nil || body["validateOnly"] != true ||
			body["searchSettings"] != nil {
			t.Errorf("result = %s, body = %v", out, body)
		}
	})
	t.Run("metric attributes and a row that is not an object", func(t *testing.T) {
		e := newEnvironment(t, nil, nil)
		e.fake.routes["POST "+searchPath(clientID)] = reportReply(`,"metricAttributes":[{"metric":"x","note":"a\u0001b"}]`, campaignRow("1", "1"))
		out, err := e.invoke(reportsSearchID, reportArgs(""))
		if err != nil || !strings.Contains(out, `"metric_attributes":[{"metric":"x","note":"a b"}]`) {
			t.Errorf("out = %s, err = %v", out, err)
		}
		for _, bad := range []reply{
			okReply(`{"results":["text"]}`), okReply(`{"results":[[1]]}`), okReply(`{"results":[],"totalResultsCount":"abc"}`),
			okReply(`{"results":[],"queryResourceConsumption":"-1"}`), okReply(`{"results":[],"summaryRow":"x"}`),
		} {
			e := newEnvironment(t, nil, nil)
			e.fake.routes["POST "+searchPath(clientID)] = bad
			if _, err := e.invoke(reportsSearchID, reportArgs("")); classOf(err) != provider.ClassInvalidResponse {
				t.Errorf("%s: err = %v", bad.body, err)
			}
		}
	})
}

func TestReportsSearchTargets(t *testing.T) {
	t.Run("an account outside the customer targets is refused before secret and request", func(t *testing.T) {
		e := newEnvironment(t, []string{"customers/" + clientID}, nil)
		_, err := e.invoke(reportsSearchID, `{"customer_id":"`+otherID+`","resource":"campaign","fields":["campaign.id"]}`)
		if !isInvalidRequest(err) || strings.Contains(err.Error(), otherID) {
			t.Errorf("err = %v", err)
		}
		if e.lookups != 0 || len(e.fake.api()) != 0 || e.fake.tokenRequests() != 0 {
			t.Errorf("lookups=%d requests=%d tokens=%d", e.lookups, len(e.fake.api()), e.fake.tokenRequests())
		}
	})
	t.Run("customer_id is required and checked in form", func(t *testing.T) {
		e := newEnvironment(t, nil, nil)
		for _, id := range []string{"", "123", "123-456-7890", "12345678901", "1000000003 OR 1=1"} {
			raw := `{"customer_id":"` + id + `","resource":"campaign","fields":["campaign.id"]}`
			if _, err := e.invoke(reportsSearchID, raw); err == nil {
				t.Errorf("customer_id %q accepted", id)
			}
		}
		if _, err := e.invoke(reportsSearchID, `{"resource":"campaign","fields":["campaign.id"]}`); err == nil {
			t.Error("a call without customer_id was accepted")
		}
		if e.lookups != 0 || len(e.fake.api()) != 0 {
			t.Errorf("lookups=%d requests=%d", e.lookups, len(e.fake.api()))
		}
	})
	t.Run("an account below a manager target is read after the proof only", func(t *testing.T) {
		e := newEnvironment(t, []string{"managers/" + managerID}, nil)
		e.fake.answer = func(s seen) *reply {
			if s.path == searchPath(managerID) {
				r := rows(proofItem(clientID))
				return &r
			}
			r := reportReply("", campaignRow("1", "5"))
			return &r
		}
		out, err := e.invoke(reportsSearchID, reportArgs(""))
		if err != nil || len(decode(t, out)["rows"].([]any)) != 1 {
			t.Fatalf("out = %s, err = %v", out, err)
		}
		requests := e.fake.api()
		if len(requests) != 2 || requests[0].path != searchPath(managerID) || requests[1].path != searchPath(clientID) {
			t.Errorf("requests = %+v", requests)
		}
	})
	t.Run("a failed proof ends the call", func(t *testing.T) {
		e := newEnvironment(t, []string{"managers/" + managerID}, nil)
		e.fake.routes["POST "+searchPath(managerID)] = rows()
		e.fake.routes["POST "+searchPath(clientID)] = reportReply("", campaignRow("1", "5"))
		if _, err := e.invoke(reportsSearchID, reportArgs("")); !isInvalidRequest(err) {
			t.Errorf("err = %v", err)
		}
		for _, request := range e.fake.api() {
			if request.path == searchPath(clientID) {
				t.Error("the account was read without a proved membership")
			}
		}
	})
	t.Run("resources that list other accounts need a manager target", func(t *testing.T) {
		for resource := range managerOnly {
			field := resource + ".resource_name"
			arguments := `{"customer_id":"` + managerID + `","resource":"` + resource + `","fields":["` + field + `"]}`
			e := newEnvironment(t, []string{"customers/" + managerID}, nil)
			if _, err := e.invoke(reportsSearchID, arguments); !isInvalidRequest(err) {
				t.Errorf("%s on a customer target: err = %v", resource, err)
			}
			if e.lookups != 0 || len(e.fake.api()) != 0 {
				t.Errorf("%s: lookups=%d requests=%d", resource, e.lookups, len(e.fake.api()))
			}
			e = newEnvironment(t, []string{"managers/" + managerID}, nil)
			e.fake.routes["POST "+searchPath(managerID)] = reportReply("")
			if _, err := e.invoke(reportsSearchID, arguments); err != nil {
				t.Errorf("%s on a manager target: %v", resource, err)
			}
			e = newEnvironment(t, nil, nil)
			e.fake.routes["POST "+searchPath(managerID)] = reportReply("")
			if _, err := e.invoke(reportsSearchID, arguments); err != nil {
				t.Errorf("%s without targets: %v", resource, err)
			}
		}
	})
	t.Run("the manager header is the setting of the connection", func(t *testing.T) {
		e := newEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
		e.fake.routes["POST "+searchPath(clientID)] = reportReply("")
		if _, err := e.invoke(reportsSearchID, reportArgs("")); err != nil {
			t.Fatal(err)
		}
		if requests := e.fake.api(); len(requests) != 1 || requests[0].login != loginID {
			t.Errorf("requests = %+v", requests)
		}
	})
}

func TestReportsSearchErrors(t *testing.T) {
	cases := []struct {
		name   string
		answer reply
		class  provider.Class
		text   string
	}{
		{"quota", reply{status: 429, body: adsFailure(429, "RESOURCE_EXHAUSTED", "quotaError", "RESOURCE_TEMPORARILY_EXHAUSTED",
			`{"rateScope":"ACCOUNT","rateName":"x","retryDelay":"5s"}`)}, provider.ClassRateLimited, "too many requests"},
		{"heavy query", reply{status: 429, body: adsFailure(429, "RESOURCE_EXHAUSTED", "quotaError",
			"EXCESSIVE_SHORT_TERM_QUERY_RESOURCE_CONSUMPTION", "")}, provider.ClassRateLimited, "resource-intensive"},
		{"permission", reply{status: 403, body: adsFailure(403, "PERMISSION_DENIED", "authorizationError",
			"USER_PERMISSION_DENIED", "")}, provider.ClassPermission, "login_customer_id"},
		{"test project", reply{status: 403, body: adsFailure(403, "PERMISSION_DENIED", "authorizationError",
			"CLOUD_PROJECT_NOT_APPROVED_FOR_PRODUCTION", "")}, provider.ClassPermission, "test access"},
		{"query error", reply{status: 400, body: adsFailure(400, "INVALID_ARGUMENT", "queryError",
			"PROHIBITED_METRIC_IN_SELECT_OR_WHERE_CLAUSE", "")}, provider.ClassProviderError, "rejected the query"},
		{"request error", reply{status: 400, body: adsFailure(400, "INVALID_ARGUMENT", "requestError", "PAGE_SIZE_NOT_SUPPORTED", "")},
			provider.ClassInvalidResponse, "API may have changed"},
		{"server error", reply{status: 500, body: googleError(500, "INTERNAL", "")}, provider.ClassProviderError, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newEnvironment(t, nil, nil)
			e.fake.routes["POST "+searchPath(clientID)] = c.answer
			_, err := e.invoke(reportsSearchID, reportArgs(""))
			if classOf(err) != c.class || !strings.Contains(err.Error(), c.text) {
				t.Errorf("err = %v, want %s with %q", err, c.class, c.text)
			}
			if strings.Contains(err.Error(), detailCanary) || strings.Contains(err.Error(), triggerCanary) {
				t.Errorf("error %q carries provider text", err)
			}
			var failed *provider.Error
			if c.name == "query error" && (!asProviderError(err, &failed) || failed.Original == nil) {
				t.Errorf("the code of the query error is missing from the original")
			}
		})
	}
	t.Run("a quota error holds the next request and nothing repeats", func(t *testing.T) {
		e := newEnvironment(t, nil, nil)
		e.fake.routes["POST "+searchPath(clientID)] = cases[0].answer
		_, _ = e.invoke(reportsSearchID, reportArgs(""))
		if len(e.fake.api()) != 1 {
			t.Errorf("requests = %d, want 1", len(e.fake.api()))
		}
	})
}

func asProviderError(err error, target **provider.Error) bool {
	failed, ok := err.(*provider.Error)
	if ok {
		*target = failed
	}
	return ok
}

// The withheld fields must stay in the catalog and every field of the families they belong to must be handled.
func TestWithheldFieldsCoverTheCatalog(t *testing.T) {
	for _, name := range []string{"customer_user_access.passkey_enabled", "third_party_app_analytics_link.shareable_link_id",
		"asset.lead_form_asset.delivery_methods", "android_privacy_shared_key_google_ad_group.shared_ad_group_key",
		"android_privacy_shared_key_google_campaign.shared_campaign_key",
		"android_privacy_shared_key_google_network_type.shared_network_type_key"} {
		if _, ok := LookupField(name); !ok {
			t.Errorf("%s is not in the catalog any more", name)
		}
		if !withheld(name) {
			t.Errorf("%s is not withheld", name)
		}
	}
	for _, f := range catalogFields {
		sensitive := strings.Contains(f.name, "passkey") || strings.Contains(f.name, "shareable") ||
			strings.Contains(f.name, "secret") || strings.Contains(f.name, "webhook") ||
			strings.HasPrefix(f.name, "android_privacy_shared_key_google_") && strings.Contains(f.name, "_key") &&
				strings.Contains(f.name[strings.Index(f.name, "."):], ".shared_")
		if sensitive && !withheld(f.name) {
			t.Errorf("%s looks like a credential or security flag and is not withheld", f.name)
		}
	}
	for _, name := range []string{"campaign.id", "customer_user_access.email_address",
		"android_privacy_shared_key_google_campaign.campaign_id", "asset.lead_form_asset.headline"} {
		if withheld(name) {
			t.Errorf("%s is withheld", name)
		}
	}
}

// Every selectable field of the catalog that a report may name builds into a query, and the catalog types all
// have a literal form, so no field is silently unusable.
func TestEveryFilterableFieldHasAnOperator(t *testing.T) {
	for _, f := range catalogFields {
		field, _ := LookupField(f.name)
		if !field.Filterable || withheld(f.name) {
			continue
		}
		if !operatorFits("IS NULL", field) {
			t.Errorf("%s has no operator", f.name)
		}
	}
	for _, op := range operators {
		fits := 0
		for _, f := range catalogFields {
			field, _ := LookupField(f.name)
			if field.Filterable && operatorFits(op, field) {
				fits++
			}
		}
		if fits == 0 {
			t.Errorf("no filterable field of the catalog takes %s", op)
		}
	}
}
