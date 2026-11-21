package googlemerchant

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const termsAnswer = `{"name":"termsOfService/VERSION_1","kind":"MERCHANT_CENTER","regionCode":"US","external":true,` +
	`"fileUri":"https://terms.example.invalid/v1","unknownField":"never held"}`

const stateAnswer = `{"name":"accounts/7/termsOfServiceAgreementStates/MERCHANT_CENTER-US","regionCode":"US",` +
	`"termsOfServiceKind":"MERCHANT_CENTER","required":{"termsOfService":"termsOfService/VERSION_2",` +
	`"tosFileUri":"https://terms.example.invalid/v2","extra":1},"accepted":{"termsOfService":"termsOfService/VERSION_1",` +
	`"acceptedBy":"accounts/7","validUntil":{"year":2027,"month":3,"day":1}},"unknownField":"never held"}`

func TestTermsOfServiceGetReadsOneVersionWithoutAnAccount(t *testing.T) {
	// Global documents: the same call works with no target and with any binding.
	for _, targets := range [][]string{nil, {"accounts/7"}, {"advanced-accounts/7"}} {
		h := newHarness(t, targets...)
		h.google.api = func(*http.Request) (int, string, http.Header) { return 200, termsAnswer, nil }
		out, err := h.call(t, handleTermsOfServiceGet, `{"version":"VERSION_1"}`)
		if err != nil || out["name"] != "termsOfService/VERSION_1" || out["kind"] != "MERCHANT_CENTER" ||
			out["regionCode"] != "US" || out["external"] != true || out["fileUri"] != "https://terms.example.invalid/v1" ||
			out["unknownField"] != nil {
			t.Fatalf("targets %v: out = %v, %v", targets, out, err)
		}
		if paths := h.paths(t); !slices.Equal(paths, []string{"/accounts/v1/termsOfService/VERSION_1"}) {
			t.Errorf("paths = %v", paths)
		}
	}
}

func TestTermsOfServiceRetrieveLatestSendsKindAndRegion(t *testing.T) {
	h := newHarness(t, "accounts/7")
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, termsAnswer, nil }
	out, err := h.call(t, handleTermsOfServiceRetrieveLatest, `{"kind":"MERCHANT_CENTER","region_code":"001"}`)
	if err != nil || out["name"] != "termsOfService/VERSION_1" {
		t.Fatalf("out = %v, %v", out, err)
	}
	want := []string{"/accounts/v1/termsOfService:retrieveLatest?kind=MERCHANT_CENTER&regionCode=001"}
	if paths := h.paths(t); !slices.Equal(paths, want) {
		t.Errorf("paths = %v, want %v", paths, want)
	}
}

