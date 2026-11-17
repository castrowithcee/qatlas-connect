package googleanalytics

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// exportEnv adds the write connections of the audience export tests to the environment.
func exportEnv(t *testing.T, fake *fakeGoogle) *testEnv {
	t.Helper()
	env := newEnv(t, fake)
	changing := []config.Effect{config.EffectRead, config.EffectCreate}
	env.cfg.Connections["xwrite"] = config.Connection{Service: "ga", Credential: "login", Effects: changing}
	env.cfg.Connections["xproperty"] = config.Connection{Service: "ga", Credential: "login", Effects: changing,
		Targets: []string{"properties/200"}}
	env.cfg.Connections["xaccount"] = config.Connection{Service: "ga", Credential: "login", Effects: changing,
		Targets: []string{"accounts/100"}}
	if err := env.cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	return env
}

const createArguments = `{"property":"properties/200","audience":"properties/200/audiences/300",` +
	`"dimensions":[{"dimension_name":"deviceId"},{"dimension_name":"userId"}]}`

const operationJSON = `{"name":"properties/200/audienceExports/400","metadata":{"@type":"type.googleapis.com/` +
	`google.analytics.data.v1beta.AudienceExportMetadata"}}`

// exportJSON is an export in the given state, with members outside the v1beta resource that must never be returned.
func exportJSON(id int, state string) string {
	extra := ""
	switch state {
	case "ACTIVE":
		extra = `,"percentageCompleted":100,"rowCount":1200,"creationQuotaTokensCharged":7`
	case "FAILED":
		extra = `,"percentageCompleted":12.5,"errorMessage":"quota exhausted","creationQuotaTokensCharged":3`
	default:
		extra = `,"percentageCompleted":0`
	}
	return fmt.Sprintf(`{"name":"properties/200/audienceExports/%d","audience":"properties/200/audiences/300",`+
		`"audienceDisplayName":"Purchasers","dimensions":[{"dimensionName":"deviceId"}],"state":"%s",`+
		`"beginCreatingTime":"2026-11-16T10:00:00.123456789Z","recurringAudienceList":"properties/200/recurringAudienceExports/9",`+
		`"webhookNotification":{"uri":"https://hook.invalid/x","channelToken":"channel-canary-3d"}%s}`, id, state, extra)
}

func TestAudienceExportsCreateNeedsTheConfirmationAndSendsOneRequest(t *testing.T) {
	freshQuotas(t)
	env := exportEnv(t, &fakeGoogle{api: always(http.StatusOK, operationJSON)})
	if _, err := env.call(audienceCreateID, "xwrite", createArguments); err == nil {
		t.Fatal("create without confirm was accepted")
	}
	env.noIO(t)
	result, err := env.confirmed(audienceCreateID, "xwrite", createArguments)
	if err != nil {
		t.Fatal(err)
	}
	requests, _ := env.fake.seen()
	if len(requests) != 1 {
		t.Fatalf("requests = %+v", requests)
	}
	r := requests[0]
	if r.method != http.MethodPost || r.host != "analyticsdata.googleapis.com" ||
		r.path != "/v1beta/properties/200/audienceExports" || r.rawQuery != "" || r.contentType != "application/json" ||
		r.auth != "Bearer "+accessPrefix+"1" {
		t.Fatalf("request = %+v", r)
	}
	if r.body != `{"audience":"properties/200/audiences/300","dimensions":[{"dimensionName":"deviceId"},`+
		`{"dimensionName":"userId"}]}` {
		t.Errorf("body = %s", r.body)
	}
	// The answer is a running operation; Qatlas neither waits nor asks again.
	if result != `{"done":false,"name":"properties/200/audienceExports/400"}` {
		t.Errorf("result = %s", result)
	}
	if after, _ := env.fake.seen(); len(after) != 1 {
		t.Errorf("requests = %d", len(after))
	}
}

