package gen

import "slices"

const (
	s3WriteNote = " The bucket belongs to the bound account; a bucket of another account, or in another region than the one " +
		"called, is refused."
	s3Idempotent = " Repeating the call with the same arguments leaves the same result."
	// pabAnswer is the answer of the generated test to the read before a public access block change: all four off.
	pabAnswer = `<PublicAccessBlockConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><BlockPublicAcls>false</BlockPublicAcls>` +
		`<IgnorePublicAcls>false</IgnorePublicAcls><BlockPublicPolicy>false</BlockPublicPolicy>` +
		`<RestrictPublicBuckets>false</RestrictPublicBuckets></PublicAccessBlockConfiguration>`
)

// s3BucketWrite fills what the bucket writes share: a confirmed change of one bucket of the bound account.
func s3BucketWrite(op Operation, tags ...string) Operation {
	op.Confirm = true
	op.DataSensitivity = "aws-s3-bucket-configuration"
	op.Tags = slices.Concat([]string{"aws", "s3", "bucket"}, tags)
	op.PolicyCheck = "none"
	op.TargetCheck, op.TargetArgs = "bucket-owner", []string{"Bucket"}
	op.Arguments = withBucket(op.Arguments)
	op.Description += s3WriteNote
	if op.Idempotency == "idempotent" {
		op.Description += s3Idempotent
	}
	op.Description += s3Untrusted
	return op
}

var (
	// Tag caps are protective and above the limits of S3 (128 and 256 characters, 50 tags); S3 enforces its own.
	s3TagLimits = func(set string) []InputLimit {
		const why = "the model sets no bound; a protective cap above the limits S3 enforces"
		return []InputLimit{
			{Member: set, MaxItems: 100, Reason: why},
			{Member: set + ".Key", MaxLen: 256, Reason: why},
			{Member: set + ".Value", MaxLen: 512, Reason: why},
		}
	}
	hideBucketACLs = func() []Hidden {
		const why = "ACLs are not offered: a grant could open the bucket, and bucket owner enforced ownership turns them off"
		var out []Hidden
		for _, m := range []string{"ACL", "GrantFullControl", "GrantRead", "GrantReadACP", "GrantWrite", "GrantWriteACP"} {
			out = append(out, Hidden{Member: m, Reason: why})
		}
		return out
	}
)

