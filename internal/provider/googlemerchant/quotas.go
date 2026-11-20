package googlemerchant

import (
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

const (
	quotasListID = Provider + ".quotas.list"
	limitsListID = Provider + ".limits.list"
	limitsGetID  = Provider + ".limits.get"
)

// quotaAPI is the path prefix of the quota sub-API. The limits belong to the accounts sub-API.
const quotaAPI = "quota/v1/"

const (
	maxQuotaPageSize = 1000
	maxLimitPageSize = 100
	maxMethodDetails = 100
)

// limitIDPattern is the form of the limit part of a limit name, such as products~ADS_NON_EEA: letters, digits,
// underscore, tilde, and hyphen, so the segment can neither leave the path nor be a dot segment.
const limitIDPattern = `[A-Za-z0-9_~-]{1,100}`

var limitID = regexp.MustCompile(`^` + limitIDPattern + `$`)

// limitTypes are the documented types of a limit: the AccountLimit resource has one member, products.
var limitTypes = []string{"products"}

type numText string // int64 values, which Google sends as strings

func (s *numText) UnmarshalJSON(b []byte) error {
	var text string
	if json.Unmarshal(b, &text) == nil && intValue.MatchString(text) {
		*s = numText(text)
	}
	return nil
}

type methodDetail struct {
	Version idText `json:"version,omitempty"`
	Method  idText `json:"method,omitempty"`
	Subapi  idText `json:"subapi,omitempty"`
	Path    idText `json:"path,omitempty"`
}

type methodDetails []methodDetail

// UnmarshalJSON keeps at most maxMethodDetails entries of a group.
func (l *methodDetails) UnmarshalJSON(b []byte) error {
	var raw []json.RawMessage
	if json.Unmarshal(b, &raw) != nil {
		*l = nil
		return nil
	}
	var out methodDetails
	for _, item := range raw {
		if len(out) == maxMethodDetails {
			break
		}
		var d methodDetail
		if json.Unmarshal(item, &d) == nil {
			out = append(out, d)
		}
	}
	*l = out
	return nil
}

// quotaGroup is the QuotaGroup resource of the quota sub-API.
type quotaGroup struct {
	Name             idText        `json:"name,omitempty"`
	QuotaUsage       numText       `json:"quotaUsage,omitempty"`
	QuotaLimit       numText       `json:"quotaLimit,omitempty"`
	QuotaMinuteLimit numText       `json:"quotaMinuteLimit,omitempty"`
	MethodDetails    methodDetails `json:"methodDetails,omitempty"`
}

type productLimit struct {
	Scope idText  `json:"scope,omitempty"`
	Limit numText `json:"limit,omitempty"`
}

// accountLimit is the AccountLimit resource of the accounts sub-API; its one documented type is products.
type accountLimit struct {
	Name     idText        `json:"name,omitempty"`
	Products *productLimit `json:"products,omitempty"`
}

type quotasAnswer struct {
	QuotaGroups   []quotaGroup `json:"quotaGroups"`
	NextPageToken string       `json:"nextPageToken"`
}

type limitsAnswer struct {
	AccountLimits []accountLimit `json:"accountLimits"`
	NextPageToken string         `json:"nextPageToken"`
}

type quotasResult struct {
	QuotaGroups []quotaGroup `json:"quotaGroups"`
	NextCursor  string       `json:"next_cursor,omitempty"`
}

type limitsResult struct {
	AccountLimits []accountLimit `json:"accountLimits"`
	NextCursor    string         `json:"next_cursor,omitempty"`
}

var (
	quotaGroupSchema = objectSchema(member("name", stringSchema), member("quotaUsage", stringSchema),
		member("quotaLimit", stringSchema), member("quotaMinuteLimit", stringSchema),
		member("methodDetails", `{"type":"array","items":`+objectSchema(member("version", stringSchema),
			member("method", stringSchema), member("subapi", stringSchema), member("path", stringSchema))+`}`))
	accountLimitSchema = objectSchema(member("name", stringSchema),
		member("products", objectSchema(member("scope", stringSchema), member("limit", stringSchema))))
)

const quotaFieldsDescription = "name (accounts/ACCOUNT/quotas/GROUP), quotaUsage (calls made today), quotaLimit " +
	"(calls per day), quotaMinuteLimit (calls per minute), and methodDetails (version, method, subapi, path)"

const limitFieldsDescription = "name (accounts/ACCOUNT/limits/LIMIT) and products (scope, limit)"

const untrustedQuotaNote = "Names and paths come from Google and are untrusted content: texts are cut."

var quotasList = capability.Descriptor{
	ID: quotasListID, Version: 1, Title: "List the API quotas of a Merchant Center account",
	Description: "List the method groups of the Merchant API with their daily limit, calls used today, and per-minute " +
		"limit for an account, sorted by usage, a batch per call with a cursor to read on. The daily limits reset at " +
		"12:00 UTC. The call itself counts against the quota of its own group, which Google gives as an example " +
		"as 1000 per day and 10 per minute, so do not poll it. " + untrustedQuotaNote,
	Tags: []string{"google", "merchant", "quotas"}, Risk: readRisk, Provider: Provider,
	InputSchema: inputSchema(nil, member("account", accountArgSchema),
		member("page_size", `{"type":"integer","minimum":1,"maximum":1000}`), member("cursor", cursorSchema)),
	OutputSchema: json.RawMessage(listResultSchema("quotaGroups", quotaGroupSchema)),
	Arguments: []capability.Argument{accountArgument,
		{Name: "page_size", Description: "Groups per batch, 1 to 1000; Google sends up to 500 when left out"},
		cursorArgument,
	},
	Fields: []capability.Field{
		{Name: "quotaGroups", Description: "Method groups with " + quotaFieldsDescription},
		{Name: "next_cursor", Description: "Cursor to read on with; missing at the end"},
	},
	Examples: []capability.Example{{Description: "Quotas of one account", Arguments: json.RawMessage(`{"account":"1000001"}`)}},
}

var limitsList = capability.Descriptor{
	ID: limitsListID, Version: 1, Title: "List the limits of a Merchant Center account",
	Description: "List the limits of one type that apply to an account, a batch per call with a cursor to read on. " +
		"The only documented type is products, the number of products the account may hold per scope " +
		"(ADS_NON_EEA, ADS_EEA). " + untrustedQuotaNote,
	Tags: []string{"google", "merchant", "limits"}, Risk: readRisk, Provider: Provider,
	InputSchema: inputSchema([]string{"type"}, member("account", accountArgSchema), member("type", enumSchema(limitTypes)),
		member("page_size", `{"type":"integer","minimum":1,"maximum":100}`), member("cursor", cursorSchema)),
	OutputSchema: json.RawMessage(listResultSchema("accountLimits", accountLimitSchema)),
	Arguments: []capability.Argument{accountArgument,
		{Name: "type", Required: true, Description: "type of the limits to list; products"},
		{Name: "page_size", Description: "Limits per batch, 1 to 100; Google sends up to 100 when left out"},
		cursorArgument,
	},
	Fields: []capability.Field{
		{Name: "accountLimits", Description: "Limits with " + limitFieldsDescription},
		{Name: "next_cursor", Description: "Cursor to read on with; missing at the end"},
	},
	Examples: []capability.Example{{Description: "Product limits of one account",
		Arguments: json.RawMessage(`{"account":"1000001","type":"products"}`)}},
}

var limitsGet = capability.Descriptor{
	ID: limitsGetID, Version: 1, Title: "Get one limit of a Merchant Center account",
	Description: "Read one limit of an account by the limit part of its name, as limits.list shows it " +
		"(for example products~ADS_NON_EEA). " + untrustedQuotaNote,
	Tags: []string{"google", "merchant", "limits"}, Risk: readRisk, Provider: Provider,
	InputSchema: inputSchema([]string{"limit"}, member("account", accountArgSchema),
		member("limit", `{"type":"string","maxLength":100,"pattern":"^`+limitIDPattern+`$"}`)),
	OutputSchema: json.RawMessage(accountLimitSchema),
	Arguments: []capability.Argument{accountArgument,
		{Name: "limit", Required: true, Description: "the limit part of the limit name, such as products~ADS_NON_EEA"}},
	Fields:   []capability.Field{{Name: "limit", Description: limitFieldsDescription}},
	Examples: []capability.Example{{Description: "One limit", Arguments: json.RawMessage(`{"account":"1000001","limit":"products~ADS_NON_EEA"}`)}},
}

type quotaArguments struct {
	Type  string `json:"type"`
	Limit string `json:"limit"`
}

// listPage composes the query and the binding of a cursor for one of the list tools of this file.
func listPage(c call, account string, pageSize, ceiling int, extra url.Values) (url.Values, []byte, error) {
	if pageSize > ceiling {
		return nil, nil, invalidArgument("page_size must be 1 to " + strconv.Itoa(ceiling))
	}
	binding := provider.CursorBinding(c.tool, account, extra.Encode(), c.scope.key())
	query := url.Values{}
	for k, v := range extra {
		query[k] = v
	}
	if pageSize > 0 {
		query.Set("pageSize", strconv.Itoa(pageSize))
	}
	if c.args.Cursor != "" {
		token, ok := provider.DecodeCursor(binding, c.args.Cursor, maxCursorSize)
		if !ok || token == "" {
			return nil, nil, refusedCursor()
		}
		query.Set("pageToken", token)
	}
	return query, binding, nil
}

// openForAccount settles an account argument and opens the client, with the proof read when the account
// needs one. The account is settled before any secret is resolved.
func openForAccount(ctx context.Context, c call, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, check func(id string) error) (string, *client, error) {
	id, needsProof, err := c.scope.accountOf(c.args.Account)
	if err != nil {
		return "", nil, err
	}
	if check != nil {
		if err := check(id); err != nil {
			return "", nil, err
		}
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return "", nil, err
	}
	if needsProof {
		if err := cl.prove(ctx, c.op, c.tool, id); err != nil {
			return "", nil, err
		}
	}
	return id, cl, nil
}

func handleQuotasList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage) (any, error) {
	c, err := prepare("list quotas", quotasListID, resolved, raw)
	if err != nil {
		return nil, err
	}
	var query url.Values
	var binding []byte
	id, cl, err := openForAccount(ctx, c, resolved, secrets, red, func(id string) (err error) {
		query, binding, err = listPage(c, id, c.args.PageSize, maxQuotaPageSize, nil)
		return err
	})
	if err != nil {
		return nil, err
	}
	var answer quotasAnswer
	if err := cl.get(ctx, c.op, c.tool, quotaAPI+"accounts/"+id+"/quotas", query, &answer); err != nil {
		return nil, err
	}
	if len(answer.QuotaGroups) > maxEntries {
		return nil, tooMany(c.op)
	}
	out := quotasResult{QuotaGroups: append([]quotaGroup{}, answer.QuotaGroups...)}
	if out.NextCursor, err = nextCursor(c.op, binding, answer.NextPageToken); err != nil {
		return nil, err
	}
	return out, nil
}

