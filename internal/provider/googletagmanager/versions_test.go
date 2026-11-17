package googletagmanager

import (
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const testVersionPath = testContainer + "/versions/7"

func TestVersionToolsAreReadOnlyInTheReadProfile(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	profiles := map[string][]string{}
	for _, p := range metadata.Profiles {
		profiles[p.ID] = p.Tools
	}
	for _, id := range []string{versionsGetID, versionsLiveID, versionHeadersListID, versionHeadersLatestID} {
		d := toolOf(t, id)
		if got := metadata.Token.Scopes[id]; !slices.Equal(got, []string{readonlyScope}) {
			t.Errorf("%s scopes = %v", id, got)
		}
		if d.Risk.Effect != capability.EffectRead || d.Risk.Idempotency != capability.IdempotencySafe ||
			d.Risk.Confirmation != capability.ConfirmationNone || d.RequiresToolAllowList {
			t.Errorf("%s risk = %+v", id, d.Risk)
		}
		if !slices.Contains(profiles["read"], id) || !slices.Contains(profiles["write"], id) {
			t.Errorf("%s is not in the read and write profiles", id)
		}
		if !strings.HasPrefix(id, "google.tag-manager.") {
			t.Errorf("%s has the wrong prefix", id)
		}
	}
}

const fullVersion = `{
 "path":"` + testVersionPath + `","accountId":"1000001","containerId":"2000002","containerVersionId":"7",
 "name":"Release","description":"notes","fingerprint":"99","deleted":true,"tagManagerUrl":"https://example.test/v",
 "authorizationCode":"synthetic-preview-code","unknownMember":"x",
 "container":{"path":"` + testContainer + `","containerId":"2000002","name":"Site"},
 "tag":[{"tagId":"1","name":"html tag","type":"html","parameter":[{"type":"template","key":"html","value":"<script>synthetic()</script>"}]}],
 "trigger":[{"triggerId":"2","name":"all pages","type":"pageview"}],
 "variable":[{"variableId":"3","name":"v","type":"c"}],
 "builtInVariable":[{"type":"pageUrl","name":"Page URL"}],
 "folder":[{"folderId":"4","name":"f"}],
 "customTemplate":[{"templateId":"5","name":"tpl","templateData":"___INFO___"}],
 "client":[{"clientId":"6","name":"cl","type":"gaaw_client"}],
 "transformation":[{"transformationId":"8","name":"tr","type":"tf"}],
 "zone":[{"zoneId":"9","name":"z"}],
 "gtagConfig":[{"gtagConfigId":"10","type":"googtag"}]}`

func TestVersionsGetShowsEveryObjectTypeAndNoAuthorizationCode(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, fullVersion, nil }
	out, err := h.call(t, handleVersionsGet, `{"version":"`+testVersionPath+`","containerVersionId":"published"}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"tag", "trigger", "variable", "builtInVariable", "folder", "customTemplate", "client",
		"transformation", "zone", "gtagConfig"} {
		if list, _ := out[name].([]any); len(list) != 1 {
			t.Errorf("%s = %v", name, out[name])
		}
	}
	if out["name"] != "Release" || out["description"] != "notes" || out["fingerprint"] != "99" ||
		out["deleted"] != true || out["tagManagerUrl"] == nil || out["container"].(map[string]any)["name"] != "Site" {
		t.Errorf("header members = %v", out)
	}
	if _, found := out["authorizationCode"]; found || strings.Contains(mustString(out), "synthetic-preview-code") {
		t.Error("the authorization code was shown")
	}
	if _, found := out["unknownMember"]; found || out["truncated"] != nil || out["truncation"] != nil {
		t.Errorf("out = %v", out)
	}
	tag := out["tag"].([]any)[0].(map[string]any)
	if tag["parameter"].([]any)[0].(map[string]any)["value"] != "<script>synthetic()</script>" {
		t.Errorf("tag = %v", tag)
	}
	got := h.sent(t)
	if len(got) != 1 || got[0].method != "GET" || got[0].path != "/tagmanager/v2/"+testVersionPath ||
		got[0].query != "containerVersionId=published" {
		t.Errorf("requests = %+v", got)
	}
}

func mustString(v any) string { return string(mustEncode(v)) }

func TestAnOversizedVersionIsCutAndSaysSo(t *testing.T) {
	longText := strings.Repeat("ä", maxVersionTextRunes+500)
	var tags []string
	for i := 0; i < 400; i++ {
		tags = append(tags, `{"tagId":"`+strconv.Itoa(i+1)+`","name":"t","type":"html","parameter":[{"type":"template",`+
			`"key":"html","value":"`+strings.Repeat("a", maxVersionTextRunes+10000)+`"}]}`)
	}
	body := `{"path":"` + testVersionPath + `","name":"` + strings.Repeat("n", 100) + `","description":"` + longText +
		`","tag":[` + strings.Join(tags, ",") + `],"trigger":[{"triggerId":"2","name":"keep"}]}`
	h := newHarness(t, "", testClientID)
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, body, nil }
	out, err := h.call(t, handleVersionsGet, `{"version":"`+testVersionPath+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	if out["truncated"] != true {
		t.Fatalf("truncated = %v", out["truncated"])
	}
	truncation := out["truncation"].(map[string]any)
	if !strings.Contains(truncation["notice"].(string), "Partial output") || truncation["textsCut"].(float64) < 1 {
		t.Errorf("truncation = %v", truncation)
	}
	var tagCut map[string]any
	for _, entry := range truncation["listsCut"].([]any) {
		if entry.(map[string]any)["list"] == "tag" {
			tagCut = entry.(map[string]any)
		}
	}
	shown := len(out["tag"].([]any))
	if tagCut == nil || tagCut["total"] != float64(400) || tagCut["kept"] != float64(shown) || shown >= 400 || shown == 0 {
		t.Errorf("tag cut = %v, shown %d", tagCut, shown)
	}
	if got := len([]rune(out["description"].(string))); got > maxVersionTextRunes {
		t.Errorf("description has %d characters", got)
	}
	if len(mustString(out)) > maxVersionBytes+10000 {
		t.Errorf("output has %d bytes", len(mustString(out)))
	}
	if len(out["trigger"].([]any)) != 1 {
		t.Errorf("the small list was shortened: %v", out["trigger"])
	}
}

func TestLongTextsAndListsOfOneEntityAreBoundedWithoutAnError(t *testing.T) {
	items := make([]string, maxVersionListItems+5)
	for i := range items {
		items[i] = `{"type":"template","key":"k","value":"x"}`
	}
	body := `{"tag":[{"tagId":"1","name":"t","parameter":[` + strings.Join(items, ",") + `]}],"trigger":[{"name":"` +
		strings.Repeat("b", 5<<20) + `"}]}`
	h := newHarness(t, "", testClientID)
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, body, nil }
	out, err := h.call(t, handleVersionsGet, `{"version":"`+testVersionPath+`"}`)
	if err != nil {
		t.Fatalf("a 5 MiB text is an error: %v", err)
	}
	parameters := out["tag"].([]any)[0].(map[string]any)["parameter"].([]any)
	name := out["trigger"].([]any)[0].(map[string]any)["name"].(string)
	if len(parameters) != maxVersionListItems || len([]rune(name)) != maxVersionTextRunes || out["truncated"] != true {
		t.Errorf("parameters %d, name %d, truncated %v", len(parameters), len([]rune(name)), out["truncated"])
	}
	paths := map[string]bool{}
	for _, entry := range out["truncation"].(map[string]any)["listsCut"].([]any) {
		paths[entry.(map[string]any)["list"].(string)] = true
	}
	if !paths["tag.parameter"] {
		t.Errorf("listsCut = %v", out["truncation"])
	}
}

func TestAVersionAboveTheReadLimitIsAnError(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"name":"` + strings.Repeat("n", maxVersionResponse) + `"}`, nil
	}
	if _, err := h.call(t, handleVersionsGet, `{"version":"`+testVersionPath+`"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("err = %v", err)
	}
}

