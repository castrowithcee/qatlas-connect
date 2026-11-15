package capability

import (
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/config"
)

// A provider whose credential issues access tokens declares exactly the token roles, all optional, and its
// registered scopes cannot be changed through a copy.
func TestRegisterProviderChecksTokenRoles(t *testing.T) {
	token := &config.TokenMetadata{Scopes: map[string][]string{"sample.items.get": {"items.read"}}}
	required := config.TokenSecretRoles()
	required[0].Optional = false
	for name, roles := range map[string][]config.SecretRole{
		"another role":      {{Name: "token"}},
		"a missing role":    config.TokenSecretRoles()[:3],
		"a required role":   required,
		"an additional one": append(config.TokenSecretRoles(), config.SecretRole{Name: "token", Optional: true}),
	} {
		err := NewRegistry().RegisterProvider(config.ProviderMetadata{ID: "sample", Name: "Sample", SecretRoles: roles,
			Token: token}, nil)
		if err == nil || !strings.Contains(err.Error(), "issues access tokens") {
			t.Errorf("%s: RegisterProvider() = %v, want a token role error", name, err)
		}
	}

	reg := NewRegistry()
	if err := reg.RegisterProvider(config.ProviderMetadata{ID: "sample", Name: "Sample",
		SecretRoles: config.TokenSecretRoles(), Token: token}, nil); err != nil {
		t.Fatal(err)
	}
	copied, _ := reg.ProviderMetadata("sample")
	copied.Token.Scopes["sample.items.get"][0] = "changed"
	again, _ := reg.ProviderMetadata("sample")
	if got := again.Token.Scopes["sample.items.get"][0]; got != "items.read" {
		t.Fatalf("registered scope = %q after changing a copy", got)
	}
}
