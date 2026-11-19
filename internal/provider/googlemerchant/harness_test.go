package googlemerchant

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/provider/googleapi"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/ratelimit"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// Synthetic values: none of them has the shape of a real Google token, client ID, or key.
const (
	testClientID = "synthetic-client-one"
	testRefresh  = "synthetic-refresh-one"
	testAccess   = "synthetic-access-one"
)

// fakeGoogle answers the token endpoint and the API. api returns status, body, and headers of one API
// request; a nil api answers 200 with {}.
type fakeGoogle struct {
	mu       sync.Mutex
	api      func(*http.Request) (int, string, http.Header)
	token    func() (int, string)
	fail     error
	requests []*http.Request
	grants   int
}

func (f *fakeGoogle) RoundTrip(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if req.URL.String() == googleapi.OAuth.TokenURL {
		f.grants++
		status, body := http.StatusOK, `{"access_token":"`+testAccess+`","token_type":"Bearer","expires_in":3600}`
		if f.token != nil {
			status, body = f.token()
		}
		return answer(req, status, body, http.Header{"Content-Type": {"application/json"}}), nil
	}
	f.requests = append(f.requests, req)
	if f.fail != nil {
		return nil, f.fail
	}
	status, body, header := http.StatusOK, `{}`, http.Header{}
	if f.api != nil {
		status, body, header = f.api(req)
	}
	if header == nil {
		header = http.Header{}
	}
	if header.Get("Content-Type") == "" {
		header.Set("Content-Type", "application/json; charset=UTF-8")
	}
	return answer(req, status, body, header), nil
}

func answer(req *http.Request, status int, body string, header http.Header) *http.Response {
	return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body)), Request: req}
}

func (f *fakeGoogle) seen() []*http.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*http.Request(nil), f.requests...)
}

// spacing is a clock that only the sleep of a limiter advances.
type spacing struct {
	mu    sync.Mutex
	now   time.Time
	waits []time.Duration
}

func (s *spacing) Now() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.now
}

func (s *spacing) Sleep(_ context.Context, d time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.waits = append(s.waits, d)
	s.now = s.now.Add(d)
	return nil
}

func (s *spacing) recorded() []time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]time.Duration(nil), s.waits...)
}

type harness struct {
	google   *fakeGoogle
	resolved *config.Resolved
	secrets  *secret.Resolver
	red      *redact.Redactor
	spacing  *spacing
	lookups  *atomic.Int32
}

// newHarness wires one connection with a consent credential, optional targets, and the fake Google.
func newHarness(t *testing.T, targets ...string) *harness {
	t.Helper()
	h := &harness{google: &fakeGoogle{}, red: &redact.Redactor{}, spacing: &spacing{now: time.Unix(1_700_000_000, 0)},
		lookups: &atomic.Int32{}}
	transport, tokenCache = h.google, provider.NewTokenCache(time.Now)
	t.Cleanup(func() { transport, tokenCache = nil, nil })
	restore := limiters.Replace("client:"+testClientID, ratelimit.New(0, h.spacing.Now, h.spacing.Sleep))
	t.Cleanup(restore)
	h.resolved = resolve(t, targets)
	env := map[string]string{"MERCHANT_TEST_CLIENT": testClientID, "MERCHANT_TEST_REFRESH": testRefresh}
	h.secrets = secret.NewWith(func(name string) string {
		h.lookups.Add(1)
		return env[name]
	}, nil, nil, h.red)
	return h
}

func resolve(t *testing.T, targets []string) *config.Resolved {
	t.Helper()
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	cfg := config.New(reg)
	cfg.Services = map[string]config.Service{"merchant": {Provider: Provider}}
	cfg.Credentials = map[string]config.Credential{"cred": {Provider: Provider, Type: config.CredentialTypeEnv,
		Values: map[string]string{config.RoleClientID: "MERCHANT_TEST_CLIENT", config.RoleRefreshToken: "MERCHANT_TEST_REFRESH"}}}
	cfg.Connections = map[string]config.Connection{"conn": {Service: "merchant", Credential: "cred", Targets: targets,
		Tools: allToolIDs()}}
	resolved, err := cfg.Resolve("conn", "")
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func allToolIDs() []string {
	var ids []string
	for _, d := range descriptors() {
		ids = append(ids, d.ID)
	}
	return ids
}

func (h *harness) call(t *testing.T, handler capability.Handler, args string) (map[string]any, error) {
	t.Helper()
	out, err := handler(context.Background(), h.resolved, h.secrets, h.red, json.RawMessage(args))
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded, nil
}

// paths lists the requests so far as path and query, and checks that each one stayed on the fixed origin with
// the bearer token in the header only.
func (h *harness) paths(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, req := range h.google.seen() {
		if req.URL.Host != "merchantapi.googleapis.com" || req.URL.Scheme != "https" || req.Method != http.MethodGet {
			t.Errorf("request %s %s left the fixed origin or method", req.Method, req.URL.Host)
		}
		if req.Header.Get("Authorization") != "Bearer "+testAccess {
			t.Errorf("request %s carries no bearer token", req.URL.Path)
		}
		if strings.Contains(req.URL.String(), testAccess) || strings.Contains(req.URL.String(), testRefresh) {
			t.Errorf("request URL carries a token: %s", req.URL.Redacted())
		}
		path := req.URL.Path
		if req.URL.RawQuery != "" {
			path += "?" + req.URL.RawQuery
		}
		out = append(out, path)
	}
	return out
}

func (h *harness) untouched(t *testing.T) {
	t.Helper()
	if n := len(h.google.seen()); n != 0 || h.google.grants != 0 || h.lookups.Load() != 0 {
		t.Errorf("%d requests, %d grants, %d secret lookups; want none", n, h.google.grants, h.lookups.Load())
	}
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

func accountRow(id, name string) string {
	return `{"name":"accounts/` + id + `","accountId":"` + id + `","accountName":"` + name + `"}`
}
