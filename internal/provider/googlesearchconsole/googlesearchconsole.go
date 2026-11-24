// Package googlesearchconsole implements the Google Search Console API (the properties an account can see,
// the sitemaps of a property, its search traffic, and the index state of a URL) and the Indexing API, which
// reads and sends the notifications of single URLs.
//
// The credential issues access tokens: a person's consent, a service account key, or the impersonation of a
// service account. The core token source obtains them from Google's fixed endpoints; this package only names
// the scope each tool needs and sends the token in the Authorization header, never in a URL. Both API origins
// are fixed; a configured base URL other than the default is refused before any secret is read.
//
// A connection may be bound to one property, either a domain property (sc-domain:example.com) or a URL
// prefix ending in a slash. A bound connection reaches that property only: another site_url is refused
// before anything is resolved or sent, and so is an Indexing API URL outside the property. Property URLs,
// sitemap paths, Indexing API URLs and every other value Google returns are untrusted third-party content:
// strings and lists are bounded and URLs are never fetched.
package googlesearchconsole

import (
	"context"
	"errors"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/provider/googleapi"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// Provider is the provider name used in the configuration and as the operation namespace.
const Provider = "google.search-console"

// scopeRead is the read-only scope of the Search Console API, the smallest scope every read method of the
// discovery document accepts.
const scopeRead = "https://www.googleapis.com/auth/webmasters.readonly"

// scopeEdit is the scope the changing methods of the discovery document need; the read scope does not
// suffice for them.
const scopeEdit = "https://www.googleapis.com/auth/webmasters"

// toolScopes maps every tool to the smallest scopes it needs.
var toolScopes = map[string][]string{
	sitesListID:       {scopeRead},
	sitesGetID:        {scopeRead},
	sitemapsListID:    {scopeRead},
	sitemapsGetID:     {scopeRead},
	searchAnalyticsID: {scopeRead},
	sitesAddID:        {scopeEdit},
	sitesDeleteID:     {scopeEdit},
	sitemapsSubmitID:  {scopeEdit},
	sitemapsDeleteID:  {scopeEdit},
	urlInspectionID:   {scopeRead},
	// The Indexing API names one scope for reading and publishing alike.
	indexingMetadataID:      {scopeIndexing},
	indexingMetadataBatchID: {scopeIndexing},
	indexingPublishID:       {scopeIndexing},
	indexingPublishBatchID:  {scopeIndexing},
}

const testOp = "test connection"

// TestConnection obtains an access token the way a call does, then lists the properties of the account and,
// for a bound connection, reads the bound property. It proves the credential, the scope, an enabled Search
// Console API in the Cloud project, and at least one property the account has access to; it proves nothing
// about other APIs. No property with access is not-found, and a bound property without access is
// permission.
func TestConnection(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor) (provider.Class, error) {
	if err := checkConnection(resolved, secrets); err != nil {
		return testClass(err)
	}
	bound, err := boundProperty(resolved)
	if err != nil {
		return testClass(err)
	}
	return provider.TestTokenConnection(ctx, resolved, secrets, red, tokenOptions(),
		func(ctx context.Context, source *provider.TokenSource) (provider.Class, error) {
			c := newClient(source, resolved)
			var list sitesAnswer
			if err := c.get(ctx, testOp, sitesListID, sitesPath, nil, notFoundSite, &list); err != nil {
				return "", err
			}
			if !anyAccess(list.SiteEntry) {
				return provider.ClassNotFound, nil
			}
			if bound == "" {
				return provider.ClassOK, nil
			}
			var entry site
			if err := c.get(ctx, testOp, sitesGetID, sitePath(bound), nil, notFoundSite, &entry); err != nil {
				return "", err
			}
			if !hasAccess(string(entry.PermissionLevel)) {
				return provider.ClassPermission, nil
			}
			return provider.ClassOK, nil
		})
}

// testClass turns a provider failure into the class of a connection test; any other error is returned.
func testClass(err error) (provider.Class, error) {
	var failed *provider.Error
	if errors.As(err, &failed) {
		return failed.Class, nil
	}
	return "", err
}

// Register adds the provider metadata, its consent endpoints, its connection test, and its tools.
func Register(reg *capability.Registry) error {
	operations := []capability.Operation{
		{Descriptor: sitesList, Handler: handleSitesList},
		{Descriptor: sitesGet, Handler: handleSitesGet},
		{Descriptor: sitemapsList, Handler: handleSitemapsList},
		{Descriptor: sitemapsGet, Handler: handleSitemapsGet},
		{Descriptor: searchAnalyticsQuery, Handler: handleSearchAnalytics},
		{Descriptor: sitesAdd, Handler: handleSitesAdd},
		{Descriptor: sitesDelete, Handler: handleSitesDelete},
		{Descriptor: sitemapsSubmit, Handler: handleSitemapsSubmit},
		{Descriptor: sitemapsDelete, Handler: handleSitemapsDelete},
		{Descriptor: urlInspectionInspect, Handler: handleURLInspection},
		{Descriptor: indexingMetadata, Handler: handleIndexingMetadata},
		{Descriptor: indexingMetadataBatch, Handler: handleIndexingMetadataBatch},
		{Descriptor: indexingPublish, Handler: handleIndexingPublish, ConfirmationSummary: publishSummary},
		{Descriptor: indexingPublishBatch, Handler: handleIndexingPublishBatch, ConfirmationSummary: publishBatchSummary},
	}
	if err := reg.RegisterProvider(config.ProviderMetadata{
		ID: Provider, Name: "Google Search Console", DefaultBaseURL: apiRoot,
		Description:    "Properties, sitemaps and search traffic of Google Search Console and its Indexing API notifications, used with an OAuth consent or a service account",
		DefaultEffects: []config.Effect{config.EffectRead},
		ValidateBaseURL: func(raw string) error {
			if !isDefaultRoot(raw) {
				return errors.New("a Google Search Console service has a fixed origin; leave the base URL as " + apiRoot)
			}
			return nil
		},
		SecretRoles: config.TokenSecretRoles(),
		Token:       &config.TokenMetadata{Scopes: toolScopes},
		Target: config.TargetMetadata{
			Label: "property",
			Description: "optional: one Search Console property, either sc-domain:DOMAIN or a URL prefix such as " +
				"https://www.example.com/ ending in a slash; without it the connection reaches every property " +
				"of the account",
			Validate: func(raw string) error { return checkProperty("the property", raw) },
		},
		Profiles: []config.ToolProfile{{
			ID: "read", Title: "Read", Recommended: true,
			Description: "lists the properties of the account, reads their sitemaps, queries their search traffic and inspects the index state of a URL; nothing is changed",
			Tools:       []string{sitesListID, sitesGetID, sitemapsListID, sitemapsGetID, searchAnalyticsID, urlInspectionID},
		}, {
			ID: "edit", Title: "Add properties and edit sitemaps",
			Description: "reads what the read profile reads and the Indexing API status of URLs, adds properties, " +
				"and submits and removes sitemaps in Search Console; every change needs its own confirmation, " +
				"and removing a property is listed-only",
			// The Indexing API reads join here and not the read profile: their only scope also allows
			// notifications, which a read-only consent should not grant.
			Tools: []string{sitesListID, sitesGetID, sitemapsListID, sitemapsGetID, searchAnalyticsID, urlInspectionID,
				sitesAddID, sitemapsSubmitID, sitemapsDeleteID, indexingMetadataID, indexingMetadataBatchID},
		}},
	}, TestConnection); err != nil {
		return err
	}
	if err := reg.RegisterOAuth(Provider, googleapi.OAuth); err != nil {
		return err
	}
	for _, op := range operations {
		if err := reg.Register(Provider, op); err != nil {
			return err
		}
	}
	return nil
}
