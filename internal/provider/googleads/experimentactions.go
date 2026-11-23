package googleads

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	experimentsScheduleID = Provider + ".experiments.schedule"
	experimentsEndID      = Provider + ".experiments.end"
	experimentsGraduateID = Provider + ".experiments.graduate"
	experimentsPromoteID  = Provider + ".experiments.promote"

	// graduateMappingsMax is the bound the reference of v26 states for campaign_budget_mappings: "The maximum size is one."
	graduateMappingsMax = 1
)

// --- arguments and results ---

type (
	experimentActionArguments struct {
		CustomerID   string `json:"customer_id"`
		ExperimentID string `json:"experiment_id"`
		ValidateOnly bool   `json:"validate_only"`
	}
	experimentsBudgetMapping struct {
		ExperimentCampaignID string `json:"experiment_campaign_id"`
		CampaignBudgetID     string `json:"campaign_budget_id"`
	}
	experimentsGraduateArguments struct {
		CustomerID             string                     `json:"customer_id"`
		ExperimentID           string                     `json:"experiment_id"`
		CampaignBudgetMappings []experimentsBudgetMapping `json:"campaign_budget_mappings"`
		ValidateOnly           bool                       `json:"validate_only"`
	}
	// experimentActed is the result of ending and graduating: Google answers with an empty message.
	experimentActed struct {
		CustomerID   string `json:"customer_id"`
		ExperimentID string `json:"experiment_id"`
		ValidateOnly bool   `json:"validate_only"`
	}
	// experimentOperated is the result of scheduling and promoting: Google answers with a long-running operation.
	experimentOperated struct {
		CustomerID   string      `json:"customer_id"`
		ExperimentID string      `json:"experiment_id"`
		ValidateOnly bool        `json:"validate_only"`
		Operation    string      `json:"operation,omitempty"`
		Done         bool        `json:"done"`
		Error        *asyncError `json:"error,omitempty"`
	}
)

// check validates the arguments all four tools share.
func (a experimentActionArguments) check() error {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	return checkID("experiment_id", a.ExperimentID)
}

// mappings builds the wire form of the budget mappings from checked IDs of the account alone.
func (a experimentsGraduateArguments) mappings() ([]any, error) {
	if len(a.CampaignBudgetMappings) < 1 || len(a.CampaignBudgetMappings) > graduateMappingsMax {
		return nil, badRequest("campaign_budget_mappings must hold exactly one entry")
	}
	out := make([]any, 0, len(a.CampaignBudgetMappings))
	for _, m := range a.CampaignBudgetMappings {
		if err := checkID("experiment_campaign_id", m.ExperimentCampaignID); err != nil {
			return nil, err
		}
		if err := checkID("campaign_budget_id", m.CampaignBudgetID); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{
			"experimentCampaign": customerName(a.CustomerID) + "/campaigns/" + m.ExperimentCampaignID,
			"campaignBudget":     customerName(a.CustomerID) + "/campaignBudgets/" + m.CampaignBudgetID})
	}
	return out, nil
}

// --- descriptors ---

var experimentActionOutputProperties = schema{"customer_id": stringSchema, "experiment_id": stringSchema,
	"validate_only": booleanSchema}

var experimentActedSchema = object(experimentActionOutputProperties, "customer_id", "experiment_id", "validate_only")

var experimentOperatedSchema = object(schema{"customer_id": stringSchema, "experiment_id": stringSchema,
	"validate_only": booleanSchema, "operation": stringSchema, "done": booleanSchema,
	"error": object(schema{"code": integerSchema, "message": stringSchema}, "code")},
	"customer_id", "experiment_id", "validate_only", "done")

const experimentActionSensitivity = "an experiment of a Google Ads account and the campaigns it changes, which the " +
	"confirmation names; the change can alter what the account serves and spends; no data of other people"

const experimentActionNote = " The experiment is selected by experiment_id alone: Qatlas Connect builds its resource " +
	"name from customer_id and experiment_id. Reading the experiment (status, dates) is reports.search on the " +
	"experiment resource, and the errors of a failed asynchronous step are read with experiments.list-async-errors. " +
	"validate_only asks Google to validate the request without making it; Google then answers errors only. The " +
	"request is sent once and never repeated."

