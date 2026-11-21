package googleads

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const (
	fieldsSearchPath = "/v26/googleAdsFields:search"
	fieldsPathPrefix = "/v26/googleAdsFields/"
)

const statusField = `{"resourceName":"googleAdsFields/campaign.status","name":"campaign.status","category":"ATTRIBUTE",` +
	`"dataType":"ENUM","selectable":true,"filterable":true,"sortable":true,"selectableWith":["campaign","metrics.clicks"],` +
	`"enumValues":["ENABLED","PAUSED","REMOVED","UNKNOWN","UNSPECIFIED"],"typeUrl":"com.google.ads.googleads.v26.enums.CampaignStatusEnum.CampaignStatus",` +
	`"isRepeated":false}`

func TestFieldsGet(t *testing.T) {
	t.Run("maps every documented member", func(t *testing.T) {
		e := newEnvironment(t, nil, nil)
		e.fake.routes["GET "+fieldsPathPrefix+"campaign.status"] = okReply(statusField)
		out, err := e.invoke(fieldsGetID, `{"name":"campaign.status"}`)
		if err != nil {
			t.Fatal(err)
		}
		got := decode(t, out)
		values, _ := got["enum_values"].([]any)
		with, _ := got["selectable_with"].([]any)
		if got["name"] != "campaign.status" || got["resource_name"] != "googleAdsFields/campaign.status" ||
			got["category"] != "ATTRIBUTE" || got["data_type"] != "ENUM" || got["selectable"] != true ||
			got["filterable"] != true || got["sortable"] != true || len(values) != 5 || len(with) != 2 ||
			!strings.HasSuffix(got["type_url"].(string), "CampaignStatus") {
			t.Errorf("result = %s", out)
		}
		requests := e.fake.api()
		if len(requests) != 1 || requests[0].method != "GET" || requests[0].login != "" || requests[0].auth == "" ||
			requests[0].developerToken != "" {
			t.Errorf("requests = %+v", requests)
		}
	})
	t.Run("a manager header is not sent, there is no account", func(t *testing.T) {
		e := newEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
		e.fake.routes["GET "+fieldsPathPrefix+"campaign.status"] = okReply(statusField)
		if _, err := e.invoke(fieldsGetID, `{"name":"campaign.status"}`); err != nil {
			t.Fatal(err)
		}
		if requests := e.fake.api(); len(requests) != 1 || requests[0].login != "" {
			t.Errorf("requests = %+v", requests)
		}
	})
	t.Run("marks a withheld field", func(t *testing.T) {
		e := newEnvironment(t, nil, nil)
		e.fake.routes["GET "+fieldsPathPrefix+"customer_user_access.passkey_enabled"] = okReply(
			`{"name":"customer_user_access.passkey_enabled","category":"ATTRIBUTE","dataType":"BOOLEAN","selectable":true}`)
		out, err := e.invoke(fieldsGetID, `{"name":"customer_user_access.passkey_enabled"}`)
		if err != nil || decode(t, out)["value_withheld"] != true {
			t.Errorf("out = %s, err = %v", out, err)
		}
	})
	t.Run("a resource lists its metrics and segments", func(t *testing.T) {
		e := newEnvironment(t, nil, nil)
		e.fake.routes["GET "+fieldsPathPrefix+"campaign"] = okReply(`{"name":"campaign","category":"RESOURCE",` +
			`"selectable":true,"filterable":false,"sortable":false,"attributeResources":["customer"],"metrics":["metrics.clicks"],` +
			`"segments":["segments.date"]}`)
		out, err := e.invoke(fieldsGetID, `{"name":"campaign"}`)
		got := decode(t, out)
		if err != nil || got["category"] != "RESOURCE" || got["attribute_resources"] == nil || got["metrics"] == nil ||
			got["segments"] == nil || got["resource_name"] != "googleAdsFields/campaign" {
			t.Errorf("out = %s, err = %v", out, err)
		}
	})
	t.Run("long lists and text are bounded", func(t *testing.T) {
		e := newEnvironment(t, nil, nil)
		var names []string
		for i := 0; i < maxFieldList+10; i++ {
			names = append(names, `"metrics.m`+strings.Repeat("x", 3)+`"`)
		}
		e.fake.routes["GET "+fieldsPathPrefix+"campaign"] = okReply(`{"name":"campaign","category":"RESOURCE",` +
			`"selectableWith":[` + strings.Join(names, ",") + `],"typeUrl":"a\u0007` + strings.Repeat("u", 3000) + `"}`)
		out, err := e.invoke(fieldsGetID, `{"name":"campaign"}`)
		if err != nil {
			t.Fatal(err)
		}
		got := decode(t, out)
		if with, _ := got["selectable_with"].([]any); len(with) != maxFieldList || got["lists_truncated"] != true ||
			len(got["type_url"].(string)) > maxText {
			t.Errorf("lists=%d truncated=%v", len(with), got["lists_truncated"])
		}
	})
	t.Run("a malformed name ends before secret and request", func(t *testing.T) {
		e := newEnvironment(t, nil, nil)
		for _, name := range []string{"", "Campaign", "campaign/..", "campaign?x=1", "campaign status", "googleAdsFields/campaign",
			"campaign.*", "-campaign", strings.Repeat("a", 129)} {
			raw, _ := json.Marshal(map[string]string{"name": name})
			if _, err := e.invoke(fieldsGetID, string(raw)); err == nil {
				t.Errorf("name %q accepted", name)
			}
			if _, err := handleFieldsGet(context.Background(), e.resolved(), e.secrets(), e.red, raw); !isInvalidRequest(err) {
				t.Errorf("name %q: err = %v", name, err)
			}
		}
		if e.lookups != 0 || len(e.fake.api()) != 0 || e.fake.tokenRequests() != 0 {
			t.Errorf("lookups=%d requests=%d tokens=%d", e.lookups, len(e.fake.api()), e.fake.tokenRequests())
		}
	})
	t.Run("another field than asked for is invalid", func(t *testing.T) {
		for _, body := range []string{statusField, `{"name":"campaign.status","resourceName":"googleAdsFields/campaign.name"}`,
			`{"name":"Campaign.Status"}`, `{}`} {
			e := newEnvironment(t, nil, nil)
			e.fake.routes["GET "+fieldsPathPrefix+"campaign.name"] = okReply(body)
			if _, err := e.invoke(fieldsGetID, `{"name":"campaign.name"}`); classOf(err) != provider.ClassInvalidResponse {
				t.Errorf("%.60s: err = %v", body, err)
			}
		}
	})
	t.Run("errors keep their classes", func(t *testing.T) {
		for _, c := range []struct {
			answer reply
			class  provider.Class
		}{
			{reply{status: 404, body: googleError(404, "NOT_FOUND", "")}, provider.ClassNotFound},
			{reply{status: 429, body: adsFailure(429, "RESOURCE_EXHAUSTED", "quotaError", "RESOURCE_TEMPORARILY_EXHAUSTED", "")},
				provider.ClassRateLimited},
			{reply{status: 403, body: adsFailure(403, "PERMISSION_DENIED", "authorizationError", "CLOUD_PROJECT_NOT_APPROVED_FOR_PRODUCTION", "")},
				provider.ClassPermission},
		} {
			e := newEnvironment(t, nil, nil)
			e.fake.routes["GET "+fieldsPathPrefix+"campaign.nope"] = c.answer
			_, err := e.invoke(fieldsGetID, `{"name":"campaign.nope"}`)
			if classOf(err) != c.class || strings.Contains(err.Error(), detailCanary) {
				t.Errorf("err = %v, want %s", err, c.class)
			}
		}
	})
}

