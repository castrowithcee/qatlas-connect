//go:build linux || darwin

package vaultproc

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/invokelog"
	"github.com/castrowithcee/qatlas-connect/internal/vault"
	"github.com/castrowithcee/qatlas-kit/release/releasetest"
	kitvaultproc "github.com/castrowithcee/qatlas-kit/vaultproc"
	"golang.org/x/crypto/ssh"
)

// The vault process hands a secret only to a connection the vault approved as it is now, answers what it
// does not hold as not found, takes a set, a delete and new bindings, and checks without a value.
func TestServerHandsSecretsOnlyToApprovedConnections(t *testing.T) {
	tv := newTestVault(t)
	_, client := tv.serve(t, ServerOptions{})
	ctx := withTimeout(t)

	if value, found, err := client.Get(ctx, "wiki", "token", tv.scope); err != nil || !found || value != testSecret {
		t.Fatalf("Get() = %q, %v, %v", value, found, err)
	}
	if err := client.Check(ctx, tv.scope); err != nil {
		t.Fatalf("Check() = %v", err)
	}
	if _, found, err := client.Get(ctx, "wiki", "other-role", tv.scope); err != nil || found {
		t.Fatalf("Get() of a role not held = %v, %v, want not found", found, err)
	}
	wider := tv.scope
	wider.Effects = []string{"read", "delete"}
	if _, _, err := client.Get(ctx, "wiki", "token", wider); !errors.Is(err, vault.ErrApprovalRequired) {
		t.Fatalf("Get() for a changed connection = %v, want ErrApprovalRequired", err)
	}
	if err := client.Check(ctx, wider); !errors.Is(err, vault.ErrApprovalRequired) {
		t.Fatalf("Check() for a changed connection = %v, want ErrApprovalRequired", err)
	}
	other := tv.scope
	other.Credential = "crm"
	if _, _, err := client.Get(ctx, "wiki", "token", other); !errors.Is(err, vault.ErrApprovalRequired) {
		t.Fatalf("Get() of another credential than the connection reads = %v, want ErrApprovalRequired", err)
	}

	if err := client.Set(ctx, "wiki", "token", "changed-"+testSecret); err != nil {
		t.Fatalf("Set() = %v", err)
	}
	if value, _, err := client.Get(ctx, "wiki", "token", tv.scope); err != nil || value != "changed-"+testSecret {
		t.Fatalf("Get() after Set() = %q, %v", value, err)
	}
	// A credential stored while the process runs is bound to no connection until the bindings say so.
	if err := client.Set(ctx, "crm", "token", "crm-secret"); err != nil {
		t.Fatal(err)
	}
	crm := vault.Scope{Connection: "crm", Credential: "crm", Provider: "x", Origin: "https://crm.example"}
	if _, _, err := client.Get(ctx, "crm", "token", crm); !errors.Is(err, vault.ErrApprovalRequired) {
		t.Fatalf("Get() of an unbound credential = %v, want ErrApprovalRequired", err)
	}
	if err := client.Bind(ctx, vault.Bindings{}); err != nil {
		t.Fatalf("Bind() = %v", err)
	}
	if _, _, err := client.Get(ctx, "wiki", "token", tv.scope); !errors.Is(err, vault.ErrApprovalRequired) {
		t.Fatalf("Get() after the approvals were dropped = %v, want ErrApprovalRequired", err)
	}
	bindings, err := tv.vault.Bindings()
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Bind(ctx, bindings); err != nil {
		t.Fatal(err)
	}
	if err := client.Delete(ctx, "wiki", "token"); err != nil {
		t.Fatalf("Delete() = %v", err)
	}
	if _, found, err := client.Get(ctx, "wiki", "token", tv.scope); err != nil || found {
		t.Fatalf("Get() after Delete() = %v, %v, want not found", found, err)
	}
	// The entry went with its last role, and so did its id: storing it again needs new bindings.
	if err := client.Set(ctx, "wiki", "token", testSecret); err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.Get(ctx, "wiki", "token", tv.scope); !errors.Is(err, vault.ErrApprovalRequired) {
		t.Fatalf("Get() of an entry stored again = %v, want ErrApprovalRequired", err)
	}
}

