package googlemerchant

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// Synthetic reviewer values: none of them has the shape of a real identifier.
const (
	syntheticReviewerID    = "reviewer-id-synthetic-one"
	syntheticReviewerName  = "reviewer-name-synthetic-one"
	syntheticReviewerImage = "https://img.example.invalid/reviewer-image-synthetic.png"
)

const reviewFixture = `{"name":"accounts/1000001/productReviews/review-1","productReviewId":"review-1",` +
	`"productReviewAttributes":{"reviewTime":"2026-01-02T03:04:05Z","pros":["fast","cheap"],"cons":["loud"],` +
	`"reviewLink":{"type":"SINGLETON","link":"https://reviews.example.invalid/r1"},` +
	`"reviewerImageLinks":["` + syntheticReviewerImage + `"],"productNames":["Widget"],` +
	`"productLinks":["https://shop.example.invalid/widget"],"asins":["B000000000"],"gtins":["00012345678905"],` +
	`"mpns":["W-1"],"skus":["sku-1"],"brands":["Brand"],"collectionMethod":"POST_FULFILLMENT",` +
	`"transactionId":"tx-1","aggregatorName":"agg","subclientName":"sub","publisherName":"pub",` +
	`"publisherFavicon":"https://pub.example.invalid/f.png","reviewerId":"` + syntheticReviewerID + `",` +
	`"reviewerIsAnonymous":true,"reviewerUsername":"` + syntheticReviewerName + `","reviewLanguage":"en",` +
	`"reviewCountry":"US","title":"Good","content":"Works well","minRating":"1","maxRating":"5","rating":4.5,` +
	`"isSpam":false,"isVerifiedPurchase":true,"isIncentivizedReview":true,"unknownMember":"never held"},` +
	`"customAttributes":[{"name":"c1","value":"v1","groupValues":[{"name":"g1","value":"gv1"}]}],` +
	`"dataSource":"accounts/1000001/dataSources/55",` +
	`"productReviewStatus":{"destinationStatuses":[{"reportingContext":"PRODUCT_REVIEWS"}],` +
	`"itemLevelIssues":[{"code":"c","severity":"NOT_IMPACTED","resolution":"MERCHANT_ACTION","attribute":"title",` +
	`"reportingContext":"PRODUCT_REVIEWS","description":"d","detail":"dt","documentation":"https://doc.example.invalid/x"}],` +
	`"createTime":"2026-01-03T00:00:00Z","lastUpdateTime":"2026-01-04T00:00:00Z"}}`

func TestReviewToolsAreDeclaredWithTheirRisk(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	for id, w := range map[string]struct {
		effect      capability.Effect
		idempotency capability.Idempotency
		change      bool
	}{
		productReviewsListID:   {capability.EffectRead, capability.IdempotencySafe, false},
		productReviewsGetID:    {capability.EffectRead, capability.IdempotencySafe, false},
		productReviewsDeleteID: {capability.EffectDelete, capability.IdempotencyIdempotent, true},
	} {
		d, _, ok := reg.Lookup(id)
		if !ok || d.Version != 1 || d.Risk.Effect != w.effect || d.Risk.Idempotency != w.idempotency ||
			d.RequiresToolAllowList != w.change || !d.Risk.OpenWorld {
			t.Fatalf("%s = %+v, %v", id, d, ok)
		}
		inRead := slices.Contains(metadata.Profiles[0].Tools, id)
		if inRead == w.change {
			t.Errorf("%s in read profile = %t", id, inRead)
		}
		if w.change {
			if d.Risk.Confirmation != capability.ConfirmationRequired || reg.ConfirmationSummary(id) == nil {
				t.Errorf("%s needs a confirmation and its summary", id)
			}
			for _, part := range []string{"afterwards", "only that Google accepted", "never repeated", "cannot be submitted"} {
				if !strings.Contains(d.Description, part) {
					t.Errorf("%s description misses %q", id, part)
				}
			}
		} else if d.Risk.Confirmation != capability.ConfirmationNone {
			t.Errorf("%s asks for confirmation", id)
		}
		if scopes := metadata.Token.Scopes[id]; len(scopes) != 1 || scopes[0] != contentScope {
			t.Errorf("%s scopes = %v", id, scopes)
		}
		for _, bad := range []string{`"body"`, "data_source", "dataSource", "reviewer"} {
			if strings.Contains(string(d.InputSchema), bad) {
				t.Errorf("%s input schema holds %s: %s", id, bad, d.InputSchema)
			}
		}
		if !strings.Contains(string(d.InputSchema), `"additionalProperties":false`) {
			t.Errorf("%s input schema is open", id)
		}
	}
	// Submitting a review is excluded: no tool of that kind exists, whatever its spelling.
	for _, d := range descriptors() {
		if strings.Contains(d.ID, "product-reviews") && !slices.Contains(
			[]string{productReviewsListID, productReviewsGetID, productReviewsDeleteID}, d.ID) {
			t.Errorf("unexpected review tool %s", d.ID)
		}
	}
}

