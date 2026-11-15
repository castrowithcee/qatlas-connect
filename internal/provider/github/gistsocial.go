package github

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// The social gist tools comment on a gist, read its revisions, list and create its forks, and star it. They
// follow the targets of the gist tools in gists.go: a connection without targets reaches whatever its token
// reaches, one with targets needs users/LOGIN targets and no repository or project target, and a gist id is
// read first and refused as not found unless its owner is admitted. A fork is created for the account behind
// the token, so, like a new gist, it needs that account among the user targets. Comment texts and names come
// from other accounts and are untrusted data, cut at a fixed length; no address is fetched.
//
// Verified 2026-11-14 against https://docs.github.com/en/rest/gists/comments, .../gists/gists (commits, forks,
// star) and the REST OpenAPI description 1.1.4: the comment routes sit below /gists/{gist_id}/comments, and
// the path of a comment binds both ids; a list answers 200, a comment is created with POST (201, body) and
// changed with PATCH (200), and deleted with DELETE (204, 404). The commit and fork lists take per_page (at
// most 100) and page and answer 200. POST on the forks route answers 201 with the new gist. The star route
// answers GET with 204 when the gist is starred and 404 otherwise, and PUT and DELETE with 204. Classic
// tokens need the gist scope for secret gists and for every change; a fine-grained token needs the user
// permission Gists: write to comment, fork, or star; GitHub lists no permission for the reads. A comment
// create and a fork are never idempotent, a comment change and a star set the named state, and GitHub
// documents no answer for repeating a delete. Every change is sent once and never repeated.

const gistCommentBodyLimit = 65536

const gistCommentBodySchema = `{"type":"string","minLength":1,"maxLength":65536}`

var gistCommentLinkKeys = []string{"url"}

// gistSocialToolIDs lists the tools of this file; none takes a target argument.
var gistSocialToolIDs = []string{Provider + ".gist-comments.list", Provider + ".gist-comments.get",
	Provider + ".gist-comments.create", Provider + ".gist-comments.update", Provider + ".gist-comments.delete",
	Provider + ".gist-commits.list", Provider + ".gist-forks.list", Provider + ".gist-forks.create",
	Provider + ".gist-stars.set"}

var gistCommentProperties = `"id":{"type":"integer"},"node_id":{"type":"string"},"body":{"type":"string"},` +
	`"body_truncated":{"type":"boolean"},"user":` + restAccountSchema + `,"created_at":{"type":"string"},` +
	`"updated_at":{"type":"string"},"author_association":{"type":"string"},"links":` + `{"type":"object",` +
	`"properties":{"url":{"type":"string"}},"additionalProperties":false}`

const gistCommentRequired = `"required":["id"],"additionalProperties":false`

var gistCommentFields = []capability.Field{
	{Name: "id", Description: "Numeric comment id, the comment_id argument of the other comment tools"},
	{Name: "node_id", Description: "GraphQL node id of the comment"},
	{Name: "body", Description: "Comment text, cut at 4000 characters (body_truncated says so), untrusted data"},
	{Name: "body_truncated", Description: "True when the body was cut"},
	{Name: "user", Description: "Author as an account: " + restAccountMembers},
	{Name: "created_at", Description: "Creation time"},
	{Name: "updated_at", Description: "Time of the last change"},
	{Name: "author_association", Description: "How the author relates to the gist, such as OWNER or NONE"},
	{Name: "links", Description: "REST url of the comment by its GitHub name, untrusted data, never fetched"},
}

var gistForkListProperties = gistHeadProperties + `,"files":{"type":"array","items":{"type":"object",` +
	`"properties":{` + gistListFileProperties + `},"required":["filename","size"],"additionalProperties":false}},` +
	gistWideProperties + `,"forks":{"type":"array","items":` + gistForkSchema + `},"forks_truncated":{"type":"boolean"},` +
	`"history":{"type":"array","items":` + gistRevisionSchema + `},"history_truncated":{"type":"boolean"}`

