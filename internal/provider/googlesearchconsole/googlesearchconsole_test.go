package googlesearchconsole

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/provider/googleapi"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/ratelimit"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// No test reaches Google: a fake transport answers the token endpoints and the API. The values below are
// synthetic stand-ins for a credential and for text a failing Google echoes.
const (
	clientIDValue = "sample-client-id"
	refreshValue  = "synthetic-refresh-canary"
	accessPrefix  = "synthetic-access-canary-"
	detailCanary  = "provider-detail-canary-5e2a"
	clientIDEnv   = "TEST_SEARCH_CONSOLE_CLIENT_ID"
	refreshEnv    = "TEST_SEARCH_CONSOLE_REFRESH"
	keyEnv        = "TEST_SEARCH_CONSOLE_KEY"
	tokenEndpoint = "https://oauth2.googleapis.com/token"
	domainSite    = "sc-domain:example.com"
	prefixSite    = "https://www.example.com/"
	sitemapURL    = "https://www.example.com/sitemap.xml"
)

var robot = "robot" + "@" + "sample.invalid"

type request struct {
	method, host, escapedPath, rawQuery, authorization, accept, full, contentType, body string
}

// fakeGoogle is the transport of every request. The token endpoint issues accessPrefix1, 2, ... unless
// tokenAnswer says otherwise; the API answers with api, by default an empty object.
type fakeGoogle struct {
	mu          sync.Mutex
	api         []request
	grants      []url.Values
	impersonate []string
	tokenAnswer func(n int) (int, string)
	answer      func(r *http.Request, n int) (*http.Response, error)
}

func (f *fakeGoogle) RoundTrip(r *http.Request) (*http.Response, error) {
	body := ""
	if r.Body != nil {
		raw, _ := io.ReadAll(r.Body)
		body = string(raw)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case r.URL.String() == tokenEndpoint:
		form, _ := url.ParseQuery(body)
		f.grants = append(f.grants, form)
		n := len(f.grants)
		if f.tokenAnswer != nil {
			status, text := f.tokenAnswer(n)
			return respond(r, status, "application/json", text), nil
		}
		return respond(r, http.StatusOK, "application/json", fmt.Sprintf(
			`{"access_token":"%s%d","token_type":"Bearer","expires_in":3600}`, accessPrefix, n)), nil
	case r.URL.Host == "iamcredentials.googleapis.com":
		f.impersonate = append(f.impersonate, r.URL.EscapedPath())
		return respond(r, http.StatusOK, "application/json", fmt.Sprintf(
			`{"accessToken":"%simpersonated","expireTime":"%s"}`, accessPrefix,
			time.Now().Add(time.Hour).UTC().Format(time.RFC3339))), nil
	case r.URL.Host == "searchconsole.googleapis.com":
		f.api = append(f.api, request{method: r.Method, host: r.URL.Host, escapedPath: r.URL.EscapedPath(),
			rawQuery: r.URL.RawQuery, authorization: r.Header.Get("Authorization"), accept: r.Header.Get("Accept"),
			full: r.URL.String(), contentType: r.Header.Get("Content-Type"), body: body})
		if f.answer != nil {
			return f.answer(r, len(f.api))
		}
		return respond(r, http.StatusOK, "application/json", `{}`), nil
	}
	return nil, errors.New("unexpected host")
}

func (f *fakeGoogle) calls() ([]request, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]request(nil), f.api...), len(f.grants)
}

func respond(r *http.Request, status int, contentType, body string) *http.Response {
	header := http.Header{}
	if contentType != "" {
		header.Set("Content-Type", contentType)
	}
	return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body)), Request: r}
}

func reply(status int, body string) func(*http.Request, int) (*http.Response, error) {
	return func(r *http.Request, _ int) (*http.Response, error) {
		return respond(r, status, "application/json; charset=UTF-8", body), nil
	}
}

// errorObject is the Google error form with a message and details that echo the access token and a canary.
func errorObject(code int, status, reason string) string {
	return fmt.Sprintf(`{"error":{"code":%d,"message":"%s %s1","status":"%s","details":[{"@type":`+
		`"type.googleapis.com/google.rpc.ErrorInfo","reason":"%s","domain":"googleapis.com",`+
		`"metadata":{"token":"%s1"}}]}}`, code, detailCanary, accessPrefix, status, reason, accessPrefix)
}

type environment struct {
	fake   *fakeGoogle
	cfg    *config.Config
	core   *application.Core
	red    *redact.Redactor
	values map[string]string
}

