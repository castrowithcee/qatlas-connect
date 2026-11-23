package googleads

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"net/http"
	"regexp"
	"slices"
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
	recommendationsGenerateID = Provider + ".recommendations.generate"
	recommendationsApplyID    = Provider + ".recommendations.apply"
	recommendationsDismissID  = Provider + ".recommendations.dismiss"

	// recommendationService is the path of the RecommendationService below the customer. Its actions are
	// ...:generate, ...:apply and ...:dismiss; none of them has a Mutate form.
	recommendationService = "/recommendations"

	// maxRecommendationOperations is Qatlas's own bound of the operations of one apply or dismiss request.
	// Google allows 100; every operation of an apply can change a budget, a bid or a keyword.
	maxRecommendationOperations = 10

	recMaxText       = 255
	recMaxMicros     = 9007199254740991
	maxAnswerNodes   = 50000
	maxAnswerDepth   = 16
	maxKeywordWords  = 10
	maxKeywordLength = 80
)

// recommendationNamePattern is the form of a Recommendation resource name; the account part is checked against
// the account of the call, never taken from here.
const recommendationNamePatternText = `^customers/[0-9]{1,10}/recommendations/[A-Za-z0-9_.:=~-]{1,128}$`

var recommendationIDPattern = regexp.MustCompile(`^[A-Za-z0-9_.:=~-]{1,128}$`)

// checkRecommendationName refuses a resource name that is not customers/ID/recommendations/ID of exactly the
// checked account.
func checkRecommendationName(argument, customerID, name string) error {
	rest, ok := strings.CutPrefix(name, customerName(customerID)+"/recommendations/")
	if !ok || !recommendationIDPattern.MatchString(rest) {
		return badRequest(argument + " must be a recommendation of the account of customer_id, " +
			"customers/CUSTOMER_ID/recommendations/RECOMMENDATION_ID")
	}
	return nil
}

// recCheckText refuses an empty text, one with control characters, and one longer than limit characters.
func recCheckText(argument, value string, limit int) error {
	if value == "" || utf8.RuneCountInString(value) > limit || !utf8.ValidString(value) {
		return badRequest(argument + " must be 1 to " + recItoa(limit) + " characters")
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return badRequest(argument + " must not hold control characters")
		}
	}
	return nil
}

func recItoa(n int) string { return strconv.Itoa(n) }

// recCheckFinite refuses a number that is not finite or lies outside the bounds. A bound is exclusive when its
// flag is set.
func recCheckFinite(argument string, v, lo, hi float64, loExclusive, hiExclusive bool) error {
	if math.IsNaN(v) || math.IsInf(v, 0) || v < lo || v > hi || (loExclusive && v <= lo) || (hiExclusive && v >= hi) {
		return badRequest(argument + " is outside the range Google documents")
	}
	return nil
}

// --- shared enums and schemas ---

var (
	recGenerateTypes = []string{"CAMPAIGN_BUDGET", "KEYWORD", "MAXIMIZE_CLICKS_OPT_IN", "MAXIMIZE_CONVERSIONS_OPT_IN",
		"MAXIMIZE_CONVERSION_VALUE_OPT_IN", "SET_TARGET_CPA", "SET_TARGET_ROAS", "SITELINK_ASSET", "TARGET_CPA_OPT_IN",
		"TARGET_ROAS_OPT_IN"}
	recGenerateChannels = []string{"SEARCH", "PERFORMANCE_MAX"}
	recTrackingStatuses = []string{"NOT_CONVERSION_TRACKED", "CONVERSION_TRACKING_MANAGED_BY_SELF",
		"CONVERSION_TRACKING_MANAGED_BY_THIS_MANAGER", "CONVERSION_TRACKING_MANAGED_BY_ANOTHER_MANAGER"}
	recBiddingStrategyTypes = []string{"COMMISSION", "ENHANCED_CPC", "FIXED_CPM", "FIXED_SHARE_OF_VOICE", "INVALID",
		"MANUAL_CPA", "MANUAL_CPC", "MANUAL_CPM", "MANUAL_CPV", "MAXIMIZE_CONVERSIONS", "MAXIMIZE_CONVERSION_VALUE",
		"PAGE_ONE_PROMOTED", "PERCENT_CPC", "TARGET_CPA", "TARGET_CPC", "TARGET_CPM", "TARGET_CPV",
		"TARGET_IMPRESSION_SHARE", "TARGET_OUTRANK_SHARE", "TARGET_ROAS", "TARGET_SPEND"}
	recAdGroupTypes = []string{"SEARCH_STANDARD", "DISPLAY_STANDARD", "SHOPPING_PRODUCT_ADS", "HOTEL_ADS",
		"SHOPPING_SMART_ADS", "VIDEO_BUMPER", "VIDEO_TRUE_VIEW_IN_STREAM", "VIDEO_TRUE_VIEW_IN_DISPLAY",
		"VIDEO_NON_SKIPPABLE_IN_STREAM", "SEARCH_DYNAMIC_ADS", "SHOPPING_COMPARISON_LISTING_ADS", "PROMOTED_HOTEL_ADS",
		"VIDEO_RESPONSIVE", "VIDEO_EFFICIENT_REACH", "SMART_CAMPAIGN_ADS", "TRAVEL_ADS", "YOUTUBE_AUDIO"}
	recShareLocations    = []string{"ANYWHERE_ON_PAGE", "TOP_OF_PAGE", "ABSOLUTE_TOP_OF_PAGE"}
	recKeywordMatchTypes = []string{"EXACT", "PHRASE", "BROAD"}

	// recNeedsBidding are the types for which the reference calls conversion tracking status and bidding
	// information necessary; CAMPAIGN_BUDGET needs the bidding information only.
	recNeedsBidding = []string{"MAXIMIZE_CLICKS_OPT_IN", "MAXIMIZE_CONVERSIONS_OPT_IN", "MAXIMIZE_CONVERSION_VALUE_OPT_IN",
		"SET_TARGET_CPA", "SET_TARGET_ROAS", "TARGET_CPA_OPT_IN", "TARGET_ROAS_OPT_IN"}
)

