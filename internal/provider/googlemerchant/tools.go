package googlemerchant

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"unicode"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// contentScope is the only scope of the Merchant API. It allows reading and writing; the role of the user in
// Merchant Center decides what a call may do.
const contentScope = "https://www.googleapis.com/auth/content"

func toolScopes() map[string][]string {
	scopes := map[string][]string{}
	for _, d := range descriptors() {
		scopes[d.ID] = []string{contentScope}
	}
	return scopes
}

var readRisk = capability.Risk{
	Effect: capability.EffectRead, Idempotency: capability.IdempotencySafe, Confirmation: capability.ConfirmationNone,
	OpenWorld: true, DataSensitivity: "merchant-account-data",
}

const untrustedNote = "Account names and home page addresses are untrusted content of whoever runs the account: " +
	"texts are cut, and addresses are text that is never fetched."

const (
	accountsListID              = Provider + ".accounts.list"
	accountsGetID               = Provider + ".accounts.get"
	developerRegistrationsGetID = Provider + ".developer-registrations.get"
)

// accountsAPI is the path prefix of the accounts sub-API.
const accountsAPI = "accounts/v1/"

// Bounds of the arguments and of the proof reads of the sub-accounts of an advanced account.
const (
	maxSubaccountPages = 20
	subaccountPageSize = 500
)

const (
	accountArgSchema = `{"type":"string","maxLength":19,"pattern":"^` + idPattern + `$",` +
		`"x-form":"the numeric Merchant Center account ID"}`
	cursorSchema   = `{"type":"string","minLength":1,"maxLength":4096}`
	filterSchema   = `{"type":"string","minLength":1,"maxLength":500}`
	pageSizeSchema = `{"type":"integer","minimum":1,"maximum":500}`
)

const (
	accountFieldsDescription = "name (accounts/ACCOUNT), accountId, accountName, testAccount, adultContent, " +
		"languageCode, homePageUri, and timeZone (id, version)"
	accountsListQuotaNote = "The quota of this method is counted per user, not per account."
	accountPathNote       = "Google lists the accounts of the user and, for an advanced account, its sub-accounts."
)

var (
	accountArgument = capability.Argument{Name: "account", Target: kindAccount, Description: "Merchant Center " +
		"account ID, digits only; optional when the connection is bound to exactly one target, and then it is that account"}
	advancedArgument = capability.Argument{Name: "account", Target: kindAdvanced, Description: "ID of the advanced " +
		"account, digits only; on a bound connection it must be an advanced account the connection is bound to, " +
		"and it is optional when the connection is bound to exactly one advanced account"}
	cursorArgument = capability.Argument{Name: "cursor", Description: "next_cursor of the previous call with the " +
		"same arguments, to read on; leave it out for the start"}
)

// inputSchema is a closed object schema of the members; required names those the call cannot do without.
func inputSchema(required []string, members ...string) json.RawMessage {
	schema := `{"type":"object","properties":{` + strings.Join(members, ",") + `}`
	if len(required) > 0 {
		names, _ := json.Marshal(required)
		schema += `,"required":` + string(names)
	}
	return json.RawMessage(schema + `,"additionalProperties":false}`)
}

