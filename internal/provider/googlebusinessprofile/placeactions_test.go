package googlebusinessprofile

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const (
	linkOne    = boundLocation + "/placeActionLinks/4000001"
	linkOther  = otherLocation + "/placeActionLinks/4000002"
	bookingURI = "https://booking.example.com/shop?ref=profile"
)

const linkAnswer = `{"name":"` + linkOne + `","uri":"` + bookingURI + `","placeActionType":"APPOINTMENT",` +
	`"providerType":"MERCHANT","isEditable":true,"isDisabled":false,"isPreferred":true,` +
	`"createTime":"2026-11-01T10:00:00Z","updateTime":"2026-11-02T10:00:00Z"}`

// placeRouter answers by method and path and keeps every method sent.
type placeRouter struct {
	mu      sync.Mutex
	methods []string
	// pages answers the type list by pageToken; empty keeps the single answer.
	pages  map[string]string
	status int
}

func (p *placeRouter) answer(r *http.Request, _ int) (*http.Response, error) {
	p.mu.Lock()
	p.methods = append(p.methods, r.Method)
	p.mu.Unlock()
	path := r.URL.Path
	json := func(body string) (*http.Response, error) {
		return respond(r, http.StatusOK, "application/json", body), nil
	}
	switch {
	case r.Method != http.MethodGet && p.status != 0:
		return respond(r, p.status, "application/json", errorObject(p.status, "UNAVAILABLE", "BACKEND")), nil
	case path == "/v1/"+boundAccount+"/locations":
		return json(`{"locations":[{"name":"` + boundLocation + `"}]}`)
	case r.Method == http.MethodPost && path == "/v1/"+boundLocation+"/placeActionLinks":
		return json(linkAnswer)
	case r.Method == http.MethodPatch && path == "/v1/"+linkOne:
		return json(linkAnswer)
	case r.Method == http.MethodDelete && path == "/v1/"+linkOne:
		return json(`{}`)
	case r.Method == http.MethodGet && path == "/v1/"+linkOne:
		return json(linkAnswer)
	case r.Method == http.MethodGet && path == "/v1/"+boundLocation+"/placeActionLinks":
		if r.URL.Query().Get("filter") == "place_action_type=FOOD_ORDERING" {
			return json(`{}`)
		}
		return json(`{"placeActionLinks":[` + linkAnswer + `],"nextPageToken":"page-two"}`)
	case path == "/v1/placeActionTypeMetadata":
		if p.pages != nil {
			return json(p.pages[r.URL.Query().Get("pageToken")])
		}
		return json(`{"placeActionTypeMetadata":[{"displayName":"Termin","placeActionType":"APPOINTMENT"}]}`)
	}
	return respond(r, http.StatusNotFound, "application/json", errorObject(404, "NOT_FOUND", "NOT_FOUND")), nil
}

func (p *placeRouter) sent(method string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for _, m := range p.methods {
		if m == method {
			n++
		}
	}
	return n
}

func placeEnvironment(t *testing.T) (*environment, *placeRouter) {
	t.Helper()
	env := newEnvironment(t, nil)
	router := &placeRouter{}
	env.fake.answer = router.answer
	effects := []config.Effect{config.EffectRead, config.EffectCreate, config.EffectUpdate, config.EffectDelete}
	for name, target := range map[string]string{"p-all": "", "p-account": boundAccount, "p-location": boundLocation} {
		env.cfg.Connections[name] = config.Connection{Service: "profile", Credential: "google", Effects: effects,
			Target: target}
	}
	return env, router
}

func (e *environment) needsConfirmation(t *testing.T, operation, connection, arguments string) *application.ConfirmationRequiredError {
	t.Helper()
	_, err := e.core.Invoke(context.Background(), application.InvokeRequest{Operation: operation,
		Connection: connection, Arguments: json.RawMessage(arguments)})
	var needs *application.ConfirmationRequiredError
	if !errors.As(err, &needs) {
		t.Fatalf("%s: error = %v, want confirmation required", operation, err)
	}
	return needs
}

