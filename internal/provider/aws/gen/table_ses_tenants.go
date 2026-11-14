package gen

import "slices"

const (
	sesTenantSensitivity     = "aws-ses-tenant"
	sesReputationSensitivity = "aws-ses-reputation"

	descSESTenantName = "Name of the tenant, up to 64 characters: letters, digits, hyphens, and underscores"
	descSESTenantARN  = "ARN of an identity, a configuration set, or an email template in the bound account and an allowed region, " +
		"such as arn:aws:ses:REGION:ACCOUNT:identity/example.com, arn:aws:ses:REGION:ACCOUNT:configuration-set/NAME, or " +
		"arn:aws:ses:REGION:ACCOUNT:template/NAME"
	descSESReputationRef = "ARN of the tenant, identity, configuration set, or email template whose reputation entity is meant, in the " +
		"bound account and an allowed region, such as arn:aws:ses:REGION:ACCOUNT:tenant/NAME/ID or arn:aws:ses:REGION:ACCOUNT:identity/example.com"
	descSESEntityType = "Type of the reputation entity: RESOURCE"
	descSESSendStatus = "ENABLED, REINSTATED, or DISABLED"

	sesPauseNote = "Setting the status DISABLED stops sending for the entity; ENABLED or REINSTATED allows it again."
)

func sesTenantRead(op Operation, tags ...string) Operation {
	op.Effect, op.Idempotency = "read", "safe"
	op.DataSensitivity = sesTenantSensitivity
	op.Tags = slices.Concat([]string{"aws", "ses", "tenant"}, tags)
	if op.TargetCheck == "" {
		op.TargetCheck = "none"
	}
	op.PolicyCheck = "none"
	return op
}

func sesTenantWrite(op Operation, effect, idempotency string, tags ...string) Operation {
	op.Effect, op.Idempotency, op.Confirm = effect, idempotency, true
	op.DataSensitivity = sesTenantSensitivity
	op.Tags = slices.Concat([]string{"aws", "ses", "tenant"}, tags)
	if op.TargetCheck == "" {
		op.TargetCheck = "none"
	}
	op.PolicyCheck = "none"
	return op
}

func sesReputationRead(op Operation, tags ...string) Operation {
	op = sesTenantRead(op, tags...)
	op.DataSensitivity = sesReputationSensitivity
	op.Tags = slices.Concat([]string{"aws", "ses", "reputation"}, tags)
	return op
}

func sesReputationWrite(op Operation, tags ...string) Operation {
	op = sesTenantWrite(op, "update", "idempotent", tags...)
	op.DataSensitivity = sesReputationSensitivity
	op.Tags = slices.Concat([]string{"aws", "ses", "reputation"}, tags)
	return op
}

var sesTenantNameLimit = InputLimit{Member: "TenantName", MaxLen: 64, Reason: "AWS documents at most 64 characters; the model gives no bound"}

