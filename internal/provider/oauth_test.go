package provider_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	sampleAuthorize = "https://login.sample.invalid/authorize"
	sampleRevoke    = "https://login.sample.invalid/revoke"
	// The values a consent carries are synthetic, and none of them may reach a message.
	sampleClientID     = "sample-client-id"
	sampleClientCanary = "synthetic-client-canary-91c4"
	sampleCode         = "synthetic-code-5d02"
	sampleRefresh      = "synthetic-refresh-6e17"
	sampleEcho         = "echoed-provider-text-3a9b"
)

// oauthServer is the fake transport of a provider's token and revocation endpoints. It records every form it
// receives and answers with what answer gives for the endpoint.
type oauthServer struct {
	mu     sync.Mutex
	forms  []url.Values
	urls   []string
	answer func(endpoint string) (int, string)
	fail   error
}

func (s *oauthServer) RoundTrip(req *http.Request) (*http.Response, error) {
	body, _ := io.ReadAll(req.Body)
	form, _ := url.ParseQuery(string(body))
	s.mu.Lock()
	s.forms = append(s.forms, form)
	s.urls = append(s.urls, req.Method+" "+req.URL.String())
	s.mu.Unlock()
	if s.fail != nil {
		return nil, s.fail
	}
	status, text := http.StatusOK, `{"access_token":"synthetic-access-1","token_type":"Bearer","expires_in":600,"refresh_token":"`+sampleRefresh+`"}`
	if s.answer != nil {
		status, text = s.answer(req.URL.String())
	}
	return answer(req, status, text), nil
}

func (s *oauthServer) seen() ([]url.Values, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]url.Values(nil), s.forms...), append([]string(nil), s.urls...)
}

func sampleOAuth(transport http.RoundTripper) provider.OAuth {
	return provider.OAuth{
		AuthorizeURL: sampleAuthorize, TokenURL: sampleToken, RevokeURL: sampleRevoke,
		Params: map[string]string{"access_type": "offline"}, Transport: transport,
	}
}

func TestPKCEAndStateAreFreshAndS256(t *testing.T) {
	verifier, challenge, err := provider.NewPKCE()
	if err != nil {
		t.Fatal(err)
	}
	if len(verifier) != 43 || strings.Trim(verifier, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_") != "" {
		t.Errorf("verifier %q is not 43 characters of base64url", verifier)
	}
	sum := sha256.Sum256([]byte(verifier))
	if want := base64.RawURLEncoding.EncodeToString(sum[:]); challenge != want {
		t.Errorf("challenge = %q, want the base64url SHA-256 of the verifier %q", challenge, want)
	}
	again, _, _ := provider.NewPKCE()
	first, _ := provider.NewState()
	second, _ := provider.NewState()
	if again == verifier || first == second || len(first) != 43 {
		t.Error("verifier and state must be fresh random values of every start")
	}
}

func TestOAuthValidate(t *testing.T) {
	if err := sampleOAuth(nil).Validate(); err != nil {
		t.Fatalf("Validate() = %v, want a valid OAuth", err)
	}
	for name, change := range map[string]func(*provider.OAuth){
		"http endpoint":         func(o *provider.OAuth) { o.TokenURL = "http://login.sample.invalid/token" },
		"endpoint with query":   func(o *provider.OAuth) { o.AuthorizeURL += "?tenant=x" },
		"missing endpoint":      func(o *provider.OAuth) { o.RevokeURL = "" },
		"endpoint with user":    func(o *provider.OAuth) { o.TokenURL = "https://user" + "@login.sample.invalid/token" },
		"reserved parameter":    func(o *provider.OAuth) { o.Params = map[string]string{"redirect_uri": "https://x.invalid"} },
		"reserved in any case":  func(o *provider.OAuth) { o.Params = map[string]string{"Scope": "all"} },
		"parameter value empty": func(o *provider.OAuth) { o.Params = map[string]string{"prompt": ""} },
	} {
		o := sampleOAuth(nil)
		change(&o)
		if o.Validate() == nil {
			t.Errorf("%s: Validate() = nil, want a refusal", name)
		}
	}
}

