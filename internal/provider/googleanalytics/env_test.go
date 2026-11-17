package googleanalytics

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
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

// No test reaches Google: the package transport answers the token endpoints and the Admin API. The canaries
// stand for the secrets of the credential, the tokens Google issues, and text a failing Google echoes.
const (
	clientIDValue  = "client-canary-5e1f.apps.googleusercontent.com"
	refreshValue   = "refresh-canary-8a2b"
	accessPrefix   = "ya29.access-canary-"
	impersonated   = "ya29.impersonated-canary"
	detailCanary   = "provider-detail-canary-7c1d"
	serviceAccount = "robot" + "@" + "sample.invalid"
)

// recorded is one request to the Admin API.
type recorded struct {
	method, host, path, rawQuery, url, auth, accept, contentType, body string
}

// fakeGoogle is the transport of every test. api answers the n-th Admin API request, counting from 1;
// tokenAnswer, when set, replaces a successful answer of the token endpoint.
type fakeGoogle struct {
	mu          sync.Mutex
	api         func(n int, r *http.Request) (*http.Response, error)
	tokenAnswer func(n int) (int, string)
	requests    []recorded
	tokenForms  []url.Values
	impersonate int
	urls        []string
}

func (f *fakeGoogle) RoundTrip(r *http.Request) (*http.Response, error) {
	f.mu.Lock()
	f.urls = append(f.urls, r.URL.String())
	f.mu.Unlock()
	switch r.URL.Host {
	case "oauth2.googleapis.com":
		body, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(body))
		f.mu.Lock()
		f.tokenForms = append(f.tokenForms, form)
		n := len(f.tokenForms)
		f.mu.Unlock()
		status, answer := http.StatusOK, fmt.Sprintf(`{"access_token":"%s%d","token_type":"Bearer","expires_in":3600}`,
			accessPrefix, n)
		if f.tokenAnswer != nil {
			status, answer = f.tokenAnswer(n)
		}
		return respond(r, status, "application/json", answer), nil
	case "iamcredentials.googleapis.com":
		f.mu.Lock()
		f.impersonate++
		f.mu.Unlock()
		return respond(r, http.StatusOK, "application/json",
			`{"accessToken":"`+impersonated+`","expireTime":"`+time.Now().Add(time.Hour).UTC().Format(time.RFC3339)+`"}`), nil
	case "analyticsadmin.googleapis.com", "analyticsdata.googleapis.com":
		var sent []byte
		if r.Body != nil {
			sent, _ = io.ReadAll(r.Body)
		}
		f.mu.Lock()
		f.requests = append(f.requests, recorded{method: r.Method, host: r.URL.Host, path: r.URL.EscapedPath(), rawQuery: r.URL.RawQuery,
			url: r.URL.String(), auth: r.Header.Get("Authorization"), accept: r.Header.Get("Accept"),
			contentType: r.Header.Get("Content-Type"), body: string(sent)})
		n := len(f.requests)
		f.mu.Unlock()
		if f.api == nil {
			return respond(r, http.StatusOK, "application/json", `{}`), nil
		}
		return f.api(n, r)
	}
	return nil, errors.New("unexpected host")
}

func (f *fakeGoogle) seen() ([]recorded, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]recorded(nil), f.requests...), len(f.tokenForms)
}

func respond(r *http.Request, status int, contentType, body string) *http.Response {
	header := http.Header{}
	if contentType != "" {
		header.Set("Content-Type", contentType)
	}
	return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body)), Request: r}
}

// always answers every Admin API request the same way.
func always(status int, body string) func(int, *http.Request) (*http.Response, error) {
	return func(_ int, r *http.Request) (*http.Response, error) {
		return respond(r, status, "application/json", body), nil
	}
}

// errorObject is the Google error form with a message and details that echo a canary.
func errorObject(code int, status, reason string) string {
	return fmt.Sprintf(`{"error":{"code":%d,"message":"%s","status":"%s","details":[{"@type":`+
		`"type.googleapis.com/google.rpc.ErrorInfo","reason":"%s","domain":"googleapis.com",`+
		`"metadata":{"service":"%s"}}]}}`, code, detailCanary, status, reason, detailCanary)
}

// serviceAccountKey is one generated key file, shared by every test of the package.
var serviceAccountKey = sync.OnceValue(func() string {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		panic(err)
	}
	file, _ := json.Marshal(map[string]string{
		"type": "service_account", "client_email": serviceAccount, "private_key_id": "key-id-1",
		"private_key": string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})),
		"token_uri":   "https://oauth2.googleapis.com/token",
	})
	return string(file)
})

// testEnv is a configured core whose credentials resolve from canary variables.
type testEnv struct {
	fake   *fakeGoogle
	reg    *capability.Registry
	cfg    *config.Config
	core   *application.Core
	red    *redact.Redactor
	reads  *atomic.Int32
	waits  *[]time.Duration
	lookup func(string) string
}

func registry(t *testing.T) *capability.Registry {
	t.Helper()
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatalf("Register() = %v", err)
	}
	if err := reg.ValidateProfiles(); err != nil {
		t.Fatalf("ValidateProfiles() = %v", err)
	}
	return reg
}

