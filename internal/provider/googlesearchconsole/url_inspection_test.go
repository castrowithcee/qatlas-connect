package googlesearchconsole

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-kit/ratelimit"
)

const inspectPath = "/v1/urlInspection/index:inspect"

func inspectArgs(site, address string) string {
	return `{"site_url":"` + site + `","inspection_url":"` + address + `"}`
}

const fullAnswer = `{"inspectionResult":{"inspectionResultLink":"https://search.google.com/search-console/inspect?x=1",` +
	`"indexStatusResult":{"verdict":"PASS","coverageState":"Submitted and indexed","robotsTxtState":"ALLOWED",` +
	`"indexingState":"INDEXING_ALLOWED","lastCrawlTime":"2026-11-10T08:00:00Z","pageFetchState":"SUCCESSFUL",` +
	`"googleCanonical":"https://www.example.com/a","userCanonical":"https://www.example.com/a","crawledAs":"MOBILE",` +
	`"sitemap":["https://www.example.com/sitemap.xml"],"referringUrls":["https://www.example.com/"]},` +
	`"ampResult":{"verdict":"FAIL","ampIndexStatusVerdict":"NEUTRAL","indexingState":"AMP_INDEXING_ALLOWED",` +
	`"robotsTxtState":"ALLOWED","pageFetchState":"SUCCESSFUL","lastCrawlTime":"2026-11-09T08:00:00Z",` +
	`"ampUrl":"https://www.example.com/a/amp","issues":[{"severity":"ERROR","issueMessage":"Invalid tag"}]},` +
	`"richResultsResult":{"verdict":"PARTIAL","detectedItems":[{"richResultType":"Breadcrumbs","items":` +
	`[{"name":"Crumbs","issues":[{"severity":"WARNING","issueMessage":"Missing field"}]}]}]},` +
	`"mobileUsabilityResult":{"verdict":"PASS","issues":[{"issueType":"CONFIGURE_VIEWPORT"}]}}}`

func TestURLInspectionReturnsEveryBlockAndSendsDocumentedBody(t *testing.T) {
	env := newEnvironment(t, nil)
	env.fake.answer = reply(http.StatusOK, fullAnswer)
	out, err := env.invoke(t, urlInspectionID, "all", `{"site_url":"sc-domain:example.com",`+
		`"inspection_url":"https://www.example.com/a?x=1","language_code":"de-CH"}`)
	if err != nil {
		t.Fatal(err)
	}
	calls, _ := env.fake.calls()
	if len(calls) != 1 || calls[0].method != http.MethodPost || calls[0].escapedPath != inspectPath ||
		calls[0].host != "searchconsole.googleapis.com" || calls[0].contentType != "application/json" ||
		calls[0].rawQuery != "" || !strings.HasPrefix(calls[0].authorization, "Bearer "+accessPrefix) ||
		strings.Contains(calls[0].body, accessPrefix) {
		t.Fatalf("requests = %+v", calls)
	}
	want := map[string]any{"inspectionUrl": "https://www.example.com/a?x=1", "siteUrl": "sc-domain:example.com",
		"languageCode": "de-CH"}
	if got := bodyOf(t, calls, 0); !reflect.DeepEqual(got, want) {
		t.Errorf("body = %v, want %v", got, want)
	}
	result := out["inspectionResult"].(map[string]any)
	index := result["indexStatusResult"].(map[string]any)
	if len(index) != 11 || index["verdict"] != "PASS" || index["crawledAs"] != "MOBILE" ||
		index["googleCanonical"] != "https://www.example.com/a" || len(index["sitemap"].([]any)) != 1 {
		t.Errorf("indexStatusResult = %v", index)
	}
	amp := result["ampResult"].(map[string]any)
	if len(amp) != 8 || amp["ampUrl"] != "https://www.example.com/a/amp" ||
		amp["issues"].([]any)[0].(map[string]any)["severity"] != "ERROR" {
		t.Errorf("ampResult = %v", amp)
	}
	rich := result["richResultsResult"].(map[string]any)
	group := rich["detectedItems"].([]any)[0].(map[string]any)
	if rich["verdict"] != "PARTIAL" || group["richResultType"] != "Breadcrumbs" ||
		group["items"].([]any)[0].(map[string]any)["issues"].([]any)[0].(map[string]any)["issueMessage"] != "Missing field" {
		t.Errorf("richResultsResult = %v", rich)
	}
	if _, ok := result["mobileUsabilityResult"]; ok || result["inspectionResultLink"] == nil || out["truncated"] != nil {
		t.Errorf("result = %v", result)
	}
}

