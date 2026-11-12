package awserr

import (
	"net/http"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/provider/aws/endpoints"
)

// The rules of the restXml services (S3, Route 53). Their models define the error shapes by the wire code,
// so every client error code of the two models is a row; a code that is in neither this table nor the shared
// one stays an unknown code for ClassifyXML.
var (
	// badState is a refusal because of the state the entity is in, not because of the request itself.
	badState = rule{provider.ClassProviderError, Subject + " refuses the operation in the current state of the entity; read it again before retrying"}
	// xmlInSuccess is an Error element in the body of an answer with a success status.
	xmlInSuccess = rule{provider.ClassProviderError, Subject + " reported an error in an answer with a success status; the operation may not have completed"}

	xmlRules = map[string]rule{
		// Route 53 answers PriorRequestNotComplete (400) while another change of the zone is still running.
		// It is treated like a rate limit: repeated only by a safe read with a Retry-After, never by a write.
		"PriorRequestNotComplete": rateLimited,

		// S3.
		"NoSuchBucket": notFound, "NoSuchKey": notFound, "NoSuchUpload": notFound, "NoSuchAnnotation": notFound,
		"NotFound":                notFound,
		"BucketAlreadyExists":     conflict,
		"BucketAlreadyOwnedByYou": conflict, "IdempotencyParameterMismatch": conflict,
		// The S3 codes of the bucket writes: the model defines none of them, the service sends them.
		"BucketNotEmpty": conflict, "OperationAborted": conflict, "TooManyBuckets": quota,
		"InvalidBucketName": invalid, "InvalidLocationConstraint": invalid, "MalformedXML": invalid,
		"InvalidBucketState": badState,
		// The codes of the object tools: a failed condition, a restore that is already running, a version or a range
		// that does not exist, and a method the object does not allow (a delete marker).
		"PreconditionFailed": badState, "ConditionalRequestConflict": conflict, "RestoreAlreadyInProgress": conflict,
		"NoSuchVersion": notFound, "InvalidRange": invalid, "MethodNotAllowed": invalid,
		"InvalidObjectState": badState, "ObjectAlreadyInActiveTierError": badState, "ObjectNotInActiveTierError": badState,
		"EncryptionTypeMismatch": invalid, "InvalidAnnotationName": invalid, "InvalidPrefix": invalid,
		"InvalidRequest": invalid, "InvalidWriteOffset": invalid, "TooManyParts": invalid,
		"UnsupportedMediaType": invalid, "AnnotationNameTooLong": invalid,
		"AnnotationLimitExceeded": quota,
		// A bucket without such a configuration; the models do not define these codes, the service sends them.
		"ServerSideEncryptionConfigurationNotFoundError": notFound, "NoSuchTagSet": notFound,
		"NoSuchPublicAccessBlockConfiguration": notFound, "NoSuchLifecycleConfiguration": notFound,
		"NoSuchBucketPolicy": notFound, "MalformedPolicy": invalid,
		"NoSuchCORSConfiguration": notFound, "NoSuchWebsiteConfiguration": notFound,
		"ReplicationConfigurationNotFoundError": notFound, "OwnershipControlsNotFoundError": notFound,
		"ObjectLockConfigurationNotFoundError": notFound, "NoSuchObjectLockConfiguration": notFound,

		// Route 53.
		"NoSuchChange": notFound, "NoSuchCidrCollectionException": notFound, "NoSuchCidrLocationException": notFound,
		"NoSuchCloudWatchLogsLogGroup": notFound, "NoSuchDelegationSet": notFound, "NoSuchGeoLocation": notFound,
		"NoSuchHealthCheck": notFound, "NoSuchHostedZone": notFound, "NoSuchKeySigningKey": notFound,
		"NoSuchQueryLoggingConfig": notFound, "NoSuchTrafficPolicy": notFound, "NoSuchTrafficPolicyInstance": notFound,
		"VPCAssociationAuthorizationNotFound": notFound, "VPCAssociationNotFound": notFound, "HostedZoneNotFound": notFound,
		"DNSSECNotFound": notFound, "KeySigningKeyWithActiveStatusNotFound": notFound,

		"CidrBlockInUseException": conflict, "CidrCollectionAlreadyExistsException": conflict,
		"CidrCollectionInUseException": conflict, "CidrCollectionVersionMismatchException": conflict,
		"ConflictingDomainExists": conflict, "ConflictingTypes": conflict, "DelegationSetAlreadyCreated": conflict,
		"DelegationSetAlreadyReusable": conflict, "DelegationSetInUse": conflict, "HealthCheckAlreadyExists": conflict,
		"HealthCheckInUse": conflict, "HealthCheckVersionMismatch": conflict, "HostedZoneAlreadyExists": conflict,
		"HostedZoneNotEmpty": conflict, "KeySigningKeyAlreadyExists": conflict, "KeySigningKeyInUse": conflict,
		"QueryLoggingConfigAlreadyExists": conflict, "TrafficPolicyAlreadyExists": conflict,
		"TrafficPolicyInUse": conflict, "TrafficPolicyInstanceAlreadyExists": conflict,

		"DelegationSetNotAvailable": badState, "DelegationSetNotReusable": badState, "HostedZoneNotPrivate": badState,
		"HostedZonePartiallyDelegated": badState, "IncompatibleVersion": badState,
		"InsufficientCloudWatchLogsResourcePolicy": badState, "InvalidKeySigningKeyStatus": badState,
		"InvalidSigningStatus": badState, "KeySigningKeyInParentDSRecord": badState, "LastVPCAssociation": badState,
		"PublicZoneVPCAssociation": badState,

		"InvalidArgument": invalid, "InvalidChangeBatch": invalid, "InvalidDomainName": invalid, "InvalidInput": invalid,
		"InvalidKMSArn": invalid, "InvalidKeySigningKeyName": invalid, "InvalidPaginationToken": invalid,
		"InvalidTrafficPolicyDocument": invalid, "InvalidVPCId": invalid,

		"LimitsExceeded": quota, "TooManyHealthChecks": quota, "TooManyHostedZones": quota, "TooManyKeySigningKeys": quota,
		"TooManyTrafficPolicies": quota, "TooManyTrafficPolicyInstances": quota,
		"TooManyTrafficPolicyVersionsForCurrentPolicy": quota, "TooManyVPCAssociationAuthorizations": quota,

		"NotAuthorizedException": denied,
	}
)

