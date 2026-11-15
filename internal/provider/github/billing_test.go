package github

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const billingProviderCanary = "billing-provider-text-canary-5e1f"

const billingUsageJSON = `{"usageItems":[` +
	`{"date":"2026-03-01","product":"actions","sku":"actions_linux","quantity":120,"unitType":"minutes",` +
	`"pricePerUnit":0.008,"grossAmount":0.96000000000000000001,"discountAmount":0.96,"netAmount":0,` +
	`"organizationName":"octo-org","repositoryName":"example","hidden":"` + billingProviderCanary + `"},` +
	`{"date":"2026-03-02","product":"actions","sku":"actions_linux","quantity":5,"unitType":"minutes",` +
	`"pricePerUnit":0.008,"grossAmount":0.04,"discountAmount":0,"netAmount":0.04,"organizationName":"octo-org",` +
	`"repositoryName":"secret"}]}`

const billingUserUsageJSON = `{"usageItems":[{"date":"2026-03-01","product":"copilot","sku":"copilot_pro",` +
	`"quantity":1,"unitType":"seats","pricePerUnit":10,"grossAmount":10,"discountAmount":0,"netAmount":10}]}`

const billingSummaryJSON = `{"timePeriod":{"year":2026,"month":3},"organization":"octo-org","repository":"octo-org/example",` +
	`"product":"actions","sku":"actions_linux","usageItems":[{"product":"actions","sku":"actions_linux",` +
	`"unitType":"minutes","pricePerUnit":0.008,"grossQuantity":125,"grossAmount":1,"discountQuantity":120,` +
	`"discountAmount":0.96,"netQuantity":5,"netAmount":0.04,"extra":"` + billingProviderCanary + `"}]}`

const billingUserSummaryJSON = `{"timePeriod":{"year":2026},"user":"octocat","usageItems":[]}`

const billingModelJSON = `{"timePeriod":{"year":2026,"month":3,"day":4},"organization":"octo-org","user":"octocat",` +
	`"product":"Copilot","model":"GPT-5","usageItems":[{"product":"Copilot","sku":"Copilot Premium Request",` +
	`"model":"GPT-5","unitType":"requests","pricePerUnit":0.04,"grossQuantity":10,"grossAmount":0.4,` +
	`"discountQuantity":10,"discountAmount":0.4,"netQuantity":0,"netAmount":0}]}`

const billingUserModelJSON = `{"timePeriod":{"year":2026},"user":"octocat","usageItems":[]}`

const billingBudgetJSON = `{"id":"2066deda-923f-43f9","budget_type":"SkuPricing","budget_amount":100,` +
	`"prevent_further_usage":true,"budget_scope":"repository","budget_entity_name":"octo-org/example",` +
	`"consumed_amount":12.5,"budget_product_sku":"actions_linux","budget_alerting":{"will_alert":true,` +
	`"alert_recipients":["mona","lisa"]},"expires_at":"2027-01-01"}`

const billingSecretBudgetJSON = `{"id":"secret-budget-1","budget_type":"ProductPricing","budget_amount":5,` +
	`"prevent_further_usage":false,"budget_scope":"repository","budget_entity_name":"octo-org/secret",` +
	`"budget_product_sku":"actions","budget_alerting":{"will_alert":false,"alert_recipients":[]}}`

const billingBudgetsJSON = `{"budgets":[` + billingBudgetJSON + `,` + billingSecretBudgetJSON + `,` +
	`{"id":"org-budget-1","budget_type":"ProductPricing","budget_amount":50,"prevent_further_usage":false,` +
	`"budget_scope":"organization","budget_entity_name":"octo-org","budget_product_sku":"copilot",` +
	`"budget_alerting":{"will_alert":false,"alert_recipients":[]}}],"user":"mona",` +
	`"effective_budget":{"id":"eff-1","budget_amount":20,"consumed_amount":3.25},"has_next_page":true,"total_count":3}`

// fakeBilling answers the billing routes of octo-org and of the user octocat through the failure hook.
type fakeBilling struct {
	mu sync.Mutex
	// status, when set, answers every billing read with this status and GitHub's own text.
	status int
	// items, when set, replaces the usage items of the organization usage report with this many entries.
	items int
}

