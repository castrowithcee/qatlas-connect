package googleanalytics

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const (
	adsLinkOneJSON = `{"name":"properties/200/googleAdsLinks/11","customerId":"1234567890","canManageClients":true,` +
		`"adsPersonalizationEnabled":false,"creatorEmailAddress":"owner@example.com",` +
		`"createTime":"2024-01-02T03:04:05.123456Z","updateTime":"2024-02-03T04:05:06Z","extra":1}`
	adsLinkTwoJSON   = `{"name":"properties/200/googleAdsLinks/12","customerId":"2345678901"}`
	adsLinkThreeJSON = `{"name":"properties/200/googleAdsLinks/13","customerId":"3456789012","adsPersonalizationEnabled":true}`

	firebaseOneJSON = `{"name":"properties/200/firebaseLinks/21","project":"projects/1234567",` +
		`"createTime":"2024-01-02T03:04:05Z","extra":1}`
	firebaseTwoJSON   = `{"name":"properties/200/firebaseLinks/22","project":"projects/2345678"}`
	firebaseThreeJSON = `{"name":"properties/200/firebaseLinks/23","project":"projects/3456789"}`

	bigQueryFullJSON = `{"name":"properties/200/bigQueryLinks/abc567","project":"projects/1234567",` +
		`"datasetLocation":"europe-west6","dailyExportEnabled":true,"freshDailyExportEnabled":false,` +
		`"streamingExportEnabled":true,"includeAdvertisingId":true,` +
		`"exportStreams":["properties/200/dataStreams/2001","properties/200/dataStreams/2002"],` +
		`"excludedEvents":["scroll","user_engagement"],"createTime":"2024-01-02T03:04:05Z","extra":1}`
	bigQueryTwoJSON   = `{"name":"properties/200/bigQueryLinks/def890","project":"projects/2345678"}`
	bigQueryThreeJSON = `{"name":"properties/200/bigQueryLinks/ghi123","project":"projects/3456789"}`
)

// linkTools are the four tools of the link tests with their list key and the path of the request.
var linkListTools = []struct {
	id, key, path string
	items         [3]string
}{
	{adsLinksListID, "googleAdsLinks", "/v1beta/properties/200/googleAdsLinks",
		[3]string{adsLinkOneJSON, adsLinkTwoJSON, adsLinkThreeJSON}},
	{firebaseLinksListID, "firebaseLinks", "/v1beta/properties/200/firebaseLinks",
		[3]string{firebaseOneJSON, firebaseTwoJSON, firebaseThreeJSON}},
	{bigQueryLinksListID, "bigqueryLinks", "/v1alpha/properties/200/bigQueryLinks",
		[3]string{bigQueryFullJSON, bigQueryTwoJSON, bigQueryThreeJSON}},
}

func TestLinkListsPageThroughThreePages(t *testing.T) {
	for _, tool := range linkListTools {
		env := newEnv(t, &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
			body := `{"` + tool.key + `":[` + tool.items[n-1] + `]`
			if n < 3 {
				body += `,"nextPageToken":"t` + string(rune('1'+n)) + `"`
			}
			return respond(r, http.StatusOK, "application/json", body+`}`), nil
		}})
		cursor, names := "", []string{}
		for i := 0; i < 3; i++ {
			args := `{"property":"properties/200","page_size":1`
			if cursor != "" {
				args += `,"cursor":"` + cursor + `"`
			}
			result, err := env.call(tool.id, "read", args+`}`)
			if err != nil {
				t.Fatalf("%s: %v", tool.id, err)
			}
			var out map[string]json.RawMessage
			var items []struct{ Name string }
			var more bool
			var next string
			if json.Unmarshal([]byte(result), &out) != nil || json.Unmarshal(out[tool.key], &items) != nil ||
				len(items) != 1 || json.Unmarshal(out["has_more"], &more) != nil || more != (i < 2) ||
				strings.Contains(result, "extra") {
				t.Fatalf("%s page %d = %s", tool.id, i, result)
			}
			names = append(names, items[0].Name)
			_ = json.Unmarshal(out["next_cursor"], &next)
			cursor = next
		}
		requests, _ := env.fake.seen()
		if len(names) != 3 || len(requests) != 3 || requests[0].path != tool.path || requests[0].rawQuery != "pageSize=1" ||
			!strings.Contains(requests[1].rawQuery, "pageToken=t2") || !strings.Contains(requests[2].rawQuery, "pageToken=t3") {
			t.Errorf("%s: names %v, requests %+v", tool.id, names, requests)
		}
		if strings.Contains(strings.Join(names, ""), "extra") {
			t.Errorf("%s: names %v", tool.id, names)
		}
		// A cursor fits only the property it came from.
		if _, err := env.call(tool.id, "read", `{"property":"properties/201","cursor":"`+cursor+`"}`); cursor != "" && err == nil {
			t.Errorf("%s: a cursor of another property was accepted", tool.id)
		}
	}
}

