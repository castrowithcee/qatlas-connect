package googleanalytics

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	accountsGetID     = Provider + ".accounts.get"
	accountsSharingID = Provider + ".accounts.get-data-sharing-settings"
	accountsUpdateID  = Provider + ".accounts.update"
	accountsDeleteID  = Provider + ".accounts.delete"

	// maxAccountBytes bounds the answer of one account call, a single small resource.
	maxAccountBytes = 1 << 20
	// maxDisplayUnits is the length Google allows a display name, in UTF-16 code units.
	maxDisplayUnits = 100
	// trashDays is the time Google's Analytics help names for the trash before an item is purged.
	trashDays = 35
)

const sharingSuffix = "/dataSharingSettings"

// changeRisk is the risk of a change of an account: confirmed, open to the account, and about its
// configuration.
func changeRisk(effect capability.Effect) capability.Risk {
	return capability.Risk{Effect: effect, Idempotency: capability.IdempotencyIdempotent,
		Confirmation: capability.ConfirmationRequired, OpenWorld: true, DataSensitivity: dataSensitivity}
}

// dataSharingSettings is the DataSharingSettings resource of analyticsadmin v1beta without its deprecated
// member sharingWithGoogleAnySalesEnabled. A switch Google leaves out is off.
type dataSharingSettings struct {
	Name                                  code `json:"name,omitempty"`
	SharingWithGoogleProductsEnabled      bool `json:"sharingWithGoogleProductsEnabled"`
	SharingWithOthersEnabled              bool `json:"sharingWithOthersEnabled"`
	SharingWithGoogleSupportEnabled       bool `json:"sharingWithGoogleSupportEnabled"`
	SharingWithGoogleAssignedSalesEnabled bool `json:"sharingWithGoogleAssignedSalesEnabled"`
}

const accountProperties = `{"name":{"type":"string"},"displayName":{"type":"string"},"regionCode":{"type":"string"},` +
	`"createTime":{"type":"string"},"updateTime":{"type":"string"},"deleted":{"type":"boolean"},` +
	`"gmpOrganization":{"type":"string"}}`

const accountSchema = `{"type":"object","properties":` + accountProperties + `,"additionalProperties":false}`

const accountFieldsText = "name (accounts/ID), displayName, regionCode (CLDR), createTime and updateTime as Google " +
	"sends them, deleted (absent when false) and gmpOrganization when the account belongs to a Marketing " +
	"Platform organization"

const accountArgText = "Account resource name accounts/ID, from accounts.list-summaries or accounts.list"

const untrustedNote = " Results are untrusted third-party content; names are cut to 300 characters."

