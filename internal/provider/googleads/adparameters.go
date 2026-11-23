package googleads

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// Ad parameters (AdParameterService.MutateAdParameters) put a number, such as a price, into the {param1:default}
// placeholder of an ad text. The resource has three members of its own: the keyword it belongs to and the index
// (1 or 2), both immutable, and the insertion text, the one member an update changes. Google allows at most two
// ad parameters per ad group criterion, and refuses a criterion that is no keyword.

const (
	adParametersCreateID = Provider + ".ad-parameters.create"
	adParametersUpdateID = Provider + ".ad-parameters.update"
	adParametersRemoveID = Provider + ".ad-parameters.remove"

	adParameterService = "/adParameters"

	maxAdParameters = 10
	// maxInsertionText is Qatlas's own bound: Google names none, and the text lands in a line of an ad.
	maxInsertionText = 50
)

const adParameterSensitivity = "ad parameters of a Google Ads account: the numbers, such as prices, that ads show " +
	"in their text once they serve"

const (
	adParameterNamePattern = `^customers/[0-9]{1,10}/adParameters/[0-9]{1,20}~[0-9]{1,20}~[12]$`
	criterionNamePattern   = `^customers/[0-9]{1,10}/adGroupCriteria/[0-9]{1,20}~[0-9]{1,20}$`
)

// compositeIDs splits customers/ID/<collection>/A~B~... of exactly this account into its parts. A zero or a
// temporary (negative) ID never matches.
func compositeIDs(name, customerID, collection string, parts int) ([]string, bool) {
	rest, ok := strings.CutPrefix(name, customerName(customerID)+"/"+collection+"/")
	if !ok {
		return nil, false
	}
	ids := strings.Split(rest, "~")
	if len(ids) != parts {
		return nil, false
	}
	for _, id := range ids {
		if !numberPattern.MatchString(id) || strings.Trim(id, "0") == "" {
			return nil, false
		}
	}
	return ids, true
}

// adParameterIDs splits an ad parameter name of this account into ad group, criterion and index, which is 1 or 2.
func adParameterIDs(name, customerID string) (group, criterion, index string, ok bool) {
	ids, ok := compositeIDs(name, customerID, "adParameters", 3)
	if !ok || (ids[2] != "1" && ids[2] != "2") {
		return "", "", "", false
	}
	return ids[0], ids[1], ids[2], true
}

// checkInsertionText refuses what Google's description of the insertion text cannot allow: it holds a digit, and
// besides digits only the separators . and , the signs + - and %, the / between two numbers, a currency symbol,
// and one currency code of three capital letters. Whether the form is right is Google's to say.
func checkInsertionText(argument, value string) error {
	bad := func() error {
		return badRequest(argument + " must be a number as Google documents it, such as 99.99, $99.99, 99.99USD, 1.0%, " +
			"-10.99, 25+ or 4/1, of 1 to " + strconv.Itoa(maxInsertionText) + " bytes")
	}
	if value == "" || len(value) > maxInsertionText {
		return bad()
	}
	digits, letters, runs := 0, 0, 0
	for _, r := range value {
		switch {
		case r >= '0' && r <= '9':
			digits++
			letters = 0
		case r >= 'A' && r <= 'Z':
			if letters == 0 {
				runs++
			}
			letters++
			if letters > 3 || runs > 1 {
				return bad()
			}
		case r == '.' || r == ',' || r == '%' || r == '+' || r == '-' || r == '/' || unicode.Is(unicode.Sc, r):
			if letters != 0 && letters != 3 {
				return bad()
			}
			letters = 0
		default:
			return bad()
		}
	}
	if digits == 0 || (letters != 0 && letters != 3) {
		return bad()
	}
	return nil
}

