package googlemerchant

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// Synthetic values: upper case placeholders, none of them a real credential or address.
const (
	fixtureFetchPassword = "PLACEHOLDER_FETCH_PASSWORD"
	fixtureFetchUser     = "PLACEHOLDER_FETCH_USER"
)

const fixtureSources = `{"dataSources":[` +
	`{"name":"accounts/1000001/dataSources/11","dataSourceId":"11","displayName":"Main","input":"API",` +
	`"primaryProductDataSource":{"feedLabel":"US","contentLanguage":"en","countries":["US","CA"],` +
	`"destinations":[{"destination":"SHOPPING_ADS","state":"ENABLED"}],"legacyLocal":true,` +
	`"defaultRule":{"takeFromDataSources":[{"self":true},{"supplementalDataSourceName":"accounts/1000001/dataSources/12",` +
	`"primaryDataSourceName":"ignored"}]},"unknownField":"never held"}},` +
	`{"name":"accounts/1000001/dataSources/12","dataSourceId":"12","displayName":"Extra","input":"API",` +
	`"supplementalProductDataSource":{"feedLabel":"US","contentLanguage":"en",` +
	`"referencingPrimaryDataSources":[{"self":true}]}},` +
	`{"name":"accounts/1000001/dataSources/13","dataSourceId":"13","displayName":"Feed","input":"FILE",` +
	`"fileInput":{"fileName":"feed.xml","fileInputType":"FETCH","fetchSettings":{` +
	`"fetchUri":"https://` + fixtureFetchUser + `:` + fixtureFetchPassword + `@feeds.example.invalid/feed.xml",` +
	`"username":"` + fixtureFetchUser + `","password":"` + fixtureFetchPassword + `","frequency":"FREQUENCY_WEEKLY",` +
	`"timeZone":"Europe/Zurich","timeOfDay":{"hours":6},"dayOfWeek":"MONDAY","enabled":true}}},` +
	`{"name":"accounts/1000001/dataSources/14","dataSourceId":"14","displayName":"Local",` +
	`"localInventoryDataSource":{"feedLabel":"US","contentLanguage":"en"}},` +
	`{"name":"accounts/1000001/dataSources/15","dataSourceId":"15","displayName":"Reviews",` +
	`"productReviewDataSource":{}}],"nextPageToken":"page-two"}`

// sent is one request as the fake Google saw it.
type sent struct {
	method, path, query, contentType, body string
}

// recorded reads the requests so far with their bodies; only the write tests use it, because the shared
// harness helper expects reads.
func (h *harness) recorded(t *testing.T) []sent {
	t.Helper()
	var out []sent
	for _, req := range h.google.seen() {
		if req.URL.Host != "merchantapi.googleapis.com" || req.URL.Scheme != "https" {
			t.Errorf("request %s left the fixed origin", req.URL.Host)
		}
		if req.Header.Get("Authorization") != "Bearer "+testAccess {
			t.Errorf("request %s carries no bearer token", req.URL.Path)
		}
		if strings.Contains(req.URL.String(), testAccess) {
			t.Errorf("request URL carries a token")
		}
		out = append(out, sent{req.Method, req.URL.Path, req.URL.RawQuery, req.Header.Get("Content-Type"), recordedBody[req]})
	}
	return out
}

var recordedBody = map[*http.Request]string{}

// answerWith makes the fake answer every request with the status and body, and remembers the bodies.
func (h *harness) answerWith(status int, body string) {
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		if req.Body != nil {
			data, _ := io.ReadAll(req.Body)
			recordedBody[req] = string(data)
		}
		return status, body, nil
	}
}

