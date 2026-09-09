package intent_test

import (
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/intent"
)

// planInput builds a complete plan compilation input for a simulated promotion.
func planInput(t *testing.T, def intent.Definition, rev intent.ProposalRevision) intent.PlanInput {
	t.Helper()
	return intent.PlanInput{
		Proposal:   rev,
		Definition: def,
		Mode:       intent.ModeSimulate,
		Governance: intent.GovernanceSnapshot{
			SnapshotDigest: "governance-1",
			AuthZDecision:  "PERMIT",
			LegalDecision:  "PERMIT",
			PolicyDecision: "PERMIT",
			RiskDecision:   "ACCEPT",
		},
		Conflict: intent.ConflictSnapshot{
			SnapshotDigest: "conflict-1",
			FenceToken:     "fence-1",
			FootprintRef:   "promotion_affected_fields_and_effective_interval/v1",
		},
		Participants: []intent.PlanParticipant{{
			ParticipantID: "participant:people",
			StreamID:      "people.employment.9001",
			StorageClass:  "LOCAL_POSTGRES",
			Local:         true,
		}},
		Reads: []intent.PlannedRead{{
			ResourceKey:      mustResourceKey(t, "employment", "9001", "primary"),
			ExpectedRevision: mustSequenceRevision(t, "people.employment.9001", 42),
		}},
		Appends: []intent.PlannedAppend{{
			StreamID:         "people.employment.9001",
			ExpectedSequence: 43,
			EventType:        "people.assignment_position_changed/v1",
			PayloadDigest:    "append-payload-1",
		}},
		ProjectionMutations: []intent.ProjectionMutation{{
			ProjectionID: "projection.worker_state/v1",
			ResourceKey:  mustResourceKey(t, "employment", "9001", "primary"),
			Operation:    "UPSERT",
		}},
		Preconditions: []intent.CommitPrecondition{{
			Kind: "SOURCE_AUTHORITY", Ref: "authority.local_master/v1",
		}},
		Reservations: []intent.ReservationBinding{{
			ReservationID: "reservation:1", Expiry: startOfNextYear(),
		}},
		IdempotencyRecordRef:   "idempotency:promote:9001",
		ApprovalRequirementIDs: []string{"req.promotion_manager/v1"},
		RevalidationRuleRefs:   []string{"promotion_execution_revalidation/v1"},
		ExpiresAt:              startOfNextYear(),
	}
}