func recEnumSchema(values []string) schema { return schema{"type": "string", "enum": values} }

func recTextSchema(limit int) schema {
	return schema{"type": "string", "minLength": 1, "maxLength": limit}
}

func recMicrosSchema() schema {
	return schema{"type": "integer", "minimum": 1, "maximum": recMaxMicros}
}

func recListOf(items schema, max int) schema {
	return schema{"type": "array", "minItems": 1, "maxItems": max, "items": items}
}

// --- generate: arguments ---

type (
	recKeywordArgument struct {
		Text      string `json:"text"`
		MatchType string `json:"match_type"`
	}
	impressionShareArgument struct {
		Location                   string `json:"location"`
		TargetImpressionShareMicro int64  `json:"target_impression_share_micros"`
		MaxCPCBidCeiling           *int64 `json:"max_cpc_bid_ceiling"`
	}
	biddingInfoArgument struct {
		BiddingStrategyType       string                   `json:"bidding_strategy_type"`
		TargetCPAMicros           *int64                   `json:"target_cpa_micros"`
		TargetROAS                *float64                 `json:"target_roas"`
		TargetImpressionShareInfo *impressionShareArgument `json:"target_impression_share_info"`
	}
	adGroupInfoArgument struct {
		AdGroupType string               `json:"ad_group_type"`
		Keywords    []recKeywordArgument `json:"keywords"`
	}
	seedInfoArgument struct {
		URLSeed      string   `json:"url_seed"`
		KeywordSeeds []string `json:"keyword_seeds"`
	}
	budgetInfoArgument struct {
		CurrentBudget int64 `json:"current_budget"`
	}
	assetGroupInfoArgument struct {
		FinalURL    string   `json:"final_url"`
		Headline    []string `json:"headline"`
		Description []string `json:"description"`
	}
)

type generateArguments struct {
	CustomerID                 string                   `json:"customer_id"`
	RecommendationTypes        []string                 `json:"recommendation_types"`
	AdvertisingChannelType     string                   `json:"advertising_channel_type"`
	CampaignSitelinkCount      *int                     `json:"campaign_sitelink_count"`
	ConversionTrackingStatus   string                   `json:"conversion_tracking_status"`
	BiddingInfo                *biddingInfoArgument     `json:"bidding_info"`
	AdGroupInfo                []adGroupInfoArgument    `json:"ad_group_info"`
	SeedInfo                   *seedInfoArgument        `json:"seed_info"`
	BudgetInfo                 *budgetInfoArgument      `json:"budget_info"`
	CampaignImageAssetCount    *int                     `json:"campaign_image_asset_count"`
	CampaignCallAssetCount     *int                     `json:"campaign_call_asset_count"`
	CountryCodes               []string                 `json:"country_codes"`
	LanguageCodes              []string                 `json:"language_codes"`
	PositiveLocationIDs        []int64                  `json:"positive_location_ids"`
	NegativeLocationIDs        []int64                  `json:"negative_location_ids"`
	AssetGroupInfo             []assetGroupInfoArgument `json:"asset_group_info"`
	TargetPartnerSearchNetwork *bool                    `json:"target_partner_search_network"`
	TargetContentNetwork       *bool                    `json:"target_content_network"`
	MerchantCenterAccountID    string                   `json:"merchant_center_account_id"`
	IsNewCustomer              *bool                    `json:"is_new_customer"`
}