const experimentOperationNote = " Google answers with a long-running operation and only a done state: the tool " +
	"returns its name (untrusted provider text, cut), done, and an error when the operation reports one, and no tool " +
	"polls an operation. The outcome is the status of the experiment, read with reports.search on experiment.status."

const experimentEmptyNote = " Google answers with an empty message, so the result only repeats the call: this " +
	"is no long-running operation. The outcome is the status of the experiment, read with reports.search on " +
	"experiment.status."

func experimentActionArgumentDocs(experimentDoc string) []capability.Argument {
	return append(slices.Clone(experimentsArgumentDocs),
		capability.Argument{Name: "experiment_id", Required: true, Description: experimentDoc})
}

func experimentActionDescriptor(id, title, description, tag string, output schema) capability.Descriptor {
	return capability.Descriptor{
		ID: id, Version: 1, Title: title, Description: description + experimentActionNote + mutateNote,
		Tags: []string{"google", "ads", "experiments", tag}, Provider: Provider, RequiresToolAllowList: true,
		Risk: changeRisk(capability.EffectUpdate, capability.IdempotencyNonIdempotent, experimentActionSensitivity),
		InputSchema: encoded(object(schema{"customer_id": customerIDArgument, "experiment_id": experimentIDSchema,
			"validate_only": validateOnlySchema}, "customer_id", "experiment_id")),
		OutputSchema: encoded(output),
	}
}

var experimentActedFields = []capability.Field{
	{Name: "customer_id", Description: "The account of the call"},
	{Name: "experiment_id", Description: "The experiment of the call"},
	{Name: "validate_only", Description: "true when Google only validated the request"},
}

var experimentOperatedFields = append(slices.Clone(experimentActedFields),
	capability.Field{Name: "operation", Description: "Name of the long-running operation as Google answered, an " +
		"untrusted provider text that is cut; absent when Google gave none"},
	capability.Field{Name: "done", Description: "true when Google reports the operation as done; the outcome is the " +
		"status of the experiment"},
	capability.Field{Name: "error", Description: "code and message (cut, untrusted) when the operation reports an " +
		"error; the details of the status are not returned"})