func adParameterMembers() []node {
	criterion := textNode("ad_group_criterion", "Keyword of this account the parameter belongs to, "+
		"customers/ID/adGroupCriteria/AD_GROUP_ID~CRITERION_ID; Google refuses a criterion that is no keyword; fixed "+
		"after creation", 100)
	criterion.pattern = criterionNamePattern
	insertion := textNode("insertion_text", "The number put into the {param1:default} or {param2:default} placeholder of "+
		"the ads of the keyword: digits with . or , as separator, a currency symbol or a three-letter currency code, "+
		"%, a sign, or / between two numbers, such as 99.99, $99.99, EUR200, 1.0%, -10.99, 25+ or 4/1", maxInsertionText)
	insertion.check = checkInsertionText
	return []node{createOnly(criterion),
		createOnly(wholeNode("parameter_index", "1 for {param1:...} or 2 for {param2:...}; one of each per keyword at most; "+
			"fixed after creation", 1, 2)),
		insertion}
}

var adParameterTree = objectNode("ad_parameter", "", adParameterMembers()...)

// adParameterOutputTree is the ad parameter Google returns with MUTABLE_RESOURCE: the members the tools offer
// and the resource name.
var adParameterOutputTree = func() node {
	t := adParameterTree
	t.fields = append(slices.Clone(t.fields), textNode("resource_name", "", maxNameArgument))
	return t
}()

var adParameterRequiredOnCreate = []string{"ad_group_criterion", "parameter_index", "insertion_text"}

type adParameterArguments struct {
	CustomerID          string   `json:"customer_id"`
	AdParameters        []any    `json:"ad_parameters"`
	ResourceNames       []string `json:"resource_names"`
	ValidateOnly        bool     `json:"validate_only"`
	ResponseContentType string   `json:"response_content_type"`
}

func readAdParameterArguments(op string, raw json.RawMessage) (adParameterArguments, error) {
	var a adParameterArguments
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&a); err != nil {
		return a, failure(op, "the validated arguments could not be read")
	}
	return a, nil
}

func adParameterItem(items []any, i int) (map[string]any, pathOfMember, error) {
	at := pathOfMember{snake: fmt.Sprintf("ad_parameters[%d]", i)}
	in, ok := items[i].(map[string]any)
	if !ok {
		return nil, at, badRequest(at.snake + " must be an object")
	}
	return in, at, nil
}

// adParameterCreateOperations checks every ad parameter to create and builds its operation. The names of the
// ad parameters Google must answer with are returned for the check of the answer: they follow from the keyword
// and the index.
func adParameterCreateOperations(customerID string, items []any) ([]any, []string, error) {
	if len(items) == 0 || len(items) > maxAdParameters {
		return nil, nil, badRequest("ad_parameters must hold one to " + strconv.Itoa(maxAdParameters) + " ad parameters")
	}
	ops := make([]any, 0, len(items))
	names := make([]string, 0, len(items))
	for i := range items {
		in, at, err := adParameterItem(items, i)
		if err != nil {
			return nil, nil, err
		}
		for _, required := range adParameterRequiredOnCreate {
			if _, found := in[required]; !found {
				return nil, nil, badRequest(at.snake + "." + required + " is required")
			}
		}
		b := builder{customerID: customerID, create: true}
		body, err := b.object(adParameterTree, in, at)
		if err != nil {
			return nil, nil, err
		}
		criterion, _ := body["adGroupCriterion"].(string)
		ids, ok := compositeIDs(criterion, customerID, "adGroupCriteria", 2)
		if !ok {
			return nil, nil, badRequest(at.snake + ".ad_group_criterion must be customers/" + queryNumber(customerID) +
				"/adGroupCriteria/AD_GROUP_ID~CRITERION_ID of the account of customer_id; a temporary negative ID is not accepted")
		}
		index, _ := body["parameterIndex"].(json.Number)
		name := customerName(customerID) + "/adParameters/" + ids[0] + "~" + ids[1] + "~" + index.String()
		if slices.Contains(names, name) {
			return nil, nil, badRequest("a keyword and index appear in ad_parameters only once")
		}
		names = append(names, name)
		ops = append(ops, map[string]any{"create": body})
	}
	return ops, names, nil
}