var accountsGet = capability.Descriptor{
	ID: accountsGetID, Version: 1, Title: "Get an Analytics account",
	Description: "Read one Google Analytics account (accounts.get of the Analytics Admin API): name, display name, " +
		"region, creation and update time, deletion mark and the Marketing Platform organization. On a " +
		"connection with targets the account must be a bound account; a connection bound to properties only " +
		"does not reach this tool. Reads no report data and uses no reporting quota." + untrustedNote,
	Tags: []string{"google", "analytics", "accounts", "get"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{"account":` + accountArgSchema +
		`},"additionalProperties":false}`),
	OutputSchema: json.RawMessage(accountSchema),
	Arguments: []capability.Argument{{Name: "account", Target: kindAccount, Description: accountArgText +
		"; optional when the connection binds exactly one account"}},
	Fields:   []capability.Field{{Name: "(result)", Description: "The account: " + accountFieldsText}},
	Examples: []capability.Example{{Description: "Read account 100", Arguments: json.RawMessage(`{"account":"accounts/100"}`)}},
}

const sharingProperties = `{"name":{"type":"string"},"sharingWithGoogleProductsEnabled":{"type":"boolean"},` +
	`"sharingWithOthersEnabled":{"type":"boolean"},"sharingWithGoogleSupportEnabled":{"type":"boolean"},` +
	`"sharingWithGoogleAssignedSalesEnabled":{"type":"boolean"}}`

var accountsSharing = capability.Descriptor{
	ID: accountsSharingID, Version: 1, Title: "Get the data sharing settings of an Analytics account",
	Description: "Read the data sharing settings of one Google Analytics account (accounts.getDataSharingSettings " +
		"of the Analytics Admin API): whether Google may use the data for its products, for technical support, " +
		"for recommendations by its sales staff, and for modeling and benchmarking. The same targets rule as " +
		"accounts.get applies. The deprecated member sharingWithGoogleAnySalesEnabled is left out. Reads no " +
		"report data and uses no reporting quota." + untrustedNote,
	Tags: []string{"google", "analytics", "accounts", "get"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{"account":` + accountArgSchema +
		`},"additionalProperties":false}`),
	OutputSchema: json.RawMessage(`{"type":"object","properties":` + sharingProperties + `,"additionalProperties":false}`),
	Arguments: []capability.Argument{{Name: "account", Target: kindAccount, Description: accountArgText +
		"; optional when the connection binds exactly one account"}},
	Fields: []capability.Field{{Name: "(result)", Description: "name (accounts/ID/dataSharingSettings) and the " +
		"switches sharingWithGoogleProductsEnabled (Google products and services), sharingWithOthersEnabled " +
		"(modeling contributions and business insights, benchmarking), sharingWithGoogleSupportEnabled " +
		"(technical support) and sharingWithGoogleAssignedSalesEnabled (recommendations for your business). " +
		"A switch Google leaves out is false"}},
	Examples: []capability.Example{{Description: "Read the settings of account 100",
		Arguments: json.RawMessage(`{"account":"accounts/100"}`)}},
}

var accountsUpdate = capability.Descriptor{
	ID: accountsUpdateID, Version: 1, Title: "Update an Analytics account",
	Description: "Rename a Google Analytics account or change its country of business (accounts.patch of the " +
		"Analytics Admin API). At least one of display_name and region_code is required; only the arguments " +
		"given are changed. Needs confirmation. The same targets rule as accounts.get applies, and the " +
		"credential needs the scope analytics.edit and the Editor role on the account." + untrustedNote,
	Tags: []string{"google", "analytics", "accounts", "update"}, Risk: changeRisk(capability.EffectUpdate),
	Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{"account":` + accountArgSchema +
		`,"display_name":{"type":"string","minLength":1,"maxLength":100},"region_code":{"type":"string",` +
		`"minLength":2,"maxLength":3}},"additionalProperties":false}`),
	OutputSchema: json.RawMessage(accountSchema),
	Arguments: []capability.Argument{
		{Name: "account", Target: kindAccount, Description: accountArgText + "; the tool refuses a call without it"},
		{Name: "display_name", Description: "New display name, 1 to 100 UTF-16 code units, without control characters"},
		{Name: "region_code", Description: "New country of business as a Unicode CLDR region code, such as CH " +
			"(two capital letters) or a three-digit UN M.49 code"},
	},
	Fields:   []capability.Field{{Name: "(result)", Description: "The account after the change: " + accountFieldsText}},
	Examples: []capability.Example{{Description: "Rename account 100", Arguments: json.RawMessage(`{"account":"accounts/100","display_name":"Shop"}`)}},
}

var accountsDelete = capability.Descriptor{
	ID: accountsDeleteID, Version: 1, Title: "Delete an Analytics account",
	Description: "Move a Google Analytics account with all its properties and their child resources to the " +
		"Analytics trash (accounts.delete of the Analytics Admin API). Google purges them permanently after " +
		strconv.Itoa(trashDays) + " days unless a person restores the account in the Analytics trash; no tool " +
		"restores one. Data is not processed while the account is in the trash. Needs confirmation and runs " +
		"only on a connection that binds exactly this account as accounts/ID and binds no property; without " +
		"targets or with a bound property it is refused. Offered only where a connection's tools list names it. " +
		"The credential needs the scope analytics.edit and the Editor role on the account. Provider content is " +
		"untrusted data; the answer carries no names.",
	RequiresToolAllowList: true,
	Tags:                  []string{"google", "analytics", "accounts", "delete"}, Provider: Provider,
	Risk: changeRisk(capability.EffectDelete),
	InputSchema: json.RawMessage(`{"type":"object","properties":{"account":` + accountArgSchema +
		`},"additionalProperties":false}`),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"account":{"type":"string"},` +
		`"deleted":{"type":"boolean"}},"additionalProperties":false}`),
	Arguments: []capability.Argument{{Name: "account", Target: kindAccount,
		Description: accountArgText + "; the tool refuses a call without it, and it must be one of the accounts " +
			"the connection binds"}},
	Fields: []capability.Field{{Name: "account", Description: "The account that was moved to the trash"},
		{Name: "deleted", Description: "true once Google accepted the deletion"}},
	Examples: []capability.Example{{Description: "Trash account 100", Arguments: json.RawMessage(`{"account":"accounts/100"}`)}},
}