func TestAudienceExportsCreateReturnsAnOperationThatAlreadyHoldsAnOutcome(t *testing.T) {
	freshQuotas(t)
	answer := `{"name":"properties/200/audienceExports/400","done":true,"response":` + exportJSON(400, "ACTIVE") + `}`
	env := exportEnv(t, &fakeGoogle{api: always(http.StatusOK, answer)})
	result, err := env.confirmed(audienceCreateID, "xwrite", createArguments)
	var out exportOperation
	if err != nil || json.Unmarshal([]byte(result), &out) != nil || !out.Done || out.Response == nil ||
		out.Response.State != "ACTIVE" || strings.Contains(result, "channel-canary") || strings.Contains(result, "hook.invalid") {
		t.Errorf("result = %s, %v", result, err)
	}
	failed := `{"name":"properties/200/audienceExports/400","done":true,"error":{"code":8,"message":"quota","details":[{"x":1}]}}`
	env = exportEnv(t, &fakeGoogle{api: always(http.StatusOK, failed)})
	result, err = env.confirmed(audienceCreateID, "xwrite", createArguments)
	if err != nil || !strings.Contains(result, `"error":{"code":8,"message":"quota"}`) || strings.Contains(result, "details") {
		t.Errorf("result = %s, %v", result, err)
	}
}

func TestAudienceExportsCreateChecksTheTargetsAndTheAudienceBeforeAnyIO(t *testing.T) {
	freshQuotas(t)
	env := exportEnv(t, &fakeGoogle{api: always(http.StatusOK, operationJSON)})
	for name, c := range map[string]struct{ connection, arguments string }{
		"audience of another property": {"xwrite", `{"property":"properties/200","audience":"properties/201/audiences/300","dimensions":[{"dimension_name":"deviceId"}]}`},
		"audience form":                {"xwrite", `{"property":"properties/200","audience":"properties/200/audienceExports/300","dimensions":[{"dimension_name":"deviceId"}]}`},
		"audience path":                {"xwrite", `{"property":"properties/200","audience":"properties/200/audiences/300/x","dimensions":[{"dimension_name":"deviceId"}]}`},
		"foreign property":             {"xproperty", `{"property":"properties/201","audience":"properties/201/audiences/300","dimensions":[{"dimension_name":"deviceId"}]}`},
		"account without property":     {"xaccount", `{"audience":"properties/200/audiences/300","dimensions":[{"dimension_name":"deviceId"}]}`},
		"no dimensions":                {"xwrite", `{"property":"properties/200","audience":"properties/200/audiences/300","dimensions":[]}`},
		"no audience":                  {"xwrite", `{"property":"properties/200","dimensions":[{"dimension_name":"deviceId"}]}`},
		"unknown member":               {"xwrite", `{"property":"properties/200","audience":"properties/200/audiences/300","dimensions":[{"dimension_name":"deviceId"}],"webhook_notification":{}}`},
		"empty dimension name":         {"xwrite", `{"property":"properties/200","audience":"properties/200/audiences/300","dimensions":[{}]}`},
	} {
		if _, err := env.confirmed(audienceCreateID, c.connection, c.arguments); !refusedInvalid(err) ||
			strings.Contains(err.Error(), "201") {
			t.Errorf("%s: %v", name, err)
		}
	}
	env.noIO(t)
	// The default property of a connection bound to one property.
	if _, err := env.confirmed(audienceCreateID, "xproperty", `{"audience":"properties/200/audiences/300",`+
		`"dimensions":[{"dimension_name":"deviceId"}]}`); err != nil {
		t.Errorf("bound default: %v", err)
	}
}

