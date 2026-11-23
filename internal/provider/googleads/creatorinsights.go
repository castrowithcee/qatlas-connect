package googleads

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"slices"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	contentCreatorInsightsGenerateCreatorID  = Provider + ".content-creator-insights.generate-creator"
	contentCreatorInsightsGenerateTrendingID = Provider + ".content-creator-insights.generate-trending"

	// Google documents no bound for any list of these two methods. These are Qatlas's own, large enough for a
	// planning request and small enough to keep one call from becoming a bulk export.
	maxInsightCountries    = 25
	maxInsightSubCountries = 50
	maxInsightAttributes   = 50
	maxInsightGroups       = 20
	maxInsightEntities     = 20
	maxInsightChannels     = 50
	maxInsightGroupText    = 255
	// maxInsightNodes bounds the answer: up to 500 creators with channels, videos and audience attributes.
	maxInsightNodes = 200000
)

const (
	machineIDPatternText   = `^/[a-z]/[A-Za-z0-9_]{1,40}$`
	handlePatternText      = `^@[^\s\x00-\x1f\x7f"'\\/?#&]{1,100}$`
	userInterestPatternTxt = `^customers/[0-9]{1,10}/userInterests/[1-9][0-9]{0,18}$`
)

var userInterestPattern = regexp.MustCompile(userInterestPatternTxt)

var (
	insightGeoPattern = regexp.MustCompile(geoConstantPattern)
	machineIDPattern  = regexp.MustCompile(machineIDPatternText)
	handlePattern     = regexp.MustCompile(handlePatternText)
	categoryIDPattern = idPattern
)

// Enumerations of the attribute types. UNSPECIFIED and UNKNOWN are no input.
var (
	insightAgeRanges = []string{"AGE_RANGE_18_24", "AGE_RANGE_25_34", "AGE_RANGE_35_44", "AGE_RANGE_45_54",
		"AGE_RANGE_55_64", "AGE_RANGE_65_UP", "AGE_RANGE_UNDETERMINED"}
	insightGenders        = []string{"FEMALE", "MALE", "UNDETERMINED"}
	insightParentalStatus = []string{"NOT_A_PARENT", "PARENT", "UNDETERMINED"}
	insightIncomeRanges   = []string{"INCOME_RANGE_0_50", "INCOME_RANGE_50_60", "INCOME_RANGE_60_70", "INCOME_RANGE_70_80",
		"INCOME_RANGE_80_90", "INCOME_RANGE_90_UP", "INCOME_RANGE_UNDETERMINED"}
	insightDevices = []string{"CONNECTED_TV", "DESKTOP", "MOBILE", "OTHER", "TABLET"}
)

// insightAttributeArgument is one AudienceInsightsAttribute: exactly one member is set. Location, lineup,
// YouTube channel and video and user list are members of the type that neither method supports as an audience
// attribute, so they are not offered.
type insightAttributeArgument struct {
	AgeRange       string `json:"age_range"`
	Gender         string `json:"gender"`
	UserInterest   string `json:"user_interest"`
	Entity         string `json:"entity"`
	Category       string `json:"category"`
	Device         string `json:"device"`
	ParentalStatus string `json:"parental_status"`
	IncomeRange    string `json:"income_range"`
}

