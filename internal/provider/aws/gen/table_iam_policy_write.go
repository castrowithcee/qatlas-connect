package gen

const (
	iamOwnIdentity = "Refused for the IAM user or role whose credentials the connection uses, which the tool asks " +
		"sts:GetCallerIdentity for first, so a connection never gives itself or takes from itself permissions; needs " +
		"sts:GetCallerIdentity, which is free."
	iamIdentityPolicyRule = "The policy document is checked before any request: valid JSON with Version and a Statement " +
		"that is an object or a list, a key given twice or spelled in another case is refused, as is any statement with " +
		"Principal or NotPrincipal, and a document over 131072 characters. How wide the permissions reach is not judged: " +
		"the confirmation is the safeguard."
	iamPolicyGrants = "Policies grant permissions."

	descNewPolicyName    = "Name of the policy, 1 to 128 letters, digits, or the characters +=,.@_-"
	descPolicyDocument   = "Policy document as JSON text, 1 to 131072 characters, checked before any request as the description says"
	descPolicyDesc       = "Description of the policy, 0 to 1000 characters; it cannot be changed later"
	descPolicyVersionID  = "ID of the policy version, such as v1"
	descInlinePolicyName = "Name of the inline policy, 1 to 128 letters, digits, or the characters +=,.@_-"
	descExistingUser     = "Name of the user in the bound account, 1 to 128 letters, digits, or the characters +=,.@_-"
	descBoundaryUser     = "Name of the user in the bound account, 1 to 64 letters, digits, or the characters +=,.@_-"
	descGroupName        = "Name of the group in the bound account, 1 to 128 letters, digits, or the characters +=,.@_-"
	descPolicyTags       = "Tags, 1 to 50, each with key (1 to 128 characters) and value (0 to 256 characters)"
)

// policyTarget is one kind of identity a policy tool attaches to or writes for: its name in tool IDs, its model
// member, and the own-identity check that applies to it. Groups have none: a group is never the caller.
type policyTarget struct {
	kind, name, desc string
	selfRole         string
	selfUser         string
}

var policyTargets = []policyTarget{
	{kind: "user", name: "UserName", desc: descExistingUser, selfUser: "UserName"},
	{kind: "group", name: "GroupName", desc: descGroupName},
	{kind: "role", name: "RoleName", desc: descRoleName, selfRole: "RoleName"},
}

// policyTargetWrite adds the own-identity check and the policy checks every attach, detach, and inline policy
// tool shares.
func policyTargetWrite(op Operation, id policyTarget, effect, idempotency string, tags ...string) Operation {
	op.SelfRole, op.SelfUser = id.selfRole, id.selfUser
	return iamWrite(op, effect, idempotency, append([]string{id.kind}, tags...)...)
}

func policyTargetGuard(id policyTarget) string {
	if id.selfRole == "" && id.selfUser == "" {
		return ""
	}
	return " " + iamOwnIdentity
}