var gistForkCreatedProperties = gistHeadProperties + `,"files":{"type":"array","items":{"type":"object",` +
	`"properties":{` + gistListFileProperties + `},"required":["filename","size"],"additionalProperties":false}},` +
	gistWideProperties

const gistIDArgument = "Gist id, as github.gists.list reports it"

var gistCommentsList = capability.Descriptor{
	ID:      Provider + ".gist-comments.list",
	Version: 1,
	Title:   "List GitHub gist comments",
	Description: "List one bounded batch of the comments on one gist, oldest first; comment texts are cut at " +
		"4000 characters; with targets a gist of a user no user target names is refused",
	Tags:         []string{"github", "gists", "comments", "list"},
	Risk:         readRisk,
	Provider:     Provider,
	InputSchema:  inputSchema(`"gist_id":`+gistIDSchema+`,`+pagingKeys, "gist_id"),
	OutputSchema: listOutput("comments", gistCommentProperties, gistCommentRequired),
	Arguments: append([]capability.Argument{
		{Name: "gist_id", Description: gistIDArgument, Required: true},
	}, pagingArguments...),
	Fields: append([]capability.Field{
		{Name: "comments", Description: "Comments of the gist with the members of GitHub's gist comment below"},
	}, append(slices.Clone(gistCommentFields), pagingFields...)...),
	Examples: []capability.Example{{
		Description: "List the comments of a gist",
		Arguments:   json.RawMessage(`{"gist_id":"aa5a315d61ae9438b18d"}`),
	}},
}

var gistCommentsGet = capability.Descriptor{
	ID:      Provider + ".gist-comments.get",
	Version: 1,
	Title:   "Get a GitHub gist comment",
	Description: "Read one comment of a gist by its id, with its text cut at 4000 characters; with targets a " +
		"gist of a user no user target names is refused",
	Tags:        []string{"github", "gists", "comments", "get"},
	Risk:        readRisk,
	Provider:    Provider,
	InputSchema: inputSchema(`"gist_id":`+gistIDSchema+`,"comment_id":`+actionsIDSchema, "gist_id", "comment_id"),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{` + gistCommentProperties + `},` +
		gistCommentRequired + `}`),
	Arguments: []capability.Argument{
		{Name: "gist_id", Description: gistIDArgument, Required: true},
		{Name: "comment_id", Description: "Numeric id of the comment, as github.gist-comments.list reports it", Required: true},
	},
	Fields: gistCommentFields,
	Examples: []capability.Example{{
		Description: "Read one comment of a gist",
		Arguments:   json.RawMessage(`{"gist_id":"aa5a315d61ae9438b18d","comment_id":1}`),
	}},
}

var gistCommentsCreate = capability.Descriptor{
	ID:      Provider + ".gist-comments.create",
	Version: 1,
	Title:   "Create a GitHub gist comment",
	Description: "Comment on one gist as the account behind the connection's token; the gist is read first when " +
		"the connection has targets and a gist of a user no user target names is refused",
	Tags:        []string{"github", "gists", "comments", "create"},
	Risk:        changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent),
	Provider:    Provider,
	InputSchema: inputSchema(`"gist_id":`+gistIDSchema+`,"body":`+gistCommentBodySchema, "gist_id", "body"),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{` + gistCommentProperties + `},` +
		gistCommentRequired + `}`),
	Arguments: []capability.Argument{
		{Name: "gist_id", Description: gistIDArgument, Required: true},
		{Name: "body", Description: "Comment text, at most 65536 characters", Required: true},
	},
	Fields: gistCommentFields,
	Examples: []capability.Example{{
		Description: "Comment on a gist",
		Arguments:   json.RawMessage(`{"gist_id":"aa5a315d61ae9438b18d","body":"Thanks for sharing."}`),
	}},
}

