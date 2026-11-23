package googlemerchant

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"strconv"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	dataSourcesListID   = Provider + ".data-sources.list"
	dataSourcesGetID    = Provider + ".data-sources.get"
	dataSourcesCreateID = Provider + ".data-sources.create"
	dataSourcesUpdateID = Provider + ".data-sources.update"
)

// dataSourcesAPI is the path prefix of the data sources sub-API.
const dataSourcesAPI = "datasources/v1/"

// Bounds of the list tool. Google returns every data source of the account when no page size is sent and
// coerces a size above 1000 to 1000, so Qatlas always sends one.
const (
	dataSourcesPageSize    = 100
	dataSourcesMaxPageSize = 1000
	// maxNested bounds every list inside one data source that a result shows.
	maxNested = 300
)

// The members of a data source are those of the datasources v1 discovery document. Text types cut
// themselves while they are decoded. Of the fetch settings of a file source the password is not a member,
// so it is never held, and the user info of the address is removed.

// capped is a list that keeps at most maxNested entries and drops an entry of the wrong type.
type capped[T any] []T

func (l *capped[T]) UnmarshalJSON(b []byte) error {
	var raw []json.RawMessage
	if json.Unmarshal(b, &raw) != nil {
		*l = nil
		return nil
	}
	out := make([]T, 0, min(len(raw), maxNested))
	for _, item := range raw {
		if len(out) == maxNested {
			break
		}
		var value T
		if json.Unmarshal(item, &value) == nil {
			out = append(out, value)
		}
	}
	*l = out
	return nil
}

// fetchURL is the address a file source is fetched from. It is text that is never fetched here, and a user
// name or password inside it is removed.
type fetchURL string

func (s *fetchURL) UnmarshalJSON(b []byte) error {
	text := clipJSON(b, maxURLRunes)
	if parsed, err := url.Parse(text); err == nil && parsed.User != nil {
		parsed.User = nil
		text = parsed.String()
	}
	*s = fetchURL(text)
	return nil
}

type destination struct {
	Destination idText `json:"destination,omitempty"`
	State       idText `json:"state,omitempty"`
}

type dataSourceReference struct {
	SupplementalDataSourceName idText `json:"supplementalDataSourceName,omitempty"`
	Self                       bool   `json:"self,omitempty"`
}

type defaultRule struct {
	TakeFromDataSources capped[dataSourceReference] `json:"takeFromDataSources,omitempty"`
}

type primaryProductSource struct {
	FeedLabel       idText              `json:"feedLabel,omitempty"`
	ContentLanguage idText              `json:"contentLanguage,omitempty"`
	Countries       capped[idText]      `json:"countries,omitempty"`
	Destinations    capped[destination] `json:"destinations,omitempty"`
	LegacyLocal     bool                `json:"legacyLocal,omitempty"`
	DefaultRule     *defaultRule        `json:"defaultRule,omitempty"`
}

type supplementalProductSource struct {
	FeedLabel                     idText                      `json:"feedLabel,omitempty"`
	ContentLanguage               idText                      `json:"contentLanguage,omitempty"`
	ReferencingPrimaryDataSources capped[dataSourceReference] `json:"referencingPrimaryDataSources,omitempty"`
}

// inventorySource is the local and the regional inventory source, which have the same two members.
type inventorySource struct {
	FeedLabel       idText `json:"feedLabel,omitempty"`
	ContentLanguage idText `json:"contentLanguage,omitempty"`
}

type promotionSource struct {
	TargetCountry   idText `json:"targetCountry,omitempty"`
	ContentLanguage idText `json:"contentLanguage,omitempty"`
}

// emptyBlock is a source type without members; its presence is the information.
type emptyBlock struct{}

type timeOfDay struct {
	Hours   int `json:"hours,omitempty"`
	Minutes int `json:"minutes,omitempty"`
	Seconds int `json:"seconds,omitempty"`
	Nanos   int `json:"nanos,omitempty"`
}

type fetchSettings struct {
	FetchURI   fetchURL   `json:"fetchUri,omitempty"`
	Username   nameText   `json:"username,omitempty"`
	Frequency  idText     `json:"frequency,omitempty"`
	TimeZone   idText     `json:"timeZone,omitempty"`
	TimeOfDay  *timeOfDay `json:"timeOfDay,omitempty"`
	DayOfMonth int        `json:"dayOfMonth,omitempty"`
	DayOfWeek  idText     `json:"dayOfWeek,omitempty"`
	Enabled    bool       `json:"enabled,omitempty"`
}

