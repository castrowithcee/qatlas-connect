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

// Search Ads 360 links are resources of the Admin API v1alpha (analyticsadmin, revision 20261116) below a
// property: properties/ID/searchAds360Links/ID. The link ID is not the advertiser ID. Only reading is offered.
const (
	searchAdsLinksListID = Provider + ".search-ads360-links.list"
	searchAdsLinksGetID  = Provider + ".search-ads360-links.get"

	searchAdsLinksSegment = "/searchAds360Links"
)

const notFoundSearchAdsLink = subject + " did not find the Search Ads 360 link, or the credential may not see it"

// searchAdsLinkName is properties/ID/searchAds360Links/ID; any other value is dropped.
type searchAdsLinkName string

func (n *searchAdsLinkName) UnmarshalJSON(b []byte) error {
	var value string
	if json.Unmarshal(b, &value) == nil {
		if _, ok := splitLinkName(value, searchAdsLinksSegment, validID); ok {
			*n = searchAdsLinkName(value)
		}
	}
	return nil
}

// checkSearchAdsLinkName checks an argument that names a Search Ads 360 link and returns its property. The
// refusal names the argument, never the value.
func checkSearchAdsLinkName(argument, value string) (string, error) {
	property, ok := splitLinkName(value, searchAdsLinksSegment, validID)
	if !ok {
		return "", badRequest(argument + " must be properties/ID/searchAds360Links/ID with numeric IDs")
	}
	return property, nil
}

// searchAdsLink is the SearchAds360Link resource of analyticsadmin v1alpha, every documented member.
type searchAdsLink struct {
	Name                       searchAdsLinkName `json:"name,omitempty"`
	AdvertiserID               code              `json:"advertiserId,omitempty"`
	AdvertiserDisplayName      text              `json:"advertiserDisplayName,omitempty"`
	CampaignDataSharingEnabled *bool             `json:"campaignDataSharingEnabled,omitempty"`
	CostDataSharingEnabled     *bool             `json:"costDataSharingEnabled,omitempty"`
	AdsPersonalizationEnabled  *bool             `json:"adsPersonalizationEnabled,omitempty"`
	SiteStatsSharingEnabled    *bool             `json:"siteStatsSharingEnabled,omitempty"`
}

const searchAdsLinkSchema = `{"type":"object","properties":{"name":{"type":"string"},"advertiserId":` +
	`{"type":"string"},"advertiserDisplayName":{"type":"string"},"campaignDataSharingEnabled":` +
	`{"type":"boolean"},"costDataSharingEnabled":{"type":"boolean"},"adsPersonalizationEnabled":` +
	`{"type":"boolean"},"siteStatsSharingEnabled":{"type":"boolean"}},"additionalProperties":false}`

const searchAdsLinkFieldsText = "name (properties/ID/searchAds360Links/ID, whose ID is not the advertiser ID), " +
	"advertiserId (the Search Ads 360 advertiser that is linked), advertiserDisplayName (cut to 300 " +
	"characters), campaignDataSharingEnabled (campaign data is imported from Search Ads 360), " +
	"costDataSharingEnabled (cost data is imported), adsPersonalizationEnabled (personalized advertising " +
	"features are on) and siteStatsSharingEnabled (site stats are exported). A member Google does not send is " +
	"left out, so an absent flag is not false"

type (
	searchAdsLinksPage struct {
		Links         []searchAdsLink `json:"searchAds360Links"`
		NextPageToken string          `json:"nextPageToken"`
	}
	searchAdsLinksResult struct {
		Links      []searchAdsLink `json:"searchAds360Links"`
		NextCursor string          `json:"next_cursor,omitempty"`
		HasMore    bool            `json:"has_more"`
	}
)

