//go:build linux || darwin || windows

package cli

import (
	"context"
	"errors"
	"path/filepath"

	"filippo.io/age"
	"golang.org/x/crypto/ssh"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/vault"
	"github.com/castrowithcee/qatlas-connect/internal/vaultproc"
)

// vaultProcessPlatform reports whether this platform runs a vault process.
const vaultProcessPlatform = true

// runVaultServe is 'qconnect vault serve', the vault process itself (see vaultproc.Serve). It is started by
// 'qconnect vault unlock', or by the TUI's own 'ctrl+l' (see vaultproc.StartProcess); run any other way, it
// refuses before it reads anything. As a successor it is started by a running vault process from the program
// an update installed; only a platform that hands the vault over starts one (see vaultHandoverPlatform).
func runVaultServe(opts *Options, _ *capability.Registry, successor bool) error {
	path, err := absConfigPath(opts)
	if err != nil {
		return err
	}
	var keys []ssh.PublicKey
	if vaultHandoverPlatform {
		keys = releaseKeys()
	}
	err = vaultproc.Serve(context.Background(), vaultproc.ServeOptions{ConfigPath: path, Successor: successor,
		SuccessorConfig: successorConfigPath, TrustedKeys: keys})
	if errors.Is(err, vaultproc.ErrNotStarted) {
		return &UsageError{errors.New("'qconnect vault serve' is started by 'qconnect vault unlock'; run that instead")}
	}
	return err
}

// successorConfigPath is the configuration file a successor serves, started with path. A vault process of
// the release before qatlas-connect that serves the default location of that release starts its successor
// with that location's file, which this release reaches only once migrated. Where path is exactly that file,
// nothing in the environment names an explicit place, and such a process serves the vault, the successor
// migrates as a run from the default location does and serves the current default file. It does not start
// when the migration fails or the migrated vault is not the one key opens, rather than serve a place no new
// client reaches. Any other path stays as it is.
func successorConfigPath(path string, key *age.X25519Identity) (string, error) {
	vaultDir := vault.New(filepath.Dir(path)).Dir()
	if !config.LegacyDefault(path) || !vaultproc.EarlierServes(vaultDir) {
		return path, nil
	}
	current, err := config.MigrateDefault()
	if err != nil {
		return "", err
	}
	if recipient, err := vault.New(filepath.Dir(current)).Recipient(); err != nil ||
		recipient != key.Recipient().String() {
		return "", errors.New("the vault in the current configuration directory is not the vault handed over")
	}
	return current, nil
}
