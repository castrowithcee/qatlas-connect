package googleanalytics

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// The tools of this file act on access bindings that exist: they read several at once, replace the roles of the
// user of one binding, and remove one or several bindings (Admin API v1alpha). Creating a binding (create,
// batchCreate) and changing the user of a binding would give someone new access, so neither is a tool; batchUpdate
// is left out too, because it could change the user and only an extra check per entry could rule that out.
const (
	accessBindingsBatchGetID    = Provider + ".access-bindings.batch-get"
	accessBindingsUpdateID      = Provider + ".access-bindings.update"
	accessBindingsDeleteID      = Provider + ".access-bindings.delete"
	accessBindingsBatchDeleteID = Provider + ".access-bindings.batch-delete"

	// maxBatchBindings is the most bindings Google reads or removes in one batch call.
	maxBatchBindings = 1000
	// maxUserLength bounds the address given as previous_user.
	maxUserLength = 320
)

// scopeUsers is the one scope of the Admin API that changes access bindings (analyticsadmin v1alpha). It also
// allows creating bindings, which no tool does.
const scopeUsers = "https://www.googleapis.com/auth/analytics.manage.users"

// accessBindingsProfile holds the changing binding tools besides what the profile read reads. Their methods
// accept only scopeUsers, so they are in neither read nor write.
const accessBindingsProfile = "access-bindings"

// predefinedRoles are the roles the v1alpha AccessBinding documents, in its order.
var predefinedRoles = []string{"predefinedRoles/viewer", "predefinedRoles/analyst", "predefinedRoles/editor",
	"predefinedRoles/admin", "predefinedRoles/no-cost-data", "predefinedRoles/no-revenue-data"}

func bindingWriteRisk(effect capability.Effect) capability.Risk {
	return capability.Risk{Effect: effect, Idempotency: capability.IdempotencyIdempotent,
		Confirmation: capability.ConfirmationRequired, OpenWorld: true, DataSensitivity: dataSensitivityBindings}
}

const (
	bindingNamesSchema = `{"type":"array","minItems":1,"maxItems":1000,"uniqueItems":true,"items":` +
		bindingNameSchema + `}`
	bindingRolesSchema = `{"type":"array","minItems":1,"maxItems":6,"uniqueItems":true,"items":{"type":"string",` +
		`"enum":["predefinedRoles/viewer","predefinedRoles/analyst","predefinedRoles/editor","predefinedRoles/admin",` +
		`"predefinedRoles/no-cost-data","predefinedRoles/no-revenue-data"]}}`
	previousRolesSchema = `{"type":"array","minItems":1,"maxItems":16,"uniqueItems":true,"items":{"type":"string",` +
		`"minLength":1,"maxLength":64}}`

	selfLockoutNote = "Removing the binding of the very user or service account behind the credential, or taking " +
		"its admin role, can lock this connection out; the tool cannot tell, because Google does not name the user " +
		"of the token. "
	bindingWriteNote = "The parent of an account binding is account-wide: the connection must bind that account " +
		"or bind no target, and a bound property is not enough. A property binding needs a bound property or a " +
		"property of a bound account, which is proven by one read of the property first. Needs the scope " +
		"analytics.manage.users, which also lets the token grant access, although no tool does. Addresses are " +
		"personal data and untrusted content. "
)

var namesArgument = capability.Argument{Name: "names", Required: true, Description: "1 to 1000 binding resource " +
	"names, each accounts/ID/accessBindings/ID or properties/ID/accessBindings/ID of parent, without repeats"}

var parentArgument = capability.Argument{Name: "parent", Required: true, Description: "Account resource name " +
	"accounts/ID or property resource name properties/ID that holds every binding of names"}

