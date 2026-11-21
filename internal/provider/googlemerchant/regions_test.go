package googlemerchant

import (
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// regionWire is one request of a region test as the fake Google saw it, with its body.
type regionWire struct{ method, path, query, contentType, body string }

// regionAnswers makes the fake answer with route and records every API request. The recorded list is the
// complete traffic of the test, so a count of it shows that a change was sent once.
func (h *harness) regionAnswers(t *testing.T, route func(r regionWire) (int, string)) *[]regionWire {
	t.Helper()
	var seen []regionWire
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		if req.URL.Host != "merchantapi.googleapis.com" || req.URL.Scheme != "https" ||
			req.Header.Get("Authorization") != "Bearer "+testAccess {
			t.Errorf("request %s left the fixed origin or lacks the bearer token", req.URL.Path)
		}
		var body string
		if req.Body != nil {
			data, _ := io.ReadAll(req.Body)
			body = string(data)
		}
		w := regionWire{req.Method, req.URL.Path, req.URL.RawQuery, req.Header.Get("Content-Type"), body}
		seen = append(seen, w)
		status, answer := route(w)
		return status, answer, nil
	}
	return &seen
}

func always(status int, body string) func(regionWire) (int, string) {
	return func(regionWire) (int, string) { return status, body }
}

const regionFixture = `{"name":"accounts/1000001/regions/north-1","displayName":"North","postalCodeArea":{` +
	`"regionCode":"US","postalCodes":[{"begin":"94108"},{"begin":"95*","end":"96*"}]},` +
	`"shippingEligible":true,"regionalInventoryEligible":false,"unknownMember":"never held"}`

func TestRegionToolsAreDeclaredWithTheirRisk(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	for id, w := range map[string]struct {
		effect      capability.Effect
		idempotency capability.Idempotency
		change      bool
	}{
		regionsListID:   {capability.EffectRead, capability.IdempotencySafe, false},
		regionsGetID:    {capability.EffectRead, capability.IdempotencySafe, false},
		regionsCreateID: {capability.EffectCreate, capability.IdempotencyNonIdempotent, true},
		regionsUpdateID: {capability.EffectUpdate, capability.IdempotencyIdempotent, true},
	} {
		d, _, ok := reg.Lookup(id)
		if !ok || d.Version != 1 || d.Risk.Effect != w.effect || d.Risk.Idempotency != w.idempotency ||
			d.RequiresToolAllowList != w.change || !d.Risk.OpenWorld {
			t.Fatalf("%s = %+v, %v", id, d, ok)
		}
		if w.change {
			if d.Risk.Confirmation != capability.ConfirmationRequired || reg.ConfirmationSummary(id) == nil {
				t.Errorf("%s needs a confirmation and its summary", id)
			}
			for _, part := range []string{"administrator role", "some minutes", "only that Google accepted"} {
				if !strings.Contains(d.Description, part) {
					t.Errorf("%s description misses %q", id, part)
				}
			}
			for _, profile := range metadata.Profiles {
				if slices.Contains(profile.Tools, id) {
					t.Errorf("%s is in profile %s", id, profile.ID)
				}
			}
		} else if d.Risk.Confirmation != capability.ConfirmationNone {
			t.Errorf("%s asks for confirmation", id)
		}
		if scopes := metadata.Token.Scopes[id]; len(scopes) != 1 || scopes[0] != contentScope {
			t.Errorf("%s scopes = %v", id, scopes)
		}
		if !strings.Contains(string(d.InputSchema), `"additionalProperties":false`) ||
			strings.Contains(string(d.InputSchema), "updateMask") || strings.Contains(string(d.InputSchema), `"body"`) {
			t.Errorf("%s input schema is open: %s", id, d.InputSchema)
		}
	}
}

