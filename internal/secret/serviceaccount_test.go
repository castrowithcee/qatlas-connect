package secret

import (
	"context"
	"errors"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/vault"
)

// Editing only the file to impersonate another service account does not reach the secret: the vault approved
// the connection with the former one. A connection approved without one keeps its approval.
func TestChangingTheServiceAccountOutsideTheVaultNeedsAnApproval(t *testing.T) {
	r, v, _ := encryptedForwardVault(t)
	first, second := wikiConnection(), wikiConnection()
	first.ServiceAccount, second.ServiceAccount = "robot-one", "robot-two"
	if ScopeOf(first).ServiceAccount != "robot-one" || ScopeOf(wikiConnection()).ServiceAccount != "" {
		t.Fatal("ScopeOf() does not carry exactly the service account")
	}
	if err := v.Approve([]vault.Scope{ScopeOf(first)}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Resolve(ForConnection(context.Background(), first), credName, vaultCred(), role); err != nil {
		t.Fatalf("Resolve() as approved = %v", err)
	}
	for name, changed := range map[string]*config.Resolved{"another service account": second, "none": wikiConnection()} {
		_, err := r.Resolve(ForConnection(context.Background(), changed), credName, vaultCred(), role)
		if !errors.Is(err, vault.ErrApprovalRequired) {
			t.Errorf("Resolve() with %s = %v, want ErrApprovalRequired", name, err)
		}
	}
}
