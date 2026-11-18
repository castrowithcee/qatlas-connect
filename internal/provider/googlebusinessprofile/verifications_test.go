package googlebusinessprofile

import (
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const (
	verificationsHostName  = "mybusinessverifications.googleapis.com"
	notificationsHostName  = "mybusinessnotifications.googleapis.com"
	informationHostName    = "mybusinessbusinessinformation.googleapis.com"
	maskedPhone            = "phone-masked-sample"
	maskedEmailUser        = "masked-user"
	maskedEmailDomain      = "mail-sample.invalid"
	maskedPostcardBusiness = "Sample bakery"
)

// verificationsHosts lets the fake transport answer the verifications and notifications APIs and records
// the method of each request next to what the fake records.
type verificationsHosts struct {
	fake    *fakeGoogle
	methods *[]string
}

func (v verificationsHosts) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Host != verificationsHostName && r.URL.Host != notificationsHostName {
		if r.URL.Host == informationHostName {
			*v.methods = append(*v.methods, r.Method)
		}
		return v.fake.RoundTrip(r)
	}
	body := ""
	if r.Body != nil {
		raw, _ := io.ReadAll(r.Body)
		body = string(raw)
	}
	v.fake.mu.Lock()
	v.fake.api = append(v.fake.api, seen{host: r.URL.Host, path: r.URL.EscapedPath(), query: r.URL.RawQuery,
		authorization: r.Header.Get("Authorization"), full: r.URL.String(), body: body})
	*v.methods = append(*v.methods, r.Method)
	n := len(v.fake.api)
	answer := v.fake.answer
	v.fake.mu.Unlock()
	if answer != nil {
		return answer(r, n)
	}
	return respond(r, http.StatusOK, "application/json", `{}`), nil
}

func newVerificationsEnvironment(t *testing.T) (*environment, *[]string) {
	t.Helper()
	env := newEnvironment(t, nil)
	methods := &[]string{}
	transport = verificationsHosts{fake: env.fake, methods: methods}
	return env, methods
}

const verificationLocation = `"location":"` + boundLocation + `"`

