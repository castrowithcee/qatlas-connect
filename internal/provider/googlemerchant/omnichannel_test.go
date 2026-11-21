package googlemerchant

import (
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// omniRequest is one request the fake saw: method, path with query, and the JSON body.
type omniRequest struct{ method, path, body string }

func (h *harness) omniRequests(t *testing.T) []omniRequest {
	t.Helper()
	var out []omniRequest
	for _, req := range h.google.seen() {
		if req.URL.Host != "merchantapi.googleapis.com" || req.URL.Scheme != "https" {
			t.Errorf("request left the fixed origin: %s", req.URL.Host)
		}
		if req.Header.Get("Authorization") != "Bearer "+testAccess || strings.Contains(req.URL.String(), testAccess) {
			t.Errorf("token missing from the header or present in the URL of %s", req.URL.Path)
		}
		body := ""
		if req.Body != nil {
			b, _ := io.ReadAll(req.Body)
			body = string(b)
		}
		path := req.URL.Path
		if req.URL.RawQuery != "" {
			path += "?" + req.URL.RawQuery
		}
		out = append(out, omniRequest{req.Method, path, body})
	}
	return out
}

// The mail address of the verification contact is put together so no line holds one.
var testContactEmail = "verifier" + at + "shop.example.invalid"

const testOmnichannelAnswer = `{"name":"accounts/1000002/omnichannelSettings/CH","regionCode":"CH","lsfType":"MHLSF_FULL",` +
	`"inStock":{"uri":"https://shop.example.invalid/in-stock","state":"RUNNING"},` +
	`"pickup":{"uri":"https://shop.example.invalid/pickup","state":"ACTIVE"},` +
	`"lfpLink":{"lfpProvider":"lfpProviders/77","externalAccountId":"ext-1","state":"ACTIVE"},` +
	`"odo":{"uri":"https://shop.example.invalid/odo","state":"FAILED"},` +
	`"about":{"uri":"https://shop.example.invalid/about","state":"ACTION_REQUIRED"},` +
	`"inventoryVerification":{"state":"INACTIVE","contact":"Example Contact","contactEmail":"` + "verifier" + at +
	`shop.example.invalid","contactState":"RUNNING"},"unknownMember":"dropped"}`

func TestOmnichannelDescriptorsCarryRiskAndToolListRequirement(t *testing.T) {
	for _, c := range []struct {
		d           capability.Descriptor
		effect      capability.Effect
		idempotency capability.Idempotency
		listed      bool
	}{
		{omnichannelSettingsList, capability.EffectRead, capability.IdempotencySafe, false},
		{omnichannelSettingsGet, capability.EffectRead, capability.IdempotencySafe, false},
		{omnichannelSettingsCreate, capability.EffectCreate, capability.IdempotencyNonIdempotent, true},
		{omnichannelSettingsUpdate, capability.EffectUpdate, capability.IdempotencyIdempotent, true},
		{omnichannelVerify, capability.EffectExecute, capability.IdempotencyNonIdempotent, true},
	} {
		r := c.d.Risk
		changes := c.effect != capability.EffectRead
		if c.d.Version != 1 || r.Effect != c.effect || r.Idempotency != c.idempotency || !r.OpenWorld ||
			(r.Confirmation == capability.ConfirmationRequired) != changes || c.d.RequiresToolAllowList != c.listed {
			t.Errorf("%s: version %d, risk %+v, tools list %t", c.d.ID, c.d.Version, r, c.d.RequiresToolAllowList)
		}
		if changes && !strings.Contains(c.d.Description, "never repeated") {
			t.Errorf("%s does not say the call is never repeated", c.d.ID)
		}
		if changes && !strings.Contains(c.d.Description, "a couple of hours") {
			t.Errorf("%s does not name the delayed processing", c.d.ID)
		}
		if len(c.d.OutputSchema) == 0 || len(c.d.InputSchema) == 0 {
			t.Errorf("%s has no schema", c.d.ID)
		}
	}
	// Only the documented, writable members are arguments: no output-only member, no mask, no resource name.
	for _, d := range []capability.Descriptor{omnichannelSettingsCreate, omnichannelSettingsUpdate} {
		for _, name := range []string{"state", "lfp_link", "lfpLink", "update_mask", "updateMask", "name", "contactState",
			"contact_state", "regionCode", "lsfType"} {
			if strings.Contains(string(d.InputSchema), `"`+name+`"`) {
				t.Errorf("%s takes %s", d.ID, name)
			}
		}
		for _, name := range []string{"region_code", "lsf_type", "in_stock", "pickup", "odo", "about", "inventory_verification"} {
			if !strings.Contains(string(d.InputSchema), `"`+name+`"`) {
				t.Errorf("%s does not take %s", d.ID, name)
			}
		}
	}
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	tools := metadata.Profiles[0].Tools
	if !slices.Contains(tools, omnichannelSettingsListID) || !slices.Contains(tools, omnichannelSettingsGetID) ||
		slices.Contains(tools, omnichannelVerifyID) || slices.Contains(tools, omnichannelSettingsCreateID) || slices.Contains(tools, omnichannelSettingsUpdateID) {
		t.Errorf("profile tools = %v", tools)
	}
	if got, _ := metadata.Target.Reach(omnichannelSettingsUpdateID, []string{"accounts/2"}); !got {
		t.Error("an account target is refused the update tool")
	}
}

func TestOmnichannelToolsSettleTheAccountBeforeSecretAndRequest(t *testing.T) {
	for _, tool := range []struct {
		name    string
		handler capability.Handler
		extra   string
	}{
		{"list", handleOmnichannelList, ``},
		{"get", handleOmnichannelGet, `,"region_code":"CH"`},
		{"create", handleOmnichannelCreate, `,"region_code":"CH","lsf_type":"GHLSF"`},
		{"update", handleOmnichannelUpdate, `,"region_code":"CH","lsf_type":"GHLSF"`},
		{"verify", handleOmnichannelVerify, `,"region_code":"CH"`},
	} {
		for name, c := range map[string]struct {
			targets []string
			account string
		}{
			"outside the accounts":       {[]string{"accounts/1"}, `"account":"2"`},
			"outside a bound advanced":   {[]string{"accounts/1"}, `"account":"9"`},
			"not an ID":                  {nil, `"account":"1/users"`},
			"leading zero":               {nil, `"account":"01"`},
			"two targets, no argument":   {[]string{"accounts/1", "accounts/2"}, ``},
			"no target and no argument":  {nil, ``},
			"advanced target, other one": {[]string{"advanced-accounts/1", "accounts/2"}, `"account":"3x"`},
		} {
			t.Run(tool.name+" "+name, func(t *testing.T) {
				h := newHarness(t, c.targets...)
				args := `{` + c.account + tool.extra + `}`
				if c.account == "" {
					args = `{` + strings.TrimPrefix(tool.extra, ",") + `}`
				}
				if _, err := tool.handler(t.Context(), h.resolved, h.secrets, h.red, json.RawMessage(args)); !isInvalidRequest(err) {
					t.Errorf("err = %v, want invalid request", err)
				}
				h.untouched(t)
			})
		}
	}
}

func TestOmnichannelWritesNameTheirAccountExplicitly(t *testing.T) {
	// One target would be enough for a read, but a change must say which account it changes.
	for name, c := range map[string]struct {
		handler capability.Handler
		args    string
	}{
		"create": {handleOmnichannelCreate, `{"region_code":"CH","lsf_type":"GHLSF"}`},
		"update": {handleOmnichannelUpdate, `{"region_code":"CH","lsf_type":"GHLSF"}`},
		"verify": {handleOmnichannelVerify, `{"region_code":"CH"}`},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, "accounts/1")
			if _, err := h.call(t, c.handler, c.args); !isInvalidRequest(err) {
				t.Errorf("err = %v, want invalid request", err)
			}
			h.untouched(t)
		})
	}
}

