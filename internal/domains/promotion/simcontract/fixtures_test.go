package simcontract_test

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/simassign"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/simcomp"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/simcontract"
	"github.com/monstercameron/human-capital-management-suite/internal/governance/decision"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/messaging"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/conflict"
)

// ---------------------------------------------------------------------------
// Zero-side-effect assertion harness
//
// PROMO-004's RED case requires that assembling and persisting a simulation
// contract never creates a domain revision, calls an external operation,
// enqueues an outbox effect, raises a MessageIntent or creates a WorkItem.
// simcontract.Assemble and simcontract.Persist accept none of these as
// dependencies -- there is no parameter through which one could be wired in
// at all -- so the harness below exists to make that structural fact an
// active, regression-proof test rather than an unenforced claim: every fake
// fails the test the instant any of its methods is called, and the primary
// test exercises the full Assemble -> Validate -> Persist.Store -> Persist.Load
// path while holding one of each.
// ---------------------------------------------------------------------------

// revisionStorePort is what a domain revision store looks like from the
// outside: append one more effective-dated fact to an owned stream.
type revisionStorePort interface {
	AppendRevision(ctx context.Context, streamID string, expectedSequence uint64, payload []byte) error
}

// externalOperationPort is what INTG-001's external-operation boundary looks
// like from the outside: invoke a write-capable operation against a
// connected incumbent system. It is a write shape, not
// [github.com/monstercameron/human-capital-management-suite/internal/connectivity/spi.Adapter],
// which is read-only by contract in this release.
type externalOperationPort interface {
	InvokeExternalOperation(ctx context.Context, destinationRef string, payload []byte) (observationRef string, err error)
}

// outboxPort is what the durable outbox looks like from the outside: enqueue
// one more effect intent for later delivery.
type outboxPort interface {
	Enqueue(ctx context.Context, effectID, destinationRef, idempotencyKey string) error
}

// messageIntentSink is what raising a communication looks like from the
// outside, in the exact typed shape internal/messaging.Accept consumes.
type messageIntentSink interface {
	Send(ctx context.Context, m messaging.MessageIntent) error
}

// workItemStore is what durable human-work persistence looks like from the
// outside, in the exact typed shape internal/humanwork/workitem.WorkItem
// records.
type workItemStore interface {
	Create(ctx context.Context, item workitem.WorkItem) error
}

// failingRevisionStore, failingExternalOperationPort, failingOutbox,
// failingMessageSink and failingWorkItemStore each fail the enclosing test the
// instant any of their one method is invoked. A zero-effect harness's whole
// job is to never have a reason to report anything else.
type failingRevisionStore struct{ t *testing.T }

func (f failingRevisionStore) AppendRevision(context.Context, string, uint64, []byte) error {
	f.t.Helper()
	f.t.Fatal("zero-effect violation: a domain revision store was called")
	return nil
}

type failingExternalOperationPort struct{ t *testing.T }

func (f failingExternalOperationPort) InvokeExternalOperation(context.Context, string, []byte) (string, error) {
	f.t.Helper()
	f.t.Fatal("zero-effect violation: an external operation was invoked")
	return "", nil
}

type failingOutbox struct{ t *testing.T }

func (f failingOutbox) Enqueue(context.Context, string, string, string) error {
	f.t.Helper()
	f.t.Fatal("zero-effect violation: an outbox effect was enqueued")
	return nil
}

type failingMessageSink struct{ t *testing.T }

func (f failingMessageSink) Send(context.Context, messaging.MessageIntent) error {
	f.t.Helper()
	f.t.Fatal("zero-effect violation: a MessageIntent was sent")
	return nil
}

type failingWorkItemStore struct{ t *testing.T }

func (f failingWorkItemStore) Create(context.Context, workitem.WorkItem) error {
	f.t.Helper()
	f.t.Fatal("zero-effect violation: a WorkItem was created")
	return nil
}

// zeroEffectHarness bundles one of each failing port. Holding it live for the
// duration of a test proves nothing under test reaches for one: type
// assertions against these interfaces are unreachable, the ports are never
// passed anywhere, and yet the harness is exactly what a caller would wire in
// if simcontract ever grew a dependency on one of these five surfaces.
type zeroEffectHarness struct {
	Revisions         revisionStorePort
	ExternalOperation externalOperationPort
	Outbox            outboxPort
	Messages          messageIntentSink
	WorkItems         workItemStore
}

func newZeroEffectHarness(t *testing.T) zeroEffectHarness {
	t.Helper()
	return zeroEffectHarness{
		Revisions:         failingRevisionStore{t: t},
		ExternalOperation: failingExternalOperationPort{t: t},
		Outbox:            failingOutbox{t: t},
		Messages:          failingMessageSink{t: t},
		WorkItems:         failingWorkItemStore{t: t},
	}
}

