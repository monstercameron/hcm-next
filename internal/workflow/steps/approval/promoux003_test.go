package approval_test

// PROMOUX-003: "Enforce and explain separation of duties across promotion
// approvals." This file proves RED's core clause -- "one principal completes
// both finance and manager approval for the same proposal" -- is impossible,
// not merely discouraged, entirely through internal/workflow/steps/approval
// and internal/humanwork/workitem with no UI, transport or journey engine
// anywhere in the call path.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/rules"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/wire/digest"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/approverclass"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	intentapproval "github.com/monstercameron/human-capital-management-suite/internal/intent/approval"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	stepapproval "github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/approval"
)

// promoux003At is the fixed instant every PROMOUX-003 fixture below is
// stamped with, always well before the requirement's own decision deadline.
var promoux003At = time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)

// promoRequirement compiles a real, quorum-1 requirement the way
// internal/workflow/promotionexec compiles the finance and manager
// approvals -- distinct RequirementID, its own AuthorityFloor, a decision
// deadline -- through the same humanwork.Compile contract, without importing
// that sibling workflow package for a test fixture.
func promoRequirement(id, authorityFloor string) humanwork.ApprovalRequirement {
	decideBy := values.NewInstant(time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC))
	expiry := values.NewInstant(time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC))
	req, err := humanwork.Compile(humanwork.RequirementSpec{
		RequirementID: id, Revision: 1, Stage: 1,
		Candidates:     humanwork.Named("principal:placeholder", "policy:"+id, humanwork.Scope{Kind: humanwork.ScopeOrganization, Ref: "acme/engineering"}),
		AuthorityFloor: []string{authorityFloor},
		Quorum:         humanwork.Quorum{MinApprovals: 1},
		Deadline:       humanwork.Deadline{DecideBy: decideBy, Expiry: expiry},
		Escalation:     humanwork.EscalationPolicy{OnDeadline: humanwork.EscalationBlock, RuleID: "rule.promoux003.escalation/v1"},
		Separation:     humanwork.SeparationConstraints{RequesterMayNotApprove: true, RuleID: "rule.promoux003.separation/v1"},
		Invalidators:   []humanwork.Invalidator{{Kind: humanwork.InvalidatorMaterialProposalChange, RuleID: "rule.promoux003.invalidate/v1"}},
		Source: humanwork.RequirementSource{
			Tier: rules.ApprovalTierStandard, TableID: "table", TableVersion: "1", TableDigest: "sha256:" + strings.Repeat("d", 64),
			MatchedRowID: "row", GovernancePolicyRef: "policy:promoux003:1",
		},
	})
	if err != nil {
		panic(err)
	}
	return req
}

// promoProposal builds a proposal revision naming digestHex as its material
// digest -- distinct proposals get distinct digests, so tests that must not
// share a sibling scope never accidentally do.
func promoProposal(digestHex string) intent.ProposalRevision {
	intentID := "intent:promoux003:" + digestHex[:8]
	revisionID := "proposal:promoux003:" + digestHex[:8]
	return intent.ProposalRevision{
		IntentID: intentID, ProposalRevisionID: revisionID, Revision: 1,
		MaterialDigest: digest.Reference{
			ProfileID: "PROPOSAL", ProfileVersion: 1,
			SchemaID: "hcmnext.intent.ProposalRevision", SchemaVersion: 1,
			AlgorithmID: "sha256", CanonicalLength: 42,
			Digest: digestHex, ScopeBindingDigest: strings.Repeat("b", 64),
			IntentID: &intentID, ProposalRevisionID: &revisionID,
		},
	}
}