// body checks the one member and returns it in the form of the JSON mapping. A user interest is an object of
// an account, so it must belong to the account of the call.
func (a insightAttributeArgument) body(argument, customerID string, device bool) (map[string]any, error) {
	type member struct {
		name, value string
		body        func() map[string]any
		ok          bool
	}
	typed := func(key string, values []string, v string) member {
		return member{name: key, value: v, ok: slices.Contains(values, v),
			body: func() map[string]any { return map[string]any{insightCamel(key): map[string]any{"type": v}} }}
	}
	members := []member{
		typed("age_range", insightAgeRanges, a.AgeRange),
		typed("gender", insightGenders, a.Gender),
		{name: "user_interest", value: a.UserInterest,
			ok: strings.HasPrefix(a.UserInterest, customerName(customerID)+"/userInterests/") &&
				userInterestPattern.MatchString(a.UserInterest),
			body: func() map[string]any {
				return map[string]any{"userInterest": map[string]any{"userInterestCategory": a.UserInterest}}
			}},
		{name: "entity", value: a.Entity, ok: machineIDPattern.MatchString(a.Entity), body: func() map[string]any {
			return map[string]any{"entity": map[string]any{"knowledgeGraphMachineId": a.Entity}}
		}},
		{name: "category", value: a.Category, ok: categoryIDPattern.MatchString(a.Category), body: func() map[string]any {
			return map[string]any{"category": map[string]any{"categoryId": a.Category}}
		}},
		typed("parental_status", insightParentalStatus, a.ParentalStatus),
		typed("income_range", insightIncomeRanges, a.IncomeRange),
	}
	if device {
		members = append(members, typed("device", insightDevices, a.Device))
	} else if a.Device != "" {
		return nil, badRequest(argument + " must not name a device: this method does not take it")
	}
	var set []member
	for _, m := range members {
		if m.value != "" {
			set = append(set, m)
		}
	}
	if len(set) != 1 {
		return nil, badRequest(argument + " must hold exactly one attribute")
	}
	if !set[0].ok {
		return nil, badRequest(argument + "." + set[0].name + " is not a value Google documents, or names an object " +
			"of another account")
	}
	return set[0].body(), nil
}

// insightCamel turns age_range into ageRange.
func insightCamel(name string) string {
	parts := strings.Split(name, "_")
	for i := 1; i < len(parts); i++ {
		parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
	}
	return strings.Join(parts, "")
}

