package aws

import (
	"context"
	"regexp"
	"slices"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/provider/aws/endpoints"
	"github.com/castrowithcee/qatlas-connect/internal/provider/aws/httpbind"
	"github.com/castrowithcee/qatlas-connect/internal/provider/aws/sigv4"
)

// The S3 write tools. Two kinds of hand-written step belong to them: a guard judges arguments before any
// I/O, a readback reads the current state of the bucket and refuses a change that would loosen it.

// guard judges the arguments of a tool with the binding and the region of the call before any I/O.
type guard func(b binding, region string, args map[string]any) error

// guards are the guards, by the name a curation table gives them.
var guards = map[string]guard{
	"location-constraint":         checkLocationConstraint,
	"new-bucket-name":             checkNewBucketName,
	"kms-key-arn":                 checkKMSKeys,
	"object-kms-key":              checkObjectKMSKey,
	"copy-source":                 checkCopySource,
	"upload-kms-key":              checkUploadKMSKey,
	"ownership-enforce":           checkOwnershipEnforce,
	"object-lock-governance-only": checkObjectLockGovernanceOnly,
	"route53-vpc":                 checkRoute53VPC,
	"route53-vpc-lookup":          checkRoute53VPCLookup,
	"route53-change":              checkRoute53Change,
	"route53-name":                checkRoute53Name,
	"route53-health":              checkRoute53Health,
	"route53-ksk":                 checkRoute53KSK,
	"route53-log-group":           checkRoute53LogGroup,
	"route53-policy":              checkRoute53Policy,
	"ses-send":                    checkSESSend,
	"route53-cidr":                checkRoute53CIDR,

	"ses-configuration-set-values": checkSESConfigurationSetValues,
}

// readback runs after the account check and before the change, with the credentials that passed it.
type readback func(ctx context.Context, c *Client, s toolSpec, creds sigv4.Credentials, region string, args map[string]any) error

// readbacks are the readbacks, by the name a curation table gives them.
var readbacks = map[string]readback{"pab-tighten": pabTighten}

// usEast1 is the region S3 creates a bucket in without a location constraint.
const usEast1 = "us-east-1"

// checkNewBucketName holds a new bucket to the rules of today: lower case letters, digits, hyphens and dots.
// Upper case letters and underscores are valid only for buckets that already exist.
func checkNewBucketName(_ binding, _ string, args map[string]any) error {
	name, _ := args["bucket"].(string)
	if !httpbind.ValidBucketName(name) || strings.ContainsAny(name, "_ABCDEFGHIJKLMNOPQRSTUVWXYZ") {
		return &provider.InvalidRequestError{Message: "bucket must be a new bucket name: 3 to 63 lower case letters, digits, hyphens and dots"}
	}
	return nil
}

// checkLocationConstraint requires the location constraint of a new bucket to be the region of the call, which
// the connection allows. Without one the bucket lands in us-east-1, which is then the only region that may
// be called.
func checkLocationConstraint(b binding, region string, args map[string]any) error {
	config, _ := args["create_bucket_configuration"].(map[string]any)
	constraint, set := config["location_constraint"]
	if !set {
		if region != usEast1 {
			return &provider.InvalidRequestError{Message: "create_bucket_configuration.location_constraint is required outside us-east-1 and must be the region of the call"}
		}
		return nil
	}
	text, _ := constraint.(string)
	if !slices.Contains(b.regions, text) || text != region {
		return &provider.InvalidRequestError{Message: "create_bucket_configuration.location_constraint must be the region of the call, which the connection allows"}
	}
	return nil
}

// checkOwnershipEnforce requires the ownership controls to be exactly one rule with BucketOwnerEnforced, checked
// as a structure: a missing or empty value, an empty rule list, more than one rule, and any other value are refused.
func checkOwnershipEnforce(_ binding, _ string, args map[string]any) error {
	const message = "ownership_controls must be exactly one rule with object_ownership BucketOwnerEnforced"
	controls, _ := args["ownership_controls"].(map[string]any)
	rules, _ := controls["rules"].([]any)
	if len(rules) != 1 {
		return &provider.InvalidRequestError{Message: message}
	}
	rule, _ := rules[0].(map[string]any)
	if value, _ := rule["object_ownership"].(string); len(rule) != 1 || value != "BucketOwnerEnforced" {
		return &provider.InvalidRequestError{Message: message}
	}
	return nil
}

