package googlemerchant

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	renderAccountIssuesID = Provider + ".issue-resolutions.render-account-issues"
	renderProductIssuesID = Provider + ".issue-resolutions.render-product-issues"
)

// renderAPI is the path prefix of the issue resolution sub-API.
const renderAPI = "issueresolution/v1/"

// Bounds of the rendered content. The issue texts come from Google but quote what the business or its products
// hold, so they are cut like every other provider text.
const (
	maxIssues       = 200
	maxRenderedText = 2000
	maxHTMLRunes    = 16384
	maxLanguageLen  = 35
	maxTimeZoneLen  = 64
	maxProductArg   = 512
)

type (
	bodyText string // titles, messages, labels: text of the issue
	htmlText string // pre-rendered HTML, kept as text and never interpreted
)

func (s *bodyText) UnmarshalJSON(b []byte) error {
	*s = bodyText(clipJSON(b, maxRenderedText))
	return nil
}
func (s *htmlText) UnmarshalJSON(b []byte) error {
	*s = htmlText(clipJSON(b, maxHTMLRunes))
	return nil
}

// decodeRenderedList keeps at most limit entries of a JSON array, in order. An entry that does not decode is dropped.

func decodeRenderedList[T any](b []byte, limit int) []T {
	var raw []json.RawMessage
	if json.Unmarshal(b, &raw) != nil {
		return nil
	}
	var out []T
	for _, item := range raw {
		if len(out) == limit {
			break
		}
		var v T
		if json.Unmarshal(item, &v) == nil {
			out = append(out, v)
		}
	}
	return out
}

type (
	issueList       []renderedIssue
	itemList[T any] []T
	textList        []bodyText
)

func (l *issueList) UnmarshalJSON(b []byte) error {
	*l = decodeRenderedList[renderedIssue](b, maxIssues)
	return nil
}
func (l *textList) UnmarshalJSON(b []byte) error {
	*l = decodeRenderedList[bodyText](b, maxListItems)
	return nil
}
func (l *itemList[T]) UnmarshalJSON(b []byte) error {
	*l = decodeRenderedList[T](b, maxListItems)
	return nil
}

