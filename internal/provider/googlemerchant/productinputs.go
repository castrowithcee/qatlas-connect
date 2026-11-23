package googlemerchant

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	productInputsInsertID = Provider + ".product-inputs.insert"
	productInputsUpdateID = Provider + ".product-inputs.update"
	productInputsDeleteID = Provider + ".product-inputs.delete"
)

// The identity of a product input is stricter than what the read tools accept in a product name: Google
// documents the feed label as A-Z, 0-9, hyphen, and underscore and the language as a two-letter ISO 639-1 code,
// so a write sends only that form.
var (
	productLanguageForm  = regexp.MustCompile(`^[a-z]{2}$`)
	productFeedLabelForm = regexp.MustCompile(`^[A-Z0-9_-]{1,20}$`)
	customNameForm       = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,` + strconv.Itoa(maxCustomNameRunes-1) + `}$`)
)

const (
	productLanguagePattern  = `^[a-z]{2}$`
	productFeedLabelPattern = `^[A-Z0-9_-]{1,20}$`
	customNamePattern       = `^[A-Za-z0-9][A-Za-z0-9_-]{0,99}$`
)

// Prefix of the update mask paths. Google takes the attributes below productAttributes and a custom
// attribute below custom_attribute.
const (
	maskAttributePrefix = "productAttributes."
	maskCustomPrefix    = "custom_attribute."
)

// productIdentity names one product input the way Google does: language, feed label, and offer ID, and
// whether it is a legacy local product.
type productIdentity struct {
	ContentLanguage string `json:"content_language"`
	FeedLabel       string `json:"feed_label"`
	OfferID         string `json:"offer_id"`
	LegacyLocal     bool   `json:"legacy_local"`
}

type productDeleteArguments struct {
	Account    string `json:"account"`
	DataSource string `json:"data_source"`
	productIdentity
}

type productInsertArguments struct {
	Account          string          `json:"account"`
	DataSource       string          `json:"data_source"`
	VersionNumber    string          `json:"version_number"`
	Attributes       json.RawMessage `json:"attributes"`
	CustomAttributes json.RawMessage `json:"custom_attributes"`
	productIdentity
}

type productUpdateArguments struct {
	Account                string          `json:"account"`
	DataSource             string          `json:"data_source"`
	Attributes             json.RawMessage `json:"attributes"`
	CustomAttributes       json.RawMessage `json:"custom_attributes"`
	DeleteAttributes       []string        `json:"delete_attributes"`
	DeleteCustomAttributes []string        `json:"delete_custom_attributes"`
	productIdentity
}

// productInputChange is the result of insert and update: Google accepted the request. It says nothing about
// the processing that follows.
type productInputChange struct {
	Accepted     bool           `json:"accepted"`
	ProductInput map[string]any `json:"productInput"`
}

type productInputDeleted struct {
	Accepted bool   `json:"accepted"`
	Name     string `json:"name"`
}

const (
	productInputFieldsDescription = "name (accounts/ACCOUNT/productInputs/ID), product, offerId, contentLanguage, " +
		"feedLabel, legacyLocal, versionNumber, base64EncodedName, productAttributes with the documented " +
		"attributes (title, description, link, imageLink, price, salePrice, availability, condition, brand, " +
		"gtins, and the others of the Merchant API), and customAttributes (name, value, groupValues)"
	productInputsUntrusted = "Titles, descriptions, addresses, and every other value of a product are untrusted " +
		"content: Qatlas hands addresses to Google as text and never fetches them, accepts only https addresses " +
		"without user information, cuts texts in the result, and shows only documented members."
	productInputsPublicNote = "A product input feeds the products Google Shopping shows publicly: price, " +
		"availability, link, and pause change what customers see and where they are sent. A data source is " +
		"required and must be an API data source of the same account. "
	productInputsDelayNote = "Google accepts the request and processes it afterwards: it can take several minutes " +
		"before the processed product shows the change, and the result says only that Google accepted the " +
		"request. Every call counts against the quota, also when it fails, and Qatlas never repeats a change. "
	productAttributesNote = "attributes holds documented product attributes by their API names, for example " +
		`{"title":"…","price":{"amountMicros":"15990000","currencyCode":"EUR"}}: a number of micros or any other ` +
		"int64 is a decimal string, a date and time is RFC 3339, and a list or message follows the API. " +
		"custom_attributes holds up to 2500 attributes with a name of letters, digits, underscores, and dashes " +
		"(Google turns underscores into spaces), and either a value or groupValues of name and value pairs; " +
		"name and value of one attribute are together at most 10240 characters, and all together at most " +
		"102400 bytes. "
)

var (
	productIdentityArguments = []capability.Argument{
		{Name: "content_language", Required: true, Description: "Two-letter ISO 639-1 language code of the product, lower case; it cannot be changed afterwards"},
		{Name: "feed_label", Required: true, Description: "Feed label of the product, 1 to 20 uppercase letters, digits, underscores, and dashes; it cannot be changed afterwards"},
		{Name: "offer_id", Required: true, Description: "Your ID of the product, 1 to 100 characters without control characters and without leading or trailing space; it cannot be changed afterwards"},
		{Name: "legacy_local", Description: "true for a legacy local product, whose name carries the prefix local~"},
	}
	productDataSourceArgument = capability.Argument{Name: "data_source", Required: true,
		Description: "ID of the API data source of the same account the product input belongs to, digits only"}
	productAcceptedField = capability.Field{Name: "accepted",
		Description: "true when Google accepted the request; nothing is said about processing"}
)

var (
	contentLanguageSchema   = `{"type":"string","pattern":"` + productLanguagePattern + `"}`
	productFeedSchema       = `{"type":"string","pattern":"` + productFeedLabelPattern + `"}`
	offerIDSchema           = `{"type":"string","minLength":1,"maxLength":` + strconv.Itoa(maxOfferIDRunes) + `}`
	versionNumberSchema     = `{"type":"string","pattern":"^[0-9]{1,19}$"}`
	attributeNameSchema     = `{"type":"string","enum":` + quoteList(attributeNames()) + `}`
	productCustomNameSchema = `{"type":"string","pattern":"` + customNamePattern + `"}`
	customAttributesInput   = func() string {
		pair := `{"type":"object","properties":{"name":` + productCustomNameSchema + `,"value":{"type":"string","minLength":1,"maxLength":` +
			strconv.Itoa(maxCustomRunes) + `}},"required":["name","value"],"additionalProperties":false}`
		return `{"type":"array","minItems":1,"maxItems":` + strconv.Itoa(maxCustomAttributes) + `,"items":` +
			`{"type":"object","properties":{"name":` + productCustomNameSchema + `,"value":{"type":"string","minLength":1,"maxLength":` +
			strconv.Itoa(maxCustomRunes) + `},"groupValues":{"type":"array","minItems":1,"maxItems":` +
			strconv.Itoa(maxGroupValues) + `,"items":` + pair + `}},"required":["name"],"additionalProperties":false}}`
	}()
	attributesInput = nProductAttributes.inputSchema("")
	deleteListInput = func(item string) string {
		return `{"type":"array","minItems":1,"maxItems":` + strconv.Itoa(maxCustomAttributes) +
			`,"uniqueItems":true,"items":` + item + `}`
	}
	productChangeSchema = objectSchema(member("accepted", boolSchema),
		member("productInput", productInputNode.outputSchema()))
	productDeletedSchema = objectSchema(member("accepted", boolSchema), member("name", stringSchema))
)

func productIdentityMembers() []string {
	return []string{member("content_language", contentLanguageSchema), member("feed_label", productFeedSchema),
		member("offer_id", offerIDSchema), member("legacy_local", boolSchema)}
}

var productIdentityRequired = []string{"data_source", "content_language", "feed_label", "offer_id"}

var productInputsInsert = capability.Descriptor{
	ID: productInputsInsertID, Version: 1, Title: "Insert a Merchant Center product input",
	Description: "Upload a product input to a Merchant Center account: the attributes and custom attributes of " +
		"one product in one API data source. If a product input with the same content_language, offer_id, and " +
		"data source exists, this call replaces it completely, so every attribute that is not sent is gone; if " +
		"the data source differs, the product moves to the new data source. version_number, an int64 string, " +
		"keeps the order of updates: Google refuses the insert when it is lower than the current version, and " +
		"it is only for primary data sources. " + productInputsPublicNote + productAttributesNote +
		productInputsDelayNote + "Changing products needs a role that may edit product data in Merchant " +
		"Center; Google refuses the call otherwise. The confirmation shows the account, the data source, and " +
		"the product identity. " + productInputsUntrusted,
	Tags: []string{"google", "merchant", "products", "product-inputs"}, Risk: changeRisk(capability.EffectCreate,
		capability.IdempotencyIdempotent), Provider: Provider, RequiresToolAllowList: true,
	InputSchema: inputSchema(productIdentityRequired, append([]string{member("account", accountArgSchema),
		member("data_source", dataSourceIDSchema), member("version_number", versionNumberSchema),
		member("attributes", attributesInput), member("custom_attributes", customAttributesInput)},
		productIdentityMembers()...)...),
	OutputSchema: json.RawMessage(productChangeSchema),
	Arguments: append(append([]capability.Argument{accountArgument, productDataSourceArgument},
		productIdentityArguments...),
		capability.Argument{Name: "version_number", Description: "int64 as a string; Google refuses the insert when it is lower than the current version"},
		capability.Argument{Name: "attributes", Description: "Product attributes by their API names, for example title, description, link, imageLink, price, availability; every address needs https and no user information"},
		capability.Argument{Name: "custom_attributes", Description: "Custom attributes: a list of name with value or groupValues"}),
	Fields: []capability.Field{productAcceptedField,
		{Name: "productInput", Description: "The product input as Google returned it, with " + productInputFieldsDescription}},
	Examples: []capability.Example{{Description: "Insert a product with a title, an address, and a price",
		Arguments: json.RawMessage(`{"account":"1000001","data_source":"2000002","content_language":"en","feed_label":"US",` +
			`"offer_id":"sku-example-1","attributes":{"title":"Example product","link":"https://shop.example.invalid/p/1",` +
			`"availability":"IN_STOCK","price":{"amountMicros":"15990000","currencyCode":"USD"}}}`)}},
}

var productInputsUpdate = capability.Descriptor{
	ID: productInputsUpdateID, Version: 1, Title: "Update a Merchant Center product input",
	Description: "Change attributes of one product input in an API data source of a Merchant Center account. " +
		"Qatlas builds the update mask from the arguments: each member of attributes is set (replacing the " +
		"whole attribute, because the mask names top-level attributes only), each name in delete_attributes " +
		"is removed from the product input, and the same holds for custom_attributes and " +
		"delete_custom_attributes. Everything else stays as it is. At least one of the four is needed, and a " +
		"name cannot be set and removed in one call. Google's own rule is that an attribute in the mask without " +
		"a value is deleted; that is why removal has its own argument. " + productInputsPublicNote +
		productAttributesNote + productInputsDelayNote + "Changing products needs a role that may edit product " +
		"data in Merchant Center; Google refuses the call otherwise. The confirmation shows the account, the " +
		"product identity, and the attributes that are set and removed. " + productInputsUntrusted,
	Tags: []string{"google", "merchant", "products", "product-inputs"}, Risk: changeRisk(capability.EffectUpdate,
		capability.IdempotencyIdempotent), Provider: Provider, RequiresToolAllowList: true,
	InputSchema: inputSchema(productIdentityRequired, append([]string{member("account", accountArgSchema),
		member("data_source", dataSourceIDSchema), member("attributes", attributesInput),
		member("custom_attributes", customAttributesInput),
		member("delete_attributes", deleteListInput(attributeNameSchema)),
		member("delete_custom_attributes", deleteListInput(productCustomNameSchema))},
		productIdentityMembers()...)...),
	OutputSchema: json.RawMessage(productChangeSchema),
	Arguments: append(append([]capability.Argument{accountArgument, productDataSourceArgument},
		productIdentityArguments...),
		capability.Argument{Name: "attributes", Description: "Product attributes to set, by their API names; each replaces the whole attribute"},
		capability.Argument{Name: "custom_attributes", Description: "Custom attributes to set: a list of name with value or groupValues"},
		capability.Argument{Name: "delete_attributes", Description: "Names of product attributes to remove"},
		capability.Argument{Name: "delete_custom_attributes", Description: "Names of custom attributes to remove"}),
	Fields: []capability.Field{productAcceptedField,
		{Name: "productInput", Description: "The product input as Google returned it, with " + productInputFieldsDescription}},
	Examples: []capability.Example{{Description: "Mark a product out of stock and remove its sale price",
		Arguments: json.RawMessage(`{"account":"1000001","data_source":"2000002","content_language":"en","feed_label":"US",` +
			`"offer_id":"sku-example-1","attributes":{"availability":"OUT_OF_STOCK"},"delete_attributes":["salePrice"]}`)}},
}

var productInputsDelete = capability.Descriptor{
	ID: productInputsDeleteID, Version: 1, Title: "Delete a Merchant Center product input",
	Description: "Delete one product input from a data source of a Merchant Center account. The product " +
		"disappears from Google Shopping once Google has processed it, unless another data source still " +
		"holds an input for it, and Qatlas offers no way to undo it. " + productInputsDelayNote +
		"Changing products needs a role that may edit product data in Merchant Center; Google refuses the " +
		"call otherwise. The confirmation shows the account, the data source, and the product identity. " +
		productInputsUntrusted,
	Tags: []string{"google", "merchant", "products", "product-inputs", "delete"}, Risk: changeRisk(capability.EffectDelete,
		capability.IdempotencyIdempotent), Provider: Provider, RequiresToolAllowList: true,
	InputSchema: inputSchema(productIdentityRequired, append([]string{member("account", accountArgSchema),
		member("data_source", dataSourceIDSchema)}, productIdentityMembers()...)...),
	OutputSchema: json.RawMessage(productDeletedSchema),
	Arguments: append([]capability.Argument{accountArgument, productDataSourceArgument},
		productIdentityArguments...),
	Fields: []capability.Field{productAcceptedField,
		{Name: "name", Description: "Name of the product input that was deleted: accounts/ACCOUNT/productInputs/ID"}},
	Examples: []capability.Example{{Description: "Delete one product input",
		Arguments: json.RawMessage(`{"account":"1000001","data_source":"2000002","content_language":"en",` +
			`"feed_label":"US","offer_id":"sku-example-1"}`)}},
}

func productInputDescriptors() []capability.Descriptor {
	return []capability.Descriptor{productInputsInsert, productInputsUpdate, productInputsDelete}
}

func productInputHandlers() map[string]capability.Handler {
	return map[string]capability.Handler{productInputsInsertID: handleProductInputsInsert,
		productInputsUpdateID: handleProductInputsUpdate, productInputsDeleteID: handleProductInputsDelete}
}

func productInputSummaries() map[string]capability.SummaryFunc {
	return map[string]capability.SummaryFunc{productInputsInsertID: productInsertSummary,
		productInputsUpdateID: productUpdateSummary, productInputsDeleteID: productDeleteSummary}
}

func (p productIdentity) check() error {
	switch {
	case !productLanguageForm.MatchString(p.ContentLanguage):
		return invalidArgument("content_language must be a two-letter ISO 639-1 code in lower case")
	case !productFeedLabelForm.MatchString(p.FeedLabel):
		return invalidArgument("feed_label must be 1 to 20 uppercase letters, digits, underscores, and dashes")
	case !validText(p.OfferID, maxOfferIDRunes) || strings.TrimSpace(p.OfferID) != p.OfferID:
		return invalidArgument("offer_id must be 1 to " + strconv.Itoa(maxOfferIDRunes) +
			" characters without control characters and without leading or trailing space")
	}
	return nil
}

// inputName is the resource name Google documents: the unpadded base64url form of
// content_language~feed_label~offer_id, which works for every offer ID, with the local~ prefix for a legacy
// local product.
func (p productIdentity) inputName(account string) string {
	id := p.ContentLanguage + "~" + p.FeedLabel + "~" + p.OfferID
	if p.LegacyLocal {
		id = "local~" + id
	}
	return "accounts/" + account + "/productInputs/" + base64.RawURLEncoding.EncodeToString([]byte(id))
}

func productDataSourceName(account, dataSource string) string {
	return "accounts/" + account + "/dataSources/" + dataSource
}

// attributeObject checks the attributes argument and returns its members.
func attributeObject(raw json.RawMessage) (map[string]any, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	tree, err := decodeTree(raw)
	if err != nil {
		return nil, invalidArgument("attributes could not be read")
	}
	object, ok := tree.(map[string]any)
	if !ok || len(object) == 0 {
		return nil, invalidArgument("attributes must be an object with at least one attribute")
	}
	if err := nProductAttributes.checkInput(object, ""); err != nil {
		return nil, err
	}
	return object, nil
}

// customAttributeList checks the custom attributes against the documented limits and returns the entries and
// their names in order.
func customAttributeList(raw json.RawMessage) ([]map[string]any, []string, error) {
	if len(raw) == 0 {
		return nil, nil, nil
	}
	tree, err := decodeTree(raw)
	list, ok := tree.([]any)
	if err != nil || !ok || len(list) == 0 || len(list) > maxCustomAttributes {
		return nil, nil, invalidArgument("custom_attributes must be a list of 1 to " +
			strconv.Itoa(maxCustomAttributes) + " attributes")
	}
	entries := make([]map[string]any, 0, len(list))
	names := make([]string, 0, len(list))
	seen := map[string]bool{}
	size := 0
	for _, item := range list {
		entry, ok := item.(map[string]any)
		name, _ := entry["name"].(string)
		if !ok || !customNameForm.MatchString(name) || seen[name] {
			return nil, nil, invalidArgument("every custom attribute needs a distinct name of letters, digits, " +
				"underscores, and dashes, at most " + strconv.Itoa(maxCustomNameRunes) + " characters")
		}
		seen[name] = true
		value, hasValue := entry["value"].(string)
		groups, hasGroups := entry["groupValues"].([]any)
		if len(entry) > 3 || hasValue == hasGroups || (hasGroups && len(groups) == 0) || (hasValue && !productText(value, maxCustomRunes)) {
			return nil, nil, invalidArgument("every custom attribute needs exactly one of a value and groupValues")
		}
		used := utf8.RuneCountInString(name)
		if hasValue {
			used += utf8.RuneCountInString(value)
		}
		for _, g := range groups {
			pair, ok := g.(map[string]any)
			gName, _ := pair["name"].(string)
			gValue, _ := pair["value"].(string)
			if !ok || len(pair) != 2 || !customNameForm.MatchString(gName) || !productText(gValue, maxCustomRunes) ||
				len(groups) > maxGroupValues {
				return nil, nil, invalidArgument("groupValues need 1 to " + strconv.Itoa(maxGroupValues) +
					" pairs of a valid name and a value")
			}
			used += utf8.RuneCountInString(gName) + utf8.RuneCountInString(gValue)
		}
		if used > maxCustomRunes {
			return nil, nil, invalidArgument("name and value of a custom attribute must be together at most " +
				strconv.Itoa(maxCustomRunes) + " characters")
		}
		size += len(name) + len(value)
		for _, g := range groups {
			pair, _ := g.(map[string]any)
			gName, _ := pair["name"].(string)
			gValue, _ := pair["value"].(string)
			size += len(gName) + len(gValue)
		}
		entries = append(entries, entry)
		names = append(names, name)
	}
	if size > maxCustomBytes {
		return nil, nil, invalidArgument("custom_attributes must be together at most " + strconv.Itoa(maxCustomBytes) + " bytes")
	}
	return entries, names, nil
}

func checkNames(list []string, known func(string) bool, what string) error {
	seen := map[string]bool{}
	for _, name := range list {
		if !known(name) || seen[name] {
			return invalidArgument(what + " must hold distinct documented names")
		}
		seen[name] = true
	}
	return nil
}

func sortedMembers(object map[string]any) []string {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// insertBody builds the body of an insert from checked arguments only.
func insertBody(a productInsertArguments) (map[string]any, error) {
	if err := a.productIdentity.check(); err != nil {
		return nil, err
	}
	body := map[string]any{"offerId": a.OfferID, "contentLanguage": a.ContentLanguage, "feedLabel": a.FeedLabel}
	if a.LegacyLocal {
		body["legacyLocal"] = true
	}
	if a.VersionNumber != "" {
		if _, err := strconv.ParseUint(a.VersionNumber, 10, 63); err != nil || len(a.VersionNumber) > 19 {
			return nil, invalidArgument("version_number must be a decimal integer of at most 19 digits as a string")
		}
		body["versionNumber"] = a.VersionNumber
	}
	attributes, err := attributeObject(a.Attributes)
	if err != nil {
		return nil, err
	}
	if attributes != nil {
		body["productAttributes"] = attributes
	}
	custom, _, err := customAttributeList(a.CustomAttributes)
	if err != nil {
		return nil, err
	}
	if custom != nil {
		body["customAttributes"] = custom
	}
	return body, checkBodySize(body)
}

func checkBodySize(body map[string]any) error {
	encoded, err := json.Marshal(body)
	if err != nil || len(encoded) > maxProductBody {
		return invalidArgument("the product input is larger than " + strconv.Itoa(maxProductBody) + " bytes")
	}
	return nil
}

// productUpdateRequest builds the update mask and the body from the members that are set. The mask holds only the
// names of documented attributes and of the custom attributes sent or named for removal, so it can never
// hold "*" or a path below an attribute.
func productUpdateRequest(account string, a productUpdateArguments) (mask []string, body map[string]any, err error) {
	if err := a.productIdentity.check(); err != nil {
		return nil, nil, err
	}
	attributes, err := attributeObject(a.Attributes)
	if err != nil {
		return nil, nil, err
	}
	custom, customNames, err := customAttributeList(a.CustomAttributes)
	if err != nil {
		return nil, nil, err
	}
	if err := checkNames(a.DeleteAttributes, isAttributeName, "delete_attributes"); err != nil {
		return nil, nil, err
	}
	if err := checkNames(a.DeleteCustomAttributes, customNameForm.MatchString, "delete_custom_attributes"); err != nil {
		return nil, nil, err
	}
	body = map[string]any{"name": a.inputName(account)}
	for _, name := range sortedMembers(attributes) {
		mask = append(mask, maskAttributePrefix+name)
	}
	for _, name := range a.DeleteAttributes {
		if _, both := attributes[name]; both {
			return nil, nil, invalidArgument("an attribute cannot be set and removed in one call")
		}
		mask = append(mask, maskAttributePrefix+name)
	}
	for _, name := range customNames {
		mask = append(mask, maskCustomPrefix+name)
	}
	for _, name := range a.DeleteCustomAttributes {
		for _, set := range customNames {
			if set == name {
				return nil, nil, invalidArgument("a custom attribute cannot be set and removed in one call")
			}
		}
		mask = append(mask, maskCustomPrefix+name)
	}
	if len(mask) == 0 {
		return nil, nil, invalidArgument("set at least one of attributes, custom_attributes, delete_attributes, " +
			"or delete_custom_attributes")
	}
	if attributes != nil {
		body["productAttributes"] = attributes
	}
	if custom != nil {
		body["customAttributes"] = custom
	}
	return mask, body, checkBodySize(body)
}

// productInputResult reads the answer of a change down to the documented members.
func productInputResult(answer objectAnswer) productInputChange {
	return productInputChange{Accepted: true, ProductInput: cleanObject(productInputNode, json.RawMessage(answer))}
}

func (cl *client) changeProductInput(ctx context.Context, op, tool, method, path string, query url.Values,
	body map[string]any) (productInputChange, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return productInputChange{}, invalidArgument("the arguments could not be encoded")
	}
	var answer objectAnswer
	if err := cl.send(ctx, op, tool, request{method: method, path: path, query: query, body: encoded, change: true},
		&answer); err != nil {
		return productInputChange{}, err
	}
	return productInputResult(answer), nil
}

// productScope is the checked account of a product tool and the proof read it may need.
func productScope(op string, resolved *config.Resolved, raw json.RawMessage, into any,
	ids func() (account, dataSource string)) (id string, needsProof bool, err error) {
	bound, err := bind(op, resolved, raw, into)
	if err != nil {
		return "", false, err
	}
	account, dataSource := ids()
	id, needsProof, err = bound.accountOf(account)
	if err != nil {
		return "", false, err
	}
	if !validID(dataSource) {
		return "", false, invalidArgument("data_source must be a decimal data source ID")
	}
	return id, needsProof, nil
}

func handleProductInputsInsert(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "insert product input"
	var args productInsertArguments
	id, needsProof, err := productScope(op, resolved, raw, &args, func() (string, string) { return args.Account, args.DataSource })
	if err != nil {
		return nil, err
	}
	body, err := insertBody(args)
	if err != nil {
		return nil, err
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := cl.settle(ctx, op, productInputsInsertID, needsProof, id); err != nil {
		return nil, err
	}
	return cl.changeProductInput(ctx, op, productInputsInsertID, http.MethodPost,
		productsAPI+"accounts/"+id+"/productInputs:insert",
		url.Values{"dataSource": {productDataSourceName(id, args.DataSource)}}, body)
}

func handleProductInputsUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "update product input"
	var args productUpdateArguments
	id, needsProof, err := productScope(op, resolved, raw, &args, func() (string, string) { return args.Account, args.DataSource })
	if err != nil {
		return nil, err
	}
	mask, body, err := productUpdateRequest(id, args)
	if err != nil {
		return nil, err
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := cl.settle(ctx, op, productInputsUpdateID, needsProof, id); err != nil {
		return nil, err
	}
	return cl.changeProductInput(ctx, op, productInputsUpdateID, http.MethodPatch,
		productsAPI+args.inputName(id), url.Values{"updateMask": {strings.Join(mask, ",")},
			"dataSource": {productDataSourceName(id, args.DataSource)}}, body)
}

func handleProductInputsDelete(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "delete product input"
	var args productDeleteArguments
	id, needsProof, err := productScope(op, resolved, raw, &args, func() (string, string) { return args.Account, args.DataSource })
	if err != nil {
		return nil, err
	}
	if err := args.productIdentity.check(); err != nil {
		return nil, err
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := cl.settle(ctx, op, productInputsDeleteID, needsProof, id); err != nil {
		return nil, err
	}
	name := args.inputName(id)
	if err := cl.send(ctx, op, productInputsDeleteID, request{method: http.MethodDelete, path: productsAPI + name,
		query: url.Values{"dataSource": {productDataSourceName(id, args.DataSource)}}, change: true}, nil); err != nil {
		return nil, err
	}
	return productInputDeleted{Accepted: true, Name: name}, nil
}

func productTarget(a productIdentity, dataSource, account string) string {
	out := " " + strconv.Quote(a.OfferID) + " (" + a.ContentLanguage + ", " + a.FeedLabel + ")"
	if a.LegacyLocal {
		out += " as a legacy local product"
	}
	out += " in data source " + dataSource
	if account != "" {
		out += " of account " + account
	}
	return out
}

func productInsertSummary(raw json.RawMessage) string {
	var a productInsertArguments
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	return "Insert product input" + productTarget(a.productIdentity, a.DataSource, a.Account) +
		"; an existing input with the same language, offer ID, and data source is replaced completely, the " +
		"product can show in Google Shopping, and Google processes it afterwards"
}

func productUpdateSummary(raw json.RawMessage) string {
	var a productUpdateArguments
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	var set, custom []string
	if tree, err := decodeTree(a.Attributes); err == nil {
		if object, ok := tree.(map[string]any); ok {
			set = sortedMembers(object)
		}
	}
	if tree, err := decodeTree(a.CustomAttributes); err == nil {
		if list, ok := tree.([]any); ok {
			for _, item := range list {
				if entry, ok := item.(map[string]any); ok {
					if name, ok := entry["name"].(string); ok {
						custom = append(custom, name)
					}
				}
			}
		}
	}
	parts := []string{}
	for _, p := range []struct {
		verb  string
		names []string
	}{{"set", set}, {"remove", a.DeleteAttributes}, {"set custom", custom}, {"remove custom", a.DeleteCustomAttributes}} {
		if len(p.names) > 0 {
			parts = append(parts, p.verb+" "+strconv.Quote(strings.Join(p.names, ", ")))
		}
	}
	return "Update product input" + productTarget(a.productIdentity, a.DataSource, a.Account) + ": " +
		strings.Join(parts, "; ") + "; Google processes it afterwards"
}

func productDeleteSummary(raw json.RawMessage) string {
	var a productDeleteArguments
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	return "Delete product input" + productTarget(a.productIdentity, a.DataSource, a.Account) +
		"; the product leaves Google Shopping once Google has processed it, and Qatlas offers no way to undo it"
}
