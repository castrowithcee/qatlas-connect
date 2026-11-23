package googleads

import (
	"context"
	"encoding/json"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	biddingDataExclusionsCreateID = Provider + ".bidding-data-exclusions.create"
	biddingDataExclusionsUpdateID = Provider + ".bidding-data-exclusions.update"
	biddingDataExclusionsRemoveID = Provider + ".bidding-data-exclusions.remove"

	biddingDataExclusionsService = "/biddingDataExclusions"
)

// Bounds as the v26 reference of BiddingDataExclusion documents them: name at most 255 characters, description at
// most 2048, at most 2000 campaigns per exclusion, and an interval [start_date_time, end_date_time) of more than
// zero and at most 14 days.
const (
	bdeNameMax        = 255
	bdeDescriptionMax = 2048
	bdeCampaignsMax   = 2000
	bdeMaxDuration    = 14 * 24 * time.Hour
	// bdeMaxOperations is Qatlas's own bound per call; Google accepts up to 10,000 operations per request.
	bdeMaxOperations = 10

	bdeTimeLayout = "2006-01-02 15:04:05"
)

var (
	bdeIDPattern   = regexp.MustCompile(`^[1-9][0-9]{0,18}$`)
	bdeTimePattern = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2}$`)

	bdeScopes   = []string{"CUSTOMER", "CAMPAIGN", "CHANNEL"}
	bdeDevices  = []string{"MOBILE", "TABLET", "DESKTOP", "CONNECTED_TV", "OTHER"}
	bdeChannels = []string{"DISPLAY", "SEARCH", "SHOPPING"}
)

func bdeResourceName(customerID, id string) string {
	return customerName(customerID) + "/biddingDataExclusions/" + id
}

func bdeCampaignName(customerID, id string) string {
	return customerName(customerID) + "/campaigns/" + id
}

// bdeIDOf returns the number of a resource name of the form prefix + N that belongs to the account.
func bdeIDOf(name, prefix string) (string, bool) {
	rest, ok := strings.CutPrefix(name, prefix)
	if !ok || !bdeIDPattern.MatchString(rest) {
		return "", false
	}
	if _, err := strconv.ParseInt(rest, 10, 64); err != nil {
		return "", false
	}
	return rest, true
}

func bdeCheckID(argument, value, what string) error {
	if _, err := strconv.ParseInt(value, 10, 64); err != nil || !bdeIDPattern.MatchString(value) {
		return badRequest(argument + " must be the ID of " + what + ", a positive number")
	}
	return nil
}

func bdeCheckCount(argument string, n int) error {
	if n < 1 || n > bdeMaxOperations {
		return badRequest(argument + " must hold 1 to " + strconv.Itoa(bdeMaxOperations) + " entries")
	}
	return nil
}

func bdeCheckEnum(argument, value string, allowed []string) error {
	if !slices.Contains(allowed, value) {
		return badRequest(argument + " must be one of " + strings.Join(allowed, ", "))
	}
	return nil
}

func bdeCheckEnums(argument string, values []string, allowed []string, max int) error {
	if len(values) > max {
		return badRequest(argument + " must hold at most " + strconv.Itoa(max) + " entries")
	}
	for i, v := range values {
		if err := bdeCheckEnum(argument, v, allowed); err != nil {
			return err
		}
		if slices.Contains(values[:i], v) {
			return badRequest(argument + " names each value at most once")
		}
	}
	return nil
}

func bdeCheckTime(argument, value string) (time.Time, error) {
	if !bdeTimePattern.MatchString(value) {
		return time.Time{}, badRequest(argument + " must be a date and time as yyyy-MM-dd HH:mm:ss")
	}
	t, err := time.Parse(bdeTimeLayout, value)
	if err != nil {
		return time.Time{}, badRequest(argument + " must be a real date and time as yyyy-MM-dd HH:mm:ss")
	}
	return t, nil
}

// bdeCheckInterval checks the given times; with both, the interval must be longer than zero and at most 14 days.
func bdeCheckInterval(start, end *string) error {
	var s, e time.Time
	var err error
	if start != nil {
		if s, err = bdeCheckTime("start_date_time", *start); err != nil {
			return err
		}
	}
	if end != nil {
		if e, err = bdeCheckTime("end_date_time", *end); err != nil {
			return err
		}
	}
	if start != nil && end != nil {
		if d := e.Sub(s); d <= 0 || d > bdeMaxDuration {
			return badRequest("the interval from start_date_time to end_date_time must be longer than zero and at most 14 days")
		}
	}
	return nil
}

func bdeCheckText(name, description *string) error {
	if name != nil {
		if err := labelsText("name", *name, 1, bdeNameMax); err != nil {
			return err
		}
	}
	if description != nil {
		return labelsText("description", *description, 0, bdeDescriptionMax)
	}
	return nil
}

// bdeCheckCampaigns checks the campaign IDs and returns their resource names, built from the checked account.
func bdeCheckCampaigns(customerID string, ids []string) ([]string, error) {
	if len(ids) > bdeCampaignsMax {
		return nil, badRequest("campaign_ids must hold at most " + strconv.Itoa(bdeCampaignsMax) + " entries")
	}
	names := make([]string, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if err := bdeCheckID("campaign_ids", id, "a campaign of the account"); err != nil {
			return nil, err
		}
		if seen[id] {
			return nil, badRequest("campaign_ids names each campaign at most once")
		}
		seen[id] = true
		names = append(names, bdeCampaignName(customerID, id))
	}
	return names, nil
}

// bdeCheckTargets checks how scope, campaigns and channel types go together. Google documents that campaigns apply
// to the scope CAMPAIGN, channel types to the scope CHANNEL, and that both together are not supported. A create
// needs the list its scope names; an update may leave it to what the exclusion already holds.
func bdeCheckTargets(scope *string, campaigns, channels int, create bool) error {
	if campaigns > 0 && channels > 0 {
		return badRequest("campaign_ids and advertising_channel_types cannot be given together")
	}
	if scope == nil {
		return nil
	}
	switch *scope {
	case "CUSTOMER":
		if campaigns > 0 || channels > 0 {
			return badRequest("scope CUSTOMER takes neither campaign_ids nor advertising_channel_types")
		}
	case "CAMPAIGN":
		if channels > 0 {
			return badRequest("scope CAMPAIGN takes campaign_ids, not advertising_channel_types")
		}
		if create && campaigns == 0 {
			return badRequest("scope CAMPAIGN needs campaign_ids")
		}
	case "CHANNEL":
		if campaigns > 0 {
			return badRequest("scope CHANNEL takes advertising_channel_types, not campaign_ids")
		}
		if create && channels == 0 {
			return badRequest("scope CHANNEL needs advertising_channel_types")
		}
	}
	return nil
}

// --- arguments ---

type (
	bdeNewExclusion struct {
		Scope                   string   `json:"scope"`
		StartDateTime           string   `json:"start_date_time"`
		EndDateTime             string   `json:"end_date_time"`
		Name                    *string  `json:"name"`
		Description             *string  `json:"description"`
		Devices                 []string `json:"devices"`
		CampaignIDs             []string `json:"campaign_ids"`
		AdvertisingChannelTypes []string `json:"advertising_channel_types"`
	}
	bdeEditExclusion struct {
		DataExclusionID         string    `json:"data_exclusion_id"`
		Scope                   *string   `json:"scope"`
		StartDateTime           *string   `json:"start_date_time"`
		EndDateTime             *string   `json:"end_date_time"`
		Name                    *string   `json:"name"`
		Description             *string   `json:"description"`
		Devices                 *[]string `json:"devices"`
		CampaignIDs             *[]string `json:"campaign_ids"`
		AdvertisingChannelTypes *[]string `json:"advertising_channel_types"`
	}
	bdeCreateArguments struct {
		CustomerID          string            `json:"customer_id"`
		DataExclusions      []bdeNewExclusion `json:"data_exclusions"`
		ValidateOnly        bool              `json:"validate_only"`
		ResponseContentType string            `json:"response_content_type"`
	}
	bdeUpdateArguments struct {
		CustomerID          string             `json:"customer_id"`
		DataExclusions      []bdeEditExclusion `json:"data_exclusions"`
		ValidateOnly        bool               `json:"validate_only"`
		ResponseContentType string             `json:"response_content_type"`
	}
	bdeRemoveArguments struct {
		CustomerID       string   `json:"customer_id"`
		DataExclusionIDs []string `json:"data_exclusion_ids"`
		ValidateOnly     bool     `json:"validate_only"`
	}
)

func bdeStrings(values []string) []any {
	out := make([]any, 0, len(values))
	for _, v := range values {
		out = append(out, v)
	}
	return out
}

func (a bdeCreateArguments) operations() ([]any, error) {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := checkContentType(a.ResponseContentType); err != nil {
		return nil, err
	}
	if err := bdeCheckCount("data_exclusions", len(a.DataExclusions)); err != nil {
		return nil, err
	}
	ops := make([]any, 0, len(a.DataExclusions))
	for _, x := range a.DataExclusions {
		if err := bdeCheckEnum("scope", x.Scope, bdeScopes); err != nil {
			return nil, err
		}
		if err := bdeCheckInterval(&x.StartDateTime, &x.EndDateTime); err != nil {
			return nil, err
		}
		if err := bdeCheckText(x.Name, x.Description); err != nil {
			return nil, err
		}
		if err := bdeCheckEnums("devices", x.Devices, bdeDevices, len(bdeDevices)); err != nil {
			return nil, err
		}
		if err := bdeCheckEnums("advertising_channel_types", x.AdvertisingChannelTypes, bdeChannels, len(bdeChannels)); err != nil {
			return nil, err
		}
		campaigns, err := bdeCheckCampaigns(a.CustomerID, x.CampaignIDs)
		if err != nil {
			return nil, err
		}
		if err := bdeCheckTargets(&x.Scope, len(campaigns), len(x.AdvertisingChannelTypes), true); err != nil {
			return nil, err
		}
		create := map[string]any{"scope": x.Scope, "startDateTime": x.StartDateTime, "endDateTime": x.EndDateTime}
		if x.Name != nil {
			create["name"] = *x.Name
		}
		if x.Description != nil {
			create["description"] = *x.Description
		}
		if len(x.Devices) > 0 {
			create["devices"] = bdeStrings(x.Devices)
		}
		if len(campaigns) > 0 {
			create["campaigns"] = bdeStrings(campaigns)
		}
		if len(x.AdvertisingChannelTypes) > 0 {
			create["advertisingChannelTypes"] = bdeStrings(x.AdvertisingChannelTypes)
		}
		ops = append(ops, map[string]any{"create": create})
	}
	return ops, nil
}

func (a bdeUpdateArguments) operations() ([]any, []string, error) {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return nil, nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := checkContentType(a.ResponseContentType); err != nil {
		return nil, nil, err
	}
	if err := bdeCheckCount("data_exclusions", len(a.DataExclusions)); err != nil {
		return nil, nil, err
	}
	ops := make([]any, 0, len(a.DataExclusions))
	names := make([]string, 0, len(a.DataExclusions))
	for _, x := range a.DataExclusions {
		if err := bdeCheckID("data_exclusion_id", x.DataExclusionID, "an existing data exclusion"); err != nil {
			return nil, nil, err
		}
		name := bdeResourceName(a.CustomerID, x.DataExclusionID)
		if slices.Contains(names, name) {
			return nil, nil, badRequest("data_exclusions names each data_exclusion_id at most once")
		}
		if x.Scope == nil && x.StartDateTime == nil && x.EndDateTime == nil && x.Name == nil && x.Description == nil &&
			x.Devices == nil && x.CampaignIDs == nil && x.AdvertisingChannelTypes == nil {
			return nil, nil, badRequest("each entry of data_exclusions needs at least one field besides data_exclusion_id")
		}
		if x.Scope != nil {
			if err := bdeCheckEnum("scope", *x.Scope, bdeScopes); err != nil {
				return nil, nil, err
			}
		}
		if err := bdeCheckInterval(x.StartDateTime, x.EndDateTime); err != nil {
			return nil, nil, err
		}
		if err := bdeCheckText(x.Name, x.Description); err != nil {
			return nil, nil, err
		}
		update := map[string]any{"resourceName": name}
		var mask []string
		set := func(field string, value any) {
			update[field] = value
			mask = append(mask, field)
		}
		if x.Scope != nil {
			set("scope", *x.Scope)
		}
		if x.StartDateTime != nil {
			set("startDateTime", *x.StartDateTime)
		}
		if x.EndDateTime != nil {
			set("endDateTime", *x.EndDateTime)
		}
		if x.Name != nil {
			set("name", *x.Name)
		}
		if x.Description != nil {
			set("description", *x.Description)
		}
		if x.Devices != nil {
			if err := bdeCheckEnums("devices", *x.Devices, bdeDevices, len(bdeDevices)); err != nil {
				return nil, nil, err
			}
			set("devices", bdeStrings(*x.Devices))
		}
		campaigns, channels := 0, 0
		if x.CampaignIDs != nil {
			list, err := bdeCheckCampaigns(a.CustomerID, *x.CampaignIDs)
			if err != nil {
				return nil, nil, err
			}
			if len(list) == 0 {
				return nil, nil, badRequest("campaign_ids must hold 1 to " + strconv.Itoa(bdeCampaignsMax) + " entries when given")
			}
			campaigns = len(list)
			set("campaigns", bdeStrings(list))
		}
		if x.AdvertisingChannelTypes != nil {
			if len(*x.AdvertisingChannelTypes) == 0 {
				return nil, nil, badRequest("advertising_channel_types must hold 1 to 3 entries when given")
			}
			if err := bdeCheckEnums("advertising_channel_types", *x.AdvertisingChannelTypes, bdeChannels, len(bdeChannels)); err != nil {
				return nil, nil, err
			}
			channels = len(*x.AdvertisingChannelTypes)
			set("advertisingChannelTypes", bdeStrings(*x.AdvertisingChannelTypes))
		}
		if err := bdeCheckTargets(x.Scope, campaigns, channels, false); err != nil {
			return nil, nil, err
		}
		names = append(names, name)
		ops = append(ops, map[string]any{"update": update, "updateMask": strings.Join(mask, ",")})
	}
	return ops, names, nil
}

func (a bdeRemoveArguments) operations() ([]any, []string, error) {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return nil, nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := bdeCheckCount("data_exclusion_ids", len(a.DataExclusionIDs)); err != nil {
		return nil, nil, err
	}
	ops := make([]any, 0, len(a.DataExclusionIDs))
	names := make([]string, 0, len(a.DataExclusionIDs))
	for _, id := range a.DataExclusionIDs {
		if err := bdeCheckID("data_exclusion_ids", id, "an existing data exclusion"); err != nil {
			return nil, nil, err
		}
		name := bdeResourceName(a.CustomerID, id)
		if slices.Contains(names, name) {
			return nil, nil, badRequest("data_exclusion_ids names each data exclusion at most once")
		}
		names = append(names, name)
		ops = append(ops, map[string]any{"remove": name})
	}
	return ops, names, nil
}

// --- results ---

type (
	bdeExclusionResult struct {
		ResourceName            string   `json:"resource_name"`
		ID                      string   `json:"id,omitempty"`
		Scope                   string   `json:"scope,omitempty"`
		Status                  string   `json:"status,omitempty"`
		StartDateTime           string   `json:"start_date_time,omitempty"`
		EndDateTime             string   `json:"end_date_time,omitempty"`
		Name                    string   `json:"name,omitempty"`
		Description             string   `json:"description,omitempty"`
		Devices                 []string `json:"devices,omitempty"`
		CampaignIDs             []string `json:"campaign_ids,omitempty"`
		AdvertisingChannelTypes []string `json:"advertising_channel_types,omitempty"`
	}
	bdeMutated struct {
		DataExclusionID string              `json:"data_exclusion_id"`
		ResourceName    string              `json:"resource_name"`
		DataExclusion   *bdeExclusionResult `json:"data_exclusion,omitempty"`
	}
	bdeChanged struct {
		CustomerID   string       `json:"customer_id"`
		ValidateOnly bool         `json:"validate_only"`
		Results      []bdeMutated `json:"results"`
	}
	bdeAnswer struct {
		Results []struct {
			ResourceName         string          `json:"resourceName"`
			BiddingDataExclusion json.RawMessage `json:"biddingDataExclusion"`
		} `json:"results"`
		PartialFailureError json.RawMessage `json:"partialFailureError"`
	}
	bdeWire struct {
		ResourceName            string   `json:"resourceName"`
		DataExclusionID         *wireInt `json:"dataExclusionId"`
		Scope                   string   `json:"scope"`
		Status                  string   `json:"status"`
		StartDateTime           string   `json:"startDateTime"`
		EndDateTime             string   `json:"endDateTime"`
		Name                    string   `json:"name"`
		Description             string   `json:"description"`
		Devices                 []string `json:"devices"`
		Campaigns               []string `json:"campaigns"`
		AdvertisingChannelTypes []string `json:"advertisingChannelTypes"`
	}
)

func bdeExclusionOf(raw json.RawMessage, customerID, name, id string) (*bdeExclusionResult, bool) {
	var w bdeWire
	if json.Unmarshal(raw, &w) != nil || w.ResourceName != name || len(w.Campaigns) > bdeCampaignsMax {
		return nil, false
	}
	out := &bdeExclusionResult{ResourceName: name, Scope: code(w.Scope), Status: code(w.Status),
		StartDateTime: code(w.StartDateTime), EndDateTime: code(w.EndDateTime),
		Name: boundedText(w.Name, maxText), Description: boundedText(w.Description, maxLongText),
		Devices: boundedList(w.Devices, maxCodeText), AdvertisingChannelTypes: boundedList(w.AdvertisingChannelTypes, maxCodeText)}
	if w.DataExclusionID != nil {
		if string(*w.DataExclusionID) != id {
			return nil, false
		}
		out.ID = id
	}
	prefix := customerName(customerID) + "/campaigns/"
	for _, c := range w.Campaigns {
		campaignID, ok := bdeIDOf(c, prefix)
		if !ok {
			return nil, false
		}
		out.CampaignIDs = append(out.CampaignIDs, campaignID)
	}
	return out, true
}

// bdeResults binds the answer to the checked account. With want names (update, remove) the results must be exactly
// these in order; a create names the new exclusions, which must belong to the account and be distinct. Everything
// else leaves the outcome open, since the request was sent.
func bdeResults(op, customerID string, answer bdeAnswer, count int, want []string, validateOnly bool) ([]bdeMutated, error) {
	if len(answer.PartialFailureError) > 0 && string(answer.PartialFailureError) != "null" {
		return nil, unsureResult(op, "answered with a partial failure although the request asked for none")
	}
	out := []bdeMutated{}
	if validateOnly && len(answer.Results) == 0 {
		return out, nil
	}
	if len(answer.Results) != count {
		return nil, unsureResult(op, "answered with another number of results than the operations asked for")
	}
	prefix := customerName(customerID) + "/biddingDataExclusions/"
	for i, r := range answer.Results {
		id, ok := bdeIDOf(r.ResourceName, prefix)
		if want != nil && r.ResourceName != want[i] || !ok {
			return nil, unsureResult(op, "answered with a result for another data exclusion than the one asked for")
		}
		for _, previous := range out {
			if previous.DataExclusionID == id {
				return nil, unsureResult(op, "answered with the same data exclusion twice")
			}
		}
		item := bdeMutated{DataExclusionID: id, ResourceName: r.ResourceName}
		if len(r.BiddingDataExclusion) > 0 && string(r.BiddingDataExclusion) != "null" {
			x, ok := bdeExclusionOf(r.BiddingDataExclusion, customerID, r.ResourceName, id)
			if !ok {
				return nil, unsureResult(op, "answered with a data exclusion that is not the one asked for")
			}
			item.DataExclusion = x
		}
		out = append(out, item)
	}
	return out, nil
}

// --- descriptors ---

var (
	bdeIDSchema       = schema{"type": "string", "pattern": bdeIDPattern.String()}
	bdeScopeSchema    = schema{"type": "string", "enum": bdeScopes}
	bdeTimeSchema     = schema{"type": "string", "pattern": bdeTimePattern.String()}
	bdeNameSchema     = schema{"type": "string", "minLength": 1, "maxLength": bdeNameMax}
	bdeTextSchema     = schema{"type": "string", "maxLength": bdeDescriptionMax}
	bdeDevicesSchema  = schema{"type": "array", "uniqueItems": true, "maxItems": len(bdeDevices), "items": schema{"type": "string", "enum": bdeDevices}}
	bdeChannelsSchema = schema{"type": "array", "uniqueItems": true, "minItems": 1, "maxItems": len(bdeChannels),
		"items": schema{"type": "string", "enum": bdeChannels}}
	bdeCampaignsSchema = schema{"type": "array", "uniqueItems": true, "minItems": 1, "maxItems": bdeCampaignsMax, "items": bdeIDSchema}

	bdeResultSchema = object(schema{"resource_name": stringSchema, "id": stringSchema, "scope": stringSchema,
		"status": stringSchema, "start_date_time": stringSchema, "end_date_time": stringSchema, "name": stringSchema,
		"description": stringSchema, "devices": arrayOf(stringSchema), "campaign_ids": arrayOf(stringSchema),
		"advertising_channel_types": arrayOf(stringSchema)}, "resource_name")
	bdeOutputSchema = object(schema{"customer_id": stringSchema, "validate_only": booleanSchema,
		"results": arrayOf(object(schema{"data_exclusion_id": stringSchema, "resource_name": stringSchema,
			"data_exclusion": bdeResultSchema}, "data_exclusion_id", "resource_name"))}, "customer_id", "validate_only", "results")
)

const bdeSensitivity = "bidding data exclusions of a Google Ads account: their periods, names, descriptions, devices " +
	"and the campaigns or channels they cover, which the confirmation names; no data of other people"

const bdeNote = " A data exclusion tells the automated bidding strategies to ignore the data of a past period, " +
	"such as a tracking outage: it changes what the strategies learn from and so how they bid and spend. It is " +
	"backward looking, for events that started in the past and end in the past or future, and an interval " +
	"[start_date_time, end_date_time) of more than zero and at most 14 days in the time zone of the account. " +
	"Read exclusions with reports.search on bidding_data_exclusion. The request goes to " +
	"BiddingDataExclusionService.MutateBiddingDataExclusions with up to 10 operations, atomic and without " +
	"partial_failure: Google applies all of them or none."

var bdeFields = []capability.Field{
	{Name: "customer_id", Description: "The account of the call"},
	{Name: "validate_only", Description: "true when Google only validated the change"},
	{Name: "results", Description: "Per operation, in the order of the arguments: data_exclusion_id, resource_name " +
		"(customers/ID/biddingDataExclusions/N, as Google answered, always of the account of the call), and with " +
		"MUTABLE_RESOURCE the data exclusion after the change (id, scope, status, start_date_time, end_date_time, " +
		"name, description, devices, campaign_ids, advertising_channel_types) as Google returns it. Empty after " +
		"validate_only"},
}

var bdeArgumentDocs = []capability.Argument{
	{Name: "customer_id", Required: true, Description: "Ten-digit customer ID of the account without hyphens; a " +
		"target of the connection or an account below a manager target. A manager account needs a manager " +
		"target that names it"},
}

const bdeFieldRules = "scope (CUSTOMER, CAMPAIGN or CHANNEL), start_date_time and end_date_time (yyyy-MM-dd " +
	"HH:mm:ss; the interval is longer than zero and at most 14 days), name (1 to 255 characters), description " +
	"(up to 2048), devices (MOBILE, TABLET, DESKTOP, CONNECTED_TV, OTHER; none means all devices), campaign_ids " +
	"(IDs of campaigns of this account, at most 2000, only with scope CAMPAIGN) and advertising_channel_types " +
	"(DISPLAY, SEARCH or SHOPPING, only with scope CHANNEL). campaign_ids and advertising_channel_types are never " +
	"given together. Text must not hold control characters"

var bdeCreate = capability.Descriptor{
	ID: biddingDataExclusionsCreateID, Version: 1, Title: "Create bidding data exclusions in a Google Ads account",
	Description: "Create one or more bidding data exclusions in a Google Ads account, each with a scope, a start and " +
		"an end, and optionally a name, a description, devices and the campaigns or channel types it covers." +
		bdeNote + " The status of an exclusion is output only and no argument." + mutateListListedNote,
	Tags: []string{"google", "ads", "bidding", "data-exclusions", "create"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent, bdeSensitivity),
	InputSchema: encoded(object(schema{
		"customer_id": customerIDArgument,
		"data_exclusions": schema{"type": "array", "minItems": 1, "maxItems": bdeMaxOperations,
			"items": object(schema{"scope": bdeScopeSchema, "start_date_time": bdeTimeSchema, "end_date_time": bdeTimeSchema,
				"name": bdeNameSchema, "description": bdeTextSchema, "devices": bdeDevicesSchema,
				"campaign_ids": bdeCampaignsSchema, "advertising_channel_types": bdeChannelsSchema},
				"scope", "start_date_time", "end_date_time")},
		"validate_only": validateOnlySchema, "response_content_type": responseContentTypeSchema,
	}, "customer_id", "data_exclusions")),
	OutputSchema: encoded(bdeOutputSchema),
	Arguments: append(slices.Clone(bdeArgumentDocs),
		capability.Argument{Name: "data_exclusions", Required: true, Description: "1 to 10 exclusions, each with " +
			bdeFieldRules + ". scope, start_date_time and end_date_time are required; a scope CAMPAIGN needs campaign_ids " +
			"and a scope CHANNEL needs advertising_channel_types"},
		validateOnlyArgument, responseContentTypeArgument),
	Fields: bdeFields,
	Examples: []capability.Example{{Description: "Exclude a tracking outage of one week for the whole account",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","data_exclusions":[{"scope":"CUSTOMER",` +
			`"start_date_time":"2026-03-01 00:00:00","end_date_time":"2026-03-08 00:00:00","name":"Tracking outage"}]}`)}},
}

