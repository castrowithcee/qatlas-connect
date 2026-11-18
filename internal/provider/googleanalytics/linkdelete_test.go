package googleanalytics

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// linkDeleteTools are the four delete tools with the request each one must send.
var linkDeleteTools = []struct {
	id, name, template, path string
}{
	{adsLinksDeleteID, "properties/200/googleAdsLinks/11", "PROP/googleAdsLinks/11", "/v1beta/properties/200/googleAdsLinks/11"},
	{firebaseLinksDeleteID, "properties/200/firebaseLinks/21", "PROP/firebaseLinks/21", "/v1beta/properties/200/firebaseLinks/21"},
	{bigQueryLinksDeleteID, "properties/200/bigQueryLinks/abc567", "PROP/bigQueryLinks/abc567", "/v1alpha/properties/200/bigQueryLinks/abc567"},
	{adsenseLinksDeleteID, "properties/200/adSenseLinks/300", "PROP/adSenseLinks/300", "/v1alpha/properties/200/adSenseLinks/300"},
}

// linkDeleteEnv adds connections that list the four delete tools, with the bindings of the stream tests.
func linkDeleteEnv(t *testing.T, fake *fakeGoogle) *testEnv {
	t.Helper()
	env := newEnv(t, fake)
	tools := []string{adsLinksDeleteID, firebaseLinksDeleteID, bigQueryLinksDeleteID, adsenseLinksDeleteID}
	for name, targets := range map[string][]string{"lfree": nil, "laccount": {"accounts/100"},
		"lproperty": {"properties/200"}, "lmixed": {"accounts/100", "properties/901"}} {
		env.cfg.Connections[name] = config.Connection{Service: "ga", Credential: "login", Targets: targets, Tools: tools}
	}
	if err := env.cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	return env
}

func TestLinkDeletesNeedConfirmationAndSendOneRequest(t *testing.T) {
	for _, tool := range linkDeleteTools {
		env := linkDeleteEnv(t, &fakeGoogle{api: always(http.StatusOK, `{}`)})
		args := `{"name":"` + tool.name + `"}`
		if _, err := env.call(tool.id, "lfree", args); err == nil {
			t.Fatalf("%s: delete without confirm was accepted", tool.id)
		}
		env.noIO(t)
		result, err := env.confirmed(tool.id, "lfree", args)
		requests, _ := env.fake.seen()
		if err != nil || len(requests) != 1 || requests[0].method != http.MethodDelete || requests[0].path != tool.path ||
			requests[0].body != "" || requests[0].rawQuery != "" ||
			!strings.Contains(result, `"deleted":true`) || !strings.Contains(result, tool.name) {
			t.Fatalf("%s: %v, %+v, %s", tool.id, err, requests, result)
		}
	}
}

func TestLinkDeletesRefuseInvalidNamesBeforeAnyIO(t *testing.T) {
	for _, tool := range linkDeleteTools {
		env := linkDeleteEnv(t, &fakeGoogle{api: always(http.StatusOK, `{}`)})
		for label, name := range map[string]string{
			"empty":         "",
			"property only": "properties/200",
			"zero property": strings.Replace(tool.name, "properties/200", "properties/0", 1),
			"wrong group":   "properties/200/keyEvents/3001",
			"extra segment": tool.name + "/x",
			"path escape":   tool.name + "/../..",
			"query":         tool.name + "?force=true",
			"other root":    strings.Replace(tool.name, "properties/200", "accounts/200", 1),
			"blank id":      tool.name + " x",
		} {
			if _, err := env.confirmed(tool.id, "lfree", `{"name":"`+name+`"}`); err == nil {
				t.Errorf("%s %s: accepted", tool.id, label)
			}
		}
		env.noIO(t)
	}
}

func TestLinkDeletesFollowTheBindingBeforeAnyIO(t *testing.T) {
	for _, tool := range linkDeleteTools {
		for _, c := range streamBindingCases {
			answers := map[string]string{
				"/v1beta/properties/200": `{"name":"properties/200","parent":"accounts/100"}`,
				"/v1beta/properties/201": `{"name":"properties/201","parent":"accounts/100"}`,
				"/v1beta/properties/901": `{"name":"properties/901","parent":"accounts/900"}`,
			}
			env := linkDeleteEnv(t, &fakeGoogle{api: func(_ int, r *http.Request) (*http.Response, error) {
				body, ok := answers[r.URL.Path]
				if !ok {
					body = `{}`
				}
				return respond(r, http.StatusOK, "application/json", body), nil
			}})
			connection := "l" + strings.TrimPrefix(c.connection, "s")
			args := `{"name":"` + strings.ReplaceAll(tool.template, "PROP", c.property) + `"}`
			result, err := env.confirmed(tool.id, connection, args)
			requests, _ := env.fake.seen()
			want := slices.Clone(c.methods)
			if i := slices.Index(want, "X"); i >= 0 {
				want[i] = "DELETE"
			}
			if (err != nil) != c.refused || !slices.Equal(methodsOf(requests), want) {
				t.Errorf("%s %s %s: %v, requests %v, want %v", tool.id, connection, c.property, err, methodsOf(requests), want)
			}
			if c.refused && (!refusedInvalid(err) || strings.Contains(err.Error(), "901") || strings.Contains(result, "901")) {
				t.Errorf("%s %s %s: refusal %v", tool.id, connection, c.property, err)
			}
			if c.refused && len(c.methods) == 0 {
				env.noIO(t)
			}
		}
	}
}

