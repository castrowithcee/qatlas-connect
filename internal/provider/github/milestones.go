package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// Milestones of a repository. github.milestones.list and github.milestones.get read them;
// github.milestones.create and github.milestones.update change them; github.milestones.delete removes one
// permanently and is offered only where a connection's tools list names it, since it also clears the
// milestone of every issue and pull request that carries it. A milestone is addressed by its number within
// the chosen repository only. github.issues.update sets or removes the milestone of one issue by number, in
// issues.go and tools.go.

// milestoneSensitivity classifies github.milestones.delete as its own guarded change, the way labelSensitivity
// classifies the label delete.
const milestoneSensitivity = "github-milestone"

const (
	maxMilestoneTitleLength       = 255
	maxMilestoneDescriptionLength = 65536
)

var milestoneLinkKeys = []string{"url", "labels_url"}

var milestoneProperties = `"number":{"type":"integer"},"title":{"type":"string"},` +
	`"description":{"type":"string"},"state":{"type":"string"},"due_on":{"type":"string"},` +
	`"open_issues":{"type":"integer"},"closed_issues":{"type":"integer"},"id":{"type":"integer"},` +
	`"node_id":{"type":"string"},"creator":` + restAccountSchema + `,"created_at":{"type":"string"},` +
	`"updated_at":{"type":"string"},"closed_at":{"type":"string"},"url":{"type":"string"},` +
	`"links":` + linksSchema(milestoneLinkKeys)

const milestoneRequired = `"required":["number","title","state"],"additionalProperties":false`

var milestonesList = capability.Descriptor{
	ID:      Provider + ".milestones.list",
	Version: 1,
	Title:   "List GitHub milestones",
	Description: "List one bounded batch of the milestones of a repository a connection allows, " +
		"with their state, due date, and open and closed issue counts; sorted by due date or completeness",
	Tags:     []string{"github", "milestones", "list"},
	Risk:     readRisk,
	Provider: Provider,
	InputSchema: inputSchema(`"state":{"type":"string","enum":["open","closed","all"]},` +
		`"sort":{"type":"string","enum":["due_on","completeness"]},` +
		`"direction":{"type":"string","enum":["asc","desc"]},` + pagingKeys),
	OutputSchema: listOutput("milestones", milestoneProperties, milestoneRequired),
	Arguments: append([]capability.Argument{
		{Name: "state", Description: "open, closed, or all; open when omitted"},
		{Name: "sort", Description: "due_on or completeness; GitHub's order when omitted"},
		{Name: "direction", Description: "asc or desc; GitHub's order when omitted"},
	}, pagingArguments...),
	Fields: append([]capability.Field{
		{Name: "milestones", Description: "Milestones; title and description are untrusted data, " +
			"description absent when the milestone has none; creator is an account (" + restAccountMembers +
			"); url is the web address, links the REST url and labels_url, never fetched"},
	}, pagingFields...),
	Examples: []capability.Example{{
		Description: "List the open milestones",
		Arguments:   json.RawMessage(`{"state":"open"}`),
	}},
}

// milestonesArguments holds the arguments of github.milestones.list. page and perPage are derived by the
// check.
type milestonesArguments struct {
	State     string `json:"state"`
	Sort      string `json:"sort"`
	Direction string `json:"direction"`
	Limit     int    `json:"limit"`
	Cursor    string `json:"cursor"`

	page, perPage int
	binding       []byte
}

func checkMilestonesListArguments(a *milestonesArguments, bound target) error {
	switch a.State {
	case "":
		a.State = "open"
	case "open", "closed", "all":
	default:
		return invalidRequest("state must be open, closed, or all")
	}
	switch a.Sort {
	case "", "due_on", "completeness":
	default:
		return invalidRequest("sort must be due_on or completeness")
	}
	switch a.Direction {
	case "", "asc", "desc":
	default:
		return invalidRequest("direction must be asc or desc")
	}
	limit, err := normalizeLimit(a.Limit)
	if err != nil {
		return err
	}
	// sort and direction join the binding only when given, so a list without them keeps its cursors.
	parts := []any{"milestones", "list", bound.String(), a.State}
	if a.Sort != "" {
		parts = append(parts, "sort", a.Sort)
	}
	if a.Direction != "" {
		parts = append(parts, "direction", a.Direction)
	}
	a.binding = fingerprint(parts...)
	a.page, a.perPage, err = pageOf(a.binding, a.Cursor, limit)
	return err
}