func fieldsReply(token string, names ...string) reply {
	var items []string
	for _, name := range names {
		items = append(items, `{"resourceName":"googleAdsFields/`+name+`","name":"`+name+`","category":"METRIC","dataType":"INT64",`+
			`"selectable":true,"filterable":true,"sortable":true}`)
	}
	next := ""
	if token != "" {
		next = `,"nextPageToken":"` + token + `"`
	}
	return okReply(`{"results":[` + strings.Join(items, ",") + `],"totalResultsCount":"7"` + next + `}`)
}

func TestFieldsSearch(t *testing.T) {
	t.Run("filters become the query of the metadata service", func(t *testing.T) {
		e := newEnvironment(t, nil, nil)
		e.fake.routes["POST "+fieldsSearchPath] = fieldsReply("")
		_, err := e.invoke(fieldsSearchID, `{"name_prefix":"metrics.conv_","name_contains":"value","category":"METRIC",`+
			`"selectable":true,"filterable":false,"sortable":true,"is_repeated":false,"page_size":10}`)
		if err != nil {
			t.Fatal(err)
		}
		want := "SELECT name, category, data_type, selectable, filterable, sortable, selectable_with, " +
			"attribute_resources, metrics, segments, enum_values, type_url, is_repeated WHERE " +
			"name LIKE 'metrics.conv[_]%' AND name LIKE '%value%' AND category = 'METRIC' AND selectable = TRUE AND " +
			"filterable = FALSE AND sortable = TRUE AND is_repeated = FALSE"
		requests := e.fake.api()
		if len(requests) != 1 || requests[0].query != want || requests[0].method != "POST" || requests[0].login != "" {
			t.Fatalf("requests = %+v\nwant query %q", requests, want)
		}
		if body := lastBody(t, e); body["pageSize"] != float64(10) || body["pageToken"] != nil {
			t.Errorf("body = %v", body)
		}
	})
	t.Run("no filter, no WHERE, default page size", func(t *testing.T) {
		e := newEnvironment(t, nil, nil)
		e.fake.routes["POST "+fieldsSearchPath] = fieldsReply("")
		if _, err := e.invoke(fieldsSearchID, `{}`); err != nil {
			t.Fatal(err)
		}
		if q := e.fake.api()[0].query; strings.Contains(q, "WHERE") {
			t.Errorf("query = %s", q)
		}
		if body := lastBody(t, e); body["pageSize"] != float64(defaultFieldPage) {
			t.Errorf("body = %v", body)
		}
	})
	t.Run("pages with a cursor bound to the arguments", func(t *testing.T) {
		e := newEnvironment(t, nil, nil)
		e.fake.answer = func(s seen) *reply {
			if strings.Contains(s.body, `"pageToken":"tok-2_a="`) {
				r := fieldsReply("", "metrics.c")
				return &r
			}
			r := fieldsReply("tok-2_a=", "metrics.a", "metrics.b")
			return &r
		}
		out, err := e.invoke(fieldsSearchID, `{"category":"METRIC","page_size":2}`)
		if err != nil {
			t.Fatal(err)
		}
		got := decode(t, out)
		cursor, _ := got["next_cursor"].(string)
		fields, _ := got["fields"].([]any)
		if len(fields) != 2 || got["has_more"] != true || cursor == "" || strings.Contains(cursor, "tok-2_a=") ||
			got["total_results_count"] != float64(7) {
			t.Fatalf("result = %s", out)
		}
		out, err = e.invoke(fieldsSearchID, `{"category":"METRIC","page_size":2,"cursor":"`+cursor+`"}`)
		if err != nil {
			t.Fatal(err)
		}
		if got = decode(t, out); len(got["fields"].([]any)) != 1 || got["has_more"] == true || got["next_cursor"] != nil {
			t.Errorf("second page = %s", out)
		}
		before := len(e.fake.api())
		for _, other := range []string{`{"category":"SEGMENT","page_size":2,"cursor":"` + cursor + `"}`,
			`{"category":"METRIC","page_size":3,"cursor":"` + cursor + `"}`, `{"category":"METRIC","cursor":"forged"}`} {
			if _, err := e.invoke(fieldsSearchID, other); err == nil {
				t.Errorf("%s accepted", other)
			}
		}
		if len(e.fake.api()) != before {
			t.Error("a foreign cursor reached Google")
		}
	})
	t.Run("a cursor does not fit another connection setting", func(t *testing.T) {
		e := newEnvironment(t, nil, nil)
		e.fake.routes["POST "+fieldsSearchPath] = fieldsReply("tok", "metrics.a")
		out, err := e.invoke(fieldsSearchID, `{"page_size":1}`)
		if err != nil {
			t.Fatal(err)
		}
		cursor := decode(t, out)["next_cursor"].(string)
		other := newEnvironment(t, []string{"managers/" + managerID}, nil)
		if _, err := other.invoke(fieldsSearchID, `{"page_size":1,"cursor":"`+cursor+`"}`); err == nil {
			t.Error("a cursor of another connection was accepted")
		}
	})
	t.Run("a page token Qatlas cannot carry is invalid", func(t *testing.T) {
		e := newEnvironment(t, nil, nil)
		e.fake.routes["POST "+fieldsSearchPath] = fieldsReply("tok en!", "metrics.a")
		if _, err := e.invoke(fieldsSearchID, `{}`); classOf(err) != provider.ClassInvalidResponse {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("invalid filters end before secret and request", func(t *testing.T) {
		e := newEnvironment(t, nil, nil)
		for _, args := range []string{`{"name_prefix":"Metrics"}`, `{"name_prefix":"a'b"}`, `{"name_contains":"a\"b"}`,
			`{"name_contains":"a\\b"}`, `{"name_contains":"a b"}`, `{"name_prefix":"metrics%"}`, `{"category":"FIELD"}`,
			`{"page_size":0}`, `{"page_size":201}`, `{"query":"SELECT name"}`, `{"customer_id":"` + clientID + `"}`,
			`{"selectable":"yes"}`} {
			if _, err := e.invoke(fieldsSearchID, args); err == nil {
				t.Errorf("%s accepted", args)
			}
		}
		for _, value := range []string{`a'b`, `a"b`, `a\b`, "a\nb", `a%b`} {
			raw, _ := json.Marshal(map[string]string{"name_contains": value})
			if _, err := handleFieldsSearch(context.Background(), e.resolved(), e.secrets(), e.red, raw); !isInvalidRequest(err) ||
				strings.Contains(err.Error(), value) {
				t.Errorf("%q: err = %v", value, err)
			}
		}
		if e.lookups != 0 || len(e.fake.api()) != 0 || e.fake.tokenRequests() != 0 {
			t.Errorf("lookups=%d requests=%d tokens=%d", e.lookups, len(e.fake.api()), e.fake.tokenRequests())
		}
	})
	t.Run("invalid fields in the answer", func(t *testing.T) {
		for _, body := range []string{`{"results":[{"name":"Bad Name"}]}`, `{"results":[{"name":"a.b","resourceName":"googleAdsFields/c.d"}]}`,
			`{"results":[],"totalResultsCount":"x"}`} {
			e := newEnvironment(t, nil, nil)
			e.fake.routes["POST "+fieldsSearchPath] = okReply(body)
			if _, err := e.invoke(fieldsSearchID, `{}`); classOf(err) != provider.ClassInvalidResponse {
				t.Errorf("%s: err = %v", body, err)
			}
		}
	})
	t.Run("the page is cut at its byte limit without a cursor", func(t *testing.T) {
		e := newEnvironment(t, nil, nil)
		var names []string
		for i := 0; i < maxFieldPage; i++ {
			names = append(names, `"metrics.x`+strings.Repeat("a", 100)+`"`)
		}
		list := strings.Join(names, ",")
		var items []string
		for i := 0; i < maxFieldPage; i++ {
			items = append(items, `{"name":"metrics.f`+strings.Repeat("b", i%9)+`","category":"METRIC","selectableWith":[`+list+`]}`)
		}
		e.fake.routes["POST "+fieldsSearchPath] = okReply(`{"results":[` + strings.Join(items, ",") + `],"nextPageToken":"tok"}`)
		out, err := e.invoke(fieldsSearchID, `{"page_size":200}`)
		if err != nil {
			t.Fatal(err)
		}
		got := decode(t, out)
		if got["truncated"] != true || got["has_more"] != true || got["next_cursor"] != nil {
			t.Errorf("truncated=%v has_more=%v cursor=%v", got["truncated"], got["has_more"], got["next_cursor"])
		}
	})
	t.Run("errors keep their classes", func(t *testing.T) {
		for _, c := range []struct {
			answer reply
			class  provider.Class
		}{
			{reply{status: 400, body: adsFailure(400, "INVALID_ARGUMENT", "queryError", "BAD_FIELD_NAME", "")}, provider.ClassInvalidResponse},
			{reply{status: 429, body: adsFailure(429, "RESOURCE_EXHAUSTED", "quotaError", "RESOURCE_EXHAUSTED", "")}, provider.ClassRateLimited},
			{reply{status: 401, body: googleError(401, "UNAUTHENTICATED", "")}, provider.ClassAuth},
		} {
			e := newEnvironment(t, nil, nil)
			e.fake.routes["POST "+fieldsSearchPath] = c.answer
			_, err := e.invoke(fieldsSearchID, `{}`)
			if classOf(err) != c.class || strings.Contains(err.Error(), detailCanary) {
				t.Errorf("err = %v, want %s", err, c.class)
			}
		}
	})
}

func TestFieldToolsFollowTheConnectionCheck(t *testing.T) {
	e := newEnvironment(t, nil, nil)
	resolved := e.resolved()
	resolved.BaseURL = "https://example.invalid"
	for name, call := range map[string]func() error{
		"fields.search": func() error {
			_, err := handleFieldsSearch(context.Background(), resolved, e.secrets(), e.red, json.RawMessage(`{}`))
			return err
		},
		"fields.get": func() error {
			_, err := handleFieldsGet(context.Background(), resolved, e.secrets(), e.red, json.RawMessage(`{"name":"campaign"}`))
			return err
		},
	} {
		if classOf(call()) != provider.ClassProviderError {
			t.Errorf("%s accepted a foreign origin", name)
		}
	}
	if e.lookups != 0 || len(e.fake.api()) != 0 || e.fake.tokenRequests() != 0 {
		t.Errorf("lookups=%d requests=%d tokens=%d", e.lookups, len(e.fake.api()), e.fake.tokenRequests())
	}
}
