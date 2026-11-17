package googletagmanager

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
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
	testClientID    = "synthetic-client-one"
	testOtherClient = "synthetic-client-two"
	testRefresh     = "synthetic-refresh-one"
	testAccess      = "synthetic-access-one"
	testEcho        = "synthetic-provider-echo"
)

// fakeGoogle answers the token endpoint and the API. api returns status, content type, body, and headers of
// one API request; a nil api answers 200 with {}.
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

// spacing records the waits of a limiter on a clock that only its sleep advances.
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
}

// newHarness wires one connection with a consent credential, an optional target, and the fake Google.
func newHarness(t *testing.T, target, clientID string) *harness {
	t.Helper()
	h := &harness{google: &fakeGoogle{}, red: &redact.Redactor{}, spacing: &spacing{now: time.Unix(1_700_000_000, 0)}}
	transport, tokenCache = h.google, provider.NewTokenCache(time.Now)
	t.Cleanup(func() { transport, tokenCache = nil, nil })
	restore := limiters.Replace("client:"+clientID, ratelimit.New(minInterval, h.spacing.Now, h.spacing.Sleep))
	t.Cleanup(restore)
	h.resolved = resolve(t, target, "GTM_TEST_CLIENT")
	env := map[string]string{"GTM_TEST_CLIENT": clientID, "GTM_TEST_REFRESH": testRefresh}
	h.secrets = secret.NewWith(func(name string) string { return env[name] }, nil, nil, h.red)
	return h
}

