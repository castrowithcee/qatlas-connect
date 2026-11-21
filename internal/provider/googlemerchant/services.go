package googlemerchant

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strconv"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	accountServicesListID      = Provider + ".account-services.list"
	accountServicesGetID       = Provider + ".account-services.get"
	accountServicesRejectID    = Provider + ".account-services.reject"
	accountRelationshipsListID = Provider + ".account-relationships.list"
)

// The discovery document types the service as one path segment without a slash and shows no further form.
// Qatlas accepts letters, digits, dash, and underscore, at most 64, so an ID can never add a path segment, a
// dot, or an escape; whether Google knows the ID is its answer.
const (
	serviceIDPattern = `[A-Za-z0-9_-]{1,64}`
	// servicesPageSize is sent when page_size is left out, the default of Google.
	servicesPageSize = 100
	maxServicesPage  = 1000
)

var serviceIDForm = regexp.MustCompile(`^` + serviceIDPattern + `$`)

const (
	serviceArgSchema = `{"type":"string","minLength":1,"maxLength":64,"pattern":"^` + serviceIDPattern + `$",` +
		`"x-form":"the service ID, the last segment of accounts/ACCOUNT/services/SERVICE"}`
	servicesPageSizeSchema = `{"type":"integer","minimum":1,"maximum":1000}`
)

const (
	serviceFieldsDescription = "name (accounts/ACCOUNT/services/SERVICE), provider, providerDisplayName, " +
		"externalAccountId, handshake (approvalState PENDING, WAITING, ESTABLISHED, or REJECTED, and actor ACCOUNT " +
		"or OTHER_PARTY), mutability (MUTABLE or IMMUTABLE), and the kind of the service as one of accountManagement, " +
		"productsManagement, campaignsManagement, localListingManagement, accountAggregation, or comparisonShopping " +
		"(an empty object that says which kind it is). providerDisplayName is shown only for a provider the " +
		"connection's targets name, for Google Ads and Google Business Profile, or on a connection without targets"
	servicesUntrusted = "Provider names and aliases come from Google and the provider account and are shown as " +
		"untrusted text: texts are cut, lists are capped, and a member this tool does not list is dropped. Linked " +
		"provider accounts outside the targets of the connection are shown by their identifier only."
	servicesOnAdvanced = "A service belongs to the advanced account itself, so on a connection bound to targets this " +
		"tool needs the advanced account target of the account."
	servicesRejectNote = "Rejecting a service ends the access the service gave the provider account: a service " +
		"provider that managed products, campaigns, or sub-accounts of the account through it loses that access, and " +
		"a proposed or an approved service can be rejected. Rejecting needs the role Admin on the account; a missing " +
		"role is answered with a permission error. A service that Google marks IMMUTABLE, for example one created " +
		"through another system, cannot be changed here and Google refuses the call. Google processes the change " +
		"afterwards and the access can take a while to end: the result says only that Google accepted the request, " +
		"so read the service again to see its state. A failed call may or may not have changed the service; it is " +
		"never repeated automatically. The tool is offered only when the connection lists it in tools, and every " +
		"call needs a confirmation that shows the account and the service. Qatlas sends a fixed empty request. " +
		"There is no tool to propose or approve a service: giving another account access is not offered."
)

type serviceArguments struct {
	Account  string `json:"account"`
	Service  string `json:"service"`
	PageSize int    `json:"page_size"`
	Cursor   string `json:"cursor"`
}

var serviceArgument = capability.Argument{Name: "service", Required: true, Description: "Service ID, the last segment of " +
	"the name accounts/ACCOUNT/services/SERVICE, letters, digits, dash, and underscore, at most 64 characters"}

