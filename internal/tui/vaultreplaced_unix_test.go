//go:build linux || darwin

package tui

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/vaultproc"
	"github.com/castrowithcee/qatlas-connect/internal/vaultproc/vaultproctest"
)

// 'ctrl+l' unlocking while a vault process with a replaced program answers ends that process, tells so with
// its id, and starts a new one, without asking anything but the passphrase. The process stands in a process
// of its own, as another process of this user behind the vault's channel.
func TestCtrlLEndsAReplacedVaultProcessBeforeStartingOne(t *testing.T) {
	const passphrase = "hunter2"
	dir := filepath.Join(t.TempDir(), "qconnect")
	path := filepath.Join(dir, "config.yaml")
	store := newTestStore(t, path)
	setup, _ := newVaultResolver(t, dir)
	mustNoError(t, setup.SetVault("other", "role", "canary-seed", func(string) (string, error) { return passphrase, nil }))
	locked, _ := newVaultResolver(t, dir)
	runtimeDir, err := os.MkdirTemp("", "qv")
	if err != nil {
		t.Fatalf("MkdirTemp() = %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(runtimeDir) })
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	withStartableVaultProcess(t)

	m, err := buildModel(store, nil, locked, nil)
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	replaced := vaultproctest.Start(t, m.secrets.Vault().Dir(), "", vaultproctest.Answer("replaced"))

	openSectionByName(t, m, sectionServices)
	pump(t, m, "ctrl+l")
	typeText(t, m, passphrase)
	pump(t, m, "enter")
	if m.fail != "" {
		t.Fatalf("unlocking reported %q", m.fail)
	}
	if !replaced.Ended(5 * time.Second) {
		t.Fatal("the replaced vault process was not ended")
	}
	if want := "Ended the vault process (pid " + strconv.Itoa(replaced.PID) + ")"; !strings.Contains(m.status, want) {
		t.Errorf("status = %q, want %q", m.status, want)
	}
	client, err := vaultproc.ProcessClientOf(m.secrets.Vault())
	if err != nil {
		t.Fatal(err)
	}
	if status, err := client.Status(t.Context()); err != nil || status.PID != os.Getpid() {
		t.Errorf("Status() = %+v, %v, want the new vault process answering", status, err)
	}
}
