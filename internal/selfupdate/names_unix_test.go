//go:build linux || darwin

package selfupdate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-kit/release/releasetest"

	"github.com/castrowithcee/qatlas-connect/internal/selfupdate/selfupdatetest"
)

// An installation the former updater left as bin/qatlas is updated under both names: the program goes to
// qconnect with its manual page, and qatlas becomes a link to it. Started through the alias, the next update
// replaces qconnect and keeps the alias.
func TestUpdateOfAFormerInstallationKeepsBothNames(t *testing.T) {
	signer := releasetest.NewSigner(t)
	server := selfupdatetest.NewServer(t, programRelease(t, signer, "v3.1.0"))
	prefix, former := installedPrefixNamed(t, "qatlas")
	if result, err := clientFor(server, signer, "v3.0.0", former).Update(context.Background()); err != nil || !result.Updated {
		t.Fatalf("Update() = %+v, %v", result, err)
	}
	current := filepath.Join(prefix, "bin", "qconnect")
	assertRegular(t, current, "new-binary")
	assertAlias(t, former)
	assertFile(t, filepath.Join(prefix, "share", "man", "man1", "qconnect.1"), "new-manpage")

	server.Set(published(t, signer, "v3.2.0", programFiles(t, "v3.2.0", "newer-binary", "newer-manpage")))
	if result, err := clientFor(server, signer, "v3.1.0", former).Update(context.Background()); err != nil || !result.Updated {
		t.Fatalf("Update() through the alias = %+v, %v", result, err)
	}
	assertRegular(t, current, "newer-binary")
	assertAlias(t, former)
}

// A link named qatlas to anything but qconnect beside it is no alias: the installation is refused.
func TestUpdateRefusesAForeignLink(t *testing.T) {
	signer := releasetest.NewSigner(t)
	server := selfupdatetest.NewServer(t, programRelease(t, signer, "v3.1.0"))
	_, executable := installedPrefix(t)
	link := filepath.Join(filepath.Dir(executable), "qatlas")
	if err := os.Symlink(executable, link); err != nil {
		t.Fatal(err)
	}
	var unsupported *UnsupportedInstallationError
	if _, err := clientFor(server, signer, "v3.0.0", link).Update(context.Background()); !errors.As(err, &unsupported) {
		t.Fatalf("Update() error = %v, want an unsupported installation", err)
	}
	assertFile(t, executable, "old-binary")
}

// renamedInstallation is qconnect with its manual page and, until the next major release, qatlas and
// qatlas.1 as links to them, as an installation of a release after the rename has it.
func renamedInstallation(t *testing.T) (prefix, executable string) {
	t.Helper()
	prefix, executable = installedPrefix(t)
	for path, target := range map[string]string{
		filepath.Join(prefix, "bin", "qatlas"):                    "qconnect",
		filepath.Join(prefix, "share", "man", "man1", "qatlas.1"): "qconnect.1",
	} {
		if err := os.Symlink(target, path); err != nil {
			t.Fatal(err)
		}
	}
	return prefix, executable
}

// rollback installs the older release tag with a validation that accepts it.
func rollback(t *testing.T, client *Client, tag string) (Result, error) {
	t.Helper()
	target, err := client.Target(context.Background(), tag)
	if err != nil || !target.Older {
		t.Fatalf("Target(%s) = %+v, %v", tag, target, err)
	}
	prepared, err := client.Prepare(context.Background(), target)
	if err != nil {
		return Result{}, err
	}
	if client.ValidateRollback == nil {
		client.ValidateRollback = func(context.Context, *Prepared) error { return nil }
	}
	return client.InstallPrepared(context.Background(), prepared)
}

// A rollback to v2.2.0, the last release before the rename, verifies it with the former signature and
// installs it as qatlas, in place of the alias, with its manual page in place of that alias, and takes
// qconnect and its manual pages away: the older program does not call itself qconnect. The release is
// published as it was, signed with the former signature only.
func TestRollbackToTheLastReleaseBeforeTheRenameInstallsTheFormerName(t *testing.T) {
	signer := releasetest.NewSigner(t)
	rel := programRelease(t, signer, "v2.2.0")
	if _, ok := rel.Assets["checksums.txt.qconnect.sig"]; ok {
		t.Fatal("the release before the rename carries the current signature")
	}
	server := selfupdatetest.NewServer(t, rel)
	prefix, current := renamedInstallation(t)
	writeFiles(t, prefix, map[string]string{"share/man/man1/qconnect-vault.1": "subpage"})
	manDir := filepath.Join(prefix, "share", "man", "man1")
	client := clientFor(server, signer, "v3.0.0", current)
	var files Release
	client.BeforeReplace = func(_ context.Context, rel Release) error {
		files = rel
		return nil
	}
	if result, err := rollback(t, client, "v2.2.0"); err != nil || !result.Updated || !result.Signed || result.Latest != "v2.2.0" {
		t.Fatalf("rollback = %+v, %v", result, err)
	}
	if !files.Rollback || filepath.Base(files.Signature) != "checksums.txt.sig" || !strings.HasPrefix(files.ArchiveName, "qatlas_v2.2.0_") {
		t.Errorf("the hooks saw %+v", files)
	}
	assertRegular(t, filepath.Join(prefix, "bin", "qatlas"), "new-binary")
	assertRegular(t, filepath.Join(manDir, "qatlas.1"), "new-manpage")
	for _, gone := range []string{current, filepath.Join(manDir, "qconnect.1"), filepath.Join(manDir, "qconnect-vault.1")} {
		assertMissing(t, gone)
	}
}