func descriptors() []capability.Descriptor {
	return append([]capability.Descriptor{accountsList, accountsGet, developerRegistrationsGet,
		productsList, productsGet, aggregateProductStatusesList, accountIssuesList,
		localInventoriesList, localInventoriesInsert, localInventoriesDelete,
		regionalInventoriesList, regionalInventoriesInsert, regionalInventoriesDelete,
		autofeedSettingsGet, autofeedSettingsUpdate, automaticImprovementsGet, automaticImprovementsUpdate,
		programsList, programsGet, programsEnable, programsDisable,
		reportsSearch, quotasList, limitsList, limitsGet,
		termsOfServiceGet, termsOfServiceRetrieveLatest, termsOfServiceAgreementStatesGet,
		termsOfServiceAgreementStatesRetrieveForApplication, usersList, usersGet, usersUpdate, usersDelete,
		regionsList, regionsGet, regionsCreate, regionsUpdate,
		regionsDelete, regionsBatchCreate, regionsBatchUpdate, regionsBatchDelete,
		notificationSubscriptionsList, notificationSubscriptionsGet, notificationSubscriptionsDelete,
		accountServicesList, accountServicesGet, accountServicesReject, accountRelationshipsList,
		conversionSourcesList, conversionSourcesGet, conversionSourcesCreate, conversionSourcesUpdate,
		conversionSourcesUndelete, conversionSourcesDelete,
		renderAccountIssues, renderProductIssues, emailPreferencesGet, emailPreferencesUpdate,
		dataSourcesList, dataSourcesGet, dataSourcesCreate, dataSourcesUpdate,
		promotionsList, promotionsGet, promotionsInsert,
		productReviewsList, productReviewsGet, productReviewsDelete,
		businessInfosGet, businessInfosUpdate, businessIdentitiesGet, businessIdentitiesUpdate},
		append(append(append(append(append(accountWriteDescriptors(), homepageDescriptors()...), omnichannelDescriptors()...),
			checkoutDescriptors()...), returnPolicyDescriptors()...),
			append(append(append(append(append(relationshipDescriptors(), linkDescriptors()...), developerRegistrationDescriptors()...),
				shippingDescriptors()...), dataSourceOperationDescriptors()...), productInputDescriptors()...)...)...)
}

var accountsList = capability.Descriptor{
	ID: accountsListID, Version: 1, Title: "List Merchant Center accounts",
	Description: "List the Merchant Center accounts the credential can access, a batch per call with a cursor to read " +
		"on. " + accountPathNote + " On a connection bound to targets only the accounts of the targets and the " +
		"direct sub-accounts of a bound advanced account are listed: every row is checked here, so a batch can be " +
		"short or empty while next_cursor says there is more. Reading the sub-accounts of a bound advanced " +
		"account costs further calls. filter takes the Merchant API filter syntax (for example " +
		`accountName = "*store*", access = DIRECT, or relationship(providerId = 123)` +
		") and only narrows what Google sends. " + accountsListQuotaNote + " " + untrustedNote,
	Tags: []string{"google", "merchant", "accounts"}, Risk: readRisk, Provider: Provider,
	InputSchema: inputSchema(nil, member("filter", filterSchema), member("page_size", pageSizeSchema),
		member("cursor", cursorSchema)),
	OutputSchema: json.RawMessage(listResultSchema("accounts", accountSchema)),
	Arguments: []capability.Argument{
		{Name: "filter", Description: "Merchant API account filter, at most 500 characters, without control " +
			"characters; see the filter syntax of the accounts API"},
		{Name: "page_size", Description: "Accounts per batch, 1 to 500; Google sends up to 250 when left out"},
		cursorArgument,
	},
	Fields: []capability.Field{
		{Name: "accounts", Description: "Accounts with " + accountFieldsDescription},
		{Name: "next_cursor", Description: "Cursor to read on with; missing at the end"},
	},
	Examples: []capability.Example{{Description: "First batch of accounts", Arguments: json.RawMessage(`{}`)}},
}

var accountsGet = capability.Descriptor{
	ID: accountsGetID, Version: 1, Title: "Get a Merchant Center account",
	Description: "Read one Merchant Center account. On a connection bound to an advanced account, an account that " +
		"is not named by a target is read only after it is found among the direct sub-accounts of a bound advanced " +
		"account, which costs further calls. After an account is created or changed it can take several minutes " +
		"until the change shows. " + untrustedNote,
	Tags: []string{"google", "merchant", "accounts"}, Risk: readRisk, Provider: Provider,
	InputSchema: inputSchema(nil, member("account", accountArgSchema)), OutputSchema: json.RawMessage(accountSchema),
	Arguments: []capability.Argument{accountArgument},
	Fields:    []capability.Field{{Name: "account", Description: accountFieldsDescription}},
	Examples:  []capability.Example{{Description: "One account", Arguments: json.RawMessage(`{"account":"1000001"}`)}},
}

