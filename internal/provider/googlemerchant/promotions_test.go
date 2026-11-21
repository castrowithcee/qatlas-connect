package googlemerchant

import (
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// fixturePromotions holds every documented member of a promotion, one member the document does not list, and
// a long title and an address that the result has to cut or keep as text.
var fixturePromotions = `{"promotions":[{"name":"accounts/1000001/promotions/en~US~spring-sale",` +
	`"promotionId":"spring-sale","contentLanguage":"en","targetCountry":"US","redemptionChannel":["ONLINE","IN_STORE"],` +
	`"dataSource":"accounts/1000001/dataSources/2000002","versionNumber":"7",` +
	`"customAttributes":[{"name":"size type","value":"regular"},{"name":"group","groupValues":[{"name":"a","value":"b"}]}],` +
	`"attributes":{"productApplicability":"SPECIFIC_PRODUCTS","offerType":"GENERIC_CODE","genericRedemptionCode":"SAVE10",` +
	`"longTitle":"` + strings.Repeat("T", 400) + `","couponValueType":"PERCENT_OFF","percentOff":"10",` +
	`"promotionDestinations":["FREE_LISTINGS","SHOPPING_ADS"],"itemIdInclusion":["sku-1","sku-2"],` +
	`"brandInclusion":["brand-a"],"itemGroupIdInclusion":["g1"],"productTypeInclusion":["t1"],` +
	`"itemIdExclusion":["sku-9"],"brandExclusion":["brand-b"],"itemGroupIdExclusion":["g2"],"productTypeExclusion":["t2"],` +
	`"minimumPurchaseAmount":{"amountMicros":"20000000","currencyCode":"USD"},"minimumPurchaseQuantity":"2",` +
	`"limitQuantity":"3","limitValue":{"amountMicros":"1","currencyCode":"USD"},` +
	`"moneyOffAmount":{"amountMicros":"2","currencyCode":"USD"},"getThisQuantityDiscounted":"1",` +
	`"freeGiftValue":{"amountMicros":"3","currencyCode":"USD"},"freeGiftDescription":"a gift","freeGiftItemId":"gift-1",` +
	`"promotionEffectiveTimePeriod":{"startTime":"2030-03-01T00:00:00Z","endTime":"2030-03-31T00:00:00Z"},` +
	`"promotionDisplayTimePeriod":{"startTime":"2030-02-20T00:00:00Z","endTime":"2030-04-01T00:00:00Z"},` +
	`"storeApplicability":"SPECIFIC_STORES","storeCodesInclusion":["s1"],"storeCodesExclusion":["s2"],` +
	`"promotionUrl":"https://shop.example.invalid/spring","redemptionRestriction":"CUSTOM",` +
	`"customRedemptionRestriction":"members only","maxDiscountAmount":{"amountMicros":"4","currencyCode":"USD"},` +
	`"googleProductCategories":["Apparel"],"audience":"LOCATION","regionIdInclusion":["north-1"],` +
	`"minMoneyOffAmount":{"amountMicros":"5","currencyCode":"USD"},"maxMoneyOffAmount":{"amountMicros":"6","currencyCode":"USD"},` +
	`"minPercentOff":"5","maxPercentOff":"15","unknownField":"never held"},` +
	`"promotionStatus":{"destinationStatuses":[{"reportingContext":"FREE_LISTINGS","status":"LIVE"}],` +
	`"itemLevelIssues":[{"code":"c1","severity":"DEMOTED","resolution":"merchant_action","attribute":"longTitle",` +
	`"reportingContext":"FREE_LISTINGS","description":"short","detail":"long","documentation":"https://help.example.invalid/x",` +
	`"applicableCountries":["US"]}],"creationDate":"2030-01-01T00:00:00Z","lastUpdateDate":"2030-01-02T00:00:00Z"}}],` +
	`"nextPageToken":"page-two"}`

const validAttributes = `{"productApplicability":"ALL_PRODUCTS","offerType":"NO_CODE","longTitle":"Ten percent off",` +
	`"couponValueType":"PERCENT_OFF","percentOff":10,"promotionDestinations":["FREE_LISTINGS"],` +
	`"promotionEffectiveTimePeriod":{"startTime":"2030-03-01T00:00:00Z","endTime":"2030-03-31T00:00:00Z"}}`

func insertArgs(attributes string, extra string) string {
	return `{"account":"1000001","data_source":"2000002","promotion_id":"spring-sale","content_language":"en",` +
		`"target_country":"US","redemption_channel":["ONLINE"],"attributes":` + attributes + extra + `}`
}

func TestPromotionToolsAreDeclaredWithTheirRisk(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	want := map[string]struct {
		effect      capability.Effect
		idempotency capability.Idempotency
		confirm     capability.Confirmation
		toolsOnly   bool
	}{
		promotionsListID:   {capability.EffectRead, capability.IdempotencySafe, capability.ConfirmationNone, false},
		promotionsGetID:    {capability.EffectRead, capability.IdempotencySafe, capability.ConfirmationNone, false},
		promotionsInsertID: {capability.EffectCreate, capability.IdempotencyIdempotent, capability.ConfirmationRequired, true},
	}
	for id, w := range want {
		d, _, ok := reg.Lookup(id)
		if !ok || d.Version != 1 || d.Risk.Effect != w.effect || d.Risk.Idempotency != w.idempotency ||
			d.Risk.Confirmation != w.confirm || !d.Risk.OpenWorld || d.RequiresToolAllowList != w.toolsOnly {
			t.Errorf("%s = %+v, %v", id, d.Risk, ok)
		}
		if scopes := metadata.Token.Scopes[id]; len(scopes) != 1 || scopes[0] != contentScope {
			t.Errorf("%s scopes = %v", id, scopes)
		}
		if inRead := slices.Contains(metadata.Profiles[0].Tools, id); inRead != !w.toolsOnly {
			t.Errorf("%s in read profile: %t", id, inRead)
		}
	}
	d, _, _ := reg.Lookup(promotionsInsertID)
	for _, phrase := range []string{"REPLACES", "several minutes", "delete", "no dry run", "https"} {
		if !strings.Contains(strings.ToLower(d.Description), strings.ToLower(phrase)) {
			t.Errorf("insert description misses %q: %s", phrase, d.Description)
		}
	}
	if strings.Contains(strings.ToLower(string(d.InputSchema)), "delete") {
		t.Error("the insert schema mentions a delete")
	}
	for _, id := range []string{promotionsListID, promotionsGetID} {
		if d, _, _ := reg.Lookup(id); !strings.Contains(d.Description, "several minutes") {
			t.Errorf("%s misses the delay note", id)
		}
	}
}

func TestPromotionsListShowsEveryDocumentedMemberAndCutsTheRest(t *testing.T) {
	h := newHarness(t)
	h.answerWith(200, fixturePromotions)
	out, err := h.call(t, handlePromotionsList, `{"account":"1000001"}`)
	if err != nil {
		t.Fatal(err)
	}
	got := h.recorded(t)
	if len(got) != 1 || got[0].method != http.MethodGet || got[0].path != "/promotions/v1/accounts/1000001/promotions" ||
		got[0].query != "pageSize=100" {
		t.Errorf("requests = %+v", got)
	}
	encoded, _ := json.Marshal(out)
	text := string(encoded)
	for _, shown := range []string{"en~US~spring-sale", "spring-sale", "IN_STORE", "dataSources/2000002", `"versionNumber":"7"`,
		"size type", "groupValues", "SPECIFIC_PRODUCTS", "GENERIC_CODE", "SAVE10", "percentOff", "FREE_LISTINGS", "sku-2",
		"brand-a", "g1", "t1", "sku-9", "brand-b", "g2", "t2", "20000000", "minimumPurchaseQuantity", "limitQuantity",
		"limitValue", "moneyOffAmount", "getThisQuantityDiscounted", "freeGiftValue", "a gift", "gift-1",
		"promotionEffectiveTimePeriod", "promotionDisplayTimePeriod", "SPECIFIC_STORES", "s1", "s2",
		"https://shop.example.invalid/spring", "CUSTOM", "members only", "maxDiscountAmount", "Apparel", "LOCATION",
		"north-1", "minMoneyOffAmount", "maxMoneyOffAmount", "minPercentOff", "maxPercentOff", "destinationStatuses",
		"LIVE", "itemLevelIssues", "DEMOTED", "merchant_action", "longTitle", "https://help.example.invalid/x",
		"creationDate", "lastUpdateDate", `"next_cursor"`} {
		if !strings.Contains(text, shown) {
			t.Errorf("result misses %q: %s", shown, text)
		}
	}
	if strings.Contains(text, "never held") || strings.Contains(text, strings.Repeat("T", 400)) ||
		!strings.Contains(text, strings.Repeat("T", 299)+"…") {
		t.Errorf("an unknown member is held or the title is not cut: %s", text)
	}
	if memberCount(promotionAttributesView{}) != 41 || memberCount(promotionAttributes{}) != 41 {
		t.Errorf("attribute members: view %d, input %d, want 41", memberCount(promotionAttributesView{}),
			memberCount(promotionAttributes{}))
	}
}

func memberCount(v any) int { return reflect.TypeOf(v).NumField() }

func TestPromotionsListCapsListsAndPages(t *testing.T) {
	h := newHarness(t, "accounts/1000001")
	var items []string
	for i := 0; i < 400; i++ {
		items = append(items, `"id-`+strings.Repeat("x", 150)+`"`)
	}
	h.answerWith(200, `{"promotions":[{"promotionId":"p","attributes":{"itemIdInclusion":[`+strings.Join(items, ",")+`]}}],`+
		`"nextPageToken":"page-two"}`)
	out, err := h.call(t, handlePromotionsList, `{"page_size":250}`)
	if err != nil {
		t.Fatal(err)
	}
	rows := out["promotions"].([]any)
	list := rows[0].(map[string]any)["attributes"].(map[string]any)["itemIdInclusion"].([]any)
	if len(list) != maxPromotionEntries || len([]rune(list[0].(string))) != maxIDRunes {
		t.Errorf("list has %d entries of %d runes", len(list), len([]rune(list[0].(string))))
	}
	cursor := out["next_cursor"].(string)
	h.answerWith(200, `{}`)
	second, err := h.call(t, handlePromotionsList, `{"page_size":250,"cursor":"`+cursor+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	if rows, ok := second["promotions"].([]any); !ok || len(rows) != 0 || second["next_cursor"] != nil {
		t.Errorf("second = %v", second)
	}
	got := h.recorded(t)
	if len(got) != 2 || got[0].query != "pageSize=250" || got[1].query != "pageSize=250&pageToken=page-two" {
		t.Errorf("requests = %+v", got)
	}
	for _, args := range []string{`{"cursor":"` + cursor + `"}`, `{"page_size":250,"cursor":"foreign"}`} {
		if _, err := h.call(t, handlePromotionsList, args); !isInvalidRequest(err) {
			t.Errorf("%s gave %v, want a refused cursor", args, err)
		}
	}
	for _, args := range []string{`{"page_size":251}`, `{"page_size":-1}`} {
		if _, err := h.call(t, handlePromotionsList, args); !isInvalidRequest(err) {
			t.Errorf("%s gave %v", args, err)
		}
	}
	if len(h.google.seen()) != 2 {
		t.Errorf("a refused call reached Google: %d requests", len(h.google.seen()))
	}
}

func TestPromotionsGetReadsOneAndRefusesBadNamesBeforeAnyRequest(t *testing.T) {
	h := newHarness(t)
	h.answerWith(200, `{"name":"accounts/1000001/promotions/en~US~spring-sale","promotionId":"spring-sale"}`)
	out, err := h.call(t, handlePromotionsGet, `{"account":"1000001","promotion":"en~US~spring-sale"}`)
	if err != nil || out["promotionId"] != "spring-sale" {
		t.Fatalf("out = %v, err = %v", out, err)
	}
	if got := h.recorded(t); len(got) != 1 || got[0].path != "/promotions/v1/accounts/1000001/promotions/en~US~spring-sale" {
		t.Errorf("requests = %+v", got)
	}
	for name, args := range map[string]string{
		"no promotion": `{"account":"1000001"}`, "slash": `{"account":"1000001","promotion":"a/../b"}`,
		"query": `{"account":"1000001","promotion":"a?x=1"}`, "percent": `{"account":"1000001","promotion":"a%2fb"}`,
		"leading dash": `{"account":"1000001","promotion":"-a"}`, "too long": `{"account":"1000001","promotion":"` + strings.Repeat("a", 201) + `"}`,
		"bad account": `{"account":"1/2","promotion":"a"}`, "unknown member": `{"account":"1000001","promotion":"a","x":1}`,
	} {
		fresh := newHarness(t)
		if _, err := fresh.call(t, handlePromotionsGet, args); !isInvalidRequest(err) {
			t.Errorf("%s gave %v", name, err)
		}
		fresh.untouched(t)
	}
}

func TestPromotionToolsCheckTheAccountBeforeSecretAndIO(t *testing.T) {
	for name, c := range map[string]struct {
		handler capability.Handler
		args    string
	}{
		"list":   {handlePromotionsList, `{"account":"2000002"}`},
		"get":    {handlePromotionsGet, `{"account":"2000002","promotion":"a"}`},
		"insert": {handlePromotionsInsert, strings.Replace(insertArgs(validAttributes, ""), `"1000001"`, `"2000002"`, 1)},
	} {
		h := newHarness(t, "accounts/1000001")
		if _, err := h.call(t, c.handler, c.args); !isInvalidRequest(err) || strings.Contains(err.Error(), "2000002") {
			t.Errorf("%s: err = %v", name, err)
		}
		h.untouched(t)
	}
}

func TestPromotionsInsertBuildsOneCheckedRequestAndReportsAcceptance(t *testing.T) {
	h := newHarness(t)
	h.answerWith(200, `{"name":"accounts/1000001/promotions/en~US~spring-sale","promotionId":"spring-sale"}`)
	attributes := strings.TrimSuffix(validAttributes, "}") + `,"promotionUrl":"https://shop.example.invalid/spring",` +
		`"minimumPurchaseAmount":{"amountMicros":20000000,"currencyCode":"USD"}}`
	out, err := h.call(t, handlePromotionsInsert, insertArgs(attributes, `,"version_number":3,`+
		`"custom_attributes":[{"name":"size type","value":"regular"}]`))
	if err != nil {
		t.Fatal(err)
	}
	if out["accepted"] != true || out["promotion"].(map[string]any)["promotionId"] != "spring-sale" {
		t.Errorf("out = %v", out)
	}
	got := h.recorded(t)
	if len(got) != 1 || got[0].method != http.MethodPost || got[0].path != "/promotions/v1/accounts/1000001/promotions:insert" ||
		got[0].query != "" || got[0].contentType != "application/json" {
		t.Fatalf("requests = %+v", got)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(got[0].body), &body); err != nil {
		t.Fatal(err)
	}
	if body["dataSource"] != "accounts/1000001/dataSources/2000002" {
		t.Errorf("dataSource = %v", body["dataSource"])
	}
	promo := body["promotion"].(map[string]any)
	if promo["promotionId"] != "spring-sale" || promo["versionNumber"] != "3" || promo["contentLanguage"] != "en" ||
		promo["targetCountry"] != "US" || len(promo["redemptionChannel"].([]any)) != 1 {
		t.Errorf("promotion = %v", promo)
	}
	attrs := promo["attributes"].(map[string]any)
	if attrs["percentOff"] != "10" || attrs["promotionUrl"] != "https://shop.example.invalid/spring" ||
		attrs["minimumPurchaseAmount"].(map[string]any)["amountMicros"] != "20000000" {
		t.Errorf("attributes = %v", attrs)
	}
	if len(body) != 2 || len(promo["customAttributes"].([]any)) != 1 {
		t.Errorf("body = %s", got[0].body)
	}
}

func TestPromotionsInsertRefusesBadArgumentsBeforeSecretOrIO(t *testing.T) {
	without := func(member string) string {
		var tree map[string]any
		_ = json.Unmarshal([]byte(validAttributes), &tree)
		delete(tree, member)
		encoded, _ := json.Marshal(tree)
		return string(encoded)
	}
	with := func(member, value string) string {
		return strings.TrimSuffix(validAttributes, "}") + `,"` + member + `":` + value + `}`
	}
	cases := map[string]string{
		"missing offerType":           insertArgs(without("offerType"), ""),
		"missing longTitle":           insertArgs(without("longTitle"), ""),
		"missing couponValueType":     insertArgs(without("couponValueType"), ""),
		"missing destinations":        insertArgs(without("promotionDestinations"), ""),
		"missing effective period":    insertArgs(without("promotionEffectiveTimePeriod"), ""),
		"neither applicability":       insertArgs(without("productApplicability"), ""),
		"both applicabilities":        insertArgs(with("eventApplicability", `"SITEWIDE"`), ""),
		"unspecified enum":            insertArgs(with("audience", `"AUDIENCE_UNSPECIFIED"`), ""),
		"unknown destination":         insertArgs(strings.Replace(validAttributes, "FREE_LISTINGS", "NOWHERE", 1), ""),
		"http address":                insertArgs(with("promotionUrl", `"http://shop.example.invalid/x"`), ""),
		"user info":                   insertArgs(with("promotionUrl", `"https://user:pw`+"@"+`shop.example.invalid/x"`), ""),
		"backslash host":              insertArgs(with("promotionUrl", `"https://shop.example.invalid\\@evil.example.invalid/"`), ""),
		"relative address":            insertArgs(with("promotionUrl", `"/x"`), ""),
		"javascript address":          insertArgs(with("promotionUrl", `"javascript:alert(1)"`), ""),
		"custom without text":         insertArgs(with("redemptionRestriction", `"CUSTOM"`), ""),
		"location without regions":    insertArgs(with("audience", `"LOCATION"`), ""),
		"all stores with codes":       insertArgs(with("storeApplicability", `"ALL_STORES"`)[:len(with("storeApplicability", `"ALL_STORES"`))-1]+`,"storeCodesInclusion":["s1"]}`, ""),
		"six categories":              insertArgs(with("googleProductCategories", `["a","b","c","d","e","f"]`), ""),
		"money range without bound":   insertArgs(strings.Replace(validAttributes, "PERCENT_OFF", "MONEY_OFF_RANGE", 1), ""),
		"percent range without bound": insertArgs(strings.Replace(validAttributes, `"PERCENT_OFF"`, `"PERCENT_OFF_RANGE"`, 1), ""),
		"negative amount":             insertArgs(with("limitValue", `{"amountMicros":-1,"currencyCode":"USD"}`), ""),
		"bad currency":                insertArgs(with("limitValue", `{"amountMicros":1,"currencyCode":"usd"}`), ""),
		"fractional amount":           insertArgs(with("limitValue", `{"amountMicros":1.5,"currencyCode":"USD"}`), ""),
		"bad time":                    insertArgs(strings.Replace(validAttributes, "2030-03-01T00:00:00Z", "tomorrow", 1), ""),
		"end before start":            insertArgs(strings.Replace(validAttributes, "2030-03-31T00:00:00Z", "2030-02-01T00:00:00Z", 1), ""),
		"control character title":     insertArgs(strings.Replace(validAttributes, "Ten percent off", `Ten\u0007off`, 1), ""),
		"unknown attribute":           insertArgs(with("surprise", `1`), ""),
		"bad language":                strings.Replace(insertArgs(validAttributes, ""), `"en"`, `"EN"`, 1),
		"bad country":                 strings.Replace(insertArgs(validAttributes, ""), `"US"`, `"usa"`, 1),
		"bad channel":                 strings.Replace(insertArgs(validAttributes, ""), `["ONLINE"]`, `["MAIL"]`, 1),
		"duplicate channel":           strings.Replace(insertArgs(validAttributes, ""), `["ONLINE"]`, `["ONLINE","ONLINE"]`, 1),
		"bad data source":             strings.Replace(insertArgs(validAttributes, ""), `"2000002"`, `"1/../2"`, 1),
		"data source path":            strings.Replace(insertArgs(validAttributes, ""), `"2000002"`, `"accounts/9/dataSources/1"`, 1),
		"no data source":              strings.Replace(insertArgs(validAttributes, ""), `"data_source":"2000002",`, ``, 1),
		"file source member":          insertArgs(validAttributes, `,"fileInput":{}`),
		"address in custom name":      insertArgs(validAttributes, `,"custom_attributes":[{"name":"promotion_url","value":"x"}]`),
		"address in custom value":     insertArgs(validAttributes, `,"custom_attributes":[{"name":"info","value":"https://evil.example.invalid"}]`),
		"address in group":            insertArgs(validAttributes, `,"custom_attributes":[{"name":"g","groupValues":[{"name":"homepage_link","value":"x"}]}]`),
		"value and group":             insertArgs(validAttributes, `,"custom_attributes":[{"name":"g","value":"v","groupValues":[{"name":"a","value":"b"}]}]`),
		"custom attribute empty":      insertArgs(validAttributes, `,"custom_attributes":[{"name":"g"}]`),
	}
	for name, args := range cases {
		h := newHarness(t)
		if _, err := h.call(t, handlePromotionsInsert, args); !isInvalidRequest(err) {
			t.Errorf("%s: err = %v", name, err)
		}
		h.untouched(t)
	}
}

func TestPromotionsInsertChecksAddressesWithTheSharedHTTPSCheck(t *testing.T) {
	h := newHarness(t)
	h.answerWith(200, `{}`)
	attributes := strings.TrimSuffix(validAttributes, "}") + `,"promotionUrl":"https://shop.example.invalid/a?b=1#c"}`
	if _, err := h.call(t, handlePromotionsInsert, insertArgs(attributes, "")); err != nil {
		t.Fatal(err)
	}
}

func TestPromotionChangesAreSentOnceAndSayWhenTheOutcomeIsOpen(t *testing.T) {
	for name, c := range map[string]struct {
		status int
		class  provider.Class
		open   bool
	}{
		"500": {500, provider.ClassProviderError, true}, "503": {503, provider.ClassUnreachable, true},
		"504": {504, provider.ClassTimeout, true}, "429 minute": {429, provider.ClassRateLimited, false},
		"400": {400, provider.ClassProviderError, false}, "401": {401, provider.ClassAuth, false},
		"403": {403, provider.ClassPermission, false}, "409": {409, provider.ClassProviderError, false},
		"404": {404, provider.ClassNotFound, false}, "200 not an object": {200, provider.ClassInvalidResponse, true},
	} {
		h := newHarness(t)
		body := `{"error":{"code":1,"status":"X","message":"google text","details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"QUOTA_REQUEST_RATE_TOO_HIGH"}]}}`
		if c.status == 200 {
			body = `[1]`
		}
		h.answerWith(c.status, body)
		_, err := h.call(t, handlePromotionsInsert, insertArgs(validAttributes, ""))
		if got := classOf(err); got != c.class {
			t.Errorf("%s: class = %q, want %q (%v)", name, got, c.class, err)
		}
		if n := len(h.google.seen()); n != 1 {
			t.Errorf("%s: %d requests, a change must go out once", name, n)
		}
		if open := err != nil && strings.Contains(err.Error(), "outcome of the change is unknown"); open != c.open {
			t.Errorf("%s: open outcome = %t, want %t (%v)", name, open, c.open, err)
		}
		if err != nil && strings.Contains(err.Error(), "google text") {
			t.Errorf("%s: message carries Google's text: %v", name, err)
		}
	}
	h := newHarness(t)
	h.google.fail = io.ErrUnexpectedEOF
	_, err := h.call(t, handlePromotionsInsert, insertArgs(validAttributes, ""))
	if err == nil || !strings.Contains(err.Error(), "outcome of the change is unknown") || len(h.google.seen()) != 1 {
		t.Errorf("err = %v, requests = %d", err, len(h.google.seen()))
	}
}

func TestPromotionsInsertProvesASubAccountOfABoundAdvancedAccount(t *testing.T) {
	h := newHarness(t, "advanced-accounts/10")
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		if strings.Contains(req.URL.Path, "listSubaccounts") {
			return 200, `{"accounts":[` + accountRow("1000001", "Sub") + `]}`, nil
		}
		return 200, `{}`, nil
	}
	if _, err := h.call(t, handlePromotionsInsert, insertArgs(validAttributes, "")); err != nil {
		t.Fatal(err)
	}
	paths := h.google.seen()
	if len(paths) != 2 || !strings.Contains(paths[0].URL.Path, "listSubaccounts") ||
		!strings.HasSuffix(paths[1].URL.Path, "promotions:insert") {
		t.Errorf("requests = %v", paths)
	}
	h = newHarness(t, "advanced-accounts/10")
	h.google.api = func(req *http.Request) (int, string, http.Header) { return 200, `{"accounts":[]}`, nil }
	if _, err := h.call(t, handlePromotionsInsert, insertArgs(validAttributes, "")); err == nil {
		t.Error("an account outside the advanced account was changed")
	}
	for _, r := range h.google.seen() {
		if strings.HasSuffix(r.URL.Path, "promotions:insert") {
			t.Error("the insert reached Google")
		}
	}
}

func TestPromotionConfirmationSummaryNamesWhatChanges(t *testing.T) {
	got := promotionSummary(json.RawMessage(insertArgs(validAttributes, "")))
	for _, w := range []string{"Insert promotion", `"spring-sale"`, "US", "en", "1000001", "2000002", "Ten percent off",
		"REPLACED", "publicly", "cannot be deleted", "afterwards"} {
		if !strings.Contains(got, w) {
			t.Errorf("summary %q misses %q", got, w)
		}
	}
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	if d, op, _ := reg.Lookup(promotionsInsertID); d.Risk.Confirmation != capability.ConfirmationRequired || op == nil {
		t.Errorf("insert needs confirmation")
	}
}
