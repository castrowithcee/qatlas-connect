package googlecloud

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	serviceAccountKeysListID = Provider + ".service-account-keys.list"
	serviceAccountKeysGetID  = Provider + ".service-account-keys.get"
)

// maxKeyRows bounds one key list. Google allows ten user-managed keys per account and lists the system-managed
// ones beside them, so this only guards against an answer of an untrusted size.
const maxKeyRows = 200

var (
	keyIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)
	keyTypes     = []string{"USER_MANAGED", "SYSTEM_MANAGED"}
)

// keyWire is the part of a ServiceAccountKey that Qatlas reads. publicKeyData and privateKeyData are left out
// on purpose: decoding drops them, so key material never reaches a result even if Google sends it.
type keyWire struct {
	Name            string `json:"name"`
	KeyAlgorithm    string `json:"keyAlgorithm"`
	KeyOrigin       string `json:"keyOrigin"`
	KeyType         string `json:"keyType"`
	ValidAfterTime  string `json:"validAfterTime"`
	ValidBeforeTime string `json:"validBeforeTime"`
	Disabled        bool   `json:"disabled"`
	DisableReason   string `json:"disableReason"`
	ExtendedStatus  []struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	} `json:"extendedStatus"`
}

type keyStatusResult struct {
	Key   string `json:"key,omitempty"`
	Value string `json:"value,omitempty"`
}

type keyResult struct {
	Name            string            `json:"name"`
	KeyAlgorithm    string            `json:"keyAlgorithm,omitempty"`
	KeyOrigin       string            `json:"keyOrigin,omitempty"`
	KeyType         string            `json:"keyType,omitempty"`
	ValidAfterTime  string            `json:"validAfterTime,omitempty"`
	ValidBeforeTime string            `json:"validBeforeTime,omitempty"`
	Disabled        bool              `json:"disabled,omitempty"`
	DisableReason   string            `json:"disableReason,omitempty"`
	ExtendedStatus  []keyStatusResult `json:"extendedStatus,omitempty"`
}

// check refuses a key that is not one of an account of project. The account segment is not compared with the
// argument, because Google may name the account by email or by unique ID.
func (k keyWire) check(op, project string) error {
	rest, ok := strings.CutPrefix(k.Name, prefixProject+project+"/serviceAccounts/")
	if !ok {
		return invalidRow(op)
	}
	account, id, found := strings.Cut(rest, "/keys/")
	if !found || account == "" || id == "" || strings.Contains(account, "/") || strings.Contains(id, "/") {
		return invalidRow(op)
	}
	return nil
}

func (k keyWire) result() keyResult {
	out := keyResult{Name: boundedText(k.Name, maxText), KeyAlgorithm: boundedText(k.KeyAlgorithm, maxNameText),
		KeyOrigin: boundedText(k.KeyOrigin, maxNameText), KeyType: boundedText(k.KeyType, maxNameText),
		ValidAfterTime:  boundedText(k.ValidAfterTime, maxNameText),
		ValidBeforeTime: boundedText(k.ValidBeforeTime, maxNameText), Disabled: k.Disabled,
		DisableReason: boundedText(k.DisableReason, maxNameText)}
	for _, status := range k.ExtendedStatus[:min(len(k.ExtendedStatus), maxListItems)] {
		out.ExtendedStatus = append(out.ExtendedStatus, keyStatusResult{Key: boundedText(status.Key, maxNameText),
			Value: boundedText(status.Value, maxText)})
	}
	return out
}

// --- shared descriptor parts ---

const (
	keyOutputSchema = `{"type":"object","properties":{"name":{"type":"string"},"keyAlgorithm":{"type":"string"},` +
		`"keyOrigin":{"type":"string"},"keyType":{"type":"string"},"validAfterTime":{"type":"string"},` +
		`"validBeforeTime":{"type":"string"},"disabled":{"type":"boolean"},"disableReason":{"type":"string"},` +
		`"extendedStatus":{"type":"array","items":{"type":"object","properties":{"key":{"type":"string"},` +
		`"value":{"type":"string"}},"additionalProperties":false}}},"required":["name"],"additionalProperties":false}`

	keyFieldText = "{name, keyAlgorithm, keyOrigin, keyType, validAfterTime, validBeforeTime, disabled, " +
		"disableReason, extendedStatus}"
)

var keyFields = []capability.Field{
	{Name: "name", Description: "Resource name, projects/PROJECT_ID/serviceAccounts/ACCOUNT/keys/KEY_ID"},
	{Name: "keyAlgorithm", Description: "KEY_ALG_RSA_1024 or KEY_ALG_RSA_2048"},
	{Name: "keyOrigin", Description: "USER_PROVIDED or GOOGLE_PROVIDED"},
	{Name: "keyType", Description: "USER_MANAGED or SYSTEM_MANAGED"},
	{Name: "validAfterTime", Description: "The key can be used after this time (RFC 3339)"},
	{Name: "validBeforeTime", Description: "The key can be used until this time (RFC 3339)"},
	{Name: "disabled", Description: "True for a disabled key; absent otherwise"},
	{Name: "disableReason", Description: "Why a disabled key was disabled, such as " +
		"SERVICE_ACCOUNT_KEY_DISABLE_REASON_EXPOSED; absent for an enabled key"},
	{Name: "extendedStatus", Description: "Permanent notes about the key, each {key, value}, such as a detected " +
		"exposure; untrusted text, cut"},
}

var keyIDArgument = capability.Argument{Name: "key_id", Required: true, Description: "ID of the key, the last " +
	"segment of its name; never a resource name or a path"}

