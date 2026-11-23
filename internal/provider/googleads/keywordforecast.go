package googleads

import (
	"context"
	"encoding/json"
	"math"
	"regexp"
	"slices"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// Qatlas's own bounds: Google documents none for these lists. The forecast bounds follow the limits of a keyword
// plan, 200 ad groups and 10000 keywords.
const (
	themeKeywordLimit       = 200
	themeAdGroupLimit       = 50
	forecastLanguageLimit   = 100
	forecastGeoLimit        = 100
	forecastAdGroupLimit    = 200
	forecastKeywordLimit    = 10000
	forecastDateLayout      = "2006-01-02"
	forecastDatePatternText = `^[0-9]{4}-[0-9]{2}-[0-9]{2}$`
)

var currencyCodePattern = regexp.MustCompile(`^[A-Z]{3}$`)

// --- generate-ad-group-themes ---

type themesArguments struct {
	CustomerID string   `json:"customer_id"`
	Keywords   []string `json:"keywords"`
	AdGroups   []string `json:"ad_groups"`
}

const adGroupNamePatternText = `^customers/[0-9]{1,10}/adGroups/[1-9][0-9]{0,18}$`

func (a themesArguments) check() error {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := checkPlainKeywords("keywords", a.Keywords, 1, themeKeywordLimit); err != nil {
		return err
	}
	if len(a.AdGroups) == 0 || len(a.AdGroups) > themeAdGroupLimit {
		return badRequest("ad_groups must name 1 to 50 ad groups")
	}
	for i, name := range a.AdGroups {
		id, ok := cutOwn(a.CustomerID, "adGroups", name)
		if !ok || !idPattern.MatchString(id) || slices.Contains(a.AdGroups[:i], name) {
			return badRequest("ad_groups must be ad groups of the account of customer_id, " +
				"customers/CUSTOMER_ID/adGroups/AD_GROUP_ID, each once")
		}
	}
	return nil
}

type (
	themesAnswer struct {
		AdGroupKeywordSuggestions []struct {
			KeywordText          string `json:"keywordText"`
			SuggestedKeywordText string `json:"suggestedKeywordText"`
			SuggestedMatchType   string `json:"suggestedMatchType"`
			SuggestedAdGroup     string `json:"suggestedAdGroup"`
			SuggestedCampaign    string `json:"suggestedCampaign"`
		} `json:"adGroupKeywordSuggestions"`
		UnusableAdGroups []struct {
			AdGroup  string `json:"adGroup"`
			Campaign string `json:"campaign"`
		} `json:"unusableAdGroups"`
	}
	themeSuggestion struct {
		KeywordText          string `json:"keyword_text,omitempty"`
		SuggestedKeywordText string `json:"suggested_keyword_text,omitempty"`
		SuggestedMatchType   string `json:"suggested_match_type,omitempty"`
		SuggestedAdGroup     string `json:"suggested_ad_group,omitempty"`
		SuggestedCampaign    string `json:"suggested_campaign,omitempty"`
	}
	unusableAdGroup struct {
		AdGroup  string `json:"ad_group,omitempty"`
		Campaign string `json:"campaign,omitempty"`
	}
	themesResult struct {
		CustomerID                string            `json:"customer_id"`
		AdGroupKeywordSuggestions []themeSuggestion `json:"ad_group_keyword_suggestions"`
		UnusableAdGroups          []unusableAdGroup `json:"unusable_ad_groups"`
		Truncated                 bool              `json:"truncated,omitempty"`
	}
)

// cutOwn returns the ID of a resource name of the collection that belongs to the account.
func cutOwn(customerID, collection, name string) (string, bool) {
	prefix := customerName(customerID) + "/" + collection + "/"
	if len(name) <= len(prefix) || name[:len(prefix)] != prefix {
		return "", false
	}
	return name[len(prefix):], true
}

var keywordThemes = capability.Descriptor{
	ID: keywordThemesID, Version: 1, Title: "Suggest ad group themes for keywords with Google Ads",
	Description: "Ask Google Ads to sort keywords into ad groups of the account " +
		"(KeywordPlanIdeaService.GenerateAdGroupThemes): for each keyword Google suggests an ad group, its " +
		"campaign, a normalized keyword text and a match type, and names the given ad groups it cannot use " +
		"(an ad group is unusable when it is not enabled or its campaign is neither enabled nor paused). The ad " +
		"groups must belong to the account of customer_id; nothing is added to them. Google allows two requests " +
		"per second per customer ID here; Qatlas Connect keeps the one second of the other methods." + keywordNote,
	Tags: []string{"google", "ads", "keywords", "ad-groups"}, Provider: Provider, Risk: keywordRisk,
	InputSchema: encoded(object(schema{
		"customer_id": customerIDArgument,
		"keywords":    recListOf(recTextSchema(maxKeywordLength), themeKeywordLimit),
		"ad_groups":   schema{"type": "array", "minItems": 1, "maxItems": themeAdGroupLimit, "uniqueItems": true, "items": schema{"type": "string", "pattern": adGroupNamePatternText}},
	}, "customer_id", "keywords", "ad_groups")),
	OutputSchema: encoded(object(schema{"customer_id": stringSchema,
		"ad_group_keyword_suggestions": arrayOf(object(schema{"keyword_text": stringSchema, "suggested_keyword_text": stringSchema,
			"suggested_match_type": stringSchema, "suggested_ad_group": stringSchema, "suggested_campaign": stringSchema})),
		"unusable_ad_groups": arrayOf(object(schema{"ad_group": stringSchema, "campaign": stringSchema})),
		"truncated":          booleanSchema}, "customer_id", "ad_group_keyword_suggestions", "unusable_ad_groups")),
	Arguments: []capability.Argument{
		{Name: "customer_id", Required: true, Description: "Ten-digit customer ID of the account without hyphens; a " +
			"target of the connection or an account below a manager target"},
		{Name: "keywords", Required: true, Description: "1 to 200 keywords of at most 80 characters and 10 words to " +
			"group; the bound is Qatlas's, Google names none"},
		{Name: "ad_groups", Required: true, Description: "1 to 50 ad groups of the account as " +
			"customers/CUSTOMER_ID/adGroups/AD_GROUP_ID, each once; the bound is Qatlas's, Google names none"},
	},
	Fields: []capability.Field{
		{Name: "customer_id", Description: "The account of the call"},
		{Name: "ad_group_keyword_suggestions", Description: "Per suggestion: keyword_text as given, " +
			"suggested_keyword_text (the normalized text of a BROAD, EXACT or PHRASE suggestion), suggested_match_type, " +
			"suggested_ad_group and suggested_campaign as resource names of the account"},
		{Name: "unusable_ad_groups", Description: "The given ad groups Google cannot use, each with ad_group and campaign"},
		{Name: "truncated", Description: "true when Google answered more than 500 suggestions and the rest was cut"},
	},
	Examples: []capability.Example{{Description: "Sort two keywords into one ad group",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","keywords":["garden chairs","patio furniture"],` +
			`"ad_groups":["customers/1234567890/adGroups/111"]}`)}},
}

