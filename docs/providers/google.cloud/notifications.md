---
description: >
  Describes the Google Cloud Storage tool notifications.list: its arguments, the target check through the bucket's project, the Pub/Sub notification configurations returned, why the topic is only read as untrusted text, the scope and profile, and what is not offered.
type: knowledge
edit: shared
created: 2026-11-18
updated: 2026-11-18
---

# Google Cloud bucket notifications

`notifications.list` reads the Pub/Sub notification configurations of a bucket from Cloud Storage
(`notifications.list` of `storage` v1, discovery revision 20261111). It changes nothing: creating and deleting
configurations are not offered. The request is a GET to
`https://storage.googleapis.com/storage/v1/b/{bucket}/notificationConfigs`.

| Tool | Arguments |
| --- | --- |
| `notifications.list` | `bucket`, `user_project` |

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
