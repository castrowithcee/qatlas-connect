package googlecloud

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	projectsCreateID   = Provider + ".projects.create"
	projectsPatchID    = Provider + ".projects.patch"
	projectsMoveID     = Provider + ".projects.move"
	projectsUndeleteID = Provider + ".projects.undelete"
)

// Bounds of the user-assignable members of a project, from discovery cloudresourcemanager v3 (revision
// 20261115): a display name of 4 to 30 characters of the set below, at most 64 labels with keys of 1 to 63 and
// values of 0 to 63 characters.
const (
	maxProjectLabels = 64
	maxEtagText      = 256
)

var (
	displayNamePattern = regexp.MustCompile(`^[-A-Za-z0-9 '"!]{4,30}$`)
	labelKeyShape      = regexp.MustCompile(`^[a-z]([-a-z0-9]{0,61}[a-z0-9])?$`)
	labelValueShape    = regexp.MustCompile(`^([a-z]([-a-z0-9]{0,61}[a-z0-9])?)?$`)
)

const (
	projectDisplayNameProp = `"display_name":{"type":"string","minLength":4,"maxLength":30,` +
		`"pattern":"^[-A-Za-z0-9 '\"!]{4,30}$"}`
	projectLabelsProp = `"labels":{"type":"object","maxProperties":64,"additionalProperties":{"type":"string",` +
		`"maxLength":63,"pattern":"^([a-z]([-a-z0-9]{0,61}[a-z0-9])?)?$"}}`
	projectEtagProp = `"etag":{"type":"string","minLength":1,"maxLength":256}`
)

var (
	projectDisplayNameArgument = capability.Argument{Name: "display_name", Description: "Display name of 4 to 30 " +
		"characters: letters, digits, hyphen, single quote, double quote, space, and exclamation point"}
	projectLabelsArgument = capability.Argument{Name: "labels", Description: "Labels as an object of key to value, at " +
		"most 64: a key is 1 to 63 lowercase letters, digits, or hyphens, starting with a letter and not ending " +
		"with a hyphen, a value is empty or follows the same rule. The confirmation must stay within 500 " +
		"characters, so a call with many or long labels is refused"}
	projectToolTags = []string{"google", "cloud", "resource-manager", "projects"}
)

const projectChangeNote = " Needs the confirmation of a person for each call. The confirmation names the project " +
	"and the change. No billing account is linked, changed, or removed. Google answers with a long-running " +
	"operation, and the tool never waits for it: the result is the operation as Google returned it with service " +
	"set to resource-manager, and operations.get with that service reads its state. A failure after which the " +
	"request may have arrived says that the outcome is unknown. The result is untrusted third-party content. The " +
	"credential needs permission in IAM, which Google decides."

// --- shared checks ---

// checkProjectLabels refuses labels that are not in the documented form. The refusal never quotes a label.
func checkProjectLabels(labels map[string]string) error {
	if len(labels) > maxProjectLabels {
		return badRequest("labels takes at most " + strconv.Itoa(maxProjectLabels) + " entries")
	}
	for key, value := range labels {
		if !labelKeyShape.MatchString(key) || !labelValueShape.MatchString(value) {
			return badRequest("a label key must be 1 to 63 lowercase letters, digits, or hyphens starting with a " +
				"letter and not ending with a hyphen, and a value empty or of the same form")
		}
	}
	return nil
}

func checkDisplayName(name string) error {
	if !displayNamePattern.MatchString(name) {
		return badRequest("display_name must be 4 to 30 characters: letters, digits, hyphen, single quote, " +
			"double quote, space, and exclamation point")
	}
	return nil
}

func checkParent(name, parent string) error {
	if !parentPattern.MatchString(parent) {
		return badRequest(name + " must be organizations/ID or folders/ID")
	}
	return nil
}

// labelText phrases labels in key order.
func labelText(labels map[string]string) string {
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, boundedText(key, maxLabelText)+"="+boundedText(labels[key], maxLabelText))
	}
	return strings.Join(parts, ", ")
}