var accessBindingsBatchGet = capability.Descriptor{
	ID: accessBindingsBatchGetID, Version: 1, Title: "Get several Analytics access bindings",
	Description: "Read up to 1000 access bindings of one Google Analytics account or property in one call " +
		"(accounts.accessBindings.batchGet and properties.accessBindings.batchGet of the Analytics Admin API): the " +
		"users with their email address and roles. Every name must belong to parent. " + bindingWarning + alphaNote,
	Tags: []string{"google", "analytics", "access-bindings", "batch-get", "alpha", "personal-data"},
	Risk: bindingRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{"parent":` + bindingParentSchema + `,"names":` +
		bindingNamesSchema + `},"required":["parent","names"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"accessBindings":{"type":"array","items":` +
		bindingSchema + `}},"additionalProperties":false}`),
	Arguments: []capability.Argument{parentArgument, namesArgument},
	Fields: []capability.Field{{Name: "accessBindings", Description: "Per binding Google returned of the names " +
		"asked for: " + bindingFieldsText}},
	Examples: []capability.Example{{Description: "Read two bindings of a property",
		Arguments: json.RawMessage(`{"parent":"properties/200","names":["properties/200/accessBindings/5001",` +
			`"properties/200/accessBindings/5002"]}`)}},
}

var accessBindingsUpdate = capability.Descriptor{
	ID: accessBindingsUpdateID, Version: 1, Title: "Change the roles of an Analytics access binding",
	Description: "Replace the roles of the user of one existing access binding of a Google Analytics account or " +
		"property (accounts.accessBindings.patch and properties.accessBindings.patch of the Analytics Admin API). " +
		"Only the roles change: there is no argument for the user, and the request carries the roles alone. The " +
		"roles take effect at once; adding admin lets the user manage users and every setting. previous_user and " +
		"previous_roles are the binding as last read: the confirmation shows them, the tool reads the binding " +
		"first and changes nothing when they differ, and they are never written. An empty role list is refused, " +
		"because Google would delete the binding. Needs the confirmation of a person for each call. " +
		selfLockoutNote + bindingWriteNote + alphaNote,
	Tags: []string{"google", "analytics", "access-bindings", "update", "alpha", "personal-data"},
	Risk: bindingWriteRisk(capability.EffectUpdate), Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{"name":` + bindingNameSchema + `,"roles":` +
		bindingRolesSchema + `,"previous_user":{"type":"string","minLength":1,"maxLength":` +
		strconv.Itoa(maxUserLength) + `},"previous_roles":` + previousRolesSchema + `},"required":["name","roles",` +
		`"previous_user","previous_roles"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(bindingSchema),
	Arguments: []capability.Argument{
		{Name: "name", Required: true, Description: "Binding resource name accounts/ID/accessBindings/ID or " +
			"properties/ID/accessBindings/ID, from access-bindings.list"},
		{Name: "roles", Required: true, Description: "The new roles, 1 to 6 of predefinedRoles/viewer, analyst, " +
			"editor, admin, no-cost-data and no-revenue-data; they replace every role of the binding"},
		{Name: "previous_user", Required: true, Description: "The email address of the binding as last read; " +
			"compared without regard to case, shown in the confirmation, never written"},
		{Name: "previous_roles", Required: true, Description: "The roles of the binding as last read, in any order; " +
			"compared, shown in the confirmation, never written"},
	},
	Fields: []capability.Field{{Name: "(result)", Description: "The binding as Google answered the change: " +
		bindingFieldsText}},
	Examples: []capability.Example{{Description: "Make a viewer an analyst",
		Arguments: json.RawMessage(`{"name":"properties/200/accessBindings/5001","roles":["predefinedRoles/analyst"],` +
			`"previous_user":"person@example.com","previous_roles":["predefinedRoles/viewer"]}`)}},
}

var accessBindingsDelete = capability.Descriptor{
	ID: accessBindingsDeleteID, Version: 1, Title: "Delete an Analytics access binding",
	Description: "Remove one access binding of a Google Analytics account or property " +
		"(accounts.accessBindings.delete and properties.accessBindings.delete of the Analytics Admin API): its user " +
		"loses every role the binding gives, at once. No tool gives the access back; a person does that in the " +
		"Analytics interface. Needs the confirmation of a person for each call. " + selfLockoutNote +
		bindingWriteNote + alphaNote,
	Tags: []string{"google", "analytics", "access-bindings", "delete", "alpha", "personal-data"},
	Risk: bindingWriteRisk(capability.EffectDelete), Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{"name":` + bindingNameSchema +
		`},"required":["name"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"},"deleted":` +
		`{"type":"boolean"}},"additionalProperties":false}`),
	Arguments: []capability.Argument{{Name: "name", Required: true, Description: "Binding resource name " +
		"accounts/ID/accessBindings/ID or properties/ID/accessBindings/ID, from access-bindings.list"}},
	Fields: []capability.Field{
		{Name: "name", Description: "The binding that was removed"},
		{Name: "deleted", Description: "true once Google confirmed the removal"},
	},
	Examples: []capability.Example{{Description: "Remove one binding",
		Arguments: json.RawMessage(`{"name":"properties/200/accessBindings/5001"}`)}},
}

