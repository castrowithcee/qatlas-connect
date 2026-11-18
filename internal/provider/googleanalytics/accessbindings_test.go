package googleanalytics

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

func bindingsEnv(t *testing.T, fake *fakeGoogle) *testEnv {
	return readerEnv(t, fake, accessBindingsListID, accessBindingsGetID)
}

const (
	bindingOneJSON = `{"name":"properties/200/accessBindings/5001","user":"first.user@example.com",` +
		`"roles":["predefinedRoles/viewer","predefinedRoles/no-cost-data"],"extra":1}`
	bindingTwoJSON   = `{"name":"properties/200/accessBindings/5002","user":"second.user@example.com","roles":["predefinedRoles/admin"]}`
	bindingThreeJSON = `{"name":"properties/200/accessBindings/5003","user":"third.user@example.com","roles":["predefinedRoles/editor"]}`
)

type bindingsOut struct {
	AccessBindings []accessBinding `json:"accessBindings"`
	NextCursor     string          `json:"next_cursor"`
	HasMore        bool            `json:"has_more"`
}

func TestAccessBindingsListPagesThroughThreePagesOnAccountAndProperty(t *testing.T) {
	for _, c := range []struct{ parent, path string }{
		{"properties/200", "/v1alpha/properties/200/accessBindings"},
		{"accounts/100", "/v1alpha/accounts/100/accessBindings"},
	} {
		page := func(entry string) string {
			return strings.ReplaceAll(entry, "properties/200", c.parent)
		}
		pages := []string{
			`{"accessBindings":[` + page(bindingOneJSON) + `],"nextPageToken":"t2"}`,
			`{"accessBindings":[` + page(bindingTwoJSON) + `],"nextPageToken":"t3"}`,
			`{"accessBindings":[` + page(bindingThreeJSON) + `]}`,
		}
		env := bindingsEnv(t, &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
			return respond(r, http.StatusOK, "application/json", pages[n-1]), nil
		}})
		cursor, users := "", []string{}
		for i := 0; i < 3; i++ {
			args := `{"parent":"` + c.parent + `","page_size":1`
			if cursor != "" {
				args += `,"cursor":"` + cursor + `"`
			}
			result, err := env.call(accessBindingsListID, "sfree", args+`}`)
			var out bindingsOut
			if err != nil || json.Unmarshal([]byte(result), &out) != nil || len(out.AccessBindings) != 1 ||
				out.HasMore != (i < 2) || strings.Contains(result, "extra") {
				t.Fatalf("%s page %d = %s, %v", c.parent, i, result, err)
			}
			users = append(users, string(out.AccessBindings[0].User))
			cursor = out.NextCursor
		}
		requests, _ := env.fake.seen()
		if !slices.Equal(users, []string{"first.user@example.com", "second.user@example.com", "third.user@example.com"}) ||
			len(requests) != 3 || requests[0].method != http.MethodGet || requests[0].path != c.path ||
			requests[0].rawQuery != "pageSize=1" || !strings.Contains(requests[1].rawQuery, "pageToken=t2") ||
			!strings.Contains(requests[2].rawQuery, "pageToken=t3") {
			t.Errorf("%s: users %v, requests %+v", c.parent, users, requests)
		}
		if _, err := env.call(accessBindingsListID, "sfree", `{"parent":"accounts/101","cursor":"x"}`); !refusedInvalid(err) {
			t.Errorf("%s: a cursor of another parent: %v", c.parent, err)
		}
	}
}

