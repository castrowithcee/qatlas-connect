package googlebusinessprofile

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	invitationsListID    = Provider + ".invitations.list"
	invitationsDeclineID = Provider + ".invitations.decline"
)

// Form of an invitation path. Like an admin ID, the ID of an invitation is a short token without a path step.
const (
	formInvitation    = "accounts/ACCOUNT/invitations/INVITATION"
	invitationPattern = `^accounts/` + idPattern + `/invitations/` + adminIDPattern + `$`

	targetAccountsOnly  = "ACCOUNTS_ONLY"
	targetLocationsOnly = "LOCATIONS_ONLY"
)

var invitationForm = regexp.MustCompile(invitationPattern)

const (
	filterSchema     = `{"type":"string","enum":["ACCOUNTS_ONLY","LOCATIONS_ONLY"]}`
	invitationSchema = `{"type":"string","maxLength":120,"pattern":"` + invitationPattern + `","x-form":"` +
		formInvitation + `"}`
)

const invitationPersonalNote = "An invitation names the account or location the consenting account is invited to " +
	"administer, with its name and, for a location, its address, which belong to whoever sent the invitation. "

var invitationsRisk = capability.Risk{
	Effect: capability.EffectRead, Idempotency: capability.IdempotencySafe, Confirmation: capability.ConfirmationNone,
	OpenWorld: true,
	DataSensitivity: "data of the sender of an invitation: the name of the target account or the name and address of " +
		"the target location, and the invited role",
}

var invitationDeclineRisk = capability.Risk{
	Effect: capability.EffectUpdate, Idempotency: capability.IdempotencyIdempotent,
	Confirmation: capability.ConfirmationRequired, OpenWorld: true,
	DataSensitivity: "data of the sender of an invitation: the name of the target account or location and the invited role",
}

type (
	targetLocation struct {
		LocationName line  `json:"locationName,omitempty"`
		Address      line  `json:"address,omitempty"`
		PlaceID      short `json:"placeId,omitempty"`
	}
	invitation struct {
		Name           short           `json:"name,omitempty"`
		Role           short           `json:"role,omitempty"`
		TargetType     short           `json:"targetType,omitempty"`
		TargetAccount  *account        `json:"targetAccount,omitempty"`
		TargetLocation *targetLocation `json:"targetLocation,omitempty"`
	}
	invitationsAnswer struct {
		Invitations list[invitation] `json:"invitations"`
	}
	invitationsResult struct {
		Invitations []invitation `json:"invitations"`
	}
	declinedResult struct {
		Name     string `json:"name"`
		Declined bool   `json:"declined"`
	}
)

var (
	invitationOutput = objectSchema("name", stringSchema, "role", stringSchema, "targetType", stringSchema,
		"targetAccount", accountSchema,
		"targetLocation", objectSchema("locationName", stringSchema, "address", stringSchema, "placeId", stringSchema))
	invitationsResultSchema = `{"type":"object","properties":{"invitations":` + arraySchema(invitationOutput) +
		`},"required":["invitations"],"additionalProperties":false}`
	declinedSchema = objectSchema("name", stringSchema, "declined", booleanSchema)
)

var invitationsList = capability.Descriptor{
	ID: invitationsListID, Version: 1, Title: "List Business Profile invitations",
	Description: "List the invitations that are pending for one Google Business Profile account: the invited role and " +
		"the account or location it is for. Google offers no paging for this list; at most 250 entries are shown. " +
		"filter keeps only invitations for accounts or only those for locations. " + invitationPersonalNote +
		"Accepting an invitation is not offered by any tool: a person accepts it in Google's own interface. A " +
		"connection bound to a location does not offer this tool. " + untrustedNote,
	Tags: []string{"google", "business-profile", "invitations"}, Risk: invitationsRisk, Provider: Provider,
	InputSchema:  inputSchema(nil, "account", accountArgSchema, "filter", filterSchema),
	OutputSchema: json.RawMessage(invitationsResultSchema),
	Arguments: []capability.Argument{
		{Name: "account", Target: "account", Description: "Account as accounts/ACCOUNT; optional when the " +
			"connection is bound to an account, and then it must be that account"},
		{Name: "filter", Description: "Only invitations for this kind of target: ACCOUNTS_ONLY or LOCATIONS_ONLY"},
	},
	Fields: []capability.Field{{Name: "invitations", Description: "Invitations with name " +
		"(accounts/ACCOUNT/invitations/INVITATION), role, targetType, targetAccount (sparse account), and " +
		"targetLocation (locationName, address, placeId)"}},
	Examples: []capability.Example{
		{Description: "Invitations of one account", Arguments: json.RawMessage(`{"account":"accounts/1000001"}`)},
		{Description: "Only invitations for locations", Arguments: json.RawMessage(
			`{"account":"accounts/1000001","filter":"LOCATIONS_ONLY"}`)},
	},
}