func TestDataSourceToolsAreDeclaredWithTheirRisk(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	want := map[string]struct {
		effect      capability.Effect
		idempotency capability.Idempotency
		confirm     capability.Confirmation
	}{
		dataSourcesListID:   {capability.EffectRead, capability.IdempotencySafe, capability.ConfirmationNone},
		dataSourcesGetID:    {capability.EffectRead, capability.IdempotencySafe, capability.ConfirmationNone},
		dataSourcesCreateID: {capability.EffectCreate, capability.IdempotencyNonIdempotent, capability.ConfirmationRequired},
		dataSourcesUpdateID: {capability.EffectUpdate, capability.IdempotencyIdempotent, capability.ConfirmationRequired},
	}
	for id, w := range want {
		d, _, ok := reg.Lookup(id)
		if !ok || d.Version != 1 || d.Risk.Effect != w.effect || d.Risk.Idempotency != w.idempotency ||
			d.Risk.Confirmation != w.confirm || !d.Risk.OpenWorld || d.RequiresToolAllowList {
			t.Errorf("%s = %+v, %v", id, d.Risk, ok)
		}
		if scopes := metadata.Token.Scopes[id]; len(scopes) != 1 || scopes[0] != contentScope {
			t.Errorf("%s scopes = %v", id, scopes)
		}
		if inRead := slices.Contains(metadata.Profiles[0].Tools, id); inRead != (w.effect == capability.EffectRead) {
			t.Errorf("%s in read profile: %t", id, inRead)
		}
	}
	for _, id := range []string{dataSourcesCreateID, dataSourcesUpdateID} {
		d, _, _ := reg.Lookup(id)
		if !strings.Contains(d.Description, "some minutes") || !strings.Contains(d.Description, "only that Google accepted") ||
			!strings.Contains(d.Description, "Google Sheets") {
			t.Errorf("%s description misses the delay, the acceptance, or the API-only limit: %s", id, d.Description)
		}
		var schema struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if err := json.Unmarshal(d.InputSchema, &schema); err != nil {
			t.Fatal(err)
		}
		for name := range schema.Properties {
			if refusedKeys[name] {
				t.Errorf("%s offers the argument %s", id, name)
			}
		}
		if strings.Contains(string(d.InputSchema), "fetch") || strings.Contains(string(d.InputSchema), "fileInput") ||
			!strings.Contains(string(d.InputSchema), `"additionalProperties":false`) {
			t.Errorf("%s input schema is open to file input: %s", id, d.InputSchema)
		}
	}
}

func TestDataSourcesListSendsPageSizeAndShowsEveryDocumentedMemberWithoutThePassword(t *testing.T) {
	h := newHarness(t)
	h.answerWith(200, fixtureSources)
	out, err := h.call(t, handleDataSourcesList, `{"account":"1000001"}`)
	if err != nil {
		t.Fatal(err)
	}
	got := h.recorded(t)
	if len(got) != 1 || got[0].method != "GET" || got[0].path != "/datasources/v1/accounts/1000001/dataSources" ||
		got[0].query != "pageSize=100" {
		t.Errorf("requests = %+v", got)
	}
	encoded, _ := json.Marshal(out)
	text := string(encoded)
	for _, banned := range []string{fixtureFetchPassword, `"password"`, "unknownField", "primaryDataSourceName", "ignored"} {
		if strings.Contains(text, banned) {
			t.Errorf("result holds %q: %s", banned, text)
		}
	}
	for _, shown := range []string{"feeds.example.invalid/feed.xml", fixtureFetchUser, "FREQUENCY_WEEKLY", "Europe/Zurich",
		`"hours":6`, "MONDAY", "FETCH", "feed.xml", "supplementalDataSourceName", "referencingPrimaryDataSources",
		"localInventoryDataSource", "productReviewDataSource", `"legacyLocal":true`, "SHOPPING_ADS", `"CA"`} {
		if !strings.Contains(text, shown) {
			t.Errorf("result misses %q: %s", shown, text)
		}
	}
	if strings.Contains(text, fixtureFetchUser+":") || strings.Contains(text, "@feeds") {
		t.Errorf("the fetch address keeps its user info: %s", text)
	}
	if out["next_cursor"] == nil || len(out["dataSources"].([]any)) != 5 {
		t.Errorf("out = %v", out)
	}
}

