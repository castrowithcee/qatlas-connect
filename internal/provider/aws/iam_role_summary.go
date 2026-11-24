package aws

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"
)

// The confirmation texts of the IAM role and instance profile write tools. They stay well below the 500 runes the
// core allows, so its cut never takes the end of a text. They name the role, the effect, and for a trust policy the
// principals it admits, never the document itself.
const (
	roleSummaryNameRunes     = 40
	iamSummaryPrincipalRunes = 40
	iamSummaryPrincipals     = 4
	iamSummaryPolicyRunes    = 60
	roleSummaryTagKeys       = 5
	iamSummaryTagKeyRunes    = 24
)

const awsManagedPolicyPrefix = "arn:aws:iam::aws:policy/"

func init() {
	registerSummary("aws.iam.role.create", roleCreateSummary)
	registerSummary("aws.iam.role.update", roleUpdateSummary)
	registerSummary("aws.iam.role.update-trust-policy", roleTrustSummary)
	registerSummary("aws.iam.role.put-permissions-boundary", roleBoundarySummary)
	registerSummary("aws.iam.role.delete-permissions-boundary", roleBoundaryRemoveSummary)
	registerSummary("aws.iam.role.tag", roleTagSummary)
	registerSummary("aws.iam.role.untag", roleUntagSummary)
	registerSummary("aws.iam.role.delete", roleDeleteSummary)
	registerSummary("aws.iam.instance-profile.create", profileCreateSummary)
	registerSummary("aws.iam.instance-profile.delete", profileDeleteSummary)
	registerSummary("aws.iam.instance-profile.add-role", profileAddRoleSummary)
	registerSummary("aws.iam.instance-profile.remove-role", profileRemoveRoleSummary)
}

type roleSummaryArgs struct {
	Role        string      `json:"role_name"`
	Profile     string      `json:"instance_profile_name"`
	Trust       string      `json:"assume_role_policy_document"`
	Boundary    string      `json:"permissions_boundary"`
	Description *string     `json:"description"`
	Session     json.Number `json:"max_session_duration"`
	Tags        []struct {
		Key string `json:"key"`
	} `json:"tags"`
	TagKeys []string `json:"tag_keys"`
}

func iamSummary(raw json.RawMessage, f func(a roleSummaryArgs) string) string {
	var a roleSummaryArgs
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	return f(a)
}

func (a roleSummaryArgs) role() string {
	return "IAM role " + summaryClip(a.Role, roleSummaryNameRunes)
}

func (a roleSummaryArgs) profile() string {
	return "IAM instance profile " + summaryClip(a.Profile, roleSummaryNameRunes)
}

// iamSummaryPolicy names a permissions boundary, and says when it is an AWS managed policy.
func iamSummaryPolicy(arn string) string {
	if name, ok := strings.CutPrefix(arn, awsManagedPolicyPrefix); ok {
		return "AWS managed policy " + summaryClip(name, iamSummaryPolicyRunes)
	}
	return "policy " + summaryClip(arn, iamSummaryPolicyRunes)
}

// trustPrincipals lists the distinct principals of the Allow statements of a trust policy, sorted, and counts the
// Deny statements. ok is false when the document is not readable, so the text then says nothing about it.
func trustPrincipals(doc string) (allow []string, deny int, ok bool) {
	var top struct {
		Statement json.RawMessage `json:"Statement"`
	}
	if json.Unmarshal([]byte(doc), &top) != nil {
		return nil, 0, false
	}
	var statements []struct {
		Effect    string          `json:"Effect"`
		Principal json.RawMessage `json:"Principal"`
	}
	if json.Unmarshal(top.Statement, &statements) != nil {
		statements = statements[:0]
		var one struct {
			Effect    string          `json:"Effect"`
			Principal json.RawMessage `json:"Principal"`
		}
		if json.Unmarshal(top.Statement, &one) != nil {
			return nil, 0, false
		}
		statements = append(statements, one)
	}
	for _, s := range statements {
		if s.Effect == "Deny" {
			deny++
			continue
		}
		var wildcard string
		var byKey map[string]json.RawMessage
		switch {
		case json.Unmarshal(s.Principal, &wildcard) == nil:
			allow = append(allow, wildcard)
		case json.Unmarshal(s.Principal, &byKey) == nil:
			keys := make([]string, 0, len(byKey))
			for k := range byKey {
				keys = append(keys, k)
			}
			slices.Sort(keys)
			for _, k := range keys {
				var values []string
				if one := ""; json.Unmarshal(byKey[k], &one) == nil {
					values = []string{one}
				} else if json.Unmarshal(byKey[k], &values) != nil {
					continue
				}
				for _, v := range values {
					if k != "AWS" && k != "Service" {
						v = k + ":" + v
					}
					allow = append(allow, v)
				}
			}
		}
	}
	return slices.Compact(slices.Sorted(slices.Values(allow))), deny, true
}