func createArgs(uri string) string {
	raw, _ := json.Marshal(map[string]any{"location": boundLocation, "uri": uri, "place_action_type": "APPOINTMENT"})
	return string(raw)
}

func TestLinkReadsAnswerEveryDocumentedMember(t *testing.T) {
	env, _ := placeEnvironment(t)
	out, err := env.invoke(t, placeActionLinksGetID, "p-all", `{"link":"`+linkOne+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"name": linkOne, "uri": bookingURI, "placeActionType": "APPOINTMENT",
		"providerType": "MERCHANT", "isEditable": true, "isDisabled": false, "isPreferred": true,
		"createTime": "2026-11-01T10:00:00Z", "updateTime": "2026-11-02T10:00:00Z"}
	for k, v := range want {
		if out[k] != v {
			t.Errorf("%s = %v, want %v", k, out[k], v)
		}
	}
	calls, _ := env.fake.calls()
	if len(calls) != 1 || calls[0].host != "mybusinessplaceactions.googleapis.com" || calls[0].path != "/v1/"+linkOne {
		t.Errorf("requests = %+v", calls)
	}
}

func TestLinkListPassesFilterPageSizeAndReadsOn(t *testing.T) {
	env, _ := placeEnvironment(t)
	out, err := env.invoke(t, placeActionLinksListID, "p-all",
		`{"location":"`+boundLocation+`","page_size":5,"filter":"place_action_type=APPOINTMENT"}`)
	if err != nil {
		t.Fatal(err)
	}
	cursor, _ := out["next_cursor"].(string)
	if cursor == "" || strings.Contains(cursor, "page-two") {
		t.Fatalf("result = %v", out)
	}
	if _, err := env.invoke(t, placeActionLinksListID, "p-all",
		`{"location":"`+boundLocation+`","cursor":"`+cursor+`"}`); !isInvalidRequest(err) {
		t.Errorf("cursor with other arguments: error = %v, want refused", err)
	}
	if _, err := env.invoke(t, placeActionLinksListID, "p-all",
		`{"location":"`+boundLocation+`","page_size":5,"filter":"place_action_type=APPOINTMENT","cursor":"`+cursor+`"}`); err != nil {
		t.Fatal(err)
	}
	calls, _ := env.fake.calls()
	if len(calls) != 2 || calls[0].values().Get("pageSize") != "5" ||
		calls[0].values().Get("filter") != "place_action_type=APPOINTMENT" ||
		calls[1].values().Get("pageToken") != "page-two" {
		t.Errorf("requests = %+v", calls)
	}
	for _, filter := range []string{"place_action_type=UNKNOWN", "place_action_type=APPOINTMENT OR x", "name=a"} {
		if _, err := env.invoke(t, placeActionLinksListID, "p-all",
			`{"location":"`+boundLocation+`","filter":"`+filter+`"}`); !isInvalidRequest(err) {
			t.Errorf("filter %q: error = %v, want refused", filter, err)
		}
	}
}

func TestLinkListAnswersAnEmptyList(t *testing.T) {
	env, _ := placeEnvironment(t)
	out, err := env.invoke(t, placeActionLinksListID, "p-all",
		`{"location":"`+boundLocation+`","filter":"place_action_type=FOOD_ORDERING"}`)
	// An empty list is left out of the result: missing means empty.
	if err != nil || len(out) != 0 {
		t.Errorf("result = %v, %v", out, err)
	}
}

func TestLinkOutputIsCut(t *testing.T) {
	env, _ := placeEnvironment(t)
	long := strings.Repeat("a", 3000)
	env.fake.answer = reply(http.StatusOK, `{"name":"`+linkOne+`","uri":"https://example.com/`+long+`","extra":"x"}`)
	out, err := env.invoke(t, placeActionLinksGetID, "p-all", `{"link":"`+linkOne+`"}`)
	uri, _ := out["uri"].(string)
	if err != nil || len([]rune(uri)) != maxURLRunes+1 || !strings.HasSuffix(uri, "…") || out["extra"] != nil {
		t.Errorf("uri length = %d, result keys = %v, %v", len([]rune(uri)), out, err)
	}
}