func TestDataSourcesListCursorPageSizeAndEmptyList(t *testing.T) {
	h := newHarness(t, "accounts/1000001")
	h.answerWith(200, fixtureSources)
	first, err := h.call(t, handleDataSourcesList, `{"page_size":2}`)
	if err != nil {
		t.Fatal(err)
	}
	cursor := first["next_cursor"].(string)
	h.answerWith(200, `{}`)
	second, err := h.call(t, handleDataSourcesList, `{"page_size":2,"cursor":"`+cursor+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	if rows, ok := second["dataSources"].([]any); !ok || len(rows) != 0 || second["next_cursor"] != nil {
		t.Errorf("second = %v", second)
	}
	got := h.recorded(t)
	if len(got) != 2 || got[0].query != "pageSize=2" || got[1].query != "pageSize=2&pageToken=page-two" {
		t.Errorf("requests = %+v", got)
	}
	for _, args := range []string{`{"cursor":"` + cursor + `"}`,
		`{"page_size":3,"cursor":"` + cursor + `"}`,
		`{"page_size":2,"cursor":"foreign"}`} {
		if _, err := h.call(t, handleDataSourcesList, args); !isInvalidRequest(err) {
			t.Errorf("%s gave %v, want a refused cursor", args, err)
		}
	}
	for _, args := range []string{`{"page_size":1001}`, `{"page_size":-1}`} {
		if _, err := h.call(t, handleDataSourcesList, args); !isInvalidRequest(err) {
			t.Errorf("%s gave %v", args, err)
		}
	}
	if len(h.google.seen()) != 2 {
		t.Errorf("a refused call reached Google: %d requests", len(h.google.seen()))
	}
}

var fileSourceWithPassword = `{"name":"accounts/1000001/dataSources/13","displayName":"Feed","fileInput":` +
	`{"fetchSettings":{"fetchUri":"https://feeds.example.invalid/f.xml","password":"` + fixtureFetchPassword + `"}}}`

func TestDataSourcesGetReadsOneSourceAndRefusesBadIDsBeforeAnyRequest(t *testing.T) {
	h := newHarness(t)
	h.answerWith(200, fileSourceWithPassword)
	out, err := h.call(t, handleDataSourcesGet, `{"account":"1000001","data_source":"13"}`)
	if err != nil {
		t.Fatal(err)
	}
	if got := h.recorded(t); len(got) != 1 || got[0].path != "/datasources/v1/accounts/1000001/dataSources/13" {
		t.Errorf("requests = %+v", got)
	}
	if encoded, _ := json.Marshal(out); strings.Contains(string(encoded), fixtureFetchPassword) {
		t.Errorf("the password is shown: %s", encoded)
	}
	for name, args := range map[string]string{
		"no data source": `{"account":"1000001"}`, "bad data source": `{"account":"1000001","data_source":"1/../2"}`,
		"leading zero": `{"account":"1000001","data_source":"013"}`, "bad account": `{"account":"1/2","data_source":"1"}`,
		"unknown member": `{"account":"1000001","data_source":"1","fileInput":{}}`,
	} {
		fresh := newHarness(t)
		if _, err := fresh.call(t, handleDataSourcesGet, args); !isInvalidRequest(err) {
			t.Errorf("%s gave %v", name, err)
		}
		fresh.untouched(t)
	}
}

func TestDataSourceToolsCheckTheAccountBeforeSecretAndIO(t *testing.T) {
	calls := map[string]struct {
		handler capability.Handler
		args    string
	}{
		"list":   {handleDataSourcesList, `{"account":"2000002"}`},
		"get":    {handleDataSourcesGet, `{"account":"2000002","data_source":"1"}`},
		"create": {handleDataSourcesCreate, `{"account":"2000002","display_name":"A","primary_product":{}}`},
		"update": {handleDataSourcesUpdate, `{"account":"2000002","data_source":"1","display_name":"A"}`},
	}
	for name, c := range calls {
		h := newHarness(t, "accounts/1000001")
		if _, err := h.call(t, c.handler, c.args); !isInvalidRequest(err) || strings.Contains(err.Error(), "2000002") {
			t.Errorf("%s: err = %v", name, err)
		}
		h.untouched(t)
		// Without an argument a connection with two targets has no single account.
		h = newHarness(t, "accounts/1", "accounts/2")
		args := strings.Replace(c.args, `"account":"2000002",`, "", 1)
		args = strings.Replace(args, `{"account":"2000002"}`, `{}`, 1)
		if _, err := h.call(t, c.handler, args); !isInvalidRequest(err) {
			t.Errorf("%s without account: err = %v", name, err)
		}
		h.untouched(t)
	}
}

func TestDataSourceWritesProveASubAccountOfABoundAdvancedAccount(t *testing.T) {
	h := newHarness(t, "advanced-accounts/10")
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		if req.Body != nil {
			data, _ := io.ReadAll(req.Body)
			recordedBody[req] = string(data)
		}
		if strings.HasSuffix(req.URL.Path, ":listSubaccounts") {
			return 200, `{"accounts":[` + accountRow("11", "Sub") + `]}`, nil
		}
		return 200, `{"name":"accounts/11/dataSources/5","displayName":"A"}`, nil
	}
	if _, err := h.call(t, handleDataSourcesUpdate, `{"account":"11","data_source":"5","display_name":"A"}`); err != nil {
		t.Fatal(err)
	}
	got := h.recorded(t)
	if len(got) != 2 || got[0].method != "GET" || !strings.HasSuffix(got[0].path, "accounts/10:listSubaccounts") ||
		got[1].method != "PATCH" {
		t.Errorf("requests = %+v", got)
	}
	// An account that is not a sub-account is refused after the proof read and never written.
	h = newHarness(t, "advanced-accounts/10")
	h.google.api = func(req *http.Request) (int, string, http.Header) { return 200, `{"accounts":[]}`, nil }
	_, err := h.call(t, handleDataSourcesCreate, `{"account":"99","display_name":"A","product_review":{}}`)
	if !isInvalidRequest(err) {
		t.Fatalf("err = %v", err)
	}
	for _, r := range h.google.seen() {
		if r.Method != http.MethodGet {
			t.Errorf("a %s request followed the failed proof", r.Method)
		}
	}
}

