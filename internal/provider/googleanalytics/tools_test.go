package googleanalytics

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// call invokes any tool of the provider.
func (e *testEnv) call(operation, connection, arguments string) (string, error) {
	response, err := e.core.Invoke(context.Background(), application.InvokeRequest{
		Operation: operation, Connection: connection, Arguments: json.RawMessage(arguments)})
	return string(response.Result), err
}

// callOwn invokes through a core of its own, for calls from parallel goroutines: a core serves one call.
func (e *testEnv) callOwn(operation, connection, arguments string) (string, error) {
	core := application.New(e.reg, e.cfg, e.secrets(), e.red)
	response, err := core.Invoke(context.Background(), application.InvokeRequest{
		Operation: operation, Connection: connection, Arguments: json.RawMessage(arguments)})
	return string(response.Result), err
}

func refusedInvalid(err error) bool {
	var refused *provider.InvalidRequestError
	return errors.As(err, &refused)
}

const accountsAnswer = `{"accounts":[{"name":"accounts/100","displayName":"Shop","regionCode":"CH",` +
	`"createTime":"2024-01-02T03:04:05.123456Z","updateTime":"2024-02-03T04:05:06Z","deleted":true,` +
	`"gmpOrganization":"marketingplatformadmin.googleapis.com/organizations/abc","unknown":"x"},` +
	`{"name":"accounts/900","displayName":"Other"}]}`

func TestAccountsListReturnsEveryMemberAndPassesTheParameters(t *testing.T) {
	env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, accountsAnswer)})
	result, err := env.call(accountsID, "read", `{"page_size":7,"show_deleted":true}`)
	if err != nil {
		t.Fatal(err)
	}
	requests, _ := env.fake.seen()
	if len(requests) != 1 || requests[0].path != "/v1beta/accounts" ||
		requests[0].rawQuery != "pageSize=7&showDeleted=true" || requests[0].auth != "Bearer "+accessPrefix+"1" {
		t.Fatalf("requests = %+v", requests)
	}
	var out struct {
		Accounts []account `json:"accounts"`
		HasMore  bool      `json:"has_more"`
	}
	if err := json.Unmarshal([]byte(result), &out); err != nil || len(out.Accounts) != 2 || out.HasMore {
		t.Fatalf("result = %s, %v", result, err)
	}
	want := account{Name: "accounts/100", DisplayName: "Shop", RegionCode: "CH", CreateTime: "2024-01-02T03:04:05.123456Z",
		UpdateTime: "2024-02-03T04:05:06Z", Deleted: true,
		GmpOrganization: "marketingplatformadmin.googleapis.com/organizations/abc"}
	if out.Accounts[0] != want {
		t.Errorf("account = %+v", out.Accounts[0])
	}
}

func TestAccountsListEmptyAnswerIsAnEmptyList(t *testing.T) {
	env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, `{}`)})
	out, err := handleAccountsList(context.Background(), env.resolved(t, "read"), env.secrets(), env.red, json.RawMessage(`{}`))
	if result, ok := out.(accountsResult); err != nil || !ok || result.Accounts == nil || len(result.Accounts) != 0 || result.HasMore {
		t.Fatalf("result = %+v, %v", out, err)
	}
	requests, _ := env.fake.seen()
	if requests[0].rawQuery != "" {
		t.Errorf("query = %q", requests[0].rawQuery)
	}
}

func TestAccountsListBindingKeepsBoundAccountsAndRefusesPropertyOnly(t *testing.T) {
	env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, accountsAnswer)})
	result, err := env.call(accountsID, "account", `{}`)
	if err != nil || !strings.Contains(result, "accounts/100") || strings.Contains(result, "accounts/900") {
		t.Fatalf("result = %s, %v", result, err)
	}
	before, _ := env.fake.seen()
	_, err = env.call(accountsID, "property", `{}`)
	after, _ := env.fake.seen()
	if !refusedInvalid(err) || len(after) != len(before) {
		t.Errorf("property-only binding: %v, requests %d -> %d", err, len(before), len(after))
	}
}