func TestTypesListReadsSeveralPagesWithFilterAndLanguage(t *testing.T) {
	env, router := placeEnvironment(t)
	router.pages = map[string]string{
		"": `{"placeActionTypeMetadata":[{"displayName":"Termin","placeActionType":"APPOINTMENT"}],"nextPageToken":"t2"}`,
		"t2": `{"placeActionTypeMetadata":[{"displayName":"Tisch","placeActionType":"DINING_RESERVATION"}],` +
			`"nextPageToken":"t3"}`,
		"t3": `{"placeActionTypeMetadata":[{"displayName":"Shop","placeActionType":"SHOP_ONLINE"}]}`,
	}
	args := `{"language_code":"de","filter":"region_code=DE","page_size":1`
	var names []string
	cursor := ""
	for page := 0; page < 5; page++ {
		call := args + `}`
		if cursor != "" {
			call = args + `,"cursor":"` + cursor + `"}`
		}
		out, err := env.invoke(t, placeActionTypesListID, "p-all", call)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range out["placeActionTypeMetadata"].([]any) {
			names = append(names, entry.(map[string]any)["displayName"].(string))
		}
		if cursor, _ = out["next_cursor"].(string); cursor == "" {
			break
		}
	}
	if strings.Join(names, ",") != "Termin,Tisch,Shop" {
		t.Errorf("names = %v", names)
	}
	calls, _ := env.fake.calls()
	if len(calls) != 3 || calls[0].path != "/v1/placeActionTypeMetadata" || calls[0].values().Get("languageCode") != "de" ||
		calls[0].values().Get("filter") != "region_code=DE" || calls[2].values().Get("pageToken") != "t3" {
		t.Errorf("requests = %+v", calls)
	}
}

func TestTypesListChecksFilterAndLanguageBeforeAnyRequest(t *testing.T) {
	env, _ := placeEnvironment(t)
	for _, args := range []string{`{"filter":"region_code=de"}`, `{"filter":"location=locations/01"}`,
		`{"filter":"location=accounts/1"}`, `{"filter":"region_code=DE&x=1"}`, `{"language_code":"de/../x"}`} {
		if _, err := env.invoke(t, placeActionTypesListID, "p-all", args); !isInvalidRequest(err) {
			t.Errorf("%s: error = %v, want refused", args, err)
		}
	}
	env.noRequests(t)
}

func TestCreateConfirmationShowsWholeAddressTypeAndLocation(t *testing.T) {
	env, router := placeEnvironment(t)
	needs := env.needsConfirmation(t, placeActionLinksCreateID, "p-all", createArgs(bookingURI))
	for _, want := range []string{bookingURI, "APPOINTMENT", boundLocation} {
		if !strings.Contains(needs.Summary, want) {
			t.Errorf("summary %q lacks %q", needs.Summary, want)
		}
	}
	env.noRequests(t)

	out, err := env.change(t, placeActionLinksCreateID, "p-all", createArgs(bookingURI))
	if err != nil || out["name"] != linkOne {
		t.Fatalf("result = %v, %v", out, err)
	}
	calls, _ := env.fake.calls()
	if len(calls) != 1 || router.sent(http.MethodPost) != 1 || calls[0].host != "mybusinessplaceactions.googleapis.com" ||
		calls[0].path != "/v1/"+boundLocation+"/placeActionLinks" || calls[0].query != "" {
		t.Fatalf("requests = %+v", calls)
	}
	var body map[string]any
	if json.Unmarshal([]byte(calls[0].body), &body) != nil || len(body) != 2 || body["uri"] != bookingURI ||
		body["placeActionType"] != "APPOINTMENT" {
		t.Errorf("body = %s", calls[0].body)
	}
}

func TestCreateSendsOptionalFields(t *testing.T) {
	env, _ := placeEnvironment(t)
	_, err := env.change(t, placeActionLinksCreateID, "p-all",
		`{"location":"`+boundLocation+`","uri":"`+bookingURI+`","place_action_type":"FOOD_ORDERING","is_preferred":true,"is_disabled":false}`)
	if err != nil {
		t.Fatal(err)
	}
	calls, _ := env.fake.calls()
	var body map[string]any
	if json.Unmarshal([]byte(calls[0].body), &body) != nil || body["isPreferred"] != true || body["isDisabled"] != false ||
		len(body) != 4 {
		t.Errorf("body = %s", calls[0].body)
	}
}

