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

type userRequest struct{ method, path, escaped, query, contentType, body string }

func (h *harness) userRequests(t *testing.T) []userRequest {
	t.Helper()
	var out []userRequest
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
		out = append(out, userRequest{req.Method, req.URL.Path, req.URL.EscapedPath(), req.URL.RawQuery,
			req.Header.Get("Content-Type"), body})
	}
	return out
}

func userDescriptor(id string) capability.Descriptor {
	for _, d := range descriptors() {
		if d.ID == id {
			return d
		}
	}
	return capability.Descriptor{}
}

const userBody = `{"name":"accounts/7/users/person@example.com","state":"VERIFIED","accessRights":["ADMIN","STANDARD"],` +
	`"email":"never held"}`

func TestUserToolsAreListedConfirmedAndOutsideTheReadProfile(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	for id, effect := range map[string]capability.Effect{usersUpdateID: capability.EffectUpdate,
		usersDeleteID: capability.EffectDelete} {
		d, _, ok := reg.Lookup(id)
		if !ok || !d.RequiresToolAllowList || d.Version != 1 || d.Risk.Effect != effect ||
			d.Risk.Confirmation != capability.ConfirmationRequired || !d.Risk.OpenWorld ||
			d.Risk.DataSensitivity != userSensitivity {
			t.Errorf("%s = %+v", id, d)
		}
		if slices.Contains(metadata.Profiles[0].Tools, id) {
			t.Errorf("the read profile holds %s", id)
		}
		var listed bool
		for _, tool := range metadata.Tools {
			if tool.ID == id {
				listed = tool.RequiresToolAllowList && tool.Effect == config.Effect(effect)
			}
		}
		if !listed {
			t.Errorf("%s is not a listed-only tool", id)
		}
	}
	for _, id := range []string{usersListID, usersGetID} {
		d, _, ok := reg.Lookup(id)
		if !ok || d.RequiresToolAllowList || d.Risk.Effect != capability.EffectRead ||
			d.Risk.DataSensitivity != userSensitivity || !slices.Contains(metadata.Profiles[0].Tools, id) {
			t.Errorf("%s = %+v", id, d)
		}
	}
}

func TestUserToolDescriptionsNameReplacementRevocationAndExclusions(t *testing.T) {
	for id, words := range map[string][]string{
		usersUpdateID: {"replaces the whole list", "role that is left out is removed", "never merged", "Only the acceptance",
			"delay", "me is refused", "users.create", "users.me.verifySelf", "advanced-accounts/ACCOUNT"},
		usersDeleteID: {"permanently withdraws its access", "last admin", "me is refused", "Only the acceptance",
			"delay", "users.create", "users.me.verifySelf", "advanced-accounts/ACCOUNT"},
	} {
		for _, word := range words {
			if !strings.Contains(strings.ToLower(userDescriptor(id).Description), strings.ToLower(word)) {
				t.Errorf("%s: description lacks %q", id, word)
			}
		}
	}
	for id, summary := range map[string]string{
		usersUpdateID: usersUpdateSummary([]byte(`{"account":"7","user":"a@example.com","access_rights":["READ_ONLY"]}`)),
		usersDeleteID: usersDeleteSummary([]byte(`{"account":"7","user":"a@example.com"}`)),
	} {
		if !strings.Contains(summary, "a@example.com") || !strings.Contains(summary, "7") {
			t.Errorf("%s summary = %s", id, summary)
		}
	}
	if s := usersUpdateSummary([]byte(`{"user":"a@example.com","access_rights":["READ_ONLY"]}`)); !strings.Contains(s, "whole list") ||
		!strings.Contains(s, "READ_ONLY") || !strings.Contains(s, "lose rights") {
		t.Errorf("update summary = %s", s)
	}
	if s := usersDeleteSummary([]byte(`{"user":"a@example.com"}`)); !strings.Contains(s, "permanently withdraws") {
		t.Errorf("delete summary = %s", s)
	}
}