func TestOmnichannelAddressesMustBePlainHTTPSBeforeSecretAndRequest(t *testing.T) {
	bad := []string{
		"http://shop.example.invalid/x",
		"https://user" + at + "shop.example.invalid/x",
		"https://shop.example.invalid\\@other.example.invalid/",
		"https://shop.example.invalid/ x",
		"javascript:alert(1)",
		"//shop.example.invalid/",
		"shop.example.invalid",
		"https:///x",
		"https://shop.example.invalid/" + strings.Repeat("a", 2048),
	}
	members := map[string]func(uri string) string{
		"in_stock": func(u string) string { return `"in_stock":{"uri":` + quote(u) + `}` },
		"pickup":   func(u string) string { return `"pickup":{"uri":` + quote(u) + `}` },
		"odo":      func(u string) string { return `"odo":{"uri":` + quote(u) + `}` },
		"about":    func(u string) string { return `"about":{"uri":` + quote(u) + `}` },
	}
	for member, build := range members {
		for _, uri := range bad {
			for tool, c := range map[string]struct {
				handler capability.Handler
				head    string
			}{
				"create": {handleOmnichannelCreate, `{"account":"1","region_code":"CH","lsf_type":"MHLSF_BASIC",`},
				"update": {handleOmnichannelUpdate, `{"account":"1","region_code":"CH",`},
			} {
				h := newHarness(t, "accounts/1")
				if _, err := h.call(t, c.handler, c.head+build(uri)+`}`); !isInvalidRequest(err) {
					t.Errorf("%s %s %.40q: err = %v, want invalid request", tool, member, uri, err)
				}
				h.untouched(t)
			}
		}
	}
	// A hosted storefront takes no in-stock address, and an address that is empty for it is no address.
	h := newHarness(t, "accounts/1")
	_, err := h.call(t, handleOmnichannelCreate, `{"account":"1","region_code":"CH","lsf_type":"GHLSF",`+
		`"in_stock":{"uri":"https://shop.example.invalid/in-stock"}}`)
	if !isInvalidRequest(err) {
		t.Errorf("GHLSF with an in-stock address: err = %v", err)
	}
	if _, err := h.call(t, handleOmnichannelUpdate, `{"account":"1","region_code":"CH","pickup":{"uri":""}}`); !isInvalidRequest(err) {
		t.Errorf("an empty pickup address: err = %v", err)
	}
	h.untouched(t)
}

