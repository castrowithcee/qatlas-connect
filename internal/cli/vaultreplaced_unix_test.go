//go:build linux || darwin

package cli

import (
	"strconv"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/vault"
	"github.com/castrowithcee/qatlas-connect/internal/vaultproc/vaultproctest"
)

// 'vault lock' and 'vault unlock' end a vault process whose handover did not finish and that still answers
// replaced, the unlock telling so with the process id, and then asking the passphrase and starting a new one.
// The process stands in a process of its own, as another process of this user behind the vault's channel.
func TestVaultLockAndUnlockEndAReplacedVaultProcess(t *testing.T) {
	dir := encryptedVaultFixture(t, "")
	withVaultProcess(t, dir)
	withVaultPassphrase(t, offeringPassphrase("s3cret-phrase"))
	vaultDir := vault.New(dir).Dir()

	fake := vaultproctest.Start(t, vaultDir, "", vaultproctest.Answer("replaced"))
	code, stdout, stderr := runWithInput(t, &Options{}, "", "vault", "lock", "--config", configIn(dir))
	if code != exitOK || !strings.Contains(stdout, "the vault is locked") {
		t.Fatalf("lock: exit code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	if !fake.Ended(endGrace) {
		t.Fatal("lock did not end the replaced vault process")
	}

	fake = vaultproctest.Start(t, vaultDir, "", vaultproctest.Answer("replaced"))
	code, stdout, stderr = runWithInput(t, &Options{}, "", "vault", "unlock", "--config", configIn(dir))
	if code != exitOK || !strings.Contains(stdout, "unlocked in a vault process (pid ") {
		t.Fatalf("unlock: exit code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	if !fake.Ended(endGrace) {
		t.Fatal("unlock did not end the replaced vault process")
	}
	if want := "ended the vault process (pid " + strconv.Itoa(fake.PID) + ")"; !strings.Contains(stderr, want) {
		t.Errorf("unlock: stderr = %q, want %q", stderr, want)
	}
}
