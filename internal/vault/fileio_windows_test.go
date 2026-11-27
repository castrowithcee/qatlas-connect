//go:build windows

package vault

import (
	"bytes"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// The handles below are held without FILE_SHARE_DELETE (os.Open) or without any sharing at all, which is
// what a concurrent reader or replacer of a vault file looks like to this process.

func holdOpen(t *testing.T, path string) *os.File {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func holdExclusive(t *testing.T, path string) windows.Handle {
	t.Helper()
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ, 0, nil, windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// releaseAfter calls release after d, and again at cleanup in case the test ends first.
func releaseAfter(t *testing.T, d time.Duration, release func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		time.Sleep(d)
		release()
		close(done)
	}()
	t.Cleanup(func() { <-done })
}

const heldPassphrase = "synthetic-held-phrase"

// heldVault returns an unlocked encrypted vault below a fresh directory that holds one entry.
func heldVault(t *testing.T) (*Vault, string) {
	t.Helper()
	lowWorkFactor(t)
	dir := t.TempDir()
	v := New(dir)
	if err := v.Set("wiki-reader", "role-0", "synthetic-held-0", offering(heldPassphrase)); err != nil {
		t.Fatal(err)
	}
	return v, dir
}

func TestWritesWaitForConcurrentReader(t *testing.T) {
	v, dir := heldVault(t)
	path := v.secretsPath()

	t.Run("Set", func(t *testing.T) {
		f := holdOpen(t, path)
		releaseAfter(t, 200*time.Millisecond, func() { _ = f.Close() })
		if err := v.Set("wiki-reader", "role-1", "synthetic-held-1", nil); err != nil {
			t.Fatalf("Set: %v", err)
		}
	})
	t.Run("ReplaceRole", func(t *testing.T) {
		f := holdOpen(t, path)
		releaseAfter(t, 200*time.Millisecond, func() { _ = f.Close() })
		current, replaced, err := v.ReplaceRole("wiki-reader", "role-1", "synthetic-held-1", "synthetic-held-2")
		if err != nil || !replaced || current != "synthetic-held-2" {
			t.Fatalf("ReplaceRole = %q, %v, %v", current, replaced, err)
		}
	})
	t.Run("Approve", func(t *testing.T) {
		f := holdOpen(t, path)
		releaseAfter(t, 200*time.Millisecond, func() { _ = f.Close() })
		if err := v.Approve([]Scope{wikiScope()}); err != nil {
			t.Fatalf("Approve: %v", err)
		}
	})

	reread := New(dir)
	got, found, _, err := reread.Get("wiki-reader", "role-1", offering(heldPassphrase))
	if err != nil || !found || got != "synthetic-held-2" {
		t.Fatalf("Get after the writes = %q, %v, %v", got, found, err)
	}
	if err := reread.CheckApproval(wikiScope()); err != nil {
		t.Errorf("the approval was not saved: %v", err)
	}
	noTempFiles(t, v.Dir())
}

func TestReadsWaitForExclusiveHandle(t *testing.T) {
	v, dir := heldVault(t)

	t.Run("Get", func(t *testing.T) {
		h := holdExclusive(t, v.secretsPath())
		releaseAfter(t, 200*time.Millisecond, func() { _ = windows.CloseHandle(h) })
		got, found, _, err := New(dir).Get("wiki-reader", "role-0", offering(heldPassphrase))
		if err != nil || !found || got != "synthetic-held-0" {
			t.Fatalf("Get = %q, %v, %v", got, found, err)
		}
	})
	t.Run("Unlock", func(t *testing.T) {
		h := holdExclusive(t, v.keyPath())
		releaseAfter(t, 200*time.Millisecond, func() { _ = windows.CloseHandle(h) })
		if _, err := New(dir).Unlock(heldPassphrase); err != nil {
			t.Fatalf("Unlock: %v", err)
		}
	})
}

func TestWriteFailsCleanlyWhenFileStaysOpen(t *testing.T) {
	v, _ := heldVault(t)
	path := v.secretsPath()
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// The kit waits out its whole retry budget, under three seconds, before it gives up.
	f := holdOpen(t, path)
	setErr := v.Set("wiki-reader", "role-1", "synthetic-held-1", nil)
	_ = f.Close()

	if setErr == nil {
		t.Fatal("Set succeeded although the file stayed open")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("the target changed")
	}
	noTempFiles(t, v.Dir())
}
