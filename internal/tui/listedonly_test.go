package tui

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// guardedRegistry registers a fixture provider whose only tool requires an allow-list, so it declares no
// profile.
func guardedRegistry(t *testing.T) *capability.Registry {
	t.Helper()
	reg := capability.NewRegistry()
	if err := reg.RegisterProvider(config.ProviderMetadata{
		ID: "guarded", Name: "Guarded",
		SecretRoles: []config.SecretRole{
			{Name: "token-id", Description: "the token id"},
			{Name: "token-secret", Description: "the token secret"},
		},
	}, nil); err != nil {
		t.Fatal(err)
	}
	send := capability.Descriptor{
		ID: "guarded.items.send", Version: 1, Description: "Send an item", Provider: "guarded",
		Risk: capability.Risk{Effect: capability.EffectCreate, Idempotency: capability.IdempotencyNonIdempotent,
			Confirmation: capability.ConfirmationRequired, DataSensitivity: "test-data"},
		InputSchema:           json.RawMessage(`{"type":"object"}`),
		OutputSchema:          json.RawMessage(`{"type":"object"}`),
		RequiresToolAllowList: true,
	}
	if err := reg.Register("guarded", capability.Operation{Descriptor: send,
		Handler: func(context.Context, *config.Resolved, *secret.Resolver, *redact.Redactor, json.RawMessage) (any, error) {
			return nil, nil
		}}); err != nil {
		t.Fatal(err)
	}
	if err := reg.ValidateProfiles(); err != nil {
		t.Fatalf("a provider with only listed-only tools and no profile was refused: %v", err)
	}
	return reg
}

func guardedModel(t *testing.T) *Model {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "qconnect")
	store := config.NewStore(filepath.Join(dir, "config.yaml"), guardedRegistry(t))
	secrets, _ := newResolver(t, dir, nil)
	m, err := buildModel(store, nil, secrets, nil)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// guardedFormModel is guardedModel over a saved service and credential, so the connection form has a provider.
func guardedFormModel(t *testing.T) *Model {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	store := config.NewStore(path, guardedRegistry(t))
	cfg := store.New()
	mustNoError(t, cfg.SetService("wiki", config.Service{Provider: "guarded", BaseURL: "https://wiki.example.invalid"}))
	mustNoError(t, cfg.SetCredential("reader", config.Credential{Provider: "guarded", Type: config.CredentialTypeKeyring}))
	mustNoError(t, store.Save(cfg))
	m, err := buildModel(store, nil, nil, &redact.Redactor{})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// The connection form of a provider without a profile starts with no tool ticked and without a profile row.
func TestConnectionFormOfAListedOnlyProviderStartsWithoutTools(t *testing.T) {
	m := guardedFormModel(t)
	openSectionByName(t, m, sectionConnections)
	pressNew(t, m)
	if got := m.formProvider(); got != "guarded" {
		t.Fatalf("provider = %q, want guarded", got)
	}
	if got := m.fieldValue(toolListLabel); got != "" {
		t.Errorf("tools = %q, want none ticked", got)
	}
	if row := m.field(profileLabel); row != nil && !row.hidden {
		t.Errorf("a provider without profiles shows a profile row: %+v", row)
	}
	if m.fail != "" {
		t.Errorf("error %q", m.fail)
	}
}

// The guided setup reaches its summary for such a provider with the default access and no tool ticked.
func TestGuidedSetupOfAListedOnlyProviderStartsWithoutTools(t *testing.T) {
	m := guardedModel(t)
	openSectionByName(t, m, sectionConnections)
	m.screen = screenList
	walkSetupFrom(t, m, "g", stepEffects)
	if m.wizard.step != stepEffects {
		t.Fatalf("step = %d, want the access step", m.wizard.step)
	}
	if got := m.fieldValue(toolListLabel); got != "" {
		t.Errorf("tools = %q, want none ticked", got)
	}
	if got := m.fieldValue(accessLabel); got == accessTools {
		t.Errorf("access = %q, want the default access", got)
	}
	press(t, m, "f2")
	if m.fail != "" || m.screen != screenSummary {
		t.Fatalf("screen = %v, error %q, want the summary", m.screen, m.fail)
	}
}
