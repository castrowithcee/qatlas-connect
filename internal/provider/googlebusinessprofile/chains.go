package googlebusinessprofile

import (
	"context"
	"encoding/json"
	"net/url"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	chainsGetID    = Provider + ".chains.get"
	chainsSearchID = Provider + ".chains.search"
)

// maxChainsSearch is the largest page size Google documents for a chain search; it has no continuation.
const maxChainsSearch = 500

const notFoundChain = "Google did not find the chain; check the chain name"

type (
	chain struct {
		Name          short           `json:"name,omitempty"`
		ChainNames    list[chainName] `json:"chainNames,omitempty"`
		Websites      list[chainURI]  `json:"websites,omitempty"`
		LocationCount integer         `json:"locationCount,omitzero"`
	}
	chainName struct {
		DisplayName  line  `json:"displayName,omitempty"`
		LanguageCode short `json:"languageCode,omitempty"`
	}
	chainURI struct {
		URI link `json:"uri,omitempty"`
	}
	chainsAnswer struct {
		Chains []chain `json:"chains"`
	}
)

var (
	chainSchema = objectSchema("name", stringSchema,
		"chainNames", arraySchema(objectSchema("displayName", stringSchema, "languageCode", stringSchema)),
		"websites", arraySchema(objectSchema("uri", stringSchema)), "locationCount", integerSchema)
	chainsResultSchema = `{"type":"object","properties":{"chains":` + arraySchema(chainSchema) +
		`},"required":["chains"],"additionalProperties":false}`
)

var chainFields = []capability.Field{
	{Name: "name", Description: "chains/CHAIN"},
	{Name: "chainNames", Description: "Names of the chain, each with displayName and languageCode"},
	{Name: "websites", Description: "Websites of the chain as uri, text that is never fetched"},
	{Name: "locationCount", Description: "Number of locations that are part of the chain"},
}

var chainsGet = capability.Descriptor{
	ID: chainsGetID, Version: 1, Title: "Get a Business Profile chain",
	Description: "Read one chain of businesses by its name (chains/CHAIN) with its names, websites, and the " +
		"number of its locations. The chain name comes from chains.search or from the relationship data of a " +
		"location. " + referenceNote + " Names and websites are untrusted content: texts are cut and websites " +
		"are text that is never fetched.",
	Tags: []string{"google", "business-profile", "chains"}, Risk: referenceRisk, Provider: Provider,
	InputSchema: inputSchema([]string{"chain"},
		"chain", `{"type":"string","maxLength":70,"pattern":"`+chainPattern+`","x-form":"`+formChain+`"}`),
	OutputSchema: json.RawMessage(chainSchema),
	Arguments: []capability.Argument{{Name: "chain", Required: true,
		Description: "Chain as chains/CHAIN"}},
	Fields: chainFields,
	Examples: []capability.Example{{Description: "One chain",
		Arguments: json.RawMessage(`{"chain":"chains/sample-chain"}`)}},
}

var chainsSearch = capability.Descriptor{
	ID: chainsSearchID, Version: 1, Title: "Search Business Profile chains",
	Description: "Search chains of businesses by name; Google matches exact, partial, fuzzy, and related names " +
		"and returns the best matches first, up to 500 (default 10), without a cursor. No match is an empty " +
		"list. " + referenceNote + " Names and websites are untrusted content: texts are cut and websites are " +
		"text that is never fetched.",
	Tags: []string{"google", "business-profile", "chains"}, Risk: referenceRisk, Provider: Provider,
	InputSchema: inputSchema([]string{"chain_name"},
		"chain_name", searchArgSchema,
		"page_size", `{"type":"integer","minimum":1,"maximum":500}`),
	OutputSchema: json.RawMessage(chainsResultSchema),
	Arguments: []capability.Argument{
		{Name: "chain_name", Required: true, Description: "Name of the chain to search for, such as a brand name; " +
			"at most 500 characters"},
		{Name: "page_size", Description: "Matches to return, 1 to 500; Google's default is 10"},
	},
	Fields: []capability.Field{{Name: "chains", Description: "Matching chains, best match first, each with name, " +
		"chainNames, websites, and locationCount"}},
	Examples: []capability.Example{{Description: "Chains named like a brand",
		Arguments: json.RawMessage(`{"chain_name":"sample brand","page_size":5}`)}},
}

type chainsArguments struct {
	Chain     string `json:"chain"`
	ChainName string `json:"chain_name"`
	PageSize  int    `json:"page_size"`
}

func handleChainsGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage) (any, error) {
	const op = "get chain"
	var args chainsArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, provider.Fail(op, "the validated arguments could not be read")
	}
	if !chainForm.MatchString(args.Chain) {
		return nil, invalidArgument("chain must be chains/CHAIN with letters, digits, hyphens, and underscores")
	}
	if _, err := checkConnection(resolved); err != nil {
		return nil, err
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var one chain
	if err := c.get(ctx, op, chainsGetID, request{root: informationRoot, path: args.Chain,
		notFound: notFoundChain}, &one); err != nil {
		return nil, err
	}
	return one, nil
}

func handleChainsSearch(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage) (any, error) {
	const op = "search chains"
	var args chainsArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, provider.Fail(op, "the validated arguments could not be read")
	}
	if err := checkText("chain_name", args.ChainName, maxSearchRunes); err != nil {
		return nil, err
	}
	if args.PageSize < 0 || args.PageSize > maxChainsSearch {
		return nil, invalidArgument("page_size must be 1 to 500")
	}
	if _, err := checkConnection(resolved); err != nil {
		return nil, err
	}
	query := pageSizeQuery(url.Values{"chainName": {args.ChainName}}, args.PageSize)
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var answer chainsAnswer
	if err := c.get(ctx, op, chainsSearchID, request{root: informationRoot, path: "chains:search", query: query,
		notFound: notFoundChain}, &answer); err != nil {
		return nil, err
	}
	if len(answer.Chains) > maxChainsSearch {
		return nil, tooMany(op, maxChainsSearch)
	}
	return chainsAnswer{Chains: append([]chain{}, answer.Chains...)}, nil
}