func policyTargetTools() []Operation {
	var ops []Operation
	for _, id := range policyTargets {
		ops = append(ops,
			policyTargetWrite(Operation{
				Operation: "Attach" + title(id.kind) + "Policy", Object: "attached-policy", Action: "attach-to-" + id.kind,
				Title: "Attach a managed policy to an IAM " + id.kind,
				Description: "Attach a managed policy to an existing IAM " + id.kind + " of the bound account, which " +
					"grants it the permissions of the policy. Needs iam:Attach" + title(id.kind) + "Policy. " +
					iamPolicyGrants + policyTargetGuard(id) + " " + iamWriteRule,
				Arguments:   map[string]Argument{id.name: {Description: id.desc}, "PolicyArn": {Description: descPolicyArn}},
				TargetCheck: "arn-account-or-managed-policy", TargetArgs: []string{"PolicyArn"},
				Examples: []Example{{Description: "Attach a read-only AWS managed policy",
					Arguments: `{"` + snake(id.name) + `":"web","policy_arn":"arn:aws:iam::aws:policy/ReadOnlyAccess"}`}},
			}, id, "update", "idempotent", "policy"),
			policyTargetWrite(Operation{
				Operation: "Detach" + title(id.kind) + "Policy", Object: "attached-policy", Action: "detach-from-" + id.kind,
				Title: "Detach a managed policy from an IAM " + id.kind,
				Description: "Detach a managed policy from an IAM " + id.kind + " of the bound account, which removes " +
					"the permissions of the policy from it. Needs iam:Detach" + title(id.kind) + "Policy." +
					policyTargetGuard(id) + " " + iamWriteRule,
				Arguments:   map[string]Argument{id.name: {Description: id.desc}, "PolicyArn": {Description: descPolicyArn}},
				TargetCheck: "arn-account-or-managed-policy", TargetArgs: []string{"PolicyArn"},
				Examples: []Example{{Description: "Detach a managed policy",
					Arguments: `{"` + snake(id.name) + `":"web","policy_arn":"arn:aws:iam::aws:policy/ReadOnlyAccess"}`}},
			}, id, "update", "idempotent", "policy"),
			policyTargetWrite(Operation{
				Operation: "Put" + title(id.kind) + "Policy", Object: "inline-policy", Action: "put-for-" + id.kind,
				Title: "Write an inline policy of an IAM " + id.kind,
				Description: "Add or replace an inline policy of an existing IAM " + id.kind + " of the bound account. " +
					"Needs iam:Put" + title(id.kind) + "Policy. " + iamPolicyGrants + " " + iamIdentityPolicyRule +
					policyTargetGuard(id) + " " + iamWriteRule,
				Arguments: map[string]Argument{id.name: {Description: id.desc},
					"PolicyName": {Description: descInlinePolicyName}, "PolicyDocument": {Description: descPolicyDocument}},
				PolicyCheck: "identity-policy", PolicyArgs: []string{"PolicyDocument"},
				Examples: []Example{{Description: "Allow reading one bucket",
					Arguments: `{"` + snake(id.name) + `":"web","policy_name":"read-logs","policy_document":"{\"Version\":\"2012-10-17\",\"Statement\":[{\"Effect\":\"Allow\",\"Action\":\"s3:GetObject\",\"Resource\":\"arn:aws:s3:::example-logs/*\"}]}"}`}},
			}, id, "update", "idempotent", "inline-policy", "policy-document"),
			policyTargetWrite(Operation{
				Operation: "Delete" + title(id.kind) + "Policy", Object: "inline-policy", Action: "delete-for-" + id.kind,
				Title: "Delete an inline policy of an IAM " + id.kind,
				Description: "Delete an inline policy of an IAM " + id.kind + " of the bound account. Needs iam:Delete" +
					title(id.kind) + "Policy." + policyTargetGuard(id) + " " + iamWriteRule,
				Arguments: map[string]Argument{id.name: {Description: id.desc}, "PolicyName": {Description: descInlinePolicyName}},
				Examples:  []Example{{Description: "Delete an inline policy", Arguments: `{"` + snake(id.name) + `":"web","policy_name":"read-logs"}`}},
			}, id, "delete", "non-idempotent", "inline-policy"),
		)
	}
	return ops
}

func title(kind string) string { return string(kind[0]-32) + kind[1:] }

