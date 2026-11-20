package googlemerchant

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	emailPreferencesGetID    = Provider + ".email-preferences.get"
	emailPreferencesUpdateID = Provider + ".email-preferences.update"
)

// The service reads and changes the preferences of the authenticated user only, which Google addresses with the
// alias users/me. No other user is ever named in a path.
func emailPreferencesPath(id string) string { return accountPath(id) + "/users/me/emailPreferences" }

var mailChangeRisk = capability.Risk{
	Effect: capability.EffectUpdate, Idempotency: capability.IdempotencyIdempotent,
	Confirmation: capability.ConfirmationRequired, OpenWorld: true, DataSensitivity: "merchant-account-data",
}

const (
	optedIn  = "OPTED_IN"
	optedOut = "OPTED_OUT"
)

// emailPreferences is the documented resource. The name Google returns holds the address of the user, so it is
// not repeated: the result names the alias that was asked for.
type emailPreferences struct {
	Name        string `json:"name"`
	NewsAndTips idText `json:"newsAndTips,omitempty"`
}

type emailPreferencesAnswer struct {
	NewsAndTips idText `json:"newsAndTips"`
}

type emailPreferencesBody struct {
	NewsAndTips string `json:"newsAndTips"`
}

// mailAccepted is the result of an update: Google accepted the request.
type mailAccepted struct {
	Accepted bool     `json:"accepted"`
	Name     string   `json:"name"`
	Updated  []string `json:"updated"`
}

const (
	newsAndTipsSchema  = `{"type":"string","enum":["OPT_IN_STATE_UNSPECIFIED","OPTED_OUT","OPTED_IN","UNCONFIRMED"]}`
	emailGetSchema     = `{"type":"object","properties":{"name":{"type":"string"},"newsAndTips":` + newsAndTipsSchema + `}}`
	mailAcceptedSchema = `{"type":"object","properties":{"accepted":{"type":"boolean"},"name":{"type":"string"},` +
		`"updated":{"type":"array","items":{"type":"string"}}}}`
	newsAndTipsInput = `{"type":"string","enum":["OPTED_IN","OPTED_OUT"]}`
)

const emailScopeNote = "The service acts only for the authenticated user of the credential: the person who gave the " +
	"consent, or the service account of the key, not for any other user of the account. Google advises users of an " +
	"advanced account to use the advanced account rather than one of its sub-accounts. Mandatory announcements are " +
	"not part of the preferences."

var emailPreferencesGet = capability.Descriptor{
	ID: emailPreferencesGetID, Version: 1, Title: "Get the email preferences of the credential's user",
	Description: "Read whether the authenticated user of the credential receives news and tips from Merchant Center " +
		"(newsAndTips: OPTED_IN, OPTED_OUT, or UNCONFIRMED while an opt-in awaits its confirmation). " +
		emailScopeNote + " The name in the result is the alias accounts/ACCOUNT/users/me/emailPreferences: the name " +
		"Google returns holds the address of the user and is not repeated. Values from Google are untrusted content " +
		"and are cut.",
	Tags: []string{"google", "merchant", "email-preferences"}, Risk: readRisk, Provider: Provider,
	InputSchema:  inputSchema(nil, member("account", accountArgSchema)),
	OutputSchema: json.RawMessage(emailGetSchema),
	Arguments:    []capability.Argument{accountArgument},
	Fields: []capability.Field{{Name: "name", Description: "accounts/ACCOUNT/users/me/emailPreferences"},
		{Name: "newsAndTips", Description: "OPT_IN_STATE_UNSPECIFIED, OPTED_OUT, OPTED_IN, or UNCONFIRMED"}},
	Examples: []capability.Example{{Description: "Preferences of the credential's user",
		Arguments: json.RawMessage(`{"account":"1000001"}`)}},
}