type projectWriteArguments struct {
	ProjectID         string            `json:"project_id"`
	DisplayName       *string           `json:"display_name"`
	Parent            string            `json:"parent"`
	Labels            map[string]string `json:"labels"`
	Etag              string            `json:"etag"`
	CurrentParent     string            `json:"current_parent"`
	DestinationParent string            `json:"destination_parent"`
}

// projectSummaryText phrases a call from its arguments alone, naming what the person confirms.
func projectSummaryText(tool string, a projectWriteArguments) string {
	project := boundedText(a.ProjectID, maxNameText)
	switch tool {
	case projectsCreateID:
		out := "Create project " + project
		if a.DisplayName != nil {
			out += ", display name \"" + boundedText(*a.DisplayName, maxNameText) + "\""
		}
		out += ", under " + boundedText(a.Parent, maxNameText)
		if len(a.Labels) > 0 {
			out += ", labels " + labelText(a.Labels)
		}
		return out + "; no billing account is linked"
	case projectsMoveID:
		return "Move project " + project + " from " + boundedText(a.CurrentParent, maxNameText) + " to " +
			boundedText(a.DestinationParent, maxNameText) + ": the IAM roles and policies it inherits change"
	case projectsPatchID:
		var parts []string
		if a.DisplayName != nil {
			parts = append(parts, "display name \""+boundedText(*a.DisplayName, maxNameText)+"\"")
		}
		switch {
		case a.Labels == nil:
		case len(a.Labels) == 0:
			parts = append(parts, "all labels removed")
		default:
			parts = append(parts, "labels replaced by "+labelText(a.Labels)+" (every other label is removed)")
		}
		out := "Change project " + project + ": " + strings.Join(parts, "; ")
		if a.Etag != "" {
			out += "; only if the etag still matches"
		}
		return out
	}
	return "Restore project " + project + ", which was marked for deletion"
}

// projectSummary is the confirmation of a tool. A call whose confirmation would be cut says so and is
// refused by the handler.
func projectSummary(tool string) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		var a projectWriteArguments
		if json.Unmarshal(raw, &a) != nil {
			return ""
		}
		text := projectSummaryText(tool, a)
		if !summaryFits(text) {
			return "This call will be refused: " + confirmationTooLong
		}
		return text
	}
}

// --- projects.create ---

