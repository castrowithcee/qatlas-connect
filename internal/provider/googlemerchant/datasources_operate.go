package googlemerchant

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	dataSourcesFetchID  = Provider + ".data-sources.fetch"
	dataSourcesDeleteID = Provider + ".data-sources.delete"
	fileUploadsGetID    = Provider + ".file-uploads.get"
)

// fileUploadAlias is the only file upload name Google accepts.
const fileUploadAlias = "latest"

// fileUploadIssue is the Issue of a file upload. Its texts come from Google and may quote the uploaded file,
// so they are untrusted.
type fileUploadIssue struct {
	Title            nameText `json:"title,omitempty"`
	Description      bodyText `json:"description,omitempty"`
	Code             idText   `json:"code,omitempty"`
	Count            idText   `json:"count,omitempty"`
	Severity         idText   `json:"severity,omitempty"`
	DocumentationURI urlText  `json:"documentationUri,omitempty"`
}

// fileUpload is the FileUpload resource. The counts are int64 values, which Google writes as strings.
type fileUpload struct {
	Name            idText                  `json:"name,omitempty"`
	DataSourceID    idText                  `json:"dataSourceId,omitempty"`
	ProcessingState idText                  `json:"processingState,omitempty"`
	Issues          capped[fileUploadIssue] `json:"issues,omitempty"`
	ItemsTotal      idText                  `json:"itemsTotal,omitempty"`
	ItemsCreated    idText                  `json:"itemsCreated,omitempty"`
	ItemsUpdated    idText                  `json:"itemsUpdated,omitempty"`
	UploadTime      idText                  `json:"uploadTime,omitempty"`
}

// dataSourceOperated is the answer of fetch and delete: Google answers with an empty object, so the result
// holds only what the request named.
type dataSourceOperated struct {
	Accepted bool   `json:"accepted"`
	Name     string `json:"name"`
}

var (
	fileUploadSchema = objectSchema(member("name", stringSchema), member("dataSourceId", stringSchema),
		member("processingState", stringSchema),
		member("issues", `{"type":"array","items":`+objectSchema(member("title", stringSchema),
			member("description", stringSchema), member("code", stringSchema), member("count", stringSchema),
			member("severity", stringSchema), member("documentationUri", stringSchema))+`}`),
		member("itemsTotal", stringSchema), member("itemsCreated", stringSchema),
		member("itemsUpdated", stringSchema), member("uploadTime", stringSchema))
	dataSourceOperatedSchema = objectSchema(member("accepted", boolSchema), member("name", stringSchema))
)

const fileUploadFieldsDescription = "name (accounts/ACCOUNT/dataSources/ID/fileUploads/latest), dataSourceId, " +
	"processingState (IN_PROGRESS, SUCCEEDED, or FAILED), itemsTotal, itemsCreated, itemsUpdated (counts as " +
	"text), uploadTime, and issues with title, description, code, count, severity (WARNING or ERROR), and " +
	"documentationUri"

const fileUploadUntrusted = "Issue titles and descriptions are text from Google that can quote the uploaded " +
	"file, so they are untrusted content: texts are cut, and addresses are text that is never fetched."

var dataSourcesFetch = capability.Descriptor{
	ID: dataSourcesFetchID, Version: 1, Title: "Fetch a Merchant Center data source now",
	Description: "Ask Merchant Center to fetch the file of a data source now, outside its fetch schedule. Google " +
		"documents this only for data sources with a file input, and any other source is answered with an error. " +
		"Qatlas neither sets nor reads the fetch address: Merchant Center calls the address, with the credentials, " +
		"that someone configured in Merchant Center, and the data of that file can change the products and offers " +
		"that Google Shopping shows. The request has no body and sets nothing. Google documents that a fetch more " +
		"than once a day is better replaced by sending product data through the Products service, and states no " +
		"quota for the method: every call is a call against the Merchant API quota and is never repeated. Google " +
		"only accepts the request and processes the file afterwards, so the answer says nothing about the result: " +
		"read it later with file-uploads.get. When the outcome is unknown, read the file upload before asking " +
		"again. The address and the file are untrusted content of whoever runs the account and are never fetched or " +
		"read by Qatlas. Needs confirmation.",
	Tags: []string{"google", "merchant", "data-sources"}, Provider: Provider,
	Risk:         changeRisk(capability.EffectExecute, capability.IdempotencyNonIdempotent),
	InputSchema:  inputSchema([]string{"data_source"}, member("account", accountArgSchema), member("data_source", dataSourceIDSchema)),
	OutputSchema: json.RawMessage(dataSourceOperatedSchema),
	Arguments:    []capability.Argument{accountArgument, dataSourceIDArgument},
	Fields: []capability.Field{
		{Name: "accepted", Description: "true when Google accepted the request; nothing is said about processing"},
		{Name: "name", Description: "the name of the data source of the request"}},
	Examples: []capability.Example{{Description: "Fetch one file data source now",
		Arguments: json.RawMessage(`{"account":"1000001","data_source":"2000002"}`)}},
}

