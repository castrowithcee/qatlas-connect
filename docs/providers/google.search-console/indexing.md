---
description: >
  Describes the Indexing API tools of the Google Search Console provider: reading the notification status of URLs and notifying Google of updated or removed URLs, singly or in a batch, the use limited to JobPosting and BroadcastEvent, the listed-only notifications, the binding to a property, the daily count of notifications and the per-minute spacing, the batch transport, and partial failures.
type: knowledge
edit: shared
created: 2026-11-16
updated: 2026-11-16
---

# Google Search Console: indexing

The Indexing API tools of the [Google Search Console provider](README.md). Credential, target, bounds, and
errors are described there. `indexing.get-metadata` and `indexing.get-metadata-batch` read and are safe to
repeat. `indexing.publish` and `indexing.publish-batch` act at Google: they are listed-only and need a
confirmation for every call.

## What the API is for

Google accepts the Indexing API only for URLs whose content carries the structured data `JobPosting` or
`BroadcastEvent` embedded in a `VideoObject`. Qatlas does not check this; keeping to it is up to you. Google's
spam policies apply to what is sent.

A notification is no request to index. `URL_UPDATED` tells Google that the URL is new or changed and that it
may crawl it soon; `URL_DELETED` tells Google that the URL is gone. Before `URL_DELETED` the URL must answer
`404` or `410` or carry the robots meta tag `noindex`.

## Origin, scope, and rights

The Indexing API has its own fixed origin, `https://indexing.googleapis.com`; the `base_url` of the service
does not apply to it. All four tools need the scope `https://www.googleapis.com/auth/indexing`, the only scope
Google names for the API, for reading and for notifying alike. The account behind the credential must be an
owner of the property in Search Console, and the Indexing API must be enabled in the Cloud project apart from
the Search Console API; see [setup](setup.md#indexing-api). A refusal for either is `permission`.

## Listed-only notifications

`indexing.publish` and `indexing.publish-batch` are in no profile, and no effect offers them. A connection
offers them only when its `tools` list names them:

```yaml
connections:
  jobs-indexing:
    service: console
    credential: search-console
    target: sc-domain:example.com
    tools:
      - google.search-console.indexing.get-metadata
      - google.search-console.indexing.get-metadata-batch
      - google.search-console.indexing.publish
      - google.search-console.indexing.publish-batch
```

The reading tools follow the rules of every reading tool: the effect `read` offers them, and the profile
`edit` holds them. The profile `read` leaves them out, because their scope would let a read-only consent send
notifications at Google.

## Effect

A notification takes effect at Google and cannot be withdrawn; only a later `URL_DELETED` counters a
`URL_UPDATED`. Qatlas never undoes a notification on its own. Every call is a new notification that Google
counts, so the tools are not idempotent, and a failed call is never repeated: after a `401` the cached token
is dropped and the next call renews it.

## Binding to a property

The Indexing API names no property. On a connection bound to a property every URL must lie under it, by the
rules of the [feedpath of a change](sitemaps.md#feedpath-of-a-change): a domain property covers the domain and
its subdomains with either scheme, and a URL prefix is compared by scheme, host, and path. A URL outside the
property, also one entry of a batch, refuses the whole call before a secret is read or a request is sent. An
unbound connection reaches any URL the account owns.

URLs are absolute `http` or `https` addresses of at most 2048 characters, without spaces, control
characters, user information, or fragment; a query is allowed. Qatlas never fetches them.

## Quota and spacing

Google counts per Cloud project: by default 200 notifications per day, 180 metadata reads per minute, and 380
requests of any Indexing API method per minute. A batch counts once per entry. Qatlas spaces the requests of
one credential within the per-minute quotas, by 334 ms for a metadata read and by 158 ms for any request. A
batch takes its first slot and moves the next request back by the spacing of its other entries, so a batch of
100 metadata reads holds the next read for about half a minute.

Qatlas counts the notifications it sends for each credential: 200 per day, and a batch counts once per entry,
reserved all at once or not at all. The count runs before any secret is read or request is sent. A call that
would pass 200 in the quota day is refused as `rate-limited` with the seconds until the day ends, and a batch
that does not fit is refused as a whole. The day ends at midnight Pacific Time. The count lives in the process
and knows only its own notifications, and a call that failed after sending still counts. Google's own count
stays authoritative: when it is used up, Google answers `429` and the tool reports `rate-limited` too. On a `429`
the next Indexing API request of the credential waits as described under
[quotas and rate spacing](README.md#quotas-and-rate-spacing).

## Single calls

`indexing.get-metadata` sends `GET https://indexing.googleapis.com/v3/urlNotifications/metadata` with the URL
as the query parameter `url`. It shows only what Google received through the API, not whether or when Google
indexes the URL. A URL without any notification so far is a result without `latestUpdate` and `latestRemove`,
not an error: Google answers it with `404` and the status `NOT_FOUND`, which Qatlas maps to that result. A
`404` without Google's error object stays `not-found`.

`indexing.publish` sends `POST https://indexing.googleapis.com/v3/urlNotifications:publish` with the JSON body
`{"url": …, "type": …}` and nothing else. An answer of Google that cannot be read after a `2xx` is
`invalid-provider-response`, and its message says that Google accepted the notification.

## Batches

The batch tools send up to 100 calls in one `POST https://indexing.googleapis.com/batch`. More than 100
entries are refused before anything is sent. The body is `multipart/mixed`: one `application/http` part per
entry with the Content-ID `item-` and its position, a fixed method and path, and only the escaped URL, or the
JSON body of URL and type, as variable content. The access token travels only in the header of the whole
batch, never in a part.

Google answers each entry in its own part and may change the order; Qatlas matches a part to its entry by the
Content-ID alone. Each entry of the result holds its URL (and type) as sent, and either Google's answer or an
`error` with class, message, and HTTP status, mapped like the error of a single call. `succeeded` and
`failed` count the entries. One failed entry does not fail the others.

An entry without a readable answer, because its part is missing, malformed, or longer than 64 KiB, or because
the batch answer ended at its bound of 8 MiB, fails as `invalid-provider-response`. For a notification that
means the outcome is unknown: Google may have taken it, so check with `indexing.get-metadata` before sending it
again. A batch answer that is no `multipart/mixed` answer fails the whole call; for notifications its message
says the same. A failure of the whole batch request, such as `401` or `403`, fails the call like a single one.

## Not verified

- No live call was made. Methods, parameters, and answers are taken from the discovery document of the
  Indexing API v3, revision 20261107, and the batch format from Google's Indexing API guide.
- The Content-ID form `response-item-N` of a batch answer follows Google's general batch format and was not
  observed for this API.
- The answer for a URL without any notification (`404` with `NOT_FOUND`) and the `429` after the daily quota
  are known from Google's documentation and community sources, not observed.
