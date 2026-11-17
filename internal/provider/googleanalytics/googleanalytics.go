// Package googleanalytics implements the Google Analytics provider on the Analytics Admin API and the
// Analytics Data API with a credential that issues access tokens: a person's consent, a service account key,
// or the impersonation of a service account. The token roles, the procedure and the renewal come from the shared token source; this
// package adds the scope every tool needs, the two fixed origins, the targets and the error mapping.
//
// A report runs on the Data API origin and costs tokens of the property quota. The package keeps the
// remainder Google reports for each property, runs at most the documented number of reports of a property at
// once, and refuses a report before any I/O when a bucket is nearly used up.
// The funnel report runs on the alpha version of the Data API and has its own Funnel quota memory. Starting
// an audience export is a Core request of the same origin and follows the Core rules; reading or listing
// exports uses no gate. Report tasks run on the alpha version too: starting one is counted with the Core
// category like an export, reading, listing and querying one uses no gate, and so does querying the users of
// an export, which are personal data.
//
// Reading tools send one request, a changing tool exactly one and never again, whatever comes back.
// Deleting an account runs only on a connection that binds exactly that account and no property. The value
// of a measurement protocol secret is never returned: it is read from Google's answer only to register it with
// the redactor.
//
// The settings and the event create rules of a web data stream and the calculated metrics, annotations, audiences
// and channel groups of a property run on the Admin API v1alpha, see alpha.go.
//
// A connection may bind Analytics accounts (accounts/ID) and properties (properties/ID). Without a target it
// reaches what the credential reaches. A bound account reaches its properties, a bound property only itself;
// a property that is not bound itself is read once as proof of its account before anything else is done with
// it. Everything Google returns is untrusted third-party content: strings and lists are bounded and resource
// names are kept only in their documented form.
package googleanalytics