func TestDataSourcesCreateBuildsTheBodyFromCheckedArguments(t *testing.T) {
	h := newHarness(t)
	h.answerWith(200, `{"name":"accounts/1000001/dataSources/77","dataSourceId":"77","displayName":"Main","input":"API"}`)
	out, err := h.call(t, handleDataSourcesCreate, `{"account":"1000001","display_name":"Main","primary_product":{`+
		`"feedLabel":"US-1","contentLanguage":"en","countries":["US","CA"],"legacyLocal":true,`+
		`"destinations":[{"destination":"SHOPPING_ADS","state":"ENABLED"},{"destination":"FREE_LISTINGS"}],`+
		`"defaultRule":{"takeFromDataSources":[{"self":true},{"supplementalDataSourceId":"12"}]}}}`)
	if err != nil {
		t.Fatal(err)
	}
	got := h.recorded(t)
	if len(got) != 1 || got[0].method != "POST" || got[0].path != "/datasources/v1/accounts/1000001/dataSources" ||
		got[0].query != "" || got[0].contentType != "application/json" {
		t.Fatalf("requests = %+v", got)
	}
	want := `{"displayName":"Main","primaryProductDataSource":{"contentLanguage":"en","countries":["US","CA"],` +
		`"defaultRule":{"takeFromDataSources":[{"self":true},{"supplementalDataSourceName":"accounts/1000001/dataSources/12"}]},` +
		`"destinations":[{"destination":"SHOPPING_ADS","state":"ENABLED"},{"destination":"FREE_LISTINGS"}],` +
		`"feedLabel":"US-1","legacyLocal":true}}`
	if got[0].body != want {
		t.Errorf("body = %s\nwant   %s", got[0].body, want)
	}
	if out["accepted"] != true || out["dataSource"].(map[string]any)["dataSourceId"] != "77" {
		t.Errorf("out = %v", out)
	}
}

