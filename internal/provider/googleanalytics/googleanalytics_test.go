package googleanalytics

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/provider/googleapi"
)

const twoAccounts = `{"accountSummaries":[` +
	`{"name":"accountSummaries/100","account":"accounts/100","displayName":"Shop","propertySummaries":[` +
	`{"property":"properties/200","displayName":"Shop web","propertyType":"PROPERTY_TYPE_ORDINARY",` +
	`"parent":"accounts/100","canEdit":true},` +
	`{"property":"properties/201","displayName":"Shop sub","propertyType":"PROPERTY_TYPE_SUBPROPERTY",` +
	`"parent":"properties/200"}]},` +
	`{"name":"accountSummaries/900","account":"accounts/900","displayName":"Other","propertySummaries":[` +
	`{"property":"properties/901","displayName":"Other web","propertyType":"PROPERTY_TYPE_ORDINARY",` +
	`"parent":"accounts/900"},` +
	`{"property":"properties/902","displayName":"Other app","propertyType":"PROPERTY_TYPE_ORDINARY",` +
	`"parent":"accounts/900"}]}]}`

type summariesOut struct {
	AccountSummaries []struct {
		Name, Account, DisplayName string
		PropertySummaries          []struct {
			Property, DisplayName, PropertyType, Parent string
			CanEdit                                     bool
		}
	} `json:"accountSummaries"`
	NextCursor string `json:"next_cursor"`
	HasMore    bool   `json:"has_more"`
}

func decodeSummaries(t *testing.T, result string) summariesOut {
	t.Helper()
	var out summariesOut
	if err := json.Unmarshal([]byte(result), &out); err != nil {
		t.Fatalf("result %s: %v", result, err)
	}
	return out
}

// toolEffects is the effect of each tool of the provider; a tool that is not listed reads.
var toolEffects = map[string]config.Effect{accountsUpdateID: config.EffectUpdate, accountsDeleteID: config.EffectDelete,
	propertiesCreateID: config.EffectCreate, propertiesUpdateID: config.EffectUpdate, propertiesDeleteID: config.EffectDelete,
	dataStreamsCreateID: config.EffectCreate, dataStreamsUpdateID: config.EffectUpdate,
	dataStreamsDeleteID: config.EffectDelete, mpSecretsDeleteID: config.EffectDelete, audienceCreateID: config.EffectCreate,
	reportTasksCreateID: config.EffectCreate,
	enhancedUpdateID:    config.EffectUpdate, redactionUpdateID: config.EffectUpdate,
	customDimensionsCreateID: config.EffectCreate, customDimensionsUpdateID: config.EffectUpdate,
	customDimensionsArchiveID: config.EffectDelete, customMetricsCreateID: config.EffectCreate,
	customMetricsUpdateID: config.EffectUpdate, customMetricsArchiveID: config.EffectDelete, keyEventsCreateID: config.EffectCreate,
	keyEventsUpdateID: config.EffectUpdate, keyEventsDeleteID: config.EffectDelete,
	eventRulesCreateID: config.EffectCreate, eventRulesUpdateID: config.EffectUpdate, eventRulesDeleteID: config.EffectDelete,
	calculatedCreateID: config.EffectCreate, calculatedUpdateID: config.EffectUpdate, calculatedDeleteID: config.EffectDelete}

func effectOf(id string) config.Effect {
	if effect, ok := toolEffects[id]; ok {
		return effect
	}
	return config.EffectRead
}

