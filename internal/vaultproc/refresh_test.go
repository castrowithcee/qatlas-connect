//go:build linux || darwin

package vaultproc

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
)

const refreshPassphrase = "synthetic-passphrase-refresh"

// refreshScope is the connection approved to read consent-login, whose refresh token the tests rotate.
func refreshScope() vault.Scope {
	return vault.Scope{Connection: "mail", Credential: "consent-login", Provider: "sample",
		Origin: "https://api.sample.invalid", Effects: []string{"read"}}
}

// refreshFixture is an encrypted vault on disk holding consent-login with a client ID and a refresh token,
// approved for refreshScope, served by a vault process with the vault's key that keeps the log. It returns
// the server, a client, and the vault directory.
func refreshFixture(t *testing.T) (*Server, *Client, string) {
	t.Helper()
	t.Setenv("XDG_RUNTIME_DIR", shortTempDir(t))
	v := vault.New(t.TempDir())
	offer := func(string) (string, error) { return refreshPassphrase, nil }
	if err := v.Set("consent-login", config.RoleClientID, "synthetic-client-id", offer); err != nil {
		t.Fatal(err)
	}
	if err := v.Set("consent-login", config.RoleRefreshToken, "synthetic-refresh-old", offer); err != nil {
		t.Fatal(err)
	}
	if err := v.Approve([]vault.Scope{refreshScope()}); err != nil {
		t.Fatal(err)
	}
	snap, err := v.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewServer(v.Dir(), snap, ServerOptions{KeepLog: true, RetentionDays: 90, Tokens: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	c, err := ProcessClientOf(v)
	if err != nil {
		t.Fatal(err)
	}
	return s, c, v.Dir()
}

// storeRefresh sends one store-refresh request and returns the value the process answers with.
func storeRefresh(c *Client, req storeRefreshRequest) (string, error) {
	var result getResult
	err := c.call(context.Background(), opStoreRefresh, req, &result)
	return result.Value, err
}

func storeRefreshOf(old, rotated string) storeRefreshRequest {
	scope := refreshScope()
	return storeRefreshRequest{Credential: "consent-login", Role: config.RoleRefreshToken, Old: old, Value: rotated,
		Scope: &scope}
}

// storedOnDisk reads the refresh token the vault on disk holds, with a fresh unlock.
func storedOnDisk(t *testing.T, dir string) string {
	t.Helper()
	v := vault.New(filepath.Dir(dir))
	value, _, _, err := v.Get("consent-login", config.RoleRefreshToken,
		func(string) (string, error) { return refreshPassphrase, nil })
	if err != nil {
		t.Fatal(err)
	}
	return value
}

// logText is the invocation log of the vault in dir.
func logText(t *testing.T, dir string) string {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(dir, "logs", "*.jsonl"))
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

// The vault process replaces the refresh token on disk and in memory while it holds the old value, answers
// with what another writer stored otherwise, and logs each write without a value.
func TestServerStoresARotatedRefreshToken(t *testing.T) {
	_, c, dir := refreshFixture(t)
	ctx := context.Background()
	stored, err := c.StoreRefreshToken(ctx, "consent-login", refreshScope(), "synthetic-refresh-old",
		"synthetic-refresh-new")
	if err != nil || stored != "synthetic-refresh-new" {
		t.Fatalf("StoreRefreshToken() = %q, %v, want the rotated value stored", stored, err)
	}
	if got := storedOnDisk(t, dir); got != "synthetic-refresh-new" {
		t.Fatalf("refresh token on disk = %q, want the rotated one", got)
	}
	value, _, err := c.Get(ctx, "consent-login", config.RoleRefreshToken, refreshScope())
	if err != nil || value != "synthetic-refresh-new" {
		t.Fatalf("Get() after the write = %q, %v, want the process to answer with the rotated value", value, err)
	}

	stored, err = c.StoreRefreshToken(ctx, "consent-login", refreshScope(), "synthetic-refresh-old",
		"synthetic-refresh-late")
	if err != nil || stored != "synthetic-refresh-new" {
		t.Fatalf("StoreRefreshToken() with a stale old value = %q, %v, want the stored value back", stored, err)
	}
	if got := storedOnDisk(t, dir); got != "synthetic-refresh-new" {
		t.Fatalf("refresh token on disk = %q, want the earlier writer to win", got)
	}

	log := logText(t, dir)
	for _, want := range []string{
		`"operation":"credential.store-refresh-token","connection":"mail","effect":"update","result":"success"`,
		`"operation":"credential.store-refresh-token","connection":"mail","effect":"update","result":"conflict"`,
	} {
		if !strings.Contains(log, want) {
			t.Errorf("log = %s, want %s", log, want)
		}
	}
	for _, value := range []string{"synthetic-refresh-old", "synthetic-refresh-new", "synthetic-refresh-late", "synthetic-client-id"} {
		if strings.Contains(log, value) {
			t.Errorf("the log holds %s", value)
		}
	}
}

// No other role is written, no connection the vault has not approved as it is now writes, and a connection
// writes only the credential it reads; a request without the old value reads as a process that cannot
// store, and a locked process refuses. Every refusal leaves the vault as it was.
func TestServerRefusesOtherRefreshWrites(t *testing.T) {
	s, c, dir := refreshFixture(t)

	otherRole := storeRefreshOf("synthetic-client-id", "synthetic-client-forged")
	otherRole.Role = config.RoleClientID
	if _, err := storeRefresh(c, otherRole); !errors.Is(err, ErrRoleRefused) {
		t.Errorf("store-refresh of the client ID = %v, want ErrRoleRefused", err)
	}

	changed := storeRefreshOf("synthetic-refresh-old", "synthetic-refresh-new")
	changed.Scope.Effects = []string{"read", "delete"}
	if _, err := storeRefresh(c, changed); !errors.Is(err, vault.ErrApprovalRequired) {
		t.Errorf("store-refresh for a changed connection = %v, want ErrApprovalRequired", err)
	}

	foreign := storeRefreshOf("synthetic-refresh-old", "synthetic-refresh-new")
	foreign.Credential = "other-login"
	if _, err := storeRefresh(c, foreign); !errors.Is(err, vault.ErrApprovalRequired) {
		t.Errorf("store-refresh of a credential the connection does not read = %v, want ErrApprovalRequired", err)
	}

	unbound := storeRefreshOf("synthetic-refresh-old", "synthetic-refresh-new")
	unbound.Scope = nil
	if _, err := storeRefresh(c, unbound); !errors.Is(err, vault.ErrApprovalRequired) {
		t.Errorf("store-refresh without a connection = %v, want ErrApprovalRequired", err)
	}

	if _, err := c.StoreRefreshToken(context.Background(), "consent-login", refreshScope(), "",
		"synthetic-refresh-new"); !errors.Is(err, ErrNoRefreshStore) {
		t.Errorf("StoreRefreshToken() without the old value = %v, want ErrNoRefreshStore", err)
	}

	if got := storedOnDisk(t, dir); got != "synthetic-refresh-old" {
		t.Fatalf("refresh token on disk = %q, want it untouched by every refusal", got)
	}
	log := logText(t, dir)
	for _, want := range []string{`"result":"role-refused"`, `"connection":"mail","effect":"update","result":"approval-required"`} {
		if !strings.Contains(log, want) {
			t.Errorf("log = %s, want %s", log, want)
		}
	}
	if strings.Contains(log, "synthetic-client-forged") || strings.Contains(log, "synthetic-refresh-new") {
		t.Error("the log holds a value")
	}

	_ = s.Close()
	if _, err := storeRefresh(c, storeRefreshOf("synthetic-refresh-old", "synthetic-refresh-new")); !errors.Is(err, ErrNotRunning) {
		t.Errorf("store-refresh on a locked process = %v, want ErrNotRunning", err)
	}
}

// Parallel renewals of several runs that redeemed the same refresh token: exactly one write wins, and every
// other run is answered with the winner's value, so no rotated token is lost.
func TestServerParallelRefreshWritesOneWins(t *testing.T) {
	_, c, dir := refreshFixture(t)
	const writers = 6
	values := make([]string, writers)
	errs := make([]error, writers)
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			values[i], errs[i] = storeRefresh(c, storeRefreshOf("synthetic-refresh-old", fmt.Sprintf("synthetic-refresh-run-%d", i)))
		}(i)
	}
	wg.Wait()
	winner := storedOnDisk(t, dir)
	if winner == "synthetic-refresh-old" {
		t.Fatal("no write won")
	}
	for i := range values {
		if errs[i] != nil || values[i] != winner {
			t.Errorf("run %d = %q, %v, want the winner's value %q", i, values[i], errs[i], winner)
		}
	}
	if n := strings.Count(logText(t, dir), `"result":"success"`); n != 1 {
		t.Errorf("successful writes logged = %d, want 1", n)
	}
}
