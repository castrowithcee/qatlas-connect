package googlemerchant

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const fixtureFileUpload = `{"name":"accounts/1000001/dataSources/13/fileUploads/latest","dataSourceId":"13",` +
	`"processingState":"SUCCEEDED","itemsTotal":"120","itemsCreated":"100","itemsUpdated":"15",` +
	`"uploadTime":"2026-11-20T08:30:00Z","issues":[` +
	`{"title":"Item too big","description":"Item 7 has too many attributes","code":"validation/too_big","count":"3",` +
	`"severity":"ERROR","documentationUri":"https://support.example.invalid/issue"},` +
	`{"title":"Value changed","code":"validation/changed","count":"9","severity":"WARNING","unknownMember":"dropped"}],` +
	`"unknownField":"never held"}`

func TestDataSourceOperationToolsAreDeclaredWithTheirRisk(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	for _, w := range []struct {
		id          string
		effect      capability.Effect
		idempotency capability.Idempotency
		confirm     capability.Confirmation
		listed      bool
	}{
		{dataSourcesFetchID, capability.EffectExecute, capability.IdempotencyNonIdempotent, capability.ConfirmationRequired, false},
		{dataSourcesDeleteID, capability.EffectDelete, capability.IdempotencyIdempotent, capability.ConfirmationRequired, true},
		{fileUploadsGetID, capability.EffectRead, capability.IdempotencySafe, capability.ConfirmationNone, false},
	} {
		d, _, ok := reg.Lookup(w.id)
		if !ok || d.Version != 1 || d.Risk.Effect != w.effect || d.Risk.Idempotency != w.idempotency ||
			d.Risk.Confirmation != w.confirm || !d.Risk.OpenWorld || d.RequiresToolAllowList != w.listed ||
			len(d.InputSchema) == 0 || len(d.OutputSchema) == 0 {
			t.Errorf("%s = %+v, tools list %t, %v", w.id, d.Risk, d.RequiresToolAllowList, ok)
		}
		if scopes := metadata.Token.Scopes[w.id]; len(scopes) != 1 || scopes[0] != contentScope {
			t.Errorf("%s scopes = %v", w.id, scopes)
		}
		// Only the tools that read without a tools list are in the read profile.
		if inRead := slices.Contains(metadata.Profiles[0].Tools, w.id); inRead != (w.effect == capability.EffectRead) {
			t.Errorf("%s in read profile: %t", w.id, inRead)
		}
		if !strings.Contains(string(d.InputSchema), `"additionalProperties":false`) || strings.Contains(string(d.InputSchema), "fetch") {
			t.Errorf("%s input schema is open: %s", w.id, d.InputSchema)
		}
		if w.effect != capability.EffectRead && !strings.Contains(d.Description, "never repeated") {
			t.Errorf("%s does not say the call is never repeated", w.id)
		}
	}
	fetch, _, _ := reg.Lookup(dataSourcesFetchID)
	for _, want := range []string{"outside its fetch schedule", "more than once a day", "Products service", "states no quota",
		"file input", "only accepts the request"} {
		if !strings.Contains(fetch.Description, want) {
			t.Errorf("fetch description misses %q: %s", want, fetch.Description)
		}
	}
	del, _, _ := reg.Lookup(dataSourcesDeleteID)
	for _, want := range []string{"documents no effect", "some minutes", "tools list"} {
		if !strings.Contains(del.Description, want) {
			t.Errorf("delete description misses %q: %s", want, del.Description)
		}
	}
}

func TestDataSourcesFetchSendsOneEmptyPostAndReportsOnlyTheAcceptance(t *testing.T) {
	h := newHarness(t)
	h.answerWith(200, `{}`)
	out, err := h.call(t, handleDataSourcesFetch, `{"account":"1000001","data_source":"13"}`)
	if err != nil {
		t.Fatal(err)
	}
	got := h.recorded(t)
	if len(got) != 1 || got[0].method != "POST" || got[0].path != "/datasources/v1/accounts/1000001/dataSources/13:fetch" ||
		got[0].query != "" || got[0].body != "{}" {
		t.Errorf("requests = %+v", got)
	}
	if out["accepted"] != true || out["name"] != "accounts/1000001/dataSources/13" || len(out) != 2 {
		t.Errorf("out = %v", out)
	}
}