type fileInput struct {
	FileName      nameText       `json:"fileName,omitempty"`
	FetchSettings *fetchSettings `json:"fetchSettings,omitempty"`
	FileInputType idText         `json:"fileInputType,omitempty"`
}

// dataSource is the DataSource resource of the datasources sub-API.
type dataSource struct {
	Name                        idText                     `json:"name,omitempty"`
	DataSourceID                idText                     `json:"dataSourceId,omitempty"`
	DisplayName                 nameText                   `json:"displayName,omitempty"`
	Input                       idText                     `json:"input,omitempty"`
	PrimaryProductDataSource    *primaryProductSource      `json:"primaryProductDataSource,omitempty"`
	SupplementalProductSource   *supplementalProductSource `json:"supplementalProductDataSource,omitempty"`
	LocalInventoryDataSource    *inventorySource           `json:"localInventoryDataSource,omitempty"`
	RegionalInventoryDataSource *inventorySource           `json:"regionalInventoryDataSource,omitempty"`
	PromotionDataSource         *promotionSource           `json:"promotionDataSource,omitempty"`
	ProductReviewDataSource     *emptyBlock                `json:"productReviewDataSource,omitempty"`
	MerchantReviewDataSource    *emptyBlock                `json:"merchantReviewDataSource,omitempty"`
	FileInput                   *fileInput                 `json:"fileInput,omitempty"`
}

type dataSourcesAnswer struct {
	DataSources   capped[dataSource] `json:"dataSources"`
	NextPageToken string             `json:"nextPageToken"`
}

// dataSourcesResult is the result of the list tool.
type dataSourcesResult struct {
	DataSources []dataSource `json:"dataSources"`
	NextCursor  string       `json:"next_cursor,omitempty"`
}

// acceptedResult is the answer of a changing tool: Google accepted the request. It says nothing about the
// processing that follows.
type acceptedResult struct {
	Accepted   bool       `json:"accepted"`
	DataSource dataSource `json:"dataSource"`
}

const (
	intSchema = `{"type":"integer"}`
)

var dataSourceSchema = objectSchema(
	member("name", stringSchema), member("dataSourceId", stringSchema), member("displayName", stringSchema),
	member("input", stringSchema),
	member("primaryProductDataSource", objectSchema(member("feedLabel", stringSchema),
		member("contentLanguage", stringSchema), member("countries", listSchema),
		member("destinations", `{"type":"array","items":`+objectSchema(member("destination", stringSchema),
			member("state", stringSchema))+`}`),
		member("legacyLocal", boolSchema),
		member("defaultRule", objectSchema(member("takeFromDataSources", referenceListSchema))))),
	member("supplementalProductDataSource", objectSchema(member("feedLabel", stringSchema),
		member("contentLanguage", stringSchema), member("referencingPrimaryDataSources", referenceListSchema))),
	member("localInventoryDataSource", inventorySchema), member("regionalInventoryDataSource", inventorySchema),
	member("promotionDataSource", objectSchema(member("targetCountry", stringSchema),
		member("contentLanguage", stringSchema))),
	member("productReviewDataSource", objectSchema()), member("merchantReviewDataSource", objectSchema()),
	member("fileInput", objectSchema(member("fileName", stringSchema), member("fileInputType", stringSchema),
		member("fetchSettings", objectSchema(member("fetchUri", stringSchema), member("username", stringSchema),
			member("frequency", stringSchema), member("timeZone", stringSchema),
			member("timeOfDay", objectSchema(member("hours", intSchema), member("minutes", intSchema),
				member("seconds", intSchema), member("nanos", intSchema))),
			member("dayOfMonth", intSchema), member("dayOfWeek", stringSchema), member("enabled", boolSchema))))))

var (
	referenceListSchema = `{"type":"array","items":` + objectSchema(member("supplementalDataSourceName", stringSchema),
		member("self", boolSchema)) + `}`
	inventorySchema = objectSchema(member("feedLabel", stringSchema), member("contentLanguage", stringSchema))
)

