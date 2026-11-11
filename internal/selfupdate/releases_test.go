package selfupdate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/castrowithcee/qatlas-kit/release/releasetest"
	"golang.org/x/crypto/ssh"

	"github.com/castrowithcee/qatlas-connect/internal/selfupdate/selfupdatetest"
)

func tags(entries []Entry) string {
	var out []string
	for _, e := range entries {
		marks := ""
		if e.Prerelease {
			marks += "p"
		}
		if e.Current {
			marks += "c"
		}
		if e.Older {
			marks += "o"
		}
		out = append(out, e.Version+marks)
	}
	return strings.Join(out, " ")
}

// The list shows the ten newest signed stable releases from MinVersion on, newer prereleases, and the
// running version, across the rename: a release before it counts with its former signature. Unsigned
// releases and prereleases, drafts, older prereleases, and releases below MinVersion are left out.
func TestListSelectionRule(t *testing.T) {
	signer := releasetest.NewSigner(t)
	var releases []selfupdatetest.Release
	for i := 0; i <= 11; i++ {
		releases = append(releases, programRelease(t, signer, fmt.Sprintf("v3.%d.0", i)))
	}
	releases = append(releases, programRelease(t, signer, "v3.12.0-rc.1"), programRelease(t, signer, "v3.5.0-rc.1"),
		programRelease(t, signer, "v2.2.0"), programRelease(t, signer, "v0.29.0"),
		unsigned(t, programRelease(t, signer, "v3.13.0")),
		draft(programRelease(t, signer, "v3.13.0-rc.1")),
		unsigned(t, programRelease(t, signer, "v3.14.0-beta.1")))
	server := selfupdatetest.NewServer(t, releases...)
	entries, err := clientFor(server, signer, "v3.2.0", "").List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := "v3.12.0-rc.1p v3.11.0 v3.10.0 v3.9.0 v3.8.0 v3.7.0 v3.6.0 v3.5.0 v3.4.0 v3.3.0 v3.2.0c"
	if got := tags(entries); got != want {
		t.Fatalf("List() = %s, want %s", got, want)
	}
	entries, err = clientFor(server, signer, "v3.3.0", "").List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want = "v3.12.0-rc.1p v3.11.0 v3.10.0 v3.9.0 v3.8.0 v3.7.0 v3.6.0 v3.5.0 v3.4.0 v3.3.0c v3.2.0o"
	if got := tags(entries); got != want {
		t.Fatalf("List() from v3.3.0 = %s, want %s", got, want)
	}
	server.Set(programRelease(t, signer, "v3.0.0"), programRelease(t, signer, "v2.2.0"))
	entries, err = clientFor(server, signer, "v3.0.0", "").List(context.Background())
	if err != nil || tags(entries) != "v3.0.0c v2.2.0o" {
		t.Fatalf("List() across the rename = %s, %v", tags(entries), err)
	}
}

// The running version is listed even outside the ten newest, marked as current; a running version that is
// no signed release from MinVersion on is not, even when such a release exists.
func TestListMarksRunningVersionOutsideTheTen(t *testing.T) {
	signer := releasetest.NewSigner(t)
	var releases []selfupdatetest.Release
	for minor := 43; minor >= 30; minor-- {
		releases = append(releases, programRelease(t, signer, fmt.Sprintf("v0.%d.0", minor)))
	}
	releases = append(releases, programRelease(t, signer, "v0.29.5"))
	server := selfupdatetest.NewServer(t, releases...)
	entries, err := clientFor(server, signer, "v0.31.0", "").List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 11 || !entries[10].Current || entries[10].Version != "v0.31.0" || entries[10].Older || entries[9].Older ||
		entries[9].Version != "v0.34.0" {
		t.Fatalf("List() = %s", tags(entries))
	}
	entries, err = clientFor(server, signer, "v0.29.5", "").List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 10 {
		t.Fatalf("List() = %s, want the ten newest only", tags(entries))
	}
	for _, e := range entries {
		if e.Current || e.Older || e.Version == "v0.29.5" {
			t.Fatalf("List() = %s", tags(entries))
		}
	}
}