func TestUserToolsNeedTheAdvancedAccountTargetOfABoundConnection(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	for _, id := range []string{usersListID, usersGetID, usersUpdateID, usersDeleteID} {
		for _, c := range []struct {
			targets []string
			want    bool
		}{
			{nil, true}, {[]string{"advanced-accounts/1"}, true}, {[]string{"accounts/2", "advanced-accounts/1"}, true},
			{[]string{"accounts/2"}, false}, {[]string{"accounts/2", "accounts/3"}, false},
		} {
			got, hint := metadata.Target.Reach(id, c.targets)
			if got != c.want || (!got && hint != "needs "+formAdvanced) {
				t.Errorf("Reach(%s, %v) = %t, %q", id, c.targets, got, hint)
			}
		}
	}
}

func TestUsersListSendsDocumentedParametersAndShowsOnlyNameStateAndRights(t *testing.T) {
	h := newHarness(t)
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		if req.URL.Query().Get("pageToken") == "two" {
			return 200, `{"users":[{"name":"accounts/7/users/b@example.com","state":"PENDING","accessRights":["READ_ONLY"]}]}`, nil
		}
		return 200, `{"users":[` + userBody + `],"nextPageToken":"two"}`, nil
	}
	out, err := h.call(t, handleUsersList, `{"account":"7","page_size":100}`)
	if err != nil {
		t.Fatal(err)
	}
	users := out["users"].([]any)
	first := users[0].(map[string]any)
	if len(users) != 1 || first["name"] != "accounts/7/users/person@example.com" || first["state"] != "VERIFIED" ||
		len(first["accessRights"].([]any)) != 2 || len(first) != 3 || out["next_cursor"] == nil {
		t.Fatalf("out = %v", out)
	}
	out, err = h.call(t, handleUsersList, `{"account":"7","page_size":100,"cursor":"`+out["next_cursor"].(string)+`"}`)
	if err != nil || len(out["users"].([]any)) != 1 || out["next_cursor"] != nil {
		t.Fatalf("second = %v, %v", out, err)
	}
	got := h.userRequests(t)
	if len(got) != 2 || got[0].method != "GET" || got[0].path != "/accounts/v1/accounts/7/users" ||
		got[0].query != "pageSize=100" || got[1].query != "pageSize=100&pageToken=two" {
		t.Errorf("requests = %+v", got)
	}
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, `{}`, nil }
	if out, err := h.call(t, handleUsersList, `{"account":"7"}`); err != nil || len(out["users"].([]any)) != 0 {
		t.Errorf("empty = %v, %v", out, err)
	}
}

func TestUsersListRefusesPageSizeAndForeignCursor(t *testing.T) {
	h := newHarness(t)
	if _, err := h.call(t, handleUsersList, `{"account":"7","page_size":101}`); !isInvalidRequest(err) {
		t.Errorf("page size err = %v", err)
	}
	if _, err := h.call(t, handleUsersList, `{"account":"7","cursor":"forged"}`); !isInvalidRequest(err) {
		t.Errorf("cursor err = %v", err)
	}
	h.untouched(t)
}

func TestUsersGetReadsOneUserAndTakesTheMeAlias(t *testing.T) {
	h := newHarness(t)
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, userBody, nil }
	for _, user := range []string{"person@example.com", "me", "first.last+tag@example.co.uk"} {
		out, err := h.call(t, handleUsersGet, `{"account":"7","user":"`+user+`"}`)
		if err != nil || out["state"] != "VERIFIED" || out["email"] != nil || len(out) != 3 {
			t.Fatalf("%s: out = %v, %v", user, out, err)
		}
	}
	got := h.userRequests(t)
	if len(got) != 3 || got[0].method != "GET" || got[0].path != "/accounts/v1/accounts/7/users/person@example.com" ||
		got[1].path != "/accounts/v1/accounts/7/users/me" ||
		got[2].path != "/accounts/v1/accounts/7/users/first.last+tag@example.co.uk" || got[0].body != "" {
		t.Errorf("requests = %+v", got)
	}
}

