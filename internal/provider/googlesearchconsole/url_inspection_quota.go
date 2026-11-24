package googlesearchconsole

import (
	"errors"
	"fmt"
	"time"
	_ "time/tzdata" // the quota day needs the zone also where the system has no zone database

	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-kit/ratelimit"
)

// Google allows the URL inspection 600 queries per minute for one site. The spacing keeps a process within
// that quota.
const inspectionInterval = 100 * time.Millisecond

// dailyInspections is Google's inspection quota per property and day.
const dailyInspections = 2000

// inspectionLimiters spaces the inspections per credential and property.
var inspectionLimiters = ratelimit.NewRegistry(inspectionInterval)

// inspectionDays counts the inspections per credential and property. Google resets the daily quota at
// midnight Pacific Time.
var inspectionDays = ratelimit.NewCounterRegistry(dailyInspections, ratelimit.Daily(pacific()))

func pacific() *time.Location {
	zone, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		panic(err)
	}
	return zone
}

// countInspection reserves one inspection of the day. It runs before any secret is read or request is
// sent. A used-up day ends the call as rate-limited; the count is only what this process sent.
func countInspection(op, key string) error {
	_, err := inspectionDays.For(key).Reserve(1)
	var used *ratelimit.ExhaustedError
	if errors.As(err, &used) {
		return &provider.Error{Class: provider.ClassRateLimited, Op: op, Message: fmt.Sprintf(
			"Qatlas counted %d inspections of this property today (quota day ends at midnight Pacific Time); "+
				"retry after %d seconds", dailyInspections, int((used.Wait+time.Second-1)/time.Second))}
	}
	return err
}