// The rendered issue types are the members of the issue resolution discovery document. actionContext of a
// built-in user input action is left out: it only serves triggeraction, which this provider does not offer.
// A reason carries an action of the leaf kind, so the nesting stays bounded.
type (
	textWithTooltip struct {
		SimpleValue        bodyText `json:"simpleValue,omitempty"`
		SimpleTooltipValue bodyText `json:"simpleTooltipValue,omitempty"`
		TooltipIconStyle   idText   `json:"tooltipIconStyle,omitempty"`
	}
	callout struct {
		FullMessage *textWithTooltip `json:"fullMessage,omitempty"`
		StyleHint   idText           `json:"styleHint,omitempty"`
	}
	renderedRegion struct {
		Code idText   `json:"code,omitempty"`
		Name nameText `json:"name,omitempty"`
	}
	breakdown struct {
		Regions itemList[renderedRegion] `json:"regions,omitempty"`
		Details textList                 `json:"details,omitempty"`
	}
	impact struct {
		Severity   idText              `json:"severity,omitempty"`
		Message    bodyText            `json:"message,omitempty"`
		Breakdowns itemList[breakdown] `json:"breakdowns,omitempty"`
	}
	externalAction struct {
		Type idText  `json:"type,omitempty"`
		URI  urlText `json:"uri,omitempty"`
	}
	additionalContent struct {
		Title      bodyText `json:"title,omitempty"`
		Paragraphs textList `json:"paragraphs,omitempty"`
	}
	simpleAction struct {
		Type              idText             `json:"type,omitempty"`
		AttributeCode     idText             `json:"attributeCode,omitempty"`
		AdditionalContent *additionalContent `json:"additionalContent,omitempty"`
	}
	choiceOption struct {
		Label           *textWithTooltip `json:"label,omitempty"`
		ID              idText           `json:"id,omitempty"`
		AdditionalInput *inputField      `json:"additionalInput,omitempty"`
	}
	textInput struct {
		Type           idText           `json:"type,omitempty"`
		FormatInfo     bodyText         `json:"formatInfo,omitempty"`
		AriaLabel      bodyText         `json:"ariaLabel,omitempty"`
		AdditionalInfo *textWithTooltip `json:"additionalInfo,omitempty"`
	}
	// inputField describes one field of a form. Nothing here can be filled in or sent.
	inputField struct {
		ID            idText           `json:"id,omitempty"`
		Label         *textWithTooltip `json:"label,omitempty"`
		Required      bool             `json:"required,omitempty"`
		TextInput     *textInput       `json:"textInput,omitempty"`
		CheckboxInput *struct{}        `json:"checkboxInput,omitempty"`
		ChoiceInput   *choiceInput     `json:"choiceInput,omitempty"`
	}
	choiceInput struct {
		Options itemList[choiceOption] `json:"options,omitempty"`
	}
	actionFlow struct {
		Label             bodyText             `json:"label,omitempty"`
		DialogTitle       bodyText             `json:"dialogTitle,omitempty"`
		DialogMessage     *textWithTooltip     `json:"dialogMessage,omitempty"`
		DialogCallout     *callout             `json:"dialogCallout,omitempty"`
		DialogButtonLabel bodyText             `json:"dialogButtonLabel,omitempty"`
		Inputs            itemList[inputField] `json:"inputs,omitempty"`
	}
	userInputAction struct {
		Flows itemList[actionFlow] `json:"flows,omitempty"`
	}
	actionLeaf struct {
		ButtonLabel         bodyText         `json:"buttonLabel,omitempty"`
		IsAvailable         *bool            `json:"isAvailable,omitempty"`
		ExternalAction      *externalAction  `json:"externalAction,omitempty"`
		BuiltinSimpleAction *simpleAction    `json:"builtinSimpleAction,omitempty"`
		BuiltinUserInput    *userInputAction `json:"builtinUserInputAction,omitempty"`
	}
	reason struct {
		Message bodyText    `json:"message,omitempty"`
		Detail  bodyText    `json:"detail,omitempty"`
		Action  *actionLeaf `json:"action,omitempty"`
	}
	issueAction struct {
		actionLeaf
		Reasons itemList[reason] `json:"reasons,omitempty"`
	}
	renderedIssue struct {
		Title                                  bodyText              `json:"title,omitempty"`
		Impact                                 *impact               `json:"impact,omitempty"`
		PrerenderedContent                     htmlText              `json:"prerenderedContent,omitempty"`
		PrerenderedOutOfCourtDisputeSettlement htmlText              `json:"prerenderedOutOfCourtDisputeSettlement,omitempty"`
		Actions                                itemList[issueAction] `json:"actions,omitempty"`
	}
)

// renderAnswer is both the answer of Google and the result: a list is never null.
type renderAnswer struct {
	RenderedIssues issueList `json:"renderedIssues"`
}

// renderRequest is the whole body of a render call. Both members are enums of the document.
type renderRequest struct {
	ContentOption         string `json:"contentOption,omitempty"`
	UserInputActionOption string `json:"userInputActionOption,omitempty"`
}

const (
	contentOptionPrerenderedHTML = "PRE_RENDERED_HTML"
	inputRedirect                = "REDIRECT_TO_MERCHANT_CENTER"
	inputBuiltIn                 = "BUILT_IN_USER_INPUT_ACTIONS"
)

const (
	renderLanguageSchema = `{"type":"string","minLength":2,"maxLength":35,"pattern":"^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8}){0,3}$",` +
		`"x-form":"a BCP-47 language tag such as en-US or de"}`
	renderTimeZoneSchema = `{"type":"string","minLength":1,"maxLength":64,` +
		`"pattern":"^[A-Za-z][A-Za-z0-9_+-]*(/[A-Za-z0-9_+-]+){0,2}$","x-form":"an IANA time zone such as Europe/Zurich"}`
	contentOptionSchema = `{"type":"string","enum":["PRE_RENDERED_HTML"]}`
	inputOptionSchema   = `{"type":"string","enum":["REDIRECT_TO_MERCHANT_CENTER","BUILT_IN_USER_INPUT_ACTIONS"]}`
	renderProductSchema = `{"type":"string","minLength":4,"maxLength":512,"x-form":"the product segment of accounts/ACCOUNT/products/PRODUCT: ` +
		`content_language~feed_label~offer_id (also with a leading local~), or that string as unpadded base64url"}`
)