func TestVersionsLiveReadsTheLiveVersionAndHandlesNone(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, fullVersion, nil }
	out, err := h.call(t, handleVersionsLive, `{"container":"`+testContainer+`"}`)
	if err != nil || out["containerVersionId"] != "7" {
		t.Fatalf("out = %v, %v", out, err)
	}
	if got := h.sent(t); got[0].path != "/tagmanager/v2/"+testContainer+"/versions:live" || got[0].query != "" {
		t.Errorf("requests = %+v", got)
	}

	empty := newHarness(t, "", testClientID)
	empty.google.api = func(*http.Request) (int, string, http.Header) { return 200, `{}`, nil }
	out, err = empty.call(t, handleVersionsLive, `{"container":"`+testContainer+`"}`)
	if err != nil || len(out) != 0 {
		t.Errorf("an empty answer gave %v, %v", out, err)
	}

	missing := newHarness(t, "", testClientID)
	missing.google.api = func(*http.Request) (int, string, http.Header) {
		return 404, `{"error":{"code":404,"message":"synthetic-provider-echo"}}`, nil
	}
	_, err = missing.call(t, handleVersionsLive, `{"container":"`+testContainer+`"}`)
	if classOf(err) != provider.ClassInvalidResponse || strings.Contains(err.Error(), testEcho) {
		t.Errorf("err = %v", err)
	}
}

