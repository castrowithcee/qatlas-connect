package googlesearchconsole

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-kit/ratelimit"
)

// change invokes a changing tool with the confirmation of the request.
func (e *environment) change(t *testing.T, operation, connection, arguments string) (map[string]any, error) {
	t.Helper()
	response, err := e.core.Invoke(context.Background(), application.InvokeRequest{
		Operation: operation, Connection: connection, Arguments: json.RawMessage(arguments), Confirmed: true})
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(response.Result, &out); err != nil {
		t.Fatalf("result %s: %v", response.Result, err)
	}
	return out, nil
}

// emptyAnswer answers with the given status, no body, and the given content type (possibly none).
func empty(status int, contentType string) func(*http.Request, int) (*http.Response, error) {
	return func(r *http.Request, _ int) (*http.Response, error) {
		return respond(r, status, contentType, ""), nil
	}
}

var changes = []struct {
	id, method, args, path string
}{
	{sitesAddID, http.MethodPut, `{"site_url":"sc-domain:example.com"}`, "/webmasters/v3/sites/sc-domain%3Aexample.com"},
	{sitesDeleteID, http.MethodDelete, `{"site_url":"https://www.example.com/"}`,
		"/webmasters/v3/sites/https%3A%2F%2Fwww.example.com%2F"},
	{sitemapsSubmitID, http.MethodPut, `{"site_url":"https://www.example.com/","feedpath":"` + sitemapURL + `"}`,
		"/webmasters/v3/sites/https%3A%2F%2Fwww.example.com%2F/sitemaps/https%3A%2F%2Fwww.example.com%2Fsitemap.xml"},
	{sitemapsDeleteID, http.MethodDelete, `{"site_url":"sc-domain:example.com","feedpath":"https://blog.example.com/s.xml?a=1"}`,
		"/webmasters/v3/sites/sc-domain%3Aexample.com/sitemaps/https%3A%2F%2Fblog.example.com%2Fs.xml%3Fa%3D1"},
}

func TestChangesNeedConfirmationAndSendNothingWithout(t *testing.T) {
	env := newEnvironment(t, nil)
	for _, c := range changes {
		_, err := env.core.Invoke(context.Background(), application.InvokeRequest{
			Operation: c.id, Connection: "edit", Arguments: json.RawMessage(c.args)})
		var needed *application.ConfirmationRequiredError
		if !errors.As(err, &needed) {
			t.Errorf("%s without confirm: error = %v, want confirmation required", c.id, err)
		}
	}
	if calls, grants := env.fake.calls(); len(calls) != 0 || grants != 0 {
		t.Errorf("%d requests and %d grants, want none", len(calls), grants)
	}
}

func TestChangesSendTheMethodAndPathAndAcceptEmptyAnswers(t *testing.T) {
	for name, answer := range map[string]func(*http.Request, int) (*http.Response, error){
		"200 empty object":       reply(http.StatusOK, `{}`),
		"200 empty no type":      empty(http.StatusOK, ""),
		"204 no content":         empty(http.StatusNoContent, ""),
		"200 empty with type":    empty(http.StatusOK, "application/json; charset=UTF-8"),
		"200 text type, no body": empty(http.StatusOK, "text/html"),
	} {
		t.Run(name, func(t *testing.T) {
			for _, c := range changes {
				env := newEnvironment(t, nil)
				env.fake.answer = answer
				out, err := env.change(t, c.id, "edit", c.args)
				if err != nil || len(out) != 0 {
					t.Fatalf("%s = %v, %v, want an empty result", c.id, out, err)
				}
				calls, grants := env.fake.calls()
				if len(calls) != 1 || grants != 1 {
					t.Fatalf("%s: %d requests, %d grants", c.id, len(calls), grants)
				}
				got := calls[0]
				if got.method != c.method || got.escapedPath != c.path || got.host != "searchconsole.googleapis.com" ||
					got.rawQuery != "" || got.body != "" || got.contentType != "" ||
					got.authorization != "Bearer "+accessPrefix+"1" {
					t.Errorf("%s: request = %+v", c.id, got)
				}
			}
		})
	}
}