func TestUsersUpdateReplacesTheRoleListWithOnePatchAndReportsAcceptance(t *testing.T) {
	h := newHarness(t)
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, userBody, nil }
	out, err := h.call(t, handleUsersUpdate,
		`{"account":"7","user":"person@example.com","access_rights":["READ_ONLY","PERFORMANCE_REPORTING"]}`)
	if err != nil || out["accepted"] != true || out["name"] != "accounts/7/users/person@example.com" ||
		len(out["updated"].([]any)) != 1 || out["updated"].([]any)[0] != "accessRights" || out["state"] != nil ||
		out["accessRights"] != nil {
		t.Fatalf("out = %v, %v", out, err)
	}
	got := h.userRequests(t)
	if len(got) != 1 || got[0].method != http.MethodPatch || got[0].path != "/accounts/v1/accounts/7/users/person@example.com" ||
		got[0].query != "updateMask=accessRights" || got[0].contentType != "application/json" ||
		got[0].body != `{"accessRights":["READ_ONLY","PERFORMANCE_REPORTING"]}` {
		t.Errorf("requests = %+v", got)
	}
}

func TestUsersDeleteSendsOneDeleteWithoutBodyAndReportsAcceptance(t *testing.T) {
	h := newHarness(t)
	out, err := h.call(t, handleUsersDelete, `{"account":"7","user":"person@example.com"}`)
	if err != nil || out["accepted"] != true || out["name"] != "accounts/7/users/person@example.com" || out["updated"] != nil {
		t.Fatalf("out = %v, %v", out, err)
	}
	got := h.userRequests(t)
	if len(got) != 1 || got[0].method != http.MethodDelete || got[0].path != "/accounts/v1/accounts/7/users/person@example.com" ||
		got[0].query != "" || got[0].body != "" || got[0].contentType != "" {
		t.Errorf("requests = %+v", got)
	}
}

