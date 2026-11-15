package gen

import (
	"slices"
	"strconv"
	"strings"
)

const (
	// s3MaxPartNumber is the highest part number of a multipart upload this package takes, for upload-part, upload-part-copy,
	// and the part list of complete. With parts of up to s3TransferLimit it bounds an object at about 10 GiB, the size
	// of the largest local file. The limit is one constant so it can be raised later; S3 itself allows 10 000 parts.
	s3MaxPartNumber = 100

	s3MultipartIDs  = "upload_id comes from create."
	s3MultipartCost = " The parts of an upload that is neither completed nor aborted stay in the bucket and are billed as storage until " +
		"abort: an AbortIncompleteMultipartUpload lifecycle rule of the bucket removes forgotten uploads."
)

var s3MultipartParts = "Part numbers go from 1 to " + strconv.Itoa(s3MaxPartNumber) + ", so an object reaches about 10 GiB; each part is at most " + s3TransferLimit +
	" and S3 wants every part but the last to hold at least 5 MiB."

var s3PartNumberLimit = InputLimit{Member: "PartNumber", MinValue: 1, MaxValue: s3MaxPartNumber,
	Reason: "the model sets no bound; the part numbers this package takes (" + strconv.Itoa(s3MaxPartNumber) + "), which with parts of " + s3TransferLimit + " bound an object at about 10 GiB"}

var s3UploadIDLimit = InputLimit{Member: "UploadId", MaxLen: 1024, Reason: "the model sets no bound; a protective cap above the length of an upload ID"}

var s3MultipartIDLimits = []InputLimit{{Member: "Key", MaxLen: 1024, Reason: s3KeyLimit}, s3UploadIDLimit}

