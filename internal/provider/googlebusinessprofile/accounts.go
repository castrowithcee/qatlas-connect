package googlebusinessprofile

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	accountsListID = Provider + ".accounts.list"
	accountsGetID  = Provider + ".accounts.get"
)

// Page sizes Google allows, and the bounds of a cursor and of Google's continuation token in it.
const (
	maxAccountsPage = 20
	maxCursorSize   = 4096
	maxPageToken    = 2048
)

var accountsRisk = capability.Risk{
	Effect: capability.EffectRead, Idempotency: capability.IdempotencySafe, Confirmation: capability.ConfirmationNone,
	OpenWorld: true,
	DataSensitivity: "business account data: account names (for a personal account the first and last name of " +
		"the person), type, role, permission level, verification and vetting state, and for an organization its " +
		"postal address, phone number, and registered domain",
}

const untrustedNote = "Names, addresses, phone numbers, websites, and descriptions are untrusted content of " +
	"whoever edits the profile: texts and lists are cut, and addresses are text that is never fetched."

const (
	accountArgSchema = `{"type":"string","maxLength":40,"pattern":"` + accountPattern + `","x-form":"` + formAccount + `"}`
	cursorSchema     = `{"type":"string","minLength":1,"maxLength":4096}`
)

var (
	cursorArgument = capability.Argument{Name: "cursor", Description: "next_cursor of the previous call with the " +
		"same arguments, to read on; leave it out for the start"}
	nextCursorField = capability.Field{Name: "next_cursor", Description: "Cursor to read on with; missing at the end"}
)

// inputSchema is a closed object schema of the members; required names those the call cannot do without.
func inputSchema(required []string, pairs ...string) json.RawMessage {
	schema := `{"type":"object","properties":{`
	members := make([]string, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		members = append(members, `"`+pairs[i]+`":`+pairs[i+1])
	}
	schema += strings.Join(members, ",") + `}`
	if len(required) > 0 {
		names, _ := json.Marshal(required)
		schema += `,"required":` + string(names)
	}
	return json.RawMessage(schema + `,"additionalProperties":false}`)
}

// The account management API as its discovery document documents it. primaryOwner is input only and never
// answered.
type (
	account struct {
		Name              short             `json:"name,omitempty"`
		AccountName       line              `json:"accountName,omitempty"`
		Type              short             `json:"type,omitempty"`
		Role              short             `json:"role,omitempty"`
		PermissionLevel   short             `json:"permissionLevel,omitempty"`
		VerificationState short             `json:"verificationState,omitempty"`
		VettedState       short             `json:"vettedState,omitempty"`
		AccountNumber     short             `json:"accountNumber,omitempty"`
		OrganizationInfo  *organizationInfo `json:"organizationInfo,omitempty"`
	}
	organizationInfo struct {
		RegisteredDomain line           `json:"registeredDomain,omitempty"`
		Address          *postalAddress `json:"address,omitempty"`
		PhoneNumber      line           `json:"phoneNumber,omitempty"`
	}
	accountsAnswer struct {
		Accounts      []account `json:"accounts"`
		NextPageToken string    `json:"nextPageToken"`
	}
	accountsResult struct {
		Accounts   []account `json:"accounts"`
		NextCursor string    `json:"next_cursor,omitempty"`
	}
)

var (
	accountSchema = objectSchema("name", stringSchema, "accountName", stringSchema, "type", stringSchema,
		"role", stringSchema, "permissionLevel", stringSchema, "verificationState", stringSchema,
		"vettedState", stringSchema, "accountNumber", stringSchema,
		"organizationInfo", objectSchema("registeredDomain", stringSchema, "address", postalAddressSchema,
			"phoneNumber", stringSchema))
	accountsResultSchema = `{"type":"object","properties":{"accounts":` + arraySchema(accountSchema) +
		`,"next_cursor":{"type":"string"}},"required":["accounts"],"additionalProperties":false}`
)

