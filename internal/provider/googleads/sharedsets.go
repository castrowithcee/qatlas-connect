package googleads

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	sharedSetsCreateID = Provider + ".shared-sets.create"
	sharedSetsUpdateID = Provider + ".shared-sets.update"
	sharedSetsRemoveID = Provider + ".shared-sets.remove"

	campaignSharedSetsCreateID = Provider + ".campaign-shared-sets.create"
	campaignSharedSetsRemoveID = Provider + ".campaign-shared-sets.remove"

	sharedSetsService         = "/sharedSets"
	sharedSetsCollection      = "sharedSets"
	campaignSharedSetsService = "/campaignSharedSets"
	campaignSharedSetsPart    = "campaignSharedSets"
)

// Bounds as the reference of v26 documents them: SharedSet.name is required and 1 to 255 UTF-8 bytes, and
// SharedSet.type is required and immutable. status, id, member_count and reference_count are output only.
// vertical_ads_item_vertical_type is immutable and required for the type VERTICAL_ADS_ITEM_GROUP_RULE_LIST.
const (
	sharedSetNameMax = 255
	// sharedSetsMaxOperations is Qatlas's own bound per call; Google documents none for these services.
	sharedSetsMaxOperations = 50
)

// The values of SharedSetType that a caller may set. UNSPECIFIED and UNKNOWN are not values to send, and WEBPAGES
// is documented as not publicly available.
var (
	sharedSetTypes = []string{"NEGATIVE_KEYWORDS", "NEGATIVE_PLACEMENTS", "BRANDS", "RETAIL_FILTER",
		"VERTICAL_ADS_ITEM_GROUP_RULE_LIST", "ACCOUNT_LEVEL_NEGATIVE_KEYWORDS"}
	sharedSetVerticalTypes = []string{"HOTELS", "FLIGHTS", "EVENTS", "RENTAL_CARS", "THINGS_TO_DO", "VACATION_RENTALS"}
)

const sharedSetVerticalRuleList = "VERTICAL_ADS_ITEM_GROUP_RULE_LIST"

func sharedSetName(customerID, id string) string {
	return customerName(customerID) + "/sharedSets/" + id
}

func campaignName(customerID, id string) string { return customerName(customerID) + "/campaigns/" + id }

func campaignSharedSetName(customerID, campaignID, sharedSetID string) string {
	return customerName(customerID) + "/campaignSharedSets/" + campaignID + "~" + sharedSetID
}

// campaignSharedSetIDsOfName returns the campaign and shared set IDs of a resource name of the account.
func campaignSharedSetIDsOfName(name, customerID string) (campaignID, sharedSetID string, ok bool) {
	rest, ok := strings.CutPrefix(name, customerName(customerID)+"/campaignSharedSets/")
	if !ok {
		return "", "", false
	}
	campaignID, sharedSetID, ok = strings.Cut(rest, "~")
	if !ok || !numericIDPattern.MatchString(campaignID) || !numericIDPattern.MatchString(sharedSetID) {
		return "", "", false
	}
	return campaignID, sharedSetID, true
}