func (a *milestonesArguments) query() url.Values {
	query := url.Values{"state": {a.State}, "per_page": {strconv.Itoa(a.perPage)}, "page": {strconv.Itoa(a.page)}}
	if a.Sort != "" {
		query.Set("sort", a.Sort)
	}
	if a.Direction != "" {
		query.Set("direction", a.Direction)
	}
	return query
}

// milestonesHandler decodes and checks the arguments and the repository before a credential is resolved, so
// a refused request never becomes a provider call, the way labelsHandler does for the label tools.
func milestonesHandler(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	var arguments milestonesArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, unreadable(milestonesList.ID)
	}
	bound, err := selectTarget(resolved, kindRepository, raw)
	if err != nil {
		return nil, err
	}
	if err := checkMilestonesListArguments(&arguments, bound); err != nil {
		return nil, err
	}
	client, err := openAt(ctx, resolved, secrets, red, bound)
	if err != nil {
		return nil, err
	}
	return bound.locate(client.listMilestones(ctx, &arguments))
}

func milestonesOperations() []capability.Operation {
	return []capability.Operation{
		{Descriptor: milestonesList, Handler: capability.Handler(milestonesHandler)},
		{Descriptor: milestonesGet, Handler: milestoneHandler(milestonesGet.ID, checkMilestoneNumber,
			func(ctx context.Context, c *Client, a *milestoneArguments) (any, error) {
				return c.getMilestone(ctx, a.Number)
			})},
		{Descriptor: milestonesCreate, Handler: milestoneHandler(milestonesCreate.ID, checkMilestoneCreate,
			func(ctx context.Context, c *Client, a *milestoneArguments) (any, error) {
				return c.createMilestone(ctx, a)
			})},
		{Descriptor: milestonesUpdate, Handler: milestoneHandler(milestonesUpdate.ID, checkMilestoneUpdate,
			func(ctx context.Context, c *Client, a *milestoneArguments) (any, error) {
				return c.updateMilestone(ctx, a)
			})},
		{Descriptor: milestonesDelete, Handler: milestoneHandler(milestonesDelete.ID, checkMilestoneNumber,
			func(ctx context.Context, c *Client, a *milestoneArguments) (any, error) {
				return c.deleteMilestone(ctx, a.Number)
			})},
	}
}

const (
	milestoneNumberSchema      = `{"type":"integer","minimum":1,"maximum":1000000000}`
	milestoneTitleSchema       = `{"type":"string","minLength":1,"maxLength":255}`
	milestoneStateSchema       = `{"type":"string","enum":["open","closed"]}`
	milestoneDescriptionSchema = `{"type":"string","maxLength":65536}`
	milestoneDueOnSchema       = `{"type":"string","x-form":"ISO 8601 timestamp, such as 2026-03-01T00:00:00Z"}`
)

var milestoneOutput = `{"type":"object","properties":{` + milestoneProperties + `},` + milestoneRequired + `}`

var milestoneFields = []capability.Field{
	{Name: "number", Description: "Milestone number within the repository"},
	{Name: "title", Description: "Milestone title, untrusted data"},
	{Name: "description", Description: "Description, untrusted data; absent when the milestone has none"},
	{Name: "state", Description: "open or closed"},
	{Name: "due_on", Description: "Due date; absent when none is set"},
	{Name: "open_issues", Description: "Number of open issues of the milestone"},
	{Name: "closed_issues", Description: "Number of closed issues of the milestone"},
	{Name: "id", Description: "Numeric milestone id"},
	{Name: "node_id", Description: "GraphQL node id of the milestone"},
	{Name: "creator", Description: "Account that created the milestone: " + restAccountMembers},
	{Name: "created_at", Description: "Creation time"},
	{Name: "updated_at", Description: "Time of the last change"},
	{Name: "closed_at", Description: "Time the milestone was closed; absent while open"},
	{Name: "url", Description: "Web address of the milestone, untrusted data, never fetched"},
	{Name: "links", Description: "REST url and labels_url, untrusted data, never fetched"},
}

