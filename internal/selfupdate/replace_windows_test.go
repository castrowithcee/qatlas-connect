//go:build windows

package selfupdate

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-kit/release/releasetest"
	"golang.org/x/crypto/ssh"

	"github.com/castrowithcee/qatlas-connect/internal/selfupdate/selfupdatetest"
)

// helperMode selects what a copy of this test program does instead of the tests: sleep runs like an
// installed qconnect during an update, cleanup starts like one and removes the leftovers of an update.
const helperMode = "QATLAS_SELFUPDATE_HELPER"

func TestMain(m *testing.M) {
	switch os.Getenv(helperMode) {
	case "sleep":
		fmt.Println("ready")
		time.Sleep(time.Minute)
		return
	case "cleanup":
		CleanupLeftovers()
		return
	}
	os.Exit(m.Run())
}

// self is this test program, followed by extra, which leaves it runnable.
func self(t *testing.T, extra string) []byte {
	t.Helper()
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return append(body, extra...)
}

// writeProgram writes body to path as a program, its directory included.
func writeProgram(t *testing.T, path string, body []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o755); err != nil {
		t.Fatal(err)
	}
}

// runCleanup starts the copy of this test program at path the way the next start of the program there
// does, which removes the leftovers of an update beside it.
func runCleanup(t *testing.T, path string) {
	t.Helper()
	cmd := exec.Command(path)
	cmd.Env = append(os.Environ(), helperMode+"=cleanup")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("start %s: %v: %s", path, err, out)
	}
}

// runningInstallation copies this test program to <prefix>\bin\qconnect.exe and starts it, so the file is a
// running executable the way an installed qconnect is during an update.
func runningInstallation(t *testing.T) (executable string, stop func()) {
	t.Helper()
	executable = filepath.Join(t.TempDir(), "bin", "qconnect.exe")
	writeProgram(t, executable, self(t, ""))
	cmd := exec.Command(executable)
	cmd.Env = append(os.Environ(), helperMode+"=sleep")
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	stop = func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }
	t.Cleanup(stop)
	// Wait until the copy really runs, so the tests do not depend on timing.
	ready := make(chan error, 1)
	go func() {
		line, err := bufio.NewReader(out).ReadString('\n')
		if err == nil && strings.TrimSpace(line) != "ready" {
			err = fmt.Errorf("unexpected output %q", line)
		}
		ready <- err
	}()
	select {
	case err := <-ready:
		if err != nil {
			t.Fatalf("the running copy did not report: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the running copy did not start")
	}
	return executable, stop
}

// windowsRelease is the release tag, a .zip with program as bin\<program>.exe and no manual page, signed
// by signer with the signature of its version.
func windowsRelease(t *testing.T, signer ssh.Signer, tag, program string) selfupdatetest.Release {
	t.Helper()
	return published(t, signer, tag, map[string]string{layoutOf(t, tag).ProgramFile("windows"): program})
}

// update updates the installation at executable from running to the newest release of server.
func update(t *testing.T, server *selfupdatetest.Server, signer ssh.Signer, running, executable string) Result {
	t.Helper()
	result, err := clientFor(server, signer, running, executable).Update(context.Background())
	if err != nil || !result.Updated || !result.Signed {
		t.Fatalf("Update() from %s = %+v, %v", running, result, err)
	}
	return result
}

// assertNoLeftovers checks that no leftover of an update, staged or moved aside, is in dir.
func assertNoLeftovers(t *testing.T, dir string) {
	t.Helper()
	for _, name := range dirNames(t, dir) {
		if strings.HasPrefix(name, ".") || strings.Contains(name, ".old") {
			t.Errorf("leftover %s in %s", name, dir)
		}
	}
}

func TestInstallReplacesRunningExecutable(t *testing.T) {
	executable, _ := runningInstallation(t)
	// A running file cannot be overwritten; this is the premise of the rename.
	if err := os.WriteFile(executable, []byte("x"), 0o755); err == nil {
		t.Fatal("overwriting a running executable succeeded")
	}
	signer := releasetest.NewSigner(t)
	server := selfupdatetest.NewServer(t, windowsRelease(t, signer, "v3.1.0", "new-binary"))
	update(t, server, signer, "v3.0.0", executable)
	assertFile(t, executable, "new-binary")
	if _, err := os.Stat(executable + ".old"); err != nil {
		t.Errorf("the running file is not kept as .old: %v", err)
	}
}

