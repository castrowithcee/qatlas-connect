package manage

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/connlog"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// The provider "sample" exists only in these tests: its credential issues access tokens, and its token and
// revocation endpoints sit behind a fake transport. The receiver of a consent is real and listens on
// 127.0.0.1 with a port the system picks.

const (
	consentClientID     = "sample-client-id"
	consentClientCanary = "synthetic-client-canary-77ab"
	consentCode         = "synthetic-code-0c4e"
	consentRefresh      = "synthetic-refresh-91d3"
	consentAccess       = "synthetic-access-5f20"
	consentEcho         = "echoed-provider-text-b8e1"
)

var consentProvider = config.ProviderMetadata{
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

type consentCatalog struct{}

func (consentCatalog) ProviderMetadata(id string) (config.ProviderMetadata, bool) {
	return consentProvider, id == consentProvider.ID
}
func (consentCatalog) ProviderMetadataAll() []config.ProviderMetadata {
	return []config.ProviderMetadata{consentProvider}
}

// tokenEndpoints is the fake transport of the token and revocation endpoints.
type tokenEndpoints struct {
	mu       sync.Mutex
	forms    map[string][]url.Values
	exchange func() (int, string)
	revoke   func() (int, string)
	fail     error
}

func (e *tokenEndpoints) RoundTrip(req *http.Request) (*http.Response, error) {
	body, _ := io.ReadAll(req.Body)
	form, _ := url.ParseQuery(string(body))
	e.mu.Lock()
	if e.forms == nil {
		e.forms = map[string][]url.Values{}
	}
	e.forms[req.URL.Path] = append(e.forms[req.URL.Path], form)
	e.mu.Unlock()
	if e.fail != nil {
		return nil, e.fail
	}
	status, text := http.StatusOK, `{"access_token":"`+consentAccess+`","token_type":"Bearer","expires_in":600,`+
		`"refresh_token":"`+consentRefresh+`"}`
	switch {
	case req.URL.Path == "/token" && e.exchange != nil:
		status, text = e.exchange()
	case req.URL.Path == "/revoke":
		status, text = http.StatusOK, ""
		if e.revoke != nil {
			status, text = e.revoke()
		}
	}
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}},
		Body: io.NopCloser(strings.NewReader(text)), Request: req}, nil
}

func (e *tokenEndpoints) sent(path string) []url.Values {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]url.Values(nil), e.forms[path]...)
}

type consentFixture struct {
	svc       *Service
	secrets   *fakeSecrets
	endpoints *tokenEndpoints
	red       *redact.Redactor
	dir       string
	proof     AdminProof
}

// newConsentFixture saves a configuration with the keyring credential "cred" of the provider sample, used by
// a reading and a writing connection, the env credential "vars", and the keyring credential "lonely" that no
// connection uses. "cred" holds a client ID and a client secret.
func newConsentFixture(t *testing.T) *consentFixture {
	t.Helper()
	dir := t.TempDir()
	store := config.NewStore(filepath.Join(dir, "config.yaml"), consentCatalog{})
	cfg := store.New()
	cfg.Services = map[string]config.Service{"svc": {Provider: "sample", BaseURL: "https://api.sample.invalid"}}
	cfg.Credentials = map[string]config.Credential{
		"cred":   {Provider: "sample", Type: config.CredentialTypeKeyring},
		"lonely": {Provider: "sample", Type: config.CredentialTypeKeyring},
		"vars": {Provider: "sample", Type: config.CredentialTypeEnv, Values: map[string]string{
			config.RoleClientID: "SAMPLE_CLIENT", config.RoleRefreshToken: "SAMPLE_REFRESH"}},
	}
	cfg.Connections = map[string]config.Connection{
		"reader": {Service: "svc", Credential: "cred", Effects: []config.Effect{config.EffectRead}},
		"writer": {Service: "svc", Credential: "cred", Tools: []string{"sample.items.update"}},
		"envy":   {Service: "svc", Credential: "vars", Effects: []config.Effect{config.EffectRead}},
	}
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	f := &consentFixture{secrets: newFakeSecrets(), endpoints: &tokenEndpoints{}, red: &redact.Redactor{}, dir: dir}
	f.secrets.entries["cred."+config.RoleClientID] = consentClientID
	f.secrets.entries["cred."+config.RoleClientSecret] = consentClientCanary
	f.secrets.entries["lonely."+config.RoleClientID] = consentClientID
	oauth := provider.OAuth{
		AuthorizeURL: "https://login.sample.invalid/authorize", TokenURL: "https://login.sample.invalid/token",
		RevokeURL: "https://login.sample.invalid/revoke", Transport: f.endpoints,
	}
	f.svc = New(store, f.secrets, connlog.SurfaceCLI, f.red).WithVaultProcessSupport(false).
		WithConsentSource(func(id string) (provider.OAuth, bool) { return oauth, id == "sample" })
	session := NewAdminSession(true, nil)
	session.Grant(0)
	f.proof, _ = session.Authorize(0)
	return f
}

