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

// returnPolicyRequest is one request the fake saw: method, path with query, and the JSON body.
type returnPolicyRequest struct{ method, path, body string }

func (h *harness) returnPolicyRequests(t *testing.T) []returnPolicyRequest {
	t.Helper()
	var out []returnPolicyRequest
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
		out = append(out, returnPolicyRequest{req.Method, path, body})
	}
	return out
}

func returnPolicyQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

const returnPolicyAnswer = `{"name":"accounts/1000002/onlineReturnPolicies/2000001","returnPolicyId":"2000001",` +
	`"label":"Example label","countries":["CH","DE"],` +
	`"policy":{"type":"NUMBER_OF_DAYS_AFTER_DELIVERY","days":"30"},` +
	`"seasonalOverrides":[{"label":"Winter","startDate":{"year":2026,"month":12,"day":1},` +
	`"endDate":{"year":2026,"month":12,"day":31},"returnUntilDate":{"year":2027,"month":1,"day":31}},` +
	`{"label":"Spring","startDate":{"year":2027,"month":3,"day":1},"endDate":{"year":2027,"month":3,"day":31},"returnDays":60}],` +
	`"restockingFee":{"microPercent":15000000},"returnMethods":["BY_MAIL","IN_STORE"],"itemConditions":["NEW","USED"],` +
	`"returnShippingFee":{"type":"FIXED","fixedFee":{"amountMicros":"5000000","currencyCode":"CHF"}},` +
	`"returnPolicyUri":"https://shop.example.invalid/returns","acceptDefectiveOnly":false,"processRefundDays":14,` +
	`"acceptExchange":true,"returnLabelSource":"DOWNLOAD_AND_PRINT","unknownMember":"dropped"}`

// returnPolicyFull sets every writable member.
const returnPolicyFull = `{"account":"1000002","label":"Example label","countries":["CH","DE"],` +
	`"policy":{"type":"NUMBER_OF_DAYS_AFTER_DELIVERY","days":30},` +
	`"seasonal_overrides":[{"label":"Winter","start_date":{"year":2026,"month":12,"day":1},` +
	`"end_date":{"year":2026,"month":12,"day":31},"return_until_date":{"year":2027,"month":1,"day":31}},` +
	`{"label":"Spring","start_date":{"year":2027,"month":3,"day":1},"end_date":{"year":2027,"month":3,"day":31},"return_days":60}],` +
	`"restocking_fee":{"micro_percent":15000000},"return_methods":["BY_MAIL","IN_STORE"],"item_conditions":["NEW","USED"],` +
	`"return_shipping_fee":{"type":"FIXED","fixed_fee":{"amount_micros":"5000000","currency_code":"CHF"}},` +
	`"return_policy_uri":"https://shop.example.invalid/returns","accept_defective_only":false,"process_refund_days":14,` +
	`"accept_exchange":true,"return_label_source":"DOWNLOAD_AND_PRINT"}`

const returnPolicyFullBody = `{"label":"Example label","countries":["CH","DE"],` +
	`"policy":{"type":"NUMBER_OF_DAYS_AFTER_DELIVERY","days":"30"},` +
	`"seasonalOverrides":[{"label":"Winter","startDate":{"year":2026,"month":12,"day":1},` +
	`"endDate":{"year":2026,"month":12,"day":31},"returnUntilDate":{"year":2027,"month":1,"day":31}},` +
	`{"label":"Spring","startDate":{"year":2027,"month":3,"day":1},"endDate":{"year":2027,"month":3,"day":31},"returnDays":60}],` +
	`"restockingFee":{"microPercent":15000000},"returnMethods":["BY_MAIL","IN_STORE"],"itemConditions":["NEW","USED"],` +
	`"returnShippingFee":{"type":"FIXED","fixedFee":{"amountMicros":"5000000","currencyCode":"CHF"}},` +
	`"returnPolicyUri":"https://shop.example.invalid/returns","acceptDefectiveOnly":false,"processRefundDays":14,` +
	`"acceptExchange":true,"returnLabelSource":"DOWNLOAD_AND_PRINT"}`

// returnPolicyMinimal is the smallest valid create.
const returnPolicyMinimal = `"account":"1","countries":["CH"],"policy":{"type":"NO_RETURNS"},` +
	`"return_policy_uri":"https://shop.example.invalid/returns"`

