package vault

import (
	"os"
	"path/filepath"
	"testing"
)

// noTempFiles checks that no temporary file of an atomic replace is left in dir.
func noTempFiles(t *testing.T, dir string) {
	t.Helper()
	left, _ := filepath.Glob(filepath.Join(dir, ".*.tmp"))
	if len(left) != 0 {
		t.Errorf("temporary files left behind: %v", left)
	}
}

// Waiting out a file another process holds for a moment is the kit's; the vault has to ask for it, and for
// the write lock every release of this vault shares.
func TestVaultRetriesFilesAndSharesTheWriteLock(t *testing.T) {
	opts := options(t.TempDir())
	if !opts.RetryFiles {
		t.Error("the vault does not retry a file another process holds for a moment")
	}
	if opts.Files.Lock != ".secrets.lock" {
		t.Errorf("lock file = %q, want .secrets.lock", opts.Files.Lock)
	}
	for _, encrypted := range []bool{false, true} {
		e := newConcEnv(t, encrypted)
		info, err := os.Stat(filepath.Join(e.a.Dir(), ".secrets.lock"))
		if err != nil || !info.Mode().IsRegular() {
			t.Errorf("encrypted=%v: lock file after a write = %v, %v", encrypted, info, err)
		}
		noTempFiles(t, e.a.Dir())
	}
}
