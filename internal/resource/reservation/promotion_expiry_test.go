package reservation

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/governance/decision"
	"github.com/monstercameron/hcm-next/internal/governance/revalidate"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func TestPromotionReservationExpiresBeforeExecutionRefusesConsumption(t *testing.T) {
	now := reservationNow()
	store := NewStore()
	req := request(Digest([]byte("promotion-expiry")))
	req.ExpiresAt = now.Add(time.Minute)

	hold, err := store.Acquire(req, Quantity{Value: 1000, Scale: 3}, now)
	if err != nil {
		t.Fatal(err)
	}

	renewed, err := store.Renew(hold.ID, hold.Fence, req.AuthorityDigest, now.Add(2*time.Minute), now.Add(30*time.Second))
	if err != nil {
		t.Fatalf("renew before expiry: %v", err)
	}
	if !renewed.Request.ExpiresAt.Equal(now.Add(2 * time.Minute)) {
		t.Fatalf("renewed expiry = %s, want %s", renewed.Request.ExpiresAt, now.Add(2*time.Minute))
	}
	if renewed.Request.ProposalDigest != req.ProposalDigest || renewed.Request.AuthorityDigest != req.AuthorityDigest {
		t.Fatalf("renewal changed proposal or authority identity: %#v", renewed.Request)
	}

	if _, err := store.Consume(hold.ID, hold.Fence, now.Add(2*time.Minute)); !errors.Is(err, ErrExpired) || CodeOf(err) != CodeExpired {
		t.Fatalf("consume after expiry = %v, code=%q; want typed expired refusal", err, CodeOf(err))
	}
	got, ok := store.Get(hold.ID)
	if !ok || got.Status != Expired {
		t.Fatalf("stored reservation after refused consume = %#v, found=%v", got, ok)
	}
	for _, event := range store.Events(hold.ID) {
		if event.To == Consumed {
			t.Fatal("expired reservation recorded consumption")
		}
	}
}

func TestPromotionReservationExpiryRoutesRevalidationToReapproval(t *testing.T) {
	historicalFacts := promotionFacts("budget-reservation-held-v1")
	historicalInputs := promotionInputs(historicalFacts)
	historicalDecision, err := decision.Compose(historicalInputs)
	if err != nil {
		t.Fatalf("compose historical approval: %v", err)
	}
	historical := revalidate.HistoricalApproval{
		Inputs: historicalInputs, Decision: historicalDecision, Facts: historicalFacts, PlanDigest: "plan:promotion-expiry",
	}

	currentFacts := historicalFacts
	currentFacts.BudgetPosition.Budget.ObservationID = "budget-reservation-expired-v1"
	result, err := revalidate.Revalidate(
		func() values.Instant { return values.NewInstant(nowForPromotionTest()) },
		historical, currentFacts, historical.PlanDigest,
	)
	if err != nil {
		t.Fatalf("revalidate expired reservation fact: %v", err)
	}
	if result.Confirmed {
		t.Fatal("expired reservation fact was confirmed")
	}
	if result.Requirement != revalidate.RequirementReplanRequired && result.Requirement != revalidate.RequirementReapprovalRequired {
		t.Fatalf("requirement = %s, want REPLAN_REQUIRED or REAPPROVAL_REQUIRED", result.Requirement)
	}
	if result.Requirement == revalidate.RequirementNone {
		t.Fatal("expired reservation fact returned no revalidation requirement")
	}
	if !slices.Contains(result.ChangedInputs, revalidate.ChangedBudgetPosition) {
		t.Fatalf("changed inputs = %v, want budget/position", result.ChangedInputs)
	}
}

// TestTodo_RESERVE_001_Mutation_ExpiryCheck kills the mutation that removes
// the execution-time expiry barrier from Store.Consume.
func TestTodo_RESERVE_001_Mutation_ExpiryCheck(t *testing.T) {
	now := reservationNow()
	store := NewStore()
	req := request(Digest([]byte("expiry-mutation")))
	req.ExpiresAt = now.Add(time.Second)
	hold, err := store.Acquire(req, Quantity{Value: 1000, Scale: 3}, now)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.Consume(hold.ID, hold.Fence, now.Add(time.Second)); CodeOf(err) != CodeExpired {
		t.Fatalf("stale consume error = %v, code=%q; removing the expiry check must fail this test", err, CodeOf(err))
	}
	if got, ok := store.Get(hold.ID); !ok || got.Status == Consumed {
		t.Fatalf("stale reservation was consumed: %#v, found=%v", got, ok)
	}
}

