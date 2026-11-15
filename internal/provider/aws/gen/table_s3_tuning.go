package gen

const (
	s3TuningList = " The list is paged: pass the cursor of the result to read the next page."
	s3IDLimit    = "the model sets no bound; a protective cap at the length S3 allows for an ID"
	// Each of the three settings is a configuration of the bucket itself, with no target outside the bound account.
	s3MetricsCost = " Request metrics for a filter are CloudWatch metrics that AWS bills for."
	s3TieringCost = " S3 Intelligent-Tiering charges a monitoring and automation fee per object, and the archive tiers have retrieval delays."
	s3AccelCost   = " Transfer acceleration charges an extra fee per GB transferred through the accelerate endpoint; this tool only " +
		"changes the setting and sends no request through that endpoint. A bucket name with a dot cannot use acceleration; S3 refuses it, and the call fails with its error code."
)

// s3TuningTools are the metrics, Intelligent-Tiering, and transfer acceleration settings of a bucket.
var s3TuningTools = []Operation{
	s3BucketRead(Operation{
		Operation: "ListBucketMetricsConfigurations", Object: "metrics", Action: "list",
		Title: "List the request metrics configurations of an S3 bucket",
		Description: "List the request metrics configurations of a bucket: each with ID and filter (prefix, tag, access point ARN, " +
			"or and). Needs s3:GetMetricsConfiguration. A bucket without any answers an empty list." + s3TuningList + s3MetricsCost,
		Fields: map[string]Field{
			"MetricsConfigurationList": {Description: "Configurations of this page, each as aws.s3.metrics.get returns it", MaxItems: 1000},
			"ContinuationToken":        {Description: "The token this page started at; empty for the first page"},
		},
		Examples: []Example{{Description: "List the metrics configurations of a bucket", Arguments: `{"bucket":"my-bucket"}`}},
	}, "metrics"),
	s3BucketRead(Operation{
		Operation: "GetBucketMetricsConfiguration", Object: "metrics", Action: "get",
		Title: "Get one request metrics configuration of an S3 bucket",
		Description: "Return one request metrics configuration of a bucket by its ID: the filter (prefix, tag, access point ARN, or and). " +
			"Needs s3:GetMetricsConfiguration. An unknown ID answers not-found (NoSuchConfiguration)." + s3MetricsCost,
		Arguments: map[string]Argument{"Id": {Description: "ID of the metrics configuration; untrusted text"}},
		Fields: map[string]Field{
			"MetricsConfiguration": {Description: "The configuration: id and filter (prefix, tag, access_point_arn, or and with prefix, tags, and access_point_arn)"},
		},
		Limits:   []InputLimit{{Member: "Id", MaxLen: 64, Reason: s3IDLimit}},
		Examples: []Example{{Description: "Read one metrics configuration", Arguments: `{"bucket":"my-bucket","id":"EntireBucket"}`}},
	}, "metrics"),
	s3BucketWrite(Operation{
		Operation: "PutBucketMetricsConfiguration", Object: "metrics", Action: "put", Effect: "update", Idempotency: "idempotent",
		Title: "Set a request metrics configuration of an S3 bucket",
		Description: "Create or replace the request metrics configuration with the given ID: S3 then publishes request metrics for " +
			"the objects the filter selects (the whole bucket without a filter) to CloudWatch. Needs s3:PutMetricsConfiguration." + s3MetricsCost,
		Arguments: map[string]Argument{
			"Id":                   {Description: "ID of the configuration; the same as in metrics_configuration.id"},
			"MetricsConfiguration": {Description: "id and an optional filter: prefix, tag (key and value), access_point_arn, or and (prefix, tags, and access_point_arn)"},
		},
		Limits:     []InputLimit{{Member: "Id", MaxLen: 64, Reason: s3IDLimit}},
		SampleArgs: `{"metrics_configuration":{"id":"EntireBucket"}}`,
		Examples: []Example{{Description: "Publish request metrics for one prefix",
			Arguments: `{"bucket":"my-bucket","id":"images","metrics_configuration":{"id":"images","filter":{"prefix":"images/"}}}`}},
	}, "metrics"),
	s3BucketWrite(Operation{
		Operation: "DeleteBucketMetricsConfiguration", Object: "metrics", Action: "delete", Effect: "delete", Idempotency: "non-idempotent",
		Title: "Remove a request metrics configuration of an S3 bucket",
		Description: "Remove the request metrics configuration with the given ID: its metrics are no longer published. Needs " +
			"s3:PutMetricsConfiguration. An unknown ID answers not-found (NoSuchConfiguration).",
		Arguments: map[string]Argument{"Id": {Description: "ID of the configuration to remove"}},
		Limits:    []InputLimit{{Member: "Id", MaxLen: 64, Reason: s3IDLimit}},
		Examples:  []Example{{Description: "Remove one metrics configuration", Arguments: `{"bucket":"my-bucket","id":"images"}`}},
	}, "metrics"),
	s3BucketRead(Operation{
		Operation: "ListBucketIntelligentTieringConfigurations", Object: "intelligent-tiering", Action: "list",
		Title: "List the Intelligent-Tiering configurations of an S3 bucket",
		Description: "List the Intelligent-Tiering archive configurations of a bucket: each with ID, filter, status, and tierings. " +
			"Needs s3:GetIntelligentTieringConfiguration. A bucket without any answers an empty list." + s3TuningList + s3TieringCost,
		Fields: map[string]Field{
			"IntelligentTieringConfigurationList": {Description: "Configurations of this page, each as aws.s3.intelligent-tiering.get returns it", MaxItems: 1000},
			"ContinuationToken":                   {Description: "The token this page started at; empty for the first page"},
		},
		Examples: []Example{{Description: "List the Intelligent-Tiering configurations of a bucket", Arguments: `{"bucket":"my-bucket"}`}},
	}, "intelligent-tiering"),
	s3BucketRead(Operation{
		Operation: "GetBucketIntelligentTieringConfiguration", Object: "intelligent-tiering", Action: "get",
		Title: "Get one Intelligent-Tiering configuration of an S3 bucket",
		Description: "Return one Intelligent-Tiering archive configuration of a bucket by its ID: filter, status, and tierings. " +
			"Needs s3:GetIntelligentTieringConfiguration. An unknown ID answers not-found (NoSuchConfiguration)." + s3TieringCost,
		Arguments: map[string]Argument{"Id": {Description: "ID of the Intelligent-Tiering configuration; untrusted text"}},
		Fields: map[string]Field{
			"IntelligentTieringConfiguration": {Description: "The configuration: id, filter (prefix, tag, or and), status (Enabled or Disabled), and tierings (each with access_tier and days)"},
		},
		Limits:   []InputLimit{{Member: "Id", MaxLen: 64, Reason: s3IDLimit}},
		Examples: []Example{{Description: "Read one Intelligent-Tiering configuration", Arguments: `{"bucket":"my-bucket","id":"archive"}`}},
	}, "intelligent-tiering"),
	s3BucketWrite(Operation{
		Operation: "PutBucketIntelligentTieringConfiguration", Object: "intelligent-tiering", Action: "put", Effect: "update", Idempotency: "idempotent",
		Title: "Set an Intelligent-Tiering configuration of an S3 bucket",
		Description: "Create or replace the Intelligent-Tiering archive configuration with the given ID: objects in the " +
			"Intelligent-Tiering storage class that the filter selects move to the archive tiers after the given days without access. " +
			"Needs s3:PutIntelligentTieringConfiguration." + s3TieringCost,
		Arguments: map[string]Argument{
			"Id":                              {Description: "ID of the configuration; the same as in intelligent_tiering_configuration.id"},
			"IntelligentTieringConfiguration": {Description: "id, status (Enabled or Disabled), tierings (each with access_tier ARCHIVE_ACCESS or DEEP_ARCHIVE_ACCESS and days), and an optional filter (prefix, tag, or and with prefix and tags)"},
		},
		Limits:     []InputLimit{{Member: "Id", MaxLen: 64, Reason: s3IDLimit}},
		SampleArgs: `{"intelligent_tiering_configuration":{"id":"archive","status":"Enabled","tierings":[{"access_tier":"ARCHIVE_ACCESS","days":90}]}}`,
		Examples: []Example{{Description: "Archive objects not accessed for 90 days",
			Arguments: `{"bucket":"my-bucket","id":"archive","intelligent_tiering_configuration":{"id":"archive","status":"Enabled","tierings":[{"access_tier":"ARCHIVE_ACCESS","days":90}]}}`}},
	}, "intelligent-tiering"),
	s3BucketWrite(Operation{
		Operation: "DeleteBucketIntelligentTieringConfiguration", Object: "intelligent-tiering", Action: "delete", Effect: "delete", Idempotency: "non-idempotent",
		Title: "Remove an Intelligent-Tiering configuration of an S3 bucket",
		Description: "Remove the Intelligent-Tiering archive configuration with the given ID: objects no longer move to the archive " +
			"tiers by it; objects already archived stay there until accessed. Needs s3:PutIntelligentTieringConfiguration. An unknown ID answers not-found (NoSuchConfiguration).",
		Arguments: map[string]Argument{"Id": {Description: "ID of the configuration to remove"}},
		Limits:    []InputLimit{{Member: "Id", MaxLen: 64, Reason: s3IDLimit}},
		Examples:  []Example{{Description: "Remove one Intelligent-Tiering configuration", Arguments: `{"bucket":"my-bucket","id":"archive"}`}},
	}, "intelligent-tiering"),
	s3BucketWrite(Operation{
		Operation: "PutBucketAccelerateConfiguration", Object: "accelerate", Action: "put", Effect: "update", Idempotency: "idempotent",
		Title:       "Set the transfer acceleration of an S3 bucket",
		Description: "Set transfer acceleration of a bucket to Enabled or Suspended. Needs s3:PutAccelerateConfiguration." + s3AccelCost,
		Arguments: map[string]Argument{
			"AccelerateConfiguration": {Description: "status: Enabled or Suspended"},
		},
		SampleArgs: `{"accelerate_configuration":{"status":"Enabled"}}`,
		Examples:   []Example{{Description: "Enable transfer acceleration", Arguments: `{"bucket":"my-bucket","accelerate_configuration":{"status":"Enabled"}}`}},
	}, "accelerate"),
}