// s3MultipartTools are the tools of a multipart upload: start, send parts, complete, abort, and the two lists.
var s3MultipartTools = []Operation{
	s3Object(Operation{
		Operation: "CreateMultipartUpload", Object: "multipart-upload", Action: "create", Effect: "create", Idempotency: "non-idempotent",
		Title: "Start a multipart upload to an S3 bucket",
		Description: "Start a multipart upload of one object in a bucket of the bound account and return its upload_id, which upload-part, " +
			"upload-part-copy, complete, list-parts, and abort take. Nothing is stored as an object until complete. Metadata, " +
			"content type, and encryption are fixed here and apply to the finished object. Order: create, then upload-part or " +
			"upload-part-copy for each part, then complete. " + s3MultipartParts + s3MultipartCost + " Needs s3:PutObject, and with " +
			"server_side_encryption aws:kms also kms:GenerateDataKey and kms:Decrypt on the key; s3:PutObjectTagging with tagging. " +
			"ACLs, customer-provided keys (SSE-C), website redirects, and Object Lock settings are not offered.",
		Arguments: map[string]Argument{
			"Key":                  {Description: "Key of the object: 1 to 1024 bytes of UTF-8; untrusted text"},
			"CacheControl":         {Description: "Cache-Control header stored with the object"},
			"ContentDisposition":   {Description: "Content-Disposition header stored with the object"},
			"ContentEncoding":      {Description: "Content-Encoding header stored with the object; the content is stored as it is, not encoded"},
			"ContentLanguage":      {Description: "Content-Language header stored with the object"},
			"ContentType":          {Description: "Content-Type stored with the object; application/octet-stream when left out"},
			"Expires":              {Description: "Expires header stored with the object, as text"},
			"Metadata":             {Description: "User metadata as a map of names to values, stored as x-amz-meta-NAME headers; untrusted data"},
			"ServerSideEncryption": {Description: "aws:kms for SSE-KMS or AES256 for SSE-S3; the bucket default applies when left out"},
			"StorageClass":         {Description: "Storage class of the object; STANDARD when left out"},
			"SSEKMSKeyId":          {Description: "With server_side_encryption aws:kms: the ARN of a KMS key or alias in the bound account and an allowed region; a bare key ID or alias name is refused"},
			"BucketKeyEnabled":     {Description: "Whether an S3 Bucket Key reduces the KMS request cost of SSE-KMS"},
			"Tagging":              {Description: "Tags as a URL-encoded query string, such as team=a&env=prod; needs s3:PutObjectTagging"},
			"ChecksumAlgorithm":    {Description: "SHA256, the only value and the one used when left out: upload-part sends the SHA-256 of each part, and S3 holds every part of an upload to the algorithm it was started with"},
		},
		Fields: s3Merge(map[string]Field{
			"AbortDate":            {Description: "Date after which a lifecycle rule aborts the upload, when one applies"},
			"AbortRuleId":          {Description: "ID of the lifecycle rule that aborts the upload, when one applies"},
			"Bucket":               {Description: "Name of the bucket"},
			"Key":                  {Description: "Key of the object being uploaded; untrusted text"},
			"UploadId":             {Description: "ID of the upload; pass it as upload_id to the other multipart tools", Required: true},
			"ServerSideEncryption": {Description: "Server-side encryption the finished object gets"},
			"SSEKMSKeyId":          {Description: "ARN of the KMS key that encrypts the object, when SSE-KMS applies"},
			"BucketKeyEnabled":     {Description: "Whether an S3 Bucket Key is used"},
			"ChecksumAlgorithm":    {Description: "Checksum algorithm of the upload, when S3 states one"},
			"ChecksumType":         {Description: "Whether checksums cover the whole object (FULL_OBJECT) or are checksums of part checksums (COMPOSITE)"},
		}),
		HiddenInputs: slices.Concat(
			hiddenAll(s3ACLWhy, "ACL", "GrantFullControl", "GrantRead", "GrantReadACP", "GrantWriteACP"),
			hiddenAll(s3SSECWhy, "SSECustomerAlgorithm", "SSECustomerKey", "SSECustomerKeyMD5"),
			hiddenAll("Object Lock writes are not a tool yet; a lock on an object that cannot be removed is not set through an upload",
				"ObjectLockMode", "ObjectLockRetainUntilDate", "ObjectLockLegalHoldStatus", "ObjectLockEventHold",
				"ObjectLockEventHoldDurationDays", "ObjectLockEventHoldDurationYears"),
			[]Hidden{
				{Member: "WebsiteRedirectLocation", Reason: s3RedirectWhy},
				{Member: "RequestPayer", Reason: s3PayerWhy},
				{Member: "SSEKMSEncryptionContext", Reason: "the encryption context is sensitive and is not offered; S3 applies the default context"},
				{Member: "ChecksumType", Reason: "SHA256 allows only the composite type, which S3 applies; the full-object type belongs to the CRC algorithms"},
			}),
		HiddenOutputs: slices.Concat([]Hidden{
			{Member: "SSEKMSEncryptionContext", Reason: "the encryption context is sensitive and is not returned"},
			{Member: "RequestCharged", Reason: s3PayerWhy},
		}, s3NoSSECOut),
		ExcludeEnum: []EnumExclusion{{Member: "ServerSideEncryption", Values: []string{"aws:fsx", "aws:backup", "aws:kms:dsse"},
			Reason: "only SSE-S3 and SSE-KMS are offered; the other values belong to other storage systems or to dual-layer encryption, which this tool does not check"},
			{Member: "ChecksumAlgorithm", Values: []string{"CRC32", "CRC32C", "SHA1", "CRC64NVME", "SHA512", "MD5", "XXHASH64", "XXHASH3", "XXHASH128"},
				Reason: "the parts are sent with their SHA-256, and S3 refuses a part whose checksum is not of the algorithm the upload was started with"}},
		Limits: []InputLimit{
			{Member: "Key", MaxLen: 1024, Reason: s3KeyLimit},
			{Member: "CacheControl", MaxLen: 1024, Reason: "the model sets no bound; a protective cap"},
			{Member: "ContentDisposition", MaxLen: 1024, Reason: "the model sets no bound; a protective cap"},
			{Member: "ContentEncoding", MaxLen: 256, Reason: "the model sets no bound; a protective cap"},
			{Member: "ContentLanguage", MaxLen: 256, Reason: "the model sets no bound; a protective cap"},
			{Member: "ContentType", MaxLen: 256, Reason: "the model sets no bound; a protective cap"},
			{Member: "Expires", MaxLen: 64, Reason: "the model sets no bound; a protective cap above an HTTP date"},
			{Member: "SSEKMSKeyId", MaxLen: 2048, Reason: "the model sets no bound; a protective cap above the length of a KMS ARN"},
			{Member: "Metadata", MaxLen: 2048, Reason: "the model sets no bound; a protective cap on each value, above the 2 KB S3 allows for all metadata together"},
			{Member: "Tagging", MaxLen: 4096, Reason: "the model sets no bound; a protective cap above what ten tags need"},
		},
		Release:    []Release{{Member: "SSEKMSKeyId", Reason: "the ARN of a KMS key, an identifier and not key material; the guard accepts only ARNs of the bound account"}},
		Guards:     []string{"upload-kms-key", "multipart-checksum"},
		SampleArgs: `{"content_type":"text/plain","ssekms_key_id":"arn:aws:kms:eu-central-1:123456789012:key/a"}`,
		Examples: []Example{{Description: "Start the upload of a large archive",
			Arguments: `{"bucket":"my-bucket","key":"backups/2026.tar","content_type":"application/x-tar"}`}},
	}, "multipart", "create"),

	withData(s3Object(Operation{
		Operation: "UploadPart", Object: "multipart-upload", Action: "upload-part", Effect: "create", Idempotency: "idempotent",
		LocalFile: "read", LocalRange: true,
		Title: "Upload one part of a multipart upload from a local file",
		Description: "Send one part of a multipart upload: the bytes of a range of a local file (the whole file without offset and length), " +
			"streamed from a directory the connection releases for reading, with their SHA-256 as the checksum S3 verifies. A part is " +
			"at most " + s3TransferLimit + "; a larger range is refused and nothing is sent. The file itself may be as large as the " +
			"connection allows, so send the parts of one file with growing offset. The answer holds the etag and checksum_sha256 that complete needs; " +
			"keep it with the part number. " + s3MultipartParts + " " + s3MultipartIDs + " Sending the same part number again " +
			"replaces that part. The call limit of 60 seconds of this provider applies, so a slow link needs smaller parts. " +
			"Needs s3:PutObject; kms:GenerateDataKey and kms:Decrypt on the key of an SSE-KMS upload." + s3MultipartCost + s3Transfer,
		Arguments: map[string]Argument{
			"Key":        {Description: "Key of the object being uploaded, as given to create; untrusted text"},
			"PartNumber": {Description: "Number of the part: 1 to " + strconv.Itoa(s3MaxPartNumber) + "; the parts are joined in this order"},
			"UploadId":   {Description: "ID of the upload, from create"},
		},
		Fields: s3Merge(map[string]Field{
			"ETag":                 {Name: "etag", Description: "ETag of the part; pass it to complete as e_tag with the part number and checksum_sha256"},
			"ServerSideEncryption": {Description: "Server-side encryption of the part"},
			"SSEKMSKeyId":          {Description: "ARN of the KMS key that encrypts the part, when SSE-KMS applies"},
			"BucketKeyEnabled":     {Description: "Whether an S3 Bucket Key was used"},
		}, s3PartChecksumOutputs()),
		HiddenInputs: slices.Concat(hiddenAll(s3SSECWhy, "SSECustomerAlgorithm", "SSECustomerKey", "SSECustomerKeyMD5"),
			[]Hidden{{Member: "RequestPayer", Reason: s3PayerWhy}}),
		HiddenOutputs: slices.Concat([]Hidden{{Member: "RequestCharged", Reason: s3PayerWhy}}, s3NoSSECOut),
		Limits:        []InputLimit{{Member: "Key", MaxLen: 1024, Reason: s3KeyLimit}, s3PartNumberLimit, s3UploadIDLimit},
		Release:       []Release{{Member: "SSEKMSKeyId", Reason: "the ARN of a KMS key, an identifier and not key material"}},
		Examples: []Example{{Description: "Send the second part of 64 MiB of a large file",
			Arguments: `{"bucket":"my-bucket","key":"backups/2026.tar","upload_id":"EXAMPLEID","part_number":2,"local_path":"~/exports/2026.tar","offset":67108864,"length":67108864}`}},
	}, "multipart", "upload-part", "file")),

	s3Object(Operation{
		Operation: "UploadPartCopy", Object: "multipart-upload", Action: "upload-part-copy", Effect: "create", Idempotency: "idempotent",
		Title: "Upload one part of a multipart upload by copying a range of an S3 object",
		Description: "Fill one part of a multipart upload with a byte range of an existing object inside S3, from copy_source. " +
			"copy_source_range is required and covers at most " + s3TransferLimit + ", as bytes=FIRST-LAST. copy_source is " +
			"bucket/key, percent-encoded, with an optional ?versionId=ID; it must name a bucket of the bound account, an access point or " +
			"other ARN is refused before any request, and the bound account is sent as the expected owner of the source bucket. " +
			"S3 can answer an error after a success status; the tool reports that as an error. The answer holds the e_tag and checksum_sha256 that " +
			"complete needs. " + s3MultipartParts + " " + s3MultipartIDs + " Sending the same part number again replaces that part. " +
			"Needs s3:GetObject on the source and s3:PutObject on the destination." + s3MultipartCost + s3Requests,
		Arguments: map[string]Argument{
			"Key":                         {Description: "Key of the object being uploaded, as given to create; untrusted text"},
			"CopySource":                  {Description: "Source as bucket/key, percent-encoded, optionally followed by ?versionId=ID; the bucket must belong to the bound account"},
			"CopySourceRange":             {Description: "Required: the bytes of the source to copy as bytes=FIRST-LAST, at most 104857600 (100 MiB), such as bytes=0-67108863"},
			"CopySourceIfMatch":           {Description: "Copy only if the ETag of the source is this"},
			"CopySourceIfModifiedSince":   {Description: "Copy only if the source changed after this date"},
			"CopySourceIfNoneMatch":       {Description: "Copy only if the ETag of the source is not this"},
			"CopySourceIfUnmodifiedSince": {Description: "Copy only if the source did not change after this date"},
			"PartNumber":                  {Description: "Number of the part: 1 to " + strconv.Itoa(s3MaxPartNumber) + "; the parts are joined in this order"},
			"UploadId":                    {Description: "ID of the upload, from create"},
		},
		Fields: map[string]Field{
			"CopySourceVersionId":  {Description: "Version of the source that was copied"},
			"CopyPartResult":       {Description: "Result of the copy: e_tag, last_modified, and the checksums of the part; pass the e_tag to complete with the part number"},
			"ServerSideEncryption": {Description: "Server-side encryption of the part"},
			"SSEKMSKeyId":          {Description: "ARN of the KMS key that encrypts the part, when SSE-KMS applies"},
			"BucketKeyEnabled":     {Description: "Whether an S3 Bucket Key was used"},
		},
		HiddenInputs:  slices.Concat(s3NoSSEC(""), s3NoSSEC("CopySource"), []Hidden{s3NoPayer}),
		HiddenOutputs: slices.Concat([]Hidden{s3NoPaid}, s3NoSSECOut),
		Limits: []InputLimit{
			{Member: "Key", MaxLen: 1024, Reason: s3KeyLimit},
			{Member: "CopySource", MaxLen: 2048, Reason: "the model sets no bound; a protective cap at the longest key plus the bucket name and version, percent-encoded"},
			{Member: "CopySourceRange", MaxLen: 128, Reason: "the model sets no bound; a protective cap above a byte range"},
			s3PartNumberLimit, s3UploadIDLimit,
		},
		Release:    []Release{{Member: "SSEKMSKeyId", Reason: "the model marks the KMS key ID sensitive, but it is an identifier and not a secret"}},
		Guards:     []string{"copy-source", "multipart-copy-range"},
		SampleArgs: `{"copy_source":"my-source-bucket/path/key.txt","copy_source_range":"bytes=0-1023"}`,
		Examples: []Example{{Description: "Copy the first 64 MiB of an object into part 1",
			Arguments: `{"bucket":"my-bucket","key":"merged.bin","upload_id":"EXAMPLEID","part_number":1,"copy_source":"my-bucket/source.bin","copy_source_range":"bytes=0-67108863"}`}},
	}, "multipart", "upload-part-copy"),

	s3Object(Operation{
		Operation: "CompleteMultipartUpload", Object: "multipart-upload", Action: "complete", Effect: "update", Idempotency: "non-idempotent",
		FailureHint: "multipart-too-small",
		Title:       "Complete a multipart upload of an S3 object",
		Description: "Join the parts of a multipart upload into the object: multipart_upload.parts lists each part with its part_number, " +
			"e_tag, and checksum_sha256 (the etag and checksum_sha256 of the part's answer) as that answer gave them (S3 wants the checksum of every part), in ascending part order, at most " + strconv.Itoa(s3MaxPartNumber) + " parts. Every part but the last must hold " +
			"at least 5 MiB, otherwise S3 answers EntityTooSmall. An object with the key is replaced by a new version unless " +
			"if_none_match * or if_match is set. S3 can answer an error after a success status, and then the object may not have been " +
			"created; the tool reports that as an error. Completing can take a while for a large object; the call limit of 60 seconds " +
			"of this provider applies. Not idempotent: once completed, the upload ID is gone. Needs s3:PutObject." + s3MultipartCost,
		Arguments: map[string]Argument{
			"Key":             {Description: "Key of the object, as given to create; untrusted text"},
			"MultipartUpload": {Description: "parts: the list of parts, each with part_number (1 to " + strconv.Itoa(s3MaxPartNumber) + "), e_tag (the etag upload-part answered), and checksum_sha256, in ascending part order"},
			"UploadId":        {Description: "ID of the upload, from create"},
			"MpuObjectSize":   {Description: "Expected size of the finished object in bytes; S3 refuses the call when the parts add up to another size"},
			"IfMatch":         {Description: "Write only when the current object has this ETag; otherwise S3 answers PreconditionFailed"},
			"IfNoneMatch":     {Description: "Write only when no object has the key: the value * ; otherwise S3 answers PreconditionFailed"},
		},
		Fields: s3Merge(map[string]Field{
			"Location":             {Description: "URL of the new object, as S3 reports it"},
			"Bucket":               {Description: "Name of the bucket"},
			"Key":                  {Description: "Key of the new object; untrusted text"},
			"Expiration":           {Description: "Expiration of the object under a lifecycle rule, when one applies: expiry-date and rule-id"},
			"ETag":                 {Name: "etag", Description: "ETag of the new object"},
			"ServerSideEncryption": {Description: "Server-side encryption of the new object"},
			"VersionId":            {Description: "Version ID of the new object; left out in a bucket without versioning"},
			"SSEKMSKeyId":          {Description: "ARN of the KMS key that encrypts the object, when SSE-KMS applies"},
			"BucketKeyEnabled":     {Description: "Whether an S3 Bucket Key was used"},
		}, s3ChecksumOutputs()),
		HiddenInputs: slices.Concat(hiddenAll(s3SSECWhy, "SSECustomerAlgorithm", "SSECustomerKey", "SSECustomerKeyMD5"),
			[]Hidden{s3NoPayer, {Member: "ChecksumType", Reason: "the checksum type is the one the upload was started with (composite for SHA256)"}}),
		HiddenOutputs: []Hidden{s3NoPaid},
		Release:       []Release{{Member: "SSEKMSKeyId", Reason: "the ARN of a KMS key, an identifier and not key material"}},
		Limits: []InputLimit{
			{Member: "Key", MaxLen: 1024, Reason: s3KeyLimit}, s3UploadIDLimit,
			{Member: "IfMatch", MaxLen: 1024, Reason: "the model sets no bound; a protective cap above the length of an ETag"},
			{Member: "IfNoneMatch", MaxLen: 1024, Reason: "the model sets no bound; a protective cap above the length of an ETag"},
			{Member: "MultipartUpload.Parts", MaxItems: s3MaxPartNumber, Reason: "at most the part numbers this package takes"},
			{Member: "MultipartUpload.Parts.PartNumber", MinValue: 1, MaxValue: s3MaxPartNumber, Reason: "the part numbers this package takes"},
			{Member: "MultipartUpload.Parts.ETag", MaxLen: 1024, Reason: "the model sets no bound; a protective cap above the length of an ETag"},
		},
		Examples: []Example{{Description: "Complete an upload of two parts",
			Arguments: `{"bucket":"my-bucket","key":"backups/2026.tar","upload_id":"EXAMPLEID","multipart_upload":{"parts":[{"part_number":1,"e_tag":"\"a54357aff0632cce46d942af68356b38\""},{"part_number":2,"e_tag":"\"0c78aef83f66abc1fa1e8477f296d394\""}]}}`}},
	}, "multipart", "complete"),

	s3Object(Operation{
		Operation: "AbortMultipartUpload", Object: "multipart-upload", Action: "abort", Effect: "delete", Idempotency: "non-idempotent",
		Title: "Abort a multipart upload to an S3 bucket",
		Description: "Abort a multipart upload and delete the parts sent so far, which stops their storage costs. The upload ID is gone afterwards; " +
			"S3 answers NoSuchUpload for an upload that does not exist or was completed or aborted already. A part that was " +
			"still being sent when the upload is aborted can remain, so list-parts afterwards shows whether any is left. Needs s3:AbortMultipartUpload.",
		Arguments: map[string]Argument{
			"Key":                  {Description: "Key of the object, as given to create; untrusted text"},
			"UploadId":             {Description: "ID of the upload, from create"},
			"IfMatchInitiatedTime": {Description: "Abort only when the upload was started at this RFC 3339 date-time; otherwise S3 answers PreconditionFailed"},
		},
		HiddenInputs:  []Hidden{s3NoPayer},
		HiddenOutputs: []Hidden{s3NoPaid},
		Limits:        s3MultipartIDLimits,
		Examples: []Example{{Description: "Abort an upload",
			Arguments: `{"bucket":"my-bucket","key":"backups/2026.tar","upload_id":"EXAMPLEID"}`}},
	}, "multipart", "abort"),

	s3Object(Operation{
		Operation: "ListMultipartUploads", Object: "multipart-upload", Action: "list", Effect: "read",
		Title: "List the multipart uploads in progress of an S3 bucket",
		Description: "List the multipart uploads of a bucket that are neither completed nor aborted, in key order, one page at a time: key, " +
			"upload_id, start time, and storage class of each. Uploads that nobody finishes are billed as storage; abort them." +
			" Needs s3:ListBucketMultipartUploads." + s3Requests,
		Arguments: map[string]Argument{
			"Delimiter":    {Description: "Character that groups keys into common prefixes, usually /"},
			"EncodingType": {Description: "url: S3 percent-encodes keys, prefixes, and the delimiter in the answer; decode them before use"},
			"Prefix":       {Description: "Only keys that begin with this prefix; untrusted text"},
		},
		HiddenInputs: []Hidden{s3NoPayer},
		Limits: []InputLimit{
			{Member: "Prefix", MaxLen: 1024, Reason: s3KeyLimit},
			{Member: "Delimiter", MaxLen: 64, Reason: "the model sets no bound; a protective cap"},
		},
		PageSizeMax: 1000,
		Fields: map[string]Field{
			"Bucket":         {Description: "Name of the bucket"},
			"KeyMarker":      {Description: "The key marker this page continued at"},
			"UploadIdMarker": {Description: "The upload marker this page continued at"},
			"Prefix":         {Description: "The prefix the list was filtered by"},
			"Delimiter":      {Description: "The delimiter of the list"},
			"MaxUploads":     {Description: "Page size S3 applied"},
			"Uploads":        {Description: "Uploads of this page, each with key, upload_id, initiated, storage_class, owner, initiator, checksum_algorithm, and checksum_type; untrusted text", MaxItems: 1000},
			"CommonPrefixes": {Description: "Common prefixes of this page, each with prefix; untrusted text", MaxItems: 1000},
			"EncodingType":   {Description: "url when keys are percent-encoded"},
		},
		HiddenOutputs: []Hidden{s3NoPaid},
		Examples:      []Example{{Description: "List the uploads in progress", Arguments: `{"bucket":"my-bucket"}`}},
	}, "multipart", "list"),

	s3Object(Operation{
		Operation: "ListParts", Object: "multipart-upload", Action: "list-parts", Effect: "read",
		Title: "List the parts sent so far of a multipart upload",
		Description: "List the parts of one multipart upload in part order, one page at a time: part number, size, ETag, last modified, and " +
			"checksums of each. Use it to see which parts are present before complete, or what is left after abort. Needs s3:ListMultipartUploadParts." + s3Requests,
		Arguments: map[string]Argument{
			"Key":      {Description: "Key of the object, as given to create; untrusted text"},
			"UploadId": {Description: "ID of the upload, from create"},
		},
		HiddenInputs: slices.Concat(hiddenAll(s3SSECWhy, "SSECustomerAlgorithm", "SSECustomerKey", "SSECustomerKeyMD5"), []Hidden{s3NoPayer}),
		Limits:       s3MultipartIDLimits,
		PageSizeMax:  1000,
		Fields: map[string]Field{
			"AbortDate":         {Description: "Date after which a lifecycle rule aborts the upload, when one applies"},
			"AbortRuleId":       {Description: "ID of the lifecycle rule that aborts the upload, when one applies"},
			"Bucket":            {Description: "Name of the bucket"},
			"Key":               {Description: "Key of the object being uploaded; untrusted text"},
			"UploadId":          {Description: "ID of the upload"},
			"PartNumberMarker":  {Description: "The part number this page continued after"},
			"MaxParts":          {Description: "Page size S3 applied"},
			"Parts":             {Description: "Parts of this page, each with part_number, last_modified, e_tag, size, and checksums", MaxItems: 1000},
			"Initiator":         {Description: "Who started the upload: id and display_name"},
			"Owner":             {Description: "Owner of the object: id and display_name"},
			"StorageClass":      {Description: "Storage class of the object"},
			"ChecksumAlgorithm": {Description: "Checksum algorithm of the upload, when S3 states one"},
			"ChecksumType":      {Description: "Whether checksums cover the whole object (FULL_OBJECT) or are checksums of part checksums (COMPOSITE)"},
		},
		HiddenOutputs: []Hidden{s3NoPaid},
		Examples: []Example{{Description: "List the parts of an upload",
			Arguments: `{"bucket":"my-bucket","key":"backups/2026.tar","upload_id":"EXAMPLEID"}`}},
	}, "multipart", "list-parts"),
}

// withData sets the data classification of the content of an object, for a tool that moves content.
func withData(op Operation) Operation {
	op.DataSensitivity = s3ObjectData
	return op
}

// s3PartChecksumOutputs are the checksum members of the answer to an upload of a part: the checksum of the part, in
// base64 as S3 states it, for each algorithm.
func s3PartChecksumOutputs() map[string]Field {
	out := map[string]Field{}
	for name, f := range s3ChecksumOutputs() {
		if name != "ChecksumType" {
			f.Description = strings.Replace(f.Description, "checksum of the object", "checksum of the part", 1)
			out[name] = f
		}
	}
	return out
}