func nowForPromotionTest() time.Time {
	return time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
}

func promotionFacts(budgetObservation string) revalidate.Facts {
	return revalidate.Facts{
		AuthZ:               revalidate.AuthZFact{Effect: decision.Allow, PolicyVersion: "authz.promotion/v1"},
		Session:             revalidate.SessionFact{Effect: decision.Allow, SessionID: "session:promotion", Assurance: "AAL2"},
		SourceAuthority:     revalidate.SourceAuthorityFact{Decision: "authority.assignment/v1"},
		FieldClassification: revalidate.FieldClassificationFact{Version: "classification.hr/v1"},
		LegalPolicy:         revalidate.LegalPolicyFact{LegalPackVersion: "legal.us/v1", PolicyPackVersion: "policy.promotion/v1"},
		BudgetPosition: revalidate.BudgetPositionFact{
			Budget:   revalidate.ResourceFact{Effect: decision.Allow, ObservationID: budgetObservation},
			Position: revalidate.ResourceFact{Effect: decision.Allow, ObservationID: "position-reservation-held-v1"},
		},
		Conflict: revalidate.ConflictFact{Effect: decision.Allow, Classification: "COMPATIBLE", EvidenceRef: "conflict:promotion/v1"},
	}
}

func promotionInputs(f revalidate.Facts) decision.Inputs {
	return decision.Inputs{
		ProposalRevisionDigest: Digest([]byte("promotion-proposal")),
		Context: decision.Context{
			Principal: "principal:manager", Delegation: "none", Capability: "promotion.execute/v1",
			Resource: "worker:promotion", Fields: []string{"assignment.position", "compensation.base_pay"},
			CurrentOrganization: "org:acme", TargetOrganization: "org:acme", Purpose: "promotion", Risk: "HIGH",
			Authority: f.SourceAuthority.Decision, Legal: f.LegalPolicy.LegalPackVersion,
		},
		ControlSnapshot: decision.ControlSnapshot{
			Digest: "control:promotion/v1", PolicyBundle: f.LegalPolicy.PolicyPackVersion,
			LegalContext: f.LegalPolicy.LegalPackVersion, Classification: f.FieldClassification.Version,
			ReferenceData: "reference:promotion/v1", SourceWatermark: "watermark:promotion/v1",
		},
		RulePackVersions:     []decision.RulePackVersion{{ID: "policy.promotion", Version: "v1", Digest: "digest:policy-promotion-v1"}},
		ApprovalRequirements: []decision.ApprovalRequirement{{ID: "approval.finance", Version: "v1", Satisfaction: decision.ApprovalSatisfied}},
		SoDVerdicts:          []decision.SoDVerdict{{RuleID: "sod.promotion", Version: "v1", State: decision.Allow, Satisfied: true}},
		Subdecisions: []decision.Subdecision{
			{ID: "authz", Source: "authz", State: f.AuthZ.Effect, FiredRules: []decision.RuleRef{{ID: "authz.policy", Version: f.AuthZ.PolicyVersion}}},
			{ID: "session", Source: "session", State: f.Session.Effect, FiredRules: []decision.RuleRef{{ID: "session.identity", Version: f.Session.SessionID}, {ID: "session.assurance", Version: f.Session.Assurance}}},
			{ID: "budget", Source: "budget", State: f.BudgetPosition.Budget.Effect, FiredRules: []decision.RuleRef{{ID: "budget.observation", Version: f.BudgetPosition.Budget.ObservationID}}},
			{ID: "position", Source: "position", State: f.BudgetPosition.Position.Effect, FiredRules: []decision.RuleRef{{ID: "position.observation", Version: f.BudgetPosition.Position.ObservationID}}},
			{ID: "conflict", Source: "conflict", State: f.Conflict.Effect, FiredRules: []decision.RuleRef{{ID: "conflict.classification", Version: f.Conflict.Classification}, {ID: "conflict.evidence", Version: f.Conflict.EvidenceRef}}},
		},
	}
}
