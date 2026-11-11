package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-kit/release/releasetest"
	"golang.org/x/crypto/ssh"

	"github.com/castrowithcee/qatlas-connect/internal/selfupdate"
	"github.com/castrowithcee/qatlas-connect/internal/selfupdate/selfupdatetest"
)

// chosenRelease is one release of the local release server.
type chosenRelease struct {
	tag        string
	prerelease bool
	unsigned   bool
}

// chosenProgram is the program the releases of the local server carry.
var chosenProgram = "new-program"

// chosenUpdater serves the given releases signed by a fresh signer, each with the names and signature of
// its version, and returns an updater for a program that runs v3.0.0.
func chosenUpdater(t *testing.T, executable string, releases []chosenRelease, replaced *int) *selfupdate.Client {
	t.Helper()
	signer := releasetest.NewSigner(t)
	var published []selfupdatetest.Release
	for _, r := range releases {
		name, archive := selfupdatetest.Archive(t, r.tag, "linux", "amd64", []byte(chosenProgram))
		var sets []selfupdatetest.Signature
		if !r.unsigned {
			sets = append(sets, selfupdatetest.SignatureOf(t, r.tag))
		}
		rel := selfupdatetest.Signed(t, signer, r.tag, map[string][]byte{name: archive}, sets...)
		rel.Prerelease = r.prerelease
		published = append(published, rel)
	}
	server := selfupdatetest.NewServer(t, published...)
	return &selfupdate.Client{
		BaseURL: server.RepositoryAPI(), HTTPClient: server.Client(), Version: "v3.0.0", GOARCH: "amd64",
		Executable: executable, SigningKeys: []ssh.PublicKey{signer.PublicKey()},
		BeforeReplace: func(context.Context, selfupdate.Release) error {
			*replaced++
			return nil
		},
	}
}

var chosenReleases = []chosenRelease{
	{tag: "v3.2.0"}, {tag: "v3.3.0-rc.1", prerelease: true}, {tag: "v3.0.0"}, {tag: "v2.3.0"},
	{tag: "v3.4.0", unsigned: true}, {tag: "v0.29.0"},
}

// chosenRun runs 'qconnect update' with args against chosenReleases.
func chosenRun(t *testing.T, args ...string) (code int, stdout, stderr, program string, replaced int) {
	t.Helper()
	executable := installedProgram(t)
	opts := &Options{Updater: chosenUpdater(t, executable, chosenReleases, &replaced)}
	code, stdout, stderr = runWithInput(t, opts, "", append([]string{"update", "--config", configIn(t.TempDir())}, args...)...)
	data, _ := os.ReadFile(executable)
	return code, stdout, stderr, string(data), replaced
}

func TestUpdateListShowsTheChoosableReleases(t *testing.T) {
	withInteractive(t, false)
	code, stdout, stderr, program, replaced := chosenRun(t, "--list", "--output", "json")
	if code != exitOK || program != "old-program" || replaced != 0 {
		t.Fatalf("exit code = %d, stderr = %q, program = %q, replaced %d", code, stderr, program, replaced)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(stdout), &rows); err != nil {
		t.Fatalf("%v: %q", err, stdout)
	}
	var got []string
	for _, row := range rows {
		got = append(got, fmt.Sprintf("%v/%v/%v/%v", row["version"], row["prerelease"], row["current"], row["older"]))
	}
	want := "v3.3.0-rc.1/true/false/false v3.2.0/false/false/false v3.0.0/false/true/false v2.3.0/false/false/true"
	if strings.Join(got, " ") != want {
		t.Fatalf("list = %v, want %s", got, want)
	}
}