func (f *consentFixture) start(t *testing.T) *Consent {
	t.Helper()
	c, err := f.svc.StartConsent(context.Background(), f.proof, "cred")
	if err != nil {
		t.Fatalf("StartConsent() = %v", err)
	}
	t.Cleanup(c.Close)
	return c
}

// answerURL is the redirect address the provider would send the browser back to.
func answerURL(c *Consent, query string) string { return c.redirect + "?" + query }

// get sends a request to the receiver with the given Host header and returns the status and the page.
func get(t *testing.T, method, rawURL, host string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, rawURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if host != "" {
		req.Host = host
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s = %v", method, rawURL, err)
	}
	defer resp.Body.Close()
	page, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(page)
}

// assertNothingStored checks that the credential holds no refresh token and the token endpoint saw nothing.
func (f *consentFixture) assertNothingStored(t *testing.T) {
	t.Helper()
	if _, ok := f.secrets.entries["cred."+config.RoleRefreshToken]; ok {
		t.Error("the credential holds a refresh token, want nothing stored")
	}
}

func TestStartConsentRefusesBeforeAnyLink(t *testing.T) {
	f := newConsentFixture(t)
	f.secrets.entries["keyed."+config.RoleClientID] = consentClientID
	tests := []struct {
		name, credential string
		proof            AdminProof
		prepare          func()
		reason           ConsentReason
	}{
		{"no admin proof", "cred", AdminProof{}, nil, ConsentAdminRequired},
		{"environment credential", "vars", f.proof, nil, ConsentNotStored},
		{"no client ID", "cred", f.proof, func() { delete(f.secrets.entries, "cred."+config.RoleClientID) }, ConsentNoClientID},
		{"service account key", "cred", f.proof, func() {
			f.secrets.entries["cred."+config.RoleClientID] = consentClientID
			f.secrets.entries["cred."+config.RoleServiceAccountKey] = "{}"
		}, ConsentServiceAccount},
		{"no connection", "lonely", f.proof, nil, ConsentNoConnection},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.prepare != nil {
				tt.prepare()
			}
			c, err := f.svc.StartConsent(context.Background(), tt.proof, tt.credential)
			var consentErr *ConsentError
			if c != nil || !errors.As(err, &consentErr) || consentErr.Reason != tt.reason || !consentErr.Precondition() {
				t.Fatalf("StartConsent() = %v, %v, want the precondition %s and no consent", c, err, tt.reason)
			}
		})
	}
	without := f.svc.WithConsentSource(nil)
	delete(f.secrets.entries, "cred."+config.RoleServiceAccountKey)
	_, err := without.StartConsent(context.Background(), f.proof, "cred")
	var consentErr *ConsentError
	if !errors.As(err, &consentErr) || consentErr.Reason != ConsentNoProvider {
		t.Errorf("StartConsent() without consent endpoints = %v, want %s", err, ConsentNoProvider)
	}
	if f.endpoints.sent("/token") != nil {
		t.Error("a refused start reached the token endpoint")
	}
}

