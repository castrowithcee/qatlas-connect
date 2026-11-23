package googleads

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	experimentArmsCreateID = Provider + ".experiment-arms.create"
	experimentArmsUpdateID = Provider + ".experiment-arms.update"
	experimentArmsRemoveID = Provider + ".experiment-arms.remove"

	experimentArmsService    = "/experimentArms"
	experimentArmsCollection = "experimentArms"
)

// Bounds of ExperimentArm as the reference of v26 documents them: name is required, 1 to 1024 characters and unique
// under the experiment; traffic_split is 1 to 100 and must total 100 between the two trial arms; campaigns[] and
// asset_groups[] hold at most one entry each; experiment and performance_max_experiment_arm_info are immutable.
// The reference states no bound for operations[], asset_group_assets[], asset_variation_infos[], asset_details[] and
// the PMax amounts: the first three and target_roas's range are Qatlas's own bounds.
const (
	experimentArmNameMax     = 1024
	experimentArmSplitMin    = 1
	experimentArmSplitMax    = 100
	experimentArmsMaxEntries = 10
	experimentArmListMax     = 50
	experimentArmRoasMax     = 1000
)

// The enum values of v26 a tool may name; UNSPECIFIED, UNKNOWN and INVALID are no values to send. The deprecated
// strategy types PAGE_ONE_PROMOTED and TARGET_OUTRANK_SHARE are not offered.
var (
	experimentArmAssetFieldTypes = []string{"AD_IMAGE", "BOOK_ON_GOOGLE", "BUSINESS_LOGO", "BUSINESS_MESSAGE",
		"BUSINESS_NAME", "CALL", "CALLOUT", "CALL_TO_ACTION", "CALL_TO_ACTION_SELECTION", "CLASSIC_DISPLAY_IMAGE",
		"DEMAND_GEN_CAROUSEL_CARD", "DESCRIPTION", "HEADLINE", "HOTEL_CALLOUT", "HOTEL_PROPERTY", "LANDING_PAGE_PREVIEW",
		"LANDSCAPE_LOGO", "LEAD_FORM", "LOGO", "LONG_DESCRIPTION", "LONG_HEADLINE", "MANDATORY_AD_TEXT",
		"MARKETING_IMAGE", "MEDIA_BUNDLE", "MOBILE_APP", "PORTRAIT_MARKETING_IMAGE", "PRICE", "PROMOTION",
		"RELATED_YOUTUBE_VIDEOS", "SITELINK", "SQUARE_MARKETING_IMAGE", "STRUCTURED_SNIPPET",
		"TALL_PORTRAIT_MARKETING_IMAGE", "TEXT_DISCLAIMER", "VIDEO", "YOUTUBE_VIDEO"}
	experimentArmDetailOperations = []string{"ADD", "REMOVE"}
	experimentArmBiddingTypes     = []string{"COMMISSION", "ENHANCED_CPC", "FIXED_CPM", "FIXED_SHARE_OF_VOICE",
		"MANUAL_CPA", "MANUAL_CPC", "MANUAL_CPM", "MANUAL_CPV", "MAXIMIZE_CONVERSIONS", "MAXIMIZE_CONVERSION_VALUE",
		"PERCENT_CPC", "TARGET_CPA", "TARGET_CPC", "TARGET_CPM", "TARGET_CPV", "TARGET_IMPRESSION_SHARE", "TARGET_ROAS",
		"TARGET_SPEND"}
)

// experimentArmName is the resource name of an arm, customers/ID/experimentArms/EXPERIMENT~ARM, from checked IDs.
func experimentArmName(customerID, experimentID, armID string) string {
	return customerName(customerID) + "/" + experimentArmsCollection + "/" + experimentID + "~" + armID
}

// experimentArmOwn is the resource name of an object of the account of the call, built from a checked ID.
func experimentArmOwn(customerID, collection, id string) string {
	return customerName(customerID) + "/" + collection + "/" + id
}

