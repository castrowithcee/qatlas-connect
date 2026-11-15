package gen

import (
	"slices"
	"strings"
	"testing"
)

// Every deferred sesv2 operation has a reason of its own: no two groups of operations share the generic text of
// an unfinished table, and the operations of the deliverability, dedicated IP, tenant, reputation, multi-region endpoint, and job tools are no longer deferred.
func TestSESv2DeferredOperationsHaveTheirOwnReason(t *testing.T) {
	reasons := map[string]string{}
	for _, o := range sesv2Table.Defer {
		reasons[o.Operation] = o.Reason
		if len(o.Reason) < 30 || strings.Contains(o.Reason, "not curated yet; the SES service package") {
			t.Errorf("%s: reason %q is not an own reason", o.Operation, o.Reason)
		}
	}
	for _, group := range []struct {
		reason string
		ops    []string
	}{
		{reasonSESPricing, []string{"PutAccountPricingAttributes"}},
	} {
		for _, op := range group.ops {
			if reasons[op] != group.reason {
				t.Errorf("%s: reason %q, want %q", op, reasons[op], group.reason)
			}
		}
	}
	for _, tool := range slices.Concat(sesDeliverabilityTools, sesDedicatedTools, sesTenantTools, sesMultiRegionTools, sesJobWriteTools) {
		if _, deferred := reasons[tool.Operation]; deferred {
			t.Errorf("%s is a tool and still deferred", tool.Operation)
		}
	}
}