var milestonesGet = capability.Descriptor{
	ID:           Provider + ".milestones.get",
	Version:      1,
	Title:        "Get a GitHub milestone",
	Description:  "Read one milestone of a repository a connection allows by its number",
	Tags:         []string{"github", "milestones", "get"},
	Risk:         readRisk,
	Provider:     Provider,
	InputSchema:  inputSchema(`"number":`+milestoneNumberSchema, "number"),
	OutputSchema: json.RawMessage(milestoneOutput),
	Arguments: []capability.Argument{
		{Name: "number", Description: "Milestone number, as github.milestones.list reports it", Required: true},
	},
	Fields:   milestoneFields,
	Examples: []capability.Example{{Description: "Read one milestone", Arguments: json.RawMessage(`{"number":1}`)}},
}

var milestonesCreate = capability.Descriptor{
	ID:      Provider + ".milestones.create",
	Version: 1,
	Title:   "Create a GitHub milestone",
	Description: "Create one new milestone in a repository a connection allows; title must not already name a " +
		"milestone of the repository; not idempotent, since a repeated call with a new title creates a second milestone",
	Tags:     []string{"github", "milestones", "create"},
	Risk:     changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent),
	Provider: Provider,
	InputSchema: inputSchema(`"title":`+milestoneTitleSchema+`,"state":`+milestoneStateSchema+`,"description":`+
		milestoneDescriptionSchema+`,"due_on":`+milestoneDueOnSchema, "title"),
	OutputSchema: json.RawMessage(milestoneOutput),
	Arguments: []capability.Argument{
		{Name: "title", Description: "Milestone title, 1 to 255 characters, unique in the repository", Required: true},
		{Name: "state", Description: "open or closed; open when omitted"},
		{Name: "description", Description: "Description text"},
		{Name: "due_on", Description: "Due date as an ISO 8601 timestamp in UTC, such as 2026-03-01T00:00:00Z"},
	},
	Fields: milestoneFields,
	Examples: []capability.Example{{
		Description: "Create a milestone",
		Arguments:   json.RawMessage(`{"title":"v1.0","due_on":"2026-03-01T00:00:00Z"}`),
	}},
}

var milestonesUpdate = capability.Descriptor{
	ID:      Provider + ".milestones.update",
	Version: 1,
	Title:   "Update a GitHub milestone",
	Description: "Change the title, state, description, or due date of one milestone of a repository a " +
		"connection allows; only the given values are sent and at least one is required; idempotent, since " +
		"sending the same values again leaves GitHub in the same state",
	Tags:     []string{"github", "milestones", "update"},
	Risk:     changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
	Provider: Provider,
	InputSchema: inputSchema(`"number":`+milestoneNumberSchema+`,"title":`+milestoneTitleSchema+`,"state":`+
		milestoneStateSchema+`,"description":`+milestoneDescriptionSchema+`,"due_on":`+milestoneDueOnSchema, "number"),
	OutputSchema: json.RawMessage(milestoneOutput),
	Arguments: []capability.Argument{
		{Name: "number", Description: "Milestone number", Required: true},
		{Name: "title", Description: "New title, 1 to 255 characters"},
		{Name: "state", Description: "open or closed"},
		{Name: "description", Description: "New description; \"\" clears it"},
		{Name: "due_on", Description: "New due date as an ISO 8601 timestamp in UTC; it cannot be cleared"},
	},
	Fields: milestoneFields,
	Examples: []capability.Example{{
		Description: "Close a milestone",
		Arguments:   json.RawMessage(`{"number":1,"state":"closed"}`),
	}},
}

const milestoneDeletedOutput = `{"type":"object","properties":{"deleted":{"type":"boolean"},"number":{"type":"integer"}},` +
	`"required":["deleted","number"],"additionalProperties":false}`

