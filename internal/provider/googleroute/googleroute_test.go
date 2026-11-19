package googleroute

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

func argumentsFor(name string) string { return `{"name":"` + name + `"}` }

func TestRegisterPublishesMetadata(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatalf("Register() = %v", err)
	}
	if err := reg.ValidateProfiles(); err != nil {
		t.Fatalf("ValidateProfiles() = %v", err)
	}
	metadata, found := reg.ProviderMetadata(Provider)
	if !found || metadata.DefaultBaseURL != "https://routeoptimization.googleapis.com" ||
		metadata.DocPath() != "google.route-optimization/setup.md" || metadata.Token == nil {
		t.Fatalf("metadata = %+v", metadata)
	}
	if !reflect.DeepEqual(metadata.SecretRoles, config.TokenSecretRoles()) {
		t.Errorf("secret roles = %+v, want the token roles", metadata.SecretRoles)
	}
	if len(metadata.Tools) != 2 || metadata.Tools[0].ID != operationsGetID ||
		metadata.Tools[0].Effect != config.EffectRead || metadata.Tools[1].ID != toursOptimizeID ||
		metadata.Tools[1].Effect != config.EffectExecute {
		t.Fatalf("tools = %+v", metadata.Tools)
	}
	for _, id := range []string{operationsGetID, toursOptimizeID} {
		if want := []string{"https://www.googleapis.com/auth/cloud-platform"}; !reflect.DeepEqual(metadata.Token.Scopes[id], want) {
			t.Errorf("scopes of %s = %v, want %v", id, metadata.Token.Scopes[id], want)
		}
	}
	if len(metadata.Profiles) != 2 || metadata.Profiles[0].ID != "read" || !metadata.Profiles[0].Recommended ||
		!reflect.DeepEqual(metadata.Profiles[0].Tools, []string{operationsGetID}) ||
		metadata.Profiles[1].ID != "optimize" || metadata.Profiles[1].Recommended ||
		!reflect.DeepEqual(metadata.Profiles[1].Tools, []string{toursOptimizeID, operationsGetID}) {
		t.Errorf("profiles = %+v", metadata.Profiles)
	}
	if err := metadata.ValidateBaseURL("https://routeoptimization.googleapis.com/"); err != nil {
		t.Errorf("official origin refused: %v", err)
	}
	for _, other := range []string{"https://example.invalid", "http://routeoptimization.googleapis.com",
		"https://routeoptimization.googleapis.com/v1", "https://routeoptimization.googleapis.com:8443",
		"https://routeoptimization.googleapis.com.example.invalid"} {
		if err := metadata.ValidateBaseURL(other); err == nil {
			t.Errorf("base URL %q accepted", other)
		}
	}
	if tool, _, _ := reg.Lookup(operationsGetID); tool.Risk.DataSensitivity == "" {
		t.Error("the tool states no data sensitivity")
	}
}

func TestTargetsAreProjectsOnly(t *testing.T) {
	for _, valid := range []string{"projects/route-project-123", "projects/415104041262"} {
		if _, err := parseTarget(valid); err != nil {
			t.Errorf("%q refused: %v", valid, err)
		}
	}
	for _, bad := range []string{"folders/1", "organizations/1", "billingAccounts/012345-ABCDEF-678901", "projects/", "projects/BAD",
		"projects/a/b", "projects/*", "route-project-123"} {
		_, err := parseTarget(bad)
		if err == nil {
			t.Errorf("%q accepted", bad)
		} else if strings.Contains(err.Error(), "BAD") {
			t.Errorf("error %q quotes the value", err)
		}
	}
	if _, err := parseAllowlist([]string{"projects/route-project-123", "projects/route-project-123"}); err == nil {
		t.Error("a duplicate target was accepted")
	}
	// A connection with an invalid target is invalid before any call.
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	cfg := config.New(reg)
	cfg.Services = map[string]config.Service{"route": {Provider: Provider}}
	cfg.Credentials = map[string]config.Credential{credential: {Provider: Provider, Type: config.CredentialTypeKeyring}}
	cfg.Connections = map[string]config.Connection{"work": {Service: "route", Credential: credential,
		Effects: []config.Effect{config.EffectRead}, Targets: []string{"folders/1"}}}
	if err := cfg.Validate(); err == nil {
		t.Error("a connection with a folder target is valid")
	}
}