// promoDecision builds the immutable decision [stepapproval.Complete] binds
// against, naming req and proposal exactly.
func promoDecision(req humanwork.ApprovalRequirement, prop intent.ProposalRevision, principal, decisionID string, outcome intentapproval.Outcome) intentapproval.ApprovalDecision {
	return intentapproval.ApprovalDecision{
		DecisionID: decisionID,
		Binding: intentapproval.DecisionBinding{
			RequirementID: req.RequirementID, RequirementRevision: req.Revision,
			IntentID: prop.IntentID, ProposalRevisionID: prop.ProposalRevisionID, ProposalDigest: prop.MaterialDigest,
			TaskVersion: 1, RequirementDigest: req.Digest(), ResolutionExpressionDigest: req.ExpressionDigest,
		},
		Outcome:              outcome,
		Approver:             intentapproval.ApproverReference{PrincipalID: principal, Via: humanwork.SourceDirect},
		AuthorityDecisionRef: "authz:promoux003:" + decisionID, Reason: "reason:reviewed",
		DecidedAt: values.NewInstant(promoux003At),
	}
}

// promoOpenRouteStart persists one real APPROVAL work item end to end --
// Open, Route to the named sole candidate, Claim and Start -- on conn's own
// tenant-scoped transaction, and returns it IN_PROGRESS and ready for
// [stepapproval.Complete]. Every field on the returned item came back from
// PostgreSQL; nothing here is an in-memory fixture standing in for a write.
func promoOpenRouteStart(
	t *testing.T, ctx context.Context, conn *pgxadapter.Conn, tenant, instance uuid.UUID,
	nodeID string, req humanwork.ApprovalRequirement, prop intent.ProposalRevision, principal string,
) workitem.WorkItem {
	t.Helper()
	store := workitem.Store{}
	at := promoux003At

	var item workitem.WorkItem
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		item, err = stepapproval.Open(ctx, tx, store, stepapproval.OpenInput{
			TenantID: tenant, WorkflowInstanceID: instance, CorrelationID: "corr-" + instance.String(),
			SubjectRefs: []string{"worker:jane"},
			Node: stepapproval.CompiledApprovalNode{
				WorkflowID: "wf.promoux003", WorkflowVersion: 1, NodeID: nodeID,
				WorkType: "promotion.approval", PolicyRouteRef: "route.promoux003/v1",
				Visibility: workitem.VisibilityAssigneeOnly, OrganizationScopeID: "org:acme:engineering",
			},
			Requirement: req, Proposal: prop,
			Now: at, Meta: workitem.TransitionMeta{ActorPrincipalID: "system:test", Reason: "workitem.created", At: at},
		})
		return err
	})

	resolution := humanwork.Resolution{
		RequirementID: req.RequirementID, RequirementRevision: req.Revision, Outcome: humanwork.OutcomeResolved,
		Candidates:        []humanwork.Candidate{{PrincipalID: principal, Via: humanwork.SourceDirect, TermRef: "term:approver"}},
		ResolvedAt:        values.NewInstant(at),
		EffectiveAt:       values.NewInstant(at),
		DirectoryVersion:  "directory.test/1",
		ExpressionDigest:  req.ExpressionDigest,
		RequirementDigest: req.Digest(),
		QuorumRequired:    req.Quorum.MinApprovals,
	}
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		item, err = store.Route(ctx, tx, tenant, item.WorkItemID, item.ItemVersion,
			workitem.Assignment{Resolution: resolution, GovernancePolicyRef: req.Source.GovernancePolicyRef, Trigger: workitem.TriggerInitialRouting, ChosenOwner: principal},
			workitem.TransitionMeta{ActorPrincipalID: "system:test", Reason: "workitem.routed", At: at})
		return err
	})
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		item, err = store.Claim(ctx, tx, workitem.ClaimInput{
			TenantID: tenant, WorkItemID: item.WorkItemID, ExpectedVersion: item.ItemVersion,
			ClaimantPrincipalID: principal, ClaimExpiresAt: at.Add(time.Hour), Now: at,
			Meta: workitem.TransitionMeta{ActorPrincipalID: principal, Reason: "workitem.claimed", At: at},
		})
		return err
	})
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		item, err = store.Start(ctx, tx, tenant, item.WorkItemID, item.ItemVersion, at,
			workitem.TransitionMeta{ActorPrincipalID: principal, Reason: "workitem.started", At: at})
		return err
	})
	if item.Status != workitem.StatusInProgress {
		t.Fatalf("promoOpenRouteStart: item %s at %s, want IN_PROGRESS", item.WorkItemID, item.Status)
	}
	return item
}

