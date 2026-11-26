package vaultproc

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/castrowithcee/qatlas-kit/background"
	"github.com/castrowithcee/qatlas-kit/localipc"
	kitvaultproc "github.com/castrowithcee/qatlas-kit/vaultproc"
)

// toldError is a kit error told with Connect's text: it matches Connect's error and the kit's alike.
type toldError struct {
	text string
	errs []error
}

func (e *toldError) Error() string   { return e.text }
func (e *toldError) Unwrap() []error { return e.errs }

// tell is kit told as connect, with connect's text and reason appended where there is one.
func tell(connect, kit error, reason string) error {
	text := connect.Error()
	if reason != "" {
		text += ": " + reason
	}
	return &toldError{text: text, errs: []error{connect, kit}}
}

// handoverText is the reason of a handover that did not happen as Connect has always told it. A file over
// the limit is the release archive before the commit and the installed program at it.
func handoverText(reason kitvaultproc.HandoverReason, commit bool) string {
	switch reason {
	case kitvaultproc.HandoverNotVerified:
		return "it could not verify the release"
	case kitvaultproc.HandoverProgramUnchanged:
		return "its program was not replaced"
	case kitvaultproc.HandoverProgramMismatch:
		return "the program installed is not the one the release holds"
	case kitvaultproc.HandoverTooLarge:
		if commit {
			return "the program installed is not the one the release holds"
		}
		return "it could not verify the release"
	}
	return "no successor took the vault over"
}

// translate tells an error of the kit as Connect always told it, keeping the process id of the peer it
// names. Errors with nothing of Connect's own are left as they are.
func translate(err error) error { return translateIn(err, false) }

// translateIn is translate, which tells a handover's reason as it is at its commit where commit is set.
func translateIn(err error, commit bool) error {
	if err == nil {
		return nil
	}
	var peer *localipc.PeerError
	pid := 0
	if errors.As(err, &peer) {
		pid = peer.PID
	}
	var told error
	var tooLong *localipc.PathTooLongError
	var handover *kitvaultproc.HandoverError
	var refusal *localipc.RefusalError
	switch {
	case errors.As(err, &handover):
		switch handover.Reason {
		case kitvaultproc.HandoverLocked:
			return ErrNotRunning
		case kitvaultproc.HandoverBadRequest:
			told = tell(errInvalidRequest, err, "")
		default:
			told = tell(ErrHandover, err, handoverText(handover.Reason, commit))
		}
	case errors.Is(err, ErrNotRunning):
		return err
	case errors.As(err, &tooLong):
		return &PathTooLongError{Path: tooLong.Path}
	case errors.Is(err, kitvaultproc.ErrReplaced):
		told = tell(ErrReplaced, err, "")
	case errors.Is(err, kitvaultproc.ErrProgramRefused):
		told = tell(ErrProgramRefused, err, "")
	case errors.Is(err, kitvaultproc.ErrUnproven):
		told = tell(errUnproven, err, "")
	case errors.Is(err, kitvaultproc.ErrVersion):
		told = tell(ErrVersion, err, "")
	case errors.Is(err, kitvaultproc.ErrHandover):
		told = tell(ErrHandover, err, handoverText("", commit))
	case errors.Is(err, kitvaultproc.ErrNoRecipient):
		told = tell(ErrNoRecipient, err, "")
	case errors.Is(err, kitvaultproc.ErrRunning):
		told = tell(ErrRunning, err, "")
	case errors.As(err, &refusal):
		// The kit's refusal reads "<localipc.ErrRefused>: <reason>"; Connect tells the reason after its own.
		told = tell(ErrRefused, err, strings.TrimPrefix(refusal.Error(), localipc.ErrRefused.Error()+": "))
	case errors.Is(err, localipc.ErrRefused):
		told = tell(ErrRefused, err, "")
	default:
		return err
	}
	if pid > 0 {
		return &PeerError{PID: pid, Err: told}
	}
	return told
}

// translateStart tells a failure to start a vault process as Connect always told it. The kit reports it
// for any background process.
func translateStart(ctx context.Context, err error) error {
	var report *background.ReportError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &report):
		return fmt.Errorf("the vault process did not start: %s", report.Reason)
	case errors.Is(err, kitvaultproc.ErrNoAnswer):
		return fmt.Errorf("the vault process does not answer: %w", translate(errors.Unwrap(err)))
	case ctx.Err() != nil && (errors.Is(err, background.ErrTimeout) || errors.Is(err, ctx.Err())):
		return fmt.Errorf("the vault process did not report: %w", ctx.Err())
	case errors.Is(err, background.ErrTimeout):
		return errors.New("the vault process did not report within " + StartTimeout.String())
	case errors.Is(err, background.ErrEnded):
		return errors.New("the vault process ended before it was ready")
	case errors.Is(err, background.ErrHandoverNotTaken):
		return errors.New("the vault process did not take the vault")
	case errors.Is(err, background.ErrStartFailed):
		return fmt.Errorf("cannot start the vault process: %w", errors.Unwrap(err))
	case errors.Is(err, background.ErrProgramNotFound):
		return fmt.Errorf("cannot find this program to start the vault process: %w", errors.Unwrap(err))
	}
	return translate(err)
}