func TestUpdateToInstallsAPrereleaseAfterTheQuestion(t *testing.T) {
	var asked []string
	withVaultConfirm(t, func(prompt string) (bool, error) {
		asked = append(asked, prompt)
		return true, nil
	})
	code, stdout, stderr, program, replaced := chosenRun(t, "--to", "v3.3.0-rc.1", "--confirm", "--output", "json")
	if code != exitOK || program != "new-program" || replaced != 1 || len(asked) != 1 {
		t.Fatalf("exit code = %d, stderr = %q, program = %q, replaced %d, questions %d", code, stderr, program, replaced, len(asked))
	}
	for _, want := range []string{"prerelease v3.3.0-rc.1", "newer", "vault process is handed over"} {
		if !strings.Contains(asked[0], want) {
			t.Errorf("the question does not say %q: %q", want, asked[0])
		}
	}
	for _, want := range []string{`"signed":true`, `"updated":true`, `"latest":"v3.3.0-rc.1"`} {
		if !strings.Contains(strings.ReplaceAll(stdout, " ", ""), want) {
			t.Errorf("stdout = %q does not hold %s", stdout, want)
		}
	}
}

func TestUpdateToAnsweringNoChangesNothing(t *testing.T) {
	withVaultConfirm(t, func(string) (bool, error) { return false, nil })
	code, _, stderr, program, replaced := chosenRun(t, "--to", "v3.2.0", "--confirm")
	if code != exitOK || program != "old-program" || replaced != 0 || !strings.Contains(stderr, "cancelled") {
		t.Fatalf("exit code = %d, stderr = %q, program = %q, replaced %d", code, stderr, program, replaced)
	}
}

func TestUpdateToWithoutTerminalNeedsYes(t *testing.T) {
	withVaultConfirm(t, func(string) (bool, error) {
		t.Error("a refused update asked")
		return true, nil
	})
	for name, args := range map[string][]string{"pipe": nil, "agent": {"--agent"}} {
		withInteractive(t, name == "agent")
		code, _, stderr, program, replaced := chosenRun(t, append([]string{"--to", "v3.2.0"}, args...)...)
		if code != exitUsage || !strings.Contains(stderr, "--yes") || program != "old-program" || replaced != 0 {
			t.Errorf("%s: exit code = %d, stderr = %q, program = %q, replaced %d", name, code, stderr, program, replaced)
		}
	}
	withInteractive(t, false)
	code, _, stderr, program, replaced := chosenRun(t, "--to", "v3.2.0", "--yes", "--confirm")
	if code != exitOK || program != "new-program" || replaced != 1 {
		t.Errorf("--yes: exit code = %d, stderr = %q, program = %q, replaced %d", code, stderr, program, replaced)
	}
}

func TestUpdateToRefusesWithoutChanges(t *testing.T) {
	withVaultConfirm(t, func(string) (bool, error) {
		t.Error("a refused update asked")
		return true, nil
	})
	for name, tt := range map[string]struct{ tag, want string }{
		"unsigned":      {"v3.4.0", "not signed"},
		"below minimum": {"v0.29.0", "oldest version"},
	} {
		code, _, stderr, program, replaced := chosenRun(t, "--to", tt.tag, "--yes", "--confirm")
		if code != exitUsage || !strings.Contains(stderr, tt.want) || program != "old-program" || replaced != 0 {
			t.Errorf("%s: exit code = %d, stderr = %q, program = %q, replaced %d", name, code, stderr, program, replaced)
		}
	}
}

func TestUpdateToTheRunningVersionOnlyReportsIt(t *testing.T) {
	withVaultConfirm(t, func(string) (bool, error) {
		t.Error("the running version asked")
		return true, nil
	})
	withInteractive(t, false)
	code, _, stderr, program, replaced := chosenRun(t, "--to", "v3.0.0")
	if code != exitOK || !strings.Contains(stderr, "already installed") || program != "old-program" || replaced != 0 {
		t.Fatalf("exit code = %d, stderr = %q, program = %q, replaced %d", code, stderr, program, replaced)
	}
}

func TestUpdateListAndToExcludeEachOther(t *testing.T) {
	for _, args := range [][]string{
		{"--list", "--to", "v3.2.0"}, {"--list", "--check"}, {"--to", "v3.2.0", "--check"}, {"--list", "--yes"}, {"--to", ""},
	} {
		code, _, stderr, program, replaced := chosenRun(t, args...)
		if code != exitUsage || program != "old-program" || replaced != 0 {
			t.Errorf("%v: exit code = %d, stderr = %q", args, code, stderr)
		}
	}
}
