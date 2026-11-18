package googlebusinessprofile

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const lodgingHostName = "mybusinesslodging.googleapis.com"

// lodgingHost lets the shared fake serve the lodging origin: it records the original host and forwards the
// request to the fake under the host of the business information API.
type lodgingHost struct {
	inner http.RoundTripper
	mu    sync.Mutex
	hosts []string
}

func (h *lodgingHost) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Host != lodgingHostName {
		return h.inner.RoundTrip(r)
	}
	h.mu.Lock()
	h.hosts = append(h.hosts, r.URL.Host)
	h.mu.Unlock()
	forward := r.Clone(r.Context())
	forward.URL.Host = "mybusinessbusinessinformation.googleapis.com"
	return h.inner.RoundTrip(forward)
}

// lodgingServer answers the lodging paths of boundLocation by method: reads answer state, a change answers
// the body it received.
type lodgingServer struct {
	mu      sync.Mutex
	state   string
	patched string
	updated string
	status  int
	methods []string
}

func (s *lodgingServer) answer(r *http.Request, _ int) (*http.Response, error) {
	s.mu.Lock()
	s.methods = append(s.methods, r.Method)
	state, updated, status, patched := s.state, s.updated, s.status, s.patched
	s.mu.Unlock()
	if r.Method == http.MethodPatch && status == 0 {
		return respond(r, http.StatusOK, "application/json", patched), nil
	}
	switch r.URL.Path {
	case "/v1/" + boundLocation + "/lodging":
		if status != 0 {
			return respond(r, status, "application/json", errorObject(status, "NOT_FOUND", "NOT_FOUND")), nil
		}
		return respond(r, http.StatusOK, "application/json", state), nil
	case "/v1/" + boundLocation + "/lodging:getGoogleUpdated":
		return respond(r, http.StatusOK, "application/json", updated), nil
	case "/v1/" + boundAccount + "/locations":
		return respond(r, http.StatusOK, "application/json", `{"locations":[{"name":"`+boundLocation+`"}]}`), nil
	}
	return respond(r, http.StatusNotFound, "application/json", errorObject(404, "NOT_FOUND", "NOT_FOUND")), nil
}

func (s *lodgingServer) patches() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, m := range s.methods {
		if m == http.MethodPatch {
			n++
		}
	}
	return n
}

// The lodging Google holds: synthetic values, a member the discovery document does not name, a value of the
// wrong type, and a text longer than the cut.
var lodgingState = `{"name":"` + boundLocation + `/lodging","metadata":{"updateTime":"2026-11-01T08:00:00Z"},` +
	`"pools":{"hotTub":true,"hotTubException":"DEPENDENT_ON_SEASON","poolsCount":2,"lazyRiver":"yes",` +
	`"undocumented":"` + detailCanary + `"},"parking":{"freeParking":false},` +
	`"policies":{"checkinTime":{"hours":15,"minutes":30},"maxChildAge":12},` +
	`"services":{"languagesSpoken":[{"languageCode":"de","spoken":true}]},` +
	`"guestUnits":[{"label":"Sample suite","codes":["S1","S2"],"features":{"maxOccupantsCount":4}}],` +
	`"allUnits":{"bungalowOrVilla":true},"undocumentedGroup":{"x":true}}`

func lodgingEnvironment(t *testing.T) (*environment, *lodgingServer, *lodgingHost) {
	t.Helper()
	env, _ := writeEnvironment(t)
	server := &lodgingServer{state: lodgingState, patched: `{}`, updated: `{"lodging":{"pools":{"hotTub":false}},` +
		`"diffMask":"pools.hotTub,undocumented.path,pools.hotTub,metadata.updateTime"}`}
	env.fake.answer = server.answer
	host := &lodgingHost{inner: env.fake}
	previous := transport
	transport = host
	t.Cleanup(func() { transport = previous })
	previousNow := lodgingNow
	lodgingNow = func() time.Time { return time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { lodgingNow = previousNow })
	return env, server, host
}