var gistCommentsUpdate = capability.Descriptor{
	ID:      Provider + ".gist-comments.update",
	Version: 1,
	Title:   "Update a GitHub gist comment",
	Description: "Replace the text of one comment of a gist; the gist is read first when the connection has " +
		"targets and a gist of a user no user target names is refused",
	Tags:     []string{"github", "gists", "comments", "update"},
	Risk:     changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
	Provider: Provider,
	InputSchema: inputSchema(`"gist_id":`+gistIDSchema+`,"comment_id":`+actionsIDSchema+`,"body":`+
		gistCommentBodySchema, "gist_id", "comment_id", "body"),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{` + gistCommentProperties + `},` +
		gistCommentRequired + `}`),
	Arguments: []capability.Argument{
		{Name: "gist_id", Description: gistIDArgument, Required: true},
		{Name: "comment_id", Description: "Numeric id of the comment, as github.gist-comments.list reports it", Required: true},
		{Name: "body", Description: "New comment text, at most 65536 characters", Required: true},
	},
	Fields: gistCommentFields,
	Examples: []capability.Example{{
		Description: "Change a comment of a gist",
		Arguments:   json.RawMessage(`{"gist_id":"aa5a315d61ae9438b18d","comment_id":1,"body":"Updated."}`),
	}},
}

var gistCommentsDelete = capability.Descriptor{
	ID:      Provider + ".gist-comments.delete",
	Version: 1,
	Title:   "Delete a GitHub gist comment",
	Description: "Delete one comment of a gist permanently; offered only where a connection's tools list names " +
		"it; the gist is read first when the connection has targets and a gist of a user no user target names " +
		"is refused",
	Tags:                  []string{"github", "gists", "comments", "delete"},
	Risk:                  guardedRisk(capability.EffectDelete, capability.IdempotencyUnknown, gistSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema:           inputSchema(`"gist_id":`+gistIDSchema+`,"comment_id":`+actionsIDSchema, "gist_id", "comment_id"),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"gist_id":{"type":"string"},` +
		`"comment_id":{"type":"integer"},"deleted":{"type":"boolean"}},"required":["gist_id","comment_id","deleted"],` +
		`"additionalProperties":false}`),
	Arguments: []capability.Argument{
		{Name: "gist_id", Description: gistIDArgument, Required: true},
		{Name: "comment_id", Description: "Numeric id of the comment, as github.gist-comments.list reports it", Required: true},
	},
	Fields: []capability.Field{
		{Name: "gist_id", Description: "The gist the comment belonged to"},
		{Name: "comment_id", Description: "The comment that was deleted"},
		{Name: "deleted", Description: "True once GitHub accepted the delete"},
	},
	Examples: []capability.Example{{
		Description: "Delete a comment of a gist",
		Arguments:   json.RawMessage(`{"gist_id":"aa5a315d61ae9438b18d","comment_id":1}`),
	}},
}

var gistCommitsList = capability.Descriptor{
	ID:      Provider + ".gist-commits.list",
	Version: 1,
	Title:   "List GitHub gist commits",
	Description: "List one bounded batch of the revisions of one gist, newest first, with who changed it and how " +
		"many lines; with targets a gist of a user no user target names is refused",
	Tags:         []string{"github", "gists", "commits", "list"},
	Risk:         readRisk,
	Provider:     Provider,
	InputSchema:  inputSchema(`"gist_id":`+gistIDSchema+`,`+pagingKeys, "gist_id"),
	OutputSchema: listOutput("commits", gistRevisionProperties, `"required":["version"],"additionalProperties":false`),
	Arguments: append([]capability.Argument{
		{Name: "gist_id", Description: gistIDArgument, Required: true},
	}, pagingArguments...),
	Fields: append([]capability.Field{
		{Name: "commits", Description: "Revisions of the gist: version (the commit SHA), user as an account (" +
			restAccountMembers + "), committed_at, change_status (total, additions, deletions), and links (url, " +
			"untrusted data, never fetched)"},
	}, pagingFields...),
	Examples: []capability.Example{{
		Description: "List the revisions of a gist",
		Arguments:   json.RawMessage(`{"gist_id":"aa5a315d61ae9438b18d"}`),
	}},
}