func resolve(t *testing.T, target, clientVariable string) *config.Resolved {
	t.Helper()
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	cfg := config.New(reg)
	cfg.Services = map[string]config.Service{"gtm": {Provider: Provider}}
	cfg.Credentials = map[string]config.Credential{"cred": {Provider: Provider, Type: config.CredentialTypeEnv,
		Values: map[string]string{config.RoleClientID: clientVariable, config.RoleRefreshToken: "GTM_TEST_REFRESH"}}}
	cfg.Connections = map[string]config.Connection{"conn": {Service: "gtm", Credential: "cred", Target: target,
		Tools: allToolIDs()}}
	resolved, err := cfg.Resolve("conn", "")
	if err != nil {
		t.Fatal(err)
	}
	return resolved
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

func (h *harness) paths(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, req := range h.google.seen() {
		if req.URL.Host != "tagmanager.googleapis.com" || req.Method != http.MethodGet {
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

func TestRegisterDeclaresTokenScopesAndTargets(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, ok := reg.ProviderMetadata(Provider)
	if !ok || metadata.Token == nil || len(reg.Provider(Provider)) != 72 || metadata.DocPath() != "google.tag-manager/setup.md" {
		t.Fatalf("metadata = %+v, %v", metadata, ok)
	}
	for _, d := range reg.Provider(Provider) {
		if d.Risk.Effect != capability.EffectRead {
			continue
		}
		if scopes := metadata.Token.Scopes[d.ID]; len(scopes) != 1 || scopes[0] != readonlyScope {
			t.Errorf("%s scopes = %v, want only %s", d.ID, scopes, readonlyScope)
		}
		if d.Risk.Idempotency != capability.IdempotencySafe ||
			d.Risk.Confirmation != capability.ConfirmationNone || !d.Risk.OpenWorld || d.Risk.DataSensitivity == "" {
			t.Errorf("%s risk = %+v", d.ID, d.Risk)
		}
	}
	oauth, ok := reg.OAuth(Provider)
	if !ok || oauth.TokenURL != googleapi.OAuth.TokenURL || oauth.Params["access_type"] != "offline" || oauth.NarrowRefresh {
		t.Errorf("OAuth = %+v, %v", oauth, ok)
	}
	for _, raw := range []string{"accounts/1", "accounts/1000001/containers/2000002"} {
		if err := validateTarget(raw); err != nil {
			t.Errorf("validateTarget(%q) = %v", raw, err)
		}
	}
	for _, raw := range []string{"", "accounts/01", "accounts/1/containers/2/workspaces/3", "accounts/x", "1"} {
		if validateTarget(raw) == nil {
			t.Errorf("validateTarget(%q) = nil, want a refusal", raw)
		}
	}
	if validateBaseURL("https://tagmanager.googleapis.com/") != nil || validateBaseURL("https://example.invalid") == nil {
		t.Error("validateBaseURL accepts another origin or refuses the fixed one")
	}
}

func TestAccountsListReadsThreeBatchesWithCursor(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		switch req.URL.Query().Get("pageToken") {
		case "":
			return 200, `{"account":[{"accountId":"1","path":"accounts/1","fingerprint":"11"}],"nextPageToken":"batch-two"}`, nil
		case "batch-two":
			return 200, `{"account":[{"accountId":"2","path":"accounts/2"}],"nextPageToken":"batch-three"}`, nil
		}
		return 200, `{"account":[{"accountId":"3","path":"accounts/3","name":"Third","shareData":true,` +
			`"tagManagerUrl":"https://tagmanager.google.com/#/admin/accounts/3","features":{"supportMultipleContainers":true}}]}`, nil
	}
	var ids []string
	cursor := ""
	for i := 0; i < 3; i++ {
		args := `{"include_google_tags":true}`
		if cursor != "" {
			args = `{"include_google_tags":true,"cursor":"` + cursor + `"}`
		}
		out, err := h.call(t, handleAccountsList, args)
		if err != nil {
			t.Fatalf("batch %d: %v", i+1, err)
		}
		for _, entry := range out["accounts"].([]any) {
			ids = append(ids, entry.(map[string]any)["accountId"].(string))
		}
		cursor, _ = out["next_cursor"].(string)
		if i == 2 {
			last := out["accounts"].([]any)[0].(map[string]any)
			if last["shareData"] != true || last["features"].(map[string]any)["supportMultipleContainers"] != true {
				t.Errorf("third account = %v", last)
			}
		}
	}
	if strings.Join(ids, ",") != "1,2,3" || cursor != "" {
		t.Errorf("ids = %v, cursor = %q, want 1,2,3 and the end", ids, cursor)
	}
	want := []string{"/tagmanager/v2/accounts?includeGoogleTags=true",
		"/tagmanager/v2/accounts?includeGoogleTags=true&pageToken=batch-two",
		"/tagmanager/v2/accounts?includeGoogleTags=true&pageToken=batch-three"}
	if got := h.paths(t); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("requests = %v, want %v", got, want)
	}
	if h.google.grants != 1 {
		t.Errorf("token grants = %d, want 1 shared by the three calls", h.google.grants)
	}
}

func TestManipulatedCursorIsRefusedBeforeAnyRequest(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"container":[{"containerId":"5"}],"nextPageToken":"batch-two"}`, nil
	}
	out, err := h.call(t, handleContainersList, `{"account":"accounts/1"}`)
	if err != nil {
		t.Fatal(err)
	}
	cursor := out["next_cursor"].(string)
	before := len(h.google.seen())
	for name, args := range map[string]string{
		"altered":             `{"account":"accounts/1","cursor":"` + cursor[:len(cursor)-1] + "A" + `"}`,
		"other account":       `{"account":"accounts/2","cursor":"` + cursor + `"}`,
		"garbage":             `{"account":"accounts/1","cursor":"not-a-cursor"}`,
		"other tool's cursor": `{"container":"accounts/1/containers/5","cursor":"` + cursor + `"}`,
	} {
		handler := handleContainersList
		if strings.Contains(args, "container\"") {
			handler = handleWorkspacesList
		}
		if _, err := h.call(t, handler, args); !isInvalidRequest(err) {
			t.Errorf("%s: err = %v, want invalid request", name, err)
		}
	}
	if after := len(h.google.seen()); after != before {
		t.Errorf("requests after refused cursors = %d, want %d", after, before)
	}
}

func TestEmptyListIsAnEmptyResult(t *testing.T) {
	h := newHarness(t, "", testClientID)
	out, err := handleWorkspacesList(context.Background(), h.resolved, h.secrets, h.red,
		json.RawMessage(`{"container":"accounts/1/containers/2"}`))
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(out)
	if string(encoded) != `{"workspaces":[]}` {
		t.Errorf("result = %s, want an empty list and no cursor", encoded)
	}
}

func TestContainersGetReturnsFingerprintAndBoundsText(t *testing.T) {
	h := newHarness(t, "", testClientID)
	long := strings.Repeat("n", maxNoteRunes+50)
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"path":"accounts/1/containers/2","accountId":"1","containerId":"2","name":"Site",` +
			`"publicId":"GTM-TEST","usageContext":["web"],"domainName":["example.invalid"],"notes":"` + long + `",` +
			`"tagIds":["GT-TEST"],"features":{"supportWorkspaces":true},"taggingServerUrls":["https://tag.example.invalid"],` +
			`"fingerprint":"1700000000000","tagManagerUrl":"https://tagmanager.google.com/#/container/accounts/1/containers/2",` +
			`"unknownMember":"dropped"}`, nil
	}
	out, err := h.call(t, handleContainersGet, `{"container":"accounts/1/containers/2"}`)
	if err != nil {
		t.Fatal(err)
	}
	if out["fingerprint"] != "1700000000000" || out["publicId"] != "GTM-TEST" || out["unknownMember"] != nil {
		t.Errorf("container = %v", out)
	}
	if notes := out["notes"].(string); len([]rune(notes)) != maxNoteRunes || !strings.HasSuffix(notes, "…") {
		t.Errorf("notes were not cut to %d runes", maxNoteRunes)
	}
	if got := h.paths(t); len(got) != 1 || got[0] != "/tagmanager/v2/accounts/1/containers/2" {
		t.Errorf("requests = %v", got)
	}
}