func TestVerificationsListReadsPagesWithACursor(t *testing.T) {
	env, methods := newVerificationsEnvironment(t)
	env.fake.answer = func(r *http.Request, n int) (*http.Response, error) {
		if n == 1 {
			return respond(r, http.StatusOK, "application/json", `{"verifications":[`+
				`{"name":"locations/2000002/verifications/v1","state":"COMPLETED","method":"ADDRESS",`+
				`"createTime":"2026-01-02T03:04:05Z","undocumented":"`+detailCanary+`"},`+
				`{"name":"locations/2000002/verifications/v2","state":"PENDING","method":"TRUSTED_PARTNER",`+
				`"announcement":"Sample announcement","createTime":"2026-02-02T03:04:05Z"}],`+
				`"nextPageToken":"page-two-token"}`), nil
		}
		return respond(r, http.StatusOK, "application/json", `{"verifications":[`+
			`{"name":"locations/2000002/verifications/v3","state":"FAILED","method":"SMS"}]}`), nil
	}
	first, err := env.invoke(t, verificationsListID, "all", `{`+verificationLocation+`,"page_size":2}`)
	if err != nil {
		t.Fatal(err)
	}
	entries := first["verifications"].([]any)
	if len(entries) != 2 || first["next_cursor"] == nil {
		t.Fatalf("first page = %v", first)
	}
	want := map[string]any{"name": "locations/2000002/verifications/v2", "state": "PENDING",
		"method": "TRUSTED_PARTNER", "announcement": "Sample announcement", "createTime": "2026-02-02T03:04:05Z"}
	if !reflect.DeepEqual(entries[1], want) {
		t.Errorf("entry = %v", entries[1])
	}
	if raw, _ := json.Marshal(first); strings.Contains(string(raw), detailCanary) || strings.Contains(string(raw), "page-two-token") {
		t.Errorf("the result shows an undocumented member or the raw page token: %s", raw)
	}
	second, err := env.invoke(t, verificationsListID, "all",
		`{`+verificationLocation+`,"page_size":2,"cursor":"`+first["next_cursor"].(string)+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(second["verifications"].([]any)) != 1 || second["next_cursor"] != nil {
		t.Errorf("second page = %v", second)
	}
	calls, _ := env.fake.calls()
	if len(calls) != 2 || calls[0].host != verificationsHostName ||
		calls[0].path != "/v1/"+boundLocation+"/verifications" || calls[0].values().Get("pageSize") != "2" ||
		calls[0].values().Get("pageToken") != "" || calls[1].values().Get("pageToken") != "page-two-token" ||
		calls[0].authorization != "Bearer "+accessPrefix+"1" {
		t.Errorf("requests = %+v", calls)
	}
	for _, m := range *methods {
		if m != http.MethodGet {
			t.Errorf("method = %s, want GET", m)
		}
	}
	// A cursor is bound to the location it was issued for.
	_, err = env.invoke(t, verificationsListID, "all",
		`{"location":"`+otherLocation+`","cursor":"`+first["next_cursor"].(string)+`"}`)
	if !isInvalidRequest(err) {
		t.Errorf("cursor of another location: error = %v", err)
	}
}

func TestVerificationsListWithNoEntriesIsEmpty(t *testing.T) {
	env, _ := newVerificationsEnvironment(t)
	out, err := env.invoke(t, verificationsListID, "all", `{`+verificationLocation+`}`)
	if err != nil || len(out) != 0 {
		t.Errorf("result = %v, error = %v", out, err)
	}
}

func TestFetchOptionsKeepsMaskedTargetsAsGoogleSentThem(t *testing.T) {
	env, methods := newVerificationsEnvironment(t)
	env.fake.answer = reply(http.StatusOK, `{"options":[`+
		`{"verificationMethod":"ADDRESS","addressData":{"business":"`+maskedPostcardBusiness+`","address":{`+
		`"regionCode":"CH","postalCode":"0000","locality":"Sample town","addressLines":["Sample street *"],`+
		`"revision":0},"expectedDeliveryDaysRegion":5}},`+
		`{"verificationMethod":"EMAIL","emailData":{"domain":"`+maskedEmailDomain+`","isUserNameEditable":true,`+
		`"user":"`+maskedEmailUser+`"}},`+
		`{"verificationMethod":"PHONE_CALL","phoneNumber":"`+maskedPhone+`"},`+
		`{"verificationMethod":"SMS","phoneNumber":"`+maskedPhone+`"},`+
		`{"verificationMethod":"AUTO"},`+
		`{"verificationMethod":"TRUSTED_PARTNER","announcement":"Sample announcement","undocumented":"`+detailCanary+`"}]}`)
	out, err := env.invoke(t, verificationsFetchOptionsID, "all", `{`+verificationLocation+`,"language_code":"de"}`)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"options": []any{
		map[string]any{"verificationMethod": "ADDRESS", "addressData": map[string]any{"business": maskedPostcardBusiness,
			"address": map[string]any{"regionCode": "CH", "postalCode": "0000", "locality": "Sample town",
				"addressLines": []any{"Sample street *"}, "revision": 0.0}, "expectedDeliveryDaysRegion": 5.0}},
		map[string]any{"verificationMethod": "EMAIL", "emailData": map[string]any{"domain": maskedEmailDomain,
			"isUserNameEditable": true, "user": maskedEmailUser}},
		map[string]any{"verificationMethod": "PHONE_CALL", "phoneNumber": maskedPhone},
		map[string]any{"verificationMethod": "SMS", "phoneNumber": maskedPhone},
		map[string]any{"verificationMethod": "AUTO"},
		map[string]any{"verificationMethod": "TRUSTED_PARTNER", "announcement": "Sample announcement"},
	}}
	if !reflect.DeepEqual(out, want) {
		got, _ := json.Marshal(out)
		t.Errorf("result = %s", got)
	}
	calls, _ := env.fake.calls()
	if len(calls) != 1 || calls[0].host != verificationsHostName ||
		calls[0].path != "/v1/"+boundLocation+":fetchVerificationOptions" || calls[0].query != "" ||
		calls[0].body != `{"languageCode":"de"}` || !reflect.DeepEqual(*methods, []string{http.MethodPost}) {
		t.Errorf("requests = %+v, methods = %v", calls, *methods)
	}
}

