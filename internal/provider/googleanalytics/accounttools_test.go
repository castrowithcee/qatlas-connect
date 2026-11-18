package googleanalytics

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const accountAnswer = `{"name":"accounts/100","displayName":"Shop","regionCode":"CH",` +
	`"createTime":"2024-01-02T03:04:05.123456Z","updateTime":"2024-02-03T04:05:06Z","deleted":true,` +
	`"gmpOrganization":"marketingplatformadmin.googleapis.com/organizations/abc","unknown":"x"}`

const sharingAnswer = `{"name":"accounts/100/dataSharingSettings","sharingWithGoogleProductsEnabled":true,` +
	`"sharingWithOthersEnabled":true,"sharingWithGoogleSupportEnabled":true,` +
	`"sharingWithGoogleAssignedSalesEnabled":true,"sharingWithGoogleAnySalesEnabled":true}`

func TestAccountsGetReturnsEveryMember(t *testing.T) {
	env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, accountAnswer)})
	result, err := env.call(accountsGetID, "read", `{"account":"accounts/100"}`)
	if err != nil {
		t.Fatal(err)
	}
	requests, _ := env.fake.seen()
	if len(requests) != 1 || requests[0].method != http.MethodGet || requests[0].path != "/v1beta/accounts/100" ||
		requests[0].rawQuery != "" {
		t.Fatalf("requests = %+v", requests)
	}
	var out account
	want := account{Name: "accounts/100", DisplayName: "Shop", RegionCode: "CH", CreateTime: "2024-01-02T03:04:05.123456Z",
		UpdateTime: "2024-02-03T04:05:06Z", Deleted: true,
		GmpOrganization: "marketingplatformadmin.googleapis.com/organizations/abc"}
	if err := json.Unmarshal([]byte(result), &out); err != nil || out != want {
		t.Fatalf("result = %s, %v", result, err)
	}
}

func TestAccountsGetDefaultsToTheOnlyBoundAccountAndChecksTheBinding(t *testing.T) {
	env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, accountAnswer)})
	if _, err := env.call(accountsGetID, "account", `{}`); err != nil {
		t.Fatal(err)
	}
	before, _ := env.fake.seen()
	for _, c := range []struct{ connection, arguments string }{
		{"account", `{"account":"accounts/900"}`},
		{"property", `{"account":"accounts/100"}`},
		{"read", `{}`},
		{"read", `{"account":"accounts/0100"}`},
	} {
		if _, err := env.call(accountsGetID, c.connection, c.arguments); !refusedInvalid(err) {
			t.Errorf("%s %s: %v", c.connection, c.arguments, err)
		}
	}
	after, _ := env.fake.seen()
	if len(after) != len(before) {
		t.Errorf("a refused call sent a request")
	}
}

func TestAccountsGetRefusesAnotherAccountInTheAnswer(t *testing.T) {
	env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"name":"accounts/900"}`)})
	if _, err := env.call(accountsGetID, "read", `{"account":"accounts/100"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("error = %v", err)
	}
}

func TestDataSharingSettingsReturnEverySwitch(t *testing.T) {
	env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, sharingAnswer)})
	result, err := env.call(accountsSharingID, "read", `{"account":"accounts/100"}`)
	if err != nil {
		t.Fatal(err)
	}
	requests, _ := env.fake.seen()
	if len(requests) != 1 || requests[0].path != "/v1beta/accounts/100/dataSharingSettings" {
		t.Fatalf("requests = %+v", requests)
	}
	var out map[string]any
	if json.Unmarshal([]byte(result), &out) != nil || len(out) != 5 || out["name"] != "accounts/100/dataSharingSettings" ||
		out["sharingWithGoogleProductsEnabled"] != true || out["sharingWithOthersEnabled"] != true ||
		out["sharingWithGoogleSupportEnabled"] != true || out["sharingWithGoogleAssignedSalesEnabled"] != true {
		t.Fatalf("result = %s", result)
	}
	if _, err := env.call(accountsSharingID, "property", `{"account":"accounts/100"}`); !refusedInvalid(err) {
		t.Errorf("property-only binding: %v", err)
	}
	env = newEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"name":"accounts/900/dataSharingSettings"}`)})
	if _, err := env.call(accountsSharingID, "read", `{"account":"accounts/100"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("answer of another account: %v", err)
	}
}

