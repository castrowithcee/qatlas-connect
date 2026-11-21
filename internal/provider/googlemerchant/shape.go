package googlemerchant

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// What Google returns is untrusted content: account names and home page addresses come from whoever runs the
// account. Text types cut themselves while they are decoded and drop a value that is not a string, list
// types keep a bounded number of entries, and a member the types do not list is never held. URLs are text
// that is never fetched. The members are those of the discovery documents of the Merchant API v1.

// Bounds of what a result shows.
const (
	maxIDRunes    = 100
	maxNameRunes  = 300
	maxURLRunes   = 2048
	maxListItems  = 100
	maxEntries    = 1000
	maxPageToken  = 2048
	maxCursorSize = 4096
)

type (
	idText   string // IDs, resource names, codes
	nameText string // display names
	urlText  string // links, never fetched
	idList   []string
)

func (s *idText) UnmarshalJSON(b []byte) error { *s = idText(clipJSON(b, maxIDRunes)); return nil }
func (s *nameText) UnmarshalJSON(b []byte) error {
	*s = nameText(clipJSON(b, maxNameRunes))
	return nil
}
func (s *urlText) UnmarshalJSON(b []byte) error { *s = urlText(clipJSON(b, maxURLRunes)); return nil }

// UnmarshalJSON keeps at most maxListItems strings of a list, each cut to the length of an ID.
func (l *idList) UnmarshalJSON(b []byte) error {
	var raw []json.RawMessage
	if json.Unmarshal(b, &raw) != nil {
		*l = nil
		return nil
	}
	var out []string
	for _, item := range raw {
		if len(out) == maxListItems {
			break
		}
		if text := clipJSON(item, maxIDRunes); text != "" {
			out = append(out, text)
		}
	}
	*l = out
	return nil
}

// clipJSON decodes a JSON string and cuts it to limit runes, marking a cut with …; anything else is empty.
func clipJSON(b []byte, limit int) string {
	var value string
	if json.Unmarshal(b, &value) != nil {
		return ""
	}
	return clipText(value, limit)
}

// clipText cuts text to limit runes, marking a cut with …, and replaces invalid UTF-8.
func clipText(value string, limit int) string {
	value = strings.ToValidUTF8(value, "�")
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	runes := []rune(value)
	return string(runes[:limit-1]) + "…"
}

// timeZone is the IANA time zone of an account.
type timeZone struct {
	ID      idText `json:"id,omitempty"`
	Version idText `json:"version,omitempty"`
}

// account is the Account resource of the accounts sub-API. Which members exist is the document's, so none
// is added or derived: the resource carries no account type.
type account struct {
	Name         idText    `json:"name,omitempty"`
	AccountID    idText    `json:"accountId,omitempty"`
	AccountName  nameText  `json:"accountName,omitempty"`
	TestAccount  bool      `json:"testAccount,omitempty"`
	AdultContent bool      `json:"adultContent,omitempty"`
	LanguageCode idText    `json:"languageCode,omitempty"`
	HomePageURI  urlText   `json:"homePageUri,omitempty"`
	TimeZone     *timeZone `json:"timeZone,omitempty"`
}

// id is the account ID of a row. Both the resource name and the ID member are read, and a row whose two
// spellings disagree or that has neither in the documented form has no usable ID, so it can never be matched
// against a target.
func (a account) id() (string, bool) {
	fromName, hasName := strings.CutPrefix(string(a.Name), accountPrefix)
	hasName = hasName && validID(fromName)
	fromID := string(a.AccountID)
	hasID := validID(fromID)
	switch {
	case hasName && hasID && fromName != fromID:
		return "", false
	case hasName:
		return fromName, true
	case hasID:
		return fromID, true
	}
	return "", false
}

// developerRegistration is the DeveloperRegistration resource: the Cloud projects registered with an account.
type developerRegistration struct {
	Name   idText `json:"name,omitempty"`
	GcpIDs idList `json:"gcpIds,omitempty"`
}

// The list answers of the API. nextPageToken is kept raw: it goes into a cursor and is bounded there.
type accountsAnswer struct {
	Accounts      []account `json:"accounts"`
	NextPageToken string    `json:"nextPageToken"`
}

// accountsResult is the result of the list tool. A list is never null, so an empty answer is an empty list.
type accountsResult struct {
	Accounts   []account `json:"accounts"`
	NextCursor string    `json:"next_cursor,omitempty"`
}

// Output schemas, written once from the types above.
const (
	stringSchema = `{"type":"string"}`
	boolSchema   = `{"type":"boolean"}`
	listSchema   = `{"type":"array","items":{"type":"string"}}`
)

func objectSchema(members ...string) string {
	return `{"type":"object","properties":{` + strings.Join(members, ",") + `}}`
}

func member(name, schema string) string { return `"` + name + `":` + schema }

func listResultSchema(list, item string) string {
	return objectSchema(member(list, `{"type":"array","items":`+item+`}`), member("next_cursor", stringSchema))
}

var (
	accountSchema = objectSchema(member("name", stringSchema), member("accountId", stringSchema),
		member("accountName", stringSchema), member("testAccount", boolSchema), member("adultContent", boolSchema),
		member("languageCode", stringSchema), member("homePageUri", stringSchema),
		member("timeZone", objectSchema(member("id", stringSchema), member("version", stringSchema))))
	developerRegistrationSchema = objectSchema(member("name", stringSchema), member("gcpIds", listSchema))
)
