package aws

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/provider/aws/awsquery"
)

// maxResourcePolicyChars is the cap on a resource policy document. The service models give such a member no
// length bound, so the default cap of every string argument is the limit.
const maxResourcePolicyChars = awsquery.DefaultMaxString

const (
	condSourceAccount = "aws:SourceAccount"
	condSourceArn     = "aws:SourceArn"
)

// checkResourcePolicyOwnAccount admits a resource policy document only when every statement, whatever its
// effect, names principals of the bound account, or AWS service principals that a condition ties to the bound
// account. It runs without I/O, and a refusal names the rule and the statement by position, never a value of
// the document. name is the argument that holds the document.
func checkResourcePolicyOwnAccount(b binding, name, value string) error {
	return checkOwnAccountDocument(b, name, value, false)
}

// checkOwnAccountDocument is the check of checkResourcePolicyOwnAccount, with a parameter for whether a source
// condition may use the operator StringLike. That is no wildcard match here: the values must still be the
// bound account or an ARN of it, spelled out, and a wildcard in them is refused as any other value.
func checkOwnAccountDocument(b binding, name, value string, stringLike bool) error {
	refuse := func(format string, args ...any) error {
		return &provider.InvalidRequestError{Message: name + ": " + fmt.Sprintf(format, args...)}
	}
	if !utf8.ValidString(value) {
		return refuse("the policy document is not valid UTF-8")
	}
	if utf8.RuneCountInString(value) > maxResourcePolicyChars {
		return refuse("the policy document is longer than %d characters", maxResourcePolicyChars)
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
		if _, set := statement["NotPrincipal"]; set {
			return refuse("statement %d uses NotPrincipal, which is never allowed", at)
		}
		principal, set := statement["Principal"]
		if !set {
			return refuse("statement %d has no Principal", at)
		}
		hasService, msg := checkOwnAccountPrincipal(b.account, principal)
		if msg != "" {
			return refuse("statement %d: %s", at, msg)
		}
		tied, msg := checkSourceConditions(b.account, statement["Condition"], stringLike)
		if msg != "" {
			return refuse("statement %d: %s", at, msg)
		}
		if hasService && !tied {
			return refuse("statement %d: a service principal needs a Condition of aws:SourceAccount with the bound account "+
				"or of aws:SourceArn with ARNs of the bound account", at)
		}
	}
	return nil
}

// checkOwnAccountPrincipal returns whether the Principal names a service, and the rule it breaks, or "".
func checkOwnAccountPrincipal(account string, principal any) (bool, string) {
	p, ok := principal.(map[string]any)
	if !ok {
		return false, "Principal must be an object with AWS or Service; a wildcard or any other string is never allowed"
	}
	if len(p) == 0 {
		return false, "Principal names no principal"
	}
	hasService := false
	// Sorted, so a document that breaks several rules is always refused with the same one.
	for _, key := range slices.Sorted(maps.Keys(p)) {
		if key != "AWS" && key != "Service" {
			if strings.EqualFold(key, "AWS") || strings.EqualFold(key, "Service") {
				return false, "a Principal key is spelled in another case than AWS or Service"
			}
			return false, "Principal may hold only AWS and Service; Federated, CanonicalUser, and every other key are never allowed"
		}
		values, ok := principalValues(p[key])
		if !ok {
			return false, "Principal." + key + " must be a string or a non-empty list of strings"
		}
		for _, v := range values {
			if key == "AWS" && (strings.ContainsAny(v, "*?") || !ownAccountPrincipal(account, v)) {
				return false, "Principal.AWS allows only the bound account as its ID or as an ARN of its root, a user, or a role; " +
					"other accounts, partitions, unique IDs, STS ARNs, and wildcards are never allowed"
			}
			if key == "Service" && (strings.ContainsAny(v, "*?") || !trustService.MatchString(v)) {
				return false, "Principal.Service allows only service principals of the form name.amazonaws.com in lower case letters, digits, hyphens, and dots"
			}
		}
		hasService = hasService || key == "Service"
	}
	return hasService, ""
}

// checkSourceConditions judges the aws:SourceAccount and aws:SourceArn conditions of a statement. It returns
// whether at least one of them is present, and the rule one breaks, or "". Other condition keys only narrow a
// statement and are not judged.
func checkSourceConditions(account string, condition any, stringLike bool) (bool, string) {
	if condition == nil {
		return false, ""
	}
	operators, ok := condition.(map[string]any)
	if !ok {
		return false, "Condition must be an object"
	}
	tied := false
	for _, operator := range slices.Sorted(maps.Keys(operators)) {
		keys, ok := operators[operator].(map[string]any)
		if !ok {
			return false, "a Condition operator must hold an object"
		}
		seen := map[string]bool{}
		for _, key := range slices.Sorted(maps.Keys(keys)) {
			var kind string
			switch {
			case strings.EqualFold(key, condSourceAccount):
				kind = condSourceAccount
			case strings.EqualFold(key, condSourceArn):
				kind = condSourceArn
			default:
				continue
			}
			// AWS compares condition keys without case, so two spellings would be one key it reads differently.
			if seen[kind] {
				return false, kind + " is set twice in one Condition operator, in different spellings"
			}
			seen[kind] = true
			values, ok := principalValues(keys[key])
			if !ok {
				return false, kind + " must be a string or a non-empty list of strings"
			}
			switch kind {
			case condSourceAccount:
				if operator != "StringEquals" && !(stringLike && operator == "StringLike") {
					return false, kind + " is allowed only with the operator StringEquals" + likeNote(stringLike, "")
				}
				for _, v := range values {
					if v != account {
						return false, kind + " allows only the bound account"
					}
				}
			case condSourceArn:
				if operator != "ArnEquals" && operator != "ArnLike" && operator != "StringEquals" && !(stringLike && operator == "StringLike") {
					return false, kind + " is allowed only with the operators ArnEquals, ArnLike, and StringEquals" + likeNote(stringLike, ",")
				}
				for _, v := range values {
					// The account field is the fifth of arn:partition:service:region:account:resource.
					parts := strings.SplitN(v, ":", 6)
					if len(parts) != 6 || parts[0] != "arn" || parts[1] != "aws" || parts[4] != account {
						return false, kind + " allows only ARNs of the bound account, with the account field spelled out"
					}
				}
			}
			tied = true
		}
	}
	return tied, ""
}

// likeNote names StringLike in the refusal of a check that allows it.
func likeNote(stringLike bool, sep string) string {
	if !stringLike {
		return ""
	}
	if sep == "," {
		return ", and StringLike"
	}
	return " or StringLike"
}