func TestRegisterPublishesMetadataScopesAndConsent(t *testing.T) {
	reg := registry(t)
	metadata, ok := reg.ProviderMetadata(Provider)
	if !ok || metadata.Name != "Google Analytics" || metadata.DefaultBaseURL != adminRoot ||
		!reflect.DeepEqual(metadata.SecretRoles, config.TokenSecretRoles()) || metadata.Token == nil ||
		metadata.DocPath() != "google.analytics/setup.md" {
		t.Fatalf("metadata = %+v", metadata)
	}
	if !reflect.DeepEqual(metadata.Token.Scopes, map[string][]string{summariesID: {scopeReadOnly},
		accountsID: {scopeReadOnly}, propertiesListID: {scopeReadOnly}, propertiesGetID: {scopeReadOnly},
		reportsRunID: {scopeReadOnly}, metadataGetID: {scopeReadOnly}, compatibilityID: {scopeReadOnly},
		reportsRunPivotID: {scopeReadOnly}, reportsBatchRunID: {scopeReadOnly}, reportsBatchRunPivotID: {scopeReadOnly},
		reportsRunRealtimeID: {scopeReadOnly}, accountsGetID: {scopeReadOnly}, accountsSharingID: {scopeReadOnly}, accountsUpdateID: {scopeEdit},
		accountsDeleteID: {scopeEdit}, propertiesCreateID: {scopeEdit}, propertiesUpdateID: {scopeEdit},
		propertiesDeleteID: {scopeEdit}, dataStreamsListID: {scopeReadOnly}, dataStreamsGetID: {scopeReadOnly},
		dataStreamsCreateID: {scopeEdit}, dataStreamsUpdateID: {scopeEdit}, dataStreamsDeleteID: {scopeEdit},
		mpSecretsListID: {scopeReadOnly}, mpSecretsGetID: {scopeReadOnly}, mpSecretsDeleteID: {scopeEdit}, funnelRunID: {scopeReadOnly},
		audienceGetID: {scopeReadOnly}, audienceListID: {scopeReadOnly}, audienceCreateID: {scopeReadOnly},
		audienceQueryID: {scopeReadOnly}, reportTasksCreateID: {scopeReadOnly}, reportTasksGetID: {scopeReadOnly},
		reportTasksListID: {scopeReadOnly}, reportTasksQueryID: {scopeReadOnly}, enhancedGetID: {scopeReadOnly},
		redactionGetID: {scopeReadOnly}, siteTagGetID: {scopeReadOnly}, enhancedUpdateID: {scopeEdit},
		redactionUpdateID: {scopeEdit}, customDimensionsListID: {scopeReadOnly}, customDimensionsGetID: {scopeReadOnly},
		customDimensionsCreateID: {scopeEdit}, customDimensionsUpdateID: {scopeEdit},
		customDimensionsArchiveID: {scopeEdit},
		customMetricsListID:       {scopeReadOnly}, customMetricsGetID: {scopeReadOnly}, customMetricsCreateID: {scopeEdit},
		customMetricsUpdateID: {scopeEdit}, customMetricsArchiveID: {scopeEdit},
		keyEventsListID: {scopeReadOnly}, keyEventsGetID: {scopeReadOnly}, keyEventsCreateID: {scopeEdit},
		keyEventsUpdateID: {scopeEdit}, keyEventsDeleteID: {scopeEdit},
		eventRulesListID: {scopeReadOnly}, eventRulesGetID: {scopeReadOnly},
		eventRulesCreateID: {scopeEdit}, eventRulesUpdateID: {scopeEdit}, eventRulesDeleteID: {scopeEdit},
		calculatedListID: {scopeReadOnly}, calculatedGetID: {scopeReadOnly},
		calculatedCreateID: {scopeEdit}, calculatedUpdateID: {scopeEdit}, calculatedDeleteID: {scopeEdit},
		accessAccountID: {scopeReadOnly}, accessPropertyID: {scopeReadOnly}}) {
		t.Errorf("scopes = %v", metadata.Token.Scopes)
	}
	ids := []string{}
	for _, tool := range metadata.Tools {
		if want := effectOf(tool.ID); tool.Effect != want {
			t.Errorf("tool %s effect = %s, want %s", tool.ID, tool.Effect, want)
		}
		ids = append(ids, tool.ID)
	}
	slices.Sort(ids)
	if !slices.Equal(ids, sortedCopy([]string{"google.analytics.accounts.delete", "google.analytics.accounts.get",
		"google.analytics.accounts.get-data-sharing-settings", "google.analytics.accounts.list",
		"google.analytics.accounts.list-summaries", "google.analytics.accounts.update",
		"google.analytics.audience-exports.create", "google.analytics.audience-exports.get",
		"google.analytics.audience-exports.list", "google.analytics.audience-exports.query",
		"google.analytics.custom-dimensions.archive", "google.analytics.custom-dimensions.create",
		"google.analytics.custom-dimensions.get", "google.analytics.custom-dimensions.list",
		"google.analytics.custom-dimensions.update",
		"google.analytics.custom-metrics.archive", "google.analytics.custom-metrics.create",
		"google.analytics.custom-metrics.get", "google.analytics.custom-metrics.list",
		"google.analytics.custom-metrics.update",
		"google.analytics.data-streams.create", "google.analytics.data-streams.delete", "google.analytics.data-streams.get",
		"google.analytics.data-streams.list", "google.analytics.data-streams.update",
		"google.analytics.data-streams.get-enhanced-measurement", "google.analytics.data-streams.update-enhanced-measurement",
		"google.analytics.data-streams.get-data-redaction", "google.analytics.data-streams.update-data-redaction",
		"google.analytics.data-streams.get-site-tag",
		"google.analytics.event-create-rules.list", "google.analytics.event-create-rules.get",
		"google.analytics.event-create-rules.create", "google.analytics.event-create-rules.update",
		"google.analytics.event-create-rules.delete",
		"google.analytics.measurement-protocol-secrets.delete", "google.analytics.measurement-protocol-secrets.get",
		"google.analytics.measurement-protocol-secrets.list",
		"google.analytics.key-events.create", "google.analytics.key-events.delete", "google.analytics.key-events.get",
		"google.analytics.key-events.list", "google.analytics.key-events.update",
		"google.analytics.calculated-metrics.list",
		"google.analytics.calculated-metrics.get", "google.analytics.calculated-metrics.create",
		"google.analytics.calculated-metrics.update", "google.analytics.calculated-metrics.delete",
		"google.analytics.properties.create", "google.analytics.properties.delete", "google.analytics.properties.get",
		"google.analytics.properties.list", "google.analytics.properties.update", "google.analytics.report-tasks.create",
		"google.analytics.report-tasks.get", "google.analytics.report-tasks.list", "google.analytics.report-tasks.query",
		"google.analytics.reports.check-compatibility", "google.analytics.reports.get-metadata",
		"google.analytics.reports.run", "google.analytics.reports.run-funnel", "google.analytics.reports.batch-run",
		"google.analytics.reports.batch-run-pivot", "google.analytics.reports.run-pivot",
		"google.analytics.reports.run-realtime", "google.analytics.accounts.run-access-report",
		"google.analytics.properties.run-access-report"})) {
		t.Errorf("tools = %v", ids)
	}
	reads := []string{summariesID, accountsID, accountsGetID, accountsSharingID, propertiesListID, propertiesGetID,
		reportsRunID, metadataGetID, compatibilityID, dataStreamsListID, dataStreamsGetID, funnelRunID, audienceGetID,
		audienceListID, reportsRunPivotID, reportsBatchRunID, reportsBatchRunPivotID, reportsRunRealtimeID,
		reportTasksGetID, reportTasksListID, reportTasksQueryID, audienceQueryID, mpSecretsListID, mpSecretsGetID,
		enhancedGetID, redactionGetID, siteTagGetID,
		customDimensionsListID, customDimensionsGetID, customMetricsListID,
		customMetricsGetID, keyEventsListID, keyEventsGetID, eventRulesListID, eventRulesGetID,
		calculatedListID, calculatedGetID, accessAccountID, accessPropertyID}
	if len(metadata.Profiles) != 2 || metadata.Profiles[0].ID != "read" || !metadata.Profiles[0].Recommended ||
		!reflect.DeepEqual(metadata.Profiles[0].Tools, reads) || metadata.Profiles[1].ID != "write" ||
		metadata.Profiles[1].Recommended ||
		!reflect.DeepEqual(metadata.Profiles[1].Tools,
			append(slices.Clone(reads), accountsUpdateID, propertiesCreateID, propertiesUpdateID,
				dataStreamsCreateID, dataStreamsUpdateID, dataStreamsDeleteID, mpSecretsDeleteID, audienceCreateID,
				reportTasksCreateID, enhancedUpdateID, redactionUpdateID, customDimensionsCreateID,
				customDimensionsUpdateID, customDimensionsArchiveID,
				customMetricsCreateID, customMetricsUpdateID, customMetricsArchiveID,
				keyEventsCreateID, keyEventsUpdateID, keyEventsDeleteID, eventRulesCreateID, eventRulesUpdateID, eventRulesDeleteID,
				calculatedCreateID, calculatedUpdateID, calculatedDeleteID)) {
		t.Errorf("profiles = %+v", metadata.Profiles)
	}
	oauth, ok := reg.OAuth(Provider)
	if !ok || oauth.TokenURL != googleapi.OAuth.TokenURL || oauth.AuthorizeURL != googleapi.OAuth.AuthorizeURL ||
		oauth.ImpersonateURL != googleapi.OAuth.ImpersonateURL {
		t.Errorf("consent endpoints = %+v, %v", oauth, ok)
	}
	for _, descriptor := range reg.Provider(Provider) {
		sensitivity := dataSensitivity
		if slices.Contains([]string{reportsRunID, metadataGetID, compatibilityID, funnelRunID, audienceGetID, audienceListID,
			reportsRunPivotID, reportsBatchRunID, reportsBatchRunPivotID, reportsRunRealtimeID,
			reportTasksGetID, reportTasksListID, reportTasksQueryID}, descriptor.ID) {
			sensitivity = dataSensitivityReports
		}
		if descriptor.ID == audienceQueryID {
			sensitivity = dataSensitivityAudienceUsers
		}
		if descriptor.ID == accessAccountID || descriptor.ID == accessPropertyID {
			sensitivity = dataSensitivityAccess
		}
		if effectOf(descriptor.ID) == config.EffectRead && descriptor.Risk != (capability.Risk{
			Effect: capability.EffectRead, Idempotency: capability.IdempotencySafe,
			Confirmation: capability.ConfirmationNone, OpenWorld: true, DataSensitivity: sensitivity}) {
			t.Errorf("%s risk = %+v", descriptor.ID, descriptor.Risk)
		}
		if !strings.Contains(descriptor.Description, "untrusted") {
			t.Errorf("%s description lacks the untrusted note", descriptor.ID)
		}
	}
	for _, target := range []string{"accounts/1", "accounts/1234567890123456789", "properties/200"} {
		if err := metadata.Target.Validate(target); err != nil {
			t.Errorf("Validate(%q) = %v", target, err)
		}
	}
	for _, target := range []string{"", "accounts/", "accounts/0", "accounts/01", "accounts/12345678901234567890",
		"accounts/1/x", "properties/x", "property/1", "accountSummaries/1", " accounts/1", "accounts/-1"} {
		if err := metadata.Target.Validate(target); err == nil || strings.Contains(err.Error(), target+"\"") {
			t.Errorf("Validate(%q) = %v, want a refusal that does not quote it", target, err)
		}
	}
	if metadata.ValidateBaseURL("") != nil || metadata.ValidateBaseURL(adminRoot+"/") != nil ||
		metadata.ValidateBaseURL("https://analyticsadmin.googleapis.com.evil.example") == nil ||
		metadata.ValidateBaseURL("http://analyticsadmin.googleapis.com") == nil {
		t.Error("base URL validation does not pin the Admin API origin")
	}
}

