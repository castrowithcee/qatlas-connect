package googlemerchant

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const (
	invProduct = "en~US~sku-1"
	invBase    = "/inventories/v1/accounts/1000001/products/" + invProduct + "/localInventories"
)

func localEntry(store, extra string) string {
	return `{"name":"accounts/1000001/products/` + invProduct + `/localInventories/` + store + `","account":"1000001",` +
		`"storeCode":"` + store + `"` + extra + `}`
}

// storeFake keeps local inventories by store code the way the API documents it: an insert replaces the whole
// entry of its store code, and nothing else is touched.
type storeFake struct {
	mu      sync.Mutex
	entries map[string]map[string]any
	order   []string
}

func (s *storeFake) route(r regionWire) (int, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case r.method == http.MethodPost && strings.HasSuffix(r.path, "/localInventories:insert"):
		var in map[string]any
		_ = json.Unmarshal([]byte(r.body), &in)
		code := in["storeCode"].(string)
		if _, ok := s.entries[code]; !ok {
			s.order = append(s.order, code)
		}
		in["name"] = "accounts/1000001/products/" + invProduct + "/localInventories/" + code
		s.entries[code] = in
		out, _ := json.Marshal(in)
		return 200, string(out)
	case r.method == http.MethodDelete:
		code := r.path[strings.LastIndex(r.path, "/")+1:]
		delete(s.entries, code)
		s.order = slices.DeleteFunc(s.order, func(c string) bool { return c == code })
		return 200, `{}`
	}
	rows := []string{}
	for _, code := range s.order {
		out, _ := json.Marshal(s.entries[code])
		rows = append(rows, string(out))
	}
	return 200, `{"localInventories":[` + strings.Join(rows, ",") + `]}`
}

