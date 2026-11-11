package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// The history of an issue and its assignees: github.issue-events.list reads the events of one issue or, without
// a number, of every issue of the repository, github.issue-timeline.list the timeline of one issue,
// github.assignees.list the users that may be assigned in the repository, and github.assignees.check whether
// one login may be. All four read one fixed route of the bound repository. Pull requests are issues for these
// routes, so a pull request number is accepted; the route stays inside the bound repository either way.
//
// Everything a timeline or an event names outside the connection's targets is reduced to identifiers: the
// repository and number of a cross-referenced issue, the repository and SHA of a referenced commit. Event
// types are an open set, so an event of a type this file does not know keeps its type and the members every
// event shares; members it cannot read are left out, never quoted raw.

const (
	issueEventsReadPermission = "GitHub refused this token the issue events of this repository; reading them " +
		"needs no scope for a public repository, or repo on a classic token, or Issues: read on a " +
		"fine-grained token, for a private one"
	issueTimelineReadPermission = "GitHub refused this token the timeline of this issue; reading it needs no " +
		"scope for a public repository, or repo on a classic token, or Issues: read on a fine-grained " +
		"token, for a private one"
	assigneesReadPermission = "GitHub refused this token the assignable users of this repository; reading " +
		"them needs no scope for a public repository, or repo on a classic token, or Issues: read on a " +
		"fine-grained token, for a private one"
)

// ---- Raw events

type eventLabelRaw struct {
	Name  *string `json:"name"`
	Color *string `json:"color"`
}

type eventMilestoneRaw struct {
	Title *string `json:"title"`
}

type eventRenameRaw struct {
	From *string `json:"from"`
	To   *string `json:"to"`
}

type eventDismissedReviewRaw struct {
	State             *string `json:"state"`
	ReviewID          *int64  `json:"review_id"`
	DismissalMessage  *string `json:"dismissal_message"`
	DismissalCommitID *string `json:"dismissal_commit_id"`
}

type eventProjectCardRaw struct {
	ID                 *int64  `json:"id"`
	URL                *string `json:"url"`
	ProjectID          *int64  `json:"project_id"`
	ProjectURL         *string `json:"project_url"`
	ColumnName         *string `json:"column_name"`
	PreviousColumnName *string `json:"previous_column_name"`
}

type eventTeamRaw struct {
	core        teamCore
	Permissions *RESTPermissions `json:"permissions"`
	Parent      *teamCore        `json:"parent"`
}

func (r *eventTeamRaw) UnmarshalJSON(data []byte) error {
	type plain eventTeamRaw
	if err := json.Unmarshal(data, (*plain)(r)); err != nil {
		return err
	}
	return json.Unmarshal(data, &r.core)
}

type eventParentRaw struct {
	SHA     *string `json:"sha"`
	HTMLURL *string `json:"html_url"`
	links   Links
}

func (r *eventParentRaw) UnmarshalJSON(data []byte) (err error) {
	type plain eventParentRaw
	if err = json.Unmarshal(data, (*plain)(r)); err != nil {
		return err
	}
	r.links, err = readLinks(data, restCommitParentKeys)
	return err
}

// eventReferenceRaw is the minimal issue reference of a sub-issue, parent-issue, or dependency event.
type eventReferenceRaw struct {
	Number      *int64         `json:"number"`
	Title       *string        `json:"title"`
	State       *string        `json:"state"`
	StateReason *string        `json:"state_reason"`
	Repository  *repositoryRaw `json:"repository"`
	IssueType   *issueTypeJSON `json:"issue_type"`
}

// eventIssueRaw is an issue an event carries, with whether it is a pull request.
type eventIssueRaw struct {
	issue       subIssueJSON
	pullRequest bool
	pull        eventPullRequestRaw
	links       Links
}

type eventPullRequestRaw struct {
	MergedAt *string `json:"merged_at"`
	HTMLURL  *string `json:"html_url"`
	links    Links
}

func (r *eventPullRequestRaw) UnmarshalJSON(data []byte) (err error) {
	type plain eventPullRequestRaw
	if err = json.Unmarshal(data, (*plain)(r)); err != nil {
		return err
	}
	r.links, err = readLinks(data, []string{"url", "diff_url", "patch_url"})
	return err
}

func (r *eventIssueRaw) UnmarshalJSON(data []byte) error {
	if err := json.Unmarshal(data, &r.issue); err != nil {
		return err
	}
	var probe struct {
		PullRequest json.RawMessage `json:"pull_request"`
	}
	_ = json.Unmarshal(data, &probe)
	r.pullRequest = len(probe.PullRequest) > 0 && string(probe.PullRequest) != "null"
	if r.pullRequest {
		_ = json.Unmarshal(probe.PullRequest, &r.pull)
	}
	r.links, _ = readLinks(data, []string{"repository_url"})
	return nil
}

// lineCommentRaw is a pull request line comment of a timeline: the compact members and the width.
type lineCommentRaw struct {
	ID             *int64  `json:"id"`
	Path           *string `json:"path"`
	Line           *int64  `json:"line"`
	Side           *string `json:"side"`
	CommitID       *string `json:"commit_id"`
	InReplyToID    *int64  `json:"in_reply_to_id"`
	Body           *string `json:"body"`
	CreatedAt      *string `json:"created_at"`
	UpdatedAt      *string `json:"updated_at"`
	CopilotReview  *bool   `json:"is_copilot_code_review"`
	CopilotSources []struct {
		Kind *string `json:"kind"`
		Name *string `json:"name"`
		Path *string `json:"path"`
	} `json:"copilot_code_review_sources"`
	wide reviewCommentWideRaw
}

func (r *lineCommentRaw) UnmarshalJSON(data []byte) error {
	type plain lineCommentRaw
	if err := json.Unmarshal(data, (*plain)(r)); err != nil {
		return err
	}
	_ = json.Unmarshal(data, &r.wide)
	return nil
}

// timelineRaw is the union of the members of GitHub's issue events and timeline events. Each event type
// sets some of them.
type timelineRaw struct {
	ID                    *int64                   `json:"id"`
	NodeID                *string                  `json:"node_id"`
	Event                 *string                  `json:"event"`
	Actor                 *accountRaw              `json:"actor"`
	CreatedAt             *string                  `json:"created_at"`
	UpdatedAt             *string                  `json:"updated_at"`
	CommitID              *string                  `json:"commit_id"`
	CommitURL             *string                  `json:"commit_url"`
	Label                 *eventLabelRaw           `json:"label"`
	Assignee              *accountRaw              `json:"assignee"`
	Assigner              *accountRaw              `json:"assigner"`
	ReviewRequester       *accountRaw              `json:"review_requester"`
	RequestedReviewer     *accountRaw              `json:"requested_reviewer"`
	RequestedTeam         *eventTeamRaw            `json:"requested_team"`
	DismissedReview       *eventDismissedReviewRaw `json:"dismissed_review"`
	Milestone             *eventMilestoneRaw       `json:"milestone"`
	ProjectCard           *eventProjectCardRaw     `json:"project_card"`
	Rename                *eventRenameRaw          `json:"rename"`
	LockReason            *string                  `json:"lock_reason"`
	AuthorAssociation     *string                  `json:"author_association"`
	PerformedViaGitHubApp *appRaw                  `json:"performed_via_github_app"`
	Intent                *struct {
		Rationale  *string `json:"rationale"`
		Confidence *string `json:"confidence"`
	} `json:"intent"`
	IssueType     *issueTypeJSON     `json:"issue_type"`
	PrevIssueType *issueTypeJSON     `json:"prev_issue_type"`
	SubIssue      *eventReferenceRaw `json:"sub_issue"`
	ParentIssue   *eventReferenceRaw `json:"parent_issue"`
	BlockedBy     *eventReferenceRaw `json:"blocked_by"`
	Blocking      *eventReferenceRaw `json:"blocking"`
	Minimized     *struct {
		Reason *string `json:"reason"`
	} `json:"minimized"`
	Pin *struct {
		PinnedAt *string     `json:"pinned_at"`
		PinnedBy *accountRaw `json:"pinned_by"`
	} `json:"pin"`
	Issue       json.RawMessage `json:"issue"`
	Body        *string         `json:"body"`
	User        *accountRaw     `json:"user"`
	HTMLURL     *string         `json:"html_url"`
	Reactions   *reactionsRaw   `json:"reactions"`
	State       *string         `json:"state"`
	StateReason *string         `json:"state_reason"`
	SubmittedAt *string         `json:"submitted_at"`
	Source      *struct {
		Type  *string         `json:"type"`
		Issue json.RawMessage `json:"issue"`
	} `json:"source"`
	SHA          *string                `json:"sha"`
	Author       *commitIdentityRaw     `json:"author"`
	Committer    *commitIdentityRaw     `json:"committer"`
	Message      *string                `json:"message"`
	Tree         *commitTreeRaw         `json:"tree"`
	Parents      []eventParentRaw       `json:"parents"`
	Verification *commitVerificationRaw `json:"verification"`
	Comments     json.RawMessage        `json:"comments"`
	links        Links
}