var developerRegistrationsGet = capability.Descriptor{
	ID: developerRegistrationsGetID, Version: 1, Title: "Get the developer registration of an account",
	Description: "Read the Google Cloud projects registered as developers with a Merchant Center account. " +
		"The registration belongs to the account the user or service account was added to, normally the advanced account or a standalone account. An account without a registration, or a call from a Cloud project that is " +
		"not registered, is refused by Google. On a connection bound to targets this tool needs the advanced " +
		"account target of the account. The list holds at most 100 project IDs.",
	Tags: []string{"google", "merchant", "developer-registrations"}, Risk: readRisk, Provider: Provider,
	InputSchema: inputSchema(nil, member("account", accountArgSchema)), OutputSchema: json.RawMessage(developerRegistrationSchema),
	Arguments: []capability.Argument{advancedArgument},
	Fields:    []capability.Field{{Name: "name", Description: "accounts/ACCOUNT/developerRegistration"}, {Name: "gcpIds", Description: "IDs of the registered Google Cloud projects"}},
	Examples:  []capability.Example{{Description: "Registration of one account", Arguments: json.RawMessage(`{"account":"1000001"}`)}},
}

type toolArguments struct {
	Account  string `json:"account"`
	Filter   string `json:"filter"`
	PageSize int    `json:"page_size"`
	Cursor   string `json:"cursor"`
}

// call is one handler run: the checked binding and arguments. Every account is settled against the binding
// before any secret is resolved.
type call struct {
	op, tool string
	scope    scope
	args     toolArguments
}

func prepare(op, tool string, resolved *config.Resolved, raw json.RawMessage) (call, error) {
	c := call{op: op, tool: tool}
	if err := json.Unmarshal(raw, &c.args); err != nil {
		return c, provider.Fail(op, "the validated arguments could not be read")
	}
	bound, err := checkConnection(resolved)
	if err != nil {
		return c, err
	}
	c.scope = bound
	return c, nil
}

func accountPath(id string) string { return accountsAPI + "accounts/" + id }

func tooMany(op string) error {
	return provider.InvalidResponse(op, subject+" returned more than "+strconv.Itoa(maxEntries)+" entries in one answer")
}

func refusedCursor() error {
	return invalidArgument("cursor is not a next_cursor of this tool with these arguments; start again without cursor")
}

