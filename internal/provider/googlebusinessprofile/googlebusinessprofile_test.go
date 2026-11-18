package googlebusinessprofile

import (
	"context"
	"encoding/json"
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
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/ratelimit"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// No test reaches Google: a fake transport answers the token endpoint and the APIs. The values below are
// synthetic stand-ins for a credential and for text a failing Google echoes.
const (
	clientIDValue = "sample-client-id"
	refreshValue  = "synthetic-refresh-canary"
	accessPrefix  = "synthetic-access-canary-"
	detailCanary  = "provider-detail-canary-7c1d"
	clientIDEnv   = "TEST_BUSINESS_PROFILE_CLIENT_ID"
	refreshEnv    = "TEST_BUSINESS_PROFILE_REFRESH"
	keyEnv        = "TEST_BUSINESS_PROFILE_KEY"
	tokenEndpoint = "https://oauth2.googleapis.com/token"

	boundAccount  = "accounts/1000001"
	otherAccount  = "accounts/1000009"
	boundLocation = "locations/2000002"
	otherLocation = "locations/2000003"
)

var robot = "robot" + "@" + "sample.invalid"

type seen struct {
	host, path, query, authorization, full, body string
}

func (r seen) values() url.Values {
	v, _ := url.ParseQuery(r.query)
	return v
}

// fakeGoogle is the transport of every request. The token endpoint issues accessPrefix1, 2, ... unless
// tokenAnswer says otherwise; the APIs answer with answer, by default an empty object.
type fakeGoogle struct {
	mu          sync.Mutex
	api         []seen
	grants      []url.Values
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
	case r.URL.Host == "mybusinessaccountmanagement.googleapis.com" ||
		r.URL.Host == "mybusinessbusinessinformation.googleapis.com" || r.URL.Host == "mybusiness.googleapis.com":
		f.api = append(f.api, seen{host: r.URL.Host, path: r.URL.EscapedPath(), query: r.URL.RawQuery,
			authorization: r.Header.Get("Authorization"), full: r.URL.String(), body: body})
		if f.answer != nil {
			return f.answer(r, len(f.api))
		}
		return respond(r, http.StatusOK, "application/json", `{}`), nil
	}
	return nil, errors.New("unexpected host")
}

func (f *fakeGoogle) calls() ([]seen, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]seen(nil), f.api...), len(f.grants)
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
		`"metadata":{"token":"%s1","quota_limit_value":"300"}}]}}`, code, detailCanary, accessPrefix, status, reason,
		accessPrefix)
}

// zeroQuotaObject is the rate limit of a project whose quota is zero.
func zeroQuotaObject() string {
	return `{"error":{"code":429,"message":"` + detailCanary + `","status":"RESOURCE_EXHAUSTED","details":[{"@type":` +
		`"type.googleapis.com/google.rpc.ErrorInfo","reason":"RATE_LIMIT_EXCEEDED","domain":"googleapis.com",` +
		`"metadata":{"quota_limit_value":"0","quota_metric":"sample-metric"}}]}}`
}

type environment struct {
	fake   *fakeGoogle
	cfg    *config.Config
	core   *application.Core
	red    *redact.Redactor
	values map[string]string
}

