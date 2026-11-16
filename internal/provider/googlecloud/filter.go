package googlecloud

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The search methods take their filter as one query string. A tool never takes that string: it takes the
// documented fields as arguments, checks each value, and builds the query here, so a value can never add a
// field, an operator, or a clause of its own.

const maxFilterValue = 64

var (
	parentPattern     = regexp.MustCompile(`^(organizations|folders)/[0-9]{1,20}$`)
	projectIDFilter   = regexp.MustCompile(`^[a-z][a-z0-9-]{0,29}\*?$`)
	domainPattern     = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]{0,251}[A-Za-z0-9])?$`)
	labelKeyPattern   = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,62}$`)
	labelValuePattern = regexp.MustCompile(`^[a-z0-9_-]{1,63}$`)
)

// filterValue returns value as the right-hand side of a clause. A value of letters, digits, '-', '_' and '.'
// stands bare. Anything else, whitespace and the words AND, OR and NOT included, is quoted, which Google asks
// for values with whitespace. A quote or a backslash inside a value is refused rather than escaped, because
// Google documents no escape rule for them. The suffix wildcard '*' is Google's only one: it is allowed after
// a bare value, since Google documents no wildcard inside quotes.
func filterValue(argument, value string, wildcard bool) (string, error) {
	body, star := value, false
	if wildcard {
		body, star = strings.CutSuffix(value, "*")
	}
	switch {
	case body == "" || len(value) > maxFilterValue || !utf8.ValidString(value):
		return "", badRequest(argument + " must hold 1 to " + strconv.Itoa(maxFilterValue) + " characters of valid text")
	case strings.ContainsFunc(value, func(r rune) bool { return unicode.IsControl(r) || r == '"' || r == '\\' }):
		return "", badRequest(argument + " must not hold control characters, quotes, or backslashes")
	case strings.Contains(body, "*"):
		return "", badRequest(argument + " takes the wildcard * only as its last character")
	}
	bare := !strings.ContainsFunc(body, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune("-_.", r)
	})
	switch strings.ToUpper(body) {
	case "AND", "OR", "NOT":
		bare = false
	}
	switch {
	case bare:
		return value, nil
	case star:
		return "", badRequest(argument + " takes the wildcard * only after letters, digits, '-', '_' or '.'")
	}
	return `"` + body + `"`, nil
}

// filterBuilder collects the clauses of one query.
type filterBuilder struct {
	clauses []string
	err     error
}

func (b *filterBuilder) add(field, operator, argument, value string, wildcard bool) {
	if b.err != nil || value == "" {
		return
	}
	quoted, err := filterValue(argument, value, wildcard)
	if err != nil {
		b.err = err
		return
	}
	b.clauses = append(b.clauses, field+operator+quoted)
}

// raw adds a clause whose value the caller checked against a fixed pattern or list.
func (b *filterBuilder) raw(field, operator, argument, value string, valid func(string) bool, rule string) {
	if b.err != nil || value == "" {
		return
	}
	if !valid(value) {
		b.err = badRequest(argument + " must be " + rule)
		return
	}
	b.clauses = append(b.clauses, field+operator+value)
}

func (b *filterBuilder) labels(labels map[string]string, operator string) {
	if b.err != nil || len(labels) == 0 {
		return
	}
	if len(labels) > 10 {
		b.err = badRequest("labels takes at most 10 entries")
		return
	}
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := labels[key]
		if !labelKeyPattern.MatchString(key) || (value != "*" && !labelValuePattern.MatchString(value)) {
			b.err = badRequest("labels takes keys of lowercase letters, digits, '-' and '_' and values of the same " +
				"characters, or * for any value")
			return
		}
		b.clauses = append(b.clauses, "labels."+key+operator+value)
	}
}

func isState(value string) bool { return value == "ACTIVE" || value == "DELETE_REQUESTED" }

func isParent(value string) bool { return parentPattern.MatchString(value) }
