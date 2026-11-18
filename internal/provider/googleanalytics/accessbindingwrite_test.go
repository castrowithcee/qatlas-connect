package googleanalytics

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

func bindingWriteEnv(t *testing.T, fake *fakeGoogle) *testEnv {
	return readerEnv(t, fake, accessBindingsListID, accessBindingsGetID, accessBindingsBatchGetID,
		accessBindingsUpdateID, accessBindingsDeleteID, accessBindingsBatchDeleteID)
}

const (
	bindingUpdateArgs = `{"name":"properties/200/accessBindings/5001","roles":["predefinedRoles/analyst"],` +
		`"previous_user":"First.User@example.com","previous_roles":["predefinedRoles/no-cost-data","predefinedRoles/viewer"]}`
	bindingUpdatedJSON = `{"name":"properties/200/accessBindings/5001","user":"first.user@example.com",` +
		`"roles":["predefinedRoles/analyst"]}`
)

// bindingServer answers a read of a binding with bindingOneJSON (or its account form) and a change with
// bindingUpdatedJSON in the same form.
func bindingServer(n int, r *http.Request) (*http.Response, error) {
	body := bindingOneJSON
	if r.Method == http.MethodPatch {
		body = bindingUpdatedJSON
	}
	if strings.HasPrefix(r.URL.Path, "/v1alpha/accounts/100/") {
		body = strings.ReplaceAll(body, "properties/200", "accounts/100")
	}
	return respond(r, http.StatusOK, "application/json", body), nil
}

