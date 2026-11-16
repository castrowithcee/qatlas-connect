package tui

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/connlog"
	"github.com/castrowithcee/qatlas-connect/internal/manage"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// The provider "sample" exists only in these tests: its credential issues access tokens, and its
// revocation endpoint sits behind a fake transport.

const consentRefreshCanary = "synthetic-refresh-4c8a"

var consentTestProvider = config.ProviderMetadata{
	ID: "sample", Name: "Sample", SecretRoles: config.TokenSecretRoles(),
	Tools: []config.ToolMetadata{{ID: "sample.items.get", Effect: config.EffectRead}},
	Token: &config.TokenMetadata{Scopes: map[string][]string{"sample.items.get": {"items.read"}}},
}

type consentTestCatalog struct{}

func (consentTestCatalog) ProviderMetadata(id string) (config.ProviderMetadata, bool) {
	return consentTestProvider, id == consentTestProvider.ID
}
func (consentTestCatalog) ProviderMetadataAll() []config.ProviderMetadata {
	return []config.ProviderMetadata{consentTestProvider}
}

// revokeEndpoint answers every revocation with status and counts them.
type revokeEndpoint struct {
	mu     sync.Mutex
	calls  int
	status int
}

func (e *revokeEndpoint) RoundTrip(req *http.Request) (*http.Response, error) {
	e.mu.Lock()
	e.calls++
	e.mu.Unlock()
	return &http.Response{StatusCode: e.status, Body: io.NopCloser(strings.NewReader("")), Request: req,
		Header: http.Header{}}, nil
}

func (e *revokeEndpoint) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls
}

// consentModel builds the editor over a keyring credential "held" of the sample provider that holds a client
// ID and a refresh token, used by the connection "items" when used is set.
func consentModel(t *testing.T, status int, used bool) (*Model, *config.Store, *secret.MemoryStore, *revokeEndpoint) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "qconnect")
	store := config.NewStore(filepath.Join(dir, "config.yaml"), consentTestCatalog{})
	cfg := store.New()
	cfg.Services = map[string]config.Service{"items": {Provider: "sample", BaseURL: "https://api.sample.invalid"}}
	mustNoError(t, cfg.SetCredential("held", config.Credential{Provider: "sample", Type: config.CredentialTypeKeyring}))
	if used {
		cfg.Connections = map[string]config.Connection{"items": {Service: "items", Credential: "held"}}
	}
	mustNoError(t, store.Save(cfg))

	secrets, mem := newResolver(t, dir, nil)
	mustNoError(t, mem.Set(secret.StoreKey("held", config.RoleClientID), "sample-client-id"))
	mustNoError(t, mem.Set(secret.StoreKey("held", config.RoleRefreshToken), consentRefreshCanary))
	endpoint := &revokeEndpoint{status: status}
	oauth := provider.OAuth{
		AuthorizeURL: "https://login.sample.invalid/authorize", TokenURL: "https://login.sample.invalid/token",
		RevokeURL: "https://login.sample.invalid/revoke", Transport: endpoint,
	}
	red := &redact.Redactor{}
	svc := manage.New(store, secrets, connlog.SurfaceTUI, red).
		WithConsentSource(func(id string) (provider.OAuth, bool) { return oauth, id == "sample" })
	m, err := New(svc, store, nil, secrets, red)
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	return m, store, mem, endpoint
}

func refreshStored(mem *secret.MemoryStore) bool {
	_, err := mem.Get(context.Background(), secret.StoreKey("held", config.RoleRefreshToken))
	return err == nil
}

// Removing the refresh token in the editor revokes it at the provider first, through the core, and says
// whether the provider confirmed; it is removed either way.
func TestRemovingTheRefreshTokenRevokesTheConsent(t *testing.T) {
	for _, tt := range []struct {
		status int
		want   string
	}{
		{http.StatusOK, "was revoked at the provider"},
		{http.StatusServiceUnavailable, "did not confirm the revocation"},
	} {
		m, _, mem, endpoint := consentModel(t, tt.status, true)
		editEntry(t, m, "held")
		focusRole(t, m, config.RoleRefreshToken)
		press(t, m, "d")
		pump(t, m, "y")
		if endpoint.count() != 1 {
			t.Errorf("status %d: revocations = %d, want 1", tt.status, endpoint.count())
		}
		if refreshStored(mem) {
			t.Errorf("status %d: the refresh token is still stored", tt.status)
		}
		if !strings.Contains(m.status+m.fail, tt.want) || strings.Contains(m.status+m.fail, consentRefreshCanary) {
			t.Errorf("status %d: status %q, fail %q, want %q", tt.status, m.status, m.fail, tt.want)
		}
	}
}

// Deleting a credential that holds a consent revokes it at the provider first and removes the refresh
// token, then removes the credential, also when the provider does not confirm, with a warning then.
func TestDeletingACredentialRevokesItsConsent(t *testing.T) {
	for _, tt := range []struct {
		status int
		want   string
	}{
		{http.StatusOK, "was revoked at the provider"},
		{http.StatusServiceUnavailable, "did not confirm the revocation"},
	} {
		m, store, mem, endpoint := consentModel(t, tt.status, false)
		openSectionByName(t, m, sectionCredentials)
		press(t, m, "d")
		if m.screen != screenConfirm {
			t.Fatalf("status %d: screen = %v, want the confirmation", tt.status, m.screen)
		}
		pump(t, m, "y")
		if endpoint.count() != 1 {
			t.Errorf("status %d: revocations = %d, want 1", tt.status, endpoint.count())
		}
		if refreshStored(mem) {
			t.Errorf("status %d: the refresh token is still stored", tt.status)
		}
		saved, err := store.Load()
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := saved.Credentials["held"]; ok {
			t.Errorf("status %d: the credential survived the deletion", tt.status)
		}
		if !strings.HasPrefix(m.status, "Deleted") || !strings.Contains(m.status, tt.want) || m.busy != "" {
			t.Errorf("status %d: status %q, fail %q, busy %q, want the deletion and %q", tt.status, m.status,
				m.fail, m.busy, tt.want)
		}
	}
}

// A credential still in use is refused before anything reaches the provider.
func TestDeletingACredentialInUseRevokesNothing(t *testing.T) {
	m, _, mem, endpoint := consentModel(t, http.StatusOK, true)
	openSectionByName(t, m, sectionCredentials)
	press(t, m, "d")
	pump(t, m, "y")
	if endpoint.count() != 0 || !refreshStored(mem) {
		t.Errorf("a refused deletion revoked (%d) or removed the refresh token", endpoint.count())
	}
	if m.fail == "" {
		t.Error("the refusal is not shown")
	}
}
