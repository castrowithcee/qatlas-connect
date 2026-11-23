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
	programsListID    = Provider + ".programs.list"
	programsGetID     = Provider + ".programs.get"
	programsEnableID  = Provider + ".programs.enable"
	programsDisableID = Provider + ".programs.disable"
)

// The discovery document types a program ID as one path segment and lists only lowercase IDs with dashes
// (free-listings, shopping-ads). Qatlas accepts that shape and no more, so an ID can never add a path segment,
// a dot, or an escape; whether Google knows the ID is its answer.
const (
	programIDPattern = `[a-z][a-z0-9-]{0,39}`
	// programsPageSize is sent when page_size is left out: Google would send up to 1000 programs.
	programsPageSize = 100
	maxProgramsPage  = 1000
)

var programIDForm = regexp.MustCompile(`^` + programIDPattern + `$`)

const (
	programArgSchema = `{"type":"string","maxLength":40,"pattern":"^` + programIDPattern + `$",` +
		`"x-form":"the program ID, such as free-listings or shopping-ads"}`
	programsPageSizeSchema = `{"type":"integer","minimum":1,"maximum":1000}`
)

const (
	programFieldsDescription = "name (accounts/ACCOUNT/programs/PROGRAM), state (NOT_ELIGIBLE, ELIGIBLE, ENABLED), " +
		"activeRegionCodes (CLDR codes where all requirements are met, 001 is global), unmetRequirements (each " +
		"with title, affectedRegionCodes, and documentationUri), and documentationUri"
	programsUntrusted = "Titles and help page addresses come from Google and are shown as untrusted text: texts are " +
		"cut, lists are capped at 100 entries, and addresses are never fetched."
	programsRoleNote = "Changing participation needs the Admin role in Merchant Center; per the Merchant Center help the " +
		"Standard role cannot enable programs, and Google answers a missing role with a permission error."
	programsChangeNote = "Enabling or disabling a program changes what Google can show publicly in Google Shopping and " +
		"other surfaces of the account. The change is processed by Google afterwards and can take a while to show: " +
		"the result says only that Google accepted the request, so read the program again to see the state. " +
		"A failed call may or may not have changed the program; it is never repeated automatically."
)

type programArguments struct {
	Account  string `json:"account"`
	Program  string `json:"program"`
	PageSize int    `json:"page_size"`
	Cursor   string `json:"cursor"`
}

func programChangeRisk() capability.Risk {
	return capability.Risk{Effect: capability.EffectUpdate, Idempotency: capability.IdempotencyIdempotent,
		Confirmation: capability.ConfirmationRequired, OpenWorld: true, DataSensitivity: "merchant-account-data"}
}

var programArgument = capability.Argument{Name: "program", Required: true, Description: "Program ID, lowercase letters, " +
	"digits, and dashes, at most 40 characters, such as free-listings, shopping-ads, checkout, or loyalty"}

var (
	programsList = capability.Descriptor{
		ID: programsListID, Version: 1, Title: "List the programs of a Merchant Center account",
		Description: "List the programs (such as free listings or shopping ads) of a Merchant Center account with the " +
			"participation state, the regions where the program is active, and the unmet requirements that affect " +
			"participation. Google sends up to 1000 programs per batch; Qatlas asks for 100 when page_size is left " +
			"out, and cursor reads on. An account outside the targets of the connection is refused without a request. " +
			programsUntrusted,
		Tags: []string{"google", "merchant", "programs"}, Risk: readRisk, Provider: Provider,
		InputSchema: inputSchema(nil, member("account", accountArgSchema), member("page_size", programsPageSizeSchema),
			member("cursor", cursorSchema)),
		OutputSchema: json.RawMessage(listResultSchema("programs", programSchema)),
		Arguments: []capability.Argument{accountArgument,
			{Name: "page_size", Description: "Programs per batch, 1 to 1000; 100 when left out"}, cursorArgument},
		Fields: []capability.Field{
			{Name: "programs", Description: "Programs with " + programFieldsDescription},
			{Name: "next_cursor", Description: "Cursor to read on with; missing at the end"},
		},
		Examples: []capability.Example{{Description: "Programs of one account", Arguments: json.RawMessage(`{"account":"1000001"}`)}},
	}
	programsGet = capability.Descriptor{
		ID: programsGetID, Version: 1, Title: "Get a program of a Merchant Center account",
		Description: "Read the participation of a Merchant Center account in one program, with the unmet requirements " +
			"that affect it. After enabling or disabling a program it can take a while until the state shows. " +
			programsUntrusted,
		Tags: []string{"google", "merchant", "programs"}, Risk: readRisk, Provider: Provider,
		InputSchema: inputSchema([]string{"program"}, member("account", accountArgSchema),
			member("program", programArgSchema)),
		OutputSchema: json.RawMessage(programSchema),
		Arguments:    []capability.Argument{accountArgument, programArgument},
		Fields:       []capability.Field{{Name: "program", Description: programFieldsDescription}},
		Examples: []capability.Example{{Description: "One program",
			Arguments: json.RawMessage(`{"account":"1000001","program":"free-listings"}`)}},
	}
	programsEnable = programChange("enable", "Enable a program of a Merchant Center account",
		"Enable participation of a Merchant Center account in a program. ", programsEnableID,
		`{"account":"1000001","program":"free-listings"}`)
	programsDisable = programChange("disable", "Disable a program of a Merchant Center account",
		"Disable participation of a Merchant Center account in a program. ", programsDisableID,
		`{"account":"1000001","program":"free-listings"}`)
)

