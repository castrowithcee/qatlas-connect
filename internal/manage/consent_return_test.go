package manage

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/config"
)

// These tests cover a consent whose redirect address a caller serves itself, as a local server does.

const returnRedirect = "http://127.0.0.1:43123/back"

func (f *consentFixture) startAt(t *testing.T, holds func() bool) *Consent {
	t.Helper()
	c, err := f.svc.StartConsentAt(context.Background(), f.proof, "cred", ConsentReturn{Redirect: returnRedirect, Holds: holds})
	if err != nil {
		t.Fatalf("StartConsentAt() = %v", err)
	}
	t.Cleanup(c.Close)
	return c
}

func returnRequest(method, host, path, query string) *http.Request {
	r := httptest.NewRequest(method, "http://"+host+path+"?"+query, nil)
	r.Host = host
	return r
}

func TestStartConsentAtRefusesAnotherRedirect(t *testing.T) {
	f := newConsentFixture(t)
	holds := func() bool { return true }
	for _, redirect := range []string{
		"", "https://127.0.0.1:43123/back", "http://localhost:43123/back", "http://127.0.0.1/back",
		"http://127.0.0.1:43123", "http://127.0.0.1:43123/back?x=1", "http://127.0.0.1:43123/back#x",
		"http://user@127.0.0.1:43123/back", "http://10.0.0.1:43123/back",
	} {
		if _, err := f.svc.StartConsentAt(context.Background(), f.proof, "cred", ConsentReturn{Redirect: redirect, Holds: holds}); err == nil {
			t.Errorf("StartConsentAt(%q) = nil, want an error", redirect)
		}
	}
	if _, err := f.svc.StartConsentAt(context.Background(), f.proof, "cred", ConsentReturn{Redirect: returnRedirect}); err == nil {
		t.Error("StartConsentAt() without Holds = nil, want an error")
	}
	var consentErr *ConsentError
	if _, err := f.svc.StartConsentAt(context.Background(), AdminProof{}, "cred",
		ConsentReturn{Redirect: returnRedirect, Holds: holds}); !errors.As(err, &consentErr) || consentErr.Reason != ConsentAdminRequired {
		t.Errorf("StartConsentAt() without proof = %v, want admin-required", err)
	}
}

func TestConsentAtAnswerAndStore(t *testing.T) {
	f := newConsentFixture(t)
	c := f.startAt(t, func() bool { return true })
	if c.server != nil {
		t.Fatal("a consent at a caller's redirect address opened its own receiver")
	}
	link, _ := url.Parse(c.Link())
	if link.Query().Get("redirect_uri") != returnRedirect {
		t.Fatalf("redirect_uri = %q, want %q", link.Query().Get("redirect_uri"), returnRedirect)
	}
	good := "state=" + c.state + "&code=" + consentCode
	for name, r := range map[string]*http.Request{
		"post":        returnRequest(http.MethodPost, "127.0.0.1:43123", "/back", good),
		"other host":  returnRequest(http.MethodGet, "localhost:43123", "/back", good),
		"other port":  returnRequest(http.MethodGet, "127.0.0.1:43124", "/back", good),
		"other path":  returnRequest(http.MethodGet, "127.0.0.1:43123", "/consent", good),
		"other state": returnRequest(http.MethodGet, "127.0.0.1:43123", "/back", "state=other&code="+consentCode),
	} {
		if err := c.Answer(r); err == nil {
			t.Errorf("%s: Answer() = nil, want a refusal", name)
		}
	}
	if err := c.Answer(returnRequest(http.MethodGet, "127.0.0.1:43123", "/back", good)); err != nil {
		t.Fatalf("Answer() = %v", err)
	}
	if err := c.Answer(returnRequest(http.MethodGet, "127.0.0.1:43123", "/back", good)); !errors.Is(err, ErrConsentEnded) {
		t.Fatalf("second Answer() = %v, want ErrConsentEnded", err)
	}
	if _, err := c.Wait(context.Background()); err != nil {
		t.Fatalf("Wait() = %v", err)
	}
	if got := f.secrets.entries["cred."+config.RoleRefreshToken]; got != consentRefresh {
		t.Errorf("stored refresh token = %q, want the one of the answer", got)
	}
	if forms := f.endpoints.sent("/token"); len(forms) != 1 || forms[0].Get("redirect_uri") != returnRedirect {
		t.Errorf("exchange = %v, want one with the caller's redirect address", forms)
	}
}

// An admin proof that no longer holds ends the consent without effect: before the exchange, nothing is sent;
// after it, nothing is stored.
func TestConsentAtEndsWhenTheAdminProofEnds(t *testing.T) {
	for name, tt := range map[string]struct {
		holdFor   int32
		exchanges int
	}{
		"before the exchange": {0, 0},
		"during the exchange": {1, 1},
	} {
		t.Run(name, func(t *testing.T) {
			f := newConsentFixture(t)
			var asked atomic.Int32
			c := f.startAt(t, func() bool { return asked.Add(1) <= tt.holdFor })
			if err := c.Answer(returnRequest(http.MethodGet, "127.0.0.1:43123", "/back", "state="+c.state+"&code="+consentCode)); err != nil {
				t.Fatal(err)
			}
			_, err := c.Wait(context.Background())
			var consentErr *ConsentError
			if !errors.As(err, &consentErr) || consentErr.Reason != ConsentAdminEnded {
				t.Fatalf("Wait() = %v, want admin-ended", err)
			}
			if got := len(f.endpoints.sent("/token")); got != tt.exchanges {
				t.Errorf("exchanges = %d, want %d", got, tt.exchanges)
			}
			f.assertNothingStored(t)
		})
	}
}
