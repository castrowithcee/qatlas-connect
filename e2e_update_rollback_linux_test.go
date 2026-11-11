//go:build linux

package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/castrowithcee/qatlas-kit/release/releasetest"

	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-connect/internal/selfupdate/selfupdatetest"
)

// TestUpdateToAnOlderVersionRestoresABackupItReads rolls real binaries back against a local signed test
// release. The configuration carries a schema version the older program does not know, so it cannot read
// it: without --restore-backup nothing changes, with it the newest backup the older program reads takes the
// configuration's place, the current file is kept as a new backup, and the installed program is the older one.
func TestUpdateToAnOlderVersionRestoresABackupItReads(t *testing.T) {
	if testing.Short() {
		t.Skip("the acceptance run builds the binary twice")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "prefix", "bin", "qconnect")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}

	signer := releasetest.NewSigner(t)
	keyLine := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))
	releases := selfupdatetest.NewServer(t)

	build := func(path, version string) {
		t.Helper()
		// Stripped like a release build: the updater refuses a program over its size limit.
		cmd := exec.Command("go", "build", "-tags", "e2e", "-buildvcs=false", "-trimpath", "-ldflags",
			"-s -w -X github.com/castrowithcee/qatlas-connect/internal/cli.version="+version+
				" -X 'github.com/castrowithcee/qatlas-connect/internal/release.extraKeyLines="+keyLine+"'"+
				" -X github.com/castrowithcee/qatlas-connect/internal/selfupdate.testBaseURL="+releases.RepositoryAPI(),
			"-o", path, ".")
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("building the binary: %v", err)
		}
	}
	build(bin, "v3.1.0")
	older := filepath.Join(dir, "older")
	build(older, "v3.0.0")
	program, err := os.ReadFile(older)
	if err != nil {
		t.Fatal(err)
	}
	releases.Set(selfupdatetest.Program(t, signer, "v3.0.0", "linux", runtime.GOARCH, program))

	const goodConfig = `version: 4
services:
  wiki:
    provider: bookstack
    base_url: http://127.0.0.1:1
credentials:
  vault-reader:
    type: vault
connections:
  wiki:
    service: wiki
    credential: vault-reader
defaults:
  connections:
    bookstack: wiki
`
	const currentConfig = "version: 99\n# changes made with the newer version\n"
	configPath := filepath.Join(dir, "config.yaml")
	backups := filepath.Join(dir, "backups")
	if err := os.MkdirAll(backups, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"config-20260201T100000Z-000.yaml": goodConfig,
		"config-20260301T100000Z-000.yaml": "version: 99\n",
	} {
		if err := os.WriteFile(filepath.Join(backups, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(configPath, []byte(currentConfig), 0o600); err != nil {
		t.Fatal(err)
	}

	var seen strings.Builder
	c := &runner{bin: bin, seen: &seen, env: []string{
		"HOME=" + dir, "PATH=" + os.Getenv("PATH"), "QCONNECT_CONFIG=" + configPath, secret.StoreSelector + "=none",
	}}
	installed := func() []byte {
		data, _ := os.ReadFile(bin)
		return data
	}
	original := installed()

	// Without the flag the rollback offers the backup and changes nothing.
	code, _, stderr := c.run(t, "update", "--to", "v3.0.0", "--yes", "--confirm")
	if code == 0 || !strings.Contains(stderr, "config-20260201T100000Z-000.yaml") ||
		!strings.Contains(stderr, "--restore-backup") || !strings.Contains(stderr, "nothing was changed") {
		t.Fatalf("update without the flag: exit %d, stderr %q", code, stderr)
	}
	if !bytes.Equal(installed(), original) {
		t.Fatal("the program was replaced without the restore confirmation")
	}
	if got, _ := os.ReadFile(configPath); string(got) != currentConfig {
		t.Fatalf("the configuration changed: %q", got)
	}
	if entries, _ := os.ReadDir(backups); len(entries) != 2 {
		t.Fatalf("the backups changed: %d files", len(entries))
	}

	code, stdout, stderr := c.run(t, "update", "--to", "v3.0.0", "--yes", "--confirm", "--restore-backup", "--output", "json")
	if code != 0 || !strings.Contains(stdout, `"updated":true`) || !strings.Contains(stdout, `"latest":"v3.0.0"`) {
		t.Fatalf("update with the flag: exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	if !bytes.Equal(installed(), program) {
		t.Fatal("the installed program is not the older release")
	}
	if got, _ := os.ReadFile(configPath); string(got) != goodConfig {
		t.Errorf("the configuration is not the restored backup: %q", got)
	}
	kept := false
	entries, _ := os.ReadDir(backups)
	for _, e := range entries {
		if data, _ := os.ReadFile(filepath.Join(backups, e.Name())); string(data) == currentConfig {
			kept = true
		}
	}
	if !kept {
		t.Error("the configuration that was replaced is not among the backups")
	}
	// The older program, now installed, reads the restored file.
	if code, _, stderr := c.run(t, "config", "validate"); code != 0 {
		t.Errorf("config validate with the older program: exit %d, stderr %q", code, stderr)
	}
}