// Only a get restarts the idle period; a lock overwrites what the process holds, and a locked process
// answers nothing.
func TestServerTouchesOnGetAndLocks(t *testing.T) {
	tv := newTestVault(t)
	server, client := tv.serve(t, ServerOptions{IdleTimeout: time.Hour})
	ctx := withTimeout(t)
	first, err := client.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	if err := client.Check(ctx, tv.scope); err != nil {
		t.Fatal(err)
	}
	if st, _ := client.Status(ctx); !st.LocksAt.Equal(first.LocksAt) {
		t.Fatalf("a check moved the lock time from %v to %v", first.LocksAt, st.LocksAt)
	}
	if _, _, err := client.Get(ctx, "wiki", "token", tv.scope); err != nil {
		t.Fatal(err)
	}
	if st, _ := client.Status(ctx); !st.LocksAt.After(first.LocksAt) {
		t.Fatalf("a get left the lock time at %v", st.LocksAt)
	}
	if first.PID != os.Getpid() || first.Update != "" {
		t.Errorf("Status() = %+v, want this process and no update setting without a handover", first)
	}
	if err := client.Lock(ctx); err != nil {
		t.Fatalf("Lock() = %v", err)
	}
	select {
	case <-server.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the server still serves after the lock")
	}
	if _, _, err := client.Get(ctx, "wiki", "token", tv.scope); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("Get() after the lock = %v, want ErrNotRunning", err)
	}
}

