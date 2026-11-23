package googlemerchant

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const businessInfoRow = `{"name":"accounts/1000001/businessInfo","phoneVerificationState":"PHONE_VERIFICATION_STATE_VERIFIED",` +
	`"phone":{"e164Number":"+1000001","extension":"12","shortCode":{"regionCode":"US","number":"611"}},` +
	`"customerService":{"uri":"https://shop.example.invalid/help","email":"help@example.invalid",` +
	`"phone":{"e164Number":"+1000002"}},` +
	`"address":{"regionCode":"DE","recipients":["Ops"],"languageCode":"de","locality":"Berlin","addressLines":["Street 1","Floor 2"],` +
	`"sublocality":"Mitte","organization":"Shop GmbH","postalCode":"10115","sortingCode":"CEDEX 7","administrativeArea":"BE","revision":0},` +
	`"koreanBusinessRegistrationNumber":"123-45-67890","unknownMember":"never held"}`

const businessIdentityRow = `{"name":"accounts/1000001/businessIdentity","promotionsConsent":"PROMOTIONS_CONSENT_GIVEN",` +
	`"blackOwned":{"identityDeclaration":"SELF_IDENTIFIES_AS"},"latinoOwned":{"identityDeclaration":"DOES_NOT_SELF_IDENTIFY_AS"},` +
	`"smallBusiness":{"identityDeclaration":"SELF_IDENTIFIES_AS"},"veteranOwned":{"identityDeclaration":"SELF_IDENTIFIES_AS"},` +
	`"womenOwned":{"identityDeclaration":"DOES_NOT_SELF_IDENTIFY_AS"},"unknownMember":"never held"}`

// businessRequests lists method, path, and query of the requests so far and checks origin and token.
func businessRequests(t *testing.T, h *harness) []string {
	t.Helper()
	var out []string
	for _, req := range h.google.seen() {
		if req.URL.Host != "merchantapi.googleapis.com" || req.URL.Scheme != "https" {
			t.Errorf("request left the fixed origin: %s", req.URL.Host)
		}
		if req.Header.Get("Authorization") != "Bearer "+testAccess || strings.Contains(req.URL.String(), testAccess) {
			t.Errorf("request %s has the token in the wrong place", req.URL.Path)
		}
		line := req.Method + " " + req.URL.Path
		if req.URL.RawQuery != "" {
			line += "?" + req.URL.RawQuery
		}
		out = append(out, line)
	}
	return out
}

var businessChangeHandlers = map[string]capability.Handler{"info": handleBusinessInfosUpdate, "identity": handleBusinessIdentitiesUpdate}

const (
	validInfoUpdate     = `{"account":"1000001","update_mask":["customerService"],"customer_service":{"email":"help@example.invalid"}}`
	validIdentityUpdate = `{"account":"1000001","update_mask":["promotionsConsent"],"promotions_consent":"PROMOTIONS_CONSENT_DENIED"}`
)

var businessValidArgs = map[string]string{"info": validInfoUpdate, "identity": validIdentityUpdate}

func TestBusinessToolsAreDeclaredWithTheirRisk(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	for _, id := range []string{businessInfosGetID, businessIdentitiesGetID} {
		d, handler, ok := reg.Lookup(id)
		if !ok || handler == nil || d.Risk != readRisk || d.RequiresToolAllowList || d.Version != 1 {
			t.Errorf("%s = %+v", id, d)
		}
	}
	for _, id := range []string{businessInfosUpdateID, businessIdentitiesUpdateID} {
		d, handler, ok := reg.Lookup(id)
		if !ok || handler == nil || d.Version != 1 || !d.RequiresToolAllowList || d.Risk.Effect != capability.EffectUpdate ||
			d.Risk.Confirmation != capability.ConfirmationRequired || d.Risk.Idempotency != capability.IdempotencyIdempotent {
			t.Errorf("%s = %+v", id, d)
		}
		for _, want := range []string{"Admin", "not an administrator", "afterwards", "never repeated", "update_mask", "confirmation"} {
			if !strings.Contains(d.Description, want) {
				t.Errorf("%s description lacks %q", id, want)
			}
		}
		for _, profile := range metadata.Profiles {
			if slices.Contains(profile.Tools, id) {
				t.Errorf("profile %s lists %s", profile.ID, id)
			}
		}
	}
	info, _, _ := reg.Lookup(businessInfosUpdateID)
	if !strings.Contains(info.Description, "https") || !strings.Contains(info.Description, "output only") {
		t.Errorf("business info description = %q", info.Description)
	}
}

