package googlemerchant

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const outcomeUnknown = "outcome of the change is unknown"

func openClient(t *testing.T, h *harness) *client {
	t.Helper()
	c, err := open(context.Background(), h.resolved, h.secrets, h.red)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestAChangeCarriesItsJSONBodyAndIsSentOnce(t *testing.T) {
	h := newHarness(t)
	var body, contentType string
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		raw, _ := io.ReadAll(req.Body)
		body, contentType = string(raw), req.Header.Get("Content-Type")
		return 200, `{"name":"accounts/1"}`, nil
	}
	var out account
	err := openClient(t, h).send(context.Background(), "op", accountsGetID,
		request{method: http.MethodPatch, path: accountPath("1"), body: []byte(`{"accountName":"n"}`), change: true}, &out)
	if err != nil || out.Name != "accounts/1" {
		t.Fatalf("out = %+v, err = %v", out, err)
	}
	seen := h.google.seen()
	if len(seen) != 1 || seen[0].Method != http.MethodPatch || seen[0].URL.Path != "/accounts/v1/accounts/1" {
		t.Fatalf("requests = %v", seen)
	}
	if body != `{"accountName":"n"}` || contentType != "application/json" {
		t.Errorf("body %q with content type %q", body, contentType)
	}
}

func TestAChangeIsNeverRepeated(t *testing.T) {
	for name, c := range map[string]struct {
		status  int
		body    string
		class   provider.Class
		unknown bool
	}{
		"401":      {401, `{"error":{"code":401,"status":"UNAUTHENTICATED"}}`, provider.ClassAuth, false},
		"400":      {400, `{"error":{"code":400,"status":"INVALID_ARGUMENT"}}`, provider.ClassProviderError, false},
		"429":      {429, quotaBody(reasonMinuteQuota), provider.ClassRateLimited, false},
		"500":      {500, `{}`, provider.ClassProviderError, true},
		"503":      {503, `{}`, provider.ClassUnreachable, true},
		"not json": {200, `<html>`, provider.ClassInvalidResponse, true},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.google.api = func(*http.Request) (int, string, http.Header) {
				if c.body == `<html>` {
					return c.status, c.body, http.Header{"Content-Type": {"text/html"}}
				}
				return c.status, c.body, nil
			}
			err := openClient(t, h).send(context.Background(), "op", accountsGetID,
				request{method: http.MethodPost, path: accountPath("1"), body: []byte(`{}`), change: true}, &account{})
			if classOf(err) != c.class {
				t.Fatalf("err = %v, want class %s", err, c.class)
			}
			if got := strings.Contains(err.Error(), outcomeUnknown); got != c.unknown {
				t.Errorf("message %q: unknown outcome named = %v, want %v", err, got, c.unknown)
			}
			if n := len(h.google.seen()); n != 1 {
				t.Errorf("%d requests, want exactly one", n)
			}
		})
	}
}

func TestAChangeThatFailsInTransitHasAnUnknownOutcome(t *testing.T) {
	h := newHarness(t)
	h.google.fail = errors.New("synthetic connection reset")
	err := openClient(t, h).send(context.Background(), "op", accountsGetID,
		request{method: http.MethodDelete, path: accountPath("1"), change: true}, nil)
	if classOf(err) != provider.ClassUnreachable || !strings.Contains(err.Error(), outcomeUnknown) {
		t.Fatalf("err = %v", err)
	}
	if n := len(h.google.seen()); n != 1 {
		t.Errorf("%d requests, want exactly one", n)
	}
}

func TestADeleteExpectsNoContent(t *testing.T) {
	for _, status := range []int{200, 204} {
		h := newHarness(t)
		h.google.api = func(*http.Request) (int, string, http.Header) {
			return status, ``, http.Header{"Content-Type": {"text/plain"}}
		}
		err := openClient(t, h).send(context.Background(), "op", accountsGetID,
			request{method: http.MethodDelete, path: accountPath("1"), change: true}, nil)
		if err != nil {
			t.Errorf("status %d: err = %v", status, err)
		}
	}
}

func TestAReadWithABodyIsSentAgainAfterARenewedToken(t *testing.T) {
	h := newHarness(t)
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 401, `{"error":{"code":401,"status":"UNAUTHENTICATED"}}`, nil
	}
	err := openClient(t, h).send(context.Background(), "op", accountsGetID,
		request{method: http.MethodPost, path: accountPath("1"), body: []byte(`{}`)}, &account{})
	if classOf(err) != provider.ClassAuth || strings.Contains(err.Error(), outcomeUnknown) {
		t.Fatalf("err = %v", err)
	}
	if n := len(h.google.seen()); n != 2 || h.google.grants != 2 {
		t.Errorf("%d requests and %d grants, want one repeat after one renewal", n, h.google.grants)
	}
}
