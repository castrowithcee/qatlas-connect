package googleanalytics

import (
	"context"
	"encoding/json"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// Proposals are the invitations to link a property with a Display & Video 360 advertiser. Approving, cancelling,
// creating and deleting one are not tools, see dv360links.go.
const (
	dv360ProposalsListID = Provider + ".display-video360-link-proposals.list"
	dv360ProposalsGetID  = Provider + ".display-video360-link-proposals.get"

	dv360ProposalsSegment = "/displayVideo360AdvertiserLinkProposals"
	dv360ProposalsKind    = "displayVideo360AdvertiserLinkProposals"
	// dataSensitivityProposals classifies the proposals: they name the email addresses of the people who
	// proposed a link. They are personal data.
	dataSensitivityProposals = "analytics-link-proposals"
)

const notFoundDV360Proposal = subject + " did not find the Display & Video 360 link proposal, or the credential " +
	"may not see it"

var proposalRisk = capability.Risk{Effect: capability.EffectRead, Idempotency: capability.IdempotencySafe,
	Confirmation: capability.ConfirmationNone, OpenWorld: true, DataSensitivity: dataSensitivityProposals}

type dv360ProposalName string

func (n *dv360ProposalName) UnmarshalJSON(b []byte) error {
	var value string
	if json.Unmarshal(b, &value) == nil {
		if _, ok := splitDV360Name(dv360ProposalsKind, value); ok {
			*n = dv360ProposalName(value)
		}
	}
	return nil
}

// proposalStatus is the LinkProposalStatusDetails of a proposal. The requestor email is personal data.
type proposalStatus struct {
	RequestorEmail    text `json:"requestorEmail,omitempty"`
	LinkProposalState code `json:"linkProposalState,omitempty"`
	InitiatingProduct code `json:"linkProposalInitiatingProduct,omitempty"`
}

// dv360Proposal is the DisplayVideo360AdvertiserLinkProposal resource of analyticsadmin v1alpha, every
// documented member. Google documents validationEmail as input only, so an answer normally leaves it out.
type dv360Proposal struct {
	Name                       dv360ProposalName `json:"name,omitempty"`
	AdvertiserID               advertiserID      `json:"advertiserId,omitempty"`
	AdvertiserDisplayName      text              `json:"advertiserDisplayName,omitempty"`
	ValidationEmail            text              `json:"validationEmail,omitempty"`
	LinkProposalStatusDetails  *proposalStatus   `json:"linkProposalStatusDetails,omitempty"`
	AdsPersonalizationEnabled  *bool             `json:"adsPersonalizationEnabled,omitempty"`
	CampaignDataSharingEnabled *bool             `json:"campaignDataSharingEnabled,omitempty"`
	CostDataSharingEnabled     *bool             `json:"costDataSharingEnabled,omitempty"`
}

const dv360ProposalSchema = `{"type":"object","properties":{"name":{"type":"string"},"advertiserId":{"type":"string"},` +
	`"advertiserDisplayName":{"type":"string"},"validationEmail":{"type":"string"},` +
	`"linkProposalStatusDetails":{"type":"object","properties":{"requestorEmail":{"type":"string"},` +
	`"linkProposalState":{"type":"string"},"linkProposalInitiatingProduct":{"type":"string"}},` +
	`"additionalProperties":false},"adsPersonalizationEnabled":{"type":"boolean"},` +
	`"campaignDataSharingEnabled":{"type":"boolean"},"costDataSharingEnabled":{"type":"boolean"}},` +
	`"additionalProperties":false}`

const dv360ProposalFieldsText = "name (properties/ID/displayVideo360AdvertiserLinkProposals/ID, the proposal ID " +
	"is not the advertiser ID), advertiserId, advertiserDisplayName (cut to 300 characters, only for proposals that " +
	"came from Display & Video 360), validationEmail (an email address, documented as input only so usually " +
	"absent), linkProposalStatusDetails with linkProposalState (AWAITING_REVIEW_FROM_GOOGLE_ANALYTICS, " +
	"AWAITING_REVIEW_FROM_LINKED_PRODUCT, WITHDRAWN, DECLINED, EXPIRED or OBSOLETE; an open proposal expires " +
	"after some time and the others are deleted after some time), linkProposalInitiatingProduct " +
	"(GOOGLE_ANALYTICS or LINKED_PRODUCT) and requestorEmail (the email address of the user who proposed the " +
	"link), and the settings adsPersonalizationEnabled, campaignDataSharingEnabled and costDataSharingEnabled. A " +
	"member Google does not send is left out"

const dv360ProposalWarning = " PERSONAL DATA: a proposal names the email address of the person who proposed the " +
	"link, and can name a validation email address. Read only what the task needs and do not pass the addresses " +
	"on. Approving or cancelling a proposal is not a tool; a person does that in Analytics or Display & Video 360."

type (
	dv360ProposalsPage struct {
		Proposals     []dv360Proposal `json:"displayVideo360AdvertiserLinkProposals"`
		NextPageToken string          `json:"nextPageToken"`
	}
	dv360ProposalsResult struct {
		Proposals  []dv360Proposal `json:"displayVideo360AdvertiserLinkProposals"`
		NextCursor string          `json:"next_cursor,omitempty"`
		HasMore    bool            `json:"has_more"`
	}
)

var dv360ProposalsList = capability.Descriptor{
	ID: dv360ProposalsListID, Version: 1, Title: "List Display & Video 360 link proposals",
	Description: "List the proposals to link a Google Analytics property with a Display & Video 360 advertiser " +
		"(properties.displayVideo360AdvertiserLinkProposals.list of the Analytics Admin API) with name, advertiser, " +
		"status of the proposal, the people involved and the settings. On a connection with targets the property " +
		"must be a bound property or belong to a bound account, which is proven by one read of its parent before " +
		"the list. Reads no report data." + dv360ProposalWarning + dv360Untrusted + alphaNote,
	Tags: []string{"google", "analytics", "display-video360-link-proposals", "list", "personal-data", "alpha"},
	Risk: proposalRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{"property":` + propertyArgSchema + `,` +
		pageArgumentsSchema + `},"required":["property"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"displayVideo360AdvertiserLinkProposals":` +
		`{"type":"array","items":` + dv360ProposalSchema + `},"next_cursor":{"type":"string"},` +
		`"has_more":{"type":"boolean"}},"additionalProperties":false}`),
	Arguments: []capability.Argument{
		{Name: "property", Required: true, Description: "Property resource name properties/ID, from " +
			"accounts.list-summaries or properties.list"},
		{Name: "page_size", Description: "Proposals per page, 1 to 200; Google returns at most 50 without it"},
		{Name: "cursor", Description: "Opaque next_cursor of a previous page; the first page when omitted. It " +
			"only fits the same connection and property"},
	},
	Fields: []capability.Field{
		{Name: "displayVideo360AdvertiserLinkProposals", Description: "Per proposal: " + dv360ProposalFieldsText +
			". A page can be empty while has_more is true"},
		{Name: "next_cursor", Description: "Cursor of the following page, absent when has_more is false"},
		{Name: "has_more", Description: "true when Google reported a further page"},
	},
	Examples: []capability.Example{{Description: "The link proposals of property 200",
		Arguments: json.RawMessage(`{"property":"properties/200"}`)}},
}

var dv360ProposalsGet = capability.Descriptor{
	ID: dv360ProposalsGetID, Version: 1, Title: "Get a Display & Video 360 link proposal",
	Description: "Read one proposal to link a Google Analytics property with a Display & Video 360 advertiser " +
		"(properties.displayVideo360AdvertiserLinkProposals.get of the Analytics Admin API) with name, advertiser, " +
		"status of the proposal, the people involved and the settings. On a connection with targets the property " +
		"of the proposal must be a bound property or belong to a bound account, which is proven by one read of " +
		"its parent. Reads no report data." + dv360ProposalWarning + dv360Untrusted + alphaNote,
	Tags: []string{"google", "analytics", "display-video360-link-proposals", "get", "personal-data", "alpha"},
	Risk: proposalRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","minLength":1,` +
		`"maxLength":80}},"required":["name"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(dv360ProposalSchema),
	Arguments: []capability.Argument{{Name: "name", Required: true, Description: "Proposal resource name " +
		"properties/ID/displayVideo360AdvertiserLinkProposals/ID, from display-video360-link-proposals.list"}},
	Fields: []capability.Field{{Name: "(result)", Description: "The proposal: " + dv360ProposalFieldsText}},
	Examples: []capability.Example{{Description: "Read one proposal",
		Arguments: json.RawMessage(`{"name":"properties/200/displayVideo360AdvertiserLinkProposals/5000"}`)}},
}

func handleDV360ProposalsList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	var page dv360ProposalsPage
	cursor, property, err := dv360List(ctx, resolved, secrets, red, raw, "list Display & Video 360 link proposals",
		dv360ProposalsListID, dv360ProposalsSegment, &page)
	if err != nil {
		return nil, err
	}
	out := dv360ProposalsResult{Proposals: []dv360Proposal{}}
	// Only proposals of the asked property are shown; an entry without a valid name of it is dropped.
	for _, proposal := range head(page.Proposals, maxSummaries) {
		if owner, ok := splitDV360Name(dv360ProposalsKind, string(proposal.Name)); ok && owner == property {
			out.Proposals = append(out.Proposals, proposal)
		}
	}
	if out.NextCursor, out.HasMore, err = cursor.next(page.NextPageToken); err != nil {
		return nil, err
	}
	return out, nil
}

func handleDV360ProposalsGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "get Display & Video 360 link proposal"
	var out dv360Proposal
	name, err := dv360Get(ctx, resolved, secrets, red, raw, op, dv360ProposalsGetID, dv360ProposalsKind,
		notFoundDV360Proposal, &out)
	if err != nil {
		return nil, err
	}
	if string(out.Name) != name {
		return nil, provider.InvalidResponse(op, subject+" answered with another proposal than the one asked for")
	}
	return out, nil
}