func TestProductReviewsShowTheDocumentedMembersIncludingReviewerIdentifiers(t *testing.T) {
	h := newHarness(t)
	long := strings.Repeat("c", 5000)
	notes := make([]string, 30)
	for i := range notes {
		notes[i] = `"` + strings.Repeat("p", 800) + `"`
	}
	second := `{"name":"accounts/1000001/productReviews/r2","productReviewAttributes":{"content":"` + long +
		`","title":"` + long + `","pros":[` + strings.Join(notes, ",") + `],"brands":[` + strings.Join(notes, ",") + `]}}`
	seen := h.regionAnswers(t, func(r regionWire) (int, string) {
		if strings.HasSuffix(r.path, "/productReviews") {
			return 200, `{"productReviews":[` + reviewFixture + `,` + second + `],"nextPageToken":"two"}`
		}
		return 200, reviewFixture
	})
	raw := func(out map[string]any) string {
		b, _ := json.Marshal(out)
		return string(b)
	}
	list, err := h.call(t, handleProductReviewsList, `{"account":"1000001","page_size":2}`)
	if err != nil {
		t.Fatal(err)
	}
	if (*seen)[0].path != "/reviews/v1/accounts/1000001/productReviews" || (*seen)[0].query != "pageSize=2" ||
		(*seen)[0].method != http.MethodGet {
		t.Errorf("request = %+v", (*seen)[0])
	}
	rows := list["productReviews"].([]any)
	first, cut := rows[0].(map[string]any), rows[1].(map[string]any)
	attrs := first["productReviewAttributes"].(map[string]any)
	for _, k := range []string{"reviewTime", "pros", "cons", "reviewLink", "productNames", "productLinks", "asins",
		"gtins", "mpns", "skus", "brands", "collectionMethod", "transactionId", "aggregatorName", "subclientName",
		"publisherName", "publisherFavicon", "reviewerIsAnonymous", "reviewLanguage", "reviewCountry", "title",
		"content", "minRating", "maxRating", "rating", "isVerifiedPurchase", "isIncentivizedReview"} {
		if attrs[k] == nil {
			t.Errorf("attribute %s is missing: %v", k, attrs)
		}
	}
	if attrs["rating"] != 4.5 || attrs["unknownMember"] != nil || first["dataSource"] == nil ||
		first["customAttributes"] == nil || first["productReviewStatus"] == nil {
		t.Errorf("first = %v", first)
	}
	status := first["productReviewStatus"].(map[string]any)
	if len(status["itemLevelIssues"].([]any)) != 1 || len(status["destinationStatuses"].([]any)) != 1 {
		t.Errorf("status = %v", status)
	}
	cutAttrs := cut["productReviewAttributes"].(map[string]any)
	if n := len([]rune(cutAttrs["content"].(string))); n != maxReviewTextLen {
		t.Errorf("content has %d runes", n)
	}
	if n := len([]rune(cutAttrs["title"].(string))); n != maxReviewTextLen {
		t.Errorf("title has %d runes", n)
	}
	pros := cutAttrs["pros"].([]any)
	if len(pros) != maxReviewListLen || len([]rune(pros[0].(string))) != maxReviewNoteLen {
		t.Errorf("pros = %d entries of %d runes", len(pros), len([]rune(pros[0].(string))))
	}
	brands := cutAttrs["brands"].([]any)
	if len(brands) != maxReviewListLen || len([]rune(brands[0].(string))) != maxNameRunes {
		t.Errorf("brands = %d entries of %d runes", len(brands), len([]rune(brands[0].(string))))
	}
	next, _ := list["next_cursor"].(string)
	if next == "" || next == "two" {
		t.Fatalf("next_cursor = %q", next)
	}
	if _, err := h.call(t, handleProductReviewsList, `{"account":"1000001","page_size":2,"cursor":"`+next+`"}`); err != nil {
		t.Fatal(err)
	}
	if got := (*seen)[1].query; !strings.Contains(got, "pageToken=two") || !strings.Contains(got, "pageSize=2") {
		t.Errorf("query = %q", got)
	}
	if _, err := h.call(t, handleProductReviewsList, `{"account":"1000001","page_size":3,"cursor":"`+next+`"}`); !isInvalidRequest(err) {
		t.Errorf("a cursor of other arguments = %v", err)
	}
	if _, err := h.call(t, handleProductReviewsList, `{"account":"1000001"}`); err != nil {
		t.Fatal(err)
	}
	if got := (*seen)[len(*seen)-1].query; got != "pageSize=25" {
		t.Errorf("default query = %q", got)
	}
	got, err := h.call(t, handleProductReviewsGet, `{"account":"1000001","product_review_id":"review-1"}`)
	if err != nil {
		t.Fatal(err)
	}
	if got["productReviewId"] != "review-1" {
		t.Errorf("get = %v", got)
	}
	if last := (*seen)[len(*seen)-1]; last.path != "/reviews/v1/accounts/1000001/productReviews/review-1" ||
		last.method != http.MethodGet {
		t.Errorf("get request = %+v", last)
	}
	// The reviewer's ID, name, and image links are shown, in a list and in one review.
	for name, out := range map[string]map[string]any{"list": list, "get": got} {
		text := raw(out)
		for _, want := range []string{syntheticReviewerID, syntheticReviewerName, syntheticReviewerImage,
			`"reviewerId"`, `"reviewerUsername"`, `"reviewerImageLinks"`, `"reviewerIsAnonymous":true`} {
			if !strings.Contains(text, want) {
				t.Errorf("%s output misses %q", name, want)
			}
		}
	}
	if cutAttrs["reviewerId"] != nil || cutAttrs["reviewerUsername"] != nil {
		t.Errorf("members that Google did not send appear: %v", cutAttrs)
	}
}

