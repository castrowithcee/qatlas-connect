package googleapi

import (
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/config"
)

func TestStatusOf(t *testing.T) {
	cases := []struct {
		name, body, status, reason string
	}{
		{"status and reason", `{"error":{"status":"PERMISSION_DENIED","details":[{"@type":"type.googleapis.com/google.rpc.Help"},{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"API_KEY_INVALID"}]}}`, "PERMISSION_DENIED", "API_KEY_INVALID"},
		{"no ErrorInfo", `{"error":{"status":"NOT_FOUND","details":[{"@type":"type.googleapis.com/google.rpc.Help","reason":"x"}]}}`, "NOT_FOUND", ""},
		{"foreign body", `{"status":"OK"}`, "", ""},
		{"not json", `<html>`, "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			status, reason := StatusOf([]byte(c.body))
			if status != c.status || reason != c.reason {
				t.Fatalf("StatusOf = %q, %q; want %q, %q", status, reason, c.status, c.reason)
			}
		})
	}
}

func TestOAuthIsValidAndFixed(t *testing.T) {
	oauth := OAuth(nil)
	if err := oauth.Validate(); err != nil {
		t.Fatalf("Validate() = %v", err)
	}
	if oauth.TokenURL != "https://oauth2.googleapis.com/token" || oauth.RevokeURL != "https://oauth2.googleapis.com/revoke" ||
		oauth.AuthorizeURL != "https://accounts.google.com/o/oauth2/v2/auth" || oauth.NarrowRefresh {
		t.Errorf("OAuth() = %+v", oauth)
	}
}

func TestTokenGrantsCoverEveryProcedure(t *testing.T) {
	grants := TokenGrants(nil)
	for _, procedure := range []config.TokenProcedure{config.TokenConsent, config.TokenServiceAccount, config.TokenImpersonation} {
		if grants[procedure] == nil {
			t.Errorf("no grant for %s", procedure)
		}
	}
	if len(grants) != 3 {
		t.Errorf("grants = %d, want 3", len(grants))
	}
}
