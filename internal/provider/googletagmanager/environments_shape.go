package googletagmanager

import (
	"encoding/json"
	"regexp"
	"strings"
)

// environmentBody holds the members of an environment a caller sets. An environment is replaced as a whole, so
// its texts are plain strings that a read returns uncut: what a read shows can be sent back without losing text.
type environmentBody struct {
	Name               string `json:"name,omitempty"`
	Type               string `json:"type,omitempty"`
	Description        string `json:"description,omitempty"`
	URL                string `json:"url,omitempty"`
	EnableDebug        bool   `json:"enableDebug,omitempty"`
	ContainerVersionID string `json:"containerVersionId,omitempty"`
}

var (
	environmentURLRE     = regexp.MustCompile(`^https?://[^\s]+$`)
	environmentVersionRE = regexp.MustCompile(`^[0-9]{1,20}$`)
)

const (
	maxEnvironmentURL = 2048
	// userEnvironment is the only type a tool of this provider sets: the system environments are Google's.
	userEnvironment = "user"
)

// checkInput refuses an environment the schema would refuse as well, for a handler reached without the schema.
func (b environmentBody) checkInput() error {
	switch {
	case strings.TrimSpace(b.Name) == "":
		return invalidArgument("name is required")
	case b.Type != "" && b.Type != userEnvironment:
		return invalidArgument("type must be user: only custom environments can be created or changed")
	case b.URL != "" && (len(b.URL) > maxEnvironmentURL || !environmentURLRE.MatchString(b.URL)):
		return invalidArgument("url must be an http or https URL of at most 2048 bytes")
	case b.ContainerVersionID != "" && !environmentVersionRE.MatchString(b.ContainerVersionID):
		return invalidArgument("containerVersionId must be a number")
	}
	return nil
}

// environment is an Environment of the API without its authorizationCode: the code is the access key of a
// preview environment, so it is never held in the result and goes to the redactor instead.
type environment struct {
	Path                   idText  `json:"path,omitempty"`
	AccountID              idText  `json:"accountId,omitempty"`
	ContainerID            idText  `json:"containerId,omitempty"`
	EnvironmentID          idText  `json:"environmentId,omitempty"`
	WorkspaceID            idText  `json:"workspaceId,omitempty"`
	Type                   idText  `json:"type,omitempty"`
	Name                   string  `json:"name,omitempty"`
	Description            string  `json:"description,omitempty"`
	URL                    string  `json:"url,omitempty"`
	EnableDebug            bool    `json:"enableDebug,omitempty"`
	ContainerVersionID     idText  `json:"containerVersionId,omitempty"`
	AuthorizationTimestamp idText  `json:"authorizationTimestamp,omitempty"`
	Fingerprint            idText  `json:"fingerprint,omitempty"`
	TagManagerURL          urlText `json:"tagManagerUrl,omitempty"`

	code string
}

func (e *environment) UnmarshalJSON(b []byte) error {
	type plain environment
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	var key struct {
		AuthorizationCode string `json:"authorizationCode"`
	}
	_ = json.Unmarshal(b, &key)
	*e = environment(p)
	e.code = key.AuthorizationCode
	return nil
}

type (
	environmentsAnswer struct {
		Environment   []environment `json:"environment"`
		NextPageToken string        `json:"nextPageToken"`
	}
	environmentsResult struct {
		Environments []environment `json:"environments"`
		NextCursor   string        `json:"next_cursor,omitempty"`
	}
	deleteEnvironmentResult struct {
		Environment string `json:"environment"`
		Deleted     bool   `json:"deleted"`
	}
)

var environmentSchema = objectSchema(member("path", stringSchema), member("accountId", stringSchema),
	member("containerId", stringSchema), member("environmentId", stringSchema), member("workspaceId", stringSchema),
	member("type", stringSchema), member("name", stringSchema), member("description", stringSchema),
	member("url", stringSchema), member("enableDebug", boolSchema), member("containerVersionId", stringSchema),
	member("authorizationTimestamp", stringSchema), member("fingerprint", stringSchema),
	member("tagManagerUrl", stringSchema))