func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestOmnichannelArgumentsAreCheckedBeforeSecretAndRequest(t *testing.T) {
	for name, c := range map[string]struct {
		handler capability.Handler
		args    string
	}{
		"get: lower-case region":     {handleOmnichannelGet, `{"account":"1","region_code":"ch"}`},
		"get: path in region":        {handleOmnichannelGet, `{"account":"1","region_code":"CH/x"}`},
		"verify: lower-case region":  {handleOmnichannelVerify, `{"account":"1","region_code":"ch"}`},
		"verify: path in region":     {handleOmnichannelVerify, `{"account":"1","region_code":"CH/x"}`},
		"verify: no region":          {handleOmnichannelVerify, `{"account":"1"}`},
		"get: no region":             {handleOmnichannelGet, `{"account":"1"}`},
		"get: three letters":         {handleOmnichannelGet, `{"account":"1","region_code":"CHE"}`},
		"create: no region":          {handleOmnichannelCreate, `{"account":"1","lsf_type":"GHLSF"}`},
		"create: no type":            {handleOmnichannelCreate, `{"account":"1","region_code":"CH"}`},
		"create: unknown type":       {handleOmnichannelCreate, `{"account":"1","region_code":"CH","lsf_type":"LSF_TYPE_UNSPECIFIED"}`},
		"create: bad region":         {handleOmnichannelCreate, `{"account":"1","region_code":"c","lsf_type":"GHLSF"}`},
		"update: no member":          {handleOmnichannelUpdate, `{"account":"1","region_code":"CH"}`},
		"update: no region":          {handleOmnichannelUpdate, `{"account":"1","lsf_type":"GHLSF"}`},
		"update: unknown type":       {handleOmnichannelUpdate, `{"account":"1","region_code":"CH","lsf_type":"ghlsf"}`},
		"update: blank contact":      {handleOmnichannelUpdate, `{"account":"1","region_code":"CH","inventory_verification":{"contact":" ","contact_email":"` + testContactEmail + `"}}`},
		"update: control in contact": {handleOmnichannelUpdate, `{"account":"1","region_code":"CH","inventory_verification":{"contact":"A\nB","contact_email":"` + testContactEmail + `"}}`},
		"update: no mail address":    {handleOmnichannelUpdate, `{"account":"1","region_code":"CH","inventory_verification":{"contact":"Example Contact","contact_email":"verifier"}}`},
		"update: two mail addresses": {handleOmnichannelUpdate, `{"account":"1","region_code":"CH","inventory_verification":{"contact":"Example Contact","contact_email":"` + testContactEmail + "," + testContactEmail + `"}}`},
		"update: space in the mail":  {handleOmnichannelUpdate, `{"account":"1","region_code":"CH","inventory_verification":{"contact":"Example Contact","contact_email":"a b` + at + `shop.example.invalid"}}`},
		"update: arguments not JSON": {handleOmnichannelUpdate, `{"account":"1","region_code":"CH","about":"https://shop.example.invalid/"}`},
		"update: long contact":       {handleOmnichannelUpdate, `{"account":"1","region_code":"CH","inventory_verification":{"contact":"` + strings.Repeat("a", 301) + `","contact_email":"` + testContactEmail + `"}}`},
		"update: long mail":          {handleOmnichannelUpdate, `{"account":"1","region_code":"CH","inventory_verification":{"contact":"Example Contact","contact_email":"` + strings.Repeat("a", 250) + at + `shop.example.invalid"}}`},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, "accounts/1")
			if _, err := h.call(t, c.handler, c.args); err == nil {
				t.Error("no error")
			}
			h.untouched(t)
		})
	}
}