func TestDataSourcesCreateBodiesOfEveryKind(t *testing.T) {
	for name, c := range map[string]struct{ args, body string }{
		"supplemental": {`"supplemental_product":{"feedLabel":"DE","contentLanguage":"de"}`,
			`{"displayName":"N","supplementalProductDataSource":{"contentLanguage":"de","feedLabel":"DE"}}`},
		"supplemental bare": {`"supplemental_product":{}`, `{"displayName":"N","supplementalProductDataSource":{}}`},
		"regional": {`"regional_inventory":{"feedLabel":"DE","contentLanguage":"de"}`,
			`{"displayName":"N","regionalInventoryDataSource":{"contentLanguage":"de","feedLabel":"DE"}}`},
		"promotion": {`"promotion":{"targetCountry":"DE","contentLanguage":"de"}`,
			`{"displayName":"N","promotionDataSource":{"contentLanguage":"de","targetCountry":"DE"}}`},
		"product review":  {`"product_review":{}`, `{"displayName":"N","productReviewDataSource":{}}`},
		"merchant review": {`"merchant_review":{}`, `{"displayName":"N","merchantReviewDataSource":{}}`},
		"primary bare":    {`"primary_product":{}`, `{"displayName":"N","primaryProductDataSource":{}}`},
	} {
		h := newHarness(t)
		h.answerWith(200, `{}`)
		if _, err := h.call(t, handleDataSourcesCreate, `{"account":"1000001","display_name":"N",`+c.args+`}`); err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if got := h.recorded(t); len(got) != 1 || got[0].body != c.body {
			t.Errorf("%s: requests = %+v, want body %s", name, got, c.body)
		}
	}
}

