package gen

import "slices"

// s3ConfigTools are the lifecycle and CORS readers and the readers without a writing tool.
var s3ConfigTools = slices.Concat([]Operation{
	s3BucketRead(Operation{
		Operation: "GetBucketLifecycleConfiguration", Object: "lifecycle", Action: "get",
		Title: "Get the lifecycle rules of an S3 bucket",
		Description: "Return the lifecycle rules of a bucket, each with ID, status, filter (prefix, tag, and, object sizes), " +
			"expiration, transitions, noncurrent version expiration and transitions, and abort of incomplete multipart " +
			"uploads, and the default minimum object size of transitions. A rule of the old form shows its prefix on the " +
			"rule itself (prefix) instead of in a filter; it limits the rule like a filter prefix. A rule with neither a " +
			"filter nor a prefix, or with an empty prefix, covers the whole bucket. Needs s3:GetLifecycleConfiguration. A bucket without rules answers not-found " +
			"(NoSuchLifecycleConfiguration).",
		Fields: map[string]Field{
			"Rules":                              {Description: "Rules, each with id, status, prefix (the old form of a filter prefix), filter, expiration, transitions, noncurrent_version_expiration, noncurrent_version_transitions, and abort_incomplete_multipart_upload; ID, prefix and tags are untrusted text", MaxItems: 1000},
			"TransitionDefaultMinimumObjectSize": {Description: "Default minimum object size for transitions: all_storage_classes_128k or varies_by_storage_class"},
		},
		KeepDeprecated: []Release{{Member: "Rules.Prefix", Reason: "the rule-level prefix is deprecated, but a rule of the old form is limited by it and by no filter; leaving it out would show a prefix-limited rule as one that covers the whole bucket"}},
		Examples:       []Example{{Description: "Read the lifecycle rules of a bucket", Arguments: `{"bucket":"my-bucket"}`}},
	}, "lifecycle"),
	s3BucketRead(Operation{
		Operation: "GetBucketCors", Object: "cors", Action: "get",
		Title: "Get the CORS rules of an S3 bucket",
		Description: "Return the CORS rules of a bucket, each with ID, allowed origins, methods, and headers, exposed headers, and " +
			"max age. Needs s3:GetBucketCORS. A bucket without rules answers not-found (NoSuchCORSConfiguration).",
		Fields: map[string]Field{
			"CORSRules": {Description: "Rules, each with id, allowed_origins, allowed_methods, allowed_headers, expose_headers, and max_age_seconds; untrusted text", MaxItems: 100},
		},
		Examples: []Example{{Description: "Read the CORS rules of a bucket", Arguments: `{"bucket":"my-bucket"}`}},
	}, "cors"),
}, s3ConfigReads)

// s3ConfigWriteTools are the lifecycle and CORS removals and the payer write.
var s3ConfigWriteTools = []Operation{
	s3BucketWrite(Operation{
		Operation: "DeleteBucketLifecycle", Object: "lifecycle", Action: "delete", Effect: "delete", Idempotency: "non-idempotent",
		Title: "Remove the lifecycle rules of an S3 bucket",
		Description: "Remove all lifecycle rules of a bucket: no object expires or moves by rule any more. Needs " +
			"s3:PutLifecycleConfiguration.",
		Examples: []Example{{Description: "Remove all lifecycle rules", Arguments: `{"bucket":"my-bucket"}`}},
	}, "lifecycle"),
	s3BucketWrite(Operation{
		Operation: "DeleteBucketCors", Object: "cors", Action: "delete", Effect: "delete", Idempotency: "non-idempotent",
		Title:       "Remove the CORS rules of an S3 bucket",
		Description: "Remove all CORS rules of a bucket: browsers on other websites can no longer read its responses. Needs s3:PutBucketCORS.",
		Examples:    []Example{{Description: "Remove all CORS rules", Arguments: `{"bucket":"my-bucket"}`}},
	}, "cors"),
	s3BucketWrite(Operation{
		Operation: "PutBucketRequestPayment", Object: "request-payment", Action: "put", Effect: "update", Idempotency: "idempotent",
		Title: "Set who pays for the requests to an S3 bucket",
		Description: "Set the payer of a bucket to Requester or BucketOwner. With Requester the costs of transfer and requests " +
			"move to whoever asks: anonymous access is no longer possible, and requests of other accounts fail unless they " +
			"send x-amz-request-payer. The change can be reverted at any time. Needs s3:PutBucketRequestPayment.",
		Arguments: map[string]Argument{
			"RequestPaymentConfiguration": {Description: "payer: Requester or BucketOwner"},
		},
		Examples: []Example{{Description: "Let requesters pay", Arguments: `{"bucket":"my-bucket","request_payment_configuration":{"payer":"Requester"}}`}},
	}, "request-payment"),
}
