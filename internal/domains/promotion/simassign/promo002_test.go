package simassign_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/simassign"
	promosnapshot "github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/snapshot"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/definitions"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// TestTodo_PROMO_002 is the primary: over the harborcare-demo Promotion
// snapshot, the simulation proposes exactly the three People/Org/Position
// effects, each effective-dated with the prior assignment's end, each carrying
// its reversibility class and the compensation and observation references a
// plan needs, each naming the snapshot inputs it derived from, and none of it
// touching anything.
func TestTodo_PROMO_002(t *testing.T) {
	t.Parallel()
	result := simulate(t, newHarness(t), nil)

	if !result.Executable() {
		t.Fatalf("the fixture promotion was refused: %v", result.Err())
	}
	if got, want := len(result.Effects), 3; got != want {
		t.Fatalf("proposed %d effects, want %d:\n%s", got, want, result.Explain())
	}
	for i, want := range simassign.EffectKinds() {
		if got := result.Effects[i].Kind; got != want {
			t.Fatalf("effect %d is %s, want %s", i, got, want)
		}
	}

	// The new revision starts on the promotion date and the prior assignment
	// ends the day before: the two never overlap.
	if got, want := result.EffectiveOn.String(), fixtureEffectiveOn; got != want {
		t.Fatalf("effective on %s, want %s", got, want)
	}
	if got, want := result.PriorAssignmentEnd.String(), "2026-05-31"; got != want {
		t.Fatalf("prior assignment ends %s, want %s", got, want)
	}
	if result.SnapshotDigest == "" || result.Digest == "" {
		t.Fatal("the result carries no snapshot digest or no result digest")
	}

	assignment, ok := result.Lookup(simassign.EffectAssignmentRevision)
	if !ok {
		t.Fatal("no assignment revision was proposed")
	}
	// The "before" of every placement field is the snapshot's, not the
	// caller's: the fixture subject sits in OPS-HRBP2/P2 today.
	wantBefore := map[string]string{
		string(people.FieldJobCode):    "OPS-HRBP2",
		string(people.FieldGrade):      "P2",
		string(people.FieldOrgUnit):    "people-ops",
		string(people.FieldPayZone):    "US-EAST",
		string(people.FieldPositionID): "POS-HRBP-204",
	}
	wantAfter := map[string]string{
		string(people.FieldJobCode):    fixtureTargetJob,
		string(people.FieldGrade):      fixtureTargetGrade,
		string(people.FieldOrgUnit):    fixtureTargetOrg,
		string(people.FieldPayZone):    fixtureTargetZone,
		string(people.FieldPositionID): fixturePositionID,
	}
	seen := map[string]bool{}
	for _, change := range assignment.Changes {
		seen[change.Field] = true
		if want, ok := wantBefore[change.Field]; ok && change.Before != want {
			t.Fatalf("%s before is %q, want %q", change.Field, change.Before, want)
		}
		if want, ok := wantAfter[change.Field]; ok && change.After != want {
			t.Fatalf("%s after is %q, want %q", change.Field, change.After, want)
		}
	}
	for field := range wantBefore {
		if !seen[field] {
			t.Fatalf("the assignment revision does not move %s", field)
		}
	}
	if !seen[string(people.FieldManagerRelation)] {
		t.Fatal("the assignment revision does not carry the manager relationship")
	}
	if got, want := assignment.Reversibility, simassign.Reversible; got != want {
		t.Fatalf("assignment reversibility is %s, want %s", got, want)
	}
	if !assignment.Local {
		t.Fatal("the assignment revision is not inside the local commit boundary")
	}
	if assignment.ExpectedRevision.String() == "" {
		t.Fatal("the assignment revision pins no baseline")
	}

	// Every effect names the snapshot inputs it derived from, and every named
	// input is one the snapshot actually declares.
	declared := map[string]bool{}
	for _, name := range promosnapshot.InputNames() {
		declared[name] = true
	}
	for _, effect := range result.Effects {
		if len(effect.DerivedFrom) == 0 {
			t.Fatalf("effect %s names no snapshot input", effect.Kind)
		}
		for _, name := range effect.DerivedFrom {
			if !declared[name] {
				t.Fatalf("effect %s derives from %q, which is not a declared input", effect.Kind, name)
			}
		}
		if effect.CompensationRef == "" || effect.ObservationRef == "" {
			t.Fatalf("effect %s declares no compensation or observation", effect.Kind)
		}
		if effect.IdempotencyKey == "" {
			t.Fatalf("effect %s carries no idempotency key", effect.Kind)
		}
	}

	// The occupancy transition brings the POSITION-003 reservation with it,
	// built and never taken.
	if !result.ReservationPlanned {
		t.Fatal("the occupancy transition planned no position reservation")
	}
	if got := result.Reservation.ProposalRevisionID; got != fixtureRevisionID {
		t.Fatalf("the reservation binds proposal %q, want %q", got, fixtureRevisionID)
	}
	if got := result.Reservation.Position; got != positionRef() {
		t.Fatalf("the reservation holds %s, want %s", got, positionRef())
	}
	if !strings.Contains(result.Explain(), "position reservation: planned") {
		t.Fatalf("Explain does not report the reservation:\n%s", result.Explain())
	}
}

