package googlesearchconsole

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	sitesAddID       = Provider + ".sites.add"
	sitesDeleteID    = Provider + ".sites.delete"
	sitemapsSubmitID = Provider + ".sitemaps.submit"
	sitemapsDeleteID = Provider + ".sitemaps.delete"
)

// changeRisk is the contract of a change of the account's Search Console entries. Google answers a repeated
// add, submit, or delete with success or 404, so every change leaves the same state when it is repeated.
func changeRisk(effect capability.Effect) capability.Risk {
	return capability.Risk{
		Effect:          effect,
		Idempotency:     capability.IdempotencyIdempotent,
		Confirmation:    capability.ConfirmationRequired,
		OpenWorld:       true,
		DataSensitivity: "the account's Search Console entries: property addresses and sitemap addresses",
	}
}

// changeOutput is the answer of every change: Google returns nothing, so the result is an empty object.
const changeOutput = `{"type":"object","properties":{},"additionalProperties":false}`

// changed is the empty result of a change.
type changed struct{}

var feedpathArgument = capability.Argument{Name: "feedpath", Required: true,
	Description: "Absolute http or https address of the sitemap under the property, such as " +
		"https://www.example.com/sitemap.xml; it is checked against the property and never fetched"}

const feedpathSchema = `"feedpath":` + sitemapAddressSchema

var sitesAdd = capability.Descriptor{
	ID: sitesAddID, Version: 1, Title: "Add a Search Console property",
	Description: "Add a property to the Search Console account of the credential. It does not verify the " +
		"property and gives no access to its data: without a verification the account holds it as an " +
		"unverified user. Adding a property that is already there changes nothing. Needs confirmation.",
	Tags: []string{"google", "search-console", "sites", "properties"}, Risk: changeRisk(capability.EffectCreate),
	Provider:     Provider,
	InputSchema:  json.RawMessage(`{"type":"object","properties":{` + siteURLSchema + `},"additionalProperties":false}`),
	OutputSchema: json.RawMessage(changeOutput),
	Arguments:    []capability.Argument{siteArgument},
	Fields:       []capability.Field{},
	Examples: []capability.Example{{Description: "Add a domain property",
		Arguments: json.RawMessage(`{"site_url":"sc-domain:example.com"}`)}},
}

var sitesDelete = capability.Descriptor{
	ID: sitesDeleteID, Version: 1, Title: "Remove a Search Console property",
	Description: "Remove a property from the Search Console account of the credential. Only the entry in " +
		"Search Console goes, not the website. The account loses the history and permissions of the property. " +
		"Needs confirmation.",
	Tags: []string{"google", "search-console", "sites", "properties"}, Risk: changeRisk(capability.EffectDelete),
	Provider:     Provider,
	InputSchema:  json.RawMessage(`{"type":"object","properties":{` + siteURLSchema + `},"additionalProperties":false}`),
	OutputSchema: json.RawMessage(changeOutput),
	Arguments:    []capability.Argument{siteArgument},
	Fields:       []capability.Field{},
	Examples: []capability.Example{{Description: "Remove a URL-prefix property",
		Arguments: json.RawMessage(`{"site_url":"https://www.example.com/"}`)}},
}

var sitemapsSubmit = capability.Descriptor{
	ID: sitemapsSubmitID, Version: 1, Title: "Submit a sitemap of a property",
	Description: "Submit a sitemap address for a Search Console property, or renew its entry. Google reads the " +
		"sitemap afterwards; Qatlas does not fetch it. Needs confirmation.",
	Tags: []string{"google", "search-console", "sitemaps"}, Risk: changeRisk(capability.EffectCreate),
	Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + siteURLSchema + `,` + feedpathSchema +
		`},"required":["feedpath"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(changeOutput),
	Arguments:    []capability.Argument{siteArgument, feedpathArgument},
	Fields:       []capability.Field{},
	Examples: []capability.Example{{Description: "Submit a sitemap of a URL-prefix property",
		Arguments: json.RawMessage(`{"site_url":"https://www.example.com/","feedpath":"https://www.example.com/sitemap.xml"}`)}},
}

var sitemapsDelete = capability.Descriptor{
	ID: sitemapsDeleteID, Version: 1, Title: "Remove a sitemap of a property",
	Description: "Remove the entry of a sitemap from the Search Console report of a property. The file of the " +
		"website stays, and Google may keep reading the sitemap. Needs confirmation.",
	Tags: []string{"google", "search-console", "sitemaps"}, Risk: changeRisk(capability.EffectDelete),
	Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + siteURLSchema + `,` + feedpathSchema +
		`},"required":["feedpath"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(changeOutput),
	Arguments:    []capability.Argument{siteArgument, feedpathArgument},
	Fields:       []capability.Field{},
	Examples: []capability.Example{{Description: "Remove a sitemap entry",
		Arguments: json.RawMessage(`{"site_url":"https://www.example.com/","feedpath":"https://www.example.com/sitemap.xml"}`)}},
}

func handleSitesAdd(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage) (any, error) {
	return changeSite(ctx, resolved, secrets, red, raw, "sites add", sitesAddID, http.MethodPut)
}

func handleSitesDelete(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage) (any, error) {
	return changeSite(ctx, resolved, secrets, red, raw, "sites delete", sitesDeleteID, http.MethodDelete)
}

func handleSitemapsSubmit(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage) (any, error) {
	return changeSitemap(ctx, resolved, secrets, red, raw, "sitemaps submit", sitemapsSubmitID, http.MethodPut)
}

func handleSitemapsDelete(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage) (any, error) {
	return changeSitemap(ctx, resolved, secrets, red, raw, "sitemaps delete", sitemapsDeleteID, http.MethodDelete)
}

// changeSite settles the property first, so a refused argument or a property outside the binding never
// reaches a secret or a request.
func changeSite(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage, op, tool, method string) (any, error) {
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
	if err := c.change(ctx, op, tool, method, sitePath(siteURL), notFoundSite); err != nil {
		return nil, err
	}
	return changed{}, nil
}

func changeSitemap(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage, op, tool, method string) (any, error) {
	var arguments sitemapArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	siteURL, err := siteOf(resolved, arguments.SiteURL)
	if err != nil {
		return nil, err
	}
	if err := checkFeedpath(siteURL, arguments.Feedpath); err != nil {
		return nil, err
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	path := sitemapsPath(siteURL) + "/" + segment(arguments.Feedpath)
	if err := c.change(ctx, op, tool, method, path, notFoundSitemap); err != nil {
		return nil, err
	}
	return changed{}, nil
}
