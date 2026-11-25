package googlebusinessprofile

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-kit/jsonschema"
)

// heldLocation is what the fake holds for locations/2000002: output-only members included.
const heldLocation = `{"name":"` + boundLocation + `","title":"Old title","websiteUri":"https://old.example.invalid",` +
	`"phoneNumbers":{"primaryPhone":"030 0000000"},` +
	`"categories":{"primaryCategory":{"name":"categories/gcid:bakery","displayName":"Bakery"}},` +
	`"openInfo":{"status":"OPEN","canReopen":true}}`

// locationFake routes by method and path and keeps the methods.
type locationFake struct {
	mu      sync.Mutex
	methods []string
	status  int
}

func (w *locationFake) answer(r *http.Request, _ int) (*http.Response, error) {
	w.mu.Lock()
	w.methods = append(w.methods, r.Method)
	w.mu.Unlock()
	json := func(body string) (*http.Response, error) {
		return respond(r, http.StatusOK, "application/json", body), nil
	}
	switch {
	case r.Method != http.MethodGet && w.status != 0:
		return respond(r, w.status, "application/json", errorObject(w.status, "UNAVAILABLE", "BACKEND")), nil
	case r.Method == http.MethodPost && r.URL.Path == "/v1/"+boundAccount+"/locations":
		return json(`{"name":"locations/2000077","title":"Sample Bakery"}`)
	case r.Method == http.MethodPatch && r.URL.Path == "/v1/"+boundLocation:
		return json(`{"name":"` + boundLocation + `","title":"New title"}`)
	case r.Method == http.MethodDelete && r.URL.Path == "/v1/"+boundLocation:
		return json(`{}`)
	case r.URL.Path == "/v1/"+boundLocation+":getGoogleUpdated":
		return json(`{"location":{"title":"Google title"},"pendingMask":"websiteUri","diffMask":"title,phoneNumbers"}`)
	case r.URL.Path == "/v1/"+boundLocation:
		return json(heldLocation)
	case r.URL.Path == "/v1/"+boundAccount+"/locations":
		return json(`{"locations":[{"name":"` + boundLocation + `"}]}`)
	}
	return respond(r, http.StatusNotFound, "application/json", errorObject(404, "NOT_FOUND", "NOT_FOUND")), nil
}

func (w *locationFake) count(method string) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := 0
	for _, m := range w.methods {
		if m == method {
			n++
		}
	}
	return n
}

// locationEnvironment adds connections that may create and change, and ones that list the delete tool.
func locationEnvironment(t *testing.T) (*environment, *locationFake) {
	t.Helper()
	env := newEnvironment(t, nil)
	w := &locationFake{}
	env.fake.answer = w.answer
	effects := []config.Effect{config.EffectRead, config.EffectCreate, config.EffectUpdate}
	for name, target := range map[string]string{"l-all": "", "l-account": boundAccount, "l-location": boundLocation} {
		env.cfg.Connections[name] = config.Connection{Service: "profile", Credential: "google", Effects: effects,
			Target: target}
	}
	env.cfg.Connections["l-effects"] = config.Connection{Service: "profile", Credential: "google",
		Effects: []config.Effect{config.EffectRead, config.EffectCreate, config.EffectUpdate, config.EffectDelete}}
	for name, target := range map[string]string{"d-all": "", "d-account": boundAccount, "d-location": boundLocation} {
		env.cfg.Connections[name] = config.Connection{Service: "profile", Credential: "google",
			Tools: []string{locationsDeleteID}, Target: target}
	}
	return env, w
}

const locationCreateArgs = `{"account":"` + boundAccount + `","title":"Sample Bakery",` +
	`"categories":{"primaryCategory":{"name":"categories/gcid:bakery"},"additionalCategories":[{"name":"categories/gcid:cafe"}]},` +
	`"languageCode":"de","storeCode":"s-1","phoneNumbers":{"primaryPhone":"030 0000000"},` +
	`"storefrontAddress":{"regionCode":"DE","locality":"Berlin","addressLines":["Sample Street 1"]},` +
	`"websiteUri":"https://www.example.invalid","labels":["a"],"latlng":{"latitude":52.5,"longitude":13.4},` +
	`"regularHours":{"periods":[{"openDay":"MONDAY","openTime":{"hours":8},"closeDay":"MONDAY","closeTime":{"hours":17}}]},` +
	`"profile":{"description":"A sample bakery."}}`