func TestAccountsUpdateBuildsTheMaskFromTheGivenArguments(t *testing.T) {
	for _, c := range []struct{ arguments, mask, body string }{
		{`{"account":"accounts/100","display_name":"Shop"}`, "updateMask=display_name", `{"displayName":"Shop"}`},
		{`{"account":"accounts/100","region_code":"CH"}`, "updateMask=region_code", `{"regionCode":"CH"}`},
		{`{"account":"accounts/100","display_name":"Shop","region_code":"CH"}`, "updateMask=display_name%2Cregion_code",
			`{"displayName":"Shop","regionCode":"CH"}`},
	} {
		env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, accountAnswer)})
		if _, err := env.confirmed(accountsUpdateID, "write", c.arguments); err != nil {
			t.Fatal(err)
		}
		requests, _ := env.fake.seen()
		if len(requests) != 1 || requests[0].method != http.MethodPatch || requests[0].path != "/v1beta/accounts/100" ||
			requests[0].rawQuery != c.mask || requests[0].body != c.body {
			t.Errorf("%s: requests = %+v", c.arguments, requests)
		}
	}
}

func TestAccountsUpdateRefusals(t *testing.T) {
	env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, accountAnswer)})
	for _, c := range []struct{ name, connection, arguments string }{
		{"no change", "write", `{"account":"accounts/100"}`},
		{"empty name", "write", `{"account":"accounts/100","display_name":""}`},
		{"control character", "write", `{"account":"accounts/100","display_name":"a\u0007b"}`},
		{"too long", "write", `{"account":"accounts/100","display_name":"` + strings.Repeat("\U0001F600", 51) + `"}`},
		{"region", "write", `{"account":"accounts/100","region_code":"ch"}`},
		{"foreign account", "write-account", `{"account":"accounts/900","display_name":"Shop"}`},
		{"property binding", "property", `{"account":"accounts/100","display_name":"Shop"}`},
	} {
		if _, err := env.confirmed(accountsUpdateID, c.connection, c.arguments); err == nil {
			t.Errorf("%s: accepted", c.name)
		}
	}
	if requests, _ := env.fake.seen(); len(requests) != 0 {
		t.Errorf("a refused call sent %d requests", len(requests))
	}
	if _, err := env.call(accountsUpdateID, "write", `{"account":"accounts/100","display_name":"Shop"}`); err == nil {
		t.Error("an update without confirmation was accepted")
	}
	if _, err := env.confirmed(accountsUpdateID, "read", `{"account":"accounts/100","display_name":"Shop"}`); err == nil {
		t.Error("an update on a read-only connection was accepted")
	}
	if requests, _ := env.fake.seen(); len(requests) != 0 {
		t.Errorf("a refused call sent %d requests", len(requests))
	}
}

func TestAccountsDeleteNeedsTheExplicitBindingOfExactlyThatAccount(t *testing.T) {
	env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, `{}`)})
	for _, c := range []struct{ name, connection, account string }{
		{"no targets", "delete-open", "accounts/100"},
		{"property bound", "delete-property", "accounts/100"},
		{"account and property bound", "delete-mixed", "accounts/100"},
		{"another account", "delete-account", "accounts/900"},
		{"one of two bound accounts, not bound", "delete-two", "accounts/102"},
		{"not listed", "delete-unlisted", "accounts/100"},
		{"read only", "account", "accounts/100"},
	} {
		if _, err := env.confirmed(accountsDeleteID, c.connection, `{"account":"`+c.account+`"}`); err == nil {
			t.Errorf("%s: accepted", c.name)
		}
	}
	if _, err := env.call(accountsDeleteID, "delete-account", `{"account":"accounts/100"}`); err == nil {
		t.Error("a delete without confirmation was accepted")
	}
	if requests, grants := env.fake.seen(); len(requests) != 0 || grants != 0 {
		t.Fatalf("a refused call sent %d requests and asked for %d tokens", len(requests), grants)
	}
	for _, c := range []struct{ connection, account string }{
		{"delete-account", "accounts/100"}, {"delete-two", "accounts/101"}} {
		result, err := env.confirmed(accountsDeleteID, c.connection, `{"account":"`+c.account+`"}`)
		if err != nil || !strings.Contains(result, c.account) || !strings.Contains(result, `"deleted":true`) {
			t.Errorf("%s: %s, %v", c.connection, result, err)
		}
	}
	requests, _ := env.fake.seen()
	if len(requests) != 2 || requests[0].method != http.MethodDelete || requests[0].path != "/v1beta/accounts/100" ||
		requests[1].path != "/v1beta/accounts/101" {
		t.Fatalf("requests = %+v", requests)
	}
}

func TestAccountsDeleteIsNeverOfferedByAProfile(t *testing.T) {
	metadata, _ := registry(t).ProviderMetadata(Provider)
	for _, profile := range metadata.Profiles {
		if strings.Contains(strings.Join(profile.Tools, " "), "accounts.delete") {
			t.Errorf("profile %s holds accounts.delete", profile.ID)
		}
	}
}

