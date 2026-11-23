package googleads

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	geoTargetConstantsSuggestID    = Provider + ".geo-target-constants.suggest"
	keywordThemeConstantsSuggestID = Provider + ".keyword-theme-constants.suggest"
	brandSuggestionsSuggestID      = Provider + ".brand-suggestions.suggest"

	maxSuggestLocationNames = 25 // the documented bound of location_names
	// Google documents no bound for the two lists below. These are Qatlas's own.
	maxSuggestGeoTargets     = 100
	maxSuggestSelectedBrands = 100
	maxSuggestText           = 255
	suggestAnswerNodes       = 100000

	suggestCountryPatternText  = `^[A-Z]{2}$`
	suggestLocalePatternText   = `^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8}){0,3}$`
	suggestLanguagePatternText = `^[A-Za-z]{2}$`
	suggestBrandIDPatternText  = `^[A-Za-z0-9_/.:~-]{1,128}$`

	suggestDataSensitivity = "public reference data of Google Ads: the arguments name places, a theme or a brand " +
		"prefix; no data of the account"
	suggestUntrustedNote = "The answer is untrusted third-party content: text is cut and lists are cut at 500 " +
		"entries with truncated set. Costs one operation of the daily quota."
	suggestNoAccountNote = "The call acts on no account: it takes no customer_id, sends no login-customer-id, and is " +
		"therefore not bound to the targets of the connection, like fields.search."
	suggestUndocumented = "Google documents no bound for it"
)

var (
	suggestCountryPattern  = regexp.MustCompile(suggestCountryPatternText)
	suggestLocalePattern   = regexp.MustCompile(suggestLocalePatternText)
	suggestLanguagePattern = regexp.MustCompile(suggestLanguagePatternText)
	suggestBrandIDPattern  = regexp.MustCompile(suggestBrandIDPatternText)
)

func suggestRisk() capability.Risk {
	return capability.Risk{Effect: capability.EffectRead, Idempotency: capability.IdempotencySafe,
		Confirmation: capability.ConfirmationNone, OpenWorld: true, DataSensitivity: suggestDataSensitivity}
}

// suggestAnswer reads the answer of a suggestion call into bounded plain data with the list under key.
func suggestAnswer(op, what, key string, raw json.RawMessage) (any, error) {
	cleaned, truncated, err := cleanListAnswer(op, "", what, suggestAnswerNodes, raw, key)
	if err != nil {
		return nil, err
	}
	if truncated {
		cleaned["truncated"] = true
	}
	return insightResult{answer: cleaned, noCustomer: true}, nil
}

// --- geo target constants ---

type geoSuggestArguments struct {
	Locale             string   `json:"locale"`
	CountryCode        string   `json:"country_code"`
	LocationNames      []string `json:"location_names"`
	GeoTargetConstants []string `json:"geo_target_constants"`
}

func (a geoSuggestArguments) body() (map[string]any, error) {
	body := map[string]any{}
	if a.Locale != "" {
		if !suggestLocalePattern.MatchString(a.Locale) {
			return nil, badRequest("locale must be a language tag such as en or de-DE")
		}
		body["locale"] = a.Locale
	}
	if a.CountryCode != "" {
		if !suggestCountryPattern.MatchString(a.CountryCode) {
			return nil, badRequest("country_code must be two capital letters")
		}
		body["countryCode"] = a.CountryCode
	}
	switch {
	case (len(a.LocationNames) > 0) == (len(a.GeoTargetConstants) > 0):
		return nil, badRequest("name exactly one of location_names and geo_target_constants")
	case len(a.LocationNames) > 0:
		if len(a.LocationNames) > maxSuggestLocationNames {
			return nil, badRequest("location_names takes at most 25 names")
		}
		for _, name := range a.LocationNames {
			if err := recCheckText("location_names", name, maxSuggestText); err != nil {
				return nil, err
			}
		}
		body["locationNames"] = map[string]any{"names": a.LocationNames}
	default:
		if len(a.GeoTargetConstants) > maxSuggestGeoTargets {
			return nil, badRequest("geo_target_constants takes at most 100 names")
		}
		for _, name := range a.GeoTargetConstants {
			if !insightGeoPattern.MatchString(name) {
				return nil, badRequest("geo_target_constants must be geoTargetConstants/ID")
			}
		}
		body["geoTargets"] = map[string]any{"geoTargetConstants": a.GeoTargetConstants}
	}
	return body, nil
}