func TestAccessBindingsGetReturnsEveryFieldWithSeveralRoles(t *testing.T) {
	env := bindingsEnv(t, &fakeGoogle{api: always(http.StatusOK, bindingOneJSON)})
	result, err := env.call(accessBindingsGetID, "sfree", `{"name":"properties/200/accessBindings/5001"}`)
	requests, _ := env.fake.seen()
	var got accessBinding
	if err != nil || json.Unmarshal([]byte(result), &got) != nil || got.Name != "properties/200/accessBindings/5001" ||
		got.User != "first.user@example.com" ||
		!slices.Equal(got.Roles, []code{"predefinedRoles/viewer", "predefinedRoles/no-cost-data"}) ||
		strings.Contains(result, "extra") || len(requests) != 1 || requests[0].path != "/v1alpha/properties/200/accessBindings/5001" {
		t.Errorf("get = %s, %v, %+v", result, err, requests)
	}
	env = bindingsEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"name":"accounts/100/accessBindings/6001",`+
		`"user":"owner@example.com","roles":["predefinedRoles/admin"]}`)})
	if result, err = env.call(accessBindingsGetID, "sfree", `{"name":"accounts/100/accessBindings/6001"}`); err != nil ||
		!strings.Contains(result, `"owner@example.com"`) {
		t.Errorf("account binding = %s, %v", result, err)
	}
	if requests, _ = env.fake.seen(); len(requests) != 1 || requests[0].path != "/v1alpha/accounts/100/accessBindings/6001" {
		t.Errorf("requests %+v", requests)
	}
	env = bindingsEnv(t, &fakeGoogle{api: always(http.StatusOK, bindingTwoJSON)})
	if _, err := env.call(accessBindingsGetID, "sfree", `{"name":"properties/200/accessBindings/5001"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("an answer for another binding: %v", err)
	}
}

func TestAccessBindingsEmptyListAndPageSize(t *testing.T) {
	env := bindingsEnv(t, &fakeGoogle{api: always(http.StatusOK, `{}`)})
	result, err := env.call(accessBindingsListID, "sfree", `{"parent":"properties/200","page_size":500}`)
	requests, _ := env.fake.seen()
	// Empty members are left out of a result.
	if err != nil || result != `{"has_more":false}` || requests[0].rawQuery != "pageSize=500" {
		t.Errorf("empty list = %s, %v, %+v", result, err, requests)
	}
	for _, args := range []string{`{"parent":"properties/200","page_size":501}`, `{"parent":"properties/200","page_size":0}`} {
		if _, err := env.call(accessBindingsListID, "sfree", args); err == nil {
			t.Errorf("%s accepted", args)
		}
	}
}