func sharedSetsCheckCustomer(customerID string) error {
	if !customerIDPattern.MatchString(customerID) {
		return badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	return nil
}

func sharedSetsCheckCount(argument string, n int) error {
	if n < 1 || n > sharedSetsMaxOperations {
		return badRequest(argument + " must hold 1 to " + strconv.Itoa(sharedSetsMaxOperations) + " entries")
	}
	return nil
}

func sharedSetsCheckID(argument, value, what string) error {
	if !numericIDPattern.MatchString(value) {
		return badRequest(argument + " must be the ID of an existing " + what + ", a positive number")
	}
	if _, err := strconv.ParseInt(value, 10, 64); err != nil {
		return badRequest(argument + " must be the ID of an existing " + what + ", a positive number")
	}
	return nil
}

func sharedSetsCheckName(value string) error {
	if n := len(value); n < 1 || n > sharedSetNameMax {
		return badRequest("name must be 1 to " + strconv.Itoa(sharedSetNameMax) + " bytes")
	}
	if !utf8.ValidString(value) || strings.ContainsFunc(value, unicode.IsControl) {
		return badRequest("name must be valid text without control characters")
	}
	return nil
}

// --- arguments ---

type (
	sharedSetsNew struct {
		Name         string `json:"name"`
		Type         string `json:"type"`
		VerticalType string `json:"vertical_ads_item_vertical_type"`
	}
	sharedSetsEdit struct {
		SharedSetID string `json:"shared_set_id"`
		Name        string `json:"name"`
	}
	sharedSetsCreateArguments struct {
		CustomerID          string          `json:"customer_id"`
		SharedSets          []sharedSetsNew `json:"shared_sets"`
		ValidateOnly        bool            `json:"validate_only"`
		ResponseContentType string          `json:"response_content_type"`
	}
	sharedSetsUpdateArguments struct {
		CustomerID          string           `json:"customer_id"`
		SharedSets          []sharedSetsEdit `json:"shared_sets"`
		ValidateOnly        bool             `json:"validate_only"`
		ResponseContentType string           `json:"response_content_type"`
	}
	sharedSetsRemoveArguments struct {
		CustomerID   string   `json:"customer_id"`
		SharedSetIDs []string `json:"shared_set_ids"`
		ValidateOnly bool     `json:"validate_only"`
	}
	campaignSharedSetLink struct {
		CampaignID  string `json:"campaign_id"`
		SharedSetID string `json:"shared_set_id"`
	}
	campaignSharedSetsCreateArguments struct {
		CustomerID          string                  `json:"customer_id"`
		Links               []campaignSharedSetLink `json:"links"`
		ValidateOnly        bool                    `json:"validate_only"`
		ResponseContentType string                  `json:"response_content_type"`
	}
	campaignSharedSetsRemoveArguments struct {
		CustomerID   string                  `json:"customer_id"`
		Links        []campaignSharedSetLink `json:"links"`
		ValidateOnly bool                    `json:"validate_only"`
	}
)

func (a sharedSetsCreateArguments) operations() ([]any, error) {
	if err := sharedSetsCheckCustomer(a.CustomerID); err != nil {
		return nil, err
	}
	if err := checkContentType(a.ResponseContentType); err != nil {
		return nil, err
	}
	if err := sharedSetsCheckCount("shared_sets", len(a.SharedSets)); err != nil {
		return nil, err
	}
	ops := make([]any, 0, len(a.SharedSets))
	for _, s := range a.SharedSets {
		if err := sharedSetsCheckName(s.Name); err != nil {
			return nil, err
		}
		if !slices.Contains(sharedSetTypes, s.Type) {
			return nil, badRequest("type must be one of " + strings.Join(sharedSetTypes, ", "))
		}
		create := map[string]any{"name": s.Name, "type": s.Type}
		switch {
		case s.Type == sharedSetVerticalRuleList && !slices.Contains(sharedSetVerticalTypes, s.VerticalType):
			return nil, badRequest("type " + sharedSetVerticalRuleList + " needs vertical_ads_item_vertical_type, one of " +
				strings.Join(sharedSetVerticalTypes, ", "))
		case s.Type != sharedSetVerticalRuleList && s.VerticalType != "":
			return nil, badRequest("vertical_ads_item_vertical_type belongs to the type " + sharedSetVerticalRuleList + " only")
		case s.VerticalType != "":
			create["verticalAdsItemVerticalType"] = s.VerticalType
		}
		ops = append(ops, map[string]any{"create": create})
	}
	return ops, nil
}

func (a sharedSetsUpdateArguments) operations() ([]any, []string, error) {
	if err := sharedSetsCheckCustomer(a.CustomerID); err != nil {
		return nil, nil, err
	}
	if err := checkContentType(a.ResponseContentType); err != nil {
		return nil, nil, err
	}
	if err := sharedSetsCheckCount("shared_sets", len(a.SharedSets)); err != nil {
		return nil, nil, err
	}
	ops := make([]any, 0, len(a.SharedSets))
	names := make([]string, 0, len(a.SharedSets))
	for _, s := range a.SharedSets {
		if err := sharedSetsCheckID("shared_set_id", s.SharedSetID, "shared set"); err != nil {
			return nil, nil, err
		}
		if err := sharedSetsCheckName(s.Name); err != nil {
			return nil, nil, err
		}
		name := sharedSetName(a.CustomerID, s.SharedSetID)
		if slices.Contains(names, name) {
			return nil, nil, badRequest("shared_sets names each shared_set_id at most once")
		}
		names = append(names, name)
		ops = append(ops, map[string]any{"update": map[string]any{"resourceName": name, "name": s.Name},
			"updateMask": "name"})
	}
	return ops, names, nil
}

func (a sharedSetsRemoveArguments) operations() ([]any, []string, error) {
	if err := sharedSetsCheckCustomer(a.CustomerID); err != nil {
		return nil, nil, err
	}
	if err := sharedSetsCheckCount("shared_set_ids", len(a.SharedSetIDs)); err != nil {
		return nil, nil, err
	}
	ops := make([]any, 0, len(a.SharedSetIDs))
	names := make([]string, 0, len(a.SharedSetIDs))
	for _, id := range a.SharedSetIDs {
		if err := sharedSetsCheckID("shared_set_ids", id, "shared set"); err != nil {
			return nil, nil, err
		}
		name := sharedSetName(a.CustomerID, id)
		if slices.Contains(names, name) {
			return nil, nil, badRequest("shared_set_ids names each shared set at most once")
		}
		names = append(names, name)
		ops = append(ops, map[string]any{"remove": name})
	}
	return ops, names, nil
}

// campaignSharedSetsLinks checks the links and returns them as resource names of the checked account.
func campaignSharedSetsLinks(customerID string, links []campaignSharedSetLink) ([]string, error) {
	if err := sharedSetsCheckCustomer(customerID); err != nil {
		return nil, err
	}
	if err := sharedSetsCheckCount("links", len(links)); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(links))
	for _, l := range links {
		if err := sharedSetsCheckID("campaign_id", l.CampaignID, "campaign"); err != nil {
			return nil, err
		}
		if err := sharedSetsCheckID("shared_set_id", l.SharedSetID, "shared set"); err != nil {
			return nil, err
		}
		name := campaignSharedSetName(customerID, l.CampaignID, l.SharedSetID)
		if slices.Contains(names, name) {
			return nil, badRequest("links names each pair of campaign_id and shared_set_id at most once")
		}
		names = append(names, name)
	}
	return names, nil
}

