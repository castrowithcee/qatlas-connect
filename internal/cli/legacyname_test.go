package cli

import (
	"strings"
	"testing"
)

func TestCalledByLegacyName(t *testing.T) {
	for program, want := range map[string]bool{
		"qatlas": true, "/usr/local/bin/qatlas": true, "./QATLAS.EXE": true, "qatlas.exe": true, "qconnect": false, "/x/qconnect": false, "qatlas-connect": false, "qatlasx": false,
	} {
		if got := calledByLegacyName(program); got != want {
			t.Errorf("calledByLegacyName(%q) = %v, want %v", program, got, want)
		}
	}
}

func TestLegacyNoticeSilentForHiddenAndCurrentName(t *testing.T) {
	for _, tt := range []struct {
		program string
		args    []string
		want    bool
	}{
		{"/bin/qatlas", []string{"providers"}, true},
		{"/bin/qatlas", []string{"mcp"}, true},
		{"/bin/qatlas", []string{"vault", "serve"}, false},
		{"/bin/qconnect", []string{"providers"}, false},
	} {
		got := legacyNotice(tt.program, tt.args, defaultRegistry())
		if has := strings.Contains(got, "now called qconnect"); has != tt.want {
			t.Errorf("legacyNotice(%q, %v) = %q, want a note: %v", tt.program, tt.args, got, tt.want)
		}
	}
}

// The notice reaches stderr once, before the command, as a line of the program.
func TestLegacyNoticeIsWrittenBeforeTheCommand(t *testing.T) {
	opts := &Options{notice: legacyNotice("/bin/qatlas", []string{"--version"}, defaultRegistry())}
	var stdout, stderr strings.Builder
	if code := run(newRootCommand(opts, sharedRegistry()), opts, []string{"--version"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("exit %d", code)
	}
	want := "qconnect: note: this program is now called qconnect; the name qatlas still works until the next major release\n"
	if stderr.String() != want || strings.Contains(stdout.String(), "note") {
		t.Errorf("stderr %q, stdout %q", stderr.String(), stdout.String())
	}
}