func TestAccessBindingsAnswerIsBoundedAndChecked(t *testing.T) {
	roles := make([]string, 30)
	for i := range roles {
		roles[i] = `"predefinedRoles/viewer"`
	}
	env := bindingsEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"accessBindings":[`+
		`{"name":"properties/200/accessBindings/1","user":"`+strings.Repeat("u", 400)+`@example.com","roles":[`+
		strings.Join(roles, ",")+`,"bad role"]},`+
		`{"name":"properties/999/accessBindings/2","user":"other@example.com"},`+
		`{"name":"accounts/200/accessBindings/3","user":"other@example.com"},`+
		`{"name":"properties/200/accessBindings/x"},{"name":"nonsense"}]}`)})
	result, err := env.call(accessBindingsListID, "sfree", `{"parent":"properties/200"}`)
	var out bindingsOut
	if err != nil || json.Unmarshal([]byte(result), &out) != nil || len(out.AccessBindings) != 1 {
		t.Fatalf("result %d, %v", len(result), err)
	}
	b := out.AccessBindings[0]
	if len([]rune(string(b.User))) != maxTextRunes+1 || len(b.Roles) != maxBindingRoles || strings.Contains(result, "other@") {
		t.Errorf("binding = %d %d", len(b.User), len(b.Roles))
	}
}

func TestAccessBindingsAccountParentIsAccountWide(t *testing.T) {
	for _, tool := range []struct{ id, arguments string }{
		{accessBindingsListID, `{"parent":"ACC"}`},
		{accessBindingsGetID, `{"name":"ACC/accessBindings/6001"}`},
	} {
		for _, c := range []struct {
			connection, account string
			refused             bool
		}{
			{"sfree", "accounts/100", false},
			{"saccount", "accounts/100", false},
			{"saccount", "accounts/900", true},
			{"sproperty", "accounts/100", true},
			{"smixed", "accounts/100", false},
			{"smixed", "accounts/900", true},
		} {
			env := bindingsEnv(t, &fakeGoogle{api: func(_ int, r *http.Request) (*http.Response, error) {
				return respond(r, http.StatusOK, "application/json",
					`{"name":"`+strings.TrimPrefix(r.URL.Path, "/v1alpha/")+`"}`), nil
			}})
			result, err := env.call(tool.id, c.connection, strings.ReplaceAll(tool.arguments, "ACC", c.account))
			requests, _ := env.fake.seen()
			if (err != nil) != c.refused || len(requests) != map[bool]int{true: 0, false: 1}[c.refused] {
				t.Errorf("%s %s %s: %v, requests %d", tool.id, c.connection, c.account, err, len(requests))
			}
			if c.refused {
				env.noIO(t)
				if !refusedInvalid(err) || strings.Contains(err.Error(), "900") || strings.Contains(result, "900") ||
					strings.Contains(err.Error(), "100") {
					t.Errorf("%s %s %s: refusal %v", tool.id, c.connection, c.account, err)
				}
			}
		}
	}
}

func TestAccessBindingsPropertyParentFollowsTheBinding(t *testing.T) {
	runBindingCases(t, accessBindingsListID, `{"parent":"PROP"}`, bindingsEnv)
	runBindingCases(t, accessBindingsGetID, `{"name":"PROP/accessBindings/5001"}`, bindingsEnv)
}

func TestAccessBindingErrorsAreClassified(t *testing.T) {
	runErrorCases(t, bindingsEnv, map[string]string{
		accessBindingsListID: `{"parent":"accounts/100"}`,
		accessBindingsGetID:  `{"name":"properties/200/accessBindings/5001"}`,
	})
	runErrorCases(t, bindingsEnv, map[string]string{accessBindingsListID: `{"parent":"properties/200"}`})
}

func TestAccessBindingArgumentsAreRefusedBeforeAnyIO(t *testing.T) {
	env := bindingsEnv(t, &fakeGoogle{api: always(http.StatusOK, `{}`)})
	for id, list := range map[string][]string{
		accessBindingsListID: {`{}`, `{"parent":"accountSummaries/100"}`, `{"parent":"accounts/0100"}`,
			`{"parent":"properties/200/dataStreams/1"}`, `{"parent":"accounts/100","user":"a@example.com"}`},
		accessBindingsGetID: {`{}`, `{"name":"accounts/100"}`, `{"name":"accounts/100/accessBindings/x"}`,
			`{"name":"datastreams/100/accessBindings/1"}`, `{"name":"accounts/100/accessBindings/1/x"}`,
			`{"name":"properties/200/rollupPropertySourceLinks/1"}`},
	} {
		for _, args := range list {
			if _, err := env.call(id, "sfree", args); err == nil {
				t.Errorf("%s %s: accepted", id, args)
			}
		}
	}
	env.noIO(t)
}

func TestAccessBindingToolsCarryThePersonalDataWarning(t *testing.T) {
	reg := registry(t)
	metadata, _ := reg.ProviderMetadata(Provider)
	profiles := map[string][]string{}
	for _, p := range metadata.Profiles {
		profiles[p.ID] = p.Tools
	}
	for _, id := range []string{accessBindingsListID, accessBindingsGetID} {
		descriptor, _, ok := reg.Lookup(id)
		if !ok || descriptor.Risk != bindingRisk || descriptor.Risk.DataSensitivity != dataSensitivityBindings ||
			descriptor.RequiresToolAllowList || !slices.Contains(descriptor.Tags, "personal-data") ||
			!strings.Contains(descriptor.Description, "PERSONAL DATA") ||
			!strings.Contains(descriptor.Description, "ALPHA") || !strings.Contains(descriptor.Description, "untrusted") {
			t.Errorf("%s: %+v", id, descriptor)
		}
		if !slices.Equal(metadata.Token.Scopes[id], []string{scopeUsersReadOnly}) ||
			!slices.Contains(profiles["read"], id) || !slices.Contains(profiles["write"], id) {
			t.Errorf("%s: scopes %v, profiles wrong", id, metadata.Token.Scopes[id])
		}
	}
	if scopeUsersReadOnly != "https://www.googleapis.com/auth/analytics.manage.users.readonly" {
		t.Errorf("scope = %s", scopeUsersReadOnly)
	}
}
