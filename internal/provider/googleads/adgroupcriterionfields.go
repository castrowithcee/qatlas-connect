package googleads

import (
	"slices"
	"strings"
	"unicode/utf8"
)

// The members of the AdGroupCriterion resource of v26 that ad-group-criteria.create and .update offer. Not
// offered:
//   - the members Google fills itself (output only): criterion_id, display_name, type, the approval and
//     serving states, the quality information, the position estimates, the effective bids and their sources,
//     labels, primary_status and its reasons, require_eu_political_advertising_declaration; they are read
//     with reports.search. The resource has no deprecated member
//   - the deprecated listing dimensions product_grouping, product_labels, product_legacy_condition and
//     product_type_full, which Google supports only in Display campaigns
//   - topic.path, which Google derives from the topic constant, and the read-only members of the webpage
//     criterion (coverage_percentage, sample)
//   - the criterion types of the campaign or customer level, which AdGroupCriterion does not hold (device,
//     ad schedule, proximity, IP block and others); ad group device adjustments are bid modifiers

const (
	maxAdGroupCriteria       = 50
	maxCriterionURLs         = 20
	maxCriterionParams       = 50
	maxCriterionConditions   = 20
	maxKeywordRunes          = 80
	maxKeywordBytes          = 255
	maxCriterionKeywordWords = 10
	maxPlacementRunes        = 250
	maxMobileApplicationID   = 200
	maxListingPath           = 20
	maxExemptionKeys         = 20
	criterionResourcePrefix  = "adGroupCriteria"
)

// criterionTypes are the members of the criterion union, of which exactly one is given on creation.
var criterionTypes = []string{"age_range", "app_payment_model", "audience", "brand_list", "combined_audience",
	"custom_affinity", "custom_audience", "custom_intent", "entity_bid", "extended_demographic", "gender",
	"income_range", "keyword", "language", "life_event", "listing_group", "location", "mobile_app_category", "mobile_application",
	"parental_status", "placement", "retail_filter_bundle", "topic", "user_interest", "user_list",
	"vertical_ads_item_group_rule_list", "video_lineup", "webpage", "youtube_channel", "youtube_video"}

// criterionRequired are the members a criterion type needs beyond the one member any of them has.
var criterionRequired = map[string][]string{
	"keyword":            {"text", "match_type"},
	"listing_group":      {"type"},
	"mobile_application": {"app_id"},
	"webpage":            {"criterion_name", "conditions"},
}

// webpageConditionRequired are the members every webpage condition needs.
var webpageConditionRequired = []string{"operand", "operator", "argument"}

// negativeRefused are the members a negative criterion does not take: it excludes, so a bid, a bid modifier and
// URLs have no meaning for it.
var negativeRefused = []string{"bid_modifier", "cpc_bid_micros", "cpm_bid_micros", "cpv_bid_micros",
	"percent_cpc_bid_micros", "final_urls", "final_mobile_urls", "final_url_suffix", "tracking_url_template",
	"url_custom_parameters"}

// The enums below are in no field of the catalog; their values are the ones of the reference of v26.
var (
	adGroupWebpageOperands  = []string{"CATEGORY", "CUSTOM_LABEL", "PAGE_CONTENT", "PAGE_TITLE", "URL"}
	adGroupWebpageOperators = []string{"CONTAINS", "EQUALS"}
)

const (
	geoConstantPattern     = `^geoTargetConstants/[1-9][0-9]{0,19}$`
	languageConstantPat    = `^languageConstants/[1-9][0-9]{0,19}$`
	appCategoryPattern     = `^mobileAppCategoryConstants/[1-9][0-9]{0,19}$`
	topicConstantPattern   = `^topicConstants/[1-9][0-9]{0,19}$`
	mobileApplicationIDPat = `^[12]-[A-Za-z0-9._]{1,198}$`
	youtubeVideoPattern    = `^[A-Za-z0-9_-]{11}$`
	youtubeChannelPattern  = `^[A-Za-z0-9_-]{1,64}$`
)

// checkAdGroupKeywordText refuses keyword text Google would: empty, longer than 80 characters or 255 bytes, more than
// ten words, or holding a control character.
func checkAdGroupKeywordText(argument, value string) error {
	if value == "" || hasControl(value) || utf8.RuneCountInString(value) > maxKeywordRunes ||
		len(value) > maxKeywordBytes || len(strings.Fields(value)) > maxCriterionKeywordWords {
		return badRequest(argument + " must be text of 1 to 80 characters, at most 255 bytes and 10 words, without control characters")
	}
	return nil
}

