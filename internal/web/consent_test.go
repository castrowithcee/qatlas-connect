package web

import (
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/connlog"
	"github.com/castrowithcee/qatlas-connect/internal/invokelog"
	"github.com/castrowithcee/qatlas-connect/internal/manage"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-connect/internal/vault"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// The provider "sample" exists only in these tests: its credential issues access tokens, and its token and
// revocation endpoints sit behind a fake transport.

const (
	webConsentClientID     = "sample-client-id"
	webConsentClientCanary = "synthetic-client-canary-3d9a"
	webConsentCode         = "synthetic-code-a71e"
	webConsentRefresh      = "synthetic-refresh-52bc"
	webConsentAccess       = "synthetic-access-e04f"
	webConsentEcho         = "echoed-provider-text-6c1d"
)

var webConsentProvider = config.ProviderMetadata{
	ID: "sample", Name: "Sample", SecretRoles: config.TokenSecretRoles(),
	Tools: []config.ToolMetadata{
		{ID: "sample.items.get", Effect: config.EffectRead},
		{ID: "sample.items.update", Effect: config.EffectUpdate},
	},
	Token: &config.TokenMetadata{Scopes: map[string][]string{
		"sample.items.get":    {"items.read"},
		"sample.items.update": {"items.write"},
	}},
}

type webConsentCatalog struct{}

func (webConsentCatalog) ProviderMetadata(id string) (config.ProviderMetadata, bool) {
	return webConsentProvider, id == webConsentProvider.ID
}
func (webConsentCatalog) ProviderMetadataAll() []config.ProviderMetadata {
	return []config.ProviderMetadata{webConsentProvider}
}

// webTokenEndpoints is the fake transport of the token and revocation endpoints.
type webTokenEndpoints struct {
	mu    sync.Mutex
	forms map[string][]url.Values
}

func (e *webTokenEndpoints) RoundTrip(req *http.Request) (*http.Response, error) {
	body, _ := io.ReadAll(req.Body)
	form, _ := url.ParseQuery(string(body))
	e.mu.Lock()
	if e.forms == nil {
		e.forms = map[string][]url.Values{}
	}
	e.forms[req.URL.Path] = append(e.forms[req.URL.Path], form)
	e.mu.Unlock()
	text := `{"access_token":"` + webConsentAccess + `","token_type":"Bearer","expires_in":600,` +
		`"refresh_token":"` + webConsentRefresh + `"}`
	if req.URL.Path == "/revoke" {
		text = ""
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}},
		Body: io.NopCloser(strings.NewReader(text)), Request: req}, nil
}

func (e *webTokenEndpoints) sent(path string) []url.Values {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]url.Values(nil), e.forms[path]...)
}

type webConsentFixture struct {
	s         *Server
	mem       *secret.MemoryStore
	endpoints *webTokenEndpoints
	path      string
	cookie    *http.Cookie
	csrf      string
	// bodies collects every response of the fixture, for the check that none carries a secret.
	bodies []string
}

