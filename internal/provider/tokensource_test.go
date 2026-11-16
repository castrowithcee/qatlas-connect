package provider_test

import (
	"context"
	"errors"
	"fmt"
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

// The tests below run a provider that exists only here: a token endpoint and an API behind a fake
// transport, reached through provider.NoRedirectClient like every provider does. Nothing is registered.

const (
	sampleToken = "https://login.sample.invalid/token"
	sampleAPI   = "https://api.sample.invalid/items"
)

var sampleMetadata = config.ProviderMetadata{
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

type sampleCatalog struct{}

func (sampleCatalog) ProviderMetadata(id string) (config.ProviderMetadata, bool) {
	return sampleMetadata, id == sampleMetadata.ID
}
func (sampleCatalog) ProviderMetadataAll() []config.ProviderMetadata {
	return []config.ProviderMetadata{sampleMetadata}
}

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// sampleServer is the fake transport: it issues synthetic-access-1, -2, ... at the token endpoint and
// answers the API with the status apiStatus gives for the n-th call, counting from 1.
type sampleServer struct {
	mu     sync.Mutex
	grants int
	seen   []string
	// redeemed holds the refresh token of every token request, in order.
	redeemed  []string
	apiStatus func(n int) int
	// tokenAnswer, when set, replaces the body of a successful token answer.
	tokenAnswer func(n int) (int, string)
	// release, when set, holds every token request until it is closed.
	release chan struct{}
}

func (s *sampleServer) RoundTrip(req *http.Request) (*http.Response, error) {
	switch req.URL.String() {
	case sampleToken:
		if s.release != nil {
			<-s.release
		}
		body, _ := io.ReadAll(req.Body)
		form, _ := url.ParseQuery(string(body))
		s.mu.Lock()
		s.grants++
		n := s.grants
		s.redeemed = append(s.redeemed, form.Get("refresh_token"))
		s.mu.Unlock()
		status, answerBody := http.StatusOK, fmt.Sprintf(
			`{"access_token":"synthetic-access-%d","token_type":"Bearer","expires_in":600,"scope":"items.read items.write"}`, n)
		if s.tokenAnswer != nil {
			status, answerBody = s.tokenAnswer(n)
		}
		return answer(req, status, answerBody), nil
	case sampleAPI:
		s.mu.Lock()
		s.seen = append(s.seen, strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer "))
		n := len(s.seen)
		s.mu.Unlock()
		status := http.StatusOK
		if s.apiStatus != nil {
			status = s.apiStatus(n)
		}
		return answer(req, status, `{}`), nil
	}
	return nil, errors.New("unexpected request")
}

func (s *sampleServer) counts() (int, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.grants, append([]string(nil), s.seen...)
}

func answer(req *http.Request, status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}},
		Body: io.NopCloser(strings.NewReader(body)), Request: req}
}

// sampleHarness is one configured connection of the sample provider with the secrets its credential holds.
type sampleHarness struct {
	server  *sampleServer
	clock   *fakeClock
	cache   *provider.TokenCache
	values  map[string]string
	effects []config.Effect
	save    func(ctx context.Context, old, rotated string) (string, error)
	// warnings collects the warnings of every call.
	warnings []string
}