// When the new program cannot take the place of the running one, the running one stays where it was,
// unchanged, and is not left behind as .old.
func TestInstallKeepsTheRunningExecutableWhenTheReplacementFails(t *testing.T) {
	executable, _ := runningInstallation(t)
	before, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	signer := releasetest.NewSigner(t)
	server := selfupdatetest.NewServer(t, windowsRelease(t, signer, "v3.1.0", "new-binary"))
	client := clientFor(server, signer, "v3.0.0", executable)
	var held *os.File
	t.Cleanup(func() {
		if held != nil {
			_ = held.Close()
		}
	})
	// Holding the staged program open without delete sharing makes every move of it fail, after the
	// running program is moved aside.
	client.BeforeReplace = func(context.Context, Release) error {
		staged, err := filepath.Glob(filepath.Join(filepath.Dir(executable), ".*"))
		if err != nil || len(staged) != 1 {
			return fmt.Errorf("staged files %v: %v", staged, err)
		}
		held, err = os.Open(staged[0])
		return err
	}
	client.AfterReplace = func(context.Context, Release) { t.Error("AfterReplace ran although nothing was replaced") }
	result, err := client.Update(context.Background())
	if err == nil || result.Updated || !strings.Contains(err.Error(), "replace executable") {
		t.Fatalf("Update() = %+v, %v, want the replacement refused", result, err)
	}
	after, err := os.ReadFile(executable)
	if err != nil || string(after) != string(before) {
		t.Fatalf("the installation changed after a failed replacement: %v", err)
	}
	if _, err := os.Stat(executable + ".old"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the running file is left as .old: %v", err)
	}
}

func TestReplaceRunningKeepsTheRunningExecutableWhenTheMoveFails(t *testing.T) {
	executable, _ := runningInstallation(t)
	before, _ := os.ReadFile(executable)
	r := &opsRecorder{failRename: map[string]error{}}
	staged := filepath.Join(filepath.Dir(executable), "staged")
	if err := os.WriteFile(staged, []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}
	r.failRename["staged>qconnect.exe"] = os.ErrPermission
	if err := replaceRunning(r.ops(), staged, executable); err == nil {
		t.Fatal("want error")
	}
	after, _ := os.ReadFile(executable)
	if string(before) != string(after) {
		t.Error("installation changed after a failed move")
	}
}

// The running program's .old stays while it runs and goes at the first start of the new program after it
// ended.
func TestLeftoverOfRunningProgramIsRemovedAfterItEnds(t *testing.T) {
	executable, stop := runningInstallation(t)
	program := string(self(t, "\nnew-program"))
	signer := releasetest.NewSigner(t)
	server := selfupdatetest.NewServer(t, windowsRelease(t, signer, "v3.1.0", program))
	update(t, server, signer, "v3.0.0", executable)
	runCleanup(t, executable)
	if _, err := os.Stat(executable + ".old"); err != nil {
		t.Fatalf(".old of a running program is gone: %v", err)
	}
	stop()
	runCleanup(t, executable)
	if _, err := os.Stat(executable + ".old"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf(".old remains after the program ended: %v", err)
	}
	if body, err := os.ReadFile(executable); err != nil || string(body) != program {
		t.Errorf("%s is not the new program: %v", executable, err)
	}
}

func TestNextUpdateRemovesLeftover(t *testing.T) {
	executable, stop := runningInstallation(t)
	signer := releasetest.NewSigner(t)
	server := selfupdatetest.NewServer(t, windowsRelease(t, signer, "v3.1.0", "v2"))
	update(t, server, signer, "v3.0.0", executable)
	stop()
	server.Set(windowsRelease(t, signer, "v3.2.0", "v3"))
	update(t, server, signer, "v3.1.0", executable)
	assertFile(t, executable, "v3")
	// Neither the leftover of the first update nor the v2 file, which no longer runs, remains.
	if _, err := os.Stat(executable + ".old"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf(".old remains although no replaced program runs: %v", err)
	}
	if matches, _ := filepath.Glob(executable + ".old-*"); len(matches) != 0 {
		t.Errorf("unique leftovers: %v", matches)
	}
}

// A Windows installation is replaced without a manual page, and no manual page, share directory, staged
// file, or former name appears; an old leftover goes.
func TestInstallOnWindowsLayoutHasNoManpage(t *testing.T) {
	prefix := t.TempDir()
	executable := filepath.Join(prefix, "bin", "qconnect.exe")
	writeProgram(t, executable, []byte("old-binary"))
	writeProgram(t, executable+".old", []byte("stale"))
	signer := releasetest.NewSigner(t)
	server := selfupdatetest.NewServer(t, windowsRelease(t, signer, "v3.1.0", "new-binary"))
	client := clientFor(server, signer, "v3.0.0", executable)
	calls := 0
	client.BeforeReplace = func(context.Context, Release) error { calls++; return nil }
	if result, err := client.Update(context.Background()); err != nil || !result.Updated {
		t.Fatalf("Update() = %+v, %v", result, err)
	}
	assertRegular(t, executable, "new-binary")
	if calls != 1 {
		t.Errorf("BeforeReplace ran %d times", calls)
	}
	if _, err := os.Stat(filepath.Join(prefix, "share")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("share directory exists: %v", err)
	}
	if names := dirNames(t, filepath.Dir(executable)); strings.Join(names, ",") != "qconnect.exe" {
		t.Errorf("bin holds %v, want only qconnect.exe", names)
	}
}

