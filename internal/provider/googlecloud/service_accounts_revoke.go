package googlecloud

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	serviceAccountKeysDisableID = Provider + ".service-account-keys.disable"
	serviceAccountKeysEnableID  = Provider + ".service-account-keys.enable"
	serviceAccountKeysDeleteID  = Provider + ".service-account-keys.delete"
	serviceAccountsDeleteID     = Provider + ".service-accounts.delete"
)

// The disable and enable requests of discovery iam v1 (revision 20261106) have no members, so the tools take no
// argument beyond the resource and send an empty body. undelete, key creation, and upload are not tools.
const (
	keyIDProp = `"key_id":{"type":"string","minLength":1,"maxLength":64,"pattern":"^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$"}`

	revokeNote = " Needs the confirmation of a person for each call. The credential needs permission on the " +
		"project in IAM, which Google decides. No key, token, or signature is created and none is returned."

	keyChangeSchema = `{"type":"object","properties":{"projectId":{"type":"string"},"account":{"type":"string"},` +
		`"keyId":{"type":"string"},"disabled":{"type":"boolean"}},"required":["projectId","account","keyId","disabled"],` +
		`"additionalProperties":false}`
	keyDeletedSchema = `{"type":"object","properties":{"projectId":{"type":"string"},"account":{"type":"string"},` +
		`"keyId":{"type":"string"},"deleted":{"type":"boolean"}},"required":["projectId","account","keyId","deleted"],` +
		`"additionalProperties":false}`
	accountDeletedSchema = `{"type":"object","properties":{"projectId":{"type":"string"},"account":{"type":"string"},` +
		`"deleted":{"type":"boolean"}},"required":["projectId","account","deleted"],"additionalProperties":false}`
)

type keyChangeResult struct {
	ProjectID string `json:"projectId"`
	Account   string `json:"account"`
	KeyID     string `json:"keyId"`
	Disabled  bool   `json:"disabled"`
}

type keyDeletedResult struct {
	ProjectID string `json:"projectId"`
	Account   string `json:"account"`
	KeyID     string `json:"keyId"`
	Deleted   bool   `json:"deleted"`
}

type accountDeletedResult struct {
	ProjectID string `json:"projectId"`
	Account   string `json:"account"`
	Deleted   bool   `json:"deleted"`
}

var (
	keyResultFields = []capability.Field{{Name: "projectId", Description: "Project that owns the account"},
		{Name: "account", Description: "Account as given"}, {Name: "keyId", Description: "Key as given"}}
	revokeTags = []string{"google", "cloud", "iam", "service-accounts", "keys"}
)

func keySwitch(id, title, effect string, disable bool) capability.Descriptor {
	verb, state := "Re-enable", "enabled"
	if disable {
		verb, state = "Disable", "disabled"
	}
	return capability.Descriptor{
		ID: id, Version: 1, Title: title,
		Description: verb + " one key of a service account (projects.serviceAccounts.keys." + effect + " of IAM v1). " +
			"The key is " + state + " afterwards; the account and its other keys stay as they are. Repeating the call " +
			"is harmless. The project must lie in the targets of the connection, and an account of another project " +
			"is refused before any request. Google answers with an empty body, so the result is the project, the " +
			"account and key as given, and the new state." + revokeNote,
		Tags: append(append([]string{}, revokeTags...), effect), Provider: Provider,
		Risk: accountWriteRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
		InputSchema: json.RawMessage(`{"type":"object","properties":{` + projectIDProp + `,` + accountProp + `,` +
			keyIDProp + `},"required":["project_id","account","key_id"],"additionalProperties":false}`),
		OutputSchema: json.RawMessage(keyChangeSchema),
		Arguments:    []capability.Argument{projectIDArgument, accountArgument, keyIDArgument},
		Fields: append(append([]capability.Field{}, keyResultFields...),
			capability.Field{Name: "disabled", Description: "New state: true after disable, false after enable"}),
		Examples: []capability.Example{{Description: title, Arguments: json.RawMessage(
			`{"project_id":"example-project-123","account":"deploy@example-project-123.iam.gserviceaccount.com",` +
				`"key_id":"0123456789abcdef0123456789abcdef01234567"}`)}},
	}
}