// checkAdGroupPlacement refuses a placement that is empty, longer than 250 characters or holds a control character.
func checkAdGroupPlacement(argument, value string) error {
	if value == "" || hasControl(value) || utf8.RuneCountInString(value) > maxPlacementRunes {
		return badRequest(argument + " must be text of 1 to 250 characters without control characters")
	}
	return nil
}

// adGroupConstantNode is a resource of Google that belongs to no account: its form is checked, not its owner.
func adGroupConstantNode(name, desc, pattern string) node {
	return patternNode(name, desc, pattern, 64)
}

func sharedSetNode(desc string) node {
	return resourceNode("shared_set", desc, "sharedSets")
}

// listingDimensionNames are the members of the union of one listing dimension.
var listingDimensionNames = []string{"hotel_id", "hotel_class", "hotel_country_region", "hotel_state", "hotel_city",
	"product_category", "product_brand", "product_channel", "product_channel_exclusivity", "product_condition",
	"product_custom_attribute", "product_item_id", "product_type", "activity_id", "activity_rating", "activity_country",
	"activity_state", "activity_city", "unknown_listing_dimension"}

// adGroupListingDimension is one dimension of a listing group: exactly one of the members.
func adGroupListingDimension(name, desc string) node {
	const base = "ad_group_criterion.listing_group.case_value."
	geo := func(member string) node {
		return adGroupConstantNode(member, "Geo target constant of Google, geoTargetConstants/N", geoConstantPattern)
	}
	value := func(desc string) node { return textNode("value", desc, maxNameArgument) }
	rating := func(desc string) node { return wholeNode("value", desc, 1, 5) }
	return oneOf(objectNode(name, desc,
		objectNode("hotel_id", "Advertiser-specific hotel ID", value("Hotel ID")),
		objectNode("hotel_class", "Class of the hotel in stars", rating("Stars from 1 to 5")),
		objectNode("hotel_country_region", "Country or region of the hotel", geo("country_region_criterion")),
		objectNode("hotel_state", "State of the hotel", geo("state_criterion")),
		objectNode("hotel_city", "City of the hotel", geo("city_criterion")),
		objectNode("product_category", "Category of a product offer",
			wholeNode("category_id", "ID of the product category of Google", 1, math64),
			enumNode("level", "Level of the category", enumOf(base+"product_category.level"))),
		objectNode("product_brand", "Brand of a product offer", value("Brand")),
		objectNode("product_channel", "Locality of a product offer",
			enumNode("channel", "Locality", enumOf(base+"product_channel.channel"))),
		objectNode("product_channel_exclusivity", "Availability of a product offer",
			enumNode("channel_exclusivity", "Availability", enumOf(base+"product_channel_exclusivity.channel_exclusivity"))),
		objectNode("product_condition", "Condition of a product offer",
			enumNode("condition", "Condition", enumOf(base+"product_condition.condition"))),
		objectNode("product_custom_attribute", "Custom attribute of a product offer", value("Value"),
			enumNode("index", "Index of the attribute", enumOf(base+"product_custom_attribute.index"))),
		objectNode("product_item_id", "Item ID of a product offer", value("Item ID")),
		objectNode("product_type", "Type of a product offer", value("Type"),
			enumNode("level", "Level of the type", enumOf(base+"product_type.level"))),
		objectNode("activity_id", "Advertiser-specific activity ID", value("Activity ID")),
		objectNode("activity_rating", "Rating of the activity", rating("Rating from 1 to 5, 5 is the best")),
		objectNode("activity_country", "Country of the activity", geo("value")),
		objectNode("activity_state", "State of the activity", geo("value")),
		objectNode("activity_city", "City of the activity", geo("value")),
		objectNode("unknown_listing_dimension", "The rest: no other dimension is set"),
	), listingDimensionNames...)
}