func TestLocalInventoryInsertReplacesTheWholeEntryOfAStoreCode(t *testing.T) {
	h := newHarness(t)
	fake := &storeFake{entries: map[string]map[string]any{}}
	seen := h.regionAnswers(t, fake.route)
	full := `{"account":"1000001","product":"` + invProduct + `","store_code":"store-1","availability":"IN_STOCK",` +
		`"quantity":5,"price":{"amountMicros":"12500000","currencyCode":"CHF"},"pickup_method":"BUY","pickup_sla":"SAME_DAY",` +
		`"instore_product_location":"aisle 4","local_shipping_label":"van"}`
	if _, err := h.call(t, handleLocalInventoriesInsert, full); err != nil {
		t.Fatal(err)
	}
	other := `{"account":"1000001","product":"` + invProduct + `","store_code":"store-2","quantity":9}`
	if _, err := h.call(t, handleLocalInventoriesInsert, other); err != nil {
		t.Fatal(err)
	}
	// The second insert for store-1 names only the availability: everything else must be gone afterwards.
	second := `{"account":"1000001","product":"` + invProduct + `","store_code":"store-1","availability":"OUT_OF_STOCK"}`
	out, err := h.call(t, handleLocalInventoriesInsert, second)
	if err != nil {
		t.Fatal(err)
	}
	wire := (*seen)[2]
	wantBody := `{"localInventoryAttributes":{"availability":"OUT_OF_STOCK"},"storeCode":"store-1"}`
	if wire.method != http.MethodPost || wire.path != invBase+":insert" || wire.query != "" ||
		wire.contentType != "application/json" || wire.body != wantBody {
		t.Errorf("replacing insert = %+v\nwant body %s", wire, wantBody)
	}
	if out["accepted"] != true {
		t.Errorf("result = %v", out)
	}
	list, err := h.call(t, handleLocalInventoriesList, `{"account":"1000001","product":"`+invProduct+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	rows := list["local_inventories"].([]any)
	if len(rows) != 2 {
		t.Fatalf("list = %v", list)
	}
	one := rows[0].(map[string]any)
	attrs := one["localInventoryAttributes"].(map[string]any)
	if one["storeCode"] != "store-1" || len(attrs) != 1 || attrs["availability"] != "OUT_OF_STOCK" {
		t.Errorf("store-1 after the replacement = %v", one)
	}
	untouched := rows[1].(map[string]any)["localInventoryAttributes"].(map[string]any)
	if rows[1].(map[string]any)["storeCode"] != "store-2" || untouched["quantity"] != "9" {
		t.Errorf("store-2 changed: %v", rows[1])
	}
	// An insert with only the store code replaces the entry by an empty one.
	if _, err := h.call(t, handleLocalInventoriesInsert, `{"account":"1000001","product":"`+invProduct+`","store_code":"store-2"}`); err != nil {
		t.Fatal(err)
	}
	if w := (*seen)[4]; w.body != `{"storeCode":"store-2"}` {
		t.Errorf("body = %s", w.body)
	}
	if _, has := fake.entries["store-2"]["localInventoryAttributes"]; has {
		t.Errorf("store-2 kept attributes: %v", fake.entries["store-2"])
	}
}

func TestLocalInventoryInsertSendsEveryDocumentedAttribute(t *testing.T) {
	h := newHarness(t)
	seen := h.regionAnswers(t, always(200, localEntry("s1", `,"localInventoryAttributes":{"salePrice":{"amountMicros":"1","currencyCode":"CHF"},`+
		`"salePriceEffectiveDate":{"startTime":"2026-12-01T00:00:00Z","endTime":"2026-12-24T00:00:00Z"},`+
		`"loyaltyPrograms":[{"programLabel":"club","tierLabel":"gold","loyaltyPoints":"10","shippingLabel":"free",`+
		`"memberPriceEffectiveInterval":{"startTime":"2026-12-01T00:00:00Z","endTime":"2026-12-02T00:00:00Z"}}],`+
		`"customAttributes":[{"name":"size type","value":"regular"},{"name":"g","groupValues":[{"name":"a","value":"b"}]}],"quantity":"3","pickupMethod":"RESERVE",`+
		`"pickupSla":"NEXT_DAY","instoreProductLocation":"A1","localShippingLabel":"van","undocumented":"x"},"undocumented":"x"`)))
	args := map[string]any{"account": "1000001", "product": invProduct, "store_code": "s1",
		"price":                     map[string]any{"amountMicros": "2", "currencyCode": "CHF"},
		"sale_price":                map[string]any{"amountMicros": "1", "currencyCode": "CHF"},
		"sale_price_effective_date": map[string]any{"startTime": "2026-12-01T00:00:00Z", "endTime": "2026-12-24T00:00:00Z"},
		"loyalty_programs": []any{map[string]any{"programLabel": "club", "tierLabel": "gold", "loyaltyPoints": "10", "shippingLabel": "free",
			"price":                        map[string]any{"amountMicros": "3", "currencyCode": "CHF"},
			"memberPriceEffectiveInterval": map[string]any{"startTime": "2026-12-01T00:00:00Z", "endTime": "2026-12-02T00:00:00Z"}}},
		"custom_attributes": []any{map[string]any{"name": "size type", "value": "regular"},
			map[string]any{"name": "g", "group_values": []any{map[string]any{"name": "a", "value": "b"}}}},
		"availability": "ON_DISPLAY_TO_ORDER", "quantity": 3, "pickup_method": "RESERVE", "pickup_sla": "NEXT_DAY",
		"instore_product_location": "A1", "local_shipping_label": "van"}
	raw, _ := json.Marshal(args)
	out, err := h.call(t, handleLocalInventoriesInsert, string(raw))
	if err != nil {
		t.Fatal(err)
	}
	var sent struct {
		StoreCode string
		Attrs     map[string]json.RawMessage `json:"localInventoryAttributes"`
	}
	if json.Unmarshal([]byte((*seen)[0].body), &sent) != nil || sent.StoreCode != "s1" {
		t.Fatalf("body = %s", (*seen)[0].body)
	}
	var names []string
	for name := range sent.Attrs {
		names = append(names, name)
	}
	slices.Sort(names)
	want := []string{"availability", "customAttributes", "instoreProductLocation", "localShippingLabel", "loyaltyPrograms",
		"pickupMethod", "pickupSla", "price", "quantity", "salePrice", "salePriceEffectiveDate"}
	if !slices.Equal(names, want) || string(sent.Attrs["quantity"]) != `"3"` {
		t.Errorf("attributes sent = %v (quantity %s)", names, sent.Attrs["quantity"])
	}
	body := (*seen)[0].body
	for _, part := range []string{`"memberPriceEffectiveInterval":{"endTime":"2026-12-02T00:00:00Z","startTime":"2026-12-01T00:00:00Z"}`,
		`"shippingLabel":"free"`, `{"groupValues":[{"name":"a","value":"b"}],"name":"g"}`, `{"name":"size type","value":"regular"}`} {
		if !strings.Contains(body, part) {
			t.Errorf("body lacks %s: %s", part, body)
		}
	}
	if strings.Contains(body, "memberPriceEffectiveDate") || strings.Contains(body, "group_values") {
		t.Errorf("body holds a name of the product API or of the tool: %s", body)
	}
	entry := out["local_inventory"].(map[string]any)
	got := entry["localInventoryAttributes"].(map[string]any)
	if entry["undocumented"] != nil || got["undocumented"] != nil || entry["storeCode"] != "s1" || got["pickupSla"] != "NEXT_DAY" ||
		got["salePriceEffectiveDate"] == nil || got["loyaltyPrograms"] == nil || got["customAttributes"] == nil ||
		got["loyaltyPrograms"].([]any)[0].(map[string]any)["memberPriceEffectiveInterval"] == nil ||
		got["loyaltyPrograms"].([]any)[0].(map[string]any)["shippingLabel"] != "free" ||
		got["customAttributes"].([]any)[1].(map[string]any)["groupValues"] == nil {
		t.Errorf("result = %v", out)
	}
}

func TestLocalInventoryListReadsEveryMemberAndPages(t *testing.T) {
	h := newHarness(t)
	var queries []string
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		queries = append(queries, req.URL.Path+"?"+req.URL.RawQuery)
		if req.URL.Query().Get("pageToken") == "" {
			return 200, `{"localInventories":[` + localEntry("s1", `,"base64EncodedName":"abc","localInventoryAttributes":{`+
				`"price":{"amountMicros":"5","currencyCode":"CHF"},"availability":"IN_STOCK","quantity":"4",`+
				`"loyaltyPrograms":[{"programLabel":"club","price":{"amountMicros":"4","currencyCode":"CHF"}}]},"unknown":1`) +
				`,{"nothing":true}],"nextPageToken":"next-token"}`, nil
		}
		return 200, `{"localInventories":[]}`, nil
	}
	out, err := h.call(t, handleLocalInventoriesList, `{"account":"1000001","product":"`+invProduct+`","page_size":50}`)
	if err != nil {
		t.Fatal(err)
	}
	rows := out["local_inventories"].([]any)
	if len(rows) != 1 || rows[0].(map[string]any)["unknown"] != nil || rows[0].(map[string]any)["base64EncodedName"] != "abc" ||
		rows[0].(map[string]any)["account"] != "1000001" {
		t.Fatalf("rows = %v", out)
	}
	cursor, _ := out["next_cursor"].(string)
	if cursor == "" {
		t.Fatalf("cursor = %q", cursor)
	}
	if _, err := h.call(t, handleLocalInventoriesList, `{"account":"1000001","product":"`+invProduct+`","page_size":50,"cursor":"`+cursor+`"}`); err != nil {
		t.Fatal(err)
	}
	if queries[0] != invBase+"?pageSize=50" || queries[1] != invBase+"?pageSize=50&pageToken=next-token" {
		t.Errorf("queries = %v", queries)
	}
	// A cursor of another page size is refused.
	if _, err := h.call(t, handleLocalInventoriesList, `{"account":"1000001","product":"`+invProduct+`","page_size":51,"cursor":"`+cursor+`"}`); !isInvalidRequest(err) {
		t.Errorf("foreign cursor = %v", err)
	}
	// Without a page size the tool sends its own, never Google's 25000.
	if _, err := h.call(t, handleLocalInventoriesList, `{"account":"1000001","product":"`+invProduct+`"}`); err != nil {
		t.Fatal(err)
	}
	if last := queries[len(queries)-1]; last != invBase+"?pageSize=100" {
		t.Errorf("default query = %s", last)
	}
}

