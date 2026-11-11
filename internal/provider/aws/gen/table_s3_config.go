package gen

// s3Foreign is said by every reader of a configuration that can name targets outside the bound account.
const s3Foreign = " The configuration can name buckets, topics, queues, or functions of other accounts; they are shown as " +
	"untrusted data and the tool follows none of them and fetches no URL from it."

// s3ConfigReads are the readers of the bucket configuration that has no writing tool of its own: the ACL, the
// website, logging, notifications, replication, ownership controls, transfer acceleration, and who pays for requests.
var s3ConfigReads = []Operation{
	s3BucketRead(Operation{
		Operation: "GetBucketAcl", Object: "acl", Action: "get",
		Title: "Get the ACL of an S3 bucket",
		Description: "Return the access control list of a bucket: the owner and the grants, each with the grantee (a canonical " +
			"user, an e-mail address, or a group URI) and the permission. A bucket with ACLs turned off (bucket owner " +
			"enforced) answers one grant for the owner. Needs s3:GetBucketAcl." + s3Foreign,
		Fields: map[string]Field{
			"Owner":  {Description: "Owner of the bucket: display_name and id (the canonical user ID)"},
			"Grants": {Description: "Grants, each with grantee (type, id, display_name, email_address, uri) and permission (FULL_CONTROL, WRITE, WRITE_ACP, READ, READ_ACP)"},
		},
		Examples: []Example{{Description: "Read the ACL of a bucket", Arguments: `{"bucket":"my-bucket"}`}},
	}, "acl"),
	s3BucketRead(Operation{
		Operation: "GetBucketWebsite", Object: "website", Action: "get",
		Title: "Get the website configuration of an S3 bucket",
		Description: "Return the static website configuration of a bucket: the index and error documents, a redirect of all " +
			"requests, and the routing rules. Needs s3:GetBucketWebsite. A bucket without a website " +
			"answers not-found (NoSuchWebsiteConfiguration).",
		Fields: map[string]Field{
			"RedirectAllRequestsTo": {Description: "Redirect of all requests: host_name and protocol"},
			"IndexDocument":         {Description: "Index document: suffix"},
			"ErrorDocument":         {Description: "Error document: key"},
			"RoutingRules":          {Description: "Routing rules, each with a condition and a redirect"},
		},
		Examples: []Example{{Description: "Read the website configuration", Arguments: `{"bucket":"my-bucket"}`}},
	}, "website"),
	s3BucketRead(Operation{
		Operation: "GetBucketLogging", Object: "logging", Action: "get",
		Title: "Get the server access logging of an S3 bucket",
		Description: "Return where a bucket delivers its server access logs: the target bucket, the prefix, the grants on the log " +
			"objects, and the key format. A bucket without logging answers an empty result. Needs s3:GetBucketLogging." + s3Foreign,
		Fields: map[string]Field{
			"LoggingEnabled": {Description: "Logging: target_bucket, target_prefix, target_grants (each with grantee and permission), and target_object_key_format; left out when logging is off"},
		},
		Examples: []Example{{Description: "Read the access logging of a bucket", Arguments: `{"bucket":"my-bucket"}`}},
	}, "logging"),
	s3BucketRead(Operation{
		Operation: "GetBucketNotificationConfiguration", Object: "notification", Action: "get",
		Title: "Get the event notifications of an S3 bucket",
		Description: "Return the event notifications of a bucket: the topic, queue, and function configurations with their " +
			"events, filters, and target ARNs. Whether the bucket sends events to EventBridge is not shown. Needs s3:GetBucketNotification." + s3Foreign,
		Fields: map[string]Field{
			"TopicConfigurations":          {Description: "SNS topic configurations, each with id, topic_arn, events, and filter"},
			"QueueConfigurations":          {Description: "SQS queue configurations, each with id, queue_arn, events, and filter"},
			"LambdaFunctionConfigurations": {Description: "Lambda function configurations, each with id, lambda_function_arn, events, and filter"},
			"EventBridgeConfiguration":     {Description: "Always left out: the setting is an empty element, which a result does not carry, so its absence does not mean that EventBridge is off"},
		},
		Examples: []Example{{Description: "Read the notifications of a bucket", Arguments: `{"bucket":"my-bucket"}`}},
	}, "notification"),
	s3BucketRead(Operation{
		Operation: "GetBucketReplication", Object: "replication", Action: "get",
		Title: "Get the replication of an S3 bucket",
		Description: "Return the replication configuration of a bucket: the role and the rules with filter, status, destination " +
			"(bucket, account, storage class, encryption, and the like), and delete marker handling. Needs " +
			"s3:GetReplicationConfiguration. A bucket without replication answers not-found " +
			"(ReplicationConfigurationNotFoundError)." + s3Foreign,
		Fields: map[string]Field{
			"ReplicationConfiguration": {Description: "The configuration: role (an IAM role ARN) and rules"},
		},
		Examples: []Example{{Description: "Read the replication of a bucket", Arguments: `{"bucket":"my-bucket"}`}},
	}, "replication"),
	s3BucketRead(Operation{
		Operation: "GetBucketOwnershipControls", Object: "ownership", Action: "get",
		Title: "Get the object ownership of an S3 bucket",
		Description: "Return the object ownership rules of a bucket: BucketOwnerEnforced (ACLs off), BucketOwnerPreferred, or " +
			"ObjectWriter. Needs s3:GetBucketOwnershipControls. A bucket without such controls answers not-found " +
			"(OwnershipControlsNotFoundError).",
		Fields: map[string]Field{
			"OwnershipControls": {Description: "The controls: rules, each with object_ownership"},
		},
		Examples: []Example{{Description: "Read the object ownership of a bucket", Arguments: `{"bucket":"my-bucket"}`}},
	}, "ownership"),
	s3BucketRead(Operation{
		Operation: "GetBucketAccelerateConfiguration", Object: "accelerate", Action: "get",
		Title: "Get the transfer acceleration of an S3 bucket",
		Description: "Return whether transfer acceleration of a bucket is Enabled or Suspended; the status is left out for a " +
			"bucket that never had it. Needs s3:GetAccelerateConfiguration.",
		HiddenInputs: []Hidden{{Member: "RequestPayer", Reason: "the call is for the bound account's own bucket and never pays for the request on behalf of another account"}},
		Fields: map[string]Field{
			"Status": {Description: "Enabled or Suspended; left out when acceleration was never configured"},
		},
		HiddenOutputs: []Hidden{{Member: "RequestCharged", Reason: "request payer is not offered, so this header is never of interest"}},
		Examples:      []Example{{Description: "Read the acceleration state of a bucket", Arguments: `{"bucket":"my-bucket"}`}},
	}, "accelerate"),
	s3BucketRead(Operation{
		Operation: "GetBucketRequestPayment", Object: "request-payment", Action: "get",
		Title: "Get who pays for the requests to an S3 bucket",
		Description: "Return whether the owner (BucketOwner) or the requester (Requester) pays for requests to and downloads from " +
			"a bucket. Needs s3:GetBucketRequestPayment.",
		Fields: map[string]Field{
			"Payer": {Description: "BucketOwner or Requester"},
		},
		Examples: []Example{{Description: "Read who pays for a bucket", Arguments: `{"bucket":"my-bucket"}`}},
	}, "request-payment"),
}
