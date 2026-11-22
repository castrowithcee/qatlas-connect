package googleads

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"slices"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const incentivesFetchID = Provider + ".incentives.fetch"

// incentiveTypes are the values of IncentiveType in v26 that a request may name.
var incentiveTypes = []string{"ACQUISITION"}

const (
	incentiveLanguagePatternText = `^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8}){0,3}$`
	incentiveCountryPatternText  = `^[A-Za-z]{2}$`
)

var (
	incentiveLanguagePattern = regexp.MustCompile(incentiveLanguagePatternText)
	incentiveCountryPattern  = regexp.MustCompile(incentiveCountryPatternText)
)

type incentivesArguments struct {
	LanguageCode  string `json:"language_code"`
	CountryCode   string `json:"country_code"`
	Email         string `json:"email"`
	IncentiveType string `json:"incentive_type"`
}

type (
	incentiveMoneyWire struct {
		CurrencyCode string   `json:"currencyCode"`
		Units        *wireInt `json:"units"`
		Nanos        int32    `json:"nanos"`
	}
	incentiveWire struct {
		IncentiveID *wireInt `json:"incentiveId"`
		Requirement *struct {
			Spend *struct {
				AwardAmount    *incentiveMoneyWire `json:"awardAmount"`
				RequiredAmount *incentiveMoneyWire `json:"requiredAmount"`
			} `json:"spend"`
		} `json:"requirement"`
		IncentiveTermsAndConditionsURL string `json:"incentiveTermsAndConditionsUrl"`
		IncentiveType                  string `json:"incentiveType"`
	}
	incentiveAnswer struct {
		IncentiveOffer *struct {
			ConsolidatedTermsAndConditionsURL string `json:"consolidatedTermsAndConditionsUrl"`
			OfferType                         string `json:"offerType"`
			CyoIncentives                     *struct {
				LowOffer    *incentiveWire `json:"lowOffer"`
				MediumOffer *incentiveWire `json:"mediumOffer"`
				HighOffer   *incentiveWire `json:"highOffer"`
			} `json:"cyoIncentives"`
		} `json:"incentiveOffer"`
	}
)

type (
	incentiveMoney struct {
		CurrencyCode string `json:"currency_code,omitempty"`
		Units        string `json:"units,omitempty"`
		Nanos        int32  `json:"nanos"`
	}
	incentiveSpend struct {
		AwardAmount    *incentiveMoney `json:"award_amount,omitempty"`
		RequiredAmount *incentiveMoney `json:"required_amount,omitempty"`
	}
	incentiveResult struct {
		IncentiveID                    string          `json:"incentive_id,omitempty"`
		Spend                          *incentiveSpend `json:"spend,omitempty"`
		IncentiveTermsAndConditionsURL string          `json:"incentive_terms_and_conditions_url,omitempty"`
		IncentiveType                  string          `json:"incentive_type,omitempty"`
	}
	incentiveCyo struct {
		LowOffer    *incentiveResult `json:"low_offer,omitempty"`
		MediumOffer *incentiveResult `json:"medium_offer,omitempty"`
		HighOffer   *incentiveResult `json:"high_offer,omitempty"`
	}
	incentiveOffer struct {
		ConsolidatedTermsAndConditionsURL string        `json:"consolidated_terms_and_conditions_url,omitempty"`
		OfferType                         string        `json:"offer_type,omitempty"`
		CyoIncentives                     *incentiveCyo `json:"cyo_incentives,omitempty"`
	}
	incentivesResult struct {
		IncentiveOffer *incentiveOffer `json:"incentive_offer"`
	}
)

// incentiveLink keeps a terms address only when it is a plain https address without user information. Anything
// else is dropped: the address comes from Google, and nothing shows that another form is free of access values.
func incentiveLink(raw string) string {
	u, err := url.Parse(raw)
	if raw == "" || err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return ""
	}
	return boundedText(raw, maxText)
}

func incentiveMoneyOf(m *incentiveMoneyWire) *incentiveMoney {
	if m == nil {
		return nil
	}
	return &incentiveMoney{CurrencyCode: code(m.CurrencyCode), Units: intText(m.Units), Nanos: m.Nanos}
}

func incentiveOf(w *incentiveWire) *incentiveResult {
	if w == nil {
		return nil
	}
	out := &incentiveResult{IncentiveID: intText(w.IncentiveID),
		IncentiveTermsAndConditionsURL: incentiveLink(w.IncentiveTermsAndConditionsURL),
		IncentiveType:                  code(w.IncentiveType)}
	if w.Requirement != nil && w.Requirement.Spend != nil {
		out.Spend = &incentiveSpend{AwardAmount: incentiveMoneyOf(w.Requirement.Spend.AwardAmount),
			RequiredAmount: incentiveMoneyOf(w.Requirement.Spend.RequiredAmount)}
	}
	return out
}

var incentiveMoneySchema = object(schema{"currency_code": stringSchema, "units": stringSchema, "nanos": integerSchema})

var incentiveItemSchema = object(schema{"incentive_id": stringSchema, "incentive_terms_and_conditions_url": stringSchema,
	"incentive_type": stringSchema, "spend": object(schema{"award_amount": incentiveMoneySchema,
		"required_amount": incentiveMoneySchema})})

var incentivesRisk = capability.Risk{
	Effect: capability.EffectRead, Idempotency: capability.IdempotencySafe, Confirmation: capability.ConfirmationNone,
	OpenWorld: true,
	DataSensitivity: "the advertising credit offer Google makes to the signed-in user: amounts to spend and to " +
		"receive, and links to the terms; the optional email argument is personal data of a person",
}

