package googlemerchant

import (
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/provider/googleapi"
)

// The tool IDs of this package, fixed so a rename or an addition shows up here.
var wantToolIDs = []string{
	"google.merchant.account-issues.list",
	"google.merchant.account-relationships.get",
	"google.merchant.account-relationships.list",
	"google.merchant.account-relationships.update",
	"google.merchant.account-services.get",
	"google.merchant.account-services.list",
	"google.merchant.account-services.reject",
	"google.merchant.accounts.create-test-account",
	"google.merchant.accounts.delete",
	"google.merchant.accounts.get",
	"google.merchant.accounts.list",
	"google.merchant.accounts.list-subaccounts",
	"google.merchant.accounts.update",
	"google.merchant.aggregate-product-statuses.list",
	"google.merchant.autofeed-settings.get",
	"google.merchant.autofeed-settings.update",
	"google.merchant.automatic-improvements.get",
	"google.merchant.automatic-improvements.update",
	"google.merchant.business-identities.get",
	"google.merchant.business-identities.update",
	"google.merchant.business-infos.get",
	"google.merchant.business-infos.update",
	"google.merchant.checkout-settings.create",
	"google.merchant.checkout-settings.delete",
	"google.merchant.checkout-settings.get",
	"google.merchant.checkout-settings.update",
	"google.merchant.conversion-sources.create",
	"google.merchant.conversion-sources.delete",
	"google.merchant.conversion-sources.get",
	"google.merchant.conversion-sources.list",
	"google.merchant.conversion-sources.undelete",
	"google.merchant.conversion-sources.update",
	"google.merchant.data-sources.create",
	"google.merchant.data-sources.delete",
	"google.merchant.data-sources.fetch",
	"google.merchant.data-sources.get",
	"google.merchant.data-sources.list",
	"google.merchant.data-sources.update",
	"google.merchant.developer-registrations.get",
	"google.merchant.developer-registrations.get-account-for-gcp-registration",
	"google.merchant.developer-registrations.unregister-gcp",
	"google.merchant.email-preferences.get",
	"google.merchant.email-preferences.update",
	"google.merchant.file-uploads.get",
	"google.merchant.gbp-accounts.list",
	"google.merchant.homepages.claim",
	"google.merchant.homepages.get",
	"google.merchant.homepages.unclaim",
	"google.merchant.homepages.update",
	"google.merchant.issue-resolutions.render-account-issues",
	"google.merchant.issue-resolutions.render-product-issues",
	"google.merchant.lfp-providers.find",
	"google.merchant.limits.get",
	"google.merchant.limits.list",
	"google.merchant.local-inventories.delete",
	"google.merchant.local-inventories.insert",
	"google.merchant.local-inventories.list",
	"google.merchant.notification-subscriptions.delete",
	"google.merchant.notification-subscriptions.get",
	"google.merchant.notification-subscriptions.list",
	"google.merchant.omnichannel-settings.create",
	"google.merchant.omnichannel-settings.get",
	"google.merchant.omnichannel-settings.list",
	"google.merchant.omnichannel-settings.request-inventory-verification",
	"google.merchant.omnichannel-settings.update",
	"google.merchant.online-return-policies.create",
	"google.merchant.online-return-policies.delete",
	"google.merchant.online-return-policies.get",
	"google.merchant.online-return-policies.list",
	"google.merchant.product-inputs.delete",
	"google.merchant.product-inputs.insert",
	"google.merchant.product-inputs.update",
	"google.merchant.product-reviews.delete",
	"google.merchant.product-reviews.get",
	"google.merchant.product-reviews.list",
	"google.merchant.products.get",
	"google.merchant.products.list",
	"google.merchant.programs.disable",
	"google.merchant.programs.enable",
	"google.merchant.programs.get",
	"google.merchant.programs.list",
	"google.merchant.promotions.get",
	"google.merchant.promotions.insert",
	"google.merchant.promotions.list",
	"google.merchant.quotas.list",
	"google.merchant.regional-inventories.delete",
	"google.merchant.regional-inventories.insert",
	"google.merchant.regional-inventories.list",
	"google.merchant.regions.batch-create",
	"google.merchant.regions.batch-delete",
	"google.merchant.regions.batch-update",
	"google.merchant.regions.create",
	"google.merchant.regions.delete",
	"google.merchant.regions.get",
	"google.merchant.regions.list",
	"google.merchant.regions.update",
	"google.merchant.reports.search",
	"google.merchant.shipping-settings.get",
	"google.merchant.shipping-settings.insert",
	"google.merchant.terms-of-service-agreement-states.get",
	"google.merchant.terms-of-service-agreement-states.retrieve-for-application",
	"google.merchant.terms-of-service.get",
	"google.merchant.terms-of-service.retrieve-latest",
	"google.merchant.users.delete",
	"google.merchant.users.get",
	"google.merchant.users.list",
	"google.merchant.users.update",
}

