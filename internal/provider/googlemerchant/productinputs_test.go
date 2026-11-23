package googlemerchant

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// Synthetic values: reserved example hosts and plain placeholders, none of them a real address or person.
const (
	fixtureIdentity = `"account":"1000001","data_source":"2000002","content_language":"en","feed_label":"US",` +
		`"offer_id":"sku-example-1"`
	fixtureInputName = "accounts/1000001/productInputs/ZW5-VVN-c2t1LWV4YW1wbGUtMQ"
	fixtureAnswer    = `{"name":"` + fixtureInputName + `","product":"accounts/1000001/products/ZW5-VVN-c2t1LWV4YW1wbGUtMQ",` +
		`"offerId":"sku-example-1","contentLanguage":"en","feedLabel":"US","versionNumber":"7",` +
		`"base64EncodedName":"ZW5-VVN-c2t1LWV4YW1wbGUtMQ","base64EncodedProduct":"never shown",` +
		`"productAttributes":{"title":"Example product","link":"https://shop.example.invalid/p/1",` +
		`"price":{"amountMicros":"15990000","currencyCode":"USD","unknownMember":"never held"},` +
		`"gtins":["00000000000000"],"isBundle":true,"latitude":1.5,"availability":"IN_STOCK",` +
		`"returns":[{"policyUrl":"https://shop.example.invalid/returns"}],"notAnAttribute":"never held"},` +
		`"customAttributes":[{"name":"size type","value":"regular"},` +
		`{"name":"group","groupValues":[{"name":"a","value":"b","extra":"never held"}]}],"unknownTop":"never held"}`
)

func TestProductInputToolsAreDeclaredWithTheirRisk(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	want := map[string]capability.Effect{productInputsInsertID: capability.EffectCreate,
		productInputsUpdateID: capability.EffectUpdate, productInputsDeleteID: capability.EffectDelete}
	for id, effect := range want {
		d, _, ok := reg.Lookup(id)
		if !ok || d.Version != 1 || d.Risk.Effect != effect || d.Risk.Idempotency != capability.IdempotencyIdempotent ||
			d.Risk.Confirmation != capability.ConfirmationRequired || !d.Risk.OpenWorld || !d.RequiresToolAllowList {
			t.Errorf("%s = %+v, %v", id, d.Risk, ok)
		}
		if scopes := metadata.Token.Scopes[id]; len(scopes) != 1 || scopes[0] != contentScope {
			t.Errorf("%s scopes = %v", id, scopes)
		}
		if slices.Contains(metadata.Profiles[0].Tools, id) {
			t.Errorf("%s is in the read profile", id)
		}
		for _, w := range []string{"several minutes", "only that Google accepted", "never repeats"} {
			if !strings.Contains(d.Description, w) {
				t.Errorf("%s description misses %q", id, w)
			}
		}
		if !json.Valid(d.InputSchema) || !json.Valid(d.OutputSchema) ||
			!strings.Contains(string(d.InputSchema), `"additionalProperties":false`) {
			t.Errorf("%s has an invalid or open schema", id)
		}
	}
	insert, _, _ := reg.Lookup(productInputsInsertID)
	if !strings.Contains(insert.Description, "replaces it completely") {
		t.Errorf("insert does not say that it replaces: %s", insert.Description)
	}
}