func TestAuthorizationURLCarriesStateAndChallengeOnly(t *testing.T) {
	link := sampleOAuth(nil).AuthorizationURL(sampleClientID, "http://127.0.0.1:4711/consent",
		[]string{"items.read", "items.write"}, "state-value", "challenge-value")
	u, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	if got := u.Scheme + "://" + u.Host + u.Path; got != sampleAuthorize {
		t.Errorf("link endpoint = %q, want %q", got, sampleAuthorize)
	}
	want := url.Values{
		"response_type": {"code"}, "client_id": {sampleClientID}, "redirect_uri": {"http://127.0.0.1:4711/consent"},
		"scope": {"items.read items.write"}, "state": {"state-value"}, "code_challenge": {"challenge-value"},
		"code_challenge_method": {"S256"}, "access_type": {"offline"},
	}
	if got := u.Query(); got.Encode() != want.Encode() {
		t.Errorf("link query = %v, want %v", got, want)
	}
}

func TestExchangeSendsVerifierRedirectAndSecretOnlyInTheBody(t *testing.T) {
	for _, withSecret := range []bool{true, false} {
		server := &oauthServer{}
		secrets := provider.TokenSecrets{ClientID: sampleClientID}
		if withSecret {
			secrets.ClientSecret = sampleClientCanary
		}
		token, err := sampleOAuth(server).Exchange(context.Background(), secrets, sampleCode, "verifier-value",
			"http://127.0.0.1:4711/consent")
		if err != nil {
			t.Fatalf("Exchange() = %v", err)
		}
		if token.Refresh != sampleRefresh {
			t.Errorf("Exchange() refresh = %q, want the answered one", token.Refresh)
		}
		forms, urls := server.seen()
		if len(forms) != 1 || urls[0] != "POST "+sampleToken {
			t.Fatalf("requests = %v, want one POST to the token endpoint without a query", urls)
		}
		want := url.Values{
			"grant_type": {"authorization_code"}, "code": {sampleCode}, "code_verifier": {"verifier-value"},
			"redirect_uri": {"http://127.0.0.1:4711/consent"}, "client_id": {sampleClientID},
		}
		if withSecret {
			want.Set("client_secret", sampleClientCanary)
		}
		if forms[0].Encode() != want.Encode() {
			t.Errorf("with secret %v: form = %v, want %v", withSecret, forms[0], want)
		}
	}
}

// A refusal of the token endpoint keeps only status and error code: an echoed request, a secret among it, or
// any description never reaches the message.
func TestExchangeRefusalCarriesNoProviderText(t *testing.T) {
	server := &oauthServer{answer: func(string) (int, string) {
		return http.StatusBadRequest, `{"error":"invalid_grant","error_description":"` + sampleEcho + ` ` + sampleCode + `"}`
	}}
	_, err := sampleOAuth(server).Exchange(context.Background(),
		provider.TokenSecrets{ClientID: sampleClientID, ClientSecret: sampleClientCanary}, sampleCode, "v",
		"http://127.0.0.1:4711/consent")
	var failed *provider.Error
	if !errors.As(err, &failed) || failed.Class != provider.ClassConsentExpired {
		t.Fatalf("Exchange() = %v, want the class of an expired consent", err)
	}
	for _, leak := range []string{sampleEcho, sampleCode, sampleClientCanary} {
		if strings.Contains(err.Error(), leak) {
			t.Errorf("error %q carries %q", err, leak)
		}
	}
}

