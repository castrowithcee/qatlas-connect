//go:build unix

package selfupdate

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-kit/release/releasetest"

	"github.com/castrowithcee/qatlas-connect/internal/selfupdate/selfupdatetest"
)

// scriptRelease prepares a release of version whose program is script.
func scriptRelease(t *testing.T, version, script string, timeout time.Duration) *Prepared {
	t.Helper()
	signer := releasetest.NewSigner(t)
	server := selfupdatetest.NewServer(t, published(t, signer, version, programFiles(t, version, script, "page")))
	client := clientFor(server, signer, "v9.0.0", "")
	client.CheckTimeout = timeout
	target, err := client.Target(context.Background(), version)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := client.Prepare(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	return prepared
}

// The prepared program runs under the name of its release, with 'config validate' on a copy of the
// configuration, and its exit status is the answer.
func TestReadsConfigRunsTheProgramOfTheRelease(t *testing.T) {
	out := filepath.Join(t.TempDir(), "seen")
	script := "#!/bin/sh\necho \"$(basename \"$0\") $1 $3 $4\" > " + out + "\ngrep -q readable \"$2\"\n"
	for version, name := range map[string]string{"v2.2.0": "qatlas", "v2.1.0": "qatlas", "v3.0.0": "qconnect"} {
		prepared := scriptRelease(t, version, script, 0)
		for config, want := range map[string]bool{"readable: yes\n": true, "other\n": false} {
			reads, err := prepared.ReadsConfig(context.Background(), []byte(config))
			if err != nil || reads != want {
				t.Fatalf("%s: ReadsConfig(%q) = %v, %v, want %v", version, config, reads, err, want)
			}
		}
		seen, _ := os.ReadFile(out)
		if got := strings.TrimSpace(string(seen)); got != name+" --config config validate" {
			t.Errorf("%s ran as %q", version, got)
		}
	}
}

// Exit status 0 is a yes and any other a no; a program ended by a signal, or one that cannot be run, is no
// answer but an error.
func TestReadsConfigFollowsTheExitStatus(t *testing.T) {
	for name, tt := range map[string]struct {
		script string
		want   bool
		fails  bool
	}{
		"exit 0":        {"#!/bin/sh\nexit 0\n", true, false},
		"exit 1":        {"#!/bin/sh\nexit 1\n", false, false},
		"exit 3":        {"#!/bin/sh\nexit 3\n", false, false},
		"killed":        {"#!/bin/sh\nkill -9 $$\n", false, true},
		"not a program": {"no shebang, not a program", false, true},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := scriptRelease(t, "v3.0.0", tt.script, 0).ReadsConfig(context.Background(), []byte("version: 4\n"))
			if got != tt.want || (err != nil) != tt.fails {
				t.Fatalf("ReadsConfig() = %v, %v; want %v, error %v", got, err, tt.want, tt.fails)
			}
		})
	}
}

// The program gets a copy in a private directory of mode 0700, an environment without the caller's
// configuration, vault, home, or tokens, and the directory is gone afterwards.
func TestReadsConfigRunsOnACopyInAPrivateEmptyEnvironment(t *testing.T) {
	t.Setenv("QCONNECT_CONFIG", "/real/config.yaml")
	t.Setenv("QCONNECT_HOME", "/real/home")
	t.Setenv("GITHUB_TOKEN", "canary-token")
	t.Setenv("GH_TOKEN", "canary-token")
	record := filepath.Join(t.TempDir(), "record")
	script := "#!/bin/sh\n" + `{ echo "args=$*"; echo "cwd=$(pwd)"; echo "mode=$(stat -c %a . 2>/dev/null || stat -f %Lp .)"; echo "copy=$(cat "$2")"; env; } > ` +
		record + "\nexit 0\n"
	secretConfig := "version: 4\n# canary-config-value\n"
	if ok, err := scriptRelease(t, "v3.0.0", script, 0).ReadsConfig(context.Background(), []byte(secretConfig)); !ok || err != nil {
		t.Fatalf("ReadsConfig() = %v, %v", ok, err)
	}
	data, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	for _, bad := range []string{"QCONNECT_CONFIG", "QCONNECT_HOME", "canary-token", "/real/"} {
		if strings.Contains(out, bad) {
			t.Errorf("the program saw %q:\n%s", bad, out)
		}
	}
	var dir, cwd string
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "args=--config "):
			dir = filepath.Dir(strings.TrimSuffix(strings.TrimPrefix(line, "args=--config "), " config validate"))
		case strings.HasPrefix(line, "cwd="):
			cwd = strings.TrimPrefix(line, "cwd=")
		}
	}
	// On macOS the temporary directory lies behind the /var symlink, so pwd names it by another path. The
	// directory itself is gone by now, so only its parent can be resolved.
	if parent, err := filepath.EvalSymlinks(filepath.Dir(dir)); err != nil || filepath.Join(parent, filepath.Base(dir)) != cwd {
		t.Errorf("the copy lies in %s (%v), the program ran in %s", dir, err, cwd)
	}
	for _, want := range []string{"args=--config " + dir + "/config.yaml config validate", "mode=700", "canary-config-value", "HOME=" + dir} {
		if !strings.Contains(out, want) {
			t.Errorf("the record lacks %q:\n%s", want, out)
		}
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("the check directory %s still exists: %v", dir, err)
	}
}

// A program that runs too long is no answer, and its process is really ended.
func TestReadsConfigEndsAProgramThatRunsTooLong(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	prepared := scriptRelease(t, "v3.0.0", "#!/bin/sh\necho $$ > "+pidFile+"\nexec sleep 30\n", 300*time.Millisecond)
	start := time.Now()
	ok, err := prepared.ReadsConfig(context.Background(), []byte("version: 4\n"))
	if ok || err == nil || !strings.Contains(err.Error(), "did not finish") {
		t.Fatalf("ReadsConfig() = %v, %v", ok, err)
	}
	if time.Since(start) > 10*time.Second {
		t.Errorf("took %s", time.Since(start))
	}
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 0 {
		t.Fatalf("pid %q: %v", raw, err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for syscall.Kill(pid, 0) == nil && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if syscall.Kill(pid, 0) == nil {
		t.Errorf("process %d still runs", pid)
	}
}
