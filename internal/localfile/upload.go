package localfile

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash"
	"io"
	"io/fs"
	"os"

	"github.com/castrowithcee/qatlas-connect/internal/config"
)

// Upload is a local file opened for an upload. It reads exactly Size bytes, the size the file had when it
// was opened, and fails when the file turns out shorter or longer, so what a provider receives matches the
// size announced to it.
type Upload struct {
	// Name is the last element of the path as the request wrote it.
	Name string
	// Size is the number of bytes Read yields.
	Size int64

	ctx  context.Context
	file *os.File
	hash hash.Hash
	read int64
	done bool
	// total is the size of the whole file when a range was opened; zero with ranged unset. A range ends
	// inside the file, so a change shows as another size of the file, not as bytes after the range.
	total  int64
	ranged bool
}

// OpenForUpload opens the regular file at path for reading, if it lies inside a directory the connection
// releases for reading. A path the request may not use yields a *PathError.
func OpenForUpload(ctx context.Context, resolved *config.Resolved, path string) (*Upload, error) {
	return openUpload(ctx, resolved, path, nil, 0)
}

// ToEnd as Range.Length reads from the offset to the end of the file.
const ToEnd int64 = -1

// Range selects Length bytes of a file from byte Offset on; ToEnd reads up to the end of the file.
type Range struct {
	Offset, Length int64
}

// RangeError reports a range the file cannot serve or the caller's limit does not allow. Nothing was read.
type RangeError struct{ Reason string }

func (e *RangeError) Error() string { return "the range " + e.Reason }

// OpenRangeForUpload is OpenForUpload for the bytes of r only: Size is the length of the range, Read yields
// just those bytes, and SHA256 covers just them. The range must lie inside the file, hold at least one byte
// (an empty file only as the whole range 0 to ToEnd), and be at most maxLength bytes; otherwise it returns a
// *RangeError. The checks of the path, the file type, the links, and the size limit of the whole file are
// those of OpenForUpload.
func OpenRangeForUpload(ctx context.Context, resolved *config.Resolved, path string, r Range, maxLength int64) (*Upload, error) {
	if r.Offset < 0 || r.Length < ToEnd || r.Length == 0 || maxLength < 1 {
		return nil, &RangeError{Reason: "is invalid: offset must not be negative and length must be at least 1"}
	}
	return openUpload(ctx, resolved, path, &r, maxLength)
}

func openUpload(ctx context.Context, resolved *config.Resolved, path string, r *Range, maxLength int64) (*Upload, error) {
	const direction = config.LocalFilesRead
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	loc, err := locate(resolved, direction, path)
	if err != nil {
		return nil, err
	}
	root, err := openRoot(loc, direction)
	if err != nil {
		return nil, err
	}
	defer root.Close()

	// The check before opening keeps a FIFO or a device from being opened at all; the check of the open
	// handle below is the one that counts, since the name may point elsewhere by then.
	before, err := root.Stat(loc.rel)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, pathError(direction, reasonMissing)
	case err != nil:
		return nil, pathError(direction, reasonUnreachable)
	case !before.Mode().IsRegular():
		return nil, pathError(direction, reasonNotRegular)
	}
	if beforeOpen != nil {
		beforeOpen()
	}
	file, err := root.OpenFile(loc.rel, os.O_RDONLY|openFlags, 0)
	if err != nil {
		return nil, pathError(direction, reasonUnreachable)
	}
	info, err := file.Stat()
	switch {
	case err != nil:
		file.Close()
		return nil, failure("examining", err)
	case !info.Mode().IsRegular():
		file.Close()
		return nil, pathError(direction, reasonNotRegular)
	case !os.SameFile(before, info):
		file.Close()
		return nil, pathError(direction, reasonChanged)
	case !singleLink(file, info):
		file.Close()
		return nil, pathError(direction, reasonHardLinks)
	case info.Size() > maxBytes:
		file.Close()
		return nil, pathError(direction, "names a file larger than the limit of "+formatBytes(maxBytes))
	}
	up := &Upload{Name: loc.name, Size: info.Size(), ctx: ctx, file: file, hash: sha256.New()}
	if r == nil {
		return up, nil
	}
	length, err := r.within(info.Size(), maxLength)
	if err == nil && r.Offset > 0 {
		_, err = file.Seek(r.Offset, io.SeekStart)
	}
	if err != nil {
		file.Close()
		var rangeErr *RangeError
		if errors.As(err, &rangeErr) {
			return nil, err
		}
		return nil, failure("positioning", err)
	}
	up.total, up.ranged, up.Size = info.Size(), true, length
	return up, nil
}

// within returns the number of bytes r selects in a file of the given size.
func (r Range) within(size, maxLength int64) (int64, error) {
	length := r.Length
	switch {
	case size == 0 && r.Offset == 0 && length == ToEnd:
		return 0, nil
	case r.Offset >= size:
		return 0, &RangeError{Reason: "starts outside the file"}
	case length == ToEnd:
		length = size - r.Offset
	case length > size-r.Offset:
		return 0, &RangeError{Reason: "ends outside the file"}
	}
	if length > maxLength {
		return 0, &RangeError{Reason: "is longer than the limit of " + formatBytes(maxLength)}
	}
	return length, nil
}

// Read reads the next bytes of the file. After Size bytes it makes sure the file ended there and then
// returns io.EOF.
func (u *Upload) Read(p []byte) (int, error) {
	if err := u.ctx.Err(); err != nil {
		return 0, err
	}
	if u.done {
		return 0, io.EOF
	}
	remaining := u.Size - u.read
	if remaining == 0 && u.ranged {
		info, err := u.file.Stat()
		switch {
		case err != nil:
			return 0, failure("examining", err)
		case info.Size() != u.total:
			return 0, errChanged
		}
		u.done = true
		return 0, io.EOF
	}
	if remaining == 0 {
		var probe [1]byte
		n, err := u.file.Read(probe[:])
		switch {
		case n > 0:
			return 0, errChanged
		case err != nil && !errors.Is(err, io.EOF):
			return 0, failure("reading", err)
		}
		u.done = true
		return 0, io.EOF
	}
	if int64(len(p)) > remaining {
		p = p[:remaining]
	}
	n, err := u.file.Read(p)
	u.hash.Write(p[:n])
	u.read += int64(n)
	switch {
	case errors.Is(err, io.EOF) && u.read < u.Size:
		return n, errChanged
	case err != nil && !errors.Is(err, io.EOF):
		return n, failure("reading", err)
	}
	return n, nil
}

// errChanged reports a file whose size changed while it was read.
var errChanged = errors.New("the local file changed while it was read")

// SHA256 returns the lowercase hex SHA-256 of the file's content, once Read has returned io.EOF; before,
// it reports false.
func (u *Upload) SHA256() (string, bool) {
	if !u.done {
		return "", false
	}
	return hex.EncodeToString(u.hash.Sum(nil)), true
}

// Close releases the file.
func (u *Upload) Close() error {
	if err := u.file.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
		return failure("closing", err)
	}
	return nil
}
