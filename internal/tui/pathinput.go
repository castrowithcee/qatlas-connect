package tui

import (
	"os"
	"path/filepath"

	"github.com/castrowithcee/qatlas-kit/projectpath"
)

// Every path the TUI asks for in a dialog is typed into the kit's dialog with the suggestions of a
// projectpath.Suggester (see pathSuggestions); these helpers read what was typed.

func endsWithSeparator(s string) bool {
	return s != "" && os.IsPathSeparator(s[len(s)-1])
}

// absolutePath is path as the working directory and the home directory make it absolute: a leading ~ is the
// home directory, which the file system would otherwise take as a directory of that name.
func absolutePath(path string) string {
	if expanded, err := projectpath.Expand(path); err == nil {
		path = expanded
	}
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}
