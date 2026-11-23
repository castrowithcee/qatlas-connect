package googleads

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	customAudiencesCreateID = Provider + ".custom-audiences.create"
	customAudiencesUpdateID = Provider + ".custom-audiences.update"
	customAudiencesRemoveID = Provider + ".custom-audiences.remove"

	customAudienceService = "/customAudiences"

	// Qatlas's own bounds: Google documents no maximum for the operations of a request or the members of an
	// audience, nor a length for the name and the description.
	maxCustomAudienceOperations = 10
	maxCustomAudienceMembers    = 100
	maxCustomAudienceText       = 4096

	// Documented by Google for a keyword member and a URL member.
	maxCustomAudienceKeywordWords = 10
	maxCustomAudienceKeywordRunes = 80
)

// CustomAudienceService.MutateCustomAudiences takes operations[] and has neither partial_failure nor
// response_content_type: a request is atomic and the answer holds resource names only. The names of update and
// remove are built from the checked customer_id and custom audience IDs. The members (keywords, URLs, apps,
// place categories) are data that describe a target group: Qatlas never fetches a URL or looks up an app.

var (
	customAudienceIDPattern  = regexp.MustCompile(`^[1-9][0-9]{0,18}$`)
	customAudienceAppPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*(\.[A-Za-z][A-Za-z0-9_]*)+$`)
	// customAudienceTypes are the types a request may set: INTEREST and PURCHASE_INTENT are refused by Google
	// for new audiences.
	customAudienceTypes       = []string{"AUTO", "SEARCH"}
	customAudienceMemberTypes = []string{"KEYWORD", "URL", "PLACE_CATEGORY", "APP"}
)

func customAudienceName(customerID, id string) string {
	return customerName(customerID) + customAudienceService + "/" + id
}

// customAudienceIDOf extracts the ID of the resource name of an audience of the account.
func customAudienceIDOf(name, customerID string) (string, bool) {
	id, ok := strings.CutPrefix(name, customerName(customerID)+customAudienceService+"/")
	return id, ok && customAudienceIDPattern.MatchString(id) && checkCustomAudienceInt64(id)
}

func checkCustomAudienceInt64(value string) bool {
	_, err := strconv.ParseInt(value, 10, 64)
	return err == nil
}

type (
	customAudienceMember struct {
		MemberType string `json:"member_type"`
		Value      string `json:"value"`
	}
	// customAudienceInput is one audience of create or update; unset members of update stay as they are.
	customAudienceInput struct {
		CustomAudienceID string                 `json:"custom_audience_id"`
		Name             *string                `json:"name"`
		Description      *string                `json:"description"`
		Type             string                 `json:"type"`
		Members          []customAudienceMember `json:"members"`
	}
	customAudienceArguments struct {
		CustomerID      string                `json:"customer_id"`
		CustomAudiences []customAudienceInput `json:"custom_audiences"`
		ValidateOnly    bool                  `json:"validate_only"`
	}
	customAudienceRemoveArguments struct {
		CustomerID        string   `json:"customer_id"`
		CustomAudienceIDs []string `json:"custom_audience_ids"`
		ValidateOnly      bool     `json:"validate_only"`
	}
)

func customAudienceText(argument, value string, limit int, allowBlank bool) error {
	if !allowBlank && strings.TrimSpace(value) == "" {
		return badRequest(argument + " must not be blank")
	}
	if len(value) > limit || !utf8.ValidString(value) {
		return badRequest(argument + " must be valid text of at most " + strconv.Itoa(limit) + " bytes")
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return badRequest(argument + " must not hold control characters")
		}
	}
	return nil
}

// check refuses a member that Google would refuse for its documented form, without naming its value.
func (m customAudienceMember) check() error {
	switch m.MemberType {
	case "KEYWORD":
		if err := customAudienceText("a keyword member", m.Value, 4*maxCustomAudienceKeywordRunes, false); err != nil {
			return err
		}
		if utf8.RuneCountInString(m.Value) > maxCustomAudienceKeywordRunes || len(strings.Fields(m.Value)) > maxCustomAudienceKeywordWords {
			return badRequest("a keyword member holds at most 10 words and 80 characters")
		}
	case "URL":
		if len(m.Value) > maxURLArgument {
			return badRequest("a URL member must not be longer than 2048 bytes")
		}
		for _, r := range m.Value {
			if unicode.IsControl(r) || unicode.IsSpace(r) || r == '\\' {
				return badRequest("a URL member must not hold spaces, backslashes or control characters")
			}
		}
		parsed, err := url.Parse(m.Value)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
			return badRequest("a URL member must be an http or https address with a host name")
		}
	case "PLACE_CATEGORY":
		if !customAudienceIDPattern.MatchString(m.Value) || !checkCustomAudienceInt64(m.Value) {
			return badRequest("a PLACE_CATEGORY member must be the number of a place category")
		}
	case "APP":
		if len(m.Value) > 256 || !customAudienceAppPattern.MatchString(m.Value) {
			return badRequest("an APP member must be the package name of an Android app, such as com.example.app")
		}
	default:
		return badRequest("member_type must be KEYWORD, URL, PLACE_CATEGORY or APP")
	}
	return nil
}

// wire is the member as the API takes it; PLACE_CATEGORY is an int64, which the JSON mapping writes as a string.
func (m customAudienceMember) wire() map[string]any {
	field := map[string]string{"KEYWORD": "keyword", "URL": "url", "PLACE_CATEGORY": "placeCategory", "APP": "app"}
	return map[string]any{"memberType": m.MemberType, field[m.MemberType]: m.Value}
}

func checkCustomAudienceMembers(members []customAudienceMember) ([]any, error) {
	if len(members) > maxCustomAudienceMembers {
		return nil, badRequest("an audience takes at most 100 members")
	}
	out := make([]any, 0, len(members))
	for _, m := range members {
		if err := m.check(); err != nil {
			return nil, err
		}
		out = append(out, m.wire())
	}
	return out, nil
}

func checkCustomAudienceCustomerID(id string) error {
	if !customerIDPattern.MatchString(id) {
		return badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	return nil
}

// operations checks the audiences and builds one operation each. For update the mask names exactly the fields
// that are set; the members then replace all existing ones.
func (a customAudienceArguments) operations(update bool) ([]any, error) {
	if err := checkCustomAudienceCustomerID(a.CustomerID); err != nil {
		return nil, err
	}
	if n := len(a.CustomAudiences); n == 0 || n > maxCustomAudienceOperations {
		return nil, badRequest("custom_audiences must hold one to 10 audiences")
	}
	var names, ids []string
	out := make([]any, 0, len(a.CustomAudiences))
	for _, in := range a.CustomAudiences {
		resource := map[string]any{}
		var mask []string
		if in.Name != nil {
			if err := customAudienceText("name", *in.Name, maxNameArgument, false); err != nil {
				return nil, err
			}
			if !update && slices.Contains(names, *in.Name) {
				return nil, badRequest("a name appears at most once in the request")
			}
			names = append(names, *in.Name)
			resource["name"] = *in.Name
			mask = append(mask, "name")
		} else if !update {
			return nil, badRequest("every audience to create needs a name")
		}
		if in.Description != nil {
			if err := customAudienceText("description", *in.Description, maxCustomAudienceText, true); err != nil {
				return nil, err
			}
			resource["description"] = *in.Description
			mask = append(mask, "description")
		}
		if in.Type != "" {
			if !slices.Contains(customAudienceTypes, in.Type) {
				return nil, badRequest("type must be AUTO or SEARCH")
			}
			resource["type"] = in.Type
			mask = append(mask, "type")
		}
		if len(in.Members) > 0 {
			members, err := checkCustomAudienceMembers(in.Members)
			if err != nil {
				return nil, err
			}
			resource["members"] = members
			mask = append(mask, "members")
		}
		if !update {
			if in.CustomAudienceID != "" {
				return nil, badRequest("custom_audience_id is not an argument of create")
			}
			out = append(out, map[string]any{"create": resource})
			continue
		}
		if !customAudienceIDPattern.MatchString(in.CustomAudienceID) || !checkCustomAudienceInt64(in.CustomAudienceID) {
			return nil, badRequest("custom_audience_id must be the ID of an existing custom audience")
		}
		if slices.Contains(ids, in.CustomAudienceID) {
			return nil, badRequest("an audience appears at most once in the request")
		}
		ids = append(ids, in.CustomAudienceID)
		if len(mask) == 0 {
			return nil, badRequest("every audience to update needs at least one of name, description, type and members")
		}
		resource["resourceName"] = customAudienceName(a.CustomerID, in.CustomAudienceID)
		out = append(out, map[string]any{"update": resource, "updateMask": strings.Join(mask, ",")})
	}
	return out, nil
}

func (a customAudienceRemoveArguments) operations() ([]any, error) {
	if err := checkCustomAudienceCustomerID(a.CustomerID); err != nil {
		return nil, err
	}
	if n := len(a.CustomAudienceIDs); n == 0 || n > maxCustomAudienceOperations {
		return nil, badRequest("custom_audience_ids must hold one to 10 IDs")
	}
	out := make([]any, 0, len(a.CustomAudienceIDs))
	for i, id := range a.CustomAudienceIDs {
		if !customAudienceIDPattern.MatchString(id) || !checkCustomAudienceInt64(id) || slices.Contains(a.CustomAudienceIDs[:i], id) {
			return nil, badRequest("custom_audience_ids must be distinct IDs of existing custom audiences")
		}
		out = append(out, map[string]any{"remove": customAudienceName(a.CustomerID, id)})
	}
	return out, nil
}

type (
	customAudienceResult struct {
		ResourceName     string `json:"resource_name"`
		CustomAudienceID string `json:"custom_audience_id"`
	}
	customAudiencesChanged struct {
		CustomerID      string                 `json:"customer_id"`
		ValidateOnly    bool                   `json:"validate_only"`
		CustomAudiences []customAudienceResult `json:"custom_audiences"`
	}
)

var (
	customAudienceMemberSchema = object(schema{"member_type": schema{"type": "string", "enum": customAudienceMemberTypes},
		"value": schema{"type": "string", "minLength": 1, "maxLength": maxURLArgument}}, "member_type", "value")
	customAudienceMembersSchema = schema{"type": "array", "minItems": 1, "maxItems": maxCustomAudienceMembers, "items": customAudienceMemberSchema}
	customAudienceNameSchema    = schema{"type": "string", "minLength": 1, "maxLength": maxNameArgument}
	customAudienceTypeSchema    = schema{"type": "string", "enum": customAudienceTypes}
	customAudienceIDSchema      = schema{"type": "string", "pattern": customAudienceIDPattern.String()}

	customAudiencesOutputSchema = encoded(object(schema{"customer_id": stringSchema, "validate_only": booleanSchema,
		"custom_audiences": arrayOf(object(schema{"resource_name": stringSchema, "custom_audience_id": stringSchema},
			"resource_name", "custom_audience_id"))}, "customer_id", "validate_only"))

	customAudiencesFields = []capability.Field{
		{Name: "customer_id", Description: "The account of the call"},
		{Name: "validate_only", Description: "true when Google only validated the request"},
		{Name: "custom_audiences", Description: "One entry per audience that Google changed, with resource_name " +
			"(customers/ID/customAudiences/AUDIENCE_ID, as Google answered) and custom_audience_id; empty after " +
			"validate_only"},
	}

	customAudienceCustomerIDDoc = capability.Argument{Name: "customer_id", Required: true, Description: "Ten-digit customer ID of the " +
		"account without hyphens; a target of the connection or an account below a manager target"}
)

const (
	customAudienceSensitivity = "custom audiences of a Google Ads account: keywords, URLs, apps and place categories " +
		"that describe whom ads can reach; no personal data of people"
	customAudienceNote = " The members (keywords, URLs, apps, place categories) are data that describe a target group: " +
		"Qatlas Connect never opens a URL or looks up an app. The service takes a list of operations, at most 10 " +
		"here, in one request without partial failure, so Google applies all of them or none; it has no " +
		"response_content_type, and the answer names the resource only. Read audiences with reports.search on " +
		"custom_audience. The tool acts on the account named by customer_id: on a connection with targets, a manager " +
		"account needs a manager target that names it, and an account below a manager target is proved first."
	customAudienceMemberDoc = "Members that describe the audience, 1 to 100: member_type KEYWORD (at most 10 words and 80 " +
		"characters), URL (http or https address with a host, at most 2048 bytes), PLACE_CATEGORY (number of a place " +
		"type that users visit) or APP (Android package name such as com.example.app), and the value"
)

var customAudienceExample = json.RawMessage(`{"customer_id":"1234567890","custom_audiences":[{"name":"Hiking gear",` +
	`"type":"SEARCH","members":[{"member_type":"KEYWORD","value":"trail running shoes"},{"member_type":"URL",` +
	`"value":"https://www.example.com/hiking"}]}],"validate_only":true}`)

var customAudiencesCreate = capability.Descriptor{
	ID: customAudiencesCreateID, Version: 1, Title: "Create custom audiences of a Google Ads account",
	Description: "Create one to 10 custom audiences, each from keywords, URLs, apps and place categories " +
		"(CustomAudienceService.MutateCustomAudiences with create operations). A custom audience can then be " +
		"targeted by campaigns and ad groups. The name must be unique among the custom audiences of the account " +
		"(Google answers NAME_ALREADY_USED otherwise). Only the types AUTO and SEARCH are offered, because Google " +
		"does not allow INTEREST and PURCHASE_INTENT for new audiences." + customAudienceNote + mutateListNote,
	Tags: []string{"google", "ads", "audiences", "custom-audiences", "create"}, Provider: Provider,
	Risk: changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent, customAudienceSensitivity),
	InputSchema: encoded(object(schema{"customer_id": customerIDArgument,
		"custom_audiences": schema{"type": "array", "minItems": 1, "maxItems": maxCustomAudienceOperations,
			"items": object(schema{"name": customAudienceNameSchema,
				"description": schema{"type": "string", "maxLength": maxCustomAudienceText}, "type": customAudienceTypeSchema,
				"members": customAudienceMembersSchema}, "name")},
		"validate_only": validateOnlySchema}, "customer_id", "custom_audiences")),
	OutputSchema: customAudiencesOutputSchema,
	Arguments: []capability.Argument{customAudienceCustomerIDDoc,
		{Name: "custom_audiences", Required: true, Description: "One to 10 audiences to create, each with name " +
			"(required, unique among the audiences of the account), description, type (AUTO or SEARCH) and members. " +
			"" + customAudienceMemberDoc},
		validateOnlyArgument},
	Fields:   customAudiencesFields,
	Examples: []capability.Example{{Description: "Validate a new audience without creating it", Arguments: customAudienceExample}},
}

var customAudiencesUpdate = capability.Descriptor{
	ID: customAudiencesUpdateID, Version: 1, Title: "Change custom audiences of a Google Ads account",
	Description: "Change one to 10 existing custom audiences (CustomAudienceService.MutateCustomAudiences with " +
		"update operations; the mask names exactly the fields that are given). Members cannot be changed one by " +
		"one: when members is given, it replaces all existing members of that audience, so repeat the ones to " +
		"keep. Fields that are left out stay as they are. The resource names are built from customer_id and " +
		"custom_audience_id." + customAudienceNote + mutateListNote,
	Tags: []string{"google", "ads", "audiences", "custom-audiences", "update"}, Provider: Provider,
	Risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, customAudienceSensitivity),
	InputSchema: encoded(object(schema{"customer_id": customerIDArgument,
		"custom_audiences": schema{"type": "array", "minItems": 1, "maxItems": maxCustomAudienceOperations,
			"items": object(schema{"custom_audience_id": customAudienceIDSchema, "name": customAudienceNameSchema,
				"description": schema{"type": "string", "maxLength": maxCustomAudienceText}, "type": customAudienceTypeSchema,
				"members": customAudienceMembersSchema}, "custom_audience_id")},
		"validate_only": validateOnlySchema}, "customer_id", "custom_audiences")),
	OutputSchema: customAudiencesOutputSchema,
	Arguments: []capability.Argument{customAudienceCustomerIDDoc,
		{Name: "custom_audiences", Required: true, Description: "One to 10 audiences to change, each with " +
			"custom_audience_id (required, as reports.search returns custom_audience.id, once per request) and at " +
			"least one of name, description (empty text clears it), type (AUTO or SEARCH) and members, which " +
			"replace all existing members. " + customAudienceMemberDoc},
		validateOnlyArgument},
	Fields: customAudiencesFields,
	Examples: []capability.Example{{Description: "Validate a new member list and name",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","custom_audiences":[{"custom_audience_id":"` +
			`1122334455","name":"Hiking gear 2","members":[{"member_type":"KEYWORD","value":"trail shoes"}]}],` +
			`"validate_only":true}`)}},
}