func TestTermsArgumentsAreCheckedBeforeSecretAndRequest(t *testing.T) {
	bad := map[string]struct {
		handler capability.Handler
		args    string
	}{
		"version with slash":      {handleTermsOfServiceGet, `{"version":"a/b"}`},
		"version with colon":      {handleTermsOfServiceGet, `{"version":"x:accept"}`},
		"version with query":      {handleTermsOfServiceGet, `{"version":"x?y=z"}`},
		"version dot dot":         {handleTermsOfServiceGet, `{"version":".."}`},
		"version empty":           {handleTermsOfServiceGet, `{"version":""}`},
		"version too long":        {handleTermsOfServiceGet, `{"version":"` + strings.Repeat("a", 65) + `"}`},
		"unused kind":             {handleTermsOfServiceRetrieveLatest, `{"kind":"TERMS_OF_SERVICE_KIND_UNSPECIFIED","region_code":"US"}`},
		"kind with query":         {handleTermsOfServiceRetrieveLatest, `{"kind":"MERCHANT_CENTER&x=1","region_code":"US"}`},
		"lowercase region":        {handleTermsOfServiceRetrieveLatest, `{"kind":"MERCHANT_CENTER","region_code":"us"}`},
		"region with query":       {handleTermsOfServiceRetrieveLatest, `{"kind":"MERCHANT_CENTER","region_code":"US&x=1"}`},
		"missing region":          {handleTermsOfServiceRetrieveLatest, `{"kind":"MERCHANT_CENTER"}`},
		"identifier path":         {handleAgreementStatesGet, `{"account":"7","identifier":"MERCHANT_CENTER-US/x"}`},
		"identifier other kind":   {handleAgreementStatesGet, `{"account":"7","identifier":"OTHER-US"}`},
		"identifier colon":        {handleAgreementStatesGet, `{"account":"7","identifier":"MERCHANT_CENTER-US:x"}`},
		"identifier missing":      {handleAgreementStatesGet, `{"account":"7"}`},
		"identifier lowercase":    {handleAgreementStatesGet, `{"account":"7","identifier":"MERCHANT_CENTER-us"}`},
		"state account not ID":    {handleAgreementStatesGet, `{"account":"7/x","identifier":"MERCHANT_CENTER-US"}`},
		"application not ID":      {handleAgreementStatesRetrieveForApplication, `{"account":"7:x"}`},
		"application no argument": {handleAgreementStatesRetrieveForApplication, `{}`},
	}
	for name, c := range bad {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			if _, err := h.call(t, c.handler, c.args); !isInvalidRequest(err) {
				t.Fatalf("err = %v, want an invalid request", err)
			}
			h.untouched(t)
		})
	}
}

func TestAgreementStatesCheckTheAccountAgainstTheTargetsBeforeSecretAndRequest(t *testing.T) {
	for name, handler := range map[string]struct {
		h    capability.Handler
		args string
	}{
		"get":                      {handleAgreementStatesGet, `{"account":"2","identifier":"MERCHANT_CENTER-US"}`},
		"retrieve-for-application": {handleAgreementStatesRetrieveForApplication, `{"account":"2"}`},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, "accounts/1")
			if _, err := h.call(t, handler.h, handler.args); !isInvalidRequest(err) ||
				strings.Contains(err.Error(), "accounts/1") {
				t.Fatalf("err = %v, want an invalid request that names no target", err)
			}
			h.untouched(t)
		})
	}
}

func TestAgreementStatesGetReadsTheStateOfANamedAccount(t *testing.T) {
	h := newHarness(t, "accounts/7")
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, stateAnswer, nil }
	out, err := h.call(t, handleAgreementStatesGet, `{"identifier":"MERCHANT_CENTER-US"}`)
	if err != nil {
		t.Fatal(err)
	}
	required, accepted := out["required"].(map[string]any), out["accepted"].(map[string]any)
	until := accepted["validUntil"].(map[string]any)
	if out["name"] != "accounts/7/termsOfServiceAgreementStates/MERCHANT_CENTER-US" || out["regionCode"] != "US" ||
		out["termsOfServiceKind"] != "MERCHANT_CENTER" || out["unknownField"] != nil ||
		required["termsOfService"] != "termsOfService/VERSION_2" || required["tosFileUri"] != "https://terms.example.invalid/v2" ||
		required["extra"] != nil || accepted["termsOfService"] != "termsOfService/VERSION_1" ||
		accepted["acceptedBy"] != "accounts/7" || until["year"] != float64(2027) || until["month"] != float64(3) ||
		until["day"] != float64(1) {
		t.Fatalf("out = %v", out)
	}
	want := []string{"/accounts/v1/accounts/7/termsOfServiceAgreementStates/MERCHANT_CENTER-US"}
	if paths := h.paths(t); !slices.Equal(paths, want) {
		t.Errorf("paths = %v, want %v", paths, want)
	}
}

