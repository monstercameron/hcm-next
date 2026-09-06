package admission

import (
	"errors"
	"reflect"
	"testing"
)

func base() (Request, Snapshot) {
	return Request{TenantID: "tenant-a", CellID: "cell-1", PlacementEpoch: 7, Criticality: P2, EstimatedCost: 10, RetryBudgetID: "retry-1"}, Snapshot{
		TenantID: "tenant-a", CellID: "cell-1", PlacementEpoch: 7, Quota: Quota{Known: true, Version: "q3", Limit: 100}, Capacity: 100, RetryRemaining: 3,
	}
}

func TestTodo_ADMISSION_001(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Request, *Snapshot)
		want   Outcome
	}{
		{"healthy", func(*Request, *Snapshot) {}, Admit},
		{"unknown quota", func(_ *Request, s *Snapshot) { s.Quota.Known = false }, Defer},
		{"stale placement", func(_ *Request, s *Snapshot) { s.PlacementEpoch = 6 }, Defer},
		{"exhausted quota p2", func(_ *Request, s *Snapshot) { s.Quota.Consumed = 95 }, Queue},
		{"exhausted capacity p3", func(r *Request, s *Snapshot) { r.Criticality = P3; s.Capacity = 5 }, Defer},
		{"noisy p4", func(r *Request, s *Snapshot) { r.Criticality = P4; s.NoisyTenant = true }, Reject},
		{"p0 reservation", func(r *Request, s *Snapshot) { r.Criticality = P0; s.Capacity = 10; s.ReservedP0 = 10 }, Admit},
		{"p0 cannot displace capacity", func(r *Request, s *Snapshot) { r.Criticality = P0; s.Capacity = 5; s.ReservedP0 = 5 }, Defer},
		{"retry exhausted", func(r *Request, s *Snapshot) { r.RetryAttempt = 1; s.RetryRemaining = 0 }, Reject},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, s := base()
			tc.mutate(&r, &s)
			got := Decide(r, s, Policy{})
			if got.Outcome != tc.want {
				t.Fatalf("outcome = %s, want %s (reason %s)", got.Outcome, tc.want, got.Reason)
			}
			if err := got.Validate(); err != nil {
				t.Fatal(err)
			}
			again := Decide(r, s, Policy{})
			if !reflect.DeepEqual(got, again) {
				t.Fatalf("decision is not deterministic:\n%+v\n%+v", got, again)
			}
		})
	}
}

func TestTodo_ADMISSION_001_Golden(t *testing.T) {
	r, s := base()
	got := Decide(r, s, Policy{QueueRetryAfter: 9, DeferRetryAfter: 19, DegradeRetryAfter: 4})
	if got.DecisionID != "adm_63e5a7eda109555728fa6e71751e6a607cd7585c9bc16ca1825b8500eda751c3" {
		t.Fatalf("decision id = %q", got.DecisionID)
	}
	if got.Evidence.TenantID != "tenant-a" || got.Evidence.CellID != "cell-1" || got.Evidence.Criticality != P2 {
		t.Fatalf("missing evidence: %+v", got.Evidence)
	}
}

func TestTodo_ADMISSION_001_Security(t *testing.T) {
	r, s := base()
	s.TenantID = "tenant-b"
	if got := Decide(r, s, Policy{}); got.Outcome != Reject || got.Reason != "TENANT_OR_CELL_MISMATCH" {
		t.Fatalf("cross-tenant result = %+v", got)
	}
	r.TenantID = ""
	if got := Decide(r, s, Policy{}); got.Outcome != Reject {
		t.Fatalf("empty tenant result = %+v", got)
	}
}

func TestTodo_ADMISSION_001_Race(t *testing.T) {
	r, s := base()
	want := Decide(r, s, Policy{})
	for i := 0; i < 100; i++ {
		if got := Decide(r, s, Policy{}); !reflect.DeepEqual(got, want) {
			t.Fatal("same snapshot changed decision")
		}
	}
}

func TestTodo_ADMISSION_001_Fault(t *testing.T) {
	r, s := base()
	s.Quota.Consumed = int(^uint(0) >> 1)
	s.Quota.Pending = 1
	if got := Decide(r, s, Policy{}); got.Outcome != Reject || got.Reason != "INVALID_CAPACITY_RESERVATION" {
		t.Fatalf("overflow snapshot = %+v", got)
	}
}

func BenchmarkTodo_ADMISSION_001(b *testing.B) {
	r, s := base()
	for i := 0; i < b.N; i++ {
		_ = Decide(r, s, Policy{})
	}
}

func TestAdmissionContractAndDecisionValidation(t *testing.T) {
	if Version() != 1 || Explain() == "" {
		t.Fatalf("contract metadata version=%d explain=%q", Version(), Explain())
	}
	valid := Decide(baseRequestForValidation(), baseSnapshotForValidation(), Policy{})
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*Decision)
	}{
		{"missing id", func(d *Decision) { d.DecisionID = "" }},
		{"missing tenant", func(d *Decision) { d.TenantID = "" }},
		{"missing cell", func(d *Decision) { d.CellID = "" }},
		{"invalid criticality", func(d *Decision) { d.Criticality = "P9" }},
		{"missing outcome", func(d *Decision) { d.Outcome = "" }},
		{"missing reason", func(d *Decision) { d.Reason = "" }},
		{"negative retry", func(d *Decision) { d.RetryAfter = -1 }},
		{"negative reservation", func(d *Decision) { d.Reservation = -1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := valid
			tc.mutate(&got)
			if !errors.Is(got.Validate(), ErrInvalidInput) {
				t.Fatalf("Validate=%v", got.Validate())
			}
		})
	}
}

func TestAdmissionDecisionSecurityBranches(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Request, *Snapshot)
		want   Outcome
		reason string
	}{
		{"invalid context", func(r *Request, _ *Snapshot) { r.EstimatedCost = 0 }, Reject, "INVALID_CONTEXT"},
		{"draining", func(_ *Request, s *Snapshot) { s.Draining = true }, Defer, "CELL_DRAINING"},
		{"p0 quota reservation", func(r *Request, s *Snapshot) { r.Criticality = P0; s.Quota.Consumed = 95 }, Defer, "P0_QUOTA_RESERVED"},
		{"p1 pressure", func(r *Request, s *Snapshot) { r.Criticality = P1; s.Capacity = 1 }, Degrade, "PRESSURE_OR_NOISY_TENANT"},
		{"p3 pressure", func(r *Request, s *Snapshot) { r.Criticality = P3; s.Capacity = 1 }, Defer, "PRESSURE_OR_NOISY_TENANT"},
		{"p4 pressure", func(r *Request, s *Snapshot) { r.Criticality = P4; s.Capacity = 1 }, Reject, "BEST_EFFORT_SHED"},
		{"retry exhausted", func(r *Request, s *Snapshot) { r.RetryAttempt = 1; s.RetryRemaining = 0 }, Reject, "RETRY_BUDGET_EXHAUSTED"},
		{"invalid capacity", func(_ *Request, s *Snapshot) { s.Capacity = -1 }, Reject, "INVALID_CAPACITY_RESERVATION"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, s := base()
			tc.mutate(&r, &s)
			got := Decide(r, s, Policy{QueueRetryAfter: 2, DeferRetryAfter: 3, DegradeRetryAfter: 4})
			if got.Outcome != tc.want || got.Reason != tc.reason {
				t.Fatalf("decision=%+v", got)
			}
			if err := got.Validate(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func baseRequestForValidation() Request   { r, _ := base(); return r }
func baseSnapshotForValidation() Snapshot { _, s := base(); return s }