// experimentArmIDsOfName returns the experiment and arm ID of an arm name of exactly the checked account.
func experimentArmIDsOfName(name, customerID string) (experimentID, armID string, ok bool) {
	rest, ok := strings.CutPrefix(name, customerName(customerID)+"/"+experimentArmsCollection+"/")
	if !ok {
		return "", "", false
	}
	experimentID, armID, ok = strings.Cut(rest, "~")
	if !ok || !numericIDPattern.MatchString(experimentID) || !numericIDPattern.MatchString(armID) {
		return "", "", false
	}
	_, e1 := strconv.ParseInt(experimentID, 10, 64)
	_, e2 := strconv.ParseInt(armID, 10, 64)
	return experimentID, armID, e1 == nil && e2 == nil
}

// --- arguments ---

type (
	experimentArmAsset struct {
		AssetID   string `json:"asset_id"`
		FieldType string `json:"field_type"`
	}
	experimentArmAssetGroup struct {
		AssetGroupID     string               `json:"asset_group_id"`
		AssetGroupAssets []experimentArmAsset `json:"asset_group_assets"`
	}
	experimentArmAssetDetail struct {
		AssetID              string `json:"asset_id"`
		AssetDetailOperation string `json:"asset_detail_operation"`
		FieldType            string `json:"field_type"`
	}
	experimentArmVariation struct {
		BaseAdID      string                     `json:"base_ad_id"`
		BaseAdGroupID string                     `json:"base_ad_group_id"`
		AssetDetails  []experimentArmAssetDetail `json:"asset_details"`
	}
	experimentArmAssetTesting struct {
		AssetVariationInfos []experimentArmVariation `json:"asset_variation_infos"`
	}
	experimentArmPmax struct {
		BiddingStrategyType *string      `json:"bidding_strategy_type"`
		BudgetAmountMicros  *json.Number `json:"budget_amount_micros"`
		TargetCpaMicros     *json.Number `json:"target_cpa_micros"`
		TargetRoas          *float64     `json:"target_roas"`
	}
	experimentArmChange struct {
		Name         *string                    `json:"name"`
		Control      *bool                      `json:"control"`
		TrafficSplit *int                       `json:"traffic_split"`
		CampaignID   *string                    `json:"campaign_id"`
		AssetGroup   *experimentArmAssetGroup   `json:"asset_group"`
		AssetTesting *experimentArmAssetTesting `json:"asset_testing_info"`
	}
	experimentArmNew struct {
		ExperimentID string `json:"experiment_id"`
		experimentArmChange
		Pmax *experimentArmPmax `json:"performance_max_settings"`
	}
	experimentArmEdit struct {
		ExperimentID string `json:"experiment_id"`
		ArmID        string `json:"arm_id"`
		experimentArmChange
	}
	experimentArmRef struct {
		ExperimentID string `json:"experiment_id"`
		ArmID        string `json:"arm_id"`
	}
	experimentArmsCreateArguments struct {
		CustomerID   string             `json:"customer_id"`
		Arms         []experimentArmNew `json:"experiment_arms"`
		ValidateOnly bool               `json:"validate_only"`
	}
	experimentArmsUpdateArguments struct {
		CustomerID   string              `json:"customer_id"`
		Arms         []experimentArmEdit `json:"experiment_arms"`
		ValidateOnly bool                `json:"validate_only"`
	}
	experimentArmsRemoveArguments struct {
		CustomerID   string             `json:"customer_id"`
		Arms         []experimentArmRef `json:"experiment_arms"`
		ValidateOnly bool               `json:"validate_only"`
	}
)

func experimentArmsCheckList(argument string, n, max int) error {
	if n < 1 || n > max {
		return badRequest(argument + " must hold 1 to " + strconv.Itoa(max) + " entries")
	}
	return nil
}

func experimentArmsAssetGroup(customerID string, g *experimentArmAssetGroup) (map[string]any, error) {
	if err := checkID("asset_group.asset_group_id", g.AssetGroupID); err != nil {
		return nil, err
	}
	out := map[string]any{"assetGroup": experimentArmOwn(customerID, "assetGroups", g.AssetGroupID)}
	if g.AssetGroupAssets != nil {
		if err := experimentArmsCheckList("asset_group.asset_group_assets", len(g.AssetGroupAssets), experimentArmListMax); err != nil {
			return nil, err
		}
		list := make([]any, 0, len(g.AssetGroupAssets))
		for _, a := range g.AssetGroupAssets {
			if err := checkID("asset_group_assets.asset_id", a.AssetID); err != nil {
				return nil, err
			}
			if err := experimentsCheckEnum("asset_group_assets.field_type", a.FieldType, experimentArmAssetFieldTypes); err != nil {
				return nil, err
			}
			list = append(list, map[string]any{"asset": experimentArmOwn(customerID, "assets", a.AssetID), "fieldType": a.FieldType})
		}
		out["assetGroupAssets"] = list
	}
	return out, nil
}

