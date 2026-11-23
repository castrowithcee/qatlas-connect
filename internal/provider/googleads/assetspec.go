package googleads

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/localfile"
)

// The asset types are described once, as data: one description yields the input schema of a create and of an
// update, the checks of the arguments, the request body and the field mask, the output schema, and the
// mapping of the answer. A member that is not in a description is neither accepted nor returned.

type assetKind int

const (
	kText assetKind = iota
	kEnum
	kDate
	kAddress
	kSuffix
	kInt
	kBool
	kTexts
	kAddresses
	kAssetRef
	kActionRef
	kObject
	kObjects
	kBytes
	kRef
)

type assetField struct {
	name string
	kind assetKind
	// req: Google requires the member to create the asset. fixed: it is set at creation and never changed.
	// out: Google only returns it.
	req, fixed, out bool
	// group names the members of one oneof of the proto: at most one of them is given.
	group string
	// limit is the number of items of a list; minItems is the smallest number Google documents.
	limit, minItems int
	// typeMember marks the member that holds the data of one asset type.
	typeMember bool
	sub        *assetSpec
	// values restricts an enum to the values the tool offers, coll names the collection of a resource name of
	// the account (kRef), and pattern is the form a text must have.
	values  []string
	coll    string
	pattern *regexp.Regexp
}

type assetSpec struct {
	fields []assetField
	// need lists the oneof groups of which one member is required to create the asset.
	need []string
}

func text(name string) assetField     { return assetField{name: name, kind: kText} }
func enum(name string) assetField     { return assetField{name: name, kind: kEnum} }
func date(name string) assetField     { return assetField{name: name, kind: kDate} }
func link(name string) assetField     { return assetField{name: name, kind: kAddress} }
func number(name string) assetField   { return assetField{name: name, kind: kInt} }
func flag(name string) assetField     { return assetField{name: name, kind: kBool} }
func texts(name string) assetField    { return assetField{name: name, kind: kTexts} }
func assetRef(name string) assetField { return assetField{name: name, kind: kAssetRef} }
func member(name string, sub *assetSpec) assetField {
	return assetField{name: name, kind: kObject, sub: sub}
}
func members(name string, sub *assetSpec) assetField {
	return assetField{name: name, kind: kObjects, sub: sub}
}

func ref(name, collection string) assetField {
	return assetField{name: name, kind: kRef, coll: collection}
}

func (f assetField) among(values ...string) assetField { f.values = values; return f }
func (f assetField) matching(re string) assetField {
	f.pattern = regexp.MustCompile(re)
	return f
}
func (f assetField) required() assetField      { f.req = true; return f }
func (f assetField) created() assetField       { f.fixed = true; return f }
func (f assetField) output() assetField        { f.out = true; return f }
func (f assetField) oneof(g string) assetField { f.group = g; return f }
func (f assetField) items(most int) assetField { f.limit = most; return f }
func (f assetField) atLeast(n int) assetField  { f.minItems = n; return f }

func spec(fields ...assetField) *assetSpec { return &assetSpec{fields: fields} }

var (
	moneySpec = spec(text("currency_code").required(), number("amount_micros").required())

	adScheduleSpec = spec(enum("day_of_week"), number("start_hour"), enum("start_minute"), number("end_hour"),
		enum("end_minute"))

	singleChoiceSpec = spec(texts("answers").required().atLeast(2).items(12))

	partnerIDSpec = &assetSpec{fields: []assetField{number("int_value").oneof("value"), text("string_value").oneof("value")},
		need: []string{"value"}}

	imageSizeSpec = spec(number("height_pixels"), number("width_pixels"), text("url"))

	attestationSpec = spec(enum("status"), enum("source"))
)

func scheduleList() assetField { return members("ad_schedule_targets", adScheduleSpec).items(42) }

func dynamicTail() []assetField {
	return []assetField{texts("contextual_keywords"), text("android_app_link"), text("ios_app_link"), number("ios_app_store_id")}
}

func withTail(fields ...assetField) *assetSpec { return spec(append(fields, dynamicTail()...)...) }