func (b *fakeBilling) route(w http.ResponseWriter, req *http.Request) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	path := strings.TrimPrefix(req.URL.EscapedPath(), "/api/v3")
	var owner, rest string
	switch {
	case strings.HasPrefix(path, "/organizations/octo-org/settings/billing/"):
		owner, rest = "org", strings.TrimPrefix(path, "/organizations/octo-org/settings/billing/")
	case strings.HasPrefix(path, "/users/octocat/settings/billing/"):
		owner, rest = "user", strings.TrimPrefix(path, "/users/octocat/settings/billing/")
	default:
		return false
	}
	if req.Method != http.MethodGet {
		return false
	}
	w.Header().Set("Content-Type", "application/json")
	if b.status != 0 {
		w.WriteHeader(b.status)
		fmt.Fprintf(w, `{"message":%q}`, billingProviderCanary)
		return true
	}
	pick := func(org, user string) { fmt.Fprint(w, map[string]string{"org": org, "user": user}[owner]) }
	switch rest {
	case "usage":
		if owner == "org" && b.items > 0 {
			fmt.Fprint(w, `{"usageItems":[`)
			for i := 0; i < b.items; i++ {
				if i > 0 {
					fmt.Fprint(w, ",")
				}
				fmt.Fprintf(w, `{"date":"2026-03-01","product":"actions","sku":"s%d","quantity":%d,"unitType":"minutes",`+
					`"pricePerUnit":1,"grossAmount":1,"discountAmount":0,"netAmount":1,"organizationName":"octo-org"}`, i, i)
			}
			fmt.Fprint(w, `]}`)
			return true
		}
		pick(billingUsageJSON, billingUserUsageJSON)
	case "usage/summary":
		pick(billingSummaryJSON, billingUserSummaryJSON)
	case "premium_request/usage", "ai_credit/usage":
		pick(billingModelJSON, billingUserModelJSON)
	case "budgets":
		pick(billingBudgetsJSON, `{"budgets":[]}`)
	case "budgets/2066deda-923f-43f9":
		pick(billingBudgetJSON, "")
	case "budgets/secret-budget-1":
		pick(billingSecretBudgetJSON, "")
	default:
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"message":"Not Found"}`)
	}
	return true
}

func serveBilling(t *testing.T) (*fakeGitHub, *fakeBilling, string) {
	t.Helper()
	b := &fakeBilling{}
	f := &fakeGitHub{}
	f.failure = b.route
	return f, b, serve(t, f)
}

func billingConfig(base string) *config.Config {
	cfg := coreConfig(base)
	read := []config.Effect{config.EffectRead}
	cfg.Connections["bill-org"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Targets: []string{"orgs/octo-org", "repos/octo-org/*"}, Effects: read}
	cfg.Connections["bill-user"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Targets: []string{"users/octocat"}, Effects: read}
	cfg.Connections["bill-excluding"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Targets: []string{"orgs/octo-org"}, Exclude: []string{"repos/octo-org/secret"}, Effects: read}
	cfg.Connections["bill-excluded-owner"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Exclude: []string{"orgs/octo-org"}, Effects: read}
	cfg.Connections["bill-repo-only"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Targets: []string{"repos/octo-org/example"}, Effects: read}
	cfg.Connections["bill-write-only"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Targets: []string{"orgs/octo-org"}, Effects: []config.Effect{config.EffectUpdate}}
	return cfg
}

func billingCore(t *testing.T, base string, reads *int) *application.Core {
	t.Helper()
	red := &redact.Redactor{}
	return application.New(registry(t), billingConfig(base), resolver(red, reads), red)
}

const (
	billingOrgPath  = "/api/v3/organizations/octo-org/settings/billing/"
	billingUserPath = "/api/v3/users/octocat/settings/billing/"
)

// billingCalls holds one valid call of every tool for each kind of owner it serves, with the request it must
// send and members its result must hold.
var billingCalls = []struct {
	name, id, connection, arguments, path, query string
	want                                         []string
}{
	{"organization usage", billingUsageGet.ID, "bill-org", `{"owner":"orgs/octo-org","year":2026,"month":3,"day":1}`,
		billingOrgPath + "usage", "day=1&month=3&year=2026",
		[]string{`"owner":"orgs/octo-org"`, `"date":"2026-03-01"`, `"sku":"actions_linux"`, `"quantity":120`,
			`"unit_type":"minutes"`, `"price_per_unit":0.008`, `"gross_amount":0.96000000000000000001`,
			`"net_amount":0`, `"organization_name":"octo-org"`, `"repository_name":"example"`, `"truncated":false`}},
	{"user usage", billingUsageGet.ID, "bill-user", `{"owner":"users/octocat"}`, billingUserPath + "usage", "",
		[]string{`"owner":"users/octocat"`, `"sku":"copilot_pro"`, `"truncated":false`}},
	{"organization summary", billingSummaryGet.ID, "bill-org",
		`{"owner":"orgs/octo-org","year":2026,"month":3,"day":4,"repository":"octo-org/example","product":"actions","sku":"actions_linux"}`,
		billingOrgPath + "usage/summary", "day=4&month=3&product=actions&repository=octo-org%2Fexample&sku=actions_linux&year=2026",
		[]string{`"time_period":{"month":3,"year":2026}`, `"organization":"octo-org"`, `"repository":"octo-org/example"`,
			`"gross_quantity":125`, `"discount_amount":0.96`, `"net_quantity":5`, `"truncated":false`}},
	{"user summary", billingSummaryGet.ID, "bill-user", `{"owner":"users/octocat","month":3}`,
		billingUserPath + "usage/summary", "month=3", []string{`"user":"octocat"`, `"time_period":{"year":2026}`}},
	{"organization premium requests", billingPremiumRequestsGet.ID, "bill-org",
		`{"owner":"orgs/octo-org","year":2026,"month":3,"day":4,"user":"octocat","model":"GPT-5","product":"Copilot"}`,
		billingOrgPath + "premium_request/usage", "day=4&model=GPT-5&month=3&product=Copilot&user=octocat&year=2026",
		[]string{`"model":"GPT-5"`, `"user":"octocat"`, `"sku":"Copilot Premium Request"`, `"gross_quantity":10`,
			`"time_period":{"day":4,"month":3,"year":2026}`}},
	{"user premium requests", billingPremiumRequestsGet.ID, "bill-user", `{"owner":"users/octocat","model":"GPT-5"}`,
		billingUserPath + "premium_request/usage", "model=GPT-5", []string{`"user":"octocat"`}},
	{"organization AI credits", billingAICreditsGet.ID, "bill-org",
		`{"owner":"orgs/octo-org","user":"octocat","model":"GPT-5","product":"Copilot","year":2026}`,
		billingOrgPath + "ai_credit/usage", "model=GPT-5&product=Copilot&user=octocat&year=2026",
		[]string{`"organization":"octo-org"`, `"net_amount":0`}},
	{"user AI credits", billingAICreditsGet.ID, "bill-user", `{"owner":"users/octocat","product":"Copilot"}`,
		billingUserPath + "ai_credit/usage", "product=Copilot", []string{`"user":"octocat"`}},
	{"budget list", billingBudgetsList.ID, "bill-org", `{"owner":"orgs/octo-org","scope":"repository","user":"mona","limit":10}`,
		billingOrgPath + "budgets", "page=1&per_page=10&scope=repository&user=mona",
		[]string{`"id":"2066deda-923f-43f9"`, `"budget_type":"SkuPricing"`, `"budget_amount":100`,
			`"prevent_further_usage":true`, `"budget_scope":"repository"`, `"budget_entity_name":"octo-org/example"`,
			`"consumed_amount":12.5`, `"budget_product_sku":"actions_linux"`, `"alert_recipients":["mona","lisa"]`,
			`"expires_at":"2027-01-01"`, `"effective_budget":{"budget_amount":20,"consumed_amount":3.25,"id":"eff-1"}`,
			`"total_count":3`, `"has_more":true`, `"next_cursor"`, `"user":"mona"`, `"owner":"orgs/octo-org"`}},
	{"budget get", billingBudgetsGet.ID, "bill-org", `{"owner":"orgs/octo-org","budget_id":"2066deda-923f-43f9"}`,
		billingOrgPath + "budgets/2066deda-923f-43f9", "",
		[]string{`"id":"2066deda-923f-43f9"`, `"budget_scope":"repository"`, `"consumed_amount":12.5`,
			`"will_alert":true`, `"owner":"orgs/octo-org"`}},
}

func TestBillingToolsSatisfyTheirContractThroughTheApplicationCore(t *testing.T) {
	f, _, base := serveBilling(t)
	core := billingCore(t, base, nil)
	for _, call := range billingCalls {
		before := len(f.recorded())
		result, err := invoke(t, core, call.id, call.connection, call.arguments, false)
		if err != nil {
			t.Fatalf("%s = %v", call.name, err)
		}
		for _, want := range call.want {
			if !strings.Contains(string(result), want) {
				t.Errorf("%s = %s, want %s", call.name, result, want)
			}
		}
		requests := f.recorded()[before:]
		if len(requests) != 1 || requests[0].method != http.MethodGet || requests[0].path != call.path ||
			requests[0].query != call.query {
			t.Fatalf("%s sent %+v, want one GET %s?%s", call.name, requests, call.path, call.query)
		}
		if strings.Contains(string(result), billingProviderCanary) {
			t.Errorf("%s carries an undocumented member: %s", call.name, result)
		}
	}
}

// A single budget does not show the expiry GitHub documents for the list only, and the budget list shows it.
func TestBillingBudgetGetCarriesNoExpiry(t *testing.T) {
	_, _, base := serveBilling(t)
	core := billingCore(t, base, nil)
	result, err := invoke(t, core, billingBudgetsGet.ID, "bill-org",
		`{"owner":"orgs/octo-org","budget_id":"2066deda-923f-43f9"}`, false)
	if err != nil || strings.Contains(string(result), "expires_at") {
		t.Errorf("budget get = %s, %v", result, err)
	}
}

// Every refusal is settled before a credential is read or GitHub is contacted.
func TestBillingRefusalsBeforeIO(t *testing.T) {
	f, _, base := serveBilling(t)
	reads := 0
	core := billingCore(t, base, &reads)
	for _, tt := range []struct {
		name, id, connection, arguments, message string
	}{
		{"owner outside the targets", billingUsageGet.ID, "bill-org", `{"owner":"orgs/other"}`, "outside the targets"},
		{"foreign owner for the budgets", billingBudgetsList.ID, "bill-org", `{"owner":"orgs/other"}`, "outside the targets"},
		{"excluded owner", billingSummaryGet.ID, "bill-excluded-owner", `{"owner":"orgs/octo-org"}`, "not available"},
		{"excluded owner without argument", billingUsageGet.ID, "bill-excluded-owner", `{}`, ""},
		{"repository target only", billingUsageGet.ID, "bill-repo-only", `{}`, ""},
		{"repository target only with owner", billingUsageGet.ID, "bill-repo-only", `{"owner":"orgs/octo-org"}`, "outside the targets"},
		{"malformed owner", billingUsageGet.ID, "bill-org", `{"owner":"octo-org"}`, ""},
		{"user owner for the budget list", billingBudgetsList.ID, "bill-user", `{"owner":"users/octocat"}`, "organization"},
		{"user owner for the budget read", billingBudgetsGet.ID, "bill-user",
			`{"owner":"users/octocat","budget_id":"abc"}`, "organization"},
		{"user filter for a user report", billingPremiumRequestsGet.ID, "bill-user",
			`{"owner":"users/octocat","user":"octocat"}`, "organization"},
		{"user filter for a user AI credit report", billingAICreditsGet.ID, "bill-user",
			`{"owner":"users/octocat","user":"octocat"}`, "organization"},
		{"path in the user filter", billingPremiumRequestsGet.ID, "bill-org", `{"owner":"orgs/octo-org","user":"a/b"}`, ""},
		{"path in the budget id", billingBudgetsGet.ID, "bill-org", `{"owner":"orgs/octo-org","budget_id":"a/../b"}`, ""},
		{"query in the budget id", billingBudgetsGet.ID, "bill-org", `{"owner":"orgs/octo-org","budget_id":"a?x=1"}`, ""},
		{"empty budget id", billingBudgetsGet.ID, "bill-org", `{"owner":"orgs/octo-org","budget_id":""}`, ""},
		{"missing budget id", billingBudgetsGet.ID, "bill-org", `{"owner":"orgs/octo-org"}`, ""},
		{"year with three digits", billingUsageGet.ID, "bill-org", `{"owner":"orgs/octo-org","year":202}`, ""},
		{"month 13", billingUsageGet.ID, "bill-org", `{"owner":"orgs/octo-org","month":13}`, ""},
		{"day 32", billingSummaryGet.ID, "bill-org", `{"owner":"orgs/octo-org","day":32}`, ""},
		{"day zero", billingSummaryGet.ID, "bill-org", `{"owner":"orgs/octo-org","day":0}`, ""},
		{"hour is no filter", billingUsageGet.ID, "bill-org", `{"owner":"orgs/octo-org","hour":3}`, ""},
		{"cost center is no filter", billingUsageGet.ID, "bill-org", `{"owner":"orgs/octo-org","cost_center_id":"x"}`, ""},
		{"repository of another owner", billingSummaryGet.ID, "bill-org",
			`{"owner":"orgs/octo-org","repository":"other/example"}`, "owner of the call"},
		{"excluded repository", billingSummaryGet.ID, "bill-excluding",
			`{"owner":"orgs/octo-org","repository":"octo-org/secret"}`, "not available"},
		{"excluded repository in another spelling", billingSummaryGet.ID, "bill-excluding",
			`{"owner":"orgs/octo-org","repository":"OCTO-org/Secret"}`, "not available"},
		{"malformed repository", billingSummaryGet.ID, "bill-org", `{"owner":"orgs/octo-org","repository":"example"}`, "OWNER/REPO"},
		{"control character in the product", billingSummaryGet.ID, "bill-org",
			"{\"owner\":\"orgs/octo-org\",\"product\":\"a\\u0007b\"}", "control characters"},
		{"control character in the model", billingAICreditsGet.ID, "bill-org",
			"{\"owner\":\"orgs/octo-org\",\"model\":\"a\\nb\"}", "control characters"},
		{"oversized sku", billingSummaryGet.ID, "bill-org",
			`{"owner":"orgs/octo-org","sku":"` + strings.Repeat("a", 101) + `"}`, ""},
		{"unknown budget scope", billingBudgetsList.ID, "bill-org", `{"owner":"orgs/octo-org","scope":"team"}`, ""},
		{"path in the budget user", billingBudgetsList.ID, "bill-org", `{"owner":"orgs/octo-org","user":"a/b"}`, ""},
		{"limit above the bound", billingBudgetsList.ID, "bill-org", `{"owner":"orgs/octo-org","limit":101}`, ""},
		{"foreign cursor", billingBudgetsList.ID, "bill-org", `{"owner":"orgs/octo-org","cursor":"AAAA"}`, ""},
		{"free route", billingUsageGet.ID, "bill-org", `{"owner":"orgs/octo-org","path":"/x"}`, ""},
	} {
		reads = 0
		before := len(f.recorded())
		_, err := invoke(t, core, tt.id, tt.connection, tt.arguments, false)
		var unsupported *capability.UnsupportedError
		if err == nil || errors.As(err, &unsupported) ||
			(tt.message != "" && !strings.Contains(err.Error(), tt.message)) {
			t.Errorf("%s: err = %v", tt.name, err)
		}
		if reads != 0 || len(f.recorded()) != before {
			t.Errorf("%s reached the credential (%d) or GitHub", tt.name, reads)
		}
	}
}

// A cursor leads to the next page of the same list and is refused for other filters.
func TestBillingBudgetListPagesByCursor(t *testing.T) {
	f, _, base := serveBilling(t)
	core := billingCore(t, base, nil)
	first, err := invoke(t, core, billingBudgetsList.ID, "bill-org", `{`+teamOwner+`,"limit":5,"scope":"repository"}`, false)
	if err != nil {
		t.Fatal(err)
	}
	cursor := between(string(first), `"next_cursor":"`, `"`)
	if cursor == "" {
		t.Fatalf("first page = %s, want a cursor", first)
	}
	before := len(f.recorded())
	if _, err := invoke(t, core, billingBudgetsList.ID, "bill-org",
		`{`+teamOwner+`,"limit":5,"scope":"repository","cursor":"`+cursor+`"}`, false); err != nil {
		t.Fatal(err)
	}
	if got := f.recorded()[before:]; len(got) != 1 || got[0].query != "page=2&per_page=5&scope=repository" {
		t.Errorf("second page sent %+v, want page=2&per_page=5&scope=repository", got)
	}
	before = len(f.recorded())
	_, err = invoke(t, core, billingBudgetsList.ID, "bill-org", `{`+teamOwner+`,"limit":5,"cursor":"`+cursor+`"}`, false)
	if !isInvalidRequest(err) || len(f.recorded()) != before {
		t.Errorf("a cursor of other filters = %v, want an invalid request without a request", err)
	}
}

// The tools are offered by the read effect only, and a refused call never reads a credential.
func TestBillingToolsNeedTheReadEffect(t *testing.T) {
	f, _, base := serveBilling(t)
	reads := 0
	core := billingCore(t, base, &reads)
	for _, call := range billingCalls {
		_, err := invoke(t, core, call.id, "bill-write-only", call.arguments, false)
		var unsupported *capability.UnsupportedError
		if !errors.As(err, &unsupported) {
			t.Errorf("%s on a connection without the read effect = %v, want unsupported", call.name, err)
		}
	}
	if reads != 0 || len(f.recorded()) != 0 {
		t.Errorf("refused calls read %d credentials and sent %d requests", reads, len(f.recorded()))
	}
}

// Money is only ever read: no billing tool changes anything, and no budget write exists.
func TestBillingCatalogHoldsOnlyReads(t *testing.T) {
	reg := registry(t)
	seen := 0
	for _, d := range reg.All() {
		if !strings.Contains(d.ID, "billing") && !strings.Contains(d.ID, "budget") {
			continue
		}
		seen++
		if d.Risk.Effect != capability.EffectRead || d.Risk.Confirmation != capability.ConfirmationNone || d.RequiresToolAllowList {
			t.Errorf("%s = %+v, want a plain read", d.ID, d.Risk)
		}
		for _, verb := range []string{".create", ".update", ".delete", ".set", ".remove", ".add"} {
			if strings.HasSuffix(d.ID, verb) {
				t.Errorf("%s is a billing write", d.ID)
			}
		}
		if d.Group != "organization-admin" {
			t.Errorf("%s is in group %q, want organization-admin", d.ID, d.Group)
		}
	}
	if seen != len(billingToolIDs) {
		t.Errorf("found %d billing tools, want %d", seen, len(billingToolIDs))
	}
	for _, id := range billingToolIDs {
		if _, _, ok := reg.Lookup(id); !ok {
			t.Errorf("%s is not registered", id)
		}
	}
}

// 403 and 404 end as the state unavailable with a fixed reason; GitHub's text never reaches the answer. Every
// other failure stays an error named by its class, and nothing is repeated.
func TestBillingRefusalsOfGitHubAreAStateAndOtherFailuresAreErrors(t *testing.T) {
	f, b, base := serveBilling(t)
	core := billingCore(t, base, nil)
	for _, call := range billingCalls {
		for _, tt := range []struct {
			status int
			class  provider.Class
		}{
			{http.StatusForbidden, ""},
			{http.StatusNotFound, ""},
			{http.StatusUnprocessableEntity, provider.ClassProviderError},
			{http.StatusServiceUnavailable, provider.ClassProviderError},
			{http.StatusGatewayTimeout, provider.ClassTimeout},
			{http.StatusUnauthorized, provider.ClassAuth},
		} {
			b.status = tt.status
			before := len(f.recorded())
			result, err := invoke(t, core, call.id, call.connection, call.arguments, false)
			if got := len(f.recorded()[before:]); got != 1 {
				t.Errorf("%s answered %d sent %d requests, want 1 without a retry", call.name, tt.status, got)
			}
			if tt.class == "" {
				if err != nil || !strings.Contains(string(result), `"state":"unavailable"`) ||
					!strings.Contains(string(result), `"reason":"`) || strings.Contains(string(result), billingProviderCanary) ||
					strings.Contains(string(result), "usage_items") || strings.Contains(string(result), `"budgets"`) {
					t.Errorf("%s answered %d = %s, %v, want the state unavailable", call.name, tt.status, result, err)
				}
				if !strings.Contains(string(result), `"owner":"`) {
					t.Errorf("%s state does not name the owner: %s", call.name, result)
				}
				continue
			}
			if classOf(err) != tt.class || strings.Contains(err.Error(), billingProviderCanary) {
				t.Errorf("%s answered %d = %v, want %s without GitHub's text", call.name, tt.status, err, tt.class)
			}
		}
		b.status = 0
	}
}

