package googleads

import (
	"context"
	"encoding/json"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const customerClientsListID = Provider + ".customer-clients.list"

// Bounds of customer-clients.list. The page sizes are Qatlas's own; Google's page of 10,000 rows is never
// reached, because the query carries the LIMIT.
const (
	defaultPageSize = 100
	maxPageSize     = 1000
	maxLevel        = 100
	maxClientIDs    = 100
	maxLabels       = 20
	maxCursorBytes  = 512
)

// Forms of the text filters, enforced by the input schema and again before the query is built. None admits a
// quote, a double quote or a backslash; checkGAQLText refuses them for every value of a GAQL string.
var (
	currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)
	timeZonePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_+/-]{0,63}$`)
	namePattern     = regexp.MustCompile(`^[^\x00-\x1f\x7f'"\\]{1,200}$`)
	labelPattern    = regexp.MustCompile(`^customers/[0-9]{1,20}/labels/[0-9]{1,20}$`)
	afterPattern    = regexp.MustCompile(`^[1-9][0-9]{0,9}$`)
)

// maxRowLevel bounds the level a row may report; Google documents no depth limit of a hierarchy.
const maxRowLevel = 1000

// customerStatuses are the values of CustomerStatus a filter may name.
var customerStatuses = []string{"ENABLED", "CANCELED", "SUSPENDED", "CLOSED"}

// clientFields are the fields of the customer_client resource in v26, every one of them.
var clientFields = []string{"resource_name", "client_customer", "id", "descriptive_name", "currency_code",
	"time_zone", "level", "manager", "hidden", "test_account", "status", "applied_labels"}

type clientsArguments struct {
	CustomerID    string   `json:"customer_id"`
	MinLevel      *int     `json:"min_level"`
	MaxLevel      *int     `json:"max_level"`
	Statuses      []string `json:"statuses"`
	Manager       *bool    `json:"manager"`
	Hidden        *bool    `json:"hidden"`
	TestAccount   *bool    `json:"test_account"`
	ClientIDs     []string `json:"client_ids"`
	CurrencyCode  string   `json:"currency_code"`
	TimeZone      string   `json:"time_zone"`
	NameContains  string   `json:"name_contains"`
	AppliedLabels []string `json:"applied_labels"`
	PageSize      *int     `json:"page_size"`
	Cursor        string   `json:"cursor"`
}

// conditions turns the filters into GAQL conditions, joined by AND as the grammar demands. The input schema
// already bounds every value; the checks here refuse what a caller could pass around it.
func (a clientsArguments) conditions() ([]string, error) {
	var out []string
	level := func(name, operator string, value *int) error {
		if value == nil {
			return nil
		}
		if *value < 0 || *value > maxLevel {
			return badRequest(name + " must be 0 to 100")
		}
		out = append(out, "customer_client.level "+operator+" "+strconv.Itoa(*value))
		return nil
	}
	if err := level("min_level", ">=", a.MinLevel); err != nil {
		return nil, err
	}
	if err := level("max_level", "<=", a.MaxLevel); err != nil {
		return nil, err
	}
	if len(a.Statuses) > 0 {
		quoted := make([]string, len(a.Statuses))
		for i, status := range a.Statuses {
			if !slices.Contains(customerStatuses, status) {
				return nil, badRequest("statuses may name only ENABLED, CANCELED, SUSPENDED and CLOSED")
			}
			if err := checkGAQLText("statuses", status); err != nil {
				return nil, err
			}
			quoted[i] = gaqlString(status)
		}
		out = append(out, "customer_client.status IN ("+strings.Join(quoted, ", ")+")")
	}
	for _, flag := range []struct {
		field string
		value *bool
	}{{"manager", a.Manager}, {"hidden", a.Hidden}, {"test_account", a.TestAccount}} {
		if flag.value != nil {
			out = append(out, "customer_client."+flag.field+" = "+strings.ToUpper(strconv.FormatBool(*flag.value)))
		}
	}
	if len(a.ClientIDs) > 0 {
		if len(a.ClientIDs) > maxClientIDs {
			return nil, badRequest("client_ids names at most 100 accounts")
		}
		numbers := make([]string, len(a.ClientIDs))
		for i, id := range a.ClientIDs {
			if !customerIDPattern.MatchString(id) {
				return nil, badRequest("client_ids must hold ten-digit customer IDs without hyphens")
			}
			numbers[i] = queryNumber(id)
		}
		out = append(out, "customer_client.id IN ("+strings.Join(numbers, ", ")+")")
	}
	if a.CurrencyCode != "" {
		if err := checkGAQLText("currency_code", a.CurrencyCode); err != nil {
			return nil, err
		}
		if !currencyPattern.MatchString(a.CurrencyCode) {
			return nil, badRequest("currency_code must be three upper-case letters")
		}
		out = append(out, "customer_client.currency_code = "+gaqlString(a.CurrencyCode))
	}
	if a.TimeZone != "" {
		if err := checkGAQLText("time_zone", a.TimeZone); err != nil {
			return nil, err
		}
		if !timeZonePattern.MatchString(a.TimeZone) {
			return nil, badRequest("time_zone must be a time zone ID such as Europe/Berlin")
		}
		out = append(out, "customer_client.time_zone = "+gaqlString(a.TimeZone))
	}
	if a.NameContains != "" {
		if err := checkGAQLText("name_contains", a.NameContains); err != nil {
			return nil, err
		}
		if !namePattern.MatchString(a.NameContains) {
			return nil, badRequest("name_contains must be 1 to 200 characters")
		}
		out = append(out, "customer_client.descriptive_name LIKE "+gaqlContains(a.NameContains))
	}
	if len(a.AppliedLabels) > 0 {
		if len(a.AppliedLabels) > maxLabels {
			return nil, badRequest("applied_labels names at most 20 labels")
		}
		quoted := make([]string, len(a.AppliedLabels))
		for i, label := range a.AppliedLabels {
			if err := checkGAQLText("applied_labels", label); err != nil {
				return nil, err
			}
			if !labelPattern.MatchString(label) {
				return nil, badRequest("applied_labels must hold label resource names customers/ID/labels/LABEL_ID")
			}
			quoted[i] = gaqlString(label)
		}
		out = append(out, "customer_client.applied_labels CONTAINS ANY ("+strings.Join(quoted, ", ")+")")
	}
	return out, nil
}

