package googleads

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// The List...AsyncErrors methods of Google Ads (ListExperimentAsyncErrors, ListCampaignDraftAsyncErrors) share
// one form: GET <resource name>:<method>?pageSize&pageToken, answered with errors[] of google.rpc.Status and
// next_page_token. A tool of such a family names the resource it asks about and the method; the rest is here.

// Google documents no length for these page tokens; the bounds follow those of the field tools.
const (
	maxAsyncPageToken = maxPageToken
	maxAsyncCursor    = maxFieldCursor
)

// asyncErrorsSpec is one page of the async errors of one object of the checked account.
type asyncErrorsSpec struct {
	op, tool, customerID string
	// resource is the resource name of the object, built from checked IDs alone.
	resource string
	// method is the custom method of the service, such as listExperimentAsyncErrors.
	method   string
	pageSize *int
	cursor   string
}

// asyncError is a google.rpc.Status of the answer. Its details are not returned: they are free-form messages of
// the provider.
type asyncError struct {
	Code    int    `json:"code"`
	Message string `json:"message,omitempty"`
}

type asyncErrorsPage struct {
	CustomerID string       `json:"customer_id"`
	Errors     []asyncError `json:"errors"`
	NextCursor string       `json:"next_cursor,omitempty"`
}

var asyncErrorsOutputSchema = object(schema{"customer_id": stringSchema,
	"errors":      arrayOf(object(schema{"code": integerSchema, "message": stringSchema}, "code")),
	"next_cursor": stringSchema}, "customer_id", "errors")

var asyncErrorsFields = []capability.Field{
	{Name: "customer_id", Description: "The account of the call"},
	{Name: "errors", Description: "The errors of the last asynchronous operation of the object, as google.rpc.Status: " +
		"code (the gRPC status code as a number) and message, an untrusted provider text that is cut. The details " +
		"of a status are not returned. Empty when the last operation had none"},
	{Name: "next_cursor", Description: "Opaque cursor of the next page; absent on the last page"},
}

var asyncErrorsPageArguments = []capability.Argument{
	{Name: "page_size", Description: "Errors per page, 1 to 1000; Google may return fewer. Default: Google's"},
	{Name: "cursor", Description: "Opaque next_cursor of a previous page of the same call; the first page when " +
		"omitted. It only fits the connection that issued it"},
}

var asyncErrorsPageProperties = schema{
	"page_size": schema{"type": "integer", "minimum": 1, "maximum": maxPageSize},
	"cursor":    schema{"type": "string", "minLength": 1, "maxLength": maxAsyncCursor},
}

type asyncErrorsAnswer struct {
	Errors []struct {
		Code    *int   `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
	NextPageToken string `json:"nextPageToken"`
}

// listAsyncErrors reads one page. Page size and cursor are checked before the secret and any request.
func (e callEnv) listAsyncErrors(spec asyncErrorsSpec) (*asyncErrorsPage, error) {
	query := url.Values{}
	if spec.pageSize != nil {
		if *spec.pageSize < 1 || *spec.pageSize > maxPageSize {
			return nil, badRequest("page_size must be 1 to 1000")
		}
		query.Set("pageSize", strconv.Itoa(*spec.pageSize))
	}
	binding := provider.CursorBinding(spec.tool, spec.customerID, spec.resource, e.conn.login, e.conn.binding.key())
	if spec.cursor != "" {
		token, ok := provider.DecodeCursor(binding, spec.cursor, maxAsyncCursor)
		if !ok || token == "" || len(token) > maxAsyncPageToken || hasControl(token) {
			return nil, badRequest("cursor is not a next_cursor of this tool with these arguments on this " +
				"connection; start again without cursor")
		}
		query.Set("pageToken", token)
	}
	c, err := e.settle(spec.customerID, true)
	if err != nil {
		return nil, err
	}
	path := "/" + spec.resource + ":" + spec.method
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	var answer asyncErrorsAnswer
	if err := c.read(e.ctx, request{op: spec.op, tool: spec.tool, method: http.MethodGet, path: path,
		onAccount: true}, &answer); err != nil {
		return nil, err
	}
	if len(answer.Errors) > maxPageSize || len(answer.NextPageToken) > maxAsyncPageToken || hasControl(answer.NextPageToken) {
		return nil, provider.InvalidResponse(spec.op, subject+" returned a page larger than the bounds of this tool")
	}
	page := &asyncErrorsPage{CustomerID: spec.customerID, Errors: make([]asyncError, 0, len(answer.Errors))}
	for _, w := range answer.Errors {
		code := 0
		if w.Code != nil {
			code = *w.Code
		}
		page.Errors = append(page.Errors, asyncError{Code: code, Message: boundedText(w.Message, maxText)})
	}
	if answer.NextPageToken != "" {
		page.NextCursor = provider.EncodeCursor(binding, answer.NextPageToken)
	}
	return page, nil
}