func (a campaignSharedSetsCreateArguments) operations() ([]any, []string, error) {
	if err := checkContentType(a.ResponseContentType); err != nil {
		return nil, nil, err
	}
	names, err := campaignSharedSetsLinks(a.CustomerID, a.Links)
	if err != nil {
		return nil, nil, err
	}
	ops := make([]any, 0, len(a.Links))
	for _, l := range a.Links {
		ops = append(ops, map[string]any{"create": map[string]any{
			"campaign":  campaignName(a.CustomerID, l.CampaignID),
			"sharedSet": sharedSetName(a.CustomerID, l.SharedSetID),
		}})
	}
	return ops, names, nil
}

func (a campaignSharedSetsRemoveArguments) operations() ([]any, []string, error) {
	names, err := campaignSharedSetsLinks(a.CustomerID, a.Links)
	if err != nil {
		return nil, nil, err
	}
	ops := make([]any, 0, len(names))
	for _, name := range names {
		ops = append(ops, map[string]any{"remove": name})
	}
	return ops, names, nil
}

// --- results ---

type (
	sharedSetResult struct {
		ResourceName string `json:"resource_name"`
		ID           string `json:"id,omitempty"`
		Name         string `json:"name,omitempty"`
		Status       string `json:"status,omitempty"`
		Type         string `json:"type,omitempty"`
		VerticalType string `json:"vertical_ads_item_vertical_type,omitempty"`
		MemberCount  string `json:"member_count,omitempty"`
		RefCount     string `json:"reference_count,omitempty"`
	}
	sharedSetMutated struct {
		SharedSetID  string           `json:"shared_set_id"`
		ResourceName string           `json:"resource_name"`
		SharedSet    *sharedSetResult `json:"shared_set,omitempty"`
	}
	sharedSetsChanged struct {
		CustomerID   string             `json:"customer_id"`
		ValidateOnly bool               `json:"validate_only"`
		Results      []sharedSetMutated `json:"results"`
	}
	sharedSetWire struct {
		ResourceName   string   `json:"resourceName"`
		ID             *wireInt `json:"id"`
		Name           string   `json:"name"`
		Status         string   `json:"status"`
		Type           string   `json:"type"`
		VerticalType   string   `json:"verticalAdsItemVerticalType"`
		MemberCount    *wireInt `json:"memberCount"`
		ReferenceCount *wireInt `json:"referenceCount"`
	}

	campaignSharedSetResult struct {
		ResourceName string `json:"resource_name"`
		Campaign     string `json:"campaign,omitempty"`
		SharedSet    string `json:"shared_set,omitempty"`
		Status       string `json:"status,omitempty"`
	}
	campaignSharedSetMutated struct {
		CampaignID        string                   `json:"campaign_id"`
		SharedSetID       string                   `json:"shared_set_id"`
		ResourceName      string                   `json:"resource_name"`
		CampaignSharedSet *campaignSharedSetResult `json:"campaign_shared_set,omitempty"`
	}
	campaignSharedSetsChanged struct {
		CustomerID   string                     `json:"customer_id"`
		ValidateOnly bool                       `json:"validate_only"`
		Results      []campaignSharedSetMutated `json:"results"`
	}
	campaignSharedSetWire struct {
		ResourceName string `json:"resourceName"`
		Campaign     string `json:"campaign"`
		SharedSet    string `json:"sharedSet"`
		Status       string `json:"status"`
	}
)

// sharedSetOf binds a returned shared set to the result it came with.
func sharedSetOf(raw json.RawMessage, name, id string) (*sharedSetResult, bool) {
	var w sharedSetWire
	if json.Unmarshal(raw, &w) != nil || w.ResourceName != name {
		return nil, false
	}
	out := &sharedSetResult{ResourceName: name, Name: boundedText(w.Name, maxText), Status: code(w.Status),
		Type: code(w.Type), VerticalType: code(w.VerticalType)}
	if w.ID != nil {
		if string(*w.ID) != id {
			return nil, false
		}
		out.ID = id
	}
	if w.MemberCount != nil {
		out.MemberCount = string(*w.MemberCount)
	}
	if w.ReferenceCount != nil {
		out.RefCount = string(*w.ReferenceCount)
	}
	return out, true
}

