package tui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-kit/projectpath"
	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// tempHome points the home directory at a new temporary one, on every platform.
func tempHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

// treeOf makes the directories and files below root; a name ending in a slash is a directory.
func treeOf(t *testing.T, root string, names ...string) {
	t.Helper()
	for _, name := range names {
		full := filepath.Join(root, filepath.FromSlash(name))
		if strings.HasSuffix(name, "/") {
			mustNoError(t, os.MkdirAll(full, 0o700))
		} else {
			mustNoError(t, os.WriteFile(full, nil, 0o600))
		}
	}
}

// pathDialog opens the dialog that adds a path to the list of a connection, below a home of its own
// that holds the directory alpha, the file alpha.txt and a hidden directory.
func pathDialog(t *testing.T) *Model {
	t.Helper()
	treeOf(t, tempHome(t), "alpha/", "alpha.txt", ".hidden/")
	return pathDialogIn(t, t.TempDir())
}

// pathDialogIn opens that dialog with start as the directory the editor was started in, the one suggestion of
// an empty input. It sets no environment, so it may run beside other tests.
func pathDialogIn(t *testing.T, start string) *Model {
	t.Helper()
	m, _ := toolsModel(t, wikiRegistry(t), map[string]config.Connection{"wiki": {Service: "wiki", Credential: "reader"}})
	sizedModel(m, 120, 40)
	openConnection(t, m, "wiki")
	openPathList(t, m)
	m.startDir = start
	press(t, m, "n")
	if m.question == nil || m.pathEdit < 0 {
		t.Fatalf("n opened no path dialog: screen %v", m.screen)
	}
	return m
}

// inHome is name below the home directory, written as a person types it.
func inHome(name string) string { return "~" + string(filepath.Separator) + name }

// The dialogs suggest by the mode of what they ask for: a path of a connection only directories, an export
// or import file directories before files; a hidden entry only once its dot is typed. A directory keeps a
// dialog that asks for a file open.
func TestPathSuggestionsFollowTheModeOfTheDialog(t *testing.T) {
	m := pathDialog(t)
	sep := string(filepath.Separator)
	texts := func(s []kittui.Suggestion) []string {
		var out []string
		for _, x := range s {
			out = append(out, x.Text)
		}
		return out
	}
	dirs := m.pathSuggestions(projectpath.Directories)(inHome(""))
	if got := texts(dirs); !slices.Equal(got, []string{inHome("alpha" + sep)}) || dirs[0].Continues {
		t.Errorf("directories = %+v, want only alpha, which closes the dialog", dirs)
	}
	files := m.pathSuggestions(projectpath.Files)(inHome("al"))
	if got := texts(files); !slices.Equal(got, []string{inHome("alpha" + sep), inHome("alpha.txt")}) ||
		!files[0].Continues || files[1].Continues {
		t.Errorf("files = %+v, want the directory first, which keeps the dialog open, then the file", files)
	}
	if got := texts(m.pathSuggestions(projectpath.Directories)(inHome("."))); !slices.Equal(got, []string{inHome(".hidden" + sep)}) {
		t.Errorf("a typed dot suggests %q, want the hidden directory", got)
	}
}

// tab takes the suggestion; enter takes the typed text, and the marked suggestion only after up or down marked
// one; ctrl+n and ctrl+p mark as down and up do.
func TestPathDialogCompletesAndTakesTheMarkOnlyAfterMarking(t *testing.T) {
	sep := string(filepath.Separator)
	m := pathDialog(t)
	typeText(t, m, inHome("al"))
	press(t, m, "tab")
	if got := typedInput(m); got != inHome("alpha"+sep) {
		t.Fatalf("tab completed to %q", got)
	}
	press(t, m, "enter")
	if m.pathEdit >= 0 || !slices.Equal(m.pathList.all, []string{inHome("alpha")}) {
		t.Fatalf("enter did not take the typed path: edit %d list %v fail %q", m.pathEdit, m.pathList.all, m.fail)
	}

	m = pathDialog(t)
	typeText(t, m, inHome("."))
	press(t, m, "ctrl+n", "ctrl+p", "down", "enter")
	if m.pathEdit >= 0 || !slices.Equal(m.pathList.all, []string{inHome(".hidden")}) {
		t.Fatalf("enter after marking did not take the suggestion: edit %d list %v fail %q", m.pathEdit, m.pathList.all, m.fail)
	}
}

// An export file dialog keeps open on a marked directory and suggests what is in it; the file is then typed.
func TestExportFileDialogKeepsOpenOnADirectory(t *testing.T) {
	v, _ := plainExportModel(t)
	m := v.m
	treeOf(t, tempHome(t), "alpha/", "alpha/old.yaml")
	v.startExport("")
	v.advance(exportTarget, exportWithout, "")
	v.typePath(inHome("al"))
	v.pump("down", "enter")
	sep := string(filepath.Separator)
	if m.export.step != exportTarget || typedInput(m) != inHome("alpha"+sep) {
		t.Fatalf("a marked directory = step %v text %q, want the dialog open in it", m.export.step, typedInput(m))
	}
	if view := askedText(m); !strings.Contains(view, inHome("alpha"+sep+"old.yaml")) {
		t.Errorf("the directory's file is not suggested:\n%s", view)
	}
	typeText(t, m, "new.yaml")
	v.pump("enter")
	if m.export.step != exportPreview || !strings.HasSuffix(m.export.target, filepath.Join("alpha", "new.yaml")) {
		t.Fatalf("enter on the typed file = step %v target %q fail %q", m.export.step, m.export.target, m.fail)
	}
}

// A directory reached through a symbolic link, such as the temporary directory of macOS, is suggested and taken
// under the name that was typed; nothing resolves the link.
func TestPathSuggestionsKeepASymlinkedDirectory(t *testing.T) {
	root := t.TempDir()
	treeOf(t, root, "real/alpha/")
	link := filepath.Join(root, "link")
	if err := os.Symlink(filepath.Join(root, "real"), link); err != nil {
		t.Skipf("no symbolic link here: %v", err)
	}
	m := pathDialog(t)
	sep := string(filepath.Separator)
	got := m.pathSuggestions(projectpath.Directories)(link + sep)
	if len(got) != 1 || got[0].Text != link+sep+"alpha"+sep {
		t.Fatalf("suggestions below the link = %+v, want its directory under the link", got)
	}
	if abs := absolutePath(link + sep + "alpha"); abs != filepath.Join(link, "alpha") {
		t.Errorf("absolutePath = %q, want the path through the link", abs)
	}
}
