package googlebusinessprofile

import (
	"context"
	"encoding/json"
	"net/url"
	"slices"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	categoriesListID     = Provider + ".categories.list"
	categoriesBatchGetID = Provider + ".categories.batch-get"
)

const (
	maxCategoriesPage = 100
	maxCategoryNames  = 50
	maxFilterText     = 100
)

type (
	categoriesAnswer struct {
		Categories    []category `json:"categories"`
		NextPageToken string     `json:"nextPageToken"`
	}
	categoriesResult struct {
		Categories []category `json:"categories"`
		NextCursor string     `json:"next_cursor,omitempty"`
	}
)

var (
	categoriesResultSchema = `{"type":"object","properties":{"categories":` + arraySchema(categorySchema) +
		`,"next_cursor":{"type":"string"}},"required":["categories"],"additionalProperties":false}`
	categoriesBatchResultSchema = `{"type":"object","properties":{"categories":` + arraySchema(categorySchema) +
		`},"required":["categories"],"additionalProperties":false}`
)

var categoryFields = []capability.Field{
	{Name: "categories", Description: "Categories with name (categories/gcid:ID), displayName, and with view " +
		"FULL also serviceTypes (serviceTypeId, displayName) and moreHoursTypes (hoursTypeId, displayName, " +
		"localizedDisplayName)"},
}

var categoriesList = capability.Descriptor{
	ID: categoriesListID, Version: 1, Title: "List Business Profile categories",
	Description: "List the business categories Google Business Profile knows for a region and a language, up to " +
		"100 per call with a cursor to read on; filter narrows them by display name. view BASIC returns name and " +
		"display name, FULL adds the service types and hours types of each category; an empty list means no " +
		"match. " + referenceNote,
	Tags: []string{"google", "business-profile", "categories"}, Risk: referenceRisk, Provider: Provider,
	InputSchema: inputSchema([]string{"region_code", "language_code", "view"},
		"region_code", regionArgSchema,
		"language_code", languageArgSchema,
		"view", viewArgSchema,
		"filter", `{"type":"string","minLength":1,"maxLength":100}`,
		"page_size", `{"type":"integer","minimum":1,"maximum":100}`,
		"cursor", cursorSchema),
	OutputSchema: json.RawMessage(categoriesResultSchema),
	Arguments: []capability.Argument{
		{Name: "region_code", Required: true, Description: "ISO 3166-1 alpha-2 country code in capitals, such as DE"},
		{Name: "language_code", Required: true, Description: "BCP 47 language code the names come in, such as de or en-GB"},
		{Name: "view", Required: true, Description: "BASIC (name and display name) or FULL (also service types and hours types)"},
		{Name: "filter", Description: "Text the display name of a category must match, for example restaurant; " +
			"Google applies it"},
		{Name: "page_size", Description: "Categories per call, 1 to 100; Google's default is 100"},
		cursorArgument,
	},
	Fields: append(append([]capability.Field{}, categoryFields...), nextCursorField),
	Examples: []capability.Example{{Description: "Categories whose name holds a text, in German",
		Arguments: json.RawMessage(`{"region_code":"DE","language_code":"de","view":"BASIC","filter":"sample"}`)}},
}

var categoriesBatchGet = capability.Descriptor{
	ID: categoriesBatchGetID, Version: 1, Title: "Get Business Profile categories by name",
	Description: "Read several business categories by their names (categories/gcid:ID, at most 50) in one " +
		"language. Google returns them in no fixed order, and a name it does not know is left out. view BASIC " +
		"returns name and display name, FULL adds the service types and hours types. " + referenceNote,
	Tags: []string{"google", "business-profile", "categories"}, Risk: referenceRisk, Provider: Provider,
	InputSchema: inputSchema([]string{"names", "language_code", "view"},
		"names", `{"type":"array","minItems":1,"maxItems":50,"uniqueItems":true,"items":{"type":"string",`+
			`"maxLength":120,"pattern":"`+categoryPattern+`","x-form":"`+formCategory+`"}}`,
		"language_code", languageArgSchema,
		"view", viewArgSchema,
		"region_code", regionArgSchema),
	OutputSchema: json.RawMessage(categoriesBatchResultSchema),
	Arguments: []capability.Argument{
		{Name: "names", Required: true, Description: "Category names as categories/gcid:ID, 1 to 50, each once; " +
			"the IDs come from categories.list or from a location"},
		{Name: "language_code", Required: true, Description: "BCP 47 language code the names come in, such as de or en-GB"},
		{Name: "view", Required: true, Description: "BASIC (name and display name) or FULL (also service types and hours types)"},
		{Name: "region_code", Description: "ISO 3166-1 alpha-2 country code in capitals, from which Google infers " +
			"a non-standard language"},
	},
	Fields: categoryFields,
	Examples: []capability.Example{{Description: "Two categories by name",
		Arguments: json.RawMessage(`{"names":["categories/gcid:sample_one","categories/gcid:sample_two"],` +
			`"language_code":"en","view":"BASIC"}`)}},
}

