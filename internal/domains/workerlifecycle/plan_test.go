package workerlifecycle

import (
	"errors"
	"testing"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func ref(kind, id string) values.EntityRef {
	return values.EntityRef{Tenant: "tenant-a", Kind: values.Kind(kind), Id: id}
}
func date(t *testing.T, s string) values.LocalDate {
	t.Helper()
	d, err := values.ParseLocalDate(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func fixture(t *testing.T) WorkerLifecyclePlan {
	t.Helper()
	cal := values.CalendarRef{Ref: "gregorian", Version: "1"}
	p, err := NewPlan(WorkerLifecyclePlan{Worker: ref("worker", "00000000-0000-4000-8000-000000000001"), Employment: ref("employment", "00000000-0000-4000-8000-000000000002"), Proposal: ref("proposal", "00000000-0000-4000-8000-000000000003"), Event: EventStart, EventDate: date(t, "2026-01-05"), Completion: CompleteAllRequired, Requirements: []Requirement{{ID: "identity", Ordinal: 1, Owner: "people", Due: DueRule{Calendar: cal}, Evidence: []values.EntityRef{ref("evidence", "00000000-0000-4000-8000-000000000011")}, VerificationPolicy: ref("evidence_policy", "00000000-0000-4000-8000-000000000021"), Completion: CompleteAllRequired, Required: true}, {ID: "training", Ordinal: 2, Owner: "learning", Due: DueRule{Calendar: cal, OffsetDays: 5}, Evidence: []values.EntityRef{ref("evidence", "00000000-0000-4000-8000-000000000012")}, VerificationPolicy: ref("evidence_policy", "00000000-0000-4000-8000-000000000022"), Completion: CompleteAllRequired, Required: true}}, Children: []ChildTemplate{{ID: "identity-child", Ordinal: 1, IntentType: "hcm.identity.verify", IntentVersion: "v1"}, {ID: "training-child", Ordinal: 2, IntentType: "hcm.learning.assign", IntentVersion: "v1", DependsOn: []string{"identity-child"}}}})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestWorkerLifecyclePlanRejectsUnownedCircularAndUnverifiableRequirements(t *testing.T) {
	p := fixture(t)
	p.Requirements[0].Owner = ""
	if _, err := NewPlan(p); !errors.Is(err, ErrRequirementInvalid) {
		t.Fatalf("unowned requirement: %v", err)
	}
	p = fixture(t)
	p.Children[0].DependsOn = []string{"training-child"}
	if _, err := NewPlan(p); !errors.Is(err, ErrChildGraph) {
		t.Fatalf("cycle: %v", err)
	}
	p = fixture(t)
	p.Requirements[0].Evidence = nil
	if _, err := NewPlan(p); !errors.Is(err, ErrRequirementInvalid) {
		t.Fatalf("unverifiable: %v", err)
	}
}

func TestTodo_WORKER_LIFE_001_Property(t *testing.T) {
	p := fixture(t)
	a, _ := p.Digest()
	b, _ := p.Digest()
	if a != b {
		t.Fatal("digest is not deterministic")
	}
	mutations := map[string]func(*WorkerLifecyclePlan){
		"worker":     func(x *WorkerLifecyclePlan) { x.Worker.Id = "00000000-0000-4000-8000-000000000099" },
		"employment": func(x *WorkerLifecyclePlan) { x.Employment.Id = "00000000-0000-4000-8000-000000000099" },
		"proposal":   func(x *WorkerLifecyclePlan) { x.Proposal.Id = "00000000-0000-4000-8000-000000000099" },
		"event":      func(x *WorkerLifecyclePlan) { x.Event = EventEnd },
		"date":       func(x *WorkerLifecyclePlan) { x.EventDate = date(t, "2027-02-20") },
		"calendar":   func(x *WorkerLifecyclePlan) { x.Requirements[0].Due.Calendar.Version = "2" },
		"due offset": func(x *WorkerLifecyclePlan) { x.Requirements[0].Due.OffsetDays = -9000 },
		"verification": func(x *WorkerLifecyclePlan) {
			x.Requirements[0].VerificationPolicy.Id = "00000000-0000-4000-8000-000000000099"
		},
		"child template": func(x *WorkerLifecyclePlan) { x.Children[0].IntentVersion = "v2" },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			x := fixture(t)
			mutate(&x)
			x.CanonicalDigest = ""
			changed, err := NewPlan(x)
			if err != nil {
				t.Fatal(err)
			}
			if changed.CanonicalDigest == a {
				t.Fatal("semantic mutation did not change digest")
			}
		})
	}
}
func TestTodo_WORKER_LIFE_001_Golden(t *testing.T) {
	p := fixture(t)
	if got, want := p.CanonicalDigest, "sha256:52a042ee4f0d8a75c180736fa2b66c1852d6cd7cd3beaa7c7742638df6143714"; got != want {
		t.Fatalf("digest=%q, want %q", got, want)
	}
}
func TestTodo_WORKER_LIFE_001_Security(t *testing.T) {
	p := fixture(t)
	p.Proposal.Tenant = "other"
	if _, err := NewPlan(p); !errors.Is(err, ErrInvalidPlan) {
		t.Fatalf("cross-tenant proposal: %v", err)
	}
}
func TestTodo_WORKER_LIFE_001_Conformance(t *testing.T) {
	p := fixture(t)
	if len(p.Children) != 2 || p.Children[1].DependsOn[0] != "identity-child" {
		t.Fatal("child ordering/dependency not retained")
	}
	bad := fixture(t)
	bad.Children[0].DependsOn = []string{"training-child"}
	if _, err := NewPlan(bad); !errors.Is(err, ErrChildGraph) {
		t.Fatalf("forward dependency: %v", err)
	}
	bad = fixture(t)
	bad.Requirements[1].Ordinal = 3
	if _, err := NewPlan(bad); !errors.Is(err, ErrRequirementInvalid) {
		t.Fatalf("requirement ordinal gap: %v", err)
	}
}
func TestTodo_WORKER_LIFE_001_Mutation(t *testing.T) {
	p := fixture(t)
	original := p.Requirements[0].Evidence[0]
	input := fixture(t)
	input.Requirements[0].Evidence[0].Id = "changed"
	if p.Requirements[0].Evidence[0] != original {
		t.Fatal("fixture unexpectedly shared")
	}
	input.Children[1].DependsOn[0] = "changed"
	if p.Children[1].DependsOn[0] != "identity-child" {
		t.Fatal("child dependency unexpectedly shared")
	}
	p.CanonicalDigest = "tampered"
	if _, err := p.Digest(); !errors.Is(err, ErrPlanMutation) {
		t.Fatalf("tampered plan: %v", err)
	}
	approvedInput := fixture(t)
	approvedInput.CanonicalDigest = ""
	approvedInput.ApprovalDecision = ref("approval_decision", "00000000-0000-4000-8000-000000000031")
	approved, err := NewPlan(approvedInput)
	if err != nil {
		t.Fatal(err)
	}
	approved.Requirements[0].Owner = "another-owner"
	if _, err := approved.Digest(); !errors.Is(err, ErrPlanMutation) {
		t.Fatalf("approved plan mutation: %v", err)
	}
	if _, err := NewPlan(approved); !errors.Is(err, ErrPlanMutation) {
		t.Fatalf("constructor blessed sealed plan mutation: %v", err)
	}
}
