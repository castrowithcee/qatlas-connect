package provider_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// The impersonation tests run the sample provider with an impersonation endpoint behind a fake transport.
// Nothing is registered.

const (
	sampleImpersonate      = "https://iam.sample.invalid/v1/accounts/" + provider.ServiceAccountSegment + ":mint"
	sampleImpersonateScope = "sample.impersonate"
	// The values below are synthetic; none of them may reach a message.
	impersonationRefresh = "synthetic-refresh-imp-1"
	impersonationRotated = "synthetic-refresh-imp-2"
	impersonationUser    = "synthetic-user-access-7c1d"
	impersonationAccess  = "synthetic-robot-access-4b8e"
	impersonationEcho    = "echoed-impersonation-text-2f6a"
)

// sampleRobot is a synthetic service account address, split so no line holds a whole address.
var sampleRobot = "robot" + "@" + "sample.invalid"

// impersonationServer is the fake transport of the sample provider's token endpoint, impersonation endpoint
// and API. It records every request in order.
type impersonationServer struct {
	mu sync.Mutex
	// events names every request: "token", "impersonate" or "api", and "save" for a stored rotation.
	events      []string
	tokenForms  []url.Values
	impURLs     []string
	impAuth     []string
	impBodies   []map[string]any
	apiAuth     []string
	tokenAnswer func() (int, string)
	impAnswer   func() (int, string)
}

func (s *impersonationServer) RoundTrip(req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil {
		body, _ = io.ReadAll(req.Body)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case req.URL.String() == sampleToken:
		form, _ := url.ParseQuery(string(body))
		s.events = append(s.events, "token")
		s.tokenForms = append(s.tokenForms, form)
		status, text := http.StatusOK, `{"access_token":"`+impersonationUser+`","token_type":"Bearer","expires_in":3600,"scope":"`+sampleImpersonateScope+`"}`
		if s.tokenAnswer != nil {
			status, text = s.tokenAnswer()
		}
		return answer(req, status, text), nil
	case req.URL.Host == "iam.sample.invalid":
		var parsed map[string]any
		_ = json.Unmarshal(body, &parsed)
		s.events = append(s.events, "impersonate")
		s.impURLs = append(s.impURLs, req.Method+" "+req.URL.EscapedPath())
		s.impAuth = append(s.impAuth, req.Header.Get("Authorization"))
		s.impBodies = append(s.impBodies, parsed)
		expiry := time.Now().Add(3 * time.Hour).UTC().Format(time.RFC3339)
		status, text := http.StatusOK, `{"accessToken":"`+impersonationAccess+`","expireTime":"`+expiry+`"}`
		if s.impAnswer != nil {
			status, text = s.impAnswer()
		}
		return answer(req, status, text), nil
	case req.URL.String() == sampleAPI:
		s.events = append(s.events, "api")
		s.apiAuth = append(s.apiAuth, strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer "))
		return answer(req, http.StatusOK, `{}`), nil
	}
	return nil, errors.New("unexpected request")
}

func (s *impersonationServer) record(event string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
}

func (s *impersonationServer) seenEvents() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.events...)
}

func impersonationOAuth(transport http.RoundTripper) provider.OAuth {
	o := sampleOAuth(transport)
	o.NarrowRefresh = true
	o.ImpersonateURL, o.ImpersonateScope = sampleImpersonate, sampleImpersonateScope
	return o
}

// impersonationHarness is a credential of the sample provider with a consent, used by connections that
// impersonate a service account.
type impersonationHarness struct {
	server *impersonationServer
	cache  *provider.TokenCache
	values map[string]string
	saved  []string
}

func newImpersonationHarness() *impersonationHarness {
	return &impersonationHarness{
		server: &impersonationServer{}, cache: provider.NewTokenCache(time.Now),
		values: map[string]string{config.RoleClientID: sampleClientID, config.RoleRefreshToken: impersonationRefresh},
	}
}