// keyScope opens a key tool: the connection, the targets, and the proof of the project of the account.
func keyScope(ctx context.Context, op, tool string, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, a accountArguments) (*client, string, error) {
	if a.Account == "" {
		return nil, "", badRequest("account must be the email or the numeric unique ID of the service account")
	}
	return openAccountScope(ctx, op, tool, resolved, secrets, red, a)
}

// --- service-account-keys.list ---

var serviceAccountKeysList = capability.Descriptor{
	ID: serviceAccountKeysListID, Version: 1, Title: "List the keys of a Google Cloud service account",
	Description: "List the metadata of the keys of one service account, optionally only the user-managed or only " +
		"the system-managed ones: name, algorithm, origin, type, validity period, whether the key is disabled " +
		"and why, and its extended status. Key material is never returned, neither the private key nor the " +
		"public key, and no tool of this provider creates or uploads a key. The project must lie in the " +
		"targets of the connection, and an account of another project is refused. The answer is not paged.",
	Tags: []string{"google", "cloud", "iam", "service-accounts", "keys"}, Risk: iamRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + projectIDProp + `,` + accountProp + `,` +
		`"key_types":{"type":"array","minItems":1,"maxItems":2,"uniqueItems":true,"items":{"type":"string",` +
		`"enum":["USER_MANAGED","SYSTEM_MANAGED"]}}},"required":["project_id","account"],` +
		`"additionalProperties":false}`),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"keys":{"type":"array","items":` + keyOutputSchema +
		`}},"required":["keys"],"additionalProperties":false}`),
	Arguments: []capability.Argument{projectIDArgument, accountArgument,
		{Name: "key_types", Description: "Only keys of these types: USER_MANAGED, SYSTEM_MANAGED, or both; " +
			"without it all keys are listed"}},
	Fields: []capability.Field{{Name: "keys", Description: "Keys of the account, each " + keyFieldText}},
	Examples: []capability.Example{{
		Description: "List the user-managed keys of a service account",
		Arguments: json.RawMessage(`{"project_id":"example-project-123",` +
			`"account":"deploy@example-project-123.iam.gserviceaccount.com","key_types":["USER_MANAGED"]}`),
	}},
}

func handleServiceAccountKeysList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list service account keys"
	var arguments struct {
		accountArguments
		KeyTypes []string `json:"key_types"`
	}
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	query := url.Values{}
	seen := map[string]bool{}
	for _, t := range arguments.KeyTypes {
		if !slices.Contains(keyTypes, t) || seen[t] {
			return nil, badRequest("key_types must name USER_MANAGED, SYSTEM_MANAGED, or both, each once")
		}
		seen[t] = true
		query.Add("keyTypes", t)
	}
	c, project, err := keyScope(ctx, op, serviceAccountKeysListID, resolved, secrets, red, arguments.accountArguments)
	if err != nil {
		return nil, err
	}
	var members map[string]json.RawMessage
	if err := c.get(ctx, op, serviceAccountKeysListID, serviceIAM, accountPath(project, arguments.Account)+"/keys",
		query, &members); err != nil {
		return nil, err
	}
	rows, _, err := decodePage[keyWire](op, members, "keys", maxKeyRows)
	if err != nil {
		return nil, err
	}
	out := make([]keyResult, 0, len(rows))
	for _, row := range rows {
		if err := row.check(op, project); err != nil {
			return nil, err
		}
		out = append(out, row.result())
	}
	return map[string]any{"keys": out}, nil
}

// --- service-account-keys.get ---

var serviceAccountKeysGet = capability.Descriptor{
	ID: serviceAccountKeysGetID, Version: 1, Title: "Get a key of a Google Cloud service account",
	Description: "Read the metadata of one key of a service account: name, algorithm, origin, type, validity " +
		"period, whether the key is disabled and why, and its extended status. Key material is never " +
		"returned: the public key (publicKeyData) is dropped even where Google sends it, and the request never " +
		"asks for it. The project must lie in the targets of the connection, and an account of another " +
		"project is refused.",
	Tags: []string{"google", "cloud", "iam", "service-accounts", "keys"}, Risk: iamRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + projectIDProp + `,` + accountProp + `,` +
		`"key_id":{"type":"string","minLength":1,"maxLength":64,"pattern":"^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$"}},` +
		`"required":["project_id","account","key_id"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(keyOutputSchema),
	Arguments:    []capability.Argument{projectIDArgument, accountArgument, keyIDArgument},
	Fields:       keyFields,
	Examples: []capability.Example{{
		Description: "Read the metadata of one key",
		Arguments: json.RawMessage(`{"project_id":"example-project-123",` +
			`"account":"deploy@example-project-123.iam.gserviceaccount.com","key_id":"0123456789abcdef0123456789abcdef01234567"}`),
	}},
}

func handleServiceAccountKeysGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "get service account key"
	var arguments struct {
		accountArguments
		KeyID string `json:"key_id"`
	}
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if !keyIDPattern.MatchString(arguments.KeyID) {
		return nil, badRequest("key_id must be the ID of the key, letters, digits, - and _")
	}
	c, project, err := keyScope(ctx, op, serviceAccountKeysGetID, resolved, secrets, red, arguments.accountArguments)
	if err != nil {
		return nil, err
	}
	// No publicKeyType is sent, so Google returns metadata only; the wire type drops the data fields anyway.
	var row keyWire
	if err := c.get(ctx, op, serviceAccountKeysGetID, serviceIAM,
		accountPath(project, arguments.Account)+"/keys/"+url.PathEscape(arguments.KeyID), nil, &row); err != nil {
		return nil, err
	}
	if err := row.check(op, project); err != nil {
		return nil, err
	}
	if !strings.HasSuffix(row.Name, "/keys/"+arguments.KeyID) {
		return nil, invalidRow(op)
	}
	return row.result(), nil
}