// TestTodo_TX_001 is the PRIMARY test for compiling immutable non-executable
// TransactionPlans.
//
// RED: a missing participant, read, write, effect, sequence, idempotency,
// approval, revalidation, compensation or observation declaration fails
// compilation.
//
// GREEN: a Gate A plan exposes exact effects and returns
// EXECUTION_PROHIBITED_GATE_A on any execution attempt.
func TestTodo_TX_001(t *testing.T) {
	reg := mustRegistry(t)
	d := mustDigester(t)
	def := mustResolve(t, reg, "hcmnext.people.promote_worker/v1")
	rev, err := intent.NewProposalRevision(promoteProposal(t, "intent:1"), def, d,
		countingIDs("01234567"), fixedClock())
	if err != nil {
		t.Fatalf("mint proposal: %v", err)
	}

	t.Run("the plan type exposes no execution", func(t *testing.T) {
		// P1A compiles plans and never runs them, so the type carries no method
		// that could be mistaken for one. This is structural, not a convention.
		typ := reflect.TypeOf(intent.TransactionPlan{})
		for _, banned := range []string{
			"Execute", "Commit", "Apply", "Run", "Prepare", "Perform", "Do", "Abort",
		} {
			if _, ok := typ.MethodByName(banned); ok {
				t.Fatalf("TransactionPlan exposes %q; Gate A grants no write authority", banned)
			}
		}
		for i := 0; i < typ.NumMethod(); i++ {
			name := typ.Method(i).Name
			if strings.HasPrefix(name, "Exec") && name != "Executable" {
				t.Fatalf("TransactionPlan exposes an execution-shaped method %q", name)
			}
		}
	})

	t.Run("RED", func(t *testing.T) {
		cases := []struct {
			name   string
			break_ func(*intent.PlanInput)
			cause  error
		}{
			{"no participant", func(in *intent.PlanInput) { in.Participants = nil }, intent.ErrInvalidPlan},
			{"participant with no stream", func(in *intent.PlanInput) {
				in.Participants[0].StreamID = ""
			}, intent.ErrInvalidPlan},
			{"no read baseline", func(in *intent.PlanInput) { in.Reads = nil }, intent.ErrInvalidPlan},
			{"unpinned read", func(in *intent.PlanInput) {
				in.Reads[0].ExpectedRevision = intent.PlannedRead{}.ExpectedRevision
			}, intent.ErrInvalidPlan},
			{"planned writes with no append", func(in *intent.PlanInput) { in.Appends = nil }, intent.ErrInvalidPlan},
			{"append with no expected sequence", func(in *intent.PlanInput) {
				in.Appends[0].ExpectedSequence = 0
			}, intent.ErrInvalidPlan},
			{"append to an undeclared participant stream", func(in *intent.PlanInput) {
				in.Appends[0].StreamID = "rewards.compensation.9001"
			}, intent.ErrInvalidPlan},
			{"append with no payload digest", func(in *intent.PlanInput) {
				in.Appends[0].PayloadDigest = ""
			}, intent.ErrInvalidPlan},
			{"no idempotency record", func(in *intent.PlanInput) {
				in.IdempotencyRecordRef = ""
			}, intent.ErrInvalidPlan},
			{"required approval not bound", func(in *intent.PlanInput) {
				in.ApprovalRequirementIDs = nil
			}, intent.ErrInvalidPlan},
			{"no revalidation rule", func(in *intent.PlanInput) {
				in.RevalidationRuleRefs = nil
			}, intent.ErrInvalidPlan},
			{"no expiry", func(in *intent.PlanInput) {
				in.ExpiresAt = intent.PlanInput{}.ExpiresAt
			}, intent.ErrInvalidPlan},
			{"undecided governance", func(in *intent.PlanInput) {
				in.Governance.LegalDecision = ""
			}, intent.ErrInvalidPlan},
			{"no conflict fence", func(in *intent.PlanInput) {
				in.Conflict.FenceToken = ""
			}, intent.ErrInvalidPlan},
			{"proposal with no minted digest", func(in *intent.PlanInput) {
				in.Proposal.MaterialDigest.Digest = ""
			}, intent.ErrInvalidPlan},
			{"mode the definition forbids", func(in *intent.PlanInput) {
				in.Mode = intent.ModeExecute
			}, intent.ErrModeNotAllowed},
			{"effect on a zero-effect definition", func(in *intent.PlanInput) {
				in.Effects = []intent.OutboxEffect{{
					EffectID: "effect:notify", DestinationRef: "provider:hris",
					IdempotencyKey: "effect-idem-1",
				}}
				in.Compensations = []intent.CompensationBinding{{
					EffectID: "effect:notify", Strategy: "REPAIR",
				}}
				in.Observations = []intent.PostCommitObservation{{
					EffectID: "effect:notify", ObservationRef: "observe:1",
				}}
			}, intent.ErrInvalidPlan},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				in := planInput(t, def, rev)
				tc.break_(&in)
				if _, err := intent.CompilePlan(in, countingIDs("aaaaaaaa")); !errors.Is(err, tc.cause) {
					t.Fatalf("incomplete plan compiled, or wrong cause: err=%v want %v", err, tc.cause)
				}
			})
		}
	})

	t.Run("RED: effects without compensation or observation", func(t *testing.T) {
		writeDef := mustResolve(t, reg, "hcmnext.rewards.change_base_pay/v1")
		writeRev, err := intent.NewProposalRevision(promoteProposal(t, "intent:2"), writeDef, d,
			countingIDs("bbbbbbbb"), fixedClock())
		if err != nil {
			t.Fatalf("mint proposal: %v", err)
		}
		effect := intent.OutboxEffect{
			EffectID: "effect:notify", DestinationRef: "provider:hris",
			IdempotencyKey: "effect-idem-1", Reversibility: "REVERSIBLE",
		}
		for _, tc := range []struct {
			name   string
			break_ func(*intent.PlanInput)
		}{
			{"no compensation", func(in *intent.PlanInput) { in.Compensations = nil }},
			{"no post-commit observation", func(in *intent.PlanInput) { in.Observations = nil }},
			{"no effect idempotency key", func(in *intent.PlanInput) {
				in.Effects[0].IdempotencyKey = ""
			}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				in := planInput(t, writeDef, writeRev)
				in.Effects = []intent.OutboxEffect{effect}
				in.Compensations = []intent.CompensationBinding{{
					EffectID: effect.EffectID, Strategy: "REPAIR", RepairPlanID: "repair:1",
				}}
				in.Observations = []intent.PostCommitObservation{{
					EffectID: effect.EffectID, ObservationRef: "observe:1",
					Deadline: startOfNextYear(),
				}}
				tc.break_(&in)
				if _, err := intent.CompilePlan(in, countingIDs("cccccccc")); !errors.Is(err, intent.ErrInvalidPlan) {
					t.Fatalf("a plan with %s compiled: %v", tc.name, err)
				}
			})
		}
	})

	t.Run("GREEN", func(t *testing.T) {
		plan, err := intent.CompilePlan(planInput(t, def, rev), countingIDs("01234567"))
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		if plan.Status != intent.PlanGovernanceValidated {
			t.Fatalf("plan status = %s", plan.Status)
		}
		if plan.ProposalDigest != rev.MaterialDigest.Digest {
			t.Fatalf("the plan is not bound to the exact proposal digest")
		}
		// The plan exposes exactly what would happen.
		if len(plan.Participants) == 0 || len(plan.Reads) == 0 || len(plan.Appends) == 0 {
			t.Fatalf("the plan does not expose its participants, reads and appends")
		}
		if len(plan.Effects) != 0 {
			t.Fatalf("a zero-effect definition compiled a plan with %d effect(s)", len(plan.Effects))
		}
		// And exposes nothing that makes it happen.
		if plan.Executable() {
			t.Fatalf("a Gate A plan reported itself executable")
		}
		err = plan.AuthorizeExecution()
		if !errors.Is(err, intent.ErrExecutionProhibitedGateA) {
			t.Fatalf("execution attempt returned %v, want EXECUTION_PROHIBITED_GATE_A", err)
		}
		if !strings.Contains(err.Error(), "EXECUTION_PROHIBITED_GATE_A") {
			t.Fatalf("the refusal does not carry the canonical code: %v", err)
		}
		// The digest derives from canonical semantic content.
		if plan.Digest == "" {
			t.Fatalf("the plan carries no digest")
		}
		if err := plan.VerifyDigest(); err != nil {
			t.Fatalf("the plan digest does not verify against its own content: %v", err)
		}
	})

	t.Run("a mandatory deny compiles as BLOCKED, not as a refusal to compile", func(t *testing.T) {
		in := planInput(t, def, rev)
		in.Governance.MandatoryDeny = true
		plan, err := intent.CompilePlan(in, countingIDs("dddddddd"))
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		if plan.Status != intent.PlanBlocked {
			t.Fatalf("a mandatory deny produced status %s", plan.Status)
		}
		if plan.Executable() {
			t.Fatalf("a blocked plan reported itself executable")
		}
	})
}