func TestReturnPolicyDescriptorsCarryRiskAndToolListRequirement(t *testing.T) {
	for _, c := range []struct {
		d           capability.Descriptor
		effect      capability.Effect
		idempotency capability.Idempotency
		listed      bool
	}{
		{onlineReturnPoliciesList, capability.EffectRead, capability.IdempotencySafe, false},
		{onlineReturnPoliciesGet, capability.EffectRead, capability.IdempotencySafe, false},
		{onlineReturnPoliciesCreate, capability.EffectCreate, capability.IdempotencyNonIdempotent, true},
		{onlineReturnPoliciesDelete, capability.EffectDelete, capability.IdempotencyUnknown, true},
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
		if changes && !strings.Contains(c.d.Description, "10 business days") {
			t.Errorf("%s does not name the delayed processing", c.d.ID)
		}
		if len(c.d.OutputSchema) == 0 || len(c.d.InputSchema) == 0 {
			t.Errorf("%s has no schema", c.d.ID)
		}
	}
	// Every writable member is an argument; no output-only member, no resource name, no free body.
	for _, name := range []string{"label", "countries", "policy", "seasonal_overrides", "restocking_fee", "return_methods",
		"item_conditions", "return_shipping_fee", "return_policy_uri", "accept_defective_only", "process_refund_days",
		"accept_exchange", "return_label_source"} {
		if !strings.Contains(string(onlineReturnPoliciesCreate.InputSchema), `"`+name+`"`) {
			t.Errorf("create does not take %s", name)
		}
		if !slices.ContainsFunc(onlineReturnPoliciesCreate.Arguments, func(a capability.Argument) bool { return a.Name == name }) {
			t.Errorf("create does not describe %s", name)
		}
	}
	for _, name := range []string{"name", "returnPolicyId", "returnPolicyUri", "body", "path", "update_mask"} {
		for _, d := range returnPolicyDescriptors() {
			if strings.Contains(string(d.InputSchema), `"`+name+`"`) {
				t.Errorf("%s takes %s", d.ID, name)
			}
		}
	}
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	tools := metadata.Profiles[0].Tools
	if !slices.Contains(tools, onlineReturnPoliciesListID) || !slices.Contains(tools, onlineReturnPoliciesGetID) ||
		slices.Contains(tools, onlineReturnPoliciesCreateID) || slices.Contains(tools, onlineReturnPoliciesDeleteID) {
		t.Errorf("profile tools = %v", tools)
	}
	if got, _ := metadata.Target.Reach(onlineReturnPoliciesDeleteID, []string{"accounts/2"}); !got {
		t.Error("an account target is refused the delete tool")
	}
}

func TestReturnPolicyToolsSettleTheAccountBeforeSecretAndRequest(t *testing.T) {
	for _, tool := range []struct {
		name    string
		handler capability.Handler
		extra   string
	}{
		{"list", handleReturnPoliciesList, ``},
		{"get", handleReturnPoliciesGet, `"return_policy_id":"1"`},
		{"create", handleReturnPoliciesCreate, returnPolicyMinimal[strings.Index(returnPolicyMinimal, `"countries"`):]},
		{"delete", handleReturnPoliciesDelete, `"return_policy_id":"1"`},
	} {
		for name, c := range map[string]struct {
			targets []string
			account string
		}{
			"outside the accounts":       {[]string{"accounts/1"}, `"account":"2"`},
			"not an ID":                  {nil, `"account":"1/users"`},
			"leading zero":               {nil, `"account":"01"`},
			"two targets, no argument":   {[]string{"accounts/1", "accounts/2"}, ``},
			"no target and no argument":  {nil, ``},
			"advanced target, other one": {[]string{"advanced-accounts/1", "accounts/2"}, `"account":"3x"`},
		} {
			t.Run(tool.name+" "+name, func(t *testing.T) {
				h := newHarness(t, c.targets...)
				parts := slices.DeleteFunc([]string{c.account, tool.extra}, func(s string) bool { return s == "" })
				args := `{` + strings.Join(parts, ",") + `}`
				if _, err := tool.handler(t.Context(), h.resolved, h.secrets, h.red, json.RawMessage(args)); !isInvalidRequest(err) {
					t.Errorf("err = %v, want invalid request", err)
				}
				h.untouched(t)
			})
		}
	}
}

