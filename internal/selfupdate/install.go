package selfupdate

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/release"
)

const (
	maxManpageSize  = 2 << 20
	maxSubpages     = 200
	maxSubpagesSize = 16 << 20
)

// installation is where a release goes: the program of its version's layout beside the running one in
// <prefix>/bin, its manual page in <prefix>/share/man/man1, and the other name of the transition beside
// the program. A release after the rename goes to qconnect and qatlas stays an alias of it; a release before
// the rename goes to qatlas, and qconnect is removed, since that program does not call itself so.
// Transition until the next major release.
type installation struct {
	layout release.Layout
	// running is the running program, a former-name alias followed.
	running string
	// program is where the release's program goes, and other the other name beside it.
	program, other  string
	manDir, manpage string
}

// installationOf checks that the running program is <prefix>/bin/qconnect or <prefix>/bin/qatlas, a
// regular file, and returns where a release with layout goes.
func (c *Client) installationOf(layout release.Layout) (installation, error) {
	executable := c.Executable
	if executable == "" {
		var err error
		if executable, err = os.Executable(); err != nil {
			return installation{}, fmt.Errorf("locate running executable: %w", err)
		}
	}
	absolute, err := filepath.Abs(executable)
	if err != nil {
		return installation{}, fmt.Errorf("resolve executable path: %w", err)
	}
	absolute = followAlias(absolute, runtime.GOOS)
	info, err := os.Lstat(absolute)
	if err != nil {
		return installation{}, fmt.Errorf("inspect installed executable: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return installation{}, &UnsupportedInstallationError{Reason: "the running executable must be a regular file, not a symlink"}
	}
	extension := ""
	if runtime.GOOS == "windows" {
		extension = ".exe"
	}
	binDir := filepath.Dir(absolute)
	if base := filepath.Base(absolute); (base != currentName+extension && base != formerName+extension) ||
		filepath.Base(binDir) != "bin" {
		return installation{}, &UnsupportedInstallationError{Reason: "install the release as <prefix>/bin/" + currentName + extension}
	}
	other := currentName
	if !layout.Legacy {
		other = formerName
	}
	manDir := filepath.Join(filepath.Dir(binDir), "share", "man", "man1")
	return installation{
		layout: layout, running: absolute,
		program: filepath.Join(binDir, layout.Program+extension), other: filepath.Join(binDir, other+extension),
		manDir: manDir, manpage: filepath.Join(manDir, layout.Program+".1"),
	}, nil
}

// prepare makes the program path of the release, and its manual page where it is one, a regular file the
// kit replaces in place: an alias of the running program there, or none, becomes a hard link of it, and a
// page that is an alias of the other name's page a hard link of that page. Either still runs or shows what
// its name stands for. undo puts back what is still as prepare left it, after an installation that failed.
func (i installation) prepare() (undo func(), err error) {
	var undos []func()
	undo = func() {
		for j := len(undos) - 1; j >= 0; j-- {
			undos[j]()
		}
	}
	u, err := materialize(i.program, i.running, true)
	if err != nil {
		return nil, err
	}
	undos = append(undos, u)
	if runtime.GOOS != "windows" {
		otherPage := filepath.Join(i.manDir, filepath.Base(strings.TrimSuffix(i.other, ".exe"))+".1")
		if u, err = materialize(i.manpage, otherPage, false); err != nil {
			undo()
			return nil, err
		}
		undos = append(undos, u)
	}
	return undo, nil
}

// materialize makes path a hard link of source beside it where path is a link to source's name, or, with
// create, where it is missing. Anything else stays as it is.
func materialize(path, source string, create bool) (undo func(), err error) {
	none := func() {}
	if path == source {
		return none, nil
	}
	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist) && create:
		if err := os.Link(source, path); err != nil {
			return nil, fmt.Errorf("prepare %s for the release: %w", filepath.Base(path), err)
		}
		return func() {
			if sameFile(path, source) {
				_ = os.Remove(path)
			}
		}, nil
	case err != nil || info.Mode()&os.ModeSymlink == 0:
		return none, nil
	}
	target, err := os.Readlink(path)
	if err != nil || target != filepath.Base(source) {
		return none, nil
	}
	if info, err := os.Lstat(source); err != nil || !info.Mode().IsRegular() {
		return none, nil
	}
	link := sibling(path, ".qconnect-link-")
	if err := os.Link(source, link); err != nil {
		return nil, fmt.Errorf("prepare %s for the release: %w", filepath.Base(path), err)
	}
	if err := os.Rename(link, path); err != nil {
		_ = os.Remove(link)
		return nil, fmt.Errorf("prepare %s for the release: %w", filepath.Base(path), err)
	}
	return func() {
		if sameFile(path, source) {
			_ = linkOver(path, target)
		}
	}, nil
}