// TestTodo_TX_001_Race compiles the same input concurrently and requires one
// stable digest: a plan digest that depended on map iteration or slice
// construction order would diverge here.
func TestTodo_TX_001_Race(t *testing.T) {
	reg := mustRegistry(t)
	d := mustDigester(t)
	def := mustResolve(t, reg, "hcmnext.people.promote_worker/v1")
	rev, err := intent.NewProposalRevision(promoteProposal(t, "intent:1"), def, d,
		countingIDs("01234567"), fixedClock())
	if err != nil {
		t.Fatalf("mint proposal: %v", err)
	}

	const n = 24
	digests := make([]string, n)
	ids := make([]string, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			in := planInput(t, def, rev)
			// Reordering the declarations must not change the digest: reads,
			// participants and preconditions are sets.
			if i%2 == 0 {
				in.Participants = append(in.Participants, intent.PlanParticipant{
					ParticipantID: "participant:audit", StreamID: "audit.ledger",
					StorageClass: "LOCAL_POSTGRES", Local: true,
				})
			} else {
				in.Participants = append([]intent.PlanParticipant{{
					ParticipantID: "participant:audit", StreamID: "audit.ledger",
					StorageClass: "LOCAL_POSTGRES", Local: true,
				}}, in.Participants...)
			}
			plan, err := intent.CompilePlan(in, nil)
			if err != nil {
				t.Errorf("goroutine %d: %v", i, err)
				return
			}
			digests[i] = plan.Digest
			ids[i] = plan.PlanID
		}(i)
	}
	wg.Wait()

	seen := map[string]bool{}
	for i := 0; i < n; i++ {
		if digests[i] != digests[0] {
			t.Fatalf("goroutine %d produced a different plan digest for equivalent content", i)
		}
		if seen[ids[i]] {
			t.Fatalf("plan id %q was minted twice", ids[i])
		}
		seen[ids[i]] = true
	}
}

