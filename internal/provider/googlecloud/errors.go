package googlecloud

import (
	"net/http"
	"strconv"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/provider/googleapi"
)

const subject = "Google Cloud"

// etagConflict is the message of a write Google refused because the resource changed since it was read.
const etagConflict = "conflict: " + subject + " refused the change because the policy changed since it was " +
	"read (etag); nothing was written, read the policy again and repeat the change if it still applies"

// failure is a provider error of the generic class.
func failure(op, message string) *provider.Error {
	return &provider.Error{Class: provider.ClassProviderError, Op: op, Message: message}
}

// badRequest is the refusal of an argument. It names the argument and the rule, never the value.
func badRequest(message string) error {
	return &provider.InvalidRequestError{Message: message}
}

// googleError maps a failed Google answer to a provider error. Only values Google documents decide beyond the
// HTTP status. The status string of the error object is a google.rpc.Code: UNAUTHENTICATED (401),
// PERMISSION_DENIED (403), NOT_FOUND (404), ABORTED (409) and RESOURCE_EXHAUSTED (429). The reason of its ErrorInfo comes
// from google.api.ErrorReason: SERVICE_DISABLED, ACCESS_TOKEN_EXPIRED, ACCESS_TOKEN_SCOPE_INSUFFICIENT,
// RATE_LIMIT_EXCEEDED and RESOURCE_QUOTA_EXCEEDED; CREDENTIALS_MISSING is the reason an anonymous request
// gets. A 401, 403 or 404 that carries none of these is an invalid response rather than a guess. The message
// and the details of the body never reach the message; they go along as the Original.
func googleError(op string, status int, body []byte) *provider.Error {
	state, reason := googleapi.StatusOf(body)
	failed := func(class provider.Class, message string) *provider.Error {
		return &provider.Error{Class: class, Op: op, Message: message,
			Original: provider.ErrorOriginal(status, body, googleapi.Fields)}
	}
	invalid := func() *provider.Error {
		return failed(provider.ClassInvalidResponse, subject+" answered with HTTP "+strconv.Itoa(status)+
			" and an error that Qatlas Connect does not know")
	}
	switch status {
	case http.StatusUnauthorized:
		if reason == "CREDENTIALS_MISSING" || reason == "ACCESS_TOKEN_EXPIRED" || state == "UNAUTHENTICATED" {
			return failed(provider.ClassAuth, subject+" did not accept the access token of the credential")
		}
		return invalid()
	case http.StatusForbidden:
		switch {
		case reason == "SERVICE_DISABLED":
			return failed(provider.ClassPermission, subject+" refused the request because a Google API it needs "+
				"(Cloud Resource Manager, IAM, or Service Usage) is not enabled in the Cloud project of the credential; a person enables it there")
		case reason == "ACCESS_TOKEN_SCOPE_INSUFFICIENT":
			return failed(provider.ClassScopeMissing, "the access token of the credential lacks a scope this tool needs")
		case state == "PERMISSION_DENIED":
			return failed(provider.ClassPermission, subject+" refused the credential for this resource; check the "+
				"roles of the account on it and that the Cloud Resource Manager and IAM APIs are enabled")
		}
		return invalid()
	case http.StatusNotFound:
		if state == "NOT_FOUND" {
			return failed(provider.ClassNotFound, subject+" did not find the resource, or the credential may not see it")
		}
		return invalid()
	case http.StatusBadRequest:
		return failed(provider.ClassInvalidResponse, subject+" rejected the request as invalid (HTTP 400) although "+
			"Qatlas Connect checked it; the API may have changed")
	case http.StatusConflict:
		// setIamPolicy answers a stale etag with 409 ABORTED. The provider classes hold no conflict, so the
		// message carries it; nothing repeats the request, the caller reads again.
		if state == "ABORTED" {
			return failed(provider.ClassProviderError, etagConflict)
		}
		return invalid()
	case http.StatusTooManyRequests:
		return failed(provider.ClassRateLimited, subject+" is rate-limiting requests or the quota is used up; wait and try again")
	}
	if status >= 400 && status < 500 {
		return invalid()
	}
	mapped := provider.ClassifyStatus(op, status, provider.StatusTexts{Subject: subject})
	mapped.Original = provider.ErrorOriginal(status, body, googleapi.Fields)
	return mapped
}
