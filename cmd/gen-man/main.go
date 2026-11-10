// Command gen-man generates release manpages from the real Cobra command tree.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/castrowithcee/qatlas-kit/manpage"
	"github.com/spf13/cobra/doc"

	"github.com/castrowithcee/qatlas-connect/internal/cli"
	"github.com/castrowithcee/qatlas-connect/internal/helptopics"
)

func main() {
	output := flag.String("output", "", "directory for generated manpages")
	version := flag.String("version", "dev", "Qatlas Connect version shown in the manpage metadata")
	flag.Parse()
	if *output == "" {
		fmt.Fprintln(os.Stderr, "gen-man: -output is required")
		os.Exit(2)
	}
	if err := generate(*output, *version); err != nil {
		fmt.Fprintf(os.Stderr, "gen-man: %v\n", err)
		os.Exit(1)
	}
}

// generate writes one manpage per command into output. Cobra leaves help topics out of the tree, so their
// text becomes a section of qconnect.1, the one page every installation carries.
func generate(output, version string) error {
	return manpage.Generate(output, "qconnect", version, topicsIntro, helptopics.All(), func(dir, version string) error {
		header := &doc.GenManHeader{Section: "1", Source: "Qatlas Connect " + version, Manual: "Qatlas Connect Manual"}
		return doc.GenManTree(cli.DocumentationCommand(version), header, dir)
	})
}

const topicsIntro = `The same text is printed by \fBqconnect help\fP \fItopic\fP and shown by ? in \fBqconnect tui\fP.`
