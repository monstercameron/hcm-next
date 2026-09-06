package cycle

import (
	"testing"
	"time"
)

func testBindingForChanges(t *testing.T) PopulationBinding {
	t.Helper()
	b, err := BindPopulation("sha256:cyclerev-1", validSnapshotRef(), time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestTodo_CYCLE_006 is the primary acceptance case: an admission or
// removal request against a frozen population binding always produces an
// explicit, reasoned disposition -- include, exclude, defer or review --
// with an effective time and downstream recalculation obligations. A late
// request is never silently dropped (it decides Defer or Review, never
// nothing) and never silently applied (it is Included/Excluded only when
// the active phase declares the operation for it); deciding never mutates
// the binding.
func TestTodo_CYCLE_006(t *testing.T) {
	binding := testBindingForChanges(t)
	cutoff := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	decidedAt := time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC)

	// Before cutoff, with the declared operation: included immediately.
	openAdmit := CompiledPhase{ID: "open", AllowedOperations: []string{OperationAdmitMember}}
	req := MembershipChangeRequest{
		Kind: PopulationChangeAdmission, SubjectRef: "emp-1", Reason: "NEW_HIRE",
		RequestedAt: decidedAt, EffectiveAt: time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC),
	}
	decision, err := DecideMembershipChange(binding, openAdmit, cutoff, req, decidedAt)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Disposition != DispositionInclude || decision.EffectiveAt.IsZero() ||
		len(decision.RecalculationObligations) == 0 || decision.Rule == "" || decision.Digest == "" {
		t.Fatalf("expected an included, obligated, explained decision, got %+v", decision)
	}

	// Before cutoff, without the declared operation: reviewed, never a
	// silent apply.
	closed := CompiledPhase{ID: "closed"}
	reviewDecision, err := DecideMembershipChange(binding, closed, cutoff, req, decidedAt)
	if err != nil {
		t.Fatal(err)
	}
	if reviewDecision.Disposition != DispositionReview || !reviewDecision.EffectiveAt.IsZero() {
		t.Fatalf("expected REVIEW without a declared admit operation, got %+v", reviewDecision)
	}

	// After cutoff, without a late-entrant override: deferred, never
	// dropped.
	lateReq := req
	lateReq.EffectiveAt = time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC)
	deferDecision, err := DecideMembershipChange(binding, openAdmit, cutoff, lateReq, decidedAt)
	if err != nil {
		t.Fatal(err)
	}
	if deferDecision.Disposition != DispositionDefer || !deferDecision.EffectiveAt.IsZero() ||
		len(deferDecision.RecalculationObligations) == 0 {
		t.Fatalf("expected a deferred late admission with no immediate effect, got %+v", deferDecision)
	}

	// After cutoff, with the late-entrant override declared: included.
	openLate := CompiledPhase{ID: "open", AllowedOperations: []string{OperationAdmitMember, OperationAdmitLateEntrant}}
	lateInclude, err := DecideMembershipChange(binding, openLate, cutoff, lateReq, decidedAt)
	if err != nil {
		t.Fatal(err)
	}
	if lateInclude.Disposition != DispositionInclude || lateInclude.EffectiveAt.IsZero() {
		t.Fatalf("expected a late admission included under an explicit override, got %+v", lateInclude)
	}

	// A removal decides just as explicitly.
	removeReq := MembershipChangeRequest{
		Kind: PopulationChangeRemoval, SubjectRef: "emp-2", Reason: "TERMINATION",
		RequestedAt: decidedAt, EffectiveAt: time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC),
	}
	openRemove := CompiledPhase{ID: "open", AllowedOperations: []string{OperationRemoveMember}}
	removeDecision, err := DecideMembershipChange(binding, openRemove, cutoff, removeReq, decidedAt)
	if err != nil {
		t.Fatal(err)
	}
	if removeDecision.Disposition != DispositionExclude {
		t.Fatalf("expected removal excluded under a declared operation, got %s", removeDecision.Disposition)
	}
	// A late removal with no override held for review rather than either
	// silently excluded or silently retained.
	lateRemoveReq := removeReq
	lateRemoveReq.EffectiveAt = time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC)
	lateRemoveReview, err := DecideMembershipChange(binding, openRemove, cutoff, lateRemoveReq, decidedAt)
	if err != nil {
		t.Fatal(err)
	}
	if lateRemoveReview.Disposition != DispositionReview {
		t.Fatalf("expected a late removal without an override to be held for review, got %s", lateRemoveReview.Disposition)
	}

	// None of the above touched the binding itself.
	if binding.Snapshot != validSnapshotRef() {
		t.Fatal("deciding membership changes mutated the population binding")
	}

	// Structural failures never produce a silent decision.
	if _, err := DecideMembershipChange(binding, openAdmit, cutoff, MembershipChangeRequest{}, decidedAt); err == nil {
		t.Fatal("expected an error for an empty membership change request")
	}
	unbound := PopulationBinding{}
	if _, err := DecideMembershipChange(unbound, openAdmit, cutoff, req, decidedAt); err == nil {
		t.Fatal("expected an error for a decision against an unbound population")
	}
	if _, err := DecideMembershipChange(binding, openAdmit, cutoff, req, time.Time{}); err == nil {
		t.Fatal("expected an error for a zero decided-at instant")
	}
}