func TestListSummariesSendsTheTokenOnlyInTheHeaderAndReturnsEveryField(t *testing.T) {
	env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, twoAccounts)})
	result, err := env.invoke("read", `{"page_size":5}`)
	if err != nil {
		t.Fatal(err)
	}
	requests, grants := env.fake.seen()
	if grants != 1 || len(requests) != 1 {
		t.Fatalf("grants = %d, requests = %+v", grants, requests)
	}
	r := requests[0]
	if r.method != http.MethodGet || r.path != "/v1beta/accountSummaries" || r.rawQuery != "pageSize=5" ||
		r.auth != "Bearer "+accessPrefix+"1" || r.accept != "application/json" {
		t.Errorf("request = %+v", r)
	}
	form := env.fake.tokenForms[0]
	if form.Get("grant_type") != "refresh_token" || form.Get("refresh_token") != refreshValue ||
		form.Get("client_id") != clientIDValue || form.Has("scope") {
		t.Errorf("token request = %v", form)
	}
	for _, u := range env.fake.urls {
		if leak := leaks(u); leak != "" {
			t.Errorf("URL %s carries %s", u, leak)
		}
	}
	if leak := leaks(result); leak != "" {
		t.Errorf("result carries %s", leak)
	}
	if masked := env.red.Apply("token " + accessPrefix + "1"); strings.Contains(masked, accessPrefix) {
		t.Errorf("the access token is not registered with the redactor: %s", masked)
	}
	out := decodeSummaries(t, result)
	if out.HasMore || out.NextCursor != "" || len(out.AccountSummaries) != 2 {
		t.Fatalf("result = %s", result)
	}
	first := out.AccountSummaries[0]
	if first.Name != "accountSummaries/100" || first.Account != "accounts/100" || first.DisplayName != "Shop" ||
		len(first.PropertySummaries) != 2 {
		t.Errorf("first = %+v", first)
	}
	p := first.PropertySummaries[0]
	if p.Property != "properties/200" || p.DisplayName != "Shop web" || p.PropertyType != "PROPERTY_TYPE_ORDINARY" ||
		p.Parent != "accounts/100" || !p.CanEdit {
		t.Errorf("property summary = %+v", p)
	}
	if sub := first.PropertySummaries[1]; sub.Parent != "properties/200" || sub.CanEdit {
		t.Errorf("subproperty summary = %+v", sub)
	}
}