// iamPolicyWriteTools are the IAM operations that create, change, tag, and delete managed policies and their
// versions, put the permissions boundary of a user, and attach policies to identities or write them inline.
var iamPolicyWriteTools = append([]Operation{
	iamWrite(Operation{
		Operation: "CreatePolicy", Object: "policy", Action: "create",
		Title: "Create an IAM managed policy",
		Description: "Create a managed policy in the bound account with the given document, and optionally path, " +
			"description, and tags; returns the new policy. It grants nothing until it is attached. Needs " +
			"iam:CreatePolicy, and iam:TagPolicy with tags. Not repeated after an unclear outcome: read the policy " +
			"before trying again. " + iamIdentityPolicyRule + " " + iamWriteRule,
		Arguments: map[string]Argument{
			"PolicyName":     {Description: descNewPolicyName},
			"Path":           {Description: descPath},
			"PolicyDocument": {Description: descPolicyDocument},
			"Description":    {Description: descPolicyDesc},
			"Tags":           {Description: descTags},
		},
		Fields:      map[string]Field{"Policy": {Description: "The new policy", Required: true}},
		PolicyCheck: "identity-policy", PolicyArgs: []string{"PolicyDocument"},
		Examples: []Example{{Description: "Create a policy that reads one bucket",
			Arguments: `{"policy_name":"read-logs","policy_document":"{\"Version\":\"2012-10-17\",\"Statement\":[{\"Effect\":\"Allow\",\"Action\":\"s3:GetObject\",\"Resource\":\"arn:aws:s3:::example-logs/*\"}]}"}`}},
	}, "create", "non-idempotent", "policy", "policy-document"),
	withPolicyArn(iamWrite(Operation{
		Operation: "DeletePolicy", Object: "policy", Action: "delete",
		Title: "Delete an IAM managed policy",
		Description: "Delete a managed policy of the bound account. AWS refuses it with a conflict while the policy is " +
			"attached to an identity or has versions other than the default; the tool detaches and deletes nothing " +
			"itself. Needs iam:DeletePolicy. " + iamWriteRule,
		Arguments: map[string]Argument{"PolicyArn": {Description: descPolicyArn}},
		Examples:  []Example{{Description: "Delete a policy", Arguments: `{"policy_arn":"arn:aws:iam::123456789012:policy/old"}`}},
	}, "delete", "non-idempotent", "policy")),
	withPolicyArn(iamWrite(Operation{
		Operation: "CreatePolicyVersion", Object: "policy-version", Action: "create",
		Title: "Create a version of an IAM managed policy",
		Description: "Add a version with the given document to a managed policy, and optionally make it the default, " +
			"which changes the permissions of everything the policy is attached to; returns the new version. A policy " +
			"holds at most five versions. Needs iam:CreatePolicyVersion. Not repeated after an unclear outcome. " +
			iamIdentityPolicyRule + " " + iamWriteRule,
		Arguments: map[string]Argument{
			"PolicyArn":      {Description: descPolicyArn},
			"PolicyDocument": {Description: descPolicyDocument},
			"SetAsDefault":   {Description: "True makes the new version the default; false when left out"},
		},
		Fields:      map[string]Field{"PolicyVersion": {Description: "The new version; its document is not returned", Required: true}},
		PolicyCheck: "identity-policy", PolicyArgs: []string{"PolicyDocument"},
		Examples: []Example{{Description: "Add a version and make it the default",
			Arguments: `{"policy_arn":"arn:aws:iam::123456789012:policy/read-logs","set_as_default":true,"policy_document":"{\"Version\":\"2012-10-17\",\"Statement\":[{\"Effect\":\"Allow\",\"Action\":\"s3:GetObject\",\"Resource\":\"*\"}]}"}`}},
	}, "create", "non-idempotent", "policy", "policy-version", "policy-document")),
	withPolicyArn(iamWrite(Operation{
		Operation: "SetDefaultPolicyVersion", Object: "policy-version", Action: "set-default",
		Title: "Make a version of an IAM managed policy the default",
		Description: "Make a version of a managed policy the default, which changes the permissions of everything the " +
			"policy is attached to. Needs iam:SetDefaultPolicyVersion. " + iamWriteRule,
		Arguments: map[string]Argument{"PolicyArn": {Description: descPolicyArn}, "VersionId": {Description: descPolicyVersionID}},
		Examples:  []Example{{Description: "Roll back to version 1", Arguments: `{"policy_arn":"arn:aws:iam::123456789012:policy/read-logs","version_id":"v1"}`}},
	}, "update", "idempotent", "policy", "policy-version")),
	withPolicyArn(iamWrite(Operation{
		Operation: "DeletePolicyVersion", Object: "policy-version", Action: "delete",
		Title: "Delete a version of an IAM managed policy",
		Description: "Delete a version of a managed policy; the default version cannot be deleted. Needs " +
			"iam:DeletePolicyVersion. " + iamWriteRule,
		Arguments: map[string]Argument{"PolicyArn": {Description: descPolicyArn}, "VersionId": {Description: descPolicyVersionID}},
		Examples:  []Example{{Description: "Delete an old version", Arguments: `{"policy_arn":"arn:aws:iam::123456789012:policy/read-logs","version_id":"v1"}`}},
	}, "delete", "non-idempotent", "policy", "policy-version")),
	withPolicyArn(iamWrite(Operation{
		Operation: "TagPolicy", Object: "policy", Action: "tag",
		Title: "Tag an IAM managed policy",
		Description: "Add tags to a managed policy or replace the values of existing keys. Needs iam:TagPolicy. Tags " +
			"can grant access through policy conditions. " + iamWriteRule,
		Arguments: map[string]Argument{"PolicyArn": {Description: descPolicyArn}, "Tags": {Description: descPolicyTags}},
		Examples:  []Example{{Description: "Tag a policy with its team", Arguments: `{"policy_arn":"arn:aws:iam::123456789012:policy/read-logs","tags":[{"key":"team","value":"web"}]}`}},
	}, "update", "idempotent", "policy", "tags")),
	withPolicyArn(iamWrite(Operation{
		Operation: "UntagPolicy", Object: "policy", Action: "untag",
		Title:       "Remove tags from an IAM managed policy",
		Description: "Remove tags from a managed policy by key. Needs iam:UntagPolicy. " + iamWriteRule,
		Arguments: map[string]Argument{
			"PolicyArn": {Description: descPolicyArn},
			"TagKeys":   {Description: "Keys of the tags to remove, 1 to 50, each 1 to 128 characters"},
		},
		Examples: []Example{{Description: "Remove a tag from a policy", Arguments: `{"policy_arn":"arn:aws:iam::123456789012:policy/read-logs","tag_keys":["team"]}`}},
	}, "update", "idempotent", "policy", "tags")),
	policyTargetWrite(Operation{
		Operation: "PutUserPermissionsBoundary", Object: "user", Action: "put-permissions-boundary",
		Title: "Set the permissions boundary of an IAM user",
		Description: "Set or replace the managed policy that bounds the permissions of an IAM user of the bound " +
			"account. Needs iam:PutUserPermissionsBoundary. " + iamOwnIdentity + " " + iamWriteRule,
		Arguments:   map[string]Argument{"UserName": {Description: descBoundaryUser}, "PermissionsBoundary": {Description: descBoundary}},
		TargetCheck: "arn-account-or-managed-policy", TargetArgs: []string{"PermissionsBoundary"},
		Examples: []Example{{Description: "Bound a user by an AWS managed policy",
			Arguments: `{"user_name":"web","permissions_boundary":"arn:aws:iam::aws:policy/PowerUserAccess"}`}},
	}, policyTargets[0], "update", "idempotent", "permissions-boundary"),
	policyTargetWrite(Operation{
		Operation: "DeleteUserPermissionsBoundary", Object: "user", Action: "delete-permissions-boundary",
		Title: "Remove the permissions boundary of an IAM user",
		Description: "Remove the permissions boundary of an IAM user of the bound account, which widens what the user " +
			"may do to its permission policies. Needs iam:DeleteUserPermissionsBoundary. " + iamOwnIdentity + " " + iamWriteRule,
		Arguments: map[string]Argument{"UserName": {Description: descBoundaryUser}},
		Examples:  []Example{{Description: "Remove the boundary of a user", Arguments: `{"user_name":"web"}`}},
	}, policyTargets[0], "delete", "non-idempotent", "permissions-boundary"),
}, policyTargetTools()...)
