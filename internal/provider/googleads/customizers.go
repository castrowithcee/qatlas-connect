package googleads

import (
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	campaignCustomizersCreateID = Provider + ".campaign-customizers.create"
	campaignCustomizersRemoveID = Provider + ".campaign-customizers.remove"
	adGroupCustomizersCreateID  = Provider + ".ad-group-customizers.create"
	adGroupCustomizersRemoveID  = Provider + ".ad-group-customizers.remove"
)

// customizerValueMax is Qatlas's own bound of string_value in characters. The v26 definition of CustomizerValue
// names none and says only that the content must match the type.
const customizerValueMax = 1024

// customizerValueTypes are the types of CustomizerValue.type, the same four as of a customizer attribute.
var customizerValueTypes = customizerAttributeTypes

var customizerValueSchema = object(schema{
	"type":         customizerAttributeTypeSchema,
	"string_value": schema{"type": "string", "minLength": 1, "maxLength": customizerValueMax},
}, "type", "string_value")

// customizerValueNode is the value as Google answers with it.
func customizerValueNode() node {
	return objectNode("value", "", enumNode("type", "", customizerValueTypes),
		textNode("string_value", "", customizerValueMax*4))
}

// customizerValueOf checks the value of a create entry and returns it as the request holds it.
func customizerValueOf(at string, raw any) (map[string]any, error) {
	in, ok := raw.(map[string]any)
	if !ok || len(in) != 2 {
		return nil, badRequest(at + " must be an object with exactly type and string_value")
	}
	kind, _ := in["type"].(string)
	text, _ := in["string_value"].(string)
	if !slices.Contains(customizerValueTypes, kind) {
		return nil, badRequest(at + ".type must be one of " + strings.Join(customizerValueTypes, ", "))
	}
	if n := utf8.RuneCountInString(text); n < 1 || n > customizerValueMax || hasControl(text) || !utf8.ValidString(text) {
		return nil, badRequest(at + ".string_value must be 1 to " + strconv.Itoa(customizerValueMax) +
			" characters without control characters")
	}
	return map[string]any{"type": kind, "stringValue": text}, nil
}

// The customizer services attach a value of a customizer attribute to an object. Like the label links they
// are keyed by the IDs of the object and the attribute, and have no update. Customizers of other levels join
// this family with their own parts.
var campaignCustomizersFamily = setLinkFamily{
	tool: "campaign-customizers", argument: "campaign_customizers", group: "customizers", value: true,
	service: "/campaignCustomizers", collection: "campaignCustomizers", key: "campaignCustomizer",
	resultKey: "campaign_customizer", noun: "campaign customizer",
	parts: []setLinkPart{{name: "campaign", collection: "campaigns", noun: "Campaign"},
		{name: "customizer_attribute", collection: "customizerAttributes", noun: "Customizer attribute"}},
	example: `{"campaign":"customers/1234567890/campaigns/21","customizer_attribute":` +
		`"customers/1234567890/customizerAttributes/71","value":{"type":"TEXT","string_value":"Spring"}}`,
	removeExample: "customers/1234567890/campaignCustomizers/21~71",
	rpc:           "CampaignCustomizerService.MutateCampaignCustomizers",
	effect:        "sets the value of a customizer attribute for a campaign",
	sensitivity:   "which customizer values a campaign of a Google Ads account has, which ads insert into their text",
}

var adGroupCustomizersFamily = setLinkFamily{
	tool: "ad-group-customizers", argument: "ad_group_customizers", group: "customizers", value: true,
	service: "/adGroupCustomizers", collection: "adGroupCustomizers", key: "adGroupCustomizer",
	resultKey: "ad_group_customizer", noun: "ad group customizer",
	parts: []setLinkPart{{name: "ad_group", collection: "adGroups", noun: "Ad group"},
		{name: "customizer_attribute", collection: "customizerAttributes", noun: "Customizer attribute"}},
	example: `{"ad_group":"customers/1234567890/adGroups/51","customizer_attribute":` +
		`"customers/1234567890/customizerAttributes/71","value":{"type":"TEXT","string_value":"Spring"}}`,
	removeExample: "customers/1234567890/adGroupCustomizers/51~71",
	rpc:           "AdGroupCustomizerService.MutateAdGroupCustomizers",
	effect:        "sets the value of a customizer attribute for an ad group",
	sensitivity:   "which customizer values an ad group of a Google Ads account has, which ads insert into their text",
}

var campaignCustomizersCreate, campaignCustomizersRemove = campaignCustomizersFamily.descriptors()
var adGroupCustomizersCreate, adGroupCustomizersRemove = adGroupCustomizersFamily.descriptors()