func TestReturnPolicyWritesNameTheirAccountExplicitly(t *testing.T) {
	// One target would be enough for a read, but a change must say which account it changes.
	for name, c := range map[string]struct {
		handler capability.Handler
		args    string
	}{
		"create": {handleReturnPoliciesCreate, `{"countries":["CH"],"policy":{"type":"NO_RETURNS"},` +
			`"return_policy_uri":"https://shop.example.invalid/returns"}`},
		"delete": {handleReturnPoliciesDelete, `{"return_policy_id":"1"}`},
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

func TestReturnPolicyURIMustBePlainHTTPSBeforeSecretAndRequest(t *testing.T) {
	for _, uri := range []string{
		"",
		"http://shop.example.invalid/returns",
		"https://user" + at + "shop.example.invalid/returns",
		"https://user:word" + at + "shop.example.invalid/returns",
		"https://shop.example.invalid\\@other.example.invalid/",
		"https://shop.example.invalid/ returns",
		"https://shop.example.invalid/\nreturns",
		"javascript:alert(1)",
		"//shop.example.invalid/returns",
		"shop.example.invalid/returns",
		"https:///returns",
		"https://shop.example.invalid/" + strings.Repeat("a", 2048),
	} {
		h := newHarness(t, "accounts/1")
		args := `{"account":"1","countries":["CH"],"policy":{"type":"NO_RETURNS"},"return_policy_uri":` + returnPolicyQuote(uri) + `}`
		if _, err := h.call(t, handleReturnPoliciesCreate, args); !isInvalidRequest(err) {
			t.Errorf("%.40q: err = %v, want invalid request", uri, err)
		}
		h.untouched(t)
	}
	h := newHarness(t, "accounts/1")
	if _, err := h.call(t, handleReturnPoliciesCreate, `{"account":"1","countries":["CH"],"policy":{"type":"NO_RETURNS"}}`); !isInvalidRequest(err) {
		t.Errorf("no address: err = %v", err)
	}
	h.untouched(t)
}

func TestReturnPolicyArgumentsAreCheckedBeforeSecretAndRequest(t *testing.T) {
	const uri = `"return_policy_uri":"https://shop.example.invalid/returns"`
	season := func(extra string) string {
		return `"seasonal_overrides":[` + extra + `],"policy":{"type":"NO_RETURNS"},` + uri
	}
	const winter = `{"label":"Winter","start_date":{"year":2026,"month":12,"day":1},"end_date":{"year":2026,"month":12,"day":31}`
	for name, c := range map[string]struct {
		handler capability.Handler
		args    string
	}{
		"get: policy member":       {handleReturnPoliciesGet, `{"account":"1","return_policy_id":"1","label":"x"}`},
		"delete: policy member":    {handleReturnPoliciesDelete, `{"account":"1","return_policy_id":"1","accept_exchange":true}`},
		"get: no ID":               {handleReturnPoliciesGet, `{"account":"1"}`},
		"get: path in ID":          {handleReturnPoliciesGet, `{"account":"1","return_policy_id":"1/x"}`},
		"get: dots in ID":          {handleReturnPoliciesGet, `{"account":"1","return_policy_id":".."}`},
		"get: query in ID":         {handleReturnPoliciesGet, `{"account":"1","return_policy_id":"1?x=y"}`},
		"get: long ID":             {handleReturnPoliciesGet, `{"account":"1","return_policy_id":"` + strings.Repeat("1", 101) + `"}`},
		"delete: no ID":            {handleReturnPoliciesDelete, `{"account":"1"}`},
		"delete: escaped slash":    {handleReturnPoliciesDelete, `{"account":"1","return_policy_id":"1%2F2"}`},
		"delete: space in ID":      {handleReturnPoliciesDelete, `{"account":"1","return_policy_id":"1 2"}`},
		"list: page size":          {handleReturnPoliciesList, `{"account":"1","page_size":101}`},
		"list: negative page size": {handleReturnPoliciesList, `{"account":"1","page_size":-1}`},
		"create: no countries":     {handleReturnPoliciesCreate, `{"account":"1","policy":{"type":"NO_RETURNS"},` + uri + `}`},
		"create: empty countries":  {handleReturnPoliciesCreate, `{"account":"1","countries":[],"policy":{"type":"NO_RETURNS"},` + uri + `}`},
		"create: lower-case":       {handleReturnPoliciesCreate, `{"account":"1","countries":["ch"],"policy":{"type":"NO_RETURNS"},` + uri + `}`},
		"create: three letters":    {handleReturnPoliciesCreate, `{"account":"1","countries":["CHE"],"policy":{"type":"NO_RETURNS"},` + uri + `}`},
		"create: repeated country": {handleReturnPoliciesCreate, `{"account":"1","countries":["CH","CH"],"policy":{"type":"NO_RETURNS"},` + uri + `}`},
		"create: too many countries": {handleReturnPoliciesCreate, `{"account":"1","countries":[` +
			strings.TrimSuffix(strings.Repeat(`"CH",`, 251), ",") + `],"policy":{"type":"NO_RETURNS"},` + uri + `}`},
		"create: empty label":       {handleReturnPoliciesCreate, `{"account":"1","label":"","countries":["CH"],"policy":{"type":"NO_RETURNS"},` + uri + `}`},
		"create: long label":        {handleReturnPoliciesCreate, `{"account":"1","label":"` + strings.Repeat("a", 51) + `","countries":["CH"],"policy":{"type":"NO_RETURNS"},` + uri + `}`},
		"create: control in label":  {handleReturnPoliciesCreate, `{"account":"1","label":"a\nb","countries":["CH"],"policy":{"type":"NO_RETURNS"},` + uri + `}`},
		"create: unknown type":      {handleReturnPoliciesCreate, `{"account":"1","countries":["CH"],"policy":{"type":"TYPE_UNSPECIFIED"},` + uri + `}`},
		"create: days missing":      {handleReturnPoliciesCreate, `{"account":"1","countries":["CH"],"policy":{"type":"NUMBER_OF_DAYS_AFTER_DELIVERY"},"return_methods":["BY_MAIL"],"item_conditions":["NEW"],` + uri + `}`},
		"create: days zero":         {handleReturnPoliciesCreate, `{"account":"1","countries":["CH"],"policy":{"type":"NUMBER_OF_DAYS_AFTER_DELIVERY","days":0},"return_methods":["BY_MAIL"],"item_conditions":["NEW"],` + uri + `}`},
		"create: days too large":    {handleReturnPoliciesCreate, `{"account":"1","countries":["CH"],"policy":{"type":"NUMBER_OF_DAYS_AFTER_DELIVERY","days":3651},"return_methods":["BY_MAIL"],"item_conditions":["NEW"],` + uri + `}`},
		"create: days as text":      {handleReturnPoliciesCreate, `{"account":"1","countries":["CH"],"policy":{"type":"NUMBER_OF_DAYS_AFTER_DELIVERY","days":"30"},"return_methods":["BY_MAIL"],"item_conditions":["NEW"],` + uri + `}`},
		"create: days on lifetime":  {handleReturnPoliciesCreate, `{"account":"1","countries":["CH"],"policy":{"type":"LIFETIME_RETURNS","days":5},"return_methods":["BY_MAIL"],"item_conditions":["NEW"],` + uri + `}`},
		"create: no methods":        {handleReturnPoliciesCreate, `{"account":"1","countries":["CH"],"policy":{"type":"LIFETIME_RETURNS"},"item_conditions":["NEW"],` + uri + `}`},
		"create: no conditions":     {handleReturnPoliciesCreate, `{"account":"1","countries":["CH"],"policy":{"type":"LIFETIME_RETURNS"},"return_methods":["BY_MAIL"],` + uri + `}`},
		"create: no policy at all":  {handleReturnPoliciesCreate, `{"account":"1","countries":["CH"],` + uri + `}`},
		"create: unknown method":    {handleReturnPoliciesCreate, `{"account":"1","countries":["CH"],"policy":{"type":"NO_RETURNS"},"return_methods":["BY_DRONE"],` + uri + `}`},
		"create: repeated method":   {handleReturnPoliciesCreate, `{"account":"1","countries":["CH"],"policy":{"type":"NO_RETURNS"},"return_methods":["BY_MAIL","BY_MAIL"],` + uri + `}`},
		"create: unknown condition": {handleReturnPoliciesCreate, `{"account":"1","countries":["CH"],"policy":{"type":"NO_RETURNS"},"item_conditions":["BROKEN"],` + uri + `}`},
		"create: unknown source":    {handleReturnPoliciesCreate, `{"account":"1","countries":["CH"],"policy":{"type":"NO_RETURNS"},"return_label_source":"BY_DRONE",` + uri + `}`},
		"create: refund days":       {handleReturnPoliciesCreate, `{"account":"1","countries":["CH"],"policy":{"type":"NO_RETURNS"},"process_refund_days":366,` + uri + `}`},
		"create: negative refund":   {handleReturnPoliciesCreate, `{"account":"1","countries":["CH"],"policy":{"type":"NO_RETURNS"},"process_refund_days":-1,` + uri + `}`},
		"create: empty restocking":  {handleReturnPoliciesCreate, `{"account":"1","countries":["CH"],"policy":{"type":"NO_RETURNS"},"restocking_fee":{},` + uri + `}`},
		"create: both restocking": {handleReturnPoliciesCreate, `{"account":"1","countries":["CH"],"policy":{"type":"NO_RETURNS"},` +
			`"restocking_fee":{"micro_percent":1,"fixed_fee":{"amount_micros":"1","currency_code":"CHF"}},` + uri + `}`},
		"create: percent too large": {handleReturnPoliciesCreate, `{"account":"1","countries":["CH"],"policy":{"type":"NO_RETURNS"},"restocking_fee":{"micro_percent":100000001},` + uri + `}`},
		"create: negative percent":  {handleReturnPoliciesCreate, `{"account":"1","countries":["CH"],"policy":{"type":"NO_RETURNS"},"restocking_fee":{"micro_percent":-1},` + uri + `}`},
		"create: amount not digits": {handleReturnPoliciesCreate, `{"account":"1","countries":["CH"],"policy":{"type":"NO_RETURNS"},"restocking_fee":{"fixed_fee":{"amount_micros":"1.5","currency_code":"CHF"}},` + uri + `}`},
		"create: leading zero":      {handleReturnPoliciesCreate, `{"account":"1","countries":["CH"],"policy":{"type":"NO_RETURNS"},"restocking_fee":{"fixed_fee":{"amount_micros":"01","currency_code":"CHF"}},` + uri + `}`},
		"create: amount as number":  {handleReturnPoliciesCreate, `{"account":"1","countries":["CH"],"policy":{"type":"NO_RETURNS"},"restocking_fee":{"fixed_fee":{"amount_micros":5,"currency_code":"CHF"}},` + uri + `}`},
		"create: currency":          {handleReturnPoliciesCreate, `{"account":"1","countries":["CH"],"policy":{"type":"NO_RETURNS"},"restocking_fee":{"fixed_fee":{"amount_micros":"1","currency_code":"chf"}},` + uri + `}`},
		"create: shipping type":     {handleReturnPoliciesCreate, `{"account":"1","countries":["CH"],"policy":{"type":"NO_RETURNS"},"return_shipping_fee":{"type":"FREE"},` + uri + `}`},
		"create: shipping no type":  {handleReturnPoliciesCreate, `{"account":"1","countries":["CH"],"policy":{"type":"NO_RETURNS"},"return_shipping_fee":{},` + uri + `}`},
		"create: fee on actual":     {handleReturnPoliciesCreate, `{"account":"1","countries":["CH"],"policy":{"type":"NO_RETURNS"},"return_shipping_fee":{"type":"CUSTOMER_PAYING_ACTUAL_FEE","fixed_fee":{"amount_micros":"1","currency_code":"CHF"}},` + uri + `}`},
		"create: unknown member":    {handleReturnPoliciesCreate, `{"account":"1","countries":["CH"],"policy":{"type":"NO_RETURNS"},"return_policy_id":"5",` + uri + `}`},
		"create: unknown nested":    {handleReturnPoliciesCreate, `{"account":"1","countries":["CH"],"policy":{"type":"NO_RETURNS","extra":1},` + uri + `}`},
		"create: unknown in price":  {handleReturnPoliciesCreate, `{"account":"1","countries":["CH"],"policy":{"type":"NO_RETURNS"},"restocking_fee":{"fixed_fee":{"amount_micros":"1","currency_code":"CHF","x":1}},` + uri + `}`},
		"create: uri not a string":  {handleReturnPoliciesCreate, `{"account":"1","countries":["CH"],"policy":{"type":"NO_RETURNS"},"return_policy_uri":5}`},
		"season: no label":          {handleReturnPoliciesCreate, `{"account":"1",` + season(`{"start_date":{"year":2026,"month":12,"day":1},"end_date":{"year":2026,"month":12,"day":31}}`) + `}`},
		"season: no end":            {handleReturnPoliciesCreate, `{"account":"1",` + season(`{"label":"W","start_date":{"year":2026,"month":12,"day":1}}`) + `}`},
		"season: long label":        {handleReturnPoliciesCreate, `{"account":"1",` + season(`{"label":"`+strings.Repeat("a", 101)+`","start_date":{"year":2026,"month":12,"day":1},"end_date":{"year":2026,"month":12,"day":31}}`) + `}`},
		"season: not a day":         {handleReturnPoliciesCreate, `{"account":"1",` + season(`{"label":"W","start_date":{"year":2026,"month":2,"day":30},"end_date":{"year":2026,"month":3,"day":31}}`) + `}`},
		"season: month 13":          {handleReturnPoliciesCreate, `{"account":"1",` + season(`{"label":"W","start_date":{"year":2026,"month":13,"day":1},"end_date":{"year":2027,"month":1,"day":1}}`) + `}`},
		"season: end before start":  {handleReturnPoliciesCreate, `{"account":"1",` + season(`{"label":"W","start_date":{"year":2026,"month":12,"day":31},"end_date":{"year":2026,"month":12,"day":1}}`) + `}`},
		"season: both ends":         {handleReturnPoliciesCreate, `{"account":"1",` + season(winter+`,"return_days":3,"return_until_date":{"year":2027,"month":1,"day":1}}`) + `}`},
		"season: bad until":         {handleReturnPoliciesCreate, `{"account":"1",` + season(winter+`,"return_until_date":{"year":2027,"month":2,"day":30}}`) + `}`},
		"season: days too large":    {handleReturnPoliciesCreate, `{"account":"1",` + season(winter+`,"return_days":3651}`) + `}`},
		"season: negative days":     {handleReturnPoliciesCreate, `{"account":"1",` + season(winter+`,"return_days":-1}`) + `}`},
		"season: overlap":           {handleReturnPoliciesCreate, `{"account":"1",` + season(winter+`},{"label":"B","start_date":{"year":2026,"month":12,"day":31},"end_date":{"year":2027,"month":1,"day":5}}`) + `}`},
		"season: too many": {handleReturnPoliciesCreate, `{"account":"1",` + season(strings.TrimSuffix(strings.Repeat(
			`{"label":"W","start_date":{"year":2026,"month":1,"day":1},"end_date":{"year":2026,"month":1,"day":1}},`, 101), ",")) + `}`},
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

func TestReturnPolicyAcceptsTheEdgesOfTheDocumentedForms(t *testing.T) {
	const uri = `"return_policy_uri":"https://shop.example.invalid/returns"`
	for name, args := range map[string]string{
		"no policy type with methods and conditions": `{"account":"1","countries":["CH"],"return_methods":["IN_STORE"],"item_conditions":["USED"],` + uri + `}`,
		"no returns without methods":                 `{"account":"1","countries":["CH"],"policy":{"type":"NO_RETURNS"},` + uri + `}`,
		"lifetime":                                   `{"account":"1","countries":["CH"],"policy":{"type":"LIFETIME_RETURNS"},"return_methods":["AT_A_KIOSK"],"item_conditions":["NEW"],` + uri + `}`,
		"free restocking":                            `{"account":"1","countries":["CH"],"policy":{"type":"NO_RETURNS"},"restocking_fee":{"micro_percent":0},` + uri + `}`,
		"fixed shipping fee left out":                `{"account":"1","countries":["CH"],"policy":{"type":"NO_RETURNS"},"return_shipping_fee":{"type":"FIXED"},` + uri + `}`,
		"zero refund days":                           `{"account":"1","countries":["CH"],"policy":{"type":"NO_RETURNS"},"process_refund_days":0,` + uri + `}`,
		"touching seasons are not overlapping":       `{"account":"1","countries":["CH"],"policy":{"type":"NO_RETURNS"},"seasonal_overrides":[{"label":"A","start_date":{"year":2026,"month":1,"day":1},"end_date":{"year":2026,"month":1,"day":31}},{"label":"B","start_date":{"year":2026,"month":2,"day":1},"end_date":{"year":2026,"month":2,"day":28}}],` + uri + `}`,
		"leap day":                                   `{"account":"1","countries":["CH"],"policy":{"type":"NO_RETURNS"},"seasonal_overrides":[{"label":"A","start_date":{"year":2028,"month":2,"day":29},"end_date":{"year":2028,"month":2,"day":29},"return_days":0}],` + uri + `}`,
		"a template placeholder in the address":      `{"account":"1","countries":["CH"],"policy":{"type":"NO_RETURNS"},"return_policy_uri":"https://shop.example.invalid/returns?lang={lang}"}`,
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, "accounts/1")
			h.google.api = func(*http.Request) (int, string, http.Header) { return 200, returnPolicyAnswer, nil }
			if _, err := h.call(t, handleReturnPoliciesCreate, args); err != nil {
				t.Errorf("err = %v", err)
			}
		})
	}
}

func TestReturnPolicyListSendsTheDocumentedParametersAndShowsEveryMember(t *testing.T) {
	h := newHarness(t)
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		if req.URL.Query().Get("pageToken") == "page-two" {
			return 200, `{"onlineReturnPolicies":[{"name":"accounts/1/onlineReturnPolicies/9","returnPolicyId":"9"}]}`, nil
		}
		return 200, `{"onlineReturnPolicies":[` + returnPolicyAnswer + `],"nextPageToken":"page-two"}`, nil
	}
	first, err := h.call(t, handleReturnPoliciesList, `{"account":"1","page_size":100}`)
	if err != nil {
		t.Fatal(err)
	}
	rows := first["onlineReturnPolicies"].([]any)
	if len(rows) != 1 || rows[0].(map[string]any)["unknownMember"] != nil {
		t.Fatalf("first = %v", first)
	}
	cursor, _ := first["next_cursor"].(string)
	if cursor == "" || cursor == "page-two" {
		t.Fatalf("next_cursor = %q", cursor)
	}
	second, err := h.call(t, handleReturnPoliciesList, `{"account":"1","page_size":100,"cursor":"`+cursor+`"}`)
	if err != nil || len(second["onlineReturnPolicies"].([]any)) != 1 || second["next_cursor"] != nil {
		t.Fatalf("second = %v, %v", second, err)
	}
	if _, err := h.call(t, handleReturnPoliciesList, `{"account":"1","page_size":10,"cursor":"`+cursor+`"}`); !isInvalidRequest(err) {
		t.Errorf("a cursor of other arguments: err = %v", err)
	}
	if _, err := h.call(t, handleReturnPoliciesList, `{"account":"2","page_size":100,"cursor":"`+cursor+`"}`); !isInvalidRequest(err) {
		t.Errorf("a cursor of another account: err = %v", err)
	}
	got := h.returnPolicyRequests(t)
	if len(got) != 2 || got[0] != (returnPolicyRequest{"GET", "/accounts/v1/accounts/1/onlineReturnPolicies?pageSize=100", ""}) ||
		got[1] != (returnPolicyRequest{"GET", "/accounts/v1/accounts/1/onlineReturnPolicies?pageSize=100&pageToken=page-two", ""}) {
		t.Errorf("requests = %v", got)
	}
	// An empty answer is an empty list, and an account without argument is the only bound one.
	h2 := newHarness(t, "accounts/7")
	empty, err := h2.call(t, handleReturnPoliciesList, `{}`)
	if rows, ok := empty["onlineReturnPolicies"].([]any); err != nil || !ok || len(rows) != 0 {
		t.Errorf("empty = %v, %v", empty, err)
	}
	if got := h2.returnPolicyRequests(t); len(got) != 1 || got[0].path != "/accounts/v1/accounts/7/onlineReturnPolicies" {
		t.Errorf("requests = %v", got)
	}
}

