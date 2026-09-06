package promotionterminal_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/promotioncommit"
	domaincommit "github.com/monstercameron/hcm-next/internal/domains/promotion/commit"
	"github.com/monstercameron/hcm-next/internal/platform/execution/promotionterminal"
	"github.com/monstercameron/hcm-next/internal/transaction/idempotency"
	"github.com/monstercameron/hcm-next/internal/workflow/execute"
)

type mutatorFunc func(context.Context, dbport.Tx, domaincommit.Command) (promotioncommit.Receipt, error)

func (f mutatorFunc) Write(ctx context.Context, tx dbport.Tx, cmd domaincommit.Command) (promotioncommit.Receipt, error) {
	return f(ctx, tx, cmd)
}

type terminalFunc func(context.Context, dbport.Tx, execute.TerminalWriteRequest) (idempotency.ResultIdentity, error)

func (f terminalFunc) Write(ctx context.Context, tx dbport.Tx, req execute.TerminalWriteRequest) (idempotency.ResultIdentity, error) {
	return f(ctx, tx, req)
}

func request() (execute.TerminalWriteRequest, domaincommit.Command) {
	tenant := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	req := execute.TerminalWriteRequest{TenantID: tenant, PlanDigest: "sha256:plan"}
	req.Proposal.Revision.ProposalRevisionID = "22222222-2222-4222-8222-222222222222"
	req.Proposal.Revision.MaterialDigest.Digest = "sha256:proposal"
	cmd := domaincommit.Command{TenantID: tenant.String(), ProposalRevisionID: req.Proposal.Revision.ProposalRevisionID,
		ProposalDigest: req.Proposal.Revision.MaterialDigest.Digest, WorkflowPlanDigest: req.PlanDigest}
	return req, cmd
}

func TestWriterRunsTheDomainCommitBeforeTheTerminalFact(t *testing.T) {
	req, cmd := request()
	order := []string{}
	w := &promotionterminal.Writer{
		Resolver: promotionterminal.ResolverFunc(func(context.Context, dbport.Tx, execute.TerminalWriteRequest) (domaincommit.Command, error) {
			return cmd, nil
		}),
		Mutation: mutatorFunc(func(context.Context, dbport.Tx, domaincommit.Command) (promotioncommit.Receipt, error) {
			order = append(order, "mutation")
			return promotioncommit.Receipt{}, nil
		}),
		Next: terminalFunc(func(context.Context, dbport.Tx, execute.TerminalWriteRequest) (idempotency.ResultIdentity, error) {
			order = append(order, "terminal")
			return idempotency.ResultIdentity{ResultRef: "complete"}, nil
		}),
	}
	got, err := w.Write(context.Background(), nil, req)
	if err != nil || got.ResultRef != "complete" {
		t.Fatalf("Write = %+v, %v", got, err)
	}
	if len(order) != 2 || order[0] != "mutation" || order[1] != "terminal" {
		t.Fatalf("order = %v", order)
	}
}

func TestWriterNeverRecordsTerminalAfterAMutationFailure(t *testing.T) {
	req, cmd := request()
	injected := errors.New("injected")
	terminalCalled := false
	w := &promotionterminal.Writer{
		Resolver: promotionterminal.ResolverFunc(func(context.Context, dbport.Tx, execute.TerminalWriteRequest) (domaincommit.Command, error) {
			return cmd, nil
		}),
		Mutation: mutatorFunc(func(context.Context, dbport.Tx, domaincommit.Command) (promotioncommit.Receipt, error) {
			return promotioncommit.Receipt{}, injected
		}),
		Next: terminalFunc(func(context.Context, dbport.Tx, execute.TerminalWriteRequest) (idempotency.ResultIdentity, error) {
			terminalCalled = true
			return idempotency.ResultIdentity{}, nil
		}),
	}
	if _, err := w.Write(context.Background(), nil, req); !errors.Is(err, injected) {
		t.Fatalf("error = %v", err)
	}
	if terminalCalled {
		t.Fatal("terminal writer ran after mutation failed")
	}
}

func TestWriterRefusesCrossProposalCommand(t *testing.T) {
	req, cmd := request()
	cmd.ProposalDigest = "sha256:other"
	w := &promotionterminal.Writer{
		Resolver: promotionterminal.ResolverFunc(func(context.Context, dbport.Tx, execute.TerminalWriteRequest) (domaincommit.Command, error) {
			return cmd, nil
		}),
		Mutation: mutatorFunc(func(context.Context, dbport.Tx, domaincommit.Command) (promotioncommit.Receipt, error) {
			t.Fatal("mutation called")
			return promotioncommit.Receipt{}, nil
		}),
		Next: terminalFunc(func(context.Context, dbport.Tx, execute.TerminalWriteRequest) (idempotency.ResultIdentity, error) {
			t.Fatal("terminal called")
			return idempotency.ResultIdentity{}, nil
		}),
	}
	if _, err := w.Write(context.Background(), nil, req); !errors.Is(err, domaincommit.ErrInvalidCommand) {
		t.Fatalf("error = %v", err)
	}
}