func TestLocationWriteInputSchemasAreValidJSONSchemas(t *testing.T) {
	for _, descriptor := range []capability.Descriptor{locationsCreate, locationsUpdate} {
		if err := jsonschema.CheckSchema(descriptor.InputSchema); err != nil {
			t.Errorf("%s: %v", descriptor.ID, err)
		}
		if strings.Contains(string(descriptor.InputSchema), `"required":null`) {
			t.Errorf("%s: input schema has a null required list", descriptor.ID)
		}
	}
}

func TestLocationWriteToolsAreRegisteredWithTheirRisk(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	want := map[string]struct {
		effect      capability.Effect
		idempotency capability.Idempotency
		listed      bool
	}{
		locationsCreateID: {capability.EffectCreate, capability.IdempotencyNonIdempotent, false},
		locationsUpdateID: {capability.EffectUpdate, capability.IdempotencyIdempotent, false},
		locationsDeleteID: {capability.EffectDelete, capability.IdempotencyIdempotent, true},
	}
	for id, w := range want {
		d, _, found := reg.Lookup(id)
		if !found || d.Risk.Effect != w.effect || d.Risk.Idempotency != w.idempotency ||
			d.Risk.Confirmation != capability.ConfirmationRequired || !d.Risk.OpenWorld ||
			d.RequiresToolAllowList != w.listed {
			t.Errorf("%s: %+v", id, d)
		}
		for _, profile := range metadata.Profiles {
			for _, tool := range profile.Tools {
				if tool == id && w.listed {
					t.Errorf("profile %s holds the listed-only %s", profile.ID, id)
				}
			}
		}
	}
	d, _, _ := reg.Lookup(locationsDeleteID)
	if !strings.Contains(d.Description, "irreversible") || !strings.Contains(d.Description, "listed-only") {
		t.Errorf("delete description = %q", d.Description)
	}
	if d, _, _ := reg.Lookup(locationsGetGoogleUpdatedID); d.Risk.Effect != capability.EffectRead {
		t.Errorf("get-google-updated risk = %+v", d.Risk)
	}
	// No tool offers a field Google fixes: name and metadata are not arguments, languageCode is create only.
	for _, id := range []string{locationsCreateID, locationsUpdateID} {
		d, _, _ := reg.Lookup(id)
		for _, a := range d.Arguments {
			if a.Name == "name" || a.Name == "metadata" || (id == locationsUpdateID && a.Name == "languageCode") {
				t.Errorf("%s takes %s", id, a.Name)
			}
		}
	}
	if _, _, found := reg.Lookup(Provider + ".locations.transfer"); found {
		t.Error("locations.transfer is a tool")
	}
}

func TestLocationDeleteIsReachableOnlyThroughTheToolsList(t *testing.T) {
	env, w := locationEnvironment(t)
	args := `{"location":"` + boundLocation + `"}`
	if _, err := env.change(t, locationsDeleteID, "l-effects", args); err == nil {
		t.Fatal("an effects-only connection offered locations.delete")
	}
	env.noRequests(t)
	// Listed, but without confirmation.
	_, err := env.core.Invoke(context.Background(), application.InvokeRequest{Operation: locationsDeleteID,
		Connection: "d-all", Arguments: json.RawMessage(args)})
	var needs *application.ConfirmationRequiredError
	if !errors.As(err, &needs) || !strings.Contains(needs.Summary, boundLocation) ||
		!strings.Contains(needs.Summary, "cannot be undone") {
		t.Fatalf("error = %v, want confirmation required", err)
	}
	env.noRequests(t)
	out, err := env.change(t, locationsDeleteID, "d-all", args)
	if err != nil || out["location"] != boundLocation || out["deleted"] != true {
		t.Fatalf("result = %v, %v", out, err)
	}
	calls, _ := env.fake.calls()
	if len(calls) != 1 || w.methods[0] != http.MethodDelete || calls[0].path != "/v1/"+boundLocation ||
		calls[0].host != "mybusinessbusinessinformation.googleapis.com" || calls[0].body != "" {
		t.Fatalf("requests = %+v", calls)
	}
}