var accessBindingsBatchDelete = capability.Descriptor{
	ID: accessBindingsBatchDeleteID, Version: 1, Title: "Delete several Analytics access bindings",
	Description: "Remove up to 1000 access bindings of one Google Analytics account or property in one call " +
		"(accounts.accessBindings.batchDelete and properties.accessBindings.batchDelete of the Analytics Admin " +
		"API): their users lose every role these bindings give, at once. Google answers a success without detail. " +
		"When Google refuses the call, Qatlas cannot tell whether some bindings were removed, because Google " +
		"documents no partial result; read them with access-bindings.batch-get before trying again. Every name " +
		"must belong to parent. Offered only by a connection whose tools list names it, and needs the " +
		"confirmation of a person for each call. " + selfLockoutNote + bindingWriteNote + alphaNote,
	Tags: []string{"google", "analytics", "access-bindings", "batch-delete", "alpha", "personal-data"},
	Risk: bindingWriteRisk(capability.EffectDelete), Provider: Provider,
	// Removing up to 1000 users at once has a broad effect, so no profile offers it.
	RequiresToolAllowList: true,
	InputSchema: json.RawMessage(`{"type":"object","properties":{"parent":` + bindingParentSchema + `,"names":` +
		bindingNamesSchema + `},"required":["parent","names"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"parent":{"type":"string"},"names":{"type":` +
		`"array","items":{"type":"string"}},"deleted":{"type":"boolean"}},"additionalProperties":false}`),
	Arguments: []capability.Argument{parentArgument, namesArgument},
	Fields: []capability.Field{
		{Name: "parent", Description: "The account or property the bindings belonged to"},
		{Name: "names", Description: "The bindings that were removed"},
		{Name: "deleted", Description: "true once Google confirmed the removal of all of them"},
	},
	Examples: []capability.Example{{Description: "Remove two bindings of a property",
		Arguments: json.RawMessage(`{"parent":"properties/200","names":["properties/200/accessBindings/5001",` +
			`"properties/200/accessBindings/5002"]}`)}},
}

type (
	bindingBatchArguments struct {
		Parent string   `json:"parent"`
		Names  []string `json:"names"`
	}
	bindingUpdateArguments struct {
		Name          string   `json:"name"`
		Roles         []string `json:"roles"`
		PreviousUser  string   `json:"previous_user"`
		PreviousRoles []string `json:"previous_roles"`
	}
	bindingBatchResult struct {
		AccessBindings []accessBinding `json:"accessBindings"`
	}
	bindingDeleteResult struct {
		Name    string `json:"name"`
		Deleted bool   `json:"deleted"`
	}
	bindingBatchDeleteResult struct {
		Parent  string   `json:"parent"`
		Names   []string `json:"names"`
		Deleted bool     `json:"deleted"`
	}
	deleteRequest struct {
		Name string `json:"name"`
	}
)

// checkBindingName returns the parent of a binding name, or refuses it without quoting it.
func checkBindingName(argument, value string) (string, error) {
	parent, ok := splitBindingName(value)
	if !ok {
		return "", badRequest(argument + " must be accounts/ID/accessBindings/ID or properties/ID/accessBindings/ID " +
			"with numeric IDs")
	}
	return parent, nil
}

// checkBatch checks the parent and every name of a batch call before any I/O: 1 to 1000 names without repeats,
// each of the parent.
func checkBatch(a bindingBatchArguments) error {
	if !isName(accountPrefix, a.Parent) && !isName(propertyPrefix, a.Parent) {
		return badRequest("parent must be accounts/ or properties/ followed by the numeric ID")
	}
	if len(a.Names) == 0 || len(a.Names) > maxBatchBindings {
		return badRequest("names must hold 1 to " + strconv.Itoa(maxBatchBindings) + " binding names")
	}
	seen := make(map[string]bool, len(a.Names))
	for _, name := range a.Names {
		parent, err := checkBindingName("every entry of names", name)
		if err != nil {
			return err
		}
		if parent != a.Parent {
			return badRequest("every entry of names must be a binding of parent")
		}
		if seen[name] {
			return badRequest("names must not repeat a binding")
		}
		seen[name] = true
	}
	return nil
}