// adParameterUpdateOperations builds one update with a field mask per ad parameter. Each is named once.
func adParameterUpdateOperations(customerID string, items []any) ([]any, []string, error) {
	if len(items) == 0 || len(items) > maxAdParameters {
		return nil, nil, badRequest("ad_parameters must hold one to " + strconv.Itoa(maxAdParameters) + " ad parameters")
	}
	tree := adParameterTree.mutable()
	ops := make([]any, 0, len(items))
	names := make([]string, 0, len(items))
	for i := range items {
		in, at, err := adParameterItem(items, i)
		if err != nil {
			return nil, nil, err
		}
		name, _ := in["resource_name"].(string)
		if _, _, _, ok := adParameterIDs(name, customerID); !ok {
			return nil, nil, badRequest(at.snake + ".resource_name must be customers/" + queryNumber(customerID) +
				"/adParameters/AD_GROUP_ID~CRITERION_ID~INDEX of the account of customer_id, with the index 1 or 2")
		}
		if slices.Contains(names, name) {
			return nil, nil, badRequest("an ad parameter appears in ad_parameters only once")
		}
		fields := map[string]any{}
		for key, value := range in {
			if key != "resource_name" {
				fields[key] = value
			}
		}
		if _, found := fields["insertion_text"]; !found || len(fields) != 1 {
			return nil, nil, badRequest(at.snake + " must give insertion_text, the one member that can change")
		}
		b := builder{customerID: customerID}
		body, err := b.object(tree, fields, at)
		if err != nil {
			return nil, nil, err
		}
		body["resourceName"] = name
		names = append(names, name)
		ops = append(ops, map[string]any{"update": body, "updateMask": strings.Join(b.mask, ",")})
	}
	return ops, names, nil
}

func adParameterRemoveOperations(customerID string, names []string) ([]any, error) {
	if len(names) == 0 || len(names) > maxAdParameters {
		return nil, badRequest("resource_names must hold one to " + strconv.Itoa(maxAdParameters) + " ad parameters")
	}
	ops := make([]any, 0, len(names))
	for i, name := range names {
		if _, _, _, ok := adParameterIDs(name, customerID); !ok {
			return nil, badRequest(fmt.Sprintf("resource_names[%d] must be customers/%s/adParameters/AD_GROUP_ID~"+
				"CRITERION_ID~INDEX of the account of customer_id, with the index 1 or 2", i, queryNumber(customerID)))
		}
		if slices.Contains(names[:i], name) {
			return nil, badRequest("an ad parameter appears in resource_names only once")
		}
		ops = append(ops, map[string]any{"remove": name})
	}
	return ops, nil
}

// --- result ---

type (
	adParameterResult struct {
		ResourceName string         `json:"resource_name"`
		AdParameter  map[string]any `json:"ad_parameter,omitempty"`
	}
	adParametersChanged struct {
		CustomerID   string              `json:"customer_id"`
		ValidateOnly bool                `json:"validate_only"`
		Results      []adParameterResult `json:"results"`
	}
)

// adParameterAnswer is the answer of MutateAdParameters.
type adParameterAnswer struct {
	Results []struct {
		ResourceName string          `json:"resourceName"`
		AdParameter  json.RawMessage `json:"adParameter"`
	} `json:"results"`
	PartialFailureError json.RawMessage `json:"partialFailureError"`
}