func TestLocationDeleteChecksTheTarget(t *testing.T) {
	env, w := locationEnvironment(t)
	if _, err := env.change(t, locationsDeleteID, "d-location", `{"location":"`+otherLocation+`"}`); !isInvalidRequest(err) {
		t.Errorf("other location: %v", err)
	}
	env.noRequests(t)
	if _, err := env.change(t, locationsDeleteID, "d-all", `{}`); !isInvalidRequest(err) {
		t.Errorf("no location on an unbound connection: %v", err)
	}
	if _, err := env.change(t, locationsDeleteID, "d-account", `{"location":"`+otherLocation+`"}`); !isInvalidRequest(err) {
		t.Errorf("location outside the account: %v", err)
	}
	if w.count(http.MethodDelete) != 0 {
		t.Error("a delete was sent")
	}
	if _, err := env.change(t, locationsDeleteID, "d-account", `{"location":"`+boundLocation+`"}`); err != nil {
		t.Errorf("location of the account: %v", err)
	}
	if w.count(http.MethodDelete) != 1 {
		t.Errorf("deletes = %d", w.count(http.MethodDelete))
	}
}

func TestLocationCreateSendsTheFieldsAndNamesTheRequestID(t *testing.T) {
	env, w := locationEnvironment(t)
	_, err := env.core.Invoke(context.Background(), application.InvokeRequest{Operation: locationsCreateID,
		Connection: "l-all", Arguments: json.RawMessage(locationCreateArgs)})
	var needs *application.ConfirmationRequiredError
	if !errors.As(err, &needs) || !strings.Contains(needs.Summary, "Sample Bakery") ||
		!strings.Contains(needs.Summary, boundAccount) {
		t.Fatalf("error = %v, want confirmation required", err)
	}
	env.noRequests(t)

	out, err := env.change(t, locationsCreateID, "l-all", locationCreateArgs)
	if err != nil || out["name"] != "locations/2000077" {
		t.Fatalf("result = %v, %v", out, err)
	}
	id, _ := out["request_id"].(string)
	calls, _ := env.fake.calls()
	if len(calls) != 1 || w.methods[0] != http.MethodPost || calls[0].path != "/v1/"+boundAccount+"/locations" ||
		calls[0].values().Get("requestId") != id || !requestIDForm.MatchString(id) || len(id) != 36 ||
		calls[0].values().Has("validateOnly") {
		t.Fatalf("requests = %+v, id = %q", calls, id)
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal([]byte(calls[0].body), &body); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"title", "categories", "languageCode", "storeCode", "phoneNumbers",
		"storefrontAddress", "websiteUri", "labels", "latlng", "regularHours", "profile"} {
		if _, ok := body[field]; !ok {
			t.Errorf("body lacks %s", field)
		}
	}
	if _, ok := body["account"]; ok || len(body) != 11 {
		t.Errorf("body = %s, want only location fields", calls[0].body)
	}
}

func TestLocationCreateKeepsAGivenRequestIDAndValidates(t *testing.T) {
	env, _ := locationEnvironment(t)
	args := strings.Replace(locationCreateArgs, `"title"`, `"request_id":"sample-request-1","validate_only":true,"title"`, 1)
	out, err := env.change(t, locationsCreateID, "l-all", args)
	if err != nil || out["request_id"] != "sample-request-1" {
		t.Fatalf("result = %v, %v", out, err)
	}
	calls, _ := env.fake.calls()
	if len(calls) != 1 || calls[0].values().Get("requestId") != "sample-request-1" ||
		calls[0].values().Get("validateOnly") != "true" {
		t.Fatalf("requests = %+v", calls)
	}
	// A validate-only call still needs the confirmation.
	_, err = env.core.Invoke(context.Background(), application.InvokeRequest{Operation: locationsCreateID,
		Connection: "l-all", Arguments: json.RawMessage(args)})
	var needs *application.ConfirmationRequiredError
	if !errors.As(err, &needs) || !strings.Contains(needs.Summary, "validate only") {
		t.Errorf("error = %v", err)
	}
}