func TestOmnichannelListSendsTheDocumentedParametersAndShowsEveryMember(t *testing.T) {
	h := newHarness(t)
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		if req.URL.Query().Get("pageToken") == "page-two" {
			return 200, `{"omnichannelSettings":[{"name":"accounts/1/omnichannelSettings/DE","regionCode":"DE","lsfType":"GHLSF",` +
				`"inStock":{"state":"ACTION_REQUIRED"}}]}`, nil
		}
		return 200, `{"omnichannelSettings":[` + testOmnichannelAnswer + `],"nextPageToken":"page-two"}`, nil
	}
	first, err := h.call(t, handleOmnichannelList, `{"account":"1","page_size":1000}`)
	if err != nil {
		t.Fatal(err)
	}
	rows := first["omnichannelSettings"].([]any)
	row := rows[0].(map[string]any)
	if len(rows) != 1 || row["regionCode"] != "CH" || row["lsfType"] != "MHLSF_FULL" || row["unknownMember"] != nil {
		t.Fatalf("first = %v", first)
	}
	for key, want := range map[string]string{"inStock": "RUNNING", "pickup": "ACTIVE", "odo": "FAILED", "about": "ACTION_REQUIRED"} {
		member := row[key].(map[string]any)
		if member["state"] != want || !strings.HasPrefix(member["uri"].(string), "https://shop.example.invalid/") {
			t.Errorf("%s = %v", key, member)
		}
	}
	link := row["lfpLink"].(map[string]any)
	verification := row["inventoryVerification"].(map[string]any)
	if link["lfpProvider"] != "lfpProviders/77" || link["externalAccountId"] != "ext-1" || link["state"] != "ACTIVE" ||
		verification["state"] != "INACTIVE" || verification["contact"] != "Example Contact" ||
		verification["contactEmail"] != testContactEmail || verification["contactState"] != "RUNNING" {
		t.Errorf("lfpLink = %v, inventoryVerification = %v", link, verification)
	}
	cursor, _ := first["next_cursor"].(string)
	if cursor == "" || cursor == "page-two" {
		t.Fatalf("next_cursor = %q", cursor)
	}
	second, err := h.call(t, handleOmnichannelList, `{"account":"1","page_size":1000,"cursor":"`+cursor+`"}`)
	if err != nil || len(second["omnichannelSettings"].([]any)) != 1 || second["next_cursor"] != nil {
		t.Fatalf("second = %v, %v", second, err)
	}
	if _, err := h.call(t, handleOmnichannelList, `{"account":"1","page_size":10,"cursor":"`+cursor+`"}`); !isInvalidRequest(err) {
		t.Errorf("a cursor of other arguments: err = %v", err)
	}
	got := h.omniRequests(t)
	if len(got) != 2 || got[0] != (omniRequest{"GET", "/accounts/v1/accounts/1/omnichannelSettings?pageSize=1000", ""}) ||
		got[1] != (omniRequest{"GET", "/accounts/v1/accounts/1/omnichannelSettings?pageSize=1000&pageToken=page-two", ""}) {
		t.Errorf("requests = %v", got)
	}
	// An empty answer is an empty list, and an account without argument is the only bound one.
	h2 := newHarness(t, "accounts/7")
	empty, err := h2.call(t, handleOmnichannelList, `{}`)
	if rows, ok := empty["omnichannelSettings"].([]any); err != nil || !ok || len(rows) != 0 {
		t.Errorf("empty = %v, %v", empty, err)
	}
	if got := h2.omniRequests(t); len(got) != 1 || got[0].path != "/accounts/v1/accounts/7/omnichannelSettings" {
		t.Errorf("requests = %v", got)
	}
}