var (
	serviceAccountKeysDisable = keySwitch(serviceAccountKeysDisableID, "Disable a key of a Google Cloud service account",
		"disable", true)
	serviceAccountKeysEnable = keySwitch(serviceAccountKeysEnableID, "Enable a key of a Google Cloud service account",
		"enable", false)

	serviceAccountKeysDelete = capability.Descriptor{
		ID: serviceAccountKeysDeleteID, Version: 1, Title: "Delete a key of a Google Cloud service account",
		Description: "Delete one key of a service account for good (projects.serviceAccounts.keys.delete of IAM v1). " +
			"This cannot be undone: a deleted key does not come back, and disable is the way to block a key for " +
			"a while. Deleting a key does not revoke short-lived credentials that were issued from it. Google " +
			"refuses system-managed keys. The project must lie in the targets of the connection, and an account " +
			"of another project is refused before any request. Google answers with an empty body, so the result " +
			"is the project, the account and key as given, and deleted." + revokeNote,
		Tags: append(append([]string{}, revokeTags...), "delete"), Provider: Provider,
		Risk: accountWriteRisk(capability.EffectDelete, capability.IdempotencyUnknown),
		InputSchema: json.RawMessage(`{"type":"object","properties":{` + projectIDProp + `,` + accountProp + `,` +
			keyIDProp + `},"required":["project_id","account","key_id"],"additionalProperties":false}`),
		OutputSchema: json.RawMessage(keyDeletedSchema),
		Arguments:    []capability.Argument{projectIDArgument, accountArgument, keyIDArgument},
		Fields: append(append([]capability.Field{}, keyResultFields...),
			capability.Field{Name: "deleted", Description: "True once Google deleted the key"}),
		Examples: []capability.Example{{Description: "Delete a key", Arguments: json.RawMessage(
			`{"project_id":"example-project-123","account":"deploy@example-project-123.iam.gserviceaccount.com",` +
				`"key_id":"0123456789abcdef0123456789abcdef01234567"}`)}},
	}

	serviceAccountsDelete = capability.Descriptor{
		ID: serviceAccountsDeleteID, Version: 1, Title: "Delete a Google Cloud service account",
		Description: "Delete a service account with its keys and role bindings (projects.serviceAccounts.delete of " +
			"IAM v1). Services that use the account fail afterwards. Google may allow a recovery for a limited " +
			"time only, and no tool of this provider undeletes; disable is the way to block an account first. " +
			"Offered only where a connection's tools list names it. The project must lie in the targets of the " +
			"connection, and an email of another project is refused before any request. Google answers with an " +
			"empty body, so the result is the project, the account as given, and deleted." + revokeNote,
		Tags: append(append([]string{}, accountToolTags...), "delete"), Provider: Provider,
		RequiresToolAllowList: true,
		Risk:                  accountWriteRisk(capability.EffectDelete, capability.IdempotencyUnknown),
		InputSchema: json.RawMessage(`{"type":"object","properties":{` + projectIDProp + `,` + accountProp +
			`},"required":["project_id","account"],"additionalProperties":false}`),
		OutputSchema: json.RawMessage(accountDeletedSchema),
		Arguments:    []capability.Argument{projectIDArgument, accountArgument},
		Fields: []capability.Field{{Name: "projectId", Description: "Project that owned the account"},
			{Name: "account", Description: "Account as given"},
			{Name: "deleted", Description: "True once Google deleted the account"}},
		Examples: []capability.Example{{Description: "Delete a service account", Arguments: json.RawMessage(
			`{"project_id":"example-project-123","account":"deploy@example-project-123.iam.gserviceaccount.com"}`)}},
	}
)

type keyArguments struct {
	accountArguments
	KeyID string `json:"key_id"`
}

// keyChange runs the one request of a key tool. The key ID is checked before any I/O, and escaped as a path
// segment so that it cannot address anything but one key.
func keyChange(id, op, method, suffix string, result func(project, account, key string) any) capability.Handler {
	return func(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
		raw json.RawMessage) (any, error) {
		var a keyArguments
		if err := json.Unmarshal(raw, &a); err != nil {
			return nil, failure(op, "the validated arguments could not be read")
		}
		if !keyIDPattern.MatchString(a.KeyID) {
			return nil, badRequest("key_id must be the ID of the key, letters, digits, - and _")
		}
		c, project, err := keyScope(ctx, op, id, resolved, secrets, red, a.accountArguments)
		if err != nil {
			return nil, err
		}
		var body any
		if method == http.MethodPost {
			body = struct{}{}
		}
		var answer map[string]json.RawMessage
		if err := c.changeAccount(ctx, op, id, method,
			accountPath(project, a.Account)+"/keys/"+url.PathEscape(a.KeyID)+suffix, body, &answer); err != nil {
			return nil, err
		}
		return result(project, boundedText(a.Account, maxNameText), a.KeyID), nil
	}
}

var (
	handleServiceAccountKeysDisable = keyChange(serviceAccountKeysDisableID, "disable service account key",
		http.MethodPost, ":disable", func(project, account, key string) any {
			return keyChangeResult{ProjectID: project, Account: account, KeyID: key, Disabled: true}
		})
	handleServiceAccountKeysEnable = keyChange(serviceAccountKeysEnableID, "enable service account key",
		http.MethodPost, ":enable", func(project, account, key string) any {
			return keyChangeResult{ProjectID: project, Account: account, KeyID: key}
		})
	handleServiceAccountKeysDelete = keyChange(serviceAccountKeysDeleteID, "delete service account key",
		http.MethodDelete, "", func(project, account, key string) any {
			return keyDeletedResult{ProjectID: project, Account: account, KeyID: key, Deleted: true}
		})
)

func handleServiceAccountsDelete(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "delete service account"
	var a accountArguments
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	c, project, err := keyScope(ctx, op, serviceAccountsDeleteID, resolved, secrets, red, a)
	if err != nil {
		return nil, err
	}
	var answer map[string]json.RawMessage
	if err := c.changeAccount(ctx, op, serviceAccountsDeleteID, http.MethodDelete, accountPath(project, a.Account), nil,
		&answer); err != nil {
		return nil, err
	}
	return accountDeletedResult{ProjectID: project, Account: boundedText(a.Account, maxNameText), Deleted: true}, nil
}

// revokeSummary phrases a revoking change from its validated arguments.
func revokeSummary(tool string) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		var a keyArguments
		if json.Unmarshal(raw, &a) != nil {
			return ""
		}
		project := "project " + boundedText(a.ProjectID, maxNameText)
		account := "service account " + boundedText(a.Account, maxNameText)
		key := "key " + strconv.Quote(boundedText(a.KeyID, maxNameText))
		switch tool {
		case serviceAccountKeysDisableID:
			return "Disable " + key + " of " + account + " in " + project + ": it can no longer be used until it is enabled again"
		case serviceAccountKeysEnableID:
			return "Enable " + key + " of " + account + " in " + project + ": it can be used again"
		case serviceAccountKeysDeleteID:
			return "Delete " + key + " of " + account + " in " + project + " for good: this cannot be undone"
		}
		return "Delete " + account + " in " + project + " with its keys and role bindings: services that use it fail " +
			"afterwards, and Google may allow a recovery only for a limited time"
	}
}