func experimentArmsAssetTesting(customerID string, t *experimentArmAssetTesting) (map[string]any, error) {
	if err := experimentArmsCheckList("asset_testing_info.asset_variation_infos", len(t.AssetVariationInfos), experimentArmListMax); err != nil {
		return nil, err
	}
	infos := make([]any, 0, len(t.AssetVariationInfos))
	for _, v := range t.AssetVariationInfos {
		if err := checkID("asset_variation_infos.base_ad_id", v.BaseAdID); err != nil {
			return nil, err
		}
		if err := checkID("asset_variation_infos.base_ad_group_id", v.BaseAdGroupID); err != nil {
			return nil, err
		}
		info := map[string]any{"baseAd": experimentArmOwn(customerID, "ads", v.BaseAdID),
			"baseAdGroup": experimentArmOwn(customerID, "adGroups", v.BaseAdGroupID)}
		if v.AssetDetails != nil {
			if err := experimentArmsCheckList("asset_variation_infos.asset_details", len(v.AssetDetails), experimentArmListMax); err != nil {
				return nil, err
			}
			details := make([]any, 0, len(v.AssetDetails))
			for _, d := range v.AssetDetails {
				if err := checkID("asset_details.asset_id", d.AssetID); err != nil {
					return nil, err
				}
				if err := experimentsCheckEnum("asset_details.asset_detail_operation", d.AssetDetailOperation,
					experimentArmDetailOperations); err != nil {
					return nil, err
				}
				if err := experimentsCheckEnum("asset_details.field_type", d.FieldType, experimentArmAssetFieldTypes); err != nil {
					return nil, err
				}
				details = append(details, map[string]any{"asset": experimentArmOwn(customerID, "assets", d.AssetID),
					"assetDetailOperation": d.AssetDetailOperation, "fieldType": d.FieldType})
			}
			info["assetDetails"] = details
		}
		infos = append(infos, info)
	}
	return map[string]any{"assetVariationInfos": infos}, nil
}

func experimentArmsPmax(p *experimentArmPmax) (map[string]any, error) {
	s := map[string]any{}
	if p.BiddingStrategyType != nil {
		if err := experimentsCheckEnum("performance_max_settings.bidding_strategy_type", *p.BiddingStrategyType,
			experimentArmBiddingTypes); err != nil {
			return nil, err
		}
		s["biddingStrategyType"] = *p.BiddingStrategyType
	}
	for _, m := range []struct {
		argument, key string
		value         *json.Number
	}{{"performance_max_settings.budget_amount_micros", "budgetAmountMicros", p.BudgetAmountMicros},
		{"performance_max_settings.target_cpa_micros", "targetCpaMicros", p.TargetCpaMicros}} {
		if m.value == nil {
			continue
		}
		amount, err := campaignBudgetsMicros(m.argument, m.value)
		if err != nil {
			return nil, err
		}
		s[m.key] = amount
	}
	if p.TargetRoas != nil {
		if *p.TargetRoas <= 0 || *p.TargetRoas > experimentArmRoasMax {
			return nil, badRequest("performance_max_settings.target_roas must be above 0 and at most " + strconv.Itoa(experimentArmRoasMax))
		}
		s["targetRoas"] = *p.TargetRoas
	}
	if len(s) == 0 {
		return nil, badRequest("performance_max_settings needs at least one setting")
	}
	return map[string]any{"experimentalPerformanceMaxCampaignSettings": s}, nil
}

