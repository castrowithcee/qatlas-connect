package googleads

import (
	"context"
	"encoding/json"
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

// The canary stands for text a failing Google echoes. No test reaches Google: every request, the token
// requests included, is answered by the fake below through the package transport.
const (
	detailCanary  = "provider-detail-canary-5e2a"
	triggerCanary = "provider-trigger-canary-9b41"
	refreshValue  = "synthetic-refresh-value-1"
	credential    = "ads-login"
)

// Synthetic account IDs of the tests.
const (
	managerID  = "1000000001"
	subManager = "1000000002"
	clientID   = "1000000003"
	otherID    = "1000000009"
	loginID    = "1000000000"
)

const (
	accessiblePath = "/v26/customers:listAccessibleCustomers"
)

func searchPath(id string) string { return "/v26/customers/" + id + "/googleAds:search" }

type seen struct {
	method, url, path, auth, login, developerToken, query, body, contentType string
}

type reply struct {
	status int
	body   string
	header http.Header
	err    error
}

// fakeGoogle answers the token endpoint and the Google Ads API. answer decides the reply of an API request;
// without it, or when it returns nil, a path without a route is the 404 Google would send.
type fakeGoogle struct {
	mu         sync.Mutex
	requests   []seen
	tokens     int
	tokenReply *reply
	routes     map[string]reply
	answer     func(s seen) *reply
}

func (f *fakeGoogle) RoundTrip(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	respond := func(r reply) (*http.Response, error) {
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
			return respond(*f.tokenReply)
		}
		f.tokens++
		return respond(reply{status: 200, body: fmt.Sprintf(`{"access_token":"synthetic-access-%d",`+
			`"token_type":"Bearer","expires_in":3600,"scope":"%s"}`, f.tokens, scopeAdwords)})
	}
	s := seen{method: req.Method, url: req.URL.String(), path: req.URL.Path, auth: req.Header.Get("Authorization"),
		login: req.Header.Get("login-customer-id"), developerToken: req.Header.Get("developer-token")}
	if req.Body != nil {
		var body struct {
			Query string `json:"query"`
		}
		raw, _ := io.ReadAll(req.Body)
		_ = json.Unmarshal(raw, &body)
		s.query = body.Query
		s.body = string(raw)
		s.contentType = req.Header.Get("Content-Type")
	}
	f.requests = append(f.requests, s)
	if req.URL.Scheme != "https" || req.URL.Host != "googleads.googleapis.com" {
		return nil, errors.New("unexpected host")
	}
	if f.answer != nil {
		if r := f.answer(s); r != nil {
			return respond(*r)
		}
	}
	if r, ok := f.routes[req.Method+" "+req.URL.Path]; ok {
		return respond(r)
	}
	return respond(reply{status: 404, body: `{"error":{"code":404,"message":"` + detailCanary + `","status":"NOT_FOUND"}}`})
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

// adsFailure is the REST error form of Google Ads with one GoogleAdsError of the given category and code. Its
// messages echo the canary.
func adsFailure(status int, rpcStatus, category, code, quotaDetails string) string {
	details := ""
	if quotaDetails != "" {
		details = `,"details":{"quotaErrorDetails":` + quotaDetails + `}`
	}
	return fmt.Sprintf(`{"error":{"code":%d,"message":"%s","status":"%s","details":[{"@type":`+
		`"type.googleapis.com/google.ads.googleads.v26.errors.GoogleAdsFailure","errors":[{"errorCode":{"%s":"%s"},`+
		`"message":"%s","trigger":{"stringValue":"%s"},"location":{"fieldPathElements":[{"fieldName":"query"}]}%s}],`+
		`"requestId":"synthetic-request-1"}]}}`, status, detailCanary, rpcStatus, category, code, detailCanary,
		triggerCanary, details)
}

// googleError is the plain Google error object with an optional ErrorInfo reason.
func googleError(status int, rpcStatus, reason string) string {
	details := ""
	if reason != "" {
		details = fmt.Sprintf(`,"details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"%s",`+
			`"domain":"googleapis.com","metadata":{"consumer":"%s"}}]`, reason, detailCanary)
	}
	return fmt.Sprintf(`{"error":{"code":%d,"message":"%s","status":"%s"%s}}`, status, detailCanary, rpcStatus, details)
}

func okReply(body string) reply { return reply{status: 200, body: body} }

// rows is a search answer with the given rows.
func rows(items ...string) reply {
	return okReply(`{"results":[` + strings.Join(items, ",") + `],"fieldMask":"x"}`)
}

// clientItem is a customer_client row below the queried account.
func clientItem(queried, id string, level int, manager bool) string {
	return fmt.Sprintf(`{"customerClient":{"resourceName":"customers/%s/customerClients/%s","clientCustomer":`+
		`"customers/%s","id":"%s","descriptiveName":"Client %s","currencyCode":"EUR","timeZone":"Europe/Berlin",`+
		`"level":"%d","manager":%t,"status":"ENABLED","appliedLabels":["customers/%s/labels/7"]}}`,
		queried, id, id, id, id, level, manager, queried)
}

// proofItem is the row of a membership read that shows the client.
func proofItem(id string) string { return `{"customerClient":{"id":"` + id + `"}}` }

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

func newEnvironment(t *testing.T, targets []string, options map[string]string) *environment {
	t.Helper()
	return newEnvironmentWith(t, targets, options, config.Connection{Effects: []config.Effect{config.EffectRead}})
}

// newMutationEnvironment is a connection whose tools list names all changing tools and that has no effects.
func newMutationEnvironment(t *testing.T, targets []string, options map[string]string) *environment {
	t.Helper()
	return newEnvironmentWith(t, targets, options,
		config.Connection{Tools: append(append([]string(nil), changeToolIDs...), notListedOnly...)})
}

func newEnvironmentWith(t *testing.T, targets []string, options map[string]string, conn config.Connection) *environment {
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
	e.cfg.Services = map[string]config.Service{"ads": {Provider: Provider, BaseURL: apiRoot, Options: options}}
	e.cfg.Credentials = map[string]config.Credential{credential: {Provider: Provider, Type: config.CredentialTypeKeyring}}
	conn.Service, conn.Credential, conn.Targets = "ads", credential, targets
	e.cfg.Connections = map[string]config.Connection{"work": conn}
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
		Operation: operation, Connection: "work", Arguments: json.RawMessage(arguments), Full: true})
	return string(response.Result), err
}

// invokeConfirmed is an invoke with the confirmation of this one call.
func (e *environment) invokeConfirmed(operation, arguments string) (string, error) {
	response, err := e.core.Invoke(context.Background(), application.InvokeRequest{
		Operation: operation, Connection: "work", Arguments: json.RawMessage(arguments), Full: true, Confirmed: true})
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

func isInvalidRequest(err error) bool {
	var invalid *provider.InvalidRequestError
	return errors.As(err, &invalid)
}

func decode(t *testing.T, out string) map[string]any {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("result %q: %v", out, err)
	}
	return got
}