// UnmarshalJSON reads as much of an event as fits: event types are an open set, so a member of a type this
// reading does not expect must not turn the whole answer into an error. A member of the wrong shape costs
// that event its other members, never the event itself or the list.
func (r *timelineRaw) UnmarshalJSON(data []byte) error {
	type plain timelineRaw
	var full plain
	var shape *json.UnmarshalTypeError
	if err := json.Unmarshal(data, &full); err != nil && !errors.As(err, &shape) {
		var basic struct {
			ID        *int64  `json:"id"`
			NodeID    *string `json:"node_id"`
			Event     *string `json:"event"`
			CreatedAt *string `json:"created_at"`
		}
		if err := json.Unmarshal(data, &basic); err != nil && !errors.As(err, &shape) {
			return err
		}
		*r = timelineRaw{ID: basic.ID, NodeID: basic.NodeID, Event: basic.Event, CreatedAt: basic.CreatedAt}
		return nil
	}
	*r = timelineRaw(full)
	r.links, _ = readLinks(data, eventLinkKeys)
	return nil
}

var eventLinkKeys = []string{"url", "commit_url", "issue_url", "pull_request_url"}

// ---- Projected events

// EventLabel is the label of a labeled or unlabeled event.
type EventLabel struct {
	Name  *string `json:"name,omitempty"`
	Color *string `json:"color,omitempty"`
}

// EventMilestone is the milestone of a milestoned or demilestoned event.
type EventMilestone struct {
	Title *string `json:"title,omitempty"`
}

// EventRename is the title change of a renamed event.
type EventRename struct {
	From *string `json:"from,omitempty"`
	To   *string `json:"to,omitempty"`
}

// EventDismissedReview is the review a review-dismissed event dismissed. The message is untrusted data.
type EventDismissedReview struct {
	State                     *string `json:"state,omitempty"`
	ReviewID                  *int64  `json:"review_id,omitempty"`
	DismissalMessage          *string `json:"dismissal_message,omitempty"`
	DismissalMessageTruncated bool    `json:"dismissal_message_truncated,omitempty"`
	DismissalCommitID         *string `json:"dismissal_commit_id,omitempty"`
}

// EventProjectCard is the classic project card of a project event.
type EventProjectCard struct {
	ID                 *int64  `json:"id,omitempty"`
	URL                *string `json:"url,omitempty"`
	ProjectID          *int64  `json:"project_id,omitempty"`
	ProjectURL         *string `json:"project_url,omitempty"`
	ColumnName         *string `json:"column_name,omitempty"`
	PreviousColumnName *string `json:"previous_column_name,omitempty"`
}

// EventTeam is the team of a review request event.
type EventTeam struct {
	RESTTeamSimple
	Permissions *RESTPermissions `json:"permissions,omitempty"`
	Parent      *RESTTeamSimple  `json:"parent,omitempty"`
}

// EventIntent is the intent an agent gave for the change of an event. Rationale is untrusted data.
type EventIntent struct {
	Rationale          *string `json:"rationale,omitempty"`
	RationaleTruncated bool    `json:"rationale_truncated,omitempty"`
	Confidence         *string `json:"confidence,omitempty"`
}

// EventIssueReference is the issue a sub-issue, parent-issue, or dependency event names. Outside the
// connection's targets only Number and the identifiers of Repository are set, with Withheld; when the
// repository cannot be told, only Withheld. Title is untrusted data.
type EventIssueReference struct {
	Number      *int64          `json:"number,omitempty"`
	Title       *string         `json:"title,omitempty"`
	State       *string         `json:"state,omitempty"`
	StateReason *string         `json:"state_reason,omitempty"`
	Repository  *RESTRepository `json:"repository,omitempty"`
	IssueType   *RESTIssueType  `json:"issue_type,omitempty"`
	Withheld    bool            `json:"withheld,omitempty"`
}

// EventIssue is an issue an event names. Outside the connection's targets only Number, Repository, and
// IsPullRequest are set.
type EventIssue struct {
	IssueEntry
	IsPullRequest bool              `json:"is_pull_request,omitempty"`
	PullRequest   *EventPullRequest `json:"pull_request,omitempty"`
}

// EventPullRequest is the pull request an issue stands for.
type EventPullRequest struct {
	MergedAt *string `json:"merged_at,omitempty"`
	URL      *string `json:"url,omitempty"`
	Links    Links   `json:"links,omitempty"`
}

// IssueEventBase holds the members every issue event and timeline event may carry.
type IssueEventBase struct {
	ID                    *int64                `json:"id,omitempty"`
	NodeID                *string               `json:"node_id,omitempty"`
	Event                 *string               `json:"event,omitempty"`
	Actor                 *RESTAccount          `json:"actor,omitempty"`
	CreatedAt             *string               `json:"created_at,omitempty"`
	CommitID              *string               `json:"commit_id,omitempty"`
	CommitRepository      *string               `json:"commit_repository,omitempty"`
	Label                 *EventLabel           `json:"label,omitempty"`
	Assignee              *RESTAccount          `json:"assignee,omitempty"`
	Assigner              *RESTAccount          `json:"assigner,omitempty"`
	ReviewRequester       *RESTAccount          `json:"review_requester,omitempty"`
	RequestedReviewer     *RESTAccount          `json:"requested_reviewer,omitempty"`
	RequestedTeam         *EventTeam            `json:"requested_team,omitempty"`
	DismissedReview       *EventDismissedReview `json:"dismissed_review,omitempty"`
	Milestone             *EventMilestone       `json:"milestone,omitempty"`
	ProjectCard           *EventProjectCard     `json:"project_card,omitempty"`
	Rename                *EventRename          `json:"rename,omitempty"`
	LockReason            *string               `json:"lock_reason,omitempty"`
	AuthorAssociation     *string               `json:"author_association,omitempty"`
	PerformedViaGitHubApp *RESTApp              `json:"performed_via_github_app,omitempty"`
	Intent                *EventIntent          `json:"intent,omitempty"`
	IssueType             *RESTIssueType        `json:"issue_type,omitempty"`
	PrevIssueType         *RESTIssueType        `json:"prev_issue_type,omitempty"`
	SubIssue              *EventIssueReference  `json:"sub_issue,omitempty"`
	ParentIssue           *EventIssueReference  `json:"parent_issue,omitempty"`
	BlockedBy             *EventIssueReference  `json:"blocked_by,omitempty"`
	Blocking              *EventIssueReference  `json:"blocking,omitempty"`
	Links                 Links                 `json:"links,omitempty"`
}