func TestLocalInventoryDeleteIsOneRequestForOneStore(t *testing.T) {
	h := newHarness(t)
	seen := h.regionAnswers(t, always(200, `{}`))
	out, err := h.call(t, handleLocalInventoriesDelete, `{"account":"1000001","product":"`+invProduct+`","store_code":"store.1_a-b"}`)
	if err != nil {
		t.Fatal(err)
	}
	w := (*seen)[0]
	if len(*seen) != 1 || w.method != http.MethodDelete || w.path != invBase+"/store.1_a-b" || w.body != "" {
		t.Errorf("requests = %+v", *seen)
	}
	if out["accepted"] != true || out["name"] != "accounts/1000001/products/"+invProduct+"/localInventories/store.1_a-b" {
		t.Errorf("result = %v", out)
	}
}

func TestLocalInventoryArgumentsAreRefusedBeforeSecretOrIO(t *testing.T) {
	encoded := base64.RawURLEncoding.EncodeToString([]byte("en~US~sku/1"))
	if _, err := productSegment(encoded); err != nil {
		t.Fatalf("encoded product refused: %v", err)
	}
	base := func(extra string) string {
		return `{"account":"1000001","product":"` + invProduct + `","store_code":"s1"` + extra + `}`
	}
	insertCases := map[string]string{
		"store slash":        strings.Replace(base(""), `"s1"`, `"a/b"`, 1),
		"store dotdot":       strings.Replace(base(""), `"s1"`, `".."`, 1),
		"store dot start":    strings.Replace(base(""), `"s1"`, `".a"`, 1),
		"store query":        strings.Replace(base(""), `"s1"`, `"a?x=1"`, 1),
		"store hash":         strings.Replace(base(""), `"s1"`, `"a#b"`, 1),
		"store percent":      strings.Replace(base(""), `"s1"`, `"a%2Fb"`, 1),
		"store empty":        strings.Replace(base(""), `"s1"`, `""`, 1),
		"store long":         strings.Replace(base(""), `"s1"`, `"`+strings.Repeat("a", 65)+`"`, 1),
		"product slash":      strings.Replace(base(""), invProduct, "en~US~a/b", 1),
		"product dotdot":     strings.Replace(base(""), invProduct, "..", 1),
		"product query":      strings.Replace(base(""), invProduct, "en~US~a?b", 1),
		"availability":       base(`,"availability":"UNKNOWN"`),
		"availability unspc": base(`,"availability":"LOCAL_INVENTORY_AVAILABILITY_UNSPECIFIED"`),
		"quantity negative":  base(`,"quantity":-1`),
		"pickup alone":       base(`,"pickup_method":"BUY"`),
		"sla alone":          base(`,"pickup_sla":"SAME_DAY"`),
		"bad sla":            base(`,"pickup_method":"BUY","pickup_sla":"LATER"`),
		"location 21 bytes":  base(`,"instore_product_location":"` + strings.Repeat("a", 21) + `"`),
		"location bytes":     base(`,"instore_product_location":"` + strings.Repeat("ä", 11) + `"`),
		"price micros":       base(`,"price":{"amountMicros":"1.5","currencyCode":"CHF"}`),
		"price currency":     base(`,"price":{"amountMicros":"1","currencyCode":"chf"}`),
		"date without sale":  base(`,"sale_price_effective_date":{"startTime":"2026-12-01T00:00:00Z","endTime":"2026-12-02T00:00:00Z"}`),
		"date reversed": base(`,"sale_price":{"amountMicros":"1","currencyCode":"CHF"},` +
			`"sale_price_effective_date":{"startTime":"2026-12-03T00:00:00Z","endTime":"2026-12-02T00:00:00Z"}`),
		"date empty":         base(`,"sale_price":{"amountMicros":"1","currencyCode":"CHF"},"sale_price_effective_date":{}`),
		"date end text":      base(`,"sale_price":{"amountMicros":"1","currencyCode":"CHF"},"sale_price_effective_date":{"startTime":"2026-12-03T00:00:00Z","endTime":"y"}`),
		"date text":          base(`,"sale_price":{"amountMicros":"1","currencyCode":"CHF"},"sale_price_effective_date":{"startTime":"x","endTime":"y"}`),
		"custom empty":       base(`,"custom_attributes":[{"name":"","value":"v"}]`),
		"custom size":        base(`,"custom_attributes":[{"name":"` + strings.Repeat("n", 100) + `","value":"` + strings.Repeat("v", 10200) + `"}]`),
		"custom neither":     base(`,"custom_attributes":[{"name":"n"}]`),
		"custom both":        base(`,"custom_attributes":[{"name":"n","value":"v","group_values":[{"name":"a","value":"b"}]}]`),
		"custom group empty": base(`,"custom_attributes":[{"name":"n","group_values":[]}]`),
		"custom nested":      base(`,"custom_attributes":[{"name":"n","group_values":[{"name":"a","value":"b","group_values":[{"name":"c","value":"d"}]}]}]`),
		"custom sub empty":   base(`,"custom_attributes":[{"name":"n","group_values":[{"name":"a"}]}]`),
		"custom group size": base(`,"custom_attributes":[{"name":"n","group_values":[{"name":"a","value":"` + strings.Repeat("v", 6000) +
			`"},{"name":"b","value":"` + strings.Repeat("v", 6000) + `"}]}]`),
		"custom count": base(`,"custom_attributes":[` + strings.Repeat(`{"name":"n","group_values":[{"name":"a","value":"b"}]},`, 50) +
			`{"name":"n","value":"v"}]`),
		"loyalty empty":  base(`,"loyalty_programs":[{}]`),
		"loyalty points": base(`,"loyalty_programs":[{"programLabel":"a","loyaltyPoints":"-1"}]`),
	}
	for name, args := range insertCases {
		h := newHarness(t)
		if _, err := h.call(t, handleLocalInventoriesInsert, args); !isInvalidRequest(err) {
			t.Errorf("insert %s = %v, want invalid request", name, err)
		}
		h.untouched(t)
	}
	for name, args := range map[string]string{
		"store slash":  strings.Replace(base(""), `"s1"`, `"a/b"`, 1),
		"store dotdot": strings.Replace(base(""), `"s1"`, `".."`, 1),
		"product":      strings.Replace(base(""), invProduct, "x", 1),
	} {
		h := newHarness(t)
		if _, err := h.call(t, handleLocalInventoriesDelete, args); !isInvalidRequest(err) {
			t.Errorf("delete %s = %v, want invalid request", name, err)
		}
		h.untouched(t)
	}
	for name, args := range map[string]string{
		"product":   `{"account":"1000001","product":"a/b"}`,
		"page size": `{"account":"1000001","product":"` + invProduct + `","page_size":251}`,
		"cursor":    `{"account":"1000001","product":"` + invProduct + `","cursor":"nonsense"}`,
	} {
		h := newHarness(t)
		if _, err := h.call(t, handleLocalInventoriesList, args); !isInvalidRequest(err) {
			t.Errorf("list %s = %v, want invalid request", name, err)
		}
		h.untouched(t)
	}
}