// experimentArmsFields checks the fields a create and an update share and adds them to the wire form and the mask.
func experimentArmsFields(customerID string, wire map[string]any, mask *[]string, c experimentArmChange) error {
	if c.Name != nil {
		if err := labelsText("name", *c.Name, 1, experimentArmNameMax); err != nil {
			return err
		}
		wire["name"] = *c.Name
		*mask = append(*mask, "name")
	}
	if c.Control != nil {
		wire["control"] = *c.Control
		*mask = append(*mask, "control")
	}
	if c.TrafficSplit != nil {
		if *c.TrafficSplit < experimentArmSplitMin || *c.TrafficSplit > experimentArmSplitMax {
			return badRequest("traffic_split must be 1 to 100")
		}
		wire["trafficSplit"] = strconv.Itoa(*c.TrafficSplit)
		*mask = append(*mask, "trafficSplit")
	}
	if c.CampaignID != nil {
		if err := checkID("campaign_id", *c.CampaignID); err != nil {
			return err
		}
		wire["campaigns"] = []any{experimentArmOwn(customerID, "campaigns", *c.CampaignID)}
		*mask = append(*mask, "campaigns")
	}
	if c.AssetGroup != nil {
		g, err := experimentArmsAssetGroup(customerID, c.AssetGroup)
		if err != nil {
			return err
		}
		wire["assetGroups"] = []any{g}
		*mask = append(*mask, "assetGroups")
	}
	if c.AssetTesting != nil {
		t, err := experimentArmsAssetTesting(customerID, c.AssetTesting)
		if err != nil {
			return err
		}
		wire["assetTestingInfo"] = t
		*mask = append(*mask, "assetTestingInfo")
	}
	return nil
}

func (a experimentArmsCreateArguments) operations() ([]any, error) {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := experimentArmsCheckList("experiment_arms", len(a.Arms), experimentArmsMaxEntries); err != nil {
		return nil, err
	}
	ops := make([]any, 0, len(a.Arms))
	for _, e := range a.Arms {
		if err := checkID("experiment_id", e.ExperimentID); err != nil {
			return nil, err
		}
		if e.Name == nil {
			return nil, badRequest("each entry of experiment_arms needs a name")
		}
		create := map[string]any{"experiment": experimentName(a.CustomerID, e.ExperimentID)}
		var mask []string
		if err := experimentArmsFields(a.CustomerID, create, &mask, e.experimentArmChange); err != nil {
			return nil, err
		}
		if e.Pmax != nil {
			p, err := experimentArmsPmax(e.Pmax)
			if err != nil {
				return nil, err
			}
			create["performanceMaxExperimentArmInfo"] = p
		}
		ops = append(ops, map[string]any{"create": create})
	}
	return ops, nil
}

func (a experimentArmsUpdateArguments) operations() ([]any, []string, error) {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return nil, nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := experimentArmsCheckList("experiment_arms", len(a.Arms), experimentArmsMaxEntries); err != nil {
		return nil, nil, err
	}
	ops := make([]any, 0, len(a.Arms))
	names := make([]string, 0, len(a.Arms))
	for _, e := range a.Arms {
		name, err := experimentArmRefName(a.CustomerID, e.ExperimentID, e.ArmID, names)
		if err != nil {
			return nil, nil, err
		}
		update := map[string]any{"resourceName": name}
		var mask []string
		if err := experimentArmsFields(a.CustomerID, update, &mask, e.experimentArmChange); err != nil {
			return nil, nil, err
		}
		if len(mask) == 0 {
			return nil, nil, badRequest("each entry of experiment_arms needs at least one field to change")
		}
		names = append(names, name)
		ops = append(ops, map[string]any{"update": update, "updateMask": strings.Join(mask, ",")})
	}
	return ops, names, nil
}

func (a experimentArmsRemoveArguments) operations() ([]any, []string, error) {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return nil, nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := experimentArmsCheckList("experiment_arms", len(a.Arms), experimentArmsMaxEntries); err != nil {
		return nil, nil, err
	}
	ops := make([]any, 0, len(a.Arms))
	names := make([]string, 0, len(a.Arms))
	for _, e := range a.Arms {
		name, err := experimentArmRefName(a.CustomerID, e.ExperimentID, e.ArmID, names)
		if err != nil {
			return nil, nil, err
		}
		names = append(names, name)
		ops = append(ops, map[string]any{"remove": name})
	}
	return ops, names, nil
}

// experimentArmRefName builds the name of an existing arm from its two IDs and refuses a repeated arm.
func experimentArmRefName(customerID, experimentID, armID string, seen []string) (string, error) {
	if err := checkID("experiment_id", experimentID); err != nil {
		return "", err
	}
	if err := checkID("arm_id", armID); err != nil {
		return "", err
	}
	name := experimentArmName(customerID, experimentID, armID)
	if slices.Contains(seen, name) {
		return "", badRequest("experiment_arms names each arm at most once")
	}
	return name, nil
}