func TestAccessBindingsBatchGetReadsOnlyTheAskedBindings(t *testing.T) {
	for _, c := range []struct{ parent, path string }{
		{"properties/200", "/v1alpha/properties/200/accessBindings:batchGet"},
		{"accounts/100", "/v1alpha/accounts/100/accessBindings:batchGet"},
	} {
		form := func(entry string) string { return strings.ReplaceAll(entry, "properties/200", c.parent) }
		env := bindingWriteEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"accessBindings":[`+form(bindingOneJSON)+
			`,`+form(bindingTwoJSON)+`,`+form(bindingOneJSON)+`,`+form(bindingThreeJSON)+
			`,{"name":"properties/999/accessBindings/5001","user":"other@example.com"}]}`)})
		args := form(`{"parent":"properties/200","names":["properties/200/accessBindings/5001",` +
			`"properties/200/accessBindings/5002"]}`)
		result, err := env.call(accessBindingsBatchGetID, "sfree", args)
		requests, _ := env.fake.seen()
		var out bindingsOut
		if err != nil || json.Unmarshal([]byte(result), &out) != nil || len(out.AccessBindings) != 2 ||
			string(out.AccessBindings[0].User) != "first.user@example.com" ||
			string(out.AccessBindings[1].User) != "second.user@example.com" || strings.Contains(result, "third.user") ||
			strings.Contains(result, "other@") || strings.Contains(result, "extra") || strings.Contains(result, "has_more") {
			t.Fatalf("%s: %s, %v", c.parent, result, err)
		}
		want := "names=" + strings.ReplaceAll(form("properties/200/accessBindings/5001"), "/", "%2F") +
			"&names=" + strings.ReplaceAll(form("properties/200/accessBindings/5002"), "/", "%2F")
		if len(requests) != 1 || requests[0].method != http.MethodGet || requests[0].path != c.path ||
			requests[0].rawQuery != want {
			t.Errorf("%s: requests %+v", c.parent, requests)
		}
	}
	env := bindingWriteEnv(t, &fakeGoogle{api: always(http.StatusOK, `{}`)})
	if result, err := env.call(accessBindingsBatchGetID, "sfree",
		`{"parent":"properties/200","names":["properties/200/accessBindings/5001"]}`); err != nil ||
		result != `{}` && result != `{"accessBindings":[]}` {
		t.Errorf("empty answer = %s, %v", result, err)
	}
}

func TestAccessBindingsUpdateSendsTheRolesAloneAfterComparingThePreviousValues(t *testing.T) {
	env := bindingWriteEnv(t, &fakeGoogle{api: bindingServer})
	if _, err := env.call(accessBindingsUpdateID, "sfree", bindingUpdateArgs); err == nil {
		t.Fatal("an update without confirm was accepted")
	}
	env.noIO(t)
	result, err := env.confirmed(accessBindingsUpdateID, "sfree", bindingUpdateArgs)
	requests, _ := env.fake.seen()
	if err != nil || !strings.Contains(result, `"predefinedRoles/analyst"`) || len(requests) != 2 ||
		requests[0].method != http.MethodGet || requests[0].path != "/v1alpha/properties/200/accessBindings/5001" ||
		requests[1].method != http.MethodPatch || requests[1].path != "/v1alpha/properties/200/accessBindings/5001" ||
		requests[1].rawQuery != "" || requests[1].body != `{"roles":["predefinedRoles/analyst"]}` {
		t.Fatalf("update = %s, %v, %+v", result, err, requests)
	}
	env = bindingWriteEnv(t, &fakeGoogle{api: bindingServer})
	accountArgs := strings.ReplaceAll(bindingUpdateArgs, "properties/200", "accounts/100")
	if _, err := env.confirmed(accessBindingsUpdateID, "saccount", accountArgs); err != nil {
		t.Errorf("account binding: %v", err)
	}
	if requests, _ = env.fake.seen(); len(requests) != 2 || requests[1].path != "/v1alpha/accounts/100/accessBindings/5001" {
		t.Errorf("account requests %+v", requests)
	}
}

func TestAccessBindingsUpdateRefusesStalePreviousValuesWithoutWriting(t *testing.T) {
	for _, args := range []string{
		strings.Replace(bindingUpdateArgs, "First.User@example.com", "someone.else@example.com", 1),
		strings.Replace(bindingUpdateArgs, `["predefinedRoles/no-cost-data","predefinedRoles/viewer"]`,
			`["predefinedRoles/viewer"]`, 1),
		strings.Replace(bindingUpdateArgs, `"predefinedRoles/no-cost-data",`, `"predefinedRoles/editor",`, 1),
	} {
		env := bindingWriteEnv(t, &fakeGoogle{api: bindingServer})
		_, err := env.confirmed(accessBindingsUpdateID, "sfree", args)
		requests, _ := env.fake.seen()
		if !refusedInvalid(err) || !strings.Contains(err.Error(), "nothing was changed") || len(requests) != 1 ||
			requests[0].method != http.MethodGet || strings.Contains(err.Error(), "example.com") {
			t.Errorf("%s: %v, %+v", args, err, requests)
		}
	}
}

func TestAccessBindingsUpdateHasNoUserArgument(t *testing.T) {
	reg := registry(t)
	descriptor, _, ok := reg.Lookup(accessBindingsUpdateID)
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if !ok || json.Unmarshal(descriptor.InputSchema, &schema) != nil {
		t.Fatalf("descriptor %+v", descriptor)
	}
	names := []string{}
	for name := range schema.Properties {
		names = append(names, name)
	}
	slices.Sort(names)
	if !slices.Equal(names, []string{"name", "previous_roles", "previous_user", "roles"}) {
		t.Errorf("arguments = %v", names)
	}
	for _, argument := range descriptor.Arguments {
		if argument.Name == "user" || argument.Name == "update_mask" {
			t.Errorf("argument %s", argument.Name)
		}
	}
	env := bindingWriteEnv(t, &fakeGoogle{api: bindingServer})
	withUser := strings.Replace(bindingUpdateArgs, `{"name"`, `{"user":"new.user@example.com","name"`, 1)
	if _, err := env.confirmed(accessBindingsUpdateID, "sfree", withUser); err == nil {
		t.Error("a user argument was accepted")
	}
	env.noIO(t)
}

func TestAccessBindingsUpdateRefusesBadRolesBeforeAnyIO(t *testing.T) {
	env := bindingWriteEnv(t, &fakeGoogle{api: bindingServer})
	for _, args := range []string{
		strings.Replace(bindingUpdateArgs, `"roles":["predefinedRoles/analyst"]`, `"roles":[]`, 1),
		strings.Replace(bindingUpdateArgs, `"roles":["predefinedRoles/analyst"]`, `"roles":["predefinedRoles/owner"]`, 1),
		strings.Replace(bindingUpdateArgs, `"roles":["predefinedRoles/analyst"]`, `"roles":["analyst"]`, 1),
		strings.Replace(bindingUpdateArgs, `"roles":["predefinedRoles/analyst"]`,
			`"roles":["predefinedRoles/analyst","predefinedRoles/analyst"]`, 1),
		strings.Replace(bindingUpdateArgs, `,"previous_user":"First.User@example.com"`, ``, 1),
		strings.Replace(bindingUpdateArgs, `"previous_roles":["predefinedRoles/no-cost-data","predefinedRoles/viewer"]`,
			`"previous_roles":[]`, 1),
		strings.Replace(bindingUpdateArgs, "properties/200/accessBindings/5001", "properties/200/accessBindings/x", 1),
		strings.Replace(bindingUpdateArgs, "properties/200/accessBindings/5001", "properties/200", 1),
	} {
		if _, err := env.confirmed(accessBindingsUpdateID, "sfree", args); err == nil {
			t.Errorf("%s accepted", args)
		}
	}
	env.noIO(t)
	// The handler checks the roles itself, also when the schema is not applied.
	for _, roles := range [][]string{nil, {}, {"predefinedRoles/owner"}, {"predefinedRoles/viewer", "predefinedRoles/viewer"}} {
		if err := checkRoles(roles); !refusedInvalid(err) {
			t.Errorf("roles %v: %v", roles, err)
		}
	}
	for _, role := range predefinedRoles {
		if err := checkRoles([]string{role}); err != nil {
			t.Errorf("%s: %v", role, err)
		}
	}
}

func TestAccessBindingsUpdateDistrustsAnAnswerOfAnotherBinding(t *testing.T) {
	env := bindingWriteEnv(t, &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodPatch {
			return respond(r, http.StatusOK, "application/json", bindingTwoJSON), nil
		}
		return bindingServer(n, r)
	}})
	if _, err := env.confirmed(accessBindingsUpdateID, "sfree", bindingUpdateArgs); classOf(err) != provider.ClassProviderError ||
		!strings.Contains(err.Error(), "unknown") {
		t.Errorf("update: %v", err)
	}
	env = bindingWriteEnv(t, &fakeGoogle{api: always(http.StatusOK, bindingTwoJSON)})
	_, err := env.confirmed(accessBindingsUpdateID, "sfree", bindingUpdateArgs)
	if requests, _ := env.fake.seen(); classOf(err) != provider.ClassInvalidResponse || len(requests) != 1 {
		t.Errorf("a read of another binding: %v, requests %d", err, len(requests))
	}
}

func TestAccessBindingsDelete(t *testing.T) {
	env := bindingWriteEnv(t, &fakeGoogle{api: always(http.StatusOK, `{}`)})
	args := `{"name":"properties/200/accessBindings/5001"}`
	if _, err := env.call(accessBindingsDeleteID, "sfree", args); err == nil {
		t.Fatal("delete without confirm was accepted")
	}
	env.noIO(t)
	result, err := env.confirmed(accessBindingsDeleteID, "sfree", args)
	requests, _ := env.fake.seen()
	if err != nil || len(requests) != 1 || requests[0].method != http.MethodDelete ||
		requests[0].path != "/v1alpha/properties/200/accessBindings/5001" || requests[0].body != "" ||
		result != `{"deleted":true,"name":"properties/200/accessBindings/5001"}` {
		t.Fatalf("%v, %+v, %s", err, requests, result)
	}
}

func TestAccessBindingsBatchDeleteSendsEveryNameOnce(t *testing.T) {
	env := bindingWriteEnv(t, &fakeGoogle{api: always(http.StatusOK, `{}`)})
	args := `{"parent":"accounts/100","names":["accounts/100/accessBindings/6001","accounts/100/accessBindings/6002"]}`
	if _, err := env.call(accessBindingsBatchDeleteID, "saccount", args); err == nil {
		t.Fatal("batch delete without confirm was accepted")
	}
	env.noIO(t)
	result, err := env.confirmed(accessBindingsBatchDeleteID, "saccount", args)
	requests, _ := env.fake.seen()
	if err != nil || len(requests) != 1 || requests[0].method != http.MethodPost ||
		requests[0].path != "/v1alpha/accounts/100/accessBindings:batchDelete" || requests[0].rawQuery != "" ||
		requests[0].body != `{"requests":[{"name":"accounts/100/accessBindings/6001"},{"name":"accounts/100/accessBindings/6002"}]}` ||
		result != `{"deleted":true,"names":["accounts/100/accessBindings/6001","accounts/100/accessBindings/6002"],"parent":"accounts/100"}` {
		t.Fatalf("%v, %+v, %s", err, requests, result)
	}
}

func TestAccessBindingsBatchDeleteRefusalSaysThatPartsMayBeGone(t *testing.T) {
	args := `{"parent":"properties/200","names":["properties/200/accessBindings/5001","properties/200/accessBindings/5002"]}`
	for _, c := range []struct {
		status int
		class  provider.Class
	}{
		{http.StatusNotFound, provider.ClassNotFound},
		{http.StatusForbidden, provider.ClassPermission},
		{http.StatusBadRequest, provider.ClassProviderError},
	} {
		env := bindingWriteEnv(t, &fakeGoogle{api: always(c.status, errorObject(c.status, "FAILED", ""))})
		_, err := env.confirmed(accessBindingsBatchDeleteID, "sfree", args)
		requests, _ := env.fake.seen()
		if classOf(err) != c.class || !strings.Contains(err.Error(), "batch-get") ||
			!strings.Contains(err.Error(), "no partial result") || len(requests) != 1 || leaks(err.Error()) != "" {
			t.Errorf("%d: %v, requests %d", c.status, err, len(requests))
		}
	}
	env := bindingWriteEnv(t, &fakeGoogle{api: always(http.StatusInternalServerError, errorObject(500, "INTERNAL", ""))})
	if _, err := env.confirmed(accessBindingsBatchDeleteID, "sfree", args); !strings.Contains(err.Error(), "unknown") ||
		strings.Contains(err.Error(), "no partial result") {
		t.Errorf("500: %v", err)
	}
	if requests, _ := env.fake.seen(); len(requests) != 1 {
		t.Errorf("500 sent %d requests, want exactly one", len(requests))
	}
}

func TestAccessBindingWritesFollowTheBindingBeforeAnyIO(t *testing.T) {
	for _, tool := range []struct {
		id, arguments string
		methods       []string
	}{
		{accessBindingsBatchGetID, `{"parent":"PROP","names":["PROP/accessBindings/5001"]}`, []string{"GET"}},
		{accessBindingsUpdateID, `{"name":"PROP/accessBindings/5001","roles":["predefinedRoles/editor"],` +
			`"previous_user":"first.user@example.com","previous_roles":["predefinedRoles/viewer","predefinedRoles/no-cost-data"]}`,
			[]string{"GET", "PATCH"}},
		{accessBindingsDeleteID, `{"name":"PROP/accessBindings/5001"}`, []string{"DELETE"}},
		{accessBindingsBatchDeleteID, `{"parent":"PROP","names":["PROP/accessBindings/5001"]}`, []string{"POST"}},
	} {
		for _, c := range streamBindingCases {
			answers := map[string]string{
				"/v1beta/properties/200": `{"name":"properties/200","parent":"accounts/100"}`,
				"/v1beta/properties/201": `{"name":"properties/201","parent":"accounts/100"}`,
				"/v1beta/properties/901": `{"name":"properties/901","parent":"accounts/900"}`,
			}
			env := bindingWriteEnv(t, &fakeGoogle{api: func(_ int, r *http.Request) (*http.Response, error) {
				if body, ok := answers[r.URL.Path]; ok {
					return respond(r, http.StatusOK, "application/json", body), nil
				}
				name := strings.TrimPrefix(r.URL.Path, "/v1alpha/")
				switch {
				case strings.HasSuffix(name, ":batchGet"):
					name = r.URL.Query().Get("names")
					return respond(r, http.StatusOK, "application/json", `{"accessBindings":[{"name":"`+name+`"}]}`), nil
				case strings.HasSuffix(name, ":batchDelete"):
					return respond(r, http.StatusOK, "application/json", `{}`), nil
				}
				return respond(r, http.StatusOK, "application/json", `{"name":"`+name+`","user":"first.user@example.com",`+
					`"roles":["predefinedRoles/viewer","predefinedRoles/no-cost-data"]}`), nil
			}})
			result, err := env.confirmed(tool.id, c.connection, strings.ReplaceAll(tool.arguments, "PROP", c.property))
			requests, _ := env.fake.seen()
			var want []string
			for _, method := range c.methods {
				if method == "X" {
					want = append(want, tool.methods...)
					continue
				}
				want = append(want, method)
			}
			if (err != nil) != c.refused || !slices.Equal(methodsOf(requests), want) {
				t.Errorf("%s %s %s: %v, requests %v, want %v", tool.id, c.connection, c.property, err, methodsOf(requests), want)
			}
			if c.refused && (!refusedInvalid(err) || strings.Contains(err.Error(), "901") || strings.Contains(result, "901")) {
				t.Errorf("%s %s %s: refusal %v", tool.id, c.connection, c.property, err)
			}
			if c.refused && len(c.methods) == 0 {
				env.noIO(t)
			}
		}
	}
}

func TestAccessBindingWritesOnAnAccountAreAccountWide(t *testing.T) {
	for _, tool := range []string{
		`{"name":"ACC/accessBindings/6001"}`,
		`{"parent":"ACC","names":["ACC/accessBindings/6001"]}`,
	} {
		id := accessBindingsDeleteID
		if strings.Contains(tool, "parent") {
			id = accessBindingsBatchDeleteID
		}
		for _, c := range []struct {
			connection, account string
			refused             bool
		}{
			{"sfree", "accounts/100", false},
			{"saccount", "accounts/100", false},
			{"saccount", "accounts/900", true},
			{"sproperty", "accounts/100", true},
			{"smixed", "accounts/900", true},
		} {
			env := bindingWriteEnv(t, &fakeGoogle{api: always(http.StatusOK, `{}`)})
			_, err := env.confirmed(id, c.connection, strings.ReplaceAll(tool, "ACC", c.account))
			requests, _ := env.fake.seen()
			if (err != nil) != c.refused || len(requests) != map[bool]int{true: 0, false: 1}[c.refused] {
				t.Errorf("%s %s %s: %v, requests %d", id, c.connection, c.account, err, len(requests))
			}
			if c.refused {
				env.noIO(t)
				if !refusedInvalid(err) || strings.Contains(err.Error(), "900") {
					t.Errorf("%s %s %s: refusal %v", id, c.connection, c.account, err)
				}
			}
		}
	}
}

func TestAccessBindingBatchNamesAreRefusedBeforeAnyIO(t *testing.T) {
	env := bindingWriteEnv(t, &fakeGoogle{api: always(http.StatusOK, `{}`)})
	tooMany := make([]string, maxBatchBindings+1)
	for i := range tooMany {
		tooMany[i] = `"properties/200/accessBindings/` + string(rune('1'+i%9)) + strings.Repeat("0", i/9+1) + `"`
	}
	for _, args := range []string{
		`{"parent":"properties/200"}`,
		`{"parent":"properties/200","names":[]}`,
		`{"parent":"properties/200","names":["properties/201/accessBindings/5001"]}`,
		`{"parent":"accounts/100","names":["properties/200/accessBindings/5001"]}`,
		`{"parent":"properties/200","names":["properties/200/accessBindings/5001","properties/200/accessBindings/5001"]}`,
		`{"parent":"properties/200","names":["properties/200/accessBindings/x"]}`,
		`{"parent":"properties/200","names":["properties/200"]}`,
		`{"parent":"properties/200/accessBindings/5001","names":["properties/200/accessBindings/5001"]}`,
		`{"parent":"properties/200","names":[` + strings.Join(tooMany, ",") + `]}`,
	} {
		for _, id := range []string{accessBindingsBatchGetID, accessBindingsBatchDeleteID} {
			if _, err := env.confirmed(id, "sfree", args); err == nil {
				t.Errorf("%s %.120s: accepted", id, args)
			}
		}
	}
	for _, args := range []string{`{}`, `{"name":"accounts/100"}`, `{"name":"properties/200/accessBindings/1/x"}`,
		`{"name":"properties/200/rollupPropertySourceLinks/1"}`} {
		if _, err := env.confirmed(accessBindingsDeleteID, "sfree", args); err == nil {
			t.Errorf("delete %s: accepted", args)
		}
	}
	env.noIO(t)
	// The handler checks the batch itself, also when the schema is not applied.
	for _, a := range []bindingBatchArguments{
		{Parent: "properties/200"},
		{Parent: "properties/200", Names: []string{"properties/201/accessBindings/1"}},
		{Parent: "properties/200", Names: []string{"properties/200/accessBindings/1", "properties/200/accessBindings/1"}},
		{Parent: "x", Names: []string{"properties/200/accessBindings/1"}},
	} {
		if err := checkBatch(a); !refusedInvalid(err) {
			t.Errorf("%+v: %v", a, err)
		}
	}
}

func TestAccessBindingWriteErrorsAreClassifiedAndNeverRepeat(t *testing.T) {
	for _, c := range []struct {
		name   string
		status int
		class  provider.Class
	}{
		{"403", http.StatusForbidden, provider.ClassPermission},
		{"404", http.StatusNotFound, provider.ClassNotFound},
		{"429", http.StatusTooManyRequests, provider.ClassRateLimited},
	} {
		for id, args := range map[string]string{
			accessBindingsBatchGetID:    `{"parent":"properties/200","names":["properties/200/accessBindings/5001"]}`,
			accessBindingsDeleteID:      `{"name":"properties/200/accessBindings/5001"}`,
			accessBindingsBatchDeleteID: `{"parent":"properties/200","names":["properties/200/accessBindings/5001"]}`,
			accessBindingsUpdateID:      bindingUpdateArgs,
		} {
			env := bindingWriteEnv(t, &fakeGoogle{api: func(_ int, r *http.Request) (*http.Response, error) {
				if r.Method == http.MethodGet && id == accessBindingsUpdateID {
					return respond(r, http.StatusOK, "application/json", bindingOneJSON), nil
				}
				return respond(r, c.status, "application/json", errorObject(c.status, "FAILED", "")), nil
			}})
			_, err := env.confirmed(id, "sfree", args)
			requests, _ := env.fake.seen()
			wantRequests := 1
			if id == accessBindingsUpdateID {
				wantRequests = 2
			}
			if classOf(err) != c.class || leaks(err.Error()) != "" || len(requests) != wantRequests {
				t.Errorf("%s %s: %v, requests %d", c.name, id, err, len(requests))
			}
			if c.status == http.StatusTooManyRequests && strings.Contains(err.Error(), "no partial result") {
				t.Errorf("%s %s: a rate limit is not a partial result: %v", c.name, id, err)
			}
		}
	}
}

func TestAccessBinding429WithoutRetryAfterHoldsTheNextRequest(t *testing.T) {
	env := bindingWriteEnv(t, &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
		if n == 1 {
			return respond(r, http.StatusTooManyRequests, "application/json", errorObject(429, "RESOURCE_EXHAUSTED", "")), nil
		}
		return respond(r, http.StatusOK, "application/json", `{}`), nil
	}})
	const args = `{"name":"properties/200/accessBindings/5001"}`
	if _, err := env.confirmed(accessBindingsDeleteID, "sfree", args); classOf(err) != provider.ClassRateLimited {
		t.Fatalf("error = %v", err)
	}
	if _, err := env.confirmed(accessBindingsDeleteID, "sfree", args); err != nil {
		t.Fatal(err)
	}
	if requests, _ := env.fake.seen(); len(requests) != 2 {
		t.Errorf("requests = %d, want 2 without a retry", len(requests))
	}
	if waits := *env.waits; len(waits) == 0 || slices.Max(waits) < 900_000_000 {
		t.Errorf("waits = %v, want the next request held for about a second", waits)
	}
}

func TestAccessBindingSummariesNameTheUserTheRolesAndTheLockout(t *testing.T) {
	reg := registry(t)
	update := reg.ConfirmationSummary(accessBindingsUpdateID)(json.RawMessage(bindingUpdateArgs))
	for _, want := range []string{"First.User@example.com", "properties/200/accessBindings/5001",
		"from no-cost-data, viewer to analyst", "adds analyst", "removes no-cost-data, viewer", "own user"} {
		if !strings.Contains(update, want) {
			t.Errorf("update summary %q lacks %q", update, want)
		}
	}
	if strings.Contains(update, "admin lets") {
		t.Errorf("update summary %q warns of admin", update)
	}
	admin := reg.ConfirmationSummary(accessBindingsUpdateID)(json.RawMessage(strings.Replace(bindingUpdateArgs,
		`"roles":["predefinedRoles/analyst"]`, `"roles":["predefinedRoles/admin"]`, 1)))
	if !strings.Contains(admin, "adds admin") || !strings.Contains(admin, "admin lets the user manage users") {
		t.Errorf("admin summary %q", admin)
	}
	remove := reg.ConfirmationSummary(accessBindingsDeleteID)(json.RawMessage(`{"name":"accounts/100/accessBindings/6001"}`))
	if !strings.Contains(remove, "accounts/100/accessBindings/6001") || !strings.Contains(remove, "own user") ||
		!strings.Contains(remove, "lose its access") {
		t.Errorf("delete summary %q", remove)
	}
	batch := reg.ConfirmationSummary(accessBindingsBatchDeleteID)(json.RawMessage(
		`{"parent":"accounts/100","names":["accounts/100/accessBindings/6001","accounts/100/accessBindings/6002"]}`))
	if !strings.HasPrefix(batch, "Remove 2 access bindings of accounts/100") || !strings.Contains(batch, "own user") ||
		!strings.Contains(batch, "accounts/100/accessBindings/6002") {
		t.Errorf("batch summary %q", batch)
	}
}

func TestAccessBindingWriteToolsAreRegisteredWithScopesProfilesAndRisk(t *testing.T) {
	reg := registry(t)
	metadata, _ := reg.ProviderMetadata(Provider)
	profiles := map[string][]string{}
	for _, p := range metadata.Profiles {
		profiles[p.ID] = p.Tools
		if p.ID == accessBindingsProfile && (p.Recommended || !strings.Contains(p.Description, "analytics.manage.users") ||
			!strings.Contains(p.Description, "personal data")) {
			t.Errorf("profile %+v", p)
		}
	}
	for _, c := range []struct {
		id       string
		risk     capability.Risk
		scope    string
		listed   bool
		profiles []string
	}{
		{accessBindingsBatchGetID, bindingRisk, scopeUsersReadOnly, false,
			[]string{"read", "write", "read-change-history", accessBindingsProfile}},
		{accessBindingsUpdateID, bindingWriteRisk(capability.EffectUpdate), scopeUsers, false, []string{accessBindingsProfile}},
		{accessBindingsDeleteID, bindingWriteRisk(capability.EffectDelete), scopeUsers, false, []string{accessBindingsProfile}},
		{accessBindingsBatchDeleteID, bindingWriteRisk(capability.EffectDelete), scopeUsers, true, nil},
	} {
		d, _, ok := reg.Lookup(c.id)
		if !ok || d.Risk != c.risk || d.RequiresToolAllowList != c.listed || d.Risk.DataSensitivity != dataSensitivityBindings ||
			!slices.Contains(d.Tags, "personal-data") || !strings.Contains(d.Description, "ALPHA") ||
			!strings.Contains(d.Description, "untrusted") {
			t.Errorf("%s: %+v", c.id, d)
		}
		if !slices.Equal(metadata.Token.Scopes[c.id], []string{c.scope}) {
			t.Errorf("%s scopes %v", c.id, metadata.Token.Scopes[c.id])
		}
		var in []string
		for id, tools := range profiles {
			if slices.Contains(tools, c.id) {
				in = append(in, id)
			}
		}
		slices.Sort(in)
		want := slices.Clone(c.profiles)
		slices.Sort(want)
		if !slices.Equal(in, want) {
			t.Errorf("%s profiles %v, want %v", c.id, in, want)
		}
		if c.risk.Confirmation == capability.ConfirmationRequired && reg.ConfirmationSummary(c.id) == nil {
			t.Errorf("%s has no confirmation summary", c.id)
		}
	}
	if scopeUsers != "https://www.googleapis.com/auth/analytics.manage.users" {
		t.Errorf("scope = %s", scopeUsers)
	}
}

func TestNoToolCreatesOrBatchUpdatesAccessBindings(t *testing.T) {
	reg := registry(t)
	var bindings []string
	for _, d := range reg.Provider(Provider) {
		if strings.HasPrefix(d.ID, Provider+".access-bindings.") {
			bindings = append(bindings, d.ID)
		}
	}
	slices.Sort(bindings)
	if !slices.Equal(bindings, []string{accessBindingsBatchDeleteID, accessBindingsBatchGetID, accessBindingsDeleteID,
		accessBindingsGetID, accessBindingsListID, accessBindingsUpdateID}) {
		t.Errorf("binding tools = %v", bindings)
	}
	for _, id := range bindings {
		for _, excluded := range []string{".create", ".batch-create", ".batch-update"} {
			if strings.HasSuffix(id, excluded) {
				t.Errorf("%s exports a way to add a user", id)
			}
		}
	}
	for _, missing := range []string{".access-bindings.create", ".access-bindings.batch-create",
		".access-bindings.batch-update"} {
		if _, _, ok := reg.Lookup(Provider + missing); ok {
			t.Errorf("%s exists", missing)
		}
	}
}