func TestListSummariesBoundsAndDropsProviderText(t *testing.T) {
	long := strings.Repeat("ä", 400)
	env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"accountSummaries":[{"name":"accountSummaries/1",`+
		`"account":"https://evil.example/accounts/1","displayName":"`+long+`","propertySummaries":[`+
		`{"property":"properties/2","propertyType":"A B","parent":"accounts/1"}]}]}`)})
	result, err := env.invoke("read", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	out := decodeSummaries(t, result)
	summary := out.AccountSummaries[0]
	if summary.Account != "" || len([]rune(summary.DisplayName)) != maxTextRunes+1 ||
		summary.PropertySummaries[0].PropertyType != "" {
		t.Errorf("summary = %+v", summary)
	}
}

func TestListSummariesEmptyAnswerIsAnEmptyList(t *testing.T) {
	env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, `{}`)})
	result, err := env.invoke("read", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	// The core leaves an empty list out of the result; missing means empty.
	if out := decodeSummaries(t, result); len(out.AccountSummaries) != 0 || out.HasMore {
		t.Errorf("result = %s", result)
	}
	direct, err := handleAccountSummaries(context.Background(), env.resolved(t, "read"), env.secrets(), env.red,
		json.RawMessage(`{}`))
	if err != nil || direct.(summariesResult).AccountSummaries == nil {
		t.Errorf("handler result = %#v, %v, want an empty list", direct, err)
	}
}

func TestListSummariesPagesThroughThreePages(t *testing.T) {
	pages := map[string]string{
		"":        `{"accountSummaries":[{"name":"accountSummaries/1","account":"accounts/1"}],"nextPageToken":"tok-2"}`,
		"tok-2":   `{"accountSummaries":[{"name":"accountSummaries/2","account":"accounts/2"}],"nextPageToken":"tok-3/="}`,
		"tok-3/=": `{"accountSummaries":[{"name":"accountSummaries/3","account":"accounts/3"}]}`,
	}
	env := newEnv(t, &fakeGoogle{api: func(_ int, r *http.Request) (*http.Response, error) {
		return respond(r, http.StatusOK, "application/json", pages[r.URL.Query().Get("pageToken")]), nil
	}})
	var accounts []string
	arguments := `{"page_size":1}`
	for page := 1; ; page++ {
		result, err := env.invoke("read", arguments)
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		out := decodeSummaries(t, result)
		for _, summary := range out.AccountSummaries {
			accounts = append(accounts, summary.Account)
		}
		if !out.HasMore {
			if out.NextCursor != "" || page != 3 {
				t.Fatalf("page %d ends with %s", page, result)
			}
			break
		}
		if strings.Contains(out.NextCursor, "tok-") {
			t.Errorf("the cursor shows the page token: %s", out.NextCursor)
		}
		// The page size may change between pages.
		arguments = `{"page_size":2,"cursor":"` + out.NextCursor + `"}`
	}
	if strings.Join(accounts, " ") != "accounts/1 accounts/2 accounts/3" {
		t.Errorf("accounts = %v", accounts)
	}
	requests, grants := env.fake.seen()
	if len(requests) != 3 || requests[1].rawQuery != "pageSize=2&pageToken=tok-2" ||
		requests[2].rawQuery != "pageSize=2&pageToken=tok-3%2F%3D" {
		t.Errorf("requests = %+v", requests)
	}
	if grants != 1 {
		t.Errorf("grants = %d, want the cached token to serve every page", grants)
	}
}

func TestManipulatedCursorIsRefusedBeforeAnySecretOrRequest(t *testing.T) {
	env := newEnv(t, &fakeGoogle{api: always(http.StatusOK,
		`{"accountSummaries":[],"nextPageToken":"tok-2"}`)})
	result, err := env.invoke("read", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	cursor := decodeSummaries(t, result).NextCursor
	raw, _ := base64.RawURLEncoding.DecodeString(cursor)
	tampered := []string{
		flipLast(cursor),
		cursor + "A",
		base64.RawURLEncoding.EncodeToString(append(raw[:len(raw)-1:len(raw)-1], raw[len(raw)-1]^1)),
		"not-a-cursor",
	}
	for _, connection := range []string{"read", "account"} {
		for i, value := range tampered {
			if connection == "account" && i > 0 {
				break
			}
			if connection == "account" {
				value = cursor // issued for the connection without targets
			}
			before := env.reads.Load()
			requestsBefore, grantsBefore := env.fake.seen()
			_, err := env.invoke(connection, `{"cursor":"`+value+`"}`)
			var refused *provider.InvalidRequestError
			if !errors.As(err, &refused) || !strings.Contains(err.Error(), "cursor") {
				t.Errorf("%s cursor %d: %v, want an invalid request", connection, i, err)
			}
			requests, grants := env.fake.seen()
			if env.reads.Load() != before || len(requests) != len(requestsBefore) || grants != grantsBefore {
				t.Errorf("%s cursor %d was read or sent", connection, i)
			}
		}
	}
}

func TestFailuresAreClassifiedWithoutProviderTextOrSecrets(t *testing.T) {
	cases := []struct {
		name     string
		api      func(int, *http.Request) (*http.Response, error)
		class    provider.Class
		requests int
		grants   int
	}{
		{"401 after renewal", always(http.StatusUnauthorized, errorObject(401, "UNAUTHENTICATED",
			"CREDENTIALS_MISSING")), provider.ClassAuth, 2, 2},
		{"403", always(http.StatusForbidden, errorObject(403, "PERMISSION_DENIED", "ACCESS_TOKEN_SCOPE_INSUFFICIENT")),
			provider.ClassPermission, 1, 1},
		{"403 api disabled", always(http.StatusForbidden, errorObject(403, "PERMISSION_DENIED", "SERVICE_DISABLED")),
			provider.ClassPermission, 1, 1},
		{"404", always(http.StatusNotFound, errorObject(404, "NOT_FOUND", "")), provider.ClassNotFound, 1, 1},
		{"429 without Retry-After", always(http.StatusTooManyRequests, errorObject(429, "RESOURCE_EXHAUSTED",
			"RATE_LIMIT_EXCEEDED")), provider.ClassRateLimited, 1, 1},
		{"500", always(http.StatusInternalServerError, errorObject(500, "INTERNAL", "")), provider.ClassProviderError, 1, 1},
		{"not json", always(http.StatusOK, `{"accountSummaries":[`), provider.ClassInvalidResponse, 1, 1},
		{"html", func(_ int, r *http.Request) (*http.Response, error) {
			return respond(r, http.StatusOK, "text/html", "<html>"+detailCanary+"</html>"), nil
		}, provider.ClassInvalidResponse, 1, 1},
		{"unreadable body", func(_ int, r *http.Request) (*http.Response, error) {
			resp := respond(r, http.StatusOK, "application/json", "")
			resp.Body = failingBody{}
			return resp, nil
		}, provider.ClassInvalidResponse, 1, 1},
		{"timeout", func(int, *http.Request) (*http.Response, error) {
			return nil, timeoutError{}
		}, provider.ClassTimeout, 1, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := newEnv(t, &fakeGoogle{api: c.api})
			_, err := env.invoke("read", `{}`)
			if classOf(err) != c.class {
				t.Fatalf("error = %v (%s), want %s", err, classOf(err), c.class)
			}
			if leak := leaks(err.Error()); leak != "" {
				t.Errorf("message %q carries %s", err.Error(), leak)
			}
			requests, grants := env.fake.seen()
			if len(requests) != c.requests || grants != c.grants {
				t.Errorf("requests = %d, grants = %d, want %d and %d", len(requests), grants, c.requests, c.grants)
			}
			for _, r := range requests {
				if leak := leaks(r.url); leak != "" {
					t.Errorf("URL carries %s", leak)
				}
			}
		})
	}
}

func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

type timeoutError struct{}

func (timeoutError) Error() string { return "i/o timeout" }

// flipLast changes the last character of a cursor.
func flipLast(cursor string) string {
	last := byte('A')
	if cursor[len(cursor)-1] == 'A' {
		last = 'B'
	}
	return cursor[:len(cursor)-1] + string(last)
}

type failingBody struct{}

func (failingBody) Read([]byte) (int, error) { return 0, errors.New(detailCanary) }
func (failingBody) Close() error             { return nil }

func TestRenewedTokenServesARepeatedRead(t *testing.T) {
	env := newEnv(t, &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
		if n == 1 {
			return respond(r, http.StatusUnauthorized, "application/json", errorObject(401, "UNAUTHENTICATED", "")), nil
		}
		return respond(r, http.StatusOK, "application/json", twoAccounts), nil
	}})
	if _, err := env.invoke("read", `{}`); err != nil {
		t.Fatal(err)
	}
	requests, grants := env.fake.seen()
	if len(requests) != 2 || grants != 2 || requests[1].auth != "Bearer "+accessPrefix+"2" {
		t.Errorf("requests = %+v, grants = %d", requests, grants)
	}
}

func TestRateLimitHoldsTheNextRequestAndNeverRepeats(t *testing.T) {
	env := newEnv(t, &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
		if n == 1 {
			return respond(r, http.StatusTooManyRequests, "application/json", errorObject(429, "RESOURCE_EXHAUSTED", "")), nil
		}
		return respond(r, http.StatusOK, "application/json", `{}`), nil
	}})
	if _, err := env.invoke("read", `{}`); classOf(err) != provider.ClassRateLimited {
		t.Fatalf("first call = %v", err)
	}
	if requests, _ := env.fake.seen(); len(requests) != 1 {
		t.Fatalf("requests = %d, want the 429 never repeated", len(requests))
	}
	if _, err := env.invoke("read", `{}`); err != nil {
		t.Fatal(err)
	}
	if len(*env.waits) != 1 || (*env.waits)[0] < 900*time.Millisecond || (*env.waits)[0] > time.Second {
		t.Errorf("waits = %v, want one hold of about a second before the next request", *env.waits)
	}
}

func TestMissingScopeEndsBeforeTheAPI(t *testing.T) {
	fake := &fakeGoogle{tokenAnswer: func(n int) (int, string) {
		return http.StatusOK, `{"access_token":"` + accessPrefix + `1","token_type":"Bearer","expires_in":3600,` +
			`"scope":"https://www.googleapis.com/auth/analytics.edit"}`
	}}
	env := newEnv(t, fake)
	if _, err := env.invoke("read", `{}`); classOf(err) != provider.ClassScopeMissing {
		t.Fatalf("error = %v, want scope-missing", err)
	}
	if requests, _ := fake.seen(); len(requests) != 0 {
		t.Errorf("requests = %d, want none", len(requests))
	}
}

func TestServiceAccountAndImpersonationObtainTheirTokens(t *testing.T) {
	env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, `{}`)})
	if _, err := env.invoke("robot", `{}`); err != nil {
		t.Fatal(err)
	}
	form := env.fake.tokenForms[0]
	if form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:jwt-bearer" {
		t.Fatalf("token request = %v", form)
	}
	parts := strings.Split(form.Get("assertion"), ".")
	claims, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var assertion struct{ Iss, Scope, Aud string }
	if len(parts) != 3 || json.Unmarshal(claims, &assertion) != nil || assertion.Iss != serviceAccount ||
		assertion.Scope != scopeReadOnly || assertion.Aud != "https://oauth2.googleapis.com/token" {
		t.Errorf("assertion claims = %s", claims)
	}
	if masked := env.red.Apply(form.Get("assertion")); strings.Contains(masked, parts[2]) {
		t.Error("the assertion is not registered with the redactor")
	}

	if _, err := env.invoke("impersonate", `{}`); err != nil {
		t.Fatal(err)
	}
	requests, grants := env.fake.seen()
	if grants != 2 || env.fake.impersonate != 1 || requests[1].auth != "Bearer "+impersonated {
		t.Errorf("grants = %d, impersonations = %d, requests = %+v", grants, env.fake.impersonate, requests)
	}
	if scope := env.fake.tokenForms[1].Get("scope"); scope != "" {
		t.Errorf("the consent of the impersonation was asked for %q", scope)
	}
}

func TestResultsAndErrorsHoldNoToken(t *testing.T) {
	env := newEnv(t, &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
		if n == 1 {
			return respond(r, http.StatusOK, "application/json", twoAccounts), nil
		}
		return respond(r, http.StatusForbidden, "application/json", errorObject(403, "PERMISSION_DENIED", "")), nil
	}})
	result, err := env.invoke("read", `{}`)
	if err != nil || leaks(result) != "" {
		t.Fatalf("result leaks %q: %v", leaks(result), err)
	}
	if _, err := env.invoke("read", `{}`); classOf(err) != provider.ClassPermission || leaks(err.Error()) != "" {
		t.Fatalf("second call = %v", err)
	}
}

func TestConnectionTest(t *testing.T) {
	t.Run("success lists one summary", func(t *testing.T) {
		env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, twoAccounts)})
		if class := env.test(t, "read"); class != provider.ClassOK {
			t.Fatalf("class = %s", class)
		}
		requests, _ := env.fake.seen()
		if len(requests) != 1 || requests[0].path != "/v1beta/accountSummaries" || requests[0].rawQuery != "pageSize=1" {
			t.Errorf("requests = %+v", requests)
		}
	})
	t.Run("a bound property is read as well", func(t *testing.T) {
		env := newEnv(t, &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
			if n == 1 {
				return respond(r, http.StatusOK, "application/json", twoAccounts), nil
			}
			return respond(r, http.StatusOK, "application/json",
				`{"name":"properties/200","parent":"accounts/100","account":"accounts/100"}`), nil
		}})
		if class := env.test(t, "property"); class != provider.ClassOK {
			t.Fatalf("class = %s", class)
		}
		requests, _ := env.fake.seen()
		if len(requests) != 2 || requests[1].path != "/v1beta/properties/200" || requests[1].rawQuery != "" {
			t.Errorf("requests = %+v", requests)
		}
	})
	t.Run("a bound property the credential cannot see", func(t *testing.T) {
		env := newEnv(t, &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
			if n == 1 {
				return respond(r, http.StatusOK, "application/json", twoAccounts), nil
			}
			return respond(r, http.StatusForbidden, "application/json", errorObject(403, "PERMISSION_DENIED", "")), nil
		}})
		if class := env.test(t, "property"); class != provider.ClassPermission {
			t.Fatalf("class = %s", class)
		}
	})
	t.Run("expired consent", func(t *testing.T) {
		env := newEnv(t, &fakeGoogle{tokenAnswer: func(int) (int, string) {
			return http.StatusBadRequest, `{"error":"invalid_grant","error_description":"` + detailCanary + `"}`
		}})
		if class := env.test(t, "read"); class != provider.ClassConsentExpired {
			t.Fatalf("class = %s", class)
		}
		if requests, _ := env.fake.seen(); len(requests) != 0 {
			t.Errorf("requests = %d, want none", len(requests))
		}
	})
	t.Run("access token refused after renewal", func(t *testing.T) {
		env := newEnv(t, &fakeGoogle{api: always(http.StatusUnauthorized, errorObject(401, "UNAUTHENTICATED", ""))})
		if class := env.test(t, "read"); class != provider.ClassAuth {
			t.Fatalf("class = %s", class)
		}
	})
	t.Run("api not enabled", func(t *testing.T) {
		env := newEnv(t, &fakeGoogle{api: always(http.StatusForbidden, errorObject(403, "PERMISSION_DENIED", "SERVICE_DISABLED"))})
		if class := env.test(t, "read"); class != provider.ClassPermission {
			t.Fatalf("class = %s", class)
		}
	})
	t.Run("no visible account", func(t *testing.T) {
		env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, `{}`)})
		if class := env.test(t, "property"); class != provider.ClassNotFound {
			t.Fatalf("class = %s", class)
		}
		if requests, _ := env.fake.seen(); len(requests) != 1 {
			t.Errorf("requests = %d, want the summaries only", len(requests))
		}
	})
}

func TestServiceDisabledNamesTheAPI(t *testing.T) {
	err := googleError("op", http.StatusForbidden, []byte(errorObject(403, "PERMISSION_DENIED", "SERVICE_DISABLED")), "")
	if err.Class != provider.ClassPermission || !strings.Contains(err.Message, "Admin API is not enabled") ||
		err.Original == nil || err.Original.Status != http.StatusForbidden {
		t.Errorf("error = %+v", err)
	}
	if other := googleError("op", http.StatusForbidden, []byte(errorObject(403, "PERMISSION_DENIED", "")), ""); strings.Contains(other.Message, "enabled") {
		t.Errorf("a plain 403 claims a disabled API: %s", other.Message)
	}
}

func TestHandlerChecksItsArgumentsItself(t *testing.T) {
	env := newEnv(t, &fakeGoogle{})
	for _, arguments := range []string{`{"page_size":0}`, `{"page_size":201}`} {
		_, err := handleAccountSummaries(context.Background(), env.resolved(t, "read"), env.secrets(), env.red,
			json.RawMessage(arguments))
		var refused *provider.InvalidRequestError
		if !errors.As(err, &refused) {
			t.Errorf("%s: %v", arguments, err)
		}
	}
	if requests, _ := env.fake.seen(); len(requests) != 0 || env.reads.Load() != 0 {
		t.Error("a refused argument was sent or read a secret")
	}
	_, err := handleAccountSummaries(context.Background(), &config.Resolved{BaseURL: "https://evil.example"},
		env.secrets(), env.red, json.RawMessage(`{}`))
	if err == nil || env.reads.Load() != 0 {
		t.Errorf("another origin = %v", err)
	}
}
