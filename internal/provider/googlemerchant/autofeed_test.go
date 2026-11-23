package googlemerchant

import (
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

var (
	listedOnlyIDs = []string{autofeedSettingsUpdateID, automaticImprovementsUpdateID}
	// flagOnlyIDs are the tools whose answers hold resource names and on/off flags, so no text of the account.
	flagOnlyIDs = []string{autofeedSettingsGetID, autofeedSettingsUpdateID, automaticImprovementsGetID,
		automaticImprovementsUpdateID}
)

// sent is one request as the fake Google saw it.
type settingsRequest struct{ method, path, query, contentType, body string }

func (h *harness) settingsRequests(t *testing.T) []settingsRequest {
	t.Helper()
	var out []settingsRequest
	for _, req := range h.google.seen() {
		if req.URL.Host != "merchantapi.googleapis.com" || req.URL.Scheme != "https" {
			t.Errorf("request left the fixed origin: %s", req.URL.Host)
		}
		if req.Header.Get("Authorization") != "Bearer "+testAccess {
			t.Errorf("request %s carries no bearer token", req.URL.Path)
		}
		body := ""
		if req.Body != nil {
			raw, _ := io.ReadAll(req.Body)
			body = string(raw)
		}
		out = append(out, settingsRequest{req.Method, req.URL.Path, req.URL.RawQuery, req.Header.Get("Content-Type"), body})
	}
	return out
}

func TestUpdatesAreListedOnlyConfirmedAndOutsideTheReadProfile(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	for _, id := range listedOnlyIDs {
		d, _, ok := reg.Lookup(id)
		if !ok || !d.RequiresToolAllowList || d.Version != 1 || d.Risk.Effect != capability.EffectUpdate ||
			d.Risk.Confirmation != capability.ConfirmationRequired || !d.Risk.OpenWorld {
			t.Errorf("%s = %+v", id, d)
		}
		if slices.Contains(metadata.Profiles[0].Tools, id) {
			t.Errorf("the read profile holds %s", id)
		}
		var found bool
		for _, tool := range metadata.Tools {
			if tool.ID == id {
				found = tool.RequiresToolAllowList && tool.Effect == config.EffectUpdate
			}
		}
		if !found {
			t.Errorf("%s is not a listed-only update tool", id)
		}
	}
	for _, id := range []string{autofeedSettingsGetID, automaticImprovementsGetID} {
		if !slices.Contains(metadata.Profiles[0].Tools, id) {
			t.Errorf("the read profile lacks %s", id)
		}
	}
}

func TestUpdateDescriptionsNamePublicEffectAcceptanceAndDelay(t *testing.T) {
	for id, words := range map[string][]string{
		autofeedSettingsUpdateID: {"crawls", "automatically", "public", "Only the acceptance", "delay"},
		automaticImprovementsUpdateID: {"price", "availability", "images", "delivery times", "public offers",
			"Only the acceptance", "delay", "replaces that whole setting"},
	} {
		var d capability.Descriptor
		for _, candidate := range descriptors() {
			if candidate.ID == id {
				d = candidate
			}
		}
		for _, word := range words {
			if !strings.Contains(strings.ToLower(d.Description), strings.ToLower(word)) {
				t.Errorf("%s: description lacks %q: %s", id, word, d.Description)
			}
		}
	}
}

func TestAutofeedSettingsGetShowsFalseAndDropsUnknownMembers(t *testing.T) {
	h := newHarness(t)
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"name":"accounts/7/autofeedSettings","enableProducts":false,"eligible":true,"extra":"never held"}`, nil
	}
	out, err := h.call(t, handleAutofeedSettingsGet, `{"account":"7"}`)
	if err != nil || out["enableProducts"] != false || out["eligible"] != true || out["extra"] != nil ||
		out["name"] != "accounts/7/autofeedSettings" {
		t.Fatalf("out = %v, %v", out, err)
	}
	if got := h.settingsRequests(t); len(got) != 1 || got[0].method != "GET" || got[0].path != "/accounts/v1/accounts/7/autofeedSettings" {
		t.Errorf("sent = %+v", got)
	}
}

func TestAutomaticImprovementsGetShowsEveryDocumentedMember(t *testing.T) {
	h := newHarness(t)
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"name":"accounts/7/automaticImprovements","itemUpdates":{"accountItemUpdatesSettings":` +
			`{"allowPriceUpdates":true,"allowAvailabilityUpdates":false,"allowStrictAvailabilityUpdates":false,` +
			`"allowConditionUpdates":true},"effectiveAllowAvailabilityUpdates":false,"effectiveAllowPriceUpdates":true,` +
			`"effectiveAllowConditionUpdates":true,"effectiveAllowStrictAvailabilityUpdates":false},` +
			`"imageImprovements":{"accountImageImprovementsSettings":{"allowAutomaticImageImprovements":false},` +
			`"effectiveAllowAutomaticImageImprovements":false},"shippingImprovements":{"allowShippingImprovements":true},` +
			`"unknown":1}`, nil
	}
	out, err := h.call(t, handleAutomaticImprovementsGet, `{"account":"7"}`)
	if err != nil {
		t.Fatal(err)
	}
	item := out["itemUpdates"].(map[string]any)
	settings := item["accountItemUpdatesSettings"].(map[string]any)
	image := out["imageImprovements"].(map[string]any)
	if settings["allowPriceUpdates"] != true || settings["allowAvailabilityUpdates"] != false ||
		settings["allowStrictAvailabilityUpdates"] != false || settings["allowConditionUpdates"] != true ||
		item["effectiveAllowPriceUpdates"] != true || item["effectiveAllowAvailabilityUpdates"] != false ||
		item["effectiveAllowConditionUpdates"] != true || item["effectiveAllowStrictAvailabilityUpdates"] != false ||
		image["accountImageImprovementsSettings"].(map[string]any)["allowAutomaticImageImprovements"] != false ||
		image["effectiveAllowAutomaticImageImprovements"] != false ||
		out["shippingImprovements"].(map[string]any)["allowShippingImprovements"] != true || out["unknown"] != nil {
		t.Fatalf("out = %v", out)
	}
	if got := h.settingsRequests(t); len(got) != 1 || got[0].path != "/accounts/v1/accounts/7/automaticImprovements" {
		t.Errorf("sent = %+v", got)
	}
}