// newWebConsentFixture saves a configuration with the keyring credential "cred" of the provider sample, used
// by a reading and a writing connection, and couples a browser; with v it proves the passphrase.
func newWebConsentFixture(t *testing.T, v *vault.Vault, adminTimeout time.Duration) *webConsentFixture {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	store := config.NewStore(path, webConsentCatalog{})
	cfg := store.New()
	cfg.Services = map[string]config.Service{"svc": {Provider: "sample", BaseURL: "https://api.sample.invalid"}}
	cfg.Credentials = map[string]config.Credential{"cred": {Provider: "sample", Type: config.CredentialTypeKeyring}}
	cfg.Connections = map[string]config.Connection{
		"reader": {Service: "svc", Credential: "cred", Effects: []config.Effect{config.EffectRead}},
		"writer": {Service: "svc", Credential: "cred", Tools: []string{"sample.items.update"}},
	}
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	f := &webConsentFixture{mem: secret.NewMemoryStore(), endpoints: &webTokenEndpoints{}, path: path}
	for role, value := range map[string]string{
		config.RoleClientID: webConsentClientID, config.RoleClientSecret: webConsentClientCanary,
	} {
		if err := f.mem.Set(secret.StoreKey("cred", role), value); err != nil {
			t.Fatal(err)
		}
	}
	red := &redact.Redactor{}
	resolver := secret.NewWith(func(string) string { return "" }, f.mem, nil, red)
	if v != nil {
		resolver.WithVault(v, nil)
	}
	oauth := provider.OAuth{
		AuthorizeURL: "https://login.sample.invalid/authorize", TokenURL: "https://login.sample.invalid/token",
		RevokeURL: "https://login.sample.invalid/revoke", Transport: f.endpoints,
	}
	svc := manage.New(store, resolver, connlog.SurfaceWeb, red).WithVaultProcessSupport(false).
		WithConsentSource(func(id string) (provider.OAuth, bool) { return oauth, id == "sample" })
	s, err := New(testOverview(), v, svc, resolver, red, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.adminTimeoutSource = func() time.Duration { return adminTimeout }
	t.Cleanup(s.close)
	f.s = s
	f.cookie, f.csrf = coupleAndApprove(t, s, v)
	return f
}

func (f *webConsentFixture) keep(rec *httptest.ResponseRecorder) *httptest.ResponseRecorder {
	var headers strings.Builder
	_ = rec.Header().Write(&headers)
	f.bodies = append(f.bodies, headers.String()+rec.Body.String())
	return rec
}

// start posts the consent start as the coupled session and returns the authorization link it shows.
func (f *webConsentFixture) start(t *testing.T) *url.URL {
	t.Helper()
	rec := f.keep(f.s.postForm(t, "/credentials/cred/consent", f.s.addr, f.cookie, "http://"+f.s.addr, f.csrf, url.Values{}))
	if rec.Code != http.StatusOK {
		t.Fatalf("start status = %d, want 200:\n%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	i := strings.Index(body, `href="https://login.sample.invalid/authorize?`)
	if i < 0 {
		t.Fatalf("the start page shows no authorization link:\n%s", body)
	}
	raw := body[i+len(`href="`):]
	raw = html.UnescapeString(raw[:strings.Index(raw, `"`)])
	link, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return link
}

// back sends the browser back the way the provider would: a GET without cookie, Origin or CSRF value.
func (f *webConsentFixture) back(t *testing.T, method, host, query string) *httptest.ResponseRecorder {
	t.Helper()
	return f.keep(f.s.request(t, method, consentReturnPath+"?"+query, host, nil, nil))
}

func answer(link *url.URL) string {
	return url.Values{"state": {link.Query().Get("state")}, "code": {webConsentCode}}.Encode()
}

func (f *webConsentFixture) storedRefresh(t *testing.T) string {
	t.Helper()
	value, err := f.mem.Get(t.Context(), secret.StoreKey("cred", config.RoleRefreshToken))
	if err != nil {
		return ""
	}
	return value
}

func (f *webConsentFixture) assertNoSecretInResponses(t *testing.T) {
	t.Helper()
	for _, body := range f.bodies {
		for _, value := range []string{webConsentCode, webConsentRefresh, webConsentAccess, webConsentClientCanary, webConsentEcho} {
			if strings.Contains(body, value) {
				t.Fatalf("a response carries %q:\n%s", value, body)
			}
		}
	}
}

func consentEntries(t *testing.T, path, operation string) []invokelog.Entry {
	t.Helper()
	var out []invokelog.Entry
	for _, e := range connectionLogEntries(t, path) {
		if e.Operation == operation {
			out = append(out, e)
		}
	}
	return out
}

func TestWebConsentStoresWhatTheTerminalStores(t *testing.T) {
	f := newWebConsentFixture(t, encryptedTestVault(t), defaultTestAdminTimeout)
	link := f.start(t)
	q := link.Query()
	redirect := "http://" + f.s.addr + consentReturnPath
	if q.Get("redirect_uri") != redirect || q.Get("code_challenge_method") != "S256" || q.Get("state") == "" ||
		q.Get("scope") != "items.read items.write" {
		t.Fatalf("authorization link = %s, want the redirect %s, S256, a state and both scopes", link, redirect)
	}

	rec := f.back(t, http.MethodGet, f.s.addr, answer(link))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Stored the consent of cred for reader, writer.") {
		t.Fatalf("result = %d:\n%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Errorf("Referrer-Policy = %q, want no-referrer", rec.Header().Get("Referrer-Policy"))
	}
	if got := f.storedRefresh(t); got != webConsentRefresh {
		t.Fatalf("stored refresh token = %q, want the one of the token endpoint", got)
	}
	exchanges := f.endpoints.sent("/token")
	if len(exchanges) != 1 || exchanges[0].Get("redirect_uri") != redirect || exchanges[0].Get("code") != webConsentCode ||
		exchanges[0].Get("code_verifier") == "" {
		t.Fatalf("exchange = %v, want one with the code, the verifier and the redirect address", exchanges)
	}
	entries := consentEntries(t, f.path, invokelog.OperationConsentGrant)
	if len(entries) != 2 {
		t.Fatalf("grant entries = %+v, want one per connection", entries)
	}
	for i, name := range []string{"reader", "writer"} {
		wantWebEntry(t, entries[i], invokelog.OperationConsentGrant, "update", name)
	}
	logs, _ := filepath.Glob(filepath.Join(vault.New(filepath.Dir(f.path)).Dir(), "logs", "*.jsonl"))
	for _, name := range logs {
		data, _ := os.ReadFile(name)
		f.bodies = append(f.bodies, string(data))
	}
	f.keep(f.s.request(t, http.MethodGet, "/credentials/cred", f.s.addr, f.cookie, nil))
	f.assertNoSecretInResponses(t)
}

func TestWebConsentStartNeedsTheAdminApproval(t *testing.T) {
	f := newWebConsentFixture(t, nil, defaultTestAdminTimeout)
	// A fresh encrypted vault whose passphrase this session never proved.
	v := encryptedTestVault(t)
	f.s.vault = v
	rec := f.s.postForm(t, "/credentials/cred/consent", f.s.addr, f.cookie, "http://"+f.s.addr, f.csrf, url.Values{})
	if rec.Code != http.StatusForbidden || f.s.consent != nil || strings.Contains(rec.Body.String(), "login.sample.invalid") {
		t.Fatalf("start without approval = %d, pending %v:\n%s", rec.Code, f.s.consent, rec.Body.String())
	}
}

func TestWebConsentStartNeedsTheRequestProtection(t *testing.T) {
	f := newWebConsentFixture(t, encryptedTestVault(t), defaultTestAdminTimeout)
	origin := "http://" + f.s.addr
	tests := map[string]struct {
		host, origin, csrf string
		cookie             *http.Cookie
	}{
		"foreign origin":   {f.s.addr, "http://sample.invalid", f.csrf, f.cookie},
		"no origin":        {f.s.addr, "", f.csrf, f.cookie},
		"wrong csrf value": {f.s.addr, origin, "not-the-csrf-value", f.cookie},
		"no csrf value":    {f.s.addr, origin, "", f.cookie},
		"no session":       {f.s.addr, origin, f.csrf, nil},
		"foreign host":     {"sample.invalid", origin, f.csrf, f.cookie},
	}
	for name, tc := range tests {
		rec := f.s.postForm(t, "/credentials/cred/consent", tc.host, tc.cookie, tc.origin, tc.csrf, url.Values{})
		if rec.Code != http.StatusForbidden || f.s.consent != nil {
			t.Errorf("%s: status = %d, pending %v, want 403 and no consent", name, rec.Code, f.s.consent)
		}
	}
	// None of them spent the approval.
	f.start(t)
}

func TestWebConsentReturnRefusesWithoutUsingTheState(t *testing.T) {
	f := newWebConsentFixture(t, encryptedTestVault(t), defaultTestAdminTimeout)
	link := f.start(t)
	good := answer(link)
	tests := []struct {
		name, method, host, query string
		want                      int
	}{
		{"wrong state", http.MethodGet, f.s.addr, url.Values{"state": {"other"}, "code": {webConsentCode}}.Encode(), http.StatusBadRequest},
		{"no state", http.MethodGet, f.s.addr, "code=" + webConsentCode, http.StatusBadRequest},
		{"foreign host", http.MethodGet, "sample.invalid", good, http.StatusForbidden},
		{"localhost", http.MethodGet, "localhost" + strings.TrimPrefix(f.s.addr, "127.0.0.1"), good, http.StatusForbidden},
		{"post", http.MethodPost, f.s.addr, good, http.StatusMethodNotAllowed},
		{"head", http.MethodHead, f.s.addr, good, http.StatusBadRequest},
	}
	for _, tc := range tests {
		if rec := f.back(t, tc.method, tc.host, tc.query); rec.Code != tc.want {
			t.Errorf("%s: status = %d, want %d", tc.name, rec.Code, tc.want)
		}
	}
	if f.storedRefresh(t) != "" || len(f.endpoints.sent("/token")) != 0 {
		t.Fatal("a refused answer reached the token endpoint or stored something")
	}
	if rec := f.back(t, http.MethodGet, f.s.addr, good); rec.Code != http.StatusOK || f.storedRefresh(t) != webConsentRefresh {
		t.Fatalf("the matching answer after the refused ones = %d:\n%s", rec.Code, rec.Body.String())
	}
	f.assertNoSecretInResponses(t)
}

func TestWebConsentTakesOneAnswerOnly(t *testing.T) {
	f := newWebConsentFixture(t, nil, defaultTestAdminTimeout)
	link := f.start(t)
	if rec := f.back(t, http.MethodGet, f.s.addr, answer(link)); rec.Code != http.StatusOK {
		t.Fatalf("first answer = %d", rec.Code)
	}
	if err := f.mem.Delete(secret.StoreKey("cred", config.RoleRefreshToken)); err != nil {
		t.Fatal(err)
	}
	rec := f.back(t, http.MethodGet, f.s.addr, answer(link))
	if rec.Code != http.StatusGone || f.storedRefresh(t) != "" || len(f.endpoints.sent("/token")) != 1 {
		t.Fatalf("second answer = %d, stored %q, exchanges %d; want 410 and no effect", rec.Code,
			f.storedRefresh(t), len(f.endpoints.sent("/token")))
	}
}

func TestWebConsentEndsWithTheAdminApproval(t *testing.T) {
	v := encryptedTestVault(t)
	f := newWebConsentFixture(t, v, time.Minute)
	clock := time.Now()
	f.s.now = func() time.Time { return clock }
	if err := f.s.VerifyAndGrantAdmin(syntheticPassphrase); err != nil {
		t.Fatal(err)
	}
	link := f.start(t)
	clock = clock.Add(2 * time.Minute)
	rec := f.back(t, http.MethodGet, f.s.addr, answer(link))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "admin approval") {
		t.Fatalf("answer after the approval ended = %d:\n%s", rec.Code, rec.Body.String())
	}
	if f.storedRefresh(t) != "" || len(f.endpoints.sent("/token")) != 0 {
		t.Fatal("the answer after the approval ended reached the token endpoint or stored something")
	}
	// The state is spent: a new approval does not revive it.
	clock = clock.Add(time.Second)
	if err := f.s.VerifyAndGrantAdmin(syntheticPassphrase); err != nil {
		t.Fatal(err)
	}
	if rec := f.back(t, http.MethodGet, f.s.addr, answer(link)); rec.Code != http.StatusGone || f.storedRefresh(t) != "" {
		t.Fatalf("answer after a new approval = %d, want 410 and nothing stored", rec.Code)
	}
}

func TestWebConsentEndsWithTheSession(t *testing.T) {
	f := newWebConsentFixture(t, nil, defaultTestAdminTimeout)
	link := f.start(t)
	f.s.close()
	if rec := f.back(t, http.MethodGet, f.s.addr, answer(link)); rec.Code != http.StatusGone || f.storedRefresh(t) != "" {
		t.Fatalf("answer after the run ended = %d, want 410 and nothing stored", rec.Code)
	}
}

func TestWebConsentWithASingleUseApproval(t *testing.T) {
	f := newWebConsentFixture(t, encryptedTestVault(t), 0)
	link := f.start(t)
	// The single use is spent on the start: no other change gets through.
	if rec := f.s.postForm(t, "/credentials/cred/consent", f.s.addr, f.cookie, "http://"+f.s.addr, f.csrf, url.Values{}); rec.Code != http.StatusForbidden {
		t.Fatalf("second start = %d, want 403", rec.Code)
	}
	if rec := f.back(t, http.MethodGet, f.s.addr, answer(link)); rec.Code != http.StatusOK || f.storedRefresh(t) != webConsentRefresh {
		t.Fatalf("answer of the single-use start = %d:\n%s", rec.Code, rec.Body.String())
	}
}

func TestWebConsentRefusedByTheProvider(t *testing.T) {
	f := newWebConsentFixture(t, nil, defaultTestAdminTimeout)
	link := f.start(t)
	query := url.Values{"state": {link.Query().Get("state")}, "error": {"access_denied"},
		"error_description": {webConsentEcho}}.Encode()
	rec := f.back(t, http.MethodGet, f.s.addr, query)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "was not given") {
		t.Fatalf("refusal = %d:\n%s", rec.Code, rec.Body.String())
	}
	if f.storedRefresh(t) != "" || len(f.endpoints.sent("/token")) != 0 {
		t.Fatal("a refusal reached the token endpoint or stored something")
	}
	f.assertNoSecretInResponses(t)
}

func TestWebConsentPagesWorkWithoutScript(t *testing.T) {
	f := newWebConsentFixture(t, nil, defaultTestAdminTimeout)
	page := f.keep(f.s.request(t, http.MethodGet, "/credentials/cred", f.s.addr, f.cookie, nil)).Body.String()
	for _, want := range []string{`<form method="post" action="/credentials/cred/consent">`,
		`<form method="post" action="/credentials/cred/revoke">`} {
		if !strings.Contains(page, want) {
			t.Fatalf("the credential page lacks %s:\n%s", want, page)
		}
	}
	link := f.start(t)
	result := f.back(t, http.MethodGet, f.s.addr, answer(link))
	for _, body := range f.bodies {
		if strings.Contains(body, "<script") || strings.Contains(body, "javascript:") {
			t.Fatalf("a consent page carries a script:\n%s", body)
		}
	}
	csp := result.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "script-src 'self'") || strings.Contains(csp, "unsafe-inline") {
		t.Fatalf("CSP of the result page = %q", csp)
	}
}

