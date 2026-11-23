package googlemerchant

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// Bounds of the arguments. Google documents none for the display name; the cap is Qatlas'.
const (
	maxDisplayNameRunes = 200
	maxCountries        = 250
	maxDestinations     = 20
	maxRuleEntries      = 50
)

// A data source takes a narrower feed label and language than a product or an issue query.
const (
	sourceFeedLabelPattern = `^[A-Z0-9-]{1,20}$`
	sourceLanguagePattern  = `^[a-z]{2}$`
)

var (
	sourceFeedLabelForm = regexp.MustCompile(sourceFeedLabelPattern)
	sourceLanguageForm  = regexp.MustCompile(sourceLanguagePattern)
)

// destinationNames are the marketing methods of the discovery document, without the unspecified value.
var destinationNames = []string{"SHOPPING_ADS", "DISPLAY_ADS", "LOCAL_INVENTORY_ADS", "FREE_LISTINGS",
	"FREE_LOCAL_LISTINGS", "YOUTUBE_SHOPPING", "YOUTUBE_SHOPPING_CHECKOUT", "YOUTUBE_AFFILIATE",
	"FREE_VEHICLE_LISTINGS", "VEHICLE_ADS", "CLOUD_RETAIL", "LOCAL_CLOUD_RETAIL", "RENTAL_ADS"}

var destinationStates = []string{"ENABLED", "DISABLED"}

// The members of the arguments are those of the sources an API can create. A file source, its fetch settings,
// and a Google Sheets source have no member at all, and local inventory sources exist only as file sources.
type (
	destinationInput struct {
		Destination string `json:"destination"`
		State       string `json:"state"`
	}
	referenceInput struct {
		Self                     bool   `json:"self"`
		SupplementalDataSourceID string `json:"supplementalDataSourceId"`
	}
	defaultRuleInput struct {
		TakeFromDataSources []referenceInput `json:"takeFromDataSources"`
	}
	primaryInput struct {
		FeedLabel       string             `json:"feedLabel"`
		ContentLanguage string             `json:"contentLanguage"`
		Countries       []string           `json:"countries"`
		Destinations    []destinationInput `json:"destinations"`
		LegacyLocal     bool               `json:"legacyLocal"`
		DefaultRule     *defaultRuleInput  `json:"defaultRule"`
	}
	supplementalInput struct {
		FeedLabel       string `json:"feedLabel"`
		ContentLanguage string `json:"contentLanguage"`
	}
	regionalInput struct {
		FeedLabel       string `json:"feedLabel"`
		ContentLanguage string `json:"contentLanguage"`
	}
	promotionInput struct {
		TargetCountry   string `json:"targetCountry"`
		ContentLanguage string `json:"contentLanguage"`
	}
)

type createArguments struct {
	Account             string             `json:"account"`
	DisplayName         string             `json:"display_name"`
	PrimaryProduct      *primaryInput      `json:"primary_product"`
	SupplementalProduct *supplementalInput `json:"supplemental_product"`
	RegionalInventory   *regionalInput     `json:"regional_inventory"`
	Promotion           *promotionInput    `json:"promotion"`
	ProductReview       *emptyBlock        `json:"product_review"`
	MerchantReview      *emptyBlock        `json:"merchant_review"`
}

// updateArguments name what an update may change: the members that are not immutable and that belong to no
// file source. The update mask is built from the members that are set, never taken from the caller.
type updateArguments struct {
	Account      string             `json:"account"`
	DataSource   string             `json:"data_source"`
	DisplayName  *string            `json:"display_name"`
	Countries    []string           `json:"countries"`
	Destinations []destinationInput `json:"destinations"`
	DefaultRule  *defaultRuleInput  `json:"default_rule"`
}

// The update mask paths, one per argument. This is the only place a path of the mask is spelled.
const (
	maskDisplayName  = "displayName"
	maskCountries    = "primaryProductDataSource.countries"
	maskDestinations = "primaryProductDataSource.destinations"
	maskDefaultRule  = "primaryProductDataSource.defaultRule"
)

