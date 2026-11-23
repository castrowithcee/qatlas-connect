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

	adGroupCriterionCustomizersCreateID = Provider + ".ad-group-criterion-customizers.create"
	adGroupCriterionCustomizersRemoveID = Provider + ".ad-group-criterion-customizers.remove"
	customerCustomizersCreateID         = Provider + ".customer-customizers.create"
	customerCustomizersRemoveID         = Provider + ".customer-customizers.remove"
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
// are keyed by the IDs of the object and the attribute, and have no update.
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

// The criterion is named by two IDs, so the key of its customizer holds three. Google requires a keyword criterion.
var adGroupCriterionCustomizersFamily = setLinkFamily{
	tool: "ad-group-criterion-customizers", argument: "ad_group_criterion_customizers", group: "customizers", value: true,
	service: "/adGroupCriterionCustomizers", collection: "adGroupCriterionCustomizers", key: "adGroupCriterionCustomizer",
	resultKey: "ad_group_criterion_customizer", noun: "ad group criterion customizer",
	parts: []setLinkPart{{name: "ad_group_criterion", collection: "adGroupCriteria", noun: "Ad group criterion", ids: 2},
		{name: "customizer_attribute", collection: "customizerAttributes", noun: "Customizer attribute"}},
	example: `{"ad_group_criterion":"customers/1234567890/adGroupCriteria/51~81","customizer_attribute":` +
		`"customers/1234567890/customizerAttributes/71","value":{"type":"TEXT","string_value":"Spring"}}`,
	removeExample: "customers/1234567890/adGroupCriterionCustomizers/51~81~71",
	rpc:           "AdGroupCriterionCustomizerService.MutateAdGroupCriterionCustomizers",
	effect:        "sets the value of a customizer attribute for an ad group criterion, which must be a keyword",
	sensitivity:   "which customizer values an ad group criterion of a Google Ads account has, which ads insert into their text",
}

// The account is the one of the call, so a customer customizer has the attribute as its only part. The resource
// has no member that names the account.
var customerCustomizersFamily = setLinkFamily{
	tool: "customer-customizers", argument: "customer_customizers", group: "customizers", value: true,
	service: "/customerCustomizers", collection: "customerCustomizers", key: "customerCustomizer",
	resultKey: "customer_customizer", noun: "customer customizer",
	parts:         []setLinkPart{{name: "customizer_attribute", collection: "customizerAttributes", noun: "Customizer attribute"}},
	example:       `{"customizer_attribute":"customers/1234567890/customizerAttributes/71","value":{"type":"TEXT","string_value":"Spring"}}`,
	removeExample: "customers/1234567890/customerCustomizers/71",
	rpc:           "CustomerCustomizerService.MutateCustomerCustomizers",
	effect:        "sets the value of a customizer attribute for the account itself",
	sensitivity:   "which customizer values a Google Ads account has itself, which ads insert into their text",
}

var campaignCustomizersCreate, campaignCustomizersRemove = campaignCustomizersFamily.descriptors()
var adGroupCustomizersCreate, adGroupCustomizersRemove = adGroupCustomizersFamily.descriptors()
var adGroupCriterionCustomizersCreate, adGroupCriterionCustomizersRemove = adGroupCriterionCustomizersFamily.descriptors()
var customerCustomizersCreate, customerCustomizersRemove = customerCustomizersFamily.descriptors()