func TestAccountsListPagesAndShowDeletedBindsTheCursor(t *testing.T) {
	fake := &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
		if n == 1 {
			return respond(r, http.StatusOK, "application/json", `{"accounts":[{"name":"accounts/1"}],"nextPageToken":"t2"}`), nil
		}
		return respond(r, http.StatusOK, "application/json", `{"accounts":[{"name":"accounts/2"}]}`), nil
	}}
	env := newEnv(t, fake)
	first, err := env.call(accountsID, "read", `{"page_size":1}`)
	if err != nil {
		t.Fatal(err)
	}
	var page struct {
		NextCursor string `json:"next_cursor"`
		HasMore    bool   `json:"has_more"`
	}
	if json.Unmarshal([]byte(first), &page) != nil || !page.HasMore || page.NextCursor == "" || strings.Contains(first, "t2") {
		t.Fatalf("first = %s", first)
	}
	if _, err := env.call(accountsID, "read", `{"page_size":1,"show_deleted":true,"cursor":"`+page.NextCursor+`"}`); !refusedInvalid(err) {
		t.Errorf("cursor with another show_deleted: %v", err)
	}
	if _, err := env.call(accountsID, "account", `{"cursor":"`+page.NextCursor+`"}`); !refusedInvalid(err) {
		t.Errorf("cursor of another connection: %v", err)
	}
	second, err := env.call(accountsID, "read", `{"page_size":1,"cursor":"`+page.NextCursor+`"}`)
	requests, _ := env.fake.seen()
	if err != nil || !strings.Contains(second, "accounts/2") || len(requests) != 2 ||
		requests[1].rawQuery != "pageSize=1&pageToken=t2" {
		t.Fatalf("second = %s, %v, requests %+v", second, err, requests)
	}
}

const propertyJSON = `{"name":"properties/200","propertyType":"PROPERTY_TYPE_ORDINARY",` +
	`"createTime":"2024-01-02T03:04:05.123456Z","updateTime":"2024-02-03T04:05:06Z","parent":"accounts/100",` +
	`"displayName":"Shop web","industryCategory":"SHOPPING","timeZone":"Europe/Zurich","currencyCode":"CHF",` +
	`"serviceLevel":"GOOGLE_ANALYTICS_STANDARD","deleteTime":"2025-01-01T00:00:00Z",` +
	`"expireTime":"2025-02-01T00:00:00Z","account":"accounts/100"}`

func TestPropertiesListRefusesWithoutFilterBeforeAnyIO(t *testing.T) {
	env := newEnv(t, &fakeGoogle{})
	for connection, arguments := range map[string]string{
		"read":     `{}`,
		"property": `{}`,
	} {
		_, err := env.call(propertiesListID, connection, arguments)
		if !refusedInvalid(err) {
			t.Errorf("%s: %v", connection, err)
		}
	}
	if requests, grants := env.fake.seen(); len(requests) != 0 || grants != 0 || env.reads.Load() != 0 {
		t.Errorf("requests %d, grants %d, secret reads %d", len(requests), grants, env.reads.Load())
	}
}

func TestPropertiesListChecksTheFilterAndTheBindingBeforeAnyIO(t *testing.T) {
	env := newEnv(t, &fakeGoogle{})
	for name, c := range map[string]struct{ connection, arguments string }{
		"free text":           {"read", `{"filter":"displayName:Shop"}`},
		"blank":               {"read", `{"filter":"parent:accounts/1 OR parent:accounts/2"}`},
		"ancestor property":   {"read", `{"filter":"ancestor:properties/1"}`},
		"bad id":              {"read", `{"filter":"parent:accounts/01"}`},
		"firebase upper case": {"read", `{"filter":"firebase_project:Bad"}`},
		"foreign account":     {"account", `{"filter":"parent:accounts/900"}`},
		"firebase on account": {"account", `{"filter":"firebase_project:demo-1"}`},
		"property of account": {"account", `{"filter":"parent:properties/200"}`},
		"property only":       {"property", `{"filter":"parent:properties/200"}`},
		"property only acct":  {"property", `{"filter":"parent:accounts/100"}`},
		"two accounts":        {"read", `{"filter":"parent:accounts/1","cursor":"x"}`},
	} {
		_, err := env.call(propertiesListID, c.connection, c.arguments)
		if !refusedInvalid(err) {
			t.Errorf("%s: %v", name, err)
		} else if strings.Contains(err.Error(), "900") || strings.Contains(err.Error(), "Shop") {
			t.Errorf("%s: the refusal quotes the filter: %v", name, err)
		}
	}
	if requests, _ := env.fake.seen(); len(requests) != 0 || env.reads.Load() != 0 {
		t.Errorf("requests %d, secret reads %d", len(requests), env.reads.Load())
	}
}

