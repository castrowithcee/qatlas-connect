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

const regBase = "/inventories/v1/accounts/1000001/products/" + invProduct + "/regionalInventories"

func regionalEntry(region, extra string) string {
	return `{"name":"accounts/1000001/products/` + invProduct + `/regionalInventories/` + region + `","account":"1000001",` +
		`"region":"` + region + `"` + extra + `}`
}

// regionFake keeps regional inventories by region the way the API documents it: an insert replaces the whole
// entry of its region, and nothing else is touched.
type regionFake struct {
	mu      sync.Mutex
	entries map[string]map[string]any
	order   []string
}

func (s *regionFake) route(r regionWire) (int, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case r.method == http.MethodPost && strings.HasSuffix(r.path, "/regionalInventories:insert"):
		var in map[string]any
		_ = json.Unmarshal([]byte(r.body), &in)
		region := in["region"].(string)
		if _, ok := s.entries[region]; !ok {
			s.order = append(s.order, region)
		}
		in["name"] = "accounts/1000001/products/" + invProduct + "/regionalInventories/" + region
		s.entries[region] = in
		out, _ := json.Marshal(in)
		return 200, string(out)
	case r.method == http.MethodDelete:
		region := r.path[strings.LastIndex(r.path, "/")+1:]
		delete(s.entries, region)
		s.order = slices.DeleteFunc(s.order, func(c string) bool { return c == region })
		return 200, `{}`
	}
	rows := []string{}
	for _, region := range s.order {
		out, _ := json.Marshal(s.entries[region])
		rows = append(rows, string(out))
	}
	return 200, `{"regionalInventories":[` + strings.Join(rows, ",") + `]}`
}