// The receiver on the same host takes the answer, the code is exchanged with the verifier and exactly the
// redirect address, and the refresh token is stored. The link carries state and the S256 challenge of the
// verifier, never a secret, and asks for the scopes of every connection of the credential together.
func TestConsentOnTheSameHost(t *testing.T) {
	f := newConsentFixture(t)
	c := f.start(t)
	link, err := url.Parse(c.Link())
	if err != nil {
		t.Fatal(err)
	}
	q := link.Query()
	_, challenge, _ := provider.NewPKCE()
	if q.Get("state") != c.state || q.Get("code_challenge_method") != "S256" || len(q.Get("code_challenge")) != len(challenge) {
		t.Errorf("link query = %v, want state and an S256 challenge", q)
	}
	if q.Get("scope") != "items.read items.write" {
		t.Errorf("scope = %q, want the union of both connections", q.Get("scope"))
	}
	if !strings.HasPrefix(q.Get("redirect_uri"), "http://127.0.0.1:") || !strings.HasSuffix(q.Get("redirect_uri"), "/consent") {
		t.Errorf("redirect_uri = %q, want the loopback address with the fixed path", q.Get("redirect_uri"))
	}
	for _, leak := range []string{consentClientCanary, c.verifier} {
		if strings.Contains(c.Link(), leak) {
			t.Errorf("the link carries a secret")
		}
	}

	status, page := get(t, http.MethodGet, answerURL(c, "state="+c.state+"&code="+consentCode), "")
	if status != http.StatusOK || strings.Contains(page, consentCode) || strings.Contains(page, "<script") {
		t.Errorf("answer page = %d %q, want 200 without code or script", status, page)
	}
	out, err := c.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() = %v", err)
	}
	if got := f.secrets.entries["cred."+config.RoleRefreshToken]; got != consentRefresh {
		t.Errorf("stored refresh token = %q, want the one of the answer", got)
	}
	if strings.Join(out.Connections, ",") != "reader,writer" || len(out.MissingScopes) != 0 {
		t.Errorf("outcome = %+v, want both connections and no missing scope", out)
	}
	forms := f.endpoints.sent("/token")
	if len(forms) != 1 {
		t.Fatalf("token requests = %d, want 1", len(forms))
	}
	want := url.Values{"grant_type": {"authorization_code"}, "code": {consentCode}, "code_verifier": {c.verifier},
		"redirect_uri": {c.redirect}, "client_id": {consentClientID}, "client_secret": {consentClientCanary}}
	if forms[0].Encode() != want.Encode() {
		t.Errorf("exchange form = %v, want %v", forms[0], want)
	}
	for _, value := range []string{consentCode, consentRefresh, consentAccess, consentClientCanary, c.verifier} {
		if got := f.red.Apply("x " + value + " x"); strings.Contains(got, value) {
			t.Errorf("the redactor does not cover a value of the consent: %q", got)
		}
	}
	assertConsentLog(t, f.dir, "config.consent.grant", 2)
}

// assertConsentLog checks that the invocation log holds want entries of operation and no value of a consent.
func assertConsentLog(t *testing.T, dir, operation string, want int) {
	t.Helper()
	var text strings.Builder
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && filepath.Base(path) != "config.yaml" {
			data, _ := os.ReadFile(path)
			text.Write(data)
		}
		return nil
	})
	if got := strings.Count(text.String(), operation); got != want {
		t.Errorf("log entries of %s = %d, want %d", operation, got, want)
	}
	for _, value := range []string{consentCode, consentRefresh, consentAccess, consentClientCanary, consentClientID} {
		if strings.Contains(text.String(), value) {
			t.Errorf("the log carries a value of the consent")
		}
	}
}