func TestProductReviewListsAreEmptyNotNullAndCapped(t *testing.T) {
	h := newHarness(t)
	h.regionAnswers(t, always(200, `{}`))
	out, err := h.call(t, handleProductReviewsList, `{"account":"1"}`)
	if err != nil {
		t.Fatal(err)
	}
	if rows, ok := out["productReviews"].([]any); !ok || len(rows) != 0 {
		t.Errorf("empty list = %v", out)
	}
	rows := make([]string, maxReviewRows+1)
	for i := range rows {
		rows[i] = `{"productReviewId":"r"}`
	}
	h.regionAnswers(t, always(200, `{"productReviews":[`+strings.Join(rows, ",")+`]}`))
	if _, err := h.call(t, handleProductReviewsList, `{"account":"1"}`); err == nil {
		t.Error("a list above the cap was shown")
	}
}

func TestProductReviewIDAndAccountAreRefusedBeforeSecretOrIO(t *testing.T) {
	for _, bad := range []string{"", "a/b", "..", ".", ".a", "a b", "a?x=1", "a%2Fb", "-a", "_a",
		strings.Repeat("a", maxReviewIDLen+1), "ä", "a\n", "../accounts/2"} {
		for name, handler := range map[string]capability.Handler{"get": handleProductReviewsGet,
			"delete": handleProductReviewsDelete} {
			h := newHarness(t)
			raw, _ := json.Marshal(map[string]any{"account": "1000001", "product_review_id": bad})
			if _, err := h.call(t, handler, string(raw)); !isInvalidRequest(err) {
				t.Errorf("%s with product_review_id %q = %v, want invalid request", name, bad, err)
			}
			h.untouched(t)
		}
	}
	for _, id := range []string{"a", "review-1", "R_2.x", strings.Repeat("a", maxReviewIDLen), "987654"} {
		if !validReviewID(id) {
			t.Errorf("review ID %q refused", id)
		}
	}
	for _, size := range []string{"101", "-1"} {
		h := newHarness(t)
		if _, err := h.call(t, handleProductReviewsList, `{"account":"1","page_size":`+size+`}`); !isInvalidRequest(err) {
			t.Errorf("page_size %s = %v", size, err)
		}
		h.untouched(t)
	}
	h := newHarness(t, "accounts/1000001")
	for _, handler := range []capability.Handler{handleProductReviewsList, handleProductReviewsGet, handleProductReviewsDelete} {
		_, err := h.call(t, handler, `{"account":"2000002","product_review_id":"a"}`)
		if !isInvalidRequest(err) || strings.Contains(err.Error(), "2000002") {
			t.Errorf("foreign account = %v", err)
		}
		if _, err := h.call(t, handler, `{"account":"01","product_review_id":"a"}`); !isInvalidRequest(err) {
			t.Errorf("malformed account = %v", err)
		}
	}
	h.untouched(t)
}