var invitationsDecline = capability.Descriptor{
	ID: invitationsDeclineID, Version: 1, Title: "Decline a Business Profile invitation",
	Description: "Decline one pending invitation of a Google Business Profile account. Declining only removes an " +
		"offer of access; the tool cannot accept an invitation and creates no admin. previous_role and " +
		"previous_target are required and only compared: the tool reads the invitations of the account first and " +
		"declines nothing unless the invitation still has exactly this role and target, so the confirmation shows " +
		"what is declined. Google counts at most 10 changes per minute and profile. " + invitationPersonalNote +
		"A connection bound to a location does not offer this tool. " + untrustedNote,
	Tags: []string{"google", "business-profile", "invitations", "update"}, Risk: invitationDeclineRisk,
	Provider: Provider,
	InputSchema: inputSchema([]string{"invitation", "previous_role", "previous_target"},
		"invitation", invitationSchema, "previous_role", previousRoleSchema, "previous_target", adminTextSchema),
	OutputSchema: json.RawMessage(declinedSchema),
	Arguments: []capability.Argument{
		{Name: "invitation", Required: true, Description: "Path of a pending invitation as listed: " + formInvitation},
		{Name: "previous_role", Required: true, Description: "The role of the invitation as last read with the " +
			"list tool; only compared, never sent, and the decline is refused when it differs"},
		{Name: "previous_target", Required: true, Description: "The target of the invitation as last read: " +
			"targetAccount.name, or targetLocation.locationName for a location; only compared, never sent, and the " +
			"decline is refused when it differs"},
	},
	Fields: []capability.Field{{Name: "name", Description: "Path of the declined invitation"},
		{Name: "declined", Description: "true once Google confirmed the decline"}},
	Examples: []capability.Example{{Description: "Decline an invitation", Arguments: json.RawMessage(
		`{"invitation":"accounts/1000001/invitations/4000004","previous_role":"MANAGER","previous_target":"accounts/1000050"}`)}},
}

type invitationArguments struct {
	Account        string `json:"account"`
	Filter         string `json:"filter"`
	Invitation     string `json:"invitation"`
	PreviousRole   string `json:"previous_role"`
	PreviousTarget string `json:"previous_target"`
}

func readInvitationArguments(op string, raw json.RawMessage) (invitationArguments, error) {
	var args invitationArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return args, provider.Fail(op, "the validated arguments could not be read")
	}
	return args, nil
}

// invitationOf checks the path of an invitation against its form and the target and returns the account it
// lies under. A location target refuses it, and every target refuses another account; no message names the
// other object.
func (s scope) invitationOf(given string) (string, error) {
	if !invitationForm.MatchString(given) {
		return "", invalidArgument("invitation must be " + formInvitation + " with a decimal account ID")
	}
	parent := given[:strings.Index(given, "/invitations/")]
	switch {
	case s.location != "":
		return "", invalidArgument("invitations are not available on a connection bound to a location")
	case s.account != "" && parent != s.account:
		return "", invalidArgument("invitation " + outsideTarget)
	}
	return parent, nil
}

// readInvitations reads the invitations of an account, once, with Google's filter when one is given. The list
// has no paging.
func (c *client) readInvitations(ctx context.Context, op, tool, account, filter string) ([]invitation, error) {
	r := request{root: accountRoot, path: account + "/invitations", notFound: notFoundAccount}
	if filter != "" {
		r.query = url.Values{"filter": {"targetType=" + filter}}
	}
	var answer invitationsAnswer
	err := c.get(ctx, op, tool, r, &answer)
	return answer.Invitations, err
}

