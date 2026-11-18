package googlecloud

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const (
	// The account addresses are assembled so that no complete address stands in the source.
	hmacAccountDomain  = ".iam.gserviceaccount.com"
	hmacCanaryMaterial = "placeholder-hmac-material"
	hmacProjectPath    = "/v3/projects/my-project-123"
	hmacKeysPathForT   = "/storage/v1/projects/my-project-123/hmacKeys"
)

func hmacAccount(name, project string) string { return name + "@" + project + hmacAccountDomain }

func hmacRow(access, project, account, state string) string {
	return `{"kind":"storage#hmacKeyMetadata","id":"` + project + `/` + access + `","selfLink":"https://example.invalid/k",` +
		`"accessId":"` + access + `","projectId":"` + project + `","serviceAccountEmail":"` + account + `","state":"` + state +
		`","timeCreated":"2026-01-02T03:04:05.000Z","updated":"2026-02-03T04:05:06.000Z","etag":"CAE=",` +
		`"secret":"` + hmacCanaryMaterial + `","metadata":{"secret":"` + hmacCanaryMaterial + `"}}`
}

func hmacEnvironment(t *testing.T, targets []string) *environment {
	e := newEnvironment(t, targets)
	e.fake.routes[hmacProjectPath] = okReply(projectBody)
	return e
}

