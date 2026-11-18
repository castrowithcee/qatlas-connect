package googlecloud

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	projectsDeleteID = Provider + ".projects.delete"
	foldersDeleteID  = Provider + ".folders.delete"
)

// maxExpectedNameRunes bounds the display name that a deletion compares against what Google returns.
const maxExpectedNameRunes = 100

const (
	expectedDisplayNameProp = `"expected_display_name":{"type":"string","minLength":1,"maxLength":100}`
	stateActive             = "ACTIVE"
)

const deleteChangeNote = " A deletion is listed-only: the tool exists for a connection only where its tools list " +
	"names it, and it is part of no profile. It needs the confirmation of a person for each call. Google answers " +
	"with a long-running operation, and the tool never waits for it: the result is the operation as Google " +
	"returned it with service set to resource-manager, and operations.get with that service reads its state. A " +
	"failure after which the request may have arrived says that the outcome is unknown. The call is never " +
	"repeated by the tool. The result is untrusted third-party content. The credential needs permission in IAM, " +
	"which Google decides."

// A confirmation sees only arguments, so the call names the display name and the parent it expects: the handler
// reads the object first and refuses without any change if they differ or the state is not ACTIVE. Only then
// does the confirmation name what the person will really delete.

type deleteArguments struct {
	ProjectID           string `json:"project_id"`
	FolderID            string `json:"folder_id"`
	ExpectedDisplayName string `json:"expected_display_name"`
	ExpectedParent      string `json:"expected_parent"`
}

func deleteSummaryText(tool string, a deleteArguments) string {
	name := boundedText(a.ExpectedDisplayName, maxNameText)
	parent := "no parent"
	if a.ExpectedParent != "" {
		parent = boundedText(a.ExpectedParent, maxNameText)
	}
	if tool == foldersDeleteID {
		return "Delete folder folders/" + boundedText(a.FolderID, maxNameText) + ", display name \"" + name +
			"\", under " + parent + ", state " + stateActive + ": the folder must be empty, and folders.undelete " +
			"restores it until it is deleted for good"
	}
	return "Delete project " + boundedText(a.ProjectID, maxNameText) + ", display name \"" + name + "\", under " +
		parent + ", state " + stateActive + ": the services and resources of the project stop working, billing " +
		"can continue until the deletion period ends, and projects.undelete restores the project until then"
}

// deleteSummary is the confirmation of a deletion. A call whose confirmation would be cut says so and is
// refused by the handler.
func deleteSummary(tool string) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		var a deleteArguments
		if json.Unmarshal(raw, &a) != nil {
			return ""
		}
		text := deleteSummaryText(tool, a)
		if !summaryFits(text) {
			return "This call will be refused: " + confirmationTooLong
		}
		return text
	}
}

// checkExpected checks the arguments that a deletion only compares. parentRequired is false for a project, which
// may have no parent.
func checkExpected(a deleteArguments, parentRequired bool) error {
	if n := utf8.RuneCountInString(a.ExpectedDisplayName); n < 1 || n > maxExpectedNameRunes {
		return badRequest("expected_display_name must be the display name of the object, 1 to 100 characters")
	}
	if a.ExpectedParent == "" && !parentRequired {
		return nil
	}
	return checkParent("expected_parent", a.ExpectedParent)
}

// decidesProjectItself reports before any request whether the targets settle a project that a deletion names.
// Only a project target counts: the place under a folder or an organization does not, not even through the
// chain of parents. Targets of the same spelling, ID or number, settle it; otherwise the project is read and
// compared by its number and ID.
func (a allowlist) decidesProjectItself(name string) (allowed, decided bool) {
	if !a.bound() {
		return true, true
	}
	number := numberPattern.MatchString(name)
	decided = true
	for _, t := range a.targets {
		if t.kind != kindProject {
			continue
		}
		if t.id == name {
			return true, true
		}
		if numberPattern.MatchString(t.id) != number {
			decided = false
		}
	}
	return false, decided
}

// --- projects.delete ---