// newEnvironment installs the fake transport, a fresh token cache and rate limits without waits, and builds
// a validated configuration with one credential and connections unbound and bound to each target kind.
func newEnvironment(t *testing.T, values map[string]string) *environment {
	t.Helper()
	fake := &fakeGoogle{}
	previousTransport, previousCache, previousLimiters, previousChanges := transport, tokenCache, limiters, changeLimiters
	transport, tokenCache = fake, provider.NewTokenCache(time.Now)
	limiters, changeLimiters = ratelimit.NewRegistry(0), ratelimit.NewRegistry(changeInterval)
	t.Cleanup(func() {
		transport, tokenCache, limiters, changeLimiters = previousTransport, previousCache, previousLimiters, previousChanges
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
	cfg.Services = map[string]config.Service{"profile": {Provider: Provider}}
	cfg.Credentials = map[string]config.Credential{"google": {Provider: Provider, Type: config.CredentialTypeEnv, Values: roles}}
	read := []config.Effect{config.EffectRead}
	cfg.Connections = map[string]config.Connection{
		"all":      {Service: "profile", Credential: "google", Effects: read},
		"account":  {Service: "profile", Credential: "google", Effects: read, Target: boundAccount},
		"location": {Service: "profile", Credential: "google", Effects: read, Target: boundLocation},
	}
	if values[clientIDEnv] != "" && values[refreshEnv] != "" {
		cfg.Connections["robot"] = config.Connection{Service: "profile", Credential: "google", Effects: read,
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

func isInvalidRequest(err error) bool {
	var invalid *provider.InvalidRequestError
	return errors.As(err, &invalid)
}

func TestRegisterPublishesMetadataScopesAndProfile(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, ok := reg.ProviderMetadata(Provider)
	if !ok || metadata.DefaultBaseURL != accountRoot || metadata.DocPath() != "google.business-profile/setup.md" ||
		!reflect.DeepEqual(metadata.SecretRoles, config.TokenSecretRoles()) || metadata.Token == nil {
		t.Fatalf("metadata = %+v", metadata)
	}
	var ids []string
	for _, tool := range metadata.Tools {
		ids = append(ids, tool.ID)
		if tool.Effect != config.EffectRead && !strings.HasSuffix(tool.ID, ".update") &&
			!strings.HasSuffix(tool.ID, "reviews.update-reply") && !strings.HasSuffix(tool.ID, "reviews.delete-reply") {
			t.Errorf("%s effect = %s, want read", tool.ID, tool.Effect)
		}
		if scopes := metadata.Token.Scopes[tool.ID]; !reflect.DeepEqual(scopes, []string{scopeManage}) {
			t.Errorf("%s scopes = %v", tool.ID, scopes)
		}
	}
	want := []string{
		"google.business-profile.account-admins.list", "google.business-profile.account-admins.update",
		"google.business-profile.accounts.get", "google.business-profile.accounts.list",
		"google.business-profile.accounts.update", "google.business-profile.location-admins.list",
		"google.business-profile.location-admins.update", "google.business-profile.locations.get",
		"google.business-profile.locations.list", "google.business-profile.performance.fetch-daily-metrics",
		"google.business-profile.performance.get-daily-metric", "google.business-profile.performance.list-search-keywords",
		"google.business-profile.reviews.batch-get", "google.business-profile.reviews.delete-reply",
		"google.business-profile.reviews.get", "google.business-profile.reviews.list",
		"google.business-profile.reviews.update-reply"}
	if !reflect.DeepEqual(ids, want) {
		t.Errorf("tools = %v, want %v", ids, want)
	}
	profile, ok := metadata.RecommendedProfile()
	if !ok || profile.ID != "read" || len(metadata.Profiles) != 2 || len(profile.Tools) != 12 ||
		!strings.Contains(profile.Description, "business.manage") {
		t.Errorf("profiles = %+v", metadata.Profiles)
	}
	for _, d := range reg.Provider(Provider) {
		risk := d.Risk
		if risk.Effect == capability.EffectUpdate || risk.Effect == capability.EffectDelete {
			continue
		}
		if risk.Effect != capability.EffectRead || risk.Idempotency != capability.IdempotencySafe ||
			risk.Confirmation != capability.ConfirmationNone || !risk.OpenWorld || risk.DataSensitivity == "" {
			t.Errorf("%s risk = %+v", d.ID, risk)
		}
	}
	for _, raw := range []string{boundAccount, boundLocation} {
		if err := metadata.Target.Validate(raw); err != nil {
			t.Errorf("target %s refused: %v", raw, err)
		}
	}
	for _, raw := range []string{"accounts/-", "accounts/01", "accounts/12/locations/3", "locations/", "1000001",
		"locations/2000002/x"} {
		if err := metadata.Target.Validate(raw); err == nil {
			t.Errorf("target %q accepted", raw)
		}
	}
}

func TestOnlyAConsentIsOffered(t *testing.T) {
	env := newEnvironment(t, map[string]string{keyEnv: "synthetic-key-file"})
	if _, err := env.invoke(t, accountsListID, "all", `{}`); classOf(err) != provider.ClassCredentialUnusable {
		t.Errorf("service account key: error = %v, want credential-unusable", err)
	}
	env = newEnvironment(t, nil)
	if _, err := env.invoke(t, accountsListID, "robot", `{}`); classOf(err) != provider.ClassCredentialUnusable {
		t.Errorf("impersonation: error = %v, want credential-unusable", err)
	}
	if calls, grants := env.fake.calls(); len(calls) != 0 || grants != 0 {
		t.Errorf("%d requests and %d grants, want none", len(calls), grants)
	}
}

func TestTokenTravelsOnlyInTheHeader(t *testing.T) {
	env := newEnvironment(t, nil)
	if _, err := env.invoke(t, accountsListID, "all", `{}`); err != nil {
		t.Fatal(err)
	}
	calls, _ := env.fake.calls()
	if len(calls) != 1 || calls[0].authorization != "Bearer "+accessPrefix+"1" ||
		strings.Contains(calls[0].full, accessPrefix) || strings.Contains(calls[0].full, refreshValue) {
		t.Errorf("requests = %+v", calls)
	}
	env.fake.mu.Lock()
	form := env.fake.grants[0]
	env.fake.mu.Unlock()
	if form.Get("grant_type") != "refresh_token" || form.Get("refresh_token") != refreshValue ||
		form.Get("client_id") != clientIDValue {
		t.Errorf("token request = %v", form)
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
		"429 with a quota of zero": {reply(http.StatusTooManyRequests, zeroQuotaObject()), ClassAccessNotApproved, 1, 1},
		"400":                      {reply(http.StatusBadRequest, errorObject(400, "INVALID_ARGUMENT", "")), provider.ClassInvalidResponse, 1, 1},
		"500":                      {reply(http.StatusInternalServerError, errorObject(500, "INTERNAL", "")), provider.ClassProviderError, 1, 1},
		"unreadable body":          {reply(http.StatusOK, `{"name":`+detailCanary), provider.ClassInvalidResponse, 1, 1},
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
			_, err := env.invoke(t, accountsGetID, "all", `{"account":"`+boundAccount+`"}`)
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

// A rate limit after a successful call is rate-limited, not a missing approval; without Retry-After the
// next call of the client waits at least a second, and nothing repeats the refused request.
func TestRateLimitAfterSuccessHoldsTheNextCall(t *testing.T) {
	env := newEnvironment(t, nil)
	limiters = ratelimit.NewRegistry(time.Millisecond)
	env.fake.answer = func(r *http.Request, n int) (*http.Response, error) {
		if n == 1 {
			return respond(r, http.StatusOK, "application/json", `{"accounts":[{"name":"`+boundAccount+`"}]}`), nil
		}
		return respond(r, http.StatusTooManyRequests, "application/json",
			errorObject(429, "RESOURCE_EXHAUSTED", "RATE_LIMIT_EXCEEDED")), nil
	}
	if _, err := env.invoke(t, accountsListID, "all", `{}`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	request := application.InvokeRequest{Operation: accountsListID, Connection: "all", Arguments: json.RawMessage(`{}`)}
	if _, err := env.core.Invoke(ctx, request); classOf(err) != provider.ClassRateLimited {
		t.Fatalf("second call = %v, want rate-limited", err)
	}
	// The hold of at least a second outlasts the deadline, so the third call ends without a request.
	if _, err := env.core.Invoke(ctx, request); classOf(err) != provider.ClassRateLimited {
		t.Fatalf("third call = %v, want rate-limited before sending", err)
	}
	if calls, _ := env.fake.calls(); len(calls) != 2 {
		t.Errorf("%d requests, want two", len(calls))
	}
}

func TestConnectionTest(t *testing.T) {
	one := `{"accounts":[{"name":"` + boundAccount + `","type":"PERSONAL"}]}`
	for name, c := range map[string]struct {
		connection string
		values     map[string]string
		token      func(int) (int, string)
		answer     func(*http.Request, int) (*http.Response, error)
		class      provider.Class
		requests   []string
	}{
		"success": {connection: "all", answer: reply(http.StatusOK, one), class: provider.ClassOK,
			requests: []string{"/v1/accounts?pageSize=1"}},
		"no account": {connection: "all", answer: reply(http.StatusOK, `{}`), class: provider.ClassOK,
			requests: []string{"/v1/accounts?pageSize=1"}},
		"account target": {connection: "account", answer: func(r *http.Request, n int) (*http.Response, error) {
			if n == 1 {
				return respond(r, http.StatusOK, "application/json", one), nil
			}
			return respond(r, http.StatusOK, "application/json", `{"name":"`+boundAccount+`"}`), nil
		}, class: provider.ClassOK, requests: []string{"/v1/accounts?pageSize=1", "/v1/" + boundAccount}},
		"account target not found": {connection: "account", answer: func(r *http.Request, n int) (*http.Response, error) {
			if n == 1 {
				return respond(r, http.StatusOK, "application/json", one), nil
			}
			return respond(r, http.StatusNotFound, "application/json", errorObject(404, "NOT_FOUND", "")), nil
		}, class: provider.ClassNotFound, requests: []string{"/v1/accounts?pageSize=1", "/v1/" + boundAccount}},
		"rate limit after success": {connection: "account", answer: func(r *http.Request, n int) (*http.Response, error) {
			if n == 1 {
				return respond(r, http.StatusOK, "application/json", one), nil
			}
			return respond(r, http.StatusTooManyRequests, "application/json",
				errorObject(429, "RESOURCE_EXHAUSTED", "RATE_LIMIT_EXCEEDED")), nil
		}, class: provider.ClassRateLimited, requests: []string{"/v1/accounts?pageSize=1", "/v1/" + boundAccount}},
		"location target": {connection: "location", answer: reply(http.StatusOK, `{"name":"`+boundLocation+`"}`),
			class: provider.ClassOK, requests: []string{"/v1/" + boundLocation + "?readMask=name"}},
		"location target not approved": {connection: "location", answer: reply(http.StatusTooManyRequests,
			errorObject(429, "RESOURCE_EXHAUSTED", "RATE_LIMIT_EXCEEDED")), class: ClassAccessNotApproved,
			requests: []string{"/v1/" + boundLocation + "?readMask=name"}},
		"not approved": {connection: "all", answer: reply(http.StatusTooManyRequests,
			errorObject(429, "RESOURCE_EXHAUSTED", "RATE_LIMIT_EXCEEDED")), class: ClassAccessNotApproved,
			requests: []string{"/v1/accounts?pageSize=1"}},
		"not approved with a quota of zero": {connection: "all", answer: reply(http.StatusTooManyRequests,
			zeroQuotaObject()), class: ClassAccessNotApproved, requests: []string{"/v1/accounts?pageSize=1"}},
		"expired consent": {connection: "all", token: func(int) (int, string) {
			return http.StatusBadRequest, `{"error":"invalid_grant","error_description":"` + detailCanary + `"}`
		}, class: provider.ClassConsentExpired},
		"api not enabled": {connection: "all", answer: reply(http.StatusForbidden,
			errorObject(403, "PERMISSION_DENIED", "SERVICE_DISABLED")), class: provider.ClassPermission,
			requests: []string{"/v1/accounts?pageSize=1"}},
		"no consent yet": {connection: "all", values: map[string]string{clientIDEnv: clientIDValue},
			class: provider.ClassConsentRequired},
		"scope missing": {connection: "all", token: func(n int) (int, string) {
			return http.StatusOK, `{"access_token":"` + accessPrefix + `1","token_type":"Bearer","expires_in":3600,` +
				`"scope":"https://www.googleapis.com/auth/indexing"}`
		}, class: provider.ClassScopeMissing},
		"service account key": {connection: "all", values: map[string]string{keyEnv: "synthetic-key-file"},
			class: provider.ClassCredentialUnusable},
	} {
		t.Run(name, func(t *testing.T) {
			env := newEnvironment(t, c.values)
			env.fake.tokenAnswer, env.fake.answer = c.token, c.answer
			if class := env.test(t, c.connection); class != c.class {
				t.Fatalf("class = %q, want %q", class, c.class)
			}
			calls, _ := env.fake.calls()
			var got []string
			for _, call := range calls {
				address := call.path
				if call.query != "" {
					address += "?" + call.query
				}
				got = append(got, address)
				if strings.Contains(call.full, accessPrefix) {
					t.Errorf("a token travelled in the URL: %s", call.full)
				}
			}
			if !reflect.DeepEqual(got, c.requests) {
				t.Errorf("requests = %v, want %v", got, c.requests)
			}
		})
	}
}

// The change spacing is ten changes per minute and location, shared by every credential, and one location
// does not hold another.
func TestChangeLimiterAllowsTenChangesPerMinutePerLocation(t *testing.T) {
	newEnvironment(t, nil)
	if changeInterval*10 != time.Minute {
		t.Fatalf("change interval %s does not make ten changes per minute", changeInterval)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := changeLimiter(boundLocation).Wait(ctx); err != nil {
		t.Fatalf("first change waited: %v", err)
	}
	var beyond *ratelimit.BeyondDeadlineError
	if err := changeLimiter(boundLocation).Wait(ctx); !errors.As(err, &beyond) ||
		beyond.Wait < changeInterval-time.Second || beyond.Wait > changeInterval {
		t.Fatalf("second change of the location = %v, want a wait of about %s", err, changeInterval)
	}
	if err := changeLimiter(otherLocation).Wait(ctx); err != nil {
		t.Errorf("a change of another location waited: %v", err)
	}
}