// newEnvironment installs the fake transport, a fresh token cache and a rate limit without waits, and
// builds a validated configuration with one credential and connections unbound and bound to each form.
func newEnvironment(t *testing.T, values map[string]string) *environment {
	t.Helper()
	fake := &fakeGoogle{}
	previousTransport, previousCache, previousLimiters, previousQuery := transport, tokenCache, limiters, queryLimiters
	transport, tokenCache, limiters = fake, provider.NewTokenCache(time.Now), ratelimit.NewRegistry(0)
	queryLimiters = ratelimit.NewRegistry(0)
	previousInspection, previousDays := inspectionLimiters, inspectionDays
	inspectionLimiters = ratelimit.NewRegistry(0)
	inspectionDays = ratelimit.NewCounterRegistry(dailyInspections, ratelimit.Daily(pacific()))
	t.Cleanup(func() {
		inspectionLimiters, inspectionDays = previousInspection, previousDays
		transport, tokenCache, limiters, queryLimiters = previousTransport, previousCache, previousLimiters, previousQuery
	})

	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatalf("Register() = %v", err)
	}
	if err := reg.ValidateProfiles(); err != nil {
		t.Fatalf("ValidateProfiles() = %v", err)
	}
	if values == nil {
		values = map[string]string{clientIDEnv: clientIDValue, refreshEnv: refreshValue}
	}
	roles := map[string]string{}
	for role, env := range map[string]string{config.RoleClientID: clientIDEnv, config.RoleRefreshToken: refreshEnv,
		config.RoleServiceAccountKey: keyEnv} {
		if values[env] != "" {
			roles[role] = env
		}
	}
	cfg := config.New(reg)
	cfg.Version = 4
	cfg.Services = map[string]config.Service{"console": {Provider: Provider}}
	cfg.Credentials = map[string]config.Credential{"google": {Provider: Provider, Type: config.CredentialTypeEnv, Values: roles}}
	read := []config.Effect{config.EffectRead}
	edit := []config.Effect{config.EffectRead, config.EffectCreate, config.EffectDelete}
	// sites.delete is listed-only, so the connections that change entries name every tool.
	changing := []string{sitesListID, sitesGetID, sitemapsListID, sitemapsGetID, searchAnalyticsID, sitesAddID,
		sitesDeleteID, sitemapsSubmitID, sitemapsDeleteID}
	cfg.Connections = map[string]config.Connection{
		"all":    {Service: "console", Credential: "google", Effects: read},
		"domain": {Service: "console", Credential: "google", Effects: read, Target: domainSite},
		"prefix": {Service: "console", Credential: "google", Effects: read, Target: prefixSite},
		// The connections that may change entries; read-only ones above never offer a changing tool.
		"edit":       {Service: "console", Credential: "google", Tools: changing},
		"editdomain": {Service: "console", Credential: "google", Tools: changing, Target: domainSite},
		"editprefix": {Service: "console", Credential: "google", Tools: changing, Target: prefixSite},
		// The effects alone reach every changing tool except the listed-only removal of a property.
		"effects": {Service: "console", Credential: "google", Effects: edit},
	}
	if values[clientIDEnv] != "" && values[refreshEnv] != "" {
		cfg.Connections["robot"] = config.Connection{Service: "console", Credential: "google", Effects: read,
			ServiceAccount: robot}
	}
	// A credential from the environment must be complete, so one without a consent only resolves unvalidated,
	// as a stored credential would.
	if values[clientIDEnv] == "" || values[refreshEnv] != "" {
		if err := cfg.Validate(); err != nil {
			t.Fatalf("Validate() = %v", err)
		}
	}
	red := &redact.Redactor{}
	lookup := func(name string) string { return values[name] }
	return &environment{fake: fake, cfg: cfg, red: red, values: values,
		core: application.New(reg, cfg, secret.NewWith(lookup, nil, nil, red), red)}
}

func (e *environment) invoke(t *testing.T, operation, connection, arguments string) (map[string]any, error) {
	t.Helper()
	response, err := e.core.Invoke(context.Background(), application.InvokeRequest{
		Operation: operation, Connection: connection, Arguments: json.RawMessage(arguments)})
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(response.Result, &out); err != nil {
		t.Fatalf("result %s: %v", response.Result, err)
	}
	return out, nil
}

func (e *environment) test(t *testing.T, connection string) provider.Class {
	t.Helper()
	resolved, err := e.cfg.Resolve(connection, "")
	if err != nil {
		t.Fatal(err)
	}
	lookup := func(name string) string { return e.values[name] }
	ctx := secret.ForConnection(context.Background(), resolved)
	class, err := TestConnection(ctx, resolved, secret.NewWith(lookup, nil, nil, e.red), e.red)
	if err != nil {
		t.Fatalf("TestConnection(%s) error = %v", connection, err)
	}
	return class
}

func classOf(err error) provider.Class {
	var failed *provider.Error
	if errors.As(err, &failed) {
		return failed.Class
	}
	return ""
}

