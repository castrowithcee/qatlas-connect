package gen

// Operations of the S3 curation tables that name a guard or a readback.

func init() {
	expectGuarded(
		"CopyObject",
		"CreateBucket",
		"CreateMultipartUpload",
		"PutBucketAnalyticsConfiguration",
		"PutBucketEncryption",
		"PutBucketInventoryConfiguration",
		"PutBucketOwnershipControls",
		"PutObject",
		"PutObjectLockConfiguration",
		"PutObjectRetention",
		"UploadPartCopy",
	)
	expectReadback(
		"PutBucketAnalyticsConfiguration",
		"PutBucketInventoryConfiguration",
		"PutPublicAccessBlock",
	)
}
