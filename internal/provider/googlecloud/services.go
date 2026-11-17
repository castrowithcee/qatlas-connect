package googlecloud

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

// apiRoot is the origin a service configuration may name: the default of the provider.
const apiRoot = "https://cloudresourcemanager.googleapis.com"