// TestTodo_CYCLE_006_Property verifies DecideMembershipChange only ever
// returns one of the four declared dispositions and is deterministic:
// identical inputs decide identically every time.
func TestTodo_CYCLE_006_Property(t *testing.T) {
	binding := testBindingForChanges(t)
	cutoff := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	decidedAt := time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC)
	phases := []CompiledPhase{
		{ID: "closed"},
		{ID: "open", AllowedOperations: []string{OperationAdmitMember, OperationRemoveMember}},
		{ID: "open-late", AllowedOperations: []string{OperationAdmitLateEntrant, OperationRemoveLateEntrant}},
	}
	kinds := []PopulationChangeKind{PopulationChangeAdmission, PopulationChangeRemoval}
	effectives := []time.Time{
		time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC),
	}
	valid := map[Disposition]bool{
		DispositionInclude: true, DispositionExclude: true,
		DispositionDefer: true, DispositionReview: true,
	}

	for _, phase := range phases {
		for _, kind := range kinds {
			for _, eff := range effectives {
				req := MembershipChangeRequest{Kind: kind, SubjectRef: "s", Reason: "r", RequestedAt: decidedAt, EffectiveAt: eff}
				a, err := DecideMembershipChange(binding, phase, cutoff, req, decidedAt)
				if err != nil {
					t.Fatal(err)
				}
				if !valid[a.Disposition] {
					t.Fatalf("unrecognized disposition %q for phase %s kind %s", a.Disposition, phase.ID, kind)
				}
				b, err := DecideMembershipChange(binding, phase, cutoff, req, decidedAt)
				if err != nil {
					t.Fatal(err)
				}
				if a.Digest != b.Digest || a.Disposition != b.Disposition {
					t.Fatalf("DecideMembershipChange is not deterministic for phase %s kind %s", phase.ID, kind)
				}
			}
		}
	}
}

// TestTodo_CYCLE_006_Mutation verifies the decision digest is sensitive to
// every field that participates in the audit record: the subject, reason,
// effective time, kind and active phase context each change the digest
// when changed, so a mutant that drops one of them from the digest input
// is caught.
func TestTodo_CYCLE_006_Mutation(t *testing.T) {
	binding := testBindingForChanges(t)
	cutoff := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	decidedAt := time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC)
	phase := CompiledPhase{ID: "open", AllowedOperations: []string{OperationAdmitMember}}
	base := MembershipChangeRequest{
		Kind: PopulationChangeAdmission, SubjectRef: "emp-1", Reason: "NEW_HIRE",
		RequestedAt: decidedAt, EffectiveAt: time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC),
	}
	baseline, err := DecideMembershipChange(binding, phase, cutoff, base, decidedAt)
	if err != nil {
		t.Fatal(err)
	}

	mutate := func(name string, mutated MembershipChangeRequest) {
		t.Helper()
		got, err := DecideMembershipChange(binding, phase, cutoff, mutated, decidedAt)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got.Digest == baseline.Digest {
			t.Fatalf("%s: digest did not change", name)
		}
	}

	withSubject := base
	withSubject.SubjectRef = "emp-2"
	mutate("subject", withSubject)

	withReason := base
	withReason.Reason = "TRANSFER"
	mutate("reason", withReason)

	withEffective := base
	withEffective.EffectiveAt = base.EffectiveAt.Add(24 * time.Hour)
	mutate("effective_at", withEffective)

	withKind := base
	withKind.Kind = PopulationChangeRemoval
	mutate("kind", withKind)

	otherPhase, err := DecideMembershipChange(binding, CompiledPhase{ID: "closed"}, cutoff, base, decidedAt)
	if err != nil {
		t.Fatal(err)
	}
	if otherPhase.Digest == baseline.Digest {
		t.Fatal("phase: digest did not change when the active phase changed")
	}
}
