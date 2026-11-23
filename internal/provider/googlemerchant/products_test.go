package googlemerchant

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// wantProductAttributes are the 145 members of ProductAttributes in the product discovery document of the
// Merchant API v1, none of them deprecated. A change of the document shows up here.
var wantProductAttributes = []string{
	"additionalImageLinks", "adsGrouping", "adsLabels", "adsRedirect", "adult", "ageGroup", "amenityFeature",
	"autoPricingMinPrice", "availability", "availabilityDate", "bodyStyle", "brand", "canonicalLink",
	"carrierShipping", "certifications", "certifiedPreOwned", "cloudExportAdditionalProperties", "co2Emissions",
	"color", "condition", "costOfGoodsSold", "customLabel0", "customLabel1", "customLabel2", "customLabel3",
	"customLabel4", "dateFirstRegistered", "description", "disclosureDate", "displayAddress", "displayAdsId",
	"displayAdsLink", "displayAdsSimilarIds", "displayAdsTitle", "displayAdsValue", "documentLinks",
	"electricRange", "emissionsStandard", "energyConsumption", "energyEfficiencyClass", "engine",
	"excludedDestinations", "expirationDate", "externalSellerId", "freeShippingThreshold", "fuelConsumption",
	"fuelConsumptionDischargedBattery", "gender", "googleProductCategory", "gtins", "handlingCutoffTimes",
	"identifierExists", "imageLink", "includedDestinations", "installment", "isBundle", "itemGroupId",
	"itemGroupTitle", "latitude", "leaseTerm", "lifestyleImageLinks", "link", "linkTemplate", "longitude",
	"loyaltyPoints", "loyaltyPrograms", "material", "maxEnergyEfficiencyClass", "maxHandlingTime",
	"maximumRetailPrice", "mileage", "minEnergyEfficiencyClass", "minHandlingTime", "minimumOrderValues",
	"mobileLink", "mobileLinkTemplate", "model", "mpn", "multipack", "neighborhood", "numberOfBathrooms",
	"numberOfBedrooms", "numberOfUnits", "pattern", "pause", "petPolicy", "pickupCost", "pickupMethod",
	"pickupSla", "popularityRank", "price", "productDetails", "productFee", "productHeight",
	"productHighlights", "productLength", "productTypes", "productWeight", "productWidth", "promotionIds",
	"propertyName", "propertyType", "questionsAndAnswers", "relatedProducts", "returnPolicyLabel", "returns",
	"salePrice", "salePriceEffectiveDate", "sellOnGoogleQuantity", "shipping", "shippingHandlingBusinessDays",
	"shippingHeight", "shippingLabel", "shippingLength", "shippingTransitBusinessDays", "shippingWeight",
	"shippingWidth", "shoppingAdsExcludedCountries", "shortTitle", "size", "sizeSystem", "sizeTypes",
	"specialtyHousingType", "structuredDescription", "structuredTitle", "subscriptionCost",
	"sustainabilityIncentives", "title", "transitTimeLabel", "trim", "unitArea", "unitPricingBaseMeasure",
	"unitPricingMeasure", "utilitiesIncluded", "variantOptions", "vehicleAllInPrice", "vehicleExpenses",
	"vehicleMandatoryInspectionIncluded", "vehicleMsrp", "vehiclePriceType", "videoLinks", "vin",
	"virtualModelLink", "warranty", "year",
}

// sample writes a value of the right type for every member of a node.
func sample(n *node) any {
	switch n.kind {
	case nodeText:
		return "text"
	case nodeBool:
		return true
	case nodeNumber:
		return 1.5
	case nodeInt:
		return "7"
	case nodeList:
		return []any{sample(n.item)}
	}
	out := map[string]any{}
	for _, fl := range n.fields {
		out[fl.name] = sample(fl.node)
	}
	return out
}

func productRow(offer string) string {
	return `{"name":"accounts/1000001/products/en~US~` + offer + `","offerId":"` + offer + `","contentLanguage":"en",` +
		`"feedLabel":"US","dataSource":"accounts/1000001/dataSources/5","versionNumber":"12","archived":false,` +
		`"productAttributes":{"title":"Synthetic title","price":{"currencyCode":"CHF","amountMicros":"12500000"}}}`
}