func handleInvitationsList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list invitations"
	args, err := readInvitationArguments(op, raw)
	if err != nil {
		return nil, err
	}
	if args.Filter != "" && args.Filter != targetAccountsOnly && args.Filter != targetLocationsOnly {
		return nil, invalidArgument("filter must be ACCOUNTS_ONLY or LOCATIONS_ONLY")
	}
	bound, err := checkConnection(resolved)
	if err != nil {
		return nil, err
	}
	name, err := bound.accountOf("account", args.Account, false)
	if err != nil {
		return nil, err
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	all, err := c.readInvitations(ctx, op, invitationsListID, name, args.Filter)
	if err != nil {
		return nil, err
	}
	// Google may ignore a filter it does not know, so a row that names another target type is dropped here.
	kept := []invitation{}
	for _, one := range all {
		if args.Filter == "" || one.TargetType == "" || string(one.TargetType) == args.Filter {
			kept = append(kept, one)
		}
	}
	return invitationsResult{Invitations: kept}, nil
}

// targetOf is the target of an invitation as the list shows it: the account name, or the location name.
func (i invitation) targetOf() string {
	switch {
	case i.TargetAccount != nil && i.TargetAccount.Name != "":
		return string(i.TargetAccount.Name)
	case i.TargetLocation != nil:
		return string(i.TargetLocation.LocationName)
	}
	return ""
}

func handleInvitationsDecline(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "decline invitation"
	args, err := readInvitationArguments(op, raw)
	if err != nil {
		return nil, err
	}
	bound, err := checkConnection(resolved)
	if err != nil {
		return nil, err
	}
	parent, err := bound.invitationOf(args.Invitation)
	if err != nil {
		return nil, err
	}
	switch {
	case args.PreviousRole != rolePrimaryOwner && args.PreviousRole != roleOwner && args.PreviousRole != roleManager &&
		args.PreviousRole != roleSiteManager:
		return nil, invalidArgument("previous_role must be PRIMARY_OWNER, OWNER, MANAGER, or SITE_MANAGER")
	case strings.TrimSpace(args.PreviousTarget) == "":
		return nil, invalidArgument("previous_target is required: the target of the invitation as last read with " +
			"the list tool")
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	invitations, err := c.readInvitations(ctx, op, invitationsDeclineID, parent, "")
	if err != nil {
		return nil, err
	}
	var current *invitation
	for i := range invitations {
		if string(invitations[i].Name) == args.Invitation {
			current = &invitations[i]
			break
		}
	}
	switch {
	case current == nil && len(invitations) >= maxItems:
		return nil, invalidArgument("the invitation is not among the first 250 invitations of the list")
	case current == nil:
		return nil, invalidArgument("the invitation is not in the list of the account; read it with the list tool")
	case string(current.Role) != args.PreviousRole || current.targetOf() != args.PreviousTarget:
		return nil, invalidArgument("previous_role and previous_target are not what Google holds for this " +
			"invitation now; read the invitation with the list tool and send those values")
	}
	// The body is the empty DeclineInvitationRequest; nothing of the call goes into it.
	var empty struct{}
	if err := c.post(ctx, op, invitationsDeclineID, change{root: accountRoot, path: args.Invitation + ":decline",
		body: []byte(`{}`), key: parent, notFound: notFoundAccount}, &empty); err != nil {
		return nil, err
	}
	return declinedResult{Name: args.Invitation, Declined: true}, nil
}

// invitationSummary phrases a confirmation from the arguments alone; the core neutralizes and bounds it.
func invitationSummary(raw json.RawMessage) string {
	var a invitationArguments
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	return "Decline the invitation " + a.Invitation + " to administer " + a.PreviousTarget + " with the role " +
		a.PreviousRole + "; the offer of access is withdrawn and the invitation cannot be accepted afterwards without " +
		"a new one; no invitation is accepted and no admin is created"
}