func handleKeywordThemes(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "generate ad group themes"
	var args themesArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	env, err := handleEnv(ctx, resolved, secrets, red, op, keywordThemesID)
	if err != nil {
		return nil, err
	}
	if err := args.check(); err != nil {
		return nil, err
	}
	var answer themesAnswer
	body := map[string]any{"keywords": args.Keywords, "adGroups": args.AdGroups}
	if err := keywordCall(env, args.CustomerID, "generateAdGroupThemes", body, &answer); err != nil {
		return nil, err
	}
	out := themesResult{CustomerID: args.CustomerID, AdGroupKeywordSuggestions: []themeSuggestion{},
		UnusableAdGroups: []unusableAdGroup{},
		Truncated:        len(answer.AdGroupKeywordSuggestions) > maxListItems || len(answer.UnusableAdGroups) > maxListItems}
	for _, s := range answer.AdGroupKeywordSuggestions[:min(len(answer.AdGroupKeywordSuggestions), maxListItems)] {
		group, err := answerRef(op, args.CustomerID, "adGroups", s.SuggestedAdGroup)
		if err != nil {
			return nil, err
		}
		campaign, err := answerRef(op, args.CustomerID, "campaigns", s.SuggestedCampaign)
		if err != nil {
			return nil, err
		}
		out.AdGroupKeywordSuggestions = append(out.AdGroupKeywordSuggestions, themeSuggestion{
			KeywordText: boundedText(s.KeywordText, maxText), SuggestedKeywordText: boundedText(s.SuggestedKeywordText, maxText),
			SuggestedMatchType: code(s.SuggestedMatchType), SuggestedAdGroup: group, SuggestedCampaign: campaign})
	}
	for _, u := range answer.UnusableAdGroups[:min(len(answer.UnusableAdGroups), maxListItems)] {
		group, err := answerRef(op, args.CustomerID, "adGroups", u.AdGroup)
		if err != nil {
			return nil, err
		}
		campaign, err := answerRef(op, args.CustomerID, "campaigns", u.Campaign)
		if err != nil {
			return nil, err
		}
		out.UnusableAdGroups = append(out.UnusableAdGroups, unusableAdGroup{AdGroup: group, Campaign: campaign})
	}
	return out, nil
}

