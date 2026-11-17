package googlecloud

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/provider/googleapi"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	serviceAccountsCreateID  = Provider + ".service-accounts.create"
	serviceAccountsPatchID   = Provider + ".service-accounts.patch"
	serviceAccountsDisableID = Provider + ".service-accounts.disable"
	serviceAccountsEnableID  = Provider + ".service-accounts.enable"
)

// Bounds of the user-assignable members, from discovery iam v1 (revision 20261106): UTF-8 bytes, not characters.
const (
	maxAccountDisplayBytes     = 100
	maxAccountDescriptionBytes = 256
	// summaryTextRunes caps what a confirmation shows of untrusted text.
	summaryTextRunes = 80
)

// accountIDPattern is the account ID of create: 6 to 30 characters of RFC 1035, as the discovery says.
var accountIDPattern = regexp.MustCompile(`^[a-z][-a-z0-9]{4,28}[a-z0-9]$`)

// accountWriteRisk is the risk of a changing service account tool. The result names principals.
func accountWriteRisk(effect capability.Effect, idempotency capability.Idempotency) capability.Risk {
	return capability.Risk{Effect: effect, Idempotency: idempotency, Confirmation: capability.ConfirmationRequired,
		OpenWorld: true, DataSensitivity: accessSensitivity}
}

// accountChangeUnsure marks a failure after which the change may or may not have happened.
func accountChangeUnsure(failed *provider.Error) *provider.Error {
	failed.Message += "; the outcome of the change is unknown, so read the service account before changing it again"
	return failed
}

// changeAccount sends the one request of a changing service account tool and decodes the JSON object answer
// into out. The change is never sent twice: provider.TokenSource.Mutate only drops the token on a 401. A failure
// that may follow a delivered request says that the outcome is unknown.
func (c *client) changeAccount(ctx context.Context, op, tool, method, path string, body any, out any) error {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return failure(op, "the request could not be built")
		}
	}
	endpoint := serviceOrigins[serviceIAM] + path
	build := func(access string) (*http.Request, error) {
		var reader io.Reader
		if payload != nil {
			reader = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
		if err != nil {
			return nil, err
		}
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set("Authorization", "Bearer "+access)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", userAgent)
		return req, nil
	}
	if err := c.limiter.Wait(ctx); err != nil {
		return provider.Waited(op, subject, err)
	}
	response, err := c.source.Mutate(ctx, c.http, tool, build)
	if err != nil {
		var failed *provider.Error
		if errors.As(err, &failed) {
			return err
		}
		return accountChangeUnsure(provider.Transport(op, subject, err))
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		raw := provider.ReadErrorBody(response.Body)
		if response.StatusCode == http.StatusTooManyRequests {
			c.limiter.HoldFor(min(max(provider.RetryAfter(response.Header), time.Second), maxHold))
		}
		if response.StatusCode == http.StatusConflict {
			if state, _ := googleapi.StatusOf(raw); state == "ALREADY_EXISTS" {
				return &provider.Error{Class: provider.ClassProviderError, Op: op,
					Message: subject + " refused the request: a service account with this account_id already exists " +
						"in the project, or existed recently and its ID is not yet free",
					Original: provider.ErrorOriginal(response.StatusCode, raw, googleapi.Fields)}
			}
		}
		mapped := googleError(op, response.StatusCode, raw)
		if response.StatusCode >= 500 {
			accountChangeUnsure(mapped)
		}
		return mapped
	}
	if mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type")); err != nil ||
		mediaType != "application/json" {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<10))
		return accountChangeUnsure(provider.InvalidResponse(op, subject+" returned an answer that is not JSON"))
	}
	if failed := provider.ReadJSON(op, subject, response.Body, maxResponseBytes, out); failed != nil {
		return accountChangeUnsure(failed)
	}
	return nil
}

// checkAccountText refuses text that is not valid UTF-8, holds a control character, or exceeds limit bytes.
// The refusal names the argument, never the value. An empty value is allowed: it clears the member.
func checkAccountText(name, value string, limit int) error {
	if !utf8.ValidString(value) {
		return badRequest(name + " must be valid UTF-8 text")
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return badRequest(name + " must not hold control characters")
		}
	}
	if len(value) > limit {
		return badRequest(name + " must be at most " + strconv.Itoa(limit) + " UTF-8 bytes")
	}
	return nil
}

