package googlemerchant

import (
	"context"
	"encoding/json"
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

const (
	checkoutSettingsGetID    = Provider + ".checkout-settings.get"
	checkoutSettingsCreateID = Provider + ".checkout-settings.create"
	checkoutSettingsUpdateID = Provider + ".checkout-settings.update"
	checkoutSettingsDeleteID = Provider + ".checkout-settings.delete"
)

// checkoutProgram is the one program ID the checkout settings accept; it is part of every path and never an
// argument.
const checkoutProgram = "checkout"

// The API documents no length for a URI template; the bound is ours and equals what a result shows.
const maxCheckoutTemplateRunes = maxURLRunes

var checkoutDestinations = []string{"SHOPPING_ADS", "FREE_LISTINGS"}

// checkoutURISettings is the UriSettings message of a result. Google sets exactly one of the two members.
type checkoutURISettings struct {
	CheckoutURITemplate urlText `json:"checkoutUriTemplate,omitempty"`
	CartURITemplate     urlText `json:"cartUriTemplate,omitempty"`
}

// checkoutSettings is the CheckoutSettings resource. The state members are output-only and never an
// argument.
type checkoutSettings struct {
	Name                     idText               `json:"name,omitempty"`
	EligibleDestinations     idList               `json:"eligibleDestinations,omitempty"`
	EffectiveURISettings     *checkoutURISettings `json:"effectiveUriSettings,omitempty"`
	URISettings              *checkoutURISettings `json:"uriSettings,omitempty"`
	EnrollmentState          idText               `json:"enrollmentState,omitempty"`
	ReviewState              idText               `json:"reviewState,omitempty"`
	EffectiveEnrollmentState idText               `json:"effectiveEnrollmentState,omitempty"`
	EffectiveReviewState     idText               `json:"effectiveReviewState,omitempty"`
}

var (
	checkoutURISettingsSchema = objectSchema(member("checkoutUriTemplate", stringSchema),
		member("cartUriTemplate", stringSchema))
	checkoutSettingsSchema = objectSchema(member("name", stringSchema), member("eligibleDestinations", listSchema),
		member("effectiveUriSettings", checkoutURISettingsSchema), member("uriSettings", checkoutURISettingsSchema),
		member("enrollmentState", stringSchema), member("reviewState", stringSchema),
		member("effectiveEnrollmentState", stringSchema), member("effectiveReviewState", stringSchema))
	checkoutDeletedSchema = `{"type":"object","properties":{"account":{"type":"string"},"deleted":{"type":"boolean"}}}`
)

const (
	checkoutTemplateSchema = `{"type":"string","minLength":1,"maxLength":2048,` +
		`"x-form":"absolute https address without user information; {id} stands for the offer ID"}`
	checkoutURISettingsInputSchema = `{"type":"object","properties":{"checkout_uri_template":` + checkoutTemplateSchema +
		`,"cart_uri_template":` + checkoutTemplateSchema + `},"minProperties":1,"maxProperties":1,` +
		`"additionalProperties":false}`
	checkoutDestinationsSchema = `{"type":"array","items":{"type":"string","enum":["SHOPPING_ADS","FREE_LISTINGS"]},` +
		`"minItems":1,"maxItems":2,"uniqueItems":true}`
)

const (
	checkoutFieldsDescription = "name (accounts/ACCOUNT/programs/checkout/checkoutSettings), eligibleDestinations, " +
		"uriSettings and effectiveUriSettings (checkoutUriTemplate or cartUriTemplate), enrollmentState, " +
		"reviewState, effectiveEnrollmentState, and effectiveReviewState"
	checkoutUntrustedNote = "The templates are untrusted content of whoever runs the account: texts are cut, and " +
		"templates are text that is never fetched."
	checkoutMembers = "eligible_destinations (SHOPPING_ADS and FREE_LISTINGS) and uri_settings, which holds exactly " +
		"one of checkout_uri_template (sends buyers to the checkout page with the item) and cart_uri_template " +
		"(sends buyers to the cart page with the item); a template is an absolute https address with a host and " +
		"without user information, at most 2048 characters, and Qatlas refuses anything else before any request; " +
		"{id} in a template is replaced by the offer ID of the product, and Google refuses a template that does " +
		"not fit its rules"
	checkoutDelayNote = "Google accepts the change and then reviews the template: the review state is IN_REVIEW after " +
		"every change of an address, so the answer only tells that Google accepted it. Every call counts against " +
		"the quota of the Merchant API, a failed one too, and a call is never repeated: when the outcome is " +
		"unknown, read the settings before changing them again. The templates steer customers to your shop. " +
		"Needs confirmation."
)

func checkoutRisk(effect capability.Effect, idempotency capability.Idempotency) capability.Risk {
	return capability.Risk{Effect: effect, Idempotency: idempotency, Confirmation: capability.ConfirmationRequired,
		OpenWorld: true, DataSensitivity: "merchant-account-data"}
}

var checkoutAccountArgument = capability.Argument{Name: "account", Target: kindAccount, Description: "Merchant " +
	"Center account ID, digits only; the tool refuses a call without it; it must be named by a target or be a " +
	"direct sub-account of a bound advanced account"}

var (
	checkoutSettingsGet = capability.Descriptor{
		ID: checkoutSettingsGetID, Version: 1, Title: "Get the checkout settings of an account",
		Description: "Read the checkout settings of one account (accounts.programs.checkoutSettings.get): the " +
			"destinations, the checkout or cart template, and the enrollment and review states of the Checkout " +
			"program. On a " +
			"connection bound to an advanced account, an account that is not named by a target is read only " +
			"after it is found among the direct sub-accounts of a bound advanced account, which costs further " +
			"calls. After a change it can take time until the states settle. " + checkoutUntrustedNote,
		Tags: []string{"google", "merchant", "checkout-settings"}, Risk: readRisk, Provider: Provider,
		InputSchema:  inputSchema(nil, member("account", accountArgSchema)),
		OutputSchema: json.RawMessage(checkoutSettingsSchema),
		Arguments:    []capability.Argument{accountArgument},
		Fields:       []capability.Field{{Name: "checkoutSettings", Description: checkoutFieldsDescription}},
		Examples: []capability.Example{{Description: "Checkout settings of one account",
			Arguments: json.RawMessage(`{"account":"1000001"}`)}},
	}

	checkoutSettingsCreate = capability.Descriptor{
		ID: checkoutSettingsCreateID, Version: 1, Title: "Create the checkout settings of an account",
		Description: "Enroll an account in the Checkout program (accounts.programs.checkoutSettings.create). " +
			"eligible_destinations is required. The members are " + checkoutMembers + ". Google needs the account to " +
			"take part in Shopping ads or free listings and to sell in the United States. A template for the " +
			"whole account replaces per-product checkout links of the feed, so use one way only. Offered only " +
			"where a connection's tools list names it. " + checkoutDelayNote + " " + checkoutUntrustedNote,
		Tags: []string{"google", "merchant", "checkout-settings"}, Provider: Provider, RequiresToolAllowList: true,
		Risk: checkoutRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent),
		InputSchema: inputSchema([]string{"eligible_destinations"}, member("account", accountArgSchema),
			member("eligible_destinations", checkoutDestinationsSchema),
			member("uri_settings", checkoutURISettingsInputSchema)),
		OutputSchema: json.RawMessage(checkoutSettingsSchema),
		Arguments: []capability.Argument{checkoutAccountArgument,
			{Name: "eligible_destinations", Required: true, Description: "Where checkout links show: SHOPPING_ADS, " +
				"FREE_LISTINGS, or both"},
			{Name: "uri_settings", Description: "The one template for the whole account: checkout_uri_template or " +
				"cart_uri_template; leave it out to select destinations only, for products that carry their own " +
				"checkout links"}},
		Fields: []capability.Field{{Name: "checkoutSettings", Description: "The settings as Google answered: " +
			checkoutFieldsDescription}},
		Examples: []capability.Example{{Description: "Enroll with a checkout template", Arguments: json.RawMessage(
			`{"account":"1000001","eligible_destinations":["FREE_LISTINGS"],` +
				`"uri_settings":{"checkout_uri_template":"https://shop.example.invalid/checkout?item_id={id}"}}`)}},
	}

	checkoutSettingsUpdate = capability.Descriptor{
		ID: checkoutSettingsUpdateID, Version: 1, Title: "Update the checkout settings of an account",
		Description: "Change the checkout settings of one account (accounts.programs.checkoutSettings.patch): only " +
			"the members you pass are sent, each named in the update mask, so every other member stays as it is. " +
			"At least one is required. The members are " + checkoutMembers + ". A member that is passed replaces " +
			"the whole member: eligible_destinations replaces the list, and uri_settings replaces the template, " +
			"so passing the other kind of template switches it. Offered only where a connection's tools list " +
			"names it. " + checkoutDelayNote + " " + checkoutUntrustedNote,
		Tags: []string{"google", "merchant", "checkout-settings"}, Provider: Provider, RequiresToolAllowList: true,
		Risk: checkoutRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
		InputSchema: inputSchema(nil, member("account", accountArgSchema),
			member("eligible_destinations", checkoutDestinationsSchema),
			member("uri_settings", checkoutURISettingsInputSchema)),
		OutputSchema: json.RawMessage(checkoutSettingsSchema),
		Arguments: []capability.Argument{checkoutAccountArgument,
			{Name: "eligible_destinations", Description: "Where checkout links show: SHOPPING_ADS, FREE_LISTINGS, or both"},
			{Name: "uri_settings", Description: "The one template for the whole account: checkout_uri_template or " +
				"cart_uri_template"}},
		Fields: []capability.Field{{Name: "checkoutSettings", Description: "The settings as Google answered: " +
			checkoutFieldsDescription}},
		Examples: []capability.Example{{Description: "Switch to a cart template", Arguments: json.RawMessage(
			`{"account":"1000001","uri_settings":{"cart_uri_template":"https://shop.example.invalid/cart?sku={id}"}}`)}},
	}

	checkoutSettingsDelete = capability.Descriptor{
		ID: checkoutSettingsDeleteID, Version: 1, Title: "Delete the checkout settings of an account",
		Description: "Delete the checkout settings of one account and unenroll it from the Checkout program " +
			"(accounts.programs.checkoutSettings.delete): checkout links stop showing on free listings and in " +
			"Shopping ads, and the template of the account is gone. The account argument is required. Offered " +
			"only where a connection's tools list names it. Provider content is untrusted data; the answer carries " +
			"none. " + checkoutDelayNote,
		Tags: []string{"google", "merchant", "checkout-settings"}, Provider: Provider, RequiresToolAllowList: true,
		Risk:         checkoutRisk(capability.EffectDelete, capability.IdempotencyUnknown),
		InputSchema:  inputSchema(nil, member("account", accountArgSchema)),
		OutputSchema: json.RawMessage(checkoutDeletedSchema),
		Arguments:    []capability.Argument{checkoutAccountArgument},
		Fields: []capability.Field{{Name: "account", Description: "ID of the account whose settings Google was asked " +
			"to delete"}, {Name: "deleted", Description: "true once Google accepted the deletion"}},
		Examples: []capability.Example{{Description: "Unenroll an account", Arguments: json.RawMessage(
			`{"account":"1000001"}`)}},
	}
)

