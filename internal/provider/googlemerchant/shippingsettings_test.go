package googlemerchant

import (
	"encoding/json"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// shipFixture is a setting that uses every kind of member the tool accepts, so one round trip shows that the
// body Google receives is the whole setting.
const shipFixture = `{"services":[` +
	`{"deliveryCountries":["DE","AT"],"serviceName":"Standard","active":true,"currencyCode":"EUR",` +
	`"deliveryTime":{"minTransitDays":2,"maxTransitDays":4,"minHandlingDays":0,"maxHandlingDays":1,` +
	`"cutoffTime":{"hour":14,"minute":30,"timeZone":"Europe/Berlin"},` +
	`"handlingBusinessDayConfig":{"businessDays":["MONDAY","TUESDAY"]}},` +
	`"loyaltyPrograms":[{"programLabel":"club","loyaltyProgramTiers":[{"tierLabel":"gold"}]}],` +
	`"rateGroups":[` +
	`{"applicableShippingLabels":["heavy"],"name":"heavy","carrierRates":[{"name":"post","carrier":"Example Carrier",` +
	`"carrierService":"Ground","originPostalCode":"10115","percentageAdjustment":"-2.5",` +
	`"flatAdjustment":{"amountMicros":"-1000000","currencyCode":"EUR"}}],` +
	`"mainTable":{"rowHeaders":{"weights":[{"amountMicros":"5000000","unit":"KILOGRAM"},{"amountMicros":"-1","unit":"KILOGRAM"}]},` +
	`"columnHeaders":{"postalCodeGroupNames":["zone one","all other locations"]},"rows":[` +
	`{"cells":[{"flatRate":{"amountMicros":"4900000","currencyCode":"EUR"}},{"carrierRate":"post"}]},` +
	`{"cells":[{"subtable":"extra"},{"pricePercentage":"5.4"}]}]},` +
	`"subtables":[{"name":"extra","rowHeaders":{"numberOfItems":["1","infinity"]},"rows":[` +
	`{"cells":[{"noShipping":true}]},{"cells":[{"flatRate":{"amountMicros":"0","currencyCode":"EUR"}}]}]}]},` +
	`{"singleValue":{"flatRate":{"amountMicros":"4900000","currencyCode":"EUR"}}}]},` +
	`{"deliveryCountries":["FR"],"serviceName":"Pickup","active":false,"currencyCode":"EUR","shipmentType":"COLLECTION_POINT",` +
	`"deliveryTime":{"warehouseBasedDeliveryTimes":[{"carrier":"Example Carrier","carrierService":"Ground","warehouse":"Main"}]},` +
	`"minimumOrderValueTable":{"storeCodeSetWithMovs":[{"storeCodes":["s1"],"value":{"amountMicros":"1000000","currencyCode":"EUR"}},` +
	`{"value":{"amountMicros":"2000000","currencyCode":"EUR"}}]}},` +
	`{"deliveryCountries":["US"],"serviceName":"Local","active":true,"currencyCode":"USD","shipmentType":"LOCAL_DELIVERY",` +
	`"deliveryTime":{"transitTimeTable":{"postalCodeGroupNames":["all other locations"],"rows":[{"values":[` +
	`{"minTransitDays":0,"maxTransitDays":1}]}]}},` +
	`"storeConfig":{"storeServiceType":"ALL_STORES","serviceRadius":{"value":10,"unit":"MILES"},` +
	`"cutoffConfig":{"storeCloseOffsetHours":2,"noDeliveryPostCutoff":true}},` +
	`"minimumOrderValue":{"amountMicros":"5000000","currencyCode":"USD"},` +
	`"rateGroups":[{"singleValue":{"noShipping":true}}]}],` +
	`"warehouses":[{"name":"Main","shippingAddress":{"streetAddress":"1 Example Road","city":"Example City",` +
	`"administrativeArea":"Example State","postalCode":"00000","regionCode":"DE"},` +
	`"cutoffTime":{"hour":12,"minute":0},"handlingDays":2,"businessDayConfig":{"businessDays":["FRIDAY"]}}]}`

func shipArgs(etag string) string {
	return `{"account":"1000002","etag":` + checkoutQuoted(etag) + `,` + strings.TrimPrefix(shipFixture, "{")
}

const testShippingAnswer = `{"name":"accounts/1000002/shippingSettings","etag":"etag-two","unknownMember":"dropped",` +
	`"services":[{"serviceName":"Standard","active":true,"currencyCode":"EUR","deliveryCountries":["DE"],` +
	`"deliveryTime":{"minTransitDays":"2","maxTransitDays":"4"},"extra":1}],` +
	`"warehouses":[{"name":"Main","handlingDays":"2"}]}`

func shipAnswer() func(*http.Request) (int, string, http.Header) {
	return func(*http.Request) (int, string, http.Header) { return 200, testShippingAnswer, nil }
}

func TestShippingDescriptorsCarryRiskAndSayInsertReplacesEverything(t *testing.T) {
	g, i := shippingSettingsGet, shippingSettingsInsert
	if g.Version != 1 || g.Risk.Effect != capability.EffectRead || g.RequiresToolAllowList {
		t.Errorf("get = %d, %+v", g.Version, g.Risk)
	}
	r := i.Risk
	if i.Version != 1 || r.Effect != capability.EffectUpdate || r.Confirmation != capability.ConfirmationRequired ||
		!r.OpenWorld || !i.RequiresToolAllowList {
		t.Errorf("insert = %d, %+v", i.Version, r)
	}
	for _, want := range []string{"REPLACES the whole shipping setting", "removed", "never repeated", "etag",
		"applies it with a delay", "only tells that Google accepted"} {
		if !strings.Contains(i.Description, want) {
			t.Errorf("insert description lacks %q", want)
		}
	}
	for _, name := range []string{"etag", "services", "warehouses", "account"} {
		if !strings.Contains(string(i.InputSchema), `"`+name+`"`) {
			t.Errorf("insert does not take %s", name)
		}
	}
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	tools := metadata.Profiles[0].Tools
	if !slices.Contains(tools, shippingSettingsGetID) || slices.Contains(tools, shippingSettingsInsertID) {
		t.Errorf("profile tools = %v", tools)
	}
	if ok, _ := metadata.Target.Reach(shippingSettingsInsertID, []string{"accounts/2"}); !ok {
		t.Error("an account target is refused the insert tool")
	}
	if _, ok := shippingSummaries()[shippingSettingsInsertID]; !ok {
		t.Error("insert has no confirmation summary")
	}
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(i.InputSchema, &schema); err != nil || len(schema.Properties) != 4 {
		t.Fatalf("input schema members = %d, %v; want account, etag, services, warehouses", len(schema.Properties), err)
	}
	for _, name := range []string{"name", "update_mask", "updateMask"} {
		if _, found := schema.Properties[name]; found {
			t.Errorf("insert takes %s", name)
		}
	}
}

func TestShippingConfirmationNamesTheFullReplacement(t *testing.T) {
	summary := shippingSummaries()[shippingSettingsInsertID](json.RawMessage(shipArgs("etag-one")))
	for _, want := range []string{"REPLACE the entire shipping setting", "account 1000002", "3 service(s)",
		`"Standard" (DE,AT)`, "1 warehouse(s)", "not in the call is removed"} {
		if !strings.Contains(summary, want) {
			t.Errorf("summary %q lacks %q", summary, want)
		}
	}
	if shippingSummaries()[shippingSettingsInsertID](json.RawMessage(`[`)) != "" {
		t.Error("a summary of unreadable arguments is not empty")
	}
}

func TestShippingToolsSettleTheAccountBeforeSecretAndRequest(t *testing.T) {
	for name, handler := range map[string]capability.Handler{"get": handleShippingGet, "insert": handleShippingInsert} {
		for label, c := range map[string]struct {
			targets []string
			account string
		}{
			"outside the accounts":      {[]string{"accounts/1"}, `"account":"2"`},
			"not an ID":                 {nil, `"account":"1/users"`},
			"two targets, no argument":  {[]string{"accounts/1", "accounts/2"}, ``},
			"no target and no argument": {nil, ``},
		} {
			t.Run(name+" "+label, func(t *testing.T) {
				h := newHarness(t, c.targets...)
				args := shipArgs("")
				if name == "get" {
					args = `{` + c.account + `}`
				} else if c.account != "" {
					args = strings.Replace(args, `"account":"1000002"`, c.account, 1)
				} else {
					args = strings.Replace(args, `"account":"1000002",`, ``, 1)
				}
				if _, err := handler(t.Context(), h.resolved, h.secrets, h.red, json.RawMessage(args)); !isInvalidRequest(err) {
					t.Errorf("err = %v, want invalid request", err)
				}
				h.untouched(t)
			})
		}
	}
	// A change names its account even when one target would be enough for a read.
	h := newHarness(t, "accounts/1000002")
	if _, err := h.call(t, handleShippingInsert, strings.Replace(shipArgs(""), `"account":"1000002",`, ``, 1)); !isInvalidRequest(err) {
		t.Errorf("err = %v, want invalid request", err)
	}
	h.untouched(t)
}

func TestShippingInsertSendsExactlyTheWholeSettingToTheFixedPath(t *testing.T) {
	h := newHarness(t, "accounts/1000002")
	h.google.api = shipAnswer()
	out, err := h.call(t, handleShippingInsert, shipArgs("etag-one"))
	if err != nil {
		t.Fatal(err)
	}
	got := h.checkoutRequests(t)
	if len(got) != 1 || got[0].method != "POST" || got[0].path != "/accounts/v1/accounts/1000002/shippingSettings:insert" {
		t.Fatalf("requests = %v", got)
	}
	var sent, want map[string]any
	if err := json.Unmarshal([]byte(got[0].body), &sent); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(shipArgs("etag-one")), &want); err != nil {
		t.Fatal(err)
	}
	delete(want, "account")
	// Replace means the body is the complete setting: every service and warehouse of the call, the etag, and
	// nothing else, with no member added, dropped, or renamed on the way.
	if !reflect.DeepEqual(sent, want) {
		t.Errorf("sent body differs from the whole setting\nsent: %s", got[0].body)
	}
	for _, key := range []string{"name", "updateMask", "account"} {
		if _, found := sent[key]; found {
			t.Errorf("body holds %s", key)
		}
	}
	if out["accepted"] != true || out["shippingSettings"].(map[string]any)["etag"] != "etag-two" {
		t.Errorf("out = %v", out)
	}
	// An empty etag for a first insert is sent as an empty string, and warehouses may be left out.
	h = newHarness(t, "accounts/1000002")
	first := `{"account":"1000002","etag":"","services":[{"deliveryCountries":["DE"],"serviceName":"Standard",` +
		`"active":true,"currencyCode":"EUR","deliveryTime":{"minTransitDays":1,"maxTransitDays":2},` +
		`"rateGroups":[{"singleValue":{"noShipping":true}}]}]}`
	if _, err := h.call(t, handleShippingInsert, first); err != nil {
		t.Fatal(err)
	}
	body := h.checkoutRequests(t)[0].body
	if !strings.Contains(body, `"etag":""`) || strings.Contains(body, "warehouses") {
		t.Errorf("body = %s", body)
	}
}