var experimentsSchedule = func() capability.Descriptor {
	d := experimentActionDescriptor(experimentsScheduleID, "Schedule an experiment of a Google Ads account",
		"Schedule an experiment (ExperimentService.ScheduleExperiment of API version v26, POST customers/ID/experiments/"+
			"EXPERIMENT_ID:scheduleExperiment): Google turns the experiment's in-design campaign into a real "+
			"experiment campaign that begins to serve ads, with the experiment first INITIALIZING. This starts ad "+
			"serving and spend. If creating the campaign fails, the errors are read with experiments.list-async-errors."+
			experimentOperationNote, "schedule", experimentOperatedSchema)
	d.Arguments = append(experimentActionArgumentDocs("The experiment of the account to schedule, as reports.search "+
		"returns it"), validateOnlyArgument)
	d.Fields = experimentOperatedFields
	d.Examples = []capability.Example{{Description: "Validate the scheduling of an experiment without making it",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","experiment_id":"112233","validate_only":true}`)}}
	return d
}()

var experimentsEnd = func() capability.Descriptor {
	d := experimentActionDescriptor(experimentsEndID, "End an experiment of a Google Ads account",
		"End an experiment immediately (ExperimentService.EndExperiment of API version v26, POST customers/ID/"+
			"experiments/EXPERIMENT_ID:endExperiment): Google sets the end date of the experiment to the time of the "+
			"request without waiting for the end of the day. The trial campaigns stop; an ended experiment does "+
			"not run again."+experimentEmptyNote, "end", experimentActedSchema)
	d.Arguments = append(experimentActionArgumentDocs("The experiment of the account to end, as reports.search "+
		"returns it"), validateOnlyArgument)
	d.Fields = experimentActedFields
	d.Examples = []capability.Example{{Description: "Validate the end of an experiment without making it",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","experiment_id":"112233","validate_only":true}`)}}
	return d
}()

var experimentsGraduate = func() capability.Descriptor {
	d := experimentActionDescriptor(experimentsGraduateID, "Graduate an experiment of a Google Ads account",
		"Graduate an experiment to a full campaign (ExperimentService.GraduateExperiment of API version v26, POST "+
			"customers/ID/experiments/EXPERIMENT_ID:graduateExperiment): the experiment campaign named in "+
			"campaign_budget_mappings becomes a campaign of its own with the campaign budget paired to it, which "+
			"changes what the account spends. Both IDs must belong to customer_id; Qatlas Connect builds the "+
			"resource names from them and never takes a resource name from the caller."+experimentEmptyNote,
		"graduate", experimentActedSchema)
	d.InputSchema = encoded(object(schema{"customer_id": customerIDArgument, "experiment_id": experimentIDSchema,
		"campaign_budget_mappings": schema{"type": "array", "minItems": 1, "maxItems": graduateMappingsMax,
			"items": object(schema{"experiment_campaign_id": experimentIDSchema, "campaign_budget_id": experimentIDSchema},
				"experiment_campaign_id", "campaign_budget_id")},
		"validate_only": validateOnlySchema}, "customer_id", "experiment_id", "campaign_budget_mappings"))
	d.Arguments = append(experimentActionArgumentDocs("The experiment of the account to graduate, as reports.search "+
		"returns it"),
		capability.Argument{Name: "campaign_budget_mappings", Required: true, Description: "Exactly one entry (the " +
			"reference allows at most one) with experiment_campaign_id (the ID of the experiment campaign of the " +
			"account to graduate) and campaign_budget_id (the ID of an existing campaign budget of the account that " +
			"becomes the budget of the graduated campaign)"},
		validateOnlyArgument)
	d.Fields = experimentActedFields
	d.Examples = []capability.Example{{Description: "Validate the graduation of an experiment campaign with a budget",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","experiment_id":"112233","campaign_budget_mappings":` +
			`[{"experiment_campaign_id":"2001","campaign_budget_id":"3001"}],"validate_only":true}`)}}
	return d
}()

var experimentsPromote = func() capability.Descriptor {
	d := experimentActionDescriptor(experimentsPromoteID, "Promote an experiment of a Google Ads account",
		"Promote an experiment (ExperimentService.PromoteExperiment of API version v26, POST customers/ID/experiments/"+
			"EXPERIMENT_ID:promoteExperiment): the changes of the trial campaign are applied to the base campaign, "+
			"which then serves with them and can change its targeting, ads and spend. If it fails, the errors are "+
			"read with experiments.list-async-errors."+experimentOperationNote, "promote", experimentOperatedSchema)
	d.Arguments = append(experimentActionArgumentDocs("The experiment of the account to promote, as reports.search "+
		"returns it"), validateOnlyArgument)
	d.Fields = experimentOperatedFields
	d.Examples = []capability.Example{{Description: "Validate the promotion of an experiment without making it",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","experiment_id":"112233","validate_only":true}`)}}
	return d
}()

// --- handlers ---

// experimentActionSend checks the account first, then sends the one request of an action on an experiment and decodes
// the answer into out. The resource name is built from the checked IDs.
func experimentActionSend(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	op, tool, customerID, experimentID, method string, body map[string]any, out any) error {
	env, err := handleEnv(ctx, resolved, secrets, red, op, tool)
	if err != nil {
		return err
	}
	c, err := env.settle(customerID, true)
	if err != nil {
		return err
	}
	encodedBody, err := json.Marshal(body)
	if err != nil {
		return failure(op, "the request body could not be encoded")
	}
	return c.send(ctx, request{op: op, tool: tool, method: http.MethodPost,
		path: "/" + experimentName(customerID, experimentID) + ":" + method, body: encodedBody, onAccount: true,
		change: true}, out)
}

func validateBody(validateOnly bool, body map[string]any) map[string]any {
	if validateOnly {
		body["validateOnly"] = true
	}
	return body
}

// handleExperimentOperation serves schedule and promote: Google answers with a long-running operation.
func handleExperimentOperation(op, tool, method string) capability.Handler {
	return func(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
		raw json.RawMessage) (any, error) {
		var args experimentActionArguments
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, failure(op, "the validated arguments could not be read")
		}
		if err := args.check(); err != nil {
			return nil, err
		}
		var answer struct {
			Name  string `json:"name"`
			Done  bool   `json:"done"`
			Error *struct {
				Code    *int   `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := experimentActionSend(ctx, resolved, secrets, red, op, tool, args.CustomerID, args.ExperimentID, method,
			validateBody(args.ValidateOnly, map[string]any{}), &answer); err != nil {
			return nil, err
		}
		out := experimentOperated{CustomerID: args.CustomerID, ExperimentID: args.ExperimentID,
			ValidateOnly: args.ValidateOnly, Operation: boundedText(answer.Name, maxText), Done: answer.Done}
		if e := answer.Error; e != nil {
			out.Error = &asyncError{Message: boundedText(e.Message, maxText)}
			if e.Code != nil {
				out.Error.Code = *e.Code
			}
		}
		return out, nil
	}
}

func handleExperimentsEnd(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "end experiment"
	var args experimentActionArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if err := args.check(); err != nil {
		return nil, err
	}
	var answer map[string]json.RawMessage
	if err := experimentActionSend(ctx, resolved, secrets, red, op, experimentsEndID, args.CustomerID, args.ExperimentID,
		"endExperiment", validateBody(args.ValidateOnly, map[string]any{}), &answer); err != nil {
		return nil, err
	}
	return experimentActed{CustomerID: args.CustomerID, ExperimentID: args.ExperimentID, ValidateOnly: args.ValidateOnly}, nil
}

func handleExperimentsGraduate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "graduate experiment"
	var args experimentsGraduateArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if err := (experimentActionArguments{CustomerID: args.CustomerID, ExperimentID: args.ExperimentID}).check(); err != nil {
		return nil, err
	}
	mappings, err := args.mappings()
	if err != nil {
		return nil, err
	}
	var answer map[string]json.RawMessage
	if err := experimentActionSend(ctx, resolved, secrets, red, op, experimentsGraduateID, args.CustomerID,
		args.ExperimentID, "graduateExperiment",
		validateBody(args.ValidateOnly, map[string]any{"campaignBudgetMappings": mappings}), &answer); err != nil {
		return nil, err
	}
	return experimentActed{CustomerID: args.CustomerID, ExperimentID: args.ExperimentID, ValidateOnly: args.ValidateOnly}, nil
}

// --- confirmation ---

func experimentActionSummary(verb, effect string) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		var a experimentActionArguments
		_ = json.Unmarshal(raw, &a)
		return campaignGroupsLead(a.ValidateOnly) + verb + " experiment " + boundedText(a.ExperimentID, maxCodeText) +
			" of Google Ads account " + a.CustomerID + ". " + effect
	}
}

var (
	experimentsScheduleSummary = experimentActionSummary("Schedule", "Its campaign then begins to serve ads and spend budget.")
	experimentsEndSummary      = experimentActionSummary("End", "The experiment stops now and does not run again.")
	experimentsPromoteSummary  = experimentActionSummary("Promote", "The base campaign then serves with the changes of "+
		"the trial campaign, which can change its targeting, ads and spend.")
)

func experimentsGraduateSummary(raw json.RawMessage) string {
	var a experimentsGraduateArguments
	_ = json.Unmarshal(raw, &a)
	pair := ""
	if len(a.CampaignBudgetMappings) > 0 {
		m := a.CampaignBudgetMappings[0]
		pair = " as experiment campaign " + boundedText(m.ExperimentCampaignID, maxCodeText) + " with campaign budget " +
			boundedText(m.CampaignBudgetID, maxCodeText)
	}
	return campaignGroupsLead(a.ValidateOnly) + "Graduate experiment " + boundedText(a.ExperimentID, maxCodeText) +
		" of Google Ads account " + a.CustomerID + " to a campaign of its own" + pair +
		". The graduated campaign then spends from that budget."
}