func sameFile(a, b string) bool {
	ai, err := os.Lstat(a)
	if err != nil {
		return false
	}
	bi, err := os.Lstat(b)
	return err == nil && os.SameFile(ai, bi)
}

// sibling is a fresh name beside path with prefix.
func sibling(path, prefix string) string {
	return filepath.Join(filepath.Dir(path), prefix+strconv.Itoa(os.Getpid())+"-"+strconv.FormatInt(time.Now().UnixNano(), 36))
}

// finish puts the names beside the installed program in order. After a release before the rename,
// qconnect and its manual pages go. After a later one, qatlas becomes an alias of qconnect again, a copy of
// it on Windows, and the other manual pages of the verified archive are installed.
func (i installation) finish(files Release) error {
	if i.layout.Legacy {
		dropCurrentName(runtime.GOOS, i.other, i.manDir)
		return nil
	}
	if runtime.GOOS == "windows" {
		return replaceCopy(i.program, i.other)
	}
	installSubpages(i.manDir, files.Archive)
	return aliasFormerName(i.other)
}

// replaceCopy replaces other, the former name on Windows, with a copy of the installed program, where it is
// a regular file. other may be running: it is moved aside as the program was.
func replaceCopy(program, other string) error {
	if info, err := os.Lstat(other); err != nil || !info.Mode().IsRegular() {
		return nil
	}
	body, err := os.ReadFile(program)
	if err != nil {
		return fmt.Errorf("replace %s: %w", filepath.Base(other), err)
	}
	staged, err := stageFile(filepath.Dir(other), ".qconnect-bin-*", body, 0o755)
	if err != nil {
		return fmt.Errorf("replace %s: %w", filepath.Base(other), err)
	}
	defer os.Remove(staged)
	if err := replaceRunning(osFileOps, staged, other); err != nil {
		return fmt.Errorf("replace %s: %w", filepath.Base(other), err)
	}
	return nil
}

// installSubpages puts the manual pages qconnect-<name>.1 of the verified archive beside qconnect.1 and,
// when it installed all of the archive's, removes the qconnect-*.1 files that the archive no longer has. An
// archive without any such page removes none. The pages are secondary: whatever does not fit the name,
// count, or size limits, or cannot be installed, is left out and never fails the update.
func installSubpages(manDir, archive string) {
	pages, complete := subpages(archive)
	staged := map[string]string{}
	for name, body := range pages {
		path, err := stageFile(manDir, ".qconnect-man-*", body, 0o644)
		if err != nil {
			complete = false
			continue
		}
		defer os.Remove(path)
		staged[name] = path
	}
	for name, path := range staged {
		if err := os.Rename(path, filepath.Join(manDir, name)); err != nil {
			complete = false
		}
	}
	if complete && len(staged) > 0 {
		removeRegular(manDir, currentName+"-*.1", staged)
	}
}

// subpages reads the manual pages qconnect-<name>.1 directly under share/man/man1/ of the .tar.gz archive,
// by file name. complete says that no entry that looked like one was left out.
func subpages(archive string) (pages map[string][]byte, complete bool) {
	file, err := os.Open(archive)
	if err != nil {
		return nil, false
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return nil, false
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	pages, complete = map[string][]byte{}, true
	var total int64
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return pages, complete
		}
		if err != nil {
			return pages, false
		}
		dir, base := path.Split(strings.TrimPrefix(header.Name, "./"))
		if header.Typeflag != tar.TypeReg || dir != "share/man/man1/" || !strings.HasPrefix(base, currentName+"-") ||
			!strings.HasSuffix(base, ".1") {
			continue
		}
		if _, replaces := pages[base]; !subpageName.MatchString(base) || (!replaces && len(pages) >= maxSubpages) {
			complete = false
			continue
		}
		body, err := io.ReadAll(io.LimitReader(reader, maxManpageSize+1))
		if err != nil || len(body) > maxManpageSize || total+int64(len(body))-int64(len(pages[base])) > maxSubpagesSize {
			complete = false
			continue
		}
		total += int64(len(body)) - int64(len(pages[base]))
		pages[base] = body
	}
}

func stageFile(dir, pattern string, body []byte, mode os.FileMode) (path string, err error) {
	file, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", err
	}
	path = file.Name()
	defer func() {
		if closeErr := file.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
		if err != nil {
			os.Remove(path)
		}
	}()
	if err = file.Chmod(mode); err != nil {
		return "", err
	}
	if _, err = file.Write(body); err != nil {
		return "", err
	}
	return path, file.Sync()
}
