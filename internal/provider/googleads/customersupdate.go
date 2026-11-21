package googleads

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
	"unicode"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const customersUpdateID = Provider + ".customers.update"

// Qatlas's own bounds of the text arguments; Google documents none for these fields.
const (
	maxNameArgument = 1024
	maxURLArgument  = 2048
)

var numberPattern = regexp.MustCompile(`^[0-9]{1,20}$`)

const conversionActionPattern = `^customers/[0-9]{1,10}/conversionActions/[0-9]{1,20}$`

// checkAddress refuses a tracking template that is not an https address without user info. Value Track
// parameters in braces are fine in the path and the query, not in the authority. A template that starts with a
// parameter such as {lpurl} names no address and is refused as well: the check has nothing to examine.
func checkAddress(argument, value string) error {
	if value == "" {
		return nil
	}
	if len(value) > maxURLArgument {
		return badRequest(argument + " must not be longer than 2048 bytes")
	}
	for _, r := range value {
		if unicode.IsControl(r) || unicode.IsSpace(r) || r == '\\' {
			return badRequest(argument + " must not hold spaces, backslashes or control characters")
		}
	}
	if !strings.HasPrefix(strings.ToLower(value), "https://") {
		return badRequest(argument + " must be an https address, such as https://track.example.com/?u={lpurl}")
	}
	authority := value[len("https://"):]
	if i := strings.IndexAny(authority, "/?#"); i >= 0 {
		authority = authority[:i]
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Hostname() == "" ||
		strings.ContainsAny(authority, "@{}") {
		return badRequest(argument + " must be an https address without user info and with a plain host name")
	}
	return nil
}

// checkSuffix refuses a final URL suffix that is not a plain list of parameters: the suffix is appended to
// final URLs, so an address in it, which would send clicks elsewhere, is refused.
func checkSuffix(argument, value string) error {
	if len(value) > maxURLArgument {
		return badRequest(argument + " must not be longer than 2048 bytes")
	}
	for _, r := range value {
		if unicode.IsControl(r) || unicode.IsSpace(r) || r == '\\' {
			return badRequest(argument + " must not hold spaces, backslashes or control characters")
		}
	}
	if strings.Contains(value, "//") || strings.HasPrefix(value, "/") {
		return badRequest(argument + " must be parameters such as utm_source=x&utm_medium=y, not an address")
	}
	return nil
}

type customerUpdateArguments struct {
	CustomerID                     string  `json:"customer_id"`
	DescriptiveName                *string `json:"descriptive_name"`
	TrackingURLTemplate            *string `json:"tracking_url_template"`
	FinalURLSuffix                 *string `json:"final_url_suffix"`
	AutoTaggingEnabled             *bool   `json:"auto_tagging_enabled"`
	CallReportingEnabled           *bool   `json:"call_reporting_enabled"`
	CallConversionReportingEnabled *bool   `json:"call_conversion_reporting_enabled"`
	CallConversionAction           *string `json:"call_conversion_action"`
	ValidateOnly                   bool    `json:"validate_only"`
	ResponseContentType            string  `json:"response_content_type"`
}

// operation checks the arguments and builds the one operation: the update with only the members that were
// given, the mask that names them, and their names for the answer. An empty tracking template, suffix or call
// conversion action clears the field.
func (a customerUpdateArguments) operation() (map[string]any, []string, error) {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return nil, nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := checkContentType(a.ResponseContentType); err != nil {
		return nil, nil, err
	}
	update := map[string]any{"resourceName": customerName(a.CustomerID)}
	var mask, names []string
	set := func(path, name string, value any) {
		mask, names = append(mask, path), append(names, name)
		if value != nil {
			setPath(update, path, value)
		}
	}
	if v := a.DescriptiveName; v != nil {
		if strings.TrimSpace(*v) == "" || len(*v) > maxNameArgument || strings.ContainsFunc(*v, unicode.IsControl) {
			return nil, nil, badRequest("descriptive_name must be text of 1 to 1024 bytes without control characters")
		}
		set("descriptiveName", "descriptive_name", *v)
	}
	if v := a.TrackingURLTemplate; v != nil {
		if err := checkAddress("tracking_url_template", *v); err != nil {
			return nil, nil, err
		}
		set("trackingUrlTemplate", "tracking_url_template", nonEmpty(*v))
	}
	if v := a.FinalURLSuffix; v != nil {
		if err := checkSuffix("final_url_suffix", *v); err != nil {
			return nil, nil, err
		}
		set("finalUrlSuffix", "final_url_suffix", nonEmpty(*v))
	}
	if v := a.AutoTaggingEnabled; v != nil {
		set("autoTaggingEnabled", "auto_tagging_enabled", *v)
	}
	if v := a.CallReportingEnabled; v != nil {
		set("callReportingSetting.callReportingEnabled", "call_reporting_enabled", *v)
	}
	if v := a.CallConversionReportingEnabled; v != nil {
		set("callReportingSetting.callConversionReportingEnabled", "call_conversion_reporting_enabled", *v)
	}
	if v := a.CallConversionAction; v != nil {
		if *v != "" && !ownedName(*v, a.CustomerID, "conversionActions") {
			return nil, nil, badRequest("call_conversion_action must be a conversion action of customer_id, such as " +
				"customers/" + queryNumber(a.CustomerID) + "/conversionActions/123, or empty to clear it")
		}
		set("callReportingSetting.callConversionAction", "call_conversion_action", nonEmpty(*v))
	}
	if len(mask) == 0 {
		return nil, nil, badRequest("give at least one field to change")
	}
	return map[string]any{"update": update, "updateMask": strings.Join(mask, ",")}, names, nil
}

// nonEmpty is the value to send for a text, or nil to leave it out of the body and clear it by the mask.
func nonEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}