// TestTodo_PROMOUX_003 is the PRIMARY: it proves the requirement's own
// authority-class binding is provably distinct (RED clause 2) and that
// separation of duties is decided by the approval owner itself -- Complete
// -- before it writes anything (RED clause 1 and PROMOUX-003's REFACTOR
// clause).
func TestTodo_PROMOUX_003(t *testing.T) {
	t.Run("approver resolution refuses an undifferentiated owner", func(t *testing.T) {
		if _, err := approverclass.DeriveDistinct("", approverclass.FinancePartner); !errors.Is(err, approverclass.ErrNoBase) {
			t.Fatalf("DeriveDistinct with no base = %v, want ErrNoBase", err)
		}
		if _, err := approverclass.DeriveDistinct("principal:base", approverclass.ClassUnspecified); !errors.Is(err, approverclass.ErrNoClass) {
			t.Fatalf("DeriveDistinct with no class = %v, want ErrNoClass", err)
		}
		finance, err := approverclass.DeriveDistinct("principal:promotion-approver", approverclass.FinancePartner)
		if err != nil {
			t.Fatalf("DeriveDistinct finance: %v", err)
		}
		manager, err := approverclass.DeriveDistinct("principal:promotion-approver", approverclass.CurrentManager)
		if err != nil {
			t.Fatalf("DeriveDistinct manager: %v", err)
		}
		if finance == manager {
			t.Fatalf("finance and manager derived to the same principal %q; RED clause 2 is not closed", finance)
		}
		if err := approverclass.RequireDistinct(finance, manager); err != nil {
			t.Fatalf("RequireDistinct(%q, %q): %v, want nil", finance, manager, err)
		}
		// The undifferentiated owner RED names literally: both classes
		// resolved (by a hypothetical caller that ignored DeriveDistinct) to
		// the identical configured principal.
		if err := approverclass.RequireDistinct("principal:promotion-approver", "principal:promotion-approver"); !errors.Is(err, approverclass.ErrSharedOwner) {
			t.Fatalf("RequireDistinct on a shared owner = %v, want ErrSharedOwner", err)
		}
		if err := approverclass.RequireDistinct("principal:a", ""); !errors.Is(err, approverclass.ErrUnresolved) {
			t.Fatalf("RequireDistinct with an unresolved class = %v, want ErrUnresolved", err)
		}
	})

	t.Run("Complete fails closed on an item with no proposal reference", func(t *testing.T) {
		req := promoRequirement("approval.promoux003.orphan", "finance_partner")
		item := workitem.WorkItem{
			WorkItemID: uuid.New(), Kind: workitem.KindApproval, ApprovalRequirementRef: req.RequirementID,
			// ProposalRef deliberately left empty.
		}
		d := promoDecision(req, promoProposal(strings.Repeat("9", 64)), "principal:x", "decision:orphan", intentapproval.OutcomeApproved)
		_, err := stepapproval.Complete(context.Background(), nil, workitem.Store{}, item, d, promoux003At, workitem.TransitionMeta{ActorPrincipalID: "principal:x", Reason: "test", At: promoux003At})
		if !errors.Is(err, stepapproval.ErrBindingMismatch) {
			t.Fatalf("Complete with no proposal ref = %v, want ErrBindingMismatch (fail closed)", err)
		}
	})

	t.Run("two distinct principals each complete their own requirement", func(t *testing.T) {
		db := pgtest.New(t)
		tenant := insertTenant(t, db, "promoux003-distinct")
		instance := uuid.New()
		insertInstance(t, db, tenant, instance, "approve_finance")
		conn := appConn(t, db)
		ctx := context.Background()

		prop := promoProposal(strings.Repeat("1", 64))
		financeReq := promoRequirement("approval.promoux003.finance.1", "finance_partner")
		managerReq := promoRequirement("approval.promoux003.manager.1", "current_manager")

		financeItem := promoOpenRouteStart(t, ctx, conn, tenant, instance, "approve_finance", financeReq, prop, "principal:finance-1")
		managerItem := promoOpenRouteStart(t, ctx, conn, tenant, instance, "approve_manager", managerReq, prop, "principal:manager-1")

		store := workitem.Store{}
		var completedFinance, completedManager workitem.WorkItem
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			d := promoDecision(financeReq, prop, "principal:finance-1", "decision:finance-1", intentapproval.OutcomeApproved)
			var err error
			completedFinance, err = stepapproval.Complete(ctx, tx, store, financeItem, d, promoux003At,
				workitem.TransitionMeta{ActorPrincipalID: "principal:finance-1", Reason: "workitem.completed", At: promoux003At})
			return err
		})
		if completedFinance.Status != workitem.StatusCompleted {
			t.Fatalf("finance item status = %s, want COMPLETED", completedFinance.Status)
		}
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			d := promoDecision(managerReq, prop, "principal:manager-1", "decision:manager-1", intentapproval.OutcomeApproved)
			var err error
			completedManager, err = stepapproval.Complete(ctx, tx, store, managerItem, d, promoux003At,
				workitem.TransitionMeta{ActorPrincipalID: "principal:manager-1", Reason: "workitem.completed", At: promoux003At})
			return err
		})
		if completedManager.Status != workitem.StatusCompleted {
			t.Fatalf("manager item status = %s, want COMPLETED", completedManager.Status)
		}
	})

	t.Run("one principal is refused the second requirement on the same proposal", func(t *testing.T) {
		db := pgtest.New(t)
		tenant := insertTenant(t, db, "promoux003-conflict")
		instance := uuid.New()
		insertInstance(t, db, tenant, instance, "approve_finance")
		conn := appConn(t, db)
		ctx := context.Background()

		prop := promoProposal(strings.Repeat("2", 64))
		financeReq := promoRequirement("approval.promoux003.finance.2", "finance_partner")
		managerReq := promoRequirement("approval.promoux003.manager.2", "current_manager")
		const conflicted = "principal:conflicted"

		financeItem := promoOpenRouteStart(t, ctx, conn, tenant, instance, "approve_finance", financeReq, prop, conflicted)
		managerItem := promoOpenRouteStart(t, ctx, conn, tenant, instance, "approve_manager", managerReq, prop, conflicted)

		store := workitem.Store{}
		var completedFinance workitem.WorkItem
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			d := promoDecision(financeReq, prop, conflicted, "decision:conflict-finance", intentapproval.OutcomeApproved)
			var err error
			completedFinance, err = stepapproval.Complete(ctx, tx, store, financeItem, d, promoux003At,
				workitem.TransitionMeta{ActorPrincipalID: conflicted, Reason: "workitem.completed", At: promoux003At})
			return err
		})
		if completedFinance.Status != workitem.StatusCompleted {
			t.Fatalf("finance item status = %s, want COMPLETED", completedFinance.Status)
		}

		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			d := promoDecision(managerReq, prop, conflicted, "decision:conflict-manager", intentapproval.OutcomeApproved)
			_, err := stepapproval.Complete(ctx, tx, store, managerItem, d, promoux003At,
				workitem.TransitionMeta{ActorPrincipalID: conflicted, Reason: "workitem.completed", At: promoux003At})
			return err
		})
		if !errors.Is(err, stepapproval.ErrSeparationConflict) {
			t.Fatalf("second (manager) Complete by the same principal %s = %v, want ErrSeparationConflict", conflicted, err)
		}

		// The refusal left the manager item exactly as Start left it: no
		// completion recorded, no version advanced, no CompletedBy set. This
		// is what "impossible, not merely discouraged" means at the row
		// level.
		var reloaded workitem.WorkItem
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var loadErr error
			reloaded, loadErr = store.Load(ctx, tx, tenant, managerItem.WorkItemID)
			return loadErr
		})
		if reloaded.Status != workitem.StatusInProgress || reloaded.ItemVersion != managerItem.ItemVersion || reloaded.CompletedBy != "" {
			t.Fatalf("manager item after refusal = %+v, want unchanged IN_PROGRESS at version %d with no completer",
				reloaded, managerItem.ItemVersion)
		}
	})
}

