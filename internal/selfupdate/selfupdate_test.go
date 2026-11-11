package selfupdate

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/castrowithcee/qatlas-kit/release/releasetest"

	"github.com/castrowithcee/qatlas-connect/internal/selfupdate/selfupdatetest"
)

func TestCheckReportsNewerStableRelease(t *testing.T) {
	signer := releasetest.NewSigner(t)
	server := selfupdatetest.NewServer(t, programRelease(t, signer, "v3.2.0"), programRelease(t, signer, "v3.1.0"))
	result, err := clientFor(server, signer, "v3.1.0", "").Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Current != "v3.1.0" || result.Latest != "v3.2.0" || !result.UpdateAvailable || result.Updated {
		t.Fatalf("Check() = %+v", result)
	}
}

func TestCheckRefusesDevelopmentBuild(t *testing.T) {
	for _, version := range []string{"", "dev"} {
		if _, err := (&Client{Version: version}).Check(context.Background()); !errors.Is(err, ErrDevelopmentBuild) {
			t.Fatalf("Check() of %q = %v, want ErrDevelopmentBuild", version, err)
		}
	}
}

// authServer answers the release list only to a request whose Authorization header is want, refuses any
// other, and records the header of every request.
func authServer(t *testing.T, want string) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Header.Get("Authorization"))
		mu.Unlock()
		if r.Header.Get("Authorization") != want {
			http.Error(w, "unexpected authorization", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte("[]"))
	}))
	t.Cleanup(server.Close)
	return server, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), seen...)
	}
}

func TestCheckUsesExplicitToken(t *testing.T) {
	server, seen := authServer(t, "Bearer rel-token")
	client := &Client{BaseURL: server.URL + selfupdatetest.RepositoryPath, HTTPClient: server.Client(), Version: "v3.0.0", Token: "rel-token"}
	if _, err := client.Check(context.Background()); err != nil {
		t.Fatalf("Check() = %v, want every request with the explicit token", err)
	}
	if len(seen()) == 0 {
		t.Fatal("no request reached the server")
	}
}

func TestTokenUsesGHEnvironment(t *testing.T) {
	t.Setenv("GH_TOKEN", "environment-token")
	if got := (&Client{}).token(); got != "environment-token" {
		t.Fatalf("token() = %q, want environment token", got)
	}
}

// A repository URL other than the production one never receives the token of the environment: the server
// refuses any request that carries one.
func TestCustomBaseURLDoesNotReceiveEnvironmentToken(t *testing.T) {
	t.Setenv("GH_TOKEN", "environment-token")
	t.Setenv("GITHUB_TOKEN", "environment-token")
	server, seen := authServer(t, "")
	client := &Client{BaseURL: server.URL + selfupdatetest.RepositoryPath, HTTPClient: server.Client(), Version: "v3.0.0"}
	if _, err := client.Check(context.Background()); err != nil {
		t.Fatalf("Check() = %v, want no request with a token", err)
	}
	if len(seen()) == 0 {
		t.Fatal("no request reached the server")
	}
}

func TestUpdateReplacesBinaryAndManpage(t *testing.T) {
	signer := releasetest.NewSigner(t)
	server := selfupdatetest.NewServer(t, programRelease(t, signer, "v3.1.0"))
	prefix, executable := installedPrefix(t)
	client := clientFor(server, signer, "v3.0.0", executable)
	// The hook runs once, with the old program still in place.
	calls := 0
	client.BeforeReplace = func(context.Context, Release) error {
		calls++
		assertFile(t, executable, "old-binary")
		return nil
	}
	result, err := client.Update(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("BeforeReplace ran %d times, want once", calls)
	}
	if !result.UpdateAvailable || !result.Updated || !result.Signed || result.Latest != "v3.1.0" || result.Current != "v3.0.0" {
		t.Fatalf("Update() = %+v, want a signed update", result)
	}
	assertRegular(t, executable, "new-binary")
	manpage := manpageOf(prefix, currentName)
	if runtime.GOOS == "windows" {
		// A Windows release has no manual page, and the installation leaves the one there alone.
		assertFile(t, manpage, "old-manpage")
		return
	}
	assertFile(t, manpage, "new-manpage")
	if info, err := os.Stat(executable); err != nil || info.Mode().Perm()&0o500 != 0o500 {
		t.Fatalf("the program is not executable: %v, %v", info, err)
	}
}