func sharedSetsResults(op string, results []listResult) ([]sharedSetMutated, error) {
	out := make([]sharedSetMutated, 0, len(results))
	for _, r := range results {
		for _, previous := range out {
			if previous.SharedSetID == r.ID {
				return nil, unsureResult(op, "answered with the same shared set twice")
			}
		}
		item := sharedSetMutated{SharedSetID: r.ID, ResourceName: r.ResourceName}
		if r.Resource != nil {
			s, ok := sharedSetOf(r.Resource, r.ResourceName, r.ID)
			if !ok {
				return nil, unsureResult(op, "answered with a shared set that is not the one asked for")
			}
			item.SharedSet = s
		}
		out = append(out, item)
	}
	return out, nil
}

// campaignSharedSetsResults binds the answer to the checked account and the pairs asked for, in order.
func campaignSharedSetsResults(op, customerID string, raw []map[string]json.RawMessage, want []string,
	validateOnly bool) ([]campaignSharedSetMutated, error) {
	out := []campaignSharedSetMutated{}
	if validateOnly && len(raw) == 0 {
		return out, nil
	}
	if len(raw) != len(want) {
		return nil, unsureResult(op, "answered with another number of results than the entries asked for")
	}
	for i, result := range raw {
		var name string
		if json.Unmarshal(result["resourceName"], &name) != nil {
			return nil, unsureResult(op, "answered with a result without a resource name")
		}
		campaignID, sharedSetID, ok := campaignSharedSetIDsOfName(name, customerID)
		if !ok || name != want[i] {
			return nil, unsureResult(op, "answered with a result for another link than the one asked for")
		}
		item := campaignSharedSetMutated{CampaignID: campaignID, SharedSetID: sharedSetID, ResourceName: name}
		if body := result["campaignSharedSet"]; len(body) > 0 && string(body) != "null" {
			var w campaignSharedSetWire
			if json.Unmarshal(body, &w) != nil || w.ResourceName != name ||
				(w.Campaign != "" && w.Campaign != campaignName(customerID, campaignID)) ||
				(w.SharedSet != "" && w.SharedSet != sharedSetName(customerID, sharedSetID)) {
				return nil, unsureResult(op, "answered with a link that is not the one asked for")
			}
			item.CampaignSharedSet = &campaignSharedSetResult{ResourceName: name, Campaign: w.Campaign,
				SharedSet: w.SharedSet, Status: code(w.Status)}
		}
		out = append(out, item)
	}
	return out, nil
}

// --- descriptors ---

var (
	sharedSetNameSchema = schema{"type": "string", "minLength": 1, "maxLength": sharedSetNameMax}
	numericIDSchema     = schema{"type": "string", "pattern": numericIDPattern.String()}

	sharedSetsOutputSchema = object(schema{"customer_id": stringSchema, "validate_only": booleanSchema,
		"results": arrayOf(object(schema{"shared_set_id": stringSchema, "resource_name": stringSchema,
			"shared_set": object(schema{"resource_name": stringSchema, "id": stringSchema, "name": stringSchema,
				"status": stringSchema, "type": stringSchema, "vertical_ads_item_vertical_type": stringSchema,
				"member_count": stringSchema, "reference_count": stringSchema}, "resource_name")},
			"shared_set_id", "resource_name"))}, "customer_id", "validate_only", "results")
	campaignSharedSetsOutputSchema = object(schema{"customer_id": stringSchema, "validate_only": booleanSchema,
		"results": arrayOf(object(schema{"campaign_id": stringSchema, "shared_set_id": stringSchema,
			"resource_name": stringSchema,
			"campaign_shared_set": object(schema{"resource_name": stringSchema, "campaign": stringSchema,
				"shared_set": stringSchema, "status": stringSchema}, "resource_name")},
			"campaign_id", "shared_set_id", "resource_name"))}, "customer_id", "validate_only", "results")
	linkSchema = object(schema{"campaign_id": numericIDSchema, "shared_set_id": numericIDSchema},
		"campaign_id", "shared_set_id")
)

// The notes end the descriptions of the changing tools. The quota is counted per operation of the list.
const (
	sharedSetsChangeNote = mutateListNote
	sharedSetsListedNote = mutateListListedNote
)

const sharedSetsSensitivity = "shared sets of a Google Ads account, their names and the campaigns that use them, " +
	"which the confirmation names; no data of other people"

const sharedSetsNote = " A shared set is a list that campaigns of the account use together, such as negative " +
	"keywords or placements; this tool does not read shared sets (read them with reports.search on the " +
	"shared_set resource) and does not change the criteria in them. The request goes to " +
	"SharedSetService.MutateSharedSets with up to 50 operations, atomic and without partial_failure: Google " +
	"applies all of them or none. A manager account has shared sets of its own, which a manager target that " +
	"names it reaches."