func TestDataSourcesDeleteSendsOneDeleteWithoutBody(t *testing.T) {
	h := newHarness(t)
	h.answerWith(200, `{}`)
	out, err := h.call(t, handleDataSourcesDelete, `{"account":"1000001","data_source":"13"}`)
	if err != nil {
		t.Fatal(err)
	}
	got := h.recorded(t)
	if len(got) != 1 || got[0].method != "DELETE" || got[0].path != "/datasources/v1/accounts/1000001/dataSources/13" ||
		got[0].query != "" || got[0].body != "" {
		t.Errorf("requests = %+v", got)
	}
	if out["accepted"] != true || out["name"] != "accounts/1000001/dataSources/13" || len(out) != 2 {
		t.Errorf("out = %v", out)
	}
}

func TestFileUploadsGetReadsTheLatestAliasAndShowsEveryDocumentedMember(t *testing.T) {
	h := newHarness(t)
	h.answerWith(200, fixtureFileUpload)
	out, err := h.call(t, handleFileUploadsGet, `{"account":"1000001","data_source":"13"}`)
	if err != nil {
		t.Fatal(err)
	}
	got := h.recorded(t)
	if len(got) != 1 || got[0].method != "GET" || got[0].query != "" ||
		got[0].path != "/datasources/v1/accounts/1000001/dataSources/13/fileUploads/latest" {
		t.Errorf("requests = %+v", got)
	}
	encoded, _ := json.Marshal(out)
	text := string(encoded)
	for _, shown := range []string{"fileUploads/latest", `"dataSourceId":"13"`, "SUCCEEDED", `"itemsTotal":"120"`,
		`"itemsCreated":"100"`, `"itemsUpdated":"15"`, "2026-11-20T08:30:00Z", "Item too big",
		"Item 7 has too many attributes", "validation/too_big", `"count":"3"`, `"severity":"ERROR"`,
		"support.example.invalid/issue", "WARNING"} {
		if !strings.Contains(text, shown) {
			t.Errorf("result misses %q: %s", shown, text)
		}
	}
	for _, banned := range []string{"unknownField", "unknownMember", "never held", "dropped"} {
		if strings.Contains(text, banned) {
			t.Errorf("result holds %q: %s", banned, text)
		}
	}
}

func TestFileUploadsGetCutsLongTextsAndManyIssues(t *testing.T) {
	var issues []string
	for range maxNested + 50 {
		issues = append(issues, `{"title":"`+strings.Repeat("t", 500)+`","description":"`+strings.Repeat("d", 5000)+`"}`)
	}
	h := newHarness(t)
	h.answerWith(200, `{"issues":[`+strings.Join(issues, ",")+`,7,"x"]}`)
	out, err := h.call(t, handleFileUploadsGet, `{"account":"1000001","data_source":"13"}`)
	if err != nil {
		t.Fatal(err)
	}
	rows := out["issues"].([]any)
	if len(rows) != maxNested {
		t.Fatalf("issues = %d, want %d", len(rows), maxNested)
	}
	row := rows[0].(map[string]any)
	if n := len([]rune(row["title"].(string))); n != maxNameRunes {
		t.Errorf("title holds %d runes", n)
	}
	if n := len([]rune(row["description"].(string))); n != maxRenderedText {
		t.Errorf("description holds %d runes", n)
	}
}

func TestDataSourceOperationsRefuseBadArgumentsBeforeSecretAndIO(t *testing.T) {
	for tool, handler := range map[string]capability.Handler{"fetch": handleDataSourcesFetch,
		"delete": handleDataSourcesDelete, "file upload": handleFileUploadsGet} {
		for name, args := range map[string]string{
			"no data source":  `{"account":"1000001"}`,
			"bad data source": `{"account":"1000001","data_source":"1/../2"}`,
			"leading zero":    `{"account":"1000001","data_source":"013"}`,
			"path":            `{"account":"1000001","data_source":"13:fetch"}`,
			"alias":           `{"account":"1000001","data_source":"13","file_upload":"older"}`,
			"bad account":     `{"account":"1/2","data_source":"13"}`,
			"fileInput":       `{"account":"1000001","data_source":"13","fileInput":{}}`,
			"fetch address":   `{"account":"1000001","data_source":"13","fetchUri":"https://feeds.example.invalid/f"}`,
			"password":        `{"account":"1000001","data_source":"13","password":"` + fixtureFetchPassword + `"}`,
			"mask":            `{"account":"1000001","data_source":"13","update_mask":"x"}`,
			"force":           `{"account":"1000001","data_source":"13","force":true}`,
			"cursor":          `{"account":"1000001","data_source":"13","cursor":"x"}`,
		} {
			h := newHarness(t)
			_, err := handler(context.Background(), h.resolved, h.secrets, h.red, json.RawMessage(args))
			if !isInvalidRequest(err) {
				t.Errorf("%s %s: err = %v", tool, name, err)
			}
			if err != nil && (strings.Contains(err.Error(), fixtureFetchPassword) || strings.Contains(err.Error(), "feeds.example")) {
				t.Errorf("%s %s: message quotes the input: %v", tool, name, err)
			}
			h.untouched(t)
		}
	}
}

