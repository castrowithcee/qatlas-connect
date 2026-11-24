package aws

// The IAM policy write tools carry a confirmation text.

func init() {
	expectSummary(
		"aws.iam.attached-policy.attach-to-group",
		"aws.iam.attached-policy.attach-to-role",
		"aws.iam.attached-policy.attach-to-user",
		"aws.iam.attached-policy.detach-from-group",
		"aws.iam.attached-policy.detach-from-role",
		"aws.iam.attached-policy.detach-from-user",
		"aws.iam.inline-policy.delete-for-group",
		"aws.iam.inline-policy.delete-for-role",
		"aws.iam.inline-policy.delete-for-user",
		"aws.iam.inline-policy.put-for-group",
		"aws.iam.inline-policy.put-for-role",
		"aws.iam.inline-policy.put-for-user",
		"aws.iam.policy-version.create",
		"aws.iam.policy-version.delete",
		"aws.iam.policy-version.set-default",
		"aws.iam.policy.create",
		"aws.iam.policy.delete",
		"aws.iam.policy.tag",
		"aws.iam.policy.untag",
		"aws.iam.user.delete-permissions-boundary",
		"aws.iam.user.put-permissions-boundary",
	)
}