// IssueEvent is one event of the issue events lists.
type IssueEvent struct {
	IssueEventBase
	Issue *EventIssue `json:"issue,omitempty"`
}

// TimelineSource is the source of a cross-reference. Withheld is true when the issue lies outside the
// connection's targets or its repository is unknown, so Issue holds identifiers only or is absent.
type TimelineSource struct {
	Type     *string     `json:"type,omitempty"`
	Issue    *EventIssue `json:"issue,omitempty"`
	Withheld bool        `json:"withheld,omitempty"`
}

// TimelineLineComment is a pull request line comment of a timeline. Body is untrusted data.
type TimelineLineComment struct {
	ID                          *int64                    `json:"id,omitempty"`
	Path                        *string                   `json:"path,omitempty"`
	Line                        *int64                    `json:"line,omitempty"`
	Side                        *string                   `json:"side,omitempty"`
	CommitID                    *string                   `json:"commit_id,omitempty"`
	InReplyToID                 *int64                    `json:"in_reply_to_id,omitempty"`
	Body                        *string                   `json:"body,omitempty"`
	BodyTruncated               bool                      `json:"body_truncated,omitempty"`
	CreatedAt                   *string                   `json:"created_at,omitempty"`
	UpdatedAt                   *string                   `json:"updated_at,omitempty"`
	IsCopilotCodeReview         *bool                     `json:"is_copilot_code_review,omitempty"`
	CopilotCodeReviewSources    []CopilotCodeReviewSource `json:"copilot_code_review_sources,omitempty"`
	CopilotCodeReviewSourcesCut bool                      `json:"copilot_code_review_sources_truncated,omitempty"`
	ReviewCommentWidth
}

// CopilotCodeReviewSource is a source a Copilot code review comment drew on. Untrusted data.
type CopilotCodeReviewSource struct {
	Kind *string `json:"kind,omitempty"`
	Name *string `json:"name,omitempty"`
	Path *string `json:"path,omitempty"`
}

// TimelineEvent is one event of an issue timeline: the shared members and those of the event types that only
// the timeline carries. Texts are untrusted data.
type TimelineEvent struct {
	IssueEventBase
	UpdatedAt               *string                 `json:"updated_at,omitempty"`
	SubmittedAt             *string                 `json:"submitted_at,omitempty"`
	Body                    *string                 `json:"body,omitempty"`
	BodyTruncated           bool                    `json:"body_truncated,omitempty"`
	User                    *RESTAccount            `json:"user,omitempty"`
	URL                     *string                 `json:"url,omitempty"`
	Reactions               *RESTReactions          `json:"reactions,omitempty"`
	Minimized               *RESTCommentMinimized   `json:"minimized,omitempty"`
	Pin                     *RESTCommentPin         `json:"pin,omitempty"`
	State                   *string                 `json:"state,omitempty"`
	StateReason             *string                 `json:"state_reason,omitempty"`
	Source                  *TimelineSource         `json:"source,omitempty"`
	SHA                     *string                 `json:"sha,omitempty"`
	Author                  *RESTCommitIdentity     `json:"author,omitempty"`
	Committer               *RESTCommitIdentity     `json:"committer,omitempty"`
	Message                 *string                 `json:"message,omitempty"`
	MessageTruncated        bool                    `json:"message_truncated,omitempty"`
	Tree                    *RESTCommitTree         `json:"tree,omitempty"`
	Parents                 []RESTCommitParent      `json:"parents,omitempty"`
	Verification            *RESTCommitVerification `json:"verification,omitempty"`
	LineComments            []TimelineLineComment   `json:"line_comments,omitempty"`
	LineCommentsTruncated   bool                    `json:"line_comments_truncated,omitempty"`
	CommitComments          []RESTCommitComment     `json:"commit_comments,omitempty"`
	CommitCommentsTruncated bool                    `json:"commit_comments_truncated,omitempty"`
}

// IssueEventList is one batch of issue events.
type IssueEventList struct {
	Events     []IssueEvent `json:"events"`
	NextCursor string       `json:"next_cursor,omitempty"`
	HasMore    bool         `json:"has_more"`
}

// IssueTimelineList is one batch of timeline events.
type IssueTimelineList struct {
	Events     []TimelineEvent `json:"events"`
	NextCursor string          `json:"next_cursor,omitempty"`
	HasMore    bool            `json:"has_more"`
}

// AssigneeList is one batch of the users that may be assigned.
type AssigneeList struct {
	Assignees  []RESTAccount `json:"assignees"`
	NextCursor string        `json:"next_cursor,omitempty"`
	HasMore    bool          `json:"has_more"`
}

// AssigneeCheck tells whether one login may be assigned in the repository.
type AssigneeCheck struct {
	Assignee   string `json:"assignee"`
	Assignable bool   `json:"assignable"`
}

// ---- Projection

// apiRepository reads the repository a GitHub API address names. after is the path segment that must follow
// the repository name, such as commits, or empty when the address ends with the name.
func apiRepository(p *string, after string) (target, bool) {
	if p == nil {
		return target{}, false
	}
	parsed, err := url.Parse(*p)
	if err != nil {
		return target{}, false
	}
	segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	for i := len(segments) - 3; i >= 0; i-- {
		if segments[i] != "repos" {
			continue
		}
		rest := segments[i+1:]
		if (after == "" && len(rest) != 2) || (after != "" && (len(rest) < 3 || rest[2] != after)) {
			return target{}, false
		}
		if !validLogin(rest[0]) || !validRepoName(rest[1]) {
			return target{}, false
		}
		return target{kind: kindRepository, owner: rest[0], repo: rest[1]}, true
	}
	return target{}, false
}

func sameRepository(a, b target) bool {
	return strings.EqualFold(a.owner, b.owner) && strings.EqualFold(a.repo, b.repo)
}

