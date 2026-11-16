package googletagmanager

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// What Google returns is untrusted content: names, notes, domains, and URLs come from whoever edits the
// account. Text types cut themselves while they are decoded and drop a value that is not a string, list
// types keep a bounded number of entries, and a member the types do not list is never held. URLs are text
// that is never fetched. The members are those of the discovery document tagmanager v2.

// Bounds of what a result shows.
const (
	maxIDRunes    = 100
	maxNameRunes  = 300
	maxNoteRunes  = 2000
	maxURLRunes   = 2048
	maxListItems  = 100
	maxEntries    = 1000
	maxPageToken  = 2048
	maxCursorSize = 4096
)

type (
	idText   string // IDs, paths, fingerprints, enumerations
	nameText string // display names and domain names
	noteText string // notes and descriptions
	urlText  string // links, never fetched
	textList []string
	idList   []string
)

func (s *idText) UnmarshalJSON(b []byte) error { *s = idText(clipJSON(b, maxIDRunes)); return nil }
func (s *nameText) UnmarshalJSON(b []byte) error {
	*s = nameText(clipJSON(b, maxNameRunes))
	return nil
}
func (s *noteText) UnmarshalJSON(b []byte) error {
	*s = noteText(clipJSON(b, maxNoteRunes))
	return nil
}
func (s *urlText) UnmarshalJSON(b []byte) error { *s = urlText(clipJSON(b, maxURLRunes)); return nil }

// UnmarshalJSON keeps at most maxListItems strings of a list, each cut to the length of a URL or domain.
func (l *textList) UnmarshalJSON(b []byte) error {
	*l = textList(clipList(b, maxURLRunes))
	return nil
}

func (l *idList) UnmarshalJSON(b []byte) error {
	*l = idList(clipList(b, maxIDRunes))
	return nil
}

func clipList(b []byte, limit int) []string {
	var raw []json.RawMessage
	if json.Unmarshal(b, &raw) != nil {
		return nil
	}
	var out []string
	for _, item := range raw {
		if len(out) == maxListItems {
			break
		}
		if text := clipJSON(item, limit); text != "" {
			out = append(out, text)
		}
	}
	return out
}

// clipJSON decodes a JSON string and cuts it to limit runes, marking a cut with …; anything else is empty.
func clipJSON(b []byte, limit int) string {
	var value string
	if json.Unmarshal(b, &value) != nil {
		return ""
	}
	value = strings.ToValidUTF8(value, "�")
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	runes := []rune(value)
	return string(runes[:limit-1]) + "…"
}

type accountFeatures struct {
	SupportMultipleContainers bool `json:"supportMultipleContainers,omitempty"`
	SupportUserPermissions    bool `json:"supportUserPermissions,omitempty"`
}

type account struct {
	Path          idText           `json:"path,omitempty"`
	AccountID     idText           `json:"accountId,omitempty"`
	Name          nameText         `json:"name,omitempty"`
	ShareData     bool             `json:"shareData,omitempty"`
	Fingerprint   idText           `json:"fingerprint,omitempty"`
	TagManagerURL urlText          `json:"tagManagerUrl,omitempty"`
	Features      *accountFeatures `json:"features,omitempty"`
}

type containerFeatures struct {
	SupportUserPermissions  bool `json:"supportUserPermissions,omitempty"`
	SupportEnvironments     bool `json:"supportEnvironments,omitempty"`
	SupportWorkspaces       bool `json:"supportWorkspaces,omitempty"`
	SupportGtagConfigs      bool `json:"supportGtagConfigs,omitempty"`
	SupportBuiltInVariables bool `json:"supportBuiltInVariables,omitempty"`
	SupportClients          bool `json:"supportClients,omitempty"`
	SupportFolders          bool `json:"supportFolders,omitempty"`
	SupportTags             bool `json:"supportTags,omitempty"`
	SupportTemplates        bool `json:"supportTemplates,omitempty"`
	SupportTriggers         bool `json:"supportTriggers,omitempty"`
	SupportVariables        bool `json:"supportVariables,omitempty"`
	SupportVersions         bool `json:"supportVersions,omitempty"`
	SupportZones            bool `json:"supportZones,omitempty"`
	SupportTransformations  bool `json:"supportTransformations,omitempty"`
}