var bdeUpdate = capability.Descriptor{
	ID: biddingDataExclusionsUpdateID, Version: 1, Title: "Change bidding data exclusions of a Google Ads account",
	Description: "Change existing bidding data exclusions of a Google Ads account (an update with a field mask that " +
		"names exactly the fields given). Only the data_exclusion_id selects an exclusion: its resource name is built " +
		"from customer_id and data_exclusion_id. An empty description or an empty devices list clears it. The status " +
		"of an exclusion is output only and no argument." + bdeNote + mutateListListedNote,
	Tags: []string{"google", "ads", "bidding", "data-exclusions", "update"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, bdeSensitivity),
	InputSchema: encoded(object(schema{
		"customer_id": customerIDArgument,
		"data_exclusions": schema{"type": "array", "minItems": 1, "maxItems": bdeMaxOperations,
			"items": object(schema{"data_exclusion_id": bdeIDSchema, "scope": bdeScopeSchema,
				"start_date_time": bdeTimeSchema, "end_date_time": bdeTimeSchema, "name": bdeNameSchema,
				"description": bdeTextSchema, "devices": bdeDevicesSchema, "campaign_ids": bdeCampaignsSchema,
				"advertising_channel_types": bdeChannelsSchema}, "data_exclusion_id")},
		"validate_only": validateOnlySchema, "response_content_type": responseContentTypeSchema,
	}, "customer_id", "data_exclusions")),
	OutputSchema: encoded(bdeOutputSchema),
	Arguments: append(slices.Clone(bdeArgumentDocs),
		capability.Argument{Name: "data_exclusions", Required: true, Description: "1 to 10 entries, each " +
			"data_exclusion_id once (as reports.search returns it), with at least one of " + bdeFieldRules +
			". With both times given the interval is checked; with one, Google checks it against the stored other"},
		validateOnlyArgument, responseContentTypeArgument),
	Fields: bdeFields,
	Examples: []capability.Example{{Description: "Shorten an exclusion and limit it to mobile devices",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","data_exclusions":[{"data_exclusion_id":"112233",` +
			`"end_date_time":"2026-03-05 00:00:00","devices":["MOBILE"]}]}`)}},
}

