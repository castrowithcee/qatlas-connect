package googlemerchant

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	homepagesGetID     = Provider + ".homepages.get"
	homepagesUpdateID  = Provider + ".homepages.update"
	homepagesClaimID   = Provider + ".homepages.claim"
	homepagesUnclaimID = Provider + ".homepages.unclaim"
)

// homepage is the Homepage resource of the accounts sub-API. claimed is always shown: false is an answer.
type homepage struct {
	Name    idText  `json:"name,omitempty"`
	URI     urlText `json:"uri,omitempty"`
	Claimed bool    `json:"claimed"`
}

var homepageSchema = objectSchema(member("name", stringSchema), member("uri", stringSchema),
	member("claimed", boolSchema))

// The Homepage resource documents no length for uri; the bound is ours and equals what a result shows.
const (
	homepageURISchema = `{"type":"string","minLength":1,"maxLength":2048,` +
		`"x-form":"absolute https address of the store home page, without user information"}`
	homepageFieldsDescription = "name (accounts/ACCOUNT/homepage), uri, and claimed (also true when the account " +
		"inherits the claim from its advanced account or is exempt from claiming)"
	homepageUntrustedNote = "The home page address is untrusted content of whoever runs the account: it is text " +
		"that is never fetched."
	homepageDelayNote = "Google accepts the change and applies it with a delay: it can take several minutes until " +
		"homepages.get and accounts.get show it, so the answer only tells that Google accepted it. Every call counts " +
		"against the quota of the Accounts group, a failed one too, and a call is never repeated: when the outcome " +
		"is unknown, read the home page before changing it again. The home page address steers customers, and a " +
		"claim is what Google checks the website against. Needs confirmation and the role Admin in Merchant Center."
)

// homepageRisk is the risk of a tool that changes the home page: always confirmed, never repeated here.
var homepageRisk = capability.Risk{Effect: capability.EffectUpdate, Idempotency: capability.IdempotencyIdempotent,
	Confirmation: capability.ConfirmationRequired, OpenWorld: true, DataSensitivity: "merchant-account-data"}

var homepageWriteArgument = capability.Argument{Name: "account", Target: kindAccount,
	Description: "Merchant Center account ID, digits only; the tool refuses a call without it; it must be named by a " +
		"target or be a direct sub-account of a bound advanced account"}