func TestOperationsGet(t *testing.T) {
	call := func(t *testing.T, targets []string, answer reply) (*environment, map[string]any, error) {
		e := newEnvironment(t, targets)
		e.fake.routes[testPath] = answer
		out, err := e.invoke(operationsGetID, argumentsFor(testName))
		var got map[string]any
		if err == nil {
			if jsonErr := json.Unmarshal([]byte(out), &got); jsonErr != nil {
				t.Fatal(jsonErr)
			}
		}
		return e, got, err
	}
	t.Run("running", func(t *testing.T) {
		e, got, err := call(t, nil, okReply(`{"name":"`+testName+`","metadata":{"@type":"type.googleapis.com/x.Meta"}}`))
		if err != nil {
			t.Fatal(err)
		}
		if got["done"] == true || got["response"] != nil || got["name"] != testName {
			t.Errorf("result = %v", got)
		}
		r := e.fake.api()
		if len(r) != 1 || r[0].method != http.MethodGet || r[0].url != "https://routeoptimization.googleapis.com/v1/"+testName {
			t.Errorf("requests = %+v", r)
		}
	})
	t.Run("done with a response", func(t *testing.T) {
		_, got, err := call(t, []string{"projects/" + testProject}, okReply(`{"name":"`+testName+
			`","done":true,"response":{"@type":"type.googleapis.com/x.Response","routes":[{"vehicleIndex":1}]}}`))
		if err != nil {
			t.Fatal(err)
		}
		response, _ := got["response"].(map[string]any)
		if got["done"] != true || response["routes"] == nil {
			t.Errorf("result = %v", got)
		}
	})
	t.Run("done with an error", func(t *testing.T) {
		_, _, err := call(t, nil, okReply(`{"name":"`+testName+`","done":true,"error":{"code":7,"message":"`+detailCanary+`"}}`))
		if classOf(err) != provider.ClassPermission {
			t.Fatalf("err = %v, want permission", err)
		}
		if strings.Contains(err.Error(), detailCanary) {
			t.Errorf("message %q carries the provider text", err)
		}
		var failed *provider.Error
		if !errors.As(err, &failed) || failed.Original == nil || failed.Original.Entries[0].Message != detailCanary {
			t.Errorf("original = %+v", failed)
		}
	})
	t.Run("error classes by code", func(t *testing.T) {
		for code, want := range map[string]provider.Class{"5": provider.ClassNotFound, "7": provider.ClassPermission,
			"8": provider.ClassRateLimited, "16": provider.ClassAuth, "13": provider.ClassProviderError} {
			_, _, err := call(t, nil, okReply(`{"name":"`+testName+`","done":true,"error":{"code":`+code+`}}`))
			if classOf(err) != want {
				t.Errorf("code %s: err = %v, want %s", code, err, want)
			}
		}
	})
	t.Run("an error while running is not an outcome", func(t *testing.T) {
		_, got, err := call(t, nil, okReply(`{"name":"`+testName+`","error":{"code":7}}`))
		if err != nil || got["done"] == true {
			t.Errorf("got=%v err=%v", got, err)
		}
	})
	t.Run("another operation answered", func(t *testing.T) {
		for _, name := range []string{"projects/" + testProject + "/locations/us-central1/operations/other",
			"projects/" + testProject + "/locations/europe-west1/operations/op-1",
			"projects/" + otherProject + "/locations/us-central1/operations/op-1", "operations/op-1", ""} {
			_, _, err := call(t, nil, okReply(`{"name":"`+name+`","done":true}`))
			if classOf(err) != provider.ClassInvalidResponse {
				t.Errorf("answer for %q: err = %v, want invalid-provider-response", name, err)
			}
		}
	})
	t.Run("the project may answer by number", func(t *testing.T) {
		_, _, err := call(t, nil, okReply(`{"name":"projects/415104041262/locations/us-central1/operations/op-1"}`))
		if err != nil {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("bounded provider data", func(t *testing.T) {
		deep := strings.Repeat(`{"a":`, 20) + `"x"` + strings.Repeat(`}`, 20)
		long := strings.Repeat("y", 5000)
		_, got, err := call(t, nil, okReply(`{"name":"`+testName+`","done":true,"response":{"deep":`+deep+`,"long":"`+long+`"}}`))
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ := json.Marshal(got["response"])
		if strings.Count(string(encoded), `"a"`) > maxDepth || strings.Contains(string(encoded), long) {
			t.Errorf("response %s is not bounded", encoded)
		}
	})
}

func TestOperationsGetRefusesBeforeSecretAndIO(t *testing.T) {
	good := "projects/" + testProject + "/locations/us-central1/operations/op-1"
	cases := []struct {
		name    string
		targets []string
		args    string
	}{
		{"project outside the targets", []string{"projects/" + otherProject}, argumentsFor(good)},
		{"number for an ID target", []string{"projects/415104041262"}, argumentsFor(good)},
		{"missing locations", nil, argumentsFor("projects/" + testProject + "/operations/op-1")},
		{"traversal", nil, argumentsFor("projects/" + testProject + "/locations/us-central1/operations/../x")},
		{"extra segment", nil, argumentsFor(good + "/x")},
		{"location with a dot", nil, argumentsFor("projects/" + testProject + "/locations/us.central1/operations/op-1")},
		{"encoded slash", nil, argumentsFor("projects/" + testProject + "/locations/us-central1/operations/a%2fb")},
		{"query", nil, argumentsFor(good + "?alt=media")},
		{"bad project form", nil, argumentsFor("projects/BAD/locations/us-central1/operations/op-1")},
		{"wildcard project", nil, argumentsFor("projects/-/locations/us-central1/operations/op-1")},
		{"no name", nil, `{}`},
		{"undeclared argument", nil, `{"name":"` + good + `","url":"https://example.invalid"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newEnvironment(t, c.targets)
			e.fake.routes["/v1/"+good] = okReply(`{"name":"` + good + `"}`)
			_, err := e.invoke(operationsGetID, c.args)
			if err == nil {
				t.Fatal("the call was accepted")
			}
			if strings.Contains(err.Error(), otherProject) || strings.Contains(err.Error(), "415104041262") {
				t.Errorf("error %q names a project", err)
			}
			if len(e.fake.api()) != 0 || e.fake.tokenRequests() != 0 || e.lookups != 0 {
				t.Errorf("api=%d tokens=%d lookups=%d, want a refusal before I/O and secret", len(e.fake.api()),
					e.fake.tokenRequests(), e.lookups)
			}
		})
	}
	t.Run("a target of the same spelling passes", func(t *testing.T) {
		e := newEnvironment(t, []string{"projects/" + otherProject, "projects/" + testProject})
		e.fake.routes[testPath] = okReply(`{"name":"` + testName + `"}`)
		if _, err := e.invoke(operationsGetID, argumentsFor(testName)); err != nil {
			t.Fatal(err)
		}
	})
}

func TestForeignBaseURLIsRefusedBeforeSecretAccess(t *testing.T) {
	e := newEnvironment(t, nil)
	resolved := e.resolved()
	resolved.BaseURL = "https://example.invalid"
	_, err := handleOperationsGet(context.Background(), resolved, e.secrets(), e.red, json.RawMessage(argumentsFor(testName)))
	if classOf(err) != provider.ClassProviderError || e.lookups != 0 || len(e.fake.api()) != 0 {
		t.Fatalf("err=%v lookups=%d requests=%d", err, e.lookups, len(e.fake.api()))
	}
	if strings.Contains(err.Error(), "example.invalid") {
		t.Errorf("error %q quotes the base URL", err)
	}
}

func TestTokenTravelsInTheHeaderOnly(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes[testPath] = okReply(`{"name":"` + testName + `"}`)
	out, err := e.invoke(operationsGetID, argumentsFor(testName))
	if err != nil {
		t.Fatal(err)
	}
	const issued = "synthetic-access-1"
	for _, request := range e.fake.api() {
		if request.auth != "Bearer "+issued {
			t.Errorf("Authorization = %q", request.auth)
		}
		if strings.Contains(request.url, issued) || strings.Contains(request.url, refreshValue) {
			t.Errorf("request %s carries a secret outside the header", request.url)
		}
	}
	if strings.Contains(out, issued) || strings.Contains(out, refreshValue) {
		t.Errorf("result %q carries a secret", out)
	}
	if got := e.red.Apply("x " + issued + " " + refreshValue); strings.Contains(got, issued) || strings.Contains(got, refreshValue) {
		t.Errorf("redactor lets %q through", got)
	}
}

func TestUnauthorizedRenewsTheTokenOnce(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.hook = func(int, *http.Request) *reply {
		r := reply{status: 401, body: errorObject(401, "UNAUTHENTICATED", "CREDENTIALS_MISSING")}
		return &r
	}
	_, err := e.invoke(operationsGetID, argumentsFor(testName))
	if classOf(err) != provider.ClassAuth {
		t.Fatalf("err = %v, want auth", err)
	}
	if got := len(e.fake.api()); got != 2 || e.fake.tokenRequests() != 2 {
		t.Errorf("requests = %d, token requests = %d, want one retry with one new token", got, e.fake.tokenRequests())
	}
	if strings.Contains(err.Error(), detailCanary) || strings.Contains(err.Error(), "synthetic-access") {
		t.Errorf("error %q carries provider text or a token", err)
	}
}

func TestGoogleErrorMapping(t *testing.T) {
	cases := []struct {
		name  string
		reply reply
		want  provider.Class
	}{
		{"unauthenticated", reply{status: 401, body: errorObject(401, "UNAUTHENTICATED", "")}, provider.ClassAuth},
		{"expired token", reply{status: 401, body: errorObject(401, "UNAUTHENTICATED", "ACCESS_TOKEN_EXPIRED")}, provider.ClassAuth},
		{"unknown 401", reply{status: 401, body: `{}`}, provider.ClassInvalidResponse},
		{"permission denied", reply{status: 403, body: errorObject(403, "PERMISSION_DENIED", "")}, provider.ClassPermission},
		{"api disabled", reply{status: 403, body: errorObject(403, "PERMISSION_DENIED", "SERVICE_DISABLED")}, provider.ClassPermission},
		{"scope", reply{status: 403, body: errorObject(403, "PERMISSION_DENIED", "ACCESS_TOKEN_SCOPE_INSUFFICIENT")}, provider.ClassScopeMissing},
		{"unknown 403", reply{status: 403, body: `<html>`}, provider.ClassInvalidResponse},
		{"not found", reply{status: 404, body: errorObject(404, "NOT_FOUND", "")}, provider.ClassNotFound},
		{"404 without a status", reply{status: 404, body: `{}`}, provider.ClassInvalidResponse},
		{"bad request", reply{status: 400, body: errorObject(400, "INVALID_ARGUMENT", "")}, provider.ClassInvalidResponse},
		{"rate limit", reply{status: 429, body: errorObject(429, "RESOURCE_EXHAUSTED", "")}, provider.ClassRateLimited},
		{"other 4xx", reply{status: 418, body: `{}`}, provider.ClassInvalidResponse},
		{"unreadable body", reply{status: 200, body: `{"name":`}, provider.ClassInvalidResponse},
		{"not an object", reply{status: 200, body: `[]`}, provider.ClassInvalidResponse},
		{"not json", reply{status: 200, body: `<html>`, header: http.Header{"Content-Type": {"text/html"}}}, provider.ClassInvalidResponse},
		{"no name", reply{status: 200, body: `{"done":true}`}, provider.ClassInvalidResponse},
		{"too large", reply{status: 200, body: `{"name":"` + testName + `","metadata":{"x":"` + strings.Repeat("x", 3<<20) + `"}}`}, provider.ClassInvalidResponse},
		{"redirect", reply{status: 302, header: http.Header{"Location": {"https://example.invalid/"}}}, provider.ClassProviderError},
		{"timeout", reply{err: context.DeadlineExceeded}, provider.ClassTimeout},
		{"unreachable", reply{err: &net.OpError{Op: "dial", Err: io.ErrUnexpectedEOF}}, provider.ClassUnreachable},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newEnvironment(t, nil)
			e.fake.routes[testPath] = c.reply
			_, err := e.invoke(operationsGetID, argumentsFor(testName))
			if classOf(err) != c.want {
				t.Fatalf("err = %v (%s), want class %s", err, classOf(err), c.want)
			}
			// A 401 renews the token once; nothing else repeats the request.
			want := 1
			if c.reply.status == http.StatusUnauthorized {
				want = 2
			}
			if got := len(e.fake.api()); got != want {
				t.Errorf("requests = %d, want %d", got, want)
			}
			if strings.Contains(err.Error(), detailCanary) || strings.Contains(err.Error(), "synthetic-access") {
				t.Errorf("error %q carries provider text or a token", err)
			}
		})
	}
	t.Run("disabled API is named and carries the original", func(t *testing.T) {
		failed := googleError("op", 403, []byte(errorObject(403, "PERMISSION_DENIED", "SERVICE_DISABLED")))
		if !strings.Contains(failed.Message, "not enabled") || !strings.Contains(failed.Message, "routeoptimization.googleapis.com") {
			t.Errorf("message = %q", failed.Message)
		}
		if strings.Contains(failed.Message, detailCanary) || failed.Original == nil {
			t.Errorf("message = %q original = %+v", failed.Message, failed.Original)
		}
	})
}

func TestRateLimitHoldsTheNextRequestAndNeverRepeats(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes[testPath] = reply{status: 429, body: errorObject(429, "RESOURCE_EXHAUSTED", ""),
		header: http.Header{"Retry-After": {"30"}}}
	_, err := e.invoke(operationsGetID, argumentsFor(testName))
	if classOf(err) != provider.ClassRateLimited || len(e.fake.api()) != 1 {
		t.Fatalf("err = %v, requests = %d", err, len(e.fake.api()))
	}
	_, _ = e.invoke(operationsGetID, argumentsFor(testName))
	if len(e.sleeps) == 0 || e.sleeps[len(e.sleeps)-1] < 29*time.Second {
		t.Errorf("sleeps = %v, want the wait Google asked for", e.sleeps)
	}
	// Without Retry-After the next call still waits a second.
	e = newEnvironment(t, nil)
	e.fake.routes[testPath] = reply{status: 429, body: errorObject(429, "RESOURCE_EXHAUSTED", "")}
	_, _ = e.invoke(operationsGetID, argumentsFor(testName))
	_, _ = e.invoke(operationsGetID, argumentsFor(testName))
	if len(e.sleeps) == 0 || e.sleeps[len(e.sleeps)-1] < time.Second {
		t.Errorf("sleeps = %v, want at least a second", e.sleeps)
	}
}

func TestRequestsAreSpacedPerCredential(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes[testPath] = okReply(`{"name":"` + testName + `"}`)
	for range 3 {
		if _, err := e.invoke(operationsGetID, argumentsFor(testName)); err != nil {
			t.Fatal(err)
		}
	}
	if len(e.sleeps) != 2 {
		t.Errorf("sleeps = %v, want two waits for three requests", e.sleeps)
	}
}

func TestMissingConsentEndsBeforeAnyRequest(t *testing.T) {
	e := newEnvironment(t, nil)
	delete(e.roles, config.RoleRefreshToken)
	_, err := e.invoke(operationsGetID, argumentsFor(testName))
	if classOf(err) != provider.ClassConsentRequired {
		t.Fatalf("err = %v, want consent-required", err)
	}
	if len(e.fake.api()) != 0 || e.fake.tokenRequests() != 0 {
		t.Error("a request was sent without a consent")
	}
}

func TestConnectionTest(t *testing.T) {
	probePath := func(project string) string {
		return "/v1/projects/" + project + "/locations/global/operations/qatlas-connection-test"
	}
	t.Run("without a target only the token is proved", func(t *testing.T) {
		e := newEnvironment(t, nil)
		class, err := e.testConnection()
		if err != nil || class != provider.ClassOK || len(e.fake.api()) != 0 || e.fake.tokenRequests() != 1 {
			t.Fatalf("class=%s err=%v requests=%d tokens=%d", class, err, len(e.fake.api()), e.fake.tokenRequests())
		}
	})
	t.Run("a missing probe operation passes for every project", func(t *testing.T) {
		e := newEnvironment(t, []string{"projects/" + testProject, "projects/415104041262"})
		class, err := e.testConnection()
		if err != nil || class != provider.ClassOK {
			t.Fatalf("class=%s err=%v", class, err)
		}
		requests := e.fake.api()
		if len(requests) != 2 || requests[0].method != http.MethodGet ||
			requests[0].url != "https://routeoptimization.googleapis.com"+probePath(testProject) ||
			requests[1].url != "https://routeoptimization.googleapis.com"+probePath("415104041262") {
			t.Errorf("requests = %+v", requests)
		}
	})
	t.Run("an existing probe operation passes", func(t *testing.T) {
		name := "projects/" + testProject + "/locations/global/operations/qatlas-connection-test"
		e := newEnvironment(t, []string{"projects/" + testProject})
		e.fake.routes[probePath(testProject)] = okReply(`{"name":"` + name + `"}`)
		if class, err := e.testConnection(); err != nil || class != provider.ClassOK {
			t.Fatalf("class=%s err=%v", class, err)
		}
	})
	failures := []struct {
		name  string
		reply reply
		want  provider.Class
	}{
		{"permission denied", reply{status: 403, body: errorObject(403, "PERMISSION_DENIED", "")}, provider.ClassPermission},
		{"api disabled", reply{status: 403, body: errorObject(403, "PERMISSION_DENIED", "SERVICE_DISABLED")}, provider.ClassPermission},
		{"scope", reply{status: 403, body: errorObject(403, "PERMISSION_DENIED", "ACCESS_TOKEN_SCOPE_INSUFFICIENT")}, provider.ClassScopeMissing},
		{"unauthenticated", reply{status: 401, body: errorObject(401, "UNAUTHENTICATED", "")}, provider.ClassAuth},
		{"unknown 404", reply{status: 404, body: `{}`}, provider.ClassInvalidResponse},
	}
	for _, c := range failures {
		t.Run(c.name, func(t *testing.T) {
			e := newEnvironment(t, []string{"projects/" + testProject})
			e.fake.routes[probePath(testProject)] = c.reply
			class, err := e.testConnection()
			if err != nil || class != c.want {
				t.Fatalf("class=%s err=%v, want %s", class, err, c.want)
			}
		})
	}
	t.Run("a failing second project fails the test", func(t *testing.T) {
		e := newEnvironment(t, []string{"projects/" + testProject, "projects/" + otherProject})
		e.fake.routes[probePath(otherProject)] = reply{status: 403, body: errorObject(403, "PERMISSION_DENIED", "")}
		if class, err := e.testConnection(); err != nil || class != provider.ClassPermission {
			t.Fatalf("class=%s err=%v", class, err)
		}
	})
	t.Run("expired consent", func(t *testing.T) {
		e := newEnvironment(t, nil)
		e.fake.tokenReply = &reply{status: 400, body: `{"error":"invalid_grant"}`}
		if class, err := e.testConnection(); err != nil || class != provider.ClassConsentExpired {
			t.Fatalf("class=%s err=%v", class, err)
		}
	})
	t.Run("no consent yet", func(t *testing.T) {
		e := newEnvironment(t, []string{"projects/" + testProject})
		delete(e.roles, config.RoleRefreshToken)
		if class, err := e.testConnection(); err != nil || class != provider.ClassConsentRequired || len(e.fake.api()) != 0 {
			t.Fatalf("class=%s err=%v", class, err)
		}
	})
	t.Run("invalid credential", func(t *testing.T) {
		e := newEnvironment(t, nil)
		e.fake.tokenReply = &reply{status: 401, body: `{"error":"invalid_client","error_description":"` + detailCanary + `"}`}
		if class, err := e.testConnection(); err != nil || class != provider.ClassAuth {
			t.Fatalf("class=%s err=%v", class, err)
		}
	})
	t.Run("missing scope in the consent", func(t *testing.T) {
		e := newEnvironment(t, nil)
		e.fake.tokenReply = &reply{status: 200, body: `{"access_token":"synthetic-access-x","token_type":"Bearer","expires_in":3600,"scope":"openid"}`}
		if class, err := e.testConnection(); err != nil || class != provider.ClassScopeMissing {
			t.Fatalf("class=%s err=%v", class, err)
		}
	})
	t.Run("bad targets end before a secret is read", func(t *testing.T) {
		e := newEnvironment(t, nil)
		resolved := e.resolved()
		resolved.Targets = []string{"folders/1"}
		class, err := TestConnection(context.Background(), resolved, e.secrets(), e.red)
		if err != nil || class != provider.ClassProviderError || e.lookups != 0 {
			t.Fatalf("class=%s err=%v lookups=%d", class, err, e.lookups)
		}
	})
	t.Run("foreign base URL", func(t *testing.T) {
		e := newEnvironment(t, nil)
		resolved := e.resolved()
		resolved.BaseURL = "https://example.invalid"
		class, err := TestConnection(context.Background(), resolved, e.secrets(), e.red)
		if err != nil || class != provider.ClassProviderError || e.lookups != 0 || len(e.fake.api()) != 0 {
			t.Fatalf("class=%s err=%v lookups=%d", class, err, e.lookups)
		}
	})
}

func TestOperationOfRejectsAnOperationWithoutName(t *testing.T) {
	if _, err := operationOf("op", operationWire{}); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("an operation without a name: %v", err)
	}
	result, err := operationOf("op", operationWire{Name: testName, Metadata: json.RawMessage(`{"@type":"t"}`)})
	if err != nil || result.Done || result.Name != testName {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}
