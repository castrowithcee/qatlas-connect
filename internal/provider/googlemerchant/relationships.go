package googlemerchant

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	accountRelationshipsGetID    = Provider + ".account-relationships.get"
	accountRelationshipsUpdateID = Provider + ".account-relationships.update"
)

// The discovery document types the relationship as one path segment (`accounts/{account}/relationships/{id}`)
// and shows only a numeric example. Qatlas accepts letters, digits, dash, and underscore, at most 64, so an ID
// can never add a path segment, a dot, or an escape; whether Google knows the ID is its answer.
const (
	relationshipIDPattern = `[A-Za-z0-9_-]{1,64}`
	// relationshipUpdateMask is the only member the discovery document lists as writable. The mask is a
	// constant, never built from input.
	relationshipUpdateMask = "accountIdAlias"
	// The alias has no documented limit; the bound is ours and equals what a result shows.
	maxRelationshipAliasRunes = maxNameRunes
)

var relationshipIDForm = regexp.MustCompile(`^` + relationshipIDPattern + `$`)

const (
	relationshipArgSchema = `{"type":"string","minLength":1,"maxLength":64,"pattern":"^` + relationshipIDPattern + `$",` +
		`"x-form":"the relationship ID, the last segment of accounts/ACCOUNT/relationships/RELATIONSHIP"}`
	relationshipAliasSchema = `{"type":"string","minLength":1,"maxLength":300}`

	relationshipDelayNote = "Google applies the change with a delay, so the result only tells that Google accepted it; read " +
		"the relationship again to see it. The call counts against the quota of the Accounts group, a failed one " +
		"too, and it is never repeated: after a failure the outcome is unknown, so read the relationship before " +
		"changing it again. Needs the role Admin in Merchant Center, a missing role is answered with a permission " +
		"error, and every call needs a confirmation."
)

var relationshipArgument = capability.Argument{Name: "relationship", Required: true, Description: "Relationship ID, the " +
	"last segment of the name accounts/ACCOUNT/relationships/RELATIONSHIP, letters, digits, dash, and underscore, at " +
	"most 64 characters"}

var (
	accountRelationshipsGet = capability.Descriptor{
		ID: accountRelationshipsGetID, Version: 1, Title: "Get a relationship of a Merchant Center account",
		Description: "Read one relationship of an account with a provider account: the provider and the alias the account " +
			"gave the relationship. An account outside the targets of the connection is refused without a request; a " +
			"sub-account of a bound advanced account is proven by reading the sub-accounts first. " + servicesUntrusted,
		Tags: []string{"google", "merchant", "account-relationships"}, Risk: readRisk, Provider: Provider,
		InputSchema: inputSchema([]string{"relationship"}, member("account", accountArgSchema),
			member("relationship", relationshipArgSchema)),
		OutputSchema: json.RawMessage(accountRelationshipSchema),
		Arguments:    []capability.Argument{accountArgument, relationshipArgument},
		Fields:       []capability.Field{{Name: "relationship", Description: relationshipFieldsDescription}},
		Examples: []capability.Example{{Description: "One relationship",
			Arguments: json.RawMessage(`{"account":"1000001","relationship":"2000002"}`)}},
	}
	accountRelationshipsUpdate = capability.Descriptor{
		ID: accountRelationshipsUpdateID, Version: 1, Title: "Set the alias of a relationship of a Merchant Center account",
		Description: "Set the alias of one relationship (accounts.relationships.patch). The alias is a name of your own for the " +
			"link and must be unique among the relationships with the same provider. It is the only member that can " +
			"change: the update mask is fixed to accountIdAlias and the provider and the name cannot be set. The alias " +
			"cannot be cleared here. This neither creates nor ends a link and gives no provider access. " +
			relationshipDelayNote + " " + servicesUntrusted,
		Tags: []string{"google", "merchant", "account-relationships"}, Provider: Provider,
		Risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
		InputSchema: inputSchema([]string{"relationship", "account_id_alias"}, member("account", accountArgSchema),
			member("relationship", relationshipArgSchema), member("account_id_alias", relationshipAliasSchema)),
		OutputSchema: json.RawMessage(accountRelationshipSchema),
		Arguments: []capability.Argument{
			{Name: "account", Target: kindAccount, Description: "Merchant Center account ID, digits only; " +
				"the tool refuses a call without it; it must be named by a target or be a direct sub-account of a bound " +
				"advanced account"},
			relationshipArgument,
			{Name: "account_id_alias", Required: true, Description: "New alias, 1 to 300 characters without control " +
				"characters"}},
		Fields: []capability.Field{{Name: "relationship", Description: "The relationship as Google answered, with " +
			relationshipFieldsDescription}},
		Examples: []capability.Example{{Description: "Name a relationship", Arguments: json.RawMessage(
			`{"account":"1000001","relationship":"2000002","account_id_alias":"Example Alias"}`)}},
	}
)