const campaignSharedSetsNote = " A campaign shared set attaches a shared set to a campaign, so that the campaign " +
	"uses the list; this tool does not read links (read them with reports.search on the campaign_shared_set " +
	"resource) and does not change shared sets or their criteria. Only negative keyword shared sets can be " +
	"used with Shopping campaigns, and only negative placement shared sets with Display mobile app campaigns. " +
	"The request goes to CampaignSharedSetService.MutateCampaignSharedSets with up to 50 operations, atomic and " +
	"without partial_failure: Google applies all of them or none."

var sharedSetsFields = []capability.Field{
	{Name: "customer_id", Description: "The account of the call"},
	{Name: "validate_only", Description: "true when Google only validated the change"},
	{Name: "results", Description: "Per operation, in the order of the arguments: shared_set_id, resource_name " +
		"(customers/ID/sharedSets/SHARED_SET_ID, as Google answered, always of the account of the call), and with " +
		"MUTABLE_RESOURCE the shared set after the change (id, name, status, type, vertical_ads_item_vertical_type, " +
		"member_count, reference_count) as Google returns it. Empty after validate_only"},
}

var campaignSharedSetsFields = []capability.Field{
	{Name: "customer_id", Description: "The account of the call"},
	{Name: "validate_only", Description: "true when Google only validated the change"},
	{Name: "results", Description: "Per operation, in the order of the arguments: campaign_id, shared_set_id, " +
		"resource_name (customers/ID/campaignSharedSets/CAMPAIGN_ID~SHARED_SET_ID, as Google answered, always of " +
		"the account of the call and the pair asked for), and with MUTABLE_RESOURCE (create only) the link after the " +
		"change (campaign, shared_set, status) as Google returns it. Empty after validate_only"},
}

var sharedSetsArgumentDocs = []capability.Argument{
	{Name: "customer_id", Required: true, Description: "Ten-digit customer ID of the account without hyphens; a " +
		"target of the connection or an account below a manager target. A manager account needs a manager " +
		"target that names it"},
}

const sharedSetTypeDoc = "type (required, immutable; NEGATIVE_KEYWORDS, NEGATIVE_PLACEMENTS, BRANDS, RETAIL_FILTER, " +
	"VERTICAL_ADS_ITEM_GROUP_RULE_LIST or ACCOUNT_LEVEL_NEGATIVE_KEYWORDS; WEBPAGES is not publicly available)"

