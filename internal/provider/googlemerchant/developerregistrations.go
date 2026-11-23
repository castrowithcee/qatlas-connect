package googlemerchant

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	developerRegistrationsGetAccountID = Provider + ".developer-registrations.get-account-for-gcp-registration"
	developerRegistrationsUnregisterID = Provider + ".developer-registrations.unregister-gcp"
)

// gcpAccountResult is the answer of getAccountForGcpRegistration: the resource name of the account the
// calling Cloud project is registered with.
type gcpAccountResult struct {
	Name idText `json:"name,omitempty"`
}

// gcpUnregisterResult says only that Google accepted the request.
type gcpUnregisterResult struct {
	Accepted bool   `json:"accepted"`
	Name     string `json:"name"`
}

var (
	gcpAccountSchema    = objectSchema(member("name", stringSchema))
	gcpUnregisterSchema = objectSchema(member("accepted", boolSchema), member("name", stringSchema))
)

const (
	gcpAccountNote = "Google takes no account for this method: it names the account the Cloud project of the credential " +
		"is registered with. On a connection bound to targets the answer is shown only if that account is named by a " +
		"target or is a direct sub-account of a bound advanced account, which can cost further calls; otherwise the " +
		"call is refused without naming the account. Without targets the call reaches what the credential reaches. " +
		"The account ID is untrusted content."
	gcpUnregisterEffect = "Unregisters the Google Cloud project of this credential from the Merchant Center account: " +
		"according to Google the project can still call the API for at most one day, and then it loses API access " +
		"to the account, and so does every credential of the project, this connection included. Qatlas cannot " +
		"register a project (registerGcp is not offered), so a new registration has to be made outside Qatlas, " +
		"for example with the API Explorer of the Merchant API."
)

var (
	developerRegistrationsGetAccount = capability.Descriptor{
		ID: developerRegistrationsGetAccountID, Version: 1,
		Title: "Get the account a Cloud project is registered with",
		Description: "Read the Merchant Center account that the Google Cloud project of the credential is registered " +
			"with as a developer (developerRegistration.getAccountForGcpRegistration). The call has no arguments. " +
			gcpAccountNote,
		Tags: []string{"google", "merchant", "developer-registrations"}, Risk: readRisk, Provider: Provider,
		InputSchema: inputSchema(nil), OutputSchema: json.RawMessage(gcpAccountSchema),
		Fields:   []capability.Field{{Name: "name", Description: "accounts/ACCOUNT or the ID of the registered account, as Google sends it"}},
		Examples: []capability.Example{{Description: "The account of the registration", Arguments: json.RawMessage(`{}`)}},
	}

	developerRegistrationsUnregister = capability.Descriptor{
		ID: developerRegistrationsUnregisterID, Version: 1,
		Title: "Unregister the Cloud project from an advanced account",
		Description: "Unregister the calling Google Cloud project from a Merchant Center account " +
			"(developerRegistration.unregisterGcp). " + gcpUnregisterEffect + " Needs the role Admin on the account. " +
			"The account argument is required and must be an advanced account this connection is bound to as " +
			"advanced-accounts/ACCOUNT: a sub-account or an account target is refused before any request. Qatlas " +
			"sends a fixed empty request once and never repeats it; the answer only says that Google accepted the " +
			"request, and Google applies it with a delay, so read the registration again after a while. Offered " +
			"only where a connection's tools list names it, and every call needs a confirmation. The answer and the " +
			"errors of Google are untrusted content; only the acceptance is reported.",
		Tags: []string{"google", "merchant", "developer-registrations", "delete"}, Provider: Provider,
		RequiresToolAllowList: true, Risk: changeRisk(capability.EffectDelete, capability.IdempotencyUnknown),
		InputSchema:  inputSchema(nil, member("account", accountArgSchema)),
		OutputSchema: json.RawMessage(gcpUnregisterSchema),
		Arguments: []capability.Argument{{Name: "account", Target: kindAdvanced, Description: "ID of the advanced account, " +
			"digits only; the tool refuses a call without it; the connection must be bound to it as an advanced account"}},
		Fields: []capability.Field{{Name: "accepted", Description: "true when Google accepted the request; nothing is said about processing"},
			{Name: "name", Description: "accounts/ACCOUNT/developerRegistration of the request"}},
		Examples: []capability.Example{{Description: "Unregister the project",
			Arguments: json.RawMessage(`{"account":"1000001"}`)}},
	}
)

func developerRegistrationDescriptors() []capability.Descriptor {
	return []capability.Descriptor{developerRegistrationsGetAccount, developerRegistrationsUnregister}
}

func developerRegistrationHandlers() map[string]capability.Handler {
	return map[string]capability.Handler{
		developerRegistrationsGetAccountID: handleGetAccountForGcpRegistration,
		developerRegistrationsUnregisterID: handleUnregisterGcp,
	}
}

func developerRegistrationSummaries() map[string]capability.SummaryFunc {
	return map[string]capability.SummaryFunc{developerRegistrationsUnregisterID: unregisterGcpSummary}
}

// handleGetAccountForGcpRegistration is fail-closed on a bound connection: Google takes no account, so the
// answer is checked against the targets before anything of it is returned.
func handleGetAccountForGcpRegistration(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("get account for gcp registration", developerRegistrationsGetAccountID, resolved, raw)
	if err != nil {
		return nil, err
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var out gcpAccountResult
	if err := cl.get(ctx, c.op, c.tool, accountsAPI+"accounts:getAccountForGcpRegistration", nil, &out); err != nil {
		return nil, err
	}
	if !c.scope.bound() {
		return out, nil
	}
	// The documented form of name is open: accept accounts/ID and a bare ID, nothing else.
	id := strings.TrimPrefix(string(out.Name), accountPrefix)
	if !validID(id) {
		return nil, provider.InvalidResponse(c.op, subject+" returned an account name in an unknown form")
	}
	switch c.scope.reachOf(id) {
	case reachNone:
		return nil, invalidArgument("the registered account " + outsideTargets)
	case reachProof:
		if err := cl.prove(ctx, c.op, c.tool, id); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func handleUnregisterGcp(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("unregister gcp", developerRegistrationsUnregisterID, resolved, raw)
	if err != nil {
		return nil, err
	}
	// The account is named on purpose: the one advanced account of a binding is not assumed.
	if c.args.Account == "" {
		return nil, invalidArgument("account is required")
	}
	id, err := c.scope.advancedOf(c.args.Account)
	if err != nil {
		return nil, err
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	path := accountPath(id) + "/developerRegistration"
	if err := cl.send(ctx, c.op, c.tool, request{method: http.MethodPost, path: path + ":unregisterGcp",
		change: true}, &struct{}{}); err != nil {
		return nil, err
	}
	return gcpUnregisterResult{Accepted: true, Name: path[len(accountsAPI):]}, nil
}

// unregisterGcpSummary phrases the confirmation from the validated arguments alone.
func unregisterGcpSummary(raw json.RawMessage) string {
	var a struct {
		Account string `json:"account"`
	}
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	return "Unregister the Google Cloud project of this credential from Merchant Center account " + a.Account +
		": within at most one day the project loses API access to the account, and so does every credential of the " +
		"project, this connection included. A new registration has to be made outside Qatlas, which cannot " +
		"register a project. Google processes the request afterwards."
}
