package github

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
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

// The social account tools read and change who follows the account behind the connection's token, whom it
// follows, whom it blocks, and its public profile. They name no repository, project, or owner. Like the gist
// tools they act for a user account: a connection without targets reaches what its token reaches; a
// connection with targets needs users/LOGIN targets and no repository and no project target, and the account
// behind the token must be one of those users. A follower or following list of another user needs that user
// as a target as well. The person a follow or a block names is checked against the exclusions before any
// secret is read. Names, bios, companies, locations, and addresses are untrusted data and addresses are
// never fetched. Every change is read once to report whether it changed anything, then sent once and never
// repeated.
//
// The lists answer simple-user objects (200), the follow and block routes answer 204 when the state holds and
// 404 when it does not, PUT and DELETE answer 204, and PATCH /user answers the private user. Verified
// 2026-11-14 per
// https://docs.github.com/en/rest/authentication/permissions-required-for-fine-grained-personal-access-tokens
// that a fine-grained token needs Followers (read, write), Block another user (read, write), or Profile (write).

const (
	blockSensitivity   = "github-account-block"
	profileSensitivity = "github-account-profile"
	accountLoginSchema = `{"type":"string","maxLength":100,"pattern":"` + loginPattern + `"}`
)

const (
	followersReadPermission = "GitHub refused this token the followers of this account; reading them needs no " +
		"scope on a classic token for public data, or Followers: read on a fine-grained token"
	followingChangePermission = "GitHub refused this change of a follow; it needs user:follow on a classic " +
		"token, or Followers: read and write on a fine-grained token"
	blocksReadPermission = "GitHub refused this token the blocked users of this account; reading them needs " +
		"the user scope on a classic token, or Block another user: read on a fine-grained token"
	blocksChangePermission = "GitHub refused this change of a block; it needs the user scope on a classic " +
		"token, or Block another user: read and write on a fine-grained token"
	profileChangePermission = "GitHub refused this change of the profile; it needs the user scope on a classic " +
		"token, or Profile: write on a fine-grained token"
)

var socialAccountProperties = `"login":{"type":"string"},` + accountPersonProperties + `,"type":{"type":"string"},` +
	`"starred_at":{"type":"string"},"url":{"type":"string"}`

const socialAccountRequired = `"required":["login"],"additionalProperties":false`

var socialAccountFields = append([]capability.Field{
	{Name: "type", Description: "Account type, untrusted data"},
	{Name: "starred_at", Description: "Star time GitHub reports for the account, untrusted data"},
	{Name: "url", Description: "Web address of the account, untrusted data, never fetched"},
}, accountPersonFields...)

var socialListArguments = append([]capability.Argument{
	{Name: "username", Description: "Login of another user; the account behind the token when omitted; with " +
		"targets it must be a user target"},
}, pagingArguments...)

func socialList(id, title, description, key, keyDescription string, tags ...string) capability.Descriptor {
	return capability.Descriptor{
		ID:           Provider + "." + id,
		Version:      1,
		Title:        title,
		Description:  description,
		Tags:         append([]string{"github", "account", "social", "list"}, tags...),
		Risk:         readRisk,
		Provider:     Provider,
		InputSchema:  inputSchema(`"username":` + accountLoginSchema + `,` + pagingKeys),
		OutputSchema: listOutput(key, socialAccountProperties, socialAccountRequired),
		Arguments:    socialListArguments,
		Fields: append(append([]capability.Field{{Name: key, Description: keyDescription}}, socialAccountFields...),
			pagingFields...),
		Examples: []capability.Example{{Description: title, Arguments: json.RawMessage(`{}`)}},
	}
}

var followersList = socialList("followers.list", "List the followers of a GitHub account",
	"List one bounded batch of the followers of the account behind the connection's token, or of one user, in "+
		"the order GitHub returns them; a connection with targets needs a user target users/LOGIN for the "+
		"account or the user listed and no repository or project target",
	"followers", "Followers by login, with the members of GitHub's simple user object, including a person's "+
		"name and email address; untrusted data", "followers")

