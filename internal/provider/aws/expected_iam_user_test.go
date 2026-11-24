package aws

// The IAM user and group write tools carry a confirmation text.

func init() {
	expectSummary(
		"aws.iam.group.add-user",
		"aws.iam.group.create",
		"aws.iam.group.delete",
		"aws.iam.group.remove-user",
		"aws.iam.group.update",
		"aws.iam.user.delete",
		"aws.iam.user.tag",
		"aws.iam.user.untag",
		"aws.iam.user.update",
	)
}
