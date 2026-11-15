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

// handoverReasons are the reasons of the kit's ErrHandover as Connect has always told them.
var handoverReasons = map[string]string{
	"it could not verify the release":                        "it could not verify the release",
	"its program was not replaced":                           "its program was not replaced",
	"the installed program is not the one the release holds": "the program installed is not the one the release holds",
	"no successor took the vault over":                       "no successor took the vault over",
	// A process without a handover answered as one whose successor did not start.
	"this platform or process does not hand over": "no successor took the vault over",
}

// translate tells an error of the kit as Connect always told it, keeping the process id of the peer it
// names. Errors with nothing of Connect's own are left as they are.
func translate(err error) error {
	if err == nil || errors.Is(err, ErrNotRunning) {
		return err
	}
	var peer *localipc.PeerError
	pid, inner := 0, err
	if errors.As(err, &peer) {
		pid, inner = peer.PID, peer.Err
	}
	var told error
	var tooLong *localipc.PathTooLongError
	switch {
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
		reason := handoverReason(inner)
		if old, ok := handoverReasons[reason]; ok {
			reason = old
		}
		told = tell(ErrHandover, err, reason)
	case errors.Is(err, kitvaultproc.ErrNoRecipient):
		told = tell(ErrNoRecipient, err, "")
	case errors.Is(err, kitvaultproc.ErrRunning):
		told = tell(ErrRunning, err, "")
	case errors.Is(err, localipc.ErrRefused):
		told = tell(ErrRefused, err, strings.TrimPrefix(inner.Error(), localipc.ErrRefused.Error()+": "))
	default:
		return err
	}
	if pid > 0 {
		return &PeerError{PID: pid, Err: told}
	}
	return told
}

// handoverReason finds the reason the kit gave its ErrHandover in the chain of err, "" for none.
func handoverReason(err error) string {
	if err == nil {
		return ""
	}
	if text, prefix := err.Error(), kitvaultproc.ErrHandover.Error()+": "; strings.HasPrefix(text, prefix) {
		return strings.TrimPrefix(text, prefix)
	}
	switch wrapped := err.(type) {
	case interface{ Unwrap() error }:
		return handoverReason(wrapped.Unwrap())
	case interface{ Unwrap() []error }:
		for _, e := range wrapped.Unwrap() {
			if reason := handoverReason(e); reason != "" {
				return reason
			}
		}
	}
	return ""
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
	case ctx.Err() != nil && strings.HasPrefix(err.Error(), "the background process did not report"):
		return fmt.Errorf("the vault process did not report: %w", ctx.Err())
	}
	switch text := err.Error(); {
	case strings.HasPrefix(text, "the vault process does not answer: ") && errors.Unwrap(err) != nil:
		return fmt.Errorf("the vault process does not answer: %w", translate(errors.Unwrap(err)))
	case text == "the background process did not report in time":
		return errors.New("the vault process did not report within " + StartTimeout.String())
	case text == "the background process ended before it reported":
		return errors.New("the vault process ended before it was ready")
	case text == "the background process did not take the handover":
		return errors.New("the vault process did not take the vault")
	case strings.HasPrefix(text, "cannot start the background process: "):
		return fmt.Errorf("cannot start the vault process: %w", errors.Unwrap(err))
	case strings.HasPrefix(text, "cannot find this program to start it in the background: "):
		return fmt.Errorf("cannot find this program to start the vault process: %w", errors.Unwrap(err))
	}
	return translate(err)
}