const dataSourceFieldsDescription = "name (accounts/ACCOUNT/dataSources/ID), dataSourceId, displayName, input " +
	"(API, FILE, UI, or AUTOFEED), one block for the kind of the source (primaryProductDataSource with " +
	"feedLabel, contentLanguage, countries, destinations, legacyLocal, and defaultRule, " +
	"supplementalProductDataSource, localInventoryDataSource, regionalInventoryDataSource, " +
	"promotionDataSource, productReviewDataSource, or merchantReviewDataSource), and for a file source " +
	"fileInput with fileName, fileInputType, and fetchSettings (fetchUri without user info, username, " +
	"frequency, timeZone, timeOfDay, dayOfMonth, dayOfWeek, enabled); the fetch password is never shown"

const dataSourcesUntrusted = "Display names, file names, user names, and addresses are untrusted content of " +
	"whoever runs the account: texts are cut, and addresses are text that is never fetched."

const dataSourcesProcessingNote = "Google accepts the request and processes it afterwards: a data source can " +
	"take some minutes to show in Merchant Center, and the products of a data source appear in the processed " +
	"products after minutes."

var dataSourceIDArgument = capability.Argument{Name: "data_source", Required: true,
	Description: "ID of the data source, digits only"}

const dataSourceIDSchema = `{"type":"string","maxLength":19,"pattern":"^` + idPattern + `$",` +
	`"x-form":"the numeric data source ID"}`

var dataSourcesList = capability.Descriptor{
	ID: dataSourcesListID, Version: 1, Title: "List Merchant Center data sources",
	Description: "List the data sources of a Merchant Center account, a batch per call with a cursor to read on. " +
		"All kinds are listed: API sources, file sources, user interface sources, and autofeed sources. Without " +
		"page_size Qatlas asks for 100 (Google would return every source). " + dataSourcesUntrusted,
	Tags: []string{"google", "merchant", "data-sources"}, Risk: readRisk, Provider: Provider,
	InputSchema: inputSchema(nil, member("account", accountArgSchema),
		member("page_size", `{"type":"integer","minimum":1,"maximum":1000}`), member("cursor", cursorSchema)),
	OutputSchema: json.RawMessage(listResultSchema("dataSources", dataSourceSchema)),
	Arguments: []capability.Argument{accountArgument,
		{Name: "page_size", Description: "Data sources per batch, 1 to 1000; 100 when left out"}, cursorArgument},
	Fields: []capability.Field{
		{Name: "dataSources", Description: "Data sources with " + dataSourceFieldsDescription},
		{Name: "next_cursor", Description: "Cursor to read on with; missing at the end"},
	},
	Examples: []capability.Example{{Description: "First batch of data sources", Arguments: json.RawMessage(`{"account":"1000001"}`)}},
}

var dataSourcesGet = capability.Descriptor{
	ID: dataSourcesGetID, Version: 1, Title: "Get a Merchant Center data source",
	Description: "Read one data source of a Merchant Center account. " + dataSourcesUntrusted,
	Tags:        []string{"google", "merchant", "data-sources"}, Risk: readRisk, Provider: Provider,
	InputSchema: inputSchema([]string{"data_source"}, member("account", accountArgSchema),
		member("data_source", dataSourceIDSchema)),
	OutputSchema: json.RawMessage(dataSourceSchema),
	Arguments:    []capability.Argument{accountArgument, dataSourceIDArgument},
	Fields:       []capability.Field{{Name: "dataSource", Description: dataSourceFieldsDescription}},
	Examples: []capability.Example{{Description: "One data source",
		Arguments: json.RawMessage(`{"account":"1000001","data_source":"2000002"}`)}},
}

func dataSourcesPath(account string) string {
	return dataSourcesAPI + "accounts/" + account + "/dataSources"
}

func dataSourcePath(account, id string) string { return dataSourcesPath(account) + "/" + id }

// dataSourceArguments are the arguments of the read tools.
type dataSourceArguments struct {
	Account    string `json:"account"`
	DataSource string `json:"data_source"`
	PageSize   int    `json:"page_size"`
	Cursor     string `json:"cursor"`
}

