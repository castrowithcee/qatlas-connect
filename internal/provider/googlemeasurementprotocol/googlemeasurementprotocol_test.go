package googlemeasurementprotocol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/ratelimit"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// The values are synthetic placeholders. No test reaches Google: a local test server answers through the
// package transport.
const (
	secretValue = "placeholder-stream-secret-one"
	secretEnv   = "TEST_MP_STREAM_SECRET"
	streamID    = "G-TESTSTREAM1"
	clientID    = "placeholder-client.1"
)

type recorded struct {
	method, path, rawQuery, body string
	header                       http.Header
}

type fakeGoogle struct {
	mu       sync.Mutex
	requests []recorded
	status   int
}

func (f *fakeGoogle) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	payload, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.requests = append(f.requests, recorded{r.Method, r.URL.EscapedPath(), r.URL.RawQuery, string(payload), r.Header.Clone()})
	f.mu.Unlock()
	if f.status == 0 {
		f.status = http.StatusNoContent
	}
	w.WriteHeader(f.status)
}

func (f *fakeGoogle) recorded() []recorded {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]recorded(nil), f.requests...)
}

type rewrite struct {
	target *url.URL
	base   http.RoundTripper
}

func (r rewrite) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Host != "www.google-analytics.com" {
		return nil, errors.New("unexpected host")
	}
	clone := req.Clone(req.Context())
	clone.URL.Scheme, clone.URL.Host, clone.Host = r.target.Scheme, r.target.Host, ""
	return r.base.RoundTrip(clone)
}

func noWait(t *testing.T) {
	t.Helper()
	t.Cleanup(limiters.Replace(streamID, ratelimit.New(0, time.Now, func(context.Context, time.Duration) error { return nil })))
}

func serve(t *testing.T, f http.Handler) {
	t.Helper()
	server := httptest.NewTLSServer(f)
	t.Cleanup(server.Close)
	target, _ := url.Parse(server.URL)
	previous := transport
	transport = rewrite{target: target, base: server.Client().Transport}
	t.Cleanup(func() { transport = previous })
	noWait(t)
}

type failingTransport struct{ err error }

func (f failingTransport) RoundTrip(*http.Request) (*http.Response, error) { return nil, f.err }

func failWith(t *testing.T, err error) {
	t.Helper()
	previous := transport
	transport = failingTransport{err}
	t.Cleanup(func() { transport = previous })
	noWait(t)
}

func registry(t *testing.T) *capability.Registry {
	t.Helper()
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatalf("Register() = %v", err)
	}
	if err := reg.ValidateProfiles(); err != nil {
		t.Fatalf("ValidateProfiles() = %v", err)
	}
	return reg
}

func coreConfig() *config.Config {
	service := func(id string) config.Service {
		options := map[string]string{}
		if id != "" {
			options[optionMeasurementID] = id
		}
		return config.Service{Provider: Provider, BaseURL: apiRoot, Options: options}
	}
	return &config.Config{
		Version: 1,
		Services: map[string]config.Service{"stream": service(streamID), "broken": service("g-lower"),
			"missing": service("")},
		Credentials: map[string]config.Credential{"key": {Type: config.CredentialTypeEnv,
			Values: map[string]string{roleSecret: secretEnv}}},
		Connections: map[string]config.Connection{
			"listed":    {Service: "stream", Credential: "key", Tools: []string{eventsSendID}},
			"unlisted":  {Service: "stream", Credential: "key"},
			"permitted": {Service: "stream", Credential: "key", Effects: []config.Effect{config.EffectCreate}},
			"broken":    {Service: "broken", Credential: "key", Tools: []string{eventsSendID}},
			"missing":   {Service: "missing", Credential: "key", Tools: []string{eventsSendID}},
			"bound":     {Service: "stream", Credential: "key", Tools: []string{eventsSendID}, Target: "somewhere"},
		},
	}
}

type environment struct {
	fake  *fakeGoogle
	core  *application.Core
	red   *redact.Redactor
	reads *int
}

func newEnvironment(t *testing.T, f *fakeGoogle) *environment {
	t.Helper()
	serve(t, f)
	return newCore(t, f)
}

func newCore(t *testing.T, f *fakeGoogle) *environment {
	t.Helper()
	reads := 0
	red := &redact.Redactor{}
	lookup := func(name string) string {
		reads++
		if name == secretEnv {
			return secretValue
		}
		return ""
	}
	return &environment{fake: f, core: application.New(registry(t), coreConfig(), secret.NewWith(lookup, nil, nil, red), red),
		red: red, reads: &reads}
}

