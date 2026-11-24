package googlesearchconsole

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-kit/ratelimit"
)

func TestIndexingPublishDayLimitRefusesBeforeIO(t *testing.T) {
	env := newEnvironment(t, nil)
	resolved, err := env.cfg.Resolve("index", "")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, pacific())
	notificationDays.Replace(credentialKey(resolved), ratelimit.NewCounter(dailyNotifications,
		ratelimit.Daily(pacific()), func() time.Time { return now }))
	single := `{"url":"` + jobURL + `","type":"URL_UPDATED"}`

	// A batch counts once per entry.
	parts := make([]batchPart, 100)
	for i := range parts {
		parts[i] = okPart(i+1, `{}`)
	}
	env.fake.answer = batchReply(parts...)
	if _, err := env.change(t, indexingPublishBatchID, "index", notificationsArgument(jobURLs(100), urlDeleted)); err != nil {
		t.Fatal(err)
	}
	env.fake.answer = reply(http.StatusOK, `{}`)
	for i := range 99 {
		if _, err := env.change(t, indexingPublishID, "index", single); err != nil {
			t.Fatalf("single %d: %v", i+1, err)
		}
	}
	// 199 are used: a batch of two does not fit and takes nothing, one single still does.
	before, grants := env.fake.calls()
	_, err = env.change(t, indexingPublishBatchID, "index", notificationsArgument(jobURLs(2), urlUpdated))
	if classOf(err) != provider.ClassRateLimited || !strings.Contains(err.Error(), "retry after 43200 seconds") {
		t.Fatalf("batch over the limit = %v", err)
	}
	if after, g := env.fake.calls(); len(after) != len(before) || g != grants {
		t.Errorf("the refused batch caused %d requests and %d grants", len(after)-len(before), g-grants)
	}
	if _, err := env.change(t, indexingPublishID, "index", single); err != nil {
		t.Fatalf("200th notification = %v", err)
	}
	// The 201st is refused before any request or token grant.
	before, grants = env.fake.calls()
	_, err = env.change(t, indexingPublishID, "index", single)
	if classOf(err) != provider.ClassRateLimited {
		t.Fatalf("201st notification = %v", err)
	}
	if after, g := env.fake.calls(); len(after) != len(before) || g != grants {
		t.Errorf("the refused call caused %d requests and %d grants", len(after)-len(before), g-grants)
	}
	// Reads are not counted.
	if _, err := env.invoke(t, indexingMetadataID, "index", `{"url":"`+jobURL+`"}`); err != nil {
		t.Errorf("metadata read = %v", err)
	}
}