func TestCreateRefusesAddressesThatAreNotPlainHTTPS(t *testing.T) {
	env, router := placeEnvironment(t)
	for name, uri := range map[string]string{
		"http":         "http://booking.example.com/shop",
		"other":        "ftp://booking.example.com/shop",
		"no scheme":    "booking.example.com/shop",
		"upper":        "HTTPS://booking.example.com/shop",
		"user info":    "https://shop.example.com" + "@" + "evil.example.net/x",
		"user and key": "https://sample" + ":" + "sample" + "@" + "booking.example.com/",
		"no host":      "https:///path",
		"space":        "https://booking.example.com/a b",
		"control":      "https://booking.example.com/a\u0001b",
		"newline":      "https://booking.example.com/a\nb",
		"backslash":    "https://booking.example.com\\@evil.example.net/",
		"bidi":         "https://booking.example.com/‮abc",
		"non ascii":    "https://böoking.example.com/",
		"too long":     "https://booking.example.com/" + strings.Repeat("a", 2000),
		"scheme only":  "https://",
	} {
		args := createArgs(uri)
		if _, err := env.change(t, placeActionLinksCreateID, "p-all", args); !isInvalidRequest(err) {
			t.Errorf("%s: error = %v, want refused", name, err)
		}
	}
	env.noRequests(t)
	if router.sent(http.MethodPost) != 0 {
		t.Error("a link was sent")
	}
	// The confirmation of a refused call says so instead of asking for approval of a change that cannot happen.
	needs := env.needsConfirmation(t, placeActionLinksCreateID, "p-all", createArgs("http://booking.example.com/"))
	if !strings.Contains(needs.Summary, "Refused before sending") {
		t.Errorf("summary = %q", needs.Summary)
	}
}

func TestCreateRefusesWhenTheConfirmationWouldNotShowEverything(t *testing.T) {
	env, router := placeEnvironment(t)
	fits := "https://booking.example.com/" + strings.Repeat("a", 250)
	long := "https://booking.example.com/" + strings.Repeat("a", 450)
	if _, err := env.change(t, placeActionLinksCreateID, "p-all", createArgs(fits)); err != nil {
		t.Fatalf("an address that fits was refused: %v", err)
	}
	before := router.sent(http.MethodPost)
	_, err := env.change(t, placeActionLinksCreateID, "p-all", createArgs(long))
	if !isInvalidRequest(err) || !strings.Contains(err.Error(), "500 characters") {
		t.Errorf("error = %v, want refusal that names the limit", err)
	}
	if router.sent(http.MethodPost) != before {
		t.Error("a long address was sent")
	}
	needs := env.needsConfirmation(t, placeActionLinksCreateID, "p-all", createArgs(long))
	if !strings.Contains(needs.Summary, "Refused before sending") || strings.Contains(needs.Summary, "…") {
		t.Errorf("summary = %q, want a refusal and no cut address", needs.Summary)
	}
	// What the core shows is never a cut of an address: the summary of an accepted call is whole.
	for _, uri := range []string{fits} {
		needs := env.needsConfirmation(t, placeActionLinksCreateID, "p-all", createArgs(uri))
		if !strings.Contains(needs.Summary, uri) {
			t.Errorf("summary lacks the whole address: %q", needs.Summary)
		}
	}
}