func TestAccountBindingRefusesPathsOutsideBeforeAnyRequest(t *testing.T) {
	h := newHarness(t, "accounts/1000001", testClientID)
	for name, c := range map[string]struct {
		handler capability.Handler
		args    string
	}{
		"account":   {handleAccountsGet, `{"account":"accounts/1000002"}`},
		"list":      {handleContainersList, `{"account":"accounts/1000002"}`},
		"container": {handleContainersGet, `{"container":"accounts/1000002/containers/5"}`},
		"workspace": {handleWorkspacesGet, `{"workspace":"accounts/1000002/containers/5/workspaces/6"}`},
		"missing":   {handleContainersGet, `{}`},
	} {
		if _, err := h.call(t, c.handler, c.args); !isInvalidRequest(err) {
			t.Errorf("%s: err = %v, want invalid request", name, err)
		}
	}
	if len(h.google.seen()) != 0 || h.google.grants != 0 {
		t.Errorf("requests = %d, grants = %d, want none", len(h.google.seen()), h.google.grants)
	}
	if _, err := h.call(t, handleContainersList, `{}`); err != nil {
		t.Fatal(err)
	}
	if got := h.paths(t); len(got) != 1 || got[0] != "/tagmanager/v2/accounts/1000001/containers" {
		t.Errorf("requests = %v, want the containers of the bound account", got)
	}
}