// A release whose archive does not match its signed checksum installs nothing.
func TestUpdateLeavesInstallationUntouchedOnChecksumMismatch(t *testing.T) {
	signer := releasetest.NewSigner(t)
	rel := programRelease(t, signer, "v3.1.0")
	name, tampered := archiveWith(t, "v3.1.0", programFiles(t, "v3.1.0", "tampered", "tampered"))
	rel.Assets[name] = tampered
	server := selfupdatetest.NewServer(t, rel)
	prefix, executable := installedPrefix(t)
	client := clientFor(server, signer, "v3.0.0", executable)
	forbidReplace(t, client)
	result, err := client.Update(context.Background())
	if err == nil || result.Updated || !strings.Contains(err.Error(), "nothing was changed") ||
		!strings.Contains(err.Error(), "archive checksum does not match") {
		t.Fatalf("Update() = %+v, %v, want the checksum refused", result, err)
	}
	assertUntouched(t, prefix, executable)
}

func TestUpdateDoesNotDowngrade(t *testing.T) {
	signer := releasetest.NewSigner(t)
	server := selfupdatetest.NewServer(t, programRelease(t, signer, "v3.0.0"))
	prefix, executable := installedPrefix(t)
	client := clientFor(server, signer, "v3.1.0", executable)
	forbidReplace(t, client)
	result, err := client.Update(context.Background())
	if err != nil || result.UpdateAvailable || result.Updated {
		t.Fatalf("Update() = %+v, %v, want no downgrade", result, err)
	}
	assertUntouched(t, prefix, executable)
}

// The newest release without its signature, and an unsigned or draft prerelease, are passed over for the
// newest signed stable one; with no other release, nothing is installed.
func TestCheckSkipsNewerUnsignedRelease(t *testing.T) {
	signer := releasetest.NewSigner(t)
	server := selfupdatetest.NewServer(t, unsigned(t, programRelease(t, signer, "v3.3.0")), programRelease(t, signer, "v3.2.0"),
		programRelease(t, signer, "v3.4.0-rc.1"), draft(programRelease(t, signer, "v3.5.0")))
	result, err := clientFor(server, signer, "v3.1.0", "").Check(context.Background())
	if err != nil || result.Latest != "v3.2.0" || !result.UpdateAvailable {
		t.Fatalf("Check() = %+v, %v, want v3.2.0", result, err)
	}

	server.Set(unsigned(t, programRelease(t, signer, "v3.3.0")), draft(programRelease(t, signer, "v3.5.0")))
	prefix, executable := installedPrefix(t)
	client := clientFor(server, signer, "v3.1.0", executable)
	forbidReplace(t, client)
	if result, err := client.Update(context.Background()); err != nil || result.Updated || result.UpdateAvailable {
		t.Fatalf("Update() = %+v, %v, want nothing installed", result, err)
	}
	assertUntouched(t, prefix, executable)
}