type categoriesArguments struct {
	RegionCode   string   `json:"region_code"`
	LanguageCode string   `json:"language_code"`
	View         string   `json:"view"`
	Filter       string   `json:"filter"`
	PageSize     int      `json:"page_size"`
	Cursor       string   `json:"cursor"`
	Names        []string `json:"names"`
}

func handleCategoriesList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage) (any, error) {
	const op = "list categories"
	var args categoriesArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, provider.Fail(op, "the validated arguments could not be read")
	}
	if err := checkRegion(args.RegionCode, true); err != nil {
		return nil, err
	}
	if err := checkLanguage("language_code", args.LanguageCode); err != nil {
		return nil, err
	}
	if err := checkView(args.View); err != nil {
		return nil, err
	}
	if args.Filter != "" {
		if err := checkText("filter", args.Filter, maxFilterText); err != nil {
			return nil, err
		}
	}
	if args.PageSize < 0 || args.PageSize > maxCategoriesPage {
		return nil, invalidArgument("page_size must be 1 to 100")
	}
	bound, err := checkConnection(resolved)
	if err != nil {
		return nil, err
	}
	binding := provider.CursorBinding(categoriesListID, args.RegionCode, args.LanguageCode, args.View, args.Filter,
		bound.binding())
	query := url.Values{"regionCode": {args.RegionCode}, "languageCode": {args.LanguageCode}, "view": {args.View}}
	if args.Filter != "" {
		query.Set("filter", "displayName="+args.Filter)
	}
	query = pageSizeQuery(query, args.PageSize)
	if query, err = pageQuery(binding, args.Cursor, query); err != nil {
		return nil, err
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var answer categoriesAnswer
	if err := c.get(ctx, op, categoriesListID, request{root: informationRoot, path: "categories", query: query,
		notFound: notFoundReference}, &answer); err != nil {
		return nil, err
	}
	if len(answer.Categories) > maxCategoriesPage {
		return nil, tooMany(op, maxCategoriesPage)
	}
	out := categoriesResult{Categories: append([]category{}, answer.Categories...)}
	if out.NextCursor, err = nextCursor(op, binding, answer.NextPageToken); err != nil {
		return nil, err
	}
	return out, nil
}

func handleCategoriesBatchGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "get categories"
	var args categoriesArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, provider.Fail(op, "the validated arguments could not be read")
	}
	switch {
	case len(args.Names) == 0:
		return nil, invalidArgument("names is required: name at least one category")
	case len(args.Names) > maxCategoryNames:
		return nil, invalidArgument("names holds at most 50 categories")
	}
	for i, name := range args.Names {
		if !categoryForm.MatchString(name) {
			return nil, invalidArgument("names must be categories/gcid:ID with lower-case letters, digits, and underscores")
		}
		if slices.Contains(args.Names[:i], name) {
			return nil, invalidArgument("names names a category twice")
		}
	}
	if err := checkLanguage("language_code", args.LanguageCode); err != nil {
		return nil, err
	}
	if err := checkView(args.View); err != nil {
		return nil, err
	}
	if err := checkRegion(args.RegionCode, false); err != nil {
		return nil, err
	}
	if _, err := checkConnection(resolved); err != nil {
		return nil, err
	}
	query := url.Values{"names": args.Names, "languageCode": {args.LanguageCode}, "view": {args.View}}
	if args.RegionCode != "" {
		query.Set("regionCode", args.RegionCode)
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var answer categoriesAnswer
	if err := c.get(ctx, op, categoriesBatchGetID, request{root: informationRoot, path: "categories:batchGet",
		query: query, notFound: notFoundReference}, &answer); err != nil {
		return nil, err
	}
	if len(answer.Categories) > len(args.Names) {
		return nil, tooMany(op, len(args.Names))
	}
	return categoriesResult{Categories: append([]category{}, answer.Categories...)}, nil
}
