//go:build linux || darwin || windows

package vaultproc

import (
	"errors"
	"testing"

	"github.com/castrowithcee/qatlas-kit/localipc"
)

// Only a process localipc accepts as the holder, whose id names a process that can be ended, is one to end;
// nothing serving is nothing to report, and every refusal, an id that cannot be used among them, is
// reported.
func TestEarlierPID(t *testing.T) {
	refused := &localipc.PeerError{PID: 4242, Err: localipc.ErrRefused}
	for _, tc := range []struct {
		name    string
		pid     int
		err     error
		want    int
		unknown bool
	}{
		{"this user's process", 4242, nil, 4242, false},
		{"nothing serves", 0, localipc.ErrNotServed, 0, false},
		{"a refused process", 0, refused, 0, true},
		{"another PID namespace", 0, nil, 0, true},
		{"a negative id", -1, nil, 0, true},
		{"the init process", 1, nil, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := earlierPID(tc.pid, tc.err)
			if got != tc.want || (err != nil) != tc.unknown || (tc.unknown && !errors.Is(err, ErrEarlierUnknown)) {
				t.Fatalf("earlierPID(%d, %v) = %d, %v, want %d, unknown %v", tc.pid, tc.err, got, err, tc.want,
					tc.unknown)
			}
		})
	}
}