var (
	homepagesGet = capability.Descriptor{
		ID: homepagesGetID, Version: 1, Title: "Get the home page of a Merchant Center account",
		Description: "Read the store home page of one account (accounts.homepage.get) and whether it is claimed. " +
			"On a connection bound to an advanced account, " +
			"an account that is not named by a target is read only after it is found among the direct " +
			"sub-accounts of a bound advanced account, which costs further calls. After a change it can take " +
			"several minutes until the change shows. " + homepageUntrustedNote,
		Tags: []string{"google", "merchant", "homepages"}, Provider: Provider,
		Risk:         readRisk,
		InputSchema:  inputSchema(nil, member("account", accountArgSchema)),
		OutputSchema: json.RawMessage(homepageSchema),
		Arguments:    []capability.Argument{accountArgument},
		Fields:       []capability.Field{{Name: "homepage", Description: homepageFieldsDescription}},
		Examples: []capability.Example{{Description: "Home page of one account",
			Arguments: json.RawMessage(`{"account":"1000001"}`)}},
	}

	homepagesUpdate = capability.Descriptor{
		ID: homepagesUpdateID, Version: 1, Title: "Set the home page address of a Merchant Center account",
		Description: "Set the store home page address of one account (accounts.homepage.update, update mask uri). uri " +
			"is required and must be an absolute https address with a host and without user information, at " +
			"most 2048 characters; Qatlas refuses anything else before any request. A changed address can " +
			"drop the claim, so claim the home page again afterwards. Offered only where a connection's tools " +
			"list names it. " + homepageDelayNote + " " + homepageUntrustedNote,
		Tags: []string{"google", "merchant", "homepages"}, Provider: Provider, RequiresToolAllowList: true,
		Risk:         homepageRisk,
		InputSchema:  inputSchema([]string{"uri"}, member("account", accountArgSchema), member("uri", homepageURISchema)),
		OutputSchema: json.RawMessage(homepageSchema),
		Arguments: []capability.Argument{homepageWriteArgument,
			{Name: "uri", Required: true, Description: "New home page address: an absolute https URL with a host, " +
				"without user information, at most 2048 characters"}},
		Fields: []capability.Field{{Name: "homepage", Description: "The home page as Google answered: " +
			homepageFieldsDescription}},
		Examples: []capability.Example{{Description: "Set the home page address",
			Arguments: json.RawMessage(`{"account":"1000001","uri":"https://shop.example.invalid/"}`)}},
	}

	homepagesClaim = capability.Descriptor{
		ID: homepagesClaimID, Version: 1, Title: "Claim the home page of a Merchant Center account",
		Description: "Claim the store home page of one account (accounts.homepage.claim): Google checks that the " +
			"account holder owns the website. An already claimed home page is verified again and the call " +
			"succeeds; an account that is exempt from claiming is exempt from the check. When ownership cannot be " +
			"verified, Google answers with an error and keeps an existing claim. WARNING: with overwrite true " +
			"Google removes an existing claim on the website that another account holds and transfers it to " +
			"this account; that account loses its claim. Leave overwrite out unless that is wanted. Offered only " +
			"where a connection's tools list names it. " + homepageDelayNote + " " + homepageUntrustedNote,
		Tags: []string{"google", "merchant", "homepages"}, Provider: Provider, RequiresToolAllowList: true,
		Risk:         homepageRisk,
		InputSchema:  inputSchema(nil, member("account", accountArgSchema), member("overwrite", boolSchema)),
		OutputSchema: json.RawMessage(homepageSchema),
		Arguments: []capability.Argument{homepageWriteArgument,
			{Name: "overwrite", Description: "WARNING: true removes an existing claim on the website held by another " +
				"account and transfers it to this account; false when left out"}},
		Fields: []capability.Field{{Name: "homepage", Description: "The home page as Google answered: " +
			homepageFieldsDescription}},
		Examples: []capability.Example{{Description: "Claim the home page",
			Arguments: json.RawMessage(`{"account":"1000001"}`)}},
	}

	homepagesUnclaim = capability.Descriptor{
		ID: homepagesUnclaimID, Version: 1, Title: "Unclaim the home page of a Merchant Center account",
		Description: "Remove the claim of the store home page of one account (accounts.homepage.unclaim). Only a " +
			"claim made directly on the account is removed: when the claim is inherited from an advanced " +
			"account, the call succeeds and claimed stays true; an account that is exempt from claiming is " +
			"refused by Google. Offered only where a connection's tools list names it. " + homepageDelayNote + " " +
			homepageUntrustedNote,
		Tags: []string{"google", "merchant", "homepages"}, Provider: Provider, RequiresToolAllowList: true,
		Risk:         homepageRisk,
		InputSchema:  inputSchema(nil, member("account", accountArgSchema)),
		OutputSchema: json.RawMessage(homepageSchema),
		Arguments:    []capability.Argument{homepageWriteArgument},
		Fields: []capability.Field{{Name: "homepage", Description: "The home page as Google answered: " +
			homepageFieldsDescription}},
		Examples: []capability.Example{{Description: "Unclaim the home page",
			Arguments: json.RawMessage(`{"account":"1000001"}`)}},
	}
)

func homepageDescriptors() []capability.Descriptor {
	return []capability.Descriptor{homepagesGet, homepagesUpdate, homepagesClaim, homepagesUnclaim}
}

func homepageHandlers() map[string]capability.Handler {
	return map[string]capability.Handler{
		homepagesGetID: handleHomepagesGet, homepagesUpdateID: handleHomepagesUpdate,
		homepagesClaimID: handleHomepagesClaim, homepagesUnclaimID: handleHomepagesUnclaim,
	}
}

type homepageArguments struct {
	Account   string  `json:"account"`
	URI       *string `json:"uri"`
	Overwrite bool    `json:"overwrite"`
}

func readHomepageArguments(raw json.RawMessage) (homepageArguments, error) {
	var a homepageArguments
	if err := json.Unmarshal(raw, &a); err != nil {
		return a, invalidArgument("the arguments could not be read")
	}
	return a, nil
}

func homepagePath(id string) string { return accountPath(id) + "/homepage" }

func handleHomepagesGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage) (any, error) {
	c, err := prepare("get home page", homepagesGetID, resolved, raw)
	if err != nil {
		return nil, err
	}
	id, needsProof, err := c.scope.accountOf(c.args.Account)
	if err != nil {
		return nil, err
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if needsProof {
		if err := cl.prove(ctx, c.op, c.tool, id); err != nil {
			return nil, err
		}
	}
	var out homepage
	if err := cl.get(ctx, c.op, c.tool, homepagePath(id), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// changeHomepage runs the shared path of the three write tools: the account is settled against the targets
// before any secret is resolved, the account is proven if needed, and the change is sent once.
func changeHomepage(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	c call, id string, needsProof bool, r request) (any, error) {
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if needsProof {
		if err := cl.prove(ctx, c.op, c.tool, id); err != nil {
			return nil, err
		}
	}
	var out homepage
	if err := cl.send(ctx, c.op, c.tool, r, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func handleHomepagesUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage) (any, error) {
	c, err := prepare("update home page", homepagesUpdateID, resolved, raw)
	if err != nil {
		return nil, err
	}
	a, err := readHomepageArguments(raw)
	if err != nil {
		return nil, err
	}
	if a.Account == "" {
		return nil, invalidArgument("account is required")
	}
	if a.URI == nil {
		return nil, invalidArgument("uri is required")
	}
	if utf8.RuneCountInString(*a.URI) > maxURLRunes {
		return nil, invalidArgument("uri must be at most " + strconv.Itoa(maxURLRunes) + " characters")
	}
	if err := checkHTTPSAddress("uri", *a.URI); err != nil {
		return nil, err
	}
	id, needsProof, err := c.scope.accountOf(a.Account)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(struct {
		URI string `json:"uri"`
	}{*a.URI})
	if err != nil {
		return nil, provider.Fail(c.op, "the request could not be built")
	}
	return changeHomepage(ctx, resolved, secrets, red, c, id, needsProof, request{method: http.MethodPatch,
		path: homepagePath(id), query: url.Values{"updateMask": {"uri"}}, body: body, change: true})
}

func handleHomepagesClaim(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage) (any, error) {
	c, err := prepare("claim home page", homepagesClaimID, resolved, raw)
	if err != nil {
		return nil, err
	}
	a, err := readHomepageArguments(raw)
	if err != nil {
		return nil, err
	}
	if a.Account == "" {
		return nil, invalidArgument("account is required")
	}
	id, needsProof, err := c.scope.accountOf(a.Account)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(struct {
		Overwrite bool `json:"overwrite,omitempty"`
	}{a.Overwrite})
	if err != nil {
		return nil, provider.Fail(c.op, "the request could not be built")
	}
	return changeHomepage(ctx, resolved, secrets, red, c, id, needsProof, request{method: http.MethodPost,
		path: homepagePath(id) + ":claim", body: body, change: true})
}

func handleHomepagesUnclaim(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage) (any, error) {
	c, err := prepare("unclaim home page", homepagesUnclaimID, resolved, raw)
	if err != nil {
		return nil, err
	}
	a, err := readHomepageArguments(raw)
	if err != nil {
		return nil, err
	}
	if a.Account == "" {
		return nil, invalidArgument("account is required")
	}
	id, needsProof, err := c.scope.accountOf(a.Account)
	if err != nil {
		return nil, err
	}
	// The unclaim method takes no request body.
	return changeHomepage(ctx, resolved, secrets, red, c, id, needsProof, request{method: http.MethodPost,
		path: homepagePath(id) + ":unclaim", change: true})
}

// homepageSummaries phrase what a confirmed call would do, from its arguments alone.
func homepageSummaries() map[string]capability.SummaryFunc {
	return map[string]capability.SummaryFunc{
		homepagesUpdateID: func(raw json.RawMessage) string {
			a, err := readHomepageArguments(raw)
			if err != nil || a.URI == nil {
				return ""
			}
			return "Set the home page address of Merchant Center account " + a.Account + " to " + strconv.Quote(*a.URI) +
				"; customers are sent there and the claim of the old address can be lost"
		},
		homepagesClaimID: func(raw json.RawMessage) string {
			a, err := readHomepageArguments(raw)
			if err != nil {
				return ""
			}
			out := "Claim the home page of Merchant Center account " + a.Account
			if a.Overwrite {
				out += "; overwrite: WARNING, removes an existing claim on the website held by another account " +
					"and transfers it to this account"
			}
			return out
		},
		homepagesUnclaimID: func(raw json.RawMessage) string {
			a, err := readHomepageArguments(raw)
			if err != nil {
				return ""
			}
			return "Remove the claim of the home page of Merchant Center account " + a.Account
		},
	}
}