// The list reads every page of the release list: with three full pages of a hundred, the newest releases
// on the last one, a fourth, empty page ends the read.
func TestListReadsEveryPage(t *testing.T) {
	var releases []selfupdatetest.Release
	for minor := range 300 {
		releases = append(releases, listed(t, fmt.Sprintf("v3.%d.0", minor)))
	}
	server := selfupdatetest.NewServer(t, releases...)
	entries, err := clientFor(server, releasetest.NewSigner(t), "v3.0.0", "").List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 11 || entries[0].Version != "v3.299.0" || entries[9].Version != "v3.290.0" || !entries[10].Current ||
		entries[10].Version != "v3.0.0" {
		t.Fatalf("List() = %s", tags(entries))
	}
	if pages := server.Requests(selfupdatetest.RepositoryPath + "/releases"); pages != 4 {
		t.Fatalf("read %d pages, want 4", pages)
	}
}

// A server that never ends its release list is read up to the page limit of twenty pages, and the list
// fails instead of being cut short. So do Check and Update, with nothing installed.
func TestListStopsAtThePageLimit(t *testing.T) {
	signer := releasetest.NewSigner(t)
	var pages atomic.Int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != selfupdatetest.RepositoryPath+"/releases" {
			http.NotFound(w, r)
			return
		}
		pages.Add(1)
		page := make([]map[string]any, 100)
		for i := range page {
			page[i] = map[string]any{"tag_name": "v3.1.0", "assets": []map[string]string{
				{"name": archiveName(t, "v3.1.0"), "url": server.URL + selfupdatetest.RepositoryPath + "/releases/assets/1"},
				{"name": "checksums.txt", "url": server.URL + selfupdatetest.RepositoryPath + "/releases/assets/2"},
				{"name": "checksums.txt.qconnect.sig", "url": server.URL + selfupdatetest.RepositoryPath + "/releases/assets/3"},
			}}
		}
		_ = json.NewEncoder(w).Encode(page)
	}))
	t.Cleanup(server.Close)
	prefix, executable := installedPrefix(t)
	client := &Client{BaseURL: server.URL + selfupdatetest.RepositoryPath, HTTPClient: server.Client(), Version: "v3.0.0",
		GOARCH: testArch, Executable: executable, SigningKeys: []ssh.PublicKey{signer.PublicKey()}}
	forbidReplace(t, client)
	if entries, err := client.List(context.Background()); err == nil || !strings.Contains(err.Error(), "page limit") {
		t.Fatalf("List() = %s, %v, want the page limit reported", tags(entries), err)
	}
	if got := pages.Load(); got != 20 {
		t.Fatalf("read %d pages, want 20", got)
	}
	if result, err := client.Check(context.Background()); err == nil {
		t.Fatalf("Check() = %+v, want the page limit reported", result)
	}
	if result, err := client.Update(context.Background()); err == nil || result.Updated {
		t.Fatalf("Update() = %+v, %v, want the page limit reported", result, err)
	}
	assertUntouched(t, prefix, executable)
}

func TestTargetRefusals(t *testing.T) {
	signer := releasetest.NewSigner(t)
	server := selfupdatetest.NewServer(t, programRelease(t, signer, "v3.2.0"), unsigned(t, programRelease(t, signer, "v3.4.0")),
		programRelease(t, signer, "v0.29.0"), draft(programRelease(t, signer, "v3.5.0")))
	for tag, want := range map[string]string{
		"3.1.0":   "is not a release version",
		"latest":  "is not a release version",
		"v0.29.0": "older than v0.30.0",
		"v3.9.0":  "does not exist",
		"v3.4.0":  "is not signed or is incomplete and cannot be chosen",
		"v3.5.0":  "is not signed or is incomplete and cannot be chosen",
	} {
		t.Run(tag, func(t *testing.T) {
			prefix, executable := installedPrefix(t)
			client := clientFor(server, signer, "v3.0.0", executable)
			forbidReplace(t, client)
			_, err := client.Target(context.Background(), tag)
			var target *TargetError
			if !errors.As(err, &target) || !strings.Contains(err.Error(), want) {
				t.Errorf("Target(%s) = %v, want a TargetError with %q", tag, err, want)
			}
			assertUntouched(t, prefix, executable)
		})
	}
}

