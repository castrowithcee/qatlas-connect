package googlecloud

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const iamPoliciesGetID = Provider + ".iam-policies.get"

// policyWire is the Policy of IAM as Google sends it, the same shape from Resource Manager and from IAM v1.
type policyWire struct {
	Version      *int              `json:"version"`
	Etag         string            `json:"etag"`
	Bindings     []bindingWire     `json:"bindings"`
	AuditConfigs []auditConfigWire `json:"auditConfigs"`
}

type bindingWire struct {
	Role      string    `json:"role"`
	Members   []string  `json:"members"`
	Condition *exprWire `json:"condition"`
}

type auditConfigWire struct {
	Service         string `json:"service"`
	AuditLogConfigs []struct {
		LogType         string   `json:"logType"`
		ExemptedMembers []string `json:"exemptedMembers"`
	} `json:"auditLogConfigs"`
}

type policyResult struct {
	Resource     string              `json:"resource"`
	Version      *int                `json:"version,omitempty"`
	Etag         string              `json:"etag,omitempty"`
	Bindings     []bindingResult     `json:"bindings"`
	AuditConfigs []auditConfigResult `json:"auditConfigs,omitempty"`
	Truncated    bool                `json:"truncated,omitempty"`
}

type bindingResult struct {
	Role      string      `json:"role"`
	Members   []string    `json:"members"`
	Condition *exprResult `json:"condition,omitempty"`
}

type auditConfigResult struct {
	Service         string `json:"service"`
	AuditLogConfigs []struct {
		LogType         string   `json:"logType,omitempty"`
		ExemptedMembers []string `json:"exemptedMembers,omitempty"`
	} `json:"auditLogConfigs,omitempty"`
}

func (p policyWire) result(resource string) policyResult {
	var cut cutter
	out := policyResult{Resource: resource, Version: p.Version, Etag: cut.text(p.Etag, maxNameText),
		Bindings: []bindingResult{}}
	if len(p.Bindings) > maxIAMBindings {
		cut.cut = true
	}
	for _, b := range p.Bindings[:min(len(p.Bindings), maxIAMBindings)] {
		members := cut.list(b.Members, maxIAMList)
		if members == nil {
			members = []string{}
		}
		out.Bindings = append(out.Bindings, bindingResult{Role: cut.text(b.Role, maxNameText), Members: members,
			Condition: cut.expr(b.Condition)})
	}
	if len(p.AuditConfigs) > maxIAMRules {
		cut.cut = true
	}
	for _, a := range p.AuditConfigs[:min(len(p.AuditConfigs), maxIAMRules)] {
		item := auditConfigResult{Service: cut.text(a.Service, maxNameText)}
		if len(a.AuditLogConfigs) > 10 {
			cut.cut = true
		}
		for _, l := range a.AuditLogConfigs[:min(len(a.AuditLogConfigs), 10)] {
			item.AuditLogConfigs = append(item.AuditLogConfigs, struct {
				LogType         string   `json:"logType,omitempty"`
				ExemptedMembers []string `json:"exemptedMembers,omitempty"`
			}{cut.text(l.LogType, maxNameText), cut.list(l.ExemptedMembers, maxIAMList)})
		}
		out.AuditConfigs = append(out.AuditConfigs, item)
	}
	out.Truncated = cut.cut
	return out
}

const policyOutputSchema = `{"type":"object","properties":{"resource":{"type":"string"},"version":{"type":"integer"},` +
	`"etag":{"type":"string"},"bindings":{"type":"array","items":{"type":"object","properties":{` +
	`"role":{"type":"string"},"members":` + stringArraySchema + `,"condition":` + exprSchema + `},` +
	`"required":["role","members"],"additionalProperties":false}},` +
	`"auditConfigs":{"type":"array","items":{"type":"object","properties":{"service":{"type":"string"},` +
	`"auditLogConfigs":{"type":"array","items":{"type":"object","properties":{"logType":{"type":"string"},` +
	`"exemptedMembers":` + stringArraySchema + `},"additionalProperties":false}}},` +
	`"required":["service"],"additionalProperties":false}},"truncated":{"type":"boolean"}},` +
	`"required":["resource"],"additionalProperties":false}`

