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
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
)

const (
	mediaOne    = v4Location + "/media/AbCd_123"
	mediaSource = "https://media.example.invalid/front.jpg"
)

// mediaFake routes the v4 media requests by method and path and keeps method, path, query, and body of each.
type mediaFake struct {
	mu      sync.Mutex
	methods []string
	queries []string
	pages   map[string]string
	item    string
	status  int
}

func (f *mediaFake) answer(r *http.Request, _ int) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.methods = append(f.methods, r.Method)
	f.queries = append(f.queries, r.URL.RawQuery)
	path := r.URL.Path
	reply := func(text string) (*http.Response, error) {
		return respond(r, http.StatusOK, "application/json", text), nil
	}
	switch {
	case f.status != 0 && r.Method != http.MethodGet:
		return respond(r, f.status, "application/json", errorObject(f.status, "UNAVAILABLE", "BACKEND")), nil
	case path == "/v1/"+boundAccount+"/locations":
		return reply(`{"locations":[{"name":"locations/2000002"}]}`)
	case r.Method == http.MethodPost && path == "/v4/"+v4Location+"/media":
		return reply(f.item)
	case r.Method == http.MethodGet && path == "/v4/"+v4Location+"/media":
		return reply(f.pages[r.URL.Query().Get("pageToken")])
	case r.Method == http.MethodPatch && path == "/v4/"+mediaOne:
		return reply(f.item)
	case r.Method == http.MethodDelete && path == "/v4/"+mediaOne:
		return reply(`{}`)
	case r.Method == http.MethodGet && path == "/v4/"+mediaOne:
		return reply(f.item)
	}
	return respond(r, http.StatusNotFound, "application/json", errorObject(404, "NOT_FOUND", "NOT_FOUND")), nil
}

// sentBody is the body of the last request that carried one.
func sentBody(env *environment) string {
	calls, _ := env.fake.calls()
	for i := len(calls) - 1; i >= 0; i-- {
		if strings.HasPrefix(calls[i].path, "/v4/") && calls[i].body != "" {
			return calls[i].body
		}
	}
	return ""
}

func mediaEnvironment(t *testing.T) (*environment, *mediaFake) {
	t.Helper()
	env := newEnvironment(t, nil)
	f := &mediaFake{pages: map[string]string{}}
	env.fake.answer = f.answer
	effects := []config.Effect{config.EffectRead, config.EffectCreate, config.EffectUpdate, config.EffectDelete}
	for name, target := range map[string]string{"m-all": "", "m-account": boundAccount, "m-location": boundLocation} {
		env.cfg.Connections[name] = config.Connection{Service: "profile", Credential: "google", Effects: effects,
			Target: target}
	}
	return env, f
}

const mediaFull = `{"name":"` + mediaOne + `","mediaFormat":"PHOTO","locationAssociation":{"category":"EXTERIOR"},` +
	`"googleUrl":"https://lh3.example.invalid/p/1","thumbnailUrl":"https://lh3.example.invalid/t/1",` +
	`"createTime":"2026-11-01T10:00:00Z","dimensions":{"widthPixels":1024,"heightPixels":768},` +
	`"insights":{"viewCount":"1234"},"attribution":{"profileName":"Sample Customer",` +
	`"profilePhotoUrl":"https://lh3.example.invalid/a/1","takedownUrl":"https://maps.example.invalid/report",` +
	`"profileUrl":"https://maps.example.invalid/contrib/1"},"description":"Front door",` +
	`"sourceUrl":"https://secret.example.invalid/?token=abc"}`

func mediaCreateArgs(extra string) string {
	return `{"location":"` + v4Location + `","media_format":"PHOTO","source_url":"` + mediaSource +
		`","location_association":{"category":"EXTERIOR"}` + extra + `}`
}