// Against DNS rebinding the receiver takes only a GET of the fixed path with exactly its host and port. A
// POST, another host, another path or another state is refused without using up the consent.
func TestConsentReceiverRefusesWithoutUsingTheState(t *testing.T) {
	f := newConsentFixture(t)
	c := f.start(t)
	good := answerURL(c, "state="+c.state+"&code="+consentCode)
	port := strings.TrimPrefix(c.host, "127.0.0.1")
	for _, tt := range []struct {
		name, method, url, host string
		status                  int
	}{
		{"POST", http.MethodPost, good, "", http.StatusMethodNotAllowed},
		{"foreign host", http.MethodGet, good, "rebound.example.invalid" + port, http.StatusNotFound},
		{"localhost instead of the address", http.MethodGet, good, "localhost" + port, http.StatusNotFound},
		{"other path", http.MethodGet, "http://" + c.host + "/other?state=" + c.state + "&code=x", "", http.StatusNotFound},
		{"wrong state", http.MethodGet, answerURL(c, "state=other&code="+consentCode), "", http.StatusBadRequest},
		{"no state", http.MethodGet, answerURL(c, "code="+consentCode), "", http.StatusBadRequest},
	} {
		if status, page := get(t, tt.method, tt.url, tt.host); status != tt.status || strings.Contains(page, consentCode) {
			t.Errorf("%s: %d %q, want %d without the code", tt.name, status, page, tt.status)
		}
	}
	if status, _ := get(t, http.MethodGet, good, ""); status != http.StatusOK {
		t.Fatalf("the valid answer after the refusals = %d, want 200", status)
	}
	if _, err := c.Wait(context.Background()); err != nil {
		t.Fatalf("Wait() = %v", err)
	}
}

// A pasted address is taken only with the scheme, host, port, path and state of the redirect address.
func TestConsentPastedAddress(t *testing.T) {
	f := newConsentFixture(t)
	c := f.start(t)
	query := "?state=" + c.state + "&code=" + consentCode
	for _, tt := range []struct {
		name, address string
		want          error
	}{
		{"wrong port", "http://127.0.0.1:1/consent" + query, ErrNotRedirect},
		{"wrong path", "http://" + c.host + "/elsewhere" + query, ErrNotRedirect},
		{"https", "https://" + c.host + "/consent" + query, ErrNotRedirect},
		{"localhost", "http://localhost" + strings.TrimPrefix(c.host, "127.0.0.1") + "/consent" + query, ErrNotRedirect},
		{"no address", "the code", ErrNotRedirect},
		{"wrong state", c.redirect + "?state=other&code=" + consentCode, ErrOtherConsent},
	} {
		if err := c.Paste(tt.address); !errors.Is(err, tt.want) {
			t.Errorf("%s: Paste() = %v, want %v", tt.name, err, tt.want)
		} else if strings.Contains(err.Error(), consentCode) {
			t.Errorf("%s: the refusal repeats the code", tt.name)
		}
	}
	if err := c.Paste("  " + c.redirect + query + "\n"); err != nil {
		t.Fatalf("Paste() of the redirect address = %v", err)
	}
	if _, err := c.Wait(context.Background()); err != nil {
		t.Fatalf("Wait() = %v", err)
	}
	if f.secrets.entries["cred."+config.RoleRefreshToken] != consentRefresh {
		t.Error("the pasted consent stored no refresh token")
	}
}

// Exactly one answer is taken: a second one, through the receiver or pasted, is refused.
func TestConsentTakesOneAnswerOnly(t *testing.T) {
	f := newConsentFixture(t)
	c := f.start(t)
	if err := c.Paste(answerURL(c, "state="+c.state+"&code="+consentCode)); err != nil {
		t.Fatal(err)
	}
	if err := c.Paste(answerURL(c, "state="+c.state+"&code=second")); !errors.Is(err, ErrConsentEnded) {
		t.Errorf("second Paste() = %v, want %v", err, ErrConsentEnded)
	}
	if status, _ := get(t, http.MethodGet, answerURL(c, "state="+c.state+"&code=second"), ""); status != http.StatusGone {
		t.Errorf("second answer through the receiver = %d, want 410", status)
	}
	if _, err := c.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	forms := f.endpoints.sent("/token")
	if len(forms) != 1 || forms[0].Get("code") != consentCode {
		t.Errorf("exchanges = %v, want one with the first code", forms)
	}
}