func TestAudienceExportsCreateProvesTheAccountOfAPropertyUnderABoundAccount(t *testing.T) {
	freshQuotas(t)
	env := exportEnv(t, &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
		if r.URL.Host == "analyticsadmin.googleapis.com" {
			return respond(r, http.StatusOK, "application/json", `{"name":"properties/200","parent":"accounts/100"}`), nil
		}
		return respond(r, http.StatusOK, "application/json", operationJSON), nil
	}})
	if _, err := env.confirmed(audienceCreateID, "xaccount", createArguments); err != nil {
		t.Fatal(err)
	}
	requests, _ := env.fake.seen()
	if len(requests) != 2 || requests[0].host != "analyticsadmin.googleapis.com" || requests[1].method != http.MethodPost {
		t.Errorf("requests = %+v", requests)
	}
	env = exportEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"name":"properties/200","parent":"accounts/900"}`)})
	if _, err := env.confirmed(audienceCreateID, "xaccount", createArguments); !refusedInvalid(err) {
		t.Errorf("foreign account: %v", err)
	}
	if requests, _ := env.fake.seen(); len(requests) != 1 {
		t.Errorf("requests = %+v", requests)
	}
}

func TestAudienceExportsCreateFollowsTheCoreQuotaRules(t *testing.T) {
	freshQuotas(t)
	env := exportEnv(t, &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, ":runReport") {
			return respond(r, http.StatusOK, "application/json", reportPage(1, 1, minTokensLeft-1)), nil
		}
		property := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1beta/"), "/audienceExports")
		return respond(r, http.StatusOK, "application/json", `{"name":"`+property+`/audienceExports/400"}`), nil
	}})
	if _, err := env.call(reportsRunID, "xwrite", `{"property":"properties/200",`+minimalReport[1:]); err != nil {
		t.Fatal(err)
	}
	before, _ := env.fake.seen()
	_, err := env.confirmed(audienceCreateID, "xwrite", createArguments)
	after, _ := env.fake.seen()
	if classOf(err) != provider.ClassRateLimited || len(after) != len(before) {
		t.Fatalf("err = %v, requests %d -> %d", err, len(before), len(after))
	}
	// Another property is unaffected, and an export holds a slot of the Core category while it is sent.
	if _, err := env.confirmed(audienceCreateID, "xwrite", `{"property":"properties/201","audience":`+
		`"properties/201/audiences/300","dimensions":[{"dimension_name":"deviceId"}]}`); err != nil {
		t.Errorf("other property: %v", err)
	}
	if got := len(quotas.running); got != 0 {
		t.Errorf("slots still held: %d", got)
	}
	// get and list are no Core requests: they run although the remainder is low.
	freshQuotas(t)
	env2 := exportEnv(t, &fakeGoogle{api: always(http.StatusOK, exportJSON(400, "ACTIVE"))})
	for i := 0; i < maxConcurrentReports; i++ {
		if _, err := quotas.enter("test", "properties/200", env2.open(t, "read").identity); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := env2.call(audienceGetID, "read", `{"name":"properties/200/audienceExports/400"}`); err != nil {
		t.Errorf("get with all Core slots taken: %v", err)
	}
}

func TestAudienceExportsGetReturnsEveryStateAndMember(t *testing.T) {
	for state, wants := range map[string][]string{
		"CREATING": {`"state":"CREATING"`, `"percentageCompleted":0`},
		"ACTIVE":   {`"state":"ACTIVE"`, `"rowCount":1200`, `"creationQuotaTokensCharged":7`, `"percentageCompleted":100`},
		"FAILED":   {`"state":"FAILED"`, `"errorMessage":"quota exhausted"`, `"percentageCompleted":12.5`},
	} {
		env := exportEnv(t, &fakeGoogle{api: always(http.StatusOK, exportJSON(400, state))})
		result, err := env.call(audienceGetID, "read", `{"name":"properties/200/audienceExports/400"}`)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range append(wants, `"name":"properties/200/audienceExports/400"`,
			`"audience":"properties/200/audiences/300"`, `"audienceDisplayName":"Purchasers"`,
			`"dimensions":[{"dimensionName":"deviceId"}]`, `"beginCreatingTime":"2026-11-16T10:00:00.123456789Z"`) {
			if !strings.Contains(result, want) {
				t.Errorf("%s: result lacks %s: %s", state, want, result)
			}
		}
		if strings.Contains(result, "webhook") || strings.Contains(result, "channel-canary") || strings.Contains(result, "hook.invalid") ||
			strings.Contains(result, "recurring") {
			t.Errorf("%s: a member outside v1beta is returned: %s", state, result)
		}
		requests, _ := env.fake.seen()
		if len(requests) != 1 || requests[0].method != http.MethodGet || requests[0].host != "analyticsdata.googleapis.com" ||
			requests[0].path != "/v1beta/properties/200/audienceExports/400" || requests[0].body != "" {
			t.Errorf("%s: requests = %+v", state, requests)
		}
	}
}

func TestAudienceExportsGetChecksTheNameAgainstTheTargetsBeforeAnyIO(t *testing.T) {
	foreign := `{"name":"properties/999/audienceExports/400"}`
	env := exportEnv(t, &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
		if r.URL.Host == "analyticsadmin.googleapis.com" {
			return respond(r, http.StatusOK, "application/json", `{"name":"properties/999","parent":"accounts/900"}`), nil
		}
		return respond(r, http.StatusOK, "application/json", exportJSON(400, "ACTIVE")), nil
	}})
	for name, c := range map[string]struct{ connection, arguments string }{
		"foreign property": {"property", foreign},
		"account only":     {"mixed", foreign}, // not bound itself and no account owns it: proof read first
		"bad form":         {"read", `{"name":"properties/200/audiences/400"}`},
		"no id":            {"read", `{"name":"properties/200/audienceExports/"}`},
		"leading zero":     {"read", `{"name":"properties/200/audienceExports/04"}`},
		"trailing path":    {"read", `{"name":"properties/200/audienceExports/4/x"}`},
		"traversal":        {"read", `{"name":"properties/200/audienceExports/../../accounts/1"}`},
		"query":            {"read", `{"name":"properties/200/audienceExports/4?alt=media"}`},
	} {
		before, _ := env.fake.seen()
		_, err := env.call(audienceGetID, c.connection, c.arguments)
		after, _ := env.fake.seen()
		wantRequests := 0
		if name == "account only" {
			wantRequests = 1
		}
		if err == nil || !refusedInvalid(err) || strings.Contains(err.Error(), "999") || len(after)-len(before) != wantRequests {
			t.Errorf("%s: %v, requests %d", name, err, len(after)-len(before))
		}
	}
}

func TestAudienceExportsGetRefusesAnAnswerForAnotherExport(t *testing.T) {
	env := exportEnv(t, &fakeGoogle{api: always(http.StatusOK, exportJSON(401, "ACTIVE"))})
	_, err := env.call(audienceGetID, "read", `{"name":"properties/200/audienceExports/400"}`)
	if classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("err = %v", err)
	}
}

func listPage(token string, ids ...int) string {
	items := make([]string, len(ids))
	for i, id := range ids {
		items[i] = exportJSON(id, "ACTIVE")
	}
	next := ""
	if token != "" {
		next = `,"nextPageToken":"` + token + `"`
	}
	return `{"audienceExports":[` + strings.Join(items, ",") + `]` + next + `}`
}

func TestAudienceExportsListPagesThreeTimesThroughTheBoundCursor(t *testing.T) {
	pages := map[string]string{"": listPage("tok-2", 1, 2), "tok-2": listPage("tok-3", 3, 4), "tok-3": listPage("", 5)}
	env := exportEnv(t, &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
		return respond(r, http.StatusOK, "application/json", pages[r.URL.Query().Get("pageToken")]), nil
	}})
	type listed struct {
		AudienceExports []audienceExport `json:"audienceExports"`
		NextCursor      string           `json:"next_cursor"`
		HasMore         bool             `json:"has_more"`
	}
	page := func(cursor string) listed {
		t.Helper()
		arguments := `{"property":"properties/200","page_size":2`
		if cursor != "" {
			arguments += `,"cursor":"` + cursor + `"`
		}
		result, err := env.call(audienceListID, "read", arguments+`}`)
		var out listed
		if err != nil || json.Unmarshal([]byte(result), &out) != nil {
			t.Fatalf("result = %s, %v", result, err)
		}
		return out
	}
	one := page("")
	two := page(one.NextCursor)
	three := page(two.NextCursor)
	if len(one.AudienceExports) != 2 || !one.HasMore || len(two.AudienceExports) != 2 || !two.HasMore ||
		len(three.AudienceExports) != 1 || three.HasMore || three.NextCursor != "" ||
		strings.Contains(one.NextCursor, "tok-2") {
		t.Fatalf("pages = %+v %+v %+v", one, two, three)
	}
	requests, _ := env.fake.seen()
	if len(requests) != 3 || requests[0].rawQuery != "pageSize=2" || requests[1].rawQuery != "pageSize=2&pageToken=tok-2" ||
		requests[2].rawQuery != "pageSize=2&pageToken=tok-3" || requests[0].path != "/v1beta/properties/200/audienceExports" ||
		requests[0].method != http.MethodGet {
		t.Errorf("requests = %+v", requests)
	}
	// A manipulated, foreign or garbage cursor never reaches Google.
	tampered := one.NextCursor[:len(one.NextCursor)-1] + "x"
	if tampered == one.NextCursor {
		tampered = one.NextCursor[:len(one.NextCursor)-1] + "y"
	}
	for name, c := range map[string]struct{ connection, property, cursor string }{
		"altered":        {"read", "properties/200", tampered},
		"garbage":        {"read", "properties/200", "not-a-cursor"},
		"other property": {"read", "properties/201", one.NextCursor},
		"other tool":     {"read", "properties/200", ""},
	} {
		arguments := `{"property":"` + c.property + `","cursor":"` + c.cursor + `"}`
		if c.cursor == "" {
			continue
		}
		if _, err := env.call(audienceListID, c.connection, arguments); !refusedInvalid(err) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := env.call(audienceListID, "property", `{"cursor":"`+one.NextCursor+`"}`); !refusedInvalid(err) {
		t.Errorf("other connection: %v", err)
	}
	if after, _ := env.fake.seen(); len(after) != 3 {
		t.Errorf("requests = %d", len(after))
	}
	if _, err := env.call(audienceListID, "read", `{"property":"properties/200","page_size":1001}`); !refusedInvalid(err) {
		t.Errorf("page size 1001: %v", err)
	}
}

func TestAudienceExportsListDropsEntriesOfAnotherProperty(t *testing.T) {
	answer := `{"audienceExports":[` + exportJSON(1, "ACTIVE") + `,{"name":"properties/999/audienceExports/5","state":"ACTIVE"},` +
		`{"name":"accounts/1","state":"ACTIVE"},{"state":"ACTIVE"}]}`
	env := exportEnv(t, &fakeGoogle{api: always(http.StatusOK, answer)})
	result, err := env.call(audienceListID, "read", `{"property":"properties/200"}`)
	var out struct {
		AudienceExports []audienceExport `json:"audienceExports"`
		Truncated       []string         `json:"truncated"`
	}
	if err != nil || json.Unmarshal([]byte(result), &out) != nil || len(out.AudienceExports) != 1 ||
		len(out.Truncated) != 1 || strings.Contains(result, "999") {
		t.Errorf("result = %s, %v", result, err)
	}
}

func TestAudienceExportsBoundUntrustedValues(t *testing.T) {
	long := strings.Repeat("y", 500)
	answer := `{"name":"properties/200/audienceExports/400","audience":"https://evil.invalid/audience","audienceDisplayName":"` +
		long + `","state":"AC TIVE","errorMessage":"` + long + `","dimensions":[` +
		strings.TrimSuffix(strings.Repeat(`{"dimensionName":"d"},`, 40), ",") + `]}`
	env := exportEnv(t, &fakeGoogle{api: always(http.StatusOK, answer)})
	result, err := env.call(audienceGetID, "read", `{"name":"properties/200/audienceExports/400"}`)
	var out audienceExport
	if err != nil || json.Unmarshal([]byte(result), &out) != nil || len(out.Dimensions) != maxAudienceDimensions ||
		len([]rune(string(out.AudienceDisplayName))) != maxTextRunes+1 || len([]rune(string(out.ErrorMessage))) != maxTextRunes+1 ||
		out.State != "" || out.Audience != "" || strings.Contains(result, "evil.invalid") {
		t.Errorf("result = %.300s, %v", result, err)
	}
}

func TestAudienceExportsMapFailuresWithoutProviderTextAndNeverRepeat(t *testing.T) {
	tools := []struct {
		id, connection, arguments string
		change                    bool
	}{
		{audienceCreateID, "xwrite", createArguments, true},
		{audienceGetID, "read", `{"name":"properties/200/audienceExports/400"}`, false},
		{audienceListID, "read", `{"property":"properties/200"}`, false},
	}
	for _, c := range []struct {
		name   string
		api    func(int, *http.Request) (*http.Response, error)
		class  provider.Class
		unsure bool
	}{
		{"403", always(http.StatusForbidden, errorObject(403, "PERMISSION_DENIED", "X")), provider.ClassPermission, false},
		{"service disabled", always(http.StatusForbidden, errorObject(403, "PERMISSION_DENIED", "SERVICE_DISABLED")),
			provider.ClassPermission, false},
		{"404", always(http.StatusNotFound, errorObject(404, "NOT_FOUND", "")), provider.ClassNotFound, false},
		{"429", always(http.StatusTooManyRequests, errorObject(429, "RESOURCE_EXHAUSTED", "")), provider.ClassRateLimited, false},
		{"500", always(http.StatusInternalServerError, errorObject(500, "INTERNAL", "")), provider.ClassProviderError, true},
		{"503", always(http.StatusServiceUnavailable, errorObject(503, "UNAVAILABLE", "")), provider.ClassUnreachable, true},
		{"not json", always(http.StatusOK, `{"name":`), provider.ClassInvalidResponse, true},
		{"unreadable body", func(_ int, r *http.Request) (*http.Response, error) {
			resp := respond(r, http.StatusOK, "application/json", "")
			resp.Body = failingBody{}
			return resp, nil
		}, provider.ClassInvalidResponse, true},
		{"timeout", func(int, *http.Request) (*http.Response, error) { return nil, timeoutError{} }, provider.ClassTimeout, true},
	} {
		for _, tool := range tools {
			t.Run(c.name+" "+tool.id, func(t *testing.T) {
				freshQuotas(t)
				env := exportEnv(t, &fakeGoogle{api: c.api})
				var err error
				if tool.change {
					_, err = env.confirmed(tool.id, tool.connection, tool.arguments)
				} else {
					_, err = env.call(tool.id, tool.connection, tool.arguments)
				}
				if classOf(err) != c.class || leaks(err.Error()) != "" {
					t.Fatalf("error = %v", err)
				}
				if got := strings.Contains(err.Error(), "outcome of the change is unknown"); got != (c.unsure && tool.change) {
					t.Errorf("unknown outcome reported = %t: %v", got, err)
				}
				if requests, _ := env.fake.seen(); len(requests) != 1 {
					t.Errorf("requests = %d, want exactly one", len(requests))
				}
			})
		}
	}
	// An answer without the name of an export of the property leaves the outcome unknown.
	for _, answer := range []string{`{}`, `{"name":"properties/201/audienceExports/4"}`, `{"name":"operations/9"}`} {
		env := exportEnv(t, &fakeGoogle{api: always(http.StatusOK, answer)})
		_, err := env.confirmed(audienceCreateID, "xwrite", createArguments)
		if classOf(err) != provider.ClassProviderError || !strings.Contains(err.Error(), "outcome of the change is unknown") {
			t.Errorf("answer %s: %v", answer, err)
		}
	}
}

func TestAudienceExportsRateLimitHoldsTheNextRequestWithoutRetryAfter(t *testing.T) {
	freshQuotas(t)
	env := exportEnv(t, &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
		if n == 1 {
			return respond(r, http.StatusTooManyRequests, "application/json", errorObject(429, "RESOURCE_EXHAUSTED", "")), nil
		}
		return respond(r, http.StatusOK, "application/json", operationJSON), nil
	}})
	if _, err := env.confirmed(audienceCreateID, "xwrite", createArguments); classOf(err) != provider.ClassRateLimited {
		t.Fatalf("429: %v", err)
	}
	if requests, _ := env.fake.seen(); len(requests) != 1 {
		t.Errorf("requests = %d", len(requests))
	}
	if _, err := env.confirmed(audienceCreateID, "xwrite", createArguments); err != nil {
		t.Fatal(err)
	}
	if len(*env.waits) == 0 {
		t.Error("the next request did not wait")
	}
}