// insightAttributesBody checks a list of attributes. Entities, categories and user interests are refused where
// only demographics are allowed.
func insightAttributesBody(argument, customerID string, list []insightAttributeArgument, device, demographicsOnly bool,
) ([]any, error) {
	if len(list) > maxInsightAttributes {
		return nil, badRequest(argument + " takes at most 50 attributes")
	}
	out := make([]any, 0, len(list))
	for _, a := range list {
		if demographicsOnly && (a.Entity != "" || a.Category != "" || a.UserInterest != "") {
			return nil, badRequest(argument + " takes no entity, category or user_interest when audience_combinations " +
				"is given: Google refuses them in both lists")
		}
		b, err := a.body(argument, customerID, device)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, nil
}

type insightGroupArgument struct {
	Attributes []insightAttributeArgument `json:"attributes"`
}

// insightAudience is the audience of SearchAttributes and SearchAudience.
type insightAudience struct {
	AudienceAttributes   []insightAttributeArgument `json:"audience_attributes"`
	AudienceCombinations []insightGroupArgument     `json:"audience_combinations"`
}

func (a insightAudience) body(customerID string, device bool) (map[string]any, error) {
	out := map[string]any{}
	attributes, err := insightAttributesBody("audience_attributes", customerID, a.AudienceAttributes, device,
		len(a.AudienceCombinations) > 0)
	if err != nil {
		return nil, err
	}
	if len(attributes) > 0 {
		out["audienceAttributes"] = attributes
	}
	if len(a.AudienceCombinations) > maxInsightGroups {
		return nil, badRequest("audience_combinations takes at most 20 groups")
	}
	groups := []any{}
	for _, g := range a.AudienceCombinations {
		if len(g.Attributes) == 0 {
			return nil, badRequest("audience_combinations groups need at least one attribute")
		}
		for _, m := range g.Attributes {
			if m.AgeRange != "" || m.Gender != "" || m.Device != "" || m.ParentalStatus != "" || m.IncomeRange != "" {
				return nil, badRequest("audience_combinations groups take entity, category and user_interest only")
			}
		}
		list, err := insightAttributesBody("audience_combinations", customerID, g.Attributes, false, false)
		if err != nil {
			return nil, err
		}
		groups = append(groups, map[string]any{"attributes": list})
	}
	if len(groups) > 0 {
		out["audienceCombinations"] = groups
	}
	return out, nil
}

type insightTopics struct {
	Entities []string `json:"entities"`
}

func (t insightTopics) body() (map[string]any, error) {
	if len(t.Entities) == 0 || len(t.Entities) > maxInsightEntities {
		return nil, badRequest("search_topics.entities must name 1 to 20 Knowledge Graph entities")
	}
	list := make([]any, 0, len(t.Entities))
	for _, mid := range t.Entities {
		if !machineIDPattern.MatchString(mid) {
			return nil, badRequest("search_topics.entities must be machine IDs such as /m/09l6s")
		}
		list = append(list, map[string]any{"knowledgeGraphMachineId": mid})
	}
	return map[string]any{"entities": list}, nil
}

// insightLocations checks geo target constant names and returns them as LocationInfo objects.
func insightLocations(argument string, names []string, max int) ([]any, error) {
	if len(names) > max {
		return nil, badRequest(argument + " takes at most " + recItoa(max) + " locations")
	}
	out := make([]any, 0, len(names))
	for _, name := range names {
		if !insightGeoPattern.MatchString(name) {
			return nil, badRequest(argument + " must be geo target constants, geoTargetConstants/ID")
		}
		out = append(out, map[string]any{"geoTargetConstant": name})
	}
	return out, nil
}

// insightCommon is what both methods share.
type insightCommon struct {
	CustomerID          string   `json:"customer_id"`
	CustomerInsightsGrp string   `json:"customer_insights_group"`
	SubCountryLocations []string `json:"sub_country_locations"`
	SupplementalData    []string `json:"supplemental_data"`
}

// check validates what both methods share and fills the common part of the body.
func (c insightCommon) check(body map[string]any, allowed []string) error {
	if !customerIDPattern.MatchString(c.CustomerID) {
		return badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := recCheckText("customer_insights_group", c.CustomerInsightsGrp, maxInsightGroupText); err != nil {
		return err
	}
	body["customerInsightsGroup"] = c.CustomerInsightsGrp
	if len(c.SupplementalData) > len(allowed) {
		return badRequest("supplemental_data names a value more than once")
	}
	for i, v := range c.SupplementalData {
		if !slices.Contains(allowed, v) || slices.Contains(c.SupplementalData[:i], v) {
			return badRequest("supplemental_data takes each of " + strings.Join(allowed, ", ") + " at most once")
		}
	}
	if len(c.SupplementalData) > 0 {
		body["supplementalData"] = c.SupplementalData
	}
	if len(c.SubCountryLocations) > 0 && slices.Contains(c.SupplementalData, "LOCAL_CREATOR_DATA") {
		return badRequest("LOCAL_CREATOR_DATA is not supported together with sub_country_locations")
	}
	subs, err := insightLocations("sub_country_locations", c.SubCountryLocations, maxInsightSubCountries)
	if err != nil {
		return err
	}
	if len(subs) > 0 {
		body["subCountryLocations"] = subs
	}
	return nil
}

// --- generate-creator ---

type creatorChannels struct {
	IDs     []string `json:"youtube_channel_ids"`
	Handles []string `json:"youtube_channel_handles"`
}

type creatorArguments struct {
	insightCommon
	CountryLocations []string `json:"country_locations"`
	SearchAttributes *struct {
		insightAudience
		CreatorEntities []string `json:"creator_entities"`
	} `json:"search_attributes"`
	SearchChannels *creatorChannels `json:"search_channels"`
	SearchTopics   *insightTopics   `json:"search_topics"`
}

func (a creatorArguments) body() (map[string]any, error) {
	body := map[string]any{}
	if err := a.check(body, []string{"LOCAL_CREATOR_DATA"}); err != nil {
		return nil, err
	}
	if len(a.CountryLocations) == 0 {
		return nil, badRequest("country_locations must name at least one country")
	}
	countries, err := insightLocations("country_locations", a.CountryLocations, maxInsightCountries)
	if err != nil {
		return nil, err
	}
	body["countryLocations"] = countries
	set := 0
	if s := a.SearchAttributes; s != nil {
		set++
		criteria, err := s.body(a.CustomerID, true)
		if err != nil {
			return nil, err
		}
		if len(s.CreatorEntities) > maxInsightEntities {
			return nil, badRequest("search_attributes.creator_entities takes at most 20 entities")
		}
		list := []any{}
		for _, mid := range s.CreatorEntities {
			if !machineIDPattern.MatchString(mid) {
				return nil, badRequest("search_attributes.creator_entities must be machine IDs such as /m/09l6s")
			}
			list = append(list, map[string]any{"entity": map[string]any{"knowledgeGraphMachineId": mid}})
		}
		if len(list) > 0 {
			criteria["creatorAttributes"] = list
		}
		if len(criteria) == 0 {
			return nil, badRequest("search_attributes needs audience_attributes, audience_combinations or creator_entities")
		}
		body["searchAttributes"] = criteria
	}
	if c := a.SearchChannels; c != nil {
		set++
		if len(c.IDs)+len(c.Handles) == 0 || len(c.IDs) > maxInsightChannels || len(c.Handles) > maxInsightChannels {
			return nil, badRequest("search_channels needs 1 to 50 channel IDs or handles, each list at most 50")
		}
		criteria := map[string]any{}
		ids := []any{}
		for _, id := range c.IDs {
			if !channelIDPattern.MatchString(id) {
				return nil, badRequest("search_channels.youtube_channel_ids must be YouTube channel IDs")
			}
			ids = append(ids, map[string]any{"channelId": id})
		}
		for _, h := range c.Handles {
			if !handlePattern.MatchString(h) {
				return nil, badRequest("search_channels.youtube_channel_handles must start with @")
			}
		}
		if len(ids) > 0 {
			criteria["youtubeChannels"] = ids
		}
		if len(c.Handles) > 0 {
			criteria["youtubeChannelHandles"] = c.Handles
		}
		body["searchChannels"] = criteria
	}
	if t := a.SearchTopics; t != nil {
		set++
		criteria, err := t.body()
		if err != nil {
			return nil, err
		}
		if len(a.CountryLocations) != 1 {
			return nil, badRequest("search_topics allows one country in country_locations only")
		}
		body["searchTopics"] = criteria
	}
	if set != 1 {
		return nil, badRequest("name exactly one of search_attributes, search_channels and search_topics")
	}
	if len(a.SubCountryLocations) > 0 && a.SearchAttributes == nil {
		return nil, badRequest("sub_country_locations works with search_attributes only")
	}
	return body, nil
}

// --- generate-trending ---

type trendingArguments struct {
	insightCommon
	CountryLocation string           `json:"country_location"`
	SearchAudience  *insightAudience `json:"search_audience"`
	SearchTopics    *insightTopics   `json:"search_topics"`
}

func (a trendingArguments) body() (map[string]any, error) {
	body := map[string]any{}
	if err := a.check(body, []string{"BRAND_SENTIMENT_DATA", "LOCAL_CREATOR_DATA"}); err != nil {
		return nil, err
	}
	country, err := insightLocations("country_location", []string{a.CountryLocation}, 1)
	if err != nil {
		return nil, err
	}
	body["countryLocation"] = country[0]
	set := 0
	if s := a.SearchAudience; s != nil {
		set++
		if len(s.AudienceAttributes) == 0 {
			return nil, badRequest("search_audience.audience_attributes is required")
		}
		criteria, err := s.body(a.CustomerID, false)
		if err != nil {
			return nil, err
		}
		body["searchAudience"] = criteria
	}
	if t := a.SearchTopics; t != nil {
		set++
		criteria, err := t.body()
		if err != nil {
			return nil, err
		}
		body["searchTopics"] = criteria
	}
	if set != 1 {
		return nil, badRequest("name exactly one of search_audience and search_topics")
	}
	if len(a.SubCountryLocations) > 0 && a.SearchAudience == nil {
		return nil, badRequest("sub_country_locations works with search_audience only")
	}
	return body, nil
}

// --- shared answer and schema ---

// insightResult is a cleaned answer with the account of the call; noCustomer leaves it out for a call that acts
// on no account.
type insightResult struct {
	CustomerID string
	noCustomer bool
	answer     map[string]any
}

func (r insightResult) MarshalJSON() ([]byte, error) {
	out := map[string]any{}
	if !r.noCustomer {
		out["customer_id"] = r.CustomerID
	}
	for k, v := range r.answer {
		out[k] = v
	}
	return json.Marshal(out)
}

// cleanListAnswer reads an answer whose members are lists of objects into bounded plain data. Members of
// lists named in keys are always present. truncated is set when a list was longer than Qatlas Connect reads.
func cleanListAnswer(op, customerID, what string, maxNodes int, raw json.RawMessage, keys ...string) (map[string]any, bool,
	error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value map[string]any
	if err := decoder.Decode(&value); err != nil || value == nil {
		return nil, false, provider.InvalidResponse(op, subject+" returned an answer Qatlas Connect cannot read")
	}
	truncated := false
	for _, v := range value {
		if list, ok := v.([]any); ok && len(list) > maxListItems {
			truncated = true
		}
	}
	c := &recommendationCleaner{op: op, customerID: customerID, what: what, maxNodes: maxNodes}
	cleaned, _, err := c.clean(value, 0)
	if err != nil {
		return nil, false, err
	}
	out := cleaned.(map[string]any)
	for _, key := range keys {
		if _, ok := out[key]; !ok {
			out[key] = []any{}
		}
	}
	return out, truncated, nil
}

var insightObject = schema{"type": "object"}

func insightMemberSchemas() map[string]schema {
	return map[string]schema{
		"age_range":       recEnumSchema(insightAgeRanges),
		"gender":          recEnumSchema(insightGenders),
		"user_interest":   schema{"type": "string", "pattern": userInterestPatternTxt},
		"entity":          schema{"type": "string", "pattern": machineIDPatternText},
		"category":        schema{"type": "string", "pattern": idPatternText},
		"device":          recEnumSchema(insightDevices),
		"parental_status": recEnumSchema(insightParentalStatus),
		"income_range":    recEnumSchema(insightIncomeRanges),
	}
}

// insightAttributeSchema is one attribute with exactly one of the named members.
func insightAttributeSchema(members ...string) schema {
	all := insightMemberSchemas()
	props := schema{}
	for _, m := range members {
		props[m] = all[m]
	}
	s := object(props)
	s["minProperties"], s["maxProperties"] = 1, 1
	return s
}

var (
	insightDemographics = []string{"age_range", "gender", "parental_status", "income_range"}
	insightInterests    = []string{"user_interest", "entity", "category"}
)

func insightAudienceProperties(device bool) schema {
	members := append(slices.Clone(insightDemographics), insightInterests...)
	if device {
		members = append(members, "device")
	}
	return schema{
		"audience_attributes": recListOf(insightAttributeSchema(members...), maxInsightAttributes),
		"audience_combinations": recListOf(object(schema{"attributes": recListOf(
			insightAttributeSchema(insightInterests...), maxInsightAttributes)}, "attributes"), maxInsightGroups),
	}
}

var insightTopicsSchema = object(schema{"entities": recListOf(schema{"type": "string", "pattern": machineIDPatternText},
	maxInsightEntities)}, "entities")

func insightCommonProperties() schema {
	return schema{
		"customer_id":             customerIDArgument,
		"customer_insights_group": recTextSchema(maxInsightGroupText),
		"sub_country_locations": recListOf(schema{"type": "string", "pattern": geoConstantPattern},
			maxInsightSubCountries),
	}
}

var insightCommonArguments = []capability.Argument{
	{Name: "customer_id", Required: true, Description: "Ten-digit customer ID of the account without hyphens; a " +
		"target of the connection or an account below a manager target"},
	{Name: "customer_insights_group", Required: true, Description: "Name of the customer or brand the request plans " +
		"for, a value of your choice, 1 to 255 characters; Google documents no format"},
	{Name: "sub_country_locations", Description: "Up to 50 sub-country locations as geo target constants, " +
		"geoTargetConstants/ID, that narrow the search; not together with LOCAL_CREATOR_DATA"},
}

const insightAttributeNote = "an attribute is an object with exactly one member: age_range, gender, " +
	"parental_status, income_range, user_interest (an interest of this account, customers/CUSTOMER_ID/userInterests/ID), " +
	"entity (a Knowledge Graph machine ID such as /m/09l6s) or category (a Product and Service category ID)"

const insightRisk = "YouTube creators and channels, their public metrics, and, where the creator consented to share " +
	"them, non-public metrics and audience attributes; no data of the account"

const insightSuffix = "The service is in beta and open to allowlisted customers only. Google limits each method to " +
	"5 queries per second. Costs one operation of the daily quota. The answer is untrusted third-party content: " +
	"text is cut, lists are cut at 500 entries with truncated set, and resource names of another account are " +
	"refused. Addresses in the answer are text and are never fetched."

func insightRiskOf() capability.Risk {
	return capability.Risk{Effect: capability.EffectRead, Idempotency: capability.IdempotencySafe,
		Confirmation: capability.ConfirmationNone, OpenWorld: true, DataSensitivity: insightRisk}
}

func propsWith(base schema, extra schema) schema {
	out := schema{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

var contentCreatorInsightsGenerateCreator = capability.Descriptor{
	ID: contentCreatorInsightsGenerateCreatorID, Version: 1, Title: "Generate YouTube creator insights",
	Description: "Return insights for YouTube creators and channels " +
		"(ContentCreatorInsightsService.GenerateCreatorInsights of API version v26, POST " +
		"customers/{customer_id}:generateCreatorInsights): the creators with their channels, channel metrics, " +
		"audience and content attributes, top videos and languages. Name the countries in country_locations and " +
		"exactly one criterion: search_attributes (an audience of viewers by audience_attributes or " +
		"audience_combinations, and creator_entities for the content), search_channels (channel IDs and " +
		"handles that start with @) or search_topics (Knowledge Graph entities of one country). " + insightAttributeNote +
		". audience_combinations are groups: entities, categories and interests inside a group are OR, the groups are " +
		"AND, and these three types then go in the groups only. Nothing in the account changes. The optional " +
		"insights_application_info of the method, which names the calling application, is not offered. " + insightSuffix,
	Tags: []string{"google", "ads", "insights", "youtube", "creators"}, Provider: Provider, Risk: insightRiskOf(),
	InputSchema: encoded(object(propsWith(insightCommonProperties(), schema{
		"country_locations": recListOf(schema{"type": "string", "pattern": geoConstantPattern}, maxInsightCountries),
		"supplemental_data": schema{"type": "array", "minItems": 1, "maxItems": 1, "uniqueItems": true,
			"items": recEnumSchema([]string{"LOCAL_CREATOR_DATA"})},
		"search_attributes": object(propsWith(insightAudienceProperties(true), schema{
			"creator_entities": recListOf(schema{"type": "string", "pattern": machineIDPatternText}, maxInsightEntities),
		})),
		"search_channels": object(schema{
			"youtube_channel_ids":     recListOf(schema{"type": "string", "pattern": channelIDPattern.String()}, maxInsightChannels),
			"youtube_channel_handles": recListOf(schema{"type": "string", "pattern": handlePatternText}, maxInsightChannels),
		}),
		"search_topics": insightTopicsSchema,
	}), "customer_id", "customer_insights_group", "country_locations")),
	OutputSchema: encoded(object(schema{"customer_id": stringSchema, "creator_insights": arrayOf(insightObject),
		"local_creator_insights": arrayOf(insightObject), "truncated": booleanSchema},
		"customer_id", "creator_insights", "local_creator_insights")),
	Arguments: append(slices.Clone(insightCommonArguments), []capability.Argument{
		{Name: "country_locations", Required: true, Description: "1 to 25 countries as geo target constants, " +
			"geoTargetConstants/ID; exactly one for search_topics"},
		{Name: "supplemental_data", Description: "LOCAL_CREATOR_DATA adds local_creator_insights for a search_topics " +
			"entity with the creator topic capability"},
		{Name: "search_attributes", Description: "Criterion: audience_attributes (up to 50), audience_combinations " +
			"(up to 20 groups with attributes) and creator_entities (up to 20 machine IDs of entities tagged as creator " +
			"attributes); " + insightAttributeNote + "; device is allowed here"},
		{Name: "search_channels", Description: "Criterion: youtube_channel_ids and youtube_channel_handles, each up " +
			"to 50; a handle starts with @"},
		{Name: "search_topics", Description: "Criterion: entities, 1 to 20 Knowledge Graph machine IDs tagged for " +
			"creator topic or trending insights"},
	}...),
	Fields: []capability.Field{
		{Name: "customer_id", Description: "The account of the call"},
		{Name: "creator_insights", Description: "Per creator: creator_name and creator_channels, each channel with " +
			"display_name, youtube_channel (channel_id), channel_url, channel_description, handle, thumbnail_url, " +
			"publish_date, country_location, channel_metrics (subscriber_count, views_count, video_count, likes_count, " +
			"shares_count, comments_count, engagement_rate, average_views_per_video, average_likes_per_video, " +
			"average_shares_per_video, average_comments_per_video, shorts_views_count, shorts_video_count, " +
			"is_active_shorts_creator, is_active_live_stream_creator, partnership_opportunities), " +
			"data_sharing_consent_given, channel_audience_attributes, channel_attributes, top_videos (attributes with " +
			"dimension, attribute, display_name, display_info, potential_youtube_reach, subscriber_share, viewer_share " +
			"and the metadata of the dimension), language_distributions, channel_type and relevance_score"},
		{Name: "local_creator_insights", Description: "Local creators in the form of creator_insights; filled for " +
			"search_topics with LOCAL_CREATOR_DATA only"},
		{Name: "truncated", Description: "true when a list held more than 500 entries and the rest was cut"},
	},
	Examples: []capability.Example{{Description: "Find creators for an audience in Germany",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","customer_insights_group":"Spring campaign",` +
			`"country_locations":["geoTargetConstants/2276"],"search_attributes":{"audience_attributes":` +
			`[{"age_range":"AGE_RANGE_25_34"},{"gender":"FEMALE"}]}}`)}},
}

var contentCreatorInsightsGenerateTrending = capability.Descriptor{
	ID: contentCreatorInsightsGenerateTrendingID, Version: 1, Title: "Generate YouTube trending insights",
	Description: "Return insights about trending content on YouTube " +
		"(ContentCreatorInsightsService.GenerateTrendingInsights of API version v26, POST " +
		"customers/{customer_id}:generateTrendingInsights): the trend of each attribute with its metrics, the " +
		"monthly history, brand sentiment, related videos and related creators. Name one country in country_location " +
		"and exactly one criterion: search_audience (an audience of viewers by audience_attributes, required, and " +
		"audience_combinations) or search_topics (Knowledge Graph entities tagged for trending insights). " +
		insightAttributeNote + ", without device. Nothing in the account changes. The optional " +
		"insights_application_info of the method, which names the calling application, is not offered. " + insightSuffix,
	Tags: []string{"google", "ads", "insights", "youtube", "trends"}, Provider: Provider, Risk: insightRiskOf(),
	InputSchema: encoded(object(propsWith(insightCommonProperties(), schema{
		"country_location": schema{"type": "string", "pattern": geoConstantPattern},
		"supplemental_data": schema{"type": "array", "minItems": 1, "maxItems": 2, "uniqueItems": true,
			"items": recEnumSchema([]string{"BRAND_SENTIMENT_DATA", "LOCAL_CREATOR_DATA"})},
		"search_audience": object(insightAudienceProperties(false)),
		"search_topics":   insightTopicsSchema,
	}), "customer_id", "customer_insights_group", "country_location")),
	OutputSchema: encoded(object(schema{"customer_id": stringSchema, "trend_insights": arrayOf(insightObject),
		"truncated": booleanSchema}, "customer_id", "trend_insights")),
	Arguments: append(slices.Clone(insightCommonArguments), []capability.Argument{
		{Name: "country_location", Required: true, Description: "The country to find trends in, as a geo target " +
			"constant, geoTargetConstants/ID"},
		{Name: "supplemental_data", Description: "BRAND_SENTIMENT_DATA adds brand sentiment for a search_topics entity " +
			"with the brand capability, LOCAL_CREATOR_DATA adds related local creators for a creator topic entity; " +
			"not together with sub_country_locations"},
		{Name: "search_audience", Description: "Criterion: audience_attributes (1 to 50, required) and " +
			"audience_combinations (up to 20 groups with attributes); " + insightAttributeNote + "; no device"},
		{Name: "search_topics", Description: "Criterion: entities, 1 to 20 Knowledge Graph machine IDs tagged for " +
			"trending insights"},
	}...),
	Fields: []capability.Field{
		{Name: "customer_id", Description: "The account of the call"},
		{Name: "trend_insights", Description: "Per trend: trend_attribute (an attribute with its metadata), " +
			"trend_metrics (views_count, views_indexed_value, audience_share, trend_change_percent), trend " +
			"(EMERGING or SUSTAINED), trend_data_points (month and trend_metrics), brand_sentiment_insights (month, " +
			"has_insufficient_data, sentiment_distributions, sentiment_summaries with sample_videos), related_videos, " +
			"related_creators and related_local_creators in the form of the creator insights"},
		{Name: "truncated", Description: "true when a list held more than 500 entries and the rest was cut"},
	},
	Examples: []capability.Example{{Description: "Trends for a topic in Germany",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","customer_insights_group":"Spring campaign",` +
			`"country_location":"geoTargetConstants/2276","search_topics":{"entities":["/m/09l6s"]}}`)}},
}

func handleCreatorInsights(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "generate creator insights"
	var args creatorArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	env, err := handleEnv(ctx, resolved, secrets, red, op, contentCreatorInsightsGenerateCreatorID)
	if err != nil {
		return nil, err
	}
	body, err := args.body()
	if err != nil {
		return nil, err
	}
	return env.insights(args.CustomerID, ":generateCreatorInsights", body, "creator_insights", "local_creator_insights")
}

func handleTrendingInsights(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "generate trending insights"
	var args trendingArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	env, err := handleEnv(ctx, resolved, secrets, red, op, contentCreatorInsightsGenerateTrendingID)
	if err != nil {
		return nil, err
	}
	body, err := args.body()
	if err != nil {
		return nil, err
	}
	return env.insights(args.CustomerID, ":generateTrendingInsights", body, "trend_insights")
}

// insights binds the account, sends the request and cleans the answer. The target check comes first, so a
// refused account reads no secret and sends nothing.
func (e callEnv) insights(customerID, action string, body map[string]any, keys ...string) (any, error) {
	encodedBody, err := json.Marshal(body)
	if err != nil {
		return nil, failure(e.op, "the request body could not be encoded")
	}
	c, err := e.settle(customerID, false)
	if err != nil {
		return nil, err
	}
	var answer json.RawMessage
	if err := c.read(e.ctx, request{op: e.op, tool: e.tool, method: http.MethodPost,
		path: "/" + customerName(customerID) + action, body: encodedBody, onAccount: true, judgesInput: true},
		&answer); err != nil {
		return nil, err
	}
	cleaned, truncated, err := cleanListAnswer(e.op, customerID, "result", maxInsightNodes, answer, keys...)
	if err != nil {
		return nil, err
	}
	if truncated {
		cleaned["truncated"] = true
	}
	return insightResult{CustomerID: customerID, answer: cleaned}, nil
}
