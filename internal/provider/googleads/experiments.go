package googleads

import (
	"context"
	"encoding/json"
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
	experimentsCreateID          = Provider + ".experiments.create"
	experimentsUpdateID          = Provider + ".experiments.update"
	experimentsRemoveID          = Provider + ".experiments.remove"
	experimentsListAsyncErrorsID = Provider + ".experiments.list-async-errors"

	experimentsService    = "/experiments"
	experimentsCollection = "experiments"
)

// Bounds of the experiment fields as the reference of v26 documents them: Experiment.name is required, 1 to
// 1024 characters and unique under the customer; description is 1 to 2048 characters; type is required;
// start_date and end_date are YYYY-MM-DD; sync_enabled and the two subtype messages can only be set at creation.
// The reference states no length for suffix, no bound for goals and no bound for operations[].
const (
	experimentNameMax        = 1024
	experimentDescriptionMax = 2048
	// experimentSuffixMax, experimentGoalsMax and experimentsMaxOperations are Qatlas's own bounds.
	experimentSuffixMax      = 255
	experimentGoalsMax       = 10
	experimentsMaxOperations = 10
	experimentDateLayout     = "2006-01-02"
	experimentDatePattern    = `^[0-9]{4}-[0-9]{2}-[0-9]{2}$`
)

// The enum values of v26 a tool may name; UNSPECIFIED and UNKNOWN are no values to send. A status is limited to the
// ones the advertiser chooses: REMOVED is what experiments.remove does, and PROMOTED and GRADUATED belong to the
// actions that promote or graduate, which this provider does not offer here; INITIATED is set by the system.
var (
	experimentTypes = []string{"ADOPT_AI_MAX", "ADOPT_BROAD_MATCH_KEYWORDS", "AD_VARIATION", "COMPARE_CAMPAIGNS",
		"DISPLAY_AND_VIDEO_360", "DISPLAY_AUTOMATED_BIDDING_STRATEGY", "DISPLAY_CUSTOM", "HOTEL_CUSTOM",
		"OPTIMIZE_ASSETS", "PMAX_REPLACEMENT_SHOPPING", "PMAX_TEXT_CUSTOMIZATION_FINAL_URL_EXPANSION",
		"SEARCH_AUTOMATED_BIDDING_STRATEGY", "SEARCH_CUSTOM", "SHOPPING_AUTOMATED_BIDDING_STRATEGY", "SMART_MATCHING",
		"YOUTUBE_CUSTOM"}
	experimentStatuses = []string{"ENABLED", "HALTED", "SETUP"}
	experimentMetrics  = []string{"AVERAGE_CPC", "CLICKS", "COMPLETED_VIDEO_VIEWS", "CONVERSIONS",
		"CONVERSIONS_PER_INTERACTION_RATE", "CONVERSIONS_VALUE_PER_COST", "CONVERSION_VALUE", "COST", "COST_PER_CONVERSION",
		"CTR", "CUSTOM_ALGORITHMS", "IMPRESSIONS", "INCREMENTAL_CONVERSIONS"}
	experimentDirections    = []string{"DECREASE", "INCREASE", "NO_CHANGE", "NO_CHANGE_OR_DECREASE", "NO_CHANGE_OR_INCREASE"}
	optimizeAssetsSubtypes  = []string{"ADD_ASSETS_TO_ASSETLESS_RETAIL", "ADD_VIDEO_ASSETS_TO_VIDEOLESS", "COMPARE_ASSETS"}
	videoExperimentSubtypes = []string{"ASSET", "ASSET_UPLIFT", "DEMAND_GEN_ASSET"}
)

// experimentName is the resource name of an experiment, built from checked IDs alone.
func experimentName(customerID, experimentID string) string {
	return customerName(customerID) + "/experiments/" + experimentID
}

func experimentIDOfName(name, customerID string) (string, bool) {
	id, ok := numericIDOf(name, customerID, experimentsCollection)
	if !ok {
		return "", false
	}
	_, err := strconv.ParseInt(id, 10, 64)
	return id, err == nil
}

func experimentsCheckCount(argument string, n int) error {
	if n < 1 || n > experimentsMaxOperations {
		return badRequest(argument + " must hold 1 to " + strconv.Itoa(experimentsMaxOperations) + " entries")
	}
	return nil
}

