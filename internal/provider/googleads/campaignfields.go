package googleads

import "strings"

// The members of the Campaign resource of v26 that campaigns.create and campaigns.update offer. Not offered:
//   - the members Google fills itself (output only), and the members of the resource that are deprecated:
//     TargetSpend.target_spend_micros, and ShoppingSetting.use_vehicle_inventory, which Google no longer
//     supports
//   - bidding_strategy_type and the other read-only status members: they are read with reports.search
//   - fixed_cpm and the reservation self-disclosure: the bidding of a reservation campaign that is booked,
//     and booking is a billing action that is read-only
//   - third_party_integration_partners and shopping_setting.advertising_partner_ids: they hand the data of the
//     campaign to accounts and partners outside this one
//   - the phone numbers of the Local Services settings, which are personal data
//   - target_restriction_operations, the delta form of target_restrictions

const (
	maxCampaigns      = 10
	maxCampaignList   = 50
	maxCampaignParams = 50
)

var (
	colorPattern     = `^#[0-9A-Fa-f]{6}$`
	hostPattern      = `^[A-Za-z0-9]([A-Za-z0-9.-]{0,251}[A-Za-z0-9])?$`
	languagePattern  = `^[A-Za-z]{2,8}(-[A-Za-z0-9]{1,8})*$`
	feedLabelPattern = `^[A-Za-z0-9_-]{0,20}$`
	keyPattern       = `^[A-Za-z0-9_]{1,64}$`

	// The enums below are in no field of the catalog; their values are the ones of the reference of v26.
	frequencyLevels           = []string{"AD_GROUP", "AD_GROUP_AD", "CAMPAIGN"}
	frequencyEvents           = []string{"IMPRESSION", "VIDEO_VIEW"}
	frequencyUnits            = []string{"DAY", "WEEK", "MONTH"}
	messagingRestrictionTypes = []string{"RESTRICTION_BASED_EXCLUSION"}
	automationStatuses        = []string{"OPTED_IN", "OPTED_OUT"}
	crawlPlatforms            = []string{"LANDING_PAGE", "SOCIAL", "YOUTUBE"}
	sequenceInteractions      = []string{"ENGAGED_IMPRESSION", "IMPRESSION", "PAID_VIEW", "SKIP"}
	automationTypes           = []string{"AUTOMATED_VIDEO_CRAWL", "FINAL_URL_EXPANSION_TEXT_ASSET_AUTOMATION",
		"GENERATE_ANIMATED_IMAGES_FROM_OTHER_ASSETS", "GENERATE_DESIGN_VERSIONS_FOR_IMAGES",
		"GENERATE_ENHANCED_YOUTUBE_VIDEOS", "GENERATE_IMAGE_ENHANCEMENT", "GENERATE_IMAGE_EXTRACTION",
		"GENERATE_LANDING_PAGE_PREVIEW", "GENERATE_LANDING_PAGE_TEXT", "GENERATE_SHORTER_YOUTUBE_VIDEOS",
		"GENERATE_VERTICAL_YOUTUBE_VIDEOS", "GENERATE_VIDEOS_FROM_OTHER_ASSETS", "OVERLAY_TEXT_ON_VIDEOS",
		"TEXT_ASSET_AUTOMATION"}

	brandFonts = []string{"Open Sans", "Roboto", "Roboto Slab", "Montserrat", "Poppins", "Lato", "Oswald", "Playfair Display"}
)

func addressNode(name, desc string, check func(string, string) error) node {
	n := textNode(name, desc, maxURLArgument)
	n.check, n.clears = check, true
	return n
}

func dateTimeNode(name, desc string, clears bool) node {
	n := textNode(name, desc, 19)
	n.pattern, n.clears = timePattern.String(), clears
	return n
}

func patternNode(name, desc, pattern string, maxLen int) node {
	n := textNode(name, desc, maxLen)
	n.pattern = pattern
	return n
}

func ceilingNode() node {
	return microsNode("cpc_bid_ceiling_micros", "Highest CPC bid in micros of the account currency; Google takes it from portfolio strategies and, for the strategies where it is documented, from the campaign")
}

func floorNode() node {
	return microsNode("cpc_bid_floor_micros", "Lowest CPC bid in micros of the account currency; for portfolio strategies only")
}

