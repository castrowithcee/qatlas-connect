package googleads

import (
	"bytes"
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	campaignLabelsCreateID = Provider + ".campaign-labels.create"
	campaignLabelsRemoveID = Provider + ".campaign-labels.remove"
	adGroupLabelsCreateID  = Provider + ".ad-group-labels.create"
	adGroupLabelsRemoveID  = Provider + ".ad-group-labels.remove"
	customerLabelsCreateID = Provider + ".customer-labels.create"
	customerLabelsRemoveID = Provider + ".customer-labels.remove"
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

// The account is the one of the call, so a customer label has the label as its only part.
var customerLabelsFamily = setLinkFamily{
	tool: "customer-labels", argument: "customer_labels", group: "labels", noContent: true,
	service: "/customerLabels", collection: "customerLabels", resultKey: "customer_label",
	noun:          "customer label",
	parts:         []setLinkPart{{name: "label", collection: "labels", noun: "Label"}},
	example:       `{"label":"customers/1234567890/labels/61"}`,
	removeExample: "customers/1234567890/customerLabels/61",
	rpc:           "CustomerLabelService.MutateCustomerLabels",
	effect:        "attaches a label to the account itself",
	sensitivity:   "which labels a Google Ads account carries itself",
}

var campaignLabelsCreate, campaignLabelsRemove = campaignLabelsFamily.descriptors()
var adGroupLabelsCreate, adGroupLabelsRemove = adGroupLabelsFamily.descriptors()
var customerLabelsCreate, customerLabelsRemove = customerLabelsDescriptors()

// customerLabelsDescriptors widens the label of a create entry: a label of a client account is usually made in
// the manager account of the connection, which Google resolves by the label ID alone.
func customerLabelsDescriptors() (capability.Descriptor, capability.Descriptor) {
	create, remove := customerLabelsFamily.descriptors()
	const was, now = "Label of this account", "Label of this account or of the login manager of the connection"
	create.InputSchema = bytes.ReplaceAll(create.InputSchema, []byte(was), []byte(now))
	create.Description = strings.Replace(create.Description, "Resource names in the arguments must belong to customer_id",
		"Resource names in the arguments must belong to customer_id, except the label, which may also belong to the "+
			"login_customer_id option of the connection", 1)
	for i := range create.Arguments {
		if create.Arguments[i].Name == customerLabelsFamily.argument {
			create.Arguments[i].Description = "One to 10 entries, each an object of label as customers/ID/labels/N, " +
				"where ID is customer_id or the login_customer_id option of the connection, each label once"
		}
	}
	return create, remove
}

// customerLabelOperations checks every entry: it names exactly a label, owned by the account of the call or by
// the login manager of the connection. No other account passes.
func customerLabelOperations(customerID, login string, items []any) ([]any, []string, error) {
	f := customerLabelsFamily
	if err := setLinkCount(f.argument, len(items)); err != nil {
		return nil, nil, err
	}
	ops := make([]any, 0, len(items))
	names := make([]string, 0, len(items))
	for i, item := range items {
		at := f.argument + "[" + strconv.Itoa(i) + "]"
		in, ok := item.(map[string]any)
		value, _ := in["label"].(string)
		if !ok || len(in) != 1 || value == "" {
			return nil, nil, badRequest(at + " must be an object with exactly label")
		}
		id, ok := numericIDOf(value, customerID, "labels")
		if !ok && login != "" {
			id, ok = numericIDOf(value, login, "labels")
		}
		if !ok {
			return nil, nil, badRequest(at + ".label must be customers/ID/labels/N of customer_id or of the login manager " +
				"of the connection; a temporary ID is not accepted")
		}
		name := f.nameOf(customerID, []string{id})
		if slices.Contains(names, name) {
			return nil, nil, badRequest("a " + f.noun + " appears in " + f.argument + " only once")
		}
		names = append(names, name)
		ops = append(ops, map[string]any{"create": map[string]any{"label": value}})
	}
	return ops, names, nil
}

// customerLabelsCreateHandler is the create handler of the family with the wider check of the label.
func customerLabelsCreateHandler(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	f := customerLabelsFamily
	tool := customerLabelsCreateID
	op := "create " + f.noun + "s"
	a, err := f.read(op, raw)
	if err != nil {
		return nil, err
	}
	env, err := handleEnv(ctx, resolved, secrets, red, op, tool)
	if err != nil {
		return nil, err
	}
	if !customerIDPattern.MatchString(a.CustomerID) {
		return nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if a.ResponseContentType != "" {
		return nil, badRequest("response_content_type is not offered by " + tool)
	}
	ops, names, err := customerLabelOperations(a.CustomerID, env.conn.login, a.Entries)
	if err != nil {
		return nil, err
	}
	c, err := env.settle(a.CustomerID, true)
	if err != nil {
		return nil, err
	}
	results, err := c.mutateList(ctx, listMutation{op: op, tool: tool, customerID: a.CustomerID,
		service: f.service, collection: f.collection, operations: ops, names: names,
		idOf: func(name string) (string, bool) {
			ids, ok := f.idsOf(name, a.CustomerID)
			return strings.Join(ids, "~"), ok
		},
		validateOnly: a.ValidateOnly})
	if err != nil {
		return nil, err
	}
	return f.result(op, a.CustomerID, results, a.ValidateOnly)
}
