package googleanalytics

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// historyEnv adds connections that list the search tool, with the bindings of the property tests.
func historyEnv(t *testing.T, fake *fakeGoogle) *testEnv {
	t.Helper()
	env := newEnv(t, fake)
	for name, targets := range map[string][]string{"hfree": nil, "haccount": {"accounts/100"},
		"hproperty": {"properties/200"}, "hmixed": {"accounts/100", "properties/901"}} {
		env.cfg.Connections[name] = config.Connection{Service: "ga", Credential: "login", Targets: targets,
			Tools: []string{changeHistoryID}}
	}
	if err := env.cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	return env
}

const (
	secretCanary = "mp-secret-canary-91f3a7c2"
	// eventJSON is one event of user1@example.com on property 200.
	eventJSON = `{"id":"9001","changeTime":"2026-11-02T10:00:00.123Z","actorType":"USER",` +
		`"userActorEmail":"user1@example.com","changesFiltered":false,"extra":1,"changes":[` +
		`{"action":"UPDATED","resource":"properties/200/dataRetentionSettings","extra":1,` +
		`"resourceBeforeChange":{"dataRetentionSettings":{"name":"properties/200/dataRetentionSettings",` +
		`"eventDataRetention":"TWO_MONTHS"},"unknownKind":{"x":1}},` +
		`"resourceAfterChange":{"dataRetentionSettings":{"name":"properties/200/dataRetentionSettings",` +
		`"eventDataRetention":"FOURTEEN_MONTHS"}}}]}`
)

type historyOut struct {
	Events []struct {
		ID              string `json:"id"`
		ChangeTime      string `json:"changeTime"`
		ActorType       string `json:"actorType"`
		UserActorEmail  string `json:"userActorEmail"`
		ChangesFiltered bool   `json:"changesFiltered"`
		Changes         []struct {
			Action   string         `json:"action"`
			Resource string         `json:"resource"`
			Before   map[string]any `json:"resourceBeforeChange"`
			After    map[string]any `json:"resourceAfterChange"`
		} `json:"changes"`
	} `json:"changeHistoryEvents"`
	NextCursor string   `json:"next_cursor"`
	HasMore    bool     `json:"has_more"`
	Truncated  []string `json:"truncated"`
}

func decodeHistory(t *testing.T, result string) historyOut {
	t.Helper()
	var out historyOut
	if err := json.Unmarshal([]byte(result), &out); err != nil {
		t.Fatalf("result %s: %v", result, err)
	}
	return out
}