func TestProductReviewsOfASubAccountAreProvenBeforeDetailOrChange(t *testing.T) {
	route := func(subs string) func(regionWire) (int, string) {
		return func(r regionWire) (int, string) {
			if strings.HasSuffix(r.path, ":listSubaccounts") {
				return 200, `{"accounts":[` + subs + `]}`
			}
			return 200, reviewFixture
		}
	}
	calls := map[string]struct {
		handler capability.Handler
		args    string
	}{
		"list":   {handleProductReviewsList, `{"account":"20"}`},
		"get":    {handleProductReviewsGet, `{"account":"20","product_review_id":"a"}`},
		"delete": {handleProductReviewsDelete, `{"account":"20","product_review_id":"a"}`},
	}
	for name, c := range calls {
		h := newHarness(t, "advanced-accounts/10")
		seen := h.regionAnswers(t, route(accountRow("20", "Twenty")))
		if _, err := h.call(t, c.handler, c.args); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(*seen) != 2 || (*seen)[0].path != "/accounts/v1/accounts/10:listSubaccounts" ||
			!strings.Contains((*seen)[1].path, "/accounts/20/productReviews") {
			t.Errorf("%s requests = %+v", name, *seen)
		}
		h = newHarness(t, "advanced-accounts/10")
		seen = h.regionAnswers(t, route(accountRow("30", "Thirty")))
		_, err := h.call(t, c.handler, c.args)
		if !isInvalidRequest(err) || strings.Contains(err.Error(), "20") {
			t.Errorf("%s of a foreign account = %v", name, err)
		}
		if len(*seen) != 1 || !strings.HasSuffix((*seen)[0].path, ":listSubaccounts") {
			t.Errorf("%s sent more than the proof read: %+v", name, *seen)
		}
	}
}