// adParametersOf maps the results of a change. One result per operation, in the order sent, each exactly the
// ad parameter asked for or, for a create, the one that follows from the keyword and the index. Anything else
// is an invalid answer with an unknown outcome, since the change may have been made.
func adParametersOf(op string, answer adParameterAnswer, customerID string, validateOnly bool,
	expected []string) (*adParametersChanged, error) {
	out := &adParametersChanged{CustomerID: customerID, ValidateOnly: validateOnly, Results: []adParameterResult{}}
	invalid := func(what string) error {
		return unsure(provider.InvalidResponse(op, subject+" answered with "+what), true)
	}
	if len(answer.PartialFailureError) > 0 && string(answer.PartialFailureError) != "null" {
		return nil, invalid("a partial failure although none was asked for")
	}
	if validateOnly && len(answer.Results) == 0 {
		return out, nil
	}
	if len(answer.Results) != len(expected) {
		return nil, invalid("another number of results than operations")
	}
	for i, r := range answer.Results {
		if _, _, _, ok := adParameterIDs(r.ResourceName, customerID); !ok || r.ResourceName != expected[i] {
			return nil, invalid("a result for another ad parameter or account than the one asked for")
		}
		item := adParameterResult{ResourceName: r.ResourceName}
		if len(r.AdParameter) > 0 && string(r.AdParameter) != "null" {
			raw, ok := decodeObject(r.AdParameter)
			var members map[string]any
			if ok {
				members, ok = outputMembers(adParameterOutputTree.fields, raw, customerID)
			}
			if ok {
				ok = adParameterMatches(members, r.ResourceName, customerID)
			}
			if !ok {
				return nil, invalid("an ad parameter that is not the one asked for")
			}
			item.AdParameter = members
		}
		out.Results = append(out.Results, item)
	}
	return out, nil
}

// adParameterMatches checks that the members of a returned ad parameter agree with its name and the account.
func adParameterMatches(members map[string]any, name, customerID string) bool {
	if v, given := members["resource_name"]; given && v != name {
		return false
	}
	group, criterion, index, _ := adParameterIDs(name, customerID)
	if v, given := members["ad_group_criterion"]; given &&
		v != customerName(customerID)+"/adGroupCriteria/"+group+"~"+criterion {
		return false
	}
	if v, given := members["parameter_index"]; given && fmt.Sprint(v) != index {
		return false
	}
	return true
}

// --- descriptors ---

func adParameterInputSchema(tool string) schema {
	var item schema
	switch tool {
	case adParametersCreateID:
		item = adParameterTree.inputSchema()
		item["required"] = adParameterRequiredOnCreate
		delete(item, "description")
	default:
		item = adParameterTree.mutable().inputSchema()
		props := item["properties"].(schema)
		props["resource_name"] = schema{"type": "string", "maxLength": 100, "pattern": adParameterNamePattern,
			"description": "Ad parameter of this account, customers/ID/adParameters/AD_GROUP_ID~CRITERION_ID~INDEX"}
		item["required"] = []string{"resource_name", "insertion_text"}
		item["minProperties"] = 2
	}
	return object(schema{"customer_id": customerIDArgument,
		"ad_parameters": schema{"type": "array", "minItems": 1, "maxItems": maxAdParameters, "items": item},
		"validate_only": validateOnlySchema, "response_content_type": responseContentTypeSchema},
		"customer_id", "ad_parameters")
}

var adParameterOutputSchema = object(schema{"customer_id": stringSchema, "validate_only": booleanSchema,
	"results": arrayOf(object(schema{"resource_name": stringSchema, "ad_parameter": adParameterOutputTree.outputSchema()},
		"resource_name"))}, "customer_id", "validate_only", "results")

var adParameterFields = []capability.Field{
	{Name: "customer_id", Description: "The account of the call"},
	{Name: "validate_only", Description: "true when Google only validated the change"},
	{Name: "results", Description: "One entry per ad parameter, in the order given, with its resource_name and, with " +
		"MUTABLE_RESOURCE, the ad parameter after the change; empty after validate_only"}}

const adParameterNote = "An ad parameter fills the {param1:default} or {param2:default} placeholder in the text of " +
	"the ads of one keyword, and the text is public once the ads serve; the call is confirmed per call. " +
	"ad_group_criterion must belong to customer_id. Up to 10 ad parameters per call, one atomic request without " +
	"partial failure; Google allows two per keyword at most, one per index."

