package approval

import (
	"context"
	"reflect"
	"testing"
)

// Naming another service account by editing the file alone leaves the connection open, the report reads both
// ends, and no direct save of a form approves it in passing, since no form shows the field.
func TestChangingTheServiceAccountIsAnOpenChange(t *testing.T) {
	cfg, v := fixture(t)
	set := func(account string) {
		conn := cfg.Connections["wiki-write"]
		conn.ServiceAccount = account
		cfg.Connections["wiki-write"] = conn
	}
	set("robot-one")
	if _, _, err := Approve(context.Background(), cfg, v, nil); err != nil {
		t.Fatal(err)
	}
	if report, err := Pending(cfg, v); err != nil || len(report.Open) != 0 {
		t.Fatalf("Pending() = %+v, %v, want nothing open", report, err)
	}
	for _, tc := range []struct{ account, after string }{{"robot-two", "robot-two"}, {"", "none"}} {
		set(tc.account)
		report, err := Pending(cfg, v)
		if err != nil {
			t.Fatal(err)
		}
		if got := openNames(report); !reflect.DeepEqual(got, []string{"wiki-write"}) {
			t.Fatalf("open after naming %q = %v", tc.account, got)
		}
		want := []FieldChange{{Field: FieldServiceAccount, Before: "robot-one", After: tc.after}}
		if got := report.Open[0].Fields; !reflect.DeepEqual(got, want) {
			t.Errorf("fields = %+v, want %+v", got, want)
		}
		if DirectApprovable(report.Open[0]) {
			t.Error("a change of the service account must never be approved by a direct save")
		}
	}
}
