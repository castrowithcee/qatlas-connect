package googlemerchant

import (
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

var (
	conversionWriteIDs = []string{conversionSourcesCreateID, conversionSourcesUpdateID}
	conversionReadIDs  = []string{conversionSourcesListID, conversionSourcesGetID}
)

// conversionRequest is one request as the fake Google saw it.
type conversionRequest struct{ method, path, query, body string }

func (h *harness) conversionRequests(t *testing.T) []conversionRequest {
	t.Helper()
	var out []conversionRequest
	for _, req := range h.google.seen() {
		if req.URL.Host != "merchantapi.googleapis.com" || req.URL.Scheme != "https" ||
			req.Header.Get("Authorization") != "Bearer "+testAccess {
			t.Errorf("request left the fixed origin or lacks the bearer token: %s", req.URL.Host)
		}
		body := ""
		if req.Body != nil {
			raw, _ := io.ReadAll(req.Body)
			body = string(raw)
		}
		out = append(out, conversionRequest{req.Method, req.URL.Path, req.URL.RawQuery, body})
	}
	return out
}

const sourceAnswer = `{"name":"accounts/7/conversionSources/mcdn:123","state":"ACTIVE","controller":"MERCHANT",` +
	`"expireTime":"2030-01-01T00:00:00Z","extra":"never held","merchantCenterDestination":{"destination":"MC-1",` +
	`"displayName":"Web store","currencyCode":"EUR","attributionSettings":{"attributionModel":"CROSS_CHANNEL_LAST_CLICK",` +
	`"attributionLookbackWindowDays":30,"conversionType":[{"name":"purchase","report":true},{"name":"signup","report":false}]}}}`

const analyticsAnswer = `{"name":"accounts/7/conversionSources/galk:9","state":"ACTIVE","controller":"MERCHANT",` +
	`"googleAnalyticsLink":{"property":"properties/55","propertyId":"55","attributionSettings":` +
	`{"attributionModel":"CROSS_CHANNEL_DATA_DRIVEN","attributionLookbackWindowDays":7}}}`

func conversionDescriptor(id string) capability.Descriptor {
	for _, d := range descriptors() {
		if d.ID == id {
			return d
		}
	}
	return capability.Descriptor{}
}

func TestConversionMutationsAreConfirmedNotListedOnlyAndOutsideTheReadProfile(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	for id, effect := range map[string]capability.Effect{conversionSourcesCreateID: capability.EffectCreate,
		conversionSourcesUpdateID: capability.EffectUpdate} {
		d, _, ok := reg.Lookup(id)
		if !ok || d.RequiresToolAllowList || d.Version != 1 || d.Risk.Effect != effect ||
			d.Risk.Confirmation != capability.ConfirmationRequired || !d.Risk.OpenWorld {
			t.Errorf("%s = %+v", id, d)
		}
		if slices.Contains(metadata.Profiles[0].Tools, id) {
			t.Errorf("the read profile holds %s", id)
		}
		if reg.ConfirmationSummary(id) == nil {
			t.Errorf("%s has no confirmation summary", id)
		}
	}
	if conversionDescriptor(conversionSourcesCreateID).Risk.Idempotency != capability.IdempotencyNonIdempotent {
		t.Error("create must not be idempotent")
	}
	for _, id := range conversionReadIDs {
		if !slices.Contains(metadata.Profiles[0].Tools, id) {
			t.Errorf("the read profile lacks %s", id)
		}
	}
}

func TestConversionDescriptionsNameDelayAcceptanceAndTheDestinationLimit(t *testing.T) {
	for _, id := range conversionWriteIDs {
		d := conversionDescriptor(id)
		for _, word := range []string{"Only the acceptance", "delay", "Merchant Center destination", "Google Analytics", "untrusted"} {
			if !strings.Contains(d.Description, word) {
				t.Errorf("%s: description lacks %q", id, word)
			}
		}
	}
	if !strings.Contains(conversionDescriptor(conversionSourcesCreateID).Description, "not idempotent") {
		t.Error("create does not say that it is not idempotent")
	}
}

// No input of a tool of this group names an analytics link, and nothing that is sent can hold one.
func TestNoConversionToolTakesOrSendsAnAnalyticsLink(t *testing.T) {
	for _, id := range conversionWriteIDs {
		d := conversionDescriptor(id)
		var schema struct {
			Properties map[string]json.RawMessage `json:"properties"`
			Additional *bool                      `json:"additionalProperties"`
		}
		if err := json.Unmarshal(d.InputSchema, &schema); err != nil || schema.Additional == nil || *schema.Additional {
			t.Fatalf("%s schema is not closed: %v", id, err)
		}
		for name := range schema.Properties {
			if strings.Contains(strings.ToLower(name), "analytics") || strings.Contains(name, "property") {
				t.Errorf("%s takes %s", id, name)
			}
		}
		for _, a := range d.Arguments {
			if strings.Contains(strings.ToLower(a.Name), "analytics") || strings.Contains(a.Name, "property") {
				t.Errorf("%s documents %s", id, a.Name)
			}
		}
	}
	two := 7
	full, err := json.Marshal(conversionWrite{MerchantCenterDestination: destinationWrite{DisplayName: "n",
		CurrencyCode: "EUR", AttributionSettings: &attributionWrite{AttributionModel: "m", AttributionLookbackWindowDays: &two}}})
	if err != nil || strings.Contains(strings.ToLower(string(full)), "analytics") {
		t.Errorf("body = %s, %v", full, err)
	}
}

func TestConversionSourcesListShowsEverySourceAndPagesWithACursor(t *testing.T) {
	h := newHarness(t)
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		if req.URL.Query().Get("pageToken") == "next-1" {
			return 200, `{}`, nil
		}
		return 200, `{"conversionSources":[` + sourceAnswer + `,` + analyticsAnswer + `],"nextPageToken":"next-1"}`, nil
	}
	out, err := h.call(t, handleConversionSourcesList, `{"account":"7","show_deleted":true,"page_size":50}`)
	if err != nil {
		t.Fatal(err)
	}
	rows := out["conversionSources"].([]any)
	first, second := rows[0].(map[string]any), rows[1].(map[string]any)
	dest := first["merchantCenterDestination"].(map[string]any)
	settings := dest["attributionSettings"].(map[string]any)
	types := settings["conversionType"].([]any)
	if len(rows) != 2 || first["state"] != "ACTIVE" || first["controller"] != "MERCHANT" || first["expireTime"] != nil ||
		first["extra"] != nil || dest["destination"] != "MC-1" || dest["displayName"] != "Web store" ||
		dest["currencyCode"] != "EUR" || settings["attributionModel"] != "CROSS_CHANNEL_LAST_CLICK" ||
		settings["attributionLookbackWindowDays"] != float64(30) || len(types) != 2 ||
		types[1].(map[string]any)["report"] != false {
		t.Errorf("first = %v", first)
	}
	link := second["googleAnalyticsLink"].(map[string]any)
	if link["property"] != "properties/55" || link["propertyId"] != "55" ||
		link["attributionSettings"].(map[string]any)["attributionLookbackWindowDays"] != float64(7) {
		t.Errorf("link = %v", link)
	}
	cursor, _ := out["next_cursor"].(string)
	if cursor == "" || cursor == "next-1" {
		t.Fatalf("next_cursor = %q", cursor)
	}
	next, err := h.call(t, handleConversionSourcesList, `{"account":"7","show_deleted":true,"page_size":50,"cursor":"`+cursor+`"}`)
	if err != nil || len(next["conversionSources"].([]any)) != 0 || next["next_cursor"] != nil {
		t.Errorf("next = %v, %v", next, err)
	}
	got := h.conversionRequests(t)
	if len(got) != 2 || got[0].method != "GET" || got[0].path != "/conversions/v1/accounts/7/conversionSources" ||
		got[0].query != "pageSize=50&showDeleted=true" || got[1].query != "pageSize=50&pageToken=next-1&showDeleted=true" {
		t.Errorf("sent = %+v", got)
	}
	if _, err := h.call(t, handleConversionSourcesList, `{"account":"7","cursor":"`+cursor+`"}`); !isInvalidRequest(err) {
		t.Errorf("a cursor of other arguments was accepted: %v", err)
	}
}

