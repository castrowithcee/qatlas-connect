package aws

import (
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// maxIdentityPolicyChars is the length the IAM model allows for a policy document (policyDocumentType).
const maxIdentityPolicyChars = 131072

// checkIdentityPolicy admits an identity-based policy document only when it is well-formed and no statement
// carries Principal or NotPrincipal, which would make it a resource policy. It does not judge how wide the
// permissions reach. It runs without I/O, and a refusal names the rule and the statement by position, never a
// value of the document.
func checkIdentityPolicy(_ binding, name, value string) error {
	refuse := func(format string, args ...any) error {
		return &provider.InvalidRequestError{Message: name + ": " + fmt.Sprintf(format, args...)}
	}
	if !utf8.ValidString(value) {
		return refuse("the policy document is not valid UTF-8")
	}
	if utf8.RuneCountInString(value) > maxIdentityPolicyChars {
		return refuse("the policy document is longer than %d characters", maxIdentityPolicyChars)
	}
	doc, err := decodeStrictJSON(value)
	if errors.Is(err, errDuplicateKey) {
		return refuse("the policy document holds a key twice in one object")
	}
	if err != nil {
		return refuse("the policy document is not valid JSON")
	}
	top, ok := doc.(map[string]any)
	if !ok {
		return refuse("the policy document must be a JSON object")
	}
	if key, ok := unknownKey(top, trustTopKeys); !ok {
		return refuse("the policy document may hold only Version, Id, and Statement, spelled exactly so%s", caseNote(key, trustTopKeys))
	}
	if _, ok := top["Version"].(string); !ok {
		return refuse("Version is required and must be a string")
	}
	var statements []any
	switch s := top["Statement"].(type) {
	case map[string]any:
		statements = []any{s}
	case []any:
		statements = s
	}
	if len(statements) == 0 {
		return refuse("Statement must be an object or a non-empty list of objects")
	}
	for i, raw := range statements {
		at := i + 1
		statement, ok := raw.(map[string]any)
		if !ok {
			return refuse("statement %d is not an object", at)
		}
		if key, ok := unknownKey(statement, trustStatementKeys); !ok {
			return refuse("statement %d holds an element the policy grammar does not define%s", at, caseNote(key, trustStatementKeys))
		}
		for _, element := range []string{"Principal", "NotPrincipal"} {
			if _, set := statement[element]; set {
				return refuse("statement %d uses %s, which an identity policy never holds", at, element)
			}
		}
	}
	return nil
}