func TestRegionsListAndGetShowTheDocumentedMembersAndCutTheRest(t *testing.T) {
	h := newHarness(t)
	long := strings.Repeat("n", 500)
	rows := make([]string, 0, 3)
	rows = append(rows, regionFixture,
		`{"name":"accounts/1000001/regions/r2","displayName":"`+long+`","geotargetArea":{"geotargetCriteriaIds":["1","2"]}}`,
		`{"name":"accounts/1000001/regions/r3","radiusArea":{"radius":12.5,"latLng":{"latitude":1.5,"longitude":-2},`+
			`"regionCode":"CH","radiusUnits":"KILOMETERS"}}`)
	seen := h.regionAnswers(t, func(r regionWire) (int, string) {
		if strings.HasSuffix(r.path, "/regions") {
			return 200, `{"regions":[` + strings.Join(rows, ",") + `],"nextPageToken":"two"}`
		}
		return 200, regionFixture
	})
	out, err := h.call(t, handleRegionsList, `{"account":"1000001","page_size":3}`)
	if err != nil {
		t.Fatal(err)
	}
	if (*seen)[0].path != "/accounts/v1/accounts/1000001/regions" || (*seen)[0].query != "pageSize=3" {
		t.Errorf("request = %+v", (*seen)[0])
	}
	list := out["regions"].([]any)
	first, second, third := list[0].(map[string]any), list[1].(map[string]any), list[2].(map[string]any)
	if first["shippingEligible"] != true || first["unknownMember"] != nil ||
		len(first["postalCodeArea"].(map[string]any)["postalCodes"].([]any)) != 2 {
		t.Errorf("first = %v", first)
	}
	if got := []rune(second["displayName"].(string)); len(got) != maxNameRunes {
		t.Errorf("displayName has %d runes", len(got))
	}
	if third["radiusArea"].(map[string]any)["radius"] != 12.5 {
		t.Errorf("third = %v", third)
	}
	next, _ := out["next_cursor"].(string)
	if next == "" || next == "two" {
		t.Fatalf("next_cursor = %q", next)
	}
	if _, err := h.call(t, handleRegionsList, `{"account":"1000001","page_size":3,"cursor":"`+next+`"}`); err != nil {
		t.Fatal(err)
	}
	if got := (*seen)[1].query; !strings.Contains(got, "pageToken=two") {
		t.Errorf("query = %q", got)
	}
	if _, err := h.call(t, handleRegionsList, `{"account":"1000001","page_size":4,"cursor":"`+next+`"}`); !isInvalidRequest(err) {
		t.Errorf("a cursor of other arguments = %v", err)
	}
	got, err := h.call(t, handleRegionsGet, `{"account":"1000001","region_id":"north-1"}`)
	if err != nil {
		t.Fatal(err)
	}
	if got["name"] != "accounts/1000001/regions/north-1" || got["displayName"] != "North" {
		t.Errorf("get = %v", got)
	}
	if last := (*seen)[len(*seen)-1]; last.path != "/accounts/v1/accounts/1000001/regions/north-1" || last.method != http.MethodGet {
		t.Errorf("get request = %+v", last)
	}
}

func TestRegionListsAreCapped(t *testing.T) {
	h := newHarness(t)
	ranges := make([]string, maxPostalRanges+50)
	for i := range ranges {
		ranges[i] = `{"begin":"` + strconv.Itoa(10000+i) + `"}`
	}
	ids := make([]string, 150)
	for i := range ids {
		ids[i] = `"` + strconv.Itoa(i+1) + `"`
	}
	h.regionAnswers(t, always(200, `{"name":"accounts/1/regions/a","postalCodeArea":{"regionCode":"US","postalCodes":[`+
		strings.Join(ranges, ",")+`]},"geotargetArea":{"geotargetCriteriaIds":[`+strings.Join(ids, ",")+`]}}`))
	out, err := h.call(t, handleRegionsGet, `{"account":"1","region_id":"a"}`)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(out["postalCodeArea"].(map[string]any)["postalCodes"].([]any)); n != maxPostalRanges {
		t.Errorf("%d ranges", n)
	}
	if n := len(out["geotargetArea"].(map[string]any)["geotargetCriteriaIds"].([]any)); n != maxListItems {
		t.Errorf("%d geotargets", n)
	}
}

