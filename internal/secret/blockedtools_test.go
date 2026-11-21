package secret

import (
	"reflect"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/config"
)

// A resolved connection has no blocked_tools to carry, so the approval scope cannot depend on them.
func TestScopeOfIgnoresBlockedTools(t *testing.T) {
	cfg := config.New(blockedCatalog{})
	if err := cfg.SetService("svc", config.Service{Provider: "sample", BaseURL: "https://sample.example.test"}); err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetCredential("key", config.Credential{Provider: "sample", Type: config.CredentialTypeKeyring}); err != nil {
		t.Fatal(err)
	}
	conn := config.Connection{Service: "svc", Credential: "key", Effects: []config.Effect{config.EffectRead}}
	if err := cfg.SetConnection("conn", conn); err != nil {
		t.Fatal(err)
	}
	plain, err := cfg.Resolve("conn", "")
	if err != nil {
		t.Fatal(err)
	}
	conn.BlockedTools = []string{"sample.items.delete"}
	if err := cfg.SetConnection("conn", conn); err != nil {
		t.Fatal(err)
	}
	blocked, err := cfg.Resolve("conn", "")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ScopeOf(plain), ScopeOf(blocked)) {
		t.Errorf("ScopeOf changed with blocked_tools:\n%+v\n%+v", ScopeOf(plain), ScopeOf(blocked))
	}
}

type blockedCatalog struct{}

var blockedProvider = config.ProviderMetadata{
	ID: "sample", SecretRoles: []config.SecretRole{{Name: "token"}},
	Tools: []config.ToolMetadata{
		{ID: "sample.items.get", Effect: config.EffectRead},
		{ID: "sample.items.delete", Effect: config.EffectDelete},
	},
}

func (blockedCatalog) ProviderMetadata(id string) (config.ProviderMetadata, bool) {
	return blockedProvider, id == blockedProvider.ID
}

func (blockedCatalog) ProviderMetadataAll() []config.ProviderMetadata {
	return []config.ProviderMetadata{blockedProvider}
}
