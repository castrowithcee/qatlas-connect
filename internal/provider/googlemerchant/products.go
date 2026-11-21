package googlemerchant

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	productsListID = Provider + ".products.list"
	productsGetID  = Provider + ".products.get"
)

// productsAPI is the path prefix of the products sub-API.
const productsAPI = "products/v1/"

// Google sends 25 products when pageSize is left out and accepts up to 1000. A product carries up to 145
// attributes, so a page of 1000 can pass the size of an answer this provider reads: the tool stops at 100.
const maxProductsPage = 100

// The form of a product name. The segment after products/ is content_language~feed_label~offer_id, or
// base64url of that string without padding, which Google requires whenever the offer ID holds a character such
// as /, %, or ~. A name of a legacy local product has the prefix local~. Qatlas puts only a segment into the
// path that cannot leave it: the plain form allows no character outside the ones below, and the encoded form
// is base64url, whose alphabet holds none of / ? # % ~ :.
var (
	languageForm   = regexp.MustCompile(`^[A-Za-z]{2}$`)
	feedLabelForm  = regexp.MustCompile(`^[A-Za-z0-9_-]{1,20}$`)
	plainOfferForm = regexp.MustCompile(`^[A-Za-z0-9._-]{1,200}$`)
	encodedForm    = regexp.MustCompile(`^[A-Za-z0-9_-]{4,400}$`)
)

const (
	maxOfferRunes = 200
	localPrefix   = "local~"
)

const productRule = "product must be content_language~feed_label~offer_id, with letters, digits, dot, underscore, and " +
	"hyphen in the offer ID, or the unpadded base64url form of that string (the last part of base64EncodedName)"

// productSegment checks the product argument against the documented forms and returns the path segment. The
// segment is the argument itself: nothing is decoded into the path.
func productSegment(given string) (string, error) {
	switch {
	case strings.Contains(given, "~"):
		rest := strings.TrimPrefix(given, localPrefix)
		parts := strings.Split(rest, "~")
		if len(parts) != 3 || !languageForm.MatchString(parts[0]) || !feedLabelForm.MatchString(parts[1]) ||
			!plainOfferForm.MatchString(parts[2]) {
			return "", invalidArgument(productRule)
		}
		return given, nil
	case encodedForm.MatchString(given):
		decoded, err := base64.RawURLEncoding.Strict().DecodeString(given)
		if err != nil || !utf8.Valid(decoded) {
			return "", invalidArgument(productRule)
		}
		rest := strings.TrimPrefix(string(decoded), localPrefix)
		parts := strings.SplitN(rest, "~", 3)
		if len(parts) != 3 || !languageForm.MatchString(parts[0]) || !feedLabelForm.MatchString(parts[1]) ||
			parts[2] == "" || utf8.RuneCountInString(parts[2]) > maxOfferRunes ||
			strings.IndexFunc(parts[2], unicode.IsControl) >= 0 {
			return "", invalidArgument(productRule)
		}
		return given, nil
	}
	return "", invalidArgument(productRule)
}

// nCustomAttribute holds a member of a group once more inside itself, which maxDepth ends.
var nCustomAttribute = obj(f("name", tName), f("value", tLong))

func init() {
	nCustomAttribute.fields = append(nCustomAttribute.fields, f("groupValues", list(nCustomAttribute)))
}

var (
	nProductItemIssue = obj(
		f("applicableCountries", list(tID)), f("attribute", tName), f("code", tName), f("description", tLong),
		f("detail", tLong), f("documentation", tURL), f("reportingContext", tID), f("resolution", tID),
		f("severity", tID))
	nDestinationStatus = obj(
		f("approvedCountries", list(tID)), f("disapprovedCountries", list(tID)), f("pendingCountries", list(tID)),
		f("reportingContext", tID))
	nProductStatus = obj(
		f("creationDate", tID), f("destinationStatuses", list(nDestinationStatus)), f("googleExpirationDate", tID),
		f("itemLevelIssues", list(nProductItemIssue)), f("lastUpdateDate", tID))
	nAutomatedDiscounts = obj(f("gadPrice", nPrice), f("priorPrice", nPrice), f("priorPriceProgressive", nPrice))
	nProduct            = obj(
		f("archived", tBool), f("automatedDiscounts", nAutomatedDiscounts), f("base64EncodedName", tName),
		f("contentLanguage", tID), f("customAttributes", list(nCustomAttribute)), f("dataSource", tName),
		f("feedLabel", tName), f("legacyLocal", tBool), f("name", tName), f("offerId", tName),
		f("productAttributes", nProductAttributes), f("productStatus", nProductStatus), f("versionNumber", tInt))
)