// A consent that ends by its deadline or by cancellation stores nothing, sends nothing, and refuses a late
// answer.
func TestConsentExpiresAndCancelsWithoutEffect(t *testing.T) {
	f := newConsentFixture(t)
	f.svc.consentTimeout = 50 * time.Millisecond
	c := f.start(t)
	_, err := c.Wait(context.Background())
	var consentErr *ConsentError
	if !errors.As(err, &consentErr) || consentErr.Reason != ConsentExpired {
		t.Fatalf("Wait() = %v, want %s", err, ConsentExpired)
	}
	if err := c.Paste(answerURL(c, "state="+c.state+"&code="+consentCode)); !errors.Is(err, ErrConsentEnded) {
		t.Errorf("late Paste() = %v, want %v", err, ErrConsentEnded)
	}

	f.svc.consentTimeout = 0
	c = f.start(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Wait(ctx); !errors.As(err, &consentErr) || consentErr.Reason != ConsentCancelled {
		t.Fatalf("Wait() after cancel = %v, want %s", err, ConsentCancelled)
	}
	if _, err := http.Get(answerURL(c, "state="+c.state+"&code="+consentCode)); err == nil {
		t.Error("the receiver still answers after the consent was cancelled")
	}
	f.assertNothingStored(t)
	if f.endpoints.sent("/token") != nil {
		t.Error("an ended consent reached the token endpoint")
	}
}

// A refusal by the provider ends without effect, and its text, even its description, reaches no message.
func TestConsentRefusedByTheProvider(t *testing.T) {
	f := newConsentFixture(t)
	c := f.start(t)
	status, page := get(t, http.MethodGet, answerURL(c, "state="+c.state+"&error=access_denied&error_description="+consentEcho), "")
	if status != http.StatusOK || strings.Contains(page, consentEcho) {
		t.Errorf("answer page = %d %q, want 200 without the provider's text", status, page)
	}
	_, err := c.Wait(context.Background())
	var consentErr *ConsentError
	if !errors.As(err, &consentErr) || consentErr.Reason != ConsentRefused || strings.Contains(err.Error(), consentEcho) {
		t.Fatalf("Wait() = %v, want %s without the provider's text", err, ConsentRefused)
	}
	f.assertNothingStored(t)
}

// An exchange answer without a refresh token, and a refused exchange, store nothing; the refusal keeps only
// the class of the token endpoint's error code.
func TestConsentExchangeWithoutEffect(t *testing.T) {
	for name, tt := range map[string]struct {
		answer func() (int, string)
		check  func(error) bool
	}{
		"no refresh token": {func() (int, string) {
			return http.StatusOK, `{"access_token":"` + consentAccess + `","token_type":"Bearer","expires_in":600}`
		}, func(err error) bool {
			var consentErr *ConsentError
			return errors.As(err, &consentErr) && consentErr.Reason == ConsentNoRefreshToken
		}},
		"invalid_grant": {func() (int, string) {
			return http.StatusBadRequest, `{"error":"invalid_grant","error_description":"` + consentEcho + `"}`
		}, func(err error) bool {
			var failed *provider.Error
			return errors.As(err, &failed) && failed.Class == provider.ClassConsentExpired
		}},
	} {
		t.Run(name, func(t *testing.T) {
			f := newConsentFixture(t)
			f.endpoints.exchange = tt.answer
			c := f.start(t)
			if err := c.Paste(answerURL(c, "state="+c.state+"&code="+consentCode)); err != nil {
				t.Fatal(err)
			}
			_, err := c.Wait(context.Background())
			if !tt.check(err) {
				t.Fatalf("Wait() = %v", err)
			}
			for _, leak := range []string{consentEcho, consentAccess, consentCode} {
				if strings.Contains(f.red.Apply(err.Error()), leak) {
					t.Errorf("error %q carries %q", err, leak)
				}
			}
			f.assertNothingStored(t)
		})
	}
}

// Scopes the token endpoint did not grant are reported; the refresh token is stored all the same, so the
// tools whose scopes were granted work.
func TestConsentReportsMissingScopes(t *testing.T) {
	f := newConsentFixture(t)
	f.endpoints.exchange = func() (int, string) {
		return http.StatusOK, `{"access_token":"` + consentAccess + `","token_type":"Bearer","expires_in":600,` +
			`"scope":"items.read","refresh_token":"` + consentRefresh + `"}`
	}
	c := f.start(t)
	if err := c.Paste(answerURL(c, "state="+c.state+"&code="+consentCode)); err != nil {
		t.Fatal(err)
	}
	out, err := c.Wait(context.Background())
	if err != nil || strings.Join(out.MissingScopes, ",") != "items.write" {
		t.Fatalf("Wait() = %+v, %v, want items.write missing", out, err)
	}
}

// A revocation always removes the refresh token here; only an answer of success confirms it.
func TestRevokeConsent(t *testing.T) {
	for _, tt := range []struct {
		name      string
		revoke    func() (int, string)
		fail      error
		noSource  bool
		noClient  bool
		confirmed bool
	}{
		{name: "confirmed", confirmed: true},
		{name: "refused", revoke: func() (int, string) { return http.StatusServiceUnavailable, consentEcho }},
		{name: "unreachable", fail: errors.New("dial refused")},
		{name: "no consent endpoints", noSource: true},
		{name: "no client ID", noClient: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newConsentFixture(t)
			f.endpoints.revoke, f.endpoints.fail = tt.revoke, tt.fail
			f.secrets.entries["cred."+config.RoleRefreshToken] = consentRefresh
			if tt.noClient {
				delete(f.secrets.entries, "cred."+config.RoleClientID)
			}
			svc := f.svc
			if tt.noSource {
				svc = svc.WithConsentSource(nil)
			}
			out, err := svc.RevokeConsent(context.Background(), f.proof, "cred")
			if err != nil {
				t.Fatalf("RevokeConsent() = %v", err)
			}
			if out.Confirmed != tt.confirmed {
				t.Errorf("Confirmed = %v, want %v", out.Confirmed, tt.confirmed)
			}
			f.assertNothingStored(t)
			if got := f.secrets.entries["cred."+config.RoleClientID]; !tt.noClient && got != consentClientID {
				t.Error("the revocation removed more than the refresh token")
			}
			result := "unconfirmed"
			if tt.confirmed {
				result = "success"
			}
			assertConsentLog(t, f.dir, "config.consent.revoke", 2)
			assertConsentLog(t, f.dir, `"result":"`+result+`"`, 2)
		})
	}
}