func TestURLInspectionAnswerWithOnlyIndexStatus(t *testing.T) {
	env := newEnvironment(t, nil)
	env.fake.answer = reply(http.StatusOK, `{"inspectionResult":{"indexStatusResult":{"verdict":"NEUTRAL"}}}`)
	out, err := env.invoke(t, urlInspectionID, "all", inspectArgs(domainSite, "https://example.com/"))
	if err != nil {
		t.Fatal(err)
	}
	result := out["inspectionResult"].(map[string]any)
	if _, amp := result["ampResult"]; amp || result["richResultsResult"] != nil ||
		result["indexStatusResult"].(map[string]any)["verdict"] != "NEUTRAL" {
		t.Errorf("result = %v", result)
	}
	calls, _ := env.fake.calls()
	if _, has := bodyOf(t, calls, 0)["languageCode"]; has {
		t.Error("an absent language_code was sent")
	}
}

func TestURLInspectionRefusesBeforeAnyRequest(t *testing.T) {
	env := newEnvironment(t, nil)
	cases := map[string]struct{ connection, args string }{
		"outside a prefix property":    {"all", inspectArgs(prefixSite, "https://www.example.com.evil.invalid/a")},
		"outside by path prefix":       {"all", inspectArgs("https://www.example.com/blog/", "https://www.example.com/shop/a")},
		"other scheme than property":   {"all", inspectArgs(prefixSite, "http://www.example.com/a")},
		"outside a domain property":    {"all", inspectArgs(domainSite, "https://notexample.com/a")},
		"domain only as a path":        {"all", inspectArgs(domainSite, "https://other.invalid/example.com/a")},
		"ftp scheme":                   {"all", inspectArgs(domainSite, "ftp://www.example.com/a")},
		"no scheme":                    {"all", inspectArgs(domainSite, "www.example.com/a/b")},
		"user information":             {"all", inspectArgs(domainSite, "https://u:p"+"@www.example.com/a")},
		"fragment":                     {"all", inspectArgs(domainSite, "https://www.example.com/a#x")},
		"space":                        {"all", inspectArgs(domainSite, "https://www.example.com/a b")},
		"bad language":                 {"all", `{"site_url":"sc-domain:example.com","inspection_url":"https://example.com/","language_code":"en_US!"}`},
		"foreign property when bound":  {"domain", inspectArgs(prefixSite, "https://www.example.com/a")},
		"other site when bound prefix": {"prefix", inspectArgs("https://shop.example.com/", "https://shop.example.com/a")},
		"no property at all":           {"all", `{"inspection_url":"https://www.example.com/a"}`},
	}
	for name, c := range cases {
		if _, err := env.invoke(t, urlInspectionID, c.connection, c.args); err == nil {
			t.Errorf("%s: accepted", name)
		} else if strings.Contains(err.Error(), "evil.invalid") || strings.Contains(err.Error(), "other.invalid") {
			t.Errorf("%s: the message quotes the value: %v", name, err)
		}
	}
	if calls, grants := env.fake.calls(); len(calls) != 0 || grants != 0 {
		t.Fatalf("%d requests and %d grants, want none", len(calls), grants)
	}
}

func TestURLInspectionAcceptsAddressesUnderTheProperty(t *testing.T) {
	env := newEnvironment(t, nil)
	env.fake.answer = reply(http.StatusOK, `{}`)
	for _, c := range [][2]string{
		{domainSite, "https://example.com/a"}, {domainSite, "http://Sub.Deep.Example.com:8080/a?q=1"},
		{prefixSite, "https://WWW.example.com/a/b?c=d"}, {"https://www.example.com/blog/", "https://www.example.com/blog/x"},
	} {
		if _, err := env.invoke(t, urlInspectionID, "all", inspectArgs(c[0], c[1])); err != nil {
			t.Errorf("%v: %v", c, err)
		}
	}
	if _, err := env.invoke(t, urlInspectionID, "domain", `{"inspection_url":"https://example.com/z"}`); err != nil {
		t.Errorf("bound property as default: %v", err)
	}
	calls, _ := env.fake.calls()
	if len(calls) != 5 || bodyOf(t, calls, 4)["siteUrl"] != domainSite {
		t.Errorf("%d requests", len(calls))
	}
}

