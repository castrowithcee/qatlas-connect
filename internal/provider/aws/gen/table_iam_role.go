package gen

import "slices"

const (
	iamWriteRule = "Offered only by a connection whose tools list names this tool exactly, and needs confirmation."
	iamOwnRole   = "Refused for the role whose credentials the connection uses, which the tool asks sts:GetCallerIdentity " +
		"for first; needs sts:GetCallerIdentity, which is free."
	iamTrustRule = "The trust policy is checked before any request: every statement, Deny included, must name a Principal " +
		"with only AWS (the bound account as its ID, or ARNs of its root, its users, and its roles) and Service " +
		"(name.amazonaws.com). NotPrincipal, wildcards, other accounts and partitions, unique IDs, STS ARNs, Federated, " +
		"CanonicalUser, a key given twice, and documents over 2048 characters are refused."

	descRoleName      = "Name of the role in the bound account, 1 to 64 letters, digits, or the characters +=,.@_-"
	descProfileName   = "Name of the instance profile in the bound account, 1 to 128 letters, digits, or the characters +=,.@_-"
	descPath          = "Path, 1 to 512 characters, a single slash or starting and ending with one; a single slash when left out"
	descTrustPolicy   = "Trust policy as JSON text, at most 2048 characters, checked before any request as the description says"
	descRoleDesc      = "Description of the role, 0 to 1000 characters"
	descMaxSession    = "Maximum session duration of the role in seconds, 3600 to 43200; one hour when left out on create"
	descBoundary      = "ARN of the managed policy that becomes the permissions boundary: an AWS managed policy or a policy of the bound account"
	descTags          = "Tags, up to 50, each with key (1 to 128 characters) and value (0 to 256 characters)"
	descTruncatedRole = "True when a string of the result was cut at its limit, such as a long trust policy"
)

// iamWrite fills what every IAM write tool shares: listed-only with confirmation, no target argument since the
// names belong to the bound account, and the own-role check on the RoleName of a tool that changes a role.
func iamWrite(op Operation, effect, idempotency string, tags ...string) Operation {
	op.Effect, op.Idempotency, op.Confirm, op.ListedOnly = effect, idempotency, true, true
	if op.TargetCheck == "" {
		op.TargetCheck = "none"
	}
	if op.PolicyCheck == "" {
		op.PolicyCheck = "none"
	}
	op.DataSensitivity = "aws-iam-identity"
	op.Tags = slices.Concat([]string{"aws", "iam"}, tags)
	return op
}