// A rate limit is an error that names the wait, not a state.
func TestBillingRateLimitIsAnError(t *testing.T) {
	f := &fakeGitHub{}
	f.failure = func(w http.ResponseWriter, r *http.Request) bool {
		if !strings.Contains(r.URL.Path, "/settings/billing/") {
			return false
		}
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"message":"rate limit `+billingProviderCanary+`"}`)
		return true
	}
	core := billingCore(t, serve(t, f), nil)
	_, err := invoke(t, core, billingUsageGet.ID, "bill-org", `{`+teamOwner+`}`, false)
	if classOf(err) != provider.ClassRateLimited || strings.Contains(err.Error(), billingProviderCanary) {
		t.Errorf("rate limit = %v, want rate-limited without GitHub's text", err)
	}
}

// A repository the connection excludes drops its line items, its budgets, and its single budget, and the
// answer counts what it dropped.
func TestBillingHidesExcludedRepositories(t *testing.T) {
	_, _, base := serveBilling(t)
	core := billingCore(t, base, nil)
	usage, err := invoke(t, core, billingUsageGet.ID, "bill-excluding", `{`+teamOwner+`}`, false)
	if err != nil || strings.Contains(string(usage), `"repository_name":"secret"`) ||
		!strings.Contains(string(usage), `"repository_name":"example"`) || !strings.Contains(string(usage), `"withheld":1`) {
		t.Errorf("usage = %s, %v, want the excluded repository counted and left out", usage, err)
	}
	budgets, err := invoke(t, core, billingBudgetsList.ID, "bill-excluding", `{`+teamOwner+`}`, false)
	if err != nil || strings.Contains(string(budgets), "secret-budget-1") || strings.Contains(string(budgets), "octo-org/secret") ||
		!strings.Contains(string(budgets), "org-budget-1") || !strings.Contains(string(budgets), `"withheld":1`) {
		t.Errorf("budgets = %s, %v, want the excluded repository's budget counted and left out", budgets, err)
	}
	_, err = invoke(t, core, billingBudgetsGet.ID, "bill-excluding", `{`+teamOwner+`,"budget_id":"secret-budget-1"}`, false)
	if classOf(err) != provider.ClassNotFound {
		t.Errorf("budget of an excluded repository = %v, want not-found", err)
	}
	other, err := invoke(t, core, billingUsageGet.ID, "bill-org", `{`+teamOwner+`}`, false)
	if err != nil || !strings.Contains(string(other), `"repository_name":"secret"`) || strings.Contains(string(other), "withheld") {
		t.Errorf("usage without an exclusion = %s, %v", other, err)
	}
}

// A report over the bound is cut at its first items and says so; a number is passed on as GitHub wrote it.
func TestBillingReportsAreBoundedAndTruncatedIsVisible(t *testing.T) {
	_, b, base := serveBilling(t)
	b.items = maxBillingItems + 7
	core := billingCore(t, base, nil)
	result, err := invoke(t, core, billingUsageGet.ID, "bill-org", `{`+teamOwner+`}`, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(result), `"sku":"s`); got != maxBillingItems || !strings.Contains(string(result), `"truncated":true`) {
		t.Errorf("report holds %d items, truncated flag in %.60s..., want %d and true", got, result, maxBillingItems)
	}
	b.items = 3
	result, err = invoke(t, core, billingUsageGet.ID, "bill-org", `{`+teamOwner+`}`, false)
	if err != nil || strings.Count(string(result), `"sku":"s`) != 3 || !strings.Contains(string(result), `"truncated":false`) {
		t.Errorf("small report = %s, %v, want 3 items and truncated false", result, err)
	}
}

