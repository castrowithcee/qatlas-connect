package googlebusinessprofile

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const informationHost = "mybusinessbusinessinformation.googleapis.com"

var referenceTools = []string{categoriesListID, categoriesBatchGetID, chainsGetID, chainsSearchID,
	googleLocationsSearchID}

// bodyOf reads the body of a request the fake transport has already consumed once.
func bodyOf(t *testing.T, r *http.Request) string {
	t.Helper()
	if r.GetBody == nil {
		return ""
	}
	body, err := r.GetBody()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(body)
	return string(raw)
}

// categoriesPage answers a category list by its pageToken: two full pages of 100 and a last one of 7.
func categoriesPage(r *http.Request, _ int) (*http.Response, error) {
	start, next, size := 0, "token-two", 100
	switch r.URL.Query().Get("pageToken") {
	case "token-two":
		start, next = 100, "token-three"
	case "token-three":
		start, next, size = 200, "", 7
	}
	entries := make([]string, 0, size)
	for i := range size {
		entries = append(entries, fmt.Sprintf(`{"name":"categories/gcid:sample_%d","displayName":"Sample %d"}`,
			start+i, start+i))
	}
	body := `{"categories":[` + strings.Join(entries, ",") + `]`
	if next != "" {
		body += `,"nextPageToken":"` + next + `"`
	}
	return respond(r, http.StatusOK, "application/json", body+"}"), nil
}

func TestCategoriesListReadsThreePagesWithTheCursor(t *testing.T) {
	env := newEnvironment(t, nil)
	env.fake.answer = categoriesPage
	const arguments = `{"region_code":"DE","language_code":"de","view":"BASIC","filter":"sample","page_size":100`
	var names []string
	cursor := ""
	for page := 1; page <= 3; page++ {
		args := arguments + `}`
		if cursor != "" {
			args = arguments + `,"cursor":"` + cursor + `"}`
		}
		out, err := env.invoke(t, categoriesListID, "all", args)
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		for _, entry := range out["categories"].([]any) {
			names = append(names, entry.(map[string]any)["name"].(string))
		}
		next, _ := out["next_cursor"].(string)
		if (page < 3) != (next != "") || strings.Contains(next, "token-") {
			t.Fatalf("page %d next_cursor = %q", page, next)
		}
		cursor = next
	}
	if len(names) != 207 || names[0] != "categories/gcid:sample_0" || names[206] != "categories/gcid:sample_206" {
		t.Errorf("categories = %d, first %v", len(names), names[:1])
	}
	calls, _ := env.fake.calls()
	for i, want := range []string{"", "token-two", "token-three"} {
		values := calls[i].values()
		if calls[i].host != informationHost || calls[i].path != "/v1/categories" ||
			values.Get("regionCode") != "DE" || values.Get("languageCode") != "de" || values.Get("view") != "BASIC" ||
			values.Get("filter") != "displayName=sample" || values.Get("pageSize") != "100" ||
			values.Get("pageToken") != want {
			t.Errorf("request %d = %+v", i+1, calls[i])
		}
	}
}

func TestCategoriesListRefusesACursorOfOtherArguments(t *testing.T) {
	env := newEnvironment(t, nil)
	env.fake.answer = categoriesPage
	const base = `"region_code":"DE","language_code":"de","view":"BASIC"`
	out, err := env.invoke(t, categoriesListID, "all", `{`+base+`}`)
	if err != nil {
		t.Fatal(err)
	}
	cursor := out["next_cursor"].(string)
	for _, args := range []string{
		`{"region_code":"AT","language_code":"de","view":"BASIC","cursor":"` + cursor + `"}`,
		`{"region_code":"DE","language_code":"en","view":"BASIC","cursor":"` + cursor + `"}`,
		`{"region_code":"DE","language_code":"de","view":"FULL","cursor":"` + cursor + `"}`,
		`{` + base + `,"filter":"x","cursor":"` + cursor + `"}`,
		`{` + base + `,"cursor":"x` + cursor + `"}`,
		`{` + base + `,"cursor":"token-two"}`,
	} {
		if _, err := env.invoke(t, categoriesListID, "all", args); !isInvalidRequest(err) {
			t.Errorf("%s: error = %v, want invalid-request", args, err)
		}
	}
	// A cursor of an unbound connection does not continue on a connection bound elsewhere.
	if _, err := env.invoke(t, categoriesListID, "account", `{`+base+`,"cursor":"`+cursor+`"}`); !isInvalidRequest(err) {
		t.Errorf("cursor across connections: error = %v, want invalid-request", err)
	}
	if calls, _ := env.fake.calls(); len(calls) != 1 {
		t.Errorf("%d requests, want one", len(calls))
	}
}

