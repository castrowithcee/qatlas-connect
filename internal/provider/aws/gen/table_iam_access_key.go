package gen

const (
	iamOwnKey = "Refused for the access key the connection signs with, found by comparing the key ID with the " +
		"credential and the signing credentials of the connection; it needs no call to AWS."

	descAccessKeyID       = "ID of the access key, 16 to 128 letters, digits, or underscores"
	descAccessKeyUserName = "Name of the user the key belongs to, 1 to 128 letters, digits, or the characters +=,.@_-; the user of the signing credentials when left out"
)

// iamAccessKeyTools are the IAM operations that deactivate and delete access keys. UpdateAccessKey always
// sends Status=Inactive, so a key is never reactivated; CreateAccessKey stays excluded.
var iamAccessKeyTools = []Operation{
	iamWrite(Operation{
		Operation: "UpdateAccessKey", Object: "access-key", Action: "deactivate",
		Title: "Deactivate an IAM access key",
		Description: "Deactivate an access key of an IAM user of the bound account: requests signed with the key are " +
			"refused from then on. The tool only sets the status Inactive; a key is never reactivated or created " +
			"through a tool. Needs iam:UpdateAccessKey. " + iamOwnKey + " " + iamWriteRule,
		Arguments: map[string]Argument{
			"AccessKeyId": {Description: descAccessKeyID},
			"UserName":    {Description: descAccessKeyUserName},
		},
		Fixed:   map[string]string{"Status": "Inactive"},
		SelfKey: "AccessKeyId",
		Examples: []Example{{Description: "Deactivate the key of a user", Arguments: `{"access_key_id":"` +
			sampleKeyID + `","user_name":"web"}`}},
	}, "update", "idempotent", "access-key"),
	iamWrite(Operation{
		Operation: "DeleteAccessKey", Object: "access-key", Action: "delete",
		Title: "Delete an IAM access key",
		Description: "Delete an access key of an IAM user of the bound account for good; this cannot be undone. " +
			"Needs iam:DeleteAccessKey. " + iamOwnKey + " " + iamWriteRule,
		Arguments: map[string]Argument{
			"AccessKeyId": {Description: descAccessKeyID},
			"UserName":    {Description: descAccessKeyUserName},
		},
		SelfKey: "AccessKeyId",
		Examples: []Example{{Description: "Delete the key of a user", Arguments: `{"access_key_id":"` +
			sampleKeyID + `","user_name":"web"}`}},
	}, "delete", "non-idempotent", "access-key"),
}

// sampleKeyID is the example key ID of the tool descriptions; it is no real key.
const sampleKeyID = "EXAMPLEKEYID000001"