// eventIssue projects an issue an event names. withheld is true when the issue is not shown in full: its
// repository lies outside the targets, or is not known and assumeOwn is false. The repository of an issue
// the route itself addresses is the bound one when GitHub leaves it out.
func eventIssue(op string, data json.RawMessage, own target, allowed allowlist, assumeOwn bool) (*EventIssue, bool, error) {
	if len(data) == 0 || string(data) == "null" {
		return nil, false, nil
	}
	var raw eventIssueRaw
	if err := json.Unmarshal(data, &raw); err != nil || raw.issue.Number < 1 {
		return nil, true, nil
	}
	repo, known := own, assumeOwn
	if raw.issue.Repository != nil && raw.issue.Repository.FullName != "" {
		owner, name, _ := strings.Cut(raw.issue.Repository.FullName, "/")
		repo, known = target{kind: kindRepository, owner: owner, repo: name}, validLogin(owner) && validRepoName(name)
	} else if found, ok := apiRepository(address(optional(raw.links["repository_url"])), ""); ok {
		repo, known = found, true
	}
	if !known {
		return nil, true, nil
	}
	if !allowed.allows(repo) {
		ref := SubIssueRef{Number: raw.issue.Number, Repository: repo.owner + "/" + repo.repo}
		return &EventIssue{IssueEntry: IssueEntry{SubIssueRef: ref}, IsPullRequest: raw.pullRequest}, true, nil
	}
	if !sameRepository(repo, own) && (raw.issue.Repository == nil || raw.issue.Repository.FullName == "") {
		raw.issue.Repository = &struct {
			FullName string `json:"full_name"`
		}{FullName: repo.owner + "/" + repo.repo}
	}
	entry, err := raw.issue.entry(op, own, allowed, "")
	if err != nil {
		return nil, false, err
	}
	result := &EventIssue{IssueEntry: entry, IsPullRequest: raw.pullRequest}
	if raw.pullRequest {
		result.PullRequest = &EventPullRequest{MergedAt: shortText(raw.pull.MergedAt), URL: address(raw.pull.HTMLURL),
			Links: raw.pull.links}
	}
	return result, false, nil
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// base projects the members every event shares. A commit outside the connection's targets keeps its SHA and
// its repository, but not its address.
func (r *timelineRaw) base(own target, allowed allowlist) IssueEventBase {
	base := IssueEventBase{ID: r.ID, NodeID: shortText(r.NodeID), Event: shortText(r.Event), Actor: r.Actor.project(),
		CreatedAt: shortText(r.CreatedAt), CommitID: shortText(r.CommitID), Assignee: r.Assignee.project(),
		Assigner: r.Assigner.project(), ReviewRequester: r.ReviewRequester.project(),
		RequestedReviewer: r.RequestedReviewer.project(), LockReason: shortText(r.LockReason),
		AuthorAssociation: shortText(r.AuthorAssociation), PerformedViaGitHubApp: r.PerformedViaGitHubApp.project(),
		Links: r.links}
	if i := r.Intent; i != nil {
		rationale, cut := cleanText(filled(i.Rationale), projectionTextLimit, true)
		base.Intent = &EventIntent{Rationale: rationale, RationaleTruncated: cut, Confidence: shortText(i.Confidence)}
	}
	base.IssueType, base.PrevIssueType = r.IssueType.detail(), r.PrevIssueType.detail()
	base.SubIssue, base.ParentIssue = r.SubIssue.project(allowed), r.ParentIssue.project(allowed)
	base.BlockedBy, base.Blocking = r.BlockedBy.project(allowed), r.Blocking.project(allowed)
	if l := r.Label; l != nil {
		base.Label = &EventLabel{Name: shortText(l.Name), Color: shortText(l.Color)}
	}
	if m := r.Milestone; m != nil {
		base.Milestone = &EventMilestone{Title: longText(m.Title)}
	}
	if n := r.Rename; n != nil {
		base.Rename = &EventRename{From: longText(n.From), To: longText(n.To)}
	}
	if d := r.DismissedReview; d != nil {
		message, cut := cleanText(filled(d.DismissalMessage), projectionBodyLimit, true)
		base.DismissedReview = &EventDismissedReview{State: shortText(d.State), ReviewID: d.ReviewID,
			DismissalMessage: message, DismissalMessageTruncated: cut, DismissalCommitID: shortText(d.DismissalCommitID)}
	}
	if c := r.ProjectCard; c != nil {
		base.ProjectCard = &EventProjectCard{ID: c.ID, URL: address(c.URL), ProjectID: c.ProjectID,
			ProjectURL: address(c.ProjectURL), ColumnName: longText(c.ColumnName),
			PreviousColumnName: longText(c.PreviousColumnName)}
	}
	if t := r.RequestedTeam; t != nil {
		team := &EventTeam{RESTTeamSimple: *t.core.simple(), Parent: t.Parent.simple()}
		if p := t.Permissions; p != nil && (p.Pull != nil || p.Triage != nil || p.Push != nil || p.Maintain != nil ||
			p.Admin != nil) {
			team.Permissions = p
		}
		base.RequestedTeam = team
	}
	if link, ok := r.links["commit_url"]; ok {
		base.Links = withoutLink(r.links, "commit_url")
		if repo, found := apiRepository(&link, "commits"); found {
			if !sameRepository(repo, own) {
				name := repo.owner + "/" + repo.repo
				base.CommitRepository = &name
			}
			if allowed.allows(repo) {
				base.Links = withEventLink(base.Links, "commit_url", link)
			}
		}
	}
	return base
}

// project reduces a reference to identifiers unless its repository lies inside the connection's targets.
func (r *eventReferenceRaw) project(allowed allowlist) *EventIssueReference {
	if r == nil || r.Number == nil {
		return nil
	}
	repo := r.Repository
	if repo == nil || repo.FullName == nil {
		return &EventIssueReference{Withheld: true}
	}
	owner, name, _ := strings.Cut(*repo.FullName, "/")
	if !repo.admitted(allowed) || !allowed.allows(target{kind: kindRepository, owner: owner, repo: name}) {
		return &EventIssueReference{Number: r.Number, Withheld: true,
			Repository: &RESTRepository{ID: repo.ID, NodeID: nameText(repo.NodeID), FullName: nameText(repo.FullName)}}
	}
	return &EventIssueReference{Number: r.Number, Title: longText(r.Title), State: shortText(r.State),
		StateReason: shortText(r.StateReason), Repository: repo.project(allowed), IssueType: r.IssueType.detail()}
}

// withEventLink returns a copy of links with key set; links is never modified, since the raw event shares it.
func withEventLink(links Links, key, value string) Links {
	out := Links{key: value}
	for k, v := range links {
		out[k] = v
	}
	return out
}

func (r *timelineRaw) event(op string, own target, allowed allowlist) (IssueEvent, error) {
	event := IssueEvent{IssueEventBase: r.base(own, allowed)}
	issue, _, err := eventIssue(op, r.Issue, own, allowed, true)
	if err != nil {
		return IssueEvent{}, err
	}
	event.Issue = issue
	return event, nil
}

func (r *lineCommentRaw) project() TimelineLineComment {
	w := &r.wide
	width := ReviewCommentWidth{PullRequestReviewID: w.PullRequestReviewID, NodeID: nameText(w.NodeID),
		Position: w.Position, OriginalPosition: w.OriginalPosition, OriginalCommitID: nameText(w.OriginalCommitID),
		OriginalLine: w.OriginalLine, StartLine: w.StartLine, OriginalStartLine: w.OriginalStartLine,
		StartSide: nameText(w.StartSide), SubjectType: nameText(w.SubjectType), AuthorDetail: w.User.project(),
		AuthorAssociation: nameText(w.AuthorAssociation), Reactions: w.Reactions.project(), URL: address(w.HTMLURL),
		Links: w.links}
	width.DiffHunk, width.DiffHunkTruncated = cleanText(w.DiffHunk, projectionBodyLimit, true)
	comment := TimelineLineComment{ID: r.ID, Line: r.Line, Side: nameText(r.Side), CommitID: nameText(r.CommitID),
		InReplyToID: r.InReplyToID, CreatedAt: nameText(r.CreatedAt), UpdatedAt: nameText(r.UpdatedAt),
		ReviewCommentWidth: width}
	comment.IsCopilotCodeReview = r.CopilotReview
	sources, cut := capped(r.CopilotSources)
	for _, source := range sources {
		path, _ := cleanText(source.Path, projectionPathLimit, false)
		comment.CopilotCodeReviewSources = append(comment.CopilotCodeReviewSources,
			CopilotCodeReviewSource{Kind: shortText(source.Kind), Name: longText(source.Name), Path: path})
	}
	comment.CopilotCodeReviewSourcesCut = cut
	comment.Path, _ = cleanText(r.Path, projectionPathLimit, false)
	comment.Body, comment.BodyTruncated = cleanText(r.Body, projectionBodyLimit, true)
	return comment
}

// comments reads the comments of a line-commented or commit-commented event, each as far as it is readable.
func eventComments[T any](data json.RawMessage) ([]T, bool) {
	var items []json.RawMessage
	if json.Unmarshal(data, &items) != nil {
		return nil, false
	}
	items, cut := capped(items)
	out := make([]T, 0, len(items))
	for _, item := range items {
		var comment T
		if json.Unmarshal(item, &comment) == nil {
			out = append(out, comment)
		}
	}
	return out, cut
}

func (r *timelineRaw) timeline(op string, own target, allowed allowlist) (TimelineEvent, error) {
	event := TimelineEvent{IssueEventBase: r.base(own, allowed), UpdatedAt: shortText(r.UpdatedAt),
		SubmittedAt: shortText(r.SubmittedAt), User: r.User.project(), URL: address(r.HTMLURL),
		Reactions: r.Reactions.project(), State: shortText(r.State), StateReason: shortText(r.StateReason),
		SHA: shortText(r.SHA), Author: r.Author.project(), Committer: r.Committer.project(),
		Verification: r.Verification.project()}
	if m := r.Minimized; m != nil {
		event.Minimized = &RESTCommentMinimized{Reason: shortText(m.Reason)}
	}
	if p := r.Pin; p != nil {
		event.Pin = &RESTCommentPin{PinnedAt: shortText(p.PinnedAt), PinnedBy: p.PinnedBy.project()}
	}
	event.Body, event.BodyTruncated = cleanText(filled(r.Body), projectionBodyLimit, true)
	event.Message, event.MessageTruncated = cleanText(filled(r.Message), maxCommitMessageLength, true)
	if t := r.Tree; t != nil {
		event.Tree = &RESTCommitTree{SHA: nameText(t.SHA), Links: t.links}
	}
	parents, _ := capped(r.Parents)
	for _, parent := range parents {
		event.Parents = append(event.Parents, RESTCommitParent{SHA: nameText(parent.SHA), URL: address(parent.HTMLURL),
			Links: parent.links})
	}
	if s := r.Source; s != nil {
		issue, withheld, err := eventIssue(op, s.Issue, own, allowed, false)
		if err != nil {
			return TimelineEvent{}, err
		}
		event.Source = &TimelineSource{Type: shortText(s.Type), Issue: issue, Withheld: withheld}
	}
	switch valueOr(event.Event) {
	case "line-commented":
		raws, cut := eventComments[lineCommentRaw](r.Comments)
		for i := range raws {
			event.LineComments = append(event.LineComments, raws[i].project())
		}
		event.LineCommentsTruncated = cut
	case "commit-commented":
		raws, cut := eventComments[commitCommentRaw](r.Comments)
		for i := range raws {
			event.CommitComments = append(event.CommitComments, *raws[i].project())
		}
		event.CommitCommentsTruncated = cut
	}
	return event, nil
}

// ---- Arguments and handlers

// issueHistoryArguments holds the arguments of every tool of this file; each input schema admits only its
// own. page, perPage, binding, and allowed are derived by the checks.
type issueHistoryArguments struct {
	Number   int      `json:"number"`
	Assignee string   `json:"assignee"`
	Exclude  []string `json:"exclude"`
	Limit    int      `json:"limit"`
	Cursor   string   `json:"cursor"`

	page, perPage int
	binding       []byte
	allowed       allowlist
}

func (a *issueHistoryArguments) query() url.Values {
	query := url.Values{"per_page": {strconv.Itoa(a.perPage)}, "page": {strconv.Itoa(a.page)}}
	if len(a.Exclude) > 0 {
		query.Set("exclude", strings.Join(a.Exclude, ","))
	}
	return query
}

// paging binds the cursor to the tool, the repository, and the issue and reads the requested page.
func (a *issueHistoryArguments) paging(tool string, bound target) error {
	limit, err := normalizeLimit(a.Limit)
	if err != nil {
		return err
	}
	a.binding = fingerprint(tool, bound.String(), a.Number, a.Exclude)
	a.page, a.perPage, err = pageOf(a.binding, a.Cursor, limit)
	return err
}

func checkIssueEvents(a *issueHistoryArguments, bound target) error {
	if a.Number != 0 {
		if err := checkNumber(a.Number); err != nil {
			return err
		}
	}
	return a.paging("issue-events", bound)
}

// maxTimelineExcludes bounds the event names one timeline request may exclude.
const maxTimelineExcludes = 50

func checkIssueTimeline(a *issueHistoryArguments, bound target) error {
	if err := checkNumber(a.Number); err != nil {
		return err
	}
	if len(a.Exclude) > maxTimelineExcludes {
		return invalidRequest(fmt.Sprintf("exclude names at most %d event types", maxTimelineExcludes))
	}
	for _, name := range a.Exclude {
		if !validEventName(name) {
			return invalidRequest("exclude names event types by their lower-case GitHub name, such as commented")
		}
	}
	return a.paging("issue-timeline", bound)
}

// validEventName accepts the shape of a GitHub timeline event name: lower-case words joined by - or _.
func validEventName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for i, r := range name {
		letter := r >= 'a' && r <= 'z'
		if !letter && (i == 0 || (r != '-' && r != '_' && (r < '0' || r > '9'))) {
			return false
		}
	}
	return true
}