func TestRegionIDAndAccountAreRefusedBeforeSecretOrIO(t *testing.T) {
	for _, bad := range []string{"", "a/b", "..", "a.b", ".", "a b", "a?x=1", "a%2Fb", "-a", "_a",
		strings.Repeat("a", maxRegionIDLen+1), "ä", "a\n", "../accounts/2"} {
		for name, call := range map[string]struct {
			handler capability.Handler
			args    map[string]any
		}{
			"get":    {handleRegionsGet, map[string]any{"account": "1000001", "region_id": bad}},
			"create": {handleRegionsCreate, map[string]any{"account": "1000001", "region_id": bad, "radius_area": json.RawMessage(validRadius())}},
			"update": {handleRegionsUpdate, map[string]any{"account": "1000001", "region_id": bad,
				"update_mask": []string{"displayName"}, "display_name": "x"}},
		} {
			h := newHarness(t)
			raw, _ := json.Marshal(call.args)
			if _, err := h.call(t, call.handler, string(raw)); !isInvalidRequest(err) {
				t.Errorf("%s with region_id %q = %v, want invalid request", name, bad, err)
			}
			h.untouched(t)
		}
	}
	for _, id := range []string{"a", "north-1", "R_2", strings.Repeat("a", maxRegionIDLen), "987654"} {
		if !validRegionID(id) {
			t.Errorf("region ID %q refused", id)
		}
	}
	h := newHarness(t, "accounts/1000001")
	for _, handler := range []capability.Handler{handleRegionsList, handleRegionsGet, handleRegionsCreate, handleRegionsUpdate} {
		_, err := h.call(t, handler, `{"account":"2000002","region_id":"a","update_mask":["displayName"],"display_name":"x",`+
			`"radius_area":`+validRadius()+`}`)
		if !isInvalidRequest(err) || strings.Contains(err.Error(), "2000002") {
			t.Errorf("foreign account = %v", err)
		}
		if _, err := h.call(t, handler, `{"account":"01","region_id":"a"}`); !isInvalidRequest(err) {
			t.Errorf("malformed account = %v", err)
		}
	}
	h.untouched(t)
}

func validRadius() string {
	return `{"radius":5,"latLng":{"latitude":47.3,"longitude":8.5},"regionCode":"CH","radiusUnits":"KILOMETERS"}`
}

func TestRegionsOfASubAccountAreProvenBeforeDetailOrChange(t *testing.T) {
	route := func(subs string) func(regionWire) (int, string) {
		return func(r regionWire) (int, string) {
			if strings.HasSuffix(r.path, ":listSubaccounts") {
				return 200, `{"accounts":[` + subs + `]}`
			}
			return 200, regionFixture
		}
	}
	calls := map[string]struct {
		handler capability.Handler
		args    string
	}{
		"get":    {handleRegionsGet, `{"account":"20","region_id":"a"}`},
		"list":   {handleRegionsList, `{"account":"20"}`},
		"create": {handleRegionsCreate, `{"account":"20","region_id":"a","radius_area":` + validRadius() + `}`},
		"update": {handleRegionsUpdate, `{"account":"20","region_id":"a","update_mask":["displayName"],"display_name":"x"}`},
	}
	for name, c := range calls {
		h := newHarness(t, "advanced-accounts/10")
		seen := h.regionAnswers(t, route(accountRow("20", "Twenty")))
		if _, err := h.call(t, c.handler, c.args); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(*seen) != 2 || (*seen)[0].path != "/accounts/v1/accounts/10:listSubaccounts" ||
			!strings.Contains((*seen)[1].path, "/accounts/20/regions") {
			t.Errorf("%s requests = %+v", name, *seen)
		}
		h = newHarness(t, "advanced-accounts/10")
		seen = h.regionAnswers(t, route(accountRow("30", "Thirty")))
		_, err := h.call(t, c.handler, c.args)
		if !isInvalidRequest(err) || strings.Contains(err.Error(), "20") {
			t.Errorf("%s of a foreign account = %v", name, err)
		}
		if len(*seen) != 1 || !strings.HasSuffix((*seen)[0].path, ":listSubaccounts") {
			t.Errorf("%s sent more than the proof read: %+v", name, *seen)
		}
	}
}