// iamTrustText says who a trust policy lets assume the role.
func iamTrustText(doc string) string {
	allow, deny, ok := trustPrincipals(doc)
	if !ok {
		return "trust policy not readable here, not summarized"
	}
	out := "no principal may assume it"
	if len(allow) > 0 {
		shown := make([]string, 0, iamSummaryPrincipals)
		for _, p := range allow[:min(len(allow), iamSummaryPrincipals)] {
			shown = append(shown, summaryClip(p, iamSummaryPrincipalRunes))
		}
		out = "assumable by " + strings.Join(shown, ", ")
		if more := len(allow) - len(shown); more > 0 {
			out += " and " + strconv.Itoa(more) + " more"
		}
	}
	if deny > 0 {
		out += "; " + strconv.Itoa(deny) + " Deny statement(s)"
	}
	return out
}

func roleCreateSummary(raw json.RawMessage) string {
	return iamSummary(raw, func(a roleSummaryArgs) string {
		out := "Create " + a.role() + ", " + iamTrustText(a.Trust)
		if a.Boundary != "" {
			out += "; permissions boundary " + iamSummaryPolicy(a.Boundary)
		}
		return out
	})
}

func roleUpdateSummary(raw json.RawMessage) string {
	return iamSummary(raw, func(a roleSummaryArgs) string {
		var set []string
		if a.Description != nil {
			set = append(set, "description")
		}
		if a.Session != "" {
			set = append(set, "maximum session duration to "+summaryClip(a.Session.String(), 10)+" seconds")
		}
		if len(set) == 0 {
			return "Update " + a.role() + " with no change given"
		}
		return "Set the " + strings.Join(set, " and the ") + " of " + a.role()
	})
}

func roleTrustSummary(raw json.RawMessage) string {
	return iamSummary(raw, func(a roleSummaryArgs) string {
		return "Replace the trust policy of " + a.role() + ", which decides who may assume it: " + iamTrustText(a.Trust)
	})
}

func roleBoundarySummary(raw json.RawMessage) string {
	return iamSummary(raw, func(a roleSummaryArgs) string {
		return "Set the permissions boundary of " + a.role() + " to " + iamSummaryPolicy(a.Boundary) + ", replacing any current boundary"
	})
}

func roleBoundaryRemoveSummary(raw json.RawMessage) string {
	return iamSummary(raw, func(a roleSummaryArgs) string {
		return "Remove the permissions boundary of " + a.role() + ": the role may afterwards do more, everything its permission policies allow"
	})
}

func iamTagKeys(keys []string) string {
	shown := make([]string, 0, roleSummaryTagKeys)
	for _, k := range keys[:min(len(keys), roleSummaryTagKeys)] {
		shown = append(shown, summaryClip(k, iamSummaryTagKeyRunes))
	}
	out := strings.Join(shown, ", ")
	if more := len(keys) - len(shown); more > 0 {
		out += " and " + strconv.Itoa(more) + " more"
	}
	return out
}

func roleTagSummary(raw json.RawMessage) string {
	return iamSummary(raw, func(a roleSummaryArgs) string {
		keys := make([]string, 0, len(a.Tags))
		for _, t := range a.Tags {
			keys = append(keys, t.Key)
		}
		return "Add or overwrite " + strconv.Itoa(len(keys)) + " tag(s) on " + a.role() + " (keys " + iamTagKeys(keys) +
			"); tags can grant access through policy conditions"
	})
}

func roleUntagSummary(raw json.RawMessage) string {
	return iamSummary(raw, func(a roleSummaryArgs) string {
		return "Remove " + strconv.Itoa(len(a.TagKeys)) + " tag(s) from " + a.role() + " (keys " + iamTagKeys(a.TagKeys) + ")"
	})
}

func roleDeleteSummary(raw json.RawMessage) string {
	return iamSummary(raw, func(a roleSummaryArgs) string {
		return "Delete " + a.role() + ", which cannot be undone"
	})
}

func profileCreateSummary(raw json.RawMessage) string {
	return iamSummary(raw, func(a roleSummaryArgs) string {
		return "Create " + a.profile() + " holding no role yet"
	})
}

func profileDeleteSummary(raw json.RawMessage) string {
	return iamSummary(raw, func(a roleSummaryArgs) string {
		return "Delete " + a.profile() + ", which cannot be undone"
	})
}

func profileAddRoleSummary(raw json.RawMessage) string {
	return iamSummary(raw, func(a roleSummaryArgs) string {
		return "Add " + a.role() + " to " + a.profile() + ": EC2 instances started with the profile receive the role's credentials"
	})
}

func profileRemoveRoleSummary(raw json.RawMessage) string {
	return iamSummary(raw, func(a roleSummaryArgs) string {
		return "Remove " + a.role() + " from " + a.profile() + ": instances that use the profile lose the role's credentials"
	})
}