// Everything below is refused before a secret is read, a token is requested, or a request is sent.
func TestDataSourceWritesRefuseFileSourcesAndBadArgumentsBeforeSecretAndIO(t *testing.T) {
	const create = `{"account":"1000001","display_name":"A",`
	const update = `{"account":"1000001","data_source":"5",`
	for name, c := range map[string]struct {
		handler capability.Handler
		args    string
		file    bool
	}{
		"create fileInput":      {handleDataSourcesCreate, create + `"fileInput":{"fetchSettings":{"fetchUri":"https://feeds.example.invalid/f"}},"primary_product":{}}`, true},
		"create fetchSettings":  {handleDataSourcesCreate, create + `"fetchSettings":{},"primary_product":{}}`, true},
		"create nested":         {handleDataSourcesCreate, create + `"primary_product":{"fileInput":{}}}`, true},
		"create password":       {handleDataSourcesCreate, create + `"primary_product":{"password":"` + fixtureFetchPassword + `"}}`, true},
		"create fetch uri":      {handleDataSourcesCreate, create + `"primary_product":{"defaultRule":{"fetchUri":"x"}}}`, true},
		"create sheets":         {handleDataSourcesCreate, create + `"google_sheets":{"url":"x"},"primary_product":{}}`, true},
		"create input":          {handleDataSourcesCreate, create + `"input":"FILE","primary_product":{}}`, true},
		"update fileInput":      {handleDataSourcesUpdate, update + `"fileInput":{"fetchSettings":{"enabled":true}}}`, true},
		"update fetchSettings":  {handleDataSourcesUpdate, update + `"display_name":"A","fetch_settings":{}}`, true},
		"update mask":           {handleDataSourcesUpdate, update + `"display_name":"A","update_mask":["fileInput.fetchSettings"]}`, false},
		"update updateMask":     {handleDataSourcesUpdate, update + `"display_name":"A","updateMask":"fileInput"}`, false},
		"create no block":       {handleDataSourcesCreate, create + `"x":1}`, false},
		"create empty":          {handleDataSourcesCreate, create[:len(create)-1] + `}`, false},
		"create none":           {handleDataSourcesCreate, `{"account":"1000001","display_name":"A"}`, false},
		"create two blocks":     {handleDataSourcesCreate, create + `"primary_product":{},"promotion":{"targetCountry":"US","contentLanguage":"en"}}`, false},
		"create no name":        {handleDataSourcesCreate, `{"account":"1000001","primary_product":{}}`, false},
		"create long name":      {handleDataSourcesCreate, `{"account":"1000001","display_name":"` + strings.Repeat("a", 201) + `","primary_product":{}}`, false},
		"create control name":   {handleDataSourcesCreate, `{"account":"1000001","display_name":"a\nb","primary_product":{}}`, false},
		"create label only":     {handleDataSourcesCreate, create + `"primary_product":{"feedLabel":"US"}}`, false},
		"create bad label":      {handleDataSourcesCreate, create + `"primary_product":{"feedLabel":"us","contentLanguage":"en"}}`, false},
		"create bad language":   {handleDataSourcesCreate, create + `"primary_product":{"feedLabel":"US","contentLanguage":"EN"}}`, false},
		"create bad country":    {handleDataSourcesCreate, create + `"primary_product":{"countries":["usa"]}}`, false},
		"create dup country":    {handleDataSourcesCreate, create + `"primary_product":{"countries":["US","US"]}}`, false},
		"create empty country":  {handleDataSourcesCreate, create + `"primary_product":{"countries":[]}}`, false},
		"create bad dest":       {handleDataSourcesCreate, create + `"primary_product":{"destinations":[{"destination":"NOWHERE"}]}}`, false},
		"create unspecified":    {handleDataSourcesCreate, create + `"primary_product":{"destinations":[{"destination":"DESTINATION_ENUM_UNSPECIFIED"}]}}`, false},
		"create bad state":      {handleDataSourcesCreate, create + `"primary_product":{"destinations":[{"destination":"SHOPPING_ADS","state":"PAUSED"}]}}`, false},
		"create rule foreign":   {handleDataSourcesCreate, create + `"primary_product":{"defaultRule":{"takeFromDataSources":[{"supplementalDataSourceId":"accounts/9/dataSources/1"}]}}}`, false},
		"create rule both":      {handleDataSourcesCreate, create + `"primary_product":{"defaultRule":{"takeFromDataSources":[{"self":true,"supplementalDataSourceId":"1"}]}}}`, false},
		"create rule neither":   {handleDataSourcesCreate, create + `"primary_product":{"defaultRule":{"takeFromDataSources":[{}]}}}`, false},
		"create rule empty":     {handleDataSourcesCreate, create + `"primary_product":{"defaultRule":{"takeFromDataSources":[]}}}`, false},
		"create regional bare":  {handleDataSourcesCreate, create + `"regional_inventory":{}}`, false},
		"create promotion bare": {handleDataSourcesCreate, create + `"promotion":{"targetCountry":"US"}}`, false},
		"create local":          {handleDataSourcesCreate, create + `"local_inventory":{"feedLabel":"US","contentLanguage":"en"}}`, false},
		"create review member":  {handleDataSourcesCreate, create + `"product_review":{"x":1}}`, false},
		"create wrong type":     {handleDataSourcesCreate, create + `"primary_product":"x"}`, false},
		"create bad account":    {handleDataSourcesCreate, `{"account":"01","display_name":"A","primary_product":{}}`, false},
		"update nothing":        {handleDataSourcesUpdate, update[:len(update)-1] + `}`, false},
		"update immutable":      {handleDataSourcesUpdate, update + `"feedLabel":"US"}`, false},
		"update language":       {handleDataSourcesUpdate, update + `"contentLanguage":"en"}`, false},
		"update legacy":         {handleDataSourcesUpdate, update + `"legacyLocal":false}`, false},
		"update empty name":     {handleDataSourcesUpdate, update + `"display_name":""}`, false},
		"update empty list":     {handleDataSourcesUpdate, update + `"countries":[]}`, false},
		"update null only":      {handleDataSourcesUpdate, update + `"display_name":null}`, false},
		"update bad id":         {handleDataSourcesUpdate, `{"account":"1000001","data_source":"x","display_name":"A"}`, false},
		"update no id":          {handleDataSourcesUpdate, `{"account":"1000001","display_name":"A"}`, false},
		"update bad country":    {handleDataSourcesUpdate, update + `"countries":["us"]}`, false},
		"update bad dest":       {handleDataSourcesUpdate, update + `"destinations":[{"destination":"X"}]}`, false},
	} {
		h := newHarness(t)
		_, err := c.handler(context.Background(), h.resolved, h.secrets, h.red, json.RawMessage(c.args))
		if !isInvalidRequest(err) {
			t.Errorf("%s: err = %v, want an invalid request", name, err)
			continue
		}
		if c.file && !strings.Contains(err.Error(), "API sources only") {
			t.Errorf("%s: message = %q, want the API-only refusal", name, err)
		}
		for _, leak := range []string{fixtureFetchPassword, "fileInput", "feeds.example"} {
			if strings.Contains(err.Error(), leak) {
				t.Errorf("%s: message quotes %q", name, leak)
			}
		}
		h.untouched(t)
	}
}