func quoteList(names []string) string {
	encoded, _ := json.Marshal(names)
	return string(encoded)
}

func closedSchema(required []string, members ...string) string {
	return string(inputSchema(required, members...))
}

var (
	displayNameSchema    = `{"type":"string","minLength":1,"maxLength":` + strconv.Itoa(maxDisplayNameRunes) + `}`
	feedLabelSchema      = `{"type":"string","pattern":"` + sourceFeedLabelPattern + `"}`
	sourceLanguageSchema = `{"type":"string","pattern":"` + sourceLanguagePattern + `"}`
	countriesSchema      = `{"type":"array","minItems":1,"maxItems":` + strconv.Itoa(maxCountries) +
		`,"uniqueItems":true,"items":{"type":"string","pattern":"` + countryPattern + `"}}`
	destinationsSchema = `{"type":"array","minItems":1,"maxItems":` + strconv.Itoa(maxDestinations) + `,"items":` +
		closedSchema([]string{"destination"},
			member("destination", `{"type":"string","enum":`+quoteList(destinationNames)+`}`),
			member("state", `{"type":"string","enum":`+quoteList(destinationStates)+`}`)) + `}`
	defaultRuleSchema = closedSchema([]string{"takeFromDataSources"},
		member("takeFromDataSources", `{"type":"array","minItems":1,"maxItems":`+strconv.Itoa(maxRuleEntries)+
			`,"items":`+closedSchema(nil, member("self", `{"type":"boolean"}`),
			member("supplementalDataSourceId", dataSourceIDSchema))+`}`))
	primarySchema = closedSchema(nil, member("feedLabel", feedLabelSchema), member("contentLanguage", sourceLanguageSchema),
		member("countries", countriesSchema), member("destinations", destinationsSchema),
		member("legacyLocal", `{"type":"boolean"}`), member("defaultRule", defaultRuleSchema))
	supplementalSchema = closedSchema(nil, member("feedLabel", feedLabelSchema),
		member("contentLanguage", sourceLanguageSchema))
	regionalSchema = closedSchema([]string{"feedLabel", "contentLanguage"}, member("feedLabel", feedLabelSchema),
		member("contentLanguage", sourceLanguageSchema))
	promotionSchema = closedSchema([]string{"targetCountry", "contentLanguage"},
		member("targetCountry", `{"type":"string","pattern":"`+countryPattern+`"}`),
		member("contentLanguage", sourceLanguageSchema))
	emptySchema = closedSchema(nil)
)

const sourceBlocksNote = "Exactly one source block says what kind of source it is: primary_product, " +
	"supplemental_product, regional_inventory, promotion, product_review, or merchant_review. feedLabel is at " +
	"most 20 uppercase letters, digits, and dashes, contentLanguage a two-letter ISO 639-1 code, and a country " +
	"a two-letter CLDR territory code; feedLabel, contentLanguage, and the target country cannot be changed " +
	"afterwards. For a primary or supplemental source, feedLabel and contentLanguage are set together or not " +
	"at all. defaultRule links the sources the primary source takes its data from, each entry either self or " +
	"the ID of a supplemental data source of the same account, and replaces the whole default rule. "

const apiOnlyNote = "Only API data sources can be handled: file input, fetch settings with an address, user " +
	"name, and password, and Google Sheets sources have no argument and are refused before any secret is read " +
	"or request is sent, and local inventory sources exist only as file sources. "