func TestDataSourceOperationsCheckTheAccountBeforeSecretAndIO(t *testing.T) {
	for name, handler := range map[string]capability.Handler{"fetch": handleDataSourcesFetch,
		"delete": handleDataSourcesDelete, "file upload": handleFileUploadsGet} {
		h := newHarness(t, "accounts/1000001")
		if _, err := h.call(t, handler, `{"account":"2000002","data_source":"1"}`); !isInvalidRequest(err) ||
			strings.Contains(err.Error(), "2000002") {
			t.Errorf("%s: err = %v", name, err)
		}
		h.untouched(t)
		h = newHarness(t, "accounts/1", "accounts/2")
		if _, err := h.call(t, handler, `{"data_source":"1"}`); !isInvalidRequest(err) {
			t.Errorf("%s without account: err = %v", name, err)
		}
		h.untouched(t)
	}
}

func TestDataSourceOperationsProveASubAccountOfABoundAdvancedAccount(t *testing.T) {
	for name, c := range map[string]struct {
		handler capability.Handler
		method  string
	}{"fetch": {handleDataSourcesFetch, "POST"}, "delete": {handleDataSourcesDelete, "DELETE"},
		"file upload": {handleFileUploadsGet, "GET"}} {
		h := newHarness(t, "advanced-accounts/10")
		h.google.api = func(req *http.Request) (int, string, http.Header) {
			if req.Body != nil {
				data, _ := io.ReadAll(req.Body)
				recordedBody[req] = string(data)
			}
			if strings.HasSuffix(req.URL.Path, ":listSubaccounts") {
				return 200, `{"accounts":[` + accountRow("11", "Sub") + `]}`, nil
			}
			return 200, `{}`, nil
		}
		if _, err := h.call(t, c.handler, `{"account":"11","data_source":"5"}`); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		got := h.recorded(t)
		if len(got) != 2 || !strings.HasSuffix(got[0].path, "accounts/10:listSubaccounts") || got[1].method != c.method {
			t.Errorf("%s: requests = %+v", name, got)
		}
		// An account that is not a sub-account is refused after the proof read and never changed.
		h = newHarness(t, "advanced-accounts/10")
		h.google.api = func(req *http.Request) (int, string, http.Header) { return 200, `{"accounts":[]}`, nil }
		if _, err := h.call(t, c.handler, `{"account":"99","data_source":"5"}`); !isInvalidRequest(err) {
			t.Fatalf("%s: err = %v", name, err)
		}
		for _, r := range h.google.seen() {
			if r.Method != http.MethodGet || strings.HasSuffix(r.URL.Path, ":fetch") {
				t.Errorf("%s: a %s request followed the failed proof", name, r.Method)
			}
		}
	}
}

func TestDataSourceOperationChangesAreNeverRepeatedAndSayWhenTheOutcomeIsOpen(t *testing.T) {
	for name, c := range map[string]struct {
		status int
		class  provider.Class
		open   bool
	}{
		"500":               {500, provider.ClassProviderError, true},
		"503":               {503, provider.ClassUnreachable, true},
		"504":               {504, provider.ClassTimeout, true},
		"429 minute":        {429, provider.ClassRateLimited, false},
		"400 no file input": {400, provider.ClassProviderError, false},
		"401":               {401, provider.ClassAuth, false},
		"403":               {403, provider.ClassPermission, false},
		"404":               {404, provider.ClassNotFound, false},
		"200 not an object": {200, provider.ClassInvalidResponse, true},
	} {
		for tool, handler := range map[string]capability.Handler{"fetch": handleDataSourcesFetch,
			"delete": handleDataSourcesDelete} {
			h := newHarness(t)
			body := `{"error":{"code":1,"status":"X","message":"google text","details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"QUOTA_REQUEST_RATE_TOO_HIGH"}]}}`
			if c.status == 200 {
				body = `[1]`
			}
			h.answerWith(c.status, body)
			_, err := h.call(t, handler, `{"account":"1000001","data_source":"5"}`)
			if got := classOf(err); got != c.class {
				t.Errorf("%s %s: class = %q, want %q (%v)", tool, name, got, c.class, err)
			}
			if n := len(h.google.seen()); n != 1 {
				t.Errorf("%s %s: %d requests, a change must go out once", tool, name, n)
			}
			if open := err != nil && strings.Contains(err.Error(), "outcome of the change is unknown"); open != c.open {
				t.Errorf("%s %s: open outcome = %t, want %t (%v)", tool, name, open, c.open, err)
			}
			if err != nil && strings.Contains(err.Error(), "google text") {
				t.Errorf("%s %s: message carries Google's text: %v", tool, name, err)
			}
		}
	}
}

