package googlecloud

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
	denyPoliciesListID = Provider + ".iam-deny-policies.list"
	denyPoliciesGetID  = Provider + ".iam-deny-policies.get"
)

// IAM v2 ignores pageSize and answers pages of up to 1000 policies, which is also the bound Qatlas accepts.
const maxDenyRows = 1000

type denyRuleWire struct {
	DeniedPrincipals     []string  `json:"deniedPrincipals"`
	ExceptionPrincipals  []string  `json:"exceptionPrincipals"`
	DeniedPermissions    []string  `json:"deniedPermissions"`
	ExceptionPermissions []string  `json:"exceptionPermissions"`
	DenialCondition      *exprWire `json:"denialCondition"`
}

type denyPolicyWire struct {
	Name        string            `json:"name"`
	UID         string            `json:"uid"`
	Kind        string            `json:"kind"`
	DisplayName string            `json:"displayName"`
	Annotations map[string]string `json:"annotations"`
	Etag        string            `json:"etag"`
	CreateTime  string            `json:"createTime"`
	UpdateTime  string            `json:"updateTime"`
	DeleteTime  string            `json:"deleteTime"`
	Rules       []struct {
		Description string        `json:"description"`
		DenyRule    *denyRuleWire `json:"denyRule"`
	} `json:"rules"`
}

type denyRuleResult struct {
	DeniedPrincipals     []string    `json:"deniedPrincipals,omitempty"`
	ExceptionPrincipals  []string    `json:"exceptionPrincipals,omitempty"`
	DeniedPermissions    []string    `json:"deniedPermissions,omitempty"`
	ExceptionPermissions []string    `json:"exceptionPermissions,omitempty"`
	DenialCondition      *exprResult `json:"denialCondition,omitempty"`
}

type denyRuleItem struct {
	Description string          `json:"description,omitempty"`
	DenyRule    *denyRuleResult `json:"denyRule,omitempty"`
}