// iamRoleTools are the IAM operations that create, change, and delete roles and instance profiles.
var iamRoleTools = []Operation{
	iamWrite(Operation{
		Operation: "CreateRole", Object: "role", Action: "create",
		Title: "Create an IAM role",
		Description: "Create an IAM role in the bound account with the given trust policy, and optionally path, " +
			"description, maximum session duration, permissions boundary, and tags; returns the new role. Needs " +
			"iam:CreateRole, and iam:TagRole with tags. Not repeated after an unclear outcome: read the role before " +
			"trying again. " + iamTrustRule + " " + iamWriteRule,
		Arguments: map[string]Argument{
			"Path":                     {Description: descPath},
			"RoleName":                 {Description: descRoleName},
			"AssumeRolePolicyDocument": {Description: descTrustPolicy},
			"Description":              {Description: descRoleDesc},
			"MaxSessionDuration":       {Description: descMaxSession},
			"PermissionsBoundary":      {Description: descBoundary},
			"Tags":                     {Description: descTags},
		},
		Fields:      map[string]Field{"Role": {Description: "The new role; its trust policy is JSON text and untrusted", Required: true}},
		TargetCheck: "arn-account-or-managed-policy", TargetArgs: []string{"PermissionsBoundary"},
		PolicyCheck: "trust-policy", PolicyArgs: []string{"AssumeRolePolicyDocument"},
		Truncated: descTruncatedRole,
		Examples: []Example{{Description: "Create a role that EC2 instances assume",
			Arguments: `{"role_name":"web","assume_role_policy_document":"{\"Version\":\"2012-10-17\",\"Statement\":[{\"Effect\":\"Allow\",\"Principal\":{\"Service\":\"ec2.amazonaws.com\"},\"Action\":\"sts:AssumeRole\"}]}"}`}},
	}, "create", "non-idempotent", "role"),
	iamWrite(Operation{
		Operation: "UpdateRole", Object: "role", Action: "update",
		Title: "Update the description or session duration of an IAM role",
		Description: "Set the description or the maximum session duration of an IAM role of the bound account. " +
			"Needs iam:UpdateRole. " + iamOwnRole + " " + iamWriteRule,
		Arguments: map[string]Argument{
			"RoleName":           {Description: descRoleName},
			"Description":        {Description: descRoleDesc},
			"MaxSessionDuration": {Description: descMaxSession},
		},
		SelfRole: "RoleName",
		Examples: []Example{{Description: "Allow two-hour sessions", Arguments: `{"role_name":"web","max_session_duration":7200}`}},
	}, "update", "idempotent", "role"),
	iamWrite(Operation{
		Operation: "UpdateAssumeRolePolicy", Object: "role", Action: "update-trust-policy",
		Title: "Replace the trust policy of an IAM role",
		Description: "Replace the trust policy of an IAM role of the bound account, which decides who may assume it. " +
			"Needs iam:UpdateAssumeRolePolicy. " + iamTrustRule + " " + iamOwnRole + " " + iamWriteRule,
		Arguments: map[string]Argument{
			"RoleName":       {Description: descRoleName},
			"PolicyDocument": {Name: "assume_role_policy_document", Description: descTrustPolicy},
		},
		PolicyCheck: "trust-policy", PolicyArgs: []string{"PolicyDocument"},
		SelfRole: "RoleName",
		Examples: []Example{{Description: "Let Lambda assume a role",
			Arguments: `{"role_name":"worker","assume_role_policy_document":"{\"Version\":\"2012-10-17\",\"Statement\":[{\"Effect\":\"Allow\",\"Principal\":{\"Service\":\"lambda.amazonaws.com\"},\"Action\":\"sts:AssumeRole\"}]}"}`}},
	}, "update", "idempotent", "role", "trust-policy"),
	iamWrite(Operation{
		Operation: "PutRolePermissionsBoundary", Object: "role", Action: "put-permissions-boundary",
		Title: "Set the permissions boundary of an IAM role",
		Description: "Set or replace the managed policy that bounds the permissions of an IAM role of the bound " +
			"account. Needs iam:PutRolePermissionsBoundary. " + iamOwnRole + " " + iamWriteRule,
		Arguments: map[string]Argument{
			"RoleName":            {Description: descRoleName},
			"PermissionsBoundary": {Description: descBoundary},
		},
		TargetCheck: "arn-account-or-managed-policy", TargetArgs: []string{"PermissionsBoundary"},
		SelfRole: "RoleName",
		Examples: []Example{{Description: "Bound a role by an AWS managed policy",
			Arguments: `{"role_name":"web","permissions_boundary":"arn:aws:iam::aws:policy/PowerUserAccess"}`}},
	}, "update", "idempotent", "role", "permissions-boundary"),
	iamWrite(Operation{
		Operation: "DeleteRolePermissionsBoundary", Object: "role", Action: "delete-permissions-boundary",
		Title: "Remove the permissions boundary of an IAM role",
		Description: "Remove the permissions boundary of an IAM role of the bound account, which widens what the role " +
			"may do to its permission policies. Needs iam:DeleteRolePermissionsBoundary. " + iamOwnRole + " " + iamWriteRule,
		Arguments: map[string]Argument{"RoleName": {Description: descRoleName}},
		SelfRole:  "RoleName",
		Examples:  []Example{{Description: "Remove the boundary of a role", Arguments: `{"role_name":"web"}`}},
	}, "delete", "non-idempotent", "role", "permissions-boundary"),
	iamWrite(Operation{
		Operation: "TagRole", Object: "role", Action: "tag",
		Title: "Tag an IAM role",
		Description: "Add tags to an IAM role of the bound account or replace the values of existing keys. Needs " +
			"iam:TagRole. Tags can grant access through policy conditions. " + iamOwnRole + " " + iamWriteRule,
		Arguments: map[string]Argument{
			"RoleName": {Description: descRoleName},
			"Tags":     {Description: descTags},
		},
		SelfRole: "RoleName",
		Examples: []Example{{Description: "Tag a role with its team", Arguments: `{"role_name":"web","tags":[{"key":"team","value":"web"}]}`}},
	}, "update", "idempotent", "role", "tags"),
	iamWrite(Operation{
		Operation: "UntagRole", Object: "role", Action: "untag",
		Title: "Remove tags from an IAM role",
		Description: "Remove tags from an IAM role of the bound account by key. Needs iam:UntagRole. " + iamOwnRole + " " +
			iamWriteRule,
		Arguments: map[string]Argument{
			"RoleName": {Description: descRoleName},
			"TagKeys":  {Description: "Keys of the tags to remove, up to 50, each 1 to 128 characters"},
		},
		SelfRole: "RoleName",
		Examples: []Example{{Description: "Remove a tag from a role", Arguments: `{"role_name":"web","tag_keys":["team"]}`}},
	}, "update", "idempotent", "role", "tags"),
	iamWrite(Operation{
		Operation: "DeleteRole", Object: "role", Action: "delete",
		Title: "Delete an IAM role",
		Description: "Delete an IAM role of the bound account. AWS refuses it with a conflict while the role still has " +
			"policies or belongs to an instance profile; the tool does not detach anything itself. Needs iam:DeleteRole. " +
			iamOwnRole + " " + iamWriteRule,
		Arguments: map[string]Argument{"RoleName": {Description: descRoleName}},
		SelfRole:  "RoleName",
		Examples:  []Example{{Description: "Delete a role", Arguments: `{"role_name":"old-web"}`}},
	}, "delete", "non-idempotent", "role"),

	iamWrite(Operation{
		Operation: "CreateInstanceProfile", Object: "instance-profile", Action: "create",
		Title: "Create an IAM instance profile",
		Description: "Create an IAM instance profile in the bound account, optionally with path and tags; returns the " +
			"new instance profile, which holds no role yet. Needs iam:CreateInstanceProfile, and " +
			"iam:TagInstanceProfile with tags. Not repeated after an unclear outcome. " + iamWriteRule,
		Arguments: map[string]Argument{
			"InstanceProfileName": {Description: descProfileName},
			"Path":                {Description: descPath},
			"Tags":                {Description: descTags},
		},
		Fields:    map[string]Field{"InstanceProfile": {Description: "The new instance profile", Required: true}},
		Truncated: descTruncatedRole,
		Examples:  []Example{{Description: "Create an instance profile", Arguments: `{"instance_profile_name":"web"}`}},
	}, "create", "non-idempotent", "instance-profile"),
	iamWrite(Operation{
		Operation: "DeleteInstanceProfile", Object: "instance-profile", Action: "delete",
		Title: "Delete an IAM instance profile",
		Description: "Delete an IAM instance profile of the bound account. AWS refuses it with a conflict while it " +
			"holds a role. Needs iam:DeleteInstanceProfile. " + iamWriteRule,
		Arguments: map[string]Argument{"InstanceProfileName": {Description: descProfileName}},
		Examples:  []Example{{Description: "Delete an instance profile", Arguments: `{"instance_profile_name":"old-web"}`}},
	}, "delete", "non-idempotent", "instance-profile"),
	iamWrite(Operation{
		Operation: "AddRoleToInstanceProfile", Object: "instance-profile", Action: "add-role",
		Title: "Add an IAM role to an instance profile",
		Description: "Add an IAM role of the bound account to an instance profile, so EC2 instances started with the " +
			"profile receive the role's credentials. An instance profile holds one role. Needs " +
			"iam:AddRoleToInstanceProfile and iam:PassRole for the role. " + iamWriteRule,
		Arguments: map[string]Argument{
			"InstanceProfileName": {Description: descProfileName},
			"RoleName":            {Description: descRoleName},
		},
		Examples: []Example{{Description: "Put a role into an instance profile", Arguments: `{"instance_profile_name":"web","role_name":"web"}`}},
	}, "update", "non-idempotent", "instance-profile", "role"),
	iamWrite(Operation{
		Operation: "RemoveRoleFromInstanceProfile", Object: "instance-profile", Action: "remove-role",
		Title: "Remove an IAM role from an instance profile",
		Description: "Remove an IAM role from an instance profile of the bound account; instances that use the profile " +
			"lose the role's credentials. Needs iam:RemoveRoleFromInstanceProfile. " + iamOwnRole + " " + iamWriteRule,
		Arguments: map[string]Argument{
			"InstanceProfileName": {Description: descProfileName},
			"RoleName":            {Description: descRoleName},
		},
		SelfRole: "RoleName",
		Examples: []Example{{Description: "Take a role out of an instance profile", Arguments: `{"instance_profile_name":"web","role_name":"web"}`}},
	}, "update", "non-idempotent", "instance-profile", "role"),
}