func TestMediaGetAnswersEveryDocumentedMemberExceptTheSource(t *testing.T) {
	env, f := mediaEnvironment(t)
	f.item = mediaFull
	out, err := env.invoke(t, mediaGetID, "m-account", `{"media_item":"`+mediaOne+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, member := range []string{"name", "mediaFormat", "locationAssociation", "googleUrl", "thumbnailUrl", "createTime",
		"dimensions", "insights", "attribution", "description"} {
		if _, ok := out[member]; !ok {
			t.Errorf("member %s missing in %v", member, out)
		}
	}
	if out["insights"].(map[string]any)["viewCount"] != "1234" ||
		out["attribution"].(map[string]any)["profileName"] != "Sample Customer" ||
		out["dimensions"].(map[string]any)["widthPixels"] != float64(1024) {
		t.Errorf("result = %v", out)
	}
	if raw, _ := json.Marshal(out); strings.Contains(string(raw), "secret.example.invalid") {
		t.Errorf("the source address was answered: %s", raw)
	}
	calls, _ := env.fake.calls()
	if len(calls) != 2 || calls[1].host != "mybusiness.googleapis.com" || calls[1].path != "/v4/"+mediaOne {
		t.Errorf("requests = %+v", calls)
	}
}

func TestMediaListReadsThreePagesWithCursor(t *testing.T) {
	env, f := mediaEnvironment(t)
	f.pages[""] = `{"mediaItems":[` + mediaFull + `],"totalMediaItemCount":3,"nextPageToken":"p2"}`
	f.pages["p2"] = `{"mediaItems":[{"name":"` + v4Location + `/media/B"}],"totalMediaItemCount":3,"nextPageToken":"p3"}`
	f.pages["p3"] = `{"mediaItems":[{"name":"` + v4Location + `/media/C"}],"totalMediaItemCount":3}`
	cursor, total := "", 0
	for page := range 3 {
		args := `{"location":"` + v4Location + `","page_size":100`
		if cursor != "" {
			args += `,"cursor":"` + cursor + `"`
		}
		out, err := env.invoke(t, mediaListID, "m-all", args+`}`)
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		total += len(out["mediaItems"].([]any))
		if out["totalMediaItemCount"] != float64(3) {
			t.Errorf("page %d: total = %v", page, out["totalMediaItemCount"])
		}
		cursor, _ = out["next_cursor"].(string)
	}
	if total != 3 || cursor != "" {
		t.Errorf("total = %d, cursor = %q", total, cursor)
	}
	calls, _ := env.fake.calls()
	if len(calls) != 3 || calls[0].values().Get("pageSize") != "100" || calls[0].path != "/v4/"+v4Location+"/media" {
		t.Errorf("requests = %+v", calls)
	}
	if _, err := env.invoke(t, mediaListID, "m-all", `{"location":"`+v4Location+`","cursor":"forged"}`); !isInvalidRequest(err) {
		t.Errorf("forged cursor: %v", err)
	}
	if _, err := env.invoke(t, mediaListID, "m-all", `{"location":"`+v4Location+`","page_size":251}`); err == nil {
		t.Error("page_size 251 accepted")
	}
}

func TestMediaTextsAreBoundedAndNeverFollowed(t *testing.T) {
	env, f := mediaEnvironment(t)
	f.item = `{"name":"` + mediaOne + `","description":` + mustJSON(instruction+strings.Repeat("x", 5000)) +
		`,"attribution":{"profileName":` + mustJSON(strings.Repeat("n", 800)) + `}}`
	out, err := env.invoke(t, mediaGetID, "m-all", `{"media_item":"`+mediaOne+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	if d := out["description"].(string); len([]rune(d)) != maxLongRunes+1 || !strings.HasPrefix(d, instruction) {
		t.Errorf("description length = %d", len([]rune(d)))
	}
	if n := out["attribution"].(map[string]any)["profileName"].(string); len([]rune(n)) != maxLineRunes+1 {
		t.Errorf("profileName length = %d", len([]rune(n)))
	}
	calls, _ := env.fake.calls()
	for _, c := range calls {
		if c.host != "mybusiness.googleapis.com" && c.host != "oauth2.googleapis.com" {
			t.Errorf("request to %s", c.host)
		}
	}
	if d := mediaGet.Risk.DataSensitivity; !strings.Contains(d, "personal data") {
		t.Errorf("data sensitivity = %q", d)
	}
}

func TestMediaPathsAreCheckedBeforeIO(t *testing.T) {
	env, _ := mediaEnvironment(t)
	for name, c := range map[string]struct{ op, connection, args string }{
		"list outside location binding": {mediaListID, "m-location", `{"location":"` + v4Other + `"}`},
		"list outside account binding":  {mediaListID, "m-account", `{"location":"` + v4ForeignAcct + `"}`},
		"get outside binding":           {mediaGetID, "m-location", `{"media_item":"` + v4Other + `/media/M1"}`},
		"get outside account":           {mediaGetID, "m-account", `{"media_item":"` + v4ForeignAcct + `/media/M1"}`},
		"v1 location form":              {mediaListID, "m-all", `{"location":"locations/2000002"}`},
		"path step":                     {mediaGetID, "m-all", `{"media_item":"` + v4Location + `/media/.."}`},
		"extra segment":                 {mediaGetID, "m-all", `{"media_item":"` + mediaOne + `/x"}`},
		"create outside binding":        {mediaCreateID, "m-location", strings.Replace(mediaCreateArgs(""), v4Location, v4Other, 1)},
		"update outside binding":        {mediaUpdateID, "m-account", `{"media_item":"` + v4ForeignAcct + `/media/M1","location_association":{"category":"MENU"},"update_mask":["locationAssociation.category"]}`},
		"delete outside binding":        {mediaDeleteID, "m-location", `{"media_item":"` + v4Other + `/media/M1"}`},
	} {
		_, err := env.change(t, c.op, c.connection, c.args)
		if err == nil {
			t.Errorf("%s: accepted", name)
		} else if strings.Contains(err.Error(), "2000003") || strings.Contains(err.Error(), "1000009") {
			t.Errorf("%s: names the other object: %v", name, err)
		}
	}
	env.noRequests(t)
}

func TestMediaLocationNotInTheBoundAccountIsRefusedAfterTheProof(t *testing.T) {
	env, f := mediaEnvironment(t)
	_, err := env.invoke(t, mediaListID, "m-account", `{"location":"`+v4ForeignLoc+`"}`)
	if !isInvalidRequest(err) {
		t.Fatalf("error = %v", err)
	}
	for _, m := range f.methods {
		if m != http.MethodGet {
			t.Errorf("method %s sent", m)
		}
	}
	calls, _ := env.fake.calls()
	for _, c := range calls {
		if strings.HasPrefix(c.path, "/v4/") {
			t.Errorf("media read after a failed proof: %s", c.path)
		}
	}
}

func TestMediaCreateNeedsConfirmationThatShowsUrlLocationAndCategory(t *testing.T) {
	env, f := mediaEnvironment(t)
	f.item = mediaFull
	needs := env.needsConfirmation(t, mediaCreateID, "m-all", mediaCreateArgs(`,"description":"Front door"`))
	for _, want := range []string{mediaSource, v4Location, "EXTERIOR", "PHOTO", "Front door"} {
		if !strings.Contains(needs.Summary, want) {
			t.Errorf("summary %q lacks %q", needs.Summary, want)
		}
	}
	env.noRequests(t)
	out, err := env.change(t, mediaCreateID, "m-all", mediaCreateArgs(`,"description":"Front door"`))
	if err != nil || out["name"] != mediaOne {
		t.Fatalf("result = %v, %v", out, err)
	}
	calls, _ := env.fake.calls()
	var posts int
	for _, m := range f.methods {
		if m == http.MethodPost {
			posts++
		}
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(sentBody(env)), &body); err != nil {
		t.Fatal(err)
	}
	if len(body) != 4 || body["sourceUrl"] != mediaSource || body["description"] != "Front door" ||
		body["mediaFormat"] != "PHOTO" || body["locationAssociation"].(map[string]any)["category"] != "EXTERIOR" {
		t.Errorf("body = %v", body)
	}
	if posts != 1 {
		t.Errorf("%d POSTs, calls = %+v", posts, calls)
	}
	for _, c := range calls {
		if c.host == "media.example.invalid" {
			t.Error("the source address was fetched")
		}
	}
}

func TestMediaCreateWithAPriceListItem(t *testing.T) {
	env, f := mediaEnvironment(t)
	f.item = mediaFull
	args := `{"location":"` + v4Location + `","media_format":"PHOTO","source_url":"` + mediaSource +
		`","location_association":{"price_list_item_id":"item-7"}}`
	if needs := env.needsConfirmation(t, mediaCreateID, "m-all", args); !strings.Contains(needs.Summary, "price list item item-7") {
		t.Errorf("summary = %q", needs.Summary)
	}
	if _, err := env.change(t, mediaCreateID, "m-all", args); err != nil {
		t.Fatal(err)
	}
	if body := sentBody(env); !strings.Contains(body, `"priceListItemId":"item-7"`) || strings.Contains(body, "category") {
		t.Errorf("body = %v", body)
	}
}

func TestMediaCreateRefusesWhatIsNotAnHttpsUrlOrCannotBeConfirmedWhole(t *testing.T) {
	env, _ := mediaEnvironment(t)
	assoc := `"location_association":{"category":"EXTERIOR"}`
	base := `{"location":"` + v4Location + `","media_format":"PHOTO",`
	for name, args := range map[string]string{
		"http":                base + `"source_url":"http://media.example.invalid/a.jpg",` + assoc + `}`,
		"no url":              base + assoc + `}`,
		"empty url":           base + `"source_url":"",` + assoc + `}`,
		"ftp":                 base + `"source_url":"ftp://media.example.invalid/a.jpg",` + assoc + `}`,
		"file":                base + `"source_url":"file:///etc/passwd",` + assoc + `}`,
		"user info":           base + `"source_url":"https://user:pw@192.0.2.1/a.jpg",` + assoc + `}`,
		"space":               base + `"source_url":"https://media.example.invalid/a b.jpg",` + assoc + `}`,
		"url over 2000 bytes": base + `"source_url":"https://media.example.invalid/` + strings.Repeat("a", 2000) + `",` + assoc + `}`,
		"no association":      base + `"source_url":"` + mediaSource + `"}`,
		"both associations":   base + `"source_url":"` + mediaSource + `","location_association":{"category":"MENU","price_list_item_id":"x"}}`,
		"unspecified":         base + `"source_url":"` + mediaSource + `","location_association":{"category":"CATEGORY_UNSPECIFIED"}}`,
		"cover description":   base + `"source_url":"` + mediaSource + `","location_association":{"category":"COVER"},"description":"x"}`,
		"control description": base + `"source_url":"` + mediaSource + `",` + assoc + `,"description":"a\u0007b"}`,
		"unknown format":      strings.Replace(base, "PHOTO", "GIF", 1) + `"source_url":"` + mediaSource + `",` + assoc + `}`,
		"data reference":      base + `"source_url":"` + mediaSource + `",` + assoc + `,"data_ref":"x"}`,
	} {
		_, err := env.change(t, mediaCreateID, "m-all", args)
		if err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	env.noRequests(t)
	// The longest address and description Google takes are shown whole.
	address := "https://media.example.invalid/" + strings.Repeat("a", 2000-len("https://media.example.invalid/"))
	description := strings.Repeat("d", maxMediaDescRune)
	summary := mediaSummary(mediaCreateID)(json.RawMessage(base + `"source_url":"` + address + `",` + assoc +
		`,"description":"` + description + `"}`))
	if !strings.Contains(summary, address) || !strings.Contains(summary, description) ||
		strings.HasPrefix(summary, "Refused before sending") || confirmationRunes(summary) > confirmLimit(mediaCreateID) {
		t.Errorf("summary of the longest call = %q", summary)
	}
}

func TestMediaUpdateNeedsAMaskAndOnlyTheCategory(t *testing.T) {
	env, f := mediaEnvironment(t)
	f.item = mediaFull
	good := `{"media_item":"` + mediaOne + `","location_association":{"category":"INTERIOR"},` +
		`"update_mask":["locationAssociation.category"]}`
	for name, args := range map[string]string{
		"no mask":     `{"media_item":"` + mediaOne + `","location_association":{"category":"INTERIOR"}}`,
		"empty mask":  `{"media_item":"` + mediaOne + `","location_association":{"category":"INTERIOR"},"update_mask":[]}`,
		"other mask":  `{"media_item":"` + mediaOne + `","location_association":{"category":"INTERIOR"},"update_mask":["description"]}`,
		"cover":       `{"media_item":"` + mediaOne + `","location_association":{"category":"COVER"},"update_mask":["locationAssociation.category"]}`,
		"profile":     `{"media_item":"` + mediaOne + `","location_association":{"category":"PROFILE"},"update_mask":["locationAssociation.category"]}`,
		"price item":  `{"media_item":"` + mediaOne + `","location_association":{"price_list_item_id":"x"},"update_mask":["locationAssociation.category"]}`,
		"no category": `{"media_item":"` + mediaOne + `","location_association":{},"update_mask":["locationAssociation.category"]}`,
		"description": `{"media_item":"` + mediaOne + `","location_association":{"category":"MENU"},"update_mask":["locationAssociation.category"],"description":"x"}`,
	} {
		if _, err := env.change(t, mediaUpdateID, "m-all", args); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	env.noRequests(t)
	if needs := env.needsConfirmation(t, mediaUpdateID, "m-all", good); !strings.Contains(needs.Summary, mediaOne) ||
		!strings.Contains(needs.Summary, "INTERIOR") {
		t.Errorf("summary = %q", needs.Summary)
	}
	env.noRequests(t)
	if _, err := env.change(t, mediaUpdateID, "m-all", good); err != nil {
		t.Fatal(err)
	}
	last := len(f.methods) - 1
	if f.methods[last] != http.MethodPatch || f.queries[last] != "updateMask=locationAssociation.category" ||
		sentBody(env) != `{"locationAssociation":{"category":"INTERIOR"}}` {
		t.Errorf("sent %s ?%s %s", f.methods[last], f.queries[last], sentBody(env))
	}
}

func TestMediaDeleteNeedsConfirmationAndSendsOneDelete(t *testing.T) {
	env, f := mediaEnvironment(t)
	args := `{"media_item":"` + mediaOne + `"}`
	needs := env.needsConfirmation(t, mediaDeleteID, "m-account", args)
	if !strings.Contains(needs.Summary, mediaOne) {
		t.Errorf("summary = %q", needs.Summary)
	}
	env.noRequests(t)
	out, err := env.change(t, mediaDeleteID, "m-account", args)
	if err != nil || out["deleted"] != true || out["media_item"] != mediaOne {
		t.Fatalf("result = %v, %v", out, err)
	}
	var deletes int
	for i, m := range f.methods {
		if m == http.MethodDelete {
			deletes++
			if f.queries[i] != "" {
				t.Errorf("delete carries ?%q", f.queries[i])
			}
		}
	}
	if deletes != 1 {
		t.Errorf("%d DELETEs", deletes)
	}
}

func TestMediaChangeFailureSaysWhenUnsureAndKeepsGoogleTextOut(t *testing.T) {
	env, f := mediaEnvironment(t)
	f.status = http.StatusServiceUnavailable
	_, err := env.change(t, mediaCreateID, "m-all", mediaCreateArgs(""))
	if err == nil || !strings.Contains(err.Error(), "outcome of the change is unknown") ||
		strings.Contains(err.Error(), "BACKEND") {
		t.Errorf("error = %v", err)
	}
	var posts int
	for _, m := range f.methods {
		if m == http.MethodPost {
			posts++
		}
	}
	if posts != 1 {
		t.Errorf("%d POSTs, want one without retry", posts)
	}
}

func TestMediaHasNoUploadAndEveryChangeIsConfirmed(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, d := range reg.Provider(Provider) {
		if !strings.HasPrefix(d.ID, Provider+".media.") {
			continue
		}
		ids = append(ids, d.ID)
		if strings.Contains(strings.ToLower(d.ID), "upload") {
			t.Errorf("upload tool %s", d.ID)
		}
		for _, a := range d.Arguments {
			if a.Name == "data_ref" || a.Name == "file" || a.Name == "path" {
				t.Errorf("%s takes %s", d.ID, a.Name)
			}
		}
		if d.Risk.Effect != capability.EffectRead && d.Risk.Confirmation != capability.ConfirmationRequired {
			t.Errorf("%s changes without confirmation", d.ID)
		}
	}
	if len(ids) != 5 {
		t.Errorf("media tools = %v", ids)
	}
}

func TestMediaConfirmationErrorIsTheCoreOne(t *testing.T) {
	env, _ := mediaEnvironment(t)
	_, err := env.core.Invoke(context.Background(), application.InvokeRequest{Operation: mediaDeleteID,
		Connection: "m-all", Arguments: json.RawMessage(`{"media_item":"` + mediaOne + `"}`)})
	var needs *application.ConfirmationRequiredError
	if !errors.As(err, &needs) {
		t.Fatalf("error = %v", err)
	}
}
