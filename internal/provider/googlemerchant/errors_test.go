package googlemerchant

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const googleErrorBody = `{"error":{"code":403,"message":"synthetic provider echo","status":"PERMISSION_DENIED",` +
	`"details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"SYNTHETIC_REASON",` +
	`"domain":"merchantapi.googleapis.com","metadata":{"method":"synthetic"}}]}}`

func quotaBody(reason string) string {
	return `{"error":{"code":429,"message":"synthetic provider echo","status":"RESOURCE_EXHAUSTED",` +
		`"details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"` + reason + `"}]}}`
}

func TestGoogleFailuresMapToClassesWithoutGoogleText(t *testing.T) {
	for name, c := range map[string]struct {
		status int
		body   string
		class  provider.Class
	}{
		"401":              {401, `{"error":{"code":401,"status":"UNAUTHENTICATED"}}`, provider.ClassAuth},
		"403":              {403, googleErrorBody, provider.ClassPermission},
		"404":              {404, `{"error":{"code":404,"status":"NOT_FOUND"}}`, provider.ClassNotFound},
		"429 minute":       {429, quotaBody(reasonMinuteQuota), provider.ClassRateLimited},
		"429 day":          {429, quotaBody(reasonDayQuota), provider.ClassRateLimited},
		"429 other":        {429, quotaBody("OTHER"), provider.ClassRateLimited},
		"503":              {503, `{}`, provider.ClassUnreachable},
		"500":              {500, `not json`, provider.ClassProviderError},
		"redirect refused": {302, ``, provider.ClassProviderError},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.google.api = func(*http.Request) (int, string, http.Header) { return c.status, c.body, nil }
			_, err := h.call(t, handleAccountsGet, `{"account":"1"}`)
			if classOf(err) != c.class {
				t.Fatalf("err = %v, want class %s", err, c.class)
			}
			for _, leaked := range []string{"synthetic provider echo", "SYNTHETIC_REASON", testAccess} {
				if strings.Contains(err.Error(), leaked) {
					t.Errorf("message %q carries %q", err, leaked)
				}
			}
			if failed, _ := err.(*provider.Error); failed == nil || (c.body != "" && c.status != 302 && failed.Original == nil) {
				t.Errorf("error %#v carries no original", err)
			}
		})
	}
}

func TestQuotaMessagesNameTheLimit(t *testing.T) {
	for reason, want := range map[string]string{reasonMinuteQuota: "per-minute", reasonDayQuota: "12:00 UTC", "": "wait and try again"} {
		body := `{}`
		if reason != "" {
			body = quotaBody(reason)
		}
		if got := googleError("op", 429, []byte(body)).Message; !strings.Contains(got, want) {
			t.Errorf("reason %q: %q lacks %q", reason, got, want)
		}
	}
}

func TestA401RenewsTheTokenOnceAndEndsWithAnAuthError(t *testing.T) {
	h := newHarness(t)
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 401, `{"error":{"code":401,"status":"UNAUTHENTICATED"}}`, nil
	}
	if _, err := h.call(t, handleAccountsGet, `{"account":"1"}`); classOf(err) != provider.ClassAuth {
		t.Fatalf("err = %v", err)
	}
	if got := len(h.google.seen()); got != 2 || h.google.grants != 2 {
		t.Errorf("%d requests and %d grants, want one repeat after one renewal", got, h.google.grants)
	}
}

func TestA403IsNeverRepeatedAndRenewsNothing(t *testing.T) {
	h := newHarness(t)
	h.google.api = func(*http.Request) (int, string, http.Header) { return 403, googleErrorBody, nil }
	if _, err := h.call(t, handleAccountsGet, `{"account":"1"}`); classOf(err) != provider.ClassPermission {
		t.Fatalf("err = %v", err)
	}
	if got := len(h.google.seen()); got != 1 || h.google.grants != 1 {
		t.Errorf("%d requests and %d grants, want one each", got, h.google.grants)
	}
}

func TestA429IsNotRepeatedAndHoldsTheNextRequestOfTheCredential(t *testing.T) {
	h := newHarness(t)
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 429, quotaBody(reasonMinuteQuota), http.Header{"Retry-After": {"30"}}
	}
	if _, err := h.call(t, handleAccountsGet, `{"account":"1"}`); classOf(err) != provider.ClassRateLimited {
		t.Fatalf("err = %v", err)
	}
	if got := len(h.google.seen()); got != 1 {
		t.Fatalf("requests = %d, want the rate-limited request not repeated", got)
	}
	h.google.api = nil
	if _, err := h.call(t, handleAccountsGet, `{"account":"1"}`); err != nil {
		t.Fatal(err)
	}
	if waits := h.spacing.recorded(); len(waits) != 1 || waits[0] < 30*time.Second || waits[0] > maxHold {
		t.Errorf("waits = %v, want the Retry-After hold", waits)
	}
}

func TestAnAnswerThatIsNotJSONAndATransportFailureAreClassifiedWithoutTheURL(t *testing.T) {
	h := newHarness(t)
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `<html>`, http.Header{"Content-Type": {"text/html"}}
	}
	if _, err := h.call(t, handleAccountsGet, `{"account":"1"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Fatalf("err = %v", err)
	}
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, `[1`, nil }
	if _, err := h.call(t, handleAccountsGet, `{"account":"1"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Fatalf("err = %v", err)
	}
	h.google.fail = http.ErrHandlerTimeout
	_, err := h.call(t, handleAccountsGet, `{"account":"1"}`)
	if err == nil || strings.Contains(err.Error(), "merchantapi.googleapis.com") {
		t.Fatalf("err = %v", err)
	}
}