// alwaysCurrentSession is a trivial [workitem.SessionRevocationPort] that
// never reports revocation, standing in for whatever session store a real
// transport composes -- irrelevant to what this test proves, which is
// separation of duties, not session validity.
type alwaysCurrentSession struct{}

func (alwaysCurrentSession) CheckRevocation(context.Context, string, time.Time) error { return nil }

// TestTodo_PROMOUX_003_Security drives a conflicted principal through the
// approval path with no UI, no transport and no journey engine anywhere in
// the call graph -- only internal/workflow/steps/approval and
// internal/humanwork/workitem, called the way a Go test calls a Go
// function -- and proves it is refused. It exercises a second, independent
// enforcement point from the PRIMARY test's direct [stepapproval.Complete]
// calls: [workitem.Store.DecideApproval], EP-WORK-003's own completion path,
// composed with [stepapproval.SeparationAuthority] as its
// [workitem.AuthorityRecheckPort] -- proving the same policy holds for a
// caller that reaches this package only through the port EP-WORK-003 left
// for exactly this purpose, not only for a caller that imports Complete
// directly.
func TestTodo_PROMOUX_003_Security(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "promoux003-security")
	instance := uuid.New()
	insertInstance(t, db, tenant, instance, "approve_finance")
	conn := appConn(t, db)
	ctx := context.Background()

	prop := promoProposal(strings.Repeat("3", 64))
	financeReq := promoRequirement("approval.promoux003.finance.3", "finance_partner")
	managerReq := promoRequirement("approval.promoux003.manager.3", "current_manager")
	const conflicted = "principal:security-conflicted"

	financeItem := promoOpenRouteStart(t, ctx, conn, tenant, instance, "approve_finance", financeReq, prop, conflicted)
	managerItem := promoOpenRouteStart(t, ctx, conn, tenant, instance, "approve_manager", managerReq, prop, conflicted)

	store := workitem.Store{}
	var completedFinance workitem.WorkItem
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		d := promoDecision(financeReq, prop, conflicted, "decision:security-finance", intentapproval.OutcomeApproved)
		var err error
		completedFinance, err = stepapproval.Complete(ctx, tx, store, financeItem, d, promoux003At,
			workitem.TransitionMeta{ActorPrincipalID: conflicted, Reason: "workitem.completed", At: promoux003At})
		return err
	})
	if completedFinance.Status != workitem.StatusCompleted {
		t.Fatalf("finance item status = %s, want COMPLETED", completedFinance.Status)
	}

	// The same conflicted principal now attempts the manager requirement
	// through EP-WORK-003's own transport-facing write, DecideApproval, with
	// PROMOUX-003's driver as its current-authority port. No HTTP handler,
	// no journey engine, no UI: this is the port a wire caller would use,
	// called directly.
	err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
		_, err := store.DecideApproval(ctx, tx, workitem.DecideApprovalInput{
			TenantID: tenant, WorkItemID: managerItem.WorkItemID, ExpectedVersion: managerItem.ItemVersion,
			ProposalRevisionRef: managerItem.ProposalRef,
			Decision:            workitem.ApprovalDecisionApprove,
			ReasonRef:           "reason:security-test",
			DecidingPrincipal:   conflicted,
			SessionRef:          "session:security-test",
			Session:             alwaysCurrentSession{},
			Authority:           stepapproval.SeparationAuthority{},
			Now:                 promoux003At,
			Meta:                workitem.TransitionMeta{ActorPrincipalID: conflicted, Reason: "workitem.completed", At: promoux003At},
		})
		return err
	})
	if !errors.Is(err, workitem.ErrWorkItem) || workitem.CodeOf(err) != workitem.CodeAuthorityChanged {
		t.Fatalf("DecideApproval by the conflicted principal = %v (code %q), want CodeAuthorityChanged", err, workitem.CodeOf(err))
	}

	var reloaded workitem.WorkItem
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var loadErr error
		reloaded, loadErr = store.Load(ctx, tx, tenant, managerItem.WorkItemID)
		return loadErr
	})
	if reloaded.Status != workitem.StatusInProgress || reloaded.CompletedBy != "" {
		t.Fatalf("manager item after the DecideApproval refusal = %+v, want unchanged IN_PROGRESS with no completer", reloaded)
	}
}
