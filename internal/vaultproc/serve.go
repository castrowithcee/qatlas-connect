package vaultproc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"time"

	"filippo.io/age"
	"golang.org/x/crypto/ssh"

	"github.com/castrowithcee/qatlas-connect/internal/release"
	"github.com/castrowithcee/qatlas-connect/internal/vault"
	"github.com/castrowithcee/qatlas-kit/background"
	"github.com/castrowithcee/qatlas-kit/localipc"
	kitvault "github.com/castrowithcee/qatlas-kit/vault"
	kitvaultproc "github.com/castrowithcee/qatlas-kit/vaultproc"
)

// SuccessorFlag is the hidden flag of 'qconnect vault serve' that starts it as the successor of a running
// vault process. With the arguments 'vault serve --config <path>' it is the successor contract between
// releases, so it stays.
const SuccessorFlag = "--successor"

// StartTimeout bounds how long StartProcess waits for the vault process it started.
const StartTimeout = 10 * time.Second

// programName is the program's file name, which a release archive holds as bin/qconnect.
const programName = "qconnect"

// ErrNotStarted reports 'qconnect vault serve' run by hand rather than by 'qconnect vault unlock'.
var ErrNotStarted = kitvaultproc.ErrNotStarted

// serveArgs are the arguments of 'qconnect vault serve' for the configuration at configPath.
func serveArgs(configPath string, successor bool) []string {
	args := []string{"vault", "serve", "--config", configPath}
	if successor {
		args = append(args, SuccessorFlag)
	}
	return args
}

// StartProcess starts 'qconnect vault serve' for the configuration at configPath as the vault process of the
// unlocked vault snap describes, through the kit (kitvaultproc.Start): this same program, detached, handed
// the vault's key and secrets through an inherited pipe only. It returns once client reaches the process.
func StartProcess(ctx context.Context, configPath string, snap vault.Snapshot, client *Client) (Status, error) {
	if !Supported {
		return Status{}, ErrUnsupported
	}
	key, err := age.ParseX25519Identity(snap.Identity)
	if err != nil {
		return Status{}, errors.New("the vault key cannot be read")
	}
	d := documentOf(snap.Secrets, snap.Bindings)
	doc, err := d.encode()
	d.wipe()
	if err != nil {
		return Status{}, err
	}
	defer clear(doc)
	if err := client.check(); err != nil {
		return Status{}, err
	}
	st, err := kitvaultproc.Start(ctx, client.kit(), background.Command{Args: serveArgs(configPath, false)},
		kitvault.Snapshot{Identity: key, Document: doc}, background.Options{Timeout: StartTimeout})
	if err != nil {
		return Status{}, translateStart(ctx, err)
	}
	return statusOf(st), nil
}

// ServeOptions configure Serve.
type ServeOptions struct {
	// ConfigPath is the absolute configuration file the process was started with.
	ConfigPath string
	// Successor starts the process as the successor of a running vault process.
	Successor bool
	// SuccessorConfig, with Successor, returns the configuration a successor serves, from the one it was
	// started with and the vault's key; nil keeps ConfigPath.
	SuccessorConfig func(configPath string, key *age.X25519Identity) (string, error)
	// TrustedKeys verify a release an update installs. Nil leaves the process without a handover, as on a
	// platform that does not hand over: an update then locks it, and it reports no update setting.
	TrustedKeys []ssh.PublicKey
}

// Serve is the vault process, 'qconnect vault serve', through kitvaultproc.Run. The idle time, the maximum
// unlock time, the log retention and the update setting come from settings.age, read with the key the
// process holds, never from the configuration file, which an agent may have written. A successor also takes
// the vault over from a vault process of a release before the kit's channel (see decodeEarlier) and from one
// that serves the channel of the release before qatlas-connect.
func Serve(ctx context.Context, opts ServeOptions) error {
	if opts.Successor && opts.TrustedKeys == nil {
		return errors.New("a vault process is never handed over on this platform, so no successor starts")
	}
	run := kitvaultproc.RunOptions{Successor: opts.Successor}
	if opts.Successor {
		run.DecodeEarlier = decodeEarlier
	}
	var configureErr error
	run.Configure = func(key *age.X25519Identity, _ kitvaultproc.Config, _ *localipc.Endpoint) (kitvaultproc.Config,
		*localipc.Endpoint, error) {
		path := opts.ConfigPath
		fromDir := vault.New(filepath.Dir(path)).Dir()
		if opts.Successor && opts.SuccessorConfig != nil {
			var err error
			if path, err = opts.SuccessorConfig(path, key); err != nil {
				return kitvaultproc.Config{}, nil, err
			}
		}
		vaultDir := vault.New(filepath.Dir(path)).Dir()
		cfg, err := processConfig(vaultDir, key, opts.TrustedKeys, path)
		if err != nil {
			return kitvaultproc.Config{}, nil, err
		}
		if !opts.Successor {
			return cfg, nil, nil
		}
		earlier, err := successorEarlier(fromDir, vaultDir)
		if err != nil {
			configureErr = err
			return kitvaultproc.Config{}, nil, err
		}
		return cfg, earlier, nil
	}
	err := kitvaultproc.Run(ctx, run)
	switch {
	case errors.Is(err, kitvaultproc.ErrNotStarted):
		return ErrNotStarted
	case err != nil && configureErr != nil:
		// The kit reports a failed Configure without its text; the process still ends with it.
		return configureErr
	}
	return translate(err)
}