// --- generate-forecast-metrics ---

type (
	forecastKeywordArgument struct {
		Text      string `json:"text"`
		MatchType string `json:"match_type"`
	}
	forecastBiddingArgument struct {
		ManualCPC *struct {
			DailyBudgetMicros *int64 `json:"daily_budget_micros"`
			MaxCPCBidMicros   int64  `json:"max_cpc_bid_micros"`
		} `json:"manual_cpc_bidding_strategy"`
		MaximizeClicks *struct {
			DailyTargetSpendMicros int64  `json:"daily_target_spend_micros"`
			MaxCPCBidCeilingMicros *int64 `json:"max_cpc_bid_ceiling_micros"`
		} `json:"maximize_clicks_bidding_strategy"`
		MaximizeConversions *struct {
			DailyTargetSpendMicros int64 `json:"daily_target_spend_micros"`
		} `json:"maximize_conversions_bidding_strategy"`
	}
	forecastArguments struct {
		CustomerID     string `json:"customer_id"`
		CurrencyCode   string `json:"currency_code"`
		ForecastPeriod *struct {
			StartDate string `json:"start_date"`
			EndDate   string `json:"end_date"`
		} `json:"forecast_period"`
		Campaign *struct {
			LanguageConstants  []string                 `json:"language_constants"`
			GeoTargetConstants []string                 `json:"geo_target_constants"`
			BiddingStrategy    *forecastBiddingArgument `json:"bidding_strategy"`
			AdGroups           []struct {
				Keywords []forecastKeywordArgument `json:"keywords"`
			} `json:"ad_groups"`
		} `json:"campaign"`
	}
)

func checkMicros(argument string, v int64) error {
	if v < 1 || v > recMaxMicros {
		return badRequest(argument + " must be a positive amount in micros")
	}
	return nil
}

func (a forecastArguments) check() error {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if a.CurrencyCode != "" && !currencyCodePattern.MatchString(a.CurrencyCode) {
		return badRequest("currency_code must be a currency code of three capital letters, such as EUR")
	}
	if p := a.ForecastPeriod; p != nil {
		start, errStart := time.Parse(forecastDateLayout, p.StartDate)
		end, errEnd := time.Parse(forecastDateLayout, p.EndDate)
		if errStart != nil || errEnd != nil || end.Before(start) {
			return badRequest("forecast_period needs start_date and end_date as yyyy-mm-dd, the end not before the start")
		}
	}
	c := a.Campaign
	if c == nil {
		return badRequest("campaign is required")
	}
	if len(c.LanguageConstants) > forecastLanguageLimit || len(c.GeoTargetConstants) > forecastGeoLimit {
		return badRequest("campaign takes at most 100 language_constants and 100 geo_target_constants")
	}
	for i, l := range c.LanguageConstants {
		if !keywordLanguageRE.MatchString(l) || slices.Contains(c.LanguageConstants[:i], l) {
			return badRequest("campaign.language_constants must be language constants, languageConstants/ID, each once")
		}
	}
	for i, g := range c.GeoTargetConstants {
		if !keywordGeoRE.MatchString(g) || slices.Contains(c.GeoTargetConstants[:i], g) {
			return badRequest("campaign.geo_target_constants must be location constants, geoTargetConstants/ID, each once")
		}
	}
	if err := c.BiddingStrategy.check(); err != nil {
		return err
	}
	if len(c.AdGroups) > forecastAdGroupLimit {
		return badRequest("campaign.ad_groups takes at most 200 ad groups")
	}
	total := 0
	for _, g := range c.AdGroups {
		if len(g.Keywords) == 0 {
			return badRequest("every ad group of campaign.ad_groups needs at least one keyword")
		}
		if total += len(g.Keywords); total > forecastKeywordLimit {
			return badRequest("campaign.ad_groups takes at most 10000 keywords in all")
		}
		for _, k := range g.Keywords {
			if err := checkPlainKeyword("campaign.ad_groups.keywords.text", k.Text); err != nil {
				return err
			}
			if !slices.Contains(recKeywordMatchTypes, k.MatchType) {
				return badRequest("campaign.ad_groups.keywords.match_type must be EXACT, PHRASE or BROAD")
			}
		}
	}
	return nil
}