func hostsOf(h *lodgingHost) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.hosts...)
}

func TestLodgingGetReadsOnlyTheMaskedFieldsAndCutsTheAnswer(t *testing.T) {
	env, _, host := lodgingEnvironment(t)
	out, err := env.invoke(t, lodgingGetID, "all",
		`{"location":"`+boundLocation+`","read_mask":["pools","parking.freeParking","guestUnits","policies"]}`)
	if err != nil {
		t.Fatal(err)
	}
	calls, _ := env.fake.calls()
	if len(calls) != 1 || calls[0].path != "/v1/"+boundLocation+"/lodging" ||
		calls[0].values().Get("readMask") != "guestUnits,parking.freeParking,policies,pools" ||
		calls[0].authorization != "Bearer "+accessPrefix+"1" || !reflect.DeepEqual(hostsOf(host), []string{lodgingHostName}) {
		t.Fatalf("requests = %+v hosts = %v", calls, hostsOf(host))
	}
	want := map[string]any{
		"name":     boundLocation + "/lodging",
		"metadata": map[string]any{"updateTime": "2026-11-01T08:00:00Z"},
		"pools":    map[string]any{"hotTub": true, "hotTubException": "DEPENDENT_ON_SEASON", "poolsCount": float64(2)},
		"parking":  map[string]any{"freeParking": false},
		"policies": map[string]any{"checkinTime": map[string]any{"hours": float64(15), "minutes": float64(30)},
			"maxChildAge": float64(12)},
		"services": map[string]any{"languagesSpoken": []any{map[string]any{"languageCode": "de", "spoken": true}}},
		"guestUnits": []any{map[string]any{"label": "Sample suite", "codes": []any{"S1", "S2"},
			"features": map[string]any{"maxOccupantsCount": float64(4)}}},
		"allUnits": map[string]any{"bungalowOrVilla": true},
	}
	if !reflect.DeepEqual(out, want) {
		t.Errorf("result = %v\nwant     %v", out, want)
	}
	if raw, _ := json.Marshal(out); strings.Contains(string(raw), detailCanary) || strings.Contains(string(raw), "lazyRiver") {
		t.Errorf("result keeps an undocumented member or a value of the wrong type: %s", raw)
	}
}

func TestLodgingTextsAreCut(t *testing.T) {
	env, server, _ := lodgingEnvironment(t)
	server.state = `{"guestUnits":[{"label":"` + strings.Repeat("a", 300) + `","codes":["` + strings.Repeat("b", 150) + `"]}]}`
	out, err := env.invoke(t, lodgingGetID, "all", `{"location":"`+boundLocation+`","read_mask":["guestUnits"]}`)
	if err != nil {
		t.Fatal(err)
	}
	unit := out["guestUnits"].([]any)[0].(map[string]any)
	if label := unit["label"].(string); len([]rune(label)) != maxLabelRunes+1 || !strings.HasSuffix(label, "…") {
		t.Errorf("label has %d characters", len([]rune(label)))
	}
	if code := unit["codes"].([]any)[0].(string); len([]rune(code)) != maxCodeRunes+1 {
		t.Errorf("code has %d characters", len([]rune(code)))
	}
}

func TestLodgingReadMaskIsRequiredAndCheckedAgainstTheFieldPaths(t *testing.T) {
	env, _, _ := lodgingEnvironment(t)
	loc := `"location":"` + boundLocation + `"`
	for name, args := range map[string]string{
		"missing":          `{` + loc + `}`,
		"empty":            `{` + loc + `,"read_mask":[]}`,
		"wildcard":         `{` + loc + `,"read_mask":["*"]}`,
		"unknown group":    `{` + loc + `,"read_mask":["spa"]}`,
		"unknown field":    `{` + loc + `,"read_mask":["pools.hotPool"]}`,
		"inside a list":    `{` + loc + `,"read_mask":["guestUnits.label"]}`,
		"deprecated field": `{` + loc + `,"read_mask":["sustainability.sustainabilityCertifications"]}`,
		"twice":            `{` + loc + `,"read_mask":["pools","pools"]}`,
		"inside another":   `{` + loc + `,"read_mask":["pools","pools.hotTub"]}`,
		"wrong case":       `{` + loc + `,"read_mask":["Pools"]}`,
		"string":           `{` + loc + `,"read_mask":"pools"}`,
	} {
		for _, op := range []string{lodgingGetID, lodgingGoogleUpdatedID} {
			if _, err := env.invoke(t, op, "all", args); err == nil {
				t.Errorf("%s %s: accepted", op, name)
			}
		}
	}
	env.noRequests(t)
}

