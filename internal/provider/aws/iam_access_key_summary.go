package aws

import "encoding/json"

// The confirmation texts of the IAM access key tools. The key ID and the user name come from untrusted
// arguments, so each is cut. The text never holds a secret: the tools take none.
const (
	accessKeySummaryIDRunes   = 40
	accessKeySummaryUserRunes = 40
)

func init() {
	registerSummary("aws.iam.access-key.deactivate", accessKeySummaryOf("Deactivate", "; requests signed with it are refused from then on"))
	registerSummary("aws.iam.access-key.delete", accessKeySummaryOf("Permanently delete", "; this cannot be undone"))
}

func accessKeySummaryOf(action, effect string) func(json.RawMessage) string {
	return func(raw json.RawMessage) string {
		var a struct {
			AccessKeyID string `json:"access_key_id"`
			UserName    string `json:"user_name"`
		}
		if json.Unmarshal(raw, &a) != nil {
			return ""
		}
		user := "the calling user"
		if a.UserName != "" {
			user = "IAM user " + summaryClip(a.UserName, accessKeySummaryUserRunes)
		}
		return action + " access key " + summaryClip(a.AccessKeyID, accessKeySummaryIDRunes) + " of " + user + effect
	}
}
