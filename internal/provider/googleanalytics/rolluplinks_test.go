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

// readerEnv adds connections that list the given tools, with the bindings of the stream tests.
func readerEnv(t *testing.T, fake *fakeGoogle, tools ...string) *testEnv {
	t.Helper()
	env := newEnv(t, fake)
	for name, targets := range map[string][]string{"sfree": nil, "saccount": {"accounts/100"},
		"sproperty": {"properties/200"}, "smixed": {"accounts/100", "properties/901"}} {
		env.cfg.Connections[name] = config.Connection{Service: "ga", Credential: "login", Targets: targets, Tools: tools}
	}
	if err := env.cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	return env
}

func rollupEnv(t *testing.T, fake *fakeGoogle) *testEnv {
	return readerEnv(t, fake, rollupLinksListID, rollupLinksGetID)
}

const (
	rollupOneJSON   = `{"name":"properties/200/rollupPropertySourceLinks/4001","sourceProperty":"properties/301","extra":1}`
	rollupTwoJSON   = `{"name":"properties/200/rollupPropertySourceLinks/4002","sourceProperty":"properties/302"}`
	rollupThreeJSON = `{"name":"properties/200/rollupPropertySourceLinks/4003","sourceProperty":"properties/303"}`
)

type rollupLinksOut struct {
	Links      []rollupLink `json:"rollupPropertySourceLinks"`
	NextCursor string       `json:"next_cursor"`
	HasMore    bool         `json:"has_more"`
}

func TestRollupLinksListPagesThroughThreePages(t *testing.T) {
	pages := []string{
		`{"rollupPropertySourceLinks":[` + rollupOneJSON + `],"nextPageToken":"t2"}`,
		`{"rollupPropertySourceLinks":[` + rollupTwoJSON + `],"nextPageToken":"t3"}`,
		`{"rollupPropertySourceLinks":[` + rollupThreeJSON + `]}`,
	}
	env := rollupEnv(t, &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
		return respond(r, http.StatusOK, "application/json", pages[n-1]), nil
	}})
	cursor, seen := "", []string{}
	for i := 0; i < 3; i++ {
		args := `{"property":"properties/200","page_size":1`
		if cursor != "" {
			args += `,"cursor":"` + cursor + `"`
		}
		result, err := env.call(rollupLinksListID, "sfree", args+`}`)
		var out rollupLinksOut
		if err != nil || json.Unmarshal([]byte(result), &out) != nil || len(out.Links) != 1 ||
			out.HasMore != (i < 2) || strings.Contains(result, "extra") {
			t.Fatalf("page %d = %s, %v", i, result, err)
		}
		seen = append(seen, string(out.Links[0].Name)+" "+string(out.Links[0].SourceProperty))
		cursor = out.NextCursor
	}
	requests, _ := env.fake.seen()
	if !slices.Equal(seen, []string{"properties/200/rollupPropertySourceLinks/4001 properties/301",
		"properties/200/rollupPropertySourceLinks/4002 properties/302",
		"properties/200/rollupPropertySourceLinks/4003 properties/303"}) ||
		len(requests) != 3 || requests[0].method != http.MethodGet ||
		requests[0].path != "/v1alpha/properties/200/rollupPropertySourceLinks" ||
		requests[0].rawQuery != "pageSize=1" || !strings.Contains(requests[1].rawQuery, "pageToken=t2") ||
		!strings.Contains(requests[2].rawQuery, "pageToken=t3") {
		t.Errorf("seen %v, requests %+v", seen, requests)
	}
	if cursor != "" {
		t.Errorf("last page has a cursor %q", cursor)
	}
	if _, err := env.call(rollupLinksListID, "sfree", `{"property":"properties/201","cursor":"x"}`); !refusedInvalid(err) {
		t.Errorf("a cursor of another property: %v", err)
	}
}