func TestLinkListsEmptyAnswerIsAnEmptyList(t *testing.T) {
	for _, tool := range linkListTools {
		env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, `{}`)})
		result, err := env.call(tool.id, "read", `{"property":"properties/200"}`)
		// The core leaves an empty list out of the result: a missing member means empty.
		if err != nil || result != `{"has_more":false}` {
			t.Errorf("%s: %s, %v", tool.id, result, err)
		}
	}
}

func TestAdsLinksReturnEveryMemberAndAreBounded(t *testing.T) {
	env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"googleAdsLinks":[`+adsLinkOneJSON+`,`+
		`{"name":"properties/999/googleAdsLinks/1"},{"name":"nonsense"},{"customerId":"1"},`+
		`{"name":"properties/200/googleAdsLinks/14","creatorEmailAddress":"`+strings.Repeat("a", 400)+`@example.com",`+
		`"customerId":"has blank"}]}`)})
	result, err := env.call(adsLinksListID, "read", `{"property":"properties/200"}`)
	var out struct {
		GoogleAdsLinks []adsLink `json:"googleAdsLinks"`
	}
	if err != nil || json.Unmarshal([]byte(result), &out) != nil || len(out.GoogleAdsLinks) != 2 {
		t.Fatalf("result = %s, %v", result, err)
	}
	first, second := out.GoogleAdsLinks[0], out.GoogleAdsLinks[1]
	if first.Name != "properties/200/googleAdsLinks/11" || first.CustomerID != "1234567890" ||
		first.CanManageClients == nil || !*first.CanManageClients ||
		first.AdsPersonalizationEnabled == nil || *first.AdsPersonalizationEnabled ||
		first.CreatorEmailAddress != "owner@example.com" || first.CreateTime != "2024-01-02T03:04:05.123456Z" ||
		first.UpdateTime != "2024-02-03T04:05:06Z" {
		t.Errorf("first = %+v", first)
	}
	if len([]rune(string(second.CreatorEmailAddress))) != maxTextRunes+1 || second.CustomerID != "" ||
		second.CanManageClients != nil {
		t.Errorf("second = %+v", second)
	}
}

func TestAdsLinksAreClassifiedAsPersonalData(t *testing.T) {
	reg := registry(t)
	for _, d := range reg.Provider(Provider) {
		personal := slices.Contains(d.Tags, "personal-data") && strings.Contains(d.ID, "links")
		if (d.ID == adsLinksListID) != personal {
			t.Errorf("%s: personal-data tag = %t", d.ID, personal)
		}
		if d.ID == adsLinksListID && (d.Risk != adsLinksRisk || d.Risk.DataSensitivity == dataSensitivity ||
			!strings.Contains(d.Description, "PERSONAL DATA")) {
			t.Errorf("%s: risk %+v, description %q", d.ID, d.Risk, d.Description)
		}
		if strings.Contains(d.ID, "links") && d.ID != adsLinksListID && d.Risk != readRisk {
			t.Errorf("%s risk = %+v", d.ID, d.Risk)
		}
	}
}

func TestFirebaseLinksCheckTheProjectName(t *testing.T) {
	env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"firebaseLinks":[`+firebaseOneJSON+`,`+
		`{"name":"properties/200/firebaseLinks/24","project":"projects/my-project"},`+
		`{"name":"properties/200/firebaseLinks/25","project":"https://evil.example/projects/1"}]}`)})
	result, err := env.call(firebaseLinksListID, "read", `{"property":"properties/200"}`)
	var out struct {
		FirebaseLinks []firebaseLink `json:"firebaseLinks"`
	}
	if err != nil || json.Unmarshal([]byte(result), &out) != nil || len(out.FirebaseLinks) != 3 {
		t.Fatalf("result = %s, %v", result, err)
	}
	if first := out.FirebaseLinks[0]; first.Project != "projects/1234567" || first.CreateTime != "2024-01-02T03:04:05Z" ||
		out.FirebaseLinks[1].Project != "" || out.FirebaseLinks[2].Project != "" || strings.Contains(result, "evil") {
		t.Errorf("links = %+v", out.FirebaseLinks)
	}
}