// The refresh grant renews through the token source. invalid_grant ends the call with consent-expired after
// exactly one request and deletes nothing: no save is ever asked for.
func TestRefreshGrantRenewsAndEndsOnInvalidGrant(t *testing.T) {
	server := &oauthServer{answer: func(string) (int, string) {
		return http.StatusOK, `{"access_token":"synthetic-access-2","token_type":"Bearer","expires_in":600}`
	}}
	oauth := sampleOAuth(server)
	cfg := config.New(sampleCatalog{})
	cfg.Services = map[string]config.Service{"svc": {Provider: "sample", BaseURL: "https://api.sample.invalid"}}
	cfg.Credentials = map[string]config.Credential{"cred": {Provider: "sample", Type: config.CredentialTypeEnv,
		Values: map[string]string{config.RoleClientID: "OAUTH_TEST_CLIENT", config.RoleRefreshToken: "OAUTH_TEST_REFRESH"}}}
	cfg.Connections = map[string]config.Connection{"conn": {Service: "svc", Credential: "cred"}}
	resolved, err := cfg.Resolve("conn", "")
	if err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"OAUTH_TEST_CLIENT": sampleClientID, "OAUTH_TEST_REFRESH": sampleRefresh}
	red := &redact.Redactor{}
	secrets := secret.NewWith(func(name string) string { return env[name] }, nil, nil, red)
	saved := 0
	options := func() provider.TokenOptions {
		return provider.TokenOptions{
			Token: *sampleMetadata.Token, Cache: provider.NewTokenCache(time.Now),
			Grants:      map[config.TokenProcedure]provider.TokenGrant{config.TokenConsent: oauth.RefreshGrant()},
			SaveRefresh: func(context.Context, string, string) error { saved++; return nil },
		}
	}
	source, err := provider.OpenTokenSource(context.Background(), resolved, secrets, red, options())
	if err != nil {
		t.Fatal(err)
	}
	if err := source.Check(context.Background()); err != nil {
		t.Fatalf("Check() = %v, want a renewed token", err)
	}
	forms, _ := server.seen()
	want := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {sampleRefresh}, "client_id": {sampleClientID}}
	if len(forms) != 1 || forms[0].Encode() != want.Encode() {
		t.Errorf("refresh form = %v, want %v and no scope", forms, want)
	}

	server.answer = func(string) (int, string) {
		return http.StatusBadRequest, `{"error":"invalid_grant","error_description":"` + sampleEcho + `"}`
	}
	source, err = provider.OpenTokenSource(context.Background(), resolved, secrets, red, options())
	if err != nil {
		t.Fatal(err)
	}
	err = source.Check(context.Background())
	var failed *provider.Error
	if !errors.As(err, &failed) || failed.Class != provider.ClassConsentExpired {
		t.Fatalf("Check() = %v, want consent-expired", err)
	}
	if forms, _ := server.seen(); len(forms) != 2 {
		t.Errorf("token requests = %d, want 2: invalid_grant is never retried", len(forms))
	}
	if saved != 0 {
		t.Errorf("SaveRefresh ran %d times, want none: invalid_grant changes nothing stored", saved)
	}
	for _, leak := range []string{sampleEcho, sampleRefresh, "synthetic-access-2"} {
		if strings.Contains(red.Apply(err.Error()), leak) || strings.Contains(err.Error(), leak) {
			t.Errorf("error %q carries %q", err, leak)
		}
	}
}

func TestRefreshGrantNarrowsScopesOnlyWhenTheProviderAllows(t *testing.T) {
	server := &oauthServer{}
	oauth := sampleOAuth(server)
	oauth.NarrowRefresh = true
	req := provider.TokenRequest{Procedure: config.TokenConsent, Scopes: []string{"items.read"},
		Secrets: provider.TokenSecrets{ClientID: sampleClientID, RefreshToken: sampleRefresh}}
	if _, err := oauth.RefreshGrant()(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	forms, _ := server.seen()
	if got := forms[0].Get("scope"); got != "items.read" {
		t.Errorf("scope = %q, want only the connection's scopes", got)
	}
}

func TestRevokeConfirmsOnlyASuccess(t *testing.T) {
	secrets := provider.TokenSecrets{ClientID: sampleClientID, ClientSecret: sampleClientCanary, RefreshToken: sampleRefresh}
	server := &oauthServer{answer: func(string) (int, string) { return http.StatusOK, "" }}
	if err := sampleOAuth(server).Revoke(context.Background(), secrets); err != nil {
		t.Fatalf("Revoke() = %v, want a confirmation", err)
	}
	forms, urls := server.seen()
	want := url.Values{"token": {sampleRefresh}, "token_type_hint": {"refresh_token"}, "client_id": {sampleClientID},
		"client_secret": {sampleClientCanary}}
	if urls[0] != "POST "+sampleRevoke || forms[0].Encode() != want.Encode() {
		t.Errorf("revocation = %v %v, want a POST of %v", urls, forms, want)
	}

	for name, server := range map[string]*oauthServer{
		"refused":     {answer: func(string) (int, string) { return http.StatusBadRequest, `{"error":"invalid_token"}` }},
		"unavailable": {answer: func(string) (int, string) { return http.StatusServiceUnavailable, sampleEcho }},
		"unreachable": {fail: errors.New("dial refused")},
	} {
		err := sampleOAuth(server).Revoke(context.Background(), secrets)
		if err == nil {
			t.Errorf("%s: Revoke() = nil, want no confirmation", name)
			continue
		}
		for _, leak := range []string{sampleEcho, sampleRefresh, sampleClientCanary} {
			if strings.Contains(err.Error(), leak) {
				t.Errorf("%s: error %q carries %q", name, err, leak)
			}
		}
	}
}