func newSampleHarness() *sampleHarness {
	clock := &fakeClock{now: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
	return &sampleHarness{
		server: &sampleServer{}, clock: clock, cache: provider.NewTokenCache(clock.Now),
		values: map[string]string{
			config.RoleClientID:     "sample-client-id",
			config.RoleRefreshToken: "synthetic-refresh-1",
		},
		effects: []config.Effect{config.EffectRead, config.EffectUpdate},
	}
}

func (h *sampleHarness) client() *http.Client {
	return provider.NoRedirectClient(5*time.Second, h.server)
}

func (h *sampleHarness) resolved(t *testing.T) *config.Resolved {
	t.Helper()
	cfg := config.New(sampleCatalog{})
	cfg.Services = map[string]config.Service{"sample": {Provider: "sample", BaseURL: "https://api.sample.invalid"}}
	cfg.Credentials = map[string]config.Credential{"login": {Provider: "sample", Type: config.CredentialTypeKeyring}}
	cfg.Connections = map[string]config.Connection{"work": {Service: "sample", Credential: "login", Effects: h.effects}}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() = %v", err)
	}
	resolved, err := cfg.Resolve("work", "")
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func (h *sampleHarness) secrets(red *redact.Redactor) *secret.Resolver {
	env := func(name string) string {
		for role, value := range h.values {
			if name == secret.DerivedEnvName("login", role) {
				return value
			}
		}
		return ""
	}
	return secret.NewWith(env, nil, nil, red)
}

func (h *sampleHarness) options() provider.TokenOptions {
	grant := func(ctx context.Context, req provider.TokenRequest) (provider.Token, error) {
		form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {req.Secrets.RefreshToken},
			"client_id": {req.Secrets.ClientID}, "scope": {strings.Join(req.Scopes, " ")}}
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, sampleToken, strings.NewReader(form.Encode()))
		if err != nil {
			return provider.Token{}, err
		}
		resp, err := h.client().Do(httpReq)
		if err != nil {
			return provider.Token{}, provider.Transport("access token", "the token endpoint", err)
		}
		defer resp.Body.Close()
		return provider.ReadTokenAnswer("access token", resp, h.clock.Now())
	}
	return provider.TokenOptions{
		Token: *sampleMetadata.Token, Cache: h.cache, SaveRefresh: h.save,
		Grants: map[config.TokenProcedure]provider.TokenGrant{
			config.TokenConsent: grant, config.TokenServiceAccount: grant,
		},
	}
}

// call runs one tool the way a handler of the sample provider does and classifies a failed answer.
func (h *sampleHarness) call(t *testing.T, red *redact.Redactor, tool string) error {
	t.Helper()
	resolved := h.resolved(t)
	ctx := secret.WithWarnings(secret.ForConnection(context.Background(), resolved), func(line string) {
		h.warnings = append(h.warnings, line)
	})
	source, err := provider.OpenTokenSource(ctx, resolved, h.secrets(red), red, h.options())
	if err != nil {
		return err
	}
	build := func(access string) (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, sampleAPI, nil)
		if err == nil {
			req.Header.Set("Authorization", "Bearer "+access)
		}
		return req, err
	}
	send := source.Read
	if tool == "sample.items.update" {
		send = source.Mutate
	}
	resp, err := send(ctx, h.client(), tool, build)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return provider.ClassifyStatus(tool, resp.StatusCode, provider.StatusTexts{
			Subject: "Sample", Auth: "Sample rejected the token", Permission: "Sample refused the operation"})
	}
	return nil
}

func classOf(err error) provider.Class {
	var failed *provider.Error
	if errors.As(err, &failed) {
		return failed.Class
	}
	return ""
}

func TestTokenIsRenewedShortlyBeforeItExpires(t *testing.T) {
	h := newSampleHarness()
	red := &redact.Redactor{}
	if err := h.call(t, red, "sample.items.get"); err != nil {
		t.Fatal(err)
	}
	// 600 seconds of life minus the reserve of two minutes leave 480 seconds of use.
	h.clock.Advance(479 * time.Second)
	if err := h.call(t, red, "sample.items.get"); err != nil {
		t.Fatal(err)
	}
	if grants, _ := h.server.counts(); grants != 1 {
		t.Fatalf("grants within the life of the token = %d, want 1", grants)
	}
	h.clock.Advance(2 * time.Second)
	if err := h.call(t, red, "sample.items.get"); err != nil {
		t.Fatal(err)
	}
	grants, seen := h.server.counts()
	if grants != 2 || seen[2] != "synthetic-access-2" {
		t.Fatalf("grants = %d, tokens seen = %v; want a renewal inside the reserve", grants, seen)
	}
}

func TestTokenReadRenewsOnceOn401(t *testing.T) {
	t.Run("then fails", func(t *testing.T) {
		h := newSampleHarness()
		h.server.apiStatus = func(int) int { return http.StatusUnauthorized }
		err := h.call(t, &redact.Redactor{}, "sample.items.get")
		if classOf(err) != provider.ClassAuth {
			t.Fatalf("error = %v, want auth", err)
		}
		grants, seen := h.server.counts()
		if grants != 2 || len(seen) != 2 || seen[0] == seen[1] {
			t.Fatalf("grants = %d, tokens seen = %v; want one renewal and one repetition", grants, seen)
		}
	})
	t.Run("then succeeds", func(t *testing.T) {
		h := newSampleHarness()
		h.server.apiStatus = func(n int) int {
			if n == 1 {
				return http.StatusUnauthorized
			}
			return http.StatusOK
		}
		if err := h.call(t, &redact.Redactor{}, "sample.items.get"); err != nil {
			t.Fatal(err)
		}
		if grants, seen := h.server.counts(); grants != 2 || len(seen) != 2 {
			t.Fatalf("grants = %d, tokens seen = %v", grants, seen)
		}
	})
}