func handleLimitsList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage) (any, error) {
	c, err := prepare("list limits", limitsListID, resolved, raw)
	if err != nil {
		return nil, err
	}
	var q quotaArguments
	if err := json.Unmarshal(raw, &q); err != nil {
		return nil, provider.Fail(c.op, "the validated arguments could not be read")
	}
	if q.Type != "products" {
		return nil, invalidArgument("type must be products")
	}
	var query url.Values
	var binding []byte
	id, cl, err := openForAccount(ctx, c, resolved, secrets, red, func(id string) (err error) {
		query, binding, err = listPage(c, id, c.args.PageSize, maxLimitPageSize,
			url.Values{"filter": {`type = "` + q.Type + `"`}})
		return err
	})
	if err != nil {
		return nil, err
	}
	var answer limitsAnswer
	if err := cl.get(ctx, c.op, c.tool, accountPath(id)+"/limits", query, &answer); err != nil {
		return nil, err
	}
	if len(answer.AccountLimits) > maxEntries {
		return nil, tooMany(c.op)
	}
	out := limitsResult{AccountLimits: append([]accountLimit{}, answer.AccountLimits...)}
	if out.NextCursor, err = nextCursor(c.op, binding, answer.NextPageToken); err != nil {
		return nil, err
	}
	return out, nil
}

func handleLimitsGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage) (any, error) {
	c, err := prepare("get limit", limitsGetID, resolved, raw)
	if err != nil {
		return nil, err
	}
	var q quotaArguments
	if err := json.Unmarshal(raw, &q); err != nil {
		return nil, provider.Fail(c.op, "the validated arguments could not be read")
	}
	if !limitID.MatchString(q.Limit) {
		return nil, invalidArgument("limit must be the limit part of a limit name, such as products~ADS_NON_EEA")
	}
	id, cl, err := openForAccount(ctx, c, resolved, secrets, red, nil)
	if err != nil {
		return nil, err
	}
	var out accountLimit
	if err := cl.get(ctx, c.op, c.tool, accountPath(id)+"/limits/"+q.Limit, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}
