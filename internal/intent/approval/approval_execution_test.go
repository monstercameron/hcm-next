package approval_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/approval"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	transactionplan "github.com/monstercameron/human-capital-management-suite/internal/transaction/plan"
)

type executionHeads struct{}

func (executionHeads) CurrentHead(_ context.Context, tenant values.TenantId, stream string) (transactionplan.Head, error) {
	return transactionplan.Head{Tenant: tenant, StreamKey: stream, Sequence: 42, Digest: "head:42", DigestAlgorithm: "sha256"}, nil
}

func executionPlan(t *testing.T, f *approval.Fixture) transactionplan.TransactionPlan {
	t.Helper()
	rev := f.Current()
	events := make([]transactionplan.PlannedEvent, 0, len(rev.Writes))
	for i, write := range rev.Writes {
		events = append(events, transactionplan.PlannedEvent{
			StreamKey: write.ExpectedRevision.Stream(), Sequence: int64(43 + i),
			EventType: "promotion.fact", SchemaRef: "hcmnext.promotion.fact/v1",
			Digest: "event-digest-" + write.FieldPath,
		})
	}
	effects := make([]transactionplan.OutboxEffect, 0, len(rev.Effects))
	for _, effect := range rev.Effects {
		effects = append(effects, transactionplan.OutboxEffect{
			EffectID: effect.EffectID, DestinationRef: effect.DestinationRef,
			SchemaRef: "hcmnext.effect/v1", PayloadDigest: "payload-" + effect.EffectID,
			IdempotencyKey: "idem-" + effect.EffectID,
		})
	}
	got, err := transactionplan.Prepare(context.Background(), executionHeads{}, transactionplan.PrepareRequest{
		Proposal: rev, GovernanceDecisionDigest: "governance:decision:1", Events: events,
		OutboxEffects: effects, IdempotencyKey: "execute:" + rev.ProposalRevisionID,
		Now:       values.NewInstant(time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)),
		ExpiresAt: values.NewInstant(time.Date(2026, 9, 5, 13, 0, 0, 0, time.UTC)),
		PlanID:    "plan:approval:test",
	})
	if err != nil {
		t.Fatalf("prepare plan: %v", err)
	}
	return got
}

func TestTodo_APPROVAL_003(t *testing.T) {
	f := mustFixture(t)
	d, err := f.Binder.Record(f.Vote(humanwork.RequirementHRBP, humanwork.PrincipalHRBP, approval.OutcomeApproved))
	if err != nil {
		t.Fatalf("record approval: %v", err)
	}
	p := executionPlan(t, f)
	now := values.NewInstant(time.Date(2026, 9, 5, 12, 30, 0, 0, time.UTC))
	store := approval.NewMemoryExecutionStore()
	got, err := store.ConsumeAndElect(context.Background(), approval.ConsumeRequest{
		Plan: p, Approvals: []approval.ApprovalRecord{{Decision: d, State: approval.ApprovalActive}}, Now: now,
	})
	if err != nil {
		t.Fatalf("consume and elect: %v", err)
	}
	if !got.Executable || got.PlanDigest != p.Digest || got.Digest == "" {
		t.Fatalf("eligibility = %+v", got)
	}
	replay, err := store.ConsumeAndElect(context.Background(), approval.ConsumeRequest{
		Plan: p, Approvals: []approval.ApprovalRecord{{Decision: d, State: approval.ApprovalActive}}, Now: now,
	})
	if err != nil || replay.Digest != got.Digest {
		t.Fatalf("same transaction replay = %+v, err=%v", replay, err)
	}
}

func TestTodo_APPROVAL_003_Property(t *testing.T) {
	f := mustFixture(t)
	d, err := f.Binder.Record(f.Vote(humanwork.RequirementHRBP, humanwork.PrincipalHRBP, approval.OutcomeApproved))
	if err != nil {
		t.Fatalf("record approval: %v", err)
	}
	p := executionPlan(t, f)
	now := values.NewInstant(time.Date(2026, 9, 5, 12, 30, 0, 0, time.UTC))
	for _, tc := range []struct {
		name  string
		state approval.ApprovalState
		exp   values.Instant
		want  error
	}{
		{"revoked", approval.ApprovalRevoked, values.Instant{}, approval.ErrApprovalRevoked},
		{"superseded", approval.ApprovalSuperseded, values.Instant{}, approval.ErrApprovalSuperseded},
		{"expired", approval.ApprovalActive, values.NewInstant(time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)), approval.ErrApprovalExpired},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := approval.NewMemoryExecutionStore()
			_, gotErr := store.ConsumeAndElect(context.Background(), approval.ConsumeRequest{
				Plan: p, Approvals: []approval.ApprovalRecord{{Decision: d, State: tc.state, ExpiresAt: tc.exp}}, Now: now,
			})
			if !errors.Is(gotErr, tc.want) {
				t.Fatalf("error = %v, want %v", gotErr, tc.want)
			}
		})
	}
	first := approval.NewMemoryExecutionStore()
	if _, err := first.ConsumeAndElect(context.Background(), approval.ConsumeRequest{Plan: p, Approvals: []approval.ApprovalRecord{{Decision: d}}, Now: now}); err != nil {
		t.Fatalf("first consume: %v", err)
	}
	other := p
	other.PlanID = "plan:other"
	// A changed plan id changes the plan digest, so the same immutable decision
	// cannot be consumed against a second executable plan.
	other.Digest = "tampered"
	_, gotErr := first.ConsumeAndElect(context.Background(), approval.ConsumeRequest{Plan: other, Approvals: []approval.ApprovalRecord{{Decision: d}}, Now: now})
	if !errors.Is(gotErr, approval.ErrPlanNotExecutable) {
		t.Fatalf("tampered second plan = %v, want ErrPlanNotExecutable", gotErr)
	}
}

func TestTodo_APPROVAL_003_Mutation(t *testing.T) {
	f := mustFixture(t)
	d, err := f.Binder.Record(f.Vote(humanwork.RequirementHRBP, humanwork.PrincipalHRBP, approval.OutcomeApproved))
	if err != nil {
		t.Fatalf("record approval: %v", err)
	}
	p := executionPlan(t, f)
	p.ProposalDigest = "sha256:" + "0" + p.ProposalDigest[7:]
	store := approval.NewMemoryExecutionStore()
	_, gotErr := store.ConsumeAndElect(context.Background(), approval.ConsumeRequest{
		Plan: p, Approvals: []approval.ApprovalRecord{{Decision: d}}, Now: values.NewInstant(time.Date(2026, 9, 5, 12, 30, 0, 0, time.UTC)),
	})
	if !errors.Is(gotErr, approval.ErrPlanNotExecutable) {
		t.Fatalf("tampered plan = %v, want ErrPlanNotExecutable", gotErr)
	}
	if _, gotErr = store.ConsumeAndElect(context.Background(), approval.ConsumeRequest{
		Plan: executionPlan(t, f), Approvals: []approval.ApprovalRecord{{Decision: d}}, Now: values.NewInstant(time.Date(2026, 9, 5, 12, 30, 0, 0, time.UTC)),
	}); gotErr != nil {
		t.Fatalf("refused plan consumed approval: %v", gotErr)
	}
}