func TestContainerBindingReadsTheTargetWithoutListingTheToken(t *testing.T) {
	h := newHarness(t, "accounts/1000001/containers/2000002", testClientID)
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		if strings.HasSuffix(req.URL.Path, "/containers/2000002") {
			return 200, `{"path":"accounts/1000001/containers/2000002","containerId":"2000002","fingerprint":"7"}`, nil
		}
		return 200, `{"path":"accounts/1000001","accountId":"1000001","fingerprint":"9"}`, nil
	}
	accounts, err := h.call(t, handleAccountsList, `{}`)
	if err != nil {
		t.Fatal(err)
	}
	containers, err := h.call(t, handleContainersList, `{}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.call(t, handleAccountsGet, `{}`); err != nil {
		t.Fatal(err)
	}
	if len(accounts["accounts"].([]any)) != 1 || len(containers["containers"].([]any)) != 1 ||
		accounts["next_cursor"] != nil || containers["next_cursor"] != nil {
		t.Errorf("accounts = %v, containers = %v, want the bound entries only", accounts, containers)
	}
	want := []string{"/tagmanager/v2/accounts/1000001", "/tagmanager/v2/accounts/1000001/containers/2000002",
		"/tagmanager/v2/accounts/1000001"}
	if got := h.paths(t); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("requests = %v, want %v and never the lists of the token", got, want)
	}
	for name, c := range map[string]struct {
		handler capability.Handler
		args    string
	}{
		"other container":     {handleContainersGet, `{"container":"accounts/1000001/containers/2000003"}`},
		"other workspaces":    {handleWorkspacesList, `{"container":"accounts/1000001/containers/2000003"}`},
		"other workspace":     {handleWorkspacesGet, `{"workspace":"accounts/1000001/containers/2000003/workspaces/1"}`},
		"other account":       {handleAccountsGet, `{"account":"accounts/1000009"}`},
		"cursor on the bound": {handleAccountsList, `{"cursor":"anything"}`},
	} {
		if _, err := h.call(t, c.handler, c.args); !isInvalidRequest(err) {
			t.Errorf("%s: err = %v, want invalid request", name, err)
		}
	}
	if got := len(h.google.seen()); got != 3 {
		t.Errorf("requests = %d, want no further one", got)
	}
}

func TestInvalidPathPatternsAreRefused(t *testing.T) {
	h := newHarness(t, "", testClientID)
	for _, args := range []string{`{"account":"accounts/01"}`, `{"account":"accounts/1/../2"}`,
		`{"account":"accounts/x"}`, `{"account":"accounts/1?alt=media"}`, `{"account":"accounts/1/containers/2"}`} {
		if _, err := h.call(t, handleAccountsGet, args); !isInvalidRequest(err) {
			t.Errorf("%s: err = %v, want invalid request", args, err)
		}
	}
	for _, args := range []string{`{"workspace":"accounts/1/containers/2"}`, `{"workspace":"accounts/1/containers/2/workspaces/a"}`} {
		if _, err := h.call(t, handleWorkspacesGet, args); !isInvalidRequest(err) {
			t.Errorf("%s: err = %v, want invalid request", args, err)
		}
	}
	if _, err := h.call(t, handleAccountsGet, `{}`); !isInvalidRequest(err) {
		t.Errorf("missing account on an unbound connection: err = %v", err)
	}
	if len(h.google.seen()) != 0 || h.google.grants != 0 {
		t.Errorf("requests = %d, grants = %d, want none", len(h.google.seen()), h.google.grants)
	}
}

func googleBody(code int, status, reason string) string {
	return `{"error":{"code":` + strconv.Itoa(code) + `,"message":"` + testEcho + `","status":"` + status + `",` +
		`"details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"` + reason + `","domain":"googleapis.com"}]}}`
}

func TestGoogleErrorsMapToClasses(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		body     string
		header   http.Header
		want     provider.Class
		requests int
	}{
		{"401", 401, googleBody(401, "UNAUTHENTICATED", "CREDENTIALS_MISSING"), nil, provider.ClassAuth, 2},
		{"403", 403, googleBody(403, "PERMISSION_DENIED", "ACCESS_TOKEN_SCOPE_INSUFFICIENT"), nil, provider.ClassPermission, 1},
		{"429 without Retry-After", 429, googleBody(429, "RESOURCE_EXHAUSTED", "RATE_LIMIT_EXCEEDED"), nil, provider.ClassRateLimited, 1},
		{"404 is not mapped", 404, googleBody(404, "NOT_FOUND", ""), nil, provider.ClassInvalidResponse, 1},
		{"500 is not mapped", 500, `oops`, nil, provider.ClassInvalidResponse, 1},
		{"unreadable body", 200, `{"account":[`, nil, provider.ClassInvalidResponse, 1},
		{"not JSON", 200, `<html></html>`, http.Header{"Content-Type": {"text/html"}}, provider.ClassInvalidResponse, 1},
		{"wrong member type", 200, `{"account":[{"shareData":"yes"}]}`, nil, provider.ClassInvalidResponse, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t, "", testClientID)
			h.google.api = func(*http.Request) (int, string, http.Header) {
				header := http.Header{}
				for k, v := range c.header {
					header[k] = v
				}
				return c.status, c.body, header
			}
			_, err := h.call(t, handleAccountsList, `{}`)
			if got := classOf(err); got != c.want {
				t.Fatalf("class = %q (%v), want %q", got, err, c.want)
			}
			if strings.Contains(err.Error(), testEcho) || strings.Contains(err.Error(), testAccess) {
				t.Errorf("message carries provider text or the token: %v", err)
			}
			if got := len(h.google.seen()); got != c.requests {
				t.Errorf("requests = %d, want %d", got, c.requests)
			}
			var failed *provider.Error
			if errors.As(err, &failed) && c.status >= 400 && (failed.Original == nil || failed.Original.Status != c.status) {
				t.Errorf("original = %+v, want the status", failed.Original)
			}
		})
	}
}