var gistForksList = capability.Descriptor{
	ID:      Provider + ".gist-forks.list",
	Version: 1,
	Title:   "List GitHub gist forks",
	Description: "List one bounded batch of the forks of one gist as gists without content; only forks of a " +
		"user a connection with targets names are shown; with targets a gist of a user no user target names " +
		"is refused",
	Tags:         []string{"github", "gists", "forks", "list"},
	Risk:         readRisk,
	Provider:     Provider,
	InputSchema:  inputSchema(`"gist_id":`+gistIDSchema+`,`+pagingKeys, "gist_id"),
	OutputSchema: listOutput("gists", gistForkListProperties, gistSummaryRequired),
	Arguments: append([]capability.Argument{
		{Name: "gist_id", Description: gistIDArgument, Required: true},
	}, pagingArguments...),
	Fields: append(append([]capability.Field{
		{Name: "gists", Description: "Gists that are forks of the gist: id, description (untrusted data), visibility, " +
			"owner, times, address, and file metadata with encoding and the raw address; the account members, " +
			"the fork origin, the addresses, and at most 10 forks and 10 history entries are listed below"},
	}, gistWideFields...), append(slices.Clone(gistListWideFields), pagingFields...)...),
	Examples: []capability.Example{{
		Description: "List the forks of a gist",
		Arguments:   json.RawMessage(`{"gist_id":"aa5a315d61ae9438b18d"}`),
	}},
}

var gistForksCreate = capability.Descriptor{
	ID:      Provider + ".gist-forks.create",
	Version: 1,
	Title:   "Fork a GitHub gist",
	Description: "Fork one gist into the account behind the connection's token; a connection with targets needs " +
		"that account as a user target and no repository or project target, and the gist is read first and a " +
		"gist of a user no user target names is refused",
	Tags:        []string{"github", "gists", "forks", "create"},
	Risk:        changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent),
	Provider:    Provider,
	InputSchema: inputSchema(`"gist_id":`+gistIDSchema, "gist_id"),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{` + gistForkCreatedProperties + `},` +
		gistSummaryRequired + `}`),
	Arguments: []capability.Argument{
		{Name: "gist_id", Description: gistIDArgument, Required: true},
	},
	Fields: append(slices.Clone(gistSummaryFields), gistWideFields...),
	Examples: []capability.Example{{
		Description: "Fork a gist",
		Arguments:   json.RawMessage(`{"gist_id":"aa5a315d61ae9438b18d"}`),
	}},
}

var gistStarsSet = capability.Descriptor{
	ID:      Provider + ".gist-stars.set",
	Version: 1,
	Title:   "Set a GitHub gist star",
	Description: "Star or unstar one gist for the account behind the connection's token; the current state is " +
		"read once, and a gist already in the requested state is reported without a further request; with " +
		"targets a gist of a user no user target names is refused",
	Tags:        []string{"github", "gists", "stars", "set"},
	Risk:        changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
	Provider:    Provider,
	InputSchema: inputSchema(`"gist_id":`+gistIDSchema+`,"starred":{"type":"boolean"}`, "gist_id", "starred"),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"gist_id":{"type":"string"},` +
		`"starred":{"type":"boolean"},"changed":{"type":"boolean"}},"required":["gist_id","starred","changed"],` +
		`"additionalProperties":false}`),
	Arguments: []capability.Argument{
		{Name: "gist_id", Description: gistIDArgument, Required: true},
		{Name: "starred", Description: "True stars the gist, false removes the star", Required: true},
	},
	Fields: []capability.Field{
		{Name: "gist_id", Description: "The gist that was addressed"},
		{Name: "starred", Description: "Whether the gist is starred now"},
		{Name: "changed", Description: "True when this call changed the star; false when it was already in the requested state"},
	},
	Examples: []capability.Example{{
		Description: "Star a gist",
		Arguments:   json.RawMessage(`{"gist_id":"aa5a315d61ae9438b18d","starred":true}`),
	}},
}

