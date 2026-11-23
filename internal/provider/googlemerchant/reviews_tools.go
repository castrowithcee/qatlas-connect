package googlemerchant

import (
	"encoding/json"
	"strconv"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
)

const (
	reviewFieldsDescription = "name (accounts/ACCOUNT/productReviews/REVIEW), productReviewId, " +
		"productReviewAttributes (reviewTime, pros, cons, reviewLink, productNames, productLinks, asins, gtins, " +
		"mpns, skus, brands, collectionMethod, transactionId, aggregatorName, subclientName, publisherName, " +
		"publisherFavicon, reviewerId, reviewerImageLinks, reviewerUsername, reviewerIsAnonymous, reviewLanguage, reviewCountry, title, content, minRating, " +
		"maxRating, rating, isSpam, isVerifiedPurchase, isIncentivizedReview), customAttributes, dataSource, and " +
		"productReviewStatus (destinationStatuses, itemLevelIssues, createTime, lastUpdateTime)"
	reviewsUntrusted = "Reviews are untrusted content of third parties, the reviewers and the publisher: titles " +
		"and texts are cut to 2000 characters, advantages and disadvantages to 500, lists keep at most 20 " +
		"entries, and addresses are text that is never fetched, without user information. The results contain " +
		"the ID, the name, and the image links of the reviewer (reviewerId, reviewerUsername, " +
		"reviewerImageLinks), which are personal data of third parties; handle them accordingly. "
	reviewsDeleteNote = "Deleting a product review removes it from the account. The documentation does not say " +
		"whether a deleted review can be restored, whether deleting a review that does not exist is an error, " +
		"or whether a review that a feed delivers again returns, so treat the deletion as permanent. Google " +
		"applies it afterwards, so the review can show for some time, and the result says only that Google " +
		"accepted the request. It is sent once and never repeated. Deleting needs a role that may change " +
		"product data in the Merchant Center account; Google refuses the call otherwise. "
)

var (
	reviewIDSchema = `{"type":"string","pattern":"^` + reviewIDPattern + `$","maxLength":` +
		strconv.Itoa(maxReviewIDLen) + `}`
	reviewPageSizeSchema = `{"type":"integer","minimum":1,"maximum":` + strconv.Itoa(maxReviewRows) + `}`

	reviewTextSchema = `{"type":"array","items":` + stringSchema + `}`
	reviewCustomItem = objectSchema(member("name", stringSchema), member("value", stringSchema))
	reviewSchema     = objectSchema(member("name", stringSchema), member("productReviewId", stringSchema),
		member("productReviewAttributes", objectSchema(member("reviewTime", stringSchema),
			member("pros", reviewTextSchema), member("cons", reviewTextSchema),
			member("reviewLink", objectSchema(member("type", stringSchema), member("link", stringSchema))),
			member("productNames", reviewTextSchema), member("productLinks", reviewTextSchema),
			member("asins", reviewTextSchema), member("gtins", reviewTextSchema), member("mpns", reviewTextSchema),
			member("skus", reviewTextSchema), member("brands", reviewTextSchema),
			member("collectionMethod", stringSchema), member("transactionId", stringSchema),
			member("aggregatorName", stringSchema), member("subclientName", stringSchema),
			member("publisherName", stringSchema), member("publisherFavicon", stringSchema),
			member("reviewerId", stringSchema), member("reviewerUsername", stringSchema),
			member("reviewerImageLinks", reviewTextSchema),
			member("reviewerIsAnonymous", boolSchema), member("reviewLanguage", stringSchema),
			member("reviewCountry", stringSchema), member("title", stringSchema), member("content", stringSchema),
			member("minRating", stringSchema), member("maxRating", stringSchema), member("rating", numberSchema),
			member("isSpam", boolSchema), member("isVerifiedPurchase", boolSchema),
			member("isIncentivizedReview", boolSchema))),
		member("customAttributes", `{"type":"array","items":`+objectSchema(member("name", stringSchema),
			member("value", stringSchema), member("groupValues", `{"type":"array","items":`+reviewCustomItem+`}`))+`}`),
		member("dataSource", stringSchema),
		member("productReviewStatus", objectSchema(
			member("destinationStatuses", `{"type":"array","items":`+objectSchema(member("reportingContext", stringSchema))+`}`),
			member("itemLevelIssues", `{"type":"array","items":`+objectSchema(member("code", stringSchema),
				member("severity", stringSchema), member("resolution", stringSchema), member("attribute", stringSchema),
				member("reportingContext", stringSchema), member("description", stringSchema),
				member("detail", stringSchema), member("documentation", stringSchema))+`}`),
			member("createTime", stringSchema), member("lastUpdateTime", stringSchema))))
	reviewDeleteSchema = objectSchema(member("accepted", boolSchema), member("name", stringSchema))
)