const (
	productFieldsDescription = "name (accounts/ACCOUNT/products/PRODUCT), offerId, contentLanguage, feedLabel, " +
		"dataSource, versionNumber, legacyLocal, archived, base64EncodedName (the form of the name that holds any offer " +
		"ID), automatedDiscounts (priorPrice, gadPrice, priorPriceProgressive), customAttributes (name, value, " +
		"groupValues), productStatus (creationDate, lastUpdateDate, googleExpirationDate, itemLevelIssues, " +
		"destinationStatuses), and productAttributes with all 145 members the Merchant API documents, among them " +
		"title, description, link, imageLink, adsRedirect, price, salePrice, availability, gtins, brand, " +
		"condition, shipping, returns, and the vehicle and property attributes; the attributes keep the shape " +
		"Google documents, prices as currencyCode and amountMicros"
	productsDelayNote = "The result is the processed product, as Merchant Center shows it after rules and supplemental " +
		"data sources: a product that was just inserted or changed shows in it only after several minutes."
	productsUntrustedNote = "Titles, descriptions, and every other text of a product come from whoever runs the " +
		"account and are untrusted content: texts are cut (links at 2048 characters, descriptions at 1000, other " +
		"texts at 300, codes at 100), lists keep at most 100 entries, and link, imageLink, adsRedirect, and every " +
		"other address are text that is never fetched. Members the API marks deprecated and base64EncodedProduct " +
		"are not part of the result."
)

var productsList = capability.Descriptor{
	ID: productsListID, Version: 1, Title: "List the products of a Merchant Center account",
	Description: "List the processed products of a Merchant Center account, a page per call with a cursor to read " +
		"on. The API has no filter. " + productsDelayNote + " Google sends 25 products when page_size is left out; " +
		"the tool takes 1 to 100, below the 1000 Google allows, because each product carries all its attributes. " +
		"Every product has productAttributes with all 145 documented members. " + productsUntrustedNote,
	Tags: []string{"google", "merchant", "products"}, Risk: readRisk, Provider: Provider,
	InputSchema: inputSchema(nil, member("account", accountArgSchema), member("page_size", productsPageSchema),
		member("cursor", cursorSchema)),
	OutputSchema: json.RawMessage(listResultSchema("products", nProduct.outputSchema())),
	Arguments: []capability.Argument{accountArgument,
		{Name: "page_size", Description: "Products per page, 1 to 100; Google sends 25 when left out"}, cursorArgument},
	Fields: []capability.Field{
		{Name: "products", Description: "Products with " + productFieldsDescription},
		{Name: "next_cursor", Description: "Cursor to read on with; missing at the end"},
	},
	Examples: []capability.Example{{Description: "First page of products", Arguments: json.RawMessage(`{"account":"1000001"}`)}},
}

var productsGet = capability.Descriptor{
	ID: productsGetID, Version: 1, Title: "Get a product of a Merchant Center account",
	Description: "Read one processed product of a Merchant Center account by its name. product is the last part of " +
		"the product name: content_language~feed_label~offer_id such as en~US~sku123, with letters, digits, dot, " +
		"underscore, and hyphen in the offer ID, or the unpadded base64url form of that string, which an offer " +
		"ID with any other character needs (the last part of base64EncodedName in a product of products.list). " +
		"A legacy local product has the prefix local~ before the content language. The path is built from the " +
		"checked account and product, never from free input. The product has productAttributes with all 145 " +
		"documented members. " + productsDelayNote + " " + productsUntrustedNote,
	Tags: []string{"google", "merchant", "products"}, Risk: readRisk, Provider: Provider,
	InputSchema:  inputSchema([]string{"product"}, member("account", accountArgSchema), member("product", productArgSchema)),
	OutputSchema: json.RawMessage(nProduct.outputSchema()),
	Arguments: []capability.Argument{accountArgument,
		{Name: "product", Required: true, Description: "Product name part: content_language~feed_label~offer_id, or its " +
			"unpadded base64url form"}},
	Fields: []capability.Field{{Name: "product", Description: productFieldsDescription}},
	Examples: []capability.Example{{Description: "One product",
		Arguments: json.RawMessage(`{"account":"1000001","product":"en~US~sku-123"}`)}},
}