// writeAccountArguments are the arguments of the four changing tools; each tool reads its own members.
type writeAccountArguments struct {
	accountArguments
	AccountID   string  `json:"account_id"`
	DisplayName *string `json:"display_name"`
	Description *string `json:"description"`
}

func (a writeAccountArguments) checkText() error {
	if a.DisplayName != nil {
		if err := checkAccountText("display_name", *a.DisplayName, maxAccountDisplayBytes); err != nil {
			return err
		}
	}
	if a.Description != nil {
		return checkAccountText("description", *a.Description, maxAccountDescriptionBytes)
	}
	return nil
}

// accountBody is the ServiceAccount of a request: only the members the caller set, never an output-only one.
type accountBody struct {
	DisplayName *string `json:"displayName,omitempty"`
	Description *string `json:"description,omitempty"`
}

// patchBody returns the ServiceAccount and the update mask of the members that are set.
func (a writeAccountArguments) patchBody() (accountBody, string) {
	var body accountBody
	var mask []string
	if a.DisplayName != nil {
		body.DisplayName, mask = a.DisplayName, append(mask, "displayName")
	}
	if a.Description != nil {
		body.Description, mask = a.Description, append(mask, "description")
	}
	return body, strings.Join(mask, ",")
}

// ownsAnswer proves that an answer of a change is the account that was addressed, in the checked project.
// Google may leave members out of a patch answer, so only the ones present are compared.
func (r serviceAccountRow) ownsAnswer(op, project, account string) error {
	if r.ProjectID == "" {
		r.ProjectID = project
	}
	if err := r.check(op, project); err != nil {
		return err
	}
	last := r.Name[strings.LastIndex(r.Name, "/")+1:]
	if account != last && account != r.Email && account != r.UniqueID {
		return invalidRow(op)
	}
	return nil
}

func (r serviceAccountRow) resultIn(project string) serviceAccountResult {
	if r.ProjectID == "" {
		r.ProjectID = project
	}
	return r.result()
}

// --- shared descriptor parts ---

const (
	displayNameProp = `"display_name":{"type":"string","maxLength":100}`
	descriptionProp = `"description":{"type":"string","maxLength":256}`

	changeNote = " Needs the confirmation of a person for each call. The credential needs permission on the " +
		"project in IAM, which Google decides. Names and descriptions are untrusted third-party text, cut in the " +
		"confirmation and in the result. No key, token, or signature is created and none is returned; the " +
		"deprecated etag is not returned."

	disabledResultSchema = `{"type":"object","properties":{"projectId":{"type":"string"},"account":{"type":"string"},` +
		`"disabled":{"type":"boolean"}},"required":["projectId","account","disabled"],"additionalProperties":false}`
)

var (
	displayNameArgument = capability.Argument{Name: "display_name", Description: "Display name, at most 100 UTF-8 " +
		"bytes, no control characters. For patch, an empty string clears it"}
	descriptionArgument = capability.Argument{Name: "description", Description: "Description, at most 256 UTF-8 " +
		"bytes, no control characters. For patch, an empty string clears it"}
	accountToolTags = []string{"google", "cloud", "iam", "service-accounts"}
)

// --- service-accounts.create ---