func TestCreateNeedsALocationAndAType(t *testing.T) {
	env, _ := placeEnvironment(t)
	for _, args := range []string{`{"uri":"` + bookingURI + `","place_action_type":"APPOINTMENT"}`,
		`{"location":"` + boundLocation + `","place_action_type":"APPOINTMENT"}`,
		`{"location":"` + boundLocation + `","uri":"` + bookingURI + `"}`,
		`{"location":"` + boundLocation + `","uri":"` + bookingURI + `","place_action_type":"PLACE_ACTION_TYPE_UNSPECIFIED"}`,
		`{"location":"` + boundLocation + `","uri":"` + bookingURI + `","place_action_type":"OTHER"}`,
		`{"location":"` + boundLocation + `","uri":"` + bookingURI + `","place_action_type":"APPOINTMENT","name":"x"}`} {
		if _, err := env.change(t, placeActionLinksCreateID, "p-all", args); err == nil {
			t.Errorf("%s accepted", args)
		}
	}
	env.noRequests(t)
}

func TestUpdateNeedsAMaskThatNamesExactlyTheGivenFields(t *testing.T) {
	env, _ := placeEnvironment(t)
	for name, args := range map[string]string{
		"no mask":            `{"link":"` + linkOne + `","uri":"` + bookingURI + `"}`,
		"empty mask":         `{"link":"` + linkOne + `","update_mask":[],"uri":"` + bookingURI + `"}`,
		"value not in mask":  `{"link":"` + linkOne + `","update_mask":["uri"],"uri":"` + bookingURI + `","is_preferred":true}`,
		"mask without value": `{"link":"` + linkOne + `","update_mask":["uri","is_disabled"],"uri":"` + bookingURI + `"}`,
		"name in mask":       `{"link":"` + linkOne + `","update_mask":["name"],"uri":"` + bookingURI + `"}`,
		"twice":              `{"link":"` + linkOne + `","update_mask":["uri","uri"],"uri":"` + bookingURI + `"}`,
		"http":               `{"link":"` + linkOne + `","update_mask":["uri"],"uri":"http://booking.example.com/"}`,
	} {
		if _, err := env.change(t, placeActionLinksUpdateID, "p-all", args); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	env.noRequests(t)
}

func TestUpdateShowsChangedFieldsAndSendsOnlyThem(t *testing.T) {
	env, router := placeEnvironment(t)
	args := `{"link":"` + linkOne + `","update_mask":["uri","is_preferred"],"uri":"` + bookingURI + `","is_preferred":false}`
	needs := env.needsConfirmation(t, placeActionLinksUpdateID, "p-all", args)
	for _, want := range []string{linkOne, bookingURI, "preferred to false"} {
		if !strings.Contains(needs.Summary, want) {
			t.Errorf("summary %q lacks %q", needs.Summary, want)
		}
	}
	env.noRequests(t)
	out, err := env.change(t, placeActionLinksUpdateID, "p-all", args)
	if err != nil || out["name"] != linkOne {
		t.Fatalf("result = %v, %v", out, err)
	}
	calls, _ := env.fake.calls()
	if len(calls) != 1 || router.sent(http.MethodPatch) != 1 || calls[0].path != "/v1/"+linkOne ||
		calls[0].values().Get("updateMask") != "is_preferred,uri" || len(calls[0].values()) != 1 {
		t.Fatalf("requests = %+v", calls)
	}
	var body map[string]any
	if json.Unmarshal([]byte(calls[0].body), &body) != nil || len(body) != 2 || body["uri"] != bookingURI ||
		body["isPreferred"] != false {
		t.Errorf("body = %s", calls[0].body)
	}
}

func TestDeleteNeedsConfirmationAndSendsOneDelete(t *testing.T) {
	env, router := placeEnvironment(t)
	args := `{"link":"` + linkOne + `"}`
	needs := env.needsConfirmation(t, placeActionLinksDeleteID, "p-all", args)
	if !strings.Contains(needs.Summary, linkOne) {
		t.Errorf("summary = %q", needs.Summary)
	}
	env.noRequests(t)
	if router.sent(http.MethodDelete) != 0 {
		t.Fatal("deleted without confirmation")
	}
	out, err := env.change(t, placeActionLinksDeleteID, "p-all", args)
	if err != nil || out["deleted"] != true || out["link"] != linkOne {
		t.Fatalf("result = %v, %v", out, err)
	}
	calls, _ := env.fake.calls()
	if len(calls) != 1 || router.sent(http.MethodDelete) != 1 || calls[0].path != "/v1/"+linkOne || calls[0].body != "" {
		t.Errorf("requests = %+v", calls)
	}
}

func TestChangesNeedAConnectionThatMayChange(t *testing.T) {
	env, _ := placeEnvironment(t)
	for op, args := range map[string]string{placeActionLinksCreateID: createArgs(bookingURI),
		placeActionLinksDeleteID: `{"link":"` + linkOne + `"}`} {
		// "all" is a read-only connection.
		if _, err := env.change(t, op, "all", args); err == nil {
			t.Errorf("%s ran on a read connection", op)
		}
	}
	env.noRequests(t)
}

func TestLinkPathsAreCheckedAgainstFormAndTargetBeforeIO(t *testing.T) {
	env, _ := placeEnvironment(t)
	for _, link := range []string{"locations/2000002", "locations/2000002/placeActionLinks/", "locations/02/placeActionLinks/1",
		"accounts/1000001/placeActionLinks/1", "locations/2000002/placeActionLinks/1/x", "locations/2000002/placeActionLinks/..",
		"locations/2000002/placeActionLinks/a%2Fb", "https://mybusinessplaceactions.googleapis.com/v1/" + linkOne} {
		for _, op := range []string{placeActionLinksGetID, placeActionLinksUpdateID, placeActionLinksDeleteID} {
			args := `{"link":"` + link + `","update_mask":["is_disabled"],"is_disabled":true}`
			if _, err := env.change(t, op, "p-all", args); err == nil {
				t.Errorf("%s accepted %q", op, link)
			}
		}
	}
	// A location target refuses a link of another location, and the refusal does not name it.
	for _, op := range []string{placeActionLinksGetID, placeActionLinksDeleteID} {
		_, err := env.change(t, op, "p-location", `{"link":"`+linkOther+`"}`)
		if !isInvalidRequest(err) || strings.Contains(err.Error(), otherLocation) {
			t.Errorf("%s: error = %v", op, err)
		}
	}
	for op, args := range map[string]string{
		placeActionLinksListID:   `{"location":"` + otherLocation + `"}`,
		placeActionLinksCreateID: `{"location":"` + otherLocation + `","uri":"` + bookingURI + `","place_action_type":"APPOINTMENT"}`,
	} {
		if _, err := env.change(t, op, "p-location", args); !isInvalidRequest(err) {
			t.Errorf("%s: error = %v, want refused", op, err)
		}
	}
	if _, err := env.change(t, placeActionTypesListID, "p-location", `{"filter":"location=`+otherLocation+`"}`); !isInvalidRequest(err) {
		t.Errorf("types of another location: error = %v", err)
	}
	env.noRequests(t)
}

func TestLocationTargetUsesItsLocationWithoutAProof(t *testing.T) {
	env, _ := placeEnvironment(t)
	if _, err := env.invoke(t, placeActionLinksListID, "p-location", `{}`); err != nil {
		t.Fatal(err)
	}
	calls, _ := env.fake.calls()
	if len(calls) != 1 || calls[0].path != "/v1/"+boundLocation+"/placeActionLinks" {
		t.Errorf("requests = %+v", calls)
	}
}

func TestAccountTargetProvesTheLocationBeforeEveryLinkTool(t *testing.T) {
	env, router := placeEnvironment(t)
	if _, err := env.change(t, placeActionLinksCreateID, "p-account", createArgs(bookingURI)); err != nil {
		t.Fatal(err)
	}
	calls, _ := env.fake.calls()
	if len(calls) != 2 || calls[0].path != "/v1/"+boundAccount+"/locations" || calls[1].path != "/v1/"+boundLocation+"/placeActionLinks" {
		t.Fatalf("requests = %+v", calls)
	}
	// A location the account's list does not hold is refused before anything is read or changed.
	before := router.sent(http.MethodPost) + router.sent(http.MethodDelete) + router.sent(http.MethodPatch)
	for op, args := range map[string]string{
		placeActionLinksGetID:    `{"link":"` + linkOther + `"}`,
		placeActionLinksDeleteID: `{"link":"` + linkOther + `"}`,
		placeActionLinksUpdateID: `{"link":"` + linkOther + `","update_mask":["is_disabled"],"is_disabled":true}`,
		placeActionLinksListID:   `{"location":"` + otherLocation + `"}`,
		placeActionLinksCreateID: `{"location":"` + otherLocation + `","uri":"` + bookingURI + `","place_action_type":"APPOINTMENT"}`,
		placeActionTypesListID:   `{"filter":"location=` + otherLocation + `"}`,
	} {
		_, err := env.change(t, op, "p-account", args)
		if !isInvalidRequest(err) || strings.Contains(err.Error(), otherLocation) {
			t.Errorf("%s: error = %v, want refusal without the location", op, err)
		}
	}
	if after := router.sent(http.MethodPost) + router.sent(http.MethodDelete) + router.sent(http.MethodPatch); after != before {
		t.Errorf("%d changes sent for locations outside the account", after-before)
	}
}

func TestTypesWithoutLocationFilterNeedNoProof(t *testing.T) {
	env, _ := placeEnvironment(t)
	if _, err := env.invoke(t, placeActionTypesListID, "p-account", `{"filter":"region_code=DE"}`); err != nil {
		t.Fatal(err)
	}
	calls, _ := env.fake.calls()
	if len(calls) != 1 || calls[0].path != "/v1/placeActionTypeMetadata" {
		t.Errorf("requests = %+v", calls)
	}
}

func TestPlaceActionFailuresKeepGoogleTextOutAndSayWhenUnsure(t *testing.T) {
	for name, c := range map[string]struct {
		status int
		class  provider.Class
		unsure bool
	}{
		"403": {http.StatusForbidden, provider.ClassPermission, false},
		"429": {http.StatusTooManyRequests, provider.ClassRateLimited, false},
		"503": {http.StatusServiceUnavailable, provider.ClassUnreachable, true},
	} {
		t.Run(name, func(t *testing.T) {
			env, router := placeEnvironment(t)
			router.status = c.status
			_, err := env.change(t, placeActionLinksCreateID, "p-all", createArgs(bookingURI))
			var failed *provider.Error
			if !errors.As(err, &failed) || strings.Contains(err.Error(), detailCanary) ||
				strings.Contains(err.Error(), accessPrefix) {
				t.Fatalf("error = %v", err)
			}
			if strings.Contains(failed.Message, "outcome of the change is unknown") != c.unsure {
				t.Errorf("message = %q, unsure = %v", failed.Message, c.unsure)
			}
			if router.sent(http.MethodPost) != 1 {
				t.Errorf("%d changes sent, want exactly one", router.sent(http.MethodPost))
			}
		})
	}
}

func TestPlaceActionReadFailuresMapToClasses(t *testing.T) {
	env, _ := placeEnvironment(t)
	env.fake.answer = reply(http.StatusNotFound, errorObject(404, "NOT_FOUND", "NOT_FOUND"))
	_, err := env.invoke(t, placeActionLinksGetID, "p-all", `{"link":"`+linkOne+`"}`)
	if classOf(err) != provider.ClassNotFound || strings.Contains(err.Error(), detailCanary) {
		t.Errorf("error = %v", err)
	}
	env.fake.answer = reply(http.StatusForbidden, errorObject(403, "PERMISSION_DENIED", "FORBIDDEN"))
	if _, err := env.invoke(t, placeActionTypesListID, "p-all", `{}`); classOf(err) != provider.ClassPermission {
		t.Errorf("error = %v", err)
	}
}

func TestChangesOfOneLocationShareTheLimiter(t *testing.T) {
	env, _ := placeEnvironment(t)
	ctx, cancel := context.WithTimeout(context.Background(), 1e9)
	defer cancel()
	if _, err := env.changeWith(t, ctx, placeActionLinksCreateID, "p-all", createArgs(bookingURI)); err != nil {
		t.Fatal(err)
	}
	_, err := env.changeWith(t, ctx, placeActionLinksDeleteID, "p-all", `{"link":"`+linkOne+`"}`)
	if classOf(err) != provider.ClassRateLimited {
		t.Errorf("second change of the location = %v, want rate-limited", err)
	}
}
