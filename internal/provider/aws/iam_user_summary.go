package aws

import (
	"encoding/json"
	"strconv"
	"strings"
)

// The confirmation texts of the IAM user and group write tools. Names, paths, and tag keys come from untrusted
// arguments, so each is cut; the texts stay well below the 500 runes the core allows.
const (
	userSummaryNameRunes   = 40
	userSummaryPathRunes   = 60
	userSummaryTagKeys     = 4
	userSummaryTagKeyRunes = 24
)

const userSummaryNoRename = "; policies and applications that name it by the old name or ARN no longer match"

func init() {
	registerSummary("aws.iam.user.update", userSummaryOf(userUpdateSummary))
	registerSummary("aws.iam.user.delete", userSummaryOf(func(a userSummaryArgs) string {
		return "Delete " + a.user() + "; this cannot be undone"
	}))
	registerSummary("aws.iam.user.tag", userSummaryOf(func(a userSummaryArgs) string {
		keys := make([]string, len(a.Tags))
		for i, t := range a.Tags {
			keys[i] = t.Key
		}
		return "Tag " + a.user() + " with " + userSummaryKeys(keys) + "; tags can grant access through policy conditions"
	}))
	registerSummary("aws.iam.user.untag", userSummaryOf(func(a userSummaryArgs) string {
		return "Remove " + userSummaryKeys(a.TagKeys) + " from " + a.user()
	}))
	registerSummary("aws.iam.group.create", userSummaryOf(func(a userSummaryArgs) string {
		out := "Create " + a.group()
		if a.Path != "" {
			out += " with path " + summaryClip(a.Path, userSummaryPathRunes)
		}
		return out
	}))
	registerSummary("aws.iam.group.update", userSummaryOf(userGroupUpdateSummary))
	registerSummary("aws.iam.group.delete", userSummaryOf(func(a userSummaryArgs) string {
		return "Delete " + a.group() + "; this cannot be undone"
	}))
	registerSummary("aws.iam.group.add-user", userSummaryOf(func(a userSummaryArgs) string {
		return "Add " + a.user() + " to " + a.group() + "; the user gains the permissions of the group"
	}))
	registerSummary("aws.iam.group.remove-user", userSummaryOf(func(a userSummaryArgs) string {
		return "Remove " + a.user() + " from " + a.group() + "; the user loses the permissions of the group"
	}))
}

type userSummaryArgs struct {
	UserName     string `json:"user_name"`
	NewUserName  string `json:"new_user_name"`
	GroupName    string `json:"group_name"`
	NewGroupName string `json:"new_group_name"`
	Path         string `json:"path"`
	NewPath      string `json:"new_path"`
	Tags         []struct {
		Key string `json:"key"`
	} `json:"tags"`
	TagKeys []string `json:"tag_keys"`
}

// userSummaryOf decodes the arguments for a text; arguments that do not decode give no text.
func userSummaryOf(f func(userSummaryArgs) string) func(json.RawMessage) string {
	return func(raw json.RawMessage) string {
		var a userSummaryArgs
		if json.Unmarshal(raw, &a) != nil {
			return ""
		}
		return f(a)
	}
}

func (a userSummaryArgs) user() string {
	return "IAM user " + summaryClip(a.UserName, userSummaryNameRunes)
}

func (a userSummaryArgs) group() string {
	return "IAM group " + summaryClip(a.GroupName, userSummaryNameRunes)
}

func userUpdateSummary(a userSummaryArgs) string {
	return userRenameSummary(a.user(), a.NewUserName, a.NewPath)
}

func userGroupUpdateSummary(a userSummaryArgs) string {
	return userRenameSummary(a.group(), a.NewGroupName, a.NewPath)
}

// userRenameSummary names what an update changes: the new name, the new path, or both.
func userRenameSummary(subject, newName, newPath string) string {
	var changes []string
	if newName != "" {
		changes = append(changes, "name to "+summaryClip(newName, userSummaryNameRunes))
	}
	if newPath != "" {
		changes = append(changes, "path to "+summaryClip(newPath, userSummaryPathRunes))
	}
	if len(changes) == 0 {
		return "Update " + subject + ", changing nothing"
	}
	out := "Change the " + strings.Join(changes, " and the ") + " of " + subject
	if newName != "" {
		out += userSummaryNoRename
	}
	return out
}

func userSummaryKeys(keys []string) string {
	shown := keys[:min(len(keys), userSummaryTagKeys)]
	out := make([]string, len(shown))
	for i, k := range shown {
		out[i] = summaryClip(k, userSummaryTagKeyRunes)
	}
	text := "tag keys " + strings.Join(out, ", ")
	if more := len(keys) - len(shown); more > 0 {
		text += " +" + strconv.Itoa(more) + " more"
	}
	return text
}