func checkoutDescriptors() []capability.Descriptor {
	return []capability.Descriptor{checkoutSettingsGet, checkoutSettingsCreate, checkoutSettingsUpdate,
		checkoutSettingsDelete}
}

func checkoutHandlers() map[string]capability.Handler {
	return map[string]capability.Handler{
		checkoutSettingsGetID: handleCheckoutGet, checkoutSettingsCreateID: handleCheckoutCreate,
		checkoutSettingsUpdateID: handleCheckoutUpdate, checkoutSettingsDeleteID: handleCheckoutDelete,
	}
}

type (
	checkoutTemplates struct {
		CheckoutURITemplate *string `json:"checkout_uri_template"`
		CartURITemplate     *string `json:"cart_uri_template"`
	}
	// checkoutArguments holds the members as pointers: a member that was passed differs from one that was left
	// out, which decides the update mask.
	checkoutArguments struct {
		Account              string             `json:"account"`
		EligibleDestinations *[]string          `json:"eligible_destinations"`
		URISettings          *checkoutTemplates `json:"uri_settings"`
	}
)

// checkoutBody is the CheckoutSettings as sent: only members the caller passed, never an output member or the
// resource name.
type checkoutBody struct {
	EligibleDestinations []string                 `json:"eligibleDestinations,omitempty"`
	URISettings          *checkoutURISettingsBody `json:"uriSettings,omitempty"`
}

