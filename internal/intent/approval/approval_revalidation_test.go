package approval_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/governance/revalidate"
	"github.com/monstercameron/hcm-next/internal/humanwork"
	"github.com/monstercameron/hcm-next/internal/intent/approval"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
	transactionplan "github.com/monstercameron/hcm-next/internal/transaction/plan"
)

type authorityPort struct {
	result approval.AuthorityResult
	err    error
}

func (p authorityPort) RevalidateApprovalAuthority(context.Context, approval.AuthorityRequest) (approval.AuthorityResult, error) {
	return p.result, p.err
}

type proposalPort struct {
	state approval.ProposalState
	err   error
}

func (p proposalPort) CurrentProposal(context.Context, string) (approval.ProposalState, error) {
	return p.state, p.err
}

type controlPort struct {
	result revalidate.Result
	err    error
}

func (p controlPort) RevalidateApproval(context.Context, revalidate.HistoricalApproval, revalidate.Facts, string) (revalidate.Result, error) {
	return p.result, p.err
}

func revalidationFixture(t *testing.T) (approval.ApprovalDecision, transactionplan.TransactionPlan, approval.ExecutionRevalidationPorts, approval.ExecutionRevalidationRequest) {
	t.Helper()
	f := mustFixture(t)
	d, err := f.Binder.Record(f.Vote(humanwork.RequirementHRBP, humanwork.PrincipalHRBP, approval.OutcomeApproved))
	if err != nil {
		t.Fatalf("record approval: %v", err)
	}
	p := executionPlan(t, f)
	authority := approval.AuthorityResult{Valid: true, RequirementID: d.Binding.RequirementID, PrincipalID: d.Approver.PrincipalID, DecisionRef: d.AuthorityDecisionRef}
	ports := approval.ExecutionRevalidationPorts{
		Authority: authorityPort{result: authority},
		Proposal:  proposalPort{state: approval.ProposalState{IntentID: d.Binding.IntentID, ProposalRevisionID: p.ProposalRevisionID, MaterialDigest: p.ProposalDigest}},
		Control:   controlPort{result: revalidate.Result{Confirmed: true, HistoricalDigest: "historical", RecomposedDigest: "historical", PlanDigest: p.Digest}},
	}
	req := approval.ExecutionRevalidationRequest{
		Decision: d, Plan: p, EvaluatedAt: values.NewInstant(time.Date(2026, 9, 5, 12, 45, 0, 0, time.UTC)),
	}
	return d, p, ports, req
}

func TestTodo_APPROVAL_005(t *testing.T) {
	d, p, ports, req := revalidationFixture(t)
	got, err := approval.RevalidateBeforeExecution(context.Background(), ports, req)
	if err != nil {
		t.Fatalf("revalidate before execution: %v", err)
	}
	if got.Decision.DecisionID != d.DecisionID || got.Plan.Digest != p.Digest || !got.Authority.Valid || !got.Controls.Confirmed {
		t.Fatalf("admission = %+v", got)
	}
}

func TestTodo_APPROVAL_005_Golden(t *testing.T) {
	_, _, ports, req := revalidationFixture(t)
	got, err := approval.RevalidateBeforeExecution(context.Background(), ports, req)
	if err != nil {
		t.Fatalf("revalidate: %v", err)
	}
	if got.Explain() == "" || got.Controls.PlanDigest != got.Plan.Digest {
		t.Fatalf("admission explanation/binding = %q / %+v", got.Explain(), got.Controls)
	}
}

func TestTodo_APPROVAL_005_Security(t *testing.T) {
	d, p, ports, req := revalidationFixture(t)
	cases := []struct {
		name   string
		mutate func(*approval.ExecutionRevalidationPorts, *approval.ExecutionRevalidationRequest)
		want   error
	}{
		{"proposal changed", func(_ *approval.ExecutionRevalidationPorts, r *approval.ExecutionRevalidationRequest) {
			r.Decision.Binding.ProposalRevisionID = "revision:other"
		}, approval.ErrPlanMismatch},
		{"authority changed", func(ps *approval.ExecutionRevalidationPorts, _ *approval.ExecutionRevalidationRequest) {
			ps.Authority = authorityPort{result: approval.AuthorityResult{Valid: false, RequirementID: d.Binding.RequirementID, PrincipalID: d.Approver.PrincipalID, Reason: "role revoked"}}
		}, approval.ErrExecutionAuthority},
		{"control changed", func(ps *approval.ExecutionRevalidationPorts, _ *approval.ExecutionRevalidationRequest) {
			ps.Control = controlPort{result: revalidate.Result{Confirmed: false, Requirement: revalidate.RequirementReapprovalRequired, PlanDigest: p.Digest, Explanation: "policy moved"}}
		}, approval.ErrReapprovalRequired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			localPorts, localReq := ports, req
			tc.mutate(&localPorts, &localReq)
			_, gotErr := approval.RevalidateBeforeExecution(context.Background(), localPorts, localReq)
			if !errors.Is(gotErr, tc.want) {
				t.Fatalf("error = %v, want %v", gotErr, tc.want)
			}
		})
	}
}

func TestTodo_APPROVAL_005_Mutation(t *testing.T) {
	_, _, ports, req := revalidationFixture(t)
	ports.Authority = nil
	if _, err := approval.RevalidateBeforeExecution(context.Background(), ports, req); !errors.Is(err, approval.ErrExecutionAuthority) {
		t.Fatalf("missing authority port = %v", err)
	}
	_, _, ports, req = revalidationFixture(t)
	ports.Control = controlPort{result: revalidate.Result{Confirmed: true, PlanDigest: "other-plan"}}
	if _, err := approval.RevalidateBeforeExecution(context.Background(), ports, req); !errors.Is(err, approval.ErrPlanMismatch) {
		t.Fatalf("control result for another plan = %v", err)
	}
}