// Text GitHub supplies is cleaned and bounded, and an entry of a budget list without an identifier is no entry.
func TestBillingTextIsCleanedAndBudgetsNeedAnIdentifier(t *testing.T) {
	f := &fakeGitHub{}
	f.failure = func(w http.ResponseWriter, r *http.Request) bool {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/settings/billing/usage"):
			fmt.Fprint(w, `{"usageItems":[{"product":"line\u0007break\nsku","sku":"`+strings.Repeat("x", 500)+`","quantity":1}]}`)
		case strings.HasSuffix(r.URL.Path, "/settings/billing/budgets"):
			fmt.Fprint(w, `{"budgets":[{"budget_amount":1}]}`)
		default:
			return false
		}
		return true
	}
	core := billingCore(t, serve(t, f), nil)
	result, err := invoke(t, core, billingUsageGet.ID, "bill-org", `{`+teamOwner+`}`, false)
	if err != nil || strings.ContainsAny(string(result), "\a\n") || strings.Contains(string(result), strings.Repeat("x", 300)) ||
		!strings.Contains(string(result), `"product":"line break sku"`) {
		t.Errorf("usage = %q, %v, want cleaned and bounded text", result, err)
	}
	_, err = invoke(t, core, billingBudgetsList.ID, "bill-org", `{`+teamOwner+`}`, false)
	if classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("budget without identifier = %v, want invalid-response", err)
	}
}