func TestDataSourceOperationsWithATransportFailureAreNotRepeated(t *testing.T) {
	for tool, handler := range map[string]capability.Handler{"fetch": handleDataSourcesFetch,
		"delete": handleDataSourcesDelete} {
		h := newHarness(t)
		h.google.fail = io.ErrUnexpectedEOF
		_, err := h.call(t, handler, `{"account":"1000001","data_source":"5"}`)
		if err == nil || !strings.Contains(err.Error(), "outcome of the change is unknown") || len(h.google.seen()) != 1 {
			t.Errorf("%s: err = %v, requests = %d", tool, err, len(h.google.seen()))
		}
	}
}

func TestDataSourceOperationsMapQuotaErrorsAndHoldTheNextRequest(t *testing.T) {
	for reason, text := range map[string]string{"QUOTA_TOO_MANY_REQUESTS": "12:00 UTC", "QUOTA_REQUEST_RATE_TOO_HIGH": "minute"} {
		h := newHarness(t)
		h.answerWith(429, `{"error":{"code":429,"status":"RESOURCE_EXHAUSTED","details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"`+reason+`"}]}}`)
		_, err := h.call(t, handleDataSourcesFetch, `{"account":"1000001","data_source":"5"}`)
		if classOf(err) != provider.ClassRateLimited || !strings.Contains(err.Error(), text) || len(h.google.seen()) != 1 {
			t.Errorf("%s: err = %v, requests = %d", reason, err, len(h.google.seen()))
		}
		h.answerWith(200, fixtureFileUpload)
		if _, err := h.call(t, handleFileUploadsGet, `{"account":"1000001","data_source":"5"}`); err != nil {
			t.Fatal(err)
		}
		if waits := h.spacing.recorded(); len(waits) == 0 {
			t.Errorf("%s: the request after a 429 did not wait", reason)
		}
	}
}

func TestA401OnAnOperationDropsTheTokenWithoutRenewingItForTheSameCall(t *testing.T) {
	h := newHarness(t)
	h.answerWith(401, `{"error":{"code":401,"status":"UNAUTHENTICATED"}}`)
	if _, err := h.call(t, handleDataSourcesDelete, `{"account":"1000001","data_source":"5"}`); classOf(err) != provider.ClassAuth {
		t.Fatalf("err = %v", err)
	}
	if h.google.grants != 1 || len(h.google.seen()) != 1 {
		t.Errorf("grants = %d, requests = %d, want one each", h.google.grants, len(h.google.seen()))
	}
}

func TestFileUploadsGetRefusesAnAnswerThatIsNotAnObject(t *testing.T) {
	h := newHarness(t)
	h.answerWith(200, `[1]`)
	if _, err := h.call(t, handleFileUploadsGet, `{"account":"1000001","data_source":"5"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("err = %v", err)
	}
}

func TestOperationSummariesNameWhatHappens(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		summary func(json.RawMessage) string
		args    string
		want    []string
	}{
		{fetchSummary, `{"account":"1000001","data_source":"13"}`,
			[]string{"Fetch data source 13", "account 1000001", "outside its schedule", "never repeated", "Google processes it afterwards"}},
		{deleteSummary, `{"account":"1000001","data_source":"13"}`,
			[]string{"Delete data source 13", "account 1000001", "not documented", "no way to undo"}},
		{deleteSummary, `{"data_source":"13"}`, []string{"the account of the connection"}},
		{deleteSummary, `{"account":"1/2","data_source":"x\ny"}`, []string{"(invalid ID)"}},
	} {
		got := c.summary(json.RawMessage(c.args))
		for _, w := range c.want {
			if !strings.Contains(got, w) {
				t.Errorf("summary %q misses %q", got, w)
			}
		}
		if strings.Contains(got, "x\ny") || strings.Contains(got, "1/2") {
			t.Errorf("summary quotes an invalid value: %q", got)
		}
	}
	for _, id := range []string{dataSourcesFetchID, dataSourcesDeleteID} {
		d, _, _ := reg.Lookup(id)
		if d.Risk.Confirmation != capability.ConfirmationRequired {
			t.Errorf("%s needs confirmation", id)
		}
	}
}