// --- results ---

type (
	experimentArmMutated struct {
		ExperimentID string `json:"experiment_id"`
		ArmID        string `json:"arm_id"`
		ResourceName string `json:"resource_name"`
	}
	experimentArmsChanged struct {
		CustomerID   string                 `json:"customer_id"`
		ValidateOnly bool                   `json:"validate_only"`
		Results      []experimentArmMutated `json:"results"`
	}
)

// --- descriptors ---

var (
	experimentArmAssetSchema = object(schema{"asset_id": experimentIDSchema,
		"field_type": schema{"type": "string", "enum": experimentArmAssetFieldTypes}}, "asset_id", "field_type")
	experimentArmChangeProperties = schema{
		"name":          schema{"type": "string", "minLength": 1, "maxLength": experimentArmNameMax},
		"control":       booleanSchema,
		"traffic_split": schema{"type": "integer", "minimum": experimentArmSplitMin, "maximum": experimentArmSplitMax},
		"campaign_id":   experimentIDSchema,
		"asset_group": object(schema{"asset_group_id": experimentIDSchema,
			"asset_group_assets": schema{"type": "array", "minItems": 1, "maxItems": experimentArmListMax,
				"items": experimentArmAssetSchema}}, "asset_group_id"),
		"asset_testing_info": object(schema{"asset_variation_infos": schema{"type": "array", "minItems": 1,
			"maxItems": experimentArmListMax, "items": object(schema{"base_ad_id": experimentIDSchema,
				"base_ad_group_id": experimentIDSchema,
				"asset_details": schema{"type": "array", "minItems": 1, "maxItems": experimentArmListMax,
					"items": object(schema{"asset_id": experimentIDSchema,
						"asset_detail_operation": schema{"type": "string", "enum": experimentArmDetailOperations},
						"field_type":             schema{"type": "string", "enum": experimentArmAssetFieldTypes}},
						"asset_id", "asset_detail_operation", "field_type")}},
				"base_ad_id", "base_ad_group_id")}}, "asset_variation_infos"),
	}
	experimentArmsOutputSchema = object(schema{"customer_id": stringSchema, "validate_only": booleanSchema,
		"results": arrayOf(object(schema{"experiment_id": stringSchema, "arm_id": stringSchema, "resource_name": stringSchema},
			"experiment_id", "arm_id", "resource_name"))}, "customer_id", "validate_only", "results")
)

func experimentArmProperties(extra schema) schema {
	p := schema{}
	for k, v := range experimentArmChangeProperties {
		p[k] = v
	}
	for k, v := range extra {
		p[k] = v
	}
	return p
}

const experimentArmsSensitivity = "experiment arms of a Google Ads account: their names, traffic split and the campaigns, " +
	"asset groups and assets they name by ID, which the confirmation names; a Performance Max arm also carries the " +
	"budget and bidding settings of its experimental campaign; no data of other people"

const experimentArmsNote = " An arm is one of the groups of an experiment (control or treatment). Experiment, arm, " +
	"campaign, asset group, ad group, ad and asset are given as IDs of customer_id, never as resource names: Qatlas " +
	"Connect builds the names from customer_id. This tool neither schedules, ends, graduates or promotes the " +
	"experiment (read arms with reports.search on the experiment_arm resource). The request goes to " +
	"ExperimentArmService.MutateExperimentArms with up to 10 operations, atomic and without partial_failure: Google " +
	"applies all of them or none. A result holds the resource name only, since Qatlas Connect asks for no " +
	"response_content_type."

var experimentArmsFields_ = []capability.Field{
	{Name: "customer_id", Description: "The account of the call"},
	{Name: "validate_only", Description: "true when Google only validated the change"},
	{Name: "results", Description: "Per operation, in the order of the arguments: experiment_id, arm_id and resource_name " +
		"(customers/ID/experimentArms/EXPERIMENT_ID~ARM_ID, as Google answered, always of the account of the call). " +
		"Empty after validate_only"},
}