func TestOmnichannelListRefusesTooManyEntriesAndALongToken(t *testing.T) {
	h := newHarness(t, "accounts/1")
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"omnichannelSettings":[],"nextPageToken":"` + strings.Repeat("x", maxPageToken+1) + `"}`, nil
	}
	if _, err := h.call(t, handleOmnichannelList, `{"account":"1"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("err = %v", err)
	}
}

func TestOmnichannelGetReadsOneCountry(t *testing.T) {
	h := newHarness(t)
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, testOmnichannelAnswer, nil }
	out, err := h.call(t, handleOmnichannelGet, `{"account":"1000002","region_code":"CH"}`)
	if err != nil || out["name"] != "accounts/1000002/omnichannelSettings/CH" || out["regionCode"] != "CH" {
		t.Fatalf("out = %v, %v", out, err)
	}
	if got := h.omniRequests(t); len(got) != 1 || got[0] != (omniRequest{"GET", "/accounts/v1/accounts/1000002/omnichannelSettings/CH", ""}) {
		t.Errorf("requests = %v", got)
	}
}

func TestOmnichannelReadsProveASubAccountOfABoundAdvancedAccount(t *testing.T) {
	proof := func(req *http.Request) (int, string, http.Header) {
		if strings.HasSuffix(req.URL.Path, ":listSubaccounts") {
			return 200, `{"accounts":[` + accountRow("2", "Two") + `]}`, nil
		}
		return 200, testOmnichannelAnswer, nil
	}
	h := newHarness(t, "advanced-accounts/1")
	h.google.api = proof
	if _, err := h.call(t, handleOmnichannelGet, `{"account":"2","region_code":"CH"}`); err != nil {
		t.Fatal(err)
	}
	if got := h.omniRequests(t); len(got) != 2 || !strings.HasSuffix(got[0].path, "/accounts/1:listSubaccounts?pageSize=500") ||
		got[1].path != "/accounts/v1/accounts/2/omnichannelSettings/CH" {
		t.Errorf("requests = %v", got)
	}
	// A proven account is changed, an unproven one is refused after the proof read and never changed.
	h = newHarness(t, "advanced-accounts/1")
	h.google.api = proof
	if _, err := h.call(t, handleOmnichannelUpdate, `{"account":"2","region_code":"CH","lsf_type":"GHLSF"}`); err != nil {
		t.Fatal(err)
	}
	h = newHarness(t, "advanced-accounts/1")
	h.google.api = proof
	if _, err := h.call(t, handleOmnichannelUpdate, `{"account":"3","region_code":"CH","lsf_type":"GHLSF"}`); !isInvalidRequest(err) {
		t.Errorf("err = %v", err)
	}
	for _, r := range h.omniRequests(t) {
		if r.method != "GET" {
			t.Errorf("request %v", r)
		}
	}
}

