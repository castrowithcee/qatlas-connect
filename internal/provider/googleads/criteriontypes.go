package googleads

import (
	"math"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The criterion types of the Mutate services of criteria. A type is one member of the oneof of the criterion
// resource; it carries the members Google documents for it and the rules Google states for its creation, so a
// service that holds the same type reuses it. Not offered here:
//   - webpage_list, which Google marks as not publicly available
//   - the feed_item_sets of location_group, which belong to the feeds Google has retired
//   - the deprecated product dimensions of listing_scope (product_grouping, product_labels,
//     product_legacy_condition, product_type_full), which Google supports in Display campaigns only
// Whether a type applies to the channel of a campaign is Google's to decide: the request is sent once and a
// refusal is shown as one.

// criterionType is one type of criterion: its member of the criterion and the rules for its creation.
type criterionType struct {
	node node
	// required are the members Google requires when the criterion is created.
	required []string
	// rules checks what the members that were given must satisfy together.
	rules func(in map[string]any) error
}

// constantNode is the reference to a constant resource of Google, such as geoTargetConstants/2276. A constant
// belongs to no account, so it is accepted and returned only in this fixed form; the collection marks the node
// as a constant for constantsBound.
func constantNode(name, desc, collection string) node {
	return constantPatternNode(name, desc, collection, `^`+collection+`/[1-9][0-9]{0,18}$`)
}

func constantPatternNode(name, desc, collection, pattern string) node {
	n := patternNode(name, desc, pattern, 64)
	n.collection = collection
	return n
}

// constantsBound reports whether every constant in the members of an answer has the fixed form.
func constantsBound(n node, members map[string]any) bool {
	matches := func(f node, value any) bool {
		text, ok := value.(string)
		return ok && regexp.MustCompile(f.pattern).MatchString(text)
	}
	for _, f := range n.fields {
		value, present := members[f.name]
		if !present {
			continue
		}
		switch {
		case f.kind == kindText && f.collection != "":
			if list, ok := value.([]any); ok {
				for _, item := range list {
					if !matches(f, item) {
						return false
					}
				}
			} else if !matches(f, value) {
				return false
			}
		case f.kind == kindObject:
			items := []any{value}
			if list, ok := value.([]any); ok {
				items = list
			}
			for _, item := range items {
				if object, ok := item.(map[string]any); ok && !constantsBound(f, object) {
					return false
				}
			}
		}
	}
	return true
}

// typeNames are the names of the types of a list.
func typeNames(types []criterionType) []string {
	names := make([]string, 0, len(types))
	for _, t := range types {
		names = append(names, t.node.name)
	}
	return names
}

func typeNamed(types []criterionType, name string) (criterionType, bool) {
	for _, t := range types {
		if t.node.name == name {
			return t, true
		}
	}
	return criterionType{}, false
}

// checkKeywordText holds a keyword to what Google states: up to 80 characters and 10 words.
func checkKeywordText(argument, value string) error {
	if value == "" || utf8.RuneCountInString(value) > 80 || len(strings.Fields(value)) > 10 || hasControl(value) {
		return badRequest(argument + " must be a keyword of 1 to 80 characters and at most 10 words without control characters")
	}
	return nil
}

// checkIPBlock accepts one IPv4 or IPv6 address or one CIDR block.
func checkIPBlock(argument, value string) error {
	if len(value) <= 50 && !hasControl(value) {
		if _, err := netip.ParsePrefix(value); err == nil {
			return nil
		}
		if address, err := netip.ParseAddr(value); err == nil && address.Zone() == "" {
			return nil
		}
	}
	return badRequest(argument + " must be one IP address or one CIDR block, IPv4 or IPv6")
}

func typeNode(name, desc string, fields ...node) node {
	return createOnly(objectNode(name, desc, fields...))
}

var locationGroupUnits = []string{"METERS", "MILES", "MILLI_MILES"}

// requireMembers refuses an object that lacks a member.
func requireMembers(in map[string]any, at string, names ...string) error {
	for _, name := range names {
		if _, ok := in[name]; !ok {
			return badRequest(at + "." + name + " is required")
		}
	}
	return nil
}

// baseCriterionTypes are the types of the criteria of a campaign. field is the prefix of the catalog field of
// the resource whose enums the type uses.
func baseCriterionTypes(field string) []criterionType {
	enum := func(name string) []string { return enumOf(field + "." + name) }
	return []criterionType{
		{node: typeNode("keyword", "Keyword",
			node{name: "text", desc: "Keyword of up to 80 characters and 10 words", kind: kindText, maxLen: 320, check: checkKeywordText},
			enumNode("match_type", "Match type", enum("keyword.match_type"))), required: []string{"text", "match_type"}},
		{node: typeNode("mobile_app_category", "Category of mobile apps",
			constantNode("mobile_app_category_constant", "Constant of Google, mobileAppCategoryConstants/N", "mobileAppCategoryConstants")),
			required: []string{"mobile_app_category_constant"}},
		{node: typeNode("mobile_application", "Mobile app",
			patternNode("app_id", "ID of the app as platform-id, where platform is 1 for iOS and 2 for Android, such as 2-com.example.app",
				`^[12]-[A-Za-z0-9._]{1,200}$`, 202),
			textNode("name", "Name of the app", 255)), required: []string{"app_id"}},
		{node: typeNode("location", "Location",
			constantNode("geo_target_constant", "Constant of Google, geoTargetConstants/N", "geoTargetConstants")),
			required: []string{"geo_target_constant"}},
		{node: typeNode("device", "Device", enumNode("type", "Device", enum("device.type"))), required: []string{"type"}},
		{node: typeNode("proximity", "Radius around a point or an address",
			objectNode("geo_point", "Point in millionths of a degree; give both members",
				wholeNode("longitude_in_micro_degrees", "Longitude from -180000000 to 180000000", -180_000_000, 180_000_000),
				wholeNode("latitude_in_micro_degrees", "Latitude from -90000000 to 90000000", -90_000_000, 90_000_000)),
			node{name: "radius", desc: "Radius, more than 0, in radius_units", kind: kindNumber, bounded: true, low: 0, high: 1_000_000},
			enumNode("radius_units", "Unit of the radius; kilometers when left out", enum("proximity.radius_units")),
			objectNode("address", "Address that gives the point, or a label for geo_point",
				textNode("street_address", "Street and number", 250), textNode("street_address2", "Second line, used only to find the point", 250),
				textNode("city_name", "City", 250), textNode("province_name", "Province or state", 250),
				textNode("province_code", "Code of the province or state", 250), textNode("postal_code", "Postal code", 250),
				textNode("country_code", "Country code such as DE", 250))),
			required: []string{"radius"}, rules: func(in map[string]any) error {
				_, point := in["geo_point"]
				_, address := in["address"]
				if !point && !address {
					return badRequest("proximity needs geo_point or address")
				}
				if object, ok := in["geo_point"].(map[string]any); ok {
					return requireMembers(object, "proximity.geo_point", "longitude_in_micro_degrees", "latitude_in_micro_degrees")
				}
				return nil
			}},
		{node: typeNode("language", "Language",
			constantNode("language_constant", "Constant of Google, languageConstants/N", "languageConstants")),
			required: []string{"language_constant"}},
		{node: typeNode("ip_block", "Address or block of IP addresses to exclude; up to 500 per campaign",
			node{name: "ip_address", desc: "One IPv4 or IPv6 address or CIDR block", kind: kindText, maxLen: 50, check: checkIPBlock}),
			required: []string{"ip_address"}},
		{node: typeNode("content_label", "Content label", enumNode("type", "Label", enum("content_label.type"))), required: []string{"type"}},
		{node: typeNode("carrier", "Carrier",
			constantNode("carrier_constant", "Constant of Google, carrierConstants/N", "carrierConstants")), required: []string{"carrier_constant"}},
		{node: typeNode("user_interest", "Interest category",
			resourceNode("user_interest_category", "Interest category of this account, customers/ID/userInterests/N", "userInterests")),
			required: []string{"user_interest_category"}},
		{node: typeNode("operating_system_version", "Operating system version",
			constantNode("operating_system_version_constant", "Constant of Google, operatingSystemVersionConstants/N", "operatingSystemVersionConstants")),
			required: []string{"operating_system_version_constant"}},
		{node: typeNode("mobile_device", "Mobile device",
			constantNode("mobile_device_constant", "Constant of Google, mobileDeviceConstants/N", "mobileDeviceConstants")),
			required: []string{"mobile_device_constant"}},
		{node: typeNode("location_group", "Radius around the locations of an asset set or a location constant",
			listOf(constantNode("geo_target_constants", "Constant of Google, geoTargetConstants/N; only one is allowed", "geoTargetConstants"), 1),
			wholeNode("radius", "Radius in radius_units", 1, math.MaxInt64),
			enumNode("radius_units", "Unit of the radius: miles and meters for geo_target_constants, milli-miles and meters for asset sets", locationGroupUnits),
			flagNode("enable_customer_level_location_asset_set", "true to use the location asset set of the account; not with location_group_asset_sets"),
			listOf(resourceNode("location_group_asset_sets", "Location asset set of this account, customers/ID/assetSets/N", "assetSets"), maxCriteria)),
			required: []string{"radius", "radius_units"}, rules: func(in map[string]any) error {
				_, sets := in["location_group_asset_sets"]
				if _, geo := in["geo_target_constants"]; geo && sets {
					return badRequest("location_group_asset_sets cannot be given with geo_target_constants")
				}
				if _, enabled := in["enable_customer_level_location_asset_set"]; enabled && sets {
					return badRequest("location_group_asset_sets cannot be given with enable_customer_level_location_asset_set")
				}
				return nil
			}},
		{node: typeNode("custom_affinity", "Custom affinity",
			resourceNode("custom_affinity", "Custom interest of this account that can be targeted, customers/ID/customInterests/N", "customInterests")),
			required: []string{"custom_affinity"}},
		{node: typeNode("custom_audience", "Custom audience",
			resourceNode("custom_audience", "Custom audience of this account, customers/ID/customAudiences/N", "customAudiences")),
			required: []string{"custom_audience"}},
		{node: typeNode("combined_audience", "Combined audience",
			resourceNode("combined_audience", "Combined audience of this account, customers/ID/combinedAudiences/N", "combinedAudiences")),
			required: []string{"combined_audience"}},
		{node: typeNode("age_range", "Age range", enumNode("type", "Age range", enum("age_range.type"))), required: []string{"type"}},
		{node: typeNode("gender", "Gender", enumNode("type", "Gender", enum("gender.type"))), required: []string{"type"}},
		{node: typeNode("income_range", "Income range", enumNode("type", "Income range", enum("income_range.type"))), required: []string{"type"}},
		{node: typeNode("parental_status", "Parental status", enumNode("type", "Parental status", enum("parental_status.type"))),
			required: []string{"type"}},
		{node: typeNode("user_list", "User list",
			resourceNode("user_list", "User list of this account, customers/ID/userLists/N", "userLists")), required: []string{"user_list"}},
		{node: typeNode("youtube_video", "YouTube video",
			patternNode("video_id", "Video ID as the YouTube watch page shows it, 11 characters", `^[A-Za-z0-9_-]{11}$`, 11)),
			required: []string{"video_id"}},
		{node: typeNode("youtube_channel", "YouTube channel",
			patternNode("channel_id", "ID of the channel of the uploader", `^[A-Za-z0-9_-]{1,64}$`, 64)), required: []string{"channel_id"}},
		{node: typeNode("topic", "Topic",
			constantNode("topic_constant", "Constant of Google, topicConstants/N", "topicConstants"),
			listOf(textNode("path", "Category path from general to specific, such as Pets & Animals, Pets, Dogs", 255), 10)),
			rules: func(in map[string]any) error {
				_, constant := in["topic_constant"]
				_, path := in["path"]
				if !constant && !path {
					return badRequest("topic needs topic_constant or path")
				}
				return nil
			}},
		{node: typeNode("ad_schedule", "Time window of a day of the week; at most six per day",
			enumNode("day_of_week", "Day", enum("ad_schedule.day_of_week")),
			wholeNode("start_hour", "Start hour from 0 to 23", 0, 23), enumNode("start_minute", "Start minute", enum("ad_schedule.start_minute")),
			wholeNode("end_hour", "End hour from 0 to 24, 24 is the end of the day", 0, 24),
			enumNode("end_minute", "End minute, exclusive", enum("ad_schedule.end_minute"))),
			required: []string{"day_of_week", "start_hour", "start_minute", "end_hour", "end_minute"}},
		{node: typeNode("life_event", "Life event",
			wholeNode("life_event_id", "ID of the life event in the taxonomy of Google", 1, math.MaxInt64)), required: []string{"life_event_id"}},
		{node: typeNode("extended_demographic", "Extended demographic",
			wholeNode("extended_demographic_id", "ID in the taxonomy of Google", 1, math.MaxInt64)), required: []string{"extended_demographic_id"}},
		{node: typeNode("brand_list", "Brand list",
			resourceNode("shared_set", "Brand list of this account, a shared set, customers/ID/sharedSets/N", "sharedSets")),
			required: []string{"shared_set"}},
		{node: typeNode("placement", "Website, domain, or page to target or exclude",
			node{name: "url", desc: "Address of the placement, such as www.example.com or https://www.example.com/page; http, https, or no scheme, no user info",
				kind: kindText, maxLen: maxPlacementURL, check: checkPlacementURL}), required: []string{"url"}},
		{node: typeNode("webpage", "Webpage target of a campaign with dynamic search ads",
			textNode("criterion_name", "Name of the target", maxNameArgument),
			allowEmpty(listOf(objectNode("conditions", "Conditions that are all met; an empty list targets all pages",
				enumNode("operand", "What the condition looks at", webpageOperands),
				enumNode("operator", "How the argument is compared", webpageOperators),
				textNode("argument", "Value compared", maxURLArgument)), maxCriteria))),
			required: []string{"criterion_name", "conditions"}, rules: func(in map[string]any) error {
				conditions, _ := in["conditions"].([]any)
				for i, item := range conditions {
					if condition, ok := item.(map[string]any); ok {
						if err := requireMembers(condition, "webpage.conditions["+strconv.Itoa(i)+"]", "operand", "operator", "argument"); err != nil {
							return err
						}
					}
				}
				return nil
			}},
		{node: typeNode("keyword_theme", "Keyword theme of a Smart campaign",
			constantPatternNode("keyword_theme_constant", "Constant of Google, keywordThemeConstants/ID~SUB_ID", "keywordThemeConstants",
				`^keywordThemeConstants/[1-9][0-9]{0,18}~[1-9][0-9]{0,18}$`),
			textNode("free_form_keyword_theme", "Free text that Google matches to a theme", 255)),
			rules: func(in map[string]any) error {
				_, constant := in["keyword_theme_constant"]
				_, text := in["free_form_keyword_theme"]
				if constant == text {
					return badRequest("keyword_theme needs exactly one of keyword_theme_constant and free_form_keyword_theme")
				}
				return nil
			}},
		{node: typeNode("local_service_id", "Service type of a Local Services campaign",
			patternNode("service_id", "Service type such as install_faucet", `^[A-Za-z0-9_:.-]{1,200}$`, 200)),
			required: []string{"service_id"}},
		{node: typeNode("video_lineup", "Video lineup",
			wholeNode("video_lineup_id", "ID of the video lineup, which Google gives on request", 1, math.MaxInt64)),
			required: []string{"video_lineup_id"}},
		{node: typeNode("listing_scope", "Scope of listings: the dimensions of products, hotels, or activities",
			listOf(listingDimension(), maxCriteria)), required: []string{"dimensions"}, rules: listingScopeRules},
	}
}

const maxPlacementURL = 1024

var (
	webpageOperands  = []string{"URL", "CATEGORY", "PAGE_TITLE", "PAGE_CONTENT", "CUSTOM_LABEL"}
	webpageOperators = []string{"EQUALS", "CONTAINS"}
)

// checkPlacementURL accepts an address with the scheme http or https or without one, and no user info. The
// address is a target of the ads: Qatlas never calls it.
func checkPlacementURL(argument, value string) error {
	if value == "" || len(value) > maxPlacementURL {
		return badRequest(argument + " must be an address of 1 to " + strconv.Itoa(maxPlacementURL) + " bytes")
	}
	for _, r := range value {
		if unicode.IsControl(r) || unicode.IsSpace(r) || r == '\\' {
			return badRequest(argument + " must not hold spaces, backslashes or control characters")
		}
	}
	rest := value
	if scheme, after, found := strings.Cut(value, "://"); found {
		if lower := strings.ToLower(scheme); lower != "http" && lower != "https" {
			return badRequest(argument + " must have the scheme http or https, or none")
		}
		rest = after
	}
	authority := rest
	if i := strings.IndexAny(authority, "/?#"); i >= 0 {
		authority = authority[:i]
	}
	if authority == "" || strings.ContainsAny(authority, "@{}") {
		return badRequest(argument + " must name a host without user info")
	}
	return nil
}

// listingDimension is one dimension of a listing scope: exactly one of its members. The deprecated product
// dimensions are not offered; unknown_listing_dimension is the dimension of the listings no other covers.
func listingDimension() node {
	text := func(desc string) node { return textNode("value", desc, 255) }
	geo := func(name, desc string) node { return constantNode(name, desc, "geoTargetConstants") }
	object := func(name, desc string, fields ...node) node { return objectNode(name, desc, fields...) }
	members := []node{
		object("hotel_id", "Hotel ID of the advertiser", text("Hotel ID")),
		object("hotel_class", "Star class of the hotel", wholeNode("value", "Class from 1 to 5", 1, 5)),
		object("hotel_country_region", "Country or region of the hotel", geo("country_region_criterion", "Constant of Google, geoTargetConstants/N")),
		object("hotel_state", "State of the hotel", geo("state_criterion", "Constant of Google, geoTargetConstants/N")),
		object("hotel_city", "City of the hotel", geo("city_criterion", "Constant of Google, geoTargetConstants/N")),
		object("product_category", "Product category of Google",
			wholeNode("category_id", "ID of the google_product_category", 1, math.MaxInt64),
			enumNode("level", "Level of the category", enumNamed("ProductCategoryLevelEnum.ProductCategoryLevel"))),
		object("product_brand", "Product brand", text("Brand")),
		object("product_channel", "Locality of the offer", enumNode("channel", "Locality", enumNamed("ProductChannelEnum.ProductChannel"))),
		object("product_channel_exclusivity", "Availability of the offer",
			enumNode("channel_exclusivity", "Availability", enumNamed("ProductChannelExclusivityEnum.ProductChannelExclusivity"))),
		object("product_condition", "Condition of the offer", enumNode("condition", "Condition", enumNamed("ProductConditionEnum.ProductCondition"))),
		object("product_custom_attribute", "Custom attribute", text("Value of the attribute"),
			enumNode("index", "Index of the attribute", enumNamed("ProductCustomAttributeIndexEnum.ProductCustomAttributeIndex"))),
		object("product_item_id", "Item ID", text("Item ID")),
		object("product_type", "Product type", text("Value of the type"),
			enumNode("level", "Level of the type", enumNamed("ProductTypeLevelEnum.ProductTypeLevel"))),
		object("activity_id", "Activity ID of the advertiser", text("Activity ID")),
		object("activity_rating", "Rating of the activity", wholeNode("value", "Rating from 1 to 5, 5 is best", 1, 5)),
		object("activity_country", "Country of the activity", geo("value", "Constant of Google, geoTargetConstants/N")),
		object("activity_state", "State of the activity", geo("value", "Constant of Google, geoTargetConstants/N")),
		object("activity_city", "City of the activity", geo("value", "Constant of Google, geoTargetConstants/N")),
		object("unknown_listing_dimension", "All listings that no other dimension covers"),
	}
	names := make([]string, 0, len(members))
	for _, m := range members {
		names = append(names, m.name)
	}
	return oneOf(objectNode("dimensions", "Each entry names exactly one dimension", members...), names...)
}

// listingScopeRules requires the members Google needs for the dimensions that pair a value with a level.
func listingScopeRules(in map[string]any) error {
	needs := map[string][]string{"product_category": {"category_id", "level"}, "product_custom_attribute": {"value", "index"},
		"product_type": {"value", "level"}, "product_channel": {"channel"}, "product_channel_exclusivity": {"channel_exclusivity"},
		"product_condition": {"condition"}}
	list, _ := in["dimensions"].([]any)
	for i, item := range list {
		dimension, _ := item.(map[string]any)
		for name, members := range needs {
			if object, ok := dimension[name].(map[string]any); ok {
				if err := requireMembers(object, "listing_scope.dimensions["+strconv.Itoa(i)+"]."+name, members...); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