func TestReturnPolicyListRefusesTooManyEntriesAndALongToken(t *testing.T) {
	h := newHarness(t, "accounts/1")
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"onlineReturnPolicies":[],"nextPageToken":"` + strings.Repeat("x", maxPageToken+1) + `"}`, nil
	}
	if _, err := h.call(t, handleReturnPoliciesList, `{"account":"1"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("err = %v", err)
	}
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"onlineReturnPolicies":[` + strings.TrimSuffix(strings.Repeat(`{},`, maxEntries+1), ",") + `]}`, nil
	}
	if _, err := h.call(t, handleReturnPoliciesList, `{"account":"1"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("err = %v", err)
	}
}

func TestReturnPolicyGetShowsEveryDocumentedMember(t *testing.T) {
	h := newHarness(t)
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, returnPolicyAnswer, nil }
	out, err := h.call(t, handleReturnPoliciesGet, `{"account":"1000002","return_policy_id":"2000001"}`)
	if err != nil {
		t.Fatal(err)
	}
	if got := h.returnPolicyRequests(t); len(got) != 1 ||
		got[0] != (returnPolicyRequest{"GET", "/accounts/v1/accounts/1000002/onlineReturnPolicies/2000001", ""}) {
		t.Errorf("requests = %v", got)
	}
	encoded, _ := json.Marshal(out)
	want := strings.Replace(returnPolicyAnswer, `,"unknownMember":"dropped"`, "", 1)
	var wantMap map[string]any
	_ = json.Unmarshal([]byte(want), &wantMap)
	wantEncoded, _ := json.Marshal(wantMap)
	if string(encoded) != string(wantEncoded) {
		t.Errorf("out = %s\nwant  %s", encoded, wantEncoded)
	}
	// A false boolean and a zero number are still shown, and odd values are dropped or cut, never fetched.
	h = newHarness(t)
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"returnPolicyId":"1","acceptDefectiveOnly":false,"processRefundDays":0,"label":"` +
			strings.Repeat("é", 400) + `","countries":"CH","returnMethods":[1,"BY_MAIL"],"seasonalOverrides":[5,{"label":"W"}]}`, nil
	}
	out, err = h.call(t, handleReturnPoliciesGet, `{"account":"1","return_policy_id":"1"}`)
	if err != nil || out["acceptDefectiveOnly"] != false || out["processRefundDays"] != float64(0) ||
		len([]rune(out["label"].(string))) != maxNameRunes || out["countries"] != nil ||
		len(out["returnMethods"].([]any)) != 1 || len(out["seasonalOverrides"].([]any)) != 1 {
		t.Errorf("out = %v, %v", out, err)
	}
}