var followingList = socialList("following.list", "List the accounts a GitHub account follows",
	"List one bounded batch of the accounts the account behind the connection's token follows, or one user "+
		"follows, in the order GitHub returns them; a connection with targets needs a user target users/LOGIN "+
		"for the account or the user listed and no repository or project target",
	"following", "Followed accounts by login, with the members of GitHub's simple user object, including a "+
		"person's name and email address; untrusted data", "following")

var blocksList = capability.Descriptor{
	ID:      Provider + ".blocks.list",
	Version: 1,
	Title:   "List the users a GitHub account blocks",
	Description: "List one bounded batch of the users the account behind the connection's token blocks, in the " +
		"order GitHub returns them; a connection with targets needs the account as a user target users/LOGIN " +
		"and no repository or project target",
	Tags:         []string{"github", "account", "blocks", "list"},
	Risk:         readRisk,
	Provider:     Provider,
	InputSchema:  inputSchema(pagingKeys),
	OutputSchema: listOutput("blocked", socialAccountProperties, socialAccountRequired),
	Arguments:    pagingArguments,
	Fields: append(append([]capability.Field{{Name: "blocked", Description: "Blocked users by login, with the " +
		"members of GitHub's simple user object, including a person's name and email address; untrusted data"}},
		socialAccountFields...), pagingFields...),
	Examples: []capability.Example{{Description: "List the blocked users", Arguments: json.RawMessage(`{}`)}},
}

const followChangeProperties = `"username":{"type":"string"},"following":{"type":"boolean"},"changed":{"type":"boolean"}`

