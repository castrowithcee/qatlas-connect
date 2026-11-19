package vault

import (
	"errors"
	"os"
	"time"

	"github.com/castrowithcee/qatlas-kit/fileretry"
)

// transientFile reports a file failure a short wait may cure, such as a sharing violation while another
// process reads or replaces the file. sleepFile waits between attempts; nil means time.Sleep. Tests replace
// both.
var (
	transientFile = fileretry.Transient
	sleepFile     func(time.Duration)
)

// retryFile runs a file operation that changes nothing when it fails, and repeats it while the failure is
// transient. A missing file is never transient: it is an answer, not a lock.
func retryFile(op func() error) error {
	return fileretry.Do(op, fileretry.Options{Transient: func(err error) bool {
		return !errors.Is(err, os.ErrNotExist) && transientFile(err)
	}, Sleep: sleepFile})
}

// statFile is os.Stat that waits out a transient failure.
func statFile(path string) (info os.FileInfo, err error) {
	err = retryFile(func() (err error) {
		info, err = os.Stat(path)
		return err
	})
	return info, err
}

// readWhole is os.ReadFile that waits out a transient failure. The file is closed before it returns, so
// the caller holds no handle that would block a writer replacing the file.
func readWhole(path string) (data []byte, err error) {
	err = retryFile(func() (err error) {
		data, err = os.ReadFile(path)
		return err
	})
	return data, err
}

// renameFile is os.Rename that waits out a transient failure. A failed rename leaves both names as they
// were, so repeating it is safe.
func renameFile(from, to string) error {
	return retryFile(func() error { return os.Rename(from, to) })
}
