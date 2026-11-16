package application

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-connect/internal/vault"
	"github.com/castrowithcee/qatlas-kit/output"
)

const tokenSampleRotated = "synthetic-refresh-rotated"

func invokeTokenSample(core *Core) error {
	_, err := core.Invoke(context.Background(), InvokeRequest{
		Operation: "sample.items.get", Connection: "work", Arguments: json.RawMessage(`{"id":"7"}`),
	})
	return err
}

// A rotated refresh token that cannot be stored because the vault was locked since the call read it ends
// the call with the code of a locked vault, before the new access token reaches the provider.
func TestTokenRotationInALockedVaultEndsBeforeTheCall(t *testing.T) {
	vault.LowerWorkFactorForTests()
	core, transport := tokenSampleCore(t, nil)
	core.config.Credentials["login"] = config.Credential{Provider: "sample", Type: config.CredentialTypeVault}
	resolved, err := core.config.Resolve("work", "")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	ask := func(string) (string, error) { return "synthetic-passphrase-rotation", nil }
	setup := vault.New(dir)
	for role, value := range map[string]string{config.RoleClientID: "sample-client-id", config.RoleRefreshToken: tokenSampleRefresh} {
		if err := setup.Set("login", role, value, ask); err != nil {
			t.Fatal(err)
		}
	}
	if err := setup.Approve([]vault.Scope{secret.ScopeOf(resolved)}); err != nil {
		t.Fatal(err)
	}
	core.secrets = secret.NewWith(nil, nil, nil, core.redactor).WithVault(vault.New(dir), ask)
	transport.refresh = tokenSampleRotated
	transport.onGrant = func() { core.secrets.Vault().Forget() }

	err = invokeTokenSample(core)
	if code := ErrorCode(err); code != output.CodeVaultLocked {
		t.Fatalf("Invoke() = %v (%s), want %s", err, code, output.CodeVaultLocked)
	}
	if len(transport.seen) != 0 {
		t.Fatalf("API calls = %v, want none with an unstored rotation", transport.seen)
	}
	for _, value := range []string{tokenSampleRefresh, tokenSampleRotated, tokenSampleAccess} {
		if strings.Contains(err.Error(), value) {
			t.Errorf("the error holds %s", value)
		}
	}
	got, _, _, err := vault.New(dir).Get("login", config.RoleRefreshToken, ask)
	if err != nil || got != tokenSampleRefresh {
		t.Fatalf("vault holds %q, %v; want the old refresh token kept", got, err)
	}
}

// A rotated refresh token from an environment variable is a warning on the audit stream that names the
// credential and the role without a value, and the call goes on.
func TestTokenRotationFromTheEnvironmentWarns(t *testing.T) {
	core, transport := tokenSampleCore(t, map[string]string{
		config.RoleClientID: "sample-client-id", config.RoleRefreshToken: tokenSampleRefresh,
	})
	transport.refresh = tokenSampleRotated
	var audit bytes.Buffer
	core.SetAudit(&audit)
	if err := invokeTokenSample(core); err != nil {
		t.Fatalf("Invoke() = %v, want the call to go on", err)
	}
	if len(transport.seen) != 1 {
		t.Fatalf("API calls = %v, want 1", transport.seen)
	}
	text := audit.String()
	if !strings.Contains(text, "qconnect: warning: ") || !strings.Contains(text, "credential login, role "+config.RoleRefreshToken) {
		t.Fatalf("audit = %q, want a warning naming the credential and the role", text)
	}
	for _, value := range []string{tokenSampleRefresh, tokenSampleRotated, tokenSampleAccess} {
		if strings.Contains(text, value) {
			t.Errorf("the warning holds %s", value)
		}
	}
}