var followingSet = capability.Descriptor{
	ID:      Provider + ".following.set",
	Version: 1,
	Title:   "Follow or unfollow a GitHub user",
	Description: "Follow one user, or stop following one, for the account behind the connection's token; an " +
		"account already in the requested state is left as it is and reported without a change request",
	Tags:        []string{"github", "account", "social", "following", "set"},
	Risk:        changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
	Provider:    Provider,
	InputSchema: inputSchema(`"username":`+accountLoginSchema+`,"following":{"type":"boolean"}`, "username", "following"),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{` + followChangeProperties + `},` +
		`"required":["username","following","changed"],"additionalProperties":false}`),
	Arguments: []capability.Argument{
		{Name: "username", Description: "Login of the user", Required: true},
		{Name: "following", Description: "true to follow the user, false to stop following", Required: true},
	},
	Fields: []capability.Field{
		{Name: "username", Description: "The user the call named"},
		{Name: "following", Description: "Whether the account follows the user now"},
		{Name: "changed", Description: "True when this call changed the follow; false when it already held"},
	},
	Examples: []capability.Example{{Description: "Follow a user",
		Arguments: json.RawMessage(`{"username":"octocat","following":true}`)}},
}

const blockChangeProperties = `"username":{"type":"string"},"blocked":{"type":"boolean"},"changed":{"type":"boolean"}`

func blockTool(id, title, description string, effect capability.Effect, blocked bool) capability.Descriptor {
	verb := "remove"
	state := "False once the user is not blocked"
	if blocked {
		verb = "add"
		state = "True once the user is blocked"
	}
	return capability.Descriptor{
		ID:                    Provider + "." + id,
		Version:               1,
		Title:                 title,
		Description:           description,
		Tags:                  []string{"github", "account", "blocks", verb},
		Risk:                  guardedRisk(effect, capability.IdempotencyIdempotent, blockSensitivity),
		Provider:              Provider,
		RequiresToolAllowList: true,
		InputSchema:           inputSchema(`"username":`+accountLoginSchema, "username"),
		OutputSchema: json.RawMessage(`{"type":"object","properties":{` + blockChangeProperties + `},` +
			`"required":["username","blocked","changed"],"additionalProperties":false}`),
		Arguments: []capability.Argument{{Name: "username", Description: "Login of the user", Required: true}},
		Fields: []capability.Field{
			{Name: "username", Description: "The user the call named"},
			{Name: "blocked", Description: state},
			{Name: "changed", Description: "True when this call changed the block; false when it already held"},
		},
		Examples: []capability.Example{{Description: title, Arguments: json.RawMessage(`{"username":"octocat"}`)}},
	}
}

var blocksAdd = blockTool("blocks.add", "Block a GitHub user",
	"Block one user for the account behind the connection's token; offered only where a connection's tools "+
		"list names it; a user already blocked is left as it is and reported without a change request",
	capability.EffectUpdate, true)

var blocksRemove = blockTool("blocks.remove", "Unblock a GitHub user",
	"Unblock one user for the account behind the connection's token; offered only where a connection's tools "+
		"list names it; a user not blocked is left as it is and reported without a change request",
	capability.EffectDelete, false)

const profileTextLimit = 255

const profileBioLimit = 500

var profileKeys = `"name":` + profileTextSchema(profileTextLimit) + `,"email":` + profileTextSchema(profileTextLimit) +
	`,"blog":` + profileTextSchema(profileTextLimit) + `,"twitter_username":` + profileTextSchema(profileTextLimit) +
	`,"company":` + profileTextSchema(profileTextLimit) + `,"location":` + profileTextSchema(profileTextLimit) +
	`,"hireable":{"type":"boolean"},"bio":` + profileTextSchema(profileBioLimit)

func profileTextSchema(limit int) string {
	return `{"type":"string","maxLength":` + strconv.Itoa(limit) + `}`
}

var profileUpdate = capability.Descriptor{
	ID:      Provider + ".profile.update",
	Version: 1,
	Title:   "Update the GitHub profile",
	Description: "Change the public profile of the account behind the connection's token: name, public email, " +
		"blog address, Twitter or X username, company, location, hireable, and bio; an empty text clears the " +
		"member and an omitted member stays as it is; offered only where a connection's tools list names it",
	Tags:                  []string{"github", "account", "profile", "update"},
	Risk:                  guardedRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, profileSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema:           inputSchema(profileKeys),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"login":{"type":"string"},"name":{"type":"string"},` +
		`"email":{"type":"string"},"twitter_username":{"type":"string"},` + accountProfileProperties + `},` +
		`"required":["login"],"additionalProperties":false}`),
	Arguments: []capability.Argument{
		{Name: "name", Description: "Display name, at most 255 characters"},
		{Name: "email", Description: "Publicly visible email address, at most 255 characters; GitHub checks it"},
		{Name: "blog", Description: "Blog or website address, stored as given and never fetched, at most 255 characters"},
		{Name: "twitter_username", Description: "Twitter or X username, at most 255 characters"},
		{Name: "company", Description: "Company, at most 255 characters"},
		{Name: "location", Description: "Location, at most 255 characters"},
		{Name: "hireable", Description: "Whether the account marks itself as available for hire"},
		{Name: "bio", Description: "Profile text, at most 500 characters"},
	},
	Fields: append([]capability.Field{
		{Name: "login", Description: "Login of the account"},
		{Name: "name", Description: "Display name now, untrusted data"},
		{Name: "email", Description: "Public email address now, untrusted data"},
		{Name: "twitter_username", Description: "Twitter or X username now, untrusted data"},
	}, accountProfileFields...),
	Examples: []capability.Example{{Description: "Change the bio and the location",
		Arguments: json.RawMessage(`{"bio":"Maintainer","location":"Berlin"}`)}},
}

var accountSocialToolIDs = []string{followersList.ID, followingList.ID, followingSet.ID, blocksList.ID,
	blocksAdd.ID, blocksRemove.ID, profileUpdate.ID}

func accountSocialOperations() []capability.Operation {
	return []capability.Operation{
		{Descriptor: followersList, Handler: capability.Handler(invokeSocialList("followers"))},
		{Descriptor: followingList, Handler: capability.Handler(invokeSocialList("following"))},
		{Descriptor: followingSet, Handler: capability.Handler(invokeFollowingSet)},
		{Descriptor: blocksList, Handler: capability.Handler(invokeBlocksList)},
		{Descriptor: blocksAdd, Handler: capability.Handler(invokeBlocksChange(true))},
		{Descriptor: blocksRemove, Handler: capability.Handler(invokeBlocksChange(false))},
		{Descriptor: profileUpdate, Handler: capability.Handler(invokeProfileUpdate)},
	}
}