func TestLocationCreateRefusalsSendNothing(t *testing.T) {
	env, _ := locationEnvironment(t)
	base := `"title":"Sample Bakery","categories":{"primaryCategory":{"name":"categories/gcid:bakery"}}`
	for name, c := range map[string]struct{ connection, args string }{
		"no primary category":   {"l-all", `{"account":"` + boundAccount + `","title":"T","categories":{"additionalCategories":[{"name":"categories/gcid:cafe"}]}}`},
		"blank primary":         {"l-all", `{"account":"` + boundAccount + `","title":"T","categories":{"primaryCategory":{"name":"  "}}}`},
		"no categories":         {"l-all", `{"account":"` + boundAccount + `","title":"T"}`},
		"no title":              {"l-all", `{"account":"` + boundAccount + `","categories":{"primaryCategory":{"name":"c"}}}`},
		"bad request id":        {"l-all", `{"account":"` + boundAccount + `","request_id":"a b",` + base + `}`},
		"account missing":       {"l-all", `{` + base + `}`},
		"other account":         {"l-account", `{"account":"` + otherAccount + `",` + base + `}`},
		"location target":       {"l-location", `{"account":"` + boundAccount + `",` + base + `}`},
		"unknown field":         {"l-all", `{"account":"` + boundAccount + `","metadata":{},` + base + `}`},
		"name":                  {"l-all", `{"account":"` + boundAccount + `","name":"locations/1",` + base + `}`},
		"too many phones":       {"l-all", `{"account":"` + boundAccount + `","phoneNumbers":{"primaryPhone":"1","additionalPhones":["2","3","4"]},` + base + `}`},
		"output only category":  {"l-all", `{"account":"` + boundAccount + `","title":"T","categories":{"primaryCategory":{"name":"c","displayName":"x"}}}`},
		"latitude out of range": {"l-all", `{"account":"` + boundAccount + `","latlng":{"latitude":91,"longitude":1},` + base + `}`},
	} {
		if _, err := env.change(t, locationsCreateID, c.connection, c.args); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	env.noRequests(t)
}

func TestLocationCreateOnABoundAccountMayLeaveTheAccountOut(t *testing.T) {
	env, _ := locationEnvironment(t)
	if _, err := env.change(t, locationsCreateID, "l-account",
		`{"title":"T","categories":{"primaryCategory":{"name":"categories/gcid:bakery"}}}`); err != nil {
		t.Errorf("bound account left out: %v", err)
	}
	if ok, _ := targetReaches(locationsCreateID, []string{boundLocation}); ok {
		t.Error("locations.create is offered to a location target")
	}
	if ok, _ := targetReaches(locationsCreateID, []string{boundAccount}); !ok {
		t.Error("locations.create is withheld from an account target")
	}
	for _, id := range []string{locationsUpdateID, locationsDeleteID, locationsGetGoogleUpdatedID} {
		if ok, _ := targetReaches(id, []string{boundLocation}); !ok {
			t.Errorf("%s is withheld from a location target", id)
		}
	}
}

func TestLocationCreateFailureNamesTheRequestIDWhenUnsure(t *testing.T) {
	env, w := locationEnvironment(t)
	w.status = http.StatusServiceUnavailable
	_, err := env.change(t, locationsCreateID, "l-all", strings.Replace(locationCreateArgs, `"title"`, `"request_id":"sample-request-2","title"`, 1))
	var failed *provider.Error
	if !errors.As(err, &failed) || !strings.Contains(failed.Message, "outcome of the change is unknown") ||
		!strings.Contains(failed.Message, "sample-request-2") || strings.Contains(err.Error(), detailCanary) {
		t.Fatalf("error = %v", err)
	}
	if w.count(http.MethodPost) != 1 {
		t.Errorf("%d creates sent, want exactly one", w.count(http.MethodPost))
	}
}

const updateArgs = `{"location":"` + boundLocation + `","update_mask":["websiteUri","phoneNumbers"],` +
	`"previous_values":{"phoneNumbers":{"primaryPhone":"030 0000000"},"websiteUri":"https://old.example.invalid"},` +
	`"phoneNumbers":{"primaryPhone":"030 1111111"},"websiteUri":"https://new.example.invalid"}`

func TestLocationUpdateShowsOldAndNewValuesAndSendsOnlyTheMask(t *testing.T) {
	env, w := locationEnvironment(t)
	_, err := env.core.Invoke(context.Background(), application.InvokeRequest{Operation: locationsUpdateID,
		Connection: "l-all", Arguments: json.RawMessage(updateArgs)})
	var needs *application.ConfirmationRequiredError
	if !errors.As(err, &needs) {
		t.Fatalf("error = %v, want confirmation required", err)
	}
	for _, want := range []string{boundLocation, "phoneNumbers", "websiteUri", "https://old.example.invalid",
		"https://new.example.invalid", "030 0000000", "030 1111111"} {
		if !strings.Contains(needs.Summary, want) {
			t.Errorf("summary %q lacks %q", needs.Summary, want)
		}
	}
	env.noRequests(t)

	out, err := env.change(t, locationsUpdateID, "l-all", updateArgs)
	if err != nil || out["title"] != "New title" {
		t.Fatalf("result = %v, %v", out, err)
	}
	calls, _ := env.fake.calls()
	if len(calls) != 2 || w.methods[0] != http.MethodGet || w.methods[1] != http.MethodPatch ||
		calls[0].values().Get("readMask") != "phoneNumbers,websiteUri" ||
		calls[1].path != "/v1/"+boundLocation || calls[1].values().Get("updateMask") != "phoneNumbers,websiteUri" ||
		calls[1].values().Has("validateOnly") {
		t.Fatalf("requests = %+v %v", calls, w.methods)
	}
	if calls[1].body != `{"phoneNumbers":{"primaryPhone":"030 1111111"},"websiteUri":"https://new.example.invalid"}` {
		t.Errorf("body = %s, want only the masked fields", calls[1].body)
	}
}

func TestLocationUpdateValidateOnlyAndOutputOnlyMembersInPrevious(t *testing.T) {
	env, _ := locationEnvironment(t)
	// Previous values as a read answers them (displayName, canReopen) and as a caller may shorten them.
	for name, previous := range map[string]string{
		"as read":   `{"categories":{"primaryCategory":{"name":"categories/gcid:bakery","displayName":"Bakery"}},"openInfo":{"status":"OPEN","canReopen":true}}`,
		"shortened": `{"categories":{"primaryCategory":{"name":"categories/gcid:bakery"}},"openInfo":{"status":"OPEN"}}`,
	} {
		args := `{"location":"` + boundLocation + `","update_mask":["categories","openInfo"],"validate_only":true,` +
			`"previous_values":` + previous + `,` +
			`"categories":{"primaryCategory":{"name":"categories/gcid:cafe"}},"openInfo":{"status":"CLOSED_TEMPORARILY"}}`
		if _, err := env.change(t, locationsUpdateID, "l-all", args); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	calls, _ := env.fake.calls()
	last := calls[len(calls)-1]
	if last.values().Get("validateOnly") != "true" || last.values().Get("updateMask") != "categories,openInfo" {
		t.Errorf("last request = %+v", last)
	}
}

func TestLocationUpdateRefusesWhatDiffersFromTheHeldStateWithoutWriting(t *testing.T) {
	env, w := locationEnvironment(t)
	stale := strings.Replace(updateArgs, `"https://old.example.invalid"`, `"https://older.example.invalid"`, 1)
	_, err := env.change(t, locationsUpdateID, "l-all", stale)
	if !isInvalidRequest(err) || !strings.Contains(err.Error(), "websiteUri") || strings.Contains(err.Error(), "older.example") {
		t.Fatalf("error = %v, want an invalid request naming the field only", err)
	}
	if w.count(http.MethodPatch) != 0 || w.count(http.MethodGet) != 1 {
		t.Errorf("methods = %v, want one read and no change", w.methods)
	}
}

func TestLocationUpdateRefusalsSendNothing(t *testing.T) {
	env, _ := locationEnvironment(t)
	loc := `"location":"` + boundLocation + `"`
	prev := `"previous_values":{"websiteUri":"https://old.example.invalid"}`
	for name, c := range map[string]struct{ connection, args string }{
		"no mask":            {"l-all", `{` + loc + `,` + prev + `,"websiteUri":"https://n.example.invalid"}`},
		"empty mask":         {"l-all", `{` + loc + `,"update_mask":[],` + prev + `,"websiteUri":"https://n.example.invalid"}`},
		"no previous values": {"l-all", `{` + loc + `,"update_mask":["websiteUri"],"websiteUri":"https://n.example.invalid"}`},
		"previous lacks":     {"l-all", `{` + loc + `,"update_mask":["websiteUri","title"],` + prev + `,"websiteUri":"x","title":"T"}`},
		"previous extra":     {"l-all", `{` + loc + `,"update_mask":["websiteUri"],"previous_values":{"websiteUri":"a","title":"T"},"websiteUri":"x"}`},
		"value lacks":        {"l-all", `{` + loc + `,"update_mask":["websiteUri","title"],"previous_values":{"websiteUri":"a","title":"T"},"websiteUri":"x"}`},
		"value not in mask":  {"l-all", `{` + loc + `,"update_mask":["websiteUri"],` + prev + `,"websiteUri":"x","title":"T"}`},
		"language":           {"l-all", `{` + loc + `,"update_mask":["languageCode"],"previous_values":{},"languageCode":"de"}`},
		"metadata":           {"l-all", `{` + loc + `,"update_mask":["metadata"],"previous_values":{},"metadata":{}}`},
		"twice":              {"l-all", `{` + loc + `,"update_mask":["websiteUri","websiteUri"],` + prev + `,"websiteUri":"x"}`},
		"nothing changes":    {"l-all", `{` + loc + `,"update_mask":["websiteUri"],"previous_values":{"websiteUri":"a"},"websiteUri":"a"}`},
		"other location":     {"l-location", `{"location":"` + otherLocation + `","update_mask":["websiteUri"],` + prev + `,"websiteUri":"x"}`},
	} {
		if _, err := env.change(t, locationsUpdateID, c.connection, c.args); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	env.noRequests(t)
}

func TestLocationUpdateOnABoundAccountProvesTheLocationFirst(t *testing.T) {
	env, w := locationEnvironment(t)
	if _, err := env.change(t, locationsUpdateID, "l-account", updateArgs); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(w.methods, " "); got != "GET GET PATCH" {
		t.Errorf("methods = %s, want the proof, the read, and the change", got)
	}
	outside := strings.Replace(updateArgs, boundLocation, otherLocation, 1)
	if _, err := env.change(t, locationsUpdateID, "l-account", outside); !isInvalidRequest(err) {
		t.Errorf("location outside the account: %v", err)
	}
	if w.count(http.MethodPatch) != 1 {
		t.Errorf("changes = %d, want 1", w.count(http.MethodPatch))
	}
}

func TestLocationGetGoogleUpdatedReturnsTheDiffMask(t *testing.T) {
	env, _ := locationEnvironment(t)
	out, err := env.invoke(t, locationsGetGoogleUpdatedID, "l-all",
		`{"location":"`+boundLocation+`","read_mask":["title","phoneNumbers"]}`)
	if err != nil || out["diffMask"] != "title,phoneNumbers" || out["pendingMask"] != "websiteUri" {
		t.Fatalf("result = %v, %v", out, err)
	}
	if location, _ := out["location"].(map[string]any); location["title"] != "Google title" {
		t.Errorf("location = %v", out["location"])
	}
	calls, _ := env.fake.calls()
	if len(calls) != 1 || calls[0].path != "/v1/"+boundLocation+":getGoogleUpdated" ||
		calls[0].values().Get("readMask") != "phoneNumbers,title" {
		t.Errorf("requests = %+v", calls)
	}
	if _, err := env.invoke(t, locationsGetGoogleUpdatedID, "l-all", `{"location":"`+boundLocation+`"}`); err == nil {
		t.Error("a call without read_mask was accepted")
	}
	if _, err := env.invoke(t, locationsGetGoogleUpdatedID, "location", `{"location":"`+otherLocation+`","read_mask":["title"]}`); !isInvalidRequest(err) {
		t.Errorf("other location: %v", err)
	}
}

func TestLocationChangesShareTheChangeLimiter(t *testing.T) {
	env, _ := locationEnvironment(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := env.changeWith(t, ctx, locationsUpdateID, "l-all", updateArgs); err != nil {
		t.Fatal(err)
	}
	if _, err := env.changeWith(t, ctx, locationsUpdateID, "l-all", updateArgs); classOf(err) != provider.ClassRateLimited {
		t.Errorf("second change of the location = %v, want rate-limited", err)
	}
	if _, err := env.changeWith(t, ctx, locationsDeleteID, "d-all", `{"location":"`+boundLocation+`"}`); classOf(err) != provider.ClassRateLimited {
		t.Errorf("delete right after a change = %v, want rate-limited", err)
	}
	// A create counts against the account.
	if _, err := env.changeWith(t, ctx, locationsCreateID, "l-all", locationCreateArgs); err != nil {
		t.Fatal(err)
	}
	if _, err := env.changeWith(t, ctx, locationsCreateID, "l-all", locationCreateArgs); classOf(err) != provider.ClassRateLimited {
		t.Errorf("second create in the account = %v, want rate-limited", err)
	}
}

func TestLocationConfirmationShowsEverythingOrTheCallIsRefused(t *testing.T) {
	env, w := locationEnvironment(t)
	// Every quote of a 4000 character description is escaped in the confirmation, so the old and the new
	// description alone need about 16000 characters, and a second field takes the call beyond the bound.
	long := strings.Repeat(`\"`, 4000)
	store := strings.Repeat("s", 500)
	args := `{"location":"` + boundLocation + `","update_mask":["profile","storeCode"],` +
		`"previous_values":{"profile":{"description":"` + long + `"},"storeCode":"` + store + `"},` +
		`"profile":{"description":"` + long[2:] + `y"},"storeCode":"` + store[1:] + `y"}`
	// Unconfirmed: no excerpt, a clear notice.
	_, err := env.core.Invoke(context.Background(), application.InvokeRequest{Operation: locationsUpdateID,
		Connection: "l-all", Arguments: json.RawMessage(args)})
	var needs *application.ConfirmationRequiredError
	if !errors.As(err, &needs) || !strings.Contains(needs.Summary, "cannot be confirmed") ||
		strings.Contains(needs.Summary, long[:50]) || strings.Contains(needs.Summary, store) {
		t.Fatalf("error = %v", err)
	}
	// Confirmed: refused before any request.
	if _, err := env.change(t, locationsUpdateID, "l-all", args); !isInvalidRequest(err) ||
		!strings.Contains(err.Error(), limitText(locationsUpdateID)) {
		t.Fatalf("error = %v", err)
	}
	env.noRequests(t)
	if len(w.methods) != 0 {
		t.Errorf("methods = %v", w.methods)
	}
	// A summary of the largest fitting size is shown whole.
	fit := strings.Repeat("z", 150)
	ok := `{"location":"` + boundLocation + `","update_mask":["storeCode"],"previous_values":{"storeCode":"` + fit +
		`"},"storeCode":"` + fit + `y"}`
	_, err = env.core.Invoke(context.Background(), application.InvokeRequest{Operation: locationsUpdateID,
		Connection: "l-all", Arguments: json.RawMessage(ok)})
	if !errors.As(err, &needs) || !strings.Contains(needs.Summary, fit+`y"`) || strings.HasSuffix(needs.Summary, "…") {
		t.Errorf("summary = %q", needs.Summary)
	}
}

func TestDeletionToolsAreListedOnlyAndInNoProfile(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	listed := []string{locationsDeleteID, mediaDeleteID, localPostsDeleteID, reviewsDeleteReplyID,
		accountAdminsDeleteID, locationAdminsDeleteID}
	for _, id := range listed {
		d, _, found := reg.Lookup(id)
		if !found || d.Risk.Effect != capability.EffectDelete || !d.RequiresToolAllowList ||
			!strings.Contains(d.Description, "listed-only") {
			t.Errorf("%s is not a listed-only deletion: %+v", id, d)
		}
		for _, profile := range metadata.Profiles {
			if slices.Contains(profile.Tools, id) {
				t.Errorf("profile %s holds the listed-only %s", profile.ID, id)
			}
		}
	}
	// A place action link is recreated from its values, so its deletion stays in the profile write.
	d, _, _ := reg.Lookup(placeActionLinksDeleteID)
	if d.RequiresToolAllowList {
		t.Errorf("%s is listed-only", placeActionLinksDeleteID)
	}
	for _, profile := range metadata.Profiles {
		if got := slices.Contains(profile.Tools, placeActionLinksDeleteID); got != (profile.ID == "write") {
			t.Errorf("profile %s holds %s: %t", profile.ID, placeActionLinksDeleteID, got)
		}
	}
}
