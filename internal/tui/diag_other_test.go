//go:build !darwin

package tui

// Temporary macOS diagnosis, to be removed: nothing to explain elsewhere.

import (
	"testing"

	"github.com/castrowithcee/qatlas-kit/localipc"
)

func macDiagReplaced(testing.TB, localipc.Endpoint, int, func(string) (localipc.Endpoint, error), func(string) int) {
}