func TestRegionalInventoryInsertReplacesTheWholeEntryOfARegion(t *testing.T) {
	h := newHarness(t)
	fake := &regionFake{entries: map[string]map[string]any{}}
	seen := h.regionAnswers(t, fake.route)
	pre := `{"account":"1000001","product":"` + invProduct + `",`
	full := pre + `"region":"west","availability":"IN_STOCK","price":{"amountMicros":"12500000","currencyCode":"USD"},` +
		`"sale_price":{"amountMicros":"10000000","currencyCode":"USD"}}`
	if _, err := h.call(t, handleRegionalInventoriesInsert, full); err != nil {
		t.Fatal(err)
	}
	if _, err := h.call(t, handleRegionalInventoriesInsert, pre+`"region":"east","availability":"IN_STOCK"}`); err != nil {
		t.Fatal(err)
	}
	// The second insert for west names only the availability: everything else must be gone afterwards.
	out, err := h.call(t, handleRegionalInventoriesInsert, pre+`"region":"west","availability":"OUT_OF_STOCK"}`)
	if err != nil {
		t.Fatal(err)
	}
	wire := (*seen)[2]
	wantBody := `{"region":"west","regionalInventoryAttributes":{"availability":"OUT_OF_STOCK"}}`
	if wire.method != http.MethodPost || wire.path != regBase+":insert" || wire.query != "" ||
		wire.contentType != "application/json" || wire.body != wantBody {
		t.Errorf("replacing insert = %+v\nwant body %s", wire, wantBody)
	}
	if out["accepted"] != true {
		t.Errorf("result = %v", out)
	}
	list, err := h.call(t, handleRegionalInventoriesList, `{"account":"1000001","product":"`+invProduct+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	rows := list["regional_inventories"].([]any)
	if len(rows) != 2 {
		t.Fatalf("list = %v", list)
	}
	one := rows[0].(map[string]any)
	attrs := one["regionalInventoryAttributes"].(map[string]any)
	if one["region"] != "west" || len(attrs) != 1 || attrs["availability"] != "OUT_OF_STOCK" {
		t.Errorf("west after the replacement = %v", one)
	}
	untouched := rows[1].(map[string]any)["regionalInventoryAttributes"].(map[string]any)
	if rows[1].(map[string]any)["region"] != "east" || untouched["availability"] != "IN_STOCK" {
		t.Errorf("east changed: %v", rows[1])
	}
	// An insert with only the region replaces the entry by an empty one.
	if _, err := h.call(t, handleRegionalInventoriesInsert, pre+`"region":"east"}`); err != nil {
		t.Fatal(err)
	}
	if w := (*seen)[4]; w.body != `{"region":"east"}` {
		t.Errorf("body = %s", w.body)
	}
	if _, has := fake.entries["east"]["regionalInventoryAttributes"]; has {
		t.Errorf("east kept attributes: %v", fake.entries["east"])
	}
}

func TestRegionalInventoryInsertSendsEveryDocumentedAttribute(t *testing.T) {
	h := newHarness(t)
	seen := h.regionAnswers(t, always(200, regionalEntry("west", `,"regionalInventoryAttributes":{`+
		`"salePrice":{"amountMicros":"1","currencyCode":"USD"},`+
		`"salePriceEffectiveDate":{"startTime":"2026-12-01T00:00:00Z","endTime":"2026-12-24T00:00:00Z"},`+
		`"loyaltyPrograms":[{"programLabel":"club","tierLabel":"gold","loyaltyPoints":"10","shippingLabel":"free",`+
		`"memberPriceEffectiveInterval":{"startTime":"2026-12-01T00:00:00Z","endTime":"2026-12-02T00:00:00Z"}}],`+
		`"availability":"IN_STOCK","undocumented":"x"},"undocumented":"x"`)))
	args := map[string]any{"account": "1000001", "product": invProduct, "region": "west",
		"price":                     map[string]any{"amountMicros": "2", "currencyCode": "USD"},
		"sale_price":                map[string]any{"amountMicros": "1", "currencyCode": "USD"},
		"sale_price_effective_date": map[string]any{"startTime": "2026-12-01T00:00:00Z", "endTime": "2026-12-24T00:00:00Z"},
		"loyalty_programs": []any{map[string]any{"programLabel": "club", "tierLabel": "gold", "loyaltyPoints": "10", "shippingLabel": "free",
			"price":                        map[string]any{"amountMicros": "3", "currencyCode": "USD"},
			"memberPriceEffectiveInterval": map[string]any{"startTime": "2026-12-01T00:00:00Z", "endTime": "2026-12-02T00:00:00Z"}}},
		"availability": "IN_STOCK"}
	raw, _ := json.Marshal(args)
	out, err := h.call(t, handleRegionalInventoriesInsert, string(raw))
	if err != nil {
		t.Fatal(err)
	}
	var sent struct {
		Region string
		Attrs  map[string]json.RawMessage `json:"regionalInventoryAttributes"`
	}
	if json.Unmarshal([]byte((*seen)[0].body), &sent) != nil || sent.Region != "west" {
		t.Fatalf("body = %s", (*seen)[0].body)
	}
	var names []string
	for name := range sent.Attrs {
		names = append(names, name)
	}
	slices.Sort(names)
	if want := []string{"availability", "loyaltyPrograms", "price", "salePrice", "salePriceEffectiveDate"}; !slices.Equal(names, want) {
		t.Errorf("attributes sent = %v", names)
	}
	body := (*seen)[0].body
	if !strings.Contains(body, `"memberPriceEffectiveInterval":{"endTime":"2026-12-02T00:00:00Z","startTime":"2026-12-01T00:00:00Z"}`) ||
		strings.Contains(body, "memberPriceEffectiveDate") {
		t.Errorf("body = %s", body)
	}
	entry := out["regional_inventory"].(map[string]any)
	got := entry["regionalInventoryAttributes"].(map[string]any)
	if entry["undocumented"] != nil || got["undocumented"] != nil || entry["region"] != "west" ||
		got["availability"] != "IN_STOCK" || got["salePriceEffectiveDate"] == nil ||
		got["loyaltyPrograms"].([]any)[0].(map[string]any)["memberPriceEffectiveInterval"] == nil {
		t.Errorf("result = %v", out)
	}
}

func TestRegionalInventoryListReadsEveryMemberAndPages(t *testing.T) {
	h := newHarness(t)
	var queries []string
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		queries = append(queries, req.URL.Path+"?"+req.URL.RawQuery)
		if req.URL.Query().Get("pageToken") == "" {
			return 200, `{"regionalInventories":[` + regionalEntry("west", `,"base64EncodedName":"abc","regionalInventoryAttributes":{`+
				`"price":{"amountMicros":"5","currencyCode":"USD"},"availability":"IN_STOCK"},"unknown":1`) +
				`,{"nothing":true}],"nextPageToken":"next-token"}`, nil
		}
		return 200, `{"regionalInventories":[]}`, nil
	}
	out, err := h.call(t, handleRegionalInventoriesList, `{"account":"1000001","product":"`+invProduct+`","page_size":50}`)
	if err != nil {
		t.Fatal(err)
	}
	rows := out["regional_inventories"].([]any)
	if len(rows) != 1 || rows[0].(map[string]any)["unknown"] != nil || rows[0].(map[string]any)["base64EncodedName"] != "abc" ||
		rows[0].(map[string]any)["account"] != "1000001" {
		t.Fatalf("rows = %v", out)
	}
	cursor, _ := out["next_cursor"].(string)
	if cursor == "" {
		t.Fatalf("cursor = %q", cursor)
	}
	if _, err := h.call(t, handleRegionalInventoriesList, `{"account":"1000001","product":"`+invProduct+`","page_size":50,"cursor":"`+cursor+`"}`); err != nil {
		t.Fatal(err)
	}
	if queries[0] != regBase+"?pageSize=50" || queries[1] != regBase+"?pageSize=50&pageToken=next-token" {
		t.Errorf("queries = %v", queries)
	}
	if _, err := h.call(t, handleRegionalInventoriesList, `{"account":"1000001","product":"`+invProduct+`","page_size":51,"cursor":"`+cursor+`"}`); !isInvalidRequest(err) {
		t.Errorf("foreign cursor = %v", err)
	}
	if _, err := h.call(t, handleRegionalInventoriesList, `{"account":"1000001","product":"`+invProduct+`"}`); err != nil {
		t.Fatal(err)
	}
	if last := queries[len(queries)-1]; last != regBase+"?pageSize=100" {
		t.Errorf("default query = %s", last)
	}
}

