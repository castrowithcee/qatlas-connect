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

const relationshipAnswer = `{"name":"accounts/10/relationships/20","provider":"providers/999",` +
	`"providerDisplayName":"Acme Platform","accountIdAlias":"Example Alias","unknownMember":"never held"}`

func TestRelationshipToolsAreDeclaredAndTheUpdateIsListedOnly(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	d, handler, ok := reg.Lookup(accountRelationshipsGetID)
	if !ok || handler == nil || d.Risk != readRisk || d.RequiresToolAllowList || d.Version != 1 {
		t.Errorf("get = %+v", d)
	}
	d, handler, ok = reg.Lookup(accountRelationshipsUpdateID)
	if !ok || handler == nil || d.Version != 1 || d.Risk.Effect != capability.EffectUpdate ||
		d.Risk.Confirmation != capability.ConfirmationRequired || d.RequiresToolAllowList {
		t.Fatalf("update = %+v", d)
	}
	for _, want := range []string{"Admin", "accountIdAlias", "never repeated", "delay", "confirmation"} {
		if !strings.Contains(d.Description, want) {
			t.Errorf("update description lacks %q", want)
		}
	}
	for _, profile := range metadata.Profiles {
		if slices.Contains(profile.Tools, accountRelationshipsUpdateID) {
			t.Errorf("profile %s lists the update", profile.ID)
		}
	}
	if relationshipUpdateMask != "accountIdAlias" {
		t.Errorf("update mask = %q, want only accountIdAlias", relationshipUpdateMask)
	}
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(accountRelationshipsUpdate.InputSchema, &schema); err != nil {
		t.Fatal(err)
	}
	for name := range schema.Properties {
		if !slices.Contains([]string{"account", "relationship", "account_id_alias"}, name) {
			t.Errorf("update takes the member %q", name)
		}
	}
}

func TestLinkCreationToolsAreNotRegistered(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{Provider + ".gbp-accounts.link", Provider + ".gbp-accounts.link-gbp-account",
		Provider + ".lfp-providers.link", Provider + ".lfp-providers.link-lfp-provider",
		Provider + ".account-relationships.link", Provider + ".account-relationships.create"} {
		if _, _, ok := reg.Lookup(id); ok {
			t.Errorf("%s is registered", id)
		}
	}
	for _, d := range reg.Provider(Provider) {
		if strings.Contains(d.ID, "link") {
			t.Errorf("%s names a link", d.ID)
		}
	}
}

func TestRelationshipGetShowsEveryDocumentedMemberAndMasksForeignProviders(t *testing.T) {
	h := newHarness(t, "accounts/10")
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, relationshipAnswer, nil }
	out, err := h.call(t, handleAccountRelationshipsGet, `{"account":"10","relationship":"20"}`)
	if err != nil {
		t.Fatal(err)
	}
	if out["name"] != "accounts/10/relationships/20" || out["provider"] != "providers/999" ||
		out["accountIdAlias"] != "Example Alias" || out["providerDisplayName"] != nil || len(out) != 3 {
		t.Errorf("bound out = %v", out)
	}
	h = newHarness(t)
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, relationshipAnswer, nil }
	out, err = h.call(t, handleAccountRelationshipsGet, `{"account":"10","relationship":"20"}`)
	if err != nil || out["providerDisplayName"] != "Acme Platform" || len(out) != 4 {
		t.Errorf("unbound out = %v, %v", out, err)
	}
	if got := serviceRequests(t, h); !slices.Equal(got, []string{"GET /accounts/v1/accounts/10/relationships/20"}) {
		t.Errorf("requests = %v", got)
	}
}

func TestRelationshipToolsRefuseBadIDsAndAccountsBeforeSecretAndRequest(t *testing.T) {
	long := strings.Repeat("a", 65)
	for name, handler := range map[string]capability.Handler{"get": handleAccountRelationshipsGet,
		"update": handleAccountRelationshipsUpdate} {
		for _, args := range []string{
			`{"account":"10","relationship":"../x","account_id_alias":"a"}`,
			`{"account":"10","relationship":"a/b","account_id_alias":"a"}`,
			`{"account":"10","relationship":"a:b","account_id_alias":"a"}`,
			`{"account":"10","relationship":"a%2Fb","account_id_alias":"a"}`,
			`{"account":"10","relationship":"a.b","account_id_alias":"a"}`,
			`{"account":"10","relationship":"a b","account_id_alias":"a"}`,
			`{"account":"10","relationship":"","account_id_alias":"a"}`,
			`{"account":"10","account_id_alias":"a"}`,
			`{"account":"10","relationship":"` + long + `","account_id_alias":"a"}`,
			`{"account":"1:x","relationship":"20","account_id_alias":"a"}`,
			`{"account":"10","relationship":"20","account_id_alias":"a","provider":"providers/1"}`,
			`{"account":"10","relationship":"20","account_id_alias":"a","name":"x"}`,
		} {
			h := newHarness(t)
			if _, err := h.call(t, handler, args); !isInvalidRequest(err) {
				t.Errorf("%s %s: err = %v", name, args, err)
			}
			h.untouched(t)
		}
		h := newHarness(t, "accounts/1")
		if _, err := h.call(t, handler, `{"account":"2","relationship":"20","account_id_alias":"a"}`); !isInvalidRequest(err) ||
			strings.Contains(err.Error(), "2") {
			t.Errorf("%s outside the targets: %v", name, err)
		}
		h.untouched(t)
	}
	h := newHarness(t)
	if _, err := h.call(t, handleAccountRelationshipsGet, `{"account":"10","relationship":"20","account_id_alias":"a"}`); !isInvalidRequest(err) {
		t.Errorf("get with an alias: %v", err)
	}
	h.untouched(t)
}