type container struct {
	Path              idText             `json:"path,omitempty"`
	AccountID         idText             `json:"accountId,omitempty"`
	ContainerID       idText             `json:"containerId,omitempty"`
	Name              nameText           `json:"name,omitempty"`
	PublicID          idText             `json:"publicId,omitempty"`
	UsageContext      idList             `json:"usageContext,omitempty"`
	DomainName        textList           `json:"domainName,omitempty"`
	Notes             noteText           `json:"notes,omitempty"`
	TagIDs            idList             `json:"tagIds,omitempty"`
	Features          *containerFeatures `json:"features,omitempty"`
	TaggingServerURLs textList           `json:"taggingServerUrls,omitempty"`
	Fingerprint       idText             `json:"fingerprint,omitempty"`
	TagManagerURL     urlText            `json:"tagManagerUrl,omitempty"`
}

type workspace struct {
	Path          idText   `json:"path,omitempty"`
	AccountID     idText   `json:"accountId,omitempty"`
	ContainerID   idText   `json:"containerId,omitempty"`
	WorkspaceID   idText   `json:"workspaceId,omitempty"`
	Name          nameText `json:"name,omitempty"`
	Description   noteText `json:"description,omitempty"`
	Fingerprint   idText   `json:"fingerprint,omitempty"`
	TagManagerURL urlText  `json:"tagManagerUrl,omitempty"`
}

// The list answers of the API. nextPageToken is kept raw: it goes into a cursor and is bounded there.
type (
	accountsAnswer struct {
		Account       []account `json:"account"`
		NextPageToken string    `json:"nextPageToken"`
	}
	containersAnswer struct {
		Container     []container `json:"container"`
		NextPageToken string      `json:"nextPageToken"`
	}
	workspacesAnswer struct {
		Workspace     []workspace `json:"workspace"`
		NextPageToken string      `json:"nextPageToken"`
	}
)

// The list results. A list is never null, so an empty answer is an empty list.
type (
	accountsResult struct {
		Accounts   []account `json:"accounts"`
		NextCursor string    `json:"next_cursor,omitempty"`
	}
	containersResult struct {
		Containers []container `json:"containers"`
		NextCursor string      `json:"next_cursor,omitempty"`
	}
	workspacesResult struct {
		Workspaces []workspace `json:"workspaces"`
		NextCursor string      `json:"next_cursor,omitempty"`
	}
)

// Output schemas, written once from the types above.
const (
	stringSchema = `{"type":"string"}`
	boolSchema   = `{"type":"boolean"}`
	listSchema   = `{"type":"array","items":{"type":"string"}}`
)

func objectSchema(members ...string) string {
	return `{"type":"object","properties":{` + strings.Join(members, ",") + `}}`
}

func member(name, schema string) string { return `"` + name + `":` + schema }

func flags(names ...string) string {
	members := make([]string, len(names))
	for i, name := range names {
		members[i] = member(name, boolSchema)
	}
	return objectSchema(members...)
}

var (
	accountSchema = objectSchema(member("path", stringSchema), member("accountId", stringSchema),
		member("name", stringSchema), member("shareData", boolSchema), member("fingerprint", stringSchema),
		member("tagManagerUrl", stringSchema),
		member("features", flags("supportMultipleContainers", "supportUserPermissions")))
	containerSchema = objectSchema(member("path", stringSchema), member("accountId", stringSchema),
		member("containerId", stringSchema), member("name", stringSchema), member("publicId", stringSchema),
		member("usageContext", listSchema), member("domainName", listSchema), member("notes", stringSchema),
		member("tagIds", listSchema),
		member("features", flags("supportUserPermissions", "supportEnvironments", "supportWorkspaces",
			"supportGtagConfigs", "supportBuiltInVariables", "supportClients", "supportFolders", "supportTags",
			"supportTemplates", "supportTriggers", "supportVariables", "supportVersions", "supportZones",
			"supportTransformations")),
		member("taggingServerUrls", listSchema), member("fingerprint", stringSchema),
		member("tagManagerUrl", stringSchema))
	workspaceSchema = objectSchema(member("path", stringSchema), member("accountId", stringSchema),
		member("containerId", stringSchema), member("workspaceId", stringSchema), member("name", stringSchema),
		member("description", stringSchema), member("fingerprint", stringSchema),
		member("tagManagerUrl", stringSchema))
)

func listResultSchema(list, item string) string {
	return objectSchema(member(list, `{"type":"array","items":`+item+`}`), member("next_cursor", stringSchema))
}
