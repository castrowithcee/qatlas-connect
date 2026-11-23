package googleads

const (
	campaignLabelsCreateID = Provider + ".campaign-labels.create"
	campaignLabelsRemoveID = Provider + ".campaign-labels.remove"
	adGroupLabelsCreateID  = Provider + ".ad-group-labels.create"
	adGroupLabelsRemoveID  = Provider + ".ad-group-labels.remove"

	adGroupAdLabelsCreateID        = Provider + ".ad-group-ad-labels.create"
	adGroupAdLabelsRemoveID        = Provider + ".ad-group-ad-labels.remove"
	adGroupCriterionLabelsCreateID = Provider + ".ad-group-criterion-labels.create"
	adGroupCriterionLabelsRemoveID = Provider + ".ad-group-criterion-labels.remove"
)

// The label assignments are links of two parts like the asset set links. Their services take no
// response_content_type and answer with resource names only, hence noContent.
var campaignLabelsFamily = setLinkFamily{
	tool: "campaign-labels", argument: "campaign_labels", group: "labels", noContent: true,
	service: "/campaignLabels", collection: "campaignLabels", resultKey: "campaign_label",
	noun: "campaign label",
	parts: []setLinkPart{{name: "campaign", collection: "campaigns", noun: "Campaign"},
		{name: "label", collection: "labels", noun: "Label"}},
	example:       `{"campaign":"customers/1234567890/campaigns/21","label":"customers/1234567890/labels/61"}`,
	removeExample: "customers/1234567890/campaignLabels/21~61",
	rpc:           "CampaignLabelService.MutateCampaignLabels",
	effect:        "attaches a label to a campaign",
	sensitivity:   "which labels a campaign of a Google Ads account carries",
}

var adGroupLabelsFamily = setLinkFamily{
	tool: "ad-group-labels", argument: "ad_group_labels", group: "labels", noContent: true,
	service: "/adGroupLabels", collection: "adGroupLabels", resultKey: "ad_group_label",
	noun: "ad group label",
	parts: []setLinkPart{{name: "ad_group", collection: "adGroups", noun: "Ad group"},
		{name: "label", collection: "labels", noun: "Label"}},
	example:       `{"ad_group":"customers/1234567890/adGroups/51","label":"customers/1234567890/labels/61"}`,
	removeExample: "customers/1234567890/adGroupLabels/51~61",
	rpc:           "AdGroupLabelService.MutateAdGroupLabels",
	effect:        "attaches a label to an ad group",
	sensitivity:   "which labels an ad group of a Google Ads account carries",
}

// The ad and the criterion are themselves named by two IDs, so the key of their label links holds three.
var adGroupAdLabelsFamily = setLinkFamily{
	tool: "ad-group-ad-labels", argument: "ad_group_ad_labels", group: "labels", noContent: true,
	service: "/adGroupAdLabels", collection: "adGroupAdLabels", resultKey: "ad_group_ad_label",
	noun: "ad group ad label",
	parts: []setLinkPart{{name: "ad_group_ad", collection: "adGroupAds", noun: "Ad", ids: 2},
		{name: "label", collection: "labels", noun: "Label"}},
	example:       `{"ad_group_ad":"customers/1234567890/adGroupAds/51~71","label":"customers/1234567890/labels/61"}`,
	removeExample: "customers/1234567890/adGroupAdLabels/51~71~61",
	rpc:           "AdGroupAdLabelService.MutateAdGroupAdLabels",
	effect:        "attaches a label to an ad",
	sensitivity:   "which labels an ad of a Google Ads account carries",
}

var adGroupCriterionLabelsFamily = setLinkFamily{
	tool: "ad-group-criterion-labels", argument: "ad_group_criterion_labels", group: "labels", noContent: true,
	service: "/adGroupCriterionLabels", collection: "adGroupCriterionLabels", resultKey: "ad_group_criterion_label",
	noun: "ad group criterion label",
	parts: []setLinkPart{{name: "ad_group_criterion", collection: "adGroupCriteria", noun: "Ad group criterion", ids: 2},
		{name: "label", collection: "labels", noun: "Label"}},
	example:       `{"ad_group_criterion":"customers/1234567890/adGroupCriteria/51~81","label":"customers/1234567890/labels/61"}`,
	removeExample: "customers/1234567890/adGroupCriterionLabels/51~81~61",
	rpc:           "AdGroupCriterionLabelService.MutateAdGroupCriterionLabels",
	effect:        "attaches a label to an ad group criterion",
	sensitivity:   "which labels an ad group criterion of a Google Ads account carries",
}

var campaignLabelsCreate, campaignLabelsRemove = campaignLabelsFamily.descriptors()
var adGroupLabelsCreate, adGroupLabelsRemove = adGroupLabelsFamily.descriptors()
var adGroupAdLabelsCreate, adGroupAdLabelsRemove = adGroupAdLabelsFamily.descriptors()
var adGroupCriterionLabelsCreate, adGroupCriterionLabelsRemove = adGroupCriterionLabelsFamily.descriptors()
