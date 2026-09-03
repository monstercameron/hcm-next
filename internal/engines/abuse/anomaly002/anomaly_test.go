package anomaly002_test

import (
	"errors"
	"testing"

	"github.com/monstercameron/hcm-next/internal/engines/abuse/anomaly002"
)

func definition() anomaly002.Definition {
	return anomaly002.Definition{ID: "privileged-activity", Version: "7", Owner: "security", MaxVolume: 100, MaxScope: 5, AllowedHours: [2]int{6, 20}, AllowedLocations: []string{"office", "vpn"}}
}

func activity() anomaly002.Activity {
	return anomaly002.Activity{ID: "a-1", Actor: "operator-1", Tenant: "tenant-a", Resource: "sensitive-record", Location: "vpn", Hour: 10, Volume: 2, Scope: 1, Sequence: 1, Sensitive: true, Version: "source-3"}
}

func TestTodo_ABUSE_002(t *testing.T) {
	cases := []struct {
		name, field string
		mutate      func(*anomaly002.Activity)
	}{
		{"unusual time", "time", func(a *anomaly002.Activity) { a.Hour = 2 }},
		{"unusual location", "location", func(a *anomaly002.Activity) { a.Location = "unknown" }},
		{"unusual volume", "volume", func(a *anomaly002.Activity) { a.Volume = 101 }},
		{"unusual scope", "scope", func(a *anomaly002.Activity) { a.Scope = 6 }},
		{"unusual sequence", "sequence", func(a *anomaly002.Activity) { a.Sequence = 2 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := activity()
			tc.mutate(&a)
			got, err := anomaly002.Detect(definition(), []anomaly002.Activity{a})
			if err != nil {
				t.Fatal(err)
			}
			if got.Code != anomaly002.RejectedCode || got.Finding == nil {
				t.Fatalf("result=%+v, want rejection", got)
			}
			if got.Finding.OffendingField != tc.field || got.Finding.State == "" || got.Finding.Version != "7" {
				t.Fatalf("finding=%+v", got.Finding)
			}
		})
	}
	approved := activity()
	approved.Hour = 2
	approved.Volume = 1000
	approved.ApprovedWork = true
	got, err := anomaly002.Detect(definition(), []anomaly002.Activity{approved})
	if err != nil || got.Code != anomaly002.AcceptedCode {
		t.Fatalf("approved work result=%+v err=%v", got, err)
	}
	incident := activity()
	incident.Location = "unknown"
	incident.IncidentWork = true
	got, err = anomaly002.Detect(definition(), []anomaly002.Activity{incident})
	if err != nil || got.Code != anomaly002.AcceptedCode {
		t.Fatalf("incident work result=%+v err=%v", got, err)
	}
}

func TestTodo_ABUSE_002_Security(t *testing.T) {
	a := activity()
	a.Privileged = false
	a.Sensitive = false
	if _, err := anomaly002.Detect(definition(), []anomaly002.Activity{a}); !errors.Is(err, anomaly002.ErrActivity) {
		t.Fatalf("unclassified activity accepted: %v", err)
	}
	bad := definition()
	bad.AllowedLocations = nil
	if _, err := anomaly002.Detect(bad, []anomaly002.Activity{activity()}); !errors.Is(err, anomaly002.ErrDefinition) {
		t.Fatalf("unscoped detector accepted: %v", err)
	}
}

func TestTodo_ABUSE_002_Mutation(t *testing.T) {
	base, err := anomaly002.Detect(definition(), []anomaly002.Activity{activity()})
	if err != nil {
		t.Fatal(err)
	}
	mut := activity()
	mut.Volume = 101
	changed, err := anomaly002.Detect(definition(), []anomaly002.Activity{mut})
	if err != nil {
		t.Fatal(err)
	}
	if base.Code == changed.Code {
		t.Fatalf("volume threshold mutation did not change result: %+v -> %+v", base, changed)
	}
	mut = activity()
	mut.Version = "source-4"
	// Source versions are carried as evidence and do not alter detector policy.
	stable, err := anomaly002.Detect(definition(), []anomaly002.Activity{mut})
	if err != nil {
		t.Fatal(err)
	}
	if stable.Code != base.Code {
		t.Fatalf("irrelevant source version changed result: %s -> %s", base.Code, stable.Code)
	}
}