func TestBigQueryLinkReturnsEveryExportSetting(t *testing.T) {
	env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, bigQueryFullJSON)})
	result, err := env.call(bigQueryLinksGetID, "read", `{"name":"properties/200/bigQueryLinks/abc567"}`)
	var got bigQueryLink
	if err != nil || json.Unmarshal([]byte(result), &got) != nil || strings.Contains(result, "extra") {
		t.Fatalf("result = %s, %v", result, err)
	}
	on := func(b *bool) bool { return b != nil && *b }
	if got.Name != "properties/200/bigQueryLinks/abc567" || got.Project != "projects/1234567" ||
		got.DatasetLocation != "europe-west6" || !on(got.DailyExportEnabled) || got.FreshDailyExportEnabled == nil ||
		*got.FreshDailyExportEnabled || !on(got.StreamingExportEnabled) || !on(got.IncludeAdvertisingID) ||
		!slices.Equal(got.ExportStreams, []streamRef{"properties/200/dataStreams/2001", "properties/200/dataStreams/2002"}) ||
		!slices.Equal(got.ExcludedEvents, []text{"scroll", "user_engagement"}) || got.CreateTime != "2024-01-02T03:04:05Z" {
		t.Errorf("link = %+v", got)
	}
	requests, _ := env.fake.seen()
	if len(requests) != 1 || requests[0].method != http.MethodGet ||
		requests[0].path != "/v1alpha/properties/200/bigQueryLinks/abc567" || requests[0].rawQuery != "" {
		t.Errorf("requests = %+v", requests)
	}
}