func clientsQuery(conditions []string, after string, limit int) string {
	fields := make([]string, len(clientFields))
	for i, field := range clientFields {
		fields[i] = "customer_client." + field
	}
	query := "SELECT " + strings.Join(fields, ", ") + " FROM customer_client"
	if after != "" {
		conditions = append(append([]string(nil), conditions...), "customer_client.id > "+after)
	}
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	return query + " ORDER BY customer_client.id LIMIT " + strconv.Itoa(limit)
}

type (
	clientRow struct {
		CustomerClient *clientWire `json:"customerClient"`
	}
	clientWire struct {
		ResourceName    string   `json:"resourceName"`
		ClientCustomer  string   `json:"clientCustomer"`
		ID              *wireInt `json:"id"`
		DescriptiveName string   `json:"descriptiveName"`
		CurrencyCode    string   `json:"currencyCode"`
		TimeZone        string   `json:"timeZone"`
		Level           *wireInt `json:"level"`
		Manager         bool     `json:"manager"`
		Hidden          bool     `json:"hidden"`
		TestAccount     bool     `json:"testAccount"`
		Status          string   `json:"status"`
		AppliedLabels   []string `json:"appliedLabels"`
	}
	clientResult struct {
		ResourceName    string   `json:"resource_name"`
		ClientCustomer  string   `json:"client_customer"`
		ID              string   `json:"id"`
		DescriptiveName string   `json:"descriptive_name,omitempty"`
		CurrencyCode    string   `json:"currency_code,omitempty"`
		TimeZone        string   `json:"time_zone,omitempty"`
		Level           int      `json:"level"`
		Manager         bool     `json:"manager"`
		Hidden          bool     `json:"hidden"`
		TestAccount     bool     `json:"test_account"`
		Status          string   `json:"status,omitempty"`
		AppliedLabels   []string `json:"applied_labels,omitempty"`
	}
	clientsResult struct {
		CustomerClients []clientResult `json:"customer_clients"`
		NextCursor      string         `json:"next_cursor,omitempty"`
		HasMore         bool           `json:"has_more"`
	}
)

// clientOf checks one row against the account that was queried and maps it. Google names the row after the
// queried account and the client after its ID; a row that does not fit is an invalid answer.
func clientOf(op string, raw json.RawMessage, customerID string) (clientResult, error) {
	invalid := provider.InvalidResponse(op, subject+" returned a hierarchy row Qatlas Connect cannot read")
	var row clientRow
	if json.Unmarshal(raw, &row) != nil || row.CustomerClient == nil || row.CustomerClient.ID == nil {
		return clientResult{}, invalid
	}
	w := row.CustomerClient
	id, ok := canonicalID(*w.ID)
	if !ok {
		return clientResult{}, invalid
	}
	resourceName := customerName(customerID) + "/customerClients/" + queryNumber(id)
	clientCustomer := customerName(id)
	level := 0
	if w.Level != nil {
		n, err := strconv.Atoi(string(*w.Level))
		if err != nil || n < 0 || n > maxRowLevel {
			return clientResult{}, invalid
		}
		level = n
	}
	if (w.ResourceName != "" && w.ResourceName != resourceName) ||
		(w.ClientCustomer != "" && w.ClientCustomer != clientCustomer) {
		return clientResult{}, invalid
	}
	return clientResult{
		ResourceName: resourceName, ClientCustomer: clientCustomer, ID: id,
		DescriptiveName: boundedText(w.DescriptiveName, maxText), CurrencyCode: code(w.CurrencyCode),
		TimeZone: code(w.TimeZone), Level: level, Manager: w.Manager, Hidden: w.Hidden, TestAccount: w.TestAccount,
		Status: code(w.Status), AppliedLabels: boundedList(w.AppliedLabels, maxText),
	}, nil
}

