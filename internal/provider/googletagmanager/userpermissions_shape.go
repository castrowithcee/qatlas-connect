package googletagmanager

import (
	"encoding/json"
	"strings"
)

// Permission values a caller may set. The unspecified values of the API are results only.
var (
	accountPermissions   = []string{"noAccess", "user", "admin"}
	containerPermissions = []string{"noAccess", "read", "edit", "approve", "publish"}
)

func enumSchema(values []string) string {
	encoded, _ := json.Marshal(values)
	return `{"type":"string","enum":` + string(encoded) + `}`
}

type accountAccess struct {
	Permission idText `json:"permission,omitempty"`
}

type containerAccess struct {
	ContainerID idText `json:"containerId,omitempty"`
	Permission  idText `json:"permission,omitempty"`
}

// userPermission is a UserPermission of the API. emailAddress is personal data and appears only in results.
type userPermission struct {
	Path            idText            `json:"path,omitempty"`
	AccountID       idText            `json:"accountId,omitempty"`
	EmailAddress    nameText          `json:"emailAddress,omitempty"`
	AccountAccess   *accountAccess    `json:"accountAccess,omitempty"`
	ContainerAccess []containerAccess `json:"containerAccess,omitempty"`
}

type (
	userPermissionsAnswer struct {
		UserPermission []userPermission `json:"userPermission"`
		NextPageToken  string           `json:"nextPageToken"`
	}
	userPermissionsResult struct {
		UserPermissions []userPermission `json:"user_permissions"`
		NextCursor      string           `json:"next_cursor,omitempty"`
	}
	deleteUserPermissionResult struct {
		UserPermission string `json:"user_permission"`
		Deleted        bool   `json:"deleted"`
	}
)

// updateBody is the request of an update. emailAddress is not an argument: it is sent back as stored.
type updateBody struct {
	EmailAddress    string                `json:"emailAddress,omitempty"`
	AccountAccess   updateAccountAccess   `json:"accountAccess"`
	ContainerAccess []updateContainerPerm `json:"containerAccess"`
}

type updateAccountAccess struct {
	Permission string `json:"permission"`
}

type updateContainerPerm struct {
	ContainerID string `json:"containerId"`
	Permission  string `json:"permission"`
}

type containerRight struct {
	Container  string `json:"container"`
	Permission string `json:"permission"`
}

// updateArguments are the members of an update call that set or compare rights; container is a path, so its
// account can be compared before any request. The previous_ members only compare and are never sent.
type updateArguments struct {
	AccountAccess           *updateAccountAccess `json:"accountAccess"`
	ContainerAccess         []containerRight     `json:"containerAccess"`
	PreviousEmailAddress    *string              `json:"previous_email_address"`
	PreviousAccountAccess   *updateAccountAccess `json:"previous_account_access"`
	PreviousContainerAccess []containerRight     `json:"previous_container_access"`
}

var (
	accountAccessSchema = objectSchema(member("permission", stringSchema))
	containerAccessList = `{"type":"array","items":` + objectSchema(member("containerId", stringSchema),
		member("permission", stringSchema)) + `}`
	userPermissionMembers = []string{member("path", stringSchema), member("accountId", stringSchema),
		member("emailAddress", stringSchema), member("accountAccess", accountAccessSchema),
		member("containerAccess", containerAccessList)}
	userPermissionSchema = objectSchema(userPermissionMembers...)
)

// summaryOf phrases the address and the rights before and after, container by container: a container that is
// added or removed shows (none) on one side.
func (u updateArguments) summaryOf() string {
	var parts []string
	if u.PreviousEmailAddress != nil {
		parts = append(parts, "user "+*u.PreviousEmailAddress)
	}
	oldAccount, newAccount := "?", "?"
	if u.PreviousAccountAccess != nil {
		oldAccount = u.PreviousAccountAccess.Permission
	}
	if u.AccountAccess != nil {
		newAccount = u.AccountAccess.Permission
	}
	parts = append(parts, "account "+oldAccount+" -> "+newAccount)
	oldRights := map[string]string{}
	var order []string
	for _, c := range u.PreviousContainerAccess {
		oldRights[c.Container] = c.Permission
		order = append(order, c.Container)
	}
	newRights := map[string]string{}
	for _, c := range u.ContainerAccess {
		newRights[c.Container] = c.Permission
		if _, held := oldRights[c.Container]; !held {
			order = append(order, c.Container)
		}
	}
	for _, container := range order {
		parts = append(parts, container+" "+orNone(oldRights, container)+" -> "+orNone(newRights, container))
	}
	return strings.Join(parts, ", ")
}

func orNone(rights map[string]string, container string) string {
	if p, ok := rights[container]; ok {
		return p
	}
	return "(none)"
}