func TestRegisterDeclaresTokenScopeTargetsAndFixedToolIDs(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, ok := reg.ProviderMetadata(Provider)
	if !ok || metadata.Token == nil || metadata.DocPath() != "google.merchant/setup.md" {
		t.Fatalf("metadata = %+v, %v", metadata, ok)
	}
	var ids []string
	for _, d := range reg.Provider(Provider) {
		ids = append(ids, d.ID)
		if scopes := metadata.Token.Scopes[d.ID]; len(scopes) != 1 || scopes[0] != contentScope {
			t.Errorf("%s scopes = %v, want only %s", d.ID, scopes, contentScope)
		}
		if d.Risk.Effect != capability.EffectRead {
			continue
		}
		if d.Version != 1 || d.Risk.Idempotency != capability.IdempotencySafe ||
			d.Risk.Confirmation != capability.ConfirmationNone || !d.Risk.OpenWorld || d.Risk.DataSensitivity == "" {
			t.Errorf("%s version or risk = %d, %+v", d.ID, d.Version, d.Risk)
		}
		if d.RequiresToolAllowList {
			t.Errorf("%s requires a tools list", d.ID)
		}
	}
	slices.Sort(ids)
	if !slices.Equal(ids, wantToolIDs) {
		t.Errorf("tool IDs = %v, want %v", ids, wantToolIDs)
	}
	oauth, ok := reg.OAuth(Provider)
	if !ok || oauth.TokenURL != googleapi.OAuth.TokenURL || oauth.Params["access_type"] != "offline" {
		t.Errorf("OAuth = %+v, %v", oauth, ok)
	}
	if len(metadata.Profiles) != 1 || metadata.Profiles[0].ID != "read" {
		t.Errorf("profiles = %+v", metadata.Profiles)
	}
	for _, id := range metadata.Profiles[0].Tools {
		if d, _, _ := reg.Lookup(id); d.Risk.Effect != capability.EffectRead || d.RequiresToolAllowList {
			t.Errorf("read profile lists %s with effect %s", id, d.Risk.Effect)
		}
	}
	var wantRead []string
	for _, d := range descriptors() {
		if d.Risk.Effect == capability.EffectRead && !d.RequiresToolAllowList {
			wantRead = append(wantRead, d.ID)
		}
	}
	if !slices.Equal(metadata.Profiles[0].Tools, wantRead) {
		t.Errorf("read profile tools = %v, want %v", metadata.Profiles[0].Tools, wantRead)
	}
	if err := metadata.Target.ValidateKindParents(); err != nil {
		t.Error(err)
	}
	for _, c := range []struct {
		kind, target string
		valid        bool
	}{
		{kindAdvanced, "advanced-accounts/1", true}, {kindAccount, "accounts/1000001", true},
		{kindAccount, "accounts/9223372036854775807", true},
		{kindAccount, "accounts/01", false}, {kindAccount, "accounts/x", false}, {kindAccount, "accounts/1/x", false},
		{kindAccount, "accounts/*", false}, {kindAdvanced, "advanced-accounts/0", false}, {kindAccount, "1", false},
	} {
		if got := metadata.Target.ValidTarget(c.kind, c.target); got != c.valid {
			t.Errorf("ValidTarget(%s, %q) = %t, want %t", c.kind, c.target, got, c.valid)
		}
	}
	if validateBaseURL("https://merchantapi.googleapis.com/") != nil || validateBaseURL("https://example.invalid") == nil ||
		validateBaseURL("http://merchantapi.googleapis.com") == nil {
		t.Error("validateBaseURL accepts another origin or refuses the fixed one")
	}
}