var customAudiencesRemove = capability.Descriptor{
	ID: customAudiencesRemoveID, Version: 1, Title: "Remove custom audiences of a Google Ads account",
	Description: "Remove one to 10 existing custom audiences (CustomAudienceService.MutateCustomAudiences with " +
		"remove operations on customers/ID/customAudiences/AUDIENCE_ID). A removed audience has the status " +
		"REMOVED and can no longer be used for targeting; Qatlas Connect cannot bring it back." + customAudienceNote +
		mutateListListedNote,
	Tags: []string{"google", "ads", "audiences", "custom-audiences", "remove"}, Provider: Provider,
	RequiresToolAllowList: true,
	Risk:                  changeRisk(capability.EffectDelete, capability.IdempotencyIdempotent, customAudienceSensitivity),
	InputSchema: encoded(object(schema{"customer_id": customerIDArgument,
		"custom_audience_ids": schema{"type": "array", "minItems": 1, "maxItems": maxCustomAudienceOperations,
			"uniqueItems": true, "items": customAudienceIDSchema},
		"validate_only": validateOnlySchema}, "customer_id", "custom_audience_ids")),
	OutputSchema: customAudiencesOutputSchema,
	Arguments: []capability.Argument{customAudienceCustomerIDDoc,
		{Name: "custom_audience_ids", Required: true, Description: "One to 10 distinct IDs of existing custom " +
			"audiences, as reports.search returns custom_audience.id"},
		validateOnlyArgument},
	Fields: customAudiencesFields,
	Examples: []capability.Example{{Description: "Validate the removal of an audience",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","custom_audience_ids":["1122334455"],"validate_only":true}`)}},
}

func handleCustomAudiencesCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return customAudiencesChange(ctx, resolved, secrets, red, raw, customAudiencesCreateID)
}

func handleCustomAudiencesUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return customAudiencesChange(ctx, resolved, secrets, red, raw, customAudiencesUpdateID)
}