// Deleting the refresh token goes through the revocation; a failed revocation still removes it, and a failed
// removal still says whether the provider confirmed.
func TestRevokeConsentEdges(t *testing.T) {
	f := newConsentFixture(t)
	_, err := f.svc.RevokeConsent(context.Background(), f.proof, "cred")
	var consentErr *ConsentError
	if !errors.As(err, &consentErr) || consentErr.Reason != ConsentMissing {
		t.Errorf("RevokeConsent() without a refresh token = %v, want %s", err, ConsentMissing)
	}
	if f.endpoints.sent("/revoke") != nil {
		t.Error("a revocation without a refresh token reached the provider")
	}
	if _, err := f.svc.RevokeConsent(context.Background(), AdminProof{}, "cred"); !errors.As(err, &consentErr) ||
		consentErr.Reason != ConsentAdminRequired {
		t.Errorf("RevokeConsent() without proof = %v, want %s", err, ConsentAdminRequired)
	}
	if _, err := f.svc.RevokeConsent(context.Background(), f.proof, "vars"); !errors.As(err, &consentErr) ||
		consentErr.Reason != ConsentNotStored {
		t.Errorf("RevokeConsent() of an env credential = %v, want %s", err, ConsentNotStored)
	}

	f.secrets.entries["cred."+config.RoleRefreshToken] = consentRefresh
	f.secrets.deleteErr[config.RoleRefreshToken] = errors.New("store refused")
	out, err := f.svc.RevokeConsent(context.Background(), f.proof, "cred")
	if err == nil || !out.Confirmed {
		t.Errorf("RevokeConsent() with a failing removal = %+v, %v, want the error and the confirmation", out, err)
	}
	if !HoldsConsent(mustLoad(t, f.svc), "cred") || HoldsConsent(mustLoad(t, f.svc), "vars") {
		t.Error("HoldsConsent() names the wrong credentials")
	}
}

