package application

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/invokelog"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// The provider "sample" exists only in this test: its credential issues access tokens from a token endpoint
// behind a fake transport, and its one tool reads an API behind the same transport.

const (
	tokenSampleEndpoint = "https://login.sample.invalid/token"
	tokenSampleAPI      = "https://api.sample.invalid/items"
	tokenSampleAccess   = "synthetic-access-value"
	tokenSampleRefresh  = "synthetic-refresh-value"
)

type tokenSampleTransport struct {
	mu   sync.Mutex
	seen []string
}

func (s *tokenSampleTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	body := `{}`
	switch req.URL.String() {
	case tokenSampleEndpoint:
		body = `{"access_token":"` + tokenSampleAccess + `","token_type":"Bearer","expires_in":3600}`
	case tokenSampleAPI:
		s.mu.Lock()
		s.seen = append(s.seen, req.URL.Path)
		s.mu.Unlock()
	default:
		return nil, errors.New("unexpected request")
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Request: req,
		Header: http.Header{"Content-Type": {"application/json"}}}, nil
}

func tokenSampleCore(t *testing.T, values map[string]string) (*Core, *tokenSampleTransport) {
	t.Helper()
	transport := &tokenSampleTransport{}
	client := provider.NoRedirectClient(5*time.Second, transport)
	token := &config.TokenMetadata{Scopes: map[string][]string{"sample.items.get": {"items.read"}}}
	grant := func(ctx context.Context, req provider.TokenRequest) (provider.Token, error) {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenSampleEndpoint,
			strings.NewReader("grant_type=refresh_token&refresh_token="+req.Secrets.RefreshToken))
		if err != nil {
			return provider.Token{}, err
		}
		resp, err := client.Do(httpReq)
		if err != nil {
			return provider.Token{}, provider.Transport("access token", "the token endpoint", err)
		}
		defer resp.Body.Close()
		return provider.ReadTokenAnswer("access token", resp, time.Now())
	}
	options := provider.TokenOptions{
		Token: *token, Cache: provider.NewTokenCache(time.Now),
		Grants: map[config.TokenProcedure]provider.TokenGrant{config.TokenConsent: grant},
	}
	handler := func(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
		red *redact.Redactor, _ json.RawMessage) (any, error) {
		source, err := provider.OpenTokenSource(ctx, resolved, secrets, red, options)
		if err != nil {
			return nil, err
		}
		resp, err := source.Read(ctx, client, "sample.items.get", func(access string) (*http.Request, error) {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, tokenSampleAPI, nil)
			if err == nil {
				req.Header.Set("Authorization", "Bearer "+access)
			}
			return req, err
		})
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		return map[string]any{"ok": true}, nil
	}

	registry := capability.NewRegistry()
	if err := registry.RegisterProvider(config.ProviderMetadata{
		ID: "sample", Name: "Sample", SecretRoles: config.TokenSecretRoles(), Token: token,
	}, nil); err != nil {
		t.Fatal(err)
	}
	read := testDescriptor("sample.items.get", capability.EffectRead, capability.ConfirmationNone)
	read.Provider = "sample"
	if err := registry.Register("sample", capability.Operation{Descriptor: read, Handler: handler}); err != nil {
		t.Fatal(err)
	}
	cfg := config.New(registry)
	cfg.Services["sample"] = config.Service{Provider: "sample", BaseURL: "https://api.sample.invalid"}
	cfg.Credentials["login"] = config.Credential{Provider: "sample", Type: config.CredentialTypeKeyring}
	cfg.Connections["work"] = config.Connection{Service: "sample", Credential: "login",
		Effects: []config.Effect{config.EffectRead}}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() = %v", err)
	}
	env := func(name string) string {
		for role, value := range values {
			if name == secret.DerivedEnvName("login", role) {
				return value
			}
		}
		return ""
	}
	redactor := &redact.Redactor{}
	return New(registry, cfg, secret.NewWith(env, nil, nil, redactor), redactor), transport
}

// The invocation log of a call that obtains and uses an access token holds neither the access token nor
// the refresh token, and a call before the first consent is logged with its own code.
func TestTokenInvokeLogHoldsNoToken(t *testing.T) {
	dir := t.TempDir()
	core, transport := tokenSampleCore(t, map[string]string{
		config.RoleClientID: "sample-client-id", config.RoleRefreshToken: tokenSampleRefresh,
	})
	core.SetInvokeLog(invokelog.New(dir, 90), "cli", nil)
	if _, err := core.Invoke(context.Background(), InvokeRequest{
		Operation: "sample.items.get", Connection: "work", Arguments: json.RawMessage(`{"id":"7"}`),
	}); err != nil {
		t.Fatalf("Invoke() = %v", err)
	}
	if len(transport.seen) != 1 {
		t.Fatalf("API calls = %v, want 1", transport.seen)
	}

	unconsented, _ := tokenSampleCore(t, map[string]string{config.RoleClientID: "sample-client-id"})
	unconsented.SetInvokeLog(invokelog.New(dir, 90), "cli", nil)
	_, err := unconsented.Invoke(context.Background(), InvokeRequest{
		Operation: "sample.items.get", Connection: "work", Arguments: json.RawMessage(`{"id":"7"}`),
	})
	if code := ErrorCode(err); code != CodeConsentRequired {
		t.Fatalf("Invoke() before consent = %v (%s), want %s", err, code, CodeConsentRequired)
	}

	files, err := filepath.Glob(filepath.Join(dir, "logs", "*.jsonl"))
	if err != nil || len(files) != 1 {
		t.Fatalf("day files = %v, %v", files, err)
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{tokenSampleAccess, tokenSampleRefresh, "sample-client-id"} {
		if strings.Contains(string(data), value) {
			t.Errorf("the invocation log holds %s: %s", value, data)
		}
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("log entries = %d, want 2", len(lines))
	}
	var entry invokelog.Entry
	if err := json.Unmarshal([]byte(lines[1]), &entry); err != nil || entry.Result != string(CodeConsentRequired) {
		t.Fatalf("second entry = %+v (%v), want the consent-required result", entry, err)
	}
}

func TestTokenClassesHaveTheirCodes(t *testing.T) {
	for class, code := range map[provider.Class]string{
		provider.ClassConsentRequired:    "consent-required",
		provider.ClassConsentExpired:     "consent-expired",
		provider.ClassScopeMissing:       "scope-missing",
		provider.ClassCredentialUnusable: "credential-unusable",
	} {
		if got := ErrorCode(&provider.Error{Class: class, Op: "access token"}); string(got) != code {
			t.Errorf("ErrorCode(%s) = %s, want %s", class, got, code)
		}
	}
}