func TestURLInspectionBoundsListsAndText(t *testing.T) {
	env := newEnvironment(t, nil)
	var sitemaps []string
	for range 80 {
		sitemaps = append(sitemaps, `"https://www.example.com/s.xml"`)
	}
	env.fake.answer = reply(http.StatusOK, `{"inspectionResult":{"indexStatusResult":{"verdict":"PASS","extra":1,`+
		`"coverageState":"`+strings.Repeat("x", 500)+`","sitemap":[`+strings.Join(sitemaps, ",")+`]}}}`)
	out, err := env.invoke(t, urlInspectionID, "all", inspectArgs(domainSite, "https://example.com/a"))
	if err != nil {
		t.Fatal(err)
	}
	index := out["inspectionResult"].(map[string]any)["indexStatusResult"].(map[string]any)
	state := index["coverageState"].(string)
	if len(index["sitemap"].([]any)) != maxInspectList || out["truncated"] != true || index["extra"] != nil ||
		!strings.HasSuffix(state, "…") || len([]rune(state)) != maxShortRunes+1 {
		t.Errorf("result = %v", out)
	}
}

func TestURLInspectionFailures(t *testing.T) {
	for name, c := range map[string]struct {
		answer   func(*http.Request, int) (*http.Response, error)
		class    provider.Class
		requests int
	}{
		"403": {reply(http.StatusForbidden, errorObject(403, "PERMISSION_DENIED", "FORBIDDEN")), provider.ClassPermission, 1},
		"429 without Retry-After": {reply(http.StatusTooManyRequests, errorObject(429, "RESOURCE_EXHAUSTED",
			"RATE_LIMIT_EXCEEDED")), provider.ClassRateLimited, 1},
		"401 after one renewal": {reply(http.StatusUnauthorized, errorObject(401, "UNAUTHENTICATED", "CREDENTIALS_MISSING")),
			provider.ClassAuth, 2},
		"unreadable body": {reply(http.StatusOK, `{"inspectionResult":`+detailCanary), provider.ClassInvalidResponse, 1},
		"not JSON": {func(r *http.Request, _ int) (*http.Response, error) {
			return respond(r, http.StatusOK, "text/html", "<p>"+detailCanary+"</p>"), nil
		}, provider.ClassInvalidResponse, 1},
		"timeout": {func(*http.Request, int) (*http.Response, error) {
			return nil, context.DeadlineExceeded
		}, provider.ClassTimeout, 1},
	} {
		t.Run(name, func(t *testing.T) {
			env := newEnvironment(t, nil)
			env.fake.answer = c.answer
			_, err := env.invoke(t, urlInspectionID, "all", inspectArgs(domainSite, "https://example.com/a"))
			if classOf(err) != c.class {
				t.Fatalf("error = %v, want %s", err, c.class)
			}
			if strings.Contains(err.Error(), detailCanary) || strings.Contains(err.Error(), accessPrefix) {
				t.Errorf("the message carries provider text: %v", err)
			}
			if calls, _ := env.fake.calls(); len(calls) != c.requests {
				t.Fatalf("%d requests, want %d", len(calls), c.requests)
			}
		})
	}
}

// fakeClock lets a limiter run a minute of calls without waiting: sleeping advances the clock.
type fakeClock struct{ now time.Time }

func (f *fakeClock) Now() time.Time { return f.now }
func (f *fakeClock) Sleep(_ context.Context, d time.Duration) error {
	f.now = f.now.Add(d)
	return nil
}