func TestLinkDeleteErrorsAreClassifiedAndNeverRepeat(t *testing.T) {
	for _, c := range []struct {
		name   string
		api    func(int, *http.Request) (*http.Response, error)
		class  provider.Class
		unsure bool
	}{
		{"403", always(http.StatusForbidden, errorObject(403, "PERMISSION_DENIED", "ACCESS_TOKEN_SCOPE_INSUFFICIENT")),
			provider.ClassPermission, false},
		{"404", always(http.StatusNotFound, errorObject(404, "NOT_FOUND", "")), provider.ClassNotFound, false},
		{"429", always(http.StatusTooManyRequests, errorObject(429, "RESOURCE_EXHAUSTED", "")), provider.ClassRateLimited, false},
		{"500", always(http.StatusInternalServerError, errorObject(500, "INTERNAL", "")), provider.ClassProviderError, true},
	} {
		for _, tool := range linkDeleteTools {
			t.Run(c.name+" "+tool.id, func(t *testing.T) {
				env := linkDeleteEnv(t, &fakeGoogle{api: c.api})
				_, err := env.confirmed(tool.id, "lfree", `{"name":"`+tool.name+`"}`)
				if err == nil || classOf(err) != c.class || leaks(err.Error()) != "" {
					t.Fatalf("error = %v", err)
				}
				if got := strings.Contains(err.Error(), "outcome of the change is unknown"); got != c.unsure {
					t.Errorf("unknown outcome reported = %t: %v", got, err)
				}
				if requests, _ := env.fake.seen(); len(requests) != 1 {
					t.Errorf("requests = %d, want exactly one", len(requests))
				}
			})
		}
	}
}

func TestLinkDelete429WithoutRetryAfterHoldsTheNextRequest(t *testing.T) {
	env := linkDeleteEnv(t, &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
		if n == 1 {
			return respond(r, http.StatusTooManyRequests, "application/json", errorObject(429, "RESOURCE_EXHAUSTED", "")), nil
		}
		return respond(r, http.StatusOK, "application/json", `{}`), nil
	}})
	args := `{"name":"properties/200/firebaseLinks/21"}`
	if _, err := env.confirmed(firebaseLinksDeleteID, "lfree", args); classOf(err) != provider.ClassRateLimited {
		t.Fatalf("error = %v", err)
	}
	if _, err := env.confirmed(firebaseLinksDeleteID, "lfree", args); err != nil {
		t.Fatal(err)
	}
	if requests, _ := env.fake.seen(); len(requests) != 2 {
		t.Errorf("requests = %d, want 2 without a retry", len(requests))
	}
	if waits := *env.waits; len(waits) == 0 || slices.Max(waits) < 900_000_000 {
		t.Errorf("waits = %v, want the next request held for about a second", waits)
	}
}

func TestLinkDeleteToolsAreRegisteredWithScopesProfilesAndSummaries(t *testing.T) {
	reg := registry(t)
	metadata, _ := reg.ProviderMetadata(Provider)
	for _, tool := range linkDeleteTools {
		if !slices.Equal(metadata.Token.Scopes[tool.id], []string{scopeEdit}) {
			t.Errorf("%s scopes = %v", tool.id, metadata.Token.Scopes[tool.id])
		}
		if slices.Contains(metadata.Profiles[0].Tools, tool.id) || !slices.Contains(metadata.Profiles[1].Tools, tool.id) {
			t.Errorf("%s has the wrong profiles", tool.id)
		}
		summary := reg.ConfirmationSummary(tool.id)
		if summary == nil {
			t.Fatalf("%s has no confirmation summary", tool.id)
		}
		if got := summary(json.RawMessage(`{"name":"` + tool.name + `"}`)); !strings.Contains(got, "Delete") ||
			!strings.Contains(got, tool.name) {
			t.Errorf("summary %q", got)
		}
		for _, d := range reg.Provider(Provider) {
			if d.ID != tool.id {
				continue
			}
			alpha := strings.HasPrefix(tool.path, alphaPrefix)
			if d.Risk != writeRisk("delete", "idempotent") || strings.Contains(d.Description, "ALPHA") != alpha ||
				!strings.Contains(d.Description, "link the property again") || !strings.Contains(d.Description, "confirmation") {
				t.Errorf("%s: risk %+v, description %q", d.ID, d.Risk, d.Description)
			}
		}
	}
}