func TestTargetMarksAnOlderVersionAndUpdateToRefusesIt(t *testing.T) {
	signer := releasetest.NewSigner(t)
	server := selfupdatetest.NewServer(t, programRelease(t, signer, "v3.0.0"), programRelease(t, signer, "v3.2.0"))
	prefix, executable := installedPrefix(t)
	client := clientFor(server, signer, "v3.1.0", executable)
	forbidReplace(t, client)
	client.ValidateRollback = func(context.Context, *Prepared) error { return nil }
	older, err := client.Target(context.Background(), "v3.0.0")
	if err != nil || !older.Older || older.Current {
		t.Fatalf("Target(v3.0.0) = %+v, %v", older, err)
	}
	newer, err := client.Target(context.Background(), "v3.2.0")
	if err != nil || newer.Older || newer.Current {
		t.Fatalf("Target(v3.2.0) = %+v, %v", newer, err)
	}
	var refused *TargetError
	if result, err := client.UpdateTo(context.Background(), older); !errors.As(err, &refused) || result.Updated {
		t.Fatalf("UpdateTo(older) = %+v, %v, want a TargetError", result, err)
	}
	assertUntouched(t, prefix, executable)
}

func TestTargetOfTheRunningVersionChangesNothing(t *testing.T) {
	signer := releasetest.NewSigner(t)
	server := selfupdatetest.NewServer(t, programRelease(t, signer, "v3.0.0"))
	prefix, executable := installedPrefix(t)
	client := clientFor(server, signer, "v3.0.0", executable)
	forbidReplace(t, client)
	target, err := client.Target(context.Background(), "v3.0.0")
	if err != nil || !target.Current || target.Older {
		t.Fatalf("Target() = %+v, %v", target, err)
	}
	if result, err := client.UpdateTo(context.Background(), target); err != nil || result.Updated || result.UpdateAvailable {
		t.Fatalf("UpdateTo() = %+v, %v", result, err)
	}
	var refused *TargetError
	if _, err := client.Prepare(context.Background(), target); !errors.As(err, &refused) {
		t.Fatalf("Prepare() = %v, want the running version refused", err)
	}
	assertUntouched(t, prefix, executable)
}

// A prerelease that is not the newest release, and not in the list's reach either, installs by its tag.
func TestUpdateToInstallsAPrerelease(t *testing.T) {
	signer := releasetest.NewSigner(t)
	server := selfupdatetest.NewServer(t, programRelease(t, signer, "v3.2.0"), programRelease(t, signer, "v3.1.0-rc.1"))
	_, executable := installedPrefix(t)
	client := clientFor(server, signer, "v3.0.0", executable)
	target, err := client.Target(context.Background(), "v3.1.0-rc.1")
	if err != nil || !target.Prerelease || target.Current || target.Older {
		t.Fatalf("Target() = %+v, %v", target, err)
	}
	result, err := client.UpdateTo(context.Background(), target)
	if err != nil || !result.Updated || !result.Signed || !result.UpdateAvailable || result.Latest != "v3.1.0-rc.1" || result.Current != "v3.0.0" {
		t.Fatalf("UpdateTo() = %+v, %v", result, err)
	}
	assertRegular(t, executable, "new-binary")
}