// sesTenantTools are the SES v2 operations on tenants, their resource associations and suppression attributes,
// and on reputation entities. Tenants and the ARNs they take belong to the bound account.
var sesTenantTools = []Operation{
	sesTenantRead(Operation{
		Operation: "GetTenant", Object: "tenant", Action: "get",
		Title:       "Get an SES tenant",
		Description: "Return one tenant of the account in one region by name: ID, ARN, creation time, tags, sending status, and suppression attributes. Needs ses:GetTenant.",
		Arguments:   map[string]Argument{"TenantName": {Description: descSESTenantName}},
		Fields:      map[string]Field{"Tenant": {Description: "name, ID, ARN, creation time, tags, sending status (ENABLED, REINSTATED, or DISABLED), and suppression attributes"}},
		Limits:      []InputLimit{sesTenantNameLimit},
		Examples:    []Example{{Description: "Read a tenant", Arguments: `{"region":"eu-central-1","tenant_name":"customer-a"}`}},
	}),
	sesTenantRead(Operation{
		Operation: "ListTenants", Object: "tenant", Action: "list",
		Title: "List SES tenants",
		Description: "List the tenants of the account in one region, one page at a time, each with name, ID, ARN, creation time, and " +
			"sending status. Needs ses:ListTenants.",
		Arguments:   map[string]Argument{"Filter": {Description: "Filter as a map of entries: TENANT_NAME_CONTAINS with a part of a name, or SENDING_STATUS with ENABLED, REINSTATED, or DISABLED"}},
		Fields:      map[string]Field{"Tenants": {Description: "Tenants of this page"}},
		PageSizeMax: 100,
		Examples:    []Example{{Description: "List the paused tenants", Arguments: `{"region":"eu-central-1","filter":{"SENDING_STATUS":"DISABLED"}}`}},
	}),
	sesTenantRead(Operation{
		Operation: "ListTenantResources", Object: "tenant-resource", Action: "list",
		Title:       "List the resources of an SES tenant",
		Description: "List the identities, configuration sets, and email templates associated with a tenant, one page at a time, each with type and ARN. Needs ses:ListTenantResources.",
		Arguments: map[string]Argument{
			"TenantName": {Description: descSESTenantName},
			"Filter":     {Description: "Filter as a map of entries: RESOURCE_TYPE with EMAIL_IDENTITY, CONFIGURATION_SET, or EMAIL_TEMPLATE"},
		},
		Fields:      map[string]Field{"TenantResources": {Description: "Resources of this page, each with resource_type and resource_arn"}},
		PageSizeMax: 100,
		Limits:      []InputLimit{sesTenantNameLimit},
		Examples:    []Example{{Description: "List the identities of a tenant", Arguments: `{"region":"eu-central-1","tenant_name":"customer-a","filter":{"RESOURCE_TYPE":"EMAIL_IDENTITY"}}`}},
	}, "resource"),
	sesTenantRead(Operation{
		Operation: "ListResourceTenants", Object: "resource-tenant", Action: "list",
		Title:       "List the SES tenants of a resource",
		Description: "List the tenants a resource of the bound account is associated with, one page at a time, each with tenant name, ID, resource ARN, and the time of the association. Needs ses:ListResourceTenants.",
		Arguments:   map[string]Argument{"ResourceArn": {Description: descSESTenantARN}},
		Fields:      map[string]Field{"ResourceTenants": {Description: "Tenants of this page, each with tenant_name, tenant_id, resource_arn, and associated_timestamp"}},
		PageSizeMax: 100,
		TargetCheck: "ses-resource-arn", TargetArgs: []string{"ResourceArn"},
		Examples: []Example{{Description: "Find the tenants of an identity", Arguments: `{"region":"eu-central-1","resource_arn":"arn:aws:ses:eu-central-1:123456789012:identity/example.com"}`}},
	}, "resource"),
	sesTenantWrite(Operation{
		Operation: "CreateTenant", Object: "tenant", Action: "create",
		Title: "Create an SES tenant",
		Description: "Create a tenant in the account in one region, a unit that groups resources and has its own sending status and " +
			"reputation. Each tenant is charged by AWS per month on top of the sending price, by the volume of mail; see the SES pricing page, the price is not retrieved by this tool. Optional tags and suppression attributes. A tenant name exists once; a repeated name is refused by SES. Needs " +
			"ses:CreateTenant, and ses:TagResource with tags. Not repeated after an unclear outcome: read the tenant before trying again. " + sesWriteRule,
		Arguments: map[string]Argument{
			"TenantName":            {Description: descSESTenantName},
			"Tags":                  {Description: descSESTags},
			"SuppressionAttributes": {Description: "suppressed_reasons (BOUNCE, COMPLAINT) and suppression_scope (ACCOUNT or TENANT) of the tenant"},
		},
		Fields: map[string]Field{
			"TenantName":            {Description: "Name of the new tenant"},
			"TenantId":              {Description: "ID of the tenant"},
			"TenantArn":             {Description: "ARN of the tenant"},
			"CreatedTimestamp":      {Description: "When the tenant was created"},
			"Tags":                  {Description: "Tags of the tenant"},
			"SendingStatus":         {Description: descSESSendStatus},
			"SuppressionAttributes": {Description: "suppressed_reasons and suppression_scope"},
		},
		Limits:   []InputLimit{sesTenantNameLimit},
		Examples: []Example{{Description: "Create a tenant", Arguments: `{"region":"eu-central-1","tenant_name":"customer-a"}`}},
	}, "create", "non-idempotent"),
	sesTenantWrite(Operation{
		Operation: "DeleteTenant", Object: "tenant", Action: "delete",
		Title: "Delete an SES tenant",
		Description: "Delete a tenant of the account in one region by name. Its sending status and reputation are gone with it. Needs " +
			"ses:DeleteTenant. " + sesWriteRule,
		Arguments: map[string]Argument{"TenantName": {Description: descSESTenantName}},
		Limits:    []InputLimit{sesTenantNameLimit},
		Examples:  []Example{{Description: "Delete a tenant", Arguments: `{"region":"eu-central-1","tenant_name":"customer-a"}`}},
	}, "delete", "non-idempotent"),
	sesTenantWrite(Operation{
		Operation: "CreateTenantResourceAssociation", Object: "tenant-resource", Action: "associate",
		Title: "Associate a resource with an SES tenant",
		Description: "Associate an identity, a configuration set, or an email template of the bound account with a tenant. The ARN " +
			"is checked before the call: it must be in the bound account and an allowed region. Needs ses:CreateTenantResourceAssociation. " + sesWriteRule,
		Arguments: map[string]Argument{
			"TenantName":  {Description: descSESTenantName},
			"ResourceArn": {Description: descSESTenantARN},
		},
		Limits:      []InputLimit{sesTenantNameLimit},
		TargetCheck: "ses-resource-arn", TargetArgs: []string{"ResourceArn"},
		Examples: []Example{{Description: "Associate an identity", Arguments: `{"region":"eu-central-1","tenant_name":"customer-a","resource_arn":"arn:aws:ses:eu-central-1:123456789012:identity/example.com"}`}},
	}, "create", "non-idempotent", "resource"),
	sesTenantWrite(Operation{
		Operation: "DeleteTenantResourceAssociation", Object: "tenant-resource", Action: "disassociate",
		Title: "Remove a resource from an SES tenant",
		Description: "Remove the association of an identity, a configuration set, or an email template with a tenant. The resource " +
			"itself stays. The ARN is checked as for the association. Needs ses:DeleteTenantResourceAssociation. " + sesWriteRule,
		Arguments: map[string]Argument{
			"TenantName":  {Description: descSESTenantName},
			"ResourceArn": {Description: descSESTenantARN},
		},
		Limits:      []InputLimit{sesTenantNameLimit},
		TargetCheck: "ses-resource-arn", TargetArgs: []string{"ResourceArn"},
		Examples: []Example{{Description: "Remove an identity", Arguments: `{"region":"eu-central-1","tenant_name":"customer-a","resource_arn":"arn:aws:ses:eu-central-1:123456789012:identity/example.com"}`}},
	}, "delete", "non-idempotent", "resource"),
	sesTenantWrite(Operation{
		Operation: "PutTenantSuppressionAttributes", Object: "tenant-suppression-attributes", Action: "put",
		Title: "Set the suppression attributes of an SES tenant",
		Description: "Set which reasons (BOUNCE, COMPLAINT) put an address on the suppression list of a tenant, and whether the " +
			"tenant uses the list of the account or its own (ACCOUNT or TENANT). Replaces the current values. Needs " +
			"ses:PutTenantSuppressionAttributes. " + sesWriteRule,
		Arguments: map[string]Argument{
			"TenantName":        {Description: descSESTenantName},
			"SuppressedReasons": {Description: "Reasons that suppress an address: BOUNCE, COMPLAINT"},
			"SuppressionScope":  {Description: "ACCOUNT to use the suppression list of the account, TENANT for a list of the tenant"},
		},
		Limits:   []InputLimit{sesTenantNameLimit},
		Examples: []Example{{Description: "Suppress bounces per tenant", Arguments: `{"region":"eu-central-1","tenant_name":"customer-a","suppressed_reasons":["BOUNCE"],"suppression_scope":"TENANT"}`}},
	}, "update", "idempotent", "suppression"),
	sesReputationRead(Operation{
		Operation: "GetReputationEntity", Object: "reputation-entity", Action: "get",
		Title: "Get an SES reputation entity",
		Description: "Return the reputation entity of a tenant or resource of the bound account: the reputation policy, the status set by " +
			"the customer and by SES, the aggregate sending status, and the reputation impact. Needs ses:GetReputationEntity.",
		Arguments: map[string]Argument{
			"ReputationEntityType":      {Description: descSESEntityType},
			"ReputationEntityReference": {Description: descSESReputationRef},
		},
		Fields:      map[string]Field{"ReputationEntity": {Description: "reference, type, management policy ARN, customer managed status and AWS managed status (each with status, cause, and time), aggregate sending status, and reputation impact (LOW or HIGH); causes are untrusted"}},
		TargetCheck: "ses-reputation-entity-arn", TargetArgs: []string{"ReputationEntityReference"},
		Examples: []Example{{Description: "Read the reputation of an identity", Arguments: `{"region":"eu-central-1","reputation_entity_type":"RESOURCE","reputation_entity_reference":"arn:aws:ses:eu-central-1:123456789012:identity/example.com"}`}},
	}),
	sesReputationRead(Operation{
		Operation: "ListReputationEntities", Object: "reputation-entity", Action: "list",
		Title:       "List SES reputation entities",
		Description: "List the reputation entities of the account in one region, one page at a time, each with the fields of the get tool. Needs ses:ListReputationEntities.",
		Arguments: map[string]Argument{"Filter": {Description: "Filter as a map of entries: ENTITY_TYPE with RESOURCE, REPUTATION_IMPACT with LOW or HIGH, " +
			"SENDING_STATUS with ENABLED, REINSTATED, or DISABLED, or ENTITY_REFERENCE_PREFIX with the start of a resource ARN"}},
		Fields:      map[string]Field{"ReputationEntities": {Description: "Entities of this page; causes are untrusted"}},
		PageSizeMax: 100,
		Examples:    []Example{{Description: "List the entities with a high impact", Arguments: `{"region":"eu-central-1","filter":{"REPUTATION_IMPACT":"HIGH"}}`}},
	}),
	sesReputationWrite(Operation{
		Operation: "UpdateReputationEntityCustomerManagedStatus", Object: "reputation-entity", Action: "update-customer-managed-status",
		Title: "Set the SES sending status of a reputation entity",
		Description: "Set the status the customer manages for the reputation entity of a tenant or resource of the bound account. " + sesPauseNote +
			" It can pause or release the sending of a tenant. Needs ses:UpdateReputationEntityCustomerManagedStatus. " + sesWriteRule,
		Arguments: map[string]Argument{
			"ReputationEntityType":      {Description: descSESEntityType},
			"ReputationEntityReference": {Description: descSESReputationRef},
			"SendingStatus":             {Description: descSESSendStatus},
		},
		TargetCheck: "ses-reputation-entity-arn", TargetArgs: []string{"ReputationEntityReference"},
		Examples: []Example{{Description: "Pause sending of an identity", Arguments: `{"region":"eu-central-1","reputation_entity_type":"RESOURCE","reputation_entity_reference":"arn:aws:ses:eu-central-1:123456789012:identity/example.com","sending_status":"DISABLED"}`}},
	}, "status"),
	sesReputationWrite(Operation{
		Operation: "UpdateReputationEntityPolicy", Object: "reputation-entity", Action: "update-policy",
		Title: "Set the SES reputation policy of an entity",
		Description: "Set the reputation management policy of the reputation entity of a tenant or resource of the bound account. The " +
			"policy is managed by SES, and decides when SES pauses sending: none never pauses automatically, standard pauses on " +
			"severe findings, strict on any finding. It can pause or release the sending of a tenant. The entity ARN must be in the " +
			"bound account and an allowed region and the policy ARN one of the three SES policies of an allowed region; both are checked before the call. Needs ses:UpdateReputationEntityPolicy. " + sesWriteRule,
		Arguments: map[string]Argument{
			"ReputationEntityType":      {Description: descSESEntityType},
			"ReputationEntityReference": {Description: descSESReputationRef},
			"ReputationEntityPolicy":    {Description: "ARN of an SES-managed policy in an allowed region: arn:aws:ses:REGION:aws:reputation-policy/none, standard, or strict"},
		},
		TargetCheck: "ses-reputation-entity-arn", TargetArgs: []string{"ReputationEntityReference"},
		Guards:     []string{"ses-reputation-policy-arn"},
		SampleArgs: `{"reputation_entity_policy":"arn:aws:ses:eu-central-1:aws:reputation-policy/standard"}`,
		Examples:   []Example{{Description: "Set the policy of an identity", Arguments: `{"region":"eu-central-1","reputation_entity_type":"RESOURCE","reputation_entity_reference":"arn:aws:ses:eu-central-1:123456789012:identity/example.com","reputation_entity_policy":"arn:aws:ses:eu-central-1:aws:reputation-policy/standard"}`}},
	}, "policy"),
}
