//go:build linux || darwin

package selfupdate

import (
	"os"
	"testing"
)

// assertAlias checks that path is a link to qconnect beside it.
func assertAlias(t *testing.T, path string) {
	t.Helper()
	assertLink(t, path, currentName)
}

// assertLink checks that path is a link to target beside it.
func assertLink(t *testing.T, path, target string) {
	t.Helper()
	if got, err := os.Readlink(path); err != nil || got != target {
		t.Fatalf("%s links to %q (%v), want %s", path, got, err, target)
	}
}
