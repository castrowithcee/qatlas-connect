package googlecloud

import (
	"net/http"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const (
	acctEmail = "deploy@example-project-123.iam.gserviceaccount.com"
	acctPath  = "/v1/projects/example-project-123/serviceAccounts/"
	keyPrefix = "projects/example-project-123/serviceAccounts/" + acctEmail + "/keys/"
)

func saRow(name, project string) string {
	return `{"name":"projects/` + project + `/serviceAccounts/` + name + `@` + project + `.iam.gserviceaccount.com",` +
		`"projectId":"` + project + `","uniqueId":"1034","email":"` + name + `@` + project + `.iam.gserviceaccount.com",` +
		`"displayName":"Deploy","description":"CI","oauth2ClientId":"1034","disabled":true,"etag":"AAA=",` +
		`"privateKeyData":"c2VjcmV0"}`
}

func TestServiceAccountsListPagesAndDropsDeprecatedEtag(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.hook = func(_ int, req *http.Request) *reply {
		switch req.URL.Query().Get("pageToken") {
		case "":
			r := okReply(`{"accounts":[` + saRow("deploy", "example-project-123") + `],"nextPageToken":"t2"}`)
			return &r
		case "t2":
			r := okReply(`{"accounts":[` + saRow("build", "example-project-123") + `]}`)
			return &r
		}
		return nil
	}
	out, err := e.invoke(serviceAccountsListID, `{"project_id":"example-project-123","page_size":1}`)
	if err != nil {
		t.Fatal(err)
	}
	got := decode(t, out)
	cursor, _ := got["next_cursor"].(string)
	if got["has_more"] != true || cursor == "" || len(got["accounts"].([]any)) != 1 {
		t.Fatalf("page 1 = %s", out)
	}
	for _, leaked := range []string{"etag", "AAA=", "privateKeyData", "c2VjcmV0"} {
		if strings.Contains(out, leaked) {
			t.Errorf("result holds %q: %s", leaked, out)
		}
	}
	account := got["accounts"].([]any)[0].(map[string]any)
	if account["email"] != acctEmail || account["projectId"] != "example-project-123" || account["disabled"] != true {
		t.Errorf("account = %v", account)
	}
	out, err = e.invoke(serviceAccountsListID, `{"project_id":"example-project-123","page_size":1,"cursor":"`+cursor+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	if got := decode(t, out); got["has_more"] != false || got["next_cursor"] != nil || len(got["accounts"].([]any)) != 1 {
		t.Errorf("page 2 = %s", out)
	}
	api := e.fake.api()
	if len(api) != 2 || api[0].method != "GET" || !strings.HasPrefix(api[0].url, "https://iam.googleapis.com/v1/projects/example-project-123/serviceAccounts?") ||
		!strings.Contains(api[1].url, "pageToken=t2") {
		t.Errorf("requests = %+v", api)
	}
	// A cursor of another project is refused before any secret.
	e2 := newEnvironment(t, nil)
	if _, err := e2.invoke(serviceAccountsListID, `{"project_id":"other-project-1","cursor":"`+cursor+`"}`); err == nil {
		t.Error("a cursor of another request was accepted")
	}
	noIO(t, e2, "foreign cursor")
}

func TestServiceAccountsListRefusesAnAccountOfAnotherProject(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes["/v1/projects/example-project-123/serviceAccounts"] = okReply(`{"accounts":[` +
		saRow("deploy", "example-other-9") + `]}`)
	_, err := e.invoke(serviceAccountsListID, `{"project_id":"example-project-123"}`)
	if classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("class = %q, err = %v", classOf(err), err)
	}
}

func TestServiceAccountsGetByEmailAndUniqueID(t *testing.T) {
	for _, account := range []string{acctEmail, "1034"} {
		e := newEnvironment(t, []string{"projects/example-project-123"})
		e.fake.routes[acctPath+account] = okReply(saRow("deploy", "example-project-123"))
		out, err := e.invoke(serviceAccountsGetID, `{"project_id":"example-project-123","account":"`+account+`"}`)
		if err != nil {
			t.Fatalf("%s: %v", account, err)
		}
		if got := decode(t, out); got["uniqueId"] != "1034" || got["name"] != "projects/example-project-123/serviceAccounts/"+acctEmail {
			t.Errorf("result = %s", out)
		}
		if api := e.fake.api(); len(api) != 1 {
			t.Errorf("a project target that names the project needs no proof, requests = %+v", api)
		}
	}
}

func TestServiceAccountsGetByProjectNumberProvesTheProject(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes["/v3/projects/415104041262"] = okReply(exampleProjectBody)
	e.fake.routes[acctPath+acctEmail] = okReply(saRow("deploy", "example-project-123"))
	if _, err := e.invoke(serviceAccountsGetID, `{"project_id":"415104041262","account":"`+acctEmail+`"}`); err != nil {
		t.Fatal(err)
	}
	api := e.fake.api()
	if len(api) != 2 || !strings.Contains(api[1].url, "/v1/projects/example-project-123/serviceAccounts/") {
		t.Errorf("requests = %+v", api)
	}
	// With a number the address is compared once the project is known.
	e = newEnvironment(t, nil)
	e.fake.routes["/v3/projects/415104041262"] = okReply(exampleProjectBody)
	_, err := e.invoke(serviceAccountsGetID, `{"project_id":"415104041262","account":"x@example-other-9.iam.gserviceaccount.com"}`)
	if err == nil {
		t.Errorf("err = %v", err)
	}
	if len(e.fake.api()) != 1 {
		t.Errorf("requests = %+v, want only the project read", e.fake.api())
	}
}

func TestServiceAccountsOfAForeignProjectAreRefusedBeforeIO(t *testing.T) {
	e := newEnvironment(t, nil)
	if _, err := e.invoke(serviceAccountsGetID,
		`{"project_id":"example-project-123","account":"deploy@example-other-9.iam.gserviceaccount.com"}`); err == nil {
		t.Fatal("an account of another project was read")
	}
	noIO(t, e, "address of another project")

	e = newEnvironment(t, []string{"projects/example-project-123"})
	if _, err := e.invoke(serviceAccountsListID, `{"project_id":"example-other-9"}`); err == nil {
		t.Fatal("a project outside the targets was listed")
	}
	noIO(t, e, "project outside the targets")

	e = newEnvironment(t, []string{"billingAccounts/ABCDEF-ABCDEF-ABCDEF"})
	if _, err := e.invoke(serviceAccountsListID, `{"project_id":"example-project-123"}`); err == nil {
		t.Fatal("a billing-only connection listed accounts")
	}
	noIO(t, e, "billing-only connection")
}

func TestServiceAccountsGetRefusesAnAnswerOfAnotherProject(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes[acctPath+"1034"] = okReply(saRow("deploy", "example-other-9"))
	_, err := e.invoke(serviceAccountsGetID, `{"project_id":"example-project-123","account":"1034"}`)
	if classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("class = %q, err = %v", classOf(err), err)
	}
}

