package googlebusinessprofile

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// fullLocation holds every documented field of a location, the deprecated metadata flags, and a member the
// discovery document does not name. The values are synthetic.
const fullLocation = `{"name":"locations/2000002","languageCode":"en","storeCode":"store-1","title":"Sample bakery",
"phoneNumbers":{"primaryPhone":"phone-sample-1","additionalPhones":["phone-sample-2"]},
"categories":{"primaryCategory":{"name":"categories/gcid:bakery","displayName":"Bakery",
"serviceTypes":[{"serviceTypeId":"job_type_id:sample","displayName":"Sample service"}],
"moreHoursTypes":[{"hoursTypeId":"DELIVERY","displayName":"Delivery","localizedDisplayName":"Delivery"}]},
"additionalCategories":[{"name":"categories/gcid:cafe","displayName":"Cafe"}]},
"storefrontAddress":{"regionCode":"CH","languageCode":"de","postalCode":"0000","administrativeArea":"Sample area",
"locality":"Sample town","sublocality":"Sample district","addressLines":["Sample street 1"],"recipients":["Front desk"],
"organization":"Sample bakery","sortingCode":"S1","revision":0},
"websiteUri":"https://www.example.com/",
"regularHours":{"periods":[{"openDay":"MONDAY","openTime":{"hours":8,"minutes":0},"closeDay":"MONDAY","closeTime":{"hours":17,"minutes":30,"seconds":0,"nanos":0}}]},
"specialHours":{"specialHourPeriods":[{"startDate":{"year":2026,"month":12,"day":24},"endDate":{"year":2026,"month":12,"day":24},"openTime":{"hours":9},"closeTime":{"hours":12},"closed":false}]},
"serviceArea":{"businessType":"CUSTOMER_AND_BUSINESS_LOCATION","places":{"placeInfos":[{"placeName":"Sample town","placeId":"place-sample-1"}]},"regionCode":"CH"},
"labels":["sample-label"],
"adWordsLocationExtensions":{"adPhone":"phone-sample-3"},
"latlng":{"latitude":47.5,"longitude":8.25},
"openInfo":{"status":"OPEN","canReopen":true,"openingDate":{"year":2020,"month":1}},
"metadata":{"hasGoogleUpdated":false,"hasPendingEdits":true,"canDelete":true,"canOperateLocalPost":true,
"canModifyServiceList":true,"canHaveFoodMenus":false,"canOperateHealthData":false,"canOperateLodgingData":false,
"placeId":"place-sample-2","duplicateLocation":"locations/2000009","mapsUri":"https://maps.example.com/sample",
"newReviewUri":"https://review.example.com/sample","canHaveBusinessCalls":true,"hasVoiceOfMerchant":true,
"isParticularlyPersonalPlace":false},
"profile":{"description":"A sample bakery."},
"relationshipData":{"parentLocation":{"placeId":"place-sample-3","relationType":"DEPARTMENT_OF"},
"childrenLocations":[{"placeId":"place-sample-4","relationType":"INDEPENDENT_ESTABLISHMENT_IN"}],"parentChain":"chains/sample"},
"moreHours":[{"hoursTypeId":"DELIVERY","periods":[{"openDay":"FRIDAY","openTime":{"hours":10},"closeDay":"FRIDAY","closeTime":{"hours":14}}]}],
"serviceItems":[{"structuredServiceItem":{"serviceTypeId":"job_type_id:sample","description":"Sample"},"price":{"currencyCode":"CHF","units":"12","nanos":500000000}},
{"freeFormServiceItem":{"category":"categories/gcid:bakery","label":{"displayName":"Cake","description":"Sample cake","languageCode":"en"}}}],
"undocumented":"provider-detail-canary-7c1d"}`

var allFields = `["name","languageCode","storeCode","title","phoneNumbers","categories","storefrontAddress",` +
	`"websiteUri","regularHours","specialHours","serviceArea","labels","adWordsLocationExtensions","latlng",` +
	`"openInfo","metadata","profile","relationshipData","moreHours","serviceItems"]`