func TestAudienceExportsToolsDeclareRiskScopeAndProfile(t *testing.T) {
	reg := registry(t)
	metadata, _ := reg.ProviderMetadata(Provider)
	for _, id := range []string{audienceCreateID, audienceGetID, audienceListID, funnelRunID} {
		if got := metadata.Token.Scopes[id]; len(got) != 1 || got[0] != scopeReadOnly {
			t.Errorf("%s scopes = %v", id, got)
		}
	}
	for _, d := range reg.Provider(Provider) {
		switch d.ID {
		case audienceCreateID:
			if string(d.Risk.Effect) != "create" || d.Risk.Confirmation != "required" || !d.Risk.OpenWorld ||
				d.RequiresToolAllowList || reg.ConfirmationSummary(d.ID) == nil {
				t.Errorf("%s = %+v", d.ID, d.Risk)
			}
		case audienceGetID, audienceListID, funnelRunID:
			if d.Risk != reportsRisk {
				t.Errorf("%s = %+v", d.ID, d.Risk)
			}
		}
	}
	for _, profile := range metadata.Profiles {
		hasCreate := false
		for _, id := range profile.Tools {
			hasCreate = hasCreate || id == audienceCreateID
		}
		if hasCreate != (profile.ID == "write") {
			t.Errorf("profile %s holds the create tool: %t", profile.ID, hasCreate)
		}
	}
	summary := reg.ConfirmationSummary(audienceCreateID)(json.RawMessage(createArguments))
	for _, want := range []string{"audience export", "properties/200/audiences/300", "2 dimension", "Core quota"} {
		if !strings.Contains(summary, want) {
			t.Errorf("summary %q lacks %q", summary, want)
		}
	}
}
