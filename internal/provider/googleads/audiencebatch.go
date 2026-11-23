package googleads

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode"
)

// The tools of custom interests and audiences take a list of operations and send it as one request. The bound
// is Qatlas' own: Google documents none for these two services. It keeps the confirmation readable, since it
// names every object, and the reach of one call small.
const (
	audienceMaxOperations          = 10
	audienceMaxDescriptionArgument = 4096
)

func audienceEnumSchema(values []string) schema { return schema{"type": "string", "enum": values} }

// audienceOperationListSchema is the schema of the list of operations a tool takes.
func audienceOperationListSchema(item schema) schema {
	return schema{"type": "array", "minItems": 1, "maxItems": audienceMaxOperations, "items": item}
}

func audienceCheckCount(argument string, n int) error {
	if n < 1 || n > audienceMaxOperations {
		return badRequest(fmt.Sprintf("%s must hold 1 to %d entries", argument, audienceMaxOperations))
	}
	return nil
}

// audienceText reports whether value is text of 1 to limit bytes that is not blank and holds no control character.
func audienceText(value string, limit int) bool {
	return strings.TrimSpace(value) != "" && len(value) <= limit && !strings.ContainsFunc(value, unicode.IsControl)
}

// audienceCheckDescription accepts any text up to the bound; an empty one clears the field on update.
func audienceCheckDescription(argument, value string) error {
	if len(value) > audienceMaxDescriptionArgument || strings.ContainsFunc(value, unicode.IsControl) {
		return badRequest(fmt.Sprintf("%s must be text of up to %d bytes without control characters",
			argument, audienceMaxDescriptionArgument))
	}
	return nil
}

// audienceObjectName refuses an object ID that is not a plain number and returns the resource name of the object in the
// account: the name is built from the checked customer ID and the checked ID, never taken from the caller.
func audienceObjectName(argument, customerID, collection, id string) (string, error) {
	if !numberPattern.MatchString(id) {
		return "", badRequest(argument + " must be the numeric ID of the object")
	}
	return customerName(customerID) + "/" + collection + "/" + id, nil
}

// audienceMutateResult is one result of a Mutate service; Audience is only present after MUTABLE_RESOURCE.
type audienceMutateResult struct {
	ResourceName string          `json:"resourceName"`
	Audience     json.RawMessage `json:"audience"`
}

// audienceMutateList checks the call and sends its operations as one atomic request. It returns the results bound to
// the operations, or nil when a validated request has none.
func (e callEnv) audienceMutateList(customerID, service, collection string, operations []any, want []string,
	validateOnly bool, content string) ([]audienceMutateResult, error) {
	c, err := e.settle(customerID, true)
	if err != nil {
		return nil, err
	}
	var answer struct {
		Results []audienceMutateResult `json:"results"`
	}
	if err := c.mutate(e.ctx, mutation{op: e.op, tool: e.tool, customerID: customerID, service: service,
		body: mutateOperationsBody(operations, validateOnly, content)}, &answer); err != nil {
		return nil, err
	}
	return audienceBindResults(e.op, answer.Results, want, customerID, collection, len(operations), validateOnly)
}

// audienceBindResults ties the answer to the request. Google applies all operations or none, so a successful answer
// holds one result per operation, in order. An update names the object it changed; a create names a new object
// of the account, never the same twice. Anything else may mean the change was made on another object, so it is
// an unknown outcome.
func audienceBindResults(op string, results []audienceMutateResult, want []string, customerID, collection string, count int,
	validateOnly bool) ([]audienceMutateResult, error) {
	if validateOnly && !slices.ContainsFunc(results, func(r audienceMutateResult) bool { return r.ResourceName != "" }) {
		return nil, nil
	}
	if len(results) != count {
		return nil, unsureResult(op, "answered with another number of results than operations sent")
	}
	seen := map[string]bool{}
	for i, r := range results {
		switch {
		case want != nil && r.ResourceName != want[i]:
			return nil, unsureResult(op, "answered with a result for another object than the one asked for")
		case want == nil && (!ownedName(r.ResourceName, customerID, collection) || seen[r.ResourceName]):
			return nil, unsureResult(op, "answered with a result that is not a new object of the account asked for")
		}
		seen[r.ResourceName] = true
	}
	return results, nil
}

// audienceClipped is a name from the arguments for a confirmation text: cut and free of control characters.
func audienceClipped(value string) string { return `"` + boundedText(value, 80) + `"` }

func audiencePlural(n int, one string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", one)
	}
	return fmt.Sprintf("%d %ss", n, one)
}

// audienceChangedNames are the fields of an update by argument name, for the answer and the confirmation.
type audienceChangedNames struct {
	mask  []string
	names []string
}

func (c *audienceChangedNames) add(path, name string) {
	c.mask, c.names = append(c.mask, path), append(c.names, name)
}

// audienceNumberIDSchema is the schema of the numeric ID of an object of the account.
var audienceNumberIDSchema = schema{"type": "string", "pattern": numberPattern.String()}