func handleCustomAudiencesRemove(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return customAudiencesChange(ctx, resolved, secrets, red, raw, customAudiencesRemoveID)
}

func customAudiencesChange(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage, tool string) (any, error) {
	op := map[string]string{customAudiencesCreateID: "create custom audiences",
		customAudiencesUpdateID: "update custom audiences", customAudiencesRemoveID: "remove custom audiences"}[tool]
	var (
		customerID   string
		validateOnly bool
		operations   []any
		sent         []string // the resource names update and remove address, in the order sent
		err          error
	)
	conn, err := connectionOf(resolved)
	if err != nil {
		return nil, err
	}
	if tool == customAudiencesRemoveID {
		var args customAudienceRemoveArguments
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, failure(op, "the validated arguments could not be read")
		}
		customerID, validateOnly = args.CustomerID, args.ValidateOnly
		if operations, err = args.operations(); err != nil {
			return nil, err
		}
		for _, id := range args.CustomAudienceIDs {
			sent = append(sent, customAudienceName(customerID, id))
		}
	} else {
		var args customAudienceArguments
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, failure(op, "the validated arguments could not be read")
		}
		customerID, validateOnly = args.CustomerID, args.ValidateOnly
		update := tool == customAudiencesUpdateID
		if operations, err = args.operations(update); err != nil {
			return nil, err
		}
		if update {
			for _, in := range args.CustomAudiences {
				sent = append(sent, customAudienceName(customerID, in.CustomAudienceID))
			}
		}
	}
	env := callEnv{ctx: ctx, resolved: resolved, secrets: secrets, red: red, conn: conn, op: op, tool: tool}
	c, err := env.settle(customerID, true)
	if err != nil {
		return nil, err
	}
	var answer struct {
		Results []struct {
			ResourceName string `json:"resourceName"`
		} `json:"results"`
	}
	if err := c.mutate(ctx, mutation{op: op, tool: tool, customerID: customerID, service: customAudienceService,
		body: mutateOperationsBody(operations, validateOnly, "")}, &answer); err != nil {
		return nil, err
	}
	out := customAudiencesChanged{CustomerID: customerID, ValidateOnly: validateOnly,
		CustomAudiences: []customAudienceResult{}}
	named := 0
	for _, r := range answer.Results {
		if r.ResourceName != "" {
			named++
		}
	}
	if validateOnly && named == 0 {
		return out, nil
	}
	// The request was sent, so an answer for other objects leaves the outcome open.
	bad := unsure(provider.InvalidResponse(op, subject+" answered with results for other custom audiences than "+
		"the ones asked for"), true)
	if len(answer.Results) != len(operations) || named != len(operations) {
		return nil, bad
	}
	for _, r := range answer.Results {
		id, ok := customAudienceIDOf(r.ResourceName, customerID)
		if !ok || slices.ContainsFunc(out.CustomAudiences, func(x customAudienceResult) bool { return x.ResourceName == r.ResourceName }) ||
			(sent != nil && !slices.Contains(sent, r.ResourceName)) {
			return nil, bad
		}
		out.CustomAudiences = append(out.CustomAudiences, customAudienceResult{ResourceName: r.ResourceName,
			CustomAudienceID: id})
	}
	return out, nil
}

