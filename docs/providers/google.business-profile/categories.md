---
description: >
  Describes the categories tools of the Google Business Profile provider: listing the categories Google knows for a region and language with display name filter, view, page size and cursor, reading several categories by name, the Google methods behind them, the argument forms, and the category fields.
type: knowledge
edit: shared
created: 2026-11-18
updated: 2026-11-18
---

# Google Business Profile: categories

The category tools of the [Google Business Profile provider](README.md). Credential, approval, quotas, bounds,
and errors are described there. Both tools read and are safe to repeat.

The categories are Google's reference data, the same for everyone. The tools read nothing of the user, name
no account or location, and stay available on a connection bound to any target. A category has a stable name
`categories/gcid:ID`, which a location lists as its primary or additional category. A call needs the region
and the language to answer in, and `view`:

- `BASIC` returns `name` and `displayName`.
- `FULL` also returns `serviceTypes` (`serviceTypeId`, `displayName`) and `moreHoursTypes` (`hoursTypeId`,
  `displayName`, `localizedDisplayName`).

`region_code` is an ISO 3166-1 alpha-2 code in capitals, such as `DE`. `language_code` is a BCP 47 code, such as
`de` or `en-GB`. `view` has no default, because Google refuses an unspecified view.

## google.business-profile.categories.list

`GET https://mybusinessbusinessinformation.googleapis.com/v1/categories`. Lists the categories for a region and
a language, 100 at most per call; `page_size` asks for 1 to 100, and Google's default is 100.

- `filter` is a text the display name of a category must match, up to 100 characters. Qatlas sends it as
  Google's filter `displayName=TEXT`, the only filter Google supports. A text with control characters is
  refused.
- `next_cursor` reads on. It is bound to the region, the language, the view, and the filter, not to the page
  size, and a cursor of other arguments or of another tool is refused without a request.

An empty list is no error.

## google.business-profile.categories.batch-get

`GET https://mybusinessbusinessinformation.googleapis.com/v1/categories:batchGet`. Reads several categories by
name in one language.

- `names` holds 1 to 50 names `categories/gcid:ID`, each once. An ID has lower-case letters, digits, and
  underscores; any other form, an empty list, and a repeated name are refused before a request.
- `region_code` is optional; Google infers a non-standard language from it.

Google returns the categories in no fixed order, and a name it does not know is left out, so the result may
hold fewer categories than names. The call has no cursor.

## Limits

- The answer of a list holds at most 100 categories and that of a batch at most as many as names; more is
  `invalid-provider-response`.
- A category's lists of service types and hours types are cut at 250 entries.
- The search text of `filter` goes to Google and is not logged by Qatlas Connect.
