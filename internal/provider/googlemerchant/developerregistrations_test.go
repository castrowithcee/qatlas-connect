package googlemerchant

import (
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

func TestDeveloperRegistrationToolsAreDeclaredWithTheirRisk(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	d, handler, ok := reg.Lookup(developerRegistrationsGetAccountID)
	if !ok || handler == nil || d.Version != 1 || d.Risk != readRisk || d.RequiresToolAllowList {
		t.Fatalf("get-account = %+v", d)
	}
	if !slices.Contains(metadata.Profiles[0].Tools, developerRegistrationsGetAccountID) {
		t.Error("the read profile lacks get-account-for-gcp-registration")
	}
	d, handler, ok = reg.Lookup(developerRegistrationsUnregisterID)
	if !ok || handler == nil || d.Version != 1 || !d.RequiresToolAllowList || d.Risk.Effect != capability.EffectDelete ||
		d.Risk.Confirmation != capability.ConfirmationRequired || !d.Risk.OpenWorld {
		t.Fatalf("unregister = %+v", d)
	}
	for _, want := range []string{"at most one day", "loses API access", "this connection included", "outside Qatlas",
		"registerGcp is not offered", "never repeats", "delay", "tools list", "Admin"} {
		if !strings.Contains(d.Description, want) {
			t.Errorf("unregister description lacks %q", want)
		}
	}
	for _, profile := range metadata.Profiles {
		if slices.Contains(profile.Tools, developerRegistrationsUnregisterID) {
			t.Errorf("profile %s lists unregister-gcp", profile.ID)
		}
	}
	if _, _, found := reg.Lookup(Provider + ".developer-registrations.register-gcp"); found {
		t.Error("register-gcp is registered")
	}
	got := unregisterGcpSummary(json.RawMessage(`{"account":"10"}`))
	for _, want := range []string{"account 10", "at most one day", "this connection included", "outside Qatlas"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary %q lacks %q", got, want)
		}
	}
}

func TestUnregisterGcpNeedsTheAdvancedTargetBeforeSecretAndRequest(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	if ok, _ := metadata.Target.Reach(developerRegistrationsUnregisterID, []string{"accounts/2"}); ok {
		t.Error("an account target reaches unregister-gcp")
	}
	if ok, _ := metadata.Target.Reach(developerRegistrationsGetAccountID, []string{"accounts/2"}); !ok {
		t.Error("an account target does not reach get-account-for-gcp-registration")
	}
	for _, c := range []struct {
		targets []string
		args    string
	}{
		{[]string{"accounts/10"}, `{"account":"10"}`},
		{[]string{"advanced-accounts/10"}, `{"account":"11"}`},
		{[]string{"advanced-accounts/10"}, `{}`},
		{nil, `{}`},
		{nil, `{"account":"10/x"}`},
		{nil, `{"account":"010"}`},
	} {
		h := newHarness(t, c.targets...)
		if _, err := h.call(t, handleUnregisterGcp, c.args); !isInvalidRequest(err) {
			t.Errorf("targets %v, args %s: err = %v", c.targets, c.args, err)
		}
		h.untouched(t)
	}
}

func TestUnregisterGcpSendsOneFixedEmptyRequestAndReportsAcceptance(t *testing.T) {
	for _, targets := range [][]string{nil, {"advanced-accounts/10"}} {
		{
			h := newHarness(t, targets...)
			h.google.api = func(*http.Request) (int, string, http.Header) { return 200, `{"unknown":"never held"}`, nil }
			out, err := h.call(t, handleUnregisterGcp, `{"account":"10"}`)
			if err != nil || out["accepted"] != true || out["name"] != "accounts/10/developerRegistration" || len(out) != 2 {
				t.Fatalf("out = %v, err = %v", out, err)
			}
			if got := serviceRequests(t, h); !slices.Equal(got, []string{"POST /accounts/v1/accounts/10/developerRegistration:unregisterGcp"}) {
				t.Errorf("requests = %v", got)
			}
			if got := h.api(t); len(got) != 1 || got[0].body != "" {
				t.Errorf("requests = %v, want one without a body", got)
			}
		}
	}
}