func TestChangeRefusesAnAnswerThatIsNoEmptyObject(t *testing.T) {
	for name, answer := range map[string]func(*http.Request, int) (*http.Response, error){
		"html": func(r *http.Request, _ int) (*http.Response, error) {
			return respond(r, 200, "text/html", "<p>x</p>"), nil
		},
		"json array":  reply(http.StatusOK, `[1]`),
		"broken json": reply(http.StatusOK, `{"a":`),
	} {
		env := newEnvironment(t, nil)
		env.fake.answer = answer
		_, err := env.change(t, sitesAddID, "edit", changes[0].args)
		if classOf(err) != provider.ClassInvalidResponse {
			t.Errorf("%s: error = %v, want invalid-provider-response", name, err)
		}
	}
}

func TestChangesAskTheServiceAccountTokenForTheEditScope(t *testing.T) {
	private, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		t.Fatal(err)
	}
	keyFile, _ := json.Marshal(map[string]string{
		"type": "service_account", "client_email": "robot-sample-account", "private_key_id": "synthetic-kid",
		"private_key": string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})),
		"token_uri":   tokenEndpoint,
	})
	env := newEnvironment(t, map[string]string{keyEnv: string(keyFile)})
	env.fake.answer = empty(http.StatusNoContent, "")
	if _, err := env.change(t, sitesAddID, "edit", changes[0].args); err != nil {
		t.Fatal(err)
	}
	env.fake.mu.Lock()
	form := env.fake.grants[0]
	env.fake.mu.Unlock()
	claims := decodeSegment(t, strings.Split(form.Get("assertion"), ".")[1])
	// A connection asks for the scopes of every tool it offers: the edit scope joins the read scope.
	if got, _ := claims["scope"].(string); !strings.Contains(" "+got+" ", " "+scopeEdit+" ") {
		t.Errorf("claims = %v, want the edit scope", claims)
	}
}

func TestChangesRefuseBadArgumentsAndOtherPropertiesBeforeAnyRequest(t *testing.T) {
	env := newEnvironment(t, nil)
	refused := []struct{ id, connection, args string }{
		{sitesAddID, "edit", `{"site_url":"shop.example.net"}`},
		{sitesAddID, "edit", `{"site_url":"https://www.example.com"}`},
		{sitesAddID, "edit", `{}`},
		{sitesDeleteID, "edit", `{"site_url":"sc-domain:localhost"}`},
		{sitesAddID, "editdomain", `{"site_url":"sc-domain:other.example"}`},
		{sitesDeleteID, "editprefix", `{"site_url":"sc-domain:example.com"}`},
		{sitemapsSubmitID, "edit", `{"site_url":"https://www.example.com/"}`},
		{sitemapsSubmitID, "edit", `{"site_url":"https://www.example.com/","feedpath":"ftp://www.example.com/s.xml"}`},
		{sitemapsSubmitID, "edit", `{"site_url":"https://www.example.com/","feedpath":"//www.example.com/s.xml"}`},
		{sitemapsSubmitID, "edit", `{"site_url":"https://www.example.com/","feedpath":"/sitemap.xml"}`},
		{sitemapsSubmitID, "edit", `{"site_url":"https://www.example.com/","feedpath":"javascript:alert(1)"}`},
		{sitemapsDeleteID, "edit", `{"site_url":"sc-domain:example.com","feedpath":"sc-domain:example.com"}`},
		{sitemapsDeleteID, "edit", `{"site_url":"https://www.example.com/","feedpath":"https://www.example.com/a#x"}`},
		{sitemapsSubmitID, "edit", `{"site_url":"https://www.example.com/","feedpath":"https://www.example.org/sitemap.xml"}`},
		{sitemapsSubmitID, "edit", `{"site_url":"https://www.example.com/","feedpath":"http://www.example.com/sitemap.xml"}`},
		{sitemapsSubmitID, "edit", `{"site_url":"https://www.example.com/","feedpath":"https://example.com/sitemap.xml"}`},
		{sitemapsSubmitID, "edit", `{"site_url":"https://www.example.com/blog/","feedpath":"https://www.example.com/sitemap.xml"}`},
		{sitemapsSubmitID, "edit", `{"site_url":"https://www.example.com/blog/","feedpath":"https://www.example.com/Blog/s.xml"}`},
		{sitemapsDeleteID, "edit", `{"site_url":"sc-domain:example.com","feedpath":"https://notexample.com/s.xml"}`},
		{sitemapsDeleteID, "edit", `{"site_url":"sc-domain:example.com","feedpath":"https://example.com.evil.invalid/s.xml"}`},
		{sitemapsSubmitID, "editprefix", `{"feedpath":"https://www.example.com.evil.invalid/s.xml"}`},
		{sitemapsSubmitID, "editdomain", `{"site_url":"https://www.example.com/","feedpath":"` + sitemapURL + `"}`},
	}
	for _, c := range refused {
		_, err := env.change(t, c.id, c.connection, c.args)
		// The input schema may refuse first; that is a refusal before any request as well.
		if err == nil {
			t.Errorf("%s %s %s was accepted", c.id, c.connection, c.args)
		}
	}
	if calls, grants := env.fake.calls(); len(calls) != 0 || grants != 0 {
		t.Fatalf("%d requests and %d grants, want none", len(calls), grants)
	}
}

