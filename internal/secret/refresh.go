package secret

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/invokelog"
	"github.com/castrowithcee/qatlas-connect/internal/vault"
	"github.com/castrowithcee/qatlas-connect/internal/vaultproc"
	"github.com/castrowithcee/qatlas-kit/filelock"
)

// storeLockFile, beside the configuration, keeps the replacements of a refresh token in the system credential
// store of several processes apart. Its name ends in .lock, so a copy of the directory leaves it out.
const storeLockFile = ".credential-store.lock"

// The results a write of a refresh token is logged with besides an error code: conflict where another writer
// stored first.
const (
	resultSuccess  = "success"
	resultConflict = "conflict"
	resultFailed   = "store-failed"
)

// warningKey marks the function the warnings of a request go to.
type warningKey struct{}

// WithWarnings returns ctx whose warnings go to warn: lines for the person running the call, which never
// hold a value. The application core sets it for every request it hands to a provider; a request without it
// drops its warnings.
func WithWarnings(ctx context.Context, warn func(line string)) context.Context {
	return context.WithValue(ctx, warningKey{}, warn)
}

func warn(ctx context.Context, line string) {
	if report, ok := ctx.Value(warningKey{}).(func(string)); ok && report != nil {
		report(line)
	}
}

// LogWrites makes every refresh token this resolver writes itself, in an unencrypted vault, a vault unlocked
// in this process or the system credential store, an entry of log, with the connection and the outcome and
// never a value. The vault process logs the writes it makes itself.
func (r *Resolver) LogWrites(log invokelog.Writer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.writeLog = log
}

func (r *Resolver) logWrite(connection, result string) {
	r.mu.Lock()
	log := r.writeLog
	r.mu.Unlock()
	if log != nil {
		_ = log.Append(invokelog.Fields{Path: "cli", Operation: invokelog.OperationStoreRefreshToken,
			Connection: connection, Effect: "update", Result: result})
	}
}

// StoreRefreshToken stores rotated, a refresh token the token endpoint issued in place of old, for the
// connection ctx is bound to (see ForConnection), in the place source names: the stage that delivered old.
// It writes the refresh token role of that connection's own credential and nothing else, and only while the
// place still holds old: of two runs that renewed with the same old value the first one stores, and the
// other one gets what it stored. It returns the refresh token stored afterwards, which later renewals use:
// rotated, what another writer stored, or "" where none is stored any more. It never removes one.
//
// An encrypted vault locked here is written by its vault process, and only for a connection it approved as
// it is now; without one it is locked, a *VaultLockedError. An unencrypted vault, an encrypted one unlocked
// in this process and the system credential store are written by this process under a lock across
// processes. An environment variable cannot be written: the rotation is a warning (see WithWarnings), and
// rotated is used on. The plaintext file is not written any more, which ends the call.
func (r *Resolver) StoreRefreshToken(ctx context.Context, credential string, cred config.Credential,
	source Source, old, rotated string) (string, error) {
	role := config.RoleRefreshToken
	r.register(rotated)
	scope, bound := scopeFrom(ctx)
	if !bound || scope.Credential != credential {
		return "", fmt.Errorf("the refresh token of credential %s is stored only for a connection that reads it",
			credential)
	}
	if old == "" || rotated == "" {
		return "", vault.ErrEmptyReplacement
	}
	switch source {
	case SourceEnv:
		warn(ctx, fmt.Sprintf("the token endpoint issued a new refresh token for credential %s, role %s, which an "+
			"environment variable holds and qconnect cannot write; the call goes on, and the variable keeps the "+
			"old one", credential, role))
		return rotated, nil
	case SourceStore:
		return r.replaceInStore(ctx, scope.Connection, credential, role, old, rotated)
	case SourceVault:
		return r.replaceInVault(ctx, scope, credential, role, old, rotated)
	}
	return "", fmt.Errorf("the token endpoint issued a new refresh token for credential %s, role %s, which the %s "+
		"holds and nothing writes any more, so the new access token was not used; move the credential with "+
		"'qconnect vault migrate' and give a new consent", credential, role, source)
}

func (r *Resolver) replaceInVault(ctx context.Context, scope vault.Scope, credential, role, old,
	rotated string) (string, error) {
	if r.vault == nil {
		return "", errNoVault
	}
	if client := r.processClient(); client != nil {
		stored, err := client.StoreRefreshToken(ctx, credential, scope, old, rotated)
		switch {
		case err == nil:
			r.register(stored)
			return stored, nil
		case errors.Is(err, vault.ErrApprovalRequired):
			return "", &ApprovalRequiredError{Connection: scope.Connection, Credential: credential}
		case errors.Is(err, vaultproc.ErrNotRunning):
			return "", &VaultLockedError{Credential: credential, Role: role}
		}
		return "", &VaultProcessError{Credential: credential, Role: role, Err: err}
	}
	state, err := r.vault.State()
	if err != nil {
		return "", err
	}
	switch state {
	case vault.StateLocked:
		return "", &VaultLockedError{Credential: credential, Role: role}
	case vault.StateUnlocked:
		switch err := r.vault.CheckApprovalFor(scope, credential); {
		case errors.Is(err, vault.ErrApprovalRequired):
			r.logWrite(scope.Connection, "approval-required")
			return "", &ApprovalRequiredError{Connection: scope.Connection, Credential: credential}
		case err != nil:
			return "", err
		}
	}
	stored, replaced, err := r.vault.ReplaceRole(credential, role, old, rotated)
	if err != nil {
		r.logWrite(scope.Connection, resultFailed)
		return "", fmt.Errorf("the new refresh token of credential %s cannot be stored in the vault, so the new "+
			"access token was not used: %w", credential, err)
	}
	r.logReplaced(scope.Connection, replaced)
	r.register(stored)
	return stored, nil
}

func (r *Resolver) replaceInStore(ctx context.Context, connection, credential, role, old,
	rotated string) (string, error) {
	failed := func(err error) (string, error) {
		r.logWrite(connection, resultFailed)
		return "", fmt.Errorf("the new refresh token of credential %s cannot be stored in the credential store, "+
			"so the new access token was not used: %w", credential, err)
	}
	if r.lockDir == "" {
		return failed(errors.New("no directory holds its lock"))
	}
	unlock, err := lockFile(filepath.Join(r.lockDir, storeLockFile))
	if err != nil {
		return failed(err)
	}
	defer unlock()
	r.mu.Lock()
	unattended := r.unattended
	r.mu.Unlock()
	if unattended {
		ctx = withoutPrompt(ctx)
	}
	key := StoreKey(credential, role)
	current, err := r.store.Get(ctx, key)
	switch {
	case errors.Is(err, ErrNoEntry):
		current = ""
	case err != nil:
		return failed(err)
	}
	if current != old {
		r.logReplaced(connection, false)
		r.register(current)
		return current, nil
	}
	if err := r.store.Set(key, rotated); err != nil {
		return failed(err)
	}
	r.logReplaced(connection, true)
	return rotated, nil
}

func (r *Resolver) logReplaced(connection string, replaced bool) {
	if replaced {
		r.logWrite(connection, resultSuccess)
		return
	}
	r.logWrite(connection, resultConflict)
}

// lockFile takes the cross-process lock on path and returns the function that releases it.
func lockFile(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("cannot open the lock file %s: %w", path, err)
	}
	if err := filelock.Lock(f); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("cannot lock %s: %w", path, err)
	}
	return func() {
		_ = filelock.Unlock(f)
		_ = f.Close()
	}, nil
}