func TestChangeHistorySearchPagesThroughThreePagesWithEveryFilter(t *testing.T) {
	pages := []string{
		`{"changeHistoryEvents":[` + eventJSON + `],"nextPageToken":"t2"}`,
		`{"changeHistoryEvents":[` + strings.Replace(eventJSON, "9001", "9002", 1) + `],"nextPageToken":"t3"}`,
		`{"changeHistoryEvents":[` + strings.Replace(eventJSON, "9001", "9003", 1) + `]}`,
	}
	env := historyEnv(t, &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
		return respond(r, http.StatusOK, "application/json", pages[n-1]), nil
	}})
	const filters = `"resource_type":["PROPERTY","DATA_RETENTION_SETTINGS"],"action":["UPDATED","CREATED"],` +
		`"actor_email":["user1@example.com","user2@example.org"],"earliest_change_time":"2026-11-01T00:00:00+01:00",` +
		`"latest_change_time":"2026-11-30T23:59:59Z","page_size":1`
	cursor, ids := "", []string{}
	for i := 0; i < 3; i++ {
		args := `{"account":"accounts/100","property":"properties/200",` + filters
		if cursor != "" {
			args += `,"cursor":"` + cursor + `"`
		}
		result, err := env.call(changeHistorySearchID(), "hfree", args+`}`)
		if err != nil {
			t.Fatal(err)
		}
		out := decodeHistory(t, result)
		if len(out.Events) != 1 || out.HasMore != (i < 2) || strings.Contains(result, "extra") ||
			strings.Contains(result, "unknownKind") || out.Events[0].UserActorEmail != "user1@example.com" ||
			out.Events[0].ActorType != "USER" || out.Events[0].ChangeTime != "2026-11-02T10:00:00.123Z" ||
			out.Events[0].Changes[0].Action != "UPDATED" || out.Events[0].Changes[0].Resource != "properties/200/dataRetentionSettings" ||
			out.Events[0].Changes[0].Before["dataRetentionSettings"] == nil || out.Events[0].Changes[0].After["dataRetentionSettings"] == nil {
			t.Fatalf("page %d = %s", i, result)
		}
		ids = append(ids, out.Events[0].ID)
		cursor = out.NextCursor
	}
	requests, _ := env.fake.seen()
	if !slices.Equal(ids, []string{"9001", "9002", "9003"}) || len(requests) != 3 {
		t.Fatalf("ids %v, requests %d", ids, len(requests))
	}
	first := bodyOf(t, requests[0])
	for key, want := range map[string]any{"property": "properties/200", "pageSize": float64(1),
		"earliestChangeTime": "2026-10-31T23:00:00Z", "latestChangeTime": "2026-11-30T23:59:59Z"} {
		if first[key] != want {
			t.Errorf("body %s = %v, want %v", key, first[key], want)
		}
	}
	for key, want := range map[string][]any{"resourceType": {"PROPERTY", "DATA_RETENTION_SETTINGS"},
		"action": {"UPDATED", "CREATED"}, "actorEmail": {"user1@example.com", "user2@example.org"}} {
		if !slices.Equal(first[key].([]any), want) {
			t.Errorf("body %s = %v, want %v", key, first[key], want)
		}
	}
	if first["pageToken"] != nil || bodyOf(t, requests[1])["pageToken"] != "t2" || bodyOf(t, requests[2])["pageToken"] != "t3" ||
		bodyOf(t, requests[2])["property"] != "properties/200" {
		t.Errorf("page tokens: %s | %s | %s", requests[0].body, requests[1].body, requests[2].body)
	}
	for _, r := range requests {
		if r.method != http.MethodPost || r.host != "analyticsadmin.googleapis.com" ||
			r.path != "/v1beta/accounts/100:searchChangeHistoryEvents" || r.rawQuery != "" ||
			r.contentType != "application/json" {
			t.Errorf("request = %+v", r)
		}
	}
}

func changeHistorySearchID() string { return changeHistoryID }