func TestRateLimitAnswerHoldsTheClientWithoutRetry(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 429, googleBody(429, "RESOURCE_EXHAUSTED", "RATE_LIMIT_EXCEEDED"), nil
	}
	if _, err := h.call(t, handleAccountsList, `{}`); classOf(err) != provider.ClassRateLimited {
		t.Fatalf("err = %v", err)
	}
	h.google.api = nil
	if _, err := h.call(t, handleAccountsList, `{}`); err != nil {
		t.Fatal(err)
	}
	waits := h.spacing.recorded()
	if len(waits) != 1 || waits[0] < minInterval {
		t.Errorf("waits = %v, want one wait of at least %s before the next request", waits, minInterval)
	}
	if got := len(h.google.seen()); got != 2 {
		t.Errorf("requests = %d, want 2: the rate-limited one is never repeated", got)
	}
}

func TestTimeoutIsItsOwnClass(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.fail = context.DeadlineExceeded
	_, err := h.call(t, handleAccountsGet, `{"account":"accounts/1"}`)
	if classOf(err) != provider.ClassTimeout {
		t.Fatalf("err = %v, want timeout", err)
	}
	if strings.Contains(err.Error(), "tagmanager.googleapis.com") || strings.Contains(err.Error(), testAccess) {
		t.Errorf("timeout message quotes the request: %v", err)
	}
}