var (
	accountServicesList = capability.Descriptor{
		ID: accountServicesListID, Version: 1, Title: "List the services of an advanced account",
		Description: "List the services the account gives to or receives from provider accounts, such as account " +
			"aggregation or product management, with the state of each handshake. Google sends up to 1000 services per " +
			"batch; Qatlas asks for 100 when page_size is left out, and cursor reads on. Google documents no filter " +
			"for this list. " + servicesOnAdvanced + " " + servicesUntrusted,
		Tags: []string{"google", "merchant", "account-services"}, Risk: readRisk, Provider: Provider,
		InputSchema: inputSchema(nil, member("account", accountArgSchema), member("page_size", servicesPageSizeSchema),
			member("cursor", cursorSchema)),
		OutputSchema: json.RawMessage(listResultSchema("accountServices", accountServiceSchema)),
		Arguments: []capability.Argument{advancedArgument,
			{Name: "page_size", Description: "Services per batch, 1 to 1000; 100 when left out"}, cursorArgument},
		Fields: []capability.Field{
			{Name: "accountServices", Description: "Services with " + serviceFieldsDescription},
			{Name: "next_cursor", Description: "Cursor to read on with; missing at the end"},
		},
		Examples: []capability.Example{{Description: "Services of one account", Arguments: json.RawMessage(`{"account":"1000001"}`)}},
	}
	accountServicesGet = capability.Descriptor{
		ID: accountServicesGetID, Version: 1, Title: "Get a service of an advanced account",
		Description: "Read one service of an account with the state of its handshake. After a service is rejected or " +
			"approved it can take a while until the state shows. " + servicesOnAdvanced + " " + servicesUntrusted,
		Tags: []string{"google", "merchant", "account-services"}, Risk: readRisk, Provider: Provider,
		InputSchema: inputSchema([]string{"service"}, member("account", accountArgSchema),
			member("service", serviceArgSchema)),
		OutputSchema: json.RawMessage(accountServiceSchema),
		Arguments:    []capability.Argument{advancedArgument, serviceArgument},
		Fields:       []capability.Field{{Name: "service", Description: serviceFieldsDescription}},
		Examples: []capability.Example{{Description: "One service",
			Arguments: json.RawMessage(`{"account":"1000001","service":"2000002"}`)}},
	}
	accountServicesReject = capability.Descriptor{
		ID: accountServicesRejectID, Version: 1, Title: "Reject a service of an advanced account",
		Description: "Reject a proposed or an approved service of an account. " + servicesRejectNote + " " +
			servicesOnAdvanced + " " + servicesUntrusted,
		Tags: []string{"google", "merchant", "account-services"}, Provider: Provider,
		Risk: capability.Risk{Effect: capability.EffectUpdate, Idempotency: capability.IdempotencyUnknown,
			Confirmation: capability.ConfirmationRequired, OpenWorld: true, DataSensitivity: "merchant-account-data"},
		RequiresToolAllowList: true,
		InputSchema: inputSchema([]string{"service"}, member("account", accountArgSchema),
			member("service", serviceArgSchema)),
		OutputSchema: json.RawMessage(serviceRejectSchema),
		Arguments:    []capability.Argument{advancedArgument, serviceArgument},
		Fields: []capability.Field{
			{Name: "accepted", Description: "true when Google accepted the request; nothing is said about processing"},
			{Name: "name", Description: "accounts/ACCOUNT/services/SERVICE of the rejected service"},
		},
		Examples: []capability.Example{{Description: "Reject one service",
			Arguments: json.RawMessage(`{"account":"1000001","service":"2000002"}`)}},
	}
	accountRelationshipsList = capability.Descriptor{
		ID: accountRelationshipsListID, Version: 1, Title: "List the relationships of a Merchant Center account",
		Description: "List the relationships of an account with provider accounts: which provider, and the alias " +
			"the account gave the relationship. Google sends up to 1000 relationships per batch; Qatlas asks for 100 " +
			"when page_size is left out, and cursor reads on. An account outside the targets of the connection is " +
			"refused without a request; a sub-account of a bound advanced account is proven by reading the " +
			"sub-accounts first. " + servicesUntrusted,
		Tags: []string{"google", "merchant", "account-relationships"}, Risk: readRisk, Provider: Provider,
		InputSchema: inputSchema(nil, member("account", accountArgSchema), member("page_size", servicesPageSizeSchema),
			member("cursor", cursorSchema)),
		OutputSchema: json.RawMessage(listResultSchema("accountRelationships", accountRelationshipSchema)),
		Arguments: []capability.Argument{accountArgument,
			{Name: "page_size", Description: "Relationships per batch, 1 to 1000; 100 when left out"}, cursorArgument},
		Fields: []capability.Field{
			{Name: "accountRelationships", Description: "Relationships with " + relationshipFieldsDescription},
			{Name: "next_cursor", Description: "Cursor to read on with; missing at the end"},
		},
		Examples: []capability.Example{{Description: "Relationships of one account", Arguments: json.RawMessage(`{"account":"1000001"}`)}},
	}
)