// checkFilter refuses a filter that is not plain text. Google parses the syntax; a control character has no
// place in it.
func checkFilter(filter string) error {
	for _, r := range filter {
		if unicode.IsControl(r) {
			return invalidArgument("filter must not contain control characters")
		}
	}
	return nil
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

func handleAccountsList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage) (any, error) {
	c, err := prepare("list accounts", accountsListID, resolved, raw)
	if err != nil {
		return nil, err
	}
	if err := checkFilter(c.args.Filter); err != nil {
		return nil, err
	}
	binding := provider.CursorBinding(c.tool, c.args.Filter, c.args.PageSize, c.scope.key())
	query := url.Values{}
	if c.args.Filter != "" {
		query.Set("filter", c.args.Filter)
	}
	if c.args.PageSize > 0 {
		query.Set("pageSize", strconv.Itoa(c.args.PageSize))
	}
	if c.args.Cursor != "" {
		token, ok := provider.DecodeCursor(binding, c.args.Cursor, maxCursorSize)
		if !ok || token == "" {
			return nil, refusedCursor()
		}
		query.Set("pageToken", token)
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var answer accountsAnswer
	if err := cl.get(ctx, c.op, c.tool, accountsAPI+"accounts", query, &answer); err != nil {
		return nil, err
	}
	if len(answer.Accounts) > maxEntries {
		return nil, tooMany(c.op)
	}
	rows, err := cl.reachable(ctx, c.op, c.tool, answer.Accounts)
	if err != nil {
		return nil, err
	}
	out := accountsResult{Accounts: rows}
	if out.NextCursor, err = nextCursor(c.op, binding, answer.NextPageToken); err != nil {
		return nil, err
	}
	return out, nil
}

func handleAccountsGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage) (any, error) {
	c, err := prepare("get account", accountsGetID, resolved, raw)
	if err != nil {
		return nil, err
	}
	id, needsProof, err := c.scope.accountOf(c.args.Account)
	if err != nil {
		return nil, err
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if needsProof {
		if err := cl.prove(ctx, c.op, c.tool, id); err != nil {
			return nil, err
		}
	}
	var out account
	if err := cl.get(ctx, c.op, c.tool, accountPath(id), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func handleDeveloperRegistrationsGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("get developer registration", developerRegistrationsGetID, resolved, raw)
	if err != nil {
		return nil, err
	}
	id, err := c.scope.advancedOf(c.args.Account)
	if err != nil {
		return nil, err
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	return cl.developerRegistration(ctx, c.op, c.tool, id)
}

func (c *client) developerRegistration(ctx context.Context, op, tool, id string) (developerRegistration, error) {
	var out developerRegistration
	err := c.get(ctx, op, tool, accountPath(id)+"/developerRegistration", nil, &out)
	return out, err
}

// walkSubaccounts reads the direct sub-accounts of an advanced account page by page and hands each ID to
// visit until it returns true. It reports whether the walk stopped early. It never reads more than
// maxSubaccountPages pages: a longer list ends the call, because an account that cannot be proven to belong
// to a target is refused, not guessed.
func (c *client) walkSubaccounts(ctx context.Context, op, tool, advanced string, visit func(id string) bool) (bool, error) {
	token := ""
	for page := 0; page < maxSubaccountPages; page++ {
		query := url.Values{"pageSize": {strconv.Itoa(subaccountPageSize)}}
		if token != "" {
			query.Set("pageToken", token)
		}
		var answer accountsAnswer
		if err := c.get(ctx, op, tool, accountPath(advanced)+":listSubaccounts", query, &answer); err != nil {
			return false, err
		}
		if len(answer.Accounts) > maxEntries {
			return false, tooMany(op)
		}
		for _, a := range answer.Accounts {
			if id, ok := a.id(); ok && visit(id) {
				return true, nil
			}
		}
		token = answer.NextPageToken
		if token == "" {
			return false, nil
		}
		if len(token) > maxPageToken {
			return false, provider.InvalidResponse(op, subject+" returned a continuation token that is too long")
		}
	}
	return false, provider.Fail(op, "an advanced account of this connection has more sub-accounts than can be checked "+
		"in one call; bind the accounts themselves instead")
}

// prove is the one proof read that binds an account that no target names to a bound advanced account: the
// account must be one of its direct sub-accounts. It refuses without naming the account otherwise.
func (c *client) prove(ctx context.Context, op, tool, id string) error {
	for _, advanced := range c.scope.advanced {
		found, err := c.walkSubaccounts(ctx, op, tool, advanced, func(sub string) bool { return sub == id })
		if err != nil {
			return err
		}
		if found {
			return nil
		}
	}
	return invalidArgument("account " + outsideTargets)
}

// reachable keeps of a batch of accounts what the binding reaches. A row whose ID cannot be read is dropped.
// The sub-accounts of the bound advanced accounts are read once, and only if a row needs them.
func (c *client) reachable(ctx context.Context, op, tool string, rows []account) ([]account, error) {
	if !c.scope.bound() {
		return append([]account{}, rows...), nil
	}
	var subs map[string]bool
	kept := []account{}
	for _, row := range rows {
		id, ok := row.id()
		if !ok {
			continue
		}
		switch c.scope.reachOf(id) {
		case reachNamed:
			kept = append(kept, row)
		case reachProof:
			if subs == nil {
				subs = map[string]bool{}
				for _, advanced := range c.scope.advanced {
					if _, err := c.walkSubaccounts(ctx, op, tool, advanced, func(sub string) bool {
						subs[sub] = true
						return false
					}); err != nil {
						return nil, err
					}
				}
			}
			if subs[id] {
				kept = append(kept, row)
			}
		}
	}
	return kept, nil
}