func TestChangesAcceptPropertiesAndFeedpathsUnderTheBinding(t *testing.T) {
	env := newEnvironment(t, nil)
	env.fake.answer = empty(http.StatusNoContent, "")
	good := []struct{ connection, args string }{
		{"editdomain", `{"feedpath":"https://example.com/s.xml"}`},
		{"editdomain", `{"feedpath":"http://Blog.Example.COM/a/s.xml"}`},
		{"editdomain", `{"site_url":"sc-domain:example.com","feedpath":"https://example.com:8443/s.xml"}`},
		{"editprefix", `{"feedpath":"https://WWW.example.com/sitemap.xml?x=1"}`},
		{"editprefix", `{"site_url":"https://www.example.com/","feedpath":"https://www.example.com/a/b.xml"}`},
	}
	for _, c := range good {
		if _, err := env.change(t, sitemapsSubmitID, c.connection, c.args); err != nil {
			t.Errorf("%s %s: %v", c.connection, c.args, err)
		}
	}
	calls, _ := env.fake.calls()
	if len(calls) != len(good) {
		t.Fatalf("%d requests, want %d", len(calls), len(good))
	}
	if calls[0].escapedPath != "/webmasters/v3/sites/sc-domain%3Aexample.com/sitemaps/https%3A%2F%2Fexample.com%2Fs.xml" {
		t.Errorf("bound domain request = %s", calls[0].escapedPath)
	}
	// A bound connection adds and removes its own property only.
	for _, id := range []string{sitesAddID, sitesDeleteID} {
		if _, err := env.change(t, id, "editdomain", `{}`); err != nil {
			t.Errorf("%s on the bound property: %v", id, err)
		}
	}
}

func TestUnderPropertyComparesPrefixesLiterally(t *testing.T) {
	for _, c := range []struct {
		property, address string
		want              bool
	}{
		{"https://www.example.com/blog/", "https://www.example.com/blog/s.xml", true},
		{"https://www.example.com/blog/", "HTTPS://WWW.EXAMPLE.COM/blog/s.xml", true},
		{"https://www.example.com/blog/", "https://www.example.com/blog", false},
		{"https://www.example.com/blog/", "https://www.example.com/blogs/s.xml", false},
		{"https://www.example.com/", "https://www.example.com", false},
		{"https://www.example.com/", "https://www.example.com:444/s.xml", false},
		{"sc-domain:example.com", "http://a.b.example.com/s.xml", true},
		{"sc-domain:Example.com", "https://EXAMPLE.com/s.xml", true},
		{"sc-domain:example.com", "https://badexample.com/s.xml", false},
	} {
		if got := underProperty(c.property, c.address); got != c.want {
			t.Errorf("underProperty(%q, %q) = %t, want %t", c.property, c.address, got, c.want)
		}
	}
}