func TestLodgingGetGoogleUpdatedAnswersTheDiffMask(t *testing.T) {
	env, _, host := lodgingEnvironment(t)
	out, err := env.invoke(t, lodgingGoogleUpdatedID, "all",
		`{"location":"`+boundLocation+`","read_mask":["pools.hotTub","metadata"]}`)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"lodging": map[string]any{"pools": map[string]any{"hotTub": false}},
		"diffMask": "pools.hotTub,metadata.updateTime"}
	if !reflect.DeepEqual(out, want) {
		t.Errorf("result = %v, want %v", out, want)
	}
	calls, _ := env.fake.calls()
	if len(calls) != 1 || calls[0].path != "/v1/"+boundLocation+"/lodging:getGoogleUpdated" ||
		calls[0].values().Get("readMask") != "metadata,pools.hotTub" || len(hostsOf(host)) != 1 {
		t.Errorf("requests = %+v", calls)
	}
}

func TestLodgingReadsRefuseLocationsOutsideTheTarget(t *testing.T) {
	env, _, _ := lodgingEnvironment(t)
	args := `{"location":"` + otherLocation + `","read_mask":["pools"]}`
	for _, op := range []string{lodgingGetID, lodgingGoogleUpdatedID} {
		if _, err := env.invoke(t, op, "location", args); !isInvalidRequest(err) {
			t.Errorf("%s on a location target: error = %v, want invalid request", op, err)
		}
		// An account target proves the location from its list and sends nothing else.
		if _, err := env.invoke(t, op, "account", args); !isInvalidRequest(err) {
			t.Errorf("%s on an account target: error = %v, want invalid request", op, err)
		}
	}
	calls, _ := env.fake.calls()
	for _, c := range calls {
		if strings.Contains(c.path, "lodging") {
			t.Errorf("a lodging request was sent: %+v", c)
		}
	}
	// Inside the target the location may be left out or the account proves it.
	if _, err := env.invoke(t, lodgingGetID, "location", `{"read_mask":["pools"]}`); err != nil {
		t.Errorf("bound location left out: %v", err)
	}
	if _, err := env.invoke(t, lodgingGetID, "account", `{"location":"`+boundLocation+`","read_mask":["pools"]}`); err != nil {
		t.Errorf("location of the account: %v", err)
	}
	if _, err := env.invoke(t, lodgingGetID, "all", `{"read_mask":["pools"]}`); !isInvalidRequest(err) {
		t.Errorf("location left out of an unbound connection: error = %v", err)
	}
}

func TestLodgingWithoutDataIsNotFound(t *testing.T) {
	env, server, _ := lodgingEnvironment(t)
	server.status = http.StatusNotFound
	_, err := env.invoke(t, lodgingGetID, "all", `{"location":"`+boundLocation+`","read_mask":["pools"]}`)
	if classOf(err) != provider.ClassNotFound || !strings.Contains(err.Error(), "no lodging data") ||
		strings.Contains(err.Error(), detailCanary) {
		t.Errorf("error = %v, want not-found", err)
	}
	if _, err := env.change(t, lodgingUpdateID, "w-all", changeArgs(`["pools.hotTub"]`, `{"pools":{"hotTub":true}}`,
		`{"pools":{"hotTub":false}}`)); classOf(err) != provider.ClassNotFound || server.patches() != 0 {
		t.Errorf("update error = %v, patches = %d", err, server.patches())
	}
}