func TestLocalInventoryAcceptsTheDocumentedBoundaries(t *testing.T) {
	h := newHarness(t)
	seen := h.regionAnswers(t, always(200, `{}`))
	for _, extra := range []string{
		`,"pickup_method":"NOT_SUPPORTED"`, `,"pickup_method":"NOT_SUPPORTED","pickup_sla":"MULTI_WEEK"`,
		`,"quantity":0`,
		`,"sale_price":{"amountMicros":"1","currencyCode":"CHF"},"sale_price_effective_date":{"startTime":"2026-12-01T00:00:00Z"}`,
		`,"sale_price":{"amountMicros":"1","currencyCode":"CHF"},"sale_price_effective_date":{"endTime":"2026-12-01T00:00:00Z"}`,
		`,"sale_price":{"amountMicros":"1","currencyCode":"CHF"},"sale_price_effective_date":{"startTime":"2026-12-01T00:00:00Z","endTime":"2026-12-01T00:00:00Z"}`, `,"custom_attributes":[{"name":"n","group_values":[{"name":"a","value":"b"},{"name":"c","value":"d"}]}]`,
		`,"loyalty_programs":[{"shippingLabel":"free"}]`, `,"instore_product_location":"` + strings.Repeat("a", 20) + `"`,
		`,"custom_attributes":[{"name":"n","value":"` + strings.Repeat("v", 10239) + `"}]`,
	} {
		args := `{"account":"1000001","product":"` + base64.RawURLEncoding.EncodeToString([]byte("en~US~sku/1")) +
			`","store_code":"` + strings.Repeat("a", 64) + `"` + extra + `}`
		if _, err := h.call(t, handleLocalInventoriesInsert, args); err != nil {
			t.Errorf("%s refused: %v", extra, err)
		}
	}
	for i, want := range []string{`"salePriceEffectiveDate":{"startTime":"2026-12-01T00:00:00Z"}`,
		`"salePriceEffectiveDate":{"endTime":"2026-12-01T00:00:00Z"}`} {
		if body := (*seen)[3+i].body; !strings.Contains(body, want) {
			t.Errorf("open interval body = %s, want %s", body, want)
		}
	}
	if len(*seen) != 10 {
		t.Errorf("%d requests", len(*seen))
	}
}