func TestBigQueryLinkAnswersAreBoundedAndChecked(t *testing.T) {
	var events []string
	for i := 0; i < maxExcludedEvents+5; i++ {
		events = append(events, `"e`+strings.Repeat("x", 3)+`"`)
	}
	body := `{"name":"properties/200/bigQueryLinks/abc567","project":"projects/12","datasetLocation":"has blank",` +
		`"exportStreams":["properties/999/dataStreams/1","nonsense","properties/200/dataStreams/7"],` +
		`"excludedEvents":[` + strings.Join(events, ",") + `]}`
	env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, body)})
	result, err := env.call(bigQueryLinksGetID, "read", `{"name":"properties/200/bigQueryLinks/abc567"}`)
	var got bigQueryLink
	if err != nil || json.Unmarshal([]byte(result), &got) != nil ||
		!slices.Equal(got.ExportStreams, []streamRef{"properties/200/dataStreams/7"}) ||
		len(got.ExcludedEvents) != maxExcludedEvents || got.DatasetLocation != "" || strings.Contains(result, "999") {
		t.Fatalf("result = %v, %v", len(result), err)
	}
	// An answer for another link than the one asked for is refused.
	for _, answer := range []string{bigQueryTwoJSON, `{"name":"properties/999/bigQueryLinks/abc567"}`, `{}`} {
		env = newEnv(t, &fakeGoogle{api: always(http.StatusOK, answer)})
		if _, err := env.call(bigQueryLinksGetID, "read", `{"name":"properties/200/bigQueryLinks/abc567"}`); classOf(err) != provider.ClassInvalidResponse {
			t.Errorf("%s: %v", answer, err)
		}
	}
	// The list drops links of other properties and without a valid name.
	env = newEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"bigqueryLinks":[`+bigQueryTwoJSON+
		`,{"name":"properties/999/bigQueryLinks/a"},{"name":"properties/200/bigQueryLinks/a b"},{"project":"projects/1"}]}`)})
	result, err = env.call(bigQueryLinksListID, "read", `{"property":"properties/200"}`)
	if err != nil || strings.Count(result, `"name"`) != 1 || strings.Contains(result, "999") {
		t.Errorf("list = %s, %v", result, err)
	}
}

func TestLinkToolsRefuseBadArgumentsBeforeAnyIO(t *testing.T) {
	env := newEnv(t, &fakeGoogle{})
	for _, c := range []struct{ id, arguments string }{
		{adsLinksListID, `{}`}, {adsLinksListID, `{"property":"accounts/100"}`},
		{firebaseLinksListID, `{"property":"properties/0"}`}, {firebaseLinksListID, `{"property":"properties/1/x"}`},
		{bigQueryLinksListID, `{"property":"properties/200","page_size":0}`},
		{bigQueryLinksListID, `{"property":"properties/200","page_size":201}`},
		{bigQueryLinksListID, `{"property":"properties/200","cursor":"x"}`},
		{bigQueryLinksListID, `{"property":"properties/200","extra":1}`},
		{bigQueryLinksGetID, `{}`}, {bigQueryLinksGetID, `{"name":"properties/200"}`},
		{bigQueryLinksGetID, `{"name":"properties/200/bigQueryLinks/"}`},
		{bigQueryLinksGetID, `{"name":"properties/200/bigQueryLinks/a/b"}`},
		{bigQueryLinksGetID, `{"name":"properties/200/bigQueryLinks/a?x=1"}`},
		{bigQueryLinksGetID, `{"name":"properties/200/bigQueryLinks/..%2f"}`},
		{bigQueryLinksGetID, `{"name":"properties/200/firebaseLinks/1"}`},
		{bigQueryLinksGetID, `{"name":"properties/0/bigQueryLinks/a"}`},
	} {
		if _, err := env.call(c.id, "read", c.arguments); !refusedInvalid(err) {
			t.Errorf("%s %s: %v", c.id, c.arguments, err)
		}
	}
	env.noIO(t)
}

func TestLinkToolsFollowTheBindingBeforeAnyIO(t *testing.T) {
	for _, tool := range []struct{ id, arguments string }{
		{adsLinksListID, `{"property":"PROP"}`},
		{firebaseLinksListID, `{"property":"PROP"}`},
		{bigQueryLinksListID, `{"property":"PROP"}`},
		{bigQueryLinksGetID, `{"name":"PROP/bigQueryLinks/abc567"}`},
	} {
		for _, c := range []struct {
			connection, property string
			methods              []string
			refused              bool
		}{
			{"read", "properties/200", []string{"X"}, false},
			{"property", "properties/200", []string{"X"}, false},
			{"property", "properties/201", nil, true},
			{"property", "properties/901", nil, true},
			{"account", "properties/201", []string{"GET", "X"}, false},
			{"account", "properties/901", []string{"GET"}, true},
			{"mixed", "properties/901", []string{"X"}, false},
			{"mixed", "properties/200", []string{"GET", "X"}, false},
		} {
			answers := map[string]string{
				"/v1beta/properties/200": `{"name":"properties/200","parent":"accounts/100"}`,
				"/v1beta/properties/201": `{"name":"properties/201","parent":"accounts/100"}`,
				"/v1beta/properties/901": `{"name":"properties/901","parent":"accounts/900"}`,
			}
			env := newEnv(t, &fakeGoogle{api: func(_ int, r *http.Request) (*http.Response, error) {
				body, ok := answers[r.URL.Path]
				if !ok {
					body = `{"name":"` + strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/v1alpha/"), "/v1beta/") + `"}`
				}
				return respond(r, http.StatusOK, "application/json", body), nil
			}})
			result, err := env.call(tool.id, c.connection, strings.ReplaceAll(tool.arguments, "PROP", c.property))
			requests, _ := env.fake.seen()
			want := slices.Clone(c.methods)
			if i := slices.Index(want, "X"); i >= 0 {
				want[i] = "GET"
			}
			if (err != nil) != c.refused || !slices.Equal(methodsOf(requests), want) {
				t.Errorf("%s %s %s: %v, requests %v, want %v", tool.id, c.connection, c.property, err,
					methodsOf(requests), want)
			}
			if c.refused && (!refusedInvalid(err) || strings.Contains(err.Error(), "901") || strings.Contains(result, "901")) {
				t.Errorf("%s %s %s: refusal %v", tool.id, c.connection, c.property, err)
			}
			if c.refused && len(c.methods) == 0 {
				env.noIO(t)
			}
		}
	}
}