func customAudienceValidateLead(only bool, verb string) string {
	if only {
		return "Only validate: " + verb
	}
	return verb
}

func customAudienceMemberCounts(members []customAudienceMember) string {
	var parts []string
	for _, t := range customAudienceMemberTypes {
		n := 0
		for _, m := range members {
			if m.MemberType == t {
				n++
			}
		}
		if n > 0 {
			parts = append(parts, strconv.Itoa(n)+" "+strings.ToLower(t))
		}
	}
	return strings.Join(parts, ", ")
}

// customAudienceSummary phrases create and update for the confirmation, from the arguments alone, with every
// audience of the request.
func customAudienceSummary(update bool) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		var a customAudienceArguments
		_ = json.Unmarshal(raw, &a)
		var parts []string
		for _, in := range a.CustomAudiences {
			var part string
			if update {
				part = "audience " + in.CustomAudienceID
				var changes []string
				if in.Name != nil {
					changes = append(changes, "name \""+*in.Name+"\"")
				}
				if in.Description != nil {
					changes = append(changes, "description")
				}
				if in.Type != "" {
					changes = append(changes, "type "+in.Type)
				}
				if len(in.Members) > 0 {
					changes = append(changes, "all members replaced by "+customAudienceMemberCounts(in.Members))
				}
				part += " (" + strings.Join(changes, ", ") + ")"
			} else {
				name := ""
				if in.Name != nil {
					name = *in.Name
				}
				part = "\"" + name + "\" (" + strings.Trim(in.Type+", "+customAudienceMemberCounts(in.Members), ", ") + ")"
			}
			parts = append(parts, part)
		}
		verb := "Create custom audiences"
		if update {
			verb = "Change custom audiences"
		}
		return customAudienceValidateLead(a.ValidateOnly, verb) + " of Google Ads account " + a.CustomerID + ": " +
			strings.Join(parts, "; ") + ". The members describe whom ads can reach; nothing is opened or fetched."
	}
}

func customAudienceRemoveSummary(raw json.RawMessage) string {
	var a customAudienceRemoveArguments
	_ = json.Unmarshal(raw, &a)
	return customAudienceValidateLead(a.ValidateOnly, "Remove custom audiences") + " " + strings.Join(a.CustomAudienceIDs, ", ") +
		" of Google Ads account " + a.CustomerID + ": they can no longer be used for targeting."
}