func experimentsCheckEnum(argument, value string, allowed []string) error {
	if !slices.Contains(allowed, value) {
		return badRequest(argument + " must be one of " + strings.Join(allowed, ", "))
	}
	return nil
}

func experimentsCheckDate(argument, value string) (time.Time, error) {
	t, err := time.Parse(experimentDateLayout, value)
	if err != nil {
		return t, badRequest(argument + " must be a date in the form YYYY-MM-DD")
	}
	return t, nil
}

// --- arguments ---

type (
	experimentsGoal struct {
		Metric    string `json:"metric"`
		Direction string `json:"direction"`
	}
	experimentsNew struct {
		Name                            string            `json:"name"`
		Type                            string            `json:"type"`
		Description                     *string           `json:"description"`
		Suffix                          *string           `json:"suffix"`
		Status                          *string           `json:"status"`
		StartDate                       *string           `json:"start_date"`
		EndDate                         *string           `json:"end_date"`
		SyncEnabled                     *bool             `json:"sync_enabled"`
		Goals                           []experimentsGoal `json:"goals"`
		OptimizeAssetsExperimentSubtype *string           `json:"optimize_assets_experiment_subtype"`
		VideoExperimentSubtype          *string           `json:"video_experiment_subtype"`
	}
	experimentsEdit struct {
		ExperimentID string            `json:"experiment_id"`
		Name         *string           `json:"name"`
		Description  *string           `json:"description"`
		Suffix       *string           `json:"suffix"`
		Status       *string           `json:"status"`
		StartDate    *string           `json:"start_date"`
		EndDate      *string           `json:"end_date"`
		Goals        []experimentsGoal `json:"goals"`
	}
	experimentsCreateArguments struct {
		CustomerID   string           `json:"customer_id"`
		Experiments  []experimentsNew `json:"experiments"`
		ValidateOnly bool             `json:"validate_only"`
	}
	experimentsUpdateArguments struct {
		CustomerID   string            `json:"customer_id"`
		Experiments  []experimentsEdit `json:"experiments"`
		ValidateOnly bool              `json:"validate_only"`
	}
	experimentsRemoveArguments struct {
		CustomerID    string   `json:"customer_id"`
		ExperimentIDs []string `json:"experiment_ids"`
		ValidateOnly  bool     `json:"validate_only"`
	}
	experimentsListAsyncErrorsArguments struct {
		CustomerID   string `json:"customer_id"`
		ExperimentID string `json:"experiment_id"`
		PageSize     *int   `json:"page_size"`
		Cursor       string `json:"cursor"`
	}
)

// experimentsFields checks the fields a create and an update share and adds them to the wire form and the mask.
func experimentsFields(wire map[string]any, mask *[]string, name, description, suffix, status, start, end *string,
	goals []experimentsGoal, goalsGiven bool) error {
	if name != nil {
		if err := labelsText("name", *name, 1, experimentNameMax); err != nil {
			return err
		}
		wire["name"] = *name
		*mask = append(*mask, "name")
	}
	if description != nil {
		if err := labelsText("description", *description, 1, experimentDescriptionMax); err != nil {
			return err
		}
		wire["description"] = *description
		*mask = append(*mask, "description")
	}
	if suffix != nil {
		if err := labelsText("suffix", *suffix, 1, experimentSuffixMax); err != nil {
			return err
		}
		wire["suffix"] = *suffix
		*mask = append(*mask, "suffix")
	}
	if status != nil {
		if err := experimentsCheckEnum("status", *status, experimentStatuses); err != nil {
			return err
		}
		wire["status"] = *status
		*mask = append(*mask, "status")
	}
	var from, to time.Time
	var err error
	if start != nil {
		if from, err = experimentsCheckDate("start_date", *start); err != nil {
			return err
		}
		wire["startDate"] = *start
		*mask = append(*mask, "startDate")
	}
	if end != nil {
		if to, err = experimentsCheckDate("end_date", *end); err != nil {
			return err
		}
		wire["endDate"] = *end
		*mask = append(*mask, "endDate")
	}
	if start != nil && end != nil && to.Before(from) {
		return badRequest("end_date must not be before start_date")
	}
	if goalsGiven {
		if len(goals) < 1 || len(goals) > experimentGoalsMax {
			return badRequest("goals must hold 1 to " + strconv.Itoa(experimentGoalsMax) + " entries")
		}
		list := make([]any, 0, len(goals))
		for _, g := range goals {
			if err := experimentsCheckEnum("goals.metric", g.Metric, experimentMetrics); err != nil {
				return err
			}
			if err := experimentsCheckEnum("goals.direction", g.Direction, experimentDirections); err != nil {
				return err
			}
			list = append(list, map[string]any{"metric": g.Metric, "direction": g.Direction})
		}
		wire["goals"] = list
		*mask = append(*mask, "goals")
	}
	return nil
}

