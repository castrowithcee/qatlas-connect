package googlecloud

import (
	"net/http"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const catalogService = "services/6F81-5844-456A"

func TestCatalogServicesListFollowsTwoPages(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.hook = func(_ int, req *http.Request) *reply {
		if req.URL.Path != "/v1/services" {
			return nil
		}
		if req.URL.Query().Get("pageToken") == "" {
			return &reply{status: 200, body: `{"services":[{"name":"` + catalogService + `","serviceId":"6F81-5844-456A",` +
				`"displayName":"Compute Engine","businessEntityName":"businessEntities/GCP"}],"nextPageToken":"p2"}`}
		}
		return &reply{status: 200, body: `{"services":[{"name":"services/0000-1111-2222","serviceId":"0000-1111-2222",` +
			`"displayName":"Other"}]}`}
	}
	out, err := e.invoke(catalogServicesListID, `{}`)
	if err != nil {
		t.Fatal(err)
	}
	rows, got := listOf(t, out, "services")
	if len(rows) != 1 || rows[0]["serviceId"] != "6F81-5844-456A" || rows[0]["displayName"] != "Compute Engine" ||
		rows[0]["businessEntityName"] != "businessEntities/GCP" || got["has_more"] != true {
		t.Fatalf("page 1 = %s", out)
	}
	out, err = e.invoke(catalogServicesListID, `{"page_size":7,"cursor":"`+got["next_cursor"].(string)+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	rows, got = listOf(t, out, "services")
	if len(rows) != 1 || rows[0]["displayName"] != "Other" || got["has_more"] != false {
		t.Fatalf("page 2 = %s", out)
	}
	queries := queriesOf(e)
	if queries[0].Get("pageSize") != "100" || queries[1].Get("pageSize") != "7" || queries[1].Get("pageToken") != "p2" {
		t.Errorf("queries = %v", queries)
	}
	if paths := apiPaths(e); paths[0] != "cloudbilling.googleapis.com/v1/services" {
		t.Errorf("path = %v", paths)
	}
}

func TestCatalogToolsIgnoreTheTargets(t *testing.T) {
	for _, targets := range [][]string{nil, {"projects/my-project-123"}, {billingA}} {
		e := newEnvironment(t, targets)
		e.fake.routes["/v1/services"] = okReply(`{"services":[]}`)
		if _, err := e.invoke(catalogServicesListID, `{}`); err != nil {
			t.Errorf("targets %v: %v", targets, err)
		}
	}
}

const skuBody = `{"name":"` + catalogService + `/skus/D0A1-2B3C-4D5E","skuId":"D0A1-2B3C-4D5E","description":"N1 core",` +
	`"category":{"serviceDisplayName":"Compute Engine","resourceFamily":"Compute","resourceGroup":"CPU","usageType":"OnDemand"},` +
	`"serviceRegions":["europe-west1"],"pricingInfo":[{"summary":"s","pricingExpression":{"usageUnit":"h",` +
	`"usageUnitDescription":"hour","baseUnit":"s","baseUnitDescription":"second","baseUnitConversionFactor":3600,` +
	`"displayQuantity":1,"tieredRates":[{"startUsageAmount":0,"unitPrice":{"currencyCode":"EUR"}},` +
	`{"startUsageAmount":100,"unitPrice":{"currencyCode":"EUR","units":"1","nanos":500000000}}]},` +
	`"currencyConversionRate":0.9,"effectiveTime":"2026-11-02T00:00:00Z","aggregationInfo":{"aggregationLevel":"ACCOUNT",` +
	`"aggregationInterval":"MONTHLY","aggregationCount":1}}],"geoTaxonomy":{"type":"REGIONAL","regions":["europe-west1"]},` +
	`"serviceProviderName":"Google"}`

func TestCatalogSkusListSendsWindowAndCurrencyAndFollowsPages(t *testing.T) {
	e := newEnvironment(t, []string{billingA})
	e.fake.hook = func(_ int, req *http.Request) *reply {
		if req.URL.Path != "/v1/"+catalogService+"/skus" {
			return nil
		}
		if req.URL.Query().Get("pageToken") == "" {
			return &reply{status: 200, body: `{"skus":[` + skuBody + `],"nextPageToken":"p2"}`}
		}
		return &reply{status: 200, body: `{"skus":[]}`}
	}
	args := `{"service_id":"` + catalogService + `","start_time":"2026-11-01T00:00:00Z","end_time":"2026-11-10T00:00:00Z",` +
		`"currency_code":"EUR","page_size":1`
	out, err := e.invoke(catalogSkusListID, args+`}`)
	if err != nil {
		t.Fatal(err)
	}
	rows, got := listOf(t, out, "skus")
	if len(rows) != 1 || got["has_more"] != true {
		t.Fatalf("page 1 = %s", out)
	}
	for _, want := range []string{`"skuId":"D0A1-2B3C-4D5E"`, `"description":"N1 core"`, `"resourceFamily":"Compute"`,
		`"serviceRegions":["europe-west1"]`, `"startUsageAmount":0`, `"startUsageAmount":100`,
		`"unitPrice":{"currencyCode":"EUR","nanos":500000000,"units":"1"}`, `"baseUnitConversionFactor":3600`,
		`"currencyConversionRate":0.9`, `"effectiveTime":"2026-11-02T00:00:00Z"`, `"aggregationCount":1`,
		`"geoTaxonomy":{"regions":["europe-west1"],"type":"REGIONAL"}`, `"serviceProviderName":"Google"`} {
		if !strings.Contains(out, want) {
			t.Errorf("result lacks %s: %s", want, out)
		}
	}
	if _, err := e.invoke(catalogSkusListID, args+`,"cursor":"`+got["next_cursor"].(string)+`"}`); err != nil {
		t.Fatal(err)
	}
	queries := queriesOf(e)
	if queries[0].Get("startTime") != "2026-11-01T00:00:00Z" || queries[0].Get("endTime") != "2026-11-10T00:00:00Z" ||
		queries[0].Get("currencyCode") != "EUR" || queries[0].Get("pageSize") != "1" || queries[1].Get("pageToken") != "p2" {
		t.Errorf("queries = %v", queries)
	}
	// The bare ID names the same service.
	if _, err := e.invoke(catalogSkusListID, `{"service_id":"6F81-5844-456A"}`); err != nil {
		t.Fatal(err)
	}
	if last := queriesOf(e)[2]; last.Get("pageSize") != "100" || last.Get("currencyCode") != "" {
		t.Errorf("query = %v", last)
	}
}

func TestCatalogToolsRefuseBeforeIO(t *testing.T) {
	for name, args := range map[string][2]string{
		"currency lowercase": {catalogSkusListID, `{"service_id":"` + catalogService + `","currency_code":"eur"}`},
		"currency long":      {catalogSkusListID, `{"service_id":"` + catalogService + `","currency_code":"EURO"}`},
		"service path":       {catalogSkusListID, `{"service_id":"services/6F81-5844-456A/skus"}`},
		"service dotdot":     {catalogSkusListID, `{"service_id":"../x"}`},
		"service lowercase":  {catalogSkusListID, `{"service_id":"6f81-5844-456a"}`},
		"no service":         {catalogSkusListID, `{}`},
		"bad start":          {catalogSkusListID, `{"service_id":"` + catalogService + `","start_time":"yesterday-and-more-text"}`},
		"bad end":            {catalogSkusListID, `{"service_id":"` + catalogService + `","end_time":"2026-13-45T00:00:00Z"}`},
		"sku page size":      {catalogSkusListID, `{"service_id":"` + catalogService + `","page_size":101}`},
		"sku cursor":         {catalogSkusListID, `{"service_id":"` + catalogService + `","cursor":"bogus"}`},
		"services page size": {catalogServicesListID, `{"page_size":0}`},
		"services cursor":    {catalogServicesListID, `{"cursor":"bogus"}`},
	} {
		e := newEnvironment(t, nil)
		if _, err := e.invoke(args[0], args[1]); err == nil {
			t.Errorf("%s: want a refusal", name)
		}
		if len(e.fake.api()) != 0 || e.fake.tokenRequests() != 0 || e.lookups != 0 {
			t.Errorf("%s: api=%d tokens=%d lookups=%d", name, len(e.fake.api()), e.fake.tokenRequests(), e.lookups)
		}
	}
	e := newEnvironment(t, nil)
	if _, err := handleCatalogSkusList(t.Context(), e.resolved(), e.secrets(), e.red,
		[]byte(`{"service_id":"`+catalogService+`","currency_code":"xx"}`)); !isInvalidRequest(err) || len(e.fake.api()) != 0 {
		t.Errorf("err = %v", err)
	}
}

func TestCatalogToolsMapForbiddenNotFoundAndInvalidRows(t *testing.T) {
	for status, want := range map[int]provider.Class{403: provider.ClassPermission, 404: provider.ClassNotFound} {
		state := map[int]string{403: "PERMISSION_DENIED", 404: "NOT_FOUND"}[status]
		for tool, args := range map[string]string{catalogServicesListID: `{}`,
			catalogSkusListID: `{"service_id":"` + catalogService + `"}`} {
			e := newEnvironment(t, nil)
			e.fake.hook = func(_ int, req *http.Request) *reply {
				return &reply{status: status, body: errorObject(status, state, "")}
			}
			_, err := e.invoke(tool, args)
			if classOf(err) != want {
				t.Errorf("%s %d: class = %q, want %q (%v)", tool, status, classOf(err), want, err)
			}
			if err != nil && strings.Contains(err.Error(), detailCanary) {
				t.Errorf("%s %d: the error echoes the provider text", tool, status)
			}
		}
	}
	e := newEnvironment(t, nil)
	e.fake.routes["/v1/"+catalogService+"/skus"] = okReply(`{"skus":[{"name":"services/0000-1111-2222/skus/X"}]}`)
	if _, err := e.invoke(catalogSkusListID, `{"service_id":"`+catalogService+`"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("class = %q", classOf(err))
	}
	e.fake.routes["/v1/services"] = okReply(`{"services":[{"name":"projects/x"}]}`)
	if _, err := e.invoke(catalogServicesListID, `{}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("class = %q", classOf(err))
	}
}

func TestCatalogSkusBoundsTextAndTiers(t *testing.T) {
	e := newEnvironment(t, nil)
	var tiers []string
	for range maxListItems + 20 {
		tiers = append(tiers, `{"startUsageAmount":1}`)
	}
	long := strings.Repeat("y", 3000)
	e.fake.routes["/v1/"+catalogService+"/skus"] = okReply(`{"skus":[{"name":"` + catalogService + `/skus/A","description":"` +
		long + `\u0007","pricingInfo":[{"pricingExpression":{"tieredRates":[` + strings.Join(tiers, ",") + `]}}]}]}`)
	out, err := e.invoke(catalogSkusListID, `{"service_id":"`+catalogService+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "\u0007") || strings.Count(out, "y") > maxText || strings.Count(out, `"startUsageAmount"`) != maxListItems {
		t.Errorf("result is not bounded: %d bytes", len(out))
	}
}
