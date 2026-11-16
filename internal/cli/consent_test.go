package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-connect/internal/vault"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// The provider "sample" of the conformance checks stands in for a provider whose credential issues access
// tokens. It exists only in these tests, with its token and revocation endpoints behind a fake transport.

const (
	consentTestClientID     = "sample-client-id"
	consentTestClientCanary = "synthetic-client-canary-2b7d"
	consentTestCode         = "synthetic-code-a41f"
	consentTestRefresh      = "synthetic-refresh-c90e"
	consentTestAccess       = "synthetic-access-7d13"
	consentTestPhrase       = "consent-test-phrase"
)

const consentConfig = `
version: 4
services:
  items:
    provider: sample
    base_url: https://api.sample.invalid
credentials:
  sample-keyring:
    provider: sample
    type: keyring
  sample-vault:
    provider: sample
    type: vault
  sample-env:
    provider: sample
    type: env
    values:
      client_id: SAMPLE_TEST_CLIENT_ID
      refresh_token: SAMPLE_TEST_REFRESH
connections:
  items-keyring:
    service: items
    credential: sample-keyring
  items-vault:
    service: items
    credential: sample-vault
  items-env:
    service: items
    credential: sample-env
`

// sampleEndpoints is the fake transport of the token and revocation endpoints.
type sampleEndpoints struct {
	mu       sync.Mutex
	requests []string
	revoke   int
}

func (e *sampleEndpoints) RoundTrip(req *http.Request) (*http.Response, error) {
	e.mu.Lock()
	e.requests = append(e.requests, req.URL.Path)
	revoke := e.revoke
	e.mu.Unlock()
	status, body := http.StatusOK, `{"access_token":"`+consentTestAccess+`","token_type":"Bearer","expires_in":600,`+
		`"refresh_token":"`+consentTestRefresh+`"}`
	if req.URL.Path == "/revoke" {
		status, body = revoke, ""
	}
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}},
		Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
}

func (e *sampleEndpoints) seen() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.requests...)
}

// consentRegistry registers the sample provider with access tokens and its consent endpoints.
func consentRegistry(t *testing.T, endpoints *sampleEndpoints) *capability.Registry {
	t.Helper()
	reg := capability.NewRegistry()
	metadata := conformantMetadata()
	metadata.SecretRoles, metadata.Token = config.TokenSecretRoles(), conformantToken()
	if err := reg.RegisterProvider(metadata, nil); err != nil {
		t.Fatal(err)
	}
	var operations []capability.Operation
	for _, descriptor := range conformantOperations() {
		operations = append(operations, capability.Operation{Descriptor: descriptor,
			Handler: func(context.Context, *config.Resolved, *secret.Resolver, *redact.Redactor, json.RawMessage) (any, error) {
				return map[string]any{}, nil
			}})
	}
	if err := reg.Register(metadata.ID, operations...); err != nil {
		t.Fatal(err)
	}
	if err := reg.RegisterOAuth(metadata.ID, provider.OAuth{
		AuthorizeURL: "https://login.sample.invalid/authorize", TokenURL: "https://login.sample.invalid/token",
		RevokeURL: "https://login.sample.invalid/revoke", Transport: endpoints,
	}); err != nil {
		t.Fatal(err)
	}
	return reg
}