func mustLoad(t *testing.T, svc *Service) *config.Config {
	t.Helper()
	cfg, _, err := svc.Load()
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// Removing the refresh token of a credential that holds a consent revokes it first, through the one path
// every surface takes, and removes it whether the provider confirmed or not.
func TestDeleteSecretRevokesTheRefreshTokenFirst(t *testing.T) {
	for _, confirmed := range []bool{true, false} {
		f := newConsentFixture(t)
		if !confirmed {
			f.endpoints.revoke = func() (int, string) { return http.StatusBadRequest, consentEcho }
		}
		f.secrets.entries["cred."+config.RoleRefreshToken] = consentRefresh
		cfg := mustLoad(t, f.svc)
		out, err := f.svc.DeleteSecret(context.Background(), cfg, SecretWrite{
			Name: "cred", Entry: cfg.Credentials["cred"], Role: config.RoleRefreshToken,
		})
		if err != nil {
			t.Fatalf("confirmed %v: DeleteSecret() = %v", confirmed, err)
		}
		if out.Revocation == nil || out.Revocation.Confirmed != confirmed {
			t.Fatalf("confirmed %v: Revocation = %+v", confirmed, out.Revocation)
		}
		if note := out.Revocation.Note(); strings.Contains(note, "did not confirm") == confirmed || strings.Contains(note, consentEcho) {
			t.Errorf("confirmed %v: note = %q", confirmed, note)
		}
		if forms := f.endpoints.sent("/revoke"); len(forms) != 1 || forms[0].Get("token") != consentRefresh {
			t.Errorf("confirmed %v: revocations = %v, want one of the stored refresh token", confirmed, forms)
		}
		f.assertNothingStored(t)
	}

	// Another role, or a credential whose provider issues no access tokens, is removed without a revocation.
	f := newConsentFixture(t)
	cfg := mustLoad(t, f.svc)
	out, err := f.svc.DeleteSecret(context.Background(), cfg, SecretWrite{
		Name: "cred", Entry: cfg.Credentials["cred"], Role: config.RoleClientSecret,
	})
	if err != nil || out.Revocation != nil || f.endpoints.sent("/revoke") != nil {
		t.Errorf("DeleteSecret() of the client secret = %+v, %v, want no revocation", out, err)
	}
}

// A revocation from the TUI is logged per connection of the credential, without a warning.
func TestRevocationsFromTheTUIAreLogged(t *testing.T) {
	f := newConsentFixture(t)
	f.svc.surface = connlog.SurfaceTUI
	f.secrets.entries["cred."+config.RoleRefreshToken] = consentRefresh
	cfg := mustLoad(t, f.svc)
	out, err := f.svc.DeleteSecret(context.Background(), cfg, SecretWrite{
		Name: "cred", Entry: cfg.Credentials["cred"], Role: config.RoleRefreshToken,
	})
	if err != nil || out.Revocation == nil {
		t.Fatalf("DeleteSecret() = %+v, %v", out, err)
	}
	if out.Revocation.LogWarning != "" {
		t.Errorf("LogWarning = %q, want none", out.Revocation.LogWarning)
	}
	assertConsentLog(t, f.dir, "config.consent.revoke", 2)
	assertConsentLog(t, f.dir, `"path":"tui"`, 2)

	// Deleting a credential from the TUI revokes without a warning. Only an unused credential can be deleted,
	// so there is no connection to log an entry for.
	f = newConsentFixture(t)
	f.svc.surface = connlog.SurfaceTUI
	f.secrets.entries["lonely."+config.RoleRefreshToken] = consentRefresh
	cfg, rev, err := f.svc.Load()
	if err != nil {
		t.Fatal(err)
	}
	deleted, err := f.svc.DeleteCredential(context.Background(), cfg, rev, "lonely")
	if err != nil || deleted.Revocation == nil {
		t.Fatalf("DeleteCredential() = %+v, %v", deleted, err)
	}
	if deleted.Revocation.LogWarning != "" {
		t.Errorf("DeleteCredential() LogWarning = %q, want none", deleted.Revocation.LogWarning)
	}
	assertConsentLog(t, f.dir, "config.consent.revoke", 0)
}

// Deleting a credential that holds a consent revokes and removes its refresh token first and then removes
// the credential, also when the provider does not confirm. A credential still in use is refused before
// anything is sent.
func TestDeleteCredentialRevokesFirst(t *testing.T) {
	for _, confirmed := range []bool{true, false} {
		f := newConsentFixture(t)
		if !confirmed {
			f.endpoints.fail = errors.New("dial refused")
		}
		f.secrets.entries["lonely."+config.RoleRefreshToken] = consentRefresh
		cfg, rev, err := f.svc.Load()
		if err != nil {
			t.Fatal(err)
		}
		out, err := f.svc.DeleteCredential(context.Background(), cfg, rev, "lonely")
		if err != nil {
			t.Fatalf("confirmed %v: DeleteCredential() = %v", confirmed, err)
		}
		if out.Revocation == nil || out.Revocation.Confirmed != confirmed {
			t.Errorf("confirmed %v: Revocation = %+v", confirmed, out.Revocation)
		}
		if _, ok := f.secrets.entries["lonely."+config.RoleRefreshToken]; ok {
			t.Errorf("confirmed %v: the refresh token is still stored", confirmed)
		}
		if _, ok := mustLoad(t, f.svc).Credentials["lonely"]; ok {
			t.Errorf("confirmed %v: the credential is still configured", confirmed)
		}
	}

	f := newConsentFixture(t)
	f.secrets.entries["cred."+config.RoleRefreshToken] = consentRefresh
	cfg, rev, _ := f.svc.Load()
	if _, err := f.svc.DeleteCredential(context.Background(), cfg, rev, "cred"); err == nil {
		t.Error("DeleteCredential() of a credential in use succeeded")
	}
	if f.endpoints.sent("/revoke") != nil || f.secrets.entries["cred."+config.RoleRefreshToken] != consentRefresh {
		t.Error("a refused deletion revoked or removed the refresh token")
	}

	// Without a stored refresh token there is nothing to revoke, and the credential is removed.
	out, err := f.svc.DeleteCredential(context.Background(), cfg, rev, "lonely")
	if err != nil || out.Revocation != nil || f.endpoints.sent("/revoke") != nil {
		t.Errorf("DeleteCredential() without a consent = %+v, %v, want no revocation", out, err)
	}

	// A refresh token that cannot be removed keeps the credential.
	f = newConsentFixture(t)
	f.secrets.entries["lonely."+config.RoleRefreshToken] = consentRefresh
	f.secrets.deleteErr[config.RoleRefreshToken] = errors.New("store refused")
	cfg, rev, _ = f.svc.Load()
	if _, err := f.svc.DeleteCredential(context.Background(), cfg, rev, "lonely"); err == nil {
		t.Error("DeleteCredential() with a failing removal succeeded")
	}
	if _, ok := mustLoad(t, f.svc).Credentials["lonely"]; !ok {
		t.Error("the credential was removed although its refresh token could not be")
	}
}
