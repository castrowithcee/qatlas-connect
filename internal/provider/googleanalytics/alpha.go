package googleanalytics

// Tools on the Analytics Admin API v1alpha share this convention: their paths are built with alphaPath and
// their descriptions end with alphaNote. Every other Admin tool stays on v1beta.

// alphaPrefix starts the path of every Admin API v1alpha request.
const alphaPrefix = "/v1alpha/"

// alphaPath is the request path of a checked resource name on the Admin API v1alpha.
func alphaPath(resource string) string { return alphaPrefix + resource }

// alphaNote ends the description of every tool on the Admin API v1alpha.
const alphaNote = " ALPHA: this tool uses the Analytics Admin API v1alpha, which Google can change or remove " +
	"without notice, and so can the arguments and answer of this tool."