// check demands exactly one strategy, with the members Google marks as required.
func (b *forecastBiddingArgument) check() error {
	if b == nil {
		return badRequest("campaign.bidding_strategy is required")
	}
	chosen := 0
	if m := b.ManualCPC; m != nil {
		chosen++
		if err := checkMicros("manual_cpc_bidding_strategy.max_cpc_bid_micros", m.MaxCPCBidMicros); err != nil {
			return err
		}
		if m.DailyBudgetMicros != nil {
			if err := checkMicros("manual_cpc_bidding_strategy.daily_budget_micros", *m.DailyBudgetMicros); err != nil {
				return err
			}
		}
	}
	if m := b.MaximizeClicks; m != nil {
		chosen++
		if err := checkMicros("maximize_clicks_bidding_strategy.daily_target_spend_micros", m.DailyTargetSpendMicros); err != nil {
			return err
		}
		if m.MaxCPCBidCeilingMicros != nil {
			if err := checkMicros("maximize_clicks_bidding_strategy.max_cpc_bid_ceiling_micros", *m.MaxCPCBidCeilingMicros); err != nil {
				return err
			}
		}
	}
	if m := b.MaximizeConversions; m != nil {
		chosen++
		if err := checkMicros("maximize_conversions_bidding_strategy.daily_target_spend_micros", m.DailyTargetSpendMicros); err != nil {
			return err
		}
	}
	if chosen != 1 {
		return badRequest("campaign.bidding_strategy names exactly one of manual_cpc_bidding_strategy, " +
			"maximize_clicks_bidding_strategy and maximize_conversions_bidding_strategy")
	}
	return nil
}

func (a forecastArguments) body() map[string]any {
	c := a.Campaign
	strategy := map[string]any{}
	switch b := c.BiddingStrategy; {
	case b.ManualCPC != nil:
		m := map[string]any{"maxCpcBidMicros": b.ManualCPC.MaxCPCBidMicros}
		if b.ManualCPC.DailyBudgetMicros != nil {
			m["dailyBudgetMicros"] = *b.ManualCPC.DailyBudgetMicros
		}
		strategy["manualCpcBiddingStrategy"] = m
	case b.MaximizeClicks != nil:
		m := map[string]any{"dailyTargetSpendMicros": b.MaximizeClicks.DailyTargetSpendMicros}
		if b.MaximizeClicks.MaxCPCBidCeilingMicros != nil {
			m["maxCpcBidCeilingMicros"] = *b.MaximizeClicks.MaxCPCBidCeilingMicros
		}
		strategy["maximizeClicksBiddingStrategy"] = m
	default:
		strategy["maximizeConversionsBiddingStrategy"] = map[string]any{
			"dailyTargetSpendMicros": b.MaximizeConversions.DailyTargetSpendMicros}
	}
	campaign := map[string]any{"biddingStrategy": strategy}
	if len(c.LanguageConstants) > 0 {
		campaign["languageConstants"] = c.LanguageConstants
	}
	if len(c.GeoTargetConstants) > 0 {
		campaign["geoTargetConstants"] = c.GeoTargetConstants
	}
	if len(c.AdGroups) > 0 {
		groups := make([]any, 0, len(c.AdGroups))
		for _, g := range c.AdGroups {
			keywords := make([]any, 0, len(g.Keywords))
			for _, k := range g.Keywords {
				keywords = append(keywords, map[string]any{"text": k.Text, "matchType": k.MatchType})
			}
			groups = append(groups, map[string]any{"keywords": keywords})
		}
		campaign["adGroups"] = groups
	}
	body := map[string]any{"campaign": campaign}
	if a.CurrencyCode != "" {
		body["currencyCode"] = a.CurrencyCode
	}
	if p := a.ForecastPeriod; p != nil {
		body["forecastPeriod"] = map[string]any{"startDate": p.StartDate, "endDate": p.EndDate}
	}
	return body
}