func requireHTTPS(argument, value string) error {
	if value == "" {
		return badRequest(argument + " must be an https address")
	}
	return checkAddress(argument, value)
}

// biddingNodes are the members of the one-of group that sets the bidding of a campaign.
func biddingNodes() []node {
	tolerance := wholeNode("target_roas_tolerance_percent_millis", "Tolerance in milli-percent, 10000 is 10 percent", 0, 1_000_000_000)
	targetRoas := node{name: "target_roas", kind: kindNumber, bounded: true, low: 0.01, high: 1000,
		desc: "Target return on ad spend from 0.01 to 1000"}
	return []node{
		resourceNode("bidding_strategy", "Portfolio bidding strategy of this account, customers/ID/biddingStrategies/N", "biddingStrategies"),
		objectNode("commission", "Commission: the advertiser pays a share of the conversion value",
			wholeNode("commission_rate_micros", "Rate in millionths, 106000 is 10.6 percent", 0, 1_000_000_000)),
		objectNode("manual_cpa", "Manual CPA bidding; Local Services campaigns only"),
		objectNode("manual_cpc", "Manual cost per click bidding", flagNode("enhanced_cpc_enabled", "true to adjust bids on the chance of a conversion")),
		objectNode("manual_cpm", "Manual cost per thousand impressions bidding"),
		objectNode("manual_cpv", "Manual cost per view bidding"),
		objectNode("maximize_conversions", "Maximize conversions within the budget", ceilingNode(), floorNode(),
			microsNode("target_cpa_micros", "Target cost per action in micros; left out aims for the lowest possible")),
		objectNode("maximize_conversion_value", "Maximize the conversion value within the budget",
			node{name: "target_roas", kind: kindNumber, desc: "Target return on ad spend; left out aims for the highest possible"},
			ceilingNode(), floorNode(), tolerance),
		objectNode("percent_cpc", "Bids as a fraction of the advertised price", ceilingNode(),
			flagNode("enhanced_cpc_enabled", "true to adjust bids on the chance of a conversion")),
		objectNode("target_cpa", "Target cost per action", microsNode("target_cpa_micros", "Target cost per action in micros"),
			ceilingNode(), floorNode()),
		objectNode("target_cpc", "Target cost per click", microsNode("target_cpc_micros", "Target cost per click in micros")),
		objectNode("target_cpm", "Target cost per thousand impressions", objectNode("target_frequency_goal",
			"Frequency goal", wholeNode("target_count", "Times to reach one user", 0, math64),
			enumNode("time_unit", "Window of target_count", enumNamed("TargetFrequencyTimeUnitEnum.TargetFrequencyTimeUnit")))),
		objectNode("target_cpv", "Target cost per view"),
		objectNode("target_impression_share", "Target share of impressions",
			enumNode("location", "Where on the results page", enumOf("campaign.target_impression_share.location")),
			wholeNode("location_fraction_micros", "Share in millionths, 10000 is 1 percent", 0, 1_000_000), ceilingNode()),
		objectNode("target_roas", "Target return on ad spend", targetRoas, ceilingNode(), floorNode(), tolerance),
		objectNode("target_spend", "Get as many clicks as possible within the budget", ceilingNode()),
	}
}

const math64 = 1<<63 - 1

// oneOf marks members of an object of which at most one may be given.
func oneOf(n node, names ...string) node { n.exclusive = names; return n }

func boolMembers(prefix string, names ...string) []node {
	out := make([]node, 0, len(names))
	for _, name := range names {
		out = append(out, flagNode(name, prefix+" "+strings.ReplaceAll(strings.TrimPrefix(name, "target_"), "_", " ")))
	}
	return out
}

// campaignTree holds the members of a campaign that the tools offer. channelTypes and the other enums come
// from the field catalog of the version.
var campaignTree = oneOf(objectNode("campaign", "", campaignMembers()...), biddingNames()...)

