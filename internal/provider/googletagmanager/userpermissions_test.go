package googletagmanager

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const (
	testUserPermission = "accounts/1000001/user_permissions/4000004"
	testUserAddress    = "person@example.org"
	testOtherAddress   = "other@example.org"
)

func storedUser(containers ...[2]string) string {
	var list []map[string]string
	for _, c := range containers {
		list = append(list, map[string]string{"containerId": c[0], "permission": c[1]})
	}
	raw, _ := json.Marshal(map[string]any{"path": testUserPermission, "accountId": "1000001",
		"emailAddress": testUserAddress, "accountAccess": map[string]string{"permission": "user"},
		"containerAccess": list})
	return string(raw)
}

const previousArgs = `"previous_email_address":"` + testUserAddress + `","previous_account_access":{"permission":"user"},` +
	`"previous_container_access":[{"container":"accounts/1000001/containers/2000002","permission":"read"}]`

const updateArgs = `{"user_permission":"` + testUserPermission + `","accountAccess":{"permission":"admin"},` +
	`"containerAccess":[{"container":"accounts/1000001/containers/2000002","permission":"publish"}],` + previousArgs + `}`

func TestUserPermissionToolsAreAFixedGroupWithoutCreate(t *testing.T) {
	want := []string{Provider + ".user-permissions.delete", Provider + ".user-permissions.get",
		Provider + ".user-permissions.list", Provider + ".user-permissions.update"}
	var got []string
	for _, d := range descriptors() {
		if strings.Contains(d.ID, "user-permissions") {
			got = append(got, d.ID)
		}
	}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("tools = %v", got)
	}
}

func TestUserPermissionToolsHaveTheirOwnProfileAndScope(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	for _, id := range userPermissionIDs() {
		if got := metadata.Token.Scopes[id]; !slices.Equal(got, []string{usersScope}) {
			t.Errorf("%s scopes = %v", id, got)
		}
		d := toolOf(t, id)
		if d.Risk.DataSensitivity != userDataSensitivity || d.RequiresToolAllowList {
			t.Errorf("%s risk = %+v, listed-only = %v", id, d.Risk, d.RequiresToolAllowList)
		}
		if !strings.Contains(d.Description, "personal data") {
			t.Errorf("%s description lacks the warning about personal data", id)
		}
	}
	for _, id := range []string{userPermissionsUpdateID, userPermissionsDeleteID} {
		d := toolOf(t, id)
		if d.Risk.Confirmation != capability.ConfirmationRequired || reg.ConfirmationSummary(id) == nil {
			t.Errorf("%s needs a confirmation with a summary", id)
		}
		if !strings.Contains(d.Description, "lock") {
			t.Errorf("%s description lacks the self-lockout note", id)
		}
	}
	for _, p := range metadata.Profiles {
		for _, id := range userPermissionIDs() {
			has := slices.Contains(p.Tools, id)
			if has != (p.ID == userPermissionsProfile) {
				t.Errorf("profile %s holds %s = %v", p.ID, id, has)
			}
		}
		if p.ID == userPermissionsProfile && p.Recommended {
			t.Error("the user permission profile is recommended")
		}
	}
}

func TestUserPermissionGroupIsUnavailableForAContainerBinding(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	for _, id := range userPermissionIDs() {
		if ok, _ := metadata.Target.Reach(id, []string{"accounts/1000001/containers/2000002"}); ok {
			t.Errorf("%s is offered to a container binding", id)
		}
		for _, targets := range [][]string{nil, {"accounts/1000001"}} {
			if ok, _ := metadata.Target.Reach(id, targets); !ok {
				t.Errorf("%s is not offered to %v", id, targets)
			}
		}
	}
	if ok, _ := metadata.Target.Reach(tagsListID, []string{"accounts/1000001/containers/2000002"}); !ok {
		t.Error("a container binding lost another tool")
	}
	h := newHarness(t, "accounts/1000001/containers/2000002", testClientID)
	for id, handler := range map[string]struct {
		h    capability.Handler
		args string
	}{userPermissionsListID: {handleUserPermissionsList, `{}`},
		userPermissionsGetID:    {handleUserPermissionsGet, `{"user_permission":"` + testUserPermission + `"}`},
		userPermissionsUpdateID: {handleUserPermissionsUpdate, updateArgs},
		userPermissionsDeleteID: {handleUserPermissionsDelete, `{"user_permission":"` + testUserPermission + `"}`}} {
		if _, err := h.call(t, handler.h, handler.args); !isInvalidRequest(err) {
			t.Errorf("%s with a container binding = %v", id, err)
		}
	}
	if len(h.google.seen()) != 0 {
		t.Error("a container binding sent a request")
	}
}