// assetType is one member of the asset_data oneof of the asset resource. immutable types cannot be changed
// after the asset exists.
type assetType struct {
	key       string
	spec      *assetSpec
	immutable bool
}

// The types Google returns in v26 minus those left out on purpose: location_asset is output only, and the
// delivery of a lead form to a webhook (delivery_methods) is not offered anywhere.
var assetTypes = []assetType{
	{"text_asset", spec(text("text").required()), true},
	{"image_asset", spec(assetField{name: "data", kind: kBytes, req: true}, number("file_size").output(),
		enum("mime_type").output(), member("full_size", imageSizeSpec).output()), true},
	{"media_bundle_asset", spec(assetField{name: "data", kind: kBytes, req: true}), true},
	{"youtube_video_asset", spec(text("youtube_video_id").required(), text("youtube_video_title").output()), true},
	{"youtube_video_list_asset", spec(members("youtube_videos", spec(assetRef("asset").required())).required().items(5)), true},
	{"sitelink_asset", spec(text("link_text").required(), text("description1"), text("description2"), date("start_date"),
		date("end_date"), scheduleList()), false},
	{"callout_asset", spec(text("callout_text").required(), date("start_date"), date("end_date"), scheduleList()), false},
	{"structured_snippet_asset", spec(text("header").required(), texts("values").required().atLeast(3).items(10)), false},
	{"call_asset", spec(text("country_code").required(), text("phone_number").required(),
		enum("call_conversion_reporting_state"), assetField{name: "call_conversion_action", kind: kActionRef},
		scheduleList()), false},
	{"mobile_app_asset", spec(text("app_id").required(), enum("app_store").required(), text("link_text").required(),
		date("start_date"), date("end_date")), false},
	{"hotel_callout_asset", spec(text("text").required(), text("language_code").required()), false},
	{"page_feed_asset", spec(link("page_url").required(), texts("labels")), false},
	{"price_asset", spec(enum("type").required(), enum("price_qualifier"), text("language_code").required(),
		members("price_offerings", spec(text("header").required(), text("description").required(),
			member("price", moneySpec).required(), enum("unit"), link("final_url").required(),
			link("final_mobile_url"))).items(8).atLeast(3)), false},
	{"promotion_asset", &assetSpec{fields: []assetField{text("promotion_target").required(), enum("discount_modifier"),
		date("redemption_start_date"), date("redemption_end_date"), enum("occasion"), text("language_code"),
		date("start_date"), date("end_date"), scheduleList(), text("terms_and_conditions_text"),
		link("terms_and_conditions_uri"),
		member("external_redeem_info", &assetSpec{fields: []assetField{member("partner_promotion_id", partnerIDSpec),
			enum("code_type").required(), enum("code_source").required()}}).created(),
		number("percent_off").oneof("discount"), member("money_amount_off", moneySpec).oneof("discount"),
		text("promotion_code").oneof("trigger"), member("orders_over_amount", moneySpec).oneof("trigger"),
		member("promotion_barcode_info", spec(enum("type"), text("barcode_content"))).oneof("trigger"),
		member("promotion_qr_code_info", spec(text("qr_code_content"))).oneof("trigger")},
		need: []string{"discount"}}, false},
	{"lead_form_asset", spec(text("business_name").required(), enum("call_to_action_type").required(),
		text("call_to_action_description").required(), text("headline").required(), text("description").required(),
		link("privacy_policy_url").required(), text("post_submit_headline"), text("post_submit_description"),
		members("fields", spec(enum("input_type"), member("single_choice_answers", singleChoiceSpec).oneof("answers"),
			flag("has_location_answer").oneof("answers"))),
		members("custom_question_fields", spec(text("custom_question_text"),
			member("single_choice_answers", singleChoiceSpec).oneof("answers"),
			flag("has_location_answer").oneof("answers"))).items(5),
		enum("post_submit_call_to_action_type"), assetRef("background_image_asset"), enum("desired_intent"),
		text("custom_disclosure")), false},
	{"book_on_google_asset", spec(), true},
	{"call_to_action_asset", spec(enum("call_to_action")), true},
	{"business_message_asset", &assetSpec{fields: []assetField{enum("message_provider").required(),
		text("starter_message").required(),
		member("call_to_action", spec(enum("call_to_action_selection").required(),
			text("call_to_action_description").required())),
		member("whatsapp_info", spec(text("country_code").required(), text("phone_number").required())).oneof("provider"),
		member("facebook_messenger_info", spec(text("page_name").required())).oneof("provider"),
		member("zalo_info", &assetSpec{fields: []assetField{number("oa_id").oneof("zalo"), text("custom_name").oneof("zalo")},
			need: []string{"zalo"}}).oneof("provider"),
		member("line_info", spec(text("line_id").required())).oneof("provider"),
		member("sms_info", spec(text("country_code").required(), text("phone_number").required())).oneof("provider")},
		need: []string{"provider"}}, false},
	{"app_deep_link_asset", spec(text("app_deep_link_uri")), true},
	{"demand_gen_carousel_card_asset", spec(assetRef("marketing_image_asset"), assetRef("square_marketing_image_asset"),
		assetRef("portrait_marketing_image_asset"), text("headline").required(), text("call_to_action_text")), true},
	{"hotel_property_asset", spec(text("place_id"), text("hotel_address"), text("hotel_name")), true},
	{"dynamic_education_asset", withTail(text("program_id").required(), text("location_id"),
		text("program_name").required(), text("subject"), text("program_description"), text("school_name"),
		text("address"), texts("similar_program_ids"), link("thumbnail_image_url"), link("image_url")), false},
	{"dynamic_real_estate_asset", withTail(text("listing_id").required(), text("listing_name").required(),
		text("city_name"), text("description"), text("address"), text("price"), link("image_url"),
		text("property_type"), text("listing_type"), text("formatted_price"), texts("similar_listing_ids")), false},
	{"dynamic_custom_asset", withTail(text("id").required(), text("id2"), text("item_title").required(),
		text("item_subtitle"), text("item_description"), text("item_address"), text("item_category"), text("price"),
		text("sale_price"), text("formatted_price"), text("formatted_sale_price"), link("image_url"),
		texts("similar_ids")), false},
	{"dynamic_hotels_and_rentals_asset", withTail(text("property_id").required(), text("property_name").required(),
		link("image_url"), text("destination_name"), text("description"), text("price"), text("sale_price"),
		number("star_rating"), text("category"), text("address"), text("formatted_price"),
		text("formatted_sale_price"), texts("similar_property_ids")), false},
	{"dynamic_flights_asset", withTail(text("destination_id").required(), text("origin_id"),
		text("flight_description").required(), link("image_url"), text("destination_name"), text("origin_name"),
		text("flight_price"), text("flight_sale_price"), text("formatted_price"), text("formatted_sale_price"),
		texts("similar_destination_ids"), text("custom_mapping")), false},
	{"dynamic_travel_asset", withTail(text("destination_id").required(), text("origin_id"), text("title").required(),
		text("destination_name"), text("destination_address"), text("origin_name"), text("price"), text("sale_price"),
		text("formatted_price"), text("formatted_sale_price"), text("category"), texts("similar_destination_ids"),
		link("image_url")), false},
	{"dynamic_local_asset", withTail(text("deal_id").required(), text("deal_name").required(), text("subtitle"),
		text("description"), text("price"), text("sale_price"), link("image_url"), text("address"), text("category"),
		text("formatted_price"), text("formatted_sale_price"), texts("similar_deal_ids")), false},
	{"dynamic_jobs_asset", withTail(text("job_id").required(), text("location_id"), text("job_title").required(),
		text("job_subtitle"), text("description"), link("image_url"), text("job_category"), text("address"),
		text("salary"), texts("similar_job_ids")), false},
}