func TestHMACKeysListFollowsPagesWithFilterAndNeverOutputsASecret(t *testing.T) {
	account := hmacAccount("deploy-account", "my-project-123")
	e := hmacEnvironment(t, nil)
	e.fake.hook = func(_ int, req *http.Request) *reply {
		if req.URL.Path != hmacKeysPathForT {
			return nil
		}
		if req.URL.Query().Get("pageToken") == "" {
			return &reply{status: 200, body: `{"kind":"storage#hmacKeysMetadata","secret":"` + hmacCanaryMaterial + `","items":[` +
				hmacRow("EXAMPLEACCESSID0001", storageProjectNumber, account, "ACTIVE") + `],"nextPageToken":"p2"}`}
		}
		return &reply{status: 200, body: `{"items":[` + hmacRow("EXAMPLEACCESSID0002", "my-project-123", account, "DELETED") + `]}`}
	}
	base := `{"project_id":"my-project-123","service_account_email":"` + account + `","show_deleted_keys":true,"max_results":1`
	out, err := e.invoke(hmacKeysListID, base+`}`)
	if err != nil {
		t.Fatal(err)
	}
	rows, got := listOf(t, out, "hmac_keys")
	if len(rows) != 1 || rows[0]["accessId"] != "EXAMPLEACCESSID0001" || rows[0]["state"] != "ACTIVE" ||
		rows[0]["serviceAccountEmail"] != account || rows[0]["timeCreated"] == nil || rows[0]["updated"] == nil ||
		rows[0]["etag"] != "CAE=" || rows[0]["id"] == nil || got["has_more"] != true {
		t.Fatalf("page 1 = %s", out)
	}
	second, err := e.invoke(hmacKeysListID, base+`,"cursor":"`+got["next_cursor"].(string)+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	rows2, got2 := listOf(t, second, "hmac_keys")
	if len(rows2) != 1 || rows2[0]["state"] != "DELETED" || got2["has_more"] != false {
		t.Errorf("page 2 = %s", second)
	}
	for _, text := range []string{out, second} {
		if strings.Contains(text, hmacCanaryMaterial) || strings.Contains(strings.ToLower(text), "secret") {
			t.Errorf("a secret reached the result: %s", text)
		}
	}
	var lists int
	for _, q := range queriesOf(e) {
		if !q.Has("maxResults") {
			continue
		}
		lists++
		if q.Get("serviceAccountEmail") != account || q.Get("showDeletedKeys") != "true" || q.Get("maxResults") != "1" ||
			(q.Get("pageToken") == "p2") != (lists == 2) {
			t.Errorf("query = %v", q)
		}
	}
	if lists != 2 {
		t.Errorf("list requests = %d", lists)
	}
}

func TestHMACKeysListRefusesAForeignAccountBeforeTheListRequest(t *testing.T) {
	foreign := []string{hmacAccount("other-account", "other-project-1"), "999999999999-compute@" + computeDomain,
		"other-project@" + appspotDomain}
	for _, account := range foreign {
		e := hmacEnvironment(t, nil)
		e.fake.routes[hmacKeysPathForT] = okReply(`{"items":[]}`)
		_, err := e.invoke(hmacKeysListID, `{"project_id":"my-project-123","service_account_email":"`+account+`"}`)
		if !isInvalidRequest(err) || strings.Contains(err.Error(), account) || strings.Contains(err.Error(), "other-") {
			t.Errorf("%s: err = %v", account, err)
		}
		for _, p := range apiPaths(e) {
			if strings.Contains(p, "hmacKeys") {
				t.Errorf("%s: the list was requested: %v", account, apiPaths(e))
			}
		}
	}
	// The default accounts of the bound project are accepted.
	for _, account := range []string{"415104041262-compute@" + computeDomain, "my-project-123@" + appspotDomain} {
		e := hmacEnvironment(t, nil)
		e.fake.routes[hmacKeysPathForT] = okReply(`{"items":[]}`)
		if _, err := e.invoke(hmacKeysListID, `{"project_id":"my-project-123","service_account_email":"`+account+`"}`); err != nil {
			t.Errorf("%s: %v", account, err)
		}
	}
	// A wrong form is refused before the secret and any request.
	for _, account := range []string{"user@" + "example.com", "a/b", "x" + hmacAccountDomain, hmacAccount("ok-account", "my-project-123") + "/x"} {
		e := newEnvironment(t, nil)
		if _, err := e.invoke(hmacKeysListID, `{"project_id":"my-project-123","service_account_email":"`+account+`"}`); !isInvalidRequest(err) {
			t.Errorf("%s: err = %v", account, err)
		}
		if len(e.fake.api()) != 0 || e.fake.tokenRequests() != 0 || e.lookups != 0 {
			t.Errorf("%s: api=%d tokens=%d lookups=%d", account, len(e.fake.api()), e.fake.tokenRequests(), e.lookups)
		}
	}
}

func TestHMACKeysListChecksEveryRowAgainstTheProject(t *testing.T) {
	account := hmacAccount("deploy-account", "my-project-123")
	e := hmacEnvironment(t, nil)
	e.fake.routes[hmacKeysPathForT] = okReply(`{"items":[` +
		hmacRow("EXAMPLEACCESSID0001", storageProjectNumber, account, "ACTIVE") + `,` +
		hmacRow("EXAMPLEACCESSID0002", foreignNumber, account, "ACTIVE") + `,` +
		hmacRow("EXAMPLEACCESSID0003", "other-project-1", account, "ACTIVE") + `,` +
		`{"accessId":"EXAMPLEACCESSID0004","state":"ACTIVE"},` +
		hmacRow("EXAMPLEACCESSID0005", "my-project-123", account, "INACTIVE") + `]}`)
	out, err := e.invoke(hmacKeysListID, `{"project_id":"my-project-123"}`)
	if err != nil {
		t.Fatal(err)
	}
	rows, _ := listOf(t, out, "hmac_keys")
	var ids []string
	for _, r := range rows {
		ids = append(ids, r["accessId"].(string))
	}
	if !slices.Equal(ids, []string{"EXAMPLEACCESSID0001", "EXAMPLEACCESSID0005"}) {
		t.Errorf("ids = %v, out = %s", ids, out)
	}
	// With a filter, a row of another account is dropped as well.
	e = hmacEnvironment(t, nil)
	e.fake.routes[hmacKeysPathForT] = okReply(`{"items":[` +
		hmacRow("EXAMPLEACCESSID0001", storageProjectNumber, account, "ACTIVE") + `,` +
		hmacRow("EXAMPLEACCESSID0002", storageProjectNumber, hmacAccount("another-account", "my-project-123"), "ACTIVE") + `]}`)
	out, err = e.invoke(hmacKeysListID, `{"project_id":"my-project-123","service_account_email":"`+account+`"}`)
	if rows, _ := listOf(t, out, "hmac_keys"); err != nil || len(rows) != 1 {
		t.Errorf("out = %s, err = %v", out, err)
	}
	// A non-string member is an invalid answer, not a way around the allow-list.
	e = hmacEnvironment(t, nil)
	e.fake.routes[hmacKeysPathForT] = okReply(`{"items":[{"accessId":"EXAMPLEACCESSID0001","etag":{"secret":"` + hmacCanaryMaterial + `"}}]}`)
	if out, err := e.invoke(hmacKeysListID, `{"project_id":"my-project-123"}`); classOf(err) != provider.ClassInvalidResponse ||
		strings.Contains(out, hmacCanaryMaterial) || err != nil && strings.Contains(err.Error(), hmacCanaryMaterial) {
		t.Errorf("out = %q, err = %v", out, err)
	}
}

func TestHMACKeysGetReturnsMetadataOnly(t *testing.T) {
	account := hmacAccount("deploy-account", "my-project-123")
	e := hmacEnvironment(t, nil)
	e.fake.routes[hmacKeysPathForT+"/EXAMPLEACCESSID0001"] = okReply(hmacRow("EXAMPLEACCESSID0001", storageProjectNumber, account, "ACTIVE"))
	out, err := e.invoke(hmacKeysGetID, `{"project_id":"my-project-123","access_id":"EXAMPLEACCESSID0001","user_project":"my-project-123"}`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, hmacCanaryMaterial) || strings.Contains(strings.ToLower(out), "secret") ||
		!strings.Contains(out, `"accessId":"EXAMPLEACCESSID0001"`) || !strings.Contains(out, `"state":"ACTIVE"`) ||
		!strings.Contains(out, `"serviceAccountEmail"`) || !strings.Contains(out, `"etag":"CAE="`) {
		t.Errorf("out = %s", out)
	}
	if q := queriesOf(e); q[len(q)-1].Get("userProject") != "my-project-123" {
		t.Errorf("query = %v", q)
	}
	// A key of another project, or another key than asked for, is refused without being shown.
	e = hmacEnvironment(t, nil)
	e.fake.routes[hmacKeysPathForT+"/EXAMPLEACCESSID0002"] = okReply(hmacRow("EXAMPLEACCESSID0002", foreignNumber, account, "ACTIVE"))
	out, err = e.invoke(hmacKeysGetID, `{"project_id":"my-project-123","access_id":"EXAMPLEACCESSID0002"}`)
	if !isInvalidRequest(err) || out != "" || strings.Contains(err.Error(), foreignNumber) {
		t.Errorf("out = %q, err = %v", out, err)
	}
	e.fake.routes[hmacKeysPathForT+"/EXAMPLEACCESSID0003"] = okReply(hmacRow("EXAMPLEACCESSID0009", storageProjectNumber, account, "ACTIVE"))
	if _, err := e.invoke(hmacKeysGetID, `{"project_id":"my-project-123","access_id":"EXAMPLEACCESSID0003"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("err = %v", err)
	}
}

func TestHMACKeysRefuseBadInputAndForeignProjectsBeforeIO(t *testing.T) {
	for name, c := range map[string]struct{ tool, arguments string }{
		"slash":         {hmacKeysGetID, `{"project_id":"my-project-123","access_id":"a/b/c/d/e/f/g/h"}`},
		"traversal":     {hmacKeysGetID, `{"project_id":"my-project-123","access_id":"../../x/yz12345"}`},
		"query":         {hmacKeysGetID, `{"project_id":"my-project-123","access_id":"abcdefgh?x=1"}`},
		"encoded slash": {hmacKeysGetID, `{"project_id":"my-project-123","access_id":"abc%2Fdefgh"}`},
		"short":         {hmacKeysGetID, `{"project_id":"my-project-123","access_id":"abc"}`},
		"missing":       {hmacKeysGetID, `{"project_id":"my-project-123"}`},
		"project path":  {hmacKeysGetID, `{"project_id":"projects/x","access_id":"EXAMPLEACCESSID0001"}`},
		"list path":     {hmacKeysListID, `{"project_id":"a/b"}`},
		"max results":   {hmacKeysListID, `{"project_id":"my-project-123","max_results":251}`},
		"unknown":       {hmacKeysListID, `{"project_id":"my-project-123","serviceAccountEmail":"x"}`},
	} {
		e := newEnvironment(t, nil)
		if _, err := e.invoke(c.tool, c.arguments); err == nil {
			t.Errorf("%s: want a refusal", name)
		}
		if len(e.fake.api()) != 0 || e.fake.tokenRequests() != 0 || e.lookups != 0 {
			t.Errorf("%s: api=%d tokens=%d lookups=%d", name, len(e.fake.api()), e.fake.tokenRequests(), e.lookups)
		}
	}
	// The handlers refuse what the schema would not have let through.
	e := newEnvironment(t, nil)
	if _, err := handleHMACKeysGet(t.Context(), e.resolved(), e.secrets(), e.red,
		[]byte(`{"project_id":"my-project-123","access_id":"a/b"}`)); !isInvalidRequest(err) {
		t.Errorf("err = %v", err)
	}
	if len(e.fake.api()) != 0 || e.lookups != 0 {
		t.Errorf("api=%d lookups=%d", len(e.fake.api()), e.lookups)
	}
	for _, id := range []string{hmacKeysListID, hmacKeysGetID} {
		for _, arguments := range []string{`{"project_id":"other-project-1","access_id":"EXAMPLEACCESSID0001"}`,
			`{"project_id":"my-project-123","access_id":"EXAMPLEACCESSID0001","user_project":"other-project-1"}`} {
			e := newEnvironment(t, []string{"projects/my-project-123"})
			_, err := e.invoke(id, arguments)
			if !isInvalidRequest(err) || strings.Contains(err.Error(), "other-project-1") {
				t.Errorf("%s %s: err = %v", id, arguments, err)
			}
			if len(e.fake.api()) != 0 || e.fake.tokenRequests() != 0 || e.lookups != 0 {
				t.Errorf("%s %s: api=%d tokens=%d lookups=%d", id, arguments, len(e.fake.api()), e.fake.tokenRequests(), e.lookups)
			}
		}
	}
}

func TestHMACKeysAreBoundToTheTargetsByTheProjectRead(t *testing.T) {
	// The targets name the project by its number, so the ID is proved by reading the project.
	e := hmacEnvironment(t, []string{"projects/" + storageProjectNumber})
	e.fake.routes[hmacKeysPathForT] = okReply(`{"items":[]}`)
	if _, err := e.invoke(hmacKeysListID, `{"project_id":"my-project-123"}`); err != nil {
		t.Fatal(err)
	}
	e = hmacEnvironment(t, []string{"projects/other-project-1"})
	e.fake.routes[hmacKeysPathForT] = okReply(`{"items":[]}`)
	if _, err := e.invoke(hmacKeysListID, `{"project_id":"my-project-123"}`); !isInvalidRequest(err) {
		t.Errorf("err = %v", err)
	}
	for _, p := range apiPaths(e) {
		if strings.Contains(p, "hmacKeys") {
			t.Errorf("the list was requested: %v", apiPaths(e))
		}
	}
}

func TestHMACKeysMapErrors(t *testing.T) {
	for _, status := range []struct {
		code   int
		reason string
		want   provider.Class
	}{{403, "PERMISSION_DENIED", provider.ClassPermission}, {404, "NOT_FOUND", provider.ClassNotFound}} {
		for _, c := range []struct{ id, arguments string }{
			{hmacKeysListID, `{"project_id":"my-project-123"}`},
			{hmacKeysGetID, `{"project_id":"my-project-123","access_id":"EXAMPLEACCESSID0001"}`},
		} {
			e := hmacEnvironment(t, nil)
			e.fake.hook = func(_ int, req *http.Request) *reply {
				if strings.HasPrefix(req.URL.Path, "/storage/") {
					return &reply{status: status.code, body: errorObject(status.code, status.reason, "")}
				}
				return nil
			}
			_, err := e.invoke(c.id, c.arguments)
			if classOf(err) != status.want || err != nil && strings.Contains(err.Error(), detailCanary) {
				t.Errorf("%s %d: err = %v", c.id, status.code, err)
			}
		}
	}
}

func TestNotificationAndHMACToolsAreReadToolsOfTheProfileRead(t *testing.T) {
	for _, id := range []string{notificationsListID, managedFoldersListID, hmacKeysListID, hmacKeysGetID} {
		if got := tokenMetadata.Scopes[id]; len(got) != 1 || got[0] != scopeStorageReadOnly {
			t.Errorf("%s scopes = %v", id, got)
		}
		if !slices.Contains(readTools, id) {
			t.Errorf("%s is not in the profile read", id)
		}
		if ok, _ := targetReaches(id, []string{"billingAccounts/AAAAAA-BBBBBB-CCCCCC"}); ok {
			t.Errorf("%s is offered to a billing-only connection", id)
		}
		e := newEnvironment(t, nil)
		d, _, found := e.reg.Lookup(id)
		if !found || d.Risk != readRisk || !strings.Contains(d.Description, "untrusted") {
			t.Errorf("%s not registered as a read tool", id)
		}
	}
}