// serviceKind is the empty payload of a service type: its presence says which kind the service is.
type serviceKind struct{}

type serviceHandshake struct {
	Actor         idText `json:"actor,omitempty"`
	ApprovalState idText `json:"approvalState,omitempty"`
}

// accountService is the AccountService resource. Every member the document lists is output; others are never
// held. A kind member that is not an object is dropped.
type accountService struct {
	Name                   idText            `json:"name,omitempty"`
	Provider               idText            `json:"provider,omitempty"`
	ProviderDisplayName    nameText          `json:"providerDisplayName,omitempty"`
	ExternalAccountID      idText            `json:"externalAccountId,omitempty"`
	Handshake              *serviceHandshake `json:"handshake,omitempty"`
	Mutability             idText            `json:"mutability,omitempty"`
	AccountManagement      *serviceKind      `json:"accountManagement,omitempty"`
	ProductsManagement     *serviceKind      `json:"productsManagement,omitempty"`
	CampaignsManagement    *serviceKind      `json:"campaignsManagement,omitempty"`
	LocalListingManagement *serviceKind      `json:"localListingManagement,omitempty"`
	AccountAggregation     *serviceKind      `json:"accountAggregation,omitempty"`
	ComparisonShopping     *serviceKind      `json:"comparisonShopping,omitempty"`
}

// object reports whether raw is a JSON object, the only form of a handshake or a service kind.
func object(raw json.RawMessage) bool { return len(raw) > 0 && raw[0] == '{' }

func kindOf(raw json.RawMessage) *serviceKind {
	if object(raw) {
		return &serviceKind{}
	}
	return nil
}

func (s *accountService) UnmarshalJSON(b []byte) error {
	var raw struct {
		Name                   idText          `json:"name"`
		Provider               idText          `json:"provider"`
		ProviderDisplayName    nameText        `json:"providerDisplayName"`
		ExternalAccountID      idText          `json:"externalAccountId"`
		Handshake              json.RawMessage `json:"handshake"`
		Mutability             idText          `json:"mutability"`
		AccountManagement      json.RawMessage `json:"accountManagement"`
		ProductsManagement     json.RawMessage `json:"productsManagement"`
		CampaignsManagement    json.RawMessage `json:"campaignsManagement"`
		LocalListingManagement json.RawMessage `json:"localListingManagement"`
		AccountAggregation     json.RawMessage `json:"accountAggregation"`
		ComparisonShopping     json.RawMessage `json:"comparisonShopping"`
	}
	*s = accountService{}
	if json.Unmarshal(b, &raw) != nil {
		return nil
	}
	*s = accountService{Name: raw.Name, Provider: raw.Provider, ProviderDisplayName: raw.ProviderDisplayName,
		ExternalAccountID: raw.ExternalAccountID, Mutability: raw.Mutability,
		AccountManagement: kindOf(raw.AccountManagement), ProductsManagement: kindOf(raw.ProductsManagement),
		CampaignsManagement: kindOf(raw.CampaignsManagement), LocalListingManagement: kindOf(raw.LocalListingManagement),
		AccountAggregation: kindOf(raw.AccountAggregation), ComparisonShopping: kindOf(raw.ComparisonShopping)}
	if object(raw.Handshake) {
		var h serviceHandshake
		if json.Unmarshal(raw.Handshake, &h) == nil {
			s.Handshake = &h
		}
	}
	return nil
}