func TestShippingInsertRefusesBadArgumentsBeforeSecretAndRequest(t *testing.T) {
	svc := func(edit func(string) string) string {
		base := `{"account":"1","etag":"e","services":[{"deliveryCountries":["DE"],"serviceName":"S","active":true,` +
			`"currencyCode":"EUR","deliveryTime":{"minTransitDays":1,"maxTransitDays":2},` +
			`"rateGroups":[{"singleValue":{"flatRate":{"amountMicros":"1000000","currencyCode":"EUR"}}}]}]}`
		return edit(base)
	}
	rep := func(old, new string) func(string) string {
		return func(s string) string {
			if !strings.Contains(s, old) {
				t.Fatalf("fixture lacks %s", old)
			}
			return strings.Replace(s, old, new, 1)
		}
	}
	for name, args := range map[string]string{
		"no etag":                 svc(rep(`"etag":"e",`, ``)),
		"no services":             `{"account":"1","etag":"e"}`,
		"empty services":          `{"account":"1","etag":"e","services":[]}`,
		"unknown member":          svc(rep(`"etag":"e"`, `"etag":"e","name":"accounts/1/shippingSettings"`)),
		"unknown nested member":   svc(rep(`"active":true`, `"active":true,"bogus":1`)),
		"not an object":           `{"account":"1","etag":"e","services":"x"}`,
		"no active":               svc(rep(`"active":true,`, ``)),
		"bad country":             svc(rep(`"DE"`, `"de"`)),
		"repeated country":        svc(rep(`["DE"]`, `["DE","DE"]`)),
		"bad currency":            svc(rep(`"currencyCode":"EUR","deliveryTime"`, `"currencyCode":"eur","deliveryTime"`)),
		"price in other currency": svc(rep(`"currencyCode":"EUR"}}}]`, `"currencyCode":"USD"}}}]`)),
		"no delivery time":        svc(rep(`"deliveryTime":{"minTransitDays":1,"maxTransitDays":2},`, ``)),
		"max below min":           svc(rep(`"maxTransitDays":2`, `"maxTransitDays":0`)),
		"handling only min":       svc(rep(`"minTransitDays":1`, `"minTransitDays":1,"minHandlingDays":1`)),
		"unspecified shipment":    svc(rep(`"active":true`, `"active":true,"shipmentType":"SHIPMENT_TYPE_UNSPECIFIED"`)),
		"table without pickup": svc(rep(`"active":true`, `"active":true,"minimumOrderValueTable":{"storeCodeSetWithMovs":[`+
			`{"storeCodes":["a"],"value":{"amountMicros":"1","currencyCode":"EUR"}},{"value":{"amountMicros":"1","currencyCode":"EUR"}}]}`)),
		"store config without local": svc(rep(`"active":true`, `"active":true,"storeConfig":{"storeServiceType":"ALL_STORES",`+
			`"serviceRadius":{"value":1,"unit":"MILES"}}`)),
		"both order values": svc(rep(`"active":true`, `"active":true,"minimumOrderValue":{"amountMicros":"1","currencyCode":"EUR"},`+
			`"minimumOrderValueTable":{"storeCodeSetWithMovs":[]}`)),
		"two values":           svc(rep(`"singleValue":{`, `"singleValue":{"noShipping":true,`)),
		"no shipping false":    svc(rep(`"singleValue":{"flatRate":{"amountMicros":"1000000","currencyCode":"EUR"}}`, `"singleValue":{"noShipping":false}`)),
		"unknown carrier rate": svc(rep(`"singleValue":{"flatRate":{"amountMicros":"1000000","currencyCode":"EUR"}}`, `"singleValue":{"carrierRate":"x"}`)),
		"subtable in single":   svc(rep(`"singleValue":{"flatRate":{"amountMicros":"1000000","currencyCode":"EUR"}}`, `"singleValue":{"subtable":"x"}`)),
		"no value":             svc(rep(`"singleValue":{"flatRate":{"amountMicros":"1000000","currencyCode":"EUR"}}`, `"applicableShippingLabels":["a"]`)),
		"empty label not last": svc(rep(`"rateGroups":[`, `"rateGroups":[{"singleValue":{"noShipping":true}},`)),
		"negative flat rate":   svc(rep(`"amountMicros":"1000000"`, `"amountMicros":"-1"`)),
		"percentage letters":   svc(rep(`"singleValue":{"flatRate":{"amountMicros":"1000000","currencyCode":"EUR"}}`, `"singleValue":{"pricePercentage":"five"}`)),
		"too many services":    `{"account":"1","etag":"e","services":[` + strings.Repeat(`{},`, 50) + `{}]}`,
		"service without name": svc(rep(`"serviceName":"S"`, `"serviceName":""`)),
		"control character":    svc(rep(`"serviceName":"S"`, `"serviceName":"S\u0007"`)),
		"duplicate service": svc(func(s string) string {
			one := s[strings.Index(s, `[{`)+1 : strings.LastIndex(s, `}]`)+1]
			return strings.Replace(s, one, one+","+one, 1)
		}),
		"warehouse without address": svc(rep(`"etag":"e"`, `"etag":"e","warehouses":[{"name":"W","cutoffTime":{"hour":1,"minute":1},"handlingDays":1}]`)),
		"warehouse hour 24": svc(rep(`"etag":"e"`, `"etag":"e","warehouses":[{"name":"W","shippingAddress":{"city":"c",`+
			`"administrativeArea":"a","postalCode":"1","regionCode":"DE"},"cutoffTime":{"hour":24,"minute":1},"handlingDays":1}]`)),
		"bad weekday": svc(rep(`"minTransitDays":1`, `"minTransitDays":1,"transitBusinessDayConfig":{"businessDays":["MON"]}`)),
		"warehouse times with table": svc(rep(`"minTransitDays":1,"maxTransitDays":2`, `"warehouseBasedDeliveryTimes":[{"carrier":"c",`+
			`"carrierService":"s","warehouse":"w"}],"minTransitDays":1`)),
		"table rows differ from headers": svc(rep(`"singleValue":{"flatRate":{"amountMicros":"1000000","currencyCode":"EUR"}}`,
			`"mainTable":{"rowHeaders":{"numberOfItems":["1","infinity"]},"rows":[{"cells":[{"noShipping":true}]}]}`)),
		"infinity not last": svc(rep(`"singleValue":{"flatRate":{"amountMicros":"1000000","currencyCode":"EUR"}}`,
			`"mainTable":{"rowHeaders":{"numberOfItems":["infinity","1"]},"rows":[{"cells":[{"noShipping":true}]},{"cells":[{"noShipping":true}]}]}`)),
		"two header kinds": svc(rep(`"singleValue":{"flatRate":{"amountMicros":"1000000","currencyCode":"EUR"}}`,
			`"mainTable":{"rowHeaders":{"numberOfItems":["1"],"postalCodeGroupNames":["a"]},"rows":[{"cells":[{"noShipping":true}]}]}`)),
		"weight unit kg": svc(rep(`"singleValue":{"flatRate":{"amountMicros":"1000000","currencyCode":"EUR"}}`,
			`"mainTable":{"rowHeaders":{"weights":[{"amountMicros":"-1","unit":"kg"}]},"rows":[{"cells":[{"noShipping":true}]}]}`)),
		"weight unit unspecified": svc(rep(`"singleValue":{"flatRate":{"amountMicros":"1000000","currencyCode":"EUR"}}`,
			`"mainTable":{"rowHeaders":{"weights":[{"amountMicros":"-1","unit":"WEIGHT_UNIT_UNSPECIFIED"}]},"rows":[{"cells":[{"noShipping":true}]}]}`)),
		"weights in two units": svc(rep(`"singleValue":{"flatRate":{"amountMicros":"1000000","currencyCode":"EUR"}}`,
			`"mainTable":{"rowHeaders":{"weights":[{"amountMicros":"1","unit":"POUND"},{"amountMicros":"-1","unit":"KILOGRAM"}]},"rows":[{"cells":[{"noShipping":true}]},{"cells":[{"noShipping":true}]}]}`)),
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, "accounts/1")
			if _, err := h.call(t, handleShippingInsert, args); !isInvalidRequest(err) {
				t.Errorf("err = %v, want invalid request", err)
			}
			h.untouched(t)
		})
	}
}

