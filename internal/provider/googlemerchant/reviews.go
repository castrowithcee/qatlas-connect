package googlemerchant

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strconv"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	productReviewsListID   = Provider + ".product-reviews.list"
	productReviewsGetID    = Provider + ".product-reviews.get"
	productReviewsDeleteID = Provider + ".product-reviews.delete"
)

// reviewsAPI is the path prefix of the reviews sub-API.
const reviewsAPI = "reviews/v1/"

// Bounds of the review tools. Google documents no default, no maximum page size, no length limit of a text
// member, and no form of a review ID, so every cap here is Qatlas' own.
const (
	defaultReviewRows = 25
	maxReviewRows     = 100
	maxReviewIDLen    = 128
	maxReviewTextLen  = 2000 // title and content
	maxReviewNoteLen  = 500  // one advantage or disadvantage
	maxReviewListLen  = 20   // entries of a list of the review
)

// A review ID is a path segment. Google documents no form for it (it is the ID in the publisher's system), so
// Qatlas accepts the narrow one that can never alter a path: ASCII letters, digits, ".", "_" and "-", starting
// with a letter or digit. A review with another ID cannot be addressed through these tools.
const reviewIDPattern = `[A-Za-z0-9][A-Za-z0-9._-]{0,127}`

var reviewIDForm = regexp.MustCompile(`^` + reviewIDPattern + `$`)

func validReviewID(raw string) bool { return reviewIDForm.MatchString(raw) }

// What Google returns for a review is untrusted content of third parties: reviewers and publishers. Text types
// cut themselves, lists keep a bounded number of entries, and a member these types do not list is never held.
// The reviewer's ID, name, and image links identify a person: they are shown as text, cut like other texts, and
// the descriptions and the documentation say that they are personal data of third parties.
type (
	reviewText string // title and content
	noteText   string // one advantage or disadvantage

	// reviewAddress is an address of a review. It is text that is never fetched, and a user name or password
	// inside it is removed.
	reviewAddress string
	// addressList is a list of such addresses.
	addressList []string

	// reviewList is a list of names, codes, and addresses of the review.
	reviewList []string
	// noteList is the list of advantages or disadvantages.
	noteList []string
)

func (s *reviewText) UnmarshalJSON(b []byte) error {
	*s = reviewText(clipJSON(b, maxReviewTextLen))
	return nil
}

func (s *noteText) UnmarshalJSON(b []byte) error {
	*s = noteText(clipJSON(b, maxReviewNoteLen))
	return nil
}

// cleanAddress cuts an address and removes user information from it.
func cleanAddress(b []byte) string {
	text := clipJSON(b, maxURLRunes)
	if parsed, err := url.Parse(text); err == nil && parsed.User != nil {
		parsed.User = nil
		text = parsed.String()
	}
	return text
}

func (s *reviewAddress) UnmarshalJSON(b []byte) error {
	*s = reviewAddress(cleanAddress(b))
	return nil
}

// UnmarshalJSON keeps at most maxReviewListLen addresses.
func (l *addressList) UnmarshalJSON(b []byte) error {
	var raw []json.RawMessage
	if json.Unmarshal(b, &raw) != nil {
		*l = nil
		return nil
	}
	var out []string
	for _, item := range raw {
		if len(out) == maxReviewListLen {
			break
		}
		if text := cleanAddress(item); text != "" {
			out = append(out, text)
		}
	}
	*l = out
	return nil
}

func decodeStrings(b []byte, limit int) []string {
	var raw []json.RawMessage
	if json.Unmarshal(b, &raw) != nil {
		return nil
	}
	var out []string
	for _, item := range raw {
		if len(out) == maxReviewListLen {
			break
		}
		if text := clipJSON(item, limit); text != "" {
			out = append(out, text)
		}
	}
	return out
}

// UnmarshalJSON keeps at most maxReviewListLen strings, each cut to the length of a name.
func (l *reviewList) UnmarshalJSON(b []byte) error {
	*l = decodeStrings(b, maxNameRunes)
	return nil
}

// UnmarshalJSON keeps at most maxReviewListLen strings, each cut to the length of a note.
func (l *noteList) UnmarshalJSON(b []byte) error {
	*l = decodeStrings(b, maxReviewNoteLen)
	return nil
}

