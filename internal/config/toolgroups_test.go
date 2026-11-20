package config

import (
	"reflect"
	"testing"
)

func TestGroupToolsAndGroupOfEntry(t *testing.T) {
	metadata := ProviderMetadata{ID: "demo", Tools: []ToolMetadata{
		{ID: "demo.task.get"}, {ID: "demo.task.add"}, {ID: "demo.project.list"},
	}}
	g := GroupTools(metadata, []string{"demo.task.get", "demo.project.list", "demo.gone.x"}, "Gone")
	if g == nil || len(g.Groups) != 3 || g.Groups[0].ID != "project" || g.Groups[1].ID != "task" || g.Groups[2].Title != "Gone" {
		t.Fatalf("groups = %+v", g)
	}
	if !reflect.DeepEqual(g.Members[UnlistedToolGroup], []string{"demo.gone.x"}) || g.Of["demo.task.get"] != "task" {
		t.Errorf("members = %v, of = %v", g.Members, g.Of)
	}
	if got := GroupTools(ProviderMetadata{ID: "demo", Tools: []ToolMetadata{{ID: "demo.task.get"}, {ID: "demo.task.add"}}}, nil, ""); got != nil {
		t.Errorf("one group = %+v, want nil", got)
	}
	for entry, want := range map[string]string{"demo.task.add": "task", "demo.project.*": "project"} {
		if group, ok := GroupOfEntry(metadata, entry); !ok || group.ID != want {
			t.Errorf("GroupOfEntry(%s) = %v, %v, want %s", entry, group, ok, want)
		}
	}
	if _, ok := GroupOfEntry(metadata, "demo.nothing.get"); ok {
		t.Error("an unregistered tool has a group")
	}
}