func TestOmnichannelCreateSendsOnlyThePassedMembers(t *testing.T) {
	h := newHarness(t, "accounts/1000002")
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, testOmnichannelAnswer, nil }
	out, err := h.call(t, handleOmnichannelCreate, `{"account":"1000002","region_code":"CH","lsf_type":"MHLSF_FULL",`+
		`"in_stock":{"uri":"https://shop.example.invalid/in-stock"},"pickup":{"uri":"https://shop.example.invalid/pickup"},`+
		`"odo":{"uri":"https://shop.example.invalid/odo"},"about":{"uri":"https://shop.example.invalid/about"},`+
		`"inventory_verification":{"contact":"Example Contact","contact_email":"`+testContactEmail+`"}}`)
	if err != nil || out["regionCode"] != "CH" {
		t.Fatalf("out = %v, %v", out, err)
	}
	got := h.omniRequests(t)
	if len(got) != 1 || got[0].method != "POST" || got[0].path != "/accounts/v1/accounts/1000002/omnichannelSettings" {
		t.Fatalf("requests = %v", got)
	}
	want := `{"regionCode":"CH","lsfType":"MHLSF_FULL","inStock":{"uri":"https://shop.example.invalid/in-stock"},` +
		`"pickup":{"uri":"https://shop.example.invalid/pickup"},"odo":{"uri":"https://shop.example.invalid/odo"},` +
		`"about":{"uri":"https://shop.example.invalid/about"},"inventoryVerification":{"contact":"Example Contact",` +
		`"contactEmail":"` + testContactEmail + `"}}`
	if got[0].body != want {
		t.Errorf("body = %s\nwant   %s", got[0].body, want)
	}
	// A hosted storefront takes an empty in-stock object.
	h = newHarness(t, "accounts/1000002")
	if _, err := h.call(t, handleOmnichannelCreate, `{"account":"1000002","region_code":"DE","lsf_type":"GHLSF","in_stock":{}}`); err != nil {
		t.Fatal(err)
	}
	if got := h.omniRequests(t); got[0].body != `{"regionCode":"DE","lsfType":"GHLSF","inStock":{}}` {
		t.Errorf("body = %s", got[0].body)
	}
}