type (
	reviewLink struct {
		Type idText        `json:"type,omitempty"`
		Link reviewAddress `json:"link,omitempty"`
	}

	// reviewAttributes holds the members of ProductReviewAttributes. A member is added or derived by no one.
	reviewAttributes struct {
		ReviewTime          idText        `json:"reviewTime,omitempty"`
		Pros                noteList      `json:"pros,omitempty"`
		Cons                noteList      `json:"cons,omitempty"`
		ReviewLink          *reviewLink   `json:"reviewLink,omitempty"`
		ProductNames        reviewList    `json:"productNames,omitempty"`
		ProductLinks        addressList   `json:"productLinks,omitempty"`
		Asins               reviewList    `json:"asins,omitempty"`
		Gtins               reviewList    `json:"gtins,omitempty"`
		Mpns                reviewList    `json:"mpns,omitempty"`
		Skus                reviewList    `json:"skus,omitempty"`
		Brands              reviewList    `json:"brands,omitempty"`
		CollectionMethod    idText        `json:"collectionMethod,omitempty"`
		TransactionID       idText        `json:"transactionId,omitempty"`
		AggregatorName      nameText      `json:"aggregatorName,omitempty"`
		SubclientName       nameText      `json:"subclientName,omitempty"`
		PublisherName       nameText      `json:"publisherName,omitempty"`
		PublisherFavicon    reviewAddress `json:"publisherFavicon,omitempty"`
		ReviewerID          idText        `json:"reviewerId,omitempty"`
		ReviewerImageLinks  addressList   `json:"reviewerImageLinks,omitempty"`
		ReviewerUsername    nameText      `json:"reviewerUsername,omitempty"`
		ReviewerIsAnonymous bool          `json:"reviewerIsAnonymous,omitempty"`
		ReviewLanguage      idText        `json:"reviewLanguage,omitempty"`
		ReviewCountry       idText        `json:"reviewCountry,omitempty"`
		Title               reviewText    `json:"title,omitempty"`
		Content             reviewText    `json:"content,omitempty"`
		MinRating           idText        `json:"minRating,omitempty"`
		MaxRating           idText        `json:"maxRating,omitempty"`
		Rating              *regionNumber `json:"rating,omitempty"`
		IsSpam              bool          `json:"isSpam,omitempty"`
		IsVerifiedPurchase  bool          `json:"isVerifiedPurchase,omitempty"`
		IsIncentivized      bool          `json:"isIncentivizedReview,omitempty"`
	}

	reviewCustomValue struct {
		Name  idText   `json:"name,omitempty"`
		Value nameText `json:"value,omitempty"`
	}
	reviewCustomAttribute struct {
		Name        idText                    `json:"name,omitempty"`
		Value       nameText                  `json:"value,omitempty"`
		GroupValues capped[reviewCustomValue] `json:"groupValues,omitempty"`
	}

	reviewDestination struct {
		ReportingContext idText `json:"reportingContext,omitempty"`
	}
	reviewIssue struct {
		Code             idText        `json:"code,omitempty"`
		Severity         idText        `json:"severity,omitempty"`
		Resolution       idText        `json:"resolution,omitempty"`
		Attribute        idText        `json:"attribute,omitempty"`
		ReportingContext idText        `json:"reportingContext,omitempty"`
		Description      nameText      `json:"description,omitempty"`
		Detail           nameText      `json:"detail,omitempty"`
		Documentation    reviewAddress `json:"documentation,omitempty"`
	}
	reviewStatus struct {
		DestinationStatuses capped[reviewDestination] `json:"destinationStatuses,omitempty"`
		ItemLevelIssues     capped[reviewIssue]       `json:"itemLevelIssues,omitempty"`
		CreateTime          idText                    `json:"createTime,omitempty"`
		LastUpdateTime      idText                    `json:"lastUpdateTime,omitempty"`
	}

	// productReview is the ProductReview resource of the reviews sub-API.
	productReview struct {
		Name                    idText                        `json:"name,omitempty"`
		ProductReviewID         idText                        `json:"productReviewId,omitempty"`
		ProductReviewAttributes *reviewAttributes             `json:"productReviewAttributes,omitempty"`
		CustomAttributes        capped[reviewCustomAttribute] `json:"customAttributes,omitempty"`
		DataSource              idText                        `json:"dataSource,omitempty"`
		ProductReviewStatus     *reviewStatus                 `json:"productReviewStatus,omitempty"`
	}
)

type reviewsAnswer struct {
	ProductReviews []productReview `json:"productReviews"`
	NextPageToken  string          `json:"nextPageToken"`
}

type reviewsResult struct {
	ProductReviews []productReview `json:"productReviews"`
	NextCursor     string          `json:"next_cursor,omitempty"`
}

// reviewDeleteResult says only that Google accepted the request.
type reviewDeleteResult struct {
	Accepted bool   `json:"accepted"`
	Name     string `json:"name"`
}

