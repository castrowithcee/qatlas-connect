package aws

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// maxTrustPolicyChars is the default IAM quota of a role trust policy. The model allows 131072 characters for
// every policy document; the quota is the narrower bound, and a document above it is refused before a call.
const maxTrustPolicyChars = 2048

var (
	// trustAWSArn is a principal of the bound account: its root, an IAM user, or an IAM role, with IAM name
	// and path characters only. The account is compared separately.
	trustAWSArn = regexp.MustCompile(`^arn:aws:iam::([0-9]{12}):(root|(user|role)/[\w+=,.@/-]+)$`)
	// trustService is an AWS service principal.
	trustService = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]*\.amazonaws\.com$`)
)

// The elements the IAM policy grammar defines, at the top and in a statement. Anything else, including a
// spelling in another case, is refused: a key AWS might read differently than this check is never sent.
var (
	trustTopKeys       = []string{"Version", "Id", "Statement"}
	trustStatementKeys = []string{"Sid", "Effect", "Principal", "NotPrincipal", "Action", "NotAction", "Resource",
		"NotResource", "Condition"}
)

// errDuplicateKey marks a JSON object that holds one key twice.
var errDuplicateKey = errors.New("duplicate key")

// checkTrustPolicy admits a role trust policy only when every statement, whatever its effect, names principals
// of the bound account or AWS service principals. It runs without I/O, and a refusal names the rule and the
// statement by position, never a value of the document.
func checkTrustPolicy(b binding, name, value string) error {
	refuse := func(format string, args ...any) error {
		return &provider.InvalidRequestError{Message: name + ": " + fmt.Sprintf(format, args...)}
	}
	if !utf8.ValidString(value) {
		return refuse("the trust policy is not valid UTF-8")
	}
	if utf8.RuneCountInString(value) > maxTrustPolicyChars {
		return refuse("the trust policy is longer than %d characters", maxTrustPolicyChars)
	}
	doc, err := decodeStrictJSON(value)
	if errors.Is(err, errDuplicateKey) {
		return refuse("the trust policy holds a key twice in one object")
	}
	if err != nil {
		return refuse("the trust policy is not valid JSON")
	}
	top, ok := doc.(map[string]any)
	if !ok {
		return refuse("the trust policy must be a JSON object")
	}
	if key, ok := unknownKey(top, trustTopKeys); !ok {
		return refuse("the trust policy may hold only Version, Id, and Statement, spelled exactly so%s", caseNote(key, trustTopKeys))
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
		if err := checkTrustPrincipal(b.account, principal); err != "" {
			return refuse("statement %d: %s", at, err)
		}
	}
	return nil
}

// checkTrustPrincipal returns the rule a Principal element breaks, or "".
func checkTrustPrincipal(account string, principal any) string {
	p, ok := principal.(map[string]any)
	if !ok {
		return "Principal must be an object with AWS or Service; a wildcard or any other string is never allowed"
	}
	if len(p) == 0 {
		return "Principal names no principal"
	}
	// Sorted, so a document that breaks several rules is always refused with the same one.
	for _, key := range slices.Sorted(maps.Keys(p)) {
		value := p[key]
		var valid func(string) bool
		switch key {
		case "AWS":
			valid = func(v string) bool { return ownAccountPrincipal(account, v) }
		case "Service":
			valid = trustService.MatchString
		default:
			if strings.EqualFold(key, "AWS") || strings.EqualFold(key, "Service") {
				return "a Principal key is spelled in another case than AWS or Service"
			}
			return "Principal may hold only AWS and Service; Federated, CanonicalUser, and every other key are never allowed"
		}
		values, ok := principalValues(value)
		if !ok {
			return "Principal." + key + " must be a string or a non-empty list of strings"
		}
		for _, v := range values {
			if strings.ContainsAny(v, "*?") || !valid(v) {
				if key == "AWS" {
					return "Principal.AWS allows only the bound account as its ID or as an ARN of its root, a user, or a role; " +
						"other accounts, partitions, unique IDs, STS ARNs, and wildcards are never allowed"
				}
				return "Principal.Service allows only service principals of the form name.amazonaws.com in lower case letters, digits, hyphens, and dots"
			}
		}
	}
	return ""
}

// ownAccountPrincipal reports whether v is the account ID or an ARN of its root, a user, or a role.
func ownAccountPrincipal(account, v string) bool {
	if v == account {
		return true
	}
	m := trustAWSArn.FindStringSubmatch(v)
	return m != nil && m[1] == account
}

// principalValues reads a principal value: one string or a non-empty list of strings.
func principalValues(value any) ([]string, bool) {
	switch v := value.(type) {
	case string:
		return []string{v}, true
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			text, ok := item.(string)
			if !ok {
				return nil, false
			}
			out = append(out, text)
		}
		return out, len(out) > 0
	}
	return nil, false
}

// unknownKey returns the first key of object, in sorted order, that is not in allowed, and false; or "", true.
func unknownKey(object map[string]any, allowed []string) (string, bool) {
	for _, key := range slices.Sorted(maps.Keys(object)) {
		if !slices.Contains(allowed, key) {
			return key, false
		}
	}
	return "", true
}

// caseNote says that a refused key is a known element in another case, without quoting the key.
func caseNote(key string, known []string) string {
	for _, k := range known {
		if strings.EqualFold(k, key) {
			return "; an element is spelled in another case than " + k
		}
	}
	return ""
}

// decodeStrictJSON decodes one JSON value generically, numbers kept as text, and refuses a key that occurs
// twice in one object and anything after the value. encoding/json would keep the last duplicate silently,
// and a struct would fold the case of names; both could let this check see another document than AWS.
func decodeStrictJSON(text string) (any, error) {
	d := json.NewDecoder(bytes.NewReader([]byte(text)))
	d.UseNumber()
	v, err := decodeStrictValue(d)
	if err != nil {
		return nil, err
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, errors.New("data after the value")
	}
	return v, nil
}

func decodeStrictValue(d *json.Decoder) (any, error) {
	tok, err := d.Token()
	if err != nil {
		return nil, err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return tok, nil
	}
	switch delim {
	case '{':
		object := map[string]any{}
		for d.More() {
			kt, err := d.Token()
			if err != nil {
				return nil, err
			}
			key, ok := kt.(string)
			if !ok {
				return nil, errors.New("object key is not a string")
			}
			if _, dup := object[key]; dup {
				return nil, errDuplicateKey
			}
			if object[key], err = decodeStrictValue(d); err != nil {
				return nil, err
			}
		}
		if _, err := d.Token(); err != nil {
			return nil, err
		}
		return object, nil
	case '[':
		list := []any{}
		for d.More() {
			v, err := decodeStrictValue(d)
			if err != nil {
				return nil, err
			}
			list = append(list, v)
		}
		if _, err := d.Token(); err != nil {
			return nil, err
		}
		return list, nil
	}
	return nil, errors.New("unexpected delimiter")
}