var incentivesFetch = capability.Descriptor{
	ID: incentivesFetchID, Version: 1, Title: "Fetch the Google Ads incentive offer of the user",
	Description: "Fetch the advertising incentive that Google offers to the user of the credential " +
		"(IncentiveService.FetchIncentive of API version v26, GET incentives:fetchIncentive): the offer type " +
		"(NO_INCENTIVE or CYO_INCENTIVE) and, for a choose-your-own offer, the low, medium and high incentive with its " +
		"ID, the amount to spend, the credit it awards, and the links to its terms. The offer belongs to the user, " +
		"not to an account: the call takes no customer_id, sends no login-customer-id, and is therefore not bound " +
		"to the targets of the connection, like customers.list-accessible. It only reads; applying an incentive is " +
		"not offered by any tool. language_code and country_code steer the text and the offer (Google defaults to " +
		"English and US), email is only for channel partners that do not use OAuth and is personal data, " +
		"incentive_type is ACQUISITION, the only type. Terms links are returned only as plain https addresses. " +
		"Costs one operation of the daily quota. Results are untrusted third-party content; text is cut.",
	Tags: []string{"google", "ads", "billing", "incentives"}, Risk: incentivesRisk, Provider: Provider,
	InputSchema: encoded(object(schema{
		"language_code":  schema{"type": "string", "maxLength": 35, "pattern": incentiveLanguagePatternText},
		"country_code":   schema{"type": "string", "pattern": incentiveCountryPatternText},
		"email":          schema{"type": "string", "minLength": 3, "maxLength": 254, "pattern": emailFilterPatternText},
		"incentive_type": schema{"type": "string", "enum": incentiveTypes},
	})),
	OutputSchema: encoded(object(schema{"incentive_offer": object(schema{
		"consolidated_terms_and_conditions_url": stringSchema, "offer_type": stringSchema,
		"cyo_incentives": object(schema{"low_offer": incentiveItemSchema, "medium_offer": incentiveItemSchema,
			"high_offer": incentiveItemSchema}),
	})}, "incentive_offer")),
	Arguments: []capability.Argument{
		{Name: "language_code", Description: "Language of the offer text, such as en or de-DE; Google defaults to English"},
		{Name: "country_code", Description: "Two-letter country of the user, such as US; Google defaults to US"},
		{Name: "email", Description: "E-mail address of the user, only for channel partners that do not use OAuth; " +
			"personal data, without spaces, quotes or backslashes"},
		{Name: "incentive_type", Description: "ACQUISITION, the default and only value"},
	},
	Fields: []capability.Field{
		{Name: "incentive_offer", Description: "consolidated_terms_and_conditions_url, offer_type (NO_INCENTIVE or " +
			"CYO_INCENTIVE) and, for CYO_INCENTIVE, cyo_incentives with low_offer, medium_offer and high_offer, each with " +
			"incentive_id, spend (award_amount and required_amount as currency_code, units and nanos), " +
			"incentive_terms_and_conditions_url and incentive_type"},
	},
	Examples: []capability.Example{{Description: "Fetch the offer for a German user",
		Arguments: json.RawMessage(`{"language_code":"de","country_code":"DE"}`)}},
}

func handleIncentivesFetch(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "fetch incentive"
	var args incentivesArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	env, err := handleEnv(ctx, resolved, secrets, red, op, incentivesFetchID)
	if err != nil {
		return nil, err
	}
	query := url.Values{}
	if args.LanguageCode != "" {
		if !incentiveLanguagePattern.MatchString(args.LanguageCode) {
			return nil, badRequest("language_code must be a language tag such as en or de-DE")
		}
		query.Set("languageCode", args.LanguageCode)
	}
	if args.CountryCode != "" {
		if !incentiveCountryPattern.MatchString(args.CountryCode) {
			return nil, badRequest("country_code must be two letters")
		}
		query.Set("countryCode", args.CountryCode)
	}
	if args.Email != "" {
		if err := checkEmailFilter("email", args.Email); err != nil {
			return nil, err
		}
		query.Set("email", args.Email)
	}
	if args.IncentiveType != "" {
		if !slices.Contains(incentiveTypes, args.IncentiveType) {
			return nil, badRequest("incentive_type may be ACQUISITION only")
		}
		query.Set("incentiveType", args.IncentiveType)
	}
	c, err := env.open()
	if err != nil {
		return nil, err
	}
	path := "/incentives:fetchIncentive"
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	var answer incentiveAnswer
	if err := c.read(ctx, request{op: op, tool: incentivesFetchID, method: http.MethodGet, path: path}, &answer); err != nil {
		return nil, err
	}
	if answer.IncentiveOffer == nil {
		return nil, provider.InvalidResponse(op, subject+" answered without an incentive offer")
	}
	o := answer.IncentiveOffer
	offer := &incentiveOffer{ConsolidatedTermsAndConditionsURL: incentiveLink(o.ConsolidatedTermsAndConditionsURL),
		OfferType: code(o.OfferType)}
	if o.CyoIncentives != nil {
		offer.CyoIncentives = &incentiveCyo{LowOffer: incentiveOf(o.CyoIncentives.LowOffer),
			MediumOffer: incentiveOf(o.CyoIncentives.MediumOffer), HighOffer: incentiveOf(o.CyoIncentives.HighOffer)}
	}
	return incentivesResult{IncentiveOffer: offer}, nil
}