func TestChangeHistoryCursorIsBoundAndAManipulatedCursorIsRefused(t *testing.T) {
	env := historyEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"changeHistoryEvents":[],"nextPageToken":"t2"}`)})
	const base = `"account":"accounts/100","property":"properties/200"`
	result, err := env.call(changeHistoryID, "hfree", `{`+base+`,"action":["UPDATED"]}`)
	first := decodeHistory(t, result)
	if err != nil || first.NextCursor == "" || !first.HasMore || len(first.Events) != 0 {
		t.Fatalf("%v %s", err, result)
	}
	requests, _ := env.fake.seen()
	sent := len(requests)
	manipulated := first.NextCursor[:len(first.NextCursor)-2] + "xx"
	if strings.HasSuffix(first.NextCursor, "xx") {
		manipulated = first.NextCursor[:len(first.NextCursor)-2] + "yy"
	}
	for name, args := range map[string]string{
		"manipulated":      `{` + base + `,"action":["UPDATED"],"cursor":"` + manipulated + `"}`,
		"token swapped":    `{` + base + `,"action":["UPDATED"],"cursor":"t2"}`,
		"truncated":        `{` + base + `,"action":["UPDATED"],"cursor":"` + first.NextCursor[:8] + `"}`,
		"other property":   `{"account":"accounts/100","property":"properties/201","action":["UPDATED"],"cursor":"` + first.NextCursor + `"}`,
		"no property":      `{"account":"accounts/100","action":["UPDATED"],"cursor":"` + first.NextCursor + `"}`,
		"other account":    `{"account":"accounts/101","property":"properties/200","action":["UPDATED"],"cursor":"` + first.NextCursor + `"}`,
		"other action":     `{` + base + `,"action":["CREATED"],"cursor":"` + first.NextCursor + `"}`,
		"other actor":      `{` + base + `,"action":["UPDATED"],"actor_email":["a@example.com"],"cursor":"` + first.NextCursor + `"}`,
		"other time":       `{` + base + `,"action":["UPDATED"],"earliest_change_time":"2026-11-01T00:00:00Z","cursor":"` + first.NextCursor + `"}`,
		"other connection": ``,
	} {
		if args == "" {
			args = `{` + base + `,"action":["UPDATED"],"cursor":"` + first.NextCursor + `"}`
			if _, err := env.call(changeHistoryID, "hproperty", args); !refusedInvalid(err) {
				t.Errorf("%s: %v", name, err)
			}
			continue
		}
		if _, err := env.call(changeHistoryID, "hfree", args); !refusedInvalid(err) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if requests, _ := env.fake.seen(); len(requests) != sent {
		t.Errorf("a refused cursor reached Google: %d requests, want %d", len(requests), sent)
	}
	// The same cursor with another page size still continues.
	if _, err := env.call(changeHistoryID, "hfree", `{`+base+`,"action":["UPDATED"],"page_size":5,"cursor":"`+first.NextCursor+`"}`); err != nil {
		t.Errorf("same search with another page size: %v", err)
	}
	requests, _ = env.fake.seen()
	if bodyOf(t, requests[len(requests)-1])["pageToken"] != "t2" {
		t.Errorf("last body = %s", requests[len(requests)-1].body)
	}
}

func TestChangeHistoryEmptyAnswerIsAnEmptyList(t *testing.T) {
	for _, answer := range []string{`{}`, `{"changeHistoryEvents":[]}`} {
		env := historyEnv(t, &fakeGoogle{api: always(http.StatusOK, answer)})
		result, err := env.call(changeHistoryID, "hfree", `{"account":"accounts/100"}`)
		if out := decodeHistory(t, result); err != nil || len(out.Events) != 0 || out.HasMore ||
			!strings.Contains(result, `"has_more":false`) || strings.Contains(result, "next_cursor") {
			t.Errorf("%s: %s, %v", answer, result, err)
		}
		requests, _ := env.fake.seen()
		if len(requests) != 1 || bodyOf(t, requests[0])["property"] != nil || requests[0].body != `{}` {
			t.Errorf("requests = %+v", requests)
		}
	}
}

func TestChangeHistoryBindingEnforcesTheFilterBeforeAnyIO(t *testing.T) {
	for _, c := range []struct {
		name, connection, args string
		methods                []string
		refused                bool
		property               string
	}{
		{"free account", "hfree", `{"account":"accounts/100"}`, []string{"POST"}, false, ""},
		{"free any property", "hfree", `{"account":"accounts/100","property":"properties/555"}`, []string{"POST"}, false, "properties/555"},
		{"property bound, no property", "hproperty", `{"account":"accounts/100"}`, nil, true, ""},
		{"property bound, other property", "hproperty", `{"account":"accounts/100","property":"properties/201"}`, nil, true, ""},
		{"property bound, other account", "hproperty", `{"account":"accounts/900","property":"properties/200"}`, []string{"GET"}, true, ""},
		{"property bound", "hproperty", `{"account":"accounts/100","property":"properties/200"}`, []string{"GET", "POST"}, false, "properties/200"},
		{"account bound, whole account", "haccount", `{"account":"accounts/100"}`, []string{"POST"}, false, ""},
		{"account bound, other account", "haccount", `{"account":"accounts/900"}`, nil, true, ""},
		{"account bound, other account with bound-looking property", "haccount", `{"account":"accounts/900","property":"properties/901"}`, nil, true, ""},
		{"account bound, its property", "haccount", `{"account":"accounts/100","property":"properties/201"}`, []string{"GET", "POST"}, false, "properties/201"},
		{"account bound, property of another account", "haccount", `{"account":"accounts/100","property":"properties/901"}`, []string{"GET"}, true, ""},
		{"mixed, bound account", "hmixed", `{"account":"accounts/100"}`, []string{"POST"}, false, ""},
		{"mixed, other account without property", "hmixed", `{"account":"accounts/900"}`, nil, true, ""},
		{"mixed, bound property", "hmixed", `{"account":"accounts/900","property":"properties/901"}`, []string{"GET", "POST"}, false, "properties/901"},
		{"mixed, bound property under the wrong account", "hmixed", `{"account":"accounts/100","property":"properties/901"}`, []string{"GET"}, true, ""},
		{"mixed, unbound property of another account", "hmixed", `{"account":"accounts/900","property":"properties/902"}`, nil, true, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			env := historyEnv(t, &fakeGoogle{api: func(_ int, r *http.Request) (*http.Response, error) {
				body := map[string]string{
					"/v1beta/properties/200": `{"name":"properties/200","parent":"accounts/100"}`,
					"/v1beta/properties/201": `{"name":"properties/201","parent":"accounts/100"}`,
					"/v1beta/properties/901": `{"name":"properties/901","parent":"accounts/900"}`,
				}[r.URL.Path]
				if body == "" {
					body = `{"changeHistoryEvents":[]}`
				}
				return respond(r, http.StatusOK, "application/json", body), nil
			}})
			result, err := env.call(changeHistoryID, c.connection, c.args)
			requests, _ := env.fake.seen()
			if (err != nil) != c.refused || !slices.Equal(methodsOf(requests), c.methods) {
				t.Fatalf("%v, requests %v, want %v", err, methodsOf(requests), c.methods)
			}
			if c.refused && (!refusedInvalid(err) || strings.Contains(err.Error(), "901") || strings.Contains(result, "901")) {
				t.Errorf("refusal %v", err)
			}
			if c.refused && len(c.methods) == 0 {
				env.noIO(t)
			}
			if !c.refused {
				got, _ := bodyOf(t, requests[len(requests)-1])["property"].(string)
				if got != c.property {
					t.Errorf("filter property = %q, want %q", got, c.property)
				}
			}
		})
	}
}

func TestChangeHistoryDropsChangesOutsideTheSearchedScope(t *testing.T) {
	answer := `{"changeHistoryEvents":[` +
		`{"id":"1","userActorEmail":"user1@example.com","actorType":"USER","changes":[` +
		`{"action":"UPDATED","resource":"properties/200/dataStreams/1"},` +
		`{"action":"UPDATED","resource":"properties/2001"},` +
		`{"action":"UPDATED","resource":"properties/201"},` +
		`{"action":"UPDATED","resource":"properties/200"},` +
		`{"action":"UPDATED","resource":"properties/200/../201"},` +
		`{"action":"UPDATED","resource":"accounts/100"},` +
		`{"action":"UPDATED","resource":"x properties/200"},{"action":"UPDATED"}]},` +
		`{"id":"2","actorType":"SYSTEM","changes":[{"action":"CREATED","resource":"properties/999/customMetrics/3"}]},` +
		`{"id":"3","actorType":"SYSTEM"}]}`
	env := historyEnv(t, &fakeGoogle{api: always(http.StatusOK, answer)})
	result, err := env.call(changeHistoryID, "hfree", `{"account":"accounts/100","property":"properties/200"}`)
	out := decodeHistory(t, result)
	if err != nil || len(out.Events) != 1 || out.Events[0].ID != "1" || !out.Events[0].ChangesFiltered ||
		len(out.Events[0].Changes) != 2 || out.Events[0].Changes[0].Resource != "properties/200/dataStreams/1" ||
		out.Events[0].Changes[1].Resource != "properties/200" || strings.Contains(result, "999") ||
		strings.Contains(result, "properties/201") || strings.Contains(result, "2001") {
		t.Fatalf("%s, %v", result, err)
	}
	// Without a property only accounts other than the asked one are dropped by name, and an event that names no
	// change at all is dropped on a bound connection.
	env = historyEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"changeHistoryEvents":[`+
		`{"id":"1","changes":[{"action":"UPDATED","resource":"accounts/100"},{"action":"UPDATED","resource":"accounts/101"},`+
		`{"action":"UPDATED","resource":"accounts/1000/x"},{"action":"UPDATED","resource":"properties/200"}]},{"id":"3"}]}`)})
	result, err = env.call(changeHistoryID, "haccount", `{"account":"accounts/100"}`)
	out = decodeHistory(t, result)
	if err != nil || len(out.Events) != 1 || len(out.Events[0].Changes) != 2 || strings.Contains(result, "accounts/101") ||
		strings.Contains(result, "accounts/1000") {
		t.Fatalf("account search: %s, %v", result, err)
	}
	result, err = env.call(changeHistoryID, "hfree", `{"account":"accounts/100"}`)
	if out = decodeHistory(t, result); err != nil || len(out.Events) != 2 {
		t.Fatalf("free connection keeps an event without changes: %s, %v", result, err)
	}
}

