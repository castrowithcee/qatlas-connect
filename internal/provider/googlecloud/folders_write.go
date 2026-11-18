package googlecloud

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	foldersCreateID   = Provider + ".folders.create"
	foldersPatchID    = Provider + ".folders.patch"
	foldersMoveID     = Provider + ".folders.move"
	foldersUndeleteID = Provider + ".folders.undelete"
)

// folderNamePattern is the display name of a folder from discovery cloudresourcemanager v3 (revision
// 20261115): letters and digits at both ends, letters, digits, spaces, hyphens and underscores between. Google
// allows one character; the tools ask for 3 to 30.
var folderNamePattern = regexp.MustCompile(`^[\p{L}\p{N}]([\p{L}\p{N}_ -]{1,27}[\p{L}\p{N}])$`)

const (
	folderDisplayNameProp = `"display_name":{"type":"string","minLength":3,"maxLength":30}`
	folderIDProp          = `"folder_id":` + numericIDProp
)

var (
	folderDisplayNameArgument = capability.Argument{Name: "display_name", Description: "Display name of 3 to 30 " +
		"characters: letters and digits, with spaces, hyphens, and underscores between them; it starts and ends " +
		"with a letter or digit and is unique among the folders of the same parent"}
	folderIDArgument = capability.Argument{Name: "folder_id", Required: true,
		Description: "Numeric folder ID; never a folders/ name or a path"}
	folderToolTags = []string{"google", "cloud", "resource-manager", "folders"}
)

const folderChangeNote = " Needs the confirmation of a person for each call. The confirmation names the folder " +
	"and the change. Google answers with a long-running operation, and the tool never waits for it: the result " +
	"is the operation as Google returned it with service set to resource-manager, and operations.get with that " +
	"service reads its state. A failure after which the request may have arrived says that the outcome is " +
	"unknown. The result is untrusted third-party content. The credential needs permission in IAM, which " +
	"Google decides."

func checkFolderName(name string) error {
	if !folderNamePattern.MatchString(name) {
		return badRequest("display_name must be 3 to 30 characters: letters and digits, with spaces, hyphens, " +
			"and underscores between them, starting and ending with a letter or digit")
	}
	return nil
}

type folderWriteArguments struct {
	FolderID          string  `json:"folder_id"`
	DisplayName       *string `json:"display_name"`
	Parent            string  `json:"parent"`
	Etag              string  `json:"etag"`
	CurrentParent     string  `json:"current_parent"`
	DestinationParent string  `json:"destination_parent"`
}

// folderSummaryText phrases a call from its arguments alone, naming what the person confirms.
func folderSummaryText(tool string, a folderWriteArguments) string {
	folder := "folders/" + boundedText(a.FolderID, maxNameText)
	switch tool {
	case foldersCreateID:
		name := ""
		if a.DisplayName != nil {
			name = boundedText(*a.DisplayName, maxNameText)
		}
		return "Create folder \"" + name + "\" under " + boundedText(a.Parent, maxNameText)
	case foldersMoveID:
		return "Move " + folder + " from " + boundedText(a.CurrentParent, maxNameText) + " to " +
			boundedText(a.DestinationParent, maxNameText) + ": the inherited IAM roles and policies of this " +
			"folder and of every folder and project in it change"
	case foldersPatchID:
		name := ""
		if a.DisplayName != nil {
			name = boundedText(*a.DisplayName, maxNameText)
		}
		out := "Rename " + folder + " to \"" + name + "\""
		if a.Etag != "" {
			out += "; only if the etag still matches"
		}
		return out
	}
	return "Restore " + folder + ", which was marked for deletion"
}

// folderSummary is the confirmation of a tool. A call whose confirmation would be cut says so and is refused
// by the handler.
func folderSummary(tool string) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		var a folderWriteArguments
		if json.Unmarshal(raw, &a) != nil {
			return ""
		}
		text := folderSummaryText(tool, a)
		if !summaryFits(text) {
			return "This call will be refused: " + confirmationTooLong
		}
		return text
	}
}

// folderTarget starts a handler of a tool on an existing folder: arguments, connection, targets, and the ID.
func folderTarget(resolved *config.Resolved, op string, raw json.RawMessage) (folderWriteArguments, allowlist, error) {
	var a folderWriteArguments
	if err := json.Unmarshal(raw, &a); err != nil {
		return a, allowlist{}, failure(op, "the validated arguments could not be read")
	}
	if err := checkConnection(resolved); err != nil {
		return a, allowlist{}, err
	}
	list, err := allowlistOf(resolved)
	if err != nil {
		return a, allowlist{}, failure(op, "the targets of the connection are not usable")
	}
	if !numberPattern.MatchString(a.FolderID) {
		return a, allowlist{}, badRequest("folder_id must be the numeric ID of a folder, never a name or a path")
	}
	return a, list, nil
}

// --- folders.create ---