func checkAssigneesList(a *issueHistoryArguments, bound target) error {
	return a.paging("assignees", bound)
}

func checkAssigneesCheck(a *issueHistoryArguments, _ target) error {
	if !validLogin(a.Assignee) {
		return invalidRequest("assignee must be a GitHub login")
	}
	return nil
}

// issueHistoryHandler decodes and checks the arguments and the bound repository before a credential is
// resolved, so a repository outside the connection's targets is refused before any secret access.
func issueHistoryHandler(id string, check func(*issueHistoryArguments, target) error,
	call func(context.Context, *Client, *issueHistoryArguments) (any, error)) capability.Handler {
	return func(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
		raw json.RawMessage) (any, error) {
		var arguments issueHistoryArguments
		if err := json.Unmarshal(raw, &arguments); err != nil {
			return nil, unreadable(id)
		}
		bound, err := selectTarget(resolved, kindRepository, raw)
		if err != nil {
			return nil, err
		}
		if arguments.allowed, err = allowlistOf(resolved); err != nil {
			return nil, providerError("open", err.Error())
		}
		if err := check(&arguments, bound); err != nil {
			return nil, err
		}
		client, err := openAt(ctx, resolved, secrets, red, bound)
		if err != nil {
			return nil, err
		}
		return bound.locate(call(ctx, client, &arguments))
	}
}

