package googleads

import "github.com/castrowithcee/qatlas-connect/internal/config"

// scopeAdwords is the only scope of the Google Ads API. It covers reading and changing alike, so what a
// connection may do is bounded by its effects, tools and targets, not by the token.
const scopeAdwords = "https://www.googleapis.com/auth/adwords"

// tokenMetadata maps every tool to the scopes it needs. The core derives the scopes of a connection from it.
var tokenMetadata = config.TokenMetadata{Scopes: map[string][]string{
	customersListAccessibleID:             {scopeAdwords},
	customersGetID:                        {scopeAdwords},
	customerClientsListID:                 {scopeAdwords},
	reportsSearchID:                       {scopeAdwords},
	fieldsSearchID:                        {scopeAdwords},
	fieldsGetID:                           {scopeAdwords},
	customersUpdateID:                     {scopeAdwords},
	conversionActionsCreateID:             {scopeAdwords},
	conversionActionsUpdateID:             {scopeAdwords},
	conversionActionsRemoveID:             {scopeAdwords},
	customerBudgetNetworkSettingsCreateID: {scopeAdwords},
	customerBudgetNetworkSettingsUpdateID: {scopeAdwords},
	customerUserAccessesListID:            {scopeAdwords},
	customerUserAccessesUpdateID:          {scopeAdwords},
	customerUserAccessesRemoveID:          {scopeAdwords},
	customerUserAccessInvitationsListID:   {scopeAdwords},
	customerUserAccessInvitationsRemoveID: {scopeAdwords},
	multiPartyAuthReviewsListID:           {scopeAdwords},
	multiPartyAuthReviewsResolveID:        {scopeAdwords},
	customInterestsCreateID:               {scopeAdwords},
	customInterestsUpdateID:               {scopeAdwords},
	audiencesCreateID:                     {scopeAdwords},
	audiencesUpdateID:                     {scopeAdwords},
}}