func TestServiceAccountArgumentsAreNeverFreePaths(t *testing.T) {
	for _, account := range []string{"../x", "projects/p/serviceAccounts/x", "a@example.com", "x@example.iam.gserviceaccount.com/keys",
		"deploy@example-project-123.iam.gserviceaccount.com?x=1", ""} {
		e := newEnvironment(t, nil)
		if _, err := e.invoke(serviceAccountsGetID, `{"project_id":"example-project-123","account":"`+account+`"}`); err == nil {
			t.Errorf("account %q was accepted", account)
		}
		noIO(t, e, "account "+account)
	}
	e := newEnvironment(t, nil)
	if _, err := e.invoke(serviceAccountsListID, `{"project_id":"projects/example-project-123"}`); err == nil {
		t.Error("a resource name was accepted as project_id")
	}
	noIO(t, e, "project name")
}

func TestServiceAccountErrors(t *testing.T) {
	for name, c := range map[string]struct {
		r     reply
		class provider.Class
	}{
		"403": {reply{status: 403, body: errorObject(403, "PERMISSION_DENIED", "")}, provider.ClassPermission},
		"404": {reply{status: 404, body: errorObject(404, "NOT_FOUND", "")}, provider.ClassNotFound},
	} {
		for _, tool := range []struct{ id, args string }{
			{serviceAccountsListID, `{"project_id":"example-project-123"}`},
			{serviceAccountsGetID, `{"project_id":"example-project-123","account":"1034"}`},
			{serviceAccountKeysListID, `{"project_id":"example-project-123","account":"1034"}`},
			{serviceAccountKeysGetID, `{"project_id":"example-project-123","account":"1034","key_id":"abc"}`},
		} {
			e := newEnvironment(t, nil)
			e.fake.hook = func(_ int, _ *http.Request) *reply { return &c.r }
			_, err := e.invoke(tool.id, tool.args)
			if classOf(err) != c.class {
				t.Errorf("%s %s: class = %q, err = %v", name, tool.id, classOf(err), err)
			}
			if err != nil && strings.Contains(err.Error(), detailCanary) {
				t.Errorf("%s %s: the message echoes Google's text", name, tool.id)
			}
		}
	}
}

func TestServiceAccountToolsNeedTheBroadScopeAndTheIAMProfile(t *testing.T) {
	for _, id := range []string{serviceAccountsListID, serviceAccountsGetID, serviceAccountKeysListID, serviceAccountKeysGetID} {
		if scopes := tokenMetadata.Scopes[id]; len(scopes) != 1 || scopes[0] != scopeCloudPlatform {
			t.Errorf("%s scopes = %v", id, scopes)
		}
		for _, tool := range readTools {
			if tool == id {
				t.Errorf("%s is in the read profile", id)
			}
		}
	}
}

// exampleProjectBody is projectBody for the project ID the service account tests use.
var exampleProjectBody = strings.Replace(projectBody, `"projectId":"my-project-123"`, `"projectId":"example-project-123"`, 1)