func TestBusinessSummariesShowAccountAndAllowlistedFieldsOnly(t *testing.T) {
	summary := businessSummary("the business info", businessInfoPaths, "x")
	got := summary(json.RawMessage(`{"account":"1000001","update_mask":["customerService","address","phone","evil"]}`))
	if !strings.HasPrefix(got, "Update the business info (address, customerService) of account 1000001;") ||
		strings.Contains(got, "evil") || strings.Contains(got, "phone") {
		t.Errorf("summary = %q", got)
	}
	if got := summary(json.RawMessage(`{"update_mask":["address"]}`)); !strings.Contains(got, "account of this connection") {
		t.Errorf("summary without account = %q", got)
	}
}

func TestBusinessInfosGetReadsOneAccountAndShowsEveryDocumentedMember(t *testing.T) {
	h := newHarness(t)
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, businessInfoRow, nil }
	out, err := h.call(t, handleBusinessInfosGet, `{"account":"1000001"}`)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{}
	if err := json.Unmarshal([]byte(businessInfoRow), &want); err != nil {
		t.Fatal(err)
	}
	delete(want, "unknownMember")
	delete(want["address"].(map[string]any), "revision") // zero is left out
	if got, _ := json.Marshal(out); string(got) == "" || out["unknownMember"] != nil {
		t.Errorf("out = %v", out)
	}
	for key, value := range want {
		gotJSON, _ := json.Marshal(out[key])
		wantJSON, _ := json.Marshal(value)
		if string(gotJSON) != string(wantJSON) {
			t.Errorf("%s = %s, want %s", key, gotJSON, wantJSON)
		}
	}
	if got := businessRequests(t, h); !slices.Equal(got, []string{"GET /accounts/v1/accounts/1000001/businessInfo"}) {
		t.Errorf("requests = %v", got)
	}
}

func TestBusinessIdentitiesGetReadsOneAccountAndShowsEveryDocumentedMember(t *testing.T) {
	h := newHarness(t)
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, businessIdentityRow, nil }
	out, err := h.call(t, handleBusinessIdentitiesGet, `{"account":"1000001"}`)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{}
	_ = json.Unmarshal([]byte(businessIdentityRow), &want)
	delete(want, "unknownMember")
	if got, _ := json.Marshal(out); string(got) != mustMarshalSorted(want) {
		t.Errorf("out = %s", got)
	}
	if got := businessRequests(t, h); !slices.Equal(got, []string{"GET /accounts/v1/accounts/1000001/businessIdentity"}) {
		t.Errorf("requests = %v", got)
	}
}

func mustMarshalSorted(v map[string]any) string { b, _ := json.Marshal(v); return string(b) }

func TestBusinessAnswersAreBounded(t *testing.T) {
	h := newHarness(t)
	var lines []string
	for i := 0; i < maxListItems+20; i++ {
		lines = append(lines, `"`+strings.Repeat("l", 500)+`"`)
	}
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"customerService":{"uri":"https://x.invalid/` + strings.Repeat("u", 3000) + `","email":"` +
			strings.Repeat("e", 500) + `","phone":"nope"},"address":{"addressLines":[` + strings.Join(lines, ",") +
			`],"locality":7,"revision":"x"},"phone":[1]}`, nil
	}
	out, err := h.call(t, handleBusinessInfosGet, `{"account":"1"}`)
	if err == nil {
		t.Fatalf("a wrongly typed object member was held: %v", out)
	}
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"customerService":{"uri":"https://x.invalid/` + strings.Repeat("u", 3000) + `","email":"` +
			strings.Repeat("e", 500) + `"},"address":{"addressLines":[` + strings.Join(lines, ",") +
			`],"locality":7,"revision":"x"}}`, nil
	}
	out, err = h.call(t, handleBusinessInfosGet, `{"account":"1"}`)
	if err != nil {
		t.Fatal(err)
	}
	service := out["customerService"].(map[string]any)
	address := out["address"].(map[string]any)
	if len([]rune(service["uri"].(string))) != maxURLRunes || len([]rune(service["email"].(string))) != maxNameRunes ||
		len(address["addressLines"].([]any)) != maxListItems ||
		len([]rune(address["addressLines"].([]any)[0].(string))) != maxNameRunes ||
		address["locality"] != nil || address["revision"] != nil {
		t.Errorf("bounds not kept: %v", out)
	}
}