// Alert recipients are bounded and the cut is visible.
func TestBillingAlertRecipientsAreBounded(t *testing.T) {
	var recipients []string
	for i := 0; i < projectionListLimit+5; i++ {
		recipients = append(recipients, fmt.Sprintf(`"u%d"`, i))
	}
	f := &fakeGitHub{}
	f.failure = func(w http.ResponseWriter, r *http.Request) bool {
		if !strings.Contains(r.URL.Path, "/settings/billing/budgets/") {
			return false
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"b1","budget_alerting":{"will_alert":true,"alert_recipients":[`+strings.Join(recipients, ",")+`]}}`)
		return true
	}
	core := billingCore(t, serve(t, f), nil)
	result, err := invoke(t, core, billingBudgetsGet.ID, "bill-org", `{`+teamOwner+`,"budget_id":"b1"}`, false)
	if err != nil || strings.Count(string(result), `"u`) != projectionListLimit ||
		!strings.Contains(string(result), `"alert_recipients_truncated":true`) {
		t.Errorf("budget = %s, %v, want %d recipients and the cut marked", result, err, projectionListLimit)
	}
}

// The owner decides the route prefix: /organizations/ for an organization and /users/ for a user, never /orgs/.
func TestBillingRoutesUseTheirOwnPrefix(t *testing.T) {
	f, _, base := serveBilling(t)
	core := billingCore(t, base, nil)
	for _, call := range billingCalls {
		if _, err := invoke(t, core, call.id, call.connection, call.arguments, false); err != nil {
			t.Fatalf("%s = %v", call.name, err)
		}
	}
	for _, request := range f.recorded() {
		if strings.HasPrefix(request.path, "/api/v3/orgs/") || !(strings.HasPrefix(request.path, billingOrgPath) ||
			strings.HasPrefix(request.path, billingUserPath)) {
			t.Errorf("request %s has an unexpected prefix", request.path)
		}
	}
}