func TestRollupLinksEmptyListAndGet(t *testing.T) {
	env := rollupEnv(t, &fakeGoogle{api: always(http.StatusOK, `{}`)})
	result, err := env.call(rollupLinksListID, "sfree", `{"property":"properties/200"}`)
	// Empty members are left out of a result.
	if err != nil || result != `{"has_more":false}` {
		t.Errorf("empty list = %s, %v", result, err)
	}
	env = rollupEnv(t, &fakeGoogle{api: always(http.StatusOK, rollupOneJSON)})
	result, err = env.call(rollupLinksGetID, "sfree", `{"name":"properties/200/rollupPropertySourceLinks/4001"}`)
	requests, _ := env.fake.seen()
	var link rollupLink
	if err != nil || json.Unmarshal([]byte(result), &link) != nil ||
		link != (rollupLink{Name: "properties/200/rollupPropertySourceLinks/4001", SourceProperty: "properties/301"}) ||
		strings.Contains(result, "extra") ||
		len(requests) != 1 || requests[0].path != "/v1alpha/properties/200/rollupPropertySourceLinks/4001" {
		t.Errorf("get = %s, %v, %+v", result, err, requests)
	}
	env = rollupEnv(t, &fakeGoogle{api: always(http.StatusOK, rollupTwoJSON)})
	if _, err := env.call(rollupLinksGetID, "sfree", `{"name":"properties/200/rollupPropertySourceLinks/4001"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("an answer for another link: %v", err)
	}
}

func TestRollupLinksAnswerIsChecked(t *testing.T) {
	env := rollupEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"rollupPropertySourceLinks":[`+
		`{"name":"properties/200/rollupPropertySourceLinks/1","sourceProperty":"accounts/5"},`+
		`{"name":"properties/999/rollupPropertySourceLinks/2","sourceProperty":"properties/7"},`+
		`{"name":"properties/200/rollupPropertySourceLinks/x","sourceProperty":"properties/7"},{"name":"nonsense"}]}`)})
	result, err := env.call(rollupLinksListID, "sfree", `{"property":"properties/200"}`)
	var out rollupLinksOut
	if err != nil || json.Unmarshal([]byte(result), &out) != nil || len(out.Links) != 1 || out.Links[0].SourceProperty != "" {
		t.Errorf("result = %s, %v", result, err)
	}
}

func TestRollupLinksFollowTheBindingBeforeAnyIO(t *testing.T) {
	for _, tool := range []struct{ id, arguments string }{
		{rollupLinksListID, `{"property":"PROP"}`},
		{rollupLinksGetID, `{"name":"PROP/rollupPropertySourceLinks/4001"}`},
	} {
		runBindingCases(t, tool.id, tool.arguments, rollupEnv)
	}
}

// runBindingCases runs a tool with a property parent on every connection of the stream binding cases.
func runBindingCases(t *testing.T, id, arguments string, newEnv func(*testing.T, *fakeGoogle) *testEnv) {
	t.Helper()
	for _, c := range streamBindingCases {
		answers := map[string]string{
			"/v1beta/properties/200": `{"name":"properties/200","parent":"accounts/100"}`,
			"/v1beta/properties/201": `{"name":"properties/201","parent":"accounts/100"}`,
			"/v1beta/properties/901": `{"name":"properties/901","parent":"accounts/900"}`,
		}
		env := newEnv(t, &fakeGoogle{api: func(_ int, r *http.Request) (*http.Response, error) {
			body, ok := answers[r.URL.Path]
			if !ok {
				body = `{"name":"` + strings.TrimPrefix(r.URL.Path, "/v1alpha/") + `"}`
			}
			return respond(r, http.StatusOK, "application/json", body), nil
		}})
		result, err := env.call(id, c.connection, strings.ReplaceAll(arguments, "PROP", c.property))
		requests, _ := env.fake.seen()
		want := slices.Clone(c.methods)
		if i := slices.Index(want, "X"); i >= 0 {
			want[i] = "GET"
		}
		if (err != nil) != c.refused || !slices.Equal(methodsOf(requests), want) {
			t.Errorf("%s %s %s: %v, requests %v, want %v", id, c.connection, c.property, err, methodsOf(requests), want)
		}
		if c.refused && (!refusedInvalid(err) || strings.Contains(err.Error(), "901") || strings.Contains(result, "901")) {
			t.Errorf("%s %s %s: refusal %v", id, c.connection, c.property, err)
		}
		if c.refused && len(c.methods) == 0 {
			env.noIO(t)
		}
	}
}

