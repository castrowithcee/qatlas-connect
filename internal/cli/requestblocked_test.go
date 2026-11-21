package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
)

func TestRequestBlockedIsARequestProblemWithANextStep(t *testing.T) {
	err := &application.RequestBlockedError{Connection: "main", Tools: []string{"fake.pages.delete"}}
	var usage *UsageError
	if !errors.As(classifyUserError(err), &usage) {
		t.Fatal("request-blocked is not a usage problem, so its exit code would not be 2")
	}
	if codeFor(err) != application.CodeRequestBlocked {
		t.Fatalf("code = %s", codeFor(err))
	}
	for _, r := range []route{routeCLI, routeMCP, routeAgent} {
		step := nextStep(err, r)
		if !strings.Contains(step, "do not request") || strings.Contains(step, "config.yaml") {
			t.Errorf("next step on route %d = %q", r, step)
		}
	}
}