var serviceAccountsCreate = capability.Descriptor{
	ID: serviceAccountsCreateID, Version: 1, Title: "Create a Google Cloud service account",
	Description: "Create a service account in one project (projects.serviceAccounts.create of IAM v1) and return it " +
		"like service-accounts.get. The project must lie in the targets of the connection, which Qatlas proves " +
		"before the request. The new account has no roles and no key: roles are granted only by the IAM binding " +
		"tools and no tool creates a key." + changeNote + " Not idempotent: a repeated call fails with ALREADY_EXISTS " +
		"or, after a delete, may create a second account.",
	Tags: append(accountToolTags, "create"), Provider: Provider,
	Risk: accountWriteRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent),
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + projectIDProp + `,"account_id":` +
		`{"type":"string","minLength":6,"maxLength":30,"pattern":"^[a-z][-a-z0-9]{4,28}[a-z0-9]$"},` +
		displayNameProp + `,` + descriptionProp + `},"required":["project_id","account_id"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(serviceAccountSchema),
	Arguments: []capability.Argument{projectIDArgument,
		{Name: "account_id", Required: true, Description: "Part of the new email before the @, 6 to 30 characters: " +
			"lowercase letters, digits, and hyphens, starting with a letter and not ending with a hyphen"},
		displayNameArgument, descriptionArgument},
	Fields: serviceAccountFields,
	Examples: []capability.Example{{Description: "Create a service account for deployments",
		Arguments: json.RawMessage(`{"project_id":"example-project-123","account_id":"deploy",` +
			`"display_name":"Deploy"}`)}},
}

func handleServiceAccountsCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "create service account"
	var a writeAccountArguments
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if !accountIDPattern.MatchString(a.AccountID) {
		return nil, badRequest("account_id must be 6 to 30 characters: lowercase letters, digits, and hyphens, " +
			"starting with a letter and ending with a letter or digit")
	}
	if err := a.checkText(); err != nil {
		return nil, err
	}
	c, project, err := openAccountScope(ctx, op, serviceAccountsCreateID, resolved, secrets, red,
		accountArguments{ProjectID: a.ProjectID})
	if err != nil {
		return nil, err
	}
	body := struct {
		AccountID      string      `json:"accountId"`
		ServiceAccount accountBody `json:"serviceAccount"`
	}{a.AccountID, accountBody{DisplayName: a.DisplayName, Description: a.Description}}
	var row serviceAccountRow
	if err := c.changeAccount(ctx, op, serviceAccountsCreateID, http.MethodPost,
		"/v1/projects/"+url.PathEscape(project)+"/serviceAccounts", body, &row); err != nil {
		return nil, err
	}
	if err := row.ownsAnswer(op, project, a.AccountID+"@"+project+".iam.gserviceaccount.com"); err != nil {
		return nil, accountChangeUnsure(err.(*provider.Error))
	}
	return row.resultIn(project), nil
}

// --- service-accounts.patch ---

var serviceAccountsPatch = capability.Descriptor{
	ID: serviceAccountsPatchID, Version: 1, Title: "Change a Google Cloud service account",
	Description: "Change the display name or description of a service account (projects.serviceAccounts.patch of " +
		"IAM v1). Only the arguments that are set are changed and at least one is required; Qatlas sends exactly " +
		"them as the update mask. The project must lie in the targets of the connection, and an email of another " +
		"project is refused before any request. The answer is the account like service-accounts.get; Google may " +
		"leave members out." + changeNote,
	Tags: append(accountToolTags, "update"), Provider: Provider,
	Risk: accountWriteRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + projectIDProp + `,` + accountProp + `,` +
		displayNameProp + `,` + descriptionProp + `},"required":["project_id","account"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(serviceAccountSchema),
	Arguments:    []capability.Argument{projectIDArgument, accountArgument, displayNameArgument, descriptionArgument},
	Fields:       serviceAccountFields,
	Examples: []capability.Example{{Description: "Rename a service account",
		Arguments: json.RawMessage(`{"project_id":"example-project-123",` +
			`"account":"deploy@example-project-123.iam.gserviceaccount.com","display_name":"Deploy CI"}`)}},
}

func handleServiceAccountsPatch(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "change service account"
	var a writeAccountArguments
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	body, mask := a.patchBody()
	if mask == "" {
		return nil, badRequest("set at least one of display_name and description")
	}
	if err := a.checkText(); err != nil {
		return nil, err
	}
	c, project, err := openAccountScope(ctx, op, serviceAccountsPatchID, resolved, secrets, red, a.accountArguments)
	if err != nil {
		return nil, err
	}
	var row serviceAccountRow
	request := struct {
		ServiceAccount accountBody `json:"serviceAccount"`
		UpdateMask     string      `json:"updateMask"`
	}{body, mask}
	if err := c.changeAccount(ctx, op, serviceAccountsPatchID, http.MethodPatch, accountPath(project, a.Account),
		request, &row); err != nil {
		return nil, err
	}
	if err := row.ownsAnswer(op, project, a.Account); err != nil {
		return nil, accountChangeUnsure(err.(*provider.Error))
	}
	return row.resultIn(project), nil
}

// --- service-accounts.disable and enable ---

type disabledResult struct {
	ProjectID string `json:"projectId"`
	Account   string `json:"account"`
	Disabled  bool   `json:"disabled"`
}

