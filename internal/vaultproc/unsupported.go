//go:build !linux && !darwin && !windows

package vaultproc

import (
	"context"

	"github.com/castrowithcee/qatlas-kit/localipc"
)

// Supported reports whether a vault process runs on this platform. It does not here.
const Supported = false

// FindEarlier finds nothing on this platform, which runs no vault process.
func FindEarlier(string) (int, error) { return 0, nil }

// EndEarlier has nothing to end on this platform, which runs no vault process.
func EndEarlier(context.Context, string) (int, error) { return 0, nil }

// EarlierServes finds no earlier vault process on this platform.
func EarlierServes(string) bool { return false }

// successorEarlier finds no earlier channel on this platform, which starts no successor.
func successorEarlier(string, string) (*localipc.Endpoint, error) { return nil, nil }

// earlierEndpoint finds no earlier channel on this platform.
func earlierEndpoint(string) (localipc.Endpoint, bool) { return localipc.Endpoint{}, false }