// assetCommon are the members of the asset resource outside asset_data.
func assetCommon() []assetField {
	return []assetField{text("name"), assetField{name: "final_urls", kind: kAddresses, limit: maxAssetURLs},
		assetField{name: "final_mobile_urls", kind: kAddresses, limit: maxAssetURLs}, link("tracking_url_template"),
		members("url_custom_parameters", spec(text("key").required(), text("value").required())).items(maxAssetURLs),
		assetField{name: "final_url_suffix", kind: kSuffix},
		member("synthetic_content_info", &assetSpec{fields: []assetField{member("advertiser_attestation", attestationSpec),
			member("system_attestation", attestationSpec).output()}}),
		number("id").output(), enum("type").output(), enum("source").output(), enum("orientation").output()}
}

// assetOperationSpec is the asset of one operation: the common members and one member per asset type.
func assetOperationSpec() *assetSpec {
	fields := assetCommon()
	for _, t := range assetTypes {
		f := member(t.key, t.spec).oneof("asset_data")
		f.typeMember, f.fixed = true, false
		fields = append(fields, f)
	}
	return &assetSpec{fields: fields, need: []string{"asset_data"}}
}

// updatableSpec is what an update may name: no immutable type, and in a type no member that is set at
// creation only.
func updatableSpec() *assetSpec {
	out := &assetSpec{}
	for _, f := range assetOperationSpec().fields {
		if f.out || f.typeMember && isImmutable(f.name) {
			continue
		}
		if f.typeMember {
			f.sub = updatable(f.sub)
		}
		out.fields = append(out.fields, f)
	}
	return out
}