var dataSourcesCreate = capability.Descriptor{
	ID: dataSourcesCreateID, Version: 1, Title: "Create a Merchant Center API data source",
	Description: "Create a new API data source in a Merchant Center account. " + apiOnlyNote + sourceBlocksNote +
		"Every call creates another data source, so a repeated call duplicates it. A data source feeds the " +
		"products and offers that Google Shopping can show: the tool creates only the container, no product. " +
		dataSourcesProcessingNote + " The result says only that Google accepted the request, and the confirmation " +
		"shows the account, the display name, and the kind of the source. " + dataSourcesUntrusted,
	Tags: []string{"google", "merchant", "data-sources"}, Risk: changeRisk(capability.EffectCreate,
		capability.IdempotencyNonIdempotent), Provider: Provider,
	InputSchema: inputSchema([]string{"display_name"}, member("account", accountArgSchema),
		member("display_name", displayNameSchema), member("primary_product", primarySchema),
		member("supplemental_product", supplementalSchema), member("regional_inventory", regionalSchema),
		member("promotion", promotionSchema), member("product_review", emptySchema),
		member("merchant_review", emptySchema)),
	OutputSchema: json.RawMessage(acceptedSchema),
	Arguments: []capability.Argument{accountArgument,
		{Name: "display_name", Required: true, Description: "Name shown in Merchant Center, 1 to 200 characters"},
		{Name: "primary_product", Description: "Primary product source: feedLabel, contentLanguage, countries " +
			"(two-letter codes), destinations (destination and state), legacyLocal, defaultRule"},
		{Name: "supplemental_product", Description: "Supplemental product source: feedLabel, contentLanguage"},
		{Name: "regional_inventory", Description: "Regional inventory source: feedLabel and contentLanguage, both needed"},
		{Name: "promotion", Description: "Promotion source: targetCountry and contentLanguage, both needed"},
		{Name: "product_review", Description: "Product review source: an empty object"},
		{Name: "merchant_review", Description: "Merchant review source: an empty object"}},
	Fields: []capability.Field{
		{Name: "accepted", Description: "true when Google accepted the request; nothing is said about processing"},
		{Name: "dataSource", Description: "The data source as Google returned it, with " + dataSourceFieldsDescription},
	},
	Examples: []capability.Example{{Description: "A primary product source for one country",
		Arguments: json.RawMessage(`{"account":"1000001","display_name":"Main products","primary_product":` +
			`{"feedLabel":"US","contentLanguage":"en","countries":["US"]}}`)}},
}

var dataSourcesUpdate = capability.Descriptor{
	ID: dataSourcesUpdateID, Version: 1, Title: "Update a Merchant Center API data source",
	Description: "Change members of a data source of a Merchant Center account: display_name, and for a primary " +
		"product source countries, destinations, and default_rule. " + apiOnlyNote + "Qatlas builds the update " +
		"mask from the arguments that are set, so only these members can change and each one needs a value: " +
		"nothing is cleared. Each list replaces the list on the data source, and default_rule replaces the whole " +
		"default rule (it is not an addition). feedLabel, contentLanguage, legacyLocal, and the target country are " +
		"immutable and have no argument. default_rule entries are either self or the ID of a supplemental data " +
		"source of the same account. " + dataSourcesProcessingNote + " The result says only that Google accepted " +
		"the request, and the confirmation shows the account, the data source, and the members. " + dataSourcesUntrusted,
	Tags: []string{"google", "merchant", "data-sources"}, Risk: changeRisk(capability.EffectUpdate,
		capability.IdempotencyIdempotent), Provider: Provider,
	InputSchema: inputSchema([]string{"data_source"}, member("account", accountArgSchema),
		member("data_source", dataSourceIDSchema), member("display_name", displayNameSchema),
		member("countries", countriesSchema), member("destinations", destinationsSchema),
		member("default_rule", defaultRuleSchema)),
	OutputSchema: json.RawMessage(acceptedSchema),
	Arguments: []capability.Argument{accountArgument, dataSourceIDArgument,
		{Name: "display_name", Description: "New name shown in Merchant Center, 1 to 200 characters"},
		{Name: "countries", Description: "Countries of a primary product source as two-letter codes, at least one"},
		{Name: "destinations", Description: "Destinations of a primary product source, each with destination and state"},
		{Name: "default_rule", Description: "Default rule of a primary product source: takeFromDataSources"}},
	Fields: []capability.Field{
		{Name: "accepted", Description: "true when Google accepted the request; nothing is said about processing"},
		{Name: "dataSource", Description: "The data source as Google returned it, with " + dataSourceFieldsDescription},
	},
	Examples: []capability.Example{{Description: "Rename a data source",
		Arguments: json.RawMessage(`{"account":"1000001","data_source":"2000002","display_name":"Main products"}`)}},
}