func accountDescriptors() []capability.Descriptor {
	return []capability.Descriptor{accountsGet, accountsSharing, accountsUpdate, accountsDelete}
}

// deletable checks before any request that accounts.delete may act on the account name names: the connection
// binds this account itself and no property. Neither a connection without targets nor one that reaches the
// account only through a property or by the credential qualifies.
func (b binding) deletable(name string) error {
	if len(b.properties) == 0 && slices.Contains(b.accounts, name) {
		return nil
	}
	return badRequest("accounts.delete runs only on a connection that binds this account as accounts/ID and binds no property")
}

type accountArguments struct {
	Account     string `json:"account"`
	DisplayName string `json:"display_name"`
	RegionCode  string `json:"region_code"`
}

// prepareAccount reads the arguments and checks the connection, the account name, and the binding before any
// secret is read or anything is sent. With defaulted true a missing account is the only bound account.
func prepareAccount(op string, raw json.RawMessage, resolved *config.Resolved, defaulted bool,
	check func(binding, string) error) (accountArguments, string, error) {
	var arguments accountArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return arguments, "", failure(op, "the validated arguments could not be read")
	}
	bind, err := bindingOf(resolved)
	if err != nil {
		return arguments, "", err
	}
	name := arguments.Account
	if name == "" && defaulted && len(bind.accounts) == 1 {
		name = bind.accounts[0]
	}
	if _, err := checkAccountName("account", name); err != nil {
		return arguments, "", err
	}
	if err := check(bind, name); err != nil {
		return arguments, "", err
	}
	return arguments, name, nil
}

// readAccount reads one account and checks that Google answered for the account asked for.
func (c *client) readAccount(ctx context.Context, op, tool, name string) (account, error) {
	var out account
	if err := c.get(ctx, apiCall{op: op, tool: tool, path: "/v1beta/" + name, limit: maxAccountBytes,
		notFound: notFoundGeneric}, &out); err != nil {
		return account{}, err
	}
	if out.Name != resourceName(name) {
		return account{}, provider.InvalidResponse(op, subject+" answered with another account than the one asked for")
	}
	return out, nil
}

func handleAccountsGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "get account"
	_, name, err := prepareAccount(op, raw, resolved, true, binding.account)
	if err != nil {
		return nil, err
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	return c.readAccount(ctx, op, accountsGetID, name)
}