func TestUserToolsRefuseBeforeSecretAndRequest(t *testing.T) {
	long := strings.Repeat("a", 60) + "@" + strings.Repeat("b", 200) + ".com"
	for name, c := range map[string]struct {
		handler capability.Handler
		targets []string
		args    string
	}{
		"get outside targets":      {handleUsersGet, []string{"advanced-accounts/1"}, `{"account":"7","user":"a@example.com"}`},
		"get account target only":  {handleUsersGet, []string{"accounts/7"}, `{"account":"7","user":"a@example.com"}`},
		"list account target only": {handleUsersList, []string{"accounts/7"}, `{"account":"7"}`},
		"update account target":    {handleUsersUpdate, []string{"accounts/7"}, `{"account":"7","user":"a@example.com","access_rights":["ADMIN"]}`},
		"delete account target":    {handleUsersDelete, []string{"accounts/7"}, `{"account":"7","user":"a@example.com"}`},
		"delete sub-account":       {handleUsersDelete, []string{"advanced-accounts/1"}, `{"account":"7","user":"a@example.com"}`},
		"no account":               {handleUsersDelete, nil, `{"user":"a@example.com"}`},
		"bad account":              {handleUsersList, nil, `{"account":"7/x"}`},
		"update me":                {handleUsersUpdate, nil, `{"account":"7","user":"me","access_rights":["ADMIN"]}`},
		"delete me":                {handleUsersDelete, nil, `{"account":"7","user":"me"}`},
		"get missing user":         {handleUsersGet, nil, `{"account":"7"}`},
		"slash":                    {handleUsersGet, nil, `{"account":"7","user":"a/b@example.com"}`},
		"traversal":                {handleUsersDelete, nil, `{"account":"7","user":"../x@example.com"}`},
		"question":                 {handleUsersGet, nil, `{"account":"7","user":"a@example.com?x=1"}`},
		"hash":                     {handleUsersGet, nil, `{"account":"7","user":"a@example.com#x"}`},
		"space":                    {handleUsersGet, nil, `{"account":"7","user":"a b@example.com"}`},
		"control":                  {handleUsersDelete, nil, `{"account":"7","user":"a@example.com\n"}`},
		"percent":                  {handleUsersDelete, nil, `{"account":"7","user":"a%2f@example.com"}`},
		"no at":                    {handleUsersDelete, nil, `{"account":"7","user":"example.com"}`},
		"two at":                   {handleUsersDelete, nil, `{"account":"7","user":"a@b@example.com"}`},
		"unicode":                  {handleUsersDelete, nil, `{"account":"7","user":"ä@example.com"}`},
		"too long":                 {handleUsersDelete, nil, `{"account":"7","user":"` + long + `"}`},
		"uppercase me":             {handleUsersGet, nil, `{"account":"7","user":"ME"}`},
		"no rights":                {handleUsersUpdate, nil, `{"account":"7","user":"a@example.com"}`},
		"empty rights":             {handleUsersUpdate, nil, `{"account":"7","user":"a@example.com","access_rights":[]}`},
		"unspecified right":        {handleUsersUpdate, nil, `{"account":"7","user":"a@example.com","access_rights":["ACCESS_RIGHT_UNSPECIFIED"]}`},
		"unknown right":            {handleUsersUpdate, nil, `{"account":"7","user":"a@example.com","access_rights":["OWNER"]}`},
		"lowercase right":          {handleUsersUpdate, nil, `{"account":"7","user":"a@example.com","access_rights":["admin"]}`},
		"duplicate right":          {handleUsersUpdate, nil, `{"account":"7","user":"a@example.com","access_rights":["ADMIN","ADMIN"]}`},
		"too many rights":          {handleUsersUpdate, nil, `{"account":"7","user":"a@example.com","access_rights":["ADMIN","STANDARD","READ_ONLY","PERFORMANCE_REPORTING","API_DEVELOPER","ADMIN"]}`},
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

func TestUserToolsAcceptAnAdvancedAccountTargetAndTheDefaultAccount(t *testing.T) {
	h := newHarness(t, "advanced-accounts/7", "accounts/9")
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, userBody, nil }
	if _, err := h.call(t, handleUsersGet, `{"user":"a@example.com"}`); err != nil {
		t.Fatal(err)
	}
	if got := h.userRequests(t); len(got) != 1 || got[0].path != "/accounts/v1/accounts/7/users/a@example.com" {
		t.Errorf("requests = %+v", got)
	}
}

func TestUserMutationsAreNeverRepeatedAndFailuresAreMapped(t *testing.T) {
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
		for tool, call := range map[string]struct {
			handler capability.Handler
			args    string
		}{
			"update": {handleUsersUpdate, `{"account":"7","user":"a@example.com","access_rights":["STANDARD"]}`},
			"delete": {handleUsersDelete, `{"account":"7","user":"a@example.com"}`},
		} {
			t.Run(name+" "+tool, func(t *testing.T) {
				h := newHarness(t)
				h.google.api = func(*http.Request) (int, string, http.Header) { return c.status, c.body, nil }
				_, err := h.call(t, call.handler, call.args)
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
}

func TestUsersGetMapsAdminAndMissingUserErrors(t *testing.T) {
	h := newHarness(t)
	h.google.api = func(*http.Request) (int, string, http.Header) { return 403, googleErrorBody, nil }
	if _, err := h.call(t, handleUsersList, `{"account":"7"}`); classOf(err) != provider.ClassPermission {
		t.Errorf("err = %v", err)
	}
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 404, `{"error":{"code":404,"status":"NOT_FOUND"}}`, nil
	}
	if _, err := h.call(t, handleUsersGet, `{"account":"7","user":"a@example.com"}`); classOf(err) != provider.ClassNotFound {
		t.Errorf("err = %v", err)
	}
}

func TestUserTextsAreBounded(t *testing.T) {
	h := newHarness(t)
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"name":"accounts/7/users/` + strings.Repeat("x", 400) + `@example.com","state":"` +
			strings.Repeat("S", 500) + `","accessRights":["` + strings.Repeat("R", 500) + `",1]}`, nil
	}
	out, err := h.call(t, handleUsersGet, `{"account":"7","user":"a@example.com"}`)
	if err != nil {
		t.Fatal(err)
	}
	if n := len([]rune(out["name"].(string))); n != maxNameRunes {
		t.Errorf("name has %d runes", n)
	}
	if n := len([]rune(out["state"].(string))); n != maxIDRunes {
		t.Errorf("state has %d runes", n)
	}
	if rights := out["accessRights"].([]any); len(rights) != 1 || len([]rune(rights[0].(string))) != maxIDRunes {
		t.Errorf("rights = %v", rights)
	}
}