// The hooks see the verified release in a private directory: before, with the installed files untouched,
// and after, with the new program in place and, where it is a link, its former name an alias of it. The
// directory is gone once Update returns.
func TestUpdateHooksSeeTheVerifiedRelease(t *testing.T) {
	signer := releasetest.NewSigner(t)
	rel := programRelease(t, signer, "v3.1.0")
	server := selfupdatetest.NewServer(t, rel)
	prefix, executable := installedPrefix(t)
	alias := filepath.Join(prefix, "bin", "qatlas")
	if runtime.GOOS != "windows" {
		if err := os.Symlink("qconnect", alias); err != nil {
			t.Fatal(err)
		}
	}
	manpage := manpageOf(prefix, currentName)
	var order []string
	var seen Release
	check := func(name string, files Release) {
		order = append(order, name)
		seen = files
		if files.ArchiveName != archiveName(t, "v3.1.0") || files.Rollback || filepath.Base(files.Signature) != "checksums.txt.qconnect.sig" {
			t.Errorf("%s: release = %+v", name, files)
		}
		for path, asset := range map[string]string{files.Checksums: "checksums.txt", files.Signature: "checksums.txt.qconnect.sig", files.Archive: files.ArchiveName} {
			info, err := os.Stat(path)
			if err != nil || !filepath.IsAbs(path) || filepath.Dir(path) != filepath.Dir(files.Checksums) {
				t.Errorf("%s: %s: %v", name, path, err)
				continue
			}
			if body, err := os.ReadFile(path); err != nil || string(body) != string(rel.Assets[asset]) {
				t.Errorf("%s: %s is not the verified %s: %v", name, path, asset, err)
			}
			if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
				t.Errorf("%s: %s is readable by others: %v", name, path, info.Mode())
			}
		}
		if runtime.GOOS != "windows" {
			if info, err := os.Stat(filepath.Dir(files.Checksums)); err != nil || info.Mode().Perm() != 0o700 {
				t.Errorf("%s: directory = %v, %v, want 0700", name, info, err)
			}
		}
	}
	client := clientFor(server, signer, "v3.0.0", executable)
	client.BeforeReplace = func(_ context.Context, files Release) error {
		check("before", files)
		assertFile(t, executable, "old-binary")
		assertFile(t, manpage, "old-manpage")
		return nil
	}
	client.AfterReplace = func(_ context.Context, files Release) {
		check("after", files)
		assertFile(t, executable, "new-binary")
		if runtime.GOOS != "windows" {
			if target, err := os.Readlink(alias); err != nil || target != currentName {
				t.Errorf("%s links to %q (%v), want qconnect", alias, target, err)
			}
		}
	}
	if _, err := client.Update(context.Background()); err != nil {
		t.Fatal(err)
	}
	if strings.Join(order, ",") != "before,after" {
		t.Fatalf("hooks ran %v, want before then after", order)
	}
	if _, err := os.Stat(filepath.Dir(seen.Checksums)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the release directory is left behind: %v", err)
	}
}

// An error of BeforeReplace ends the update with nothing installed and no AfterReplace. The hook saw the
// signed release, and its directory is gone.
func TestUpdateBeforeReplaceErrorInstallsNothing(t *testing.T) {
	signer := releasetest.NewSigner(t)
	rel := programRelease(t, signer, "v3.1.0")
	server := selfupdatetest.NewServer(t, rel)
	prefix, executable := installedPrefix(t)
	client := clientFor(server, signer, "v3.0.0", executable)
	var dir string
	client.BeforeReplace = func(_ context.Context, files Release) error {
		dir = filepath.Dir(files.Checksums)
		if body, err := os.ReadFile(files.Signature); err != nil || len(body) == 0 || string(body) != string(rel.Assets["checksums.txt.qconnect.sig"]) {
			t.Errorf("a signed release is reported as %+v (%v)", files, err)
		}
		return errors.New("cannot prepare")
	}
	client.AfterReplace = func(context.Context, Release) { t.Error("AfterReplace ran although nothing was replaced") }
	if result, err := client.Update(context.Background()); err == nil || result.Updated || !strings.Contains(err.Error(), "cannot prepare") {
		t.Fatalf("Update() = %+v, %v, want the hook's error", result, err)
	}
	assertUntouched(t, prefix, executable)
	if dir == "" {
		t.Fatal("BeforeReplace did not run")
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the release directory is left behind: %v", err)
	}
}

// Update reports the progress of the archive download only, up to its whole size.
func TestUpdateReportsArchiveProgress(t *testing.T) {
	signer := releasetest.NewSigner(t)
	rel := programRelease(t, signer, "v3.1.0")
	server := selfupdatetest.NewServer(t, rel)
	_, executable := installedPrefix(t)
	client := clientFor(server, signer, "v3.0.0", executable)
	var last int64
	client.Progress = func(done, total int64) { last = done }
	if _, err := client.Update(context.Background()); err != nil {
		t.Fatal(err)
	}
	if want := int64(len(rel.Assets[archiveName(t, "v3.1.0")])); last != want {
		t.Fatalf("last progress = %d, want the archive's %d bytes", last, want)
	}
}