var accountFields = []capability.Field{
	{Name: "name", Description: "accounts/ACCOUNT"},
	{Name: "accountName", Description: "Name of the account; for a personal account the first and last name of the person"},
	{Name: "type", Description: "PERSONAL, LOCATION_GROUP, USER_GROUP, or ORGANIZATION"},
	{Name: "role", Description: "Role of the consenting account: PRIMARY_OWNER, OWNER, MANAGER, or SITE_MANAGER"},
	{Name: "permissionLevel", Description: "OWNER_LEVEL or MEMBER_LEVEL"},
	{Name: "verificationState", Description: "VERIFIED, UNVERIFIED, or VERIFICATION_REQUESTED"},
	{Name: "vettedState", Description: "NOT_VETTED, VETTED, or INVALID"},
	{Name: "accountNumber", Description: "Account reference number, if provisioned"},
	{Name: "organizationInfo", Description: "For an organization: registeredDomain, postal address, and phoneNumber"},
}

var accountsList = capability.Descriptor{
	ID: accountsListID, Version: 1, Title: "List Business Profile accounts",
	Description: "List the Google Business Profile accounts the consenting account owns or manages, the personal " +
		"account first, up to 20 per call with a cursor to read on; an empty list means none. parent_account lists " +
		"the accounts an organization or user group reaches directly. A connection bound to an account answers " +
		"with that account only, read directly; a connection bound to a location does not offer this tool. " +
		untrustedNote,
	Tags: []string{"google", "business-profile", "accounts"}, Risk: accountsRisk, Provider: Provider,
	InputSchema: inputSchema(nil,
		"parent_account", accountArgSchema,
		"type", `{"type":"string","enum":["PERSONAL","LOCATION_GROUP","USER_GROUP","ORGANIZATION"]}`,
		"page_size", `{"type":"integer","minimum":1,"maximum":20}`,
		"cursor", cursorSchema),
	OutputSchema: json.RawMessage(accountsResultSchema),
	Arguments: []capability.Argument{
		{Name: "parent_account", Description: "Organization or user group as accounts/ACCOUNT whose directly " +
			"reachable accounts to list; leave it out for the accounts of the consenting account. Not available on " +
			"a bound connection"},
		{Name: "type", Description: "Only accounts of this type: PERSONAL, LOCATION_GROUP, USER_GROUP, or ORGANIZATION"},
		{Name: "page_size", Description: "Accounts per call, 1 to 20; Google's default is 20"},
		cursorArgument,
	},
	Fields: []capability.Field{
		{Name: "accounts", Description: "Accounts with name, accountName, type, role, permissionLevel, " +
			"verificationState, vettedState, accountNumber, and organizationInfo"}, nextCursorField,
	},
	Examples: []capability.Example{
		{Description: "First accounts", Arguments: json.RawMessage(`{}`)},
		{Description: "Location groups only", Arguments: json.RawMessage(`{"type":"LOCATION_GROUP"}`)},
	},
}

var accountsGet = capability.Descriptor{
	ID: accountsGetID, Version: 1, Title: "Get a Business Profile account",
	Description: "Read one Google Business Profile account the consenting account owns or manages. A connection " +
		"bound to a location does not offer this tool. " + untrustedNote,
	Tags: []string{"google", "business-profile", "accounts"}, Risk: accountsRisk, Provider: Provider,
	InputSchema: inputSchema(nil, "account", accountArgSchema), OutputSchema: json.RawMessage(accountSchema),
	Arguments: []capability.Argument{{Name: "account", Target: "account", Description: "Account as " +
		"accounts/ACCOUNT; optional when the connection is bound to an account, and then it must be that account"}},
	Fields: accountFields,
	Examples: []capability.Example{{Description: "One account",
		Arguments: json.RawMessage(`{"account":"accounts/1000001"}`)}},
}

type accountsArguments struct {
	ParentAccount string `json:"parent_account"`
	Type          string `json:"type"`
	PageSize      int    `json:"page_size"`
	Cursor        string `json:"cursor"`
	Account       string `json:"account"`
}