// resolved is the connection name of the sample provider that impersonates account.
func (h *impersonationHarness) resolved(t *testing.T, name, account string) *config.Resolved {
	t.Helper()
	cfg := config.New(sampleCatalog{})
	cfg.Services = map[string]config.Service{"sample": {Provider: "sample", BaseURL: "https://api.sample.invalid"}}
	cfg.Credentials = map[string]config.Credential{"login": {Provider: "sample", Type: config.CredentialTypeKeyring}}
	cfg.Connections = map[string]config.Connection{name: {Service: "sample", Credential: "login",
		Effects: []config.Effect{config.EffectRead, config.EffectUpdate}, ServiceAccount: account}}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() = %v", err)
	}
	resolved, err := cfg.Resolve(name, "")
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func (h *impersonationHarness) options() provider.TokenOptions {
	return provider.TokenOptions{
		Token: *sampleMetadata.Token, Cache: h.cache,
		Grants: map[config.TokenProcedure]provider.TokenGrant{
			config.TokenImpersonation: impersonationOAuth(h.server).ImpersonationGrant(),
		},
		SaveRefresh: func(_ context.Context, old, rotated string) (string, error) {
			h.server.record("save")
			h.saved = append(h.saved, old+">"+rotated)
			h.values[config.RoleRefreshToken] = rotated
			return rotated, nil
		},
	}
}

// call reads the sample API through a connection that impersonates account.
func (h *impersonationHarness) call(t *testing.T, red *redact.Redactor, name, account string) error {
	t.Helper()
	resolved := h.resolved(t, name, account)
	ctx := secret.ForConnection(context.Background(), resolved)
	values := copyValues(h.values)
	secrets := secret.NewWith(func(env string) string {
		for role, value := range values {
			if env == secret.DerivedEnvName("login", role) {
				return value
			}
		}
		return ""
	}, nil, nil, red)
	source, err := provider.OpenTokenSource(ctx, resolved, secrets, red, h.options())
	if err != nil {
		return err
	}
	resp, err := source.Read(ctx, provider.NoRedirectClient(5*time.Second, h.server), "sample.items.get",
		func(access string) (*http.Request, error) {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, sampleAPI, nil)
			if err == nil {
				req.Header.Set("Authorization", "Bearer "+access)
			}
			return req, err
		})
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

