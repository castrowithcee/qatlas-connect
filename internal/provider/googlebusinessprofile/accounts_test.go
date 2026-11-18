package googlebusinessprofile

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
)

// accountsPage answers an account list page by its pageToken: two full pages of 20 and a last one of 5.
func accountsPage(r *http.Request, _ int) (*http.Response, error) {
	token := r.URL.Query().Get("pageToken")
	start, next := 0, "page-two"
	switch token {
	case "page-two":
		start, next = 20, "page-three"
	case "page-three":
		start, next = 40, ""
	}
	size := 20
	if next == "" {
		size = 5
	}
	entries := make([]string, 0, size)
	for i := range size {
		entries = append(entries, fmt.Sprintf(`{"name":"accounts/%d","accountName":"Sample group %d",`+
			`"type":"LOCATION_GROUP","role":"OWNER"}`, 1000100+start+i, start+i))
	}
	body := `{"accounts":[` + strings.Join(entries, ",") + `]`
	if next != "" {
		body += `,"nextPageToken":"` + next + `"`
	}
	return respond(r, http.StatusOK, "application/json", body+"}"), nil
}

func TestAccountsListReadsThreePagesWithTheCursor(t *testing.T) {
	env := newEnvironment(t, nil)
	env.fake.answer = accountsPage
	arguments := `{"type":"LOCATION_GROUP","page_size":20`
	var names []string
	cursor := ""
	for page := 1; page <= 3; page++ {
		args := arguments + `}`
		if cursor != "" {
			args = arguments + `,"cursor":"` + cursor + `"}`
		}
		out, err := env.invoke(t, accountsListID, "all", args)
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		for _, entry := range out["accounts"].([]any) {
			names = append(names, entry.(map[string]any)["name"].(string))
		}
		next, _ := out["next_cursor"].(string)
		if (page < 3) != (next != "") || strings.Contains(next, "page-") {
			t.Fatalf("page %d next_cursor = %q", page, next)
		}
		cursor = next
	}
	if len(names) != 45 || names[0] != "accounts/1000100" || names[44] != "accounts/1000144" {
		t.Errorf("accounts = %d, first %v", len(names), names[:1])
	}
	calls, _ := env.fake.calls()
	for i, want := range []string{"", "page-two", "page-three"} {
		values := calls[i].values()
		if calls[i].host != "mybusinessaccountmanagement.googleapis.com" || calls[i].path != "/v1/accounts" ||
			values.Get("filter") != "type=LOCATION_GROUP" || values.Get("pageSize") != "20" ||
			values.Get("pageToken") != want || values.Has("parentAccount") {
			t.Errorf("request %d = %+v", i+1, calls[i])
		}
	}
}

func TestAccountsListRefusesACursorOfOtherArguments(t *testing.T) {
	env := newEnvironment(t, nil)
	env.fake.answer = accountsPage
	out, err := env.invoke(t, accountsListID, "all", `{"type":"LOCATION_GROUP"}`)
	if err != nil {
		t.Fatal(err)
	}
	cursor := out["next_cursor"].(string)
	for _, args := range []string{
		`{"type":"USER_GROUP","cursor":"` + cursor + `"}`,
		`{"parent_account":"` + boundAccount + `","type":"LOCATION_GROUP","cursor":"` + cursor + `"}`,
		`{"type":"LOCATION_GROUP","cursor":"x` + cursor + `"}`,
	} {
		if _, err := env.invoke(t, accountsListID, "all", args); !isInvalidRequest(err) {
			t.Errorf("%s: error = %v, want invalid-request", args, err)
		}
	}
	// A page size is no part of the binding: the cursor continues with another one.
	if _, err := env.invoke(t, accountsListID, "all", `{"type":"LOCATION_GROUP","page_size":5,"cursor":"`+cursor+`"}`); err != nil {
		t.Errorf("another page size: %v", err)
	}
	if calls, _ := env.fake.calls(); len(calls) != 2 {
		t.Errorf("%d requests, want two", len(calls))
	}
}