func TestBusinessInfoUpdateSendsOnlyTheMaskedStructuredBody(t *testing.T) {
	h := newHarness(t)
	var body, ctype string
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		b, _ := io.ReadAll(req.Body)
		body, ctype = string(b), req.Header.Get("Content-Type")
		return 200, businessInfoRow, nil
	}
	args := `{"account":"1000001","update_mask":["koreanBusinessRegistrationNumber","address","customerService"],` +
		`"address":{"regionCode":"DE","addressLines":["Street 1"],"postalCode":"10115","administrativeArea":"BE","locality":"Berlin"},` +
		`"customer_service":{"uri":"https://shop.example.invalid/help/{id}","email":"help@example.invalid",` +
		`"phone":{"e164Number":"+1000002","extension":"12"}},"korean_business_registration_number":"123-45-67890"}`
	out, err := h.call(t, handleBusinessInfosUpdate, args)
	if err != nil {
		t.Fatal(err)
	}
	if out["accepted"] != true || out["businessInfo"].(map[string]any)["name"] != "accounts/1000001/businessInfo" {
		t.Errorf("out = %v", out)
	}
	wantBody := `{"address":{"regionCode":"DE","addressLines":["Street 1"],"postalCode":"10115","administrativeArea":"BE","locality":"Berlin"},` +
		`"customerService":{"uri":"https://shop.example.invalid/help/{id}","email":"help@example.invalid",` +
		`"phone":{"e164Number":"+1000002","extension":"12"}},"koreanBusinessRegistrationNumber":"123-45-67890"}`
	want := []string{"PATCH /accounts/v1/accounts/1000001/businessInfo?updateMask=address%2CcustomerService%2CkoreanBusinessRegistrationNumber"}
	if got := businessRequests(t, h); !slices.Equal(got, want) || ctype != "application/json" {
		t.Errorf("requests = %v, content type %q", got, ctype)
	}
	var gotBody, wantParsed any
	_ = json.Unmarshal([]byte(body), &gotBody)
	_ = json.Unmarshal([]byte(wantBody), &wantParsed)
	if got, _ := json.Marshal(gotBody); string(got) != mustMarshal(wantParsed) {
		t.Errorf("body = %s", body)
	}
	// A short code replaces the number.
	h2 := newHarness(t)
	h2.google.api = func(req *http.Request) (int, string, http.Header) {
		b, _ := io.ReadAll(req.Body)
		body = string(b)
		return 200, `{}`, nil
	}
	if _, err := h2.call(t, handleBusinessInfosUpdate, `{"account":"1","update_mask":["customerService"],`+
		`"customer_service":{"phone":{"shortCode":{"regionCode":"US","number":"611"}}}}`); err != nil {
		t.Fatal(err)
	}
	if body != `{"customerService":{"phone":{"shortCode":{"regionCode":"US","number":"611"}}}}` {
		t.Errorf("body = %s", body)
	}
}

func mustMarshal(v any) string { b, _ := json.Marshal(v); return string(b) }

