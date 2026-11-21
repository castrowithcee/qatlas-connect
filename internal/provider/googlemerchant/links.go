package googlemerchant

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"strconv"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// The two reads of this file show objects of other parties: Google Business Profile accounts linked to the
// account, and the Local Feeds Partnership providers Google offers in a country. No tool here creates a link.

const (
	gbpAccountsListID  = Provider + ".gbp-accounts.list"
	lfpProvidersFindID = Provider + ".lfp-providers.find"
	// linkPageSize is sent when page_size is left out, the default of Google for both lists.
	linkPageSize    = 50
	maxLinkPageSize = 1000
)

const (
	linkPageSizeSchema = `{"type":"integer","minimum":1,"maximum":1000}`

	linkNote = "Nothing here creates a link: linking a Google Business Profile account or a Local Feeds Partnership " +
		"provider gives another party access to the account and is not offered. " + servicesUntrusted
	gbpFieldsDescription = "name (accounts/ACCOUNT/gbpAccount/GBPACCOUNT), gbpAccountId, type (USER or " +
		"BUSINESS_ACCOUNT), listingCount, and gbpAccountName. gbpAccountName is the owner's email address for a " +
		"personal account and the business name for a business account; it is shown only on a connection without " +
		"targets, and a connection bound to targets shows the identifiers alone"
	lfpFieldsDescription = "name (accounts/ACCOUNT/omnichannelSettings/REGION/lfpProviders/PROVIDER), regionCode, " +
		"and displayName"
)

var (
	gbpAccountsList = capability.Descriptor{
		ID: gbpAccountsListID, Version: 1, Title: "List the Google Business Profile accounts of a Merchant Center account",
		Description: "List the Google Business Profile accounts linked to an account, a batch per call with a cursor to " +
			"read on. Google sends up to 1000 per batch; Qatlas asks for 50 when page_size is left out. Google " +
			"documents no filter. A Business Profile account is not an account of this connection, so on a connection " +
			"bound to targets its name is withheld, because for a personal account it is an email address of a person. " +
			"An account outside the targets of the connection is refused without a request; a sub-account of a bound " +
			"advanced account is proven by reading the sub-accounts first. " + linkNote,
		Tags: []string{"google", "merchant", "gbp-accounts"}, Risk: readRisk, Provider: Provider,
		InputSchema: inputSchema(nil, member("account", accountArgSchema), member("page_size", linkPageSizeSchema),
			member("cursor", cursorSchema)),
		OutputSchema: json.RawMessage(listResultSchema("gbpAccounts", gbpAccountSchema)),
		Arguments: []capability.Argument{accountArgument,
			{Name: "page_size", Description: "Accounts per batch, 1 to 1000; 50 when left out"}, cursorArgument},
		Fields: []capability.Field{
			{Name: "gbpAccounts", Description: "Business Profile accounts with " + gbpFieldsDescription},
			{Name: "next_cursor", Description: "Cursor to read on with; missing at the end"},
		},
		Examples: []capability.Example{{Description: "Business Profile accounts of one account",
			Arguments: json.RawMessage(`{"account":"1000001"}`)}},
	}
	lfpProvidersFind = capability.Descriptor{
		ID: lfpProvidersFindID, Version: 1, Title: "Find Local Feeds Partnership providers in a country",
		Description: "Find the Local Feeds Partnership (LFP) provider candidates Google offers for an account in one country " +
			"(accounts.omnichannelSettings.lfpProviders.find), a batch per call with a cursor to read on. Google sends up " +
			"to 1000 per batch; Qatlas asks for 50 when page_size is left out. The country is the region code of the " +
			"omnichannel setting. A candidate is not a link of the account. An account outside the targets of the " +
			"connection is refused without a request; a sub-account of a bound advanced account is proven by reading " +
			"the sub-accounts first. " + linkNote,
		Tags: []string{"google", "merchant", "lfp-providers"}, Risk: readRisk, Provider: Provider,
		InputSchema: inputSchema([]string{"region_code"}, member("account", accountArgSchema),
			member("region_code", regionCodeSchema), member("page_size", linkPageSizeSchema), member("cursor", cursorSchema)),
		OutputSchema: json.RawMessage(listResultSchema("lfpProviders", lfpProviderSchema)),
		Arguments: []capability.Argument{accountArgument,
			{Name: "region_code", Required: true, Description: "Region code of the country, two uppercase letters"},
			{Name: "page_size", Description: "Providers per batch, 1 to 1000; 50 when left out"}, cursorArgument},
		Fields: []capability.Field{
			{Name: "lfpProviders", Description: "Providers with " + lfpFieldsDescription},
			{Name: "next_cursor", Description: "Cursor to read on with; missing at the end"},
		},
		Examples: []capability.Example{{Description: "Providers in one country",
			Arguments: json.RawMessage(`{"account":"1000001","region_code":"CH"}`)}},
	}
)