func TestCategoriesReturnEveryDocumentedField(t *testing.T) {
	env := newEnvironment(t, nil)
	long := strings.Repeat("n", maxLineRunes+10)
	env.fake.answer = reply(http.StatusOK, `{"categories":[{"name":"categories/gcid:sample_one","displayName":"`+long+`",`+
		`"serviceTypes":[{"serviceTypeId":"job_type_id:sample","displayName":"Sample service"}],`+
		`"moreHoursTypes":[{"hoursTypeId":"SAMPLE","displayName":"Sample","localizedDisplayName":"Beispiel"}],`+
		`"undocumented":"`+detailCanary+`"}]}`)
	out, err := env.invoke(t, categoriesBatchGetID, "all",
		`{"names":["categories/gcid:sample_one"],"language_code":"de","view":"FULL"}`)
	if err != nil {
		t.Fatal(err)
	}
	entry := out["categories"].([]any)[0].(map[string]any)
	if name := entry["displayName"].(string); !strings.HasSuffix(name, "…") || len([]rune(name)) != maxLineRunes+1 {
		t.Errorf("displayName was not cut: %d characters", len([]rune(name)))
	}
	service := entry["serviceTypes"].([]any)[0].(map[string]any)
	hours := entry["moreHoursTypes"].([]any)[0].(map[string]any)
	if entry["name"] != "categories/gcid:sample_one" || service["serviceTypeId"] != "job_type_id:sample" ||
		service["displayName"] != "Sample service" || hours["hoursTypeId"] != "SAMPLE" ||
		hours["displayName"] != "Sample" || hours["localizedDisplayName"] != "Beispiel" || entry["undocumented"] != nil {
		t.Errorf("category = %v", entry)
	}
}