func TestAccountBindingRefusesOtherAccountsBeforeAnyRequest(t *testing.T) {
	h := newHarness(t, "accounts/1000001", testClientID)
	other := "accounts/1000009/user_permissions/4000004"
	for name, call := range map[string]func() error{
		"get": func() error {
			_, err := h.call(t, handleUserPermissionsGet, `{"user_permission":"`+other+`"}`)
			return err
		},
		"list": func() error {
			_, err := h.call(t, handleUserPermissionsList, `{"account":"accounts/1000009"}`)
			return err
		},
		"delete": func() error {
			_, err := h.call(t, handleUserPermissionsDelete, `{"user_permission":"`+other+`"}`)
			return err
		},
		"update": func() error {
			_, err := h.call(t, handleUserPermissionsUpdate, strings.Replace(updateArgs, testUserPermission, other, 1))
			return err
		},
	} {
		if err := call(); !isInvalidRequest(err) {
			t.Errorf("%s = %v", name, err)
		}
	}
	if len(h.google.seen()) != 0 {
		t.Error("a path of another account sent a request")
	}
}

func TestUserPermissionsListReadsSeveralPages(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		if req.URL.Query().Get("pageToken") == "" {
			return 200, `{"userPermission":[` + storedUser([2]string{"2000002", "edit"}) + `],"nextPageToken":"p2"}`, nil
		}
		return 200, `{"userPermission":[` + storedUser() + `]}`, nil
	}
	first, err := h.call(t, handleUserPermissionsList, `{"account":"accounts/1000001"}`)
	if err != nil {
		t.Fatal(err)
	}
	cursor, _ := first["next_cursor"].(string)
	if cursor == "" {
		t.Fatalf("first page = %v", first)
	}
	entry := first["user_permissions"].([]any)[0].(map[string]any)
	if entry["emailAddress"] != testUserAddress || entry["path"] != testUserPermission || entry["accountId"] != "1000001" ||
		entry["accountAccess"].(map[string]any)["permission"] != "user" ||
		entry["containerAccess"].([]any)[0].(map[string]any)["containerId"] != "2000002" {
		t.Errorf("entry = %v", entry)
	}
	args, _ := json.Marshal(map[string]string{"account": "accounts/1000001", "cursor": cursor})
	second, err := h.call(t, handleUserPermissionsList, string(args))
	if err != nil || second["next_cursor"] != nil || len(second["user_permissions"].([]any)) != 1 {
		t.Fatalf("second page = %v, %v", second, err)
	}
	if got := h.paths(t); !slices.Equal(got, []string{"/tagmanager/v2/accounts/1000001/user_permissions",
		"/tagmanager/v2/accounts/1000001/user_permissions?pageToken=p2"}) {
		t.Errorf("paths = %v", got)
	}
}

func TestUserPermissionsGetReturnsEveryMember(t *testing.T) {
	h := newHarness(t, "accounts/1000001", testClientID)
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, storedUser([2]string{"2000002", "approve"}), nil
	}
	out, err := h.call(t, handleUserPermissionsGet, `{"user_permission":"`+testUserPermission+`"}`)
	if err != nil || out["emailAddress"] != testUserAddress || out["accountId"] != "1000001" ||
		out["containerAccess"].([]any)[0].(map[string]any)["permission"] != "approve" {
		t.Fatalf("out = %v, %v", out, err)
	}
}

func TestUserPermissionsUpdateReadsFirstAndSendsTheStoredAddress(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		if req.Method == http.MethodGet {
			return 200, storedUser([2]string{"2000002", "read"}), nil
		}
		return 200, strings.Replace(storedUser([2]string{"2000002", "publish"}), `"user"`, `"admin"`, 1), nil
	}
	out, err := h.call(t, handleUserPermissionsUpdate, strings.Replace(updateArgs, testUserAddress, strings.ToUpper(testUserAddress), 1))
	if err != nil {
		t.Fatal(err)
	}
	sent := h.sent(t)
	if len(sent) != 2 || sent[0].method != "GET" || sent[1].method != "PUT" ||
		sent[1].path != "/tagmanager/v2/"+testUserPermission || sent[1].contentType != "application/json" {
		t.Fatalf("requests = %+v", sent)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(sent[1].body), &body); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"emailAddress": testUserAddress, "accountAccess": map[string]any{"permission": "admin"},
		"containerAccess": []any{map[string]any{"containerId": "2000002", "permission": "publish"}}}
	if !jsonEqual(body, want) || strings.Contains(sent[1].body, "previous") {
		t.Errorf("body = %v", body)
	}
	if out["accountAccess"].(map[string]any)["permission"] != "admin" || out["emailAddress"] != testUserAddress {
		t.Errorf("out = %v", out)
	}
}