func TestAgreementStateWithoutRequiredOrAcceptedShowsNeither(t *testing.T) {
	h := newHarness(t)
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"regionCode":"DE","accepted":{"validUntil":{"year":"x","month":-1,"day":99999}}}`, nil
	}
	out, err := h.call(t, handleAgreementStatesRetrieveForApplication, `{"account":"7"}`)
	if err != nil || out["required"] != nil || out["accepted"] == nil {
		t.Fatalf("out = %v, %v", out, err)
	}
	if until, _ := out["accepted"].(map[string]any)["validUntil"].(map[string]any); len(until) != 0 {
		t.Errorf("a date part that is no plausible integer was kept: %v", until)
	}
}

func TestAgreementStatesRetrieveForApplicationProvesASubAccount(t *testing.T) {
	h := newHarness(t, "advanced-accounts/10")
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		switch req.URL.Path {
		case "/accounts/v1/accounts/10:listSubaccounts":
			return 200, `{"accounts":[` + accountRow("11", "Sub") + `]}`, nil
		case "/accounts/v1/accounts/11/termsOfServiceAgreementStates:retrieveForApplication":
			return 200, stateAnswer, nil
		}
		t.Errorf("unexpected request %s", req.URL)
		return 404, `{}`, nil
	}
	if _, err := h.call(t, handleAgreementStatesRetrieveForApplication, `{"account":"11"}`); err != nil {
		t.Fatal(err)
	}
	want := []string{"/accounts/v1/accounts/10:listSubaccounts?pageSize=500",
		"/accounts/v1/accounts/11/termsOfServiceAgreementStates:retrieveForApplication"}
	if paths := h.paths(t); !slices.Equal(paths, want) {
		t.Errorf("paths = %v, want %v", paths, want)
	}
	// An account no target reaches and no sub-account proves is refused before the state is read.
	h = newHarness(t, "advanced-accounts/10")
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		if strings.HasSuffix(req.URL.Path, ":listSubaccounts") {
			return 200, `{"accounts":[` + accountRow("11", "Sub") + `]}`, nil
		}
		t.Errorf("unexpected request %s", req.URL)
		return 404, `{}`, nil
	}
	if _, err := h.call(t, handleAgreementStatesGet, `{"account":"99","identifier":"MERCHANT_CENTER-US"}`); !isInvalidRequest(err) {
		t.Errorf("err = %v", err)
	}
}

func TestTermsToolsAreOfferedToEveryBindingAndAreReads(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	for _, d := range termsDescriptors() {
		for _, targets := range [][]string{nil, {"accounts/2"}, {"advanced-accounts/1"}, {"accounts/2", "advanced-accounts/1"}} {
			if got, hint := metadata.Target.Reach(d.ID, targets); !got || hint != "" {
				t.Errorf("Reach(%s, %v) = %t, %q, want offered", d.ID, targets, got, hint)
			}
		}
		if d.RequiresToolAllowList || d.Risk.Effect != capability.EffectRead ||
			d.Risk.Confirmation != capability.ConfirmationNone {
			t.Errorf("%s is no plain read: %+v", d.ID, d.Risk)
		}
		if !slices.Contains(metadata.Profiles[0].Tools, d.ID) {
			t.Errorf("profile read lacks %s", d.ID)
		}
	}
	for _, d := range reg.Provider(Provider) {
		if strings.HasSuffix(d.ID, ".accept") {
			t.Errorf("%s must not exist", d.ID)
		}
	}
}

func TestTermsReadsMapGoogleFailures(t *testing.T) {
	h := newHarness(t)
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 404, `{"error":{"code":404,"message":"gone","status":"NOT_FOUND"}}`, nil
	}
	if _, err := h.call(t, handleTermsOfServiceGet, `{"version":"VERSION_9"}`); classOf(err) != provider.ClassNotFound {
		t.Errorf("err = %v, want not-found", err)
	}
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 403, `{"error":{"code":403,"message":"no","status":"PERMISSION_DENIED"}}`, nil
	}
	if _, err := h.call(t, handleAgreementStatesRetrieveForApplication, `{"account":"7"}`); classOf(err) != provider.ClassPermission {
		t.Errorf("err = %v, want permission", err)
	}
}