func copyValues(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// The token of the consent, asked for with the impersonation scope alone, obtains an access token of the
// service account the connection names at the fixed endpoint, with the scopes of the connection and a
// lifetime of an hour; the API sees only the service account's token.
func TestImpersonationObtainsAnAccessTokenOfTheServiceAccount(t *testing.T) {
	h := newImpersonationHarness()
	red := &redact.Redactor{}
	if err := h.call(t, red, "work", sampleRobot); err != nil {
		t.Fatal(err)
	}
	s := h.server
	if got := s.seenEvents(); !slices.Equal(got, []string{"token", "impersonate", "api"}) {
		t.Fatalf("requests = %v", got)
	}
	form := s.tokenForms[0]
	if form.Get("grant_type") != "refresh_token" || form.Get("refresh_token") != impersonationRefresh ||
		form.Get("scope") != sampleImpersonateScope {
		t.Errorf("refresh form = %v, want the refresh token with the impersonation scope alone", form)
	}
	if want := "POST /v1/accounts/" + url.PathEscape(sampleRobot) + ":mint"; s.impURLs[0] != want {
		t.Errorf("impersonation request = %q, want %q", s.impURLs[0], want)
	}
	if s.impAuth[0] != "Bearer "+impersonationUser {
		t.Errorf("impersonation request is not authorized by the token of the consent")
	}
	body := s.impBodies[0]
	scopes, _ := body["scope"].([]any)
	if fmt.Sprint(scopes) != "[items.read items.write]" || body["lifetime"] != "3600s" || len(body) != 2 {
		t.Errorf("impersonation body = %v, want the scopes of the connection and an hour, nothing else", body)
	}
	if !slices.Equal(s.apiAuth, []string{impersonationAccess}) {
		t.Errorf("the API saw %v, want only the token of the service account", s.apiAuth)
	}
}

// The impersonated token lives an hour at most, whatever the endpoint claims, and is reused within it.
func TestImpersonatedTokenLivesAnHourAtMost(t *testing.T) {
	server := &impersonationServer{}
	grant := impersonationOAuth(server).ImpersonationGrant()
	before := time.Now()
	token, err := grant(context.Background(), provider.TokenRequest{
		Procedure: config.TokenImpersonation, ServiceAccount: sampleRobot, Scopes: []string{"items.read"},
		Secrets: provider.TokenSecrets{ClientID: sampleClientID, RefreshToken: impersonationRefresh},
	})
	if err != nil {
		t.Fatal(err)
	}
	if token.Expiry.IsZero() || token.Expiry.After(time.Now().Add(time.Hour)) || token.Expiry.Before(before.Add(59*time.Minute)) {
		t.Errorf("expiry = %v, want an hour from now at most", token.Expiry)
	}

	server.impAnswer = func() (int, string) { return http.StatusOK, `{"accessToken":"` + impersonationAccess + `"}` }
	token, err = grant(context.Background(), provider.TokenRequest{
		Procedure: config.TokenImpersonation, ServiceAccount: sampleRobot, Scopes: []string{"items.read"},
		Secrets: provider.TokenSecrets{ClientID: sampleClientID, RefreshToken: impersonationRefresh},
	})
	if err != nil || !token.Expiry.IsZero() {
		t.Errorf("an answer without expiry = %v, %v; want a token for this call only", token.Expiry, err)
	}

	h := newImpersonationHarness()
	for range 2 {
		if err := h.call(t, &redact.Redactor{}, "work", sampleRobot); err != nil {
			t.Fatal(err)
		}
	}
	if got := h.server.seenEvents(); !slices.Equal(got, []string{"token", "impersonate", "api", "api"}) {
		t.Errorf("requests = %v, want the cached token reused", got)
	}
}

// Missing permission at the service account, an expired consent and an unknown service account end with
// codes of their own, without the text of either endpoint.
func TestImpersonationTellsItsFailuresApart(t *testing.T) {
	echo := `{"error":{"code":403,"message":"` + impersonationEcho + `","status":"PERMISSION_DENIED"}}`
	tests := []struct {
		name       string
		token, imp func() (int, string)
		want       provider.Class
		requests   []string
	}{
		{"permission missing", nil, func() (int, string) { return http.StatusForbidden, echo },
			provider.ClassImpersonationDenied, []string{"token", "impersonate"}},
		{"unknown service account", nil, func() (int, string) { return http.StatusNotFound, echo },
			provider.ClassServiceAccountUnknown, []string{"token", "impersonate"}},
		{"consent expired", func() (int, string) {
			return http.StatusBadRequest, `{"error":"invalid_grant","error_description":"` + impersonationEcho + `"}`
		}, nil, provider.ClassConsentExpired, []string{"token"}},
		{"consent lacks the impersonation scope", func() (int, string) {
			return http.StatusOK, `{"access_token":"` + impersonationUser + `","token_type":"Bearer","expires_in":3600,"scope":"items.read"}`
		}, nil, provider.ClassScopeMissing, []string{"token"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newImpersonationHarness()
			h.server.tokenAnswer, h.server.impAnswer = tt.token, tt.imp
			red := &redact.Redactor{}
			err := h.call(t, red, "work", sampleRobot)
			if classOf(err) != tt.want {
				t.Fatalf("error = %v, want %s", err, tt.want)
			}
			if got := h.server.seenEvents(); !slices.Equal(got, tt.requests) {
				t.Errorf("requests = %v, want %v", got, tt.requests)
			}
			for _, leak := range []string{impersonationEcho, impersonationUser, impersonationRefresh, sampleRobot} {
				if strings.Contains(err.Error(), leak) {
					t.Errorf("the message %q carries %q", err, leak)
				}
			}
			var failed *provider.Error
			if errors.As(err, &failed) && failed.Original != nil && len(failed.Original.Entries) > 0 &&
				strings.Contains(fmt.Sprint(failed.Original.Entries), impersonationEcho) {
				t.Errorf("the original keeps the text of the endpoint: %+v", failed.Original)
			}
		})
	}
}

// Every token of the impersonation reaches the redactor before it is used, a refresh token the first answer
// rotated is stored before the token of the consent is sent on, and the service account comes only from the
// connection: one that is not a single checked segment is refused before anything is sent.
func TestImpersonationRegistersTokensAndStoresARotationFirst(t *testing.T) {
	h := newImpersonationHarness()
	h.server.tokenAnswer = func() (int, string) {
		return http.StatusOK, `{"access_token":"` + impersonationUser + `","token_type":"Bearer","expires_in":3600,"refresh_token":"` + impersonationRotated + `"}`
	}
	red := &redact.Redactor{}
	if err := h.call(t, red, "work", sampleRobot); err != nil {
		t.Fatal(err)
	}
	if got := h.server.seenEvents(); !slices.Equal(got, []string{"token", "save", "impersonate", "api"}) {
		t.Fatalf("requests = %v, want the rotation stored before the impersonation", got)
	}
	if !slices.Equal(h.saved, []string{impersonationRefresh + ">" + impersonationRotated}) {
		t.Errorf("saved = %v, want the rotation stored once", h.saved)
	}
	for _, value := range []string{impersonationUser, impersonationRotated, impersonationAccess, impersonationRefresh} {
		if got := red.Apply("value " + value); strings.Contains(got, value) {
			t.Errorf("%s is not registered with the redactor", value)
		}
	}

	server := &impersonationServer{}
	grant := impersonationOAuth(server).ImpersonationGrant()
	for _, account := range []string{"robot/../other", "robot?x=1", "..", ""} {
		_, err := grant(context.Background(), provider.TokenRequest{
			Procedure: config.TokenImpersonation, ServiceAccount: account, Scopes: []string{"items.read"},
			Secrets: provider.TokenSecrets{ClientID: sampleClientID, RefreshToken: impersonationRefresh},
		})
		if err == nil {
			t.Errorf("service account %q was accepted", account)
		}
	}
	if got := server.seenEvents(); len(got) != 0 {
		t.Errorf("requests for an unusable service account = %v, want none", got)
	}
}

// Changing the service account of a connection never reuses the token of another one, and a credential with a
// key cannot impersonate.
func TestImpersonatedTokensAreBoundToTheServiceAccount(t *testing.T) {
	h := newImpersonationHarness()
	for _, account := range []string{sampleRobot, "other-robot", sampleRobot} {
		if err := h.call(t, &redact.Redactor{}, "work", account); err != nil {
			t.Fatal(err)
		}
	}
	if got := len(h.server.impURLs); got != 2 {
		t.Errorf("impersonation requests = %d, want one per service account", got)
	}

	h = newImpersonationHarness()
	h.values = map[string]string{config.RoleServiceAccountKey: "synthetic-key-file"}
	if err := h.call(t, &redact.Redactor{}, "work", sampleRobot); classOf(err) != provider.ClassCredentialUnusable {
		t.Errorf("a key with a service account = %v, want credential-unusable", err)
	}
	if got := h.server.seenEvents(); len(got) != 0 {
		t.Errorf("requests = %v, want none", got)
	}
}

func TestImpersonationEndpointValidation(t *testing.T) {
	if err := impersonationOAuth(nil).Validate(); err != nil {
		t.Fatalf("Validate() = %v, want a valid OAuth", err)
	}
	for name, change := range map[string]func(*provider.OAuth){
		"no scope":         func(o *provider.OAuth) { o.ImpersonateScope = "" },
		"no endpoint":      func(o *provider.OAuth) { o.ImpersonateURL = "" },
		"no placeholder":   func(o *provider.OAuth) { o.ImpersonateURL = "https://iam.sample.invalid/v1/accounts/x:mint" },
		"two placeholders": func(o *provider.OAuth) { o.ImpersonateURL += "/" + provider.ServiceAccountSegment },
		"placeholder in the host": func(o *provider.OAuth) {
			o.ImpersonateURL = "https://" + provider.ServiceAccountSegment + ".sample.invalid/mint"
		},
		"placeholder in a query": func(o *provider.OAuth) {
			o.ImpersonateURL = "https://iam.sample.invalid/mint?a=" + provider.ServiceAccountSegment
		},
		"http endpoint":      func(o *provider.OAuth) { o.ImpersonateURL = strings.Replace(o.ImpersonateURL, "https", "http", 1) },
		"scope with a space": func(o *provider.OAuth) { o.ImpersonateScope = "a b" },
	} {
		o := impersonationOAuth(nil)
		change(&o)
		if o.Validate() == nil {
			t.Errorf("%s: Validate() = nil, want a refusal", name)
		}
	}
}
