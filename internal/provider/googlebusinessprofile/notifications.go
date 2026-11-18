package googlebusinessprofile

import (
	"context"
	"encoding/json"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	notificationsGetID = Provider + ".notifications.get"

	notificationSettingSuffix = "/notificationSetting"
)

var notificationsRisk = capability.Risk{
	Effect: capability.EffectRead, Idempotency: capability.IdempotencySafe, Confirmation: capability.ConfirmationNone,
	OpenWorld: true,
	DataSensitivity: "the Pub/Sub topic that receives the notifications of the account and the event types it " +
		"subscribes to, which show where business events are delivered",
}

// notificationSetting is the Pub/Sub notification setting of an account as the notifications API documents it.
type notificationSetting struct {
	Name              short       `json:"name,omitempty"`
	PubsubTopic       line        `json:"pubsubTopic,omitempty"`
	NotificationTypes list[short] `json:"notificationTypes,omitempty"`
}

var notificationSettingSchema = objectSchema("name", stringSchema, "pubsubTopic", stringSchema,
	"notificationTypes", arraySchema(stringSchema))

var notificationsGet = capability.Descriptor{
	ID: notificationsGetID, Version: 1, Title: "Get the Business Profile notification setting",
	Description: "Read the Pub/Sub notification setting of one Google Business Profile account: the topic that " +
		"receives notifications and the event types it subscribes to. The topic is a resource name, not a " +
		"secret, and is shown so that it is clear where notifications go; an account without a topic has " +
		"none to show. The tool only reads and sets no topic. A connection bound to a location does not " +
		"offer this tool. " + untrustedNote,
	Tags: []string{"google", "business-profile", "notifications"}, Risk: notificationsRisk, Provider: Provider,
	InputSchema: inputSchema(nil, "account", accountArgSchema), OutputSchema: json.RawMessage(notificationSettingSchema),
	Arguments: []capability.Argument{{Name: "account", Target: "account", Description: "Account as " +
		"accounts/ACCOUNT; optional when the connection is bound to an account, and then it must be that account"}},
	Fields: []capability.Field{
		{Name: "name", Description: "accounts/ACCOUNT/notificationSetting"},
		{Name: "pubsubTopic", Description: "Pub/Sub topic that receives the notifications, as a resource name"},
		{Name: "notificationTypes", Description: "Event types sent to the topic, such as GOOGLE_UPDATE, NEW_REVIEW, " +
			"UPDATED_REVIEW, NEW_CUSTOMER_MEDIA, DUPLICATE_LOCATION, or VOICE_OF_MERCHANT_UPDATED; Google still names " +
			"some deprecated types, which are shown as sent"},
	},
	Examples: []capability.Example{{Description: "Setting of one account",
		Arguments: json.RawMessage(`{"account":"accounts/1000001"}`)}},
}

func handleNotificationsGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "get notification setting"
	var args struct {
		Account string `json:"account"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, provider.Fail(op, "the validated arguments could not be read")
	}
	bound, err := checkConnection(resolved)
	if err != nil {
		return nil, err
	}
	name, err := bound.accountOf("account", args.Account, false)
	if err != nil {
		return nil, err
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var setting notificationSetting
	if err := c.get(ctx, op, notificationsGetID, request{root: notificationsRoot,
		path: accountPath(name) + notificationSettingSuffix, notFound: notFoundAccount}, &setting); err != nil {
		return nil, err
	}
	return setting, nil
}