func newEnv(t *testing.T, fake *fakeGoogle) *testEnv {
	t.Helper()
	previousTransport, previousTokens := transport, tokens
	transport, tokens = fake, provider.NewTokenCache(time.Now)
	t.Cleanup(func() { transport, tokens = previousTransport, previousTokens })
	waits := &[]time.Duration{}
	var mu sync.Mutex
	for _, key := range []string{"login\x00", "robot\x00", "login\x00" + serviceAccount} {
		t.Cleanup(limiters.Replace(key, ratelimit.New(0, time.Now, func(_ context.Context, d time.Duration) error {
			mu.Lock()
			defer mu.Unlock()
			*waits = append(*waits, d)
			return nil
		})))
	}

	reg := registry(t)
	cfg := config.New(reg)
	cfg.Services["ga"] = config.Service{Provider: Provider, BaseURL: adminRoot}
	cfg.Credentials["login"] = config.Credential{Provider: Provider, Type: config.CredentialTypeEnv,
		Values: map[string]string{config.RoleClientID: "GA_CLIENT_ID", config.RoleRefreshToken: "GA_REFRESH"}}
	cfg.Credentials["robot"] = config.Credential{Provider: Provider, Type: config.CredentialTypeEnv,
		Values: map[string]string{config.RoleServiceAccountKey: "GA_KEY"}}
	read := []config.Effect{config.EffectRead}
	cfg.Connections["read"] = config.Connection{Service: "ga", Credential: "login", Effects: read}
	cfg.Connections["account"] = config.Connection{Service: "ga", Credential: "login", Effects: read,
		Targets: []string{"accounts/100"}}
	cfg.Connections["property"] = config.Connection{Service: "ga", Credential: "login", Effects: read,
		Targets: []string{"properties/200"}}
	cfg.Connections["mixed"] = config.Connection{Service: "ga", Credential: "login", Effects: read,
		Targets: []string{"accounts/100", "properties/901"}}
	// The write connections hold the changing effects. delete is reachable only where tools names it, which replaces effects.
	changing := []config.Effect{config.EffectRead, config.EffectCreate, config.EffectUpdate, config.EffectDelete}
	cfg.Connections["write"] = config.Connection{Service: "ga", Credential: "login", Effects: changing}
	for name, targets := range map[string][]string{"wfree": nil, "waccount": {"accounts/100"},
		"wproperty": {"properties/200"}, "wmixed": {"accounts/100", "properties/901"}} {
		cfg.Connections[name] = config.Connection{Service: "ga", Credential: "login", Targets: targets,
			Tools: []string{propertiesGetID, propertiesCreateID, propertiesUpdateID, propertiesDeleteID}}
	}
	listed := []string{accountsDeleteID}
	cfg.Connections["write-account"] = config.Connection{Service: "ga", Credential: "login", Effects: changing,
		Targets: []string{"accounts/100"}}
	cfg.Connections["delete-unlisted"] = config.Connection{Service: "ga", Credential: "login", Effects: changing,
		Targets: []string{"accounts/100"}}
	cfg.Connections["delete-account"] = config.Connection{Service: "ga", Credential: "login",
		Targets: []string{"accounts/100"}, Tools: listed}
	cfg.Connections["delete-two"] = config.Connection{Service: "ga", Credential: "login",
		Targets: []string{"accounts/100", "accounts/101"}, Tools: listed}
	cfg.Connections["delete-open"] = config.Connection{Service: "ga", Credential: "login", Tools: listed}
	cfg.Connections["delete-mixed"] = config.Connection{Service: "ga", Credential: "login",
		Targets: []string{"accounts/100", "properties/200"}, Tools: listed}
	cfg.Connections["delete-property"] = config.Connection{Service: "ga", Credential: "login",
		Targets: []string{"properties/200"}, Tools: listed}
	cfg.Connections["robot"] = config.Connection{Service: "ga", Credential: "robot", Effects: read}
	cfg.Connections["impersonate"] = config.Connection{Service: "ga", Credential: "login", Effects: read,
		ServiceAccount: serviceAccount}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() = %v", err)
	}
	reads := &atomic.Int32{}
	values := map[string]string{"GA_CLIENT_ID": clientIDValue, "GA_REFRESH": refreshValue}
	lookup := func(name string) string {
		reads.Add(1)
		if name == "GA_KEY" {
			return serviceAccountKey()
		}
		return values[name]
	}
	red := &redact.Redactor{}
	return &testEnv{fake: fake, reg: reg, cfg: cfg, red: red, reads: reads, waits: waits, lookup: lookup,
		core: application.New(reg, cfg, secret.NewWith(lookup, nil, nil, red), red)}
}

func (e *testEnv) secrets() *secret.Resolver { return secret.NewWith(e.lookup, nil, nil, e.red) }

func (e *testEnv) resolved(t *testing.T, connection string) *config.Resolved {
	t.Helper()
	resolved, err := e.cfg.Resolve(connection, "")
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func (e *testEnv) invoke(connection, arguments string) (string, error) {
	response, err := e.core.Invoke(context.Background(), application.InvokeRequest{
		Operation: summariesID, Connection: connection, Arguments: json.RawMessage(arguments)})
	return string(response.Result), err
}

func (e *testEnv) test(t *testing.T, connection string) provider.Class {
	t.Helper()
	resolved := e.resolved(t, connection)
	class, err := e.reg.TestConnection(context.Background(), resolved, e.secrets(), e.red)
	if err != nil {
		t.Fatalf("TestConnection(%s) error = %v", connection, err)
	}
	return class
}

// open opens a client of the connection the way a handler does.
func (e *testEnv) open(t *testing.T, connection string) *client {
	t.Helper()
	resolved := e.resolved(t, connection)
	c, err := open(secret.ForConnection(context.Background(), resolved), resolved, e.secrets(), e.red)
	if err != nil {
		t.Fatalf("open(%s) = %v", connection, err)
	}
	return c
}

func classOf(err error) provider.Class {
	var failed *provider.Error
	if errors.As(err, &failed) {
		return failed.Class
	}
	return ""
}

// leaks reports the first secret or provider text that text holds.
func leaks(text string) string {
	for _, canary := range []string{clientIDValue, refreshValue, accessPrefix, impersonated, detailCanary,
		"PRIVATE KEY"} {
		if strings.Contains(text, canary) {
			return canary
		}
	}
	return ""
}
