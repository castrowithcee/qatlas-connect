package gen

const (
	iamOwnUser = "Refused for the IAM user whose access key the connection signs with, which the tool asks " +
		"sts:GetCallerIdentity for first; needs sts:GetCallerIdentity, which is free."

	descNewUserName  = "New name of the user, 1 to 64 letters, digits, or the characters +=,.@_-; the name is kept when left out"
	descNewGroupName = "New name of the group, 1 to 128 letters, digits, or the characters +=,.@_-; the name is kept when left out"
	descNewPath      = "New path, 1 to 512 characters, a single slash or starting and ending with one; the path is kept when left out"
	descTagKeys      = "Keys of the tags to remove, 1 to 50, each 1 to 128 characters"
)

// iamUserWriteTools are the IAM operations that change and delete existing users, tag them, and create, change,
// and delete groups and their membership. CreateUser stays excluded.
var iamUserWriteTools = []Operation{
	iamWrite(Operation{
		Operation: "UpdateUser", Object: "user", Action: "update",
		Title: "Rename an IAM user or change its path",
		Description: "Change the name or the path of an IAM user of the bound account. Policies, group memberships, and " +
			"credentials stay with the user, but policies and applications that name the user by its old name or ARN " +
			"no longer match it. Needs iam:UpdateUser. " + iamWriteRule,
		Arguments: map[string]Argument{
			"UserName":    {Description: descExistingUser},
			"NewPath":     {Description: descNewPath},
			"NewUserName": {Description: descNewUserName},
		},
		Examples: []Example{{Description: "Rename a user", Arguments: `{"user_name":"web","new_user_name":"web-app"}`}},
	}, "update", "idempotent", "user"),
	iamWrite(Operation{
		Operation: "DeleteUser", Object: "user", Action: "delete",
		Title: "Delete an IAM user",
		Description: "Delete an IAM user of the bound account. AWS refuses it with a conflict while the user still has " +
			"access keys, a password, MFA devices, certificates, policies, or group memberships; the tool removes none " +
			"of them itself. Needs iam:DeleteUser. " + iamOwnUser + " " + iamWriteRule,
		Arguments: map[string]Argument{"UserName": {Description: descExistingUser}},
		SelfUser:  "UserName",
		Examples:  []Example{{Description: "Delete a user", Arguments: `{"user_name":"old-web"}`}},
	}, "delete", "non-idempotent", "user"),
	iamWrite(Operation{
		Operation: "TagUser", Object: "user", Action: "tag",
		Title: "Tag an IAM user",
		Description: "Add tags to an IAM user of the bound account or replace the values of existing keys. Needs " +
			"iam:TagUser. Tags can grant access through policy conditions. " + iamWriteRule,
		Arguments: map[string]Argument{"UserName": {Description: descExistingUser}, "Tags": {Description: descTags}},
		Examples:  []Example{{Description: "Tag a user with its team", Arguments: `{"user_name":"web","tags":[{"key":"team","value":"web"}]}`}},
	}, "update", "idempotent", "user", "tags"),
	iamWrite(Operation{
		Operation: "UntagUser", Object: "user", Action: "untag",
		Title:       "Remove tags from an IAM user",
		Description: "Remove tags from an IAM user of the bound account by key. Needs iam:UntagUser. " + iamWriteRule,
		Arguments:   map[string]Argument{"UserName": {Description: descExistingUser}, "TagKeys": {Description: descTagKeys}},
		Examples:    []Example{{Description: "Remove a tag from a user", Arguments: `{"user_name":"web","tag_keys":["team"]}`}},
	}, "update", "idempotent", "user", "tags"),
	iamWrite(Operation{
		Operation: "CreateGroup", Object: "group", Action: "create",
		Title: "Create an IAM group",
		Description: "Create an IAM group in the bound account, with an optional path; returns the new group. It has " +
			"no members and no permissions. Needs iam:CreateGroup. Not repeated after an unclear outcome: read the " +
			"groups before trying again. " + iamWriteRule,
		Arguments: map[string]Argument{"Path": {Description: descPath}, "GroupName": {Description: descGroupName}},
		Fields:    map[string]Field{"Group": {Description: "The new group", Required: true}},
		Examples:  []Example{{Description: "Create a group", Arguments: `{"group_name":"developers"}`}},
	}, "create", "non-idempotent", "group"),
	iamWrite(Operation{
		Operation: "UpdateGroup", Object: "group", Action: "update",
		Title: "Rename an IAM group or change its path",
		Description: "Change the name or the path of an IAM group of the bound account. Members and policies stay with " +
			"the group, but policies and applications that name the group by its old name or ARN no longer match it. " +
			"Needs iam:UpdateGroup. " + iamWriteRule,
		Arguments: map[string]Argument{
			"GroupName":    {Description: descGroupName},
			"NewPath":      {Description: descNewPath},
			"NewGroupName": {Description: descNewGroupName},
		},
		Examples: []Example{{Description: "Rename a group", Arguments: `{"group_name":"developers","new_group_name":"engineers"}`}},
	}, "update", "idempotent", "group"),
	iamWrite(Operation{
		Operation: "DeleteGroup", Object: "group", Action: "delete",
		Title: "Delete an IAM group",
		Description: "Delete an IAM group of the bound account. AWS refuses it with a conflict while the group still has " +
			"members or policies; the tool removes none of them itself. Needs iam:DeleteGroup. " + iamWriteRule,
		Arguments: map[string]Argument{"GroupName": {Description: descGroupName}},
		Examples:  []Example{{Description: "Delete a group", Arguments: `{"group_name":"old-developers"}`}},
	}, "delete", "non-idempotent", "group"),
	iamWrite(Operation{
		Operation: "AddUserToGroup", Object: "group", Action: "add-user",
		Title: "Add an IAM user to an IAM group",
		Description: "Add an existing IAM user of the bound account to an existing IAM group, which gives the user the " +
			"permissions of the group. Needs iam:AddUserToGroup. " + iamWriteRule,
		Arguments: map[string]Argument{"GroupName": {Description: descGroupName}, "UserName": {Description: descExistingUser}},
		Examples:  []Example{{Description: "Add a user to a group", Arguments: `{"group_name":"developers","user_name":"web"}`}},
	}, "update", "non-idempotent", "group", "user"),
	iamWrite(Operation{
		Operation: "RemoveUserFromGroup", Object: "group", Action: "remove-user",
		Title: "Remove an IAM user from an IAM group",
		Description: "Remove an IAM user of the bound account from an IAM group, which takes the permissions of the group " +
			"from the user. Needs iam:RemoveUserFromGroup. " + iamOwnUser + " " + iamWriteRule,
		Arguments: map[string]Argument{"GroupName": {Description: descGroupName}, "UserName": {Description: descExistingUser}},
		SelfUser:  "UserName",
		Examples:  []Example{{Description: "Remove a user from a group", Arguments: `{"group_name":"developers","user_name":"web"}`}},
	}, "update", "non-idempotent", "group", "user"),
}
