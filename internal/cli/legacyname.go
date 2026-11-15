package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/selfupdate"
)

// calledByLegacyName reports whether program, the name the process was started under (os.Args[0]), is the
// former program name. The base name counts without its Windows extension.
// Transition: the former name keeps working until the next major release.
func calledByLegacyName(program string) bool {
	base := strings.ToLower(filepath.Base(program))
	return strings.TrimSuffix(base, ".exe") == "qatlas"
}

// legacyNotice is the note the kit writes on stderr before the command when the program was started under
// its former name, or "" for the current name. It never goes to stdout, which carries the answer and, for
// 'qconnect mcp', the JSON-RPC stream. Hidden commands are processes the program starts itself, such as the
// vault process; they stay silent.
func legacyNotice(program string, args []string, reg *capability.Registry) string {
	if !calledByLegacyName(program) {
		return ""
	}
	// The notice is fixed when the tree is built, so a tree of its own tells whether args name a hidden command.
	if c, _, err := newRootCommand(&Options{}, reg).Find(args); err == nil && c.Hidden {
		return ""
	}
	return "note: this program is now called qconnect; the name qatlas still works until the next major release"
}

// adoptProgramName gives an installation that an updater of a release before the rename left as
// <prefix>/bin/qatlas its current name, with qatlas as an alias (see selfupdate.AdoptCurrentName), and says so
// on stderr in the one process that did it. Hidden commands, which the program starts itself, and
// self-contained ones leave the installation alone. Transition until the next major release.
func adoptProgramName(c *cobra.Command) {
	if c.Hidden || selfContained(c) {
		return
	}
	if path, ok := selfupdate.AdoptCurrentName(); ok {
		fmt.Fprintf(c.ErrOrStderr(), "qconnect: this program is now installed as %s; qatlas stays an alias of it "+
			"until the next major release\n", path)
	}
}