func updatable(s *assetSpec) *assetSpec {
	out := &assetSpec{}
	for _, f := range s.fields {
		if f.fixed || f.out || f.kind == kBytes {
			continue
		}
		f.req = false
		if f.sub != nil {
			f.sub = updatable(f.sub)
		}
		out.fields = append(out.fields, f)
	}
	return out
}

func isImmutable(key string) bool {
	for _, t := range assetTypes {
		if t.key == key {
			return t.immutable
		}
	}
	return false
}

// Qatlas's own bounds: Google documents none of them for these members except where a spec says so.
const (
	maxAssetText  = 1024
	maxAssetItems = 100
	maxAssetURLs  = 20
	// maxMediaBytes is the largest image or bundle: Google's help pages give 5120 KB for images. Content
	// inline in the arguments stays within the 4 MiB that every file tool takes inline.
	maxMediaBytes    = 5120 << 10
	maxInlineBytes   = 4 << 20
	maxInlineBase64  = (maxInlineBytes + 2) / 3 * 4
	maxMediaPerCall  = 10 << 20
	maxAssetsPerCall = 10
)

var (
	enumPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)
)

const (
	assetNamePattern  = `^customers/[0-9]{1,10}/assets/[0-9]{1,20}$`
	actionNamePattern = conversionActionPattern
)

// ---- input schema ----

func (f assetField) schema(update bool) schema {
	switch f.kind {
	case kText:
		if f.pattern != nil {
			return schema{"type": "string", "maxLength": maxAssetText, "pattern": "^$|" + f.pattern.String()}
		}
		return schema{"type": "string", "maxLength": maxAssetText}
	case kEnum:
		if f.values != nil {
			return schema{"type": "string", "enum": f.values}
		}
		return schema{"type": "string", "pattern": enumPattern.String()}
	case kRef:
		return schema{"type": "string", "pattern": "^$|^customers/[0-9]{1,10}/" + f.coll + "/[0-9]{1,20}$"}
	case kDate:
		return schema{"type": "string", "pattern": "^$|" + datePattern.String()}
	case kAddress:
		return schema{"type": "string", "maxLength": maxURLArgument}
	case kSuffix:
		return schema{"type": "string", "maxLength": maxURLArgument}
	case kInt:
		return integerSchema
	case kBool:
		return booleanSchema
	case kTexts:
		return schema{"type": "array", "maxItems": f.maxItems(maxAssetItems), "items": schema{"type": "string",
			"minLength": 1, "maxLength": maxAssetText}}
	case kAddresses:
		return schema{"type": "array", "maxItems": f.maxItems(maxAssetURLs),
			"items": schema{"type": "string", "minLength": 1, "maxLength": maxURLArgument}}
	case kAssetRef:
		return schema{"type": "string", "pattern": "^$|" + assetNamePattern}
	case kActionRef:
		return schema{"type": "string", "pattern": "^$|" + actionNamePattern}
	case kObject:
		return f.sub.inputSchema(update)
	case kObjects:
		return schema{"type": "array", "maxItems": f.maxItems(maxAssetItems), "items": f.sub.inputSchema(update)}
	}
	return schema{}
}