var adParametersCreate = capability.Descriptor{
	ID: adParametersCreateID, Version: 1, Title: "Create Google Ads ad parameters",
	Description: "Create up to 10 ad parameters for keywords of a Google Ads account (AdParameterService." +
		"MutateAdParameters with create operations): Google applies all of them or none. Each entry needs " +
		"ad_group_criterion (the keyword), parameter_index (1 or 2) and insertion_text (a number such as 99.99, " +
		"$99.99, EUR200, 1.0%, 25+ or 4/1; Qatlas asks for it, Google does not require it). " + adParameterNote + mutateListNote,
	Tags: []string{"google", "ads", "ad-parameters", "create"}, Provider: Provider,
	Risk:        changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent, adParameterSensitivity),
	InputSchema: encoded(adParameterInputSchema(adParametersCreateID)), OutputSchema: encoded(adParameterOutputSchema),
	Arguments: []capability.Argument{adGroupAdCustomerArgument,
		{Name: "ad_parameters", Required: true, Description: "One to 10 ad parameters, each an object with " +
			"ad_group_criterion, parameter_index and insertion_text"}, validateOnlyArgument, responseContentTypeArgument},
	Fields: adParameterFields,
	Examples: []capability.Example{{Description: "Validate a price for the first placeholder of a keyword without creating it",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","ad_parameters":[{"ad_group_criterion":` +
			`"customers/1234567890/adGroupCriteria/31~51","parameter_index":1,"insertion_text":"$99.99"}],"validate_only":true}`)}},
}

var adParametersUpdate = capability.Descriptor{
	ID: adParametersUpdateID, Version: 1, Title: "Change Google Ads ad parameters",
	Description: "Change the insertion text of up to 10 ad parameters of a Google Ads account (AdParameterService." +
		"MutateAdParameters with update operations and a field mask): Google applies all of them or none. Each " +
		"entry names the ad parameter by resource_name and gives insertion_text, the one member that can change; " +
		"the keyword and the index are fixed after creation. " + adParameterNote + mutateListNote,
	Tags: []string{"google", "ads", "ad-parameters", "update"}, Provider: Provider,
	Risk:        changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, adParameterSensitivity),
	InputSchema: encoded(adParameterInputSchema(adParametersUpdateID)), OutputSchema: encoded(adParameterOutputSchema),
	Arguments: []capability.Argument{adGroupAdCustomerArgument,
		{Name: "ad_parameters", Required: true, Description: "One to 10 entries: resource_name of the ad parameter " +
			"and insertion_text"}, validateOnlyArgument, responseContentTypeArgument},
	Fields: adParameterFields,
	Examples: []capability.Example{{Description: "Change a price", Arguments: json.RawMessage(`{"customer_id":"1234567890",` +
		`"ad_parameters":[{"resource_name":"customers/1234567890/adParameters/31~51~1","insertion_text":"$89.99"}]}`)}},
}