// setPath sets a member of the update, creating the object of a dotted path.
func setPath(target map[string]any, path string, value any) {
	head, tail, nested := strings.Cut(path, ".")
	if !nested {
		target[head] = value
		return
	}
	inner, ok := target[head].(map[string]any)
	if !ok {
		inner = map[string]any{}
		target[head] = inner
	}
	setPath(inner, tail, value)
}

// ownedName reports whether a resource name is customers/ID/<collection>/N of exactly the checked account.
func ownedName(name, customerID, collection string) bool {
	rest, ok := strings.CutPrefix(name, customerName(customerID)+"/"+collection+"/")
	return ok && numberPattern.MatchString(rest)
}

// The members of the customer that Google returns after a change with MUTABLE_RESOURCE: only the mutable ones.
type (
	customerMutableWire struct {
		customerWire
		Flags struct {
			AutoTaggingEnabled   *bool `json:"autoTaggingEnabled"`
			CallReportingSetting *struct {
				CallReportingEnabled           *bool  `json:"callReportingEnabled"`
				CallConversionReportingEnabled *bool  `json:"callConversionReportingEnabled"`
				CallConversionAction           string `json:"callConversionAction"`
			} `json:"callReportingSetting"`
		}
	}
	customerMutable struct {
		ResourceName                string               `json:"resource_name"`
		DescriptiveName             string               `json:"descriptive_name,omitempty"`
		TrackingURLTemplate         string               `json:"tracking_url_template,omitempty"`
		FinalURLSuffix              string               `json:"final_url_suffix,omitempty"`
		AutoTaggingEnabled          *bool                `json:"auto_tagging_enabled,omitempty"`
		CallReportingSetting        *callReportingChange `json:"call_reporting_setting,omitempty"`
		GoogleAdsConversionCustomer string               `json:"google_ads_conversion_customer,omitempty"`
		VideoBrandSafetySuitability string               `json:"video_brand_safety_suitability,omitempty"`
		LocalServicesSettings       *localServices       `json:"local_services_settings,omitempty"`
		VideoCustomer               *videoCustomer       `json:"video_customer,omitempty"`
	}
	callReportingChange struct {
		CallReportingEnabled           *bool  `json:"call_reporting_enabled,omitempty"`
		CallConversionReportingEnabled *bool  `json:"call_conversion_reporting_enabled,omitempty"`
		CallConversionAction           string `json:"call_conversion_action,omitempty"`
	}
)

