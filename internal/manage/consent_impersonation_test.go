package manage

import (
	"context"
	"errors"
	"net/url"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// withImpersonation adds the keyring credential "imp" with a client ID, used only by the connection "robot",
// which impersonates a service account, and returns the fixture's service with consent endpoints that offer
// impersonation, or, with offered false, endpoints that do not.
func withImpersonation(t *testing.T, f *consentFixture, offered bool) *Service {
	t.Helper()
	cfg, err := f.svc.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Credentials["imp"] = config.Credential{Provider: "sample", Type: config.CredentialTypeKeyring}
	cfg.Connections["robot"] = config.Connection{Service: "svc", Credential: "imp",
		Effects: []config.Effect{config.EffectRead, config.EffectUpdate}, ServiceAccount: "robot-one"}
	if err := f.svc.store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	f.secrets.entries["imp."+config.RoleClientID] = consentClientID
	oauth := provider.OAuth{
		AuthorizeURL: "https://login.sample.invalid/authorize", TokenURL: "https://login.sample.invalid/token",
		RevokeURL: "https://login.sample.invalid/revoke", Transport: f.endpoints,
	}
	if offered {
		oauth.ImpersonateURL = "https://iam.sample.invalid/v1/accounts/" + provider.ServiceAccountSegment + ":mint"
		oauth.ImpersonateScope = "sample.impersonate"
	}
	return f.svc.WithConsentSource(func(id string) (provider.OAuth, bool) { return oauth, id == "sample" })
}

// The consent of a credential whose connection impersonates asks for the scope of the impersonation request
// alone, not for the scopes of the connection, which go to that request. Next to a connection that uses the
// consent directly it adds that one scope to theirs.
func TestConsentOfAnImpersonationAsksOnlyForItsScope(t *testing.T) {
	f := newConsentFixture(t)
	svc := withImpersonation(t, f, true)
	scopeOf := func(credential string) string {
		t.Helper()
		c, err := svc.StartConsent(context.Background(), f.proof, credential)
		if err != nil {
			t.Fatalf("StartConsent(%s) = %v", credential, err)
		}
		defer c.Close()
		link, err := url.Parse(c.Link())
		if err != nil {
			t.Fatal(err)
		}
		return link.Query().Get("scope")
	}
	if got := scopeOf("imp"); got != "sample.impersonate" {
		t.Errorf("scope of the impersonating credential = %q, want the impersonation scope alone", got)
	}

	cfg, err := f.svc.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	robot := cfg.Connections["robot"]
	robot.Credential = "cred"
	cfg.Connections["robot"] = robot
	if err := f.svc.store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	if got := scopeOf("cred"); got != "items.read items.write sample.impersonate" {
		t.Errorf("scope of a shared credential = %q, want the scopes of the direct connections and the impersonation scope", got)
	}
}

// A connection that impersonates, while its provider offers no impersonation, rules the consent out before
// any link.
func TestConsentRefusesAnImpersonationTheProviderDoesNotOffer(t *testing.T) {
	f := newConsentFixture(t)
	svc := withImpersonation(t, f, false)
	c, err := svc.StartConsent(context.Background(), f.proof, "imp")
	var consentErr *ConsentError
	if c != nil || !errors.As(err, &consentErr) || consentErr.Reason != ConsentNoImpersonation || !consentErr.Precondition() {
		t.Fatalf("StartConsent() = %v, %v, want the precondition %s", c, err, ConsentNoImpersonation)
	}
}