// On Windows the former name is a copy of the program: an installation running as qatlas.exe gets
// qconnect.exe, and an update replaces both copies. A rollback to v2.2.0, the last release before the
// rename, puts that program in qatlas.exe and takes qconnect.exe away.
func TestInstallOnWindowsKeepsBothNames(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "bin")
	former, current := filepath.Join(bin, "qatlas.exe"), filepath.Join(bin, "qconnect.exe")
	writeProgram(t, former, []byte("v1"))
	signer := releasetest.NewSigner(t)
	server := selfupdatetest.NewServer(t, windowsRelease(t, signer, "v3.1.0", "v3"))
	update(t, server, signer, "v3.0.0", former)
	assertRegular(t, current, "v3")
	assertRegular(t, former, "v3")

	server.Set(windowsRelease(t, signer, "v3.2.0", "v4"))
	update(t, server, signer, "v3.1.0", current)
	assertRegular(t, current, "v4")
	assertRegular(t, former, "v4")

	server.Set(windowsRelease(t, signer, "v2.2.0", "v2.2"))
	client := clientFor(server, signer, "v3.2.0", current)
	client.ValidateRollback = func(context.Context, *Prepared) error { return nil }
	target, err := client.Target(context.Background(), "v2.2.0")
	if err != nil || !target.Older {
		t.Fatalf("Target() = %+v, %v", target, err)
	}
	prepared, err := client.Prepare(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := client.InstallPrepared(context.Background(), prepared); err != nil || !result.Updated || !result.Signed {
		t.Fatalf("InstallPrepared() = %+v, %v", result, err)
	}
	assertRegular(t, former, "v2.2")
	if _, err := os.Stat(current); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("qconnect.exe remains after the rollback: %v", err)
	}
	assertNoLeftovers(t, bin)
}

// The start of the program removes the leftovers of both names beside it, and nothing else; a program
// under another name removes none.
func TestRemoveLeftoversTouchesOnlyOwnOldFiles(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "bin")
	program := self(t, "")
	writeProgram(t, filepath.Join(bin, "qconnect.exe"), program)
	writeProgram(t, filepath.Join(bin, "tool.exe"), program)
	for _, name := range []string{"qconnect.exe.old", "qconnect.exe.old-1x", "qatlas.exe", "qatlas.exe.old", "qatlas.exe.old-2y",
		"other.exe.old", "notes.txt"} {
		writeProgram(t, filepath.Join(bin, name), nil)
	}
	all := "notes.txt,other.exe.old,qatlas.exe,qatlas.exe.old,qatlas.exe.old-2y,qconnect.exe,qconnect.exe.old,qconnect.exe.old-1x,tool.exe"
	runCleanup(t, filepath.Join(bin, "tool.exe"))
	if got := strings.Join(dirNames(t, bin), ","); got != all {
		t.Errorf("another program removed files: %s", got)
	}
	runCleanup(t, filepath.Join(bin, "qconnect.exe"))
	if got := strings.Join(dirNames(t, bin), ","); got != "notes.txt,other.exe.old,qatlas.exe,qconnect.exe,tool.exe" {
		t.Errorf("left = %s", got)
	}
}

// A leftover that could not be removed during the replacement, avoided by a unique name, goes at the next
// start, and the replaced copy stays.
func TestStuckLeftoverGoesAtTheNextStart(t *testing.T) {
	dir := t.TempDir()
	writeProgram(t, filepath.Join(dir, "qconnect.exe"), self(t, ""))
	staged, target := filepath.Join(dir, "staged"), filepath.Join(dir, "qatlas.exe")
	writeProgram(t, staged, []byte("new"))
	writeProgram(t, target, []byte("old"))
	writeProgram(t, target+".old", []byte("stale"))
	r := &opsRecorder{failRemove: map[string]error{"qatlas.exe.old": errors.New("in use")}}
	if err := replaceRunning(r.ops(), staged, target); err != nil {
		t.Fatal(err)
	}
	assertFile(t, target, "new")
	matches, _ := filepath.Glob(target + ".old-*")
	if len(matches) != 1 {
		t.Fatalf("unique old files = %v", matches)
	}
	assertFile(t, matches[0], "old")
	runCleanup(t, filepath.Join(dir, "qconnect.exe"))
	if matches, _ := filepath.Glob(target + ".old*"); len(matches) != 0 {
		t.Errorf("leftovers remain: %v", matches)
	}
	assertFile(t, target, "new")
}
