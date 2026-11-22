package googleads

// The members of the AdGroupAd and Ad resources of v26 that ad-group-ads.create and ad-group-ads.update offer,
// described with the asset spec mechanism. Not offered:
//   - the members Google fills itself (output only): the ad ID and type, added_by_google_ads, policy_summary,
//     ad_strength, action_items, labels, the primary status and its reasons, and the system managed source;
//     they are read with reports.search
//   - the ad types expanded_text_ad (the reference calls it deprecated: use responsive_search_ad),
//     legacy_responsive_display_ad and legacy_app_install_ad (legacy types, the second for a few select
//     customers only), and shopping_smart_ad (Google creates no new Smart Shopping ads). Call-only ads are no
//     member of Ad in v26 at all, and Google creates none since 2026-01-01
//   - the copy source of image_ad (ad_id_to_copy_image_from): an ad ID cannot be bound to the account
//   - the image members that Google fills (sizes, URLs, mime type)

const (
	maxAdGroupAds = 10
	// maxAdPolicyTopics and maxAdPolicyKeys are Qatlas's own bounds for the exemptions of one operation.
	maxAdPolicyTopics = 20
	maxAdPolicyKeys   = 20
)

// pinnable are the values of the served asset field type that Google lets an ad pin a text to; the positions
// of sitelinks and TEXT_DISCLAIMER are output only.
var pinnable = []string{"HEADLINE_1", "HEADLINE_2", "HEADLINE_3", "DESCRIPTION_1", "DESCRIPTION_2"}

var (
	adTextSpec = spec(text("text").required(), enum("pinned_field").among(pinnable...))

	adAssetSpec = spec(assetRef("asset").required())

	adVideoSpec = spec(assetRef("asset").required(), member("ad_video_asset_info", spec(
		member("ad_video_asset_inventory_preferences", spec(flag("in_feed_preference"), flag("in_stream_preference"),
			flag("shorts_preference"))),
		member("ad_video_asset_feature_control", spec(flag("allow_youtube_comments"))))))

	companionSpec = func(extra ...assetField) *assetSpec {
		return spec(append([]assetField{member("companion_banner", adAssetSpec), text("action_button_label"),
			text("action_headline")}, extra...)...)
	}
)

func textList(name string) assetField  { return members(name, adTextSpec) }
func assetList(name string) assetField { return members(name, adAssetSpec) }
func videoList(name string) assetField { return members(name, adVideoSpec) }