func TestBusinessIdentityUpdateSendsOnlyTheMaskedStructuredBody(t *testing.T) {
	h := newHarness(t)
	var body string
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		b, _ := io.ReadAll(req.Body)
		body = string(b)
		return 200, businessIdentityRow, nil
	}
	args := `{"account":"1000001","update_mask":["womenOwned","promotionsConsent","blackOwned","latinoOwned","smallBusiness","veteranOwned"],` +
		`"promotions_consent":"PROMOTIONS_CONSENT_GIVEN","black_owned":"SELF_IDENTIFIES_AS","latino_owned":"DOES_NOT_SELF_IDENTIFY_AS",` +
		`"small_business":"SELF_IDENTIFIES_AS","veteran_owned":"SELF_IDENTIFIES_AS","women_owned":"DOES_NOT_SELF_IDENTIFY_AS"}`
	out, err := h.call(t, handleBusinessIdentitiesUpdate, args)
	if err != nil {
		t.Fatal(err)
	}
	if out["accepted"] != true || out["businessIdentity"].(map[string]any)["promotionsConsent"] != "PROMOTIONS_CONSENT_GIVEN" {
		t.Errorf("out = %v", out)
	}
	want := []string{"PATCH /accounts/v1/accounts/1000001/businessIdentity?updateMask=blackOwned%2ClatinoOwned%2CpromotionsConsent%2CsmallBusiness%2CveteranOwned%2CwomenOwned"}
	if got := businessRequests(t, h); !slices.Equal(got, want) {
		t.Errorf("requests = %v", got)
	}
	wantBody := `{"blackOwned":{"identityDeclaration":"SELF_IDENTIFIES_AS"},"latinoOwned":{"identityDeclaration":"DOES_NOT_SELF_IDENTIFY_AS"},` +
		`"promotionsConsent":"PROMOTIONS_CONSENT_GIVEN","smallBusiness":{"identityDeclaration":"SELF_IDENTIFIES_AS"},` +
		`"veteranOwned":{"identityDeclaration":"SELF_IDENTIFIES_AS"},"womenOwned":{"identityDeclaration":"DOES_NOT_SELF_IDENTIFY_AS"}}`
	if body != wantBody {
		t.Errorf("body = %s", body)
	}
}

