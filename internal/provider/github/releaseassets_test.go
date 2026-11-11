package github

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// assetFake answers the release asset and release notes routes of the bound repository. override, when set,
// answers the next change or notes request instead; asset reads are never overridden.
type assetFake struct {
	f        *fakeGitHub
	assets   map[int64]string
	override func(http.ResponseWriter)
	notes    string
}

func (a *assetFake) route(w http.ResponseWriter, r *http.Request) bool {
	rest, ok := strings.CutPrefix(r.URL.Path, actionsPrefix)
	if !ok {
		return false
	}
	w.Header().Set("Content-Type", "application/json")
	if rest == "releases/generate-notes" && r.Method == http.MethodPost {
		if a.override != nil {
			answer := a.override
			a.override = nil
			answer(w)
			return true
		}
		fmt.Fprint(w, a.notes)
		return true
	}
	digits, ok := strings.CutPrefix(rest, "releases/assets/")
	if !ok {
		return false
	}
	id, _ := strconv.ParseInt(digits, 10, 64)
	name, held := a.assets[id]
	if r.Method != http.MethodGet && a.override != nil {
		answer := a.override
		a.override = nil
		answer(w)
		return true
	}
	if !held {
		w.WriteHeader(http.StatusNotFound)
		return true
	}
	switch r.Method {
	case http.MethodPatch:
		body := a.f.recorded()[len(a.f.recorded())-1].body
		if v, ok := body["name"].(string); ok {
			name = v
			a.assets[id] = v
		}
	case http.MethodDelete:
		delete(a.assets, id)
		w.WriteHeader(http.StatusNoContent)
		return true
	}
	fmt.Fprintf(w, `{"id":%d,"node_id":"RA_1","name":%q,"label":"","state":"uploaded","size":10,`+
		`"content_type":"application/zip","download_count":3,"created_at":"2026-01-01T00:00:00Z",`+
		`"updated_at":"2026-01-02T00:00:00Z","url":"https://api.github.com/repos/octo-org/example/releases/assets/%d",`+
		`"browser_download_url":"https://github.com/octo-org/example/releases/download/v1/%s"}`, id, name, id, name)
	return true
}

// changes counts the requests that are neither reads of an asset nor notes requests.
func (a *assetFake) changes() int {
	count := 0
	for _, request := range a.f.recorded() {
		if request.method == http.MethodPatch || request.method == http.MethodDelete {
			count++
		}
	}
	return count
}

func serveAssets(t *testing.T) (*assetFake, *application.Core, *int) {
	t.Helper()
	fake := &assetFake{assets: map[int64]string{7: "old.zip"}}
	f := &fakeGitHub{failure: fake.route}
	fake.f = f
	base := serve(t, f)
	reads := 0
	red := &redact.Redactor{}
	cfg := coreChangeConfig(base)
	cfg.Connections["deleter"] = config.Connection{Service: "gh", Credential: "gh-reader", Target: repoTarget,
		Effects: []config.Effect{config.EffectRead, config.EffectUpdate, config.EffectDelete},
		Tools:   []string{releaseAssetsUpdate.ID, releaseAssetsDelete.ID, releaseNotesGenerate.ID}}
	cfg.Connections["excluding"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Targets: []string{"repos/octo-org/*"}, Exclude: []string{"repos/octo-org/secret"},
		Effects: []config.Effect{config.EffectRead, config.EffectUpdate, config.EffectDelete},
		Tools:   []string{releaseAssetsUpdate.ID, releaseAssetsDelete.ID, releaseNotesGenerate.ID}}
	return fake, application.New(registry(t), cfg, resolver(red, &reads), red), &reads
}