func TestRegisterPublishesMetadataScopesAndConsent(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, ok := reg.ProviderMetadata(Provider)
	if !ok || metadata.DefaultBaseURL != apiRoot || metadata.DocPath() != "google.search-console/setup.md" ||
		!reflect.DeepEqual(metadata.SecretRoles, config.TokenSecretRoles()) || metadata.Token == nil {
		t.Fatalf("metadata = %+v", metadata)
	}
	want := []string{searchAnalyticsID, sitemapsDeleteID, sitemapsGetID, sitemapsListID, sitemapsSubmitID,
		sitesAddID, sitesDeleteID, sitesGetID, sitesListID, urlInspectionID}
	changing := map[string]config.Effect{sitesAddID: config.EffectCreate, sitemapsSubmitID: config.EffectCreate,
		sitesDeleteID: config.EffectDelete, sitemapsDeleteID: config.EffectDelete}
	var tools []string
	for _, tool := range metadata.Tools {
		tools = append(tools, tool.ID)
		wantEffect, wantScope := config.EffectRead, scopeRead
		if effect, ok := changing[tool.ID]; ok {
			wantEffect, wantScope = effect, scopeEdit
		}
		if tool.Effect != wantEffect {
			t.Errorf("%s effect = %s, want %s", tool.ID, tool.Effect, wantEffect)
		}
		if scopes := metadata.Token.Scopes[tool.ID]; !reflect.DeepEqual(scopes, []string{wantScope}) {
			t.Errorf("%s scopes = %v, want %s", tool.ID, scopes, wantScope)
		}
	}
	if !reflect.DeepEqual(tools, want) {
		t.Errorf("tools = %v, want %v", tools, want)
	}
	if len(metadata.Profiles) != 2 || metadata.Profiles[0].ID != "read" || !metadata.Profiles[0].Recommended ||
		len(metadata.Profiles[0].Tools) != 6 || metadata.Profiles[1].ID != "edit" || metadata.Profiles[1].Recommended ||
		len(metadata.Profiles[1].Tools) != 9 {
		t.Errorf("profiles = %+v", metadata.Profiles)
	}
	oauth, ok := reg.OAuth(Provider)
	if !ok || oauth.TokenURL != tokenEndpoint || oauth.AuthorizeURL != googleapi.OAuth.AuthorizeURL ||
		oauth.RevokeURL != googleapi.OAuth.RevokeURL || oauth.Params["access_type"] != "offline" ||
		oauth.ImpersonateURL == "" || oauth.ImpersonateScope == "" {
		t.Errorf("consent endpoints = %+v, %t", oauth, ok)
	}
	if err := metadata.ValidateBaseURL("https://elsewhere.example.invalid"); err == nil {
		t.Error("another base URL was accepted")
	}
	if err := metadata.ValidateBaseURL(apiRoot + "/"); err != nil {
		t.Errorf("the default base URL was refused: %v", err)
	}
}

func TestConfigRefusesMalformedProperty(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	for _, good := range []string{domainSite, prefixSite, "http://example.com/blog/", "sc-domain:xn--bcher-kva.example"} {
		if err := metadata.Target.Validate(good); err != nil {
			t.Errorf("Validate(%q) = %v", good, err)
		}
	}
	for _, bad := range []string{"shop.example.net", "sc-domain:", "sc-domain:localhost", "https://shop.example.net",
		"https://shop.example.net/?a=1", "*", "sc-domain:-bad.example"} {
		err := metadata.Target.Validate(bad)
		if err == nil {
			t.Errorf("Validate(%q) accepted a malformed property", bad)
		} else if strings.Contains(err.Error(), bad) && bad != "*" && bad != "sc-domain:" {
			t.Errorf("Validate(%q) quotes the value: %v", bad, err)
		}
	}
}