var gistRevisionProperties = `"version":{"type":"string"},"user":` + restAccountSchema + `,` +
	`"committed_at":{"type":"string"},"change_status":` + objectSchema(`"total":{"type":"integer"},`+
	`"additions":{"type":"integer"},"deletions":{"type":"integer"}`) + `,"links":` + linksSchema(gistEntryLinkKeys)

func gistSocialOperations() []capability.Operation {
	return []capability.Operation{
		{Descriptor: gistCommentsList, Handler: capability.Handler(invokeGistCommentsList)},
		{Descriptor: gistCommentsGet, Handler: capability.Handler(invokeGistCommentsGet)},
		{Descriptor: gistCommentsCreate, Handler: capability.Handler(invokeGistCommentsCreate)},
		{Descriptor: gistCommentsUpdate, Handler: capability.Handler(invokeGistCommentsUpdate)},
		{Descriptor: gistCommentsDelete, Handler: capability.Handler(invokeGistCommentsDelete)},
		{Descriptor: gistCommitsList, Handler: capability.Handler(invokeGistCommitsList)},
		{Descriptor: gistForksList, Handler: capability.Handler(invokeGistForksList)},
		{Descriptor: gistForksCreate, Handler: capability.Handler(invokeGistForksCreate)},
		{Descriptor: gistStarsSet, Handler: capability.Handler(invokeGistStarsSet)},
	}
}

// GistComment is one comment of a gist.
type GistComment struct {
	ID                *int64       `json:"id,omitempty"`
	NodeID            *string      `json:"node_id,omitempty"`
	Body              *string      `json:"body,omitempty"`
	BodyTruncated     bool         `json:"body_truncated,omitempty"`
	User              *RESTAccount `json:"user,omitempty"`
	CreatedAt         *string      `json:"created_at,omitempty"`
	UpdatedAt         *string      `json:"updated_at,omitempty"`
	AuthorAssociation *string      `json:"author_association,omitempty"`
	Links             Links        `json:"links,omitempty"`
}

// GistCommentList is one batch of the comments of a gist.
type GistCommentList struct {
	Comments   []GistComment `json:"comments"`
	NextCursor string        `json:"next_cursor,omitempty"`
	HasMore    bool          `json:"has_more"`
}

// GistCommentDeleted is the answer of github.gist-comments.delete.
type GistCommentDeleted struct {
	GistID    string `json:"gist_id"`
	CommentID int64  `json:"comment_id"`
	Deleted   bool   `json:"deleted"`
}

// GistCommitList is one batch of the revisions of a gist.
type GistCommitList struct {
	Commits    []GistRevision `json:"commits"`
	NextCursor string         `json:"next_cursor,omitempty"`
	HasMore    bool           `json:"has_more"`
}

// GistForkList is one batch of the forks of a gist.
type GistForkList struct {
	Gists      []GistDetail `json:"gists"`
	NextCursor string       `json:"next_cursor,omitempty"`
	HasMore    bool         `json:"has_more"`
}

// GistStarChange is the answer of github.gist-stars.set.
type GistStarChange struct {
	GistID  string `json:"gist_id"`
	Starred bool   `json:"starred"`
	Changed bool   `json:"changed"`
}

type gistCommentRaw struct {
	ID                *int64      `json:"id"`
	NodeID            *string     `json:"node_id"`
	Body              *string     `json:"body"`
	User              *accountRaw `json:"user"`
	CreatedAt         *string     `json:"created_at"`
	UpdatedAt         *string     `json:"updated_at"`
	AuthorAssociation *string     `json:"author_association"`
	links             Links
}

func (r *gistCommentRaw) UnmarshalJSON(data []byte) (err error) {
	type plain gistCommentRaw
	if err = json.Unmarshal(data, (*plain)(r)); err != nil {
		return err
	}
	r.links, err = readLinks(data, gistCommentLinkKeys)
	return err
}