func refusedCursor() error {
	return invalidArgument("cursor is not a next_cursor of this tool with these arguments; start again without cursor")
}

// pageQuery turns the cursor of a call into the pageToken of the request.
func pageQuery(binding []byte, cursor string, query url.Values) (url.Values, error) {
	if cursor == "" {
		return query, nil
	}
	token, ok := provider.DecodeCursor(binding, cursor, maxCursorSize)
	if !ok || token == "" {
		return nil, refusedCursor()
	}
	if query == nil {
		query = url.Values{}
	}
	query.Set("pageToken", token)
	return query, nil
}

// nextCursor wraps the nextPageToken of an answer; none ends the list.
func nextCursor(op string, binding []byte, token string) (string, error) {
	switch {
	case token == "":
		return "", nil
	case len(token) > maxPageToken:
		return "", provider.InvalidResponse(op, subject+" returned a continuation token that is too long")
	}
	return provider.EncodeCursor(binding, token), nil
}

func tooMany(op string, limit int) error {
	return provider.InvalidResponse(op, subject+" returned more than "+strconv.Itoa(limit)+" entries in one answer")
}

func handleAccountsList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage) (any, error) {
	const op = "list accounts"
	var args accountsArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, provider.Fail(op, "the validated arguments could not be read")
	}
	bound, err := checkConnection(resolved)
	if err != nil {
		return nil, err
	}
	if bound.location != "" {
		return nil, invalidArgument("accounts are not available on a connection bound to a location")
	}
	if args.ParentAccount != "" && !accountForm.MatchString(args.ParentAccount) {
		return nil, invalidArgument("parent_account must be accounts/ACCOUNT with a decimal ID")
	}
	if bound.account != "" {
		switch {
		case args.ParentAccount != "":
			return nil, invalidArgument("parent_account is not available on a connection bound to an account")
		case args.Cursor != "":
			return nil, refusedCursor()
		}
		c, err := open(ctx, resolved, secrets, red)
		if err != nil {
			return nil, err
		}
		var one account
		if err := c.get(ctx, op, accountsListID, request{root: accountRoot, path: accountPath(bound.account),
			notFound: notFoundAccount}, &one); err != nil {
			return nil, err
		}
		out := accountsResult{Accounts: []account{}}
		if args.Type == "" || string(one.Type) == args.Type {
			out.Accounts = append(out.Accounts, one)
		}
		return out, nil
	}
	binding := provider.CursorBinding(accountsListID, args.ParentAccount, args.Type, bound.binding())
	query := url.Values{}
	if args.ParentAccount != "" {
		query.Set("parentAccount", args.ParentAccount)
	}
	if args.Type != "" {
		query.Set("filter", "type="+args.Type)
	}
	query = pageSizeQuery(query, args.PageSize)
	if query, err = pageQuery(binding, args.Cursor, query); err != nil {
		return nil, err
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var answer accountsAnswer
	if err := c.get(ctx, op, accountsListID, request{root: accountRoot, path: accountsPath, query: query,
		notFound: notFoundAccount}, &answer); err != nil {
		return nil, err
	}
	if len(answer.Accounts) > maxAccountsPage {
		return nil, tooMany(op, maxAccountsPage)
	}
	out := accountsResult{Accounts: append([]account{}, answer.Accounts...)}
	if out.NextCursor, err = nextCursor(op, binding, answer.NextPageToken); err != nil {
		return nil, err
	}
	return out, nil
}

func handleAccountsGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage) (any, error) {
	const op = "get account"
	var args accountsArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, provider.Fail(op, "the validated arguments could not be read")
	}
	bound, err := checkConnection(resolved)
	if err != nil {
		return nil, err
	}
	name, err := bound.accountOf("account", args.Account, false)
	if err != nil {
		return nil, err
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var one account
	if err := c.get(ctx, op, accountsGetID, request{root: accountRoot, path: accountPath(name),
		notFound: notFoundAccount}, &one); err != nil {
		return nil, err
	}
	return one, nil
}
