package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// fakeRegistry registers two capabilities for the provider the test configuration uses. No provider
// implementation is involved: discovery answers from the registry alone.
func registerBookstackTestMetadata(t *testing.T, reg *capability.Registry) {
	t.Helper()
	if err := reg.RegisterProvider(config.ProviderMetadata{
		ID: "bookstack", Name: "BookStack",
		SecretRoles: []config.SecretRole{{Name: "token-id"}, {Name: "token-secret"}},
		Target:      config.TargetMetadata{Label: "target"},
	}, nil); err != nil {
		t.Fatalf("RegisterProvider() = %v", err)
	}
}

func fakeRegistry(t *testing.T) *capability.Registry {
	t.Helper()
	reg := capability.NewRegistry()
	registerBookstackTestMetadata(t, reg)
	err := reg.Register("bookstack",
		capability.Operation{
			Descriptor: capability.Descriptor{
				ID:          "bookstack.pages.list",
				Version:     1,
				Description: "List pages",
				Risk: capability.Risk{
					Effect:          capability.EffectRead,
					Idempotency:     capability.IdempotencySafe,
					Confirmation:    capability.ConfirmationNone,
					DataSensitivity: "test-data",
				},
				Provider:     "bookstack",
				InputSchema:  json.RawMessage(`{"type":"object"}`),
				OutputSchema: json.RawMessage(`{"type":"array"}`),
				Arguments:    []capability.Argument{{Name: "limit", Description: "Maximum number of pages"}},
				Fields:       []capability.Field{{Name: "id"}, {Name: "name"}},
			},
			Handler: capability.Handler(func(context.Context, *config.Resolved, *secret.Resolver, *redact.Redactor, json.RawMessage) (any, error) {
				return []map[string]any{{"id": 1, "name": "Page"}}, nil
			}),
		},
		capability.Operation{
			Descriptor: capability.Descriptor{
				ID:          "bookstack.pages.get",
				Version:     1,
				Description: "Read one page",
				Risk: capability.Risk{
					Effect:          capability.EffectRead,
					Idempotency:     capability.IdempotencySafe,
					Confirmation:    capability.ConfirmationNone,
					DataSensitivity: "test-data",
				},
				Provider:     "bookstack",
				InputSchema:  json.RawMessage(`{"type":"object"}`),
				OutputSchema: json.RawMessage(`{"type":"object"}`),
				Arguments:    []capability.Argument{{Name: "id", Description: "Page identifier", Required: true}},
				Fields:       []capability.Field{{Name: "html"}},
			},
			Handler: capability.Handler(func(context.Context, *config.Resolved, *secret.Resolver, *redact.Redactor, json.RawMessage) (any, error) {
				return map[string]any{"html": "<p>Page</p>"}, nil
			}),
		},
	)
	if err != nil {
		t.Fatalf("Register() = %v", err)
	}
	return reg
}

