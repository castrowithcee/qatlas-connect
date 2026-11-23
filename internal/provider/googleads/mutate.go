package googleads

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// A mutation goes through the Mutate service of one resource family, as one atomic request without
// partial_failure: Google applies all of it or none. It is sent once and never repeated; when it may have
// reached Google and the answer is missing or unreadable, the error says that the outcome is unknown. The
// services of this package take one operation per request; a family that takes operations[] builds that body
// itself and keeps its own fixed bound.

// mutation is one request of a Mutate service: the service path below the customer ("" for the customer
// service itself), the action ("mutate" when left empty, "resolve" for the review service) and the JSON body
// the tool built from checked arguments.
type mutation struct {
	op, tool, customerID, service, action string
	body                                  map[string]any
}

// mutateBody is the body of a request with one operation. validate_only validates without writing; content
// is the response content type, empty for the default of Google, the resource name only.
func mutateBody(operation any, validateOnly bool, content string) map[string]any {
	body := map[string]any{"operation": operation}
	if validateOnly {
		body["validateOnly"] = true
	}
	if content != "" {
		body["responseContentType"] = content
	}
	return body
}

// mutateOperationsBody is the body of a request with several operations, for a family whose service takes
// operations[]. partial_failure is never set, so Google applies all operations or none.
func mutateOperationsBody(operations []any, validateOnly bool, content string) map[string]any {
	body := map[string]any{"operations": operations}
	if validateOnly {
		body["validateOnly"] = true
	}
	if content != "" {
		body["responseContentType"] = content
	}
	return body
}

// mutate sends the request once. The answer is decoded into out; with validate_only Google answers without a
// result, which out then does not receive.
func (c *client) mutate(ctx context.Context, m mutation, out any) error {
	body, err := json.Marshal(m.body)
	if err != nil {
		return failure(m.op, "the request body could not be encoded")
	}
	action := m.action
	if action == "" {
		action = "mutate"
	}
	return c.send(ctx, request{op: m.op, tool: m.tool, method: http.MethodPost,
		path: "/" + customerName(m.customerID) + m.service + ":" + action, body: body, onAccount: true, change: true}, out)
}

var responseContentTypes = []string{"RESOURCE_NAME_ONLY", "MUTABLE_RESOURCE"}

// checkContentType refuses a response content type Google does not define for this request.
func checkContentType(value string) error {
	if value != "" && !slices.Contains(responseContentTypes, value) {
		return badRequest("response_content_type must be RESOURCE_NAME_ONLY or MUTABLE_RESOURCE")
	}
	return nil
}

var (
	validateOnlySchema        = booleanSchema
	responseContentTypeSchema = schema{"type": "string", "enum": []string{"RESOURCE_NAME_ONLY", "MUTABLE_RESOURCE"}}

	validateOnlyArgument = capability.Argument{Name: "validate_only", Description: "true validates the change " +
		"with Google without making it: Google answers errors only and no result; false or left out makes the change"}
	responseContentTypeArgument = capability.Argument{Name: "response_content_type", Description: "MUTABLE_RESOURCE " +
		"adds the mutable fields of the object after the change to the answer; RESOURCE_NAME_ONLY or left out answers " +
		"the resource name only"}
)

// changeRisk is the risk of a tool that changes an account. The change is confirmed per call; whether only the
// tools list offers the tool is up to its descriptor.
func changeRisk(effect capability.Effect, idempotency capability.Idempotency, sensitivity string) capability.Risk {
	return capability.Risk{Effect: effect, Idempotency: idempotency, Confirmation: capability.ConfirmationRequired,
		OpenWorld: true, DataSensitivity: sensitivity}
}

// changeNote ends the description of a changing tool that sends exactly one operation; mutateNote adds that only
// the tools list offers it. A tool that takes a list of entries ends on mutateListNote or mutateListListedNote.
const (
	changeNote = " The change is one atomic request that Qatlas Connect sends once and never repeats; when a " +
		"timeout, a cut connection, or a server error leaves open whether Google took it, the error says that the " +
		"outcome is unknown, and the object is to be read before it is changed again. Costs one operation of the " +
		"daily quota, plus one for a membership read below a manager target and one for the check that the account " +
		"is no manager account."
	mutateNote = changeNote + " Offered only by a connection whose tools list names it."
)

// callEnv is what a handler of a changing tool holds after the connection and the arguments are checked.
type callEnv struct {
	ctx      context.Context
	resolved *config.Resolved
	secrets  *secret.Resolver
	red      *redact.Redactor
	conn     connection
	op, tool string
}

func (e callEnv) open() (*client, error) { return open(e.ctx, e.resolved, e.conn, e.secrets, e.red) }

// settle checks everything of a changing call that needs no request, then opens the client and, for an account
// that only a manager target reaches, proves the membership. When the connection has targets and none names the
// account as a manager, one read shows whether it is a manager account, which then needs a manager target of
// its own: a customer target never reaches the settings of a manager. A connection without targets reaches what
// its credential reaches and needs no such read.
func (e callEnv) settle(customerID string, managerNeedsTarget bool) (*client, error) {
	if !customerIDPattern.MatchString(customerID) {
		return nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	r, err := reachOf(e.conn, customerID)
	if err != nil {
		return nil, err
	}
	c, err := e.open()
	if err != nil {
		return nil, err
	}
	if r == reachNone {
		if _, err := c.prove(e.ctx, e.op, e.tool, e.conn, customerID); err != nil {
			return nil, err
		}
	}
	if managerNeedsTarget && e.conn.binding.bound() && !slices.Contains(e.conn.binding.managers, customerID) {
		manager, err := c.isManager(e.ctx, e.op, e.tool, customerID)
		if err != nil {
			return nil, err
		}
		if manager {
			return nil, badRequest("customer_id is a manager account, which a change reaches only through a manager " +
				"target that names it")
		}
	}
	return c, nil
}

// managerRow is the row of the check whether an account is a manager.
type managerRow struct {
	Customer struct {
		ID      *wireInt `json:"id"`
		Manager bool     `json:"manager"`
	} `json:"customer"`
}

// isManager reads whether the account is a manager account. Anything but exactly the one row of the account
// is an invalid answer, never a "no".
func (c *client) isManager(ctx context.Context, op, tool, customerID string) (bool, error) {
	rows, err := c.search(ctx, op, tool, customerID, "SELECT customer.id, customer.manager FROM customer LIMIT 2")
	if err != nil {
		return false, err
	}
	var row managerRow
	if len(rows) == 1 && json.Unmarshal(rows[0], &row) == nil && row.Customer.ID != nil {
		if id, ok := canonicalID(*row.Customer.ID); ok && id == customerID {
			return row.Customer.Manager, nil
		}
	}
	return false, provider.InvalidResponse(op, subject+" did not return exactly the one account asked for")
}