const experimentArmChangeDoc = "name (1 to 1024 characters, unique under the experiment), control (marks the arm " +
	"the other arms are compared with), traffic_split (1 to 100, the two trial arms must total 100), campaign_id (at " +
	"most one campaign of the account), asset_group (one asset_group_id with optional asset_group_assets of asset_id " +
	"and field_type, for Optimize Assets experiments) and asset_testing_info (asset_variation_infos of base_ad_id, " +
	"base_ad_group_id and asset_details of asset_id, asset_detail_operation ADD or REMOVE and field_type)"

var experimentArmsArgumentDocs = []capability.Argument{
	{Name: "customer_id", Required: true, Description: "Ten-digit customer ID of the account without hyphens; a " +
		"target of the connection or an account below a manager target. A manager account needs a manager target " +
		"that names it"},
}

var experimentArmsCreate = capability.Descriptor{
	ID: experimentArmsCreateID, Version: 1, Title: "Create experiment arms in a Google Ads account",
	Description: "Create one or more arms of existing experiments of a Google Ads account. experiment_id and name are " +
		"required; further fields are " + experimentArmChangeDoc + ". For a PMAX_REPLACEMENT_SHOPPING experiment, " +
		"performance_max_settings sets the experimental Performance Max campaign Google creates: " +
		"bidding_strategy_type, budget_amount_micros, target_cpa_micros and target_roas (immutable afterwards; they set a " +
		"budget and a bid directly, and the confirmation names them). The experiment is bound by experiment_id." +
		experimentArmsNote + mutateListListedNote,
	Tags: []string{"google", "ads", "experiment-arms", "create"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent, experimentArmsSensitivity),
	InputSchema: encoded(object(schema{
		"customer_id": customerIDArgument,
		"experiment_arms": schema{"type": "array", "minItems": 1, "maxItems": experimentArmsMaxEntries,
			"items": object(experimentArmProperties(schema{"experiment_id": experimentIDSchema,
				"performance_max_settings": object(schema{
					"bidding_strategy_type": schema{"type": "string", "enum": experimentArmBiddingTypes},
					"budget_amount_micros":  schema{"type": "integer", "minimum": 1},
					"target_cpa_micros":     schema{"type": "integer", "minimum": 1},
					"target_roas":           schema{"type": "number", "exclusiveMinimum": 0, "maximum": experimentArmRoasMax},
				})}), "experiment_id", "name")},
		"validate_only": validateOnlySchema,
	}, "customer_id", "experiment_arms")),
	OutputSchema: encoded(experimentArmsOutputSchema),
	Arguments: append(slices.Clone(experimentArmsArgumentDocs),
		capability.Argument{Name: "experiment_arms", Required: true, Description: "1 to 10 arms, each with experiment_id " +
			"and name (required) and optionally " + experimentArmChangeDoc + " and performance_max_settings"},
		validateOnlyArgument),
	Fields: experimentArmsFields_,
	Examples: []capability.Example{{Description: "Create the treatment arm of an experiment",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","experiment_arms":[{"experiment_id":"112233",` +
			`"name":"Treatment","control":false,"traffic_split":50,"campaign_id":"445566"}]}`)}},
}

var experimentArmsUpdate = capability.Descriptor{
	ID: experimentArmsUpdateID, Version: 1, Title: "Change experiment arms of a Google Ads account",
	Description: "Change existing arms of experiments of a Google Ads account (an update with a field mask that names " +
		"exactly the fields given): " + experimentArmChangeDoc + ". An arm is selected by experiment_id and arm_id " +
		"together; the experiment of an arm and its performance_max_settings cannot be changed." + experimentArmsNote +
		" Offered by a connection with the update effect or whose tools list names it; no profile holds it." + mutateListNote,
	Tags: []string{"google", "ads", "experiment-arms", "update"}, Provider: Provider,
	Risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, experimentArmsSensitivity),
	InputSchema: encoded(object(schema{
		"customer_id": customerIDArgument,
		"experiment_arms": schema{"type": "array", "minItems": 1, "maxItems": experimentArmsMaxEntries,
			"items": object(experimentArmProperties(schema{"experiment_id": experimentIDSchema, "arm_id": experimentIDSchema}),
				"experiment_id", "arm_id")},
		"validate_only": validateOnlySchema,
	}, "customer_id", "experiment_arms")),
	OutputSchema: encoded(experimentArmsOutputSchema),
	Arguments: append(slices.Clone(experimentArmsArgumentDocs),
		capability.Argument{Name: "experiment_arms", Required: true, Description: "1 to 10 entries, each arm once " +
			"(experiment_id and arm_id as reports.search returns them) with at least one of " + experimentArmChangeDoc},
		validateOnlyArgument),
	Fields: experimentArmsFields_,
	Examples: []capability.Example{{Description: "Change the traffic split of an arm",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","experiment_arms":[{"experiment_id":"112233",` +
			`"arm_id":"778899","traffic_split":30}]}`)}},
}

