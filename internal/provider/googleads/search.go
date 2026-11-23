package googleads

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"unicode"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// searchPage is the answer of GoogleAdsService.Search. Every query of this provider carries a LIMIT below the
// fixed page of 10,000 rows, so a next page never matters and its token is not read.
type searchPage struct {
	Results                  []json.RawMessage `json:"results"`
	TotalResultsCount        *wireInt          `json:"totalResultsCount"`
	FieldMask                string            `json:"fieldMask"`
	SummaryRow               json.RawMessage   `json:"summaryRow"`
	QueryResourceConsumption *wireInt          `json:"queryResourceConsumption"`
	MetricAttributes         []json.RawMessage `json:"metricAttributes"`
}

// searchRequest is the body of a search: the query and the documented request options, nothing else.
type searchRequest struct {
	Query          string          `json:"query"`
	ValidateOnly   bool            `json:"validateOnly,omitempty"`
	SearchSettings *searchSettings `json:"searchSettings,omitempty"`
}

type searchSettings struct {
	ReturnTotalResultsCount bool `json:"returnTotalResultsCount,omitempty"`
	ReturnSummaryRow        bool `json:"returnSummaryRow,omitempty"`
	OmitResults             bool `json:"omitResults,omitempty"`
}

// search runs one GAQL query that this provider built on an account and returns its rows. The query travels in
// the body, never in the URL.
func (c *client) search(ctx context.Context, op, tool, customerID, query string) ([]json.RawMessage, error) {
	page, err := c.searchPage(ctx, op, tool, customerID, searchRequest{Query: query})
	if err != nil {
		return nil, err
	}
	return page.Results, nil
}

// searchPage runs one search with the given body on an account and returns the whole first page.
func (c *client) searchPage(ctx context.Context, op, tool, customerID string, body searchRequest) (searchPage, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return searchPage{}, failure(op, "the query could not be encoded")
	}
	var page searchPage
	if err := c.read(ctx, request{op: op, tool: tool, method: http.MethodPost,
		path: "/" + customerName(customerID) + "/googleAds:search", body: encoded, onAccount: true}, &page); err != nil {
		return searchPage{}, err
	}
	return page, nil
}

// checkGAQLText refuses a value that is to become a GAQL string when it holds a quote, a double quote, a
// backslash or a control character. Qatlas does not escape GAQL strings, so such a value is refused before any
// secret is read; the message names the argument, never the value.
func checkGAQLText(argument, value string) error {
	for _, r := range value {
		if r == '\'' || r == '"' || r == '\\' || unicode.IsControl(r) {
			return badRequest(argument + " must not hold quotes, backslashes or control characters")
		}
	}
	return nil
}

// gaqlString is the GAQL string literal of a value that passed checkGAQLText.
func gaqlString(value string) string { return "'" + value + "'" }

// gaqlContains is the LIKE pattern of a GAQL string that holds value anywhere. The grammar matches [, ], % and _
// literally only inside square brackets.
func gaqlContains(value string) string {
	escaped := strings.NewReplacer("[", "[[]", "]", "[]]", "%", "[%]", "_", "[_]").Replace(value)
	return gaqlString("%" + escaped + "%")
}

// membershipRow is a row of the membership read: the ID of a client below the queried manager.
type membershipRow struct {
	CustomerClient struct {
		ID *wireInt `json:"id"`
	} `json:"customerClient"`
}

// reachOf binds the account of a call to the targets. A connection without targets, a manager target and a
// customer target need no request. An account that no target names is refused at once without a manager
// target; with one, prove must follow before the tool reads anything of the account.
func reachOf(conn connection, customerID string) (reach, error) {
	r := conn.binding.direct(customerID)
	if r == reachNone && len(conn.binding.managers) == 0 {
		return reachNone, badRequest(outsideTargets)
	}
	return r, nil
}

// prove binds an account that no target names to a manager target: one read of the hierarchy of each manager
// target, under the login-customer-id of the connection, asks for exactly this client, until one shows it.
// Only a single row with the asked ID proves membership. An answer that is unreadable or holds anything else,
// and a failed read, never prove it: the call is refused, and a failed read is reported with its class.
func (c *client) prove(ctx context.Context, op, tool string, conn connection, customerID string) (reach, error) {
	query := "SELECT customer_client.id FROM customer_client WHERE customer_client.id = " +
		queryNumber(customerID) + " LIMIT 2"
	var first error
	for _, manager := range conn.binding.managers {
		rows, err := c.search(ctx, op, tool, manager, query)
		if err == nil {
			var proved, clear bool
			proved, clear = proves(rows, customerID)
			if proved {
				return reachManager, nil
			}
			if !clear {
				err = provider.InvalidResponse(op, subject+" answered the membership check in a form Qatlas "+
					"Connect cannot read")
			}
		}
		if err != nil && first == nil {
			first = err
		}
	}
	if first != nil {
		var failed *provider.Error
		if errors.As(first, &failed) {
			copied := *failed
			copied.Message = "the membership of customer_id below a manager target could not be checked, so the " +
				"call is refused: " + failed.Message
			return reachNone, &copied
		}
		return reachNone, first
	}
	return reachNone, badRequest(outsideTargets)
}

// proves reads the answer of one membership read: whether it proves the account, and whether it was clear.
func proves(rows []json.RawMessage, customerID string) (proved, clear bool) {
	switch len(rows) {
	case 0:
		return false, true
	case 1:
		var row membershipRow
		if json.Unmarshal(rows[0], &row) != nil || row.CustomerClient.ID == nil {
			return false, false
		}
		id, ok := canonicalID(*row.CustomerClient.ID)
		return ok && id == customerID, ok && id == customerID
	}
	return false, false
}
