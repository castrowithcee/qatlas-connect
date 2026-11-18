//go:build windows

package secret

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// The handles below are held without FILE_SHARE_DELETE (os.Open) or without any sharing at all, which is
// what a concurrent reader or replacer of the credential file looks like to this process.

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

func TestWriteWaitsForConcurrentReader(t *testing.T) {
	f := tempFile(t)
	setFixture(t, f, credName, role, canaryPlaintext)
	h := holdOpen(t, f.Path())
	releaseAfter(t, 200*time.Millisecond, func() { _ = h.Close() })

	c := fixtureContent()
	c.Credentials[credName][role] = canaryPlaintext + "-next"
	if err := f.write(c); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got, err := f.Get(credName, role); err != nil || got != canaryPlaintext+"-next" {
		t.Errorf("Get = %q, %v", got, err)
	}
	noTempFiles(t, filepath.Dir(f.Path()))
}

func TestReadWaitsForExclusiveHandle(t *testing.T) {
	f := tempFile(t)
	setFixture(t, f, credName, role, canaryPlaintext)
	h := holdExclusive(t, f.Path())
	releaseAfter(t, 200*time.Millisecond, func() { _ = windows.CloseHandle(h) })

	if got, err := f.Get(credName, role); err != nil || got != canaryPlaintext {
		t.Fatalf("Get = %q, %v", got, err)
	}
}

func TestWriteFailsCleanlyWhenFileStaysOpen(t *testing.T) {
	f := tempFile(t)
	setFixture(t, f, credName, role, canaryPlaintext)
	before, err := os.ReadFile(f.Path())
	if err != nil {
		t.Fatal(err)
	}
	oldSleep := sleepFile
	t.Cleanup(func() { sleepFile = oldSleep })
	sleepFile = func(d time.Duration) { time.Sleep(d / 100) }

	h := holdOpen(t, f.Path())
	c := fixtureContent()
	c.Credentials[credName][role] = canaryPlaintext + "-next"
	writeErr := f.write(c)
	_ = h.Close()

	if writeErr == nil {
		t.Fatal("write succeeded although the file stayed open")
	}
	after, err := os.ReadFile(f.Path())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("the target changed")
	}
	noTempFiles(t, filepath.Dir(f.Path()))
}
