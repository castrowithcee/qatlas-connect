package googlecloud

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const (
	apiKeyTarget = "projects/example-project-123"
	apiKeyID     = "0a1b2c3d-4e5f-6a7b-8c9d-0e1f2a3b4c5d"
	apiKeysPath  = "/v2/projects/example-project-123/locations/global/keys"
	apiKeyURL    = apiKeysPath + "/" + apiKeyID
	// The value is not in the format of a real key, so a secret scan does not mistake it for one.
	fakeKeyValue = "synthetic-key-value-not-secret"
)

func apiKeyRow(id, extra string) string {
	return `{"name":"projects/415104041262/locations/global/keys/` + id + `","uid":"uid-` + id + `",` +
		`"displayName":"Maps key","createTime":"2026-01-02T03:04:05Z","updateTime":"2026-02-03T04:05:06Z",` +
		`"keyString":"` + fakeKeyValue + `","etag":"synthetic-etag"` + extra + `}`
}

const apiKeyRestrictions = `,"annotations":{"team":"web"},"serviceAccountEmail":"maps@example-project-123.iam.gserviceaccount.com",` +
	`"restrictions":{"apiTargets":[{"service":"maps-backend.googleapis.com","methods":["Geocode*"]}],` +
	`"androidKeyRestrictions":{"allowedApplications":[{"sha1Fingerprint":"AA:BB","packageName":"com.example.app"}]},` +
	`"browserKeyRestrictions":{"allowedReferrers":["https://example.com/*"]},` +
	`"serverKeyRestrictions":{"allowedIps":["203.0.113.7"]},` +
	`"iosKeyRestrictions":{"allowedBundleIds":["com.example.ios"]}}`

