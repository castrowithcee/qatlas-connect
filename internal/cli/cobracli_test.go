package cli

import (
	"strings"
	"testing"
)

// The command tree follows the conventions of the kit: no flag of the reserved names outside the kit's own
// registration, no command without an argument check, one-line short helps. Execute refuses a tree that
// breaks one, so this is what keeps every run from failing.
func TestCommandTreeFollowsKit(t *testing.T) {
	opts := &Options{}
	newRootCommand(opts, sharedRegistry())
	if err := opts.program.Check(); err != nil {
		t.Fatalf("the command tree breaks a convention of the kit:\n%v", err)
	}
}

// An unknown help topic is a syntax error with exit code 2, and --yes alone never confirms a change.
func TestUnknownHelpTopicExitsWithUsage(t *testing.T) {
	code, _, stderr := runWithInput(t, &Options{}, "", "help", "nope")
	if code != exitUsage || !strings.Contains(stderr, "unknown help topic") {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
}
