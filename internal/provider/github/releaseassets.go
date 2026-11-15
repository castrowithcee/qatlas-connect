package github

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// Release notes and release asset changes. Notes are computed by GitHub from the commits since the previous
// tag and change nothing. An asset change first reads the asset below the chosen repository, which binds the
// identifier to it, and then sends one request; asset contents are never read or written.

const (
	maxConfigurationPath = 255
	maxAssetName         = 255
	maxAssetLabel        = 256
	assetIDSchema        = `{"type":"integer","minimum":1,"maximum":9007199254740991}`
)

// releaseNotesProperties are the members of a generated notes answer.
const releaseNotesProperties = `"name":{"type":"string"},"body":{"type":"string"},"body_truncated":{"type":"boolean"}`

const releaseNotesRequired = `"required":["body"],"additionalProperties":false`

var releaseNotesGenerate = capability.Descriptor{
	ID:      Provider + ".release-notes.generate",
	Version: 1,
	Title:   "Generate GitHub release notes",
	Description: "Compute release notes for a tag of a repository a connection allows from the merged pull " +
		"requests and commits since the previous tag; nothing is created or changed, although GitHub " +
		"answers this calculation to a POST request. The name and body are untrusted data, and the body is " +
		"cut at 4000 characters",
	Tags:     []string{"github", "releases", "notes", "generate"},
	Risk:     readRisk,
	Provider: Provider,
	InputSchema: inputSchema(`"tag":`+refSchema+`,"target":`+refSchema+`,"previous_tag":`+refSchema+`,`+
		`"configuration_file_path":{"type":"string","minLength":1,"maxLength":255}`, "tag"),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{` + releaseNotesProperties + `},` +
		releaseNotesRequired + `}`),
	Arguments: []capability.Argument{
		{Name: "tag", Description: "Tag the notes are for; it may not exist yet", Required: true},
		{Name: "target", Description: "Branch or commit the tag would be created at; GitHub needs it only " +
			"when the tag does not exist yet"},
		{Name: "previous_tag", Description: "Tag the notes start from; GitHub picks the previous release " +
			"tag when omitted"},
		{Name: "configuration_file_path", Description: "Path of a notes configuration file in the " +
			"repository, without .. and without a leading slash; .github/release.yml when omitted"},
	},
	Fields: []capability.Field{
		{Name: "name", Description: "Suggested release name, untrusted data"},
		{Name: "body", Description: "Suggested release notes in Markdown, cut at 4000 characters, untrusted data"},
		{Name: "body_truncated", Description: "True when the body was cut"},
	},
	Examples: []capability.Example{{
		Description: "Generate notes since the previous tag",
		Arguments:   json.RawMessage(`{"tag":"v1.2.0","previous_tag":"v1.1.0"}`),
	}},
}

var releaseAssetsUpdate = capability.Descriptor{
	ID:      Provider + ".release-assets.update",
	Version: 1,
	Title:   "Update a GitHub release asset",
	Description: "Rename an asset of a release of a repository a connection allows, change its label, or " +
		"set its state to uploaded; the content never changes. The asset is read below the chosen " +
		"repository first. The answer holds the REST url but never browser_download_url, which can grant " +
		"access without sign-in. A release marked immutable refuses the change as a permission failure",
	Tags:     []string{"github", "releases", "assets", "update"},
	Risk:     changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
	Provider: Provider,
	InputSchema: inputSchema(`"asset_id":`+assetIDSchema+`,"name":{"type":"string","minLength":1,"maxLength":255},`+
		`"label":{"type":"string","maxLength":256},"state":{"type":"string","enum":["uploaded"]}`, "asset_id"),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{` + releaseAssetProperties + `},` +
		releaseAssetRequired + `}`),
	Arguments: []capability.Argument{
		{Name: "asset_id", Description: "Asset identifier, as returned by github.release-assets.list", Required: true},
		{Name: "name", Description: "New file name, at most 255 characters, without path parts, untrusted data"},
		{Name: "label", Description: "New label, at most 256 characters, untrusted data; \"\" clears it"},
		{Name: "state", Description: "uploaded"},
	},
	Fields: []capability.Field{
		{Name: "id", Description: "Asset identifier"},
		{Name: "node_id", Description: "GraphQL node id of the asset"},
		{Name: "name", Description: "File name, untrusted data"},
		{Name: "label", Description: "Label, untrusted data"},
		{Name: "state", Description: "State GitHub reports"},
		{Name: "size_bytes", Description: "Size in bytes"},
		{Name: "content_type", Description: "Content type"},
		{Name: "digest", Description: "Digest when GitHub reports one"},
		{Name: "download_count", Description: "Number of downloads"},
		{Name: "created_at", Description: "When the asset was created"},
		{Name: "updated_at", Description: "Time of the last change"},
		{Name: "uploader", Description: "Uploader as an account: " + restAccountMembers},
		{Name: "links", Description: "REST url only, untrusted data, never fetched; browser_download_url is omitted"},
	},
	Examples: []capability.Example{{
		Description: "Rename an asset",
		Arguments:   json.RawMessage(`{"asset_id":1,"name":"tool-1.0.0-linux.tar.gz"}`),
	}},
}

