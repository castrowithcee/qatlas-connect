---
description: >
  Describes the trigger tools of the Google Tag Manager provider: listing, reading, creating, replacing, deleting, and reverting the triggers of a workspace, the members of a trigger and its filter conditions, the confirmation, and what is not verified.
type: knowledge
edit: shared
created: 2026-11-16
updated: 2026-11-16
---

# Google Tag Manager: triggers

The trigger tools of the [Google Tag Manager provider](README.md). The update, revert, parameter, and
never-cut rules shared with tags are described there.

A trigger decides when the tags of a container fire. A change is made in the workspace only and takes effect on
the pages of the container with a published version, which no tool of this provider makes. Every change is
confirmed, and the confirmation shows the name, the type, and the workspace or trigger path.

| Tool | Does |
| --- | --- |
| `triggers.list` | reads the triggers of a workspace, a batch per call with `cursor` |
| `triggers.get` | reads one trigger by its path, the fingerprint included |
| `triggers.create` | creates a trigger in a workspace; a repeated call creates a second trigger |
| `triggers.update` | replaces a trigger as a whole; needs `fingerprint` |
| `triggers.delete` | deletes a trigger from the workspace |
| `triggers.revert` | discards the workspace changes of a trigger; needs `fingerprint`; only by `tools` list |

`workspace` and `trigger` are paths such as `accounts/1/containers/2/workspaces/3/triggers/5`; both are checked
against the target of the connection.

## Members

The arguments of `create` and `update` are the members of the trigger. `name` and `type` are needed, and
`type` is one of the event types of the API, such as `pageview`, `customEvent`, `click`, `linkClick`,
`formSubmission`, `timer`, `scrollDepth`, or `elementVisibility`. The others are `filter`, `autoEventFilter`,
`customEventFilter`, `parameter`, `parentFolderId`, `notes`, and these members, each one parameter:
`eventName`, `waitForTags`, `checkValidation`, `waitForTagsTimeout`, `uniqueTriggerId`, `intervalSeconds`,
`maxTimerLengthSeconds`, `limit`, `interval`, `verticalScrollPercentageList`,
`horizontalScrollPercentageList`, `visibilitySelector`, `visiblePercentageMin`, `visiblePercentageMax`,
`continuousTimeMinMilliseconds`, `totalTimeMinMilliseconds`, and `selector`. Which of them a type takes is
stated in the descriptors; Google lists some as valid for Timer, Form Submission, and Link Click triggers and
others for AMP containers only. Members that Google assigns (`path`, `accountId`, `containerId`,
`workspaceId`, `triggerId`, `fingerprint`, `tagManagerUrl`) are results only and are never sent.

A filter is a list of conditions, and the trigger fires only when all of them are true. A condition has a
`type` (`equals`, `contains`, `startsWith`, `endsWith`, `matchRegex`, `greater`, `greaterOrEquals`, `less`,
`lessOrEquals`, `cssSelector`, `urlMatches`) and parameters: `arg0` (a variable reference) and `arg1` as
operands, `ignore_case` for a case-insensitive `matchRegex`, and `negate` to invert the operator.
`autoEventFilter` serves auto event triggers such as click and form submission, `customEventFilter` a custom
event.

The members that hold one parameter, and the parameters of a condition, nest two levels, which fits a list of
percentages. The parameters of `parameter` nest five.

```json
{"workspace": "accounts/1/containers/2/workspaces/3", "name": "Example click", "type": "linkClick",
 "filter": [{"type": "contains", "parameter": [
   {"type": "template", "key": "arg0", "value": "{{Click URL}}"},
   {"type": "template", "key": "arg1", "value": "example.invalid"}]}]}
```

Filter values are data and are never run: a regular expression or a selector is stored as text, and a read
returns it uncut.

Not verified: which members each trigger type needs or accepts, and whether the API takes members that its
documentation marks for another container type.