// SocialAccount is one account of a follower, following, or block list.
type SocialAccount struct {
	Login string `json:"login"`
	AccountPerson
	Type      *string `json:"type,omitempty"`
	StarredAt *string `json:"starred_at,omitempty"`
	URL       *string `json:"url,omitempty"`
}

// FollowerList is one batch of the followers of an account.
type FollowerList struct {
	Followers  []SocialAccount `json:"followers"`
	NextCursor string          `json:"next_cursor,omitempty"`
	HasMore    bool            `json:"has_more"`
}

// FollowingList is one batch of the accounts an account follows.
type FollowingList struct {
	Following  []SocialAccount `json:"following"`
	NextCursor string          `json:"next_cursor,omitempty"`
	HasMore    bool            `json:"has_more"`
}

// BlockedList is one batch of the users an account blocks.
type BlockedList struct {
	Blocked    []SocialAccount `json:"blocked"`
	NextCursor string          `json:"next_cursor,omitempty"`
	HasMore    bool            `json:"has_more"`
}

// FollowChange is the answer to a follow or an unfollow.
type FollowChange struct {
	Username  string `json:"username"`
	Following bool   `json:"following"`
	Changed   bool   `json:"changed"`
}

// BlockChange is the answer to a block or an unblock.
type BlockChange struct {
	Username string `json:"username"`
	Blocked  bool   `json:"blocked"`
	Changed  bool   `json:"changed"`
}

// ProfileUpdated is the profile of the account after a change.
type ProfileUpdated struct {
	Login           string  `json:"login"`
	Name            *string `json:"name,omitempty"`
	Email           *string `json:"email,omitempty"`
	TwitterUsername *string `json:"twitter_username,omitempty"`
	AccountProfile
}

// requireSocialTargets refuses, before a credential is resolved, a connection whose targets name a
// repository or a project, or name no user at all.
func requireSocialTargets(resolved *config.Resolved) (allowlist, error) {
	if resolved == nil {
		return allowlist{}, providerError("open", "no connection was selected")
	}
	allowed, err := allowlistOf(resolved)
	if err != nil {
		return allowlist{}, providerError("open", err.Error())
	}
	if err := accountWideAllowed(allowed); err != nil {
		return allowlist{}, err
	}
	if allowed.bound() && !allowed.namesUser("") {
		return allowlist{}, invalidRequest("the targets of this connection name no user; these tools act for " +
			"the account behind the token, so add it as users/LOGIN to the targets or use another connection")
	}
	return allowed, nil
}

// checkSocialUser validates a login argument and refuses an excluded one before a credential is resolved.
func checkSocialUser(allowed allowlist, login string) error {
	if !validLogin(login) {
		return invalidRequest("username must be a GitHub login")
	}
	if allowed.excludedOwner(login) {
		return invalidRequest("username is not available on this connection; pass another user")
	}
	return nil
}

type socialListOptions struct {
	Username string `json:"username"`
	Limit    int    `json:"limit"`
	Cursor   string `json:"cursor"`

	page, perPage int
	binding       []byte
}

func (o *socialListOptions) normalize(allowed allowlist, kind string) error {
	if o.Username != "" {
		if !validLogin(o.Username) {
			return invalidRequest("username must be a GitHub login")
		}
		// The same rule as for the gists of a user: with targets, the user must be one of them.
		if !allowed.admitsGistOwner(o.Username) {
			return invalidRequest("username is outside the targets of this connection; pass a user they name " +
				"as users/LOGIN, or add the user to the connection's targets")
		}
	}
	limit, err := normalizeLimit(o.Limit)
	if err != nil {
		return err
	}
	o.binding = fingerprint("account", kind, strings.ToLower(o.Username))
	o.page, o.perPage, err = pageOf(o.binding, o.Cursor, limit)
	return err
}

func (o *socialListOptions) query() url.Values {
	return url.Values{"per_page": {strconv.Itoa(o.perPage)}, "page": {strconv.Itoa(o.page)}}
}