func TestAccountChangesAreSentOnceAndSayWhenTheOutcomeIsUnknown(t *testing.T) {
	for _, c := range []struct {
		name    string
		api     func(int, *http.Request) (*http.Response, error)
		class   provider.Class
		unknown bool
		grants  int
	}{
		{"401", always(http.StatusUnauthorized, errorObject(401, "UNAUTHENTICATED", "CREDENTIALS_MISSING")), provider.ClassAuth, false, 1},
		{"403", always(http.StatusForbidden, errorObject(403, "PERMISSION_DENIED", "ACCESS_TOKEN_SCOPE_INSUFFICIENT")),
			provider.ClassPermission, false, 1},
		{"404", always(http.StatusNotFound, errorObject(404, "NOT_FOUND", "")), provider.ClassNotFound, false, 1},
		{"429", always(http.StatusTooManyRequests, errorObject(429, "RESOURCE_EXHAUSTED", "RATE_LIMIT_EXCEEDED")),
			provider.ClassRateLimited, false, 1},
		{"500", always(http.StatusInternalServerError, errorObject(500, "INTERNAL", "")), provider.ClassProviderError, true, 1},
		{"timeout", func(int, *http.Request) (*http.Response, error) { return nil, timeoutError{} }, provider.ClassTimeout, true, 1},
		{"not json", always(http.StatusOK, `{"name":`), provider.ClassInvalidResponse, true, 1},
	} {
		for _, tool := range []struct{ id, connection, arguments string }{
			{accountsUpdateID, "write", `{"account":"accounts/100","display_name":"Shop"}`},
			{accountsDeleteID, "delete-account", `{"account":"accounts/100"}`},
		} {
			t.Run(c.name+" "+tool.id, func(t *testing.T) {
				if tool.id == accountsDeleteID && c.name == "not json" {
					t.Skip("a delete reads no answer")
				}
				env := newEnv(t, &fakeGoogle{api: c.api})
				_, err := env.confirmed(tool.id, tool.connection, tool.arguments)
				if classOf(err) != c.class {
					t.Fatalf("error = %v (%s), want %s", err, classOf(err), c.class)
				}
				if unknown := strings.Contains(err.Error(), "outcome of the change is unknown"); unknown != c.unknown {
					t.Errorf("unknown = %v in %q", unknown, err)
				}
				if leak := leaks(err.Error()); leak != "" {
					t.Errorf("message carries %s", leak)
				}
				requests, grants := env.fake.seen()
				if len(requests) != 1 || grants != c.grants {
					t.Errorf("requests = %d, grants = %d: the change was repeated", len(requests), grants)
				}
			})
		}
	}
}

func TestAccountToolsDeclareTheirRiskScopeAndProfile(t *testing.T) {
	reg := registry(t)
	metadata, _ := reg.ProviderMetadata(Provider)
	for id, want := range map[string][]string{accountsGetID: {scopeReadOnly}, accountsSharingID: {scopeReadOnly},
		accountsUpdateID: {scopeEdit}, accountsDeleteID: {scopeEdit}} {
		if !reflect.DeepEqual(metadata.Token.Scopes[id], want) {
			t.Errorf("%s scopes = %v", id, metadata.Token.Scopes[id])
		}
	}
	for _, d := range accountDescriptors() {
		var wantRisk capability.Risk
		switch d.ID {
		case accountsGetID, accountsSharingID:
			wantRisk = readRisk
		case accountsUpdateID:
			wantRisk = changeRisk(capability.EffectUpdate)
		case accountsDeleteID:
			wantRisk = changeRisk(capability.EffectDelete)
		}
		if d.Risk != wantRisk || d.RequiresToolAllowList != (d.ID == accountsDeleteID) {
			t.Errorf("%s risk = %+v, listed-only = %v", d.ID, d.Risk, d.RequiresToolAllowList)
		}
		if d.Risk.Confirmation == capability.ConfirmationRequired && reg.ConfirmationSummary(d.ID) == nil {
			t.Errorf("%s has no confirmation summary", d.ID)
		}
	}
	for _, profile := range metadata.Profiles {
		if profile.ID == "write" && (profile.Recommended || !strings.Contains(strings.Join(profile.Tools, " "), accountsUpdateID) ||
			len(profile.Tools) != len(readTools)+45) {
			t.Errorf("write profile = %+v", profile)
		}
	}
	if got := reg.ConfirmationSummary(accountsDeleteID)(json.RawMessage(`{"account":"accounts/100"}`)); !strings.Contains(got, "accounts/100") ||
		!strings.Contains(got, "35 days") {
		t.Errorf("delete summary = %q", got)
	}
	if got := reg.ConfirmationSummary(accountsUpdateID)(json.RawMessage(`{"account":"accounts/100","display_name":"Shop","region_code":"CH"}`)); !strings.Contains(got, `"Shop"`) ||
		!strings.Contains(got, "CH") {
		t.Errorf("update summary = %q", got)
	}
}