// runFakeCLI drives the real command tree with a fake provider registry.
func runFakeCLI(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	opts := &Options{}
	code := run(newRootCommand(opts, fakeRegistry(t)), opts, args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

// runFakeCLIInput is runFakeCLI with stdin content, for the commands that read arguments from stdin.
func runFakeCLIInput(t *testing.T, request string, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	redactor := &redact.Redactor{}
	opts := &Options{
		Input: strings.NewReader(request), Redactor: redactor,
		Secrets: secret.NewWith(nil, nil, nil, redactor),
	}
	code := run(newRootCommand(opts, fakeRegistry(t)), opts, args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

// The shipped registry must be wirable with every operation and provider metadata contract.
func TestDefaultRegistry(t *testing.T) {
	reg := defaultRegistry()
	if reg == nil {
		t.Fatal("defaultRegistry() = nil")
	}
	ids := func(provider string) []string {
		var ids []string
		for _, descriptor := range reg.Provider(provider) {
			ids = append(ids, descriptor.ID)
		}
		return ids
	}
	requireIDs(t, ids("bookstack"), "bookstack.attachments.download", "bookstack.books.create")
	telegram, ok := reg.ProviderMetadata("telegram")
	if !ok || telegram.DefaultBaseURL != "https://api.telegram.org" || !telegram.Target.Required ||
		len(telegram.SecretRoles) != 1 || telegram.SecretRoles[0].Name != "bot-token" {
		t.Errorf("Telegram metadata = %+v, %v", telegram, ok)
	}
	// One anchor per family: message, member, pin, update, chat, invite link, bot, file, media, interaction,
	// structured send, transfer, and sticker operations.
	requireIDs(t, ids("telegram"), "telegram.messages.send", "telegram.members.ban", "telegram.pins.pin",
		"telegram.updates.list", "telegram.chats.get", "telegram.invite-links.primary", "telegram.bot.get",
		"telegram.files.download", "telegram.photos.send", "telegram.callbacks.answer", "telegram.polls.send",
		"telegram.messages.forward", "telegram.sticker-sets.create")
	requireIDs(t, ids("google.measurement-protocol"), "google.measurement-protocol.events.send")
	requireIDs(t, ids("google.ads"), "google.ads.customers.list-accessible", "google.ads.customers.get",
		"google.ads.customer-clients.list", "google.ads.reports.search", "google.ads.fields.search", "google.ads.fields.get",
		"google.ads.customers.update",
		"google.ads.customer-budget-network-settings.create", "google.ads.customer-budget-network-settings.update",
		"google.ads.campaigns.create", "google.ads.campaigns.update", "google.ads.campaigns.remove",
		"google.ads.campaigns.enable-pmax-brand-guidelines",
		"google.ads.ad-groups.create", "google.ads.ad-groups.update", "google.ads.ad-groups.remove",
		"google.ads.asset-groups.create", "google.ads.asset-groups.update", "google.ads.asset-groups.remove",
		"google.ads.ad-group-criteria.create", "google.ads.ad-group-criteria.update", "google.ads.ad-group-criteria.remove",
		"google.ads.ad-group-ads.create", "google.ads.ad-group-ads.update", "google.ads.ad-group-ads.remove",
		"google.ads.ad-group-ads.remove-automatically-created-assets",
		"google.ads.ads.update", "google.ads.ad-parameters.create", "google.ads.ad-parameters.update",
		"google.ads.ad-parameters.remove",
		"google.ads.customer-user-accesses.list", "google.ads.customer-user-accesses.update",
		"google.ads.customer-user-accesses.remove",
		"google.ads.customer-user-access-invitations.list", "google.ads.customer-user-access-invitations.remove",
		"google.ads.multi-party-auth-reviews.list", "google.ads.multi-party-auth-reviews.resolve",
		"google.ads.conversion-actions.create", "google.ads.conversion-actions.update", "google.ads.conversion-actions.remove",
		"google.ads.customer-conversion-goals.update", "google.ads.campaign-conversion-goals.update",
		"google.ads.conversion-goal-campaign-configs.update", "google.ads.customer-sk-ad-network-conversion-value-schemas.update",
		"google.ads.custom-conversion-goals.create", "google.ads.custom-conversion-goals.update", "google.ads.custom-conversion-goals.remove",
		"google.ads.conversion-custom-variables.create", "google.ads.conversion-custom-variables.update",
		"google.ads.conversion-value-rules.create", "google.ads.conversion-value-rules.update", "google.ads.conversion-value-rules.remove",
		"google.ads.remarketing-actions.create", "google.ads.remarketing-actions.update",
		"google.ads.custom-audiences.create", "google.ads.custom-audiences.update", "google.ads.custom-audiences.remove",
		"google.ads.custom-interests.create", "google.ads.custom-interests.update",
		"google.ads.audiences.create", "google.ads.audiences.update",
		"google.ads.customer-manager-links.list", "google.ads.customer-manager-links.update",
		"google.ads.customer-client-links.list", "google.ads.customer-client-links.update",
		"google.ads.account-links.list", "google.ads.product-links.list", "google.ads.product-link-invitations.list",
		"google.ads.data-links.list", "google.ads.account-links.remove", "google.ads.product-links.remove",
		"google.ads.product-link-invitations.update", "google.ads.data-links.remove", "google.ads.data-links.update",
		"google.ads.assets.create", "google.ads.assets.update", "google.ads.campaign-automatically-created-assets.remove",
		"google.ads.campaign-bid-modifiers.create", "google.ads.campaign-bid-modifiers.update",
		"google.ads.campaign-bid-modifiers.remove",
		"google.ads.user-lists.create", "google.ads.user-lists.update", "google.ads.user-lists.remove",
		"google.ads.user-list-customer-types.create", "google.ads.user-list-customer-types.remove",
		"google.ads.billing-setups.list", "google.ads.account-budgets.list", "google.ads.account-budget-proposals.list",
		"google.ads.payments-accounts.list",
		"google.ads.invoices.list", "google.ads.incentives.fetch", "google.ads.third-party-app-analytics-links.list",
		"google.ads.content-creator-insights.generate-creator", "google.ads.content-creator-insights.generate-trending",
		"google.ads.geo-target-constants.suggest", "google.ads.keyword-theme-constants.suggest",
		"google.ads.brand-suggestions.suggest",
		"google.ads.labels.create", "google.ads.labels.update", "google.ads.labels.remove",
		"google.ads.campaign-assets.create", "google.ads.campaign-assets.update", "google.ads.campaign-assets.remove",
		"google.ads.campaign-budgets.create", "google.ads.campaign-budgets.update", "google.ads.campaign-budgets.remove",
		"google.ads.ad-group-assets.create", "google.ads.ad-group-assets.update", "google.ads.ad-group-assets.remove",
		"google.ads.asset-group-assets.create", "google.ads.asset-group-assets.update", "google.ads.asset-group-assets.remove",
		"google.ads.campaign-groups.create", "google.ads.campaign-groups.update", "google.ads.campaign-groups.remove",
		"google.ads.experiments.create", "google.ads.experiments.update", "google.ads.experiments.remove",
		"google.ads.experiments.list-async-errors", "google.ads.experiments.schedule", "google.ads.experiments.end",
		"google.ads.experiments.graduate", "google.ads.experiments.promote",
		"google.ads.campaign-drafts.create", "google.ads.campaign-drafts.update", "google.ads.campaign-drafts.remove",
		"google.ads.campaign-drafts.promote", "google.ads.campaign-drafts.list-async-errors",
		"google.ads.bidding-strategies.create", "google.ads.bidding-strategies.update", "google.ads.bidding-strategies.remove",
		"google.ads.bidding-data-exclusions.create", "google.ads.bidding-data-exclusions.update",
		"google.ads.bidding-data-exclusions.remove",
		"google.ads.bidding-seasonality-adjustments.create", "google.ads.bidding-seasonality-adjustments.update",
		"google.ads.bidding-seasonality-adjustments.remove",
		"google.ads.ad-group-bid-modifiers.create", "google.ads.ad-group-bid-modifiers.update",
		"google.ads.ad-group-bid-modifiers.remove",
		"google.ads.recommendation-subscriptions.create", "google.ads.recommendation-subscriptions.update",
		"google.ads.recommendations.generate", "google.ads.recommendations.apply", "google.ads.recommendations.dismiss",
		"google.ads.keyword-plan-ideas.generate-ideas", "google.ads.keyword-plan-ideas.generate-historical-metrics",
		"google.ads.keyword-plan-ideas.generate-ad-group-themes", "google.ads.keyword-plan-ideas.generate-forecast-metrics",
		"google.ads.keyword-plans.create", "google.ads.keyword-plans.update", "google.ads.keyword-plans.remove",
		"google.ads.reach-plans.list-locations", "google.ads.reach-plans.list-products", "google.ads.reach-plans.generate-forecast",
		"google.ads.reach-plans.generate-conversion-rates", "google.ads.reach-plans.list-user-lists",
		"google.ads.reach-plans.list-user-interests",
		"google.ads.asset-sets.create", "google.ads.asset-sets.update", "google.ads.asset-sets.remove",
		"google.ads.asset-set-assets.create", "google.ads.asset-set-assets.remove",
		"google.ads.campaign-asset-sets.create", "google.ads.campaign-asset-sets.remove",
		"google.ads.ad-group-asset-sets.create", "google.ads.ad-group-asset-sets.remove",
		"google.ads.customer-asset-sets.create", "google.ads.customer-asset-sets.remove",
		"google.ads.customer-assets.create", "google.ads.customer-assets.update", "google.ads.customer-assets.remove",
		"google.ads.campaign-labels.create", "google.ads.campaign-labels.remove",
		"google.ads.ad-group-labels.create", "google.ads.ad-group-labels.remove",
		"google.ads.customer-labels.create", "google.ads.customer-labels.remove",
		"google.ads.customizer-attributes.create", "google.ads.customizer-attributes.remove",
		"google.ads.campaign-customizers.create", "google.ads.campaign-customizers.remove",
		"google.ads.ad-group-customizers.create", "google.ads.ad-group-customizers.remove",
		"google.ads.ad-group-criterion-customizers.create", "google.ads.ad-group-criterion-customizers.remove",
		"google.ads.customer-customizers.create", "google.ads.customer-customizers.remove",
		"google.ads.ad-group-ad-labels.create", "google.ads.ad-group-ad-labels.remove",
		"google.ads.ad-group-criterion-labels.create", "google.ads.ad-group-criterion-labels.remove",
		"google.ads.campaign-criteria.create", "google.ads.campaign-criteria.update", "google.ads.campaign-criteria.remove",
		"google.ads.goals.create", "google.ads.goals.update", "google.ads.campaign-goal-configs.create",
		"google.ads.campaign-goal-configs.update", "google.ads.campaign-goal-configs.remove",
		"google.ads.shared-sets.create", "google.ads.shared-sets.update", "google.ads.shared-sets.remove",
		"google.ads.campaign-shared-sets.create", "google.ads.campaign-shared-sets.remove",
		"google.ads.customer-negative-criteria.create", "google.ads.customer-negative-criteria.remove",
		"google.ads.shared-criteria.create", "google.ads.shared-criteria.remove")
}

// Every shipped provider starts new connections with a valid recommended profile that selects reads only,
// except where the provider states why a change is safe to preselect. Telegram, whose read tools expose the
// content of incoming messages, is the only such provider. A provider whose tools all require an allow-list
// declares no profile and starts without a preselection.
func TestShippedProfilesAreValidAndSafe(t *testing.T) {
	reg := defaultRegistry()
	if err := reg.ValidateProfiles(); err != nil {
		t.Fatal(err)
	}
	for _, metadata := range reg.ProviderMetadataAll() {
		profile, ok := metadata.RecommendedProfile()
		if !ok && len(metadata.Profiles) == 0 && allListedOnly(metadata) {
			continue
		}
		if !ok {
			t.Errorf("provider %s has no recommended profile", metadata.ID)
			continue
		}
		permissions := metadata.ProfileEffects(profile)
		readOnly := len(permissions) == 1 && permissions[0] == config.EffectRead
		switch {
		case metadata.ID == "telegram":
			if profile.MutationReason == "" || len(profile.Tools) != 1 || profile.Tools[0] != "telegram.messages.send" {
				t.Errorf("telegram recommended profile = %+v, want only the explained send", profile)
			}
		case !readOnly || profile.MutationReason != "":
			t.Errorf("provider %s recommended profile %s selects %v, want reads only", metadata.ID, profile.ID,
				permissions)
		}
	}
}

func allListedOnly(metadata config.ProviderMetadata) bool {
	for _, tool := range metadata.Tools {
		if !tool.RequiresToolAllowList {
			return false
		}
	}
	return len(metadata.Tools) > 0
}

// The Infomaniak providers are listed under their current ID only; a former ID resolves through a rename and
// is not a namespace of its own.
func TestProvidersListsVendorFamilyProvidersUnderTheirCurrentIDs(t *testing.T) {
	t.Setenv("QCONNECT_CONFIG", "")
	t.Setenv("QCONNECT_HOME", "")
	path := writeConfig(t, "version: 4\ndefaults: {}\n")
	var reads atomic.Int32
	code, stdout, stderr := runTools(t, &reads, "providers", "--config", path, "--output", "json")
	if code != exitOK || stderr != "" {
		t.Fatalf("providers: exit=%d stderr=%q", code, stderr)
	}
	var listed struct {
		Providers []struct {
			Provider string `json:"provider"`
		} `json:"providers"`
	}
	if err := json.Unmarshal([]byte(stdout), &listed); err != nil {
		t.Fatal(err)
	}
	for current, former := range map[string]string{"infomaniak.chat": "infomaniakchat", "infomaniak.drive": "infomaniakdrive", "infomaniak.mail": "infomaniakmail", "infomaniak.dav": "infomaniakdav", "seatable.account": "seatableaccount"} {
		var hasCurrent, hasFormer bool
		for _, p := range listed.Providers {
			hasCurrent = hasCurrent || p.Provider == current
			hasFormer = hasFormer || p.Provider == former
		}
		if !hasCurrent || hasFormer {
			t.Errorf("providers = %+v, want %s and not %s", listed.Providers, current, former)
		}
	}
}