func TestLocationsGetReturnsEveryDocumentedField(t *testing.T) {
	env := newEnvironment(t, nil)
	env.fake.answer = reply(http.StatusOK, fullLocation)
	out, err := env.invoke(t, locationsGetID, "all", `{"location":"`+boundLocation+`","read_mask":`+allFields+`}`)
	if err != nil {
		t.Fatal(err)
	}
	var want map[string]any
	if err := json.Unmarshal([]byte(fullLocation), &want); err != nil {
		t.Fatal(err)
	}
	delete(want, "undocumented")
	metadata := want["metadata"].(map[string]any)
	delete(metadata, "canOperateLocalPost")
	delete(metadata, "canHaveBusinessCalls")
	if !reflect.DeepEqual(out, want) {
		got, _ := json.Marshal(out)
		t.Errorf("location =\n%s\nwant every documented field and nothing else", got)
	}
	calls, _ := env.fake.calls()
	values := calls[0].values()
	mask := strings.Split(values.Get("readMask"), ",")
	if calls[0].host != "mybusinessbusinessinformation.googleapis.com" || calls[0].path != "/v1/"+boundLocation ||
		len(mask) != len(readMaskFields) || !strings.HasPrefix(values.Get("readMask"), "adWordsLocationExtensions,") {
		t.Errorf("request = %+v", calls[0])
	}
}

func TestLocationsCutLongTextsAndLists(t *testing.T) {
	env := newEnvironment(t, nil)
	labels := make([]string, maxItems+5)
	for i := range labels {
		labels[i] = `"l"`
	}
	env.fake.answer = reply(http.StatusOK, `{"profile":{"description":"`+strings.Repeat("d", maxLongRunes+1)+`"},`+
		`"labels":[`+strings.Join(labels, ",")+`],"websiteUri":7,"latlng":{"latitude":"north"}}`)
	out, err := env.invoke(t, locationsGetID, "all", `{"location":"`+boundLocation+`","read_mask":["profile","labels","websiteUri","latlng"]}`)
	if err != nil {
		t.Fatal(err)
	}
	description := out["profile"].(map[string]any)["description"].(string)
	if len([]rune(description)) != maxLongRunes+1 || !strings.HasSuffix(description, "…") {
		t.Errorf("description has %d characters", len([]rune(description)))
	}
	if len(out["labels"].([]any)) != maxItems || out["websiteUri"] != nil {
		t.Errorf("result = %v", out)
	}
	if out["latlng"] != nil {
		t.Errorf("latlng = %v, want the value that is no number dropped", out["latlng"])
	}
}

func TestLocationsListPassesEveryArgumentAndReadsOn(t *testing.T) {
	env := newEnvironment(t, nil)
	env.fake.answer = func(r *http.Request, _ int) (*http.Response, error) {
		if r.URL.Query().Get("pageToken") == "" {
			return respond(r, http.StatusOK, "application/json",
				`{"locations":[{"name":"locations/2000002","title":"Sample"}],"nextPageToken":"next-page","totalSize":2}`), nil
		}
		return respond(r, http.StatusOK, "application/json", `{"locations":[{"name":"locations/2000003"}],"totalSize":2}`), nil
	}
	args := `{"account":"` + boundAccount + `","read_mask":["title","name"],"filter":"storefrontAddress.regionCode=\"CH\"",` +
		`"order_by":["title","store_code desc"],"page_size":100`
	out, err := env.invoke(t, locationsListID, "all", args+`}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(out["locations"].([]any)) != 1 || out["totalSize"] != 2.0 || out["next_cursor"] == nil {
		t.Fatalf("result = %v", out)
	}
	out, err = env.invoke(t, locationsListID, "all", args+`,"cursor":"`+out["next_cursor"].(string)+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	if out["next_cursor"] != nil {
		t.Errorf("last page next_cursor = %v", out["next_cursor"])
	}
	calls, _ := env.fake.calls()
	for i, token := range []string{"", "next-page"} {
		values := calls[i].values()
		if calls[i].host != "mybusinessbusinessinformation.googleapis.com" ||
			calls[i].path != "/v1/"+boundAccount+"/locations" || values.Get("readMask") != "name,title" ||
			values.Get("filter") != `storefrontAddress.regionCode="CH"` || values.Get("orderBy") != "title,store_code desc" ||
			values.Get("pageSize") != "100" || values.Get("pageToken") != token {
			t.Errorf("request %d = %+v", i+1, calls[i])
		}
	}
	// A cursor is bound to the read mask as well.
	first, _ := env.invoke(t, locationsListID, "all", `{"account":"`+boundAccount+`","read_mask":["name"]}`)
	if _, err := env.invoke(t, locationsListID, "all", `{"account":"`+boundAccount+`","read_mask":["title"],`+
		`"cursor":"`+first["next_cursor"].(string)+`"}`); !isInvalidRequest(err) {
		t.Errorf("cursor of another read mask: error = %v", err)
	}
}