func linkDescriptors() []capability.Descriptor {
	return []capability.Descriptor{gbpAccountsList, lfpProvidersFind}
}

func linkHandlers() map[string]capability.Handler {
	return map[string]capability.Handler{gbpAccountsListID: handleGbpAccountsList, lfpProvidersFindID: handleLfpProvidersFind}
}

var gbpCountForm = regexp.MustCompile(`^[0-9]{1,19}$`)

// gbpCount is an int64 member, which Google writes as a decimal string; a number is read as well, any other
// value is dropped.
type gbpCount string

func (c *gbpCount) UnmarshalJSON(b []byte) error {
	var text string
	if json.Unmarshal(b, &text) != nil {
		var number json.Number
		if json.Unmarshal(b, &number) != nil {
			*c = ""
			return nil
		}
		text = number.String()
	}
	if !gbpCountForm.MatchString(text) {
		text = ""
	}
	*c = gbpCount(text)
	return nil
}

// gbpAccount is the GbpAccount resource. gbpAccountName is a display value of another party, kept only where
// scope.showsGbpAccountName allows it.
type gbpAccount struct {
	Name           idText   `json:"name,omitempty"`
	GbpAccountID   idText   `json:"gbpAccountId,omitempty"`
	Type           idText   `json:"type,omitempty"`
	ListingCount   gbpCount `json:"listingCount,omitempty"`
	GbpAccountName nameText `json:"gbpAccountName,omitempty"`
}

// showsGbpAccountName reports whether the name of a Business Profile account may be shown. Such an account
// can never be named by a target, so on a bound connection it lies outside the binding and only its
// identifiers are shown; a connection without targets reaches everything the credential reaches.
func (s scope) showsGbpAccountName() bool { return !s.bound() }

type gbpAccountsAnswer struct {
	GbpAccounts   []gbpAccount `json:"gbpAccounts"`
	NextPageToken string       `json:"nextPageToken"`
}

type gbpAccountsResult struct {
	GbpAccounts []gbpAccount `json:"gbpAccounts"`
	NextCursor  string       `json:"next_cursor,omitempty"`
}

// lfpProvider is the LfpProvider resource of a candidate list. Its display name is the public name of the
// provider, not a value of a linked account.
type lfpProvider struct {
	Name        idText   `json:"name,omitempty"`
	RegionCode  idText   `json:"regionCode,omitempty"`
	DisplayName nameText `json:"displayName,omitempty"`
}

type lfpProvidersAnswer struct {
	LfpProviders  []lfpProvider `json:"lfpProviders"`
	NextPageToken string        `json:"nextPageToken"`
}

type lfpProvidersResult struct {
	LfpProviders []lfpProvider `json:"lfpProviders"`
	NextCursor   string        `json:"next_cursor,omitempty"`
}

var (
	gbpAccountSchema = objectSchema(member("name", stringSchema), member("gbpAccountId", stringSchema),
		member("type", stringSchema), member("listingCount", stringSchema), member("gbpAccountName", stringSchema))
	lfpProviderSchema = objectSchema(member("name", stringSchema), member("regionCode", stringSchema),
		member("displayName", stringSchema))
)