func TestUpdateToAcceptsAReleaseOutsideTheList(t *testing.T) {
	signer := releasetest.NewSigner(t)
	var releases []selfupdatetest.Release
	for minor := 13; minor >= 0; minor-- {
		releases = append(releases, programRelease(t, signer, fmt.Sprintf("v3.%d.0", minor)))
	}
	server := selfupdatetest.NewServer(t, releases...)
	_, executable := installedPrefix(t)
	client := clientFor(server, signer, "v3.0.0", executable)
	entries, err := client.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Version == "v3.2.0" {
			t.Fatalf("List() = %s, want v3.2.0 outside it", tags(entries))
		}
	}
	target, err := client.Target(context.Background(), "v3.2.0")
	if err != nil {
		t.Fatal(err)
	}
	if result, err := client.UpdateTo(context.Background(), target); err != nil || !result.Updated || result.Latest != "v3.2.0" {
		t.Fatalf("UpdateTo() = %+v, %v", result, err)
	}
	assertRegular(t, executable, "new-binary")
}

// Prepare verifies without installing and runs no hook, refuses a release whose signature does not verify
// and the running version; InstallPrepared installs what it verified, here an older release after its
// check, with each hook once.
func TestPrepareVerifiesWithoutInstallingAndInstallPreparedInstalls(t *testing.T) {
	signer := releasetest.NewSigner(t)
	bad := programRelease(t, signer, "v3.2.0")
	bad.Assets["checksums.txt.qconnect.sig"] = releasetest.Sign(t, signer, selfupdatetest.Current.Namespace, "sha512",
		append([]byte("x"), bad.Assets["checksums.txt"]...))
	server := selfupdatetest.NewServer(t, programRelease(t, signer, "v3.0.0"), bad, programRelease(t, signer, "v3.1.0"))
	prefix, executable := installedPrefix(t)
	client := clientFor(server, signer, "v3.1.0", executable)
	var before, after, validated int
	client.BeforeReplace = func(context.Context, Release) error { before++; return nil }
	client.AfterReplace = func(context.Context, Release) { after++ }
	client.ValidateRollback = func(_ context.Context, p *Prepared) error {
		validated++
		if p.Version() != "v3.0.0" {
			t.Errorf("validated %s", p.Version())
		}
		return nil
	}

	badTarget, err := client.Target(context.Background(), "v3.2.0")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Prepare(context.Background(), badTarget); err == nil || !strings.Contains(err.Error(), "does not match checksums") {
		t.Fatalf("Prepare() with a bad signature = %v", err)
	}
	current, err := client.Target(context.Background(), "v3.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Prepare(context.Background(), current); err == nil {
		t.Fatal("Prepare() of the running version succeeded")
	}

	older, err := client.Target(context.Background(), "v3.0.0")
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := client.Prepare(context.Background(), older)
	if err != nil || prepared.Version() != "v3.0.0" {
		t.Fatalf("Prepare() = %+v, %v", prepared, err)
	}
	assertUntouched(t, prefix, executable)
	if before != 0 || after != 0 || validated != 0 {
		t.Fatalf("hooks ran during Prepare: %d, %d, %d", before, after, validated)
	}
	result, err := client.InstallPrepared(context.Background(), prepared)
	if err != nil || !result.Updated || !result.Signed || result.Latest != "v3.0.0" || result.Current != "v3.1.0" {
		t.Fatalf("InstallPrepared() = %+v, %v", result, err)
	}
	assertRegular(t, executable, "new-binary")
	if before != 1 || after != 1 || validated != 1 {
		t.Fatalf("hooks = %d, %d, %d, want each once", before, after, validated)
	}
}

// The question before a rollback to a release before the rename says what that version works on.
func TestTargetQuestionNamesTheStateBeforeTheMigration(t *testing.T) {
	question := TargetQuestion("v2.2.0", false, true)
	for _, want := range []string{"~/.qatlas/cli/", "qatlas-cli", "installed as qatlas", "qconnect is removed"} {
		if !strings.Contains(question, want) {
			t.Errorf("question lacks %q: %s", want, question)
		}
	}
	if question := TargetQuestion("v2.2.1", false, true); strings.Contains(question, "~/.qatlas/cli/") {
		t.Errorf("a rollback after the rename names the former state: %s", question)
	}
}
