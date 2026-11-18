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

// linkEnv adds connections that list the four link tools, with the bindings of the stream tests.
func linkEnv(t *testing.T, fake *fakeGoogle) *testEnv {
	t.Helper()
	env := newEnv(t, fake)
	tools := []string{adsenseLinksListID, adsenseLinksGetID, searchAdsLinksListID, searchAdsLinksGetID}
	for name, targets := range map[string][]string{"sfree": nil, "saccount": {"accounts/100"},
		"sproperty": {"properties/200"}, "smixed": {"accounts/100", "properties/901"}} {
		env.cfg.Connections[name] = config.Connection{Service: "ga", Credential: "login", Targets: targets, Tools: tools}
	}
	if err := env.cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	return env
}

const (
	adsenseOneJSON   = `{"name":"properties/200/adSenseLinks/301","adClientCode":"ca-pub-0000000001","extra":1}`
	adsenseTwoJSON   = `{"name":"properties/200/adSenseLinks/302","adClientCode":"ca-pub-0000000002"}`
	adsenseThreeJSON = `{"name":"properties/200/adSenseLinks/303","adClientCode":"ca-pub-0000000003"}`

	searchOneJSON = `{"name":"properties/200/searchAds360Links/401","advertiserId":"5001","advertiserDisplayName":"Sample advertiser",` +
		`"campaignDataSharingEnabled":true,"costDataSharingEnabled":false,"adsPersonalizationEnabled":true,` +
		`"siteStatsSharingEnabled":false,"extra":1}`
	searchTwoJSON   = `{"name":"properties/200/searchAds360Links/402","advertiserId":"5002"}`
	searchThreeJSON = `{"name":"properties/200/searchAds360Links/403","advertiserId":"5003"}`
)

// linkKinds drives the tests that both link groups share.
var linkKinds = []struct {
	name, listID, getID, member, segment, ownName string
	pages                                         [3]string
}{
	{"adsense", adsenseLinksListID, adsenseLinksGetID, "adsenseLinks", "adSenseLinks", "properties/200/adSenseLinks/301",
		[3]string{adsenseOneJSON, adsenseTwoJSON, adsenseThreeJSON}},
	{"searchads360", searchAdsLinksListID, searchAdsLinksGetID, "searchAds360Links", "searchAds360Links",
		"properties/200/searchAds360Links/401", [3]string{searchOneJSON, searchTwoJSON, searchThreeJSON}},
}

func TestLinksListPagesThroughThreePages(t *testing.T) {
	for _, k := range linkKinds {
		t.Run(k.name, func(t *testing.T) {
			pages := []string{
				`{"` + k.member + `":[` + k.pages[0] + `],"nextPageToken":"t2"}`,
				`{"` + k.member + `":[` + k.pages[1] + `],"nextPageToken":"t3"}`,
				`{"` + k.member + `":[` + k.pages[2] + `]}`,
			}
			env := linkEnv(t, &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
				return respond(r, http.StatusOK, "application/json", pages[n-1]), nil
			}})
			cursor, count := "", 0
			for i := 0; i < 3; i++ {
				args := `{"property":"properties/200","page_size":1`
				if cursor != "" {
					args += `,"cursor":"` + cursor + `"`
				}
				result, err := env.call(k.listID, "sfree", args+`}`)
				if err != nil {
					t.Fatal(err)
				}
				var out map[string]json.RawMessage
				var list []json.RawMessage
				var more bool
				_ = json.Unmarshal([]byte(result), &out)
				_ = json.Unmarshal(out[outputMember(k.member)], &list)
				_ = json.Unmarshal(out["has_more"], &more)
				if len(list) != 1 || more != (i < 2) || strings.Contains(result, "extra") || strings.Contains(result, `"t`+"2") {
					t.Fatalf("page %d = %s", i, result)
				}
				count += len(list)
				cursor = ""
				_ = json.Unmarshal(out["next_cursor"], &cursor)
			}
			requests, _ := env.fake.seen()
			if count != 3 || len(requests) != 3 || requests[0].path != "/v1alpha/properties/200/"+k.segment ||
				requests[0].rawQuery != "pageSize=1" || !strings.Contains(requests[1].rawQuery, "pageToken=t2") ||
				!strings.Contains(requests[2].rawQuery, "pageToken=t3") {
				t.Errorf("count %d, requests %+v", count, requests)
			}
		})
	}
}

// outputMember is the member of a list result: Google spells the AdSense list member with a lower case s, the
// tool result with an upper case S.
func outputMember(wire string) string {
	if wire == "adsenseLinks" {
		return "adSenseLinks"
	}
	return wire
}