type (
	forecastAnswer struct {
		CampaignForecastMetrics *struct {
			AverageCPCMicros *wireInt `json:"averageCpcMicros"`
			Clicks           *float64 `json:"clicks"`
			CostMicros       *wireInt `json:"costMicros"`
			Conversions      *float64 `json:"conversions"`
			AverageCPAMicros *wireInt `json:"averageCpaMicros"`
		} `json:"campaignForecastMetrics"`
	}
	forecastMetrics struct {
		AverageCPCMicros string   `json:"average_cpc_micros,omitempty"`
		Clicks           *float64 `json:"clicks,omitempty"`
		CostMicros       string   `json:"cost_micros,omitempty"`
		Conversions      *float64 `json:"conversions,omitempty"`
		AverageCPAMicros string   `json:"average_cpa_micros,omitempty"`
	}
	forecastResult struct {
		CustomerID              string           `json:"customer_id"`
		CampaignForecastMetrics *forecastMetrics `json:"campaign_forecast_metrics,omitempty"`
	}
)

func finiteOrNil(v *float64) *float64 {
	if v == nil || math.IsNaN(*v) || math.IsInf(*v, 0) {
		return nil
	}
	return v
}

func forecastKeywordSchema() schema {
	return object(schema{"text": recTextSchema(maxKeywordLength), "match_type": recEnumSchema(recKeywordMatchTypes)},
		"text", "match_type")
}

