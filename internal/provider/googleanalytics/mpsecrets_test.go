package googleanalytics

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// sampleSecret is a synthetic value that stands for a measurement protocol secret.
const sampleSecret = "test-secret-value-redacted"

const (
	streamPath = "properties/200/dataStreams/2001"
	secretPath = streamPath + "/measurementProtocolSecrets/3001"
)

func secretJSON(id, display string) string {
	return `{"name":"` + streamPath + `/measurementProtocolSecrets/` + id + `","displayName":"` + display +
		`","secretValue":"` + sampleSecret + `","extra":1}`
}

// secretEnv adds connections that list the four tools of this group, bound like the stream connections.
func secretEnv(t *testing.T, fake *fakeGoogle) *testEnv {
	t.Helper()
	env := newEnv(t, fake)
	tools := []string{dataStreamsDeleteID, mpSecretsListID, mpSecretsGetID, mpSecretsDeleteID}
	for name, targets := range map[string][]string{"sfree": nil, "saccount": {"accounts/100"},
		"sproperty": {"properties/200"}, "smixed": {"accounts/100", "properties/901"}} {
		env.cfg.Connections[name] = config.Connection{Service: "ga", Credential: "login", Targets: targets, Tools: tools}
	}
	if err := env.cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	return env
}

// hidden fails when the secret value shows up in text or is not masked by the redactor.
func hidden(t *testing.T, what, text string, red *redact.Redactor) {
	t.Helper()
	if strings.Contains(text, sampleSecret) || strings.Contains(strings.ToLower(text), "secretvalue") {
		t.Errorf("%s carries the secret value: %s", what, text)
	}
	if got := red.Apply("x " + sampleSecret + " y"); strings.Contains(got, sampleSecret) {
		t.Errorf("the redactor does not mask the secret value: %s", got)
	}
}

func TestSecretsListPagesThroughThreePagesWithoutTheSecretValue(t *testing.T) {
	pages := []string{
		`{"measurementProtocolSecrets":[` + secretJSON("3001", "First") + `],"nextPageToken":"t2"}`,
		`{"measurementProtocolSecrets":[` + secretJSON("3002", "Second") + `],"nextPageToken":"t3"}`,
		`{"measurementProtocolSecrets":[` + secretJSON("3003", "Third") + `]}`,
	}
	env := secretEnv(t, &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
		return respond(r, http.StatusOK, "application/json", pages[n-1]), nil
	}})
	cursor, names := "", []string{}
	for i := 0; i < 3; i++ {
		args := `{"parent":"` + streamPath + `","page_size":1`
		if cursor != "" {
			args += `,"cursor":"` + cursor + `"`
		}
		result, err := env.call(mpSecretsListID, "sfree", args+`}`)
		if err != nil {
			t.Fatal(err)
		}
		hidden(t, "list result", result, env.red)
		var out secretsResult
		if err := json.Unmarshal([]byte(result), &out); err != nil || len(out.Secrets) != 1 || out.HasMore != (i < 2) ||
			out.Secrets[0].DisplayName == "" {
			t.Fatalf("page %d = %s, %v", i, result, err)
		}
		names = append(names, string(out.Secrets[0].Name))
		cursor = out.NextCursor
	}
	requests, _ := env.fake.seen()
	if len(names) != 3 || len(requests) != 3 || requests[0].method != http.MethodGet ||
		requests[0].path != "/v1beta/"+streamPath+"/measurementProtocolSecrets" || requests[0].rawQuery != "pageSize=1" ||
		!strings.Contains(requests[1].rawQuery, "pageToken=t2") || !strings.Contains(requests[2].rawQuery, "pageToken=t3") {
		t.Errorf("names %v, requests %+v", names, requests)
	}
	if _, err := env.call(mpSecretsListID, "sfree", `{"parent":"properties/200/dataStreams/9","cursor":"`+cursor+`"}`); err == nil {
		t.Error("a cursor of another stream was accepted")
	}
}