type denyPolicyResult struct {
	Name        string            `json:"name"`
	UID         string            `json:"uid,omitempty"`
	Kind        string            `json:"kind,omitempty"`
	DisplayName string            `json:"displayName,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
	Rules       []denyRuleItem    `json:"rules,omitempty"`
	Etag        string            `json:"etag,omitempty"`
	CreateTime  string            `json:"createTime,omitempty"`
	UpdateTime  string            `json:"updateTime,omitempty"`
	DeleteTime  string            `json:"deleteTime,omitempty"`
	Truncated   bool              `json:"truncated,omitempty"`
}

func (p denyPolicyWire) result() denyPolicyResult {
	var cut cutter
	out := denyPolicyResult{Name: cut.text(p.Name, maxText), UID: cut.text(p.UID, maxNameText),
		Kind: cut.text(p.Kind, maxNameText), DisplayName: cut.text(p.DisplayName, maxNameText),
		Annotations: boundedMap(p.Annotations), Etag: cut.text(p.Etag, maxNameText),
		CreateTime: cut.text(p.CreateTime, maxNameText), UpdateTime: cut.text(p.UpdateTime, maxNameText),
		DeleteTime: cut.text(p.DeleteTime, maxNameText)}
	if len(p.Annotations) > maxLabels {
		cut.cut = true
	}
	if len(p.Rules) > maxIAMRules {
		cut.cut = true
	}
	for _, rule := range p.Rules[:min(len(p.Rules), maxIAMRules)] {
		item := denyRuleItem{Description: cut.text(rule.Description, maxText)}
		if d := rule.DenyRule; d != nil {
			item.DenyRule = &denyRuleResult{DeniedPrincipals: cut.list(d.DeniedPrincipals, maxIAMList),
				ExceptionPrincipals:  cut.list(d.ExceptionPrincipals, maxIAMList),
				DeniedPermissions:    cut.list(d.DeniedPermissions, maxIAMList),
				ExceptionPermissions: cut.list(d.ExceptionPermissions, maxIAMList),
				DenialCondition:      cut.expr(d.DenialCondition)}
		}
		out.Rules = append(out.Rules, item)
	}
	out.Truncated = cut.cut
	return out
}

const denyPolicyOutputSchema = `{"type":"object","properties":{"name":{"type":"string"},"uid":{"type":"string"},` +
	`"kind":{"type":"string"},"displayName":{"type":"string"},` +
	`"annotations":{"type":"object","additionalProperties":{"type":"string"}},` +
	`"rules":{"type":"array","items":{"type":"object","properties":{"description":{"type":"string"},` +
	`"denyRule":{"type":"object","properties":{"deniedPrincipals":` + stringArraySchema + `,` +
	`"exceptionPrincipals":` + stringArraySchema + `,"deniedPermissions":` + stringArraySchema + `,` +
	`"exceptionPermissions":` + stringArraySchema + `,"denialCondition":` + exprSchema + `},` +
	`"additionalProperties":false}},"additionalProperties":false}},` +
	`"etag":{"type":"string"},"createTime":{"type":"string"},"updateTime":{"type":"string"},` +
	`"deleteTime":{"type":"string"},"truncated":{"type":"boolean"}},` +
	`"required":["name"],"additionalProperties":false}`

const denyTail = " Principals and permissions are untrusted third-party text, returned as Google sends them; " +
	"lists and strings are cut. The resource must lie in the targets of the connection, proved before any " +
	"policy is read; an organization only on a connection that names it."

var denyPolicyFields = []capability.Field{
	{Name: "name", Description: "Resource name, policies/ATTACHMENT_POINT/denypolicies/ID, with the attachment point URL-encoded"},
	{Name: "uid", Description: "Globally unique ID of the policy"},
	{Name: "kind", Description: "Kind of the policy, always DenyPolicy"},
	{Name: "displayName", Description: "Display name given by the owners; untrusted text"},
	{Name: "annotations", Description: "Annotations of the policy as key and value; untrusted text"},
	{Name: "rules", Description: "Rules of the policy, each {description, denyRule: {deniedPrincipals, " +
		"exceptionPrincipals, deniedPermissions, exceptionPermissions, denialCondition}}; the list tool " +
		"omits them, Google sends only metadata there"},
	{Name: "etag", Description: "Entity tag of the policy"},
	{Name: "createTime", Description: "Creation time, RFC 3339"},
	{Name: "updateTime", Description: "Time of the last change, RFC 3339"},
	{Name: "deleteTime", Description: "Time of the deletion, RFC 3339; absent while the policy exists"},
	truncatedField,
}

var denyPoliciesList = capability.Descriptor{
	ID: denyPoliciesListID, Version: 1, Title: "List Google Cloud deny policies",
	Description: "List the deny policies attached to a project, folder, or organization, a page at a time: " +
		"name, uid, display name, annotations, etag, and timestamps. Google sends only the metadata of each " +
		"policy here and omits its rules; iam-deny-policies.get reads one policy with its rules. Policies " +
		"inherited from a parent are not listed." + denyTail,
	Tags: []string{"google", "cloud", "iam", "deny-policies"}, Risk: iamRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + hierarchyTypes + `,` + resourceIDProp + `,` +
		`"page_size":{"type":"integer","minimum":1,"maximum":1000},` + cursorProp + `},` +
		`"required":["resource_type","resource_id"],"additionalProperties":false}`),
	OutputSchema: listOutputSchema("policies", denyPolicyOutputSchema),
	Arguments: []capability.Argument{
		hierarchyTypeArgument, policyResourceIDArgument,
		{Name: "page_size", Description: "Policies per page, 1 to 1000; the documentation of Google says IAM " +
			"ignores it and uses 1000"},
		cursorArgument,
	},
	Fields: append([]capability.Field{
		{Name: "policies", Description: "Deny policies of the page, each {name, uid, kind, displayName, " +
			"annotations, etag, createTime, updateTime, deleteTime}"},
	}, pagingFields...),
	Examples: []capability.Example{{
		Description: "List the deny policies of a project",
		Arguments:   json.RawMessage(`{"resource_type":"project","resource_id":"my-project-123"}`),
	}},
}

var denyPoliciesGet = capability.Descriptor{
	ID: denyPoliciesGetID, Version: 1, Title: "Get a Google Cloud deny policy",
	Description: "Read one deny policy by its ID from the project, folder, or organization it is attached " +
		"to, with its rules: denied and excepted principals and permissions and the denial condition." + denyTail,
	Tags: []string{"google", "cloud", "iam", "deny-policies"}, Risk: iamRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + hierarchyTypes + `,` + resourceIDProp + `,` +
		`"policy_id":{"type":"string","minLength":1,"maxLength":63,"pattern":"^[A-Za-z0-9][A-Za-z0-9._-]{0,62}$"}},` +
		`"required":["resource_type","resource_id","policy_id"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(denyPolicyOutputSchema),
	Arguments: []capability.Argument{
		hierarchyTypeArgument, policyResourceIDArgument,
		{Name: "policy_id", Required: true, Description: "ID of the deny policy, the last part of its name; " +
			"never a path"},
	},
	Fields: denyPolicyFields,
	Examples: []capability.Example{{
		Description: "Read one deny policy of a folder",
		Arguments:   json.RawMessage(`{"resource_type":"folder","resource_id":"123456789","policy_id":"block-delete"}`),
	}},
}

