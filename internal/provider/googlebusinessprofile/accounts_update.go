package googlebusinessprofile

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const accountsUpdateID = Provider + ".accounts.update"

const (
	accountsUpdateMask = "accountName"
	maxAccountName     = 500
)

var accountsUpdate = capability.Descriptor{
	ID: accountsUpdateID, Version: 1, Title: "Rename a Business Profile account",
	Description: "Change the name of one Google Business Profile account; accountName is the only field Google lets " +
		"an update change, and the tool sends nothing else, so it changes no owner. Google refuses a personal " +
		"account, which the tool refuses as well before sending. previous_account_name is required and only " +
		"compared: the tool reads the account first and changes nothing unless it still has exactly that name, so " +
		"the confirmation shows the old and the new name. validate_only asks Google to check the change without " +
		"making it. Google counts at most 10 changes per minute and profile. A connection bound to a location " +
		"does not offer this tool. " + untrustedNote,
	Tags: []string{"google", "business-profile", "accounts", "update"}, Provider: Provider,
	Risk: capability.Risk{Effect: capability.EffectUpdate, Idempotency: capability.IdempotencyIdempotent,
		Confirmation: capability.ConfirmationRequired, OpenWorld: true,
		DataSensitivity: "business account data: the name of the account, for a personal account the name of a person"},
	InputSchema: inputSchema([]string{"account_name", "previous_account_name"},
		"account", accountArgSchema,
		"account_name", `{"type":"string","minLength":1,"maxLength":500}`,
		"previous_account_name", `{"type":"string","minLength":1,"maxLength":500}`,
		"update_mask", `{"type":"string","enum":["accountName"]}`,
		"validate_only", booleanSchema),
	OutputSchema: json.RawMessage(accountSchema),
	Arguments: []capability.Argument{
		{Name: "account", Target: "account", Description: "Account as accounts/ACCOUNT; optional when the " +
			"connection is bound to an account, and then it must be that account"},
		{Name: "account_name", Required: true, Description: "The new name, up to 500 characters, without control " +
			"characters"},
		{Name: "previous_account_name", Required: true, Description: "The accountName as last read with accounts.get; " +
			"only compared with what Google holds, never sent, and the change is refused when it differs"},
		{Name: "update_mask", Description: "The fields to update; only accountName is editable and the default"},
		{Name: "validate_only", Description: "true: Google validates the change and makes none; default false"},
	},
	Fields: accountFields,
	Examples: []capability.Example{{Description: "Rename a location group", Arguments: json.RawMessage(
		`{"account":"accounts/1000001","previous_account_name":"Old group","account_name":"New group"}`)}},
}

type accountUpdateArguments struct {
	Account             string `json:"account"`
	AccountName         string `json:"account_name"`
	PreviousAccountName string `json:"previous_account_name"`
	UpdateMask          string `json:"update_mask"`
	ValidateOnly        bool   `json:"validate_only"`
}

func handleAccountsUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "rename account"
	var args accountUpdateArguments
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
	switch {
	case args.UpdateMask != "" && args.UpdateMask != accountsUpdateMask:
		return nil, invalidArgument("update_mask must be accountName, the only field of an account that can be changed")
	case strings.TrimSpace(args.AccountName) == "" || utf8.RuneCountInString(args.AccountName) > maxAccountName ||
		strings.IndexFunc(args.AccountName, unicode.IsControl) >= 0:
		return nil, invalidArgument("account_name must be 1 to 500 characters without control characters")
	case args.PreviousAccountName == "":
		return nil, invalidArgument("previous_account_name is required: the accountName as last read with accounts.get")
	case args.AccountName == args.PreviousAccountName:
		return nil, invalidArgument("account_name is the name the account has already; there is nothing to change")
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var current account
	if err := c.get(ctx, op, accountsUpdateID, request{root: accountRoot, path: accountPath(name),
		notFound: notFoundAccount}, &current); err != nil {
		return nil, err
	}
	switch {
	case string(current.Type) == "PERSONAL":
		return nil, invalidArgument("a personal account cannot be renamed with this API")
	case string(current.AccountName) != args.PreviousAccountName:
		return nil, invalidArgument("previous_account_name is not the name the account holds now; read it with " +
			"accounts.get and send that name")
	}
	// Only accountName is sent: the account type, the role, and primaryOwner stay out of the body.
	body, err := json.Marshal(map[string]string{"accountName": args.AccountName})
	if err != nil {
		return nil, provider.Fail(op, "the request body could not be built")
	}
	query := url.Values{"updateMask": {accountsUpdateMask}}
	if args.ValidateOnly {
		query.Set("validateOnly", "true")
	}
	var out account
	if err := c.patch(ctx, op, accountsUpdateID, change{root: accountRoot, path: accountPath(name), query: query,
		body: body, key: name, notFound: notFoundAccount}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// accountUpdateSummary phrases a confirmation from the arguments alone; the core neutralizes and bounds it.
func accountUpdateSummary(raw json.RawMessage) string {
	var a accountUpdateArguments
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	out := "Rename the account"
	if a.Account != "" {
		out += " " + a.Account
	}
	out += " from \"" + a.PreviousAccountName + "\" to \"" + a.AccountName + "\"; no owner is changed"
	if a.ValidateOnly {
		out += "; validate only, nothing is changed"
	}
	return out
}