func TestLocalInventoryAccountIsCheckedAgainstTheTargetsBeforeSecretOrIO(t *testing.T) {
	h := newHarness(t, "accounts/1000001")
	for name, handler := range map[string]capability.Handler{"list": handleLocalInventoriesList,
		"insert": handleLocalInventoriesInsert, "delete": handleLocalInventoriesDelete} {
		_, err := h.call(t, handler, `{"account":"2000002","product":"`+invProduct+`","store_code":"s1"}`)
		if !isInvalidRequest(err) || strings.Contains(err.Error(), "2000002") {
			t.Errorf("%s foreign account = %v", name, err)
		}
		if _, err := h.call(t, handler, `{"account":"01","product":"`+invProduct+`","store_code":"s1"}`); !isInvalidRequest(err) {
			t.Errorf("%s malformed account = %v", name, err)
		}
	}
	h.untouched(t)
}

func TestLocalInventoryOfASubAccountIsProvenFirst(t *testing.T) {
	route := func(subs string) func(regionWire) (int, string) {
		return func(r regionWire) (int, string) {
			if strings.HasSuffix(r.path, ":listSubaccounts") {
				return 200, `{"accounts":[` + subs + `]}`
			}
			return 200, `{}`
		}
	}
	for name, handler := range map[string]capability.Handler{"list": handleLocalInventoriesList,
		"insert": handleLocalInventoriesInsert, "delete": handleLocalInventoriesDelete} {
		h := newHarness(t, "advanced-accounts/10")
		seen := h.regionAnswers(t, route(accountRow("20", "x")))
		if _, err := h.call(t, handler, `{"account":"20","product":"`+invProduct+`","store_code":"s1"}`); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(*seen) != 2 || !strings.HasSuffix((*seen)[0].path, ":listSubaccounts") || !strings.Contains((*seen)[1].path, "/accounts/20/") {
			t.Errorf("%s requests = %+v", name, *seen)
		}
		h = newHarness(t, "advanced-accounts/10")
		seen = h.regionAnswers(t, route(accountRow("30", "x")))
		if _, err := h.call(t, handler, `{"account":"20","product":"`+invProduct+`","store_code":"s1"}`); !isInvalidRequest(err) {
			t.Errorf("%s foreign sub-account = %v", name, err)
		}
		if len(*seen) != 1 {
			t.Errorf("%s: %d requests for a refused account, want only the proof", name, len(*seen))
		}
	}
}

