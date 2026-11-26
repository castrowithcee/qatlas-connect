//go:build linux || darwin

package secret

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/vault"
	"github.com/castrowithcee/qatlas-connect/internal/vaultproc"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// refreshProcess builds an encrypted vault in a fresh temporary directory holding the refresh token of
// credName, approved for wikiConnection, and serves it from this test process the way 'qconnect vault
// unlock' hands it to a vault process, writing and logging like one. It returns the vault directory and a
// function that locks the process.
func refreshProcess(t *testing.T) (string, func()) {
	t.Helper()
	vault.LowerWorkFactorForTests()
	t.Setenv("XDG_RUNTIME_DIR", shortRuntimeDir(t))
	dir := t.TempDir()
	v := vault.New(dir)
	if err := v.Set(credName, config.RoleRefreshToken, refreshOld, askRefreshPass); err != nil {
		t.Fatal(err)
	}
	approveWiki(t, v)
	snap, err := v.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	s, err := vaultproc.NewServer(v.Dir(), snap, vaultproc.ServerOptions{KeepLog: true, RetentionDays: 90,
		Tokens: true})
	if err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	lock := func() { once.Do(func() { _ = s.Close() }) }
	t.Cleanup(lock)
	return dir, lock
}

// processResolver is an unattended resolver over the vault in dir that asks its vault process and never a
// passphrase.
func processResolver(t *testing.T, dir string) *Resolver {
	t.Helper()
	ask := func(string) (string, error) {
		t.Errorf("the passphrase was asked for")
		return "", vault.ErrNoTerminal
	}
	r := NewWith(nil, nil, nil, &redact.Redactor{}).WithVault(vault.New(dir), ask)
	r.process = true
	r.Unattended()
	return r
}

func processLog(t *testing.T, dir string) string {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(dir, vault.DirName, "logs", "*.jsonl"))
	var text strings.Builder
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		text.Write(data)
	}
	return text.String()
}

// The vault process writes the rotated refresh token for the approved connection and the resolver reads it
// back from it; a connection the vault has not approved as it is now is refused, and once the process is
// locked the write fails with the error of a locked vault. The process logs each write without a value.
func TestStoreRefreshTokenThroughTheVaultProcess(t *testing.T) {
	dir, lock := refreshProcess(t)
	r := processResolver(t, dir)
	ctx := throughWiki()

	stored, err := r.StoreRefreshToken(ctx, credName, vaultCred(), SourceVault, refreshOld, refreshRotated)
	if err != nil || stored != refreshRotated {
		t.Fatalf("StoreRefreshToken() = %q, %v, want the rotated value stored", stored, err)
	}
	got, err := r.Resolve(ctx, credName, vaultCred(), config.RoleRefreshToken)
	if err != nil || got.Secret != refreshRotated {
		t.Fatalf("Resolve() after the write = %q, %v, want the rotated value", got.Secret, err)
	}

	changed := wikiConnection()
	changed.Effects = []config.Effect{config.EffectRead, config.EffectDelete}
	_, err = r.StoreRefreshToken(ForConnection(context.Background(), changed), credName, vaultCred(), SourceVault,
		refreshRotated, refreshOther)
	var approval *ApprovalRequiredError
	if !errors.As(err, &approval) {
		t.Fatalf("StoreRefreshToken() for a changed connection = %v, want *ApprovalRequiredError", err)
	}
	noValueIn(t, "the refusal", err.Error())

	lock()
	_, err = r.StoreRefreshToken(ctx, credName, vaultCred(), SourceVault, refreshRotated, refreshOther)
	var locked *VaultLockedError
	if !errors.As(err, &locked) {
		t.Fatalf("StoreRefreshToken() after the lock = %v, want *VaultLockedError", err)
	}
	if value := storedRefresh(t, dir, askRefreshPass); value != refreshRotated {
		t.Fatalf("vault holds %q, want the one write that was allowed", value)
	}
	log := processLog(t, dir)
	for _, want := range []string{`"connection":"wiki","effect":"update","result":"success"`,
		`"connection":"wiki","effect":"update","result":"approval-required"`} {
		if !strings.Contains(log, want) {
			t.Errorf("log = %s, want %s", log, want)
		}
	}
	noValueIn(t, "the log", log)
}

// Two runs that redeemed the same refresh token store their rotations at the same time through the vault
// process: one wins, and the other one gets the winner's value, so neither loses a token.
func TestStoreRefreshTokenParallelRunsThroughTheVaultProcess(t *testing.T) {
	dir, _ := refreshProcess(t)
	const runs = 4
	stored := make([]string, runs)
	errs := make([]error, runs)
	var wg sync.WaitGroup
	for i := 0; i < runs; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r := processResolver(t, dir)
			stored[i], errs[i] = r.StoreRefreshToken(throughWiki(), credName, vaultCred(), SourceVault, refreshOld,
				fmt.Sprintf("synthetic-refresh-run-%d", i))
		}(i)
	}
	wg.Wait()
	winner := storedRefresh(t, dir, askRefreshPass)
	if winner == refreshOld {
		t.Fatal("no run stored its rotation")
	}
	for i := range stored {
		if errs[i] != nil || stored[i] != winner {
			t.Errorf("run %d = %q, %v, want the winner's value %q", i, stored[i], errs[i], winner)
		}
	}
	if n := strings.Count(processLog(t, dir), `"result":"success"`); n != 1 {
		t.Errorf("successful writes logged = %d, want 1", n)
	}
}