func criterionNodes() []node {
	keywordText := textNode("text", "Keyword text, at most 80 characters, 255 bytes and 10 words; Google allows an asterisk only in a negative keyword", maxKeywordRunes)
	keywordText.check = checkAdGroupKeywordText
	placementURL := textNode("url", "Placement: a site, such as www.example.com, or a page two levels deep at most, up to 250 characters; Google refuses adsenseformobileapps.com", maxPlacementRunes)
	placementURL.check = checkAdGroupPlacement
	return []node{
		objectNode("age_range", "Age range", enumNode("type", "Age range", enumOf("ad_group_criterion.age_range.type"))),
		objectNode("app_payment_model", "App payment model; targeting only",
			enumNode("type", "Payment model", enumOf("ad_group_criterion.app_payment_model.type"))),
		objectNode("audience", "Audience of this account; targeting only, and Google needs audience_setting.use_audience_grouped on the ad group",
			resourceNode("audience", "Audience of this account, customers/ID/audiences/N", "audiences")),
		objectNode("brand_list", "Brand list; targeting only at the ad group level",
			sharedSetNode("Shared set of the brand list in this account, customers/ID/sharedSets/N")),
		objectNode("combined_audience", "Combined audience; targeting only",
			resourceNode("combined_audience", "Combined audience of this account, customers/ID/combinedAudiences/N", "combinedAudiences")),
		objectNode("custom_affinity", "Custom affinity; targeting only",
			resourceNode("custom_affinity", "Custom interest of this account, customers/ID/customInterests/N", "customInterests")),
		objectNode("custom_audience", "Custom audience; targeting only",
			resourceNode("custom_audience", "Custom audience of this account, customers/ID/customAudiences/N", "customAudiences")),
		objectNode("custom_intent", "Custom intent; targeting only",
			resourceNode("custom_intent", "Custom interest of this account, customers/ID/customInterests/N", "customInterests")),
		objectNode("entity_bid", "Bid on one travel entity such as a hotel; targeting only",
			textNode("item_code", "ID of the travel entity", maxNameArgument)),
		objectNode("extended_demographic", "Extended demographic group",
			wholeNode("extended_demographic_id", "Taxonomy ID of the group", 1, math64)),
		objectNode("gender", "Gender", enumNode("type", "Gender", enumOf("ad_group_criterion.gender.type"))),
		objectNode("income_range", "Income range", enumNode("type", "Income range", enumOf("ad_group_criterion.income_range.type"))),
		objectNode("keyword", "Keyword", keywordText,
			enumNode("match_type", "Match type", enumOf("ad_group_criterion.keyword.match_type"))),
		objectNode("language", "Language; targeting only, and Google refuses it in Search campaigns",
			adGroupConstantNode("language_constant", "Language constant of Google, languageConstants/N", languageConstantPat)),
		objectNode("life_event", "Life event", wholeNode("life_event_id", "Taxonomy ID of the life event", 1, math64)),
		objectNode("listing_group", "Listing group of Hotel and Shopping ads, targeting only. A new tree, with a parent and its children in one request, needs temporary IDs and cannot be created; a unit is created under an existing subdivision",
			enumNode("type", "SUBDIVISION splits further, UNIT is a leaf that can have bids", enumOf("ad_group_criterion.listing_group.type")),
			adGroupListingDimension("case_value", "Dimension with which this group refines its parent; none for the root"),
			patternNode("parent_ad_group_criterion", "Subdivision of the same ad group and account this group refines, customers/ID/adGroupCriteria/ADGROUPID~CRITERIONID; none for the root", criterionNamePattern, 100),
			objectNode("path", "Complete path of dimensions from the root, without the root",
				listOf(adGroupListingDimension("dimensions", ""), maxListingPath))),
		objectNode("location", "Location",
			adGroupConstantNode("geo_target_constant", "Geo target constant of Google, geoTargetConstants/N", geoConstantPattern)),
		objectNode("mobile_app_category", "Mobile app category",
			adGroupConstantNode("mobile_app_category_constant", "Mobile app category constant of Google, mobileAppCategoryConstants/N", appCategoryPattern)),
		objectNode("mobile_application", "Mobile application",
			patternNode("app_id", "App as 1-NINE_DIGIT_ID for iOS or 2-PACKAGE_NAME for Android", mobileApplicationIDPat, maxMobileApplicationID),
			textNode("name", "Name of the app", maxNameArgument)),
		objectNode("parental_status", "Parental status", enumNode("type", "Parental status", enumOf("ad_group_criterion.parental_status.type"))),
		objectNode("placement", "Placement", placementURL),
		objectNode("retail_filter_bundle", "Retail filter bundle; Google offers it to allowlisted accounts only",
			sharedSetNode("Shared set of the retail filter in this account, customers/ID/sharedSets/N")),
		objectNode("topic", "Topic",
			adGroupConstantNode("topic_constant", "Topic constant of Google, topicConstants/N", topicConstantPattern)),
		objectNode("user_interest", "User interest",
			resourceNode("user_interest_category", "User interest of this account, customers/ID/userInterests/N", "userInterests")),
		objectNode("user_list", "User list",
			resourceNode("user_list", "User list of this account, customers/ID/userLists/N", "userLists")),
		objectNode("vertical_ads_item_group_rule_list", "Rule list of item groups in vertical ads, for Search campaigns with a travel feed",
			sharedSetNode("Shared set of the rule list in this account, customers/ID/sharedSets/N")),
		objectNode("video_lineup", "Video lineup", wholeNode("video_lineup_id", "ID of the lineup", 1, math64)),
		objectNode("webpage", "Webpage of the website of the campaign",
			textNode("criterion_name", "Name of the criterion; required", maxNameArgument),
			listOf(objectNode("conditions", "Conditions that are all met; at least one is needed, so that a criterion never covers every page by accident",
				enumNode("operand", "Part of the page the condition looks at", adGroupWebpageOperands),
				enumNode("operator", "Comparison", adGroupWebpageOperators),
				textNode("argument", "Value compared with", maxNameArgument)), maxCriterionConditions)),
		objectNode("youtube_channel", "YouTube channel",
			patternNode("channel_id", "ID of the channel or its channel code", youtubeChannelPattern, 64)),
		objectNode("youtube_video", "YouTube video",
			patternNode("video_id", "ID of the video as in the watch page address", youtubeVideoPattern, 11)),
	}
}