func (f assetField) maxItems(fallback int) int {
	if f.limit > 0 {
		return f.limit
	}
	return fallback
}

func (s *assetSpec) inputSchema(update bool) schema {
	properties := schema{}
	var required []string
	for _, f := range s.fields {
		if f.out {
			continue
		}
		if f.kind == kBytes {
			properties[localfile.LocalPathArgument] = schema{"type": "string", "minLength": 1, "maxLength": 4096}
			properties[localfile.ContentArgument] = schema{"type": "string", "maxLength": maxInlineBase64}
			continue
		}
		properties[f.name] = f.schema(update)
		if f.req && !update {
			required = append(required, f.name)
		}
	}
	return object(properties, required...)
}

// ---- checks and request body ----

// assetCall holds what the checks of one call need: the account, whether it is an update, and the loading of
// image bytes, which counts what the call carries.
type assetCall struct {
	customerID string
	update     bool
	load       func(path, content *string, argument string) ([]byte, error)
}

func argumentError(path, rule string) error { return badRequest(path + " " + rule) }

// object checks the members given for the spec and returns the wire form, and the paths of the field mask:
// every member that was given, a cleared one included. Within a type member the paths name its members.
func (w *assetCall) object(s *assetSpec, in map[string]any, path string) (map[string]any, []string, error) {
	known := map[string]bool{}
	for _, f := range s.fields {
		switch {
		case f.out:
		case f.kind == kBytes:
			known[localfile.LocalPathArgument], known[localfile.ContentArgument] = true, true
		default:
			known[f.name] = true
		}
	}
	for name := range in {
		if !known[name] {
			return nil, nil, argumentError(path+name, "is not an argument here")
		}
	}
	wire := map[string]any{}
	var mask []string
	groups := map[string]string{}
	for _, f := range s.fields {
		if f.out {
			continue
		}
		here := path + f.name
		if f.kind == kBytes {
			_, hasPath := in[localfile.LocalPathArgument]
			_, hasContent := in[localfile.ContentArgument]
			if !f.req && !hasPath && !hasContent {
				continue
			}
			data, err := w.bytes(in, path)
			if err != nil {
				return nil, nil, err
			}
			wire["data"] = base64.StdEncoding.EncodeToString(data)
			mask = append(mask, "data")
			continue
		}
		v, given := in[f.name]
		if v == nil {
			given = false
		}
		if !given {
			if f.req && !w.update {
				return nil, nil, argumentError(here, "is required")
			}
			continue
		}
		if f.group != "" {
			if other, dup := groups[f.group]; dup {
				return nil, nil, argumentError(here, "and "+other+" exclude each other")
			}
			groups[f.group] = f.name
		}
		value, nested, err := w.value(f, v, here)
		if err != nil {
			return nil, nil, err
		}
		if value == nil && f.req && !w.update {
			return nil, nil, argumentError(here, "must not be empty")
		}
		if value != nil {
			wire[camel(f.name)] = value
		}
		if f.typeMember {
			for _, m := range nested {
				mask = append(mask, camel(f.name)+"."+m)
			}
		} else {
			mask = append(mask, camel(f.name))
		}
	}
	if !w.update {
		for _, g := range s.need {
			if groups[g] == "" {
				return nil, nil, argumentError(strings.TrimSpace(strings.TrimSuffix(path, ".")+" the asset"),
					"needs exactly one of the members of the group "+g)
			}
		}
	}
	return wire, mask, nil
}

func (w *assetCall) bytes(in map[string]any, path string) ([]byte, error) {
	if w.update {
		return nil, argumentError(path, "cannot change image content")
	}
	var local, content *string
	if v, ok := in[localfile.LocalPathArgument].(string); ok {
		local = &v
	}
	if v, ok := in[localfile.ContentArgument].(string); ok {
		content = &v
	}
	return w.load(local, content, path)
}