var (
	recCountryCodePattern  = regexp.MustCompile(`^[A-Z]{2}$`)
	recLanguageCodePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{2,16}$`)
)

// recCheckKeyword checks a keyword text: Google documents at most 80 characters and 10 words.
func recCheckKeyword(argument string, k recKeywordArgument) error {
	if err := recCheckText(argument+".text", k.Text, maxKeywordLength); err != nil {
		return err
	}
	if len(strings.Fields(k.Text)) > maxKeywordWords {
		return badRequest(argument + ".text must not be longer than 10 words")
	}
	if !slices.Contains(recKeywordMatchTypes, k.MatchType) {
		return badRequest(argument + ".match_type must be EXACT, PHRASE or BROAD")
	}
	return nil
}

// recCheckSeedURL checks the address that Google reads keyword ideas from. The reference writes it without a
// scheme, such as www.example.com/cars. Qatlas never fetches it; Google does. It must name a plain host, and a
// scheme, when given, must be https, without user info.
func recCheckSeedURL(argument, value string) error {
	if err := recCheckText(argument, value, maxURLArgument); err != nil {
		return err
	}
	for _, r := range value {
		if unicode.IsSpace(r) || r == '\\' || r == '{' || r == '}' {
			return badRequest(argument + " must not hold spaces, backslashes or braces")
		}
	}
	if strings.Contains(value, "://") {
		return checkAddress(argument, value)
	}
	authority := value
	if i := strings.IndexAny(authority, "/?#"); i >= 0 {
		authority = authority[:i]
	}
	if authority == "" || strings.ContainsAny(authority, "@:") {
		return badRequest(argument + " must be a host name with an optional path, such as www.example.com/cars, " +
			"without a scheme other than https and without user info")
	}
	return nil
}

func (a generateArguments) has(kind string) bool { return slices.Contains(a.RecommendationTypes, kind) }

// check checks every argument and the pairs the reference calls necessary for a requested type. The pairs are
// the ones the reference names with "necessary" or "required"; Google decides the rest.
func (a generateArguments) check() error {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if len(a.RecommendationTypes) == 0 || len(a.RecommendationTypes) > len(recGenerateTypes) {
		return badRequest("recommendation_types must name 1 to 10 types")
	}
	var seen []string
	for _, t := range a.RecommendationTypes {
		if !slices.Contains(recGenerateTypes, t) || slices.Contains(seen, t) {
			return badRequest("recommendation_types may name only " + joinOr(recGenerateTypes) + ", each once")
		}
		seen = append(seen, t)
	}
	if !slices.Contains(recGenerateChannels, a.AdvertisingChannelType) {
		return badRequest("advertising_channel_type must be SEARCH or PERFORMANCE_MAX")
	}
	if a.ConversionTrackingStatus != "" && !slices.Contains(recTrackingStatuses, a.ConversionTrackingStatus) {
		return badRequest("conversion_tracking_status is not a value Google defines")
	}
	for name, n := range map[string]*int{"campaign_sitelink_count": a.CampaignSitelinkCount,
		"campaign_image_asset_count": a.CampaignImageAssetCount, "campaign_call_asset_count": a.CampaignCallAssetCount} {
		if n != nil && (*n < 0 || *n > 1_000_000) {
			return badRequest(name + " must be 0 to 1000000")
		}
	}
	if err := a.checkBidding(); err != nil {
		return err
	}
	if len(a.AdGroupInfo) > 1 {
		return badRequest("ad_group_info takes the information of one ad group")
	}
	for _, g := range a.AdGroupInfo {
		if g.AdGroupType != "" && !slices.Contains(recAdGroupTypes, g.AdGroupType) {
			return badRequest("ad_group_info.ad_group_type is not a value Google defines")
		}
		if len(g.Keywords) > 200 {
			return badRequest("ad_group_info.keywords takes at most 200 keywords")
		}
		for _, k := range g.Keywords {
			if err := recCheckKeyword("ad_group_info.keywords", k); err != nil {
				return err
			}
		}
	}
	if s := a.SeedInfo; s != nil {
		if s.URLSeed == "" && len(s.KeywordSeeds) == 0 {
			return badRequest("seed_info needs url_seed or keyword_seeds")
		}
		if s.URLSeed != "" {
			if err := recCheckSeedURL("seed_info.url_seed", s.URLSeed); err != nil {
				return err
			}
		}
		if len(s.KeywordSeeds) > 50 {
			return badRequest("seed_info.keyword_seeds takes at most 50 seeds")
		}
		for _, k := range s.KeywordSeeds {
			if err := recCheckText("seed_info.keyword_seeds", k, recMaxText); err != nil {
				return err
			}
		}
	}
	if b := a.BudgetInfo; b != nil && (b.CurrentBudget < 1 || b.CurrentBudget > recMaxMicros) {
		return badRequest("budget_info.current_budget must be a positive amount in micros")
	}
	if len(a.CountryCodes) > 250 || len(a.LanguageCodes) > 100 {
		return badRequest("country_codes takes at most 250 and language_codes at most 100 entries")
	}
	for _, c := range a.CountryCodes {
		if !recCountryCodePattern.MatchString(c) {
			return badRequest("country_codes must be two capital letters each, such as DE")
		}
	}
	for _, c := range a.LanguageCodes {
		if !recLanguageCodePattern.MatchString(c) {
			return badRequest("language_codes must be 2 to 16 letters, digits, '-' or '_' each")
		}
	}
	for name, ids := range map[string][]int64{"positive_location_ids": a.PositiveLocationIDs,
		"negative_location_ids": a.NegativeLocationIDs} {
		if len(ids) > 1000 {
			return badRequest(name + " takes at most 1000 IDs")
		}
		for _, id := range ids {
			if id < 1 {
				return badRequest(name + " must be positive whole numbers")
			}
		}
	}
	if len(a.AssetGroupInfo) > 20 {
		return badRequest("asset_group_info takes at most 20 asset groups")
	}
	for _, g := range a.AssetGroupInfo {
		if g.FinalURL == "" {
			return badRequest("asset_group_info.final_url is required")
		}
		if err := checkAddress("asset_group_info.final_url", g.FinalURL); err != nil {
			return err
		}
		if len(g.Headline) > 20 || len(g.Description) > 20 {
			return badRequest("asset_group_info takes at most 20 headlines and 20 descriptions")
		}
		for _, t := range append(slices.Clone(g.Headline), g.Description...) {
			if err := recCheckText("asset_group_info texts", t, recMaxText); err != nil {
				return err
			}
		}
	}
	if a.MerchantCenterAccountID != "" {
		if err := checkID("merchant_center_account_id", a.MerchantCenterAccountID); err != nil {
			return err
		}
		if a.AdvertisingChannelType != "PERFORMANCE_MAX" {
			return badRequest("merchant_center_account_id is for advertising_channel_type PERFORMANCE_MAX only")
		}
	}
	return a.checkNecessary()
}

func (a generateArguments) checkBidding() error {
	b := a.BiddingInfo
	if b == nil {
		return nil
	}
	if b.BiddingStrategyType != "" && !slices.Contains(recBiddingStrategyTypes, b.BiddingStrategyType) {
		return badRequest("bidding_info.bidding_strategy_type is not a value Google defines")
	}
	targets := 0
	if b.TargetCPAMicros != nil {
		targets++
		if *b.TargetCPAMicros < 1 || *b.TargetCPAMicros > recMaxMicros {
			return badRequest("bidding_info.target_cpa_micros must be a positive amount in micros")
		}
	}
	if b.TargetROAS != nil {
		targets++
		if err := recCheckFinite("bidding_info.target_roas", *b.TargetROAS, 0, 1000, true, false); err != nil {
			return err
		}
	}
	if t := b.TargetImpressionShareInfo; t != nil {
		targets++
		if !slices.Contains(recShareLocations, t.Location) {
			return badRequest("bidding_info.target_impression_share_info.location must be ANYWHERE_ON_PAGE, " +
				"TOP_OF_PAGE or ABSOLUTE_TOP_OF_PAGE")
		}
		if t.TargetImpressionShareMicro < 1 || t.TargetImpressionShareMicro > 1_000_000 {
			return badRequest("bidding_info.target_impression_share_info.target_impression_share_micros must be " +
				"1 to 1000000")
		}
		if t.MaxCPCBidCeiling != nil && (*t.MaxCPCBidCeiling < 1 || *t.MaxCPCBidCeiling > recMaxMicros) {
			return badRequest("bidding_info.target_impression_share_info.max_cpc_bid_ceiling must be a positive " +
				"amount in micros")
		}
	}
	if targets > 1 {
		return badRequest("bidding_info takes at most one of target_cpa_micros, target_roas and " +
			"target_impression_share_info")
	}
	return nil
}

// checkNecessary refuses a request that leaves out what the reference calls necessary for a requested type.
func (a generateArguments) checkNecessary() error {
	strategy := ""
	if a.BiddingInfo != nil {
		strategy = a.BiddingInfo.BiddingStrategyType
	}
	for _, t := range a.RecommendationTypes {
		switch {
		case t == "SITELINK_ASSET" && a.CampaignSitelinkCount == nil:
			return badRequest("campaign_sitelink_count is necessary for SITELINK_ASSET")
		case slices.Contains(recNeedsBidding, t) && (a.ConversionTrackingStatus == "" || strategy == ""):
			return badRequest("conversion_tracking_status and bidding_info.bidding_strategy_type are necessary for " + t)
		case t == "CAMPAIGN_BUDGET" && strategy == "":
			return badRequest("bidding_info.bidding_strategy_type is necessary for CAMPAIGN_BUDGET")
		case t == "KEYWORD" && a.SeedInfo == nil:
			return badRequest("seed_info is necessary for KEYWORD")
		case t == "KEYWORD":
			for _, g := range a.AdGroupInfo {
				if g.AdGroupType == "" {
					return badRequest("ad_group_info.ad_group_type is necessary for KEYWORD when ad_group_info is given")
				}
			}
		}
	}
	if a.has("CAMPAIGN_BUDGET") && a.AdvertisingChannelType == "SEARCH" {
		switch {
		case len(a.AdGroupInfo) == 0 || len(a.AdGroupInfo[0].Keywords) == 0:
			return badRequest("ad_group_info with keywords is necessary for CAMPAIGN_BUDGET on SEARCH")
		case len(a.CountryCodes) == 0 || len(a.LanguageCodes) == 0:
			return badRequest("country_codes and language_codes are necessary for CAMPAIGN_BUDGET on SEARCH")
		case len(a.PositiveLocationIDs) == 0 && len(a.NegativeLocationIDs) == 0:
			return badRequest("positive_location_ids or negative_location_ids is necessary for CAMPAIGN_BUDGET on SEARCH")
		case strategy == "TARGET_IMPRESSION_SHARE" && a.BiddingInfo.TargetImpressionShareInfo == nil:
			return badRequest("bidding_info.target_impression_share_info is necessary for CAMPAIGN_BUDGET on SEARCH " +
				"with the strategy TARGET_IMPRESSION_SHARE")
		}
	}
	if a.has("CAMPAIGN_BUDGET") && a.AdvertisingChannelType == "PERFORMANCE_MAX" && len(a.AssetGroupInfo) == 0 {
		return badRequest("asset_group_info is necessary for CAMPAIGN_BUDGET on PERFORMANCE_MAX")
	}
	return nil
}

// body builds the request body in the recCamelCase of the JSON mapping.
func (a generateArguments) body() map[string]any {
	body := map[string]any{"recommendationTypes": a.RecommendationTypes, "advertisingChannelType": a.AdvertisingChannelType}
	set := func(name string, v any, ok bool) {
		if ok {
			body[name] = v
		}
	}
	set("campaignSitelinkCount", a.CampaignSitelinkCount, a.CampaignSitelinkCount != nil)
	set("conversionTrackingStatus", a.ConversionTrackingStatus, a.ConversionTrackingStatus != "")
	if b := a.BiddingInfo; b != nil {
		info := map[string]any{}
		set2 := func(name string, v any, ok bool) {
			if ok {
				info[name] = v
			}
		}
		set2("biddingStrategyType", b.BiddingStrategyType, b.BiddingStrategyType != "")
		set2("targetCpaMicros", b.TargetCPAMicros, b.TargetCPAMicros != nil)
		set2("targetRoas", b.TargetROAS, b.TargetROAS != nil)
		if t := b.TargetImpressionShareInfo; t != nil {
			share := map[string]any{"location": t.Location, "targetImpressionShareMicros": t.TargetImpressionShareMicro}
			if t.MaxCPCBidCeiling != nil {
				share["maxCpcBidCeiling"] = *t.MaxCPCBidCeiling
			}
			info["targetImpressionShareInfo"] = share
		}
		body["biddingInfo"] = info
	}
	if len(a.AdGroupInfo) > 0 {
		groups := []any{}
		for _, g := range a.AdGroupInfo {
			group := map[string]any{}
			if g.AdGroupType != "" {
				group["adGroupType"] = g.AdGroupType
			}
			if len(g.Keywords) > 0 {
				keywords := []any{}
				for _, k := range g.Keywords {
					keywords = append(keywords, map[string]any{"text": k.Text, "matchType": k.MatchType})
				}
				group["keywords"] = keywords
			}
			groups = append(groups, group)
		}
		body["adGroupInfo"] = groups
	}
	if s := a.SeedInfo; s != nil {
		seed := map[string]any{}
		set3 := func(name string, v any, ok bool) {
			if ok {
				seed[name] = v
			}
		}
		set3("urlSeed", s.URLSeed, s.URLSeed != "")
		set3("keywordSeeds", s.KeywordSeeds, len(s.KeywordSeeds) > 0)
		body["seedInfo"] = seed
	}
	if b := a.BudgetInfo; b != nil {
		body["budgetInfo"] = map[string]any{"currentBudget": b.CurrentBudget}
	}
	set("campaignImageAssetCount", a.CampaignImageAssetCount, a.CampaignImageAssetCount != nil)
	set("campaignCallAssetCount", a.CampaignCallAssetCount, a.CampaignCallAssetCount != nil)
	set("countryCodes", a.CountryCodes, len(a.CountryCodes) > 0)
	set("languageCodes", a.LanguageCodes, len(a.LanguageCodes) > 0)
	// The reference spells these two fields positive_locations_ids and negative_locations_ids.
	set("positiveLocationsIds", a.PositiveLocationIDs, len(a.PositiveLocationIDs) > 0)
	set("negativeLocationsIds", a.NegativeLocationIDs, len(a.NegativeLocationIDs) > 0)
	if len(a.AssetGroupInfo) > 0 {
		groups := []any{}
		for _, g := range a.AssetGroupInfo {
			group := map[string]any{"finalUrl": g.FinalURL}
			if len(g.Headline) > 0 {
				group["headline"] = g.Headline
			}
			if len(g.Description) > 0 {
				group["description"] = g.Description
			}
			groups = append(groups, group)
		}
		body["assetGroupInfo"] = groups
	}
	set("targetPartnerSearchNetwork", a.TargetPartnerSearchNetwork, a.TargetPartnerSearchNetwork != nil)
	set("targetContentNetwork", a.TargetContentNetwork, a.TargetContentNetwork != nil)
	set("merchantCenterAccountId", a.MerchantCenterAccountID, a.MerchantCenterAccountID != "")
	set("isNewCustomer", a.IsNewCustomer, a.IsNewCustomer != nil)
	return body
}

// --- generate: answer ---

// recDropped are the members of a recommendation that no answer carries: the ways out of lead form assets, which
// no tool of this provider offers, and the members the reference marks as deprecated.
var recDropped = []string{"google_secret", "advertiser_webhook_url", "webhook", "delivery_methods", "shareable_link_id",
	"app_conversion_goals", "current_auto_goals", "current_custom_goal"}

var recAnswerKeyPattern = regexp.MustCompile(`^[a-z][A-Za-z0-9]{0,63}$`)

// recSnakeCase turns a member name of the JSON mapping, such as campaignBudgetRecommendation, into the name of the
// reference, campaign_budget_recommendation.
func recSnakeCase(name string) string {
	var out strings.Builder
	for i, r := range name {
		if unicode.IsUpper(r) {
			if i > 0 {
				out.WriteByte('_')
			}
			r = unicode.ToLower(r)
		}
		out.WriteRune(r)
	}
	return out.String()
}

// recommendationCleaner turns the recommendation of an answer into bounded plain data. Every string is
// untrusted text: control characters become spaces and the text is cut. A resource name must belong to the
// account of the call, so an answer never points at another account.
type recommendationCleaner struct {
	op, customerID string
	nodes          int
	// what names the cleaned answer in messages; empty means a recommendation. cut is set when a list was cut.
	what string
	cut  bool
}

func (c *recommendationCleaner) bad(message string) error {
	if c.what != "" {
		message = strings.ReplaceAll(message, "a recommendation", "a "+c.what)
		message = strings.ReplaceAll(message, "recommendation data", c.what+" data")
	}
	return provider.InvalidResponse(c.op, subject+" "+message)
}

func (c *recommendationCleaner) clean(value any, depth int) (any, bool, error) {
	if depth > maxAnswerDepth {
		return nil, false, c.bad("returned a recommendation nested deeper than Qatlas Connect reads")
	}
	if c.nodes++; c.nodes > maxAnswerNodes {
		return nil, false, c.bad("returned more recommendation data than Qatlas Connect reads")
	}
	switch v := value.(type) {
	case map[string]any:
		out := map[string]any{}
		for name, child := range v {
			key := recSnakeCase(name)
			if !recAnswerKeyPattern.MatchString(name) || slices.Contains(recDropped, key) {
				continue
			}
			cleaned, keep, err := c.clean(child, depth+1)
			if err != nil {
				return nil, false, err
			}
			if keep {
				out[key] = cleaned
			}
		}
		return out, true, nil
	case []any:
		c.cut = c.cut || len(v) > maxListItems
		out := make([]any, 0, min(len(v), maxListItems))
		for _, child := range v[:min(len(v), maxListItems)] {
			cleaned, keep, err := c.clean(child, depth+1)
			if err != nil {
				return nil, false, err
			}
			if keep {
				out = append(out, cleaned)
			}
		}
		return out, true, nil
	case string:
		if strings.HasPrefix(v, "customers/") && !strings.HasPrefix(v, customerName(c.customerID)+"/") {
			return nil, false, c.bad("returned a recommendation that names an object of another account than the " +
				"one asked for")
		}
		return boundedText(v, maxText), true, nil
	case json.Number, bool:
		return v, true, nil
	}
	return nil, false, nil
}

// recommendationOf cleans one recommendation of an answer.
func recommendationOf(op, customerID string, raw json.RawMessage) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value map[string]any
	if err := decoder.Decode(&value); err != nil || value == nil {
		return nil, provider.InvalidResponse(op, subject+" returned a recommendation Qatlas Connect cannot read")
	}
	c := &recommendationCleaner{op: op, customerID: customerID}
	cleaned, _, err := c.clean(value, 0)
	if err != nil {
		return nil, err
	}
	out := cleaned.(map[string]any)
	if name, ok := out["resource_name"].(string); ok && name != "" {
		if checkRecommendationName("resource_name", customerID, name) != nil {
			return nil, provider.InvalidResponse(op, subject+" returned a recommendation that is not one of the "+
				"account asked for")
		}
	}
	return out, nil
}

type generateResult struct {
	CustomerID      string           `json:"customer_id"`
	Recommendations []map[string]any `json:"recommendations"`
	Truncated       bool             `json:"truncated,omitempty"`
}

// --- generate: descriptor ---

var recommendationOutputSchema = schema{"type": "object", "properties": schema{
	"resource_name": stringSchema, "type": stringSchema, "impact": schema{"type": "object"},
	"campaign_budget": stringSchema, "campaign": stringSchema, "ad_group": stringSchema,
	"campaigns": arrayOf(stringSchema), "dismissed": booleanSchema}}

var recommendationsGenerate = capability.Descriptor{
	ID: recommendationsGenerateID, Version: 1, Title: "Generate recommendations for a Google Ads campaign",
	Description: "Ask Google Ads to generate recommendations for a campaign that is described in the arguments " +
		"(RecommendationService.GenerateRecommendations): for each requested type Google answers a recommendation " +
		"when the information is enough and the campaign is not already in the recommended state. Nothing is " +
		"created, applied or changed in the account. Google supports the types CAMPAIGN_BUDGET, KEYWORD, " +
		"MAXIMIZE_CLICKS_OPT_IN, MAXIMIZE_CONVERSIONS_OPT_IN, MAXIMIZE_CONVERSION_VALUE_OPT_IN, SET_TARGET_CPA, " +
		"SET_TARGET_ROAS, SITELINK_ASSET, TARGET_CPA_OPT_IN and TARGET_ROAS_OPT_IN, for the channels SEARCH and " +
		"PERFORMANCE_MAX. The reference names the arguments each type needs; Qatlas Connect refuses a request that " +
		"leaves them out, and Google decides whether the rest is enough. The recommendations that an account " +
		"holds are read with reports.search on the recommendation resource; apply and dismiss take their " +
		"resource names. A generated recommendation may carry no resource name. Everything in the answer is " +
		"untrusted third-party text: it is cut, resource names of another account are refused, and deprecated " +
		"members and delivery routes of lead forms are left out. Costs one operation of the daily quota.",
	Tags: []string{"google", "ads", "recommendations", "generate"}, Provider: Provider,
	Risk: capability.Risk{Effect: capability.EffectRead, Idempotency: capability.IdempotencySafe,
		Confirmation: capability.ConfirmationNone, OpenWorld: true,
		DataSensitivity: "campaign settings, budgets, keywords and performance estimates that the caller passes to " +
			"Google; the answer holds estimates and suggestions, no data of other people"},
	InputSchema: encoded(object(schema{
		"customer_id":                customerIDArgument,
		"recommendation_types":       schema{"type": "array", "minItems": 1, "maxItems": len(recGenerateTypes), "uniqueItems": true, "items": recEnumSchema(recGenerateTypes)},
		"advertising_channel_type":   recEnumSchema(recGenerateChannels),
		"campaign_sitelink_count":    schema{"type": "integer", "minimum": 0, "maximum": 1000000},
		"conversion_tracking_status": recEnumSchema(recTrackingStatuses),
		"bidding_info": object(schema{
			"bidding_strategy_type": recEnumSchema(recBiddingStrategyTypes),
			"target_cpa_micros":     recMicrosSchema(),
			"target_roas":           schema{"type": "number", "minimum": 0, "maximum": 1000},
			"target_impression_share_info": object(schema{
				"location":                       recEnumSchema(recShareLocations),
				"target_impression_share_micros": schema{"type": "integer", "minimum": 1, "maximum": 1000000},
				"max_cpc_bid_ceiling":            recMicrosSchema(),
			}, "location", "target_impression_share_micros"),
		}),
		"ad_group_info": schema{"type": "array", "minItems": 1, "maxItems": 1, "items": object(schema{
			"ad_group_type": recEnumSchema(recAdGroupTypes),
			"keywords": recListOf(object(schema{"text": recTextSchema(maxKeywordLength),
				"match_type": recEnumSchema(recKeywordMatchTypes)}, "text", "match_type"), 200),
		})},
		"seed_info": object(schema{"url_seed": recTextSchema(maxURLArgument),
			"keyword_seeds": recListOf(recTextSchema(recMaxText), 50)}),
		"budget_info":                object(schema{"current_budget": recMicrosSchema()}, "current_budget"),
		"campaign_image_asset_count": schema{"type": "integer", "minimum": 0, "maximum": 1000000},
		"campaign_call_asset_count":  schema{"type": "integer", "minimum": 0, "maximum": 1000000},
		"country_codes":              recListOf(schema{"type": "string", "pattern": "^[A-Z]{2}$"}, 250),
		"language_codes":             recListOf(schema{"type": "string", "pattern": recLanguageCodePattern.String()}, 100),
		"positive_location_ids":      recListOf(recMicrosSchema(), 1000),
		"negative_location_ids":      recListOf(recMicrosSchema(), 1000),
		"asset_group_info": recListOf(object(schema{"final_url": recTextSchema(maxURLArgument),
			"headline": recListOf(recTextSchema(recMaxText), 20), "description": recListOf(recTextSchema(recMaxText), 20)},
			"final_url"), 20),
		"target_partner_search_network": booleanSchema,
		"target_content_network":        booleanSchema,
		"merchant_center_account_id":    schema{"type": "string", "pattern": idPatternText},
		"is_new_customer":               booleanSchema,
	}, "customer_id", "recommendation_types", "advertising_channel_type")),
	OutputSchema: encoded(object(schema{"customer_id": stringSchema,
		"recommendations": arrayOf(recommendationOutputSchema), "truncated": booleanSchema},
		"customer_id", "recommendations")),
	Arguments: []capability.Argument{
		{Name: "customer_id", Required: true, Description: "Ten-digit customer ID of the account without hyphens; a " +
			"target of the connection or an account below a manager target"},
		{Name: "recommendation_types", Required: true, Description: "1 to 10 of CAMPAIGN_BUDGET, KEYWORD, " +
			"MAXIMIZE_CLICKS_OPT_IN, MAXIMIZE_CONVERSIONS_OPT_IN, MAXIMIZE_CONVERSION_VALUE_OPT_IN, SET_TARGET_CPA, " +
			"SET_TARGET_ROAS, SITELINK_ASSET, TARGET_CPA_OPT_IN, TARGET_ROAS_OPT_IN, each once"},
		{Name: "advertising_channel_type", Required: true, Description: "SEARCH or PERFORMANCE_MAX"},
		{Name: "campaign_sitelink_count", Description: "Number of sitelinks of the campaign; necessary for SITELINK_ASSET"},
		{Name: "conversion_tracking_status", Description: "NOT_CONVERSION_TRACKED, CONVERSION_TRACKING_MANAGED_BY_SELF, " +
			"CONVERSION_TRACKING_MANAGED_BY_THIS_MANAGER or CONVERSION_TRACKING_MANAGED_BY_ANOTHER_MANAGER; necessary " +
			"for the opt-in types, SET_TARGET_CPA and SET_TARGET_ROAS"},
		{Name: "bidding_info", Description: "Current bidding of the campaign: bidding_strategy_type (necessary for " +
			"CAMPAIGN_BUDGET and the types that need conversion_tracking_status) and at most one of target_cpa_micros, " +
			"target_roas (0 to 1000) and target_impression_share_info (location, target_impression_share_micros 1 to " +
			"1000000, optional max_cpc_bid_ceiling; necessary for CAMPAIGN_BUDGET on SEARCH with " +
			"TARGET_IMPRESSION_SHARE)"},
		{Name: "ad_group_info", Description: "One ad group: ad_group_type (necessary for KEYWORD when given) and " +
			"keywords (text up to 80 characters and 10 words, match_type EXACT, PHRASE or BROAD; necessary for " +
			"CAMPAIGN_BUDGET on SEARCH)"},
		{Name: "seed_info", Description: "Seeds for KEYWORD: url_seed, a page such as www.example.com/cars that " +
			"Google reads, without a scheme or with https and without user info, and keyword_seeds; necessary for KEYWORD"},
		{Name: "budget_info", Description: "current_budget in micros of the campaign"},
		{Name: "campaign_image_asset_count", Description: "Number of image assets of the campaign"},
		{Name: "campaign_call_asset_count", Description: "Number of call assets of the campaign"},
		{Name: "country_codes", Description: "Country codes of the campaign, two capital letters; necessary for " +
			"CAMPAIGN_BUDGET on SEARCH"},
		{Name: "language_codes", Description: "Language codes of the campaign; necessary for CAMPAIGN_BUDGET on SEARCH"},
		{Name: "positive_location_ids", Description: "Positive location IDs of the campaign; one of the two location " +
			"lists is necessary for CAMPAIGN_BUDGET on SEARCH"},
		{Name: "negative_location_ids", Description: "Negative location IDs of the campaign"},
		{Name: "asset_group_info", Description: "Asset groups of the campaign: final_url (https without user info), " +
			"headline and description lists; necessary for CAMPAIGN_BUDGET on PERFORMANCE_MAX"},
		{Name: "target_partner_search_network", Description: "true when the campaign serves on the Google Partner Network"},
		{Name: "target_content_network", Description: "true when the campaign serves on specified placements of the " +
			"Google Display Network"},
		{Name: "merchant_center_account_id", Description: "Merchant Center account ID; PERFORMANCE_MAX only, which then " +
			"generates for Performance Max for retail"},
		{Name: "is_new_customer", Description: "true for an account without a campaign yet: Google then uses its " +
			"model for new customers"},
	},
	Fields: []capability.Field{
		{Name: "customer_id", Description: "The account of the call"},
		{Name: "recommendations", Description: "Per recommendation, with the member names of the reference in snake_case: " +
			"resource_name when present, type, impact (base_metrics and potential_metrics with impressions, clicks, " +
			"cost_micros, conversions, conversions_value, video_views), campaign_budget, campaign, ad_group, campaigns, " +
			"dismissed, and the one member of the type, such as campaign_budget_recommendation, " +
			"keyword_recommendation, target_cpa_opt_in_recommendation, target_roas_opt_in_recommendation or " +
			"sitelink_asset_recommendation, with every member of the reference below it (budgets and bids in micros, " +
			"options, search terms, recommended assets)"},
		{Name: "truncated", Description: "true when Google answered more than 500 recommendations and the rest was cut"},
	},
	Examples: []capability.Example{{Description: "Generate a budget recommendation for a search campaign",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","recommendation_types":["CAMPAIGN_BUDGET"],` +
			`"advertising_channel_type":"SEARCH","bidding_info":{"bidding_strategy_type":"TARGET_SPEND"},` +
			`"ad_group_info":[{"keywords":[{"text":"garden chairs","match_type":"PHRASE"}]}],` +
			`"country_codes":["DE"],"language_codes":["de"],"positive_location_ids":[2276],` +
			`"budget_info":{"current_budget":10000000}}`)}},
}

