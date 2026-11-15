package gen

const (
	s3ReportCost = " The reports cost money: S3 charges for the inventory or analytics listing it produces, for the storage the " +
		"reports occupy in the destination bucket, and for the requests that write them."
	s3ReportDestination = " The report goes only to a bucket of the bound account. destination.s3_bucket_destination.bucket " +
		"must be a bucket ARN (arn:aws:s3:::NAME) and the account ID next to it must be set and be the bound account, " +
		"since a bucket ARN carries no account and the name alone does not exclude a bucket of another account. " +
		"Before the change the destination bucket is checked with HeadBucket and the bound account as expected owner " +
		"(this needs s3:ListBucket on it); a failure or a different owner refuses the call, and nothing is sent. A " +
		"destination in another region than the source bucket is allowed and is a cross-region transfer that S3 charges for."
	s3ReportList = " The list is paged: pass the cursor of the result to read the next page."
)

// s3ReportWrite fills what the inventory and analytics tools share. The destination of a
// report is a standing data path out of the bucket, so the writes keep it inside the bound account.
func s3ReportWrite(op Operation, tags ...string) Operation {
	if op.Action == "put" {
		op.Guards, op.Readback = []string{"s3-report-destination"}, "s3-report-destination"
		op.ReadbackAnswer = reportHeadAnswer
		op.Description += s3ReportDestination + s3ReportCost
	}
	return s3BucketWrite(op, tags...)
}

// reportHeadAnswer is the answer of the generated test to the HeadBucket of the destination: headers only.
const reportHeadAnswer = "<Ok/>"