func (a experimentsCreateArguments) operations() ([]any, error) {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := experimentsCheckCount("experiments", len(a.Experiments)); err != nil {
		return nil, err
	}
	ops := make([]any, 0, len(a.Experiments))
	for _, e := range a.Experiments {
		if err := experimentsCheckEnum("type", e.Type, experimentTypes); err != nil {
			return nil, err
		}
		create := map[string]any{"type": e.Type}
		var mask []string
		if err := experimentsFields(create, &mask, &e.Name, e.Description, e.Suffix, e.Status, e.StartDate, e.EndDate,
			e.Goals, e.Goals != nil); err != nil {
			return nil, err
		}
		if e.SyncEnabled != nil {
			create["syncEnabled"] = *e.SyncEnabled
		}
		if s := e.OptimizeAssetsExperimentSubtype; s != nil {
			if e.Type != "OPTIMIZE_ASSETS" {
				return nil, badRequest("optimize_assets_experiment_subtype belongs to the type OPTIMIZE_ASSETS")
			}
			if err := experimentsCheckEnum("optimize_assets_experiment_subtype", *s, optimizeAssetsSubtypes); err != nil {
				return nil, err
			}
			create["optimizeAssetsExperiment"] = map[string]any{"optimizeAssetsExperimentSubtype": *s}
		}
		if s := e.VideoExperimentSubtype; s != nil {
			if e.Type != "YOUTUBE_CUSTOM" {
				return nil, badRequest("video_experiment_subtype belongs to the type YOUTUBE_CUSTOM")
			}
			if err := experimentsCheckEnum("video_experiment_subtype", *s, videoExperimentSubtypes); err != nil {
				return nil, err
			}
			create["videoExperiment"] = map[string]any{"videoExperimentSubtype": *s}
		}
		ops = append(ops, map[string]any{"create": create})
	}
	return ops, nil
}

func (a experimentsUpdateArguments) operations() ([]any, []string, error) {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return nil, nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := experimentsCheckCount("experiments", len(a.Experiments)); err != nil {
		return nil, nil, err
	}
	ops := make([]any, 0, len(a.Experiments))
	names := make([]string, 0, len(a.Experiments))
	for _, e := range a.Experiments {
		if err := checkID("experiment_id", e.ExperimentID); err != nil {
			return nil, nil, err
		}
		name := experimentName(a.CustomerID, e.ExperimentID)
		if slices.Contains(names, name) {
			return nil, nil, badRequest("experiments names each experiment_id at most once")
		}
		update := map[string]any{"resourceName": name}
		var mask []string
		if err := experimentsFields(update, &mask, e.Name, e.Description, e.Suffix, e.Status, e.StartDate, e.EndDate,
			e.Goals, e.Goals != nil); err != nil {
			return nil, nil, err
		}
		if len(mask) == 0 {
			return nil, nil, badRequest("each entry of experiments needs at least one field to change")
		}
		names = append(names, name)
		ops = append(ops, map[string]any{"update": update, "updateMask": strings.Join(mask, ",")})
	}
	return ops, names, nil
}

func (a experimentsRemoveArguments) operations() ([]any, []string, error) {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return nil, nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := experimentsCheckCount("experiment_ids", len(a.ExperimentIDs)); err != nil {
		return nil, nil, err
	}
	ops := make([]any, 0, len(a.ExperimentIDs))
	names := make([]string, 0, len(a.ExperimentIDs))
	for _, id := range a.ExperimentIDs {
		if err := checkID("experiment_ids", id); err != nil {
			return nil, nil, err
		}
		name := experimentName(a.CustomerID, id)
		if slices.Contains(names, name) {
			return nil, nil, badRequest("experiment_ids names each experiment at most once")
		}
		names = append(names, name)
		ops = append(ops, map[string]any{"remove": name})
	}
	return ops, names, nil
}