func TestOmnichannelUpdateMasksOnlyThePassedMembersInSnakeCase(t *testing.T) {
	for name, c := range map[string]struct{ args, mask, body string }{
		"type":       {`"lsf_type":"MHLSF_BASIC"`, "lsf_type", `{"lsfType":"MHLSF_BASIC"}`},
		"in stock":   {`"in_stock":{"uri":"https://shop.example.invalid/in-stock"}`, "in_stock", `{"inStock":{"uri":"https://shop.example.invalid/in-stock"}}`},
		"empty":      {`"lsf_type":"GHLSF","in_stock":{}`, "lsf_type,in_stock", `{"lsfType":"GHLSF","inStock":{}}`},
		"pickup":     {`"pickup":{"uri":"https://shop.example.invalid/pickup"}`, "pickup", `{"pickup":{"uri":"https://shop.example.invalid/pickup"}}`},
		"odo":        {`"odo":{"uri":"https://shop.example.invalid/odo"}`, "odo", `{"odo":{"uri":"https://shop.example.invalid/odo"}}`},
		"about":      {`"about":{"uri":"https://shop.example.invalid/about"}`, "about", `{"about":{"uri":"https://shop.example.invalid/about"}}`},
		"contact":    {`"inventory_verification":{"contact":"Example Contact","contact_email":"` + testContactEmail + `"}`, "inventory_verification", `{"inventoryVerification":{"contact":"Example Contact","contactEmail":"` + testContactEmail + `"}}`},
		"everything": {`"lsf_type":"MHLSF_FULL","in_stock":{"uri":"https://shop.example.invalid/a"},"pickup":{"uri":"https://shop.example.invalid/b"},"odo":{"uri":"https://shop.example.invalid/c"},"about":{"uri":"https://shop.example.invalid/d"},"inventory_verification":{"contact":"X","contact_email":"` + testContactEmail + `"}`, "lsf_type,in_stock,pickup,odo,about,inventory_verification", `{"lsfType":"MHLSF_FULL","inStock":{"uri":"https://shop.example.invalid/a"},"pickup":{"uri":"https://shop.example.invalid/b"},"odo":{"uri":"https://shop.example.invalid/c"},"about":{"uri":"https://shop.example.invalid/d"},"inventoryVerification":{"contact":"X","contactEmail":"` + testContactEmail + `"}}`},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, "accounts/1000002")
			h.google.api = func(*http.Request) (int, string, http.Header) { return 200, testOmnichannelAnswer, nil }
			if _, err := h.call(t, handleOmnichannelUpdate, `{"account":"1000002","region_code":"CH",`+c.args+`}`); err != nil {
				t.Fatal(err)
			}
			got := h.omniRequests(t)
			wantPath := "/accounts/v1/accounts/1000002/omnichannelSettings/CH?updateMask=" + strings.ReplaceAll(c.mask, ",", "%2C")
			if len(got) != 1 || got[0].method != "PATCH" || got[0].path != wantPath || got[0].body != c.body {
				t.Errorf("requests = %v\nwant %s %s", got, wantPath, c.body)
			}
		})
	}
}

func TestOmnichannelChangeIsSentOnceAndAnUnclearOutcomeIsSaid(t *testing.T) {
	for name, c := range map[string]struct {
		status      int
		unclearText bool
		class       provider.Class
	}{
		"server error":  {500, true, provider.ClassProviderError},
		"unauthorized":  {401, false, provider.ClassAuth},
		"rate limited":  {429, false, provider.ClassRateLimited},
		"bad request":   {400, false, ""},
		"unavailable":   {503, true, provider.ClassUnreachable},
		"gateway error": {502, true, provider.ClassProviderError},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, "accounts/1")
			h.google.api = func(*http.Request) (int, string, http.Header) {
				return c.status, `{"error":{"code":` + "0" + `,"message":"untrusted text","status":"X"}}`, nil
			}
			_, err := h.call(t, handleOmnichannelUpdate, `{"account":"1","region_code":"CH","lsf_type":"GHLSF"}`)
			if err == nil {
				t.Fatal("no error")
			}
			if c.class != "" && classOf(err) != c.class {
				t.Errorf("class = %s, want %s", classOf(err), c.class)
			}
			if unclear := strings.Contains(err.Error(), "outcome of the change is unknown"); unclear != c.unclearText {
				t.Errorf("unclear outcome said = %t: %v", unclear, err)
			}
			if strings.Contains(err.Error(), "untrusted text") {
				t.Errorf("the message carries Google's text: %v", err)
			}
			if got := h.omniRequests(t); len(got) != 1 {
				t.Errorf("%d requests, want exactly one", len(got))
			}
		})
	}
}