func TestTokenMutationIsNotRepeatedOn401AndDropsTheToken(t *testing.T) {
	h := newSampleHarness()
	h.server.apiStatus = func(n int) int {
		if n == 1 {
			return http.StatusUnauthorized
		}
		return http.StatusOK
	}
	red := &redact.Redactor{}
	if err := h.call(t, red, "sample.items.update"); classOf(err) != provider.ClassAuth {
		t.Fatalf("error = %v, want auth", err)
	}
	if grants, seen := h.server.counts(); grants != 1 || len(seen) != 1 {
		t.Fatalf("grants = %d, tokens seen = %v; want one request without renewal", grants, seen)
	}
	// The dropped token is renewed by the next call although it has not expired.
	if err := h.call(t, red, "sample.items.get"); err != nil {
		t.Fatal(err)
	}
	if grants, seen := h.server.counts(); grants != 2 || seen[1] != "synthetic-access-2" {
		t.Fatalf("grants = %d, tokens seen = %v; want the next call to renew", grants, seen)
	}
}

func TestToken403NeverRenews(t *testing.T) {
	for _, tool := range []string{"sample.items.get", "sample.items.update"} {
		t.Run(tool, func(t *testing.T) {
			h := newSampleHarness()
			h.server.apiStatus = func(int) int { return http.StatusForbidden }
			if err := h.call(t, &redact.Redactor{}, tool); classOf(err) != provider.ClassPermission {
				t.Fatalf("error = %v, want permission", err)
			}
			if grants, seen := h.server.counts(); grants != 1 || len(seen) != 1 {
				t.Fatalf("grants = %d, tokens seen = %v", grants, seen)
			}
		})
	}
}

func TestTokenParallelCallsShareOneRenewal(t *testing.T) {
	h := newSampleHarness()
	h.server.release = make(chan struct{})
	const calls = 8
	var wg sync.WaitGroup
	errs := make(chan error, calls)
	for range calls {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- h.call(t, &redact.Redactor{}, "sample.items.get")
		}()
	}
	// Every call that arrives while the first grant is held waits for it; one that arrives later finds the
	// token cached. Either way only one grant may happen.
	time.Sleep(50 * time.Millisecond)
	close(h.server.release)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if grants, seen := h.server.counts(); grants != 1 || len(seen) != calls {
		t.Fatalf("grants = %d, API calls = %d; want one grant for %d calls", grants, len(seen), calls)
	}
}

func TestTokenRedactorKnowsEveryNewToken(t *testing.T) {
	h := newSampleHarness()
	h.server.tokenAnswer = func(n int) (int, string) {
		return http.StatusOK, fmt.Sprintf(`{"access_token":"synthetic-access-%d","token_type":"Bearer",`+
			`"expires_in":600,"refresh_token":"synthetic-refresh-%d"}`, n, n+1)
	}
	var saved []string
	h.save = func(_ context.Context, old, rotated string) (string, error) {
		saved = append(saved, old+">"+rotated)
		h.values[config.RoleRefreshToken] = rotated
		return rotated, nil
	}
	h.server.apiStatus = func(n int) int {
		if n == 1 {
			return http.StatusUnauthorized
		}
		return http.StatusOK
	}
	red := &redact.Redactor{}
	if err := h.call(t, red, "sample.items.get"); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"synthetic-access-1", "synthetic-access-2", "synthetic-refresh-2", "synthetic-refresh-3"} {
		if got := red.Apply("value " + value); strings.Contains(got, value) {
			t.Errorf("the redactor does not know %s: %q", value, got)
		}
	}
	// The renewal redeems the rotated refresh token, not the one the call started with.
	if len(saved) != 2 || saved[0] != "synthetic-refresh-1>synthetic-refresh-2" ||
		saved[1] != "synthetic-refresh-2>synthetic-refresh-3" {
		t.Errorf("saved rotations = %v", saved)
	}
	// A cached token reaches the redactor of a later call too, such as the next request of a server.
	later := &redact.Redactor{}
	if err := h.call(t, later, "sample.items.get"); err != nil {
		t.Fatal(err)
	}
	if grants, _ := h.server.counts(); grants != 2 {
		t.Fatalf("grants = %d, want the cached token for the later call", grants)
	}
	if got := later.Apply("synthetic-access-2"); got != redact.Marker {
		t.Errorf("the later redactor does not know the cached token: %q", got)
	}
}

