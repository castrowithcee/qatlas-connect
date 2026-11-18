package googlebusinessprofile

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
)

const (
	attrWifi = "attributes/has_wifi"
	attrMenu = "attributes/url_menu"
	attrPay  = "attributes/pay_cards"
	attrSeat = "attributes/seating"
	attrOld  = "attributes/old_flag"
)

// metadataPages are two pages of attribute metadata: a boolean, a URL, an enumeration, a repeated
// enumeration, and a deprecated attribute, and a member the discovery document does not name.
var metadataPages = map[string]string{
	"": `{"attributeMetadata":[
{"parent":"` + attrWifi + `","displayName":"Wi-Fi","valueType":"BOOL","groupDisplayName":"Amenities",
 "valueMetadata":[{"displayName":"Yes","value":true},{"displayName":"No","value":false}],"undocumented":"provider-detail-canary-7c1d"},
{"parent":"` + attrMenu + `","displayName":"Menu link","valueType":"URL","repeatable":true},
{"parent":"` + attrSeat + `","displayName":"Seating","valueType":"ENUM",
 "valueMetadata":[{"displayName":"Indoor","value":"INDOOR"},{"displayName":"Outdoor","value":"OUTDOOR"}]}],
"nextPageToken":"page-2"}`,
	"page-2": `{"attributeMetadata":[
{"parent":"` + attrPay + `","displayName":"Cards","valueType":"REPEATED_ENUM","repeatable":true,
 "valueMetadata":[{"displayName":"Visa","value":"VISA"},{"displayName":"Amex","value":"AMEX"}]},
{"parent":"` + attrOld + `","displayName":"Old","valueType":"BOOL","deprecated":true,
 "valueMetadata":[{"value":true}]}]}`,
}

const currentAttributes = `{"name":"` + boundLocation + `/attributes","attributes":[
{"name":"` + attrWifi + `","valueType":"BOOL","values":[false]},
{"name":"` + attrMenu + `","valueType":"URL","uriValues":[{"uri":"https://menu.example.com/a"}]},
{"name":"attributes/pay_other","valueType":"REPEATED_ENUM","repeatedEnumValue":{"setValues":["X"],"unsetValues":["Y"]}}]}`

// attributeFake routes the business information API for attributes and keeps what was sent.
type attributeFake struct {
	mu      sync.Mutex
	methods []string
	current string
}

func (a *attributeFake) answer(r *http.Request, _ int) (*http.Response, error) {
	a.mu.Lock()
	a.methods = append(a.methods, r.Method)
	a.mu.Unlock()
	path := r.URL.Path
	switch {
	case r.Method == http.MethodPatch:
		return respond(r, http.StatusOK, "application/json", `{"name":"`+boundLocation+`/attributes","attributes":[`+
			`{"name":"`+attrWifi+`","valueType":"BOOL","values":[true]}]}`), nil
	case path == "/v1/attributes":
		return respond(r, http.StatusOK, "application/json", metadataPages[r.URL.Query().Get("pageToken")]), nil
	case path == "/v1/"+boundLocation+"/attributes":
		return respond(r, http.StatusOK, "application/json", a.current), nil
	case path == "/v1/"+boundAccount+"/locations":
		return respond(r, http.StatusOK, "application/json", `{"locations":[{"name":"`+boundLocation+`"}]}`), nil
	}
	return respond(r, http.StatusNotFound, "application/json", errorObject(404, "NOT_FOUND", "NOT_FOUND")), nil
}

func attributeEnvironment(t *testing.T) (*environment, *attributeFake) {
	t.Helper()
	env, _ := writeEnvironment(t)
	a := &attributeFake{current: currentAttributes}
	env.fake.answer = a.answer
	return env, a
}

func (a *attributeFake) patched() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return strings.Contains(strings.Join(a.methods, " "), http.MethodPatch)
}