func TestLinksListEmptyAnswerIsAnEmptyListAndCursorIsBound(t *testing.T) {
	for _, k := range linkKinds {
		env := linkEnv(t, &fakeGoogle{api: always(http.StatusOK, `{}`)})
		result, err := env.call(k.listID, "sfree", `{"property":"properties/200"}`)
		if err != nil || strings.Contains(result, "Links") || !strings.Contains(result, `"has_more":false`) {
			t.Errorf("%s: %s, %v", k.name, result, err)
		}
		env = linkEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"nextPageToken":"t2"}`)})
		result, err = env.call(k.listID, "sfree", `{"property":"properties/200"}`)
		var page struct {
			NextCursor string `json:"next_cursor"`
		}
		if err != nil || json.Unmarshal([]byte(result), &page) != nil || page.NextCursor == "" {
			t.Fatalf("%s: %s, %v", k.name, result, err)
		}
		if _, err := env.call(k.listID, "sfree", `{"property":"properties/201","cursor":"`+page.NextCursor+`"}`); !refusedInvalid(err) {
			t.Errorf("%s: a cursor of another property was accepted: %v", k.name, err)
		}
	}
}

func TestAdsenseLinkGetReturnsEveryDocumentedField(t *testing.T) {
	env := linkEnv(t, &fakeGoogle{api: always(http.StatusOK, adsenseOneJSON)})
	result, err := env.call(adsenseLinksGetID, "sfree", `{"name":"properties/200/adSenseLinks/301"}`)
	var got adsenseLink
	if err != nil || json.Unmarshal([]byte(result), &got) != nil || strings.Contains(result, "extra") {
		t.Fatalf("%v %s", err, result)
	}
	requests, _ := env.fake.seen()
	if got.Name != "properties/200/adSenseLinks/301" || got.AdClientCode != "ca-pub-0000000001" ||
		len(requests) != 1 || requests[0].method != http.MethodGet ||
		requests[0].path != "/v1alpha/properties/200/adSenseLinks/301" {
		t.Errorf("link = %+v, requests %+v", got, requests)
	}
}

func TestSearchAdsLinkGetReturnsEveryDocumentedField(t *testing.T) {
	env := linkEnv(t, &fakeGoogle{api: always(http.StatusOK, searchOneJSON)})
	result, err := env.call(searchAdsLinksGetID, "sfree", `{"name":"properties/200/searchAds360Links/401"}`)
	var got searchAdsLink
	if err != nil || json.Unmarshal([]byte(result), &got) != nil || strings.Contains(result, "extra") {
		t.Fatalf("%v %s", err, result)
	}
	requests, _ := env.fake.seen()
	if got.Name != "properties/200/searchAds360Links/401" || got.AdvertiserID != "5001" ||
		got.AdvertiserDisplayName != "Sample advertiser" || got.CampaignDataSharingEnabled == nil ||
		!*got.CampaignDataSharingEnabled || got.CostDataSharingEnabled == nil || *got.CostDataSharingEnabled ||
		got.AdsPersonalizationEnabled == nil || !*got.AdsPersonalizationEnabled ||
		got.SiteStatsSharingEnabled == nil || *got.SiteStatsSharingEnabled ||
		len(requests) != 1 || requests[0].path != "/v1alpha/properties/200/searchAds360Links/401" {
		t.Errorf("link = %+v, requests %+v", got, requests)
	}
}

func TestLinkAnswersAreBoundedAndChecked(t *testing.T) {
	env := linkEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"adsenseLinks":[`+
		`{"name":"properties/200/adSenseLinks/1","adClientCode":"bad code"},`+
		`{"name":"properties/999/adSenseLinks/2"},{"name":"properties/200/adSenseLinks/x"},`+
		`{"name":"properties/200/searchAds360Links/3"},{"name":"nonsense"}]}`)})
	result, err := env.call(adsenseLinksListID, "sfree", `{"property":"properties/200"}`)
	var out struct {
		Links []adsenseLink `json:"adSenseLinks"`
	}
	if err != nil || json.Unmarshal([]byte(result), &out) != nil || len(out.Links) != 1 || out.Links[0].AdClientCode != "" {
		t.Fatalf("result %s, %v", result, err)
	}
	env = linkEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"searchAds360Links":[`+
		`{"name":"properties/200/searchAds360Links/1","advertiserDisplayName":"`+strings.Repeat("n", 400)+`","advertiserId":"not numeric id"},`+
		`{"name":"properties/999/searchAds360Links/2"},{"name":"properties/200/adSenseLinks/3"}]}`)})
	result, err = env.call(searchAdsLinksListID, "sfree", `{"property":"properties/200"}`)
	var search struct {
		Links []searchAdsLink `json:"searchAds360Links"`
	}
	if err != nil || json.Unmarshal([]byte(result), &search) != nil || len(search.Links) != 1 ||
		len([]rune(string(search.Links[0].AdvertiserDisplayName))) != maxTextRunes+1 || search.Links[0].AdvertiserID != "" {
		t.Fatalf("result %s, %v", result, err)
	}
	env = linkEnv(t, &fakeGoogle{api: always(http.StatusOK, adsenseOneJSON)})
	if _, err := env.call(adsenseLinksGetID, "sfree", `{"name":"properties/200/adSenseLinks/999"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("adsense get of another link: %v", err)
	}
	env = linkEnv(t, &fakeGoogle{api: always(http.StatusOK, searchOneJSON)})
	if _, err := env.call(searchAdsLinksGetID, "sfree", `{"name":"properties/200/searchAds360Links/999"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("search ads get of another link: %v", err)
	}
}

func TestLinksRefuseBadInputBeforeAnyIO(t *testing.T) {
	env := linkEnv(t, &fakeGoogle{})
	for _, c := range []struct{ id, arguments string }{
		{adsenseLinksListID, `{"property":"accounts/100"}`},
		{adsenseLinksListID, `{"property":"properties/0"}`},
		{adsenseLinksGetID, `{"name":"properties/200/adSenseLinks/0"}`},
		{adsenseLinksGetID, `{"name":"properties/200/adSenseLinks/1/x"}`},
		{adsenseLinksGetID, `{"name":"properties/200/searchAds360Links/1"}`},
		{searchAdsLinksListID, `{"property":"properties/x"}`},
		{searchAdsLinksGetID, `{"name":"properties/200/adSenseLinks/1"}`},
		{searchAdsLinksGetID, `{"name":"properties/200/searchAds360Links/01"}`},
	} {
		if _, err := env.call(c.id, "sfree", c.arguments); !refusedInvalid(err) {
			t.Errorf("%s %s: %v", c.id, c.arguments, err)
		}
	}
	env.noIO(t)
}

func TestLinksFollowTheBindingBeforeAnyIO(t *testing.T) {
	for _, tool := range []struct{ id, arguments string }{
		{adsenseLinksListID, `{"property":"PROP"}`},
		{adsenseLinksGetID, `{"name":"PROP/adSenseLinks/301"}`},
		{searchAdsLinksListID, `{"property":"PROP"}`},
		{searchAdsLinksGetID, `{"name":"PROP/searchAds360Links/401"}`},
	} {
		for _, c := range streamBindingCases {
			answers := map[string]string{
				"/v1beta/properties/200": `{"name":"properties/200","parent":"accounts/100"}`,
				"/v1beta/properties/201": `{"name":"properties/201","parent":"accounts/100"}`,
				"/v1beta/properties/901": `{"name":"properties/901","parent":"accounts/900"}`,
			}
			env := linkEnv(t, &fakeGoogle{api: func(_ int, r *http.Request) (*http.Response, error) {
				body, ok := answers[r.URL.Path]
				if !ok {
					body = `{"name":"` + strings.TrimPrefix(r.URL.Path, "/v1alpha/") + `"}`
				}
				return respond(r, http.StatusOK, "application/json", body), nil
			}})
			args := strings.ReplaceAll(tool.arguments, "PROP", c.property)
			result, err := env.call(tool.id, c.connection, args)
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

func TestLinkErrorsAreClassified(t *testing.T) {
	calls := []struct{ id, arguments string }{
		{adsenseLinksListID, `{"property":"properties/200"}`},
		{adsenseLinksGetID, `{"name":"properties/200/adSenseLinks/301"}`},
		{searchAdsLinksListID, `{"property":"properties/200"}`},
		{searchAdsLinksGetID, `{"name":"properties/200/searchAds360Links/401"}`},
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
	} {
		for _, call := range calls {
			t.Run(c.name+" "+call.id, func(t *testing.T) {
				env := linkEnv(t, &fakeGoogle{api: c.api})
				_, err := env.call(call.id, "sfree", call.arguments)
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

func TestAdvertiserLink429WithoutRetryAfterHoldsTheNextRequest(t *testing.T) {
	env := linkEnv(t, &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
		if n == 1 {
			return respond(r, http.StatusTooManyRequests, "application/json", errorObject(429, "RESOURCE_EXHAUSTED", "")), nil
		}
		return respond(r, http.StatusOK, "application/json", adsenseOneJSON), nil
	}})
	const args = `{"name":"properties/200/adSenseLinks/301"}`
	if _, err := env.call(adsenseLinksGetID, "sfree", args); classOf(err) != provider.ClassRateLimited {
		t.Fatalf("error = %v", err)
	}
	if _, err := env.call(adsenseLinksGetID, "sfree", args); err != nil {
		t.Fatal(err)
	}
	if requests, _ := env.fake.seen(); len(requests) != 2 {
		t.Errorf("requests = %d, want 2 without a retry", len(requests))
	}
	if waits := *env.waits; len(waits) == 0 || slices.Max(waits) < 900_000_000 {
		t.Errorf("waits = %v, want the next request held for about a second", waits)
	}
}

// The Search Ads 360 and AdSense link tools only read here; the AdSense delete is covered by the delete tests.
func TestSearchAdsLinksHaveNoChangingTool(t *testing.T) {
	reg := registry(t)
	for _, descriptor := range reg.Provider(Provider) {
		if strings.Contains(descriptor.ID, "search-ads360-links.") && descriptor.Risk.Effect != "read" {
			t.Errorf("%s effect = %s", descriptor.ID, descriptor.Risk.Effect)
		}
	}
	for _, verb := range []string{"create", "update", "patch", "delete"} {
		if _, ok := scopes[Provider+".search-ads360-links."+verb]; ok {
			t.Errorf("search-ads360-links.%s exists", verb)
		}
	}
	for _, verb := range []string{"create", "update", "patch"} {
		if _, ok := scopes[Provider+".adsense-links."+verb]; ok {
			t.Errorf("adsense-links.%s exists", verb)
		}
	}
}