// adTypes are the members of the ad_data oneof that the tools offer, with the numbers of the reference of v26
// where it states them: a list limit is the largest number it names, atLeast the smallest.
func adTypes() []assetField {
	t := func(name string, s *assetSpec) assetField { return member(name, s).oneof("ad_data") }
	return []assetField{
		t("responsive_search_ad", spec(textList("headlines").required().atLeast(3).items(15),
			textList("descriptions").required().atLeast(2).items(4), text("path1"), text("path2"))),
		t("expanded_dynamic_search_ad", spec(text("description"), text("description2"))),
		t("text_ad", spec(text("headline"), text("description1"), text("description2"))),
		t("hotel_ad", spec()),
		t("travel_ad", spec()),
		t("shopping_product_ad", spec()),
		t("shopping_comparison_listing_ad", spec(text("headline").required())),
		t("image_ad", spec(member("image_asset", adAssetSpec), assetField{name: "data", kind: kBytes})),
		t("video_ad", &assetSpec{fields: []assetField{member("video", adVideoSpec),
			member("in_stream", companionSpec()).oneof("format"),
			member("bumper", companionSpec()).oneof("format"),
			member("out_stream", spec(text("headline"), text("description"))).oneof("format"),
			member("non_skippable", companionSpec()).oneof("format"),
			member("in_feed", spec(text("headline"), text("description1"), text("description2"),
				enum("thumbnail").among(enumNamed("VideoThumbnailEnum.VideoThumbnail")...))).oneof("format"),
			member("audio", spec()).oneof("format")}}),
		t("video_responsive_ad", spec(textList("headlines").items(1), textList("long_headlines").items(1),
			textList("descriptions").items(1), textList("call_to_actions").items(1),
			videoList("videos").required().items(1), member("business_name", adTextSpec).required(),
			assetList("logo_images").required(), assetList("companion_banners").items(1), text("breadcrumb1"),
			text("breadcrumb2"))),
		t("responsive_display_ad", spec(assetList("marketing_images").required().items(15),
			assetList("square_marketing_images").required().items(15), assetList("logo_images").items(5),
			assetList("square_logo_images").items(5), textList("headlines").required().items(5),
			member("long_headline", adTextSpec).required(), textList("descriptions").required().items(5),
			videoList("youtube_videos").items(5), text("business_name"),
			text("main_color").matching(colorPattern), text("accent_color").matching(colorPattern),
			flag("allow_flexible_color"), text("call_to_action_text"), text("price_prefix"), text("promo_text"),
			enum("format_setting").among(enumNamed("DisplayAdFormatSettingEnum.DisplayAdFormatSetting")...),
			member("control_spec", spec(flag("enable_asset_enhancements"), flag("enable_autogen_video"))))),
		t("app_ad", spec(member("mandatory_ad_text", adTextSpec), textList("headlines"), textList("descriptions"),
			assetList("images"), videoList("youtube_videos"), assetList("html5_media_bundles"),
			member("app_deep_link", adAssetSpec))),
		t("app_engagement_ad", spec(textList("headlines"), textList("descriptions"), assetList("images"),
			videoList("videos"))),
		t("app_pre_registration_ad", spec(textList("headlines"), textList("descriptions"), assetList("images"),
			videoList("youtube_videos"))),
		t("local_ad", spec(textList("headlines").required().items(5), textList("descriptions").required().items(5),
			textList("call_to_actions").required().items(5), assetList("marketing_images").required().items(20),
			assetList("logo_images").required().items(5), videoList("videos").required().items(20), text("path1"),
			text("path2"))),
		t("display_upload_ad", &assetSpec{fields: []assetField{
			enum("display_upload_product_type").among(enumNamed("DisplayUploadProductTypeEnum.DisplayUploadProductType")...),
			member("media_bundle", adAssetSpec).oneof("media_asset")}}),
		t("smart_campaign_ad", spec(textList("headlines").required().atLeast(3).items(15),
			textList("descriptions").required().atLeast(2).items(4))),
		t("demand_gen_multi_asset_ad", spec(assetList("marketing_images").items(20), assetList("square_marketing_images").items(20),
			assetList("portrait_marketing_images").items(20), assetList("tall_portrait_marketing_images").items(20),
			assetList("logo_images").atLeast(1).items(5), assetList("classic_display_images").items(20),
			textList("headlines").items(5), textList("descriptions").items(5), text("business_name").required(),
			text("call_to_action_text"))),
		t("demand_gen_carousel_ad", spec(text("business_name").required(), member("logo_image", adAssetSpec).required(),
			member("headline", adTextSpec).required(), member("description", adTextSpec).required(),
			text("call_to_action_text"), members("carousel_cards", adAssetSpec).required().atLeast(2).items(10))),
		t("demand_gen_video_responsive_ad", spec(textList("headlines"), textList("long_headlines"), textList("descriptions"),
			videoList("videos").required(), assetList("logo_images").required(), assetList("companion_banners").items(1),
			text("breadcrumb1"), text("breadcrumb2"), member("business_name", adTextSpec).required(),
			assetList("call_to_actions"))),
		t("demand_gen_product_ad", spec(member("headline", adTextSpec).required(), member("description", adTextSpec).required(),
			member("logo_image", adAssetSpec).required(), text("breadcrumb1"), text("breadcrumb2"),
			member("business_name", adTextSpec).required(), member("call_to_action", adAssetSpec))),
	}
}

// adSpec is the ad of an operation: the common members and one member per offered ad type. Google requires
// exactly one type.
func adSpec() *assetSpec {
	fields := []assetField{
		assetField{name: "final_urls", kind: kAddresses, limit: maxAssetURLs},
		assetField{name: "final_mobile_urls", kind: kAddresses, limit: maxAssetURLs},
		link("tracking_url_template"),
		assetField{name: "final_url_suffix", kind: kSuffix},
		members("url_custom_parameters", spec(text("key").required().matching(keyPattern),
			text("value").required())).items(maxAssetURLs),
		enum("device_preference").among("MOBILE"),
		text("name"),
		text("display_url"),
		members("url_collections", spec(text("url_collection_id"),
			assetField{name: "final_urls", kind: kAddresses, limit: maxAssetURLs},
			assetField{name: "final_mobile_urls", kind: kAddresses, limit: maxAssetURLs},
			link("tracking_url_template"))).items(maxAssetURLs),
		members("final_app_urls", spec(enum("os_type").required().among("IOS", "ANDROID"),
			text("url").required())).items(maxAssetURLs),
		member("synthetic_content_info", spec(member("advertiser_attestation", attestationSpec))),
		ref("resource_name", "ads").output(),
	}
	return &assetSpec{fields: append(fields, adTypes()...), need: []string{"ad_data"}}
}

// adGroupAdSpec is one ad group ad: what a create takes, and what Google answers with.
func adGroupAdSpec() *assetSpec {
	return &assetSpec{fields: []assetField{
		enum("status").required().among("ENABLED", "PAUSED"),
		ref("ad_group", "adGroups").required().created(),
		member("ad", adSpec()).required().created(),
		text("start_date_time").matching(timePattern.String()),
		text("end_date_time").matching(timePattern.String()),
		members("ad_group_ad_asset_automation_settings", spec(
			enum("asset_automation_type").among(automationTypes...),
			enum("asset_automation_status").among(automationStatuses...))).items(len(automationTypes)),
	}}
}

// adGroupAdUpdateSpec is what an update may name: the members Google does not mark immutable.
func adGroupAdUpdateSpec() *assetSpec { return updatable(adGroupAdSpec()) }
