package localfile

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func checkRangeError(t *testing.T, err error) {
	t.Helper()
	var rangeErr *RangeError
	if !errors.As(err, &rangeErr) {
		t.Fatalf("error = %v, want a *RangeError", err)
	}
	checkNoPath(t, err)
}

func TestOpenRangeForUpload(t *testing.T) {
	tr := newTree(t)
	name := filepath.Join(tr.read, "file.txt") // "hello"
	for _, tc := range []struct {
		name string
		r    Range
		want string
	}{
		{"middle", Range{Offset: 1, Length: 3}, "ell"},
		{"from start", Range{Offset: 0, Length: 2}, "he"},
		{"to the end", Range{Offset: 2, Length: ToEnd}, "llo"},
		{"whole file", Range{Offset: 0, Length: ToEnd}, "hello"},
		{"exactly to the end", Range{Offset: 4, Length: 1}, "o"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			up, err := OpenRangeForUpload(context.Background(), tr.resolved(), name, tc.r, 100)
			if err != nil {
				t.Fatal(err)
			}
			defer up.Close()
			if up.Size != int64(len(tc.want)) {
				t.Fatalf("Size = %d, want %d", up.Size, len(tc.want))
			}
			got, err := io.ReadAll(up)
			if err != nil || string(got) != tc.want {
				t.Fatalf("content = %q, %v; want %q", got, err, tc.want)
			}
			hexSum, ok := up.SHA256()
			if !ok || hexSum != sum(tc.want) {
				t.Fatalf("SHA256 = %q, %v; want the digest of the range only", hexSum, ok)
			}
		})
	}
}

func TestOpenRangeForUploadRefused(t *testing.T) {
	tr := newTree(t)
	name := filepath.Join(tr.read, "file.txt") // 5 bytes
	for _, tc := range []struct {
		name  string
		r     Range
		limit int64
	}{
		{"offset at the end", Range{Offset: 5, Length: ToEnd}, 100},
		{"offset beyond", Range{Offset: 9, Length: 1}, 100},
		{"length beyond", Range{Offset: 3, Length: 3}, 100},
		{"length far beyond", Range{Offset: 0, Length: 1 << 40}, 1 << 41},
		{"negative offset", Range{Offset: -1, Length: 1}, 100},
		{"zero length", Range{Offset: 0, Length: 0}, 100},
		{"negative length", Range{Offset: 0, Length: -2}, 100},
		{"over the caller's limit", Range{Offset: 0, Length: 4}, 3},
		{"to the end over the limit", Range{Offset: 1, Length: ToEnd}, 3},
		{"no limit", Range{Offset: 0, Length: 1}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			up, err := OpenRangeForUpload(context.Background(), tr.resolved(), name, tc.r, tc.limit)
			if err == nil {
				up.Close()
			}
			checkRangeError(t, err)
		})
	}
}

func TestOpenRangeForUploadAtLimit(t *testing.T) {
	tr := newTree(t)
	up, err := OpenRangeForUpload(context.Background(), tr.resolved(), filepath.Join(tr.read, "file.txt"), Range{Offset: 1, Length: 3}, 3)
	if err != nil {
		t.Fatalf("a range at the limit: %v", err)
	}
	up.Close()
}

func TestOpenRangeForUploadEmptyFile(t *testing.T) {
	tr := newTree(t)
	name := filepath.Join(tr.read, "empty.txt")
	writeFile(t, name, "")
	up, err := OpenRangeForUpload(context.Background(), tr.resolved(), name, Range{Offset: 0, Length: ToEnd}, 10)
	if err != nil {
		t.Fatal(err)
	}
	defer up.Close()
	got, err := io.ReadAll(up)
	if err != nil || len(got) != 0 || up.Size != 0 {
		t.Fatalf("content = %q, %v, size %d", got, err, up.Size)
	}
	if _, err := OpenRangeForUpload(context.Background(), tr.resolved(), name, Range{Offset: 0, Length: 1}, 10); err == nil {
		t.Fatal("a range of bytes in an empty file must be refused")
	}
}

