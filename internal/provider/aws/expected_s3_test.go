package aws

// Tools of the S3 tables that name a guard or a readback.

func init() {
	expectGuarded(
		"aws.s3.analytics.put",
		"aws.s3.bucket.create",
		"aws.s3.encryption.put",
		"aws.s3.inventory.put",
		"aws.s3.multipart-upload.create",
		"aws.s3.multipart-upload.upload-part-copy",
		"aws.s3.object-lock.put",
		"aws.s3.object-retention.put",
		"aws.s3.object.copy",
		"aws.s3.object.upload",
		"aws.s3.ownership.put",
	)
	expectReadback(
		"aws.s3.analytics.put",
		"aws.s3.inventory.put",
		"aws.s3.public-access-block.put",
	)
	expectSummary(
		"aws.s3.cors.put",
		"aws.s3.lifecycle.put",
		"aws.s3.object-legal-hold.put",
		"aws.s3.object-lock.put",
		"aws.s3.object-retention.put",
		"aws.s3.request-payment.put",
	)
}