var s3ReportTools = []Operation{
	s3BucketRead(Operation{
		Operation: "ListBucketInventoryConfigurations", Object: "inventory", Action: "list",
		Title: "List the inventory configurations of an S3 bucket",
		Description: "List the inventory configurations of a bucket: each with ID, enabled state, included object versions, " +
			"optional fields, schedule, filter, and destination (bucket ARN, account ID, format, prefix, encryption). " +
			"Needs s3:GetInventoryConfiguration. A bucket without any answers an empty list." + s3ReportList + s3Foreign,
		Fields: map[string]Field{
			"InventoryConfigurationList": {Description: "Configurations of this page, each as aws.s3.inventory.get returns it", MaxItems: 1000},
			"ContinuationToken":          {Description: "The token this page started at; empty for the first page"},
		},
		Release:  []Release{{Member: "InventoryConfigurationList.Destination.S3BucketDestination.Encryption.SSEKMS.KeyId", Reason: s3KeyRead}},
		Examples: []Example{{Description: "List the inventories of a bucket", Arguments: `{"bucket":"my-bucket"}`}},
	}, "inventory"),
	s3BucketRead(Operation{
		Operation: "GetBucketInventoryConfiguration", Object: "inventory", Action: "get",
		Title: "Get one inventory configuration of an S3 bucket",
		Description: "Return one inventory configuration of a bucket by its ID: enabled state, included object versions, " +
			"optional fields, schedule, filter, and destination. Needs s3:GetInventoryConfiguration. An unknown ID " +
			"answers not-found (NoSuchConfiguration)." + s3Foreign,
		Arguments: map[string]Argument{"Id": {Description: "ID of the inventory configuration; untrusted text"}},
		Fields: map[string]Field{
			"InventoryConfiguration": {Description: "The configuration: destination (s3_bucket_destination with account_id, bucket, format, prefix, encryption), is_enabled, filter, id, included_object_versions, optional_fields, schedule"},
		},
		Release:  []Release{{Member: "InventoryConfiguration.Destination.S3BucketDestination.Encryption.SSEKMS.KeyId", Reason: s3KeyRead}},
		Limits:   []InputLimit{{Member: "Id", MaxLen: 64, Reason: "the model sets no bound; a protective cap at the length S3 allows for an ID"}},
		Examples: []Example{{Description: "Read one inventory", Arguments: `{"bucket":"my-bucket","id":"weekly"}`}},
	}, "inventory"),
	s3ReportWrite(Operation{
		Operation: "PutBucketInventoryConfiguration", Object: "inventory", Action: "put", Effect: "update", Idempotency: "idempotent",
		Title: "Set an inventory configuration of an S3 bucket",
		Description: "Create or replace the inventory configuration with the given ID: S3 then writes a daily or weekly list " +
			"of the objects of the bucket (with the chosen optional fields) to the destination. Needs " +
			"s3:PutInventoryConfiguration. With SSE-KMS the key must be named by the ARN of a key of the bound " +
			"account in an allowed region, and the destination bucket's policy must allow S3 to write.",
		Arguments: map[string]Argument{
			"Id":                     {Description: "ID of the configuration; the same as in inventory_configuration.id"},
			"InventoryConfiguration": {Description: "destination (s3_bucket_destination with account_id, bucket as arn:aws:s3:::NAME, format, prefix, and encryption as sses3 (an empty object) or ssekms with key_id), is_enabled, filter (prefix), id, included_object_versions (All or Current), optional_fields, and schedule (frequency Daily or Weekly)"},
		},
		Release:    []Release{{Member: "InventoryConfiguration.Destination.S3BucketDestination.Encryption.SSEKMS.KeyId", Reason: s3KeyReason}},
		Limits:     []InputLimit{{Member: "Id", MaxLen: 64, Reason: "the model sets no bound; a protective cap at the length S3 allows for an ID"}},
		SampleArgs: `{"inventory_configuration":{"destination":{"s3_bucket_destination":{"account_id":"123456789012","bucket":"arn:aws:s3:::reports","format":"CSV"}},"is_enabled":true,"id":"weekly","included_object_versions":"All","schedule":{"frequency":"Weekly"}}}`,
		Examples: []Example{{Description: "Write a weekly inventory to a bucket of the account",
			Arguments: `{"bucket":"my-bucket","id":"weekly","inventory_configuration":{"id":"weekly","is_enabled":true,"included_object_versions":"Current","schedule":{"frequency":"Weekly"},"destination":{"s3_bucket_destination":{"account_id":"123456789012","bucket":"arn:aws:s3:::my-reports","format":"CSV","prefix":"inventory"}}}}`}},
	}, "inventory"),
	s3ReportWrite(Operation{
		Operation: "DeleteBucketInventoryConfiguration", Object: "inventory", Action: "delete", Effect: "delete", Idempotency: "non-idempotent",
		Title: "Remove an inventory configuration of an S3 bucket",
		Description: "Remove the inventory configuration with the given ID: no further reports are written. Reports already " +
			"written stay in the destination bucket. Needs s3:PutInventoryConfiguration.",
		Arguments: map[string]Argument{"Id": {Description: "ID of the configuration to remove"}},
		Limits:    []InputLimit{{Member: "Id", MaxLen: 64, Reason: "the model sets no bound; a protective cap at the length S3 allows for an ID"}},
		Examples:  []Example{{Description: "Remove one inventory", Arguments: `{"bucket":"my-bucket","id":"weekly"}`}},
	}, "inventory"),
	s3BucketRead(Operation{
		Operation: "ListBucketAnalyticsConfigurations", Object: "analytics", Action: "list",
		Title: "List the storage class analytics configurations of an S3 bucket",
		Description: "List the storage class analytics configurations of a bucket: each with ID, filter, and the data export " +
			"(schema version and destination). Needs s3:GetAnalyticsConfiguration. A bucket without any answers an empty list." +
			s3ReportList + s3Foreign,
		Fields: map[string]Field{
			"AnalyticsConfigurationList": {Description: "Configurations of this page, each as aws.s3.analytics.get returns it", MaxItems: 1000},
			"ContinuationToken":          {Description: "The token this page started at; empty for the first page"},
		},
		Examples: []Example{{Description: "List the analytics of a bucket", Arguments: `{"bucket":"my-bucket"}`}},
	}, "analytics"),
	s3BucketRead(Operation{
		Operation: "GetBucketAnalyticsConfiguration", Object: "analytics", Action: "get",
		Title: "Get one storage class analytics configuration of an S3 bucket",
		Description: "Return one storage class analytics configuration of a bucket by its ID: filter and data export. Needs " +
			"s3:GetAnalyticsConfiguration. An unknown ID answers not-found (NoSuchConfiguration)." + s3Foreign,
		Arguments: map[string]Argument{"Id": {Description: "ID of the analytics configuration; untrusted text"}},
		Fields: map[string]Field{
			"AnalyticsConfiguration": {Description: "The configuration: id, filter (prefix, tag, or and), and storage_class_analysis with data_export (output_schema_version and destination)"},
		},
		Limits:   []InputLimit{{Member: "Id", MaxLen: 64, Reason: "the model sets no bound; a protective cap at the length S3 allows for an ID"}},
		Examples: []Example{{Description: "Read one analytics configuration", Arguments: `{"bucket":"my-bucket","id":"all"}`}},
	}, "analytics"),
	s3ReportWrite(Operation{
		Operation: "PutBucketAnalyticsConfiguration", Object: "analytics", Action: "put", Effect: "update", Idempotency: "idempotent",
		Title: "Set a storage class analytics configuration of an S3 bucket",
		Description: "Create or replace the storage class analytics configuration with the given ID: S3 analyses the access " +
			"pattern of the objects (limited by the filter) and, with a data export, writes a daily CSV report to the " +
			"destination. Needs s3:PutAnalyticsConfiguration. A configuration without a data export has no destination " +
			"and is not checked for one.",
		Arguments: map[string]Argument{
			"Id":                     {Description: "ID of the configuration; the same as in analytics_configuration.id"},
			"AnalyticsConfiguration": {Description: "id, filter (prefix, tag, or and with prefix and tags), and storage_class_analysis with data_export (output_schema_version V_1 and destination.s3_bucket_destination with format CSV, bucket_account_id, bucket as arn:aws:s3:::NAME, and prefix)"},
		},
		Limits:     []InputLimit{{Member: "Id", MaxLen: 64, Reason: "the model sets no bound; a protective cap at the length S3 allows for an ID"}},
		SampleArgs: `{"analytics_configuration":{"id":"all","storage_class_analysis":{"data_export":{"output_schema_version":"V_1","destination":{"s3_bucket_destination":{"format":"CSV","bucket_account_id":"123456789012","bucket":"arn:aws:s3:::reports"}}}}}}`,
		Examples: []Example{{Description: "Export the analysis of a bucket to a bucket of the account",
			Arguments: `{"bucket":"my-bucket","id":"all","analytics_configuration":{"id":"all","storage_class_analysis":{"data_export":{"output_schema_version":"V_1","destination":{"s3_bucket_destination":{"format":"CSV","bucket_account_id":"123456789012","bucket":"arn:aws:s3:::my-reports","prefix":"analytics"}}}}}}`}},
	}, "analytics"),
	s3ReportWrite(Operation{
		Operation: "DeleteBucketAnalyticsConfiguration", Object: "analytics", Action: "delete", Effect: "delete", Idempotency: "non-idempotent",
		Title: "Remove a storage class analytics configuration of an S3 bucket",
		Description: "Remove the storage class analytics configuration with the given ID: no further analysis or export is made. " +
			"Reports already written stay in the destination bucket. Needs s3:PutAnalyticsConfiguration.",
		Arguments: map[string]Argument{"Id": {Description: "ID of the configuration to remove"}},
		Limits:    []InputLimit{{Member: "Id", MaxLen: 64, Reason: "the model sets no bound; a protective cap at the length S3 allows for an ID"}},
		Examples:  []Example{{Description: "Remove one analytics configuration", Arguments: `{"bucket":"my-bucket","id":"all"}`}},
	}, "analytics"),
}

const s3KeyReason = "the model marks the KMS key ID sensitive, but it is an identifier and not a secret; it is checked to be a key ARN of the bound account"

const s3KeyRead = "the model marks the KMS key ID sensitive, but it is an identifier and not a secret; it shows which key encrypts the report"