// TestTodo_PROMO_002_Property proves the determinism the artifact rests on:
// identical snapshots and identical intentions yield identical digests, and any
// material change to either yields a different one.
func TestTodo_PROMO_002_Property(t *testing.T) {
	t.Parallel()
	base := simulate(t, newHarness(t), nil)

	// Two independent builds of the same fixture, simulated independently.
	for i := range 8 {
		again := simulate(t, newHarness(t), nil)
		if again.Digest != base.Digest {
			t.Fatalf("run %d produced digest %s, want %s", i, again.Digest, base.Digest)
		}
		if again.SnapshotDigest != base.SnapshotDigest {
			t.Fatalf("run %d read a different snapshot: %s", i, again.SnapshotDigest)
		}
	}

	// A different intention is a different simulation.
	moved := simulate(t, newHarness(t), func(r *simassign.Request) { r.Target.Grade = "P4" })
	if moved.Digest == base.Digest {
		t.Fatal("changing the target grade did not change the result digest")
	}

	// A different snapshot is a different simulation, even with the same
	// intention.
	h := newHarness(t)
	h.Budget.ref.BaselineVersion = "finance.budget.baseline/2026.10"
	shifted := simulate(t, h, nil)
	if shifted.SnapshotDigest == base.SnapshotDigest {
		t.Fatal("a changed budget baseline did not change the snapshot digest")
	}
	if shifted.Digest == base.Digest {
		t.Fatal("a changed snapshot did not change the result digest")
	}
}

