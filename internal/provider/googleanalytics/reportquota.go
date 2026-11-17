package googleanalytics

import (
	"fmt"
	"sync"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// Quota rules of the Data API (quotas page of the Analytics Data API): a standard property runs at most 10
// concurrent requests and holds 40,000 tokens per hour, 14,000 per project and hour and 200,000 per day;
// an Analytics 360 property has more of each. Most reports cost 10 tokens or fewer.
const (
	// maxConcurrentReports is the documented concurrency of a standard property. A 360 property allows more,
	// but the service level is not known before a report runs, so the lower number holds for every property.
	maxConcurrentReports = 10
	// minTokensLeft is the reserve below which no further report is sent: ten times the usual maximum cost
	// of one report, so a burst of costly reports cannot take a bucket to zero before the answers arrive.
	minTokensLeft = 100
	// quotaMemory bounds how long a remembered remainder counts. The hourly buckets refill within the hour,
	// so an older value says nothing; Google still refuses what is really exhausted.
	quotaMemory = time.Hour
	// maxQuotaEntries bounds the remembered properties.
	maxQuotaEntries = 1000
)

// tokenQuota is the remaining tokens of the three token buckets of a property; -1 is unknown.
type tokenQuota struct {
	hour, day, projectHour int64
}

type quotaEntry struct {
	left tokenQuota
	at   time.Time
}

// quotaBook remembers the last propertyQuota of every property per credential, and counts running reports
// per property.
type quotaBook struct {
	mu      sync.Mutex
	entries map[string]quotaEntry
	running map[string]int
}

// quotas is the memory of the process for the core reports. It is replaced by tests.
var quotas = &quotaBook{entries: map[string]quotaEntry{}, running: map[string]int{}}

// realtimeQuotas is the memory of the realtime reports. Google keeps a quota of its own for them, which its
// answer reports as the realtime quota, so what one kind of report leaves says nothing about the other.
var realtimeQuotas = &quotaBook{entries: map[string]quotaEntry{}, running: map[string]int{}}

// now is the clock of the quota memory.
var now = time.Now

// rateLimited is the refusal that sends no request.
func rateLimited(op, message string) *provider.Error {
	return &provider.Error{Class: provider.ClassRateLimited, Op: op, Message: message}
}

// enter takes one of the concurrent slots of a property, or refuses before any I/O when all are taken or the
// remembered quota is below the reserve. The returned function releases the slot.
func (q *quotaBook) enter(op, property, identity string) (func(), error) {
	return q.enterN(op, property, identity, 1)
}

// enterN takes n slots at once for a batch, which Google counts as n reports. The reserve grows with n for the
// same reason. A batch that does not fit is refused as a whole, so no part of it is sent.
func (q *quotaBook) enterN(op, property, identity string, n int) (func(), error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if e, ok := q.entries[property+"\x00"+identity]; ok && now().Sub(e.at) < quotaMemory {
		for _, b := range []struct {
			name string
			left int64
		}{{"tokens per hour", e.left.hour}, {"tokens per day", e.left.day},
			{"tokens per project and hour", e.left.projectHour}} {
			if reserve := int64(minTokensLeft * n); b.left >= 0 && b.left < reserve {
				return nil, rateLimited(op, fmt.Sprintf("the reporting quota of the property is nearly used up "+
					"(%s left: %d, reserve %d); no report was sent, wait for the quota to refill",
					b.name, b.left, reserve))
			}
		}
	}
	if q.running[property]+n > maxConcurrentReports {
		return nil, rateLimited(op, fmt.Sprintf("%d reports of this property are already running and %d more "+
			"would exceed the %d Google allows at once; no report was sent, try again when one has finished",
			q.running[property], n, maxConcurrentReports))
	}
	q.running[property] += n
	return func() {
		q.mu.Lock()
		defer q.mu.Unlock()
		if q.running[property] -= n; q.running[property] <= 0 {
			delete(q.running, property)
		}
	}, nil
}

// remember keeps the remainders of an answer. A bucket the answer does not name stays unknown.
func (q *quotaBook) remember(property, identity string, p *propertyQuota) {
	if p == nil {
		return
	}
	left := tokenQuota{hour: p.TokensPerHour.remaining(), day: p.TokensPerDay.remaining(),
		projectHour: p.TokensPerProjectPerHour.remaining()}
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.entries) >= maxQuotaEntries {
		for k, e := range q.entries {
			if now().Sub(e.at) >= quotaMemory {
				delete(q.entries, k)
			}
		}
		if len(q.entries) >= maxQuotaEntries {
			clear(q.entries)
		}
	}
	q.entries[property+"\x00"+identity] = quotaEntry{left: left, at: now()}
}

// universalMetadata is the name that asks for the dimensions and metrics common to all properties.
const universalMetadata = "properties/0"

// reportProperty resolves the property of a Data API tool before any I/O: the argument, or the one bound
// property. A real property follows the binding; proof says that a proof read must show its account first.
// With universal set, properties/0 is accepted: it names no property, so the binding does not apply to it.
func (b binding) reportProperty(given string, universal bool) (name string, proof bool, err error) {
	name = given
	if name == "" && len(b.properties) == 1 && len(b.accounts) == 0 {
		name = b.properties[0]
	}
	if universal && name == universalMetadata {
		return name, false, nil
	}
	if _, err := checkPropertyName("property", name); err != nil {
		return "", false, err
	}
	proof, err = b.property(name)
	return name, proof, err
}