// --- results ---

type (
	experimentsMutated struct {
		ExperimentID string `json:"experiment_id"`
		ResourceName string `json:"resource_name"`
	}
	experimentsChanged struct {
		CustomerID   string               `json:"customer_id"`
		ValidateOnly bool                 `json:"validate_only"`
		Results      []experimentsMutated `json:"results"`
	}
)

// --- descriptors ---

var (
	experimentIDSchema   = schema{"type": "string", "pattern": idPatternText}
	experimentDateSchema = schema{"type": "string", "pattern": experimentDatePattern}
	experimentGoalSchema = object(schema{"metric": schema{"type": "string", "enum": experimentMetrics},
		"direction": schema{"type": "string", "enum": experimentDirections}}, "metric", "direction")
	experimentGoalsSchema = schema{"type": "array", "minItems": 1, "maxItems": experimentGoalsMax,
		"items": experimentGoalSchema}
	experimentsOutputSchema = object(schema{"customer_id": stringSchema, "validate_only": booleanSchema,
		"results": arrayOf(object(schema{"experiment_id": stringSchema, "resource_name": stringSchema},
			"experiment_id", "resource_name"))}, "customer_id", "validate_only", "results")
)

func experimentsEditProperties(extra schema) schema {
	p := schema{
		"name":        schema{"type": "string", "minLength": 1, "maxLength": experimentNameMax},
		"description": schema{"type": "string", "minLength": 1, "maxLength": experimentDescriptionMax},
		"suffix":      schema{"type": "string", "minLength": 1, "maxLength": experimentSuffixMax},
		"status":      schema{"type": "string", "enum": experimentStatuses},
		"start_date":  experimentDateSchema, "end_date": experimentDateSchema, "goals": experimentGoalsSchema,
	}
	for k, v := range extra {
		p[k] = v
	}
	return p
}

const experimentsSensitivity = "experiments of a Google Ads account: their names, descriptions and goals, which the " +
	"confirmation names; no data of other people"

const experimentsNote = " An experiment tests a change of a base campaign against the original; this tool neither " +
	"schedules, ends, graduates or promotes it, nor changes its arms (read experiments with reports.search on the " +
	"experiment resource). The request goes to ExperimentService.MutateExperiments with up to 10 operations, atomic " +
	"and without partial_failure: Google applies all of them or none. Google offers no response_content_type here, so " +
	"a result holds the resource name only."

var experimentsFields_ = []capability.Field{
	{Name: "customer_id", Description: "The account of the call"},
	{Name: "validate_only", Description: "true when Google only validated the change"},
	{Name: "results", Description: "Per operation, in the order of the arguments: experiment_id and resource_name " +
		"(customers/ID/experiments/EXPERIMENT_ID, as Google answered, always of the account of the call). Empty " +
		"after validate_only"},
}

var experimentsArgumentDocs = []capability.Argument{
	{Name: "customer_id", Required: true, Description: "Ten-digit customer ID of the account without hyphens; a " +
		"target of the connection or an account below a manager target. A manager account needs a manager " +
		"target that names it"},
}

const experimentsEditDoc = "name (1 to 1024 characters, unique under the account), description (1 to 2048), suffix " +
	"(1 to 255, appended to the names of the campaigns of a system-managed experiment), status (ENABLED, HALTED or " +
	"SETUP), start_date and end_date (YYYY-MM-DD, in the time zone of the account) and goals (1 to 10 pairs of " +
	"metric and direction)"