var experimentArmsRemove = capability.Descriptor{
	ID: experimentArmsRemoveID, Version: 1, Title: "Remove experiment arms from a Google Ads account",
	Description: "Remove existing arms of experiments of a Google Ads account (a remove of customers/ID/experimentArms/" +
		"EXPERIMENT_ID~ARM_ID, built from customer_id, experiment_id and arm_id). No tool of this provider brings a " +
		"removed arm back." + experimentArmsNote + mutateListListedNote,
	Tags: []string{"google", "ads", "experiment-arms", "remove"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: changeRisk(capability.EffectDelete, capability.IdempotencyIdempotent, experimentArmsSensitivity),
	InputSchema: encoded(object(schema{
		"customer_id": customerIDArgument,
		"experiment_arms": schema{"type": "array", "minItems": 1, "maxItems": experimentArmsMaxEntries,
			"items": object(schema{"experiment_id": experimentIDSchema, "arm_id": experimentIDSchema},
				"experiment_id", "arm_id")},
		"validate_only": validateOnlySchema,
	}, "customer_id", "experiment_arms")),
	OutputSchema: encoded(experimentArmsOutputSchema),
	Arguments: append(slices.Clone(experimentArmsArgumentDocs),
		capability.Argument{Name: "experiment_arms", Required: true, Description: "1 to 10 existing arms, each once, " +
			"as experiment_id and arm_id as reports.search returns them"},
		validateOnlyArgument),
	Fields: experimentArmsFields_,
	Examples: []capability.Example{{Description: "Validate the removal of an arm without making it",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","experiment_arms":[{"experiment_id":"112233",` +
			`"arm_id":"778899"}],"validate_only":true}`)}},
}

// --- handlers ---

func handleExperimentArmsCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "create experiment arms"
	var args experimentArmsCreateArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	ops, err := args.operations()
	if err != nil {
		return nil, err
	}
	return experimentArmsSend(ctx, resolved, secrets, red, op, experimentArmsCreateID, args.CustomerID, ops, nil, args.ValidateOnly)
}

func handleExperimentArmsUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "update experiment arms"
	var args experimentArmsUpdateArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	ops, names, err := args.operations()
	if err != nil {
		return nil, err
	}
	return experimentArmsSend(ctx, resolved, secrets, red, op, experimentArmsUpdateID, args.CustomerID, ops, names, args.ValidateOnly)
}

func handleExperimentArmsRemove(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "remove experiment arms"
	var args experimentArmsRemoveArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	ops, names, err := args.operations()
	if err != nil {
		return nil, err
	}
	return experimentArmsSend(ctx, resolved, secrets, red, op, experimentArmsRemoveID, args.CustomerID, ops, names, args.ValidateOnly)
}

func experimentArmsSend(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	op, tool, customerID string, ops []any, want []string, validateOnly bool) (any, error) {
	env, err := handleEnv(ctx, resolved, secrets, red, op, tool)
	if err != nil {
		return nil, err
	}
	c, err := env.settle(customerID, true)
	if err != nil {
		return nil, err
	}
	results, err := c.mutateList(ctx, listMutation{op: op, tool: tool, customerID: customerID,
		service: experimentArmsService, collection: experimentArmsCollection, operations: ops, names: want,
		idOf: func(name string) (string, bool) {
			e, a, ok := experimentArmIDsOfName(name, customerID)
			return e + "~" + a, ok
		}, validateOnly: validateOnly})
	if err != nil {
		return nil, err
	}
	out := make([]experimentArmMutated, 0, len(results))
	seen := map[string]bool{}
	for _, r := range results {
		if seen[r.ID] {
			return nil, unsureResult(op, "answered with the same experiment arm twice")
		}
		seen[r.ID] = true
		e, a, _ := strings.Cut(r.ID, "~")
		out = append(out, experimentArmMutated{ExperimentID: e, ArmID: a, ResourceName: r.ResourceName})
	}
	return experimentArmsChanged{CustomerID: customerID, ValidateOnly: validateOnly, Results: out}, nil
}