// checkObjectLockGovernanceOnly requires every retention mode of the call to be exactly GOVERNANCE: the mode of an
// object retention, and the mode of the default retention of a bucket configuration. Any other value, a value of
// another type or case, and a retention without a mode are refused, so COMPLIANCE cannot get through in any
// spelling. A configuration without a default retention only turns Object Lock on. A call with neither member is refused.
func checkObjectLockGovernanceOnly(_ binding, _ string, args map[string]any) error {
	const message = "the retention mode must be GOVERNANCE; COMPLIANCE is not offered"
	check := func(retention any) error {
		fields, _ := retention.(map[string]any)
		if mode, _ := fields["mode"].(string); mode != "GOVERNANCE" {
			return &provider.InvalidRequestError{Message: message}
		}
		return nil
	}
	retention, hasRetention := args["retention"]
	config, hasConfig := args["object_lock_configuration"]
	if !hasRetention && !hasConfig {
		// Both members are optional in the model; a call without either carries no body to judge.
		return &provider.InvalidRequestError{Message: "retention or object_lock_configuration is required"}
	}
	if hasRetention {
		if err := check(retention); err != nil {
			return err
		}
	}
	if hasConfig {
		config, _ := config.(map[string]any)
		rule, _ := config["rule"].(map[string]any)
		if retention, set := rule["default_retention"]; set {
			return check(retention)
		}
	}
	return nil
}

var (
	kmsKeyID     = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
	kmsAliasName = regexp.MustCompile(`^[A-Za-z0-9/_-]{1,256}$`)
)

// checkKMSKeys requires every KMS key of the encryption rules to be named by the ARN of a key or an alias in
// the bound account and an allowed region. A bare key ID or alias name carries no account and is refused.
func checkKMSKeys(b binding, _ string, args map[string]any) error {
	config, _ := args["server_side_encryption_configuration"].(map[string]any)
	rules, _ := config["rules"].([]any)
	for _, rule := range rules {
		rule, _ := rule.(map[string]any)
		by, _ := rule["apply_server_side_encryption_by_default"].(map[string]any)
		value, set := by["kms_master_key_id"]
		if !set {
			continue
		}
		text, _ := value.(string)
		if !kmsARN(b, text) {
			return &provider.InvalidRequestError{Message: "kms_master_key_id must be the ARN of a KMS key or alias in the account this connection is bound to and a region it allows"}
		}
	}
	return nil
}

// checkObjectKMSKey requires the KMS key of a written object to be named like the key of a bucket encryption rule.
func checkObjectKMSKey(b binding, _ string, args map[string]any) error {
	value, set := args["ssekms_key_id"]
	if !set {
		return nil
	}
	if text, _ := value.(string); !kmsARN(b, text) {
		return &provider.InvalidRequestError{Message: "ssekms_key_id must be the ARN of a KMS key or alias in the account this connection is bound to and a region it allows"}
	}
	return nil
}

// checkCopySource requires the source of a copy to be bucket/key with an optional versionId, naming a general
// purpose bucket. Access point and other ARNs are refused because their account would not be the one the
// expected-owner header checks; a plain bucket of another account is refused by S3 on that header.
func checkCopySource(_ binding, _ string, args map[string]any) error {
	const message = "copy_source must be bucket/key, percent-encoded, with an optional ?versionId=ID, naming a bucket of the bound account"
	source, _ := args["copy_source"].(string)
	path, query, hasQuery := strings.Cut(strings.TrimPrefix(source, "/"), "?")
	bucket, key, ok := strings.Cut(path, "/")
	if !ok || key == "" || !httpbind.ValidBucketName(bucket) {
		return &provider.InvalidRequestError{Message: message}
	}
	if id, isVersion := strings.CutPrefix(query, "versionId="); hasQuery && (!isVersion || id == "" || strings.ContainsAny(id, "&?")) {
		return &provider.InvalidRequestError{Message: message}
	}
	return nil
}

func kmsARN(b binding, value string) bool {
	parts := strings.SplitN(value, ":", 6)
	if len(parts) != 6 || parts[0] != "arn" || parts[1] != "aws" || parts[2] != "kms" ||
		!slices.Contains(b.regions, parts[3]) || parts[4] != b.account {
		return false
	}
	if id, ok := strings.CutPrefix(parts[5], "key/"); ok {
		return kmsKeyID.MatchString(id)
	}
	name, ok := strings.CutPrefix(parts[5], "alias/")
	return ok && kmsAliasName.MatchString(name)
}

// pabSwitches are the four settings of a public access block, by argument key.
var pabSwitches = []string{"block_public_acls", "ignore_public_acls", "block_public_policy", "restrict_public_buckets"}

const (
	pabArgument = "public_access_block_configuration"
	pabReadTool = "aws.s3.public-access-block.get"
	pabMissing  = "NoSuchPublicAccessBlockConfiguration"
)

