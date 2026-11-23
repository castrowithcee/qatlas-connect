package googleads

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
)

// Some Mutate services take operations[]. A tool of such a family takes a list of entries with its own fixed
// bound and sends them as one atomic request without partial_failure: Google applies all entries or none.

// maxListOperations is Qatlas's own bound of the entries of one call; Google documents none for these services.
const maxListOperations = 10

// mutateListNote ends the description of every changing tool that takes a list of entries.
const mutateListNote = " The entries are sent as one atomic request that Qatlas Connect sends once and never " +
	"repeats: Google applies all of them or none. When a timeout, a cut connection, or a server error leaves open " +
	"whether Google took it, the error says that the outcome is unknown, and the objects are to be read before " +
	"they are changed again. Costs one operation of the daily quota per entry, plus one for a membership read " +
	"below a manager target and one for the check that the account is no manager."

// mutateListListedNote is mutateListNote for a tool that only a connection whose tools list names it offers.
const mutateListListedNote = mutateListNote + " Offered only by a connection whose tools list names it."

// numericIDPattern is a positive int64 in decimal form, as the numeric ID of a resource name.
var numericIDPattern = regexp.MustCompile(`^[1-9][0-9]{0,18}$`)

// listMutation is one request of a Mutate service with operations[].
type listMutation struct {
	op, tool, customerID string
	service, collection  string
	// resourceKey is the member of a result that holds the object after the change.
	resourceKey string
	operations  []any
	// names are the resource names an update names, one per operation in order; nil for a creation.
	names []string
	// idOf reads the ID of a resource name of the collection; the default is a single positive number, a
	// family with a composite key such as 12~34 brings its own.
	idOf         func(name string) (string, bool)
	validateOnly bool
	content      string
}

// listResult is one result of the answer: its resource name, bound to the account, and the object when Google
// returned it.
type listResult struct {
	ResourceName string
	ID           string
	Resource     json.RawMessage
}

// numericIDOf returns the numeric ID of customers/ID/<collection>/N of exactly the checked account.
func numericIDOf(name, customerID, collection string) (string, bool) {
	rest, ok := strings.CutPrefix(name, customerName(customerID)+"/"+collection+"/")
	return rest, ok && numericIDPattern.MatchString(rest)
}

// mutateList sends the request once and checks the answer. Anything that is not exactly what was asked for leaves
// the outcome open, because the request was sent.
func (c *client) mutateList(ctx context.Context, m listMutation) ([]listResult, error) {
	body := mutateOperationsBody(m.operations, m.validateOnly, m.content)
	var answer struct {
		Results             []map[string]json.RawMessage `json:"results"`
		PartialFailureError json.RawMessage              `json:"partialFailureError"`
	}
	if err := c.mutate(ctx, mutation{op: m.op, tool: m.tool, customerID: m.customerID, service: m.service,
		body: body}, &answer); err != nil {
		return nil, err
	}
	if len(answer.PartialFailureError) > 0 && string(answer.PartialFailureError) != "null" {
		return nil, unsureResult(m.op, "answered with a partial failure, which this request did not ask for")
	}
	if len(answer.Results) == 0 && m.validateOnly {
		return []listResult{}, nil
	}
	if len(answer.Results) != len(m.operations) {
		return nil, unsureResult(m.op, "answered with another number of results than the entries asked for")
	}
	out := make([]listResult, 0, len(answer.Results))
	for i, result := range answer.Results {
		var name string
		if json.Unmarshal(result["resourceName"], &name) != nil {
			return nil, unsureResult(m.op, "answered with a result without a resource name")
		}
		var id string
		var ok bool
		if m.idOf != nil {
			id, ok = m.idOf(name)
		} else {
			id, ok = numericIDOf(name, m.customerID, m.collection)
		}
		if !ok {
			return nil, unsureResult(m.op, "answered with a result for another account than the one asked for")
		}
		if m.names != nil && name != m.names[i] {
			return nil, unsureResult(m.op, "answered with a result for another object than the one asked for")
		}
		r := listResult{ResourceName: name, ID: id}
		if raw := result[m.resourceKey]; len(raw) > 0 && string(raw) != "null" {
			r.Resource = raw
		}
		out = append(out, r)
	}
	return out, nil
}