// ---------------------------------------------------------------------------
// Shared fixture helpers
// ---------------------------------------------------------------------------

func mustResourceKey(t testing.TB, tenant values.TenantId, kind values.Kind, segments ...string) values.ResourceKey {
	t.Helper()
	key, err := values.NewResourceKey(tenant, kind, segments...)
	if err != nil {
		t.Fatalf("NewResourceKey: %v", err)
	}
	return key
}

func mustRevisionToken(t testing.TB, stream string, seq uint64) values.RevisionToken {
	t.Helper()
	rev, err := values.NewSequenceRevision(stream, seq)
	if err != nil {
		t.Fatalf("NewSequenceRevision: %v", err)
	}
	return rev
}

func mustFixtureMoney(t testing.TB, amount, currency string) values.Money {
	t.Helper()
	m, err := fixtures.Money(amount, currency)
	if err != nil {
		t.Fatalf("Money(%q,%q): %v", amount, currency, err)
	}
	return m
}

func mustFixtureWorker(t testing.TB, key string) values.EntityRef {
	t.Helper()
	ref, err := fixtures.WorkerRef(key)
	if err != nil {
		t.Fatalf("WorkerRef(%q): %v", key, err)
	}
	return ref
}

// The resource kinds these fixtures address writes and reads by. They are
// package-local, the same way internal/domains/promotion/snapshot mints its
// own resourceKindPromoIn rather than borrowing a domain's kind.
const (
	kindWorkerAssignment    values.Kind = "worker_assignment"
	kindWorkerCompensation  values.Kind = "worker_compensation"
	kindManagerRelationship values.Kind = "manager_relationship"
	kindCompensationPool    values.Kind = "compensation_pool"
)

// ---------------------------------------------------------------------------
// Promotion fixture: Jane Doe, ENG-SWE3/P3 -> ENG-MGR1/M1, 165,000 -> 180,000
// USD effective 2026-10-01, mirroring the promote-into-management reference
// (internal/workflow/simulate.NewPromotionSetup) and PROMO-002/003's own
// effect vocabulary. The raise is a 9.09% increase, within the 10% finance
// threshold, but the grade change alone escalates the reference workflow to a
// required Finance Partner approval -- which is exactly the approval PROMO-003
// (simcomp.ApprovalFinancePartner) names for an above-band or, as here, a
// grade-change placement.
// ---------------------------------------------------------------------------