// --- confirmation ---

func experimentArmsFieldText(c experimentArmChange) string {
	text := experimentsOptional("name", c.Name)
	if c.Control != nil {
		text += " control " + strconv.FormatBool(*c.Control)
	}
	if c.TrafficSplit != nil {
		text += " traffic split " + strconv.Itoa(*c.TrafficSplit)
	}
	if c.CampaignID != nil {
		text += " campaign " + boundedText(*c.CampaignID, maxCodeText)
	}
	if c.AssetGroup != nil {
		text += " asset group " + boundedText(c.AssetGroup.AssetGroupID, maxCodeText) + " with " +
			strconv.Itoa(len(c.AssetGroup.AssetGroupAssets)) + " asset(s)"
	}
	if c.AssetTesting != nil {
		text += " asset testing of " + strconv.Itoa(len(c.AssetTesting.AssetVariationInfos)) + " ad(s)"
	}
	return text
}

func experimentArmsPmaxText(p *experimentArmPmax) string {
	if p == nil {
		return ""
	}
	text := " Performance Max campaign settings"
	if p.BiddingStrategyType != nil {
		text += " bidding " + boundedText(*p.BiddingStrategyType, maxCodeText)
	}
	if p.BudgetAmountMicros != nil {
		text += " budget " + boundedText(p.BudgetAmountMicros.String(), maxCodeText) + " micros"
	}
	if p.TargetCpaMicros != nil {
		text += " target CPA " + boundedText(p.TargetCpaMicros.String(), maxCodeText) + " micros"
	}
	if p.TargetRoas != nil {
		text += " target ROAS " + strconv.FormatFloat(*p.TargetRoas, 'g', 6, 64)
	}
	return text
}

func experimentArmsCreateSummary(raw json.RawMessage) string {
	var a experimentArmsCreateArguments
	_ = json.Unmarshal(raw, &a)
	var parts []string
	for _, e := range a.Arms[:min(len(a.Arms), 10)] {
		parts = append(parts, "arm of experiment "+boundedText(e.ExperimentID, maxCodeText)+
			experimentArmsFieldText(e.experimentArmChange)+experimentArmsPmaxText(e.Pmax))
	}
	return campaignGroupsLead(a.ValidateOnly) + "Create " + strconv.Itoa(len(a.Arms)) +
		" experiment arm(s) in Google Ads account " + a.CustomerID + ": " + strings.Join(parts, "; ") +
		campaignGroupsMore(len(a.Arms), len(parts)) + "."
}

func experimentArmsUpdateSummary(raw json.RawMessage) string {
	var a experimentArmsUpdateArguments
	_ = json.Unmarshal(raw, &a)
	var parts []string
	for _, e := range a.Arms[:min(len(a.Arms), 10)] {
		parts = append(parts, "arm "+boundedText(e.ArmID, maxCodeText)+" of experiment "+
			boundedText(e.ExperimentID, maxCodeText)+experimentArmsFieldText(e.experimentArmChange))
	}
	return campaignGroupsLead(a.ValidateOnly) + "Change " + strconv.Itoa(len(a.Arms)) +
		" experiment arm(s) of Google Ads account " + a.CustomerID + ": " + strings.Join(parts, "; ") +
		campaignGroupsMore(len(a.Arms), len(parts)) + "."
}

func experimentArmsRemoveSummary(raw json.RawMessage) string {
	var a experimentArmsRemoveArguments
	_ = json.Unmarshal(raw, &a)
	var parts []string
	for _, e := range a.Arms[:min(len(a.Arms), 10)] {
		parts = append(parts, "arm "+boundedText(e.ArmID, maxCodeText)+" of experiment "+boundedText(e.ExperimentID, maxCodeText))
	}
	return campaignGroupsLead(a.ValidateOnly) + "Remove " + strconv.Itoa(len(a.Arms)) +
		" experiment arm(s) from Google Ads account " + a.CustomerID + ": " + strings.Join(parts, ", ") +
		campaignGroupsMore(len(a.Arms), len(parts)) + ". No tool brings them back."
}