// The whole-file limit still applies to a file a range is taken from.
func TestOpenRangeForUploadFileLimit(t *testing.T) {
	tr := newTree(t)
	withLimit(t, 4)
	_, err := OpenRangeForUpload(context.Background(), tr.resolved(), filepath.Join(tr.read, "file.txt"), Range{Offset: 0, Length: 1}, 10)
	checkPathError(t, err, "names a file larger than the limit of 4 bytes")
}

// A call without a range reads the whole file and its digest, as before.
func TestOpenForUploadWithoutRange(t *testing.T) {
	tr := newTree(t)
	up, err := OpenForUpload(context.Background(), tr.resolved(), filepath.Join(tr.read, "file.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer up.Close()
	got, err := io.ReadAll(up)
	if err != nil || string(got) != "hello" || up.Size != 5 {
		t.Fatalf("content = %q, %v, size %d", got, err, up.Size)
	}
	if hexSum, ok := up.SHA256(); !ok || hexSum != sum("hello") {
		t.Fatalf("SHA256 = %q, %v", hexSum, ok)
	}
}

func TestOpenRangeForUploadPaths(t *testing.T) {
	tr := newTree(t)
	r := Range{Offset: 0, Length: 1}
	_, err := OpenRangeForUpload(context.Background(), tr.resolved(), filepath.Join(tr.outside, "secret.txt"), r, 10)
	checkPathError(t, err, reasonOutside)
	_, err = OpenRangeForUpload(context.Background(), tr.resolved(), filepath.Join(tr.read, "nothing.txt"), r, 10)
	checkPathError(t, err, reasonMissing)
	_, err = OpenRangeForUpload(context.Background(), tr.resolved(), filepath.Join(tr.read, "sub"), r, 10)
	checkPathError(t, err, reasonNotRegular)
	_, err = OpenRangeForUpload(context.Background(), tr.resolved(), tr.read+string(filepath.Separator)+".."+string(filepath.Separator)+"outside"+string(filepath.Separator)+"secret.txt", r, 10)
	checkPathError(t, err, reasonDotDot)

	symlink(t, filepath.Join(tr.outside, "secret.txt"), filepath.Join(tr.read, "link.txt"))
	_, err = OpenRangeForUpload(context.Background(), tr.resolved(), filepath.Join(tr.read, "link.txt"), r, 10)
	checkPathError(t, err, "")
}

func TestOpenRangeForUploadHardLinks(t *testing.T) {
	tr := newTree(t)
	if err := os.Link(filepath.Join(tr.read, "file.txt"), filepath.Join(tr.read, "second.txt")); err != nil {
		t.Skipf("hard links are not available: %v", err)
	}
	_, err := OpenRangeForUpload(context.Background(), tr.resolved(), filepath.Join(tr.read, "file.txt"), Range{Offset: 0, Length: 1}, 10)
	checkPathError(t, err, reasonHardLinks)
}

func TestRangeChangedWhileRead(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(name string) error
	}{
		{"grown", func(name string) error {
			f, err := os.OpenFile(name, os.O_WRONLY|os.O_APPEND, 0)
			if err != nil {
				return err
			}
			defer f.Close()
			_, err = f.WriteString(" world")
			return err
		}},
		{"shrunk below the range", func(name string) error { return os.Truncate(name, 2) }},
		{"shrunk behind the range", func(name string) error { return os.Truncate(name, 4) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr := newTree(t)
			name := filepath.Join(tr.read, "file.txt")
			up, err := OpenRangeForUpload(context.Background(), tr.resolved(), name, Range{Offset: 1, Length: 3}, 10)
			if err != nil {
				t.Fatal(err)
			}
			defer up.Close()
			if err := tc.change(name); err != nil {
				t.Fatal(err)
			}
			if _, err = io.ReadAll(up); !errors.Is(err, errChanged) {
				t.Fatalf("error = %v, want errChanged", err)
			}
			if _, ok := up.SHA256(); ok {
				t.Fatal("SHA256 must not be ready for a file that changed")
			}
		})
	}
}