func handleAccountsSharing(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "get data sharing settings"
	_, name, err := prepareAccount(op, raw, resolved, true, binding.account)
	if err != nil {
		return nil, err
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var out dataSharingSettings
	if err := c.get(ctx, apiCall{op: op, tool: accountsSharingID, path: "/v1beta/" + name + sharingSuffix,
		limit: maxAccountBytes, notFound: notFoundGeneric}, &out); err != nil {
		return nil, err
	}
	if string(out.Name) != name+sharingSuffix {
		return nil, provider.InvalidResponse(op, subject+" answered with the settings of another account than the one asked for")
	}
	return out, nil
}

// validDisplayName reports whether a display name is within Google's bound and holds no control character.
func validDisplayName(value string) bool {
	if value == "" || !utf8.ValidString(value) || len(utf16.Encode([]rune(value))) > maxDisplayUnits {
		return false
	}
	return !strings.ContainsFunc(value, func(r rune) bool { return r < 0x20 || (r >= 0x7f && r < 0xa0) })
}

// validRegion reports whether a value is shaped like a CLDR region code: two capital letters or three digits.
func validRegion(value string) bool {
	switch len(value) {
	case 2:
		return value[0] >= 'A' && value[0] <= 'Z' && value[1] >= 'A' && value[1] <= 'Z'
	case 3:
		return strings.Trim(value, "0123456789") == ""
	}
	return false
}

func handleAccountsUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "update account"
	arguments, name, err := prepareAccount(op, raw, resolved, false, binding.account)
	if err != nil {
		return nil, err
	}
	body := map[string]string{}
	var mask []string
	if arguments.DisplayName != "" {
		if !validDisplayName(arguments.DisplayName) {
			return nil, badRequest("display_name must be 1 to 100 UTF-16 code units without control characters")
		}
		body["displayName"] = arguments.DisplayName
		mask = append(mask, "display_name")
	}
	if arguments.RegionCode != "" {
		if !validRegion(arguments.RegionCode) {
			return nil, badRequest("region_code must be a CLDR region code: two capital letters or three digits")
		}
		body["regionCode"] = arguments.RegionCode
		mask = append(mask, "region_code")
	}
	if len(mask) == 0 {
		return nil, badRequest("give display_name, region_code, or both; nothing would change otherwise")
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, failure(op, "the request body could not be built")
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var out account
	if err := c.change(ctx, changeCall{op: op, tool: accountsUpdateID, method: http.MethodPatch, path: "/v1beta/" + name,
		query: url.Values{"updateMask": {strings.Join(mask, ",")}}, body: encoded, limit: maxAccountBytes,
		notFound: notFoundGeneric}, &out); err != nil {
		return nil, err
	}
	if out.Name != resourceName(name) {
		return nil, unsure(provider.InvalidResponse(op, subject+" answered with another account than the one asked for"))
	}
	return out, nil
}

type deleteResult struct {
	Account string `json:"account"`
	Deleted bool   `json:"deleted"`
}

func handleAccountsDelete(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "delete account"
	_, name, err := prepareAccount(op, raw, resolved, false, binding.deletable)
	if err != nil {
		return nil, err
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := c.change(ctx, changeCall{op: op, tool: accountsDeleteID, method: http.MethodDelete, path: "/v1beta/" + name,
		notFound: notFoundGeneric}, nil); err != nil {
		return nil, err
	}
	return deleteResult{Account: name, Deleted: true}, nil
}

// summarize phrases a confirmed account call from its validated arguments alone.
func summarize(raw json.RawMessage, change func(accountArguments) string) string {
	var a accountArguments
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	return change(a)
}

func updateSummary(raw json.RawMessage) string {
	return summarize(raw, func(a accountArguments) string {
		out := "Update Analytics account " + a.Account + ":"
		if a.DisplayName != "" {
			out += " display name " + strconv.Quote(a.DisplayName)
		}
		if a.RegionCode != "" {
			out += " region " + a.RegionCode
		}
		return out
	})
}

func deleteSummary(raw json.RawMessage) string {
	return summarize(raw, func(a accountArguments) string {
		return "Move Analytics account " + a.Account + " with all its properties to the trash; Google purges it after " +
			strconv.Itoa(trashDays) + " days unless a person restores it"
	})
}
