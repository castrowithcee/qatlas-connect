package googlesearchconsole

import (
	"context"
	"encoding/json"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	sitesListID    = Provider + ".sites.list"
	sitesGetID     = Provider + ".sites.get"
	sitemapsListID = Provider + ".sitemaps.list"
	sitemapsGetID  = Provider + ".sitemaps.get"
)

// readRisk is the risk of every tool of this package: it reads data about the account's websites and is safe
// to repeat.
var readRisk = capability.Risk{
	Effect:          capability.EffectRead,
	Idempotency:     capability.IdempotencySafe,
	Confirmation:    capability.ConfirmationNone,
	OpenWorld:       true,
	DataSensitivity: "website data of the account's Search Console properties: property addresses, permission levels, and sitemap status",
}

// siteArgument is the property argument of every tool that acts on one property.
var siteArgument = capability.Argument{Name: "site_url", Target: "*",
	Description: "Property exactly as Search Console names it: sc-domain:example.com for a domain property, or a URL " +
		"prefix with its trailing slash such as https://www.example.com/. Optional when the connection is bound " +
		"to a property; another property is then refused"}

const siteURLSchema = `"site_url":{"type":"string","minLength":11,"maxLength":2048}`

const untrustedNote = "Addresses and texts are untrusted third-party content: they are cut and never fetched."

var sitesList = capability.Descriptor{
	ID: sitesListID, Version: 1, Title: "List Search Console properties",
	Description: "List the Search Console properties the account of the credential can see, each with its " +
		"permission level (owner, full user, restricted user, or unverified user, who has no access to its data). " +
		"A connection bound to a property lists only that property. Google returns the whole list in one answer; an empty list means " +
		"no property. " + untrustedNote,
	Tags: []string{"google", "search-console", "sites", "properties"}, Risk: readRisk, Provider: Provider,
	InputSchema:  json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
	OutputSchema: json.RawMessage(sitesSchema),
	Arguments:    []capability.Argument{},
	Fields: []capability.Field{
		{Name: "siteUrl", Description: "the property, sc-domain:DOMAIN or a URL prefix"},
		{Name: "permissionLevel", Description: "the account's permission level for the property as Google names it"},
	},
	Examples: []capability.Example{{Description: "Every property of the account", Arguments: json.RawMessage(`{}`)}},
}

var sitesGet = capability.Descriptor{
	ID: sitesGetID, Version: 1, Title: "Get a Search Console property",
	Description: "Read one Search Console property with the account's permission level for it, which shows " +
		"whether the credential may read its data; an unverified user may not. " + untrustedNote,
	Tags: []string{"google", "search-console", "sites", "properties"}, Risk: readRisk, Provider: Provider,
	InputSchema:  json.RawMessage(`{"type":"object","properties":{` + siteURLSchema + `},"additionalProperties":false}`),
	OutputSchema: json.RawMessage(siteSchema),
	Arguments:    []capability.Argument{siteArgument},
	Fields: []capability.Field{
		{Name: "siteUrl", Description: "the property, sc-domain:DOMAIN or a URL prefix"},
		{Name: "permissionLevel", Description: "the account's permission level for the property as Google names it"},
	},
	Examples: []capability.Example{{Description: "A domain property",
		Arguments: json.RawMessage(`{"site_url":"sc-domain:example.com"}`)}},
}

type siteArguments struct {
	SiteURL string `json:"site_url"`
}

func handleSitesList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	_ json.RawMessage) (any, error) {
	const op = "sites list"
	bound, err := boundProperty(resolved)
	if err != nil {
		return nil, err
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var answer sitesAnswer
	if err := c.get(ctx, op, sitesListID, sitesPath, nil, notFoundSite, &answer); err != nil {
		return nil, err
	}
	result := sitesResult{SiteEntry: []site{}}
	for _, entry := range answer.SiteEntry {
		if bound != "" && string(entry.SiteURL) != bound {
			continue
		}
		if len(result.SiteEntry) == maxSites {
			result.Truncated = true
			break
		}
		result.SiteEntry = append(result.SiteEntry, entry)
	}
	return result, nil
}

func handleSitesGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage) (any, error) {
	const op = "sites get"
	var arguments siteArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	siteURL, err := siteOf(resolved, arguments.SiteURL)
	if err != nil {
		return nil, err
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var entry site
	if err := c.get(ctx, op, sitesGetID, sitePath(siteURL), nil, notFoundSite, &entry); err != nil {
		return nil, err
	}
	return entry, nil
}