func consentFixture(t *testing.T) string {
	t.Helper()
	t.Setenv("QCONNECT_CONFIG", "")
	t.Setenv("QCONNECT_HOME", "")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(consentConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// withPastedAddress answers the hidden prompt with what paste returns, once, and then waits like a person
// who types nothing more.
func withPastedAddress(t *testing.T, paste func(prompt string) string) {
	t.Helper()
	original := readHiddenLine
	var once sync.Once
	readHiddenLine = func(ctx context.Context, prompt string) (string, error) {
		answer := ""
		once.Do(func() { answer = paste(prompt) })
		if answer != "" {
			return answer, nil
		}
		<-ctx.Done()
		return "", ctx.Err()
	}
	t.Cleanup(func() { readHiddenLine = original })
}

// linkIn returns the authorization link the command printed.
func linkIn(t *testing.T, stderr string) *url.URL {
	t.Helper()
	for _, line := range strings.Split(stderr, "\n") {
		if strings.HasPrefix(line, "https://login.sample.invalid/authorize?") {
			link, err := url.Parse(line)
			if err != nil {
				t.Fatal(err)
			}
			return link
		}
	}
	t.Fatalf("stderr %q holds no authorization link", stderr)
	return nil
}

func runConsent(t *testing.T, opts *Options, reg *capability.Registry, stderr io.Writer, args ...string) (int, string) {
	t.Helper()
	var stdout bytes.Buffer
	code := run(newRootCommand(opts, reg), opts, args, &stdout, stderr)
	return code, stdout.String()
}

func assertNoConsentValue(t *testing.T, outputs ...string) {
	t.Helper()
	for _, out := range outputs {
		for _, value := range []string{consentTestCode, consentTestRefresh, consentTestAccess, consentTestClientCanary} {
			if strings.Contains(out, value) {
				t.Errorf("output %q carries a value of the consent", out)
			}
		}
	}
}

// From another device the person pastes the address the browser ended on. The refresh token lands in the
// keyring, and no output shows a code, a token or the client secret.
func TestCredentialConsentWithAPastedAddress(t *testing.T) {
	dir := consentFixture(t)
	store := secret.NewMemoryStore()
	_ = store.Set(secret.StoreKey("sample-keyring", config.RoleClientID), consentTestClientID)
	_ = store.Set(secret.StoreKey("sample-keyring", config.RoleClientSecret), consentTestClientCanary)
	opts := testOptionsIn(t, dir, store)
	endpoints := &sampleEndpoints{}
	stderr := &syncBuffer{}
	withPastedAddress(t, func(string) string {
		link := linkIn(t, stderr.String())
		q := link.Query()
		return q.Get("redirect_uri") + "?state=" + q.Get("state") + "&code=" + consentTestCode
	})

	code, stdout := runConsent(t, opts, consentRegistry(t, endpoints), stderr,
		"credential", "consent", "sample-keyring", "--config", configIn(dir))
	if code != exitOK {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, exitOK, stderr)
	}
	if !strings.Contains(stdout, "stored the consent of sample-keyring for items-keyring") {
		t.Errorf("stdout = %q, want the stored consent and its connection", stdout)
	}
	got, err := store.Get(context.Background(), secret.StoreKey("sample-keyring", config.RoleRefreshToken))
	if err != nil || got != consentTestRefresh {
		t.Errorf("stored refresh token = %q, %v, want the answered one", got, err)
	}
	link := linkIn(t, stderr.String())
	if link.Query().Get("code_challenge_method") != "S256" || link.Query().Get("client_id") != consentTestClientID {
		t.Errorf("link = %s, want the client ID and an S256 challenge", link)
	}
	assertNoConsentValue(t, stdout, stderr.String())
}

// On the same host the browser reaches the receiver itself; the vault, encrypted, takes the refresh token
// after the passphrase of this run.
func TestCredentialConsentOnTheSameHostIntoAnEncryptedVault(t *testing.T) {
	dir := consentFixture(t)
	seedEncryptedVault(t, dir, "sample-vault", config.RoleClientID, consentTestClientID, consentTestPhrase)
	withVaultPassphrase(t, offeringPassphrase(consentTestPhrase))
	endpoints := &sampleEndpoints{}
	stderr := &syncBuffer{}
	answered := make(chan int, 1)
	withPastedAddress(t, func(string) string {
		q := linkIn(t, stderr.String()).Query()
		go func() {
			resp, err := http.Get(q.Get("redirect_uri") + "?state=" + q.Get("state") + "&code=" + consentTestCode)
			if err != nil {
				answered <- 0
				return
			}
			_ = resp.Body.Close()
			answered <- resp.StatusCode
		}()
		return ""
	})

	code, stdout := runConsent(t, &Options{Redactor: &redact.Redactor{}}, consentRegistry(t, endpoints), stderr,
		"credential", "consent", "sample-vault", "--config", configIn(dir))
	if code != exitOK {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, exitOK, stderr)
	}
	select {
	case status := <-answered:
		if status != http.StatusOK {
			t.Errorf("receiver answered %d, want 200", status)
		}
	case <-time.After(5 * time.Second):
		t.Error("the receiver never answered")
	}
	got, found, _, err := vault.New(dir).Get("sample-vault", config.RoleRefreshToken, offeringPassphrase(consentTestPhrase))
	if err != nil || !found || got != consentTestRefresh {
		t.Errorf("vault refresh token = %q, %v, %v, want the answered one", got, found, err)
	}
	assertNoConsentValue(t, stdout, stderr.String())
}