func issueHistoryOperations() []capability.Operation {
	return []capability.Operation{
		{Descriptor: issueEventsList, Handler: issueHistoryHandler(issueEventsList.ID, checkIssueEvents,
			func(ctx context.Context, c *Client, a *issueHistoryArguments) (any, error) {
				return c.listIssueEvents(ctx, a)
			})},
		{Descriptor: issueTimelineList, Handler: issueHistoryHandler(issueTimelineList.ID, checkIssueTimeline,
			func(ctx context.Context, c *Client, a *issueHistoryArguments) (any, error) {
				return c.listIssueTimeline(ctx, a)
			})},
		{Descriptor: assigneesList, Handler: issueHistoryHandler(assigneesList.ID, checkAssigneesList,
			func(ctx context.Context, c *Client, a *issueHistoryArguments) (any, error) {
				return c.listAssignees(ctx, a)
			})},
		{Descriptor: assigneesCheck, Handler: issueHistoryHandler(assigneesCheck.ID, checkAssigneesCheck,
			func(ctx context.Context, c *Client, a *issueHistoryArguments) (any, error) {
				return c.checkAssignee(ctx, a)
			})},
	}
}

// ---- Reads

func (c *Client) listIssueEvents(ctx context.Context, a *issueHistoryArguments) (*IssueEventList, error) {
	const op = "list issue events"
	path := issuesPath(c.target) + "/events"
	if a.Number != 0 {
		path = issuePath(c.target, a.Number) + "/events"
	}
	var raw []timelineRaw
	hasNext, err := c.restPage(ctx, op, path, a.query(), &raw)
	if err != nil {
		return nil, actionsFailure(err, issueEventsReadPermission)
	}
	result := &IssueEventList{Events: make([]IssueEvent, 0, len(raw))}
	for i := range raw {
		event, err := raw[i].event(op, c.target, a.allowed)
		if err != nil {
			return nil, err
		}
		result.Events = append(result.Events, event)
	}
	result.HasMore, result.NextCursor = morePage(a.binding, a.page, a.perPage, hasNext)
	return result, nil
}

func (c *Client) listIssueTimeline(ctx context.Context, a *issueHistoryArguments) (*IssueTimelineList, error) {
	const op = "list issue timeline"
	var raw []timelineRaw
	hasNext, err := c.restPage(ctx, op, issuePath(c.target, a.Number)+"/timeline", a.query(), &raw)
	if err != nil {
		return nil, actionsFailure(err, issueTimelineReadPermission)
	}
	result := &IssueTimelineList{Events: make([]TimelineEvent, 0, len(raw))}
	for i := range raw {
		event, err := raw[i].timeline(op, c.target, a.allowed)
		if err != nil {
			return nil, err
		}
		result.Events = append(result.Events, event)
	}
	result.HasMore, result.NextCursor = morePage(a.binding, a.page, a.perPage, hasNext)
	return result, nil
}

func (c *Client) listAssignees(ctx context.Context, a *issueHistoryArguments) (*AssigneeList, error) {
	const op = "list assignees"
	var raw []accountRaw
	hasNext, err := c.restPage(ctx, op, c.repoPath("assignees"), a.query(), &raw)
	if err != nil {
		return nil, actionsFailure(err, assigneesReadPermission)
	}
	result := &AssigneeList{Assignees: make([]RESTAccount, 0, len(raw))}
	for i := range raw {
		if raw[i].Login == nil || *raw[i].Login == "" {
			return nil, invalidEntry(op, "an assignee")
		}
		result.Assignees = append(result.Assignees, *raw[i].project())
	}
	result.HasMore, result.NextCursor = morePage(a.binding, a.page, a.perPage, hasNext)
	return result, nil
}

// checkAssignee asks GitHub whether a login may be assigned. Only 204 means yes and a 404 means no; GitHub
// answers 404 as well for a repository the token cannot see, so the answer is not told apart.
func (c *Client) checkAssignee(ctx context.Context, a *issueHistoryArguments) (*AssigneeCheck, error) {
	const op = "check assignee"
	var status int
	err := c.do(ctx, op, http.MethodGet, c.endpoints.rest+c.repoPath("assignees/"+url.PathEscape(a.Assignee)), nil,
		nil, false, nil, &status)
	var failure *provider.Error
	switch {
	case err == nil && status == http.StatusNoContent:
		return &AssigneeCheck{Assignee: a.Assignee, Assignable: true}, nil
	case err == nil:
		return nil, invalidResponse(op, false)
	case errors.As(err, &failure) && failure.Class == provider.ClassNotFound:
		return &AssigneeCheck{Assignee: a.Assignee}, nil
	}
	return nil, actionsFailure(err, assigneesReadPermission)
}

// ---- Descriptors

var (
	eventLabelSchema     = objectSchema(`"name":{"type":"string"},"color":{"type":"string"}`)
	eventMilestoneSchema = objectSchema(`"title":{"type":"string"}`)
	eventRenameSchema    = objectSchema(`"from":{"type":"string"},"to":{"type":"string"}`)
	eventDismissedSchema = objectSchema(`"state":{"type":"string"},"review_id":{"type":"integer"},` +
		`"dismissal_message":{"type":"string"},"dismissal_message_truncated":{"type":"boolean"},` +
		`"dismissal_commit_id":{"type":"string"}`)
	eventProjectCardSchema = objectSchema(`"id":{"type":"integer"},"url":{"type":"string"},` +
		`"project_id":{"type":"integer"},"project_url":{"type":"string"},"column_name":{"type":"string"},` +
		`"previous_column_name":{"type":"string"}`)
	eventTeamSchema = strings.TrimSuffix(teamSimpleSchema, `},"additionalProperties":false}`) +
		`,"permissions":` + teamPermissionsSchema + `,"parent":` + teamSimpleSchema + `},"additionalProperties":false}`
	eventIssueSchema = `{"type":"object","properties":{` + issueEntryProperties +
		`,"is_pull_request":{"type":"boolean"},"pull_request":` + objectSchema(`"merged_at":{"type":"string"},`+
		`"url":{"type":"string"},"links":`+linksSchema([]string{"url", "diff_url", "patch_url"})) + `},` +
		issueRefRequired + `}`
	eventLinksSchema  = linksSchema(eventLinkKeys)
	eventIntentSchema = objectSchema(`"rationale":{"type":"string"},"rationale_truncated":{"type":"boolean"},` +
		`"confidence":{"type":"string"}`)
	eventReferenceSchema = objectSchema(`"number":{"type":"integer"},"title":{"type":"string"},` +
		`"state":{"type":"string"},"state_reason":{"type":"string"},"repository":` + restRepositorySchema +
		`,"issue_type":` + objectSchema(issueTypeProperties) + `,"withheld":{"type":"boolean"}`)
)

var issueEventBaseProperties = `"id":{"type":"integer"},"node_id":{"type":"string"},"event":{"type":"string"},` +
	`"actor":` + restAccountSchema + `,"created_at":{"type":"string"},"commit_id":{"type":"string"},` +
	`"commit_repository":{"type":"string"},"label":` + eventLabelSchema + `,"assignee":` + restAccountSchema +
	`,"assigner":` + restAccountSchema + `,"review_requester":` + restAccountSchema + `,"requested_reviewer":` +
	restAccountSchema + `,"requested_team":` + eventTeamSchema + `,"dismissed_review":` + eventDismissedSchema +
	`,"milestone":` + eventMilestoneSchema + `,"project_card":` + eventProjectCardSchema + `,"rename":` +
	eventRenameSchema + `,"lock_reason":{"type":"string"},"author_association":{"type":"string"},` +
	`"performed_via_github_app":` + restAppSchema + `,"intent":` + eventIntentSchema +
	`,"issue_type":` + objectSchema(issueTypeProperties) + `,"prev_issue_type":` + objectSchema(issueTypeProperties) +
	`,"sub_issue":` + eventReferenceSchema + `,"parent_issue":` + eventReferenceSchema + `,"blocked_by":` +
	eventReferenceSchema + `,"blocking":` + eventReferenceSchema + `,"links":` + eventLinksSchema