// processConfig is the kit configuration of the vault process of the vault in vaultDir, from settings.age
// read with key. An untrusted settings.age gives the defaults: the default idle time, no maximum, no log
// truncation, and lock on an update.
func processConfig(vaultDir string, key *age.X25519Identity, trusted []ssh.PublicKey, configPath string) (
	kitvaultproc.Config, error) {
	endpoint, err := Endpoint(vaultDir)
	if err != nil {
		return kitvaultproc.Config{}, err
	}
	idle, maxUnlock, retention := processSettings(vaultDir, key)
	p := newProcess(key, processOptions{vaultDir: vaultDir, keepLog: true, retentionDays: retention, tokens: true,
		update: trusted != nil})
	cfg := p.config(limits{endpoint: endpoint, idleTimeout: idle, maxUnlock: maxUnlock})
	if trusted != nil {
		cfg.Handover = p.handover(trusted, configPath)
	}
	return cfg, nil
}

// processSettings are the idle time, the maximum unlock time and the log retention authenticated in
// settings.age, read with key. An untrusted settings.age gives the default idle time, no maximum and a
// retention of zero, which never truncates.
func processSettings(vaultDir string, key *age.X25519Identity) (idle, maxUnlock time.Duration, retention int) {
	settings, _ := vault.ReadSettings(vaultDir, key)
	idle = settings.IdleTimeout
	if idle <= 0 {
		idle = DefaultIdleTimeout
	}
	return idle, settings.MaxUnlock, settings.RetentionDays
}

// handover is what the process needs to hand the vault over to a successor: the release keys and namespace
// of a release after the rename, the program's name, the successor contract's arguments, the update setting,
// and Connect's stricter limit on the archive and the program.
func (p *process) handover(trusted []ssh.PublicKey, configPath string) *kitvaultproc.HandoverConfig {
	return &kitvaultproc.HandoverConfig{Namespace: release.Namespace, TrustedKeys: trusted, ProgramName: programName,
		SuccessorArgs: serveArgs(configPath, true), HandsOver: p.handsOver, MaxFileSize: maxHandoverFile}
}

// ServerOptions configure a Server.
type ServerOptions struct {
	// IdleTimeout is how long the server holds the vault without a get; zero means DefaultIdleTimeout.
	IdleTimeout time.Duration
	// KeepLog makes the server the writer of the vault's invocation log, keeping RetentionDays days.
	KeepLog       bool
	RetentionDays int
	// Tokens lets the server approve with agent tokens and tell the vault's names.
	Tokens bool
	// TrustedKeys let the server hand over and report the update setting, as a vault process on Linux and
	// macOS does; ConfigPath is the configuration a successor is started with.
	TrustedKeys []ssh.PublicKey
	ConfigPath  string
}

// Server is a vault process served from the calling process, with the configuration a vault process has,
// for a test that reaches it through a Client.
type Server struct {
	kit  *kitvaultproc.Server
	l    net.Listener
	done chan struct{}
	err  error
}

// NewServer listens on the channel of the vault in vaultDir and serves what snap holds from this process
// until Close.
func NewServer(vaultDir string, snap vault.Snapshot, opts ServerOptions) (*Server, error) {
	key, err := age.ParseX25519Identity(snap.Identity)
	if err != nil {
		return nil, errors.New("the vault key cannot be read")
	}
	endpoint, err := Endpoint(vaultDir)
	if err != nil {
		return nil, err
	}
	idle := opts.IdleTimeout
	if idle <= 0 {
		idle = DefaultIdleTimeout
	}
	p := newProcess(key, processOptions{vaultDir: vaultDir, keepLog: opts.KeepLog, retentionDays: opts.RetentionDays,
		tokens: opts.Tokens, update: opts.TrustedKeys != nil})
	cfg := p.config(limits{endpoint: endpoint, idleTimeout: idle})
	if opts.TrustedKeys != nil {
		cfg.Handover = p.handover(opts.TrustedKeys, opts.ConfigPath)
	}
	d := documentOf(snap.Secrets, snap.Bindings)
	doc, err := d.encode()
	d.wipe()
	if err != nil {
		return nil, err
	}
	server, err := kitvaultproc.NewServer(cfg, kitvaultproc.State{Identity: key, Document: doc}, false)
	if err != nil {
		return nil, err
	}
	l, err := endpoint.ListenWith(localipc.ListenOptions{Refused: server.Refused})
	if err != nil {
		_ = server.Close()
		return nil, err
	}
	s := &Server{kit: server, l: l, done: make(chan struct{})}
	go func() {
		s.err = server.Serve(l)
		close(s.done)
	}()
	return s, nil
}

// Close locks the server, as a lock request would, and waits until it overwrote what it held.
func (s *Server) Close() error {
	_ = s.kit.Close()
	<-s.done
	return s.err
}

// Done is closed once the server locked.
func (s *Server) Done() <-chan struct{} { return s.done }

// String names the server for a test's messages.
func (s *Server) String() string { return fmt.Sprintf("vault process server at %s", s.l.Addr()) }