func TestURLInspectionSpacesCallsToSixHundredPerMinute(t *testing.T) {
	env := newEnvironment(t, nil)
	env.fake.answer = reply(http.StatusOK, `{}`)
	if inspectionInterval*600 != time.Minute {
		t.Fatalf("interval = %s, want 600 per minute", inspectionInterval)
	}
	clock := &fakeClock{now: time.Unix(1_800_000_000, 0)}
	start := clock.now
	registry := ratelimit.NewRegistry(inspectionInterval)
	inspectionLimiters = registry
	resolved, err := env.cfg.Resolve("all", "")
	if err != nil {
		t.Fatal(err)
	}
	key := credentialKey(resolved) + "\x00" + domainSite
	registry.Replace(key, ratelimit.New(inspectionInterval, clock.Now, clock.Sleep))
	for i := range 601 {
		if _, err := env.invoke(t, urlInspectionID, "all", inspectArgs(domainSite, "https://example.com/a")); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		if i == 599 && clock.now.Sub(start) >= time.Minute {
			t.Fatalf("600 calls took %s, want within the minute", clock.now.Sub(start))
		}
	}
	// The 601st call waits for the next minute's first slot.
	if got := clock.now.Sub(start); got != time.Minute {
		t.Errorf("601 calls spanned %s, want one minute", got)
	}
	// Another property has its own spacing.
	if inspectionLimiters.For(credentialKey(resolved)+"\x00"+prefixSite) == inspectionLimiters.For(key) {
		t.Error("two properties share one limiter")
	}
}

func TestURLInspectionRefusesThe2001stCallOfADayBeforeIO(t *testing.T) {
	env := newEnvironment(t, nil)
	env.fake.answer = reply(http.StatusOK, `{}`)
	resolved, err := env.cfg.Resolve("all", "")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, pacific())
	key := credentialKey(resolved) + "\x00" + domainSite
	inspectionDays.Replace(key, ratelimit.NewCounter(dailyInspections, ratelimit.Daily(pacific()),
		func() time.Time { return now }))
	for i := range dailyInspections {
		if _, err := env.invoke(t, urlInspectionID, "all", inspectArgs(domainSite, "https://example.com/a")); err != nil {
			t.Fatalf("call %d: %v", i+1, err)
		}
	}
	before, _ := env.fake.calls()
	_, err = env.invoke(t, urlInspectionID, "all", inspectArgs(domainSite, "https://example.com/a"))
	if classOf(err) != provider.ClassRateLimited || !strings.Contains(err.Error(), "retry after 43200 seconds") {
		t.Fatalf("call 2001 = %v", err)
	}
	if after, _ := env.fake.calls(); len(after) != len(before) {
		t.Errorf("the refused call sent %d requests", len(after)-len(before))
	}
	// Another property has its own count.
	if _, err := env.invoke(t, urlInspectionID, "all", inspectArgs(prefixSite, "https://www.example.com/a")); err != nil {
		t.Errorf("another property = %v", err)
	}
}

func TestURLInspectionTooManyRequestsHoldsTheNextCallAndIsNotOverridden(t *testing.T) {
	env := newEnvironment(t, nil)
	inspectionLimiters = ratelimit.NewRegistry(time.Millisecond)
	env.fake.answer = reply(http.StatusTooManyRequests, errorObject(429, "RESOURCE_EXHAUSTED", "RATE_LIMIT_EXCEEDED"))
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	request := application.InvokeRequest{Operation: urlInspectionID, Connection: "all",
		Arguments: json.RawMessage(inspectArgs(domainSite, "https://example.com/a"))}
	for i := range 2 {
		if _, err := env.core.Invoke(ctx, request); classOf(err) != provider.ClassRateLimited {
			t.Fatalf("call %d = %v", i, err)
		}
	}
	if calls, _ := env.fake.calls(); len(calls) != 1 {
		t.Errorf("%d requests, want one: a 429 never repeats and holds the next call", len(calls))
	}
}

func TestURLInspectionDescriptorIsAReadWithoutConfirmation(t *testing.T) {
	r := urlInspectionInspect.Risk
	if r.Effect != "read" || r.Confirmation != "none" || !r.OpenWorld || r.Idempotency != "safe" {
		t.Errorf("risk = %+v", r)
	}
	if strings.Contains(strings.ToLower(urlInspectionInspect.Description), "pages") ||
		!strings.Contains(urlInspectionInspect.Description, "2,000") || !strings.Contains(urlInspectionInspect.Description, "600") {
		t.Error("the description must name the quota and avoid the search word")
	}
}
