package googleanalytics

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// Deleting an existing link is the only change a link allows: creating or changing one is not a tool. The
// Google Ads and Firebase links are deleted on the Admin API v1beta, the BigQuery and AdSense links on v1alpha
// (analyticsadmin, revision 20261116). The link is not read before it is deleted.
const (
	adsLinksDeleteID      = Provider + ".google-ads-links.delete"
	firebaseLinksDeleteID = Provider + ".firebase-links.delete"
	bigQueryLinksDeleteID = Provider + ".bigquery-links.delete"
	adsenseLinksDeleteID  = Provider + ".adsense-links.delete"
)

const (
	notFoundAdsLink      = subject + " did not find the Google Ads link, or the credential may not see it"
	notFoundFirebaseLink = subject + " did not find the Firebase link, or the credential may not see it"

	linkDeleteNote = " Needs the confirmation of a person for each call. The link is not read before it is " +
		"deleted. Only a person can link the property again, outside Qatlas: creating a link is not a tool. On a " +
		"connection with targets the property of the link must be a bound property or belong to a bound " +
		"account, which is proven by one read of its parent before the change. " + streamDeleteNote

	linkDeleteInput = `{"type":"object","properties":{"name":` + linkNameSchema +
		`},"required":["name"],"additionalProperties":false}`
	linkDeleteOutput = `{"type":"object","properties":{"name":{"type":"string"},"deleted":{"type":"boolean"}},` +
		`"additionalProperties":false}`
)

// linkDelete is what the four delete tools differ in.
type linkDelete struct {
	id, op, segment, noun, notFound string
	valid                           func(string) bool
	alpha                           bool
}

var (
	adsLinkDelete = linkDelete{id: adsLinksDeleteID, op: "delete Google Ads link", segment: adsLinksSegment,
		noun: "Google Ads", notFound: notFoundAdsLink, valid: validID}
	firebaseLinkDelete = linkDelete{id: firebaseLinksDeleteID, op: "delete Firebase link", segment: firebaseLinksSegment,
		noun: "Firebase", notFound: notFoundFirebaseLink, valid: validID}
	bigQueryLinkDelete = linkDelete{id: bigQueryLinksDeleteID, op: "delete BigQuery link", segment: bigQuerySegment,
		noun: "BigQuery", notFound: notFoundBigQueryLink, valid: validLinkID, alpha: true}
	adsenseLinkDelete = linkDelete{id: adsenseLinksDeleteID, op: "delete AdSense link", segment: adsenseLinksSegment,
		noun: "AdSense", notFound: notFoundAdsenseLink, valid: validID, alpha: true}
)

// descriptor builds the descriptor of a delete tool from its group; effect is what ending the link stops.
func (l linkDelete) descriptor(title, group, method, effect, form, example string) capability.Descriptor {
	description := "Delete one " + l.noun + " link of a Google Analytics property (" + method + " of the Analytics " +
		"Admin API). " + effect + " Google offers no way to restore a deleted link and this provider offers no " +
		"way to create one." + linkDeleteNote
	tags := []string{"google", "analytics", group, "delete"}
	if l.alpha {
		description += alphaNote
		tags = append(tags, "alpha")
	}
	return capability.Descriptor{
		ID: l.id, Version: 1, Title: title, Description: description, Tags: tags, Provider: Provider,
		Risk:         writeRisk(capability.EffectDelete, capability.IdempotencyIdempotent),
		InputSchema:  json.RawMessage(linkDeleteInput),
		OutputSchema: json.RawMessage(linkDeleteOutput),
		Arguments: []capability.Argument{{Name: "name", Required: true, Description: l.noun + " link resource " +
			"name " + form + ", from " + group + ".list"}},
		Fields: []capability.Field{
			{Name: "name", Description: "The link name that was deleted"},
			{Name: "deleted", Description: "true once Google confirmed the deletion"},
		},
		Examples: []capability.Example{{Description: "Delete one " + l.noun + " link",
			Arguments: json.RawMessage(`{"name":"` + example + `"}`)}},
	}
}

var (
	adsLinksDelete = adsLinkDelete.descriptor("Delete an Analytics Google Ads link", "google-ads-links",
		"properties.googleAdsLinks.delete",
		"The data flow of the link ends: the property no longer shares its audiences and conversions with the "+
			"Google Ads customer over it.",
		"properties/ID/googleAdsLinks/ID", "properties/200/googleAdsLinks/11")
	firebaseLinksDelete = firebaseLinkDelete.descriptor("Delete an Analytics Firebase link", "firebase-links",
		"properties.firebaseLinks.delete",
		"The data flow between the property and the Firebase project over this link ends.",
		"properties/ID/firebaseLinks/ID", "properties/200/firebaseLinks/21")
	bigQueryLinksDelete = bigQueryLinkDelete.descriptor("Delete an Analytics BigQuery link", "bigquery-links",
		"properties.bigQueryLinks.delete",
		"The export to BigQuery ends. Data already exported stays in the Google Cloud project.",
		"properties/ID/bigQueryLinks/ID", "properties/200/bigQueryLinks/abc567")
	adsenseLinksDelete = adsenseLinkDelete.descriptor("Delete an Analytics AdSense link", "adsense-links",
		"properties.adSenseLinks.delete",
		"The data flow between the property and the AdSense ad client over this link ends.",
		"properties/ID/adSenseLinks/ID", "properties/200/adSenseLinks/300")
)

// property checks the name of a link and returns its property. The refusal names the argument, never the value.
func (l linkDelete) property(value string) (string, error) {
	property, ok := splitLinkName(value, l.segment, l.valid)
	if !ok {
		return "", badRequest("name must be properties/ID" + l.segment + "/ID with a numeric property ID")
	}
	return property, nil
}

// handler returns the handler of the tool: one request without a retry, after the binding is checked.
func (l linkDelete) handler() capability.Handler {
	return func(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
		raw json.RawMessage) (any, error) {
		var arguments linkNameArguments
		if err := json.Unmarshal(raw, &arguments); err != nil {
			return nil, failure(l.op, "the validated arguments could not be read")
		}
		bind, err := bindingOf(resolved)
		if err != nil {
			return nil, err
		}
		property, err := l.property(arguments.Name)
		if err != nil {
			return nil, err
		}
		proof, err := bind.property(property)
		if err != nil {
			return nil, err
		}
		c, err := open(ctx, resolved, secrets, red)
		if err != nil {
			return nil, err
		}
		if err := c.proveProperty(ctx, l.op, l.id, property, proof); err != nil {
			return nil, err
		}
		path := "/v1beta/" + arguments.Name
		if l.alpha {
			path = alphaPath(arguments.Name)
		}
		if err := c.change(ctx, changeCall{op: l.op, tool: l.id, method: http.MethodDelete, path: path,
			notFound: l.notFound}, nil); err != nil {
			return nil, err
		}
		return deleteTargetResult{Name: arguments.Name, Deleted: true}, nil
	}
}

// summary phrases what a confirmed call would do from its arguments alone. The arguments are untrusted data;
// the core cuts and neutralizes what this returns.
func (l linkDelete) summary(raw json.RawMessage) string {
	var a linkNameArguments
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	return "Delete Analytics " + l.noun + " link " + a.Name + "; its data flow ends and only a person can link " +
		"the property again outside Qatlas"
}