func (e *environment) invoke(connection, arguments string, confirmed bool) (string, error) {
	response, err := e.core.Invoke(context.Background(), application.InvokeRequest{
		Operation: eventsSendID, Connection: connection, Arguments: json.RawMessage(arguments), Confirmed: confirmed})
	return string(response.Result), err
}

func classOf(err error) provider.Class {
	var failed *provider.Error
	if errors.As(err, &failed) {
		return failed.Class
	}
	return ""
}

func oneEvent(extra string) string {
	return `{"client_id":"` + clientID + `","events":[{"name":"qatlas_example","params":{"step":1}}]` + extra + `}`
}

func TestRegisterIsListedOnlyWithoutProfile(t *testing.T) {
	reg := registry(t)
	metadata, ok := reg.ProviderMetadata(Provider)
	if !ok || len(metadata.Profiles) != 0 || len(metadata.SecretRoles) != 1 || metadata.SecretRoles[0].Name != "api_secret" ||
		metadata.DocPath() != "google.measurement-protocol/setup.md" || len(metadata.Tools) != 1 {
		t.Fatalf("metadata = %+v", metadata)
	}
	tool := metadata.Tools[0]
	if tool.ID != "google.measurement-protocol.events.send" || !tool.RequiresToolAllowList || tool.Effect != config.EffectCreate {
		t.Errorf("tool = %+v", tool)
	}
	d, _, _ := reg.Lookup(eventsSendID)
	if d.Risk.Confirmation != capability.ConfirmationRequired || d.Risk.Idempotency != capability.IdempotencyNonIdempotent ||
		!d.Risk.OpenWorld || d.Risk.DataSensitivity != dataSensitivity {
		t.Errorf("risk = %+v", d.Risk)
	}
	if err := metadata.Target.Validate("x"); err == nil {
		t.Error("a target was accepted")
	}
}

func TestSendOneEventSendsTheDocumentedRequest(t *testing.T) {
	for _, status := range []int{http.StatusNoContent, http.StatusOK} {
		env := newEnvironment(t, &fakeGoogle{status: status})
		result, err := env.invoke("listed", oneEvent(`,"user_id":"u-1","non_personalized_ads":true,`+
			`"consent":{"ad_user_data":"DENIED","ad_personalization":"DENIED"},`+
			`"user_properties":{"plan":"free"}`), true)
		if err != nil {
			t.Fatalf("status %d: %v", status, err)
		}
		var got map[string]any
		_ = json.Unmarshal([]byte(result), &got)
		if got["sent"] != true || got["event_count"] != float64(1) || got["accepted"] != nil {
			t.Errorf("status %d: result = %s", status, result)
		}
		requests := env.fake.recorded()
		if len(requests) != 1 || requests[0].method != "POST" || requests[0].path != "/mp/collect" {
			t.Fatalf("requests = %+v", requests)
		}
		query, _ := url.ParseQuery(requests[0].rawQuery)
		if len(query) != 2 || query.Get("measurement_id") != streamID || query.Get("api_secret") != secretValue {
			t.Errorf("query keys = %v", query)
		}
		var body map[string]any
		if err := json.Unmarshal([]byte(requests[0].body), &body); err != nil {
			t.Fatal(err)
		}
		props, _ := body["user_properties"].(map[string]any)
		consent, _ := body["consent"].(map[string]any)
		if body["client_id"] != clientID || body["user_id"] != "u-1" || body["non_personalized_ads"] != true ||
			consent["ad_user_data"] != "DENIED" || props["plan"].(map[string]any)["value"] != "free" || len(body) != 6 {
			t.Errorf("body = %s", requests[0].body)
		}
	}
}

func manyEvents(n int) string {
	events := make([]string, n)
	for i := range events {
		events[i] = fmt.Sprintf(`{"name":"e%d"}`, i)
	}
	return `{"client_id":"` + clientID + `","events":[` + strings.Join(events, ",") + `]}`
}