// TestTodo_PROMO_002_Integration proves the effects are plan-compilable: the
// kernel accepts a proposal revision and a transaction plan built from nothing
// but this simulation's own projections, against the real promote_worker
// definition, and the plan it compiles is still non-executable.
func TestTodo_PROMO_002_Integration(t *testing.T) {
	t.Parallel()
	result := simulate(t, newHarness(t), nil)

	definition, ok := promoteWorkerDefinition(t)
	if !ok {
		t.Fatal("the intent catalog publishes no promote_worker definition")
	}

	writes := result.PlannedWrites()
	if len(writes) == 0 {
		t.Fatal("the simulation implies no planned write")
	}
	for _, write := range writes {
		if write.SourceAuthorityDecision == "" {
			t.Fatalf("planned write on %s records no authority decision", write.FieldPath)
		}
		if !write.ExpectedRevision.IsSpecified() {
			t.Fatalf("planned write on %s pins no baseline", write.FieldPath)
		}
	}

	// Every People/Org/Position effect is inside the local commit boundary, so
	// a ZERO_EFFECT P1A definition can compile a plan from them: there is no
	// outbox record to refuse.
	if got := result.OutboxEffects(); len(got) != 0 {
		t.Fatalf("the assignment simulation plans %d outbox effect(s); all three are local", len(got))
	}

	proposal := intent.ProposalRevision{
		ProposalRevisionID: fixtureRevisionID,
		IntentID:           "int_promo002",
		Revision:           1,
		Tenant:             fixtures.Tenant,
		Subjects:           result.EffectSubjects(),
		EffectiveTime:      result.Effects[0].Effective,
		Writes:             writes,
	}
	proposal.MaterialDigest.Digest = result.Digest

	plan, err := intent.CompilePlan(intent.PlanInput{
		Proposal:   proposal,
		Definition: definition,
		Mode:       intent.ModeSimulate,
		Governance: intent.GovernanceSnapshot{
			SnapshotDigest: "sha256:governance",
			AuthZDecision:  "ALLOW", LegalDecision: "ALLOW",
			PolicyDecision: "ALLOW", RiskDecision: "ALLOW",
		},
		Conflict: intent.ConflictSnapshot{
			SnapshotDigest: "sha256:conflict", FenceToken: "fence-1", FootprintRef: "footprint-1",
		},
		Participants:         result.Participants(),
		Reads:                result.Reads(),
		Appends:              plannedAppends(result),
		IdempotencyRecordRef: "idem_promo002",
		RevalidationRuleRefs: []string{"revalidate.promotion.assignment/v1"},
		ExpiresAt:            mustInstant(t, fixtureReservationExpiry),
	}, func() (string, error) { return "plan_promo002", nil })
	if err != nil {
		t.Fatalf("CompilePlan over the simulated effects: %v", err)
	}
	if plan.Status != intent.PlanGovernanceValidated {
		t.Fatalf("plan status is %s, want GOVERNANCE_VALIDATED", plan.Status)
	}
	if plan.Executable() {
		t.Fatal("a Gate A plan reported itself executable")
	}
	if err := plan.VerifyDigest(); err != nil {
		t.Fatalf("compiled plan digest: %v", err)
	}
}