type reviewArguments struct {
	Account         string `json:"account"`
	ProductReviewID string `json:"product_review_id"`
	PageSize        int    `json:"page_size"`
	Cursor          string `json:"cursor"`
}

func reviewsPath(account string) string {
	return reviewsAPI + "accounts/" + account + "/productReviews"
}

func reviewPath(account, id string) string { return reviewsPath(account) + "/" + id }

var errReviewID = "product_review_id must be 1 to 128 ASCII letters, digits, dots, dashes, and underscores, " +
	"starting with a letter or digit"

// prepareReview reads the arguments and settles the account against the binding before any secret is
// resolved. It reuses the call state of the region tools.
func prepareReview(op, tool string, resolved *config.Resolved, raw json.RawMessage) (regionCall, reviewArguments, error) {
	var args reviewArguments
	c := regionCall{op: op, tool: tool}
	if err := json.Unmarshal(raw, &args); err != nil {
		return c, args, provider.Fail(op, "the validated arguments could not be read")
	}
	var err error
	if c.scope, err = checkConnection(resolved); err != nil {
		return c, args, err
	}
	c.account, c.needsProof, err = c.scope.accountOf(args.Account)
	return c, args, err
}

func handleProductReviewsList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, args, err := prepareReview("list product reviews", productReviewsListID, resolved, raw)
	if err != nil {
		return nil, err
	}
	pageSize := args.PageSize
	if pageSize == 0 {
		pageSize = defaultReviewRows
	}
	if pageSize < 1 || pageSize > maxReviewRows {
		return nil, invalidArgument("page_size must be 1 to " + strconv.Itoa(maxReviewRows))
	}
	binding := provider.CursorBinding(c.tool, c.account, pageSize, c.scope.key())
	query := url.Values{"pageSize": {strconv.Itoa(pageSize)}}
	if args.Cursor != "" {
		token, ok := provider.DecodeCursor(binding, args.Cursor, maxCursorSize)
		if !ok || token == "" {
			return nil, refusedCursor()
		}
		query.Set("pageToken", token)
	}
	cl, err := c.client(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var answer reviewsAnswer
	if err := cl.get(ctx, c.op, c.tool, reviewsPath(c.account), query, &answer); err != nil {
		return nil, err
	}
	if len(answer.ProductReviews) > maxReviewRows {
		return nil, tooMany(c.op)
	}
	out := reviewsResult{ProductReviews: answer.ProductReviews}
	if out.ProductReviews == nil {
		out.ProductReviews = []productReview{}
	}
	if out.NextCursor, err = nextCursor(c.op, binding, answer.NextPageToken); err != nil {
		return nil, err
	}
	return out, nil
}

func handleProductReviewsGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, args, err := prepareReview("get product review", productReviewsGetID, resolved, raw)
	if err != nil {
		return nil, err
	}
	if !validReviewID(args.ProductReviewID) {
		return nil, invalidArgument(errReviewID)
	}
	cl, err := c.client(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var out productReview
	if err := cl.get(ctx, c.op, c.tool, reviewPath(c.account, args.ProductReviewID), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func handleProductReviewsDelete(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, args, err := prepareReview("delete product review", productReviewsDeleteID, resolved, raw)
	if err != nil {
		return nil, err
	}
	if !validReviewID(args.ProductReviewID) {
		return nil, invalidArgument(errReviewID)
	}
	cl, err := c.client(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	path := reviewPath(c.account, args.ProductReviewID)
	if err := cl.send(ctx, c.op, c.tool, request{method: http.MethodDelete, path: path, change: true}, nil); err != nil {
		return nil, err
	}
	return reviewDeleteResult{Accepted: true, Name: path[len(reviewsAPI):]}, nil
}

// reviewDeleteSummary phrases the confirmation from the validated arguments alone.
func reviewDeleteSummary(raw json.RawMessage) string {
	var a reviewArguments
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	id := a.ProductReviewID
	if !validReviewID(id) {
		id = "(invalid ID)"
	}
	out := "Delete product review " + strconv.Quote(id)
	if validID(a.Account) {
		out += " of account " + a.Account
	}
	return out + "; Google applies the deletion afterwards, and Qatlas cannot create the review again"
}

func reviewHandlers() map[string]capability.Handler {
	return map[string]capability.Handler{productReviewsListID: handleProductReviewsList,
		productReviewsGetID: handleProductReviewsGet, productReviewsDeleteID: handleProductReviewsDelete}
}

func reviewSummaries() map[string]capability.SummaryFunc {
	return map[string]capability.SummaryFunc{productReviewsDeleteID: reviewDeleteSummary}
}