func TestSecretsListKeepsOnlySecretsOfTheStreamAndStillMasksTheirValues(t *testing.T) {
	const other = "other-test-secret-value-redacted"
	env := secretEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"measurementProtocolSecrets":[`+
		secretJSON("3001", strings.Repeat("n", 400))+`,`+
		`{"name":"properties/200/dataStreams/9/measurementProtocolSecrets/1","displayName":"Foreign","secretValue":"`+other+`"},`+
		`{"name":"nonsense","secretValue":"`+other+`"}]}`)})
	result, err := env.call(mpSecretsListID, "sfree", `{"parent":"`+streamPath+`"}`)
	var out secretsResult
	if err != nil || json.Unmarshal([]byte(result), &out) != nil || len(out.Secrets) != 1 ||
		len([]rune(string(out.Secrets[0].DisplayName))) != maxTextRunes+1 || strings.Contains(result, other) {
		t.Fatalf("result %s, %v", result, err)
	}
	hidden(t, "list result", result, env.red)
	if strings.Contains(env.red.Apply(other), other) {
		t.Error("the value of a dropped entry is not masked")
	}
}

func TestSecretsGetReturnsNameAndDisplayNameOnly(t *testing.T) {
	env := secretEnv(t, &fakeGoogle{api: always(http.StatusOK, secretJSON("3001", "Server events"))})
	result, err := env.call(mpSecretsGetID, "sfree", `{"name":"`+secretPath+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	hidden(t, "get result", result, env.red)
	var raw map[string]any
	if json.Unmarshal([]byte(result), &raw) != nil || len(raw) != 2 || raw["name"] != secretPath ||
		raw["displayName"] != "Server events" {
		t.Errorf("result = %s", result)
	}
	requests, _ := env.fake.seen()
	if len(requests) != 1 || requests[0].method != http.MethodGet || requests[0].path != "/v1beta/"+secretPath {
		t.Errorf("requests = %+v", requests)
	}
	// An answer for another secret is not trusted, and its value is masked all the same.
	env = secretEnv(t, &fakeGoogle{api: always(http.StatusOK, secretJSON("3002", "Other"))})
	result, err = env.call(mpSecretsGetID, "sfree", `{"name":"`+secretPath+`"}`)
	if classOf(err) != provider.ClassInvalidResponse {
		t.Fatalf("error = %v", err)
	}
	hidden(t, "error", err.Error()+result, env.red)
}

func TestSecretDeletesNeedConfirmationAndSendOneDelete(t *testing.T) {
	for _, c := range []struct{ id, args, path string }{
		{dataStreamsDeleteID, `{"name":"` + streamPath + `"}`, streamPath},
		{mpSecretsDeleteID, `{"name":"` + secretPath + `"}`, secretPath},
	} {
		env := secretEnv(t, &fakeGoogle{api: always(http.StatusOK, `{}`)})
		if _, err := env.call(c.id, "sfree", c.args); err == nil {
			t.Fatalf("%s without confirm was accepted", c.id)
		}
		env.noIO(t)
		for _, bad := range []string{`{}`, `{"name":"properties/200"}`, `{"name":"` + streamPath + `/x"}`,
			`{"name":"` + c.path + `","force":true}`, `{"name":"properties/0/dataStreams/1"}`} {
			if _, err := env.confirmed(c.id, "sfree", bad); err == nil {
				t.Errorf("%s accepted %s", c.id, bad)
			}
		}
		env.noIO(t)
		result, err := env.confirmed(c.id, "sfree", c.args)
		requests, _ := env.fake.seen()
		if err != nil || len(requests) != 1 || requests[0].method != http.MethodDelete ||
			requests[0].path != "/v1beta/"+c.path || requests[0].rawQuery != "" || requests[0].body != "" ||
			requests[0].contentType != "" {
			t.Fatalf("%s: %v, %+v", c.id, err, requests)
		}
		var out deleteTargetResult
		if json.Unmarshal([]byte(result), &out) != nil || out.Name != c.path || !out.Deleted {
			t.Errorf("%s result = %s", c.id, result)
		}
	}
}

func TestEntityDeletesAreReachableThroughEffects(t *testing.T) {
	// The write connection holds every effect: neither delete of a single entity is listed-only.
	for _, c := range []struct{ id, name string }{{dataStreamsDeleteID, streamPath}, {mpSecretsDeleteID, secretPath}} {
		env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, `{}`)})
		if _, err := env.confirmed(c.id, "write", `{"name":"`+c.name+`"}`); err != nil {
			t.Errorf("%s is not reachable through effects: %v", c.id, err)
		}
	}
}

