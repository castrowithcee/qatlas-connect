---
description: >
  Describes the notification tool of the Google Business Profile provider: reading the Pub/Sub notification setting of an account with its topic and event types, the Google method behind it, the method that sets the topic and is left out, and how account and location targets narrow it.
type: knowledge
edit: shared
created: 2026-11-18
updated: 2026-11-18
---

# Google Business Profile: notifications

The notification tool of the [Google Business Profile provider](README.md). Credential, target, approval, quotas,
bounds, and errors are described there. The tool only reads and is safe to repeat. It uses the My Business
Notifications API, which is enabled in the Cloud project on its own, at
`https://mybusinessnotifications.googleapis.com`. Method, parameter, and answer follow its discovery document,
revision 20261116.

## google.business-profile.notifications.get

`GET https://mybusinessnotifications.googleapis.com/v1/{account}/notificationSetting`. Reads the Pub/Sub
notification setting of one account. `account` is `accounts/ACCOUNT` with a decimal ID. A connection bound to an
account may leave it out; it must then be that account. A connection bound to a location does not offer this tool.

The result holds:

| Member | Holds |
| --- | --- |
| `name` | `accounts/ACCOUNT/notificationSetting` |
| `pubsubTopic` | the Pub/Sub topic that receives the notifications, as a resource name such as `projects/PROJECT/topics/TOPIC` |
| `notificationTypes` | the event types sent to the topic, such as `GOOGLE_UPDATE`, `NEW_REVIEW`, `UPDATED_REVIEW`, `NEW_CUSTOMER_MEDIA`, `DUPLICATE_LOCATION`, or `VOICE_OF_MERCHANT_UPDATED` |

The topic is a resource name, not a secret. It is shown so that it is clear where notifications go. An account
that has no topic yet has no `pubsubTopic`. Google still names some deprecated types, such as the question and
answer types and `LOSS_OF_VOICE_OF_MERCHANT`; they are shown as Google sends them.

## Left out

`accounts.updateNotificationSetting` sets the topic and the event types, or removes the setting with an empty
list of types. It has no tool: it would send the events of the account to a destination chosen by the caller.
The setting is changed in the Business Profile or in Google's own tools.