// pabTighten reads the public access block of the bucket and refuses a change that turns a setting that is on
// off, or leaves it out. A bucket without a configuration has all four off. Any other failure to read, and an
// answer that is not a configuration (or none of whose four settings is there), refuse the change: a block that cannot be seen is not loosened. Another
// writer can still change the block between this read and the change.
func pabTighten(ctx context.Context, c *Client, s toolSpec, creds sigv4.Credentials, region string, args map[string]any) error {
	read := slices.IndexFunc(generatedSpecs, func(g toolSpec) bool { return g.descriptor.ID == pabReadTool })
	if read < 0 || s.service != endpoints.S3 {
		return provider.Fail(s.op, "the tool description is invalid")
	}
	bucket, _ := args["bucket"].(string)
	_, send, err := generatedSpecs[read].encode(map[string]any{"bucket": bucket}, nil, c.binding.account)
	if err != nil {
		return err
	}
	var out map[string]any
	current := map[string]bool{}
	_, failure := send(ctx, c, creds, region, &out)
	switch {
	case failure == nil:
		config, isConfig := out[pabArgument].(map[string]any)
		if !isConfig {
			return provider.InvalidResponse(s.op, "AWS returned an invalid response to the read of the current public access block; nothing was changed")
		}
		seen := 0
		for _, name := range pabSwitches {
			value, present := config[name]
			on, isBool := value.(bool)
			if present && !isBool {
				return provider.InvalidResponse(s.op, "AWS returned an invalid response to the read of the current public access block; nothing was changed")
			}
			if present {
				seen++
			}
			current[name] = on
		}
		if seen == 0 {
			// A configuration without any setting is not one S3 sends; it may be another document.
			return provider.InvalidResponse(s.op, "AWS returned an invalid response to the read of the current public access block; nothing was changed")
		}
	case failure.Class == provider.ClassNotFound && failure.Original != nil && failure.Original.Status == 404 &&
		len(failure.Original.Entries) == 1 && failure.Original.Entries[0].Code == pabMissing:
		// No configuration: all four are off.
	default:
		return &provider.Error{Class: failure.Class, Op: s.op, Message: "the current public access block could not be read " +
			"(this needs s3:GetBucketPublicAccessBlock), so the change was not sent: " + failure.Message}
	}
	wanted, _ := args[pabArgument].(map[string]any)
	var loosened []string
	for _, name := range pabSwitches {
		if on, _ := wanted[name].(bool); current[name] && !on {
			loosened = append(loosened, name)
		}
	}
	if len(loosened) > 0 {
		return &provider.InvalidRequestError{Message: pabArgument + " would turn off " + strings.Join(loosened, ", ") +
			" or leave it out; this tool keeps or tightens a public access block and never loosens it"}
	}
	return nil
}

const policyStatusTool = "aws.s3.bucket-policy.get-status"

// addBucketPolicyStatus sets public_status after a policy was written: is_public as S3 evaluates the policy,
// read with a call of its own. The write has happened by then, so a status that cannot be read is no failure of
// the tool; it is reported as unavailable, without the answer of AWS. The status is a hint and no check.
func addBucketPolicyStatus(ctx context.Context, c *Client, s toolSpec, creds sigv4.Credentials, result map[string]any, region string, args map[string]any) {
	unavailable := map[string]any{"unavailable": "the policy was written, but its public status could not be read " +
		"(this needs s3:GetBucketPolicyStatus); read it with " + policyStatusTool}
	result["public_status"] = unavailable
	read := slices.IndexFunc(generatedSpecs, func(g toolSpec) bool { return g.descriptor.ID == policyStatusTool })
	if read < 0 || s.service != endpoints.S3 {
		return
	}
	bucket, _ := args["bucket"].(string)
	_, send, err := generatedSpecs[read].encode(map[string]any{"bucket": bucket}, nil, c.binding.account)
	if err != nil {
		return
	}
	var out map[string]any
	if _, failure := send(ctx, c, creds, region, &out); failure != nil {
		return
	}
	status, _ := out["policy_status"].(map[string]any)
	if public, ok := status["is_public"].(bool); ok {
		result["public_status"] = map[string]any{"is_public": public}
	}
}

// failureHint adds to a failure of a call what a person needs to know about it.
type failureHint func(failure *provider.Error) *provider.Error

// failureHints are the hand-written failure steps, by the name a curation table gives them.
var failureHints = map[string]failureHint{"bucket-policy-lockout": bucketPolicyLockout}

// bucketPolicyLockout explains the two failures of a policy write that can mean the policy locked out its own
// writer: AccessDenied and MalformedPolicy. The class and the original stay; only the message grows.
func bucketPolicyLockout(failure *provider.Error) *provider.Error {
	if failure.Original == nil {
		return failure
	}
	for _, entry := range failure.Original.Entries {
		if entry.Code == "AccessDenied" || entry.Code == "MalformedPolicy" {
			out := *failure
			out.Message += "; if the policy was meant to be active, check that it does not deny the access of this connection: " +
				"a policy that locks out its own writer can only be removed by the root user of the account"
			return &out
		}
	}
	return failure
}