func TestConversionSourcesListDefaultsToNoFilterAndReturnsAnEmptyList(t *testing.T) {
	h := newHarness(t)
	out, err := h.call(t, handleConversionSourcesList, `{"account":"7"}`)
	if err != nil {
		t.Fatal(err)
	}
	if rows, ok := out["conversionSources"].([]any); !ok || len(rows) != 0 {
		t.Errorf("out = %v", out)
	}
	if got := h.conversionRequests(t); len(got) != 1 || got[0].query != "" {
		t.Errorf("sent = %+v", got)
	}
}

func TestConversionSourcesGetReadsBothTypesAndHidesTheDeprecatedExpiry(t *testing.T) {
	for id, answer := range map[string]string{"mcdn:123": sourceAnswer, "galk:9": analyticsAnswer} {
		h := newHarness(t)
		h.google.api = func(*http.Request) (int, string, http.Header) { return 200, answer, nil }
		out, err := h.call(t, handleConversionSourcesGet, `{"account":"7","conversion_source":"`+id+`"}`)
		if err != nil || out["state"] != "ACTIVE" || out["expireTime"] != nil || out["extra"] != nil {
			t.Fatalf("%s: out = %v, %v", id, out, err)
		}
		if (id == "galk:9") != (out["googleAnalyticsLink"] != nil) {
			t.Errorf("%s: out = %v", id, out)
		}
		if got := h.conversionRequests(t); len(got) != 1 || got[0].method != "GET" ||
			got[0].path != "/conversions/v1/accounts/7/conversionSources/"+id {
			t.Errorf("sent = %+v", got)
		}
	}
}