func TestSecretToolsFollowTheBindingBeforeAnyIO(t *testing.T) {
	for _, tool := range []struct{ id, arguments, method string }{
		{mpSecretsListID, `{"parent":"PROP/dataStreams/2001"}`, "GET"},
		{mpSecretsGetID, `{"name":"PROP/dataStreams/2001/measurementProtocolSecrets/3001"}`, "GET"},
		{mpSecretsDeleteID, `{"name":"PROP/dataStreams/2001/measurementProtocolSecrets/3001"}`, "DELETE"},
		{dataStreamsDeleteID, `{"name":"PROP/dataStreams/2001"}`, "DELETE"},
	} {
		for _, c := range streamBindingCases {
			answers := map[string]string{
				"/v1beta/properties/200": `{"name":"properties/200","parent":"accounts/100"}`,
				"/v1beta/properties/201": `{"name":"properties/201","parent":"accounts/100"}`,
				"/v1beta/properties/901": `{"name":"properties/901","parent":"accounts/900"}`,
			}
			env := secretEnv(t, &fakeGoogle{api: func(_ int, r *http.Request) (*http.Response, error) {
				body, ok := answers[r.URL.Path]
				if !ok {
					body = `{"measurementProtocolSecrets":[],"name":"` + strings.TrimPrefix(r.URL.Path, "/v1beta/") + `"}`
				}
				return respond(r, http.StatusOK, "application/json", body), nil
			}})
			result, err := env.confirmed(tool.id, c.connection, strings.ReplaceAll(tool.arguments, "PROP", c.property))
			requests, _ := env.fake.seen()
			want := slices.Clone(c.methods)
			if i := slices.Index(want, "X"); i >= 0 {
				want[i] = tool.method
			}
			if (err != nil) != c.refused || !slices.Equal(methodsOf(requests), want) {
				t.Errorf("%s %s %s: %v, requests %v, want %v", tool.id, c.connection, c.property, err,
					methodsOf(requests), want)
			}
			if c.refused && (!refusedInvalid(err) || strings.Contains(err.Error(), "901") || strings.Contains(result, "901")) {
				t.Errorf("%s %s %s: refusal %v", tool.id, c.connection, c.property, err)
			}
			if c.refused && len(c.methods) == 0 {
				env.noIO(t)
			}
		}
	}
}

func TestSecretToolErrorsAreClassifiedAndNeverCarryTheSecretValue(t *testing.T) {
	calls := []struct {
		id, arguments string
		change        bool
	}{
		{mpSecretsListID, `{"parent":"` + streamPath + `"}`, false},
		{mpSecretsGetID, `{"name":"` + secretPath + `"}`, false},
		{mpSecretsDeleteID, `{"name":"` + secretPath + `"}`, true},
		{dataStreamsDeleteID, `{"name":"` + streamPath + `"}`, true},
	}
	// Google echoes the value in the body of a failure; it must not reach the error.
	echo := func(status int, body string) func(int, *http.Request) (*http.Response, error) {
		return always(status, strings.Replace(body, `"message"`, `"secretValue":"`+sampleSecret+`","message"`, 1))
	}
	for _, c := range []struct {
		name   string
		api    func(int, *http.Request) (*http.Response, error)
		class  provider.Class
		unsure bool
	}{
		{"403", echo(http.StatusForbidden, errorObject(403, "PERMISSION_DENIED", "ACCESS_TOKEN_SCOPE_INSUFFICIENT")),
			provider.ClassPermission, false},
		{"404", echo(http.StatusNotFound, errorObject(404, "NOT_FOUND", "")), provider.ClassNotFound, false},
		{"429", echo(http.StatusTooManyRequests, errorObject(429, "RESOURCE_EXHAUSTED", "")), provider.ClassRateLimited, false},
		{"500", echo(http.StatusInternalServerError, errorObject(500, "INTERNAL", "")), provider.ClassProviderError, true},
	} {
		for _, call := range calls {
			t.Run(c.name+" "+call.id, func(t *testing.T) {
				env := secretEnv(t, &fakeGoogle{api: c.api})
				_, err := env.confirmed(call.id, "sfree", call.arguments)
				if classOf(err) != c.class || leaks(err.Error()) != "" || strings.Contains(err.Error(), sampleSecret) {
					t.Fatalf("error = %v", err)
				}
				if got := strings.Contains(err.Error(), "outcome of the change is unknown"); got != (c.unsure && call.change) {
					t.Errorf("unknown outcome reported = %t: %v", got, err)
				}
				if requests, _ := env.fake.seen(); len(requests) != 1 {
					t.Errorf("requests = %d, want exactly one", len(requests))
				}
			})
		}
	}
}