func TestBusinessUpdateRefusesBadArgumentsBeforeSecretAndRequest(t *testing.T) {
	info := []string{
		`{"account":"1","update_mask":[]}`,
		`{"account":"1","update_mask":["customerService"]}`,
		`{"account":"1","update_mask":["phone"],"customer_service":{"email":"a@example.invalid"}}`,
		`{"account":"1","update_mask":["phoneVerificationState"]}`,
		`{"account":"1","update_mask":["name"],"customer_service":{"email":"a@example.invalid"}}`,
		`{"account":"1","update_mask":["customer_service"],"customer_service":{"email":"a@example.invalid"}}`,
		`{"account":"1","update_mask":["customerService,address"],"customer_service":{"email":"a@example.invalid"}}`,
		`{"account":"1","update_mask":["customerService","customerService"],"customer_service":{"email":"a@example.invalid"}}`,
		`{"account":"1","update_mask":["address"],"customer_service":{"email":"a@example.invalid"}}`,
		`{"account":"1","update_mask":["customerService"],"customer_service":{"email":"a@example.invalid"},"phone":{"e164Number":"+1000001"}}`,
		`{"account":"1","update_mask":["customerService"],"customer_service":{"email":"a@example.invalid","extra":1}}`,
		`{"account":"1","update_mask":["customerService"],"customer_service":{}}`,
		`{"account":"1","update_mask":["customerService"],"customer_service":{"uri":"http://shop.example.invalid"}}`,
		`{"account":"1","update_mask":["customerService"],"customer_service":{"uri":"https://user:@shop.example.invalid"}}`,
		`{"account":"1","update_mask":["customerService"],"customer_service":{"uri":"https://shop.example.invalid\\@evil.invalid"}}`,
		`{"account":"1","update_mask":["customerService"],"customer_service":{"uri":"javascript:alert(1)"}}`,
		`{"account":"1","update_mask":["customerService"],"customer_service":{"email":"Name <a@example.invalid>"}}`,
		`{"account":"1","update_mask":["customerService"],"customer_service":{"email":"a@example.invalid, b@example.invalid"}}`,
		`{"account":"1","update_mask":["customerService"],"customer_service":{"email":"not-an-address"}}`,
		`{"account":"1","update_mask":["customerService"],"customer_service":{"phone":{"e164Number":"0555 222"}}}`,
		`{"account":"1","update_mask":["customerService"],"customer_service":{"phone":{"e164Number":"+1000001","shortCode":{"regionCode":"US","number":"611"}}}}`,
		`{"account":"1","update_mask":["customerService"],"customer_service":{"phone":{}}}`,
		`{"account":"1","update_mask":["customerService"],"customer_service":{"phone":{"e164Number":"+1000001","extension":"1;2"}}}`,
		`{"account":"1","update_mask":["customerService"],"customer_service":{"phone":{"shortCode":{"regionCode":"usa","number":"611"}}}}`,
		`{"account":"1","update_mask":["address"],"address":{}}`,
		`{"account":"1","update_mask":["address"],"address":{"regionCode":"Germany"}}`,
		`{"account":"1","update_mask":["address"],"address":{"regionCode":"DE","phoneVerificationState":"x"}}`,
		`{"account":"1","update_mask":["address"],"address":{"regionCode":"DE","addressLines":["a","b","c","d","e","f"]}}`,
		`{"account":"1","update_mask":["address"],"address":{"regionCode":"DE","addressLines":["` + strings.Repeat("a", 201) + `"]}}`,
		`{"account":"1","update_mask":["address"],"address":{"regionCode":"DE","locality":"Ber\nlin"}}`,
		`{"account":"1","update_mask":["address"],"address":{"regionCode":"DE","revision":0}}`,
		`{"account":"1","update_mask":["koreanBusinessRegistrationNumber"],"korean_business_registration_number":"1234567890"}`,
		`{"account":"1","update_mask":["customerService"],"customer_service":{"email":"a@example.invalid"},"account2":"2"}`,
		`{"account":"01","update_mask":["customerService"],"customer_service":{"email":"a@example.invalid"}}`,
		`{"update_mask":["customerService"],"customer_service":{"email":"a@example.invalid"}}`,
	}
	identity := []string{
		`{"account":"1","update_mask":[]}`,
		`{"account":"1","update_mask":["promotionsConsent"]}`,
		`{"account":"1","update_mask":["promotionsConsent"],"promotions_consent":"PROMOTIONS_CONSENT_UNSPECIFIED"}`,
		`{"account":"1","update_mask":["promotionsConsent"],"promotions_consent":"yes"}`,
		`{"account":"1","update_mask":["blackOwned"],"black_owned":"IDENTITY_DECLARATION_UNSPECIFIED"}`,
		`{"account":"1","update_mask":["blackOwned"],"black_owned":"PROMOTIONS_CONSENT_GIVEN"}`,
		`{"account":"1","update_mask":["black_owned"],"black_owned":"SELF_IDENTIFIES_AS"}`,
		`{"account":"1","update_mask":["name"],"black_owned":"SELF_IDENTIFIES_AS"}`,
		`{"account":"1","update_mask":["blackOwned"],"black_owned":"SELF_IDENTIFIES_AS","women_owned":"SELF_IDENTIFIES_AS"}`,
		`{"account":"1","update_mask":["blackOwned","blackOwned"],"black_owned":"SELF_IDENTIFIES_AS"}`,
		`{"account":"1","update_mask":["blackOwned"],"black_owned":{"identityDeclaration":"SELF_IDENTIFIES_AS"}}`,
		`{"account":"1","update_mask":["blackOwned"],"black_owned":"SELF_IDENTIFIES_AS","extra":1}`,
	}
	for kind, list := range map[string][]string{"info": info, "identity": identity} {
		for _, args := range list {
			h := newHarness(t)
			if _, err := h.call(t, businessChangeHandlers[kind], args); !isInvalidRequest(err) {
				t.Errorf("%s %s: err = %v", kind, args, err)
			}
			h.untouched(t)
		}
	}
	// What the checks must keep passing.
	for kind, args := range businessValidArgs {
		h := newHarness(t)
		if _, err := h.call(t, businessChangeHandlers[kind], args); err != nil {
			t.Errorf("%s: valid arguments refused: %v", kind, err)
		}
	}
}

func TestBusinessGetsRefuseUnknownMembersBeforeSecretAndRequest(t *testing.T) {
	for _, handler := range []capability.Handler{handleBusinessInfosGet, handleBusinessIdentitiesGet} {
		for _, args := range []string{`{"account":"1","update_mask":["address"]}`, `{"account":"1/2"}`, `{"account":"01"}`, `{}`} {
			h := newHarness(t)
			if _, err := h.call(t, handler, args); !isInvalidRequest(err) {
				t.Errorf("%s: err = %v", args, err)
			}
			h.untouched(t)
		}
	}
}