func TestRelationshipGetProvesASubAccountFirst(t *testing.T) {
	h := newHarness(t, "advanced-accounts/10")
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		if strings.Contains(req.URL.Path, "relationships") {
			return 200, relationshipAnswer, nil
		}
		return 200, `{"accounts":[` + accountRow("11", "Sub") + `]}`, nil
	}
	if _, err := h.call(t, handleAccountRelationshipsGet, `{"account":"11","relationship":"20"}`); err != nil {
		t.Fatal(err)
	}
	got := serviceRequests(t, h)
	if len(got) != 2 || !strings.HasPrefix(got[0], "GET /accounts/v1/accounts/10:listSubaccounts") ||
		got[1] != "GET /accounts/v1/accounts/11/relationships/20" {
		t.Errorf("requests = %v", got)
	}
	h = newHarness(t, "advanced-accounts/10")
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, `{"accounts":[]}`, nil }
	if _, err := h.call(t, handleAccountRelationshipsUpdate,
		`{"account":"99","relationship":"20","account_id_alias":"a"}`); !isInvalidRequest(err) {
		t.Errorf("err = %v", err)
	}
	for _, line := range serviceRequests(t, h) {
		if !strings.HasPrefix(line, "GET ") {
			t.Errorf("an account outside the targets was changed: %s", line)
		}
	}
}

func TestRelationshipUpdateSendsOneFixedPatchWithTheAliasAlone(t *testing.T) {
	h := newHarness(t, "accounts/10")
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, relationshipAnswer, nil }
	out, err := h.call(t, handleAccountRelationshipsUpdate,
		`{"account":"10","relationship":"20","account_id_alias":"Example Alias"}`)
	if err != nil || out["accountIdAlias"] != "Example Alias" || out["providerDisplayName"] != nil {
		t.Fatalf("out = %v, %v", out, err)
	}
	got := h.api(t)
	want := api{"PATCH", "/accounts/v1/accounts/10/relationships/20?updateMask=accountIdAlias",
		`{"accountIdAlias":"Example Alias"}`}
	if len(got) != 1 || got[0] != want {
		t.Errorf("requests = %v, want %v", got, want)
	}
}

func TestRelationshipUpdateRefusesBadAliasesBeforeSecretAndRequest(t *testing.T) {
	for name, args := range map[string]string{
		"no alias":       `{"account":"10","relationship":"20"}`,
		"empty":          `{"account":"10","relationship":"20","account_id_alias":""}`,
		"blank":          `{"account":"10","relationship":"20","account_id_alias":"  "}`,
		"control":        `{"account":"10","relationship":"20","account_id_alias":"a\nb"}`,
		"long":           `{"account":"10","relationship":"20","account_id_alias":"` + strings.Repeat("a", maxRelationshipAliasRunes+1) + `"}`,
		"no account":     `{"relationship":"20","account_id_alias":"a"}`,
		"wrong type":     `{"account":"10","relationship":"20","account_id_alias":5}`,
		"null alias":     `{"account":"10","relationship":"20","account_id_alias":null}`,
		"mask injection": `{"account":"10","relationship":"20","account_id_alias":"a","update_mask":"provider"}`,
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			if _, err := h.call(t, handleAccountRelationshipsUpdate, args); !isInvalidRequest(err) {
				t.Errorf("err = %v", err)
			}
			h.untouched(t)
		})
	}
}

func TestRelationshipUpdateIsSentOnceAndAnUnclearOutcomeIsReported(t *testing.T) {
	const args = `{"account":"10","relationship":"20","account_id_alias":"Example Alias"}`
	for status, want := range map[int]provider.Class{
		401: provider.ClassAuth, 403: provider.ClassPermission, 404: provider.ClassNotFound,
		429: provider.ClassRateLimited, 500: provider.ClassProviderError, 503: provider.ClassUnreachable,
	} {
		h := newHarness(t)
		h.google.api = func(*http.Request) (int, string, http.Header) {
			return status, `{"error":{"code":1,"status":"X"}}`, nil
		}
		_, err := h.call(t, handleAccountRelationshipsUpdate, args)
		if classOf(err) != want {
			t.Fatalf("status %d: class = %q, want %q (%v)", status, classOf(err), want, err)
		}
		if unknown := strings.Contains(err.Error(), "outcome of the change is unknown"); unknown != (status >= 500) {
			t.Errorf("status %d: unknown outcome reported = %t", status, unknown)
		}
		if n := len(h.google.seen()); n != 1 {
			t.Errorf("status %d: %d requests, want exactly one", status, n)
		}
	}
}

func TestRelationshipGetMapsGoogleErrors(t *testing.T) {
	for status, want := range map[int]provider.Class{
		403: provider.ClassPermission, 404: provider.ClassNotFound, 429: provider.ClassRateLimited,
	} {
		h := newHarness(t)
		h.google.api = func(*http.Request) (int, string, http.Header) { return status, `{"error":{"status":"X"}}`, nil }
		if _, err := h.call(t, handleAccountRelationshipsGet, `{"account":"10","relationship":"20"}`); classOf(err) != want {
			t.Errorf("status %d: class = %q, want %q", status, classOf(err), want)
		}
	}
}

func TestRelationshipUpdateSummaryNamesWhatTheConfirmationApproves(t *testing.T) {
	got := relationshipSummaries()[accountRelationshipsUpdateID](
		json.RawMessage(`{"account":"10","relationship":"20","account_id_alias":"Example Alias"}`))
	for _, want := range []string{`"20"`, "account 10", `"Example Alias"`, "afterwards"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary %q lacks %q", got, want)
		}
	}
	if relationshipSummaries()[accountRelationshipsUpdateID](json.RawMessage(`{`)) != "" {
		t.Error("an unreadable argument list got a summary")
	}
}