func TestAutofeedSettingsUpdateSendsOnePatchWithMaskAndReportsAcceptance(t *testing.T) {
	h := newHarness(t)
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"name":"accounts/7/autofeedSettings","enableProducts":true}`, nil
	}
	out, err := h.call(t, handleAutofeedSettingsUpdate, `{"account":"7","enable_products":false}`)
	if err != nil || out["accepted"] != true || out["name"] != "accounts/7/autofeedSettings" ||
		len(out["updated"].([]any)) != 1 || out["updated"].([]any)[0] != "enableProducts" || out["enableProducts"] != nil {
		t.Fatalf("out = %v, %v", out, err)
	}
	got := h.settingsRequests(t)
	if len(got) != 1 || got[0].method != http.MethodPatch || got[0].path != "/accounts/v1/accounts/7/autofeedSettings" ||
		got[0].query != "updateMask=enableProducts" || got[0].contentType != "application/json" ||
		got[0].body != `{"enableProducts":false}` {
		t.Errorf("sent = %+v", got)
	}
}

func TestAutomaticImprovementsUpdateMasksExactlyTheGivenMembers(t *testing.T) {
	for name, c := range map[string]struct{ args, query, body string }{
		"image only": {`{"account":"7","image_improvements":false}`, "updateMask=imageImprovements",
			`{"imageImprovements":{"accountImageImprovementsSettings":{"allowAutomaticImageImprovements":false}}}`},
		"shipping only": {`{"account":"7","shipping_improvements":true}`, "updateMask=shippingImprovements",
			`{"shippingImprovements":{"allowShippingImprovements":true}}`},
		"item updates": {`{"account":"7","item_updates":{"allow_price_updates":true,"allow_condition_updates":false}}`,
			"updateMask=itemUpdates",
			`{"itemUpdates":{"accountItemUpdatesSettings":{"allowPriceUpdates":true,"allowConditionUpdates":false}}}`},
		"all": {`{"account":"7","item_updates":{"allow_availability_updates":true,"allow_strict_availability_updates":true},` +
			`"image_improvements":true,"shipping_improvements":false}`,
			"updateMask=itemUpdates%2CimageImprovements%2CshippingImprovements",
			`{"itemUpdates":{"accountItemUpdatesSettings":{"allowAvailabilityUpdates":true,` +
				`"allowStrictAvailabilityUpdates":true}},"shippingImprovements":{"allowShippingImprovements":false},` +
				`"imageImprovements":{"accountImageImprovementsSettings":{"allowAutomaticImageImprovements":true}}}`},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.google.api = func(*http.Request) (int, string, http.Header) { return 200, `{}`, nil }
			out, err := h.call(t, handleAutomaticImprovementsUpdate, c.args)
			if err != nil || out["accepted"] != true || out["name"] != "accounts/7/automaticImprovements" {
				t.Fatalf("out = %v, %v", out, err)
			}
			got := h.settingsRequests(t)
			if len(got) != 1 || got[0].method != http.MethodPatch || got[0].path != "/accounts/v1/accounts/7/automaticImprovements" ||
				got[0].query != c.query || got[0].body != c.body {
				t.Errorf("sent = %+v, want query %s body %s", got, c.query, c.body)
			}
		})
	}
}

func TestUpdatesRefuseBeforeSecretAndRequest(t *testing.T) {
	for name, c := range map[string]struct {
		handler capability.Handler
		targets []string
		args    string
	}{
		"autofeed without flag":     {handleAutofeedSettingsUpdate, nil, `{"account":"7"}`},
		"autofeed outside targets":  {handleAutofeedSettingsUpdate, []string{"accounts/1"}, `{"account":"7","enable_products":true}`},
		"autofeed path injection":   {handleAutofeedSettingsUpdate, nil, `{"account":"7/x","enable_products":true}`},
		"autofeed no account":       {handleAutofeedSettingsUpdate, nil, `{"enable_products":true}`},
		"improvements nothing":      {handleAutomaticImprovementsUpdate, nil, `{"account":"7"}`},
		"improvements empty item":   {handleAutomaticImprovementsUpdate, nil, `{"account":"7","item_updates":{}}`},
		"improvements outside":      {handleAutomaticImprovementsUpdate, []string{"accounts/1"}, `{"account":"7","image_improvements":true}`},
		"improvements mask smuggle": {handleAutomaticImprovementsUpdate, nil, `{"account":"7:x","image_improvements":true}`},
		"autofeed get outside":      {handleAutofeedSettingsGet, []string{"accounts/1"}, `{"account":"7"}`},
		"improvements get outside":  {handleAutomaticImprovementsGet, []string{"accounts/1"}, `{"account":"7"}`},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, c.targets...)
			if _, err := h.call(t, c.handler, c.args); !isInvalidRequest(err) {
				t.Fatalf("err = %v, want an invalid request", err)
			}
			h.untouched(t)
		})
	}
}

func TestUpdateOfASubAccountIsProvenBeforeThePatch(t *testing.T) {
	h := newHarness(t, "advanced-accounts/1")
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		if strings.HasSuffix(req.URL.Path, ":listSubaccounts") {
			return 200, `{"accounts":[` + accountRow("7", "Seven") + `]}`, nil
		}
		return 200, `{}`, nil
	}
	if _, err := h.call(t, handleAutofeedSettingsUpdate, `{"account":"7","enable_products":true}`); err != nil {
		t.Fatal(err)
	}
	got := h.settingsRequests(t)
	if len(got) != 2 || got[0].method != "GET" || !strings.HasSuffix(got[0].path, ":listSubaccounts") ||
		got[1].method != http.MethodPatch {
		t.Fatalf("sent = %+v", got)
	}
	h = newHarness(t, "advanced-accounts/1")
	h.google.api = func(req *http.Request) (int, string, http.Header) { return 200, `{"accounts":[]}`, nil }
	if _, err := h.call(t, handleAutofeedSettingsUpdate, `{"account":"8","enable_products":true}`); !isInvalidRequest(err) {
		t.Fatalf("err = %v", err)
	}
	for _, r := range h.settingsRequests(t) {
		if r.method != "GET" {
			t.Errorf("a %s went out for an unproven account", r.method)
		}
	}
}

func TestMutationsAreNeverRepeatedAndFailuresAreMapped(t *testing.T) {
	for name, c := range map[string]struct {
		status int
		body   string
		class  provider.Class
	}{
		"401": {401, `{"error":{"code":401,"status":"UNAUTHENTICATED"}}`, provider.ClassAuth},
		"403": {403, googleErrorBody, provider.ClassPermission},
		"404": {404, `{"error":{"code":404,"status":"NOT_FOUND"}}`, provider.ClassNotFound},
		"429": {429, quotaBody(reasonDayQuota), provider.ClassRateLimited},
		"500": {500, `{}`, provider.ClassProviderError},
		"503": {503, `{}`, provider.ClassUnreachable},
		"504": {504, `{}`, provider.ClassTimeout},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.google.api = func(*http.Request) (int, string, http.Header) { return c.status, c.body, nil }
			_, err := h.call(t, handleAutofeedSettingsUpdate, `{"account":"7","enable_products":true}`)
			if classOf(err) != c.class {
				t.Fatalf("err = %v, want class %s", err, c.class)
			}
			if n := len(h.google.seen()); n != 1 {
				t.Errorf("%d requests, want exactly one", n)
			}
			if strings.Contains(err.Error(), "synthetic provider echo") {
				t.Errorf("message carries Google's text: %v", err)
			}
		})
	}
}

func TestMutationAfter401DropsTheTokenAndTheNextCallRenewsIt(t *testing.T) {
	h := newHarness(t)
	status := 401
	h.google.api = func(*http.Request) (int, string, http.Header) { return status, `{}`, nil }
	if _, err := h.call(t, handleAutofeedSettingsUpdate, `{"account":"7","enable_products":true}`); classOf(err) != provider.ClassAuth {
		t.Fatalf("err = %v", err)
	}
	if h.google.grants != 1 {
		t.Fatalf("grants = %d after the 401", h.google.grants)
	}
	status = 200
	if _, err := h.call(t, handleAutofeedSettingsUpdate, `{"account":"7","enable_products":true}`); err != nil {
		t.Fatal(err)
	}
	if h.google.grants != 2 || len(h.google.seen()) != 2 {
		t.Errorf("grants = %d, requests = %d", h.google.grants, len(h.google.seen()))
	}
}

func TestRateLimitedMutationHoldsTheNextRequest(t *testing.T) {
	h := newHarness(t)
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 429, quotaBody(reasonMinuteQuota), http.Header{"Retry-After": {"3"}}
	}
	_, _ = h.call(t, handleAutofeedSettingsUpdate, `{"account":"7","enable_products":true}`)
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, `{}`, nil }
	if _, err := h.call(t, handleAutofeedSettingsGet, `{"account":"7"}`); err != nil {
		t.Fatal(err)
	}
	if waits := h.spacing.recorded(); len(waits) == 0 || waits[len(waits)-1] < holdMin {
		t.Errorf("waits = %v, want a hold of at least %s", waits, holdMin)
	}
}

func TestConfirmationSummariesNameTheChangeAndItsPublicEffect(t *testing.T) {
	got := autofeedSummary([]byte(`{"account":"7","enable_products":true}`))
	for _, want := range []string{"autofeed", "7", "on", "automatically", "publicly", "delay"} {
		if !strings.Contains(got, want) {
			t.Errorf("autofeed summary %q lacks %q", got, want)
		}
	}
	got = improvementsSummary([]byte(`{"account":"7","item_updates":{"allow_price_updates":false},"image_improvements":true}`))
	for _, want := range []string{"price updates off", "image improvements on", "images", "publicly", "delay"} {
		if !strings.Contains(got, want) {
			t.Errorf("improvements summary %q lacks %q", got, want)
		}
	}
}