var issueEventProperties = issueEventBaseProperties + `,"issue":` + eventIssueSchema

var lineCommentSchema = objectSchema(`"id":{"type":"integer"},"path":{"type":"string"},"line":{"type":"integer"},` +
	`"side":{"type":"string"},"commit_id":{"type":"string"},"in_reply_to_id":{"type":"integer"},` +
	`"body":{"type":"string"},"body_truncated":{"type":"boolean"},"created_at":{"type":"string"},` +
	`"updated_at":{"type":"string"},"is_copilot_code_review":{"type":"boolean"},` +
	`"copilot_code_review_sources":{"type":"array","maxItems":100,"items":` +
	objectSchema(`"kind":{"type":"string"},"name":{"type":"string"},"path":{"type":"string"}`) + `},` +
	`"copilot_code_review_sources_truncated":{"type":"boolean"},` + reviewCommentWidthProperties)

var timelineEventProperties = issueEventBaseProperties + `,"updated_at":{"type":"string"},` +
	`"submitted_at":{"type":"string"},"body":{"type":"string"},"body_truncated":{"type":"boolean"},` +
	`"user":` + restAccountSchema + `,"url":{"type":"string"},"reactions":` + restReactionsSchema + `,` +
	`"minimized":` + objectSchema(`"reason":{"type":"string"}`) + `,"pin":` +
	objectSchema(`"pinned_at":{"type":"string"},"pinned_by":`+restAccountSchema) + `,` +
	`"state":{"type":"string"},"state_reason":{"type":"string"},"source":` +
	objectSchema(`"type":{"type":"string"},"issue":`+eventIssueSchema+`,"withheld":{"type":"boolean"}`) + `,` +
	`"sha":{"type":"string"},"author":` + restCommitIdentitySchema + `,"committer":` + restCommitIdentitySchema +
	`,"message":{"type":"string"},"message_truncated":{"type":"boolean"},"tree":` +
	objectSchema(`"sha":{"type":"string"},"links":`+linksSchema(restCommitTreeLinkKeys)) +
	`,"parents":` + restCommitParentsSchema + `,"verification":` + restCommitVerificationSchema +
	`,"line_comments":{"type":"array","maxItems":100,"items":` + lineCommentSchema + `},` +
	`"line_comments_truncated":{"type":"boolean"},"commit_comments":{"type":"array","maxItems":100,"items":` +
	objectSchema(restCommitCommentProperties) + `},"commit_comments_truncated":{"type":"boolean"}`

const timelineExcludeSchema = `{"type":"array","maxItems":50,"uniqueItems":true,"items":{"type":"string",` +
	`"minLength":1,"maxLength":64,"pattern":"^[a-z][a-z0-9_-]*$"}}`

const eventEntryRequired = `"additionalProperties":false`

// issueEventBaseFields describe IssueEventBase.
var issueEventBaseFields = []capability.Field{
	{Name: "id", Description: "Numeric id of the event"},
	{Name: "node_id", Description: "GraphQL node id of the event"},
	{Name: "event", Description: "Event type such as labeled, assigned, closed, referenced, or cross-referenced; " +
		"GitHub adds types over time, and an event of a type this tool does not know keeps its type and the " +
		"members every event shares, untrusted data"},
	{Name: "actor", Description: "Account that caused the event: " + restAccountMembers},
	{Name: "created_at", Description: "When the event happened"},
	{Name: "commit_id", Description: "SHA of the commit the event refers to, such as the one that closed the issue"},
	{Name: "commit_repository", Description: "Repository of that commit as OWNER/REPO, only when it differs from the " +
		"repository of the request; a commit outside the connection's targets keeps this and commit_id and loses " +
		"its address"},
	{Name: "label", Description: "name and color of the label of a labeled or unlabeled event, untrusted data"},
	{Name: "assignee", Description: "Account that was assigned or unassigned: " + restAccountMembers},
	{Name: "assigner", Description: "Account that assigned: " + restAccountMembers},
	{Name: "review_requester", Description: "Account that requested a review: " + restAccountMembers},
	{Name: "requested_reviewer", Description: "Account whose review was requested or whose request was removed: " +
		restAccountMembers},
	{Name: "requested_team", Description: "Team whose review was requested, with the members of a team " +
		"(id, node_id, name, slug, description, privacy, notification_setting, permission, type, " +
		"organization_id, enterprise_id, ldap_dn, url, links), permissions, and its parent team; untrusted data"},
	{Name: "dismissed_review", Description: "Review a review-dismissed event dismissed: state, review_id, " +
		"dismissal_message (cut at 4000 characters, dismissal_message_truncated says so, untrusted data), and " +
		"dismissal_commit_id"},
	{Name: "milestone", Description: "title of the milestone of a milestoned or demilestoned event, untrusted data"},
	{Name: "project_card", Description: "Classic project card of a project event: id, url, project_id, project_url, " +
		"column_name, and previous_column_name; untrusted data, addresses never fetched"},
	{Name: "rename", Description: "from and to title of a renamed event, untrusted data"},
	{Name: "lock_reason", Description: "Why the conversation was locked, of a locked event"},
	{Name: "author_association", Description: "How the actor relates to the repository, such as OWNER or MEMBER"},
	{Name: "performed_via_github_app", Description: "App the event was made through: " + restAppMembers},
	{Name: "intent", Description: "Intent an agent gave for the change of a labeled, unlabeled, assigned, " +
		"state-change, or issue type event: rationale (cut at 1000 characters, rationale_truncated says so, " +
		"untrusted data) and confidence (LOW, MEDIUM, or HIGH); only while GitHub's issue suggestions are on"},
	{Name: "issue_type", Description: "Issue type of an issue-type event: id, name, and color, untrusted data"},
	{Name: "prev_issue_type", Description: "Issue type before an issue-type-changed event: id, name, and color, " +
		"untrusted data"},
	{Name: "sub_issue", Description: refDescription("sub-issue added or removed")},
	{Name: "parent_issue", Description: refDescription("parent issue added or removed")},
	{Name: "blocked_by", Description: refDescription("blocked-by dependency added or removed")},
	{Name: "blocking", Description: refDescription("blocking dependency added or removed")},
	{Name: "links", Description: "REST url, issue_url, and pull_request_url of the event and, inside the " +
		"connection's targets, the commit_url of its commit, by their GitHub names, untrusted data, never fetched"},
}

// refDescription describes the issue reference of one event type.
func refDescription(what string) string {
	return "Issue of a " + what + " event: number, title, state, state_reason, repository as the members of a " +
		"repository, and issue_type (id, node_id, name, color); untrusted data. Outside the connection's " +
		"targets only number and the id, node_id, and full_name of repository are set, with withheld; when " +
		"the repository cannot be told, withheld alone"
}

var issueEventFields = append(append([]capability.Field{
	{Name: "events", Description: "Events, oldest first, untrusted data, with the members below; only the " +
		"members the event type carries are set"},
}, issueEventBaseFields...), capability.Field{Name: "issue", Description: "Issue of the event, only in the list of " +
	"the whole repository: the members of an issue entry as github.sub-issues.list answers them, plus " +
	"is_pull_request and, for a pull request, pull_request (merged_at, url, links); outside the connection's " +
	"targets only number, repository, and is_pull_request"})