func (r gistCommentRaw) project(op string) (GistComment, error) {
	if r.ID == nil {
		return GistComment{}, invalidEntry(op, "a gist comment")
	}
	body, cut := cleanText(r.Body, gistCommentShown, true)
	return GistComment{ID: r.ID, NodeID: nameText(r.NodeID), Body: body, BodyTruncated: cut, User: r.User.project(),
		CreatedAt: shortText(r.CreatedAt), UpdatedAt: shortText(r.UpdatedAt),
		AuthorAssociation: shortText(r.AuthorAssociation), Links: r.links}, nil
}

// gistCommentShown is the length a comment text is cut at in the output.
const gistCommentShown = discussionCommentBodyLimit

// openGist checks the gist id and the connection's targets before a credential is resolved, opens the client,
// and, when the targets narrow the owners, refuses a gist of an owner they do not admit as not found.
func openGist(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	op, id string) (*Client, error) {
	if err := checkGistID(id); err != nil {
		return nil, err
	}
	if _, err := requireGistTargets(resolved); err != nil {
		return nil, err
	}
	client, err := Open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if !client.allowed.open() {
		if _, err := client.gist(ctx, op, id); err != nil {
			return nil, err
		}
	}
	return client, nil
}

func gistPath(id string, tail ...string) string {
	return "/gists/" + url.PathEscape(id) + strings.Join(tail, "")
}

type gistPageArguments struct {
	GistID string `json:"gist_id"`
	Limit  int    `json:"limit"`
	Cursor string `json:"cursor"`
}

// pageQuery reads the paging of a gist sub-list and returns its query and binding.
func (a *gistPageArguments) pageQuery(kind string) (url.Values, []byte, int, int, error) {
	limit, err := normalizeLimit(a.Limit)
	if err != nil {
		return nil, nil, 0, 0, err
	}
	if err := checkGistID(a.GistID); err != nil {
		return nil, nil, 0, 0, err
	}
	binding := fingerprint("gists", kind, strings.ToLower(a.GistID))
	page, perPage, err := pageOf(binding, a.Cursor, limit)
	if err != nil {
		return nil, nil, 0, 0, err
	}
	return url.Values{"per_page": {strconv.Itoa(perPage)}, "page": {strconv.Itoa(page)}}, binding, page, perPage, nil
}

func invokeGistCommentsList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list gist comments"
	var a gistPageArguments
	if err := readArguments(raw, op, &a); err != nil {
		return nil, err
	}
	query, binding, page, perPage, err := a.pageQuery("comments")
	if err != nil {
		return nil, err
	}
	client, err := openGist(ctx, resolved, secrets, red, op, a.GistID)
	if err != nil {
		return nil, err
	}
	var rows []gistCommentRaw
	hasNext, err := client.restPage(ctx, op, gistPath(a.GistID, "/comments"), query, &rows)
	if err != nil {
		return nil, actionsFailure(err, gistsReadPermission)
	}
	result := &GistCommentList{Comments: make([]GistComment, 0, len(rows))}
	for _, row := range rows {
		comment, err := row.project(op)
		if err != nil {
			return nil, err
		}
		result.Comments = append(result.Comments, comment)
	}
	result.HasMore, result.NextCursor = morePage(binding, page, perPage, hasNext)
	return result, nil
}

// belongsToGist reports whether a comment's own REST address, when GitHub sent one, names the gist and the
// comment that were asked for. The path of the route binds both, so this only catches a stray answer.
func belongsToGist(links Links, gistID string, commentID int64) bool {
	address, ok := links["url"]
	if !ok {
		return true
	}
	parsed, err := url.Parse(address)
	if err != nil {
		return false
	}
	want := "/gists/" + gistID + "/comments/" + strconv.FormatInt(commentID, 10)
	path := strings.TrimRight(parsed.Path, "/")
	return len(path) >= len(want) && strings.EqualFold(path[len(path)-len(want):], want)
}

func invokeGistCommentsGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "get gist comment"
	var a struct {
		GistID    string `json:"gist_id"`
		CommentID int64  `json:"comment_id"`
	}
	if err := readArguments(raw, op, &a); err != nil {
		return nil, err
	}
	if a.CommentID < 1 {
		return nil, invalidRequest("comment_id must be a positive number")
	}
	client, err := openGist(ctx, resolved, secrets, red, op, a.GistID)
	if err != nil {
		return nil, err
	}
	var row gistCommentRaw
	if err := client.rest(ctx, op, gistPath(a.GistID, "/comments/", strconv.FormatInt(a.CommentID, 10)), &row); err != nil {
		return nil, actionsFailure(err, gistsReadPermission)
	}
	if !belongsToGist(row.links, a.GistID, a.CommentID) {
		return nil, notFound(op, subject{what: "this gist"})
	}
	return row.project(op)
}

func checkGistCommentBody(body string) error {
	if body == "" || utf8.RuneCountInString(body) > gistCommentBodyLimit {
		return invalidRequest("body must hold 1 to 65536 characters")
	}
	return nil
}

func invokeGistCommentsCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "create gist comment"
	var a struct {
		GistID string `json:"gist_id"`
		Body   string `json:"body"`
	}
	if err := readArguments(raw, op, &a); err != nil {
		return nil, err
	}
	if err := checkGistCommentBody(a.Body); err != nil {
		return nil, err
	}
	client, err := openGist(ctx, resolved, secrets, red, op, a.GistID)
	if err != nil {
		return nil, err
	}
	var created gistCommentRaw
	if err := client.restChange(ctx, op, http.MethodPost, gistPath(a.GistID, "/comments"),
		map[string]string{"body": a.Body}, &created); err != nil {
		return nil, actionsFailure(err, gistsChangePermission)
	}
	return created.project(op)
}

func invokeGistCommentsUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "update gist comment"
	var a struct {
		GistID    string `json:"gist_id"`
		CommentID int64  `json:"comment_id"`
		Body      string `json:"body"`
	}
	if err := readArguments(raw, op, &a); err != nil {
		return nil, err
	}
	if a.CommentID < 1 {
		return nil, invalidRequest("comment_id must be a positive number")
	}
	if err := checkGistCommentBody(a.Body); err != nil {
		return nil, err
	}
	client, err := openGist(ctx, resolved, secrets, red, op, a.GistID)
	if err != nil {
		return nil, err
	}
	var updated gistCommentRaw
	if err := client.restChange(ctx, op, http.MethodPatch,
		gistPath(a.GistID, "/comments/", strconv.FormatInt(a.CommentID, 10)), map[string]string{"body": a.Body},
		&updated); err != nil {
		return nil, actionsFailure(err, gistsChangePermission)
	}
	return updated.project(op)
}

func invokeGistCommentsDelete(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "delete gist comment"
	var a struct {
		GistID    string `json:"gist_id"`
		CommentID int64  `json:"comment_id"`
	}
	if err := readArguments(raw, op, &a); err != nil {
		return nil, err
	}
	if a.CommentID < 1 {
		return nil, invalidRequest("comment_id must be a positive number")
	}
	client, err := openGist(ctx, resolved, secrets, red, op, a.GistID)
	if err != nil {
		return nil, err
	}
	if err := client.restChange(ctx, op, http.MethodDelete,
		gistPath(a.GistID, "/comments/", strconv.FormatInt(a.CommentID, 10)), struct{}{}, nil); err != nil {
		return nil, actionsFailure(err, gistsChangePermission)
	}
	return &GistCommentDeleted{GistID: a.GistID, CommentID: a.CommentID, Deleted: true}, nil
}

func invokeGistCommitsList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list gist commits"
	var a gistPageArguments
	if err := readArguments(raw, op, &a); err != nil {
		return nil, err
	}
	query, binding, page, perPage, err := a.pageQuery("commits")
	if err != nil {
		return nil, err
	}
	client, err := openGist(ctx, resolved, secrets, red, op, a.GistID)
	if err != nil {
		return nil, err
	}
	var rows []gistHistoryRaw
	hasNext, err := client.restPage(ctx, op, gistPath(a.GistID, "/commits"), query, &rows)
	if err != nil {
		return nil, actionsFailure(err, gistsReadPermission)
	}
	result := &GistCommitList{Commits: make([]GistRevision, 0, len(rows))}
	for _, h := range rows {
		result.Commits = append(result.Commits, h.revision())
	}
	result.HasMore, result.NextCursor = morePage(binding, page, perPage, hasNext)
	return result, nil
}