func TestBusinessAccountOutsideTheTargetsIsRefusedWithoutNamingIt(t *testing.T) {
	for name, handler := range map[string]capability.Handler{"info get": handleBusinessInfosGet,
		"identity get": handleBusinessIdentitiesGet} {
		h := newHarness(t, "accounts/7")
		_, err := h.call(t, handler, `{"account":"4242"}`)
		if !isInvalidRequest(err) || strings.Contains(err.Error(), "4242") {
			t.Errorf("%s: err = %v", name, err)
		}
		h.untouched(t)
	}
	for kind, handler := range businessChangeHandlers {
		h := newHarness(t, "accounts/7")
		args := strings.Replace(businessValidArgs[kind], `"1000001"`, `"4242"`, 1)
		_, err := h.call(t, handler, args)
		if !isInvalidRequest(err) || strings.Contains(err.Error(), "4242") {
			t.Errorf("%s: err = %v", kind, err)
		}
		h.untouched(t)
	}
}

func TestBusinessSubAccountIsProvenBeforeTheReadOrTheChange(t *testing.T) {
	for name, c := range map[string]struct {
		handler capability.Handler
		args    string
		last    string
	}{
		"info get":        {handleBusinessInfosGet, `{"account":"11"}`, "GET /accounts/v1/accounts/11/businessInfo"},
		"identity get":    {handleBusinessIdentitiesGet, `{"account":"11"}`, "GET /accounts/v1/accounts/11/businessIdentity"},
		"info update":     {handleBusinessInfosUpdate, strings.Replace(validInfoUpdate, "1000001", "11", 1), "PATCH /accounts/v1/accounts/11/businessInfo?updateMask=customerService"},
		"identity update": {handleBusinessIdentitiesUpdate, strings.Replace(validIdentityUpdate, "1000001", "11", 1), "PATCH /accounts/v1/accounts/11/businessIdentity?updateMask=promotionsConsent"},
	} {
		h := newHarness(t, "advanced-accounts/10")
		h.google.api = func(req *http.Request) (int, string, http.Header) {
			if strings.HasSuffix(req.URL.Path, ":listSubaccounts") {
				return 200, `{"accounts":[` + accountRow("11", "Sub") + `]}`, nil
			}
			return 200, `{}`, nil
		}
		if _, err := h.call(t, c.handler, c.args); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		want := []string{"GET /accounts/v1/accounts/10:listSubaccounts?pageSize=500", c.last}
		if got := businessRequests(t, h); !slices.Equal(got, want) {
			t.Errorf("%s: requests = %v, want %v", name, got, want)
		}
	}
}

func TestBusinessAccountThatIsNoSubAccountSendsNoChangeAndIsNotNamed(t *testing.T) {
	for name, c := range map[string]struct {
		handler capability.Handler
		args    string
	}{
		"info get":        {handleBusinessInfosGet, `{"account":"4242"}`},
		"identity get":    {handleBusinessIdentitiesGet, `{"account":"4242"}`},
		"info update":     {handleBusinessInfosUpdate, strings.Replace(validInfoUpdate, "1000001", "4242", 1)},
		"identity update": {handleBusinessIdentitiesUpdate, strings.Replace(validIdentityUpdate, "1000001", "4242", 1)},
	} {
		h := newHarness(t, "advanced-accounts/10")
		h.google.api = func(*http.Request) (int, string, http.Header) {
			return 200, `{"accounts":[` + accountRow("11", "Sub") + `]}`, nil
		}
		_, err := h.call(t, c.handler, c.args)
		if !isInvalidRequest(err) || strings.Contains(err.Error(), "4242") {
			t.Fatalf("%s: err = %v", name, err)
		}
		if got := businessRequests(t, h); len(got) != 1 || !strings.Contains(got[0], ":listSubaccounts") {
			t.Errorf("%s: requests = %v, want only the proof read", name, got)
		}
	}
}