var experimentsCreate = capability.Descriptor{
	ID: experimentsCreateID, Version: 1, Title: "Create experiments in a Google Ads account",
	Description: "Create one or more experiments in a Google Ads account. name and type are required; further " +
		"fields are " + experimentsEditDoc + ", sync_enabled (syncs changes of the base campaign to the trial " +
		"campaigns, settable only here) and, for the type OPTIMIZE_ASSETS or YOUTUBE_CUSTOM, the subtype of that " +
		"type." + experimentsNote + " Offered by a connection with the create effect or whose tools list names it; " +
		"no profile holds it." + mutateListNote,
	Tags: []string{"google", "ads", "experiments", "create"}, Provider: Provider,
	Risk: changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent, experimentsSensitivity),
	InputSchema: encoded(object(schema{
		"customer_id": customerIDArgument,
		"experiments": schema{"type": "array", "minItems": 1, "maxItems": experimentsMaxOperations,
			"items": object(experimentsEditProperties(schema{
				"type":                               schema{"type": "string", "enum": experimentTypes},
				"sync_enabled":                       booleanSchema,
				"optimize_assets_experiment_subtype": schema{"type": "string", "enum": optimizeAssetsSubtypes},
				"video_experiment_subtype":           schema{"type": "string", "enum": videoExperimentSubtypes},
			}), "name", "type")},
		"validate_only": validateOnlySchema,
	}, "customer_id", "experiments")),
	OutputSchema: encoded(experimentsOutputSchema),
	Arguments: append(slices.Clone(experimentsArgumentDocs),
		capability.Argument{Name: "experiments", Required: true, Description: "1 to 10 experiments, each with name " +
			"and type (required) and optionally " + experimentsEditDoc + ", sync_enabled, " +
			"optimize_assets_experiment_subtype (only with OPTIMIZE_ASSETS) and video_experiment_subtype (only with " +
			"YOUTUBE_CUSTOM)"},
		validateOnlyArgument),
	Fields: experimentsFields_,
	Examples: []capability.Example{{Description: "Create a search experiment with a goal",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","experiments":[{"name":"Bidding test","type":"SEARCH_CUSTOM",` +
			`"goals":[{"metric":"CONVERSIONS","direction":"INCREASE"}]}]}`)}},
}