var bdeRemove = capability.Descriptor{
	ID: biddingDataExclusionsRemoveID, Version: 1, Title: "Remove bidding data exclusions from a Google Ads account",
	Description: "Remove existing bidding data exclusions of a Google Ads account (a remove of " +
		"customers/ID/biddingDataExclusions/N, built from customer_id and data_exclusion_id). The bidding strategies " +
		"then learn from the data of that period again. Google marks a removed exclusion REMOVED; no tool of this " +
		"provider brings it back." + bdeNote + mutateListListedNote,
	Tags: []string{"google", "ads", "bidding", "data-exclusions", "remove"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: changeRisk(capability.EffectDelete, capability.IdempotencyIdempotent, bdeSensitivity),
	InputSchema: encoded(object(schema{
		"customer_id": customerIDArgument,
		"data_exclusion_ids": schema{"type": "array", "minItems": 1, "maxItems": bdeMaxOperations, "uniqueItems": true,
			"items": bdeIDSchema},
		"validate_only": validateOnlySchema,
	}, "customer_id", "data_exclusion_ids")),
	OutputSchema: encoded(bdeOutputSchema),
	Arguments: append(slices.Clone(bdeArgumentDocs),
		capability.Argument{Name: "data_exclusion_ids", Required: true, Description: "1 to 10 IDs of existing data " +
			"exclusions, each once, as reports.search returns them"},
		validateOnlyArgument),
	Fields: bdeFields,
	Examples: []capability.Example{{Description: "Validate the removal of an exclusion without making it",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","data_exclusion_ids":["112233"],"validate_only":true}`)}},
}

// --- handlers ---

func handleBiddingDataExclusionsCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "create bidding data exclusions"
	var args bdeCreateArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	ops, err := args.operations()
	if err != nil {
		return nil, err
	}
	return bdeSend(ctx, resolved, secrets, red, op, biddingDataExclusionsCreateID, args.CustomerID, ops, nil,
		args.ValidateOnly, args.ResponseContentType)
}

func handleBiddingDataExclusionsUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "update bidding data exclusions"
	var args bdeUpdateArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	ops, names, err := args.operations()
	if err != nil {
		return nil, err
	}
	return bdeSend(ctx, resolved, secrets, red, op, biddingDataExclusionsUpdateID, args.CustomerID, ops, names,
		args.ValidateOnly, args.ResponseContentType)
}

func handleBiddingDataExclusionsRemove(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "remove bidding data exclusions"
	var args bdeRemoveArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	ops, names, err := args.operations()
	if err != nil {
		return nil, err
	}
	return bdeSend(ctx, resolved, secrets, red, op, biddingDataExclusionsRemoveID, args.CustomerID, ops, names,
		args.ValidateOnly, "")
}

func bdeSend(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	op, tool, customerID string, ops []any, want []string, validateOnly bool, content string) (any, error) {
	env, err := handleEnv(ctx, resolved, secrets, red, op, tool)
	if err != nil {
		return nil, err
	}
	c, err := env.settle(customerID, true)
	if err != nil {
		return nil, err
	}
	body := map[string]any{"operations": ops}
	if validateOnly {
		body["validateOnly"] = true
	}
	if content != "" {
		body["responseContentType"] = content
	}
	var answer bdeAnswer
	if err := c.mutate(ctx, mutation{op: op, tool: tool, customerID: customerID, service: biddingDataExclusionsService,
		body: body}, &answer); err != nil {
		return nil, err
	}
	results, err := bdeResults(op, customerID, answer, len(ops), want, validateOnly)
	if err != nil {
		return nil, err
	}
	return bdeChanged{CustomerID: customerID, ValidateOnly: validateOnly, Results: results}, nil
}

// --- confirmation ---

func bdeQuote(value string) string { return strconv.Quote(boundedText(value, bdeNameMax*4)) }

func bdeLead(validateOnly bool) string {
	if validateOnly {
		return "Only validate: "
	}
	return ""
}

func bdeMore(total, shown int) string {
	if total > shown {
		return " and " + strconv.Itoa(total-shown) + " more"
	}
	return ""
}

func bdeList(values []string) string {
	shown := make([]string, 0, 5)
	for _, v := range values[:min(len(values), 5)] {
		shown = append(shown, boundedText(v, maxCodeText))
	}
	return strings.Join(shown, ",") + bdeMore(len(values), len(shown))
}

func bdeCreateSummary(raw json.RawMessage) string {
	var a bdeCreateArguments
	_ = json.Unmarshal(raw, &a)
	var parts []string
	for _, x := range a.DataExclusions[:min(len(a.DataExclusions), 10)] {
		part := "scope " + boundedText(x.Scope, maxCodeText) + " from " + bdeQuote(x.StartDateTime) + " to " + bdeQuote(x.EndDateTime)
		if x.Name != nil {
			part += " name " + bdeQuote(*x.Name)
		}
		if len(x.Devices) > 0 {
			part += " devices " + bdeList(x.Devices)
		}
		if len(x.CampaignIDs) > 0 {
			part += " campaigns " + bdeList(x.CampaignIDs)
		}
		if len(x.AdvertisingChannelTypes) > 0 {
			part += " channels " + bdeList(x.AdvertisingChannelTypes)
		}
		parts = append(parts, part)
	}
	return bdeLead(a.ValidateOnly) + "Create " + strconv.Itoa(len(a.DataExclusions)) + " bidding data exclusion(s) in " +
		"Google Ads account " + a.CustomerID + ": " + strings.Join(parts, "; ") + bdeMore(len(a.DataExclusions), len(parts)) + "."
}

func bdeUpdateSummary(raw json.RawMessage) string {
	var a bdeUpdateArguments
	_ = json.Unmarshal(raw, &a)
	var parts []string
	for _, x := range a.DataExclusions[:min(len(a.DataExclusions), 10)] {
		part := "data exclusion " + boundedText(x.DataExclusionID, maxCodeText)
		if x.Scope != nil {
			part += " scope " + boundedText(*x.Scope, maxCodeText)
		}
		if x.StartDateTime != nil {
			part += " start " + bdeQuote(*x.StartDateTime)
		}
		if x.EndDateTime != nil {
			part += " end " + bdeQuote(*x.EndDateTime)
		}
		if x.Name != nil {
			part += " name " + bdeQuote(*x.Name)
		}
		if x.Description != nil {
			part += " description " + bdeQuote(*x.Description)
		}
		if x.Devices != nil {
			part += " devices " + bdeList(*x.Devices)
		}
		if x.CampaignIDs != nil {
			part += " campaigns " + bdeList(*x.CampaignIDs)
		}
		if x.AdvertisingChannelTypes != nil {
			part += " channels " + bdeList(*x.AdvertisingChannelTypes)
		}
		parts = append(parts, part)
	}
	return bdeLead(a.ValidateOnly) + "Change " + strconv.Itoa(len(a.DataExclusions)) + " bidding data exclusion(s) of " +
		"Google Ads account " + a.CustomerID + ": " + strings.Join(parts, "; ") + bdeMore(len(a.DataExclusions), len(parts)) + "."
}

func bdeRemoveSummary(raw json.RawMessage) string {
	var a bdeRemoveArguments
	_ = json.Unmarshal(raw, &a)
	return bdeLead(a.ValidateOnly) + "Remove " + strconv.Itoa(len(a.DataExclusionIDs)) + " bidding data exclusion(s) " +
		"from Google Ads account " + a.CustomerID + ": " + bdeList(a.DataExclusionIDs) +
		". Google marks them removed; no tool brings them back."
}