func TestUserPermissionsUpdateRefusesStalePreviousValuesWithoutWriting(t *testing.T) {
	stale := map[string]string{
		"address": strings.Replace(updateArgs, testUserAddress, testOtherAddress, 1),
		"account": strings.Replace(updateArgs, `"previous_account_access":{"permission":"user"}`,
			`"previous_account_access":{"permission":"admin"}`, 1),
		"container permission": strings.Replace(updateArgs, `"permission":"read"`, `"permission":"edit"`, 1),
		"container removed": strings.Replace(updateArgs, `"previous_container_access":[{"container":"accounts/1000001/containers/2000002","permission":"read"}]`,
			`"previous_container_access":[]`, 1),
	}
	for name, args := range stale {
		h := newHarness(t, "", testClientID)
		h.google.api = func(*http.Request) (int, string, http.Header) {
			return 200, storedUser([2]string{"2000002", "read"}), nil
		}
		_, err := h.call(t, handleUserPermissionsUpdate, args)
		if !isInvalidRequest(err) {
			t.Errorf("%s: err = %v", name, err)
			continue
		}
		if strings.Contains(err.Error(), "example.org") {
			t.Errorf("%s: the refusal quotes an address: %v", name, err)
		}
		for _, req := range h.google.seen() {
			if req.Method != http.MethodGet {
				t.Errorf("%s: %s was sent", name, req.Method)
			}
		}
	}
}