func TestConversionSourcesTruncateUntrustedTextAndBoundTheTypeList(t *testing.T) {
	h := newHarness(t)
	var types []string
	for i := 0; i < maxListItems+20; i++ {
		types = append(types, `{"name":"t"}`)
	}
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"merchantCenterDestination":{"displayName":"` + strings.Repeat("n", 1000) +
			`","attributionSettings":{"conversionType":[` + strings.Join(types, ",") + `]}}}`, nil
	}
	out, err := h.call(t, handleConversionSourcesGet, `{"account":"7","conversion_source":"mcdn:1"}`)
	if err != nil {
		t.Fatal(err)
	}
	dest := out["merchantCenterDestination"].(map[string]any)
	if name := dest["displayName"].(string); len([]rune(name)) != maxNameRunes || !strings.HasSuffix(name, "…") {
		t.Errorf("displayName has %d runes", len([]rune(name)))
	}
	if n := len(dest["attributionSettings"].(map[string]any)["conversionType"].([]any)); n != maxListItems {
		t.Errorf("%d conversion types", n)
	}
}

func TestConversionSourcesCreateSendsOnePostWithOnlyDestinationMembers(t *testing.T) {
	h := newHarness(t)
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"name":"accounts/7/conversionSources/mcdn:55","state":"PENDING","extra":1}`, nil
	}
	out, err := h.call(t, handleConversionSourcesCreate, `{"account":"7","display_name":"Web store","currency_code":"EUR",`+
		`"attribution_settings":{"attribution_model":"CROSS_CHANNEL_LAST_CLICK","attribution_lookback_window_days":30}}`)
	if err != nil || out["accepted"] != true || out["name"] != "accounts/7/conversionSources/mcdn:55" ||
		out["state"] != "PENDING" || out["extra"] != nil {
		t.Fatalf("out = %v, %v", out, err)
	}
	got := h.conversionRequests(t)
	want := `{"merchantCenterDestination":{"displayName":"Web store","currencyCode":"EUR","attributionSettings":` +
		`{"attributionModel":"CROSS_CHANNEL_LAST_CLICK","attributionLookbackWindowDays":30}}}`
	if len(got) != 1 || got[0].method != http.MethodPost || got[0].path != "/conversions/v1/accounts/7/conversionSources" ||
		got[0].query != "" || got[0].body != want {
		t.Errorf("sent = %+v", got)
	}
}

func TestConversionSourcesUpdateSendsOnePatchMaskingExactlyTheGivenMembers(t *testing.T) {
	for name, c := range map[string]struct{ args, mask, body string }{
		"name": {`"display_name":"New"`, "merchantCenterDestination.displayName",
			`{"merchantCenterDestination":{"displayName":"New"}}`},
		"currency": {`"currency_code":"USD"`, "merchantCenterDestination.currencyCode",
			`{"merchantCenterDestination":{"currencyCode":"USD"}}`},
		"lookback only": {`"attribution_settings":{"attribution_lookback_window_days":40}`,
			"merchantCenterDestination.attributionSettings.attributionLookbackWindowDays",
			`{"merchantCenterDestination":{"attributionSettings":{"attributionLookbackWindowDays":40}}}`},
		"all": {`"display_name":"N","currency_code":"GBP","attribution_settings":` +
			`{"attribution_model":"CROSS_CHANNEL_LINEAR","attribution_lookback_window_days":7}`,
			"merchantCenterDestination.displayName,merchantCenterDestination.currencyCode," +
				"merchantCenterDestination.attributionSettings.attributionModel," +
				"merchantCenterDestination.attributionSettings.attributionLookbackWindowDays",
			`{"merchantCenterDestination":{"displayName":"N","currencyCode":"GBP","attributionSettings":` +
				`{"attributionModel":"CROSS_CHANNEL_LINEAR","attributionLookbackWindowDays":7}}}`},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.google.api = func(*http.Request) (int, string, http.Header) { return 200, sourceAnswer, nil }
			out, err := h.call(t, handleConversionSourcesUpdate, `{"account":"7","conversion_source":"mcdn:123",`+c.args+`}`)
			if err != nil || out["accepted"] != true || out["name"] != "accounts/7/conversionSources/mcdn:123" ||
				strings.Join(conversionStrings(out["updated"]), ",") != c.mask || out["merchantCenterDestination"] != nil {
				t.Fatalf("out = %v, %v", out, err)
			}
			got := h.conversionRequests(t)
			if len(got) != 1 || got[0].method != http.MethodPatch ||
				got[0].path != "/conversions/v1/accounts/7/conversionSources/mcdn:123" ||
				got[0].query != "updateMask="+strings.NewReplacer(",", "%2C").Replace(c.mask) || got[0].body != c.body {
				t.Errorf("sent = %+v", got)
			}
		})
	}
}

func conversionStrings(v any) []string {
	var out []string
	for _, item := range v.([]any) {
		out = append(out, item.(string))
	}
	return out
}

func TestConversionRefusalsComeBeforeSecretAndRequest(t *testing.T) {
	const attribution = `"attribution_settings":{"attribution_model":"CROSS_CHANNEL_LINEAR","attribution_lookback_window_days":7}`
	for name, c := range map[string]struct {
		handler capability.Handler
		targets []string
		args    string
	}{
		"create no name":           {handleConversionSourcesCreate, nil, `{"account":"7","currency_code":"EUR",` + attribution + `}`},
		"create no attribution":    {handleConversionSourcesCreate, nil, `{"account":"7","display_name":"n","currency_code":"EUR"}`},
		"create partial settings":  {handleConversionSourcesCreate, nil, `{"account":"7","display_name":"n","currency_code":"EUR","attribution_settings":{"attribution_model":"CROSS_CHANNEL_LINEAR"}}`},
		"create lower currency":    {handleConversionSourcesCreate, nil, `{"account":"7","display_name":"n","currency_code":"eur",` + attribution + `}`},
		"create long name":         {handleConversionSourcesCreate, nil, `{"account":"7","display_name":"` + strings.Repeat("n", 65) + `","currency_code":"EUR",` + attribution + `}`},
		"create blank name":        {handleConversionSourcesCreate, nil, `{"account":"7","display_name":"  ","currency_code":"EUR",` + attribution + `}`},
		"create unspecified model": {handleConversionSourcesCreate, nil, `{"account":"7","display_name":"n","currency_code":"EUR","attribution_settings":{"attribution_model":"ATTRIBUTION_MODEL_UNSPECIFIED","attribution_lookback_window_days":7}}`},
		"create bad window":        {handleConversionSourcesCreate, nil, `{"account":"7","display_name":"n","currency_code":"EUR","attribution_settings":{"attribution_model":"CROSS_CHANNEL_LINEAR","attribution_lookback_window_days":14}}`},
		"create analytics link":    {handleConversionSourcesCreate, nil, `{"account":"7","display_name":"n","currency_code":"EUR",` + attribution + `,"google_analytics_link":{"property_id":"5"}}`},
		"create property id":       {handleConversionSourcesCreate, nil, `{"account":"7","display_name":"n","currency_code":"EUR",` + attribution + `,"property_id":"5"}`},
		"create outside":           {handleConversionSourcesCreate, []string{"accounts/1"}, `{"account":"7","display_name":"n","currency_code":"EUR",` + attribution + `}`},
		"create no account":        {handleConversionSourcesCreate, nil, `{"display_name":"n","currency_code":"EUR",` + attribution + `}`},
		"update analytics source":  {handleConversionSourcesUpdate, nil, `{"account":"7","conversion_source":"galk:9","display_name":"n"}`},
		"update other type":        {handleConversionSourcesUpdate, nil, `{"account":"7","conversion_source":"abcd:9","display_name":"n"}`},
		"update no source":         {handleConversionSourcesUpdate, nil, `{"account":"7","display_name":"n"}`},
		"update path injection":    {handleConversionSourcesUpdate, nil, `{"account":"7","conversion_source":"mcdn:1/../x","display_name":"n"}`},
		"update undelete suffix":   {handleConversionSourcesUpdate, nil, `{"account":"7","conversion_source":"mcdn:1:undelete","display_name":"n"}`},
		"update nothing":           {handleConversionSourcesUpdate, nil, `{"account":"7","conversion_source":"mcdn:1"}`},
		"update empty settings":    {handleConversionSourcesUpdate, nil, `{"account":"7","conversion_source":"mcdn:1","attribution_settings":{}}`},
		"update analytics link":    {handleConversionSourcesUpdate, nil, `{"account":"7","conversion_source":"mcdn:1","display_name":"n","google_analytics_link":{}}`},
		"update outside":           {handleConversionSourcesUpdate, []string{"accounts/1"}, `{"account":"7","conversion_source":"mcdn:1","display_name":"n"}`},
		"get bad id":               {handleConversionSourcesGet, nil, `{"account":"7","conversion_source":"mcdn:1/x"}`},
		"get no id":                {handleConversionSourcesGet, nil, `{"account":"7"}`},
		"get outside":              {handleConversionSourcesGet, []string{"accounts/1"}, `{"account":"7","conversion_source":"mcdn:1"}`},
		"list outside":             {handleConversionSourcesList, []string{"accounts/1"}, `{"account":"7"}`},
		"list page size":           {handleConversionSourcesList, nil, `{"account":"7","page_size":201}`},
		"list bad cursor":          {handleConversionSourcesList, nil, `{"account":"7","cursor":"junk"}`},
		"list path injection":      {handleConversionSourcesList, nil, `{"account":"7/x"}`},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, c.targets...)
			if _, err := h.call(t, c.handler, c.args); !isInvalidRequest(err) {
				t.Fatalf("err = %v, want an invalid request", err)
			}
			h.untouched(t)
		})
	}
}

// A source Google reports with an analytics link is not read first: the ID type decides, and a failure of
// Google for a source that is not a destination is reported by class.
func TestUpdateOfASourceGoogleRefusesIsReportedByClass(t *testing.T) {
	h := newHarness(t)
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 400, `{"error":{"code":400,"status":"INVALID_ARGUMENT","message":"synthetic provider echo"}}`, nil
	}
	_, err := h.call(t, handleConversionSourcesUpdate, `{"account":"7","conversion_source":"mcdn:1","display_name":"n"}`)
	if err == nil || strings.Contains(err.Error(), "synthetic provider echo") {
		t.Fatalf("err = %v", err)
	}
	got := h.conversionRequests(t)
	if len(got) != 1 || got[0].method != http.MethodPatch {
		t.Errorf("sent = %+v: no read of the source goes before the patch", got)
	}
}

func TestConversionWritesProveASubAccountBeforeTheRequest(t *testing.T) {
	const args = `{"account":"7","display_name":"n","currency_code":"EUR","attribution_settings":` +
		`{"attribution_model":"CROSS_CHANNEL_LINEAR","attribution_lookback_window_days":7}}`
	h := newHarness(t, "advanced-accounts/1")
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		if strings.HasSuffix(req.URL.Path, ":listSubaccounts") {
			return 200, `{"accounts":[` + accountRow("7", "Seven") + `]}`, nil
		}
		return 200, `{}`, nil
	}
	if _, err := h.call(t, handleConversionSourcesCreate, args); err != nil {
		t.Fatal(err)
	}
	got := h.conversionRequests(t)
	if len(got) != 2 || got[0].method != "GET" || !strings.HasSuffix(got[0].path, ":listSubaccounts") ||
		got[1].method != http.MethodPost {
		t.Fatalf("sent = %+v", got)
	}
	h = newHarness(t, "advanced-accounts/1")
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, `{"accounts":[]}`, nil }
	if _, err := h.call(t, handleConversionSourcesCreate, args); !isInvalidRequest(err) {
		t.Fatalf("err = %v", err)
	}
	for _, r := range h.conversionRequests(t) {
		if r.method != "GET" {
			t.Errorf("a %s went out for an unproven account", r.method)
		}
	}
}

func TestConversionMutationsAreNeverRepeatedAndFailuresAreMapped(t *testing.T) {
	for name, c := range map[string]struct {
		status int
		body   string
		class  provider.Class
	}{
		"401": {401, `{"error":{"code":401,"status":"UNAUTHENTICATED"}}`, provider.ClassAuth},
		"403": {403, googleErrorBody, provider.ClassPermission},
		"404": {404, `{"error":{"code":404,"status":"NOT_FOUND"}}`, provider.ClassNotFound},
		"429": {429, quotaBody(reasonDayQuota), provider.ClassRateLimited},
		"500": {500, `{}`, provider.ClassProviderError},
		"503": {503, `{}`, provider.ClassUnreachable},
		"504": {504, `{}`, provider.ClassTimeout},
	} {
		for tool, call := range map[string]struct {
			handler capability.Handler
			args    string
		}{
			"create": {handleConversionSourcesCreate, `{"account":"7","display_name":"n","currency_code":"EUR","attribution_settings":` +
				`{"attribution_model":"CROSS_CHANNEL_LINEAR","attribution_lookback_window_days":7}}`},
			"update": {handleConversionSourcesUpdate, `{"account":"7","conversion_source":"mcdn:1","display_name":"n"}`},
		} {
			t.Run(tool+" "+name, func(t *testing.T) {
				h := newHarness(t)
				h.google.api = func(*http.Request) (int, string, http.Header) { return c.status, c.body, nil }
				_, err := h.call(t, call.handler, call.args)
				if classOf(err) != c.class {
					t.Fatalf("err = %v, want class %s", err, c.class)
				}
				if n := len(h.google.seen()); n != 1 {
					t.Errorf("%d requests, want exactly one", n)
				}
			})
		}
	}
}

func TestConversionSummariesNameTheChange(t *testing.T) {
	got := conversionCreateSummary([]byte(`{"account":"7","display_name":"Web store","currency_code":"EUR"}`))
	for _, want := range []string{"Merchant Center destination", "7", "Web store", "EUR", "second source", "delay"} {
		if !strings.Contains(got, want) {
			t.Errorf("create summary %q lacks %q", got, want)
		}
	}
	got = conversionUpdateSummary([]byte(`{"account":"7","conversion_source":"mcdn:1","currency_code":"USD",` +
		`"attribution_settings":{"attribution_lookback_window_days":40}}`))
	for _, want := range []string{"mcdn:1", "7", "USD", "40 days", "delay"} {
		if !strings.Contains(got, want) {
			t.Errorf("update summary %q lacks %q", got, want)
		}
	}
}

func TestConversionToolsAreOfferedByEffectOnAConnection(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	effects := map[string]config.Effect{}
	for _, tool := range metadata.Tools {
		effects[tool.ID] = tool.Effect
		if slices.Contains(conversionWriteIDs, tool.ID) && tool.RequiresToolAllowList {
			t.Errorf("%s is listed-only", tool.ID)
		}
	}
	if effects[conversionSourcesCreateID] != config.EffectCreate || effects[conversionSourcesUpdateID] != config.EffectUpdate {
		t.Errorf("effects = %v", effects)
	}
}

func TestConversionRemovalToolsAreListedOnlyConfirmedAndOutsideTheProfile(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	for id, effect := range map[string]capability.Effect{conversionSourcesUndeleteID: capability.EffectUpdate,
		conversionSourcesDeleteID: capability.EffectDelete} {
		d, _, ok := reg.Lookup(id)
		if !ok || !d.RequiresToolAllowList || d.Version != 1 || d.Risk.Effect != effect ||
			d.Risk.Confirmation != capability.ConfirmationRequired || !d.Risk.OpenWorld {
			t.Errorf("%s = %+v", id, d)
		}
		if slices.Contains(metadata.Profiles[0].Tools, id) {
			t.Errorf("the read profile holds %s", id)
		}
		if reg.ConfirmationSummary(id) == nil {
			t.Errorf("%s has no confirmation summary", id)
		}
	}
	del := conversionDescriptor(conversionSourcesDeleteID).Description
	for _, word := range []string{"archived", "30 days", "immediately", "delay"} {
		if !strings.Contains(del, word) {
			t.Errorf("delete description lacks %q", word)
		}
	}
	if !strings.Contains(conversionDescriptor(conversionSourcesUndeleteID).Description, "only for a source of the type Merchant Center destination") {
		t.Error("undelete does not name the destination limit")
	}
	got := conversionDeleteSummary([]byte(`{"account":"7","conversion_source":"galk:9"}`))
	for _, want := range []string{"galk:9", "7", "archived", "30 days", "immediately"} {
		if !strings.Contains(got, want) {
			t.Errorf("delete summary %q lacks %q", got, want)
		}
	}
	got = conversionUndeleteSummary([]byte(`{"account":"7","conversion_source":"mcdn:1"}`))
	if !strings.Contains(got, "only for Merchant Center destinations") {
		t.Errorf("undelete summary %q", got)
	}
}

func TestConversionSourcesUndeleteAndDeleteSendOneRequest(t *testing.T) {
	h := newHarness(t)
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"name":"accounts/7/conversionSources/mcdn:123","state":"ACTIVE","extra":1}`, nil
	}
	out, err := h.call(t, handleConversionSourcesUndelete, `{"account":"7","conversion_source":"mcdn:123"}`)
	if err != nil || out["accepted"] != true || out["state"] != "ACTIVE" || out["extra"] != nil {
		t.Fatalf("out = %v, %v", out, err)
	}
	got := h.conversionRequests(t)
	if len(got) != 1 || got[0].method != http.MethodPost || got[0].body != "" ||
		got[0].path != "/conversions/v1/accounts/7/conversionSources/mcdn:123:undelete" {
		t.Errorf("sent = %+v", got)
	}
	for _, src := range []string{"mcdn:123", "galk:9"} {
		h = newHarness(t)
		h.google.api = func(*http.Request) (int, string, http.Header) { return 200, `{}`, nil }
		out, err = h.call(t, handleConversionSourcesDelete, `{"account":"7","conversion_source":"`+src+`"}`)
		if err != nil || out["deleted"] != true || out["conversion_source"] != src || out["account"] != "7" {
			t.Fatalf("out = %v, %v", out, err)
		}
		got = h.conversionRequests(t)
		if len(got) != 1 || got[0].method != http.MethodDelete || got[0].body != "" ||
			got[0].path != "/conversions/v1/accounts/7/conversionSources/"+src {
			t.Errorf("sent = %+v", got)
		}
	}
}

