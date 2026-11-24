package aws

// The IAM access key tools carry a confirmation text.

func init() {
	expectSummary(
		"aws.iam.access-key.deactivate",
		"aws.iam.access-key.delete",
	)
}