func TestPropertiesListSendsTheFilterAndSetsItForABoundAccount(t *testing.T) {
	for _, c := range []struct {
		connection, arguments, query string
	}{
		{"read", `{"filter":"ancestor:accounts/9","page_size":3,"show_deleted":true}`,
			"filter=ancestor%3Aaccounts%2F9&pageSize=3&showDeleted=true"},
		{"read", `{"filter":"firebase_project:demo-1"}`, "filter=firebase_project%3Ademo-1"},
		{"account", `{}`, "filter=parent%3Aaccounts%2F100"},
		{"account", `{"filter":"ancestor:accounts/100"}`, "filter=ancestor%3Aaccounts%2F100"},
		{"mixed", `{"filter":"parent:accounts/100"}`, "filter=parent%3Aaccounts%2F100"},
	} {
		env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"properties":[`+propertyJSON+`]}`)})
		result, err := env.call(propertiesListID, c.connection, c.arguments)
		requests, _ := env.fake.seen()
		if err != nil || len(requests) != 1 || requests[0].path != "/v1beta/properties" || requests[0].rawQuery != c.query {
			t.Errorf("%s %s: %v, %+v", c.connection, c.arguments, err, requests)
			continue
		}
		var out struct {
			Properties []property `json:"properties"`
		}
		var want property
		_ = json.Unmarshal([]byte(propertyJSON), &want)
		if json.Unmarshal([]byte(result), &out) != nil || len(out.Properties) != 1 || out.Properties[0] != want {
			t.Errorf("result = %s", result)
		}
	}
}

func TestPropertiesListEmptyAnswerPagesAndCursorBinding(t *testing.T) {
	env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, `{}`)})
	out, err := handlePropertiesList(context.Background(), env.resolved(t, "read"), env.secrets(), env.red,
		json.RawMessage(`{"filter":"parent:accounts/1"}`))
	if result, ok := out.(propertiesResult); err != nil || !ok || result.Properties == nil || len(result.Properties) != 0 {
		t.Fatalf("result = %+v, %v", out, err)
	}
	fake := &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
		switch n {
		case 1:
			return respond(r, http.StatusOK, "application/json", `{"properties":[],"nextPageToken":"p2"}`), nil
		case 2:
			return respond(r, http.StatusOK, "application/json", `{"properties":[{"name":"properties/5"}],"nextPageToken":"p3"}`), nil
		}
		return respond(r, http.StatusOK, "application/json", `{"properties":[{"name":"properties/6"}]}`), nil
	}}
	env = newEnv(t, fake)
	args := func(extra string) string { return `{"filter":"parent:accounts/1"` + extra + `}` }
	var cursor string
	for i, want := range []string{"", "properties/5", "properties/6"} {
		extra := ""
		if cursor != "" {
			extra = `,"cursor":"` + cursor + `"`
		}
		out, err := env.call(propertiesListID, "read", args(extra))
		if err != nil || (want != "" && !strings.Contains(out, want)) {
			t.Fatalf("page %d: %s, %v", i, out, err)
		}
		var page struct {
			NextCursor string `json:"next_cursor"`
			HasMore    bool   `json:"has_more"`
		}
		_ = json.Unmarshal([]byte(out), &page)
		if page.HasMore != (i < 2) {
			t.Errorf("page %d has_more = %t", i, page.HasMore)
		}
		if i == 0 {
			for _, other := range []string{`{"filter":"parent:accounts/2"`, `{"filter":"parent:accounts/1","show_deleted":true`} {
				if _, err := env.call(propertiesListID, "read", other+`,"cursor":"`+page.NextCursor+`"}`); !refusedInvalid(err) {
					t.Errorf("cursor accepted for %s: %v", other, err)
				}
			}
		}
		cursor = page.NextCursor
	}
	requests, _ := env.fake.seen()
	if len(requests) != 3 || !strings.Contains(requests[2].rawQuery, "pageToken=p3") {
		t.Errorf("requests = %+v", requests)
	}
}

func TestPropertiesGetRefusesBeforeIOAndReturnsEveryMember(t *testing.T) {
	env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, propertyJSON)})
	for connection, arguments := range map[string]string{
		"read":     `{"property":"accounts/100"}`,
		"read2":    `{"property":"properties/0"}`,
		"read3":    `{"property":"properties/1/x"}`,
		"read4":    `{}`,
		"property": `{"property":"properties/901"}`,
		"mixed":    `{"property":"property/200"}`,
	} {
		name := strings.TrimRight(connection, "0123456789")
		if _, err := env.call(propertiesGetID, name, arguments); !refusedInvalid(err) {
			t.Errorf("%s %s: %v", name, arguments, err)
		}
	}
	if requests, _ := env.fake.seen(); len(requests) != 0 || env.reads.Load() != 0 {
		t.Fatalf("requests %d, secret reads %d", len(requests), env.reads.Load())
	}
	result, err := env.call(propertiesGetID, "read", `{"property":"properties/200"}`)
	var got, want property
	_ = json.Unmarshal([]byte(propertyJSON), &want)
	if err != nil || json.Unmarshal([]byte(result), &got) != nil || got != want {
		t.Fatalf("result = %s, %v", result, err)
	}
	if requests, _ := env.fake.seen(); len(requests) != 1 || requests[0].path != "/v1beta/properties/200" {
		t.Errorf("requests = %+v", requests)
	}
}

func TestPropertiesGetFollowsTheBinding(t *testing.T) {
	answers := map[string]string{
		"/v1beta/properties/200": `{"name":"properties/200","parent":"accounts/100"}`,
		"/v1beta/properties/201": `{"name":"properties/201","parent":"accounts/100"}`,
		"/v1beta/properties/901": `{"name":"properties/901","parent":"accounts/900"}`,
	}
	fake := &fakeGoogle{api: func(_ int, r *http.Request) (*http.Response, error) {
		return respond(r, http.StatusOK, "application/json", answers[r.URL.Path]), nil
	}}
	for _, c := range []struct {
		connection, arguments string
		requests              int
		refused               bool
	}{
		{"property", `{"property":"properties/200"}`, 1, false},
		{"property", `{}`, 1, false}, // the one bound property is the default
		{"property", `{"property":"properties/201"}`, 0, true},
		{"property", `{"property":"properties/901"}`, 0, true},
		{"account", `{"property":"properties/201"}`, 1, false},
		{"account", `{"property":"properties/901"}`, 1, true}, // proof shows another account
		{"account", `{}`, 0, true},
		{"mixed", `{"property":"properties/901"}`, 1, false}, // bound itself
		{"mixed", `{"property":"properties/200"}`, 1, false},
	} {
		env := newEnv(t, fake)
		before, _ := fake.seen()
		result, err := env.call(propertiesGetID, c.connection, c.arguments)
		after, _ := fake.seen()
		if len(after)-len(before) != c.requests || (err != nil) != c.refused ||
			(c.refused && (!refusedInvalid(err) || strings.Contains(err.Error(), "901") || strings.Contains(result, "901"))) {
			t.Errorf("%s %s: requests %d, error %v", c.connection, c.arguments, len(after)-len(before), err)
		}
	}
}

func TestPropertyToolsClassifyFailuresWithoutProviderText(t *testing.T) {
	for _, tool := range []struct{ id, arguments string }{
		{accountsID, `{}`}, {propertiesListID, `{"filter":"parent:accounts/1"}`},
		{propertiesGetID, `{"property":"properties/1"}`},
	} {
		env := newEnv(t, &fakeGoogle{api: always(http.StatusForbidden, errorObject(403, "PERMISSION_DENIED", "X"))})
		_, err := env.call(tool.id, "read", tool.arguments)
		if classOf(err) != provider.ClassPermission || leaks(err.Error()) != "" {
			t.Errorf("%s: %v", tool.id, err)
		}
	}
}