func TestChangeFailuresMapToClassesWithoutProviderText(t *testing.T) {
	for name, c := range map[string]struct {
		answer   func(*http.Request, int) (*http.Response, error)
		class    provider.Class
		requests int
		grants   int
		contains string
	}{
		"403 without owner right": {reply(http.StatusForbidden, errorObject(403, "PERMISSION_DENIED", "FORBIDDEN")),
			provider.ClassPermission, 1, 1, ""},
		"404 on delete": {reply(http.StatusNotFound, errorObject(404, "NOT_FOUND", "")),
			provider.ClassNotFound, 1, 1, ""},
		"429 without Retry-After": {reply(http.StatusTooManyRequests, errorObject(429, "RESOURCE_EXHAUSTED",
			"RATE_LIMIT_EXCEEDED")), provider.ClassRateLimited, 1, 1, ""},
		// A change is never repeated: after a 401 the cached token is dropped, the request is not resent.
		"401 is not repeated": {reply(http.StatusUnauthorized, errorObject(401, "UNAUTHENTICATED", "CREDENTIALS_MISSING")),
			provider.ClassAuth, 1, 1, ""},
		"500": {reply(http.StatusInternalServerError, errorObject(500, "INTERNAL", "")),
			provider.ClassProviderError, 1, 1, ""},
	} {
		t.Run(name, func(t *testing.T) {
			for _, tool := range changes {
				env := newEnvironment(t, nil)
				env.fake.answer = c.answer
				_, err := env.change(t, tool.id, "edit", tool.args)
				if classOf(err) != c.class {
					t.Fatalf("%s: error = %v (class %q), want %q", tool.id, err, classOf(err), c.class)
				}
				if strings.Contains(err.Error(), detailCanary) || strings.Contains(err.Error(), accessPrefix) {
					t.Errorf("%s: the message carries provider text or a token: %v", tool.id, err)
				}
				calls, grants := env.fake.calls()
				if len(calls) != c.requests || grants != c.grants {
					t.Errorf("%s: %d requests and %d grants, want %d and %d", tool.id, len(calls), grants, c.requests, c.grants)
				}
			}
		})
	}
}

func TestNotFoundOfADeleteNamesTheRightThing(t *testing.T) {
	env := newEnvironment(t, nil)
	env.fake.answer = reply(http.StatusNotFound, errorObject(404, "NOT_FOUND", ""))
	_, siteErr := env.change(t, sitesDeleteID, "edit", changes[1].args)
	_, mapErr := env.change(t, sitemapsDeleteID, "edit", changes[3].args)
	if !strings.Contains(siteErr.Error(), "property") || !strings.Contains(mapErr.Error(), "sitemap") {
		t.Errorf("messages = %v / %v", siteErr, mapErr)
	}
}