// errorCases are the failures every reading tool of this group maps by status.
func runErrorCases(t *testing.T, newEnv func(*testing.T, *fakeGoogle) *testEnv, calls map[string]string) {
	t.Helper()
	for _, c := range []struct {
		name  string
		api   func(int, *http.Request) (*http.Response, error)
		class provider.Class
	}{
		{"403", always(http.StatusForbidden, errorObject(403, "PERMISSION_DENIED", "ACCESS_TOKEN_SCOPE_INSUFFICIENT")),
			provider.ClassPermission},
		{"404", always(http.StatusNotFound, errorObject(404, "NOT_FOUND", "")), provider.ClassNotFound},
		{"429", always(http.StatusTooManyRequests, errorObject(429, "RESOURCE_EXHAUSTED", "")), provider.ClassRateLimited},
	} {
		for id, arguments := range calls {
			env := newEnv(t, &fakeGoogle{api: c.api})
			_, err := env.call(id, "sfree", arguments)
			if classOf(err) != c.class || leaks(err.Error()) != "" {
				t.Errorf("%s %s: error = %v", c.name, id, err)
			}
			if requests, _ := env.fake.seen(); len(requests) != 1 {
				t.Errorf("%s %s: requests = %d, want exactly one", c.name, id, len(requests))
			}
		}
	}
}

func TestRollupLinkErrorsAreClassified(t *testing.T) {
	runErrorCases(t, rollupEnv, map[string]string{
		rollupLinksListID: `{"property":"properties/200"}`,
		rollupLinksGetID:  `{"name":"properties/200/rollupPropertySourceLinks/4001"}`,
	})
}

func TestRollupLink429WithoutRetryAfterHoldsTheNextRequest(t *testing.T) {
	env := rollupEnv(t, &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
		if n == 1 {
			return respond(r, http.StatusTooManyRequests, "application/json", errorObject(429, "RESOURCE_EXHAUSTED", "")), nil
		}
		return respond(r, http.StatusOK, "application/json", rollupOneJSON), nil
	}})
	const args = `{"name":"properties/200/rollupPropertySourceLinks/4001"}`
	if _, err := env.call(rollupLinksGetID, "sfree", args); classOf(err) != provider.ClassRateLimited {
		t.Fatalf("error = %v", err)
	}
	if _, err := env.call(rollupLinksGetID, "sfree", args); err != nil {
		t.Fatal(err)
	}
	if requests, _ := env.fake.seen(); len(requests) != 2 {
		t.Errorf("requests = %d, want 2 without a retry", len(requests))
	}
	if waits := *env.waits; len(waits) == 0 || slices.Max(waits) < 900_000_000 {
		t.Errorf("waits = %v, want the next request held for about a second", waits)
	}
}

func TestRollupLinkArgumentsAreRefusedBeforeAnyIO(t *testing.T) {
	env := rollupEnv(t, &fakeGoogle{api: always(http.StatusOK, `{}`)})
	for id, list := range map[string][]string{
		rollupLinksListID: {`{}`, `{"property":"accounts/100"}`, `{"property":"properties/0200"}`,
			`{"property":"properties/200","page_size":201}`, `{"property":"properties/200","page_size":0}`,
			`{"property":"properties/200","filter":"x"}`},
		rollupLinksGetID: {`{}`, `{"name":"properties/200"}`, `{"name":"properties/200/rollupPropertySourceLinks/x"}`,
			`{"name":"properties/200/rollupPropertySourceLinks/1/x"}`, `{"name":"properties/200/accessBindings/1"}`},
	} {
		for _, args := range list {
			if _, err := env.call(id, "sfree", args); err == nil {
				t.Errorf("%s %s: accepted", id, args)
			}
		}
	}
	env.noIO(t)
}

func TestRollupLinkToolsAreReadOnlyAlphaTools(t *testing.T) {
	reg := registry(t)
	for _, id := range []string{rollupLinksListID, rollupLinksGetID} {
		descriptor, _, ok := reg.Lookup(id)
		if !ok || descriptor.RequiresToolAllowList || descriptor.Risk != readRisk ||
			!strings.Contains(descriptor.Description, "ALPHA") || !strings.Contains(descriptor.Description, "untrusted") {
			t.Errorf("%s: %+v", id, descriptor)
		}
	}
}
