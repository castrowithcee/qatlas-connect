package github

import (
	"context"
	"net/url"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/config"
)

// The invitation tools address an existing account by login only. A prompt-injected login would otherwise
// hand repository or organization access to an attacker's account, and a confirmation shows no account
// details, so the connection's own targets decide: only a login the targets name as users/LOGIN can be invited.

// inviteeAccount is the account an invitation tool reads before it writes: the login in GitHub's canonical
// spelling and the numeric ID a later request may need.
type inviteeAccount struct {
	Login string
	ID    int64
}

// allDigits reports whether the value is a non-empty run of ASCII digits, which GitHub would read as an ID.
func allDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// selectInvitee checks the login of an invitation before a credential is resolved or any request is made:
// it must be a plain login, neither an e-mail address nor an ID, and the connection's targets must name it as
// users/LOGIN, compared without case. An exclusion of the login wins. A connection without a users/LOGIN
// target can invite nobody. It returns the login as the target spells it.
func selectInvitee(resolved *config.Resolved, username string) (string, error) {
	if strings.Contains(username, "@") || allDigits(username) || !validLogin(username) {
		return "", invalidRequest("username must be a GitHub login, not an e-mail address or a numeric ID")
	}
	if resolved == nil {
		return "", providerError("open", "no connection was selected")
	}
	allowed, err := allowlistOf(resolved)
	if err != nil {
		return "", providerError("open", err.Error())
	}
	for _, entry := range allowed.targets {
		if entry.kind == kindOwner && entry.scope == "users" && strings.EqualFold(entry.owner, username) {
			if allowed.excludedOwner(entry.owner) {
				break
			}
			return entry.owner, nil
		}
	}
	return "", invalidRequest("username is not available for invitations on this connection; only a login " +
		"its targets name as users/LOGIN can be invited, so add it there or use another connection")
}

// readInvitee reads GET /users/{login} and accepts only an account of type User whose login equals the
// requested one without case, so an organization, a bot, or a renamed account never receives an invitation.
func (c *Client) readInvitee(ctx context.Context, op, login string) (*inviteeAccount, error) {
	var account struct {
		Login string `json:"login"`
		ID    int64  `json:"id"`
		Type  string `json:"type"`
	}
	if err := c.rest(ctx, op, "/users/"+url.PathEscape(login), &account); err != nil {
		if isNotFound(err) {
			return nil, invalidRequest("the account does not exist; nothing was sent")
		}
		return nil, err
	}
	if account.Type != "User" || account.ID < 1 || !strings.EqualFold(account.Login, login) ||
		!validLogin(account.Login) {
		return nil, invalidRequest("the account is no user account with this login; nothing was sent")
	}
	return &inviteeAccount{Login: account.Login, ID: account.ID}, nil
}
