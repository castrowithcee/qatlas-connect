package aws

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"
)

// The confirmation texts of the IAM policy write tools. The core bounds, redacts, and neutralizes the text; the
// limits here keep long arguments from pushing the effect and the document figures out of the 500 runes.
const (
	iamSummaryARNRunes    = 80
	iamSummaryNameRunes   = 40
	iamSummaryActionRunes = 30
	iamSummaryActions     = 3
	iamSummaryAccounts    = 3
	iamSummaryTagKeys     = 4
)

const iamSummaryImmediate = "takes effect at once for everything the policy is attached to"

func init() {
	for _, kind := range []string{"user", "group", "role"} {
		registerSummary("aws.iam.attached-policy.attach-to-"+kind, iamAttachSummary("Attach", "to", kind))
		registerSummary("aws.iam.attached-policy.detach-from-"+kind, iamAttachSummary("Detach", "from", kind))
		registerSummary("aws.iam.inline-policy.put-for-"+kind, iamInlineSummary(kind, true))
		registerSummary("aws.iam.inline-policy.delete-for-"+kind, iamInlineSummary(kind, false))
	}
	registerSummary("aws.iam.policy.create", iamPolicyCreateSummary)
	registerSummary("aws.iam.policy.delete", iamPolicyArnSummary("Delete IAM managed policy ", ""))
	registerSummary("aws.iam.policy-version.create", iamVersionCreateSummary)
	registerSummary("aws.iam.policy-version.set-default", iamVersionSummary("Make version %s the default of IAM managed policy ", "; it "+iamSummaryImmediate))
	registerSummary("aws.iam.policy-version.delete", iamVersionSummary("Delete version %s of IAM managed policy ", ""))
	registerSummary("aws.iam.policy.tag", iamTagSummary)
	registerSummary("aws.iam.policy.untag", iamUntagSummary)
	registerSummary("aws.iam.user.put-permissions-boundary", iamBoundarySummary)
	registerSummary("aws.iam.user.delete-permissions-boundary", iamBoundaryDeleteSummary)
}

type iamSummaryArgs struct {
	User, Group, Role string `json:"-"`
	PolicyARN         string `json:"policy_arn"`
	PolicyName        string `json:"policy_name"`
	PolicyDocument    string `json:"policy_document"`
	VersionID         string `json:"version_id"`
	SetAsDefault      bool   `json:"set_as_default"`
	Boundary          string `json:"permissions_boundary"`
	Tags              []struct {
		Key string `json:"key"`
	} `json:"tags"`
	TagKeys []string `json:"tag_keys"`
}

func iamSummaryDecode(raw json.RawMessage) (iamSummaryArgs, bool) {
	var a struct {
		iamSummaryArgs
		UserName  string `json:"user_name"`
		GroupName string `json:"group_name"`
		RoleName  string `json:"role_name"`
	}
	ok := json.Unmarshal(raw, &a) == nil
	a.User, a.Group, a.Role = a.UserName, a.GroupName, a.RoleName
	return a.iamSummaryArgs, ok
}

func (a iamSummaryArgs) identity(kind string) string {
	name := map[string]string{"user": a.User, "group": a.Group, "role": a.Role}[kind]
	return "IAM " + kind + " " + summaryClip(name, iamSummaryNameRunes)
}

// iamARNOwner says whose a policy ARN is: the AWS managed ones carry the account "aws".
func iamARNOwner(arn string) string {
	switch account := iamARNAccount(arn); account {
	case "aws":
		return " (AWS managed)"
	case "":
		return ""
	default:
		return " (account " + summaryClip(account, 20) + ")"
	}
}

func iamARNAccount(arn string) string {
	parts := strings.SplitN(arn, ":", 6)
	if len(parts) < 6 || parts[0] != "arn" {
		return ""
	}
	return parts[4]
}