// The special acceptance: a missing Admin role is a 403 and ends as a permission error with a clear message,
// after exactly one request.
func TestBusinessMissingAdminRoleIsAPermissionErrorWithoutRetry(t *testing.T) {
	const adminBody = `{"error":{"code":403,"message":"User is not an administrator of account 1000001","status":"PERMISSION_DENIED"}}`
	for kind, handler := range businessChangeHandlers {
		h := newHarness(t)
		h.google.api = func(*http.Request) (int, string, http.Header) { return 403, adminBody, nil }
		_, err := h.call(t, handler, businessValidArgs[kind])
		if classOf(err) != provider.ClassPermission {
			t.Fatalf("%s: err = %v", kind, err)
		}
		for _, want := range []string{"Admin role", "not an administrator", "administrator of the account"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s: message lacks %q: %v", kind, want, err)
			}
		}
		if got := len(h.google.seen()); got != 1 {
			t.Errorf("%s: %d requests, want exactly one", kind, got)
		}
		if strings.Contains(err.Error(), "outcome of the change is unknown") || strings.Contains(err.Error(), "merchantapi.googleapis.com") {
			t.Errorf("%s: err = %v", kind, err)
		}
		var failed *provider.Error
		if !errors.As(err, &failed) || failed.Original == nil || failed.Original.Status != http.StatusForbidden {
			t.Errorf("%s: the status does not travel as the original: %v", kind, err)
		}
	}
	// A read that is refused does not claim an admin duty.
	h := newHarness(t)
	h.google.api = func(*http.Request) (int, string, http.Header) { return 403, adminBody, nil }
	if _, err := h.call(t, handleBusinessInfosGet, `{"account":"1"}`); classOf(err) != provider.ClassPermission ||
		strings.Contains(err.Error(), "ask an administrator") {
		t.Errorf("read err = %v", err)
	}
}

func TestBusinessFailedChangeIsNeverRepeatedAndSaysWhenTheOutcomeIsOpen(t *testing.T) {
	type answer struct {
		status int
		body   string
		header http.Header
	}
	for name, c := range map[string]struct {
		answer  answer
		fail    error
		class   provider.Class
		unknown bool
	}{
		"401":      {answer: answer{401, `{}`, nil}, class: provider.ClassAuth},
		"404":      {answer: answer{404, `{}`, nil}, class: provider.ClassNotFound},
		"429":      {answer: answer{429, quotaBody(reasonDayQuota), http.Header{"Retry-After": {"30"}}}, class: provider.ClassRateLimited},
		"500":      {answer: answer{500, `{}`, nil}, class: provider.ClassProviderError, unknown: true},
		"not JSON": {answer: answer{200, `<html>`, http.Header{"Content-Type": {"text/html"}}}, class: provider.ClassInvalidResponse, unknown: true},
		"broken":   {answer: answer{200, `{"name":`, nil}, class: provider.ClassInvalidResponse, unknown: true},
		"timeout":  {fail: http.ErrHandlerTimeout, unknown: true},
	} {
		for kind, handler := range businessChangeHandlers {
			h := newHarness(t)
			h.google.fail = c.fail
			h.google.api = func(*http.Request) (int, string, http.Header) { return c.answer.status, c.answer.body, c.answer.header }
			_, err := h.call(t, handler, businessValidArgs[kind])
			if err == nil {
				t.Fatalf("%s %s: no error", kind, name)
			}
			if got := classOf(err); c.class != "" && got != c.class {
				t.Errorf("%s %s: class = %q, want %q (%v)", kind, name, got, c.class, err)
			}
			if got := len(h.google.seen()); got != 1 {
				t.Errorf("%s %s: %d requests, want exactly one", kind, name, got)
			}
			if open := strings.Contains(err.Error(), "outcome of the change is unknown"); open != c.unknown {
				t.Errorf("%s %s: outcome note = %t, want %t (%v)", kind, name, open, c.unknown, err)
			}
		}
	}
}

func TestBusinessRateLimitedChangeHoldsTheNextRequest(t *testing.T) {
	h := newHarness(t)
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 429, quotaBody(reasonMinuteQuota), http.Header{"Retry-After": {"30"}}
	}
	if _, err := h.call(t, handleBusinessInfosUpdate, validInfoUpdate); classOf(err) != provider.ClassRateLimited {
		t.Fatalf("err = %v", err)
	}
	h.google.api = nil
	if _, err := h.call(t, handleBusinessInfosGet, `{"account":"1"}`); err != nil {
		t.Fatal(err)
	}
	if waits := h.spacing.recorded(); len(waits) != 1 || waits[0] < 30*time.Second || waits[0] > maxHold {
		t.Errorf("waits = %v, want the Retry-After hold", waits)
	}
}