func handleRecommendationsGenerate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "generate recommendations"
	var args generateArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	env, err := handleEnv(ctx, resolved, secrets, red, op, recommendationsGenerateID)
	if err != nil {
		return nil, err
	}
	if err := args.check(); err != nil {
		return nil, err
	}
	body, err := json.Marshal(args.body())
	if err != nil {
		return nil, failure(op, "the request body could not be encoded")
	}
	c, err := env.settle(args.CustomerID, false)
	if err != nil {
		return nil, err
	}
	var answer struct {
		Recommendations []json.RawMessage `json:"recommendations"`
	}
	if err := c.read(ctx, request{op: op, tool: env.tool, method: http.MethodPost,
		path: "/" + customerName(args.CustomerID) + recommendationService + ":generate", body: body,
		onAccount: true, judgesInput: true}, &answer); err != nil {
		return nil, err
	}
	out := generateResult{CustomerID: args.CustomerID, Recommendations: []map[string]any{},
		Truncated: len(answer.Recommendations) > maxListItems}
	for _, item := range answer.Recommendations[:min(len(answer.Recommendations), maxListItems)] {
		cleaned, err := recommendationOf(op, args.CustomerID, item)
		if err != nil {
			return nil, err
		}
		out.Recommendations = append(out.Recommendations, cleaned)
	}
	return out, nil
}