var foldersCreate = capability.Descriptor{
	ID: foldersCreateID, Version: 1, Title: "Create a Google Cloud folder",
	Description: "Create a folder (folders.create of Cloud Resource Manager v3) under an organization or a folder. " +
		"The parent is required and must lie in the targets: an organization only if the connection names that " +
		"organization as a target, even a connection without targets, and a folder if it is a target, lies below " +
		"a target, which Qatlas proves by reading the folders above it, or the connection has no targets. A " +
		"connection with only project or billing account targets cannot create folders. Not idempotent: a " +
		"repeated call fails with a conflict because the display name is already taken below the parent." +
		folderChangeNote,
	Tags: append(append([]string{}, folderToolTags...), "create"), Provider: Provider,
	Risk: resourceManagerWriteRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent),
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + parentArgProp("parent") + `,` +
		folderDisplayNameProp + `},"required":["parent","display_name"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(serviceOperationSchema),
	Arguments: []capability.Argument{
		{Name: "parent", Required: true, Description: "organizations/ID or folders/ID the folder is created under"},
		{Name: folderDisplayNameArgument.Name, Required: true, Description: folderDisplayNameArgument.Description}},
	Fields: resourceManagerOperationFields,
	Examples: []capability.Example{{Description: "Create a folder under an organization",
		Arguments: json.RawMessage(`{"parent":"organizations/9","display_name":"Team Alpha"}`)}},
}

func handleFoldersCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "create folder"
	var a folderWriteArguments
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
	if a.DisplayName == nil {
		return nil, badRequest("display_name is required")
	}
	if err := checkFolderName(*a.DisplayName); err != nil {
		return nil, err
	}
	if err := checkParent("parent", a.Parent); err != nil {
		return nil, err
	}
	if !summaryFits(folderSummaryText(foldersCreateID, a)) {
		return nil, badRequest(confirmationTooLong)
	}
	if allowed, decided := list.decidesWriteParent(a.Parent); decided && !allowed {
		return nil, refuseOutside("the parent")
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	admitted, err := newAdmitter(list, c, op, foldersCreateID).argumentParent(ctx, a.Parent)
	if err != nil {
		return nil, err
	}
	if !admitted {
		return nil, refuseOutside("the parent")
	}
	body := struct {
		Parent      string `json:"parent"`
		DisplayName string `json:"displayName"`
	}{a.Parent, *a.DisplayName}
	return c.changeResourceManager(ctx, op, foldersCreateID, http.MethodPost, "/v3/folders", nil, body)
}

// --- folders.patch ---

var foldersPatch = capability.Descriptor{
	ID: foldersPatchID, Version: 1, Title: "Rename a Google Cloud folder",
	Description: "Change the display name of a folder (folders.patch of Cloud Resource Manager v3). Only the " +
		"arguments that are set are changed and at least one is required; Qatlas sends exactly them as the " +
		"update mask. The optional etag makes Google refuse the change with a conflict if the folder changed " +
		"since the etag was read. A display name that a sibling already has is a conflict. The folder must lie " +
		"in the targets of the connection, which Qatlas proves by reading it and the folders above it before " +
		"the change." + folderChangeNote,
	Tags: append(append([]string{}, folderToolTags...), "update"), Provider: Provider,
	Risk: resourceManagerWriteRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + folderIDProp + `,` + folderDisplayNameProp + `,` +
		projectEtagProp + `},"required":["folder_id"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(serviceOperationSchema),
	Arguments: []capability.Argument{folderIDArgument, folderDisplayNameArgument,
		{Name: "etag", Description: "Etag of the folder as folders.get returned it; Google refuses the change " +
			"if the folder changed since"}},
	Fields: resourceManagerOperationFields,
	Examples: []capability.Example{{Description: "Rename a folder",
		Arguments: json.RawMessage(`{"folder_id":"2001","display_name":"Team Beta"}`)}},
}