func TestReleaseNotesAreGeneratedAsOneReadingPost(t *testing.T) {
	fake, core, _ := serveAssets(t)
	fake.notes = `{"name":"v1.2.0","body":"## What's Changed\n* a fix\u0007 by @octocat"}`
	result, err := invoke(t, core, "github.release-notes.generate", "repo",
		`{"tag":"v1.2.0","target":"main","previous_tag":"v1.1.0","configuration_file_path":".github/notes.yml"}`, false)
	if err != nil {
		t.Fatal(err)
	}
	var got ReleaseNotes
	if err := json.Unmarshal(result, &got); err != nil || got.Name != "v1.2.0" ||
		!strings.Contains(got.Body, "a fix  by @octocat") || got.BodyTruncated {
		t.Fatalf("notes = %s, %v", result, err)
	}
	requests := fake.f.recorded()
	if len(requests) != 1 || requests[0].method != http.MethodPost {
		t.Fatalf("requests = %+v, want exactly one POST", requests)
	}
	want := map[string]any{"tag_name": "v1.2.0", "target_commitish": "main", "previous_tag_name": "v1.1.0",
		"configuration_file_path": ".github/notes.yml"}
	if len(requests[0].body) != len(want) {
		t.Errorf("body = %v, want %v", requests[0].body, want)
	}
	for key, value := range want {
		if requests[0].body[key] != value {
			t.Errorf("body[%s] = %v, want %v", key, requests[0].body[key], value)
		}
	}

	// A long answer is cut hard.
	fake.notes = fmt.Sprintf(`{"name":"x","body":%q}`, strings.Repeat("a", projectionBodyLimit+50))
	result, err = invoke(t, core, "github.release-notes.generate", "repo", `{"tag":"v1.2.0"}`, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(result, &got); err != nil || !got.BodyTruncated || len(got.Body) != projectionBodyLimit {
		t.Errorf("long notes = truncated %v, %d bytes, %v", got.BodyTruncated, len(got.Body), err)
	}
}

func TestReleaseNotesRefuseBadArgumentsAndMapFailures(t *testing.T) {
	fake, core, _ := serveAssets(t)
	for name, arguments := range map[string]string{
		"a missing tag":       `{}`,
		"a bad tag":           `{"tag":"a..b"}`,
		"a bad previous tag":  `{"tag":"v1","previous_tag":"-x"}`,
		"a bad target":        `{"tag":"v1","target":"a b"}`,
		"a parent path":       `{"tag":"v1","configuration_file_path":"../x.yml"}`,
		"an embedded parent":  `{"tag":"v1","configuration_file_path":"a/../x.yml"}`,
		"an absolute path":    `{"tag":"v1","configuration_file_path":"/etc/x.yml"}`,
		"a control character": `{"tag":"v1","configuration_file_path":"a\u0000b.yml"}`,
		"a newline":           `{"tag":"v1","configuration_file_path":"a\nb.yml"}`,
		"an empty path":       `{"tag":"v1","configuration_file_path":""}`,
	} {
		t.Run(name, func(t *testing.T) {
			before := len(fake.f.recorded())
			if _, err := invoke(t, core, "github.release-notes.generate", "repo", arguments, false); err == nil {
				t.Error("err = nil, want a refusal")
			}
			if len(fake.f.recorded()) != before {
				t.Error("a refused request reached GitHub")
			}
		})
	}

	const marker = "marker-text-from-github"
	for name, tt := range map[string]struct {
		answer func(http.ResponseWriter)
		class  provider.Class
	}{
		"forbidden": {func(w http.ResponseWriter) {
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"message":"Resource not accessible `+marker+`"}`)
		}, provider.ClassPermission},
		"not found": {func(w http.ResponseWriter) { w.WriteHeader(http.StatusNotFound) }, provider.ClassNotFound},
		"unprocessable": {func(w http.ResponseWriter) {
			w.WriteHeader(http.StatusUnprocessableEntity)
			fmt.Fprint(w, `{"message":"`+marker+`"}`)
		}, provider.ClassProviderError},
		"timeout": {func(w http.ResponseWriter) { w.WriteHeader(http.StatusGatewayTimeout) }, provider.ClassTimeout},
	} {
		t.Run(name, func(t *testing.T) {
			fake.override = tt.answer
			before := len(fake.f.recorded())
			_, err := invoke(t, core, "github.release-notes.generate", "repo", `{"tag":"v9"}`, false)
			if classOf(err) != tt.class {
				t.Fatalf("err = %v, want class %s", err, tt.class)
			}
			if strings.Contains(err.Error(), marker) || strings.Contains(err.Error(), "may have been applied") {
				t.Errorf("err = %v, want no GitHub text and no claim of a change", err)
			}
			if got := len(fake.f.recorded()) - before; got != 1 {
				t.Errorf("requests = %d, want exactly one", got)
			}
		})
	}
}

func TestReleaseAssetUpdateRenamesAfterReadingTheAsset(t *testing.T) {
	fake, core, _ := serveAssets(t)
	result, err := invoke(t, core, "github.release-assets.update", "repo",
		`{"asset_id":7,"name":"new.zip","label":"Linux build","state":"uploaded"}`, true)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		ID    int64             `json:"id"`
		Name  string            `json:"name"`
		Links map[string]string `json:"links"`
	}
	if err := json.Unmarshal(result, &got); err != nil || got.ID != 7 || got.Name != "new.zip" {
		t.Fatalf("asset = %s, %v", result, err)
	}
	if _, has := got.Links["browser_download_url"]; has || got.Links["url"] == "" ||
		strings.Contains(string(result), "browser_download_url") || strings.Contains(string(result), "releases/download") {
		t.Errorf("links = %v, want the REST url and no download address", got.Links)
	}
	requests := fake.f.recorded()
	if len(requests) != 2 || requests[0].method != http.MethodGet || requests[1].method != http.MethodPatch ||
		requests[0].path != requests[1].path {
		t.Fatalf("requests = %+v, want one read then one PATCH of the same asset", requests)
	}
	if body := requests[1].body; len(body) != 3 || body["name"] != "new.zip" || body["label"] != "Linux build" ||
		body["state"] != "uploaded" {
		t.Errorf("body = %v", body)
	}

	// An empty label clears it and is sent as given.
	if _, err := invoke(t, core, "github.release-assets.update", "repo", `{"asset_id":7,"label":""}`, true); err != nil {
		t.Fatal(err)
	}
	if body := fake.f.recorded()[len(fake.f.recorded())-1].body; len(body) != 1 || body["label"] != "" {
		t.Errorf("body = %v, want only the empty label", body)
	}
}

func TestReleaseAssetChangesRefuseBadArgumentsBeforeGitHub(t *testing.T) {
	fake, core, _ := serveAssets(t)
	long := strings.Repeat("a", 256)
	for name, tt := range map[string]struct{ operation, arguments string }{
		"no change":            {"update", `{"asset_id":7}`},
		"a zero id":            {"update", `{"asset_id":0,"name":"a"}`},
		"a path in the name":   {"update", `{"asset_id":7,"name":"dir/a.zip"}`},
		"a backslash":          {"update", `{"asset_id":7,"name":"dir\\a.zip"}`},
		"a parent name":        {"update", `{"asset_id":7,"name":".."}`},
		"a dot name":           {"update", `{"asset_id":7,"name":"."}`},
		"a blank name":         {"update", `{"asset_id":7,"name":"  "}`},
		"a control name":       {"update", `{"asset_id":7,"name":"a\nb"}`},
		"a long name":          {"update", `{"asset_id":7,"name":"` + long + `"}`},
		"a long label":         {"update", `{"asset_id":7,"label":"` + long + `x"}`},
		"a control label":      {"update", `{"asset_id":7,"label":"a\u0007"}`},
		"another state":        {"update", `{"asset_id":7,"state":"open"}`},
		"a delete without id":  {"delete", `{}`},
		"a delete with a zero": {"delete", `{"asset_id":0}`},
	} {
		t.Run(name, func(t *testing.T) {
			before := len(fake.f.recorded())
			_, err := invoke(t, core, "github.release-assets."+tt.operation, "deleter", tt.arguments, true)
			if err == nil {
				t.Error("err = nil, want a refusal")
			}
			if len(fake.f.recorded()) != before {
				t.Error("a refused request reached GitHub")
			}
		})
	}
}

// An identifier GitHub does not find below the chosen repository is refused after the read, and nothing is
// changed.
func TestReleaseAssetChangesRefuseAForeignAssetIdentifier(t *testing.T) {
	fake, core, _ := serveAssets(t)
	for operation, arguments := range map[string]string{
		"update": `{"asset_id":99,"name":"x.zip"}`,
		"delete": `{"asset_id":99}`,
	} {
		before := len(fake.f.recorded())
		_, err := invoke(t, core, "github.release-assets."+operation, "deleter", arguments, true)
		if classOf(err) != provider.ClassNotFound || !strings.Contains(err.Error(), "release asset 99") {
			t.Errorf("%s = %v, want not found naming the asset", operation, err)
		}
		if got := len(fake.f.recorded()) - before; got != 1 || fake.changes() != 0 {
			t.Errorf("%s: requests = %d, changes = %d, want the read only", operation, got, fake.changes())
		}
	}
}

func TestReleaseAssetChangesMapGitHubRefusalsAndNeverRepeat(t *testing.T) {
	fake, core, _ := serveAssets(t)
	const marker = "marker-text-from-github"
	status := func(code int, body string) func(http.ResponseWriter) {
		return func(w http.ResponseWriter) {
			w.WriteHeader(code)
			fmt.Fprint(w, body)
		}
	}
	for name, tt := range map[string]struct {
		answer   func(http.ResponseWriter)
		class    provider.Class
		contains string
	}{
		"forbidden":        {status(403, `{"message":"x `+marker+`"}`), provider.ClassPermission, "Contents: read and write"},
		"immutable by 422": {status(422, `{"message":"Cannot modify assets of an immutable release `+marker+`"}`), provider.ClassPermission, "immutable"},
		"immutable by 403": {status(403, `{"message":"Release is immutable `+marker+`"}`), provider.ClassPermission, "immutable"},
		"unprocessable":    {status(422, `{"message":"`+marker+`"}`), provider.ClassProviderError, "rejected the request"},
		"not found":        {status(404, ``), provider.ClassNotFound, "release asset 7"},
		"server error":     {status(502, ``), provider.ClassProviderError, "may have been applied"},
		"gateway timeout":  {status(504, ``), provider.ClassTimeout, "may have been applied"},
		"dropped connection": {func(w http.ResponseWriter) {
			connection, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				connection.Close()
			}
		}, "", "may have been applied"},
	} {
		for operation, arguments := range map[string]string{
			"update": `{"asset_id":7,"name":"n.zip"}`,
			"delete": `{"asset_id":7}`,
		} {
			t.Run(name+" "+operation, func(t *testing.T) {
				fake.override = tt.answer
				before := fake.changes()
				_, err := invoke(t, core, "github.release-assets."+operation, "deleter", arguments, true)
				if err == nil || (tt.class != "" && classOf(err) != tt.class) {
					t.Fatalf("err = %v (%s), want class %q", err, classOf(err), tt.class)
				}
				if strings.Contains(err.Error(), marker) {
					t.Errorf("err = %v, want none of GitHub's text", err)
				}
				if !strings.Contains(err.Error(), tt.contains) {
					t.Errorf("err = %v, want it to contain %q", err, tt.contains)
				}
				if got := fake.changes() - before; got != 1 {
					t.Errorf("changes = %d, want exactly one, never a retry", got)
				}
			})
		}
	}
}

func TestReleaseAssetDeleteRemovesTheAsset(t *testing.T) {
	fake, core, _ := serveAssets(t)
	result, err := invoke(t, core, "github.release-assets.delete", "deleter", `{"asset_id":7}`, true)
	if err != nil {
		t.Fatal(err)
	}
	var got DeletedReleaseAsset
	if err := json.Unmarshal(result, &got); err != nil || got.ID != 7 || !got.Deleted {
		t.Fatalf("deleted = %s, %v", result, err)
	}
	if _, held := fake.assets[7]; held {
		t.Error("the asset is still held")
	}
	requests := fake.f.recorded()
	if len(requests) != 2 || requests[0].method != http.MethodGet || requests[1].method != http.MethodDelete {
		t.Errorf("requests = %+v, want one read then one DELETE", requests)
	}
}

// A repository outside the targets or inside an exclusion is refused before a secret is read or GitHub is
// contacted, and the delete is offered only where a connection's tools list names it.
func TestReleaseAssetToolsAreBoundToTheRepositoryAndDeleteIsListed(t *testing.T) {
	fake, core, reads := serveAssets(t)
	for name, tt := range map[string]struct{ operation, connection, arguments string }{
		"a foreign repository":        {"github.release-assets.update", "deleter", `{"asset_id":7,"name":"a","repository":"octo-org/other"}`},
		"a foreign repository delete": {"github.release-assets.delete", "deleter", `{"asset_id":7,"repository":"octo-org/other"}`},
		"a foreign repository notes":  {"github.release-notes.generate", "deleter", `{"tag":"v1","repository":"octo-org/other"}`},
		"an excluded update":          {"github.release-assets.update", "excluding", `{"asset_id":7,"name":"a","repository":"octo-org/secret"}`},
		"an excluded delete":          {"github.release-assets.delete", "excluding", `{"asset_id":7,"repository":"octo-org/secret"}`},
		"an excluded notes":           {"github.release-notes.generate", "excluding", `{"tag":"v1","repository":"octo-org/secret"}`},
	} {
		t.Run(name, func(t *testing.T) {
			*reads = 0
			before := len(fake.f.recorded())
			_, err := invoke(t, core, tt.operation, tt.connection, tt.arguments, true)
			if !isInvalidRequest(err) {
				t.Errorf("err = %v, want an invalid request", err)
			}
			if *reads != 0 || len(fake.f.recorded()) != before {
				t.Errorf("secret reads = %d, requests = %d; want none", *reads, len(fake.f.recorded())-before)
			}
		})
	}

	before := len(fake.f.recorded())
	_, err := invoke(t, core, "github.release-assets.delete", "repo", `{"asset_id":7}`, true)
	if _, ok := err.(*capability.UnsupportedError); !ok {
		t.Errorf("delete without a tools entry = %T %v, want unsupported", err, err)
	}
	if len(fake.f.recorded()) != before {
		t.Error("an unsupported delete reached GitHub")
	}
	if _, err := invoke(t, core, "github.release-assets.update", "deleter", `{"asset_id":7,"name":"a"}`, false); err == nil {
		t.Error("an unconfirmed update succeeded")
	}
}

// The delete is in no profile; the notes and the update are only in the not-recommended releases profile.
func TestReleaseAssetToolsAndProfiles(t *testing.T) {
	metadata, _ := registry(t).ProviderMetadata(Provider)
	for _, profile := range metadata.Profiles {
		for _, id := range profile.Tools {
			if id == releaseAssetsDelete.ID {
				t.Errorf("profile %s selects the listed-only delete", profile.ID)
			}
			if (id == releaseAssetsUpdate.ID || id == releaseNotesGenerate.ID) && (profile.ID != "releases" || profile.Recommended) {
				t.Errorf("profile %s selects %s", profile.ID, id)
			}
		}
	}
	for _, descriptor := range registry(t).Provider(Provider) {
		if descriptor.ID == releaseNotesGenerate.ID && (descriptor.Risk.Effect != capability.EffectRead ||
			descriptor.Risk.Confirmation != capability.ConfirmationNone || !strings.Contains(descriptor.Description, "POST")) {
			t.Errorf("notes descriptor = %+v", descriptor)
		}
	}
}