var projectsCreate = capability.Descriptor{
	ID: projectsCreateID, Version: 1, Title: "Create a Google Cloud project",
	Description: "Create a project (projects.create of Cloud Resource Manager v3) under an organization or a " +
		"folder. The parent is required and must lie in the targets: an organization only if the connection names " +
		"that organization as a target, even a connection without targets, and a folder if it is a target, lies " +
		"below a target, which Qatlas proves by reading the folders above it, or the connection has no targets. A " +
		"connection with only project or billing account targets cannot create projects. The new project lies below " +
		"its parent and so in the targets. The project gets no billing account, so services that need billing " +
		"stay unusable until a person links one outside Qatlas. Not idempotent: a repeated call fails with a " +
		"conflict because the ID exists, and a deleted project's ID is not free again at once." + projectChangeNote,
	Tags: append(append([]string{}, projectToolTags...), "create"), Provider: Provider,
	Risk: resourceManagerWriteRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent),
	InputSchema: json.RawMessage(`{"type":"object","properties":{` +
		`"project_id":{"type":"string","minLength":6,"maxLength":30,"pattern":"^[a-z][a-z0-9-]{4,28}[a-z0-9]$"},` +
		projectDisplayNameProp + `,` + parentArgProp("parent") + `,` + projectLabelsProp + `},` +
		`"required":["project_id","parent"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(serviceOperationSchema),
	Arguments: []capability.Argument{
		{Name: "project_id", Required: true, Description: "New project ID: 6 to 30 lowercase letters, digits, and " +
			"hyphens, starting with a letter and not ending with a hyphen; unique across Google Cloud and " +
			"immutable"},
		projectDisplayNameArgument,
		{Name: "parent", Required: true, Description: "organizations/ID or folders/ID the project is created " +
			"under; never omitted"},
		projectLabelsArgument},
	Fields: resourceManagerOperationFields,
	Examples: []capability.Example{{Description: "Create a project under a folder",
		Arguments: json.RawMessage(`{"project_id":"my-new-project-123","display_name":"My New Project",` +
			`"parent":"folders/2001","labels":{"env":"test"}}`)}},
}

func handleProjectsCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "create project"
	var a projectWriteArguments
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if err := checkConnection(resolved); err != nil {
		return nil, err
	}
	list, err := allowlistOf(resolved)
	if err != nil {
		return nil, failure(op, "the targets of the connection are not usable")
	}
	if !projectIDShape.MatchString(a.ProjectID) {
		return nil, badRequest("project_id must be 6 to 30 lowercase letters, digits, and hyphens, starting with " +
			"a letter and ending with a letter or digit")
	}
	if a.DisplayName != nil {
		if err := checkDisplayName(*a.DisplayName); err != nil {
			return nil, err
		}
	}
	if err := checkParent("parent", a.Parent); err != nil {
		return nil, err
	}
	if err := checkProjectLabels(a.Labels); err != nil {
		return nil, err
	}
	if !summaryFits(projectSummaryText(projectsCreateID, a)) {
		return nil, badRequest(confirmationTooLong)
	}
	if allowed, decided := list.decidesWriteParent(a.Parent); decided && !allowed {
		return nil, refuseOutside("the parent")
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	admitted, err := newAdmitter(list, c, op, projectsCreateID).argumentParent(ctx, a.Parent)
	if err != nil {
		return nil, err
	}
	if !admitted {
		return nil, refuseOutside("the parent")
	}
	body := struct {
		ProjectID   string            `json:"projectId"`
		DisplayName *string           `json:"displayName,omitempty"`
		Parent      string            `json:"parent"`
		Labels      map[string]string `json:"labels,omitempty"`
	}{a.ProjectID, a.DisplayName, a.Parent, a.Labels}
	return c.changeResourceManager(ctx, op, projectsCreateID, http.MethodPost, "/v3/projects", nil, body)
}

// --- projects.patch ---

var projectsPatch = capability.Descriptor{
	ID: projectsPatchID, Version: 1, Title: "Change a Google Cloud project",
	Description: "Change the display name or the labels of a project (projects.patch of Cloud Resource Manager v3). " +
		"Only the arguments that are set are changed and at least one is required; Qatlas sends exactly them as " +
		"the update mask. Setting labels replaces the whole label list: every label not given is removed, and an " +
		"empty object removes them all. The optional etag makes Google refuse the change with a conflict if the " +
		"project changed since the etag was read. The project must lie in the targets of the connection, which " +
		"Qatlas proves by reading it and the folders above it before the change." + projectChangeNote,
	Tags: append(append([]string{}, projectToolTags...), "update"), Provider: Provider,
	Risk: resourceManagerWriteRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + serviceProjectProp + `,` +
		projectDisplayNameProp + `,` + projectLabelsProp + `,` + projectEtagProp + `},` +
		`"required":["project_id"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(serviceOperationSchema),
	Arguments: []capability.Argument{
		{Name: "project_id", Required: true, Description: "Project ID or number; never a projects/ name or a path"},
		projectDisplayNameArgument,
		{Name: "labels", Description: "New labels as an object of key to value. They replace all labels of the " +
			"project; an empty object removes them all. At most 64 entries, keys of 1 to 63 lowercase letters, " +
			"digits, or hyphens starting with a letter, values empty or of the same form"},
		{Name: "etag", Description: "Etag of the project as projects.get returned it; Google refuses the change " +
			"if the project changed since"}},
	Fields: resourceManagerOperationFields,
	Examples: []capability.Example{{Description: "Rename a project and set its labels",
		Arguments: json.RawMessage(`{"project_id":"my-project-123","display_name":"My Project","labels":{"env":"prod"}}`)}},
}