func promotionFixtureInput(t testing.TB) simcontract.AssembleInput {
	t.Helper()
	tenant := fixtures.Tenant
	worker := mustFixtureWorker(t, "jane-doe")

	assignmentSubject := intent.SubjectReference{Kind: "worker", SubjectID: worker.Id, AuthorityDomain: "people"}
	compensationSubject := intent.SubjectReference{Kind: "worker", SubjectID: worker.Id, AuthorityDomain: "rewards"}

	assignmentKey := mustResourceKey(t, tenant, kindWorkerAssignment, worker.Id)
	compensationKey := mustResourceKey(t, tenant, kindWorkerCompensation, worker.Id)
	budgetKey := mustResourceKey(t, tenant, kindCompensationPool, "eng-platform", "FY2026")

	assignmentRevision := mustRevisionToken(t, "people.assignment."+worker.Id, 7)
	compensationRevision := mustRevisionToken(t, "rewards.compensation."+worker.Id, 11)
	budgetRevision := mustRevisionToken(t, "budget.compensation_pool.eng-platform.FY2026", 3)

	assignmentEffectID := "people.assignment.revision@2026-10-01#jane-doe-001"
	compensationEffectID := "rewards.compensation.revision@2026-10-01#jane-doe-001"
	budgetEffectID := "budget.compensation_pool.reservation@2026-10-01#jane-doe-001"

	assignmentEffect, err := simcontract.NewSideEffect(
		assignmentEffectID, string(simassign.EffectAssignmentRevision), "people.assignment",
		"people.assignment/"+worker.Id, string(simassign.Reversible),
		"people.assignment.supersede", "observe.people.assignment_effective",
	)
	if err != nil {
		t.Fatalf("NewSideEffect(assignment): %v", err)
	}
	compensationEffect, err := simcontract.NewSideEffect(
		compensationEffectID, string(simcomp.EffectCompensationRevision), "rewards.compensation",
		"rewards.compensation/"+worker.Id, string(simassign.Reversible),
		"rewards.compensation.supersede", "observe.rewards.compensation_effective",
	)
	if err != nil {
		t.Fatalf("NewSideEffect(compensation): %v", err)
	}
	budgetEffect, err := simcontract.NewSideEffect(
		budgetEffectID, string(simcomp.EffectBudgetReservation), "budget.compensation_pool",
		"budget.compensation_pool/eng-platform@FY2026", string(simassign.Compensatable),
		"budget.release_compensation_budget", "observe.budget.reservation_confirmed",
	)
	if err != nil {
		t.Fatalf("NewSideEffect(budget): %v", err)
	}

	delta := mustFixtureMoney(t, "15000.00", "USD")

	return simcontract.AssembleInput{
		Intent: simcontract.IntentRef{
			IntentID:      "intent_promo_jane_doe_001",
			IntentType:    promotion.IntentType,
			IntentVersion: promotion.IntentVersion,
		},
		Snapshot: simcontract.SnapshotRef{
			SnapshotDigest: "sha256:fixture-promotion-input-snapshot-jane-doe-2026-10-01",
			Tenant:         tenant,
			Subject:        worker,
		},
		ProposalCandidateDigest: "sha256:fixture-promotion-candidate-jane-doe-2026-10-01",

		Reads: []intent.PlannedRead{
			{ResourceKey: assignmentKey, ExpectedRevision: assignmentRevision},
			{ResourceKey: compensationKey, ExpectedRevision: compensationRevision},
			{ResourceKey: budgetKey, ExpectedRevision: budgetRevision},
		},
		Writes: []intent.PlannedWrite{
			{
				Subject: assignmentSubject, ResourceKey: assignmentKey,
				FieldPath: "people.job_code", CurrentCanonicalText: "ENG-SWE3", ProposedCanonicalText: "ENG-MGR1",
				SourceAuthorityDecision: "PEOPLE_ASSIGNMENT_WRITE_ALLOWED", ExpectedRevision: assignmentRevision,
			},
			{
				Subject: assignmentSubject, ResourceKey: assignmentKey,
				FieldPath: "people.grade", CurrentCanonicalText: "P3", ProposedCanonicalText: "M1",
				SourceAuthorityDecision: "PEOPLE_ASSIGNMENT_WRITE_ALLOWED", ExpectedRevision: assignmentRevision,
			},
			{
				Subject: compensationSubject, ResourceKey: compensationKey,
				FieldPath:            "rewards.compensation.annualized_base_pay",
				CurrentCanonicalText: "165000.00 USD", ProposedCanonicalText: "180000.00 USD",
				SourceAuthorityDecision: "REWARDS_COMPENSATION_WRITE_ALLOWED", ExpectedRevision: compensationRevision,
			},
		},
		Streams: []intent.PlanParticipant{
			{ParticipantID: "people.assignment", StreamID: "people.assignment." + worker.Id, StorageClass: "LOCAL_EVENT_STREAM", Local: true},
			{ParticipantID: "rewards.compensation", StreamID: "rewards.compensation." + worker.Id, StorageClass: "LOCAL_EVENT_STREAM", Local: true},
			{ParticipantID: "budget.compensation_pool", StreamID: "budget.compensation_pool.eng-platform.FY2026", StorageClass: "EXTERNAL_AUTHORITY", Local: false},
		},
		Conflicts: []conflict.Candidate{},
		Approvals: []decision.ApprovalRequirement{
			{ID: simcomp.ApprovalFinancePartner, Version: "v1", Satisfaction: decision.ApprovalPending},
		},
		Authority: []evidence.SourceAuthority{
			{Kind: evidence.AuthorityLocal, System: "hcmnext.people", PolicyRef: "people.assignment.write/2026.1"},
			{Kind: evidence.AuthorityLocal, System: "hcmnext.rewards", PolicyRef: "rewards.compensation.write/2026.1"},
		},
		LegalObligations: []decision.Obligation{
			{ID: "legal.pay_equity_review", Version: "2026.1", Scope: "compensation_change", Owner: "legal.compliance"},
		},
		SideEffects: []simcontract.SideEffect{assignmentEffect, compensationEffect, budgetEffect},
		Repair: []intent.CompensationBinding{
			{EffectID: assignmentEffectID, Strategy: "SUPERSEDING_REVISION", RepairPlanID: "people.assignment.supersede"},
			{EffectID: compensationEffectID, Strategy: "SUPERSEDING_REVISION", RepairPlanID: "rewards.compensation.supersede"},
			{EffectID: budgetEffectID, Strategy: "RELEASE_RESERVATION", RepairPlanID: "budget.release_compensation_budget"},
		},
		Cost: simcontract.Cost{State: simcontract.CostEvaluated, Amount: &delta},
		Completion: simcontract.Completion{
			State:                simcontract.CompletionPendingApproval,
			OutstandingApprovals: []string{simcomp.ApprovalFinancePartner},
			Detail:               "the promotion changes grade, which the reference threshold table always escalates to a required Finance Partner approval",
		},
		Revalidation: simcontract.Revalidation{
			Rules:                 []string{promotion.RulePackVersion, "rewards.band.rule_pack/2026.1"},
			ControlSnapshotDigest: "sha256:fixture-promotion-control-snapshot-2026-09-01",
		},
		Findings: []promotion.Finding{
			{Code: promotion.CodeBudgetObservationOnly, Severity: promotion.SeverityAdvisory, Field: "budget", Message: "observed budget is not a reservation"},
		},
	}
}

