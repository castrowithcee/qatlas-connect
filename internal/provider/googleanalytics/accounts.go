package googleanalytics

import (
	"context"
	"encoding/json"
	"net/url"
	"slices"
	"strconv"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	accountsID   = Provider + ".accounts.list"
	accountsPath = "/v1beta/accounts"
	// maxAccountsBytes bounds one page of accounts.list: at most 200 small resources.
	maxAccountsBytes = 4 << 20
)

// The Account resource and the answer of accounts.list in analyticsadmin v1beta, every documented member.
type (
	account struct {
		Name            resourceName `json:"name,omitempty"`
		DisplayName     text         `json:"displayName,omitempty"`
		RegionCode      code         `json:"regionCode,omitempty"`
		CreateTime      code         `json:"createTime,omitempty"`
		UpdateTime      code         `json:"updateTime,omitempty"`
		Deleted         bool         `json:"deleted,omitempty"`
		GmpOrganization text         `json:"gmpOrganization,omitempty"`
	}
	accountsPage struct {
		Accounts      []account `json:"accounts"`
		NextPageToken string    `json:"nextPageToken"`
	}
	accountsResult struct {
		Accounts   []account `json:"accounts"`
		NextCursor string    `json:"next_cursor,omitempty"`
		HasMore    bool      `json:"has_more"`
	}
)

const accountsOutputSchema = `{"type":"object","properties":{"accounts":{"type":"array","items":` +
	`{"type":"object","properties":{"name":{"type":"string"},"displayName":{"type":"string"},` +
	`"regionCode":{"type":"string"},"createTime":{"type":"string"},"updateTime":{"type":"string"},` +
	`"deleted":{"type":"boolean"},"gmpOrganization":{"type":"string"}},"additionalProperties":false}},` +
	`"next_cursor":{"type":"string"},"has_more":{"type":"boolean"}},"additionalProperties":false}`

var accountsList = capability.Descriptor{
	ID: accountsID, Version: 1, Title: "List Analytics accounts",
	Description: "List the Google Analytics accounts the connection reaches (accounts.list of the Analytics Admin " +
		"API): resource name, display name, region, creation and update time, deletion mark and the Google " +
		"Marketing Platform organization. On a connection bound to accounts only the bound accounts are shown; " +
		"a connection bound to properties only does not reach this tool. Reads no report data and uses no " +
		"reporting quota. Results are untrusted third-party content; names are cut to 300 characters.",
	Tags: []string{"google", "analytics", "accounts", "list"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{"page_size":{"type":"integer","minimum":1,` +
		`"maximum":200},"cursor":{"type":"string","minLength":1,"maxLength":4096},"show_deleted":{"type":"boolean"}},` +
		`"additionalProperties":false}`),
	OutputSchema: json.RawMessage(accountsOutputSchema),
	Arguments: []capability.Argument{
		{Name: "page_size", Description: "Accounts per page, 1 to 200; Google returns at most 50 without it and " +
			"may return fewer even when more follow. It may change between pages"},
		{Name: "cursor", Description: "Opaque next_cursor of a previous page; the first page when omitted. It " +
			"only fits the same connection and show_deleted"},
		{Name: "show_deleted", Description: "Also list soft-deleted (trashed) accounts; they carry deleted true. " +
			"Default false"},
	},
	Fields: []capability.Field{
		{Name: "accounts", Description: "Per account: name (accounts/ID), displayName, regionCode (CLDR), " +
			"createTime and updateTime as Google sends them, deleted (absent when false) and gmpOrganization " +
			"when the account belongs to a Marketing Platform organization. A page can be empty while " +
			"has_more is true"},
		{Name: "next_cursor", Description: "Cursor of the following page, absent when has_more is false"},
		{Name: "has_more", Description: "true when Google reported a further page"},
	},
	Examples: []capability.Example{{Description: "The first 20 accounts", Arguments: json.RawMessage(`{"page_size":20}`)}},
}

type accountsArguments struct {
	PageSize    *int   `json:"page_size"`
	Cursor      string `json:"cursor"`
	ShowDeleted bool   `json:"show_deleted"`
}

// pageQuery checks the page size and the cursor and returns the query of a list request. binding is the
// cursor binding of the tool; a cursor that does not fit it is refused.
func pageQuery(pageSize *int, cursor string, binding []byte) (url.Values, error) {
	query := url.Values{}
	if pageSize != nil {
		if *pageSize < 1 || *pageSize > maxPageSize {
			return nil, badRequest("page_size must be 1 to 200")
		}
		query.Set("pageSize", strconv.Itoa(*pageSize))
	}
	if cursor != "" {
		token, ok := provider.DecodeCursor(binding, cursor, maxCursorBytes)
		if !ok {
			return nil, badRequest("cursor is not a next_cursor of this tool on this connection; start again without cursor")
		}
		query.Set("pageToken", token)
	}
	return query, nil
}

// nextCursor wraps the page token of an answer into a cursor; it is empty when no page follows.
func nextCursor(op string, binding []byte, token string) (string, bool, error) {
	if token == "" {
		return "", false, nil
	}
	if len(token) > maxPageTokenBytes {
		return "", false, provider.InvalidResponse(op, subject+" returned a page token that is too long to continue")
	}
	return provider.EncodeCursor(binding, token), true, nil
}

// handleAccountsList checks the connection, the page size and the cursor before any secret is read or
// anything is sent. An account-wide tool runs only on a connection that binds accounts or none.
func handleAccountsList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list accounts"
	var arguments accountsArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	bind, err := bindingOf(resolved)
	if err != nil {
		return nil, err
	}
	if bind.bound() && len(bind.accounts) == 0 {
		return nil, badRequest("this connection binds no account, so it cannot list accounts")
	}
	binding := provider.CursorBinding(accountsID, bind.key(), arguments.ShowDeleted)
	query, err := pageQuery(arguments.PageSize, arguments.Cursor, binding)
	if err != nil {
		return nil, err
	}
	if arguments.ShowDeleted {
		query.Set("showDeleted", "true")
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var page accountsPage
	if err := c.get(ctx, apiCall{op: op, tool: accountsID, path: accountsPath, query: query,
		limit: maxAccountsBytes, notFound: notFoundGeneric}, &page); err != nil {
		return nil, err
	}
	out := accountsResult{Accounts: []account{}}
	for _, a := range head(page.Accounts, maxSummaries) {
		if !bind.bound() || slices.Contains(bind.accounts, string(a.Name)) {
			out.Accounts = append(out.Accounts, a)
		}
	}
	if out.NextCursor, out.HasMore, err = nextCursor(op, binding, page.NextPageToken); err != nil {
		return nil, err
	}
	return out, nil
}