var milestonesDelete = capability.Descriptor{
	ID:      Provider + ".milestones.delete",
	Version: 1,
	Title:   "Delete a GitHub milestone",
	Description: "Delete one milestone of a repository a connection allows permanently, removing it from " +
		"every issue and pull request that carries it; offered only where a connection's tools list names it",
	Tags:                  []string{"github", "milestones", "delete"},
	Risk:                  guardedRisk(capability.EffectDelete, capability.IdempotencyUnknown, milestoneSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema:           inputSchema(`"number":`+milestoneNumberSchema, "number"),
	OutputSchema:          json.RawMessage(milestoneDeletedOutput),
	Arguments: []capability.Argument{
		{Name: "number", Description: "Milestone number to delete", Required: true},
	},
	Fields: []capability.Field{
		{Name: "deleted", Description: "True once GitHub deleted the milestone"},
		{Name: "number", Description: "Number of the milestone that was deleted"},
	},
	Examples: []capability.Example{{Description: "Delete a milestone", Arguments: json.RawMessage(`{"number":3}`)}},
}

// milestoneArguments holds the arguments of the milestone get, create, update, and delete tools. The optional
// values are pointers so update tells a value left out from one sent empty.
type milestoneArguments struct {
	Number      int     `json:"number"`
	Title       *string `json:"title"`
	State       *string `json:"state"`
	Description *string `json:"description"`
	DueOn       *string `json:"due_on"`
}

// milestoneHandler checks the arguments and the repository before a credential is resolved, so a refused
// request never becomes a provider call.
func milestoneHandler(id string, check func(*milestoneArguments) error,
	call func(context.Context, *Client, *milestoneArguments) (any, error)) capability.Handler {
	return func(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
		raw json.RawMessage) (any, error) {
		var arguments milestoneArguments
		if err := json.Unmarshal(raw, &arguments); err != nil {
			return nil, unreadable(id)
		}
		bound, err := selectTarget(resolved, kindRepository, raw)
		if err != nil {
			return nil, err
		}
		if err := check(&arguments); err != nil {
			return nil, err
		}
		client, err := openAt(ctx, resolved, secrets, red, bound)
		if err != nil {
			return nil, err
		}
		return bound.locate(call(ctx, client, &arguments))
	}
}

func checkMilestoneNumber(a *milestoneArguments) error {
	if a.Number < 1 || a.Number > 1000000000 {
		return invalidRequest("number must be a positive milestone number")
	}
	return nil
}

func checkMilestoneTitle(value *string) error {
	if value == nil {
		return nil
	}
	if err := checkText("title", *value); err != nil {
		return err
	}
	if n := utf8.RuneCountInString(*value); n < 1 || n > maxMilestoneTitleLength {
		return invalidRequest("title must be 1 to 255 characters")
	}
	return nil
}

func checkMilestoneValues(a *milestoneArguments) error {
	if err := checkMilestoneTitle(a.Title); err != nil {
		return err
	}
	if a.State != nil && *a.State != "open" && *a.State != "closed" {
		return invalidRequest("state must be open or closed")
	}
	if a.Description != nil {
		if err := checkText("description", *a.Description); err != nil {
			return err
		}
		if utf8.RuneCountInString(*a.Description) > maxMilestoneDescriptionLength {
			return invalidRequest("description must be at most 65536 characters")
		}
	}
	if a.DueOn != nil {
		if _, err := time.Parse(time.RFC3339, *a.DueOn); err != nil {
			return invalidRequest("due_on must be an ISO 8601 timestamp, such as 2026-03-01T00:00:00Z")
		}
	}
	return nil
}

func checkMilestoneCreate(a *milestoneArguments) error {
	if a.Title == nil {
		return invalidRequest("title is required")
	}
	return checkMilestoneValues(a)
}

func checkMilestoneUpdate(a *milestoneArguments) error {
	if err := checkMilestoneNumber(a); err != nil {
		return err
	}
	if a.Title == nil && a.State == nil && a.Description == nil && a.DueOn == nil {
		return invalidRequest("give at least one of title, state, description, or due_on to change")
	}
	return checkMilestoneValues(a)
}

const milestonesChangePermission = "GitHub refused this change of a milestone; it needs repo on a classic " +
	"token, or Issues: read and write on a fine-grained token"

func milestonePath(number int) string { return "milestones/" + strconv.Itoa(number) }

func (c *Client) getMilestone(ctx context.Context, number int) (*Milestone, error) {
	const op = "get milestone"
	var raw milestoneRaw
	if err := c.rest(ctx, op, c.repoPath(milestonePath(number)), &raw); err != nil {
		return nil, actionsFailure(err, milestonesReadPermission)
	}
	return milestoneResult(op, &raw)
}

func milestoneResult(op string, raw *milestoneRaw) (*Milestone, error) {
	milestone := raw.project()
	if milestone.Number < 1 || milestone.Title == "" {
		return nil, invalidEntry(op, "a milestone")
	}
	return &milestone, nil
}

func (c *Client) createMilestone(ctx context.Context, a *milestoneArguments) (*Milestone, error) {
	const op = "create milestone"
	body := milestoneBody(a)
	var raw milestoneRaw
	if err := c.restChange(ctx, op, http.MethodPost, c.repoPath("milestones"), body, &raw); err != nil {
		return nil, actionsFailure(err, milestonesChangePermission)
	}
	return milestoneResult(op, &raw)
}

func (c *Client) updateMilestone(ctx context.Context, a *milestoneArguments) (*Milestone, error) {
	const op = "update milestone"
	var raw milestoneRaw
	if err := c.restChange(ctx, op, http.MethodPatch, c.repoPath(milestonePath(a.Number)), milestoneBody(a),
		&raw); err != nil {
		return nil, actionsFailure(err, milestonesChangePermission)
	}
	return milestoneResult(op, &raw)
}

func (c *Client) deleteMilestone(ctx context.Context, number int) (any, error) {
	const op = "delete milestone"
	if err := c.restChange(ctx, op, http.MethodDelete, c.repoPath(milestonePath(number)), nil, nil); err != nil {
		return nil, actionsFailure(err, milestonesChangePermission)
	}
	return map[string]any{"deleted": true, "number": number}, nil
}

// milestoneBody holds only the values that were given.
func milestoneBody(a *milestoneArguments) map[string]any {
	body := map[string]any{}
	if a.Title != nil {
		body["title"] = *a.Title
	}
	if a.State != nil {
		body["state"] = *a.State
	}
	if a.Description != nil {
		body["description"] = *a.Description
	}
	if a.DueOn != nil {
		body["due_on"] = *a.DueOn
	}
	return body
}

// Permission message of the milestone tool. GitHub decides on every request; the message names what such a
// request needs without claiming what the configured token holds.
const milestonesReadPermission = "GitHub refused this token the milestones of this repository; reading them " +
	"needs no scope for a public repository, or repo on a classic token, or Issues: read on a fine-grained " +
	"token, for a private one"

// milestoneRaw is the REST milestone; links holds its REST url and labels_url.
type milestoneRaw struct {
	ID           *int64      `json:"id"`
	NodeID       *string     `json:"node_id"`
	Number       *int        `json:"number"`
	State        *string     `json:"state"`
	Title        *string     `json:"title"`
	Description  *string     `json:"description"`
	Creator      *accountRaw `json:"creator"`
	OpenIssues   *int        `json:"open_issues"`
	ClosedIssues *int        `json:"closed_issues"`
	CreatedAt    *string     `json:"created_at"`
	UpdatedAt    *string     `json:"updated_at"`
	ClosedAt     *string     `json:"closed_at"`
	DueOn        *string     `json:"due_on"`
	HTMLURL      *string     `json:"html_url"`
	links        Links
}

func (r *milestoneRaw) UnmarshalJSON(data []byte) (err error) {
	type plain milestoneRaw
	if err = json.Unmarshal(data, (*plain)(r)); err != nil {
		return err
	}
	r.links, err = readLinks(data, milestoneLinkKeys)
	return err
}

// Milestone is the view of one milestone: the compact members first, then the remainder of GitHub's
// milestone. URL is the web address; the REST url and labels_url sit in Links.
type Milestone struct {
	Number       int          `json:"number"`
	Title        string       `json:"title"`
	Description  string       `json:"description,omitempty"`
	State        string       `json:"state"`
	DueOn        string       `json:"due_on,omitempty"`
	OpenIssues   int          `json:"open_issues"`
	ClosedIssues int          `json:"closed_issues"`
	ID           *int64       `json:"id,omitempty"`
	NodeID       *string      `json:"node_id,omitempty"`
	Creator      *RESTAccount `json:"creator,omitempty"`
	CreatedAt    *string      `json:"created_at,omitempty"`
	UpdatedAt    *string      `json:"updated_at,omitempty"`
	ClosedAt     *string      `json:"closed_at,omitempty"`
	URL          *string      `json:"url,omitempty"`
	Links        Links        `json:"links,omitempty"`
}

// project keeps the compact members exactly as GitHub sent them, the way the milestone decoded before the
// remaining members were added; only the new members pass through the projection helpers.
func (r *milestoneRaw) project() Milestone {
	return Milestone{Number: valueOr(r.Number), Title: stringOr(r.Title), Description: stringOr(r.Description),
		State: stringOr(r.State), DueOn: stringOr(r.DueOn), OpenIssues: valueOr(r.OpenIssues),
		ClosedIssues: valueOr(r.ClosedIssues), ID: r.ID, NodeID: nameText(r.NodeID), Creator: r.Creator.project(),
		CreatedAt: nameText(r.CreatedAt), UpdatedAt: nameText(r.UpdatedAt), ClosedAt: nameText(r.ClosedAt),
		URL: address(r.HTMLURL), Links: r.links}
}

// RESTMilestone is a milestone as another object carries it, such as an issue: the members of Milestone,
// every one passed through the projection helpers and left out when GitHub leaves it out.
type RESTMilestone struct {
	Number       *int         `json:"number,omitempty"`
	Title        *string      `json:"title,omitempty"`
	Description  *string      `json:"description,omitempty"`
	State        *string      `json:"state,omitempty"`
	DueOn        *string      `json:"due_on,omitempty"`
	OpenIssues   *int         `json:"open_issues,omitempty"`
	ClosedIssues *int         `json:"closed_issues,omitempty"`
	ID           *int64       `json:"id,omitempty"`
	NodeID       *string      `json:"node_id,omitempty"`
	Creator      *RESTAccount `json:"creator,omitempty"`
	CreatedAt    *string      `json:"created_at,omitempty"`
	UpdatedAt    *string      `json:"updated_at,omitempty"`
	ClosedAt     *string      `json:"closed_at,omitempty"`
	URL          *string      `json:"url,omitempty"`
	Links        Links        `json:"links,omitempty"`
}

func (r *milestoneRaw) detail() *RESTMilestone {
	if r == nil {
		return nil
	}
	return &RESTMilestone{Number: r.Number, Title: shortText(r.Title), Description: bulkText(r.Description),
		State: shortText(r.State), DueOn: shortText(r.DueOn), OpenIssues: r.OpenIssues, ClosedIssues: r.ClosedIssues,
		ID: r.ID, NodeID: shortText(r.NodeID), Creator: r.Creator.project(), CreatedAt: shortText(r.CreatedAt),
		UpdatedAt: shortText(r.UpdatedAt), ClosedAt: shortText(r.ClosedAt), URL: address(r.HTMLURL), Links: r.links}
}

// MilestoneList is one batch of milestones.
type MilestoneList struct {
	Milestones []Milestone `json:"milestones"`
	NextCursor string      `json:"next_cursor,omitempty"`
	HasMore    bool        `json:"has_more"`
}

func (c *Client) listMilestones(ctx context.Context, a *milestonesArguments) (*MilestoneList, error) {
	const op = "list milestones"
	var raw []milestoneRaw
	hasNext, err := c.restPage(ctx, op, c.repoPath("milestones"), a.query(), &raw)
	if err != nil {
		return nil, actionsFailure(err, milestonesReadPermission)
	}
	result := &MilestoneList{Milestones: make([]Milestone, 0, len(raw))}
	for i := range raw {
		milestone := raw[i].project()
		if milestone.Number < 1 || milestone.Title == "" {
			return nil, invalidEntry(op, "a milestone")
		}
		result.Milestones = append(result.Milestones, milestone)
	}
	result.HasMore, result.NextCursor = morePage(a.binding, a.page, a.perPage, hasNext)
	return result, nil
}

// milestonesSubject names the milestone a repository path below /milestones addresses: milestones for the
// collection, or milestones/NUMBER for one, the way labelsSubject names a label below /labels. It is empty
// for any other path.
func milestonesSubject(path string) string {
	if path == "milestones" {
		return "the milestones of this repository"
	}
	if number, ok := trimNumericSuffix(path, "milestones/"); ok {
		return "milestone " + number
	}
	return ""
}

// trimNumericSuffix reports whether path is prefix followed by one or more digits and nothing else, and
// returns the digits.
func trimNumericSuffix(path, prefix string) (string, bool) {
	rest, ok := strings.CutPrefix(path, prefix)
	if !ok || rest == "" {
		return "", false
	}
	for _, r := range rest {
		if r < '0' || r > '9' {
			return "", false
		}
	}
	return rest, true
}