// A process that hands over reports the update setting it reads from settings.age with its own key, as an
// operation of its own beside the kit's status.
func TestStatusReportsTheUpdateSetting(t *testing.T) {
	tv := newTestVault(t)
	signer := releasetest.NewSigner(t)
	_, client := tv.serve(t, ServerOptions{TrustedKeys: []ssh.PublicKey{signer.PublicKey()},
		ConfigPath: filepath.Join(tv.dir, "config.yaml")})
	ctx := withTimeout(t)
	if st, err := client.Status(ctx); err != nil || st.Update != vault.UpdateHandover || st.SettingsUntrusted {
		t.Fatalf("Status() = %+v, %v, want handover", st, err)
	}
	if err := tv.vault.SetUpdateBehaviour(vault.UpdateLock); err != nil {
		t.Fatal(err)
	}
	if st, err := client.Status(ctx); err != nil || st.Update != vault.UpdateLock {
		t.Fatalf("Status() = %+v, %v, want lock", st, err)
	}
	if err := os.WriteFile(filepath.Join(tv.vault.Dir(), "settings.age"), []byte("garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	if st, err := client.Status(ctx); err != nil || st.Update != vault.UpdateLock || !st.SettingsUntrusted {
		t.Fatalf("Status() with an untrusted settings.age = %+v, %v, want lock and untrusted", st, err)
	}
}

// The process signs the log it keeps with the log key it derives from the vault's key, checks stored lines
// without answering with the key, refuses invalid fields, and a process without a log refuses log requests.
func TestServerSignsTheLogItKeeps(t *testing.T) {
	tv := newTestVault(t)
	_, client := tv.serve(t, ServerOptions{KeepLog: true, RetentionDays: 90})
	ctx := withTimeout(t)
	entry := invokelog.Fields{Path: "mcp", Client: &invokelog.ClientInfo{Name: "client", Version: "1"},
		Operation: "bookstack.pages.list", Version: 1, Connection: "wiki", Effect: "read", Result: "success",
		Duration: 12 * time.Millisecond, Session: "fedcba9876543210", ReqBytes: new(int64), RespBytes: new(int64)}
	for i := 0; i < 3; i++ {
		if err := client.Log(ctx, entry); err != nil {
			t.Fatalf("Log() = %v", err)
		}
	}
	files, _ := filepath.Glob(filepath.Join(tv.vault.Dir(), "logs", "*.jsonl"))
	var lines [][]byte
	for _, file := range files {
		data, _ := os.ReadFile(file)
		for _, line := range bytes.Split(bytes.TrimRight(data, "\n"), []byte("\n")) {
			lines = append(lines, line)
		}
	}
	key, err := invokelog.DeriveKey(tv.key)
	if err != nil {
		t.Fatal(err)
	}
	report, err := invokelog.VerifyWith(tv.vault.Dir(), key)
	if err != nil || len(lines) != 3 || report.Broken || report.Days[0].Unverified != 0 {
		t.Fatalf("the log = %d lines, %+v, %v, want 3 signed entries", len(lines), report, err)
	}
	changed := bytes.Replace(lines[2], []byte(`"wiki"`), []byte(`"crm"`), 1)
	valid, err := client.CheckLog(ctx, [][]byte{lines[0], changed, []byte("{}")})
	if err != nil || len(valid) != 3 || !valid[0] || valid[1] || valid[2] {
		t.Fatalf("CheckLog() = %v, %v, want valid, changed, malformed", valid, err)
	}
	invalid := entry
	invalid.Path = "elsewhere"
	if err := client.Log(ctx, invalid); err == nil {
		t.Fatal("Log() of invalid fields succeeded")
	}

	other := newTestVault(t)
	_, without := other.serve(t, ServerOptions{})
	if err := without.Log(ctx, entry); !errors.Is(err, ErrNoLog) {
		t.Fatalf("Log() to a process without a log = %v, want ErrNoLog", err)
	}
}

// A process that approves with agent tokens tells the vault's names; one that does not refuses both.
func TestServerTellsNamesOnlyWithTheVaultDirectory(t *testing.T) {
	tv := newTestVault(t)
	_, client := tv.serve(t, ServerOptions{Tokens: true})
	ctx := withTimeout(t)
	names, err := client.Names(ctx)
	if err != nil {
		t.Fatalf("Names() = %v", err)
	}
	if _, ok := names.Entries["wiki"]; !ok {
		t.Fatalf("Names() = %+v, want the entry wiki", names)
	}
	if _, _, err := client.ApproveWithToken(ctx, "not-a-token", nil, nil, nil); !errors.Is(err, vault.ErrTokenUnknown) {
		t.Fatalf("ApproveWithToken() of an unknown token = %v, want ErrTokenUnknown", err)
	}

	other := newTestVault(t)
	_, without := other.serve(t, ServerOptions{})
	if _, err := without.Names(ctx); !errors.Is(err, ErrNoNames) {
		t.Fatalf("Names() = %v, want ErrNoNames", err)
	}
	if _, _, err := without.ApproveWithToken(ctx, "x", nil, nil, nil); !errors.Is(err, ErrNoTokens) {
		t.Fatalf("ApproveWithToken() = %v, want ErrNoTokens", err)
	}
}

// The process takes its idle time, maximum and log retention from settings.age, read with its own key, and
// from nothing else; an untrusted settings.age gives the defaults and never truncates the log.
func TestProcessSettingsComeFromSettingsAge(t *testing.T) {
	tv := newTestVault(t)
	if idle, maxUnlock, retention := processSettings(tv.vault.Dir(), tv.key); idle != vault.DefaultSettings().IdleTimeout ||
		maxUnlock != 0 || retention != vault.DefaultRetentionDays {
		t.Fatalf("processSettings() without settings.age = %v, %v, %d", idle, maxUnlock, retention)
	}
	settings := vault.DefaultSettings()
	settings.IdleTimeout, settings.MaxUnlock, settings.RetentionDays = time.Hour, 2*time.Hour, 7
	if err := tv.vault.SetSettings(settings); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tv.dir, "config.yaml"), []byte("logs:\n  retention_days: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if idle, maxUnlock, retention := processSettings(tv.vault.Dir(), tv.key); idle != time.Hour ||
		maxUnlock != 2*time.Hour || retention != 7 {
		t.Fatalf("processSettings() = %v, %v, %d, want settings.age's", idle, maxUnlock, retention)
	}
	if err := os.WriteFile(filepath.Join(tv.vault.Dir(), "settings.age"), []byte("garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	if idle, maxUnlock, retention := processSettings(tv.vault.Dir(), tv.key); idle != DefaultIdleTimeout ||
		maxUnlock != 0 || retention != 0 {
		t.Fatalf("processSettings() with an untrusted settings.age = %v, %v, %d", idle, maxUnlock, retention)
	}
}

// A handover that fails on a file over the limit is logged as before the kit: the archive, before the release
// verified, as release-unverified, and the installed program, after, as program-mismatch.
func TestHandoverOfATooLargeFileIsLoggedAsBefore(t *testing.T) {
	tv := newTestVault(t)
	p := newProcess(tv.key, processOptions{vaultDir: tv.vault.Dir(), keepLog: true, update: true})
	tooLarge := kitvaultproc.HandoverResult{Reason: kitvaultproc.HandoverTooLarge}
	_ = p.onHandover(nil, tooLarge)
	p.handsOver(tv.key)
	_ = p.onHandover(nil, tooLarge)
	_ = p.onHandover(nil, kitvaultproc.HandoverResult{Reason: kitvaultproc.HandoverDeclined})
	files, _ := filepath.Glob(filepath.Join(tv.vault.Dir(), "logs", "*.jsonl"))
	var log []byte
	for _, file := range files {
		data, _ := os.ReadFile(file)
		log = append(log, data...)
	}
	lines := bytes.Split(bytes.TrimRight(log, "\n"), []byte("\n"))
	if len(lines) != 2 || !bytes.Contains(lines[0], []byte(`"result":"release-unverified"`)) ||
		!bytes.Contains(lines[1], []byte(`"result":"program-mismatch"`)) {
		t.Fatalf("the log = %s, want release-unverified, then program-mismatch, and nothing for the declined one", log)
	}
}
