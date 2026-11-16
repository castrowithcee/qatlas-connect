package secret

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/invokelog"
	"github.com/castrowithcee/qatlas-connect/internal/vault"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	refreshOld     = "synthetic-refresh-old"
	refreshRotated = "synthetic-refresh-rotated"
	refreshOther   = "synthetic-refresh-other"
	refreshPass    = "synthetic-passphrase-refresh"
)

// recordingLog keeps every entry a resolver logs.
type recordingLog struct {
	mu      sync.Mutex
	entries []invokelog.Fields
}

func (l *recordingLog) Append(f invokelog.Fields) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := f.Validate(); err != nil {
		return err
	}
	l.entries = append(l.entries, f)
	return nil
}

func (l *recordingLog) results() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var results []string
	for _, f := range l.entries {
		if f.Operation != invokelog.OperationStoreRefreshToken || f.Connection != "wiki" || f.Effect != "update" {
			results = append(results, "unexpected entry")
			continue
		}
		results = append(results, f.Result)
	}
	return results
}

// noValueIn fails the test when text holds one of the synthetic tokens.
func noValueIn(t *testing.T, what, text string) {
	t.Helper()
	for _, value := range []string{refreshOld, refreshRotated, refreshOther} {
		if strings.Contains(text, value) {
			t.Errorf("%s holds a token value: %q", what, text)
		}
	}
}