func TestLocalInventoryChangesAreSentOnceAndMapGoogleErrors(t *testing.T) {
	insert := `{"account":"1000001","product":"` + invProduct + `","store_code":"s1","quantity":1}`
	del := `{"account":"1000001","product":"` + invProduct + `","store_code":"s1"}`
	const unknown = "outcome of the change is unknown"
	for name, c := range map[string]struct {
		status  int
		body    string
		class   provider.Class
		unknown bool
	}{
		"server error": {500, `{"error":{"status":"INTERNAL"}}`, provider.ClassProviderError, true},
		"unavailable":  {503, `{"error":{"status":"UNAVAILABLE"}}`, provider.ClassUnreachable, true},
		"not json":     {200, `<html>`, provider.ClassInvalidResponse, true},
		"minute quota": {429, quotaBody(reasonMinuteQuota), provider.ClassRateLimited, false},
		"permission":   {403, googleErrorBody, provider.ClassPermission, false},
		"missing":      {404, googleErrorBody, provider.ClassNotFound, false},
		"expired":      {401, `{"error":{"status":"UNAUTHENTICATED"}}`, provider.ClassAuth, false},
	} {
		for tool, call := range map[string]struct {
			handler capability.Handler
			args    string
		}{"insert": {handleLocalInventoriesInsert, insert}, "delete": {handleLocalInventoriesDelete, del}} {
			if tool == "delete" && c.body == `<html>` {
				continue // a delete expects no content
			}
			h := newHarness(t)
			header := "application/json"
			if c.body == `<html>` {
				header = "text/html"
			}
			h.google.api = func(*http.Request) (int, string, http.Header) {
				return c.status, c.body, http.Header{"Content-Type": {header}}
			}
			_, err := h.call(t, call.handler, call.args)
			if err == nil {
				t.Fatalf("%s %s: no error", name, tool)
			}
			if got := len(h.google.seen()); got != 1 {
				t.Errorf("%s %s: %d requests, want one, never a repeat", name, tool, got)
			}
			if c.class != "" && classOf(err) != c.class {
				t.Errorf("%s %s: class = %q, want %q", name, tool, classOf(err), c.class)
			}
			if strings.Contains(err.Error(), unknown) != c.unknown {
				t.Errorf("%s %s: error %q, unknown outcome %t", name, tool, err, c.unknown)
			}
			if strings.Contains(err.Error(), "synthetic provider echo") {
				t.Errorf("%s %s: the message carries Google's text", name, tool)
			}
		}
	}
}