// customerMutableOf maps the customer of a mutate answer. It must be the account of the call, and a
// conversion action in it must belong to that account.
func customerMutableOf(raw json.RawMessage, customerID string) (*customerMutable, bool) {
	var w customerMutableWire
	if json.Unmarshal(raw, &w.customerWire) != nil || json.Unmarshal(raw, &w.Flags) != nil ||
		w.ResourceName != customerName(customerID) {
		return nil, false
	}
	out := &customerMutable{ResourceName: w.ResourceName, DescriptiveName: boundedText(w.DescriptiveName, maxText),
		TrackingURLTemplate:         boundedText(w.TrackingURLTemplate, maxLongText),
		FinalURLSuffix:              boundedText(w.FinalURLSuffix, maxLongText),
		AutoTaggingEnabled:          w.Flags.AutoTaggingEnabled,
		GoogleAdsConversionCustomer: boundedText(w.ConversionTrackingSetting.GoogleAdsConversionCustomer, maxText),
		VideoBrandSafetySuitability: code(w.VideoBrandSafetySuitability)}
	if cr := w.Flags.CallReportingSetting; cr != nil {
		if cr.CallConversionAction != "" && !ownedName(cr.CallConversionAction, customerID, "conversionActions") {
			return nil, false
		}
		out.CallReportingSetting = &callReportingChange{CallReportingEnabled: cr.CallReportingEnabled,
			CallConversionReportingEnabled: cr.CallConversionReportingEnabled,
			CallConversionAction:           cr.CallConversionAction}
	}
	if ext := w.LocalServicesSettings.RedemptionProviderInfo.ExternalMerchantID; ext.AlphanumericID != "" || ext.NumericID != nil {
		out.LocalServicesSettings = &localServices{ExternalMerchantNumericID: intText(ext.NumericID),
			ExternalMerchantAlphaID: boundedText(ext.AlphanumericID, maxText)}
	}
	if vp := w.VideoCustomer.Partners; len(vp.BrandLift)+len(vp.BrandSafety)+len(vp.ConversionAttribution)+
		len(vp.Reach)+len(vp.Viewability) > 0 {
		out.VideoCustomer = &videoCustomer{
			BrandLift:             partnersOf(vp.BrandLift, func(p partnerWire) string { return p.BrandLift }),
			BrandSafety:           partnersOf(vp.BrandSafety, func(p partnerWire) string { return p.BrandSafety }),
			ConversionAttribution: partnersOf(vp.ConversionAttribution, func(p partnerWire) string { return p.ConversionAttribution }),
			Reach:                 partnersOf(vp.Reach, func(p partnerWire) string { return p.Reach }),
			Viewability:           partnersOf(vp.Viewability, func(p partnerWire) string { return p.Viewability })}
	}
	return out, true
}

type customerChanged struct {
	CustomerID   string           `json:"customer_id"`
	ValidateOnly bool             `json:"validate_only"`
	Updated      []string         `json:"updated"`
	ResourceName string           `json:"resource_name,omitempty"`
	Customer     *customerMutable `json:"customer,omitempty"`
}

var customerMutableSchema = object(schema{
	"resource_name": stringSchema, "descriptive_name": stringSchema, "tracking_url_template": stringSchema,
	"final_url_suffix": stringSchema, "auto_tagging_enabled": booleanSchema,
	"call_reporting_setting": object(schema{"call_reporting_enabled": booleanSchema,
		"call_conversion_reporting_enabled": booleanSchema, "call_conversion_action": stringSchema}),
	"google_ads_conversion_customer": stringSchema, "video_brand_safety_suitability": stringSchema,
	"local_services_settings": object(schema{"external_merchant_numeric_id": stringSchema,
		"external_merchant_alphanumeric_id": stringSchema}),
	"video_customer": object(schema{"brand_lift_integration_partners": arrayOf(partnerSchema),
		"brand_safety_integration_partners":           arrayOf(partnerSchema),
		"conversion_attribution_integration_partners": arrayOf(partnerSchema),
		"reach_integration_partners":                  arrayOf(partnerSchema),
		"viewability_integration_partners":            arrayOf(partnerSchema)}),
}, "resource_name")