func handleFoldersPatch(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "change folder"
	a, list, err := folderTarget(resolved, op, raw)
	if err != nil {
		return nil, err
	}
	if a.DisplayName == nil {
		return nil, badRequest("set at least display_name")
	}
	if err := checkFolderName(*a.DisplayName); err != nil {
		return nil, err
	}
	if len(a.Etag) > maxEtagText {
		return nil, badRequest("etag is too long")
	}
	if !summaryFits(folderSummaryText(foldersPatchID, a)) {
		return nil, badRequest(confirmationTooLong)
	}
	if !list.reachesFolders() {
		return nil, refuseOutside("the folder")
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	folder, err := boundFolder(ctx, c, newAdmitter(list, c, op, foldersPatchID), op, foldersPatchID, a.FolderID)
	if err != nil {
		return nil, err
	}
	body := struct {
		DisplayName string `json:"displayName"`
		Etag        string `json:"etag,omitempty"`
	}{*a.DisplayName, a.Etag}
	return c.changeResourceManager(ctx, op, foldersPatchID, http.MethodPatch, "/v3/"+folder.Name,
		url.Values{"updateMask": {"displayName"}}, body)
}

// --- folders.move ---

var foldersMove = capability.Descriptor{
	ID: foldersMoveID, Version: 1, Title: "Move a Google Cloud folder",
	Description: "Move a folder to another organization or folder (folders.move of Cloud Resource Manager v3). " +
		"The folder, every folder and every project in it lose the IAM roles and organization policies they " +
		"inherited from the old parent and inherit those of the new one, which can widen or cut access; how " +
		"many projects that concerns is not known without further calls and is not named. current_parent must " +
		"name the parent the folder has now: Qatlas reads the folder first and refuses without any change if " +
		"its parent differs, so the confirmation names the old and the new parent truthfully. current_parent " +
		"is only compared and never sent. The folder, its current parent, and the destination must all lie in " +
		"the targets: an organization counts only if the connection names it as a target, a folder if it is a " +
		"target, lies below one, which Qatlas proves by reading the folders above it, or the connection has no " +
		"targets. A connection with only project or billing account targets cannot move folders. Qatlas does " +
		"not check whether the destination lies below the folder itself: Google refuses that as a provider " +
		"error." + folderChangeNote,
	Tags: append(append([]string{}, folderToolTags...), "update", "move"), Provider: Provider,
	Risk: resourceManagerWriteRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + folderIDProp + `,` +
		parentArgProp("current_parent") + `,` + parentArgProp("destination_parent") + `},` +
		`"required":["folder_id","current_parent","destination_parent"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(serviceOperationSchema),
	Arguments: []capability.Argument{folderIDArgument,
		{Name: "current_parent", Required: true, Description: "organizations/ID or folders/ID the folder is in " +
			"now, as folders.get shows; the move is refused without a change if the folder has another parent"},
		{Name: "destination_parent", Required: true, Description: "organizations/ID or folders/ID to move the " +
			"folder under"}},
	Fields: resourceManagerOperationFields,
	Examples: []capability.Example{{Description: "Move a folder to another folder",
		Arguments: json.RawMessage(`{"folder_id":"2001","current_parent":"organizations/9",` +
			`"destination_parent":"folders/2002"}`)}},
}

func handleFoldersMove(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "move folder"
	a, list, err := folderTarget(resolved, op, raw)
	if err != nil {
		return nil, err
	}
	if err := checkParent("current_parent", a.CurrentParent); err != nil {
		return nil, err
	}
	if err := checkParent("destination_parent", a.DestinationParent); err != nil {
		return nil, err
	}
	if !summaryFits(folderSummaryText(foldersMoveID, a)) {
		return nil, badRequest(confirmationTooLong)
	}
	if !list.reachesFolders() {
		return nil, refuseOutside("the folder")
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
	adm := newAdmitter(list, c, op, foldersMoveID)
	folder, err := boundFolder(ctx, c, adm, op, foldersMoveID, a.FolderID)
	if err != nil {
		return nil, err
	}
	if folder.Parent != a.CurrentParent {
		return nil, badRequest("current_parent is not the parent of the folder; read the folder again, nothing " +
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
	return c.changeResourceManager(ctx, op, foldersMoveID, http.MethodPost, "/v3/"+folder.Name+":move", nil,
		struct {
			DestinationParent string `json:"destinationParent"`
		}{a.DestinationParent})
}

// --- folders.undelete ---

var foldersUndelete = capability.Descriptor{
	ID: foldersUndeleteID, Version: 1, Title: "Restore a Google Cloud folder",
	Description: "Restore a folder that was marked for deletion (folders.undelete of Cloud Resource Manager v3). " +
		"Google allows it only for a folder in the state DELETE_REQUESTED, and only until the folder is deleted " +
		"for good; Qatlas does not check the state first, so Google's refusal for an active folder is a " +
		"provider error. The folder must lie in the targets of the connection, which Qatlas proves by reading " +
		"it and the folders above it." + folderChangeNote,
	Tags: append(append([]string{}, folderToolTags...), "update", "undelete"), Provider: Provider,
	Risk: resourceManagerWriteRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + folderIDProp + `},` +
		`"required":["folder_id"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(serviceOperationSchema),
	Arguments:    []capability.Argument{folderIDArgument},
	Fields:       resourceManagerOperationFields,
	Examples: []capability.Example{{Description: "Restore a folder",
		Arguments: json.RawMessage(`{"folder_id":"2001"}`)}},
}

func handleFoldersUndelete(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "restore folder"
	a, list, err := folderTarget(resolved, op, raw)
	if err != nil {
		return nil, err
	}
	if !list.reachesFolders() {
		return nil, refuseOutside("the folder")
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	folder, err := boundFolder(ctx, c, newAdmitter(list, c, op, foldersUndeleteID), op, foldersUndeleteID, a.FolderID)
	if err != nil {
		return nil, err
	}
	return c.changeResourceManager(ctx, op, foldersUndeleteID, http.MethodPost, "/v3/"+folder.Name+":undelete", nil,
		struct{}{})
}