// masked drops the display name of a provider the binding does not show.
func (s accountService) masked(sc scope) accountService {
	if !sc.showsProviderName(s.Provider) {
		s.ProviderDisplayName = ""
	}
	return s
}

type accountServicesAnswer struct {
	AccountServices []accountService `json:"accountServices"`
	NextPageToken   string           `json:"nextPageToken"`
}

type accountServicesResult struct {
	AccountServices []accountService `json:"accountServices"`
	NextCursor      string           `json:"next_cursor,omitempty"`
}

type serviceRejectResult struct {
	Accepted bool   `json:"accepted"`
	Name     string `json:"name"`
}

const serviceKindSchema = `{"type":"object"}`

var (
	accountServiceSchema = objectSchema(member("name", stringSchema), member("provider", stringSchema),
		member("providerDisplayName", stringSchema), member("externalAccountId", stringSchema),
		member("handshake", objectSchema(member("actor", stringSchema), member("approvalState", stringSchema))),
		member("mutability", stringSchema), member("accountManagement", serviceKindSchema),
		member("productsManagement", serviceKindSchema), member("campaignsManagement", serviceKindSchema),
		member("localListingManagement", serviceKindSchema), member("accountAggregation", serviceKindSchema),
		member("comparisonShopping", serviceKindSchema))
	serviceRejectSchema = objectSchema(member("accepted", boolSchema), member("name", stringSchema))
)

func servicesPath(account string) string { return accountPath(account) + "/services" }

func servicePath(account, id string) string { return servicesPath(account) + "/" + id }

func relationshipsPath(account string) string { return accountPath(account) + "/relationships" }

// bindService reads the validated arguments, refusing any member they do not name, and settles the
// connection, the account, and, when needed, the service ID. Nothing is requested and no secret is read.
// onAdvanced selects the advanced account argument; otherwise the account argument may need a proof.
func bindService(resolved *config.Resolved, raw json.RawMessage, onAdvanced, needService bool) (
	args serviceArguments, bound scope, account string, needsProof bool, err error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&args) != nil {
		return args, bound, "", false, invalidArgument("the arguments hold a member or a value this tool does not take")
	}
	if bound, err = checkConnection(resolved); err != nil {
		return args, bound, "", false, err
	}
	if onAdvanced {
		account, err = bound.advancedOf(args.Account)
	} else {
		account, needsProof, err = bound.accountOf(args.Account)
	}
	if err != nil {
		return args, bound, "", false, err
	}
	switch {
	case needService && !serviceIDForm.MatchString(args.Service):
		return args, bound, "", false, invalidArgument("service must be a service ID of letters, digits, dash, and " +
			"underscore, at most 64 characters")
	case !needService && args.Service != "":
		return args, bound, "", false, invalidArgument("service is not an argument of this tool")
	case needService && (args.PageSize != 0 || args.Cursor != ""):
		return args, bound, "", false, invalidArgument("page_size and cursor are not arguments of this tool")
	}
	return args, bound, account, needsProof, nil
}

// servicePage builds the query of a list call and the binding of its cursor.
func servicePage(tool, account string, args serviceArguments, bound scope) (url.Values, []byte, error) {
	binding := provider.CursorBinding(tool, account, args.PageSize, bound.key())
	pageSize := servicesPageSize
	if args.PageSize > 0 {
		pageSize = min(args.PageSize, maxServicesPage)
	}
	query := url.Values{"pageSize": {strconv.Itoa(pageSize)}}
	if args.Cursor != "" {
		token, ok := provider.DecodeCursor(binding, args.Cursor, maxCursorSize)
		if !ok || token == "" {
			return nil, nil, refusedCursor()
		}
		query.Set("pageToken", token)
	}
	return query, binding, nil
}