func TestReturnPolicyReadsAndWritesProveASubAccountOfABoundAdvancedAccount(t *testing.T) {
	proof := func(req *http.Request) (int, string, http.Header) {
		if strings.HasSuffix(req.URL.Path, ":listSubaccounts") {
			return 200, `{"accounts":[` + accountRow("2", "Two") + `]}`, nil
		}
		return 200, returnPolicyAnswer, nil
	}
	for name, c := range map[string]struct {
		handler capability.Handler
		args    string
		method  string
		path    string
	}{
		"get":    {handleReturnPoliciesGet, `{"account":"2","return_policy_id":"5"}`, "GET", "/accounts/v1/accounts/2/onlineReturnPolicies/5"},
		"create": {handleReturnPoliciesCreate, `{` + strings.Replace(returnPolicyMinimal, `"account":"1"`, `"account":"2"`, 1) + `}`, "POST", "/accounts/v1/accounts/2/onlineReturnPolicies"},
		"delete": {handleReturnPoliciesDelete, `{"account":"2","return_policy_id":"5"}`, "DELETE", "/accounts/v1/accounts/2/onlineReturnPolicies/5"},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, "advanced-accounts/1")
			h.google.api = proof
			if _, err := h.call(t, c.handler, c.args); err != nil {
				t.Fatal(err)
			}
			got := h.returnPolicyRequests(t)
			if len(got) != 2 || !strings.HasSuffix(got[0].path, "/accounts/1:listSubaccounts?pageSize=500") ||
				got[1].method != c.method || got[1].path != c.path {
				t.Errorf("requests = %v", got)
			}
			// An unproven account is refused after the proof read and never changed.
			h = newHarness(t, "advanced-accounts/1")
			h.google.api = proof
			other := strings.Replace(c.args, `"account":"2"`, `"account":"3"`, 1)
			if _, err := h.call(t, c.handler, other); !isInvalidRequest(err) {
				t.Errorf("err = %v", err)
			}
			for _, r := range h.returnPolicyRequests(t) {
				if r.method != "GET" {
					t.Errorf("request %v", r)
				}
			}
		})
	}
}