func TestFetchOptionsSendsTheAddressContext(t *testing.T) {
	env, _ := newVerificationsEnvironment(t)
	_, err := env.invoke(t, verificationsFetchOptionsID, "all", `{`+verificationLocation+`,"language_code":"en-GB",`+
		`"context":{"address":{"revision":0,"regionCode":"CH","languageCode":"de","postalCode":"0000",`+
		`"sortingCode":"S1","administrativeArea":"Sample area","locality":"Sample town","sublocality":"Sample district",`+
		`"addressLines":["Sample street 1"],"recipients":["Front desk"],"organization":"Sample bakery"}}}`)
	if err != nil {
		t.Fatal(err)
	}
	calls, _ := env.fake.calls()
	want := `{"languageCode":"en-GB","context":{"address":{"revision":0,"regionCode":"CH","languageCode":"de",` +
		`"postalCode":"0000","sortingCode":"S1","administrativeArea":"Sample area","locality":"Sample town",` +
		`"sublocality":"Sample district","addressLines":["Sample street 1"],"recipients":["Front desk"],` +
		`"organization":"Sample bakery"}}}`
	if len(calls) != 1 || calls[0].body != want {
		t.Errorf("body = %+v, want %s", calls, want)
	}
}

func TestFetchOptionsRefusesBadArgumentsBeforeAnyRequest(t *testing.T) {
	env, _ := newVerificationsEnvironment(t)
	for name, args := range map[string]string{
		"no language":            `{` + verificationLocation + `}`,
		"bad language":           `{` + verificationLocation + `,"language_code":"de de"}`,
		"address without region": `{` + verificationLocation + `,"language_code":"de","context":{"address":{"locality":"x"}}}`,
		"unknown address member": `{` + verificationLocation + `,"language_code":"de","context":{"address":{"regionCode":"CH","other":"x"}}}`,
		"unknown context member": `{` + verificationLocation + `,"language_code":"de","context":{"other":1}}`,
		"unknown argument":       `{` + verificationLocation + `,"language_code":"de","method":"EMAIL"}`,
		"address line too long":  `{` + verificationLocation + `,"language_code":"de","context":{"address":{"regionCode":"CH","addressLines":["` + strings.Repeat("a", 501) + `"]}}}`,
	} {
		if _, err := env.invoke(t, verificationsFetchOptionsID, "all", args); !isInvalidRequest(err) {
			t.Errorf("%s: error = %v, want invalid-request", name, err)
		}
	}
	if calls, grants := env.fake.calls(); len(calls) != 0 || grants != 0 {
		t.Errorf("%d requests and %d grants, want none", len(calls), grants)
	}
}

func TestMerchantStateReportsEveryDocumentedMember(t *testing.T) {
	env, _ := newVerificationsEnvironment(t)
	for name, c := range map[string]struct {
		body string
		want map[string]any
	}{
		"in good standing": {`{"hasVoiceOfMerchant":true,"hasBusinessAuthority":true,"undocumented":"` + detailCanary + `"}`,
			map[string]any{"hasVoiceOfMerchant": true, "hasBusinessAuthority": true}},
		"under review": {`{"hasVoiceOfMerchant":false,"waitForVoiceOfMerchant":{}}`,
			map[string]any{"hasVoiceOfMerchant": false, "waitForVoiceOfMerchant": true}},
		"guidelines": {`{"complyWithGuidelines":{"recommendationReason":"BUSINESS_LOCATION_SUSPENDED"}}`,
			map[string]any{"complyWithGuidelines": map[string]any{"recommendationReason": "BUSINESS_LOCATION_SUSPENDED"}}},
		"duplicate": {`{"hasBusinessAuthority":false,"resolveOwnershipConflict":{}}`,
			map[string]any{"hasBusinessAuthority": false, "resolveOwnershipConflict": true}},
		"verify": {`{"verify":{"hasPendingVerification":true}}`,
			map[string]any{"verify": map[string]any{"hasPendingVerification": true}}},
		"verify without pending": {`{"verify":{}}`,
			map[string]any{"verify": map[string]any{"hasPendingVerification": false}}},
	} {
		env.fake.answer = reply(http.StatusOK, c.body)
		out, err := env.invoke(t, verificationsMerchantStateID, "all", `{`+verificationLocation+`}`)
		if err != nil || !reflect.DeepEqual(out, c.want) {
			t.Errorf("%s: result = %v, error = %v, want %v", name, out, err, c.want)
		}
	}
	calls, _ := env.fake.calls()
	if calls[0].host != verificationsHostName || calls[0].path != "/v1/"+boundLocation+"/VoiceOfMerchantState" {
		t.Errorf("request = %+v", calls[0])
	}
}