func invokeSocialList(kind string) func(context.Context, *config.Resolved, *secret.Resolver, *redact.Redactor,
	json.RawMessage) (any, error) {
	op := "list " + kind
	return func(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
		raw json.RawMessage) (any, error) {
		var o socialListOptions
		if err := readArguments(raw, op, &o); err != nil {
			return nil, err
		}
		allowed, err := requireSocialTargets(resolved)
		if err != nil {
			return nil, err
		}
		if err := o.normalize(allowed, kind); err != nil {
			return nil, err
		}
		client, err := Open(ctx, resolved, secrets, red)
		if err != nil {
			return nil, err
		}
		if o.Username == "" {
			if err := client.requireOwnAccount(ctx, op); err != nil {
				return nil, err
			}
		}
		path := "/user/" + kind
		if o.Username != "" {
			path = "/users/" + url.PathEscape(o.Username) + "/" + kind
		}
		accounts, hasNext, err := client.socialAccounts(ctx, op, path, &o)
		if err != nil {
			return nil, err
		}
		more, cursor := morePage(o.binding, o.page, o.perPage, hasNext)
		if kind == "followers" {
			return &FollowerList{Followers: accounts, NextCursor: cursor, HasMore: more}, nil
		}
		return &FollowingList{Following: accounts, NextCursor: cursor, HasMore: more}, nil
	}
}

func invokeBlocksList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list blocked users"
	var o socialListOptions
	if err := readArguments(raw, op, &o); err != nil {
		return nil, err
	}
	allowed, err := requireSocialTargets(resolved)
	if err != nil {
		return nil, err
	}
	if err := o.normalize(allowed, "blocks"); err != nil {
		return nil, err
	}
	client, err := Open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := client.requireOwnAccount(ctx, op); err != nil {
		return nil, err
	}
	accounts, hasNext, err := client.socialAccounts(ctx, op, "/user/blocks", &o)
	if err != nil {
		return nil, err
	}
	more, cursor := morePage(o.binding, o.page, o.perPage, hasNext)
	return &BlockedList{Blocked: accounts, NextCursor: cursor, HasMore: more}, nil
}

func (c *Client) socialAccounts(ctx context.Context, op, path string, o *socialListOptions) ([]SocialAccount,
	bool, error) {
	permission := followersReadPermission
	if strings.HasPrefix(path, "/user/blocks") {
		permission = blocksReadPermission
	}
	var raw []accountRaw
	hasNext, err := c.restPage(ctx, op, path, o.query(), &raw)
	if err != nil {
		return nil, false, actionsFailure(err, permission)
	}
	accounts := make([]SocialAccount, 0, len(raw))
	for _, entry := range raw {
		if entry.Login == nil || *entry.Login == "" {
			return nil, false, invalidEntry(op, "an account")
		}
		accounts = append(accounts, SocialAccount{Login: *entry.Login, AccountPerson: entry.person(),
			Type: shortText(entry.Type), StarredAt: shortText(entry.StarredAt), URL: address(entry.HTMLURL)})
	}
	return accounts, hasNext, nil
}

// relationState reads whether a follow or a block holds: GitHub answers these routes with 204 when it does and
// 404 when it does not.
func (c *Client) relationState(ctx context.Context, op, path, permission string) (bool, error) {
	err := c.rest(ctx, op, path, nil)
	if err == nil {
		return true, nil
	}
	var failure *provider.Error
	if errors.As(err, &failure) && failure.Class == provider.ClassNotFound {
		return false, nil
	}
	return false, actionsFailure(err, permission)
}

// setRelation brings a follow or a block into the wanted state. The current state is read first, so one that
// already holds is reported without a change; otherwise the change is sent once and never repeated.
func (c *Client) setRelation(ctx context.Context, op, path string, want bool, readPermission,
	changePermission string) (bool, error) {
	have, err := c.relationState(ctx, op, path, readPermission)
	if err != nil {
		return false, err
	}
	if have == want {
		return false, nil
	}
	method := http.MethodPut
	if !want {
		method = http.MethodDelete
	}
	if err := c.restChange(ctx, op, method, path, struct{}{}, nil); err != nil {
		return false, actionsFailure(err, changePermission)
	}
	return true, nil
}