var (
	renderLanguageForm = regexp.MustCompile(`^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8}){0,3}$`)
	timeZonePattern    = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_+-]*(/[A-Za-z0-9_+-]+){0,2}$`)
	// A plain product segment: language, feed label, offer ID. The offer ID of the plain form must not hold
	// the characters the document names (/, %, ~); only a conservative set stays, anything else is sent encoded.
	plainProduct   = regexp.MustCompile(`^(local~)?[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8})?~[A-Za-z0-9_-]{1,20}~[A-Za-z0-9._-]{1,400}$`)
	encodedProduct = regexp.MustCompile(`^[A-Za-z0-9_-]{4,512}$`)
)

func renderSchema() string {
	text := func(name string) string { return member(name, stringSchema) }
	tooltip := objectSchema(text("simpleValue"), text("simpleTooltipValue"), text("tooltipIconStyle"))
	list := func(item string) string { return `{"type":"array","items":` + item + `}` }
	input := objectSchema(text("id"), member("label", tooltip), member("required", boolSchema),
		member("textInput", objectSchema(text("type"), text("formatInfo"), text("ariaLabel"), member("additionalInfo", tooltip))),
		member("checkboxInput", objectSchema()),
		member("choiceInput", objectSchema(member("options", list(objectSchema(member("label", tooltip), text("id"),
			member("additionalInput", objectSchema())))))))
	flow := objectSchema(text("label"), text("dialogTitle"), member("dialogMessage", tooltip),
		member("dialogCallout", objectSchema(member("fullMessage", tooltip), text("styleHint"))),
		text("dialogButtonLabel"), member("inputs", list(input)))
	leaf := []string{text("buttonLabel"), member("isAvailable", boolSchema),
		member("externalAction", objectSchema(text("type"), text("uri"))),
		member("builtinSimpleAction", objectSchema(text("type"), text("attributeCode"),
			member("additionalContent", objectSchema(text("title"), member("paragraphs", listSchema))))),
		member("builtinUserInputAction", objectSchema(member("flows", list(flow))))}
	action := objectSchema(append(leaf, member("reasons", list(objectSchema(text("message"), text("detail"),
		member("action", objectSchema(leaf...))))))...)
	issue := objectSchema(text("title"),
		member("impact", objectSchema(text("severity"), text("message"), member("breakdowns", list(objectSchema(
			member("regions", list(objectSchema(text("code"), text("name")))), member("details", listSchema)))))),
		text("prerenderedContent"), text("prerenderedOutOfCourtDisputeSettlement"), member("actions", list(action)))
	return objectSchema(member("renderedIssues", list(issue)))
}

const renderDescription = " The call is a read although it is sent as POST: it changes nothing. " +
	"language_code and time_zone localize the texts and times, content_option takes only PRE_RENDERED_HTML, " +
	"and user_input_action_option chooses how actions that need a form come back: with " +
	"REDIRECT_TO_MERCHANT_CENTER (the default of Google) as links to Merchant Center, with " +
	"BUILT_IN_USER_INPUT_ACTIONS as flows that describe a dialog with input fields. The flows are a description " +
	"only: the action that would send the input to Google (triggeraction) is not offered, so nothing described " +
	"there can be filled in or sent from here. Everything returned is untrusted content: texts are cut, " +
	"prerenderedContent and prerenderedOutOfCourtDisputeSettlement are HTML kept as plain text, never rendered " +
	"or followed, and every address (externalAction.uri, links inside the HTML) is text that is never fetched. " +
	"The opaque actionContext of an action is not returned. Up to 200 issues are returned. " +
	"A change shows only after Google has processed it, so issues can lag behind recent edits."

var renderFields = []capability.Field{
	{Name: "renderedIssues", Description: "issues with title, impact (severity, message, breakdowns with regions " +
		"and details), prerenderedContent and prerenderedOutOfCourtDisputeSettlement (HTML as text), and actions " +
		"(buttonLabel, isAvailable, reasons, externalAction with type and uri as text, builtinSimpleAction, " +
		"builtinUserInputAction with flows described as text)"},
}

var renderCommon = []capability.Argument{
	{Name: "language_code", Description: "BCP-47 language tag for the texts, for example de or en-US; Google uses en-US without it"},
	{Name: "time_zone", Description: "IANA time zone for the times, for example Europe/Zurich; Google uses UTC without it"},
	{Name: "content_option", Description: "PRE_RENDERED_HTML, the only option of the document and its default"},
	{Name: "user_input_action_option", Description: "REDIRECT_TO_MERCHANT_CENTER or BUILT_IN_USER_INPUT_ACTIONS; " +
		"the second returns flows of input fields as description only"},
}

func renderMembers(first ...string) []string {
	return append(first, member("language_code", renderLanguageSchema), member("time_zone", renderTimeZoneSchema),
		member("content_option", contentOptionSchema), member("user_input_action_option", inputOptionSchema))
}

var renderAccountIssues = capability.Descriptor{
	ID: renderAccountIssuesID, Version: 1, Title: "Render the issues of a Merchant Center account",
	Description: "Read the issues of a Merchant Center account with Google's resolution content and the actions " +
		"that would solve them, meant to be shown to the business." + renderDescription,
	Tags: []string{"google", "merchant", "issue-resolutions"}, Risk: readRisk, Provider: Provider,
	InputSchema:  inputSchema(nil, renderMembers(member("account", accountArgSchema))...),
	OutputSchema: json.RawMessage(renderSchema()),
	Arguments:    append([]capability.Argument{accountArgument}, renderCommon...),
	Fields:       renderFields,
	Examples: []capability.Example{{Description: "Issues of one account in German",
		Arguments: json.RawMessage(`{"account":"1000001","language_code":"de","time_zone":"Europe/Zurich"}`)}},
}

var renderProductIssues = capability.Descriptor{
	ID: renderProductIssuesID, Version: 1, Title: "Render the issues of a Merchant Center product",
	Description: "Read the issues of one product with Google's resolution content and the actions that would solve " +
		"them, meant to be shown to the business. product is the product segment of its name: " +
		"content_language~feed_label~offer_id, for legacy local products with a leading local~, or that string as " +
		"unpadded base64url. Offer IDs of the plain form may only use letters, digits, dot, underscore and hyphen; " +
		"send any other offer ID in the base64url form." + renderDescription,
	Tags: []string{"google", "merchant", "issue-resolutions", "products"}, Risk: readRisk, Provider: Provider,
	InputSchema:  inputSchema([]string{"product"}, renderMembers(member("account", accountArgSchema), member("product", renderProductSchema))...),
	OutputSchema: json.RawMessage(renderSchema()),
	Arguments: append([]capability.Argument{accountArgument,
		{Name: "product", Description: "product segment: content_language~feed_label~offer_id (for example en~US~sku123), " +
			"with a leading local~ for legacy local products, or that string as unpadded base64url", Required: true}},
		renderCommon...),
	Fields: renderFields,
	Examples: []capability.Example{{Description: "Issues of one product",
		Arguments: json.RawMessage(`{"account":"1000001","product":"en~US~sku123"}`)}},
}

type renderArguments struct {
	Product               string `json:"product"`
	LanguageCode          string `json:"language_code"`
	TimeZone              string `json:"time_zone"`
	ContentOption         string `json:"content_option"`
	UserInputActionOption string `json:"user_input_action_option"`
}

// checkRenderProduct accepts the two forms of the reference and returns the segment. The encoded form must decode
// to the documented structure, and nothing outside the character sets reaches a path.
func checkRenderProduct(raw string) (string, error) {
	switch {
	case raw == "":
		return "", invalidArgument("product is required")
	case len(raw) > maxProductArg:
		return "", invalidArgument("product is longer than " + "512 characters")
	case strings.Contains(raw, "~"):
		if !plainProduct.MatchString(raw) {
			return "", invalidArgument("product must be content_language~feed_label~offer_id; an offer ID with " +
				"other characters than letters, digits, dot, underscore and hyphen must be sent as unpadded base64url")
		}
		return raw, nil
	case !encodedProduct.MatchString(raw):
		return "", invalidArgument("product must be content_language~feed_label~offer_id or its unpadded base64url form")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || !utf8.Valid(decoded) {
		return "", invalidArgument("product is not valid unpadded base64url")
	}
	text := string(decoded)
	for _, r := range text {
		if unicode.IsControl(r) {
			return "", invalidArgument("the decoded product must not contain control characters")
		}
	}
	text = strings.TrimPrefix(text, "local~")
	if parts := strings.SplitN(text, "~", 3); len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return "", invalidArgument("the decoded product must be content_language~feed_label~offer_id")
	}
	return raw, nil
}

// renderBody checks the options of a render call and builds the query and the body.
func renderBody(args renderArguments) (url.Values, renderRequest, error) {
	query := url.Values{}
	if args.LanguageCode != "" {
		if len(args.LanguageCode) > maxLanguageLen || !renderLanguageForm.MatchString(args.LanguageCode) {
			return nil, renderRequest{}, invalidArgument("language_code must be a BCP-47 language tag such as en-US")
		}
		query.Set("languageCode", args.LanguageCode)
	}
	if args.TimeZone != "" {
		if len(args.TimeZone) > maxTimeZoneLen || !timeZonePattern.MatchString(args.TimeZone) {
			return nil, renderRequest{}, invalidArgument("time_zone must be an IANA time zone such as Europe/Zurich")
		}
		query.Set("timeZone", args.TimeZone)
	}
	var body renderRequest
	switch args.ContentOption {
	case "":
	case contentOptionPrerenderedHTML:
		body.ContentOption = args.ContentOption
	default:
		return nil, renderRequest{}, invalidArgument("content_option must be PRE_RENDERED_HTML")
	}
	switch args.UserInputActionOption {
	case "":
	case inputRedirect, inputBuiltIn:
		body.UserInputActionOption = args.UserInputActionOption
	default:
		return nil, renderRequest{}, invalidArgument("user_input_action_option must be REDIRECT_TO_MERCHANT_CENTER " +
			"or BUILT_IN_USER_INPUT_ACTIONS")
	}
	return query, body, nil
}

// settleRendering settles the account against the binding, which needs no secret and no request, and then
// opens the client and, for an account no target names, reads the proof. Every refusal comes before the
// first secret.
func settleRendering(ctx context.Context, c call, resolved *config.Resolved, secrets *secret.Resolver,
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

// postRender sends the POST of a render call as a read (not a change): nothing is changed at Google, and the answer
// is data only; no address in it is followed.
func postRender(ctx context.Context, cl *client, c call, path string, query url.Values, body renderRequest) (any, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, provider.Fail(c.op, "the request body could not be encoded")
	}
	var out renderAnswer
	if err := cl.send(ctx, c.op, c.tool, request{method: http.MethodPost, path: path, query: query, body: payload}, &out); err != nil {
		return nil, err
	}
	if out.RenderedIssues == nil {
		out.RenderedIssues = issueList{}
	}
	return out, nil
}

func handleRenderAccountIssues(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("render account issues", renderAccountIssuesID, resolved, raw)
	if err != nil {
		return nil, err
	}
	var args renderArguments
	if json.Unmarshal(raw, &args) != nil {
		return nil, invalidArgument("the arguments could not be read")
	}
	query, body, err := renderBody(args)
	if err != nil {
		return nil, err
	}
	cl, id, err := settleRendering(ctx, c, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	return postRender(ctx, cl, c, renderAPI+"accounts/"+id+":renderaccountissues", query, body)
}

func handleRenderProductIssues(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("render product issues", renderProductIssuesID, resolved, raw)
	if err != nil {
		return nil, err
	}
	var args renderArguments
	if json.Unmarshal(raw, &args) != nil {
		return nil, invalidArgument("the arguments could not be read")
	}
	segment, err := checkRenderProduct(args.Product)
	if err != nil {
		return nil, err
	}
	query, body, err := renderBody(args)
	if err != nil {
		return nil, err
	}
	cl, id, err := settleRendering(ctx, c, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	return postRender(ctx, cl, c,
		renderAPI+"accounts/"+id+"/products/"+url.PathEscape(segment)+":renderproductissues", query, body)
}