func TestChangeHistoryBoundsProviderContentAndNeverReturnsASecret(t *testing.T) {
	long := strings.Repeat("a", 400)
	var deep = `1`
	for i := 0; i < 12; i++ {
		deep = `{"n":` + deep + `}`
	}
	wide := make([]string, 80)
	for i := range wide {
		wide[i] = fmt.Sprintf(`"k%02d"`, i)
	}
	event := `{"id":"1","actorType":"USER","userActorEmail":"` + long + `@example.com","changes":[` +
		`{"action":"CREATED","resource":"properties/200/dataStreams/1/measurementProtocolSecrets/5",` +
		`"resourceAfterChange":{"measurementProtocolSecret":{"name":"properties/200/dataStreams/1/measurementProtocolSecrets/5",` +
		`"displayName":"` + long + `","secretValue":"` + secretCanary + `","nested":{"SecretValue":"` + secretCanary + `"}},` +
		`"dataStream":{"deep":` + deep + `,"list":[` + strings.Join(wide, ",") + `],"flag":true,"nothing":null,"num":1.5},` +
		`"notDocumented":{"x":"y"}}}]}`
	env := historyEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"changeHistoryEvents":[`+event+`]}`)})
	result, err := env.call(changeHistoryID, "hfree", `{"account":"accounts/100"}`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result, secretCanary) || strings.Contains(result, "ecretValue") || strings.Contains(result, "notDocumented") ||
		strings.Contains(result, long) {
		t.Fatalf("result leaks or is unbounded: %s", result)
	}
	if env.red.Apply(secretCanary) == secretCanary {
		t.Error("the secret value was not registered with the redactor")
	}
	out := decodeHistory(t, result)
	after := out.Events[0].Changes[0].After
	stream, _ := after["dataStream"].(map[string]any)
	list, _ := stream["list"].([]any)
	if len(list) != maxSnapshotItems || stream["flag"] != true || stream["num"] != json.Number("1.5").String() && stream["num"] != 1.5 ||
		!slices.Contains(out.Truncated, "snapshots") {
		t.Errorf("snapshot = %v, truncated %v", after, out.Truncated)
	}
	if len([]rune(out.Events[0].UserActorEmail)) > maxTextRunes+1 {
		t.Errorf("email of %d characters was not cut", len([]rune(out.Events[0].UserActorEmail)))
	}
	secretSnapshot, _ := after["measurementProtocolSecret"].(map[string]any)
	if len(secretSnapshot) != 2 || secretSnapshot["name"] == nil {
		t.Errorf("secret snapshot = %v", secretSnapshot)
	}
	// Snapshots stop at the depth bound.
	if strings.Count(result, `"n"`) >= 12 {
		t.Errorf("depth was not bounded: %s", result)
	}
}

func TestChangeHistoryCutsEventsAndChangesAtTheirBounds(t *testing.T) {
	changes := make([]string, maxChangesPerEvent+5)
	for i := range changes {
		changes[i] = `{"action":"UPDATED","resource":"properties/200"}`
	}
	events := make([]string, maxSummaries+3)
	for i := range events {
		events[i] = fmt.Sprintf(`{"id":"%d","changes":[{"action":"UPDATED","resource":"properties/200"}]}`, i+1)
	}
	env := historyEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"changeHistoryEvents":[{"id":"1","changes":[`+
		strings.Join(changes, ",")+`]}]}`)})
	result, err := env.call(changeHistoryID, "hfree", `{"account":"accounts/100"}`)
	out := decodeHistory(t, result)
	if err != nil || len(out.Events[0].Changes) != maxChangesPerEvent || !out.Events[0].ChangesFiltered ||
		!slices.Contains(out.Truncated, "changes") {
		t.Errorf("changes: %v, %d", err, len(out.Events[0].Changes))
	}
	env = historyEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"changeHistoryEvents":[`+strings.Join(events, ",")+`]}`)})
	result, err = env.call(changeHistoryID, "hfree", `{"account":"accounts/100"}`)
	if out = decodeHistory(t, result); err != nil || len(out.Events) != maxSummaries || !slices.Contains(out.Truncated, "events") {
		t.Errorf("events: %v, %d", err, len(out.Events))
	}
}

func TestChangeHistoryRefusesBadArgumentsBeforeAnyIO(t *testing.T) {
	env := historyEnv(t, &fakeGoogle{api: always(http.StatusOK, `{}`)})
	for name, args := range map[string]string{
		"no account":           `{}`,
		"account is property":  `{"account":"properties/100"}`,
		"account with suffix":  `{"account":"accounts/100/x"}`,
		"property is account":  `{"account":"accounts/100","property":"accounts/100"}`,
		"unknown argument":     `{"account":"accounts/100","filter":"x"}`,
		"unspecified type":     `{"account":"accounts/100","resource_type":["CHANGE_HISTORY_RESOURCE_TYPE_UNSPECIFIED"]}`,
		"unknown type":         `{"account":"accounts/100","resource_type":["FOO"]}`,
		"repeated type":        `{"account":"accounts/100","resource_type":["PROPERTY","PROPERTY"]}`,
		"empty type list":      `{"account":"accounts/100","resource_type":[]}`,
		"unspecified action":   `{"account":"accounts/100","action":["ACTION_TYPE_UNSPECIFIED"]}`,
		"lower case action":    `{"account":"accounts/100","action":["created"]}`,
		"email without at":     `{"account":"accounts/100","actor_email":["user1.example.com"]}`,
		"email with blank":     `{"account":"accounts/100","actor_email":["user 1@example.com"]}`,
		"email repeated":       `{"account":"accounts/100","actor_email":["a@example.com","a@example.com"]}`,
		"email not a list":     `{"account":"accounts/100","actor_email":"a@example.com"}`,
		"time without zone":    `{"account":"accounts/100","earliest_change_time":"2026-11-01T00:00:00"}`,
		"time not a time":      `{"account":"accounts/100","latest_change_time":"yesterday-at-noon-please"}`,
		"earliest after last":  `{"account":"accounts/100","earliest_change_time":"2026-12-01T00:00:00Z","latest_change_time":"2026-11-01T00:00:00Z"}`,
		"page size 0":          `{"account":"accounts/100","page_size":0}`,
		"page size 201":        `{"account":"accounts/100","page_size":201}`,
		"empty cursor":         `{"account":"accounts/100","cursor":""}`,
		"cursor not text":      `{"account":"accounts/100","cursor":5}`,
		"property not text":    `{"account":"accounts/100","property":5}`,
		"property blank":       `{"account":"accounts/100","property":""}`,
		"too many emails":      `{"account":"accounts/100","actor_email":["` + strings.Join(manyEmails(51), `","`) + `"]}`,
		"page token as member": `{"account":"accounts/100","page_token":"x"}`,
	} {
		if _, err := env.call(changeHistoryID, "hfree", args); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	env.noIO(t)
}

func manyEmails(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("user%d@example.com", i)
	}
	return out
}

func TestChangeHistoryErrorsAreClassifiedAndNothingIsRepeated(t *testing.T) {
	for _, c := range []struct {
		name  string
		api   func(int, *http.Request) (*http.Response, error)
		class provider.Class
	}{
		{"403 missing scope", always(http.StatusForbidden, errorObject(403, "PERMISSION_DENIED", "ACCESS_TOKEN_SCOPE_INSUFFICIENT")),
			provider.ClassPermission},
		{"404", always(http.StatusNotFound, errorObject(404, "NOT_FOUND", "")), provider.ClassNotFound},
		{"429", always(http.StatusTooManyRequests, errorObject(429, "RESOURCE_EXHAUSTED", "")), provider.ClassRateLimited},
		{"500", always(http.StatusInternalServerError, errorObject(500, "INTERNAL", "")), provider.ClassProviderError},
	} {
		t.Run(c.name, func(t *testing.T) {
			env := historyEnv(t, &fakeGoogle{api: c.api})
			_, err := env.call(changeHistoryID, "hfree", `{"account":"accounts/100"}`)
			if classOf(err) != c.class || leaks(err.Error()) != "" {
				t.Fatalf("error = %v", err)
			}
			if requests, _ := env.fake.seen(); len(requests) != 1 {
				t.Errorf("requests = %d, want exactly one", len(requests))
			}
		})
	}
}

func TestChangeHistory429WithoutRetryAfterHoldsTheNextRequest(t *testing.T) {
	env := historyEnv(t, &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
		if n == 1 {
			return respond(r, http.StatusTooManyRequests, "application/json", errorObject(429, "RESOURCE_EXHAUSTED", "")), nil
		}
		return respond(r, http.StatusOK, "application/json", `{}`), nil
	}})
	if _, err := env.call(changeHistoryID, "hfree", `{"account":"accounts/100"}`); classOf(err) != provider.ClassRateLimited {
		t.Fatalf("error = %v", err)
	}
	if _, err := env.call(changeHistoryID, "hfree", `{"account":"accounts/100"}`); err != nil {
		t.Fatal(err)
	}
	if requests, _ := env.fake.seen(); len(requests) != 2 {
		t.Errorf("requests = %d, want 2 without a retry", len(requests))
	}
	if waits := *env.waits; len(waits) == 0 || slices.Max(waits) < 900_000_000 {
		t.Errorf("waits = %v, want the next request held for about a second", waits)
	}
}

func TestChangeHistoryProofReadMustNameTheProperty(t *testing.T) {
	env := historyEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"name":"properties/999","parent":"accounts/100"}`)})
	_, err := env.call(changeHistoryID, "hproperty", `{"account":"accounts/100","property":"properties/200"}`)
	if classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("error = %v", err)
	}
	if requests, _ := env.fake.seen(); len(requests) != 1 {
		t.Errorf("requests = %d, want only the proof read", len(requests))
	}
}