func storedRefresh(t *testing.T, dir string, ask vault.PassphraseFunc) string {
	t.Helper()
	value, _, _, err := vault.New(dir).Get(credName, config.RoleRefreshToken, ask)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func askRefreshPass(string) (string, error) { return refreshPass, nil }

// An unencrypted vault is written by the run itself, only while it still holds the old value, and each
// write is logged without a value.
func TestStoreRefreshTokenInAnUnencryptedVault(t *testing.T) {
	dir := t.TempDir()
	if err := vault.New(dir).Set(credName, config.RoleRefreshToken, refreshOld, nil); err != nil {
		t.Fatal(err)
	}
	log := &recordingLog{}
	red := &redact.Redactor{}
	r := NewWith(nil, nil, nil, red).WithVault(vault.New(dir), nil)
	r.LogWrites(log)

	stored, err := r.StoreRefreshToken(throughWiki(), credName, vaultCred(), SourceVault, refreshOld, refreshRotated)
	if err != nil || stored != refreshRotated {
		t.Fatalf("StoreRefreshToken() = %q, %v, want the rotated value stored", stored, err)
	}
	stored, err = r.StoreRefreshToken(throughWiki(), credName, vaultCred(), SourceVault, refreshOld, refreshOther)
	if err != nil || stored != refreshRotated {
		t.Fatalf("StoreRefreshToken() with a stale old value = %q, %v, want the stored value", stored, err)
	}
	if got := storedRefresh(t, dir, nil); got != refreshRotated {
		t.Fatalf("vault holds %q, want the first write to win", got)
	}
	if got := log.results(); len(got) != 2 || got[0] != "success" || got[1] != "conflict" {
		t.Errorf("logged results = %v, want success and conflict", got)
	}
	if out := red.Apply(refreshRotated + " " + refreshOther); strings.Contains(out, "synthetic") {
		t.Errorf("the redactor does not know every value: %q", out)
	}
}

// A vault the run unlocked itself is written by the run, only for a connection it approved as it is now.
func TestStoreRefreshTokenInAVaultUnlockedHere(t *testing.T) {
	vault.LowerWorkFactorForTests()
	dir := t.TempDir()
	setup := vault.New(dir)
	if err := setup.Set(credName, config.RoleRefreshToken, refreshOld, askRefreshPass); err != nil {
		t.Fatal(err)
	}
	approveWiki(t, setup)
	log := &recordingLog{}
	r := NewWith(nil, nil, nil, &redact.Redactor{}).WithVault(vault.New(dir), askRefreshPass)
	r.LogWrites(log)
	if _, err := r.Resolve(throughWiki(), credName, vaultCred(), config.RoleRefreshToken); err != nil {
		t.Fatalf("Resolve() = %v", err)
	}

	changed := wikiConnection()
	changed.Effects = []config.Effect{config.EffectRead, config.EffectDelete}
	_, err := r.StoreRefreshToken(ForConnection(context.Background(), changed), credName, vaultCred(), SourceVault,
		refreshOld, refreshRotated)
	var approval *ApprovalRequiredError
	if !errors.As(err, &approval) {
		t.Fatalf("StoreRefreshToken() for a changed connection = %v, want *ApprovalRequiredError", err)
	}
	noValueIn(t, "the refusal", err.Error())

	stored, err := r.StoreRefreshToken(throughWiki(), credName, vaultCred(), SourceVault, refreshOld, refreshRotated)
	if err != nil || stored != refreshRotated {
		t.Fatalf("StoreRefreshToken() = %q, %v", stored, err)
	}
	if got := storedRefresh(t, dir, askRefreshPass); got != refreshRotated {
		t.Fatalf("vault holds %q, want the rotated value", got)
	}
	if got := log.results(); len(got) != 2 || got[0] != "approval-required" || got[1] != "success" {
		t.Errorf("logged results = %v", got)
	}
}

// An encrypted vault locked here with no vault process to write it fails with the error of a locked vault,
// and nothing is written.
func TestStoreRefreshTokenInALockedVault(t *testing.T) {
	vault.LowerWorkFactorForTests()
	dir := t.TempDir()
	if err := vault.New(dir).Set(credName, config.RoleRefreshToken, refreshOld, askRefreshPass); err != nil {
		t.Fatal(err)
	}
	r := NewWith(nil, nil, nil, nil).WithVault(vault.New(dir), nil)
	_, err := r.StoreRefreshToken(throughWiki(), credName, vaultCred(), SourceVault, refreshOld, refreshRotated)
	var locked *VaultLockedError
	if !errors.As(err, &locked) {
		t.Fatalf("StoreRefreshToken() = %v, want *VaultLockedError", err)
	}
	noValueIn(t, "the error", err.Error())
	if got := storedRefresh(t, dir, askRefreshPass); got != refreshOld {
		t.Fatalf("vault holds %q, want it untouched", got)
	}
}

// The system credential store is written by the run under a lock across processes, with the same compare:
// a value another run stored first is returned and kept.
func TestStoreRefreshTokenInTheCredentialStore(t *testing.T) {
	store := NewMemoryStore()
	key := StoreKey(credName, config.RoleRefreshToken)
	if err := store.Set(key, refreshOther); err != nil {
		t.Fatal(err)
	}
	log := &recordingLog{}
	r := NewWith(nil, store, nil, &redact.Redactor{})
	r.lockDir = t.TempDir()
	r.LogWrites(log)
	ctx := throughWiki()

	stored, err := r.StoreRefreshToken(ctx, credName, keyringCred(), SourceStore, refreshOld, refreshRotated)
	if err != nil || stored != refreshOther {
		t.Fatalf("StoreRefreshToken() against another value = %q, %v, want that value", stored, err)
	}
	if got, _ := store.Get(ctx, key); got != refreshOther {
		t.Fatalf("store holds %q, want the value another run stored", got)
	}
	stored, err = r.StoreRefreshToken(ctx, credName, keyringCred(), SourceStore, refreshOther, refreshRotated)
	if err != nil || stored != refreshRotated {
		t.Fatalf("StoreRefreshToken() = %q, %v", stored, err)
	}
	if got, _ := store.Get(ctx, key); got != refreshRotated {
		t.Fatalf("store holds %q, want the rotated value", got)
	}

	store.Fail(ErrLocked)
	_, err = r.StoreRefreshToken(ctx, credName, keyringCred(), SourceStore, refreshRotated, refreshOther)
	if !errors.Is(err, ErrLocked) {
		t.Fatalf("StoreRefreshToken() with a locked store = %v, want ErrLocked", err)
	}
	noValueIn(t, "the error", err.Error())
	if got := log.results(); len(got) != 3 || got[0] != "conflict" || got[1] != "success" || got[2] != "store-failed" {
		t.Errorf("logged results = %v", got)
	}
}

// An environment variable is not written: the run warns, naming the credential and the role without a value,
// and goes on with the rotated value. The plaintext file is not written either, which ends the call.
func TestStoreRefreshTokenElsewhere(t *testing.T) {
	log := &recordingLog{}
	r := NewWith(nil, NewMemoryStore(), nil, nil)
	r.LogWrites(log)
	var warnings []string
	ctx := WithWarnings(throughWiki(), func(line string) { warnings = append(warnings, line) })

	stored, err := r.StoreRefreshToken(ctx, credName, envCred("SYNTHETIC_REFRESH"), SourceEnv, refreshOld, refreshRotated)
	if err != nil || stored != refreshRotated {
		t.Fatalf("StoreRefreshToken() from the environment = %q, %v, want the call to go on", stored, err)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "credential "+credName+", role "+config.RoleRefreshToken) {
		t.Fatalf("warnings = %q, want one naming the credential and the role", warnings)
	}
	noValueIn(t, "the warning", warnings[0])

	_, err = r.StoreRefreshToken(ctx, credName, keyringCred(), SourcePlaintext, refreshOld, refreshRotated)
	if err == nil {
		t.Fatal("StoreRefreshToken() from the plaintext file succeeded, want it to end the call")
	}
	noValueIn(t, "the error", err.Error())

	if _, err := r.StoreRefreshToken(context.Background(), credName, keyringCred(), SourceStore, refreshOld,
		refreshRotated); err == nil {
		t.Error("StoreRefreshToken() without a connection succeeded")
	}
	other := wikiConnection()
	other.Credential = "other-login"
	if _, err := r.StoreRefreshToken(ForConnection(context.Background(), other), credName, keyringCred(),
		SourceStore, refreshOld, refreshRotated); err == nil {
		t.Error("StoreRefreshToken() for a connection reading another credential succeeded")
	}
	if got := log.results(); len(got) != 0 {
		t.Errorf("logged results = %v, want none without a write", got)
	}
}
