package googlesearchconsole

import (
	"context"
	"encoding/json"
	"net/url"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const sitemapAddressSchema = `{"type":"string","minLength":10,"maxLength":2048}`

var sitemapFields = []capability.Field{
	{Name: "path", Description: "address of the sitemap"},
	{Name: "type", Description: "kind of sitemap as Google names it, such as sitemap or rssFeed"},
	{Name: "lastSubmitted", Description: "when the sitemap was submitted, RFC 3339"},
	{Name: "lastDownloaded", Description: "when Google last downloaded the sitemap, RFC 3339"},
	{Name: "isPending", Description: "true while Google has not processed the sitemap"},
	{Name: "isSitemapsIndex", Description: "true for a sitemap index, a collection of sitemaps"},
	{Name: "warnings", Description: "number of warnings, as a decimal string"},
	{Name: "errors", Description: "number of errors in the sitemap itself, as a decimal string"},
	{Name: "contents", Description: "per content type (such as web, image, video) the number of submitted " +
		"URLs as a decimal string, at most 20 entries"},
}

var sitemapsList = capability.Descriptor{
	ID: sitemapsListID, Version: 1, Title: "List the sitemaps of a property",
	Description: "List the sitemaps submitted for one Search Console property, or those listed in one of its " +
		"sitemap indexes, each with its processing state, warning and error counts, and submitted URLs per " +
		"content type. Google returns the whole list in one answer; an empty list means no sitemap; more than 500 are cut and " +
		"marked truncated. " + untrustedNote,
	Tags: []string{"google", "search-console", "sitemaps"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + siteURLSchema +
		`,"sitemap_index":` + sitemapAddressSchema + `},"additionalProperties":false}`),
	OutputSchema: json.RawMessage(sitemapsSchema),
	Arguments: []capability.Argument{siteArgument,
		{Name: "sitemap_index", Description: "Absolute http or https address of a sitemap index of the property, " +
			"such as https://www.example.com/sitemap_index.xml; lists the sitemaps it contains instead of those " +
			"submitted for the property"}},
	Fields: sitemapFields,
	Examples: []capability.Example{{Description: "Sitemaps of a URL-prefix property",
		Arguments: json.RawMessage(`{"site_url":"https://www.example.com/"}`)}},
}

var sitemapsGet = capability.Descriptor{
	ID: sitemapsGetID, Version: 1, Title: "Get a sitemap of a property",
	Description: "Read one sitemap of a Search Console property: its processing state, warning and error counts, " +
		"and submitted URLs per content type. " + untrustedNote,
	Tags: []string{"google", "search-console", "sitemaps"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + siteURLSchema +
		`,"feedpath":` + sitemapAddressSchema + `},"required":["feedpath"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(sitemapSchema),
	Arguments: []capability.Argument{siteArgument,
		{Name: "feedpath", Required: true, Description: "Absolute http or https address of the sitemap, such as " +
			"https://www.example.com/sitemap.xml, as it was submitted"}},
	Fields: sitemapFields,
	Examples: []capability.Example{{Description: "One sitemap of a URL-prefix property",
		Arguments: json.RawMessage(`{"site_url":"https://www.example.com/","feedpath":"https://www.example.com/sitemap.xml"}`)}},
}

type sitemapArguments struct {
	SiteURL      string `json:"site_url"`
	SitemapIndex string `json:"sitemap_index"`
	Feedpath     string `json:"feedpath"`
}

func handleSitemapsList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage) (any, error) {
	const op = "sitemaps list"
	var arguments sitemapArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	siteURL, err := siteOf(resolved, arguments.SiteURL)
	if err != nil {
		return nil, err
	}
	var query url.Values
	if arguments.SitemapIndex != "" {
		if err := checkSitemap("sitemap_index", arguments.SitemapIndex); err != nil {
			return nil, err
		}
		query = url.Values{"sitemapIndex": {arguments.SitemapIndex}}
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var answer sitemapsAnswer
	if err := c.get(ctx, op, sitemapsListID, sitemapsPath(siteURL), query, notFoundSite, &answer); err != nil {
		return nil, err
	}
	result := sitemapsResult{Sitemap: []sitemap{}}
	for _, entry := range answer.Sitemap {
		if len(result.Sitemap) == maxSitemaps {
			result.Truncated = true
			break
		}
		result.Sitemap = append(result.Sitemap, entry.bound())
	}
	return result, nil
}

func handleSitemapsGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage) (any, error) {
	const op = "sitemaps get"
	var arguments sitemapArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	siteURL, err := siteOf(resolved, arguments.SiteURL)
	if err != nil {
		return nil, err
	}
	if err := checkSitemap("feedpath", arguments.Feedpath); err != nil {
		return nil, err
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var entry sitemap
	path := sitemapsPath(siteURL) + "/" + segment(arguments.Feedpath)
	if err := c.get(ctx, op, sitemapsGetID, path, nil, notFoundSitemap, &entry); err != nil {
		return nil, err
	}
	return entry.bound(), nil
}