var acceptedSchema = objectSchema(member("accepted", boolSchema), member("dataSource", dataSourceSchema))

func validText(value string, max int) bool {
	if value == "" || utf8.RuneCountInString(value) > max || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func validLabelPair(label, language string) error {
	if label != "" && !sourceFeedLabelForm.MatchString(label) {
		return invalidArgument("feedLabel must be at most 20 uppercase letters, digits, and dashes")
	}
	if language != "" && !sourceLanguageForm.MatchString(language) {
		return invalidArgument("contentLanguage must be a two-letter ISO 639-1 code in lower case")
	}
	if (label == "") != (language == "") {
		return invalidArgument("feedLabel and contentLanguage must be set together or not at all")
	}
	return nil
}

func validCountries(countries []string) error {
	if len(countries) == 0 || len(countries) > maxCountries {
		return invalidArgument("countries must hold between 1 and " + strconv.Itoa(maxCountries) + " country codes")
	}
	seen := map[string]bool{}
	for _, country := range countries {
		if !countryForm.MatchString(country) || seen[country] {
			return invalidArgument("countries must be distinct two-letter upper case territory codes")
		}
		seen[country] = true
	}
	return nil
}

func destinationsBody(destinations []destinationInput) ([]map[string]any, error) {
	if len(destinations) == 0 || len(destinations) > maxDestinations {
		return nil, invalidArgument("destinations must hold between 1 and " + strconv.Itoa(maxDestinations) + " entries")
	}
	seen := map[string]bool{}
	out := make([]map[string]any, 0, len(destinations))
	for _, d := range destinations {
		if !slices.Contains(destinationNames, d.Destination) || seen[d.Destination] ||
			(d.State != "" && !slices.Contains(destinationStates, d.State)) {
			return nil, invalidArgument("destinations need distinct known destination names and a state of ENABLED or DISABLED")
		}
		seen[d.Destination] = true
		entry := map[string]any{"destination": d.Destination}
		if d.State != "" {
			entry["state"] = d.State
		}
		out = append(out, entry)
	}
	return out, nil
}

// defaultRuleBody builds the rule from IDs of the same account, so a rule can never name a data source of
// another account.
func defaultRuleBody(account string, rule *defaultRuleInput) (map[string]any, error) {
	if len(rule.TakeFromDataSources) == 0 || len(rule.TakeFromDataSources) > maxRuleEntries {
		return nil, invalidArgument("default rule needs between 1 and " + strconv.Itoa(maxRuleEntries) + " entries")
	}
	entries := make([]map[string]any, 0, len(rule.TakeFromDataSources))
	for _, ref := range rule.TakeFromDataSources {
		switch {
		case ref.Self && ref.SupplementalDataSourceID == "":
			entries = append(entries, map[string]any{"self": true})
		case !ref.Self && validID(ref.SupplementalDataSourceID):
			entries = append(entries, map[string]any{
				"supplementalDataSourceName": "accounts/" + account + "/dataSources/" + ref.SupplementalDataSourceID})
		default:
			return nil, invalidArgument("each default rule entry is either self or the ID of a supplemental data source")
		}
	}
	return map[string]any{"takeFromDataSources": entries}, nil
}

func primaryBody(account string, in *primaryInput) (map[string]any, error) {
	if err := validLabelPair(in.FeedLabel, in.ContentLanguage); err != nil {
		return nil, err
	}
	out := map[string]any{}
	if in.FeedLabel != "" {
		out["feedLabel"], out["contentLanguage"] = in.FeedLabel, in.ContentLanguage
	}
	if in.Countries != nil {
		if err := validCountries(in.Countries); err != nil {
			return nil, err
		}
		out["countries"] = in.Countries
	}
	if in.Destinations != nil {
		destinations, err := destinationsBody(in.Destinations)
		if err != nil {
			return nil, err
		}
		out["destinations"] = destinations
	}
	if in.LegacyLocal {
		out["legacyLocal"] = true
	}
	if in.DefaultRule != nil {
		rule, err := defaultRuleBody(account, in.DefaultRule)
		if err != nil {
			return nil, err
		}
		out["defaultRule"] = rule
	}
	return out, nil
}

// kind names the source block of a create call for the confirmation and the check that exactly one is set.
func (a createArguments) kind() (kind string, count int) {
	for _, b := range []struct {
		set  bool
		name string
	}{
		{a.PrimaryProduct != nil, "primary product"}, {a.SupplementalProduct != nil, "supplemental product"},
		{a.RegionalInventory != nil, "regional inventory"}, {a.Promotion != nil, "promotion"},
		{a.ProductReview != nil, "product review"}, {a.MerchantReview != nil, "merchant review"},
	} {
		if b.set {
			kind, count = b.name, count+1
		}
	}
	return kind, count
}

// createBody builds the body of a create call from checked arguments only.
func createBody(account string, a createArguments) (map[string]any, error) {
	if !validText(a.DisplayName, maxDisplayNameRunes) {
		return nil, invalidArgument("display_name must be 1 to " + strconv.Itoa(maxDisplayNameRunes) +
			" characters without control characters")
	}
	if _, count := a.kind(); count != 1 {
		return nil, invalidArgument("exactly one source block is needed: primary_product, supplemental_product, " +
			"regional_inventory, promotion, product_review, or merchant_review")
	}
	body := map[string]any{"displayName": a.DisplayName}
	switch {
	case a.PrimaryProduct != nil:
		block, err := primaryBody(account, a.PrimaryProduct)
		if err != nil {
			return nil, err
		}
		body["primaryProductDataSource"] = block
	case a.SupplementalProduct != nil:
		if err := validLabelPair(a.SupplementalProduct.FeedLabel, a.SupplementalProduct.ContentLanguage); err != nil {
			return nil, err
		}
		block := map[string]any{}
		if a.SupplementalProduct.FeedLabel != "" {
			block["feedLabel"], block["contentLanguage"] = a.SupplementalProduct.FeedLabel, a.SupplementalProduct.ContentLanguage
		}
		body["supplementalProductDataSource"] = block
	case a.RegionalInventory != nil:
		r := a.RegionalInventory
		if r.FeedLabel == "" || r.ContentLanguage == "" {
			return nil, invalidArgument("regional_inventory needs feedLabel and contentLanguage")
		}
		if err := validLabelPair(r.FeedLabel, r.ContentLanguage); err != nil {
			return nil, err
		}
		body["regionalInventoryDataSource"] = map[string]any{"feedLabel": r.FeedLabel, "contentLanguage": r.ContentLanguage}
	case a.Promotion != nil:
		p := a.Promotion
		if !countryForm.MatchString(p.TargetCountry) || !sourceLanguageForm.MatchString(p.ContentLanguage) {
			return nil, invalidArgument("promotion needs a two-letter upper case targetCountry and a two-letter " +
				"lower case contentLanguage")
		}
		body["promotionDataSource"] = map[string]any{"targetCountry": p.TargetCountry, "contentLanguage": p.ContentLanguage}
	case a.ProductReview != nil:
		body["productReviewDataSource"] = map[string]any{}
	case a.MerchantReview != nil:
		body["merchantReviewDataSource"] = map[string]any{}
	}
	return body, nil
}

// updateRequest builds the update mask and the body from the members that are set. The mask holds only the
// paths of the four constants above, so no mask can reach a file input or a fetch setting.
func updateRequest(account string, a updateArguments) (mask []string, body map[string]any, err error) {
	body = map[string]any{}
	primary := map[string]any{}
	if a.DisplayName != nil {
		if !validText(*a.DisplayName, maxDisplayNameRunes) {
			return nil, nil, invalidArgument("display_name must be 1 to " + strconv.Itoa(maxDisplayNameRunes) +
				" characters without control characters")
		}
		mask, body["displayName"] = append(mask, maskDisplayName), *a.DisplayName
	}
	if a.Countries != nil {
		if err := validCountries(a.Countries); err != nil {
			return nil, nil, err
		}
		mask, primary["countries"] = append(mask, maskCountries), a.Countries
	}
	if a.Destinations != nil {
		destinations, err := destinationsBody(a.Destinations)
		if err != nil {
			return nil, nil, err
		}
		mask, primary["destinations"] = append(mask, maskDestinations), destinations
	}
	if a.DefaultRule != nil {
		rule, err := defaultRuleBody(account, a.DefaultRule)
		if err != nil {
			return nil, nil, err
		}
		mask, primary["defaultRule"] = append(mask, maskDefaultRule), rule
	}
	if len(mask) == 0 {
		return nil, nil, invalidArgument("set at least one of display_name, countries, destinations, or default_rule")
	}
	if len(primary) > 0 {
		body["primaryProductDataSource"] = primary
	}
	return mask, body, nil
}

func (cl *client) change(ctx context.Context, op, tool, method, path string, query url.Values,
	body map[string]any) (acceptedResult, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return acceptedResult{}, invalidArgument("the arguments could not be encoded")
	}
	var out dataSource
	if err := cl.send(ctx, op, tool, request{method: method, path: path, query: query, body: encoded, change: true},
		&out); err != nil {
		return acceptedResult{}, err
	}
	return acceptedResult{Accepted: true, DataSource: out}, nil
}

func handleDataSourcesCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "create data source"
	var args createArguments
	bound, err := bind(op, resolved, raw, &args)
	if err != nil {
		return nil, err
	}
	id, needsProof, err := bound.accountOf(args.Account)
	if err != nil {
		return nil, err
	}
	body, err := createBody(id, args)
	if err != nil {
		return nil, err
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := cl.settle(ctx, op, dataSourcesCreateID, needsProof, id); err != nil {
		return nil, err
	}
	return cl.change(ctx, op, dataSourcesCreateID, http.MethodPost, dataSourcesPath(id), nil, body)
}

func handleDataSourcesUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "update data source"
	var args updateArguments
	bound, err := bind(op, resolved, raw, &args)
	if err != nil {
		return nil, err
	}
	id, needsProof, err := bound.accountOf(args.Account)
	if err != nil {
		return nil, err
	}
	if !validID(args.DataSource) {
		return nil, invalidArgument("data_source must be a decimal data source ID")
	}
	mask, body, err := updateRequest(id, args)
	if err != nil {
		return nil, err
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := cl.settle(ctx, op, dataSourcesUpdateID, needsProof, id); err != nil {
		return nil, err
	}
	return cl.change(ctx, op, dataSourcesUpdateID, http.MethodPatch, dataSourcePath(id, args.DataSource),
		url.Values{"updateMask": {strings.Join(mask, ",")}}, body)
}

// createSummary and updateSummary phrase the confirmation from the validated arguments alone.
func createSummary(raw json.RawMessage) string {
	var a createArguments
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	kind, _ := a.kind()
	out := "Create an API data source " + strconv.Quote(a.DisplayName)
	if kind != "" {
		out += " (" + kind + ")"
	}
	if a.Account != "" {
		out += " in account " + a.Account
	}
	return out + "; a repeated call creates another one, and Google processes it afterwards"
}

func updateSummary(raw json.RawMessage) string {
	var a updateArguments
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	var members []string
	for _, m := range []struct {
		set  bool
		name string
	}{{a.DisplayName != nil, "display name"}, {a.Countries != nil, "countries"},
		{a.Destinations != nil, "destinations"}, {a.DefaultRule != nil, "default rule"}} {
		if m.set {
			members = append(members, m.name)
		}
	}
	out := "Update data source " + a.DataSource
	if a.Account != "" {
		out += " of account " + a.Account
	}
	if a.DisplayName != nil {
		out += " (new name " + strconv.Quote(*a.DisplayName) + ")"
	}
	return out + ": " + strings.Join(members, ", ") + "; Google processes it afterwards"
}