var searchAdsLinksList = capability.Descriptor{
	ID: searchAdsLinksListID, Version: 1, Title: "List Analytics Search Ads 360 links",
	Description: "List the Search Ads 360 links of a Google Analytics property " +
		"(properties.searchAds360Links.list of the Analytics Admin API) with name, advertiser ID and name, and the " +
		"settings for campaign data, cost data, ads personalization and site stats. On a connection with targets " +
		"the property must be a bound property or belong to a bound account, which is proven by one read of its " +
		"parent before the list. Reads no report data. Creating or changing a link is not a tool; a person does " +
		"that in the Analytics or Search Ads 360 interface." + advertiserLinkUntrusted + alphaNote,
	Tags: []string{"google", "analytics", "search-ads360-links", "list", "alpha"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{"property":` + propertyArgSchema + `,"page_size":` +
		`{"type":"integer","minimum":1,"maximum":200},"cursor":{"type":"string","minLength":1,"maxLength":4096}},` +
		`"required":["property"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"searchAds360Links":{"type":"array","items":` +
		searchAdsLinkSchema + `},"next_cursor":{"type":"string"},"has_more":{"type":"boolean"}},"additionalProperties":false}`),
	Arguments: []capability.Argument{
		{Name: "property", Required: true, Description: "Property resource name properties/ID, from " +
			"accounts.list-summaries or properties.list"},
		{Name: "page_size", Description: "Links per page, 1 to 200; Google returns at most 50 without it"},
		{Name: "cursor", Description: "Opaque next_cursor of a previous page; the first page when omitted. It " +
			"only fits the same connection and property"},
	},
	Fields: []capability.Field{
		{Name: "searchAds360Links", Description: "Per link: " + searchAdsLinkFieldsText + ". A page can be empty while has_more is true"},
		{Name: "next_cursor", Description: "Cursor of the following page, absent when has_more is false"},
		{Name: "has_more", Description: "true when Google reported a further page"},
	},
	Examples: []capability.Example{{Description: "The Search Ads 360 links of property 200",
		Arguments: json.RawMessage(`{"property":"properties/200"}`)}},
}

var searchAdsLinksGet = capability.Descriptor{
	ID: searchAdsLinksGetID, Version: 1, Title: "Get an Analytics Search Ads 360 link",
	Description: "Read one Search Ads 360 link of a Google Analytics property " +
		"(properties.searchAds360Links.get of the Analytics Admin API) with name, advertiser ID and name, and the " +
		"settings for campaign data, cost data, ads personalization and site stats. On a connection with targets " +
		"the property of the link must be a bound property or belong to a bound account, which is proven by one " +
		"read of its parent. Reads no report data." + advertiserLinkUntrusted + alphaNote,
	Tags: []string{"google", "analytics", "search-ads360-links", "get", "alpha"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{"name":` + linkNameSchema +
		`},"required":["name"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(searchAdsLinkSchema),
	Arguments: []capability.Argument{{Name: "name", Required: true, Description: "Search Ads 360 link " +
		"resource name properties/ID/searchAds360Links/ID, from search-ads360-links.list"}},
	Fields: []capability.Field{{Name: "(result)", Description: "The link: " + searchAdsLinkFieldsText}},
	Examples: []capability.Example{{Description: "Read one Search Ads 360 link",
		Arguments: json.RawMessage(`{"name":"properties/200/searchAds360Links/300"}`)}},
}

func handleSearchAdsLinksList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list Search Ads 360 links"
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
	binding := provider.CursorBinding(searchAdsLinksListID, bind.key(), name)
	query, err := pageQuery(arguments.PageSize, arguments.Cursor, binding)
	if err != nil {
		return nil, err
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := c.proveProperty(ctx, op, searchAdsLinksListID, name, proof); err != nil {
		return nil, err
	}
	var page searchAdsLinksPage
	if err := c.get(ctx, apiCall{op: op, tool: searchAdsLinksListID, path: alphaPath(name + searchAdsLinksSegment),
		query: query, limit: maxLinkBytes, notFound: notFoundProperty}, &page); err != nil {
		return nil, err
	}
	out := searchAdsLinksResult{Links: []searchAdsLink{}}
	// Only links of the asked property are shown; an entry without a valid name of it is dropped.
	for _, link := range head(page.Links, maxSummaries) {
		if owner, ok := splitLinkName(string(link.Name), searchAdsLinksSegment, validID); ok && owner == name {
			out.Links = append(out.Links, link)
		}
	}
	if out.NextCursor, out.HasMore, err = nextCursor(op, binding, page.NextPageToken); err != nil {
		return nil, err
	}
	return out, nil
}

func handleSearchAdsLinksGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "get Search Ads 360 link"
	var arguments linkNameArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	bind, err := bindingOf(resolved)
	if err != nil {
		return nil, err
	}
	property, err := checkSearchAdsLinkName("name", arguments.Name)
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
	if err := c.proveProperty(ctx, op, searchAdsLinksGetID, property, proof); err != nil {
		return nil, err
	}
	var out searchAdsLink
	if err := c.get(ctx, apiCall{op: op, tool: searchAdsLinksGetID, path: alphaPath(arguments.Name),
		limit: maxLinkBytes, notFound: notFoundSearchAdsLink}, &out); err != nil {
		return nil, err
	}
	if string(out.Name) != arguments.Name {
		return nil, provider.InvalidResponse(op, subject+" answered with another Search Ads 360 link than the one asked for")
	}
	return out, nil
}