// TestTodo_PROMO_002_Security proves the two disclosure rules the package
// exists to keep: a caller cannot supply a current value, and a withheld input
// refuses the effect that needed it instead of producing a guess.
func TestTodo_PROMO_002_Security(t *testing.T) {
	t.Parallel()

	// A hand-built snapshot -- the only shape a caller-supplied current value
	// can take, since PromotionInputSnapshot keeps its inputs unexported -- is
	// refused, digest and all.
	forged := promosnapshot.PromotionInputSnapshot{
		Tenant:      fixtures.Tenant,
		Subject:     mustWorkerRef(t),
		EffectiveOn: mustLocalDate(t, fixtureEffectiveOn),
		Digest:      "sha256:whatever-the-caller-says",
	}
	if _, err := simassign.Simulate(simulateRequest(t, forged)); !errors.Is(err, simassign.ErrSnapshotUntrusted) {
		t.Fatalf("a hand-built snapshot was accepted or refused for the wrong reason: %v", err)
	}

	// A real snapshot whose digest was edited is refused too.
	h := newHarness(t)
	tampered := h.build(t, fixtureRequest(t))
	tampered.Digest = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	if _, err := simassign.Simulate(simulateRequest(t, tampered)); !errors.Is(err, simassign.ErrSnapshotUntrusted) {
		t.Fatalf("a re-digested snapshot was accepted: %v", err)
	}

	// Mutating the copy the accessor hands back changes nothing: the effects
	// derive from the snapshot's own bound inputs.
	snap := newHarness(t).build(t, fixtureRequest(t))
	stolen := snap.Inputs()
	for i := range stolen {
		stolen[i].CanonicalText = "assignment.job_code=ATTACKER"
	}
	after, err := simassign.Simulate(simulateRequest(t, snap))
	if err != nil {
		t.Fatalf("Simulate after tampering with the returned slice: %v", err)
	}
	assignment, ok := after.Lookup(simassign.EffectAssignmentRevision)
	if !ok {
		t.Fatal("no assignment revision was proposed")
	}
	for _, change := range assignment.Changes {
		if strings.Contains(change.Before, "ATTACKER") {
			t.Fatalf("%s took its before from the caller's copy", change.Field)
		}
	}

	// A withheld placement refuses the assignment revision by name, and the
	// refusal carries no value.
	denied := newHarness(t)
	req := fixtureRequest(t)
	req.Authorization.Worker = fixtures.DenyFields(req.Authorization.Worker,
		"policy:no_placement_disclosure", people.FieldJobCode)
	partial, buildErr := promosnapshot.Build(t.Context(), denied.readers(), req)
	if buildErr == nil {
		t.Fatal("a build with a denied placement field was accepted")
	}
	result, err := simassign.Simulate(simulateRequest(t, partial))
	if err != nil {
		t.Fatalf("Simulate over a refused snapshot: %v", err)
	}
	if result.Executable() {
		t.Fatal("a promotion whose placement was withheld reported itself executable")
	}
	if _, ok := result.Lookup(simassign.EffectAssignmentRevision); ok {
		t.Fatal("an assignment revision was proposed from a withheld placement")
	}
	var refusal simassign.Refusal
	if !errors.As(result.Err(), &refusal) {
		t.Fatalf("the refusal is not typed: %v", result.Err())
	}
	if refusal.InputName != promosnapshot.InputCurrentPlacement {
		t.Fatalf("the refusal names %q, want %q", refusal.InputName, promosnapshot.InputCurrentPlacement)
	}
	if refusal.Reason != simassign.ReasonInputWithheld {
		t.Fatalf("the refusal reason is %s, want %s", refusal.Reason, simassign.ReasonInputWithheld)
	}
	if !errors.Is(result.Err(), simassign.ErrRefused) {
		t.Fatal("the refusal does not match ErrRefused")
	}
	if strings.Contains(result.Explain(), "OPS-HRBP2") {
		t.Fatalf("the explanation leaks a withheld value:\n%s", result.Explain())
	}
}

