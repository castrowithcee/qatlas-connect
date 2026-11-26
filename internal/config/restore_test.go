package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestBackupsListConfigBackupsNewestFirst(t *testing.T) {
	store, _ := newTarget(t)
	dir := store.backupDir()
	must(t, os.MkdirAll(dir, 0o700))
	for _, name := range []string{
		"config-20260101T000000Z-000.yaml",
		"config-20260103T000000Z-001.yaml",
		"config-20260103T000000Z-002-hand.yaml",
		"config-first-hand-20251201T000000Z-000.yaml",
		"credentials-20260105T000000Z-000.bak", // a vault copy
		"config-notes.yaml",
		"notes.txt",
	} {
		must(t, os.WriteFile(filepath.Join(dir, name), []byte(name), 0o600))
	}
	got, err := store.Backups()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"config-20260103T000000Z-002-hand.yaml", "config-20260103T000000Z-001.yaml",
		"config-20260101T000000Z-000.yaml", "config-first-hand-20251201T000000Z-000.yaml",
	}
	if len(got) != len(want) {
		t.Fatalf("Backups() = %v, want %v", got, want)
	}
	for i, b := range got {
		if b.Name != want[i] {
			t.Errorf("Backups()[%d] = %s, want %s", i, b.Name, want[i])
		}
	}
	if y, m, d := got[1].Time.Date(); y != 2026 || m != 1 || d != 3 {
		t.Errorf("time = %v", got[1].Time)
	}
	data, err := store.ReadBackup(got[0].Name)
	if err != nil || string(data) != want[0] {
		t.Errorf("ReadBackup() = %q, %v", data, err)
	}
}

func TestBackupsOfAMissingDirectoryAreNone(t *testing.T) {
	store, _ := newTarget(t)
	if got, err := store.Backups(); err != nil || len(got) != 0 {
		t.Fatalf("Backups() = %v, %v", got, err)
	}
}

func TestRestoreBackupSavesTheCurrentFileAndUndoPutsItBack(t *testing.T) {
	store, path := newTarget(t)
	must(t, store.Save(sample(t)))
	saveN(t, store, 2)
	before := mustRead(t, path)
	backups, err := store.Backups()
	if err != nil || len(backups) == 0 {
		t.Fatalf("Backups() = %v, %v", backups, err)
	}
	oldest := backups[len(backups)-1].Name
	older, err := store.ReadBackup(oldest)
	must(t, err)
	if bytes.Equal(older, before) {
		t.Fatal("the fixture needs a backup that differs from the file")
	}
	_, rev, err := store.Snapshot()
	must(t, err)

	restored, err := store.RestoreBackup(oldest, rev)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(mustRead(t, path), older) {
		t.Error("the file is not the restored backup")
	}
	if !bytes.Equal(mustRead(t, store.BackupPath(restored.Previous)), before) {
		t.Error("the new backup does not hold the replaced file")
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
			t.Errorf("mode = %v", fi.Mode().Perm())
		}
	}
	count := len(backupNames(t, store))
	must(t, store.UndoRestore(restored))
	if !bytes.Equal(mustRead(t, path), before) {
		t.Error("UndoRestore did not put the file back")
	}
	if got := len(backupNames(t, store)); got != count {
		t.Errorf("UndoRestore made a backup: %d -> %d", count, got)
	}
}

func TestRestoreBackupRefusesAChangedFile(t *testing.T) {
	store, path := newTarget(t)
	must(t, store.Save(sample(t)))
	_, rev, err := store.Snapshot()
	must(t, err)
	saveN(t, store, 1)
	changed := mustRead(t, path)
	names := backupNames(t, store)
	if _, err := store.RestoreBackup(names[0], rev); !errors.Is(err, ErrConflict) {
		t.Fatalf("RestoreBackup() error = %v, want a conflict", err)
	}
	if !bytes.Equal(mustRead(t, path), changed) || len(backupNames(t, store)) != len(names) {
		t.Error("a refused restore changed something")
	}
}

func TestSnapshotOfAMissingFile(t *testing.T) {
	store, _ := newTarget(t)
	var nf *NotFoundError
	if _, _, err := store.Snapshot(); !errors.As(err, &nf) {
		t.Fatalf("Snapshot() error = %v", err)
	}
}