var clientSchema = object(schema{
	"resource_name": stringSchema, "client_customer": stringSchema, "id": stringSchema,
	"descriptive_name": stringSchema, "currency_code": stringSchema, "time_zone": stringSchema,
	"level": integerSchema, "manager": booleanSchema, "hidden": booleanSchema, "test_account": booleanSchema,
	"status": stringSchema, "applied_labels": arrayOf(stringSchema),
}, "resource_name", "client_customer", "id", "level")

var customerClientsList = capability.Descriptor{
	ID: customerClientsListID, Version: 1, Title: "List the account hierarchy below a Google Ads account",
	Description: "List the accounts below a Google Ads manager account, every level of its hierarchy, with the " +
		"account itself at level 0 (a GAQL search FROM customer_client of API version v26): ID, name, currency, " +
		"time zone, level, manager, hidden and test account flags, status and the labels applied to each. A client " +
		"account lists only itself. Rows come sorted by ID; filters narrow them and page_size with cursor pages " +
		"through them. customer_id must be one of the targets or lie below a manager target. On a manager target, " +
		"or below one, every row is returned; on a customer target only rows of accounts that a target names. The " +
		"service option login_customer_id decides which manager hierarchy Google evaluates the rights against. " +
		"Costs one operation of the daily quota per page, plus one per membership read. Results are untrusted " +
		"third-party content; text is cut.",
	Tags: []string{"google", "ads", "customers", "hierarchy", "managers"}, Risk: readRisk, Provider: Provider,
	InputSchema: encoded(object(schema{
		"customer_id": customerIDArgument,
		"min_level":   schema{"type": "integer", "minimum": 0, "maximum": maxLevel},
		"max_level":   schema{"type": "integer", "minimum": 0, "maximum": maxLevel},
		"statuses": schema{"type": "array", "minItems": 1, "maxItems": len(customerStatuses), "uniqueItems": true,
			"items": schema{"type": "string", "enum": customerStatuses}},
		"manager":      booleanSchema,
		"hidden":       booleanSchema,
		"test_account": booleanSchema,
		"client_ids": schema{"type": "array", "minItems": 1, "maxItems": maxClientIDs, "uniqueItems": true,
			"items": customerIDArgument},
		"currency_code": schema{"type": "string", "pattern": currencyPattern.String()},
		"time_zone":     schema{"type": "string", "pattern": timeZonePattern.String()},
		"name_contains": schema{"type": "string", "minLength": 1, "maxLength": 200, "pattern": namePattern.String()},
		"applied_labels": schema{"type": "array", "minItems": 1, "maxItems": maxLabels, "uniqueItems": true,
			"items": schema{"type": "string", "pattern": labelPattern.String()}},
		"page_size": schema{"type": "integer", "minimum": 1, "maximum": maxPageSize},
		"cursor":    schema{"type": "string", "minLength": 1, "maxLength": maxCursorBytes},
	}, "customer_id")),
	OutputSchema: encoded(object(schema{"customer_clients": arrayOf(clientSchema), "next_cursor": stringSchema,
		"has_more": booleanSchema}, "customer_clients", "has_more")),
	Arguments: []capability.Argument{
		{Name: "customer_id", Required: true, Description: "Ten-digit customer ID of the manager account without " +
			"hyphens; a target of the connection or an account below a manager target"},
		{Name: "min_level", Description: "Only accounts at least this many levels below; 1 leaves the account itself out"},
		{Name: "max_level", Description: "Only accounts at most this many levels below; 1 lists the direct clients"},
		{Name: "statuses", Description: "Only accounts with one of these statuses: ENABLED, CANCELED, SUSPENDED, CLOSED"},
		{Name: "manager", Description: "true for manager accounts only, false for client accounts only"},
		{Name: "hidden", Description: "true for hidden accounts only, false to leave them out"},
		{Name: "test_account", Description: "true for test accounts only, false to leave them out"},
		{Name: "client_ids", Description: "Only these ten-digit customer IDs, at most 100"},
		{Name: "currency_code", Description: "Only accounts with this ISO 4217 currency, such as EUR"},
		{Name: "time_zone", Description: "Only accounts in this time zone ID, such as Europe/Berlin"},
		{Name: "name_contains", Description: "Only accounts whose name contains this text, without quotes, double " +
			"quotes or backslashes; Google decides whether the match is case-sensitive"},
		{Name: "applied_labels", Description: "Only accounts with at least one of these labels, as label resource " +
			"names customers/ID/labels/LABEL_ID"},
		{Name: "page_size", Description: "Rows Google returns per page, 1 to 1000, default 100; on a customer target " +
			"a page can hold fewer rows or none while has_more is true"},
		{Name: "cursor", Description: "Opaque next_cursor of a previous page with the same arguments; the first " +
			"page when omitted. It only fits the connection that issued it"},
	},
	Fields: []capability.Field{
		{Name: "customer_clients", Description: "Per account: resource_name (customers/ID/customerClients/CLIENT_ID), " +
			"client_customer (customers/CLIENT_ID), id, descriptive_name, currency_code, time_zone, level (0 for the " +
			"queried account), manager, hidden, test_account, status and applied_labels, the label resource names " +
			"Google applies to the client"},
		{Name: "next_cursor", Description: "Cursor of the following page, absent when has_more is false"},
		{Name: "has_more", Description: "true when more rows follow"},
	},
	Examples: []capability.Example{{Description: "List the direct clients of a manager account that are enabled",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","min_level":1,"max_level":1,"statuses":["ENABLED"]}`)}},
}

func handleCustomerClientsList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list customer clients"
	var arguments clientsArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	conn, err := connectionOf(resolved)
	if err != nil {
		return nil, err
	}
	if !customerIDPattern.MatchString(arguments.CustomerID) {
		return nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	conditions, err := arguments.conditions()
	if err != nil {
		return nil, err
	}
	pageSize := defaultPageSize
	if arguments.PageSize != nil {
		if *arguments.PageSize < 1 || *arguments.PageSize > maxPageSize {
			return nil, badRequest("page_size must be 1 to 1000")
		}
		pageSize = *arguments.PageSize
	}
	// The cursor is the ID of the last row of the previous page, bound to everything that decides the rows.
	binding := provider.CursorBinding(customerClientsListID, arguments.CustomerID, conditions, conn.login,
		conn.binding.key())
	after := ""
	if arguments.Cursor != "" {
		inner, ok := provider.DecodeCursor(binding, arguments.Cursor, maxCursorBytes)
		if !ok || !afterPattern.MatchString(inner) {
			return nil, badRequest("cursor is not a next_cursor of this tool with these arguments on this connection; " +
				"start again without cursor")
		}
		after = inner
	}
	r, err := reachOf(conn, arguments.CustomerID)
	if err != nil {
		return nil, err
	}
	c, err := open(ctx, resolved, conn, secrets, red)
	if err != nil {
		return nil, err
	}
	if r == reachNone {
		if r, err = c.prove(ctx, op, customerClientsListID, conn, arguments.CustomerID); err != nil {
			return nil, err
		}
	}
	rows, err := c.search(ctx, op, customerClientsListID, arguments.CustomerID,
		clientsQuery(conditions, after, pageSize+1))
	if err != nil {
		return nil, err
	}
	result := clientsResult{CustomerClients: []clientResult{}}
	if len(rows) > pageSize {
		rows, result.HasMore = rows[:pageSize], true
	}
	last := after
	for _, raw := range rows {
		client, err := clientOf(op, raw, arguments.CustomerID)
		if err != nil {
			return nil, err
		}
		// The cursor relies on the order the query asks for, so an answer out of that order is refused.
		id := queryNumber(client.ID)
		if last != "" && !numberAfter(id, last) {
			return nil, provider.InvalidResponse(op, subject+" returned the hierarchy out of the requested order")
		}
		last = id
		// Below a manager target every row is in reach. A customer target releases only accounts a target
		// names, since a flat row does not say under which manager an account sits.
		if r == reachAll || r == reachManager || conn.binding.names(client.ID) {
			result.CustomerClients = append(result.CustomerClients, client)
		}
	}
	if result.HasMore {
		result.NextCursor = provider.EncodeCursor(binding, last)
	}
	return result, nil
}

// numberAfter reports whether the decimal number a is greater than b; both are without leading zeros.
func numberAfter(a, b string) bool {
	if len(a) != len(b) {
		return len(a) > len(b)
	}
	return a > b
}