// checkRoles refuses an empty role list, which would make Google delete the binding, an unknown role and a
// repeat.
func checkRoles(roles []string) error {
	if len(roles) == 0 {
		return badRequest("roles must name at least one role; an empty list would delete the binding")
	}
	for i, role := range roles {
		if !slices.Contains(predefinedRoles, role) {
			return badRequest("roles must be predefinedRoles/viewer, analyst, editor, admin, no-cost-data or " +
				"no-revenue-data")
		}
		if slices.Contains(roles[:i], role) {
			return badRequest("roles must not repeat a role")
		}
	}
	return nil
}

// sameRoles reports whether two role lists hold the same roles, in any order.
func sameRoles(given []string, stored []code) bool {
	if len(given) != len(stored) {
		return false
	}
	for _, role := range stored {
		if !slices.Contains(given, string(role)) {
			return false
		}
	}
	return true
}

// openBinding checks the target of parent, opens the client and proves a property of a bound account.
func openBinding(ctx context.Context, op, tool, parent string, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor) (*client, error) {
	bind, err := bindingOf(resolved)
	if err != nil {
		return nil, err
	}
	property, proof, err := bind.bindingTarget(parent)
	if err != nil {
		return nil, err
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := c.proveProperty(ctx, op, tool, property, proof); err != nil {
		return nil, err
	}
	return c, nil
}

func handleAccessBindingsBatchGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "get access bindings"
	var arguments bindingBatchArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if err := checkBatch(arguments); err != nil {
		return nil, err
	}
	c, err := openBinding(ctx, op, accessBindingsBatchGetID, arguments.Parent, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var page bindingBatchResult
	if err := c.get(ctx, apiCall{op: op, tool: accessBindingsBatchGetID,
		path: alphaPath(arguments.Parent + bindingsSegment + ":batchGet"), query: url.Values{"names": arguments.Names},
		limit: maxBindingBytes, notFound: notFoundBinding}, &page); err != nil {
		return nil, err
	}
	out := bindingBatchResult{AccessBindings: []accessBinding{}}
	// Only bindings that were asked for are shown, each once.
	for _, entry := range head(page.AccessBindings, maxBatchBindings) {
		name := string(entry.Name)
		if slices.Contains(arguments.Names, name) && !slices.ContainsFunc(out.AccessBindings,
			func(b accessBinding) bool { return string(b.Name) == name }) {
			out.AccessBindings = append(out.AccessBindings, entry.bounded())
		}
	}
	return out, nil
}

func handleAccessBindingsUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "update access binding"
	var arguments bindingUpdateArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	parent, err := checkBindingName("name", arguments.Name)
	if err != nil {
		return nil, err
	}
	if err := checkRoles(arguments.Roles); err != nil {
		return nil, err
	}
	if arguments.PreviousUser == "" || len(arguments.PreviousRoles) == 0 {
		return nil, badRequest("previous_user and previous_roles are required: the binding as last read")
	}
	// The body holds the roles alone, so the user of the binding cannot change.
	body, err := json.Marshal(struct {
		Roles []string `json:"roles"`
	}{arguments.Roles})
	if err != nil {
		return nil, failure(op, "the roles could not be encoded")
	}
	c, err := openBinding(ctx, op, accessBindingsUpdateID, parent, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var stored accessBinding
	if err := c.get(ctx, apiCall{op: op, tool: accessBindingsUpdateID, path: alphaPath(arguments.Name),
		limit: maxBindingBytes, notFound: notFoundBinding}, &stored); err != nil {
		return nil, err
	}
	if string(stored.Name) != arguments.Name {
		return nil, provider.InvalidResponse(op, subject+" answered with another access binding than the one asked for")
	}
	if !strings.EqualFold(string(stored.User), arguments.PreviousUser) || !sameRoles(arguments.PreviousRoles, stored.Roles) {
		return nil, badRequest("previous_user or previous_roles differ from the binding as stored; nothing was " +
			"changed, read it again with access-bindings.get")
	}
	var out accessBinding
	if err := c.change(ctx, changeCall{op: op, tool: accessBindingsUpdateID, method: http.MethodPatch,
		path: alphaPath(arguments.Name), body: body, limit: maxBindingBytes, notFound: notFoundBinding}, &out); err != nil {
		return nil, err
	}
	if string(out.Name) != arguments.Name {
		return nil, unsure(failure(op, subject+" answered with another access binding than the one asked for"))
	}
	return out.bounded(), nil
}

func handleAccessBindingsDelete(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "delete access binding"
	var arguments bindingArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	parent, err := checkBindingName("name", arguments.Name)
	if err != nil {
		return nil, err
	}
	c, err := openBinding(ctx, op, accessBindingsDeleteID, parent, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := c.change(ctx, changeCall{op: op, tool: accessBindingsDeleteID, method: http.MethodDelete,
		path: alphaPath(arguments.Name), notFound: notFoundBinding}, nil); err != nil {
		return nil, err
	}
	return bindingDeleteResult{Name: arguments.Name, Deleted: true}, nil
}

// batchUnknownNote ends a refused batch deletion: Google documents no partial result.
const batchUnknownNote = "; Google documents no partial result, so some bindings may have been removed: read them " +
	"with access-bindings.batch-get before trying again"

func handleAccessBindingsBatchDelete(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "delete access bindings"
	var arguments bindingBatchArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if err := checkBatch(arguments); err != nil {
		return nil, err
	}
	requests := make([]deleteRequest, len(arguments.Names))
	for i, name := range arguments.Names {
		requests[i] = deleteRequest{Name: name}
	}
	body, err := json.Marshal(struct {
		Requests []deleteRequest `json:"requests"`
	}{requests})
	if err != nil {
		return nil, failure(op, "the names could not be encoded")
	}
	c, err := openBinding(ctx, op, accessBindingsBatchDeleteID, arguments.Parent, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := c.change(ctx, changeCall{op: op, tool: accessBindingsBatchDeleteID, method: http.MethodPost,
		path: alphaPath(arguments.Parent + bindingsSegment + ":batchDelete"), body: body,
		notFound: notFoundBinding}, nil); err != nil {
		// A rate limit stops the call before Google acts; an unsure outcome already says so.
		var failed *provider.Error
		if errors.As(err, &failed) && failed.Class != provider.ClassRateLimited &&
			!strings.HasSuffix(failed.Message, unsureNote) {
			failed.Message += batchUnknownNote
		}
		return nil, err
	}
	return bindingBatchDeleteResult{Parent: arguments.Parent, Names: arguments.Names, Deleted: true}, nil
}

// roleList names roles without their common prefix.
func roleList(roles []string) string {
	short := make([]string, len(roles))
	for i, role := range roles {
		short[i] = strings.TrimPrefix(role, "predefinedRoles/")
	}
	return strings.Join(short, ", ")
}

// bindingUpdateSummary phrases the change from the arguments alone: the user and the roles before come from the
// previous_ arguments, which the handler compares with the stored binding. The arguments are untrusted data;
// the core cuts and neutralizes what this returns.
func bindingUpdateSummary(raw json.RawMessage) string {
	var a bindingUpdateArguments
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	var added, removed []string
	for _, role := range a.Roles {
		if !slices.Contains(a.PreviousRoles, role) {
			added = append(added, role)
		}
	}
	for _, role := range a.PreviousRoles {
		if !slices.Contains(a.Roles, role) {
			removed = append(removed, role)
		}
	}
	summary := "Change the roles of user " + a.PreviousUser + " on access binding " + a.Name + " from " +
		roleList(a.PreviousRoles) + " to " + roleList(a.Roles)
	if len(added) > 0 {
		summary += "; adds " + roleList(added)
	}
	if len(removed) > 0 {
		summary += "; removes " + roleList(removed)
	}
	if slices.Contains(added, "predefinedRoles/admin") {
		summary += "; admin lets the user manage users and every setting"
	}
	return summary + "; refused if the stored user or roles differ. If this is the credential's own user, it can " +
		"lose access"
}

func bindingDeleteSummary(raw json.RawMessage) string {
	var a bindingArguments
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	return "Remove access binding " + a.Name + ": its user loses every role of it at once, and no tool gives it " +
		"back. If it is the binding of the credential's own user, this connection can lose its access"
}

func bindingBatchDeleteSummary(raw json.RawMessage) string {
	var a bindingBatchArguments
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	// The count leads, so a cut list of names still says how many go.
	return "Remove " + strconv.Itoa(len(a.Names)) + " access bindings of " + a.Parent + ": their users lose every " +
		"role of them at once, and no tool gives it back. If one is the binding of the credential's own user, this " +
		"connection can lose its access. Names: " + strings.Join(a.Names, ", ")
}