func adGroupCriterionMembers() []node {
	bidModifier := node{name: "bid_modifier", kind: kindNumber, bounded: true, low: 0.1, high: 10,
		desc: "Factor on the bid when the criterion matches, from 0.1 to 10; changes what a click costs"}
	finalURLs := textNode("final_urls", "Final URLs, each an https address without user info; a list replaces the current one", maxURLArgument)
	finalURLs.check = requireHTTPS
	finalMobileURLs := textNode("final_mobile_urls", "Final mobile URLs, each an https address without user info; a list replaces the current one", maxURLArgument)
	finalMobileURLs.check = requireHTTPS
	members := []node{
		createOnly(resourceNode("ad_group", "Ad group of this account the criterion belongs to, customers/ID/adGroups/N; fixed after creation", "adGroups")),
		createOnly(flagNode("negative", "true to exclude the criterion, false (the default) to target it; fixed after creation, and Google does not update a negative criterion")),
		enumNode("status", "ENABLED serves and bids within the budget of the campaign; PAUSED does not. Required for a criterion that targets; a negative criterion takes ENABLED or none. A criterion is removed with ad-group-criteria.remove",
			[]string{"ENABLED", "PAUSED"}),
		bidModifier,
		microsNode("cpc_bid_micros", "Maximum CPC bid in micros of the account currency; Google validates it against the bidding of the ad group"),
		microsNode("cpm_bid_micros", "Maximum CPM bid in micros of the account currency"),
		microsNode("cpv_bid_micros", "Maximum CPV bid in micros of the account currency"),
		wholeNode("percent_cpc_bid_micros", "Percent CPC bid as a fraction from 0 up to, not including, 1, in millionths: 500000 is 50 percent", 0, 999_999),
		listOf(finalURLs, maxCriterionURLs),
		listOf(finalMobileURLs, maxCriterionURLs),
		addressNode("final_url_suffix", "Parameters appended to final URLs, such as utm_source=x, no address, up to 2048 bytes; empty on an update clears it", checkSuffix),
		addressNode("tracking_url_template", "Tracking template: an https address without user info, up to 2048 bytes, ValueTrack parameters such as {lpurl} after the host; empty on an update clears it", checkAddress),
		listOf(objectNode("url_custom_parameters", "Custom parameters for the tracking template and the URLs; the list replaces the current one",
			patternNode("key", "Name of the parameter tag", keyPattern, 64), textNode("value", "Value substituted for the tag", maxNameArgument)), maxCriterionParams),
	}
	for _, n := range criterionNodes() {
		members = append(members, createOnly(n))
	}
	return members
}

// adGroupCriterionTree holds the members of an ad group criterion that the tools offer; the criterion types are
// one union.
var adGroupCriterionTree = oneOf(objectNode("ad_group_criterion", "", adGroupCriterionMembers()...), criterionTypes...)

// adGroupCriterionOutputTree is the criterion Google returns with MUTABLE_RESOURCE: the members the tools
// offer and the resource name.
var adGroupCriterionOutputTree = func() node {
	t := adGroupCriterionTree
	t.fields = append(slices.Clone(t.fields), textNode("resource_name", "", maxNameArgument))
	return t
}()