func TestLocalInventoryListMapsGoogleErrors(t *testing.T) {
	for status, class := range map[int]provider.Class{401: provider.ClassAuth, 403: provider.ClassPermission,
		404: provider.ClassNotFound, 429: provider.ClassRateLimited} {
		h := newHarness(t)
		h.regionAnswers(t, always(status, googleErrorBody))
		if _, err := h.call(t, handleLocalInventoriesList, `{"account":"1","product":"`+invProduct+`"}`); classOf(err) != class {
			t.Errorf("status %d = class %q, want %q", status, classOf(err), class)
		}
	}
}

func TestLocalInventoryToolsAreDeclaredAndOfferedAsDocumented(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	d, _, ok := reg.Lookup(localInventoriesListID)
	if !ok || d.Version != 1 || d.RequiresToolAllowList || d.Risk.Effect != capability.EffectRead ||
		d.Risk.Confirmation != capability.ConfirmationNone || !slices.Contains(metadata.Profiles[0].Tools, localInventoriesListID) {
		t.Errorf("list = %+v", d)
	}
	for id, w := range map[string]struct {
		effect capability.Effect
		words  []string
	}{
		localInventoriesInsertID: {capability.EffectCreate, []string{"REPLACES the whole local inventory of that store",
			"every attribute you leave out is removed", "up to 30 minutes", "never repeats", "only that Google accepted",
			"Other stores of the product are not touched"}},
		localInventoriesDeleteID: {capability.EffectDelete, []string{"up to an hour", "never repeats", "permanent",
			"only that Google accepted"}},
	} {
		d, _, ok := reg.Lookup(id)
		if !ok || d.Version != 1 || !d.RequiresToolAllowList || d.Risk.Effect != w.effect || !d.Risk.OpenWorld ||
			d.Risk.Confirmation != capability.ConfirmationRequired || reg.ConfirmationSummary(id) == nil {
			t.Fatalf("%s = %+v, %v", id, d, ok)
		}
		if slices.Contains(metadata.Profiles[0].Tools, id) {
			t.Errorf("the read profile holds %s", id)
		}
		var listed bool
		for _, tool := range metadata.Tools {
			if tool.ID == id {
				listed = tool.RequiresToolAllowList && tool.Effect == config.Effect(w.effect)
			}
		}
		if !listed {
			t.Errorf("%s is not a listed-only tool", id)
		}
		for _, word := range w.words {
			if !strings.Contains(d.Description, word) {
				t.Errorf("%s description lacks %q", id, word)
			}
		}
		if scopes := metadata.Token.Scopes[id]; len(scopes) != 1 || scopes[0] != contentScope {
			t.Errorf("%s scopes = %v", id, scopes)
		}
		schema := string(d.InputSchema)
		if !strings.Contains(schema, `"additionalProperties":false`) || strings.Contains(schema, `"parent"`) ||
			strings.Contains(schema, `"body"`) || strings.Contains(schema, "updateMask") {
			t.Errorf("%s input schema is open: %s", id, schema)
		}
	}
	insert, _, _ := reg.Lookup(localInventoriesInsertID)
	for _, name := range []string{"store_code", "price", "sale_price", "sale_price_effective_date", "loyalty_programs",
		"custom_attributes", "availability", "quantity", "pickup_method", "pickup_sla", "instore_product_location",
		"local_shipping_label"} {
		if !strings.Contains(string(insert.InputSchema), `"`+name+`"`) {
			t.Errorf("insert schema lacks %s", name)
		}
	}
}