// ClassifyXML is ClassifyStrict for the restXml services. It adds three things. The rows of xmlRules. S3's
// SlowDown with status 503, which AWS documents as a rate limit but the model does not define: it is a rate
// limit here, with no wait of its own, so only a Retry-After the answer carries is ever honoured. And the
// answers a status alone explains: a 404 without a code (a HEAD answer has no body) is a missing entity. An
// answer with a 2xx status is an Error element found in the body of a success: it is never the status that
// decides, and the operation may not have completed, which the message says.
func ClassifyXML(op string, status int, header http.Header, code, errType string) (*provider.Error, time.Duration) {
	if status >= 200 && status <= 299 {
		r, ok := xmlRules[code]
		if !ok {
			r, ok = codeRules[code]
		}
		if !ok || r.class == provider.ClassInvalidResponse {
			r = xmlInSuccess
		}
		return finish(op, status, header, code, errType, r)
	}
	if wrongRegion(status, code) {
		return finish(op, status, header, code, errType, regionRule(header))
	}
	if r, ok := xmlRules[code]; ok {
		return finish(op, status, header, code, errType, r)
	}
	if code == "SlowDown" && status == http.StatusServiceUnavailable {
		return finish(op, status, header, code, errType, rateLimited)
	}
	if code == "" && status == http.StatusNotFound {
		return finish(op, status, header, code, errType, notFound)
	}
	return ClassifyStrict(op, status, header, code, errType)
}

// wrongRegion reports the answers S3 gives when the bucket lives in another region than the one called: a
// redirect, PermanentRedirect, AuthorizationHeaderMalformed (the signature names the wrong region) and the
// endpoint codes. None is followed: the caller is told the region and asks again.
func wrongRegion(status int, code string) bool {
	switch code {
	case "PermanentRedirect", "AuthorizationHeaderMalformed", "IllegalLocationConstraintException", "IncorrectEndpoint":
		return true
	}
	return code == "" && (status == http.StatusMovedPermanently || status == http.StatusTemporaryRedirect || status == http.StatusPermanentRedirect)
}

// regionRule names the region the answer reports in x-amz-bucket-region, a provider value that is used only
// when it has the form of a region of the aws partition.
func regionRule(header http.Header) rule {
	message := Subject + " says the bucket is in another region than the one called; no redirect is followed"
	if region := header.Get("X-Amz-Bucket-Region"); endpoints.ValidRegion(region) == nil {
		message += "; call again with region " + region
	} else {
		message += "; look the region up with the bucket location first"
	}
	return rule{provider.ClassProviderError, message}
}

func finish(op string, status int, header http.Header, code, errType string, r rule) (*provider.Error, time.Duration) {
	if r.class == provider.ClassRateLimited {
		failure, hold := limited(op, header)
		failure.Original = original(status, code, errType)
		return failure, hold
	}
	failure := &provider.Error{Class: r.class, Op: op, Message: r.message}
	failure.Original = original(status, code, errType)
	return failure, 0
}

// Defines reports whether code is an error code of the restXml services' rows, shared or their own. It lets a
// codec trust a bare element name as a code only when the name is one.
func Defines(code string) bool {
	if _, ok := xmlRules[code]; ok {
		return true
	}
	r, ok := codeRules[code]
	return ok && r.class != provider.ClassInvalidResponse
}