func TestWebConsentRevoke(t *testing.T) {
	f := newWebConsentFixture(t, nil, defaultTestAdminTimeout)
	if err := f.mem.Set(secret.StoreKey("cred", config.RoleRefreshToken), webConsentRefresh); err != nil {
		t.Fatal(err)
	}
	origin := "http://" + f.s.addr
	rec := f.keep(f.s.postForm(t, "/credentials/cred/revoke", f.s.addr, f.cookie, origin, f.csrf, url.Values{}))
	if rec.Code != http.StatusOK || f.storedRefresh(t) != webConsentRefresh || len(f.endpoints.sent("/revoke")) != 0 {
		t.Fatalf("revoke without the confirmation = %d, want the page again and no change", rec.Code)
	}
	rec = f.keep(f.s.postForm(t, "/credentials/cred/revoke", f.s.addr, f.cookie, origin, f.csrf, url.Values{"confirm": {"1"}}))
	if rec.Code != http.StatusSeeOther || f.storedRefresh(t) != "" {
		t.Fatalf("revoke = %d, stored %q; want a redirect and the refresh token removed", rec.Code, f.storedRefresh(t))
	}
	if sent := f.endpoints.sent("/revoke"); len(sent) != 1 || sent[0].Get("token") != webConsentRefresh {
		t.Fatalf("revocations = %v, want one of the stored refresh token", sent)
	}
	page := f.keep(f.s.request(t, http.MethodGet, "/credentials/cred", f.s.addr, f.cookie, nil)).Body.String()
	if !strings.Contains(page, "was revoked at the provider") {
		t.Fatalf("the credential page does not report the revocation:\n%s", page)
	}
	entries := consentEntries(t, f.path, invokelog.OperationConsentRevoke)
	if len(entries) != 2 {
		t.Fatalf("revoke entries = %+v, want one per connection", entries)
	}
	for i, name := range []string{"reader", "writer"} {
		wantWebEntry(t, entries[i], invokelog.OperationConsentRevoke, "delete", name)
	}
	f.assertNoSecretInResponses(t)
}
