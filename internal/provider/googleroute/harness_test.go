package googleroute

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
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

// The canary stands for text a failing Google echoes. No test reaches Google: every request, the token
// requests included, is answered by the fake below through the package transport.
const (
	detailCanary = "provider-detail-canary-7c1d"
	refreshValue = "synthetic-refresh-value-1"
	credential   = "login"
)

// Names of the synthetic operations of the tests.
const (
	testProject  = "route-project-123"
	otherProject = "other-project-456"
	testName     = "projects/" + testProject + "/locations/us-central1/operations/op-1"
	testPath     = "/v1/" + testName
)

type seen struct {
	method, url, auth string
}

type reply struct {
	status int
	body   string
	header http.Header
	err    error
}

// fakeGoogle answers the token endpoint and the Route Optimization API. routes maps a path of the API to its
// answer, and a path without a route is a 404 that Google would send.
type fakeGoogle struct {
	mu         sync.Mutex
	requests   []seen
	tokens     int
	tokenReply *reply
	routes     map[string]reply
	hook       func(n int, req *http.Request) *reply
}

func (f *fakeGoogle) RoundTrip(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	answer := func(r reply) (*http.Response, error) {
		if r.err != nil {
			return nil, r.err
		}
		header := http.Header{"Content-Type": {"application/json; charset=UTF-8"}}
		for name, values := range r.header {
			header[name] = values
		}
		return &http.Response{StatusCode: r.status, Header: header, Body: io.NopCloser(strings.NewReader(r.body)),
			Request: req}, nil
	}
	if req.URL.String() == googleapi.OAuth.TokenURL {
		if f.tokenReply != nil {
			return answer(*f.tokenReply)
		}
		f.tokens++
		return answer(reply{status: 200, body: fmt.Sprintf(`{"access_token":"synthetic-access-%d","token_type":"Bearer",`+
			`"expires_in":3600,"scope":"%s"}`, f.tokens, strings.Join(allScopes(), " "))})
	}
	f.requests = append(f.requests, seen{method: req.Method, url: req.URL.String(), auth: req.Header.Get("Authorization")})
	if req.URL.Scheme != "https" || req.URL.Host != "routeoptimization.googleapis.com" {
		return nil, errors.New("unexpected host")
	}
	if f.hook != nil {
		if r := f.hook(len(f.requests), req); r != nil {
			return answer(*r)
		}
	}
	if r, ok := f.routes[req.URL.Path]; ok {
		return answer(r)
	}
	return answer(reply{status: 404, body: errorObject(404, "NOT_FOUND", "")})
}

func (f *fakeGoogle) api() []seen {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]seen(nil), f.requests...)
}

func (f *fakeGoogle) tokenRequests() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tokens
}

// errorObject is the Google error form with a message and details that echo a canary.
func errorObject(code int, status, reason string) string {
	details := ""
	if reason != "" {
		details = fmt.Sprintf(`,"details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"%s",`+
			`"domain":"googleapis.com","metadata":{"consumer":"%s"}}]`, reason, detailCanary)
	}
	return fmt.Sprintf(`{"error":{"code":%d,"message":"%s","status":"%s"%s}}`, code, detailCanary, status, details)
}

func okReply(body string) reply { return reply{status: 200, body: body} }

// environment is one configured connection with the fake behind it.
type environment struct {
	t       *testing.T
	fake    *fakeGoogle
	reg     *capability.Registry
	cfg     *config.Config
	core    *application.Core
	red     *redact.Redactor
	roles   map[string]string
	lookups int
	sleeps  []time.Duration
}

func newEnvironment(t *testing.T, targets []string) *environment {
	t.Helper()
	return newEnvironmentWith(t, targets, config.EffectRead)
}

func newEnvironmentWith(t *testing.T, targets []string, effects ...config.Effect) *environment {
	t.Helper()
	e := &environment{t: t, fake: &fakeGoogle{routes: map[string]reply{}}, red: &redact.Redactor{}}
	e.roles = map[string]string{config.RoleClientID: "client-" + t.Name(), config.RoleRefreshToken: refreshValue}
	previous := transport
	transport = e.fake
	t.Cleanup(func() { transport = previous })
	now := time.Now()
	t.Cleanup(limiters.Replace(credential, ratelimit.New(time.Second, func() time.Time { return now },
		func(_ context.Context, d time.Duration) error {
			e.sleeps = append(e.sleeps, d)
			now = now.Add(d)
			return nil
		})))

	e.reg = capability.NewRegistry()
	if err := Register(e.reg); err != nil {
		t.Fatalf("Register() = %v", err)
	}
	if err := e.reg.ValidateProfiles(); err != nil {
		t.Fatalf("ValidateProfiles() = %v", err)
	}
	e.cfg = config.New(e.reg)
	e.cfg.Services = map[string]config.Service{"route": {Provider: Provider, BaseURL: apiRoot}}
	e.cfg.Credentials = map[string]config.Credential{credential: {Provider: Provider, Type: config.CredentialTypeKeyring}}
	e.cfg.Connections = map[string]config.Connection{"work": {Service: "route", Credential: credential,
		Effects: effects, Targets: targets}}
	if err := e.cfg.Validate(); err != nil {
		t.Fatalf("Validate() = %v", err)
	}
	e.core = application.New(e.reg, e.cfg, e.secrets(), e.red)
	return e
}

func (e *environment) secrets() *secret.Resolver {
	env := func(name string) string {
		e.lookups++
		for role, value := range e.roles {
			if name == secret.DerivedEnvName(credential, role) {
				return value
			}
		}
		return ""
	}
	return secret.NewWith(env, nil, nil, e.red)
}

func (e *environment) invoke(operation, arguments string) (string, error) {
	return e.invokeConfirmed(operation, arguments, false)
}

func (e *environment) invokeConfirmed(operation, arguments string, confirmed bool) (string, error) {
	response, err := e.core.Invoke(context.Background(), application.InvokeRequest{
		Operation: operation, Connection: "work", Arguments: json.RawMessage(arguments), Confirmed: confirmed})
	return string(response.Result), err
}

func (e *environment) resolved() *config.Resolved {
	e.t.Helper()
	resolved, err := e.cfg.Resolve("work", "")
	if err != nil {
		e.t.Fatal(err)
	}
	return resolved
}

func (e *environment) testConnection() (provider.Class, error) {
	return TestConnection(context.Background(), e.resolved(), e.secrets(), e.red)
}

func classOf(err error) provider.Class {
	var failed *provider.Error
	if errors.As(err, &failed) {
		return failed.Class
	}
	return ""
}

// allScopes are the scopes of every tool of the provider, which the fake token endpoint grants.
func allScopes() []string {
	set := map[string]bool{}
	for _, scopes := range tokenMetadata.Scopes {
		for _, scope := range scopes {
			set[scope] = true
		}
	}
	out := make([]string, 0, len(set))
	for scope := range set {
		out = append(out, scope)
	}
	sort.Strings(out)
	return out
}