func TestVersionHeadersListPagesOverThreePagesWithIncludeDeleted(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		switch req.URL.Query().Get("pageToken") {
		case "":
			return 200, `{"containerVersionHeader":[{"containerVersionId":"1","name":"a","numTags":"3","numMacros":"0"}],"nextPageToken":"two"}`, nil
		case "two":
			return 200, `{"containerVersionHeader":[{"containerVersionId":"2","deleted":true,"numTriggers":4}],"nextPageToken":"three"}`, nil
		case "three":
			return 200, `{"containerVersionHeader":[{"containerVersionId":"3","numCustomTemplates":"1","numClients":"2",` +
				`"numGtagConfigs":"3","numTransformations":"4","numZones":"5","numVariables":"6","numRules":"7"}]}`, nil
		}
		return 400, `{}`, nil
	}
	args := `{"container":"` + testContainer + `","includeDeleted":true`
	var ids []string
	cursor := ""
	for page := 0; page < 3; page++ {
		call := args + `}`
		if cursor != "" {
			call = args + `,"cursor":"` + cursor + `"}`
		}
		out, err := h.call(t, handleVersionHeadersList, call)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range out["versionHeaders"].([]any) {
			ids = append(ids, entry.(map[string]any)["containerVersionId"].(string))
		}
		cursor, _ = out["next_cursor"].(string)
		if (cursor == "") != (page == 2) {
			t.Fatalf("page %d: cursor %q", page, cursor)
		}
	}
	if !slices.Equal(ids, []string{"1", "2", "3"}) {
		t.Errorf("ids = %v", ids)
	}
	got := h.sent(t)
	if got[0].path != "/tagmanager/v2/"+testContainer+"/version_headers" || got[0].query != "includeDeleted=true" ||
		got[1].query != "includeDeleted=true&pageToken=two" || got[2].query != "includeDeleted=true&pageToken=three" {
		t.Errorf("requests = %+v", got)
	}
}

func TestVersionHeaderCountsAreShownAsGiven(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"containerVersionHeader":[{"containerVersionId":"2","deleted":true,"numTags":"3","numTriggers":4,` +
			`"numMacros":{"x":1}}]}`, nil
	}
	out, err := h.call(t, handleVersionHeadersList, `{"container":"`+testContainer+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	entry := out["versionHeaders"].([]any)[0].(map[string]any)
	if entry["numTags"] != "3" || entry["numTriggers"] != "4" || entry["deleted"] != true || entry["numMacros"] != nil {
		t.Errorf("entry = %v", entry)
	}
	if got := h.sent(t); got[0].query != "" {
		t.Errorf("includeDeleted was sent by default: %+v", got)
	}
}

func TestVersionHeaderCursorsAreBoundToTheirArgumentsAndNeverTrustedWhenAltered(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"containerVersionHeader":[{"containerVersionId":"1"}],"nextPageToken":"two"}`, nil
	}
	first, err := h.call(t, handleVersionHeadersList, `{"container":"`+testContainer+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	cursor := first["next_cursor"].(string)
	before := len(h.google.seen())
	for _, args := range []string{
		`{"container":"` + testContainer + `","includeDeleted":true,"cursor":"` + cursor + `"}`,
		`{"container":"accounts/1000001/containers/2000009","cursor":"` + cursor + `"}`,
		`{"container":"` + testContainer + `","cursor":"` + cursor[:len(cursor)-2] + `xx"}`,
		`{"container":"` + testContainer + `","cursor":"bogus"}`,
	} {
		if _, err := h.call(t, handleVersionHeadersList, args); !isInvalidRequest(err) {
			t.Errorf("%s: err = %v", args, err)
		}
	}
	if len(h.google.seen()) != before {
		t.Error("a refused cursor reached Google")
	}
}

