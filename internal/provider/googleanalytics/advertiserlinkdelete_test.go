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

var advertiserDeleteTools = []struct {
	id, name, path, method string
}{
	{searchAdsLinksDeleteID, "properties/200/searchAds360Links/300", "/v1alpha/properties/200/searchAds360Links/300", "DELETE"},
	{dv360LinksDeleteID, "properties/200/displayVideo360AdvertiserLinks/4000",
		"/v1alpha/properties/200/displayVideo360AdvertiserLinks/4000", "DELETE"},
	{dv360ProposalsCancelID, "properties/200/displayVideo360AdvertiserLinkProposals/5000",
		"/v1alpha/properties/200/displayVideo360AdvertiserLinkProposals/5000:cancel", "POST"},
	{dv360ProposalsDeleteID, "properties/200/displayVideo360AdvertiserLinkProposals/5000",
		"/v1alpha/properties/200/displayVideo360AdvertiserLinkProposals/5000", "DELETE"},
	{rollupLinksDeleteID, "properties/200/rollupPropertySourceLinks/4000",
		"/v1alpha/properties/200/rollupPropertySourceLinks/4000", "DELETE"},
}

// advertiserDeleteEnv adds connections that list the five tools, with the bindings of the stream tests.
func advertiserDeleteEnv(t *testing.T, fake *fakeGoogle) *testEnv {
	t.Helper()
	env := newEnv(t, fake)
	var tools []string
	for _, tool := range advertiserDeleteTools {
		tools = append(tools, tool.id)
	}
	for name, targets := range map[string][]string{"dfree": nil, "daccount": {"accounts/100"},
		"dproperty": {"properties/200"}, "dmixed": {"accounts/100", "properties/901"}} {
		env.cfg.Connections[name] = config.Connection{Service: "ga", Credential: "login", Targets: targets, Tools: tools}
	}
	if err := env.cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	return env
}

// answerFor is the proposal Google returns for a cancel and the empty object for a delete.
func answerFor(method, path string) string {
	if method != http.MethodPost {
		return `{}`
	}
	name := strings.TrimSuffix(strings.TrimPrefix(path, "/v1alpha/"), ":cancel")
	return `{"name":"` + name + `","advertiserId":"7001","linkProposalStatusDetails":{"linkProposalState":"WITHDRAWN"}}`
}

func TestAdvertiserDeletesNeedConfirmationAndSendOneRequest(t *testing.T) {
	for _, tool := range advertiserDeleteTools {
		env := advertiserDeleteEnv(t, &fakeGoogle{api: func(_ int, r *http.Request) (*http.Response, error) {
			return respond(r, http.StatusOK, "application/json", answerFor(r.Method, r.URL.Path)), nil
		}})
		args := `{"name":"` + tool.name + `"}`
		if _, err := env.call(tool.id, "dfree", args); err == nil {
			t.Fatalf("%s: accepted without confirm", tool.id)
		}
		env.noIO(t)
		result, err := env.confirmed(tool.id, "dfree", args)
		requests, _ := env.fake.seen()
		if err != nil || len(requests) != 1 || requests[0].method != tool.method || requests[0].path != tool.path {
			t.Fatalf("%s: %v, %+v", tool.id, err, requests)
		}
		if tool.id == dv360ProposalsCancelID {
			var got dv360Proposal
			if json.Unmarshal([]byte(result), &got) != nil || string(got.Name) != tool.name || got.AdvertiserID != "7001" ||
				got.LinkProposalStatusDetails == nil || got.LinkProposalStatusDetails.LinkProposalState != "WITHDRAWN" ||
				requests[0].body != "{}" {
				t.Errorf("cancel: %s, body %q", result, requests[0].body)
			}
			continue
		}
		if requests[0].body != "" || !strings.Contains(result, `"deleted":true`) || !strings.Contains(result, tool.name) {
			t.Errorf("%s: %s, body %q", tool.id, result, requests[0].body)
		}
	}
}

