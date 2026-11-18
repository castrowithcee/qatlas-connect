package googlebusinessprofile

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
)

func TestRegistrationDeclaresTheBoundOfEachTool(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	for id, want := range confirmLimits {
		if got := reg.ConfirmationSummaryRunes(id); got != want {
			t.Errorf("%s declares %d, want %d", id, got, want)
		}
		if reg.ConfirmationSummary(id) == nil {
			t.Errorf("%s declares a bound without a summary", id)
		}
	}
	// A tool without public text keeps the default of the core.
	for _, id := range []string{locationsDeleteID, mediaUpdateID, mediaDeleteID, localPostsDeleteID,
		reviewsDeleteReplyID, placeActionLinksDeleteID} {
		if got := reg.ConfirmationSummaryRunes(id); got != 0 {
			t.Errorf("%s declares %d, want the default", id, got)
		}
		if confirmLimit(id) != defaultConfirmRunes {
			t.Errorf("%s checks against %d", id, confirmLimit(id))
		}
	}
}

func TestConfirmationRunesCountsWhatTheCoreWrites(t *testing.T) {
	for text, want := range map[string]int{
		"abc":          3,
		"a\nb\tc\rd":   10,
		"a\u0007b":     8,
		"a‮b":          8,
		"a\U000e0001b": 12,
		"a\xffb":       3,
	} {
		if got := confirmationRunes(text); got != want {
			t.Errorf("confirmationRunes(%q) = %d, want %d", text, got, want)
		}
	}
}

// shownWhole is the text as the core writes it: a line break becomes its two-character escape.
func shownWhole(text string) string { return strings.ReplaceAll(text, "\n", `\n`) }

func TestReplyOfGoogleMaximumIsConfirmedWholeAndOneByteMoreIsRefused(t *testing.T) {
	env, f := reviewsEnvironment(t)
	f.review = reviewJSON("AbCd_123", "FIVE", "ok")
	plain := strings.Repeat("Thank you for your visit.\n", 200)[:maxReplyBytes]
	// Every byte of an address is shown twice, the worst case for the confirmation.
	links := strings.Repeat("https://example.invalid/"+strings.Repeat("a", 60)+"\n", 100)[:maxReplyBytes]
	for name, comment := range map[string]string{"line breaks": plain, "addresses": links} {
		_, err := env.core.Invoke(context.Background(), application.InvokeRequest{Operation: reviewsUpdateReplyID,
			Connection: "w-all", Arguments: json.RawMessage(replyArgs(comment, "FIVE", "ok", "an old reply"))})
		var needs *application.ConfirmationRequiredError
		if !errors.As(err, &needs) {
			t.Fatalf("%s: error = %v, want confirmation required", name, err)
		}
		if !strings.Contains(needs.Summary, shownWhole(strings.TrimSpace(comment))) || strings.Contains(needs.Summary, "…") ||
			strings.Contains(needs.Summary, "refused") {
			t.Errorf("%s: summary is not the whole reply: %.200q", name, needs.Summary)
		}
		if got := confirmationRunes(replySummary(reviewsArgumentsOf(t, replyArgs(comment, "FIVE", "ok", "an old reply")))); got > confirmLimit(reviewsUpdateReplyID) {
			t.Errorf("%s: %d characters exceed the bound", name, got)
		}
	}
	if _, err := env.change(t, reviewsUpdateReplyID, "w-all", replyArgs(plain+"a", "FIVE", "ok", "")); !isInvalidRequest(err) {
		t.Errorf("a reply of 4097 bytes: error = %v, want invalid request", err)
	}
	env.noRequests(t)
}

func reviewsArgumentsOf(t *testing.T, raw string) reviewsArguments {
	t.Helper()
	var a reviewsArguments
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestLocalPostOf1500CharactersIsConfirmedWhole(t *testing.T) {
	env, _ := postsEnvironment(t)
	text := strings.Repeat("Fresh bread every morning.\n", 60)[:1500]
	summary := env.postConfirmation(t, localPostsCreateID, "w-all", createOf("STANDARD",
		`,"summary":`+mustJSON(text)+`,"call_to_action":{"action_type":"LEARN_MORE","url":"https://example.invalid/`+
			strings.Repeat("a", 1900)+`"}`))
	if !strings.Contains(summary, shownWhole(strings.TrimSpace(text))) || strings.Contains(summary, "…") ||
		strings.HasPrefix(summary, "Refused") {
		t.Errorf("summary is not the whole text: %.200q", summary)
	}
	// An update shows the text whole too.
	summary = env.postConfirmation(t, localPostsUpdateID, "w-all", `{"local_post":"`+v4Location+`/localPosts/AbCd_123",`+
		`"topic_type":"STANDARD","update_mask":["summary"],"summary":`+mustJSON(text)+`}`)
	if !strings.Contains(summary, shownWhole(strings.TrimSpace(text))) || strings.Contains(summary, "…") {
		t.Errorf("update summary is not the whole text: %.200q", summary)
	}
}

func TestLocationCreateShowsAnEscapedTitleOfMaximumLengthWhole(t *testing.T) {
	env, _ := locationEnvironment(t)
	title := strings.Repeat(`"`, 500)
	args := strings.Replace(locationCreateArgs, `"Sample Bakery"`, mustJSON(title), 1)
	_, err := env.core.Invoke(context.Background(), application.InvokeRequest{Operation: locationsCreateID,
		Connection: "l-all", Arguments: json.RawMessage(args)})
	var needs *application.ConfirmationRequiredError
	if !errors.As(err, &needs) || !strings.Contains(needs.Summary, strings.Repeat(`\"`, 500)) ||
		strings.Contains(needs.Summary, "cannot be confirmed") || strings.Contains(needs.Summary, "…") {
		t.Fatalf("error = %v", err)
	}
}