var dataSourcesDelete = capability.Descriptor{
	ID: dataSourcesDeleteID, Version: 1, Title: "Delete a Merchant Center data source",
	Description: "Delete one data source of a Merchant Center account, of any kind. Google documents no effect on " +
		"the products the data source delivered, no kind that cannot be deleted, and no role that is needed, so " +
		"products that came from it may stop being updated or disappear. Qatlas offers no way to undo it, and a " +
		"data source that was created from a file or a Google Sheet cannot be recreated with these tools. " +
		dataSourcesProcessingNote + " The request is sent once and never repeated: when the outcome is unknown, " +
		"read the data source before asking again. The result holds no text of the data source, whose names and " +
		"addresses are untrusted content. Needs confirmation, and is offered only by a connection whose tools list names it.",
	Tags: []string{"google", "merchant", "data-sources", "delete"}, Provider: Provider, RequiresToolAllowList: true,
	Risk:         changeRisk(capability.EffectDelete, capability.IdempotencyIdempotent),
	InputSchema:  inputSchema([]string{"data_source"}, member("account", accountArgSchema), member("data_source", dataSourceIDSchema)),
	OutputSchema: json.RawMessage(dataSourceOperatedSchema),
	Arguments:    []capability.Argument{accountArgument, dataSourceIDArgument},
	Fields: []capability.Field{
		{Name: "accepted", Description: "true when Google accepted the request; nothing is said about processing"},
		{Name: "name", Description: "the name of the data source of the request"}},
	Examples: []capability.Example{{Description: "Delete one data source",
		Arguments: json.RawMessage(`{"account":"1000001","data_source":"2000002"}`)}},
}

var fileUploadsGet = capability.Descriptor{
	ID: fileUploadsGetID, Version: 1, Title: "Get the latest file upload of a Merchant Center data source",
	Description: "Read the latest file upload of a data source of a Merchant Center account: its processing state, " +
		"the number of items processed, created, and updated, the upload time, and the issues Google found. " +
		"Google accepts only the alias latest, so no older upload can be read, and documents the call only for " +
		"data sources with a file input. " + fileUploadUntrusted,
	Tags: []string{"google", "merchant", "data-sources"}, Risk: readRisk, Provider: Provider,
	InputSchema:  inputSchema([]string{"data_source"}, member("account", accountArgSchema), member("data_source", dataSourceIDSchema)),
	OutputSchema: json.RawMessage(fileUploadSchema),
	Arguments:    []capability.Argument{accountArgument, dataSourceIDArgument},
	Fields:       []capability.Field{{Name: "fileUpload", Description: fileUploadFieldsDescription}},
	Examples: []capability.Example{{Description: "The latest upload of a file data source",
		Arguments: json.RawMessage(`{"account":"1000001","data_source":"2000002"}`)}},
}

func dataSourceOperationDescriptors() []capability.Descriptor {
	return []capability.Descriptor{dataSourcesFetch, dataSourcesDelete, fileUploadsGet}
}

func dataSourceOperationHandlers() map[string]capability.Handler {
	return map[string]capability.Handler{dataSourcesFetchID: handleDataSourcesFetch,
		dataSourcesDeleteID: handleDataSourcesDelete, fileUploadsGetID: handleFileUploadsGet}
}