const (
	productsPageSchema = `{"type":"integer","minimum":1,"maximum":100}`
	productArgSchema   = `{"type":"string","minLength":4,"maxLength":400,` +
		`"x-form":"content_language~feed_label~offer_id, or its unpadded base64url form"}`
)

type productsArguments struct {
	Account  string `json:"account"`
	Product  string `json:"product"`
	PageSize int    `json:"page_size"`
	Cursor   string `json:"cursor"`
}

// settle checks the connection and the account argument of a call before any secret is read, and returns the
// binding and the account with the proof it still needs.
func settle(op string, resolved *config.Resolved, raw json.RawMessage, args any, given func() string) (id string,
	needsProof bool, sc scope, err error) {
	if err := json.Unmarshal(raw, args); err != nil {
		return "", false, scope{}, provider.Fail(op, "the validated arguments could not be read")
	}
	if sc, err = checkConnection(resolved); err != nil {
		return "", false, scope{}, err
	}
	id, needsProof, err = sc.accountOf(given())
	return id, needsProof, sc, err
}

// openFor opens the client of a call and, for an account that no target names, reads the proof.
func openFor(ctx context.Context, op, tool, id string, needsProof bool, resolved *config.Resolved,
	secrets *secret.Resolver, red *redact.Redactor) (*client, error) {
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if needsProof {
		if err := cl.prove(ctx, op, tool, id); err != nil {
			return nil, err
		}
	}
	return cl, nil
}

// pageQuery builds the paging part of a request. The cursor must come from this tool with the same
// arguments, which the binding ties it to.
func pageQuery(binding []byte, pageSize int, cursor string) (url.Values, error) {
	query := url.Values{}
	if pageSize > 0 {
		query.Set("pageSize", strconv.Itoa(pageSize))
	}
	if cursor != "" {
		token, ok := provider.DecodeCursor(binding, cursor, maxCursorSize)
		if !ok || token == "" {
			return nil, refusedCursor()
		}
		query.Set("pageToken", token)
	}
	return query, nil
}

type productsResult struct {
	Products   []map[string]any `json:"products"`
	NextCursor string           `json:"next_cursor,omitempty"`
}

func handleProductsList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage) (any, error) {
	const op = "list products"
	var args productsArguments
	id, needsProof, sc, err := settle(op, resolved, raw, &args, func() string { return args.Account })
	if err != nil {
		return nil, err
	}
	if args.PageSize < 0 || args.PageSize > maxProductsPage {
		return nil, invalidArgument("page_size must be 1 to " + strconv.Itoa(maxProductsPage))
	}
	binding := provider.CursorBinding(productsListID, id, args.PageSize, sc.key())
	query, err := pageQuery(binding, args.PageSize, args.Cursor)
	if err != nil {
		return nil, err
	}
	cl, err := openFor(ctx, op, productsListID, id, needsProof, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var answer struct {
		Products      []json.RawMessage `json:"products"`
		NextPageToken string            `json:"nextPageToken"`
	}
	if err := cl.get(ctx, op, productsListID, productsAPI+"accounts/"+id+"/products", query, &answer); err != nil {
		return nil, err
	}
	if len(answer.Products) > maxEntries {
		return nil, tooMany(op)
	}
	out := productsResult{Products: cleanObjects(nProduct, answer.Products)}
	if out.NextCursor, err = nextCursor(op, binding, answer.NextPageToken); err != nil {
		return nil, err
	}
	return out, nil
}

func handleProductsGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage) (any, error) {
	const op = "get product"
	var args productsArguments
	id, needsProof, _, err := settle(op, resolved, raw, &args, func() string { return args.Account })
	if err != nil {
		return nil, err
	}
	segment, err := productSegment(args.Product)
	if err != nil {
		return nil, err
	}
	cl, err := openFor(ctx, op, productsGetID, id, needsProof, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var answer json.RawMessage
	if err := cl.get(ctx, op, productsGetID, productsAPI+"accounts/"+id+"/products/"+segment, nil, &answer); err != nil {
		return nil, err
	}
	return cleanObject(nProduct, answer), nil
}