func handleDenyPoliciesList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list deny policies"
	var arguments struct {
		policyArguments
		PageSize *int   `json:"page_size"`
		Cursor   string `json:"cursor"`
	}
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if arguments.PageSize != nil && (*arguments.PageSize < 1 || *arguments.PageSize > maxDenyRows) {
		return nil, badRequest("page_size must be 1 to " + strconv.Itoa(maxDenyRows))
	}
	// The cursor binds the tool, the targets, and the attachment point, so it continues only the list that
	// issued it. Everything it needs to be refused is known before the secret is read.
	binding := provider.CursorBinding(denyPoliciesListID, provider.TargetsOf(resolved), arguments.ResourceType,
		arguments.ResourceID)
	token := ""
	if arguments.Cursor != "" {
		var ok bool
		if token, ok = provider.DecodeCursor(binding, arguments.Cursor, maxCursorLength); !ok {
			return nil, badRequest("cursor is not a next_cursor of this list; start again without cursor")
		}
	}
	c, r, err := openResource(ctx, op, denyPoliciesListID, resolved, secrets, red, arguments.policyArguments,
		kindProject, kindFolder, kindOrganization)
	if err != nil {
		return nil, err
	}
	query := url.Values{}
	if arguments.PageSize != nil {
		query.Set("pageSize", strconv.Itoa(*arguments.PageSize))
	}
	if token != "" {
		query.Set("pageToken", token)
	}
	var page struct {
		Policies      []denyPolicyWire `json:"policies"`
		NextPageToken string           `json:"nextPageToken"`
	}
	if err := c.get(ctx, op, denyPoliciesListID, serviceIAM, "/v2/policies/"+r.attachmentPoint()+"/denypolicies",
		query, &page); err != nil {
		return nil, err
	}
	if len(page.Policies) > maxDenyRows || len(page.NextPageToken) > maxPageTokenBytes {
		return nil, invalidRow(op)
	}
	rows := make([]any, 0, len(page.Policies))
	for _, policy := range page.Policies {
		if !strings.HasPrefix(policy.Name, "policies/") || !strings.Contains(policy.Name, "/denypolicies/") {
			return nil, invalidRow(op)
		}
		rows = append(rows, policy.result())
	}
	out := map[string]any{"policies": rows, "has_more": page.NextPageToken != ""}
	if page.NextPageToken != "" {
		out["next_cursor"] = provider.EncodeCursor(binding, page.NextPageToken)
	}
	return out, nil
}

func handleDenyPoliciesGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "get deny policy"
	var arguments struct {
		policyArguments
		PolicyID string `json:"policy_id"`
	}
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if !policyIDPattern.MatchString(arguments.PolicyID) {
		return nil, badRequest("policy_id must be the ID of a deny policy, never a path")
	}
	c, r, err := openResource(ctx, op, denyPoliciesGetID, resolved, secrets, red, arguments.policyArguments,
		kindProject, kindFolder, kindOrganization)
	if err != nil {
		return nil, err
	}
	var policy denyPolicyWire
	if err := c.get(ctx, op, denyPoliciesGetID, serviceIAM,
		"/v2/policies/"+r.attachmentPoint()+"/denypolicies/"+url.PathEscape(arguments.PolicyID), nil, &policy); err != nil {
		return nil, err
	}
	if !strings.HasPrefix(policy.Name, "policies/") || !strings.HasSuffix(policy.Name, "/denypolicies/"+arguments.PolicyID) {
		return nil, invalidRow(op)
	}
	return policy.result(), nil
}