func TestUnregisterGcpIsSentOnceAndAnUnclearOutcomeIsReported(t *testing.T) {
	for status, want := range map[int]provider.Class{
		401: provider.ClassAuth, 403: provider.ClassPermission, 404: provider.ClassNotFound,
		429: provider.ClassRateLimited, 500: provider.ClassProviderError, 503: provider.ClassUnreachable,
	} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			h := newHarness(t)
			h.google.api = func(*http.Request) (int, string, http.Header) {
				return status, `{"error":{"code":1,"status":"X","message":"untrusted"}}`, nil
			}
			_, err := h.call(t, handleUnregisterGcp, `{"account":"10"}`)
			if classOf(err) != want {
				t.Fatalf("class = %q, want %q (%v)", classOf(err), want, err)
			}
			if unknown := strings.Contains(err.Error(), "outcome of the change is unknown"); unknown != (status >= 500) {
				t.Errorf("status %d: unknown outcome reported = %t", status, unknown)
			}
			if n := len(h.google.seen()); n != 1 {
				t.Errorf("%d requests, want exactly one", n)
			}
		})
	}
	h := newHarness(t)
	h.google.fail = io.ErrUnexpectedEOF
	_, err := h.call(t, handleUnregisterGcp, `{"account":"10"}`)
	if err == nil || !strings.Contains(err.Error(), "outcome of the change is unknown") || len(h.google.seen()) != 1 {
		t.Errorf("err = %v, %d requests", err, len(h.google.seen()))
	}
}

func TestGetAccountForGcpRegistrationIsFailClosedOnABoundConnection(t *testing.T) {
	const registered = "/accounts/v1/accounts:getAccountForGcpRegistration"
	subaccounts := func(req *http.Request, answer string) (int, string, http.Header) {
		if req.URL.Path == registered {
			return 200, answer, nil
		}
		return 200, `{"accounts":[` + accountRow("30", "Sub") + `]}`, nil
	}
	for _, c := range []struct {
		name    string
		targets []string
		answer  string
		ok      bool
		calls   int
	}{
		{"unbound", nil, `{"name":"accounts/99"}`, true, 1},
		{"unbound unknown form", nil, `{"name":"anything"}`, true, 1},
		{"advanced target", []string{"advanced-accounts/10"}, `{"name":"accounts/10"}`, true, 1},
		{"account target", []string{"accounts/20"}, `{"name":"accounts/20"}`, true, 1},
		{"sub-account of advanced target", []string{"advanced-accounts/10"}, `{"name":"accounts/30"}`, true, 2},
		{"outside account target", []string{"accounts/20"}, `{"name":"accounts/99"}`, false, 1},
		{"parent of account target", []string{"accounts/30"}, `{"name":"accounts/10"}`, false, 1},
		{"outside advanced target", []string{"advanced-accounts/10"}, `{"name":"accounts/99"}`, false, 2},
		{"unknown form", []string{"accounts/20"}, `{"name":"accounts/20/x"}`, false, 1},
		{"no name", []string{"accounts/20"}, `{}`, false, 1},
		{"plain digits", []string{"accounts/20"}, `{"name":"20"}`, true, 1},
		{"plain digits of a sub-account", []string{"advanced-accounts/10"}, `{"name":"30"}`, true, 2},
		{"outside plain digits", []string{"accounts/20"}, `{"name":"99"}`, false, 1},
		{"plain digits with leading zero", []string{"accounts/20"}, `{"name":"020"}`, false, 1},
		{"double prefix", []string{"accounts/20"}, `{"name":"accounts/accounts/20"}`, false, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t, c.targets...)
			h.google.api = func(req *http.Request) (int, string, http.Header) { return subaccounts(req, c.answer) }
			out, err := h.call(t, handleGetAccountForGcpRegistration, `{}`)
			if c.ok != (err == nil) {
				t.Fatalf("out = %v, err = %v", out, err)
			}
			if err == nil {
				var want struct{ Name string }
				_ = json.Unmarshal([]byte(c.answer), &want)
				if out["name"] != want.Name {
					t.Errorf("out = %v", out)
				}
			} else if strings.Contains(err.Error(), "99") || strings.Contains(err.Error(), "accounts/") {
				t.Errorf("error names the account: %v", err)
			}
			if strings.HasPrefix(c.name, "outside") || strings.HasPrefix(c.name, "parent") {
				if !isInvalidRequest(err) || !strings.Contains(err.Error(), outsideTargets) {
					t.Errorf("err = %v, want the outside-targets refusal", err)
				}
			}
			reqs := serviceRequests(t, h)
			if len(reqs) != c.calls || reqs[0] != "GET "+registered {
				t.Errorf("requests = %v", reqs)
			}
		})
	}
}