func TestReturnPolicyCreateSendsOnlyThePassedMembers(t *testing.T) {
	h := newHarness(t, "accounts/1000002")
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, returnPolicyAnswer, nil }
	out, err := h.call(t, handleReturnPoliciesCreate, returnPolicyFull)
	if err != nil || out["returnPolicyId"] != "2000001" {
		t.Fatalf("out = %v, %v", out, err)
	}
	got := h.returnPolicyRequests(t)
	if len(got) != 1 || got[0].method != "POST" || got[0].path != "/accounts/v1/accounts/1000002/onlineReturnPolicies" {
		t.Fatalf("requests = %v", got)
	}
	if got[0].body != returnPolicyFullBody {
		t.Errorf("body = %s\nwant   %s", got[0].body, returnPolicyFullBody)
	}
	h = newHarness(t, "accounts/1")
	if _, err := h.call(t, handleReturnPoliciesCreate, `{`+returnPolicyMinimal+`}`); err != nil {
		t.Fatal(err)
	}
	want := `{"countries":["CH"],"policy":{"type":"NO_RETURNS"},"returnPolicyUri":"https://shop.example.invalid/returns"}`
	if got := h.returnPolicyRequests(t); got[0].body != want {
		t.Errorf("body = %s\nwant   %s", got[0].body, want)
	}
	// A zero percent fee and a seasonal override without a return window are sent as passed.
	h = newHarness(t, "accounts/1")
	if _, err := h.call(t, handleReturnPoliciesCreate, `{"account":"1","countries":["CH"],"policy":{"type":"NO_RETURNS"},`+
		`"restocking_fee":{"micro_percent":0},"seasonal_overrides":[{"label":"W","start_date":{"year":2026,"month":1,"day":1},`+
		`"end_date":{"year":2026,"month":1,"day":2},"return_days":0}],"process_refund_days":0,"accept_exchange":false,`+
		`"return_policy_uri":"https://shop.example.invalid/returns"}`); err != nil {
		t.Fatal(err)
	}
	want = `{"countries":["CH"],"policy":{"type":"NO_RETURNS"},` +
		`"seasonalOverrides":[{"label":"W","startDate":{"year":2026,"month":1,"day":1},` +
		`"endDate":{"year":2026,"month":1,"day":2},"returnDays":0}],"restockingFee":{"microPercent":0},` +
		`"returnPolicyUri":"https://shop.example.invalid/returns","processRefundDays":0,"acceptExchange":false}`
	if got := h.returnPolicyRequests(t); got[0].body != want {
		t.Errorf("body = %s\nwant   %s", got[0].body, want)
	}
}