var timelineEventFields = append(append([]capability.Field{
	{Name: "events", Description: "Timeline events, oldest first, untrusted data, with the members below; only " +
		"the members the event type carries are set"},
}, issueEventBaseFields...), []capability.Field{
	{Name: "updated_at", Description: "When the comment or the cross-reference last changed"},
	{Name: "submitted_at", Description: "When the review was submitted"},
	{Name: "body", Description: "Text of a comment or review, cut at 4000 characters (body_truncated says so), " +
		"untrusted data; the rendered variants are left out"},
	{Name: "body_truncated", Description: "True when the body was cut"},
	{Name: "user", Description: "Author of a comment or review as an account: " + restAccountMembers},
	{Name: "url", Description: "Web address of the comment, review, or commit, untrusted data, never fetched"},
	{Name: "reactions", Description: "Reaction counts of a comment: " + restReactionsMembers},
	{Name: "minimized", Description: "reason a comment was minimized, untrusted data"},
	{Name: "pin", Description: "pinned_at and pinned_by (an account) of a pinned comment"},
	{Name: "state", Description: "State of a review, such as approved or changes_requested"},
	{Name: "state_reason", Description: "Why the issue was closed or reopened, such as completed or not_planned"},
	{Name: "source", Description: "Source of a cross-referenced event: type, the issue as the members of an issue " +
		"entry plus is_pull_request, and withheld; an issue outside the connection's targets, or of an unknown " +
		"repository, is withheld and keeps only number, repository, and is_pull_request, or nothing"},
	{Name: "sha", Description: "SHA of the commit of a committed event"},
	{Name: "author", Description: "Git author of a committed event: name, email, and date, untrusted data"},
	{Name: "committer", Description: "Git committer of a committed event: name, email, and date, untrusted data"},
	{Name: "message", Description: "Commit message of a committed event, cut at 4096 characters " +
		"(message_truncated says so), untrusted data"},
	{Name: "message_truncated", Description: "True when the commit message was cut"},
	{Name: "tree", Description: "Tree of a committed event: sha and links"},
	{Name: "parents", Description: "Parents of a committed event, at most 100: sha, url, and links"},
	{Name: "verification", Description: "Signature verification of a committed event: verified, reason, " +
		"verified_at, payload, and signature, the last two cut at 4096 characters"},
	{Name: "line_comments", Description: "Pull request line comments of a line-commented event, at most 100: id, " +
		"path, line, side, commit_id, in_reply_to_id, body (cut, body_truncated says so), created_at, updated_at, " +
		"is_copilot_code_review, copilot_code_review_sources (at most 100: kind, name, path), then the members of a " +
		"line comment as github.pull-request-review-comments.list answers them; untrusted data"},
	{Name: "line_comments_truncated", Description: "True when the event holds more line comments than shown"},
	{Name: "commit_comments", Description: "Commit comments of a commit-commented event, at most 100, with the " +
		"members of a commit comment as github.commit-comments.list answers them; untrusted data"},
	{Name: "commit_comments_truncated", Description: "True when the event holds more commit comments than shown"},
}...)

var issueEventsList = capability.Descriptor{
	ID:      Provider + ".issue-events.list",
	Version: 1,
	Title:   "List GitHub issue events",
	Description: "List one bounded batch of the events of one issue or pull request of a repository an " +
		"explicit connection allows, or, without a number, of every issue and pull request of the repository, " +
		"oldest first; a commit an event names outside the connection's targets keeps only its SHA and repository",
	Tags:         []string{"github", "issues", "events", "list"},
	Risk:         readRisk,
	Provider:     Provider,
	InputSchema:  inputSchema(`"number":` + numberSchema + `,` + pagingKeys),
	OutputSchema: listOutput("events", issueEventProperties, eventEntryRequired),
	Arguments: append([]capability.Argument{
		{Name: "number", Description: "Issue or pull request number in the repository; the events of every issue " +
			"of the repository when omitted"},
	}, pagingArguments...),
	Fields: append(issueEventFields, pagingFields...),
	Examples: []capability.Example{{
		Description: "List the events of one issue",
		Arguments:   json.RawMessage(`{"number":42}`),
	}},
}

var issueTimelineList = capability.Descriptor{
	ID:      Provider + ".issue-timeline.list",
	Version: 1,
	Title:   "List GitHub issue timeline",
	Description: "List one bounded batch of the timeline of one issue or pull request of a repository an " +
		"explicit connection allows, oldest first: comments, reviews, commits, state changes, and references; " +
		"an issue or commit it names outside the connection's targets is reduced to identifiers, and texts are " +
		"cut and untrusted",
	Tags:         []string{"github", "issues", "timeline", "list"},
	Risk:         readRisk,
	Provider:     Provider,
	InputSchema:  inputSchema(`"number":`+numberSchema+`,"exclude":`+timelineExcludeSchema+`,`+pagingKeys, "number"),
	OutputSchema: listOutput("events", timelineEventProperties, eventEntryRequired),
	Arguments: append([]capability.Argument{
		{Name: "number", Description: "Issue or pull request number in the repository", Required: true},
		{Name: "exclude", Description: "Timeline event types to leave out, at most 50, each by its lower-case " +
			"GitHub name such as commented or cross-referenced; sent to GitHub as one comma-separated list, and " +
			"part of what a cursor is bound to"},
	}, pagingArguments...),
	Fields: append(timelineEventFields, pagingFields...),
	Examples: []capability.Example{{
		Description: "List the timeline of one issue",
		Arguments:   json.RawMessage(`{"number":42}`),
	}},
}

var assigneesList = capability.Descriptor{
	ID:      Provider + ".assignees.list",
	Version: 1,
	Title:   "List GitHub assignable users",
	Description: "List one bounded batch of the users that may be assigned to issues and pull requests of a " +
		"repository an explicit connection allows",
	Tags:         []string{"github", "issues", "assignees", "list"},
	Risk:         readRisk,
	Provider:     Provider,
	InputSchema:  inputSchema(pagingKeys),
	OutputSchema: listOutput("assignees", restAccountProperties, eventEntryRequired),
	Arguments:    pagingArguments,
	Fields: append(append([]capability.Field{
		{Name: "assignees", Description: "Assignable users with the members of an account: " + restAccountMembers},
	}, restAccountFields...), pagingFields...),
	Examples: []capability.Example{{Description: "List the assignable users", Arguments: json.RawMessage(`{}`)}},
}

var assigneesCheck = capability.Descriptor{
	ID:      Provider + ".assignees.check",
	Version: 1,
	Title:   "Check a GitHub assignee",
	Description: "Check whether one login may be assigned to issues and pull requests of a repository an " +
		"explicit connection allows; a login that may not be assigned is an answer, not an error",
	Tags:        []string{"github", "issues", "assignees", "check"},
	Risk:        readRisk,
	Provider:    Provider,
	InputSchema: inputSchema(`"assignee":{"type":"string","minLength":1,"maxLength":100,"pattern":"`+loginPattern+`"}`, "assignee"),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"assignee":{"type":"string"},` +
		`"assignable":{"type":"boolean"}},"required":["assignee","assignable"],"additionalProperties":false}`),
	Arguments: []capability.Argument{
		{Name: "assignee", Description: "GitHub login to check", Required: true},
	},
	Fields: []capability.Field{
		{Name: "assignee", Description: "The login that was checked"},
		{Name: "assignable", Description: "True when GitHub answered 204; false when it answered 404, which it also " +
			"does for a repository this token cannot see"},
	},
	Examples: []capability.Example{{Description: "Check one login", Arguments: json.RawMessage(`{"assignee":"octocat"}`)}},
}
