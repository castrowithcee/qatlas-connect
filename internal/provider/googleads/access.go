package googleads

import (
	"context"
	"encoding/json"
	"regexp"
	"slices"
	"strconv"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// Shared by the tools of the invitations and of the multi-party reviews of an account. Both belong to the
// control of who may act in an account, so every one of them, reading ones included, needs a manager target
// that names a manager account (callEnv.settle with managerNeedsTarget).

// idPattern is the form of an int64 ID that an argument or a cursor holds: positive, without leading zeros.
const idPatternText = `^[1-9][0-9]{0,18}$`

var idPattern = regexp.MustCompile(idPatternText)

// checkID refuses an ID argument that is not a positive int64.
func checkID(argument, value string) error {
	if n, err := strconv.ParseInt(value, 10, 64); !idPattern.MatchString(value) || err != nil || n < 1 {
		return badRequest(argument + " must be a positive whole number without leading zeros")
	}
	return nil
}

// idOf reads an int64 ID of an answer: it must be positive.
func idOf(value *wireInt) (string, bool) {
	if value == nil {
		return "", false
	}
	n, err := strconv.ParseInt(string(*value), 10, 64)
	if err != nil || n < 1 {
		return "", false
	}
	return strconv.FormatInt(n, 10), true
}

// emailFilterPattern is the form of an address that filters a list: no space, quote, backslash or control
// character, so it can stand in a GAQL string; checkGAQLText refuses the same characters.
const emailFilterPatternText = `^[^\x00-\x20\x7f'"\\]{3,254}$`

var emailFilterPattern = regexp.MustCompile(emailFilterPatternText)

func checkEmailFilter(argument, value string) error {
	if value == "" {
		return nil
	}
	if err := checkGAQLText(argument, value); err != nil {
		return err
	}
	if !emailFilterPattern.MatchString(value) {
		return badRequest(argument + " must be 3 to 254 characters without spaces, quotes or backslashes")
	}
	return nil
}

// enumList builds a GAQL condition "field IN (A, B)" from names of a closed enum. The names are checked
// against the list, so they hold nothing else than capital letters and underscores.
func enumList(argument, field string, values, allowed []string) (string, error) {
	if len(values) == 0 {
		return "", nil
	}
	if len(values) > len(allowed) {
		return "", badRequest(argument + " names a value more than once")
	}
	var seen []string
	list := ""
	for _, v := range values {
		if !slices.Contains(allowed, v) || slices.Contains(seen, v) {
			return "", badRequest(argument + " may name only " + joinOr(allowed) + ", each once")
		}
		seen = append(seen, v)
		if list != "" {
			list += ", "
		}
		list += v
	}
	return field + " IN (" + list + ")", nil
}

func joinOr(values []string) string {
	out := ""
	for i, v := range values {
		switch {
		case i == 0:
		case i == len(values)-1:
			out += " and "
		default:
			out += ", "
		}
		out += v
	}
	return out
}

// idList builds "field IN (1, 2)" from ID arguments.
func idList(argument, field string, values []string) (string, error) {
	if len(values) == 0 {
		return "", nil
	}
	if len(values) > maxClientIDs {
		return "", badRequest(argument + " names at most 100 IDs")
	}
	list := ""
	for i, v := range values {
		if err := checkID(argument, v); err != nil {
			return "", err
		}
		if i > 0 {
			list += ", "
		}
		list += v
	}
	return field + " IN (" + list + ")", nil
}

// accessReadRisk is the risk of the read tools of this group: they reach the e-mail addresses of other people.
var accessReadRisk = capability.Risk{
	Effect: capability.EffectRead, Idempotency: capability.IdempotencySafe, Confirmation: capability.ConfirmationNone,
	OpenWorld: true,
	DataSensitivity: "personal data of third parties: e-mail addresses of invited and requesting people, their " +
		"access roles and user IDs, and the justification text of a request; no passkey setting or other security " +
		"data of a person",
}

// listSpec is what a list of this group needs besides the account: the filters as GAQL conditions, the page,
// and the query for a page after a key.
type listSpec struct {
	customerID string
	tool, op   string
	conditions []string
	pageSize   *int
	cursor     string
	query      func(conditions []string, after string, limit int) string
}

// listAccess runs one page of a list sorted by an ID that is its keyset: it checks page and cursor, settles the
// account, reads one row more than the page to tell whether more follow, and maps each row. mapRow returns
// the item and the ID of its row; an answer out of the requested order is refused.
func listAccess[T any](e callEnv, spec listSpec, mapRow func(json.RawMessage) (T, string, error)) (items []T,
	next string, hasMore bool, err error) {
	size := defaultPageSize
	if spec.pageSize != nil {
		if *spec.pageSize < 1 || *spec.pageSize > maxPageSize {
			return nil, "", false, badRequest("page_size must be 1 to 1000")
		}
		size = *spec.pageSize
	}
	binding := provider.CursorBinding(spec.tool, spec.customerID, spec.conditions, e.conn.login, e.conn.binding.key())
	after := ""
	if spec.cursor != "" {
		inner, ok := provider.DecodeCursor(binding, spec.cursor, maxCursorBytes)
		if !ok || !idPattern.MatchString(inner) {
			return nil, "", false, badRequest("cursor is not a next_cursor of this tool with these arguments on this " +
				"connection; start again without cursor")
		}
		after = inner
	}
	c, err := e.settle(spec.customerID, true)
	if err != nil {
		return nil, "", false, err
	}
	rows, err := c.search(e.ctx, spec.op, spec.tool, spec.customerID, spec.query(spec.conditions, after, size+1))
	if err != nil {
		return nil, "", false, err
	}
	if len(rows) > size {
		rows, hasMore = rows[:size], true
	}
	items = []T{}
	last := after
	for _, raw := range rows {
		item, id, err := mapRow(raw)
		if err != nil {
			return nil, "", false, err
		}
		if last != "" && !numberAfter(id, last) {
			return nil, "", false, provider.InvalidResponse(spec.op, subject+" returned the rows out of the requested order")
		}
		last = id
		items = append(items, item)
	}
	if hasMore {
		next = provider.EncodeCursor(binding, last)
	}
	return items, next, hasMore, nil
}

// pageArguments are the arguments every list of this group shares.
var pageArguments = []capability.Argument{
	{Name: "page_size", Description: "Rows per page, 1 to 1000, default 100"},
	{Name: "cursor", Description: "Opaque next_cursor of a previous page with the same arguments; the first page " +
		"when omitted. It only fits the connection that issued it"},
}

var pageProperties = schema{
	"page_size": schema{"type": "integer", "minimum": 1, "maximum": maxPageSize},
	"cursor":    schema{"type": "string", "minLength": 1, "maxLength": maxCursorBytes},
}

// handleEnv is the start of every handler of this group: it reads the connection.
func handleEnv(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	op, tool string) (callEnv, error) {
	conn, err := connectionOf(resolved)
	if err != nil {
		return callEnv{}, err
	}
	return callEnv{ctx: ctx, resolved: resolved, secrets: secrets, red: red, conn: conn, op: op, tool: tool}, nil
}

// unsureResult is the error of a change whose answer is not the one asked for: the change may have been made.
func unsureResult(op, message string) error {
	return unsure(provider.InvalidResponse(op, subject+" "+message), true)
}