func iamPolicyRef(arn string) string {
	return summaryClip(arn, iamSummaryARNRunes) + iamARNOwner(arn)
}

func iamAttachSummary(verb, prep, kind string) func(json.RawMessage) string {
	return func(raw json.RawMessage) string {
		a, ok := iamSummaryDecode(raw)
		if !ok {
			return ""
		}
		if verb == "Attach" {
			return "Attach managed policy " + iamPolicyRef(a.PolicyARN) + " to " + a.identity(kind) +
				"; it gains the permissions of the policy"
		}
		return "Detach managed policy " + iamPolicyRef(a.PolicyARN) + " from " + a.identity(kind) +
			"; it loses the permissions of the policy"
	}
}

func iamInlineSummary(kind string, put bool) func(json.RawMessage) string {
	return func(raw json.RawMessage) string {
		a, ok := iamSummaryDecode(raw)
		if !ok {
			return ""
		}
		name := summaryClip(a.PolicyName, iamSummaryNameRunes)
		if !put {
			return "Delete inline policy " + name + " of " + a.identity(kind)
		}
		return "Write inline policy " + name + " of " + a.identity(kind) + ", replacing one of that name" +
			iamDocumentSummary(a.PolicyDocument)
	}
}

func iamPolicyCreateSummary(raw json.RawMessage) string {
	a, ok := iamSummaryDecode(raw)
	if !ok {
		return ""
	}
	return "Create IAM managed policy " + summaryClip(a.PolicyName, iamSummaryNameRunes) +
		", which grants nothing until attached" + iamDocumentSummary(a.PolicyDocument)
}

func iamPolicyArnSummary(prefix, suffix string) func(json.RawMessage) string {
	return func(raw json.RawMessage) string {
		a, ok := iamSummaryDecode(raw)
		if !ok {
			return ""
		}
		return prefix + iamPolicyRef(a.PolicyARN) + suffix
	}
}

func iamVersionSummary(format, suffix string) func(json.RawMessage) string {
	return func(raw json.RawMessage) string {
		a, ok := iamSummaryDecode(raw)
		if !ok {
			return ""
		}
		return strings.Replace(format, "%s", summaryClip(a.VersionID, 12), 1) + iamPolicyRef(a.PolicyARN) + suffix
	}
}

func iamVersionCreateSummary(raw json.RawMessage) string {
	a, ok := iamSummaryDecode(raw)
	if !ok {
		return ""
	}
	out := "Create a version of IAM managed policy " + iamPolicyRef(a.PolicyARN)
	if a.SetAsDefault {
		out += " and make it the default: it " + iamSummaryImmediate
	} else {
		out += ", not the default"
	}
	return out + iamDocumentSummary(a.PolicyDocument)
}

func iamTagSummary(raw json.RawMessage) string {
	a, ok := iamSummaryDecode(raw)
	if !ok {
		return ""
	}
	keys := make([]string, len(a.Tags))
	for i, t := range a.Tags {
		keys[i] = t.Key
	}
	return "Tag IAM managed policy " + iamPolicyRef(a.PolicyARN) + " with " + iamKeys(keys) +
		"; tags can grant access through policy conditions"
}

func iamUntagSummary(raw json.RawMessage) string {
	a, ok := iamSummaryDecode(raw)
	if !ok {
		return ""
	}
	return "Remove " + iamKeys(a.TagKeys) + " from IAM managed policy " + iamPolicyRef(a.PolicyARN)
}

func iamKeys(keys []string) string {
	shown := keys[:min(len(keys), iamSummaryTagKeys)]
	out := "tag keys "
	for i, k := range shown {
		if i > 0 {
			out += ", "
		}
		out += summaryClip(k, iamSummaryNameRunes)
	}
	if more := len(keys) - len(shown); more > 0 {
		out += " +" + strconv.Itoa(more) + " more"
	}
	return out
}

