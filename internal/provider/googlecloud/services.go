package googlecloud

import "strings"

// service names one Google Cloud API this provider reaches. A tool never takes an origin: it names its
// service, and the origin comes from the fixed table below.
type service string

const (
	serviceResourceManager service = "resource-manager"
	serviceIAM             service = "iam"
	serviceUsage           service = "service-usage"
	serviceCompute         service = "compute"
	serviceStorage         service = "storage"
	serviceDNS             service = "dns"
	serviceBilling         service = "billing"
	serviceAPIKeys         service = "api-keys"
	serviceQuotas          service = "quotas"
)

// serviceOrigins holds the https origin of every service, as the rootUrl of its discovery document names it.
// The base path of a service (compute/v1 and storage/v1 have one) belongs to the tool that calls it. Nothing
// here comes from configuration, an argument, or an answer.
var serviceOrigins = map[service]string{
	serviceResourceManager: "https://cloudresourcemanager.googleapis.com",
	serviceIAM:             "https://iam.googleapis.com",
	serviceUsage:           "https://serviceusage.googleapis.com",
	serviceCompute:         "https://compute.googleapis.com",
	serviceStorage:         "https://storage.googleapis.com",
	serviceDNS:             "https://dns.googleapis.com",
	serviceBilling:         "https://cloudbilling.googleapis.com",
	serviceAPIKeys:         "https://apikeys.googleapis.com",
	serviceQuotas:          "https://cloudquotas.googleapis.com",
}

// serviceNames holds the display name of every service, for the message of a disabled API. Like the origins,
// it is fixed here and never read from an answer.
var serviceNames = map[service]string{
	serviceResourceManager: "Cloud Resource Manager API",
	serviceIAM:             "Identity and Access Management (IAM) API",
	serviceUsage:           "Service Usage API",
	serviceCompute:         "Compute Engine API",
	serviceStorage:         "Cloud Storage API",
	serviceDNS:             "Cloud DNS API",
	serviceBilling:         "Cloud Billing API",
	serviceAPIKeys:         "API Keys API",
	serviceQuotas:          "Cloud Quotas API",
}

// disabledAPI names the API of a service for the message of a disabled API: the host of its fixed origin and
// its display name. A service outside the tables yields the generic wording.
func disabledAPI(svc service) string {
	origin, ok := serviceOrigins[svc]
	if !ok {
		return "a Google API it needs"
	}
	host := strings.TrimPrefix(origin, "https://")
	if name, ok := serviceNames[svc]; ok {
		return "the " + name + " (" + host + ")"
	}
	return "the Google API " + host
}

// apiRoot is the origin a service configuration may name: the default of the provider.
const apiRoot = "https://cloudresourcemanager.googleapis.com"