func TestRegionalInventoryDeleteIsOneRequestForOneRegion(t *testing.T) {
	h := newHarness(t)
	seen := h.regionAnswers(t, always(200, `{}`))
	out, err := h.call(t, handleRegionalInventoriesDelete, `{"account":"1000001","product":"`+invProduct+`","region":"west_1-a"}`)
	if err != nil {
		t.Fatal(err)
	}
	w := (*seen)[0]
	if len(*seen) != 1 || w.method != http.MethodDelete || w.path != regBase+"/west_1-a" || w.body != "" {
		t.Errorf("requests = %+v", *seen)
	}
	if out["accepted"] != true || out["name"] != "accounts/1000001/products/"+invProduct+"/regionalInventories/west_1-a" {
		t.Errorf("result = %v", out)
	}
}

func TestRegionalInventoryArgumentsAreRefusedBeforeSecretOrIO(t *testing.T) {
	base := func(extra string) string {
		return `{"account":"1000001","product":"` + invProduct + `","region":"west"` + extra + `}`
	}
	sale := `,"sale_price":{"amountMicros":"1","currencyCode":"USD"}`
	insertCases := map[string]string{
		"region slash":       strings.Replace(base(""), `"west"`, `"a/b"`, 1),
		"region dotdot":      strings.Replace(base(""), `"west"`, `".."`, 1),
		"region dot":         strings.Replace(base(""), `"west"`, `"a.b"`, 1),
		"region query":       strings.Replace(base(""), `"west"`, `"a?x=1"`, 1),
		"region hash":        strings.Replace(base(""), `"west"`, `"a#b"`, 1),
		"region percent":     strings.Replace(base(""), `"west"`, `"a%2Fb"`, 1),
		"region empty":       strings.Replace(base(""), `"west"`, `""`, 1),
		"region long":        strings.Replace(base(""), `"west"`, `"`+strings.Repeat("a", 65)+`"`, 1),
		"region missing":     `{"account":"1000001","product":"` + invProduct + `"}`,
		"product slash":      strings.Replace(base(""), invProduct, "en~US~a/b", 1),
		"product dotdot":     strings.Replace(base(""), invProduct, "..", 1),
		"availability":       base(`,"availability":"UNKNOWN"`),
		"availability local": base(`,"availability":"LIMITED_AVAILABILITY"`),
		"availability unspc": base(`,"availability":"REGIONAL_INVENTORY_AVAILABILITY_UNSPECIFIED"`),
		"price micros":       base(`,"price":{"amountMicros":"1.5","currencyCode":"USD"}`),
		"price currency":     base(`,"price":{"amountMicros":"1","currencyCode":"usd"}`),
		"date without sale":  base(`,"sale_price_effective_date":{"startTime":"2026-12-01T00:00:00Z","endTime":"2026-12-02T00:00:00Z"}`),
		"date reversed":      base(sale + `,"sale_price_effective_date":{"startTime":"2026-12-03T00:00:00Z","endTime":"2026-12-02T00:00:00Z"}`),
		"date empty":         base(sale + `,"sale_price_effective_date":{}`),
		"date text":          base(sale + `,"sale_price_effective_date":{"startTime":"x"}`),
		"loyalty empty":      base(`,"loyalty_programs":[{}]`),
		"loyalty points":     base(`,"loyalty_programs":[{"programLabel":"a","loyaltyPoints":"-1"}]`),
		"loyalty count":      base(`,"loyalty_programs":[` + strings.Repeat(`{"programLabel":"a"},`, 20) + `{"programLabel":"a"}]`),
	}
	for name, args := range insertCases {
		h := newHarness(t)
		if _, err := h.call(t, handleRegionalInventoriesInsert, args); !isInvalidRequest(err) {
			t.Errorf("insert %s = %v, want invalid request", name, err)
		}
		h.untouched(t)
	}
	for name, args := range map[string]string{
		"region slash":  strings.Replace(base(""), `"west"`, `"a/b"`, 1),
		"region dotdot": strings.Replace(base(""), `"west"`, `".."`, 1),
		"region dot":    strings.Replace(base(""), `"west"`, `".a"`, 1),
		"product":       strings.Replace(base(""), invProduct, "x", 1),
	} {
		h := newHarness(t)
		if _, err := h.call(t, handleRegionalInventoriesDelete, args); !isInvalidRequest(err) {
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
		if _, err := h.call(t, handleRegionalInventoriesList, args); !isInvalidRequest(err) {
			t.Errorf("list %s = %v, want invalid request", name, err)
		}
		h.untouched(t)
	}
}

func TestRegionalInventoryAcceptsTheDocumentedBoundaries(t *testing.T) {
	h := newHarness(t)
	seen := h.regionAnswers(t, always(200, `{}`))
	sale := `,"sale_price":{"amountMicros":"1","currencyCode":"USD"}`
	for _, extra := range []string{``, `,"availability":"IN_STOCK"`, `,"availability":"OUT_OF_STOCK"`,
		sale + `,"sale_price_effective_date":{"startTime":"2026-12-01T00:00:00Z"}`,
		sale + `,"sale_price_effective_date":{"endTime":"2026-12-01T00:00:00Z"}`,
		sale + `,"sale_price_effective_date":{"startTime":"2026-12-01T00:00:00Z","endTime":"2026-12-01T00:00:00Z"}`,
		`,"loyalty_programs":[{"shippingLabel":"free"}]`,
	} {
		args := `{"account":"1000001","product":"` + base64.RawURLEncoding.EncodeToString([]byte("en~US~sku/1")) +
			`","region":"` + strings.Repeat("a", 64) + `"` + extra + `}`
		if _, err := h.call(t, handleRegionalInventoriesInsert, args); err != nil {
			t.Errorf("%s refused: %v", extra, err)
		}
	}
	if len(*seen) != 7 {
		t.Errorf("%d requests", len(*seen))
	}
}

func TestRegionalInventoryAccountIsCheckedAgainstTheTargetsBeforeSecretOrIO(t *testing.T) {
	h := newHarness(t, "accounts/1000001")
	for name, handler := range map[string]capability.Handler{"list": handleRegionalInventoriesList,
		"insert": handleRegionalInventoriesInsert, "delete": handleRegionalInventoriesDelete} {
		_, err := h.call(t, handler, `{"account":"2000002","product":"`+invProduct+`","region":"west"}`)
		if !isInvalidRequest(err) || strings.Contains(err.Error(), "2000002") {
			t.Errorf("%s foreign account = %v", name, err)
		}
		if _, err := h.call(t, handler, `{"account":"01","product":"`+invProduct+`","region":"west"}`); !isInvalidRequest(err) {
			t.Errorf("%s malformed account = %v", name, err)
		}
	}
	h.untouched(t)
}

func TestRegionalInventoryOfASubAccountIsProvenFirst(t *testing.T) {
	route := func(subs string) func(regionWire) (int, string) {
		return func(r regionWire) (int, string) {
			if strings.HasSuffix(r.path, ":listSubaccounts") {
				return 200, `{"accounts":[` + subs + `]}`
			}
			return 200, `{}`
		}
	}
	for name, handler := range map[string]capability.Handler{"list": handleRegionalInventoriesList,
		"insert": handleRegionalInventoriesInsert, "delete": handleRegionalInventoriesDelete} {
		h := newHarness(t, "advanced-accounts/10")
		seen := h.regionAnswers(t, route(accountRow("20", "x")))
		if _, err := h.call(t, handler, `{"account":"20","product":"`+invProduct+`","region":"west"}`); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(*seen) != 2 || !strings.HasSuffix((*seen)[0].path, ":listSubaccounts") || !strings.Contains((*seen)[1].path, "/accounts/20/") {
			t.Errorf("%s requests = %+v", name, *seen)
		}
		h = newHarness(t, "advanced-accounts/10")
		seen = h.regionAnswers(t, route(accountRow("30", "x")))
		if _, err := h.call(t, handler, `{"account":"20","product":"`+invProduct+`","region":"west"}`); !isInvalidRequest(err) {
			t.Errorf("%s foreign sub-account = %v", name, err)
		}
		if len(*seen) != 1 {
			t.Errorf("%s: %d requests for a refused account, want only the proof", name, len(*seen))
		}
	}
}

func TestRegionalInventoryChangesAreSentOnceAndMapGoogleErrors(t *testing.T) {
	insert := `{"account":"1000001","product":"` + invProduct + `","region":"west","availability":"IN_STOCK"}`
	del := `{"account":"1000001","product":"` + invProduct + `","region":"west"}`
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
		"day quota":    {429, quotaBody(reasonDayQuota), provider.ClassRateLimited, false},
		"permission":   {403, googleErrorBody, provider.ClassPermission, false},
		"missing":      {404, googleErrorBody, provider.ClassNotFound, false},
		"expired":      {401, `{"error":{"status":"UNAUTHENTICATED"}}`, provider.ClassAuth, false},
	} {
		for tool, call := range map[string]struct {
			handler capability.Handler
			args    string
		}{"insert": {handleRegionalInventoriesInsert, insert}, "delete": {handleRegionalInventoriesDelete, del}} {
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

func TestRegionalInventoryListMapsGoogleErrors(t *testing.T) {
	for status, class := range map[int]provider.Class{401: provider.ClassAuth, 403: provider.ClassPermission,
		404: provider.ClassNotFound, 429: provider.ClassRateLimited} {
		h := newHarness(t)
		h.regionAnswers(t, always(status, googleErrorBody))
		if _, err := h.call(t, handleRegionalInventoriesList, `{"account":"1","product":"`+invProduct+`"}`); classOf(err) != class {
			t.Errorf("status %d = class %q, want %q", status, classOf(err), class)
		}
	}
}

func TestRegionalInventoryToolsAreDeclaredAndOfferedAsDocumented(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	d, _, ok := reg.Lookup(regionalInventoriesListID)
	if !ok || d.Version != 1 || d.RequiresToolAllowList || d.Risk.Effect != capability.EffectRead ||
		d.Risk.Confirmation != capability.ConfirmationNone || !slices.Contains(metadata.Profiles[0].Tools, regionalInventoriesListID) {
		t.Errorf("list = %+v", d)
	}
	for id, w := range map[string]struct {
		effect capability.Effect
		words  []string
	}{
		regionalInventoriesInsertID: {capability.EffectCreate, []string{"REPLACES the whole regional inventory of that region",
			"every attribute you leave out is removed", "up to 30 minutes", "never repeats", "only that Google accepted",
			"Other regions of the product are not touched"}},
		regionalInventoriesDeleteID: {capability.EffectDelete, []string{"up to an hour", "never repeats", "permanent",
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
	insert, _, _ := reg.Lookup(regionalInventoriesInsertID)
	for _, name := range []string{"region", "price", "sale_price", "sale_price_effective_date", "loyalty_programs",
		"availability"} {
		if !strings.Contains(string(insert.InputSchema), `"`+name+`"`) {
			t.Errorf("insert schema lacks %s", name)
		}
	}
	if strings.Contains(string(insert.InputSchema), "LIMITED_AVAILABILITY") {
		t.Errorf("insert schema offers a local availability: %s", insert.InputSchema)
	}
}

func TestRegionalInventoryOutputSchemaCoversEveryDocumentedMember(t *testing.T) {
	got := nRegionalInventory.names()
	slices.Sort(got)
	if want := []string{"account", "base64EncodedName", "name", "region", "regionalInventoryAttributes"}; !slices.Equal(got, want) {
		t.Errorf("members = %v", got)
	}
	got = nRegionalInventoryAttributes.names()
	slices.Sort(got)
	if want := []string{"availability", "loyaltyPrograms", "price", "salePrice", "salePriceEffectiveDate"}; !slices.Equal(got, want) {
		t.Errorf("attributes = %v", got)
	}
}

func TestRegionalInventoryConfirmationNamesTheReplacementAndTheDelay(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	got := reg.ConfirmationSummary(regionalInventoriesInsertID)(json.RawMessage(
		`{"account":"1000001","product":"en~US~sku-1","region":"west","price":{"amountMicros":"1","currencyCode":"USD"},"availability":"IN_STOCK"}`))
	for _, part := range []string{"1000001", `"west"`, `"en~US~sku-1"`, "availability, price",
		"replaces the whole regional inventory of that region", "every attribute not given here is removed", "30 minutes", "never repeats"} {
		if !strings.Contains(got, part) {
			t.Errorf("insert summary %q misses %q", got, part)
		}
	}
	got = reg.ConfirmationSummary(regionalInventoriesDeleteID)(json.RawMessage(
		`{"account":"1000001","product":"en~US~sku-1","region":"west"}`))
	for _, part := range []string{"1000001", `"west"`, "an hour", "no way to undo"} {
		if !strings.Contains(got, part) {
			t.Errorf("delete summary %q misses %q", got, part)
		}
	}
}