func TestReturnPolicyDeleteSendsOneBodylessDeleteAndReportsAcceptance(t *testing.T) {
	for name, status := range map[string]int{"empty object": 200, "no content": 204} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, "accounts/1000002")
			h.google.api = func(*http.Request) (int, string, http.Header) { return status, `{}`, nil }
			out, err := h.call(t, handleReturnPoliciesDelete, `{"account":"1000002","return_policy_id":"2000001"}`)
			if err != nil || out["deleted"] != true || out["account"] != "1000002" || out["return_policy_id"] != "2000001" {
				t.Fatalf("out = %v, %v", out, err)
			}
			if got := h.returnPolicyRequests(t); len(got) != 1 ||
				got[0] != (returnPolicyRequest{"DELETE", "/accounts/v1/accounts/1000002/onlineReturnPolicies/2000001", ""}) {
				t.Errorf("requests = %v", got)
			}
		})
	}
}

func TestReturnPolicyChangeIsSentOnceAndAnUnclearOutcomeIsSaid(t *testing.T) {
	for _, tool := range []struct {
		name    string
		handler capability.Handler
		args    string
	}{
		{"create", handleReturnPoliciesCreate, `{` + returnPolicyMinimal + `}`},
		{"delete", handleReturnPoliciesDelete, `{"account":"1","return_policy_id":"5"}`},
	} {
		for name, c := range map[string]struct {
			status      int
			unclearText bool
			class       provider.Class
		}{
			"server error":  {500, true, provider.ClassProviderError},
			"unauthorized":  {401, false, provider.ClassAuth},
			"rate limited":  {429, false, provider.ClassRateLimited},
			"bad request":   {400, false, ""},
			"not found":     {404, false, provider.ClassNotFound},
			"unavailable":   {503, true, provider.ClassUnreachable},
			"gateway error": {502, true, provider.ClassProviderError},
		} {
			t.Run(tool.name+" "+name, func(t *testing.T) {
				h := newHarness(t, "accounts/1")
				h.google.api = func(*http.Request) (int, string, http.Header) {
					return c.status, `{"error":{"code":0,"message":"untrusted text","status":"X"}}`, nil
				}
				_, err := h.call(t, tool.handler, tool.args)
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
				if got := h.returnPolicyRequests(t); len(got) != 1 {
					t.Errorf("%d requests, want exactly one", len(got))
				}
			})
		}
	}
}