func TestAPIKeysListFollowsTwoPagesWithShowDeletedAndDropsKeyString(t *testing.T) {
	e := newEnvironment(t, []string{apiKeyTarget})
	e.fake.hook = func(_ int, req *http.Request) *reply {
		if req.URL.Path != apiKeysPath {
			return nil
		}
		if req.URL.Query().Get("pageToken") == "" {
			return &reply{status: 200, body: `{"keys":[` + apiKeyRow("key-one", apiKeyRestrictions) + `],"nextPageToken":"p2"}`}
		}
		return &reply{status: 200, body: `{"keys":[` + apiKeyRow("key-two", `,"deleteTime":"2026-03-04T05:06:07Z"`) + `]}`}
	}
	out, err := e.invoke(apiKeysListID, `{"project_id":"example-project-123","show_deleted":true,"page_size":1}`)
	if err != nil {
		t.Fatal(err)
	}
	rows, page := listOf(t, out, "keys")
	cursor, _ := page["next_cursor"].(string)
	if len(rows) != 1 || page["has_more"] != true || cursor == "" || cursor == "p2" {
		t.Fatalf("first page = %s", out)
	}
	first := rows[0]
	limits := first["restrictions"].(map[string]any)
	if first["displayName"] != "Maps key" || first["etag"] != "synthetic-etag" || first["uid"] != "uid-key-one" ||
		first["serviceAccountEmail"] == nil || first["annotations"].(map[string]any)["team"] != "web" ||
		limits["apiTargets"].([]any)[0].(map[string]any)["methods"].([]any)[0] != "Geocode*" ||
		limits["androidKeyRestrictions"].(map[string]any)["allowedApplications"].([]any)[0].(map[string]any)["packageName"] != "com.example.app" ||
		limits["browserKeyRestrictions"].(map[string]any)["allowedReferrers"].([]any)[0] != "https://example.com/*" ||
		limits["serverKeyRestrictions"].(map[string]any)["allowedIps"].([]any)[0] != "203.0.113.7" ||
		limits["iosKeyRestrictions"].(map[string]any)["allowedBundleIds"].([]any)[0] != "com.example.ios" {
		t.Errorf("first row = %v", first)
	}
	out2, err := e.invoke(apiKeysListID, `{"project_id":"example-project-123","show_deleted":true,"page_size":1,"cursor":"`+cursor+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	rows2, page2 := listOf(t, out2, "keys")
	if len(rows2) != 1 || rows2[0]["deleteTime"] != "2026-03-04T05:06:07Z" || page2["has_more"] != false {
		t.Errorf("second page = %s", out2)
	}
	for _, got := range []string{out, out2} {
		if strings.Contains(got, fakeKeyValue) || strings.Contains(got, "keyString") {
			t.Errorf("result holds the key value: %s", got)
		}
	}
	api := e.fake.api()
	q0, _ := url.ParseQuery(strings.SplitN(api[0].url, "?", 2)[1])
	q1, _ := url.ParseQuery(strings.SplitN(api[1].url, "?", 2)[1])
	if len(api) != 2 || q0.Get("showDeleted") != "true" || q0.Get("pageSize") != "1" || q1.Get("pageToken") != "p2" ||
		!strings.HasPrefix(api[0].url, "https://apikeys.googleapis.com"+apiKeysPath+"?") || api[0].method != http.MethodGet {
		t.Errorf("requests = %+v", api)
	}
	if strings.Contains(api[0].url, "getKeyString") || strings.Contains(api[1].url, "getKeyString") {
		t.Error("a request asked for the key string")
	}
}

func TestAPIKeysListRefusesACursorOfAnotherListAndInvalidInputBeforeIO(t *testing.T) {
	e := newEnvironment(t, []string{apiKeyTarget})
	e.fake.routes[apiKeysPath] = okReply(`{"keys":[],"nextPageToken":"p2"}`)
	out, err := e.invoke(apiKeysListID, `{"project_id":"example-project-123"}`)
	if err != nil {
		t.Fatal(err)
	}
	_, page := listOf(t, out, "keys")
	cursor := page["next_cursor"].(string)
	before := len(e.fake.api())
	if _, err := e.invoke(apiKeysListID, `{"project_id":"example-project-123","show_deleted":true,"cursor":"`+cursor+`"}`); err == nil {
		t.Error("a cursor of another filter was accepted")
	}
	if _, err := e.invoke(apiKeysListID, `{"project_id":"example-project-123","cursor":"forged"}`); err == nil {
		t.Error("a forged cursor was accepted")
	}
	if len(e.fake.api()) != before {
		t.Error("a request was sent for a refused cursor")
	}
	for _, args := range []string{`{}`, `{"project_id":"projects/example-project-123"}`, `{"project_id":"EXAMPLE"}`,
		`{"project_id":"example-project-123","page_size":0}`, `{"project_id":"example-project-123","page_size":101}`,
		`{"project_id":"example-project-123","show_deleted":"yes"}`, `{"project_id":"example-project-123","extra":1}`,
		`{"project_id":"example-other-9"}`} {
		e := newEnvironment(t, []string{apiKeyTarget})
		if _, err := e.invoke(apiKeysListID, args); err == nil {
			t.Errorf("%s was accepted", args)
		}
		noIO(t, e, args)
	}
}

func TestAPIKeysListRefusesRowsOfAnotherProjectOrWrongShape(t *testing.T) {
	for _, row := range []string{
		`{"name":"projects/415104041262/locations/global/keys/../other"}`,
		`{"name":"projects/415104041262/locations/europe/keys/key-one"}`,
		`{"name":"projects/other-project-9/locations/global/keys/key-one"}`,
		`{"name":"keys/key-one"}`, `{}`,
	} {
		e := newEnvironment(t, []string{apiKeyTarget})
		e.fake.routes[apiKeysPath] = okReply(`{"keys":[` + apiKeyRow("key-one", "") + `,` + row + `]}`)
		_, err := e.invoke(apiKeysListID, `{"project_id":"example-project-123"}`)
		if classOf(err) != provider.ClassInvalidResponse {
			t.Errorf("row %s: err = %v", row, err)
		}
	}
	// A project number in the argument is compared with the number of the row.
	e := newEnvironment(t, []string{"projects/415104041262"})
	e.fake.routes["/v2/projects/415104041262/locations/global/keys"] = okReply(`{"keys":[` +
		`{"name":"projects/999/locations/global/keys/key-one"}]}`)
	if _, err := e.invoke(apiKeysListID, `{"project_id":"415104041262"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("err = %v", err)
	}
}

func TestAPIKeysListBoundsTheAnswer(t *testing.T) {
	e := newEnvironment(t, []string{apiKeyTarget})
	var rows []string
	for i := 0; i < 2*maxPageSize+1; i++ {
		rows = append(rows, `{"name":"projects/1/locations/global/keys/key-one"}`)
	}
	e.fake.routes[apiKeysPath] = okReply(`{"keys":[` + strings.Join(rows, ",") + `]}`)
	if _, err := e.invoke(apiKeysListID, `{"project_id":"example-project-123"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("err = %v", err)
	}
	e = newEnvironment(t, []string{apiKeyTarget})
	long := strings.Repeat("x", 5000)
	e.fake.routes[apiKeyURL] = okReply(`{"name":"projects/1/locations/global/keys/` + apiKeyID + `","displayName":"` + long + `",` +
		`"annotations":{"` + long + `":"` + long + `"},"restrictions":{"browserKeyRestrictions":{"allowedReferrers":["` + long + `"]}}}`)
	out, err := e.invoke(apiKeysGetID, `{"project_id":"example-project-123","key_id":"`+apiKeyID+`"}`)
	if err != nil || len(out) > 4000 {
		t.Errorf("len = %d, err = %v", len(out), err)
	}
}

func TestAPIKeysGetReadsOneKeyWithoutItsValue(t *testing.T) {
	e := newEnvironment(t, []string{apiKeyTarget})
	e.fake.routes[apiKeyURL] = okReply(apiKeyRow(apiKeyID, apiKeyRestrictions))
	out, err := e.invoke(apiKeysGetID, `{"project_id":"example-project-123","key_id":"`+apiKeyID+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	got := decode(t, out)
	if got["name"] != "projects/415104041262/locations/global/keys/"+apiKeyID || got["displayName"] != "Maps key" ||
		got["etag"] != "synthetic-etag" || got["restrictions"] == nil {
		t.Errorf("key = %s", out)
	}
	if strings.Contains(out, fakeKeyValue) || strings.Contains(out, "keyString") {
		t.Errorf("result holds the key value: %s", out)
	}
	api := e.fake.api()
	if len(api) != 1 || api[0].method != http.MethodGet || api[0].url != "https://apikeys.googleapis.com"+apiKeyURL {
		t.Errorf("requests = %+v", api)
	}
	if !strings.HasPrefix(api[0].auth, "Bearer ") || strings.Contains(api[0].url, "?") {
		t.Errorf("request = %+v", api[0])
	}
}

func TestAPIKeysGetRefusesAnotherKeyAndInvalidInputBeforeIO(t *testing.T) {
	e := newEnvironment(t, []string{apiKeyTarget})
	e.fake.routes[apiKeyURL] = okReply(apiKeyRow("key-other", ""))
	if _, err := e.invoke(apiKeysGetID, `{"project_id":"example-project-123","key_id":"`+apiKeyID+`"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("err = %v", err)
	}
	for _, args := range []string{`{"project_id":"example-project-123"}`, `{"key_id":"` + apiKeyID + `"}`,
		`{"project_id":"example-project-123","key_id":"../keys"}`, `{"project_id":"example-project-123","key_id":"KEY"}`,
		`{"project_id":"example-project-123","key_id":"a/b"}`, `{"project_id":"example-project-123","key_id":"-key"}`,
		`{"project_id":"example-project-123","key_id":"key-"}`, `{"project_id":"example-project-123","key_id":"` + strings.Repeat("a", 64) + `"}`,
		`{"project_id":"example-project-123","key_id":"key?x=1"}`, `{"project_id":"example-project-123","key_id":"key%2F"}`,
		`{"project_id":"example-other-9","key_id":"` + apiKeyID + `"}`,
		`{"project_id":"projects/example-project-123","key_id":"` + apiKeyID + `"}`} {
		e := newEnvironment(t, []string{apiKeyTarget})
		if _, err := e.invoke(apiKeysGetID, args); err == nil {
			t.Errorf("%s was accepted", args)
		}
		noIO(t, e, args)
	}
}

func TestAPIKeysGetOfAnUnknownKeyIsNotFound(t *testing.T) {
	e := newEnvironment(t, []string{apiKeyTarget})
	_, err := e.invoke(apiKeysGetID, `{"project_id":"example-project-123","key_id":"`+apiKeyID+`"}`)
	if classOf(err) != provider.ClassNotFound {
		t.Errorf("err = %v", err)
	}
}

func TestAPIKeyErrorsDoNotCarryTheKeyValue(t *testing.T) {
	e := newEnvironment(t, []string{apiKeyTarget})
	e.fake.routes[apiKeyURL] = reply{status: 403, body: `{"error":{"code":403,"message":"denied ` + detailCanary +
		`","status":"PERMISSION_DENIED","keyString":"` + fakeKeyValue + `","details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo",` +
		`"reason":"X","metadata":{"keyString":"` + fakeKeyValue + `"}}]}}`}
	_, err := e.invoke(apiKeysGetID, `{"project_id":"example-project-123","key_id":"`+apiKeyID+`"}`)
	if err == nil || strings.Contains(err.Error(), fakeKeyValue) {
		t.Errorf("err = %v", err)
	}
	var failed *provider.Error
	if errors.As(err, &failed) {
		if original, _ := json.Marshal(failed.Original); strings.Contains(string(original), fakeKeyValue) {
			t.Errorf("original = %s", original)
		}
	}
}

func TestAPIKeysDeleteSendsOneDeleteWithAndWithoutEtag(t *testing.T) {
	const running = `{"name":"operations/op-1","metadata":{"@type":"type.googleapis.com/google.api.apikeys.v2.OperationMetadata"}}`
	e := revokeEnvironment(t, []string{apiKeyTarget}, nil)
	e.fake.routes[apiKeyURL] = okReply(running)
	out, err := e.invokeConfirmed(apiKeysDeleteID, `{"project_id":"example-project-123","key_id":"`+apiKeyID+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	if got := decode(t, out); got["name"] != "operations/op-1" || got["done"] != false {
		t.Errorf("result = %s", out)
	}
	e2 := revokeEnvironment(t, []string{apiKeyTarget}, nil)
	e2.fake.routes[apiKeyURL] = okReply(`{"name":"operations/op-2","done":true,"response":{"@type":"type.googleapis.com/google.protobuf.Empty"}}`)
	out, err = e2.invokeConfirmed(apiKeysDeleteID, `{"project_id":"example-project-123","key_id":"`+apiKeyID+
		`","etag":"W/\"synthetic\"","check_existing_usage":"CHECK"}`)
	if err != nil || decode(t, out)["done"] != true {
		t.Fatalf("result = %s, %v", out, err)
	}
	for i, env := range []*environment{e, e2} {
		api := env.fake.api()
		if len(api) != 1 || api[0].method != http.MethodDelete || api[0].body != "" ||
			!strings.HasPrefix(api[0].url, "https://apikeys.googleapis.com"+apiKeyURL) {
			t.Fatalf("case %d: requests = %+v", i, api)
		}
	}
	if strings.Contains(e.fake.api()[0].url, "?") {
		t.Errorf("url = %s", e.fake.api()[0].url)
	}
	q, _ := url.ParseQuery(strings.SplitN(e2.fake.api()[0].url, "?", 2)[1])
	if q.Get("etag") != `W/"synthetic"` || q.Get("checkExistingUsage") != "CHECK" {
		t.Errorf("query = %v", q)
	}
}

func TestAPIKeysDeleteWithAStaleEtagIsAProviderError(t *testing.T) {
	e := revokeEnvironment(t, []string{apiKeyTarget}, nil)
	e.fake.routes[apiKeyURL] = reply{status: 400, body: errorObject(400, "FAILED_PRECONDITION", "")}
	_, err := e.invokeConfirmed(apiKeysDeleteID, `{"project_id":"example-project-123","key_id":"`+apiKeyID+`","etag":"stale"}`)
	if classOf(err) != provider.ClassProviderError || strings.Contains(err.Error(), detailCanary) ||
		strings.Contains(err.Error(), "outcome") || len(e.fake.api()) != 1 {
		t.Errorf("err = %v, requests = %d", err, len(e.fake.api()))
	}
}

func TestAPIKeysUndeleteSendsOneEmptyPostAndReportsAnExpiredRestore(t *testing.T) {
	e := revokeEnvironment(t, []string{apiKeyTarget}, nil)
	e.fake.routes[apiKeyURL+":undelete"] = okReply(`{"name":"operations/op-3"}`)
	out, err := e.invokeConfirmed(apiKeysUndeleteID, `{"project_id":"example-project-123","key_id":"`+apiKeyID+`"}`)
	if err != nil || decode(t, out)["name"] != "operations/op-3" {
		t.Fatalf("result = %s, %v", out, err)
	}
	api := e.fake.api()
	if len(api) != 1 || api[0].method != http.MethodPost || api[0].body != "{}" ||
		api[0].url != "https://apikeys.googleapis.com"+apiKeyURL+":undelete" {
		t.Errorf("requests = %+v", api)
	}
	// Outside the retention period Google refuses; the error is Google's, not a guess of Qatlas.
	e = revokeEnvironment(t, []string{apiKeyTarget}, nil)
	e.fake.routes[apiKeyURL+":undelete"] = reply{status: 400, body: errorObject(400, "FAILED_PRECONDITION", "")}
	_, err = e.invokeConfirmed(apiKeysUndeleteID, `{"project_id":"example-project-123","key_id":"`+apiKeyID+`"}`)
	if classOf(err) != provider.ClassProviderError || len(e.fake.api()) != 1 {
		t.Errorf("err = %v", err)
	}
	e = revokeEnvironment(t, []string{apiKeyTarget}, nil)
	_, err = e.invokeConfirmed(apiKeysUndeleteID, `{"project_id":"example-project-123","key_id":"`+apiKeyID+`"}`)
	if classOf(err) != provider.ClassNotFound {
		t.Errorf("err = %v", err)
	}
}

func TestAPIKeyChangesNeverPassTheKeyValueOfAnOperation(t *testing.T) {
	// A done operation of an undelete carries the restored key as its response.
	body := `{"name":"operations/op-4","done":true,"metadata":{"keyString":"` + fakeKeyValue + `"},` +
		`"response":{"@type":"type.googleapis.com/google.api.apikeys.v2.Key","name":"projects/1/locations/global/keys/k",` +
		`"keyString":"` + fakeKeyValue + `","nested":[{"KeyString":"` + fakeKeyValue + `","keep":"yes"}],"displayName":"Maps key"}}`
	for _, tool := range []string{apiKeysDeleteID, apiKeysUndeleteID} {
		e := revokeEnvironment(t, []string{apiKeyTarget}, nil)
		path := apiKeyURL
		if tool == apiKeysUndeleteID {
			path += ":undelete"
		}
		e.fake.routes[path] = okReply(body)
		out, err := e.invokeConfirmed(tool, `{"project_id":"example-project-123","key_id":"`+apiKeyID+`"}`)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(strings.ToLower(out), "keystring") || strings.Contains(out, fakeKeyValue) {
			t.Errorf("%s result holds the key value: %s", tool, out)
		}
		if !strings.Contains(out, `"displayName":"Maps key"`) || !strings.Contains(out, `"keep":"yes"`) {
			t.Errorf("%s result lost other members: %s", tool, out)
		}
	}
}

func TestOperationsGetReadsTheOperationOfAnAPIKeyChange(t *testing.T) {
	read := func(t *testing.T, targets []string, answer reply, service string) (*environment, string, error) {
		e := newEnvironment(t, targets)
		e.fake.routes["/v2/operations/op-1"] = answer
		out, err := e.invoke(operationsGetID, `{"service":"`+service+`","name":"operations/op-1"}`)
		return e, out, err
	}
	t.Run("running", func(t *testing.T) {
		e, out, err := read(t, nil, okReply(`{"name":"operations/op-1","metadata":{"@type":"x.Meta","keyString":"`+fakeKeyValue+`"}}`), "api-keys")
		if err != nil {
			t.Fatal(err)
		}
		got := decode(t, out)
		if got["service"] != "api-keys" || got["done"] != false || got["response"] != nil ||
			strings.Contains(out, fakeKeyValue) {
			t.Errorf("result = %s", out)
		}
		if api := e.fake.api(); len(api) != 1 || api[0].url != "https://apikeys.googleapis.com/v2/operations/op-1" || api[0].method != http.MethodGet {
			t.Errorf("requests = %+v", api)
		}
	})
	t.Run("done without the key value", func(t *testing.T) {
		_, out, err := read(t, nil, okReply(`{"name":"operations/op-1","done":true,"response":{"@type":"x.Key","keyString":"`+
			fakeKeyValue+`","name":"projects/1/locations/global/keys/k"}}`), "api-keys")
		if err != nil || strings.Contains(out, fakeKeyValue) || !strings.Contains(out, `"done":true`) ||
			!strings.Contains(out, "projects/1/locations/global/keys/k") {
			t.Errorf("result = %s, %v", out, err)
		}
	})
	t.Run("failed", func(t *testing.T) {
		_, _, err := read(t, nil, okReply(`{"name":"operations/op-1","done":true,"error":{"code":7,"message":"`+detailCanary+`"}}`), "api-keys")
		if classOf(err) != provider.ClassPermission || strings.Contains(err.Error(), detailCanary) {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("a project target cannot prove an operation", func(t *testing.T) {
		e, _, err := read(t, []string{apiKeyTarget}, okReply(`{"name":"operations/op-1"}`), "api-keys")
		if err == nil {
			t.Error("a connection with a project target read an operation")
		}
		noIO(t, e, "operations.get")
	})
	t.Run("the other service keeps its path", func(t *testing.T) {
		e := newEnvironment(t, nil)
		e.fake.routes["/v3/operations/op-1"] = okReply(`{"name":"operations/op-1"}`)
		if _, err := e.invoke(operationsGetID, `{"service":"resource-manager","name":"operations/op-1"}`); err != nil {
			t.Fatal(err)
		}
		if api := e.fake.api(); len(api) != 1 || !strings.HasPrefix(api[0].url, "https://cloudresourcemanager.googleapis.com/v3/") {
			t.Errorf("requests = %+v", api)
		}
	})
}

func TestAPIKeyChangesRefuseBadInputBeforeIO(t *testing.T) {
	for id, bad := range map[string][]string{
		apiKeysDeleteID: {`{"project_id":"example-project-123"}`, `{"project_id":"example-project-123","key_id":"../x"}`,
			`{"project_id":"example-project-123","key_id":"` + apiKeyID + `","etag":""}`,
			`{"project_id":"example-project-123","key_id":"` + apiKeyID + `","etag":"a\nb"}`,
			`{"project_id":"example-project-123","key_id":"` + apiKeyID + `","etag":"` + strings.Repeat("a", 257) + `"}`,
			`{"project_id":"example-project-123","key_id":"` + apiKeyID + `","check_existing_usage":"CHECK_EXISTING_USAGE_UNSPECIFIED"}`,
			`{"project_id":"example-project-123","key_id":"` + apiKeyID + `","keyString":"x"}`,
			`{"project_id":"example-other-9","key_id":"` + apiKeyID + `"}`},
		apiKeysUndeleteID: {`{"project_id":"example-project-123"}`, `{"project_id":"example-project-123","key_id":"a b"}`,
			`{"project_id":"example-other-9","key_id":"` + apiKeyID + `"}`,
			`{"project_id":"example-project-123","key_id":"` + apiKeyID + `","etag":"x"}`},
	} {
		for _, args := range bad {
			e := revokeEnvironment(t, []string{apiKeyTarget}, nil)
			if _, err := e.invokeConfirmed(id, args); err == nil {
				t.Errorf("%s accepted %s", id, args)
			}
			noIO(t, e, id+" "+args)
		}
	}
}

func TestAPIKeyChangesNeedConfirmationAndAProfile(t *testing.T) {
	args := `{"project_id":"example-project-123","key_id":"` + apiKeyID + `"}`
	for id, effect := range map[string]string{apiKeysDeleteID: "delete", apiKeysUndeleteID: "update"} {
		e := revokeEnvironment(t, []string{apiKeyTarget}, nil)
		if _, err := e.invoke(id, args); err == nil {
			t.Errorf("%s ran without confirmation", id)
		}
		noIO(t, e, id)
		d, _, _ := e.reg.Lookup(id)
		if d.Risk.Confirmation != "required" || string(d.Risk.Effect) != effect || !d.Risk.OpenWorld || d.RequiresToolAllowList {
			t.Errorf("%s risk = %+v", id, d.Risk)
		}
		summary := e.reg.ConfirmationSummary(id)
		if summary == nil {
			t.Fatalf("%s has no confirmation summary", id)
		}
		text := summary(json.RawMessage(args))
		for _, part := range []string{"example-project-123", apiKeyID} {
			if !strings.Contains(text, part) {
				t.Errorf("%s summary %q lacks %q", id, text, part)
			}
		}
		if id == apiKeysDeleteID && !strings.Contains(text, "fail afterwards") {
			t.Errorf("delete summary %q does not warn", text)
		}
	}
}

func TestAPIKeyToolsAreInTheRightProfilesWithTheRightScopes(t *testing.T) {
	e := newEnvironment(t, nil)
	metadata, _ := e.reg.ProviderMetadata(Provider)
	profiles := map[string][]string{}
	for _, p := range metadata.Profiles {
		profiles[p.ID] = p.Tools
	}
	for _, id := range []string{apiKeysListID, apiKeysGetID} {
		if !slices.Contains(profiles["read"], id) || !slices.Contains(profiles["read-iam"], id) ||
			!slices.Equal(metadata.Token.Scopes[id], []string{scopeReadOnly}) {
			t.Errorf("%s: read profiles or scopes are wrong: %v", id, metadata.Token.Scopes[id])
		}
	}
	if !slices.Equal(profiles["api-keys-revoke"], []string{apiKeysDeleteID, apiKeysUndeleteID}) {
		t.Errorf("api-keys-revoke = %v", profiles["api-keys-revoke"])
	}
	for _, id := range []string{apiKeysDeleteID, apiKeysUndeleteID} {
		for name, tools := range profiles {
			if name != "api-keys-revoke" && slices.Contains(tools, id) {
				t.Errorf("%s is in profile %s", id, name)
			}
		}
		if !slices.Equal(metadata.Token.Scopes[id], []string{scopeAPIKeys}) {
			t.Errorf("%s scopes = %v", id, metadata.Token.Scopes[id])
		}
	}
	// The value of a key and the creation of one are not tools.
	for _, tool := range metadata.Tools {
		if strings.Contains(tool.ID, "api-keys") && !slices.Contains([]string{apiKeysListID, apiKeysGetID, apiKeysDeleteID,
			apiKeysUndeleteID}, tool.ID) {
			t.Errorf("unexpected API key tool %s", tool.ID)
		}
	}
}

func TestStripKeyStringRemovesTheMemberAtAnyDepth(t *testing.T) {
	var value any
	if err := json.Unmarshal([]byte(`{"keyString":"a","x":{"KEYSTRING":"b","y":[{"keyString":"c","z":1}]},"w":"keyString"}`), &value); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(stripKeyString(value))
	if string(data) != `{"w":"keyString","x":{"y":[{"z":1}]}}` {
		t.Errorf("stripped = %s", data)
	}
}