func TestOmnichannelConfirmationSummariesNameTheChange(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	summaries := omnichannelSummaries()
	if len(summaries) != 3 || summaries[omnichannelSettingsListID] != nil || summaries[omnichannelSettingsGetID] != nil {
		t.Fatalf("summaries = %v", summaries)
	}
	create := summaries[omnichannelSettingsCreateID](json.RawMessage(`{"account":"1","region_code":"CH","lsf_type":"MHLSF_BASIC",` +
		`"in_stock":{"uri":"https://shop.example.invalid/in-stock"}}`))
	if !strings.Contains(create, `"CH"`) || !strings.Contains(create, `"MHLSF_BASIC"`) || !strings.Contains(create, "account 1") ||
		!strings.Contains(create, `in_stock "https://shop.example.invalid/in-stock"`) {
		t.Errorf("create summary = %q", create)
	}
	update := summaries[omnichannelSettingsUpdateID](json.RawMessage(`{"account":"1","region_code":"CH","lsf_type":"GHLSF","in_stock":{}}`))
	if !strings.Contains(update, "set lsf_type, in_stock") || strings.Contains(update, "addresses") {
		t.Errorf("update summary = %q", update)
	}
	if summaries[omnichannelSettingsUpdateID](json.RawMessage(`[`)) != "" || summaries[omnichannelSettingsCreateID](json.RawMessage(`{}`)) != "" {
		t.Error("a summary of unreadable arguments is not empty")
	}
}

func TestOmnichannelVerifyPostsAnEmptyBodyOnceAndShowsTheSetting(t *testing.T) {
	h := newHarness(t, "accounts/1000002")
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"omnichannelSetting":` + testOmnichannelAnswer + `}`, nil
	}
	out, err := h.call(t, handleOmnichannelVerify, `{"account":"1000002","region_code":"CH"}`)
	if err != nil || out["regionCode"] != "CH" || out["unknownMember"] != nil {
		t.Fatalf("out = %v, %v", out, err)
	}
	verification := out["inventoryVerification"].(map[string]any)
	if verification["state"] != "INACTIVE" || verification["contactEmail"] != testContactEmail ||
		verification["contactState"] != "RUNNING" {
		t.Errorf("inventoryVerification = %v", verification)
	}
	want := omniRequest{"POST", "/accounts/v1/accounts/1000002/omnichannelSettings/CH:requestInventoryVerification", "{}"}
	if got := h.omniRequests(t); len(got) != 1 || got[0] != want {
		t.Errorf("requests = %v, want %v", got, want)
	}
	summary := omnichannelSummaries()[omnichannelVerifyID](json.RawMessage(`{"account":"1","region_code":"CH"}`))
	if !strings.Contains(summary, "verification process at Google") || !strings.Contains(summary, `"CH"`) ||
		!strings.Contains(summary, "contact") {
		t.Errorf("summary = %q", summary)
	}
}

func TestOmnichannelVerifyIsSentOnceAndMapsGoogleAndQuotaErrors(t *testing.T) {
	for name, c := range map[string]struct {
		status  int
		unclear bool
		class   provider.Class
	}{
		"server error": {500, true, provider.ClassProviderError},
		"unauthorized": {401, false, provider.ClassAuth},
		"quota":        {429, false, provider.ClassRateLimited},
		"bad request":  {400, false, ""},
		"not found":    {404, false, ""},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, "accounts/1")
			h.google.api = func(*http.Request) (int, string, http.Header) {
				return c.status, `{"error":{"code":0,"message":"untrusted text","status":"X"}}`, nil
			}
			_, err := h.call(t, handleOmnichannelVerify, `{"account":"1","region_code":"CH"}`)
			if err == nil {
				t.Fatal("no error")
			}
			if c.class != "" && classOf(err) != c.class {
				t.Errorf("class = %s, want %s", classOf(err), c.class)
			}
			if unclear := strings.Contains(err.Error(), "outcome of the change is unknown"); unclear != c.unclear {
				t.Errorf("unclear outcome said = %t: %v", unclear, err)
			}
			if strings.Contains(err.Error(), "untrusted text") {
				t.Errorf("the message carries Google's text: %v", err)
			}
			if got := h.omniRequests(t); len(got) != 1 {
				t.Errorf("%d requests, want exactly one", len(got))
			}
		})
	}
}
