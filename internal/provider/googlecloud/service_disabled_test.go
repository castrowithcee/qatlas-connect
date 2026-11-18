package googlecloud

import (
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// The message of a disabled API names the service the request went to, from the fixed tables, and nothing
// from the answer.
func TestServiceDisabledNamesTheCalledService(t *testing.T) {
	legacy := `{"error":{"code":403,"message":"` + detailCanary + `","errors":[{"reason":"forbidden"}],` +
		`"details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"SERVICE_DISABLED",` +
		`"domain":"googleapis.com","metadata":{"service":"` + detailCanary + `.example.com"}}]}}`
	cases := []struct {
		name string
		svc  service
		body string
		want string
	}{
		{"resource manager", serviceResourceManager, errorObject(403, "PERMISSION_DENIED", "SERVICE_DISABLED"),
			"Cloud Resource Manager API (cloudresourcemanager.googleapis.com)"},
		{"iam", serviceIAM, errorObject(403, "PERMISSION_DENIED", "SERVICE_DISABLED"),
			"Identity and Access Management (IAM) API (iam.googleapis.com)"},
		{"service usage", serviceUsage, errorObject(403, "PERMISSION_DENIED", "SERVICE_DISABLED"),
			"Service Usage API (serviceusage.googleapis.com)"},
		{"compute legacy form", serviceCompute, legacy, "Compute Engine API (compute.googleapis.com)"},
		{"storage", serviceStorage, errorObject(403, "PERMISSION_DENIED", "SERVICE_DISABLED"),
			"Cloud Storage API (storage.googleapis.com)"},
		{"dns", serviceDNS, errorObject(403, "PERMISSION_DENIED", "SERVICE_DISABLED"),
			"Cloud DNS API (dns.googleapis.com)"},
		{"billing", serviceBilling, errorObject(403, "PERMISSION_DENIED", "SERVICE_DISABLED"),
			"Cloud Billing API (cloudbilling.googleapis.com)"},
		{"billing budgets", serviceBillingBudgets, errorObject(403, "PERMISSION_DENIED", "SERVICE_DISABLED"),
			"Cloud Billing Budget API (billingbudgets.googleapis.com)"},
		{"api keys", serviceAPIKeys, errorObject(403, "PERMISSION_DENIED", "SERVICE_DISABLED"),
			"API Keys API (apikeys.googleapis.com)"},
		{"quotas", serviceQuotas, errorObject(403, "PERMISSION_DENIED", "SERVICE_DISABLED"),
			"Cloud Quotas API (cloudquotas.googleapis.com)"},
	}
	if len(cases) != len(serviceOrigins) || len(serviceNames) != len(serviceOrigins) {
		t.Fatalf("cases = %d, origins = %d, names = %d", len(cases), len(serviceOrigins), len(serviceNames))
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			failed := googleError("op", c.svc, 403, []byte(c.body))
			if failed.Class != provider.ClassPermission {
				t.Fatalf("class = %s", failed.Class)
			}
			if !strings.Contains(failed.Message, "the "+c.want+" is not enabled") {
				t.Errorf("message = %q, want it to name %s", failed.Message, c.want)
			}
			if strings.Contains(failed.Error(), detailCanary) {
				t.Errorf("message %q carries provider text", failed.Error())
			}
			for _, other := range serviceOrigins {
				host := strings.TrimPrefix(other, "https://")
				if host != strings.TrimPrefix(serviceOrigins[c.svc], "https://") && strings.Contains(failed.Message, host) {
					t.Errorf("message %q names another service %s", failed.Message, host)
				}
			}
		})
	}
}

func TestServiceDisabledThroughATool(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes[quotaInfosPath] = reply{status: 403, body: errorObject(403, "PERMISSION_DENIED", "SERVICE_DISABLED")}
	_, err := e.invoke(quotaInfosListID, `{"project_id":"my-project-123","service":"compute.googleapis.com"}`)
	if err == nil || !strings.Contains(err.Error(), "cloudquotas.googleapis.com") ||
		strings.Contains(err.Error(), "compute.googleapis.com") || strings.Contains(err.Error(), detailCanary) {
		t.Fatalf("err = %v", err)
	}
}