var keywordForecast = capability.Descriptor{
	ID: keywordForecastID, Version: 1, Title: "Forecast keyword campaign metrics with Google Ads",
	Description: "Ask Google Ads to forecast the metrics of a keyword campaign that is described in the arguments " +
		"(KeywordPlanIdeaService.GenerateKeywordForecastMetrics): targeting, one bidding strategy and ad groups " +
		"with keywords. The campaign is not created and refers to no object of the account. Google needs the " +
		"start of forecast_period in the future and its end within one year of today, in the time zone of the " +
		"account, and uses the next Sunday to the following Saturday when it is left out; Qatlas Connect checks " +
		"only the form and the order of the dates and leaves the rest to Google. Google enforces a minimum " +
		"budget or spend for the currency and answers an error that names it." + keywordNote,
	Tags: []string{"google", "ads", "keywords", "forecast"}, Provider: Provider, Risk: keywordRisk,
	InputSchema: encoded(object(schema{
		"customer_id":   customerIDArgument,
		"currency_code": schema{"type": "string", "pattern": currencyCodePattern.String()},
		"forecast_period": object(schema{"start_date": schema{"type": "string", "pattern": forecastDatePatternText},
			"end_date": schema{"type": "string", "pattern": forecastDatePatternText}}, "start_date", "end_date"),
		"campaign": object(schema{
			"language_constants":   schema{"type": "array", "minItems": 1, "maxItems": forecastLanguageLimit, "uniqueItems": true, "items": schema{"type": "string", "pattern": languageConstantPat}},
			"geo_target_constants": schema{"type": "array", "minItems": 1, "maxItems": forecastGeoLimit, "uniqueItems": true, "items": schema{"type": "string", "pattern": geoConstantPattern}},
			"bidding_strategy": object(schema{
				"manual_cpc_bidding_strategy": object(schema{"daily_budget_micros": recMicrosSchema(),
					"max_cpc_bid_micros": recMicrosSchema()}, "max_cpc_bid_micros"),
				"maximize_clicks_bidding_strategy": object(schema{"daily_target_spend_micros": recMicrosSchema(),
					"max_cpc_bid_ceiling_micros": recMicrosSchema()}, "daily_target_spend_micros"),
				"maximize_conversions_bidding_strategy": object(schema{"daily_target_spend_micros": recMicrosSchema()},
					"daily_target_spend_micros"),
			}),
			"ad_groups": recListOf(object(schema{"keywords": recListOf(forecastKeywordSchema(), forecastKeywordLimit)},
				"keywords"), forecastAdGroupLimit),
		}, "bidding_strategy"),
	}, "customer_id", "campaign")),
	OutputSchema: encoded(object(schema{"customer_id": stringSchema,
		"campaign_forecast_metrics": object(schema{"average_cpc_micros": stringSchema, "clicks": numberSchema,
			"cost_micros": stringSchema, "conversions": numberSchema, "average_cpa_micros": stringSchema})},
		"customer_id")),
	Arguments: []capability.Argument{
		{Name: "customer_id", Required: true, Description: "Ten-digit customer ID of the account without hyphens; a " +
			"target of the connection or an account below a manager target"},
		{Name: "currency_code", Description: "Currency of the amounts when it differs from the account currency, " +
			"three capital letters such as EUR; left out, the account currency"},
		{Name: "forecast_period", Description: "start_date and end_date as yyyy-mm-dd, both inclusive, the start in " +
			"the future and the end within one year of today; left out, next Sunday to the following Saturday"},
		{Name: "campaign", Required: true, Description: "The campaign to forecast: language_constants " +
			"(languageConstants/ID, at most 100), geo_target_constants (geoTargetConstants/ID, unique, at most 100), " +
			"bidding_strategy (required, exactly one of manual_cpc_bidding_strategy with max_cpc_bid_micros and " +
			"optional daily_budget_micros, maximize_clicks_bidding_strategy with daily_target_spend_micros and " +
			"optional max_cpc_bid_ceiling_micros, maximize_conversions_bidding_strategy with " +
			"daily_target_spend_micros, all in micros) and ad_groups (at most 200, each with at least one keyword of " +
			"text and match_type EXACT, PHRASE or BROAD, 10000 keywords in all; the bounds are Qatlas's)"},
	},
	Fields: []capability.Field{
		{Name: "customer_id", Description: "The account of the call"},
		{Name: "campaign_forecast_metrics", Description: "The forecast of the campaign: average_cpc_micros (only with " +
			"clicks above zero), clicks (only for manual CPC and maximize clicks), cost_micros, conversions (only for " +
			"maximize conversions) and average_cpa_micros; a member Google leaves out is left out here"},
	},
	Examples: []capability.Example{{Description: "Forecast a manual CPC campaign for one ad group",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","currency_code":"EUR","campaign":{` +
			`"language_constants":["languageConstants/1001"],"geo_target_constants":["geoTargetConstants/2276"],` +
			`"bidding_strategy":{"manual_cpc_bidding_strategy":{"daily_budget_micros":20000000,"max_cpc_bid_micros":1500000}},` +
			`"ad_groups":[{"keywords":[{"text":"garden chairs","match_type":"PHRASE"}]}]}}`)}},
}

func handleKeywordForecast(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "generate keyword forecast metrics"
	var args forecastArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	env, err := handleEnv(ctx, resolved, secrets, red, op, keywordForecastID)
	if err != nil {
		return nil, err
	}
	if err := args.check(); err != nil {
		return nil, err
	}
	var answer forecastAnswer
	if err := keywordCall(env, args.CustomerID, "generateKeywordForecastMetrics", args.body(), &answer); err != nil {
		return nil, err
	}
	out := forecastResult{CustomerID: args.CustomerID}
	if m := answer.CampaignForecastMetrics; m != nil {
		out.CampaignForecastMetrics = &forecastMetrics{AverageCPCMicros: wireDecimal(m.AverageCPCMicros),
			Clicks: finiteOrNil(m.Clicks), CostMicros: wireDecimal(m.CostMicros), Conversions: finiteOrNil(m.Conversions),
			AverageCPAMicros: wireDecimal(m.AverageCPAMicros)}
	}
	return out, nil
}
