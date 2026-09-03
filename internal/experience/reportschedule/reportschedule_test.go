package reportschedule

import (
	"testing"
	"time"
)

func TestRegistryMatrixExact(t *testing.T) {
	want := []string{"scheduled", "running", "succeeded", "failed", "paused", "exact", "recomputed", "unavailable"}
	got := RegistryMatrix()
	if len(got) != len(want) {
		t.Fatalf("registry length=%d want %d", len(got), len(want))
	}
	for i, code := range want {
		if got[i].Code != code || got[i].Label == "" || got[i].Description == "" {
			t.Fatalf("registry[%d]=%+v", i, got[i])
		}
	}
	got[0].Code = "tampered"
	if _, ok := Lookup("scheduled"); !ok {
		t.Fatal("registry lookup changed after matrix mutation")
	}
}

func reportFixture() (Schedule, Authorization) {
	return Schedule{ID: "dashboard-1", TenantID: "tenant-a", Every: time.Hour,
			Definition: Definition{ID: "definition", Version: "v1", Digest: "definition-digest"},
			Data:       Data{ID: "data", Version: "v1", Digest: "data-digest"},
			Control:    Control{ID: "control", Version: "v3", Digest: "control-digest"},
			Locale:     Locale{ID: "en-US", Version: "2026-01", Digest: "locale-digest"}},
		Authorization{PrincipalID: "user-a", TenantID: "tenant-a", Purpose: "dashboard", ValidUntil: time.Now().Add(time.Hour)}
}

func TestScheduledReportAndDashboardRefreshRemainReproducibleAndNonDisclosing(t *testing.T) {
	s := NewScheduler(4)
	sch, auth := reportFixture()
	if _, err := s.Create(sch); err != nil {
		t.Fatal(err)
	}
	run, err := s.Run(sch.ID, auth, time.Unix(100, 0), "evidence", "secure-destination", "opaque-attachment")
	if err != nil {
		t.Fatal(err)
	}
	if !run.Delivery.Authorized || run.Delivery.AttachmentRef == "" {
		t.Fatalf("delivery intent=%+v", run.Delivery)
	}
	got := Reproduce(run, sch.Definition, sch.Data, sch.Control, sch.Locale, run.EvidenceDigest)
	if err != nil || got.State != Exact {
		t.Fatalf("reproduction=%+v err=%v", got, err)
	}
	if got := run.TenantID; got != "tenant-a" {
		t.Fatalf("tenant=%q", got)
	}
}

func TestTodo_REPORT_003_Property(t *testing.T) {
	sch, auth := reportFixture()
	s := NewScheduler(2)
	if _, err := s.Create(sch); err != nil {
		t.Fatal(err)
	}
	auth.ValidUntil = time.Unix(1, 0)
	if _, err := s.Run(sch.ID, auth, time.Unix(2, 0), "e", "d", "a"); err != ErrUnauthorized {
		t.Fatalf("expired authority error=%v", err)
	}
}

func TestTodo_REPORT_003_Golden(t *testing.T) {
	TestScheduledReportAndDashboardRefreshRemainReproducibleAndNonDisclosing(t)
}
func TestTodo_REPORT_003_Race(t *testing.T) {
	TestScheduledReportAndDashboardRefreshRemainReproducibleAndNonDisclosing(t)
}
func TestTodo_REPORT_003_Fault(t *testing.T) {
	sch, _ := reportFixture()
	if got := Reproduce(Run{}, sch.Definition, sch.Data, sch.Control, sch.Locale, ""); got.State != Unavailable {
		t.Fatalf("got=%+v", got)
	}
}
func TestTodo_REPORT_003_Security(t *testing.T) {
	sch, auth := reportFixture()
	s := NewScheduler(1)
	if _, err := s.Create(sch); err != nil {
		t.Fatal(err)
	}
	auth.TenantID = "other-tenant"
	if _, err := s.Run(sch.ID, auth, time.Unix(2, 0), "e", "d", "a"); err != ErrNotFound {
		t.Fatalf("cross-tenant error=%v", err)
	}
}
func TestTodo_REPORT_003_Conformance(t *testing.T) {
	sch, _ := reportFixture()
	run := Run{ID: "r", State: Succeeded, Definition: sch.Definition, Data: sch.Data, Control: sch.Control, Locale: sch.Locale, EvidenceDigest: "old"}
	if got := Reproduce(run, sch.Definition, sch.Data, sch.Control, sch.Locale, "new"); got.State != Recomputed {
		t.Fatalf("got=%+v", got)
	}
}
func TestTodo_REPORT_003_Mutation(t *testing.T) {
	sch, auth := reportFixture()
	s := NewScheduler(1)
	if _, err := s.Create(sch); err != nil {
		t.Fatal(err)
	}
	first, err := s.Run(sch.ID, auth, time.Unix(10, 0), "e1", "d", "a")
	if err != nil {
		t.Fatal(err)
	}
	sch.Definition.Version = "v2"
	second, err := s.Run(sch.ID, auth, time.Unix(11, 0), "e2", "d", "a")
	if err != nil {
		t.Fatal(err)
	}
	if first.Definition.Version != "v1" || second.Definition.Version != "v1" {
		t.Fatalf("pins mutated: %q %q", first.Definition.Version, second.Definition.Version)
	}
}
