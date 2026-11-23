package googleads

const (
	campaignLabelsCreateID = Provider + ".campaign-labels.create"
	campaignLabelsRemoveID = Provider + ".campaign-labels.remove"
	adGroupLabelsCreateID  = Provider + ".ad-group-labels.create"
	adGroupLabelsRemoveID  = Provider + ".ad-group-labels.remove"
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

var campaignLabelsCreate, campaignLabelsRemove = campaignLabelsFamily.descriptors()
var adGroupLabelsCreate, adGroupLabelsRemove = adGroupLabelsFamily.descriptors()