func TestLinkToolsClassifyErrorsAndNeverRepeat(t *testing.T) {
	calls := []struct{ id, arguments string }{
		{adsLinksListID, `{"property":"properties/200"}`},
		{firebaseLinksListID, `{"property":"properties/200"}`},
		{bigQueryLinksListID, `{"property":"properties/200"}`},
		{bigQueryLinksGetID, `{"name":"properties/200/bigQueryLinks/abc567"}`},
	}
	for _, c := range []struct {
		name  string
		api   func(int, *http.Request) (*http.Response, error)
		class provider.Class
	}{
		{"403", always(http.StatusForbidden, errorObject(403, "PERMISSION_DENIED", "ACCESS_TOKEN_SCOPE_INSUFFICIENT")),
			provider.ClassPermission},
		{"404", always(http.StatusNotFound, errorObject(404, "NOT_FOUND", "")), provider.ClassNotFound},
		{"429", always(http.StatusTooManyRequests, errorObject(429, "RESOURCE_EXHAUSTED", "")), provider.ClassRateLimited},
		{"500", always(http.StatusInternalServerError, errorObject(500, "INTERNAL", "")), provider.ClassProviderError},
	} {
		for _, call := range calls {
			t.Run(c.name+" "+call.id, func(t *testing.T) {
				env := newEnv(t, &fakeGoogle{api: c.api})
				_, err := env.call(call.id, "read", call.arguments)
				if classOf(err) != c.class || leaks(err.Error()) != "" {
					t.Fatalf("error = %v", err)
				}
				if requests, _ := env.fake.seen(); len(requests) != 1 {
					t.Errorf("requests = %d, want exactly one", len(requests))
				}
			})
		}
	}
}

func TestLink429WithoutRetryAfterHoldsTheNextRequest(t *testing.T) {
	env := newEnv(t, &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
		if n == 1 {
			return respond(r, http.StatusTooManyRequests, "application/json", errorObject(429, "RESOURCE_EXHAUSTED", "")), nil
		}
		return respond(r, http.StatusOK, "application/json", bigQueryFullJSON), nil
	}})
	args := `{"name":"properties/200/bigQueryLinks/abc567"}`
	if _, err := env.call(bigQueryLinksGetID, "read", args); classOf(err) != provider.ClassRateLimited {
		t.Fatalf("error = %v", err)
	}
	if _, err := env.call(bigQueryLinksGetID, "read", args); err != nil {
		t.Fatal(err)
	}
	if requests, _ := env.fake.seen(); len(requests) != 2 {
		t.Errorf("requests = %d, want 2 without a retry", len(requests))
	}
	if waits := *env.waits; len(waits) == 0 || slices.Max(waits) < 900_000_000 {
		t.Errorf("waits = %v, want the next request held for about a second", waits)
	}
}

func TestLinksHaveOnlyReadingTools(t *testing.T) {
	reg := registry(t)
	var links []string
	for _, d := range reg.Provider(Provider) {
		id := strings.ToLower(d.ID)
		if !strings.Contains(id, "-links.") {
			continue
		}
		links = append(links, d.ID)
		if effectOf(d.ID) != config.EffectRead || !strings.HasSuffix(id, ".list") && !strings.HasSuffix(id, ".get") ||
			d.Risk.Effect != "read" {
			t.Errorf("%s is not a reading tool", d.ID)
		}
	}
	slices.Sort(links)
	if !slices.Equal(links, sortedCopy([]string{adsLinksListID, firebaseLinksListID, bigQueryLinksListID, bigQueryLinksGetID,
		dv360LinksListID, dv360LinksGetID, adsenseLinksListID, adsenseLinksGetID, searchAdsLinksListID,
		searchAdsLinksGetID})) {
		t.Errorf("link tools = %v", links)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	for _, profile := range metadata.Profiles {
		for _, id := range profile.Tools {
			if strings.Contains(id, "-links.") && !slices.Contains(links, id) {
				t.Errorf("profile %s holds %s", profile.ID, id)
			}
		}
	}
}
