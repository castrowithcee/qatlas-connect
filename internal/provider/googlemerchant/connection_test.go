package googlemerchant

import (
	"net/http"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

func (h *harness) test(t *testing.T) provider.Class {
	t.Helper()
	class, err := TestConnection(t.Context(), h.resolved, h.secrets, h.red)
	if err != nil {
		t.Fatal(err)
	}
	return class
}

const registeredBody = `{"name":"accounts/10/developerRegistration","gcpIds":["project-one"]}`

func TestConnectionWithoutTargetsListsOneAccountAndReadsItsRegistration(t *testing.T) {
	h := newHarness(t)
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		if strings.HasSuffix(req.URL.Path, "/developerRegistration") {
			return 200, registeredBody, nil
		}
		return 200, `{"accounts":[` + accountRow("10", "Ten") + `]}`, nil
	}
	if class := h.test(t); class != provider.ClassOK {
		t.Fatalf("class = %q", class)
	}
	want := []string{"/accounts/v1/accounts?pageSize=1", "/accounts/v1/accounts/10/developerRegistration"}
	if paths := h.paths(t); strings.Join(paths, " ") != strings.Join(want, " ") {
		t.Errorf("paths = %v, want %v", paths, want)
	}
}

func TestConnectionReportsAMissingRegistrationAsPermission(t *testing.T) {
	for name, answer := range map[string]struct {
		status int
		body   string
		class  provider.Class
	}{
		"no project":      {200, `{"name":"accounts/10/developerRegistration"}`, provider.ClassPermission},
		"refused":         {403, googleErrorBody, provider.ClassPermission},
		"quota":           {429, quotaBody(reasonDayQuota), provider.ClassRateLimited},
		"token rejection": {401, `{"error":{"status":"UNAUTHENTICATED"}}`, provider.ClassAuth},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, "advanced-accounts/10")
			h.google.api = func(req *http.Request) (int, string, http.Header) {
				if strings.HasSuffix(req.URL.Path, "/developerRegistration") {
					return answer.status, answer.body, nil
				}
				return 200, accountRow("10", "Ten"), nil
			}
			if class := h.test(t); class != answer.class {
				t.Fatalf("class = %q, want %q", class, answer.class)
			}
		})
	}
}

func TestConnectionReadsOnlyTheBoundAccountOfAnAccountTarget(t *testing.T) {
	h := newHarness(t, "accounts/30", "accounts/20")
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, accountRow("20", "Twenty"), nil }
	if class := h.test(t); class != provider.ClassOK {
		t.Fatalf("class = %q", class)
	}
	if paths := h.paths(t); len(paths) != 1 || paths[0] != "/accounts/v1/accounts/20" {
		t.Errorf("paths = %v, want one read of the first bound account and no registration read", paths)
	}
}

func TestConnectionPrefersTheAdvancedAccountTarget(t *testing.T) {
	h := newHarness(t, "accounts/5", "advanced-accounts/10")
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		if strings.HasSuffix(req.URL.Path, "/developerRegistration") {
			return 200, registeredBody, nil
		}
		return 200, accountRow("10", "Ten"), nil
	}
	if class := h.test(t); class != provider.ClassOK {
		t.Fatalf("class = %q", class)
	}
	want := "/accounts/v1/accounts/10 /accounts/v1/accounts/10/developerRegistration"
	if paths := strings.Join(h.paths(t), " "); paths != want {
		t.Errorf("paths = %q, want %q", paths, want)
	}
}

func TestConnectionWithoutAnyAccountIsNotFound(t *testing.T) {
	h := newHarness(t)
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, `{}`, nil }
	if class := h.test(t); class != provider.ClassNotFound {
		t.Fatalf("class = %q", class)
	}
}

func TestConnectionMapsGoogleFailuresOfTheAccountRead(t *testing.T) {
	for status, class := range map[int]provider.Class{401: provider.ClassAuth, 403: provider.ClassPermission,
		429: provider.ClassRateLimited} {
		h := newHarness(t)
		h.google.api = func(*http.Request) (int, string, http.Header) { return status, `{}`, nil }
		if got := h.test(t); got != class {
			t.Errorf("status %d: class = %q, want %q", status, got, class)
		}
	}
}

func TestConnectionWithoutConsentEndsBeforeAnyRequest(t *testing.T) {
	h := newHarness(t)
	h.resolved.Secrets = config.Credential{Provider: Provider, Type: config.CredentialTypeEnv,
		Values: map[string]string{config.RoleClientID: "MERCHANT_TEST_CLIENT"}}
	if class := h.test(t); class != provider.ClassConsentRequired {
		t.Fatalf("class = %q", class)
	}
	if len(h.google.seen()) != 0 || h.google.grants != 0 {
		t.Errorf("%d requests, %d grants; want none", len(h.google.seen()), h.google.grants)
	}
}

func TestConnectionWithAnExpiredConsentIsNamedAsSuch(t *testing.T) {
	h := newHarness(t)
	h.google.token = func() (int, string) { return 400, `{"error":"invalid_grant"}` }
	if class := h.test(t); class != provider.ClassConsentExpired {
		t.Fatalf("class = %q", class)
	}
	if len(h.google.seen()) != 0 {
		t.Errorf("requests = %d, want none", len(h.google.seen()))
	}
}