func TestProductReviewsDeleteSendsOneRequest(t *testing.T) {
	h := newHarness(t)
	seen := h.regionAnswers(t, always(200, `{}`))
	out, err := h.call(t, handleProductReviewsDelete, `{"account":"1000001","product_review_id":"review-1"}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(*seen) != 1 {
		t.Fatalf("%d requests", len(*seen))
	}
	w := (*seen)[0]
	if w.method != http.MethodDelete || w.path != "/reviews/v1/accounts/1000001/productReviews/review-1" ||
		w.query != "" || w.body != "" {
		t.Errorf("request = %+v", w)
	}
	if out["accepted"] != true || out["name"] != "accounts/1000001/productReviews/review-1" {
		t.Errorf("result = %v", out)
	}
}

func TestProductReviewDeleteIsSentOnceAndReportsAnUnknownOutcome(t *testing.T) {
	const unknown = "outcome of the change is unknown"
	for name, c := range map[string]struct {
		status  int
		body    string
		class   provider.Class
		unknown bool
	}{
		"server error": {500, `{"error":{"status":"INTERNAL"}}`, provider.ClassProviderError, true},
		"quota":        {429, quotaBody(reasonMinuteQuota), provider.ClassRateLimited, false},
		"daily quota":  {429, quotaBody(reasonDayQuota), provider.ClassRateLimited, false},
		"role":         {403, googleErrorBody, provider.ClassPermission, false},
		"missing":      {404, googleErrorBody, provider.ClassNotFound, false},
		"expired":      {401, `{"error":{"status":"UNAUTHENTICATED"}}`, provider.ClassAuth, false},
	} {
		h := newHarness(t)
		h.google.api = func(*http.Request) (int, string, http.Header) {
			return c.status, c.body, http.Header{"Content-Type": {"application/json"}}
		}
		_, err := h.call(t, handleProductReviewsDelete, `{"account":"1000001","product_review_id":"a"}`)
		if err == nil {
			t.Fatalf("%s: no error", name)
		}
		if got := len(h.google.seen()); got != 1 {
			t.Errorf("%s: %d requests, want one", name, got)
		}
		if classOf(err) != c.class {
			t.Errorf("%s: class = %q, want %q", name, classOf(err), c.class)
		}
		if strings.Contains(err.Error(), unknown) != c.unknown {
			t.Errorf("%s: error %q, unknown outcome %t", name, err, c.unknown)
		}
		if strings.Contains(err.Error(), "synthetic provider echo") {
			t.Errorf("%s: the message carries Google's text", name)
		}
	}
}

func TestProductReviewReadsMapGoogleErrors(t *testing.T) {
	for status, class := range map[int]provider.Class{401: provider.ClassAuth, 403: provider.ClassPermission,
		404: provider.ClassNotFound, 429: provider.ClassRateLimited} {
		for name, args := range map[string]struct {
			handler capability.Handler
			args    string
		}{"get": {handleProductReviewsGet, `{"account":"1","product_review_id":"a"}`},
			"list": {handleProductReviewsList, `{"account":"1"}`}} {
			h := newHarness(t)
			h.regionAnswers(t, always(status, googleErrorBody))
			if _, err := h.call(t, args.handler, args.args); classOf(err) != class {
				t.Errorf("%s status %d = class %q, want %q", name, status, classOf(err), class)
			}
		}
	}
}

func TestProductReviewDeleteSummaryNamesAccountAndReview(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	got := reg.ConfirmationSummary(productReviewsDeleteID)(json.RawMessage(`{"account":"1000001","product_review_id":"review-1"}`))
	for _, part := range []string{"1000001", `"review-1"`, "afterwards"} {
		if !strings.Contains(got, part) {
			t.Errorf("summary %q misses %q", got, part)
		}
	}
	got = reg.ConfirmationSummary(productReviewsDeleteID)(json.RawMessage(`{"account":"1000001","product_review_id":"x\ny"}`))
	if strings.Contains(got, "x\ny") || strings.Contains(got, "\n") {
		t.Errorf("summary shows an invalid ID: %q", got)
	}
}

func TestReviewerIdentifiersAreCutAndAddressesLoseUserInformation(t *testing.T) {
	h := newHarness(t)
	longID, longName := strings.Repeat("i", 500), strings.Repeat("n", 900)
	images := make([]string, 30)
	for i := range images {
		images[i] = `"https://img.example.invalid/` + strings.Repeat("p", 3000) + `"`
	}
	login, host := "login-synthetic"+":"+"pass-synthetic", "img.example.invalid"
	userinfo := "https://" + login + "@" + host + "/a.png"
	h.regionAnswers(t, always(200, `{"productReviewAttributes":{"reviewerId":"`+longID+`","reviewerUsername":"`+
		longName+`","reviewerImageLinks":[`+strings.Join(images, ",")+`],"reviewLink":{"link":"`+userinfo+
		`"},"publisherFavicon":"`+userinfo+`","productLinks":["`+userinfo+`"]}}`))
	out, err := h.call(t, handleProductReviewsGet, `{"account":"1","product_review_id":"a"}`)
	if err != nil {
		t.Fatal(err)
	}
	attrs := out["productReviewAttributes"].(map[string]any)
	if n := len([]rune(attrs["reviewerId"].(string))); n != maxIDRunes {
		t.Errorf("reviewerId has %d runes", n)
	}
	if n := len([]rune(attrs["reviewerUsername"].(string))); n != maxNameRunes {
		t.Errorf("reviewerUsername has %d runes", n)
	}
	links := attrs["reviewerImageLinks"].([]any)
	if len(links) != maxReviewListLen || len([]rune(links[0].(string))) != maxURLRunes {
		t.Errorf("reviewerImageLinks = %d entries of %d runes", len(links), len([]rune(links[0].(string))))
	}
	text, _ := json.Marshal(out)
	if strings.Contains(string(text), login) || strings.Contains(string(text), "@"+host) {
		t.Errorf("user information survived: %s", text)
	}
	if attrs["publisherFavicon"] != "https://img.example.invalid/a.png" ||
		attrs["reviewLink"].(map[string]any)["link"] != "https://img.example.invalid/a.png" ||
		attrs["productLinks"].([]any)[0] != "https://img.example.invalid/a.png" {
		t.Errorf("addresses = %v", attrs)
	}
}

func TestReviewToolsNameThePersonalDataOfReviewers(t *testing.T) {
	for _, d := range descriptors() {
		if !strings.Contains(d.ID, ".product-reviews.") {
			continue
		}
		if d.Risk.DataSensitivity != reviewSensitivity || !strings.Contains(d.Risk.DataSensitivity, "personal-data") {
			t.Errorf("%s data sensitivity = %q", d.ID, d.Risk.DataSensitivity)
		}
		for _, part := range []string{"reviewerId", "reviewerUsername", "reviewerImageLinks", "personal data of third parties"} {
			if d.ID != productReviewsDeleteID && !strings.Contains(d.Description, part) {
				t.Errorf("%s description misses %q", d.ID, part)
			}
		}
	}
}