// Without an alias, the rollback to a release before the rename still installs it as qatlas.
func TestRollbackBeforeTheRenameWithoutAnAlias(t *testing.T) {
	signer := releasetest.NewSigner(t)
	server := selfupdatetest.NewServer(t, programRelease(t, signer, "v2.1.0"))
	prefix, current := installedPrefix(t)
	if _, err := rollback(t, clientFor(server, signer, "v3.0.0", current), "v2.1.0"); err != nil {
		t.Fatal(err)
	}
	assertRegular(t, filepath.Join(prefix, "bin", "qatlas"), "new-binary")
	assertMissing(t, current)
}

// A rollback the validation refuses, or that cannot replace the program, leaves the installation as it
// was, the aliases included.
func TestFailedRollbackKeepsTheInstallation(t *testing.T) {
	for name, tweak := range map[string]func(*Client){
		"validation refuses": func(c *Client) {
			c.ValidateRollback = func(context.Context, *Prepared) error { return errors.New("does not read") }
		},
		"before replace fails": func(c *Client) {
			c.BeforeReplace = func(context.Context, Release) error { return errors.New("cannot lock") }
		},
		"no validation": func(c *Client) { c.ValidateRollback = nil },
	} {
		t.Run(name, func(t *testing.T) {
			signer := releasetest.NewSigner(t)
			server := selfupdatetest.NewServer(t, programRelease(t, signer, "v2.2.0"))
			prefix, current := renamedInstallation(t)
			client := clientFor(server, signer, "v3.0.0", current)
			target, err := client.Target(context.Background(), "v2.2.0")
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := client.Prepare(context.Background(), target)
			if err != nil {
				t.Fatal(err)
			}
			client.ValidateRollback = func(context.Context, *Prepared) error { return nil }
			tweak(client)
			if result, err := client.InstallPrepared(context.Background(), prepared); err == nil || result.Updated {
				t.Fatalf("InstallPrepared() = %+v, %v, want a refusal", result, err)
			}
			assertRegular(t, current, "old-binary")
			assertAlias(t, filepath.Join(prefix, "bin", "qatlas"))
			assertRegular(t, filepath.Join(prefix, "share", "man", "man1", "qconnect.1"), "old-manpage")
			assertLink(t, filepath.Join(prefix, "share", "man", "man1", "qatlas.1"), "qconnect.1")
			if names := manNames(t, filepath.Join(prefix, "bin")); len(names) != 2 {
				t.Errorf("bin holds %v, want qconnect and qatlas only", names)
			}
		})
	}
}

// Releases after the rename update each other under the current names, the aliases kept, and the way back
// to one of them is a plain rollback.
func TestReleasesAfterTheRenameUpdateAndRollBackUnderTheCurrentName(t *testing.T) {
	signer := releasetest.NewSigner(t)
	server := selfupdatetest.NewServer(t, programRelease(t, signer, "v3.1.0"))
	prefix, current := renamedInstallation(t)
	if _, err := clientFor(server, signer, "v3.0.0", current).Update(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertRegular(t, current, "new-binary")
	assertAlias(t, filepath.Join(prefix, "bin", "qatlas"))
	server.Set(published(t, signer, "v3.0.0", programFiles(t, "v3.0.0", "older-binary", "older-manpage")))
	if _, err := rollback(t, clientFor(server, signer, "v3.1.0", current), "v3.0.0"); err != nil {
		t.Fatal(err)
	}
	assertRegular(t, current, "older-binary")
	assertAlias(t, filepath.Join(prefix, "bin", "qatlas"))
	assertLink(t, filepath.Join(prefix, "share", "man", "man1", "qatlas.1"), "qconnect.1")
}