func TestDataSourcesUpdateBuildsTheMaskFromTheMembersThatAreSet(t *testing.T) {
	const wantPath = "/datasources/v1/accounts/1000001/dataSources/5"
	for name, c := range map[string]struct{ args, mask, body string }{
		"name": {`"display_name":"New"`, "displayName", `{"displayName":"New"}`},
		"countries": {`"countries":["US"]`, "primaryProductDataSource.countries",
			`{"primaryProductDataSource":{"countries":["US"]}}`},
		"destinations": {`"destinations":[{"destination":"FREE_LISTINGS","state":"DISABLED"}]`,
			"primaryProductDataSource.destinations",
			`{"primaryProductDataSource":{"destinations":[{"destination":"FREE_LISTINGS","state":"DISABLED"}]}}`},
		"rule": {`"default_rule":{"takeFromDataSources":[{"self":true},{"supplementalDataSourceId":"9"}]}`,
			"primaryProductDataSource.defaultRule",
			`{"primaryProductDataSource":{"defaultRule":{"takeFromDataSources":[{"self":true},` +
				`{"supplementalDataSourceName":"accounts/1000001/dataSources/9"}]}}}`},
		"all": {`"display_name":"New","countries":["US"],"destinations":[{"destination":"SHOPPING_ADS"}],` +
			`"default_rule":{"takeFromDataSources":[{"self":true}]}`,
			"displayName,primaryProductDataSource.countries,primaryProductDataSource.destinations,primaryProductDataSource.defaultRule",
			`{"displayName":"New","primaryProductDataSource":{"countries":["US"],"defaultRule":{"takeFromDataSources":[{"self":true}]},` +
				`"destinations":[{"destination":"SHOPPING_ADS"}]}}`},
	} {
		h := newHarness(t)
		h.answerWith(200, `{"name":"accounts/1000001/dataSources/5","displayName":"New"}`)
		out, err := h.call(t, handleDataSourcesUpdate, `{"account":"1000001","data_source":"5",`+c.args+`}`)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		got := h.recorded(t)
		parsed, _ := url.ParseQuery(got[0].query)
		query := parsed.Get("updateMask")
		if len(got) != 1 || got[0].method != "PATCH" || got[0].path != wantPath || query != c.mask || got[0].body != c.body {
			t.Errorf("%s: requests = %+v, want mask %s body %s", name, got, c.mask, c.body)
		}
		if strings.Contains(got[0].query+got[0].body, "fileInput") || strings.Contains(got[0].query+got[0].body, "fetch") {
			t.Errorf("%s: the request touches file input: %+v", name, got[0])
		}
		if out["accepted"] != true {
			t.Errorf("%s: out = %v", name, out)
		}
	}
}

func TestEveryUpdateMaskPathIsOneOfTheFourAndNeverReachesAFileInput(t *testing.T) {
	// Setting every member at once yields the whole set of paths this tool can ever send.
	display := "N"
	mask, _, err := updateRequest("1", updateArguments{DisplayName: &display, Countries: []string{"US"},
		Destinations: []destinationInput{{Destination: "SHOPPING_ADS"}},
		DefaultRule:  &defaultRuleInput{TakeFromDataSources: []referenceInput{{Self: true}}}})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"displayName", "primaryProductDataSource.countries", "primaryProductDataSource.destinations",
		"primaryProductDataSource.defaultRule"}
	if !slices.Equal(mask, want) {
		t.Errorf("mask = %v, want %v", mask, want)
	}
	for _, path := range mask {
		if strings.Contains(strings.ToLower(path), "file") || strings.Contains(strings.ToLower(path), "fetch") {
			t.Errorf("mask path %s reaches file input", path)
		}
	}
}