func relationshipDescriptors() []capability.Descriptor {
	return []capability.Descriptor{accountRelationshipsGet, accountRelationshipsUpdate}
}

func relationshipHandlers() map[string]capability.Handler {
	return map[string]capability.Handler{accountRelationshipsGetID: handleAccountRelationshipsGet,
		accountRelationshipsUpdateID: handleAccountRelationshipsUpdate}
}

func relationshipSummaries() map[string]capability.SummaryFunc {
	return map[string]capability.SummaryFunc{accountRelationshipsUpdateID: relationshipUpdateSummary}
}

type relationshipArguments struct {
	Account        string  `json:"account"`
	Relationship   string  `json:"relationship"`
	AccountIDAlias *string `json:"account_id_alias"`
}

// relationshipBody is the AccountRelationship as sent: the one writable member and nothing else.
type relationshipBody struct {
	AccountIDAlias string `json:"accountIdAlias"`
}

// relationshipBind reads the arguments, refusing any member they do not name, and settles the account and
// the relationship ID. Nothing is requested and no secret is read.
func relationshipBind(resolved *config.Resolved, raw json.RawMessage, update bool) (
	args relationshipArguments, bound scope, id string, needsProof bool, err error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&args) != nil {
		return args, bound, "", false, invalidArgument("the arguments hold a member or a value this tool does not take")
	}
	if bound, err = checkConnection(resolved); err != nil {
		return args, bound, "", false, err
	}
	if update && args.Account == "" {
		return args, bound, "", false, invalidArgument("account is required")
	}
	if id, needsProof, err = bound.accountOf(args.Account); err != nil {
		return args, bound, "", false, err
	}
	switch {
	case !relationshipIDForm.MatchString(args.Relationship):
		return args, bound, "", false, invalidArgument("relationship must be a relationship ID of letters, digits, " +
			"dash, and underscore, at most 64 characters")
	case !update && args.AccountIDAlias != nil:
		return args, bound, "", false, invalidArgument("account_id_alias is not an argument of this tool")
	case update && args.AccountIDAlias == nil:
		return args, bound, "", false, invalidArgument("account_id_alias is required")
	case update && !validRelationshipAlias(*args.AccountIDAlias):
		return args, bound, "", false, invalidArgument("account_id_alias must be 1 to " +
			strconv.Itoa(maxRelationshipAliasRunes) + " characters without control characters")
	}
	return args, bound, id, needsProof, nil
}

func validRelationshipAlias(alias string) bool {
	return strings.TrimSpace(alias) != "" && utf8.RuneCountInString(alias) <= maxRelationshipAliasRunes &&
		utf8.ValidString(alias) && strings.IndexFunc(alias, unicode.IsControl) < 0
}

func relationshipPath(account, id string) string { return relationshipsPath(account) + "/" + id }

func handleAccountRelationshipsGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "get account relationship"
	args, bound, account, needsProof, err := relationshipBind(resolved, raw, false)
	if err != nil {
		return nil, err
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if needsProof {
		if err := cl.prove(ctx, op, accountRelationshipsGetID, account); err != nil {
			return nil, err
		}
	}
	var out accountRelationship
	if err := cl.get(ctx, op, accountRelationshipsGetID, relationshipPath(account, args.Relationship), nil, &out); err != nil {
		return nil, err
	}
	return out.masked(bound), nil
}

// handleAccountRelationshipsUpdate sends one patch with the fixed mask and a body of the alias alone; a failure
// is reported with its open outcome, never repeated.
func handleAccountRelationshipsUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "update account relationship"
	args, bound, account, needsProof, err := relationshipBind(resolved, raw, true)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(relationshipBody{AccountIDAlias: *args.AccountIDAlias})
	if err != nil {
		return nil, provider.Fail(op, "the request could not be built")
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if needsProof {
		if err := cl.prove(ctx, op, accountRelationshipsUpdateID, account); err != nil {
			return nil, err
		}
	}
	var out accountRelationship
	r := request{method: http.MethodPatch, path: relationshipPath(account, args.Relationship),
		query: url.Values{"updateMask": {relationshipUpdateMask}}, body: body, change: true}
	if err := cl.send(ctx, op, accountRelationshipsUpdateID, r, &out); err != nil {
		return nil, err
	}
	return out.masked(bound), nil
}

// relationshipUpdateSummary phrases the confirmation from the arguments alone.
func relationshipUpdateSummary(raw json.RawMessage) string {
	var a relationshipArguments
	if json.Unmarshal(raw, &a) != nil || a.AccountIDAlias == nil {
		return ""
	}
	return "Set the alias of relationship " + strconv.Quote(a.Relationship) + " of account " + a.Account + " to " +
		strconv.Quote(*a.AccountIDAlias) + "; Google applies the change afterwards"
}