func TestDeveloperRegistrationNeedsTheAdvancedAccountTargetOfABoundConnection(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	for _, c := range []struct {
		targets []string
		tool    string
		want    bool
	}{
		{nil, developerRegistrationsGetID, true},
		{[]string{"advanced-accounts/1"}, developerRegistrationsGetID, true},
		{[]string{"accounts/2", "advanced-accounts/1"}, developerRegistrationsGetID, true},
		{[]string{"accounts/2"}, developerRegistrationsGetID, false},
		{[]string{"accounts/2"}, accountsGetID, true},
		{[]string{"accounts/2"}, accountsListID, true},
	} {
		got, hint := metadata.Target.Reach(c.tool, c.targets)
		if got != c.want || (!got && hint != "needs "+formAdvanced) {
			t.Errorf("Reach(%s, %v) = %t, %q", c.tool, c.targets, got, hint)
		}
	}
}

func TestAccountsListReadsBatchesWithCursorAndSendsTheDocumentedParameters(t *testing.T) {
	h := newHarness(t)
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		if req.URL.Query().Get("pageToken") == "batch-two" {
			return 200, `{"accounts":[` + accountRow("2", "Second") + `]}`, nil
		}
		return 200, `{"accounts":[{"name":"accounts/1","accountId":"1","accountName":"First Store","testAccount":true,` +
			`"adultContent":true,"languageCode":"en-US","homePageUri":"https://shop.example.invalid/",` +
			`"timeZone":{"id":"Europe/Zurich","version":"2025a"},"unknownField":"never held"}],"nextPageToken":"batch-two"}`, nil
	}
	first, err := h.call(t, handleAccountsList, `{"filter":"accountName = \"*first*\"","page_size":2}`)
	if err != nil {
		t.Fatal(err)
	}
	rows := first["accounts"].([]any)
	row := rows[0].(map[string]any)
	if len(rows) != 1 || row["name"] != "accounts/1" || row["accountId"] != "1" || row["accountName"] != "First Store" ||
		row["testAccount"] != true || row["adultContent"] != true || row["languageCode"] != "en-US" ||
		row["homePageUri"] != "https://shop.example.invalid/" || row["unknownField"] != nil ||
		row["timeZone"].(map[string]any)["id"] != "Europe/Zurich" || row["timeZone"].(map[string]any)["version"] != "2025a" {
		t.Fatalf("first batch = %v", first)
	}
	cursor, _ := first["next_cursor"].(string)
	if cursor == "" || strings.Contains(cursor, "batch-two") && cursor == "batch-two" {
		t.Fatalf("next_cursor = %q, want a wrapped cursor", cursor)
	}
	second, err := h.call(t, handleAccountsList, `{"filter":"accountName = \"*first*\"","page_size":2,"cursor":"`+cursor+`"}`)
	if err != nil || second["next_cursor"] != nil || len(second["accounts"].([]any)) != 1 {
		t.Fatalf("second batch = %v, %v", second, err)
	}
	paths := h.paths(t)
	if len(paths) != 2 || !strings.HasPrefix(paths[0], "/accounts/v1/accounts?") ||
		!strings.Contains(paths[0], "filter=accountName+%3D+%22%2Afirst%2A%22") || !strings.Contains(paths[0], "pageSize=2") ||
		!strings.Contains(paths[1], "pageToken=batch-two") {
		t.Errorf("paths = %v", paths)
	}
	// A cursor belongs to the arguments it came from.
	if _, err := h.call(t, handleAccountsList, `{"cursor":"`+cursor+`"}`); !isInvalidRequest(err) {
		t.Errorf("a cursor of other arguments gave %v", err)
	}
	if _, err := h.call(t, handleAccountsList, `{"cursor":"not-a-cursor"}`); !isInvalidRequest(err) {
		t.Errorf("a foreign cursor gave %v", err)
	}
	if len(h.google.seen()) != 2 {
		t.Errorf("a refused cursor reached Google: %v", h.paths(t))
	}
}