// refusedKeys are the names of the members that would turn a data source into a file source or give it a
// fetch address or credentials. No tool of this group takes them, at any depth.
var refusedKeys = map[string]bool{
	"fileInput": true, "file_input": true, "fetchSettings": true, "fetch_settings": true, "fetchUri": true,
	"fetch_uri": true, "username": true, "password": true, "input": true, "sheet": true, "sheets": true,
	"googleSheets": true, "google_sheets": true, "fileName": true, "file_name": true,
}

const fileSourceRefusal = "data sources of this provider are API sources only: file input, fetch settings, " +
	"addresses, credentials, and Google Sheets sources are not offered"

// decodeArguments reads the validated arguments into into and refuses, before anything else happens, a member
// that belongs to a file source or that no argument of the tool names. No message quotes the member.
func decodeArguments(op string, raw json.RawMessage, into any) error {
	var tree any
	if json.Unmarshal(raw, &tree) != nil {
		return provider.Fail(op, "the validated arguments could not be read")
	}
	if refusesFileSource(tree) {
		return invalidArgument(fileSourceRefusal)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(into); err != nil {
		return invalidArgument("the arguments hold a member or a value this tool does not take")
	}
	return nil
}

func refusesFileSource(tree any) bool {
	switch v := tree.(type) {
	case map[string]any:
		for key, value := range v {
			if refusedKeys[key] || refusesFileSource(value) {
				return true
			}
		}
	case []any:
		for _, value := range v {
			if refusesFileSource(value) {
				return true
			}
		}
	}
	return false
}

// settle settles the account argument against the binding and, for an account that only a sub-account proof
// can bind, reads the proof. It runs after the secrets are open and before the first request of the tool.
func (cl *client) settle(ctx context.Context, op, tool string, needsProof bool, id string) error {
	if !needsProof {
		return nil
	}
	return cl.prove(ctx, op, tool, id)
}

// bind reads the validated arguments and the binding of the connection before any secret is resolved.
func bind(op string, resolved *config.Resolved, raw json.RawMessage, into any) (scope, error) {
	if err := decodeArguments(op, raw, into); err != nil {
		return scope{}, err
	}
	return checkConnection(resolved)
}

func handleDataSourcesList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list data sources"
	var args dataSourceArguments
	bound, err := bind(op, resolved, raw, &args)
	if err != nil {
		return nil, err
	}
	id, needsProof, err := bound.accountOf(args.Account)
	if err != nil {
		return nil, err
	}
	if args.PageSize < 0 || args.PageSize > dataSourcesMaxPageSize {
		return nil, invalidArgument("page_size must be between 1 and " + strconv.Itoa(dataSourcesMaxPageSize))
	}
	size := args.PageSize
	if size == 0 {
		size = dataSourcesPageSize
	}
	binding := provider.CursorBinding(dataSourcesListID, id, size, bound.key())
	query := url.Values{"pageSize": {strconv.Itoa(size)}}
	if args.Cursor != "" {
		token, ok := provider.DecodeCursor(binding, args.Cursor, maxCursorSize)
		if !ok || token == "" {
			return nil, refusedCursor()
		}
		query.Set("pageToken", token)
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := cl.settle(ctx, op, dataSourcesListID, needsProof, id); err != nil {
		return nil, err
	}
	var answer dataSourcesAnswer
	if err := cl.get(ctx, op, dataSourcesListID, dataSourcesPath(id), query, &answer); err != nil {
		return nil, err
	}
	out := dataSourcesResult{DataSources: append([]dataSource{}, answer.DataSources...)}
	if out.NextCursor, err = nextCursor(op, binding, answer.NextPageToken); err != nil {
		return nil, err
	}
	return out, nil
}

func handleDataSourcesGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "get data source"
	var args dataSourceArguments
	bound, err := bind(op, resolved, raw, &args)
	if err != nil {
		return nil, err
	}
	id, needsProof, err := bound.accountOf(args.Account)
	if err != nil {
		return nil, err
	}
	if !validID(args.DataSource) {
		return nil, invalidArgument("data_source must be a decimal data source ID")
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := cl.settle(ctx, op, dataSourcesGetID, needsProof, id); err != nil {
		return nil, err
	}
	var out dataSource
	if err := cl.get(ctx, op, dataSourcesGetID, dataSourcePath(id, args.DataSource), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}