func TestAccountsListPassesTheParentAccountAndAnEmptyList(t *testing.T) {
	env := newEnvironment(t, nil)
	out, err := env.invoke(t, accountsListID, "all", `{"parent_account":"`+boundAccount+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	// An empty list is left out of the result: missing means empty.
	if len(out) != 0 {
		t.Errorf("result = %v, want an empty list", out)
	}
	calls, _ := env.fake.calls()
	if values := calls[0].values(); values.Get("parentAccount") != boundAccount || values.Has("filter") ||
		values.Has("pageSize") {
		t.Errorf("request = %+v", calls[0])
	}
}

func TestAccountsReturnEveryDocumentedField(t *testing.T) {
	env := newEnvironment(t, nil)
	long := strings.Repeat("n", maxLineRunes+10)
	env.fake.answer = reply(http.StatusOK, `{"name":"`+boundAccount+`","accountName":"`+long+`","type":"ORGANIZATION",`+
		`"role":"PRIMARY_OWNER","permissionLevel":"OWNER_LEVEL","verificationState":"VERIFIED","vettedState":"VETTED",`+
		`"accountNumber":"sample-number","primaryOwner":"accounts/1","undocumented":"`+detailCanary+`",`+
		`"organizationInfo":{"registeredDomain":"example.com","phoneNumber":"phone-sample-1","address":{"regionCode":"CH",`+
		`"revision":0,"addressLines":["Sample street 1"],"locality":"Sample town","postalCode":"0000"}}}`)
	out, err := env.invoke(t, accountsGetID, "all", `{"account":"`+boundAccount+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"name", "type", "role", "permissionLevel", "verificationState", "vettedState",
		"accountNumber"} {
		if out[field] == nil {
			t.Errorf("field %s is missing: %v", field, out)
		}
	}
	if name := out["accountName"].(string); !strings.HasSuffix(name, "…") || len([]rune(name)) != maxLineRunes+1 {
		t.Errorf("accountName was not cut: %d characters", len([]rune(name)))
	}
	if out["primaryOwner"] != nil || out["undocumented"] != nil {
		t.Errorf("an input-only or undocumented member was kept: %v", out)
	}
	organization := out["organizationInfo"].(map[string]any)
	address := organization["address"].(map[string]any)
	if organization["phoneNumber"] != "phone-sample-1" || address["regionCode"] != "CH" || address["revision"] != 0.0 {
		t.Errorf("organizationInfo = %v", organization)
	}
}

func TestAccountTargetAnswersWithItsAccount(t *testing.T) {
	env := newEnvironment(t, nil)
	env.fake.answer = reply(http.StatusOK, `{"name":"`+boundAccount+`","type":"LOCATION_GROUP"}`)
	out, err := env.invoke(t, accountsListID, "account", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	if accounts := out["accounts"].([]any); len(accounts) != 1 {
		t.Errorf("accounts = %v", accounts)
	}
	out, err = env.invoke(t, accountsListID, "account", `{"type":"PERSONAL"}`)
	if err != nil {
		t.Fatal(err)
	}
	if out["accounts"] != nil {
		t.Errorf("accounts of another type = %v", out["accounts"])
	}
	if _, err := env.invoke(t, accountsGetID, "account", `{}`); err != nil {
		t.Errorf("get without account: %v", err)
	}
	calls, _ := env.fake.calls()
	for _, call := range calls {
		if call.path != "/v1/"+boundAccount || call.query != "" {
			t.Errorf("request = %+v, want the bound account only", call)
		}
	}
	for tool, args := range map[string]string{
		accountsListID: `{"parent_account":"` + boundAccount + `"}`,
		accountsGetID:  `{"account":"` + otherAccount + `"}`,
	} {
		if _, err := env.invoke(t, tool, "account", args); !isInvalidRequest(err) {
			t.Errorf("%s %s: error = %v, want invalid-request", tool, args, err)
		}
	}
	if after, _ := env.fake.calls(); len(after) != len(calls) {
		t.Errorf("a refused call reached Google")
	}
}

func TestAccountArgumentsAreCheckedBeforeAnyRequest(t *testing.T) {
	env := newEnvironment(t, nil)
	for _, args := range []string{`{}`, `{"account":"accounts/-"}`, `{"account":"accounts/0100"}`,
		`{"account":"accounts/1/x"}`, `{"account":"` + boundLocation + `"}`} {
		if _, err := env.invoke(t, accountsGetID, "all", args); !isInvalidRequest(err) {
			t.Errorf("%s: error = %v, want invalid-request", args, err)
		}
	}
	if calls, grants := env.fake.calls(); len(calls) != 0 || grants != 0 {
		t.Errorf("%d requests and %d grants, want none", len(calls), grants)
	}
}

func TestLocationTargetOffersNoAccountTool(t *testing.T) {
	env := newEnvironment(t, nil)
	for _, tool := range []string{accountsListID, accountsGetID} {
		_, err := env.invoke(t, tool, "location", `{}`)
		var unsupported *capability.UnsupportedError
		if !errors.As(err, &unsupported) {
			t.Errorf("%s: error = %v, want unsupported-capability", tool, err)
		}
	}
	if calls, grants := env.fake.calls(); len(calls) != 0 || grants != 0 {
		t.Errorf("%d requests and %d grants, want none", len(calls), grants)
	}
}
