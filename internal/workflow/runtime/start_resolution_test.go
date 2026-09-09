package runtime_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

func TestResolveStartOutcome_CommittedStartIsProvenInReadOnlyTransaction(t *testing.T) {
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenantID := insertTenant(t, db, "dbedge003-committed")
	pf := newPromotionFixture(t, "dbedge003-tenant", "intent:dbedge003")
	req := pf.baseStartRequest(tenantID, "lost-response-key")
	selection := pf.Resolver.(stubResolver).sel

	var started runtime.StartReceipt
	inTenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
		var err error
		started, err = runtime.Start(context.Background(), tx, req)
		return err
	})

	// Resolution is historical proof and must not need approval facts that
	// were consulted before the original commit but are not stored on it.
	req.ProposalFacts = nil
	req.ApprovalFacts = nil
	var resolved runtime.StartResolution
	inTenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
		if _, err := tx.Exec(context.Background(), "SET TRANSACTION READ ONLY"); err != nil {
			return err
		}
		var err error
		resolved, err = runtime.ResolveStartOutcome(context.Background(), tx, req, selection)
		return err
	})
	if resolved.Instance.InstanceID != started.InstanceID || resolved.Instance.InputRef == "" {
		t.Fatalf("resolved instance = %+v, started id = %s", resolved.Instance, started.InstanceID)
	}
	if resolved.Instance.CorrelationID != req.CorrelationID || resolved.SemanticVersion != "1.0.0" {
		t.Fatalf("resolution = %+v", resolved)
	}
}

func TestResolveStartOutcome_RolledBackOrMissingRemainsUnresolved(t *testing.T) {
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenantID := insertTenant(t, db, "dbedge003-rollback")
	pf := newPromotionFixture(t, "dbedge003-tenant", "intent:dbedge003-rollback")
	req := pf.baseStartRequest(tenantID, "rolled-back-key")
	selection := pf.Resolver.(stubResolver).sel

	rollback := errors.New("simulate lost transaction")
	err := inTenantTxErr(conn, tenantID, func(tx dbport.Tx) error {
		if _, err := runtime.Start(context.Background(), tx, req); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("rollback setup: %v", err)
	}

	err = inTenantTxErr(conn, tenantID, func(tx dbport.Tx) error {
		if _, err := tx.Exec(context.Background(), "SET TRANSACTION READ ONLY"); err != nil {
			return err
		}
		_, err := runtime.ResolveStartOutcome(context.Background(), tx, req, selection)
		return err
	})
	if !errors.Is(err, runtime.ErrStartOutcomeUnresolved) {
		t.Fatalf("missing start error = %v, want unresolved", err)
	}
}

func TestResolveStartOutcome_RefusesEveryMismatchedIdentityPin(t *testing.T) {
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenantID := insertTenant(t, db, "dbedge003-mismatch")
	pf := newPromotionFixture(t, "dbedge003-tenant", "intent:dbedge003-mismatch")
	base := pf.baseStartRequest(tenantID, "exact-key")
	selection := pf.Resolver.(stubResolver).sel
	businessTx := uuid.New()
	base.BusinessTransactionID = &businessTx
	inTenantTx(t, conn, tenantID, func(tx dbport.Tx) error { _, err := runtime.Start(context.Background(), tx, base); return err })

	tests := []struct {
		name   string
		mutate func(*runtime.StartRequest, *runtime.WorkflowSelection)
	}{
		{name: "tenant", mutate: func(r *runtime.StartRequest, _ *runtime.WorkflowSelection) { r.TenantID = uuid.New() }},
		{name: "cell", mutate: func(r *runtime.StartRequest, _ *runtime.WorkflowSelection) { r.CellID = "cell-other" }},
		{name: "business transaction", mutate: func(r *runtime.StartRequest, _ *runtime.WorkflowSelection) {
			v := uuid.New()
			r.BusinessTransactionID = &v
		}},
		{name: "idempotency key", mutate: func(r *runtime.StartRequest, _ *runtime.WorkflowSelection) { r.StartIdempotencyKey = "other-key" }},
		{name: "proposal digest", mutate: func(r *runtime.StartRequest, _ *runtime.WorkflowSelection) {
			r.Proposal.Revision.MaterialDigest.Digest = "sha256:other"
		}},
		{name: "selection workflow", mutate: func(_ *runtime.StartRequest, s *runtime.WorkflowSelection) { s.WorkflowID = "workflow.other" }},
		{name: "selection digest", mutate: func(_ *runtime.StartRequest, s *runtime.WorkflowSelection) {
			s.Pin = version.Pin{CompiledPlanDigest: "sha256:other"}
		}},
		{name: "conflicting semantic and digest pin", mutate: func(_ *runtime.StartRequest, s *runtime.WorkflowSelection) { s.Pin.SemanticVersion = "9.9.9" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := base
			sel := selection
			tc.mutate(&req, &sel)
			err := inTenantTxErr(conn, tenantID, func(tx dbport.Tx) error {
				if _, err := tx.Exec(context.Background(), "SET TRANSACTION READ ONLY"); err != nil {
					return err
				}
				_, err := runtime.ResolveStartOutcome(context.Background(), tx, req, sel)
				return err
			})
			if err == nil || (!errors.Is(err, runtime.ErrStartOutcomeUnresolved) && runtime.CodeOf(err) != runtime.CodeStartConflict) {
				t.Fatalf("mismatch accepted: %v", err)
			}
		})
	}
}

func TestResolveStartOutcome_RequiresCompleteExactIdentityWithoutQuery(t *testing.T) {
	_, err := runtime.ResolveStartOutcome(context.Background(), nil, runtime.StartRequest{}, runtime.WorkflowSelection{})
	if !errors.Is(err, runtime.ErrStartOutcomeUnresolved) {
		t.Fatalf("err = %v, want unresolved outcome", err)
	}
	if got := fmt.Sprint(err); got == "" {
		t.Fatal("unresolved error has no diagnostic")
	}
}