func TestDataSourceChangesAreNeverRepeatedAndSayWhenTheOutcomeIsOpen(t *testing.T) {
	const create = `{"account":"1000001","display_name":"A","product_review":{}}`
	for name, c := range map[string]struct {
		status int
		class  provider.Class
		open   bool
	}{
		"500":               {500, provider.ClassProviderError, true},
		"503":               {503, provider.ClassUnreachable, true},
		"504":               {504, provider.ClassTimeout, true},
		"429 minute":        {429, provider.ClassRateLimited, false},
		"400":               {400, provider.ClassProviderError, false},
		"401":               {401, provider.ClassAuth, false},
		"403":               {403, provider.ClassPermission, false},
		"409":               {409, provider.ClassProviderError, false},
		"404":               {404, provider.ClassNotFound, false},
		"200 not an object": {200, provider.ClassInvalidResponse, true},
	} {
		for tool, call := range map[string]struct {
			handler capability.Handler
			args    string
		}{
			"create": {handleDataSourcesCreate, create},
			"update": {handleDataSourcesUpdate, `{"account":"1000001","data_source":"5","display_name":"A"}`},
		} {
			h := newHarness(t)
			body := `{"error":{"code":` + "1" + `,"status":"X","message":"google text","details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"QUOTA_REQUEST_RATE_TOO_HIGH"}]}}`
			if c.status == 200 {
				body = `[1]`
			}
			h.answerWith(c.status, body)
			_, err := h.call(t, call.handler, call.args)
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

func TestDataSourceChangeWithATransportFailureIsNotRepeated(t *testing.T) {
	h := newHarness(t)
	h.google.fail = io.ErrUnexpectedEOF
	_, err := h.call(t, handleDataSourcesCreate, `{"account":"1000001","display_name":"A","product_review":{}}`)
	if err == nil || !strings.Contains(err.Error(), "outcome of the change is unknown") || len(h.google.seen()) != 1 {
		t.Errorf("err = %v, requests = %d", err, len(h.google.seen()))
	}
}

func TestA401OnAChangeDropsTheTokenWithoutRenewingItForTheSameCall(t *testing.T) {
	h := newHarness(t)
	h.answerWith(401, `{"error":{"code":401,"status":"UNAUTHENTICATED"}}`)
	if _, err := h.call(t, handleDataSourcesCreate, `{"account":"1000001","display_name":"A","product_review":{}}`); classOf(err) != provider.ClassAuth {
		t.Fatalf("err = %v", err)
	}
	if h.google.grants != 1 || len(h.google.seen()) != 1 {
		t.Errorf("grants = %d, requests = %d, want one each", h.google.grants, len(h.google.seen()))
	}
}

func TestDataSourceRateLimitHoldsTheNextRequestAndIsNotRepeated(t *testing.T) {
	h := newHarness(t)
	h.answerWith(429, `{"error":{"code":429,"status":"RESOURCE_EXHAUSTED","details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"QUOTA_TOO_MANY_REQUESTS"}]}}`)
	_, err := h.call(t, handleDataSourcesCreate, `{"account":"1000001","display_name":"A","product_review":{}}`)
	if classOf(err) != provider.ClassRateLimited || !strings.Contains(err.Error(), "12:00 UTC") || len(h.google.seen()) != 1 {
		t.Errorf("err = %v, requests = %d", err, len(h.google.seen()))
	}
	h.answerWith(200, `{}`)
	if _, err := h.call(t, handleDataSourcesGet, `{"account":"1000001","data_source":"5"}`); err != nil {
		t.Fatal(err)
	}
	if waits := h.spacing.recorded(); len(waits) == 0 {
		t.Error("the request after a 429 did not wait")
	}
}

func TestConfirmationSummariesNameWhatChanges(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		summary func(json.RawMessage) string
		args    string
		want    []string
	}{
		{createSummary, `{"account":"1000001","display_name":"Main","primary_product":{}}`,
			[]string{"Create an API data source", `"Main"`, "primary product", "1000001", "processes it afterwards"}},
		{updateSummary, `{"account":"1000001","data_source":"5","display_name":"New","countries":["US"]}`,
			[]string{"Update data source 5", "1000001", `"New"`, "display name, countries"}},
	} {
		got := c.summary(json.RawMessage(c.args))
		for _, w := range c.want {
			if !strings.Contains(got, w) {
				t.Errorf("summary %q misses %q", got, w)
			}
		}
	}
	for _, id := range []string{dataSourcesCreateID, dataSourcesUpdateID} {
		if d, _, _ := reg.Lookup(id); d.Risk.Confirmation != capability.ConfirmationRequired {
			t.Errorf("%s needs confirmation", id)
		}
	}
}