func TestAccountsListRefusesAFilterWithControlCharactersBeforeAnyRequest(t *testing.T) {
	h := newHarness(t)
	if _, err := h.call(t, handleAccountsList, `{"filter":"accountName = \"a\nb\""}`); !isInvalidRequest(err) {
		t.Fatalf("err = %v", err)
	}
	h.untouched(t)
}

func TestAccountsListWithoutTargetsReturnsEveryRow(t *testing.T) {
	h := newHarness(t)
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"accounts":[` + accountRow("1", "A") + `,{"accountName":"No ID"}]}`, nil
	}
	out, err := h.call(t, handleAccountsList, `{}`)
	if err != nil || len(out["accounts"].([]any)) != 2 {
		t.Fatalf("out = %v, %v", out, err)
	}
}

func TestAccountsListFiltersEveryRowAgainstAccountTargetsWithoutProofReads(t *testing.T) {
	h := newHarness(t, "accounts/1", "accounts/3")
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"accounts":[` + accountRow("1", "Mine") + `,` + accountRow("2", "Foreign") + `,` +
			accountRow("3", "Also mine") + `,{"name":"accounts/4","accountId":"5"},{"accountName":"No ID"},` +
			`{"name":"accounts/01"}],"nextPageToken":"more"}`, nil
	}
	out, err := h.call(t, handleAccountsList, `{}`)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, row := range out["accounts"].([]any) {
		ids = append(ids, row.(map[string]any)["accountId"].(string))
	}
	if !slices.Equal(ids, []string{"1", "3"}) {
		t.Errorf("ids = %v, want only the bound accounts", ids)
	}
	if out["next_cursor"] == nil {
		t.Error("a filtered batch lost its cursor")
	}
	if paths := h.paths(t); len(paths) != 1 {
		t.Errorf("paths = %v, want only the list", paths)
	}
}

func TestAccountsListKeepsTheAdvancedAccountAndItsSubAccounts(t *testing.T) {
	h := newHarness(t, "advanced-accounts/10", "accounts/50")
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		switch req.URL.Path {
		case "/accounts/v1/accounts":
			return 200, `{"accounts":[` + accountRow("10", "Advanced") + `,` + accountRow("11", "Sub one") + `,` +
				accountRow("12", "Sub two") + `,` + accountRow("99", "Foreign") + `,` + accountRow("50", "Named") + `]}`, nil
		case "/accounts/v1/accounts/10:listSubaccounts":
			if req.URL.Query().Get("pageToken") == "p2" {
				return 200, `{"accounts":[` + accountRow("12", "Sub two") + `]}`, nil
			}
			return 200, `{"accounts":[` + accountRow("11", "Sub one") + `],"nextPageToken":"p2"}`, nil
		}
		t.Errorf("unexpected request %s", req.URL)
		return 404, `{}`, nil
	}
	out, err := h.call(t, handleAccountsList, `{}`)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, row := range out["accounts"].([]any) {
		ids = append(ids, row.(map[string]any)["accountId"].(string))
	}
	if !slices.Equal(ids, []string{"10", "11", "12", "50"}) {
		t.Errorf("ids = %v", ids)
	}
	paths := h.paths(t)
	if len(paths) != 3 || !strings.Contains(paths[1], "pageSize=500") || !strings.Contains(paths[2], "pageToken=p2") {
		t.Errorf("paths = %v, want the list and two sub-account pages", paths)
	}
}

