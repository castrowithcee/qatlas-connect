package secret

import (
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

func blockedByDir(t *testing.T) (*File, func()) {
	t.Helper()
	path := filepath.Join(t.TempDir(), FileName)
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	return NewFile(path), func() {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
}

func noTempFiles(t *testing.T, dir string) {
	t.Helper()
	left, _ := filepath.Glob(filepath.Join(dir, ".qatlas-credentials-*.tmp"))
	if len(left) != 0 {
		t.Errorf("temporary files left behind: %v", left)
	}
}

func fixtureContent() content {
	return content{Version: fileVersion, AllowPlaintext: true,
		Credentials: map[string]map[string]string{credName: {role: canaryPlaintext}}}
}

func TestLoadRetriesTransientFailureUntilSuccess(t *testing.T) {
	good := tempFile(t)
	setFixture(t, good, credName, role, canaryPlaintext)
	data, err := os.ReadFile(good.Path())
	if err != nil {
		t.Fatal(err)
	}

	f, free := blockedByDir(t)
	slept := seamFile(t, true, func(n int) {
		if n == 2 {
			free()
			if err := os.WriteFile(f.Path(), data, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	})
	if _, err := f.load(); err != nil {
		t.Fatalf("load: %v", err)
	}
	if *slept != 2 {
		t.Errorf("waited %d times, want 2", *slept)
	}
}

func TestWriteRetriesTransientReplaceUntilSuccess(t *testing.T) {
	f, free := blockedByDir(t)
	slept := seamFile(t, true, func(n int) {
		if n == 3 {
			free()
		}
	})
	if err := f.write(fixtureContent()); err != nil {
		t.Fatal(err)
	}
	if *slept != 3 {
		t.Errorf("waited %d times, want 3", *slept)
	}
	if got, err := f.Get(credName, role); err != nil || got != canaryPlaintext {
		t.Errorf("Get = %q, %v", got, err)
	}
	noTempFiles(t, filepath.Dir(f.Path()))
}

func TestWriteGivesUpAfterBudgetAndCleansUp(t *testing.T) {
	f, _ := blockedByDir(t)
	slept := seamFile(t, true, nil)
	err := f.write(fixtureContent())
	if err == nil || !strings.HasPrefix(err.Error(), "cannot replace ") ||
		!strings.Contains(err.Error(), FileName) {
		t.Fatalf("err = %v, want cannot replace", err)
	}
	if *slept != len(fileretry.DefaultDelays()) {
		t.Errorf("waited %d times, want %d", *slept, len(fileretry.DefaultDelays()))
	}
	if fi, err := os.Stat(f.Path()); err != nil || !fi.IsDir() {
		t.Errorf("target changed: %v, %v", fi, err)
	}
	noTempFiles(t, filepath.Dir(f.Path()))
}

func TestPermanentFileFailureIsNotRetried(t *testing.T) {
	f, _ := blockedByDir(t)
	slept := seamFile(t, false, nil)
	if err := f.write(fixtureContent()); err == nil {
		t.Fatal("write succeeded")
	}
	if _, err := f.load(); err == nil || errors.Is(err, ErrDisabled) {
		t.Fatalf("load err = %v, want a read error", err)
	}
	if *slept != 0 {
		t.Errorf("waited %d times, want none", *slept)
	}
	noTempFiles(t, filepath.Dir(f.Path()))
}

func TestMissingFileIsNeverRetried(t *testing.T) {
	f := tempFile(t)
	slept := seamFile(t, true, nil)
	if _, err := f.load(); !errors.Is(err, ErrDisabled) {
		t.Errorf("load err = %v, want ErrDisabled", err)
	}
	if *slept != 0 {
		t.Errorf("waited %d times, want none", *slept)
	}
}