func plainText(path, v string, limit int) error {
	if len(v) > limit || !utf8.ValidString(v) || strings.ContainsFunc(v, unicode.IsControl) {
		return argumentError(path, "must be text of at most "+strconv.Itoa(limit)+" bytes without control characters")
	}
	return nil
}

// value checks one member. A nil value with a nil error clears the member on an update, and is nothing to
// send on a create.
func (w *assetCall) value(f assetField, v any, path string) (any, []string, error) {
	bad := func(rule string) (any, []string, error) { return nil, nil, argumentError(path, rule) }
	switch f.kind {
	case kText, kEnum, kDate, kAddress, kSuffix, kAssetRef, kActionRef, kRef:
		s, ok := v.(string)
		if !ok {
			return bad("must be a string")
		}
		if s == "" {
			return nil, nil, nil
		}
		switch f.kind {
		case kText:
			if err := plainText(path, s, maxAssetText); err != nil {
				return nil, nil, err
			}
			if f.pattern != nil && !f.pattern.MatchString(s) {
				return bad("must have the form " + f.pattern.String())
			}
		case kEnum:
			if f.values != nil && !slices.Contains(f.values, s) {
				return bad("must be one of " + strings.Join(f.values, ", "))
			}
			if !enumPattern.MatchString(s) || strings.HasPrefix(s, "UNSPECIFIED") || strings.HasPrefix(s, "UNKNOWN") {
				return bad("must be the name of an enum value of the API, such as SOME_VALUE")
			}
		case kDate:
			if !datePattern.MatchString(s) {
				return bad("must be a date as yyyy-mm-dd")
			}
		case kAddress:
			if err := checkAddress(path, s); err != nil {
				return nil, nil, err
			}
		case kSuffix:
			if err := checkSuffix(path, s); err != nil {
				return nil, nil, err
			}
		case kAssetRef:
			if !ownedName(s, w.customerID, "assets") {
				return bad("must be an asset of customer_id, customers/ID/assets/N; temporary IDs are not accepted")
			}
		case kActionRef:
			if !ownedName(s, w.customerID, "conversionActions") {
				return bad("must be a conversion action of customer_id, customers/ID/conversionActions/N")
			}
		case kRef:
			if !ownedResource(s, w.customerID, f.coll) {
				return bad("must be customers/ID/" + f.coll + "/N of customer_id; temporary IDs are not accepted")
			}
		}
		return s, nil, nil
	case kInt:
		n, ok := v.(json.Number)
		if !ok {
			return bad("must be an integer")
		}
		i, err := strconv.ParseInt(string(n), 10, 64)
		if err != nil {
			return bad("must be an integer")
		}
		return i, nil, nil
	case kBool:
		b, ok := v.(bool)
		if !ok {
			return bad("must be true or false")
		}
		return b, nil, nil
	case kTexts, kAddresses:
		list, ok := v.([]any)
		if !ok || len(list) > f.maxItems(maxAssetItems) {
			return bad("must be a list of at most " + strconv.Itoa(f.maxItems(maxAssetItems)) + " entries")
		}
		if len(list) == 0 {
			return nil, nil, nil
		}
		if f.minItems > 0 && len(list) < f.minItems {
			return bad("needs at least " + strconv.Itoa(f.minItems) + " entries")
		}
		out := make([]any, 0, len(list))
		for i, item := range list {
			s, ok := item.(string)
			if !ok || s == "" {
				return bad("must hold non-empty strings")
			}
			if f.kind == kAddresses {
				if err := checkAddress(path+"["+strconv.Itoa(i)+"]", s); err != nil {
					return nil, nil, err
				}
			} else if err := plainText(path+"["+strconv.Itoa(i)+"]", s, maxAssetText); err != nil {
				return nil, nil, err
			}
			out = append(out, s)
		}
		return out, nil, nil
	case kObject:
		m, ok := v.(map[string]any)
		if !ok {
			return bad("must be an object")
		}
		wire, mask, err := w.object(f.sub, m, path+".")
		if err != nil {
			return nil, nil, err
		}
		return wire, mask, nil
	case kObjects:
		list, ok := v.([]any)
		if !ok || len(list) > f.maxItems(maxAssetItems) {
			return bad("must be a list of at most " + strconv.Itoa(f.maxItems(maxAssetItems)) + " objects")
		}
		if len(list) == 0 {
			return nil, nil, nil
		}
		if f.minItems > 0 && len(list) < f.minItems {
			return bad("needs at least " + strconv.Itoa(f.minItems) + " entries")
		}
		out := make([]any, 0, len(list))
		for i, item := range list {
			m, ok := item.(map[string]any)
			if !ok {
				return bad("must hold objects")
			}
			wire, _, err := w.object(f.sub, m, path+"["+strconv.Itoa(i)+"].")
			if err != nil {
				return nil, nil, err
			}
			out = append(out, wire)
		}
		return out, nil, nil
	}
	return bad("is not supported")
}