func TestShippingGetShowsTheSettingAndCutsUntrustedContent(t *testing.T) {
	h := newHarness(t, "accounts/1000002")
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, strings.Replace(testShippingAnswer, `"Main"`, `"`+strings.Repeat("w", 400)+`"`, 1), nil
	}
	out, err := h.call(t, handleShippingGet, `{}`)
	if err != nil {
		t.Fatal(err)
	}
	service := out["services"].([]any)[0].(map[string]any)
	time := service["deliveryTime"].(map[string]any)
	house := out["warehouses"].([]any)[0].(map[string]any)
	if out["name"] != "accounts/1000002/shippingSettings" || out["etag"] != "etag-two" || out["unknownMember"] != nil ||
		service["serviceName"] != "Standard" || service["active"] != true || service["extra"] != nil ||
		time["minTransitDays"] != float64(2) || time["maxTransitDays"] != float64(4) ||
		house["handlingDays"] != float64(2) || len([]rune(house["name"].(string))) != maxNameRunes {
		t.Errorf("out = %v", out)
	}
	if got := h.checkoutRequests(t); len(got) != 1 ||
		got[0] != (checkoutRequest{"GET", "/accounts/v1/accounts/1000002/shippingSettings", ""}) {
		t.Errorf("requests = %v", got)
	}
	// A value of the wrong type is an invalid answer, not a silent drop.
	h = newHarness(t, "accounts/1000002")
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, `{"services":{"a":1}}`, nil }
	if _, err := h.call(t, handleShippingGet, `{}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("err = %v", err)
	}
}

func TestShippingGetReadsWeightUnitsAsGoogleNamesThem(t *testing.T) {
	h := newHarness(t, "accounts/1000002")
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"services":[{"serviceName":"S","rateGroups":[{"mainTable":{"rowHeaders":{"weights":[` +
			`{"amountMicros":"-1","unit":"KILOGRAM"}]},"rows":[{"cells":[{"noShipping":true}]}]}}]}]}`, nil
	}
	out, err := h.call(t, handleShippingGet, `{}`)
	if err != nil {
		t.Fatal(err)
	}
	table := out["services"].([]any)[0].(map[string]any)["rateGroups"].([]any)[0].(map[string]any)["mainTable"].(map[string]any)
	weights := table["rowHeaders"].(map[string]any)["weights"].([]any)
	if weights[0].(map[string]any)["unit"] != "KILOGRAM" {
		t.Errorf("weights = %v", weights)
	}
}