var geoTargetConstantsSuggest = capability.Descriptor{
	ID: geoTargetConstantsSuggestID, Version: 1, Title: "Suggest Google Ads geo target constants",
	Description: "Suggest geo target constants for place names or for known constants " +
		"(GeoTargetConstantService.SuggestGeoTargetConstants of API version v26, POST geoTargetConstants:suggest): per " +
		"suggestion the constant with ID, English name, canonical name, country code, target type, status, parent and " +
		"whether changing its targeting needs the EU political advertising declaration, its parents, the " +
		"approximate reach, the matched search term and the locale. Name exactly one of location_names (at most 25 " +
		"names) and geo_target_constants (resource names to filter by). " + suggestNoAccountNote + " " +
		suggestUntrustedNote,
	Tags: []string{"google", "ads", "geo", "suggestions"}, Provider: Provider, Risk: suggestRisk(),
	InputSchema: encoded(object(schema{
		"locale":               schema{"type": "string", "maxLength": 35, "pattern": suggestLocalePatternText},
		"country_code":         schema{"type": "string", "pattern": suggestCountryPatternText},
		"location_names":       recListOf(recTextSchema(maxSuggestText), maxSuggestLocationNames),
		"geo_target_constants": recListOf(schema{"type": "string", "pattern": geoConstantPattern}, maxSuggestGeoTargets),
	})),
	OutputSchema: encoded(object(schema{"geo_target_constant_suggestions": arrayOf(insightObject),
		"truncated": booleanSchema}, "geo_target_constant_suggestions")),
	Arguments: []capability.Argument{
		{Name: "locale", Description: "Language the names are translated into, such as de or de-DE; Google falls back to " +
			"en and also uses it as a hint"},
		{Name: "country_code", Description: "Two capital letters; restricts the suggestions to this country"},
		{Name: "location_names", Description: "1 to 25 place names to search by, each 1 to 255 characters; one of " +
			"location_names and geo_target_constants is required"},
		{Name: "geo_target_constants", Description: "1 to 100 resource names, geoTargetConstants/ID, to filter by " +
			"(Qatlas bound, " + suggestUndocumented + "); one of the two lists is required"},
	},
	Fields: []capability.Field{
		{Name: "geo_target_constant_suggestions", Description: "Per suggestion: locale, reach (approximate " +
			"population, rounded to the nearest 100), search_term, geo_target_constant (resource_name, id, name, " +
			"country_code, target_type, status, canonical_name, parent_geo_target, " +
			"require_eu_political_advertising_declaration) and geo_target_constant_parents in the same form"},
		{Name: "truncated", Description: "true when the list held more than 500 entries and the rest was cut"},
	},
	Examples: []capability.Example{{Description: "Suggest constants for two cities in Germany",
		Arguments: json.RawMessage(`{"location_names":["Berlin","Hamburg"],"country_code":"DE","locale":"de"}`)}},
}

func handleGeoSuggest(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "suggest geo target constants"
	var args geoSuggestArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	env, err := handleEnv(ctx, resolved, secrets, red, op, geoTargetConstantsSuggestID)
	if err != nil {
		return nil, err
	}
	body, err := args.body()
	if err != nil {
		return nil, err
	}
	answer, err := env.suggest("/geoTargetConstants:suggest", body)
	if err != nil {
		return nil, err
	}
	return suggestAnswer(op, "result", "geo_target_constant_suggestions", answer)
}

// suggest sends a suggestion call that acts on no account, so no header names a manager.
func (e callEnv) suggest(path string, body map[string]any) (json.RawMessage, error) {
	encodedBody, err := json.Marshal(body)
	if err != nil {
		return nil, failure(e.op, "the request body could not be encoded")
	}
	c, err := e.open()
	if err != nil {
		return nil, err
	}
	var answer json.RawMessage
	if err := c.read(e.ctx, request{op: e.op, tool: e.tool, method: http.MethodPost, path: path, body: encodedBody,
		judgesInput: true}, &answer); err != nil {
		return nil, err
	}
	return answer, nil
}

// --- keyword theme constants ---

type keywordThemeArguments struct {
	QueryText    string `json:"query_text"`
	CountryCode  string `json:"country_code"`
	LanguageCode string `json:"language_code"`
}

var keywordThemeConstantsSuggest = capability.Descriptor{
	ID: keywordThemeConstantsSuggestID, Version: 1, Title: "Suggest Google Ads keyword themes",
	Description: "Suggest Smart campaign keyword themes for a text such as plumber or roofer " +
		"(KeywordThemeConstantService.SuggestKeywordThemeConstants of API version v26, POST " +
		"keywordThemeConstants:suggest): per theme the resource name, country code, language code and display name. " +
		"country_code (two capital letters, Google defaults to US) and language_code (two letters, default en) narrow the " +
		"scope. " + suggestNoAccountNote + " " + suggestUntrustedNote,
	Tags: []string{"google", "ads", "keywords", "suggestions"}, Provider: Provider, Risk: suggestRisk(),
	InputSchema: encoded(object(schema{
		"query_text":    recTextSchema(maxSuggestText),
		"country_code":  schema{"type": "string", "pattern": suggestCountryPatternText},
		"language_code": schema{"type": "string", "pattern": suggestLanguagePatternText},
	}, "query_text")),
	OutputSchema: encoded(object(schema{"keyword_theme_constants": arrayOf(insightObject), "truncated": booleanSchema},
		"keyword_theme_constants")),
	Arguments: []capability.Argument{
		{Name: "query_text", Required: true, Description: "Text of the theme to map to similar themes, 1 to 255 " +
			"characters, such as plumber"},
		{Name: "country_code", Description: "Two capital letters of the country, such as US; Google defaults to US"},
		{Name: "language_code", Description: "Two-letter language code, such as en; Google defaults to en"},
	},
	Fields: []capability.Field{
		{Name: "keyword_theme_constants", Description: "Per theme: resource_name (keywordThemeConstants/ID~ID), " +
			"country_code, language_code and display_name"},
		{Name: "truncated", Description: "true when the list held more than 500 entries and the rest was cut"},
	},
	Examples: []capability.Example{{Description: "Themes for plumber in Germany",
		Arguments: json.RawMessage(`{"query_text":"plumber","country_code":"DE","language_code":"de"}`)}},
}