func campaignMembers() []node {
	bidding := biddingNodes()
	members := []node{
		textNode("name", "Name of the campaign, 1 to 1024 bytes; unique in the account", maxNameArgument),
		enumNode("status", "ENABLED serves and spends within the budget; PAUSED does not. A campaign is removed with campaigns.remove",
			[]string{"ENABLED", "PAUSED"}),
		createOnly(enumNode("advertising_channel_type", "Channel of the campaign; fixed after creation",
			enumOf("campaign.advertising_channel_type"))),
		createOnly(enumNode("advertising_channel_sub_type", "Sub type of the channel; fixed after creation",
			enumOf("campaign.advertising_channel_sub_type"))),
		resourceNode("campaign_budget", "Budget of this account, customers/ID/campaignBudgets/N", "campaignBudgets"),
		resourceNode("campaign_group", "Group of this account, customers/ID/campaignGroups/N", "campaignGroups"),
		objectNode("network_settings", "Networks the campaign serves on", boolMembers("serve on",
			"target_google_search", "target_search_network", "target_content_network", "target_partner_search_network",
			"target_youtube", "target_google_tv_network")...),
		objectNode("geo_target_type_setting", "How locations are matched",
			enumNode("positive_geo_target_type", "Matching of the locations to target", enumOf("campaign.geo_target_type_setting.positive_geo_target_type")),
			enumNode("negative_geo_target_type", "Matching of the locations to exclude", enumOf("campaign.geo_target_type_setting.negative_geo_target_type"))),
		dateTimeNode("start_date_time", "Start in the time zone of the account, yyyy-MM-dd HH:mm:ss", false),
		dateTimeNode("end_date_time", "End in the time zone of the account, yyyy-MM-dd HH:mm:ss; empty on an update makes the campaign run without end", true),
		addressNode("tracking_url_template", "Tracking template: an https address without user info, up to 2048 bytes, ValueTrack parameters such as {lpurl} after the host; empty on an update clears it", checkAddress),
		addressNode("final_url_suffix", "Parameters appended to final URLs, such as utm_source=x, no address, up to 2048 bytes; empty on an update clears it", checkSuffix),
		listOf(objectNode("url_custom_parameters", "Custom parameters for tracking templates and final URLs; the list replaces the current one",
			patternNode("key", "Name of the parameter tag", keyPattern, 64), textNode("value", "Value substituted for the tag", maxNameArgument)), maxCampaignParams),
		listOf(objectNode("frequency_caps", "Limits on how often a user sees the ads; the list replaces the current one",
			objectNode("key", "What the cap counts",
				enumNode("level", "Level the cap applies to", frequencyLevels),
				enumNode("event_type", "Event counted", frequencyEvents),
				enumNode("time_unit", "Unit of the time range", frequencyUnits),
				wholeNode("time_length", "Number of time units", 1, 1<<31-1)),
			wholeNode("cap", "Most events in the range", 0, 1<<31-1)), maxCampaignList),
		enumNode("ad_serving_optimization_status", "Ad rotation", enumOf("campaign.ad_serving_optimization_status")),
		enumNode("contains_eu_political_advertising", "Declaration whether the campaign has political advertising of the EU",
			enumOf("campaign.contains_eu_political_advertising")),
		enumNode("keyword_match_type", "Keyword match type of the campaign", enumOf("campaign.keyword_match_type")),
		enumNode("payment_mode", "What is paid for", enumOf("campaign.payment_mode")),
		createOnly(enumNode("listing_type", "Listing type; fixed after creation", enumOf("campaign.listing_type"))),
		createOnly(resourceNode("hotel_property_asset_set", "Hotel property asset set of this account, customers/ID/assetSets/N; fixed after creation", "assetSets")),
		createOnly(objectNode("audience_setting", "Audience handling; fixed after creation",
			flagNode("use_audience_grouped", "true to use the Audience resource"))),
		createOnly(flagNode("brand_guidelines_enabled", "true to enable brand guidelines of a Performance Max campaign; settable at creation only")),
		objectNode("brand_guidelines", "Brand guidelines of a Performance Max campaign",
			patternNode("main_color", "Hex color such as #00ff00; needs accent_color", colorPattern, 7),
			patternNode("accent_color", "Hex color such as #00ff00; needs main_color", colorPattern, 7),
			enumNode("predefined_font_family", "Font family, case-sensitive", brandFonts)),
		objectNode("text_guidelines", "Guidelines for generated text of Performance Max and Search campaigns",
			listOf(textNode("term_exclusions", "Words and phrases to leave out, up to 30 characters each", 120), 25),
			listOf(objectNode("messaging_restrictions", "Instructions for generated text",
				textNode("restriction_text", "Instruction, up to 300 characters", 1200),
				enumNode("restriction_type", "Type of the restriction", messagingRestrictionTypes)), 40)),
		objectNode("ai_max_setting", "AI Max of Search and Shopping campaigns", flagNode("enable_ai_max", "false serves no AI Max feature")),
		listOf(objectNode("asset_automation_settings", "Opt in or out of automated assets; the list replaces the current one",
			enumNode("asset_automation_type", "Automation", automationTypes),
			enumNode("asset_automation_status", "OPTED_IN or OPTED_OUT", automationStatuses),
			objectNode("automated_video_crawl_setting", "Sources Google crawls for videos", listOf(objectNode("automated_video_crawl_infos",
				"Sources", func() node {
					n := textNode("url", "An https address without user info", maxURLArgument)
					n.check = requireHTTPS
					return n
				}(),
				enumNode("source_platform", "Kind of source", crawlPlatforms),
				flagNode("enabled", "true when the source is opted in")), maxCampaignList))), maxCampaignList),
		listOf(enumNode("excluded_parent_asset_field_types", "Asset field types of the account that the campaign does not inherit",
			enumNamed("AssetFieldTypeEnum.AssetFieldType")), maxCampaignList),
		listOf(enumNode("excluded_parent_asset_set_types", "Asset set types of the account that the campaign does not inherit",
			enumNamed("AssetSetTypeEnum.AssetSetType")), maxCampaignList),
		objectNode("optimization_goal_setting", "Optimization goals", listOf(enumNode("optimization_goal_types", "Goal types",
			enumNamed("OptimizationGoalTypeEnum.OptimizationGoalType")), maxCampaignList)),
		objectNode("selective_optimization", "Conversion actions of an app campaign", listOf(resourceNode("conversion_actions",
			"Conversion actions of this account, customers/ID/conversionActions/N", "conversionActions"), maxCampaignList)),
		objectNode("dynamic_search_ads_setting", "Dynamic search ads",
			patternNode("domain_name", "Domain, such as www.example.com, without scheme and path", hostPattern, 253),
			patternNode("language_code", "Language of the domain, such as en", languagePattern, 20),
			flagNode("use_supplied_urls_only", "true to use only the URLs the advertiser supplied")),
		objectNode("shopping_setting", "Shopping campaigns",
			wholeNode("merchant_id", "Merchant Center account ID; required on creation of a Shopping campaign", 1, math64),
			patternNode("feed_label", "Feed label of up to 20 letters, digits, hyphens and underscores", feedLabelPattern, 20),
			wholeNode("campaign_priority", "Priority from 0 to 2; higher takes precedence", 0, 2),
			flagNode("enable_local", "true to include local products"),
			flagNode("disable_product_feed", "true to disable the product feed (Demand Gen only)"),
			flagNode("ignore_brand_exclusion_in_shopping_ads", "true to ignore brand exclusions in Shopping ads")),
		objectNode("local_campaign_setting", "Local campaigns",
			enumNode("location_source_type", "Source of the locations", enumOf("campaign.local_campaign_setting.location_source_type"))),
		objectNode("app_campaign_setting", "App campaigns",
			enumNode("bidding_strategy_goal_type", "Goal of the bidding", enumOf("campaign.app_campaign_setting.bidding_strategy_goal_type")),
			createOnly(textNode("app_id", "ID of the app; fixed after creation", 255)),
			createOnly(enumNode("app_store", "Store of the app; fixed after creation", enumOf("campaign.app_campaign_setting.app_store")))),
		objectNode("hotel_setting", "Hotel campaigns", wholeNode("hotel_center_id", "Hotel Center account ID", 1, math64),
			flagNode("disable_hotel_setting", "true to disable the optional hotel setting (Demand Gen only)")),
		createOnly(objectNode("travel_campaign_settings", "Travel campaigns; fixed after creation",
			createOnly(wholeNode("travel_account_id", "Travel account ID", 1, math64)))),
		createOnly(objectNode("demand_gen_campaign_settings", "Demand Gen campaigns; fixed after creation",
			createOnly(flagNode("upgraded_targeting", "true for targeting at ad group level")))),
		objectNode("pmax_campaign_settings", "Performance Max campaigns",
			objectNode("brand_targeting_overrides", "Brand targeting overrides",
				flagNode("ignore_exclusions_for_shopping_ads", "true to ignore brand exclusions for Shopping ads")),
			createOnly(flagNode("local_services_enabled", "true for a Local Services campaign; fixed after creation")),
			objectNode("local_services_pmax_campaign_settings", "Local Services Performance Max",
				flagNode("navigational_query_leads_enabled", "true to use the navigational query filter"),
				wholeNode("founding_year", "Year the business was founded", 1, 9999),
				createOnly(patternNode("country_code", "Upper-case ISO-3166 two-letter country; required and fixed at creation", `^[A-Z]{2}$`, 2)),
				createOnly(wholeNode("listing_id", "Business Profile listing ID; required and fixed at creation", 1, math64)))),
		objectNode("local_services_campaign_settings", "Local Services campaigns", listOf(objectNode("category_bids", "Bids per category",
			textNode("category_id", "Category, such as xcat:service_area_business_plumber", maxNameArgument),
			microsNode("manual_cpa_bid_micros", "Manual CPA bid in micros; above the reserve price of the category"),
			microsNode("target_cpa_bid_micros", "Target CPA bid in micros")), maxCampaignList)),
		objectNode("vanity_pharma", "Display of pharma URLs",
			enumNode("vanity_pharma_display_url_mode", "Display mode", enumOf("campaign.vanity_pharma.vanity_pharma_display_url_mode")),
			enumNode("vanity_pharma_text", "Text when the mode is WEBSITE_DESCRIPTION", enumOf("campaign.vanity_pharma.vanity_pharma_text"))),
		oneOf(objectNode("video_campaign_settings", "Video campaigns",
			objectNode("video_ad_sequence", "Sequence of video ads",
				listOf(objectNode("steps", "Steps",
					wholeNode("video_ad_sequence_step_id", "ID of the step", 1, math64),
					wholeNode("asset_id", "ID of the YouTube video asset", 1, math64),
					enumNode("ad_group_type", "Video format of the step", enumNamed("AdGroupTypeEnum.AdGroupType")),
					wholeNode("previous_step_id", "Earlier step; required except for the first", 1, math64),
					enumNode("previous_step_interaction_type", "Interaction on the earlier step; required except for the first",
						sequenceInteractions)), maxCampaignList),
				enumNode("minimum_duration", "Period before a user may see the sequence again", enumOf("campaign.video_campaign_settings.video_ad_sequence.minimum_duration"))),
			objectNode("video_ad_inventory_control", "Inventory of reach campaigns; not together with video_ad_format_control",
				flagNode("allow_in_stream", "Allow in-stream"), flagNode("allow_in_feed", "Allow in-feed"),
				flagNode("allow_shorts", "Allow Shorts"), flagNode("allow_non_skippable_in_stream", "Allow non-skippable in-stream")),
			objectNode("video_ad_format_control", "Format restriction; not together with video_ad_inventory_control",
				enumNode("format_restriction", "Format all ads must respect", enumOf("campaign.video_campaign_settings.video_ad_format_control.format_restriction")),
				objectNode("non_skippable_in_stream_restrictions", "Limits of non-skippable in-stream ads",
					enumNode("min_duration", "Shortest duration", enumOf("campaign.video_campaign_settings.video_ad_format_control.non_skippable_in_stream_restrictions.min_duration")),
					enumNode("max_duration", "Longest duration", enumOf("campaign.video_campaign_settings.video_ad_format_control.non_skippable_in_stream_restrictions.max_duration"))))),
			"video_ad_inventory_control", "video_ad_format_control"),
		flagNode("view_through_conversion_optimization_enabled", "true to optimize for view-through conversions"),
		objectNode("real_time_bidding_setting", "Real-time bidding of Ad Manager campaigns", flagNode("opt_in", "true to opt in")),
		objectNode("targeting_setting", "Restriction of the reach by targeting dimension; the list replaces the current one",
			listOf(objectNode("target_restrictions", "Restrictions",
				enumNode("targeting_dimension", "Dimension", enumNamed("TargetingDimensionEnum.TargetingDimension")),
				flagNode("bid_only", "true: the criteria only adjust bids; false: they restrict targeting too")), maxCampaignList)),
	}
	members = append(members, bidding...)
	return members
}

// biddingNames are the members of which a campaign holds at most one.
func biddingNames() []string {
	var names []string
	for _, n := range biddingNodes() {
		names = append(names, n.name)
	}
	return names
}