func TestChangeTooManyRequestsHoldsTheNextCall(t *testing.T) {
	env := newEnvironment(t, nil)
	env.fake.answer = reply(http.StatusTooManyRequests, errorObject(429, "RESOURCE_EXHAUSTED", "RATE_LIMIT_EXCEEDED"))
	limiters = ratelimit.NewRegistry(time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	request := application.InvokeRequest{Operation: sitesAddID, Connection: "edit",
		Arguments: json.RawMessage(changes[0].args), Confirmed: true}
	if _, err := env.core.Invoke(ctx, request); classOf(err) != provider.ClassRateLimited {
		t.Fatalf("first call = %v", err)
	}
	if _, err := env.core.Invoke(ctx, request); classOf(err) != provider.ClassRateLimited {
		t.Fatalf("second call = %v, want rate-limited before sending", err)
	}
	if calls, _ := env.fake.calls(); len(calls) != 1 {
		t.Errorf("%d requests, want one", len(calls))
	}
}

func TestChangeDescriptorsCarryTheRiskAndWarnings(t *testing.T) {
	want := map[string]struct {
		effect capability.Effect
		hint   string
	}{
		sitesAddID:       {capability.EffectCreate, "does not verify"},
		sitesDeleteID:    {capability.EffectDelete, "not the website"},
		sitemapsSubmitID: {capability.EffectCreate, "does not fetch"},
		sitemapsDeleteID: {capability.EffectDelete, "file of the website stays"},
	}
	for _, d := range []capability.Descriptor{sitesAdd, sitesDelete, sitemapsSubmit, sitemapsDelete} {
		w := want[d.ID]
		r := d.Risk
		if r.Effect != w.effect || r.Idempotency != capability.IdempotencyIdempotent ||
			r.Confirmation != capability.ConfirmationRequired || !r.OpenWorld || r.DataSensitivity == "" {
			t.Errorf("%s risk = %+v", d.ID, r)
		}
		if !strings.Contains(d.Description, w.hint) {
			t.Errorf("%s description lacks %q", d.ID, w.hint)
		}
		if strings.Contains(strings.ToLower(d.Description+d.Title+strings.Join(d.Tags, " ")), "pages") {
			t.Errorf("%s text contains the search word pages", d.ID)
		}
	}
}

func TestReadOnlyConnectionsDoNotOfferChangingTools(t *testing.T) {
	env := newEnvironment(t, nil)
	for _, c := range changes {
		_, err := env.core.Invoke(context.Background(), application.InvokeRequest{
			Operation: c.id, Connection: "all", Arguments: json.RawMessage(c.args), Confirmed: true})
		if err == nil {
			t.Errorf("%s ran on a read-only connection", c.id)
		}
	}
	if calls, _ := env.fake.calls(); len(calls) != 0 {
		t.Errorf("%d requests, want none", len(calls))
	}
}

func TestPropertyRemovalIsListedOnlyAndInNoProfile(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	d, _, found := reg.Lookup(sitesDeleteID)
	if !found || d.Risk.Effect != capability.EffectDelete || !d.RequiresToolAllowList ||
		!strings.Contains(d.Description, "listed-only") {
		t.Errorf("%s is not a listed-only deletion: %+v", sitesDeleteID, d)
	}
	for _, profile := range metadata.Profiles {
		if slices.Contains(profile.Tools, sitesDeleteID) {
			t.Errorf("profile %s holds the listed-only %s", profile.ID, sitesDeleteID)
		}
	}
	// A sitemap is submitted again, so its removal stays in the profile edit.
	d, _, _ = reg.Lookup(sitemapsDeleteID)
	if d.RequiresToolAllowList {
		t.Errorf("%s is listed-only", sitemapsDeleteID)
	}
	for _, profile := range metadata.Profiles {
		if got := slices.Contains(profile.Tools, sitemapsDeleteID); got != (profile.ID == "edit") {
			t.Errorf("profile %s holds %s: %t", profile.ID, sitemapsDeleteID, got)
		}
	}
	env := newEnvironment(t, nil)
	env.fake.answer = empty(http.StatusNoContent, "")
	if _, err := env.change(t, sitesDeleteID, "effects", changes[1].args); err == nil {
		t.Errorf("%s is reachable through effects", sitesDeleteID)
	}
	if _, err := env.change(t, sitemapsDeleteID, "effects", changes[3].args); err != nil {
		t.Errorf("%s is not reachable through effects: %v", sitemapsDeleteID, err)
	}
	if _, err := env.change(t, sitesDeleteID, "edit", changes[1].args); err != nil {
		t.Errorf("%s is not reachable through a tools list: %v", sitesDeleteID, err)
	}
}