// s3BucketWriteTools are the changes of a bucket and of its versioning, default encryption, tags, and public
// access block.
var s3BucketWriteTools = []Operation{
	s3BucketWrite(Operation{
		Operation: "CreateBucket", Object: "bucket", Action: "create", Effect: "create", Idempotency: "non-idempotent",
		Title: "Create an S3 bucket",
		Description: "Create a general purpose bucket in the region of the call, owned by the bound account, with ACLs turned " +
			"off (bucket owner enforced). Outside us-east-1 location_constraint must be that region; in us-east-1 it " +
			"must be left out. A name that is taken answers a conflict: BucketAlreadyOwnedByYou when the bound account " +
			"owns it, BucketAlreadyExists when another account does. Needs s3:CreateBucket. S3 charges for the data a " +
			"bucket holds and for requests to it; an empty bucket holds none. Object Lock, ACLs, grants, and directory " +
			"buckets are not offered. The call does not take the expected-owner header, since the bucket does not exist yet.",
		Arguments: map[string]Argument{
			"CreateBucketConfiguration": {Description: "location_constraint (the region of the call, required outside us-east-1) and optional tags, each with key and value"},
			"ObjectOwnership":           {Description: "BucketOwnerEnforced, the only value offered and the default of S3: ACLs are turned off and the bucket owner owns every object"},
		},
		Fields: map[string]Field{
			"Location":  {Description: "Path of the new bucket, as S3 reports it"},
			"BucketArn": {Description: "ARN of the new bucket"},
		},
		HiddenInputs: append(hideBucketACLs(),
			Hidden{Member: "ObjectLockEnabledForBucket", Reason: "Object Lock is not offered; it cannot be turned off again once on"},
			Hidden{Member: "BucketNamespace", Reason: "the account regional namespace changes the naming rules the name check relies on"},
			Hidden{Member: "CreateBucketConfiguration.Location", Reason: "directory buckets are not offered"},
			Hidden{Member: "CreateBucketConfiguration.Bucket", Reason: "directory buckets are not offered"}),
		ExcludeEnum: []EnumExclusion{{Member: "ObjectOwnership", Values: []string{"ObjectWriter", "BucketOwnerPreferred"},
			Reason: "both turn ACLs on, which could open the bucket"}},
		Limits:     s3TagLimits("CreateBucketConfiguration.Tags"),
		Guards:     []string{"new-bucket-name", "location-constraint"},
		SampleArgs: `{"create_bucket_configuration":{"location_constraint":"eu-central-1"}}`,
		Examples: []Example{{Description: "Create a bucket in eu-central-1 with ACLs off",
			Arguments: `{"bucket":"my-new-bucket","region":"eu-central-1","create_bucket_configuration":{"location_constraint":"eu-central-1"},"object_ownership":"BucketOwnerEnforced"}`}},
	}, "region"),
	s3BucketWrite(Operation{
		Operation: "DeleteBucket", Object: "bucket", Action: "delete", Effect: "delete", Idempotency: "non-idempotent",
		Title: "Delete an empty S3 bucket",
		Description: "Delete a bucket that holds no objects, no versions, and no delete markers. A bucket that is not empty " +
			"answers a conflict (BucketNotEmpty) and stays. The name becomes free for anyone. Needs s3:DeleteBucket.",
		Examples: []Example{{Description: "Delete an empty bucket", Arguments: `{"bucket":"my-old-bucket"}`}},
	}),
	s3BucketWrite(Operation{
		Operation: "PutBucketVersioning", Object: "versioning", Action: "put", Effect: "update", Idempotency: "idempotent",
		Title: "Set the versioning of an S3 bucket",
		Description: "Set versioning of a bucket to Enabled or Suspended. Versioning can never be turned off, only suspended; " +
			"versions that exist stay and are billed as storage. MFA delete is not offered. Needs s3:PutBucketVersioning.",
		Arguments: map[string]Argument{
			"VersioningConfiguration": {Description: "status: Enabled or Suspended"},
		},
		HiddenInputs: []Hidden{
			{Member: "MFA", Reason: "MFA delete needs a code of a device, which no tool handles"},
			{Member: "VersioningConfiguration.MFADelete", Reason: "MFA delete needs a code of a device, which no tool handles"},
		},
		Examples: []Example{{Description: "Turn versioning on", Arguments: `{"bucket":"my-bucket","versioning_configuration":{"status":"Enabled"}}`}},
	}, "versioning"),
	s3BucketWrite(Operation{
		Operation: "PutBucketEncryption", Object: "encryption", Action: "put", Effect: "update", Idempotency: "idempotent",
		Title: "Set the default encryption of an S3 bucket",
		Description: "Replace the default server-side encryption of a bucket: SSE-S3 (AES256) or SSE-KMS (aws:kms, " +
			"aws:kms:dsse), with the bucket key setting, and optionally the encryption types to block. The rules replace " +
			"the whole configuration, so repeat what is to stay. A KMS key must be named by the ARN of a key " +
			"(arn:aws:kms:REGION:ACCOUNT:key/ID) or of an alias (arn:aws:kms:REGION:ACCOUNT:alias/NAME) of the bound " +
			"account in an allowed region; a bare key ID or alias name is refused before any request. Customer-provided " +
			"keys (SSE-C) are not offered. Needs s3:PutEncryptionConfiguration, and the key's policy must allow it.",
		Arguments: map[string]Argument{
			"ServerSideEncryptionConfiguration": {Description: "rules, each with apply_server_side_encryption_by_default (sse_algorithm, and kms_master_key_id as an ARN for aws:kms), bucket_key_enabled, and blocked_encryption_types"},
		},
		ExcludeEnum: []EnumExclusion{{Member: "ServerSideEncryptionConfiguration.Rules.ApplyServerSideEncryptionByDefault.SSEAlgorithm",
			Values: []string{"aws:fsx", "aws:backup"}, Reason: "these algorithms belong to other AWS services and are not set by a person"}},
		Release: []Release{{Member: "ServerSideEncryptionConfiguration.Rules.ApplyServerSideEncryptionByDefault.KMSMasterKeyID",
			Reason: "the model marks the KMS key ID sensitive, but it is an identifier and not a secret; it is checked to be a key ARN of the bound account"}},
		Guards:     []string{"kms-key-arn"},
		SampleArgs: `{"server_side_encryption_configuration":{"rules":[{"apply_server_side_encryption_by_default":{"sse_algorithm":"aws:kms","kms_master_key_id":"arn:aws:kms:eu-central-1:123456789012:key/a"}}]}}`,
		Examples: []Example{{Description: "Encrypt new objects with SSE-S3",
			Arguments: `{"bucket":"my-bucket","server_side_encryption_configuration":{"rules":[{"apply_server_side_encryption_by_default":{"sse_algorithm":"AES256"}}]}}`}},
	}, "encryption"),
	s3BucketWrite(Operation{
		Operation: "DeleteBucketEncryption", Object: "encryption", Action: "delete", Effect: "delete", Idempotency: "non-idempotent",
		Title: "Remove the default encryption of an S3 bucket",
		Description: "Remove the default encryption configuration of a bucket; S3 then encrypts new objects with SSE-S3 " +
			"as it does for every bucket. Needs s3:PutEncryptionConfiguration.",
		Examples: []Example{{Description: "Remove the default encryption", Arguments: `{"bucket":"my-bucket"}`}},
	}, "encryption"),
	s3BucketWrite(Operation{
		Operation: "PutBucketTagging", Object: "tagging", Action: "put", Effect: "update", Idempotency: "idempotent",
		Title: "Set the tags of an S3 bucket",
		Description: "Replace all tags of a bucket with the given ones, each with key and value; tags not given are removed. " +
			"Needs s3:PutBucketTagging. S3 limits the number and the length of tags.",
		Arguments: map[string]Argument{
			"Tagging": {Description: "tag_set: the tags, each with key and value"},
		},
		Limits:   s3TagLimits("Tagging.TagSet"),
		Examples: []Example{{Description: "Tag a bucket", Arguments: `{"bucket":"my-bucket","tagging":{"tag_set":[{"key":"team","value":"data"}]}}`}},
	}, "tags"),
	s3BucketWrite(Operation{
		Operation: "DeleteBucketTagging", Object: "tagging", Action: "delete", Effect: "delete", Idempotency: "non-idempotent",
		Title:       "Remove the tags of an S3 bucket",
		Description: "Remove all tags of a bucket. Needs s3:PutBucketTagging.",
		Examples:    []Example{{Description: "Remove all tags", Arguments: `{"bucket":"my-bucket"}`}},
	}, "tags"),
	s3BucketWrite(Operation{
		Operation: "PutPublicAccessBlock", Object: "public-access-block", Action: "put", Effect: "update", Idempotency: "idempotent",
		Title: "Set the public access block of an S3 bucket",
		Description: "Set the four public access block settings of a bucket: block_public_acls, ignore_public_acls, " +
			"block_public_policy, and restrict_public_buckets. This tool keeps or tightens the block and never loosens " +
			"it: before the change it reads the current block, and it refuses the call, without changing anything, when a " +
			"setting that is on would be turned off or left out (a setting left out counts as off), and when the " +
			"current block cannot be read. A bucket without a block counts as all four off. Another change between the " +
			"read and the write is not excluded. Needs s3:PutBucketPublicAccessBlock and s3:GetBucketPublicAccessBlock.",
		Arguments: map[string]Argument{
			"PublicAccessBlockConfiguration": {Description: "block_public_acls, ignore_public_acls, block_public_policy, and restrict_public_buckets, each true or false; a setting left out counts as false"},
		},
		Readback: "pab-tighten", ReadbackAnswer: pabAnswer,
		Examples: []Example{{Description: "Turn all four settings on",
			Arguments: `{"bucket":"my-bucket","public_access_block_configuration":{"block_public_acls":true,"ignore_public_acls":true,"block_public_policy":true,"restrict_public_buckets":true}}`}},
	}, "public-access"),
}