var experimentsUpdate = capability.Descriptor{
	ID: experimentsUpdateID, Version: 1, Title: "Change experiments of a Google Ads account",
	Description: "Change existing experiments of a Google Ads account (an update with a field mask that names exactly " +
		"the fields given): " + experimentsEditDoc + ". goals replaces all goals. type, sync_enabled and the subtypes " +
		"cannot be changed. Only the experiment_id selects an experiment: its resource name is built from customer_id " +
		"and experiment_id." + experimentsNote + " Offered by a connection with the update effect or whose tools list " +
		"names it; no profile holds it." + mutateListNote,
	Tags: []string{"google", "ads", "experiments", "update"}, Provider: Provider,
	Risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, experimentsSensitivity),
	InputSchema: encoded(object(schema{
		"customer_id": customerIDArgument,
		"experiments": schema{"type": "array", "minItems": 1, "maxItems": experimentsMaxOperations,
			"items": object(experimentsEditProperties(schema{"experiment_id": experimentIDSchema}), "experiment_id")},
		"validate_only": validateOnlySchema,
	}, "customer_id", "experiments")),
	OutputSchema: encoded(experimentsOutputSchema),
	Arguments: append(slices.Clone(experimentsArgumentDocs),
		capability.Argument{Name: "experiments", Required: true, Description: "1 to 10 entries, each experiment_id once " +
			"(as reports.search returns it) with at least one of " + experimentsEditDoc},
		validateOnlyArgument),
	Fields: experimentsFields_,
	Examples: []capability.Example{{Description: "Rename an experiment",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","experiments":[{"experiment_id":"112233","name":"Bidding test 2"}]}`)}},
}

var experimentsRemove = capability.Descriptor{
	ID: experimentsRemoveID, Version: 1, Title: "Remove experiments from a Google Ads account",
	Description: "Remove existing experiments of a Google Ads account (a remove of customers/ID/experiments/" +
		"EXPERIMENT_ID, built from customer_id and experiment_id). Google marks a removed experiment REMOVED; no tool " +
		"of this provider brings it back." + experimentsNote + mutateListListedNote,
	Tags: []string{"google", "ads", "experiments", "remove"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: changeRisk(capability.EffectDelete, capability.IdempotencyIdempotent, experimentsSensitivity),
	InputSchema: encoded(object(schema{
		"customer_id": customerIDArgument,
		"experiment_ids": schema{"type": "array", "minItems": 1, "maxItems": experimentsMaxOperations,
			"uniqueItems": true, "items": experimentIDSchema},
		"validate_only": validateOnlySchema,
	}, "customer_id", "experiment_ids")),
	OutputSchema: encoded(experimentsOutputSchema),
	Arguments: append(slices.Clone(experimentsArgumentDocs),
		capability.Argument{Name: "experiment_ids", Required: true, Description: "1 to 10 IDs of existing experiments, " +
			"each once, as reports.search returns them"},
		validateOnlyArgument),
	Fields: experimentsFields_,
	Examples: []capability.Example{{Description: "Validate the removal of an experiment without making it",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","experiment_ids":["112233"],"validate_only":true}`)}},
}

var experimentsListAsyncErrors = capability.Descriptor{
	ID: experimentsListAsyncErrorsID, Version: 1, Title: "List the errors of the last asynchronous operation of a Google Ads experiment",
	Description: "List the errors that occurred during the last update of an experiment, either its scheduling or its " +
		"promotion (ExperimentService.ListExperimentAsyncErrors of API version v26, GET customers/ID/experiments/" +
		"EXPERIMENT_ID:listExperimentAsyncErrors, paged with a page token that Qatlas Connect wraps in an opaque " +
		"cursor). The state of the experiment itself is read with reports.search on the experiment resource. " +
		"experiment_id must belong to customer_id; Qatlas Connect builds the resource name from both. customer_id " +
		"must be one of the targets or lie below a manager target; on a connection with targets, a manager account " +
		"needs a manager target that names it. Costs one operation of the daily quota, plus one for a membership " +
		"read below a manager target and one for the check that the account is no manager account. The messages " +
		"are untrusted provider text and are cut; the details of a status are not returned.",
	Tags: []string{"google", "ads", "experiments", "async-errors"}, Provider: Provider,
	Risk: capability.Risk{Effect: capability.EffectRead, Idempotency: capability.IdempotencySafe,
		Confirmation: capability.ConfirmationNone, OpenWorld: true,
		DataSensitivity: "error codes and messages of the asynchronous operations of an experiment of a Google Ads " +
			"account; the messages are provider text and may name settings of the account; no data of other people"},
	InputSchema: encoded(object(schema{"customer_id": customerIDArgument, "experiment_id": experimentIDSchema,
		"page_size": asyncErrorsPageProperties["page_size"], "cursor": asyncErrorsPageProperties["cursor"]},
		"customer_id", "experiment_id")),
	OutputSchema: encoded(asyncErrorsOutputSchema),
	Arguments: append(append(slices.Clone(experimentsArgumentDocs),
		capability.Argument{Name: "experiment_id", Required: true, Description: "ID of an experiment of the account, as " +
			"reports.search returns it"}), asyncErrorsPageArguments...),
	Fields: asyncErrorsFields,
	Examples: []capability.Example{{Description: "List the errors of the last scheduling of an experiment",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","experiment_id":"112233"}`)}},
}

// --- handlers ---

func handleExperimentsCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "create experiments"
	var args experimentsCreateArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	ops, err := args.operations()
	if err != nil {
		return nil, err
	}
	return experimentsSend(ctx, resolved, secrets, red, op, experimentsCreateID, args.CustomerID, ops, nil, args.ValidateOnly)
}

func handleExperimentsUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "update experiments"
	var args experimentsUpdateArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	ops, names, err := args.operations()
	if err != nil {
		return nil, err
	}
	return experimentsSend(ctx, resolved, secrets, red, op, experimentsUpdateID, args.CustomerID, ops, names, args.ValidateOnly)
}

func handleExperimentsRemove(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "remove experiments"
	var args experimentsRemoveArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	ops, names, err := args.operations()
	if err != nil {
		return nil, err
	}
	return experimentsSend(ctx, resolved, secrets, red, op, experimentsRemoveID, args.CustomerID, ops, names, args.ValidateOnly)
}

func experimentsSend(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
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
		service: experimentsService, collection: experimentsCollection, operations: ops, names: want,
		idOf: func(name string) (string, bool) { return experimentIDOfName(name, customerID) }, validateOnly: validateOnly})
	if err != nil {
		return nil, err
	}
	out := make([]experimentsMutated, 0, len(results))
	seen := map[string]bool{}
	for _, r := range results {
		if seen[r.ID] {
			return nil, unsureResult(op, "answered with the same experiment twice")
		}
		seen[r.ID] = true
		out = append(out, experimentsMutated{ExperimentID: r.ID, ResourceName: r.ResourceName})
	}
	return experimentsChanged{CustomerID: customerID, ValidateOnly: validateOnly, Results: out}, nil
}

func handleExperimentsListAsyncErrors(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list experiment async errors"
	var args experimentsListAsyncErrorsArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if !customerIDPattern.MatchString(args.CustomerID) {
		return nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := checkID("experiment_id", args.ExperimentID); err != nil {
		return nil, err
	}
	env, err := handleEnv(ctx, resolved, secrets, red, op, experimentsListAsyncErrorsID)
	if err != nil {
		return nil, err
	}
	return env.listAsyncErrors(asyncErrorsSpec{op: op, tool: experimentsListAsyncErrorsID, customerID: args.CustomerID,
		resource: experimentName(args.CustomerID, args.ExperimentID), method: "listExperimentAsyncErrors",
		pageSize: args.PageSize, cursor: args.Cursor})
}

// --- confirmation ---

func experimentsGoalText(goals []experimentsGoal) string {
	parts := make([]string, 0, len(goals))
	for _, g := range goals[:min(len(goals), experimentGoalsMax)] {
		parts = append(parts, boundedText(g.Direction, maxCodeText)+" "+boundedText(g.Metric, maxCodeText))
	}
	return strings.Join(parts, " and ")
}

func experimentsOptional(label string, value *string) string {
	if value == nil {
		return ""
	}
	return " " + label + " " + campaignGroupsQuote(*value)
}

func experimentsCreateSummary(raw json.RawMessage) string {
	var a experimentsCreateArguments
	_ = json.Unmarshal(raw, &a)
	var parts []string
	for _, e := range a.Experiments[:min(len(a.Experiments), 10)] {
		part := campaignGroupsQuote(e.Name) + " (type " + boundedText(e.Type, maxCodeText) + ")" +
			experimentsOptional("status", e.Status) + experimentsOptional("start", e.StartDate) +
			experimentsOptional("end", e.EndDate)
		if len(e.Goals) > 0 {
			part += " goals " + experimentsGoalText(e.Goals)
		}
		parts = append(parts, part)
	}
	return campaignGroupsLead(a.ValidateOnly) + "Create " + strconv.Itoa(len(a.Experiments)) +
		" experiment(s) in Google Ads account " + a.CustomerID + ": " + strings.Join(parts, "; ") +
		campaignGroupsMore(len(a.Experiments), len(parts)) + "."
}

func experimentsUpdateSummary(raw json.RawMessage) string {
	var a experimentsUpdateArguments
	_ = json.Unmarshal(raw, &a)
	var parts []string
	for _, e := range a.Experiments[:min(len(a.Experiments), 10)] {
		part := "experiment " + boundedText(e.ExperimentID, maxCodeText) + experimentsOptional("name", e.Name) +
			experimentsOptional("suffix", e.Suffix) + experimentsOptional("status", e.Status) +
			experimentsOptional("start", e.StartDate) + experimentsOptional("end", e.EndDate)
		if e.Description != nil {
			part += " description"
		}
		if len(e.Goals) > 0 {
			part += " goals " + experimentsGoalText(e.Goals)
		}
		parts = append(parts, part)
	}
	return campaignGroupsLead(a.ValidateOnly) + "Change " + strconv.Itoa(len(a.Experiments)) +
		" experiment(s) of Google Ads account " + a.CustomerID + ": " + strings.Join(parts, "; ") +
		campaignGroupsMore(len(a.Experiments), len(parts)) + "."
}

func experimentsRemoveSummary(raw json.RawMessage) string {
	var a experimentsRemoveArguments
	_ = json.Unmarshal(raw, &a)
	shown := make([]string, 0, 10)
	for _, id := range a.ExperimentIDs[:min(len(a.ExperimentIDs), 10)] {
		shown = append(shown, boundedText(id, maxCodeText))
	}
	return campaignGroupsLead(a.ValidateOnly) + "Remove " + strconv.Itoa(len(a.ExperimentIDs)) +
		" experiment(s) from Google Ads account " + a.CustomerID + ": " + strings.Join(shown, ", ") +
		campaignGroupsMore(len(a.ExperimentIDs), len(shown)) + ". Google marks them removed; no tool brings them back."
}