func jsonEqual(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

func TestUserPermissionsUpdateOfAMissingEntryWritesNothing(t *testing.T) {
	for name, api := range map[string]func(*http.Request) (int, string, http.Header){
		"404":   func(*http.Request) (int, string, http.Header) { return 404, googleBody(404, "NOT_FOUND", ""), nil },
		"empty": func(*http.Request) (int, string, http.Header) { return 200, `{}`, nil },
	} {
		h := newHarness(t, "", testClientID)
		h.google.api = api
		_, err := h.call(t, handleUserPermissionsUpdate, updateArgs)
		if classOf(err) != provider.ClassNotFound {
			t.Errorf("%s: err = %v", name, err)
		}
		for _, req := range h.google.seen() {
			if req.Method != http.MethodGet {
				t.Errorf("%s: %s was sent", name, req.Method)
			}
		}
	}
}

func TestUserPermissionsUpdateRefusesForeignContainersAndAddressesBeforeAnyRequest(t *testing.T) {
	h := newHarness(t, "", testClientID)
	base := `{"user_permission":"` + testUserPermission + `","accountAccess":{"permission":"user"},` + previousArgs + `,"containerAccess":`
	for name, args := range map[string]string{
		"foreign container": base + `[{"container":"accounts/1000009/containers/2000002","permission":"read"}]}`,
		"twice": base + `[{"container":"accounts/1000001/containers/2000002","permission":"read"},` +
			`{"container":"accounts/1000001/containers/2000002","permission":"edit"}]}`,
	} {
		if _, err := h.call(t, handleUserPermissionsUpdate, args); !isInvalidRequest(err) {
			t.Errorf("%s = %v", name, err)
		}
	}
	if len(h.google.seen()) != 0 {
		t.Error("a refused update sent a request")
	}
	schema := userPermissionsUpdate.InputSchema
	for name, args := range map[string]string{
		"emailAddress":          `{"user_permission":"` + testUserPermission + `","emailAddress":"` + testOtherAddress + `","accountAccess":{"permission":"user"},"containerAccess":[],` + previousArgs + `}`,
		"unspecified account":   `{"user_permission":"` + testUserPermission + `","accountAccess":{"permission":"accountPermissionUnspecified"},"containerAccess":[],` + previousArgs + `}`,
		"unspecified container": base + `[{"container":"accounts/1000001/containers/2000002","permission":"containerPermissionUnspecified"}]}`,
		"no containerAccess":    `{"user_permission":"` + testUserPermission + `","accountAccess":{"permission":"user"},` + previousArgs + `}`,
		"no previous values":    `{"user_permission":"` + testUserPermission + `","accountAccess":{"permission":"user"},"containerAccess":[]}`,
		"address as path":       `{"user_permission":"accounts/1000001/user_permissions/` + testOtherAddress + `","accountAccess":{"permission":"user"},"containerAccess":[],` + previousArgs + `}`,
		"member of a container": base + `[{"container":"accounts/1000001/containers/2000002","permission":"read","emailAddress":"x"}]}`,
	} {
		if application.ValidateJSON(schema, json.RawMessage(args)) == nil {
			t.Errorf("schema accepts %s", name)
		}
	}
	if err := application.ValidateJSON(schema, json.RawMessage(updateArgs)); err != nil {
		t.Errorf("schema refuses a valid update: %v", err)
	}
}

func TestUserPermissionsDeleteSendsOneDelete(t *testing.T) {
	h := newHarness(t, "accounts/1000001", testClientID)
	h.google.api = func(*http.Request) (int, string, http.Header) { return 204, ``, nil }
	out, err := h.call(t, handleUserPermissionsDelete, `{"user_permission":"`+testUserPermission+`"}`)
	if err != nil || out["deleted"] != true || out["user_permission"] != testUserPermission {
		t.Fatalf("out = %v, %v", out, err)
	}
	sent := h.sent(t)
	if len(sent) != 1 || sent[0].method != "DELETE" || sent[0].path != "/tagmanager/v2/"+testUserPermission {
		t.Errorf("requests = %+v", sent)
	}
}

func TestUserPermissionSummariesShowAddressOnlyOnUpdate(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	got := reg.ConfirmationSummary(userPermissionsUpdateID)(json.RawMessage(updateArgs))
	for _, want := range []string{testUserPermission, testUserAddress, "account user -> admin",
		"accounts/1000001/containers/2000002 read -> publish"} {
		if !strings.Contains(got, want) {
			t.Errorf("update summary %q lacks %q", got, want)
		}
	}
	added := strings.Replace(updateArgs, `"previous_container_access":[{"container":"accounts/1000001/containers/2000002","permission":"read"}]`,
		`"previous_container_access":[{"container":"accounts/1000001/containers/2000003","permission":"edit"}]`, 1)
	got = reg.ConfirmationSummary(userPermissionsUpdateID)(json.RawMessage(added))
	for _, want := range []string{"2000002 (none) -> publish", "2000003 edit -> (none)"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary of an added and a removed container %q lacks %q", got, want)
		}
	}
	got = reg.ConfirmationSummary(userPermissionsDeleteID)(json.RawMessage(`{"user_permission":"` + testUserPermission + `"}`))
	if !strings.Contains(got, testUserPermission) || strings.Contains(got, "@") {
		t.Errorf("delete summary %q", got)
	}
	for _, id := range []string{userPermissionsUpdateID, userPermissionsDeleteID} {
		if toolOf(t, id).Risk.Confirmation != capability.ConfirmationRequired {
			t.Errorf("%s needs no confirmation", id)
		}
	}
}

func TestUserPermissionErrorsCarryNoAddress(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 403, `{"error":{"code":403,"status":"PERMISSION_DENIED","message":"no right for ` + testUserAddress + `"}}`, nil
	}
	for name, run := range map[string]func() error{
		"get": func() error {
			_, err := h.call(t, handleUserPermissionsGet, `{"user_permission":"`+testUserPermission+`"}`)
			return err
		},
		"update": func() error { _, err := h.call(t, handleUserPermissionsUpdate, updateArgs); return err },
		"delete": func() error {
			_, err := h.call(t, handleUserPermissionsDelete, `{"user_permission":"`+testUserPermission+`"}`)
			return err
		},
		"list": func() error {
			_, err := h.call(t, handleUserPermissionsList, `{"account":"accounts/1000001"}`)
			return err
		},
	} {
		err := run()
		if classOf(err) != provider.ClassPermission {
			t.Errorf("%s = %v", name, err)
			continue
		}
		failed := err.(*provider.Error)
		encoded, _ := json.Marshal(failed.Original)
		if strings.Contains(err.Error(), "example.org") || strings.Contains(string(encoded), "example.org") {
			t.Errorf("%s error carries an address: %v %s", name, err, encoded)
		}
	}
}