func TestShippingToolsProveASubAccountOfABoundAdvancedAccount(t *testing.T) {
	proof := func(req *http.Request) (int, string, http.Header) {
		if strings.HasSuffix(req.URL.Path, ":listSubaccounts") {
			return 200, `{"accounts":[` + accountRow("2", "Two") + `]}`, nil
		}
		return 200, testShippingAnswer, nil
	}
	h := newHarness(t, "advanced-accounts/1")
	h.google.api = proof
	if _, err := h.call(t, handleShippingGet, `{"account":"2"}`); err != nil {
		t.Fatal(err)
	}
	if got := h.checkoutRequests(t); len(got) != 2 || got[1].path != "/accounts/v1/accounts/2/shippingSettings" {
		t.Errorf("requests = %v", got)
	}
	args := strings.Replace(shipArgs("e"), `"1000002"`, `"2"`, 1)
	h = newHarness(t, "advanced-accounts/1")
	h.google.api = proof
	if _, err := h.call(t, handleShippingInsert, args); err != nil {
		t.Fatal(err)
	}
	h = newHarness(t, "advanced-accounts/1")
	h.google.api = proof
	if _, err := h.call(t, handleShippingInsert, strings.Replace(args, `"2"`, `"3"`, 1)); !isInvalidRequest(err) {
		t.Errorf("err = %v", err)
	}
	for _, r := range h.checkoutRequests(t) {
		if r.method != "GET" {
			t.Errorf("an unproven account was changed: %v", r)
		}
	}
}