func TestUndoRestoreLeavesALaterChange(t *testing.T) {
	store, path := newTarget(t)
	must(t, store.Save(sample(t)))
	saveN(t, store, 1)
	backups, err := store.Backups()
	must(t, err)
	_, rev, err := store.Snapshot()
	must(t, err)
	restored, err := store.RestoreBackup(backups[0].Name, rev)
	must(t, err)
	saveN(t, store, 1)
	changed := mustRead(t, path)

	if err := store.UndoRestore(restored); !errors.Is(err, ErrConflict) {
		t.Fatalf("UndoRestore() error = %v, want a conflict", err)
	}
	if !bytes.Equal(mustRead(t, path), changed) {
		t.Error("UndoRestore overwrote a later change")
	}
}

// Backups named by earlier releases are listed and restored as they are, without being renamed.
func TestBackupsOfEarlierReleasesRestoreWithoutRenaming(t *testing.T) {
	store, path := newTarget(t)
	must(t, store.Save(sample(t)))
	dir := store.backupDir()
	must(t, os.MkdirAll(dir, 0o700))
	names := []string{
		"config-20250101T000000Z-000.yaml",
		"config-20250102T000000Z-003-hand.yaml",
		"config-first-hand-20241201T000000Z-000.yaml",
	}
	for i, name := range names {
		must(t, os.WriteFile(filepath.Join(dir, name), []byte(fmt.Sprintf("version: 4\n# backup %d\n", i)), 0o600))
	}
	for i, name := range names {
		_, rev, err := store.Snapshot()
		must(t, err)
		if _, err := store.RestoreBackup(name, rev); err != nil {
			t.Fatalf("RestoreBackup(%s) = %v", name, err)
		}
		if got := string(mustRead(t, path)); got != fmt.Sprintf("version: 4\n# backup %d\n", i) {
			t.Errorf("after restoring %s the file holds %q", name, got)
		}
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("the restored backup %s is gone: %v", name, err)
		}
	}
	listed := map[string]bool{}
	backups, err := store.Backups()
	must(t, err)
	for _, b := range backups {
		listed[b.Name] = true
	}
	for _, name := range names {
		if !listed[name] {
			t.Errorf("%s is not listed after the restores: %v", name, backups)
		}
	}
	if len(backups) != 2*len(names) {
		t.Errorf("backups = %v, want the three earlier ones plus one per restore", backups)
	}
}

// A configuration as the last release writes and reads it loads, is backed up by a save and comes back
// byte for byte from that backup.
func TestConfigOfTheLastReleaseLoadsBacksUpAndRestores(t *testing.T) {
	release, err := os.ReadFile(validFixture)
	must(t, err)
	store, path := newTarget(t)
	must(t, os.MkdirAll(filepath.Dir(path), 0o700))
	must(t, os.WriteFile(path, release, 0o600))

	cfg, err := store.Load()
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	must(t, cfg.SetProviderNote("bookstack", "team wiki"))
	must(t, store.Save(cfg))
	backups, err := store.Backups()
	if err != nil || len(backups) != 1 || !bytes.Equal(mustRead(t, store.BackupPath(backups[0].Name)), release) {
		t.Fatalf("Backups() = %v, %v, want one backup of the release file", backups, err)
	}
	_, rev, err := store.Snapshot()
	must(t, err)
	if _, err := store.RestoreBackup(backups[0].Name, rev); err != nil {
		t.Fatalf("RestoreBackup() = %v", err)
	}
	if !bytes.Equal(mustRead(t, path), release) {
		t.Error("the restored file differs from the release file")
	}
	if _, err := store.Load(); err != nil {
		t.Errorf("the restored file does not load: %v", err)
	}
}

// Every replacement leaves the file private, whatever permissions the replaced file had.
func TestReplacedFileIsPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes do not carry on Windows")
	}
	store, path := newTarget(t)
	must(t, store.Save(sample(t)))
	var restored Restored
	for _, step := range []struct {
		name    string
		replace func() error
	}{
		{"Save", func() error { saveN(t, store, 1); return nil }},
		{"RestoreBackup", func() error {
			backups, err := store.Backups()
			must(t, err)
			_, rev, err := store.Snapshot()
			must(t, err)
			restored, err = store.RestoreBackup(backups[0].Name, rev)
			return err
		}},
		{"UndoRestore", func() error { return store.UndoRestore(restored) }},
	} {
		must(t, os.Chmod(path, 0o644))
		if err := step.replace(); err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}
		if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
			t.Errorf("%s: mode = %v, %v, want 600", step.name, fi.Mode().Perm(), err)
		}
	}
}
