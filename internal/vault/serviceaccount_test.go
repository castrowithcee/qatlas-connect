package vault

import (
	"slices"
	"testing"
	"time"
)

// A connection that impersonates no service account keeps the fingerprint it had before the field existed;
// naming one changes it, and naming another changes it again.
func TestFingerprintCoversTheServiceAccount(t *testing.T) {
	const before = "4e83b7a5c7201175c9e37bccbfb39049023b62d0e8d15ea98e0e2205ca0e928a"
	if got := fingerprintWith(legacyFingerprintLabel, wikiScope(), "id-1"); got != before {
		t.Fatalf("Fingerprint() = %s, want the fingerprint from before service accounts existed", got)
	}
	first, second := wikiScope(), wikiScope()
	first.ServiceAccount, second.ServiceAccount = "robot-one", "robot-two"
	if fingerprintWith(legacyFingerprintLabel, first, "id-1") == before ||
		Fingerprint(first, "id-1") == Fingerprint(second, "id-1") {
		t.Fatal("the service account is not part of the fingerprint")
	}

	// The approval binds the service account: a scope that names another one, or one where the approval named
	// none, reads nothing.
	b := Bindings{IDs: map[string]string{"wiki-reader": "id-1"}, Approvals: map[string]string{"wiki": Fingerprint(first, "id-1")}}
	if !b.Allows(first, "wiki-reader") {
		t.Fatal("Allows() refused the approved service account")
	}
	if b.Allows(second, "wiki-reader") {
		t.Error("Allows() accepted another service account than the approved one")
	}
	b.Approvals["wiki"] = Fingerprint(wikiScope(), "id-1")
	if b.Allows(first, "wiki-reader") || !b.Allows(wikiScope(), "wiki-reader") {
		t.Error("an approval without a service account must cover exactly the scope without one")
	}
}

// An agent token covers a change only for the very service account of its model.
func TestTokenCoversOnlyTheServiceAccountOfItsModel(t *testing.T) {
	for name, tc := range map[string]struct {
		model, change string
		covered       bool
	}{
		"same":    {model: "robot-one", change: "robot-one", covered: true},
		"other":   {model: "robot-one", change: "robot-two"},
		"added":   {model: "", change: "robot-one"},
		"dropped": {model: "robot-one", change: ""},
	} {
		v := tokenVault(t)
		vorbild := vorbildScope(t.TempDir())
		vorbild.Paths, vorbild.ServiceAccount = nil, tc.model
		approveScopes(t, v, vorbild)
		token := createToken(t, v, "agent", "vorbild")

		next := vorbild
		next.Connection, next.Effects, next.ServiceAccount = "child", []string{"read"}, tc.change
		result, err := v.ApproveWithToken(token.Value, []Scope{vorbild, next}, nil, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Changes) != 1 || result.Changes[0].Approved != tc.covered ||
			slices.Contains(result.Changes[0].Gaps, GapServiceAccount) == tc.covered {
			t.Errorf("%s: changes = %+v, covered want %v", name, result.Changes, tc.covered)
		}
	}
}
