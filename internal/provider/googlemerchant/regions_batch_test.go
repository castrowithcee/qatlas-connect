package googlemerchant

import (
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const (
	postalEntry  = `{"region_id":"north-1","display_name":"North","postal_code_area":{"regionCode":"US","postalCodes":[{"begin":"94108"}]}}`
	geoEntry     = `{"region_id":"east-1","geotarget_area":{"geotargetCriteriaIds":["21137"]}}`
	updateEntryA = `{"region_id":"north-1","update_mask":["displayName"],"display_name":"North and east"}`
	updateEntryB = `{"region_id":"east-1","update_mask":["geotargetArea"],"geotarget_area":{"geotargetCriteriaIds":["21138"]}}`
)

func batchRegionIDs(n int) []string {
	ids := make([]string, n)
	for i := range ids {
		ids[i] = "r" + strconv.Itoa(i)
	}
	return ids
}

func TestRegionBatchToolsAreDeclaredWithTheirRisk(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	for id, w := range map[string]struct {
		effect      capability.Effect
		idempotency capability.Idempotency
	}{
		regionsDeleteID:      {capability.EffectDelete, capability.IdempotencyIdempotent},
		regionsBatchCreateID: {capability.EffectCreate, capability.IdempotencyNonIdempotent},
		regionsBatchUpdateID: {capability.EffectUpdate, capability.IdempotencyIdempotent},
		regionsBatchDeleteID: {capability.EffectDelete, capability.IdempotencyIdempotent},
	} {
		d, _, ok := reg.Lookup(id)
		if !ok || d.Version != 1 || d.Risk.Effect != w.effect || d.Risk.Idempotency != w.idempotency ||
			!d.RequiresToolAllowList || !d.Risk.OpenWorld || d.Risk.Confirmation != capability.ConfirmationRequired ||
			reg.ConfirmationSummary(id) == nil {
			t.Fatalf("%s = %+v, %v", id, d, ok)
		}
		parts := []string{"administrator role", "some minutes", "only that Google accepted"}
		if strings.Contains(id, "delete") {
			parts = append(parts, "never repeated", "permanent", "can be affected")
		}
		if strings.Contains(id, "batch") {
			parts = append(parts, "exactly one request", "counts as one call", "all or none", "1 to 100")
		}
		for _, part := range parts {
			if !strings.Contains(d.Description, part) {
				t.Errorf("%s description misses %q", id, part)
			}
		}
		for _, profile := range metadata.Profiles {
			if slices.Contains(profile.Tools, id) {
				t.Errorf("%s is in profile %s", id, profile.ID)
			}
		}
		if scopes := metadata.Token.Scopes[id]; len(scopes) != 1 || scopes[0] != contentScope {
			t.Errorf("%s scopes = %v", id, scopes)
		}
		schema := string(d.InputSchema)
		if !strings.Contains(schema, `"additionalProperties":false`) || strings.Contains(schema, "updateMask") ||
			strings.Contains(schema, `"body"`) || strings.Contains(schema, `"parent"`) || strings.Contains(schema, `"name"`) {
			t.Errorf("%s input schema is open: %s", id, schema)
		}
		if strings.Contains(id, "batch") && !strings.Contains(schema, `"maxItems":100`) {
			t.Errorf("%s has no cap of 100: %s", id, schema)
		}
	}
}

func TestRegionBatchCreateIsOneRequestWithCheckedBody(t *testing.T) {
	h := newHarness(t)
	seen := h.regionAnswers(t, always(200, `{"regions":[`+regionFixture+`,{"name":"accounts/1000001/regions/east-1"}],"unknown":1}`))
	out, err := h.call(t, handleRegionsBatchCreate, `{"account":"1000001","regions":[`+postalEntry+`,`+geoEntry+`]}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(*seen) != 1 {
		t.Fatalf("%d requests, want one for the whole batch", len(*seen))
	}
	w := (*seen)[0]
	if w.method != http.MethodPost || w.path != "/accounts/v1/accounts/1000001/regions:batchCreate" || w.query != "" ||
		w.contentType != "application/json" {
		t.Errorf("request = %+v", w)
	}
	want := `{"requests":[{"parent":"accounts/1000001","region":{"displayName":"North","postalCodeArea":{` +
		`"postalCodes":[{"begin":"94108"}],"regionCode":"US"}},"regionId":"north-1"},{"parent":"accounts/1000001",` +
		`"region":{"geotargetArea":{"geotargetCriteriaIds":["21137"]}},"regionId":"east-1"}]}`
	if w.body != want {
		t.Errorf("body = %s\nwant   %s", w.body, want)
	}
	list := out["regions"].([]any)
	if out["accepted"] != true || out["requested"] != float64(2) || len(list) != 2 || out["unknown"] != nil ||
		list[0].(map[string]any)["unknownMember"] != nil || list[0].(map[string]any)["shippingEligible"] != true {
		t.Errorf("result = %v", out)
	}
}

func TestRegionBatchOfTheMaximumIsStillOneRequest(t *testing.T) {
	ids := batchRegionIDs(maxRegionBatch)
	entries := make([]string, len(ids))
	rows := make([]string, 150)
	for i := range rows {
		rows[i] = `{"name":"accounts/1/regions/r` + strconv.Itoa(i) + `"}`
	}
	for i, id := range ids {
		entries[i] = `{"region_id":"` + id + `","radius_area":` + validRadius() + `}`
	}
	h := newHarness(t)
	seen := h.regionAnswers(t, always(200, `{"regions":[`+strings.Join(rows, ",")+`]}`))
	out, err := h.call(t, handleRegionsBatchCreate, `{"account":"1","regions":[`+strings.Join(entries, ",")+`]}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(*seen) != 1 {
		t.Fatalf("%d requests for %d regions, want one", len(*seen), maxRegionBatch)
	}
	var sent struct{ Requests []json.RawMessage }
	if json.Unmarshal([]byte((*seen)[0].body), &sent) != nil || len(sent.Requests) != maxRegionBatch {
		t.Errorf("body holds %d requests", len(sent.Requests))
	}
	if n := len(out["regions"].([]any)); n != maxRegionBatch || out["requested"] != float64(maxRegionBatch) {
		t.Errorf("%d regions held, result %v", n, out["requested"])
	}
	// Every other batch tool sends one request for the same number of entries.
	for name, c := range map[string]struct {
		call func() (map[string]any, error)
	}{
		"update": {func() (map[string]any, error) {
			parts := make([]string, len(ids))
			for i, id := range ids {
				parts[i] = `{"region_id":"` + id + `","update_mask":["displayName"],"display_name":"x"}`
			}
			return h.call(t, handleRegionsBatchUpdate, `{"account":"1","regions":[`+strings.Join(parts, ",")+`]}`)
		}},
		"delete": {func() (map[string]any, error) {
			raw, _ := json.Marshal(map[string]any{"account": "1", "region_ids": ids})
			return h.call(t, handleRegionsBatchDelete, string(raw))
		}},
	} {
		before := len(*seen)
		if _, err := c.call(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(*seen)-before != 1 {
			t.Errorf("%s sent %d requests, want one", name, len(*seen)-before)
		}
	}
}

func TestRegionBatchUpdateAndDeleteBodies(t *testing.T) {
	h := newHarness(t)
	seen := h.regionAnswers(t, func(r regionWire) (int, string) {
		if strings.HasSuffix(r.path, ":batchDelete") {
			return 200, `{}`
		}
		return 200, `{"regions":[` + regionFixture + `]}`
	})
	out, err := h.call(t, handleRegionsBatchUpdate, `{"account":"1000001","regions":[`+updateEntryA+`,`+updateEntryB+`]}`)
	if err != nil {
		t.Fatal(err)
	}
	w := (*seen)[0]
	want := `{"requests":[{"region":{"displayName":"North and east","name":"accounts/1000001/regions/north-1"},"updateMask":"displayName"},` +
		`{"region":{"geotargetArea":{"geotargetCriteriaIds":["21138"]},"name":"accounts/1000001/regions/east-1"},"updateMask":"geotargetArea"}]}`
	if w.method != http.MethodPost || w.path != "/accounts/v1/accounts/1000001/regions:batchUpdate" || w.body != want {
		t.Errorf("update request = %+v\nwant body %s", w, want)
	}
	if out["accepted"] != true || out["requested"] != float64(2) {
		t.Errorf("update result = %v", out)
	}
	out, err = h.call(t, handleRegionsBatchDelete, `{"account":"1000001","region_ids":["north-1","east-1"]}`)
	if err != nil {
		t.Fatal(err)
	}
	w = (*seen)[1]
	if w.method != http.MethodPost || w.path != "/accounts/v1/accounts/1000001/regions:batchDelete" ||
		w.body != `{"requests":[{"name":"accounts/1000001/regions/north-1"},{"name":"accounts/1000001/regions/east-1"}]}` || len(*seen) != 2 {
		t.Errorf("delete request = %+v", w)
	}
	names := out["names"].([]any)
	if out["accepted"] != true || out["requested"] != float64(2) || len(names) != 2 ||
		names[0] != "accounts/1000001/regions/north-1" {
		t.Errorf("delete result = %v", out)
	}
}

func TestRegionDeleteIsOneRequest(t *testing.T) {
	h := newHarness(t)
	seen := h.regionAnswers(t, always(200, `{}`))
	out, err := h.call(t, handleRegionsDelete, `{"account":"1000001","region_id":"north-1"}`)
	if err != nil {
		t.Fatal(err)
	}
	w := (*seen)[0]
	if len(*seen) != 1 || w.method != http.MethodDelete || w.path != "/accounts/v1/accounts/1000001/regions/north-1" ||
		w.body != "" {
		t.Errorf("requests = %+v", *seen)
	}
	if out["accepted"] != true || out["name"] != "accounts/1000001/regions/north-1" {
		t.Errorf("result = %v", out)
	}
}

func TestRegionBatchRefusesAnyBadEntryBeforeSecretOrIO(t *testing.T) {
	good := postalEntry
	many := func(n int) string {
		parts := make([]string, n)
		for i := range parts {
			parts[i] = `{"region_id":"r` + strconv.Itoa(i) + `","radius_area":` + validRadius() + `}`
		}
		return strings.Join(parts, ",")
	}
	badID := func(id string) string { return `{"region_id":"` + id + `","radius_area":` + validRadius() + `}` }
	for name, c := range map[string]struct {
		handler capability.Handler
		args    string
	}{
		"create empty":        {handleRegionsBatchCreate, `{"account":"1","regions":[]}`},
		"create missing":      {handleRegionsBatchCreate, `{"account":"1"}`},
		"create over cap":     {handleRegionsBatchCreate, `{"account":"1","regions":[` + many(maxRegionBatch+1) + `]}`},
		"create bad id last":  {handleRegionsBatchCreate, `{"account":"1","regions":[` + good + `,` + badID("a/b") + `]}`},
		"create dot id":       {handleRegionsBatchCreate, `{"account":"1","regions":[` + badID("..") + `]}`},
		"create no area":      {handleRegionsBatchCreate, `{"account":"1","regions":[` + good + `,{"region_id":"b"}]}`},
		"create two areas":    {handleRegionsBatchCreate, `{"account":"1","regions":[{"region_id":"b","radius_area":` + validRadius() + `,"geotarget_area":{"geotargetCriteriaIds":["1"]}}]}`},
		"create bad name":     {handleRegionsBatchCreate, `{"account":"1","regions":[` + good + `,{"region_id":"b","display_name":"a\u0000","radius_area":` + validRadius() + `}]}`},
		"create duplicate":    {handleRegionsBatchCreate, `{"account":"1","regions":[` + good + `,` + good + `]}`},
		"create case twin":    {handleRegionsBatchCreate, `{"account":"1","regions":[` + badID("Ab") + `,` + badID("aB") + `]}`},
		"create with mask":    {handleRegionsBatchCreate, `{"account":"1","regions":[{"region_id":"b","update_mask":["displayName"],"radius_area":` + validRadius() + `}]}`},
		"create foreign acct": {handleRegionsBatchCreate, `{"account":"01","regions":[` + good + `]}`},
		"update empty":        {handleRegionsBatchUpdate, `{"account":"1","regions":[]}`},
		"update over cap":     {handleRegionsBatchUpdate, `{"account":"1","regions":[` + strings.Repeat(updateEntryA+",", maxRegionBatch) + updateEntryA + `]}`},
		"update bad mask":     {handleRegionsBatchUpdate, `{"account":"1","regions":[` + updateEntryA + `,{"region_id":"b","update_mask":["name"],"display_name":"x"}]}`},
		"update no mask":      {handleRegionsBatchUpdate, `{"account":"1","regions":[{"region_id":"b","display_name":"x"}]}`},
		"update area no body": {handleRegionsBatchUpdate, `{"account":"1","regions":[{"region_id":"b","update_mask":["radiusArea"]}]}`},
		"update bad id":       {handleRegionsBatchUpdate, `{"account":"1","regions":[` + updateEntryA + `,{"region_id":"a b","update_mask":["displayName"],"display_name":"x"}]}`},
		"update duplicate":    {handleRegionsBatchUpdate, `{"account":"1","regions":[` + updateEntryA + `,` + updateEntryA + `]}`},
		"delete empty":        {handleRegionsBatchDelete, `{"account":"1","region_ids":[]}`},
		"delete missing":      {handleRegionsBatchDelete, `{"account":"1"}`},
		"delete over cap": {handleRegionsBatchDelete, func() string {
			raw, _ := json.Marshal(map[string]any{"account": "1", "region_ids": batchRegionIDs(maxRegionBatch + 1)})
			return string(raw)
		}()},
		"delete bad id":    {handleRegionsBatchDelete, `{"account":"1","region_ids":["a","../b"]}`},
		"delete empty id":  {handleRegionsBatchDelete, `{"account":"1","region_ids":["a",""]}`},
		"delete duplicate": {handleRegionsBatchDelete, `{"account":"1","region_ids":["a","b","a"]}`},
		"delete foreign":   {handleRegionsBatchDelete, `{"account":"01","region_ids":["a"]}`},
		"single bad id":    {handleRegionsDelete, `{"account":"1","region_id":"a/b"}`},
		"single no id":     {handleRegionsDelete, `{"account":"1","region_id":""}`},
		"single bad acct":  {handleRegionsDelete, `{"account":"x","region_id":"a"}`},
	} {
		h := newHarness(t)
		if _, err := h.call(t, c.handler, c.args); !isInvalidRequest(err) {
			t.Errorf("%s = %v, want invalid request", name, err)
		}
		h.untouched(t)
	}
	// An account outside the bound targets is refused without naming it.
	h := newHarness(t, "accounts/1000001")
	for _, handler := range []capability.Handler{handleRegionsDelete, handleRegionsBatchCreate, handleRegionsBatchUpdate,
		handleRegionsBatchDelete} {
		_, err := h.call(t, handler, `{"account":"2000002","region_id":"a","region_ids":["a"],"regions":[`+postalEntry+`]}`)
		if !isInvalidRequest(err) || strings.Contains(err.Error(), "2000002") {
			t.Errorf("foreign account = %v", err)
		}
	}
	h.untouched(t)
}

func TestRegionBatchRefusalNamesTheEntryButNotItsContent(t *testing.T) {
	h := newHarness(t)
	_, err := h.call(t, handleRegionsBatchCreate, `{"account":"1","regions":[`+postalEntry+
		`,{"region_id":"secret-id-x","display_name":"`+strings.Repeat("y", maxRegionNameRunes+1)+`","radius_area":`+validRadius()+`}]}`)
	if !isInvalidRequest(err) || !strings.Contains(err.Error(), "regions[1]") || strings.Contains(err.Error(), "secret-id-x") {
		t.Errorf("error = %v", err)
	}
	h.untouched(t)
}

func TestRegionBatchOfASubAccountIsProvenFirst(t *testing.T) {
	calls := map[string]struct {
		handler capability.Handler
		args    string
	}{
		"delete":       {handleRegionsDelete, `{"account":"20","region_id":"a"}`},
		"batch create": {handleRegionsBatchCreate, `{"account":"20","regions":[` + postalEntry + `]}`},
		"batch update": {handleRegionsBatchUpdate, `{"account":"20","regions":[` + updateEntryA + `]}`},
		"batch delete": {handleRegionsBatchDelete, `{"account":"20","region_ids":["a"]}`},
	}
	for name, c := range calls {
		route := func(subs string) func(regionWire) (int, string) {
			return func(r regionWire) (int, string) {
				if strings.HasSuffix(r.path, ":listSubaccounts") {
					return 200, `{"accounts":[` + subs + `]}`
				}
				return 200, `{}`
			}
		}
		h := newHarness(t, "advanced-accounts/10")
		seen := h.regionAnswers(t, route(accountRow("20", "Twenty")))
		if _, err := h.call(t, c.handler, c.args); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(*seen) != 2 || !strings.HasSuffix((*seen)[0].path, ":listSubaccounts") ||
			!strings.Contains((*seen)[1].path, "/accounts/20/regions") {
			t.Errorf("%s requests = %+v", name, *seen)
		}
		h = newHarness(t, "advanced-accounts/10")
		seen = h.regionAnswers(t, route(accountRow("30", "Thirty")))
		if _, err := h.call(t, c.handler, c.args); !isInvalidRequest(err) {
			t.Errorf("%s of a foreign account = %v", name, err)
		}
		if len(*seen) != 1 || !strings.HasSuffix((*seen)[0].path, ":listSubaccounts") {
			t.Errorf("%s sent more than the proof read: %+v", name, *seen)
		}
	}
}

func TestRegionBatchAndDeleteAreSentOnceAndReportAnUnknownOutcome(t *testing.T) {
	const unknown = "outcome of the change is unknown"
	calls := map[string]struct {
		handler capability.Handler
		args    string
	}{
		"delete":       {handleRegionsDelete, `{"account":"1000001","region_id":"a"}`},
		"batch create": {handleRegionsBatchCreate, `{"account":"1000001","regions":[` + postalEntry + `]}`},
		"batch update": {handleRegionsBatchUpdate, `{"account":"1000001","regions":[` + updateEntryA + `]}`},
		"batch delete": {handleRegionsBatchDelete, `{"account":"1000001","region_ids":["a","b"]}`},
	}
	for name, c := range map[string]struct {
		status  int
		body    string
		class   provider.Class
		unknown bool
	}{
		"server error": {500, `{"error":{"status":"INTERNAL"}}`, provider.ClassProviderError, true},
		"quota":        {429, quotaBody(reasonMinuteQuota), provider.ClassRateLimited, false},
		"admin":        {403, googleErrorBody, provider.ClassPermission, false},
		"missing":      {404, googleErrorBody, provider.ClassNotFound, false},
		"expired":      {401, `{"error":{"status":"UNAUTHENTICATED"}}`, provider.ClassAuth, false},
	} {
		for tool, call := range calls {
			h := newHarness(t)
			h.google.api = func(*http.Request) (int, string, http.Header) {
				return c.status, c.body, http.Header{"Content-Type": {"application/json"}}
			}
			_, err := h.call(t, call.handler, call.args)
			if err == nil {
				t.Fatalf("%s %s: no error", name, tool)
			}
			if got := len(h.google.seen()); got != 1 {
				t.Errorf("%s %s: %d requests, want one", name, tool, got)
			}
			if classOf(err) != c.class || strings.Contains(err.Error(), unknown) != c.unknown {
				t.Errorf("%s %s: class %q, error %q", name, tool, classOf(err), err)
			}
			if strings.Contains(err.Error(), "synthetic provider echo") {
				t.Errorf("%s %s: the message carries Google's text", name, tool)
			}
		}
	}
	// An answer that is not JSON after a batch create leaves the outcome open.
	h := newHarness(t)
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `<html>`, http.Header{"Content-Type": {"text/html"}}
	}
	_, err := h.call(t, handleRegionsBatchCreate, calls["batch create"].args)
	if classOf(err) != provider.ClassInvalidResponse || !strings.Contains(err.Error(), unknown) || len(h.google.seen()) != 1 {
		t.Errorf("not json = %v, %d requests", err, len(h.google.seen()))
	}
}

func TestRegionBatchAnswersAreBoundedAndUntrusted(t *testing.T) {
	h := newHarness(t)
	h.regionAnswers(t, always(200, `{"regions":["text",{"name":"accounts/1/regions/a","displayName":"`+
		strings.Repeat("n", 500)+`","extra":"x"},7]}`))
	out, err := h.call(t, handleRegionsBatchUpdate, `{"account":"1","regions":[`+updateEntryA+`]}`)
	if err != nil {
		t.Fatal(err)
	}
	list := out["regions"].([]any)
	if len(list) != 1 || len([]rune(list[0].(map[string]any)["displayName"].(string))) != maxNameRunes ||
		list[0].(map[string]any)["extra"] != nil {
		t.Errorf("regions = %v", list)
	}
	h = newHarness(t)
	h.regionAnswers(t, always(200, `{}`))
	out, err = h.call(t, handleRegionsBatchCreate, `{"account":"1","regions":[`+postalEntry+`]}`)
	if err != nil || out["regions"] == nil || len(out["regions"].([]any)) != 0 {
		t.Errorf("empty answer = %v, %v", out, err)
	}
}

func TestRegionBatchSummariesNameAccountCountAndIDs(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		args  string
		parts []string
	}{
		regionsDeleteID: {`{"account":"1000001","region_id":"north-1"}`,
			[]string{"1000001", `"north-1"`, "can be affected", "no way to undo"}},
		regionsBatchCreateID: {`{"account":"1000001","regions":[` + postalEntry + `,` + geoEntry + `]}`,
			[]string{"1000001", "2 regions", `"north-1"`, `"east-1"`, "one request", "all or none"}},
		regionsBatchUpdateID: {`{"account":"1000001","regions":[` + updateEntryA + `]}`,
			[]string{"1000001", "1 regions", `"north-1"`, "one request"}},
		regionsBatchDeleteID: {`{"account":"1000001","region_ids":["a","b","c"]}`,
			[]string{"1000001", "3 regions", `"a"`, `"c"`, "one request", "no way to undo"}},
	}
	for id, c := range cases {
		got := reg.ConfirmationSummary(id)(json.RawMessage(c.args))
		for _, part := range c.parts {
			if !strings.Contains(got, part) {
				t.Errorf("%s summary %q misses %q", id, got, part)
			}
		}
	}
	raw, _ := json.Marshal(map[string]any{"account": "1", "region_ids": batchRegionIDs(30)})
	if got := reg.ConfirmationSummary(regionsBatchDeleteID)(raw); !strings.Contains(got, "30 regions") ||
		!strings.Contains(got, "and 20 more") {
		t.Errorf("long summary = %q", got)
	}
}