func TestAttributeMetadataListsPagesAndEveryDocumentedMember(t *testing.T) {
	env, _ := attributeEnvironment(t)
	out, err := env.invoke(t, attributesListID, "all", `{"region_code":"CH","language_code":"de","show_all":true,"page_size":3}`)
	if err != nil {
		t.Fatal(err)
	}
	first := out["attributeMetadata"].([]any)
	if len(first) != 3 || out["next_cursor"] == nil {
		t.Fatalf("first page = %v", out)
	}
	wifi := first[0].(map[string]any)
	want := map[string]any{"parent": attrWifi, "displayName": "Wi-Fi", "valueType": "BOOL", "groupDisplayName": "Amenities",
		"valueMetadata": []any{map[string]any{"displayName": "Yes", "value": true}, map[string]any{"displayName": "No", "value": false}}}
	if !reflect.DeepEqual(wifi, want) {
		t.Errorf("wifi = %v, want %v", wifi, want)
	}
	if first[1].(map[string]any)["repeatable"] != true {
		t.Errorf("repeatable missing: %v", first[1])
	}
	calls, _ := env.fake.calls()
	q := calls[0].values()
	if calls[0].path != "/v1/attributes" || q.Get("regionCode") != "CH" || q.Get("languageCode") != "de" ||
		q.Get("showAll") != "true" || q.Get("pageSize") != "3" || q.Has("parent") || q.Has("categoryName") {
		t.Errorf("request = %+v", calls[0])
	}
	second, err := env.invoke(t, attributesListID, "all",
		`{"region_code":"CH","language_code":"de","show_all":true,"page_size":3,"cursor":"`+out["next_cursor"].(string)+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	items := second["attributeMetadata"].([]any)
	if len(items) != 2 || items[1].(map[string]any)["deprecated"] != true || second["next_cursor"] != nil {
		t.Errorf("second page = %v", second)
	}
	calls, _ = env.fake.calls()
	if calls[1].values().Get("pageToken") != "page-2" {
		t.Errorf("page token = %v", calls[1].values())
	}
	if strings.Contains(string(mustMarshal(t, out)), detailCanary) {
		t.Error("an undocumented member was returned")
	}
	// A cursor of other arguments is refused.
	if _, err := env.invoke(t, attributesListID, "all", `{"region_code":"DE","cursor":"`+out["next_cursor"].(string)+`"}`); !isInvalidRequest(err) {
		t.Errorf("foreign cursor error = %v", err)
	}
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestAttributeMetadataForALocationAndItsTarget(t *testing.T) {
	env, _ := attributeEnvironment(t)
	if _, err := env.invoke(t, attributesListID, "account", `{"location":"`+boundLocation+`","category_name":"categories/gcid:bakery"}`); err != nil {
		t.Fatal(err)
	}
	calls, _ := env.fake.calls()
	if len(calls) != 2 || calls[0].path != "/v1/"+boundAccount+"/locations" || calls[1].path != "/v1/attributes" ||
		calls[1].values().Get("parent") != boundLocation || calls[1].values().Get("categoryName") != "categories/gcid:bakery" {
		t.Errorf("requests = %+v", calls)
	}
	for name, c := range map[string]struct{ connection, args string }{
		"neither location nor region": {"all", `{"show_all":true}`},
		"other location":              {"location", `{"location":"` + otherLocation + `"}`},
		"bad location":                {"all", `{"location":"locations/x"}`},
		"bad region":                  {"all", `{"region_code":"ch"}`},
		"bad category":                {"all", `{"region_code":"CH","category_name":"categories/../x"}`},
		"page size 201":               {"all", `{"region_code":"CH","page_size":201}`},
	} {
		if _, err := env.invoke(t, attributesListID, c.connection, c.args); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if calls, _ = env.fake.calls(); len(calls) != 2 {
		t.Errorf("refusals sent requests: %+v", calls[2:])
	}
}

func TestAttributesGetShowsValuesAndAddressesAsText(t *testing.T) {
	env, a := attributeEnvironment(t)
	long := strings.Repeat("u", 3000)
	a.current = `{"name":"` + boundLocation + `/attributes","attributes":[
{"name":"` + attrMenu + `","valueType":"URL","uriValues":[{"uri":"https://menu.example.com/` + long + `"}]},
{"name":"` + attrWifi + `","valueType":"BOOL","values":[true,"x",7,null]},
{"name":"` + attrPay + `","valueType":"REPEATED_ENUM","repeatedEnumValue":{"setValues":["VISA"],"unsetValues":["AMEX"]},"undocumented":"x"}]}`
	out, err := env.invoke(t, attributesGetID, "account", `{"location":"`+boundLocation+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	attrs := out["attributes"].([]any)
	menu := attrs[0].(map[string]any)["uriValues"].([]any)[0].(map[string]any)["uri"].(string)
	if !strings.HasSuffix(menu, "…") || len([]rune(menu)) != maxURLRunes+1 {
		t.Errorf("uri not cut: %d runes", len([]rune(menu)))
	}
	if values := attrs[1].(map[string]any)["values"]; !reflect.DeepEqual(values, []any{true, "x"}) {
		t.Errorf("values = %v", values)
	}
	if enum := attrs[2].(map[string]any)["repeatedEnumValue"]; !reflect.DeepEqual(enum,
		map[string]any{"setValues": []any{"VISA"}, "unsetValues": []any{"AMEX"}}) {
		t.Errorf("enum = %v", enum)
	}
	calls, _ := env.fake.calls()
	if len(calls) != 2 || calls[1].path != "/v1/"+boundLocation+"/attributes" || calls[1].query != "" {
		t.Errorf("requests = %+v", calls)
	}
	// Only the bound location is read, and nothing but the attributes of it.
	if _, err := env.invoke(t, attributesGetID, "location", `{"location":"`+otherLocation+`"}`); err == nil {
		t.Error("another location was accepted")
	}
	if _, err := env.invoke(t, attributesGetID, "location", `{}`); err != nil {
		t.Errorf("bound location: %v", err)
	}
}

func setWifi(previous string) string {
	return `{"location":"` + boundLocation + `","attribute_mask":["` + attrWifi + `"],` +
		`"attributes":[{"name":"` + attrWifi + `","values":[true]}],"previous_attributes":[` + previous + `]}`
}

const previousWifi = `{"name":"` + attrWifi + `","valueType":"BOOL","values":[false]}`

func TestAttributesUpdateShowsOldAndNewValuesAndSendsOnlyTheChange(t *testing.T) {
	env, a := attributeEnvironment(t)
	args := setWifi(previousWifi)
	_, err := env.core.Invoke(context.Background(), application.InvokeRequest{Operation: attributesUpdateID,
		Connection: "w-account", Arguments: json.RawMessage(args)})
	var needs *application.ConfirmationRequiredError
	if !errors.As(err, &needs) {
		t.Fatalf("error = %v, want confirmation required", err)
	}
	for _, want := range []string{boundLocation, attrWifi, "false", "true"} {
		if !strings.Contains(needs.Summary, want) {
			t.Errorf("summary %q lacks %q", needs.Summary, want)
		}
	}
	env.noRequests(t)

	out, err := env.change(t, attributesUpdateID, "w-account", args)
	if err != nil || out["name"] != boundLocation+"/attributes" {
		t.Fatalf("result = %v, %v", out, err)
	}
	calls, _ := env.fake.calls()
	// Proof of the location, the metadata over two pages is not needed since the attribute is on page 1, the
	// current attributes, and then the one change.
	if len(calls) != 4 || calls[0].path != "/v1/"+boundAccount+"/locations" || calls[1].path != "/v1/attributes" ||
		calls[1].values().Get("parent") != boundLocation || calls[2].path != "/v1/"+boundLocation+"/attributes" ||
		calls[3].path != "/v1/"+boundLocation+"/attributes" || a.methods[3] != http.MethodPatch ||
		calls[3].host != "mybusinessbusinessinformation.googleapis.com" {
		t.Fatalf("requests = %+v %v", calls, a.methods)
	}
	if calls[3].values().Get("attributeMask") != attrWifi || len(calls[3].values()) != 1 {
		t.Errorf("query = %v", calls[3].values())
	}
	if want := `{"name":"` + boundLocation + `/attributes","attributes":[{"name":"` + attrWifi + `","values":[true]}]}`; calls[3].body != want {
		t.Errorf("body = %s, want %s", calls[3].body, want)
	}
}

func TestAttributesUpdateShowsAnAddressCompletely(t *testing.T) {
	env, _ := attributeEnvironment(t)
	address := "https://menu.example.com/new/" + strings.Repeat("p", 150)
	args := `{"location":"` + boundLocation + `","attribute_mask":["` + attrMenu + `"],` +
		`"attributes":[{"name":"` + attrMenu + `","uriValues":[{"uri":"` + address + `"}]}],` +
		`"previous_attributes":[{"name":"` + attrMenu + `","uriValues":[{"uri":"https://menu.example.com/a"}]}]}`
	_, err := env.core.Invoke(context.Background(), application.InvokeRequest{Operation: attributesUpdateID,
		Connection: "w-all", Arguments: json.RawMessage(args)})
	var needs *application.ConfirmationRequiredError
	if !errors.As(err, &needs) {
		t.Fatalf("error = %v", err)
	}
	if !strings.Contains(needs.Summary, address) || !strings.Contains(needs.Summary, "https://menu.example.com/a") {
		t.Errorf("summary lacks an address: %q", needs.Summary)
	}
	// An address is never fetched: only the API origin is contacted.
	if _, err := env.change(t, attributesUpdateID, "w-all", args); err != nil {
		t.Fatal(err)
	}
	calls, _ := env.fake.calls()
	for _, call := range calls {
		if call.host != "mybusinessbusinessinformation.googleapis.com" {
			t.Errorf("request to %s", call.host)
		}
	}
}

func TestAttributesUpdateRemovesAnAttributeNamedOnlyInTheMask(t *testing.T) {
	env, _ := attributeEnvironment(t)
	args := `{"location":"` + boundLocation + `","attribute_mask":["` + attrMenu + `"],"attributes":[],` +
		`"previous_attributes":[{"name":"` + attrMenu + `","uriValues":[{"uri":"https://menu.example.com/a"}]}]}`
	if _, err := env.change(t, attributesUpdateID, "w-all", args); err != nil {
		t.Fatal(err)
	}
	calls, _ := env.fake.calls()
	last := calls[len(calls)-1]
	if last.body != `{"name":"`+boundLocation+`/attributes","attributes":[]}` || last.values().Get("attributeMask") != attrMenu {
		t.Errorf("change = %+v", last)
	}
}

func TestAttributesUpdateNeedsMaskAndConfirmationAndRefusesWithoutIO(t *testing.T) {
	env, _ := attributeEnvironment(t)
	// Five addresses of 2000 characters exceed the bound of the tool.
	long := strings.TrimSuffix(strings.Repeat(`{"uri":"https://a.example.com/`+strings.Repeat("p", 1960)+`"},`, 5), ",")
	for name, args := range map[string]string{
		"no mask":    `{"location":"` + boundLocation + `","attributes":[{"name":"` + attrWifi + `","values":[true]}],"previous_attributes":[]}`,
		"empty mask": `{"location":"` + boundLocation + `","attribute_mask":[],"attributes":[],"previous_attributes":[]}`,
		"attribute not in mask": `{"location":"` + boundLocation + `","attribute_mask":["` + attrMenu + `"],` +
			`"attributes":[{"name":"` + attrWifi + `","values":[true]}],"previous_attributes":[]}`,
		"no previous":           `{"location":"` + boundLocation + `","attribute_mask":["` + attrWifi + `"],"attributes":[{"name":"` + attrWifi + `","values":[true]}]}`,
		"previous outside mask": setWifi(previousWifi + `,{"name":"` + attrMenu + `","values":[true]}`),
		"valueType in new": `{"location":"` + boundLocation + `","attribute_mask":["` + attrWifi + `"],` +
			`"attributes":[{"name":"` + attrWifi + `","valueType":"BOOL","values":[true]}],"previous_attributes":[]}`,
		"two kinds": `{"location":"` + boundLocation + `","attribute_mask":["` + attrWifi + `"],` +
			`"attributes":[{"name":"` + attrWifi + `","values":[true],"uriValues":[{"uri":"https://a.example.com"}]}],"previous_attributes":[]}`,
		"unchanged": setWifi(`{"name":"` + attrWifi + `","values":[true]}`),
		"file uri": `{"location":"` + boundLocation + `","attribute_mask":["` + attrMenu + `"],` +
			`"attributes":[{"name":"` + attrMenu + `","uriValues":[{"uri":"file:///etc/passwd"}]}],"previous_attributes":[]}`,
		"user info": `{"location":"` + boundLocation + `","attribute_mask":["` + attrMenu + `"],` +
			`"attributes":[{"name":"` + attrMenu + `","uriValues":[{"uri":"https://user:pw@192.0.2.1/"}]}],"previous_attributes":[]}`,
		"non ascii": `{"location":"` + boundLocation + `","attribute_mask":["` + attrMenu + `"],` +
			`"attributes":[{"name":"` + attrMenu + `","uriValues":[{"uri":"https://menü.example.com/"}]}],"previous_attributes":[]}`,
		"bidi in value": `{"location":"` + boundLocation + `","attribute_mask":["` + attrSeat + `"],` +
			`"attributes":[{"name":"` + attrSeat + `","values":["IN‮DOOR"]}],"previous_attributes":[]}`,
		"summary too long": `{"location":"` + boundLocation + `","attribute_mask":["` + attrMenu + `"],` +
			`"attributes":[{"name":"` + attrMenu + `","uriValues":[` + long + `]}],"previous_attributes":[]}`,
		"other location": `{"location":"` + otherLocation + `","attribute_mask":["` + attrWifi + `"],` +
			`"attributes":[{"name":"` + attrWifi + `","values":[true]}],"previous_attributes":[` + previousWifi + `]}`,
		"bad name": `{"location":"` + boundLocation + `","attribute_mask":["attributes/../x"],"attributes":[],"previous_attributes":[]}`,
	} {
		connection := "w-all"
		if name == "other location" {
			connection = "w-location"
		}
		if _, err := env.change(t, attributesUpdateID, connection, args); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	env.noRequests(t)
}

func TestAttributesUpdateChecksTheStateAndTheMetadataBeforeChanging(t *testing.T) {
	env, a := attributeEnvironment(t)
	mask := func(name, value string) string {
		return `{"location":"` + boundLocation + `","attribute_mask":["` + name + `"],"attributes":[{"name":"` + name +
			`",` + value + `}],"previous_attributes":[]}`
	}
	for name, args := range map[string]string{
		"previous differs":          setWifi(`{"name":"` + attrWifi + `","values":[true]}`),
		"previous missing":          setWifi(``),
		"deprecated":                mask(attrOld, `"values":[true]`),
		"unknown attribute":         mask("attributes/nothing", `"values":[true]`),
		"bool needs bool":           strings.Replace(setWifi(previousWifi), `"values":[true]`, `"values":["YES"]`, 1),
		"enum value not listed":     mask(attrSeat, `"values":["ROOFTOP"]`),
		"enum needs a value":        mask(attrSeat, `"uriValues":[{"uri":"https://a.example.com/"}]`),
		"url needs an address":      mask(attrMenu, `"values":[true]`),
		"repeated not listed":       mask(attrPay, `"repeatedEnumValue":{"setValues":["DISCOVER"]}`),
		"repeated set and unset":    mask(attrPay, `"repeatedEnumValue":{"setValues":["VISA"],"unsetValues":["VISA"]}`),
		"two values not repeatable": strings.Replace(setWifi(previousWifi), `"values":[true]`, `"values":[true,false]`, 1),
	} {
		if _, err := env.change(t, attributesUpdateID, "w-all", args); !isInvalidRequest(err) {
			t.Errorf("%s: error = %v, want invalid request", name, err)
		}
	}
	if a.patched() {
		t.Fatal("a change was sent")
	}
}

func TestAttributesUpdateWithAnAttributeBeyondTheFirstMetadataPage(t *testing.T) {
	env, a := attributeEnvironment(t)
	args := `{"location":"` + boundLocation + `","attribute_mask":["` + attrPay + `"],` +
		`"attributes":[{"name":"` + attrPay + `","repeatedEnumValue":{"setValues":["VISA"],"unsetValues":["AMEX"]}}],"previous_attributes":[]}`
	if _, err := env.change(t, attributesUpdateID, "w-all", args); err != nil {
		t.Fatal(err)
	}
	calls, _ := env.fake.calls()
	if len(calls) != 4 || calls[0].values().Has("pageToken") || calls[1].values().Get("pageToken") != "page-2" || !a.patched() {
		t.Errorf("requests = %+v", calls)
	}
}

func TestAttributesUpdateProofIsRefusedOutsideTheTarget(t *testing.T) {
	env, a := attributeEnvironment(t)
	// An account target refuses a location its list does not hold, before the metadata or a change.
	args := strings.Replace(setWifi(previousWifi), boundLocation, otherLocation, 1)
	if _, err := env.change(t, attributesUpdateID, "w-account", args); !isInvalidRequest(err) {
		t.Errorf("error = %v", err)
	}
	if a.patched() {
		t.Fatal("a change was sent")
	}
	calls, _ := env.fake.calls()
	if len(calls) != 1 || calls[0].path != "/v1/"+boundAccount+"/locations" {
		t.Errorf("requests = %+v", calls)
	}
}

func TestAttributesUpdateAfterAnUncertainOutcomeIsNotRepeated(t *testing.T) {
	env, _ := attributeEnvironment(t)
	inner := env.fake.answer
	env.fake.answer = func(r *http.Request, n int) (*http.Response, error) {
		if r.Method == http.MethodPatch {
			return respond(r, http.StatusServiceUnavailable, "application/json", errorObject(503, "UNAVAILABLE", "BACKEND")), nil
		}
		return inner(r, n)
	}
	_, err := env.change(t, attributesUpdateID, "w-all", setWifi(previousWifi))
	if err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("error = %v, want an unknown outcome", err)
	}
	calls, _ := env.fake.calls()
	patches := 0
	for _, call := range calls {
		if strings.HasSuffix(call.path, "/attributes") && call.body != "" {
			patches++
		}
	}
	if patches != 1 {
		t.Errorf("%d changes sent", patches)
	}
}

func TestAttributeToolsAreInTheProfiles(t *testing.T) {
	for _, id := range []string{attributesListID, attributesGetID} {
		if !contains(readTools, id) || contains(writeTools, id) || len(toolScopes[id]) != 1 {
			t.Errorf("%s misplaced", id)
		}
	}
	if !contains(writeTools, attributesUpdateID) || contains(readTools, attributesUpdateID) || len(toolScopes[attributesUpdateID]) != 1 {
		t.Error("attributes.update misplaced")
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