// TestTodo_PROMO_002_Mutation kills the mutants that would make the simulation
// look right while being wrong: a cycle accepted, a depth bound ignored, a full
// position filled anyway, a vacancy that arrives too late, and a prior
// assignment left open across the effective date.
func TestTodo_PROMO_002_Mutation(t *testing.T) {
	t.Parallel()

	t.Run("a self-referential manager is a refused cycle", func(t *testing.T) {
		t.Parallel()
		result := simulate(t, newHarness(t), func(r *simassign.Request) {
			r.ProposedManager = mustWorkerRef(t)
		})
		if result.Chain.Verdict != simassign.ChainCycle {
			t.Fatalf("chain verdict is %s, want CYCLE", result.Chain.Verdict)
		}
		if _, ok := result.Lookup(simassign.EffectManagerRelationship); ok {
			t.Fatal("a manager relationship was proposed despite a cycle")
		}
		if result.Executable() {
			t.Fatal("a cyclic promotion reported itself executable")
		}
		if !hasRefusal(result, simassign.ReasonManagerCycle) {
			t.Fatalf("no cycle refusal:\n%s", result.Explain())
		}
	})

	t.Run("a chain deeper than the bound is refused", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		h.Org.withSkipLevel(t)
		result := simulate(t, h, func(r *simassign.Request) {
			r.ProposedManager = managerRef()
			r.ChainDepthBound = 1
		})
		if result.Chain.Verdict != simassign.ChainExceedsBound {
			t.Fatalf("chain verdict is %s, want EXCEEDS_BOUND", result.Chain.Verdict)
		}
		if result.Chain.ProposedDepth != 2 {
			t.Fatalf("proposed depth is %d, want 2", result.Chain.ProposedDepth)
		}
		if !hasRefusal(result, simassign.ReasonChainDepthExceeded) {
			t.Fatalf("no depth refusal:\n%s", result.Explain())
		}
	})

	t.Run("a chain inside the bound resolves", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		h.Org.withSkipLevel(t)
		result := simulate(t, h, func(r *simassign.Request) { r.ProposedManager = skipManagerRef() })
		if result.Chain.Verdict != simassign.ChainWithinBound {
			t.Fatalf("chain verdict is %s, want WITHIN_BOUND", result.Chain.Verdict)
		}
		if result.Chain.ProposedDepth != 1 {
			t.Fatalf("proposed depth is %d, want 1", result.Chain.ProposedDepth)
		}
		if !result.Executable() {
			t.Fatalf("a bounded chain was refused: %v", result.Err())
		}
	})

	t.Run("a manager outside the resolved chain leaves the depth unknown", func(t *testing.T) {
		t.Parallel()
		result := simulate(t, newHarness(t), nil)
		if result.Chain.Verdict != simassign.ChainTailNotEstablished {
			t.Fatalf("chain verdict is %s, want TAIL_NOT_ESTABLISHED", result.Chain.Verdict)
		}
		if result.Chain.ProposedDepth != -1 {
			t.Fatalf("proposed depth is %d, want unknown", result.Chain.ProposedDepth)
		}
		if !result.Executable() {
			t.Fatalf("an unestablished chain tail blocked the promotion: %v", result.Err())
		}
	})

	t.Run("a full position with no vacancy is refused", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		h.Position.revision.Capacity.CapacityHeads = 0
		h.Position.revision.Capacity.CapacityFTE = mustDecimal(t, "0.0000", 4)
		snap, _ := promosnapshot.Build(t.Context(), h.readers(), fixtureRequest(t))
		result, err := simassign.Simulate(simulateRequest(t, snap))
		if err != nil {
			t.Fatalf("Simulate: %v", err)
		}
		if result.Occupancy.Verdict != simassign.OccupancyAtCapacity {
			t.Fatalf("occupancy verdict is %s, want AT_CAPACITY", result.Occupancy.Verdict)
		}
		if _, ok := result.Lookup(simassign.EffectPositionOccupancy); ok {
			t.Fatal("an occupancy transition was proposed into a full position")
		}
		if result.ReservationPlanned {
			t.Fatal("a reservation was planned for a full position")
		}
	})

	t.Run("the prior assignment closes the day before the new one opens", func(t *testing.T) {
		t.Parallel()
		result := simulate(t, newHarness(t), nil)
		if got := result.PriorAssignmentEnd.AddDays(1).Compare(result.EffectiveOn); got != 0 {
			t.Fatalf("the prior assignment does not end the day before the promotion: %s vs %s",
				result.PriorAssignmentEnd, result.EffectiveOn)
		}
	})
}

// hasRefusal reports whether the result carries a refusal for a reason.
func hasRefusal(result simassign.Result, reason simassign.Reason) bool {
	for _, refusal := range result.Refusals {
		if refusal.Reason == reason {
			return true
		}
	}
	return false
}

// promoteWorkerDefinition returns the catalog's promote_worker definition.
func promoteWorkerDefinition(t testing.TB) (intent.Definition, bool) {
	t.Helper()
	for _, def := range definitions.All() {
		if def.Ref.TypeID == "hcmnext.people.promote_worker" {
			return def, true
		}
	}
	return intent.Definition{}, false
}

// plannedAppends turns each effect into the one append its participant would
// take, which is what CompilePlan requires beside a planned write.
func plannedAppends(result simassign.Result) []intent.PlannedAppend {
	out := make([]intent.PlannedAppend, 0, len(result.Effects))
	for i, effect := range result.Effects {
		out = append(out, intent.PlannedAppend{
			StreamID:         effect.Participant,
			ExpectedSequence: uint64(i + 1),
			EventType:        string(effect.Kind),
			PayloadDigest:    effect.IdempotencyKey,
		})
	}
	return out
}

var _ = values.LocalDate{}
