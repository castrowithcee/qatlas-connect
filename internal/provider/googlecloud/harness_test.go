package googlecloud

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

// The canaries stand for text a failing Google echoes. No test reaches Google: every request, the token
// requests included, is answered by the fake below through the package transport.
const (
	detailCanary = "provider-detail-canary-7c1d"
	refreshValue = "synthetic-refresh-value-1"
	credential   = "login"
)

type seen struct {
	method, url, auth, body string
	header                  http.Header
}

type reply struct {
	status int
	body   string
	header http.Header
	err    error
}

// fakeGoogle answers the token endpoint and the Resource Manager API. routes maps a path of the API to its
// answer, and a path without a route is a 404 that Google would send.
type fakeGoogle struct {
	mu       sync.Mutex
	requests []seen
	tokens   int
	// tokenReply, when set, replaces the answer of the token endpoint.
	tokenReply *reply
	routes     map[string]reply
	// hook, when set, decides the answer of an API request before the routes do.
	hook func(n int, req *http.Request) *reply
}

func (f *fakeGoogle) RoundTrip(req *http.Request) (*http.Response, error) {
	payload := ""
	if req.Body != nil {
		data, _ := io.ReadAll(req.Body)
		payload = string(data)
	}
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
			`"expires_in":3600,"scope":"%s %s"}`, f.tokens, scopeReadOnly, scopeCloudPlatform)})
	}
	f.requests = append(f.requests, seen{method: req.Method, url: req.URL.String(),
		auth: req.Header.Get("Authorization"), body: payload, header: req.Header.Clone()})
	if req.URL.Scheme != "https" || !strings.HasSuffix(req.URL.Host, ".googleapis.com") {
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

const projectBody = `{"name":"projects/415104041262","parent":"folders/2001","projectId":"my-project-123",` +
	`"state":"ACTIVE","displayName":"My Project","createTime":"2026-01-02T03:04:05Z","updateTime":"2026-02-03T04:05:06Z",` +
	`"etag":"W/\"abc\"","labels":{"env":"test"},"managementType":"SELF_MANAGED"}`

// environment is one configured connection with the fake behind it.
type environment struct {
	t        *testing.T
	fake     *fakeGoogle
	reg      *capability.Registry
	cfg      *config.Config
	core     *application.Core
	red      *redact.Redactor
	roles    map[string]string
	lookups  int
	sleeps   []time.Duration
	clientID string
}

func newEnvironment(t *testing.T, targets []string) *environment {
	t.Helper()
	e := &environment{t: t, fake: &fakeGoogle{routes: map[string]reply{}}, red: &redact.Redactor{},
		clientID: "client-" + t.Name()}
	e.roles = map[string]string{config.RoleClientID: e.clientID, config.RoleRefreshToken: refreshValue}
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
	e.cfg.Services = map[string]config.Service{"cloud": {Provider: Provider, BaseURL: apiRoot}}
	e.cfg.Credentials = map[string]config.Credential{credential: {Provider: Provider, Type: config.CredentialTypeKeyring}}
	e.cfg.Connections = map[string]config.Connection{"work": {Service: "cloud", Credential: credential,
		Effects: []config.Effect{config.EffectRead}, Targets: targets}}
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
	response, err := e.core.Invoke(context.Background(), application.InvokeRequest{
		Operation: operation, Connection: "work", Arguments: json.RawMessage(arguments)})
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

// serviceAccountKey returns a key file of a service account made for the test, which names the token endpoint
// of Google.
func serviceAccountKey(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	file, err := json.Marshal(map[string]string{
		"type": "service_account", "client_email": "synthetic@example-project.iam.gserviceaccount.com",
		"private_key":    string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})),
		"private_key_id": "synthetic-key-id", "token_uri": googleapi.OAuth.TokenURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(file)
}