func TestAccountsListReadsNoSubAccountsWhenEveryRowIsNamed(t *testing.T) {
	h := newHarness(t, "advanced-accounts/10")
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"accounts":[` + accountRow("10", "Advanced") + `]}`, nil
	}
	if _, err := h.call(t, handleAccountsList, `{}`); err != nil {
		t.Fatal(err)
	}
	if paths := h.paths(t); len(paths) != 1 {
		t.Errorf("paths = %v", paths)
	}
}

func TestAccountsListFailsClosedWhenTheSubAccountsCannotBeChecked(t *testing.T) {
	h := newHarness(t, "advanced-accounts/10")
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		if strings.HasSuffix(req.URL.Path, ":listSubaccounts") {
			return 200, `{"accounts":[` + accountRow("11", "Sub") + `],"nextPageToken":"again"}`, nil
		}
		return 200, `{"accounts":[` + accountRow("12", "Unproven") + `]}`, nil
	}
	_, err := h.call(t, handleAccountsList, `{}`)
	if classOf(err) != provider.ClassProviderError || !strings.Contains(err.Error(), "more sub-accounts") {
		t.Fatalf("err = %v", err)
	}
	if got := len(h.google.seen()); got != 1+maxSubaccountPages {
		t.Errorf("requests = %d, want the list and %d pages", got, maxSubaccountPages)
	}
}

func TestAccountsListRefusesMoreRowsThanTheBound(t *testing.T) {
	h := newHarness(t)
	rows := make([]string, maxEntries+1)
	for i := range rows {
		rows[i] = `{"name":"accounts/1"}`
	}
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"accounts":[` + strings.Join(rows, ",") + `]}`, nil
	}
	if _, err := h.call(t, handleAccountsList, `{}`); classOf(err) != provider.ClassInvalidResponse {
		t.Fatalf("err = %v", err)
	}
}

func TestAccountsGetChecksTheAccountBeforeSecretAndRequest(t *testing.T) {
	for name, c := range map[string]struct {
		targets []string
		args    string
	}{
		"not an ID":                  {nil, `{"account":"accounts/1"}`},
		"leading zero":               {nil, `{"account":"01"}`},
		"path injection":             {nil, `{"account":"1/users"}`},
		"query injection":            {nil, `{"account":"1?x=y"}`},
		"colon method injection":     {nil, `{"account":"1:delete"}`},
		"outside an account target":  {[]string{"accounts/1"}, `{"account":"2"}`},
		"no argument, no target":     {nil, `{}`},
		"no argument, two targets":   {[]string{"accounts/1", "accounts/2"}, `{}`},
		"no argument, advanced pair": {[]string{"advanced-accounts/1", "accounts/2"}, `{}`},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, c.targets...)
			if _, err := h.call(t, handleAccountsGet, c.args); !isInvalidRequest(err) {
				t.Fatalf("err = %v, want an invalid request", err)
			}
			h.untouched(t)
		})
	}
}

func TestAccountsGetReadsANamedAccountWithoutProofRead(t *testing.T) {
	for _, targets := range [][]string{nil, {"accounts/7"}, {"advanced-accounts/7"}, {"accounts/3", "accounts/7"}} {
		h := newHarness(t, targets...)
		h.google.api = func(*http.Request) (int, string, http.Header) {
			return 200, accountRow("7", "Seven"), nil
		}
		out, err := h.call(t, handleAccountsGet, `{"account":"7"}`)
		if err != nil || out["accountName"] != "Seven" {
			t.Fatalf("targets %v: %v, %v", targets, out, err)
		}
		if paths := h.paths(t); len(paths) != 1 || paths[0] != "/accounts/v1/accounts/7" {
			t.Errorf("targets %v: paths = %v", targets, paths)
		}
	}
}

func TestAccountsGetUsesTheOnlyTargetWhenTheArgumentIsLeftOut(t *testing.T) {
	for _, target := range []string{"accounts/7", "advanced-accounts/7"} {
		h := newHarness(t, target)
		h.google.api = func(*http.Request) (int, string, http.Header) { return 200, accountRow("7", "Seven"), nil }
		if _, err := h.call(t, handleAccountsGet, `{}`); err != nil {
			t.Fatal(err)
		}
		if paths := h.paths(t); len(paths) != 1 || paths[0] != "/accounts/v1/accounts/7" {
			t.Errorf("%s: paths = %v", target, paths)
		}
	}
}

func TestAccountsGetProvesASubAccountBeforeTheRead(t *testing.T) {
	h := newHarness(t, "advanced-accounts/10", "advanced-accounts/20")
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		switch req.URL.Path {
		case "/accounts/v1/accounts/10:listSubaccounts":
			return 200, `{"accounts":[` + accountRow("11", "Sub") + `]}`, nil
		case "/accounts/v1/accounts/20:listSubaccounts":
			return 200, `{"accounts":[` + accountRow("21", "Other sub") + `]}`, nil
		case "/accounts/v1/accounts/21":
			return 200, accountRow("21", "Other sub"), nil
		}
		t.Errorf("unexpected request %s", req.URL)
		return 404, `{}`, nil
	}
	if _, err := h.call(t, handleAccountsGet, `{"account":"21"}`); err != nil {
		t.Fatal(err)
	}
	want := []string{"/accounts/v1/accounts/10:listSubaccounts?pageSize=500", "/accounts/v1/accounts/20:listSubaccounts?pageSize=500",
		"/accounts/v1/accounts/21"}
	if paths := h.paths(t); !slices.Equal(paths, want) {
		t.Errorf("paths = %v, want %v", paths, want)
	}
}