func handleAccountServicesList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list account services"
	args, bound, account, _, err := bindService(resolved, raw, true, false)
	if err != nil {
		return nil, err
	}
	query, binding, err := servicePage(accountServicesListID, account, args, bound)
	if err != nil {
		return nil, err
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var answer accountServicesAnswer
	if err := cl.get(ctx, op, accountServicesListID, servicesPath(account), query, &answer); err != nil {
		return nil, err
	}
	if len(answer.AccountServices) > maxEntries {
		return nil, tooMany(op)
	}
	out := accountServicesResult{AccountServices: make([]accountService, 0, len(answer.AccountServices))}
	for _, s := range answer.AccountServices {
		out.AccountServices = append(out.AccountServices, s.masked(bound))
	}
	if out.NextCursor, err = nextCursor(op, binding, answer.NextPageToken); err != nil {
		return nil, err
	}
	return out, nil
}

func handleAccountServicesGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "get account service"
	args, bound, account, _, err := bindService(resolved, raw, true, true)
	if err != nil {
		return nil, err
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var out accountService
	if err := cl.get(ctx, op, accountServicesGetID, servicePath(account, args.Service), nil, &out); err != nil {
		return nil, err
	}
	return out.masked(bound), nil
}

// handleAccountServicesReject sends the fixed empty request once. Nothing of the body comes from the
// arguments, and a failure is reported with its open outcome, never repeated.
func handleAccountServicesReject(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "reject account service"
	args, _, account, _, err := bindService(resolved, raw, true, true)
	if err != nil {
		return nil, err
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	path := servicePath(account, args.Service)
	var empty struct{}
	if err := cl.send(ctx, op, accountServicesRejectID, request{method: http.MethodPost, path: path + ":reject",
		body: []byte(`{}`), change: true}, &empty); err != nil {
		return nil, err
	}
	return serviceRejectResult{Accepted: true, Name: path[len(accountsAPI):]}, nil
}

func handleAccountRelationshipsList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list account relationships"
	args, bound, account, needsProof, err := bindService(resolved, raw, false, false)
	if err != nil {
		return nil, err
	}
	query, binding, err := servicePage(accountRelationshipsListID, account, args, bound)
	if err != nil {
		return nil, err
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if needsProof {
		if err := cl.prove(ctx, op, accountRelationshipsListID, account); err != nil {
			return nil, err
		}
	}
	var answer accountRelationshipsAnswer
	if err := cl.get(ctx, op, accountRelationshipsListID, relationshipsPath(account), query, &answer); err != nil {
		return nil, err
	}
	if len(answer.AccountRelationships) > maxEntries {
		return nil, tooMany(op)
	}
	out := accountRelationshipsResult{
		AccountRelationships: make([]accountRelationship, 0, len(answer.AccountRelationships))}
	for _, r := range answer.AccountRelationships {
		out.AccountRelationships = append(out.AccountRelationships, r.masked(bound))
	}
	if out.NextCursor, err = nextCursor(op, binding, answer.NextPageToken); err != nil {
		return nil, err
	}
	return out, nil
}

// serviceRejectSummary phrases the confirmation from the validated arguments alone.
func serviceRejectSummary(raw json.RawMessage) string {
	var a serviceArguments
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	out := "Reject service " + strconv.Quote(a.Service)
	if a.Account != "" {
		out += " of account " + a.Account
	} else {
		out += " of the account of this connection"
	}
	return out + "; the provider account of the service loses the access the service gave it, and Google processes " +
		"the change afterwards"
}