func TestNotificationSettingShowsTheTopicAndTypes(t *testing.T) {
	env, methods := newVerificationsEnvironment(t)
	env.fake.answer = reply(http.StatusOK, `{"name":"`+boundAccount+`/notificationSetting",`+
		`"pubsubTopic":"projects/sample-project/topics/sample-topic",`+
		`"notificationTypes":["GOOGLE_UPDATE","NEW_REVIEW","NEW_QUESTION","VOICE_OF_MERCHANT_UPDATED"],`+
		`"undocumented":"`+detailCanary+`"}`)
	out, err := env.invoke(t, notificationsGetID, "all", `{"account":"`+boundAccount+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"name": boundAccount + "/notificationSetting",
		"pubsubTopic":       "projects/sample-project/topics/sample-topic",
		"notificationTypes": []any{"GOOGLE_UPDATE", "NEW_REVIEW", "NEW_QUESTION", "VOICE_OF_MERCHANT_UPDATED"}}
	if !reflect.DeepEqual(out, want) {
		t.Errorf("result = %v", out)
	}
	calls, _ := env.fake.calls()
	if len(calls) != 1 || calls[0].host != notificationsHostName ||
		calls[0].path != "/v1/"+boundAccount+"/notificationSetting" || !reflect.DeepEqual(*methods, []string{http.MethodGet}) {
		t.Errorf("requests = %+v, methods = %v", calls, *methods)
	}

	// An account that has no topic yet.
	env.fake.answer = reply(http.StatusOK, `{"name":"`+boundAccount+`/notificationSetting"}`)
	out, err = env.invoke(t, notificationsGetID, "all", `{"account":"`+boundAccount+`"}`)
	if err != nil || !reflect.DeepEqual(out, map[string]any{"name": boundAccount + "/notificationSetting"}) {
		t.Errorf("without a topic: result = %v, error = %v", out, err)
	}
}

func TestNotificationSettingFollowsTheTarget(t *testing.T) {
	env, _ := newVerificationsEnvironment(t)
	if _, err := env.invoke(t, notificationsGetID, "account", `{}`); err != nil {
		t.Errorf("bound account without argument: %v", err)
	}
	if _, err := env.invoke(t, notificationsGetID, "account", `{"account":"`+otherAccount+`"}`); !isInvalidRequest(err) {
		t.Errorf("other account: error = %v", err)
	}
	for _, args := range []string{`{}`, `{"account":"` + boundAccount + `"}`} {
		if _, err := env.invoke(t, notificationsGetID, "location", args); err == nil ||
			!strings.Contains(err.Error(), "does not offer tool") {
			t.Errorf("location target %s: error = %v, want the tool not offered", args, err)
		}
	}
	for _, bad := range []string{"accounts/-", "accounts/01", boundLocation, "accounts/1/x"} {
		if _, err := env.invoke(t, notificationsGetID, "all", `{"account":"`+bad+`"}`); !isInvalidRequest(err) {
			t.Errorf("%s: error = %v", bad, err)
		}
	}
	if _, err := env.invoke(t, notificationsGetID, "all", `{}`); !isInvalidRequest(err) {
		t.Errorf("unbound without account: error = %v", err)
	}
	if calls, _ := env.fake.calls(); len(calls) != 1 {
		t.Errorf("%d requests, want one for the bound account only", len(calls))
	}
	// A location target does not offer the tool at all.
	if ok, _ := targetReaches(notificationsGetID, []string{boundLocation}); ok {
		t.Error("a location target reaches notifications.get")
	}
}

func providerMetadata(t *testing.T) ([]string, error) {
	t.Helper()
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, d := range reg.Provider(Provider) {
		ids = append(ids, d.ID)
	}
	return ids, nil
}

