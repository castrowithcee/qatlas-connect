//go:build linux || darwin || windows

package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/vault"
	"github.com/castrowithcee/qatlas-connect/internal/vaultproc"
)

const (
	// testBinaryEnv names the process id of the test binary that runs the tests. A test run sets it for
	// itself; every process this binary starts as a helper inherits it and ends itself once that process is
	// gone, however it went (timeout, kill), because nothing then runs its cleanups.
	testBinaryEnv = "QATLAS_CLI_TEST_BINARY_PID"
	// pidRegistryEnv names a directory in which every process running as qconnect (runAsQatlasEnv) creates
	// an empty file named after its process id, so a test can end what it started even when the process no
	// longer answers on its socket.
	pidRegistryEnv = "QATLAS_CLI_TEST_PID_REGISTRY"
	// orphanEnv makes the test binary start a vault process through 'vault unlock' in the configuration
	// directory orphanEnv_DIR, print its process id and exit without any cleanup.
	orphanEnv = "QATLAS_CLI_TEST_ORPHAN"

	guardInterval = 250 * time.Millisecond
	// endGrace is how long a process may take to end by itself before it is killed.
	endGrace = 5 * time.Second
)

// guardTestBinary ends this helper process as soon as the test binary named by testBinaryEnv is gone. It
// watches that binary, not the parent: a successor after a vault handover has lost its parent by design.
func guardTestBinary() {
	pid, err := strconv.Atoi(os.Getenv(testBinaryEnv))
	if err != nil || pid <= 0 {
		return
	}
	if !processAlive(pid) {
		os.Exit(1)
	}
	go func() {
		for range time.Tick(guardInterval) {
			if !processAlive(pid) {
				os.Exit(1)
			}
		}
	}()
}

// registerPID records this process in the registry named by pidRegistryEnv, if there is one.
func registerPID() {
	dir := os.Getenv(pidRegistryEnv)
	if dir == "" {
		return
	}
	_ = os.WriteFile(filepath.Join(dir, strconv.Itoa(os.Getpid())), nil, 0o600)
}

// unregisterPID removes the entry of this process when it ends regularly, so a later cleanup never sees its
// process id once the system may have reused it.
func unregisterPID() {
	if dir := os.Getenv(pidRegistryEnv); dir != "" {
		_ = os.Remove(filepath.Join(dir, strconv.Itoa(os.Getpid())))
	}
}

// guardEnv is what a helper process started with an explicit environment needs to be guarded.
func guardEnv() []string {
	return []string{testBinaryEnv + "=" + os.Getenv(testBinaryEnv)}
}

// pidRegistry creates a registry directory for the test and ends every process left in it on cleanup. It
// returns the pidRegistryEnv entry for an explicit environment.
func pidRegistry(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Cleanup(func() { endRegistered(t, dir) })
	return pidRegistryEnv + "=" + dir
}

// errorReporter is the part of testing.T that endRegistered needs, so a test can catch its reports.
type errorReporter interface {
	Errorf(format string, args ...any)
}

// endRegistered kills every process in the registry dir that still runs after endGrace and reports each as
// an error. It only ever touches the process ids this test's own processes registered.
func endRegistered(t errorReporter, dir string) {
	entries, _ := os.ReadDir(dir)
	var pids []int
	for _, e := range entries {
		if pid, err := strconv.Atoi(e.Name()); err == nil && pid > 0 {
			pids = append(pids, pid)
		}
	}
	for deadline := time.Now().Add(endGrace); time.Now().Before(deadline); {
		running := false
		for _, pid := range pids {
			running = running || processAlive(pid)
		}
		if !running {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	for _, pid := range pids {
		if processAlive(pid) {
			killProcess(pid)
			t.Errorf("the vault process %d did not end by itself and was killed", pid)
		}
	}
}

// runOrphan is the intermediate process of TestVaultProcessEndsWithTheTestBinary: it stands in for a test
// binary that is aborted, starts a vault process and leaves without ending it.
func runOrphan() int {
	dir := os.Getenv(orphanEnv + "_DIR")
	_ = os.Setenv(runAsQatlasEnv, "1")
	vaultProcessSupported = true
	readVaultPassphrase = offeringPassphrase("s3cret-phrase")
	opts := &Options{}
	if code := run(newRootCommand(opts, sharedRegistry()), opts,
		[]string{"vault", "unlock", "--config", configIn(dir)}, os.Stderr, os.Stderr); code != exitOK {
		return code
	}
	client, err := vaultproc.ProcessClientOf(vault.New(dir))
	if err != nil {
		return 1
	}
	status, err := client.Status(context.Background())
	if err != nil {
		return 1
	}
	_, _ = os.Stdout.WriteString(strconv.Itoa(status.PID) + "\n")
	return 0
}

// startOrphan runs this binary as the intermediate process of runOrphan and returns the process id of the
// vault process it left behind. The intermediate counts as the test binary for its vault process.
func startOrphan(t *testing.T, dir string, registry string) int {
	t.Helper()
	program, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(program)
	cmd.Env = append(os.Environ(), orphanEnv+"=1", orphanEnv+"_DIR="+dir, runAsQatlasEnv+"=",
		"XDG_RUNTIME_DIR="+shortRuntimeDir(t), registry)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("the intermediate process failed: %v, %q", err, out)
	}
	pid, err := strconv.Atoi(string(trimNewline(out)))
	if err != nil {
		t.Fatalf("the intermediate process printed %q, want a process id", out)
	}
	return pid
}

func trimNewline(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}