var iamPoliciesGet = capability.Descriptor{
	ID: iamPoliciesGetID, Version: 1, Title: "Get a Google Cloud IAM policy",
	Description: "Read the IAM policy of a project, folder, organization, or service account: its version, etag, " +
		"bindings (role, members, and an optional condition), and audit configs. The policy of the resource " +
		"itself is returned, not the one it inherits. The request is a POST that only reads and is safe to " +
		"repeat. The resource must lie in the targets of the connection, which Qatlas proves before the " +
		"policy is read: a project or a folder through the parent chain, a service account through the project " +
		"Google reports for it, and an organization only on a connection that names it. Members such as user:, group:, and " +
		"serviceAccount: identities are personal data and untrusted third-party text; they are returned as " +
		"Google sends them, and nothing is flagged or removed. Lists and strings are cut.",
	Tags: []string{"google", "cloud", "iam", "policies"}, Risk: iamRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + resourceTypeProp + `,` + resourceIDProp +
		`,"requested_policy_version":{"type":"integer","enum":[0,1,3]}},` +
		`"required":["resource_type","resource_id"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(policyOutputSchema),
	Arguments: []capability.Argument{
		resourceTypeArgument, resourceIDArgument,
		{Name: "requested_policy_version", Description: "Highest policy version Google may format the policy " +
			"in: 0, 1, or 3. The default is 3, which shows the conditions of bindings; a policy with conditions " +
			"cannot be read faithfully with a lower version"},
	},
	Fields: []capability.Field{
		resourceField,
		{Name: "version", Description: "Format version of the policy, 0, 1, or 3; 3 when it has conditional bindings"},
		{Name: "etag", Description: "Entity tag of the policy, base64"},
		{Name: "bindings", Description: "Bindings of the policy, each {role, members, condition}; members " +
			"are identities such as user:, group:, serviceAccount:, domain:, allUsers; condition is " +
			"{expression, title, description, location} and absent for an unconditional binding"},
		{Name: "auditConfigs", Description: "Audit logging configuration, each {service, auditLogConfigs: " +
			"[{logType, exemptedMembers}]}"},
		truncatedField,
	},
	Examples: []capability.Example{{
		Description: "Read the policy of a project with its conditions",
		Arguments:   json.RawMessage(`{"resource_type":"project","resource_id":"my-project-123"}`),
	}, {
		Description: "Read the policy of a service account",
		Arguments: json.RawMessage(`{"resource_type":"service-account",` +
			`"resource_id":"deploy@example-project.iam.gserviceaccount.com"}`),
	}},
}

func handleIAMPoliciesGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "get iam policy"
	var arguments struct {
		iamArguments
		Version *int `json:"requested_policy_version"`
	}
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	version := 3
	if arguments.Version != nil {
		version = *arguments.Version
	}
	if version != 0 && version != 1 && version != 3 {
		return nil, badRequest("requested_policy_version must be 0, 1, or 3")
	}
	c, r, err := openResource(ctx, op, iamPoliciesGetID, resolved, secrets, red, arguments.iamArguments,
		kindProject, kindFolder, kindOrganization, kindServiceAccount)
	if err != nil {
		return nil, err
	}
	svc, base := r.service()
	path := base + r.escaped() + ":getIamPolicy"
	var wire policyWire
	if r.kind == kindServiceAccount {
		// IAM v1 takes the option as a query parameter and has no request body.
		query := url.Values{"options.requestedPolicyVersion": {strconv.Itoa(version)}}
		err = c.post(ctx, op, iamPoliciesGetID, svc, path, query, nil, &wire)
	} else {
		body := map[string]any{"options": map[string]int{"requestedPolicyVersion": version}}
		err = c.post(ctx, op, iamPoliciesGetID, svc, path, nil, body, &wire)
	}
	if err != nil {
		return nil, err
	}
	return wire.result(r.name()), nil
}