type checkoutURISettingsBody struct {
	CheckoutURITemplate string `json:"checkoutUriTemplate,omitempty"`
	CartURITemplate     string `json:"cartUriTemplate,omitempty"`
}

func readCheckoutArguments(raw json.RawMessage) (checkoutArguments, error) {
	var a checkoutArguments
	if err := json.Unmarshal(raw, &a); err != nil {
		return a, invalidArgument("the arguments could not be read")
	}
	return a, nil
}

// checkTemplate refuses a template that is too long or not plain https, with the one shared check.
func checkTemplate(name, raw string) error {
	if utf8.RuneCountInString(raw) > maxCheckoutTemplateRunes {
		return invalidArgument(name + " must be at most " + strconv.Itoa(maxCheckoutTemplateRunes) + " characters")
	}
	return checkHTTPSAddress(name, raw)
}

// check refuses a member that is not in the documented form, before any secret is read.
func (a checkoutArguments) check() error {
	if d := a.EligibleDestinations; d != nil {
		if len(*d) == 0 || len(*d) > len(checkoutDestinations) {
			return invalidArgument("eligible_destinations must name SHOPPING_ADS, FREE_LISTINGS, or both")
		}
		seen := map[string]bool{}
		for _, name := range *d {
			if !slices.Contains(checkoutDestinations, name) || seen[name] {
				return invalidArgument("eligible_destinations must name SHOPPING_ADS, FREE_LISTINGS, or both, each once")
			}
			seen[name] = true
		}
	}
	if u := a.URISettings; u != nil {
		if (u.CheckoutURITemplate == nil) == (u.CartURITemplate == nil) {
			return invalidArgument("uri_settings must hold exactly one of checkout_uri_template and cart_uri_template")
		}
		if u.CheckoutURITemplate != nil {
			return checkTemplate("uri_settings.checkout_uri_template", *u.CheckoutURITemplate)
		}
		return checkTemplate("uri_settings.cart_uri_template", *u.CartURITemplate)
	}
	return nil
}