func TestConversionRemovalRefusalsComeBeforeSecretAndRequest(t *testing.T) {
	for name, c := range map[string]struct {
		handler capability.Handler
		targets []string
		args    string
	}{
		"undelete analytics link": {handleConversionSourcesUndelete, nil, `{"account":"7","conversion_source":"galk:9"}`},
		"undelete no account":     {handleConversionSourcesUndelete, nil, `{"conversion_source":"mcdn:1"}`},
		"undelete injection":      {handleConversionSourcesUndelete, nil, `{"account":"7","conversion_source":"mcdn:1/x"}`},
		"undelete outside":        {handleConversionSourcesUndelete, []string{"accounts/1"}, `{"account":"7","conversion_source":"mcdn:1"}`},
		"undelete extra member":   {handleConversionSourcesUndelete, nil, `{"account":"7","conversion_source":"mcdn:1","property_id":"5"}`},
		"delete other type":       {handleConversionSourcesDelete, nil, `{"account":"7","conversion_source":"abcd:9"}`},
		"delete no account":       {handleConversionSourcesDelete, nil, `{"conversion_source":"mcdn:1"}`},
		"delete no source":        {handleConversionSourcesDelete, nil, `{"account":"7"}`},
		"delete injection":        {handleConversionSourcesDelete, nil, `{"account":"7","conversion_source":"mcdn:1:undelete"}`},
		"delete outside":          {handleConversionSourcesDelete, []string{"accounts/1"}, `{"account":"7","conversion_source":"mcdn:1"}`},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, c.targets...)
			if _, err := h.call(t, c.handler, c.args); !isInvalidRequest(err) {
				t.Fatalf("err = %v, want an invalid request", err)
			}
			h.untouched(t)
		})
	}
}