var adParametersRemove = capability.Descriptor{
	ID: adParametersRemoveID, Version: 1, Title: "Remove Google Ads ad parameters",
	Description: "Remove up to 10 ad parameters of a Google Ads account (AdParameterService.MutateAdParameters " +
		"with remove operations): Google applies all of them or none. The ads of the keyword no longer get the " +
		"value of the parameter in their placeholder; an ad parameter that is removed can be created again. " +
		"Resource names must be ad parameters of customer_id. Offers no response_content_type: a removed ad " +
		"parameter has no mutable resource to show." + mutateListListedNote,
	Tags: []string{"google", "ads", "ad-parameters", "remove"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: changeRisk(capability.EffectDelete, capability.IdempotencyIdempotent, adParameterSensitivity),
	InputSchema: encoded(object(schema{"customer_id": customerIDArgument,
		"resource_names": schema{"type": "array", "minItems": 1, "maxItems": maxAdParameters, "items": schema{"type": "string",
			"maxLength": 100, "pattern": adParameterNamePattern,
			"description": "Ad parameter of this account, customers/ID/adParameters/AD_GROUP_ID~CRITERION_ID~INDEX"}},
		"validate_only": validateOnlySchema}, "customer_id", "resource_names")),
	OutputSchema: encoded(adParameterOutputSchema),
	Arguments: []capability.Argument{adGroupAdCustomerArgument,
		{Name: "resource_names", Required: true, Description: "One to 10 ad parameters of this account, " +
			"customers/ID/adParameters/AD_GROUP_ID~CRITERION_ID~INDEX"}, validateOnlyArgument},
	Fields: adParameterFields,
	Examples: []capability.Example{{Description: "Validate the removal of an ad parameter without removing it",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","resource_names":["customers/1234567890/adParameters/31~51~1"],"validate_only":true}`)}},
}

// --- handlers ---

func handleAdParametersCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return adParametersChange(ctx, resolved, secrets, red, raw, "create ad parameters", adParametersCreateID)
}

func handleAdParametersUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return adParametersChange(ctx, resolved, secrets, red, raw, "update ad parameters", adParametersUpdateID)
}

func handleAdParametersRemove(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return adParametersChange(ctx, resolved, secrets, red, raw, "remove ad parameters", adParametersRemoveID)
}

func adParametersChange(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage, op, tool string) (any, error) {
	a, err := readAdParameterArguments(op, raw)
	if err != nil {
		return nil, err
	}
	conn, err := connectionOf(resolved)
	if err != nil {
		return nil, err
	}
	if !customerIDPattern.MatchString(a.CustomerID) {
		return nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := checkContentType(a.ResponseContentType); err != nil {
		return nil, err
	}
	var ops []any
	var expected []string
	switch tool {
	case adParametersCreateID:
		ops, expected, err = adParameterCreateOperations(a.CustomerID, a.AdParameters)
	case adParametersUpdateID:
		ops, expected, err = adParameterUpdateOperations(a.CustomerID, a.AdParameters)
	default:
		ops, err = adParameterRemoveOperations(a.CustomerID, a.ResourceNames)
		expected = a.ResourceNames
	}
	if err != nil {
		return nil, err
	}
	env := callEnv{ctx: ctx, resolved: resolved, secrets: secrets, red: red, conn: conn, op: op, tool: tool}
	c, err := env.settle(a.CustomerID, true)
	if err != nil {
		return nil, err
	}
	var answer adParameterAnswer
	body := mutateOperationsBody(ops, a.ValidateOnly, a.ResponseContentType)
	if err := c.mutate(ctx, mutation{op: op, tool: tool, customerID: a.CustomerID, service: adParameterService, body: body},
		&answer); err != nil {
		return nil, err
	}
	return adParametersOf(op, answer, a.CustomerID, a.ValidateOnly, expected)
}

// --- confirmation ---

// adParameterSummary phrases the change for the confirmation, from the arguments alone.
func adParameterSummary(tool string) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		a, _ := readAdParameterArguments("", raw)
		verb := map[string]string{adParametersCreateID: "Create", adParametersUpdateID: "Change", adParametersRemoveID: "Remove"}[tool]
		if a.ValidateOnly {
			verb = "Only validate: " + strings.ToLower(verb[:1]) + verb[1:]
		}
		parts := slices.Clone(a.ResourceNames)
		for _, item := range a.AdParameters {
			in, _ := item.(map[string]any)
			part := stringOf(in["resource_name"])
			if part == "" {
				part = stringOf(in["ad_group_criterion"])
			}
			var facts []string
			if v, ok := in["parameter_index"].(json.Number); ok {
				facts = append(facts, "index "+v.String())
			}
			if v := stringOf(in["insertion_text"]); v != "" {
				facts = append(facts, "text "+strconv.Quote(boundedText(v, maxInsertionText)))
			}
			parts = append(parts, part+" ("+strings.Join(facts, ", ")+")")
		}
		summary := verb + " ad parameters of Google Ads account " + a.CustomerID + ": " + strings.Join(parts, "; ") + "."
		if tool == adParametersRemoveID {
			return summary + " The ads of the keyword no longer get the value of the parameter in their placeholder."
		}
		return summary + " The ads of the keyword show this number in their text once they serve."
	}
}