func accountSwitch(id, title, effect string, disable bool) capability.Descriptor {
	verb := "Lift the block of"
	state := "enabled"
	if disable {
		verb, state = "Block the access of", "disabled"
	}
	return capability.Descriptor{
		ID: id, Version: 1, Title: title,
		Description: verb + " a service account (projects.serviceAccounts." + effect + " of IAM v1). The account " +
			"is " + state + " afterwards; its keys, roles, and policies stay as they are. Repeating the call is " +
			"harmless. The project must lie in the targets of the connection, and an email of another project is " +
			"refused before any request. Google answers with an empty body, so the result is the project, the " +
			"account as given, and the new state." + changeNote,
		Tags: append(accountToolTags, effect), Provider: Provider,
		Risk: accountWriteRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
		InputSchema: json.RawMessage(`{"type":"object","properties":{` + projectIDProp + `,` + accountProp +
			`},"required":["project_id","account"],"additionalProperties":false}`),
		OutputSchema: json.RawMessage(disabledResultSchema),
		Arguments:    []capability.Argument{projectIDArgument, accountArgument},
		Fields: []capability.Field{{Name: "projectId", Description: "Project that owns the account"},
			{Name: "account", Description: "Account as given"},
			{Name: "disabled", Description: "New state: true after disable, false after enable"}},
		Examples: []capability.Example{{Description: title,
			Arguments: json.RawMessage(`{"project_id":"example-project-123",` +
				`"account":"deploy@example-project-123.iam.gserviceaccount.com"}`)}},
	}
}

var (
	serviceAccountsDisable = accountSwitch(serviceAccountsDisableID, "Disable a Google Cloud service account", "disable", true)
	serviceAccountsEnable  = accountSwitch(serviceAccountsEnableID, "Enable a Google Cloud service account", "enable", false)
)

func handleAccountSwitch(id, op, method string, disable bool) capability.Handler {
	return func(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
		raw json.RawMessage) (any, error) {
		var a accountArguments
		if err := json.Unmarshal(raw, &a); err != nil {
			return nil, failure(op, "the validated arguments could not be read")
		}
		c, project, err := openAccountScope(ctx, op, id, resolved, secrets, red, a)
		if err != nil {
			return nil, err
		}
		var answer map[string]json.RawMessage
		if err := c.changeAccount(ctx, op, id, http.MethodPost, accountPath(project, a.Account)+":"+method,
			struct{}{}, &answer); err != nil {
			return nil, err
		}
		return disabledResult{ProjectID: project, Account: boundedText(a.Account, maxNameText), Disabled: disable}, nil
	}
}

var (
	handleServiceAccountsDisable = handleAccountSwitch(serviceAccountsDisableID, "disable service account", "disable", true)
	handleServiceAccountsEnable  = handleAccountSwitch(serviceAccountsEnableID, "enable service account", "enable", false)
)

// --- confirmation ---

// accountSummary phrases a change from its validated arguments. Text of the caller is cut and quoted.
func accountSummary(tool string) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		var a writeAccountArguments
		if json.Unmarshal(raw, &a) != nil {
			return ""
		}
		quote := func(value string) string {
			return strconv.Quote(boundedText(cutRunes(value, summaryTextRunes), maxNameText))
		}
		project := "project " + boundedText(a.ProjectID, maxNameText)
		account := "service account " + boundedText(a.Account, maxNameText)
		switch tool {
		case serviceAccountsCreateID:
			out := "Create service account " + boundedText(a.AccountID, maxNameText) + " in " + project +
				" without roles or keys"
			if a.DisplayName != nil {
				out += ", display name " + quote(*a.DisplayName)
			}
			if a.Description != nil {
				out += ", description " + quote(*a.Description)
			}
			return out
		case serviceAccountsPatchID:
			out := "Change of " + account + " in " + project + ":"
			if a.DisplayName != nil {
				out += " display name to " + quote(*a.DisplayName) + ";"
			}
			if a.Description != nil {
				out += " description to " + quote(*a.Description) + ";"
			}
			return strings.TrimSuffix(out, ";")
		case serviceAccountsDisableID:
			return "Disable " + account + " in " + project + ": it can no longer be used until it is enabled again"
		}
		return "Enable " + account + " in " + project + ": it can be used again"
	}
}

// cutRunes cuts value to n runes, marking the cut.
func cutRunes(value string, n int) string {
	if utf8.RuneCountInString(value) <= n {
		return value
	}
	return string([]rune(value)[:n]) + "…"
}
