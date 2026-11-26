package vaultproc

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/castrowithcee/qatlas-kit/background"
	kitvaultproc "github.com/castrowithcee/qatlas-kit/vaultproc"
)

// A handover's reason is told by its kind, never by the kit's text: a file over the limit is the archive
// before the commit and the program at it, and a process that was already locking is not running.
func TestHandoverReasonsAreToldAsBefore(t *testing.T) {
	tooLarge := &kitvaultproc.HandoverError{Reason: kitvaultproc.HandoverTooLarge}
	for _, tc := range []struct {
		err    error
		commit bool
		want   string
	}{
		{tooLarge, false, ErrHandover.Error() + ": it could not verify the release"},
		{tooLarge, true, ErrHandover.Error() + ": the program installed is not the one the release holds"},
		{&kitvaultproc.HandoverError{Reason: kitvaultproc.HandoverProgramUnchanged}, true,
			ErrHandover.Error() + ": its program was not replaced"},
		{&kitvaultproc.HandoverError{Reason: kitvaultproc.HandoverNoSuccessor}, true,
			ErrHandover.Error() + ": no successor took the vault over"},
		{&kitvaultproc.HandoverError{Reason: kitvaultproc.HandoverRollback}, false,
			ErrHandover.Error() + ": no successor took the vault over"},
	} {
		if got := translateIn(tc.err, tc.commit); got.Error() != tc.want || !errors.Is(got, ErrHandover) {
			t.Errorf("translateIn(%v, %v) = %v, want %q", tc.err, tc.commit, got, tc.want)
		}
	}
	locked := &kitvaultproc.HandoverError{Reason: kitvaultproc.HandoverLocked}
	if got := translate(locked); got != ErrNotRunning {
		t.Errorf("translate(%v) = %v, want ErrNotRunning", locked, got)
	}
}

// A failed start is told by the kind of the kit's error, with the cause it carries.
func TestStartFailuresAreToldByKind(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, tc := range []struct {
		ctx  context.Context
		err  error
		want string
	}{
		{context.Background(), background.ErrTimeout, "the vault process did not report within " + StartTimeout.String()},
		{canceled, fmt.Errorf("the background process did not report: %w", context.Canceled),
			"the vault process did not report: context canceled"},
		{canceled, background.ErrTimeout, "the vault process did not report: context canceled"},
		{context.Background(), background.ErrEnded, "the vault process ended before it was ready"},
		{context.Background(), background.ErrHandoverNotTaken, "the vault process did not take the vault"},
		{context.Background(), &background.ReportError{Reason: "the vault key handed over cannot be read"},
			"the vault process did not start: the vault key handed over cannot be read"},
	} {
		if got := translateStart(tc.ctx, tc.err); got == nil || got.Error() != tc.want {
			t.Errorf("translateStart(%v) = %v, want %q", tc.err, got, tc.want)
		}
	}
}
