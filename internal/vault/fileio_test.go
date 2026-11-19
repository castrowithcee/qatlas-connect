package vault

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-kit/fileretry"
)

// seamFile replaces the retry seam for one test. A directory standing where the file belongs stands in for
// a lock that another process releases.
func seamFile(t *testing.T, transient bool, onSleep func(n int)) *int {
	t.Helper()
	oldTransient, oldSleep := transientFile, sleepFile
	t.Cleanup(func() { transientFile, sleepFile = oldTransient, oldSleep })
	slept := 0
	transientFile = func(error) bool { return transient }
	sleepFile = func(time.Duration) {
		slept++
		if onSleep != nil {
			onSleep(slept)
		}
	}
	return &slept
}

func blockedByDir(t *testing.T) (string, func()) {
	t.Helper()
	path := filepath.Join(t.TempDir(), secretsFile)
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	return path, func() {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
}

func noTempFiles(t *testing.T, dir string) {
	t.Helper()
	left, _ := filepath.Glob(filepath.Join(dir, ".qatlas-vault-*.tmp"))
	if len(left) != 0 {
		t.Errorf("temporary files left behind: %v", left)
	}
}

func TestReadFileRetriesTransientFailureUntilSuccess(t *testing.T) {
	path, free := blockedByDir(t)
	want := []byte("synthetic-vault-body")
	slept := seamFile(t, true, func(n int) {
		if n == 2 {
			free()
			if err := os.WriteFile(path, want, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	})
	got, err := readFile(path)
	if err != nil {
		t.Fatalf("readFile: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("read %q, want %q", got, want)
	}
	if *slept != 2 {
		t.Errorf("waited %d times, want 2", *slept)
	}
}

func TestWriteFileRetriesTransientReplaceUntilSuccess(t *testing.T) {
	path, free := blockedByDir(t)
	slept := seamFile(t, true, func(n int) {
		if n == 3 {
			free()
		}
	})
	if err := writeFile(path, []byte("synthetic-vault-body")); err != nil {
		t.Fatal(err)
	}
	if *slept != 3 {
		t.Errorf("waited %d times, want 3", *slept)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "synthetic-vault-body" {
		t.Errorf("file = %q, %v", got, err)
	}
	noTempFiles(t, filepath.Dir(path))
}

func TestWriteFileGivesUpAfterBudgetAndCleansUp(t *testing.T) {
	path, _ := blockedByDir(t)
	slept := seamFile(t, true, nil)
	err := writeFile(path, []byte("synthetic-vault-body"))
	if err == nil || !strings.HasPrefix(err.Error(), "cannot replace ") ||
		!strings.Contains(err.Error(), filepath.Base(path)) {
		t.Fatalf("err = %v, want cannot replace", err)
	}
	if *slept != len(fileretry.DefaultDelays()) {
		t.Errorf("waited %d times, want %d", *slept, len(fileretry.DefaultDelays()))
	}
	if fi, err := os.Stat(path); err != nil || !fi.IsDir() {
		t.Errorf("target changed: %v, %v", fi, err)
	}
	noTempFiles(t, filepath.Dir(path))
}

func TestPermanentFileFailureIsNotRetried(t *testing.T) {
	path, _ := blockedByDir(t)
	slept := seamFile(t, false, nil)
	if err := writeFile(path, []byte("synthetic-vault-body")); err == nil {
		t.Fatal("writeFile succeeded")
	}
	if _, err := readFile(path); err == nil {
		t.Fatal("readFile succeeded")
	}
	if *slept != 0 {
		t.Errorf("waited %d times, want none", *slept)
	}
	noTempFiles(t, filepath.Dir(path))
}

func TestMissingFileIsNeverRetried(t *testing.T) {
	slept := seamFile(t, true, nil)
	if _, err := readFile(filepath.Join(t.TempDir(), secretsFile)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("err = %v, want not exist", err)
	}
	if *slept != 0 {
		t.Errorf("waited %d times, want none", *slept)
	}
}