func iamBoundarySummary(raw json.RawMessage) string {
	a, ok := iamSummaryDecode(raw)
	if !ok {
		return ""
	}
	return "Set the permissions boundary of " + a.identity("user") + " to " + iamPolicyRef(a.Boundary)
}

func iamBoundaryDeleteSummary(raw json.RawMessage) string {
	a, ok := iamSummaryDecode(raw)
	if !ok {
		return ""
	}
	return "Remove the permissions boundary of " + a.identity("user") + ", which widens what it may do"
}

// iamDocumentSummary counts the statements of an identity policy and marks the wide ones. It reads best effort:
// the document check refuses what it cannot read, so an unreadable one gives no figures. A statement counts as
// wide only when it is not a Deny, and a NotAction or NotResource counts as a wildcard since it names all but a few.
func iamDocumentSummary(doc string) string {
	if len(doc) > maxIdentityPolicyChars {
		return ""
	}
	var d struct {
		Statement json.RawMessage
	}
	if json.Unmarshal([]byte(doc), &d) != nil {
		return ""
	}
	var statements []map[string]json.RawMessage
	if json.Unmarshal(d.Statement, &statements) != nil {
		var one map[string]json.RawMessage
		if json.Unmarshal(d.Statement, &one) != nil {
			return ""
		}
		statements = append(statements, one)
	}
	var wideAction, wideResource, deny int
	var actions, accounts []string
	for _, s := range statements {
		if effect, _ := iamStrings(s["Effect"]); len(effect) == 1 && effect[0] == "Deny" {
			deny++
			continue
		}
		if a, wide := iamWide(s, "Action"); wide {
			wideAction++
			actions = append(actions, a...)
		}
		if _, wide := iamWide(s, "Resource"); wide {
			wideResource++
		}
		for _, k := range []string{"Resource", "NotResource"} {
			resources, _ := iamStrings(s[k])
			for _, r := range resources {
				if account := iamARNAccount(r); account != "" && account != "aws" && !slices.Contains(accounts, account) {
					accounts = append(accounts, account)
				}
			}
		}
	}
	out := ": " + strconv.Itoa(len(statements)) + " statements"
	if deny > 0 {
		out += " (" + strconv.Itoa(deny) + " Deny)"
	}
	out += ", " + strconv.Itoa(wideAction) + " allow with wildcard Action"
	if wideAction > 0 {
		out += " [" + iamList(actions, iamSummaryActions, iamSummaryActionRunes) + "]"
	}
	out += ", " + strconv.Itoa(wideResource) + " allow with wildcard Resource"
	if len(accounts) > 0 {
		out += ", resource ARNs of accounts " + iamList(accounts, iamSummaryAccounts, 20)
	}
	return out
}

// iamStrings reads a JSON string or list of strings; other shapes give none.
func iamStrings(raw json.RawMessage) ([]string, bool) {
	var one string
	if json.Unmarshal(raw, &one) == nil {
		return []string{one}, true
	}
	var many []string
	err := json.Unmarshal(raw, &many)
	return many, err == nil
}

// iamWide reports the Action or Resource entries of a statement that hold "*", or all of a Not-element.
func iamWide(s map[string]json.RawMessage, element string) ([]string, bool) {
	if not, ok := s["Not"+element]; ok {
		v, _ := iamStrings(not)
		return []string{"Not" + element + " " + strings.Join(v, ",")}, true
	}
	var out []string
	v, _ := iamStrings(s[element])
	for _, e := range v {
		if strings.Contains(e, "*") {
			out = append(out, e)
		}
	}
	return out, len(out) > 0
}

func iamList(items []string, n, runes int) string {
	shown := items[:min(len(items), n)]
	out := make([]string, len(shown))
	for i, s := range shown {
		out[i] = summaryClip(s, runes)
	}
	text := strings.Join(out, ", ")
	if more := len(items) - len(shown); more > 0 {
		text += " +" + strconv.Itoa(more) + " more"
	}
	return text
}
