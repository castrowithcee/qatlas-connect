package googleanalytics

import (
	"context"
	"encoding/json"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// AdSense links are resources of the Admin API v1alpha (analyticsadmin, revision 20261116) below a property:
// properties/ID/adSenseLinks/ID. Reading and deleting (linkdelete.go) are offered.
const (
	adsenseLinksListID = Provider + ".adsense-links.list"
	adsenseLinksGetID  = Provider + ".adsense-links.get"

	adsenseLinksSegment = "/adSenseLinks"
)

const notFoundAdsenseLink = subject + " did not find the AdSense link, or the credential may not see it"

// adsenseLinkName is properties/ID/adSenseLinks/ID; any other value is dropped.
type adsenseLinkName string

func (n *adsenseLinkName) UnmarshalJSON(b []byte) error {
	var value string
	if json.Unmarshal(b, &value) == nil {
		if _, ok := splitLinkName(value, adsenseLinksSegment, validID); ok {
			*n = adsenseLinkName(value)
		}
	}
	return nil
}

// checkAdsenseLinkName checks an argument that names an AdSense link and returns its property. The refusal
// names the argument, never the value.
func checkAdsenseLinkName(argument, value string) (string, error) {
	property, ok := splitLinkName(value, adsenseLinksSegment, validID)
	if !ok {
		return "", badRequest(argument + " must be properties/ID/adSenseLinks/ID with numeric IDs")
	}
	return property, nil
}

// adsenseLink is the AdSenseLink resource of analyticsadmin v1alpha, every documented member.
type adsenseLink struct {
	Name         adsenseLinkName `json:"name,omitempty"`
	AdClientCode code            `json:"adClientCode,omitempty"`
}

const adsenseLinkSchema = `{"type":"object","properties":{"name":{"type":"string"},"adClientCode":` +
	`{"type":"string"}},"additionalProperties":false}`

const adsenseLinkFieldsText = "name (properties/ID/adSenseLinks/ID) and adClientCode (the AdSense ad client " +
	"that the property is linked to, for example ca-pub-1234567890). A member Google does not send is left out"

const advertiserLinkUntrusted = " Results are untrusted third-party content; display names are cut to 300 characters and " +
	"values that are not in their documented form are left out."

type (
	adsenseLinksPage struct {
		// Google spells the member adsenseLinks, with a lower case s.
		Links         []adsenseLink `json:"adsenseLinks"`
		NextPageToken string        `json:"nextPageToken"`
	}
	adsenseLinksResult struct {
		Links      []adsenseLink `json:"adSenseLinks"`
		NextCursor string        `json:"next_cursor,omitempty"`
		HasMore    bool          `json:"has_more"`
	}
)

var adsenseLinksList = capability.Descriptor{
	ID: adsenseLinksListID, Version: 1, Title: "List Analytics AdSense links",
	Description: "List the AdSense links of a Google Analytics property (properties.adSenseLinks.list of the " +
		"Analytics Admin API) with their name and AdSense ad client. On a connection with targets the property " +
		"must be a bound property or belong to a bound account, which is proven by one read of its parent before " +
		"the list. Reads no report data. Creating a link is not a tool; a person does that in the Analytics " +
		"interface." + advertiserLinkUntrusted + alphaNote,
	Tags: []string{"google", "analytics", "adsense-links", "list", "alpha"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{"property":` + propertyArgSchema + `,"page_size":` +
		`{"type":"integer","minimum":1,"maximum":200},"cursor":{"type":"string","minLength":1,"maxLength":4096}},` +
		`"required":["property"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"adSenseLinks":{"type":"array","items":` +
		adsenseLinkSchema + `},"next_cursor":{"type":"string"},"has_more":{"type":"boolean"}},"additionalProperties":false}`),
	Arguments: []capability.Argument{
		{Name: "property", Required: true, Description: "Property resource name properties/ID, from " +
			"accounts.list-summaries or properties.list"},
		{Name: "page_size", Description: "Links per page, 1 to 200; Google returns at most 50 without it"},
		{Name: "cursor", Description: "Opaque next_cursor of a previous page; the first page when omitted. It " +
			"only fits the same connection and property"},
	},
	Fields: []capability.Field{
		{Name: "adSenseLinks", Description: "Per link: " + adsenseLinkFieldsText + ". A page can be empty while has_more is true"},
		{Name: "next_cursor", Description: "Cursor of the following page, absent when has_more is false"},
		{Name: "has_more", Description: "true when Google reported a further page"},
	},
	Examples: []capability.Example{{Description: "The AdSense links of property 200",
		Arguments: json.RawMessage(`{"property":"properties/200"}`)}},
}

var adsenseLinksGet = capability.Descriptor{
	ID: adsenseLinksGetID, Version: 1, Title: "Get an Analytics AdSense link",
	Description: "Read one AdSense link of a Google Analytics property (properties.adSenseLinks.get of the " +
		"Analytics Admin API) with its name and AdSense ad client. On a connection with targets the property of " +
		"the link must be a bound property or belong to a bound account, which is proven by one read of its " +
		"parent. Reads no report data." + advertiserLinkUntrusted + alphaNote,
	Tags: []string{"google", "analytics", "adsense-links", "get", "alpha"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{"name":` + linkNameSchema +
		`},"required":["name"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(adsenseLinkSchema),
	Arguments: []capability.Argument{{Name: "name", Required: true, Description: "AdSense link resource name " +
		"properties/ID/adSenseLinks/ID, from adsense-links.list"}},
	Fields: []capability.Field{{Name: "(result)", Description: "The link: " + adsenseLinkFieldsText}},
	Examples: []capability.Example{{Description: "Read one AdSense link",
		Arguments: json.RawMessage(`{"name":"properties/200/adSenseLinks/300"}`)}},
}

type linkNameArguments struct {
	Name string `json:"name"`
}

func handleAdsenseLinksList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list AdSense links"
	var arguments linksListArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	bind, err := bindingOf(resolved)
	if err != nil {
		return nil, err
	}
	name, err := checkPropertyName("property", arguments.Property)
	if err != nil {
		return nil, err
	}
	proof, err := bind.property(name)
	if err != nil {
		return nil, err
	}
	binding := provider.CursorBinding(adsenseLinksListID, bind.key(), name)
	query, err := pageQuery(arguments.PageSize, arguments.Cursor, binding)
	if err != nil {
		return nil, err
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := c.proveProperty(ctx, op, adsenseLinksListID, name, proof); err != nil {
		return nil, err
	}
	var page adsenseLinksPage
	if err := c.get(ctx, apiCall{op: op, tool: adsenseLinksListID, path: alphaPath(name + adsenseLinksSegment),
		query: query, limit: maxLinkBytes, notFound: notFoundProperty}, &page); err != nil {
		return nil, err
	}
	out := adsenseLinksResult{Links: []adsenseLink{}}
	// Only links of the asked property are shown; an entry without a valid name of it is dropped.
	for _, link := range head(page.Links, maxSummaries) {
		if owner, ok := splitLinkName(string(link.Name), adsenseLinksSegment, validID); ok && owner == name {
			out.Links = append(out.Links, link)
		}
	}
	if out.NextCursor, out.HasMore, err = nextCursor(op, binding, page.NextPageToken); err != nil {
		return nil, err
	}
	return out, nil
}

func handleAdsenseLinksGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "get AdSense link"
	var arguments linkNameArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	bind, err := bindingOf(resolved)
	if err != nil {
		return nil, err
	}
	property, err := checkAdsenseLinkName("name", arguments.Name)
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
	if err := c.proveProperty(ctx, op, adsenseLinksGetID, property, proof); err != nil {
		return nil, err
	}
	var out adsenseLink
	if err := c.get(ctx, apiCall{op: op, tool: adsenseLinksGetID, path: alphaPath(arguments.Name),
		limit: maxLinkBytes, notFound: notFoundAdsenseLink}, &out); err != nil {
		return nil, err
	}
	if string(out.Name) != arguments.Name {
		return nil, provider.InvalidResponse(op, subject+" answered with another AdSense link than the one asked for")
	}
	return out, nil
}