func TestConversionRemovalsAreNeverRepeatedAndFailuresAreMapped(t *testing.T) {
	for name, c := range map[string]struct {
		status int
		body   string
		class  provider.Class
	}{
		"401": {401, `{"error":{"code":401,"status":"UNAUTHENTICATED"}}`, provider.ClassAuth},
		"403": {403, googleErrorBody, provider.ClassPermission},
		"404": {404, `{"error":{"code":404,"status":"NOT_FOUND"}}`, provider.ClassNotFound},
		"429": {429, quotaBody(reasonDayQuota), provider.ClassRateLimited},
		"500": {500, `{}`, provider.ClassProviderError},
		"503": {503, `{}`, provider.ClassUnreachable},
	} {
		for tool, handler := range map[string]capability.Handler{"undelete": handleConversionSourcesUndelete,
			"delete": handleConversionSourcesDelete} {
			t.Run(tool+" "+name, func(t *testing.T) {
				h := newHarness(t)
				h.google.api = func(*http.Request) (int, string, http.Header) { return c.status, c.body, nil }
				_, err := h.call(t, handler, `{"account":"7","conversion_source":"mcdn:1"}`)
				if classOf(err) != c.class {
					t.Fatalf("err = %v, want class %s", err, c.class)
				}
				if n := len(h.google.seen()); n != 1 {
					t.Errorf("%d requests, want exactly one", n)
				}
			})
		}
	}
}