func changeArgs(mask, previous, groups string) string {
	out := `{"location":"` + boundLocation + `","update_mask":` + mask + `,"previous_values":` + previous
	if groups != "" {
		out += "," + strings.TrimSuffix(strings.TrimPrefix(groups, "{"), "}")
	}
	return out + `}`
}

func TestLodgingUpdateSendsOnlyTheNamedFieldsAfterReadingThem(t *testing.T) {
	env, server, host := lodgingEnvironment(t)
	server.patched = `{"name":"` + boundLocation + `/lodging","pools":{"hotTub":false}}`
	out, err := env.change(t, lodgingUpdateID, "w-all", changeArgs(
		`["pools.hotTub","policies.maxChildAge","services.languagesSpoken"]`,
		`{"pools":{"hotTub":true},"policies":{"maxChildAge":12},"services":{"languagesSpoken":[{"languageCode":"de","spoken":true}]}}`,
		`{"pools":{"hotTub":false},"services":{"languagesSpoken":[{"languageCode":"en","spoken":true},`+
			`{"languageCode":"fr","spokenException":"DEPENDENT_ON_SEASON"}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(out, map[string]any{"name": boundLocation + "/lodging",
		"pools": map[string]any{"hotTub": false}}) {
		t.Errorf("result = %v", out)
	}
	calls, _ := env.fake.calls()
	if len(calls) != 2 || calls[0].path != "/v1/"+boundLocation+"/lodging" ||
		calls[0].values().Get("readMask") != "policies.maxChildAge,pools.hotTub,services.languagesSpoken" ||
		calls[1].path != "/v1/"+boundLocation+"/lodging" ||
		calls[1].values().Get("updateMask") != "policies.maxChildAge,pools.hotTub,services.languagesSpoken" ||
		!reflect.DeepEqual(hostsOf(host), []string{lodgingHostName, lodgingHostName}) {
		t.Fatalf("requests = %+v", calls)
	}
	if server.methods[0] != http.MethodGet || server.methods[1] != http.MethodPatch {
		t.Errorf("methods = %v", server.methods)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(calls[1].body), &body); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"name":     boundLocation + "/lodging",
		"metadata": map[string]any{"updateTime": "2026-11-18T10:00:00Z"},
		"pools":    map[string]any{"hotTub": false},
		"services": map[string]any{"languagesSpoken": []any{map[string]any{"languageCode": "en", "spoken": true},
			map[string]any{"languageCode": "fr", "spokenException": "DEPENDENT_ON_SEASON"}}},
	}
	// The previous values are never sent, and a named field without a value is cleared by leaving it out.
	if !reflect.DeepEqual(body, want) {
		t.Errorf("body = %v\nwant   %v", body, want)
	}
}

func TestLodgingUpdateTakesTheCallersUpdateTime(t *testing.T) {
	env, _, _ := lodgingEnvironment(t)
	args := strings.TrimSuffix(changeArgs(`["parking.freeParking"]`, `{"parking":{"freeParking":false}}`,
		`{"parking":{"freeParking":true}}`), `}`) + `,"metadata":{"updateTime":"2026-11-17T09:00:00+01:00"}}`
	if _, err := env.change(t, lodgingUpdateID, "w-all", args); err != nil {
		t.Fatal(err)
	}
	calls, _ := env.fake.calls()
	if !strings.Contains(calls[1].body, `"updateTime":"2026-11-17T09:00:00+01:00"`) {
		t.Errorf("body = %s", calls[1].body)
	}
}

func TestLodgingUpdateNeedsConfirmationWithOldAndNewValues(t *testing.T) {
	env, server, _ := lodgingEnvironment(t)
	_, err := env.core.Invoke(context.Background(), application.InvokeRequest{Operation: lodgingUpdateID,
		Connection: "w-all", Arguments: json.RawMessage(changeArgs(`["pools.hotTub","pools.hotTubException"]`,
			`{"pools":{"hotTub":true,"hotTubException":"DEPENDENT_ON_SEASON"}}`, `{"pools":{"hotTub":false}}`))})
	var needs *application.ConfirmationRequiredError
	if !errors.As(err, &needs) {
		t.Fatalf("error = %v, want confirmation required", err)
	}
	for _, part := range []string{boundLocation, "pools.hotTub true -> false",
		`pools.hotTubException "DEPENDENT_ON_SEASON" -> unset`} {
		if !strings.Contains(needs.Summary, part) {
			t.Errorf("summary %q lacks %q", needs.Summary, part)
		}
	}
	env.noRequests(t)
	if server.patches() != 0 {
		t.Error("a change was sent")
	}
}

func TestLodgingUpdateRefusesWhatItCannotConfirmInFull(t *testing.T) {
	env, server, _ := lodgingEnvironment(t)
	var labels, previous []string
	for i := 0; i < 5; i++ {
		labels = append(labels, `{"label":"`+strings.Repeat("n", 100)+`","codes":["N`+string(rune('A'+i))+`"]}`)
		previous = append(previous, `{"label":"`+strings.Repeat("o", 100)+`","codes":["O`+string(rune('A'+i))+`"]}`)
	}
	args := changeArgs(`["guestUnits"]`, `{"guestUnits":[`+strings.Join(previous, ",")+`]}`,
		`{"guestUnits":[`+strings.Join(labels, ",")+`]}`)
	_, err := env.core.Invoke(context.Background(), application.InvokeRequest{Operation: lodgingUpdateID,
		Connection: "w-all", Arguments: json.RawMessage(args)})
	var needs *application.ConfirmationRequiredError
	if !errors.As(err, &needs) || !strings.Contains(needs.Summary, "fewer fields per call") ||
		strings.Contains(needs.Summary, "…") {
		t.Fatalf("error = %v, want a summary that names the refusal and is not cut", err)
	}
	if _, err := env.change(t, lodgingUpdateID, "w-all", args); !isInvalidRequest(err) ||
		!strings.Contains(err.Error(), "fewer fields per call") {
		t.Errorf("error = %v, want the refusal", err)
	}
	env.noRequests(t)
	if server.patches() != 0 {
		t.Error("a change was sent")
	}
	// A text that fits within the bound is confirmed in full.
	short := changeArgs(`["guestUnits"]`, `{"guestUnits":[{"label":"Old","codes":["O1"]}]}`,
		`{"guestUnits":[{"label":"New","codes":["N1"]}]}`)
	_, err = env.core.Invoke(context.Background(), application.InvokeRequest{Operation: lodgingUpdateID,
		Connection: "w-all", Arguments: json.RawMessage(short)})
	if !errors.As(err, &needs) || !strings.Contains(needs.Summary, `[{"codes":["O1"],"label":"Old"}] -> [{"codes":["N1"],"label":"New"}]`) {
		t.Errorf("summary = %v", err)
	}
}

func TestLodgingUpdateWithoutConfirmationSendsNothing(t *testing.T) {
	env, _, _ := lodgingEnvironment(t)
	_, err := env.invoke(t, lodgingUpdateID, "w-all", changeArgs(`["pools.hotTub"]`, `{"pools":{"hotTub":true}}`,
		`{"pools":{"hotTub":false}}`))
	var needs *application.ConfirmationRequiredError
	if !errors.As(err, &needs) {
		t.Errorf("error = %v, want confirmation required", err)
	}
	env.noRequests(t)
}

func TestLodgingUpdateNeedsMaskAndPreviousValues(t *testing.T) {
	env, _, _ := lodgingEnvironment(t)
	loc := `"location":"` + boundLocation + `"`
	for name, args := range map[string]string{
		"no mask":              `{` + loc + `,"previous_values":{"pools":{"hotTub":true}},"pools":{"hotTub":false}}`,
		"empty mask":           `{` + loc + `,"update_mask":[],"previous_values":{},"pools":{"hotTub":false}}`,
		"wildcard":             `{` + loc + `,"update_mask":["*"],"previous_values":{},"pools":{"hotTub":false}}`,
		"no previous values":   `{` + loc + `,"update_mask":["pools.hotTub"],"pools":{"hotTub":false}}`,
		"metadata in mask":     `{` + loc + `,"update_mask":["metadata"],"previous_values":{}}`,
		"read-only in mask":    `{` + loc + `,"update_mask":["allUnits"],"previous_values":{}}`,
		"name in mask":         `{` + loc + `,"update_mask":["name"],"previous_values":{}}`,
		"unknown path":         `{` + loc + `,"update_mask":["pools.hotPool"],"previous_values":{}}`,
		"inside another":       `{` + loc + `,"update_mask":["pools","pools.hotTub"],"previous_values":{}}`,
		"value outside mask":   `{` + loc + `,"update_mask":["pools.hotTub"],"previous_values":{"pools":{"hotTub":true}},"pools":{"hotTub":false,"lazyRiver":true}}`,
		"previous outside":     `{` + loc + `,"update_mask":["pools.hotTub"],"previous_values":{"pools":{"hotTub":true,"lazyRiver":true}},"pools":{"hotTub":false}}`,
		"group outside mask":   `{` + loc + `,"update_mask":["pools.hotTub"],"previous_values":{"pools":{"hotTub":true}},"pools":{"hotTub":false},"parking":{"freeParking":true}}`,
		"nothing changes":      `{` + loc + `,"update_mask":["pools.hotTub"],"previous_values":{"pools":{"hotTub":true}},"pools":{"hotTub":true}}`,
		"both unset":           `{` + loc + `,"update_mask":["pools.hotTub"],"previous_values":{}}`,
		"read-only group":      `{` + loc + `,"update_mask":["parking"],"previous_values":{},"allUnits":{"bungalowOrVilla":true}}`,
		"name argument":        `{` + loc + `,"update_mask":["parking"],"previous_values":{},"name":"locations/1/lodging"}`,
		"unknown group":        `{` + loc + `,"update_mask":["parking"],"previous_values":{},"spa":{"x":true}}`,
		"unknown field":        `{` + loc + `,"update_mask":["pools.hotTub"],"previous_values":{},"pools":{"hotPool":true}}`,
		"wrong boolean":        `{` + loc + `,"update_mask":["pools.hotTub"],"previous_values":{},"pools":{"hotTub":"yes"}}`,
		"wrong enum":           `{` + loc + `,"update_mask":["pools.hotTubException"],"previous_values":{},"pools":{"hotTubException":"SOMETIMES"}}`,
		"count too large":      `{` + loc + `,"update_mask":["pools.poolsCount"],"previous_values":{},"pools":{"poolsCount":2147483648}}`,
		"count negative":       `{` + loc + `,"update_mask":["pools.poolsCount"],"previous_values":{},"pools":{"poolsCount":-1}}`,
		"count fraction":       `{` + loc + `,"update_mask":["pools.poolsCount"],"previous_values":{},"pools":{"poolsCount":1.5}}`,
		"hour out of range":    `{` + loc + `,"update_mask":["policies.checkinTime"],"previous_values":{},"policies":{"checkinTime":{"hours":25}}}`,
		"label with control":   `{` + loc + `,"update_mask":["guestUnits"],"previous_values":{},"guestUnits":[{"label":"a\nb","codes":["A"]}]}`,
		"label too long":       `{` + loc + `,"update_mask":["guestUnits"],"previous_values":{},"guestUnits":[{"label":"` + strings.Repeat("a", 201) + `","codes":["A"]}]}`,
		"bad language":         `{` + loc + `,"update_mask":["services.languagesSpoken"],"previous_values":{},"services":{"languagesSpoken":[{"languageCode":"not a tag"}]}}`,
		"bad time":             `{` + loc + `,"update_mask":["parking.freeParking"],"previous_values":{},"parking":{"freeParking":true},"metadata":{"updateTime":"yesterday"}}`,
		"empty list":           `{` + loc + `,"update_mask":["guestUnits"],"previous_values":{},"guestUnits":[]}`,
		"unknown argument":     `{` + loc + `,"update_mask":["parking"],"previous_values":{},"validate_only":true}`,
		"metadata other field": `{` + loc + `,"update_mask":["parking"],"previous_values":{},"metadata":{"other":"x"}}`,
	} {
		if _, err := env.change(t, lodgingUpdateID, "w-all", args); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	env.noRequests(t)
}

func TestLodgingUpdateRefusesPreviousValuesThatDifferFromGoogle(t *testing.T) {
	env, server, _ := lodgingEnvironment(t)
	for name, args := range map[string]string{
		"other value": changeArgs(`["pools.hotTub"]`, `{"pools":{"hotTub":false}}`, `{"pools":{"hotTub":true}}`),
		"was unset":   changeArgs(`["pools.lazyRiver"]`, `{"pools":{"lazyRiver":true}}`, `{"pools":{"lazyRiver":false}}`),
		"is set":      changeArgs(`["parking.freeParking"]`, `{}`, `{"parking":{"freeParking":true}}`),
		"group":       changeArgs(`["pools"]`, `{"pools":{"hotTub":true}}`, `{"pools":{"hotTub":false}}`),
	} {
		_, err := env.change(t, lodgingUpdateID, "w-all", args)
		if !isInvalidRequest(err) || !strings.Contains(err.Error(), "previous_values does not match") {
			t.Errorf("%s: error = %v, want a refusal of the previous values", name, err)
		}
	}
	if server.patches() != 0 {
		t.Error("a change was sent")
	}
	// The same values as Google holds pass, also for a whole group.
	if _, err := env.change(t, lodgingUpdateID, "w-all", changeArgs(`["parking"]`,
		`{"parking":{"freeParking":false}}`, `{"parking":{"freeParking":true}}`)); err != nil {
		t.Errorf("matching previous values: %v", err)
	}
}

func TestLodgingUpdateRefusesLocationsOutsideTheTarget(t *testing.T) {
	env, server, _ := lodgingEnvironment(t)
	mask, previous, groups := `["parking.freeParking"]`, `{"parking":{"freeParking":false}}`, `{"parking":{"freeParking":true}}`
	other := strings.Replace(changeArgs(mask, previous, groups), boundLocation, otherLocation, 1)
	for _, connection := range []string{"w-location", "w-account"} {
		if _, err := env.change(t, lodgingUpdateID, connection, other); !isInvalidRequest(err) {
			t.Errorf("%s: error = %v, want invalid request", connection, err)
		}
	}
	if server.patches() != 0 {
		t.Error("a change was sent")
	}
	calls, _ := env.fake.calls()
	for _, c := range calls {
		if strings.HasSuffix(c.path, "/lodging") {
			t.Errorf("lodging was read for a location outside the target: %+v", c)
		}
	}
	for _, connection := range []string{"w-location", "w-account", "w-all"} {
		// Each connection starts a fresh environment: one location shares its change spacing.
		env, _, _ := lodgingEnvironment(t)
		if _, err := env.change(t, lodgingUpdateID, connection, changeArgs(mask, previous, groups)); err != nil {
			t.Errorf("%s: %v", connection, err)
		}
	}
}

func TestLodgingChangesCountAgainstTheLimitOfTheLocation(t *testing.T) {
	env, _, _ := lodgingEnvironment(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	args := changeArgs(`["parking.freeParking"]`, `{"parking":{"freeParking":false}}`, `{"parking":{"freeParking":true}}`)
	if _, err := env.changeWith(t, ctx, lodgingUpdateID, "w-all", args); err != nil {
		t.Fatal(err)
	}
	if _, err := env.changeWith(t, ctx, lodgingUpdateID, "w-all", args); classOf(err) != provider.ClassRateLimited {
		t.Errorf("second change of the location = %v, want rate-limited", err)
	}
}

func TestLodgingUpdateFailureIsUnsureAndKeepsGoogleTextOut(t *testing.T) {
	env, server, _ := lodgingEnvironment(t)
	env.fake.answer = func(r *http.Request, n int) (*http.Response, error) {
		if r.Method == http.MethodPatch {
			return respond(r, http.StatusServiceUnavailable, "application/json",
				errorObject(503, "UNAVAILABLE", "BACKEND")), nil
		}
		return server.answer(r, n)
	}
	_, err := env.change(t, lodgingUpdateID, "w-all", changeArgs(`["parking.freeParking"]`,
		`{"parking":{"freeParking":false}}`, `{"parking":{"freeParking":true}}`))
	var failed *provider.Error
	if !errors.As(err, &failed) || failed.Class != provider.ClassUnreachable ||
		!strings.Contains(failed.Message, "outcome of the change is unknown") ||
		strings.Contains(err.Error(), detailCanary) || strings.Contains(err.Error(), accessPrefix) {
		t.Errorf("error = %v", err)
	}
}

func TestLodgingTablesCoverTheDiscoveryAndLeaveOutWhatItExcludes(t *testing.T) {
	var input, output map[string]any
	if err := json.Unmarshal(lodgingUpdate.InputSchema, &input); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(lodgingGet.OutputSchema, &output); err != nil {
		t.Fatal(err)
	}
	inputText, outputText := string(lodgingUpdate.InputSchema), string(lodgingGet.OutputSchema)
	for _, group := range []string{"accessibility", "activities", "business", "commonLivingArea", "connectivity",
		"families", "foodAndDrink", "guestUnits", "healthAndSafety", "housekeeping", "parking", "pets", "policies",
		"pools", "property", "services", "sustainability", "transportation", "wellness"} {
		if _, ok := input["properties"].(map[string]any)[group]; !ok {
			t.Errorf("update has no argument %s", group)
		}
		if _, ok := output["properties"].(map[string]any)[group]; !ok {
			t.Errorf("get has no field %s", group)
		}
	}
	// Only Google answers the units summaries, the name, and the Google-assessed building design; the
	// certifications are deprecated and in neither direction.
	for _, only := range []string{`"allUnits"`, `"someUnits"`, `"greenBuildingDesign"`} {
		if strings.Contains(inputText, only) || !strings.Contains(outputText, only) {
			t.Errorf("%s: in input %t, in output %t", only, strings.Contains(inputText, only),
				strings.Contains(outputText, only))
		}
	}
	for _, gone := range []string{"sustainabilityCertifications", "ecoCertifications"} {
		if strings.Contains(inputText, gone) || strings.Contains(outputText, gone) {
			t.Errorf("deprecated %s is offered", gone)
		}
	}
	for _, p := range []string{"allUnits", "someUnits", "name", "metadata", "metadata.updateTime",
		"sustainability.energyEfficiency.greenBuildingDesign"} {
		for _, known := range writePaths {
			if known == p {
				t.Errorf("update_mask offers %s", p)
			}
		}
	}
	for _, p := range []string{"pools.hotTub", "policies.checkinTime.hours", "guestUnits", "services.languagesSpoken",
		"allUnits.totalLivingAreas.sleeping.bedsCount", "metadata.updateTime"} {
		found := false
		for _, known := range readPaths {
			found = found || known == p
		}
		if !found {
			t.Errorf("read_mask lacks %s", p)
		}
	}
	for _, d := range []string{lodgingGetID, lodgingGoogleUpdatedID, lodgingUpdateID} {
		if !reflect.DeepEqual(toolScopes[d], []string{scopeManage}) {
			t.Errorf("%s scopes = %v", d, toolScopes[d])
		}
	}
}

func TestLodgingToolsAreOfferedByProfile(t *testing.T) {
	if !slices.Contains(readTools, lodgingGetID) || !slices.Contains(readTools, lodgingGoogleUpdatedID) ||
		!slices.Contains(writeTools, lodgingUpdateID) || slices.Contains(readTools, lodgingUpdateID) {
		t.Errorf("readTools = %v, writeTools = %v", readTools, writeTools)
	}
}