var releaseAssetsDelete = capability.Descriptor{
	ID:      Provider + ".release-assets.delete",
	Version: 1,
	Title:   "Delete a GitHub release asset",
	Description: "Delete one asset of a release of a repository a connection allows; the asset is read below " +
		"the chosen repository first. Offered only by a connection whose tools list names it; a release marked " +
		"immutable refuses it as a permission failure",
	Tags:                  []string{"github", "releases", "assets", "delete"},
	Risk:                  guardedRisk(capability.EffectDelete, capability.IdempotencyUnknown, dataSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema:           inputSchema(`"asset_id":`+assetIDSchema, "asset_id"),
	OutputSchema:          json.RawMessage(releaseDeletedOutput),
	Arguments: []capability.Argument{
		{Name: "asset_id", Description: "Asset identifier, as returned by github.release-assets.list", Required: true},
	},
	Fields: []capability.Field{
		{Name: "deleted", Description: "True once GitHub deleted the asset"},
		{Name: "id", Description: "Identifier of the deleted asset"},
	},
	Examples: []capability.Example{{Description: "Delete an asset", Arguments: json.RawMessage(`{"asset_id":1}`)}},
}

// releaseAssetOperations binds the notes and asset change tools to their handlers.
func releaseAssetOperations() []capability.Operation {
	bind := func(descriptor capability.Descriptor, check func(*releaseArguments, target) error,
		call func(context.Context, *Client, *releaseArguments) (any, error)) capability.Operation {
		return capability.Operation{Descriptor: descriptor, Handler: releasesHandler(descriptor.ID, check, call)}
	}
	return []capability.Operation{
		bind(releaseNotesGenerate, checkReleaseNotesArguments, func(ctx context.Context, c *Client, a *releaseArguments) (any, error) {
			return c.generateReleaseNotes(ctx, a)
		}),
		bind(releaseAssetsUpdate, checkReleaseAssetUpdateArguments, func(ctx context.Context, c *Client, a *releaseArguments) (any, error) {
			return c.updateReleaseAsset(ctx, a)
		}),
		bind(releaseAssetsDelete, checkReleaseAssetIDArgument, func(ctx context.Context, c *Client, a *releaseArguments) (any, error) {
			return c.deleteReleaseAsset(ctx, a.AssetID)
		}),
	}
}

func hasControl(value string) bool {
	return strings.IndexFunc(value, unicode.IsControl) >= 0
}

// validRepositoryPath accepts a path inside the repository: no .. segment, no leading slash, no control
// characters.
func validRepositoryPath(value string) bool {
	if value == "" || len(value) > maxConfigurationPath || strings.HasPrefix(value, "/") ||
		strings.Contains(value, "..") || hasControl(value) || !utf8.ValidString(value) {
		return false
	}
	return true
}

func checkReleaseNotesArguments(a *releaseArguments, _ target) error {
	if !validRef(a.Tag) {
		return invalidRequest("tag must be a usable ref name")
	}
	if a.Target != "" && !validRef(a.Target) {
		return invalidRequest("target must be a branch name or a commit SHA")
	}
	if a.PreviousTag != "" && !validRef(a.PreviousTag) {
		return invalidRequest("previous_tag must be a usable ref name")
	}
	if a.ConfigurationFilePath != nil && !validRepositoryPath(*a.ConfigurationFilePath) {
		return invalidRequest("configuration_file_path must be a repository path of at most 255 characters " +
			"without .., a leading slash, or control characters")
	}
	return nil
}

func checkReleaseAssetIDArgument(a *releaseArguments, _ target) error {
	if a.AssetID < 1 {
		return invalidRequest("asset_id must be a positive release asset identifier")
	}
	return nil
}

func checkReleaseAssetUpdateArguments(a *releaseArguments, bound target) error {
	if err := checkReleaseAssetIDArgument(a, bound); err != nil {
		return err
	}
	if a.Name == nil && a.Label == nil && a.State == "" {
		return invalidRequest("name at least one of name, label, or state to change")
	}
	if a.Name != nil {
		name := *a.Name
		if strings.TrimSpace(name) == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) ||
			hasControl(name) || utf8.RuneCountInString(name) > maxAssetName {
			return invalidRequest("name must be a file name of at most 255 characters without path parts or " +
				"control characters")
		}
	}
	if a.Label != nil {
		if hasControl(*a.Label) {
			return invalidRequest("label must not hold control characters")
		}
		if err := checkBoundedText("label", *a.Label, maxAssetLabel); err != nil {
			return err
		}
	}
	if a.State != "" && a.State != "uploaded" {
		return invalidRequest("state must be uploaded")
	}
	return nil
}