func TestDV360CancelBoundsTheAnswerAndDistrustsAnotherProposal(t *testing.T) {
	name := "properties/200/displayVideo360AdvertiserLinkProposals/5000"
	env := advertiserDeleteEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"name":"`+name+`","advertiserId":"x y",`+
		`"advertiserDisplayName":"`+strings.Repeat("n", 400)+`","extra":1}`)})
	result, err := env.confirmed(dv360ProposalsCancelID, "dfree", `{"name":"`+name+`"}`)
	var got dv360Proposal
	if err != nil || json.Unmarshal([]byte(result), &got) != nil || got.AdvertiserID != "" ||
		len(string(got.AdvertiserDisplayName)) > 300+len("…") || strings.Contains(result, "extra") {
		t.Fatalf("%v, %s", err, result)
	}
	env = advertiserDeleteEnv(t, &fakeGoogle{api: always(http.StatusOK,
		`{"name":"properties/999/displayVideo360AdvertiserLinkProposals/1"}`)})
	_, err = env.confirmed(dv360ProposalsCancelID, "dfree", `{"name":"`+name+`"}`)
	if classOf(err) != provider.ClassProviderError || !strings.Contains(err.Error(), "unknown") {
		t.Errorf("another proposal: %v", err)
	}
}

func TestDV360ProposalDeleteOfAnOpenProposalIsRefusedByGoogleWithoutRetry(t *testing.T) {
	env := advertiserDeleteEnv(t, &fakeGoogle{api: always(http.StatusBadRequest,
		errorObject(400, "FAILED_PRECONDITION", "PROPOSAL_NOT_CANCELLED"))})
	_, err := env.confirmed(dv360ProposalsDeleteID, "dfree",
		`{"name":"properties/200/displayVideo360AdvertiserLinkProposals/5000"}`)
	if err == nil || classOf(err) != provider.ClassProviderError || leaks(err.Error()) != "" ||
		strings.Contains(err.Error(), "outcome of the change is unknown") {
		t.Errorf("error = %v (class %s)", err, classOf(err))
	}
	if requests, _ := env.fake.seen(); len(requests) != 1 {
		t.Errorf("requests = %d, want exactly one", len(requests))
	}
}

func TestAdvertiserDeletesFollowTheBindingBeforeAnyIO(t *testing.T) {
	for _, tool := range advertiserDeleteTools {
		for _, c := range streamBindingCases {
			answers := map[string]string{
				"/v1beta/properties/200": `{"name":"properties/200","parent":"accounts/100"}`,
				"/v1beta/properties/201": `{"name":"properties/201","parent":"accounts/100"}`,
				"/v1beta/properties/901": `{"name":"properties/901","parent":"accounts/900"}`,
			}
			env := advertiserDeleteEnv(t, &fakeGoogle{api: func(_ int, r *http.Request) (*http.Response, error) {
				body, ok := answers[r.URL.Path]
				if !ok {
					body = answerFor(r.Method, r.URL.Path)
				}
				return respond(r, http.StatusOK, "application/json", body), nil
			}})
			connection := "d" + strings.TrimPrefix(c.connection, "s")
			name := strings.Replace(tool.name, "properties/200", c.property, 1)
			result, err := env.confirmed(tool.id, connection, `{"name":"`+name+`"}`)
			requests, _ := env.fake.seen()
			want := slices.Clone(c.methods)
			if i := slices.Index(want, "X"); i >= 0 {
				want[i] = tool.method
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

func TestAdvertiserDeletesRefuseOtherNamesBeforeAnyIO(t *testing.T) {
	for _, tool := range advertiserDeleteTools {
		for _, name := range []string{"", "properties/200", "properties/x/" + tool.name[strings.Index(tool.name[11:], "/")+12:],
			"properties/200/adSenseLinks/1", tool.name + "/x", tool.name + "?a=b", "../" + tool.name} {
			env := advertiserDeleteEnv(t, &fakeGoogle{api: always(http.StatusOK, `{}`)})
			if _, err := env.confirmed(tool.id, "dfree", `{"name":"`+name+`"}`); err == nil {
				t.Errorf("%s accepted %q", tool.id, name)
			}
			env.noIO(t)
		}
	}
}

func TestAdvertiserDeleteErrorsAreClassifiedAndChangesNeverRepeat(t *testing.T) {
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
		for _, tool := range advertiserDeleteTools {
			t.Run(c.name+" "+tool.id, func(t *testing.T) {
				env := advertiserDeleteEnv(t, &fakeGoogle{api: c.api})
				_, err := env.confirmed(tool.id, "dfree", `{"name":"`+tool.name+`"}`)
				if classOf(err) != c.class || leaks(err.Error()) != "" {
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

func TestAdvertiserDelete429WithoutRetryAfterHoldsTheNextRequest(t *testing.T) {
	env := advertiserDeleteEnv(t, &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
		if n == 1 {
			return respond(r, http.StatusTooManyRequests, "application/json", errorObject(429, "RESOURCE_EXHAUSTED", "")), nil
		}
		return respond(r, http.StatusOK, "application/json", `{}`), nil
	}})
	args := `{"name":"properties/200/rollupPropertySourceLinks/4000"}`
	if _, err := env.confirmed(rollupLinksDeleteID, "dfree", args); classOf(err) != provider.ClassRateLimited {
		t.Fatalf("error = %v", err)
	}
	if _, err := env.confirmed(rollupLinksDeleteID, "dfree", args); err != nil {
		t.Fatal(err)
	}
	if requests, _ := env.fake.seen(); len(requests) != 2 {
		t.Errorf("requests = %d, want 2 without a retry", len(requests))
	}
	if waits := *env.waits; len(waits) == 0 || slices.Max(waits) < 900_000_000 {
		t.Errorf("waits = %v, want the next request held for about a second", waits)
	}
}

func TestAdvertiserDeleteToolsAreRegisteredWithScopesProfilesAndSummaries(t *testing.T) {
	reg := registry(t)
	metadata, _ := reg.ProviderMetadata(Provider)
	byID := map[string]bool{}
	for _, d := range reg.Provider(Provider) {
		byID[d.ID] = true
	}
	for _, tool := range advertiserDeleteTools {
		if !byID[tool.id] || !slices.Equal(metadata.Token.Scopes[tool.id], []string{scopeEdit}) ||
			effectOf(tool.id) != config.EffectDelete {
			t.Errorf("%s: registration, scope or effect is wrong", tool.id)
		}
		for _, d := range reg.Provider(Provider) {
			if d.ID == tool.id && (d.Risk.Effect != "delete" || d.Risk.Confirmation != "required" || !d.Risk.OpenWorld ||
				!strings.HasSuffix(d.Description, alphaNote) || !strings.Contains(d.Description, "confirmation")) {
				t.Errorf("%s risk or description = %+v", tool.id, d.Risk)
			}
		}
		for _, p := range metadata.Profiles {
			if slices.Contains(p.Tools, tool.id) != (p.ID == "write") {
				t.Errorf("%s has the wrong profile %s", tool.id, p.ID)
			}
		}
		summary := reg.ConfirmationSummary(tool.id)
		if summary == nil {
			t.Fatalf("%s has no confirmation summary", tool.id)
		}
		if got := summary(json.RawMessage(`{"name":"` + tool.name + `"}`)); !strings.Contains(got, tool.name) {
			t.Errorf("summary %q lacks the name", got)
		}
	}
}