func TestLocationToolsFollowTheTarget(t *testing.T) {
	tools := map[string]string{
		verificationsListID:          ``,
		verificationsFetchOptionsID:  `,"language_code":"en"`,
		verificationsMerchantStateID: ``,
	}
	t.Run("location target", func(t *testing.T) {
		env, _ := newVerificationsEnvironment(t)
		for tool, rest := range tools {
			if _, err := env.invoke(t, tool, "location", `{`+strings.TrimPrefix(rest, ",")+`}`); err != nil {
				t.Errorf("%s without location: %v", tool, err)
			}
			if _, err := env.invoke(t, tool, "location", `{"location":"`+otherLocation+`"`+rest+`}`); !isInvalidRequest(err) {
				t.Errorf("%s other location: error = %v", tool, err)
			}
		}
		calls, _ := env.fake.calls()
		if len(calls) != len(tools) {
			t.Fatalf("%d requests, want one per tool", len(calls))
		}
		for _, call := range calls {
			if !strings.HasPrefix(call.path, "/v1/"+boundLocation) {
				t.Errorf("request = %+v", call)
			}
		}
	})
	t.Run("account target proves the location", func(t *testing.T) {
		for tool, rest := range tools {
			env, _ := newVerificationsEnvironment(t)
			env.fake.answer = func(r *http.Request, _ int) (*http.Response, error) {
				if r.URL.Host == informationHostName {
					return respond(r, http.StatusOK, "application/json", `{"locations":[{"name":"`+boundLocation+`"}]}`), nil
				}
				return respond(r, http.StatusOK, "application/json", `{}`), nil
			}
			if _, err := env.invoke(t, tool, "account", `{`+verificationLocation+rest+`}`); err != nil {
				t.Fatalf("%s: %v", tool, err)
			}
			calls, _ := env.fake.calls()
			if len(calls) != 2 || calls[0].host != informationHostName || calls[0].path != "/v1/"+boundAccount+"/locations" ||
				calls[1].host != verificationsHostName {
				t.Fatalf("%s: requests = %+v, want the proof and then the read", tool, calls)
			}
			_, err := env.invoke(t, tool, "account", `{"location":"`+otherLocation+`"`+rest+`}`)
			if !isInvalidRequest(err) || strings.Contains(err.Error(), otherLocation) {
				t.Errorf("%s outside the account: error = %v", tool, err)
			}
			after, _ := env.fake.calls()
			for _, call := range after[2:] {
				if call.host == verificationsHostName {
					t.Errorf("%s: a location outside the account was read: %+v", tool, call)
				}
			}
		}
	})
	t.Run("a location is required and checked", func(t *testing.T) {
		env, _ := newVerificationsEnvironment(t)
		for tool, rest := range tools {
			if _, err := env.invoke(t, tool, "account", `{`+strings.TrimPrefix(rest, ",")+`}`); !isInvalidRequest(err) {
				t.Errorf("%s: error = %v", tool, err)
			}
			for _, bad := range []string{"locations/-", "locations/02", boundAccount, "locations/1/verifications/2"} {
				if _, err := env.invoke(t, tool, "all", `{"location":"`+bad+`"`+rest+`}`); !isInvalidRequest(err) {
					t.Errorf("%s %s: error = %v", tool, bad, err)
				}
			}
		}
		if calls, grants := env.fake.calls(); len(calls) != 0 || grants != 0 {
			t.Errorf("%d requests and %d grants, want none", len(calls), grants)
		}
	})
}

func TestVerificationToolsMapErrorsLikeTheOtherGroups(t *testing.T) {
	for name, c := range map[string]struct {
		answer func(*http.Request, int) (*http.Response, error)
		class  provider.Class
	}{
		"401": {reply(http.StatusUnauthorized, errorObject(401, "UNAUTHENTICATED", "CREDENTIALS_MISSING")), provider.ClassAuth},
		"403": {reply(http.StatusForbidden, errorObject(403, "PERMISSION_DENIED", "SERVICE_DISABLED")), provider.ClassPermission},
		"404": {reply(http.StatusNotFound, errorObject(404, "NOT_FOUND", "")), provider.ClassNotFound},
		"429": {reply(http.StatusTooManyRequests, errorObject(429, "RESOURCE_EXHAUSTED", "RATE_LIMIT_EXCEEDED")),
			provider.ClassRateLimited},
		"429 with a quota of zero": {reply(http.StatusTooManyRequests, zeroQuotaObject()), ClassAccessNotApproved},
		"400":                      {reply(http.StatusBadRequest, errorObject(400, "INVALID_ARGUMENT", "")), provider.ClassInvalidResponse},
		"not JSON": {func(r *http.Request, _ int) (*http.Response, error) {
			return respond(r, http.StatusOK, "text/html", "<p>"+detailCanary+"</p>"), nil
		}, provider.ClassInvalidResponse},
	} {
		for tool, args := range map[string]string{
			verificationsListID:          `{` + verificationLocation + `}`,
			verificationsFetchOptionsID:  `{` + verificationLocation + `,"language_code":"en"}`,
			verificationsMerchantStateID: `{` + verificationLocation + `}`,
			notificationsGetID:           `{"account":"` + boundAccount + `"}`,
		} {
			t.Run(name+" "+tool, func(t *testing.T) {
				env, _ := newVerificationsEnvironment(t)
				env.fake.answer = c.answer
				_, err := env.invoke(t, tool, "all", args)
				if classOf(err) != c.class {
					t.Fatalf("error = %v (class %q), want %q", err, classOf(err), c.class)
				}
				if strings.Contains(err.Error(), detailCanary) || strings.Contains(err.Error(), accessPrefix) {
					t.Errorf("the message carries provider text or a token: %v", err)
				}
			})
		}
	}
}