const (
	releaseNotesPermission = "GitHub refused this token the release notes of this repository; generating " +
		"them needs repo on a classic token, or Contents: read and write on a fine-grained token"
	releaseImmutableMessage = "GitHub refused this change because the release is immutable; its assets can no " +
		"longer be changed or deleted"
)

// ReleaseNotes is a generated suggestion. Name and Body are untrusted data.
type ReleaseNotes struct {
	Name          string `json:"name,omitempty"`
	Body          string `json:"body"`
	BodyTruncated bool   `json:"body_truncated,omitempty"`
}

func (c *Client) generateReleaseNotes(ctx context.Context, a *releaseArguments) (*ReleaseNotes, error) {
	const op = "generate release notes"
	payload := map[string]any{"tag_name": a.Tag}
	if a.Target != "" {
		payload["target_commitish"] = a.Target
	}
	if a.PreviousTag != "" {
		payload["previous_tag_name"] = a.PreviousTag
	}
	if a.ConfigurationFilePath != nil {
		payload["configuration_file_path"] = *a.ConfigurationFilePath
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, providerError(op, "the request could not be built")
	}
	var raw struct {
		Name *string `json:"name"`
		Body *string `json:"body"`
	}
	// A calculation, not a change: sent as a read, so a failure never claims an uncertain effect.
	err = c.do(ctx, op, http.MethodPost, c.endpoints.rest+c.repoPath("releases/generate-notes"), body, &raw,
		false, nil, nil)
	if err != nil {
		return nil, actionsFailure(err, releaseNotesPermission)
	}
	text, cut := cleanText(raw.Body, projectionBodyLimit, true)
	if text == nil {
		return nil, invalidResponse(op, false)
	}
	notes := &ReleaseNotes{Body: *text, BodyTruncated: cut}
	if name := nameText(raw.Name); name != nil {
		notes.Name = *name
	}
	return notes, nil
}

func releaseAssetPath(c *Client, id int64) string {
	return c.repoPath("releases/assets/" + strconv.FormatInt(id, 10))
}

// bindReleaseAsset reads the asset below the bound repository, which is the only proof the identifier belongs
// to it; GitHub answers an asset of another repository with not found.
func (c *Client) bindReleaseAsset(ctx context.Context, op string, id int64) (releaseAssetJSON, error) {
	var raw releaseAssetJSON
	if err := c.releaseRead(ctx, op, releaseAssetPath(c, id), &raw); err != nil {
		return releaseAssetJSON{}, err
	}
	if raw.ID != id {
		return releaseAssetJSON{}, invalidResponse(op, false)
	}
	return raw, nil
}

// releaseAssetFailure names a refused asset change. GitHub's text never enters the message; it is matched only
// to recognise a release marked immutable, whatever status carries it.
func releaseAssetFailure(err error) error {
	var failure *provider.Error
	if errors.As(err, &failure) && failure.Original != nil &&
		(failure.Class == provider.ClassProviderError || failure.Class == provider.ClassPermission) {
		for _, entry := range failure.Original.Entries {
			if strings.Contains(strings.ToLower(entry.Message), "immutable") {
				refused := *failure
				refused.Class = provider.ClassPermission
				refused.Message = releaseImmutableMessage
				return &refused
			}
		}
	}
	return actionsFailure(err, releasesChangePermission)
}

func (c *Client) updateReleaseAsset(ctx context.Context, a *releaseArguments) (*ReleaseAsset, error) {
	const op = "update release asset"
	if _, err := c.bindReleaseAsset(ctx, op, a.AssetID); err != nil {
		return nil, err
	}
	payload := map[string]any{}
	if a.Name != nil {
		payload["name"] = *a.Name
	}
	if a.Label != nil {
		payload["label"] = *a.Label
	}
	if a.State != "" {
		payload["state"] = a.State
	}
	var raw releaseAssetJSON
	if err := c.restChange(ctx, op, http.MethodPatch, releaseAssetPath(c, a.AssetID), payload, &raw); err != nil {
		return nil, releaseAssetFailure(err)
	}
	if raw.ID != a.AssetID {
		return nil, invalidResponse(op, true)
	}
	view := raw.view()
	// The download address can grant access without sign-in, so only the REST address stays.
	delete(view.Links, "browser_download_url")
	return &view, nil
}

// DeletedReleaseAsset is the answer of a deleted release asset.
type DeletedReleaseAsset struct {
	ID      int64 `json:"id"`
	Deleted bool  `json:"deleted"`
}

func (c *Client) deleteReleaseAsset(ctx context.Context, id int64) (*DeletedReleaseAsset, error) {
	const op = "delete release asset"
	if _, err := c.bindReleaseAsset(ctx, op, id); err != nil {
		return nil, err
	}
	if err := c.do(ctx, op, http.MethodDelete, c.endpoints.rest+releaseAssetPath(c, id), nil, nil, true, nil,
		nil); err != nil {
		return nil, releaseAssetFailure(err)
	}
	return &DeletedReleaseAsset{ID: id, Deleted: true}, nil
}
