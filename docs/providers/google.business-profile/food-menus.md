---
description: >
  Describes the food menu tools of the Google Business Profile provider: reading the menus of a location with sections, dishes, prices, allergens, and descriptions, replacing them as a whole with a required update mask and the previous state read first, the confirmation with the number of entries before and after and the changed entries within 500 characters, the bounds on untrusted menu text, and what the v4 documentation leaves unverified.
type: knowledge
edit: shared
created: 2026-11-18
updated: 2026-11-18
---

# Google Business Profile: food menus

The food menu tools of the [Google Business Profile provider](README.md). Credential, target, approval, quotas,
bounds, and errors are described there. The tools use the My Business API v4 at
`https://mybusiness.googleapis.com/v4`, like the [review tools](reviews.md#the-v4-client). The parameters and
answer members come from the reference at `developers.google.com/my-business/reference/rest/v4` (the methods
`accounts.locations.getFoodMenus` and `accounts.locations.updateFoodMenus` and the FoodMenus resource with its
nested types and enums), read on 2026-11-18; no live call was made. Google documents both methods only for a
location whose `locationState.canHaveFoodMenus` is true.

The menus are **public**: names, descriptions, prices, allergens, and dietary restrictions appear on the Google
profile of the business.

## The v4 paths

The tools take the location as `accounts/ACCOUNT/locations/LOCATION` and add `/foodMenus` themselves. They check
the path against its form and the target before a secret is read, and prove the location for an account target,
exactly as the review tools do: see [the v4 client](reviews.md#the-v4-client). A refusal never names the other
object. The tools are offered on every target.

## Reading

`food-menus.get` belongs to the profile `read` and is safe to repeat.

| Argument | Meaning |
| --- | --- |
| `location` | required, `accounts/ACCOUNT/locations/LOCATION` |
| `read_mask` | `name`, `menus`, or both; the whole resource when omitted. Google does not accept a nested field such as `menus.sections` |

The result holds `name` and `menus`. A menu has `labels` (`displayName`, `description`, `languageCode`),
`sourceUrl`, `cuisines`, and `sections`; a section has `labels` and `items`; an item has `labels`, `attributes`,
and `options`, which repeat `labels` and `attributes`. The attributes are `price` (`currencyCode`, `units` as a
string, `nanos`), `spiciness`, `allergen`, `dietaryRestriction`, `nutritionFacts` (`calories`, `totalFat`,
`cholesterol`, `sodium`, `totalCarbohydrate`, `protein`), `ingredients`, `servesNumPeople`,
`preparationMethods`, `portionSize`, and `mediaKeys`. A location without menus answers with `name` only.

### Untrusted text

Names, descriptions, and every other text of a menu are written by whoever edits the profile. They are returned
unchanged as data: texts are cut at 4000 characters (codes and enumerations at 100, the address of a menu page
at 2048) and lists at 250 entries, members the reference does not list are dropped, and nothing in a result is
followed. No link is ever fetched.

## Replacing

`food-menus.update` (`PATCH .../foodMenus?updateMask=menus`) replaces the menus of a location as a whole. It
belongs to the profile `write` and needs confirmation. Google replaces a repeated field only as a whole, so
`menus` is the complete new list, and every menu, section, and dish that is not in it is removed.

| Argument | Meaning |
| --- | --- |
| `location` | required, the location path |
| `menus` | required, the complete new list of 1 to 250 menus, with the members of the `get` result |
| `previous_menus` | required, `menus` of `food-menus.get` as last read, `[]` for a location without menus; only compared, never sent |
| `update_mask` | required, exactly `["menus"]` |

`update_mask` is the only field of the resource that can be replaced (`name` cannot be updated). A missing mask
is refused, since Google would treat it as a full update that can clear fields. The tool never clears the
menus: an empty list is refused.

Before anything is sent the tool refuses what Google documents as incomplete: every menu needs a `labels` entry
with a `displayName` and at least one section, every section the same and at least one item, every item a
`labels` entry with a `displayName` and `attributes` with a `price` (`currencyCode`, `units`), and every option
`labels` and `attributes`. A `displayName` has at most 140 characters and stays on one line, a `description` has
at most 1000 characters. Control, invisible formatting, and separator characters are refused. A list of more than
250 entries at any level, a text over its bound, and a body of more than 1 MiB are refused and never cut.

### Read first

No update runs without the state it replaces. After the checks, and after the proof of the location for an
account target, the tool reads the menus again and sends nothing unless they equal `previous_menus` exactly.
Otherwise it answers `invalid-argument` and asks for a new `food-menus.get`. A live list that the tool itself
would cut (over 250 entries or a text over its bound) cannot be counted or compared, so it is not replaced from
here and has to be changed in Google's own interface.

### Confirmation

The confirmation names the location, the number of menus, sections, and dishes before and after, and the name
of every menu, section, and dish that is added, changed, or removed, and says when only the order changes:

```text
Replace the public food menus of accounts/1000001/locations/2000002: menus 1 -> 1, sections 2 -> 2, dishes 3 -> 3;
added: dish "Fish pie"; changed: dish "Bean stew"; removed: dish "Rye bread"
```

An entry is the same before and after when its position below its parents, its name, and its own content match;
a renamed entry shows as removed and added. A call that changes nothing is refused. The core cuts a
confirmation at 500 characters, which would hide what a person confirms, so an update whose confirmation does
not fit is refused before anything is sent. Change fewer entries per call or give them shorter names.

The result is the menus as Google holds them after the change. A change counts against the 10
changes per minute and profile; Qatlas spaces them by 6 seconds per location, like the other changes. A change
whose outcome is unclear (timeout, `5xx`, unreadable answer) is reported as such and never repeated: read the
menus with `food-menus.get` before changing them again.

## Not verified

- No live call was made. The reference states no length for names of items other than the 140 and 1000
  characters of the labels, none for `sourceUrl`, `mediaKeys`, or the number of menus, sections, and dishes:
  Qatlas holds 250 entries per list and 1 MiB per update.
- The reference marks `price` required for an item and does not say whether an option needs one: Qatlas
  requires `attributes` for an option and a `price` for an item only.
- The reference does not say whether `updateMask=menus` needs `name` in the body: Qatlas sends it.
- `locationState.canHaveFoodMenus` is not read: Google answers an ineligible location with an error.