type linkArguments struct {
	Account    string `json:"account"`
	RegionCode string `json:"region_code"`
	PageSize   int    `json:"page_size"`
	Cursor     string `json:"cursor"`
}

// linkBind reads the arguments, refusing any member they do not name, and settles the connection, the
// account, the region, the page query, and the cursor binding. Nothing is requested and no secret is read.
func linkBind(resolved *config.Resolved, raw json.RawMessage, tool string, needRegion bool) (
	args linkArguments, bound scope, account string, needsProof bool, query url.Values, binding []byte, err error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&args) != nil {
		err = invalidArgument("the arguments hold a member or a value this tool does not take")
		return
	}
	if bound, err = checkConnection(resolved); err != nil {
		return
	}
	if account, needsProof, err = bound.accountOf(args.Account); err != nil {
		return
	}
	switch {
	case needRegion && !regionCodeForm.MatchString(args.RegionCode):
		err = invalidArgument("region_code must be a two-letter uppercase region code such as CH")
		return
	case !needRegion && args.RegionCode != "":
		err = invalidArgument("region_code is not an argument of this tool")
		return
	}
	binding = provider.CursorBinding(tool, account, args.RegionCode, args.PageSize, bound.key())
	pageSize := linkPageSize
	if args.PageSize > 0 {
		pageSize = min(args.PageSize, maxLinkPageSize)
	}
	query = url.Values{"pageSize": {strconv.Itoa(pageSize)}}
	if args.Cursor != "" {
		token, ok := provider.DecodeCursor(binding, args.Cursor, maxCursorSize)
		if !ok || token == "" {
			err = refusedCursor()
			return
		}
		query.Set("pageToken", token)
	}
	return
}

func handleGbpAccountsList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list gbp accounts"
	_, bound, account, needsProof, query, binding, err := linkBind(resolved, raw, gbpAccountsListID, false)
	if err != nil {
		return nil, err
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if needsProof {
		if err := cl.prove(ctx, op, gbpAccountsListID, account); err != nil {
			return nil, err
		}
	}
	var answer gbpAccountsAnswer
	if err := cl.get(ctx, op, gbpAccountsListID, accountPath(account)+"/gbpAccounts", query, &answer); err != nil {
		return nil, err
	}
	if len(answer.GbpAccounts) > maxEntries {
		return nil, tooMany(op)
	}
	out := gbpAccountsResult{GbpAccounts: make([]gbpAccount, 0, len(answer.GbpAccounts))}
	for _, g := range answer.GbpAccounts {
		if !bound.showsGbpAccountName() {
			g.GbpAccountName = ""
		}
		out.GbpAccounts = append(out.GbpAccounts, g)
	}
	if out.NextCursor, err = nextCursor(op, binding, answer.NextPageToken); err != nil {
		return nil, err
	}
	return out, nil
}

func handleLfpProvidersFind(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "find lfp providers"
	args, _, account, needsProof, query, binding, err := linkBind(resolved, raw, lfpProvidersFindID, true)
	if err != nil {
		return nil, err
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if needsProof {
		if err := cl.prove(ctx, op, lfpProvidersFindID, account); err != nil {
			return nil, err
		}
	}
	var answer lfpProvidersAnswer
	path := omnichannelSettingsPath(account) + "/" + args.RegionCode + "/lfpProviders:find"
	if err := cl.get(ctx, op, lfpProvidersFindID, path, query, &answer); err != nil {
		return nil, err
	}
	if len(answer.LfpProviders) > maxEntries {
		return nil, tooMany(op)
	}
	out := lfpProvidersResult{LfpProviders: append([]lfpProvider{}, answer.LfpProviders...)}
	if out.NextCursor, err = nextCursor(op, binding, answer.NextPageToken); err != nil {
		return nil, err
	}
	return out, nil
}