func TestShippingInsertIsSentOnceAndAnUnclearOutcomeIsSaid(t *testing.T) {
	for name, c := range map[string]struct {
		status  int
		unclear bool
		class   provider.Class
	}{
		"server error": {500, true, provider.ClassProviderError},
		"unavailable":  {503, true, provider.ClassUnreachable},
		"unauthorized": {401, false, provider.ClassAuth},
		"rate limited": {429, false, provider.ClassRateLimited},
		"bad request":  {400, false, ""},
		"stale etag":   {409, false, ""},
		"not found":    {404, false, provider.ClassNotFound},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, "accounts/1000002")
			h.google.api = func(*http.Request) (int, string, http.Header) {
				return c.status, `{"error":{"code":0,"message":"untrusted text","status":"X"}}`, nil
			}
			_, err := h.call(t, handleShippingInsert, shipArgs("e"))
			if err == nil {
				t.Fatal("no error")
			}
			if c.class != "" && classOf(err) != c.class {
				t.Errorf("class = %s, want %s", classOf(err), c.class)
			}
			if unclear := strings.Contains(err.Error(), "outcome of the change is unknown"); unclear != c.unclear {
				t.Errorf("unclear outcome said = %t: %v", unclear, err)
			}
			if strings.Contains(err.Error(), "untrusted text") {
				t.Errorf("the message carries Google's text: %v", err)
			}
			if got := h.checkoutRequests(t); len(got) != 1 {
				t.Errorf("%d requests, want exactly one", len(got))
			}
		})
	}
}