var sharedSetsCreate = capability.Descriptor{
	ID: sharedSetsCreateID, Version: 1, Title: "Create shared sets in a Google Ads account",
	Description: "Create one or more shared sets in a Google Ads account: a name of 1 to 255 bytes and a type, " +
		"which cannot be changed afterwards; the type VERTICAL_ADS_ITEM_GROUP_RULE_LIST also needs the vertical " +
		"(hotels, flights, events, rental cars, things to do, vacation rentals). Google decides which types an " +
		"account may create and enforces that a name is unique among the active shared sets of the same type." +
		sharedSetsNote + " Offered by a connection with the create effect or whose tools list names it; no " +
		"profile holds it." + sharedSetsChangeNote,
	Tags: []string{"google", "ads", "shared-sets", "create"}, Provider: Provider,
	Risk: changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent, sharedSetsSensitivity),
	InputSchema: encoded(object(schema{
		"customer_id": customerIDArgument,
		"shared_sets": schema{"type": "array", "minItems": 1, "maxItems": sharedSetsMaxOperations,
			"items": object(schema{"name": sharedSetNameSchema, "type": schema{"type": "string", "enum": sharedSetTypes},
				"vertical_ads_item_vertical_type": schema{"type": "string", "enum": sharedSetVerticalTypes}},
				"name", "type")},
		"validate_only": validateOnlySchema, "response_content_type": responseContentTypeSchema,
	}, "customer_id", "shared_sets")),
	OutputSchema: encoded(sharedSetsOutputSchema),
	Arguments: append(slices.Clone(sharedSetsArgumentDocs),
		capability.Argument{Name: "shared_sets", Required: true, Description: "1 to 50 shared sets, each with name " +
			"(required, 1 to 255 bytes, no control characters), " + sharedSetTypeDoc + " and " +
			"vertical_ads_item_vertical_type (HOTELS, FLIGHTS, EVENTS, RENTAL_CARS, THINGS_TO_DO or " +
			"VACATION_RENTALS; required for VERTICAL_ADS_ITEM_GROUP_RULE_LIST and refused for any other type)"},
		validateOnlyArgument, responseContentTypeArgument),
	Fields: sharedSetsFields,
	Examples: []capability.Example{{Description: "Create a negative keyword list",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","shared_sets":[{"name":"Brand terms","type":"NEGATIVE_KEYWORDS"}]}`)}},
}

var sharedSetsUpdate = capability.Descriptor{
	ID: sharedSetsUpdateID, Version: 1, Title: "Rename shared sets of a Google Ads account",
	Description: "Rename existing shared sets of a Google Ads account (an update with the field mask name). The " +
		"name is the only field of a shared set that can be changed: its type and vertical are immutable and its " +
		"status, member count, and reference count are output only. Only the shared_set_id selects a shared set: " +
		"its resource name is built from customer_id and shared_set_id." + sharedSetsNote +
		" Offered by a connection with the update effect or whose tools list names it; no profile holds it." +
		sharedSetsChangeNote,
	Tags: []string{"google", "ads", "shared-sets", "update"}, Provider: Provider,
	Risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, sharedSetsSensitivity),
	InputSchema: encoded(object(schema{
		"customer_id": customerIDArgument,
		"shared_sets": schema{"type": "array", "minItems": 1, "maxItems": sharedSetsMaxOperations,
			"items": object(schema{"shared_set_id": numericIDSchema, "name": sharedSetNameSchema},
				"shared_set_id", "name")},
		"validate_only": validateOnlySchema, "response_content_type": responseContentTypeSchema,
	}, "customer_id", "shared_sets")),
	OutputSchema: encoded(sharedSetsOutputSchema),
	Arguments: append(slices.Clone(sharedSetsArgumentDocs),
		capability.Argument{Name: "shared_sets", Required: true, Description: "1 to 50 entries, each shared_set_id once, " +
			"with the new name (1 to 255 bytes, no control characters). shared_set_id is the id of the shared set as " +
			"reports.search returns it"},
		validateOnlyArgument, responseContentTypeArgument),
	Fields: sharedSetsFields,
	Examples: []capability.Example{{Description: "Rename a shared set",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","shared_sets":[{"shared_set_id":"112233","name":"Archive"}]}`)}},
}

var sharedSetsRemove = capability.Descriptor{
	ID: sharedSetsRemoveID, Version: 1, Title: "Remove shared sets from a Google Ads account",
	Description: "Remove existing shared sets of a Google Ads account (a remove of customers/ID/sharedSets/ID, " +
		"built from customer_id and shared_set_id). Google marks a removed shared set REMOVED; no tool of this " +
		"provider brings it back. Whether Google refuses the removal of a shared set that campaigns still use is " +
		"Google's to say; check reference_count with reports.search first." + sharedSetsNote + sharedSetsListedNote,
	Tags: []string{"google", "ads", "shared-sets", "remove"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: changeRisk(capability.EffectDelete, capability.IdempotencyIdempotent, sharedSetsSensitivity),
	InputSchema: encoded(object(schema{
		"customer_id": customerIDArgument,
		"shared_set_ids": schema{"type": "array", "minItems": 1, "maxItems": sharedSetsMaxOperations,
			"uniqueItems": true, "items": numericIDSchema},
		"validate_only": validateOnlySchema,
	}, "customer_id", "shared_set_ids")),
	OutputSchema: encoded(sharedSetsOutputSchema),
	Arguments: append(slices.Clone(sharedSetsArgumentDocs),
		capability.Argument{Name: "shared_set_ids", Required: true, Description: "1 to 50 IDs of existing shared sets, " +
			"each once, as reports.search returns them"},
		validateOnlyArgument),
	Fields: sharedSetsFields,
	Examples: []capability.Example{{Description: "Validate the removal of a shared set without making it",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","shared_set_ids":["112233"],"validate_only":true}`)}},
}

var campaignSharedSetsCreate = capability.Descriptor{
	ID: campaignSharedSetsCreateID, Version: 1, Title: "Attach shared sets to campaigns of a Google Ads account",
	Description: "Attach existing shared sets to existing campaigns of a Google Ads account, one pair of campaign " +
		"and shared set per entry. The resource names of both are built from customer_id, campaign_id and " +
		"shared_set_id; a campaign and a shared set of another account cannot be named." + campaignSharedSetsNote +
		" Offered by a connection with the create effect or whose tools list names it; no profile holds it." +
		sharedSetsChangeNote,
	Tags: []string{"google", "ads", "campaign-shared-sets", "create"}, Provider: Provider,
	Risk: changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent, sharedSetsSensitivity),
	InputSchema: encoded(object(schema{
		"customer_id": customerIDArgument,
		"links": schema{"type": "array", "minItems": 1, "maxItems": sharedSetsMaxOperations,
			"items": linkSchema},
		"validate_only": validateOnlySchema, "response_content_type": responseContentTypeSchema,
	}, "customer_id", "links")),
	OutputSchema: encoded(campaignSharedSetsOutputSchema),
	Arguments: append(slices.Clone(sharedSetsArgumentDocs),
		capability.Argument{Name: "links", Required: true, Description: "1 to 50 pairs, each pair once, of " +
			"campaign_id (the id of an existing campaign) and shared_set_id (the id of an existing shared set), as " +
			"reports.search returns them"},
		validateOnlyArgument, responseContentTypeArgument),
	Fields: campaignSharedSetsFields,
	Examples: []capability.Example{{Description: "Attach a shared set to a campaign",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","links":[{"campaign_id":"445566","shared_set_id":"112233"}]}`)}},
}

var campaignSharedSetsRemove = capability.Descriptor{
	ID: campaignSharedSetsRemoveID, Version: 1, Title: "Detach shared sets from campaigns of a Google Ads account",
	Description: "Detach shared sets from campaigns of a Google Ads account, one pair of campaign and shared set " +
		"per entry (a remove of customers/ID/campaignSharedSets/CAMPAIGN_ID~SHARED_SET_ID, built from customer_id, " +
		"campaign_id and shared_set_id). The campaign stops using the list; the shared set stays. Google marks a " +
		"removed link REMOVED." + campaignSharedSetsNote + sharedSetsListedNote,
	Tags: []string{"google", "ads", "campaign-shared-sets", "remove"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: changeRisk(capability.EffectDelete, capability.IdempotencyIdempotent, sharedSetsSensitivity),
	InputSchema: encoded(object(schema{
		"customer_id": customerIDArgument,
		"links": schema{"type": "array", "minItems": 1, "maxItems": sharedSetsMaxOperations,
			"items": linkSchema},
		"validate_only": validateOnlySchema,
	}, "customer_id", "links")),
	OutputSchema: encoded(campaignSharedSetsOutputSchema),
	Arguments: append(slices.Clone(sharedSetsArgumentDocs),
		capability.Argument{Name: "links", Required: true, Description: "1 to 50 pairs, each pair once, of " +
			"campaign_id and shared_set_id of an existing link, as reports.search returns them"},
		validateOnlyArgument),
	Fields: campaignSharedSetsFields,
	Examples: []capability.Example{{Description: "Validate the detaching of a shared set without making it",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","links":[{"campaign_id":"445566","shared_set_id":"112233"}],"validate_only":true}`)}},
}

// --- handlers ---

func handleSharedSetsCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "create shared sets"
	var args sharedSetsCreateArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	ops, err := args.operations()
	if err != nil {
		return nil, err
	}
	return sharedSetsSend(ctx, resolved, secrets, red, op, sharedSetsCreateID, args.CustomerID, ops, nil,
		args.ValidateOnly, args.ResponseContentType)
}

func handleSharedSetsUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "update shared sets"
	var args sharedSetsUpdateArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	ops, names, err := args.operations()
	if err != nil {
		return nil, err
	}
	return sharedSetsSend(ctx, resolved, secrets, red, op, sharedSetsUpdateID, args.CustomerID, ops, names,
		args.ValidateOnly, args.ResponseContentType)
}

func handleSharedSetsRemove(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "remove shared sets"
	var args sharedSetsRemoveArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	ops, names, err := args.operations()
	if err != nil {
		return nil, err
	}
	return sharedSetsSend(ctx, resolved, secrets, red, op, sharedSetsRemoveID, args.CustomerID, ops, names,
		args.ValidateOnly, "")
}

func sharedSetsSend(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	op, tool, customerID string, ops []any, want []string, validateOnly bool, content string) (any, error) {
	env, err := handleEnv(ctx, resolved, secrets, red, op, tool)
	if err != nil {
		return nil, err
	}
	c, err := env.settle(customerID, true)
	if err != nil {
		return nil, err
	}
	results, err := c.mutateList(ctx, listMutation{op: op, tool: tool, customerID: customerID,
		service: sharedSetsService, collection: sharedSetsCollection, resourceKey: "sharedSet", operations: ops,
		names: want, validateOnly: validateOnly, content: content})
	if err != nil {
		return nil, err
	}
	mutated, err := sharedSetsResults(op, results)
	if err != nil {
		return nil, err
	}
	return sharedSetsChanged{CustomerID: customerID, ValidateOnly: validateOnly, Results: mutated}, nil
}

func handleCampaignSharedSetsCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "attach shared sets to campaigns"
	var args campaignSharedSetsCreateArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	ops, names, err := args.operations()
	if err != nil {
		return nil, err
	}
	return campaignSharedSetsSend(ctx, resolved, secrets, red, op, campaignSharedSetsCreateID, args.CustomerID, ops,
		names, args.ValidateOnly, args.ResponseContentType)
}

func handleCampaignSharedSetsRemove(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "detach shared sets from campaigns"
	var args campaignSharedSetsRemoveArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	ops, names, err := args.operations()
	if err != nil {
		return nil, err
	}
	return campaignSharedSetsSend(ctx, resolved, secrets, red, op, campaignSharedSetsRemoveID, args.CustomerID, ops,
		names, args.ValidateOnly, "")
}

func campaignSharedSetsSend(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, op, tool, customerID string, ops []any, want []string, validateOnly bool,
	content string) (any, error) {
	env, err := handleEnv(ctx, resolved, secrets, red, op, tool)
	if err != nil {
		return nil, err
	}
	c, err := env.settle(customerID, true)
	if err != nil {
		return nil, err
	}
	var answer struct {
		Results             []map[string]json.RawMessage `json:"results"`
		PartialFailureError json.RawMessage              `json:"partialFailureError"`
	}
	if err := c.mutate(ctx, mutation{op: op, tool: tool, customerID: customerID, service: campaignSharedSetsService,
		body: mutateOperationsBody(ops, validateOnly, content)}, &answer); err != nil {
		return nil, err
	}
	if len(answer.PartialFailureError) > 0 && string(answer.PartialFailureError) != "null" {
		return nil, unsureResult(op, "answered with a partial failure, which this request did not ask for")
	}
	results, err := campaignSharedSetsResults(op, customerID, answer.Results, want, validateOnly)
	if err != nil {
		return nil, err
	}
	return campaignSharedSetsChanged{CustomerID: customerID, ValidateOnly: validateOnly, Results: results}, nil
}

// --- confirmation ---

func sharedSetsQuote(value string) string {
	return strconv.Quote(boundedText(value, sharedSetNameMax*4))
}

func sharedSetsLead(validateOnly bool) string {
	if validateOnly {
		return "Only validate: "
	}
	return ""
}

func sharedSetsMore(total, shown int) string {
	if total > shown {
		return " and " + strconv.Itoa(total-shown) + " more"
	}
	return ""
}

func sharedSetsCreateSummary(raw json.RawMessage) string {
	var a sharedSetsCreateArguments
	_ = json.Unmarshal(raw, &a)
	var parts []string
	for _, s := range a.SharedSets[:min(len(a.SharedSets), 10)] {
		part := sharedSetsQuote(s.Name) + " (" + boundedText(s.Type, maxCodeText)
		if s.VerticalType != "" {
			part += " " + boundedText(s.VerticalType, maxCodeText)
		}
		parts = append(parts, part+")")
	}
	return sharedSetsLead(a.ValidateOnly) + "Create " + strconv.Itoa(len(a.SharedSets)) + " shared set(s) in Google Ads account " +
		a.CustomerID + ": " + strings.Join(parts, ", ") + sharedSetsMore(len(a.SharedSets), len(parts)) + "."
}

func sharedSetsUpdateSummary(raw json.RawMessage) string {
	var a sharedSetsUpdateArguments
	_ = json.Unmarshal(raw, &a)
	var parts []string
	for _, s := range a.SharedSets[:min(len(a.SharedSets), 10)] {
		parts = append(parts, "shared set "+boundedText(s.SharedSetID, maxCodeText)+" name "+sharedSetsQuote(s.Name))
	}
	return sharedSetsLead(a.ValidateOnly) + "Rename " + strconv.Itoa(len(a.SharedSets)) + " shared set(s) of Google Ads account " +
		a.CustomerID + ": " + strings.Join(parts, "; ") + sharedSetsMore(len(a.SharedSets), len(parts)) + "."
}

func sharedSetsRemoveSummary(raw json.RawMessage) string {
	var a sharedSetsRemoveArguments
	_ = json.Unmarshal(raw, &a)
	shown := make([]string, 0, 10)
	for _, id := range a.SharedSetIDs[:min(len(a.SharedSetIDs), 10)] {
		shown = append(shown, boundedText(id, maxCodeText))
	}
	return sharedSetsLead(a.ValidateOnly) + "Remove " + strconv.Itoa(len(a.SharedSetIDs)) + " shared set(s) from Google Ads account " +
		a.CustomerID + ": " + strings.Join(shown, ", ") + sharedSetsMore(len(a.SharedSetIDs), len(shown)) +
		". Google marks them removed; no tool brings them back."
}

func campaignSharedSetsPairs(links []campaignSharedSetLink) ([]string, int) {
	parts := make([]string, 0, 10)
	for _, l := range links[:min(len(links), 10)] {
		parts = append(parts, "shared set "+boundedText(l.SharedSetID, maxCodeText)+" and campaign "+
			boundedText(l.CampaignID, maxCodeText))
	}
	return parts, len(links)
}

func campaignSharedSetsCreateSummary(raw json.RawMessage) string {
	var a campaignSharedSetsCreateArguments
	_ = json.Unmarshal(raw, &a)
	parts, total := campaignSharedSetsPairs(a.Links)
	return sharedSetsLead(a.ValidateOnly) + "Attach " + strconv.Itoa(total) + " shared set(s) to campaigns of Google Ads account " +
		a.CustomerID + ": " + strings.Join(parts, "; ") + sharedSetsMore(total, len(parts)) + "."
}

func campaignSharedSetsRemoveSummary(raw json.RawMessage) string {
	var a campaignSharedSetsRemoveArguments
	_ = json.Unmarshal(raw, &a)
	parts, total := campaignSharedSetsPairs(a.Links)
	return sharedSetsLead(a.ValidateOnly) + "Detach " + strconv.Itoa(total) + " shared set(s) from campaigns of Google Ads account " +
		a.CustomerID + ": " + strings.Join(parts, "; ") + sharedSetsMore(total, len(parts)) + "."
}