func TestEveryDocumentedAttributeIsAMemberOfTheSchemas(t *testing.T) {
	if got := len(attributeNames()); got != 145 {
		t.Fatalf("%d attributes, want 145", got)
	}
	var schema struct {
		Properties map[string]struct {
			Properties map[string]json.RawMessage `json:"properties"`
		} `json:"properties"`
	}
	for _, d := range []capability.Descriptor{productInputsInsert, productInputsUpdate} {
		if err := json.Unmarshal(d.InputSchema, &schema); err != nil {
			t.Fatal(err)
		}
		for _, name := range attributeNames() {
			if _, ok := schema.Properties["attributes"].Properties[name]; !ok {
				t.Errorf("%s: attribute %s is missing", d.ID, name)
			}
		}
	}
	var out struct {
		Properties struct {
			ProductInput struct {
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"productInput"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(productInputsInsert.OutputSchema, &out); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"name", "product", "offerId", "contentLanguage", "feedLabel", "legacyLocal",
		"versionNumber", "base64EncodedName", "productAttributes", "customAttributes"} {
		if _, ok := out.Properties.ProductInput.Properties[name]; !ok {
			t.Errorf("output member %s is missing", name)
		}
	}
	if _, ok := out.Properties.ProductInput.Properties["base64EncodedProduct"]; ok {
		t.Error("base64EncodedProduct must not be an output member")
	}
}

// requestBody decodes the JSON body of the nth recorded request.
func requestBody(t *testing.T, got []sent, n int) map[string]any {
	t.Helper()
	var body map[string]any
	if n >= len(got) || json.Unmarshal([]byte(got[n].body), &body) != nil {
		t.Fatalf("no JSON body at request %d of %+v", n, got)
	}
	return body
}

func TestProductInputsInsertSendsTheCheckedProductAndShowsOnlyDocumentedMembers(t *testing.T) {
	h := newHarness(t)
	h.answerWith(200, fixtureAnswer)
	out, err := h.call(t, handleProductInputsInsert, `{`+fixtureIdentity+`,"legacy_local":true,"version_number":"7",`+
		`"attributes":{"title":"Example product","link":"https://shop.example.invalid/p/1","imageLink":"https://img.example.invalid/1.jpg",`+
		`"price":{"amountMicros":"15990000","currencyCode":"USD"},"availability":"IN_STOCK","gtins":["00000000000000"],`+
		`"isBundle":true,"latitude":1.5,"availabilityDate":"2030-01-31T09:00:00Z"},`+
		`"custom_attributes":[{"name":"size_type","value":"regular"},{"name":"group","groupValues":[{"name":"a","value":"b"}]}]}`)
	if err != nil {
		t.Fatal(err)
	}
	got := h.recorded(t)
	if len(got) != 1 || got[0].method != http.MethodPost || got[0].path != "/products/v1/accounts/1000001/productInputs:insert" ||
		got[0].contentType != "application/json" {
		t.Fatalf("requests = %+v", got)
	}
	query, _ := url.ParseQuery(got[0].query)
	if len(query) != 1 || query.Get("dataSource") != "accounts/1000001/dataSources/2000002" {
		t.Errorf("query = %v, want the data source only and no mask", query)
	}
	body := requestBody(t, got, 0)
	if body["offerId"] != "sku-example-1" || body["contentLanguage"] != "en" || body["feedLabel"] != "US" ||
		body["legacyLocal"] != true || body["versionNumber"] != "7" || body["name"] != nil {
		t.Errorf("body = %v", body)
	}
	attributes := body["productAttributes"].(map[string]any)
	if attributes["title"] != "Example product" || attributes["latitude"] != 1.5 ||
		attributes["price"].(map[string]any)["amountMicros"] != "15990000" || len(attributes) != 9 {
		t.Errorf("attributes = %v", attributes)
	}
	if custom := body["customAttributes"].([]any); len(custom) != 2 {
		t.Errorf("custom attributes = %v", custom)
	}
	if out["accepted"] != true {
		t.Errorf("result = %v", out)
	}
	shown := out["productInput"].(map[string]any)
	encoded, _ := json.Marshal(shown)
	for _, never := range []string{"never held", "never shown", "base64EncodedProduct", "unknownTop"} {
		if strings.Contains(string(encoded), never) {
			t.Errorf("result shows %q: %s", never, encoded)
		}
	}
	if shown["name"] != fixtureInputName || shown["versionNumber"] != "7" || shown["base64EncodedName"] == nil ||
		shown["productAttributes"].(map[string]any)["link"] != "https://shop.example.invalid/p/1" ||
		shown["productAttributes"].(map[string]any)["returns"].([]any)[0].(map[string]any)["policyUrl"] != "https://shop.example.invalid/returns" {
		t.Errorf("shown = %v", shown)
	}
	if groups := shown["customAttributes"].([]any)[1].(map[string]any)["groupValues"].([]any); len(groups[0].(map[string]any)) != 2 {
		t.Errorf("group values = %v", groups)
	}
}

// Insert replaces: the body is the complete input and carries no mask, so what a second insert leaves out
// is gone at Google, and the same identity always goes to the same data source.
func TestProductInputsInsertReplacesTheWholeInputWithoutAMask(t *testing.T) {
	h := newHarness(t)
	h.answerWith(200, fixtureAnswer)
	for _, attributes := range []string{`{"title":"One","price":{"amountMicros":"1000000","currencyCode":"USD"}}`, `{"title":"Two"}`} {
		if _, err := h.call(t, handleProductInputsInsert, `{`+fixtureIdentity+`,"attributes":`+attributes+`}`); err != nil {
			t.Fatal(err)
		}
	}
	got := h.recorded(t)
	if len(got) != 2 || got[0].path != got[1].path || got[0].query != got[1].query || strings.Contains(got[1].query, "updateMask") {
		t.Fatalf("requests = %+v", got)
	}
	first, second := requestBody(t, got, 0), requestBody(t, got, 1)
	if _, ok := first["productAttributes"].(map[string]any)["price"]; !ok {
		t.Errorf("first body = %v", first)
	}
	if attrs := second["productAttributes"].(map[string]any); len(attrs) != 1 || attrs["title"] != "Two" {
		t.Errorf("second body = %v, want only what the call sent", second)
	}
	if first["offerId"] != second["offerId"] || first["contentLanguage"] != second["contentLanguage"] ||
		first["feedLabel"] != second["feedLabel"] {
		t.Error("the identity of the input changed between the calls")
	}
	// A minimal insert is only the identity.
	h = newHarness(t)
	h.answerWith(200, `{}`)
	out, err := h.call(t, handleProductInputsInsert, `{`+fixtureIdentity+`}`)
	if err != nil || len(requestBody(t, h.recorded(t), 0)) != 3 || out["accepted"] != true {
		t.Errorf("minimal insert: %v, %v", out, err)
	}
}

func decodeMask(t *testing.T, q string) []string {
	t.Helper()
	values, err := url.ParseQuery(q)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(values.Get("updateMask"), ",")
}

func TestProductInputsUpdateBuildsTheMaskFromTheMembersThatAreSet(t *testing.T) {
	h := newHarness(t)
	h.answerWith(200, fixtureAnswer)
	out, err := h.call(t, handleProductInputsUpdate, `{`+fixtureIdentity+`,"attributes":{"title":"New","availability":"OUT_OF_STOCK",`+
		`"price":{"amountMicros":"1000000","currencyCode":"USD"}},"delete_attributes":["salePrice","brand"],`+
		`"custom_attributes":[{"name":"size_type","value":"regular"}],"delete_custom_attributes":["old_label"]}`)
	if err != nil {
		t.Fatal(err)
	}
	got := h.recorded(t)
	name := "accounts/1000001/productInputs/" + base64.RawURLEncoding.EncodeToString([]byte("en~US~sku-example-1"))
	if len(got) != 1 || got[0].method != http.MethodPatch || got[0].path != "/products/v1/"+name {
		t.Fatalf("requests = %+v", got)
	}
	wantMask := []string{"productAttributes.availability", "productAttributes.price", "productAttributes.title",
		"productAttributes.salePrice", "productAttributes.brand", "custom_attribute.size_type", "custom_attribute.old_label"}
	if mask := decodeMask(t, got[0].query); !slices.Equal(mask, wantMask) {
		t.Errorf("mask = %v, want %v", mask, wantMask)
	}
	values, _ := url.ParseQuery(got[0].query)
	if values.Get("dataSource") != "accounts/1000001/dataSources/2000002" || len(values) != 2 {
		t.Errorf("query = %v", values)
	}
	body := requestBody(t, got, 0)
	attributes := body["productAttributes"].(map[string]any)
	if body["name"] != name || len(attributes) != 3 || attributes["salePrice"] != nil || attributes["brand"] != nil ||
		body["offerId"] != nil || len(body["customAttributes"].([]any)) != 1 {
		t.Errorf("body = %v", body)
	}
	if out["accepted"] != true || out["productInput"].(map[string]any)["name"] != fixtureInputName {
		t.Errorf("result = %v", out)
	}
	// The name of a legacy local product carries the local~ prefix, and an offer ID with a slash is encoded.
	h = newHarness(t)
	h.answerWith(200, `{}`)
	_, err = h.call(t, handleProductInputsUpdate, `{"account":"1000001","data_source":"2000002","content_language":"de",`+
		`"feed_label":"CH_1","offer_id":"a/b~c","legacy_local":true,"delete_attributes":["pause"]}`)
	if err != nil {
		t.Fatal(err)
	}
	local := h.recorded(t)[0]
	if want := "/products/v1/accounts/1000001/productInputs/" + base64.RawURLEncoding.EncodeToString([]byte("local~de~CH_1~a/b~c")); local.path != want {
		t.Errorf("path = %s, want %s", local.path, want)
	}
	if mask := decodeMask(t, local.query); !slices.Equal(mask, []string{"productAttributes.pause"}) {
		t.Errorf("mask = %v", mask)
	}
	if body := requestBody(t, h.recorded(t), 0); body["productAttributes"] != nil || body["customAttributes"] != nil {
		t.Errorf("a removal carries a value: %v", body)
	}
}

func TestEveryMaskPathIsADocumentedAttributeOrACustomAttributeNeverAWildcard(t *testing.T) {
	for _, name := range attributeNames() {
		mask, _, err := productUpdateRequest("1000001", productUpdateArguments{productIdentity: productIdentity{
			ContentLanguage: "en", FeedLabel: "US", OfferID: "x"}, DeleteAttributes: []string{name}})
		if err != nil || len(mask) != 1 || mask[0] != "productAttributes."+name || strings.ContainsAny(mask[0], "*,") {
			t.Errorf("%s: mask %v, %v", name, mask, err)
		}
	}
	for _, name := range []string{"*", "productAttributes.title", "title,price", "custom_attribute.x", "unknown", "", "TITLE"} {
		_, _, err := productUpdateRequest("1000001", productUpdateArguments{productIdentity: productIdentity{
			ContentLanguage: "en", FeedLabel: "US", OfferID: "x"}, DeleteAttributes: []string{name}})
		if !isInvalidRequest(err) {
			t.Errorf("delete_attributes %q: err = %v", name, err)
		}
	}
	for _, name := range []string{"*", "a.b", "a,b", "a b", "", "_a", strings.Repeat("a", 101)} {
		_, _, err := productUpdateRequest("1000001", productUpdateArguments{productIdentity: productIdentity{
			ContentLanguage: "en", FeedLabel: "US", OfferID: "x"}, DeleteCustomAttributes: []string{name}})
		if !isInvalidRequest(err) {
			t.Errorf("delete_custom_attributes %q: err = %v", name, err)
		}
	}
}

func TestProductInputWritesRefuseBadArgumentsBeforeSecretAndIO(t *testing.T) {
	const update = `{` + fixtureIdentity
	long := strings.Repeat("a", 5001)
	manyCustom := make([]string, 2501)
	for i := range manyCustom {
		manyCustom[i] = fmt.Sprintf(`{"name":"n%d","value":"v"}`, i)
	}
	cases := map[string]struct {
		handler capability.Handler
		args    string
	}{
		"no data source":        {handleProductInputsInsert, `{"account":"1000001","content_language":"en","feed_label":"US","offer_id":"x"}`},
		"data source not id":    {handleProductInputsInsert, `{"account":"1000001","data_source":"x","content_language":"en","feed_label":"US","offer_id":"x"}`},
		"data source path":      {handleProductInputsDelete, `{"account":"1000001","data_source":"1/../2","content_language":"en","feed_label":"US","offer_id":"x"}`},
		"language":              {handleProductInputsInsert, `{"account":"1000001","data_source":"1","content_language":"EN","feed_label":"US","offer_id":"x"}`},
		"feed label":            {handleProductInputsInsert, `{"account":"1000001","data_source":"1","content_language":"en","feed_label":"us","offer_id":"x"}`},
		"feed label long":       {handleProductInputsDelete, `{"account":"1000001","data_source":"1","content_language":"en","feed_label":"AAAAAAAAAAAAAAAAAAAAA","offer_id":"x"}`},
		"offer id empty":        {handleProductInputsDelete, `{"account":"1000001","data_source":"1","content_language":"en","feed_label":"US","offer_id":""}`},
		"offer id space":        {handleProductInputsDelete, `{"account":"1000001","data_source":"1","content_language":"en","feed_label":"US","offer_id":" x"}`},
		"offer id control":      {handleProductInputsInsert, `{"account":"1000001","data_source":"1","content_language":"en","feed_label":"US","offer_id":"a\u0000b"}`},
		"unknown member":        {handleProductInputsInsert, update + `,"update_mask":["title"]}`},
		"mask on delete":        {handleProductInputsDelete, update + `,"attributes":{"title":"A"}}`},
		"version not digits":    {handleProductInputsInsert, update + `,"version_number":"-1"}`},
		"version too long":      {handleProductInputsInsert, update + `,"version_number":"12345678901234567890"}`},
		"attributes empty":      {handleProductInputsInsert, update + `,"attributes":{}}`},
		"attributes list":       {handleProductInputsInsert, update + `,"attributes":[]}`},
		"unknown attribute":     {handleProductInputsInsert, update + `,"attributes":{"nonsense":"x"}}`},
		"unknown nested":        {handleProductInputsInsert, update + `,"attributes":{"price":{"amountMicros":"1","currencyCode":"USD","x":1}}}`},
		"text type":             {handleProductInputsInsert, update + `,"attributes":{"title":5}}`},
		"text empty":            {handleProductInputsInsert, update + `,"attributes":{"title":""}}`},
		"text long":             {handleProductInputsInsert, update + `,"attributes":{"title":"` + long + `"}}`},
		"text control":          {handleProductInputsInsert, update + `,"attributes":{"title":"a\u0007b"}}`},
		"null":                  {handleProductInputsInsert, update + `,"attributes":{"title":null}}`},
		"int64 as number":       {handleProductInputsInsert, update + `,"attributes":{"price":{"amountMicros":1000000,"currencyCode":"USD"}}}`},
		"int64 text":            {handleProductInputsInsert, update + `,"attributes":{"price":{"amountMicros":"1.5","currencyCode":"USD"}}}`},
		"int64 overflow":        {handleProductInputsInsert, update + `,"attributes":{"multipack":"99999999999999999999"}}`},
		"enum":                  {handleProductInputsInsert, update + `,"attributes":{"availability":"MAYBE"}}`},
		"enum unspecified":      {handleProductInputsInsert, update + `,"attributes":{"availability":"AVAILABILITY_UNSPECIFIED"}}`},
		"bool":                  {handleProductInputsInsert, update + `,"attributes":{"isBundle":"yes"}}`},
		"number range":          {handleProductInputsInsert, update + `,"attributes":{"latitude":91}}`},
		"number type":           {handleProductInputsInsert, update + `,"attributes":{"latitude":"1"}}`},
		"datetime":              {handleProductInputsInsert, update + `,"attributes":{"availabilityDate":"tomorrow"}}`},
		"list empty":            {handleProductInputsInsert, update + `,"attributes":{"gtins":[]}}`},
		"list too long":         {handleProductInputsInsert, update + `,"attributes":{"gtins":["1","2","3","4","5","6","7","8","9","10","11"]}}`},
		"list wrong item":       {handleProductInputsInsert, update + `,"attributes":{"gtins":[1]}}`},
		"link http":             {handleProductInputsInsert, update + `,"attributes":{"link":"http://shop.example.invalid/p"}}`},
		"link user info":        {handleProductInputsInsert, update + `,"attributes":{"link":"https://user:pw` + "@" + `shop.example.invalid/p"}}`},
		"link scheme":           {handleProductInputsInsert, update + `,"attributes":{"link":"javascript:alert(1)"}}`},
		"link no host":          {handleProductInputsInsert, update + `,"attributes":{"link":"https:///p"}}`},
		"link backslash":        {handleProductInputsInsert, update + `,"attributes":{"link":"https://shop.example.invalid\\@evil.example.invalid/"}}`},
		"link space":            {handleProductInputsInsert, update + `,"attributes":{"link":"https://shop.example.invalid/a b"}}`},
		"link long":             {handleProductInputsInsert, update + `,"attributes":{"link":"https://shop.example.invalid/` + strings.Repeat("a", 2000) + `"}}`},
		"mobile link":           {handleProductInputsUpdate, update + `,"attributes":{"mobileLink":"http://m.example.invalid/"}}`},
		"canonical link":        {handleProductInputsInsert, update + `,"attributes":{"canonicalLink":"ftp://shop.example.invalid/"}}`},
		"ads redirect":          {handleProductInputsInsert, update + `,"attributes":{"adsRedirect":"//shop.example.invalid/"}}`},
		"link template":         {handleProductInputsInsert, update + `,"attributes":{"linkTemplate":"http://shop.example.invalid/{id}"}}`},
		"image link":            {handleProductInputsInsert, update + `,"attributes":{"imageLink":"http://img.example.invalid/1.jpg"}}`},
		"additional image":      {handleProductInputsInsert, update + `,"attributes":{"additionalImageLinks":["https://img.example.invalid/1.jpg","http://img.example.invalid/2.jpg"]}}`},
		"video link":            {handleProductInputsInsert, update + `,"attributes":{"videoLinks":["https://u` + "@" + `v.example.invalid/1"]}}`},
		"nested policy url":     {handleProductInputsInsert, update + `,"attributes":{"returns":[{"policyUrl":"http://shop.example.invalid/r"}]}}`},
		"nested certification":  {handleProductInputsInsert, update + `,"attributes":{"certifications":[{"certificationDocumentLink":"http://x.example.invalid/c"}]}}`},
		"custom not list":       {handleProductInputsInsert, update + `,"custom_attributes":{"name":"a","value":"b"}}`},
		"custom empty":          {handleProductInputsInsert, update + `,"custom_attributes":[]}`},
		"custom no name":        {handleProductInputsInsert, update + `,"custom_attributes":[{"value":"b"}]}`},
		"custom bad name":       {handleProductInputsInsert, update + `,"custom_attributes":[{"name":"a.b","value":"b"}]}`},
		"custom duplicate":      {handleProductInputsInsert, update + `,"custom_attributes":[{"name":"a","value":"b"},{"name":"a","value":"c"}]}`},
		"custom both":           {handleProductInputsInsert, update + `,"custom_attributes":[{"name":"a","value":"b","groupValues":[{"name":"c","value":"d"}]}]}`},
		"custom neither":        {handleProductInputsInsert, update + `,"custom_attributes":[{"name":"a"}]}`},
		"custom empty value":    {handleProductInputsInsert, update + `,"custom_attributes":[{"name":"a","value":""}]}`},
		"custom empty group":    {handleProductInputsInsert, update + `,"custom_attributes":[{"name":"a","groupValues":[]}]}`},
		"custom nested group":   {handleProductInputsInsert, update + `,"custom_attributes":[{"name":"a","groupValues":[{"name":"c","groupValues":[]}]}]}`},
		"custom too long":       {handleProductInputsInsert, update + `,"custom_attributes":[{"name":"abc","value":"` + strings.Repeat("v", 10238) + `"}]}`},
		"custom too many":       {handleProductInputsInsert, update + `,"custom_attributes":[` + strings.Join(manyCustom, ",") + `]}`},
		"custom total size":     {handleProductInputsInsert, update + `,"custom_attributes":[` + customOverTotal() + `]}`},
		"nothing to update":     {handleProductInputsUpdate, update + `}`},
		"set and remove":        {handleProductInputsUpdate, update + `,"attributes":{"title":"A"},"delete_attributes":["title"]}`},
		"custom set and remove": {handleProductInputsUpdate, update + `,"custom_attributes":[{"name":"a","value":"b"}],"delete_custom_attributes":["a"]}`},
		"delete twice":          {handleProductInputsUpdate, update + `,"delete_attributes":["title","title"]}`},
		"wildcard":              {handleProductInputsUpdate, update + `,"delete_attributes":["*"]}`},
		"update link http":      {handleProductInputsUpdate, update + `,"attributes":{"link":"http://shop.example.invalid/"}}`},
	}
	for name, c := range cases {
		h := newHarness(t)
		_, err := h.call(t, c.handler, c.args)
		if !isInvalidRequest(err) {
			t.Errorf("%s: err = %v", name, err)
		} else {
			for _, leaked := range []string{"example.invalid", "user:pw"} {
				if strings.Contains(err.Error(), leaked) {
					t.Errorf("%s: the message quotes the value: %v", name, err)
				}
			}
		}
		h.untouched(t)
	}
}

// customOverTotal is custom attributes whose single sizes pass but whose total exceeds 102400 bytes.
func customOverTotal() string {
	var parts []string
	for i := 0; i < 11; i++ {
		parts = append(parts, fmt.Sprintf(`{"name":"n%d","value":"%s"}`, i, strings.Repeat("v", 10000)))
	}
	return strings.Join(parts, ",")
}

func TestProductInputAddressesAreCheckedAtEveryDepthAndAcceptedWithHTTPS(t *testing.T) {
	h := newHarness(t)
	h.answerWith(200, `{}`)
	args := `{` + fixtureIdentity + `,"attributes":{"link":"https://shop.example.invalid/p/{id}","mobileLink":"https://m.example.invalid/p",` +
		`"canonicalLink":"https://shop.example.invalid/c","adsRedirect":"https://shop.example.invalid/ads","imageLink":"https://img.example.invalid/1.jpg",` +
		`"additionalImageLinks":["https://img.example.invalid/2.jpg"],"returns":[{"policyUrl":"https://shop.example.invalid/r"}],` +
		`"certifications":[{"certificationLabelLink":"https://x.example.invalid/l"}],"linkTemplate":"https://shop.example.invalid/{id}"}}`
	if _, err := h.call(t, handleProductInputsInsert, args); err != nil {
		t.Fatal(err)
	}
	// Nothing is fetched: the only requests are the one change to the fixed origin.
	if got := h.recorded(t); len(got) != 1 {
		t.Errorf("requests = %+v", got)
	}
}

func TestProductInputToolsCheckTheAccountBeforeSecretAndIO(t *testing.T) {
	calls := map[string]struct {
		handler capability.Handler
		args    string
	}{
		"insert": {handleProductInputsInsert, `{"account":"2000002","data_source":"1","content_language":"en","feed_label":"US","offer_id":"x"}`},
		"update": {handleProductInputsUpdate, `{"account":"2000002","data_source":"1","content_language":"en","feed_label":"US","offer_id":"x","delete_attributes":["pause"]}`},
		"delete": {handleProductInputsDelete, `{"account":"2000002","data_source":"1","content_language":"en","feed_label":"US","offer_id":"x"}`},
	}
	for name, c := range calls {
		h := newHarness(t, "accounts/1000001")
		if _, err := h.call(t, c.handler, c.args); !isInvalidRequest(err) || strings.Contains(err.Error(), "2000002") {
			t.Errorf("%s: err = %v", name, err)
		}
		h.untouched(t)
		h = newHarness(t, "accounts/1", "accounts/2")
		if _, err := h.call(t, c.handler, strings.Replace(c.args, `"account":"2000002",`, "", 1)); !isInvalidRequest(err) {
			t.Errorf("%s without account: err = %v", name, err)
		}
		h.untouched(t)
	}
}

func TestProductInputWritesProveASubAccountOfABoundAdvancedAccount(t *testing.T) {
	h := newHarness(t, "advanced-accounts/10")
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		if req.Body != nil {
			data, _ := io.ReadAll(req.Body)
			recordedBody[req] = string(data)
		}
		if strings.HasSuffix(req.URL.Path, ":listSubaccounts") {
			return 200, `{"accounts":[` + accountRow("11", "Sub") + `]}`, nil
		}
		return 200, `{}`, nil
	}
	if _, err := h.call(t, handleProductInputsDelete, `{"account":"11","data_source":"5","content_language":"en","feed_label":"US","offer_id":"x"}`); err != nil {
		t.Fatal(err)
	}
	got := h.recorded(t)
	if len(got) != 2 || got[0].method != http.MethodGet || !strings.HasSuffix(got[0].path, "accounts/10:listSubaccounts") ||
		got[1].method != http.MethodDelete {
		t.Errorf("requests = %+v", got)
	}
	h = newHarness(t, "advanced-accounts/10")
	h.google.api = func(req *http.Request) (int, string, http.Header) { return 200, `{"accounts":[]}`, nil }
	if _, err := h.call(t, handleProductInputsInsert, `{"account":"99","data_source":"5","content_language":"en","feed_label":"US","offer_id":"x"}`); !isInvalidRequest(err) {
		t.Fatalf("err = %v", err)
	}
	for _, r := range h.google.seen() {
		if r.Method != http.MethodGet {
			t.Errorf("a %s request followed the failed proof", r.Method)
		}
	}
}

func TestProductInputsDeleteSendsOneDeleteWithTheDataSource(t *testing.T) {
	h := newHarness(t)
	h.answerWith(200, `{}`)
	out, err := h.call(t, handleProductInputsDelete, `{`+fixtureIdentity+`}`)
	if err != nil {
		t.Fatal(err)
	}
	got := h.recorded(t)
	if len(got) != 1 || got[0].method != http.MethodDelete || got[0].path != "/products/v1/"+fixtureInputName ||
		got[0].body != "" {
		t.Fatalf("requests = %+v", got)
	}
	if query, _ := url.ParseQuery(got[0].query); len(query) != 1 || query.Get("dataSource") != "accounts/1000001/dataSources/2000002" {
		t.Errorf("query = %v", got[0].query)
	}
	if out["accepted"] != true || out["name"] != fixtureInputName {
		t.Errorf("result = %v", out)
	}
	h = newHarness(t)
	h.answerWith(204, ``)
	if out, err = h.call(t, handleProductInputsDelete, `{`+fixtureIdentity+`}`); err != nil || out["accepted"] != true {
		t.Errorf("204: %v, %v", out, err)
	}
}

func TestProductInputResultsAreCutAndCapped(t *testing.T) {
	var custom []string
	for i := 0; i < 150; i++ {
		custom = append(custom, `{"name":"n","value":"v"}`)
	}
	h := newHarness(t)
	h.answerWith(200, `{"name":"`+strings.Repeat("n", 600)+`","productAttributes":{"title":"`+strings.Repeat("t", 6000)+`",`+
		`"gtins":["1","2"],"additionalImageLinks":["https://img.example.invalid/1"],"shipping":[`+strings.Repeat(`{"country":"US"},`, 400)+`{"country":"US"}]},`+
		`"customAttributes":[`+strings.Join(custom, ",")+`]}`)
	out, err := h.call(t, handleProductInputsInsert, `{`+fixtureIdentity+`}`)
	if err != nil {
		t.Fatal(err)
	}
	shown := out["productInput"].(map[string]any)
	attributes := shown["productAttributes"].(map[string]any)
	if n := len([]rune(shown["name"].(string))); n != maxNameRunes {
		t.Errorf("name has %d runes", n)
	}
	if n := len([]rune(attributes["title"].(string))); n != maxNameRunes {
		t.Errorf("title has %d runes", n)
	}
	if n := len(attributes["shipping"].([]any)); n != maxListItems {
		t.Errorf("shipping has %d entries", n)
	}
	if n := len(shown["customAttributes"].([]any)); n != maxListItems {
		t.Errorf("custom attributes have %d entries", n)
	}
}

func TestProductInputChangesAreNeverRepeatedAndSayWhenTheOutcomeIsOpen(t *testing.T) {
	calls := map[string]struct {
		handler capability.Handler
		args    string
	}{
		"insert": {handleProductInputsInsert, `{` + fixtureIdentity + `}`},
		"update": {handleProductInputsUpdate, `{` + fixtureIdentity + `,"delete_attributes":["pause"]}`},
		"delete": {handleProductInputsDelete, `{` + fixtureIdentity + `}`},
	}
	for name, c := range map[string]struct {
		status int
		class  provider.Class
		open   bool
	}{
		"500": {500, provider.ClassProviderError, true}, "503": {503, provider.ClassUnreachable, true},
		"504": {504, provider.ClassTimeout, true}, "429": {429, provider.ClassRateLimited, false},
		"400": {400, provider.ClassProviderError, false}, "401": {401, provider.ClassAuth, false},
		"403": {403, provider.ClassPermission, false}, "404": {404, provider.ClassNotFound, false},
		"409": {409, provider.ClassProviderError, false},
	} {
		for tool, call := range calls {
			h := newHarness(t)
			h.answerWith(c.status, `{"error":{"code":1,"status":"X","message":"google text","details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"QUOTA_REQUEST_RATE_TOO_HIGH"}]}}`)
			_, err := h.call(t, call.handler, call.args)
			if got := classOf(err); got != c.class {
				t.Errorf("%s %s: class = %q, want %q (%v)", tool, name, got, c.class, err)
			}
			if n := len(h.google.seen()); n != 1 {
				t.Errorf("%s %s: %d requests, a change must go out once", tool, name, n)
			}
			if open := err != nil && strings.Contains(err.Error(), "outcome of the change is unknown"); open != c.open {
				t.Errorf("%s %s: open outcome = %t, want %t (%v)", tool, name, open, c.open, err)
			}
			if err != nil && strings.Contains(err.Error(), "google text") {
				t.Errorf("%s %s: message carries Google's text: %v", tool, name, err)
			}
		}
	}
	for tool, call := range calls {
		h := newHarness(t)
		h.google.fail = io.ErrUnexpectedEOF
		if _, err := h.call(t, call.handler, call.args); err == nil || !strings.Contains(err.Error(), "outcome of the change is unknown") ||
			len(h.google.seen()) != 1 {
			t.Errorf("%s transport failure: err = %v, requests = %d", tool, err, len(h.google.seen()))
		}
	}
	// An answer that is not a JSON object leaves the outcome open too, and a quota answer holds the next call.
	for _, body := range []string{`[1]`, `"text"`, `null`} {
		h := newHarness(t)
		h.answerWith(200, body)
		_, err := h.call(t, handleProductInputsInsert, `{`+fixtureIdentity+`}`)
		if classOf(err) != provider.ClassInvalidResponse || !strings.Contains(err.Error(), "outcome of the change is unknown") {
			t.Errorf("answer %s: err = %v", body, err)
		}
	}
	h := newHarness(t)
	h.answerWith(429, `{"error":{"code":429,"status":"RESOURCE_EXHAUSTED","details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"QUOTA_TOO_MANY_REQUESTS"}]}}`)
	if _, err := h.call(t, handleProductInputsDelete, `{`+fixtureIdentity+`}`); !strings.Contains(err.Error(), "12:00 UTC") {
		t.Errorf("err = %v", err)
	}
	h.answerWith(200, `{}`)
	if _, err := h.call(t, handleProductInputsDelete, `{`+fixtureIdentity+`}`); err != nil || len(h.spacing.recorded()) == 0 {
		t.Errorf("the request after a 429 did not wait: %v", err)
	}
}

func TestA401OnAProductInputChangeDropsTheTokenWithoutRenewingItForTheSameCall(t *testing.T) {
	h := newHarness(t)
	h.answerWith(401, `{"error":{"code":401,"status":"UNAUTHENTICATED"}}`)
	if _, err := h.call(t, handleProductInputsInsert, `{`+fixtureIdentity+`}`); classOf(err) != provider.ClassAuth {
		t.Fatalf("err = %v", err)
	}
	if h.google.grants != 1 || len(h.google.seen()) != 1 {
		t.Errorf("grants = %d, requests = %d, want one each", h.google.grants, len(h.google.seen()))
	}
}

func TestProductInputSummariesNameWhatChanges(t *testing.T) {
	for _, c := range []struct {
		summary func(json.RawMessage) string
		args    string
		want    []string
	}{
		{productInsertSummary, `{` + fixtureIdentity + `,"legacy_local":true}`,
			[]string{"Insert product input", `"sku-example-1"`, "(en, US)", "legacy local", "data source 2000002", "account 1000001",
				"replaced completely", "Google Shopping", "processes it afterwards"}},
		{productUpdateSummary, `{` + fixtureIdentity + `,"attributes":{"title":"A","price":{"currencyCode":"USD"}},` +
			`"delete_attributes":["salePrice"],"custom_attributes":[{"name":"size_type","value":"x"}],"delete_custom_attributes":["old"]}`,
			[]string{"Update product input", `"sku-example-1"`, `set "price, title"`, `remove "salePrice"`, `set custom "size_type"`,
				`remove custom "old"`, "processes it afterwards"}},
		{productDeleteSummary, `{` + fixtureIdentity + `}`,
			[]string{"Delete product input", `"sku-example-1"`, "data source 2000002", "account 1000001", "no way to undo"}},
	} {
		got := c.summary(json.RawMessage(c.args))
		for _, w := range c.want {
			if !strings.Contains(got, w) {
				t.Errorf("summary %q misses %q", got, w)
			}
		}
	}
}

// find follows a dotted path of members through objects and lists.
func find(n *node, path string) *node {
	for _, name := range strings.Split(path, ".") {
		for n != nil && n.kind == nodeList {
			n = n.item
		}
		if n == nil || n.kind != nodeObject {
			return nil
		}
		n = n.field(name)
	}
	for n != nil && n.kind == nodeList {
		n = n.item
	}
	return n
}

func TestConstraintsFitTheTreeTheyAreWrittenFor(t *testing.T) {
	check := func(kind string, paths []string, ok func(*node) bool) {
		for _, path := range paths {
			if n := find(nProductAttributes, path); n == nil || !ok(n) {
				t.Errorf("%s constraint of %s does not fit the tree", kind, path)
			}
		}
	}
	isText := func(n *node) bool { return n.kind == nodeText && n != tURL }
	var enums, ranges, items, runes []string
	for p := range constraints.Enums {
		enums = append(enums, p)
	}
	for p := range constraints.Ranges {
		ranges = append(ranges, p)
	}
	for p := range constraints.MaxItems {
		items = append(items, p)
	}
	for p := range constraints.MaxRunes {
		runes = append(runes, p)
	}
	check("enum", enums, isText)
	check("date and time", constraints.DateTimes, isText)
	check("range", ranges, func(n *node) bool { return n.kind == nodeNumber })
	check("text", runes, isText)
	for _, path := range items {
		if n := find(nProductAttributes, path); n == nil {
			t.Errorf("list bound of %s does not fit the tree", path)
		}
	}
	for path, values := range constraints.Enums {
		if len(values) == 0 || slices.ContainsFunc(values, func(v string) bool { return strings.HasSuffix(v, "_UNSPECIFIED") }) {
			t.Errorf("enum of %s = %v", path, values)
		}
	}
}

func TestEveryAddressMemberOfTheTreeNeedsHTTPS(t *testing.T) {
	var found []string
	var walk func(n *node, path string, depth int)
	walk = func(n *node, path string, depth int) {
		switch {
		case n == tURL:
			found = append(found, path)
		case n.kind == nodeList:
			walk(n.item, path, depth)
		case n.kind == nodeObject && depth < 4:
			for _, fl := range n.fields {
				walk(fl.node, joinPath(path, fl.name), depth+1)
			}
		}
	}
	walk(nProductAttributes, "", 0)
	slices.Sort(found)
	want := []string{"additionalImageLinks", "adsRedirect", "canonicalLink", "certifications.certificationDocumentLink",
		"certifications.certificationLabelLink", "displayAdsLink", "documentLinks", "imageLink", "lifestyleImageLinks",
		"link", "linkTemplate", "mobileLink", "mobileLinkTemplate", "returns.policyUrl", "videoLinks", "virtualModelLink"}
	if !slices.Equal(found, want) {
		t.Fatalf("address members = %v, want %v", found, want)
	}
	for _, path := range found {
		n := find(nProductAttributes, path)
		if err := n.checkInput("http://shop.example.invalid/x", path); !isInvalidRequest(err) {
			t.Errorf("%s accepts http: %v", path, err)
		}
		if err := n.checkInput("https://shop.example.invalid/x", path); err != nil {
			t.Errorf("%s refuses https: %v", path, err)
		}
	}
}