func TestLocalInventoryOutputSchemaCoversEveryDocumentedMember(t *testing.T) {
	got := nLocalInventory.names()
	slices.Sort(got)
	if want := []string{"account", "base64EncodedName", "localInventoryAttributes", "name", "storeCode"}; !slices.Equal(got, want) {
		t.Errorf("members = %v", got)
	}
	got = nLocalInventoryAttributes.names()
	slices.Sort(got)
	want := []string{"availability", "customAttributes", "instoreProductLocation", "localShippingLabel", "loyaltyPrograms",
		"pickupMethod", "pickupSla", "price", "quantity", "salePrice", "salePriceEffectiveDate"}
	if !slices.Equal(got, want) {
		t.Errorf("attributes = %v", got)
	}
}

func TestLocalInventoryConfirmationNamesTheReplacementAndTheDelay(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	got := reg.ConfirmationSummary(localInventoriesInsertID)(json.RawMessage(
		`{"account":"1000001","product":"en~US~sku-1","store_code":"store-1","quantity":2,"availability":"IN_STOCK"}`))
	for _, part := range []string{"1000001", `"store-1"`, `"en~US~sku-1"`, "availability, quantity",
		"replaces the whole local inventory of that store", "every attribute not given here is removed", "30 minutes", "never repeats"} {
		if !strings.Contains(got, part) {
			t.Errorf("insert summary %q misses %q", got, part)
		}
	}
	got = reg.ConfirmationSummary(localInventoriesDeleteID)(json.RawMessage(
		`{"account":"1000001","product":"en~US~sku-1","store_code":"store-1"}`))
	for _, part := range []string{"1000001", `"store-1"`, "an hour", "no way to undo"} {
		if !strings.Contains(got, part) {
			t.Errorf("delete summary %q misses %q", got, part)
		}
	}
}