func programChange(verb, title, lead, id, example string) capability.Descriptor {
	return capability.Descriptor{
		ID: id, Version: 1, Title: title,
		Description: lead + "Qatlas sends a fixed request and only the program and the account come from the " +
			"arguments. " + programsChangeNote + " " + programsRoleNote + " The tool is offered only when the " +
			"connection lists it in tools, and every call needs a confirmation that shows the account and the " +
			"program. " + programsUntrusted,
		Tags: []string{"google", "merchant", "programs"}, Risk: programChangeRisk(), Provider: Provider,
		RequiresToolAllowList: true,
		InputSchema: inputSchema([]string{"program"}, member("account", accountArgSchema),
			member("program", programArgSchema)),
		OutputSchema: json.RawMessage(programChangeSchema),
		Arguments:    []capability.Argument{accountArgument, programArgument},
		Fields: []capability.Field{
			{Name: "accepted", Description: "true when Google accepted the request; nothing is said about processing"},
			{Name: "program", Description: "The program as Google returned it, with " + programFieldsDescription},
		},
		Examples: []capability.Example{{Description: verb + " one program", Arguments: json.RawMessage(example)}},
	}
}

// requirement is a Requirement of the accounts sub-API: what the account still has to meet for a program.
type requirement struct {
	Title              nameText `json:"title,omitempty"`
	AffectedRegionCode idList   `json:"affectedRegionCodes,omitempty"`
	DocumentationURI   urlText  `json:"documentationUri,omitempty"`
}

// requirementList keeps at most maxListItems requirements of a program.
type requirementList []requirement

func (l *requirementList) UnmarshalJSON(b []byte) error {
	var raw []json.RawMessage
	if json.Unmarshal(b, &raw) != nil {
		*l = nil
		return nil
	}
	out := requirementList{}
	for _, item := range raw {
		if len(out) == maxListItems {
			break
		}
		var r requirement
		if json.Unmarshal(item, &r) != nil {
			continue
		}
		out = append(out, r)
	}
	*l = out
	return nil
}

// program is the Program resource. Every member is output only; members the discovery document does not
// list are never held.
type program struct {
	Name              idText          `json:"name,omitempty"`
	State             idText          `json:"state,omitempty"`
	ActiveRegionCodes idList          `json:"activeRegionCodes,omitempty"`
	UnmetRequirements requirementList `json:"unmetRequirements,omitempty"`
	DocumentationURI  urlText         `json:"documentationUri,omitempty"`
}

type programsAnswer struct {
	Programs      []program `json:"programs"`
	NextPageToken string    `json:"nextPageToken"`
}

type programsResult struct {
	Programs   []program `json:"programs"`
	NextCursor string    `json:"next_cursor,omitempty"`
}

type programChangeResult struct {
	Accepted bool    `json:"accepted"`
	Program  program `json:"program"`
}

var (
	requirementSchema = objectSchema(member("title", stringSchema), member("affectedRegionCodes", listSchema),
		member("documentationUri", stringSchema))
	programSchema = objectSchema(member("name", stringSchema), member("state", stringSchema),
		member("activeRegionCodes", listSchema),
		member("unmetRequirements", `{"type":"array","items":`+requirementSchema+`}`),
		member("documentationUri", stringSchema))
	programChangeSchema = objectSchema(member("accepted", boolSchema), member("program", programSchema))
)

func programsPath(account string) string { return accountPath(account) + "/programs" }

func programPath(account, id string) string { return programsPath(account) + "/" + id }