func TestProductAttributesAreAll145DocumentedMembers(t *testing.T) {
	if len(wantProductAttributes) != 145 {
		t.Fatalf("the fixed list holds %d members, want 145", len(wantProductAttributes))
	}
	got := nProductAttributes.names()
	slices.Sort(got)
	if !slices.Equal(got, wantProductAttributes) {
		t.Errorf("members = %v, want %v", got, wantProductAttributes)
	}
	for _, name := range []string{"base64EncodedProduct"} {
		if slices.Contains(got, name) {
			t.Errorf("%s must not be an output member", name)
		}
	}
}

func TestProductsGetReturnsEveryAttributeAndEveryOtherMember(t *testing.T) {
	h := newHarness(t)
	body, _ := json.Marshal(map[string]any{
		"name": "accounts/1000001/products/en~US~sku1", "offerId": "sku1", "contentLanguage": "en", "feedLabel": "US",
		"dataSource": "accounts/1000001/dataSources/5", "versionNumber": "3", "legacyLocal": true, "archived": true,
		"base64EncodedName":    "accounts/1000001/products/ZW5-VVN-c2t1MQ",
		"base64EncodedProduct": "never held",
		"automatedDiscounts":   sample(nAutomatedDiscounts),
		"customAttributes":     []any{map[string]any{"name": "size type", "value": "regular", "groupValues": []any{map[string]any{"name": "a", "value": "b"}}}},
		"productStatus":        sample(nProductStatus),
		"productAttributes":    sample(nProductAttributes),
		"undocumented":         "never held",
	})
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, string(body), nil }
	out, err := h.call(t, handleProductsGet, `{"account":"1000001","product":"en~US~sku1"}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"name", "offerId", "contentLanguage", "feedLabel", "dataSource", "versionNumber",
		"legacyLocal", "archived", "base64EncodedName", "automatedDiscounts", "customAttributes", "productStatus",
		"productAttributes"} {
		if out[name] == nil {
			t.Errorf("member %s is missing", name)
		}
	}
	if out["base64EncodedProduct"] != nil || out["undocumented"] != nil {
		t.Errorf("an undocumented member was held: %v", out)
	}
	attributes := out["productAttributes"].(map[string]any)
	if len(attributes) != 145 {
		t.Errorf("productAttributes holds %d members, want 145", len(attributes))
	}
	for _, name := range wantProductAttributes {
		if attributes[name] == nil {
			t.Errorf("attribute %s is missing", name)
		}
	}
	status := out["productStatus"].(map[string]any)
	for _, name := range []string{"creationDate", "lastUpdateDate", "googleExpirationDate", "itemLevelIssues", "destinationStatuses"} {
		if status[name] == nil {
			t.Errorf("status member %s is missing", name)
		}
	}
	group := out["customAttributes"].([]any)[0].(map[string]any)
	if group["groupValues"] == nil {
		t.Errorf("custom attribute = %v", group)
	}
}

func TestProductsGetSendsOnePathBuiltFromAccountAndProduct(t *testing.T) {
	// ZW5-VVN-c2t1LzEyMw is the encoding of en~US~sku/123 the documentation gives.
	for _, c := range []struct{ product, path string }{
		{"en~US~sku-123", "/products/v1/accounts/1000001/products/en~US~sku-123"},
		{"local~en~US~sku.1_x", "/products/v1/accounts/1000001/products/local~en~US~sku.1_x"},
		{"ZW5-VVN-c2t1LzEyMw", "/products/v1/accounts/1000001/products/ZW5-VVN-c2t1LzEyMw"},
	} {
		h := newHarness(t)
		h.google.api = func(*http.Request) (int, string, http.Header) { return 200, productRow("sku"), nil }
		out, err := h.call(t, handleProductsGet, `{"account":"1000001","product":"`+c.product+`"}`)
		if err != nil || out["offerId"] != "sku" {
			t.Fatalf("%s: %v, %v", c.product, out, err)
		}
		if paths := h.paths(t); !slices.Equal(paths, []string{c.path}) {
			t.Errorf("%s: paths = %v, want %s", c.product, paths, c.path)
		}
		if req := h.google.seen()[0]; req.URL.EscapedPath() != c.path || req.URL.RawQuery != "" {
			t.Errorf("%s: sent %s?%s", c.product, req.URL.EscapedPath(), req.URL.RawQuery)
		}
	}
}

func TestProductsGetRefusesAMalformedProductBeforeSecretAndRequest(t *testing.T) {
	for _, product := range []string{
		"", "abc", "../x", "en~US~../x", "en~US~a/b", "en~US~a?b", "en~US~a#b", "en~US~a%2Fb", "en~US~a:render",
		"en~US~a b", "en~US~", "en~~sku", "e~US~sku", "eng~US~sku", "en~US~sku~x", "en~U S~sku", "en~" + strings.Repeat("A", 21) + "~sku",
		"local~local~en~US~sku", "ZW5-VVN-c2t1LzEyMw==", "ZW5-VVN-c2t1LzEyMw/", "ZW5+VVN+c2t1LzEyMw", "ZW5", "!!!!",
		"bm90IGEgcHJvZHVjdA", // base64url of "not a product"
		"ZW4~VVN",
		"en~US~" + strings.Repeat("a", 201),
	} {
		h := newHarness(t)
		arguments, _ := json.Marshal(map[string]string{"account": "1000001", "product": product})
		if _, err := h.call(t, handleProductsGet, string(arguments)); !isInvalidRequest(err) {
			t.Errorf("product %q gave %v", product, err)
		}
		h.untouched(t)
	}
	h := newHarness(t)
	if _, err := h.call(t, handleProductsGet, `{"account":"1000001"}`); !isInvalidRequest(err) {
		t.Errorf("a missing product gave %v", err)
	}
	h.untouched(t)
}

func TestProductToolsCheckTheAccountAgainstTheTargetsBeforeSecretAndRequest(t *testing.T) {
	for _, c := range []struct {
		handler capability.Handler
		args    string
	}{
		{handleProductsList, `{"account":"2"}`},
		{handleProductsGet, `{"account":"2","product":"en~US~sku"}`},
		{handleAggregateProductStatusesList, `{"account":"2"}`},
		{handleAccountIssuesList, `{"account":"2"}`},
		{handleProductsList, `{"account":"1/../2"}`},
		{handleProductsList, `{"account":"01"}`},
		{handleProductsList, `{}`},
	} {
		h := newHarness(t, "accounts/1", "accounts/3")
		if _, err := h.call(t, c.handler, c.args); !isInvalidRequest(err) {
			t.Errorf("%s gave %v", c.args, err)
		}
		h.untouched(t)
	}
}

func TestProductToolsProveASubAccountOfABoundAdvancedAccount(t *testing.T) {
	h := newHarness(t, "advanced-accounts/10")
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		switch req.URL.Path {
		case "/accounts/v1/accounts/10:listSubaccounts":
			return 200, `{"accounts":[` + accountRow("11", "Sub") + `]}`, nil
		case "/products/v1/accounts/11/products":
			return 200, `{"products":[` + productRow("sku") + `]}`, nil
		}
		t.Errorf("unexpected request %s", req.URL)
		return 404, `{}`, nil
	}
	if _, err := h.call(t, handleProductsList, `{"account":"11"}`); err != nil {
		t.Fatal(err)
	}
	want := []string{"/accounts/v1/accounts/10:listSubaccounts?pageSize=500", "/products/v1/accounts/11/products"}
	if paths := h.paths(t); !slices.Equal(paths, want) {
		t.Errorf("paths = %v, want %v", paths, want)
	}
	if _, err := h.call(t, handleProductsList, `{"account":"12"}`); !isInvalidRequest(err) {
		t.Errorf("an account outside the advanced account gave %v", err)
	}
}

func TestProductToolsUseTheOnlyTargetAndReadANamedAccountWithoutProof(t *testing.T) {
	h := newHarness(t, "accounts/7")
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, `{"products":[]}`, nil }
	for _, args := range []string{`{}`, `{"account":"7"}`} {
		out, err := h.call(t, handleProductsList, args)
		if err != nil || out["products"] == nil {
			t.Fatalf("%s: %v, %v", args, out, err)
		}
	}
	for _, path := range h.paths(t) {
		if path != "/products/v1/accounts/7/products" {
			t.Errorf("path = %s", path)
		}
	}
}

func TestProductsListReadsPagesWithACursorAndBoundsTheTexts(t *testing.T) {
	h := newHarness(t)
	long := strings.Repeat("t", 2000)
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		if req.URL.Query().Get("pageToken") == "page-two" {
			return 200, `{"products":[` + productRow("sku2") + `]}`, nil
		}
		return 200, `{"products":[{"name":"accounts/1000001/products/en~US~sku1","offerId":"sku1",` +
			`"productAttributes":{"title":"` + long + `","description":"` + long + `","link":"https://shop.example.invalid/` +
			strings.Repeat("p", 3000) + `","imageLink":"https://img.example.invalid/a.png","adsRedirect":"https://ads.example.invalid/r",` +
			`"gtins":["1","2"],"adult":false,"mpn":7,"price":{"currencyCode":"CHF","amountMicros":9},"unknownAttribute":"x"},` +
			`"productStatus":{"itemLevelIssues":[{"code":"c","documentation":"https://doc.example.invalid/","severity":"DISAPPROVED"}]}},` +
			`"not an object",{}],"nextPageToken":"page-two"}`, nil
	}
	first, err := h.call(t, handleProductsList, `{"account":"1000001","page_size":2}`)
	if err != nil {
		t.Fatal(err)
	}
	rows := first["products"].([]any)
	if len(rows) != 1 {
		t.Fatalf("products = %v", rows)
	}
	attributes := rows[0].(map[string]any)["productAttributes"].(map[string]any)
	if got := len([]rune(attributes["title"].(string))); got != maxNameRunes || !strings.HasSuffix(attributes["title"].(string), "…") {
		t.Errorf("title has %d runes", got)
	}
	if got := len([]rune(attributes["description"].(string))); got != maxTextRunes {
		t.Errorf("description has %d runes", got)
	}
	if got := len([]rune(attributes["link"].(string))); got != maxURLRunes {
		t.Errorf("link has %d runes", got)
	}
	if attributes["unknownAttribute"] != nil || attributes["mpn"] != nil || attributes["adult"] != false ||
		attributes["price"].(map[string]any)["amountMicros"] != "9" {
		t.Errorf("attributes = %v", attributes)
	}
	cursor, _ := first["next_cursor"].(string)
	if cursor == "" || cursor == "page-two" {
		t.Fatalf("next_cursor = %q, want a wrapped cursor", cursor)
	}
	second, err := h.call(t, handleProductsList, `{"account":"1000001","page_size":2,"cursor":"`+cursor+`"}`)
	if err != nil || second["next_cursor"] != nil || len(second["products"].([]any)) != 1 {
		t.Fatalf("second page = %v, %v", second, err)
	}
	paths := h.paths(t)
	if len(paths) != 2 || paths[0] != "/products/v1/accounts/1000001/products?pageSize=2" ||
		paths[1] != "/products/v1/accounts/1000001/products?pageSize=2&pageToken=page-two" {
		t.Errorf("paths = %v", paths)
	}
	// Links are text: nothing but the API was ever requested.
	for _, req := range h.google.seen() {
		if req.URL.Host != "merchantapi.googleapis.com" {
			t.Errorf("request to %s", req.URL.Host)
		}
	}
	for _, args := range []string{`{"account":"1000001","cursor":"` + cursor + `"}`, `{"account":"1000001","page_size":2,"cursor":"x"}`,
		`{"account":"1000002","page_size":2,"cursor":"` + cursor + `"}`} {
		if _, err := h.call(t, handleProductsList, args); !isInvalidRequest(err) {
			t.Errorf("%s gave %v", args, err)
		}
	}
	if len(h.google.seen()) != 2 {
		t.Errorf("a refused cursor reached Google: %v", h.paths(t))
	}
}

func TestProductsListRefusesAPageAboveTheBoundAndMoreEntriesThanTheBound(t *testing.T) {
	h := newHarness(t)
	if _, err := h.call(t, handleProductsList, `{"account":"1","page_size":101}`); !isInvalidRequest(err) {
		t.Errorf("page_size 101 gave %v", err)
	}
	h.untouched(t)
	rows := make([]string, maxEntries+1)
	for i := range rows {
		rows[i] = `{}`
	}
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"products":[` + strings.Join(rows, ",") + `]}`, nil
	}
	if _, err := h.call(t, handleProductsList, `{"account":"1"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("err = %v", err)
	}
}

func TestProductsGetListsAreBoundedAndDepthIsEnded(t *testing.T) {
	h := newHarness(t)
	items := make([]string, maxListItems+20)
	for i := range items {
		items[i] = `"x"`
	}
	nested := `{"name":"a","value":"v"}`
	for range maxDepth + 2 {
		nested = `{"name":"g","groupValues":[` + nested + `]}`
	}
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"productAttributes":{"gtins":[` + strings.Join(items, ",") + `]},"customAttributes":[` + nested + `]}`, nil
	}
	out, err := h.call(t, handleProductsGet, `{"account":"1","product":"en~US~sku"}`)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(out["productAttributes"].(map[string]any)["gtins"].([]any)); got != maxListItems {
		t.Errorf("gtins holds %d entries, want %d", got, maxListItems)
	}
	depth := 0
	for value := any(out["customAttributes"].([]any)[0]); value != nil; depth++ {
		groups, _ := value.(map[string]any)["groupValues"].([]any)
		if len(groups) == 0 {
			break
		}
		value = groups[0]
	}
	if depth >= maxDepth {
		t.Errorf("nesting reached depth %d, want fewer than %d", depth, maxDepth)
	}
}

func TestProductToolsMapGoogleFailuresAndNeverRepeat(t *testing.T) {
	for _, c := range []struct {
		status int
		body   string
		class  provider.Class
	}{
		{403, googleErrorBody, provider.ClassPermission},
		{404, `{}`, provider.ClassNotFound},
		{429, quotaBody(reasonDayQuota), provider.ClassRateLimited},
	} {
		h := newHarness(t)
		h.google.api = func(*http.Request) (int, string, http.Header) { return c.status, c.body, nil }
		if _, err := h.call(t, handleProductsGet, `{"account":"1","product":"en~US~sku"}`); classOf(err) != c.class {
			t.Errorf("status %d gave %v", c.status, err)
		}
		if got := len(h.google.seen()); got != 1 {
			t.Errorf("status %d: %d requests, want one", c.status, got)
		}
	}
}

func TestProductDescriptorsNameTheDelayAndTheUntrustedContent(t *testing.T) {
	for _, d := range []string{productsList.Description, productsGet.Description} {
		for _, want := range []string{"several minutes", "untrusted", "never fetched", "145", "base64EncodedProduct"} {
			if !strings.Contains(d, want) {
				t.Errorf("description lacks %q", want)
			}
		}
	}
	for _, d := range []string{productsList.Description, productsGet.Description, aggregateProductStatusesList.Description,
		accountIssuesList.Description} {
		if !strings.Contains(d, "several minutes") {
			t.Errorf("description names no delay: %s", d)
		}
	}
	if !strings.Contains(aggregateProductStatusesList.Description, "standalone account or a sub-account") {
		t.Error("the aggregate statuses do not name the accounts they work for")
	}
}

func TestProductOutputSchemasCarryEveryMember(t *testing.T) {
	var schema struct {
		Properties map[string]struct {
			Properties map[string]json.RawMessage `json:"properties"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(productsGet.OutputSchema, &schema); err != nil {
		t.Fatal(err)
	}
	if got := len(schema.Properties["productAttributes"].Properties); got != 145 {
		t.Errorf("the schema holds %d attributes, want 145", got)
	}
	for _, name := range []string{"name", "offerId", "productStatus", "customAttributes", "automatedDiscounts"} {
		if _, ok := schema.Properties[name]; !ok {
			t.Errorf("the schema lacks %s", name)
		}
	}
	for _, d := range descriptors() {
		if !json.Valid(d.OutputSchema) || !json.Valid(d.InputSchema) {
			t.Errorf("%s has an invalid schema", d.ID)
		}
	}
}