func TestTokenRotatedRefreshTokenIsStoredBeforeUse(t *testing.T) {
	rotating := func(h *sampleHarness) {
		h.server.tokenAnswer = func(n int) (int, string) {
			return http.StatusOK, `{"access_token":"synthetic-access-1","token_type":"Bearer","expires_in":600,` +
				`"refresh_token":"synthetic-refresh-2"}`
		}
	}
	t.Run("stored first", func(t *testing.T) {
		h := newSampleHarness()
		rotating(h)
		h.save = func(_ context.Context, _, rotated string) (string, error) {
			if _, seen := h.server.counts(); len(seen) != 0 {
				t.Error("the new access token was used before the rotated refresh token was stored")
			}
			return rotated, nil
		}
		if err := h.call(t, &redact.Redactor{}, "sample.items.get"); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("storing fails", func(t *testing.T) {
		h := newSampleHarness()
		rotating(h)
		h.save = func(context.Context, string, string) (string, error) { return "", errors.New("vault locked") }
		if err := h.call(t, &redact.Redactor{}, "sample.items.get"); err == nil {
			t.Fatal("call succeeded, want the rotation to end it")
		}
		if _, seen := h.server.counts(); len(seen) != 0 {
			t.Fatalf("API calls = %v, want none with an unstored rotation", seen)
		}
	})
}

// A run that lost the race to store its rotation keeps its access token for the call, and every later renewal
// redeems the refresh token the other run stored.
func TestTokenLostRotationRedeemsTheStoredRefreshToken(t *testing.T) {
	h := newSampleHarness()
	h.server.tokenAnswer = func(n int) (int, string) {
		return http.StatusOK, fmt.Sprintf(`{"access_token":"synthetic-access-%d","token_type":"Bearer",`+
			`"expires_in":600,"refresh_token":"synthetic-refresh-mine-%d"}`, n, n)
	}
	h.save = func(_ context.Context, old, rotated string) (string, error) {
		if old == "synthetic-refresh-1" {
			return "synthetic-refresh-theirs", nil
		}
		return rotated, nil
	}
	h.server.apiStatus = func(n int) int {
		if n == 1 {
			return http.StatusUnauthorized
		}
		return http.StatusOK
	}
	red := &redact.Redactor{}
	if err := h.call(t, red, "sample.items.get"); err != nil {
		t.Fatal(err)
	}
	h.server.mu.Lock()
	redeemed, seen := append([]string(nil), h.server.redeemed...), append([]string(nil), h.server.seen...)
	h.server.mu.Unlock()
	if len(seen) != 2 || seen[0] != "synthetic-access-1" {
		t.Fatalf("API calls = %v, want the first access token used despite the lost rotation", seen)
	}
	if len(redeemed) != 2 || redeemed[0] != "synthetic-refresh-1" || redeemed[1] != "synthetic-refresh-theirs" {
		t.Fatalf("redeemed refresh tokens = %v, want the one the other run stored for the renewal", redeemed)
	}
	if got := red.Apply("synthetic-refresh-theirs"); got != redact.Marker {
		t.Errorf("the redactor does not know the stored refresh token: %q", got)
	}
}

// A refresh token from an environment variable cannot be written: the rotation is a warning naming the
// credential and the role, without a value, and the call goes on with the new access token.
func TestTokenRotationFromAnEnvironmentVariableWarns(t *testing.T) {
	h := newSampleHarness()
	h.server.tokenAnswer = func(n int) (int, string) {
		return http.StatusOK, `{"access_token":"synthetic-access-1","token_type":"Bearer","expires_in":600,` +
			`"refresh_token":"synthetic-refresh-2"}`
	}
	if err := h.call(t, &redact.Redactor{}, "sample.items.get"); err != nil {
		t.Fatalf("call = %v, want it to go on", err)
	}
	if _, seen := h.server.counts(); len(seen) != 1 || seen[0] != "synthetic-access-1" {
		t.Fatalf("API calls = %v, want the new access token used", seen)
	}
	if len(h.warnings) != 1 || !strings.Contains(h.warnings[0], `credential login, role refresh_token`) {
		t.Fatalf("warnings = %q, want one naming the credential and the role", h.warnings)
	}
	for _, value := range []string{"synthetic-refresh-1", "synthetic-refresh-2", "synthetic-access-1"} {
		if strings.Contains(h.warnings[0], value) {
			t.Errorf("the warning holds %s", value)
		}
	}
}

// A connection whose credential holds a client ID but no refresh token is valid, as the configuration test
// of the harness shows, and still ends before any request.
func TestTokenConnectionWithoutRefreshTokenIsValidButNotUsable(t *testing.T) {
	h := newSampleHarness()
	delete(h.values, config.RoleRefreshToken)
	red := &redact.Redactor{}
	if err := h.call(t, red, "sample.items.get"); classOf(err) != provider.ClassConsentRequired {
		t.Fatalf("error = %v, want consent-required", err)
	}
	class, err := provider.TestTokenConnection(context.Background(), h.resolved(t), h.secrets(red), red,
		h.options(), nil)
	if err != nil || class != provider.ClassConsentRequired {
		t.Fatalf("connection test = %q, %v; want consent-required", class, err)
	}
	if grants, seen := h.server.counts(); grants != 0 || len(seen) != 0 {
		t.Fatalf("requests = %d grants, %d API calls; want none", grants, len(seen))
	}
}

func TestTokenKeyAndClientIDTogetherFailClosed(t *testing.T) {
	for name, values := range map[string]map[string]string{
		"both": {config.RoleClientID: "sample-client-id", config.RoleRefreshToken: "synthetic-refresh-1",
			config.RoleServiceAccountKey: "synthetic-key-material"},
		"neither": {config.RoleClientSecret: "synthetic-client-secret"},
	} {
		t.Run(name, func(t *testing.T) {
			h := newSampleHarness()
			h.values = values
			err := h.call(t, &redact.Redactor{}, "sample.items.get")
			if classOf(err) != provider.ClassCredentialUnusable {
				t.Fatalf("error = %v, want credential-unusable", err)
			}
			for _, value := range values {
				if strings.Contains(err.Error(), value) {
					t.Errorf("error %q names a stored value", err)
				}
			}
			if grants, seen := h.server.counts(); grants != 0 || len(seen) != 0 {
				t.Fatalf("requests = %d grants, %d API calls; want none", grants, len(seen))
			}
		})
	}
}

func TestTokenMissingScopeLocksOnlyThatTool(t *testing.T) {
	h := newSampleHarness()
	h.server.tokenAnswer = func(n int) (int, string) {
		return http.StatusOK, `{"access_token":"synthetic-access-1","token_type":"Bearer","expires_in":600,"scope":"items.read"}`
	}
	red := &redact.Redactor{}
	err := h.call(t, red, "sample.items.update")
	if classOf(err) != provider.ClassScopeMissing || !strings.Contains(err.Error(), "items.write") {
		t.Fatalf("error = %v, want scope-missing naming items.write", err)
	}
	if _, seen := h.server.counts(); len(seen) != 0 {
		t.Fatalf("API calls = %v, want none for the tool without its scope", seen)
	}
	if err := h.call(t, red, "sample.items.get"); err != nil {
		t.Fatalf("the tool with its scope failed: %v", err)
	}
	class, err := provider.TestTokenConnection(context.Background(), h.resolved(t), h.secrets(red), red, h.options(), nil)
	if err != nil || class != provider.ClassScopeMissing {
		t.Fatalf("connection test = %q, %v; want scope-missing", class, err)
	}
}

// An answer without scopes grants the requested ones; one without a lifetime serves only its own call.
func TestTokenAnswerWithoutScopesOrLifetime(t *testing.T) {
	h := newSampleHarness()
	h.server.tokenAnswer = func(n int) (int, string) {
		return http.StatusOK, fmt.Sprintf(`{"access_token":"synthetic-access-%d","token_type":"bearer"}`, n)
	}
	red := &redact.Redactor{}
	for range 2 {
		if err := h.call(t, red, "sample.items.update"); err != nil {
			t.Fatal(err)
		}
	}
	if grants, _ := h.server.counts(); grants != 2 {
		t.Fatalf("grants = %d, want a token without lifetime to stay uncached", grants)
	}
}

// The connection test tells a usable credential from an expired consent, and the refusal keeps only the
// status and the error code of the token endpoint, never its text.
func TestTokenConnectionTestAndRefusedGrant(t *testing.T) {
	h := newSampleHarness()
	red := &redact.Redactor{}
	probed := false
	class, err := provider.TestTokenConnection(context.Background(), h.resolved(t), h.secrets(red), red, h.options(),
		func(context.Context, *provider.TokenSource) (provider.Class, error) {
			probed = true
			return provider.ClassOK, nil
		})
	if err != nil || class != provider.ClassOK || !probed {
		t.Fatalf("connection test = %q, %v, probed %v; want ok after the probe", class, err, probed)
	}

	h = newSampleHarness()
	const echoed = "echoed-request-text"
	h.server.tokenAnswer = func(int) (int, string) {
		return http.StatusBadRequest, `{"error":"invalid_grant","error_description":"` + echoed + ` synthetic-refresh-1"}`
	}
	class, err = provider.TestTokenConnection(context.Background(), h.resolved(t), h.secrets(red), red, h.options(), nil)
	if err != nil || class != provider.ClassConsentExpired {
		t.Fatalf("connection test = %q, %v; want consent-expired", class, err)
	}
	err = h.call(t, red, "sample.items.get")
	var failed *provider.Error
	if !errors.As(err, &failed) || failed.Class != provider.ClassConsentExpired {
		t.Fatalf("error = %v, want consent-expired", err)
	}
	if strings.Contains(err.Error(), echoed) || failed.Original == nil || failed.Original.Status != http.StatusBadRequest ||
		len(failed.Original.Entries) != 1 || failed.Original.Entries[0] != (provider.OriginalEntry{Code: "invalid_grant"}) {
		t.Fatalf("error = %q, original = %+v; want only status and code", err, failed.Original)
	}
	if _, seen := h.server.counts(); len(seen) != 0 {
		t.Fatalf("API calls = %v, want none after a refused grant", seen)
	}
}

func TestTokenEndpointErrorClasses(t *testing.T) {
	tests := []struct {
		status int
		body   string
		class  provider.Class
		code   string
	}{
		{400, `{"error":"invalid_grant"}`, provider.ClassConsentExpired, "invalid_grant"},
		{400, `{"error":"invalid_scope"}`, provider.ClassScopeMissing, "invalid_scope"},
		{401, `{"error":"invalid_client"}`, provider.ClassAuth, "invalid_client"},
		{429, ``, provider.ClassRateLimited, ""},
		{500, `<html>synthetic page</html>`, provider.ClassProviderError, ""},
		{400, `{"error":"bad\u0001code"}`, provider.ClassProviderError, ""},
	}
	for _, tt := range tests {
		failed := provider.TokenEndpointError("access token", tt.status, []byte(tt.body))
		if failed.Class != tt.class || failed.Original.Status != tt.status {
			t.Errorf("%d %s: class %q, status %d", tt.status, tt.body, failed.Class, failed.Original.Status)
		}
		code := ""
		if len(failed.Original.Entries) > 0 {
			code = failed.Original.Entries[0].Code
		}
		if code != tt.code || strings.Contains(failed.Error(), "synthetic") {
			t.Errorf("%d %s: code %q, message %q", tt.status, tt.body, code, failed.Error())
		}
	}
}

// A service account has no consent: its usable key yields a token, and a grant its endpoint refuses is a
// rejected key, not an expired consent.
func TestTokenServiceAccount(t *testing.T) {
	h := newSampleHarness()
	h.values = map[string]string{config.RoleServiceAccountKey: "synthetic-key-material"}
	if err := h.call(t, &redact.Redactor{}, "sample.items.get"); err != nil {
		t.Fatal(err)
	}
	h = newSampleHarness()
	h.values = map[string]string{config.RoleServiceAccountKey: "synthetic-key-material"}
	h.server.tokenAnswer = func(int) (int, string) { return http.StatusBadRequest, `{"error":"invalid_grant"}` }
	if err := h.call(t, &redact.Redactor{}, "sample.items.get"); classOf(err) != provider.ClassAuth {
		t.Fatalf("error = %v, want auth", err)
	}
}