func TestLocationsListAnswersAnEmptyList(t *testing.T) {
	env := newEnvironment(t, nil)
	out, err := env.invoke(t, locationsListID, "all", `{"account":"accounts/-","read_mask":["name"]}`)
	if err != nil {
		t.Fatal(err)
	}
	// An empty list is left out of the result: missing means empty.
	if len(out) != 0 {
		t.Errorf("result = %v, want an empty list", out)
	}
	if calls, _ := env.fake.calls(); calls[0].path != "/v1/accounts/-/locations" {
		t.Errorf("request = %+v", calls[0])
	}
}

func TestReadMaskIsRequiredAndKnown(t *testing.T) {
	env := newEnvironment(t, nil)
	for tool, base := range map[string]string{
		locationsListID: `"account":"` + boundAccount + `"`,
		locationsGetID:  `"location":"` + boundLocation + `"`,
	} {
		for _, mask := range []string{``, `,"read_mask":[]`, `,"read_mask":["*"]`, `,"read_mask":["title","title"]`,
			`,"read_mask":["metadata.placeId"]`, `,"read_mask":"name"`} {
			if _, err := env.invoke(t, tool, "all", `{`+base+mask+`}`); !isInvalidRequest(err) {
				t.Errorf("%s %s: error = %v, want invalid-request", tool, mask, err)
			}
		}
	}
	if _, err := readMaskOf([]string{"name", "unknown"}); err == nil {
		t.Error("readMaskOf accepted an unknown field")
	}
	if calls, grants := env.fake.calls(); len(calls) != 0 || grants != 0 {
		t.Errorf("%d requests and %d grants, want none", len(calls), grants)
	}
}

func TestAccountTargetBindsTheLocationsList(t *testing.T) {
	env := newEnvironment(t, nil)
	if _, err := env.invoke(t, locationsListID, "account", `{"read_mask":["name"]}`); err != nil {
		t.Fatal(err)
	}
	if calls, _ := env.fake.calls(); calls[0].path != "/v1/"+boundAccount+"/locations" {
		t.Errorf("request = %+v", calls[0])
	}
	for _, args := range []string{`{"account":"` + otherAccount + `","read_mask":["name"]}`,
		`{"account":"accounts/-","read_mask":["name"]}`} {
		if _, err := env.invoke(t, locationsListID, "account", args); !isInvalidRequest(err) {
			t.Errorf("%s: error = %v, want invalid-request", args, err)
		}
	}
	if calls, _ := env.fake.calls(); len(calls) != 1 {
		t.Errorf("a refused call reached Google: %d requests", len(calls))
	}
}

func TestLocationTargetListsOnlyItsLocation(t *testing.T) {
	env := newEnvironment(t, nil)
	env.fake.answer = reply(http.StatusOK, `{"name":"`+boundLocation+`","title":"Sample"}`)
	out, err := env.invoke(t, locationsListID, "location", `{"read_mask":["title","name"],"order_by":["title"]}`)
	if err != nil {
		t.Fatal(err)
	}
	if locations := out["locations"].([]any); len(locations) != 1 || out["next_cursor"] != nil {
		t.Errorf("result = %v", out)
	}
	if _, err := env.invoke(t, locationsGetID, "location", `{"read_mask":["name"]}`); err != nil {
		t.Errorf("get without location: %v", err)
	}
	calls, _ := env.fake.calls()
	for _, call := range calls {
		if call.path != "/v1/"+boundLocation || strings.Contains(call.path, "accounts") {
			t.Errorf("request = %+v, want the bound location only", call)
		}
	}
	for tool, args := range map[string]string{
		locationsListID: `{"account":"` + boundAccount + `","read_mask":["name"]}`,
		locationsGetID:  `{"location":"` + otherLocation + `","read_mask":["name"]}`,
	} {
		if _, err := env.invoke(t, tool, "location", args); !isInvalidRequest(err) {
			t.Errorf("%s %s: error = %v, want invalid-request", tool, args, err)
		}
	}
	if _, err := env.invoke(t, locationsListID, "location", `{"filter":"title=\"Sample\"","read_mask":["name"]}`); !isInvalidRequest(err) {
		t.Errorf("filter on a location target: error = %v", err)
	}
	if after, _ := env.fake.calls(); len(after) != len(calls) {
		t.Errorf("a refused call reached Google")
	}
}

