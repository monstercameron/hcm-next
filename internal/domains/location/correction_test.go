package location

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/decision"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func correctionAuthority() decision.Decision {
	return decision.Decision{State: decision.Allow, ProposalRevisionDigest: "sha256:proposal", Digest: "sha256:decision"}
}

func correctionPeriod(t *testing.T, start, end int) values.EffectiveInterval {
	t.Helper()
	a, err := values.NewLocalDate(2026, time.January, start)
	if err != nil {
		t.Fatal(err)
	}
	b, err := values.NewLocalDate(2026, time.January, end)
	if err != nil {
		t.Fatal(err)
	}
	interval, err := values.NewLocalDateInterval(a, b, values.CalendarRef{Ref: "us-federal", Version: "2026"})
	if err != nil {
		t.Fatal(err)
	}
	return interval
}

func correctionLocation(t *testing.T) WorkLocationRevision {
	t.Helper()
	a := validAddress(t)
	w, err := NewWorkLocationRevision(WorkLocationRevision{LocationID: "location:correction", Revision: 1, Address: a, SourceAuthority: "registry/v1", Confidence: ConfidenceHigh, Effective: correctionPeriod(t, 1, 20), KnownAt: knownAt(t)})
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func correctionDependencies(t *testing.T) []DependentPeriod {
	t.Helper()
	period := correctionPeriod(t, 5, 10)
	return []DependentPeriod{
		{Kind: ImpactLegal, SubjectRef: "legal:worker-1", Period: period, RuleRelease: "legal-2026.1"},
		{Kind: ImpactTax, SubjectRef: "tax:worker-1", Period: period, RuleRelease: "tax-2026.1"},
		{Kind: ImpactPayroll, SubjectRef: "payroll:run-1", Period: period, RuleRelease: "payroll-2026.1"},
		{Kind: ImpactLeave, SubjectRef: "leave:worker-1", Period: period, RuleRelease: "leave-2026.1"},
		{Kind: ImpactSchedule, SubjectRef: "schedule:worker-1", Period: period, RuleRelease: "schedule-2026.1"},
		{Kind: ImpactTax, SubjectRef: "tax:unaffected", Period: correctionPeriod(t, 20, 25), RuleRelease: "tax-2026.1"},
	}
}

func TestMaterialLocationChangeEmitsExactLegalTaxPayrollLeaveAndScheduleImpacts(t *testing.T) {
	current := correctionLocation(t)
	plan, err := CorrectWorkLocation(WorkLocationCorrectionRequest{Current: current, Replacement: WorkLocationRevision{Address: validAddress(t), SourceAuthority: "correction/v1", Confidence: ConfidenceAuthoritative, Effective: correctionPeriod(t, 5, 15), KnownAt: knownAt(t)}, Authority: correctionAuthority(), Dependencies: correctionDependencies(t)})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Successor.Revision != 2 || plan.Successor.ParentDigest != current.CanonicalDigest || len(plan.Impacts) != 5 {
		t.Fatalf("correction plan = %+v", plan)
	}
	seen := map[ImpactKind]bool{}
	for _, impact := range plan.Impacts {
		seen[impact.Kind] = true
		if impact.LocationRevisionDigest != plan.Successor.CanonicalDigest || impact.RuleRelease == "" {
			t.Fatalf("unbound impact = %+v", impact)
		}
	}
	for _, kind := range []ImpactKind{ImpactLegal, ImpactTax, ImpactPayroll, ImpactLeave, ImpactSchedule} {
		if !seen[kind] {
			t.Fatalf("missing %s impact: %+v", kind, plan.Impacts)
		}
	}
	if plan.CanonicalDigest == "" {
		t.Fatal("correction plan has no digest")
	}
}

func TestTodo_LOCATION_003_Property(t *testing.T) {
	current := correctionLocation(t)
	plan, err := CorrectWorkLocation(WorkLocationCorrectionRequest{Current: current, Replacement: WorkLocationRevision{Address: validAddress(t), SourceAuthority: "correction/v1", Confidence: ConfidenceAuthoritative, Effective: current.Effective, KnownAt: knownAt(t)}, Authority: correctionAuthority(), Dependencies: correctionDependencies(t)})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Previous.CanonicalDigest != current.CanonicalDigest || plan.Successor.ParentDigest != current.CanonicalDigest {
		t.Fatal("correction overwrote or detached history")
	}
}

func TestTodo_LOCATION_003_Golden(t *testing.T) {
	current := correctionLocation(t)
	plan, err := CorrectWorkLocation(WorkLocationCorrectionRequest{Current: current, Replacement: WorkLocationRevision{Address: validAddress(t), SourceAuthority: "correction/v1", Confidence: ConfidenceAuthoritative, Effective: current.Effective, KnownAt: knownAt(t)}, Authority: correctionAuthority(), Dependencies: correctionDependencies(t)})
	if err != nil {
		t.Fatal(err)
	}
	explanation, err := plan.Explain()
	if err != nil || explanation.ImpactCount != 5 || explanation.Digest != plan.CanonicalDigest {
		t.Fatalf("explanation = %+v/%v", explanation, err)
	}
}

func TestTodo_LOCATION_003_Race(t *testing.T) {
	current := correctionLocation(t)
	request := WorkLocationCorrectionRequest{Current: current, Replacement: WorkLocationRevision{Address: validAddress(t), SourceAuthority: "correction/v1", Confidence: ConfidenceAuthoritative, Effective: current.Effective, KnownAt: knownAt(t)}, Authority: correctionAuthority(), Dependencies: correctionDependencies(t)}
	done := make(chan struct{}, 12)
	for i := 0; i < 12; i++ {
		go func() { _, _ = CorrectWorkLocation(request); done <- struct{}{} }()
	}
	for i := 0; i < 12; i++ {
		<-done
	}
}

func TestTodo_LOCATION_003_Fault(t *testing.T) {
	current := correctionLocation(t)
	request := WorkLocationCorrectionRequest{Current: current, Replacement: WorkLocationRevision{Address: validAddress(t), SourceAuthority: "correction/v1", Confidence: ConfidenceAuthoritative, Effective: current.Effective, KnownAt: knownAt(t)}, Authority: decision.Decision{State: decision.Deny, ProposalRevisionDigest: "sha256:proposal", Digest: "sha256:decision"}, Dependencies: correctionDependencies(t)}
	if _, err := CorrectWorkLocation(request); !errors.Is(err, ErrCorrectionNotGoverned) {
		t.Fatalf("denied correction = %v", err)
	}
	request.Authority = correctionAuthority()
	request.Dependencies = []DependentPeriod{{Kind: ImpactTax, SubjectRef: "tax:outside", Period: correctionPeriod(t, 25, 30), RuleRelease: "tax-2026.1"}}
	if _, err := CorrectWorkLocation(request); !errors.Is(err, ErrNoAffectedPeriod) {
		t.Fatalf("unaffected correction = %v", err)
	}
}

func TestTodo_LOCATION_003_Security(t *testing.T) {
	current := correctionLocation(t)
	plan, err := CorrectWorkLocation(WorkLocationCorrectionRequest{Current: current, Replacement: WorkLocationRevision{Address: validAddress(t), SourceAuthority: "correction/v1", Confidence: ConfidenceAuthoritative, Effective: current.Effective, KnownAt: knownAt(t)}, Authority: correctionAuthority(), Dependencies: correctionDependencies(t)})
	if err != nil {
		t.Fatal(err)
	}
	explanation, err := plan.Explain()
	if err != nil {
		t.Fatal(err)
	}
	if explanation.ImpactCount != len(plan.Impacts) {
		t.Fatal("explanation does not match bounded plan")
	}
}

func TestTodo_LOCATION_003_Conformance(t *testing.T) {
	current := correctionLocation(t)
	request := WorkLocationCorrectionRequest{Current: current, Replacement: WorkLocationRevision{Address: validAddress(t), SourceAuthority: "correction/v1", Confidence: ConfidenceAuthoritative, Effective: current.Effective, KnownAt: knownAt(t)}, Authority: correctionAuthority(), Dependencies: correctionDependencies(t)}
	plan, err := CorrectWorkLocation(request)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Successor.Revision != current.Revision+1 {
		t.Fatal("successor is not append-only")
	}
	for _, impact := range plan.Impacts {
		if !impact.Kind.Valid() || impact.RuleRelease == "" {
			t.Fatalf("invalid downstream intent = %+v", impact)
		}
	}
}

func TestTodo_LOCATION_003_Mutation(t *testing.T) {
	current := correctionLocation(t)
	plan, err := CorrectWorkLocation(WorkLocationCorrectionRequest{Current: current, Replacement: WorkLocationRevision{Address: validAddress(t), SourceAuthority: "correction/v1", Confidence: ConfidenceAuthoritative, Effective: current.Effective, KnownAt: knownAt(t)}, Authority: correctionAuthority(), Dependencies: correctionDependencies(t)})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Previous.Revision != 1 || plan.Previous.CanonicalDigest != current.CanonicalDigest {
		t.Fatal("previous revision was mutated")
	}
}