func TestRegionsCreateSendsOneCheckedRequest(t *testing.T) {
	h := newHarness(t)
	seen := h.regionAnswers(t, always(200, regionFixture))
	out, err := h.call(t, handleRegionsCreate, `{"account":"1000001","region_id":"north-1","display_name":"North",`+
		`"postal_code_area":{"regionCode":"US","postalCodes":[{"begin":"94108"},{"begin":"95*","end":"96*"}]}}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(*seen) != 1 {
		t.Fatalf("%d requests", len(*seen))
	}
	w := (*seen)[0]
	if w.method != http.MethodPost || w.path != "/accounts/v1/accounts/1000001/regions" || w.query != "regionId=north-1" ||
		w.contentType != "application/json" {
		t.Errorf("request = %+v", w)
	}
	want := `{"displayName":"North","postalCodeArea":{"postalCodes":[{"begin":"94108"},{"begin":"95*","end":"96*"}],` +
		`"regionCode":"US"}}`
	if w.body != want {
		t.Errorf("body = %s, want %s", w.body, want)
	}
	if out["accepted"] != true || out["region"].(map[string]any)["name"] != "accounts/1000001/regions/north-1" {
		t.Errorf("result = %v", out)
	}
	for _, c := range []struct{ name, body string }{
		{"geotarget", `"geotarget_area":{"geotargetCriteriaIds":["21137","21138"]}`},
		{"radius", `"radius_area":` + validRadius()},
	} {
		if _, err := h.call(t, handleRegionsCreate, `{"account":"1000001","region_id":"r-`+c.name+`",`+c.body+`}`); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
	}
	if got := (*seen)[1].body; got != `{"geotargetArea":{"geotargetCriteriaIds":["21137","21138"]}}` {
		t.Errorf("geotarget body = %s", got)
	}
	if got := (*seen)[2].body; got != `{"radiusArea":{"latLng":{"latitude":47.3,"longitude":8.5},"radius":5,`+
		`"radiusUnits":"KILOMETERS","regionCode":"CH"}}` {
		t.Errorf("radius body = %s", got)
	}
}

func TestRegionsCreateRefusesBadArgumentsBeforeSecretOrIO(t *testing.T) {
	postal := `"postal_code_area":{"regionCode":"US","postalCodes":[{"begin":"94108"}]}`
	for name, args := range map[string]string{
		"no area":            `{"region_id":"a","account":"1"}`,
		"two areas":          `{"region_id":"a","account":"1",` + postal + `,"radius_area":` + validRadius() + `}`,
		"empty ranges":       `{"region_id":"a","account":"1","postal_code_area":{"regionCode":"US","postalCodes":[]}}`,
		"lower case country": `{"region_id":"a","account":"1","postal_code_area":{"regionCode":"us","postalCodes":[{"begin":"1"}]}}`,
		"bad postal code":    `{"region_id":"a","account":"1","postal_code_area":{"regionCode":"US","postalCodes":[{"begin":"9/4"}]}}`,
		"mixed end":          `{"region_id":"a","account":"1","postal_code_area":{"regionCode":"US","postalCodes":[{"begin":"95*","end":"96"}]}}`,
		"geotarget text":     `{"region_id":"a","account":"1","geotarget_area":{"geotargetCriteriaIds":["x"]}}`,
		"geotarget twice":    `{"region_id":"a","account":"1","geotarget_area":{"geotargetCriteriaIds":["5","5"]}}`,
		"radius zero":        `{"region_id":"a","account":"1","radius_area":{"radius":0,"latLng":{"latitude":1,"longitude":1},"regionCode":"CH"}}`,
		"radius no center":   `{"region_id":"a","account":"1","radius_area":{"radius":3,"regionCode":"CH"}}`,
		"latitude range":     `{"region_id":"a","account":"1","radius_area":{"radius":3,"latLng":{"latitude":91,"longitude":1},"regionCode":"CH"}}`,
		"units":              `{"region_id":"a","account":"1","radius_area":{"radius":3,"latLng":{"latitude":1,"longitude":1},"regionCode":"CH","radiusUnits":"PARSECS"}}`,
		"control name":       `{"region_id":"a","account":"1","display_name":"a\u0000b",` + postal + `}`,
		"long name":          `{"region_id":"a","account":"1","display_name":"` + strings.Repeat("x", maxRegionNameRunes+1) + `",` + postal + `}`,
	} {
		h := newHarness(t)
		if _, err := h.call(t, handleRegionsCreate, args); !isInvalidRequest(err) {
			t.Errorf("%s = %v, want invalid request", name, err)
		}
		h.untouched(t)
	}
}

func TestRegionsUpdateBuildsTheMaskFromTheAllowlistOnly(t *testing.T) {
	h := newHarness(t)
	seen := h.regionAnswers(t, always(200, regionFixture))
	out, err := h.call(t, handleRegionsUpdate, `{"account":"1000001","region_id":"north-1",`+
		`"update_mask":["radiusArea","displayName","postalCodeArea"],"display_name":"East","radius_area":`+validRadius()+`}`)
	if err != nil {
		t.Fatal(err)
	}
	w := (*seen)[0]
	if w.method != http.MethodPatch || w.path != "/accounts/v1/accounts/1000001/regions/north-1" ||
		w.query != "updateMask=displayName%2CpostalCodeArea%2CradiusArea" || w.contentType != "application/json" {
		t.Errorf("request = %+v", w)
	}
	if w.body != `{"displayName":"East","radiusArea":{"latLng":{"latitude":47.3,"longitude":8.5},"radius":5,`+
		`"radiusUnits":"KILOMETERS","regionCode":"CH"}}` {
		t.Errorf("body = %s", w.body)
	}
	if out["accepted"] != true {
		t.Errorf("result = %v", out)
	}
	if _, err := h.call(t, handleRegionsUpdate, `{"account":"1000001","region_id":"a","update_mask":["displayName"],`+
		`"display_name":"Only"}`); err != nil {
		t.Fatal(err)
	}
	if got := (*seen)[1]; got.query != "updateMask=displayName" || got.body != `{"displayName":"Only"}` {
		t.Errorf("rename = %+v", got)
	}
}

func TestRegionsUpdateRefusesMasksOutsideTheAllowlistBeforeSecretOrIO(t *testing.T) {
	postal := `"postal_code_area":{"regionCode":"US","postalCodes":[{"begin":"1"}]}`
	for name, args := range map[string]string{
		"no mask":           `{"region_id":"a","account":"1","display_name":"x"}`,
		"empty mask":        `{"region_id":"a","account":"1","update_mask":[],"display_name":"x"}`,
		"name path":         `{"region_id":"a","account":"1","update_mask":["name"],"display_name":"x"}`,
		"sub path":          `{"region_id":"a","account":"1","update_mask":["postalCodeArea.regionCode"],` + postal + `}`,
		"comma text":        `{"region_id":"a","account":"1","update_mask":["displayName,name"],"display_name":"x"}`,
		"wildcard":          `{"region_id":"a","account":"1","update_mask":["*"],"display_name":"x"}`,
		"output only":       `{"region_id":"a","account":"1","update_mask":["shippingEligible"],"display_name":"x"}`,
		"repeated":          `{"region_id":"a","account":"1","update_mask":["displayName","displayName"],"display_name":"x"}`,
		"name without path": `{"region_id":"a","account":"1","update_mask":["postalCodeArea"],"display_name":"x",` + postal + `}`,
		"path without name": `{"region_id":"a","account":"1","update_mask":["displayName"]}`,
		"area not in mask":  `{"region_id":"a","account":"1","update_mask":["displayName"],"display_name":"x",` + postal + `}`,
		"mask area no body": `{"region_id":"a","account":"1","update_mask":["radiusArea"]}`,
		"two areas":         `{"region_id":"a","account":"1","update_mask":["postalCodeArea","radiusArea"],` + postal + `,"radius_area":` + validRadius() + `}`,
		"bad area":          `{"region_id":"a","account":"1","update_mask":["radiusArea"],"radius_area":{"radius":-1}}`,
	} {
		h := newHarness(t)
		if _, err := h.call(t, handleRegionsUpdate, args); !isInvalidRequest(err) {
			t.Errorf("%s = %v, want invalid request", name, err)
		}
		h.untouched(t)
	}
}

func TestRegionChangesAreSentOnceAndReportAnUnknownOutcome(t *testing.T) {
	create := `{"account":"1000001","region_id":"a","radius_area":` + validRadius() + `}`
	update := `{"account":"1000001","region_id":"a","update_mask":["displayName"],"display_name":"x"}`
	const unknown = "outcome of the change is unknown"
	for name, c := range map[string]struct {
		status  int
		body    string
		class   provider.Class
		unknown bool
	}{
		"server error": {500, `{"error":{"status":"INTERNAL"}}`, provider.ClassProviderError, true},
		"not json":     {200, `<html>`, provider.ClassInvalidResponse, true},
		"quota":        {429, quotaBody(reasonMinuteQuota), provider.ClassRateLimited, false},
		"admin":        {403, googleErrorBody, provider.ClassPermission, false},
		"exists":       {409, `{"error":{"status":"ALREADY_EXISTS"}}`, "", false},
		"expired":      {401, `{"error":{"status":"UNAUTHENTICATED"}}`, provider.ClassAuth, false},
	} {
		for tool, call := range map[string]struct {
			handler capability.Handler
			args    string
		}{"create": {handleRegionsCreate, create}, "update": {handleRegionsUpdate, update}} {
			h := newHarness(t)
			header := "application/json"
			if c.body == `<html>` {
				header = "text/html"
			}
			h.google.api = func(*http.Request) (int, string, http.Header) {
				return c.status, c.body, http.Header{"Content-Type": {header}}
			}
			_, err := h.call(t, call.handler, call.args)
			if err == nil {
				t.Fatalf("%s %s: no error", name, tool)
			}
			if got := len(h.google.seen()); got != 1 {
				t.Errorf("%s %s: %d requests, want one", name, tool, got)
			}
			if c.class != "" && classOf(err) != c.class {
				t.Errorf("%s %s: class = %q, want %q", name, tool, classOf(err), c.class)
			}
			if strings.Contains(err.Error(), unknown) != c.unknown {
				t.Errorf("%s %s: error %q, unknown outcome %t", name, tool, err, c.unknown)
			}
			if strings.Contains(err.Error(), "synthetic provider echo") {
				t.Errorf("%s %s: the message carries Google's text", name, tool)
			}
		}
	}
}

func TestRegionReadsMapGoogleErrors(t *testing.T) {
	for status, class := range map[int]provider.Class{401: provider.ClassAuth, 403: provider.ClassPermission,
		404: provider.ClassNotFound, 429: provider.ClassRateLimited} {
		h := newHarness(t)
		h.regionAnswers(t, always(status, googleErrorBody))
		if _, err := h.call(t, handleRegionsGet, `{"account":"1","region_id":"a"}`); classOf(err) != class {
			t.Errorf("status %d = class %q, want %q", status, classOf(err), class)
		}
	}
}

func TestRegionConfirmationSummariesNameAccountRegionAndMembers(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	create, update := reg.ConfirmationSummary(regionsCreateID), reg.ConfirmationSummary(regionsUpdateID)
	got := create(json.RawMessage(`{"account":"1000001","region_id":"north-1","display_name":"North",` +
		`"geotarget_area":{"geotargetCriteriaIds":["1"]}}`))
	for _, part := range []string{"1000001", `"north-1"`, `"North"`, "geotargetArea", "afterwards"} {
		if !strings.Contains(got, part) {
			t.Errorf("create summary %q misses %q", got, part)
		}
	}
	got = update(json.RawMessage(`{"account":"1000001","region_id":"north-1",` +
		`"update_mask":["displayName","radiusArea"],"display_name":"East"}`))
	for _, part := range []string{"1000001", `"north-1"`, "displayName, radiusArea", `"East"`, "afterwards"} {
		if !strings.Contains(got, part) {
			t.Errorf("update summary %q misses %q", got, part)
		}
	}
}