func TestReturnPolicyQuotaErrorIsMappedAndNotRepeated(t *testing.T) {
	h := newHarness(t, "accounts/1")
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 429, `{"error":{"code":429,"status":"RESOURCE_EXHAUSTED","details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo",` +
			`"reason":"QUOTA_TOO_MANY_REQUESTS"}]}}`, nil
	}
	_, err := h.call(t, handleReturnPoliciesCreate, `{`+returnPolicyMinimal+`}`)
	if classOf(err) != provider.ClassRateLimited || !strings.Contains(err.Error(), "daily") {
		t.Errorf("err = %v", err)
	}
	if got := h.returnPolicyRequests(t); len(got) != 1 {
		t.Errorf("%d requests, want exactly one", len(got))
	}
}

func TestReturnPolicyConfirmationSummariesNameTheChange(t *testing.T) {
	summaries := returnPolicySummaries()
	if len(summaries) != 2 || summaries[onlineReturnPoliciesListID] != nil || summaries[onlineReturnPoliciesGetID] != nil {
		t.Fatalf("summaries = %v", summaries)
	}
	create := summaries[onlineReturnPoliciesCreateID](json.RawMessage(returnPolicyFull))
	for _, want := range []string{"account 1000002", "CH, DE", `"Example label"`, "NUMBER_OF_DAYS_AFTER_DELIVERY 30 days",
		`return_policy_uri "https://shop.example.invalid/returns"`} {
		if !strings.Contains(create, want) {
			t.Errorf("create summary %q lacks %q", create, want)
		}
	}
	del := summaries[onlineReturnPoliciesDeleteID](json.RawMessage(`{"account":"1","return_policy_id":"5"}`))
	if !strings.Contains(del, `"5"`) || !strings.Contains(del, "account 1") {
		t.Errorf("delete summary = %q", del)
	}
	if summaries[onlineReturnPoliciesCreateID](json.RawMessage(`[`)) != "" || summaries[onlineReturnPoliciesCreateID](json.RawMessage(`{}`)) != "" ||
		summaries[onlineReturnPoliciesDeleteID](json.RawMessage(`[`)) != "" {
		t.Error("a summary of unreadable arguments is not empty")
	}
}