// TestTodo_TX_001_Mutation perturbs one semantic element of a compiled plan at a
// time and requires the digest to move. A plan digest that ignored an element
// would let a plan be substituted after review.
func TestTodo_TX_001_Mutation(t *testing.T) {
	reg := mustRegistry(t)
	d := mustDigester(t)
	def := mustResolve(t, reg, "hcmnext.people.promote_worker/v1")
	rev, err := intent.NewProposalRevision(promoteProposal(t, "intent:1"), def, d,
		countingIDs("01234567"), fixedClock())
	if err != nil {
		t.Fatalf("mint proposal: %v", err)
	}
	baseline, err := intent.CompilePlan(planInput(t, def, rev), countingIDs("aaaaaaaa"))
	if err != nil {
		t.Fatalf("baseline: %v", err)
	}

	mutations := []struct {
		name   string
		mutate func(*intent.PlanInput)
	}{
		{"a participant", func(in *intent.PlanInput) { in.Participants[0].StorageClass = "REMOTE" }},
		{"a read baseline", func(in *intent.PlanInput) {
			in.Reads[0].ExpectedRevision = mustSequenceRevision(t, "people.employment.9001", 41)
		}},
		{"an expected sequence", func(in *intent.PlanInput) { in.Appends[0].ExpectedSequence = 44 }},
		{"an append payload digest", func(in *intent.PlanInput) {
			in.Appends[0].PayloadDigest = "append-payload-2"
		}},
		{"a projection mutation", func(in *intent.PlanInput) {
			in.ProjectionMutations[0].Operation = "DELETE"
		}},
		{"a commit precondition", func(in *intent.PlanInput) {
			in.Preconditions[0].Ref = "authority.external_master/v1"
		}},
		{"a reservation binding", func(in *intent.PlanInput) {
			in.Reservations[0].ReservationID = "reservation:2"
		}},
		{"the idempotency record", func(in *intent.PlanInput) {
			in.IdempotencyRecordRef = "idempotency:other"
		}},
		{"an approval requirement", func(in *intent.PlanInput) {
			in.ApprovalRequirementIDs = append(in.ApprovalRequirementIDs, "req.vp/v1")
		}},
		{"a revalidation rule", func(in *intent.PlanInput) {
			in.RevalidationRuleRefs = []string{"other_revalidation/v1"}
		}},
		{"the governance snapshot", func(in *intent.PlanInput) {
			in.Governance.SnapshotDigest = "governance-2"
		}},
		{"the conflict fence", func(in *intent.PlanInput) { in.Conflict.FenceToken = "fence-2" }},
		{"the execution mode", func(in *intent.PlanInput) { in.Mode = intent.ModeShadow }},
		{"the expiry", func(in *intent.PlanInput) {
			in.ExpiresAt = fixedClock()()
		}},
	}
	for _, m := range mutations {
		t.Run(m.name, func(t *testing.T) {
			in := planInput(t, def, rev)
			m.mutate(&in)
			plan, err := intent.CompilePlan(in, countingIDs("bbbbbbbb"))
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			if plan.Digest == baseline.Digest {
				t.Fatalf("changing %s left the plan digest unchanged", m.name)
			}
			if err := plan.VerifyDigest(); err != nil {
				t.Fatalf("the mutated plan digest does not verify: %v", err)
			}
			if plan.Executable() {
				t.Fatalf("a compiled plan reported itself executable")
			}
		})
	}

	t.Run("a tampered plan fails its own digest check", func(t *testing.T) {
		tampered := baseline
		tampered.Appends = append([]intent.PlannedAppend(nil), tampered.Appends...)
		tampered.Appends[0].ExpectedSequence = 99
		if err := tampered.VerifyDigest(); !errors.Is(err, intent.ErrInvalidPlan) {
			t.Fatalf("a tampered plan verified: %v", err)
		}
	})
}