// bindProgram reads the validated arguments, refusing any member they do not name, and settles the
// connection, the account, and the program ID. Nothing is requested and no secret is read.
func bindProgram(op string, resolved *config.Resolved, raw json.RawMessage, needProgram bool) (
	args programArguments, bound scope, account string, needsProof bool, err error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&args) != nil {
		return args, bound, "", false, invalidArgument("the arguments hold a member or a value this tool does not take")
	}
	if bound, err = checkConnection(resolved); err != nil {
		return args, bound, "", false, err
	}
	if account, needsProof, err = bound.accountOf(args.Account); err != nil {
		return args, bound, "", false, err
	}
	if needProgram && !programIDForm.MatchString(args.Program) {
		return args, bound, "", false, invalidArgument("program must be a program ID of lowercase letters, digits, " +
			"and dashes, such as free-listings")
	}
	return args, bound, account, needsProof, nil
}

// openProven opens the client and, for an account that only a sub-account proof binds to the targets, reads
// that proof before the detail or the change follows.
func openProven(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	op, tool string, needsProof bool, account string) (*client, error) {
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if needsProof {
		if err := cl.prove(ctx, op, tool, account); err != nil {
			return nil, err
		}
	}
	return cl, nil
}

func handleProgramsList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage) (any, error) {
	const op = "list programs"
	args, bound, account, needsProof, err := bindProgram(op, resolved, raw, false)
	if err != nil {
		return nil, err
	}
	if args.Program != "" {
		return nil, invalidArgument("program is not an argument of this tool")
	}
	binding := provider.CursorBinding(programsListID, account, args.PageSize, bound.key())
	pageSize := programsPageSize
	if args.PageSize > 0 {
		pageSize = min(args.PageSize, maxProgramsPage)
	}
	query := url.Values{"pageSize": {strconv.Itoa(pageSize)}}
	if args.Cursor != "" {
		token, ok := provider.DecodeCursor(binding, args.Cursor, maxCursorSize)
		if !ok || token == "" {
			return nil, refusedCursor()
		}
		query.Set("pageToken", token)
	}
	cl, err := openProven(ctx, resolved, secrets, red, op, programsListID, needsProof, account)
	if err != nil {
		return nil, err
	}
	var answer programsAnswer
	if err := cl.get(ctx, op, programsListID, programsPath(account), query, &answer); err != nil {
		return nil, err
	}
	if len(answer.Programs) > maxEntries {
		return nil, tooMany(op)
	}
	out := programsResult{Programs: append([]program{}, answer.Programs...)}
	if out.NextCursor, err = nextCursor(op, binding, answer.NextPageToken); err != nil {
		return nil, err
	}
	return out, nil
}

func handleProgramsGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage) (any, error) {
	const op = "get program"
	args, _, account, needsProof, err := bindProgram(op, resolved, raw, true)
	if err != nil {
		return nil, err
	}
	if args.PageSize != 0 || args.Cursor != "" {
		return nil, invalidArgument("page_size and cursor are not arguments of this tool")
	}
	cl, err := openProven(ctx, resolved, secrets, red, op, programsGetID, needsProof, account)
	if err != nil {
		return nil, err
	}
	var out program
	if err := cl.get(ctx, op, programsGetID, programPath(account, args.Program), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// changeProgram sends the fixed empty request of enable or disable once. The body is the documented empty
// request message; nothing of it comes from the arguments.
func changeProgram(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage, op, tool, action string) (any, error) {
	args, _, account, needsProof, err := bindProgram(op, resolved, raw, true)
	if err != nil {
		return nil, err
	}
	if args.PageSize != 0 || args.Cursor != "" {
		return nil, invalidArgument("page_size and cursor are not arguments of this tool")
	}
	cl, err := openProven(ctx, resolved, secrets, red, op, tool, needsProof, account)
	if err != nil {
		return nil, err
	}
	var out program
	err = cl.send(ctx, op, tool, request{method: http.MethodPost, path: programPath(account, args.Program) + ":" + action,
		body: []byte(`{}`), change: true}, &out)
	if err != nil {
		return nil, err
	}
	return programChangeResult{Accepted: true, Program: out}, nil
}

func handleProgramsEnable(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return changeProgram(ctx, resolved, secrets, red, raw, "enable program", programsEnableID, "enable")
}

func handleProgramsDisable(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return changeProgram(ctx, resolved, secrets, red, raw, "disable program", programsDisableID, "disable")
}

// programSummary phrases the confirmation from the validated arguments alone.
func programSummary(verb string) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		var a programArguments
		if json.Unmarshal(raw, &a) != nil {
			return ""
		}
		out := verb + " program " + strconv.Quote(a.Program)
		if a.Account != "" {
			out += " of account " + a.Account
		} else {
			out += " of the account of this connection"
		}
		return out + "; this can change what Google shows publicly in Google Shopping, and Google processes it afterwards"
	}
}