func dataSourceOperationSummaries() map[string]capability.SummaryFunc {
	return map[string]capability.SummaryFunc{dataSourcesFetchID: fetchSummary, dataSourcesDeleteID: deleteSummary}
}

// operateArguments are the arguments of the three tools: no member beyond these is accepted.
type operateArguments struct {
	Account    string `json:"account"`
	DataSource string `json:"data_source"`
}

// dataSourceTarget checks the arguments and the account before a secret is read and, for a sub-account that
// only a proof can bind, reads the proof after the secrets are open.
func dataSourceTarget(op string, resolved *config.Resolved, raw json.RawMessage) (id string,
	needsProof bool, args operateArguments, err error) {
	bound, err := bind(op, resolved, raw, &args)
	if err != nil {
		return "", false, args, err
	}
	if id, needsProof, err = bound.accountOf(args.Account); err != nil {
		return "", false, args, err
	}
	if !validID(args.DataSource) {
		return "", false, args, invalidArgument("data_source must be a decimal data source ID")
	}
	return id, needsProof, args, nil
}

func handleDataSourcesFetch(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "fetch data source"
	id, needsProof, args, err := dataSourceTarget(op, resolved, raw)
	if err != nil {
		return nil, err
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := cl.settle(ctx, op, dataSourcesFetchID, needsProof, id); err != nil {
		return nil, err
	}
	var answer struct{}
	// The documented request body is empty; an empty object is the JSON form of it.
	if err := cl.send(ctx, op, dataSourcesFetchID, request{method: http.MethodPost,
		path: dataSourcePath(id, args.DataSource) + ":fetch", body: []byte("{}"), change: true}, &answer); err != nil {
		return nil, err
	}
	return dataSourceOperated{Accepted: true, Name: "accounts/" + id + "/dataSources/" + args.DataSource}, nil
}

func handleDataSourcesDelete(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "delete data source"
	id, needsProof, args, err := dataSourceTarget(op, resolved, raw)
	if err != nil {
		return nil, err
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := cl.settle(ctx, op, dataSourcesDeleteID, needsProof, id); err != nil {
		return nil, err
	}
	var answer struct{}
	if err := cl.send(ctx, op, dataSourcesDeleteID, request{method: http.MethodDelete,
		path: dataSourcePath(id, args.DataSource), change: true}, &answer); err != nil {
		return nil, err
	}
	return dataSourceOperated{Accepted: true, Name: "accounts/" + id + "/dataSources/" + args.DataSource}, nil
}

func handleFileUploadsGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "get file upload"
	id, needsProof, args, err := dataSourceTarget(op, resolved, raw)
	if err != nil {
		return nil, err
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := cl.settle(ctx, op, fileUploadsGetID, needsProof, id); err != nil {
		return nil, err
	}
	var out fileUpload
	if err := cl.get(ctx, op, fileUploadsGetID,
		dataSourcePath(id, args.DataSource)+"/fileUploads/"+fileUploadAlias, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// operatedText names the data source and the account of a summary from arguments the schema has checked.
func operatedText(raw json.RawMessage) (source, account string) {
	var a operateArguments
	if json.Unmarshal(raw, &a) != nil {
		return "", ""
	}
	source = a.DataSource
	if !validID(source) {
		source = "(invalid ID)"
	}
	account = "the account of the connection"
	if validID(a.Account) {
		account = "account " + a.Account
	} else if a.Account != "" {
		account = "account (invalid ID)"
	}
	return source, account
}

func fetchSummary(raw json.RawMessage) string {
	source, account := operatedText(raw)
	return "Fetch data source " + source + " of Merchant Center " + account + " now. Merchant Center " +
		"calls the address configured for the file, outside its schedule, and the data can change the products " +
		"it delivers; the call counts against the quota and is never repeated. Google processes it afterwards"
}

func deleteSummary(raw json.RawMessage) string {
	source, account := operatedText(raw)
	return "Delete data source " + source + " of Merchant Center " + account + ". The effect on its " +
		"products is not documented, and Qatlas offers no way to undo it. Google processes the deletion afterwards"
}
