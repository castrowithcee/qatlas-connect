package googlecloud

import (
	"slices"
	"strings"
	"testing"
)

func TestParseTarget(t *testing.T) {
	valid := map[string]target{
		"organizations/123456789":              {kindOrganization, "123456789"},
		"folders/2001":                         {kindFolder, "2001"},
		"projects/my-project-123":              {kindProject, "my-project-123"},
		"projects/415104041262":                {kindProject, "415104041262"},
		"billingAccounts/012345-ABCDEF-678901": {kindBilling, "012345-ABCDEF-678901"},
	}
	for raw, want := range valid {
		if got, err := parseTarget(raw); err != nil || got != want {
			t.Errorf("parseTarget(%q) = %+v, %v; want %+v", raw, got, err, want)
		}
	}
	for _, raw := range []string{"", "*", "organizations/", "organizations/abc", "organizations/*", "folders/12/34",
		"folders/../1", "projects/My-Project", "projects/a", "projects/-bad-id1", "projects/bad-id-", "projects/x y z",
		"projects/my-project-123/", "billingAccounts/12345", "billingAccounts/012345-abcdef-678901", "billing/012345-ABCDEF-678901",
		"Organizations/1", "projects/my-project-123?x=1", "folders/" + strings.Repeat("1", 21)} {
		_, err := parseTarget(raw)
		if err == nil {
			t.Errorf("parseTarget(%q) accepted", raw)
		} else if raw != "" && len(raw) > 3 && strings.Contains(err.Error(), raw) {
			t.Errorf("error %q quotes the value", err)
		}
	}
}

func TestTargetListRefusesDuplicatesAndBadEntries(t *testing.T) {
	if _, err := parseAllowlist([]string{"folders/1", "folders/1"}); err == nil {
		t.Error("a duplicate target was accepted")
	}
	if err := targetMetadata.ValidateSet([]string{"folders/1", "projects/BAD"}); err == nil {
		t.Error("a bad entry was accepted")
	}
	if err := targetMetadata.ValidateSet([]string{"organizations/1", "folders/2", "projects/my-project-123", "billingAccounts/012345-ABCDEF-678901"}); err != nil {
		t.Errorf("a mix of every kind was refused: %v", err)
	}
}

func TestTargetKindsNestAsGoogleNests(t *testing.T) {
	if err := targetMetadata.ValidateKindParents(); err != nil {
		t.Fatal(err)
	}
	folder, _ := targetMetadata.KindByName(kindFolder)
	project, _ := targetMetadata.KindByName(kindProject)
	billing, _ := targetMetadata.KindByName(kindBilling)
	if !folder.Nested || !slices.Equal(folder.Parents, []string{kindOrganization}) ||
		!slices.Contains(project.Parents, kindFolder) || !slices.Contains(project.Parents, kindOrganization) ||
		len(billing.Parents) != 0 || billing.Nested {
		t.Errorf("kinds = %+v %+v %+v", folder, project, billing)
	}
	for kind, target := range map[string]string{kindOrganization: "organizations/1", kindFolder: "folders/1",
		kindProject: "projects/my-project-123", kindBilling: "billingAccounts/012345-ABCDEF-678901"} {
		if !targetMetadata.ValidTarget(kind, target) {
			t.Errorf("%s is not a valid %s target", target, kind)
		}
	}
}

func TestBillingAccountsAreReachedOnlyByAConnectionThatNamesThem(t *testing.T) {
	const account = "012345-ABCDEF-678901"
	for name, targets := range map[string][]string{
		"unbound":         nil,
		"organization":    {"organizations/1"},
		"folder":          {"folders/1"},
		"project":         {"projects/my-project-123"},
		"another account": {"billingAccounts/AAAAAA-BBBBBB-CCCCCC"},
	} {
		list, err := parseAllowlist(targets)
		if err != nil || list.allowsBillingAccount(account) {
			t.Errorf("%s: err=%v, reaches the billing account", name, err)
		}
	}
	list, _ := parseAllowlist([]string{"organizations/1", "billingAccounts/" + account})
	if !list.allowsBillingAccount(account) {
		t.Error("a connection that names the account does not reach it")
	}
}

func TestDecidesProjectBeforeAnyRead(t *testing.T) {
	cases := []struct {
		name             string
		targets          []string
		project          string
		allowed, decided bool
	}{
		{"unbound", nil, "my-project-123", true, true},
		{"same ID", []string{"projects/my-project-123"}, "my-project-123", true, true},
		{"other ID", []string{"projects/my-project-123"}, "other-project", false, true},
		{"other number", []string{"projects/415104041262"}, "999", false, true},
		{"ID and number differ in spelling", []string{"projects/415104041262"}, "my-project-123", false, false},
		{"folder may hold it", []string{"folders/1"}, "my-project-123", false, false},
		{"organization may hold it", []string{"organizations/1", "projects/other-project"}, "my-project-123", false, false},
		{"billing account alone", []string{"billingAccounts/012345-ABCDEF-678901"}, "my-project-123", false, true},
	}
	for _, c := range cases {
		list, err := parseAllowlist(c.targets)
		if err != nil {
			t.Fatal(err)
		}
		if allowed, decided := list.decidesProject(c.project); allowed != c.allowed || decided != c.decided {
			t.Errorf("%s: allowed=%v decided=%v, want %v %v", c.name, allowed, decided, c.allowed, c.decided)
		}
	}
}

func TestTargetsDecideWhichToolsAreReached(t *testing.T) {
	cases := []struct {
		tool    string
		targets []string
		reached bool
	}{
		{projectsGetID, nil, true},
		{projectsGetID, []string{"projects/my-project-123"}, true},
		{projectsGetID, []string{"folders/1"}, true},
		{projectsGetID, []string{"billingAccounts/012345-ABCDEF-678901"}, false},
		{projectsSearchID, nil, true},
		{projectsSearchID, []string{"projects/my-project-123"}, true},
		{projectsSearchID, []string{"billingAccounts/012345-ABCDEF-678901"}, false},
		{foldersSearchID, nil, true},
		{foldersSearchID, []string{"folders/1"}, true},
		{foldersSearchID, []string{"organizations/1", "projects/my-project-123"}, true},
		{foldersSearchID, []string{"projects/my-project-123"}, false},
		{foldersSearchID, []string{"billingAccounts/012345-ABCDEF-678901"}, false},
		{organizationsSearchID, nil, false},
		{organizationsSearchID, []string{"folders/1"}, false},
		{organizationsSearchID, []string{"projects/my-project-123"}, false},
		{organizationsSearchID, []string{"organizations/1"}, true},
		{organizationsSearchID, []string{"organizations/1", "projects/my-project-123"}, true},
		{organizationsSearchID, []string{"projects/BAD"}, true},
		{operationsGetID, nil, true},
		{operationsGetID, []string{"organizations/1", "projects/my-project-123"}, true},
		{operationsGetID, []string{"folders/1"}, false},
		{operationsGetID, []string{"projects/my-project-123"}, false},
		{operationsGetID, []string{"projects/BAD"}, true},
	}
	for _, c := range cases {
		reached, needs := targetMetadata.Reach(c.tool, c.targets)
		if reached != c.reached || (!reached && needs == "") {
			t.Errorf("Reach(%s, %v) = %v, %q; want %v with a hint", c.tool, c.targets, reached, needs, c.reached)
		}
	}
}