// --- dismiss ---

type dismissArguments struct {
	CustomerID    string   `json:"customer_id"`
	ResourceNames []string `json:"resource_names"`
}

// check refuses the call before any secret is read: the account, the bound, the names of the account, no name twice.
func (a dismissArguments) check() error {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if len(a.ResourceNames) == 0 || len(a.ResourceNames) > maxRecommendationOperations {
		return badRequest("resource_names must name 1 to 10 recommendations")
	}
	for i, name := range a.ResourceNames {
		if err := checkRecommendationName("resource_names", a.CustomerID, name); err != nil {
			return err
		}
		if slices.Contains(a.ResourceNames[:i], name) {
			return badRequest("resource_names names a recommendation more than once")
		}
	}
	return nil
}

// actionResults is what ApplyRecommendation and DismissRecommendation answer. partial_failure is never set, so a
// partial_failure_error is not expected.
type recActionAnswer struct {
	Results []struct {
		ResourceName string `json:"resourceName"`
	} `json:"results"`
	PartialFailureError json.RawMessage `json:"partialFailureError"`
}

// recSettledNames checks the answer of an atomic action against the names asked for: one result per operation, in
// the same order, with the same name. The request was sent, so anything else leaves the outcome open.
func recSettledNames(op string, answer recActionAnswer, asked []string) ([]string, error) {
	if len(answer.PartialFailureError) > 0 && string(answer.PartialFailureError) != "null" {
		return nil, unsureResult(op, "answered with a partial failure although the request asked for none")
	}
	if len(answer.Results) != len(asked) {
		return nil, unsureResult(op, "answered with another number of results than the operations asked for")
	}
	out := make([]string, 0, len(asked))
	for i, result := range answer.Results {
		if result.ResourceName != asked[i] {
			return nil, unsureResult(op, "answered with a result for another recommendation than the one asked for")
		}
		out = append(out, result.ResourceName)
	}
	return out, nil
}