// On an account target, locations.get first proves from the location list of the account, names only, that
// the location belongs to it; a location the list does not hold is refused without reading it.
func TestAccountTargetProvesTheLocationBeforeReadingIt(t *testing.T) {
	env := newEnvironment(t, nil)
	env.fake.answer = func(r *http.Request, n int) (*http.Response, error) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/locations") && r.URL.Query().Get("pageToken") == "":
			return respond(r, http.StatusOK, "application/json",
				`{"locations":[{"name":"locations/2000005"}],"nextPageToken":"proof-two"}`), nil
		case strings.HasSuffix(r.URL.Path, "/locations"):
			return respond(r, http.StatusOK, "application/json", `{"locations":[{"name":"`+boundLocation+`"}]}`), nil
		}
		return respond(r, http.StatusOK, "application/json", `{"name":"`+boundLocation+`","title":"Sample"}`), nil
	}
	out, err := env.invoke(t, locationsGetID, "account", `{"location":"`+boundLocation+`","read_mask":["title"]}`)
	if err != nil || out["title"] != "Sample" {
		t.Fatalf("result = %v, %v", out, err)
	}
	calls, _ := env.fake.calls()
	if len(calls) != 3 {
		t.Fatalf("requests = %+v", calls)
	}
	for i, token := range []string{"", "proof-two"} {
		values := calls[i].values()
		if calls[i].path != "/v1/"+boundAccount+"/locations" || values.Get("readMask") != "name" ||
			values.Get("pageSize") != "100" || values.Get("pageToken") != token {
			t.Errorf("proof request %d = %+v", i+1, calls[i])
		}
	}
	if calls[2].path != "/v1/"+boundLocation || calls[2].values().Get("readMask") != "title" {
		t.Errorf("read = %+v", calls[2])
	}

	_, err = env.invoke(t, locationsGetID, "account", `{"location":"`+otherLocation+`","read_mask":["title"]}`)
	if !isInvalidRequest(err) || strings.Contains(err.Error(), otherLocation) {
		t.Errorf("location outside the account: error = %v, want invalid-request without the location", err)
	}
	after, _ := env.fake.calls()
	if len(after) != 5 || after[4].path != "/v1/"+boundAccount+"/locations" {
		t.Errorf("a location outside the account was read: %+v", after[3:])
	}
}

func TestLocationArgumentsAreCheckedBeforeAnyRequest(t *testing.T) {
	env := newEnvironment(t, nil)
	for _, args := range []string{`{"read_mask":["name"]}`, `{"location":"locations/-","read_mask":["name"]}`,
		`{"location":"locations/02","read_mask":["name"]}`, `{"location":"` + boundAccount + `","read_mask":["name"]}`,
		`{"location":"accounts/1/locations/2","read_mask":["name"]}`} {
		if _, err := env.invoke(t, locationsGetID, "all", args); !isInvalidRequest(err) {
			t.Errorf("%s: error = %v, want invalid-request", args, err)
		}
	}
	for _, args := range []string{`{"read_mask":["name"]}`, `{"account":"accounts/x","read_mask":["name"]}`,
		`{"account":"` + boundAccount + `","read_mask":["name"],"filter":"a\u0001b"}`,
		`{"account":"` + boundAccount + `","read_mask":["name"],"order_by":["name"]}`,
		`{"account":"` + boundAccount + `","read_mask":["name"],"page_size":101}`} {
		if _, err := env.invoke(t, locationsListID, "all", args); !isInvalidRequest(err) {
			t.Errorf("%s: error = %v, want invalid-request", args, err)
		}
	}
	if calls, grants := env.fake.calls(); len(calls) != 0 || grants != 0 {
		t.Errorf("%d requests and %d grants, want none", len(calls), grants)
	}
}