// ---------------------------------------------------------------------------
// Manager Change fixture: the pure-HRIS boundary reference (PROMO-006). Omar
// Reyes keeps every field of his placement; only his manager relationship
// moves. No compensation, no budget, no position occupancy -- reusing
// [simcontract.AssembleInput]'s exact shape with several sections legitimately
// stated as empty rather than richly populated, which is the point PROMO-006
// exists to prove.
// ---------------------------------------------------------------------------

func managerChangeFixtureInput(t testing.TB) simcontract.AssembleInput {
	t.Helper()
	tenant := fixtures.Tenant
	worker := mustFixtureWorker(t, "omar-reyes")

	subject := intent.SubjectReference{Kind: "worker", SubjectID: worker.Id, AuthorityDomain: "org"}
	relationshipKey := mustResourceKey(t, tenant, kindManagerRelationship, worker.Id)
	relationshipRevision := mustRevisionToken(t, "org.relationship."+worker.Id, 4)
	effectID := "org.manager_relationship.change@2026-07-01#omar-reyes-001"

	effect, err := simcontract.NewSideEffect(
		effectID, string(simassign.EffectManagerRelationship), "org.manager_relationship",
		"org.manager_relationship/"+worker.Id, string(simassign.Reversible),
		"org.manager_relationship.supersede", "observe.org.manager_chain_current",
	)
	if err != nil {
		t.Fatalf("NewSideEffect(manager relationship): %v", err)
	}

	return simcontract.AssembleInput{
		Intent: simcontract.IntentRef{
			IntentID:      "intent_manager_change_omar_reyes_001",
			IntentType:    "hcmnext.people.change_manager",
			IntentVersion: "v1",
		},
		Snapshot: simcontract.SnapshotRef{
			SnapshotDigest: "sha256:fixture-manager-change-input-snapshot-omar-reyes-2026-07-01",
			Tenant:         tenant,
			Subject:        worker,
		},
		ProposalCandidateDigest: "sha256:fixture-manager-change-candidate-omar-reyes-2026-07-01",

		Reads: []intent.PlannedRead{
			{ResourceKey: relationshipKey, ExpectedRevision: relationshipRevision},
		},
		Writes: []intent.PlannedWrite{
			{
				Subject: subject, ResourceKey: relationshipKey,
				FieldPath:               "org.manager_relationship.manager_id",
				CurrentCanonicalText:    "44444444-4444-4444-8444-444444444444",
				ProposedCanonicalText:   "77777777-7777-4777-8777-777777777777",
				SourceAuthorityDecision: "ORG_MANAGER_RELATIONSHIP_WRITE_ALLOWED", ExpectedRevision: relationshipRevision,
			},
		},
		Streams: []intent.PlanParticipant{
			{ParticipantID: "org.manager_relationship", StreamID: "org.relationship." + worker.Id, StorageClass: "LOCAL_EVENT_STREAM", Local: true},
		},
		Conflicts:        []conflict.Candidate{},
		Approvals:        []decision.ApprovalRequirement{},
		LegalObligations: []decision.Obligation{},
		Authority: []evidence.SourceAuthority{
			{Kind: evidence.AuthorityLocal, System: "hcmnext.org", PolicyRef: "org.manager_relationship.write/2026.1"},
		},
		SideEffects: []simcontract.SideEffect{effect},
		Repair: []intent.CompensationBinding{
			{EffectID: effectID, Strategy: "SUPERSEDING_REVISION", RepairPlanID: "org.manager_relationship.supersede"},
		},
		Cost: simcontract.Cost{State: simcontract.CostNone},
		Completion: simcontract.Completion{
			State:  simcontract.CompletionReady,
			Detail: "a manager relationship change requires no approval beyond the base change request",
		},
		Revalidation: simcontract.Revalidation{
			Rules:                 []string{"org.manager_relationship.rules/1.0.0"},
			ControlSnapshotDigest: "sha256:fixture-manager-change-control-snapshot-2026-06-01",
		},
	}
}
