package googlesearchconsole

import (
	"errors"
	"fmt"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-kit/ratelimit"
)

// dailyNotifications is Google's default Indexing API quota of notifications per day.
const dailyNotifications = 200

// notificationDays counts the notifications per credential. Google resets the daily quota at midnight
// Pacific Time.
var notificationDays = ratelimit.NewCounterRegistry(dailyNotifications, ratelimit.Daily(pacific()))

// countNotifications reserves n notifications of the day, all or none. It runs before any secret is read or
// request is sent. A used-up day ends the call as rate-limited; the count is only what this process sent.
func countNotifications(op string, resolved *config.Resolved, n int) error {
	_, err := notificationDays.For(credentialKey(resolved)).Reserve(n)
	var used *ratelimit.ExhaustedError
	if errors.As(err, &used) {
		return &provider.Error{Class: provider.ClassRateLimited, Op: op, Message: fmt.Sprintf(
			"the %d notifications Qatlas counted for this credential today leave no room for %d more (quota day "+
				"ends at midnight Pacific Time); retry after %d seconds", dailyNotifications, n,
			int((used.Wait+time.Second-1)/time.Second))}
	}
	return err
}
