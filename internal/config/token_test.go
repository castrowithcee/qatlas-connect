package config

import (
	"slices"
	"testing"
)

// tokenProviders has one provider, "sample", whose credential issues access tokens.
var tokenProviders ProviderCatalog = testProviderCatalog{
	"sample": {
		ID: "sample", Name: "Sample",
		SecretRoles:    TokenSecretRoles(),
		DefaultEffects: []Effect{EffectRead},
		Tools: []ToolMetadata{
			{ID: "sample.files.download", Effect: EffectRead, LocalFiles: LocalFilesWrite},
			{ID: "sample.items.delete", Effect: EffectDelete, RequiresToolAllowList: true},
			{ID: "sample.items.get", Effect: EffectRead},
			{ID: "sample.items.list", Effect: EffectRead},
			{ID: "sample.items.update", Effect: EffectUpdate},
		},
		Token: &TokenMetadata{Scopes: map[string][]string{
			"sample.files.download": {"files.read"},
			"sample.items.delete":   {"items.admin"},
			"sample.items.get":      {"items.read"},
			"sample.items.list":     {"items.read"},
			"sample.items.update":   {"items.read", "items.write"},
		}},
	},
}

func tokenConfig(cred Credential, conn Connection) *Config {
	cfg := New(tokenProviders)
	cfg.Services = map[string]Service{"sample": {Provider: "sample", BaseURL: "https://sample.example.invalid"}}
	cfg.Credentials = map[string]Credential{"login": cred}
	conn.Service, conn.Credential = "sample", "login"
	cfg.Connections = map[string]Connection{"work": conn}
	return cfg
}

func TestTokenProcedureOf(t *testing.T) {
	tests := []struct {
		name           string
		clientID, key  bool
		serviceAccount string
		want           TokenProcedure
		ok             bool
	}{
		{"client ID", true, false, "", TokenConsent, true},
		{"key", false, true, "", TokenServiceAccount, true},
		{"client ID and a named service account", true, false, "robot", TokenImpersonation, true},
		{"client ID and key", true, true, "", "", false},
		{"neither", false, false, "", "", false},
		{"key and a named service account", false, true, "robot", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := TokenProcedureOf(tt.clientID, tt.key, tt.serviceAccount)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("TokenProcedureOf = %q, %v; want %q, %v", got, ok, tt.want, tt.ok)
			}
		})
	}
}

// Validation reads only the file: a stored credential is valid whatever its store holds, also a client ID
// without a refresh token before the first consent, while an environment credential must name exactly one
// complete procedure. A token credential names its provider whatever its type.
func TestTokenCredentialValidation(t *testing.T) {
	env := func(values map[string]string) Credential {
		return Credential{Provider: "sample", Type: CredentialTypeEnv, Values: values}
	}
	tests := []struct {
		name string
		cred Credential
		want string
	}{
		{"vault before the first consent", Credential{Provider: "sample", Type: CredentialTypeVault}, ""},
		{"keyring", Credential{Provider: "sample", Type: CredentialTypeKeyring}, ""},
		{"env with a consent", env(map[string]string{
			RoleClientID: "SAMPLE_CLIENT_ID", RoleClientSecret: "SAMPLE_CLIENT_SECRET", RoleRefreshToken: "SAMPLE_REFRESH",
		}), ""},
		{"env with a consent without client secret", env(map[string]string{
			RoleClientID: "SAMPLE_CLIENT_ID", RoleRefreshToken: "SAMPLE_REFRESH",
		}), ""},
		{"env with a service account", env(map[string]string{RoleServiceAccountKey: "SAMPLE_KEY"}), ""},
		{"env with a client ID and no refresh token", env(map[string]string{RoleClientID: "SAMPLE_CLIENT_ID"}),
			CodeTokenProcedure},
		{"env with both procedures", env(map[string]string{
			RoleClientID: "SAMPLE_CLIENT_ID", RoleRefreshToken: "SAMPLE_REFRESH", RoleServiceAccountKey: "SAMPLE_KEY",
		}), CodeTokenProcedure},
		{"env with a client secret only", env(map[string]string{RoleClientSecret: "SAMPLE_CLIENT_SECRET"}),
			CodeTokenProcedure},
		{"vault without a provider", Credential{Type: CredentialTypeVault}, CodeTokenProviderMissing},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tokenConfig(tt.cred, Connection{}).Validate()
			if tt.want == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want valid", err)
				}
				return
			}
			if codes := codesOf(err); !slices.Equal(codes, []string{tt.want}) {
				t.Fatalf("codes = %v, want only %s", codes, tt.want)
			}
		})
	}
}

// The scopes of a connection follow from the tools its effects, tools list and file release offer.
func TestTokenScopes(t *testing.T) {
	cred := Credential{Provider: "sample", Type: CredentialTypeVault}
	tests := []struct {
		name string
		conn Connection
		want []string
	}{
		{"default effects", Connection{}, []string{"items.read"}},
		{"read and update", Connection{Effects: []Effect{EffectRead, EffectUpdate}},
			[]string{"items.read", "items.write"}},
		{"the allow-listed tool needs its name", Connection{Effects: []Effect{EffectRead, EffectDelete}},
			[]string{"items.read"}},
		{"a tools list", Connection{Tools: []string{"sample.items.delete"}}, []string{"items.admin"}},
		{"a file release adds the download", Connection{Files: Files{Write: []string{"/srv/downloads"}}},
			[]string{"files.read", "items.read"}},
		{"no effects", Connection{Effects: []Effect{}}, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tokenConfig(cred, tt.conn)
			if got := cfg.TokenScopes("work"); !slices.Equal(got, tt.want) {
				t.Fatalf("TokenScopes = %v, want %v", got, tt.want)
			}
			resolved, err := cfg.Resolve("work", "")
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(resolved.TokenScopes, tt.want) {
				t.Fatalf("Resolved.TokenScopes = %v, want %v", resolved.TokenScopes, tt.want)
			}
		})
	}
	if got := optionalConfig(Credential{Provider: "site", Type: CredentialTypeVault}).TokenScopes("site"); got != nil {
		t.Errorf("TokenScopes of a provider without tokens = %v, want nil", got)
	}
}

// joinedCatalog answers from the first of its catalogs that knows a provider.
type joinedCatalog []ProviderCatalog

func (j joinedCatalog) ProviderMetadata(id string) (ProviderMetadata, bool) {
	for _, catalog := range j {
		if metadata, ok := catalog.ProviderMetadata(id); ok {
			return metadata, true
		}
	}
	return ProviderMetadata{}, false
}

func (j joinedCatalog) ProviderMetadataAll() []ProviderMetadata {
	var all []ProviderMetadata
	for _, catalog := range j {
		all = append(all, catalog.ProviderMetadataAll()...)
	}
	return all
}

// withTokenConnection adds the provider of tokenProviders and a connection "tok" over it with credential
// "tokkey".
func withTokenConnection(c *Config, cred Credential) {
	c.providers = joinedCatalog{c.providerCatalog(), tokenProviders}
	c.Services["tok"] = Service{Provider: "sample", BaseURL: "https://sample.example.invalid"}
	c.Credentials["tokkey"] = cred
	c.Connections["tok"] = Connection{Service: "tok", Credential: "tokkey"}
}