func TestChangeHistoryNeedsTheEditScopeAndSitsOutsideTheReadProfile(t *testing.T) {
	reg := registry(t)
	metadata, _ := reg.ProviderMetadata(Provider)
	d, _, ok := reg.Lookup(changeHistoryID)
	if !ok || d.Risk != changeHistoryRisk || d.Risk.DataSensitivity == dataSensitivity ||
		d.Risk.DataSensitivity == dataSensitivityAccess || !d.RequiresToolAllowList {
		t.Fatalf("descriptor = %+v", d)
	}
	for _, part := range []string{"PERSONAL DATA", "email addresses", "analytics.edit", "untrusted", "never returned"} {
		if !strings.Contains(d.Description, part) {
			t.Errorf("description lacks %q", part)
		}
	}
	if got := metadata.Token.Scopes[changeHistoryID]; !slices.Equal(got, []string{scopeEdit}) {
		t.Errorf("scopes = %v", got)
	}
	profiles := map[string]config.ToolProfile{}
	for _, p := range metadata.Profiles {
		profiles[p.ID] = p
	}
	if slices.Contains(profiles["read"].Tools, changeHistoryID) || !profiles["read"].Recommended ||
		!slices.Contains(profiles["write"].Tools, changeHistoryID) {
		t.Error("the search belongs to write and not to the recommended read profile")
	}
	for _, tool := range profiles["read"].Tools {
		if !slices.Equal(metadata.Token.Scopes[tool], []string{scopeReadOnly}) {
			t.Errorf("profile read holds %s with scopes %v", tool, metadata.Token.Scopes[tool])
		}
	}
	own := profiles["read-change-history"]
	if own.Recommended || !slices.Equal(own.Tools, append(slices.Clone(readTools), changeHistoryID)) {
		t.Errorf("profile = %+v", own)
	}
	if reg.ConfirmationSummary(changeHistoryID) != nil {
		t.Error("a reading tool has a confirmation summary")
	}
}

func TestChangeHistoryIsNotOfferedByEffectAndKeepsReadTokensReadOnly(t *testing.T) {
	env := historyEnv(t, &fakeGoogle{api: always(http.StatusOK, `{}`)})
	for _, connection := range []string{"read", "write", "property"} {
		if _, err := env.call(changeHistoryID, connection, `{"account":"accounts/100"}`); err == nil {
			t.Errorf("%s: a tool that is not listed was reachable", connection)
		}
	}
	env.noIO(t)
}