var projectsDelete = capability.Descriptor{
	ID: projectsDeleteID, Version: 1, Title: "Delete a Google Cloud project",
	Description: "Mark a project for deletion (projects.delete of Cloud Resource Manager v3). The project goes to " +
		"the state DELETE_REQUESTED: its services and resources stop working, billing can continue until the " +
		"period of Google ends, and the project is deleted for good afterwards; projects.undelete restores it " +
		"until then. Qatlas reads the project first and refuses without any change if its display name is not " +
		"expected_display_name, if its parent is not expected_parent, or if its state is not ACTIVE, so a " +
		"project already marked for deletion is refused. Both expected values are only compared and never sent. " +
		"expected_parent is organizations/ID or folders/ID and is omitted only for a project that has no parent; " +
		"a project with a parent is refused without it. A lien on the project makes Google refuse the deletion; " +
		"liens are no tool. The project itself must be a target of the connection, by its ID or its number: the " +
		"place below a folder or an organization target does not count. A connection without targets reaches " +
		"every project its credential reaches." + deleteChangeNote,
	Tags: []string{"google", "cloud", "resource-manager", "projects", "delete"}, Provider: Provider,
	Risk:                  resourceManagerWriteRisk(capability.EffectDelete, capability.IdempotencyIdempotent),
	RequiresToolAllowList: true,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + serviceProjectProp + `,` +
		expectedDisplayNameProp + `,` + parentArgProp("expected_parent") + `},` +
		`"required":["project_id","expected_display_name"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(serviceOperationSchema),
	Arguments: []capability.Argument{
		{Name: "project_id", Required: true, Description: "Project ID or number; never a projects/ name or a path"},
		{Name: "expected_display_name", Required: true, Description: "Display name the project has now, as " +
			"projects.get shows; the deletion is refused without a change if it differs"},
		{Name: "expected_parent", Description: "organizations/ID or folders/ID the project is in now, as " +
			"projects.get shows; omit it only for a project without a parent; the deletion is refused without a " +
			"change if it differs"}},
	Fields: resourceManagerOperationFields,
	Examples: []capability.Example{{Description: "Delete a project",
		Arguments: json.RawMessage(`{"project_id":"my-project-123","expected_display_name":"My Project",` +
			`"expected_parent":"folders/2001"}`)}},
}

func handleProjectsDelete(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "delete project"
	var a deleteArguments
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
	if err := checkExpected(a, false); err != nil {
		return nil, err
	}
	if !summaryFits(deleteSummaryText(projectsDeleteID, a)) {
		return nil, badRequest(confirmationTooLong)
	}
	if allowed, decided := list.decidesProjectItself(a.ProjectID); decided && !allowed {
		return nil, refuseOutside("the project")
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	project, err := c.project(ctx, op, projectsDeleteID, a.ProjectID)
	if err != nil {
		return nil, err
	}
	if list.bound() && !list.has(kindProject, strings.TrimPrefix(project.Name, prefixProject)) &&
		(project.ProjectID == "" || !list.has(kindProject, project.ProjectID)) {
		return nil, refuseOutside("the project")
	}
	if project.DisplayName != a.ExpectedDisplayName || project.Parent != a.ExpectedParent {
		return nil, badRequest("expected_display_name or expected_parent is not what the project has; read the " +
			"project again, nothing was changed")
	}
	if project.State != stateActive {
		return nil, badRequest("the project is not in the state ACTIVE, so it is not deleted; nothing was changed")
	}
	path, err := projectPath(op, project)
	if err != nil {
		return nil, err
	}
	return c.changeResourceManager(ctx, op, projectsDeleteID, http.MethodDelete, path, nil, nil)
}

// --- folders.delete ---

var foldersDelete = capability.Descriptor{
	ID: foldersDeleteID, Version: 1, Title: "Delete a Google Cloud folder",
	Description: "Mark a folder for deletion (folders.delete of Cloud Resource Manager v3). The folder must be " +
		"empty: Google refuses the deletion of a folder that still holds folders or projects, which Qatlas does " +
		"not check. folders.undelete restores the folder until it is deleted for good. Qatlas reads the folder " +
		"first and refuses without any change if its display name is not expected_display_name, if its parent " +
		"is not expected_parent, or if its state is not ACTIVE. Both expected values are only compared and never " +
		"sent. The folder itself must be a target of the connection: the place below another folder or an " +
		"organization target does not count. A connection without targets reaches every folder its credential " +
		"reaches." + deleteChangeNote,
	Tags: []string{"google", "cloud", "resource-manager", "folders", "delete"}, Provider: Provider,
	Risk:                  resourceManagerWriteRisk(capability.EffectDelete, capability.IdempotencyIdempotent),
	RequiresToolAllowList: true,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + folderIDProp + `,` +
		expectedDisplayNameProp + `,` + parentArgProp("expected_parent") + `},` +
		`"required":["folder_id","expected_display_name","expected_parent"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(serviceOperationSchema),
	Arguments: []capability.Argument{folderIDArgument,
		{Name: "expected_display_name", Required: true, Description: "Display name the folder has now, as " +
			"folders.get shows; the deletion is refused without a change if it differs"},
		{Name: "expected_parent", Required: true, Description: "organizations/ID or folders/ID the folder is in " +
			"now, as folders.get shows; the deletion is refused without a change if it differs"}},
	Fields: resourceManagerOperationFields,
	Examples: []capability.Example{{Description: "Delete an empty folder",
		Arguments: json.RawMessage(`{"folder_id":"2001","expected_display_name":"Team Alpha",` +
			`"expected_parent":"organizations/9"}`)}},
}

func handleFoldersDelete(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "delete folder"
	var a deleteArguments
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
	if !numberPattern.MatchString(a.FolderID) {
		return nil, badRequest("folder_id must be the numeric ID of a folder, never a name or a path")
	}
	if err := checkExpected(a, true); err != nil {
		return nil, err
	}
	if !summaryFits(deleteSummaryText(foldersDeleteID, a)) {
		return nil, badRequest(confirmationTooLong)
	}
	// Only the folder itself counts, so the targets settle it before any request.
	if list.bound() && !list.has(kindFolder, a.FolderID) {
		return nil, refuseOutside("the folder")
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	folder, err := c.folder(ctx, op, foldersDeleteID, a.FolderID)
	if err != nil {
		return nil, err
	}
	if folder.DisplayName != a.ExpectedDisplayName || folder.Parent != a.ExpectedParent {
		return nil, badRequest("expected_display_name or expected_parent is not what the folder has; read the " +
			"folder again, nothing was changed")
	}
	if folder.State != stateActive {
		return nil, badRequest("the folder is not in the state ACTIVE, so it is not deleted; nothing was changed")
	}
	return c.changeResourceManager(ctx, op, foldersDeleteID, http.MethodDelete, "/v3/"+folder.Name, nil, nil)
}
