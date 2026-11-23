package googleads

import (
	"slices"
	"unicode/utf8"
)

// The members of the AdGroup resource of v26 that ad-groups.create and ad-groups.update offer. Not offered:
//   - the members Google fills itself (output only): id, base_ad_group, labels, the effective bids and their
//     sources, primary_status and its reasons; they are read with reports.search. The resource has no
//     deprecated member
//   - fixed_cpm_micros, the price of a reservation campaign that is booked, and booking is a billing action
//     that is read-only; an answer still shows it
//   - SHOPPING_SMART_ADS as the type: Google documents that new ones cannot be created

const (
	maxAdGroups      = 10
	maxAdGroupList   = 50
	maxAdGroupParams = 50
	// maxAdGroupWidth is the longest name: Google allows fewer than 255 full-width characters, in which a
	// full-width character counts twice.
	maxAdGroupWidth = 254
)

// checkAdGroupName refuses a name Google would: empty, longer than its width allows, or holding a null, a line
// feed, a carriage return or any other control character.
func checkAdGroupName(argument, value string) error {
	width := 0
	for _, r := range value {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) || r == utf8.RuneError {
			return badRequest(argument + " must not hold control characters")
		}
		width++
		if r >= 0x1100 {
			width++
		}
	}
	if value == "" || width > maxAdGroupWidth {
		return badRequest(argument + " must be a name of 1 to 254 characters, counting a full-width character twice")
	}
	return nil
}

func adGroupMembers() []node {
	name := textNode("name", "Name of the ad group, fewer than 255 characters (a full-width character counts twice) without line breaks; unique among the ad groups of the campaign", 254)
	name.check = checkAdGroupName
	types := slices.DeleteFunc(enumOf("ad_group.type"), func(v string) bool { return v == "SHOPPING_SMART_ADS" })
	return []node{
		name,
		enumNode("status", "ENABLED serves and bids within the budget of the campaign; PAUSED does not. An ad group is removed with ad-groups.remove",
			[]string{"ENABLED", "PAUSED"}),
		createOnly(resourceNode("campaign", "Campaign of this account the ad group belongs to, customers/ID/campaigns/N; fixed after creation", "campaigns")),
		createOnly(enumNode("type", "Type of the ad group; fixed after creation. SHOPPING_SMART_ADS is not offered: Google creates no new ones", types)),
		enumNode("ad_rotation_mode", "Ad rotation of the ad group", enumOf("ad_group.ad_rotation_mode")),
		microsNode("cpc_bid_micros", "Maximum CPC bid in micros of the account currency; Google ignores it when the campaign uses a portfolio bidding strategy"),
		microsNode("cpm_bid_micros", "Maximum CPM bid in micros of the account currency"),
		microsNode("cpv_bid_micros", "Maximum CPV bid in micros of the account currency"),
		wholeNode("percent_cpc_bid_micros", "Percent CPC bid as a fraction from 0 up to, not including, 1, in millionths: 500000 is 50 percent", 0, 999_999),
		microsNode("target_cpa_micros", "Target CPA in micros; overrides the campaign only for target CPA and for maximize conversions with a target CPA"),
		microsNode("target_cpc_micros", "Target CPC in micros; overrides the target CPC of the campaign"),
		microsNode("target_cpm_micros", "Target CPM in micros"),
		microsNode("target_cpv_micros", "Target CPV in micros"),
		node{name: "target_roas", kind: kindNumber, desc: "Target return on ad spend; Google refuses it for a portfolio bidding strategy and ignores it for the others"},
		enumNode("display_custom_bid_dimension", "Dimension of the bid of Display-only campaigns", enumOf("ad_group.display_custom_bid_dimension")),
		flagNode("optimized_targeting_enabled", "true to let Google expand the audience; replaces audience expansion"),
		flagNode("exclude_demographic_expansion", "true to leave demographics out of the expansion; Google ignores it unless optimized_targeting_enabled is true"),
		listOf(enumNode("excluded_parent_asset_field_types", "Asset field types of the campaign and the account that the ad group does not inherit",
			enumNamed("AssetFieldTypeEnum.AssetFieldType")), maxAdGroupList),
		listOf(enumNode("excluded_parent_asset_set_types", "Asset set types that the ad group does not inherit; Google supports only LOCATION_SYNC",
			[]string{"LOCATION_SYNC"}), maxAdGroupList),
		addressNode("tracking_url_template", "Tracking template: an https address without user info, up to 2048 bytes, ValueTrack parameters such as {lpurl} after the host; empty on an update clears it", checkAddress),
		addressNode("final_url_suffix", "Parameters appended to final URLs, such as utm_source=x, no address, up to 2048 bytes; empty on an update clears it", checkSuffix),
		listOf(objectNode("url_custom_parameters", "Custom parameters for tracking templates and final URLs; the list replaces the current one",
			patternNode("key", "Name of the parameter tag", keyPattern, 64), textNode("value", "Value substituted for the tag", maxNameArgument)), maxAdGroupParams),
		objectNode("targeting_setting", "Restriction of the reach by targeting dimension; the list replaces the current one",
			listOf(objectNode("target_restrictions", "Restrictions",
				enumNode("targeting_dimension", "Dimension", enumNamed("TargetingDimensionEnum.TargetingDimension")),
				flagNode("bid_only", "true: the criteria only adjust bids; false: they restrict targeting too")), maxAdGroupList)),
		createOnly(objectNode("audience_setting", "Audience handling; fixed after creation",
			flagNode("use_audience_grouped", "true to use the Audience resource"))),
		objectNode("ai_max_ad_group_setting", "AI Max of a standard Search ad group",
			flagNode("disable_search_term_matching", "true to turn off search term matching while AI Max is enabled")),
		objectNode("vertical_ads_format_setting", "Format controls of vertical ads in Search campaigns; Google needs AI Max and a travel feed on the campaign",
			flagNode("disable_text_ads", "true to disable text ads"),
			flagNode("enable_booking_links", "true to enable booking links"),
			flagNode("enable_vertical_promotion_ads", "true to enable vertical promotion ads")),
		objectNode("demand_gen_ad_group_settings", "Settings of Demand Gen ad groups",
			objectNode("channel_controls", "Channel controls",
				enumNode("channel_config", "Whether channel_strategy or selected_channels decides",
					enumOf("ad_group.demand_gen_ad_group_settings.channel_controls.channel_config")),
				enumNode("channel_strategy", "Strategy over the channels",
					enumOf("ad_group.demand_gen_ad_group_settings.channel_controls.channel_strategy")),
				objectNode("selected_channels", "Channels to serve on",
					flagNode("discover", "Discover"), flagNode("display", "Display"), flagNode("gmail", "Gmail"),
					flagNode("maps", "Maps"), flagNode("youtube_in_feed", "YouTube in-feed"),
					flagNode("youtube_in_stream", "YouTube in-stream"), flagNode("youtube_shorts", "YouTube Shorts")))),
		objectNode("video_ad_group_settings", "Settings of video ad groups",
			objectNode("video_ad_sequence", "Step of a video ad sequence",
				wholeNode("step_id", "ID of the step", 1, math64))),
	}
}

// adGroupTree holds the members of an ad group that the tools offer.
var adGroupTree = objectNode("ad_group", "", adGroupMembers()...)

// adGroupOutputTree is the ad group Google returns with MUTABLE_RESOURCE: the members the tools offer, the
// reservation price that is not offered, and the resource name.
var adGroupOutputTree = func() node {
	t := adGroupTree
	t.fields = append(slices.Clone(t.fields), microsNode("fixed_cpm_micros", ""), textNode("resource_name", "", maxNameArgument))
	return t
}()