func TestVersionHeadersLatestReadsOneHeader(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"path":"` + testVersionPath + `","containerVersionId":"7","name":"n","numTags":"2"}`, nil
	}
	out, err := h.call(t, handleVersionHeadersLatest, `{"container":"`+testContainer+`"}`)
	if err != nil || out["containerVersionId"] != "7" || out["numTags"] != "2" {
		t.Fatalf("out = %v, %v", out, err)
	}
	if got := h.sent(t); got[0].path != "/tagmanager/v2/"+testContainer+"/version_headers:latest" {
		t.Errorf("requests = %+v", got)
	}
}

func TestVersionToolsOutsideTheBindingAreRefusedBeforeAnyRequest(t *testing.T) {
	cases := []struct{ target, container, version string }{
		{"accounts/1000001/containers/2000002", "accounts/1000001/containers/2000009",
			"accounts/1000001/containers/2000009/versions/7"},
		{"accounts/1000001/containers/2000002", "accounts/1000009/containers/2000002",
			"accounts/1000009/containers/2000002/versions/7"},
		{"accounts/1000001", "accounts/1000009/containers/2000002", "accounts/1000009/containers/2000002/versions/7"},
	}
	for _, c := range cases {
		h := newHarness(t, c.target, testClientID)
		reachNothing(t, h, handleVersionsGet, `{"version":"`+c.version+`"}`)
		reachNothing(t, h, handleVersionsLive, `{"container":"`+c.container+`"}`)
		reachNothing(t, h, handleVersionHeadersList, `{"container":"`+c.container+`"}`)
		reachNothing(t, h, handleVersionHeadersLatest, `{"container":"`+c.container+`"}`)
	}
	h := newHarness(t, "accounts/1000001/containers/2000002", testClientID)
	if _, err := h.call(t, handleVersionsGet, `{"version":"`+testVersionPath+`"}`); err != nil {
		t.Errorf("a version inside the target is refused: %v", err)
	}
	if _, err := h.call(t, handleVersionsLive, `{}`); err != nil {
		t.Errorf("the container of the target is not the default: %v", err)
	}
	for _, bad := range []string{testContainer + "/versions/07", testContainer + "/versions/", testContainer + "/versions/7/x",
		testContainer + "/versions/7?a=b", testContainer + "/workspaces/7", testContainer, "versions/7",
		testContainer + "/versions/-1"} {
		reachNothing(t, newHarness(t, "", testClientID), handleVersionsGet, `{"version":"`+bad+`"}`)
	}
	for _, bad := range []string{"07", "latest", "7/../8", "7?x=1", strings.Repeat("9", 21)} {
		reachNothing(t, newHarness(t, "", testClientID), handleVersionsGet,
			`{"version":"`+testVersionPath+`","containerVersionId":"`+bad+`"}`)
	}
	reachNothing(t, newHarness(t, "", testClientID), handleVersionsLive, `{}`)
}

func TestVersionZeroIsAValidPath(t *testing.T) {
	h := newHarness(t, "", testClientID)
	if _, err := h.call(t, handleVersionsGet, `{"version":"`+testContainer+`/versions/0"}`); err != nil {
		t.Fatal(err)
	}
}

func TestVersionToolsMapTheErrorFormsOfTheProvider(t *testing.T) {
	handlers := map[string]capability.Handler{`{"version":"` + testVersionPath + `"}`: handleVersionsGet,
		`{"container":"` + testContainer + `"}`: handleVersionHeadersList}
	for _, c := range []struct {
		status int
		class  provider.Class
	}{{401, provider.ClassAuth}, {403, provider.ClassPermission}, {429, provider.ClassRateLimited},
		{500, provider.ClassInvalidResponse}} {
		for args, handler := range handlers {
			h := newHarness(t, "", testClientID)
			h.google.api = func(*http.Request) (int, string, http.Header) {
				return c.status, `{"error":{"message":"` + testEcho + `"}}`, nil
			}
			_, err := h.call(t, handler, args)
			if classOf(err) != c.class || strings.Contains(err.Error(), testEcho) {
				t.Errorf("status %d: err = %v", c.status, err)
			}
		}
	}
	notJSON := newHarness(t, "", testClientID)
	notJSON.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `<html></html>`, http.Header{"Content-Type": {"text/html"}}
	}
	if _, err := notJSON.call(t, handleVersionsLive, `{"container":"`+testContainer+`"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("err = %v", err)
	}
}

func TestVersionOutputIsOneReusableForm(t *testing.T) {
	var v containerVersion
	if err := json.Unmarshal([]byte(fullVersion), &v); err != nil {
		t.Fatal(err)
	}
	out := versionOutput(v)
	if out.Truncated || out.Truncation != nil || len(out.Tag) != 1 || len(out.CustomTemplate) != 1 {
		t.Errorf("out = %+v", out)
	}
}