func invokeFollowingSet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "set following"
	var a struct {
		Username  string `json:"username"`
		Following *bool  `json:"following"`
	}
	if err := readArguments(raw, op, &a); err != nil {
		return nil, err
	}
	if a.Following == nil {
		return nil, invalidRequest("following is required: true to follow, false to stop following")
	}
	allowed, err := requireSocialTargets(resolved)
	if err != nil {
		return nil, err
	}
	if err := checkSocialUser(allowed, a.Username); err != nil {
		return nil, err
	}
	client, err := Open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := client.requireOwnAccount(ctx, op); err != nil {
		return nil, err
	}
	changed, err := client.setRelation(ctx, op, "/user/following/"+url.PathEscape(a.Username), *a.Following,
		followersReadPermission, followingChangePermission)
	if err != nil {
		return nil, err
	}
	return &FollowChange{Username: a.Username, Following: *a.Following, Changed: changed}, nil
}

func invokeBlocksChange(block bool) func(context.Context, *config.Resolved, *secret.Resolver, *redact.Redactor,
	json.RawMessage) (any, error) {
	op := "unblock user"
	if block {
		op = "block user"
	}
	return func(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
		raw json.RawMessage) (any, error) {
		var a struct {
			Username string `json:"username"`
		}
		if err := readArguments(raw, op, &a); err != nil {
			return nil, err
		}
		allowed, err := requireSocialTargets(resolved)
		if err != nil {
			return nil, err
		}
		if err := checkSocialUser(allowed, a.Username); err != nil {
			return nil, err
		}
		client, err := Open(ctx, resolved, secrets, red)
		if err != nil {
			return nil, err
		}
		if err := client.requireOwnAccount(ctx, op); err != nil {
			return nil, err
		}
		changed, err := client.setRelation(ctx, op, "/user/blocks/"+url.PathEscape(a.Username), block,
			blocksReadPermission, blocksChangePermission)
		if err != nil {
			return nil, err
		}
		return &BlockChange{Username: a.Username, Blocked: block, Changed: changed}, nil
	}
}

type profileArguments struct {
	Name            *string `json:"name"`
	Email           *string `json:"email"`
	Blog            *string `json:"blog"`
	TwitterUsername *string `json:"twitter_username"`
	Company         *string `json:"company"`
	Location        *string `json:"location"`
	Hireable        *bool   `json:"hireable"`
	Bio             *string `json:"bio"`
}

// body is the PATCH body: exactly the members the call named.
func (a *profileArguments) body() (map[string]any, error) {
	body := map[string]any{}
	for name, value := range map[string]*string{"name": a.Name, "email": a.Email, "blog": a.Blog,
		"twitter_username": a.TwitterUsername, "company": a.Company, "location": a.Location, "bio": a.Bio} {
		if value == nil {
			continue
		}
		limit := profileTextLimit
		if name == "bio" {
			limit = profileBioLimit
		}
		if utf8.RuneCountInString(*value) > limit || strings.IndexFunc(*value, unicode.IsControl) >= 0 {
			return nil, invalidRequest(name + " must be at most " + strconv.Itoa(limit) +
				" characters without control characters")
		}
		body[name] = *value
	}
	if a.Hireable != nil {
		body["hireable"] = *a.Hireable
	}
	if len(body) == 0 {
		return nil, invalidRequest("name at least one profile member to change: " +
			"name, email, blog, twitter_username, company, location, hireable, bio")
	}
	return body, nil
}

func invokeProfileUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "update profile"
	var a profileArguments
	if err := readArguments(raw, op, &a); err != nil {
		return nil, err
	}
	body, err := a.body()
	if err != nil {
		return nil, err
	}
	if _, err := requireSocialTargets(resolved); err != nil {
		return nil, err
	}
	client, err := Open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := client.requireOwnAccount(ctx, op); err != nil {
		return nil, err
	}
	var answer accountRead
	if err := client.restChange(ctx, op, http.MethodPatch, "/user", body, &answer); err != nil {
		return nil, actionsFailure(err, profileChangePermission)
	}
	if answer.Login == "" {
		return nil, invalidEntry(op, "the account")
	}
	return &ProfileUpdated{Login: answer.Login, Name: shortText(&answer.Name), Email: shortText(answer.account.Email),
		TwitterUsername: shortText(answer.extra.TwitterUsername), AccountProfile: answer.profile.project()}, nil
}