func TestAccountsGetRefusesAnAccountThatIsNoSubAccountWithoutNamingIt(t *testing.T) {
	h := newHarness(t, "advanced-accounts/10")
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"accounts":[` + accountRow("11", "Sub") + `]}`, nil
	}
	_, err := h.call(t, handleAccountsGet, `{"account":"4242"}`)
	if !isInvalidRequest(err) || strings.Contains(err.Error(), "4242") {
		t.Fatalf("err = %v", err)
	}
	if paths := h.paths(t); len(paths) != 1 || !strings.HasSuffix(paths[0], ":listSubaccounts?pageSize=500") {
		t.Errorf("paths = %v, want only the proof read", paths)
	}
}

func TestAccountsGetDoesNotTrustAnAdvancedTargetForANestedSubAccount(t *testing.T) {
	// Only direct sub-accounts are proven: an account below a sub-account needs its own target.
	h := newHarness(t, "advanced-accounts/10")
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		return 200, `{"accounts":[` + accountRow("11", "Sub") + `]}`, nil
	}
	if _, err := h.call(t, handleAccountsGet, `{"account":"111"}`); !isInvalidRequest(err) {
		t.Fatalf("err = %v", err)
	}
}

func TestDeveloperRegistrationsGet(t *testing.T) {
	registration := `{"name":"accounts/10/developerRegistration","gcpIds":["project-one","project-two"]}`
	t.Run("unbound connection takes any account", func(t *testing.T) {
		h := newHarness(t)
		h.google.api = func(*http.Request) (int, string, http.Header) { return 200, registration, nil }
		out, err := h.call(t, handleDeveloperRegistrationsGet, `{"account":"10"}`)
		if err != nil || out["name"] != "accounts/10/developerRegistration" || len(out["gcpIds"].([]any)) != 2 {
			t.Fatalf("out = %v, %v", out, err)
		}
		if paths := h.paths(t); len(paths) != 1 || paths[0] != "/accounts/v1/accounts/10/developerRegistration" {
			t.Errorf("paths = %v", paths)
		}
		if _, err := h.call(t, handleDeveloperRegistrationsGet, `{}`); !isInvalidRequest(err) {
			t.Errorf("no account gave %v", err)
		}
	})
	t.Run("a bound connection needs the advanced account target", func(t *testing.T) {
		for _, c := range []struct {
			targets []string
			args    string
			ok      bool
		}{
			{[]string{"advanced-accounts/10"}, `{"account":"10"}`, true},
			{[]string{"advanced-accounts/10"}, `{}`, true},
			{[]string{"advanced-accounts/10", "accounts/3"}, `{}`, true},
			{[]string{"advanced-accounts/10"}, `{"account":"11"}`, false},
			{[]string{"accounts/10"}, `{"account":"10"}`, false},
			{[]string{"accounts/10"}, `{}`, false},
			{[]string{"advanced-accounts/10", "advanced-accounts/20"}, `{}`, false},
			{[]string{"advanced-accounts/10"}, `{"account":"10/x"}`, false},
		} {
			h := newHarness(t, c.targets...)
			h.google.api = func(*http.Request) (int, string, http.Header) { return 200, registration, nil }
			_, err := h.call(t, handleDeveloperRegistrationsGet, c.args)
			if c.ok != (err == nil) || (err != nil && !isInvalidRequest(err)) {
				t.Errorf("targets %v, args %s: err = %v", c.targets, c.args, err)
			}
			if !c.ok {
				h.untouched(t)
			}
		}
	})
}

func TestEveryFailureOfTheTargetsAndTheConnectionEndsBeforeSecretAndRequest(t *testing.T) {
	for name, mutate := range map[string]func(*config.Resolved){
		"invalid target": func(r *config.Resolved) { r.Targets = []string{"accounts/x"} },
		"folder target":  func(r *config.Resolved) { r.Targets = []string{"folders/1"} },
		"leading zero":   func(r *config.Resolved) { r.Targets = []string{"advanced-accounts/01"} },
		"padded form":    func(r *config.Resolved) { r.Targets = []string{"accounts/1/"} },
		"exclusion":      func(r *config.Resolved) { r.Exclude = []string{"accounts/2"} },
		"other base URL": func(r *config.Resolved) { r.BaseURL = "https://example.invalid" },
		"http base URL":  func(r *config.Resolved) { r.BaseURL = "http://merchantapi.googleapis.com" },
		"too many targets": func(r *config.Resolved) {
			r.Targets = nil
			for i := 1; i <= maxTargets+1; i++ {
				r.Targets = append(r.Targets, "accounts/"+strconv.Itoa(i))
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			mutate(h.resolved)
			for handler, args := range map[string]struct {
				h    capability.Handler
				body string
			}{
				"list": {handleAccountsList, `{}`}, "get": {handleAccountsGet, `{"account":"1"}`},
				"registration": {handleDeveloperRegistrationsGet, `{"account":"1"}`},
			} {
				if _, err := h.call(t, args.h, args.body); classOf(err) != provider.ClassProviderError {
					t.Errorf("%s: err = %v, want provider-error", handler, err)
				}
			}
			h.untouched(t)
			if class, err := TestConnection(t.Context(), h.resolved, h.secrets, h.red); err != nil ||
				class != provider.ClassProviderError {
				t.Errorf("connection test = %q, %v", class, err)
			}
			h.untouched(t)
		})
	}
}

func TestTextsAreBoundedAndAnAccountWithTwoSpellingsIsDropped(t *testing.T) {
	long := strings.Repeat("n", 400)
	h := newHarness(t, "accounts/1", "accounts/2")
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"accounts":[{"name":"accounts/1","accountName":"` + long + `","homePageUri":"https://x.example.invalid/` +
			strings.Repeat("p", 3000) + `","languageCode":5},{"name":"accounts/2","accountId":"3"}]}`, nil
	}
	out, err := h.call(t, handleAccountsList, `{}`)
	if err != nil {
		t.Fatal(err)
	}
	rows := out["accounts"].([]any)
	if len(rows) != 1 {
		t.Fatalf("rows = %v, want the account with two disagreeing IDs dropped", rows)
	}
	row := rows[0].(map[string]any)
	if got := []rune(row["accountName"].(string)); len(got) != maxNameRunes || got[len(got)-1] != '…' {
		t.Errorf("accountName has %d runes", len(got))
	}
	if got := []rune(row["homePageUri"].(string)); len(got) != maxURLRunes {
		t.Errorf("homePageUri has %d runes", len(got))
	}
	if row["languageCode"] != nil {
		t.Errorf("a number as languageCode = %v, want it dropped", row["languageCode"])
	}
}

func TestGcpIDsAreBounded(t *testing.T) {
	h := newHarness(t)
	ids := make([]string, 150)
	for i := range ids {
		ids[i] = `"project-` + strconv.Itoa(i) + `"`
	}
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"name":"accounts/1/developerRegistration","gcpIds":[` + strings.Join(ids, ",") + `]}`, nil
	}
	out, err := h.call(t, handleDeveloperRegistrationsGet, `{"account":"1"}`)
	if err != nil || len(out["gcpIds"].([]any)) != maxListItems {
		t.Fatalf("out = %v, %v", out, err)
	}
}

func TestEveryToolCarriesExamplesFieldsAndAnUntrustedContentNote(t *testing.T) {
	for _, d := range descriptors() {
		if len(d.Examples) == 0 || len(d.Fields) == 0 {
			t.Errorf("%s has no example or no fields", d.ID)
		}
		if !strings.Contains(d.Description, "untrusted") && d.ID != developerRegistrationsGetID &&
			!slices.Contains(flagOnlyIDs, d.ID) {
			t.Errorf("%s does not warn about untrusted content", d.ID)
		}
	}
}
