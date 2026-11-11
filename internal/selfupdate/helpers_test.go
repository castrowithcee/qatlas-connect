package selfupdate

import (
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-kit/release/releasetest"
	"golang.org/x/crypto/ssh"

	"github.com/castrowithcee/qatlas-connect/internal/release"
	"github.com/castrowithcee/qatlas-connect/internal/selfupdate/selfupdatetest"
)

// testArch is the architecture the test releases are built for, whatever the host's.
const testArch = "amd64"

// exe is the file name extension of a program on this platform.
func exe() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

func assertFile(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(got) != want {
		t.Fatalf("%s = %q, want %q", path, got, want)
	}
}

// assertRegular checks that path is a regular file with want.
func assertRegular(t *testing.T, path, want string) {
	t.Helper()
	if info, err := os.Lstat(path); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("%s is %v, %v, want a regular file", path, info, err)
	}
	assertFile(t, path, want)
}

func assertMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("%s exists: %v", path, err)
	}
}

// assertUntouched checks that the installation installedPrefix made is as it was: its program, its manual
// page, and nothing else in bin.
func assertUntouched(t *testing.T, prefix, executable string) {
	t.Helper()
	assertRegular(t, executable, "old-binary")
	assertRegular(t, manpageOf(prefix, filepath.Base(executable)), "old-manpage")
	if names := dirNames(t, filepath.Join(prefix, "bin")); len(names) != 1 {
		t.Errorf("bin holds %v, want only %s", names, filepath.Base(executable))
	}
}

// manpageOf is the manual page of program in prefix.
func manpageOf(prefix, program string) string {
	return filepath.Join(prefix, "share", "man", "man1", strings.TrimSuffix(program, ".exe")+".1")
}

// dirNames lists the names in dir.
func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// layoutOf is the layout of version.
func layoutOf(t testing.TB, version string) release.Layout {
	t.Helper()
	layout, err := release.LayoutOf(version)
	if err != nil {
		t.Fatal(err)
	}
	return layout
}

// programFiles are the program and, except on Windows, whose releases have none, the manual page of a
// release of version, under its layout's names.
func programFiles(t testing.TB, version, program, manpage string) map[string]string {
	t.Helper()
	layout := layoutOf(t, version)
	files := map[string]string{layout.ProgramFile(runtime.GOOS): program}
	if runtime.GOOS != "windows" {
		files[layout.ManpageFile()] = manpage
	}
	return files
}

// archiveName is the archive name of version for this platform and testArch.
func archiveName(t testing.TB, version string) string {
	t.Helper()
	return layoutOf(t, version).ArchiveName(version, runtime.GOOS, testArch)
}

// archiveWith returns the archive name of version for this platform and testArch, and an archive of the
// files, by path.
func archiveWith(t testing.TB, version string, files map[string]string) (string, []byte) {
	t.Helper()
	name := archiveName(t, version)
	var entries []releasetest.File
	for _, path := range slices.Sorted(maps.Keys(files)) {
		entries = append(entries, releasetest.File{Name: path, Body: []byte(files[path])})
	}
	return name, releasetest.Archive(t, name, entries...)
}

// published is the release tag with an archive of files, signed by signer in sets, or with the signature
// of its version when no set is named.
func published(t testing.TB, signer ssh.Signer, tag string, files map[string]string, sets ...selfupdatetest.Signature) selfupdatetest.Release {
	t.Helper()
	if sets == nil {
		sets = []selfupdatetest.Signature{selfupdatetest.SignatureOf(t, tag)}
	}
	name, archive := archiveWith(t, tag, files)
	return selfupdatetest.Signed(t, signer, tag, map[string][]byte{name: archive}, sets...)
}

// programRelease is published with the program and manual page "new-binary" and "new-manpage".
func programRelease(t testing.TB, signer ssh.Signer, tag string) selfupdatetest.Release {
	t.Helper()
	return published(t, signer, tag, programFiles(t, tag, "new-binary", "new-manpage"))
}

// listed is a release of tag with every file a list or a choice looks for, the archive, checksums.txt and
// the signature of its version, by name only: their bodies are no release, so nothing of it can be
// downloaded and verified.
func listed(t testing.TB, tag string) selfupdatetest.Release {
	t.Helper()
	return selfupdatetest.Release{Tag: tag, Prerelease: strings.Contains(tag, "-"), Assets: map[string][]byte{
		archiveName(t, tag): []byte("archive"), "checksums.txt": []byte("checksums"),
		selfupdatetest.SignatureOf(t, tag).Asset: []byte("signature"),
	}}
}

// unsigned is rel without the signature of its version.
func unsigned(t testing.TB, rel selfupdatetest.Release) selfupdatetest.Release {
	t.Helper()
	rel.Assets = maps.Clone(rel.Assets)
	delete(rel.Assets, selfupdatetest.SignatureOf(t, rel.Tag).Asset)
	return rel
}

// draft is rel as a draft.
func draft(rel selfupdatetest.Release) selfupdatetest.Release {
	rel.Draft = true
	return rel
}

// clientFor is an updater of the running version installed at executable, reading server and trusting
// signer.
func clientFor(server *selfupdatetest.Server, signer ssh.Signer, version, executable string) *Client {
	return &Client{BaseURL: server.RepositoryAPI(), HTTPClient: server.Client(), Version: version, GOARCH: testArch,
		Executable: executable, SigningKeys: []ssh.PublicKey{signer.PublicKey()}}
}

// forbidReplace makes BeforeReplace of client fail the test: nothing is to be replaced.
func forbidReplace(t *testing.T, client *Client) {
	client.BeforeReplace = func(context.Context, Release) error {
		t.Error("BeforeReplace ran although nothing is replaced")
		return nil
	}
}

// installedPrefix is an installation of qconnect: the program "old-binary" and its manual page
// "old-manpage".
func installedPrefix(t *testing.T) (prefix, executable string) {
	t.Helper()
	return installedPrefixNamed(t, currentName)
}

// installedPrefixNamed is installedPrefix under the program name program, with the platform's extension.
// The manual page is there on Windows too, where an installation leaves it alone.
func installedPrefixNamed(t *testing.T, program string) (prefix, executable string) {
	t.Helper()
	prefix = t.TempDir()
	executable = filepath.Join(prefix, "bin", program+exe())
	for path, body := range map[string]string{executable: "old-binary", manpageOf(prefix, program): "old-manpage"} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return prefix, executable
}