func TestSecret429WithoutRetryAfterHoldsTheNextRequest(t *testing.T) {
	env := secretEnv(t, &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
		if n == 1 {
			return respond(r, http.StatusTooManyRequests, "application/json", errorObject(429, "RESOURCE_EXHAUSTED", "")), nil
		}
		return respond(r, http.StatusOK, "application/json", secretJSON("3001", "S")), nil
	}})
	args := `{"name":"` + secretPath + `"}`
	if _, err := env.call(mpSecretsGetID, "sfree", args); classOf(err) != provider.ClassRateLimited {
		t.Fatalf("error = %v", err)
	}
	if _, err := env.call(mpSecretsGetID, "sfree", args); err != nil {
		t.Fatal(err)
	}
	if requests, _ := env.fake.seen(); len(requests) != 2 {
		t.Errorf("requests = %d, want 2 without a retry", len(requests))
	}
	if waits := *env.waits; len(waits) == 0 || slices.Max(waits) < 900_000_000 {
		t.Errorf("waits = %v, want the next request held for about a second", waits)
	}
}

func TestSecretValueNeverReachesAnySchemaOrToolOfTheGroup(t *testing.T) {
	reg := registry(t)
	metadata, _ := reg.ProviderMetadata(Provider)
	for _, d := range reg.Provider(Provider) {
		id := strings.ToLower(d.ID)
		if strings.Contains(id, "measurement-protocol-secrets") && (strings.HasSuffix(id, ".create") ||
			strings.HasSuffix(id, ".update") || strings.HasSuffix(id, ".patch")) {
			t.Errorf("%s changes or creates a measurement protocol secret", d.ID)
		}
		if strings.Contains(strings.ToLower(string(d.OutputSchema)), "secretvalue") {
			t.Errorf("%s output schema names the secret value", d.ID)
		}
		for _, field := range d.Fields {
			if field.Name == "secretValue" {
				t.Errorf("%s lists the secret value as a field", d.ID)
			}
		}
	}
	got := []string{}
	for _, tool := range metadata.Tools {
		if strings.Contains(tool.ID, "measurement-protocol-secrets") {
			got = append(got, tool.ID)
		}
	}
	slices.Sort(got)
	if !slices.Equal(got, []string{mpSecretsDeleteID, mpSecretsGetID, mpSecretsListID}) {
		t.Errorf("secret tools = %v", got)
	}
}

func TestSecretToolsAreRegisteredWithScopesRisksAndSummaries(t *testing.T) {
	reg := registry(t)
	metadata, _ := reg.ProviderMetadata(Provider)
	for id, want := range map[string]string{mpSecretsListID: scopeReadOnly, mpSecretsGetID: scopeReadOnly,
		mpSecretsDeleteID: scopeEdit, dataStreamsDeleteID: scopeEdit} {
		if !slices.Equal(metadata.Token.Scopes[id], []string{want}) {
			t.Errorf("%s scopes = %v", id, metadata.Token.Scopes[id])
		}
	}
	for _, d := range reg.Provider(Provider) {
		switch d.ID {
		case mpSecretsDeleteID, dataStreamsDeleteID:
			if string(d.Risk.Effect) != "delete" || d.Risk.Confirmation != "required" ||
				d.RequiresToolAllowList || reg.ConfirmationSummary(d.ID) == nil {
				t.Errorf("%s = %+v, listed %t", d.ID, d.Risk, d.RequiresToolAllowList)
			}
		case mpSecretsListID, mpSecretsGetID:
			if d.Risk != readRisk || d.RequiresToolAllowList {
				t.Errorf("%s = %+v", d.ID, d.Risk)
			}
		}
	}
	for _, profile := range metadata.Profiles {
		for _, id := range []string{mpSecretsDeleteID, dataStreamsDeleteID} {
			if slices.Contains(profile.Tools, id) != (profile.ID == "write") {
				t.Errorf("profile %s and %s do not match", profile.ID, id)
			}
		}
	}
	if got := reg.ConfirmationSummary(mpSecretsDeleteID)(json.RawMessage(`{"name":"` + secretPath + `"}`)); !strings.Contains(got, secretPath) {
		t.Errorf("summary = %q", got)
	}
	if got := reg.ConfirmationSummary(dataStreamsDeleteID)(json.RawMessage(`{"name":"` + streamPath + `"}`)); !strings.Contains(got, streamPath) {
		t.Errorf("summary = %q", got)
	}
}
