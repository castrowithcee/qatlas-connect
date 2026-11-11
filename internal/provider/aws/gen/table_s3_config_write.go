package gen

import "slices"

const s3Lifecycle = "LifecycleConfiguration.Rules"

// lifecycleLimits cap the lists and strings of a lifecycle configuration that the model leaves unbounded. The caps
// are protective and above what S3 enforces (1000 rules, 255 characters of a rule ID, 1024 of a key prefix, 128 and
// 256 of a tag); S3 enforces its own.
func lifecycleLimits() []InputLimit {
	const why = "the model sets no bound; a protective cap above the limits S3 enforces"
	r := s3Lifecycle
	out := []InputLimit{
		{Member: r, MaxItems: 1000, Reason: why},
		{Member: r + ".ID", MaxLen: 255, Reason: why},
		{Member: r + ".Filter.Prefix", MaxLen: 1024, Reason: why},
		{Member: r + ".Filter.Tag.Key", MaxLen: 256, Reason: why},
		{Member: r + ".Filter.Tag.Value", MaxLen: 512, Reason: why},
		{Member: r + ".Filter.And.Prefix", MaxLen: 1024, Reason: why},
		{Member: r + ".Filter.And.Tags", MaxItems: 100, Reason: why},
		{Member: r + ".Filter.And.Tags.Key", MaxLen: 256, Reason: why},
		{Member: r + ".Filter.And.Tags.Value", MaxLen: 512, Reason: why},
		{Member: r + ".Transitions", MaxItems: 100, Reason: why},
		{Member: r + ".NoncurrentVersionTransitions", MaxItems: 100, Reason: why},
	}
	return out
}

const (
	corsRules = "CORSConfiguration.CORSRules"
	// s3CORSNote is part of the confirmation text of the CORS write.
	s3CORSNote = " An origin of * lets a browser on any website read the bucket's responses; CORS is not a permission, and the " +
		"bucket policy, the ACLs, and the public access block still decide who may use the bucket."
)

func corsLimits() []InputLimit {
	const why = "the model sets no bound; a protective cap above the limits S3 enforces (100 rules)"
	r := corsRules
	return []InputLimit{
		{Member: r, MaxItems: 100, Reason: why},
		{Member: r + ".ID", MaxLen: 255, Reason: why},
		{Member: r + ".AllowedHeaders", MaxItems: 100, Reason: why},
		{Member: r + ".AllowedHeaders", MaxLen: 256, Reason: why},
		{Member: r + ".AllowedMethods", MaxItems: 10, Reason: why},
		{Member: r + ".AllowedOrigins", MaxItems: 100, Reason: why},
		{Member: r + ".AllowedOrigins", MaxLen: 512, Reason: why},
		{Member: r + ".ExposeHeaders", MaxItems: 100, Reason: why},
		{Member: r + ".ExposeHeaders", MaxLen: 256, Reason: why},
	}
}

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

// s3ConfigWriteTools are the lifecycle and CORS writes: replacing and removing the rules of a bucket.
var s3ConfigWriteTools = []Operation{
	s3BucketWrite(Operation{
		Operation: "PutBucketLifecycleConfiguration", Object: "lifecycle", Action: "put", Effect: "update", Idempotency: "idempotent",
		Title: "Set the lifecycle rules of an S3 bucket",
		Description: "Replace all lifecycle rules of a bucket with the given ones; rules not given are removed, so repeat what is to " +
			"stay. A rule with expiration deletes objects after the given days or on the given date, and a rule with " +
			"noncurrent version expiration deletes noncurrent versions after the given days, each limited to the objects " +
			"its filter selects (prefix, tag, size). A rule without a filter, or with an empty prefix, covers the whole " +
			"bucket. The old rule-level prefix is not offered: when writing back a rule that a read showed with a rule-level prefix, set filter.prefix to the same value, or the rule covers the whole bucket. Deleted data cannot be restored unless versioning keeps it. Rules with transitions move objects " +
			"to other storage classes, which S3 bills. Each rule needs an ID and the status Enabled or Disabled. " +
			"Needs s3:PutLifecycleConfiguration.",
		Arguments: map[string]Argument{
			"LifecycleConfiguration":             {Description: "rules, each with id, status, filter (prefix, tag with key and value, object_size_greater_than, object_size_less_than, or and with prefix, tags and sizes), expiration (days, date, or expired_object_delete_marker), transitions (days or date, storage_class), noncurrent_version_expiration (noncurrent_days, newer_noncurrent_versions), noncurrent_version_transitions, and abort_incomplete_multipart_upload (days_after_initiation)"},
			"TransitionDefaultMinimumObjectSize": {Description: "all_storage_classes_128k or varies_by_storage_class: the default minimum object size for transitions"},
		},
		Fields: map[string]Field{
			"TransitionDefaultMinimumObjectSize": {Description: "The default minimum object size for transitions that S3 applied"},
		},
		Limits: lifecycleLimits(),
		Examples: []Example{{Description: "Delete objects under logs/ after 30 days",
			Arguments: `{"bucket":"my-bucket","lifecycle_configuration":{"rules":[{"id":"expire-logs","status":"Enabled","filter":{"prefix":"logs/"},"expiration":{"days":30}}]}}`}},
	}, "lifecycle"),
	s3BucketWrite(Operation{
		Operation: "DeleteBucketLifecycle", Object: "lifecycle", Action: "delete", Effect: "delete", Idempotency: "non-idempotent",
		Title: "Remove the lifecycle rules of an S3 bucket",
		Description: "Remove all lifecycle rules of a bucket: no object expires or moves by rule any more. Needs " +
			"s3:PutLifecycleConfiguration.",
		Examples: []Example{{Description: "Remove all lifecycle rules", Arguments: `{"bucket":"my-bucket"}`}},
	}, "lifecycle"),
	s3BucketWrite(Operation{
		Operation: "PutBucketCors", Object: "cors", Action: "put", Effect: "update", Idempotency: "idempotent",
		Title: "Set the CORS rules of an S3 bucket",
		Description: "Replace all CORS rules of a bucket with the given ones, each with allowed origins, methods, and headers, " +
			"exposed headers, a max age, and an ID; rules not given are removed, so repeat what is to stay." + s3CORSNote +
			" Needs s3:PutBucketCORS.",
		Arguments: map[string]Argument{
			"CORSConfiguration": {Description: "cors_rules, each with allowed_origins and allowed_methods (required), allowed_headers, expose_headers, max_age_seconds, and id"},
		},
		Limits: corsLimits(),
		Examples: []Example{{Description: "Allow GET from one website",
			Arguments: `{"bucket":"my-bucket","cors_configuration":{"cors_rules":[{"allowed_origins":["https://example.com"],"allowed_methods":["GET"]}]}}`}},
	}, "cors"),
	s3BucketWrite(Operation{
		Operation: "DeleteBucketCors", Object: "cors", Action: "delete", Effect: "delete", Idempotency: "non-idempotent",
		Title:       "Remove the CORS rules of an S3 bucket",
		Description: "Remove all CORS rules of a bucket: browsers on other websites can no longer read its responses. Needs s3:PutBucketCORS.",
		Examples:    []Example{{Description: "Remove all CORS rules", Arguments: `{"bucket":"my-bucket"}`}},
	}, "cors"),
}