const updateAddressNote = " tracking_url_template sends the clicks on the ads of the whole account through the " +
	"address it names, and final_url_suffix is appended to every final URL: both can pass click data to a third " +
	"party. The template must be an https address without user info (a template that starts with a parameter " +
	"such as {lpurl} is refused), the suffix must be parameters, not an address; an empty value clears the field."

var customersUpdate = capability.Descriptor{
	ID: customersUpdateID, Version: 1, Title: "Change the settings of a Google Ads account",
	Description: "Change the name, the tracking template, the final URL suffix, auto-tagging and the call reporting " +
		"of a Google Ads account (CustomerService.MutateCustomer with an update and its field mask); only the " +
		"fields that are given change." + updateAddressNote + " Auto-tagging adds a click ID to the final URLs of " +
		"the account, and call reporting routes phone calls through Google forwarding numbers. The account the " +
		"conversions are tracked in (google_ads_conversion_customer) is not offered and stays as it is. On a " +
		"connection with targets, a manager account needs a manager target that names it." + mutateNote,
	Tags: []string{"google", "ads", "customers", "update"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent,
		"settings of a Google Ads account: name, tracking template, URL suffix, auto-tagging, call reporting"),
	InputSchema: encoded(object(schema{
		"customer_id":           customerIDArgument,
		"descriptive_name":      schema{"type": "string", "minLength": 1, "maxLength": maxNameArgument},
		"tracking_url_template": schema{"type": "string", "maxLength": maxURLArgument},
		"final_url_suffix":      schema{"type": "string", "maxLength": maxURLArgument},
		"auto_tagging_enabled":  booleanSchema, "call_reporting_enabled": booleanSchema,
		"call_conversion_reporting_enabled": booleanSchema,
		"call_conversion_action":            schema{"type": "string", "maxLength": 100, "pattern": "^$|" + conversionActionPattern},
		"validate_only":                     validateOnlySchema, "response_content_type": responseContentTypeSchema,
	}, "customer_id")),
	OutputSchema: encoded(object(schema{"customer_id": stringSchema, "validate_only": booleanSchema,
		"updated": arrayOf(stringSchema), "resource_name": stringSchema, "customer": customerMutableSchema},
		"customer_id", "validate_only", "updated")),
	Arguments: []capability.Argument{
		{Name: "customer_id", Required: true, Description: "Ten-digit customer ID of the account without hyphens; a " +
			"target of the connection or an account below a manager target"},
		{Name: "descriptive_name", Description: "New name of the account, 1 to 1024 bytes"},
		{Name: "tracking_url_template", Description: "Tracking template of the account: an https address without " +
			"user info, up to 2048 bytes, with Value Track parameters such as {lpurl} after the host; empty clears it"},
		{Name: "final_url_suffix", Description: "Parameters appended to final URLs, such as utm_source=x, up to 2048 " +
			"bytes, no address; empty clears it"},
		{Name: "auto_tagging_enabled", Description: "true or false: tag final URLs with a click ID"},
		{Name: "call_reporting_enabled", Description: "true or false: report phone call events through Google forwarding numbers"},
		{Name: "call_conversion_reporting_enabled", Description: "true or false: report call conversions"},
		{Name: "call_conversion_action", Description: "Conversion action of this account, customers/ID/conversionActions/N, " +
			"that call conversions count for; empty clears it, which makes Google use its default"},
		validateOnlyArgument, responseContentTypeArgument},
	Fields: []capability.Field{
		{Name: "customer_id", Description: "The account of the call"},
		{Name: "validate_only", Description: "true when Google only validated the change"},
		{Name: "updated", Description: "The fields the change named, by argument name"},
		{Name: "resource_name", Description: "customers/ID, as Google answered; absent after validate_only"},
		{Name: "customer", Description: "With MUTABLE_RESOURCE: the mutable fields of the account after the change, " +
			"as Google returns them, google_ads_conversion_customer included"}},
	Examples: []capability.Example{
		{Description: "Validate a new final URL suffix without changing anything",
			Arguments: json.RawMessage(`{"customer_id":"1234567890","final_url_suffix":"utm_source=google","validate_only":true}`)},
		{Description: "Turn auto-tagging on", Arguments: json.RawMessage(`{"customer_id":"1234567890","auto_tagging_enabled":true}`)}},
}

func handleCustomersUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "update customer"
	var args customerUpdateArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	conn, err := connectionOf(resolved)
	if err != nil {
		return nil, err
	}
	operation, names, err := args.operation()
	if err != nil {
		return nil, err
	}
	env := callEnv{ctx: ctx, resolved: resolved, secrets: secrets, red: red, conn: conn, op: op, tool: customersUpdateID}
	c, err := env.settle(args.CustomerID, true)
	if err != nil {
		return nil, err
	}
	var answer struct {
		Result *struct {
			ResourceName string          `json:"resourceName"`
			Customer     json.RawMessage `json:"customer"`
		} `json:"result"`
	}
	if err := c.mutate(ctx, mutation{op: op, tool: customersUpdateID, customerID: args.CustomerID,
		body: mutateBody(operation, args.ValidateOnly, args.ResponseContentType)}, &answer); err != nil {
		return nil, err
	}
	out := customerChanged{CustomerID: args.CustomerID, ValidateOnly: args.ValidateOnly, Updated: names}
	want := customerName(args.CustomerID)
	if answer.Result != nil {
		out.ResourceName = answer.Result.ResourceName
	}
	if args.ValidateOnly && out.ResourceName == "" {
		return out, nil
	}
	if out.ResourceName != want {
		return nil, unsure(provider.InvalidResponse(op, subject+" answered with a result for another account than the "+
			"one asked for"), true)
	}
	if len(answer.Result.Customer) > 0 && string(answer.Result.Customer) != "null" {
		customer, ok := customerMutableOf(answer.Result.Customer, args.CustomerID)
		if !ok {
			return nil, unsure(provider.InvalidResponse(op, subject+" answered with an account that is not the one "+
				"asked for"), true)
		}
		out.Customer = customer
	}
	return out, nil
}

// customerUpdateSummary phrases the change for the confirmation, from the arguments alone.
func customerUpdateSummary(raw json.RawMessage) string {
	var a customerUpdateArguments
	_ = json.Unmarshal(raw, &a)
	var parts []string
	text := func(name string, v *string) {
		if v == nil {
			return
		}
		if *v == "" {
			parts = append(parts, "clear "+name)
		} else {
			parts = append(parts, "set "+name+" to "+*v)
		}
	}
	flag := func(name string, v *bool) {
		if v != nil {
			parts = append(parts, "set "+name+" "+flagWord(*v))
		}
	}
	text("the name", a.DescriptiveName)
	text("the tracking template", a.TrackingURLTemplate)
	text("the final URL suffix", a.FinalURLSuffix)
	flag("auto-tagging", a.AutoTaggingEnabled)
	flag("call reporting", a.CallReportingEnabled)
	flag("call conversion reporting", a.CallConversionReportingEnabled)
	text("the call conversion action", a.CallConversionAction)
	prefix := "Change"
	if a.ValidateOnly {
		prefix = "Only validate the change of"
	}
	summary := prefix + " the settings of Google Ads account " + a.CustomerID + ": " + strings.Join(parts, ", ") + "."
	if a.TrackingURLTemplate != nil || a.FinalURLSuffix != nil {
		summary += " Click data of every ad of the account passes through these values."
	}
	return summary
}

func flagWord(on bool) string {
	if on {
		return "on"
	}
	return "off"
}
