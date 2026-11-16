package application

import (
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// A call resolves the service account the connection names and nothing else, and the failures of an
// impersonation keep codes of their own.
func TestImpersonationReachesTheCallAndKeepsItsCodes(t *testing.T) {
	core, _ := tokenSampleCore(t, nil)
	conn := core.config.Connections["work"]
	conn.ServiceAccount = "robot-one"
	core.config.Connections["work"] = conn
	resolved, err := core.connection("work")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.ServiceAccount != "robot-one" {
		t.Errorf("Resolved.ServiceAccount = %q, want the one of the connection", resolved.ServiceAccount)
	}
	for class, want := range map[provider.Class]string{
		provider.ClassImpersonationDenied:   string(CodeImpersonationDenied),
		provider.ClassServiceAccountUnknown: string(CodeServiceAccountUnknown),
		provider.ClassConsentExpired:        string(CodeConsentExpired),
	} {
		if got := string(providerCode(class)); got != want {
			t.Errorf("code of %s = %s, want %s", class, got, want)
		}
	}
}