// Without a terminal, for an env credential and for a credential without client ID the command ends before
// it prints a link or reaches the provider.
func TestCredentialConsentRefusals(t *testing.T) {
	dir := consentFixture(t)
	endpoints := &sampleEndpoints{}
	withPastedAddress(t, func(string) string { t.Error("a refused consent asked for an address"); return "" })
	for _, tt := range []struct {
		name, credential, code string
		interactive            bool
	}{
		{"no terminal", "sample-keyring", "admin-required", false},
		{"env credential", "sample-env", "usage", true},
		{"no client ID", "sample-keyring", "usage", true},
		{"unknown credential", "missing", "usage", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			withInteractive(t, tt.interactive)
			stderr := &syncBuffer{}
			code, stdout := runConsent(t, testOptionsIn(t, dir, nil), consentRegistry(t, endpoints), stderr,
				"credential", "consent", tt.credential, "--config", configIn(dir))
			if code != exitUsage || !strings.Contains(stderr.String(), tt.code) {
				t.Errorf("exit code = %d, stderr = %q, want %d and %s", code, stderr, exitUsage, tt.code)
			}
			if stdout != "" || strings.Contains(stderr.String(), "https://login.sample.invalid") {
				t.Errorf("a refused consent printed %q %q", stdout, stderr)
			}
		})
	}
	if len(endpoints.seen()) != 0 {
		t.Errorf("refused consents reached the provider: %v", endpoints.seen())
	}
}

// 'credential revoke' revokes and removes the refresh token; 'credential delete <credential> refresh_token'
// does the same, and a revocation the provider does not confirm still removes it, with a warning.
func TestCredentialRevokeAndDelete(t *testing.T) {
	for _, tt := range []struct {
		name      string
		args      []string
		status    int
		confirmed bool
	}{
		{"revoke confirmed", []string{"credential", "revoke", "sample-keyring"}, http.StatusOK, true},
		{"revoke unconfirmed", []string{"credential", "revoke", "sample-keyring"}, http.StatusServiceUnavailable, false},
		{"delete with failed revocation", []string{"credential", "delete", "sample-keyring", config.RoleRefreshToken},
			http.StatusServiceUnavailable, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := consentFixture(t)
			store := secret.NewMemoryStore()
			_ = store.Set(secret.StoreKey("sample-keyring", config.RoleClientID), consentTestClientID)
			_ = store.Set(secret.StoreKey("sample-keyring", config.RoleRefreshToken), consentTestRefresh)
			opts := testOptionsIn(t, dir, store)
			opts.Redactor.Add(consentTestRefresh)
			endpoints := &sampleEndpoints{revoke: tt.status}
			stderr := &syncBuffer{}
			args := append(append([]string(nil), tt.args...), "--yes", "--confirm", "--config", configIn(dir))
			code, stdout := runConsent(t, opts, consentRegistry(t, endpoints), stderr, args...)
			if code != exitOK {
				t.Fatalf("exit code = %d, want %d (stderr: %s)", code, exitOK, stderr)
			}
			if _, err := store.Get(context.Background(), secret.StoreKey("sample-keyring", config.RoleRefreshToken)); err == nil {
				t.Error("the refresh token is still stored")
			}
			if got := endpoints.seen(); len(got) != 1 || got[0] != "/revoke" {
				t.Errorf("requests = %v, want one revocation", got)
			}
			warned := strings.Contains(stderr.String(), "did not confirm the revocation")
			if warned == tt.confirmed {
				t.Errorf("stdout %q, stderr %q: want the warning only for an unconfirmed revocation", stdout, stderr)
			}
			assertNoConsentValue(t, stdout, stderr.String())
		})
	}
}

// Revoking needs a terminal like every credential command, and --confirm.
func TestCredentialRevokeGates(t *testing.T) {
	dir := consentFixture(t)
	store := secret.NewMemoryStore()
	_ = store.Set(secret.StoreKey("sample-keyring", config.RoleRefreshToken), consentTestRefresh)
	endpoints := &sampleEndpoints{revoke: http.StatusOK}

	withInteractive(t, false)
	stderr := &syncBuffer{}
	code, _ := runConsent(t, testOptionsIn(t, dir, store), consentRegistry(t, endpoints), stderr,
		"credential", "revoke", "sample-keyring", "--yes", "--confirm", "--config", configIn(dir))
	if code != exitUsage || !strings.Contains(stderr.String(), "admin-required") {
		t.Errorf("without a terminal: exit code = %d, stderr = %q, want admin-required", code, stderr)
	}

	withInteractive(t, true)
	stderr = &syncBuffer{}
	code, _ = runConsent(t, testOptionsIn(t, dir, store), consentRegistry(t, endpoints), stderr,
		"credential", "revoke", "sample-keyring", "--yes", "--config", configIn(dir))
	if code == exitOK || !strings.Contains(stderr.String(), "confirmation-required") {
		t.Errorf("without --confirm: exit code = %d, stderr = %q, want confirmation-required", code, stderr)
	}
	if len(endpoints.seen()) != 0 {
		t.Errorf("a refused revocation reached the provider: %v", endpoints.seen())
	}
	if _, err := store.Get(context.Background(), secret.StoreKey("sample-keyring", config.RoleRefreshToken)); err != nil {
		t.Error("a refused revocation removed the refresh token")
	}
}
