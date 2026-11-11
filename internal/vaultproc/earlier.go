//go:build linux || darwin || windows

package vaultproc

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"time"

	"github.com/castrowithcee/qatlas-kit/localipc"
	kitvaultproc "github.com/castrowithcee/qatlas-kit/vaultproc"
)

// ErrEarlierUnknown reports that something serves the socket, or the pipe, of the release before
// qatlas-connect for a vault, but cannot be told to be a vault process of this user. It is not ended.
var ErrEarlierUnknown = errors.New("something serves the vault process socket of the release before " +
	"qatlas-connect, but its process cannot be identified, so it is not ended")

// earlierEndWait bounds how long EndEarlier waits for the process it ended to stop serving.
const earlierEndWait = 2 * time.Second

// earlierProbeTimeout bounds one look at the earlier socket or pipe.
const earlierProbeTimeout = time.Second

// FindEarlier returns the id of the vault process of the release before qatlas-connect that holds the vault
// in vaultDir unlocked, which this release cannot ask, since it reaches that process by other names; 0 where
// there is none.
//
// Only another process of this user counts that serves the earlier release's channel for vaultDir as
// localipc's Endpoint.Holder accepts it. Anything else that serves the channel is reported as
// ErrEarlierUnknown.
func FindEarlier(vaultDir string) (int, error) {
	e, ok := earlierEndpoint(vaultDir)
	if !ok {
		return 0, nil
	}
	return earlierPID(probeEarlier(context.Background(), e))
}

// EndEarlier ends the vault process FindEarlier finds for vaultDir with localipc's Endpoint.EndHolder, on
// which it overwrites its secrets and ends. It asks nothing of it, and waits until it no longer serves. It
// returns the id of the process ended, 0 where there was none. A process that cannot be identified is not
// ended (see FindEarlier).
func EndEarlier(ctx context.Context, vaultDir string) (int, error) {
	e, ok := earlierEndpoint(vaultDir)
	if !ok {
		return 0, nil
	}
	pid, err := earlierPID(probeEarlier(ctx, e))
	if pid == 0 || err != nil {
		return 0, err
	}
	endCtx, cancel := context.WithTimeout(ctx, earlierProbeTimeout)
	ended, err := e.EndHolder(endCtx)
	cancel()
	switch {
	case errors.Is(err, localipc.ErrNotServed):
		return 0, nil
	case err != nil:
		return pid, fmt.Errorf("cannot end the vault process of the release before qatlas-connect (pid %d): %w",
			pid, err)
	}
	for deadline := time.Now().Add(earlierEndWait); ; {
		if _, err := probeEarlier(ctx, e); errors.Is(err, localipc.ErrNotServed) {
			return ended, nil
		}
		if time.Now().After(deadline) {
			return ended, fmt.Errorf("the vault process of the release before qatlas-connect (pid %d) did not end "+
				"in time", ended)
		}
		select {
		case <-ctx.Done():
			return ended, ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// earlierEndpoint returns the channel the earlier release serves the vault in vaultDir on, false where it
// cannot be told or is this release's own, as it is in a build that stands in for the earlier release.
func earlierEndpoint(vaultDir string) (localipc.Endpoint, bool) {
	abs, err := filepath.Abs(vaultDir)
	if err != nil {
		return localipc.Endpoint{}, false
	}
	prefix := "vault-"
	if runtime.GOOS == "windows" {
		prefix = ""
	}
	base, err := kitvaultproc.SocketBase(prefix, abs)
	if err != nil {
		return localipc.Endpoint{}, false
	}
	e := localipc.Endpoint{
		RuntimeDirName: legacyRuntimeDirName,
		SocketBase:     base,
		FallbackDir:    filepath.Join(filepath.Dir(abs), "run"),
		PipePrefix:     legacyPipeName,
		ErrorName:      "the vault process channel of the release before qatlas-connect",
	}
	path, err := e.Path()
	if err != nil {
		return localipc.Endpoint{}, false
	}
	if current, err := SocketPath(vaultDir); err != nil || current == path {
		return localipc.Endpoint{}, false
	}
	return e, true
}

// probeEarlier asks localipc who serves e, within earlierProbeTimeout.
func probeEarlier(ctx context.Context, e localipc.Endpoint) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, earlierProbeTimeout)
	defer cancel()
	return e.Holder(ctx)
}

// earlierPID decides on what probeEarlier found: the id of a process that may be ended, 0 where nothing
// serves the channel, and ErrEarlierUnknown for anything else, an id that names no process to end safely
// among it.
func earlierPID(pid int, err error) (int, error) {
	switch {
	case errors.Is(err, localipc.ErrNotServed):
		return 0, nil
	case err != nil:
		return 0, fmt.Errorf("%w: %w", ErrEarlierUnknown, err)
	case pid <= 1:
		// 1 is the init process of a PID namespace, which this release never ends.
		return 0, fmt.Errorf("%w: its process id %d cannot be used", ErrEarlierUnknown, pid)
	}
	return pid, nil
}