import (
	"context"
	"errors"
	"net/url"
	"slices"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/provider/googleapi"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// Provider is the provider name used in the configuration and as the operation namespace.
const Provider = "google.analytics"

// subject names the provider in messages.
const subject = "Google Analytics"

// scopeReadOnly is the smallest scope that reads Analytics configuration and report data (analyticsadmin and
// analyticsdata v1beta and v1alpha). Google also lists it for audienceLists.create.
const scopeReadOnly = "https://www.googleapis.com/auth/analytics.readonly"

// scopeEdit is the scope that changes Analytics accounts, properties and annotations (analyticsadmin v1beta and
// v1alpha).
const scopeEdit = "https://www.googleapis.com/auth/analytics.edit"

// dataSensitivity classifies the results: the configuration of the Analytics accounts the credential reaches.
const dataSensitivity = "analytics-configuration"

// readRisk is the risk of every reading tool.
var readRisk = capability.Risk{Effect: capability.EffectRead, Idempotency: capability.IdempotencySafe,
	Confirmation: capability.ConfirmationNone, OpenWorld: true, DataSensitivity: dataSensitivity}

// scopes maps every tool to the smallest scopes it needs. It is the one source of the token metadata.
var scopes = map[string][]string{
	summariesID:       {scopeReadOnly},
	accountsID:        {scopeReadOnly},
	accountsGetID:     {scopeReadOnly},
	accountsSharingID: {scopeReadOnly},
	propertiesListID:  {scopeReadOnly},
	propertiesGetID:   {scopeReadOnly},
	reportsRunID:      {scopeReadOnly},
	metadataGetID:     {scopeReadOnly},
	compatibilityID:   {scopeReadOnly},
	funnelRunID:       {scopeReadOnly},
	audienceGetID:     {scopeReadOnly},
	audienceListID:    {scopeReadOnly},
	audienceCreateID:  {scopeReadOnly},

	reportsRunPivotID:      {scopeReadOnly},
	reportsBatchRunID:      {scopeReadOnly},
	reportsBatchRunPivotID: {scopeReadOnly},
	reportsRunRealtimeID:   {scopeReadOnly},
	audienceQueryID:        {scopeReadOnly},
	reportTasksCreateID:    {scopeReadOnly},
	reportTasksGetID:       {scopeReadOnly},
	reportTasksListID:      {scopeReadOnly},
	reportTasksQueryID:     {scopeReadOnly},

	accountsUpdateID:   {scopeEdit},
	accountsDeleteID:   {scopeEdit},
	propertiesCreateID: {scopeEdit},
	propertiesUpdateID: {scopeEdit},
	propertiesDeleteID: {scopeEdit},

	dataStreamsListID:   {scopeReadOnly},
	dataStreamsGetID:    {scopeReadOnly},
	dataStreamsCreateID: {scopeEdit},
	dataStreamsUpdateID: {scopeEdit},
	dataStreamsDeleteID: {scopeEdit},

	mpSecretsListID:   {scopeReadOnly},
	mpSecretsGetID:    {scopeReadOnly},
	mpSecretsDeleteID: {scopeEdit},

	enhancedGetID:     {scopeReadOnly},
	redactionGetID:    {scopeReadOnly},
	siteTagGetID:      {scopeReadOnly},
	enhancedUpdateID:  {scopeEdit},
	redactionUpdateID: {scopeEdit},

	customDimensionsListID:    {scopeReadOnly},
	customDimensionsGetID:     {scopeReadOnly},
	customDimensionsCreateID:  {scopeEdit},
	customDimensionsUpdateID:  {scopeEdit},
	customDimensionsArchiveID: {scopeEdit},

	customMetricsListID:    {scopeReadOnly},
	customMetricsGetID:     {scopeReadOnly},
	customMetricsCreateID:  {scopeEdit},
	customMetricsUpdateID:  {scopeEdit},
	customMetricsArchiveID: {scopeEdit},

	keyEventsListID:   {scopeReadOnly},
	keyEventsGetID:    {scopeReadOnly},
	keyEventsCreateID: {scopeEdit},
	keyEventsUpdateID: {scopeEdit},
	keyEventsDeleteID: {scopeEdit},

	eventRulesListID:   {scopeReadOnly},
	eventRulesGetID:    {scopeReadOnly},
	eventRulesCreateID: {scopeEdit},
	eventRulesUpdateID: {scopeEdit},
	eventRulesDeleteID: {scopeEdit},

	calculatedListID:   {scopeReadOnly},
	calculatedGetID:    {scopeReadOnly},
	calculatedCreateID: {scopeEdit},
	calculatedUpdateID: {scopeEdit},
	calculatedDeleteID: {scopeEdit},

	accessAccountID:  {scopeReadOnly},
	accessPropertyID: {scopeReadOnly},

	audiencesListID:    {scopeReadOnly},
	audiencesGetID:     {scopeReadOnly},
	audiencesCreateID:  {scopeEdit},
	audiencesUpdateID:  {scopeEdit},
	audiencesArchiveID: {scopeEdit},

	annotationsListID:   {scopeReadOnly},
	annotationsGetID:    {scopeReadOnly},
	annotationsCreateID: {scopeEdit},
	annotationsUpdateID: {scopeEdit},
	annotationsDeleteID: {scopeEdit},

	channelGroupsListID:   {scopeReadOnly},
	channelGroupsGetID:    {scopeReadOnly},
	channelGroupsCreateID: {scopeEdit},
	channelGroupsUpdateID: {scopeEdit},
	channelGroupsDeleteID: {scopeEdit},

	dataRetentionGetID:    {scopeReadOnly},
	dataRetentionUpdateID: {scopeEdit},
	googleSignalsGetID:    {scopeReadOnly},
	googleSignalsUpdateID: {scopeEdit},
}

func tokenMetadata() config.TokenMetadata { return config.TokenMetadata{Scopes: scopes} }

// tokenOptions are the options of the token source of every call: Google's grants through the package
// transport and the token cache of the package.
func tokenOptions() provider.TokenOptions {
	return provider.TokenOptions{Token: tokenMetadata(), Grants: googleapi.TokenGrants(transport), Cache: tokens}
}

// TestConnection obtains an access token and lists one account summary, which reads no report data and so
// uses no reporting quota. A summary proves the token, an enabled Analytics Admin API in the Cloud project of
// the credential and at least one visible account; none is not-found. A bound property is read as well. The
// Analytics Data API is not proven.
func TestConnection(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor) (provider.Class, error) {
	bind, err := bindingOf(resolved)
	if err != nil {
		return testClass(err)
	}
	return provider.TestTokenConnection(ctx, resolved, secrets, red, tokenOptions(),
		func(ctx context.Context, source *provider.TokenSource) (provider.Class, error) {
			c := newClient(resolved, source, bind)
			const op = "test connection"
			var page summariesPage
			if err := c.get(ctx, apiCall{op: op, tool: summariesID, path: summariesPath,
				query: url.Values{"pageSize": {"1"}}, limit: maxSummariesBytes, notFound: notFoundGeneric}, &page); err != nil {
				return "", err
			}
			if len(page.AccountSummaries) == 0 {
				return provider.ClassNotFound, nil
			}
			for _, name := range bind.properties {
				if _, err := c.getProperty(ctx, op, summariesID, name); err != nil {
					return "", err
				}
			}
			return provider.ClassOK, nil
		})
}

// testClass turns a provider failure into the class of a connection test; any other error is returned.
func testClass(err error) (provider.Class, error) {
	var failed *provider.Error
	if errors.As(err, &failed) {
		return failed.Class, nil
	}
	return "", err
}

// isDefaultRoot reports whether a configured base URL is the Admin API origin or empty. The origin is fixed,
// so any other value could only suggest that it is not.
func isDefaultRoot(raw string) bool {
	trimmed := strings.TrimRight(strings.TrimSpace(raw), "/")
	return trimmed == "" || trimmed == adminRoot
}

// readTools are the tools of the profile read, and the start of every wider profile.
var readTools = []string{summariesID, accountsID, accountsGetID, accountsSharingID, propertiesListID, propertiesGetID,
	reportsRunID, metadataGetID, compatibilityID, dataStreamsListID, dataStreamsGetID, funnelRunID, audienceGetID,
	audienceListID, reportsRunPivotID, reportsBatchRunID, reportsBatchRunPivotID, reportsRunRealtimeID,
	reportTasksGetID, reportTasksListID, reportTasksQueryID, audienceQueryID, mpSecretsListID, mpSecretsGetID,
	enhancedGetID, redactionGetID, siteTagGetID,
	customDimensionsListID, customDimensionsGetID, customMetricsListID,
	customMetricsGetID, keyEventsListID, keyEventsGetID, eventRulesListID, eventRulesGetID,
	calculatedListID, calculatedGetID, accessAccountID, accessPropertyID, dataRetentionGetID, googleSignalsGetID,
	annotationsListID, annotationsGetID,
	audiencesListID, audiencesGetID, channelGroupsListID, channelGroupsGetID}

// Register adds the provider metadata, its consent endpoints, its connection test and its tools.
func Register(reg *capability.Registry) error {
	token := tokenMetadata()
	if err := reg.RegisterProvider(config.ProviderMetadata{
		ID: Provider, Name: subject, DefaultBaseURL: adminRoot,
		Description:    "Google Analytics 4 accounts, properties, annotations and reports through the Analytics Admin and Data APIs with an OAuth or service account token",
		DefaultEffects: []config.Effect{config.EffectRead},
		ValidateBaseURL: func(raw string) error {
			if !isDefaultRoot(raw) {
				return errors.New("a Google Analytics service has a fixed origin; leave the base URL as " + adminRoot)
			}
			return nil
		},
		SecretRoles: config.TokenSecretRoles(),
		Token:       &token,
		Target: config.TargetMetadata{
			Label: "account or property",
			Description: "optional: accounts/ID binds an Analytics account with its properties, properties/ID one " +
				"property; without a target the connection reaches what the credential reaches",
			Multiple: true,
			Kinds:    targetKinds,
			Validate: validateTarget,
			ValidateSet: func(targets []string) error {
				if len(targets) > maxTargets {
					return errors.New("a Google Analytics connection binds at most 100 targets")
				}
				return nil
			},
		},
		Profiles: []config.ToolProfile{{
			ID: "read", Title: "Read", Recommended: true,
			Description: "reads the Analytics accounts, properties, custom dimensions and metrics, key events, calculated metrics, audiences, channel groups, reports and report tasks the connection reaches; " +
				"nothing is changed. It includes reading the users of an audience export and the Data Access Reports, " +
				"which are personal data",
			Tools: readTools,
		}, {
			ID: "write", Title: "Read and change",
			Description: "reads what the read profile reads and changes Analytics accounts, properties, data streams, custom " +
				"dimensions, custom metrics, key events, event create rules, calculated metrics, audiences, annotations, channel groups and Google Signals, starts audience exports and report tasks, " +
				"and deletes data streams, measurement protocol secrets, key events and channel groups and archives audiences; every change needs its own " +
				"confirmation, and deleting an account or a property stays unlisted, and so does changing the data retention",
			Tools: append(slices.Clone(readTools), accountsUpdateID, propertiesCreateID, propertiesUpdateID,
				dataStreamsCreateID, dataStreamsUpdateID, dataStreamsDeleteID, mpSecretsDeleteID, audienceCreateID,
				reportTasksCreateID, enhancedUpdateID, redactionUpdateID, customDimensionsCreateID,
				customDimensionsUpdateID, customDimensionsArchiveID,
				customMetricsCreateID, customMetricsUpdateID, customMetricsArchiveID,
				keyEventsCreateID, keyEventsUpdateID, keyEventsDeleteID, eventRulesCreateID, eventRulesUpdateID, eventRulesDeleteID,
				calculatedCreateID, calculatedUpdateID, calculatedDeleteID, googleSignalsUpdateID,
				annotationsCreateID, annotationsUpdateID, annotationsDeleteID,
				audiencesCreateID, audiencesUpdateID, audiencesArchiveID,
				channelGroupsCreateID, channelGroupsUpdateID, channelGroupsDeleteID),
		}},
	}, TestConnection); err != nil {
		return err
	}
	if err := reg.RegisterOAuth(Provider, googleapi.OAuth); err != nil {
		return err
	}
	for _, operation := range []capability.Operation{
		{Descriptor: accountSummariesList, Handler: handleAccountSummaries},
		{Descriptor: accountsList, Handler: handleAccountsList},
		{Descriptor: propertiesList, Handler: handlePropertiesList},
		{Descriptor: propertiesGet, Handler: handlePropertiesGet},
		{Descriptor: accountsGet, Handler: handleAccountsGet},
		{Descriptor: reportsRun, Handler: handleReportsRun},
		{Descriptor: metadataGet, Handler: handleMetadataGet},
		{Descriptor: compatibilityCheck, Handler: handleCompatibilityCheck},
		{Descriptor: reportsRunPivot, Handler: handleReportsRunPivot},
		{Descriptor: reportsBatchRun, Handler: handleReportsBatchRun},
		{Descriptor: reportsBatchRunPivot, Handler: handleReportsBatchRunPivot},
		{Descriptor: reportsRunRealtime, Handler: handleReportsRunRealtime},
		{Descriptor: accountsSharing, Handler: handleAccountsSharing},
		{Descriptor: accountsUpdate, Handler: handleAccountsUpdate, ConfirmationSummary: updateSummary},
		{Descriptor: accountsDelete, Handler: handleAccountsDelete, ConfirmationSummary: deleteSummary},
		{Descriptor: propertiesCreate, Handler: handlePropertiesCreate,
			ConfirmationSummary: propertyWriteSummary(propertiesCreateID)},
		{Descriptor: propertiesUpdate, Handler: handlePropertiesUpdate,
			ConfirmationSummary: propertyWriteSummary(propertiesUpdateID)},
		{Descriptor: propertiesDelete, Handler: handlePropertiesDelete,
			ConfirmationSummary: propertyWriteSummary(propertiesDeleteID)},
		{Descriptor: dataStreamsList, Handler: handleDataStreamsList},
		{Descriptor: dataStreamsGet, Handler: handleDataStreamsGet},
		{Descriptor: dataStreamsCreate, Handler: handleDataStreamsCreate,
			ConfirmationSummary: streamWriteSummary(dataStreamsCreateID)},
		{Descriptor: dataStreamsUpdate, Handler: handleDataStreamsUpdate,
			ConfirmationSummary: streamWriteSummary(dataStreamsUpdateID)},
		{Descriptor: dataStreamsDelete, Handler: handleDataStreamsDelete, ConfirmationSummary: streamDeleteSummary},
		{Descriptor: mpSecretsList, Handler: handleSecretsList},
		{Descriptor: mpSecretsGet, Handler: handleSecretsGet},
		{Descriptor: mpSecretsDelete, Handler: handleSecretsDelete, ConfirmationSummary: secretDeleteSummary},
		{Descriptor: enhancedGet, Handler: handleEnhancedGet},
		{Descriptor: enhancedUpdate, Handler: handleEnhancedUpdate, ConfirmationSummary: settingsSummary(enhancedUpdateID)},
		{Descriptor: redactionGet, Handler: handleRedactionGet},
		{Descriptor: redactionUpdateDescriptor, Handler: handleRedactionUpdate,
			ConfirmationSummary: settingsSummary(redactionUpdateID)},
		{Descriptor: siteTagGet, Handler: handleSiteTagGet},
		{Descriptor: customDimensionsList, Handler: handleCustomDimensionsList},
		{Descriptor: customDimensionsGet, Handler: handleCustomDimensionsGet},
		{Descriptor: customDimensionsCreate, Handler: handleCustomDimensionsCreate,
			ConfirmationSummary: dimensionWriteSummary(customDimensionsCreateID)},
		{Descriptor: customDimensionsUpdate, Handler: handleCustomDimensionsUpdate,
			ConfirmationSummary: dimensionWriteSummary(customDimensionsUpdateID)},
		{Descriptor: customDimensionsArchive, Handler: handleCustomDimensionsArchive,
			ConfirmationSummary: dimensionWriteSummary(customDimensionsArchiveID)},
		{Descriptor: customMetricsList, Handler: handleCustomMetricsList},
		{Descriptor: customMetricsGet, Handler: handleCustomMetricsGet},
		{Descriptor: customMetricsCreate, Handler: handleCustomMetricsCreate,
			ConfirmationSummary: metricWriteSummary(customMetricsCreateID)},
		{Descriptor: customMetricsUpdate, Handler: handleCustomMetricsUpdate,
			ConfirmationSummary: metricWriteSummary(customMetricsUpdateID)},
		{Descriptor: customMetricsArchive, Handler: handleCustomMetricsArchive,
			ConfirmationSummary: metricWriteSummary(customMetricsArchiveID)},
		{Descriptor: keyEventsList, Handler: handleKeyEventsList},
		{Descriptor: keyEventsGet, Handler: handleKeyEventsGet},
		{Descriptor: keyEventsCreate, Handler: handleKeyEventsCreate, ConfirmationSummary: keyEventSummary(keyEventsCreateID)},
		{Descriptor: keyEventsUpdate, Handler: handleKeyEventsUpdate, ConfirmationSummary: keyEventSummary(keyEventsUpdateID)},
		{Descriptor: keyEventsDelete, Handler: handleKeyEventsDelete, ConfirmationSummary: keyEventSummary(keyEventsDeleteID)},
		{Descriptor: eventRulesList, Handler: handleEventRulesList},
		{Descriptor: eventRulesGet, Handler: handleEventRulesGet},
		{Descriptor: eventRulesCreate, Handler: handleEventRulesCreate,
			ConfirmationSummary: ruleWriteSummary(eventRulesCreateID)},
		{Descriptor: eventRulesUpdate, Handler: handleEventRulesUpdate,
			ConfirmationSummary: ruleWriteSummary(eventRulesUpdateID)},
		{Descriptor: eventRulesDelete, Handler: handleEventRulesDelete,
			ConfirmationSummary: ruleWriteSummary(eventRulesDeleteID)},
		{Descriptor: calculatedList, Handler: handleCalculatedList},
		{Descriptor: calculatedGet, Handler: handleCalculatedGet},
		{Descriptor: calculatedCreate, Handler: handleCalculatedCreate,
			ConfirmationSummary: calculatedWriteSummary(calculatedCreateID)},
		{Descriptor: calculatedUpdate, Handler: handleCalculatedUpdate,
			ConfirmationSummary: calculatedWriteSummary(calculatedUpdateID)},
		{Descriptor: calculatedDelete, Handler: handleCalculatedDelete,
			ConfirmationSummary: calculatedWriteSummary(calculatedDeleteID)},
		{Descriptor: audiencesList, Handler: handleAudiencesList},
		{Descriptor: audiencesGet, Handler: handleAudiencesGet},
		{Descriptor: audiencesCreate, Handler: handleAudiencesCreate,
			ConfirmationSummary: audienceWriteSummary(audiencesCreateID)},
		{Descriptor: audiencesUpdate, Handler: handleAudiencesUpdate,
			ConfirmationSummary: audienceWriteSummary(audiencesUpdateID)},
		{Descriptor: audiencesArchive, Handler: handleAudiencesArchive,
			ConfirmationSummary: audienceWriteSummary(audiencesArchiveID)},
		{Descriptor: annotationsList, Handler: handleAnnotationsList},
		{Descriptor: annotationsGet, Handler: handleAnnotationsGet},
		{Descriptor: annotationsCreate, Handler: handleAnnotationsCreate,
			ConfirmationSummary: annotationSummary(annotationsCreateID)},
		{Descriptor: annotationsUpdate, Handler: handleAnnotationsUpdate,
			ConfirmationSummary: annotationSummary(annotationsUpdateID)},
		{Descriptor: annotationsDelete, Handler: handleAnnotationsDelete,
			ConfirmationSummary: annotationSummary(annotationsDeleteID)},
		{Descriptor: channelGroupsList, Handler: handleChannelGroupsList},
		{Descriptor: channelGroupsGet, Handler: handleChannelGroupsGet},
		{Descriptor: channelGroupsCreate, Handler: handleChannelGroupsCreate,
			ConfirmationSummary: channelGroupSummary(channelGroupsCreateID)},
		{Descriptor: channelGroupsUpdate, Handler: handleChannelGroupsUpdate,
			ConfirmationSummary: channelGroupSummary(channelGroupsUpdateID)},
		{Descriptor: channelGroupsDelete, Handler: handleChannelGroupsDelete,
			ConfirmationSummary: channelGroupSummary(channelGroupsDeleteID)},
		{Descriptor: funnelRun, Handler: handleFunnelRun},
		{Descriptor: audienceGet, Handler: handleAudienceGet},
		{Descriptor: audienceList, Handler: handleAudienceList},
		{Descriptor: audienceCreate, Handler: handleAudienceCreate, ConfirmationSummary: audienceCreateSummary},
		{Descriptor: audienceQuery, Handler: handleAudienceQuery},
		{Descriptor: reportTasksCreate, Handler: handleReportTasksCreate, ConfirmationSummary: reportTaskCreateSummary},
		{Descriptor: reportTasksGet, Handler: handleReportTasksGet},
		{Descriptor: reportTasksList, Handler: handleReportTasksList},
		{Descriptor: reportTasksQuery, Handler: handleReportTasksQuery},
		{Descriptor: accessAccountReport, Handler: handleAccessAccount},
		{Descriptor: accessPropertyReport, Handler: handleAccessProperty},
		{Descriptor: dataRetentionGet, Handler: handleDataRetentionGet},
		{Descriptor: dataRetentionUpdate, Handler: handleDataRetentionUpdate, ConfirmationSummary: retentionSummary},
		{Descriptor: googleSignalsGet, Handler: handleGoogleSignalsGet},
		{Descriptor: googleSignalsUpdate, Handler: handleGoogleSignalsUpdate, ConfirmationSummary: signalsSummary},
	} {
		if err := reg.Register(Provider, operation); err != nil {
			return err
		}
	}
	return nil
}
