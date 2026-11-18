---
description: >
  Describes the Google Cloud Storage tools notifications.list and notifications.delete: their arguments, the target check through the bucket's project, the Pub/Sub notification configurations returned, why the topic is only read as untrusted text and compared before a deletion, the scopes and profiles, and what is not offered.
type: knowledge
edit: shared
created: 2026-11-18
updated: 2026-11-18
---

# Google Cloud bucket notifications

`notifications.list` reads the Pub/Sub notification configurations of a bucket from Cloud Storage
(`notifications.list` of `storage` v1, discovery revision 20261111). It changes nothing. The request is a GET to
`https://storage.googleapis.com/storage/v1/b/{bucket}/notificationConfigs`. A second tool
[deletes a configuration](#deleting-a-configuration). Creating a configuration (`notifications.insert`) is not
offered.

| Tool | Arguments |
| --- | --- |
| `notifications.list` | `bucket`, `user_project` |
| `notifications.delete` | `bucket`, `notification_id`, `topic`, `user_project` |

`bucket` and `user_project` follow the rules of [buckets](buckets.md): the bucket name is checked before any secret
is read and is never a path, and the bucket is bound to the targets by one read of the bucket and its project
number before the list is read. A bucket outside the targets is refused without naming it.

The result is `items`, each with `id`, `topic`, `event_types`, `custom_attributes`, `object_name_prefix`,
`payload_format`, `etag`, `kind`, and `selfLink`. `topic` is the Pub/Sub topic that receives the events. Qatlas
returns it as untrusted text and never fetches, resolves, or calls it. A member of another type than the documented
one makes the answer invalid. Google allows at most 100 configurations per bucket; an answer with more is invalid.
The result is empty if Google sends no configuration.

The method lists `devstorage.read_only`, so the tool asks for it and belongs to the profile `read`. The account
needs `storage.buckets.get` for the proof read of the bucket's project and for the list, and the Cloud Storage JSON
API must be enabled in the project.

## Deleting a configuration

`notifications.delete` is `notifications.delete` of `storage` v1
(`DELETE .../b/{bucket}/notificationConfigs/{notification}`). It needs the confirmation of a person and is sent once,
without a retry. After a timeout, a 5xx answer, or an unreadable answer the tool says the outcome is unknown, and the
configuration must be read before it is deleted again. Deleting stops the events of the bucket to its topic and cannot
be undone, and no tool creates a configuration.

`notification_id` is 1 to 64 letters, digits, hyphens, and underscores, checked before any secret is read and escaped
in the path. `topic` is required and is the Pub/Sub topic as `notifications.list` shows it, up to 256 characters. It
is only a comparison and is never written, fetched, or called: the short form `projects/P/topics/T` names the same
topic as `//pubsub.googleapis.com/projects/P/topics/T`.

Qatlas binds the bucket to the targets before any other request, then reads the configuration with
`notifications.get`. It ends without a deletion if the answer is another configuration or has another topic than
`topic`, and the refusal does not name the topic it read. Only then is the one deletion sent. A bucket outside the
targets is refused without naming it, after the one read of the bucket. On a connection with targets the bucket is
read once more just before the deletion. The confirmation names the bucket, the configuration, and the topic. The
result is `bucket`, `notificationId`, and `topic` as given, and `deleted`; Google answers with an empty body. The time
between the reading and the deletion is a documented limit.

The method lists `devstorage.read_write`, which `notifications.get` and `buckets.get` accept as well, so the tool asks
for it. It belongs to the profile `storage-revoke`, which is not recommended. The account needs
`storage.buckets.update` for the deletion and `storage.buckets.get`, and the Cloud Storage JSON API must be enabled.