// mask lists the members the call sets, in the lowerCamelCase the update mask of this method documents.
func (a checkoutArguments) mask() string {
	var fields []string
	if a.EligibleDestinations != nil {
		fields = append(fields, "eligibleDestinations")
	}
	if a.URISettings != nil {
		fields = append(fields, "uriSettings")
	}
	return strings.Join(fields, ",")
}

func (a checkoutArguments) body() checkoutBody {
	var b checkoutBody
	if a.EligibleDestinations != nil {
		b.EligibleDestinations = *a.EligibleDestinations
	}
	if u := a.URISettings; u != nil {
		b.URISettings = &checkoutURISettingsBody{}
		if u.CheckoutURITemplate != nil {
			b.URISettings.CheckoutURITemplate = *u.CheckoutURITemplate
		}
		if u.CartURITemplate != nil {
			b.URISettings.CartURITemplate = *u.CartURITemplate
		}
	}
	return b
}

func checkoutSettingsPath(id string) string {
	return accountPath(id) + "/programs/" + checkoutProgram + "/checkoutSettings"
}

func handleCheckoutGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("get checkout settings", checkoutSettingsGetID, resolved, raw)
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
	var out checkoutSettings
	if err := cl.get(ctx, c.op, c.tool, checkoutSettingsPath(id), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// changeCheckout runs the shared path of the write tools: the account was settled against the targets before
// any secret is resolved, it is proven if needed, and the change is sent once. A nil out expects no content.
func changeCheckout(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	c call, id string, needsProof bool, r request, out any) error {
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return err
	}
	if needsProof {
		if err := cl.prove(ctx, c.op, c.tool, id); err != nil {
			return err
		}
	}
	return cl.send(ctx, c.op, c.tool, r, out)
}

// writeCheckout reads and settles the arguments of a write tool. A change names its account: the one account
// of a binding is not assumed.
func writeCheckout(op, tool string, resolved *config.Resolved, raw json.RawMessage) (call, checkoutArguments, error) {
	c, err := prepare(op, tool, resolved, raw)
	if err != nil {
		return c, checkoutArguments{}, err
	}
	a, err := readCheckoutArguments(raw)
	if err != nil {
		return c, a, err
	}
	if a.Account == "" {
		return c, a, invalidArgument("account is required")
	}
	return c, a, nil
}

func handleCheckoutCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, a, err := writeCheckout("create checkout settings", checkoutSettingsCreateID, resolved, raw)
	if err != nil {
		return nil, err
	}
	if a.EligibleDestinations == nil {
		return nil, invalidArgument("eligible_destinations is required")
	}
	if err := a.check(); err != nil {
		return nil, err
	}
	id, needsProof, err := c.scope.accountOf(a.Account)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(a.body())
	if err != nil {
		return nil, provider.Fail(c.op, "the request could not be built")
	}
	r := request{method: http.MethodPost, path: checkoutSettingsPath(id), body: body, change: true}
	var out checkoutSettings
	if err := changeCheckout(ctx, resolved, secrets, red, c, id, needsProof, r, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func handleCheckoutUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, a, err := writeCheckout("update checkout settings", checkoutSettingsUpdateID, resolved, raw)
	if err != nil {
		return nil, err
	}
	if a.mask() == "" {
		return nil, invalidArgument("pass at least one of eligible_destinations and uri_settings")
	}
	if err := a.check(); err != nil {
		return nil, err
	}
	id, needsProof, err := c.scope.accountOf(a.Account)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(a.body())
	if err != nil {
		return nil, provider.Fail(c.op, "the request could not be built")
	}
	r := request{method: http.MethodPatch, path: checkoutSettingsPath(id),
		query: url.Values{"updateMask": {a.mask()}}, body: body, change: true}
	var out checkoutSettings
	if err := changeCheckout(ctx, resolved, secrets, red, c, id, needsProof, r, &out); err != nil {
		return nil, err
	}
	return out, nil
}

type checkoutDeleteResult struct {
	Account string `json:"account"`
	Deleted bool   `json:"deleted"`
}

func handleCheckoutDelete(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, a, err := writeCheckout("delete checkout settings", checkoutSettingsDeleteID, resolved, raw)
	if err != nil {
		return nil, err
	}
	id, needsProof, err := c.scope.accountOf(a.Account)
	if err != nil {
		return nil, err
	}
	r := request{method: http.MethodDelete, path: checkoutSettingsPath(id), change: true}
	if err := changeCheckout(ctx, resolved, secrets, red, c, id, needsProof, r, nil); err != nil {
		return nil, err
	}
	return checkoutDeleteResult{Account: id, Deleted: true}, nil
}

// checkoutSummaries phrase what a confirmed call would do, from its arguments alone.
func checkoutSummaries() map[string]capability.SummaryFunc {
	return map[string]capability.SummaryFunc{
		checkoutSettingsCreateID: func(raw json.RawMessage) string {
			a, err := readCheckoutArguments(raw)
			if err != nil {
				return ""
			}
			return "Enroll Merchant Center account " + a.Account + " in the Checkout program" + a.members()
		},
		checkoutSettingsUpdateID: func(raw json.RawMessage) string {
			a, err := readCheckoutArguments(raw)
			if err != nil {
				return ""
			}
			return "Update the checkout settings of Merchant Center account " + a.Account + ": set " +
				strings.ReplaceAll(a.mask(), ",", ", ") + a.members()
		},
		checkoutSettingsDeleteID: func(raw json.RawMessage) string {
			a, err := readCheckoutArguments(raw)
			if err != nil {
				return ""
			}
			return "Delete the checkout settings of Merchant Center account " + a.Account +
				" and unenroll it from the Checkout program"
		},
	}
}

// members names the destinations and the template of the call; the template steers customers.
func (a checkoutArguments) members() string {
	var out []string
	if a.EligibleDestinations != nil {
		out = append(out, "destinations "+strings.Join(*a.EligibleDestinations, ", "))
	}
	if u := a.URISettings; u != nil {
		if u.CheckoutURITemplate != nil {
			out = append(out, "checkout template "+strconv.Quote(*u.CheckoutURITemplate))
		}
		if u.CartURITemplate != nil {
			out = append(out, "cart template "+strconv.Quote(*u.CartURITemplate))
		}
	}
	if len(out) == 0 {
		return ""
	}
	return "; " + strings.Join(out, ", ")
}