func TestLimitsAreCheckedBeforeAnyRequestOrSecret(t *testing.T) {
	long := strings.Repeat("x", 500)
	params := make([]string, 25)
	for i := range params {
		params[i] = fmt.Sprintf(`"p%d":"%s"`, i, long)
	}
	event := `{"name":"big","params":{` + strings.Join(params, ",") + `}}`
	events := make([]string, 25)
	for i := range events {
		events[i] = event
	}
	tooBig := `{"client_id":"` + clientID + `","events":[` + strings.Join(events, ",") + `]}`

	env := newEnvironment(t, &fakeGoogle{})
	if _, err := env.invoke("listed", manyEvents(25), true); err != nil {
		t.Fatalf("25 events: %v", err)
	}
	if got := len(env.fake.recorded()); got != 1 {
		t.Fatalf("requests after 25 events = %d", got)
	}
	reads := *env.reads
	for name, arguments := range map[string]string{
		"26 events":  manyEvents(26),
		"body size":  tooBig,
		"no events":  `{"client_id":"` + clientID + `","events":[]}`,
		"no client":  `{"events":[{"name":"a"}]}`,
		"blank":      `{"client_id":" ","events":[{"name":"a"}]}`,
		"client int": `{"client_id":5,"events":[{"name":"a"}]}`,
	} {
		if _, err := env.invoke("listed", arguments, true); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if got := len(env.fake.recorded()); got != 1 {
		t.Errorf("a rejected call sent a request: %d", got)
	}
	if *env.reads != reads {
		t.Error("a rejected call read the secret")
	}
}

// The core checks the schema first, so the handler's own bounds are proved directly.
func TestHandlerBoundsStandAlone(t *testing.T) {
	var a sendArguments
	_ = json.Unmarshal([]byte(manyEvents(26)), &a)
	if _, err := buildBody(&a); err == nil || !strings.Contains(err.Error(), "25") {
		t.Errorf("26 events: %v", err)
	}
	long := strings.Repeat("x", 500)
	var big sendArguments
	big.ClientID = clientID
	for i := 0; i < 25; i++ {
		params := map[string]json.RawMessage{}
		for j := 0; j < 25; j++ {
			params[fmt.Sprintf("p%d", j)] = json.RawMessage(`"` + long + `"`)
		}
		big.Events = append(big.Events, eventArgument{Name: "big", Params: params})
	}
	if _, err := buildBody(&big); err == nil || !strings.Contains(err.Error(), "130 kB") {
		t.Errorf("large body: %v", err)
	}
}

func TestNameAndParameterRules(t *testing.T) {
	name41 := strings.Repeat("a", 41)
	prop25 := strings.Repeat("a", 25)
	for name, arguments := range map[string]string{
		"leading digit":     `{"client_id":"c","events":[{"name":"1abc"}]}`,
		"dash":              `{"client_id":"c","events":[{"name":"a-b"}]}`,
		"space":             `{"client_id":"c","events":[{"name":"a b"}]}`,
		"too long":          `{"client_id":"c","events":[{"name":"` + name41 + `"}]}`,
		"reserved":          `{"client_id":"c","events":[{"name":"session_start"}]}`,
		"app only":          `{"client_id":"c","events":[{"name":"screen_view"}]}`,
		"param prefix ga_":  `{"client_id":"c","events":[{"name":"a","params":{"ga_x":1}}]}`,
		"param prefix g_":   `{"client_id":"c","events":[{"name":"a","params":{"google_x":1}}]}`,
		"param prefix fb_":  `{"client_id":"c","events":[{"name":"a","params":{"firebase_x":1}}]}`,
		"param conversion":  `{"client_id":"c","events":[{"name":"a","params":{"firebase_conversion":1}}]}`,
		"param underscore":  `{"client_id":"c","events":[{"name":"a","params":{"_x":1}}]}`,
		"param name length": `{"client_id":"c","events":[{"name":"a","params":{"` + name41 + `":1}}]}`,
		"param nested":      `{"client_id":"c","events":[{"name":"a","params":{"x":{"y":1}}}]}`,
		"param null":        `{"client_id":"c","events":[{"name":"a","params":{"x":null}}]}`,
		"param value long":  `{"client_id":"c","events":[{"name":"a","params":{"x":"` + strings.Repeat("v", 501) + `"}}]}`,
		"prop name length":  `{"client_id":"c","events":[{"name":"a"}],"user_properties":{"` + prop25 + `":"v"}}`,
		"prop value length": `{"client_id":"c","events":[{"name":"a"}],"user_properties":{"p":"` + strings.Repeat("v", 37) + `"}}`,
		"prop reserved":     `{"client_id":"c","events":[{"name":"a"}],"user_properties":{"user_id":"v"}}`,
		"prop prefix":       `{"client_id":"c","events":[{"name":"a"}],"user_properties":{"google_p":"v"}}`,
		"consent value":     `{"client_id":"c","events":[{"name":"a"}],"consent":{"ad_user_data":"MAYBE"}}`,
		"timestamp old":     `{"client_id":"c","events":[{"name":"a"}],"timestamp_micros":1}`,
		"timestamp future":  `{"client_id":"c","events":[{"name":"a"}],"timestamp_micros":99999999999999999}`,
		"extra argument":    `{"client_id":"c","events":[{"name":"a"}],"url":"https://example.com"}`,
	} {
		env := newEnvironment(t, &fakeGoogle{})
		if _, err := env.invoke("listed", arguments, true); err == nil {
			t.Errorf("%s: accepted", name)
		}
		if len(env.fake.recorded()) != 0 || *env.reads != 0 {
			t.Errorf("%s: I/O happened", name)
		}
	}
	// The allowed edges.
	env := newEnvironment(t, &fakeGoogle{})
	recent := time.Now().Add(-time.Hour).UnixMicro()
	ok := fmt.Sprintf(`{"client_id":"c","events":[{"name":"%s","params":{"%s":"%s","ok":true}}],`+
		`"user_properties":{"%s":"%s"},"timestamp_micros":%d}`,
		strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("v", 500),
		strings.Repeat("p", 24), strings.Repeat("v", 36), recent)
	if _, err := env.invoke("listed", ok, true); err != nil {
		t.Errorf("edge values refused: %v", err)
	}
}

func TestInvalidMeasurementIDIsRefusedBeforeTheSecret(t *testing.T) {
	for _, connection := range []string{"broken", "missing"} {
		env := newEnvironment(t, &fakeGoogle{})
		_, err := env.invoke(connection, oneEvent(""), true)
		if classOf(err) != provider.ClassProviderError || !strings.Contains(err.Error(), "measurement_id") ||
			strings.Contains(err.Error(), "g-lower") {
			t.Errorf("%s: err = %v", connection, err)
		}
		if *env.reads != 0 || len(env.fake.recorded()) != 0 {
			t.Errorf("%s: secret read or request sent", connection)
		}
	}
	env := newEnvironment(t, &fakeGoogle{})
	if _, err := env.invoke("bound", oneEvent(""), true); err == nil || len(env.fake.recorded()) != 0 {
		t.Errorf("a connection with a target was served: %v", err)
	}
}

func TestConfirmationIsRequired(t *testing.T) {
	env := newEnvironment(t, &fakeGoogle{})
	_, err := env.invoke("listed", oneEvent(""), false)
	if err == nil || !strings.Contains(err.Error(), "requires confirmation") {
		t.Fatalf("err = %v", err)
	}
	if len(env.fake.recorded()) != 0 || *env.reads != 0 {
		t.Error("an unconfirmed call did I/O")
	}
}

func TestToolIsInvisibleAndUncallableWithoutAToolsEntry(t *testing.T) {
	env := newEnvironment(t, &fakeGoogle{})
	if _, err := env.invoke("unlisted", oneEvent(""), true); err == nil ||
		!strings.Contains(err.Error(), "does not offer tool") {
		t.Errorf("invoke without a tools entry: %v", err)
	}
	// Even with the create effect permitted, only a tools entry offers the tool.
	if _, err := env.invoke("permitted", oneEvent(""), true); err == nil ||
		!strings.Contains(err.Error(), "requires-tool-allow-list") {
		t.Errorf("invoke with the effect but no tools entry: %v", err)
	}
	if len(env.fake.recorded()) != 0 || *env.reads != 0 {
		t.Error("an unlisted call did I/O")
	}
	offers := func(connection string) bool {
		response, err := env.core.Tools(application.SearchRequest{Connection: connection})
		if err != nil {
			t.Fatal(err)
		}
		for _, hit := range response.Tools {
			if hit.ID == eventsSendID {
				return true
			}
		}
		return false
	}
	if offers("unlisted") || offers("permitted") || !offers("listed") {
		t.Errorf("visibility: unlisted=%v listed=%v", offers("unlisted"), offers("listed"))
	}
}

func TestServerErrorsAndTimeoutsAreFailuresWithoutTheSecret(t *testing.T) {
	for _, status := range []int{500, 503} {
		env := newEnvironment(t, &fakeGoogle{status: status})
		_, err := env.invoke("listed", oneEvent(""), true)
		if err == nil || !strings.Contains(err.Error(), "may have reached") ||
			strings.Contains(err.Error(), secretValue) || strings.Contains(err.Error(), streamID) {
			t.Errorf("status %d: err = %v", status, err)
		}
		if len(env.fake.recorded()) != 1 {
			t.Errorf("status %d: the request was repeated", status)
		}
	}
	// A transport error that quotes the full URL, as Go's url.Error does.
	quoting := &url.Error{Op: "Post", URL: apiRoot + collectPath + "?measurement_id=" + streamID + "&api_secret=" + secretValue,
		Err: context.DeadlineExceeded}
	for name, cause := range map[string]error{"timeout": quoting, "reset": &url.Error{Op: "Post", URL: quoting.URL,
		Err: errors.New("read: connection reset by peer " + secretValue)}} {
		env := newCore(t, &fakeGoogle{})
		failWith(t, cause)
		_, err := env.invoke("listed", oneEvent(""), true)
		if err == nil || strings.Contains(err.Error(), secretValue) || strings.Contains(err.Error(), "api_secret") ||
			strings.Contains(err.Error(), streamID) {
			t.Errorf("%s: err = %v", name, err)
		}
		if name == "timeout" && classOf(err) != provider.ClassTimeout {
			t.Errorf("timeout class = %q", classOf(err))
		}
	}
}

func TestRedactorMasksTheSecret(t *testing.T) {
	env := newEnvironment(t, &fakeGoogle{})
	if _, err := env.invoke("listed", oneEvent(""), true); err != nil {
		t.Fatal(err)
	}
	if got := env.red.Apply("leaked " + secretValue); strings.Contains(got, secretValue) {
		t.Errorf("redactor leaves the secret: %q", got)
	}
}

func TestConnectionTestSendsNothing(t *testing.T) {
	f := &fakeGoogle{}
	serve(t, f)
	for name, tc := range map[string]struct {
		resolved *config.Resolved
		value    string
		class    provider.Class
	}{
		"ok":           {resolvedFor(streamID), secretValue, provider.ClassOK},
		"bad id":       {resolvedFor("G-"), secretValue, provider.ClassProviderError},
		"no secret":    {resolvedFor(streamID), "", ""},
		"short secret": {resolvedFor(streamID), "short", provider.ClassAuth},
		"foreign host": {&config.Resolved{BaseURL: "https://example.com", Options: map[string]string{optionMeasurementID: streamID}}, secretValue, provider.ClassProviderError},
	} {
		red := &redact.Redactor{}
		class, err := TestConnection(context.Background(), tc.resolved,
			secret.NewWith(func(string) string { return tc.value }, nil, nil, red), red)
		if tc.class == "" {
			if err == nil && class == provider.ClassOK {
				t.Errorf("%s: passed without a secret", name)
			}
			continue
		}
		if err != nil || class != tc.class {
			t.Errorf("%s: class = %q, err = %v, want %q", name, class, err, tc.class)
		}
	}
	if len(f.recorded()) != 0 {
		t.Errorf("the connection test sent %d requests", len(f.recorded()))
	}
	// With no transport server at all, a pass still needs no network.
	failWith(t, errors.New("network used"))
	if class, err := TestConnection(context.Background(), resolvedFor(streamID),
		secret.NewWith(func(string) string { return secretValue }, nil, nil, nil), nil); err != nil || class != provider.ClassOK {
		t.Errorf("class = %q, err = %v", class, err)
	}
}

func resolvedFor(id string) *config.Resolved {
	return &config.Resolved{BaseURL: apiRoot, Credential: "key", Secrets: coreConfig().Credentials["key"],
		Options: map[string]string{optionMeasurementID: id}}
}

func TestBaseURLIsFixed(t *testing.T) {
	if _, err := checkConnection(&config.Resolved{BaseURL: "https://www.google-analytics.com.evil.example",
		Options: map[string]string{optionMeasurementID: streamID}}); err == nil {
		t.Error("a foreign origin was accepted")
	}
	reg := registry(t)
	metadata, _ := reg.ProviderMetadata(Provider)
	if metadata.ValidateBaseURL("https://example.com") == nil || metadata.ValidateBaseURL(apiRoot+"/") != nil {
		t.Error("base URL validation")
	}
}