func handleProjectsPatch(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "change project"
	var a projectWriteArguments
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if a.DisplayName == nil && a.Labels == nil {
		return nil, badRequest("set at least one of display_name and labels")
	}
	if err := checkConnection(resolved); err != nil {
		return nil, err
	}
	list, err := allowlistOf(resolved)
	if err != nil {
		return nil, failure(op, "the targets of the connection are not usable")
	}
	if !validProject(a.ProjectID) {
		return nil, badRequest("project_id must be a project ID or a project number, never a projects/ name or a path")
	}
	if a.DisplayName != nil {
		if err := checkDisplayName(*a.DisplayName); err != nil {
			return nil, err
		}
	}
	if err := checkProjectLabels(a.Labels); err != nil {
		return nil, err
	}
	if len(a.Etag) > maxEtagText {
		return nil, badRequest("etag is too long")
	}
	if !summaryFits(projectSummaryText(projectsPatchID, a)) {
		return nil, badRequest(confirmationTooLong)
	}
	if allowed, decided := list.decidesProject(a.ProjectID); decided && !allowed {
		return nil, refuseOutside("the project")
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	project, err := boundProject(ctx, c, newAdmitter(list, c, op, projectsPatchID), op, projectsPatchID, a.ProjectID)
	if err != nil {
		return nil, err
	}
	path, err := projectPath(op, project)
	if err != nil {
		return nil, err
	}
	var mask []string
	if a.DisplayName != nil {
		mask = append(mask, "displayName")
	}
	if a.Labels != nil {
		mask = append(mask, "labels")
	}
	body := struct {
		DisplayName *string           `json:"displayName,omitempty"`
		Labels      map[string]string `json:"labels,omitempty"`
		Etag        string            `json:"etag,omitempty"`
	}{a.DisplayName, a.Labels, a.Etag}
	return c.changeResourceManager(ctx, op, projectsPatchID, http.MethodPatch, path,
		url.Values{"updateMask": {strings.Join(mask, ",")}}, body)
}

// --- projects.move ---

var projectsMove = capability.Descriptor{
	ID: projectsMoveID, Version: 1, Title: "Move a Google Cloud project",
	Description: "Move a project to another organization or folder (projects.move of Cloud Resource Manager v3). " +
		"The project loses the IAM roles and organization policies it inherited from its old parent and inherits " +
		"those of the new one, which can widen or cut access. current_parent must name the parent the project " +
		"has now: Qatlas reads the project first and refuses without any change if its parent differs, so the " +
		"confirmation names the old and the new parent truthfully. current_parent is only compared and never sent. " +
		"The project, its current parent, and the destination must all lie in the targets: an organization " +
		"counts only if the connection names it as a target, a folder if it is a target, lies below one, which " +
		"Qatlas proves by reading the folders above it, or the connection has no targets. A connection with only " +
		"project or billing account targets cannot move projects." + projectChangeNote,
	Tags: append(append([]string{}, projectToolTags...), "update", "move"), Provider: Provider,
	Risk: resourceManagerWriteRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + serviceProjectProp + `,` +
		parentArgProp("current_parent") + `,` + parentArgProp("destination_parent") + `},` +
		`"required":["project_id","current_parent","destination_parent"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(serviceOperationSchema),
	Arguments: []capability.Argument{
		{Name: "project_id", Required: true, Description: "Project ID or number; never a projects/ name or a path"},
		{Name: "current_parent", Required: true, Description: "organizations/ID or folders/ID the project is in now, " +
			"as projects.get shows; the move is refused without a change if the project has another parent"},
		{Name: "destination_parent", Required: true, Description: "organizations/ID or folders/ID to move the " +
			"project under"}},
	Fields: resourceManagerOperationFields,
	Examples: []capability.Example{{Description: "Move a project to another folder",
		Arguments: json.RawMessage(`{"project_id":"my-project-123","current_parent":"folders/2001",` +
			`"destination_parent":"folders/2002"}`)}},
}

func handleProjectsMove(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "move project"
	var a projectWriteArguments
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if err := checkConnection(resolved); err != nil {
		return nil, err
	}
	list, err := allowlistOf(resolved)
	if err != nil {
		return nil, failure(op, "the targets of the connection are not usable")
	}
	if !validProject(a.ProjectID) {
		return nil, badRequest("project_id must be a project ID or a project number, never a projects/ name or a path")
	}
	if err := checkParent("current_parent", a.CurrentParent); err != nil {
		return nil, err
	}
	if err := checkParent("destination_parent", a.DestinationParent); err != nil {
		return nil, err
	}
	if !summaryFits(projectSummaryText(projectsMoveID, a)) {
		return nil, badRequest(confirmationTooLong)
	}
	if allowed, decided := list.decidesProject(a.ProjectID); decided && !allowed {
		return nil, refuseOutside("the project")
	}
	for _, parent := range []string{a.CurrentParent, a.DestinationParent} {
		if allowed, decided := list.decidesWriteParent(parent); decided && !allowed {
			return nil, refuseOutside("the parent")
		}
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	adm := newAdmitter(list, c, op, projectsMoveID)
	project, err := boundProject(ctx, c, adm, op, projectsMoveID, a.ProjectID)
	if err != nil {
		return nil, err
	}
	if project.Parent != a.CurrentParent {
		return nil, badRequest("current_parent is not the parent of the project; read the project again, nothing " +
			"was changed")
	}
	for _, parent := range []string{a.CurrentParent, a.DestinationParent} {
		admitted, err := adm.argumentParent(ctx, parent)
		if err != nil {
			return nil, err
		}
		if !admitted {
			return nil, refuseOutside("the parent")
		}
	}
	path, err := projectPath(op, project)
	if err != nil {
		return nil, err
	}
	return c.changeResourceManager(ctx, op, projectsMoveID, http.MethodPost, path+":move", nil,
		struct {
			DestinationParent string `json:"destinationParent"`
		}{a.DestinationParent})
}

// --- projects.undelete ---

var projectsUndelete = capability.Descriptor{
	ID: projectsUndeleteID, Version: 1, Title: "Restore a Google Cloud project",
	Description: "Restore a project that was marked for deletion (projects.undelete of Cloud Resource Manager v3). " +
		"Google allows it only for a project in the state DELETE_REQUESTED, and only until the project is deleted " +
		"for good; Qatlas does not check the state first, so Google's refusal for an active project is a provider " +
		"error. The project must lie in the targets of the connection, which Qatlas proves by reading it and the " +
		"folders above it." + projectChangeNote,
	Tags: append(append([]string{}, projectToolTags...), "update", "undelete"), Provider: Provider,
	Risk: resourceManagerWriteRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + serviceProjectProp + `},` +
		`"required":["project_id"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(serviceOperationSchema),
	Arguments: []capability.Argument{
		{Name: "project_id", Required: true, Description: "Project ID or number; never a projects/ name or a path"}},
	Fields: resourceManagerOperationFields,
	Examples: []capability.Example{{Description: "Restore a project",
		Arguments: json.RawMessage(`{"project_id":"my-project-123"}`)}},
}

func handleProjectsUndelete(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "restore project"
	var a projectWriteArguments
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if err := checkConnection(resolved); err != nil {
		return nil, err
	}
	list, err := allowlistOf(resolved)
	if err != nil {
		return nil, failure(op, "the targets of the connection are not usable")
	}
	if !validProject(a.ProjectID) {
		return nil, badRequest("project_id must be a project ID or a project number, never a projects/ name or a path")
	}
	if allowed, decided := list.decidesProject(a.ProjectID); decided && !allowed {
		return nil, refuseOutside("the project")
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	project, err := boundProject(ctx, c, newAdmitter(list, c, op, projectsUndeleteID), op, projectsUndeleteID, a.ProjectID)
	if err != nil {
		return nil, err
	}
	path, err := projectPath(op, project)
	if err != nil {
		return nil, err
	}
	return c.changeResourceManager(ctx, op, projectsUndeleteID, http.MethodPost, path+":undelete", nil, struct{}{})
}
