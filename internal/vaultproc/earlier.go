//go:build linux || darwin || windows

package vaultproc

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/castrowithcee/qatlas-kit/localipc"
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

// earlierEndpoint returns the channel the release before qatlas-connect serves the vault in vaultDir on,
// false where it cannot be told or is this release's own, as it is in a build that stands in for that release
// or where both fall back to the same socket beside the vault.
func earlierEndpoint(vaultDir string) (localipc.Endpoint, bool) {
	e, err := endpointNamed(vaultDir, legacyRuntimeDirName, legacyPipeName,
		"the vault process channel of the release before qatlas-connect")
	if err != nil {
		return localipc.Endpoint{}, false
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

// EarlierServes reports whether a vault process of the release before qatlas-connect serves the vault in
// vaultDir and none of this release does, which is when a successor takes the vault over from that process.
func EarlierServes(vaultDir string) bool { return earlierServesFor(vaultDir, vaultDir) }

// earlierServesFor reports whether a vault process of the release before qatlas-connect serves the vault in
// fromDir and none of this release serves the vault in vaultDir.
func earlierServesFor(fromDir, vaultDir string) bool {
	e, ok := earlierEndpoint(fromDir)
	if !ok {
		return false
	}
	if pid, err := probeEarlier(context.Background(), e); err != nil || pid <= 1 {
		return false
	}
	current, err := Endpoint(vaultDir)
	if err != nil {
		return false
	}
	_, err = probeEarlier(context.Background(), current)
	return errors.Is(err, localipc.ErrNotServed)
}

// errNoEarlierForDir refuses a successor for another vault directory than the one handed over without a
// vault process of the earlier release to take it over from: only such a process hands a vault over across
// a migration.
var errNoEarlierForDir = errors.New("no vault process of the earlier release serves the vault handed over")

// successorEarlier is the earlier channel a successor serving the vault in vaultDir takes the vault over
// from, handed over for the vault in fromDir; nil where there is none. A successor for another directory
// fails with errNoEarlierForDir unless a vault process of the earlier release serves fromDir and none of
// this release serves vaultDir.
func successorEarlier(fromDir, vaultDir string) (*localipc.Endpoint, error) {
	e, ok := earlierEndpoint(fromDir)
	if filepath.Clean(fromDir) != filepath.Clean(vaultDir) && (!ok || !earlierServesFor(fromDir, vaultDir)) {
		return nil, errNoEarlierForDir
	}
	if !ok {
		return nil, nil
	}
	return &e, nil
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