var emailPreferencesUpdate = capability.Descriptor{
	ID: emailPreferencesUpdateID, Version: 1, Title: "Change the email preferences of the credential's user",
	Description: "Opt the authenticated user of the credential in or out of news and tips mails of Merchant Center " +
		"(news_and_tips). This changes the mail settings of the person, or of the service account, of the " +
		"credential, and no other user. " + emailScopeNote + " Only the acceptance of the request is reported: " +
		"Google applies the change with a delay, and in some countries an opt-in stays UNCONFIRMED until the " +
		"user confirms it by mail, so read the preferences again after a while. The result holds no content " +
		"from Google, which would be untrusted.",
	Tags: []string{"google", "merchant", "email-preferences", "update"}, Risk: mailChangeRisk, Provider: Provider,
	InputSchema: inputSchema([]string{"news_and_tips"}, member("account", accountArgSchema),
		member("news_and_tips", newsAndTipsInput)),
	OutputSchema: json.RawMessage(mailAcceptedSchema),
	Arguments: []capability.Argument{accountArgument,
		{Name: "news_and_tips", Description: "OPTED_IN or OPTED_OUT; the other states of the document cannot be set",
			Required: true}},
	Fields: []capability.Field{{Name: "accepted", Description: "true when Google accepted the request"},
		{Name: "name", Description: "accounts/ACCOUNT/users/me/emailPreferences"},
		{Name: "updated", Description: "the preference the request named"}},
	Examples: []capability.Example{{Description: "Opt out of news and tips",
		Arguments: json.RawMessage(`{"account":"1000001","news_and_tips":"OPTED_OUT"}`)}},
}

func handleEmailPreferencesGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("get email preferences", emailPreferencesGetID, resolved, raw)
	if err != nil {
		return nil, err
	}
	cl, id, err := settleMail(ctx, c, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var answer emailPreferencesAnswer
	if err := cl.get(ctx, c.op, c.tool, emailPreferencesPath(id), nil, &answer); err != nil {
		return nil, err
	}
	return emailPreferences{Name: emailPreferencesPath(id)[len(accountsAPI):], NewsAndTips: answer.NewsAndTips}, nil
}

func handleEmailPreferencesUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("update email preferences", emailPreferencesUpdateID, resolved, raw)
	if err != nil {
		return nil, err
	}
	var args struct {
		NewsAndTips string `json:"news_and_tips"`
	}
	if json.Unmarshal(raw, &args) != nil || (args.NewsAndTips != optedIn && args.NewsAndTips != optedOut) {
		return nil, invalidArgument("news_and_tips is required and must be OPTED_IN or OPTED_OUT")
	}
	cl, id, err := settleMail(ctx, c, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	path := emailPreferencesPath(id)
	payload, err := json.Marshal(emailPreferencesBody{NewsAndTips: args.NewsAndTips})
	if err != nil {
		return nil, provider.Fail(c.op, "the request body could not be encoded")
	}
	var answer emailPreferencesAnswer
	if err := cl.send(ctx, c.op, c.tool, request{method: http.MethodPatch, path: path,
		query: url.Values{"updateMask": {"newsAndTips"}}, body: payload, change: true}, &answer); err != nil {
		return nil, err
	}
	return mailAccepted{Accepted: true, Name: path[len(accountsAPI):], Updated: []string{"newsAndTips"}}, nil
}

func emailPreferencesSummary(raw json.RawMessage) string {
	var args struct {
		Account     string `json:"account"`
		NewsAndTips string `json:"news_and_tips"`
	}
	_ = json.Unmarshal(raw, &args)
	account := args.Account
	if account == "" {
		account = "of the connection"
	}
	state := "out of"
	if args.NewsAndTips == optedIn {
		state = "in to"
	}
	return "Opt the user of the credential (the person or service account behind it) " + state +
		" news and tips mails of Merchant Center account " + account + ". Google applies it with a delay."
}

// settleMail settles the account against the binding before any secret is read, then opens the client and, for
// an account no target names, reads the proof.
func settleMail(ctx context.Context, c call, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor) (*client, string, error) {
	id, needsProof, err := c.scope.accountOf(c.args.Account)
	if err != nil {
		return nil, "", err
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, "", err
	}
	if needsProof {
		if err := cl.prove(ctx, c.op, c.tool, id); err != nil {
			return nil, "", err
		}
	}
	return cl, id, nil
}
