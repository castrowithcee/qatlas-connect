package aws

// Tools of the IAM tables that carry a confirmation text.

func init() {
	expectSummary(
		"aws.iam.instance-profile.add-role",
		"aws.iam.instance-profile.create",
		"aws.iam.instance-profile.delete",
		"aws.iam.instance-profile.remove-role",
		"aws.iam.role.create",
		"aws.iam.role.delete",
		"aws.iam.role.delete-permissions-boundary",
		"aws.iam.role.put-permissions-boundary",
		"aws.iam.role.tag",
		"aws.iam.role.untag",
		"aws.iam.role.update",
		"aws.iam.role.update-trust-policy",
	)
}