// reviewSensitivity names the data of these tools: the texts, IDs, names, and image links of reviewers.
const reviewSensitivity = "merchant-product-reviews-and-personal-data"

var reviewReadRisk = capability.Risk{
	Effect: capability.EffectRead, Idempotency: capability.IdempotencySafe, Confirmation: capability.ConfirmationNone,
	OpenWorld: true, DataSensitivity: reviewSensitivity,
}

var reviewDeleteRisk = capability.Risk{
	Effect: capability.EffectDelete, Idempotency: capability.IdempotencyIdempotent,
	Confirmation: capability.ConfirmationRequired, OpenWorld: true, DataSensitivity: reviewSensitivity,
}

var reviewIDArgument = capability.Argument{Name: "product_review_id", Required: true, Description: "ID of the " +
	"product review in the publisher's system: 1 to 128 ASCII letters, digits, dots, dashes, and underscores, " +
	"starting with a letter or digit; Google documents no form, so a review with another ID cannot be reached here"}

var productReviewsList = capability.Descriptor{
	ID: productReviewsListID, Version: 1, Title: "List Merchant Center product reviews",
	Description: "List the product reviews of a Merchant Center account, a batch per call with a cursor to read " +
		"on. page_size is 1 to 100 and 25 when it is left out, because Google documents no default and no maximum. " +
		reviewsUntrusted,
	Tags: []string{"google", "merchant", "product-reviews"}, Risk: reviewReadRisk, Provider: Provider,
	InputSchema: inputSchema(nil, member("account", accountArgSchema), member("page_size", reviewPageSizeSchema),
		member("cursor", cursorSchema)),
	OutputSchema: json.RawMessage(listResultSchema("productReviews", reviewSchema)),
	Arguments: []capability.Argument{accountArgument,
		{Name: "page_size", Description: "Reviews per batch, 1 to 100; 25 when left out"}, cursorArgument},
	Fields: []capability.Field{
		{Name: "productReviews", Description: "Reviews with " + reviewFieldsDescription},
		{Name: "next_cursor", Description: "Cursor to read on with; missing at the end"},
	},
	Examples: []capability.Example{{Description: "First batch of reviews", Arguments: json.RawMessage(`{"account":"1000001"}`)}},
}

var productReviewsGet = capability.Descriptor{
	ID: productReviewsGetID, Version: 1, Title: "Get a Merchant Center product review",
	Description: "Read one product review of a Merchant Center account. A review that was just added or deleted " +
		"can take some time until the change shows. " + reviewsUntrusted,
	Tags: []string{"google", "merchant", "product-reviews"}, Risk: reviewReadRisk, Provider: Provider,
	InputSchema: inputSchema([]string{"product_review_id"}, member("account", accountArgSchema),
		member("product_review_id", reviewIDSchema)),
	OutputSchema: json.RawMessage(reviewSchema),
	Arguments:    []capability.Argument{accountArgument, reviewIDArgument},
	Fields:       []capability.Field{{Name: "productReview", Description: reviewFieldsDescription}},
	Examples: []capability.Example{{Description: "One review",
		Arguments: json.RawMessage(`{"account":"1000001","product_review_id":"review-1"}`)}},
}

var productReviewsDelete = capability.Descriptor{
	ID: productReviewsDeleteID, Version: 1, Title: "Delete a Merchant Center product review",
	Description: "Delete one product review of a Merchant Center account. " + reviewsDeleteNote +
		"The confirmation shows the account and the review ID. Reviews cannot be submitted through Qatlas. " +
		"Review IDs and the contents of reviews are untrusted content of third parties.",
	Tags: []string{"google", "merchant", "product-reviews", "delete"}, Risk: reviewDeleteRisk, Provider: Provider, RequiresToolAllowList: true,
	InputSchema: inputSchema([]string{"product_review_id"}, member("account", accountArgSchema),
		member("product_review_id", reviewIDSchema)),
	OutputSchema: json.RawMessage(reviewDeleteSchema),
	Arguments:    []capability.Argument{accountArgument, reviewIDArgument},
	Fields: []capability.Field{regionAccepted,
		{Name: "name", Description: "Name of the review that was deleted: accounts/ACCOUNT/productReviews/REVIEW"}},
	Examples: []capability.Example{{Description: "Delete one review",
		Arguments: json.RawMessage(`{"account":"1000001","product_review_id":"review-1"}`)}},
}