func TestRequestsAreSpacedPerClientID(t *testing.T) {
	h := newHarness(t, "", testClientID)
	if _, err := h.call(t, handleAccountsGet, `{"account":"accounts/1"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := h.call(t, handleAccountsGet, `{"account":"accounts/1"}`); err != nil {
		t.Fatal(err)
	}
	if waits := h.spacing.recorded(); len(waits) != 1 || waits[0] != minInterval {
		t.Errorf("waits = %v, want one wait of %s between two requests of one client", waits, minInterval)
	}

	// A connection of another client keeps its own spacing.
	other := &spacing{now: time.Unix(1_700_000_000, 0)}
	t.Cleanup(limiters.Replace("client:"+testOtherClient, ratelimit.New(minInterval, other.Now, other.Sleep)))
	resolved := resolve(t, "", "GTM_TEST_OTHER")
	env := map[string]string{"GTM_TEST_OTHER": testOtherClient, "GTM_TEST_REFRESH": testRefresh}
	secrets := secret.NewWith(func(name string) string { return env[name] }, nil, nil, h.red)
	if _, err := handleAccountsGet(context.Background(), resolved, secrets, h.red, json.RawMessage(`{"account":"accounts/1"}`)); err != nil {
		t.Fatal(err)
	}
	if waits := other.recorded(); len(waits) != 0 {
		t.Errorf("other client waits = %v, want none", waits)
	}
	if waits := h.spacing.recorded(); len(waits) != 1 {
		t.Errorf("first client waits = %v, want still one", waits)
	}
}

func TestConnectionTest(t *testing.T) {
	t.Run("success lists the first batch of accounts", func(t *testing.T) {
		h := newHarness(t, "", testClientID)
		class, err := TestConnection(context.Background(), h.resolved, h.secrets, h.red)
		if err != nil || class != provider.ClassOK {
			t.Fatalf("TestConnection = %q, %v", class, err)
		}
		if got := h.paths(t); len(got) != 1 || got[0] != "/tagmanager/v2/accounts" || h.google.grants != 1 {
			t.Errorf("requests = %v, grants = %d", got, h.google.grants)
		}
		if h.red.Apply("token "+testAccess) == "token "+testAccess {
			t.Error("the access token is not registered with the redactor")
		}
	})
	t.Run("expired consent sends nothing to the API", func(t *testing.T) {
		h := newHarness(t, "", testClientID)
		h.google.token = func() (int, string) {
			return http.StatusBadRequest, `{"error":"invalid_grant","error_description":"` + testEcho + `"}`
		}
		class, err := TestConnection(context.Background(), h.resolved, h.secrets, h.red)
		if err != nil || class != provider.ClassConsentExpired {
			t.Fatalf("TestConnection = %q, %v, want consent-expired", class, err)
		}
		if len(h.google.seen()) != 0 {
			t.Errorf("API requests = %d, want none", len(h.google.seen()))
		}
	})
	t.Run("API not enabled", func(t *testing.T) {
		h := newHarness(t, "", testClientID)
		h.google.api = func(*http.Request) (int, string, http.Header) {
			return 403, googleBody(403, "PERMISSION_DENIED", "SERVICE_DISABLED"), nil
		}
		class, err := TestConnection(context.Background(), h.resolved, h.secrets, h.red)
		if err != nil || class != provider.ClassPermission {
			t.Fatalf("TestConnection = %q, %v, want permission", class, err)
		}
	})
	t.Run("container binding reads the container", func(t *testing.T) {
		h := newHarness(t, "accounts/1000001/containers/2000002", testClientID)
		class, err := TestConnection(context.Background(), h.resolved, h.secrets, h.red)
		if err != nil || class != provider.ClassOK {
			t.Fatalf("TestConnection = %q, %v", class, err)
		}
		if got := h.paths(t); len(got) != 1 || got[0] != "/tagmanager/v2/accounts/1000001/containers/2000002" {
			t.Errorf("requests = %v, want only the bound container", got)
		}
	})
	t.Run("missing consent sends nothing", func(t *testing.T) {
		h := newHarness(t, "", testClientID)
		h.secrets = secret.NewWith(func(name string) string {
			return map[string]string{"GTM_TEST_CLIENT": testClientID}[name]
		}, nil, nil, h.red)
		class, err := TestConnection(context.Background(), h.resolved, h.secrets, h.red)
		if err != nil || class != provider.ClassConsentRequired || h.google.grants != 0 {
			t.Fatalf("TestConnection = %q, %v after %d grants, want consent-required", class, err, h.google.grants)
		}
	})
}

func TestTokenNeverReachesURLOrMessage(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 401, googleBody(401, "UNAUTHENTICATED", "CREDENTIALS_MISSING"), nil
	}
	_, err := h.call(t, handleContainersGet, `{"container":"accounts/1/containers/2"}`)
	if err == nil {
		t.Fatal("want an auth failure")
	}
	h.paths(t)
	for _, text := range []string{err.Error(), h.red.Error(err)} {
		if strings.Contains(text, testAccess) || strings.Contains(text, testRefresh) || strings.Contains(text, testClientID) {
			t.Errorf("message carries a secret: %s", text)
		}
	}
	if got := h.red.Apply(testAccess + " " + testRefresh); strings.Contains(got, testAccess) || strings.Contains(got, testRefresh) {
		t.Errorf("redactor leaves a token: %s", got)
	}
}

// allToolIDs is the tools list of the test connection: it offers every tool, the listed-only ones included.
func allToolIDs() []string {
	var ids []string
	for _, d := range descriptors() {
		ids = append(ids, d.ID)
	}
	return ids
}
