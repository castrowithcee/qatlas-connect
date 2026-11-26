// Package vaultproc is Connect's vault process: an unlocked vault held in a process of its own, so later
// invocations of qconnect, the CLI and the MCP broker alike, read secrets without asking for the passphrase
// again.
//
// The process, its channel, the check of both ends, the challenge, the locks and the handover to a successor
// are the kit's (qatlas-kit vaultproc and localipc). This package holds what is Connect's own: the names of
// the channel (Endpoint), the proof label, the document the process holds (the secrets and the bindings that
// say which connection may read which credential), the operations on it (get, check, set, delete, bind, log,
// logcheck, approve-token, names, update and store-refresh), what the process reads from settings.age with
// its own key, the events it writes to the invocation log, and the formats and channels of earlier releases
// it still takes a vault over from or ends.
//
// A secret leaves the process only for a connection the vault approved (see vault.Bindings). While it runs,
// the process is the one writer of the invocation log that signs its entries, with the log key it derives
// from the vault's key, which never leaves it; see invokelog. No error or answer other than a successful get
// or store-refresh carries a secret value.
package vaultproc

import (
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"time"

	"github.com/castrowithcee/qatlas-kit/localipc"
	kitvaultproc "github.com/castrowithcee/qatlas-kit/vaultproc"
)

// Version is Connect's protocol version within the kit's protocol (kitvaultproc.Config.Version). Either side
// refuses a message of another version. Version 11 is the first on the kit's channel; versions up to 10
// spoke Connect's own protocol, which a client of this release answers as ErrVersion.
const Version = 11

// DefaultIdleTimeout is how long the process holds the vault without a get before it locks itself, where
// settings.age names no idle time.
const DefaultIdleTimeout = 12 * time.Hour

// DefaultRequestTimeout bounds one request on either side of a connection.
const DefaultRequestTimeout = kitvaultproc.DefaultRequestTimeout

// HandoverTimeout bounds each phase of a handover.
const HandoverTimeout = kitvaultproc.HandoverTimeout

// maxHandoverFile is the largest release archive the process reads and program it hashes in a handover,
// stricter than the kit's limit.
const maxHandoverFile = 128 << 20

// The errors of a client. The kit's errors are told as these, with the texts Connect always had, and still
// match the kit's (see translate).
var (
	// ErrNotRunning reports that no vault process answers: the vault is not unlocked in a process.
	ErrNotRunning = kitvaultproc.ErrNotRunning
	// ErrRunning reports that another vault process already serves the channel.
	ErrRunning = errors.New("a vault process is already running")
	// ErrRefused reports a peer that is not this program run by this user. It wraps the reason, which never
	// contains a secret.
	ErrRefused = errors.New("the process at the other end of the vault socket is not this qconnect of this user")
	// errUnproven reports a process that did not prove that it holds the vault's key.
	errUnproven = fmt.Errorf("%w: it cannot prove that it holds this vault's key", ErrRefused)
	// ErrProgramRefused reports that the vault process refused this program, as it refuses every program
	// once its own was replaced. It wraps ErrRefused.
	ErrProgramRefused = fmt.Errorf("%w: the vault process refused this program", ErrRefused)
	// ErrReplaced reports a vault process that hands, or handed, the vault over to a successor, or whose
	// handover did not finish. It wraps ErrProgramRefused, which such a process answered before.
	ErrReplaced = fmt.Errorf("%w; its own program was replaced since it started", ErrProgramRefused)
	// ErrVersion reports a vault process of another protocol version, one of another build.
	ErrVersion = errors.New("the vault process speaks another protocol version; lock it and unlock the vault again")
	// ErrHandover reports a handover that did not happen; the vault process locked itself. It wraps the
	// reason, which never carries a secret or a path.
	ErrHandover = errors.New("the vault process did not hand the vault over and locked itself")
	// ErrNoRecipient reports a client without a vault recipient: the vault is not encrypted.
	ErrNoRecipient = errors.New("the vault has no recipient to check the vault process with; it is not encrypted")

	// ErrNoLog reports a vault process that does not keep the invocation log, or cannot sign it.
	ErrNoLog = errors.New("the vault process does not keep the invocation log")
	// ErrNoNames reports a vault process that cannot tell the names of the vault.
	ErrNoNames = errors.New("the vault process cannot tell the names the vault holds")
	// ErrNoTokens reports a vault process that does not approve with agent tokens.
	ErrNoTokens = errors.New("the vault process does not approve with agent tokens")
	// ErrUnsupported reports a platform the vault process does not run on.
	ErrUnsupported = fmt.Errorf("the vault process is not supported on this platform: %w", errors.ErrUnsupported)
)

// maxSocketPath is the longest socket path the kit accepts, in bytes.
const maxSocketPath = 103

// PathTooLongError reports a socket path the kernel would cut off.
type PathTooLongError struct{ Path string }

func (e *PathTooLongError) Error() string {
	return fmt.Sprintf("the vault socket path %s is %d bytes long, more than the %d a socket path can hold; "+
		"use a shorter configuration directory or set XDG_RUNTIME_DIR", e.Path, len(e.Path), maxSocketPath)
}

// PeerError is a failure at a process that did listen, with its process id.
type PeerError = localipc.PeerError

// Endpoint returns the channel of the vault process that serves the vault in vaultDir, as vault.Vault.Dir
// reports it. On Unix the socket lives at $XDG_RUNTIME_DIR/qatlas-connect, on Linux without the variable at
// /run/user/<uid>/qatlas-connect, and otherwise in run beside the vault (<parent>/run); on macOS a path too
// long for a socket moves to $TMPDIR/qatlas-connect. Its name is a hash of the vault's absolute path. On
// Windows it is the pipe qatlas-connect-vault-<sid>-<hash>.
func Endpoint(vaultDir string) (localipc.Endpoint, error) {
	return endpointNamed(vaultDir, runtimeDirName, pipeName, "the vault process channel")
}

// endpointNamed is Endpoint under the names of a release.
func endpointNamed(vaultDir, runtimeDir, pipe, label string) (localipc.Endpoint, error) {
	abs, err := filepath.Abs(vaultDir)
	if err != nil {
		return localipc.Endpoint{}, fmt.Errorf("cannot determine the vault directory: %w", err)
	}
	prefix := "vault-"
	if runtime.GOOS == "windows" {
		prefix = ""
	}
	base, err := kitvaultproc.SocketBase(prefix, abs)
	if err != nil {
		return localipc.Endpoint{}, err
	}
	return localipc.Endpoint{
		RuntimeDirName:  runtimeDir,
		SocketBase:      base,
		FallbackDir:     filepath.Dir(abs),
		FallbackDirName: "run",
		// A runtime directory others may read, though not write, holds the socket as it did before the kit.
		RuntimeAccess: localipc.RuntimeReadable,
		PipePrefix:    pipe,
		ErrorName:     label,
	}, nil
}

// SocketPath returns the socket, or on Windows the pipe, of the vault process for the vault in vaultDir.
func SocketPath(vaultDir string) (string, error) {
	e, err := Endpoint(vaultDir)
	if err != nil {
		return "", err
	}
	path, err := e.Path()
	return path, translate(err)
}

// ReplacedProgram returns the path this process's program had at its start and whether that file was
// removed or replaced since, as an update does. Where that cannot be told, it reports not replaced.
func ReplacedProgram() (string, bool) {
	path, replaced, err := localipc.ProgramReplaced()
	if err != nil {
		return "", false
	}
	return path, replaced
}