// revision projects one history entry of a gist.
func (h gistHistoryRaw) revision() GistRevision {
	r := GistRevision{Version: shortText(h.Version), User: h.User.project(), CommittedAt: shortText(h.CommittedAt),
		Links: gqlLinks(gistEntryLinkKeys, h.URL)}
	if c := h.ChangeStatus; c != nil {
		r.ChangeStatus = &GistChangeStatus{Total: c.Total, Additions: c.Additions, Deletions: c.Deletions}
	}
	return r
}

func invokeGistForksList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list gist forks"
	var a gistPageArguments
	if err := readArguments(raw, op, &a); err != nil {
		return nil, err
	}
	query, binding, page, perPage, err := a.pageQuery("forks")
	if err != nil {
		return nil, err
	}
	client, err := openGist(ctx, resolved, secrets, red, op, a.GistID)
	if err != nil {
		return nil, err
	}
	var rows []gistJSON
	hasNext, err := client.restPage(ctx, op, gistPath(a.GistID, "/forks"), query, &rows)
	if err != nil {
		return nil, actionsFailure(err, gistsReadPermission)
	}
	result := &GistForkList{Gists: make([]GistDetail, 0, len(rows))}
	for _, row := range rows {
		view, err := row.detail(op, false, true)
		if err != nil {
			return nil, err
		}
		if !client.allowed.admitsGistOwner(row.owner()) {
			continue
		}
		result.Gists = append(result.Gists, view)
	}
	result.HasMore, result.NextCursor = morePage(binding, page, perPage, hasNext)
	return result, nil
}

func invokeGistForksCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "fork gist"
	var a struct {
		GistID string `json:"gist_id"`
	}
	if err := readArguments(raw, op, &a); err != nil {
		return nil, err
	}
	if err := checkGistID(a.GistID); err != nil {
		return nil, err
	}
	if _, err := requireGistTargets(resolved); err != nil {
		return nil, err
	}
	client, err := Open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := client.requireOwnAccount(ctx, op); err != nil {
		return nil, err
	}
	if !client.allowed.open() {
		if _, err := client.gist(ctx, op, a.GistID); err != nil {
			return nil, err
		}
	}
	var created gistJSON
	if err := client.restChange(ctx, op, http.MethodPost, gistPath(a.GistID, "/forks"), struct{}{},
		&created); err != nil {
		return nil, actionsFailure(err, gistsChangePermission)
	}
	return created.detail(op, false, false)
}

func invokeGistStarsSet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "set gist star"
	var a struct {
		GistID  string `json:"gist_id"`
		Starred *bool  `json:"starred"`
	}
	if err := readArguments(raw, op, &a); err != nil {
		return nil, err
	}
	if a.Starred == nil {
		return nil, invalidRequest("starred must be true or false")
	}
	client, err := openGist(ctx, resolved, secrets, red, op, a.GistID)
	if err != nil {
		return nil, err
	}
	path := gistPath(a.GistID, "/star")
	starred := true
	if err := client.rest(ctx, op, path, nil); err != nil {
		var failure *provider.Error
		if !errors.As(err, &failure) || failure.Class != provider.ClassNotFound {
			return nil, actionsFailure(err, gistsReadPermission)
		}
		starred = false
	}
	if starred == *a.Starred {
		return &GistStarChange{GistID: a.GistID, Starred: starred, Changed: false}, nil
	}
	method := http.MethodPut
	if !*a.Starred {
		method = http.MethodDelete
	}
	if err := client.restChange(ctx, op, method, path, struct{}{}, nil); err != nil {
		return nil, actionsFailure(err, gistsChangePermission)
	}
	return &GistStarChange{GistID: a.GistID, Starred: *a.Starred, Changed: true}, nil
}