func TestMethodsThatTriggerOrChangeVerificationsHaveNoTool(t *testing.T) {
	ids, _ := providerMetadata(t)
	have := map[string]bool{}
	for _, id := range ids {
		have[id] = true
	}
	for _, id := range []string{verificationsListID, verificationsFetchOptionsID, verificationsMerchantStateID,
		notificationsGetID} {
		if !have[id] {
			t.Errorf("tool %s is missing", id)
		}
	}
	for _, id := range ids {
		if strings.HasPrefix(id, Provider+".verifications.") || strings.HasPrefix(id, Provider+".notifications.") {
			switch strings.TrimPrefix(strings.TrimPrefix(id, Provider+".verifications."), Provider+".notifications.") {
			case "list", "fetch-options", "get-merchant-state", "get":
			default:
				t.Errorf("unexpected tool %s", id)
			}
		}
	}
	for _, banned := range []string{"verify", "complete", "generate", "update", "set", "tokens", "pin"} {
		for _, id := range ids {
			if (strings.Contains(id, ".verifications.") || strings.Contains(id, ".notifications.")) &&
				strings.Contains(id, banned) {
				t.Errorf("tool %s names %s", id, banned)
			}
		}
	}
	for _, d := range capabilityDescriptors(t) {
		if strings.Contains(d.ID, ".verifications.") || strings.Contains(d.ID, ".notifications.") {
			if d.Risk.Effect != capability.EffectRead || d.Risk.Idempotency != capability.IdempotencySafe ||
				d.Risk.Confirmation != capability.ConfirmationNone {
				t.Errorf("%s risk = %+v, want a safe read", d.ID, d.Risk)
			}
		}
	}
}

func capabilityDescriptors(t *testing.T) []capability.Descriptor {
	t.Helper()
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	return reg.Provider(Provider)
}

// Only the fetch of the verification options is a POST, and every request of these groups goes to a fixed
// origin with the fixed path of its method.
func TestNoRequestOfTheGroupsChangesAnything(t *testing.T) {
	env, methods := newVerificationsEnvironment(t)
	for tool, args := range map[string]string{
		verificationsListID:          `{` + verificationLocation + `}`,
		verificationsFetchOptionsID:  `{` + verificationLocation + `,"language_code":"en"}`,
		verificationsMerchantStateID: `{` + verificationLocation + `}`,
		notificationsGetID:           `{"account":"` + boundAccount + `"}`,
	} {
		if _, err := env.invoke(t, tool, "all", args); err != nil {
			t.Fatalf("%s: %v", tool, err)
		}
	}
	calls, _ := env.fake.calls()
	posts := 0
	for i, call := range calls {
		if (*methods)[i] == http.MethodPost {
			posts++
			if !strings.HasSuffix(call.path, ":fetchVerificationOptions") {
				t.Errorf("POST to %s", call.path)
			}
		}
		for _, banned := range []string{":verify", ":complete", ":generate", "verificationTokens"} {
			if strings.Contains(call.path, banned) {
				t.Errorf("request to %s", call.path)
			}
		}
		if (*methods)[i] == http.MethodPatch || (*methods)[i] == http.MethodDelete || (*methods)[i] == http.MethodPut {
			t.Errorf("%s %s", (*methods)[i], call.path)
		}
	}
	if posts != 1 {
		t.Errorf("%d POST requests, want 1", posts)
	}
}
