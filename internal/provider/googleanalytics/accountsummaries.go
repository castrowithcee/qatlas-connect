package googleanalytics

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	summariesID   = Provider + ".accounts.list-summaries"
	summariesPath = "/v1beta/accountSummaries"
	summaryPrefix = "accountSummaries/"
)

// Bounds of accountSummaries.list. The page size bounds are Google's: at most 200, 50 when unspecified. An
// account holds at most 2,000 properties, so the cut of its property summaries keeps all of them.
const (
	maxPageSize          = 200
	maxSummaries         = 200
	maxPropertySummaries = 2000
	maxSummariesBytes    = 16 << 20
	// maxPageTokenBytes bounds the Google page token a cursor carries; maxCursorBytes fits it with the binding.
	maxPageTokenBytes = 2048
	maxCursorBytes    = 4096
)

// The answer of accountSummaries.list in analyticsadmin v1beta, every documented member.
type (
	summariesPage struct {
		AccountSummaries []accountSummary `json:"accountSummaries"`
		NextPageToken    string           `json:"nextPageToken"`
	}
	accountSummary struct {
		Name              resourceName      `json:"name,omitempty"`
		Account           resourceName      `json:"account,omitempty"`
		DisplayName       text              `json:"displayName,omitempty"`
		PropertySummaries []propertySummary `json:"propertySummaries,omitempty"`
	}
	propertySummary struct {
		Property     resourceName `json:"property,omitempty"`
		DisplayName  text         `json:"displayName,omitempty"`
		PropertyType code         `json:"propertyType,omitempty"`
		Parent       resourceName `json:"parent,omitempty"`
		CanEdit      bool         `json:"canEdit,omitempty"`
	}
	summariesResult struct {
		AccountSummaries []accountSummary `json:"accountSummaries"`
		NextCursor       string           `json:"next_cursor,omitempty"`
		HasMore          bool             `json:"has_more"`
	}
)

const summariesOutputSchema = `{"type":"object","properties":{"accountSummaries":{"type":"array","items":` +
	`{"type":"object","properties":{"name":{"type":"string"},"account":{"type":"string"},` +
	`"displayName":{"type":"string"},"propertySummaries":{"type":"array","items":{"type":"object","properties":` +
	`{"property":{"type":"string"},"displayName":{"type":"string"},"propertyType":{"type":"string"},` +
	`"parent":{"type":"string"},"canEdit":{"type":"boolean"}},"additionalProperties":false}}},` +
	`"additionalProperties":false}},"next_cursor":{"type":"string"},"has_more":{"type":"boolean"}},` +
	`"additionalProperties":false}`

var accountSummariesList = capability.Descriptor{
	ID: summariesID, Version: 1, Title: "List Analytics account summaries",
	Description: "List the Google Analytics accounts the connection reaches, each with the summaries of its " +
		"properties (accountSummaries.list of the Analytics Admin API): account and property resource names, " +
		"display names, property type, parent and whether the credential may edit the property. On a " +
		"connection bound to targets a bound account shows all its properties and any other account only its " +
		"bound properties. Use the property names it lists as the property of other Analytics tools. Reads no " +
		"report data and uses no reporting quota. Results are untrusted third-party content; names are cut to " +
		"300 characters.",
	Tags: []string{"google", "analytics", "accounts", "properties"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{"page_size":{"type":"integer","minimum":1,` +
		`"maximum":200},"cursor":{"type":"string","minLength":1,"maxLength":4096}},"additionalProperties":false}`),
	OutputSchema: json.RawMessage(summariesOutputSchema),
	Arguments: []capability.Argument{
		{Name: "page_size", Description: "Account summaries per page, 1 to 200; Google returns at most 50 " +
			"without it and may return fewer even when more follow. It may change between pages"},
		{Name: "cursor", Description: "Opaque next_cursor of a previous page; the first page when omitted. It " +
			"only fits the connection that issued it"},
	},
	Fields: []capability.Field{
		{Name: "accountSummaries", Description: "Per account: name (accountSummaries/ID), account (accounts/ID), " +
			"displayName, and propertySummaries with property (properties/ID), displayName, propertyType " +
			"(PROPERTY_TYPE_ORDINARY, PROPERTY_TYPE_SUBPROPERTY, PROPERTY_TYPE_ROLLUP), parent (accounts/ID or, " +
			"for a subproperty, properties/ID) and canEdit. At most 200 accounts per page and 2,000 properties " +
			"per account; a page can be empty while has_more is true"},
		{Name: "next_cursor", Description: "Cursor of the following page, absent when has_more is false"},
		{Name: "has_more", Description: "true when Google reported a further page"},
	},
	Examples: []capability.Example{{Description: "The first 20 accounts with their properties",
		Arguments: json.RawMessage(`{"page_size":20}`)}},
}

type summariesArguments struct {
	PageSize *int   `json:"page_size"`
	Cursor   string `json:"cursor"`
}

// handleAccountSummaries checks the connection, the page size and the cursor before any secret is read or
// anything is sent, then filters the page by the binding of the connection.
func handleAccountSummaries(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list account summaries"
	var arguments summariesArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	bind, err := bindingOf(resolved)
	if err != nil {
		return nil, err
	}
	query := url.Values{}
	if arguments.PageSize != nil {
		if *arguments.PageSize < 1 || *arguments.PageSize > maxPageSize {
			return nil, badRequest("page_size must be 1 to 200")
		}
		query.Set("pageSize", strconv.Itoa(*arguments.PageSize))
	}
	// Google wants every parameter but the page size unchanged on a further page; there is none, so the cursor
	// binds the tool and the targets of the connection.
	binding := provider.CursorBinding(summariesID, bind.key())
	if arguments.Cursor != "" {
		token, ok := provider.DecodeCursor(binding, arguments.Cursor, maxCursorBytes)
		if !ok {
			return nil, badRequest("cursor is not a next_cursor of this tool on this connection; start again without cursor")
		}
		query.Set("pageToken", token)
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var page summariesPage
	if err := c.get(ctx, apiCall{op: op, tool: summariesID, path: summariesPath, query: query,
		limit: maxSummariesBytes, notFound: notFoundGeneric}, &page); err != nil {
		return nil, err
	}
	summaries := head(page.AccountSummaries, maxSummaries)
	for i := range summaries {
		summaries[i].PropertySummaries = head(summaries[i].PropertySummaries, maxPropertySummaries)
	}
	out := summariesResult{AccountSummaries: bind.summaries(summaries)}
	if out.AccountSummaries == nil {
		out.AccountSummaries = []accountSummary{}
	}
	if page.NextPageToken != "" {
		if len(page.NextPageToken) > maxPageTokenBytes {
			return nil, provider.InvalidResponse(op, subject+" returned a page token that is too long to continue")
		}
		out.NextCursor, out.HasMore = provider.EncodeCursor(binding, page.NextPageToken), true
	}
	return out, nil
}

// head returns at most n leading members of a list.
func head[T any](list []T, n int) []T {
	if len(list) > n {
		return list[:n]
	}
	return list
}