func TestCategoriesBatchGetSendsEveryNameAndRefusesWhatIsNoName(t *testing.T) {
	env := newEnvironment(t, nil)
	env.fake.answer = reply(http.StatusOK, `{"categories":[{"name":"categories/gcid:sample_two"},`+
		`{"name":"categories/gcid:sample_one"}]}`)
	out, err := env.invoke(t, categoriesBatchGetID, "all", `{"names":["categories/gcid:sample_one",`+
		`"categories/gcid:sample_two"],"language_code":"en-GB","region_code":"GB","view":"BASIC"}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(out["categories"].([]any)) != 2 {
		t.Errorf("categories = %v", out)
	}
	calls, _ := env.fake.calls()
	values := calls[0].values()
	if len(calls) != 1 || calls[0].path != "/v1/categories:batchGet" ||
		!reflect.DeepEqual(values["names"], []string{"categories/gcid:sample_one", "categories/gcid:sample_two"}) ||
		values.Get("languageCode") != "en-GB" || values.Get("regionCode") != "GB" || values.Get("view") != "BASIC" {
		t.Errorf("request = %+v", calls)
	}
	many := make([]string, 51)
	for i := range many {
		many[i] = fmt.Sprintf(`"categories/gcid:sample_%d"`, i)
	}
	for name, args := range map[string]string{
		"empty list":     `{"names":[],"language_code":"de","view":"BASIC"}`,
		"no names":       `{"language_code":"de","view":"BASIC"}`,
		"twice":          `{"names":["categories/gcid:a","categories/gcid:a"],"language_code":"de","view":"BASIC"}`,
		"a path":         `{"names":["categories/../accounts/1"],"language_code":"de","view":"BASIC"}`,
		"a query":        `{"names":["categories/gcid:a&view=FULL"],"language_code":"de","view":"BASIC"}`,
		"upper case":     `{"names":["categories/gcid:Sample"],"language_code":"de","view":"BASIC"}`,
		"too many":       `{"names":[` + strings.Join(many, ",") + `],"language_code":"de","view":"BASIC"}`,
		"no language":    `{"names":["categories/gcid:a"],"view":"BASIC"}`,
		"bad language":   `{"names":["categories/gcid:a"],"language_code":"de&x=1","view":"BASIC"}`,
		"no view":        `{"names":["categories/gcid:a"],"language_code":"de"}`,
		"unspecified":    `{"names":["categories/gcid:a"],"language_code":"de","view":"CATEGORY_VIEW_UNSPECIFIED"}`,
		"bad region":     `{"names":["categories/gcid:a"],"language_code":"de","view":"BASIC","region_code":"de"}`,
		"unknown member": `{"names":["categories/gcid:a"],"language_code":"de","view":"BASIC","parent":"x"}`,
	} {
		if _, err := env.invoke(t, categoriesBatchGetID, "all", args); !isInvalidRequest(err) {
			t.Errorf("%s: error = %v, want invalid-request", name, err)
		}
	}
	if calls, _ := env.fake.calls(); len(calls) != 1 {
		t.Errorf("%d requests, want only the first", len(calls))
	}
}

func TestCategoriesListRefusesBadArgumentsBeforeAnyRequest(t *testing.T) {
	env := newEnvironment(t, nil)
	for name, args := range map[string]string{
		"no region":      `{"language_code":"de","view":"BASIC"}`,
		"lower region":   `{"region_code":"de","language_code":"de","view":"BASIC"}`,
		"no language":    `{"region_code":"DE","view":"BASIC"}`,
		"no view":        `{"region_code":"DE","language_code":"de"}`,
		"big page":       `{"region_code":"DE","language_code":"de","view":"BASIC","page_size":101}`,
		"zero page":      `{"region_code":"DE","language_code":"de","view":"BASIC","page_size":0}`,
		"control filter": `{"region_code":"DE","language_code":"de","view":"BASIC","filter":"a\nb"}`,
		"long filter":    `{"region_code":"DE","language_code":"de","view":"BASIC","filter":"` + strings.Repeat("a", 101) + `"}`,
	} {
		if _, err := env.invoke(t, categoriesListID, "all", args); !isInvalidRequest(err) {
			t.Errorf("%s: error = %v, want invalid-request", name, err)
		}
	}
	if calls, grants := env.fake.calls(); len(calls) != 0 || grants != 0 {
		t.Errorf("%d requests and %d grants, want none", len(calls), grants)
	}
}

func TestCategoriesListAnswersAnEmptyList(t *testing.T) {
	env := newEnvironment(t, nil)
	env.fake.answer = reply(http.StatusOK, `{}`)
	out, err := env.invoke(t, categoriesListID, "all", `{"region_code":"DE","language_code":"de","view":"BASIC"}`)
	if err != nil || out["categories"] != nil || out["next_cursor"] != nil {
		t.Errorf("result = %v, error = %v, want an empty list", out, err)
	}
}

func TestCategoriesListRefusesTooManyEntriesAndALongToken(t *testing.T) {
	env := newEnvironment(t, nil)
	entries := strings.TrimSuffix(strings.Repeat(`{"name":"categories/gcid:a"},`, maxCategoriesPage+1), ",")
	env.fake.answer = reply(http.StatusOK, `{"categories":[`+entries+`]}`)
	const args = `{"region_code":"DE","language_code":"de","view":"BASIC"}`
	if _, err := env.invoke(t, categoriesListID, "all", args); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("too many entries: error = %v", err)
	}
	env.fake.answer = reply(http.StatusOK, `{"categories":[],"nextPageToken":"`+strings.Repeat("x", maxPageToken+1)+`"}`)
	if _, err := env.invoke(t, categoriesListID, "all", args); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("long token: error = %v", err)
	}
}

func TestChainsGetChecksThePathBeforeAnyRequest(t *testing.T) {
	env := newEnvironment(t, nil)
	env.fake.answer = reply(http.StatusOK, `{"name":"chains/sample-chain","chainNames":[{"displayName":"Sample Chain",`+
		`"languageCode":"en"}],"websites":[{"uri":"https://example.com/"}],"locationCount":0,`+
		`"undocumented":"`+detailCanary+`"}`)
	out, err := env.invoke(t, chainsGetID, "all", `{"chain":"chains/sample-chain_1"}`)
	if err != nil {
		t.Fatal(err)
	}
	names := out["chainNames"].([]any)[0].(map[string]any)
	sites := out["websites"].([]any)[0].(map[string]any)
	if out["name"] != "chains/sample-chain" || names["displayName"] != "Sample Chain" || names["languageCode"] != "en" ||
		sites["uri"] != "https://example.com/" || out["locationCount"] != 0.0 || out["undocumented"] != nil {
		t.Errorf("chain = %v", out)
	}
	calls, _ := env.fake.calls()
	if len(calls) != 1 || calls[0].host != informationHost || calls[0].path != "/v1/chains/sample-chain_1" ||
		calls[0].query != "" {
		t.Errorf("request = %+v", calls)
	}
	for _, chain := range []string{"", "chains/", "chains", "sample", "accounts/1000001", "chains/a/b", "chains/..",
		"chains/../accounts/1", "chains/a?readMask=name", "chains/a#x", "chains/a%2Fb", "chains/a b", "chains/a:b",
		"chains/" + strings.Repeat("a", 65), "/chains/a", "chains/a/", "chains/ä"} {
		args, _ := json.Marshal(map[string]string{"chain": chain})
		if _, err := env.invoke(t, chainsGetID, "all", string(args)); !isInvalidRequest(err) {
			t.Errorf("chain %q: error = %v, want invalid-request", chain, err)
		}
	}
	if _, err := env.invoke(t, chainsGetID, "all", `{}`); !isInvalidRequest(err) {
		t.Errorf("no chain: error = %v", err)
	}
	if calls, _ := env.fake.calls(); len(calls) != 1 {
		t.Errorf("%d requests, want only the first", len(calls))
	}
}

func TestChainsGetAnswersNotFound(t *testing.T) {
	env := newEnvironment(t, nil)
	env.fake.answer = reply(http.StatusNotFound, errorObject(404, "NOT_FOUND", ""))
	_, err := env.invoke(t, chainsGetID, "all", `{"chain":"chains/sample"}`)
	if classOf(err) != provider.ClassNotFound || strings.Contains(err.Error(), detailCanary) {
		t.Errorf("error = %v", err)
	}
}

func TestChainsSearchWithoutAMatchIsAnEmptyList(t *testing.T) {
	env := newEnvironment(t, nil)
	env.fake.answer = reply(http.StatusOK, `{}`)
	out, err := env.invoke(t, chainsSearchID, "all", `{"chain_name":"sample brand & more","page_size":500}`)
	if err != nil || out["chains"] != nil {
		t.Errorf("result = %v, error = %v, want an empty list", out, err)
	}
	calls, _ := env.fake.calls()
	values := calls[0].values()
	if calls[0].path != "/v1/chains:search" || values.Get("chainName") != "sample brand & more" ||
		values.Get("pageSize") != "500" || len(values) != 2 {
		t.Errorf("request = %+v", calls[0])
	}
	env.fake.answer = reply(http.StatusOK, `{"chains":[{"name":"chains/one","locationCount":3},{"name":"chains/two"}]}`)
	out, err = env.invoke(t, chainsSearchID, "all", `{"chain_name":"sample"}`)
	if err != nil || len(out["chains"].([]any)) != 2 {
		t.Errorf("result = %v, error = %v", out, err)
	}
	for _, args := range []string{`{}`, `{"chain_name":""}`, `{"chain_name":"a\u0000b"}`, `{"chain_name":"a","page_size":501}`,
		`{"chain_name":"` + strings.Repeat("a", 501) + `"}`} {
		if _, err := env.invoke(t, chainsSearchID, "all", args); !isInvalidRequest(err) {
			t.Errorf("%.40s: error = %v, want invalid-request", args, err)
		}
	}
	if calls, _ := env.fake.calls(); len(calls) != 2 {
		t.Errorf("%d requests, want two", len(calls))
	}
}

func TestGoogleLocationsSearchByTextSendsOnePostWithAnswerFields(t *testing.T) {
	env := newEnvironment(t, nil)
	var method, contentType, body string
	env.fake.answer = func(r *http.Request, _ int) (*http.Response, error) {
		method, contentType, body = r.Method, r.Header.Get("Content-Type"), bodyOf(t, r)
		return respond(r, http.StatusOK, "application/json", `{"googleLocations":[`+
			`{"name":"googleLocations/sample-claimed","requestAdminRightsUri":"https://example.com/request",`+
			`"location":{"title":"Sample Bakery","storefrontAddress":{"regionCode":"DE","locality":"Sample Town",`+
			`"addressLines":["Sample street 1"]},"phoneNumbers":{"primaryPhone":"phone-sample-1"},`+
			`"websiteUri":"https://example.com/","categories":{"primaryCategory":{"name":"categories/gcid:sample",`+
			`"displayName":"Sample"}},"metadata":{"placeId":"sample-place","mapsUri":"https://example.com/map"}}},`+
			`{"name":"googleLocations/sample-free","location":{"title":"Sample Shop"},"undocumented":"`+detailCanary+`"}]}`), nil
	}
	out, err := env.invoke(t, googleLocationsSearchID, "all", `{"query":"sample bakery","page_size":2}`)
	if err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPost || contentType != "application/json" || body != `{"query":"sample bakery","pageSize":2}` {
		t.Errorf("method %s, content type %s, body %s", method, contentType, body)
	}
	calls, _ := env.fake.calls()
	if len(calls) != 1 || calls[0].host != informationHost || calls[0].path != "/v1/googleLocations:search" ||
		calls[0].query != "" || strings.Contains(calls[0].full, "sample") {
		t.Errorf("request = %+v", calls)
	}
	matches := out["googleLocations"].([]any)
	claimed, free := matches[0].(map[string]any), matches[1].(map[string]any)
	one := claimed["location"].(map[string]any)
	if len(matches) != 2 || claimed["name"] != "googleLocations/sample-claimed" ||
		claimed["requestAdminRightsUri"] != "https://example.com/request" || one["title"] != "Sample Bakery" ||
		one["phoneNumbers"].(map[string]any)["primaryPhone"] != "phone-sample-1" ||
		one["metadata"].(map[string]any)["placeId"] != "sample-place" ||
		free["requestAdminRightsUri"] != nil || free["undocumented"] != nil {
		t.Errorf("googleLocations = %v", matches)
	}
}

func TestGoogleLocationsSearchByDetailsSendsTheLocation(t *testing.T) {
	env := newEnvironment(t, nil)
	var body string
	env.fake.answer = func(r *http.Request, _ int) (*http.Response, error) {
		body = bodyOf(t, r)
		return respond(r, http.StatusOK, "application/json", `{}`), nil
	}
	out, err := env.invoke(t, googleLocationsSearchID, "all", `{"location":{"title":"Sample Bakery","languageCode":"de",`+
		`"storefrontAddress":{"regionCode":"DE","languageCode":"de","postalCode":"00000","administrativeArea":"Sample area",`+
		`"locality":"Sample Town","sublocality":"Sample quarter","addressLines":["Sample street 1"]},`+
		`"websiteUri":"https://example.com/","phoneNumbers":{"primaryPhone":"phone-sample-1"},`+
		`"categories":{"primaryCategory":{"name":"categories/gcid:sample"}},"latlng":{"latitude":0,"longitude":0}}}`)
	if err != nil || out["googleLocations"] != nil {
		t.Errorf("result = %v, error = %v, want an empty list", out, err)
	}
	var sent map[string]map[string]any
	if err := json.Unmarshal([]byte(body), &sent); err != nil {
		t.Fatalf("body %s: %v", body, err)
	}
	l := sent["location"]
	address := l["storefrontAddress"].(map[string]any)
	if l["title"] != "Sample Bakery" || l["languageCode"] != "de" || l["websiteUri"] != "https://example.com/" ||
		address["regionCode"] != "DE" || address["locality"] != "Sample Town" ||
		address["addressLines"].([]any)[0] != "Sample street 1" ||
		l["phoneNumbers"].(map[string]any)["primaryPhone"] != "phone-sample-1" ||
		l["categories"].(map[string]any)["primaryCategory"].(map[string]any)["name"] != "categories/gcid:sample" ||
		l["latlng"].(map[string]any)["latitude"] != 0.0 || len(sent) != 1 {
		t.Errorf("body = %s", body)
	}
}

func TestGoogleLocationsSearchRefusesBadArgumentsBeforeAnyRequest(t *testing.T) {
	env := newEnvironment(t, nil)
	for name, args := range map[string]string{
		"neither":          `{}`,
		"both":             `{"query":"a","location":{"title":"a"}}`,
		"no title":         `{"location":{"languageCode":"de"}}`,
		"empty title":      `{"location":{"title":""}}`,
		"empty query":      `{"query":""}`,
		"control query":    `{"query":"a\u0007b"}`,
		"long query":       `{"query":"` + strings.Repeat("a", 501) + `"}`,
		"big page":         `{"query":"a","page_size":11}`,
		"unknown member":   `{"location":{"title":"a","regularHours":{}}}`,
		"bad region":       `{"location":{"title":"a","storefrontAddress":{"regionCode":"Germany"}}}`,
		"many lines":       `{"location":{"title":"a","storefrontAddress":{"addressLines":["a","b","c","d","e","f"]}}}`,
		"bad category":     `{"location":{"title":"a","categories":{"primaryCategory":{"name":"restaurant"}}}}`,
		"latitude":         `{"location":{"title":"a","latlng":{"latitude":91,"longitude":0}}}`,
		"longitude only":   `{"location":{"title":"a","latlng":{"longitude":0}}}`,
		"control in lines": `{"location":{"title":"a","storefrontAddress":{"addressLines":["a\nb"]}}}`,
		"bad language":     `{"location":{"title":"a","languageCode":"de_DE"}}`,
	} {
		if _, err := env.invoke(t, googleLocationsSearchID, "all", args); !isInvalidRequest(err) {
			t.Errorf("%s: error = %v, want invalid-request", name, err)
		}
	}
	if calls, grants := env.fake.calls(); len(calls) != 0 || grants != 0 {
		t.Errorf("%d requests and %d grants, want none", len(calls), grants)
	}
}

func TestGoogleLocationsSearchRefusesTooManyMatches(t *testing.T) {
	env := newEnvironment(t, nil)
	entries := strings.TrimSuffix(strings.Repeat(`{"name":"googleLocations/a"},`, maxGoogleLocations+1), ",")
	env.fake.answer = reply(http.StatusOK, `{"googleLocations":[`+entries+`]}`)
	if _, err := env.invoke(t, googleLocationsSearchID, "all", `{"query":"a"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("error = %v", err)
	}
}

// The reference tools name no account or location, so a connection bound to either still offers and runs them.
func TestReferenceToolsStayReachableOnEveryTarget(t *testing.T) {
	env := newEnvironment(t, nil)
	calls := map[string]string{
		categoriesListID:        `{"region_code":"DE","language_code":"de","view":"BASIC"}`,
		categoriesBatchGetID:    `{"names":["categories/gcid:a"],"language_code":"de","view":"BASIC"}`,
		chainsGetID:             `{"chain":"chains/a"}`,
		chainsSearchID:          `{"chain_name":"a"}`,
		googleLocationsSearchID: `{"query":"a"}`,
	}
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	for _, tool := range referenceTools {
		for _, targets := range [][]string{nil, {boundAccount}, {boundLocation}} {
			if ok, _ := targetReaches(tool, targets); !ok {
				t.Errorf("targetReaches(%s, %v) = false", tool, targets)
			}
			if ok, _ := metadata.Target.Reaches(tool, targets); !ok {
				t.Errorf("Target.Reaches(%s, %v) = false", tool, targets)
			}
		}
		for _, connection := range []string{"all", "account", "location"} {
			if _, err := env.invoke(t, tool, connection, calls[tool]); err != nil {
				t.Errorf("%s on %s: %v", tool, connection, err)
			}
		}
	}
	if got, _ := env.fake.calls(); len(got) != 15 {
		t.Errorf("%d requests, want 15", len(got))
	}
}

func TestReferenceToolsSayWhatTheyAre(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	for _, d := range reg.Provider(Provider) {
		isReference := false
		for _, id := range referenceTools {
			isReference = isReference || d.ID == id
		}
		if !isReference {
			continue
		}
		if !strings.Contains(d.Description, "not data of the user") || !strings.Contains(d.Description, "every connection offers it") {
			t.Errorf("%s does not say it reads no data of the user: %s", d.ID, d.Description)
		}
		if strings.Contains(d.Description, "pages") {
			t.Errorf("%s description holds a word that other tests search for", d.ID)
		}
		if d.ID == googleLocationsSearchID && (!strings.Contains(d.Description, "not the user's") ||
			!strings.Contains(d.Description, "not logged")) {
			t.Errorf("%s does not warn about other businesses: %s", d.ID, d.Description)
		}
	}
}

func TestReferenceFailuresMapWithoutProviderTextOrSearchTerms(t *testing.T) {
	const term = "search-term-canary-5e2a"
	for name, c := range map[string]struct {
		tool, args string
		status     int
		class      provider.Class
	}{
		"categories 403":       {categoriesListID, `{"region_code":"DE","language_code":"de","view":"BASIC","filter":"` + term + `"}`, 403, provider.ClassPermission},
		"batch 404":            {categoriesBatchGetID, `{"names":["categories/gcid:a"],"language_code":"de","view":"BASIC"}`, 404, provider.ClassNotFound},
		"chains search 429":    {chainsSearchID, `{"chain_name":"` + term + `"}`, 429, provider.ClassRateLimited},
		"chains get 500":       {chainsGetID, `{"chain":"chains/a"}`, 500, provider.ClassProviderError},
		"google locations 400": {googleLocationsSearchID, `{"query":"` + term + `"}`, 400, provider.ClassInvalidResponse},
		"google locations 403": {googleLocationsSearchID, `{"query":"` + term + `"}`, 403, provider.ClassPermission},
	} {
		t.Run(name, func(t *testing.T) {
			env := newEnvironment(t, nil)
			statusText := map[int]string{400: "INVALID_ARGUMENT", 403: "PERMISSION_DENIED", 404: "NOT_FOUND",
				429: "RESOURCE_EXHAUSTED", 500: "INTERNAL"}[c.status]
			reason := ""
			if c.status == 429 {
				reason = "RATE_LIMIT_EXCEEDED"
			}
			env.fake.answer = reply(c.status, errorObject(c.status, statusText, reason))
			_, err := env.invoke(t, c.tool, "all", c.args)
			if classOf(err) != c.class {
				t.Fatalf("error = %v (class %q), want %q", err, classOf(err), c.class)
			}
			if strings.Contains(err.Error(), detailCanary) || strings.Contains(err.Error(), accessPrefix) ||
				strings.Contains(err.Error(), term) {
				t.Errorf("the message carries provider text, a token, or a search term: %v", err)
			}
		})
	}
}

func TestGoogleLocationsSearchRenewsARejectedTokenOnceWithTheSameBody(t *testing.T) {
	env := newEnvironment(t, nil)
	var bodies []string
	env.fake.answer = func(r *http.Request, n int) (*http.Response, error) {
		bodies = append(bodies, bodyOf(t, r))
		if n == 1 {
			return respond(r, http.StatusUnauthorized, "application/json",
				errorObject(401, "UNAUTHENTICATED", "CREDENTIALS_MISSING")), nil
		}
		return respond(r, http.StatusOK, "application/json", `{}`), nil
	}
	if _, err := env.invoke(t, googleLocationsSearchID, "all", `{"query":"a"}`); err != nil {
		t.Fatal(err)
	}
	if len(bodies) != 2 || bodies[0] != bodies[1] || bodies[0] == "" {
		t.Errorf("bodies = %q", bodies)
	}
}