type recActionResult struct {
	CustomerID string   `json:"customer_id"`
	Results    []string `json:"results"`
}

var recActionOutputSchema = object(schema{"customer_id": stringSchema, "results": arrayOf(stringSchema)},
	"customer_id", "results")

var recommendationsDismiss = capability.Descriptor{
	ID: recommendationsDismissID, Version: 1, Title: "Dismiss recommendations of a Google Ads account",
	Description: "Dismiss recommendations of a Google Ads account " +
		"(RecommendationService.DismissRecommendation with 1 to 10 operations): Google stops showing them. The " +
		"reference offers no call that restores a dismissed recommendation, and Google may generate a new one. " +
		"Nothing else in the account changes. The request is atomic, without partial_failure: Google dismisses all " +
		"of them or none, and the reference asks for recommendations of one type per request. The service has no " +
		"validate_only, so this tool has none either: the call dismisses. Recommendations are named by their " +
		"resource names, customers/CUSTOMER_ID/recommendations/ID of the account of the call." + mutateListNote,
	Tags: []string{"google", "ads", "recommendations", "dismiss"}, Provider: Provider,
	Risk: changeRisk(capability.EffectUpdate, capability.IdempotencyNonIdempotent,
		"suggestions of a Google Ads account: hides recommendations; no spend changes"),
	InputSchema: encoded(object(schema{"customer_id": customerIDArgument,
		"resource_names": schema{"type": "array", "minItems": 1, "maxItems": maxRecommendationOperations,
			"uniqueItems": true, "items": schema{"type": "string", "pattern": recommendationNamePatternText}}},
		"customer_id", "resource_names")),
	OutputSchema: encoded(recActionOutputSchema),
	Arguments: []capability.Argument{
		{Name: "customer_id", Required: true, Description: "Ten-digit customer ID of the account without hyphens; a " +
			"target of the connection or an account below a manager target"},
		{Name: "resource_names", Required: true, Description: "1 to 10 resource names of recommendations of this " +
			"account, customers/CUSTOMER_ID/recommendations/ID, each once; the customer must be the one of customer_id"},
	},
	Fields: []capability.Field{
		{Name: "customer_id", Description: "The account of the call"},
		{Name: "results", Description: "The resource names of the dismissed recommendations, in the order asked for"},
	},
	Examples: []capability.Example{{Description: "Dismiss one recommendation",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","resource_names":["customers/1234567890/recommendations/abc123"]}`)}},
}

func handleRecommendationsDismiss(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "dismiss recommendations"
	var args dismissArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	env, err := handleEnv(ctx, resolved, secrets, red, op, recommendationsDismissID)
	if err != nil {
		return nil, err
	}
	if err := args.check(); err != nil {
		return nil, err
	}
	operations := make([]any, 0, len(args.ResourceNames))
	for _, name := range args.ResourceNames {
		operations = append(operations, map[string]any{"resourceName": name})
	}
	c, err := env.settle(args.CustomerID, true)
	if err != nil {
		return nil, err
	}
	var answer recActionAnswer
	if err := c.mutate(ctx, mutation{op: op, tool: env.tool, customerID: args.CustomerID, service: recommendationService,
		action: "dismiss", body: map[string]any{"operations": operations}}, &answer); err != nil {
		return nil, err
	}
	names, err := recSettledNames(op, answer, args.ResourceNames)
	if err != nil {
		return nil, err
	}
	return recActionResult{CustomerID: args.CustomerID, Results: names}, nil
}

// recommendationDismissSummary phrases the change for the confirmation, from the arguments alone.
func recommendationDismissSummary(raw json.RawMessage) string {
	var a dismissArguments
	_ = json.Unmarshal(raw, &a)
	names := make([]string, 0, len(a.ResourceNames))
	for _, n := range a.ResourceNames {
		names = append(names, boundedText(n, 200))
	}
	return "Dismiss " + recItoa(len(names)) + " recommendation(s) of Google Ads account " + a.CustomerID + ": " +
		strings.Join(names, ", ") + ". Google stops showing them and the API cannot restore them."
}