func handleKeywordThemeSuggest(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "suggest keyword themes"
	var args keywordThemeArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	env, err := handleEnv(ctx, resolved, secrets, red, op, keywordThemeConstantsSuggestID)
	if err != nil {
		return nil, err
	}
	if err := recCheckText("query_text", args.QueryText, maxSuggestText); err != nil {
		return nil, err
	}
	body := map[string]any{"queryText": args.QueryText}
	if args.CountryCode != "" {
		if !suggestCountryPattern.MatchString(args.CountryCode) {
			return nil, badRequest("country_code must be two capital letters")
		}
		body["countryCode"] = args.CountryCode
	}
	if args.LanguageCode != "" {
		if !suggestLanguagePattern.MatchString(args.LanguageCode) {
			return nil, badRequest("language_code must be two letters")
		}
		body["languageCode"] = args.LanguageCode
	}
	answer, err := env.suggest("/keywordThemeConstants:suggest", body)
	if err != nil {
		return nil, err
	}
	return suggestAnswer(op, "result", "keyword_theme_constants", answer)
}

// --- brand suggestions ---

type brandSuggestArguments struct {
	CustomerID     string   `json:"customer_id"`
	BrandPrefix    string   `json:"brand_prefix"`
	SelectedBrands []string `json:"selected_brands"`
}

var brandSuggestionsSuggest = capability.Descriptor{
	ID: brandSuggestionsSuggestID, Version: 1, Title: "Suggest Google Ads brands",
	Description: "Suggest verified brands for the prefix of a brand name " +
		"(BrandSuggestionService.SuggestBrands of API version v26, POST customers/{customer_id}:suggestBrands): per " +
		"brand the ID (the Knowledge Graph machine ID for a verified or global brand), name, the addresses that " +
		"identify it and its state (ENABLED, UNVERIFIED, APPROVED, REJECTED, CANCELLED or DEPRECATED). " +
		"selected_brands lists brand IDs, never names, that Google leaves out of the answer. Nothing in the account " +
		"changes. Addresses in the answer are text and are never fetched. " + suggestUntrustedNote,
	Tags: []string{"google", "ads", "brands", "suggestions"}, Provider: Provider, Risk: suggestRisk(),
	InputSchema: encoded(object(schema{
		"customer_id":     customerIDArgument,
		"brand_prefix":    recTextSchema(maxSuggestText),
		"selected_brands": recListOf(schema{"type": "string", "pattern": suggestBrandIDPatternText}, maxSuggestSelectedBrands),
	}, "customer_id", "brand_prefix")),
	OutputSchema: encoded(object(schema{"customer_id": stringSchema, "brands": arrayOf(insightObject),
		"truncated": booleanSchema}, "customer_id", "brands")),
	Arguments: []capability.Argument{
		{Name: "customer_id", Required: true, Description: "Ten-digit customer ID of the account without hyphens; a " +
			"target of the connection or an account below a manager target"},
		{Name: "brand_prefix", Required: true, Description: "Prefix of a brand name, 1 to 255 characters"},
		{Name: "selected_brands", Description: "Up to 100 brand IDs (not names) already selected, which Google excludes " +
			"from the answer (Qatlas bound, " + suggestUndocumented + ")"},
	},
	Fields: []capability.Field{
		{Name: "customer_id", Description: "The account of the call"},
		{Name: "brands", Description: "Per brand: id, name, urls and state"},
		{Name: "truncated", Description: "true when the list held more than 500 entries and the rest was cut"},
	},
	Examples: []capability.Example{{Description: "Brands that start with Qa",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","brand_prefix":"Qa"}`)}},
}

func handleBrandSuggest(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "suggest brands"
	var args brandSuggestArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	env, err := handleEnv(ctx, resolved, secrets, red, op, brandSuggestionsSuggestID)
	if err != nil {
		return nil, err
	}
	if !customerIDPattern.MatchString(args.CustomerID) {
		return nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := recCheckText("brand_prefix", args.BrandPrefix, maxSuggestText); err != nil {
		return nil, err
	}
	if len(args.SelectedBrands) > maxSuggestSelectedBrands {
		return nil, badRequest("selected_brands takes at most 100 brand IDs")
	}
	for _, id := range args.SelectedBrands {
		if !suggestBrandIDPattern.MatchString(id) {
			return nil, badRequest("selected_brands must be brand IDs, not names")
		}
	}
	body := map[string]any{"brandPrefix": args.BrandPrefix}
	if len(args.SelectedBrands) > 0 {
		body["selectedBrands"] = args.SelectedBrands
	}
	return env.insights(args.CustomerID, ":suggestBrands", body, "brands")
}