// ---- output ----

func outputSchemaOf(f assetField) schema {
	switch f.kind {
	case kInt:
		return integerSchema
	case kBool:
		return booleanSchema
	case kTexts, kAddresses:
		return arrayOf(stringSchema)
	case kObject:
		return f.sub.outputSchema()
	case kObjects:
		return arrayOf(f.sub.outputSchema())
	}
	return stringSchema
}

func (s *assetSpec) outputSchema() schema {
	properties := schema{}
	for _, f := range s.fields {
		if f.kind != kBytes {
			properties[f.name] = outputSchemaOf(f)
		}
	}
	return object(properties)
}

// outObject maps an object of an answer. Only the members of the spec are taken, text is bounded, a number
// that is none fails the answer, and a resource name must belong to the account.
func outObject(s *assetSpec, raw map[string]any, customerID string) (map[string]any, bool) {
	out := map[string]any{}
	for _, f := range s.fields {
		if f.kind == kBytes {
			continue
		}
		v, ok := raw[camel(f.name)]
		if !ok || v == nil {
			continue
		}
		switch f.kind {
		case kText, kDate, kAddress, kSuffix:
			t, ok := v.(string)
			if !ok {
				return nil, false
			}
			out[f.name] = boundedText(t, maxLongText)
		case kEnum:
			t, ok := v.(string)
			if !ok {
				return nil, false
			}
			out[f.name] = code(t)
		case kAssetRef, kActionRef, kRef:
			t, ok := v.(string)
			collection := "assets"
			switch f.kind {
			case kActionRef:
				collection = "conversionActions"
			case kRef:
				collection = f.coll
			}
			if !ok || !ownedName(t, customerID, collection) {
				return nil, false
			}
			out[f.name] = t
		case kInt:
			var text string
			switch n := v.(type) {
			case json.Number:
				text = string(n)
			case string:
				text = n
			}
			i, err := strconv.ParseInt(text, 10, 64)
			if err != nil {
				return nil, false
			}
			out[f.name] = i
		case kBool:
			b, ok := v.(bool)
			if !ok {
				return nil, false
			}
			out[f.name] = b
		case kTexts, kAddresses:
			list, ok := v.([]any)
			if !ok {
				return nil, false
			}
			items := []string{}
			for _, item := range list[:min(len(list), maxListItems)] {
				t, ok := item.(string)
				if !ok {
					return nil, false
				}
				items = append(items, boundedText(t, maxLongText))
			}
			out[f.name] = items
		case kObject:
			m, ok := v.(map[string]any)
			if !ok {
				return nil, false
			}
			inner, ok := outObject(f.sub, m, customerID)
			if !ok {
				return nil, false
			}
			out[f.name] = inner
		case kObjects:
			list, ok := v.([]any)
			if !ok {
				return nil, false
			}
			items := []any{}
			for _, item := range list[:min(len(list), maxListItems)] {
				m, ok := item.(map[string]any)
				if !ok {
					return nil, false
				}
				inner, ok := outObject(f.sub, m, customerID)
				if !ok {
					return nil, false
				}
				items = append(items, inner)
			}
			out[f.name] = items
		}
	}
	return out, true
}

// decodeLoose decodes JSON with numbers kept as written, so a 64-bit integer survives.
func decodeLoose(raw []byte, out any) error {
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	if err := dec.Decode(out); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return io.ErrUnexpectedEOF
	}
	return nil
}