func TestSitesListReturnsEveryEntryWithItsLevel(t *testing.T) {
	env := newEnvironment(t, nil)
	env.fake.answer = reply(http.StatusOK, `{"siteEntry":[`+
		`{"siteUrl":"sc-domain:example.com","permissionLevel":"siteOwner"},`+
		`{"siteUrl":"https://www.example.com/","permissionLevel":"siteFullUser"},`+
		`{"siteUrl":"https://shop.example.com/","permissionLevel":"siteRestrictedUser"},`+
		`{"siteUrl":"https://old.example.com/","permissionLevel":"siteUnverifiedUser"},`+
		`{"siteUrl":"https://new.example.com/","permissionLevel":"SITE_PERMISSION_LEVEL_UNSPECIFIED","extra":1}]}`)
	out, err := env.invoke(t, sitesListID, "all", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := out["siteEntry"].([]any)
	if len(entries) != 5 {
		t.Fatalf("siteEntry = %v", out)
	}
	levels := []string{}
	for _, entry := range entries {
		item := entry.(map[string]any)
		if _, ok := item["extra"]; ok {
			t.Errorf("an undocumented member reached the result: %v", item)
		}
		levels = append(levels, item["permissionLevel"].(string))
	}
	if !reflect.DeepEqual(levels, []string{"siteOwner", "siteFullUser", "siteRestrictedUser", "siteUnverifiedUser",
		"SITE_PERMISSION_LEVEL_UNSPECIFIED"}) {
		t.Errorf("levels = %v", levels)
	}
	calls, grants := env.fake.calls()
	if len(calls) != 1 || grants != 1 {
		t.Fatalf("calls = %+v, grants = %d", calls, grants)
	}
	call := calls[0]
	if call.method != http.MethodGet || call.escapedPath != "/webmasters/v3/sites" || call.rawQuery != "" ||
		call.authorization != "Bearer "+accessPrefix+"1" || call.accept != "application/json" {
		t.Errorf("request = %+v", call)
	}
	if strings.Contains(call.full, accessPrefix) {
		t.Errorf("the access token travelled in the URL: %s", call.full)
	}
	if got := env.red.Apply("bearer " + accessPrefix + "1"); strings.Contains(got, accessPrefix) {
		t.Errorf("the access token is not registered with the redactor: %s", got)
	}
}

func TestSitesListEmptyIsAnEmptyList(t *testing.T) {
	env := newEnvironment(t, nil)
	env.fake.answer = reply(http.StatusOK, `{}`)
	out, err := env.invoke(t, sitesListID, "all", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	// The core leaves an empty list out of the result; missing means empty.
	if entries, ok := out["siteEntry"]; ok && len(entries.([]any)) != 0 {
		t.Errorf("result = %v, want no entries", out)
	}
	resolved, err := env.cfg.Resolve("all", "")
	if err != nil {
		t.Fatal(err)
	}
	lookup := func(name string) string { return env.values[name] }
	got, err := handleSitesList(secret.ForConnection(context.Background(), resolved), resolved,
		secret.NewWith(lookup, nil, nil, env.red), env.red, json.RawMessage(`{}`))
	if result, ok := got.(sitesResult); err != nil || !ok || result.SiteEntry == nil || len(result.SiteEntry) != 0 {
		t.Errorf("handler = %#v, %v, want an empty, non-nil list", got, err)
	}
}

func TestLongListsAreCutAndMarked(t *testing.T) {
	env := newEnvironment(t, nil)
	entries := make([]string, maxSites+5)
	for i := range entries {
		entries[i] = fmt.Sprintf(`{"siteUrl":"https://s%d.example.com/","permissionLevel":"siteOwner"}`, i)
	}
	env.fake.answer = reply(http.StatusOK, `{"siteEntry":[`+strings.Join(entries, ",")+`]}`)
	out, err := env.invoke(t, sitesListID, "all", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	if list, _ := out["siteEntry"].([]any); len(list) != maxSites || out["truncated"] != true {
		t.Errorf("%d entries, truncated %v", len(list), out["truncated"])
	}
}

func TestSitesListOfABoundConnectionShowsOnlyItsProperty(t *testing.T) {
	env := newEnvironment(t, nil)
	env.fake.answer = reply(http.StatusOK, `{"siteEntry":[`+
		`{"siteUrl":"sc-domain:example.com","permissionLevel":"siteOwner"},`+
		`{"siteUrl":"https://www.example.com/","permissionLevel":"siteFullUser"}]}`)
	out, err := env.invoke(t, sitesListID, "prefix", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := out["siteEntry"].([]any)
	if len(entries) != 1 || entries[0].(map[string]any)["siteUrl"] != prefixSite {
		t.Errorf("siteEntry = %v, want only the bound property", out)
	}
}

func TestSitesGetEscapesBothPropertyForms(t *testing.T) {
	for _, c := range []struct{ site, path string }{
		{domainSite, "/webmasters/v3/sites/sc-domain%3Aexample.com"},
		{prefixSite, "/webmasters/v3/sites/https%3A%2F%2Fwww.example.com%2F"},
		{"https://www.example.com/a b%/", ""},
	} {
		env := newEnvironment(t, nil)
		env.fake.answer = reply(http.StatusOK, fmt.Sprintf(`{"siteUrl":%q,"permissionLevel":"siteOwner"}`, c.site))
		out, err := env.invoke(t, sitesGetID, "all", fmt.Sprintf(`{"site_url":%q}`, c.site))
		calls, _ := env.fake.calls()
		if c.path == "" {
			if err == nil || len(calls) != 0 {
				t.Errorf("%q: error %v after %d requests, want a refusal before any request", c.site, err, len(calls))
			}
			continue
		}
		if err != nil {
			t.Fatalf("%q: %v", c.site, err)
		}
		if out["siteUrl"] != c.site || out["permissionLevel"] != "siteOwner" {
			t.Errorf("%q: result = %v", c.site, out)
		}
		if len(calls) != 1 || calls[0].escapedPath != c.path {
			t.Errorf("%q: requests = %+v, want path %s", c.site, calls, c.path)
		}
	}
}

func TestMalformedSiteURLIsRefusedBeforeAnyRequest(t *testing.T) {
	env := newEnvironment(t, nil)
	for _, site := range []string{"shop.example.net", "sc-domain:exa_mple.com", "https://shop.example.net",
		"https://user" + "@" + "www.example.com/", "https://www.example.com/?q=1", "https://www.example.com/#top",
		"ftp://www.example.com/", "sc-domain:localhost", "javascript:alert(1)/"} {
		for _, operation := range []string{sitesGetID, sitemapsListID} {
			_, err := env.invoke(t, operation, "all", fmt.Sprintf(`{"site_url":%q}`, site))
			var invalid *provider.InvalidRequestError
			if !errors.As(err, &invalid) || strings.Contains(err.Error(), site) {
				t.Errorf("%s %q: error %v, want an invalid request that does not quote the value", operation, site, err)
			}
		}
	}
	if calls, grants := env.fake.calls(); len(calls) != 0 || grants != 0 {
		t.Errorf("%d requests and %d token grants, want none", len(calls), grants)
	}
	if _, err := env.invoke(t, sitesGetID, "all", `{}`); err == nil {
		t.Error("an unbound connection served sites.get without site_url")
	}
}

func TestBoundConnectionRefusesAnotherProperty(t *testing.T) {
	env := newEnvironment(t, nil)
	env.fake.answer = reply(http.StatusOK, `{"siteUrl":"sc-domain:example.com","permissionLevel":"siteOwner"}`)
	for _, other := range []string{prefixSite, "sc-domain:other.example", "sc-domain:EXAMPLE.com"} {
		for _, operation := range []string{sitesGetID, sitemapsListID} {
			_, err := env.invoke(t, operation, "domain", fmt.Sprintf(`{"site_url":%q}`, other))
			var invalid *provider.InvalidRequestError
			if !errors.As(err, &invalid) || !strings.Contains(err.Error(), "outside") || strings.Contains(err.Error(), other) {
				t.Errorf("%s %q: error %v, want a refusal that names neither property", operation, other, err)
			}
		}
	}
	if calls, grants := env.fake.calls(); len(calls) != 0 || grants != 0 {
		t.Fatalf("%d requests and %d token grants, want none", len(calls), grants)
	}
	if _, err := env.invoke(t, sitesGetID, "domain", `{"site_url":"sc-domain:example.com"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := env.invoke(t, sitesGetID, "domain", `{}`); err != nil {
		t.Fatal(err)
	}
	calls, _ := env.fake.calls()
	if len(calls) != 2 || calls[1].escapedPath != "/webmasters/v3/sites/sc-domain%3Aexample.com" {
		t.Errorf("requests = %+v, want both on the bound property", calls)
	}
}

func TestSitemapsListWithAndWithoutIndex(t *testing.T) {
	env := newEnvironment(t, nil)
	env.fake.answer = reply(http.StatusOK, `{"sitemap":[{"path":"https://www.example.com/sitemap.xml",`+
		`"lastSubmitted":"2026-10-01T08:00:00.000Z","isPending":false,"isSitemapsIndex":true,"type":"sitemap",`+
		`"lastDownloaded":"2026-10-02T08:00:00.000Z","warnings":"0","errors":"0",`+
		`"contents":[{"type":"web","submitted":"120","indexed":"100"}]}]}`)
	out, err := env.invoke(t, sitemapsListID, "all", `{"site_url":"https://www.example.com/"}`)
	if err != nil {
		t.Fatal(err)
	}
	maps, _ := out["sitemap"].([]any)
	if len(maps) != 1 {
		t.Fatalf("result = %v", out)
	}
	entry := maps[0].(map[string]any)
	if entry["isSitemapsIndex"] != true || entry["isPending"] != false || entry["type"] != "sitemap" ||
		entry["lastDownloaded"] != "2026-10-02T08:00:00.000Z" {
		t.Errorf("entry = %v", entry)
	}
	content := entry["contents"].([]any)[0].(map[string]any)
	if content["submitted"] != "120" || content["type"] != "web" {
		t.Errorf("content = %v", content)
	}
	if _, ok := content["indexed"]; ok {
		t.Error("the deprecated indexed count reached the result")
	}

	if _, err := env.invoke(t, sitemapsListID, "all",
		`{"site_url":"sc-domain:example.com","sitemap_index":"https://www.example.com/sitemap_index.xml?part=1&x=a b"}`); err == nil {
		t.Error("a sitemap index with a space was accepted")
	}
	if _, err := env.invoke(t, sitemapsListID, "all",
		`{"site_url":"sc-domain:example.com","sitemap_index":"https://www.example.com/sitemap_index.xml?part=1"}`); err != nil {
		t.Fatal(err)
	}
	calls, _ := env.fake.calls()
	if len(calls) != 2 {
		t.Fatalf("requests = %+v", calls)
	}
	if calls[0].escapedPath != "/webmasters/v3/sites/https%3A%2F%2Fwww.example.com%2F/sitemaps" || calls[0].rawQuery != "" {
		t.Errorf("first request = %+v", calls[0])
	}
	query, _ := url.ParseQuery(calls[1].rawQuery)
	if calls[1].escapedPath != "/webmasters/v3/sites/sc-domain%3Aexample.com/sitemaps" || len(query) != 1 ||
		query.Get("sitemapIndex") != "https://www.example.com/sitemap_index.xml?part=1" {
		t.Errorf("second request = %+v", calls[1])
	}
}

func TestSitemapsGetWithWarningsAndErrors(t *testing.T) {
	env := newEnvironment(t, nil)
	long := strings.Repeat("x", 3000)
	env.fake.answer = reply(http.StatusOK, `{"path":"https://www.example.com/sitemap.xml","isPending":"maybe",`+
		`"type":"`+long+`","warnings":"12","errors":"3","contents":[{"type":"image","submitted":"7"}]}`)
	out, err := env.invoke(t, sitemapsGetID, "prefix", `{"feedpath":"https://www.example.com/sitemap.xml"}`)
	if err != nil {
		t.Fatal(err)
	}
	if out["warnings"] != "12" || out["errors"] != "3" || out["path"] != sitemapURL {
		t.Errorf("result = %v", out)
	}
	if _, ok := out["isPending"]; ok {
		t.Error("a pending flag that is no boolean reached the result")
	}
	if kind, _ := out["type"].(string); len([]rune(kind)) != maxShortRunes+1 {
		t.Errorf("type was not cut: %d characters", len([]rune(kind)))
	}
	calls, _ := env.fake.calls()
	if len(calls) != 1 || calls[0].escapedPath !=
		"/webmasters/v3/sites/https%3A%2F%2Fwww.example.com%2F/sitemaps/https%3A%2F%2Fwww.example.com%2Fsitemap.xml" {
		t.Errorf("requests = %+v", calls)
	}
	for _, bad := range []string{"sitemap.xml", "sc-domain:example.com", "https://user" + "@" + "www.example.com/s.xml"} {
		if _, err := env.invoke(t, sitemapsGetID, "prefix", fmt.Sprintf(`{"feedpath":%q}`, bad)); err == nil {
			t.Errorf("feedpath %q was accepted", bad)
		}
	}
	if calls, _ := env.fake.calls(); len(calls) != 1 {
		t.Errorf("a refused feedpath reached Google: %d requests", len(calls))
	}
}

func TestFailuresMapToClassesWithoutProviderText(t *testing.T) {
	for name, c := range map[string]struct {
		answer   func(*http.Request, int) (*http.Response, error)
		class    provider.Class
		requests int
		grants   int
	}{
		"401 after one renewal": {reply(http.StatusUnauthorized, errorObject(401, "UNAUTHENTICATED", "CREDENTIALS_MISSING")),
			provider.ClassAuth, 2, 2},
		"403": {reply(http.StatusForbidden, errorObject(403, "PERMISSION_DENIED", "SERVICE_DISABLED")),
			provider.ClassPermission, 1, 1},
		"404": {reply(http.StatusNotFound, errorObject(404, "NOT_FOUND", "")), provider.ClassNotFound, 1, 1},
		"429 without Retry-After": {reply(http.StatusTooManyRequests, errorObject(429, "RESOURCE_EXHAUSTED",
			"RATE_LIMIT_EXCEEDED")), provider.ClassRateLimited, 1, 1},
		"400":             {reply(http.StatusBadRequest, errorObject(400, "INVALID_ARGUMENT", "")), provider.ClassInvalidResponse, 1, 1},
		"500":             {reply(http.StatusInternalServerError, errorObject(500, "INTERNAL", "")), provider.ClassProviderError, 1, 1},
		"unreadable body": {reply(http.StatusOK, `{"siteUrl":`+detailCanary), provider.ClassInvalidResponse, 1, 1},
		"not JSON": {func(r *http.Request, _ int) (*http.Response, error) {
			return respond(r, http.StatusOK, "text/html", "<p>"+detailCanary+"</p>"), nil
		}, provider.ClassInvalidResponse, 1, 1},
		"timeout": {func(*http.Request, int) (*http.Response, error) {
			return nil, fmt.Errorf("%s: %w", detailCanary, context.DeadlineExceeded)
		}, provider.ClassTimeout, 1, 1},
	} {
		t.Run(name, func(t *testing.T) {
			env := newEnvironment(t, nil)
			env.fake.answer = c.answer
			_, err := env.invoke(t, sitesGetID, "all", `{"site_url":"sc-domain:example.com"}`)
			if classOf(err) != c.class {
				t.Fatalf("error = %v (class %q), want %q", err, classOf(err), c.class)
			}
			if strings.Contains(err.Error(), detailCanary) || strings.Contains(err.Error(), accessPrefix) {
				t.Errorf("the message carries provider text or a token: %v", err)
			}
			calls, grants := env.fake.calls()
			if len(calls) != c.requests || grants != c.grants {
				t.Errorf("%d requests and %d grants, want %d and %d", len(calls), grants, c.requests, c.grants)
			}
			var failed *provider.Error
			if errors.As(err, &failed) && failed.Original != nil {
				original, _ := json.Marshal(failed.Original)
				if strings.Contains(env.red.Apply(string(original)), accessPrefix) {
					t.Errorf("the original keeps a token after redaction: %s", original)
				}
			}
		})
	}
}

func TestTooManyRequestsHoldsTheNextCall(t *testing.T) {
	env := newEnvironment(t, nil)
	limiters = ratelimit.NewRegistry(time.Millisecond)
	env.fake.answer = reply(http.StatusTooManyRequests, errorObject(429, "RESOURCE_EXHAUSTED", "RATE_LIMIT_EXCEEDED"))
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	request := application.InvokeRequest{Operation: sitesListID, Connection: "all", Arguments: json.RawMessage(`{}`)}
	if _, err := env.core.Invoke(ctx, request); classOf(err) != provider.ClassRateLimited {
		t.Fatalf("first call = %v", err)
	}
	// The hold of at least a second outlasts the deadline, so the second call ends without a request.
	if _, err := env.core.Invoke(ctx, request); classOf(err) != provider.ClassRateLimited {
		t.Fatalf("second call = %v, want rate-limited before sending", err)
	}
	if calls, _ := env.fake.calls(); len(calls) != 1 {
		t.Errorf("%d requests, want one", len(calls))
	}
}

func TestConnectionTest(t *testing.T) {
	owned := `{"siteEntry":[{"siteUrl":"https://www.example.com/","permissionLevel":"siteOwner"}]}`
	for name, c := range map[string]struct {
		connection string
		values     map[string]string
		token      func(int) (int, string)
		answer     func(*http.Request, int) (*http.Response, error)
		class      provider.Class
		requests   int
	}{
		"success": {connection: "all", answer: reply(http.StatusOK, owned), class: provider.ClassOK, requests: 1},
		"bound success": {connection: "prefix", answer: func(r *http.Request, n int) (*http.Response, error) {
			if n == 1 {
				return respond(r, http.StatusOK, "application/json", owned), nil
			}
			return respond(r, http.StatusOK, "application/json", `{"siteUrl":"https://www.example.com/","permissionLevel":"siteFullUser"}`), nil
		}, class: provider.ClassOK, requests: 2},
		"no property":     {connection: "all", answer: reply(http.StatusOK, `{}`), class: provider.ClassNotFound, requests: 1},
		"only unverified": {connection: "all", answer: reply(http.StatusOK, `{"siteEntry":[{"siteUrl":"https://www.example.com/","permissionLevel":"siteUnverifiedUser"}]}`), class: provider.ClassNotFound, requests: 1},
		"bound unverified": {connection: "domain", answer: func(r *http.Request, n int) (*http.Response, error) {
			if n == 1 {
				return respond(r, http.StatusOK, "application/json", owned), nil
			}
			return respond(r, http.StatusOK, "application/json", `{"siteUrl":"sc-domain:example.com","permissionLevel":"siteUnverifiedUser"}`), nil
		}, class: provider.ClassPermission, requests: 2},
		"bound without access": {connection: "domain", answer: func(r *http.Request, n int) (*http.Response, error) {
			if n == 1 {
				return respond(r, http.StatusOK, "application/json", owned), nil
			}
			return respond(r, http.StatusForbidden, "application/json", errorObject(403, "PERMISSION_DENIED", "")), nil
		}, class: provider.ClassPermission, requests: 2},
		"expired consent": {connection: "all", token: func(int) (int, string) {
			return http.StatusBadRequest, `{"error":"invalid_grant","error_description":"` + detailCanary + `"}`
		}, class: provider.ClassConsentExpired},
		"api not enabled": {connection: "all", answer: reply(http.StatusForbidden,
			errorObject(403, "PERMISSION_DENIED", "SERVICE_DISABLED")), class: provider.ClassPermission, requests: 1},
		"no consent yet": {connection: "all", values: map[string]string{clientIDEnv: clientIDValue},
			class: provider.ClassConsentRequired},
		"scope missing": {connection: "all", token: func(n int) (int, string) {
			return http.StatusOK, `{"access_token":"` + accessPrefix + `1","token_type":"Bearer","expires_in":3600,` +
				`"scope":"https://www.googleapis.com/auth/indexing"}`
		}, class: provider.ClassScopeMissing},
	} {
		t.Run(name, func(t *testing.T) {
			env := newEnvironment(t, c.values)
			env.fake.tokenAnswer, env.fake.answer = c.token, c.answer
			if class := env.test(t, c.connection); class != c.class {
				t.Fatalf("class = %q, want %q", class, c.class)
			}
			if calls, _ := env.fake.calls(); len(calls) != c.requests {
				t.Errorf("%d API requests, want %d", len(calls), c.requests)
			}
		})
	}
}

func TestConsentRefreshSendsTheCredential(t *testing.T) {
	env := newEnvironment(t, nil)
	if _, err := env.invoke(t, sitesListID, "all", `{}`); err != nil {
		t.Fatal(err)
	}
	env.fake.mu.Lock()
	form := env.fake.grants[0]
	env.fake.mu.Unlock()
	if form.Get("grant_type") != "refresh_token" || form.Get("refresh_token") != refreshValue ||
		form.Get("client_id") != clientIDValue || form.Has("client_secret") {
		t.Errorf("token request = %v", form)
	}
	// A second call of the same process reuses the cached token.
	if _, err := env.invoke(t, sitesListID, "all", `{}`); err != nil {
		t.Fatal(err)
	}
	if _, grants := env.fake.calls(); grants != 1 {
		t.Errorf("%d grants, want the cached token reused", grants)
	}
}

func TestServiceAccountKeyObtainsTheReadScope(t *testing.T) {
	private, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		t.Fatal(err)
	}
	keyFile, _ := json.Marshal(map[string]string{
		"type": "service_account", "client_email": "robot-sample-account", "private_key_id": "synthetic-kid",
		"private_key": string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})),
		"token_uri":   tokenEndpoint,
	})
	env := newEnvironment(t, map[string]string{keyEnv: string(keyFile)})
	env.fake.answer = reply(http.StatusOK, `{}`)
	if _, err := env.invoke(t, sitesListID, "all", `{}`); err != nil {
		t.Fatal(err)
	}
	env.fake.mu.Lock()
	form := env.fake.grants[0]
	env.fake.mu.Unlock()
	if form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:jwt-bearer" {
		t.Fatalf("token request = %v", form)
	}
	parts := strings.Split(form.Get("assertion"), ".")
	if len(parts) != 3 {
		t.Fatalf("assertion has %d parts", len(parts))
	}
	claims := decodeSegment(t, parts[1])
	if claims["scope"] != scopeRead || claims["aud"] != tokenEndpoint {
		t.Errorf("claims = %v", claims)
	}
	if got := env.red.Apply(form.Get("assertion")); strings.Contains(got, parts[2]) {
		t.Error("the signed assertion is not registered with the redactor")
	}
}

func TestImpersonationAsksForTheReadScope(t *testing.T) {
	env := newEnvironment(t, nil)
	env.fake.answer = reply(http.StatusOK, `{}`)
	if _, err := env.invoke(t, sitesListID, "robot", `{}`); err != nil {
		t.Fatal(err)
	}
	calls, grants := env.fake.calls()
	env.fake.mu.Lock()
	impersonated := append([]string(nil), env.fake.impersonate...)
	consentScope := env.fake.grants[0].Get("scope")
	env.fake.mu.Unlock()
	if grants != 1 || len(impersonated) != 1 || len(calls) != 1 ||
		calls[0].authorization != "Bearer "+accessPrefix+"impersonated" {
		t.Fatalf("grants %d, impersonation %v, requests %+v", grants, impersonated, calls)
	}
	if !strings.Contains(impersonated[0], "/serviceAccounts/robot"+"@"+"sample.invalid:generateAccessToken") {
		t.Errorf("impersonation path = %s", impersonated[0])
	}
	if consentScope != "" {
		t.Errorf("the refresh asked for scope %q although Google does not narrow a refresh", consentScope)
	}
}

func decodeSegment(t *testing.T, segment string) map[string]any {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(segment)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}
