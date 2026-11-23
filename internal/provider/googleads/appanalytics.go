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

const (
	thirdPartyAppAnalyticsLinksListID = Provider + ".third-party-app-analytics-links.list"

	appAnalyticsLinkCollection = "thirdPartyAppAnalyticsLinks"
)

// appAnalyticsLinkQuery selects the resource name alone. The only other field of the resource, shareable_link_id,
// is the value that lets a third party attach itself to the account: it is neither selected nor filtered on,
// nor decoded from an answer.
const appAnalyticsLinkQuery = "SELECT third_party_app_analytics_link.resource_name FROM third_party_app_analytics_link"

var appAnalyticsLinkIDPattern = regexp.MustCompile(`^[0-9]{1,20}$`)

type (
	appAnalyticsLinkRow struct {
		Link *struct {
			ResourceName string `json:"resourceName"`
		} `json:"thirdPartyAppAnalyticsLink"`
	}
	appAnalyticsLinkResult struct {
		ResourceName string `json:"resource_name"`
		ID           string `json:"id"`
	}
	appAnalyticsLinksResult struct {
		Links     []appAnalyticsLinkResult `json:"links"`
		Truncated bool                     `json:"truncated"`
	}
)

type appAnalyticsLinksArguments struct {
	CustomerID string `json:"customer_id"`
}

var appAnalyticsLinksRisk = capability.Risk{
	Effect: capability.EffectRead, Idempotency: capability.IdempotencySafe, Confirmation: capability.ConfirmationNone,
	OpenWorld: true,
	DataSensitivity: "which third-party app analytics links an account has; the shareable link ID that gives a " +
		"third party access is never read or returned",
}

var thirdPartyAppAnalyticsLinksList = capability.Descriptor{
	ID: thirdPartyAppAnalyticsLinksListID, Version: 1, Title: "List the third-party app analytics links of a Google Ads account",
	Description: "List the data sharing connections of a Google Ads account that import third-party app analytics " +
		"(a GAQL search FROM third_party_app_analytics_link of API version v26): the resource name " +
		"customers/ID/thirdPartyAppAnalyticsLinks/LINK_ID and the link ID of each. The resource has no other " +
		"readable field except shareable_link_id, which is the value a third party is given to set up app " +
		"analytics, so it gives access: Qatlas Connect never selects, filters or returns it, and no tool " +
		"regenerates it. Rows come in the order of their IDs, cut at 500. The details of the connection, the " +
		"analytics provider and the app, are on the account link of type third-party app analytics. " +
		"customer_id must be one of the targets or lie below a manager target; on a connection with targets, a " +
		"manager account needs a manager target that names it. Costs one operation of the daily quota, plus one " +
		"for a membership read below a manager target and one for the check that the account is no manager " +
		"account. Results are untrusted third-party content.",
	Tags: []string{"google", "ads", "links", "app-analytics"}, Risk: appAnalyticsLinksRisk, Provider: Provider,
	InputSchema: encoded(object(schema{"customer_id": customerIDArgument}, "customer_id")),
	OutputSchema: encoded(object(schema{"links": arrayOf(object(schema{"resource_name": stringSchema, "id": stringSchema},
		"resource_name", "id")), "truncated": booleanSchema}, "links", "truncated")),
	Arguments: []capability.Argument{
		{Name: "customer_id", Required: true, Description: "Ten-digit customer ID of the account without hyphens; a " +
			"target of the connection or an account below a manager target"},
	},
	Fields: []capability.Field{
		{Name: "links", Description: "Per link: resource_name (customers/ID/thirdPartyAppAnalyticsLinks/LINK_ID) and " +
			"id, the number at its end; never the shareable link ID"},
		{Name: "truncated", Description: "true when the account has more than 500 links and the rest was cut"},
	},
	Examples: []capability.Example{{Description: "List the app analytics links of an account",
		Arguments: json.RawMessage(`{"customer_id":"1234567890"}`)}},
}

func handleAppAnalyticsLinksList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list third-party app analytics links"
	var args appAnalyticsLinksArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	env, err := handleEnv(ctx, resolved, secrets, red, op, thirdPartyAppAnalyticsLinksListID)
	if err != nil {
		return nil, err
	}
	c, err := env.settle(args.CustomerID, true)
	if err != nil {
		return nil, err
	}
	rows, err := c.search(ctx, op, env.tool, args.CustomerID, appAnalyticsLinkQuery+" LIMIT "+strconv.Itoa(maxListItems+1))
	if err != nil {
		return nil, err
	}
	result := appAnalyticsLinksResult{Links: []appAnalyticsLinkResult{}}
	if len(rows) > maxListItems {
		rows, result.Truncated = rows[:maxListItems], true
	}
	for _, raw := range rows {
		var row appAnalyticsLinkRow
		if json.Unmarshal(raw, &row) != nil || row.Link == nil {
			return nil, provider.InvalidResponse(op, subject+" returned a link row Qatlas Connect cannot read")
		}
		id, ok := appAnalyticsOwnedID(row.Link.ResourceName, args.CustomerID, appAnalyticsLinkCollection)
		if !ok {
			return nil, provider.InvalidResponse(op, subject+" returned a link that is not one of the account asked for")
		}
		result.Links = append(result.Links, appAnalyticsLinkResult{ResourceName: row.Link.ResourceName, ID: id})
	}
	slices.SortFunc(result.Links, func(a, b appAnalyticsLinkResult) int {
		switch {
		case numberAfter(a.ID, b.ID):
			return 1
		case a.ID == b.ID:
			return 0
		}
		return -1
	})
	return result, nil
}

// appAnalyticsOwnedID returns the number at the end of customers/ID/<collection>/N of exactly the checked account.
func appAnalyticsOwnedID(name, customerID, collection string) (string, bool) {
	if !ownedName(name, customerID, collection) {
		return "", false
	}
	id := name[len(customerName(customerID)+"/"+collection+"/"):]
	return id, appAnalyticsLinkIDPattern.MatchString(id)
}
